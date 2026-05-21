# ADR: Baseline Image Extension and ISO Composition Boundaries

**Status**: Proposed  
**Date**: 2026-05-21  
**Updated**: N/A  
**Authors**: OS Image Composer Team  
**Technical Area**: Image Composition / Provisioning / Live Installer 

---

## Summary

OS Image Composer supports declarative composition of bootable Linux images and
already provides an ICT-owned ISO live installer flow for attended and unattended
installation scenarios.

This ADR defines the boundary for using existing artifacts as baselines for
additional composition.

The decision is to support additive extension of existing disk image baselines,
such as RAW/VHD-style images, while avoiding generic mutation of arbitrary ISO
installer media.

Disk images may be mounted, inspected, extended with additional repositories and
packages, resized where supported, validated, and emitted as updated artifacts.

ISO installers are different. An ISO installer is not simply an installed OS
image. It contains boot media, installer behavior, payloads, manifests, and
distro-specific logic. ICT should continue to support its own ISO live installer
and may evolve that installer explicitly over time. However, ICT should not
attempt to generically extend arbitrary existing ISO installers.

---

## Context

Image Composer Tool supports image creation across multiple Linux distributions
using declarative templates. The current schema already captures target OS,
distribution, architecture, output image type, disk layout, system packages,
repositories, bootloader, kernel, network, users, and immutability settings.

The schema supports `target.imageType` values such as `raw`, `img`, and `iso`,
and disk output artifacts such as `raw`, `qcow2`, `vhd`, `vhdx`, `vmdk`, and
`vdi`.

A new requirement is emerging: allow users to start from an existing image
artifact and add enablement such as Intel packages, EdgePack profiles, DKMS
packages, firmware, AI runtimes, and workload-specific packages.

This is practical for disk images because the installed root filesystem can be
mounted and modified. It is significantly more complex for ISO installers
because the install result is controlled by installer-specific payloads and
logic.

---

## Problem Statement

Image Composer Tool needs a clear and supportable model for using existing
artifacts as composition baselines.

Extending an existing RAW/VHD-style disk image is practical because the image
contains the deployed target root filesystem. The image can be mounted,
inspected, modified, validated, resized, and repackaged.

For ISO installers, extension is ambiguous and distro-specific:

- Adding packages may affect only the live environment, not the installed target.
- The installed target may come from a compressed filesystem payload.
- The installer may use package pools, manifests, autoinstall files, kickstart,
preseed, Subiquity, Anaconda, Debian Installer, or custom logic.
- Modified content may require regenerated checksums, manifests, signatures, or
Secure Boot-related artifacts.
- Each ISO family may require different handling.

Therefore, generic ISO extension has high complexity, weak portability, and
questionable ROI.

---

## Decision / Recommendation

### Disk Image Baselines

A disk image baseline typically contains the actual installed target system:

- Partition table.
- EFI System Partition.
- Root filesystem.
- Bootloader configuration.
- Kernel and initramfs artifacts.
- Package database.
- OS release metadata.
- Installed package set.

Because the root filesystem is directly available, OS Image Composer can modify
it using native package manager flows.

### ISO Installer Media

An ISO installer is usually not the installed system. Depending on the
distribution, it may include:

- A live boot environment.
- SquashFS or another compressed filesystem payload.
- Package pools.
- Installer configuration.
- Autoinstall, preseed, kickstart, or cloud-init metadata.
- Release manifests and checksums.
- Secure Boot artifacts.
- Installer-specific logic such as Subiquity, Debian Installer, Anaconda,
Calamares, or custom installers.

Therefore, “add a package to an ISO” is ambiguous. It may mean:

- Add the package to the live environment only.
- Add the package to the installed target system.
- Add the package to an offline package pool.
- Modify the installer recipe.
- Regenerate the install payload.
- Recalculate manifests.
- Preserve or recreate boot and signing behavior.

These operations are not portable across ISO formats.

---

## Proposal

Image Composer Tool will support three explicit composition modes:

### 1. Fresh Image Composition

Image Composer Tool composes an image from a known distro/template baseline.

This remains the primary model for creating minimal, workload-optimized,
security-hardened, and reproducible images.

Supported output artifacts may include:

- RAW
- IMG
- VHD/VHDX
- QCOW2
- VMDK
- ISO, where generated from a supported template

