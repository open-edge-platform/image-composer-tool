// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// What the builder actually accepts in a `disk:` block.
//
// os-image-template.schema.json constrains almost none of this: every Disk and
// Partition field except `partitionTableType` and the two artifact enums is an
// unconstrained `string`. The real rules are in the Go implementation, so this
// module transcribes them, each with the file and line it came from, and
// lib/disk.ts validates against them.
//
// Two of the schema's own enums do not match the implementation. Both are
// transcribed as the *intersection* — offering a value the schema permits and
// the builder then refuses would be a trap:
//
//   - `artifacts[].type` lists `tar`, which convertImageFile has no case for
//     (imageconvert.go:246-262 -> "unsupported image type: tar"). It is only
//     meaningful for `imageType: wsl2`, which takes a different code path.
//   - `artifacts[].compression` lists `gzip` and `bz2`, neither of which
//     compression.CompressFile implements (compression.go:58-76 ->
//     "unsupported compression type"). Conversely CompressFile implements
//     `tar.gz`/`tar.xz`, which the schema's enum rejects.
//
// Regenerating this by hand is the cost of not having the Go side publish it.
// If a build starts failing on a value this module allows, fix it here and add
// a test naming the Go source.

// --- Hard rules: the build fails outright ------------------------------------

// imagedisc.go:728 + :763 — diskPartitionCreate rejects anything else with
// "unknown fs type for partition N".
export const FS_TYPES = [
  'fat32',
  'fat16',
  'vfat',
  'ext2',
  'ext3',
  'ext4',
  'xfs',
  'linux-swap',
] as const

// imagedisc.go:96 sizeSuffixesList, enforced by VerifyFileSize (imagedisc.go:329)
// against `^(\d+)(.*)$`. Consequences worth stating, because they are stricter
// than they look and stricter than lib/size.ts's own parser:
//   - case-sensitive: "1mib" is rejected
//   - integers only: "1.5GiB" parses as num="1", suffix=".5GiB" -> rejected
//   - a bare number is rejected (empty suffix is not in the list)
//   - no TiB
// "0" is special-cased before the regex and means "rest of the disk".
export const OFFSET_SUFFIXES = ['KiB', 'MiB', 'GiB', 'K', 'M', 'G', 'KB', 'MB', 'GB'] as const

const OFFSET_RE = new RegExp(`^\\d+(${OFFSET_SUFFIXES.join('|')})$`)

// isValidOffset reports whether VerifyFileSize would accept a start/end value.
export function isValidOffset(value: string): boolean {
  return value === '0' || OFFSET_RE.test(value)
}

// isValidDiskSize reports whether disk.size / disk.maxSize would parse. Same
// suffix table and same `^(\d+)(.*)$` pattern, transcribed three times on the Go
// side already (config.go:1405, validate/validate.go:257, both mirroring
// imagedisc.TranslateSizeStrToBytes) — but without VerifyFileSize's "0" case,
// since a zero-byte disk is not a thing.
export function isValidDiskSize(value: string): boolean {
  return OFFSET_RE.test(value)
}

// --- Artifacts ---------------------------------------------------------------

// imageconvert.go:246-262 — the qemu-img conversions that exist. `raw` is the
// pass-through case (imageconvert.go:50) and needs no conversion.
export const ARTIFACT_TYPES_DISK = ['raw', 'qcow2', 'vhd', 'vhdx', 'vmdk', 'vdi'] as const

// compression.go:58-76 ∩ the schema's enum. CompressFile also implements
// tar.gz/tar.xz, but the schema rejects those, so they are unusable today.
export const COMPRESSIONS_DISK = ['gz', 'xz', 'zstd'] as const

// wsl2maker.go:106-123 — archiveFormat requires artifacts[0] to be
// `{type: tar, compression: gz}` and rejects every other combination. Unlike the
// disk path, an artifact is *required*: an empty list fails with "wsl2 image
// requires a tar artifact with compression".
export const ARTIFACT_TYPES_WSL2 = ['tar'] as const
export const COMPRESSIONS_WSL2 = ['gz', 'gzip'] as const

// Which image types run the artifact pipeline at all. `raw` reaches it via
// rawmaker.go:191 and overlay via session.go:132; `iso` (isomaker) and `img`
// (initrdmaker) never call ConvertImageFile, so an artifact list on those is
// carried in the template and then ignored.
export type ArtifactSupport = 'disk' | 'wsl2' | 'ignored'

