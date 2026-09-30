# Ubuntu 26.04 templates

`target.dist: ubuntu26` — 3 templates.

| Template | Arch | Type | Purpose | CI |
|---|---|---|---|---|
| [`ubuntu26-x86_64-edgepack-raw.yml`](./ubuntu26-x86_64-edgepack-raw.yml) | x86_64 | raw | Intel EdgePack (PTL/WCL) platform enablement | — |
| [`ubuntu26-x86_64-edgepack-server-raw.yml`](./ubuntu26-x86_64-edgepack-server-raw.yml) | x86_64 | raw | minimal Intel EdgePack (PTL/WCL) server | — |
| [`ubuntu26-x86_64-minimal-raw.yml`](./ubuntu26-x86_64-minimal-raw.yml) | x86_64 | raw | minimal | — |

## CI coverage

0 of 3 templates here are built on every pull request (via `scripts/build_*.sh`). The others are schema-validated only, so build them locally before opening a PR.

---

See [../README.md](../README.md) for the full catalog, [../COMPOSITION.md](../COMPOSITION.md) for `extends:` and overlay mode, and [../CONVENTIONS.md](../CONVENTIONS.md) for naming rules.
