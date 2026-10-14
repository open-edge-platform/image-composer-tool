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
//     there is no per-partition `size` field. Keeping offsets contiguous by hand
//     is work, so size mode holds a size per partition and derives the offsets;
//     offset mode does the reverse. `end: "0"` means "rest of the disk".
//  2. Full template vs. user template. The merged YAML spells out every key,
//     including empty ones (`path: ""`, `typeUUID: ""`, `artifacts: []`) because
//     the Go structs carry no `omitempty`. Emitting those back would put noise
//     into a user template, so toDiskConfig drops anything empty.
//
// What counts as a *valid* value for each field lives in ./diskrules.ts, which
// transcribes the Go implementation's allowlists — the JSON schema types nearly
// every Disk and Partition field as an unconstrained string.

import yaml from 'js-yaml'
import { MIB, formatSize, parseMiB, parseSize } from './size'
import {
  FS_TYPES,
  OFFSET_SUFFIXES,
  PARTITION_FLAGS,
  PARTITION_TYPES,
  PARTITION_TYPE_GUIDS,
  artifactOptions,
  artifactSupport,
  isValidDiskSize,
  isValidOffset,
  isValidTypeUUID,
} from './diskrules'

// Enumerated by the schema ($defs.Disk.partitionTableType), and the one Disk
// enum the implementation agrees with. The artifact type/compression enums are
// *not* re-exported here on purpose: the schema's versions include values the
// builder cannot produce, so lib/diskrules.ts owns those and there is only one
// place to look.
export const PARTITION_TABLE_TYPES = ['gpt', 'mbr'] as const

export type PartitionTableType = (typeof PARTITION_TABLE_TYPES)[number]

// Table types that are in the schema but not offered for selection, each with
// the reason shown on the disabled control. Kept visible-but-disabled rather
// than hidden, matching how the Packages step handles an unavailable choice: a
// template that already declares one still renders its real value honestly
// instead of silently reading as GPT.
export const PARTITION_TABLE_DISABLED: Partial<Record<PartitionTableType, string>> = {
  mbr: 'MBR is not yet enabled.',
}

// isPartitionTableSelectable reports whether the user may switch to a table type.
export function isPartitionTableSelectable(t: PartitionTableType): boolean {
  return PARTITION_TABLE_DISABLED[t] === undefined
}

// How the partition table is edited.
//
//  - 'size'   — one size per partition; start/end are derived contiguously.
//               Safe default: the invariant that each start equals the previous
//               end cannot be broken, because it is never stored.
//  - 'offset' — start/end are typed directly, exactly as the schema stores them.
//               Sizes become derived. Gaps and overlaps become possible, so they
//               are reported rather than prevented.
//
// Switching converts the current layout in place (`setLayoutMode`), so neither
// mode is lossy: the model always carries both sizeMiB and start/end, and the
// mode only decides which side is authoritative.
export const LAYOUT_MODES = ['size', 'offset'] as const
export type LayoutMode = (typeof LAYOUT_MODES)[number]

// First partition's start offset. Every template in this repo starts at 1MiB
// (the conventional gap for the partition table itself); it is carried on the
// model rather than hardcoded so an unedited template re-emits byte-identical
// offsets even if a future template starts elsewhere.
export const DEFAULT_FIRST_START_MIB = 1

export interface PartitionModel {
  // Stable identity for React lists and reordering. Client-only — never emitted.
  key: string
  id: string
  // Partition index (sdX). Optional in the schema and unused by every template
  // in this repo, but carried so a template that sets one round-trips.
  index: number | null
  name: string
  type: string
  typeUUID: string
  fsType: string
  fsLabel: string
  mountPoint: string
  mountOptions: string
  flags: string[]
  // Authoritative in 'size' mode: size in MiB, or null for "use the rest of the
  // disk" (emitted as end: "0").
  sizeMiB: number | null
  // Authoritative in 'offset' mode: the schema's own offset strings. Seeded
  // verbatim from the template, and kept in step with sizeMiB by setLayoutMode.
  start: string
  end: string
}

export interface ArtifactModel {
  key: string
  type: string
  compression: string
}

