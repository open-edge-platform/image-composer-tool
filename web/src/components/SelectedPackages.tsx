import { useEffect, type ReactNode } from 'react'
import { useStore, confirmRepoRelease, hasAIChanges, isAIChange, type AddedPackage } from '../store'
import type { PackageRepo } from '../api/types'
import type { VersionResolver } from '../lib/useVersionResolver'
import type { BaseLock } from '../lib/baseLock'

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
  // The matched template's own packages, to tell a new package (Added) from a
  // template package given another version (Modified). Without it every pick
  // counts as Added.
  baseLock?: BaseLock
  // Author mode, where template packages can also be removed.
  authoring?: boolean
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

// SelectedPackages is the right rail, "Changes": how the user's packages
// differ from the matched template. Added packages are grouped by the
// repository they came from; Modified lists template packages given another
// version; Removed (Author mode only) lists template packages taken out. Each
// can be undone on its own, or all at once with Reset to template defaults.
// Stickiness is handled by the caller (PackagesStep.tsx).
export function SelectedPackages({
  repos,
  resolver = noResolver,
  lockedRepoIds = noLockedRepos,
  baseLock,
  authoring = false,
}: SelectedPackagesProps) {
  const picks = useStore((s) => s.addedPackages)
  const removePackage = useStore((s) => s.removePackage)
  const clearPackages = useStore((s) => s.clearPackages)
  const storedRemovals = useStore((s) => s.removedPackages)
  const setPackageRemoved = useStore((s) => s.setPackageRemoved)
  const clearRemovedPackages = useStore((s) => s.clearRemovedPackages)
  // Which changes Ask AI made, for the AI tag (see isAIChange).
  const aiChanges = useStore((s) => s.aiChanges)
  const undoAIChanges = useStore((s) => s.undoAIChanges)
  const fromAI = (name: string) => isAIChange({ aiChanges }, name)
  const inTemplate = (name: string) => !!baseLock?.info(name).locked
  const addedPackages = picks.filter((p) => !inTemplate(p.name))
  const modified = picks.filter((p) => inTemplate(p.name))
  const removed = authoring ? storedRemovals : []

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

  const total = addedPackages.length + modified.length + removed.length
  const undo = (name: string) =>
    removePackage(name, {
      releaseRepo: confirmRepoRelease(picks, labelFor, [name], isProtectedRepo),
      protectedRepos: lockedRepoIds,
    })
  const templateVersion = (name: string) => {
    const info = baseLock?.info(name)
    return info?.currentIsFloating ? 'unpinned' : (info?.currentVersion ?? 'template version')
  }

  return (
    <div className="rounded-lg border border-slate-200 bg-white">
      <div className="flex items-center justify-between gap-2 border-b border-slate-100 px-3 py-2.5">
        <span className="text-[13px] font-bold text-[#00285a]">Changes ({total})</span>
        <div className="flex items-center gap-1.5">
          {hasAIChanges(aiChanges) && (
            <button
              type="button"
              onClick={undoAIChanges}
              title="Undo what Ask AI changed (packages, repositories, disk size and image name) and keep your own changes"
              className="rounded-md border border-violet-300 px-3 py-1 text-xs font-medium text-violet-800 hover:border-violet-400 hover:bg-violet-50"
            >
              Undo AI changes
            </button>
          )}
          <button
            type="button"
            onClick={() => {
              clearPackages(lockedRepoIds)
              clearRemovedPackages()
            }}
            disabled={total === 0}
            className="rounded-md border border-slate-300 px-3 py-1 text-xs font-medium text-slate-600 hover:border-slate-400 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-40"
          >
            Reset to template defaults
          </button>
        </div>
      </div>
      {/* The list's cap tracks the viewport so the rail, which starts level
          with the search box, still ends above the Back/Next row (25rem is the
          chrome above and below it); below the floor the page scrolls instead. */}
      {total === 0 ? (
        <p className="px-3 py-8 text-center text-[12px] text-slate-500">No changes yet.</p>
      ) : (
        <div className="max-h-[clamp(240px,calc(100vh-25rem),520px)] overflow-y-auto">
          {addedPackages.length > 0 && (
            <div className="border-b border-slate-100 bg-[#f4f9fd] py-1.5 last:border-b-0">
              <div className="px-3 pb-1 text-[10px] font-bold uppercase tracking-[0.5px] text-[#0071c5]">
                Added ({addedPackages.length})
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
                      {fromAI(p.name) && <AITag />}
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
                        onClick={() => undo(p.name)}
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
          {modified.length > 0 && (
            <Section title={`Modified (${modified.length})`}>
              {modified.map((p) => (
                <Item key={p.name} name={p.name} ai={fromAI(p.name)} onUndo={() => undo(p.name)} undoLabel={`Undo change to ${p.name}`}>
                  <span
                    className="shrink-0 font-mono text-[10px] text-slate-500"
                    title={`Template: ${templateVersion(p.name)} · Yours: ${p.version || 'unpinned'}`}
                  >
                    {templateVersion(p.name)} → <span className="font-semibold text-[#0071c5]">{p.version || 'unpinned'}</span>
                  </span>
                </Item>
              ))}
            </Section>
          )}
          {removed.length > 0 && (
            <Section title={`Removed (${removed.length})`}>
              {removed.map((name) => (
                <Item
                  key={name}
                  name={name}
                  struck
                  ai={fromAI(name)}
                  onUndo={() => setPackageRemoved(name, false)}
                  undoLabel={`Put back ${name}`}
                />
              ))}
            </Section>
          )}
        </div>
      )}
    </div>
  )
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="border-b border-slate-100 py-1.5 last:border-b-0">
      <div className="px-3 pb-1 text-[10px] font-bold uppercase tracking-[0.5px] text-[#0071c5]">{title}</div>
      {children}
    </div>
  )
}

// One modified or removed package, with a button that undoes the change.
function Item({
  name,
  struck = false,
  ai = false,
  onUndo,
  undoLabel,
  children,
}: {
  name: string
  struck?: boolean
  ai?: boolean
  onUndo: () => void
  undoLabel: string
  children?: ReactNode
}) {
  return (
    <div className="flex items-center justify-between gap-2 px-3 py-1">
      <span
        className={'min-w-0 flex-1 truncate text-[12px] ' + (struck ? 'text-slate-500 line-through' : 'text-[#0071c5]')}
        title={name}
      >
        {name}
      </span>
      {ai && <AITag />}
      {children}
      <button
        type="button"
        onClick={onUndo}
        aria-label={undoLabel}
        title={undoLabel}
        className="shrink-0 text-[12px] text-slate-400 hover:text-[#0071c5]"
      >
        ↺
      </button>
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

// Marks a change Ask AI made.
function AITag() {
  return (
    <span
      title="Suggested by Ask AI"
      className="shrink-0 rounded bg-violet-100 px-1 text-[9px] font-bold uppercase tracking-[0.5px] text-violet-800"
    >
      AI
    </span>
  )
}
