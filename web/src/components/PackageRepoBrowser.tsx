import { useEffect, useRef, useState } from 'react'
import { useStore, confirmRepoRelease } from '../store'
import { api } from '../api/client'
import type { PackageRepo, PackageSearchResult } from '../api/types'
import { PackageRow } from './PackageRow'
import { ensureBaseVersions, type BaseLock } from '../lib/baseLock'
import type { VersionResolver } from '../lib/useVersionResolver'

// Packages fetched per page when browsing the merged catalog (no query).
const PAGE_SIZE = 100

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

// PackageRepoBrowser is the two-pane repository browser: a rail of
// repositories and a pane listing the catalog of whichever one is currently
// clicked. A repository's checkbox is a separate action from clicking it —
// the checkbox controls whether a build resolves packages from it at all,
// while clicking (anywhere on the row but the checkbox) controls which
// repository's catalog is shown on the right. Keeping these apart is what
// lets a repo with tens of thousands of packages (Ubuntu's base repo, say)
// be checked for the build without flooding the browse list the moment it
// is.
//
// A rail checkbox can be checked either because the user checked it directly
// or because adding a package (from here or from search) enabled it on their
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
  // turned off, so either belongs at the top of the rail rather than
  // wherever its priority happens to land it among the optional repos.
  // Stable sort keeps the server's priority-descending order within each
  // group.
  const alwaysOn = (r: PackageRepo) => r.enabledByDefault || lockedRepoIds.has(r.id)
  const railRepos = [...repos].sort((a, b) => Number(alwaysOn(b)) - Number(alwaysOn(a)))

  // A repo the catalog marks enabledByDefault is the target's base repository:
  // every build reads it, so it cannot be turned off. A repo in
  // lockedRepoIds is the same situation for a different reason: the matched
  // template already draws a package from it.
  const isEnabled = (r: PackageRepo) => alwaysOn(r) || enabledRepos.includes(r.id)

  // Which repository's catalog is shown on the right — independent of which
  // repos are checked. Defaults to the top-ranked rail repo (base or
  // template-locked, if either exists) so there's always something to look
  // at, and re-picks a default if the current selection disappears (e.g. the
  // target OS changes) without overriding a user's own click.
  const [selectedRepoId, setSelectedRepoId] = useState<string | undefined>(undefined)
  const railRepoIdsKey = railRepos.map((r) => r.id).join(',')
  useEffect(() => {
    if (railRepos.length === 0) return
    if (selectedRepoId && railRepos.some((r) => r.id === selectedRepoId)) return
    setSelectedRepoId(railRepos[0].id)
    // eslint-disable-next-line react-hooks/exhaustive-deps -- railRepoIdsKey stands in for railRepos
  }, [railRepoIdsKey, selectedRepoId])
  const selectedRepo = railRepos.find((r) => r.id === selectedRepoId)

  // The curated-subset toggle only makes sense against whichever repo is
  // currently browsed, so switching repos resets it rather than carrying a
  // filter over that might not even apply to the new one.
  const [frequentOnly, setFrequentOnly] = useState(false)
  useEffect(() => {
    setFrequentOnly(false)
  }, [selectedRepoId])

  return (
    <>
      <p className="mb-2 text-[11px] font-medium text-amber-700">
        Only checked repositories are used when resolving packages at build
        time — checking or unchecking one can change what an unpinned pick
        resolves to.
      </p>
      {selectedRepo?.hasCuratedPackages && (
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
      <div className="grid grid-cols-[240px_minmax(0,1fr)] overflow-hidden rounded-lg border border-slate-200">
        <div
          role="group"
          aria-label="Package repositories"
          className="max-h-[480px] overflow-y-auto border-r border-slate-200 bg-slate-50"
        >
          {railRepos.map((r) => (
            <RepoRailRow
              key={r.id}
              repo={r}
              enabled={isEnabled(r)}
              locked={lockedRepoIds.has(r.id)}
              selected={r.id === selectedRepo?.id}
              onToggle={(on) => setRepoEnabled(r.id, on)}
              onSelect={() => setSelectedRepoId(r.id)}
            />
          ))}
        </div>
        <div
          role="region"
          aria-label="Repository packages"
          className="max-h-[480px] overflow-y-auto bg-[#d6ecf8] px-[18px] py-3.5"
        >
          <MergedPane
            repos={repos}
            isEnabled={isEnabled}
            selectedRepo={selectedRepo}
            os={os}
            baseLock={baseLock}
            resolver={resolver}
            lockedRepoIds={lockedRepoIds}
            frequentOnly={frequentOnly}
          />
        </div>
      </div>
      <p className="mt-2 text-sm text-slate-500">
        Click a repository on the left to browse its catalog, paged 100 at a
        time — picking a package from one checks it for you if it
        isn&apos;t checked already, the same as the search box above already
        does. Checking a repository&apos;s own box controls whether a build
        resolves packages from it at all; it doesn&apos;t change which one
        you&apos;re browsing. A package already selected from a different
        repository still shows as checked here, with a note naming the
        version and repository it&apos;s actually pinned to, since picking
        the one shown on this page would be a different pin, not the same
        package twice. Where the repository you&apos;re browsing offers
        curated picks, check &quot;Show frequently used&quot; above to
        narrow the list to them. What the matched template ships on its own,
        and everything you&apos;ve added across every checked repository,
        shows in the Selected panel on the right instead. Picking
        &quot;Latest&quot; looks up what it actually resolves to across
        every checked repository right away — not just the one being
        browsed — and shows a note naming the real version and repository
        once that lookup lands. The &quot;Select all&quot; checkbox adds
        everything on the current page to your selection, and removes it
        again when unchecked.
      </p>
    </>
  )
}

function RepoRailRow({
  repo,
  enabled,
  locked,
  selected,
  onToggle,
  onSelect,
}: {
  repo: PackageRepo
  enabled: boolean
  locked: boolean
  selected: boolean
  onToggle: (on: boolean) => void
  onSelect: () => void
}) {
  const badge = repo.enabledByDefault ? 'Base — always on' : locked ? 'In template — always on' : null
  const title = repo.enabledByDefault
    ? 'Base repository — always enabled'
    : locked
      ? "Already in template — can't be unchecked"
      : undefined
  return (
    <div
      role="button"
      tabIndex={0}
      aria-pressed={selected}
      onClick={onSelect}
      onKeyDown={(e) => {
        // Guards against the nested checkbox: its own Space keydown bubbles
        // up here too, and without this check preventDefault() would cancel
        // the checkbox's native toggle and select the repository instead —
        // keyboard users could never enable or disable one. Only handle the
        // row div's own key, not ones from something inside it.
        if (e.target !== e.currentTarget) return
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault()
          onSelect()
        }
      }}
      className={
        'flex cursor-pointer items-center gap-2.5 border-b border-slate-100 px-3 py-2.5 ' +
        (selected ? 'bg-[#d6ecf8] font-semibold' : 'hover:bg-[#eef4fb]')
      }
    >
      <input
        type="checkbox"
        id={`repo-${repo.id}`}
        aria-label={repo.displayName}
        checked={enabled}
        disabled={repo.enabledByDefault || locked}
        onClick={(e) => e.stopPropagation()}
        onChange={(e) => onToggle(e.target.checked)}
        className="h-[15px] w-[15px] shrink-0 accent-[#0071c5] disabled:cursor-not-allowed"
        title={title}
      />
      <span className="min-w-0 flex-1">
        <span
          // The 240px rail truncates the longer Intel repo names, so keep the
          // full one reachable on hover.
          title={repo.displayName}
          className="block truncate text-[13px] font-semibold text-slate-700"
        >
          {repo.displayName}
        </span>
        {badge && <span className="text-[11px] text-slate-500">{badge}</span>}
      </span>
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
}: {
  repos: PackageRepo[]
  isEnabled: (r: PackageRepo) => boolean
  selectedRepo: PackageRepo | undefined
  os: string
  baseLock: BaseLock
  resolver: VersionResolver
  lockedRepoIds: Set<string>
  frequentOnly: boolean
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
  const addedPackages = useStore((s) => s.addedPackages)
  const setPackage = useStore((s) => s.setPackage)
  const removePackage = useStore((s) => s.removePackage)
  const setPackages = useStore((s) => s.setPackages)
  const removePackages = useStore((s) => s.removePackages)
  const setRepoEnabled = useStore((s) => s.setRepoEnabled)

  const enabledRepoIds = repos.filter(isEnabled).map((r) => r.id)
  const { liveResolved, resolvingNames, resolveVersion, resolveAll: resolveAllVersions } = resolver
  // What the pane is actually fetched against — only the one repository
  // currently clicked in the rail, checked or not. Kept separate from
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
  // changes.
  useEffect(() => {
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
  }, [fetchIdsKey, os, frequentOnly, page])

  const pageCount = Math.max(1, Math.ceil(total / PAGE_SIZE))

  // Resolve default-repo versions for any locked, unpinned row as it
  // renders, so its "current" chip can be marked once that lands.
  useEffect(() => {
    ensureBaseVersions(baseLock, hits.map((h) => h.name))
  }, [hits, baseLock])

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
        No repository selected — click one on the left to browse its
        packages.
      </p>
    )
  }

  return (
    <>
      <div className="mb-2 flex flex-wrap items-center gap-4 text-[12px] text-slate-600">
        {hits.length > 0 && (
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
      </div>
      {error ? (
        <p className="px-5 py-12 text-center text-[13px] text-red-600">{error}</p>
      ) : hits.length === 0 ? (
        <p className="px-5 py-12 text-center text-[13px] text-slate-500">
          {loading
            ? 'Loading packages…'
            : frequentOnly
              ? `No frequently used packages in ${repoLabelFor(selectedRepo?.id ?? '')}. Uncheck "Show frequently used" to browse all ${fullTotal}.`
              : `No packages found in ${repoLabelFor(selectedRepo?.id ?? '')}.`}
        </p>
      ) : (
        <>
          {pageCount > 1 && (
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
              className="mb-2"
            />
          )}
          <div className="rounded border border-slate-100">
            {hits.map((h) => {
              const lock = baseLock.info(h.name)
              // A pin resolves the same way regardless of which repos are
              // checked, so it never consults the live-resolver cache —
              // that cache is keyed only by name and can carry a stale
              // answer from an unrelated floating pick of the same name.
              // Only a floating entry's default-repo-only currentVersion is
              // worth replacing with the fresher, cross-repo live result.
              const currentVersion = lock.currentIsFloating
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
                  resolving={resolvingNames.has(h.name)}
                />
              )
            })}
          </div>
          {pageCount > 1 && (
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
        </>
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
      <span className="flex items-center gap-1">
        Page
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
          className="w-14 rounded border border-slate-200 px-1 py-0.5 text-center"
          aria-label="Page number"
        />
        of {pageCount} ({total} packages)
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
