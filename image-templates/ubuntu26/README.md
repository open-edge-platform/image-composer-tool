# Ubuntu 26.04 templates

`target.dist: ubuntu26` — 2 templates.

| Template | Arch | Type | Purpose | CI |
|---|---|---|---|---|
| [`ubuntu26-x86_64-minimal-raw.yml`](./ubuntu26-x86_64-minimal-raw.yml) | x86_64 | raw | minimal | — |
| [`ubuntu26-x86_64-fedaero-base-iso.yml`](./ubuntu26-x86_64-fedaero-base-iso.yml) <br>*Fed Aero base platform* | x86_64 | iso | unattended installer with users, SSH keys, cloud-init, provisioning | — |

## CI coverage

0 of 2 templates here are built on every pull request (via `scripts/build_*.sh`). The others are schema-validated only, so build them locally before opening a PR.

---

See [../README.md](../README.md) for the full catalog, [../COMPOSITION.md](../COMPOSITION.md) for `extends:` and overlay mode, and [../CONVENTIONS.md](../CONVENTIONS.md) for naming rules.
