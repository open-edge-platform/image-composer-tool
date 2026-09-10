# ADR: Building DKMS Kernel Modules During Image Composition

**Status**: Proposed
**Date**: 2026-09-10
**Authors**: ICT Team
**Technical Area**: Package Management / Kernel Modules / Build System

---

## Summary

Add first-class support for building **vendor DKMS kernel-module packages**
against the image's target kernel during composition. The vendor DKMS package
(`.deb`/`.rpm`) is consumed through the existing package-install path; ICT then
provisions the matching **target-kernel headers**, the **DKMS toolchain**, and a
build toolchain into the target rootfs, and drives a **kernel-version-pinned
`dkms build`/`install` against the resolved target kernel**, verifying that each
module actually built. A new typed `systemConfig.dkms` construct is added as a
**first-class alternative** to the `configurations[].cmd` escape hatch for this
workflow — `cmd` remains available for DKMS builds too; `dkms.enabled` gives
kernel-release derivation, ordering, and verification the `cmd` path cannot
guarantee on its own.

Because the module must be compiled against the specific kernel shipped in the
image — and that kernel varies across templates and builds — the module cannot be
a generic pre-built binary; it must be built at compose time. The build toolchain
is **retained in the final image** so the module can be rebuilt against that same
compose-time kernel later (e.g. after `dkms remove`, or if `/var/lib/dkms` state
is lost). Guaranteeing an automatic rebuild after the target installs a *future*
kernel version is out of scope — see *Non-Goals*.

---

## Context

### Problem Statement

Some images must ship an out-of-tree kernel module delivered as a **vendor DKMS
package**. A DKMS module is **kernel-version-specific**: it must be compiled
against the exact kernel that ships in the image. The target kernel version
**varies across templates and builds**, so the module cannot be produced once as
a generic binary — it has to be built during composition, against the resolved
target kernel.

Today the only way to accomplish this is the generic escape hatch
`systemConfig.configurations[].cmd`, e.g. a hand-authored `dkms build`/`dkms
install` (or a bare `dpkg -i` of the DKMS package hoping its post-install trigger
does the right thing). This is problematic:

- **Wrong-kernel hazard.** Inside the build chroot, `uname -r` is the *build
  host's* kernel, not the target's. A naive `dkms autoinstall` or `dpkg -i`
  trigger can build against the wrong kernel (or no kernel), producing an image
  that boots but silently lacks the module.
- **Verification is opt-in, not enforced.** A failing `cmd` does propagate today
  and fail the compose, but nothing requires the author to actually invoke `dkms
  status` and check the result — a script that swallows the build's exit code or
  never checks `dkms status` produces a silently-broken image with no gate
  stopping it.
- **Opaque and unauditable.** The build is an arbitrary `bash -c` string the
  shell allowlist cannot reason about.
- **Manual, error-prone provisioning.** The correct `linux-headers-<version>` /
  `kernel-devel`, `dkms`, and toolchain must be hand-added and hand-ordered.

`systemConfig.dkms` does not retire `configurations[].cmd` — that escape hatch
remains available, including for DKMS builds, for cases this typed construct
doesn't cover. It gives templates that need the guarantees above (correct
kernel targeting, ordering, enforced verification) a typed path that doesn't
require hand-authoring them.

### Current System

- **Vendor packages are consumed through the existing package path.** A vendor
  DKMS `.deb`/`.rpm` is staged via `packageRepositories[].packages[]` and
  installed by naming it in `systemConfig.packages[]`; installation is performed
  by apt/dnf against a local file repository (`internal/chroot/deb`,
  `internal/chroot/rpm`, `internal/image/imageos`).
