# Red Hat compatible distribution 10 templates

`target.dist: el10` — 2 templates.

> Filenames use the `rcd10-` prefix while the templates declare `dist: el10`; the directory follows what the templates declare.

| Template | Arch | Type | Purpose | CI |
|---|---|---|---|---|
| [`rcd10-x86_64-dlstreamer.yml`](./rcd10-x86_64-dlstreamer.yml) | x86_64 | raw | AI media / DL Streamer | — |
| [`rcd10-x86_64-minimal-raw.yml`](./rcd10-x86_64-minimal-raw.yml) | x86_64 | raw | minimal | — |

## CI coverage

0 of 2 templates here are built on every pull request (via `scripts/build_*.sh`). The others are schema-validated only, so build them locally before opening a PR.

---

See [../README.md](../README.md) for the full catalog, [../COMPOSITION.md](../COMPOSITION.md) for `extends:` and overlay mode, and [../CONVENTIONS.md](../CONVENTIONS.md) for naming rules.
