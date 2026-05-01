# ARM64 Image Creation Guide

A practical guide to build ARM64/aarch64 images with Image Composer Tool (ICT) on a x86_64 host platform for:

- Wind River eLxr (`elxr12`)
- Azure Linux (`azl3`)
- Ubuntu (`ubuntu24`)

This guide covers both ARM64 boot paths used in this repository:

- GRUB on UEFI
- UKI with systemd-boot on UEFI

## Table of Contents

- [ARM64 Image Creation Guide](#arm64-image-creation-guide)
  - [Table of Contents](#table-of-contents)
  - [Before You Start](#before-you-start)
  - [ARM64 Templates in This Repository](#arm64-templates-in-this-repository)
  - [Choose Boot Mode](#choose-boot-mode)
  - [Build Commands](#build-commands)
    - [Ubuntu ARM64 (GRUB)](#ubuntu-arm64-grub)
    - [Ubuntu ARM64 (UKI)](#ubuntu-arm64-uki)
    - [eLxr ARM64 (UKI Default)](#elxr-arm64-uki-default)
    - [Azure Linux ARM64 (UKI Default)](#azure-linux-arm64-uki-default)
  - [Validate Before Build](#validate-before-build)
  - [Verify ARM64 Boot Artifacts](#verify-arm64-boot-artifacts)
  - [Secure Boot Signing (Optional)](#secure-boot-signing-optional)
  - [Common ARM64 Troubleshooting](#common-arm64-troubleshooting)
  - [Related Documentation](#related-documentation)

---

## Before You Start

1. Build ICT:

```bash
go build -buildmode=pie -ldflags "-s -w" ./cmd/image-composer-tool
```

2. Verify host dependencies (Ubuntu host recommended):
ICT automatically installs the following packages at runtime, but you can pre-install them to speed up subsequent builds:

```bash
sudo apt install systemd-ukify mmdebstrap qemu-user-static arch-test binfmt-support grub-common sbsigntool dosfstools mtools xorriso qemu-utils
```

3. Use `sudo -E` for builds (required for loop devices, mounts, chroot operations):

```bash
sudo -E ./image-composer-tool build <template.yml>
```

## ARM64 Templates in This Repository

Use these templates as working starting points:

- eLxr UKI raw: [image-templates/elxr12-aarch64-minimal-raw.yml](../../image-templates/elxr12-aarch64-minimal-raw.yml)
- Azure Linux UKI raw: [image-templates/azl3-aarch64-edge-raw.yml](../../image-templates/azl3-aarch64-edge-raw.yml)
- Ubuntu GRUB raw: [image-templates/ubuntu24-aarch64-minimal-raw.yml](../../image-templates/ubuntu24-aarch64-minimal-raw.yml)
- Ubuntu UKI raw: [image-templates/ubuntu24-aarch64-minimal-uki.yml](../../image-templates/ubuntu24-aarch64-minimal-uki.yml)
- Ubuntu edge UKI raw: [image-templates/ubuntu24-aarch64-edge-raw.yml](../../image-templates/ubuntu24-aarch64-edge-raw.yml)

Default ARM64 baseline configs are in:

- Ubuntu: [config/osv/ubuntu/ubuntu24/imageconfigs/defaultconfigs/default-raw-aarch64.yml](../../config/osv/ubuntu/ubuntu24/imageconfigs/defaultconfigs/default-raw-aarch64.yml)
- eLxr: [config/osv/wind-river-elxr/elxr12/imageconfigs/defaultconfigs/default-raw-aarch64.yml](../../config/osv/wind-river-elxr/elxr12/imageconfigs/defaultconfigs/default-raw-aarch64.yml)
- Azure Linux: [config/osv/azure-linux/azl3/imageconfigs/defaultconfigs/default-raw-aarch64.yml](../../config/osv/azure-linux/azl3/imageconfigs/defaultconfigs/default-raw-aarch64.yml)

## Choose Boot Mode

Use this decision table for ARM64:

| Distro | Bootloader provider | Typical setting |
|-------|----------------------|-----------------|
| Ubuntu | `grub` | `bootType: efi`, `provider: grub` |
| Ubuntu | `systemd-boot` + UKI | `bootType: efi`, `provider: systemd-boot` |
| eLxr | `systemd-boot` + UKI (default profile) | `bootType: efi`, `provider: systemd-boot` |
| Azure Linux | `systemd-boot` + UKI (default profile) | `bootType: efi`, `provider: systemd-boot` |

For ARM64, use UEFI boot. Do not use legacy boot mode.

## Build Commands

### Ubuntu ARM64 (GRUB)

```bash
sudo -E ./image-composer-tool build image-templates/ubuntu24-aarch64-minimal-raw.yml
```

### Ubuntu ARM64 (UKI, experimental / currently non-bootable)

This command is provided for experimental inspection and development only. It may produce a UKI artifact,
but Ubuntu UKI images composed using ICT are currently **not expected to boot** on ARM64 UEFI systems.
Use the Ubuntu ARM64 GRUB template above for a bootable Ubuntu image.

> **Note:** Ubuntu UKI images composed with ICT are currently **not bootable** (see [Known issues](#known-issues)). The build command is provided for reference and testing purposes only.

```bash
sudo -E ./image-composer-tool build image-templates/ubuntu24-aarch64-minimal-uki.yml
```

### eLxr ARM64 (UKI Default)

```bash
sudo -E ./image-composer-tool build image-templates/elxr12-aarch64-minimal-raw.yml
```

### Azure Linux ARM64 (UKI Default)

```bash
sudo -E ./image-composer-tool build image-templates/azl3-aarch64-edge-raw.yml
```

Output path pattern:

```text
<work_dir>/<os>-<dist>-<arch>/imagebuild/<system-config-name>/
```

With repository defaults, this is typically under `./workspace/...`.

## Validate Before Build

Always validate template syntax and schema first:

```bash
./image-composer-tool validate image-templates/ubuntu24-aarch64-minimal-uki.yml
```

## Verify ARM64 Boot Artifacts

After build, inspect the raw image:

```bash
./image-composer-tool inspect <path-to-image>.raw
```

Expected ARM64 UEFI indicators:

1. ESP partition mounted at `/boot/efi` during build.
2. For UKI images, `EFI/Linux/linux.efi` present.
3. ARM64 fallback loader should be `EFI/BOOT/BOOTAA64.EFI`.

Expected GRUB indicators:

1. GRUB config generated under `/boot/grub*`.
2. EFI GRUB installation path for ARM64 (`arm64-efi`) used by build logic.

## Secure Boot Signing (Optional)

To sign UKI and bootloader binaries, provide key/cert paths in template:

- `systemConfig.immutability.secureBootDBKey`
- `systemConfig.immutability.secureBootDBCrt`
- `systemConfig.immutability.secureBootDBCer`

Then set:

```yaml
systemConfig:
  immutability:
    enabled: true
```

Reference: [docs/tutorial/configure-secure-boot.md](./configure-secure-boot.md)

## Common ARM64 Troubleshooting

1. Cross-architecture dependency failures (`mmdebstrap`, `arch-test`, `qemu-user-static`, `binfmt-support`):
Install missing host tools and retry.

2. UKI build failures (`ukify` not found or stub missing):
Ensure `systemd-ukify` is installed and ARM64 EFI stubs are present on host.

3. `systemd-boot` package post-install warnings in chroot:
Some EFI variable errors are expected in chrooted installs; check final ESP artifacts rather than postinst output alone.

4. Boot mode mismatch:
For ARM64, use `bootType: efi`. Avoid legacy boot mode templates.

5. Missing GRUB ARM64 packages:
For Ubuntu GRUB templates, include ARM64 GRUB EFI packages (for example `grub-efi-arm64`) when overriding defaults.

6. Build permissions or mount failures:
Run build with `sudo -E` and ensure loop device/mount operations are allowed on host.

## Known issues
1. Ubuntu UKI images composed using the ICT are not bootable. 

## Related Documentation

- [Usage Guide](./usage-guide.md)
- [Installation Guide](./installation.md)
- [Secure Boot Configuration](./configure-secure-boot.md)
- [Image Template Guide](../architecture/image-composer-tool-templates.md)
- [Build Process Details](../architecture/image-composer-tool-build-process.md)
