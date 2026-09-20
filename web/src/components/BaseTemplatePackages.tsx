import { useState } from 'react'

interface BaseTemplatePackagesProps {
  // The matched template's own package list (ComposeResponse.basePackages),
  // before the user's Advanced-mode picks are applied. Raw strings, shown
  // as-is: a pinned entry's `name_version` form isn't split back apart here,
  // matching store.ts's own reasoning for encodePackage — package names can
  // themselves contain underscores, so the split would sometimes be wrong.
  packages: string[]
}

// BaseTemplatePackages is a read-only, collapsed-by-default panel showing what
// the curated template already ships, so the user can see what's already
// there before adding more in the "Selected" rail below it. Hidden entirely
// until the compose resolves and the base template turns out to carry any
// packages of its own.
export function BaseTemplatePackages({ packages }: BaseTemplatePackagesProps) {
  const [expanded, setExpanded] = useState(false)

  if (packages.length === 0) return null

  const sorted = [...packages].sort((a, b) => a.localeCompare(b))

  return (
    <div className="rounded-lg border border-slate-200 bg-white">
      <button
        type="button"
        onClick={() => setExpanded((e) => !e)}
        aria-expanded={expanded}
        className="flex w-full items-center justify-between px-3 py-2.5 text-left"
      >
        <span className="text-[13px] font-bold text-[#00285a]">
          Already in template ({sorted.length})
        </span>
        <span className="text-slate-400">{expanded ? '▲' : '▼'}</span>
      </button>
      {expanded && (
        <div className="max-h-[520px] overflow-y-auto border-t border-slate-100 py-1.5">
          <p className="px-3 pb-1.5 text-[11px] text-slate-500">
            Packages the base template already installs. Read-only — remove them
            from the template itself to change what it ships.
          </p>
          {sorted.map((p) => (
            <div key={p} className="px-3 py-1 text-[12px] text-slate-700" title={p}>
              {p}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
