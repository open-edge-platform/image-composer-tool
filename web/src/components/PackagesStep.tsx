import { useEffect, useRef, useState } from 'react'
import { useStore, confirmRepoRelease } from '../store'
import { api } from '../api/client'
import type { EdgePack, PackageRepo, PackageSearchResult, PackageVersion } from '../api/types'
import { EdgePackBrowser } from './EdgePackBrowser'
import { PackageRepoBrowser } from './PackageRepoBrowser'
import { PackageRow } from './PackageRow'
import { SelectedPackages } from './SelectedPackages'
import { useBaseLock, ensureBaseVersions, type BaseLock } from '../lib/baseLock'

interface PackagesStepProps {
  // Target OS id (a manifest `targets[].id`, e.g. "ubuntu24"). Empty until the
  // selection reaches an OS; the step stays idle rather than listing every repo
  // in the catalog for a target the user hasn't chosen yet.
  os: string
  // True only while the Advanced tab is visible. Both tab pages stay mounted, so
  // the fetch is gated on this to keep a hidden page from issuing requests.
  active: boolean
  // The matched template's own package list (ComposeResponse.basePackages),
  // fetched by AdvancedPage's compose call. Empty until that call resolves.
  basePackages: string[]
}

// The two browsing surfaces over one selection. Edge Pack groups packages by
// what they let an image do, Repositories by where they come from; a package
// picked on either is picked on both, because both write into the same
// addedPackages list.
const TABS = [
  { id: 'edge-pack', label: 'Edge Pack' },
  { id: 'repos', label: 'Repositories' },
] as const

type TabID = (typeof TABS)[number]['id']

// PackagesStep is the wizard's "Choose Packages to Compose" step: which
// repositories the target offers and which are enabled, a cross-repository
// package search, the Edge Pack and per-repository browsing surfaces, and the
// running list of added packages.
export function PackagesStep({ os, active, basePackages }: PackagesStepProps) {
  const [repos, setRepos] = useState<PackageRepo[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  // undefined while in flight, null once it is known there is no Edge Pack to
  // show. The two are kept apart so the tab can say "loading" rather than
  // "unavailable" for the moment before the response lands. A failed fetch
  // degrades this step to the Repositories tab rather than blanking it, so it
  // is not raised into `error` — that would hide the whole step.
  const [edgePack, setEdgePack] = useState<EdgePack | null | undefined>(undefined)
  // Tab choice is local state: selections live in the store, so switching tabs
  // cannot disturb them. Edge Pack leads because a capability is the question
  // most users arrive with; provenance is the follow-up.
  const [tab, setTab] = useState<TabID>('edge-pack')
  const manifest = useStore((s) => s.manifest)

  useEffect(() => {
    if (!os) {
      setRepos(null)
      setEdgePack(undefined)
      setError(null)
      return
    }
    if (!active) return

    setError(null)
    setRepos(null)
    setEdgePack(undefined)
    let cancelled = false
    api
      .listPackageRepos(os)
      .then((r) => {
        if (cancelled) return
        setRepos(r.repos)
      })
      .catch((e) => {
        if (cancelled) return
        setError((e as Error).message)
      })
    // Fetched alongside the repos rather than on first showing the tab, so the
    // domain counts are right the moment the step opens. A backend that
    // predates /edge-pack 404s here, which is why this failure is swallowed
    // into "no Edge Pack" instead of failing the step.
    api
      .getEdgePack(os)
      .then((p) => {
        if (!cancelled) setEdgePack(p)
      })
      .catch(() => {
        if (!cancelled) setEdgePack(null)
      })
    return () => {
      cancelled = true
    }
  }, [active, os])

  const baseLock = useBaseLock(os, repos, basePackages)
  const targetLabel = manifest?.targets.find((t) => t.id === os)?.displayName ?? os
  const repoLabelFor = (id: string) => repos?.find((r) => r.id === id)?.displayName ?? id

  return (
    <div>
      <h2 className="mb-1 text-lg font-bold text-[#00285a]">Choose Packages to Compose</h2>
      <p className="mb-3 text-sm text-slate-500">
        Choose which package repositories to pull from, then search or browse
        for packages to add. What you select appears in the Review step&apos;s
        template and is installed by the build.
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
            {/* Search sits above the tabs rather than inside one: it looks
                across every repository, so it is the path that works when you
                don't know which surface a package lives on. */}
            <PackageSearch os={os} repos={repos} baseLock={baseLock} />
            <BrowseTabs tab={tab} onTab={setTab} />
            <div role="tabpanel" id={`panel-${tab}`} aria-labelledby={`tab-${tab}`}>
              {tab === 'repos' ? (
                <PackageRepoBrowser repos={repos} os={os} baseLock={baseLock} />
              ) : edgePack ? (
                <EdgePackBrowser
                  pack={edgePack}
                  repoLabel={repoLabelFor(edgePack.repo)}
                  targetLabel={targetLabel}
                />
              ) : (
                <p className="rounded-lg border border-dashed border-slate-300 px-4 py-10 text-center text-sm text-slate-400">
                  {edgePack === undefined
                    ? 'Loading Edge Pack…'
                    : 'Edge Pack is unavailable. Use the Repositories tab to pick packages.'}
                </p>
              )}
            </div>
          </div>
          <div className="sticky top-4 flex flex-col gap-3">
            <SelectedPackages repos={repos} />
          </div>
        </div>
      )}
    </div>
  )
}

