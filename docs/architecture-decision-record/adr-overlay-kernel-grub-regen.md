# ADR: Kernel command line & GRUB2 regeneration for overlay baselines

**Status**: Accepted
**Date**: 2026-07-21
**Authors**: Image Composer Tool Team
**Technical Area**: Image Composition / Overlay / Boot
**Parent ADR**: [Baseline Image Overlay and ISO Composition Boundaries](adr-image-extension.md)

---

## Summary

Overlay mode installs additional packages into an existing disk-image baseline.
When those packages include a **new kernel**, or when the operator supplies a
**kernel command line override** (`overlayPolicy.kernelCmdline`), the bootloader
configuration must be updated so the change takes effect at boot. Until now the
overlay pipeline regenerated only the **initramfs**
([`RegenerateBoot`](../../internal/image/overlay/bootupdate.go)); GRUB config
regeneration and command-line application were explicitly scoped out, and
`overlayPolicy.kernelCmdline` — though present in the schema and struct — was
never read by any code.

This note records the design of the **GRUB2 regeneration** stage
([`RegenerateGrub`](../../internal/image/overlay/grubupdate.go)): it applies
`overlayPolicy.kernelCmdline` (full-line-replacing `GRUB_CMDLINE_LINUX`) and the
optional `overlayPolicy.grubDefault` (full-line-replacing `GRUB_DEFAULT`, to pin
the default boot entry) in `/etc/default/grub`, then regenerates the GRUB config
with the baseline's native tool (`update-grub` / `grub-mkconfig`). It is
deliberately narrow and preserves the overlay immutability contract: it never
touches the bootloader binary or the ESP (mounted read-only), and never runs
`grub-install`.

## Context

- The conventional way to change the kernel command line on a GRUB-based image is
  to full-line-replace `GRUB_CMDLINE_LINUX` (and, to pin the default entry,
  `GRUB_DEFAULT`) in `/etc/default/grub` — e.g.
  `sed -i 's/GRUB_CMDLINE_LINUX=.*/GRUB_CMDLINE_LINUX="<value>"/' /etc/default/grub` —
  and then run `update-grub`. The story's acceptance criterion to check for an
  existing GRUB2 regeneration script to reuse resolved to: **there is no such
  standalone script in this repo to invoke** — the sequence is the inline `sed` +
  `update-grub` above — so overlay reproduces its effect natively rather than
  shelling out to it.
- On Debian/Ubuntu a kernel ships as a **version-qualified package name**
  (`linux-image-6.8.0-50-generic`), so installing a newer kernel is an *addition*,
  not a replacement of an existing package. Overlay preflight already classifies
  that as `ActionAdd` and permits it; it continues to **block** in-place
  replacement of an installed kernel image (`ruleKernelImmutable`) and any
  bootloader-package change (`ruleBootloaderImmutable`).
- The overlay ESP is mounted **read-only** at `<root>/boot/efi`
  ([`layout.go`](../../internal/image/overlay/layout.go)); the GRUB config
  (`/boot/grub/grub.cfg` or `/boot/grub2/grub.cfg`) lives on the **writable
  root**, so regenerating it does not violate the read-only-ESP contract.
- Create mode already regenerates GRUB
  ([`imageboot.go`](../../internal/image/imageboot/imageboot.go)); it is the
  reference for tool selection and command shape, but its cmdline handling reads
  `systemConfig.kernel.cmdline` — a **different** field from the overlay
  `overlayPolicy.kernelCmdline` this stage consumes.

## Decision Points

### 1. Kernel scope — add-only (no policy relaxation)

**Decision:** Keep the existing preflight gate blocking in-place kernel-image
*replacement*. Support only **adding** a new kernel alongside the existing one
(the Ubuntu norm); GRUB regeneration then generates its menu entry.
`update-grub`/`grub-mkconfig` auto-enumerate every installed kernel, so a new
`vmlinuz-<ver>` gets an entry with no version-specific logic.

Rejected: relaxing `ruleKernelImmutable` to allow `rpm -U kernel`-style in-place
replacement. That is a larger, riskier change to preflight policy for a case the
add-only model already covers on the primary (deb) target; it can be a future ADR
if an rpm-family need arises.

### 2. Defaults-file overrides — full-line replace, not merge

**Decision:** The stage rewrites two `/etc/default/grub` keys, each a **complete**
value the stage replaces wholesale (matching the conventional `sed` behavior):

- `overlayPolicy.kernelCmdline` → `GRUB_CMDLINE_LINUX` (never
  `GRUB_CMDLINE_LINUX_DEFAULT`).
- `overlayPolicy.grubDefault` → `GRUB_DEFAULT` (the pinned default boot entry).

