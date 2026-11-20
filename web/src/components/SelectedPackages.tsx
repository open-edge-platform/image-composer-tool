import { useEffect } from 'react'
import { useStore, confirmRepoRelease, type AddedPackage } from '../store'
import type { PackageRepo } from '../api/types'
import type { VersionResolver } from '../lib/useVersionResolver'

interface SelectedPackagesProps {
  repos: PackageRepo[]
  // No longer read here — version resolution now goes through the shared
  // `resolver` prop, which already carries an os-scoped cache from
  // PackagesStep.tsx. Kept optional so existing call sites (including tests)
  // that still pass it don't need updating just for this.
  os?: string
  // Shared with the Repositories tab and the search dropdown (lifted in
  // PackagesStep.tsx), so a floating pick's "what does this resolve to"
  // answer is identical everywhere it's shown. Optional only for test setups
  // that don't exercise resolution at all.
  resolver?: VersionResolver
  lockedRepoIds?: Set<string>
}

const noResolver: VersionResolver = {
  liveResolved: {},
  resolvingNames: new Set(),
  resolveVersion: () => Promise.resolve(),
  resolveAll: () => {},
  bulkResolving: false,
  attemptedNames: new Set(),
  lookupNewest: () => Promise.resolve({ found: {}, complete: true, unavailableRepos: [], stale: false }),
}

const noLockedRepos = new Set<string>()

