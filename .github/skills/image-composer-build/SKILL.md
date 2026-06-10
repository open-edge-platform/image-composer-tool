---
name: image-composer-build
description: "Use when building OS disk images using the image-composer-tool and YAML template definitions for any supported OS/imageType."
argument-hint: "template-name or use-case keyword"
user-invocable: true
---

# image-composer-tool Build

## Overview

Build OS disk images using the `image-composer-tool` ELF binary and declarative YAML template definitions. The tool supports **8+ OS families** and **~60 templates** producing raw, vhdx, iso, qcow2, vmdk, vdi, and initrd artifacts.

To ensure the latest build engine is used, recompile `image-composer-tool` before every build run.

Templates define the full image specification: target OS/distribution, disk layout (partition table, partitions, filesystems), system configuration (packages, kernel, bootloader, SELinux, additional files), and output artifact formats.

## When to Use

- Build, compose, or generate a disk image from any template under `image-templates/`
- User provides a template name or asks for a specific OS/type combination
- Debugging a failed image build
- Checking available templates before starting a build

> **Need extra packages or a third-party repo?** Don't edit the canonical templates in `image-templates/`. Use the `image-composer-custom` skill to clone a base template and inject additional packages/repos into `user-templates/`.

## Supported OS Families

| OS | Prefix | Variants |
|----|--------|----------|
| **Ubuntu 24.04** | `ubuntu24-` | minimal-raw, minimal-iso, minimal-initrd, edge-raw, server-cloud, desktop, dlstreamer, dkms-demo, ptl-pv, unattended-iso, robotics (jazzy, ros2) |
| **Ubuntu 26.04** | `ubuntu26-` | minimal-raw |
| **Ubuntu (minimal cloud)** | `ubuntu-minimal-` | cloud-amd64 |
| **Debian 13** | `debian13-` | minimal-raw, minimal-iso, minimal-initrd, desktop-virtualization-iso (×86_64 + aarch64) |
| **AZL3** (Azure Linux 3) | `azl3-` | minimal-raw, minimal-iso, minimal-initrd, edge-raw, dlstreamer (×86_64 + aarch64) |
| **ELXR12** | `elxr12-` | minimal-raw, minimal-iso, minimal-initrd, edge-raw, dlstreamer, cloud (×86_64 + aarch64) |
| **ELXR Edge 26.04** | `elxr-edge-26.04-` | minimal-raw, minimal-iso, minimal-initrd, edge-raw (×86_64 + aarch64) |
| **EMT3** | `emt3-` | minimal-raw, minimal-iso, minimal-initrd, edge-raw, desktop-virtualization-iso, dlstreamer, emf-raw, emf-rt-raw, elvin-emf (×86_64) |
| **RCD10** (Rocky Linux) | `rcd10-` | minimal-raw, rockylinux, dlstreamer (×86_64) |

Each combination maps to a template file named `<prefix><variant>.yml` in `image-templates/`.

> **Reference files:** `references/gpg-key-workaround.md` — GPG key verification failures and the `[trusted=yes]` fix. `references/non-fatal-chroot-warnings.md` — systemd-boot EFI variable warnings and other non-fatal chroot errors.

## Supported Output Types

Set via `disk.artifacts[].type` in the template:

| Type | Format | Use case |
|------|--------|----------|
| `raw` | Raw disk image | Cloud, VM direct boot |
| `raw + gz` | Compressed raw | Distribution/storage |
| `vhdx` | Hyper-V VHDX | Windows hypervisors |
| `iso` | Bootable ISO | Installers, live media |
| `qcow2` | QEMU Copy-On-Write | QEMU/KVM (template-driven) |
| `vmdk` | VMware VMDK | VMware (template-driven) |
| `vdi` | VirtualBox VDI | VirtualBox (template-driven) |
| `vhd` | Azure VHD | Azure (template-driven) |
| `initrd` | Init RAM FS | Minimal boot initramfs |

Templates can produce multiple artifacts — e.g., minimal-raw templates commonly output both `raw + gz` and `vhdx`.

## Basic Command

```bash
# From the directory containing the binary and image-templates/
sudo -E ./image-composer-tool build image-templates/<template>.yml
```

## Mandatory Pre-Build: Recompile Engine

Always run this first, immediately before any image build:

