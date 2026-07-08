---
name: image-composer-list-os
description: "List all buildable OS targets and template details from image-templates/. Use when users ask what OSes, distros, arches, or image types are available."
argument-hint: "optional filters like --os ubuntu --arch x86_64 --image-type raw"
user-invocable: true
---

# image-composer-tool OS Availability

## Overview

Discover what can be built from `image-templates/` by listing all template metadata.
This skill answers:

- Which OS families are available
- Which distro variants are available
- Which architectures are available
- Which image types are available
- Which exact template files can be built

## When to Use

- User asks "what OS can I build?"
- User asks for available distro/arch/imageType combinations
- User needs to pick a valid template before running a build
- User wants a full inventory of all templates under `image-templates/`

## Primary Command

```bash
python3 .github/skills/image-composer-list-os/scripts/list-os-templates.py
```

## Common Filters

```bash
# Only Ubuntu templates
python3 .github/skills/image-composer-list-os/scripts/list-os-templates.py --os ubuntu

# Only x86_64 templates
python3 .github/skills/image-composer-list-os/scripts/list-os-templates.py --arch x86_64

# Only raw image templates
python3 .github/skills/image-composer-list-os/scripts/list-os-templates.py --image-type raw

# Narrow by distro + image type
python3 .github/skills/image-composer-list-os/scripts/list-os-templates.py --dist ubuntu24 --image-type raw

# Summary only
python3 .github/skills/image-composer-list-os/scripts/list-os-templates.py --summary-only
```

## Output Format

The script prints:

1. Summary counts by OS family
2. Detailed table for each template:
   - template filename
   - target.os
   - target.dist
   - target.arch
   - target.imageType
   - metadata.description

## Notes

- Source of truth is `image-templates/*.yml`
- Templates that fail to parse are reported with warnings
- This skill is read-only and does not modify templates
