# Template conventions

Rules for adding or editing a template here. The authoritative machine-readable
version lives in
[`.github/instructions/image-templates.instructions.md`](../.github/instructions/image-templates.instructions.md);
this file is the human explanation plus the quirks of the existing tree.

## Where a template goes

In the directory named after its `target.dist`:

```
image-templates/<target.dist>/<dist>-<arch>-<purpose>-<imageType>.yml
```

The directory is chosen by what the file *declares*, not by what it is called.
That produces three mismatches worth knowing about, all pre-existing:

| Directory | Filenames inside | Why |
|---|---|---|
| `el10/` | `rcd10-*.yml` | the files declare `dist: el10` |
| `elxr13/` | `elxr-edge-26.04-*.yml` | the files declare `dist: elxr13` |

Also: files named `*-initrd.yml` declare `imageType: img`, not `initrd`. And
three cloud templates use `amd64` in the filename where everything else uses
`x86_64`, though all of them declare `arch: x86_64`. These are left alone
deliberately — renaming them would churn build scripts, the API manifest and
documentation for no functional gain.

## Filenames

`<dist>-<arch>-<purpose>-<imageType>.yml`

- `<dist>` — lowercase distribution and major version: `ubuntu24`, `emt3`,
  `azl3`, `elxr12`, `debian13`, `rcd10`.
- `<arch>` — `x86_64` or `aarch64`.
- `<purpose>` — `minimal`, `edge`, `dlstreamer`, `robotics-jazzy`,
  `desktop-virtualization`, …
- `<imageType>` — `raw`, `iso`, `initrd`.

The filename repeats the distribution that the directory already carries. That
is on purpose: a bare filename still identifies a template unambiguously in a
build script, a grep, a bug report, or `internal/api/data/manifest.yaml`.

## Required content

```yaml
metadata:                     # always include — powers discovery and search,
  description: One line.      # and is NOT inherited through extends
  use_cases:
    - Short bullet
  keywords: [edge, minimal, ubuntu]

image:
  name: my-image
  version: 1.0.0

target:
  os: ubuntu                  # must match a provider's OsName
  dist: ubuntu24
  arch: x86_64
  imageType: raw              # raw | img | iso | wsl2
```

Everything else — `systemConfig`, `disk`, `packageRepositories` — comes from the
`config/osv/` defaults and only needs to appear where you override it.

Top-level keys stay in this order: `metadata`, `image`, `target`, `disk`,
`systemConfig`, `packageRepositories`. Two-space indent, no tabs.

## Authoring rules

- Name packages exactly. Globs like `wayland*` are allowed; versioned globs are
  not.
- Pin kernel and bootloader versions when reproducibility matters.
- Never commit secrets, tokens or private repository URLs. Use
  `packageRepositories[].pkey` for GPG verification.
- Relative `additionalFiles.local` paths are resolved against the directory of
  each template in the chain. From inside a distribution directory, the shared
  files are `../additionalfiles/…` and repository config is `../../config/…`.
- To *remove* a package that a default or a parent installs: you cannot. Open an
  issue rather than working around it.

## Before you commit

```bash
./image-composer-tool validate image-templates/<dist>/<your-template>.yml
```

If your template uses `extends:`, also check what it merges to:

```bash
./image-composer-tool resolve image-templates/<dist>/<your-template>.yml --full
```

Only 24 of the templates here are built by CI. If yours is not one of them,
build it locally before opening a PR — nothing else will catch a break.

When editing an existing template: bump `image.version` if behaviour changes,
and note user-visible changes in
[`docs/user-guide/release-notes.md`](../docs/user-guide/release-notes.md). If you
change a `config/osv/` default, check every template that relies on it.

## Adding a distribution

1. Create `image-templates/<dist>/` and add at least a
   `<dist>-<arch>-minimal-raw.yml`.
2. Add a `README.md` to the directory following the pattern of its siblings.
3. Add the row to the table in [README.md](./README.md).
4. Populate `config/osv/<os>/<dist>/` — see
   [the architecture guide](../docs/user-guide/architecture/architecture.md).

Note that template discovery walks this tree recursively, so a new directory is
picked up automatically by the AI/RAG index and the web UI.