- **Kernel *package* selection is already resolved** during the build
  (`debutils.ConfigureKernelSelection`, `systemConfig.kernel`), but only as a
  package-version filter used to pick which kernel package(s) to install
  (`matchesKernelVersion` matches against the package `Version` field, not a
  `uname -r`-style release). On Debian/Ubuntu the resolved kernel package name
  is persisted (`ResolvedKernelPackages`); no RPM-based provider persists an
  equivalent selection today. Nothing today derives the actual kernel **release**
  string (`6.8.0-49-generic`) that `dkms -k` requires — this ADR must add that.
- **Post-install commands run in the target rootfs chroot** via `addImageConfigs`
  (the `configurations[].cmd` escape hatch), strictly after the full dependency-
  ordered package install list has run.
- **Package install order is dependency-driven, not stageable.** `installImagePkgs`
  installs `pkgsorter`'s dependency-sorted list one package at a time; there is no
  way today to force "headers + dkms + toolchain" ahead of a vendor DKMS package
  in that list unless they are its declared `Depends`.
- There is **no first-class DKMS handling**: no automatic header/toolchain
  provisioning, no kernel-release derivation, no kernel-pinned build, and no
  enforced build-success verification.

### Key Design Constraints

- **Build against the exact target kernel, which varies per build.** The module
  must be compiled for the kernel shipped in the image; a generic pre-built
  binary is not possible.
- **`uname -r` is unusable in the chroot.** The build must pass the **explicitly
  resolved** target kernel release (`dkms ... -k <target-release>`), never rely on
  the running kernel.
- **The target kernel release must be derived, not assumed.** Nothing in ICT
  today exposes a `uname -r`-style release string; it must be derived from the
  resolved kernel package (name + version) as part of this design, for both deb
  and rpm.
- **Correct inputs must be present and installed before the vendor package's
  post-install runs.** The matching kernel headers, `dkms`, and a compiler/make
  toolchain must land **before** the vendor DKMS package installs, since its
  post-install script may itself invoke `dkms autoinstall`/`dkms_autoinstaller`
  against whatever (wrong) kernel context is available; ICT derives the correct
  headers package from the resolved kernel rather than requiring the user to
  name it.
- **Builds must be verified per module, explicitly.** Each DKMS module must be
  built/installed with an explicit `-m <module> -v <version>` (or discovered and
  iterated one at a time) — DKMS has no bare "build everything" form — and a
  failure of any module must fail the compose, not ship a silently-broken image.
- **Secure Boot module signing is a separate, currently-unsolved problem.**
  `dkms status` proves a module built and installed; it proves nothing about
  whether the target's Secure Boot policy will load it. ICT has no module-signing
  infrastructure today (`imagesign` only signs the UKI/bootloader), so `dkms` and
  Secure Boot enrollment must not be silently combined (see *Non-Goals*).
- **Toolchain is retained in the image.** To allow the module to rebuild against
  the *same* compose-time kernel later, the DKMS toolchain and its matching
  kernel headers remain in the shipped image. This is a deliberate trade-off
  (see *Retained Toolchain and Security*) and does **not** by itself cover
  rebuilding against a future kernel update (see *Non-Goals*).

---

## Decision

Add an optional typed `systemConfig.dkms` construct. The vendor DKMS package
continues to be consumed through the existing package path; when `dkms.enabled`
is set, ICT additionally:

1. **Resolves the target kernel package** from the existing kernel selection, and
   **derives the kernel release string** (`6.8.0-49-generic`) `dkms -k` needs from
   that package's name/version — a new capability; nothing today exposes this.
2. **Installs the matching kernel headers, `dkms`, and the build toolchain**
   (`gcc`, `make`, …) **in a dedicated pass before** the vendor DKMS package, so
   they are present if the vendor package's own post-install script attempts a
   `dkms autoinstall`. Any such automatic post-install DKMS trigger is suppressed
   (the same diversion technique the code already uses for
   `dracut`/`update-initramfs` triggers during install), so only ICT's explicit
   step below builds the module.