### 2. Disk Image Extension

Image Composer Tool may use an existing disk image as an additive baseline.

Supported input formats may include:

- RAW
- IMG
- VHD/VHDX
- QCOW2
- VMDK, where tooling support exists

Supported operations include:

- Mount and inspect the image.
- Detect OS metadata, architecture, package manager, kernel, bootloader, and
partition layout.
- Add package repositories.
- Resolve package dependencies.
- Install additional packages.
- Detect and report package conflicts.
- Fail on incompatible package versions unless explicitly allowed.
- Resize the disk image where supported.
- Grow supported filesystems.
- Regenerate initramfs, UKI, or bootloader configuration when required.
- Generate package inventory, SBOM, and CVE reports.
- Inspect and compare the resulting image.
- Emit updated image artifacts.

### 3. ISO Remastering

Generic ISO remastering is out of scope.

Distro-specific ISO remastering may be added later only through explicit adapters.

Each adapter must define:

- Supported distribution and release.
- Supported ISO variants.
- Expected ISO layout.
- Whether the live environment can be modified.
- Whether the installed target payload can be modified.
- Whether offline package pools are supported.
- How installer configuration is modified.
- How manifests and checksums are regenerated.
- How Secure Boot behavior is preserved or invalidated.
- How the resulting ISO is validated.

---

## Core Design Principles

### Treat disk images as composition inputs

Existing disk images can be mounted and extended because they contain the target
root filesystem.

### Treat ISO installers as generated artifacts by default

ISO installers should be created from declarative templates where Image Composer
Tool owns the installer structure and validation contract.

### Prefer additive composition

Initial baseline extension supports adding repositories, packages,
configuration, and metadata. Removing existing content is out of scope.

### Avoid structural mutation of existing images

Changing partition table type, filesystem type, boot mode, or fundamental disk
layout of an existing baseline is out of scope unless explicitly supported by a
future migration feature.

### Fail safely on conflicts

Package conflicts, missing dependencies, unsupported repository combinations,
unsupported filesystem layouts, or incompatible baselines should result in clear
failure.

### Preserve standard OS lifecycle

Extended images should continue to use standard OS package managers and day-2
update mechanisms wherever possible.

### Make output inspectable

Generated and extended images should support inspection, SBOM generation, CVE
verification, and optional semantic comparison.

---

## Separation of Responsibilities

### OS Image Composer Responsibilities

OS Image Composer is responsible for:

- Declarative image definition.
- Baseline inspection.
- Repository configuration.
- Additive package installation orchestration.
- Dependency and conflict resolution through native package managers.
- Disk resize orchestration where supported.
- Boot artifact regeneration where required.
- SBOM generation.
- CVE verification integration.
- Image inspection and comparison.
- Emitting bootable output artifacts.

### Native Package Manager Responsibilities

The native package manager is responsible for:

- Resolving package dependencies.
- Enforcing package constraints.
- Detecting package conflicts.
- Installing packages into the target root filesystem.
- Running package maintainer scripts where supported.
- Maintaining package database integrity.

Examples include:

- `apt` / `dpkg`
- `dnf` / `rpm`
- `yum` / `rpm`

### Distribution Installer Responsibilities

External distro installers remain responsible for:

- Their own boot behavior.
- Their own target installation flow.
- Their own live environment behavior.
- Their own package selection semantics.
- Their own payload layout.
- Their own manifests, checksums, and signing behavior.

ICT should not generically reverse-engineer or override arbitrary external
installer behavior.

---

## Template Schema

The existing template schema should be extended rather than replaced.

The recommended addition is a top-level `baseline` object that declares whether
the template performs fresh composition, disk image extension, ISO generation,
or supported ISO remastering.

### Fresh composition

```yaml
image:
  name: ubuntu-edge-ai
  version: 1.0.0

target:
  os: ubuntu
  dist: ubuntu24
  arch: x86_64
  imageType: raw

baseline:
  mode: compose
  type: distro-template

systemConfig:
  packages:
    - edgepack-physical-ai
```

### Disk image extensions