// BrowseTabs switches between the two browsing surfaces. Both tabs are always
// offered, including while Edge Pack is still loading — a tab strip that grows
// a tab a moment after the step opens would move the Repositories tab out from
// under a click already on its way to it.
//
// Only the active tab is in the tab order, with the arrow keys moving between
// them, as expected of a tablist; Tab itself therefore leaves the strip rather
// than walking through every tab.
function BrowseTabs({ tab, onTab }: { tab: TabID; onTab: (t: TabID) => void }) {
  const move = (from: TabID, key: string) => {
    if (key !== 'ArrowRight' && key !== 'ArrowLeft') return
    const i = TABS.findIndex((t) => t.id === from)
    const step = key === 'ArrowRight' ? 1 : TABS.length - 1
    const next = TABS[(i + step) % TABS.length].id
    onTab(next)
    // Focus follows the selection so a keyboard user lands on the control they
    // just moved to. Harmless on a mouse click, which already focused it.
    document.getElementById(`tab-${next}`)?.focus()
  }

  return (
    <div role="tablist" aria-label="Browse packages by" className="mb-3 flex gap-1 border-b border-slate-200">
      {TABS.map((t) => {
        const activeTab = t.id === tab
        return (
          <button
            key={t.id}
            type="button"
            role="tab"
            id={`tab-${t.id}`}
            aria-selected={activeTab}
            aria-controls={`panel-${t.id}`}
            tabIndex={activeTab ? 0 : -1}
            onClick={() => onTab(t.id)}
            onKeyDown={(e) => {
              if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft') return
              e.preventDefault()
              move(t.id, e.key)
            }}
            className={
              '-mb-px border-b-2 px-3.5 py-2 text-[13px] font-semibold ' +
              (activeTab
                ? 'border-[#0071c5] text-[#0071c5]'
                : 'border-transparent text-slate-500 hover:text-slate-700')
            }
          >
            {t.label}
          </button>
        )
      })}
    </div>
  )
}

// How many hits each repository contributes, and how many the dropdown shows
// once they are merged.
const SEARCH_LIMIT = 8

// How many past queries the search dropdown's "Recent searches" list keeps.
const HISTORY_LIMIT = 8

// SearchOutcome is the stream's terminal `done` event: how many repositories
// were searched, how many reported an error, and whether the stream was cut
// short by the server's budget before every one answered.
interface SearchOutcome {
  repos: number
  failed: number
  truncated: boolean
  // Set when the stream broke before its `done` event, which is the only
  // message carrying real counts. The client cannot derive them: a `hits` event
  // arrives only from a repository that matched something, so counting those
  // would report far fewer repositories than were actually searched. An
  // interrupted outcome therefore reports no numbers at all.
  interrupted?: boolean
}

// versionsOf normalises a hit's version list, tolerating a backend that
// predates the `versions` field by falling back to the single version it does
// report.
function versionsOf(p: PackageSearchResult): PackageVersion[] {
  return p.versions?.length ? p.versions : [{ version: p.version, repository: p.repository }]
}

