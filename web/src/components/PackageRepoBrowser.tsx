import { useEffect, useState } from 'react'
import { useStore } from '../store'
import { api } from '../api/client'
import type { PackageRepo, PackageSearchResult } from '../api/types'
import { PackageRow } from './PackageRow'

// Packages fetched per page when browsing a repo's full contents (no query).
const PAGE_SIZE = 100

interface PackageRepoBrowserProps {
  repos: PackageRepo[]
  activeRepo: string
  onActiveRepo: (id: string) => void
  os: string
}

// PackageRepoBrowser is the two-pane repository browser: a rail listing every
// repository offered for the target, and a pane describing whichever one is
// being browsed.
//
// Each rail row carries two separate affordances — the checkbox enables the
// repository, the row body selects it for browsing. They are siblings rather
// than nested (the prototype nests a checkbox inside a clickable div, which
// leaves the browse action unreachable by keyboard).
export function PackageRepoBrowser({ repos, activeRepo, onActiveRepo, os }: PackageRepoBrowserProps) {
  const enabledRepos = useStore((s) => s.enabledRepos)
  const setRepoEnabled = useStore((s) => s.setRepoEnabled)
  const active = repos.find((r) => r.id === activeRepo)

  // A repo the catalog marks enabledByDefault is the target's base repository:
  // every build reads it, so it cannot be turned off.
  const isEnabled = (r: PackageRepo) => r.enabledByDefault || enabledRepos.includes(r.id)

  const onToggle = (r: PackageRepo, on: boolean) => {
    setRepoEnabled(r.id, on)
    // Enabling a repo is a statement of interest in it, so bring it into the
    // pane. Disabling deliberately leaves the pane put — jumping away from the
    // row the user just clicked would be disorienting.
    if (on) onActiveRepo(r.id)
  }

  return (
    <>
      <div className="grid grid-cols-[240px_minmax(0,1fr)] overflow-hidden rounded-lg border border-slate-200">
        <div
          role="group"
          aria-label="Package repositories"
          className="max-h-[480px] overflow-y-auto border-r border-slate-200 bg-slate-50"
        >
          {repos.map((r) => (
            <RepoRailRow
              key={r.id}
              repo={r}
              enabled={isEnabled(r)}
              active={r.id === activeRepo}
              onToggle={(on) => onToggle(r, on)}
              onBrowse={() => onActiveRepo(r.id)}
            />
          ))}
        </div>
        <div
          role="region"
          aria-label="Repository details"
          className="max-h-[480px] overflow-y-auto px-[18px] py-3.5"
        >
          {active ? <RepoPane repo={active} enabled={isEnabled(active)} os={os} /> : <RepoPaneEmpty />}
        </div>
      </div>
      <p className="mt-2 text-sm text-slate-500">
        Enable a repository with its checkbox, then click it to browse. Browsing
        starts on the repo&apos;s full catalog (loaded in pages of 100); where a
        repository offers curated picks, check &quot;Show frequently used&quot; to
        narrow the list to them. The &quot;Select all&quot; checkbox above the
        list adds everything currently shown to your selection, and removes it
        again when unchecked.
      </p>
    </>
  )
}

