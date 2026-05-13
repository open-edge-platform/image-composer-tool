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

For air-gapped or offline edge deployments where network connectivity cannot
be assumed after installation, cloud-init configuration files (`user-data`,
`meta-data`, `network-config`) can be **injected at build time** via the
template. The composer embeds them into the image as a NoCloud seed
directory (`/etc/cloud/seed/nocloud/`), so cloud-init runs locally on first
boot without requiring a network datasource.

```yaml
systemConfig:
  cloudInit:
    userDataFile: /path/to/user-data
    metaDataFile: /path/to/meta-data         # optional
    networkConfigFile: /path/to/network-config  # optional
```

---

## Phased Implementation

### Phase 1: Unattended Provisioning

Phase 1 delivers a fully functional unattended installer that can provision
an edge node without manual intervention. It covers automated disk selection,
declarative network configuration, dynamic inputs via cloud-init injection,
and comprehensive documentation.

#### 1.1 Automated Disk Selection

**Problem**: The unattended installer requires `disk.path: /dev/sdX` to be
hardcoded in the template - a value that varies across hardware.

**Solution**: Add a `selectionPolicy` field to `DiskConfig` that allows the
installer to discover and select a disk automatically at install time.

##### Template Schema

```yaml
disk:
  # Explicit path (existing behavior, still supported):
  # path: /dev/sda

  # New: policy-based selection for unattended installs
  selectionPolicy:
    strategy: largest           # largest | fastest
    excludeRemovable: true      # skip USB/removable media (default: true)

  partitionTableType: gpt
  partitions:
    - id: esp
      # ...
```

When `disk.path` is empty, the installer resolves the disk using the policy.
When `disk.path` is set, the policy is ignored (backward compatible).

##### Approach

- Add a `DiskSelectionPolicy` struct to the config and a new disk selection
  module in `internal/image/imagedisc/`
- Reuse the existing `SystemBlockDevices()` enumeration, extending the
  `lsblk` query to include `TRAN` (transport), `ROTA` (rotational), and
  `RM` (removable) fields
- Implement strategy-based selection:
  - `largest` — select the disk with the most capacity
  - `fastest` — prefer NVMe over SATA SSD over HDD (based on transport
    type and rotational flag)
- Filter removable devices and ISO installer media (existing logic)
- When `disk.path` is empty in the live installer, fall back to policy-based
  selection before proceeding to partition creation

#### 1.2 Declarative Network Configuration

**Problem**: Network configuration for the installed OS is not part of the
template schema. Users must add it via `configurations` commands or rely
entirely on cloud-init.

**Solution**: Add a `network` section to the template that generates the
appropriate network configuration files for the target OS.

##### Template Schema

```yaml
systemConfig:
  network:
    backend: netplan            # netplan | networkmanager | systemd-networkd

    # NIC selection policy (when interface names vary across hardware)
    nicPolicy: link-up          # all | link-up | by-name (default: all)

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

    proxy:
      httpProxy: "http://proxy.corp.example.com:8080"
      httpsProxy: "http://proxy.corp.example.com:8080"
      noProxy: "localhost,127.0.0.1,.corp.example.com"
      # Future: WPAD/DHCP-based automatic proxy discovery
```

When `nicPolicy` is `all`, every listed interface is configured. When
`link-up`, only interfaces with an active link at install time are
configured (useful when interface names are unpredictable). When `by-name`,
only explicitly named interfaces are configured (same as current behavior).

**WiFi support** is out of scope for the initial implementation and will be
addressed in a future iteration.

##### Approach

- Add `NetworkConfig`, `NetworkInterface`, and `ProxyConfig` structs to
  `SystemConfig`
- Create a new `internal/image/imagenetwork/` package that generates the
  appropriate config files based on the selected backend:
  - **netplan** → `/etc/netplan/01-installer-config.yaml`
  - **systemd-networkd** → `/etc/systemd/network/10-<name>.network`
  - **networkmanager** → `/etc/NetworkManager/system-connections/<name>.nmconnection`
- Implement NIC discovery using `ip link show` to enumerate interfaces and
  filter by link state when `nicPolicy: link-up`