Rejected: token-level merge (override-wins per key). The reference sequence is a
full replace, and a merge would silently retain baseline args the operator meant
to drop, diverging from the behavior being replicated.

**Why `GRUB_DEFAULT` is needed:** an overlay can add a *flavored/custom* kernel
(e.g. a real-time or vendor build), possibly with `--allow-downgrades`, so the
added kernel is not necessarily the highest-versioned entry GRUB auto-selects
with `GRUB_DEFAULT=0`. Pinning `GRUB_DEFAULT` to the added kernel's menu entry is
what makes the machine actually boot it; without it the box could still boot the
baseline's stock kernel, defeating the overlay. The exact value is
**supplied by the template**, not inferred: its shape (e.g. the Ubuntu submenu
path `"Advanced options for Ubuntu>Ubuntu, with Linux <ver>"`) depends on
`GRUB_DISTRIBUTOR` and on `GRUB_DISABLE_SUBMENU`, which are too fragile to derive
reliably. `grubDefault` is optional — omit it and a menu entry is still generated
for the added kernel, just not made the default.

**Implementation:** both replaces are done **host-side in Go**
(`replaceGrubAssignment`), not via `sed`, so an arbitrary value (containing `*`,
spaces, `/`, `&`, `=`, or `GRUB_DEFAULT`'s `>`-delimited submenu path) cannot be
mangled by `sed` replacement syntax or a shell. Both edits share a single
read/write pass over the defaults file. The destination is resolved through the
baseline-confined symlink walk (`resolveInRoot`) before the read/write, so a
symlink at `etc/default/grub` (or along the path) that escapes the baseline cannot
redirect the sudo-backed copy onto an arbitrary host file. The root-owned file is
written via `file.Write` (temp-stage + sudo copy), the same mechanism
`seedChrootDNS` uses.
The value is placed verbatim between the assignment's double quotes (not Go-quoted
via `%q`, which would escape tabs/backslashes and write a transformed value).
Config validation ([`OverlayPolicy.validate`](../../internal/config/config.go))
rejects either value containing a double quote, dollar sign, backtick, backslash, or
newline: the defaults file is `.`-sourced by `update-grub`/`grub-mkconfig` as root,
so inside the double-quoted assignment a quote/newline would break it, a `$`/backtick
would be expanded or command-substituted (an injection surface), and a trailing
backslash would escape the closing quote — none of which these fields need. Rejecting
them up front is what makes verbatim writing safe.

### 3. Gating — grub2 baseline AND (override set OR kernel added)

**Decision:** Run the stage only when `info.Bootloader == "grub2"` (covers apt
`boot/grub` and rpm `boot/grub2`) **and** there is work to do: a non-empty
`kernelCmdline` or `grubDefault`, or a kernel version that appeared since the
pre-install baseline scan (`detectKernels` diff against `BaselineInfo.Kernels`).

