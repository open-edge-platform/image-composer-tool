import { useEffect } from 'react'
import { useStore, confirmRepoRelease, type AddedPackage } from '../store'
import type { PackageRepo } from '../api/types'
import type { BaseLock } from '../lib/baseLock'
import type { VersionResolver } from '../lib/useVersionResolver'

interface SelectedPackagesProps {
  repos: PackageRepo[]
  // No longer read here — version resolution now goes through the shared
  // `resolver` prop, which already carries an os-scoped cache from
  // PackagesStep.tsx. Kept optional so existing call sites (including tests)
  // that still pass it don't need updating just for this.
  os?: string
  baseLock?: BaseLock
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
}

const noLockedRepos = new Set<string>()

// Callers that have no matched template (or are pre-baseLock test setups)
// can omit baseLock entirely — the "Already in template" section simply
// has nothing to show.
const noBaseLock: BaseLock = {
  info: () => ({ locked: false, currentIsFloating: false }),
  ensureResolved: () => {},
  concreteNames: [],
}

// SelectedPackages is the right rail: everything the user has added so far,
// grouped by the repository it came from, with a per-item remove and a
// clear-all. Stickiness is handled by the caller (PackagesStep.tsx).
export function SelectedPackages({
  repos,
  baseLock = noBaseLock,
  resolver = noResolver,
  lockedRepoIds = noLockedRepos,
}: SelectedPackagesProps) {
  const addedPackages = useStore((s) => s.addedPackages)
  const removePackage = useStore((s) => s.removePackage)
  const clearPackages = useStore((s) => s.clearPackages)
  const importedTemplate = useStore((s) => s.importedTemplate)
  const removedBasePackages = useStore((s) => s.removedBasePackages)
  const setBasePackageRemoved = useStore((s) => s.setBasePackageRemoved)

  const labelFor = (repoId: string) => repos.find((r) => r.id === repoId)?.displayName ?? repoId
  const isBaseRepo = (repoId: string) => repos.find((r) => r.id === repoId)?.enabledByDefault ?? false
  const isProtectedRepo = (repoId: string) => isBaseRepo(repoId) || lockedRepoIds.has(repoId)

  // Filtered here rather than read back off the next compose, so a removal
  // leaves the list immediately instead of after a server round trip.
  const imported = (importedTemplate?.packages ?? []).filter(
    (p) => !removedBasePackages.includes(p),
  )

  // What the matched template ships on its own, not already covered by an
  // explicit pick above — shown here by default rather than requiring the
  // Repositories tab's "Show only selected" to be checked, so "what's in
  // this build" is visible without digging for it. Not removable: the
  // template merge is additive, so unchecking one here would be a no-op —
  // picking a different version for it (from the browser or search) is the
  // real override, and moves it into the added-packages groups above once
  // that happens.
  const addedNames = new Set(addedPackages.map((p) => p.name))
  const baseOnlyNames = baseLock.concreteNames.filter((name) => !addedNames.has(name)).sort()

  const { liveResolved, resolvingNames, resolveVersion, resolveAll, bulkResolving } = resolver
  const baseVersionOf = (name: string) => liveResolved[name]?.version ?? baseLock.info(name).currentVersion
  const baseIsUnresolved = (name: string) => !liveResolved[name] && baseVersionOf(name) === undefined
  const unresolvedBaseNames = baseOnlyNames.filter((name) => baseIsUnresolved(name) && !resolvingNames.has(name))

  // An explicit floating pick (version === '') is exactly the same kind of
  // unresolved name as a template-shipped one above — it just also carries
  // the repo it was picked from, which is only a fallback for display until
  // the live lookup lands with the repo it actually resolves from.
  const floatingAdded = addedPackages.filter((p) => p.version === '')
  const unresolvedFloatingNames = floatingAdded
    .filter((p) => !liveResolved[p.name] && !resolvingNames.has(p.name))
    .map((p) => p.name)

  // Auto-resolve only names the user explicitly just picked — each one came
  // from a single click (or MergedPane's/search's own already-explicit
  // "Latest" pick), so the batch here is bounded by how many the user has
  // actually added. baseOnlyNames below is a template's *entire* unpinned
  // package list, which can run into the hundreds — auto-firing a lookup for
  // all of them on every mount would flood a slow network with requests
  // nobody asked for, so that list stays on the manual "Resolve (N)" button.
  const toResolveKey = [...unresolvedFloatingNames].sort().join(',')
  useEffect(() => {
    if (toResolveKey) resolveAll(toResolveKey.split(','))
    // eslint-disable-next-line react-hooks/exhaustive-deps -- toResolveKey stands in for the name list
  }, [toResolveKey])

  // Grouped the same way the added-packages section above is: by the
  // repository the name actually resolves from. A live search hit (from
  // resolving a floating entry) names its own repo; a pinned or
  // not-yet-resolved entry's currentVersion was always checked against the
  // default repo (see useBaseLock), so that's the only repo a resolved-but-
  // not-live-searched name can be attributed to. Still-unresolved names have
  // no repo to show yet and fall into a trailing, ungrouped bucket.
  const defaultRepoId = repos.find((r) => r.enabledByDefault)?.id
  const repoOf = (name: string): string | undefined =>
    liveResolved[name]?.repository ?? (baseVersionOf(name) !== undefined ? defaultRepoId : undefined)
  const baseGroups = new Map<string, string[]>()
  const baseUngrouped: string[] = []
  for (const name of baseOnlyNames) {
    const repoId = repoOf(name)
    if (!repoId) {
      baseUngrouped.push(name)
      continue
    }
    const list = baseGroups.get(repoId) ?? []
    list.push(name)
    baseGroups.set(repoId, list)
  }
  const baseRepoIds = [...baseGroups.keys()].sort((a, b) => labelFor(a).localeCompare(labelFor(b)))

  // A pinned pick's repo is exact — it's part of the pin itself. A floating
  // pick's stored repo is only ever where it happened to be picked from, so
  // once the shared resolver knows where it actually resolves from, group it
  // there instead — the same correction baseGroups already applies to
  // template-shipped floating names above.
  const groups = new Map<string, AddedPackage[]>()
  for (const p of addedPackages) {
    const repoId = p.version === '' ? (liveResolved[p.name]?.repository ?? p.repo) : p.repo
    const list = groups.get(repoId) ?? []
    list.push(p)
    groups.set(repoId, list)
  }
  const repoIds = [...groups.keys()].sort((a, b) => labelFor(a).localeCompare(labelFor(b)))

  const total = imported.length + addedPackages.length + baseOnlyNames.length

  return (
    <div className="rounded-lg border border-slate-200 bg-white">
      <div className="flex items-center justify-between border-b border-slate-100 px-3 py-2.5">
        <span className="text-[13px] font-bold text-[#00285a]">Selected ({total})</span>
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
      {total === 0 ? (
        <p className="px-3 py-8 text-center text-[12px] text-slate-500">No packages added yet.</p>
      ) : (
        <div className="max-h-[520px] overflow-y-auto">
          {/* After the user's own picks, before the imported-template list:
              a newly picked package should land in view above this, and the
              imported template's own list is the long, mostly-static one
              that belongs last. Each section gets its own tint — blue for
              what the user is actively changing, neutral grey for what's
              already there — so the two are distinguishable at a glance,
              not just by their header text. */}
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
          {baseOnlyNames.length > 0 && (
            <div className="border-b border-slate-100 bg-slate-50 py-1.5 last:border-b-0">
              <div className="flex items-center justify-between gap-2 px-3 pb-1">
                <span className="text-[10px] font-bold uppercase tracking-[0.5px] text-slate-500">
                  Already in template
                </span>
                {unresolvedBaseNames.length > 0 && (
                  <button
                    type="button"
                    onClick={() => resolveAll(unresolvedBaseNames)}
                    disabled={bulkResolving}
                    className="text-[11px] font-medium text-[#0071c5] hover:underline disabled:cursor-wait disabled:text-slate-400 disabled:no-underline"
                  >
                    {bulkResolving ? 'Resolving…' : `Resolve (${unresolvedBaseNames.length})`}
                  </button>
                )}
              </div>
              {baseRepoIds.map((repoId) => (
                <div key={repoId}>
                  <div className="px-3 pb-1 pt-1 text-[10px] font-bold uppercase tracking-[0.5px] text-slate-400">
                    {labelFor(repoId)}
                  </div>
                  {baseGroups.get(repoId)!.map((name) => (
                    <BaseOnlyRow
                      key={name}
                      name={name}
                      version={baseVersionOf(name)}
                      floating={baseLock.info(name).currentIsFloating}
                      unresolved={baseIsUnresolved(name)}
                      resolving={resolvingNames.has(name)}
                      onResolve={() => resolveVersion(name)}
                    />
                  ))}
                </div>
              ))}
              {baseUngrouped.length > 0 && (
                <div>
                  <div className="px-3 pb-1 pt-1 text-[10px] font-bold uppercase tracking-[0.5px] text-slate-400">
                    Unresolved
                  </div>
                  {baseUngrouped.map((name) => (
                    <BaseOnlyRow
                      key={name}
                      name={name}
                      version={baseVersionOf(name)}
                      floating={baseLock.info(name).currentIsFloating}
                      unresolved={baseIsUnresolved(name)}
                      resolving={resolvingNames.has(name)}
                      onResolve={() => resolveVersion(name)}
                    />
                  ))}
                </div>
              )}
            </div>
          )}
          {imported.length > 0 && (
            <div className="border-b border-slate-100 py-1.5 last:border-b-0">
              <div className="px-3 pb-1 text-[10px] font-bold uppercase tracking-[0.5px] text-slate-400">
                From {importedTemplate?.displayName}
              </div>
              {imported.map((name) => (
                <div key={name} className="flex items-center justify-between gap-2 px-3 py-1">
                  <span
                    className="min-w-0 flex-1 truncate text-[12px] text-slate-700"
                    title={name}
                  >
                    {name}
                  </span>
                  <button
                    type="button"
                    onClick={() => setBasePackageRemoved(name, true)}
                    aria-label={`Remove ${name}`}
                    className="shrink-0 text-slate-400 hover:text-red-600"
                  >
                    ×
                  </button>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

interface BaseOnlyRowProps {
  name: string
  version: string | undefined
  floating: boolean
  unresolved: boolean
  resolving: boolean
  onResolve: () => void
}

function BaseOnlyRow({ name, version, floating, unresolved, resolving, onResolve }: BaseOnlyRowProps) {
  return (
    <div className="flex items-center justify-between gap-2 px-3 py-1">
      <span className="min-w-0 flex-1 truncate text-[12px] text-slate-700" title={name}>
        {name}
      </span>
      {unresolved ? (
        <button
          type="button"
          onClick={onResolve}
          disabled={resolving}
          className="shrink-0 text-[11px] font-medium text-[#0071c5] hover:underline disabled:cursor-wait disabled:text-slate-400 disabled:no-underline"
        >
          {resolving ? 'Resolving…' : 'Resolve'}
        </button>
      ) : (
        <VersionBadge
          version={version!}
          floating={floating}
          title={floating ? `currently resolves to ${version} (unpinned)` : `pinned to ${version} by the template`}
        />
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
      {floating && <span className="text-[9px] font-semibold uppercase tracking-wide text-slate-400">latest</span>}
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
