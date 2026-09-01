// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import { useMemo, useState } from 'react'
import { useStore } from '../store'
import {
  ARTIFACT_TYPES,
  COMPRESSION_TYPES,
  PARTITION_TABLE_TYPES,
  appendPartition,
  computeOffsets,
  diskYamlFragment,
  isRest,
  newArtifact,
  partitionSizeMiB,
  setLayoutMode,
  suggestedSize,
  usedMiB,
  validateDisk,
} from '../lib/disk'
import type { DiskModel, LayoutMode, PartitionModel, PartitionTableType } from '../lib/disk'
import { MIB, SIZE_UNITS, amountOf, formatSize, parseSize, unitOf } from '../lib/size'
import type { SizeUnit } from '../lib/size'

// Step 3 of the Advanced wizard: "Disk Layout".
//
// Laid out in the #822 prototype's order and copy (Disk Size → Partition Table →
// Partitions), with the controls actually wired up — the prototype's partition
// list is read-only and its unit select and "+ Add Partition" have no handlers.
//
// Two things the prototype could not model, both forced by the real schema in
// internal/config/schema/os-image-template.schema.json:
//
//  - The prototype gives each partition a `size` field. There is no such field:
//    the schema has `start`/`end` offsets. Both are editable here, one at a time
//    (see LAYOUT_MODES in lib/disk.ts) — size-based derives the offsets and
//    keeps them contiguous, offset-based lets them be typed directly.
//  - MBR is offered, because the schema allows it.
//
// The Output Artifacts section has no prototype counterpart. It is
// `disk.artifacts[]` — a Disk property, and the place ICT actually produces
// QCOW2/VHD/VMDK output (target.imageType does not offer those).
//
// Nothing here reaches a build yet: the model is client-side only until the
// Review step generates a full template. The YAML preview at the bottom is what
// this step contributes to that, shown so the output is reviewable today.

const CHIP_BASE =
  'cursor-pointer select-none rounded-md border px-3.5 py-1.5 text-[13px] font-medium transition-colors'
const CHIP_ON = 'border-[#0071c5] bg-[#e6f2fa] text-[#0071c5]'
const CHIP_OFF = 'border-slate-300 text-slate-600 hover:border-slate-400'

const FIELD =
  'w-full rounded-md border border-slate-300 bg-white px-2 py-1.5 text-sm text-[#00285a] disabled:cursor-not-allowed disabled:bg-slate-100 disabled:text-slate-400 focus:border-[#0071c5] focus:outline-none focus:ring-1 focus:ring-[#0071c5]'
const LABEL = 'mb-1 block text-sm font-semibold text-[#00285a]'
const COL_HEAD = 'text-[11px] font-semibold uppercase tracking-wide text-slate-400'
const DERIVED = 'truncate px-1 font-mono text-xs text-slate-400'
const ICON_BTN =
  'rounded border border-slate-300 px-1.5 py-0.5 text-xs text-slate-500 hover:border-slate-400 hover:text-slate-700 disabled:cursor-not-allowed disabled:opacity-30'

// One grid for the header and every row, so the columns line up.
const ROW_GRID =
  'grid grid-cols-[minmax(84px,1fr)_minmax(78px,1fr)_82px_146px_minmax(88px,1fr)_98px_98px_auto] items-center gap-2'

const MODE_LABELS: Record<LayoutMode, string> = {
  size: 'Size-based (contiguous)',
  offset: 'Offset-based (manual)',
}

