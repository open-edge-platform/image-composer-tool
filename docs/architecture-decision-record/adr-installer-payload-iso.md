# ADR: Unattended Payload ISO — Installer-Side Deployment of a Pre-Built Raw Image

**Status**: Accepted
**Date**: 2026-09-27
**Updated**: N/A
**Authors**: Image Composer Tool Team
**Technical Area**: Image Composition / Provisioning / Live Installer

---

## Summary

ICT's existing unattended ISO flow builds a GRUB2 + xorriso hybrid ISO that
carries every resolved `.deb` in a `cache-repo/` and re-installs packages onto
the target disk at deploy time via `imageos.InstallImageOs`. This is slow,
non-deterministic (the target resolves the same dependency closure the build
host did), and unsuitable for fleet provisioning of a fully-known custom
software stack that needs a fully deterministic, pre-built system written to
target hardware block-for-block with no package resolution on target.

This ADR adds `systemConfig.installerPayload`, a mode flag on the existing ISO
template. When enabled, isomaker chains the existing rawmaker internally to
build a raw disk image from the same template, compresses it onto the ISO with
a sha256 manifest, and live-installer gains a deploy path that verifies the
payload, writes it directly to the target disk, grows the last partition and
its filesystem to fill the disk, resets machine identity, and reboots -
instead of running the package-install path.

---

## Context

Some deployments need an ISO that deploys a pre-built raw disk image already
loaded with a tuned kernel, an Ubuntu 24.04 base, and a custom stack of
applications, middleware, drivers, libraries, and services. Package
re-installation on the target is undesirable for this use case:

- It is slow relative to a block-level `dd` write.
- It requires the target to resolve and download the same dependency closure
  the build host resolved, which is non-deterministic across time (upstream
  repos can change) and requires target-side network access to a package
  mirror.
- It does not deterministically reproduce the exact bytes validated at build
  time, which matters for a stack with strict compliance/AI-workload
  requirements.

Not in scope: Ubuntu Subiquity/autoinstall, preseed, kickstart. ICT's own live
installer remains the deployment mechanism, per
[adr-declarative-installer.md](adr-declarative-installer.md) and the ISO
composition boundary defined in
[adr-image-extension.md](adr-image-extension.md).

---

## Decision

### 1. One-stage build, not two separate templates

`systemConfig.installerPayload.enabled: true` is a mode flag on the **same**
ISO template that already describes the deployed system - packages, kernel,
users, disk layout. It is not a new `target.imageType` value and not a
separately-referenced payload template.

**Why not a new `imageType`.** It would need a JSON-Schema enum change, a new
`default-*.yml` per OS/arch in `DefaultConfigLoader.LoadDefaultConfig`, and a
new `case` in six providers' `BuildImage` switch, or unsupported OS/arch
combinations would hard-fail with "unsupported image type" instead of falling
back to the existing `iso` behavior.

**Why not `baseline.source` (referencing a raw template as an overlay
baseline).** `IsOverlayMode()` is checked before the `ImageType` switch, and
overlay mode skips the OS-default merge entirely, silently dropping the
live-installer wiring that an unattended ISO depends on.

**Why not a separate payload template referenced from the ISO template
(mirroring `systemConfig.initramfs.template`).** In the existing ISO flow, the
main template's `systemConfig` + `disk` already describe the deployed system:
`template-dump.yaml` is handed to live-installer, which calls
`InstallImageOs` with it. The separately-referenced initramfs template is the
*installer environment*, not the payload. The ISO template is already the
payload spec - payload mode only changes **how** the described system reaches
the target (a raw image written at deploy time, versus package-by-package
installation), so every existing field keeps its exact current meaning. A
second template would duplicate packages, kernel, users, and disk layout, and
require new cross-template validation (matching os/dist/arch, rejecting a
`baseline` on the payload, distinct `systemConfig.name`) that a single
template makes unnecessary.

Consequences:

- No duplication: packages, kernel, users, disk layout are declared once.
- Migrating an existing unattended-ISO template to payload mode is a 2-line
  diff (`systemConfig.installerPayload.enabled: true` plus, optionally, a
  `compression` override).
- One build produces **both** artifacts: the payload `.raw` lands in
  `ImageBuildDir` before being grafted onto the ISO, so a single invocation
  yields a standalone bootable raw image (usable directly in a VM for stack
  testing) and the `.iso`.

### 2. Installer-side disk growth, not build-time over-provisioning

