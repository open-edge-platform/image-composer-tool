# Usage Guide

A practical guide for common ICT workflows. For the complete
command reference, see the
[CLI Specification](../architecture/image-composer-tool-cli-specification.md).

## Table of Contents

  - [Binary Location](#binary-location)
  - [Commands Overview](#commands-overview)
  - [Building an Image](#building-an-image)
    - [Build Output](#build-output)
  - [Comparing Overlay Outputs](#comparing-overlay-outputs)
    - [Baseline RAW vs Overlay RAW](#baseline-raw-vs-overlay-raw)
    - [SBOM Comparison (Complete SBOM)](#sbom-comparison-complete-sbom)
    - [Delta SBOM: Optional Quick-Change View](#delta-sbom-optional-quick-change-view)
  - [Validating a Template](#validating-a-template)
  - [Resolving a Template](#resolving-a-template)
    - [Debugging an Extends Chain](#debugging-an-extends-chain)
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
image-composer-tool resolve       # Print the merged template YAML for debugging
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
sudo -E ./image-composer-tool build image-templates/azl3/azl3-x86_64-edge-raw.yml

# earthly +build — binary is in ./build/
sudo -E ./build/image-composer-tool build image-templates/azl3/azl3-x86_64-edge-raw.yml

# Debian package — binary is on PATH
sudo image-composer-tool build /usr/share/image-composer-tool/examples/azl3-x86_64-edge-raw.yml

# Override config settings with flags
sudo -E ./image-composer-tool build --workers 16 --cache-dir /tmp/cache image-templates/azl3/azl3-x86_64-edge-raw.yml

# Build from scratch in throwaway cache/workspace dirs (removed after the build)
sudo -E ./image-composer-tool build --no-cache image-templates/azl3/azl3-x86_64-edge-raw.yml
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

## Comparing Overlay Outputs

An [overlay build](../architecture/image-composer-tool-templates.md#baseline)
layers packages onto an existing baseline RAW image and emits, alongside the
result, SPDX SBOM sidecars (see
[SBOM generation (overlay mode)](../architecture/image-composer-tool-templates.md#sbom-generation-overlay-mode)):

| Artifact | Name |
|----------|------|
| Overlay RAW image | `<name>-<version>.raw` |
| Delta SBOM (overlay changes only) | `<name>-<version>.delta.spdx.json` |
| Complete SBOM (full final inventory) | `<name>-<version>.complete.spdx.json` (only when a base SBOM is available; otherwise skipped) |

The existing `compare` command works directly against these outputs — no
overlay-specific flags are needed. The examples below assume the baseline is at
`baseline.raw` and the overlay build produced `myimage-1.0.*` under the build
output directory.

### Baseline RAW vs Overlay RAW

Compare the structural/binary differences between the baseline and the overlay
result. Add `--hash-images` to compute SHA256 and get a binary-identity verdict:

```bash
# Human-readable structural diff (partition table, filesystems, bootloader, SBOM metadata)
./image-composer-tool compare baseline.raw myimage-1.0.raw

# High-level counts as JSON, suitable for CI
./image-composer-tool compare --format=json --mode=summary baseline.raw myimage-1.0.raw

# Binary-identity check (slower — hashes both images)
./image-composer-tool compare --hash-images baseline.raw myimage-1.0.raw
```

Expected output pattern (text): a top `Equality:` verdict followed by the changed
sections. Because the overlay grows the disk and rewrites the embedded SBOM, the
verdict is `different`, and the summary reports `sbomChanged` and a size change:

```
Equality: different (meaningful diffs: ...)
Image:
  Size: 4.0 GiB -> 6.0 GiB
SBOM:
  PackageCount: 120 -> 123
```

The equality classes are `binary_identical` (only with `--hash-images`, when the
SHA256s match), `semantically_identical` / `semantically_identical_unverified`
(no meaningful diffs), or `different`.

> **Note:** In the default image modes the compare tool reports SBOM *metadata*
> (package count, canonical hash) only. For a package-by-package breakdown, use
> the SBOM comparison below.

### SBOM Comparison (Complete SBOM)

Use `--mode=spdx` for an accurate package-level diff. The complete SBOM
represents the full final image inventory, so comparing the baseline's SBOM
against the overlay complete SBOM reports exactly which packages were added or
upgraded:

```bash
# Compare the baseline SBOM against the overlay complete SBOM
./image-composer-tool compare --mode=spdx baseline.spdx.json myimage-1.0.complete.spdx.json

# Either argument may also be an image — its embedded SBOM (/usr/share/sbom) is
# extracted automatically, so you can compare the baseline RAW directly:
./image-composer-tool compare --mode=spdx baseline.raw myimage-1.0.complete.spdx.json

# JSON for tooling
./image-composer-tool compare --format=json --mode=spdx baseline.spdx.json myimage-1.0.complete.spdx.json
```

Expected output pattern (text): added, removed, and upgraded package lists. A
same-name version bump is reported as a single upgrade (`~`), not a remove + add:

```
SPDX Compare
============
Equal:    false
Packages: 120 -> 123

Added packages:
  + tree|2.1.1|...

Upgraded packages:
  ~ curl: 8.5.0 -> 8.6.0
```

Overlay builds are additive by default, so you will typically see additions and
upgrades. Removals appear only when
[`overlayPolicy.allowPackageRemoval`](../architecture/image-composer-tool-templates.md#overlaypolicy)
is enabled (a conflict-driven baseline package removal).

### Delta SBOM: Optional Quick-Change View

The delta SBOM lists only the overlay-contributed packages (what changed). It is
an **optional** convenience for a quick "what did this overlay add?" view without
diffing against the baseline — you can inspect it directly or diff it against an
empty document:

```bash
# The delta SBOM is a plain SPDX JSON file — read it directly
jq '.packages[].name' myimage-1.0.delta.spdx.json

# Or diff it against an empty SBOM to render the same added-package list via compare
echo '{"packages":[]}' > empty.spdx.json
./image-composer-tool compare --mode=spdx empty.spdx.json myimage-1.0.delta.spdx.json
```

For an authoritative before/after picture of the image, prefer the **complete**
SBOM comparison above; the delta is a shortcut, not a replacement.

## Validating a Template

Check a template for errors before starting a build:

```bash
./image-composer-tool validate image-templates/azl3/azl3-x86_64-edge-raw.yml
```

## Resolving a Template

Print the merged template YAML to stdout — useful for debugging templates that
use `extends:` or for previewing exactly what the tool will build. Sensitive
fields (user passwords, `systemConfig.users[*].hash_algo`, and the secure boot
signing paths `systemConfig.immutability.secureBootDBKey`,
`systemConfig.immutability.secureBootDBCrt`, and
`systemConfig.immutability.secureBootDBCer`) are always redacted in the output:

```bash
# Chain-merge only, without OS defaults
./image-composer-tool resolve image-templates/ubuntu24/ubuntu24-x86_64-extends-example-raw.yml

# Full build-time view: extends chain + OS defaults
./image-composer-tool resolve image-templates/azl3/azl3-x86_64-edge-raw.yml --full
```

### Debugging an Extends Chain

When a template uses `extends:` to inherit from a parent, `resolve` folds
the chain and prints the effective template so you can verify what the
leaf actually inherits before running a build:

```bash
# Show what an extends child inherits from its parent
./image-composer-tool resolve image-templates/ubuntu24/ubuntu24-x86_64-extends-example-raw.yml
```

The output includes the union of the parent's and child's
`systemConfig.packages`, plus every other field folded through the
chain's per-section merge rules. OS defaults are not applied unless you
pass `--full`, so the output reflects only what the templates in the
chain declared — useful for spotting an accidentally-overridden field.
For the complete inheritance semantics (single-parent chains, cycle
detection, target-match rules, and the depth-warning threshold), see
[Template Extends (Inheritance)](../architecture/image-composer-tool-templates.md#template-extends-inheritance).

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
