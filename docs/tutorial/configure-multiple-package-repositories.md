# Configure Multiple Package Repositories

## Overview

The OS Image Composer supports adding multiple custom package repositories to your image builds through the `packageRepositories` section in image template files. This feature allows you to include packages from additional repositories beyond the default OS repositories, enabling you to integrate specialized software, proprietary packages, or packages from specific vendors into your custom images.

## How It Works

Package repositories are configured during the image build process and are added to the package manager configuration before any packages are installed. This ensures that packages from custom repositories are available during the package installation phase, allowing you to install packages from multiple sources in a single build.

## Configuration Structure

The `packageRepositories` section should be placed at the root level of your image template YAML file, alongside other top-level configuration sections:

```yaml
image:
  name: your-image-name
  version: "1.0"

target:
  os: ubuntu
  dist: ubuntu24
  arch: x86_64
  imageType: raw

# Package repositories are configured before any other operations
packageRepositories:
  - codename: "EdgeAI"
    url: "https://yum.repos.intel.com/edgeai/"
    pkey: "https://yum.repos.intel.com/edgeai/GPG-PUB-KEY-INTEL-DLS.gpg"

disk:
  name: ....
  # .... other disk configuration

systemConfig:
  name: ....
  packages:
    - ubuntu-minimal
    - edge-ai-package    # This package comes from the EdgeAI repository
    # .... other packages
```

## Repository Configuration Properties

Each repository entry supports the following properties:

- **codename** (required): A unique identifier for the repository
- **url** (required): The base URL of the package repository
- **pkey** (strongly recommended): URL to the GPG public key for repository authentication. Technically this field is optional, but omitting it (or using `[trusted=yes]` to bypass GPG verification) should be limited to explicitly trusted internal repositories, as it disables signature verification and reduces security.

## Complete Template Structure

Here's how the packageRepositories section fits within a complete image template:

```yaml
image:
  name: multi-repo-image
  version: "1.0"

target:
  os: ubuntu
  dist: ubuntu24
  arch: x86_64
  imageType: raw

# Multiple package repositories configuration
packageRepositories:
  - codename: "EdgeAI"
    url: "https://yum.repos.intel.com/edgeai/"
    pkey: "https://yum.repos.intel.com/edgeai/GPG-PUB-KEY-INTEL-DLS.gpg"

  - codename: "edge-base"
    url: "https://files-rs.edgeorchestration.intel.com/files-edge-orch/microvisor/rpms/3.0/base"
    pkey: "https://raw.githubusercontent.com/open-edge-platform/edge-microvisor-toolkit/refs/heads/3.0/SPECS/edge-repos/INTEL-RPM-GPG-KEY"

disk:
  name: ....
  # .... disk configuration

systemConfig:
  name: ....
  description: ....

  packages:
    - ubuntu-minimal
    - openvino-toolkit     # From OpenVINO repository
    - edge-ai-runtime      # From EdgeAI repository
    - microvisor-base      # From edge-base repository
    # .... other packages from various repositories

  kernel:
    version: ....
    # .... kernel configuration

  configurations:
    # .... custom configurations
```

## Real-World Example

A comprehensive multi-repository configuration for edge computing and AI workloads:

```yaml
packageRepositories:
  - codename: "EdgeAI"
    url: "https://yum.repos.intel.com/edgeai/"
    pkey: "https://yum.repos.intel.com/edgeai/GPG-PUB-KEY-INTEL-DLS.gpg"

  - codename: "edge-base"
    url: "https://files-rs.edgeorchestration.intel.com/files-edge-orch/microvisor/rpms/3.0/base"
    pkey: "https://raw.githubusercontent.com/open-edge-platform/edge-microvisor-toolkit/refs/heads/3.0/SPECS/edge-repos/INTEL-RPM-GPG-KEY"

  - codename: "OpenVINO"
    url: "https://yum.repos.intel.com/openvino/"
    pkey: "https://yum.repos.intel.com/intel-gpg-keys/GPG-PUB-KEY-INTEL-SW-PRODUCTS.PUB"

  - codename: "mariner"
    url: "https://packages.microsoft.com/yumrepos/cbl-mariner-2.0-prod-extended-x86_64/"
    pkey: "https://packages.microsoft.com/azurelinux/3.0/prod/base/x86_64/repodata/repomd.xml.key"
```

## Repository Configuration Examples

### Intel Edge AI Stack

```yaml
packageRepositories:
  - codename: "EdgeAI"
    url: "https://yum.repos.intel.com/edgeai/"
    pkey: "https://yum.repos.intel.com/edgeai/GPG-PUB-KEY-INTEL-DLS.gpg"

  - codename: "OpenVINO"
    url: "https://yum.repos.intel.com/openvino/"
    pkey: "https://yum.repos.intel.com/intel-gpg-keys/GPG-PUB-KEY-INTEL-SW-PRODUCTS.PUB"

systemConfig:
  packages:
    - openvino-toolkit
    - edge-ai-runtime
    - intel-media-driver
    # ....
```

### Microsoft and Intel Integration

