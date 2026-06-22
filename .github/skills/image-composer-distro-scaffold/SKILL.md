---
name: image-composer-distro-scaffold
description: "Scaffold and validate lean new-distro onboarding in internal/provider and config/osv, with optional pilot user-template validation."
argument-hint: "quick-add --os <target.os> --dist <dist> [--create-pilot-template] | quick-validate --os <target.os> --dist <dist>"
user-invocable: true
---

# image-composer-distro-scaffold

## Overview

Create and verify new distro onboarding for this repository.

Primary zero-knowledge flow:
- `quick-add`: one command to add a new dist under an existing OS provider.
- `quick-validate`: one command to verify the onboarding and optional pilot template.

Advanced flow remains available:
- `scaffold` and `validate` for explicit low-level control.

## When to Use

- You want to support a new dist (example: add ubuntu22) without knowing internal repo layout.
- You want the skill to copy required dist assets (`config.yml`, `providerconfigs`, `chrootenvconfigs`, image configs).
- You want quick sanity checks with optional template validation.

## Usage

### Recommended: Zero-Knowledge Flow

User intent: "add ubuntu22 in the project"

```bash
python3 .github/skills/image-composer-distro-scaffold/scripts/distro-scaffold.py \
  quick-add \
  --os ubuntu \
  --dist ubuntu22 \
  --create-pilot-template
```

Then validate:

```bash
python3 .github/skills/image-composer-distro-scaffold/scripts/distro-scaffold.py \
  quick-validate \
  --os ubuntu \
  --dist ubuntu22 \
  --check-user-template user-templates/ubuntu22-x86_64-minimal-raw.yml \
  --run-template-validate \
  --run-template-validate-merged
```

`quick-add` behavior:
- Auto-detects an existing reference dist (or use `--from-dist`).
- Copies full dist tree under `config/osv/<os>/<dist>/`.
- Rewrites source dist tokens to target dist tokens in copied YAML/list files.
- Updates `internal/config/schema/os-image-template.schema.json` dist enum.
- Auto-creates an example template at `image-templates/<dist>-<arch>-minimal-<imageType>.yml`.
- Optionally creates a pilot user template.

You can skip the image-templates example generation with `--skip-example-template`.

### Advanced: Explicit Flow

```bash
python3 .github/skills/image-composer-distro-scaffold/scripts/distro-scaffold.py \
  scaffold \
  --os my-new-os \
  --dist mydist1 \
  --provider-dir myos \
  --provider-alias myos \
  --archs x86_64 \
  --image-types raw,iso \
  --dry-run
```

```bash
python3 .github/skills/image-composer-distro-scaffold/scripts/distro-scaffold.py \
  validate \
  --os my-new-os \
  --dist mydist1 \
  --provider-dir myos \
  --provider-alias myos \
  --archs x86_64 \
  --image-types raw,iso \
  --check-user-template user-templates/mydist1-minimal-raw.yml \
  --run-template-validate

# Existing-provider flow (for new dists on an existing OS provider, e.g. ubuntu22)
python3 .github/skills/image-composer-distro-scaffold/scripts/distro-scaffold.py \
  scaffold \
  --os ubuntu \
  --dist ubuntu22 \
  --provider-dir ubuntu \
  --provider-alias ubuntu \
  --archs x86_64 \
  --image-types raw,iso,initrd \
  --use-existing-provider

python3 .github/skills/image-composer-distro-scaffold/scripts/distro-scaffold.py \
  validate \
  --os ubuntu \
  --dist ubuntu22 \
  --provider-dir ubuntu \
  --provider-alias ubuntu \
  --archs x86_64 \
  --image-types raw,iso,initrd \
  --use-existing-provider \
  --check-user-template user-templates/ubuntu22-x86_64-minimal-raw.yml \
  --run-template-validate \
  --run-template-validate-merged
```

## Notes

- Use `quick-add` first for existing providers. It is the easiest and recommended path.
- `--provider-alias` must match the alias used in `cmd/image-composer-tool/build.go` imports/switch case for advanced mode.
- Validation checks structure and wiring. It does not guarantee provider business logic completeness.
- For repeatable testing, run `validate` once in pass state, then remove one expected scaffold file and confirm failure output.
- Use `--use-existing-provider` when introducing a new dist under an already-supported OS provider.