```yaml
image:
  name: ubuntu-edge-ai-extended
  version: 1.0.0

target:
  os: ubuntu
  dist: ubuntu24
  arch: x86_64
  imageType: raw

baseline:
  mode: extend
  type: disk-image
  source: ./input/ubuntu-24.04-baseline.raw
  format: raw

extensionPolicy:
  packages:
    operation: additive-only
    conflictPolicy: fail
    allowDowngrade: false
    allowRemoval: false

disk:
  name: primary
  size: 32GiB
  artifacts:
    - type: raw
    - type: vhdx

packageRepositories:
  - codename: intel-edgepack
    url: https://example.com/edgepack/ubuntu
    component: main
    pkey: https://example.com/edgepack.gpg

systemConfig:
  packages:
    - edgepack-physical-ai
    - intel-edge-graphics
```

### ISO generation

```yaml
image:
  name: ubuntu-edge-ai-installer
  version: 1.0.0

target:
  os: ubuntu
  dist: ubuntu24
  arch: x86_64
  imageType: iso

baseline:
  mode: compose
  type: installer-template
  installer: ubuntu-live-server

disk:
  name: install-target
  selectionPolicy:
    strategy: largest
    excludeRemovable: true
    requireEmpty: true

systemConfig:
  packages:
    - edgepack-physical-ai
```

---

## Approach

### Phase 1: Disk Image Extension

Implement first-class support for RAW/VHD-style disk image extension.

#### Initial capabilities

- Mount disk image.
- Inspect partition table and filesystem layout.
- Identify root filesystem.
- Detect OS and package manager.
- Configure repositories.
- Resolve dependencies.
- Install additional packages.
- Detect conflicts.
- Fail on unsafe version changes.
- Resize disk image where supported.
- Grow supported filesystems.
- Regenerate boot artifacts where required.
- Generate SBOM.
- Run CVE verification.
- Inspect and compare output image.
- Boot-test representative images in CI.

#### Initial limitations

- No package removal.
- No filesystem type conversion.
- No partition table conversion.
- No arbitrary repartitioning.
- No shrinking.
- No cross-distro package conversion.
- No unsupported bootloader migration.
- No mutation of encrypted partitions unless explicitly supported.
- No mutation of dm-verity protected root filesystems unless verity metadata is
regenerated through a supported flow.

### Phase 2: Optional ISO Remastering Adapters

Add ISO remastering only for high-value, explicitly supported installer families.

Examples:

- Ubuntu live-server ISO adapter.
- Ubuntu desktop ISO adapter.
- Debian Installer ISO adapter.
- Fedora Anaconda ISO adapter.

Each adapter must include version-specific validation and negative tests for unsupported variants.

### In Scope

- Disk Image Extension
- RAW/IMG disk image extension.
- VHD/VHDX extension, where tooling support exists.
- QCOW2/VMDK support, where tooling support exists or conversion is supported.
- Additive package installation.
- Repository addition.
- Dependency resolution.
- Package conflict detection.
- Version conflict detection.
- Disk image resize.
- Filesystem grow operation for supported filesystems.
- Boot artifact regeneration where required.
- SBOM generation.
- CVE verification.
- Image inspect and compare.
- Support declarative package/profile selection.

## Out of Scope

### Disk Image Extension Out of Scope

- Removing packages or content from the baseline image.
- Shrinking images or filesystems.
- Changing filesystem type, such as ext4 to XFS or Btrfs.
- Changing partition table type, such as MBR to GPT.
- Arbitrary repartitioning of existing baselines.
- Replacing the distribution identity of the baseline image.
- Converting a baseline from one package ecosystem to another.
- Migrating bootloaders unless explicitly supported.
- Silently resolving package conflicts through downgrade or replacement.
- Modifying encrypted partitions unless explicitly supported.
- Modifying dm-verity protected root filesystems unless regenerated through a supported flow.

## Alternatives Consideres

### Alternative 1: Support Generic ISO Mutation

Allow users to provide any ISO and request package additions or configuration changes.

**Rejected.**

Generic ISO mutation requires understanding each installer’s internal structure, payload model, package selection behavior, manifests, checksums, and boot/security model. This approach is brittle, expensive to maintain, hard to validate, and unlikely to scale.

### Alternative 2: Treat ISO and RAW/VHD Inputs the Same

Expose one baseline abstraction for all artifact types.

**Rejected.**

Disk images and ISO installers have different semantics. A disk image usually contains the target root filesystem. An ISO installer may contain multiple environments and installer-specific payloads.

### Alternative 3: Support Full Structural Mutation of Disk Images

