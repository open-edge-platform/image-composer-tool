import { useEffect, useState } from 'react'
import { api } from '../api/client'
import type { PackageRepo } from '../api/types'
import { PackageRepoBrowser } from './PackageRepoBrowser'

interface PackagesStepProps {
  // Target OS id (a manifest `targets[].id`, e.g. "ubuntu24"). Empty until the
  // selection reaches an OS; the step stays idle rather than listing every repo
  // in the catalog for a target the user hasn't chosen yet.
  os: string
  // True only while the Advanced tab is visible. Both tab pages stay mounted, so
  // the fetch is gated on this to keep a hidden page from issuing requests.
  active: boolean
}

// PackagesStep is the wizard's "Repositories & Packages" step. It currently
// covers the repository half: which repositories the target offers, and which
// are enabled. Package search and selection need the package index API and
// arrive later.
export function PackagesStep({ os, active }: PackagesStepProps) {
  const [repos, setRepos] = useState<PackageRepo[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [activeRepo, setActiveRepo] = useState('')

  useEffect(() => {
    if (!os) {
      setRepos(null)
      setError(null)
      setActiveRepo('')
      return
    }
    if (!active) return

    setError(null)
    setRepos(null)
    let cancelled = false
    api
      .listPackageRepos(os)
      .then((r) => {
        if (cancelled) return
        setRepos(r.repos)
        // Open on the target's base repository rather than the first row. The
        // API orders by descending priority, so the base repo (priority 500) is
        // usually last — starting at repos[0] would open on a disabled pane.
        setActiveRepo(r.repos.find((x) => x.enabledByDefault)?.id ?? r.repos[0]?.id ?? '')
      })
      .catch((e) => {
        if (cancelled) return
        setError((e as Error).message)
      })
    return () => {
      cancelled = true
    }
  }, [active, os])

  return (
    <div>
      <h2 className="mb-1 text-lg font-bold text-[#00285a]">Repositories &amp; Packages</h2>
      <p className="mb-3 text-sm text-slate-500">
        Choose which package repositories to pull from. Enable a repository with
        its checkbox, or click it to see what it provides.
      </p>
      <p className="mb-5 rounded border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800">
        Repository toggles are UI state in this update — they aren't sent to the
        compose request yet, so the Review step's YAML won't change.
      </p>

      {error && <div className="mb-3 rounded bg-red-50 p-3 text-sm text-red-700">{error}</div>}

      {!error && !os && (
        <p className="rounded-lg border border-dashed border-slate-300 px-4 py-8 text-center text-sm text-slate-400">
          Select an operating system in the Target step to see its repositories.
        </p>
      )}

      {!error && os && repos === null && (
        <p className="rounded-lg border border-dashed border-slate-300 px-4 py-8 text-center text-sm text-slate-400">
          Loading repositories…
        </p>
      )}

      {!error && os && repos?.length === 0 && (
        <p className="rounded-lg border border-dashed border-slate-300 px-4 py-8 text-center text-sm text-slate-400">
          No repositories are configured for this target.
        </p>
      )}

      {!error && os && repos && repos.length > 0 && (
        <PackageRepoBrowser
          repos={repos}
          activeRepo={activeRepo}
          onActiveRepo={setActiveRepo}
        />
      )}
    </div>
  )
}
