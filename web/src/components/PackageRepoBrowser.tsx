import { useEffect, useRef, useState } from 'react'
import { useStore, confirmRepoRelease } from '../store'
import { api } from '../api/client'
import type { PackageRepo, PackageSearchResult } from '../api/types'
import { PackageRow } from './PackageRow'
import { ensureBaseVersions, type BaseLock } from '../lib/baseLock'
import type { VersionResolver } from '../lib/useVersionResolver'

// Packages fetched per page when browsing the merged catalog (no query).
const PAGE_SIZE = 100

// Height cap for the package list. It tracks the viewport so the step's
// Back/Next row stays on screen: 37rem is roughly the page chrome above the
// list (header, stepper, search, tabs, repository toolbar, list controls) plus
// the nav row below it. The floor keeps several compact rows visible on short
// windows (the page scrolls instead); the ceiling stops very tall windows from
// stretching the list into a wall of rows.
const LIST_MAX_H = 'max-h-[clamp(240px,calc(100vh-37rem),680px)]'

interface PackageRepoBrowserProps {
  repos: PackageRepo[]
  os: string
  baseLock: BaseLock
  // Shared with the Selected rail and the search dropdown (lifted in
  // PackagesStep.tsx) so a floating pick resolves to the same version/repo
  // no matter which surface shows it.
  resolver: VersionResolver
  // Repos the matched template already draws a package from (currently just
  // Edge Pack's — see templateLockedRepos in lib/edgepack.ts). Treated the
  // same as a base repo: checked and unable to be unchecked.
  lockedRepoIds?: Set<string>
}

const noLockedRepos = new Set<string>()

// PackageRepoBrowser is the repository browser: a toolbar whose dropdown picks
// which repository's catalog is listed, above a full-width pane listing it.
// Using a repository for the build (the toolbar's "Use in builds" checkbox) is
// a separate action from browsing it — the checkbox controls whether a build
// resolves packages from it at all, while the dropdown only controls which
// catalog is shown. Keeping these apart is what lets a repo with tens of
// thousands of packages (Ubuntu's base repo, say) be checked for the build
// without flooding the browse list the moment it is.
//
// A repo can be in use either because the user checked it directly or
// because adding a package (from here or from search) enabled it on their
// behalf — the store doesn't distinguish the two. Every package mutation
// here therefore runs through confirmRepoRelease before it could uncheck a
// repo as a side effect, the same as the search dropdown does.
export function PackageRepoBrowser({
  repos,
  os,
  baseLock,
  resolver,
  lockedRepoIds = noLockedRepos,
}: PackageRepoBrowserProps) {
  const enabledRepos = useStore((s) => s.enabledRepos)
  const setRepoEnabled = useStore((s) => s.setRepoEnabled)

  // A base repo or a template-locked one is always enabled and can't be
  // turned off, so either belongs at the top of the dropdown rather than
  // wherever its priority happens to land it among the optional repos.
  // Stable sort keeps the server's priority-descending order within each
  // group.
  const alwaysOn = (r: PackageRepo) => r.enabledByDefault || lockedRepoIds.has(r.id)
  const pickerRepos = [...repos].sort((a, b) => Number(alwaysOn(b)) - Number(alwaysOn(a)))

  // A repo the catalog marks enabledByDefault is the target's base repository:
  // every build reads it, so it cannot be turned off. A repo in
  // lockedRepoIds is the same situation for a different reason: the matched
  // template already draws a package from it.
  const isEnabled = (r: PackageRepo) => alwaysOn(r) || enabledRepos.includes(r.id)

  // Which repository's catalog is listed — independent of which
  // repos are checked. Defaults to the top-ranked repo (base or
  // template-locked, if either exists) so there's always something to look
  // at, and re-picks a default if the current selection disappears (e.g. the
  // target OS changes) without overriding a user's own click.
  const [selectedRepoId, setSelectedRepoId] = useState<string | undefined>(undefined)
  const pickerRepoIdsKey = pickerRepos.map((r) => r.id).join(',')
  useEffect(() => {
    if (pickerRepos.length === 0) return
    if (selectedRepoId && pickerRepos.some((r) => r.id === selectedRepoId)) return
    setSelectedRepoId(pickerRepos[0].id)
    // eslint-disable-next-line react-hooks/exhaustive-deps -- pickerRepoIdsKey stands in for pickerRepos
  }, [pickerRepoIdsKey, selectedRepoId])
  const selectedRepo = pickerRepos.find((r) => r.id === selectedRepoId)

  // The curated-subset toggle only makes sense against whichever repo is
  // currently browsed, so switching repos resets it rather than carrying a
  // filter over that might not even apply to the new one.
  const [frequentOnly, setFrequentOnly] = useState(false)
  useEffect(() => {
    setFrequentOnly(false)
  }, [selectedRepoId])

  const inUseCount = pickerRepos.filter(isEnabled).length

  return (
    <div className="overflow-hidden rounded-lg border border-slate-200">
      {selectedRepo && (
        <RepoToolbar
          repos={pickerRepos}
          selectedRepo={selectedRepo}
          isEnabled={isEnabled}
          isAlwaysOn={alwaysOn}
          locked={lockedRepoIds.has(selectedRepo.id)}
          inUseCount={inUseCount}
          onSelect={setSelectedRepoId}
          onToggle={(on) => setRepoEnabled(selectedRepo.id, on)}
        />
      )}
      <div role="region" aria-label="Repository packages" className="bg-[#d6ecf8] px-3 py-2">
        <MergedPane
          repos={repos}
          isEnabled={isEnabled}
          selectedRepo={selectedRepo}
          os={os}
          baseLock={baseLock}
          resolver={resolver}
          lockedRepoIds={lockedRepoIds}
          frequentOnly={frequentOnly}
          onFrequentOnlyChange={selectedRepo?.hasCuratedPackages ? setFrequentOnly : undefined}
        />
      </div>
    </div>
  )
}