Allow users to change partition layout, filesystem type, partition table type, encryption model, and boot mode of an existing baseline.

**Rejected for initial implementation.**

Structural mutation has high complexity and risk. Significant layout changes should use fresh composition from a declarative template.

### Alternative 4: Support Package Removal During Baseline Extension

Allow users to remove existing packages from a baseline image.

**Deferred.**

Package removal can have complex dependency side effects and may break assumptions in the baseline image. Initial support should be additive only.

### Alternative 5: Generate All Images from Scratch Only

Require all images to be generated from templates and disallow existing image baselines.

**Rejected.**

Extending existing disk images provides practical value for customer-specific customization, ODM workflows, and incremental platform enablement.

## Schema Recommendation

**Overlay onto the current schema** instead of creating a second template schema.

The current schema has:

- `target.imageType` with `raw`, `img`, and `iso`.
- `disk.artifacts` supporting `raw`, `qcow2`, `vhd`, `vhdx`, `vmdk`, and `vdi`.
- `disk.selectionPolicy` for installer-time disk selection.
- `systemConfig.packages` for additive package inclusion.
- `packageRepositories` with repo priority and allow-list support.
- `immutability`, `bootloader`, `kernel`, and `network`.

What is missing is the **composition mode**.

```json
{
  "baseline": {
    "$ref": "#/$defs/Baseline"
  },
  "extensionPolicy": {
    "$ref": "#/$defs/ExtensionPolicy"
  },
  "validation": {
    "$ref": "#/$defs/Validation"
  }
}
```

### Proposed Baseline Definition

```json
"Baseline": {
  "type": "object",
  "description": "Declares the image composition starting point and operation mode.",
  "properties": {
    "mode": {
      "type": "string",
      "description": "Composition mode.",
      "enum": ["compose", "extend", "remaster"]
    },
    "type": {
      "type": "string",
      "description": "Baseline artifact type.",
      "enum": ["distro-template", "installer-template", "disk-image", "iso"]
    },
    "source": {
      "type": "string",
      "description": "Path or URI to the baseline artifact."
    },
    "format": {
      "type": "string",
      "description": "Input artifact format.",
      "enum": ["raw", "img", "qcow2", "vhd", "vhdx", "vmdk", "vdi", "iso"]
    },
    "installer": {
      "type": "string",
      "description": "Known installer template or installer family.",
      "enum": ["ubuntu-live-server", "ubuntu-desktop", "debian-installer", "fedora-anaconda"]
    },
    "adapter": {
      "type": "string",
      "description": "Explicit distro-specific remastering adapter."
    }
  },
  "required": ["mode", "type"],
  "additionalProperties": false,
  "allOf": [
    {
      "if": {
        "properties": {
          "mode": { "const": "extend" },
          "type": { "const": "disk-image" }
        }
      },
      "then": {
        "required": ["source", "format"],
        "properties": {
          "format": {
            "enum": ["raw", "img", "qcow2", "vhd", "vhdx", "vmdk", "vdi"]
          }
        }
      }
    },
    {
      "if": {
        "properties": {
          "mode": { "const": "extend" },
          "type": { "const": "iso" }
        }
      },
      "then": false
    },
    {
      "if": {
        "properties": {
          "mode": { "const": "remaster" },
          "type": { "const": "iso" }
        }
      },
      "then": {
        "required": ["source", "adapter"]
      }
    },
    {
      "if": {
        "properties": {
          "mode": { "const": "compose" },
          "type": { "const": "installer-template" }
        }
      },
      "then": {
        "required": ["installer"]
      }
    }
  ]
}
```

### Proposed Extension Policy

```json
"ExtensionPolicy": {
  "type": "object",
  "description": "Policy for extending an existing disk image baseline.",
  "properties": {
    "packageOperation": {
      "type": "string",
      "description": "Allowed package mutation model.",
      "enum": ["additive-only"],
      "default": "additive-only"
    },
    "conflictPolicy": {
      "type": "string",
      "description": "Behavior when package conflicts are detected.",
      "enum": ["fail", "allow-explicit"],
      "default": "fail"
    },
    "allowDowngrade": {
      "type": "boolean",
      "description": "Allow package downgrades during dependency resolution.",
      "default": false
    },
    "allowRemoval": {
      "type": "boolean",
      "description": "Allow package removals during dependency resolution.",
      "const": false,
      "default": false
    },
    "resize": {
      "type": "object",
      "description": "Disk resize policy for baseline extension.",
      "properties": {
        "enabled": {
          "type": "boolean",
          "default": false
        },
        "growFilesystems": {
          "type": "boolean",
          "default": true
        }
      },
      "additionalProperties": false
    }
  },
  "additionalProperties": false
}
```

