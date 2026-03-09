# OS Image Composer

OS Image Composer is a command-line tool for building custom, bootable Linux
images from pre-built packages. Define your requirements in a YAML template,
run one command to get a RAW image ready to deploy (ISO installers require an extra step; see the Installation Guide).

**Supported distributions:** Azure Linux (azl3), Edge Microvisor Toolkit
(emt3), Wind River eLxr (elxr12), Ubuntu (ubuntu24), and Red Hat-compatible
distributions (rcd10).

## Quick Start

```bash
# 1. Clone and build (requires Go 1.24+)
git clone https://github.com/open-edge-platform/os-image-composer.git
cd os-image-composer
go build -buildmode=pie -ldflags "-s -w" ./cmd/os-image-composer

# 2. Install prerequisites
sudo apt install systemd-ukify mmdebstrap
# Or run it directly:
go run ./cmd/os-image-composer --help

# 3. Compose an image
sudo -E ./os-image-composer build image-templates/azl3-x86_64-edge-raw.yml
```

For build options (Earthly, Debian package) and prerequisite details, see the
[Installation Guide](./tutorial/installation.md).

## Guides

| Guide | Description |
|-------|-------------|
| [Installation Guide](./tutorial/installation.md) | Build methods, Debian packaging, prerequisites |
| [Usage Guide](./tutorial/usage-guide.md) | CLI commands, configuration, build output, shell completion |
| [CLI Reference](./architecture/os-image-composer-cli-specification.md) | Complete command-line specification |
| [Image Templates](./architecture/os-image-composer-templates.md) | Template structure, variables, best practices |
| [Build Process](./architecture/os-image-composer-build-process.md) | Pipeline stages, caching, troubleshooting |
| [Architecture](./architecture/architecture.md) | System design and component overview |

## Tutorials

| Tutorial | Description |
|----------|-------------|
| [Prerequisites](./tutorial/prerequisite.md) | Manual ukify and mmdebstrap installation |
| [Secure Boot](./tutorial/configure-secure-boot.md) | Configuring secure boot for images |
| [Configure Users](./tutorial/configure-image-user.md) | Adding users to images |
| [Custom Build Actions](./tutorial/configure-additional-actions-for-build.md) | Pre/post-build scripts |
| [Multiple Repos](./tutorial/configure-multiple-package-repositories.md) | Using multiple package repositories |

## Get Help

- Run `os-image-composer --help` (using the binary path from your install method)
- [Start a discussion](https://github.com/open-edge-platform/os-image-composer/discussions)
- [Troubleshoot build issues](./architecture/os-image-composer-build-process.md#troubleshooting-build-issues)

## Contribute

- [Open an issue](https://github.com/open-edge-platform/os-image-composer/issues)
- [Report a security vulnerability](https://github.com/open-edge-platform/os-image-composer/blob/main/SECURITY.md)
- [Submit a pull request](https://github.com/open-edge-platform/os-image-composer/pulls)

## License

[MIT](https://github.com/open-edge-platform/os-image-composer/blob/main/LICENSE)

<!--hide_directive
:::{toctree}
:hidden:

Installation Guide <tutorial/installation.md>
Prerequisites <tutorial/prerequisite.md>
Architecture <architecture/architecture.md>
Usage Guide <tutorial/usage-guide.md>
Secure Boot Configuration <tutorial/configure-secure-boot.md>
Configure Users <tutorial/configure-image-user.md>
Customize Image Build <tutorial/configure-additional-actions-for-build.md>
Configure Multiple Package Repositories <tutorial/configure-multiple-package-repositories.md>
release-notes.md

:::
hide_directive-->