function RepoRailRow({
  repo,
  enabled,
  active,
  onToggle,
  onBrowse,
}: {
  repo: PackageRepo
  enabled: boolean
  active: boolean
  onToggle: (on: boolean) => void
  onBrowse: () => void
}) {
  return (
    <div
      className={
        'flex items-center gap-2.5 border-b border-slate-100 px-3 py-2.5 ' +
        // The negative margin paints the row's background 1px past the rail's
        // right border, erasing that sliver of the divider so the active row
        // reads as continuous with the pane it describes.
        (active ? 'bg-[#e6f2fa] shadow-[inset_4px_0_0_#0071c5] -mr-px' : 'hover:bg-[#eef4fb]')
      }
    >
      <input
        type="checkbox"
        id={`repo-${repo.id}`}
        checked={enabled}
        disabled={repo.enabledByDefault}
        onChange={(e) => onToggle(e.target.checked)}
        className="h-[15px] w-[15px] shrink-0 accent-[#0071c5] disabled:cursor-not-allowed"
        title={repo.enabledByDefault ? 'Base repository — always enabled' : undefined}
      />
      <button
        type="button"
        onClick={onBrowse}
        // Marks which repository the adjacent pane is describing, so the active
        // row is conveyed by more than the inset bar and colour shift.
        aria-current={active ? 'true' : undefined}
        className="flex min-w-0 flex-1 items-center gap-2 text-left"
      >
        <span className="min-w-0 flex-1">
          <span
            // The 240px rail truncates the longer Intel repo names, so keep the
            // full one reachable on hover.
            title={repo.displayName}
            className={
              'block truncate text-[13px] ' +
              (active ? 'font-bold text-[#0071c5]' : 'font-semibold text-slate-700')
            }
          >
            {repo.displayName}
          </span>
          {repo.enabledByDefault && (
            <span className="text-[11px] text-slate-500">Base — always on</span>
          )}
        </span>
        {/* Kept in the layout when inactive so a row never changes height. */}
        <span
          aria-hidden="true"
          className={'shrink-0 text-xs text-[#0071c5] ' + (active ? 'visible' : 'invisible')}
        >
          ▸
        </span>
      </button>
    </div>
  )
}

