# ADR: Kernel replacement (full swap) for overlay baselines

**Status**: Accepted
**Date**: 2026-08-19
**Authors**: Image Composer Tool Team
**Technical Area**: Image Composition / Overlay / Boot
**Parent ADR**: [Kernel command line & GRUB2 regeneration for overlay baselines](adr-overlay-kernel-grub-regen.md)

---

## Summary

Overlay mode could previously only **add** a kernel alongside the baseline's; an
in-place upgrade or removal of an installed kernel image was blocked by preflight
(`ruleKernelImmutable`), and that restriction was recorded as out-of-scope in the
[parent ADR](adr-overlay-kernel-grub-regen.md) (Decision 1).

This ADR records the decision to support a **full kernel swap** behind a new,
opt-in policy field, `overlayPolicy.replaceKernel`. When set, the overlay installs
the named replacement kernel and removes the baseline kernel **family** (bootable
image + meta-package + modules + headers), so the emitted image ships **only** the
new kernel, and regenerates the GRUB config so the boot menu drops the removed
kernel and defaults to the new one. It **supersedes Decision 1** of the parent ADR;
all other decisions there (defaults-file handling, gating on grub2, ordering, tool
selection, failure = no emit, Secure Boot advisory) are unchanged and reused.

## Context

- Requirement: let an operator replace the kernel of a golden image (e.g. swap the
  stock `-generic` kernel for a vendor/OEM/real-time kernel) without rebuilding
  from scratch, and have the resulting image boot the new kernel.
- The bootloader update needed for a kernel swap is a **GRUB config regeneration**,
  not a bootloader reinstall: GRUB resolves the kernel path from `grub.cfg` at
  boot, and the core image on the read-only ESP does not encode a kernel version.
  So `RegenerateGrub` (parent ADR) already provides the mechanism; the ESP and
  `grub-install` stay out of scope.
- On Debian/Ubuntu a kernel is a version-qualified package name, so the *new*
  kernel installs as an `ActionAdd` (already permitted). The work is the *removal*
  of the old kernel, which preflight blocked twice: by `ruleKernelImmutable` (any
  non-add action on a kernel image) and by the `allowPackageRemoval` gate.

## Decision Points

### 1. Opt-in via a dedicated field, not a package-list operation

**Decision:** Add `overlayPolicy.replaceKernel.package` (a single package name).
The replacement is named explicitly rather than being inferred from
`systemConfig.packages`, so the intent is unambiguous and can drive both the
old-kernel removal and the GRUB default pin. `systemConfig.kernel` stays rejected
in overlay mode (it is a create-mode construct).

Rejected: relying on the user to list the new kernel in `systemConfig.packages`
plus a separate remove-list — an explicit removal list is a footgun (a partial
list re-orphans kernel packages and trips the install cascade guard). The
replacement package is injected into the resolved set in `overlayRequestedPackages`.

### 2. Full swap with an auto-detected, complete removal family

**Decision:** Remove every installed baseline package matched by
`isKernelFamilyPackage` (a superset of the bootable-image matcher: `linux-image`,
`linux-modules`, `linux-headers`, the `linux-generic`/`linux-oem`/`linux-lowlatency`
metas; rpm `kernel`, `kernel-core`, `kernel-modules`), minus anything the overlay
is installing (the resolved `ToInstall` set) or the named replacement. Userspace
kernel-adjacent packages (`linux-libc-dev`, `linux-tools-common`, rpm
`kernel-headers`/`kernel-devel` via `kernelSafeExactNames`) are **kept**.

**Why the complete family matters:** removing only the bootable image would orphan
the meta/modules/headers, and the install step's post-removal cascade guard fails
**closed** on a kernel package. Removing the whole family as one batch leaves no
kernel package orphaned, so the guard is never reached and `install.go` needs no
change.

Rejected: a reverse-dependency walk over `BaselinePackage.Dependencies` to grow the
set. The family matcher + `ToInstall` subtraction already covers the meta/module/
header chain; the reverse-dep walk is a possible future belt-and-suspenders.

### 3. Self-authorizing removals, orthogonal to `allowPackageRemoval`

**Decision:** Preflight emits the old-kernel removals as `ActionRemove` marked
`KernelReplacement`; `violatedRule` permits a marked removal past **both**
`ruleKernelImmutable` and the `allowPackageRemoval` gate, and the report records it
in `ToRemove`/`ApprovedRemovals`. `replaceKernel` therefore does **not** require
`allowPackageRemoval` — that flag governs conflict-driven removal of *non-kernel*
baseline packages and stays independent. Immutability is untouched for any build
that does not set `replaceKernel`: a kernel remove/upgrade without the marker still
returns `ruleKernelImmutable`.

`replaceKernel` requires `packageOperation: additive-and-upgrade` (a swap is more
invasive than an in-place upgrade), mirroring the `allowPackageRemoval` constraint.

