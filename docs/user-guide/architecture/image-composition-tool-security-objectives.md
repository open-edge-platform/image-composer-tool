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