export function DiskStep() {
  const disk = useStore((s) => s.disk)
  const diskEdited = useStore((s) => s.diskEdited)
  const setDisk = useStore((s) => s.setDisk)
  const resetDisk = useStore((s) => s.resetDisk)
  // The auto-expand rules key off target.imageType, which lives outside the
  // disk block; the cascade selection is the same value the resolved template
  // carries, since compose looks the template up by it.
  const imageType = useStore((s) => s.selection.imageType)

  const [expanded, setExpanded] = useState<Record<string, boolean>>({})
  const [rowUnits, setRowUnits] = useState<Record<string, SizeUnit>>({})
  const [showYaml, setShowYaml] = useState(false)

  const offsets = useMemo(() => (disk ? computeOffsets(disk) : []), [disk])
  const issues = useMemo(() => (disk ? validateDisk(disk, { imageType }) : null), [disk, imageType])

  if (!disk) {
    return (
      <div>
        <Heading />
        <div className="rounded-lg border border-dashed border-slate-300 p-8 text-center text-sm text-slate-400">
          Complete all selections in the Target step to load the template's disk layout.
        </div>
      </div>
    )
  }

  const patch = (next: Partial<DiskModel>) => setDisk({ ...disk, ...next })

  const patchPart = (index: number, next: Partial<PartitionModel>) =>
    patch({ partitions: disk.partitions.map((p, i) => (i === index ? { ...p, ...next } : p)) })

  const movePart = (index: number, delta: number) => {
    const to = index + delta
    if (to < 0 || to >= disk.partitions.length) return
    const partitions = [...disk.partitions]
    ;[partitions[index], partitions[to]] = [partitions[to], partitions[index]]
    patch({ partitions })
  }

  const sizeUnit = unitOf(disk.size)
  const sizeAmount = amountOf(disk.size)
  const sizeBytes = parseSize(disk.size)
  const used = usedMiB(disk)
  const hasRest = disk.partitions.some((_, i) => offsets[i]?.end === '0')
  const isIso = imageType.toLowerCase() === 'iso'
  // A "rest of disk" partition has to stay last — its end of "0" leaves anything
  // after it with no offset to start from. Reordering is fenced off accordingly.
  const restIsLast = disk.partitions.length > 0 && isRest(disk, disk.partitions.length - 1)

  return (
    <div>
      <Heading>
        <button
          type="button"
          onClick={resetDisk}
          disabled={!diskEdited}
          className="rounded-md border border-slate-300 px-3 py-1 text-xs font-medium text-slate-600 hover:border-slate-400 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-40"
        >
          Reset to template defaults
        </button>
      </Heading>

      {issues && (issues.errors.length > 0 || issues.warnings.length > 0) && (
        <div className="mb-5 space-y-1">
          {issues.errors.map((e) => (
            <div key={e} className="rounded bg-red-50 px-3 py-2 text-sm text-red-700">
              {e}
            </div>
          ))}
          {issues.warnings.map((w) => (
            <div key={w} className="rounded bg-amber-50 px-3 py-2 text-sm text-amber-700">
              {w}
            </div>
          ))}
        </div>
      )}

      {/* Disk name — schema-required ($defs.Disk.required), so it is editable
          here rather than hardcoded the way the prototype's YAML generator does. */}
      <div className="mb-4">
        <label htmlFor="disk-name" className={LABEL}>
          Disk Name
        </label>
        <input
          id="disk-name"
          type="text"
          className={FIELD}
          value={disk.name}
          onChange={(e) => patch({ name: e.target.value })}
        />
        <p className="mt-1 text-xs text-slate-400">
          Required by the template schema. Conventionally matches the system config name.
        </p>
      </div>

      <div className="mb-4">
        <label htmlFor="disk-size" className={LABEL}>
          Disk Size
        </label>
        <div className="flex gap-2">
          <input
            id="disk-size"
            type="number"
            min={1}
            className={`${FIELD} w-[110px]`}
            value={sizeAmount ?? ''}
            placeholder="none"
            onChange={(e) =>
              patch({ size: e.target.value === '' ? '' : formatSize(Number(e.target.value), sizeUnit) })
            }
          />
          <select
            aria-label="Disk size unit"
            className={`${FIELD} w-[90px]`}
            value={sizeUnit}
            onChange={(e) =>
              patch({
                size: sizeAmount === null ? '' : formatSize(sizeAmount, e.target.value as SizeUnit),
              })
            }
          >
            {SIZE_UNITS.map((u) => (
              <option key={u} value={u}>
                {u}
              </option>
            ))}
          </select>
          {!disk.size && disk.partitions.length > 0 && (
            <button
              type="button"
              onClick={() => patch({ size: suggestedSize(disk) })}
              className="rounded-md border border-slate-300 px-3 text-xs font-medium text-slate-600 hover:border-slate-400 hover:bg-slate-50"
            >
              Use {suggestedSize(disk)}
            </button>
          )}
        </div>
        <p className="mt-1 text-xs text-slate-400">
          {disk.size
            ? `Partitions use ${used} MiB of ${sizeBytes === null ? '?' : Math.floor(sizeBytes / MIB)} MiB${hasRest ? '; the last partition takes the remainder.' : '.'}`
            : 'The resolved template sets no disk size — ISO images size their own media. Leave it empty to keep it that way.'}
        </p>
      </div>

      {/* maxSize is an overlay-mode ceiling (internal/config/config.go). It is
          shown only when the resolved template already has one: offering it on a
          create-mode template would advertise a field the build ignores. */}
      {disk.maxSize !== '' && (
        <div className="mb-4">
          <label htmlFor="disk-max-size" className={LABEL}>
            Max Size
          </label>
          <input
            id="disk-max-size"
            type="text"
            className={`${FIELD} w-[200px]`}
            value={disk.maxSize}
            onChange={(e) => patch({ maxSize: e.target.value })}
          />
          <p className="mt-1 text-xs text-slate-400">
            Overlay-mode ceiling: package-driven growth stops here. Must exceed the disk size.
          </p>
        </div>
      )}

      <div className="mb-4">
        <span className={LABEL}>Partition Table</span>
        <div className="flex flex-wrap gap-2">
          {PARTITION_TABLE_TYPES.map((t) => (
            <button
              key={t}
              type="button"
              aria-pressed={disk.partitionTableType === t}
              onClick={() => patch({ partitionTableType: t as PartitionTableType })}
              className={`${CHIP_BASE} ${disk.partitionTableType === t ? CHIP_ON : CHIP_OFF}`}
            >
              {t.toUpperCase()}
            </button>
          ))}
        </div>
      </div>

      <div className="mb-4">
        <div className="mb-1 flex flex-wrap items-baseline justify-between gap-2">
          <span className={LABEL}>Partitions</span>
          <div className="flex items-center gap-2">
            <span className="text-xs text-slate-400">Edit by</span>
            {(Object.keys(MODE_LABELS) as LayoutMode[]).map((m) => (
              <button
                key={m}
                type="button"
                aria-pressed={disk.layoutMode === m}
                onClick={() => setDisk(setLayoutMode(disk, m))}
                className={`cursor-pointer select-none rounded-md border px-2.5 py-1 text-xs font-medium transition-colors ${
                  disk.layoutMode === m ? CHIP_ON : CHIP_OFF
                }`}
              >
                {MODE_LABELS[m]}
              </button>
            ))}
          </div>
        </div>

        <div className="rounded-md border border-slate-200 bg-slate-50 p-3">
          {disk.partitions.length === 0 ? (
            <p className="py-2 text-center text-sm text-slate-400">No partitions defined.</p>
          ) : (
            <>
              <div className={`${ROW_GRID} px-2 pb-1`}>
                <span className={COL_HEAD}>Name</span>
                <span className={COL_HEAD}>Label</span>
                <span className={COL_HEAD}>FS</span>
                <span className={COL_HEAD}>
                  Size{disk.layoutMode === 'offset' && <em className="font-normal"> (derived)</em>}
                </span>
                <span className={COL_HEAD}>Mount Point</span>
                <span className={COL_HEAD}>
                  Start{disk.layoutMode === 'size' && <em className="font-normal"> (calc)</em>}
                </span>
                <span className={COL_HEAD}>
                  End{disk.layoutMode === 'size' && <em className="font-normal"> (calc)</em>}
                </span>
                <span className={COL_HEAD} />
              </div>
              {disk.partitions.map((p, i) => (
                <PartitionRow
                  key={p.key}
                  partition={p}
                  index={i}
                  count={disk.partitions.length}
                  mode={disk.layoutMode}
                  offsets={offsets[i]}
                  sizeMiB={partitionSizeMiB(disk, i)}
                  unit={rowUnits[p.key] ?? defaultRowUnit(p)}
                  rest={isRest(disk, i)}
                  restForced={i === disk.partitions.length - 1 && disk.extendLastPartitionToFillDisk}
                  restIsLast={restIsLast}
                  expanded={!!expanded[p.key]}
                  onToggleExpand={() => setExpanded((s) => ({ ...s, [p.key]: !s[p.key] }))}
                  onUnitChange={(u) => setRowUnits((s) => ({ ...s, [p.key]: u }))}
                  onChange={(next) => patchPart(i, next)}
                  onMove={(delta) => movePart(i, delta)}
                  onRemove={() => patch({ partitions: disk.partitions.filter((_, j) => j !== i) })}
                />
              ))}
            </>
          )}
          <button
            type="button"
            onClick={() => patch({ partitions: appendPartition(disk) })}
            className="mt-2 rounded-md border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-[#00285a] hover:border-slate-400 hover:bg-slate-100"
          >
            + Add Partition
          </button>
        </div>

        <p className="mt-1 text-xs text-slate-400">
          {disk.layoutMode === 'size'
            ? 'The schema stores start/end offsets, not sizes. Partitions are laid out contiguously from the first start offset, so Start and End are calculated for you.'
            : 'Start and End are written to the template exactly as typed. Gaps and overlaps are reported but not corrected.'}
        </p>

        {/* The template loader rejects this for ISO and, for RAW, unless the
            last partition is the rootfs (internal/config/validate/validate.go).
            The checkbox stays enabled so a template that arrives with it set can
            still be unset; validateDisk reports the violation. */}
        <label className="mt-2 flex items-center gap-2 text-xs text-slate-500">
          <input
            type="checkbox"
            checked={disk.extendLastPartitionToFillDisk}
            onChange={(e) => patch({ extendLastPartitionToFillDisk: e.target.checked })}
          />
          Force the last partition to fill the remaining disk space
          {isIso && <span className="text-slate-400">— not supported for ISO images</span>}
        </label>
      </div>

      <ArtifactsSection
        artifacts={disk.artifacts}
        onChange={(artifacts) => patch({ artifacts })}
      />

      <div className="rounded-md border border-slate-200 bg-white">
        <button
          type="button"
          aria-expanded={showYaml}
          onClick={() => setShowYaml((v) => !v)}
          className="flex w-full items-center justify-between px-4 py-2 text-left"
        >
          <span className="text-sm font-semibold text-[#00285a]">
            {showYaml ? '▾' : '▸'} Generated disk block
          </span>
          <span className="text-xs text-slate-400">
            {diskEdited ? 'edited' : 'matches the resolved template'}
          </span>
        </button>
        {showYaml && (
          <pre className="max-h-80 overflow-auto border-t border-slate-200 px-4 py-3 font-mono text-xs leading-relaxed text-slate-700">
            {diskYamlFragment(disk)}
          </pre>
        )}
      </div>
      <p className="mt-2 text-xs text-slate-400">
        Preview only. The Review step still composes from the pre-authored template; sending an
        edited disk layout to a build arrives with client-side template generation.
      </p>
    </div>
  )
}