### Proposed Validation Policy

```json
"Validation": {
  "type": "object",
  "description": "Validation and reporting outputs for composed or extended images.",
  "properties": {
    "inspect": {
      "type": "boolean",
      "default": true
    },
    "compare": {
      "type": "object",
      "properties": {
        "enabled": {
          "type": "boolean",
          "default": false
        },
        "baseline": {
          "type": "string",
          "description": "Path or URI to comparison baseline."
        }
      },
      "additionalProperties": false
    },
    "sbom": {
      "type": "boolean",
      "default": false
    },
    "cveCheck": {
      "type": "boolean",
      "default": false
    },
    "bootTest": {
      "type": "boolean",
      "default": false
    }
  },
  "additionalProperties": false
}
```

## Risks and Mitigations

| Risk | Impact | Mitigation |
| --- | --- | --- |
| Users expect arbitrary ISO patching | Scope creep, brittle behavior, and unclear support expectations | Explicitly document that arbitrary ISO mutation is out of scope; fail unsupported ISO inputs early with clear diagnostics |
| Confusion between ICT-owned ISO installer and external ISO remastering | Users may assume all ISO images can be extended the same way | Distinguish between ICT-owned ISO installer evolution and external ISO remastering; require explicit adapters for any external ISO support |
| Package conflicts when extending disk images | Broken package state or failed image builds | Use native package manager dependency and conflict resolution; fail by default on unresolved conflicts |
| Repository incompatibility | Unexpected upgrades, downgrades, or dependency drift | Require explicit repository configuration, package priority policy, and generate package diff/SBOM output |
| Unsafe package downgrades | Runtime instability or loss of security fixes | Disallow downgrades by default; allow only through explicit policy if ever needed |
| Package removal requested during extension | Dependency breakage or unpredictable baseline behavior | Keep extension additive-only; require fresh composition for minimal images |
| Disk resize failure | Corrupt, unusable, or unbootable image | Support only known-good partition and filesystem combinations; validate before and after resize |
| Filesystem growth failure | Image may boot but not expose expected capacity | Support explicit filesystem grow operations only for validated filesystems; fail clearly when unsupported |
| Partition table or filesystem conversion requested | High complexity, data-loss risk, and poor ROI | Keep structural conversion out of scope; require fresh image composition for layout changes |
| Boot artifacts become stale after package installation | Image may fail to boot after kernel or initramfs changes | Detect kernel, initramfs, UKI, and bootloader changes; regenerate supported artifacts when required |
| Secure Boot chain invalidated | Image may fail on Secure Boot-enabled systems | Detect modified or unsigned boot artifacts; require signing material or fail clearly |
| dm-verity protected root modified in place | Integrity verification failure at boot | Do not mutate protected roots unless verity metadata is regenerated through a supported flow |
| Encrypted partitions cannot be modified safely | Failed extension or inaccessible root filesystem | Treat encrypted baselines as unsupported unless keys and explicit support are provided |
| Baseline OS detection fails | Incorrect package manager or boot handling | Require reliable OS metadata detection; fail when OS, architecture, or package manager cannot be identified |
| Cross-distro or mixed package ecosystems requested | Broken dependency model or unsupported package state | Do not support converting between package ecosystems; require package compatibility with the baseline OS |
| External ISO adapter maintenance burden | High validation cost across distro releases and installer variants | Add adapters only for high-value, versioned ISO families with explicit validation coverage |
| Installed target differs from expected ISO content | Packages may be added to live media but not installed system | For ICT-owned ISO flows, validate installed target content after installation, not just ISO contents |
| Image inspection or compare produces false confidence | Differences may be missed or misinterpreted | Define semantic comparison boundaries clearly; include package inventory, boot artifacts, filesystem, and metadata checks |
| CVE/SBOM data is incomplete or stale | Supply chain transparency may be misleading | Record package versions and repository sources; treat CVE checks as point-in-time reports tied to scanner/database version |
