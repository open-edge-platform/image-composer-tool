# Ubuntu 24.04 templates

`target.dist: ubuntu24` — 34 templates.

| Template | Arch | Type | Purpose | CI |
|---|---|---|---|---|
| [`ubuntu24-x86_64-minimal-initrd.yml`](./ubuntu24-x86_64-minimal-initrd.yml) | x86_64 | img | minimal | — |
| [`ubuntu24-x86_64-minimal-iso.yml`](./ubuntu24-x86_64-minimal-iso.yml) | x86_64 | iso | minimal | yes |
| [`ubuntu24-x86_64-minimal-unattended-iso.yml`](./ubuntu24-x86_64-minimal-unattended-iso.yml) | x86_64 | iso | unattended installer | yes |
| [`ubuntu24-x86_64-edgepack-unattended-iso.yml`](./ubuntu24-x86_64-edgepack-unattended-iso.yml) <br>*EdgePack* | x86_64 | iso | unattended installer for an Intel EdgePack (PTL/WCL) desktop with users, SSH keys, cloud-init, provisioning | — |
| [`ubuntu24-x86_64-edgepack-server-unattended-iso.yml`](./ubuntu24-x86_64-edgepack-server-unattended-iso.yml) <br>*EdgePack* | x86_64 | iso | unattended installer for a minimal Intel EdgePack (PTL/WCL) server | — |
| [`ubuntu24-x86_64-edgepack-standard-only-unattended-iso.yml`](./ubuntu24-x86_64-edgepack-standard-only-unattended-iso.yml) <br>*EdgePack* | x86_64 | iso | demo: Intel EdgePack (PTL/WCL) Standard base runtime only, no domain add-ons | — |
| [`ubuntu24-aarch64-edge-raw.yml`](./ubuntu24-aarch64-edge-raw.yml) | aarch64 | raw | edge | — |
| [`ubuntu24-aarch64-minimal-raw.yml`](./ubuntu24-aarch64-minimal-raw.yml) | aarch64 | raw | minimal | yes |
| [`ubuntu24-aarch64-minimal-uki.yml`](./ubuntu24-aarch64-minimal-uki.yml) | aarch64 | raw | unified kernel image | — |
| [`ubuntu24-aarch64-server-cloud.yml`](./ubuntu24-aarch64-server-cloud.yml) | aarch64 | raw | cloud server | — |
| [`generic-handheld-os-template.yml`](./generic-handheld-os-template.yml) <br>*Fed Aero blueprint* | x86_64 | raw | handheld / desktop | — |
| [`generic-companion-os-server-template.yml`](./generic-companion-os-server-template.yml) <br>*Fed Aero blueprint* | x86_64 | raw | companion OS server | — |
| [`robotics-demo-ubuntu24-x86_64.yml`](./robotics-demo-ubuntu24-x86_64.yml) | x86_64 | raw | robotics / ROS 2 | — |
| [`ubuntu-minimal-cloud-amd64.yml`](./ubuntu-minimal-cloud-amd64.yml) | x86_64 | raw | cloud | — |
| [`ubuntu24-server-cloud-amd64.yml`](./ubuntu24-server-cloud-amd64.yml) | x86_64 | raw | cloud server | — |
| [`ubuntu24-x86_64-dkms-demo.yml`](./ubuntu24-x86_64-dkms-demo.yml) | x86_64 | raw | DKMS driver demo | — |
| [`ubuntu24-x86_64-dlstreamer.yml`](./ubuntu24-x86_64-dlstreamer.yml) | x86_64 | raw | AI media / DL Streamer | yes |
| [`ubuntu24-x86_64-edge-raw.yml`](./ubuntu24-x86_64-edge-raw.yml) | x86_64 | raw | edge | yes |
| [`ubuntu24-x86_64-edgepack-raw.yml`](./ubuntu24-x86_64-edgepack-raw.yml) <br>*EdgePack* | x86_64 | raw | Intel EdgePack (PTL/WCL) platform enablement, desktop | — |
| [`ubuntu24-x86_64-edgepack-server-raw.yml`](./ubuntu24-x86_64-edgepack-server-raw.yml) <br>*EdgePack* | x86_64 | raw | minimal Intel EdgePack (PTL/WCL) server | — |
| [`ubuntu24-x86_64-extends-example-raw.yml`](./ubuntu24-x86_64-extends-example-raw.yml) <br>*extends `ubuntu24-x86_64-minimal-raw.yml`* | x86_64 | raw | extends demo | — |
| [`ubuntu24-x86_64-fde-raw.yml`](./ubuntu24-x86_64-fde-raw.yml) | x86_64 | raw | full-disk encryption | — |
| [`ubuntu24-x86_64-generic-handheld-os-desktop-raw.yml`](./ubuntu24-x86_64-generic-handheld-os-desktop-raw.yml) <br>*extends `ubuntu24-x86_64-minimal-ptl-pv-desktop-base-raw.yml`* | x86_64 | raw | handheld desktop | — |
| [`ubuntu24-x86_64-minimal-desktop-raw.yml`](./ubuntu24-x86_64-minimal-desktop-raw.yml) | x86_64 | raw | desktop | — |
| [`ubuntu24-x86_64-minimal-ptl-pv-desktop-base-raw.yml`](./ubuntu24-x86_64-minimal-ptl-pv-desktop-base-raw.yml) | x86_64 | raw | PTL PV desktop base / extends parent | — |
| [`ubuntu24-x86_64-minimal-raw-expand-partition.yml`](./ubuntu24-x86_64-minimal-raw-expand-partition.yml) | x86_64 | raw | partition expansion | — |
| [`ubuntu24-x86_64-minimal-raw.yml`](./ubuntu24-x86_64-minimal-raw.yml) | x86_64 | raw | minimal | yes |
| [`ubuntu24-x86_64-mozilla-apt-raw.yml`](./ubuntu24-x86_64-mozilla-apt-raw.yml) | x86_64 | raw | Mozilla APT regression | — |
| [`ubuntu24-x86_64-overlay-raw.yml`](./ubuntu24-x86_64-overlay-raw.yml) <br>*overlay mode* | x86_64 | raw | overlay-mode demo | — |
| [`ubuntu24-x86_64-overlay-replace-kernel-raw.yml`](./ubuntu24-x86_64-overlay-replace-kernel-raw.yml) <br>*overlay mode* | x86_64 | raw | overlay kernel replacement (full swap) | — |
| [`ubuntu24-x86_64-robotics-hw-overlay-qcow2.yml`](./ubuntu24-x86_64-robotics-hw-overlay-qcow2.yml) <br>*overlay mode* | x86_64 | raw | robotics HW enablement (overlay base) | — |
| [`ubuntu24-x86_64-robotics-jazzy-overlay-extends.yml`](./ubuntu24-x86_64-robotics-jazzy-overlay-extends.yml) <br>*extends `ubuntu24-x86_64-robotics-hw-overlay-qcow2.yml`* | x86_64 | raw | robotics / ROS 2 (overlay+extends) | — |
| [`ubuntu24-x86_64-ros2.yml`](./ubuntu24-x86_64-ros2.yml) | x86_64 | raw | robotics / ROS 2 | — |
| [`ubuntu24-x86_64-agentic-wsl2.yml`](./ubuntu24-x86_64-agentic-wsl2.yml) | x86_64 | wsl2 | WSL2 agentic | — |

