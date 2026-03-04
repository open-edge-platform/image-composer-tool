# OS Image Composer

OS Image Composer is a command-line tool that uses a simple toolchain to build
mutable or immutable Linux distributions from the pre-built packages from
different OS distribution repositories.
Developed in the Go programming language, or Golang, the tool builds
custom images for
Edge Microvisor Toolkit, Azure Linux, Wind River eLxr, Ubuntu, and Red Hat-compatible distributions (RCD).

## Get Started

The initial release of the OS Image Composer tool has been tested and validated
to work with Ubuntu 24.04, which is the recommended distribution for running
the tool. Other standard Linux distributions should also work but haven't been
validated. The plan for later releases is to include a containerized version to
support portability across operating systems.

- Download the tool by cloning and checking out the latest tagged release on
  [GitHub](https://github.com/open-edge-platform/os-image-composer/).
  Alternatively, you can download the
  [latest tagged release](https://github.com/open-edge-platform/os-image-composer/releases)
  of the ZIP archive.

- Install version 1.24.0 or later of the Go programming language before
  building the tool; see the
  [Go installation instructions](https://go.dev/doc/manage-install)
  for your Linux distribution.

### Build the Tool

Build the OS Image Composer command-line utility by using Go directly or by
using the Earthly framework:

```bash
# Build the tool:
go build -buildmode=pie -ldflags "-s -w" ./cmd/os-image-composer

# Or run it directly:
go run ./cmd/os-image-composer --help
```

Using the Earthly framework produces a reproducible build that automatically
includes the version number (from the `--version` parameter), the build date
(the current UTC date), and the Git commit SHA (current repository commit).

```bash
# Default build
earthly +build
```

| Guide | Description |
|-------|-------------|
| [Installation Guide](./tutorial/installation.md) | Build methods, Debian packaging, prerequisites |
| [Usage Guide](./tutorial/usage-guide.md) | CLI commands, configuration, build output, shell completion |
| [CLI Reference](./architecture/os-image-composer-cli-specification.md) | Complete command-line specification |
| [Image Templates](./architecture/os-image-composer-templates.md) | Field-by-field template reference, merge behavior, best practices |
| [Build Process](./architecture/os-image-composer-build-process.md) | Pipeline stages, caching, troubleshooting |
| [Architecture](./architecture/architecture.md) | System design and component overview |

### Install the Prerequisites for Composing an Image

Before you compose an operating system image with the OS Image Composer tool,
follow the
[instructions to install two prerequisites](./tutorial/prerequisite.md):

- `ukify`, which combines components -- typically a kernel, an initrd, and a
  UEFI boot stub -- to create a signed Unified Kernel Image (UKI), which is a
  PE binary that firmware executes to start an embedded Linux kernel.

- `mmdebstrap`, which downloads, unpacks, and installs Debian packages to
  initialize a chroot.

### Compose or Validate an Image

Now you're ready to compose an image from a built-in template or validate a
template.

```bash
# Build an image from template
sudo -E ./os-image-composer build image-templates/azl3-x86_64-edge-raw.yml

# Validate a template:
./os-image-composer validate image-templates/azl3-x86_64-edge-raw.yml
```

After the image finishes building, check your output directory. The exact
name of the output directory varies by environment and image but should look
something like this:

```bash
/os-image-composer/tmp/os-image-composer/azl3-x86_64-edge-raw/imagebuild/Minimal_Raw
```

To build an image from your own template, see
[Creating and Reusing Image Templates](./architecture/os-image-composer-templates.md).
For complete usage instructions, see the
[Command-Line Reference](./architecture/os-image-composer-cli-specification.md).

## Configuration

### Global Configuration

The OS Image Composer tool supports global configuration files for setting
tool-level parameters that apply across all image builds. Image-specific
parameters are defined in YAML image template files. See
[Understanding the OS Image Build Process](./architecture/os-image-composer-build-process.md).

### Configuration File Locations

The tool searches for configuration files in the following order:

1. `os-image-composer.yaml` (current directory)
2. `os-image-composer.yml` (current directory)
3. `.os-image-composer.yaml` (hidden file in current directory)
4. `~/.os-image-composer/config.yaml` (user home directory)
5. `~/.config/os-image-composer/config.yaml` (XDG config directory)
6. `/etc/os-image-composer/config.yaml` (system-wide)

### Configuration Parameters

```yaml
# Core tool settings
workers: 12                              # Number of concurrent download workers (1-100, default: 8)
cache_dir: "/var/cache/os-image-composer"   # Package cache directory (default: ./cache)
work_dir: "/tmp/os-image-composer"          # Working directory for builds (default: ./workspace)
temp_dir: ""                             # Temporary directory (empty = system default)

# Logging configuration
logging:
  level: "info"                          # Log level: debug, info, warn, error (default: info)
```

### Configuration Management Commands

```bash
# Create a new configuration file
./os-image-composer config init

# Create config file at specific location
./os-image-composer config init /path/to/config.yaml

# Show current configuration
./os-image-composer config show

# Use specific configuration file
./os-image-composer --config /path/to/config.yaml build template.yml
```

## Operations Requiring Sudo Access

The OS Image Composer performs several system-level operations that require
elevated privileges (sudo access).

### System Directory Access and Modification

The following system directories require root access for
OS Image Composer operations:

- **`/etc/` directory operations**: Writing system configuration files,
  modifying network configurations, updating system settings
- **`/dev/` device access**: Block device operations, loop device management,
  hardware access
- **`/sys/` filesystem access**: System parameter modification, kernel
  interface access
- **`/proc/` filesystem modification**: Process and system state changes
- **`/boot/` directory**: Boot loader and kernel image management
- **`/var/` system directories**: System logs, package databases, runtime state
- **`/usr/sbin/` and `/sbin/`**: System administrator binaries

### Common Privileged Operations

OS Image Composer typically requires sudo for:

- **Block device management**: Creating loop devices, partitioning,
  filesystem creation
- **Mount/unmount operations**: Mounting filesystems, managing mount points
- **Chroot environment setup**: Creating and managing isolated
  build environments
- **Package installation**: System-wide package management operations
- **Boot configuration**: Installing bootloaders, managing EFI settings
- **Security operations**: Secure boot signing, cryptographic operations

## Usage

The OS Image Composer tool uses a command-line interface with various commands.
Here are some examples:

```bash
# Show help
./os-image-composer --help

# Build command with template file as positional argument
sudo -E ./os-image-composer build image-templates/azl3-x86_64-edge-raw.yml

# Override config settings with command-line flags
sudo -E ./os-image-composer build --workers 16 --cache-dir /tmp/cache image-templates/azl3-x86_64-edge-raw.yml

# Validate a template file against the schema
./os-image-composer validate image-templates/azl3-x86_64-edge-raw.yml

# Display version information
./os-image-composer version

# Install shell completion for your current shell
./os-image-composer install-completion
```

### Commands

The OS Image Composer tool provides the following commands:

#### build

Builds a Linux distribution image based on the specified image template file:

```bash
sudo -E ./os-image-composer build [flags] TEMPLATE_FILE
```

Flags:

- `--workers, -w`: Number of concurrent download workers (overrides the
  configuration file)
- `--cache-dir, -d`: Package cache directory (overrides the configuration file)
- `--work-dir`: Working directory for builds (overrides the configuration file)
- `--verbose, -v`: Enable verbose output
- `--dotfile, -f`: Generate a dependency graph for the merged template as a dot file
- `--system-packages-only`: Use together with `--dotfile` to keep only `SystemConfig.Packages` roots in the graph (dependencies still appear if required)
- `--config`: Path to the configuration file
- `--log-level`: Log level (debug, info, warn, and error)

Example:

```bash
sudo -E ./os-image-composer build --workers 12 --cache-dir ./package-cache image-templates/azl3-x86_64-edge-raw.yml
```

#### config

Manages the global configuration:

```bash
# Show current configuration
./os-image-composer config show

# Initialize new configuration file
./os-image-composer config init [config-file]
```

#### validate

Validates a YAML template file against the schema without building an image:

```bash
./os-image-composer validate TEMPLATE_FILE
```

The `os-image-composer validate` command is useful for verifying template
configurations before starting the potentially time-consuming build process.

#### version

Displays the tool’s version number, build date, and Git commit SHA:

```bash
./os-image-composer version
```

#### install-completion

Installs the shell completion feature for your current shell or
a specified shell:

```bash
# Auto-detect shell and create completion file
./os-image-composer install-completion

# Specify shell type
./os-image-composer install-completion --shell bash
./os-image-composer install-completion --shell zsh
./os-image-composer install-completion --shell fish
./os-image-composer install-completion --shell powershell

# Force overwrite existing completion files
./os-image-composer install-completion --force
```

**Important**: The command creates completion files but additional activation
steps are required:

Bash:

```bash
# Add to your ~/.bashrc
echo "source ~/.bash_completion.d/os-image-composer.bash" >> ~/.bashrc
source ~/.bashrc
```

Reload your shell configuration based on the shell that you are using:

Bash:

```bash
source ~/.bashrc
```

Zsh (May need fpath setup):

```zsh
# Ensure completion directory is in fpath (add to ~/.zshrc if needed)
echo 'fpath=(~/.zsh/completion $fpath)' >> ~/.zshrc
source ~/.zshrc
```

Fish (Works automatically):

```fish
# Just restart your terminal
```

PowerShell (May need execution policy):

```powershell
# May need to allow script execution
Set-ExecutionPolicy -ExecutionPolicy RemoteSigned -Scope CurrentUser
. $PROFILE
```

Test the completion:

```bash
os-image-composer [TAB]
os-image-composer b[TAB]
os-image-composer build --[TAB]
```

### Image Template Format

Written in the YAML format, templates define the requirements for building an
operating system image. The template structure enables you to define key
parameters, such as the operating system distribution, version, architecture,
software packages, output format, and kernel configuration. The image template
format is validated against a JSON schema to check syntax and semantics before
building the image.

If you're an entry-level user or have straightforward requirements, you can
reuse the basic template and add the packages you require. If you're addressing
an advanced use case with, for instance, robust security requirements, you can
modify the template to define disc and partition layouts and
other settings for security.

```yaml
image:
  name: azl3-x86_64-edge
  version: "1.0.0"

target:
  os: azure-linux    # Target OS name
  dist: azl3          # Target OS distribution
  arch: x86_64        # Target OS architecture
  imageType: raw      # Image type: raw, iso, img, vhd

systemConfigs:
  - name: edge
    description: Default configuration for edge image

    # Package Configuration
    packages:
      # Additional packages beyond the base system
      - openssh-server      # Remote access
      - docker-ce          # Container runtime
      - vim                # Text editor
      - curl               # HTTP client
      - wget               # File downloader

    # Kernel Configuration
    kernel:
      version: "6.12"
      cmdline: "quiet splash"
```

#### Key Components

Here are the key components of an image template.

##### 1. `image`

Basic image identification and metadata:

- `name`: Name of the resulting image
- `version`: Version for tracking and naming

##### 2. `target`

Defines the target OS and image configuration:

- `os`: Target OS (`azure-linux`, `emt`, and `elxr`)
- `dist`: Distribution identifier (`azl3`, `emt3`, and `elxr12`)
- `arch`: Target architecture (`x86_64`and `aarch64`)
- `imageType`: Output format (`raw`, `iso`, `img`, and `vhd`)

##### 3. `systemConfigs`

Array of system configurations that define what goes into the image:

- `name`: Configuration name
- `description`: Human-readable description
- `packages`: List of packages to include in the OS build
- `kernel`: Kernel configuration with version and command-line parameters

#### Supported Distributions

| OS | Distribution | Version | Provider |
|----|-------------|---------|----------|
| azure-linux | azl3 | 3 | AzureLinux3 |
| emt | emt3 | 3.0 | EMT3.0 |
| elxr | elxr12 | 12 | eLxr12 |

#### Package Examples

You can include common packages:

- `cloud-init`: For initializing cloud instances
- `python3`: The Python 3 programming language interpreter
- `rsyslog`: A logging system for Linux OS
- `openssh-server`: SSH server for remote access
- `docker-ce`: Docker container runtime

### Shell Completion Feature

The OS Image Composer CLI supports shell auto-completion for the Bash, Zsh,
Fish, and PowerShell command-line shells. This feature helps users discover
and use commands and flags more efficiently.

#### Generate Completion Scripts

```bash
# Bash
./os-image-composer completion bash > os-image-composer_completion.bash

# Zsh
./os-image-composer completion zsh > os-image-composer_completion.zsh

# Fish
./os-image-composer completion fish > os-image-composer_completion.fish

# PowerShell
./os-image-composer completion powershell > os-image-composer_completion.ps1
```

#### Install Completion Scripts

After you install the completion script for your command-line shell, you can
use tab completion to navigate commands, flags, and arguments.

**Bash**:

```bash
# Temporary use
source os-image-composer_completion.bash

# Permanent installation (Linux)
sudo cp os-image-composer_completion.bash /etc/bash_completion.d/
# or add to your ~/.bashrc
echo "source /path/to/os-image-composer_completion.bash" >> ~/.bashrc
```

**Zsh**:

```bash
# Add to your .zshrc
echo "source /path/to/os-image-composer_completion.zsh" >> ~/.zshrc
# Or copy to a directory in your fpath
cp os-image-composer_completion.zsh ~/.zfunc/_os-image-composer
```

**Fish**:

```bash
cp os-image-composer_completion.fish ~/.config/fish/completions/os-image-composer.fish
```

**PowerShell**:

```powershell
# Add to your PowerShell profile
echo ". /path/to/os-image-composer_completion.ps1" >> $PROFILE
```

#### Examples of Completion in Action

Once the completion script is installed, the tool is configured to suggest YAML
files when completing the template file argument for the build and validate
commands, and you can see that in action:

```bash
# Tab-complete commands
./os-image-composer <TAB>
build      completion  config     help       validate    version

# Tab-complete flags
sudo -E ./os-image-composer build --<TAB>
--cache-dir  --config    --help       --log-level  --verbose    --work-dir   --workers

# Tab-complete YAML files for template file argument
sudo -E ./os-image-composer build <TAB>
# Will show YAML files in the current directory
```

## Template Examples

Here are several example YAML template files. You can use YAML image templates
to rapidly reproduce custom, verified, and inventoried operating systems; see
[Creating and Reusing Image Templates](./architecture/os-image-composer-templates.md).

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

systemConfigs:
  - name: minimal
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

systemConfigs:
  - name: development
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
  os: emt
  dist: emt3
  arch: x86_64
  imageType: raw

systemConfigs:
  - name: edge
    description: Edge Microvisor Toolkit configuration
    packages:
      - openssh-server
      - docker-ce
      - edge-runtime
      - telemetry-agent
    kernel:
      version: "6.12"
      cmdline: "quiet splash systemd.unified_cgroup_hierarchy=0"
```

## Get Help

- Run the following command in the command-line tool to see all the commands
  and options:

  ```sh
  ./os-image-composer --help
  ```

- See the [CLI Specification and Reference](./architecture/os-image-composer-cli-specification.md).
- Read the [documentation](https://github.com/open-edge-platform/os-image-composer/tree/main/docs).
- Troubleshoot by using the [Build Process documentation](./architecture/os-image-composer-build-process.md#troubleshooting-build-issues).
- [Participate in discussions](https://github.com/open-edge-platform/os-image-composer/discussions).

## Contribute

- [Open an issue](https://github.com/open-edge-platform/os-image-composer/issues).
- [Report a security vulnerability](https://github.com/open-edge-platform/os-image-composer/blob/main/SECURITY.md).
- [Submit a pull request](https://github.com/open-edge-platform/os-image-composer/pulls).

## License Information

See [License](https://github.com/open-edge-platform/os-image-composer/blob/main/LICENSE).

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