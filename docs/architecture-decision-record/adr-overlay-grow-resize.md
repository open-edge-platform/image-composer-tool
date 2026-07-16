# ADR: Grow-Only Resize for Overlay Baselines

**Status**: Proposed
**Date**: 2026-07-14
**Updated**: N/A
**Authors**: Image Composer Tool Team
**Technical Area**: Image Composition / Overlay
**Parent ADR**: [Baseline Image Overlay and ISO Composition Boundaries](adr-image-extension.md)

---

## Summary

Overlay mode installs additional packages into an existing disk-image baseline.
A near-full baseline root filesystem leaves no room for the added packages, so
overlay needs a way to enlarge the image. This note investigates the **grow-only
resize** requirement: the schema shape, the supported filesystem/partition
matrix, the tooling, the validation steps, and the failure modes — and gives a
go/no-go recommendation.

**Scope is deliberately narrow.** Resize is *grow-only* (never shrinks), *in
place* (never repartitions, never relocates the bootloader or ESP), and applies
only to the detected root partition and its filesystem. This preserves the
overlay immutability contract established in the parent ADR ("Avoid structural
mutation of existing images").

A first-cut implementation already exists in
[`internal/image/overlay/resize.go`](../../internal/image/overlay/resize.go),
gated behind an explicit `overlayPolicy.allowDiskResize` opt-in. This note
documents the design that code implements, records the one safety gap that
blocks calling it production-ready on arbitrary baselines (a non-last root
partition) plus a minor LVM-message improvement, and files them as a hardening
implementation story tracked in JIRA.

---

## Context

- Overlay adds packages but does **not** auto-grow the image. Sizing is keyed
  solely on `disk.size` vs. the baseline's current size.
- Without headroom the install step fails with "no space left on device".
- The parent ADR lists "Resize the disk image where supported" and "Grow
  supported filesystems" as in-scope Phase-1 capabilities, and "Shrinking images
  or filesystems" and "Arbitrary repartitioning" as out of scope.
- Growing a partition safely assumes the partition is **last** on the disk with
  free space immediately after it, and that the filesystem sits directly on the
  partition (no intervening LVM/LUKS layer).

---

## Decision Points

### 1. Schema shape — `disk.size` + an explicit opt-in flag

**Options considered**

| Option | Shape | Verdict |
| --- | --- | --- |
| A. Reuse `disk.size` alone | Grow whenever `disk.size` > baseline | **Rejected** — silent structural change; violates immutability contract; surprised users (this was the original behavior and the reason for this note) |
| B. New `overlayPolicy.resize` block | `{ enabled, growFilesystems }` (as sketched in parent ADR) | Rejected for v1 — `growFilesystems` has no meaningful `false` case (a grown partition with an ungrown FS exposes no capacity), so the sub-object is over-modelled |
| C. `disk.size` + `overlayPolicy.allowDiskResize: bool` | Target from `disk.size`; a single boolean opt-in | **Recommended** |

**Recommendation: Option C.** The target size already has a home (`disk.size`)
and a shared unit parser (`imagedisc.TranslateSizeStrToBytes`, accepting
`"6GiB"`, `"8GB"`, …). All that is missing is *consent*. A single boolean keeps
the surface minimal and fits the existing `overlayPolicy` block, whose other
controls (`packageOperation`, `conflictPolicy`) are likewise small, explicit
opt-ins — `allowDiskResize` would be its first boolean gate. (Note: downgrade
and removal are *not* user-facing `overlayPolicy` flags; they are internal
policy fields that the schema deliberately rejects, so they set no precedent
here.) Growing the partition without growing the filesystem is never useful, so
the two are one atomic operation with no separate flag.

```yaml
disk:
  size: 6GiB            # target; grow-only relative to the baseline

overlayPolicy:
  allowDiskResize: true # explicit consent; default false
```

Semantics:
- `disk.size` unset **or** ≤ baseline → no-op (resize never needed; opt-in not required).
- `disk.size` > baseline **and** `allowDiskResize: false` (default) → **hard error** with remediation text.
- `disk.size` > baseline **and** `allowDiskResize: true` → grow.

The schema uses `additionalProperties: false`, so the flag must be declared in
`OverlayPolicy` in both the Go struct and `os-image-template.schema.json`.

### 2. Supported filesystems (v1)

| FS | v1 | Grow tool | Notes |
| --- | --- | --- | --- |
| ext4 | **Yes** | `resize2fs <dev>` | Primary target; Ubuntu cloud images ship ext4 root |
| ext3 / ext2 | **Yes** | `resize2fs <dev>` | Same tool; low marginal cost, matches inspector's accepted set |
| xfs | **Yes (conditional)** | `xfs_growfs <mountpoint>` | Grows online by mount point; keep only if a shipping baseline actually uses xfs, otherwise defer to keep the test matrix small |
| btrfs / f2fs / zfs | No | — | Out of scope v1 |

The set matches `supportedRootFilesystems` in
[`layout.go`](../../internal/image/overlay/layout.go) and the `growFilesystem`
switch in `resize.go`. **xfs recommendation: keep the code path (it is cheap and
already written) but treat it as tier-2** — gate CI coverage on whether an xfs
baseline is in the shipping set; if none is, mark xfs "best-effort, untested" in
docs rather than claiming support.

### 3. Supported partition layouts (v1)

- **Single-partition-grow, last-partition-only.** `growpart` extends a partition
  into free space that immediately follows it. The root partition must therefore
  be the **last** partition on the disk. This is the common cloud-image layout
  (ESP/boot first, root last).
- **GPT and MBR/DOS both supported.** On GPT the backup header is relocated to
  the new end of disk (`sgdisk -e`) before `growpart`; on MBR that step is
  skipped.
- **LVM: out of scope for v1.** A root on an LVM logical volume needs
  `pvresize` + `lvextend` before the FS grow, and detection of the PV/VG/LV
  chain. An LVM member partition currently surfaces as FS type `lvm2_member`
  (`layout.go` lowercases every detected `fstype`), which the unsupported-FS
  check already rejects — so it fails safely, but with a generic message. The
  hardening story should add an **LVM-specific** rejection message.
- **LUKS / encrypted root: already rejected** at layout-build time
  (`analyzeLayout` refuses `crypto_LUKS` before a root is even selected — see
  [`layout.go`](../../internal/image/overlay/layout.go)), consistent with the
  parent ADR excluding mutation of encrypted partitions. No resize-path work
  needed.
- **dm-verity root: already rejected** at layout-build time (`isDMVerity`),
  before resize runs. No resize-path work needed.

### 4. Tooling selection

| Job | Chosen tool | Package | Rejected alternative | Reason |
| --- | --- | --- | --- | --- |
| Grow partition | `growpart` | `cloud-guest-utils` | `parted resizepart`, `sfdisk` | `growpart` is purpose-built for "extend last partition to fill", table-type agnostic, and non-interactive; `parted`/`sfdisk` need explicit sector math and are riskier to script |
| Relocate GPT backup header | `sgdisk -e` | `gdisk` | `parted` | Single-purpose, scriptable |
| Refresh loop capacity | `losetup -c` | `util-linux` | — | Standard |
| Re-read partition table | `partx -u` | `util-linux` | `partprobe` | Works reliably on loop devices |
| Grow ext filesystem | `resize2fs` | `e2fsprogs` | — | Standard |
| Grow xfs filesystem | `xfs_growfs` | `xfsprogs` | — | Standard; online-only, by mount point |

**Availability**: all of the above are already registered in the tool's
`commandMap` (`internal/utils/shell/shell.go`) with host-path fallbacks, so the
allowlisted-exec layer can find them. `growpart`, `resize2fs`, `xfs_growfs`,
`sgdisk`, `losetup`, `partx` must be confirmed present in the build image /
CI environment (hardening-story acceptance item).

### 5. Pre/post validation steps

| Step | When | Tool | v1? |
| --- | --- | --- | --- |
| Reject unsupported FS | before | (map lookup) | Yes — already in `layout.go` |
| Reject non-last root partition | before | partition-table inspection | **Yes — gap, tracked in JIRA** |
| Reject LUKS / dm-verity root | before | layout detection | ✅ already rejected in `analyzeLayout` |
| Reject LVM root | before | layout detection | ⚠️ fails via unsupported-FS (`lvm2_member`); wants LVM-specific message |
| Reject `target ≤ current` / unparseable / > int64 | before | `planResize` | Yes — already implemented |
| Filesystem clean check (`e2fsck -fn` / `xfs_repair -n`) | before | `e2fsck` / `xfs_repair` | **Defer** — see note |
| Re-read table after `growpart` | during | `partx -u` | Yes — implemented |
| Post-grow FS consistency check | after | `e2fsck -fn` | **Defer** |

**fsck note.** `resize2fs` refuses to operate on an unclean ext filesystem and
errors clearly, and `xfs_growfs` operates online on a mounted (therefore
replayed) FS — so an explicit pre-grow fsck is defensive, not strictly required
for correctness. It is **deferred** to keep v1 lean; `e2fsck` is already in
`commandMap` (so a pre-flight `e2fsck -fn` for ext roots is a clean follow-up),
while `xfs_repair` would still need to be added for the xfs path. The baseline
is a fresh user-owned copy, lowering the dirty-FS risk.

### 6. Failure modes and required handling

| Failure mode | Required behavior (v1) | Status |
| --- | --- | --- |
| `disk.size` > baseline but no opt-in | Hard error naming `allowDiskResize`, before touching disk | ✅ implemented |
| Requested size ≤ current | No-op with logged reason | ✅ implemented |
| Requested size unparseable / > int64 max | Hard error | ✅ implemented |
| Root partition is **not** the last partition | Hard error before `growpart`; do not resize | ❌ **gap — tracked in JIRA** |
| LVM logical-volume root | Detect and hard error | ⚠️ fails via unsupported-FS; wants LVM-specific message — tracked in JIRA |
| LUKS / encrypted root | Detect and hard error | ✅ rejected in `analyzeLayout` |
| dm-verity root | Refuse | ✅ rejected in `analyzeLayout` (`isDMVerity`) |
| Unsupported FS type | Hard error | ✅ implemented (`growFilesystem` default case + layout check) |
| Filesystem dirty | Surface `resize2fs`/tool error clearly | ⚠️ relies on tool error; no pre-check |
| `growpart`/`resize2fs`/`sgdisk` missing in env | Fail with tool-not-found | ✅ via `commandMap` verify |
| Any resize command fails mid-sequence | Wrapped error; build fails; artifact not emitted | ✅ implemented |

The **non-last-partition** row is the one finding that makes the current code
genuinely unsafe on general baselines even though it worked on the Ubuntu cloud
image (whose root happens to be last, on a bare partition): a non-last root
would be grown into space that belongs to a following partition. LUKS and
dm-verity are already rejected up front; LVM already fails (as an unsupported
FS) but deserves a clearer message. These are the core of the hardening story.

---

## Recommendation: **GO**

Grow-only resize is well-bounded, the tooling is standard and already wired, the
schema shape is minimal, and a working core already exists behind an explicit
opt-in. The remaining work is *hardening the guards*, not green-field design.

Proceed with **Option C** schema, the ext{2,3,4}+xfs matrix, `growpart`+FS-grow
tooling, and file a **JIRA hardening story** for the guard gaps and validation.

---

## Hardening Implementation Story

**Tracking**: to be filed in JIRA.

**Title**: Harden grow-only overlay resize (guards + validation)

**Goal**: Make the existing grow-only resize safe on arbitrary supported
baselines by rejecting layouts it cannot handle, rather than mis-resizing them.

**Acceptance criteria**
1. `overlayPolicy.allowDiskResize` is documented in schema + templates; a grow
   without it is a clear hard error. *(done — verify only)*
2. Resize **refuses** (clear error, no disk mutation) when the root partition is
   not the last partition on the disk. *(primary safety gap)*
3. An LVM logical-volume root is rejected with an **LVM-specific** message
   (today it fails via the generic unsupported-FS path). LUKS and dm-verity
   roots are already rejected in `analyzeLayout`; add a regression test pinning
   that they never reach the resize path.
4. Confirm `growpart`, `resize2fs`, `xfs_growfs`, `sgdisk`, `losetup`, `partx`
   are present in the build/CI image; add a pre-flight tool-availability check
   or documented dependency.
5. xfs path is either CI-covered by a real xfs baseline or explicitly marked
   best-effort/untested in docs.
6. Unit tests: non-last-partition rejection, LVM/LUKS rejection, GPT vs MBR
   sequence, ext4 and (if in scope) xfs grow, and the existing grow/no-grow/opt-in
   cases. A boot-test of one grown Ubuntu image in CI.
7. *(Optional, may split out)* pre-grow `e2fsck -fn` for ext roots (`e2fsck` is
   already in `commandMap`); add `xfs_repair` to `commandMap` if the xfs path
   gains a pre-grow check.

**Out of scope**: shrinking, non-last-partition repartitioning, LVM/LUKS grow,
FS-type conversion, multi-partition grow.

**Story-point estimate**: **3 points** (≈2 dev-days).
Breakdown: last-partition guard + tests (2), LVM-specific message + regression
tests for the already-rejected LUKS/verity paths (0.5), tooling/CI + boot-test
wiring (0.5). Add **+3 points** if the optional pre/post fsck (AC 7) and an xfs
CI baseline are pulled into the same story rather than split.

If the team instead decides resize is not needed near-term, **the hardening
story stays a backlog placeholder** and the existing code remains behind its
default-off `allowDiskResize` flag (safe: default is no resize).

---

## Risks and Mitigations

| Risk | Impact | Mitigation |
| --- | --- | --- |
| Root not last partition | `growpart` grows into wrong/absent space; corrupt table | Reject non-last root before any mutation (hardening story AC2) |
| LVM root grown as bare partition | Partition grown but LV/FS unchanged, or corruption | Already fails as unsupported FS; add LVM-specific rejection (hardening story AC3) |
| GPT backup header not moved | Table invalid on grown disk | `sgdisk -e` before `growpart` (implemented) |
| Dirty ext filesystem | `resize2fs` aborts | Tool errors clearly; optional pre-`e2fsck` (AC7) |
| Tool missing in build env | Runtime failure late in build | Verify via `commandMap` + pre-flight check (AC4) |
| Silent grow surprises user | Unexpected layout change | Default-off `allowDiskResize`; hard error otherwise (implemented) |
| xfs path unexercised | Undetected regression | CI baseline or mark best-effort (AC5) |

---

## Alternatives Considered

- **`overlayPolicy.resize` sub-object** (parent ADR sketch): rejected for v1 —
  `growFilesystems: false` has no useful semantics; a single boolean is clearer.
- **Auto-size from package footprint**: rejected — non-deterministic, hides real
  disk sizing from the template; resize stays keyed on explicit `disk.size`.
- **`parted`/`sfdisk` instead of `growpart`**: rejected — more sector math, more
  failure surface, for no gain over the purpose-built tool.
- **Always allow grow when `disk.size` larger** (original behavior): rejected —
  silent structural mutation; the reason this note exists.
