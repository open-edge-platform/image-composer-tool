# OS Image Composer Architecture

## Table of Contents

- [OS Image Composer Architecture](#os-image-composer-architecture)
  - [Table of Contents](#table-of-contents)
  - [Overview](#overview)
  - [OS Image Composer System Network Context](#os-image-composer-system-network-context)
    - [Network Security Considerations](#network-security-considerations)
    - [Package Sign Verification](#package-sign-verification)
  - [Components Overview](#components-overview)
    - [Provider](#provider)
    - [Chroot](#chroot)
    - [Image](#image)
    - [Config](#config)
    - [OsPackage](#ospackage)
  - [Build Process Flow](#build-process-flow)
  - [Related Documentation](#related-documentation)

## Overview

The OS Image Composer is a tool for creating customized OS images from pre-built packages. It takes an image template file (YAML) as input and produces bootable OS images in raw or ISO formats suitable for deployment on bare metal systems, virtual machines, and edge devices.

The tool uses a layered configuration approach: OS-specific default templates provide base settings for supported distributions (Azure Linux, Edge Microvisor Toolkit, Wind River eLxr, Ubuntu, and Red Hat-compatible distributions), which are merged with user-provided image templates to generate the final image specification. This approach simplifies the process by handling OS-specific details automatically while allowing full customization when needed.

Pre-built packages are fetched securely from distribution-specific remote repositories over HTTPS, with automatic dependency resolution and GPG signature verification. The tool maintains local caches for both packages and reusable chroot environments to optimize build performance across multiple image builds.

The following diagram shows the input and output of the OS Image Composer tool:

![Overview](assets/overview.drawio.svg)

## OS Image Composer System Network Context

The following diagram shows the network context of the OS Image Composer tool:

![OS Image Composer Network Diagram](assets/os-image-composer-network-diagram.drawio.svg)

The diagram illustrates how different components of the product's system architecture communicate with each other.

### Network Security Considerations

The OS Image Composer tool downloads required packages using HTTP requests to the distribution-specific package repositories over TLS 1.2+ connections. Each of the package repositories does server-side validation on the package download requests, so it is expected that the system running the OS Image Composer tool is provisioned with a CA root chain.

### Package Sign Verification

When packages are downloaded, they are verified for integrity by using the GPG public keys and SHA256/MD5 checksum published at the package repositories.

## Components Overview

The following diagram outlines the high-level components of the OS Image Composer tool:

![components high level view](assets/components.drawio.svg)

The tools for composing an image are grouped under the following components: **Provider**, **Chroot**, **Image**, **OsPackage**, and **Config**. For modularity, each group contains a set of components for the OS Image Composer tool's functions.

The **Provider** component takes data from **Config** as its input, then orchestrates **Chroot**, **Image**, and **OsPackage** components to build the image. **Chroot** libraries are used to create an isolated chroot environment for building the OS image. The **Image** libraries provide general functions for building OS images. The **OsPackage** libraries include utilities for handling Debian and RPM packages. The **Config** component contains configuration data for the image that will be created.

### Provider

The Provider component is the orchestrator of the image build process. Each supported operating system (Azure Linux, EMT, eLxr, Ubuntu, RCD) has its own provider implementation that understands the specific requirements and package management for that OS.

**Provider Interface:**
- `Init(dist, arch string)` - Initialize the provider with distribution and architecture
- `PreProcess(template *ImageTemplate)` - Validate template and prepare environment
- `BuildImage(template *ImageTemplate)` - Execute the complete build process
- `PostProcess(template *ImageTemplate, buildErr error)` - Cleanup and finalization

The provider encapsulates all OS-specific logic while maintaining a consistent interface for the build command to use.

**Supported Providers:**
- **Azure Linux** (azl3) - RPM-based distribution
- **Edge Microvisor Toolkit** (emt3) - Specialized edge OS
- **eLxr** (elxr12) - Wind River embedded Linux
- **Ubuntu** (ubuntu24) - Debian-based distribution
- **RCD** (rcd10) - Red Hat-compatible distributions

### Chroot

The OS Image Composer tool generates a `chroot` environment, which is used for the image composition and creation process, isolated from the host operating system's file system. The chroot environment is reused across builds for the same provider, while packages are fetched and cached locally.

**Key Responsibilities:**
- Create and manage isolated chroot environments
- Mount necessary filesystems (proc, sys, dev)
- Install packages within the chroot
- Execute scripts in the chroot environment
- Clean up chroot resources after build

**Chroot Reuse:** The chroot environment is created once per provider and reused across multiple image builds. This significantly improves build performance by avoiding the overhead of recreating the base environment.

The chroot environment provides a clean, reproducible build environment that ensures the image composition process doesn't interfere with or depend on the host system's configuration.

### Image

![components - image](assets/components.drawio.image.svg)

The Image component groups the libraries that generate the final image output. It creates raw disk images or ISO images according to an image template file.

**Subcomponents:**

- **RawMaker** - Creates raw disk images with partitions and filesystems
- **ISOmaker** - Creates bootable ISO images
- **ImageDisc** - Manages disk partitioning and formatting
- **ImageBoot** - Installs and configures bootloaders (GRUB, systemd-boot)
- **ImageOs** - Configures the OS environment (users, network, services)
- **ImageSecure** - Applies security configurations (SELinux, dm-verity, secure boot)
- **ImageSign** - Handles image signing for integrity verification
- **ImageConvert** - Converts images between formats
- **InitrdMaker** - Generates initial ramdisk images

**Supported Output Formats:**

- **raw** - Raw disk image with partitions (for bare metal, VMs, cloud). Can be converted to vhd, vhdx, qcow2, vmdk, vdi formats.
- **iso** - Bootable ISO image (for installation media, live systems)

### Config

![components - Config](assets/components.drawio.config.svg)

The **Config** component contains configuration data for the image that will be created. It serves as input data for the **Provider**, which builds the OS image according to that data.

**Configuration Types:**

1. **Global Configuration** (Default: `os-image-composer/config.yml`)
   - System-wide settings (cache directories, worker count, logging)
   - Applies to all builds on the system

2. **Default Templates** (`config/osv/{os-name}/{dist}/imageconfigs/defaultconfigs/`)
   - OS and architecture-specific default templates provided with the tool
   - `default-raw-{arch}.yml` - Default configuration for raw images
   - `default-iso-{arch}.yml` - Default configuration for ISO images
   - `default-initrd-{arch}.yml` - Default configuration for initrd images
   - Example locations in repository:
     - `config/osv/azure-linux/azl3/imageconfigs/defaultconfigs/`
     - `config/osv/edge-microvisor-toolkit/emt3/imageconfigs/defaultconfigs/`
     - `config/osv/wind-river-elxr/elxr12/imageconfigs/defaultconfigs/`

3. **Image Templates** (Sample templatesat `image-templates/` , user-provided)
   - Per-image build specifications created by users
   - Define packages, partitions, bootloader, security settings
   - Merged with OS-specific default templates during build

The configuration system uses a layered approach: OS-specific default templates (from repository) provide base settings, user image templates override specific values, and command-line flags override both, providing flexibility while maintaining sensible defaults.

### OsPackage

![components - package](assets/components.drawio.OsPackage.svg)

The **OsPackage** component groups the libraries that provide the unified interface to operating system vendors' remote package repositories. It analyzes given package lists and downloads all the packages and dependencies from the target operating system's remote package repository to a local cache.

**Key Functions:**

- Parse and analyze package repository metadata
- Resolve package dependencies recursively
- Download packages from remote repositories
- Verify package signatures and integrity
- Cache packages locally for reuse
- Provide unified interface for both RPM and DEB package systems

It verifies signatures of the downloaded packages to ensure they are authenticated and from a certified source. It also provides the unified interface to install the packages and the dependencies in the correct order into the image rootfs directory.

## Build Process Flow

The following diagram illustrates the overall image composition workflow:

![Image composition workflow](./assets/image.composition.workflow.drawio.svg)

The build process follows these high-level steps:

1. **Load and Validate Template** - Parse the image template and merge with default templates, validate against schema
2. **Initialize Provider** - Select and initialize the appropriate provider for the target OS
3. **PreProcess** - Provider validates requirements and prepares the build environment
4. **BuildImage** - Provider executes the complete image build pipeline:
   - Set up or reuse chroot environment
   - Download and cache packages
   - Create disk image with partitions (for raw) or ISO structure
   - Install packages in chroot
   - Configure OS settings
   - Install bootloader
   - Apply security configurations
   - Generate output format (raw or ISO)
5. **PostProcess** - Provider performs cleanup and generates SBOM

## Related Documentation

- [Understanding the Build Process](./os-image-composer-build-process.md) - Detailed explanation of build stages
- [Understanding Caching](./os-image-composer-caching.md) - How package and chroot caching work
- [Understanding Templates](./os-image-composer-templates.md) - How to create and use image templates
- [Multiple Package Repository Support](./os-image-composer-multi-repo-support.md) - Adding custom package repositories
- [OS Image Composer CLI Reference](./os-image-composer-cli-specification.md) - Complete CLI documentation

<!--hide_directive
:::{toctree}
:hidden:

CLI Specification <os-image-composer-cli-specification.md>
Security Objectives <image-composition-tool-security-objectives.md>
Build Process <os-image-composer-build-process.md>
image-manifest-specification.md
Coding Style Guide <os-image-composer-coding-style.md>
Caching in OS Image Composer <os-image-composer-caching.md>
Multiple-package Repo Support <os-image-composer-multi-repo-support.md>
Using Templates <os-image-composer-templates.md>
Dependency Graph Analyzer <adr-dep-analyzer.md>
Image Inspection and Comparison <adr-image-inspect.md>
Template-Enriched RAG <adr-template-enriched-rag.md>

:::
hide_directive-->
