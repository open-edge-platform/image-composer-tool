# Image Template Reference

Templates are YAML files that define what goes into a custom OS image, the
target platform, packages, disk layout, users, and build-time customizations.
This document is the authoritative field-by-field reference for the template
format.

For a conceptual overview of how templates fit into the build pipeline, see
[Understanding the Build Process](./os-image-composer-build-process.md).

## Table of Contents

- [Image Template Reference](#image-template-reference)
  - [Table of Contents](#table-of-contents)
  - [How Templates Work](#how-templates-work)
  - [Quick Start Example](#quick-start-example)
  - [Top-Level Structure](#top-level-structure)
  - [Field Reference](#field-reference)
    - [`metadata`](#metadata)
    - [`image` (required)](#image-required)
    - [`target` (required)](#target-required)
    - [`disk`](#disk)
      - [`disk.artifacts[]`](#diskartifacts)
      - [`disk.partitions[]`](#diskpartitions)
    - [`packageRepositories`](#packagerepositories)
    - [`systemConfig`](#systemconfig)
      - [`systemConfig.kernel`](#systemconfigkernel)
      - [`systemConfig.bootloader`](#systemconfigbootloader)
      - [`systemConfig.immutability`](#systemconfigimmutability)
      - [`systemConfig.users[]`](#systemconfigusers)
      - [`systemConfig.initramfs`](#systemconfiginitramfs)
      - [`systemConfig.additionalFiles[]`](#systemconfigadditionalfiles)
      - [`systemConfig.configurations[]`](#systemconfigconfigurations)
  - [Template Merge Behavior](#template-merge-behavior)
  - [Variable Substitution](#variable-substitution)
- [Using Templates to Build Images](#using-templates-to-build-images)
- [Template Storage](#template-storage)
- [Template Variables](#template-variables)
- [Package Repositories](#package-repositories)
  - [Repository Fields](#repository-fields)
  - [Priority Behavior](#priority-behavior)
  - [AllowPackages White List](#allowpackages-white-list)
- [Best Practices](#best-practices)
  - [Template Organization](#template-organization)
  - [Template Design](#template-design)
  - [Template Sharing](#template-sharing)
- [Conclusion](#conclusion)
- [Related Documentation](#related-documentation)

## What Are Templates and How Do They Work?

Templates are predefined build specifications that serve as a foundation for
building operating system images. Here's what templates empower you to do:

- Create standardized baseline configurations.
- Impose consistency across multiple images.
- Reduce duplication of effort.
- Share and reuse common configurations with your team.

The OS Image Composer provides default image templates on a per-distribution
basis and image type (RAW vs. ISO) that can be used directly to build an
operating system from those defaults. You can override these default templates
by providing your own template and configure or override the settings and
values you want. The tool will internally merge the two to create the final
template used for image composition.

![image-templates](./assets/template.drawio.svg)

## How Templates Work

OS Image Composer ships **default templates** for each distribution and image
type (raw, ISO, initrd). When you provide a user template, the tool merges it
with the matching default; your values override or extend the defaults. The
merged result is validated against a JSON schema before the build begins.

![image-templates](./assets/template.drawio.svg)

Default templates live at:

```text
config/osv/<target.os>/<target.dist>/imageconfigs/defaultconfigs/default-<imageType>-<arch>.yml
```

> **Note:** `imageType: img` maps to `default-initrd-<arch>.yml` (there is no
> `default-img-` filename).

You do not need to edit the defaults. You can start from one of the examples in
`image-templates/` and override only what you need.

## Quick Start Example

A minimal user template only needs `image`, `target`, and optionally
`systemConfig` with extra packages:

```yaml
image:
  name: my-edge-device
  version: "1.0.0"

target:
  os: edge-microvisor-toolkit
  dist: emt3
  arch: x86_64
  imageType: raw

systemConfig:
  name: edge
  packages:
    - cloud-init
    - rsyslog
```

Everything else (disk layout, bootloader, kernel, default packages) comes from
the default template for `emt3 / raw / x86_64`.

## Top-Level Structure

A template file has up to five top-level sections plus an optional `metadata`
block:

```yaml
metadata:       # Optional - AI-searchable discovery metadata
  ...
image:          # Required - image name and version
  ...
target:         # Required - OS, distribution, architecture, image type
  ...
disk:           # Optional - disk layout, partitions, output artifacts
  ...
packageRepositories:  # Optional - additional package repositories
  - ...
systemConfig:   # Required in merged template - packages, kernel, users, etc.
  ...
```

> **Note:** **User templates** require only `image` and `target`. The remaining sections
> are merged from the default template if omitted.

---

## Field Reference

### `metadata`

Optional block for AI-powered template discovery. Ignored by the build engine.

| Field | Type | Description |
|-------|------|-------------|
| `description` | string | Human-readable description of the template |
| `use_cases` | string[] | Use cases this template targets |
| `keywords` | string[] | Keywords for search and discovery |

```yaml
metadata:
  description: "Edge device image with container runtime"
  use_cases: ["edge computing", "IoT gateway"]
  keywords: [edge, docker, emt3]
```

---

### `image` (required)

Image identification. Both fields are required.

| Field | Type | Required | Validation | Description |
|-------|------|----------|------------|-------------|
| `name` | string | **Yes** | `^[a-zA-Z0-9]([a-zA-Z0-9\-_]*[a-zA-Z0-9])?$` | Image name (alphanumeric, hyphens, underscores) |
| `version` | string | **Yes** | Semver-like: `1.0.0`, `24.04`, `1.0.0+build1` | Version string |

```yaml
image:
  name: my-edge-device
  version: "1.0.0"
```

---

### `target` (required)

Target platform. All four fields are required.

| Field | Type | Required | Valid Values | Description |
|-------|------|----------|--------------|-------------|
| `os` | string | **Yes** | `azure-linux`, `edge-microvisor-toolkit`, `wind-river-elxr`, `ubuntu`, `redhat-compatible-distro` | Target operating system |
| `dist` | string | **Yes** | See OS constraints below | Distribution identifier |
| `arch` | string | **Yes** | `x86_64`, `aarch64`, `armv7hl` | Target CPU architecture |
| `imageType` | string | **Yes** | `raw`, `iso`, `img` | Output image format |

**OS → dist constraints:**

| OS | Valid `dist` |
|----|-------------|
| `azure-linux` | `azl3` |
| `edge-microvisor-toolkit` | `emt3` |
| `wind-river-elxr` | `elxr12` |
| `ubuntu` | Any (e.g., `ubuntu24`) |
| `redhat-compatible-distro` | Any (e.g., `el10`) |

```yaml
target:
  os: ubuntu
  dist: ubuntu24
  arch: x86_64
  imageType: raw
```

---

### `disk`

Disk layout, partition scheme, and output artifact formats. If omitted, the
default template provides sensible values (typically 4–6 GiB GPT disk with EFI
boot and ext4 root partitions).

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | **Yes** (schema) | Disk configuration name (e.g., `"Default_Raw"`) |
| `path` | string | No | Disk device path (used by live installer, e.g., `/dev/sda`) |
| `size` | string | No | Disk size. Accepts: `"4GiB"`, `"8GB"`, `"4096 MiB"` |
| `partitionTableType` | string | No | `gpt` or `mbr` |
| `artifacts` | artifact[] | No | Output formats and optional compression |
| `partitions` | partition[] | No | Partition layout definitions |

#### `disk.artifacts[]`

Each entry defines one output format:

| Field | Type | Required | Valid Values | Description |
|-------|------|----------|--------------|-------------|
| `type` | string | **Yes** | `raw`, `qcow2`, `vhd`, `vhdx`, `vmdk`, `vdi` | Output image format |
| `compression` | string | No | `gz`, `gzip`, `xz`, `zstd`, `bz2` | Compression to apply |

#### `disk.partitions[]`

Each entry defines one partition:

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Partition identifier (e.g., `boot`, `rootfs`, `roothashmap`, `userdata`) |
| `name` | string | Partition label |
| `type` | string | Partition type (e.g., `esp`, `linux-root-amd64`, `linux`) |
| `typeUUID` | string | GPT type GUID (e.g., `8300`) |
| `fsType` | string | Filesystem type: `ext4`, `fat32`, `xfs`, etc. |
| `fsLabel` | string | Filesystem label |
| `start` | string | Start offset (e.g., `1MiB`, `513MiB`) |
| `end` | string | End offset (`0` means rest of disk) |
| `mountPoint` | string | Mount point (e.g., `/boot/efi`, `/`, `none`) |
| `mountOptions` | string | Mount options (e.g., `defaults`, `umask=0077`) |
| `flags` | string[] | Partition flags (e.g., `boot`, `esp`, `hidden`) |

**Example - raw disk with two partitions and two output formats:**

```yaml
disk:
  name: Edge_Raw
  size: 4GiB
  partitionTableType: gpt
  artifacts:
    - type: raw
      compression: gz
    - type: vhdx
  partitions:
    - id: boot
      type: esp
      flags: [esp, boot]
      start: 1MiB
      end: 513MiB
      fsType: fat32
      mountPoint: /boot/efi
      mountOptions: umask=0077
    - id: rootfs
      type: linux-root-amd64
      start: 513MiB
      end: "0"
      fsType: ext4
      mountPoint: /
      mountOptions: defaults
```

---

### `packageRepositories`

Optional list of additional package repositories beyond the OS base repos.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `codename` | string | **Yes** | Repository identifier (e.g., `company-internal`) |
| `url` | string | **Yes** | Repository base URL (must be a valid URI) |
| `pkey` | string | **Yes** | GPG key URL, absolute file path, or `[trusted=yes]` to skip verification |
| `component` | string | No | Repository component (e.g., `main`, `restricted`) |
| `priority` | int | No | Priority from `-9999` to `9999` (default: `0`, higher = preferred) |
| `AllowPackages` | string[] | No | Specific packages to include from this repo (package pinning) |

```yaml
packageRepositories:
  - codename: "company-internal"
    url: "https://packages.example.com/repo"
    pkey: "https://packages.example.com/gpg.key"
    component: "main"
    priority: 100
  - codename: "dev-tools"
    url: "https://dev.example.com/repo"
    pkey: "[trusted=yes]"
```

See [Multiple Package Repository Support](./os-image-composer-multi-repo-support.md)
for detailed configuration guidance.

---

### `systemConfig`

System configuration - packages, kernel, users, bootloader, build-time
commands, and more. Required in the final merged template, but optional in
user templates (as defaults already provide a complete base).

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | No | Configuration name |
| `description` | string | No | Human-readable description |
| `hostname` | string | No | System hostname |
| `packages` | string[] | No | Packages to install (additive with defaults) |
| `kernel` | object | No | Kernel configuration |
| `bootloader` | object | No | Bootloader configuration |
| `immutability` | object | No | dm-verity / Secure Boot configuration |
| `users` | user[] | No | User account definitions |
| `initramfs` | object | No | Initramfs config (ISO/initrd builds) |
| `additionalFiles` | file[] | No | Extra files to copy into the image |
| `configurations` | cmd[] | No | Shell commands to run during build |

Package names must match: `^[A-Za-z0-9](?:[A-Za-z0-9+_.:~-]*[A-Za-z0-9+])?$`
and must be unique within the list.

#### `systemConfig.kernel`

| Field | Type | Description |
|-------|------|-------------|
| `version` | string | Kernel version (e.g., `"6.12"`, `"6.14"`) |
| `cmdline` | string | Kernel boot command line |
| `packages` | string[] | Kernel packages (e.g., `["linux-image-generic-hwe-24.04"]`) |
| `enableExtraModules` | string | Additional kernel modules to load |
| `uki` | bool | Enable Unified Kernel Image (typically set by defaults) |

```yaml
systemConfig:
  kernel:
    version: "6.14"
    cmdline: "console=ttyS0,115200 console=tty0 loglevel=7"
    packages:
      - linux-image-generic-hwe-24.04

# Optional additional repositories
packageRepositories:
  - codename: emtNext
    url: https://example.com/rpms/next/base
    pkey: https://example.com/RPM-GPG-KEY
    priority: 1001
    allowPackages:
      - kernel-6.17.11
      - kernel-drivers-gpu-6.17.11
      - libva*

  - codename: edgeai
    url: https://example2.com/edgeai/
    pkey: https://example2.com/edgeai/GPG-PUB-KEY.gpg
    priority: 500
```

#### `systemConfig.bootloader`

| Field | Type | Valid Values | Description |
|-------|------|--------------|-------------|
| `bootType` | string | `efi`, `legacy` | Boot firmware type |
| `provider` | string | `grub`, `grub2`, `systemd-boot` | Bootloader software |

Typical defaults: raw images use `efi` / `systemd-boot`; ISO images use
`efi` / `grub`.

#### `systemConfig.immutability`

Configures dm-verity immutable root filesystem and optional UEFI Secure Boot
signing.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `enabled` | bool | **Yes** (when section present) | Enable dm-verity immutable root |
| `secureBootDBKey` | string | Conditional | Private key file (`.key` or `.pem`) |
| `secureBootDBCrt` | string | Conditional | Certificate in PEM format (`.crt` or `.pem`) |
| `secureBootDBCer` | string | Conditional | Certificate in DER format (`.cer`) |

> **Note:** If **any** Secure Boot field is provided, **all three** must be provided and
> `enabled` must be `true`.

```yaml
systemConfig:
  immutability:
    enabled: true
    secureBootDBKey: /path/to/db.key
    secureBootDBCrt: /path/to/db.crt
    secureBootDBCer: /path/to/db.cer
```

#### `systemConfig.users[]`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | **Yes** | Username |
| `password` | string | No | Password (plain text or pre-hashed with `$` prefix) |
| `hash_algo` | string | No | Hash algorithm: `bcrypt`, `sha512`, `sha256`, `md5` (md5 is insecure — avoid in production) |
| `passwordMaxAge` | int | No | Max password age in days |
| `startupScript` | string | No | Script to run on login |
| `groups` | string[] | No | Additional groups |
| `sudo` | bool | No | Grant sudo permissions |
| `home` | string | No | Custom home directory |
| `shell` | string | No | Login shell (e.g., `/bin/bash`) |

```yaml
systemConfig:
  users:
    - name: admin
      password: "changeme"
      sudo: true
      groups: [docker, wheel]
      shell: /bin/bash
      - name: service-account
      shell: /usr/sbin/nologin
```

#### `systemConfig.initramfs`

Used for ISO and initrd builds. Points to the initramfs configuration template.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `template` | string | **Yes** (when section present) | Path to the initramfs config template file |

#### `systemConfig.additionalFiles[]`

Copy host files into the image at build time.

| Field | Type | Description |
|-------|------|-------------|
| `local` | string | Source path on the host (absolute, or relative to template directory) |
| `final` | string | Destination path inside the image |

```yaml
systemConfig:
  additionalFiles:
    - local: files/dhcp.network
      final: /etc/systemd/network/dhcp.network
    - local: files/motd
      final: /etc/motd
```

#### `systemConfig.configurations[]`

Shell commands executed inside the chroot during the configuration stage.

| Field | Type | Description |
|-------|------|-------------|
| `cmd` | string | Shell command to execute |

```yaml
systemConfig:
  configurations:
    - cmd: systemctl enable docker
    - cmd: echo "BuildDate=$(date)" >> /etc/image-info
```

## Package Repositories

Use `packageRepositories` to add extra Debian or RPM repositories to a build.
Each entry defines where to fetch package metadata and how candidates are
selected when the same package exists in multiple repositories.

### Repository Fields

- `codename`: repository identifier.
- `url`: repository base URL.
- `component`: optional Debian component (for example, `main`, `universe`) for multi-component repositories.
- `pkey`: GPG key reference; supports `http://`/`https://` URLs, `file://` URLs, absolute local paths, or `[trusted=yes]` for supported Debian flows.
- `priority`: numeric repository preference used in conflict resolution.
- `allowPackages`: optional package white list for metadata filtering.

### Priority Behavior

`priority` is evaluated during package candidate selection across repositories.

- Higher numeric values are preferred.
- Debian resolver also supports APT-like behavior:
  - `< 0`: block packages from that repository
  - `990`: prefer over default repositories
  - `1000`: install even if lower version
  - `> 1000`: force preference

When candidates have equivalent priority, version constraints and dependency
context determine the final package choice.

### AllowPackages White List

`allowPackages` limits which package names are indexed from a specific
repository.

- If omitted or empty, all repository packages are eligible.
- If present, only matching package names are indexed.
- Supported matching modes:
  - exact name (for example `spice-server`)
  - prefix/version pin (for example `kernel-6.17.11`)
  - glob patterns (for example `libva*`, `wayland*`)

Filtering happens at metadata-parse time, before dependency resolution.

---

## Template Merge Behavior

When your user template is merged with the default template, different sections
follow different strategies:

| Section | Strategy |
|---------|----------|
| `image.name`, `image.version` | User overrides default if non-empty |
| `target` | User value used entirely |
| `disk` | User replaces entire default if non-empty |
| `systemConfig.packages` | **Additive** - user packages appended to defaults (deduplicated) |
| `systemConfig.kernel` | User overrides `version`, `cmdline`, `packages` individually if non-empty |
| `systemConfig.bootloader` | User overrides individual fields if non-empty |
| `systemConfig.users` | Merged by `name` - same-name users merged field-by-field; new users appended |
| `systemConfig.additionalFiles` | Merged by `final` path - same destination overrides; new files appended |
| `systemConfig.configurations` | **Additive** - user commands appended after defaults |
| `systemConfig.immutability` | Merged only if user explicitly provides the section |
| `packageRepositories` | Merged by `codename` - same codename overrides; new repos appended |

## Variable Substitution

Templates support variable substitution using `${variable_name}` syntax. You
can provide variable values via a separate YAML file or command-line flags at
build time.

To learn how variables interact with each build stage, see
[Build Stages in Detail](./os-image-composer-build-process.md#build-stages-in-detail).



## Best Practices

1. **Start from examples** - copy a template from `image-templates/` and modify
   only the fields you need. Let defaults handle the rest.
2. **Keep templates minimal** - override only what differs from the default.
   Smaller templates are easier to maintain and review.
3. **Use descriptive names** - name images and configs after their purpose
   (e.g., `factory-floor-edge`, not `test-image-3`).
4. **Version control your templates** - store them in Git alongside your
   deployment code.
5. **Validate before building** - run `os-image-composer validate template.yml`
   to catch errors early.
6. **Prefer `additionalFiles` over `configurations`** - copying config files is
   more reproducible than running arbitrary shell commands.

## Related Documentation

- [Understanding the Build Process](./os-image-composer-build-process.md)
- [Multiple Package Repository Support](./os-image-composer-multi-repo-support.md)
- [OS Image Composer CLI Reference](./os-image-composer-cli-specification.md)
- [Common Build Patterns](./os-image-composer-build-process.md#common-build-patterns)

<!--hide_directive
:::{toctree}
:hidden:

os-image-composer-multi-repo-support
:::
hide_directive-->
