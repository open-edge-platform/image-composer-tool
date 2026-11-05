---
name: cbkc-to-overlay-template
description: "Convert a BKC or cBKC platform-enablement shell script into an image-composer YAML template. Use for configurable BKC migration, vendor kernel and userspace enablement, internal repositories, and proxy carry-forward. Always use an existing disk-image baseline in overlay mode."
argument-hint: "script-path platform baseline-raw-path"
user-invocable: true
---

# Convert cBKC to an Overlay Template

## Inputs and safety

Read the script as data; never execute or source it. Ask for the platform branch,
OS release, architecture, RT/non-RT selection, baseline path, disk floor/ceiling,
and bare-metal versus VM deployment. Ask about ambiguous behavior rather than
copying live-system commands. Do not fetch or build images without a request.
Read the repository's template instructions and current schema before authoring.
Keep private URLs in local `user-templates/` unless the user approves a shared
template under `image-templates/`. Never carry credentials or lab SSH keys over.

## Conversion

1. Inventory the selected branch: kernel image/headers versions, full command
   line, GRUB selection, every package and version, repository snapshots and
   signing keys, proxy settings, external artifacts and runtime-only actions.
2. Name the template `<dist>-<arch>-<platform>-bkc-overlay-raw.yml`, with metadata
   and `target.imageType: raw`. Use `baseline.mode: overlay` and a whole-disk
   `baseline.source.path` with `format: raw`. If unknown, use an explicit
   `/path/to/` placeholder and document it. Never silently switch to create mode.
3. Use `overlayPolicy.packageOperation: additive-and-upgrade`,
   `conflictPolicy: fail`, and `replaceKernel.package` plus `additionalPackages`
   for headers. Translate Debian `name=version` to ICT `name_version`, including
   epochs and tildes. `replaceKernel.version` is descriptive, not a version pin.
   Use `overlayPolicy.kernelCmdline` and `grubDefault`, not create-mode
   `systemConfig.kernel` or `bootloader`. GRUB binaries/ESP remain untouched.
4. Use `disk.size` as a grow-only floor and `disk.maxSize` as the package-growth
   ceiling. Preserve the baseline partition layout. Growth requires a supported
   plain root filesystem on the final partition, not LUKS/LVM/verity. Never port
   live partition deletion, swap recreation or `grub-install` into commands.
5. Deduplicate selected packages, retaining exact versions and failing on
   conflicting pins. Include canonical install lists, not just vendor packages.
   Reproduce signed repositories through `packageRepositories`, priorities and
   public signing keys. Never disable verification to work around missing keys.
6. Use `systemConfig.proxy` for target HTTP/HTTPS/FTP proxies and bypass hosts;
   the shared writer persists login, APT and systemd settings after build-time
   configuration commands. Document host proxy exports separately. Repository
   declarations resolve packages but are not automatically persisted in overlays;
   write signed DEB822 sources and preferences with `configurations` or
   `additionalFiles`. Do not assume `/etc/environment` is sourced by each command.
7. Prefer native packages. For unavoidable out-of-repository content, use
   `configurations[].cmd` with `set -euo pipefail`, failing HTTPS downloads,
   unique temporary directories, cleanup traps and published checksum checks
   when available. Commands run after packages but before boot regeneration;
   default-stage `additionalFiles` run later and cannot feed those commands.
   Document dependency, sizing, reproducibility and SBOM limitations of downloads.
8. Exclude accounts, passwords, authorized keys, blanket sudo, autologin,
   security-policy changes, blanket upgrades, reboots and lab NTP by default.
   Preserve the baseline initramfs generator/swap. Never detect guest PCI devices
   on the builder; VM-dependent rules need explicit target-time provisioning.

## Verification and handoff

- Run current-source `validate --merged` and `resolve --full`; rebuild a stale
  binary instead of dropping supported configuration to satisfy its old schema.
- Mechanically compare every selected package/version with the source script.
  Parse YAML structurally, assert unique requests, and run `bash -n` on each
  embedded command. Do not execute configuration commands on the host.
- Run repository-required tests/lint. A valid template is not a successful build:
  report missing baseline, unverified repository availability and boot testing.
- Explain downgrade restrictions, kernel/Secure Boot requirements, baseline
  immutability, proxy environments and deliberate deviations. Update the distro
  README and release notes for shared templates.

Reference: `image-templates/ubuntu26/ubuntu26-x86_64-nvl-bkc-overlay-raw.yml`
and its neighboring README show the NVL non-RT conversion and known limitations.