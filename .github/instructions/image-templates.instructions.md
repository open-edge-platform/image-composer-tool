---
applyTo: "image-templates/**/*.yml"
---

# Image template conventions

Use these in addition to the root `copilot-instructions.md`. Schema: [os-image-template.schema.json](../../internal/config/schema/os-image-template.schema.json).

## Naming

`<dist>-<arch>-<purpose>-<imageType>.yml`

Examples: `emt3-x86_64-minimal-raw.yml`, `ubuntu24-aarch64-edge-raw.yml`, `azl3-x86_64-minimal-iso.yml`.

- `<dist>`: lowercase distro + major version (`emt3`, `azl3`, `elxr12`, `ubuntu24`, `debian13`, `rcd10`).
- `<arch>`: `x86_64` or `aarch64` (match Go's `runtime.GOARCH` convention only when the schema requires it — otherwise use these).
- `<purpose>`: `minimal`, `edge`, `dlstreamer`, `desktop-virtualization`, etc.
- `<imageType>`: `raw`, `iso`, `initrd`.

## Required sections for a user-facing template

```yaml
metadata:
  description: One-line summary of what this image is for.
  use_cases:
    - Short bullet
    - Another bullet
  keywords: [edge, minimal, ubuntu]

image:
  name: my-image
  version: 1.0.0

target:
  os: ubuntu          # must match a provider's OsName
  os_version: "24.04"
  arch: x86_64
```

Everything else (`packages`, `disk`, `bootloader`, …) is supplied by `config/osv/{osname}/` defaults and only needs to appear when the user overrides it.

## Merge semantics — important

| Field | Behavior |
|---|---|
| `packages` (and nested package lists) | **Additive** — user entries are merged by name with defaults |
| `disk` | **Replace** — providing `disk` discards the OS default entirely; copy the default and edit it |
| `metadata` | Replace per top-level key |
| Scalar overrides (`image.name`, `target.os_version`, …) | Replace |

If you intend to *remove* a default package, you currently cannot do that with the merge — open an issue rather than working around it.

## Authoring rules

- Always include the `metadata` block — it powers template discoverability and `image-composer-tool list`.
- Reference packages by exact name (no globs).
- Pin kernel and bootloader versions explicitly when reproducibility matters.
- Do **not** embed secrets, tokens, or private repo URLs. Use the `auth` block + env vars.
- Keep YAML 2-space indented, no tabs. Sort top-level keys: `metadata`, `image`, `target`, `packages`, `disk`, `bootloader`, `auth`, rest.

## Before committing

```sh
image-composer-tool validate -t image-templates/<your-template>.yml
```

The validator runs the JSON schema and additional semantic checks. CI will reject templates that fail validation.

## When updating an existing template

- Bump `image.version` if behavior changes for downstream consumers.
- Note user-visible changes in `docs/release-notes.md`.
- If you change a default in `config/osv/`, audit every template under `image-templates/` that depends on it.
