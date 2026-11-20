# Ubuntu 26.04 templates

`target.dist: ubuntu26` — 6 templates.

| Template | Arch | Type | Purpose | CI |
| --- | --- | --- | --- | --- |
| [`ubuntu26-x86_64-edgepack-raw.yml`](./ubuntu26-x86_64-edgepack-raw.yml) <br>*EdgePack* | x86_64 | raw | Intel EdgePack (PTL/WCL) platform enablement, desktop | — |
| [`ubuntu26-x86_64-edgepack-server-raw.yml`](./ubuntu26-x86_64-edgepack-server-raw.yml) <br>*EdgePack* | x86_64 | raw | minimal Intel EdgePack (PTL/WCL) server | — |
| [`ubuntu26-x86_64-minimal-raw.yml`](./ubuntu26-x86_64-minimal-raw.yml) | x86_64 | raw | minimal | — |
| [`ubuntu26-x86_64-nvl-bkc-overlay-raw.yml`](./ubuntu26-x86_64-nvl-bkc-overlay-raw.yml) <br>*overlay mode* | x86_64 | raw | starter for the Nova Lake (NVL) BKC overlay | — |
| [`ubuntu26-x86_64-edgepack-unattended-iso.yml`](./ubuntu26-x86_64-edgepack-unattended-iso.yml) <br>*EdgePack* | x86_64 | iso | unattended installer for an Intel EdgePack (PTL/WCL) desktop with users, SSH keys, cloud-init, provisioning | — |
| [`ubuntu26-x86_64-edgepack-server-unattended-iso.yml`](./ubuntu26-x86_64-edgepack-server-unattended-iso.yml) <br>*EdgePack* | x86_64 | iso | unattended installer for a minimal Intel EdgePack (PTL/WCL) server | — |

## EdgePack templates

The four `ubuntu26-x86_64-edgepack*` templates are the Ubuntu 26.04 counterparts
of the Ubuntu 24.04 EdgePack templates, for Intel Panther Lake (PTL) and
Wildcat Lake (WCL): a desktop and a headless server, each as a raw image and as
an unattended ISO. They add the signed EdgePack repository
(`https://apt.repos.intel.com/edgepack`), install the EdgePack Standard base,
Media and Manageability metapackages, and build the EdgePack DKMS modules for
the pinned kernel (`7.0.0-34-generic` in the raw templates, `7.0.0-38-generic`
in the unattended ISO templates). The desktop templates install Firefox as a
`.deb` from Mozilla's repository (`https://packages.mozilla.org/apt`); the
Mozilla Team PPA cannot be used on 26.04, whose apt rejects its rsa1024 key.

The unattended ISO templates need a login for their sudo `admin` user, whose
`additionalfiles/base-platform/admin.pub` ships as a placeholder with no key:
pass `--ssh-authorized-key admin=FILE`, put a real key in `admin.pub`, or add a
password hash. See [`../ubuntu24/README.md`](../ubuntu24/README.md#edgepack-templates)
for the shared details. The unattended ISO templates are reachable from the web
UI's Basic tab under Platform Enablement.

## Nova Lake BKC starter

`ubuntu26-x86_64-nvl-bkc-overlay-raw.yml` is an overlay-mode template: it layers
packages onto an installed Ubuntu 26.04 RAW image instead of building one from
scratch. The copy here is a starter that adds a single package (`vim`) so the
web UI's **Platform Enablement** / **Upstream BKC** selection for NVL (Nova
Lake) stays buildable. The full NVL BKC package set is maintained outside this
repository and replaces this file, under the same name, where it is deployed.

Before building, point `baseline.source.path` (a placeholder,
`/path/to/ubuntu-26.04-base.raw`) at an installed Ubuntu 26.04 RAW image on your
build host.

## CI coverage

0 of 6 templates here are built on every pull request (via `scripts/build_*.sh`). The others are schema-validated only, so build them locally before opening a PR.

The `build-edgepack.yml` workflow also builds `ubuntu26-x86_64-edgepack-raw.yml`
and `ubuntu26-x86_64-edgepack-server-raw.yml` when a pull request changes an
EdgePack template or the Ubuntu build code.

## Internal templates

Platform-enablement templates requiring private repositories are maintained in
the internal template repository and are not included in this public catalog.

---

See [../README.md](../README.md) for the full catalog, [../COMPOSITION.md](../COMPOSITION.md) for `extends:` and overlay mode, and [../CONVENTIONS.md](../CONVENTIONS.md) for naming rules.
