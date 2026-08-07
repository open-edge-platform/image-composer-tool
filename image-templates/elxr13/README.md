# Wind River eLxr 13 (26.04) templates

`target.dist: elxr13` — 5 templates.

> Filenames use the `elxr-edge-26.04-` prefix while the templates declare `dist: elxr13`; the directory follows what the templates declare.

| Template | Arch | Type | Purpose | CI |
|---|---|---|---|---|
| [`elxr-edge-26.04-x86_64-minimal-initrd.yml`](./elxr-edge-26.04-x86_64-minimal-initrd.yml) | x86_64 | img | edge | — |
| [`elxr-edge-26.04-x86_64-minimal-iso.yml`](./elxr-edge-26.04-x86_64-minimal-iso.yml) | x86_64 | iso | edge | yes |
| [`elxr-edge-26.04-aarch64-minimal-raw.yml`](./elxr-edge-26.04-aarch64-minimal-raw.yml) | aarch64 | raw | edge | yes |
| [`elxr-edge-26.04-x86_64-edge-raw.yml`](./elxr-edge-26.04-x86_64-edge-raw.yml) | x86_64 | raw | edge | yes |
| [`elxr-edge-26.04-x86_64-minimal-raw.yml`](./elxr-edge-26.04-x86_64-minimal-raw.yml) | x86_64 | raw | edge | yes |

## CI coverage

4 of 5 templates here are built on every pull request (via `scripts/build_*.sh`). The others are schema-validated only, so build them locally before opening a PR.

---

See [../README.md](../README.md) for the full catalog, [../COMPOSITION.md](../COMPOSITION.md) for `extends:` and overlay mode, and [../CONVENTIONS.md](../CONVENTIONS.md) for naming rules.