function Heading({ children }: { children?: React.ReactNode }) {
  return (
    <div className="mb-5 flex items-start justify-between gap-4">
      <div>
        <h2 className="mb-1 text-lg font-bold text-[#00285a]">Disk Layout</h2>
        <p className="text-sm text-slate-500">
          Configure disk size, partition table, and partitions.
        </p>
      </div>
      {children}
    </div>
  )
}

// A partition's size is stored in MiB; pick the friendlier unit to show it in.
function defaultRowUnit(p: PartitionModel): SizeUnit {
  const mib = p.sizeMiB
  return mib !== null && mib >= 1024 && mib % 1024 === 0 ? 'GiB' : 'MiB'
}

interface PartitionRowProps {
  partition: PartitionModel
  index: number
  count: number
  mode: LayoutMode
  offsets: { start: string; end: string } | undefined
  sizeMiB: number | null | undefined
  unit: SizeUnit
  rest: boolean
  restForced: boolean
  restIsLast: boolean
  expanded: boolean
  onToggleExpand: () => void
  onUnitChange: (u: SizeUnit) => void
  onChange: (next: Partial<PartitionModel>) => void
  onMove: (delta: number) => void
  onRemove: () => void
}

function PartitionRow({
  partition: p,
  index,
  count,
  mode,
  offsets,
  sizeMiB,
  unit,
  rest,
  restForced,
  restIsLast,
  expanded,
  onToggleExpand,
  onUnitChange,
  onChange,
  onMove,
  onRemove,
}: PartitionRowProps) {
  const divisor = unit === 'GiB' ? 1024 : 1
  const amount = p.sizeMiB === null ? '' : String(p.sizeMiB / divisor)

  // Templates label a partition with `name`, `id`, or both — the ISO defaults
  // in config/osv/ set only `id`. The Name column edits whichever one the
  // template actually used, so it never shows an empty field next to a
  // partition that is clearly named; the other stays in the details panel.
  const labelsById = !p.name && !!p.id

  // Unchecking "use remaining space" needs a concrete extent to fall back to.
  const unsetRest = () =>
    mode === 'size'
      ? { sizeMiB: 1024 }
      : { end: `${(Number(p.start.replace(/[^\d.]/g, '')) || 0) + 1024}MiB` }

  return (
    <div className="mb-2 rounded-md border border-slate-200 bg-white p-2">
      <div className={ROW_GRID}>
        <input
          type="text"
          aria-label={`Partition ${index + 1} name`}
          placeholder="name"
          className={FIELD}
          value={labelsById ? p.id : p.name}
          onChange={(e) =>
            onChange(labelsById ? { id: e.target.value } : { name: e.target.value })
          }
        />
        <input
          type="text"
          aria-label={`Partition ${index + 1} filesystem label`}
          placeholder="fsLabel"
          className={FIELD}
          value={p.fsLabel}
          onChange={(e) => onChange({ fsLabel: e.target.value })}
        />
        <input
          type="text"
          aria-label={`Partition ${index + 1} filesystem`}
          placeholder="fsType"
          className={FIELD}
          value={p.fsType}
          onChange={(e) => onChange({ fsType: e.target.value })}
        />

        {mode === 'size' ? (
          <div className="flex items-center gap-1">
            <input
              type="number"
              min={0}
              aria-label={`Partition ${index + 1} size`}
              className={`${FIELD} w-[70px]`}
              value={amount}
              placeholder="rest"
              disabled={rest}
              onChange={(e) =>
                onChange({ sizeMiB: e.target.value === '' ? 0 : Math.round(Number(e.target.value) * divisor) })
              }
            />
            <select
              aria-label={`Partition ${index + 1} size unit`}
              className={`${FIELD} w-[68px]`}
              value={unit}
              disabled={rest}
              onChange={(e) => onUnitChange(e.target.value as SizeUnit)}
            >
              <option value="MiB">MiB</option>
              <option value="GiB">GiB</option>
            </select>
          </div>
        ) : (
          <span className={DERIVED} title="Derived from Start and End">
            {rest ? 'rest' : sizeMiB === undefined ? '—' : `${sizeMiB} MiB`}
          </span>
        )}

        <input
          type="text"
          aria-label={`Partition ${index + 1} mount point`}
          placeholder="mountPoint"
          className={FIELD}
          value={p.mountPoint}
          onChange={(e) => onChange({ mountPoint: e.target.value })}
        />

        {mode === 'offset' ? (
          <>
            <input
              type="text"
              aria-label={`Partition ${index + 1} start`}
              placeholder="1MiB"
              className={FIELD}
              value={p.start}
              onChange={(e) => onChange({ start: e.target.value })}
            />
            <input
              type="text"
              aria-label={`Partition ${index + 1} end`}
              placeholder="513MiB"
              className={FIELD}
              value={rest ? '0' : p.end}
              disabled={rest}
              onChange={(e) => onChange({ end: e.target.value })}
            />
          </>
        ) : (
          <>
            <span className={DERIVED} title="Calculated from the sizes above">
              {offsets?.start ?? '?'}
            </span>
            <span className={DERIVED} title="Calculated from the sizes above">
              {offsets?.end ?? '?'}
            </span>
          </>
        )}

        <div className="flex items-center gap-1">
          <button
            type="button"
            className={ICON_BTN}
            title="Move up"
            disabled={index === 0 || (restIsLast && index === count - 1)}
            onClick={() => onMove(-1)}
          >
            ▲
          </button>
          <button
            type="button"
            className={ICON_BTN}
            title="Move down"
            disabled={index === count - 1 || (restIsLast && index === count - 2)}
            onClick={() => onMove(1)}
          >
            ▼
          </button>
          <button
            type="button"
            className={ICON_BTN}
            title={expanded ? 'Hide details' : 'Edit details'}
            aria-expanded={expanded}
            onClick={onToggleExpand}
          >
            ✎
          </button>
          <button type="button" className={ICON_BTN} title="Remove partition" onClick={onRemove}>
            ✕
          </button>
        </div>
      </div>

      <div className="mt-1 flex flex-wrap items-center gap-3 px-1 text-xs text-slate-400">
        <label className="flex items-center gap-1.5">
          <input
            type="checkbox"
            checked={rest}
            disabled={restForced}
            onChange={(e) => onChange(e.target.checked ? restPatch(mode) : unsetRest())}
          />
          use remaining space
        </label>
        {restForced && <span className="text-amber-600">forced by the fill-disk option</span>}
      </div>

      {expanded && (
        <div className="mt-2 grid grid-cols-2 gap-2 border-t border-slate-100 pt-2">
          <LabelledField
            label="id"
            value={p.id}
            rowKey={p.key}
            onChange={(v) => onChange({ id: v })}
          />
          <LabelledField
            label="index"
            value={p.index === null ? '' : String(p.index)}
            rowKey={p.key}
            placeholder="auto"
            onChange={(v) => onChange({ index: v.trim() === '' ? null : Number(v) })}
          />
          <LabelledField
            label="type"
            value={p.type}
            rowKey={p.key}
            onChange={(v) => onChange({ type: v })}
          />
          <LabelledField
            label="typeUUID"
            value={p.typeUUID}
            rowKey={p.key}
            onChange={(v) => onChange({ typeUUID: v })}
          />
          <LabelledField
            label="mountOptions"
            value={p.mountOptions}
            rowKey={p.key}
            onChange={(v) => onChange({ mountOptions: v })}
          />
          <LabelledField
            label="flags (comma separated)"
            value={p.flags.join(', ')}
            rowKey={p.key}
            onChange={(v) =>
              onChange({ flags: v.split(',').map((f) => f.trim()).filter((f) => f !== '') })
            }
          />
        </div>
      )}
    </div>
  )
}