- Generate proxy configuration in `/etc/environment` and backend-specific
  proxy settings
- Call network configuration from the OS installation flow after package
  installation

#### 1.3 Dynamic Inputs (Cloud-init Injection)

**Problem**: Unattended deployments — especially air-gapped or offline edge
nodes — need per-node customization (hostname, users, SSH keys, services)
without requiring network connectivity after installation.

**Solution**: Allow customer-provided cloud-init configuration files to be
injected into the image at build time via the template. The composer embeds
them as a NoCloud seed directory so cloud-init runs locally on first boot.

##### Template Schema

```yaml
systemConfig:
  cloudInit:
    userDataFile: /path/to/user-data
    metaDataFile: /path/to/meta-data             # optional
    networkConfigFile: /path/to/network-config    # optional
```

##### Approach

- Add a `CloudInitConfig` struct to `SystemConfig`
- During image build, copy referenced files into the rootfs at
  `/etc/cloud/seed/nocloud/` (user-data, meta-data, network-config)
- Validate that referenced files exist during template validation
- Cloud-init auto-detects the NoCloud seed on first boot

#### 1.4 Documentation

All Phase 1 features must include corresponding documentation updates:

- **Template specification**: Update `docs/architecture/os-image-composer-templates.md`
  with `selectionPolicy`, `network`, and `cloudInit` schema definitions
- **Usage guide**: Update `docs/tutorial/usage-guide.md` with unattended
  provisioning examples
- **JSON schema**: Update `os-image-template.schema.json` with new fields
- **Example templates**: Add example ISO templates demonstrating unattended
  provisioning with auto disk selection and network config

---

### Phase 2: Security Hardening

Phase 2 adds security capabilities to the provisioning flow: Full Disk
Encryption, SELinux enforcement, and the install manifest architectural
refactor that cleanly separates artifact production from provisioning logic.

#### 2.1 Full Disk Encryption (FDE)

**Problem**: Production edge deployments require encrypted root filesystems.
The boot parameter template has `{{.LuksUUID}}` and `{{.EncryptionBootUUID}}`
placeholders but they are never populated.

**Solution**: Add LUKS2 encryption support to the live installer, triggered by
a new `encryption` section in the template.

##### Template Schema

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

##### Approach

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

##### Unlock Modes

The encryption configuration supports three unlock modes, determined by the
combination of `tpmEnroll` and `recoveryKey` flags:

| Mode | Config | Boot Behavior |
|------|--------|---------------|
| **TPM auto-unlock** | `tpmEnroll: true` | Passphrase sealed in TPM2 PCRs; disk decrypted automatically at boot with no user interaction. This is the primary production mode for unattended edge nodes. |
| **Recovery key** | `recoveryKey: true` | A one-time recovery key is generated and saved to the ESP. Used as fallback if TPM unlock fails (e.g., hardware change, firmware update). Requires manual entry. |
| **Interactive passphrase** | `tpmEnroll: false`, `recoveryKey: false` | User must type a passphrase at the boot prompt. Suitable for development, testing, or systems without TPM2 hardware. |

These modes are **composable** — a production deployment would typically use
`tpmEnroll: true` + `recoveryKey: true` to get automatic unlock with a
recovery fallback. A dev/test environment might use only interactive
passphrase.

##### Security Considerations

- Passphrase for non-TPM scenarios must be provided via template or secure
  input mechanism (never logged)
- Recovery keys are written only to the ESP, not to the root filesystem
- TPM enrollment happens after OS installation is complete
- TPM PCR policy binds the key to specific boot measurements, preventing
  offline disk extraction attacks

#### 2.2 SELinux Enforcement

**Problem**: Edge deployments with security requirements need SELinux in
enforcing mode. The boot parameter template has a `{{.SELinux}}` placeholder
but it is never populated, and there is no automated SELinux configuration.

**Solution**: Add an `selinux` section to the template that configures SELinux
mode, policy type, and filesystem relabeling strategy.

##### Template Schema