## Inheritance

- `ubuntu24-x86_64-extends-example-raw.yml` extends `ubuntu24-x86_64-minimal-raw.yml`
- `ubuntu24-x86_64-generic-handheld-os-desktop-raw.yml` extends `ubuntu24-x86_64-minimal-ptl-pv-desktop-base-raw.yml`

**Robotics on top of a vendor cloud image** — overlay + extends composed together:

```
Canonical noble cloud image (qcow2, never modified)
  -> ubuntu24-x86_64-robotics-hw-overlay-qcow2.yml       Intel oneAPI / Level Zero / NPU / RealSense
       -> ubuntu24-x86_64-robotics-jazzy-overlay-extends.yml  + ROS 2 Jazzy, OpenVINO, Gazebo, SLAM
```

`ubuntu24-x86_64-robotics-jazzy-raw.yml` is the standalone robotics equivalent, built
entirely from scratch. `ubuntu24-x86_64-overlay-raw.yml` demonstrates overlay mode on
its own.

Run `image-composer-tool resolve <template> --full` to see the merged result.

## Fed Aero blueprints

`generic-handheld-os-template.yml` and `generic-companion-os-server-template.yml`
are vendored from [`edge-node-infrastructure-blueprint`](https://github.com/open-edge-platform/edge-node-infrastructure-blueprint/tree/release-2026.2.0/infrastructure/host-os/ict)
at tag `release-2026.2.0`. They differ from upstream in two places:

- The `users:` block. Upstream ships `<USERNAME>`/`<PASSWORD>` placeholders,
  which are filled here with `user` and an empty password (set at deploy time)
  so the templates pass schema validation and stay buildable.
- The Firefox repository. Upstream takes Firefox from the Mozilla Team PPA,
  whose `InRelease` is signed with an rsa1024 key; here it comes from Mozilla's
  own repository (`https://packages.mozilla.org/apt`), limited to the `firefox`
  package with `allowPackages`.

Both target Panther Lake and pull the 6.18 Intel kernel from a pinned snapshot of
the Intel edge overlay, so re-syncing them from upstream means re-checking
`internal/api/service/data/package-repos.yaml` for new repositories — see the
drift guard in `internal/api/service/repos_test.go`.

Both are reachable from the web UI's Basic tab under Fed Aero — see the
`fed-aero` combinations in
[`internal/api/service/data/manifest.yaml`](../../internal/api/service/data/manifest.yaml).

## EdgePack templates

The five `ubuntu24-x86_64-edgepack*` templates enable Intel Panther Lake (PTL)
and Wildcat Lake (WCL) platforms with the Intel EdgePack packages. Each one adds
the signed EdgePack repository (`https://apt.repos.intel.com/edgepack`) through
`packageRepositories`, installs the EdgePack metapackages from it, and builds
the EdgePack DKMS modules for the pinned kernel through `systemConfig.dkms`.
The same templates serve both platforms.

| Template | Image | Base | EdgePack packages | Kernel pin |
|---|---|---|---|---|
| `ubuntu24-x86_64-edgepack-raw.yml` | raw | desktop (`ubuntu-desktop-minimal`) | Standard base, Media, Manageability | `7.0.0-34-generic` |
| `ubuntu24-x86_64-edgepack-server-raw.yml` | raw | headless server (`ubuntu-server-minimal`) | Standard base, Media, Manageability | `7.0.0-34-generic` |
| `ubuntu24-x86_64-edgepack-unattended-iso.yml` | unattended ISO | desktop | Standard base, Media, Manageability | `7.0.0-38-generic` |
| `ubuntu24-x86_64-edgepack-server-unattended-iso.yml` | unattended ISO | headless server | Standard base, Media, Manageability | `7.0.0-38-generic` |
| `ubuntu24-x86_64-edgepack-standard-only-unattended-iso.yml` | unattended ISO | desktop | Standard base only (demo) | `7.0.0-38-generic` |

The desktop templates install Firefox as a `.deb` from Mozilla's repository
(`https://packages.mozilla.org/apt`), because Ubuntu's own `firefox` package
only installs the snap.

The unattended ISO templates install the system hands-free and provision it with
cloud-init and a first-boot script (see the
[Unattended ISO Installer guide](../../docs/user-guide/get-started/unattended-iso-provisioning.md)).
They create a sudo `admin` user whose only credential is
`additionalfiles/base-platform/admin.pub`, which ships as a placeholder with no
key in it. A build fails until you supply a login: pass
`--ssh-authorized-key admin=FILE`, put a real key in `admin.pub`, or add a
password hash to the template. `additionalfiles/base-platform/10-platform-bootstrap.sh`
is a sample first-boot script; replace it with the provisioning your deployment
needs.

The unattended ISO templates are reachable from the web UI's Basic tab under
Platform Enablement as the **EdgePack Minimal Desktop**, **EdgePack Minimal
Server** and **EdgePack Standard Base Only (Demo)** SKUs.

## CI coverage

6 of 34 templates here are built on every pull request (via `scripts/build_*.sh`). The others are schema-validated only, so build them locally before opening a PR.

The `build-edgepack.yml` workflow also builds `ubuntu24-x86_64-edgepack-raw.yml`
and `ubuntu24-x86_64-edgepack-server-raw.yml` when a pull request changes an
EdgePack template or the Ubuntu build code.

---

See [../README.md](../README.md) for the full catalog, [../COMPOSITION.md](../COMPOSITION.md) for `extends:` and overlay mode, and [../CONVENTIONS.md](../CONVENTIONS.md) for naming rules.
