# Release Notes

## Current Release

**Version**: 1.0
**Release Date**: 12th December 2025

### Features

- Support for building OS images with Intel® specific OOT Kernel packages.
- Support for building Wind River eLxr 12 images.
- Support for adding multiple Debian package repositories, e.g., Intel® and OSV.
- Ability to set priority for repositories to manage conflicts.
- Ability to prioritize specific packages to manage conflicts.
- Caching for consistent and faster composition.
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

### Known Issues/Opens

- Installation from ISO images on NVMe SSD and via USB is not functional on
  RPL platforms.
- Face Detection and Recognition application output video is not
  displayed locally.
- Support for building Ubuntu OS images is being considered.
