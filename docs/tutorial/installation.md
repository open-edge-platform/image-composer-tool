# Installation Guide

This guide covers all the ways to install ICT and its
prerequisites.

## Table of Contents

- [Installation Guide](#installation-guide)
  - [Table of Contents](#table-of-contents)
  - [Prerequisites](#prerequisites)
  - [Development Build (Go)](#development-build-go)
    - [Including Version Information in Dev Builds](#including-version-information-in-dev-builds)
    - [VS Code MCP Servers (Optional)](#vs-code-mcp-servers-optional)
  - [Production Build (Earthly)](#production-build-earthly)
  - [Install via Debian Package](#install-via-debian-package)
    - [Build the Debian Package](#build-the-debian-package)
    - [Install the Package](#install-the-package)
    - [Verify Installation](#verify-installation)
    - [Package Contents](#package-contents)
    - [Package Dependencies](#package-dependencies)
    - [Uninstall](#uninstall)
  - [Image Composition Prerequisites](#image-composition-prerequisites)
    - [ukify](#ukify)
    - [mmdebstrap](#mmdebstrap)
  - [Next Steps](#next-steps)

---

## Prerequisites

- **OS**: Ubuntu 24.04 (recommended; other standard Linux distributions may
  work but are not validated)
- **Go**: Version 1.24.0 or later — see
  [Go installation instructions](https://go.dev/doc/manage-install)

## Development Build (Go)

For development and testing purposes, build directly with Go.
The binary is placed in the **repo root** as `./image-composer-tool`.

```bash
# Build the tool (output: ./image-composer-tool)
go build -buildmode=pie -ldflags "-s -w" ./cmd/image-composer-tool

# Build the live-installer (required for ISO images)
go build -buildmode=pie -o ./build/live-installer -ldflags "-s -w" ./cmd/live-installer

# Or run without building
go run ./cmd/image-composer-tool --help
```

> **Note:** Development builds show default version information (e.g.,
> `Version: 0.1.0`, `Build Date: unknown`). This is expected.

### Including Version Information in Dev Builds

To embed version metadata via ldflags:

```bash
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE=$(date -u '+%Y-%m-%d')

go build -buildmode=pie \
  -ldflags "-s -w \
    -X 'github.com/open-edge-platform/image-composer-tool/internal/config/version.Version=$VERSION' \
    -X 'github.com/open-edge-platform/image-composer-tool/internal/config/version.Toolname=Image-Composer' \
    -X 'github.com/open-edge-platform/image-composer-tool/internal/config/version.Organization=Open Edge Platform' \
    -X 'github.com/open-edge-platform/image-composer-tool/internal/config/version.BuildDate=$BUILD_DATE' \
    -X 'github.com/open-edge-platform/image-composer-tool/internal/config/version.CommitSHA=$COMMIT'" \
  ./cmd/image-composer-tool

# Required for ISO images
go build -buildmode=pie \
  -o ./build/live-installer \
  -ldflags "-s -w \
    -X 'github.com/open-edge-platform/image-composer-tool/internal/config/version.Version=$VERSION' \
    -X 'github.com/open-edge-platform/image-composer-tool/internal/config/version.Toolname=Image-Composer' \
    -X 'github.com/open-edge-platform/image-composer-tool/internal/config/version.Organization=Open Edge Platform' \
    -X 'github.com/open-edge-platform/image-composer-tool/internal/config/version.BuildDate=$BUILD_DATE' \
    -X 'github.com/open-edge-platform/image-composer-tool/internal/config/version.CommitSHA=$COMMIT'" \
  ./cmd/live-installer
```

### VS Code MCP Servers (Optional)

This repository includes `.vscode/mcp.json` which configures
[MCP servers](https://code.visualstudio.com/docs/copilot/chat/mcp-servers) for
AI-assisted development with GitHub Copilot. These are **optional** — everything
works without them.

| Requirement | What it provides | Install |
|-------------|-----------------|---------|
| Go 1.24+ with `gopls` | Go workspace tools (diagnostics, search, references) | VS Code Go extension installs `gopls` automatically |
| Node.js 18+ with `npx` | GitHub, fetch, context7, sequential-thinking, memory servers | [nodejs.org](https://nodejs.org/) |
| GitHub PAT (optional) | GitHub MCP server authentication | [Creating a PAT](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens) |

If a server's prerequisites are not met, VS Code will skip that server with no
impact on normal development.

## Production Build (Earthly)

For reproducible production builds with automatic version injection.
The binary is placed in `./build/image-composer-tool`.

```bash
# Default build (output: ./build/image-composer-tool)
earthly +build

# Build with custom version metadata
earthly +build --VERSION=1.2.0
```

## Install via Debian Package

For Ubuntu and Debian systems, ICT can be installed as a `.deb`
package with proper package management.

### Build the Debian Package

```bash
# Build with default parameters (latest git tag, amd64)
earthly +deb

# Build with custom version
earthly +deb --VERSION=1.2.0
```

The package is created in the `dist/` directory as
`ict_<VERSION>_<ARCH>.deb`.

### Install the Package

```bash
# Recommended — automatically resolves dependencies
sudo apt install <PATH>/ict_1.0.0_amd64.deb
```

Alternatively, using `dpkg`:

```bash
sudo apt-get update
sudo apt-get install -y bash coreutils unzip dosfstools xorriso grub-common
sudo dpkg -i dist/ict_1.0.0_amd64.deb
# Optional bootstrap tools:
sudo apt-get install -y mmdebstrap || sudo apt-get install -y debootstrap
```

> **Tip:** If `dpkg -i` reports dependency errors, run
> `sudo apt-get install -f` to resolve them.

### Verify Installation

```bash
dpkg -l | grep image-composer-tool
image-composer-tool version
```

### Package Contents

| Path | Description |
|------|-------------|
| `/usr/local/bin/image-composer-tool` | Main executable |
| `/etc/image-composer-tool/config.yml` | Global configuration |
| `/etc/image-composer-tool/config/` | OS variant configuration files |
| `/usr/share/image-composer-tool/examples/` | Sample image templates |
| `/usr/share/doc/image-composer-tool/` | README, LICENSE, CLI specification |
| `/var/cache/image-composer-tool/` | Package cache storage |

### Package Dependencies

**Required:**

- `bash`, `coreutils`, `unzip`, `dosfstools`, `xorriso`, `grub-common`

**Recommended (installed if available):**

- `mmdebstrap` (preferred, version 1.4.3+ required)
- `debootstrap` (alternative)

> **Important:** `mmdebstrap` version 0.8.x (included in Ubuntu 22.04) has
> known issues. For Ubuntu 22.04, install version 1.4.3+ manually — see
> [mmdebstrap instructions](./prerequisite.md#mmdebstrap).

### Uninstall

```bash
# Remove package (keeps config files)
sudo dpkg -r image-composer-tool

# Remove package and config files
sudo dpkg --purge image-composer-tool
```

## Image Composition Prerequisites

Before composing images, install these additional tools:

### ukify

Combines kernel, initrd, and UEFI boot stub to create signed Unified Kernel
Images (UKI).

- **Ubuntu 23.04+**: `sudo apt install systemd-ukify`
- **Ubuntu 22.04 and earlier**: Install manually from systemd source — see
  [ukify installation instructions](./prerequisite.md#ukify)

### mmdebstrap

Downloads and installs Debian packages to initialize a chroot.

- **Ubuntu 23.04+**: Available in system repositories (version 1.4.3+)
- **Ubuntu 22.04**: The repository version (0.8.x) will not work — install
  1.4.3+ manually per the
  [mmdebstrap instructions](./prerequisite.md#mmdebstrap)
- **Alternative**: `debootstrap` can be used for Debian-based images

---

## Next Steps

- [Quick Start](../../README.md#quick-start) — build your first image
- [Usage Guide](./usage-guide.md) — CLI commands, configuration, and
  shell completion
- [Image Templates](../architecture/image-composer-tool-templates.md) —
  creating and reusing templates
