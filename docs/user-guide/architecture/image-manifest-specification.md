# Image Manifest Specification

As with any system that uses an A/B updates mechanism to update the system
reliably, the manifest file is crucial for specifying the update metadata,
including the partition information and update instructions. The manifest file
describes the contents of the update package, the way it's applied, and how to
switch between multiple images.

Here are the key aspects of the A/B update manifest. This information is used
prior to booting a system image to prepare the storage layout and select the
system version to boot, among other things.

**Partition information:** The manifest specifies the partitions to be updated,
their sizes, and other metadata.

**Update instructions:** It defines how the update process works, including
mount points, device mapper configurations, and any necessary
post-installation scripts.

**Slot management:** The manifest guides the update process to select the
appropriate slot (A or B) for updating and switching to the updated slot after
the update is complete.

**Dynamic partitions:** For devices with dynamic partitions, the manifest
includes additional information on how to handle groups and partitions.

**Update metadata:** The manifest contains metadata about the update, including
the update's version number, type (full or delta), and any necessary conditions
for the update to apply.

## Image Manifest Format

The following is an image manifest's minimal format that the ICT
tool can output along with the system image it creates:

```text
Software_package_manifest {
   // Manifest schema version: bump this if you ever change the manifest format
"schema_version": "1.0",

// The update payload version—used for “newer than” comparison, assume this is what you meant by package version?
"image_version": "2025.05.15-rc1",

// Time stamp (helps with debugging / auditing)
"built_at": "2025-05-15T08:30:00Z",

// Which device or hardware revisions this image targets
"arch" : "x86_64",

// How much disk space the image needs
"size_bytes": 104857600,

// a cryptographic hash of the raw image (use SHA-256, not CRC)
"hash": "3b7f...d9ae",
"hash_alg": "SHA-256",

// authenticity: signature over the manifest. Need one additional for the image itself if we are signing
"signature": "MEUCIQDU...",
"sig_alg": "ECDSA+SHA256",

// 9. minimum current version required to apply this update (perhaps not needed, not sure...)
"min_current_version": "2025.04.01",
}
```