```yaml
packageRepositories:
  - codename: "mariner"
    url: "https://packages.microsoft.com/yumrepos/cbl-mariner-2.0-prod-extended-x86_64/"
    pkey: "https://packages.microsoft.com/azurelinux/3.0/prod/base/x86_64/repodata/repomd.xml.key"

  - codename: "edge-base"
    url: "https://files-rs.edgeorchestration.intel.com/files-edge-orch/microvisor/rpms/3.0/base"
    pkey: "https://raw.githubusercontent.com/open-edge-platform/edge-microvisor-toolkit/refs/heads/3.0/SPECS/edge-repos/INTEL-RPM-GPG-KEY"

systemConfig:
  packages:
    - mariner-base-packages
    - microvisor-runtime
    - edge-orchestration-tools
    # ....
```

### Development Environment

```yaml
packageRepositories:
  - codename: "docker-ce"
    url: "https://download.docker.com/linux/ubuntu"
    pkey: "https://download.docker.com/linux/ubuntu/gpg"

  - codename: "nodejs"
    url: "https://deb.nodesource.com/node_18.x"
    pkey: "https://deb.nodesource.com/gpgkey/nodesource.gpg.key"

systemConfig:
  packages:
    - docker-ce
    - docker-ce-cli
    - nodejs
    - npm
    # ....
```

### Trusted Repository (No GPG Verification)

```yaml
# WARNING: "[trusted=yes]" disables signature verification.
# Only use this for repositories fully controlled by your organization,
# typically in development or testing, and never for public or third-party repos.
packageRepositories:
  - codename: "internal-repo"
    url: "https://internal.company.com/packages"
    pkey: "[trusted=yes]"  # Bypasses GPG verification; ONLY for internal, organization-controlled repos.

  - codename: "test-repo"
    url: "https://test.example.com/packages"
    pkey: "[trusted=yes]"  # No signature verification; for internal dev/test only, not public/third-party repos.

systemConfig:
  packages:
    - internal-package
    - test-package
    # ....
```

## Execution Process

### Repository Setup Phase

The build process follows this sequence when multiple repositories are configured:

1. **Repository Configuration**: All repositories in `packageRepositories` are added to the package manager
2. **GPG Key Import**: Public keys are downloaded and imported for repository authentication (skipped for `[trusted=yes]` repositories)
3. **Repository Refresh**: Package lists are updated from all configured repositories
4. **Package Installation**: Packages from all repositories become available for installation

### Package Resolution

When packages are installed:

- The package manager searches all configured repositories
- Dependencies can be resolved across multiple repositories
- Repository priority may affect package selection when conflicts exist

## Best Practices

### 1. Always Include GPG Keys

Include GPG keys for repository authentication and security:

```yaml
packageRepositories:
  # Good - includes GPG key for security
  - codename: "secure-repo"
    url: "https://example.com/packages"
    pkey: "https://example.com/gpg-key.pub"

  # Use trusted=yes only for internal/trusted repositories
  - codename: "internal-repo"
    url: "https://internal.company.com/packages"
    pkey: "[trusted=yes]"

  # Avoid - missing pkey entirely reduces security
  - codename: "insecure-repo"
    url: "https://example.com/packages"
```

### 2. Use Descriptive Codenames

Choose clear, descriptive codenames that indicate the repository purpose:

```yaml
packageRepositories:
  # Good - descriptive codenames
  - codename: "EdgeAI"
  - codename: "OpenVINO"
  - codename: "docker-ce"

  # Avoid - unclear codenames
  - codename: "repo1"
  - codename: "custom"
```

### 3. Verify Repository URLs

Ensure repository URLs are correct and accessible:

```yaml
packageRepositories:
  # Verify these URLs are accessible during build
  - codename: "EdgeAI"
    url: "https://yum.repos.intel.com/edgeai/"
    pkey: "https://yum.repos.intel.com/edgeai/GPG-PUB-KEY-INTEL-DLS.gpg"
```

### 4. Document Repository Sources

Add comments to document repository purposes:

```yaml
packageRepositories:
  # Intel Edge AI packages for computer vision and inference
  - codename: "EdgeAI"
    url: "https://yum.repos.intel.com/edgeai/"
    pkey: "https://yum.repos.intel.com/edgeai/GPG-PUB-KEY-INTEL-DLS.gpg"

  # Microsoft CBL-Mariner extended packages
  - codename: "mariner"
    url: "https://packages.microsoft.com/yumrepos/cbl-mariner-2.0-prod-extended-x86_64/"
    pkey: "https://packages.microsoft.com/azurelinux/3.0/prod/base/x86_64/repodata/repomd.xml.key"
```

## Security Considerations

### Repository Trust

- Only add repositories from trusted sources
- Always include GPG keys for repository authentication when available
- Use `pkey: "[trusted=yes]"` only when GPG keys are unavailable and the repository is under your organization's direct control
- Regularly review and update repository configurations
- Be cautious with repositories that don't provide GPG keys

### Network Security

- Use HTTPS URLs when available
- Consider using local repository mirrors for improved security and performance
- Validate GPG key fingerprints when possible

## Related Documentation

- [Image Templates](../architecture/os-image-composer-templates.md)
- [Understanding the OS Image Build Process](../architecture/os-image-composer-build-process.md)
- [Configuring Custom Commands During Image Build](configure-additional-actions-for-build.md)