// mergeHits flattens the per-repository batches into one ranked list.
//
// A package several repositories carry appears once, offering the versions
// from all of them: the server groups versions within a repository, but only
// the client sees every repository's batch. Which repository leads the merged
// list is decided by catalog priority rather than arrival order — batches
// arrive as each repository finishes, so otherwise the "via <repo>" label and
// the repository a pick enables would depend on which mirror answered first.
function mergeHits(
  byRepo: Map<string, PackageSearchResult[]>,
  priorityOf: (repoId: string) => number,
): PackageSearchResult[] {
  const merged = new Map<string, PackageSearchResult>()
  for (const batch of byRepo.values()) {
    for (const p of batch) {
      const held = merged.get(p.name)
      if (!held) {
        merged.set(p.name, { ...p, versions: versionsOf(p) })
        continue
      }
      // Union the version lists, keeping each repository's own ordering and
      // putting the higher-priority repository's versions first. Comparing
      // versions across repositories would need the target's version rules,
      // which only the server has.
      const [a, b] = [priorityOf(p.repository), priorityOf(held.repository)]
      const leadsWithP = a > b || (a === b && p.repository < held.repository)
      const [first, second] = leadsWithP ? [p, held] : [held, p]
      const seen = new Set<string>()
      const versions = [...versionsOf(first), ...versionsOf(second)].filter((v) => {
        const k = `${v.repository} ${v.version}`
        if (seen.has(k)) return false
        seen.add(k)
        return true
      })
      merged.set(p.name, { ...first, versions })
    }
  }
  return [...merged.values()]
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
function PackageSearch({ os, repos, baseLock }: { os: string; repos: PackageRepo[]; baseLock: BaseLock }) {
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<PackageSearchResult[]>([])
  const [open, setOpen] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [outcome, setOutcome] = useState<SearchOutcome | null>(null)
  // Past queries that actually ran, most recent first, offered when the
  // input is focused empty. Session-only by design — there's no need to
  // persist a search history across reloads.
  const [history, setHistory] = useState<string[]>([])
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
    let cancelled = false
    const debounce = setTimeout(() => {
      // Recorded here, not on every keystroke: the debounce only fires once
      // the user actually pauses on a query, which is what "searched for X"
      // should mean.
      setHistory((h) => [q, ...h.filter((x) => x !== q)].slice(0, HISTORY_LIMIT))
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
      // means the stream itself broke (or the backend has no such route and
      // the SPA fallback answered with HTML).
      es.addEventListener('error', () => {
        es?.close()
        if (byRepo.size > 0) {
          // Keep what already arrived rather than replacing it with an error:
          // a stream that dies partway still found real packages.
          setLoading(false)
          setOutcome({ repos: 0, failed: 0, truncated: false, interrupted: true })
          return
        }
        // Nothing arrived, so fall back to the non-streaming endpoint. It is
        // slower, but it works against a backend that predates the stream
        // route or a proxy that buffers text/event-stream.
        api
          .searchPackages({ q, os, limit: SEARCH_LIMIT })
          .then((r) => {
            if (cancelled) return
            setResults(r.packages)
          })
          .catch((e) => {
            if (cancelled) return
            setError((e as Error).message)
          })
          .finally(() => {
            if (!cancelled) setLoading(false)
          })
      })
    }, 300)
    return () => {
      clearTimeout(debounce)
      cancelled = true
      // Closing the stream is what actually cancels a superseded keystroke;
      // without it the server keeps fanning out across every repository for a
      // query the user has already replaced.
      es?.close()
    }
  }, [query, os, repos])

  // Resolve default-repo versions for any locked, unpinned result as it
  // renders, so its "current" chip can be marked once that lands.
  useEffect(() => {
    ensureBaseVersions(baseLock, results.map((r) => r.name))
  }, [results, baseLock])

  const labelFor = (repoId: string) => repos.find((r) => r.id === repoId)?.displayName ?? repoId
  const isEnabled = (repoId: string) => {
    const r = repos.find((x) => x.id === repoId)
    return r?.enabledByDefault || enabledRepos.includes(repoId)
  }
  const isBaseRepo = (repoId: string) => repos.find((r) => r.id === repoId)?.enabledByDefault ?? false

  // Adding a package is a statement of interest in its repo, so it's brought
  // into the enabled set even if the user never touched that repo's checkbox.
  // The repo comes from the chosen version, not the row: pinning an older
  // version can select a different repository than the newest one came from
  // — if it does, and nothing else needs the one it's leaving, confirm
  // before dropping that repo too (it might have been checked by hand).
  //
  // Picking any version (not just ticking the row's checkbox) is treated as
  // a finished decision and closes the dropdown, matching how a single
  // click elsewhere already would.
  const add = (hit: PackageSearchResult, repo: string, version: string) => {
    const previous = addedPackages.find((p) => p.name === hit.name)
    const opts =
      previous && previous.repo !== repo
        ? { releaseRepo: confirmRepoRelease(addedPackages, labelFor, [hit.name], isBaseRepo) }
        : undefined
    setPackage({ name: hit.name, version, repo }, opts)
    if (!isEnabled(repo)) setRepoEnabled(repo, true)
    setQuery('')
    setOpen(false)
  }

  return (
    <>
      <div
        ref={containerRef}
        className="relative"
        onBlur={(e) => {
          // Attached to the whole dropdown, not just the input: clicking a
          // version chip moves focus to that chip's button (deliberately,
          // so the dropdown stays open for comparing versions), so the input
          // is no longer the focused element afterward. An onBlur on the
          // input alone would then miss a later click elsewhere entirely,
          // since it fires on whatever's actually focused. This fires
          // whenever focus leaves the container for good, wherever it was.
          if (e.currentTarget.contains(e.relatedTarget as Node)) return
          setOpen(false)
        }}
      >
        <input
          type="text"
          value={query}
          onChange={(e) => {
            setQuery(e.target.value)
            setOpen(true)
          }}
          onFocus={() => setOpen(true)}
          placeholder="Search packages across all repositories…"
          className="w-full rounded border border-slate-300 px-3 py-2 text-sm focus:border-[#0071c5] focus:outline-none"
        />
        {open && (
          <div className="absolute z-10 mt-1 max-h-[360px] w-full overflow-y-auto rounded-lg border border-slate-200 bg-white shadow-lg">
            {query.trim().length < 2 ? (
              history.length > 0 ? (
                <div className="py-1">
                  <p className="px-3 pb-1 text-[11px] font-semibold uppercase tracking-wide text-slate-400">
                    Recent searches
                  </p>
                  {history.map((h) => (
                    <button
                      key={h}
                      type="button"
                      onClick={() => setQuery(h)}
                      className="block w-full px-3 py-1.5 text-left text-[13px] text-slate-700 hover:bg-[#eef4fb]"
                    >
                      {h}
                    </button>
                  ))}
                </div>
              ) : (
                <p className="px-3 py-4 text-center text-[12px] text-slate-500">
                  Type at least 2 characters to search.
                </p>
              )
            ) : error ? (
              <p className="px-3 py-4 text-center text-[12px] text-red-600">{error}</p>
            ) : (
              <>
                {/* Hits render while the stream is still open, so a slow
                    repository never hides what the fast ones already found. */}
                {results.map((hit) => {
                  const lock = baseLock.info(hit.name)
                  return (
                    <PackageRow
                      key={`${hit.repository}:${hit.name}`}
                      name={hit.name}
                      version={hit.version}
                      description={hit.description}
                      versions={versionsOf(hit)}
                      repoLabelFor={labelFor}
                      selection={addedPackages.find((p) => p.name === hit.name)}
                      locked={lock.locked}
                      currentVersion={lock.currentVersion}
                      currentIsFloating={lock.currentIsFloating}
                      onToggle={(checked) => {
                        if (checked) {
                          add(hit, hit.repository, '')
                          return
                        }
                        removePackage(hit.name, {
                          releaseRepo: confirmRepoRelease(addedPackages, labelFor, [hit.name], isBaseRepo),
                        })
                      }}
                      onChooseVersion={(v) => add(hit, v.repository, v.version)}
                    />
                  )
                })}
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
      <p className="mb-4 mt-1.5 text-sm text-slate-500">
        Search across all repositories. Pick &quot;Latest&quot; or a specific
        version to add it.
      </p>
    </>
  )
}

// partialNote describes what the search could not cover, so an incomplete
// result set is stated rather than passed off as the whole answer. Returns null
// when every repository reported.
function partialNote(outcome: SearchOutcome | null): string | null {
  if (!outcome) return null
  // No counts are known for an interrupted stream, so none are quoted — a
  // number derived from the batches that happened to arrive would understate
  // what was searched and read as authoritative.
  if (outcome.interrupted) {
    return 'The search was interrupted before every repository reported — these results may be incomplete.'
  }
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