The payload raw image is built at a fixed size (`disk.size` in the template,
sized to the stack's footprint plus headroom). live-installer grows the GPT,
the last partition, and its filesystem to fill whatever target disk was
actually selected, after the write completes and before reboot.

This duplicates the fs-type dispatch logic already present in
`configureFirstBootLastPartitionAutoExpand` (first-boot growth for a `raw`
image type), because that existing helper derives its target disk from
`findmnt /` on the *build host* and is gated to `imageType == "raw"` - it is
not reusable from the installer process, which runs in the initrd against a
disk it has just written and has no live root filesystem to introspect via
`findmnt`. The alternative (building at the exact target disk size ahead of
time) is not viable because the target disk size is not known until deploy
time, and fleet hardware is not homogeneous.

### 3. Compressed payload with a pre-write sha256 gate, no rollback

The payload is carried as `payload.raw.<ext>` plus `payload.manifest.yaml`
(recording `compressedSha256`, `rawSha256`, and both byte counts) plus
`payload.sbom.json`. Compression defaults to zstd; xz, gz, and none are also
supported.

The **pre-write gate is the compressed digest** - it is the only one checkable
in one cheap pass before any byte touches the target disk, and the booted
initrd's `/tmp` is a RAM-backed tmpfs with no room to stage a decompressed
multi-GB image for a `rawSha256` check ahead of time.

A whole-disk write is irreversible by nature, so there is **no rollback**.
Before the first byte is written, any failure (checksum mismatch, disk too
small, disk not found) exits non-zero with the target disk untouched and the
unattended installer script drops to an interactive shell, exactly as it does
today for a package-install failure. After the write begins, a failure during
the write, partition growth, or identity reset wipes the target disk's
signatures (`wipefs -a`) so it cannot present a half-written bootable GPT,
logs prominently, exits non-zero, and does not reboot. The one exception is
the final EFI boot-order update: by then the disk is already fully written,
grown, and identity-reset, so a failure there is reported as an error but
does **not** wipe the disk - the deployed system is valid and can be booted
manually or have its boot entry repaired.

### 4. Zero new shell-allowlist entries

`dd`, `sgdisk`, `sfdisk`, `growpart`, `wipefs`, `sync`, `sha256sum`, `zstd`,
`xz`, `gzip`, `partx`, `udevadm`, `lsblk`, `e2fsck`, `resize2fs`, `xfs_growfs`,
`mkswap` were all already present in the shell command allowlist
(`internal/utils/shell/shell.go`). `partprobe`, `btrfs`, and `blockdev` are
not allowlisted; the implementation uses `partx -u` + `udevadm settle` instead
of `partprobe`, reuses the exported `imagedisc.SystemBlockDevices()` instead of
shelling out to `blockdev`, and fails cleanly with a clear message on btrfs
payloads rather than adding a new allowlist entry for an unsupported path.

No command string emitted by the deploy path may contain `` || ``:
`internal/utils/shell`'s command-validation separator scan checks `"|"` before
`"||"`, so `a || b` is mis-split into `a` and `| b`, which is then rejected as
`command | not found`. This is called out in code comments at every call site
that assembles a compound command, and is asserted against directly in tests.

---

## Consequences

- **`installRoot` collision, solved by staging the ISO tree into a sibling
  directory.** With one template, rawmaker's mount point and isomaker's ISO
  staging tree resolve to the same path
  (`<chrootImageBuildDir>/<systemConfig.name>`), and the ISO build's staging
  cleanup does an `rm -rf` on it. isomaker stages the ISO tree into
  `<installRoot>-isoroot` instead of `ImageOs.GetInstallRoot()` when payload
  mode is enabled - a contained change inside isomaker, not a naming
  constraint pushed onto template authors.
- **`disk.artifacts` (e.g. qcow2 conversion) is ignored in payload mode**,
  logged rather than silently dropped: the payload raw image is retained in
  `ImageBuildDir` as a first-class second artifact, not converted away by the
  normal raw-image post-processing pipeline.
- **`copyImagePkgsToIso` (the `cache-repo/` of every resolved `.deb`) is
  skipped in payload mode.** live-installer's deploy path never calls
  `InstallImageOs`, so shipping every package on the ISO as well as inside the
  payload raw image is dead weight.
- **The ISO template's OS defaults come from `default-iso-x86_64.yml`, not
  `default-raw-x86_64.yml`.** This means the raw default's dm-verity
  immutability and systemd-boot do not apply to payload-mode builds. This is
  correct, not a gap: a dm-verity-protected root is fundamentally incompatible
  with installer-side growth (growth would invalidate the verity hash tree),
  and the ISO default's simpler `esp + ext4 root` layout is the right payload
  shape. `ValidateISOPrerequisites` rejects a payload-mode template with
  `immutability.enabled` set, since installer-side growth of the `end: "0"`
  partition is mandatory and there is no way to opt out of it.
- **`-iso-level 3` was added to both xorriso invocation branches** (hybrid
  BIOS+UEFI and UEFI-only). Without it, the ISO9660 single-file 4 GiB cap
  would reject a multi-gigabyte payload. Both branches already pass
  `-graft-points`, which is how the compressed payload, manifest, and SBOM are
  grafted onto the ISO without a full-size copy into the staging tree.
- **`manifest.DefaultSPDXFile` is a mutable process-global**, reassigned on
  every SBOM generation. Chaining raw-image build + initrd build + ISO build
  in one process makes the payload's own SBOM subject to last-writer-wins
  against the initrd's SBOM. The payload SBOM is snapshotted to
  `payload.sbom.json` immediately after the raw image build completes and
  before initrd package resolution runs, to avoid losing the
  compliance-critical stack SBOM. A follow-up should thread the SBOM path as
  an explicit parameter instead of relying on snapshot timing.

---

## Risks and Mitigations

| Risk | Impact | Mitigation |
| --- | --- | --- |
| `installRoot` collision between rawmaker's mount point and isomaker's ISO staging tree | ISO staging cleanup could `rm -rf` the raw build's own mount point mid-build | Stage the ISO tree into `<installRoot>-isoroot` in payload mode; unit-tested that the two paths differ |
| Peak disk usage - one invocation now holds the payload raw, its compressed form, and the ISO simultaneously | Build host could run out of disk space on large stacks | `df` free-space pre-check in `ValidateISOPrerequisites`; graft-instead-of-copy avoids one additional full-size copy of the payload |
| The `\|\|` shell-allowlist trap | A command string containing `\|\|` is silently mis-parsed and rejected | Documented in code comments at every compound-command call site; unit tests assert no emitted command contains `\|\|` |
| `dd` is last in a decompression pipe, so a mid-stream decompressor failure can yield exit 0 on a truncated disk (no `pipefail` available - `set` is not allowlisted and `;` would split the command) | A corrupted or truncated payload write could go undetected | Dual belt: the pre-write compressed-digest check guarantees valid input bytes; the post-write `dd` byte-count check catches a short write. Residual risk: a non-corruption decompressor failure (e.g. OOM) after a valid digest check is caught only by the byte count, not by content |
| Only ext2/ext3/ext4, xfs, and swap growable partitions, LVM, and LUKS payloads are unsupported (`btrfs`/fat family are not shell-allowlisted for growth; LVM/LUKS growth is a materially different problem) | An unsupported payload filesystem could otherwise silently fail to grow, reporting a successful deploy | `validateInstallerPayloadDiskLayout` rejects these at template-validation time, before any target disk write is attempted |
| Post-write full-content verification (`rawSha256` readback) is not performed by default | A silent bit-level corruption during write, not caught by the byte-count check, would go undetected | Left as a default-off option for a future change; roughly doubles install time on a large payload, judged not worth the default cost given the belt-and-braces byte-count check |

---

## Alternatives Considered

- **Two-stage build** (build the payload raw as a separate artifact in a prior
  pipeline stage, reference it from the ISO template): rejected because it
  requires the raw and ISO templates to be kept in sync by hand (packages,
  kernel, users, disk layout duplicated), and needs new cross-template
  validation. The one-stage, single-template design gets both artifacts from
  one build with no duplication.
- **Build-time-only disk sizing** (size the payload raw to exactly the target
  disk's capacity, skip installer-side growth): rejected because the target
  disk's exact size is not known at build time, and target hardware is not
  homogeneous across the fleet.
- **Uncompressed payload carriage**: rejected as the default because it
  roughly doubles the ISO size for no benefit when the initrd already carries
  a decompressor; `compression: none` remains available for constrained
  decompression environments.
- **New `target.imageType: iso-payload` enum value**: rejected - see
  [Decision §1](#1-one-stage-build-not-two-separate-templates) above.

---

## Related

- [adr-image-extension.md](adr-image-extension.md) - defines the ISO
  composition boundary this ADR operates within; updated with a note
  clarifying that this feature is not "generic ISO remastering."
- [adr-declarative-installer.md](adr-declarative-installer.md) - the
  declarative live installer this ADR adds a new deploy path to.
- [adr-overlay-grow-resize.md](adr-overlay-grow-resize.md) - prior art for
  disk/filesystem growth logic in ICT.
