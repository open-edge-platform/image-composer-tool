// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Client-side edit model for the Advanced tab's Disk step.
//
// The step seeds from the resolved template that POST /templates/compose
// returns (the same YAML `image-composer-tool resolve --full` prints), lets the
// user edit it, and emits a `disk:` block that validates against
// internal/config/schema/os-image-template.schema.json.
//
// Two translations happen here, and they are the reason this is a module rather
// than component state:
//
//  1. Sizes vs. offsets. The schema stores `start`/`end` offsets per partition;
//     there is no per-partition `size` field. Editing offsets by hand means the
//     user has to keep them contiguous, so the UI is size-oriented and the
//     offsets are computed. `end: "0"` means "rest of the disk".
//  2. Full template vs. user template. The merged YAML spells out every key,
//     including empty ones (`path: ""`, `typeUUID: ""`, `artifacts: []`) because
//     the Go structs carry no `omitempty`. Emitting those back would put noise
//     into a user template, so toDiskConfig drops anything empty.

import yaml from 'js-yaml'
import { MIB, formatSize, parseMiB, parseSize } from './size'

// Enumerated by the schema ($defs.Disk / $defs.Disk.artifacts.items).
export const PARTITION_TABLE_TYPES = ['gpt', 'mbr'] as const
export const ARTIFACT_TYPES = ['raw', 'qcow2', 'vhd', 'vhdx', 'vmdk', 'vdi', 'tar'] as const
export const COMPRESSION_TYPES = ['gz', 'gzip', 'xz', 'zstd', 'bz2'] as const

export type PartitionTableType = (typeof PARTITION_TABLE_TYPES)[number]

// First partition's start offset. Every template in this repo starts at 1MiB
// (the conventional gap for the partition table itself); it is carried on the
// model rather than hardcoded so an unedited template re-emits byte-identical
// offsets even if a future template starts elsewhere.
export const DEFAULT_FIRST_START_MIB = 1

export interface PartitionModel {
  // Stable identity for React lists and reordering. Client-only — never emitted.
  key: string
  id: string
  name: string
  type: string
  typeUUID: string
  fsType: string
  fsLabel: string
  mountPoint: string
  mountOptions: string
  flags: string[]
  // Size in MiB, or null for "use the rest of the disk" (emitted as end: "0").
  sizeMiB: number | null
}

export interface ArtifactModel {
  key: string
  type: string
  compression: string
}

export interface DiskModel {
  name: string
  // Empty when the resolved template sets none. ISO templates legitimately do —
  // never invent a default here, or the UI would silently add a disk size the
  // template never asked for.
  size: string
  maxSize: string
  partitionTableType: PartitionTableType
  extendLastPartitionToFillDisk: boolean
  artifacts: ArtifactModel[]
  partitions: PartitionModel[]
  firstStartMiB: number
  // Read from the template and preserved on emit, but not editable here: `path`
  // and `selectionPolicy` are live-installer concerns, not image layout.
  path: string
  selectionPolicy: Record<string, unknown> | null
}

// Keys are only ever compared for equality within one model, so a module
// counter is enough and keeps parse output deterministic for tests.
let keySeq = 0
const nextKey = (prefix: string): string => `${prefix}-${++keySeq}`

export function newPartition(): PartitionModel {
  return {
    key: nextKey('part'),
    id: '',
    name: '',
    type: '',
    typeUUID: '',
    fsType: 'ext4',
    fsLabel: '',
    mountPoint: '',
    mountOptions: '',
    flags: [],
    sizeMiB: 1024,
  }
}

export function newArtifact(): ArtifactModel {
  return { key: nextKey('art'), type: 'raw', compression: '' }
}

