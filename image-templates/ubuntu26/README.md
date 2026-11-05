# Ubuntu 26.04 templates

`target.dist: ubuntu26` — 6 templates.

| Template | Arch | Type | Purpose | CI |
| --- | --- | --- | --- | --- |
| [`ubuntu26-x86_64-edgepack-raw.yml`](./ubuntu26-x86_64-edgepack-raw.yml) | x86_64 | raw | Intel EdgePack (PTL/WCL) platform enablement | — |
| [`ubuntu26-x86_64-edgepack-server-raw.yml`](./ubuntu26-x86_64-edgepack-server-raw.yml) | x86_64 | raw | minimal Intel EdgePack (PTL/WCL) server | — |
| [`ubuntu26-x86_64-minimal-raw.yml`](./ubuntu26-x86_64-minimal-raw.yml) | x86_64 | raw | minimal | — |
| [`ubuntu26-x86_64-nvl-bkc-overlay-raw.yml`](./ubuntu26-x86_64-nvl-bkc-overlay-raw.yml) | x86_64 | raw | Nova Lake cBKC overlay on an installed baseline | — |
| [`ubuntu26-x86_64-edgepack-unattended-iso.yml`](./ubuntu26-x86_64-edgepack-unattended-iso.yml) | x86_64 | iso | unattended installer for Intel EdgePack (PTL/WCL) with users, SSH keys, cloud-init, provisioning | — |
| [`ubuntu26-x86_64-edgepack-server-unattended-iso.yml`](./ubuntu26-x86_64-edgepack-server-unattended-iso.yml) | x86_64 | iso | unattended installer for a minimal Intel EdgePack (PTL/WCL) server | — |

## CI coverage

0 of 6 templates here are built on every pull request (via `scripts/build_*.sh`). The others are schema-validated only, so build them locally before opening a PR.

## Nova Lake cBKC overlay

The NVL template targets **bare-metal Ubuntu 26.04 (resolute), x86_64, non-RT**.
It copies an installed baseline, replaces its kernel with
`linux-image-7.3-intel_261014t080608z-resolute1` and matching headers, applies
the cBKC's Xe/serial kernel command line, and regenerates initramfs and GRUB.
The Intel APT snapshot is `20261023-0019`. The template matches the first
successful partial build: **169 active userspace requests**, comprising
49 exact vendor pins, 113 unpinned reference requests, and seven systemd
compatibility requests. The 32 vendor GStreamer pins remain commented out
because they require downgrades; this is **not full cBKC media parity**.
The temporary signed
`ubuntu-virt/lp2168017` PPA is retained for the libvirt workaround.

### Prepare and build

1. Install Ubuntu 26.04 separately with GRUB, an unencrypted non-LVM ext4 root **as the last partition**, and any desired users and swap file. No trailing swap or `/residues` partition may remain if growth is needed.
2. Shut down the installation before exporting/converting its disk. For example, `qemu-img convert -f qcow2 -O raw ubuntu26.qcow2 ubuntu26-base.raw`. This is a whole-disk image, not an ISO or root filesystem archive.
3. Set `baseline.source.path` in the template to the absolute RAW path. Alternatively, specify `format: qcow2` and its path; ICT converts it to RAW. The source is copied, never modified in place.
4. Ensure the build host can reach the Intel internal repositories. Configure appropriate internal proxy URLs and `no_proxy`/`NO_PROXY` endpoints in the host environment when required. The shared template intentionally omits site-specific proxy settings. Add `systemConfig.proxy` to your local template if the deployed image also needs proxy persistence; this does not configure build-host downloads. Overlay does not automatically persist `packageRepositories` into the guest, so signed APT sources are still written by `configurations`.

```bash
go build -o ./build/image-composer-tool ./cmd/image-composer-tool
./build/image-composer-tool validate --merged image-templates/ubuntu26/ubuntu26-x86_64-nvl-bkc-overlay-raw.yml
sudo -E ./build/image-composer-tool build image-templates/ubuntu26/ubuntu26-x86_64-nvl-bkc-overlay-raw.yml
```

Disk size is a **64 GiB grow-only floor**, with package-driven growth capped
at **128 GiB**. Existing larger baselines are never shrunk. Allow sufficient
host space for the baseline copy, downloaded packages and extracted artifacts;
the ceiling does not account for arbitrary configuration-command downloads.

### Scope and limitations

- Included: NVL NPU driver `1.38.1.20261006-37481778717`, two GuC `70.72.0` blobs, cBKC SOF firmware/topologies, and SR-IOV toolkit `v2.1-20260309T102500Z`, fetched during composition before boot regeneration.
- Included: bare-metal Wayland/Xwrapper settings and dracut resume module configuration **only when dracut is already installed**. Existing swap and initramfs generator are preserved; working hibernation is not guaranteed.
- Excluded: live partition deletion, swap recreation, blanket `apt upgrade`, GRUB purge/reinstall, lab accounts/SSH keys, passwordless sudo, autologin, disabling security/update policies, lab NTP changes and scheduled reboot. VM-specific GPU udev rules are not generated from the builder's hardware.
- Overlay preflight refuses downgrades, unlike cBKC's `--allow-downgrades`. Use a baseline with package versions no newer than the pins. Other package conflicts fail rather than removing unrelated baseline packages.
- The seven explicit systemd requests keep integration packages aligned with upgraded libraries. `systemd-timesyncd` is not requested because the tested baseline uses chrony. Do not copy a second local parent into the catalog: the shared template already contains the tested package selection; keep baseline paths and proxy settings local.
- Bootloader binaries and EFI partition are preserved. Verify Secure Boot compatibility/signing of the Intel kernel separately and confirm the generated GRUB entry matches `Ubuntu, with Linux 7.3-intel` before deploying.
- Artifact downloads require internal connectivity and currently have no independent published checksums. Out-of-repository NPU/firmware/toolkit content bypasses resolver preflight and may not appear in the overlay SBOM. NPU `.deb` dependencies must already be satisfied; `dpkg -i` fails otherwise.
- The matching local template completed an overlay build against an installed Ubuntu 26.04 Desktop baseline and reached the desktop under QEMU/KVM with Secure Boot disabled. Its site-specific proxy block was removed from the shared template. This is not NVL hardware validation or proof of media parity. See the [first-build BOM comparison](../../docs/user-guide/nvl-bom-comparison.md).

For subsequent scripts, use the workspace `cbkc-to-overlay-template` skill.

---

See [../README.md](../README.md) for the full catalog, [../COMPOSITION.md](../COMPOSITION.md) for `extends:` and overlay mode, and [../CONVENTIONS.md](../CONVENTIONS.md) for naming rules.
