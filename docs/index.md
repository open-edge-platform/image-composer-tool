# ICT

ICT is a command-line tool for building custom, bootable Linux
images from pre-built packages. Define your requirements in a YAML template,
run one command to get a RAW image ready for deployment (ISO installers require an extra step; see the Installation Guide).

**Supported distributions:** Azure Linux (azl3),
[Edge Microvisor Toolkit](https://docs.openedgeplatform.intel.com/2026.0/edge-microvisor-toolkit/index.html)
(emt3), Wind River eLxr (elxr12), Ubuntu (ubuntu24), and Red Hat-compatible
distributions (rcd10).

## Quick Start

```bash
# 1. Clone and build (requires Go 1.24+)
git clone https://github.com/open-edge-platform/image-composer-tool.git
cd image-composer-tool
go build -buildmode=pie -ldflags "-s -w" ./cmd/image-composer-tool

# 2. Install prerequisites
sudo apt install systemd-ukify mmdebstrap
# Or run it directly:
go run ./cmd/image-composer-tool --help

# 3. Compose an image
sudo -E ./image-composer-tool build image-templates/azl3-x86_64-edge-raw.yml
```

For build options (Earthly, Debian package) and prerequisite details, see the
[Installation Guide](./tutorial/installation.md).

## Guides

| Guide | Description |
|-------|-------------|
| [Installation Guide](./tutorial/installation.md) | Build methods, Debian packaging, prerequisites |
| [Usage Guide](./tutorial/usage-guide.md) | CLI commands, configuration, build output, shell completion |
| [ARM64 Image Creation Guide](./tutorial/arm64-image-creation-guide.md) | Build Ubuntu, eLxr, and Azure Linux ARM64 images with GRUB or UKI |
| [CLI Reference](./architecture/image-composer-tool-cli-specification.md) | Complete command-line specification |
| [Image Templates](./architecture/image-composer-tool-templates.md) | Template structure, variables, best practices |
| [Build Process](./architecture/image-composer-tool-build-process.md) | Pipeline stages, caching, troubleshooting |
| [Architecture](./architecture.md) | System design and component overview |

## Tutorials

| Tutorial | Description |
|----------|-------------|
| [Prerequisites](./tutorial/prerequisite.md) | Manual ukify and mmdebstrap installation |
| [Secure Boot](./tutorial/configure-secure-boot.md) | Configuring secure boot for images |
| [Configure Users](./tutorial/configure-image-user.md) | Adding users to images |
| [Custom Build Actions](./tutorial/configure-additional-actions-for-build.md) | Pre/post-build scripts |
| [Multiple Repos](./tutorial/configure-multiple-package-repositories.md) | Using multiple package repositories |

## Get Help

- Run `image-composer-tool --help` (using the binary path from your install method)
- [Start a discussion](https://github.com/open-edge-platform/image-composer-tool/discussions)
- [Troubleshoot build issues](./architecture/image-composer-tool-build-process.md#troubleshooting-build-issues)

## Contribute

- [Open an issue](https://github.com/open-edge-platform/image-composer-tool/issues)
- [Report a security vulnerability](https://github.com/open-edge-platform/image-composer-tool/blob/main/SECURITY.md)
- [Submit a pull request](https://github.com/open-edge-platform/image-composer-tool/pulls)

## License

[MIT](https://github.com/open-edge-platform/image-composer-tool/blob/main/LICENSE)

<!--hide_directive
:::{toctree}
:hidden:

Installation Guide <./tutorial/installation.md>
Prerequisites <./tutorial/prerequisite.md>
Architecture <./architecture.md>
Usage Guide <./tutorial/usage-guide.md>
ARM64 Image Creation Guide <./tutorial/arm64-image-creation-guide.md>
Secure Boot Configuration <./tutorial/configure-secure-boot.md>
Configure Users <./tutorial/configure-image-user.md>
Customize Image Build <./tutorial/configure-additional-actions-for-build.md>
Configure Multiple Package Repositories <./tutorial/configure-multiple-package-repositories.md>
AI Template Generation (RAG) <./tutorial/ai-template-generation.md>
release-notes.md

:::
hide_directive-->