// appendPartition inserts before a trailing "rest of disk" partition rather than
// after it. Every seeded layout ends with the rootfs consuming the remainder, so
// appending blindly would push the rest partition into the middle — where its
// end of "0" leaves the partition after it with no offset to start from, and
// the computed layout overlaps.
export function appendPartition(
  partitions: PartitionModel[],
  next: PartitionModel = newPartition(),
): PartitionModel[] {
  const last = partitions.length - 1
  if (last >= 0 && partitions[last].sizeMiB === null) {
    return [...partitions.slice(0, last), next, partitions[last]]
  }
  return [...partitions, next]
}

// --- Seeding from the resolved template -------------------------------------

const str = (v: unknown): string => (typeof v === 'string' ? v : v == null ? '' : String(v))

// parseDiskFromYaml pulls the `disk:` block out of a resolved template and
// converts it to the edit model. Returns null when the YAML has no disk block
// or cannot be parsed — the caller keeps whatever model it already had rather
// than blanking the step.
export function parseDiskFromYaml(text: string): DiskModel | null {
  let doc: unknown
  try {
    doc = yaml.load(text)
  } catch {
    return null
  }
  if (typeof doc !== 'object' || doc === null) return null

  const raw = (doc as Record<string, unknown>).disk
  if (typeof raw !== 'object' || raw === null) return null
  return diskFromObject(raw as Record<string, unknown>)
}

export function diskFromObject(d: Record<string, unknown>): DiskModel {
  const rawParts = Array.isArray(d.partitions) ? (d.partitions as Record<string, unknown>[]) : []
  const firstStart = rawParts.length > 0 ? parseMiB(str(rawParts[0].start)) : null

  const table = str(d.partitionTableType).toLowerCase()

  return {
    name: str(d.name),
    size: str(d.size),
    maxSize: str(d.maxSize),
    partitionTableType: table === 'mbr' ? 'mbr' : 'gpt',
    extendLastPartitionToFillDisk: d.extendLastPartitionToFillDisk === true,
    artifacts: (Array.isArray(d.artifacts) ? (d.artifacts as Record<string, unknown>[]) : []).map(
      (a) => ({ key: nextKey('art'), type: str(a.type), compression: str(a.compression) }),
    ),
    partitions: rawParts.map(partitionFromObject),
    firstStartMiB: firstStart ?? DEFAULT_FIRST_START_MIB,
    path: str(d.path),
    selectionPolicy:
      typeof d.selectionPolicy === 'object' && d.selectionPolicy !== null
        ? (d.selectionPolicy as Record<string, unknown>)
        : null,
  }
}

function partitionFromObject(p: Record<string, unknown>): PartitionModel {
  const end = str(p.end)
  // "0" is the schema's sentinel for "rest of the disk"; anything else is an
  // absolute offset the size is derived from.
  const rest = end === '' || parseSize(end) === 0
  const startMiB = parseMiB(str(p.start))
  const endMiB = parseMiB(end)

  let sizeMiB: number | null = null
  if (!rest && startMiB !== null && endMiB !== null) {
    sizeMiB = Math.max(0, endMiB - startMiB)
  }

  return {
    key: nextKey('part'),
    id: str(p.id),
    name: str(p.name),
    type: str(p.type),
    typeUUID: str(p.typeUUID),
    fsType: str(p.fsType),
    fsLabel: str(p.fsLabel),
    mountPoint: str(p.mountPoint),
    mountOptions: str(p.mountOptions),
    flags: Array.isArray(p.flags) ? p.flags.map(str) : [],
    sizeMiB,
  }
}

// --- Size <-> offset translation --------------------------------------------

export interface Offsets {
  start: string
  end: string
}

// isRest reports whether a partition consumes the remaining disk. The last
// partition is always "rest" when extendLastPartitionToFillDisk is set — the
// builder forces its end to "0" (internal/config/config.go), so the preview has
// to agree or it would show something that isn't what gets built.
export function isRest(model: DiskModel, index: number): boolean {
  const last = index === model.partitions.length - 1
  if (last && model.extendLastPartitionToFillDisk) return true
  return model.partitions[index].sizeMiB === null
}

