import { useEffect, useRef, useState } from 'react'
import { useStore } from '../store'
import { api } from '../api/client'
import type { PackageRepo, PackageSearchResult } from '../api/types'
import { PackageRepoBrowser } from './PackageRepoBrowser'
import { PackageRow } from './PackageRow'
import { SelectedPackages } from './SelectedPackages'

interface PackagesStepProps {
  // Target OS id (a manifest `targets[].id`, e.g. "ubuntu24"). Empty until the
  // selection reaches an OS; the step stays idle rather than listing every repo
  // in the catalog for a target the user hasn't chosen yet.
  os: string
  // True only while the Advanced tab is visible. Both tab pages stay mounted, so
  // the fetch is gated on this to keep a hidden page from issuing requests.
  active: boolean
}

// PackagesStep is the wizard's "Repositories & Packages" step: which
// repositories the target offers and which are enabled, a cross-repository
// package search, per-repository browsing, and the running list of added
// packages.
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
        Choose which package repositories to pull from, then search or browse
        for packages to add.
      </p>
      <p className="mb-5 rounded border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800">
        Repository toggles and package selections are UI state in this update —
        they aren't sent to the compose request yet, so the Review step's YAML
        won't change.
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
        // items-start is what makes the selected rail's `sticky` actually
        // stick. Under the default `stretch` the rail is stretched to the full
        // row height, so it exactly fills its grid area and sticky has no
        // travel room — it scrolls away with the content at short viewports.
        <div className="grid grid-cols-[1fr_300px] items-start gap-5">
          <div>
            <PackageSearch os={os} repos={repos} />
            <PackageRepoBrowser
              repos={repos}
              activeRepo={activeRepo}
              onActiveRepo={setActiveRepo}
              os={os}
            />
          </div>
          <SelectedPackages repos={repos} />
        </div>
      )}
    </div>
  )
}

// How many hits each repository contributes, and how many the dropdown shows
// once they are merged.
const SEARCH_LIMIT = 8

// SearchOutcome is the stream's terminal `done` event: how many repositories
// were searched, how many reported an error, and whether the stream was cut
// short by the server's budget before every one answered.
interface SearchOutcome {
  repos: number
  failed: number
  truncated: boolean
}

// mergeHits flattens the per-repository batches into one ranked list.
//
// A package several repositories carry is shown once, credited to the
// highest-priority one. Batches arrive in completion order, not catalog order,
// so picking the first to report would make the "via <repo>" label — and which
// repository a pick enables — depend on which mirror happened to answer first.
function mergeHits(
  byRepo: Map<string, PackageSearchResult[]>,
  priorityOf: (repoId: string) => number,
): PackageSearchResult[] {
  const best = new Map<string, PackageSearchResult>()
  for (const batch of byRepo.values()) {
    for (const p of batch) {
      const held = best.get(p.name)
      if (!held) {
        best.set(p.name, p)
        continue
      }
      const [a, b] = [priorityOf(p.repository), priorityOf(held.repository)]
      if (a > b || (a === b && p.repository < held.repository)) best.set(p.name, p)
    }
  }
  return [...best.values()]
    .sort((a, b) => a.name.localeCompare(b.name))
    .slice(0, SEARCH_LIMIT)
}