// RepoToolbar picks which repository's catalog is browsed (the dropdown) and,
// as a separate control, whether that repository is used by the build (the
// "Use in builds" checkbox). The dropdown alone never enables anything, so a
// repo with tens of thousands of packages can be looked through without
// changing what an unpinned pick resolves to.
//
// Only the browsed repo's checkbox is on screen, so the dropdown's own option
// labels carry every repo's state ("✓" for an optional repo in use) and the
// "N of M in use" count keeps the overall picture a list of every repo would give.
function RepoToolbar({
  repos,
  selectedRepo,
  isEnabled,
  isAlwaysOn,
  locked,
  inUseCount,
  onSelect,
  onToggle,
}: {
  repos: PackageRepo[]
  selectedRepo: PackageRepo
  isEnabled: (r: PackageRepo) => boolean
  isAlwaysOn: (r: PackageRepo) => boolean
  locked: boolean
  inUseCount: number
  onSelect: (repoId: string) => void
  onToggle: (on: boolean) => void
}) {
  const alwaysOnRepos = repos.filter(isAlwaysOn)
  const optionalRepos = repos.filter((r) => !isAlwaysOn(r))
  const badge = selectedRepo.enabledByDefault ? 'Base — always on' : locked ? 'In template — always on' : null
  const title = selectedRepo.enabledByDefault
    ? 'Base repository — always enabled'
    : locked
      ? "Already in template — can't be unchecked"
      : 'Only repositories in use are read when resolving packages at build time'
  return (
    <div className="border-b border-slate-200 bg-slate-50 px-3 py-2">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <label className="flex min-w-[280px] flex-1 items-center gap-2 text-[12px] font-semibold text-slate-600">
          Repository
          <select
            value={selectedRepo.id}
            onChange={(e) => onSelect(e.target.value)}
            // Long Intel repo names are why this caps rather than sizing to
            // content; the full name is in the option list and the tooltip.
            title={selectedRepo.displayName}
            className="min-w-0 max-w-[420px] flex-1 cursor-pointer truncate rounded-md border border-slate-300 bg-white px-2 py-1 text-[13px] font-semibold text-slate-700 hover:border-[#0071c5] focus:border-[#0071c5] focus:outline-none focus:ring-2 focus:ring-[#0071c5]/25"
          >
            {alwaysOnRepos.length > 0 && (
              <optgroup label="Always on">
                {alwaysOnRepos.map((r) => (
                  <option key={r.id} value={r.id}>
                    {r.displayName}
                  </option>
                ))}
              </optgroup>
            )}
            {optionalRepos.length > 0 && (
              <optgroup label="Optional">
                {optionalRepos.map((r) => (
                  <option key={r.id} value={r.id}>
                    {isEnabled(r) ? `✓ ${r.displayName}` : r.displayName}
                  </option>
                ))}
              </optgroup>
            )}
          </select>
        </label>
        <label
          title={title}
          className={
            'flex items-center gap-1.5 text-[12px] font-medium text-slate-700 ' +
            (badge ? 'cursor-not-allowed' : 'cursor-pointer')
          }
        >
          <input
            type="checkbox"
            aria-label={`Use ${selectedRepo.displayName} in builds`}
            checked={isEnabled(selectedRepo)}
            disabled={badge != null}
            onChange={(e) => onToggle(e.target.checked)}
            className="h-[14px] w-[14px] shrink-0 accent-[#0071c5] disabled:cursor-not-allowed"
          />
          Use in builds
          {badge && (
            <span className="rounded-full bg-slate-200 px-1.5 py-0.5 text-[10px] font-semibold text-slate-600">
              {badge}
            </span>
          )}
        </label>
        <span className="ml-auto text-[11px] text-slate-500">
          {inUseCount} of {repos.length} repositories in use
        </span>
      </div>
      <p className="mt-1 text-[11px] text-amber-700">
        Only repositories in use are read at build time — adding or removing one
        can change what an unpinned pick resolves to.
      </p>
    </div>
  )
}

