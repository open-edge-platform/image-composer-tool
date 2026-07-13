# Image Composer Tool

<!--hide_directive
<div class="component_card_widget">
  <a class="icon_github" href="https://github.com/open-edge-platform/image-composer-tool">
     GitHub
  </a>
  <a class="icon_document" href="https://github.com/open-edge-platform/image-composer-tool/blob/main/README.md">
     Readme
  </a>
</div>
hide_directive-->

Image Composer Tool (ICT) is a command-line tool for building custom, bootable Linux
images from pre-built packages. Define your requirements in a YAML template,
run one command to get a RAW image ready for deployment (ISO installers require an extra step; see the Installation Guide).

**Supported distributions:** Azure Linux (azl3),
[Edge Microvisor Toolkit](https://docs.openedgeplatform.intel.com/2026.0/edge-microvisor-toolkit/index.html)
(emt3), Wind River eLxr (elxr12), Ubuntu (ubuntu24), and Red Hat-compatible
distributions (rcd10).

## Quick Start

See the [Quick Start guide](./get-started/quick-start.md) to build your first image in three steps.

## Guides

| Guide                                                                    | Description                                                 |
| ------------------------------------------------------------------------ | ----------------------------------------------------------- |
| [Installation Guide](./get-started/installation.md)                         | Build methods, Debian packaging, prerequisites              |
| [Usage Guide](./get-started/usage-guide.md)                                 | CLI commands, configuration, build output, shell completion |
| [CLI Reference](./architecture/image-composer-tool-cli-specification.md) | Complete command-line specification                         |
| [Image Templates](./architecture/image-composer-tool-templates.md)       | Template structure, variables, best practices               |
| [Build Process](./architecture/image-composer-tool-build-process.md)     | Pipeline stages, caching, troubleshooting                   |
| [Architecture](./architecture/architecture.md)                                        | System design and component overview                        |

## Tutorials

| Tutorial                                                                     | Description                              |
| ---------------------------------------------------------------------------- | ---------------------------------------- |
| [Prerequisites](./get-started/prerequisites.md)                                  | Manual ukify and mmdebstrap installation |
| [Secure Boot](./configuration/configure-secure-boot.md)                           | Configuring secure boot for images       |
| [Configure Users](./configuration/configure-image-user.md)                        | Adding users to images                   |
| [Custom Build Actions](./configuration/configure-additional-actions-for-build.md) | Commands during image compose (chroot)   |
| [Custom Initrd Script](./configuration/configure-custom-initrd-script.md)       | Debian 13 GRUB initramfs-tools initrd hook |
| [Multiple Repos](./configuration/configure-multiple-package-repositories.md)      | Using multiple package repositories      |

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
:caption: Get Started

Quick Start <./get-started/quick-start.md>
Installation <./get-started/installation.md>
Prerequisites <./get-started/prerequisites.md>
Usage Guide <./get-started/usage-guide.md>
AI Template Generation (RAG) <./get-started/ai-template-generation.md>

:::

:::{toctree}
:hidden:
:caption: Configuration

Secure Boot Configuration <./configuration/configure-secure-boot.md>
Configure Users <./configuration/configure-image-user.md>
Customize Image Build <./configuration/configure-additional-actions-for-build.md>
Configure Custom Initrd Script <./configuration/configure-custom-initrd-script.md>
Configure Multiple Package Repositories <./configuration/configure-multiple-package-repositories.md>

:::

:::{toctree}
:hidden:
:caption: Architecture

Architecture Overview <./architecture/architecture.md>
CLI Specification <./architecture/image-composer-tool-cli-specification.md>
Security Objectives <./architecture/image-composition-tool-security-objectives.md>
Build Process <./architecture/image-composer-tool-build-process.md>
Image Manifest Specification <./architecture/image-manifest-specification.md>
Coding Style Guide <./architecture/image-composer-tool-coding-style.md>
Caching <./architecture/image-composer-tool-caching.md>
Multiple Package Repo Support <./architecture/image-composer-tool-multi-repo-support.md>
Image Template Reference <./architecture/image-composer-tool-templates.md>

:::

:::{toctree}
:hidden:
:caption: --------------------

Release Notes <./release-notes.md>

:::
hide_directive-->