// computeOffsets lays the partitions out contiguously from firstStartMiB, so
// each start is the previous end. Mirrors how every template in image-templates
// is written.
export function computeOffsets(model: DiskModel): Offsets[] {
  let cursor = model.firstStartMiB
  return model.partitions.map((p, i) => {
    const start = `${cursor}MiB`
    if (isRest(model, i)) return { start, end: '0' }
    cursor += p.sizeMiB ?? 0
    return { start, end: `${cursor}MiB` }
  })
}

// usedMiB is the space the fixed-size partitions occupy, including the leading
// gap. Drives the "fits on the disk" check and the space summary.
export function usedMiB(model: DiskModel): number {
  return model.partitions.reduce(
    (total, p, i) => (isRest(model, i) ? total : total + (p.sizeMiB ?? 0)),
    model.firstStartMiB,
  )
}

// --- Emission ----------------------------------------------------------------

// toDiskConfig renders the model as the plain object for a user template's
// `disk:` block. Empty values are dropped: the resolved template spells every
// key out (the Go structs have no `omitempty`), but echoing `typeUUID: ""` back
// into a user template is noise the schema does not require.
export function toDiskConfig(model: DiskModel): Record<string, unknown> {
  const offsets = computeOffsets(model)

  const disk: Record<string, unknown> = { name: model.name }
  if (model.path) disk.path = model.path
  if (model.selectionPolicy) disk.selectionPolicy = model.selectionPolicy
  if (model.artifacts.length > 0) {
    disk.artifacts = model.artifacts
      .filter((a) => a.type !== '')
      .map((a) => (a.compression ? { type: a.type, compression: a.compression } : { type: a.type }))
  }
  if (model.size) disk.size = model.size
  if (model.maxSize) disk.maxSize = model.maxSize
  disk.partitionTableType = model.partitionTableType
  if (model.extendLastPartitionToFillDisk) disk.extendLastPartitionToFillDisk = true

  if (model.partitions.length > 0) {
    disk.partitions = model.partitions.map((p, i) => {
      // Key order follows the hand-written templates in image-templates/, so a
      // reviewer diffing the generated block against one of those sees only
      // real differences.
      const out: Record<string, unknown> = {}
      if (p.id) out.id = p.id
      if (p.name) out.name = p.name
      if (p.type) out.type = p.type
      if (p.typeUUID) out.typeUUID = p.typeUUID
      if (p.flags.length > 0) out.flags = [...p.flags]
      if (p.fsType) out.fsType = p.fsType
      if (p.fsLabel) out.fsLabel = p.fsLabel
      out.start = offsets[i].start
      out.end = offsets[i].end
      if (p.mountPoint) out.mountPoint = p.mountPoint
      if (p.mountOptions) out.mountOptions = p.mountOptions
      return out
    })
  }

  return disk
}

// diskYamlFragment renders the `disk:` block the way the Disk step previews it.
// lineWidth: -1 stops js-yaml folding long values (typeUUIDs, mount options)
// onto continuation lines, which would not match how the templates are written.
export function diskYamlFragment(model: DiskModel): string {
  return yaml.dump({ disk: toDiskConfig(model) }, { lineWidth: -1, noRefs: true })
}

// --- Validation ---------------------------------------------------------------

export interface DiskIssues {
  errors: string[]
  warnings: string[]
}

export interface DiskContext {
  // target.imageType for the resolved combination. Needed because
  // extendLastPartitionToFillDisk is constrained by it, and imageType lives
  // outside the disk block.
  imageType?: string
}