function RepoPane({ repo, enabled, os }: { repo: PackageRepo; enabled: boolean; os: string }) {
  const [hits, setHits] = useState<PackageSearchResult[]>([])
  const [total, setTotal] = useState(0)
  // The full-catalog count, tracked separately from `total` so the empty
  // state under "Show frequently used" can say how many packages are being
  // hidden rather than just that none are curated.
  const [fullTotal, setFullTotal] = useState(0)
  const [frequentOnly, setFrequentOnly] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const addedPackages = useStore((s) => s.addedPackages)
  const setPackage = useStore((s) => s.setPackage)
  const removePackage = useStore((s) => s.removePackage)
  const setPackages = useStore((s) => s.setPackages)
  const removePackages = useStore((s) => s.removePackages)

  // Switching repos always starts back on the full catalog.
  useEffect(() => {
    setFrequentOnly(false)
  }, [repo.id])

  // Browse this repo's full contents (or its curated subset) on mount and
  // whenever the repo being browsed, or the curation toggle, changes.
  // Disabled repos don't fetch — there's nothing to show until the user
  // re-enables it.
  useEffect(() => {
    setHits([])
    setTotal(0)
    setError(null)
    if (!enabled) return
    setLoading(true)
    let cancelled = false
    api
      .searchPackages({ os, repos: [repo.id], limit: PAGE_SIZE, offset: 0, curated: frequentOnly })
      .then((r) => {
        if (cancelled) return
        setHits(r.packages)
        setTotal(r.total)
        if (!frequentOnly) setFullTotal(r.total)
      })
      .catch((e) => {
        if (cancelled) return
        setError((e as Error).message)
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [repo.id, enabled, os, frequentOnly])

  const loadMore = () => {
    setLoading(true)
    setError(null)
    api
      .searchPackages({ os, repos: [repo.id], limit: PAGE_SIZE, offset: hits.length, curated: frequentOnly })
      .then((r) => setHits((prev) => [...prev, ...r.packages]))
      .catch((e) => setError((e as Error).message))
      .finally(() => setLoading(false))
  }

  // Checked when every currently-loaded package is already selected.
  const allVisibleSelected =
    hits.length > 0 && hits.every((h) => addedPackages.some((p) => p.name === h.name))

  const onToggleSelectAll = (checked: boolean) => {
    if (checked) {
      // Skip rows already selected so a manually pinned version isn't reset
      // back to "latest".
      const toAdd = hits
        .filter((h) => !addedPackages.some((p) => p.name === h.name))
        .map((h) => ({ name: h.name, version: '', repo: h.repository }))
      setPackages(toAdd)
    } else {
      removePackages(hits.map((h) => h.name))
    }
  }

  return (
    <>
      <div className="mb-2.5 rounded-md bg-[#e6f2fa] px-3 py-2.5">
        <div className="text-[10px] font-bold uppercase tracking-[0.6px] text-[#0071c5]">
          Browsing
        </div>
        <div className="mt-px text-[15px] font-bold text-[#00285a]">{repo.displayName}</div>
        <p className="mt-0.5 break-all font-mono text-[11px] text-slate-500">
          {repo.url}
          {repo.priority != null && ` · priority ${repo.priority}`}
        </p>
        {repo.description && (
          <p className="mt-1 text-xs text-slate-600">{repo.description}</p>
        )}
      </div>
      {!enabled ? (
        <p className="px-5 py-12 text-center text-[13px] text-slate-500">
          <span className="font-semibold text-slate-600">This repository is disabled.</span>
          <br />
          Enable it with the checkbox on the left to pick from its packages.
        </p>
      ) : (
        <>
          {/* Only offered where the catalog defines curated picks for this
              repo. Elsewhere the toggle could only ever empty the list, which
              reads as the feature being broken rather than as "this repo has
              no curated picks". */}
          {repo.hasCuratedPackages && (
            <label className="mb-2 flex items-center gap-1.5 text-[12px] text-slate-600">
              <input
                type="checkbox"
                checked={frequentOnly}
                onChange={(e) => setFrequentOnly(e.target.checked)}
                className="h-[13px] w-[13px] accent-[#0071c5]"
              />
              Show frequently used
            </label>
          )}
          {error ? (
            <p className="px-5 py-12 text-center text-[13px] text-red-600">{error}</p>
          ) : hits.length === 0 ? (
            <p className="px-5 py-12 text-center text-[13px] text-slate-500">
              {loading
                ? 'Loading packages…'
                : frequentOnly
                  ? `No frequently used packages for this repository. Uncheck "Show frequently used" to browse all ${fullTotal}.`
                  : 'No packages found in this repository.'}
            </p>
          ) : (
            <>
              <label className="mb-1.5 flex items-center gap-1.5 border-b border-slate-100 pb-1.5 text-[12px] font-medium text-slate-600">
                <input
                  type="checkbox"
                  checked={allVisibleSelected}
                  onChange={(e) => onToggleSelectAll(e.target.checked)}
                  className="h-[13px] w-[13px] accent-[#0071c5]"
                />
                Select all
              </label>
              <div className="rounded border border-slate-100">
                {hits.map((h) => (
                  <PackageRow
                    key={h.name}
                    name={h.name}
                    version={h.version}
                    description={h.description}
                    repoLabel={repo.displayName}
                    showRepo={false}
                    versions={h.versions?.length ? h.versions : [{ version: h.version, repository: h.repository }]}
                    repoLabelFor={() => repo.displayName}
                    selection={addedPackages.find((p) => p.name === h.name)}
                    onToggle={(checked) =>
                      checked
                        ? setPackage({ name: h.name, version: '', repo: h.repository })
                        : removePackage(h.name)
                    }
                    onChooseVersion={(v) =>
                      setPackage({ name: h.name, version: v.version, repo: v.repository })
                    }
                  />
                ))}
              </div>
              {hits.length < total && (
                <button
                  type="button"
                  onClick={loadMore}
                  disabled={loading}
                  className="mt-2 w-full rounded border border-slate-200 py-1.5 text-[12px] font-medium text-[#0071c5] hover:bg-[#eef4fb] disabled:opacity-50"
                >
                  {loading ? 'Loading…' : `Load more (${hits.length} of ${total})`}
                </button>
              )}
            </>
          )}
        </>
      )}
    </>
  )
}

function RepoPaneEmpty() {
  return (
    <p className="px-5 py-12 text-center text-[13px] text-slate-500">
      Select a repository on the left.
    </p>
  )
}
