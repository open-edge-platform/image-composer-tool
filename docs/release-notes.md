# Release Notes: Image Composer Tool

## Version 2026.1

**June 17, 2026**

**New**
- Build from scratch with `--nocache`: The `build` command now accepts a `--nocache` flag that runs the build in fresh, unique cache and workspace directories (ignoring any existing caches) and removes them once the build finishes. The final image is copied into the configured `work_dir` beforehand. `--nocache` cannot be combined with `--cache-dir` or `--work-dir`.
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

- sudo suppressed when already root: `GetFullCmdStr` detects when the process is already running as root (`euid == 0`) and omits the sudo prefix in chroot commands, avoiding permission escalation errors in CI environments that run as root.

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

**Fixed**

- `fix(ubuntu)`: `AllowPackages` not propagated to debutils.Repository (#480): The `allowPackages` list in user-provided package repository configuration was silently dropped instead of being passed through to the DEB package resolver.

- `fix(inspect)`: ext4 filesystem misdetection in image inspect (#484): The image inspect command was incorrectly classifying some ext4 partitions as a different filesystem type.

- Fixes for error logs when building UKI (#485): Spurious or incorrect error log entries emitted during UKI image construction were corrected.

- `fix(templates)`: pin intel-dlstreamer to 2025.2.0 (#492): `intel-dlstreamer` in `eLxR/RCD` templates was not version-pinned, causing uncontrolled version updates.

- `fix(templates)`: kernel version metadata 6.14 → 6.17 (#494): Template metadata version field for Ubuntu 24 kernels corrected to match the actual installed kernel series.

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
