# ICT Tool Security Objectives

The ICT tool enables you to build minimal, verifiable, and secure
operating system images so you can reduce the attack surface of images and
simplify the adoption of modern boot mechanisms. The tool lets you encrypt
partitions, use dm-verity for root protection, and support Secure Boot.

## 1. Reduced Attack Surface

The ICT tool allows customization, enabling the inclusion of only
necessary kernel features and executables, reducing the overall attack surface
as defined by the user.

## 2. Secure Image Generation

To generate secure images, the tool optionally supports the following:

* Cryptographic signing of the boot chain (kernel, initial RAM disk, kernel
  command line) for Secure Boot, ensuring integrity and authenticity.
* Protect the root filesystem with dm-verity, making offline attacks
  more difficult.
* Generate a Software Bill of Materials (SBOM) for each image, providing
  transparency for its components in SPDX format which can be used with tools
  like `gradle` and `Maven`

## 3. Support for Modern Boot Mechanisms

The tool simplifies the adoption of Unified Kernel Image (UKI), a modern boot
mechanism, potentially improving edge node security by supporting secure boot.

## 4. Partition Customization

The ICT tool allows you to customize the partition layout,
including optionally encrypting root partitions, customizing partition sizes,
and adding partitions for security.

## 5. Installer Payload Deployment

`systemConfig.installerPayload` (see
[ADR: Unattended Payload ISO](../../architecture-decision-record/adr-installer-payload-iso.md))
deploys a pre-built raw disk image to target hardware instead of reinstalling
packages, with the following security properties:

* **Pre-write integrity gate.** The payload's compressed sha256 digest is
  verified against its manifest before any byte is written to the target
  disk. A mismatch aborts with the target disk untouched.
* **Fail-closed on write failure.** After the write to the target disk
  begins, a failure during the write, partition growth, or identity reset
  runs `wipefs -a` on the target so it cannot be left presenting a
  half-written, potentially bootable GPT; the installer then exits non-zero
  without rebooting. The one exception is the final EFI boot-order update:
  by then the disk is already fully written, grown, and identity-reset, so a
  failure there is reported as an error without wiping the completed disk.
* **Post-write byte-count check.** The write is verified against the
  manifest's recorded byte count to detect a truncated write.
* **Fresh machine identity per unit.** By default (`resetInstanceIdentity:
  true`), the installer clears `/etc/machine-id`, the D-Bus machine-id, SSH
  host keys, and the systemd random seed on the target disk before first
  boot, so every deployed unit gets its own identity rather than cloning the
  build host's.
* **No new shell allowlist entries.** Every command the deploy path shells
  out to (`dd`, `zstd`/`xz`/`gzip`, `sha256sum`, `wipefs`, `sgdisk`,
  `growpart`, `partx`, `udevadm`, `e2fsck`, `resize2fs`, `xfs_growfs`,
  `mkswap`) was already present in the shell command allowlist
  (`internal/utils/shell/shell.go`) before this feature; no new commands were
  added to run it.
* **No rollback after the first written byte**, by design: a whole-disk write
  is irreversible. This is a deliberate trade-off, not an oversight — see the
  ADR's Decision §3 for the full rationale.