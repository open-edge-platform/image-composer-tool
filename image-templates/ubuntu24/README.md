# Ubuntu 24.04 templates

`target.dist: ubuntu24` — 27 templates.

| Template | Arch | Type | Purpose | CI |
|---|---|---|---|---|
| [`ubuntu24-x86_64-minimal-initrd.yml`](./ubuntu24-x86_64-minimal-initrd.yml) | x86_64 | img | minimal | — |
| [`ubuntu24-x86_64-minimal-iso.yml`](./ubuntu24-x86_64-minimal-iso.yml) | x86_64 | iso | minimal | yes |
| [`ubuntu24-x86_64-minimal-unattended-iso.yml`](./ubuntu24-x86_64-minimal-unattended-iso.yml) | x86_64 | iso | unattended installer | yes |
| [`ubuntu24-x86_64-robotics-jazzy-iso.yml`](./ubuntu24-x86_64-robotics-jazzy-iso.yml) | x86_64 | iso | robotics / ROS 2 | — |
| [`ubuntu24-aarch64-edge-raw.yml`](./ubuntu24-aarch64-edge-raw.yml) | aarch64 | raw | edge | — |
| [`ubuntu24-aarch64-minimal-raw.yml`](./ubuntu24-aarch64-minimal-raw.yml) | aarch64 | raw | minimal | yes |
| [`ubuntu24-aarch64-minimal-uki.yml`](./ubuntu24-aarch64-minimal-uki.yml) | aarch64 | raw | unified kernel image | — |
| [`ubuntu24-aarch64-server-cloud.yml`](./ubuntu24-aarch64-server-cloud.yml) | aarch64 | raw | cloud server | — |
| [`generic-handheld-os-template.yml`](./generic-handheld-os-template.yml) | x86_64 | raw | handheld / desktop | — |
| [`robotics-demo-ubuntu24-x86_64.yml`](./robotics-demo-ubuntu24-x86_64.yml) | x86_64 | raw | robotics / ROS 2 | — |
| [`ubuntu-minimal-cloud-amd64.yml`](./ubuntu-minimal-cloud-amd64.yml) | x86_64 | raw | cloud | — |
| [`ubuntu24-server-cloud-amd64.yml`](./ubuntu24-server-cloud-amd64.yml) | x86_64 | raw | cloud server | — |
| [`ubuntu24-x86_64-dkms-demo.yml`](./ubuntu24-x86_64-dkms-demo.yml) | x86_64 | raw | DKMS driver demo | — |
| [`ubuntu24-x86_64-dlstreamer.yml`](./ubuntu24-x86_64-dlstreamer.yml) | x86_64 | raw | AI media / DL Streamer | yes |
| [`ubuntu24-x86_64-edge-raw.yml`](./ubuntu24-x86_64-edge-raw.yml) | x86_64 | raw | edge | yes |
| [`ubuntu24-x86_64-extends-example-raw.yml`](./ubuntu24-x86_64-extends-example-raw.yml) <br>*extends `ubuntu24-x86_64-minimal-raw.yml`* | x86_64 | raw | extends demo | — |
| [`ubuntu24-x86_64-fde-raw.yml`](./ubuntu24-x86_64-fde-raw.yml) | x86_64 | raw | full-disk encryption | — |
| [`ubuntu24-x86_64-minimal-desktop-raw.yml`](./ubuntu24-x86_64-minimal-desktop-raw.yml) | x86_64 | raw | desktop | — |
| [`ubuntu24-x86_64-minimal-ptl-pv-raw.yml`](./ubuntu24-x86_64-minimal-ptl-pv-raw.yml) | x86_64 | raw | desktop (Panther Lake) | — |
| [`ubuntu24-x86_64-minimal-raw-expand-partition.yml`](./ubuntu24-x86_64-minimal-raw-expand-partition.yml) | x86_64 | raw | partition expansion | — |
| [`ubuntu24-x86_64-minimal-raw.yml`](./ubuntu24-x86_64-minimal-raw.yml) | x86_64 | raw | minimal | yes |
| [`ubuntu24-x86_64-overlay-raw.yml`](./ubuntu24-x86_64-overlay-raw.yml) <br>*overlay mode* | x86_64 | raw | overlay-mode demo | — |
| [`ubuntu24-x86_64-robotics-hw-overlay-qcow2.yml`](./ubuntu24-x86_64-robotics-hw-overlay-qcow2.yml) <br>*overlay mode* | x86_64 | raw | robotics HW enablement (overlay base) | — |
| [`ubuntu24-x86_64-robotics-jazzy-overlay-extends.yml`](./ubuntu24-x86_64-robotics-jazzy-overlay-extends.yml) <br>*extends `ubuntu24-x86_64-robotics-hw-overlay-qcow2.yml`* | x86_64 | raw | robotics / ROS 2 (overlay+extends) | — |
| [`ubuntu24-x86_64-robotics-jazzy-raw.yml`](./ubuntu24-x86_64-robotics-jazzy-raw.yml) | x86_64 | raw | robotics / ROS 2 | — |
| [`ubuntu24-x86_64-ros2.yml`](./ubuntu24-x86_64-ros2.yml) | x86_64 | raw | robotics / ROS 2 | — |
| [`ubuntu24-x86_64-agentic-wsl2.yml`](./ubuntu24-x86_64-agentic-wsl2.yml) | x86_64 | wsl2 | WSL2 agentic | — |

## Inheritance

- `ubuntu24-x86_64-extends-example-raw.yml` extends `ubuntu24-x86_64-minimal-raw.yml`

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

## CI coverage

6 of 27 templates here are built on every pull request (via `scripts/build_*.sh`). The others are schema-validated only, so build them locally before opening a PR.

---

See [../README.md](../README.md) for the full catalog, [../COMPOSITION.md](../COMPOSITION.md) for `extends:` and overlay mode, and [../CONVENTIONS.md](../CONVENTIONS.md) for naming rules.