// validateDisk applies the constraints the schema and the builder enforce, so
// the user sees them here rather than at build time. It is not a substitute for
// POST /templates/validate — it only covers what this step can get wrong.
export function validateDisk(model: DiskModel, ctx: DiskContext = {}): DiskIssues {
  const errors: string[] = []
  const warnings: string[] = []

  // schema: $defs.Disk.required = ["name"]
  if (!model.name.trim()) errors.push('Disk name is required.')

  // Only the final partition can consume the remaining space: an interior
  // "rest" leaves every partition after it with no start offset.
  const restIndexes = model.partitions
    .map((_, i) => i)
    .filter((i) => model.partitions[i].sizeMiB === null)
  const lastIndex = model.partitions.length - 1
  if (restIndexes.some((i) => i !== lastIndex)) {
    errors.push('Only the last partition can use the remaining disk space.')
  }

  model.partitions.forEach((p, i) => {
    if (!isRest(model, i) && (p.sizeMiB === null || p.sizeMiB <= 0)) {
      errors.push(`Partition ${i + 1}${p.id ? ` (${p.id})` : ''} needs a size greater than zero.`)
    }
    if (!p.id && !p.name) {
      warnings.push(`Partition ${i + 1} has no id or name.`)
    }
  })

  const sizeBytes = parseSize(model.size)
  if (model.size && sizeBytes === null) {
    errors.push(`Disk size "${model.size}" is not a valid size (for example 32GiB).`)
  }
  if (sizeBytes !== null && model.partitions.length > 0) {
    const used = usedMiB(model)
    if (used * MIB > sizeBytes) {
      errors.push(
        `Partitions need ${used} MiB but the disk is ${Math.floor(sizeBytes / MIB)} MiB.`,
      )
    }
  }

  // internal/config validateDiskMaxSize: maxSize requires size, and must exceed it.
  const maxBytes = parseSize(model.maxSize)
  if (model.maxSize) {
    if (maxBytes === null) {
      errors.push(`Max size "${model.maxSize}" is not a valid size (for example 64GiB).`)
    } else if (sizeBytes === null) {
      errors.push('Max size requires a disk size to be set.')
    } else if (maxBytes <= sizeBytes) {
      errors.push('Max size must be greater than the disk size.')
    }
  }

  errors.push(...autoExpandErrors(model, ctx))

  model.artifacts.forEach((a, i) => {
    if (!a.type) errors.push(`Output artifact ${i + 1} needs a format.`)
  })

  // Not an error — the builder can produce an image without an explicit
  // artifact list (ISO templates carry none) — but worth saying out loud when
  // the user is looking at a step that offers the control.
  if (model.partitions.length === 0) {
    warnings.push('No partitions are defined.')
  }

  return { errors, warnings }
}

// autoExpandErrors mirrors validateAutoExpandLastPartitionConstraints in
// internal/config/validate/validate.go. These rules are enforced by the loader,
// not by the JSON schema, so a disk block can be schema-clean and still be
// rejected before a build starts — which is exactly what happened the first
// time this step's output was run through `image-composer-tool validate`.
//
// Not mirrored here: the rule that auto-expand requires immutability to be
// disabled. That lives in systemConfig, which this step neither reads nor
// edits; POST /templates/validate catches it.
function autoExpandErrors(model: DiskModel, ctx: DiskContext): string[] {
  if (!model.extendLastPartitionToFillDisk) return []

  const imageType = (ctx.imageType ?? '').toLowerCase()
  if (imageType === 'iso') {
    return ['Filling the remaining disk space is not supported for ISO images.']
  }
  // The loader only applies the remaining checks to raw images.
  if (imageType !== 'raw') return []

  if (model.partitions.length === 0) {
    return ['Filling the remaining disk space needs at least one partition.']
  }
  const last = model.partitions[model.partitions.length - 1]
  if (last.mountPoint !== '/') {
    return [
      `Filling the remaining disk space requires the last partition to be the root filesystem ("/"), not ${last.mountPoint === '' ? 'an unmounted partition' : `"${last.mountPoint}"`}.`,
    ]
  }
  return []
}

// suggestedSize rounds the space the partitions need up to whole GiB. Used to
// offer a disk size when the resolved template sets none.
export function suggestedSize(model: DiskModel): string {
  const gib = Math.max(1, Math.ceil((usedMiB(model) * MIB) / 1024 ** 3))
  return formatSize(gib, 'GiB')
}