3. **Builds and installs each DKMS module explicitly, one at a time,** against
   the resolved target kernel release in the target rootfs chroot: for every
   `name/version` (from `modules`, or discovered under `/usr/src` when omitted),
   `dkms build -m <name> -v <version> -k <target-release>` then `dkms install
   -m <name> -v <version> -k <target-release>` (never relying on `uname -r`).
4. **Verifies** via `dkms status` that each expected module is `installed` for the
   target kernel release, and **fails the compose** otherwise.
5. **Rejects the combination of `dkms.enabled` and Secure Boot enrollment** at
   validation time, since ICT has no mechanism to sign DKMS-built modules for the
   target's Secure Boot policy today (see *Non-Goals*).
6. **Retains** the headers and toolchain in the image so the module can be
   rebuilt against that *same* compose-time kernel later; this does not by itself
   make the module rebuildable after a *future* kernel update (see *Non-Goals*).

### Template Example

```yaml
packageRepositories:
  - codename: noble
    url: https://vendor.example/apt
    component: main
    pkey: /keys/vendor.gpg
    packages:
      - https://vendor.example/acme-driver-dkms_1.2.3_all.deb   # vendor DKMS package

systemConfig:
  packages:
    - acme-driver-dkms          # install the vendor DKMS (source) package
  kernel:
    version: "6.8.0-49.49"      # kernel PACKAGE version (matches apt/dnf metadata);
                                 # ICT derives the dkms -k release string from the
                                 # resolved kernel package, not from this value directly
  dkms:
    enabled: true               # provision headers+toolchain, build against target kernel, verify
    modules:                    # OPTIONAL assertion list (name/version)
      - acme-driver/1.2.3
```

The user names the vendor DKMS package to install, selects the target kernel
*package* via `systemConfig.kernel.version` (unchanged, existing field/semantics),
and enables `dkms`. Kernel headers, `dkms`, the toolchain, kernel-release
derivation, the kernel-pinned build, and verification are all handled
automatically. `modules` is an optional safety net: when present, ICT asserts
each entry is built for the target kernel and fails the compose if not; when
omitted, ICT discovers and builds/verifies every DKMS module registered by the
installed packages.

### Build & Verify Flow

```mermaid
flowchart TD
    A[Resolve target kernel package\n+ derive dkms -k release string] --> B[Install pass 1:\nlinux-headers-<ver> / kernel-devel + dkms + toolchain]
    B --> C[Install pass 2:\nvendor DKMS package\n(auto-trigger suppressed)]
    C --> D[Post-install DKMS step per module:\ndkms build/install -m <name> -v <ver> -k <release>]
    D --> E{dkms status: module\ninstalled for <release>?}
    E -- Yes --> F[Module present in image;\nheaders + toolchain retained for\nsame-kernel rebuilds]
    E -- No --> G[Fail the compose\nwith a clear error]

    style F fill:#4a4,color:#fff
    style G fill:#f44,color:#fff
```

### Changes Required

| Component | Change |
|-----------|--------|
| `ImageTemplate` / `SystemConfig` struct | Add `Dkms` (`Enabled bool`, `Modules []string`) |
| JSON schema | Add `systemConfig.dkms` object; `enabled` boolean, `modules` array of `name/version` strings |
| `validate` command | Validate `modules` format; require a resolvable target kernel when `enabled`; reject `dkms.enabled` + Secure Boot enrollment |
| Kernel-release derivation (new) | For deb, derive the `dkms -k` release from `ResolvedKernelPackages`' name/version; for rpm, first add persistence of the selected kernel package (currently missing), then derive the release |
| RPM kernel-package persistence (new) | No `rcd`/`azl`/`emt` provider persists a selected kernel package today; add the equivalent of deb's `ResolvedKernelPackages` |
| Staged install ordering (new) | Install matching kernel headers + `dkms` + toolchain in a pass **before** the vendor DKMS package; suppress the vendor package's automatic `dkms autoinstall` post-install trigger (reusing the existing dracut/update-initramfs diversion pattern) |
| Post-install DKMS step (new) | In the target rootfs chroot, for each module run kernel-pinned `dkms build -m <name> -v <version> -k <release>` / `dkms install -m <name> -v <version> -k <release>`, then verify via `dkms status` |
| Shell allowlist | Add `dkms`, with justification |
| Provider wiring | Invoke the DKMS step after `installImagePkgs`, before finalize; deb vs rpm behind `OsName` |
| Overlay wiring (new) | Overlay composition (`internal/image/overlay`) is a separate pipeline from create-mode `installImagePkgs`/`addImageConfigs`; wire the same DKMS step into the overlay builder. In scope: `ubuntu24-x86_64-robotics-hw-overlay-qcow2.yml` already ships `librealsense2-dkms` |
| Tests | Build a trivial DKMS module against a target kernel; verify success; assert failure path fails the compose; wrong-kernel guard; overlay build coverage |
| Documentation | Template docs, security objectives (retained toolchain, Secure Boot restriction), release notes |

