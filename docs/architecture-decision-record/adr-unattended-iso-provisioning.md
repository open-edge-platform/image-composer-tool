# ADR: Provisioning Settings for the Unattended ISO Installer

**Status**: Accepted
**Date**: 2026-09-29
**Updated**: N/A
**Authors**: Image Composer Tool Team
**Technical Area**: Provisioning / Live Installer / Templates

---

## Summary

The unattended ISO installer already selects a target disk, partitions it,
installs the OS from packages carried on the ISO, and updates the firmware boot
order. This ADR records how the remaining installation-time and first-boot
settings a base platform image needs are added on that same path: SSH keys,
proxy, boot-time provisioning scripts, cloud-init, root sizing for disks of
unknown size, boot entry ordering, an APT upgrade policy, and a composition
manifest. It implements part of Phase 1 of
[ADR: Declarative Live ISO Installer](./adr-declarative-installer.md) and
notes where it deviates from that proposal.

## Context

A base platform image must install hands-free on hardware whose disks vary in
number and size, and must come up with its users, keys, proxy, and
provisioning in place. The ISO path installs the system on target with the
same `imageos.InstallImageOs` a raw build uses, from `template-dump.yaml` and
the ISO's local package repository. Anything the installed system needs from
the build host must therefore either be a template field or travel on the ISO.

## Decisions

### 1. Apply everything in the shared install step

All new settings are applied in `updateImageConfig`/`updateRootfsConfig`
(`internal/image/imageos`), through a new `internal/image/imageprovision`
package. A raw build and an ISO install produce the same result, and the live
installer needs no new code paths except for boot order.
`imageprovision` writes only through `os.Root`, so no template value can
redirect a write outside the image root.

### 2. Lower host-file inputs to existing primitives at load time

Provisioning scripts, cloud-init files, and SSH key files are host files. After
the template is merged, `finalizeProvisioning` turns scripts and cloud-init
files into `additionalFiles` entries and reads key files into inline keys.
`additionalFiles` already travel on the ISO and are copied during install, so
no new transport exists. The step is idempotent and runs only on the build
host; the live installer loads the dump with `LoadTemplate`, which only makes
paths absolute and never fails on paths that do not exist on target.

### 3. `systemConfig.proxy`, not `systemConfig.network.proxy`

The declarative-installer proposal nests the proxy under `network`. `network`
requires a `backend` whenever the section is present, and the proxy has
nothing to do with which network backend renders interfaces. A top-level
section lets a template set a proxy without choosing a backend.

### 4. cloud-init through a pinned NoCloud seed

Field names follow the proposal (`userDataFile`, `metaDataFile`,
`networkConfigFile`), plus `enabled` and `configFiles`. The seed lives in
`/var/lib/cloud/seed/nocloud/`, the directory cloud-init reads, rather than
`/etc/cloud/seed/nocloud/` as the proposal states. The datasource is pinned to
`[ NoCloud, None ]` so first boot on bare metal does not wait for cloud
metadata services. When meta-data is not supplied, a oneshot unit ordered before
`cloud-init-local` writes it on first boot with a fresh `instance-id`, so every
deployed system gets its own ID even when a raw image is cloned.

### 5. Provisioning scripts as generated systemd units

Each script gets one oneshot unit, ordered after `network-online.target`,
after `cloud-final.service` (ignored by systemd when cloud-init is not
installed, so scripts can rely on what cloud-init applies), and after the
previous script. First-boot scripts use a stamp file written only on
success, so a failed script is retried on the next boot.

The script units are in no target's wants directory. `multi-user.target` is
implicitly ordered after every unit it wants, and cloud-init's
`cloud-final.service` is ordered after `multi-user.target` (cloud-init 26.1 on
Ubuntu 24.04), so a wanted script unit that is also after `cloud-final.service`
is an ordering cycle and systemd deletes its start job. Instead a trigger unit,
`ict-provision-start.service`, is enabled by creating the
`multi-user.target.wants` symlink directly (what `systemctl enable` does,
keeping the step testable without a chroot). It runs
`systemctl start --no-block` on the script units, which then wait for
`cloud-final.service` when cloud-init runs and start at once when it does not,
for example when it is disabled or `ds-identify` finds no datasource. Hooking the
scripts under `cloud-final.service` instead would skip them in those cases.

### 6. End-relative partition offsets instead of a post-install resize

