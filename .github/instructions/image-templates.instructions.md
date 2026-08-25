---
applyTo: "image-templates/**/*.yml"
---

# Image template conventions

Use these in addition to the root `copilot-instructions.md`. Schema: [os-image-template.schema.json](../../internal/config/schema/os-image-template.schema.json).

## Location and naming

`image-templates/<target.dist>/<dist>-<arch>-<purpose>-<imageType>.yml`

Examples: `emt3/emt3-x86_64-minimal-raw.yml`, `ubuntu24/ubuntu24-aarch64-edge-raw.yml`, `azl3/azl3-x86_64-minimal-iso.yml`.

- The directory is the template's `target.dist` value, not its filename prefix. So `rcd10-*.yml` (which declare `dist: el10`) live in `el10/`, and `elxr-edge-26.04-*.yml` (which declare `dist: elxr13`) live in `elxr13/`.
- The filename keeps the full `<dist>-` prefix even though the directory repeats it, so a bare filename stays unambiguous in build scripts, `internal/api/data/manifest.yaml`, and bug reports.
- `<dist>`: lowercase distro + major version (`emt3`, `azl3`, `elxr12`, `ubuntu24`, `debian13`, `rcd10`).
- `<arch>`: `x86_64` or `aarch64` (match Go's `runtime.GOARCH` convention only when the schema requires it — otherwise use these).
- `<purpose>`: `minimal`, `edge`, `dlstreamer`, `desktop-virtualization`, etc.
- `<imageType>`: `raw`, `iso`, `initrd`.

Grouping is by distribution because a chain of `extends:` templates must all be siblings in one directory and must share `os`/`dist`/`arch`/`imageType`. Any grouping drawn from those four fields can never split a legal chain; grouping by purpose would. See [image-templates/COMPOSITION.md](../../image-templates/COMPOSITION.md).

When you move or add a template, update the `scripts/build_*.sh` path that references it (24 templates are wired to a build script), and the per-distribution `README.md`.

## Reusing an existing template

If your template would be an existing one plus a few packages or commands, use `extends:` instead of copying it. `image-templates/COMPOSITION.md` covers the rules and the traps — chiefly that packages are a union with no removal syntax, and that `packageRepositories` sharing a `codename` collapse when split across layers.

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
  os: ubuntu          # must match a provider's OsName (target.os enum)
  dist: ubuntu24      # distro + major version (target.dist)
  arch: x86_64
  imageType: raw      # raw | img | iso
```

Everything else (`systemConfig` with its `packages`, `bootloader`, …, plus `disk` and `packageRepositories`) is supplied by `config/osv/{osname}/` defaults and only needs to appear when the user overrides it.

## Merge semantics — important

| Field | Behavior |
|---|---|
| `systemConfig.packages` (and nested package lists) | **Additive** — user entries are merged by name with defaults |
| `disk` | **Replace** — providing `disk` discards the OS default entirely; copy the default and edit it |
| `metadata` | Replace per top-level key |
| Scalar overrides (`image.name`, `target.dist`, …) | Replace |

If you intend to *remove* a default package, you currently cannot do that with the merge — open an issue rather than working around it.

## Authoring rules

- Always include the `metadata` block — it powers template discoverability and `image-composer-tool list`.
- Reference packages by exact name under `systemConfig.packages` (glob patterns like `wayland*` are allowed; versioned globs are not).
- Pin kernel and bootloader versions explicitly when reproducibility matters.
- Do **not** embed secrets, tokens, or private repo URLs. For repository GPG verification use `packageRepositories[].pkey` / `pkeys`.
- Keep YAML 2-space indented, no tabs. Top-level keys are `metadata`, `image`, `target`, `disk`, `systemConfig`, `packageRepositories` — keep them in that order.

## Before committing

```sh
image-composer-tool validate image-templates/<your-template>.yml
```

The validator runs the JSON schema and additional semantic checks. CI will reject templates that fail validation.

## When updating an existing template

- Bump `image.version` if behavior changes for downstream consumers.
- Note user-visible changes in `docs/user-guide/release-notes.md`.
- If you change a default in `config/osv/`, audit every template under `image-templates/` that depends on it.