// "Use remaining space" means sizeMiB: null in size mode and end: "0" in offset
// mode — the schema's own sentinel.
function restPatch(mode: LayoutMode): Partial<PartitionModel> {
  return mode === 'size' ? { sizeMiB: null } : { end: '0' }
}

function LabelledField({
  label,
  value,
  rowKey,
  placeholder,
  onChange,
}: {
  label: string
  value: string
  rowKey: string
  placeholder?: string
  onChange: (v: string) => void
}) {
  // Include the row key: without it every partition's details panel would emit
  // the same id, and a label would focus the first row's field.
  const id = `part-${rowKey}-${label.replace(/\W+/g, '-')}`
  return (
    <div>
      <label htmlFor={id} className="mb-0.5 block text-xs text-slate-400">
        {label}
      </label>
      <input
        id={id}
        type="text"
        className={FIELD}
        value={value}
        placeholder={placeholder}
        onChange={(e) => onChange(e.target.value)}
      />
    </div>
  )
}

// disk.artifacts[] — the output formats the finished image is converted to.
// This is where ICT produces QCOW2/VHD/VMDK; target.imageType (raw/img/iso/wsl2)
// controls how the image is built, which is a different axis.
function ArtifactsSection({
  artifacts,
  onChange,
}: {
  artifacts: DiskModel['artifacts']
  onChange: (next: DiskModel['artifacts']) => void
}) {
  return (
    <div className="mb-4">
      <span className={LABEL}>Output Artifacts</span>
      <div className="rounded-md border border-slate-200 bg-slate-50 p-3">
        {artifacts.length === 0 ? (
          <p className="py-2 text-center text-sm text-slate-400">
            No output artifacts — the builder writes its default format.
          </p>
        ) : (
          artifacts.map((a, i) => (
            <div key={a.key} className="mb-2 flex items-center gap-2">
              <select
                aria-label={`Artifact ${i + 1} format`}
                className={`${FIELD} w-[140px]`}
                value={a.type}
                onChange={(e) =>
                  onChange(artifacts.map((x, j) => (j === i ? { ...x, type: e.target.value } : x)))
                }
              >
                {ARTIFACT_TYPES.map((t) => (
                  <option key={t} value={t}>
                    {t}
                  </option>
                ))}
              </select>
              <select
                aria-label={`Artifact ${i + 1} compression`}
                className={`${FIELD} w-[160px]`}
                value={a.compression}
                onChange={(e) =>
                  onChange(
                    artifacts.map((x, j) => (j === i ? { ...x, compression: e.target.value } : x)),
                  )
                }
              >
                <option value="">no compression</option>
                {COMPRESSION_TYPES.map((c) => (
                  <option key={c} value={c}>
                    {c}
                  </option>
                ))}
              </select>
              <button
                type="button"
                className={ICON_BTN}
                title="Remove artifact"
                onClick={() => onChange(artifacts.filter((_, j) => j !== i))}
              >
                ✕
              </button>
            </div>
          ))
        )}
        <button
          type="button"
          onClick={() => onChange([...artifacts, newArtifact()])}
          className="mt-1 rounded-md border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-[#00285a] hover:border-slate-400 hover:bg-slate-100"
        >
          + Add Artifact
        </button>
      </div>
      <p className="mt-1 text-xs text-slate-400">
        Converts the finished image to another container format. QCOW2, VHD, VMDK and the rest
        live here, not in the image type.
      </p>
    </div>
  )
}
