# Wind River eLxr 12 templates

`target.dist: elxr12` — 7 templates.

| Template | Arch | Type | Purpose | CI |
|---|---|---|---|---|
| [`elxr12-x86_64-minimal-initrd.yml`](./elxr12-x86_64-minimal-initrd.yml) | x86_64 | img | minimal | — |
| [`elxr12-x86_64-minimal-iso.yml`](./elxr12-x86_64-minimal-iso.yml) | x86_64 | iso | minimal | yes |
| [`elxr12-aarch64-minimal-raw.yml`](./elxr12-aarch64-minimal-raw.yml) | aarch64 | raw | minimal | yes |
| [`elxr-cloud-amd64.yml`](./elxr-cloud-amd64.yml) | x86_64 | raw | cloud | — |
| [`elxr12-x86_64-dlstreamer.yml`](./elxr12-x86_64-dlstreamer.yml) | x86_64 | raw | AI media / DL Streamer | yes |
| [`elxr12-x86_64-edge-raw.yml`](./elxr12-x86_64-edge-raw.yml) | x86_64 | raw | edge | yes |
| [`elxr12-x86_64-minimal-raw.yml`](./elxr12-x86_64-minimal-raw.yml) | x86_64 | raw | minimal | yes |

## CI coverage

5 of 7 templates here are built on every pull request (via `scripts/build_*.sh`). The others are schema-validated only, so build them locally before opening a PR.

---

See [../README.md](../README.md) for the full catalog, [../COMPOSITION.md](../COMPOSITION.md) for `extends:` and overlay mode, and [../CONVENTIONS.md](../CONVENTIONS.md) for naming rules.
