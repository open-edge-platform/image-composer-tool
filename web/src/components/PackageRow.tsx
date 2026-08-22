import type { AddedPackage } from '../store'

interface PackageRowProps {
  name: string
  version: string
  description?: string
  repoLabel: string
  // Shown next to the name when a row can come from more than one repo (the
  // search dropdown); omitted in the per-repo browse pane, where it would
  // just repeat the pane header.
  showRepo: boolean
  // This row's current pick, if any. undefined means not added.
  selection: AddedPackage | undefined
  onToggle: (checked: boolean) => void
  onChooseVersion: (version: string) => void
}

// PackageRow is the shared package row used by both the repo browse pane and
// the search dropdown, so a package looks the same wherever it's found.
//
// Real apt/dnf metadata carries exactly one version per package per repo, so
// unlike the prototype's mock multi-version data, the version chips always
// render exactly two: "Latest" (floats with whatever the repo serves next
// time it's indexed) and the one known version (pinned, frozen even if the
// repo updates later).
export function PackageRow({
  name,
  version,
  description,
  repoLabel,
  showRepo,
  selection,
  onToggle,
  onChooseVersion,
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
          version={version}
          pinned={selection?.version}
          onChoose={(v) => onChooseVersion(v)}
        />
      </span>
    </label>
  )
}

function VersionChips({
  version,
  pinned,
  onChoose,
}: {
  version: string
  pinned: string | undefined
  onChoose: (version: string) => void
}) {
  // Chips live inside the row's <label>, so a click must stop it from also
  // toggling the checkbox.
  const chip = (label: string, value: string) => {
    const active = pinned === value
    return (
      <button
        key={value || 'latest'}
        type="button"
        onClick={(e) => {
          e.preventDefault()
          e.stopPropagation()
          onChoose(value)
        }}
        className={
          'rounded-full px-2 py-0.5 text-[11px] font-medium ' +
          (active
            ? 'bg-[#0071c5] text-white'
            : 'bg-[#e6f2fa] text-[#0071c5] hover:bg-[#d3e9f8]')
        }
      >
        {label}
      </button>
    )
  }

  return (
    <span className="mt-1.5 flex gap-1.5">
      {chip('Latest', '')}
      {chip(version, version)}
    </span>
  )
}