// SelectedPackages is the right rail: everything the user has added so far,
// grouped by the repository it came from, with a per-item remove and a
// clear-all. It lists only what this tab adds or changes, not the matched
// template's own packages, which the Repositories tab shows checked where
// they're published. Stickiness is handled by the caller (PackagesStep.tsx).
export function SelectedPackages({
  repos,
  resolver = noResolver,
  lockedRepoIds = noLockedRepos,
}: SelectedPackagesProps) {
  const addedPackages = useStore((s) => s.addedPackages)
  const removePackage = useStore((s) => s.removePackage)
  const clearPackages = useStore((s) => s.clearPackages)

  const labelFor = (repoId: string) => repos.find((r) => r.id === repoId)?.displayName ?? repoId
  const isBaseRepo = (repoId: string) => repos.find((r) => r.id === repoId)?.enabledByDefault ?? false
  const isProtectedRepo = (repoId: string) => isBaseRepo(repoId) || lockedRepoIds.has(repoId)

  const { liveResolved, resolvingNames, resolveVersion, resolveAll, attemptedNames } = resolver
  // An explicit floating pick (version === '') carries the repo it was
  // picked from, which is only a fallback for display until the live lookup
  // lands with the repo it actually resolves from.
  const floatingAdded = addedPackages.filter((p) => p.version === '')
  const unresolvedFloatingNames = floatingAdded
    .filter((p) => !liveResolved[p.name] && !resolvingNames.has(p.name))
    .map((p) => p.name)

  // Auto-resolve the names the user picked as "Latest" — each one came from a
  // single click (or MergedPane's/search's own already-explicit "Latest"
  // pick), so the batch here is bounded by how many the user has added.
  const toResolveKey = [...unresolvedFloatingNames].sort().join(',')
  useEffect(() => {
    if (!toResolveKey) return
    // Excludes a name a prior automatic attempt already failed on — without
    // this, a failed name cycles back into toResolveKey on every render
    // (unresolved again once resolvingNames clears it), retrying forever. A
    // manual Resolve click still reaches it: resolveVersion's own guard only
    // checks liveResolved/resolvingNames, not attemptedNames.
    const names = toResolveKey.split(',').filter((name) => !attemptedNames.has(name))
    if (names.length > 0) resolveAll(names)
    // eslint-disable-next-line react-hooks/exhaustive-deps -- toResolveKey stands in for the name list
  }, [toResolveKey])

  // A pinned pick's repo is exact — it's part of the pin itself. A floating
  // pick's stored repo is only ever where it happened to be picked from, so
  // once the shared resolver knows where it actually resolves from, group it
  // there instead.
  const groups = new Map<string, AddedPackage[]>()
  for (const p of addedPackages) {
    const repoId = p.version === '' ? (liveResolved[p.name]?.repository ?? p.repo) : p.repo
    const list = groups.get(repoId) ?? []
    list.push(p)
    groups.set(repoId, list)
  }
  const repoIds = [...groups.keys()].sort((a, b) => labelFor(a).localeCompare(labelFor(b)))

  const total = addedPackages.length

  return (
    <div className="rounded-lg border border-slate-200 bg-white">
      <div className="flex items-center justify-between border-b border-slate-100 px-3 py-2.5">
        <span className="text-[13px] font-bold text-[#00285a]">
          Selected ({total})
        </span>
        {addedPackages.length > 0 && (
          <button
            type="button"
            onClick={() => clearPackages(lockedRepoIds)}
            className="text-[12px] font-medium text-[#0071c5] hover:underline"
          >
            Clear
          </button>
        )}
      </div>
      {/* The list's cap tracks the viewport so the rail, which starts level
          with the search box, still ends above the Back/Next row (25rem is the
          chrome above and below it); below the floor the page scrolls instead. */}
      {total === 0 ? (
        <p className="px-3 py-8 text-center text-[12px] text-slate-500">No packages added yet.</p>
      ) : (
        <div className="max-h-[clamp(240px,calc(100vh-25rem),520px)] overflow-y-auto">
          {addedPackages.length > 0 && (
            <div className="border-b border-slate-100 bg-[#f4f9fd] py-1.5 last:border-b-0">
              <div className="px-3 pb-1 text-[10px] font-bold uppercase tracking-[0.5px] text-[#0071c5]">
                You are adding ({addedPackages.length})
              </div>
              {repoIds.map((repoId) => (
                <div key={repoId}>
                  <div className="px-3 pb-1 pt-1 text-[10px] font-bold uppercase tracking-[0.5px] text-[#0071c5]">
                    {labelFor(repoId)}
                  </div>
                  {groups.get(repoId)!.map((p) => (
                    <div key={p.name} className="flex items-center justify-between gap-2 px-3 py-1">
                      <span
                        className="min-w-0 flex-1 truncate text-[12px] text-[#0071c5]"
                        title={p.name}
                      >
                        {p.name}
                      </span>
                      {p.version === '' ? (
                        liveResolved[p.name] ? (
                          <VersionBadge
                            version={liveResolved[p.name].version}
                            floating
                            title={`currently resolves to ${liveResolved[p.name].version} from ${labelFor(liveResolved[p.name].repository)} (unpinned)`}
                          />
                        ) : (
                          <button
                            type="button"
                            onClick={() => resolveVersion(p.name)}
                            disabled={resolvingNames.has(p.name)}
                            className="shrink-0 text-[11px] font-medium text-[#0071c5] hover:underline disabled:cursor-wait disabled:text-slate-400 disabled:no-underline"
                          >
                            {resolvingNames.has(p.name) ? 'Resolving…' : 'Resolve'}
                          </button>
                        )
                      ) : (
                        <VersionBadge version={p.version} floating={false} title={`pinned to ${p.version} from ${labelFor(p.repo)}`} />
                      )}
                      <button
                        type="button"
                        onClick={() =>
                          removePackage(p.name, {
                            releaseRepo: confirmRepoRelease(addedPackages, labelFor, [p.name], isProtectedRepo),
                            protectedRepos: lockedRepoIds,
                          })
                        }
                        aria-label={`Remove ${p.name}`}
                        className="shrink-0 text-slate-400 hover:text-red-600"
                      >
                        ×
                      </button>
                    </div>
                  ))}
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

// A pinned version and a "Latest" pick that currently happens to resolve to
// the same-looking version string are easy to mistake for each other once
// both render as a plain badge — the "latest" tag and dashed outline here are
// the only visual difference, since the pin itself is otherwise identical: a
// mono version number in a pill.
function VersionBadge({ version, floating, title }: { version: string; floating: boolean; title: string }) {
  return (
    <span className="flex shrink-0 items-center gap-1">
      {floating && <span className="text-[9px] font-semibold uppercase tracking-wide text-slate-400">unpinned</span>}
      <span
        className={
          'rounded-full px-1.5 py-0.5 font-mono text-[10px] font-medium ' +
          (floating ? 'border border-dashed border-[#0071c5] text-[#0071c5]' : 'bg-[#e6f2fa] text-[#0071c5]')
        }
        title={title}
      >
        {version}
      </span>
    </span>
  )
}
