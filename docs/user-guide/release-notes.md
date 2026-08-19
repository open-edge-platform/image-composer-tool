# Release Notes: Image Composer Tool

## Version 2026.1

**June 17, 2026**

**New**

- Overlay cascade removal of orphaned reverse-dependencies: When `overlayPolicy.allowPackageRemoval` is enabled, a conflict-driven removal that orphans an unrelated baseline package (one that only `Depends:` on the removed package — for example the Debian cloud image's `cloud-initramfs-growroot` depending on `initramfs-tools`) is now resolved automatically instead of failing the build. The post-install dependency audit becomes a bounded cascade: each baseline package that is broken *after* a removal but was whole *before* it is itself removed, transitively, until the package manager's own check (`apt-get check` / `dnf check`) reports the dependency tree is whole again. The package manager's audit is the ground truth, so a dependency that an alternative still satisfies is never mistaken for breakage and nothing is over-removed. The cascade still fails **closed**: if resolving the breakage would require removing a bootloader/kernel-image package or a package the overlay is installing, the build fails. Cascade removals are folded into the preflight report's approved removals, surfaced on `InstallResult.CascadeRemoved`, and reflected in the OVERLAY PACKAGE STATISTICS summary and the complete SBOM. The behavior is entirely gated by the existing `allowPackageRemoval` flag — there is no new schema field, and with the flag off a removal that would orphan another package still fails the build.

- Overlay user provisioning with a baseline-conflict guard: Overlay builds now honor `systemConfig.users`, provisioning each account onto the baseline using the same implementation as create mode (useradd, password/hashing, groups, sudo, startup script). A requested user that **already exists in the baseline image fails the build up front** — before any resize or package install mutates the baseline — because an overlay cannot redefine a baseline account. A user's `startupScript` must reference a path present when users are created (shipped by the baseline or installed by an overlay `packages` entry), not one delivered via `additionalFiles`, which are copied later in the overlay pipeline. `systemConfig.users` is therefore no longer rejected as an unsupported overlay section.

- Robotics image composed from Canonical's cloud image via overlay + extends: two new templates show the composition features used together instead of building an OS from scratch. `image-templates/ubuntu24/ubuntu24-x86_64-robotics-hw-overlay-qcow2.yml` is an overlay base: it layers the Intel hardware-enablement stack (oneAPI runtime, Level Zero, NPU drivers, RealSense DKMS) onto Canonical's official noble server cloud image and emits a qcow2, leaving the vendor image unmodified. `ubuntu24-x86_64-robotics-jazzy-overlay-extends.yml` extends that base to add the ROS 2 Jazzy stack (ros-jazzy-desktop, OpenVINO nodes, Gazebo Harmonic, collaborative SLAM). The child declares no `baseline`, `overlayPolicy`, `packageRepositories` or `disk` — inheriting them is what keeps its packages additive to the base rather than replacing them, and keeps the base's seven package repositories (three sharing the codename `noble`) in a single layer. `ubuntu24-x86_64-robotics-jazzy-raw.yml`, which builds the equivalent image entirely from scratch, is retained for comparison. The overlay base is build- and boot-verified on Ubuntu 24.04: it emits a 1.67 GB qcow2, grows the baseline from 3.5 GB to 24 GiB, and boots under QEMU/OVMF with KVM to a login prompt on the cloud image's own kernel (6.8.0-136-generic) with 29 targets reached and no failed units; mounting the artifact confirms the overlaid packages, apt pins, udev rule and SBOM. Two caveats: the resize path needs a build host with util-linux >= 2.38 (Ubuntu 24.04+, as the README already recommends), because it reads partition start sectors via `lsblk -o PATH,START,TYPE`; and the ROS 2 child currently clears preflight but cannot finish installing, because the overlay installer passes all 2004 artifact paths in one `dpkg -i` command and the ~141932-byte command string exceeds the 131072-byte per-argument limit — batching that list is a separate fix.

- Web UI Basic tab shows the full supported matrix with unavailable combinations grayed out: The Basic tab now lists every planned vertical/SKU/platform/OS selection, including combinations whose template is not yet authored. Not-ready options appear disabled ("coming soon") in the cascading dropdowns and cannot be selected or built. Availability is driven by the manifest — a combination entry with an empty `template` is treated as planned-but-unavailable.

- Graceful cancellation on Ctrl+C / SIGTERM: interrupting a build (SIGINT or SIGTERM) now triggers cooperative cleanup before the tool exits. Chroot bind mounts (`/proc`, `/sys`, `/dev/{pts,shm}`, `/run`, and the cache-repo bind) are torn down in reverse order, loop devices attached to files under the work directory are detached, and every spawned child process (bash, sudo, mmdebstrap, apt, mksquashfs, losetup, mkfs.*, xorriso, dracut, ukify, sbsign, qemu-img, …) runs in its own process group so a single kill reaches the whole subtree. In-flight HTTPS downloads of DEB/RPM packages and repository metadata (Go-level `net/http` requests) also observe the same cancellation context, so a signal during the download stage aborts within one retry-backoff quantum instead of running to completion. The tool exits with the conventional exit code `130` after user-initiated cancellation. A second signal during cleanup is a hard exit (also `130`) so a wedged umount cannot pin the process forever. Internal deadlines (such as the 2-minute PostProcess cleanup budget) that exceed their limit surface as exit `1` — distinguishable from a user-initiated signal — so operators can tell "aborted by me" from "cleanup timed out". Any residual mount/loop that could not be reaped is logged with detail so the operator knows exactly what to clean up manually. No user-visible flag changes; the behavior is on by default.

- Overlay `additionalFiles` support: Overlay builds now honor `systemConfig.additionalFiles`, copying each host file into the baseline root at its `final` path (mirroring create-mode behavior for the Ubuntu and Debian overlay providers). The copy runs as the **last** build step — after both initramfs and GRUB regeneration — so a prebuilt boot artifact such as a custom `/boot/initrd.img-*` lands after regeneration instead of being overwritten by `update-initramfs`. Files that must instead be *consumed by* regeneration (for example initramfs-tools hooks under `/etc/initramfs-tools/`) still belong in a `systemConfig.configurations` command that runs the generator, which executes earlier in the pipeline. Unlike create mode, overlay does not auto-inject apt source/preferences/GPG files into `additionalFiles` (overlay installs from prepared artifacts, not live apt repositories), so only user-authored entries are copied.

- Overlay kernel command line & GRUB2 regeneration: Overlay builds on a GRUB2 baseline now apply `overlayPolicy.kernelCmdline` (a full-line replacement of `GRUB_CMDLINE_LINUX` in `/etc/default/grub`) and the optional `overlayPolicy.grubDefault` (a full-line replacement of `GRUB_DEFAULT`, to pin the default boot menu entry — e.g. an Ubuntu submenu path for an overlay-added flavored kernel), then regenerate the GRUB configuration with the baseline's native tool (`update-grub` / `grub-mkconfig`) after the initramfs is rebuilt. A kernel added by the overlay gets a boot menu entry automatically. The bootloader binary and the read-only ESP are never modified (`grub-install` is never run); regeneration failures fail the build so no image is emitted; a best-effort warning is logged when a Secure Boot baseline has no signing material. Bootloader- and kernel-image *replacement* remain blocked by overlay preflight.

- Overlay package removal & SBOM sidecars: Overlay builds gain two hardening features. `overlayPolicy.allowPackageRemoval` (default off, and only valid with `packageOperation: additive-and-upgrade`) permits removing a baseline package that an added package conflicts with (for example removing `initramfs-tools` so `dracut` can install); bootloader and bootable-kernel packages are never removed, removals are shown in the OVERLAY PACKAGE STATISTICS summary, and a removal that leaves an unrelated baseline package with an unmet dependency fails the build (later relaxed into a bounded cascade — see the cascade-removal note above). Overlay builds also emit SPDX SBOM sidecars next to the image: a **delta** SBOM (`<image>-<version>.delta.spdx.json`, always written) listing the overlay-contributed packages, and — when a base SBOM is available (an inherited `/usr/share/sbom` inventory or an external `baseline.source.sbomPath`) — a **complete** SBOM (`<image>-<version>.complete.spdx.json`) with the full baseline+overlay inventory. The initramfs generator is selected by what the baseline actually ships (dracut vs update-initramfs) rather than by package-manager family. Unsupported `systemConfig` sections in an overlay template (`hostname`, `network`, `initramfs`, `kernel`, `immutability`, `fde`, `bootloader`) now fail the build up front instead of being silently ignored, and overlay builds no longer inherit the create-mode OS default configuration (disk size/partitions, bootloader, kernel, and base packages come from the baseline image).

- Overlay `additionalFiles.stage` marker & inspection CLI change: A new per-file `additionalFiles.stage` field controls WHEN an overlay copies a file relative to boot/initramfs regeneration. The default (`stage: ""`, or omitted) keeps the historical behavior — the file is copied at the end of the build, after regeneration. Set `stage: pre-initramfs` to copy the file BEFORE initramfs/boot regeneration so the generator can consume it (e.g. a dracut module or an initramfs-tools hook that must be baked into the initramfs). The marker is overlay-only; create-mode builds ignore it. Separately, the overlay `--inspect` flag now defaults **off** (it previously defaulted on): when set, the post-build inspection report is written to a `<image>-<version>.inspect.txt` sidecar in the build artifacts directory instead of the console, and when unset nothing is written. The now-redundant `--no-inspect` flag has been removed — inspection is off by default, so scripts that passed `--no-inspect` to disable it should simply drop the flag.

- Overlay emits every build-mode output format: Overlay builds now honor `disk.artifacts` the same way create-mode builds do, so requesting `qcow2`, `vhd`, `vhdx`, `vmdk`, `vdi`, or `tar` (with optional compression) produces those formats instead of silently emitting only a `.raw` file. The overlay always assembles a RAW image internally, then converts it to the requested formats after the post-build RAW inspection; a template whose `disk.artifacts` omits `raw` deletes the intermediate RAW during conversion, and a build with `disk.artifacts` empty or listing only `raw` is unchanged (plain RAW, no conversion step).

- Build from scratch with `--no-cache`: The `build` command now accepts a `--no-cache` flag that runs the build in fresh, unique cache and workspace directories (ignoring any existing caches) and removes them once the build finishes. The final image is copied into the configured `work_dir` beforehand. `--no-cache` cannot be combined with `--cache-dir` or `--work-dir`.

- Template `extends` inheritance: User templates now accept an optional `extends:` field pointing at a parent template. The parent is resolved relative to the child's directory, and the chain (root → intermediate levels → leaf, up to 4 recommended levels) is folded together before OS defaults are applied — using the same per-section merge rules the two-layer user↔default merge already uses (packages additive+deduped, users merged by `name`, `additionalFiles` merged by `final` path, `disk` replaced wholesale, `kernel`/`bootloader`/`network` per-field, and so on). Cycle detection, target-match enforcement, path-containment guards, and symlink rejection all apply, and the resolved chain is logged at info level during builds. Use `image-composer-tool resolve TEMPLATE.yml` to inspect the chain-merged result before building. See [Template Extends (Inheritance)](./architecture/image-composer-tool-templates.md#template-extends-inheritance) for the full reference.

- `resolve` subcommand for template debugging: A new `image-composer-tool resolve <template.yml>` command prints the merged image template as YAML to stdout, so contributors can see exactly what the tool sees before running a build. By default the extends chain is folded without OS defaults; passing `--full` additionally merges the OS defaults, producing the exact template that would be built. Sensitive fields (user passwords, hash algorithms, and secure boot key/cert/cer paths) are always redacted in the output, and the merged view is computed on demand and never cached.

- ARM64/aarch64 cross-architecture image builds: Ubuntu 24, eLxR 12, and AZL3 images can now be composed on an x86_64 host targeting ARM64. The builder validates host-side prerequisites (arch-test, qemu-user-static), normalizes architectures for `mmdebstrap` and `dpkg`, and forces a host-side ukify execution when the host and target architectures differ.

- Ubuntu 24 ARM64 bootable server image: Added a user template and supporting configuration to produce a bootable Ubuntu 24 `aarch64` server image.

- Ubuntu 26.04 LTS (Resolute Raccoon) support: New OS target and associated configuration for Ubuntu 26.04.

- eLxR Edge 26.04 / eLxR 13 support: New OS provider, image configuration, and user templates for eLxR 13 (elxr-edge-26.04) raw image builds.

- Debian 13 user templates: New raw image template and Desktop Virtualization (IDV) ISO installer template for Debian 13.

- ROS 2 Jazzy robotics templates: New AMR raw image template and a companion ISO installer template for ROS 2 Jazzy edge robotics platforms.

- PTL PV attended and unattended ISO templates: New attended and unattended ISO installer templates for PTL (Platform Validation Toolkit) PV (Para-Virtual) configurations including cloud-init example configuration files.

- Unattended ISO installer with policy-based target disk selection: `live-installer` now supports fully automatic installation using a `selectionPolicy` block in the disk template section. Supported strategies: first, largest, fastest (prefers NVMe over SSD over HDD), and largest-free (selects the disk with the most unallocated span). Removable and externally attached disks are excluded by default and can be included explicitly with `excludeRemovable: false`.

- Declarative network configuration in image templates: A new `systemConfig.network` section defines network interfaces at image composition time. It supports `systemd-networkd` and `netplan` backends, configures DHCP, static IP/CIDR addresses, default gateways (via routes), and DNS nameservers per interface.

- Network configuration view in attended ISO installer: The attended (interactive) ISO installer now includes a "Configure Network" step that allows selecting an interface and entering DHCP or static IP/gateway/DNS settings before installation.

- Local package repository population via `packageRepositories` section: The `packageRepositories` schema now accepts a package list whose entries are HTTPS URLs (downloaded at build time) or local file/directory paths (copied). Archives (.tar, .tar.gz, .tgz, .zip) are extracted for their .deb/.rpm payloads. The `path` field is optional when `packages` is set. A temporary directory is auto-created and cleaned up. An optional `insecureSkipVerify` flag allows skipping TLS certificate verification for downloads from environments with self-signed certificates.

- Full offline/cache mode for DEB and RPM repositories: DEB Packages.gz metadata is now cached by SHA-256 checksum (`packages.parsed.json`) under `cache_dir/` and reused on rebuilds with no network access. RPM `primary.xml` metadata and `primary.location.json` are cached under `cache_dir/rpm-metadata/`. Debian repository GPG keys are cached in `cache_dir/gpg-keys/`. Repository file-existence check results and package-list URLs are cached in-process per run to eliminate redundant HEAD requests.

- DKMS module installation: Package resolution now uses a target-name-aware candidate filter (`filterCandidatesByPriorityWithTarget`) that prefers exact-name matches over Provides virtual package matches, preventing kernel packages that provide a DKMS module name from being selected instead of the actual DKMS package.

**Improved**

- RPM package cache: `DownloadPackagesComplete` now checks for a valid local cache before contacting the repository. If all required packages are present, no network request is made. Only the missing packages are re-fetched, preserving existing cached files.

- DEB package cache: `DownloadPackages` performs a staleness check against the local `.deb` cache (by name) before downloading. Version-pinned requirements and epoch-prefixed package names are matched correctly.

- Chroot environment package isolation: The chroot-build tool package cache and the initrd package cache are now stored in dedicated subdirectories (`chrootenv/` and `initrd/` respectively) to prevent the stale-cache check from evicting image packages when the two sets do not overlap.

- Chroot cleanup error handling: `CleanupChrootEnv` and `UmountChrootSysfs` now accumulate all cleanup errors rather than short-circuiting on the first failure. All partial errors are surfaced in the returned error.

- Mount rollback on failure: `mountDiskToChroot` and `MountSysfs` now roll back previously mounted paths when a later mount step fails, preventing orphaned bind mounts.

- Loop device cleanup: `LoopSetupDelete` now detects and disables any SWAP partitions on the loop device before calling `losetup -d`, preventing detach failures caused by active swap.

- Loop device error cleanup on creation failure: If loop device creation fails but a partial loop device path is returned, `BuildRawImage` now detaches it immediately rather than leaking the resource.

- Disk partition creation reliability: `createPartitionTable` now retries wipe (`wipefs`) and `sfdisk` commands in separate loops with a 30-second timeout each, verifying via `lsblk/sfdisk` that the expected state is actually reached before proceeding.

- Grub command detection in install root: `getGrubVersion` and `updateGrubConfig` now resolve grub binaries by checking known absolute paths in the install root (`/usr/sbin/`, `/usr/bin/`) before falling back to shell `command -v`. `update-grub` is now also accepted as a valid fallback.

- `apt-get` install with `--no-install-recommends`: DEB package installation in the chroot environment now passes `--no-install-recommends`, reducing unnecessary package pulls.

- sudo suppressed when already root: `GetFullCmdStr` detects when the process is already running as root (`euid == 0`) and omits the redundant inner `sudo` prefix from both chroot and host commands. ICT is launched as root (`sudo -E image-composer-tool build ...`, or the server's `sudo -n ...`), so an inner `sudo` is a root-to-root no-op that only forks an extra process per command; dropping it also avoids permission-escalation errors in CI environments that run as root. When the process is not root the prefix is kept so the per-command sudo model still elevates.

- Partition mount-point path resolution: `resolveInstallRootMountPoint` is now the single canonical function for joining the install root and partition mount points. It handles empty, /-absolute, and relative mount-point strings uniformly.

- Default installer partitioning mode: The attended ISO installer now starts in manual partitioning mode by default; partition template state is cleared when entering manual mode to avoid stale configuration.

- Installer startup scripts hardened: `attendedinstaller` and `unattendedinstaller` shell scripts replaced with `set -euo pipefail`, standardized quote handling, and `[[...]]` conditionals for more robust error propagation.

- Dual GPG key per repo for RPM EMT distro: RPM-based EMT repositories now support a second GPG public key (`pkeys` list), enabling repositories that require two signing keys.

- Boot partition label in EMT-EMF template: Explicit partition labels added to the boot partition.

- `systemd-resolved` enabled at startup for RCD: RCD image builds now enable and start `systemd-resolved` as part of post-install configuration.

- `intel-dlstreamer / OpenVINO` version alignment for RCD: Fixed version mismatch between `intel-dlstreamer` and `openvino` in RCD templates. `intel-dlstreamer` is pinned to 2025.2.0.

- `ukify` lookup paths: `shell.go` now searches additional known installation prefixes for `ukify` so builds on distributions that install it in non-standard locations do not fall back to host-side execution unnecessarily.

- Progress bar terminal output: A trailing newline is now emitted after progress bars finish (`VerifyDEBs`, `VerifyAll`, `FetchPackages`) to prevent the next log line from overwriting the progress bar.

- `CopyDir` empty-source handling: Fixed glob pattern from `/*` to `/.` so that copying an empty source directory does not produce a shell error.

- RPM dependency graph (`PkgName`): `GenerateDot` now uses the `PkgName` field for node names in dependency graphs, producing clean package names instead of raw filenames.

- Network schema validation: IPv4/IPv6 CIDR addresses, gateway addresses, and nameservers in `systemConfig.network` are now validated against typed formats in the JSON schema; DHCP and static addresses cannot be combined on the same interface.

- Debian 13 Bayonne Bridge graphics template ships a desktop terminal and GUI installer: The `debian13-x86_64-bb-graphics-raw.yml` template now adds `gnome-terminal` and `gnome-software` on top of its GNOME desktop stack (`gdm3` + `gnome-session` + `gnome-shell`). Previously the composed desktop had no terminal application in the Activities overview and no graphical way to browse or install packages, because `gnome-shell`/`gnome-session` do not pull those in (only the larger `gnome-core`/`gnome` metapackages do). Both packages merge additively under the template's inherited `additive-and-upgrade` overlay policy; the CLI `apt` is unchanged and already present.

- Image templates grouped by distribution: `image-templates/` is now organized into one subdirectory per `target.dist` (`azl3/`, `debian13/`, `el10/`, `elxr12/`, `elxr13/`, `emt3/`, `ubuntu24/`, `ubuntu26/`) instead of a single flat listing of 60 files. Filenames are unchanged, so `image-templates/ubuntu24-x86_64-minimal-raw.yml` becomes `image-templates/ubuntu24/ubuntu24-x86_64-minimal-raw.yml`. **If you reference a template by path in a script or automation, add the distribution directory.** Templates packaged into the `.deb` under `/usr/share/ict/examples/` gain the same subdirectories. Distribution is the grouping used because an `extends:` chain must be siblings in one directory and must share `os`/`dist`/`arch`/`imageType`, so a distribution directory can never split a valid chain. New guides ship alongside the templates: `image-templates/README.md` (catalog), `COMPOSITION.md` (`extends:` and overlay mode) and `CONVENTIONS.md` (naming), plus a `README.md` per distribution.

- Templates composed with `extends` instead of duplication: Several templates now inherit a base template rather than restating it. `emt3-x86_64-emf-raw.yml` and `emt3-x86_64-dlstreamer.yml` extend `emt3-x86_64-edge-raw.yml`, and `emt3-x86_64-emf-rt-raw.yml` extends `emt3-x86_64-emf-raw.yml`. This removes a 41-package block that had been copied verbatim into four EMT3 templates. Each derived template was verified with `resolve --full` to produce the same functional fields as before. Note that because package lists are a union with no removal syntax, a derived template also installs its parent's packages. And because the three EMT3 templates now inherit `emt3-x86_64-edge-raw.yml`, they also inherit its three sample repositories (`company-internal`, `dev-tools`, `intel-openvino`), so `emf-raw` and `emf-rt-raw` resolve to three repositories where they previously declared none and `dlstreamer` resolves to six rather than three. Those entries are inert — their URLs are the literal placeholder `<URL>`, which `rpmutils` skips before fetching, and EMT3 is RPM-based so apt-source generation never runs for it — so the built image is unchanged. Each of the three templates notes this in its header.

**Fixed**

- Templates in subdirectories are now discovered: template scanning walked only the top level of the templates directory and skipped subdirectories, which would have hidden every template from the AI/RAG index and the web UI template list once templates were grouped into per-distribution directories. The scan is now recursive, as are the `image-composer-*` Copilot skill scripts.

- `elxr-cloud-amd64.yml` additional files were silently dropped: the template referenced `files/etc/...` while the files ship in `elxr-cloud-amd64/files/etc/...`, so all four `additionalFiles` entries failed to resolve and were skipped with a warning rather than being copied into the image.

- Drifted RealSense apt pin in the robotics raw template: `ubuntu24-x86_64-robotics-jazzy-raw.yml` pinned `librealsense2` where its ISO counterpart pinned `librealsense2*`, leaving the RealSense sub-packages unpinned in raw images. Both templates now use the glob.

- `fix(config)`: drop stale `kernel.version` pin from the ubuntu24 OS defaults: ubuntu24 builds failed in pre-processing with `kernel version mismatch: requires kernel version "6.17", but available versions are: [6.8.0-31.31 7.0.0-28.28~24.04.1]`. Four default configs under `config/osv/ubuntu/ubuntu24/imageconfigs/defaultconfigs/` pinned `kernel.version: "6.17"` alongside the rolling metapackage `linux-image-generic-hwe-24.04`, and Ubuntu noble no longer ships 6.17. Any template without its own `kernel.version` inherited the stale pin, so `ubuntu24-x86_64-minimal-initrd.yml` and `ubuntu24-x86_64-dkms-demo.yml` failed even though the templates themselves were already clean. #765 removed this antipattern from the 12 affected user templates but did not cover the OS defaults, which is why the failure recurred; `image-templates/robotics-demo-ubuntu24-x86_64.yml` was also missed there and is fixed here. Dropping the pin lets `apt` resolve whatever the metapackage currently points to — no pin, nothing to go stale. Templates that pin a concrete kernel package (for example `linux-image-6.11.0-17-generic`, `linux-image-6.12-intel`) are unaffected.

- `fix(ubuntu)`: `AllowPackages` not propagated to debutils.Repository (#480): The `allowPackages` list in user-provided package repository configuration was silently dropped instead of being passed through to the DEB package resolver.

- `fix(inspect)`: ext4 filesystem misdetection in image inspect (#484): The image inspect command was incorrectly classifying some ext4 partitions as a different filesystem type.

- Fixes for error logs when building UKI (#485): Spurious or incorrect error log entries emitted during UKI image construction were corrected.

- `fix(templates)`: pin intel-dlstreamer to 2025.2.0 (#492): `intel-dlstreamer` in `eLxR/RCD` templates was not version-pinned, causing uncontrolled version updates.

- `fix(templates)`: kernel version metadata 6.14 → 6.17 (#494): Template metadata version field for Ubuntu 24 kernels corrected to match the actual installed kernel series.

- `fix(templates)`: pin ubuntu24 edge kernel to noble GA (6.8) (#761): The `ubuntu24-x86_64-edge-raw` and `ubuntu24-aarch64-edge-raw` templates pinned `kernel.version: 6.17` with `linux-image-generic-hwe-24.04`, a combination Ubuntu noble no longer ships — only `6.8.0-31.31` (GA) and `7.0.0-28.28~24.04.1` (HWE-edge) are available. Pin the kernel to the noble GA combination (`6.8` + `linux-image-generic`) so the `build-ubuntu24-immutable` CI job can complete.

- `fix(templates)`: drop stale `kernel.version` pin across ubuntu24 metapackage templates (#765): Every ubuntu24 template that combined a `kernel.version` string with a rolling metapackage (`linux-image-generic-hwe-24.04`, `linux-image-generic`, `linux-image-generic*`) was one HWE roll away from the same class of CI break that #494, #669, and #761 fixed. Drop the `kernel.version` line from those templates so `apt` resolves whatever the metapackage currently points to — no pin, nothing to go stale. Templates with an intentionally-pinned concrete kernel package (e.g. `linux-image-6.11.0-17-generic` in `ubuntu24-server-cloud-amd64.yml`) are unaffected.

- RPM DOT file naming bug (#538): `GenerateDot` used the raw filename (e.g., `glibc-2.38-16.azl3.x86_64.rpm`) as a node label instead of the canonical package name (`glibc`), producing incorrect dependency graphs.

- Swap partition cleanup before loop device detach (#568): Building images that include a swap partition would fail at teardown because the loop device was busy. The swap partition is now detected and disabled with `swapoff` before `losetup -d`.

- Ubuntu 24 ARM64 minimal raw template boot partition type: The `xbootldr` partition in `ubuntu24-aarch64-minimal-raw.yml` had an incorrect `fsType: vfat`. It is now corrected to `ext4`.

- Local DEB repo path in chroot: `initDebLocalRepoWithinInstallRoot` used an incorrect path separator for the `/cdrom/cache-repo` mount point inside the chroot, causing package installation failures.

- Deferred cleanup of local DEB repo: De-initialization of the local Debian repository inside the install root is now performed via a defer statement, ensuring cleanup happens even when package installation fails midway.

- `fix(scripts)`: remove Intel-internal proxy from repository configuration (#561): An Intel-internal proxy URL was hardcoded in repository configuration, causing failures in external environments.

**Known Issues**

- Unattended ISO installer is a first-pass implementation: The unattended installer (`ubuntu24-x86_64-minimal-unattended-iso.yml`) does not yet support all advanced partition layouts (e.g., `LVM`, `LUKS`). Complex partition schemes must use the attended installer or a custom startup script.

- ARM64 cross-architecture builds require host tools: Builds targeting aarch64 from an `x86_64` host require arch-test and qemu-user-static installed on the build host. The builder will detect and report missing dependencies but does not install them automatically.

- Loop devices not destroyed when image building is terminated abruptly: When the image build process is terminated abruptly using `ctrl-C`, loop devices created just prior to `ctrl-C` are not removed automatically. The loop devices must be manually removed by the user.

## Version 1.0

**December 12, 2025**

**Features**

- Support for building OS images with Intel® specific OOT Kernel packages.
- Support for building Wind River eLxr 12 images.
- Support for adding multiple Debian package repositories, e.g., Intel® and OSV.
- Ability to set priority for repositories to manage conflicts.
- Ability to prioritize specific packages to manage conflicts.
- Caching for consistent and faster composition.
- Debian repository GPG keys are now cached in `cache_dir/gpg-keys` and reused on rebuilds to avoid re-downloading.
- RPM repository metadata is now cached in `cache_dir/rpm-metadata` and reused on rebuilds to avoid network fetches.
- Native support for Debian and RPM based distributions.
- Support for building immutable OS images with DM-Verity and read-only file
  system support.
- Generation of signed OS images using provided keys for Secure Boot.
- Support for Unified Kernel Image (UKI) with systemd over UEFI BIOS or
  Legacy BIOS.
- Verbose and filtered logging based on severity to provide easy troubleshooting.
- User-defined OS image configuration.
- Seamless support for AI software stacks -
  [Edge AI Libraries](https://docs.openedgeplatform.intel.com/2025.2/ai-libraries.html)
  in user space of the OS distribution.
- Support for composing the OS images to include ECG Sample Apps.

**Known Issues/Opens**

- Installation from ISO images on NVMe SSD and via USB is not functional on
  RPL platforms.
- Face Detection and Recognition application output video is not
  displayed locally.
- Support for building Ubuntu OS images is being considered.
