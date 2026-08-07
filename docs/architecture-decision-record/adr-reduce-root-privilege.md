# ADR: Reduce the Root Privilege Surface of Image Builds

**Status**: Proposed
**Date**: 2026-08-07
**Updated**: N/A
**Authors**: OS Image Composer Team
**Technical Area**: Build Runtime / Security / Portability

---

## Summary

ICT builds run as full root because they perform loop-device attach, host
mounts, and `chroot`. This ADR proposes shrinking that privilege surface in
portable, incremental steps rather than a single rewrite:

1. **Tier 1 (shipped)** — return build-artifact ownership to the invoking
   `sudo` user after a successful build, so root is required only *during* the
   build, not to read or delete its output afterward.
2. **Tier 2 — capability confinement (this ADR's focus).** Run the build with a
   *minimal, explicit set of Linux capabilities* instead of unrestricted root.
   The build mechanism (loop + mount + chroot) is unchanged, so this stays
   portable across every target kernel/distro/arch; only the granted privilege
   shrinks.
3. **Tier 3 — optional backends and further isolation.** A libguestfs/guestfish
   image backend for environments that specifically want *zero host mounts* (and
   have KVM), offered opt-in; and, if desired later, a privileged-helper split
   that confines the dangerous capabilities to a small audited binary.

Genuinely-rootless-everywhere via unprivileged user namespaces was considered
and **rejected as the default** on portability grounds (see Alternatives): the
hardened distros ICT targets (e.g. Ubuntu 24.04) restrict unprivileged user
namespaces by default, so a userns build would fail on exactly the hosts we care
about.

---

## Context

### Problem Statement

ICT is launched as root — the CLI via `sudo -E ict build …`, the server via
`sudo -n ict build …`. It needs privilege for four classes of operation:

| Operation | Capability it classically needs | Where |
| --- | --- | --- |
| `losetup` attach/detach (raw image → loop device) | `CAP_SYS_ADMIN` | `internal/image/imagedisc/loopdev.go` |
| `mount` / bind / sysfs into the chroot | `CAP_SYS_ADMIN` | `internal/utils/mount/mount.go` |
| `chroot` into the rootfs | `CAP_SYS_CHROOT` | `internal/chroot/chrootenv.go`, installers |
| device nodes, ownership of root-owned rootfs files | `CAP_MKNOD`, `CAP_CHOWN`, `CAP_FOWNER`, `CAP_DAC_OVERRIDE` | scattered |

Running as unrestricted root means a bug, a malicious package post-install
script, or a path-traversal in the build can affect the entire host, not just
the build tree. Reducing the granted privilege bounds that blast radius.

### Key architectural facts (verified in the code)

- **Single exec funnel.** Every privileged operation runs as a shell string
  through `shell.ExecCmd*` (`internal/utils/shell/shell.go`). There are ~176
  call sites with `sudo=true`. Nothing uses Go's `syscall.Mount` /
  `syscall.Chroot` directly — it is all `losetup` / `mount` / `chroot` command
  strings. This funnel is the key asset: it is the single place to intercept,
  audit, or redirect privileged work.
- **The dangerous capabilities are concentrated.** Only loop attach
  (`loopdev.go`), mount (`mount.go`), and chroot-enter (the `ExecCmd` calls with
  a non-`HostPath` chrootPath) genuinely need `CAP_SYS_ADMIN` / `CAP_SYS_CHROOT`.
- **The majority of `sudo=true` sites are ordinary file operations** (`rm`,
  `chmod`, `touch`, `mkfs` on an already-attached device, writing into the
  rootfs) that require root only because the *files/devices are root-owned* —
  i.e. they need `CAP_CHOWN` / `CAP_FOWNER` / `CAP_DAC_OVERRIDE`, not
  `CAP_SYS_ADMIN`.

This concentration is what makes capability confinement tractable and portable
rather than a rewrite.

---

## Decision

Adopt the tiered plan above, with **Tier 2 (capability confinement) as the
portable default direction** and Tier 3 backends as opt-in.

### Candidate minimal capability set

To be confirmed empirically by Phase 0:

```
CAP_SYS_ADMIN      # losetup, mount, bind, sysfs, mkswap
CAP_SYS_CHROOT     # entering the chroot
CAP_CHOWN          # own root-owned rootfs files
CAP_FOWNER         # operate on files not owned by the euid
CAP_DAC_OVERRIDE   # read/traverse root-owned trees regardless of perms
CAP_MKNOD          # device nodes in the rootfs (mmdebstrap/rpm may need this)
```

Possible additions the audit may surface: `CAP_SETFCAP` (file capabilities set
inside the rootfs, e.g. on `ping`), `CAP_SETUID`/`CAP_SETGID` (accounts created
in the rootfs), `CAP_MAC_ADMIN`/`CAP_MAC_OVERRIDE` (SELinux/AppArmor labelling).

### Implementation phases

**Phase 0 — capability audit harness (this deliverable).** Provide a way to run
ICT under a dropped capability set (via `capsh`) and run one real Ubuntu 24.04
build. Whatever fails identifies the true minimal set. This is the go/no-go gate
before any code rework. Deliverable: `scripts/cap-audit.sh` + this ADR.

**Phase 1 — ship the minimal-capability invocation as the portable default.**
Document and provide a `setcap` wrapper / systemd unit granting only the
confirmed set. No change to the build mechanism. Verify across the build matrix
(Ubuntu / ELXR / EMT / azl, x86 + ARM).

**Phase 2 (optional) — privileged-helper split.** Route the
`CAP_SYS_ADMIN` / `CAP_SYS_CHROOT` core (loopdev + mount + chroot-enter) through
a small audited helper holding only those caps, while the main process runs with
none. The `shell.ExecCmd` funnel is the interception seam. Security review is the
long pole.

**Tier 3 — libguestfs backend (opt-in, parallel track).** Introduce a backend
interface behind `loopdev.go` + the mount-based assembly path:
`HostLoopBackend` (default, today's code) vs `GuestfishBackend`. Select via
`--image-backend=guestfish` or auto-detect `/dev/kvm`. The guestfish path does
partition / mkfs / populate *inside a VM*, needing zero host loop devices and
zero host mounts — dropping `CAP_SYS_ADMIN` entirely for image assembly. Offered
opt-in only: it needs `/dev/kvm` for acceptable speed and pulls a heavy appliance
dependency, so the capability-confined host path remains the portable default.

---

## Alternatives Considered

### Unprivileged user namespaces (`unshare -r`, `mmdebstrap --mode=unshare`)

The cleanest *rootless* mechanism, but the **least portable**. Ubuntu 24.04 ships
an AppArmor profile restricting unprivileged user namespaces by default; many CI
runners are themselves containers where nested userns is blocked; behaviour
depends on `kernel.unprivileged_userns_clone` and kernel version. A userns build
would work on some hosts and silently fail on others, and would need per-host
privileged configuration to enable — defeating the goal. **Rejected as the
default;** may be offered as a third opt-in backend later for hosts that allow
it.

### Keep unrestricted root (status quo)

Maximally portable and simplest, but leaves the full-host blast radius. Tier 1
already removed the *user-facing* pain (unreadable artifacts); capability
confinement addresses the *security* dimension the status quo ignores.

### Full libguestfs-only (no host-mount path at all)

Uniform behaviour, but the `/dev/kvm` speed cliff and heavy dependency make it a
poor *default* for the portability requirement. Hence backend, not replacement.

---

## Consequences

**Positive**
- Bounded blast radius: a compromised build cannot trivially affect the whole
  host.
- Portable: capabilities exist on every target kernel; no dependency on userns
  being enabled.
- Incremental and low-risk: Phase 1 changes granted privilege, not the build
  mechanism, so the matrix-verified behaviour is unchanged.
- The `shell.ExecCmd` funnel means later isolation (Phase 2) has a clean seam.

**Negative / risks**
- The true minimal capability set is not fully known until Phase 0 runs; some
  operations may pull in more caps than the candidate set.
- `CAP_SYS_ADMIN` is broad — confining to it is a real but partial win; the
  privileged-helper split (Phase 2) or guestfish backend (Tier 3) is needed to
  drop it entirely.
- The libguestfs backend adds a heavy optional dependency and a KVM requirement
  for its fast path.

---

## Verification

- **Phase 0 (DONE, 2026-08-07):** two Ubuntu 24.04 x86 builds ran to
  `image build completed successfully` (exit 0) under the candidate capability
  set — all 30 other capabilities dropped from the bounding set via
  `scripts/cap-audit.sh` — with no `EPERM` / "operation not permitted" failures:
    - **Overlay** build (10m27s): losetup attach/detach, root + ESP mounts, the
      chroot configuration steps, and the Tier-1 ownership restore.
    - **Full-bootstrap raw** build (`image-templates/ubuntu24-x86_64-minimal-raw.yml`,
      11m28s): the from-scratch path — `mmdebstrap` bootstrap into `chrootenv/`
      (Chroot Env Initialization 18s), Image Build 7m53s, mkfs, loop
      attach/detach, image conversion — exercising `CAP_MKNOD` / `CAP_SETUID` /
      `CAP_SETGID` / `CAP_SETFCAP` that the overlay path does not.

  **The 11-cap candidate set is confirmed sufficient on Ubuntu 24.04 x86 for
  both the overlay and full-bootstrap paths.** Still to confirm before Phase 1:
  an ARM target, and the other providers (ELXR/EMT/azl) in the build matrix.
- **Phase 1:** full build matrix (Ubuntu / ELXR / EMT / azl, x86 + ARM) green
  under the minimal-capability invocation.
- **Tier 3:** a guestfish-backed build producing a bootable image with no host
  loop device or host mount held during assembly.
