# Edge Microvisor Toolkit 3 templates

`target.dist: emt3` — 7 templates.

| Template | Arch | Type | Purpose | CI |
|---|---|---|---|---|
| [`emt3-x86_64-minimal-initrd.yml`](./emt3-x86_64-minimal-initrd.yml) | x86_64 | img | minimal | — |
| [`emt3-x86_64-minimal-iso.yml`](./emt3-x86_64-minimal-iso.yml) | x86_64 | iso | minimal | yes |
| [`emt3-x86_64-dlstreamer.yml`](./emt3-x86_64-dlstreamer.yml) <br>*extends `emt3-x86_64-edge-raw.yml`* | x86_64 | raw | AI media / DL Streamer | yes |
| [`emt3-x86_64-edge-raw.yml`](./emt3-x86_64-edge-raw.yml) | x86_64 | raw | edge | yes |
| [`emt3-x86_64-emf-raw.yml`](./emt3-x86_64-emf-raw.yml) <br>*extends `emt3-x86_64-edge-raw.yml`* | x86_64 | raw | edge multifunction | — |
| [`emt3-x86_64-emf-rt-raw.yml`](./emt3-x86_64-emf-rt-raw.yml) <br>*extends `emt3-x86_64-emf-raw.yml`* | x86_64 | raw | edge multifunction (real-time) | — |
| [`emt3-x86_64-minimal-raw.yml`](./emt3-x86_64-minimal-raw.yml) | x86_64 | raw | minimal | yes |

## Inheritance

- `emt3-x86_64-dlstreamer.yml` extends `emt3-x86_64-edge-raw.yml`
- `emt3-x86_64-emf-raw.yml` extends `emt3-x86_64-edge-raw.yml`
- `emt3-x86_64-emf-rt-raw.yml` extends `emt3-x86_64-emf-raw.yml`

Run `image-composer-tool resolve <template> --full` to see the merged result.

## CI coverage

4 of 7 templates here are built on every pull request (via `scripts/build_*.sh`). The others are schema-validated only, so build them locally before opening a PR.

---

See [../README.md](../README.md) for the full catalog, [../COMPOSITION.md](../COMPOSITION.md) for `extends:` and overlay mode, and [../CONVENTIONS.md](../CONVENTIONS.md) for naming rules.
