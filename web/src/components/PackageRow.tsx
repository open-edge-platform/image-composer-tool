import { useState } from 'react'
import type { AddedPackage } from '../store'
import type { PackageVersion } from '../api/types'

// How many version chips a row shows before collapsing the rest behind
// "+N more". A package usually has two — the release and its -updates or
// -security counterpart — so two keeps the common row to a single line.
const DEFAULT_VISIBLE_VERSIONS = 2

interface PackageRowProps {
  name: string
  version: string
  description?: string
  repoLabel: string
  // Shown next to the name when a row can come from more than one repo (the
  // search dropdown); omitted in the per-repo browse pane, where it would
  // just repeat the pane header.
  showRepo: boolean
  // Every version on offer, newest first. Each carries its own repository
  // because pinning a version also pins where it comes from.
  versions: PackageVersion[]
  // This row's current pick, if any. undefined means not added.
  selection: AddedPackage | undefined
  onToggle: (checked: boolean) => void
  onChooseVersion: (v: PackageVersion) => void
  // Renders a repository ID as its display name, for the chip tooltips.
  repoLabelFor: (repoId: string) => string
}

// PackageRow is the shared package row used by both the repo browse pane and
// the search dropdown, so a package looks the same wherever it's found.
export function PackageRow({
  name,
  version,
  description,
  repoLabel,
  showRepo,
  versions,
  selection,
  onToggle,
  onChooseVersion,
  repoLabelFor,
}: PackageRowProps) {
  const checked = selection != null

  return (
    <label className="flex cursor-pointer items-start gap-2.5 border-b border-slate-100 px-3 py-2.5 last:border-b-0 hover:bg-[#eef4fb]">
      <input
        type="checkbox"
        checked={checked}
        onChange={(e) => onToggle(e.target.checked)}
        className="mt-0.5 h-[15px] w-[15px] shrink-0 accent-[#0071c5]"
      />
      <span className="min-w-0 flex-1">
        <span className="flex flex-wrap items-baseline gap-1.5">
          <span className="text-[13px] font-semibold text-slate-700">{name}</span>
          <span className="font-mono text-[11px] text-slate-500">{version}</span>
          {showRepo && (
            <span className="text-[11px] text-slate-400">via {repoLabel}</span>
          )}
        </span>
        {description && (
          <span className="mt-0.5 block truncate text-[11px] text-slate-500" title={description}>
            {description}
          </span>
        )}
        <VersionChips
          versions={versions}
          pinned={selection?.version}
          onChoose={onChooseVersion}
          repoLabelFor={repoLabelFor}
        />
      </span>
    </label>
  )
}

// VersionChips offers "Latest" plus one chip per available version. Latest is a
// floating pick that follows whatever the repository publishes next, where a
// version chip freezes that exact string — which is what a reproducible build
// needs. A package carried by several suites or repositories offers all of
// them, newest first, with the tail behind "+N more".
function VersionChips({
  versions,
  pinned,
  onChoose,
  repoLabelFor,
}: {
  versions: PackageVersion[]
  pinned: string | undefined
  onChoose: (v: PackageVersion) => void
  repoLabelFor: (repoId: string) => string
}) {
  const [expanded, setExpanded] = useState(false)
  const hidden = versions.length - DEFAULT_VISIBLE_VERSIONS
  const shown = expanded ? versions : versions.slice(0, DEFAULT_VISIBLE_VERSIONS)

  // Chips live inside the row's <label>, so a click must be stopped from also
  // toggling the checkbox.
  const stop = (e: React.MouseEvent) => {
    e.preventDefault()
    e.stopPropagation()
  }

  return (
    <span className="mt-1.5 flex flex-wrap items-center gap-1.5">
      <button
        type="button"
        onClick={(e) => {
          stop(e)
          onChoose({ version: '', repository: versions[0]?.repository ?? '' })
        }}
        className={chipClass(pinned === '')}
      >
        Latest
      </button>
      {shown.map((v) => (
        <button
          key={`${v.repository} ${v.version}`}
          type="button"
          title={`${v.version} from ${repoLabelFor(v.repository)}`}
          onClick={(e) => {
            stop(e)
            onChoose(v)
          }}
          className={chipClass(pinned === v.version)}
        >
          {v.version}
        </button>
      ))}
      {hidden > 0 && (
        <button
          type="button"
          onClick={(e) => {
            stop(e)
            setExpanded(!expanded)
          }}
          className="rounded-full px-2 py-0.5 text-[11px] font-medium text-slate-500 hover:text-[#0071c5] hover:underline"
        >
          {expanded ? 'show less' : `+${hidden} more`}
        </button>
      )}
    </span>
  )
}

function chipClass(active: boolean): string {
  return (
    'rounded-full px-2 py-0.5 font-mono text-[11px] font-medium ' +
    (active ? 'bg-[#0071c5] text-white' : 'bg-[#e6f2fa] text-[#0071c5] hover:bg-[#d3e9f8]')
  )
}