### Validation Rules

- **Resolvable target kernel required** — when `dkms.enabled` is set, the target
  kernel package must be resolvable and its release derivable; otherwise the
  compose fails, since the matching headers cannot be selected.
- **Headers must be obtainable** — the matching `linux-headers-<version>` /
  `kernel-devel` must be resolvable from the configured repositories; a missing
  header package fails the compose with a clear error.
- **Secure Boot + DKMS is rejected** — `dkms.enabled` together with Secure Boot
  enrollment fails validation, since ICT cannot sign the built module for the
  target's Secure Boot policy today (see *Non-Goals*).
- **Kernel-pinned, module-scoped build only** — every `dkms build`/`install` call
  includes an explicit `-m <module> -v <version> -k <target-release>`; the
  running-kernel (`uname -r`) path and bare selector-less `dkms build` are never
  used.
- **Verification gate** — after the build, `dkms status` must show each expected
  module `installed` for the target kernel release. If `modules` is specified,
  each listed `name/version` is asserted; otherwise every module discovered under
  `/usr/src` is asserted. Any unbuilt module fails the compose.
- **Deterministic failure** — a build error or a failed verification fails the
  compose; no silently-broken image is produced.

### Retained Toolchain and Security

By design, the DKMS toolchain (`dkms`, `gcc`, `make`) and the kernel headers are
**left in the shipped image**. This enables the module to rebuild against the
*same* compose-time kernel later (e.g. after `dkms remove`, or if DKMS state is
lost) — the primary reason to use DKMS rather than a fixed pre-built module.
It does **not** by itself guarantee an automatic rebuild after the target
installs a *future* kernel: the retained `linux-headers-<version>` package is
pinned to the compose-time kernel's exact ABI and will not match a later kernel.
Keeping a future kernel rebuildable requires the target's own package management
to bring in matching headers when it installs that kernel (e.g. a tracking
metapackage) — outside what ICT controls at compose time (see *Non-Goals*).

This is a deliberate trade-off and must be documented for operators:

- **Larger image and wider attack surface.** A compiler, `make`, and kernel
  headers in a production image increase size and expand what an attacker can use
  on-device.
- **More scanner findings.** Trivy (a merge gate) will surface additional CVEs
  from `gcc`/`binutils`/headers.
- **Scope it deliberately.** Retaining the toolchain is a consequence of enabling
  `dkms`, applied to images that genuinely need on-target rebuilds — it must not
  be applied globally to minimal images that do not.

---

## Consequences

### Benefits

- **Correct-by-construction kernel targeting.** The module is always built against
  the resolved target kernel via an explicit `-k`, eliminating the wrong-kernel
  hazard of `uname -r`/`autoinstall` in a chroot.
- **CI-gated.** A build or verification failure fails the compose, not the device
  in the field.