function MergedPane({
  repos,
  isEnabled,
  selectedRepo,
  os,
  baseLock,
  resolver,
  lockedRepoIds,
  frequentOnly,
  onFrequentOnlyChange,
}: {
  repos: PackageRepo[]
  isEnabled: (r: PackageRepo) => boolean
  selectedRepo: PackageRepo | undefined
  os: string
  baseLock: BaseLock
  resolver: VersionResolver
  lockedRepoIds: Set<string>
  frequentOnly: boolean
  // Set only when the browsed repo has a curated subset to filter down to.
  onFrequentOnlyChange?: (on: boolean) => void
}) {
  const [hits, setHits] = useState<PackageSearchResult[]>([])
  const [total, setTotal] = useState(0)
  // The full-catalog count, tracked separately from `total` so the empty
  // state under "Show frequently used" can say how many packages are being
  // hidden rather than just that none are curated.
  const [fullTotal, setFullTotal] = useState(0)
  // 0-indexed current page. A catalog this large (tens of thousands of
  // packages) can't be browsed by accumulating "load more" pages — reaching
  // anything starting with a later letter would take hundreds of clicks — so
  // only one page is fetched and rendered at a time, with direct navigation.
  const [page, setPage] = useState(0)
  const [pageInput, setPageInput] = useState('1')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // When true, the network fetch below is skipped and the list shows what's
  // already picked from the browsed repo instead — reviewing selections
  // shouldn't depend on which page of a large catalog they happen to land
  // on. Only makes sense against whichever repo is currently browsed, so
  // switching repos resets it rather than carrying a review over that might
  // show nothing for the new one.
  const [selectedOnly, setSelectedOnly] = useState(false)
  useEffect(() => {
    setSelectedOnly(false)
  }, [selectedRepo?.id])
  // Toggling the review on or off leaves `page` wherever browsing the
  // catalog last left it — without this, unchecking it refetches that same
  // (possibly far-in) offset, which can legitimately come back empty even
  // though the repo has plenty of packages on earlier pages.
  useEffect(() => {
    setPage(0)
  }, [selectedOnly])
  // Per repo, each template package name mapped to that repo's own entry
  // for it, or null when the repo doesn't publish it (or the lookup
  // failed — counted as not published rather than retried in a loop).
  // Filled for whichever repo is browsed, once per repo, so the review list
  // and its count below can say which template packages come from here.
  const [templateHits, setTemplateHits] = useState<Record<string, Record<string, PackageSearchResult | null>>>({})
  const templateNamesKey = baseLock.concreteNames.join(',')
  useEffect(() => {
    setTemplateHits({})
  }, [os, templateNamesKey])
  useEffect(() => {
    const repoId = selectedRepo?.id
    if (!repoId) return
    const known = templateHits[repoId] ?? {}
    const missing = baseLock.concreteNames.filter((name) => !(name in known))
    if (missing.length === 0) return
    let cancelled = false
    // One request for the whole list: one search per name queued the
    // browser's few connections per host and held up the catalog fetch
    // itself, for a template with a couple of hundred packages.
    api
      .lookupPackages({ os, repos: [repoId], names: missing })
      .catch(() => [] as PackageSearchResult[])
      .then((found) => {
        if (cancelled) return
        const byName = new Map(found.map((p) => [p.name, p]))
        setTemplateHits((prev) => {
          const next = { ...prev[repoId] }
          for (const name of missing) next[name] = byName.get(name) ?? null
          return { ...prev, [repoId]: next }
        })
      })
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- templateNamesKey stands in for baseLock.concreteNames
  }, [selectedRepo?.id, os, templateNamesKey, templateHits])

  const addedPackages = useStore((s) => s.addedPackages)
  const setPackage = useStore((s) => s.setPackage)
  const removePackage = useStore((s) => s.removePackage)
  const setPackages = useStore((s) => s.setPackages)
  const removePackages = useStore((s) => s.removePackages)
  const setRepoEnabled = useStore((s) => s.setRepoEnabled)

  const enabledRepoIds = repos.filter(isEnabled).map((r) => r.id)
  const {
    liveResolved,
    resolvingNames,
    resolveVersion,
    resolveAll: resolveAllVersions,
    bulkResolving,
    lookupNewest,
  } = resolver
  const [pinning, setPinning] = useState(false)

  // "Latest" ('') is written to the template unpinned and resolved only at
  // build time, so the image can differ from what was reviewed here. This
  // pins every added package, floating or already pinned and from any repo,
  // to the exact newest version the checked repos offer right now, so the
  // YAML records it. The repo a package was picked from stays enabled even if
  // the newest version lives elsewhere: releasing it would change the repo
  // scope the versions were just looked up in. Reads the selection again
  // after the lookups so a package removed meanwhile isn't added back.
  const pinAllToNewest = () => {
    setPinning(true)
    lookupNewest(addedPackages.map((p) => p.name))
      .then((newest) => {
        const pins = useStore
          .getState()
          .addedPackages.filter((p) => newest[p.name])
          .map((p) => ({ ...p, version: newest[p.name].version, repo: newest[p.name].repository }))
        if (pins.length > 0) setPackages(pins)
      })
      .finally(() => setPinning(false))
  }
  // What the pane is actually fetched against — only the one repository
  // currently picked in the dropdown, checked or not. Kept separate from
  // enabledRepoIds: resolveVersion and the "already added" rows below
  // intentionally stay scoped to what's checked regardless of which repo is
  // being browsed, since those answer "what does this resolve to given my
  // current repo choices," not "what's in the repo I happen to be looking
  // at."
  const fetchRepoIds = selectedRepo ? [selectedRepo.id] : []
  const fetchIdsKey = fetchRepoIds.join(',')
  const repoLabelFor = (repoId: string) => repos.find((r) => r.id === repoId)?.displayName ?? repoId
  const isBaseRepo = (repoId: string) => repos.find((r) => r.id === repoId)?.enabledByDefault ?? false
  const isProtectedRepo = (repoId: string) => isBaseRepo(repoId) || lockedRepoIds.has(repoId)
  // Adding a package (checkbox, a version chip, or bulk "Select all") from a
  // repo that isn't checked yet is a statement of interest in it, exactly
  // like picking a search-dropdown hit that isn't checked already enables
  // its repo — reachable whenever the browsed repo itself isn't checked.
  // Returns whether this call is what enabled it, so a caller about to
  // resolve a name against the newly-expanded scope knows to defer that
  // lookup instead (see pendingResolveRef below).
  const ensureRepoEnabled = (repoId: string): boolean => {
    if (enabledRepoIds.includes(repoId)) return false
    setRepoEnabled(repoId, true)
    return true
  }
  // Names (or, in the bulk "Select all" case, a batch of names) queued for an
  // automatic resolve that enabled their own repo in the same action —
  // `resolver` here still reflects the scope from before that enable (it's
  // rebuilt in PackagesStep.tsx only once the store update that follows
  // re-renders this tree), so resolving immediately would check against a
  // scope missing the repo just turned on. Drained once a fresh `resolver`
  // prop arrives from that re-render.
  const pendingResolveRef = useRef<string[]>([])

  useEffect(() => {
    if (pendingResolveRef.current.length === 0) return
    const names = pendingResolveRef.current
    pendingResolveRef.current = []
    resolveAllVersions(names)
  }, [resolver])

  // The page number only makes sense against whatever's currently fetched,
  // so switching which repo is browsed starts back on page 1 rather than
  // carrying a now-possibly-out-of-range page over.
  useEffect(() => {
    setPage(0)
  }, [fetchIdsKey])

  // The page-number input is free text while being edited (jumpToPage parses
  // it on Enter/blur), but should reflect the actual page whenever that
  // changes some other way (Prev/Next, or the reset above).
  useEffect(() => {
    setPageInput(String(page + 1))
  }, [page])

  // Fetch one page of the browsed repository's catalog (or its curated
  // subset) whenever the browsed repo, the curation toggle, or the page
  // changes. Skipped while reviewing selections only — that view is built
  // straight from the store below, no fetch needed.
  useEffect(() => {
    if (selectedOnly) {
      setLoading(false)
      return
    }
    setHits([])
    setError(null)
    if (fetchRepoIds.length === 0) {
      setTotal(0)
      return
    }
    setLoading(true)
    let cancelled = false
    api
      .searchPackages({ os, repos: fetchRepoIds, limit: PAGE_SIZE, offset: page * PAGE_SIZE, curated: frequentOnly })
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
    // eslint-disable-next-line react-hooks/exhaustive-deps -- fetchIdsKey stands in for fetchRepoIds
  }, [fetchIdsKey, os, frequentOnly, page, selectedOnly])

  const pageCount = Math.max(1, Math.ceil(total / PAGE_SIZE))

  // Already-picked packages from the browsed repo, synthesized straight from
  // the store rather than fetched — addedPackages only carries name/version/
  // repo, not description or sibling versions, which is fine for a short
  // review list. A floating pick (version === '') uses whatever was
  // live-resolved for it.
  const addedRows: PackageSearchResult[] = addedPackages
    .filter((p) => p.repo === selectedRepo?.id)
    .map((p) => {
      if (p.version === '' && liveResolved[p.name]) return liveResolved[p.name]
      return {
        name: p.name,
        version: p.version,
        repository: p.repo,
        versions: p.version ? [{ version: p.version, repository: p.repo }] : [],
      }
    })

  // The matched template's own packages never touch addedPackages, yet the
  // normal view shows each one checked in every repo that publishes it. So
  // the review list includes the template names the browsed repo publishes,
  // the same rule, and uses that repo's own row for each, exactly as the
  // normal view would. Previously only the base repo listed them, and it
  // listed every template name whether it published it or not, so e.g.
  // Intel EdgePack read "0 selected" while its template packages showed
  // checked. Template names are bare (no repo), so the only way to tell is
  // to look each one up in the browsed repo; see templateHits above.
  const browsedTemplateHits = selectedRepo ? templateHits[selectedRepo.id] : undefined
  const checkingTemplate =
    !!selectedRepo && baseLock.concreteNames.some((name) => !(name in (browsedTemplateHits ?? {})))
  const lockedRows: PackageSearchResult[] = baseLock.concreteNames
    .filter((name) => browsedTemplateHits?.[name] && !addedRows.some((r) => r.name === name))
    .map((name) => browsedTemplateHits![name]!)

  const selectedRows: PackageSearchResult[] = [...addedRows, ...lockedRows].sort((a, b) =>
    a.name.localeCompare(b.name),
  )

  const displayRows = selectedOnly ? selectedRows : hits

  // Resolve default-repo versions for any locked, unpinned row as it
  // renders, so its "current" chip can be marked once that lands. Skipped in
  // the review list: there, an unpinned row deliberately starts at "Latest"
  // rather than a default-repo guess — see resolveVersion above.
  useEffect(() => {
    if (selectedOnly) return
    ensureBaseVersions(baseLock, displayRows.map((h) => h.name))
  }, [displayRows, baseLock, selectedOnly])

  // Every row in the review list that's still just "Latest" — what the bulk
  // button resolves, and how it reports its own count/label. versions is
  // optional on the wire type (an older backend might omit it), so absent
  // counts as unresolved too, not a type error waiting to happen.
  const isUnresolved = (h: PackageSearchResult) => !h.versions?.length
  const unresolvedNames = selectedOnly
    ? selectedRows.filter((h) => isUnresolved(h) && !resolvingNames.has(h.name)).map((h) => h.name)
    : []
  const resolveAll = () => resolveAllVersions(unresolvedNames)

  const jumpToPage = () => {
    const n = parseInt(pageInput, 10)
    if (Number.isNaN(n)) {
      setPageInput(String(page + 1))
      return
    }
    setPage(Math.min(Math.max(n, 1), pageCount) - 1)
  }

  // Checked when every currently-loaded, unlocked package is already
  // selected — a locked row is already "satisfied" but isn't a real
  // addedPackages entry, so it's excluded from this and from bulk add/remove.
  const selectableHits = hits.filter((h) => !baseLock.info(h.name).locked)
  const allVisibleSelected =
    selectableHits.length > 0 && selectableHits.every((h) => addedPackages.some((p) => p.name === h.name))

  const onToggleSelectAll = (checked: boolean) => {
    if (checked) {
      // Skip rows already selected so a manually pinned version isn't reset
      // back to "latest".
      const toAdd = selectableHits
        .filter((h) => !addedPackages.some((p) => p.name === h.name))
        .map((h) => ({ name: h.name, version: '', repo: h.repository }))
      setPackages(toAdd)
      const newlyEnabledRepos = new Set<string>()
      new Set(toAdd.map((p) => p.repo)).forEach((repoId) => {
        if (ensureRepoEnabled(repoId)) newlyEnabledRepos.add(repoId)
      })
      const deferred = toAdd.filter((p) => newlyEnabledRepos.has(p.repo)).map((p) => p.name)
      const immediate = toAdd.filter((p) => !newlyEnabledRepos.has(p.repo)).map((p) => p.name)
      pendingResolveRef.current.push(...deferred)
      resolveAllVersions(immediate)
    } else {
      const names = selectableHits.map((h) => h.name)
      removePackages(names, {
        releaseRepo: confirmRepoRelease(addedPackages, repoLabelFor, names, isProtectedRepo),
        protectedRepos: lockedRepoIds,
      })
    }
  }

  if (fetchRepoIds.length === 0) {
    return (
      <p className="px-5 py-12 text-center text-[13px] text-slate-500">
        No repository selected — pick one above to browse its packages.
      </p>
    )
  }

  return (
    <>
      <div className="mb-2 flex flex-wrap items-center gap-x-4 gap-y-1.5 text-[12px] text-slate-600">
        {onFrequentOnlyChange && (
          <label className="flex items-center gap-1.5">
            <input
              type="checkbox"
              checked={frequentOnly}
              onChange={(e) => onFrequentOnlyChange(e.target.checked)}
              className="h-[13px] w-[13px] accent-[#0071c5]"
            />
            Show frequently used
          </label>
        )}
        <label className="flex items-center gap-1.5">
          <input
            type="checkbox"
            checked={selectedOnly}
            onChange={(e) => setSelectedOnly(e.target.checked)}
            className="h-[13px] w-[13px] accent-[#0071c5]"
          />
          Show only selected{' '}
          {/* How much of the browsed repo is already picked — otherwise only
              discoverable by checking this box, since picks are scattered
              across catalog pages. Always shown, even at zero, so a repo
              whose picks all live elsewhere — e.g. an Edge Pack template's
              base packages — reads as "none from here" rather than as a
              missing count. */}
          <span className="text-slate-500">
            ({checkingTemplate ? 'checking…' : `${selectedRows.length} selected`})
          </span>
        </label>
        {!selectedOnly && hits.length > 0 && (
          <label className="flex items-center gap-1.5 font-medium">
            <input
              type="checkbox"
              checked={allVisibleSelected}
              onChange={(e) => onToggleSelectAll(e.target.checked)}
              className="h-[13px] w-[13px] accent-[#0071c5]"
            />
            Select all on this page
          </label>
        )}
        {selectedOnly && unresolvedNames.length > 0 && (
          <button
            type="button"
            onClick={resolveAll}
            disabled={bulkResolving}
            className="font-medium text-[#0071c5] hover:underline disabled:cursor-wait disabled:text-slate-400 disabled:no-underline"
          >
            {bulkResolving
              ? 'Resolving versions — this may take a moment…'
              : `Resolve versions (${unresolvedNames.length})`}
          </button>
        )}
        {/* Acts on everything added, not just this repo or page, so it sits
            apart toward the end of the row, before the pager. Disabled rather than hidden until
            something is added, so it can be found before it's needed. */}
        <button
          type="button"
          onClick={pinAllToNewest}
          disabled={pinning || addedPackages.length === 0}
          title={
            addedPackages.length === 0
              ? 'Add packages first: this pins every package you are adding to its newest version'
              : 'Pin every package you are adding, from any repository, to the newest version in the checked repositories, so the template records that exact version instead of resolving it at build time'
          }
          className="ml-auto font-medium text-[#0071c5] hover:underline disabled:cursor-not-allowed disabled:text-slate-400 disabled:no-underline"
        >
          {pinning ? 'Updating…' : `Update to latest version (${addedPackages.length})`}
        </button>
        {/* Sits on the controls row rather than a line of its own, so the
            vertical space goes to package rows instead. */}
        {!selectedOnly && pageCount > 1 && displayRows.length > 0 && !error && (
          <Pager
            page={page}
            pageCount={pageCount}
            total={total}
            pageInput={pageInput}
            loading={loading}
            onPrev={() => setPage((p) => Math.max(0, p - 1))}
            onNext={() => setPage((p) => Math.min(pageCount - 1, p + 1))}
            onPageInputChange={setPageInput}
            onJump={jumpToPage}
            compact
          />
        )}
      </div>
      {error ? (
        <p className="px-5 py-12 text-center text-[13px] text-red-600">{error}</p>
      ) : displayRows.length === 0 ? (
        <p className="px-5 py-12 text-center text-[13px] text-slate-500">
          {selectedOnly
            ? `You haven't added any packages from ${repoLabelFor(selectedRepo?.id ?? '')} yet.`
            : loading
              ? 'Loading packages…'
              : frequentOnly
                ? `No frequently used packages in ${repoLabelFor(selectedRepo?.id ?? '')}. Uncheck "Show frequently used" to browse all ${fullTotal}.`
                : `No packages found in ${repoLabelFor(selectedRepo?.id ?? '')}.`}
        </p>
      ) : (
        // Only the list scrolls: the toolbar and controls above it stay put,
        // so switching repo or page never needs a scroll back up.
        <div className={`${LIST_MAX_H} overflow-y-auto`}>
          <div className="rounded border border-slate-100 bg-white">
            {displayRows.map((h) => {
              const lock = baseLock.info(h.name)
              // A pin resolves the same way regardless of which repos are
              // checked, so it never consults the live-resolver cache —
              // that cache is keyed only by name and can carry a stale
              // answer from an unrelated floating pick of the same name.
              // Only a floating entry's default-repo-only currentVersion is
              // worth replacing with the fresher, cross-repo live result —
              // never done in the review list, where an unpinned row
              // deliberately starts at "Latest" rather than a default-repo
              // guess (see the ensureBaseVersions effect above).
              const currentVersion =
                lock.currentIsFloating && !selectedOnly
                  ? (liveResolved[h.name]?.version ?? lock.currentVersion)
                  : lock.currentVersion
              return (
                <PackageRow
                  key={h.name}
                  name={h.name}
                  version={h.version}
                  description={h.description}
                  versions={h.versions?.length ? h.versions : [{ version: h.version, repository: h.repository }]}
                  repoLabelFor={repoLabelFor}
                  selection={addedPackages.find((p) => p.name === h.name)}
                  locked={lock.locked}
                  currentVersion={currentVersion}
                  currentIsFloating={lock.currentIsFloating}
                  resolvedVersion={liveResolved[h.name]?.version}
                  resolvedRepo={liveResolved[h.name]?.repository}
                  onToggle={(checked) => {
                    if (checked) {
                      setPackage({ name: h.name, version: '', repo: h.repository })
                      // Picked from whichever repo was being browsed, but
                      // what it actually resolves to can depend on every
                      // other checked repo too — look that up right away
                      // instead of leaving the pick as a bare "latest". If
                      // this pick just enabled its own repo, defer the
                      // lookup until `resolver` reflects that (see
                      // pendingResolveRef above) rather than checking it
                      // against a scope that's one render behind.
                      if (ensureRepoEnabled(h.repository)) {
                        pendingResolveRef.current.push(h.name)
                      } else {
                        resolveVersion(h.name)
                      }
                      return
                    }
                    removePackage(h.name, {
                      releaseRepo: confirmRepoRelease(addedPackages, repoLabelFor, [h.name], isProtectedRepo),
                      protectedRepos: lockedRepoIds,
                    })
                  }}
                  onChooseVersion={(v) => {
                    const previous = addedPackages.find((p) => p.name === h.name)
                    const opts =
                      previous && previous.repo !== v.repository
                        ? {
                            releaseRepo: confirmRepoRelease(addedPackages, repoLabelFor, [h.name], isProtectedRepo),
                            protectedRepos: lockedRepoIds,
                          }
                        : undefined
                    setPackage({ name: h.name, version: v.version, repo: v.repository }, opts)
                    const newlyEnabled = ensureRepoEnabled(v.repository)
                    if (v.version === '') {
                      if (newlyEnabled) {
                        pendingResolveRef.current.push(h.name)
                      } else {
                        resolveVersion(h.name)
                      }
                    }
                  }}
                  onResolve={selectedOnly && isUnresolved(h) ? () => resolveVersion(h.name) : undefined}
                  resolving={resolvingNames.has(h.name)}
                />
              )
            })}
          </div>
          {!selectedOnly && pageCount > 1 && (
            <Pager
              page={page}
              pageCount={pageCount}
              total={total}
              pageInput={pageInput}
              loading={loading}
              onPrev={() => setPage((p) => Math.max(0, p - 1))}
              onNext={() => setPage((p) => Math.min(pageCount - 1, p + 1))}
              onPageInputChange={setPageInput}
              onJump={jumpToPage}
              className="mt-2"
            />
          )}
        </div>
      )}
    </>
  )
}