export function artifactSupport(imageType: string): ArtifactSupport {
  switch (imageType.toLowerCase()) {
    case 'wsl2':
      return 'wsl2'
    case 'iso':
    case 'img':
      return 'ignored'
    default:
      // raw, and anything new that lands on the rawmaker/overlay path.
      return 'disk'
  }
}

// artifactOptions returns the type/compression choices that are actually
// buildable for an image type, so the dropdowns cannot offer a dead combination.
export function artifactOptions(imageType: string): {
  types: readonly string[]
  compressions: readonly string[]
  compressionRequired: boolean
} {
  if (artifactSupport(imageType) === 'wsl2') {
    return {
      types: ARTIFACT_TYPES_WSL2,
      compressions: COMPRESSIONS_WSL2,
      compressionRequired: true,
    }
  }
  return { types: ARTIFACT_TYPES_DISK, compressions: COMPRESSIONS_DISK, compressionRequired: false }
}

// --- Soft rules: the build degrades quietly rather than failing ---------------

// imagedisc.go:98-115 partitionTypeNameToGUID. PartitionTypeStrToGUID does
// return an error for an unknown name, but the caller discards it
// (imagedisc.go:814, `typeGUID, _ = ...`), so an unrecognised type means sgdisk
// is simply never given `-t` and the partition gets a default type. That is a
// warning, not an error.
export const PARTITION_TYPES = [
  'linux',
  'bios',
  'esp',
  'xbootldr',
  'linux-root-amd64',
  'linux-root-arm64',
  'linux-swap',
  'linux-home',
  'linux-srv',
  'linux-var',
  'linux-tmp',
  'linux-lvm',
  'linux-raid',
  'linux-luks',
  'linux-dm-crypt',
] as const

// imagedisc.go:79-90. Only `boot` is acted on, and only on the MBR path
// (imagedisc.go:874-880, "bootable" in the sfdisk script); the GPT path passes
// no flags to sgdisk at all. The rest are declared and used to seed defaults
// (imagedisc.go:1644-1649). An unknown flag is silently ignored.
export const PARTITION_FLAGS = ['esp', 'grub', 'bios_grub', 'bios-grub', 'boot', 'dmroot'] as const

// typeUUID goes straight to `sgdisk -t N:<value>` (imagedisc.go:812-830), so
// sgdisk is the arbiter. It accepts a full GPT GUID or one of its own short hex
// codes — config.go:439 documents the latter ("8300" for Linux filesystem).
const GUID_RE = /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/
const SGDISK_SHORTCODE_RE = /^[0-9a-fA-F]{4}$/

export function isValidTypeUUID(value: string): boolean {
  return GUID_RE.test(value) || SGDISK_SHORTCODE_RE.test(value)
}

// guidForPartitionType mirrors partitionTypeNameToGUID, used to point out when a
// hand-entered typeUUID contradicts the chosen type. Only the entries a template
// in this repo actually uses are needed for that message, but the whole table is
// carried so the check works for any type the builder knows.
export const PARTITION_TYPE_GUIDS: Record<string, string> = {
  linux: '0fc63daf-8483-4772-8e79-3d69d8477de4',
  bios: '21686148-6449-6e6f-744e-656564454649',
  esp: 'c12a7328-f81f-11d2-ba4b-00a0c93ec93b',
  xbootldr: 'bc13c2ff-59e6-4262-a352-b275fd6f7172',
  'linux-root-amd64': '4f68bce3-e8cd-4db1-96e7-fbcaf984b709',
  'linux-root-arm64': 'b921b045-1df0-41c3-af44-4c6f280d3fae',
  'linux-swap': '0657fd6d-a4ab-43c4-84e5-0933c84b4f4f',
  'linux-home': '933ac7e1-2eb4-4f13-b844-0e14e2aef915',
  'linux-srv': '3b8f8425-20e0-4f3b-907f-1a25a76f98e8',
  'linux-var': '4d21b016-b534-45c2-a9fb-5c16e091fd2d',
  'linux-tmp': '7ec6f557-3bc5-4aca-b293-16ef5df639d1',
  'linux-lvm': 'e6d6d379-f507-44c2-a23c-238f2a3df928',
  'linux-raid': 'a19d880f-05fc-4d3b-a006-743f0f84911e',
  'linux-luks': 'ca7d7ccb-63ed-4c53-861c-1742536059cc',
  'linux-dm-crypt': '7ffec5c9-2d00-49b7-8941-3ea10a5586b7',
}

// imageos.go:487 — a swap partition is detected by fsType, and it is the one
// case where a partition legitimately has no real mount point (the templates
// write `mountPoint: none`).
export function isSwap(fsType: string): boolean {
  return fsType === 'swap' || fsType === 'linux-swap'
}
