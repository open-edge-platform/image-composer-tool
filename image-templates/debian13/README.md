# Debian 13 templates

`target.dist: debian13` — 4 templates.

| Template | Arch | Type | Purpose | CI |
|---|---|---|---|---|
| [`debian13-x86_64-minimal-initrd.yml`](./debian13-x86_64-minimal-initrd.yml) | x86_64 | img | minimal | — |
| [`debian13-x86_64-minimal-iso.yml`](./debian13-x86_64-minimal-iso.yml) | x86_64 | iso | minimal | — |
| [`debian13-aarch64-minimal-raw.yml`](./debian13-aarch64-minimal-raw.yml) | aarch64 | raw | minimal | — |
| [`debian13-x86_64-minimal-raw.yml`](./debian13-x86_64-minimal-raw.yml) | x86_64 | raw | minimal | — |

## CI coverage

0 of 4 templates here are built on every pull request (via `scripts/build_*.sh`). The others are schema-validated only, so build them locally before opening a PR.

---

See [../README.md](../README.md) for the full catalog, [../COMPOSITION.md](../COMPOSITION.md) for `extends:` and overlay mode, and [../CONVENTIONS.md](../CONVENTIONS.md) for naming rules.
