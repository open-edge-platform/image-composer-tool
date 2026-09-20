import { useEffect, useState } from 'react'
import { useStore, confirmRepoRelease } from '../store'
import { api } from '../api/client'
import type { PackageRepo, PackageSearchResult } from '../api/types'
import { PackageRow } from './PackageRow'
import { ensureBaseVersions, type BaseLock } from '../lib/baseLock'

// Packages fetched per page when browsing the merged catalog (no query).
const PAGE_SIZE = 100

interface PackageRepoBrowserProps {
  repos: PackageRepo[]
  os: string
  baseLock: BaseLock
}

// PackageRepoBrowser is the two-pane repository browser: a rail of checkboxes
// enabling/disabling repositories, and a pane listing the merged catalog of
// every currently-enabled repository. A package carried by more than one
// enabled repository appears once, showing every repository that publishes
// it — which repository a build actually resolves it from isn't something
// the picker predicts; picking a specific version pins that.
//
// A rail checkbox can be checked either because the user browsed it directly
// or because adding a package (from here or from search) enabled it on their
// behalf — the store doesn't distinguish the two. Every package mutation
// here therefore runs through confirmRepoRelease before it could uncheck a
// repo as a side effect, the same as the search dropdown does.
export function PackageRepoBrowser({ repos, os, baseLock }: PackageRepoBrowserProps) {
  const enabledRepos = useStore((s) => s.enabledRepos)
  const setRepoEnabled = useStore((s) => s.setRepoEnabled)

  // Base repos are always enabled and can't be turned off, so they belong at
  // the top of the rail rather than wherever their priority happens to land
  // them among the optional repos. Stable sort keeps the server's
  // priority-descending order within each group.
  const railRepos = [...repos].sort((a, b) => Number(b.enabledByDefault) - Number(a.enabledByDefault))

  // A repo the catalog marks enabledByDefault is the target's base repository:
  // every build reads it, so it cannot be turned off.
  const isEnabled = (r: PackageRepo) => r.enabledByDefault || enabledRepos.includes(r.id)

  return (
    <>
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
              onToggle={(on) => setRepoEnabled(r.id, on)}
            />
          ))}
        </div>
        <div
          role="region"
          aria-label="Repository packages"
          className="max-h-[480px] overflow-y-auto px-[18px] py-3.5"
        >
          <MergedPane repos={railRepos} isEnabled={isEnabled} os={os} baseLock={baseLock} />
        </div>
      </div>
      <p className="mt-2 text-sm text-slate-500">
        Check a repository to pull packages from it — the list on the right
        merges every checked repository&apos;s catalog, paged 100 at a time,
        showing which repositories publish each package. Where any checked
        repository offers curated picks, check &quot;Show frequently
        used&quot; to narrow the list to them, or check &quot;Show only
        selected&quot; to review what you&apos;ve already added. The
        &quot;Select all&quot; checkbox adds everything on the current page to
        your selection, and removes it again when unchecked.
      </p>
    </>
  )
}

function RepoRailRow({
  repo,
  enabled,
  onToggle,
}: {
  repo: PackageRepo
  enabled: boolean
  onToggle: (on: boolean) => void
}) {
  return (
    <div className="flex items-center gap-2.5 border-b border-slate-100 px-3 py-2.5 hover:bg-[#eef4fb]">
      <input
        type="checkbox"
        id={`repo-${repo.id}`}
        checked={enabled}
        disabled={repo.enabledByDefault}
        onChange={(e) => onToggle(e.target.checked)}
        className="h-[15px] w-[15px] shrink-0 accent-[#0071c5] disabled:cursor-not-allowed"
        title={repo.enabledByDefault ? 'Base repository — always enabled' : undefined}
      />
      <label htmlFor={`repo-${repo.id}`} className="min-w-0 flex-1 cursor-pointer">
        <span
          // The 240px rail truncates the longer Intel repo names, so keep the
          // full one reachable on hover.
          title={repo.displayName}
          className="block truncate text-[13px] font-semibold text-slate-700"
        >
          {repo.displayName}
        </span>
        {repo.enabledByDefault && (
          <span className="text-[11px] text-slate-500">Base — always on</span>
        )}
      </label>
    </div>
  )
}

