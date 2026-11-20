# Debian 13 templates

`target.dist: debian13` — 6 templates.

| Template | Arch | Type | Purpose | CI |
|---|---|---|---|---|
| [`debian13-x86_64-minimal-initrd.yml`](./debian13-x86_64-minimal-initrd.yml) | x86_64 | img | minimal | — |
| [`debian13-x86_64-minimal-iso.yml`](./debian13-x86_64-minimal-iso.yml) | x86_64 | iso | minimal | — |
| [`debian13-x86_64-bb-graphics-raw.yml`](./debian13-x86_64-bb-graphics-raw.yml) | x86_64 | raw | graphics desktop (extends the overlay base) | — |
| [`debian13-x86_64-bb-overlay-initrd-raw.yml`](./debian13-x86_64-bb-overlay-initrd-raw.yml) | x86_64 | raw | overlay base with custom initrd | — |
| [`debian13-aarch64-minimal-raw.yml`](./debian13-aarch64-minimal-raw.yml) | aarch64 | raw | minimal | — |
| [`debian13-x86_64-minimal-raw.yml`](./debian13-x86_64-minimal-raw.yml) | x86_64 | raw | minimal | — |

Overlay base and child: `debian13-x86_64-bb-overlay-initrd-raw.yml` overlays the
official Debian 13 generic cloud qcow2 with a base package set, switches the
initramfs generator to dracut with a custom module, and emits a qcow2;
`debian13-x86_64-bb-graphics-raw.yml` extends it with a graphics stack. The
overlay base is the worked example in
[Enable Networking and SSH on Bare Metal](../../docs/user-guide/configuration/configure-baremetal-network-ssh.md).

Three more Debian 13 templates live directly under `image-templates/`:
`debian13-x86_64-bb-raw.yml` and `debian13-x86_64-bb-dracut-raw.yml` (custom
initrd script via initramfs-tools or dracut; the dracut one also boots a
GNOME-on-Xorg desktop and ships a sample first-boot script — see
[Configure a Custom Script in the Initrd](../../docs/user-guide/configuration/configure-custom-initrd-script.md)),
and `debian13-x86_64-desktop-virtualization-iso.yml`.

## CI coverage

0 of 6 templates here are built on every pull request (via `scripts/build_*.sh`). The others are schema-validated only, so build them locally before opening a PR.

---

See [../README.md](../README.md) for the full catalog, [../COMPOSITION.md](../COMPOSITION.md) for `extends:` and overlay mode, and [../CONVENTIONS.md](../CONVENTIONS.md) for naming rules.
