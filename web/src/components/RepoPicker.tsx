import { useEffect, useState } from 'react'
import { api } from '../api/client'
import type { PackageRepo } from '../api/types'

interface RepoPickerProps {
  // Target OS id (a manifest `targets[].id`, e.g. "ubuntu24"). Empty until the
  // selection reaches an OS; the picker stays idle rather than listing every
  // repo in the catalog for a target the user hasn't chosen yet.
  os: string
  // True only while the Advanced tab is visible. Both tab pages stay mounted, so
  // the fetch is gated on this to keep a hidden page from issuing requests.
  active: boolean
}

// RepoPicker lists the package repositories available for the selected target and
// lets the user toggle them. The toggles are seeded from each repo's
// `enabledByDefault` (true for the target's own base OS repo).
//
// The enabled set is deliberately local state: nothing consumes it yet. It
// becomes the `repos` filter for package search, and feeds the exported
// template's `packageRepositories` block, in later PRs — this PR delivers the
// listing and the picker itself.
export function RepoPicker({ os, active }: RepoPickerProps) {
  const [repos, setRepos] = useState<PackageRepo[] | null>(null)
  const [enabled, setEnabled] = useState<Set<string>>(new Set())
  const [error, setError] = useState<string | null>(null)

  // Refetch whenever the target changes (repos are OS-scoped) or the tab becomes
  // visible. Clear the previous target's list and error up front so a stale set
  // of repos never lingers under a new selection, and guard against out-of-order
  // responses with a cancel flag.
  useEffect(() => {
    setError(null)
    setRepos(null)
    setEnabled(new Set())
    if (!active || !os) return
    let cancelled = false
    api
      .listPackageRepos(os)
      .then((r) => {
        if (cancelled) return
        setRepos(r.repos)
        setEnabled(new Set(r.repos.filter((x) => x.enabledByDefault).map((x) => x.id)))
      })
      .catch((e) => {
        if (!cancelled) setError((e as Error).message)
      })
    return () => {
      cancelled = true
    }
  }, [active, os])

  const toggle = (id: string) =>
    setEnabled((prev) => {
      const next = new Set(prev)
      if (!next.delete(id)) next.add(id)
      return next
    })

  return (
    <div className="rounded-lg border border-slate-200 bg-white shadow-sm">
      <div className="flex items-center justify-between border-b border-slate-200 px-4 py-2">
        <span className="text-xs font-semibold uppercase tracking-wide text-slate-500">
          Package Repositories
        </span>
        {repos && repos.length > 0 && (
          <span className="text-xs text-slate-400">
            {enabled.size} of {repos.length} enabled
          </span>
        )}
      </div>

      {error && <div className="m-3 rounded bg-red-50 p-3 text-sm text-red-700">{error}</div>}

      {!error && !os && (
        <p className="px-4 py-6 text-center text-sm text-slate-400">
          Select an operating system to see its repositories.
        </p>
      )}

      {!error && os && repos === null && (
        <p className="px-4 py-6 text-center text-sm text-slate-400">Loading repositories…</p>
      )}

      {!error && os && repos?.length === 0 && (
        <p className="px-4 py-6 text-center text-sm text-slate-400">
          No repositories are configured for this target.
        </p>
      )}

      {!error && repos && repos.length > 0 && (
        <ul className="divide-y divide-slate-100">
          {repos.map((r) => (
            <li key={r.id} className="flex items-start gap-3 px-4 py-2.5">
              <input
                type="checkbox"
                id={`repo-${r.id}`}
                checked={enabled.has(r.id)}
                onChange={() => toggle(r.id)}
                className="mt-0.5 h-4 w-4 shrink-0 accent-[#0071c5]"
              />
              <div className="min-w-0 flex-1">
                <label
                  htmlFor={`repo-${r.id}`}
                  className="flex cursor-pointer flex-wrap items-center gap-2 text-sm font-medium text-slate-700"
                >
                  {r.displayName}
                  {r.enabledByDefault && (
                    <span className="rounded bg-slate-100 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-slate-500">
                      Base
                    </span>
                  )}
                  {/* Only surface a non-default priority: showing "500" on every
                      row is noise, while a raised value explains why this repo
                      wins a package that also exists elsewhere. */}
                  {r.priority != null && r.priority !== 500 && (
                    <span className="rounded bg-amber-50 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-amber-700">
                      Priority {r.priority}
                    </span>
                  )}
                </label>
                {r.description && (
                  <p className="mt-0.5 text-xs text-slate-500">{r.description}</p>
                )}
                <p className="mt-0.5 truncate font-mono text-[11px] text-slate-400" title={r.url}>
                  {r.url}
                </p>
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