```bash
go build -buildmode=pie -ldflags "-s -w" ./cmd/image-composer-tool
```

## Workflow

### 0. Find the Right Template (Discoverability Script)

A helper script scans all templates, extracts descriptions from the YAML, and prints them grouped by OS family. This is the fastest way to find what you need.

```bash
python3 .github/skills/image-composer-build/scripts/list-templates.py
```

**Filter by keyword** — pass any keyword to see only matching templates:

| You want | Run this |
|----------|----------|
| ROS2 / robotics | `python3 .github/skills/image-composer-build/scripts/list-templates.py ros2` |
| Cloud images | `python3 .github/skills/image-composer-build/scripts/list-templates.py cloud` |
| DL Streamer / AI video | `python3 .github/skills/image-composer-build/scripts/list-templates.py dlstreamer` |
| Desktop / GUI | `python3 .github/skills/image-composer-build/scripts/list-templates.py desktop` |
| ISO installers | `python3 .github/skills/image-composer-build/scripts/list-templates.py iso` |
| ARM64 / aarch64 | `python3 .github/skills/image-composer-build/scripts/list-templates.py aarch64` |
| Edge / IoT | `python3 .github/skills/image-composer-build/scripts/list-templates.py edge` |

When a filter is applied, the script also prints the exact `sudo -E ./image-composer-tool build` commands at the bottom for quick execution.

Alternatively, a plain `ls` + `grep` also works:

```bash
# All templates
ls image-templates/

# Filter by OS
ls image-templates/ | grep ubuntu24
ls image-templates/ | grep debian13
ls image-templates/ | grep emt3
ls image-templates/ | grep elxr

# Filter by type
ls image-templates/ | grep raw
ls image-templates/ | grep iso
ls image-templates/ | grep initrd
```

### 1. Review the Template

Open the YAML to verify it matches the target. Key fields:

- **metadata.description** — human-readable summary of what the image is for
- **metadata.use_cases** — listed use cases (cloud, dev, desktop, virtualization, etc.)
- **target.os / dist / arch / imageType** — what OS and format
- **disk.artifacts[].type** — what output formats are produced
- **disk.size** — how much space the build needs
- **systemConfig.packages** — what gets installed
- **systemConfig.kernel** — kernel version, cmdline, and packages

### 2. Recompile Build Engine (Required)

Always rebuild the binary right before the image build so template/schema and build logic are current:

```bash
go build -buildmode=pie -ldflags "-s -w" ./cmd/image-composer-tool
```

### 3. Check Disk Space

Images are typically 4-8 GiB. Build process needs ~2x headroom for temp + compression.

```bash
df -h .
```

### 4. Run the Build

```bash
sudo -E ./image-composer-tool build image-templates/<template>.yml
```

The tool streams progress to stdout/stderr. Watch for:
- Partition table creation
- Filesystem formatting (mkfs)
- Package installation (package manager output)
- Final artifact path and size on completion

### 5. Verify the Artifact

Check the working directory for output files (the tool writes to `workspace/`):

```bash
find workspace/ -name "*.raw*" -o -name "*.vhdx*" -o -name "*.iso*" 2>/dev/null
ls -lh *.raw* *.vhdx* *.iso* *.qcow2* 2>/dev/null
```

If compressed (`.gz`), verify integrity:

```bash
gunzip -t <artifact>.gz
```

## Common Pitfalls

1. **Missing `sudo -E`** — the tool needs root for loop device management, mount, partitions. `-E` preserves environment variables the tool may need.

2. **Wrong working directory** — the tool resolves `image-templates/` and output paths relative to cwd. Run from the directory containing both `./image-composer-tool` and `./image-templates/`.

3. **Disk space exhaustion** — a 4 GiB raw image needs at least 6-8 GiB free during the build. Use `df -h .` before starting.

4. **Missing template fields** — if the YAML omits required fields (e.g., `partitionTableType` for raw, `kernel.version` for package resolution), the tool errors with `field X is required`. Read the error and add the missing field.

5. **Kernel version not available** — templates pin a specific kernel version (e.g., `"6.12.74"` for Debian 13). If the distro's repos don't carry that exact version, the build fails. Fallback: use a kernel package glob like `linux-image-amd64` or `linux-image-generic*` and adjust the version field to a known-good value.

