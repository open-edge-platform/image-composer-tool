# Usage Guide

A practical guide for common ICT workflows. For the complete
command reference, see the
[CLI Specification](../architecture/ict-cli-specification.md).

## Table of Contents

- [Usage Guide](#usage-guide)
  - [Table of Contents](#table-of-contents)
  - [Binary Location](#binary-location)
  - [Commands Overview](#commands-overview)
  - [Building an Image](#building-an-image)
    - [Build Output](#build-output)
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

The path to the `ict` binary depends on how you built or
installed it:

| Build method | Binary path |
|-------------|-------------|
| `go build ./cmd/ict` | `./ict` |
| `earthly +build` | `./build/ict` |
| Debian package | `ict` (installed to `/usr/local/bin/`) |

The examples below use `./ict` (the `go build` location).
Substitute the path that matches your setup.

## Commands Overview

```bash
ict build         # Build an image from a template
ict validate      # Validate a template without building
ict inspect       # Inspect a raw image's structure
ict compare       # Compare two images
ict ai            # AI-powered template generation (RAG)
ict cache clean   # Manage cached artifacts
ict config        # Manage configuration (init, show)
ict version       # Display version info
ict --help        # Show all commands and options
```

For the full details on every command — including `inspect`, `compare`, and
`cache` — see the
[CLI Specification](../architecture/ict-cli-specification.md#commands).

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
sudo -E ./ict build image-templates/azl3-x86_64-edge-raw.yml

# earthly +build — binary is in ./build/
sudo -E ./build/ict build image-templates/azl3-x86_64-edge-raw.yml

# Debian package — binary is on PATH
sudo ict build /usr/share/ict/examples/azl3-x86_64-edge-raw.yml

# Override config settings with flags
sudo -E ./ict build --workers 16 --cache-dir /tmp/cache image-templates/azl3-x86_64-edge-raw.yml
```

Common flags: `--workers`, `--cache-dir`, `--work-dir`, `--verbose`,
`--dotfile`, `--config`, `--log-level`.
See the full
[build flag reference](../architecture/ict-cli-specification.md#build-command)
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
| Debian package | `/tmp/ict` | `/tmp/ict/azure-linux-azl3-x86_64/imagebuild/edge/` |

You can override it with `--work-dir` or by setting `work_dir` in your
configuration file.

## Validating a Template

Check a template for errors before starting a build:

```bash
./ict validate image-templates/azl3-x86_64-edge-raw.yml
```

## Configuration

The tool uses a layered configuration: config file values are overridden by
command-line flags. A config file is auto-discovered from several standard
locations (current directory, home directory, `/etc/`), or you can specify one
explicitly with `--config`.

```bash
# Create a default configuration file
./ict config init

# Show the active configuration
./ict config show

# Use a specific configuration file
./ict --config /path/to/config.yaml build template.yml
```

Key settings:

| Setting | Default (cloned repo) | Default (Debian pkg) |
|---------|----------------------|----------------------|
| `workers` | 8 | 8 |
| `cache_dir` | `./cache` | `/var/cache/ict` |
| `work_dir` | `./workspace` | `/tmp/ict` |

For the complete search order and all configuration fields, see
[Configuration Files](../architecture/ict-cli-specification.md#configuration-files)
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
./ict install-completion

# Or specify a shell: bash, zsh, fish, powershell
./ict install-completion --shell bash
```

After installing, reload your shell configuration (e.g., `source ~/.bashrc`).
For per-shell activation steps and manual completion script generation, see the
[Install-Completion Command](../architecture/ict-cli-specification.md#install-completion-command)
reference.

## Template Examples

Templates are YAML files that define the requirements for an image build.
For the full template system documentation, see
[Creating and Reusing Image Templates](../architecture/ict-templates.md).

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
- [CLI Specification and Reference](../architecture/ict-cli-specification.md)
- [Image Templates](../architecture/ict-templates.md)
- [Build Process](../architecture/ict-build-process.md)
- [Installation Guide](./installation.md)
- [Edge Microvisor Toolkit](https://docs.openedgeplatform.intel.com/2026.0/edge-microvisor-toolkit/index.html)
