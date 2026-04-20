# ICT

[![License](https://img.shields.io/badge/License-MIT-blue.svg)](./LICENSE)
[![Go Lint Check](https://github.com/open-edge-platform/ict/actions/workflows/go-lint.yml/badge.svg)](https://github.com/open-edge-platform/ict/actions/workflows/go-lint.yml) [![Unit and Coverage](https://github.com/open-edge-platform/ict/actions/workflows/unit-test-and-coverage-gate.yml/badge.svg)](https://github.com/open-edge-platform/ict/actions/workflows/unit-test-and-coverage-gate.yml) [![Security zizmor 🌈](https://github.com/open-edge-platform/ict/actions/workflows/zizmor.yml/badge.svg)](https://github.com/open-edge-platform/ict/actions/workflows/zizmor.yml) [![Fuzz test](https://github.com/open-edge-platform/ict/actions/workflows/fuzz-test.yml/badge.svg)](https://github.com/open-edge-platform/ict/actions/workflows/fuzz-test.yml) [![Trivy scan](https://github.com/open-edge-platform/ict/actions/workflows/trivy-scan.yml/badge.svg)](https://github.com/open-edge-platform/ict/actions/workflows/trivy-scan.yml)

A command-line tool for building custom Linux images from pre-built packages. Define what you need in a YAML template, run one command, and get a bootable RAW or ISO image.

## How It Works

![ICT Build Flow](./docs/architecture/assets/ict-build-flow.svg)

The diagram above illustrates the overall build flow and may show representative or planned provider/architecture
combinations. The table below lists the OS and architecture combinations that are currently supported.
## Supported Distributions

| OS | Distribution | Architecture |
|----|-------------|--------------|
| Azure Linux | azl3 | x86_64 |
| Edge Microvisor Toolkit | emt3 | x86_64 |
| Red Hat Compatible Distro | rcd10 | x86_64 |
| Wind River eLxr | elxr12 | x86_64 |
| Ubuntu | ubuntu24 | x86_64 |
| Ubuntu | ubuntu26 | x86_64 |

## Quick Start

### 1. Build the Tool

Requires Go 1.24+.
Recommended Ubuntu 24.04

```bash
git clone https://github.com/open-edge-platform/ict.git
cd ict
go build -buildmode=pie -ldflags "-s -w" ./cmd/ict
```

This produces `./ict` in the repo root.

Alternatively, use [Earthly](https://earthly.dev/) for reproducible production
builds (output: `./build/ict`):

```bash
earthly +build
```

See the [Installation Guide](./docs/tutorial/installation.md) for Debian
package installation, prerequisite setup, and other options.

### 2. Install Prerequisites

```bash
# Required for image composition
sudo apt install systemd-ukify mmdebstrap
```

> Specific versions and alternative installation methods are documented in the
> [Installation Guide](./docs/tutorial/installation.md#image-composition-prerequisites).

### 3. Compose an Image

```bash
# If built with go build:
sudo -E ./ict build image-templates/azl3-x86_64-edge-raw.yml

# If built with earthly:
sudo -E ./build/ict build image-templates/azl3-x86_64-edge-raw.yml
```

That's it. The output image will be under
`./workspace/<os>-<dist>-<arch>/imagebuild/<config-name>/`
(or `/tmp/ict/...` if installed via the Debian package).

### 4. Validate a Template (Optional)

Check a template for errors before starting a build:

```bash
./ict validate image-templates/azl3-x86_64-edge-raw.yml
```

## Image Templates

Templates are YAML files that define what goes into your image — OS, packages,
kernel, partitioning, and more. Ready-to-use examples are in
[`image-templates/`](./image-templates/).

A minimal template looks like this:

```yaml
image:
  name: my-edge-device
  version: "1.0.0"

target:
  os: azure-linux
  dist: azl3
  arch: x86_64
  imageType: raw

systemConfig:
  name: edge
  packages:
    - openssh-server
    - ca-certificates
  kernel:
      version: "6.12"
      cmdline: "quiet"
```

To learn about template structure, variable substitution, and best practices,
see [Creating and Reusing Image Templates](./docs/architecture/ict-templates.md).

## Documentation

| Guide | Description |
|-------|-------------|
| [Installation Guide](./docs/tutorial/installation.md) | Build options, Debian packaging, prerequisites |
| [Usage Guide](./docs/tutorial/usage-guide.md) | CLI commands, configuration, shell completion |
| [CLI Reference](./docs/architecture/ict-cli-specification.md) | Complete command-line specification |
| [Image Templates](./docs/architecture/ict-templates.md) | Template system and customization |
| [Build Process](./docs/architecture/ict-build-process.md) | How image composition works, troubleshooting |
| [Secure Boot](./docs/tutorial/configure-secure-boot.md) | Configuring secure boot for images |
| [Multiple Repos](./docs/architecture/ict-multi-repo-support.md) | Using multiple package repositories |

## Get Help

- Run `./ict --help`
- [Browse the documentation](https://docs.openedgeplatform.intel.com/2026.0/ict/index.html)
- [Start a discussion](https://github.com/open-edge-platform/ict/discussions)
- [Troubleshoot build issues](./docs/architecture/ict-build-process.md#troubleshooting-build-issues)

## Contribute

- [Open an issue](https://github.com/open-edge-platform/ict/issues)
- [Report a security vulnerability](./SECURITY.md)
- [Submit a pull request](https://github.com/open-edge-platform/ict/pulls)

## License

[MIT](./LICENSE)
