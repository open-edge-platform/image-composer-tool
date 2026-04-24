# ADR: Declarative Live ISO Installer

**Status**: Proposed  
**Date**: 2026-04-17  
**Updated**: N/A  
**Authors**: OS Image Composer Team  
**Technical Area**: Provisioning / Live Installer / Security

---

## Summary

This ADR proposes extending the existing Live ISO installer to become a
**declarative provisioning engine** that supports automated disk selection,
Full Disk Encryption (FDE), dm-verity root integrity, SELinux enforcement,
and network configuration - all driven by the image template.

The goal is to eliminate the need for a separate interim OS provisioning
environment by closing the implementation gaps in the current Live ISO
installer, resulting in a single, maintainable provisioning path for all
use cases.

---

## Context

### Problem Statement

The OS Image Composer (ICT) generates fully qualified images with all required
packages and tools, but is not a complete edge node provisioning and
installation solution. Several capabilities required for production edge
deployments are not available through the current Live ISO installer:

1. **Manual disk selection** - The unattended installer requires a hardcoded
   `disk.path` (e.g., `/dev/sda`) in the template. There is no automatic disk
   discovery or policy-based selection.

2. **No Full Disk Encryption** - The boot parameter template contains
   `{{.LuksUUID}}` and `{{.EncryptionBootUUID}}` placeholders, but they are
   replaced with empty strings. No LUKS provisioning logic exists.

3. **No SELinux automation** - The boot parameter template contains a
   `{{.SELinux}}` placeholder, also replaced with an empty string. SELinux
   packages can be installed manually, but there is no automated mode
   configuration, policy selection, or filesystem relabeling.

4. **No declarative network configuration** - Network setup for the installed
   OS is not part of the template schema. Post-install networking relies on
   cloud-init or manual `configurations` commands.

5. **No install manifest separation** - The ISO builder and live installer are
   tightly coupled to the image template. There is no distinct concept of an
   "install manifest" that separates *what to install* from *how to lay out
   the disk*.

### Background

An alternative approach was proposed: introducing an **interim OS** (based on
LinuxKit or a minimal provisioning environment) that would boot first, perform
disk provisioning and security setup, then deploy the target OS. While this
approach is technically viable, it introduces significant concerns:

- **Duplicated provisioning logic** across two environments
- **Increased maintenance burden** for two boot paths
- **Risk of script drift** between the interim OS and the ISO installer
- **Multiple provisioning paths** to test, validate, and support

The architect's position - and the recommendation of this ADR - is that these
capabilities should be implemented directly in the Live ISO installer, which
already has substantial infrastructure in place.

### Existing Infrastructure

The current codebase provides a strong foundation:

| Capability | Status | Location |
|---|---|---|
| Disk enumeration via `lsblk` | Implemented (attended TUI only) | `imagedisc.SystemBlockDevices()` |
| ISO media exclusion | Implemented | `imagedisc.isReadOnlyISO()` |
| GPT/MBR partition creation | Implemented | `imagedisc.DiskPartitionsCreate()` |
| dm-verity (hash partition + root hash) | Implemented | `imageos.prepareVeritySetup()` |
| Overlay filesystem (read-only root) | Implemented | `imagesecure.ConfigImageSecurity()` |
| UKI (Unified Kernel Image) | Implemented | `imageos.buildImageUKI()` |
| Secure Boot signing | Implemented | `imagesign` package |
| Unattended install mode | Implemented | `live-installer unattendedInstall()` |
| Attended install TUI | Implemented | `live-installer texture-ui` |
| Boot parameter template | Implemented (with unused placeholders) | `config/general/image/efi/bootParams.conf` |
| `cryptsetup` in shell allowlist | Present | `shell.commandMap` |
| Cloud-init as optional package | Present | Various templates |

---

## Decision / Recommendation

Extend the Live ISO installer to support all required provisioning
capabilities natively through the existing template schema. Do not
introduce a separate interim OS provisioning environment.

### Core Design Principles

1. **Single provisioning path** - All provisioning flows (BKC, Robotics,
   Edge, etc.) use the same Live ISO installer.

2. **Declarative configuration** - All provisioning behavior is driven by
   the image template YAML. No imperative scripts or manual intervention.

3. **Backward compatibility** - Existing templates continue to work without
   modification. New fields are optional with safe defaults.

4. **Separation of responsibilities** - The installer handles low-level
   hardware provisioning; cloud-init handles high-level OS customization.

5. **Incremental delivery** - Each capability is independently valuable
   and can be shipped without waiting for the others.

---

## Separation of Responsibilities

The target architecture cleanly separates concerns across three layers:

### OS Image Composer (`os-image-composer build`)

Produces installable artifacts:

- Root filesystem payload (packages installed into chroot)
- Kernel, initrd, or Unified Kernel Image (UKI)
- Install manifest (declarative provisioning instructions)

Does **not** produce fixed disk layouts or partition tables.

### Live ISO Installer (`live-installer`)

Declarative provisioning engine responsible for:

- Hardware detection (disk enumeration, interface discovery)
- Disk selection (policy-based or explicit)
- Partition table creation (GPT/MBR)
- Filesystem creation and formatting
- Full Disk Encryption (LUKS2 provisioning)
- dm-verity setup (hash partition, root hash injection)
- SELinux base configuration (mode, policy, relabeling)
- Bootloader installation (GRUB2, systemd-boot, UKI)
- Root filesystem deployment

### Cloud-init (post-first-boot)

Handles customer customization:

- User accounts and SSH keys
- Hostname
- Network overrides
- Package installation
- Service configuration
- Application deployment

---

## Phased Implementation

### Phase 1: Automated Disk Selection

**Problem**: The unattended installer requires `disk.path: /dev/sdX` to be
hardcoded in the template - a value that varies across hardware.

**Solution**: Add a `selectionPolicy` field to `DiskConfig` that allows the
installer to discover and select a disk automatically at install time.

#### Template Schema

```yaml
disk:
  # Explicit path (existing behavior, still supported):
  # path: /dev/sda

  # New: policy-based selection for unattended installs
  selectionPolicy:
    strategy: largest           # largest | first | by-id | by-serial | by-model
    match: ""                   # value for by-id / by-serial / by-model
    excludeRemovable: true      # skip USB/removable media (default: true)

  partitionTableType: gpt
  partitions:
    - id: esp
      # ...
```

When `disk.path` is empty, the installer resolves the disk using the policy.
When `disk.path` is set, the policy is ignored (backward compatible).

#### Approach

- Add a `DiskSelectionPolicy` struct to the config and a new disk selection
  module in `internal/image/imagedisc/`
- Reuse the existing `SystemBlockDevices()` enumeration, extending the
  `lsblk` query to include `SERIAL`, `TRAN` (transport), and `RM` (removable)
  fields
- Implement strategy-based selection: `largest`, `first`, `by-id`,
  `by-serial`, `by-model`
- Filter removable devices and ISO installer media (existing logic)
- When `disk.path` is empty in the live installer, fall back to policy-based
  selection before proceeding to partition creation

---

### Phase 2: Full Disk Encryption (FDE)

**Problem**: Production edge deployments require encrypted root filesystems.
The boot parameter template has `{{.LuksUUID}}` and `{{.EncryptionBootUUID}}`
placeholders but they are never populated.

**Solution**: Add LUKS2 encryption support to the live installer, triggered by
a new `encryption` section in the template.

#### Template Schema

```yaml
systemConfig:
  encryption:
    enabled: true
    type: luks2                 # luks2 (default, only supported type)
    tpmEnroll: true             # enroll TPM2 for auto-unlock (optional)
    recoveryKey: true           # generate recovery key (optional)
    partitions:                 # partition IDs to encrypt
      - root
```

#### Approach

- Add an `EncryptionConfig` struct to `SystemConfig` and a new
  `internal/image/imageencrypt/` package
- Insert encryption into the install flow **after** partition creation but
  **before** rootfs installation:
  1. Format target partitions with LUKS2 via `cryptsetup`
  2. Open the LUKS container and update the disk path map to use
     `/dev/mapper/...` devices
  3. Install the OS into the opened container
  4. Generate `/etc/crypttab` in the installed rootfs
  5. Optionally enroll TPM2 via `systemd-cryptenroll`
  6. Optionally generate a recovery key saved to the ESP
- Populate the existing `{{.LuksUUID}}` boot parameter placeholder with
  `rd.luks.uuid=<uuid>` instead of the current empty string
- Add `systemd-cryptenroll` to the shell command allowlist (`cryptsetup`
  is already present)

#### Security Considerations

- Passphrase for non-TPM scenarios must be provided via template or secure
  input mechanism (never logged)
- Recovery keys are written only to the ESP, not to the root filesystem
- TPM enrollment happens after OS installation is complete

---

### Phase 3: SELinux Enforcement

**Problem**: Edge deployments with security requirements need SELinux in
enforcing mode. The boot parameter template has a `{{.SELinux}}` placeholder
but it is never populated, and there is no automated SELinux configuration.

**Solution**: Add an `selinux` section to the template that configures SELinux
mode, policy type, and filesystem relabeling strategy.

#### Template Schema

```yaml
systemConfig:
  selinux:
    mode: enforcing             # enforcing | permissive | disabled
    policy: targeted            # targeted (default) | mls | minimum
    relabel: first-boot         # first-boot (default) | install-time
```

