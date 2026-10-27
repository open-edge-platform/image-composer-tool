// The Advanced-tab delta — what a selection changed relative to the template it
// extends — as a collapsible card. Shared by the Advanced tab's review step
// (open by default) and a build's Compose details (collapsed by default), so
// both describe a build's changes the same way.
import { useState } from 'react'
import type { ReactNode } from 'react'
import type { AdvancedChanges } from '../api/types'

export function AdvancedChangesCard({
  changes,
  defaultOpen = false,
  frame = 'rounded bg-white p-3 shadow-sm',
}: {
  changes: AdvancedChanges
  defaultOpen?: boolean
  // Border/background/padding, so the card can match the panels around it.
  frame?: string
}) {
  const [open, setOpen] = useState(defaultOpen)
  return (
    <div className={`${frame} text-xs`}>
      <button
        type="button"
        className="flex w-full items-center gap-1.5 text-left"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
      >
        <span className="text-[10px] text-slate-400">{open ? '▼' : '▶'}</span>
        <span className="text-[10px] font-semibold uppercase tracking-wide text-slate-400">Advanced changes</span>
        <span className="text-[11px] text-slate-400">— {advancedHeadline(changes)}</span>
      </button>
      {open && <AdvancedChangesList changes={changes} />}
    </div>
  )
}

// One-line digest of an Advanced-tab delta for the collapsed header, so two
// builds of the same template can be told apart without expanding either.
function advancedHeadline(c: AdvancedChanges): string {
  const n = (count: number | undefined, one: string, many = `${one}s`) =>
    count ? `${count} ${count === 1 ? one : many}` : null
  const parts = [
    c.imageName ? 'image name' : null,
    n(c.packages?.length, 'package'),
    n(c.repos?.length, 'repo'),
    c.disk ? 'disk layout' : null,
    n(c.newUsers?.length, 'new user'),
    n(c.credentialsFor?.length, 'login'),
  ].filter(Boolean)
  return parts.length ? parts.join(', ') : 'changed from the template'
}

// How many entries a list shows before "Show more".
const LIST_PREVIEW = 8

// A list of changed items, capped at LIST_PREVIEW entries until expanded.
function ExpandableList({ items, mono = true }: { items: string[]; mono?: boolean }) {
  const [expanded, setExpanded] = useState(false)
  const shown = expanded ? items : items.slice(0, LIST_PREVIEW)
  const hidden = items.length - LIST_PREVIEW
  return (
    <div>
      <div className="flex flex-wrap gap-1">
        {shown.map((item, i) => (
          <span
            key={`${item}-${i}`}
            className={`rounded bg-slate-100 px-1.5 py-0.5 text-slate-700 ${mono ? 'font-mono text-[11px]' : ''}`}
          >
            {item}
          </span>
        ))}
      </div>
      {hidden > 0 && (
        <button
          className="mt-1 text-[11px] font-semibold text-blue-600 hover:underline"
          onClick={() => setExpanded((e) => !e)}
          aria-expanded={expanded}
        >
          {expanded ? 'Show less' : `Show ${hidden} more`}
        </button>
      )}
    </div>
  )
}

// The Advanced-tab delta as a label/value table, styled like the Selection and
// Image Configuration cards above it.
function AdvancedChangesList({ changes }: { changes: AdvancedChanges }) {
  const disk = changes.disk
  const partitions = (disk?.partitions ?? []).map((p) =>
    [p.id, p.fsType, p.mountPoint, p.start || p.end ? `${p.start ?? ''}–${p.end ?? ''}` : '']
      .filter(Boolean)
      .join(' · '),
  )
  const list = (items?: string[]) => (items?.length ? <ExpandableList items={items} /> : null)
  const rows: [string, ReactNode][] = [
    ['Image name', changes.imageName || null],
    ['Packages', list(changes.packages)],
    ['Repositories', list(changes.repos)],
    [
      'Disk',
      disk ? (
        <>
          {(disk.size || disk.partitionTable) && (
            <p className="mb-1">{[disk.size, disk.partitionTable?.toUpperCase()].filter(Boolean).join(', ')}</p>
          )}
          {list(partitions)}
        </>
      ) : null,
    ],
    ['Users added', list(changes.newUsers)],
    ['Logins set for', list(changes.credentialsFor)],
  ]
  return (
    <table className="mt-2 w-full">
      <tbody>
        {rows
          .filter(([, v]) => v)
          .map(([k, v]) => (
            <tr key={k}>
              <td className="w-24 py-1 pr-3 align-top font-semibold text-slate-500">{k}</td>
              <td className="py-1 text-slate-700">{v}</td>
            </tr>
          ))}
      </tbody>
    </table>
  )
}