// PackageSearch searches across every repository the target offers (not just
// the enabled ones — picking a hit auto-enables its source repo). Gated to
// queries of at least 2 characters, matching the backend's own minimum: an
// empty query means "browse the whole catalog," which is too expensive to
// trigger on every keystroke.
//
// Results stream in per repository rather than arriving all at once: a search
// fans out over the whole catalog, and an unreachable mirror would otherwise
// hold up hits already found elsewhere until it hit its dial timeout.
function PackageSearch({ os, repos }: { os: string; repos: PackageRepo[] }) {
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<PackageSearchResult[]>([])
  const [open, setOpen] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [outcome, setOutcome] = useState<SearchOutcome | null>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  const addedPackages = useStore((s) => s.addedPackages)
  const setPackage = useStore((s) => s.setPackage)
  const removePackage = useStore((s) => s.removePackage)
  const setRepoEnabled = useStore((s) => s.setRepoEnabled)
  const enabledRepos = useStore((s) => s.enabledRepos)

  useEffect(() => {
    const q = query.trim()
    setOutcome(null)
    if (q.length < 2) {
      setResults([])
      setError(null)
      setLoading(false)
      return
    }
    setLoading(true)
    setResults([])
    let es: EventSource | null = null
    const debounce = setTimeout(() => {
      // Batches arrive per repository. Merging here (rather than showing them
      // grouped) keeps the dropdown ranked by name as it fills in, so a hit
      // doesn't jump around as later repositories report.
      const byRepo = new Map<string, PackageSearchResult[]>()
      const priorityOf = (repoId: string) => repos.find((r) => r.id === repoId)?.priority ?? 0
      es = new EventSource(api.searchStreamUrl({ q, os, limit: SEARCH_LIMIT }))

      es.addEventListener('hits', (e) => {
        const data = JSON.parse((e as MessageEvent).data) as {
          repo: string
          packages: PackageSearchResult[]
        }
        byRepo.set(data.repo, data.packages)
        setResults(mergeHits(byRepo, priorityOf))
      })
      es.addEventListener('done', (e) => {
        const data = JSON.parse((e as MessageEvent).data) as SearchOutcome
        es?.close()
        setOutcome(data)
        setLoading(false)
      })
      // `error` is EventSource's own transport-failure event — the server
      // deliberately never sends one by that name, so reaching here always
      // means the stream itself broke.
      es.addEventListener('error', () => {
        es?.close()
        setLoading(false)
        setError('Search connection failed.')
      })
    }, 300)
    return () => {
      clearTimeout(debounce)
      // Closing the stream is what actually cancels a superseded keystroke;
      // without it the server keeps fanning out across every repository for a
      // query the user has already replaced.
      es?.close()
    }
  }, [query, os, repos])

  const labelFor = (repoId: string) => repos.find((r) => r.id === repoId)?.displayName ?? repoId
  const isEnabled = (repoId: string) => {
    const r = repos.find((x) => x.id === repoId)
    return r?.enabledByDefault || enabledRepos.includes(repoId)
  }

  // Adding a package is a statement of interest in its repo, so it's brought
  // into the enabled set even if the user never touched that repo's checkbox.
  const pick = (hit: PackageSearchResult, version: string) => {
    setPackage({ name: hit.name, version, repo: hit.repository })
    if (!isEnabled(hit.repository)) setRepoEnabled(hit.repository, true)
    setQuery('')
    setOpen(false)
  }

  return (
    <div ref={containerRef} className="relative mb-4">
      <input
        type="text"
        value={query}
        onChange={(e) => {
          setQuery(e.target.value)
          setOpen(true)
        }}
        onFocus={() => setOpen(true)}
        onBlur={(e) => {
          // Guards against closing before a click inside the dropdown (e.g. a
          // version chip) registers — a setTimeout-based delay would instead
          // race that click.
          if (containerRef.current?.contains(e.relatedTarget as Node)) return
          setOpen(false)
        }}
        placeholder="Search packages across all repositories…"
        className="w-full rounded border border-slate-300 px-3 py-2 text-sm focus:border-[#0071c5] focus:outline-none"
      />
      {open && (
        <div className="absolute z-10 mt-1 max-h-[360px] w-full overflow-y-auto rounded-lg border border-slate-200 bg-white shadow-lg">
          {query.trim().length < 2 ? (
            <p className="px-3 py-4 text-center text-[12px] text-slate-500">
              Type at least 2 characters to search.
            </p>
          ) : error ? (
            <p className="px-3 py-4 text-center text-[12px] text-red-600">{error}</p>
          ) : (
            <>
              {/* Hits render while the stream is still open, so a slow
                  repository never hides what the fast ones already found. */}
              {results.map((hit) => (
                <PackageRow
                  key={`${hit.repository}:${hit.name}`}
                  name={hit.name}
                  version={hit.version}
                  description={hit.description}
                  repoLabel={labelFor(hit.repository)}
                  showRepo
                  selection={addedPackages.find((p) => p.name === hit.name)}
                  onToggle={(checked) => (checked ? pick(hit, '') : removePackage(hit.name))}
                  onChooseVersion={(v) => pick(hit, v)}
                />
              ))}
              {loading ? (
                <p className="px-3 py-2 text-center text-[11px] text-slate-500">
                  {results.length > 0 ? 'Searching more repositories…' : 'Searching…'}
                </p>
              ) : results.length === 0 ? (
                <p className="px-3 py-4 text-center text-[12px] text-slate-500">
                  No matching packages{partialNote(outcome) ? ' yet' : ''}.
                  {partialNote(outcome) && (
                    <span className="mt-1 block text-[11px] text-slate-400">
                      {partialNote(outcome)}
                    </span>
                  )}
                </p>
              ) : (
                partialNote(outcome) && (
                  <p className="border-t border-slate-100 px-3 py-2 text-[11px] text-slate-400">
                    {partialNote(outcome)}
                  </p>
                )
              )}
            </>
          )}
        </div>
      )}
    </div>
  )
}

// partialNote describes what the search could not cover, so an incomplete
// result set is stated rather than passed off as the whole answer. Returns null
// when every repository reported.
function partialNote(outcome: SearchOutcome | null): string | null {
  if (!outcome) return null
  const parts: string[] = []
  if (outcome.failed > 0) {
    parts.push(`${outcome.failed} of ${outcome.repos} repositories unreachable`)
  }
  if (outcome.truncated) {
    parts.push('some repositories did not respond in time')
  }
  if (parts.length === 0) return null
  return `Searched ${outcome.repos} repositories — ${parts.join('; ')}.`
}