#### Approach

- Add a `SELinuxConfig` struct to `SystemConfig`
- Add SELinux configuration logic to the OS installation flow
  (post-package-install phase in `imageos`):
  1. Write `/etc/selinux/config` with the desired mode and policy type
  2. If `relabel: first-boot` → create `/.autorelabel` marker
  3. If `relabel: install-time` → run `setfiles` in the chroot
  4. Auto-inject required SELinux packages for the target OS
- Populate the existing `{{.SELinux}}` boot parameter placeholder:
  - `enforcing` → `security=selinux selinux=1 enforcing=1`
  - `permissive` → `security=selinux selinux=1 enforcing=0`
  - `disabled` → empty string (current behavior)
- Add `setfiles` and `restorecon` to the shell command allowlist

---

### Phase 4: Declarative Network Configuration

**Problem**: Network configuration for the installed OS is not part of the
template schema. Users must add it via `configurations` commands or rely
entirely on cloud-init.

**Solution**: Add a `network` section to the template that generates the
appropriate network configuration files for the target OS.

#### Template Schema

```yaml
systemConfig:
  network:
    backend: netplan            # netplan | networkmanager | systemd-networkd
    interfaces:
      - name: eth0
        dhcp4: true
      - name: eth1
        addresses:
          - "192.168.1.10/24"
        gateway4: "192.168.1.1"
        nameservers:
          - "8.8.8.8"
          - "8.8.4.4"
```

#### Approach

- Add `NetworkConfig` and `NetworkInterface` structs to `SystemConfig`
- Create a new `internal/image/imagenetwork/` package that generates the
  appropriate config files based on the selected backend:
  - **netplan** → `/etc/netplan/01-installer-config.yaml`
  - **systemd-networkd** → `/etc/systemd/network/10-<name>.network`
  - **networkmanager** → `/etc/NetworkManager/system-connections/<name>.nmconnection`
- Call network configuration from the OS installation flow after package
  installation

---

### Phase 5: Install Manifest and Architectural Separation

**Problem**: The ISO builder and live installer are tightly coupled to the
full image template. The ISO carries a complete package cache and the installer
replays the entire build. This conflates artifact production with provisioning
logic.

**Solution**: Introduce an **install manifest** - a declarative YAML document
that describes *how* to provision a system using pre-built artifacts. The
ISO carries the manifest alongside a rootfs payload, kernel, and initrd/UKI.

#### Install Manifest Structure

```yaml
version: "1.0"

payloads:
  rootfs: /payloads/rootfs.tar.zst
  kernel: /payloads/vmlinuz
  initrd: /payloads/initrd.img    # or UKI path

diskPolicy:
  strategy: largest
  excludeRemovable: true

partitions:
  - id: esp
    type: esp
    fsType: fat32
    start: 1MiB
    end: 512MiB
    mountPoint: /boot/efi
  - id: root
    type: linux-root-amd64
    fsType: ext4
    start: 512MiB
    end: "0"
    mountPoint: /

security:
  encryption:
    enabled: true
    type: luks2
    tpmEnroll: true
    partitions: [root]
  immutability:
    enabled: true
  selinux:
    mode: enforcing
    policy: targeted
    relabel: first-boot

network:
  backend: netplan
  interfaces:
    - name: eth0
      dhcp4: true

bootloader:
  bootType: efi
  provider: systemd-boot

cloudInit: true
```

#### ISO Builder Changes

The ISO builder (`isomaker`) currently creates an initrd-based rootfs, copies
the package cache, and assembles the ISO. The modified flow:

1. Build rootfs as a compressed tarball (instead of copying raw packages)
2. Generate `install-manifest.yml` from the template
3. Embed both under `/payloads/` and `/manifest/` on the ISO

#### Live Installer Flow (manifest-driven)

```
Boot ISO
  ↓
Read /manifest/install-manifest.yml
  ↓
Discover hardware → select disk via diskPolicy
  ↓
Create partitions per manifest
  ↓
Encrypt partitions if security.encryption.enabled
  ↓
Extract rootfs tarball to mounted partitions
  ↓
Configure SELinux if security.selinux.mode != ""
  ↓
Install bootloader (GRUB2 / systemd-boot / UKI)
  ↓
Apply dm-verity if security.immutability.enabled
  ↓
Write network configuration
  ↓
Reboot → cloud-init handles post-install customization
```

---

## Example Template: Full Declarative ISO

This example demonstrates all new capabilities in a single template:

