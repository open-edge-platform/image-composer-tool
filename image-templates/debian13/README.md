# Debian 13 templates

`target.dist: debian13` — 7 templates.

| Template | Arch | Type | Purpose | CI |
|---|---|---|---|---|
| [`debian13-x86_64-minimal-initrd.yml`](./debian13-x86_64-minimal-initrd.yml) | x86_64 | img | minimal | — |
| [`debian13-x86_64-desktop-virtualization-iso.yml`](./debian13-x86_64-desktop-virtualization-iso.yml) | x86_64 | iso | desktop + virtualization | — |
| [`debian13-x86_64-minimal-iso.yml`](./debian13-x86_64-minimal-iso.yml) | x86_64 | iso | minimal | — |
| [`debian13-aarch64-minimal-raw.yml`](./debian13-aarch64-minimal-raw.yml) | aarch64 | raw | minimal | — |
| [`debian13-x86_64-bb-dracut-raw.yml`](./debian13-x86_64-bb-dracut-raw.yml) | x86_64 | raw | custom initrd (dracut) | — |
| [`debian13-x86_64-bb-raw.yml`](./debian13-x86_64-bb-raw.yml) <br>*extends `debian13-x86_64-minimal-raw.yml`* | x86_64 | raw | custom initrd (initramfs-tools) | — |
| [`debian13-x86_64-minimal-raw.yml`](./debian13-x86_64-minimal-raw.yml) | x86_64 | raw | minimal | — |

## Inheritance

- `debian13-x86_64-bb-raw.yml` extends `debian13-x86_64-minimal-raw.yml`

Run `image-composer-tool resolve <template> --full` to see the merged result.

## CI coverage

0 of 7 templates here are built on every pull request (via `scripts/build_*.sh`). The others are schema-validated only, so build them locally before opening a PR.

---

See [../README.md](../README.md) for the full catalog, [../COMPOSITION.md](../COMPOSITION.md) for `extends:` and overlay mode, and [../CONVENTIONS.md](../CONVENTIONS.md) for naming rules.