The installer partitions the real target disk, so "grow root to fill the disk"
needs no resize after installation. Root already fills the disk when it is the
last partition with `end: "0"`. When a fixed-size partition follows root, a
negative offset (`end: "-20GiB"`, next partition `start: "-20GiB"`) is measured
back from the end of the disk the installer selected. This works for raw builds
too and needs no first-boot service. The existing
`extendLastPartitionToFillDisk` first-boot service stays raw-only: it exists for
images copied onto a larger disk.

### 7. APT policy: restrict template repositories; OS archives only when immutable

`aptPolicy.upgradeAllowedRepos` lists the template repositories (PPAs) allowed
to upgrade packages. All other template repositories are pinned to priority 50:
their packages can still be installed but never upgrade an installed package.
The distribution archives, including security updates, are **not** restricted.
A deployed system is expected to drift through OS updates unless the template
is immutable. When `systemConfig.immutability.enabled` is true, every archive
and repository is pinned and unattended-upgrades is given no origins.

Repository pins are written by the existing per-repository preferences
generator, so each repository has exactly one pin and one origin. A separate
override file would depend on how apt orders matching pins and on two origin
extractors agreeing. Only the distribution-archive pin for immutable images
lives in its own file, `/etc/apt/preferences.d/00-ict-apt-policy`.

### 8. Explicit boot order

`efibootmgr --create` is not trusted to place the new entry first. Some
firmware does not, and the install medium can still win. The installer sets
`BootOrder` explicitly: `preserve` (default) keeps the existing `BootOrder`
after the new entry, and `exclusive` lists only the new one. Entries outside the
previous `BootOrder` are not added, since the platform left them out on
purpose, and no entry other than an earlier `ICT` one is deleted.
Only entries labelled exactly `ICT` are deleted; the previous substring match
could have removed unrelated entries. They are deleted last, after the new
entry exists and is first in `BootOrder`, so a failure part-way leaves the disk
bootable. The loader path follows the architecture.
The partition number passed to `efibootmgr` is read from sysfs rather than
parsed from the device name. A `/dev/disk/by-*` target path is resolved to the
kernel device in `imagedisc.ResolveInstallDiskPath`, because partition device
names are derived from the disk path.

### 9. SBOM and composition manifest for ISO builds

An ISO build previously retained no SBOM, because the target system's SBOM was
only produced at install time. The build now writes the target SPDX from the
package set resolved at compose time, plus a
`<image>-<version>.composition.json` manifest. The manifest lists base and
template repositories, packages with versions and checksums, kernel,
provisioning inputs, and every additional file with its SHA-256. Repository
and package URLs have credentials, query strings, and fragments removed, since
they may carry tokens; proxy URLs are reduced to hosts. The manifest carries a
`schemaVersion`, so later features such as bundled content packs can add
sections.

Proxy URLs themselves may not contain credentials, a query string, or a fragment: the proxy is written to
world-readable files such as `/etc/environment` and to the template carried on
the ISO, and no file mode protects every copy.

## Consequences

- Nine new template fields, all optional; existing templates behave as before.
  One exception: the generated fstab now lists partitions in template order and
  skips non-swap partitions with no mount point. Previously such a partition
  produced a malformed line.
- `useradd` now honours `shell` and `home` (`home` only for new accounts; an
  existing account keeps its home), and `passwordMaxAge` is applied. A
  crypt(3) hash in `password` is always set as a hash, even without
  `hash_algo`.
- On immutable ubuntu/debian images, the APT policy files are written even
  without an `aptPolicy` section.
- Build commands now inherit the host's `no_proxy` and `ftp_proxy`, not just
  `http_proxy`/`https_proxy`.
- The `proxy`, `provisioning`, `cloudInit`, and `aptPolicy` sections are
  rejected in overlay mode, which does not run the configuration step.
- Additional files are stored on the ISO in a directory derived from their
  in-image destination, under their source basename, so two files with the
  same basename no longer collide. Keeping the basename preserves the
  installer's `cp` semantics for a destination that is an existing directory
  (`final: /etc` yields `/etc/<basename>`, as in a raw build).
- The proxy is written after `configurations[]`, so build-time commands never
  go through the deployment-site proxy.

## Alternatives Considered

- **Write a pre-built raw image to the target from the ISO.** This was rejected
  because the requirement is an unattended installer, not image deployment.
  The per-target disk layout is also created more simply by partitioning the
  selected disk directly.
- **First-boot resize service for ISO installs.** Rejected. The partition
  layout is created on the real disk, so offsets relative to the disk end
  produce the final layout at install time.
- **Delegate users, keys, and proxy entirely to cloud-init.** Rejected as the
  only mechanism. Those settings must also work with cloud-init disabled, and
  must be validated at build time. cloud-init remains available for
  deployment-specific bootstrap.