```yaml
metadata:
  description: Ubuntu 24.04 edge node with FDE, SELinux, and auto disk selection
  use_cases:
    - Secure edge node provisioning
    - Zero-touch bare metal deployment
    - BKC qualified image installation
  keywords:
    - iso
    - fde
    - selinux
    - dm-verity
    - unattended
    - edge

image:
  name: edge-node-ubuntu
  version: "24.04"

target:
  os: ubuntu
  dist: ubuntu24
  arch: x86_64
  imageType: iso

disk:
  selectionPolicy:
    strategy: largest
    excludeRemovable: true
  partitionTableType: gpt
  partitions:
    - id: esp
      name: EFI System Partition
      type: esp
      fsType: fat32
      start: 1MiB
      end: 512MiB
      mountPoint: /boot/efi
      flags: [boot]
    - id: boot
      name: Boot
      type: linux-root-amd64
      fsType: ext4
      start: 512MiB
      end: 1GiB
      mountPoint: /boot
    - id: root
      name: Root
      type: linux-root-amd64
      fsType: ext4
      start: 1GiB
      end: "0"
      mountPoint: /

systemConfig:
  name: edge-node
  description: Secure edge node configuration
  hostname: edge-node

  bootloader:
    bootType: efi
    provider: systemd-boot

  kernel:
    version: "6.8"
    uki: true
    packages:
      - linux-image-generic

  immutability:
    enabled: true

  encryption:
    enabled: true
    type: luks2
    tpmEnroll: true
    recoveryKey: true
    partitions:
      - root

  selinux:
    mode: enforcing
    policy: targeted
    relabel: first-boot

  network:
    backend: netplan
    interfaces:
      - name: eth0
        dhcp4: true

  packages:
    - cloud-init
    - openssh-server
    - policycoreutils
    - selinux-basics
    - selinux-policy-default

  users:
    - name: admin
      sudo: true
      shell: /bin/bash
```

---

## Delivery Milestones

| Milestone | Deliverable | Dependencies | Parallelizable |
|---|---|---|---|
| M1 | Disk auto-selection (Phase 1) | None | Yes |
| M2 | Full Disk Encryption (Phase 2) | M1 (needs resolved disk path) | After M1 |
| M3 | SELinux enforcement (Phase 3) | None | Yes (parallel with M1) |
| M4 | Network configuration (Phase 4) | None | Yes (parallel with M1, M3) |
| M5 | Install manifest v1 (Phase 5) | M1 + M2 + M3 + M4 | Last (integrates all) |

M1, M3, and M4 can be developed **in parallel** by different engineers.
M2 depends on M1 for the disk resolution path. M5 ties together all prior
work into the architectural refactor.

---

## Risks and Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| FDE adds complexity to boot failure debugging | Medium | Recovery key generation; clear error messages; fallback to unencrypted mode |
| SELinux relabeling at install time is slow for large rootfs | Low | Default to `first-boot` relabeling; `install-time` is opt-in |
| Disk auto-selection picks the wrong disk | High | Conservative defaults (`excludeRemovable: true`); support `by-id`/`by-serial` for deterministic selection; attended TUI override |
| Phase 5 manifest refactor is invasive | Medium | Deliver as last milestone; phases 1-4 work with existing template structure |
| TPM2 not available on all target hardware | Low | `tpmEnroll` is optional; LUKS works without TPM (passphrase or recovery key) |

---

## Testing Strategy

Each phase includes dedicated tests:

- **Unit tests**: Table-driven tests for each new function (disk selection
  strategies, encryption config generation, SELinux config writing, network
  config rendering)
- **Integration tests**: Build ISO with new template fields, verify installer
  behavior in QEMU/KVM
- **Security tests**: Verify LUKS UUID appears in boot params, verify
  dm-verity root hash is correct, verify SELinux mode in installed OS
- **Backward compatibility tests**: Existing templates without new fields
  continue to build and install correctly

---

## Alternatives Considered

### Alternative 1: Interim OS (LinuxKit-based)

Boot a minimal provisioning OS, perform disk setup and security configuration,
then deploy the target OS.

**Rejected because**:
- Duplicates provisioning logic between two environments
- Two boot paths to maintain and test
- Risk of script drift
- Higher long-term maintenance cost

---

## References

- Boot parameter template: `config/general/image/efi/bootParams.conf`
- Disk enumeration: `internal/image/imagedisc/imagedisc.go` - `SystemBlockDevices()`
- dm-verity setup: `internal/image/imageos/imageos.go` - `prepareVeritySetup()`
- Immutability/overlay: `internal/image/imagesecure/imagesecure.go` - `ConfigImageSecurity()`
- Live installer: `cmd/live-installer/install.go` - `install()`
- Image template schema: `internal/config/schema/os-image-template.schema.json`
- Shell command allowlist: `internal/utils/shell/shell.go` - `commandMap`
- Security objectives: `docs/architecture/image-composition-tool-security-objectives.md`
