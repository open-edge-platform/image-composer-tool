import { useStore } from '../store'
import type { PackageRepo } from '../api/types'

interface PackageRepoBrowserProps {
  repos: PackageRepo[]
  activeRepo: string
  onActiveRepo: (id: string) => void
}

// PackageRepoBrowser is the two-pane repository browser: a rail listing every
// repository offered for the target, and a pane describing whichever one is
// being browsed.
//
// Each rail row carries two separate affordances — the checkbox enables the
// repository, the row body selects it for browsing. They are siblings rather
// than nested (the prototype nests a checkbox inside a clickable div, which
// leaves the browse action unreachable by keyboard).
export function PackageRepoBrowser({ repos, activeRepo, onActiveRepo }: PackageRepoBrowserProps) {
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
        {active ? <RepoPane repo={active} enabled={isEnabled(active)} /> : <RepoPaneEmpty />}
      </div>
    </div>
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

function RepoPane({ repo, enabled }: { repo: PackageRepo; enabled: boolean }) {
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
      {enabled ? (
        <p className="px-5 py-12 text-center text-[13px] text-slate-500">
          <span className="font-semibold text-slate-600">
            This repository's package list isn't available yet.
          </span>
          <br />
          Browsing and searching a repository's packages needs the package index
          API, which arrives in a later update.
        </p>
      ) : (
        <p className="px-5 py-12 text-center text-[13px] text-slate-500">
          <span className="font-semibold text-slate-600">This repository is disabled.</span>
          <br />
          Enable it with the checkbox on the left to pick from its packages.
        </p>
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