6. **Artifact collision** — the tool may refuse to overwrite existing output. Clean up old artifacts first:
   ```bash
   rm -f *.raw* *.vhdx* *.iso* 2>/dev/null
   ```

7. **Wrong arch template for hardware** — an aarch64 template on x86_64 hardware will fail (no cross-arch QEMU user mode). Match `target.arch` to your build machine.

8. **Missing dependencies for ISO builds** — ISO templates (desktop-virtualization, unattended-iso) need `xorriso` or `genisoimage` installed on the host. Error early if missing.

9. **gunzip -t timeouts on large images** — verifying integrity with `gunzip -t` on compressed artifacts over ~1 GB can take 30+ seconds. This is normal; the tool must decompress the entire file to check the CRC. For a quick check, use `gzip -l <file>.gz` instead (reads the stored header size without full decompression).

10. **systemd-boot EFI variable warning in chroot** — during package installation in a chroot build environment, systemd-boot may emit:
   ```
   Failed to write 'LoaderSystemToken' EFI variable: No such file or directory
   dpkg: error processing package systemd-boot (--configure):
     installed systemd-boot package post-installation script subprocess returned error exit status 1
   ```
   **This is non-fatal.** EFI variables are unavailable inside a chroot. The image-composer-tool treats this as INFO-level and the build continues to completion with exit code 0. The final image boots fine because the bootloader is installed in a separate post-build step that runs outside the chroot.

11. **GPG key verification failure** — some third-party repos distribute keys in a format the tool cannot verify, causing
12. **Artifacts not in current directory** — the tool outputs to
   Error: signature verification failed (tried both armored and binary): openpgp: invalid data: tag byte does not have MSB set
   ```
   **Fix:** replace the `pkey` URL with `pkey: "[trusted=yes]"` to skip GPG verification for that repo. Package metadata is still checksum-verified. See `references/gpg-key-workaround.md` for details and example.

10. **Artifacts not in current directory** — the tool outputs to `workspace/<os-dist>-<arch>/imagebuild/<name>/`, not cwd. After a successful build, check there instead of `ls *.raw*`:
    ```bash
    find workspace/ -name "*.raw*" -o -name "*.vhdx*" -o -name "*.iso*" 2>/dev/null
    ```

## Verification Checklist

- [ ] Command ran as `sudo -E ./image-composer-tool build image-templates/<name>.yml` from the correct directory
- [ ] `image-composer-tool` was recompiled right before build using `go build -buildmode=pie -ldflags "-s -w" ./cmd/image-composer-tool`
- [ ] Build completed with exit code 0 and no error messages in output
- [ ] Artifact exists at the expected path (check with `find workspace/ -name '*.raw*' -o -name '*.vhdx*' -o -name '*.iso*' 2>/dev/null` or `ls -lh` on the workspace subdirectory)
- [ ] If `.gz`, integrity verified: `gunzip -t <artifact>.gz` (can be 30s+ on files >1 GB; use `gzip -l <file>.gz` for a quick header check)
- [ ] Artifact size reported to the user

## One-Shot Recipes

### Minimal Ubuntu 24.04 raw image (the most common)

```bash
cd /path/to/project
sudo -E ./image-composer-tool build image-templates/ubuntu24-x86_64-minimal-raw.yml
```
Output: `*.raw.gz` + `*.vhdx`

### Debian 13 minimal raw image

```bash
sudo -E ./image-composer-tool build image-templates/debian13-x86_64-minimal-raw.yml
```

### EMT3 Desktop Virtualization ISO

```bash
sudo -E ./image-composer-tool build image-templates/emt3-x86_64-desktop-virtualization-iso.yml
```
Output: a bootable ISO with QEMU/KVM, GPU SR-IOV, X11, and SELinux.

### ELXR Cloud image

```bash
sudo -E ./image-composer-tool build image-templates/elxr-cloud-amd64.yml
```

### RCD10 Rocky Linux minimal raw

```bash
sudo -E ./image-composer-tool build image-templates/rcd10-x86_64-minimal-raw.yml
```

### Quick template browse by use case

```bash
# Find cloud images
grep -l 'cloud' image-templates/*.yml

# Find ISO templates
grep -l 'imageType: iso' image-templates/*.yml

# Find aarch64 templates
grep -l 'arch: aarch64' image-templates/*.yml
```