```yaml
systemConfig:
  selinux:
    mode: enforcing             # enforcing | permissive | disabled
    policy: targeted            # targeted (default) | mls | minimum
    relabel: first-boot         # first-boot (default) | install-time
    policyFiles:                # optional: customer-provided policy modules
      - /path/to/custom.pp
```

ICT treats `.pp` files as opaque artifacts, copies them into the chroot and installs them via `semodule -i`.
The `.pp` format is the industry-standard SELinux module format used across
all major Linux distributions.

##### Approach

- Add a `SELinuxConfig` struct to `SystemConfig`
- Add SELinux configuration logic to the OS installation flow
  (post-package-install phase in `imageos`):
  1. Write `/etc/selinux/config` with the desired mode and policy type
  2. If `relabel: first-boot` → create `/.autorelabel` marker
  3. If `relabel: install-time` → run `setfiles` in the chroot
  4. Auto-inject required SELinux packages for the target OS
  5. If `policyFiles` are specified, copy each `.pp` file into the chroot
     and install via `semodule -i`
- Populate the existing `{{.SELinux}}` boot parameter placeholder:
  - `enforcing` → `security=selinux selinux=1 enforcing=1`
  - `permissive` → `security=selinux selinux=1 enforcing=0`
  - `disabled` → empty string (current behavior)
- Add `setfiles`, `restorecon`, and `semodule` to the shell command allowlist

#### 2.3 Install Manifest and Architectural Separation

**Problem**: The ISO builder and live installer are tightly coupled to the
full image template. The ISO carries a complete package cache and the installer
replays the entire build. This conflates artifact production with provisioning
logic.

**Solution**: Introduce an **install manifest** - a declarative YAML document
that describes *how* to provision a system using pre-built artifacts. The
ISO carries the manifest alongside a rootfs payload, kernel, and initrd/UKI.

##### Install Manifest Structure

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

cloudInit:
  userDataFile: /payloads/cloud-init/user-data
  metaDataFile: /payloads/cloud-init/meta-data
```

##### ISO Builder Changes

The ISO builder (`isomaker`) currently creates an initrd-based rootfs, copies
the package cache, and assembles the ISO. The modified flow:

1. Build rootfs as a compressed tarball (instead of copying raw packages)
2. Generate `install-manifest.yml` from the template
3. Embed both under `/payloads/` and `/manifest/` on the ISO

##### Live Installer Flow (manifest-driven)

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

  cloudInit:
    userDataFile: /path/to/user-data
    metaDataFile: /path/to/meta-data

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
| M1.1 | Disk auto-selection (Phase 1.1) | None | Yes |
| M1.2 | Network configuration (Phase 1.2) | None | Yes (parallel with M1.1) |
| M1.3 | Cloud-init injection (Phase 1.3) | None | Yes (parallel with M1.1, M1.2) |
| M1.4 | Documentation (Phase 1.4) | M1.1 + M1.2 + M1.3 | After implementation |
| M2.1 | Full Disk Encryption (Phase 2.1) | M1.1 (needs resolved disk path) | After M1.1 |
| M2.2 | SELinux enforcement (Phase 2.2) | None | Yes (parallel with M2.1) |
| M2.3 | Install manifest v1 (Phase 2.3) | M2.1 + M2.2 | Last (integrates all) |

Phase 1 milestones (M1.1, M1.2, M1.3) can be developed **in parallel** by
different engineers. M1.4 (documentation) follows after all Phase 1
implementation is complete. Phase 2 starts after Phase 1 is delivered, with
M2.1 and M2.2 parallelizable.

---

## Risks and Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| FDE adds complexity to boot failure debugging | Medium | Recovery key generation; clear error messages; fallback to unencrypted mode |
| SELinux relabeling at install time is slow for large rootfs | Low | Default to `first-boot` relabeling; `install-time` is opt-in |
| Disk auto-selection picks the wrong disk | High | Conservative defaults (`excludeRemovable: true`); `fastest` strategy uses transport type hierarchy; attended TUI override |
| Install manifest refactor is invasive | Medium | Deliver as last milestone in Phase 2; Phase 1 works with existing template structure |
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
