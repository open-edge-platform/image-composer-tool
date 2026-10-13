import { useStore, confirmRepoRelease, type AddedPackage } from '../store'
import type { PackageRepo } from '../api/types'

interface SelectedPackagesProps {
  repos: PackageRepo[]
}

// SelectedPackages is the right rail: everything the user has added so far,
// grouped by the repository it came from, with a per-item remove and a
// clear-all. Stickiness is handled by the caller (PackagesStep.tsx).
export function SelectedPackages({ repos }: SelectedPackagesProps) {
  const addedPackages = useStore((s) => s.addedPackages)
  const removePackage = useStore((s) => s.removePackage)
  const clearPackages = useStore((s) => s.clearPackages)
  const importedTemplate = useStore((s) => s.importedTemplate)
  const removedBasePackages = useStore((s) => s.removedBasePackages)
  const setBasePackageRemoved = useStore((s) => s.setBasePackageRemoved)

  const labelFor = (repoId: string) => repos.find((r) => r.id === repoId)?.displayName ?? repoId
  const isBaseRepo = (repoId: string) => repos.find((r) => r.id === repoId)?.enabledByDefault ?? false

  // Filtered here rather than read back off the next compose, so a removal
  // leaves the list immediately instead of after a server round trip.
  const imported = (importedTemplate?.packages ?? []).filter(
    (p) => !removedBasePackages.includes(p),
  )

  const groups = new Map<string, AddedPackage[]>()
  for (const p of addedPackages) {
    const list = groups.get(p.repo) ?? []
    list.push(p)
    groups.set(p.repo, list)
  }
  const repoIds = [...groups.keys()].sort((a, b) => labelFor(a).localeCompare(labelFor(b)))

  const total = imported.length + addedPackages.length

  return (
    <div className="rounded-lg border border-slate-200 bg-white">
      <div className="flex items-center justify-between border-b border-slate-100 px-3 py-2.5">
        <span className="text-[13px] font-bold text-[#00285a]">Selected ({total})</span>
        {addedPackages.length > 0 && (
          <button
            type="button"
            onClick={clearPackages}
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
          {repoIds.map((repoId) => (
            <div key={repoId} className="border-b border-slate-100 py-1.5 last:border-b-0">
              <div className="px-3 pb-1 text-[10px] font-bold uppercase tracking-[0.5px] text-slate-400">
                {labelFor(repoId)}
              </div>
              {groups.get(repoId)!.map((p) => (
                <div key={p.name} className="flex items-center justify-between gap-2 px-3 py-1">
                  <span
                    className="min-w-0 flex-1 truncate text-[12px] text-slate-700"
                    title={p.name}
                  >
                    {p.name}
                  </span>
                  <span
                    className="shrink-0 rounded-full bg-[#e6f2fa] px-1.5 py-0.5 font-mono text-[10px] font-medium text-[#0071c5]"
                    title={
                      p.version
                        ? `pinned to ${p.version} from ${labelFor(p.repo)}`
                        : `follows the latest version ${labelFor(p.repo)} publishes`
                    }
                  >
                    {p.version || 'latest'}
                  </span>
                  <button
                    type="button"
                    onClick={() =>
                      removePackage(p.name, {
                        releaseRepo: confirmRepoRelease(addedPackages, labelFor, [p.name], isBaseRepo),
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
          {/* Last: a newly picked package should land in view, and the
              template's own list is the long, mostly-static one. */}
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