function MergedPane({
  repos,
  isEnabled,
  os,
  baseLock,
}: {
  repos: PackageRepo[]
  isEnabled: (r: PackageRepo) => boolean
  os: string
  baseLock: BaseLock
}) {
  const [hits, setHits] = useState<PackageSearchResult[]>([])
  const [total, setTotal] = useState(0)
  // The full-catalog count, tracked separately from `total` so the empty
  // state under "Show frequently used" can say how many packages are being
  // hidden rather than just that none are curated.
  const [fullTotal, setFullTotal] = useState(0)
  const [frequentOnly, setFrequentOnly] = useState(false)
  // When true, the network fetch below is skipped entirely and the list
  // shows addedPackages instead — reviewing what's already picked shouldn't
  // depend on which page of an 86,000-package catalog it happens to land on.
  const [selectedOnly, setSelectedOnly] = useState(false)
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

  const enabledRepoIds = repos.filter(isEnabled).map((r) => r.id)
  const enabledIdsKey = enabledRepoIds.join(',')
  const hasCuratedPackages = repos.some((r) => isEnabled(r) && r.hasCuratedPackages)
  const repoLabelFor = (repoId: string) => repos.find((r) => r.id === repoId)?.displayName ?? repoId
  const isBaseRepo = (repoId: string) => repos.find((r) => r.id === repoId)?.enabledByDefault ?? false

  // The curation toggle and the page number both only make sense against
  // whatever's currently checked, so a change to the checked set starts back
  // on page 1 of the full merged catalog rather than carrying a
  // now-possibly-out-of-range page or curated filter over.
  useEffect(() => {
    setFrequentOnly(false)
    setPage(0)
  }, [enabledIdsKey])

  // The page-number input is free text while being edited (jumpToPage parses
  // it on Enter/blur), but should reflect the actual page whenever that
  // changes some other way (Prev/Next, or the reset above).
  useEffect(() => {
    setPageInput(String(page + 1))
  }, [page])

  // Fetch one page of the merged catalog (or its curated subset) whenever the
  // checked repository set, the curation toggle, or the page changes. Skipped
  // while reviewing selections only — that view is built straight from the
  // store below, no fetch needed.
  useEffect(() => {
    if (selectedOnly) {
      setLoading(false)
      return
    }
    setHits([])
    setError(null)
    if (enabledRepoIds.length === 0) {
      setTotal(0)
      return
    }
    setLoading(true)
    let cancelled = false
    api
      .searchPackages({ os, repos: enabledRepoIds, limit: PAGE_SIZE, offset: page * PAGE_SIZE, curated: frequentOnly })
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
    // eslint-disable-next-line react-hooks/exhaustive-deps -- enabledIdsKey stands in for enabledRepoIds
  }, [enabledIdsKey, os, frequentOnly, page, selectedOnly])

  const pageCount = Math.max(1, Math.ceil(total / PAGE_SIZE))

  // Already-picked packages relevant to the checked repos, synthesized
  // straight from the store rather than fetched — addedPackages only carries
  // name/version/repo, not description or sibling versions, which is fine
  // for a short review list.
  const selectedRows: PackageSearchResult[] = addedPackages
    .filter((p) => enabledRepoIds.includes(p.repo))
    .map((p) => ({
      name: p.name,
      version: p.version,
      repository: p.repo,
      versions: p.version ? [{ version: p.version, repository: p.repo }] : [],
    }))
    .sort((a, b) => a.name.localeCompare(b.name))

  const displayRows = selectedOnly ? selectedRows : hits

  // Resolve default-repo versions for any locked, unpinned row as it
  // renders, so its "current" chip can be marked once that lands.
  useEffect(() => {
    ensureBaseVersions(baseLock, displayRows.map((h) => h.name))
  }, [displayRows, baseLock])

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
    } else {
      const names = selectableHits.map((h) => h.name)
      removePackages(names, { releaseRepo: confirmRepoRelease(addedPackages, repoLabelFor, names, isBaseRepo) })
    }
  }

  if (enabledRepoIds.length === 0) {
    return (
      <p className="px-5 py-12 text-center text-[13px] text-slate-500">
        No repositories are checked — check one on the left to browse its
        packages.
      </p>
    )
  }

  return (
    <>
      <div className="mb-2 flex flex-wrap items-center gap-4 text-[12px] text-slate-600">
        {hasCuratedPackages && !selectedOnly && (
          <label className="flex items-center gap-1.5">
            <input
              type="checkbox"
              checked={frequentOnly}
              onChange={(e) => setFrequentOnly(e.target.checked)}
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
          Show only selected
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
      </div>
      {error ? (
        <p className="px-5 py-12 text-center text-[13px] text-red-600">{error}</p>
      ) : displayRows.length === 0 ? (
        <p className="px-5 py-12 text-center text-[13px] text-slate-500">
          {selectedOnly
            ? "You haven't added any packages from the checked repositories yet."
            : loading
              ? 'Loading packages…'
              : frequentOnly
                ? `No frequently used packages in the checked repositories. Uncheck "Show frequently used" to browse all ${fullTotal}.`
                : 'No packages found in the checked repositories.'}
        </p>
      ) : (
        <>
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
              className="mb-2"
            />
          )}
          <div className="rounded border border-slate-100">
            {displayRows.map((h) => {
              const lock = baseLock.info(h.name)
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
                  currentVersion={lock.currentVersion}
                  currentIsFloating={lock.currentIsFloating}
                  onToggle={(checked) => {
                    if (checked) {
                      setPackage({ name: h.name, version: '', repo: h.repository })
                      return
                    }
                    removePackage(h.name, { releaseRepo: confirmRepoRelease(addedPackages, repoLabelFor, [h.name], isBaseRepo) })
                  }}
                  onChooseVersion={(v) => {
                    const previous = addedPackages.find((p) => p.name === h.name)
                    const opts =
                      previous && previous.repo !== v.repository
                        ? { releaseRepo: confirmRepoRelease(addedPackages, repoLabelFor, [h.name], isBaseRepo) }
                        : undefined
                    setPackage({ name: h.name, version: v.version, repo: v.repository }, opts)
                  }}
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