- **First-class, typed alternative.** Gives templates a validated construct
  instead of hand-authoring `dkms build`/`install`/`status` in a `cmd`;
  `configurations[].cmd` remains available for cases this construct doesn't cover.
- **Zero manual provisioning.** Headers, `dkms`, and the toolchain are derived and
  installed automatically from the resolved kernel.
- **Same-kernel rebuilds preserved.** The retained toolchain lets the module
  rebuild against the compose-time kernel later, as DKMS intends; rebuilding
  after a *future* target kernel update is explicitly out of scope.
- **Reuses existing infrastructure.** The vendor DKMS package flows through the
  existing package-install path unchanged.

### Risks and Mitigations

| Risk | Mitigation |
|------|-----------|
| Build in chroot targets the wrong kernel | Always build with explicit `-k <resolved-target-release>`; never use `uname -r` |
| Vendor package's post-install script auto-invokes DKMS before headers/toolchain are ready | Staged install: headers + `dkms` + toolchain land first; the vendor package's auto-trigger is suppressed |
| Module silently fails to build | `dkms status` verification gate fails the compose |
| Matching kernel headers unavailable in repos | Clear build-time failure; user adds a repo providing the headers |
| DKMS-built module rejected by Secure Boot policy at boot | `dkms.enabled` + Secure Boot enrollment is rejected at validation until module signing is supported |
| Retained toolchain enlarges image / widens attack surface / raises Trivy findings | Documented, deliberate trade-off scoped to `dkms`-enabled images; not applied to minimal images |
| Kernel and module-source versions drift | Kernel is resolved from selection; `modules` assertion pins expected `name/version` |
| New `dkms` command widens the shell allowlist | Add only `dkms`, with justification; run inside the sanctioned shell layer rather than opaque `bash -c` |

### Alternatives Considered

- **Build the module on the target at first boot** — Rejected. Non-deterministic
  and un-gated; needs a toolchain and network at boot (air-gapped/constrained
  targets fail); the module is missing until first boot; failures surface on the
  device, not in CI.
- **Ship a pre-built binary kernel-module package (kmod/akmod) built per kernel**
  — Rejected. The target kernel varies across templates and builds, so a generic
  binary cannot be pre-built; and it would couple a separate per-kernel build
  pipeline to every kernel bump.
- **Rely on the DKMS package's post-install `dkms autoinstall` alone** — Rejected
  as insufficient. In a chroot the target kernel is not running; `autoinstall`
  kernel detection is unreliable there. ICT must drive an explicit kernel-pinned
  build and verify it.
- **Rely solely on `systemConfig.configurations[].cmd`** — Rejected as the
  *only* mechanism: it's opaque, gives no built-in kernel-release derivation,
  install ordering, or enforced verification. It remains supported alongside
  `systemConfig.dkms`, not retired, for cases the typed construct doesn't cover.
- **Strip the toolchain after building** — Rejected for this requirement.
  Retaining it is a deliberate choice to allow same-kernel on-target rebuilds;
  the size/security cost is accepted and documented.

---

## Non-Goals

- Building kernel modules on the target device at runtime / first boot — this ADR
  builds at compose time.
- Producing pre-built binary kernel-module packages.
- Stripping the DKMS toolchain from the image — it is intentionally retained.
- Building against a kernel that is not the one shipped in the image.
- Retiring or deprecating `configurations[].cmd` — this ADR adds a typed
  alternative for DKMS builds; `cmd` remains fully supported, including for
  DKMS, for cases the typed construct doesn't cover.
- **Signing DKMS-built modules for Secure Boot / MOK enrollment.** ICT has no
  module-signing infrastructure today; `dkms.enabled` is rejected at validation
  when combined with Secure Boot enrollment until that capability exists.
- **Guaranteeing automatic module rebuilds after a *future* target kernel
  update.** This ADR provisions matching headers/toolchain only for the
  compose-time kernel; keeping a later kernel rebuildable is the target's
  package-management responsibility, not something compose-time can ensure.