### 4. GRUB — force regeneration and auto-pin the new kernel

**Decision:** A kernel swap **forces** `RegenerateGrub` (independent of the
`addedKernels` diff), so the removed kernel's menu entry is dropped even in the
edge case where the new kernel's version string coincides with the removed one's.
When the template leaves `grubDefault` unset, `GRUB_DEFAULT` is auto-pinned to
`"0"` — after the baseline kernel is removed the new kernel is the sole (first)
entry. An explicit `grubDefault` always wins. A `replaceKernel` on a non-GRUB2
baseline is a **hard error** (consistent with the parent ADR's cmdline/grubDefault
handling).

This is the one relaxation of the parent ADR's "add-only" decision; everything
else about the GRUB stage (host-side defaults edit, ordering after initramfs,
tool probing, failure = no emit) is reused unchanged.

### 5. Bootloader scope — config only (no ESP writes)

**Decision:** "Update the bootloader" for a kernel swap means regenerate the GRUB
**config** on the writable root. The bootloader binary and the read-only ESP are
never modified, and `grub-install` is never run. A kernel version change does not
require reinstalling GRUB (the binary reads `grub.cfg` at boot), so reinstalling it
would add risk (ESP read-write, Secure Boot re-signing) for no benefit.

Rejected: `grub-install`/ESP rewrite and ESP-staged kernel-copy updates — only
relevant to bootloader-binary changes or non-standard boot layouts, neither caused
by a kernel swap.

### 6. Secure Boot — advisory only (unchanged)

The parent ADR's best-effort warning already fires when a kernel is added on a
Secure Boot baseline with no signing material. A swapped-in kernel is treated the
same: overlay never re-signs (ESP read-only), so the operator signs out of band.

## Recommendation

**GO.** The change reuses the established overlay preflight/report/GRUB machinery,
adds only a narrow install-side guard so a *collateral* non-kernel orphan fails
closed unless `allowPackageRemoval` is set (the complete-family batch means the
kernel family itself is never orphaned), preserves the read-only-ESP contract, and
leaves kernel immutability intact for every build that does not opt in.

## Risks and Mitigations

| Risk | Impact | Mitigation |
| --- | --- | --- |
| Incomplete removal family | An orphaned kernel package trips the install cascade guard and fails the build | Broadened `isKernelFamilyPackage` covers image + meta + modules + headers (deb & rpm), verified against real Ubuntu package names; removed as one batch |
| Kernel swap orphans a collateral non-kernel package | The install cascade could silently remove an unrelated baseline package (e.g. a DKMS module bound to the removed modules/headers) the operator never consented to remove | The cascade of *non-kernel* orphans is gated on `allowPackageRemoval` (`report.CollateralRemovalAuthorized`); a `replaceKernel`-only build fails **closed** on collateral breakage with an actionable error pointing at `allowPackageRemoval` |
| Over-broad matcher removes userspace packages | Breaks DKMS or dev tooling depending on `linux-libc-dev`/`kernel-devel` | Boundary-aware matcher; `kernelSafeExactNames` and the `linux-libc-dev`/`linux-tools-common` exclusions keep userspace packages |
| New kernel version equals removed one | GRUB menu keeps a stale entry | Swap forces regeneration regardless of the `addedKernels` diff |
| New kernel not the boot default | Machine boots the wrong entry | `GRUB_DEFAULT` auto-pinned to `"0"` (or the template's `grubDefault`) |
| Unsigned swapped-in kernel on Secure Boot baseline | Image fails to boot under SB | Best-effort warning; overlay never signs (ESP read-only) — documented |
| `replaceKernel` on a non-GRUB2 baseline | Would silently ship an unbootable swap | Hard error at the GRUB stage |

## Alternatives Considered

- **Keep add-only (parent ADR Decision 1):** rejected — does not satisfy the
  "ship only the new kernel" requirement.
- **Reinstall the bootloader to the ESP (`grub-install`):** rejected — unnecessary
  for a kernel swap and pulls in ESP-read-write + Secure Boot re-signing.
- **Require `allowPackageRemoval` for the swap:** rejected — conflates two
  orthogonal policies; `replaceKernel` self-authorizes its kernel-family removals.
- **User-supplied explicit remove-list:** rejected — a partial list re-orphans
  kernel packages; auto-detection of the complete family is safer.

## Out of scope / follow-ups

- Reverse-dependency walk over `BaselinePackage.Dependencies` as a belt-and-
  suspenders for the removal set (family matcher already covers the known chain).
- rpm-family end-to-end validation of a `kernel` swap under `rpm -e`/`rpm -i`.
- ESP-staged kernel/initrd layouts and separate `/boot` partitions (shared
  limitation with the parent ADR).
- Re-signing the swapped-in kernel for Secure Boot within overlay mode.