export interface DiskModel {
  // Which side of the size/offset pair the user is editing. Seeded to 'size'.
  layoutMode: LayoutMode
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
    index: null,
    name: '',
    type: '',
    typeUUID: '',
    fsType: 'ext4',
    fsLabel: '',
    mountPoint: '',
    mountOptions: '',
    flags: [],
    sizeMiB: 1024,
    start: '',
    end: '',
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
//
// In 'offset' mode the new partition also needs offsets: it is slotted into the
// gap it is displacing, starting where its predecessor ended. That is a
// starting point to edit, not a guarantee — the partition after it is not moved,
// so an overlap is likely and validateDisk will say so.
export function appendPartition(
  model: DiskModel,
  next: PartitionModel = newPartition(),
): PartitionModel[] {
  const parts = model.partitions
  const last = parts.length - 1
  const insertAt = last >= 0 && isRest(model, last) ? last : parts.length

  if (model.layoutMode === 'offset') {
    const prevEnd = insertAt > 0 ? parseMiB(parts[insertAt - 1].end) : null
    const startMiB = prevEnd ?? model.firstStartMiB
    next = {
      ...next,
      start: `${startMiB}MiB`,
      end: `${startMiB + (next.sizeMiB ?? 1024)}MiB`,
    }
  }
  return [...parts.slice(0, insertAt), next, ...parts.slice(insertAt)]
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
    // Seeded size-oriented: the safe mode, and the one the templates are
    // effectively written in (every layout in this repo is contiguous).
    layoutMode: 'size',
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
  // null ("rest of the disk") also covers offsets that don't parse.
  const span = spanMiB(str(p.start), end)
  const sizeMiB = typeof span === 'number' ? Math.max(0, span) : null

  return {
    key: nextKey('part'),
    id: str(p.id),
    index: typeof p.index === 'number' ? p.index : null,
    name: str(p.name),
    type: str(p.type),
    typeUUID: str(p.typeUUID),
    fsType: str(p.fsType),
    fsLabel: str(p.fsLabel),
    mountPoint: str(p.mountPoint),
    mountOptions: str(p.mountOptions),
    flags: Array.isArray(p.flags) ? p.flags.map(str) : [],
    sizeMiB,
    // Kept verbatim so 'offset' mode starts from exactly what the template said,
    // not from a value round-tripped through MiB.
    start: str(p.start),
    end,
  }
}

// --- Size <-> offset translation --------------------------------------------

export interface Offsets {
  start: string
  end: string
}

// fromEndMiB returns the distance of an offset measured back from the end of the
// disk ("-20GiB", imagedisc.go parseBoundary), or null when value is not one.
function fromEndMiB(value: string): number | null {
  const v = value.trim()
  if (!v.startsWith('-')) return null
  const mib = parseMiB(v.slice(1))
  return mib !== null && mib > 0 ? mib : null
}

// "0" is the schema's sentinel for the end of the disk.
const isDiskEnd = (end: string): boolean => end === '' || parseSize(end) === 0

// spanMiB is the size between start and end: null when it runs from an absolute
// start into the end of the disk, so its size depends on the disk; undefined
// when the offsets don't parse. A partition anchored to the end ("-4GiB" to "0")
// has a fixed size.
function spanMiB(start: string, end: string): number | null | undefined {
  const startBack = fromEndMiB(start)
  if (startBack !== null) {
    const endBack = isDiskEnd(end) ? 0 : fromEndMiB(end)
    return endBack === null ? undefined : startBack - endBack
  }
  if (isDiskEnd(end) || fromEndMiB(end) !== null) return null
  const s = parseMiB(start)
  const e = parseMiB(end)
  return s === null || e === null ? undefined : e - s
}

// fromEndOffset writes a distance back from the end of the disk the way the
// templates do: whole GiB where it can be.
const fromEndOffset = (mib: number): string =>
  mib % 1024 === 0 ? `-${mib / 1024}GiB` : `-${mib}MiB`

// isRest reports whether a partition consumes the remaining disk. The last
// partition is always "rest" when extendLastPartitionToFillDisk is set — the
// builder forces its end to "0" (internal/config/config.go), so the preview has
// to agree or it would show something that isn't what gets built.
export function isRest(model: DiskModel, index: number): boolean {
  const p = model.partitions[index]
  if (index === model.partitions.length - 1 && model.extendLastPartitionToFillDisk) return true
  if (model.layoutMode === 'offset') return p.end === '' || parseSize(p.end) === 0
  return p.sizeMiB === null
}

// computeOffsets returns the start/end pair each partition will be emitted with.
//
// In 'size' mode the partitions are laid out contiguously from firstStartMiB, so
// each start is the previous end — mirroring how every template in
// image-templates is written. Partitions after the one using the remaining space
// are laid out back from the end of the disk ("-20GiB"), as the EdgePack
// templates are. In 'offset' mode the user's own strings are returned untouched;
// whether they are contiguous is up to them, and validateDisk reports gaps and
// overlaps.
export function computeOffsets(model: DiskModel): Offsets[] {
  if (model.layoutMode === 'offset') {
    return model.partitions.map((p, i) => ({
      start: p.start,
      end: isRest(model, i) ? '0' : p.end,
    }))
  }
  const restAt = model.partitions.findIndex((_, i) => isRest(model, i))
  let back =
    restAt < 0 ? 0 : model.partitions.slice(restAt + 1).reduce((total, p) => total + (p.sizeMiB ?? 0), 0)
  let cursor = model.firstStartMiB
  return model.partitions.map((p, i) => {
    if (restAt >= 0 && i > restAt) {
      const start = fromEndOffset(back)
      back -= p.sizeMiB ?? 0
      return { start, end: back > 0 ? fromEndOffset(back) : '0' }
    }
    const start = `${cursor}MiB`
    if (i === restAt) return { start, end: back > 0 ? fromEndOffset(back) : '0' }
    cursor += p.sizeMiB ?? 0
    return { start, end: `${cursor}MiB` }
  })
}

// partitionSizeMiB is the size to display for a partition: the stored value in
// 'size' mode, and the span between the offsets in 'offset' mode. null means
// "the rest of the disk", and undefined means the offsets don't parse.
export function partitionSizeMiB(model: DiskModel, index: number): number | null | undefined {
  const p = model.partitions[index]
  if (model.layoutMode === 'size') return isRest(model, index) ? null : p.sizeMiB
  return spanMiB(p.start, isRest(model, index) ? '0' : p.end)
}

// setLayoutMode switches which side of the size/offset pair is authoritative,
// converting the layout in place so nothing is lost either way: leaving 'size'
// writes the computed offsets down, and leaving 'offset' derives sizes from
// whatever offsets the user ended up with.
export function setLayoutMode(model: DiskModel, mode: LayoutMode): DiskModel {
  if (mode === model.layoutMode) return model

  if (mode === 'offset') {
    const offsets = computeOffsets(model)
    return {
      ...model,
      layoutMode: 'offset',
      partitions: model.partitions.map((p, i) => ({ ...p, ...offsets[i] })),
    }
  }

  // offset -> size. A partition whose offsets don't parse keeps its previous
  // size rather than collapsing to zero.
  const firstStart = model.partitions.length > 0 ? parseMiB(model.partitions[0].start) : null
  return {
    ...model,
    layoutMode: 'size',
    firstStartMiB: firstStart ?? model.firstStartMiB,
    partitions: model.partitions.map((p, i) => {
      const span = partitionSizeMiB({ ...model, layoutMode: 'offset' }, i)
      return { ...p, sizeMiB: span === undefined ? p.sizeMiB : span }
    }),
  }
}

// The builder keeps 1 MiB between the absolute and end-relative regions so the
// partition spanning them is not empty (imagedisc.go endRelativeAlignBytes).
const END_REGION_GAP_MIB = 1

// usedMiB is the space the layout occupies up to the start of the "rest"
// partition (or the end of the last one), including the leading gap. Drives the
// "fits on the disk" check and the space summary.
export function usedMiB(model: DiskModel): number {
  if (model.layoutMode === 'offset') {
    // Offsets are absolute, so the high-water mark is the answer — summing
    // spans would ignore gaps and double-count overlaps. Offsets measured back
    // from the end reserve that much on top (imagedisc.go requiredInstallDiskBytes).
    let high = 0
    let back = 0
    model.partitions.forEach((p, i) => {
      const toEnd = isRest(model, i) || fromEndMiB(p.end) !== null
      high = Math.max(high, (toEnd ? parseMiB(p.start) : parseMiB(p.end)) ?? 0)
      back = Math.max(back, fromEndMiB(p.start) ?? 0, fromEndMiB(p.end) ?? 0)
    })
    return back > 0 ? high + back + END_REGION_GAP_MIB : high
  }
  const restAt = model.partitions.findIndex((_, i) => isRest(model, i))
  let back = 0
  const used = model.partitions.reduce((total, p, i) => {
    if (isRest(model, i)) return total
    if (restAt >= 0 && i > restAt) back += p.sizeMiB ?? 0
    return total + (p.sizeMiB ?? 0)
  }, model.firstStartMiB)
  return back > 0 ? used + END_REGION_GAP_MIB : used
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
      if (p.index !== null) out.index = p.index
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

  if (model.layoutMode === 'offset') {
    // end: "0" is the end of the disk, which only the last partition can reach.
    const lastIndex = model.partitions.length - 1
    if (model.partitions.some((p, i) => i !== lastIndex && parseSize(p.end) === 0)) {
      errors.push('Only the last partition can use the remaining disk space.')
    }
  } else if (model.partitions.filter((_, i) => isRest(model, i)).length > 1) {
    // Size mode places the partitions after the rest one back from the end of the disk.
    errors.push('Only one partition can use the remaining disk space.')
  }

  model.partitions.forEach((p, i) => {
    const label = `Partition ${i + 1}${p.id || p.name ? ` (${p.id || p.name})` : ''}`
    if (!isRest(model, i)) {
      const span = partitionSizeMiB(model, i)
      if (span === undefined) {
        errors.push(`${label} has offsets that are not valid sizes (for example 1MiB).`)
      } else if (span !== null && span <= 0) {
        errors.push(
          model.layoutMode === 'offset'
            ? `${label} ends at or before it starts.`
            : `${label} needs a size greater than zero.`,
        )
      }
    }
    if (!p.id && !p.name) {
      warnings.push(`Partition ${i + 1} has no id or name.`)
    }
    const { errors: fe, warnings: fw } = partitionFieldIssues(model, i, label)
    errors.push(...fe)
    warnings.push(...fw)
  })

  const contiguity = contiguityIssues(model)
  errors.push(...contiguity.errors)
  warnings.push(...contiguity.warnings)
  const artifacts = artifactIssues(model, ctx)
  errors.push(...artifacts.errors)
  warnings.push(...artifacts.warnings)

  // The builder's own suffix table, not lib/size.ts's looser parser: a value
  // like "1.5TiB" or "32gib" parses here and is refused at build time.
  const sizeBytes = parseSize(model.size)
  if (model.size && !isValidDiskSize(model.size)) {
    errors.push(
      `Disk size "${model.size}" is not a size the builder accepts. Use a whole number and one of ${OFFSET_SUFFIXES.join(', ')} — exact case, no decimals.`,
    )
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
    if (!isValidDiskSize(model.maxSize) || maxBytes === null) {
      errors.push(
        `Max size "${model.maxSize}" is not a size the builder accepts. Use a whole number and one of ${OFFSET_SUFFIXES.join(', ')} — exact case, no decimals.`,
      )
    } else if (sizeBytes === null) {
      errors.push('Max size requires a disk size to be set.')
    } else if (maxBytes <= sizeBytes) {
      errors.push('Max size must be greater than the disk size.')
    }
  }

  errors.push(...autoExpandErrors(model, ctx))


  // Not an error — the builder can produce an image without an explicit
  // artifact list (ISO templates carry none) — but worth saying out loud when
  // the user is looking at a step that offers the control.
  if (model.partitions.length === 0) {
    warnings.push('No partitions are defined.')
  }

  return { errors, warnings }
}

// partitionFieldIssues checks the per-field rules the builder enforces. The JSON
// schema types all of these as plain strings, so without this the first sign of
// a bad value is a failed build. See lib/diskrules.ts for where each rule
// comes from, and which of them fail hard versus degrade quietly.
function partitionFieldIssues(model: DiskModel, i: number, label: string): DiskIssues {
  const p = model.partitions[i]
  const errors: string[] = []
  const warnings: string[] = []

  // Hard: diskPartitionCreate rejects an unlisted fsType outright.
  if (!p.fsType) {
    errors.push(`${label} needs a filesystem type.`)
  } else if (!(FS_TYPES as readonly string[]).includes(p.fsType)) {
    errors.push(`${label} has an unsupported filesystem "${p.fsType}". Use one of ${FS_TYPES.join(', ')}.`)
  }

  // Hard: VerifyFileSize is stricter than it looks — integers only, exact-case
  // suffix, no bare byte counts. Only reachable in offset mode, since size mode
  // generates the offsets itself.
  if (model.layoutMode === 'offset') {
    for (const [field, value] of [
      ['Start', p.start],
      ['End', isRest(model, i) ? '0' : p.end],
    ] as const) {
      if (!isValidOffset(value)) {
        errors.push(
          `${label}: ${field} "${value}" is not a size the builder accepts. ` +
            `Use a whole number and one of ${OFFSET_SUFFIXES.join(', ')} — exact case, no decimals${field === 'End' ? ', or 0 for the rest of the disk' : ''}.`,
        )
      }
    }
  }

  // Soft: an unknown partition type means sgdisk is never given -t, so the
  // partition silently gets a default type instead of the intended one.
  if (p.type && !(PARTITION_TYPES as readonly string[]).includes(p.type)) {
    warnings.push(
      `${label} has an unrecognised type "${p.type}"; the partition type will be left at the default.`,
    )
  }

  // Soft: sgdisk validates the value, but not until build time.
  if (p.typeUUID && !isValidTypeUUID(p.typeUUID)) {
    warnings.push(
      `${label} has a typeUUID that is neither a GUID nor a 4-digit sgdisk code: "${p.typeUUID}".`,
    )
  }
  // typeUUID wins over type (imagedisc.go:812), so a mismatch is a silent
  // override rather than a conflict the build would report.
  const expected = PARTITION_TYPE_GUIDS[p.type]
  if (p.typeUUID && expected && p.typeUUID.toLowerCase() !== expected.toLowerCase()) {
    warnings.push(
      `${label}: typeUUID does not match type "${p.type}" (${expected}); the typeUUID is used.`,
    )
  }

  for (const flag of p.flags) {
    if (!(PARTITION_FLAGS as readonly string[]).includes(flag)) {
      warnings.push(`${label} has an unrecognised flag "${flag}"; it will be ignored.`)
    }
  }

  return { errors, warnings }
}

// artifactIssues checks disk.artifacts[] against the pipeline the image type
// actually runs. The schema's enums are wider than the implementation on both
// axes (see lib/diskrules.ts), and the image type decides which set applies.
function artifactIssues(model: DiskModel, ctx: DiskContext): DiskIssues {
  const errors: string[] = []
  const warnings: string[] = []
  const imageType = ctx.imageType ?? ''
  const support = artifactSupport(imageType)
  const { types, compressions, compressionRequired } = artifactOptions(imageType)

  if (support === 'ignored') {
    if (model.artifacts.length > 0) {
      warnings.push(
        `Output artefacts are ignored for ${imageType.toUpperCase()} images — the artefact pipeline only runs for RAW and WSL2.`,
      )
    }
    return { errors, warnings }
  }

  if (support === 'wsl2' && model.artifacts.length === 0) {
    errors.push('A WSL2 image needs exactly one tar artefact with gz compression.')
  }
  if (support === 'wsl2' && model.artifacts.length > 1) {
    warnings.push('A WSL2 image uses only the first artefact; the rest are ignored.')
  }

  // Displayed alongside the Output Artefacts rows, so it is spelled the way that
  // section's heading is. The template key it edits stays `artifacts:`.
  model.artifacts.forEach((a, i) => {
    const label = `Output artefact ${i + 1}`
    if (!a.type) {
      errors.push(`${label} needs a format.`)
    } else if (!types.includes(a.type)) {
      errors.push(
        `${label}: ${imageType.toUpperCase() || 'this'} images cannot be written as "${a.type}". Use ${types.join(', ')}.`,
      )
    }
    if (!a.compression) {
      if (compressionRequired) errors.push(`${label} needs compression (${compressions.join(' or ')}).`)
    } else if (!compressions.includes(a.compression)) {
      errors.push(
        `${label}: "${a.compression}" compression is not implemented for ${imageType.toUpperCase() || 'this'} images. Use ${compressions.join(', ')}.`,
      )
    }
  })

  return { errors, warnings }
}

// contiguityIssues reports gaps and overlaps between consecutive partitions.
//
// Only reachable in 'offset' mode — 'size' mode derives the offsets and cannot
// produce either. A gap is legal (the schema and the builder both accept it) and
// occasionally deliberate, so it is a warning. An overlap is an error: the
// builder rejects it before touching the disk (imagedisc.go
// validatePartitionLayoutOrder), for absolute and end-relative offsets alike.
function contiguityIssues(model: DiskModel): DiskIssues {
  const errors: string[] = []
  const warnings: string[] = []
  if (model.layoutMode !== 'offset') return { errors, warnings }

  for (let i = 1; i < model.partitions.length; i++) {
    // Measured back from the end, so a gap is a start nearer the end than the previous end.
    const prevBack = fromEndMiB(model.partitions[i - 1].end)
    const startBack = fromEndMiB(model.partitions[i].start)
    if (prevBack !== null && startBack !== null) {
      if (startBack < prevBack) {
        warnings.push(`Gap of ${prevBack - startBack} MiB between partitions ${i} and ${i + 1}.`)
      } else if (startBack > prevBack) {
        errors.push(`Partitions ${i} and ${i + 1} overlap by ${startBack - prevBack} MiB.`)
      }
      continue
    }
    const prevEnd = parseMiB(model.partitions[i - 1].end)
    const start = parseMiB(model.partitions[i].start)
    // An interior "rest" partition is already a hard error; don't pile on.
    if (prevEnd === null || start === null || isRest(model, i - 1)) continue
    if (start > prevEnd) {
      warnings.push(`Gap of ${start - prevEnd} MiB between partitions ${i} and ${i + 1}.`)
    } else if (start < prevEnd) {
      errors.push(`Partitions ${i} and ${i + 1} overlap by ${prevEnd - start} MiB.`)
    }
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
