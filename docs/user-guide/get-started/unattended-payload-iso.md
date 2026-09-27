# Unattended Payload ISO Tutorial

This guide walks through building and deploying an **installer payload ISO**:
an ISO that carries a pre-built raw disk image and writes it directly to
target hardware, instead of reinstalling packages on the target. This is the
right shape when the deployed system is fully known at build time — for
example a custom software stack (tuned kernel, base OS, applications and
runtime dependencies, middleware, drivers, and services) — and you want
deployment to be fast, deterministic, and independent of target-side package
resolution.

For the field reference, see
[`systemConfig.installerPayload`](../architecture/image-composer-tool-templates.md#systemconfiginstallerpayload).
For the design rationale, see
[ADR: Unattended Payload ISO](../../architecture-decision-record/adr-installer-payload-iso.md).

## How it differs from a regular unattended ISO

A regular `imageType: iso` template builds an ISO that carries a `cache-repo/`
of every resolved `.deb` and a serialized template. On boot, `live-installer`
partitions the target disk and calls the same package-install logic used at
build time — the target re-resolves and installs the same packages.

A payload ISO instead builds the described system as a raw disk image
*during the same build*, compresses it onto the ISO with a sha256 manifest,
and skips package installation on target entirely: `live-installer` verifies
the payload, writes it to the target disk with `dd`, grows the last partition
and its filesystem to fill whatever disk was selected, resets machine
identity, and reboots.

## 1. Enable payload mode on an ISO template

Payload mode is a two-line addition to an existing `imageType: iso` template
— every other field (`packages`, `kernel`, `users`, `disk`) keeps its exact
existing meaning, since it already describes the system that ends up on the
target disk:

```yaml
target:
  os: ubuntu
  dist: ubuntu24
  arch: x86_64
  imageType: iso

systemConfig:
  installerPayload:
    enabled: true
    compression: zstd   # optional; zstd is the default

  packages:
    - your-stack-packages-here

  initramfs:
    template: default-initrd-unattended-x86_64.yml

disk:
  size: 20GiB   # required: payload mode's build preflight needs disk.size
                # explicit, since the ISO OS defaults don't supply one
```

A worked starting point is
[`image-templates/ubuntu24/ubuntu24-x86_64-installer-payload-iso.yml`](../../../image-templates/ubuntu24/ubuntu24-x86_64-installer-payload-iso.yml),
which marks the stack-specific package list, kernel, package
repositories, and additional files as clearly-labeled placeholders.

Validate the template before building:

```bash
./image-composer-tool validate image-templates/ubuntu24/ubuntu24-x86_64-installer-payload-iso.yml
```

## 2. Build

Payload mode needs both binaries — `image-composer-tool` and
`live-installer` — exactly like a regular unattended ISO build (see the
[Usage Guide](./usage-guide.md#building-an-image)):

```bash
go build -buildmode=pie -o ./build/live-installer ./cmd/live-installer
earthly +build   # or: go build -o ./build/image-composer-tool ./cmd/image-composer-tool

sudo -E ./build/image-composer-tool build image-templates/ubuntu24/ubuntu24-x86_64-installer-payload-iso.yml
```

One invocation emits **two artifacts** in the build output directory:

- `<name>-<version>.iso` — the installer ISO
- `<name>-<version>.raw` — the same system as a standalone bootable raw
  image, useful for testing the stack directly in a VM without going
  through the installer

A helper script mirroring the existing unattended-ISO scripts is at
[`scripts/build_ubuntu24_installer_payload_iso.sh`](../../../scripts/build_ubuntu24_installer_payload_iso.sh)
(supports `--qemu-test` / `--with-qemu`).

## 3. Deploy and verify with QEMU

Create a blank target disk and an NVRAM copy, then boot the ISO against the
disk with OVMF, exactly as for a regular unattended ISO:

```bash
qemu-img create -f raw /tmp/target.raw 60G
cp /usr/share/OVMF/OVMF_VARS_4M.fd /tmp/ovmf_vars.fd

sudo qemu-system-x86_64 \
    -m 2048 -enable-kvm -cpu host \
    -drive if=none,file=<built.iso>,format=raw,readonly=on,id=cdrom0 \
    -device ide-cd,drive=cdrom0,bootindex=1 \
    -drive if=none,file=/tmp/target.raw,format=raw,id=disk0 \
    -device nvme,drive=disk0,serial=deadbeef \
    -drive if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd \
    -drive if=pflash,format=raw,file=/tmp/ovmf_vars.fd \
    -nographic -serial mon:stdio
```

On the serial console, the deploy path logs each step in order:

```
manifest loaded
payload sha256 verified
target disk selected: /dev/sda
writing payload...
write verified
growing partition and filesystem
machine identity reset
```

No `bash -i` interactive shell should appear — that only happens on failure
(see [Troubleshooting](#troubleshooting) below).

After reboot, verify the deployed disk directly (no ISO attached this time):

```bash
sudo qemu-system-x86_64 \
    -m 2048 -enable-kvm -cpu host \
    -drive if=none,file=/tmp/target.raw,format=raw,id=disk0 \
    -device nvme,drive=disk0,serial=deadbeef \
    -drive if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd \
    -drive if=pflash,format=raw,file=/tmp/ovmf_vars.fd \
    -nographic -serial mon:stdio
```

Confirm:

1. `login:` appears on the console.
2. A stack marker is present — the pinned kernel (`uname -r`) and, once the
   custom stack is dropped in, its services under `systemctl list-units`.
3. The root filesystem grew to fill the disk (`df -h /` should show ~60G,
   not the template's original `disk.size`).

(2) and (3) are the checks that actually distinguish a payload deployment
from a fresh package install — a bare `login:` grep alone would pass either
way.

## Swapping in your own stack

Every extension point already exists in the template schema; none of this is
new for payload mode:

| What | Where |
|---|---|
| Private package drop (e.g. an internal `.deb` repo) | `packageRepositories[].path` |
| Signing keys | `pkey` / `pkeys` |
| Application/runtime packages | `systemConfig.packages[]` |
| Custom or tuned kernel | `systemConfig.kernel.{packages,version,enableExtraModules,cmdline}` |
| Config files, unit files, licenses, model weights | `systemConfig.additionalFiles[]` |
| Build-time setup (DKMS builds, post-install steps) | `systemConfig.configurations[].cmd` |

## Constraints

- **`imageType: iso` only.** The schema rejects `installerPayload` on any
  other image type.
- **Incompatible with `systemConfig.immutability`** (dm-verity): installer-side
  growth would invalidate a dm-verity hash tree, and there is no way to opt
  out of that growth, so validation **rejects** templates that set both.
- **`disk.artifacts[]` is ignored** — the payload is always a raw image.
- Only `ext2`/`ext3`/`ext4`, `xfs`, and swap partitions are grown by the
  installer; validation rejects any other fsType (fat32/fat16/vfat, btrfs) on
  the growable (`end: "0"`) partition. LVM and LUKS payloads are not supported
  in this release.
- **`resetInstanceIdentity` requires a single-root layout**: machine-id, SSH
  host keys, and the systemd random seed are reset on the partition mounted
  at `/` only, so validation rejects a layout with a separate partition
  mounted at or under `/etc` or `/var`.
- The initrd template referenced by `systemConfig.initramfs.template` must
  boot the **unattended** installer path — `live-installer`'s attended path
  rejects `installerPayload` mode at runtime, so validation rejects an
  attended initrd template up front.
- The write to the target disk is **irreversible** once started: on failure
  after the write begins, the installer wipes the target disk's partition
  signatures and exits without rebooting, rather than attempting a rollback.
  The one exception is the final EFI boot-order update: since the disk is by
  then already fully written, grown, and identity-reset, a failure there is
  reported as an error but does **not** wipe the disk — the deployed system
  is valid and can be booted manually or have its boot entry repaired.

## Troubleshooting

If the installer drops to an interactive shell (`bash -i`), any nonzero
`live-installer` exit reaches this same shell — including a failure during
partition growth, identity reset, or the final boot-order update, all of
which happen after `dd` has already written the target disk. The shell alone
does not tell you whether the destructive boundary was crossed. Check
`/tmp/unattended-installer.log` inside that shell for the specific gate that
failed (manifest load, sha256 mismatch, disk too small, disk not found,
partition growth, identity reset, boot order).

If the log shows the failure happened **before** the write began (manifest
load, sha256 mismatch, disk too small, disk not found), the target disk is
untouched and you can just retry.

If the log shows the failure happened **after** the write began (during
`dd`, partition growth, or identity reset), the target disk's partition
signatures have been wiped (`wipefs -a`) so it cannot present a half-written,
potentially bootable disk. Re-run the install from a clean ISO boot. The one
exception is a boot-order failure: the disk is left fully written and
bootable, not wiped (see [Constraints](#constraints)).

## Related Documentation

- [ADR: Unattended Payload ISO](../../architecture-decision-record/adr-installer-payload-iso.md)
- [`systemConfig.installerPayload` reference](../architecture/image-composer-tool-templates.md#systemconfiginstallerpayload)
- [Security Objectives](../architecture/image-composition-tool-security-objectives.md)
- [Usage Guide](./usage-guide.md)
