# ICT Web Interface - UI Design & Wireframes

**Status**: Proposed  
**Date**: 2026-06-24  
**Related**: [ADR: Web Interface Architecture](adr-web-interface-architecture.md)

---

## Design Philosophy

The UI follows a **wizard + canvas** pattern: a guided step-by-step flow for beginners, with a visual canvas for power users who want to drag-and-drop building blocks. Both converge to the same validated YAML output.

**Key UX Principles:**
1. Progressive disclosure - show simple options first, advanced settings behind expandable panels
2. Instant feedback - validate as you configure, not after
3. Visual building blocks - each template section is a discrete, draggable card
4. Dual mode - wizard (guided) or canvas (free-form), switchable at any time

---

## Application Layout

```
┌─────────────────────────────────────────────────────────────────────────┐
│  ┌─────┐  ICT Web                    [Validate] [Build] [Export YAML]   │
│  │ ICT │  ─────────────────────────────────────────────────────────────  │
│  └─────┘  [Templates] [Builder] [Chat] [Builds]         user@org ▼     │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                         │
│                        (Active View Content)                            │
│                                                                         │
└─────────────────────────────────────────────────────────────────────────┘
```

**Navigation:**
- **Templates** - Browse/search the template library (54 templates)
- **Builder** - Visual template composer (this document's focus)
- **Chat** - AI conversational template generation
- **Builds** - Build dashboard with history and logs

---

## Builder View - Two Modes

### Mode Toggle (top-left of builder)

```
┌──────────────────────────────┐
│  [■ Wizard] [ □ Canvas ]     │
└──────────────────────────────┘
```

---

## Mode 1: Wizard (Guided Step-by-Step)

Best for: new users, straightforward images, learning the building blocks.

```
┌─────────────────────────────────────────────────────────────────────────┐
│                                                                         │
│  Step Progress:                                                         │
│  ● Target ─── ● Disk ─── ○ Packages ─── ○ Kernel ─── ○ Network ───    │
│  ○ Users ─── ○ Security ─── ○ Scripts ─── ○ Review                     │
│                                                                         │
├───────────────────────────────────────────┬─────────────────────────────┤
│                                           │                             │
│  STEP 1: Target Platform                  │  Live Preview (YAML)        │
│  ─────────────────────────                │                             │
│                                           │  ```yaml                    │
│  Operating System:                        │  image:                     │
│  ┌─────────────────────────────────┐      │    name: my-image           │
│  │ ◉ Azure Linux                   │      │    version: "1.0.0"         │
│  │ ○ Edge Microvisor Toolkit       │      │  target:                    │
│  │ ○ Wind River eLxr               │      │    os: azure-linux          │
│  │ ○ Ubuntu                        │      │    dist: azl3               │
│  │ ○ Red Hat Compatible Distro     │      │    arch: x86_64             │
│  │ ○ Debian                        │      │    imageType: raw           │
│  └─────────────────────────────────┘      │  ```                        │
│                                           │                             │
│  Distribution:                            │  ─────────────────────────  │
│  ┌───────────────────┐                    │  Validation: ✓ Valid        │
│  │ azl3           ▼  │                    │                             │
│  └───────────────────┘                    │                             │
│  (auto-filtered by OS selection)          │                             │
│                                           │                             │
│  Architecture:                            │                             │
│  [■ x86_64] [□ aarch64] [□ armv7hl]      │                             │
│                                           │                             │
│  Image Type:                              │                             │
│  ┌────────┐ ┌────────┐ ┌────────┐        │                             │
│  │  RAW   │ │  ISO   │ │  IMG   │        │                             │
│  │  ════  │ │        │ │        │        │                             │
│  │ Disk   │ │ Boot   │ │Initrd  │        │                             │
│  │ image  │ │  media │ │  only  │        │                             │
│  └────────┘ └────────┘ └────────┘        │                             │
│                                           │                             │
│  Image Name: [my-image____________]       │                             │
│  Version:    [1.0.0_______________]       │                             │
│                                           │                             │
│         [← Back]              [Next →]    │                             │
│                                           │                             │
└───────────────────────────────────────────┴─────────────────────────────┘
```

### Step 2: Disk Configuration

```
┌───────────────────────────────────────────┬─────────────────────────────┐
│                                           │                             │
│  STEP 2: Disk Layout                      │  Live Preview (YAML)        │
│  ───────────────────                      │                             │
│                                           │  disk:                      │
│  [✓] Use default disk layout              │    name: default            │
│      (Recommended for most use cases)     │    size: "4GiB"             │
│                                           │    partitionTableType: gpt  │
│  ─── OR customize: ───────────────────    │    partitions:              │
│                                           │      - id: boot             │
│  Disk Size: [4    ] [GiB ▼]              │        ...                  │
│  Partition Table: [◉ GPT] [○ MBR]         │                             │
│                                           │                             │
│  Partitions:                              │  ─────────────────────────  │
│  ┌─────────────────────────────────────┐  │                             │
│  │ ┌─────────┐ ┌──────────────────┐   │  │  Visual Disk Map:           │
│  │ │  boot   │ │     rootfs       │   │  │                             │
│  │ │ 512MiB  │ │    (rest)        │   │  │  ┌────┬──────────────────┐  │
│  │ │ vfat    │ │    ext4          │   │  │  │boot│     rootfs       │  │
│  │ │ /boot   │ │    /             │   │  │  │512M│     3.5G         │  │
│  │ └─────────┘ └──────────────────┘   │  │  └────┴──────────────────┘  │
│  └─────────────────────────────────────┘  │   4 GiB total               │
│                                           │                             │
│  [+ Add Partition]                        │                             │
│                                           │                             │
│  Output Artifacts:                        │                             │
│  [✓] raw    [□] qcow2   [□] vhd          │                             │
│  [□] vhdx   [□] vmdk    [□] vdi          │                             │
│                                           │                             │
│  Compression: [None ▼]                    │                             │
│                                           │                             │
│         [← Back]              [Next →]    │                             │
│                                           │                             │
└───────────────────────────────────────────┴─────────────────────────────┘
```

### Step 3: Packages

```
┌───────────────────────────────────────────┬─────────────────────────────┐
│                                           │                             │
│  STEP 3: Packages                         │  Live Preview (YAML)        │
│  ────────────────                         │                             │
│                                           │  systemConfig:              │
│  Search: [docker________________] 🔍      │    packages:                │
│                                           │      - cloud-init           │
│  Popular Package Groups:                  │      - rsyslog              │
│  ┌─────────────────────────────────────┐  │      - docker-ce           │
│  │ [✓] Container Runtime               │  │      - docker-ce-cli       │
│  │     docker-ce, docker-ce-cli,       │  │      - containerd.io       │
│  │     containerd.io                   │  │      - nginx               │
│  │                                     │  │                             │
│  │ [□] Kubernetes                      │  │                             │
│  │     kubelet, kubeadm, kubectl       │  │  ─────────────────────────  │
│  │                                     │  │                             │
│  │ [□] Monitoring                      │  │  Package Count: 6           │
│  │     prometheus-node-exporter,       │  │  Estimated Size: ~340 MB    │
│  │     collectd                        │  │                             │
│  │                                     │  │                             │
│  │ [✓] Web Server                      │  │                             │
│  │     nginx                           │  │                             │
│  │                                     │  │                             │
│  │ [□] Development Tools               │  │                             │
│  │     git, make, gcc                  │  │                             │
│  │                                     │  │                             │
│  │ [□] Networking                      │  │                             │
│  │     iptables, nftables, tcpdump     │  │                             │
│  └─────────────────────────────────────┘  │                             │
│                                           │                             │
│  Individual Packages (add manually):      │                             │
│  ┌─────────────────────────────────────┐  │                             │
│  │ cloud-init  [×]                     │  │                             │
│  │ rsyslog     [×]                     │  │                             │
│  │ [+ add package________________]     │  │                             │
│  └─────────────────────────────────────┘  │                             │
│                                           │                             │
│         [← Back]              [Next →]    │                             │
│                                           │                             │
└───────────────────────────────────────────┴─────────────────────────────┘
```

### Step 4: Kernel

```
┌───────────────────────────────────────────┬─────────────────────────────┐
│                                           │                             │
│  STEP 4: Kernel Configuration             │  Live Preview (YAML)        │
│  ────────────────────────────             │                             │
│                                           │  kernel:                    │
│  Kernel Version: [6.6_____________]       │    version: "6.6"           │
│                                           │    cmdline: "console=..."   │
│  Command Line Parameters:                 │                             │
│  [console=ttyS0,115200 console=tty0   ]   │                             │
│  [loglevel=7_________________________]    │                             │
│                                           │                             │
│  Quick-add common parameters:             │                             │
│  [+ console=ttyS0] [+ quiet] [+ debug]   │                             │
│  [+ intel_iommu=on] [+ hugepages=1024]    │                             │
│                                           │                             │
│  Additional Kernel Packages:              │                             │
│  [□] kernel-azure                         │                             │
│  [□] kernel-modules-azure                 │                             │
│  [□] kernel-rt (real-time)                │                             │
│  [+ add package________________]          │                             │
│                                           │                             │
│         [← Back]              [Next →]    │                             │
│                                           │                             │
└───────────────────────────────────────────┴─────────────────────────────┘
```

### Step 5: Network

```
┌───────────────────────────────────────────┬─────────────────────────────┐
│                                           │                             │
│  STEP 5: Network Configuration            │  Live Preview (YAML)        │
│  ─────────────────────────────            │                             │
│                                           │  network:                   │
│  Backend: [◉ systemd-networkd]            │    backend: systemd-networkd│
│           [○ netplan          ]           │    interfaces:              │
│                                           │      - name: eth0           │
│  Interfaces:                              │        dhcp4: true          │
│  ┌─────────────────────────────────────┐  │                             │
│  │ ┌─────────────────────────────────┐ │  │                             │
│  │ │ Interface: eth0          [×]    │ │  │                             │
│  │ │ Mode: [◉ DHCP v4] [□ DHCP v6]  │ │  │                             │
│  │ │       [○ Static IP           ]  │ │  │                             │
│  │ └─────────────────────────────────┘ │  │                             │
│  │                                     │  │                             │
│  │ [+ Add Interface]                   │  │                             │
│  └─────────────────────────────────────┘  │                             │
│                                           │                             │
│  DNS Servers (optional):                  │                             │
│  [8.8.8.8___________] [×]                 │                             │
│  [+ add DNS server]                       │                             │
│                                           │                             │
│         [← Back]              [Next →]    │                             │
│                                           │                             │
└───────────────────────────────────────────┴─────────────────────────────┘
```

### Step 6: Users

```
┌───────────────────────────────────────────┬─────────────────────────────┐
│                                           │                             │
│  STEP 6: User Accounts                    │  Live Preview (YAML)        │
│  ──────────────────────                   │                             │
│                                           │  users:                     │
│  ┌─────────────────────────────────────┐  │    - name: admin            │
│  │ ┌─────────────────────────────────┐ │  │      groups: ["wheel"]      │
│  │ │ 👤 admin                  [×]   │ │  │      sudo: true             │
│  │ │ Groups: wheel                   │ │  │    - name: deploy           │
│  │ │ Sudo: ✓                         │ │  │      groups: ["docker"]     │
│  │ │ Shell: /bin/bash                │ │  │                             │
│  │ │ [Edit]                          │ │  │                             │
│  │ └─────────────────────────────────┘ │  │                             │
│  │ ┌─────────────────────────────────┐ │  │                             │
│  │ │ 👤 deploy                 [×]   │ │  │                             │
│  │ │ Groups: docker                  │ │  │                             │
│  │ │ Sudo: ✗                         │ │  │                             │
│  │ │ Shell: /bin/bash                │ │  │                             │
│  │ │ [Edit]                          │ │  │                             │
│  │ └─────────────────────────────────┘ │  │                             │
│  │                                     │  │                             │
│  │ [+ Add User]                        │  │                             │
│  └─────────────────────────────────────┘  │                             │
│                                           │                             │
│  ⚠ Passwords are not stored in the UI.   │                             │
│    Set them via hashed values or leave    │                             │
│    empty for key-based auth.              │                             │
│                                           │                             │
│         [← Back]              [Next →]    │                             │
│                                           │                             │
└───────────────────────────────────────────┴─────────────────────────────┘
```

### Step 7: Security

```
┌───────────────────────────────────────────┬─────────────────────────────┐
│                                           │                             │
│  STEP 7: Security & Immutability          │  Live Preview (YAML)        │
│  ────────────────────────────             │                             │
│                                           │  immutability:              │
│  dm-verity (Read-Only Root):              │    enabled: true            │
│  [ ◉ Enabled ] [ ○ Disabled ]            │                             │
│                                           │                             │
│  UEFI Secure Boot:                        │                             │
│  [□] Enable Secure Boot signing           │                             │
│                                           │                             │
│  ┌─ Secure Boot Files (if enabled) ────┐  │                             │
│  │ DB Key:  [________________________] │  │                             │
│  │ DB Cert: [________________________] │  │                             │
│  │ DB Cer:  [________________________] │  │                             │
│  └─────────────────────────────────────┘  │                             │
│                                           │                             │
│  Bootloader:                              │                             │
│  Type:     [◉ EFI] [○ Legacy]            │                             │
│  Provider: [◉ grub2] [○ systemd-boot]    │                             │
│                                           │                             │
│         [← Back]              [Next →]    │                             │
│                                           │                             │
└───────────────────────────────────────────┴─────────────────────────────┘
```

### Step 8: Custom Scripts & Files

```
┌───────────────────────────────────────────┬─────────────────────────────┐
│                                           │                             │
│  STEP 8: Custom Scripts & Files           │  Live Preview (YAML)        │
│  ──────────────────────────────           │                             │
│                                           │  configurations:            │
│  Post-install Scripts (run in order):     │    - name: enable-docker    │
│  ┌─────────────────────────────────────┐  │      run: |                 │
│  │ ≡ 1. enable-docker           [×]   │  │        systemctl enable ... │
│  │ ≡ 2. configure-firewall     [×]   │  │                             │
│  │                                     │  │  additionalFiles:           │
│  │ [+ Add Script]                      │  │    - source: ./motd         │
│  └─────────────────────────────────────┘  │      destination: /etc/motd │
│  (drag ≡ to reorder)                     │                             │
│                                           │                             │
│  Additional Files:                        │                             │
│  ┌─────────────────────────────────────┐  │                             │
│  │ Source         → Destination        │  │                             │
│  │ ./motd         → /etc/motd    [×]   │  │                             │
│  │ ./sshd_config  → /etc/ssh/..  [×]   │  │                             │
│  │                                     │  │                             │
│  │ [+ Add File] or [📁 Upload]         │  │                             │
│  └─────────────────────────────────────┘  │                             │
│                                           │                             │
│         [← Back]              [Next →]    │                             │
│                                           │                             │
└───────────────────────────────────────────┴─────────────────────────────┘
```

### Step 9: Review & Generate

```
┌─────────────────────────────────────────────────────────────────────────┐
│                                                                         │
│  STEP 9: Review Template                                                │
│  ───────────────────────                                                │
│                                                                         │
│  ┌─────────────────────────────────────────────────────────────────┐    │
│  │ Summary                                                         │    │
│  │                                                                 │    │
│  │ Image:     my-edge-image v1.0.0                                 │    │
│  │ Target:    Azure Linux (azl3) / x86_64 / raw                    │    │
│  │ Disk:      4 GiB, GPT, 2 partitions                            │    │
│  │ Packages:  6 packages (cloud-init, docker-ce, ...)              │    │
│  │ Kernel:    6.6 with custom cmdline                              │    │
│  │ Network:   1 interface (eth0, DHCPv4)                           │    │
│  │ Users:     2 users (admin, deploy)                              │    │
│  │ Security:  dm-verity enabled, EFI + grub2                       │    │
│  │ Scripts:   2 post-install scripts                               │    │
│  └─────────────────────────────────────────────────────────────────┘    │
│                                                                         │
│  Validation: ✓ Template is valid                                        │
│                                                                         │
│  ┌─────────────────────────────────────────────────────────────────┐    │
│  │ Generated YAML (editable)                                       │    │
│  │ ─────────────────────────                                       │    │
│  │ image:                                                          │    │
│  │   name: my-edge-image                                           │    │
│  │   version: "1.0.0"                                              │    │
│  │ target:                                                         │    │
│  │   os: azure-linux                                               │    │
│  │   ...                                                           │    │
│  └─────────────────────────────────────────────────────────────────┘    │
│                                                                         │
│  [📋 Copy YAML] [💾 Save Template] [🔨 Build Image] [📤 Export File]   │
│                                                                         │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## Mode 2: Canvas (Drag & Drop)

Best for: power users, complex images, quick iteration.

The canvas shows all building blocks as draggable cards. Users drag blocks from the palette onto the canvas to compose their template.

```
┌─────────────────────────────────────────────────────────────────────────┐
│ Builder: [□ Wizard] [■ Canvas]                                          │
├──────────────────┬──────────────────────────────────┬───────────────────┤
│                  │                                  │                   │
│  Block Palette   │  Template Canvas                 │  YAML Preview     │
│  ──────────────  │  ──────────────                  │  ────────────     │
│                  │                                  │                   │
│  ┌────────────┐  │  ┌──────────────────────────┐   │  image:           │
│  │ 🎯 Target  │  │  │ 🎯 TARGET                │   │    name: my-img   │
│  │            │ ─┼─▶│ Azure Linux / azl3        │   │    version: 1.0.0 │
│  └────────────┘  │  │ x86_64 / raw         [⚙] │   │  target:          │
│                  │  └──────────────────────────┘   │    os: azure-linux│
│  ┌────────────┐  │         │                       │    ...            │
│  │ 💿 Disk    │  │         ▼                       │  disk:            │
│  │            │ ─┼─▶┌──────────────────────────┐   │    name: default  │
│  └────────────┘  │  │ 💿 DISK                  │   │    size: "4GiB"   │
│                  │  │ 4GiB, GPT, 2 parts   [⚙] │   │    ...            │
│  ┌────────────┐  │  └──────────────────────────┘   │  systemConfig:    │
│  │ 📦 Packages│  │         │                       │    packages:      │
│  │            │ ─┼─▶┌──────────────────────────┐   │      - docker-ce  │
│  └────────────┘  │  │ 📦 PACKAGES              │   │      - nginx      │
│                  │  │ 6 packages           [⚙] │   │    ...            │
│  ┌────────────┐  │  └──────────────────────────┘   │                   │
│  │ ⚙ Kernel   │  │         │                       │                   │
│  │            │  │  ┌──────────────────────────┐   │                   │
│  └────────────┘  │  │ ⚙ KERNEL                 │   │                   │
│                  │  │ v6.6, custom cmdline  [⚙] │   │                   │
│  ┌────────────┐  │  └──────────────────────────┘   │                   │
│  │ 🌐 Network │  │         │                       │                   │
│  │            │  │  ┌──────────────────────────┐   │                   │
│  └────────────┘  │  │ 🌐 NETWORK               │   │                   │
│                  │  │ 1 interface, DHCP    [⚙] │   │                   │
│  ┌────────────┐  │  └──────────────────────────┘   │                   │
│  │ 👤 Users   │  │         │                       │                   │
│  │            │  │  ┌──────────────────────────┐   │                   │
│  └────────────┘  │  │ 👤 USERS                 │   │                   │
│                  │  │ 2 users (admin, deploy)[⚙]│   │                   │
│  ┌────────────┐  │  └──────────────────────────┘   │                   │
│  │ 🔒 Security│  │         │                       │                   │
│  │            │  │  ┌──────────────────────────┐   │                   │
│  └────────────┘  │  │ 🔒 SECURITY              │   │                   │
│                  │  │ dm-verity, EFI       [⚙] │   │                   │
│  ┌────────────┐  │  └──────────────────────────┘   │                   │
│  │ 📜 Scripts │  │                                  │                   │
│  │            │  │                                  │                   │
│  └────────────┘  │                                  │                   │
│                  │                                  │                   │
│  ┌────────────┐  │                                  │                   │
│  │ 📁 Files   │  │                                  │                   │
│  │            │  │                                  │                   │
│  └────────────┘  │                                  │                   │
│                  │                                  │                   │
│  ┌────────────┐  │                                  │                   │
│  │ 📂 Repos   │  │                                  │                   │
│  │            │  │                                  │                   │
│  └────────────┘  │                                  │                   │
│                  │                                  │                   │
└──────────────────┴──────────────────────────────────┴───────────────────┘
```

### Canvas Block Interaction

Clicking [⚙] on any canvas block opens an inline editor panel:

```
┌──────────────────────────────────────┐
│ 📦 PACKAGES                    [×]   │
│ ─────────────────────────────────    │
│                                      │
│ Search: [_________________________]  │
│                                      │
│ Quick Groups:                        │
│ [✓] Container Runtime                │
│ [□] Kubernetes                       │
│ [✓] Web Server                       │
│                                      │
│ Added:                               │
│ ┌──────────────────────────────────┐ │
│ │ docker-ce [×] containerd.io [×]  │ │
│ │ docker-ce-cli [×] nginx [×]     │ │
│ │ cloud-init [×] rsyslog [×]      │ │
│ └──────────────────────────────────┘ │
│                                      │
│ [+ add_______________]               │
│                                      │
│ [Apply]                              │
└──────────────────────────────────────┘
```

---

## Template Library View

```
┌─────────────────────────────────────────────────────────────────────────┐
│                                                                         │
│  Template Library                                    [+ New Template]    │
│  ════════════════                                                        │
│                                                                         │
│  Filters:                                                               │
│  OS: [All ▼]  Arch: [All ▼]  Type: [All ▼]  Search: [___________] 🔍  │
│                                                                         │
│  ┌─────────────────────┐ ┌─────────────────────┐ ┌─────────────────────┐
│  │ azl3-x86_64-edge    │ │ azl3-x86_64-minimal │ │ debian13-x86_64-min │
│  │ ─────────────────── │ │ ─────────────────── │ │ ─────────────────── │
│  │ Azure Linux 3       │ │ Azure Linux 3       │ │ Debian 13           │
│  │ x86_64 | raw        │ │ x86_64 | raw        │ │ x86_64 | raw        │
│  │                     │ │                     │ │                     │
│  │ Tags: edge, docker  │ │ Tags: minimal       │ │ Tags: minimal       │
│  │       iot, cloud    │ │                     │ │                     │
│  │                     │ │                     │ │                     │
│  │ [Edit] [Clone]      │ │ [Edit] [Clone]      │ │ [Edit] [Clone]      │
│  │ [Build] [AI Refine] │ │ [Build] [AI Refine] │ │ [Build] [AI Refine] │
│  └─────────────────────┘ └─────────────────────┘ └─────────────────────┘
│                                                                         │
│  ┌─────────────────────┐ ┌─────────────────────┐ ┌─────────────────────┐
│  │ elxr12-x86_64-edge  │ │ ubuntu24-aarch64    │ │ ...                 │
│  │ ...                  │ │ ...                  │ │                     │
│  └─────────────────────┘ └─────────────────────┘ └─────────────────────┘
│                                                                         │
│  Showing 1-6 of 54 templates                    [< 1 2 3 ... 9 >]      │
│                                                                         │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## Build Dashboard

```
┌─────────────────────────────────────────────────────────────────────────┐
│                                                                         │
│  Build Dashboard                                                        │
│  ═══════════════                                                        │
│                                                                         │
│  Active Builds:                                                         │
│  ┌─────────────────────────────────────────────────────────────────┐    │
│  │ 🔨 azl3-x86_64-edge v1.0.0                                     │    │
│  │ Started: 2 min ago | Phase: Installing packages                 │    │
│  │ ████████████████████░░░░░░░░░░ 65%                              │    │
│  │                                                                 │    │
│  │ Live Logs:                                                      │    │
│  │ ┌─────────────────────────────────────────────────────────────┐ │    │
│  │ │ [14:32:01] Resolving package dependencies...                │ │    │
│  │ │ [14:32:03] Downloading docker-ce-24.0.7.rpm (45 MB)        │ │    │
│  │ │ [14:32:15] Downloading containerd.io-1.6.28.rpm (28 MB)    │ │    │
│  │ │ [14:32:22] Installing docker-ce...                          │ │    │
│  │ │ [14:32:30] Installing nginx...                              │ │    │
│  │ │ █                                                           │ │    │
│  │ └─────────────────────────────────────────────────────────────┘ │    │
│  │                                                       [Cancel]  │    │
│  └─────────────────────────────────────────────────────────────────┘    │
│                                                                         │
│  Build History:                                                         │
│  ┌──────────┬─────────────────┬────────────┬──────────┬──────────┐     │
│  │ Status   │ Template        │ Duration   │ Size     │ Date     │     │
│  ├──────────┼─────────────────┼────────────┼──────────┼──────────┤     │
│  │ ✓ Pass   │ debian13-min    │ 4m 23s     │ 1.2 GB   │ Jun 23   │     │
│  │ ✗ Fail   │ elxr12-edge     │ 2m 11s     │ —        │ Jun 22   │     │
│  │ ✓ Pass   │ azl3-minimal    │ 3m 45s     │ 890 MB   │ Jun 22   │     │
│  └──────────┴─────────────────┴────────────┴──────────┴──────────┘     │
│                                                                         │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## Validation UX

Validation runs continuously as the user configures blocks. Feedback is shown inline:

```
┌─────────────────────────────────────────┐
│ Validation Status                       │
│ ─────────────────                       │
│                                         │
│ ✓ image: valid                          │
│ ✓ target: valid                         │
│ ✓ disk: valid                           │
│ ⚠ packages: 1 warning                  │
│   └─ "docker-ce" may require           │
│      additional repository              │
│ ✗ network: 1 error                      │
│   └─ Static IP "192.168.1.10" missing   │
│      CIDR notation (e.g. /24)           │
│ ✓ users: valid                          │
│ ✓ security: valid                       │
│                                         │
│ Overall: ⚠ 1 error, 1 warning          │
│ [Fix errors to enable build]            │
└─────────────────────────────────────────┘
```

---

## Responsive Behavior

- **Desktop (>1200px)**: Full three-column layout (palette + canvas + preview)
- **Tablet (768-1200px)**: Two columns, YAML preview in collapsible drawer
- **Mobile (<768px)**: Wizard mode only, single column, no canvas

---

## Component Library Recommendation

| Component | Library | Rationale |
|-----------|---------|-----------|
| UI Framework | React + TypeScript | Ecosystem, type safety |
| Component Library | Radix UI + Tailwind | Accessible, unstyled primitives |
| Drag & Drop | dnd-kit | Modern, accessible, tree-shakable |
| YAML Editor | CodeMirror 6 | Lightweight, YAML mode, inline markers |
| State Management | Zustand | Simple, template state fits single store |
| Form Validation | Zod | Mirror JSON Schema constraints |
| SSE Client | Native EventSource | No library needed |
| Icons | Lucide React | Consistent, open source |

---

## User Flows

### Flow 1: New user creates first image
```
Templates → Pick "azl3-x86_64-minimal" → [Clone] → Builder (Wizard)
→ Modify packages → Add user → Review → Validate → Build
```

### Flow 2: Power user iterates quickly
```
Builder (Canvas) → Drag Target + Disk + Packages → Configure each [⚙]
→ Validate (auto) → Export YAML → Build from CLI
```

### Flow 3: AI-assisted generation
```
Chat → "Create an edge image with docker for Azure Linux"
→ AI generates template → [Open in Builder] → Fine-tune in Canvas
→ Validate → Build
```

### Flow 4: Template from existing
```
Templates → Filter "elxr" → [Clone] → Builder → Modify → Save as new
```
