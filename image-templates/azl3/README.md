# Azure Linux 3 templates

`target.dist: azl3` — 6 templates.

| Template | Arch | Type | Purpose | CI |
|---|---|---|---|---|
| [`azl3-x86_64-minimal-initrd.yml`](./azl3-x86_64-minimal-initrd.yml) | x86_64 | img | minimal | — |
| [`azl3-x86_64-minimal-iso.yml`](./azl3-x86_64-minimal-iso.yml) | x86_64 | iso | minimal | yes |
| [`azl3-aarch64-edge-raw.yml`](./azl3-aarch64-edge-raw.yml) | aarch64 | raw | edge | yes |
| [`azl3-x86_64-dlstreamer.yml`](./azl3-x86_64-dlstreamer.yml) | x86_64 | raw | AI media / DL Streamer | yes |
| [`azl3-x86_64-edge-raw.yml`](./azl3-x86_64-edge-raw.yml) | x86_64 | raw | edge | yes |
| [`azl3-x86_64-minimal-raw.yml`](./azl3-x86_64-minimal-raw.yml) | x86_64 | raw | minimal | yes |

## CI coverage

5 of 6 templates here are built on every pull request (via `scripts/build_*.sh`). The others are schema-validated only, so build them locally before opening a PR.

---

See [../README.md](../README.md) for the full catalog, [../COMPOSITION.md](../COMPOSITION.md) for `extends:` and overlay mode, and [../CONVENTIONS.md](../CONVENTIONS.md) for naming rules.