- **Not** gated on "boot-relevant content changed" (the initramfs stage's gate):
  rebuilding an *existing* kernel's initramfs does not change grub.cfg menu
  entries, which key on kernel version/path — only a **new** kernel warrants a new
  entry. Regenerating otherwise would be needless churn.
- **Non-grub baseline, no override:** clean no-op (systemd-boot/uki manage
  entries via kernel-install hooks).
- **Non-grub baseline, `kernelCmdline` or `grubDefault` set:** **hard error**.
  Honoring the request would require rewriting loader entries this stage does not
  own; shipping an image that silently ignores an explicit operator request is
  worse than failing the build.

### 4. Ordering — after initramfs, cmdline before regen

**Decision:** The GRUB stage runs **after** Boot Regeneration in `Builder.Build()`
(so a new kernel's initrd exists before `grub-mkconfig` enumerates it), and
applies the cmdline **before** invoking the generator (which sources
`/etc/default/grub`). It is a **separate** stage/function from `RegenerateBoot`,
not folded in: the two have different gating policies (GRUB must run for a
cmdline-only change, which the initramfs gate deliberately skips), and separating
them keeps each stage's tests focused.

### 5. Tool selection

Probe PATH inside the chroot, in order, mirroring create-mode `updateGrubConfig`:

| Tool | Command | Family |
| --- | --- | --- |
| `update-grub` | `update-grub` | Debian/Ubuntu wrapper (preferred first) |
| `grub2-mkconfig` | `grub2-mkconfig -o /boot/grub2/grub.cfg` | rpm/dnf |
| `grub-mkconfig` | `grub-mkconfig -o /boot/grub/grub.cfg` | generic GRUB2 |

All three, plus `sed`, are already registered in the shell allowlist
(`internal/utils/shell/shell.go`) — **no allowlist additions**. The generator is
only probed once the stage has decided there is work to do (an override or a new
kernel), so a baseline that ships none of them is a **hard error**, not a skip:
emitting a stale grub.cfg would silently drop the requested boot change. A
present-but-failing generator **is** likewise an error, so a failed regeneration
prevents image emission.

### 6. Failure = no image emitted

A returned error propagates out of `Build()`; `b.built` stays false, and
`Postprocess` skips SBOM + emit and force-removes the workspace copy
([`session.go`](../../internal/image/overlay/session.go)). The mutated
`/etc/default/grub` and any half-written grub.cfg live only in the unemitted,
to-be-deleted workspace copy — never shipped.

### 7. Secure Boot — best-effort advisory only

**Decision:** Emit a **warning** (never an error) when the baseline looks like a
Secure Boot setup (a shim binary on the ESP), the overlay **added a kernel**, and
the template carries no signing material (`IsImmutabilityEnabled()` + secure-boot
key/crt/cer paths). The signal is the added kernel specifically: a regenerated
grub.cfg is not part of the shim signature chain, so it raises no Secure Boot
concern on its own.

Overlay mode does **not** re-sign boot artifacts even when material is present:
the ESP is read-only (shim/grub are immutable) and the regenerated grub.cfg is
not part of the shim signature chain. The real Secure Boot risk is a
locally-added **unsigned kernel**, which firmware will reject; the warning
surfaces that so the operator can sign it out of band.

## Recommendation

**GO.** The change is additive, reuses established overlay seam/mount/skip
conventions, needs no new shell-allowlist entries, and preserves the read-only-ESP
immutability contract. It makes `overlayPolicy.kernelCmdline` — previously dead
config — functional, and gives overlay-added kernels a working boot entry.

## Risks and Mitigations

| Risk | Impact | Mitigation |
| --- | --- | --- |
| Separate `/boot` partition | `MountLayout` mounts only root + ESP(ro); grub.cfg and the kernel scan would target an empty `<root>/boot` | Pre-existing limitation shared with `RegenerateBoot`; common cloud images keep `/boot` on root. Documented; a future story can detect and skip-with-warning or mount `/boot`. |
| Arbitrary cmdline metacharacters | Broken/injected `/etc/default/grub` | Host-side Go replace (no `sed`/shell); value written verbatim between quotes; validation rejects quote, `$`, backtick, backslash, and newline |
| Symlinked `etc/default/grub` escaping the baseline | Sudo-backed copy overwrites an arbitrary host file | Destination resolved through the baseline-confined symlink walk (`resolveInRoot`) before read/write |
| Present-but-failing generator | Half-written grub.cfg | Error propagates → no emit; workspace copy removed |
| Unsigned added kernel on Secure Boot baseline | Image fails to boot under SB | Best-effort warning; overlay never signs (ESP read-only) — documented |
| Added kernel not the default boot entry | Machine boots baseline's stock kernel, not the overlay-added one | `overlayPolicy.grubDefault` pins `GRUB_DEFAULT` (template-supplied exact value; see Decision 2) |
| `grubDefault` value wrong for the baseline's GRUB layout | Pins a non-existent entry; GRUB falls back | Template author owns the exact string (submenu path depends on `GRUB_DISTRIBUTOR`/`GRUB_DISABLE_SUBMENU`); not inferred |
| `/etc/default/grub.d/*.cfg` drop-ins override `GRUB_CMDLINE_LINUX` | Applied cmdline shadowed by a drop-in | Only the base file is edited (matching the conventional flow); documented |

## Alternatives Considered

- **Fold GRUB regen into `RegenerateBoot`**: rejected — different gating (cmdline-
  only change must regenerate GRUB but not the initramfs) would entangle two
  policies and break the initramfs stage's focused tests.
- **Apply the cmdline with `sed`** (verbatim reference sequence): rejected — `sed`
  replacement syntax and shell quoting are hazardous for arbitrary values; a pure
  Go line replace is safer and equivalent in effect.
- **Token-merge the cmdline**: rejected — diverges from the full-replace behavior
  being replicated; risks silently retaining dropped args.
- **Relax preflight to allow in-place kernel replacement**: rejected for now —
  larger policy change; add-only covers the primary case.

## Out of scope / follow-ups

- Auto-deriving the `GRUB_DEFAULT` value from the added kernel (the exact submenu
  path is template-supplied via `overlayPolicy.grubDefault`; inference is too
  fragile — see Decision 2).
- Separate `/boot` partition handling (detect + mount, or skip-with-warning).
- In-place kernel-image replacement (would require relaxing `ruleKernelImmutable`).
- Re-signing boot artifacts for Secure Boot within overlay mode.
- A full real `linux-image-*` end-to-end integration test (kernel package
  postinst + menu entry) — heavier than the current simulated-kernel integration
  test; a CI-gated follow-up.