// Pager is the Previous/page-jump/Next control, shown both above and below
// the list — with a page count in the hundreds or thousands (the base repo
// alone can carry tens of thousands of packages), scrolling all the way back
// up just to change page would defeat the point of having a jump-to-page box.
function Pager({
  page,
  pageCount,
  total,
  pageInput,
  loading,
  onPrev,
  onNext,
  onPageInputChange,
  onJump,
  compact = false,
  className = '',
}: {
  page: number
  pageCount: number
  total: number
  pageInput: string
  loading: boolean
  onPrev: () => void
  onNext: () => void
  onPageInputChange: (v: string) => void
  onJump: () => void
  // Drops the "Page" prefix and moves the package total into a tooltip, so
  // the copy on the list's controls row fits on that row instead of wrapping.
  compact?: boolean
  className?: string
}) {
  return (
    <div className={'flex items-center justify-center gap-2 text-[12px] text-slate-600 ' + className}>
      <button
        type="button"
        onClick={onPrev}
        disabled={loading || page === 0}
        className="rounded border border-slate-200 px-2 py-1 font-medium text-[#0071c5] hover:bg-[#eef4fb] disabled:opacity-50"
      >
        Previous
      </button>
      <span className="flex items-center gap-1" title={compact ? `${total.toLocaleString()} packages` : undefined}>
        {!compact && 'Page'}
        <input
          type="number"
          min={1}
          max={pageCount}
          value={pageInput}
          onChange={(e) => onPageInputChange(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') onJump()
          }}
          onBlur={onJump}
          className="w-14 rounded border border-slate-200 bg-white px-1 py-0.5 text-center"
          aria-label="Page number"
        />
        of {pageCount.toLocaleString()}
        {!compact && ` (${total.toLocaleString()} packages)`}
      </span>
      <button
        type="button"
        onClick={onNext}
        disabled={loading || page >= pageCount - 1}
        className="rounded border border-slate-200 px-2 py-1 font-medium text-[#0071c5] hover:bg-[#eef4fb] disabled:opacity-50"
      >
        Next
      </button>
    </div>
  )
}
