# Usage Guide

A practical guide for common ICT workflows. For the complete
command reference, see the
[CLI Specification](../architecture/image-composer-tool-cli-specification.md).

## Table of Contents

  - [Binary Location](#binary-location)
  - [Commands Overview](#commands-overview)
  - [Building an Image](#building-an-image)
    - [Build Output](#build-output)
  - [Building an Overlay Image](#building-an-overlay-image)
  - [Validating a Template](#validating-a-template)
  - [Configuration](#configuration)
  - [Operations Requiring Sudo](#operations-requiring-sudo)
  - [Shell Completion](#shell-completion)
  - [Template Examples](#template-examples)
    - [Minimal Edge Device](#minimal-edge-device)
    - [Development Environment](#development-environment)
    - [Edge Microvisor Toolkit](#edge-microvisor-toolkit)
  - [Related Documentation](#related-documentation)

---

## Binary Location

The path to the `image-composer-tool` binary depends on how you built or
installed it:

| Build method | Binary path |
|-------------|-------------|
| `go build ./cmd/image-composer-tool` | `./image-composer-tool` |
| `earthly +build` | `./build/image-composer-tool` |
| Debian package | `image-composer-tool` (installed to `/usr/local/bin/`) |

The examples below use `./image-composer-tool` (the `go build` location).
Substitute the path that matches your setup.

## Commands Overview

```bash
image-composer-tool build         # Build an image from a template
image-composer-tool validate      # Validate a template without building
image-composer-tool inspect       # Inspect a raw image's structure
image-composer-tool compare       # Compare two images
image-composer-tool ai            # AI-powered template generation (RAG)
image-composer-tool cache clean   # Manage cached artifacts
image-composer-tool config        # Manage configuration (init, show)
image-composer-tool version       # Display version info
image-composer-tool --help        # Show all commands and options
```

For the full details on every command — including `inspect`, `compare`, and
`cache` — see the
[CLI Specification](../architecture/image-composer-tool-cli-specification.md#commands).

## Building an Image

> **ISO images require the `live-installer` binary.** Build it before starting
> an ISO build:
>
> ```bash
> go build -buildmode=pie -o ./build/live-installer ./cmd/live-installer
> ```
>
> If you use `earthly +build`, both binaries are built automatically. See the
> [Installation Guide](./installation.md) for details.

```bash
# go build — binary is in the repo root
sudo -E ./image-composer-tool build image-templates/azl3-x86_64-edge-raw.yml

# earthly +build — binary is in ./build/
sudo -E ./build/image-composer-tool build image-templates/azl3-x86_64-edge-raw.yml

# Debian package — binary is on PATH
sudo image-composer-tool build /usr/share/image-composer-tool/examples/azl3-x86_64-edge-raw.yml

# Override config settings with flags
sudo -E ./image-composer-tool build --workers 16 --cache-dir /tmp/cache image-templates/azl3-x86_64-edge-raw.yml

# Build from scratch in throwaway cache/workspace dirs (removed after the build)
sudo -E ./image-composer-tool build --no-cache image-templates/azl3-x86_64-edge-raw.yml
```

Common flags: `--workers`, `--cache-dir`, `--work-dir`, `--no-cache`, `--verbose`,
`--dotfile`, `--config`, `--log-level`.
See the full
[build flag reference](../architecture/image-composer-tool-cli-specification.md#build-command)
for descriptions and additional flags like `--system-packages-only`.

### Build Output

After the image finishes building, the output is placed under the configured
`work_dir`. The full path follows this pattern:

```
<work_dir>/<os>-<dist>-<arch>/imagebuild/<system-config-name>/
```

The default `work_dir` depends on how you installed the tool:

| Install method | Default `work_dir` | Example output path |
|----------------|-------------------|---------------------|
| Cloned repo | `./workspace` (relative to repo root) | `./workspace/azure-linux-azl3-x86_64/imagebuild/edge/` |
| Debian package | `/tmp/image-composer-tool` | `/tmp/image-composer-tool/azure-linux-azl3-x86_64/imagebuild/edge/` |

You can override it with `--work-dir` or by setting `work_dir` in your
configuration file.

## Building an Overlay Image

Overlay mode layers additional packages onto an **existing** Ubuntu RAW
baseline image instead of composing a new image from scratch. The baseline
you supply is never modified in place — it is copied into the build
workspace first. Overlay is strictly additive by default: packages may be
added, but not removed or downgraded, and any conflict fails the preflight
gate.

For the full design rationale, see
[ADR: Baseline Image Overlay](../../architecture-decision-record/adr-image-extension.md)
and
[ADR: Grow-Only Resize for Overlay Baselines](../../architecture-decision-record/adr-overlay-grow-resize.md).

### Prerequisites

- An existing baseline disk image (`raw`, `qcow2`, `vhd`, or `vhdx`) either
  on the local filesystem or reachable over `https://`.
- `qemu-img` installed on the build host when the baseline is not RAW
  (used to convert into RAW before mounting).
- Enough free disk space to hold a full copy of the baseline plus the
  overlaid packages.

### Example template

The repository ships a ready-to-use example at
[`image-templates/ubuntu24-x86_64-overlay-raw.yml`](../../../image-templates/ubuntu24-x86_64-overlay-raw.yml).
Edit `baseline.source.path` (or switch to `baseline.source.url:`) to point
at your own baseline before building.

Key fields:

```yaml
baseline:
  mode: overlay
  source:
    path: /path/to/ubuntu-24.04-base.img
    format: raw

overlayPolicy:
  packageOperation: additive-only
  conflictPolicy: fail

systemConfig:
  packages:
    - tree
    - jq
```

### Run the build

```bash
# Build using the path declared in the template
sudo -E ./image-composer-tool build image-templates/ubuntu24-x86_64-overlay-raw.yml

# Override the baseline path from the CLI (takes precedence over the template)
sudo -E ./image-composer-tool build \
  --baseline-image /images/ubuntu-24.04-base.img \
  image-templates/ubuntu24-x86_64-overlay-raw.yml

# Skip post-build inspection for a faster iteration loop
sudo -E ./image-composer-tool build --no-inspect image-templates/ubuntu24-x86_64-overlay-raw.yml
```

See the
[`--baseline-image` and `--no-inspect` flag reference](../architecture/image-composer-tool-cli-specification.md#build-command)
for full details.

### What overlay mode does and does not do

| Behavior | Overlay mode |
|----------|--------------|
| Add packages listed in `systemConfig.packages` | Yes (plus transitive deps) |
| Remove or downgrade packages already in the baseline | No — fails preflight |
| Rebuild the kernel or bootloader | No — baseline artifacts are preserved |
| Regenerate initramfs / bootloader config when required by installed packages | Yes |
| Grow the baseline image to a larger `disk.size` | Only when `overlayPolicy.allowDiskResize: true` (grow-only, never shrinks) |
| Modify the user-supplied baseline file in place | No — a copy is used |

### Verify the result

Compare the resulting overlay image against the baseline to confirm only
the expected changes landed:

```bash
image-composer-tool compare /images/ubuntu-24.04-base.img \
  workspace/ubuntu-ubuntu24-x86_64/imagebuild/overlay/ubuntu-overlay.raw
```

See the
[Compare Command reference](../architecture/image-composer-tool-cli-specification.md#compare-command)
for JSON output modes and SBOM diffs.

## Validating a Template

Check a template for errors before starting a build:

```bash
./image-composer-tool validate image-templates/azl3-x86_64-edge-raw.yml
```

## Configuration

The tool uses a layered configuration: config file values are overridden by
command-line flags. A config file is auto-discovered from several standard
locations (current directory, home directory, `/etc/`), or you can specify one
explicitly with `--config`.

```bash
# Create a default configuration file
./image-composer-tool config init

# Show the active configuration
./image-composer-tool config show

# Use a specific configuration file
./image-composer-tool --config /path/to/config.yaml build template.yml
```

Key settings:

| Setting | Default (cloned repo) | Default (Debian pkg) |
|---------|----------------------|----------------------|
| `workers` | 8 | 8 |
| `cache_dir` | `./cache` | `/var/cache/image-composer-tool` |
| `work_dir` | `./workspace` | `/tmp/image-composer-tool` |

For the complete search order and all configuration fields, see
[Configuration Files](../architecture/image-composer-tool-cli-specification.md#configuration-files)
in the CLI Specification.

## Operations Requiring Sudo

The `build` command requires `sudo` because it performs system-level
operations: creating loop devices, mounting filesystems, setting up chroot
environments, installing packages, and configuring bootloaders.

Always run builds with `sudo -E` to preserve your environment variables
(such as `$PATH` and proxy settings).

## Shell Completion

```bash
# Auto-detect shell and install completion
./image-composer-tool install-completion

# Or specify a shell: bash, zsh, fish, powershell
./image-composer-tool install-completion --shell bash
```

After installing, reload your shell configuration (e.g., `source ~/.bashrc`).
For per-shell activation steps and manual completion script generation, see the
[Install-Completion Command](../architecture/image-composer-tool-cli-specification.md#install-completion-command)
reference.

## Template Examples

Templates are YAML files that define the requirements for an image build.
For the full template system documentation, see
[Creating and Reusing Image Templates](../architecture/image-composer-tool-templates.md).

The `image-templates/` directory contains ready-to-use examples for all
supported distributions and image types.

### Minimal Edge Device

```yaml
image:
  name: minimal-edge
  version: "1.0.0"

target:
  os: azure-linux
  dist: azl3
  arch: x86_64
  imageType: raw

systemConfig:
  name: minimal
  description: Minimal edge device configuration
  packages:
    - openssh-server
    - ca-certificates
  kernel:
    version: "6.12"
    cmdline: "quiet"
```

### Development Environment

```yaml
image:
  name: dev-environment
  version: "1.0.0"

target:
  os: azure-linux
  dist: azl3
  arch: x86_64
  imageType: raw

systemConfig:
  name: development
  description: Development environment with tools
  packages:
    - openssh-server
    - git
    - docker-ce
    - vim
    - curl
    - wget
    - python3
  kernel:
    version: "6.12"
    cmdline: "quiet splash"
```

### Edge Microvisor Toolkit

```yaml
image:
  name: emt-edge-device
  version: "1.0.0"

target:
  os: edge-microvisor-toolkit
  dist: emt3
  arch: x86_64
  imageType: raw

systemConfig:
  name: edge
  description: Edge Microvisor Toolkit configuration
  packages:
    - cloud-init
    - rsyslog
  kernel:
    version: "6.12"
    cmdline: "console=ttyS0,115200 console=tty0 loglevel=7"
```

---

## Related Documentation

- [AI-Powered Template Generation](./ai-template-generation.md)
- [CLI Specification and Reference](../architecture/image-composer-tool-cli-specification.md)
- [Image Templates](../architecture/image-composer-tool-templates.md)
- [Build Process](../architecture/image-composer-tool-build-process.md)
- [Installation Guide](./installation.md)
- [Edge Microvisor Toolkit](https://docs.openedgeplatform.intel.com/2026.0/edge-microvisor-toolkit/index.html)
