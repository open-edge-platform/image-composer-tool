import { useState } from 'react'
import type { AddedPackage } from '../store'
import type { PackageVersion } from '../api/types'

// How many version chips a row shows before collapsing the rest behind
// "+N more". A package usually has two — the release and its -updates or
// -security counterpart — so two keeps the common row to a single line.
const DEFAULT_VISIBLE_VERSIONS = 2

interface PackageRowProps {
  name: string
  version: string
  description?: string
  // Every version on offer, newest first. Each carries its own repository
  // because pinning a version also pins where it comes from. The distinct
  // repositories across these versions are what the "via ..." line lists.
  versions: PackageVersion[]
  // This row's current pick, if any. undefined means not added.
  selection: AddedPackage | undefined
  onToggle: (checked: boolean) => void
  onChooseVersion: (v: PackageVersion) => void
  // Renders a repository ID as its display name, for the "via ..." line and
  // the chip tooltips.
  repoLabelFor: (repoId: string) => string
  // True for a package the matched template already ships. The checkbox
  // stays checked+disabled either way — the extends merge unions package
  // lists, so unchecking it could never actually remove it — but version
  // chips stay live: picking a different one is a real, meaningful override
  // on top of what the template already includes, not a no-op.
  locked?: boolean
  // The version already in effect from the template itself, when knowable
  // (see BaseLock in lib/baseLock.ts). Its matching chip renders disabled
  // rather than selectable, since picking it again wouldn't change anything.
  currentVersion?: string
  // True when the template's own entry is unpinned, meaning "Latest" is
  // just as redundant a pick as currentVersion's chip — the template
  // already floats to whatever's newest.
  currentIsFloating?: boolean
  // Offered only by the "Show only selected" review panel, for a row that's
  // unpinned (a locked-but-floating template entry, or a floating user
  // pick) and hasn't been checked against the currently-checked repos yet.
  // Fires a real lookup so the row can show what it actually resolves to
  // today instead of a bare "Latest".
  onResolve?: () => void
  resolving?: boolean
}

// PackageRow is the shared package row used by both the repo browse pane and
// the search dropdown, so a package looks the same wherever it's found.
export function PackageRow({
  name,
  version,
  description,
  versions,
  selection,
  onToggle,
  onChooseVersion,
  repoLabelFor,
  locked,
  currentVersion,
  currentIsFloating,
  onResolve,
  resolving,
}: PackageRowProps) {
  const checked = locked || selection != null
  const repoNames = [...new Set(versions.map((v) => v.repository).filter(Boolean))].map(repoLabelFor)
  // An override only ever exists on a locked row once the user has
  // explicitly picked a version — the template's own inclusion never
  // becomes a real addedPackages entry on its own.
  const overridden = locked && selection != null

  // Clicking the chip that's already the current pick removes it via the
  // same path the checkbox uses, rather than re-adding the identical entry
  // (a no-op) and leaving the only way back "not selected" a trip to the
  // Selected rail. For a locked row this falls back to the template's own
  // (unoverridden) state rather than removing anything real.
  const chooseVersion = (v: PackageVersion) => {
    if (selection && selection.version === v.version) {
      onToggle(false)
      return
    }
    onChooseVersion(v)
  }

  return (
    <label
      className={
        'flex cursor-pointer items-start gap-2.5 border-b border-slate-100 px-3 py-2.5 last:border-b-0 ' +
        (locked ? 'bg-slate-50' : 'hover:bg-[#eef4fb]')
      }
    >
      <input
        type="checkbox"
        checked={checked}
        disabled={locked}
        onChange={(e) => onToggle(e.target.checked)}
        className="mt-0.5 h-[15px] w-[15px] shrink-0 accent-[#0071c5] disabled:cursor-not-allowed"
        title={locked ? "Already included by the matched template — this can't be unchecked" : undefined}
      />
      <span className="min-w-0 flex-1">
        <span className="flex flex-wrap items-baseline gap-1.5">
          <span className={'text-[13px] font-semibold ' + (locked ? 'text-slate-500' : 'text-slate-700')}>
            {name}
          </span>
          <span className="font-mono text-[11px] text-slate-500">
            {locked && currentVersion ? currentVersion : version || 'latest'}
          </span>
          {repoNames.length > 0 && (
            <span className="text-[11px] text-slate-400">via {repoNames.join(', ')}</span>
          )}
        </span>
        {description && (
          <span className="mt-0.5 block truncate text-[11px] text-slate-500" title={description}>
            {description}
          </span>
        )}
        <VersionChips
          versions={versions}
          pinned={selection?.version}
          onChoose={chooseVersion}
          repoLabelFor={repoLabelFor}
          currentVersion={locked ? currentVersion : undefined}
          currentIsFloating={locked ? currentIsFloating : undefined}
        />
        {locked && (
          <span className="mt-1.5 flex flex-wrap items-center gap-1.5 text-[11px]">
            <span className="font-medium text-slate-500">
              Already in template
              {currentVersion
                ? currentIsFloating
                  ? ` — currently resolves to ${currentVersion} (unpinned; pin a chip to freeze it)`
                  : ` (${currentVersion})`
                : ' (version resolved at build time)'}
            </span>
            {overridden && (
              <span className="rounded-full bg-amber-100 px-1.5 py-0.5 font-medium text-amber-800">
                Overrides with {selection?.version || 'latest'}
              </span>
            )}
          </span>
        )}
        {onResolve && (
          <button
            type="button"
            onClick={(e) => {
              e.preventDefault()
              e.stopPropagation()
              onResolve()
            }}
            disabled={resolving}
            className="mt-1 block text-[11px] font-medium text-[#0071c5] hover:underline disabled:cursor-wait disabled:text-slate-400 disabled:no-underline"
          >
            {resolving ? 'Resolving…' : 'Resolve version'}
          </button>
        )}
      </span>
    </label>
  )
}

// VersionChips offers "Latest" plus one chip per available version. Latest is a
// floating pick that follows whatever the repository publishes next, where a
// version chip freezes that exact string — which is what a reproducible build
// needs. A package carried by several suites or repositories offers all of
// them, newest first, with the tail behind "+N more".
//
// currentVersion/currentIsFloating (locked rows only) mark whichever pick is
// already the template's own choice: that chip (or Latest, if the template's
// own entry floats) renders disabled rather than selectable, since it
// wouldn't change anything — every other chip is a real override.
function VersionChips({
  versions,
  pinned,
  onChoose,
  repoLabelFor,
  currentVersion,
  currentIsFloating,
}: {
  versions: PackageVersion[]
  pinned: string | undefined
  onChoose: (v: PackageVersion) => void
  repoLabelFor: (repoId: string) => string
  currentVersion?: string
  currentIsFloating?: boolean
}) {
  const [expanded, setExpanded] = useState(false)
  const currentIdx = currentVersion !== undefined ? versions.findIndex((v) => v.version === currentVersion) : -1
  let shown = expanded ? versions : versions.slice(0, DEFAULT_VISIBLE_VERSIONS)
  // The template's own version is the one chip a locked row most needs
  // visible — don't let it hide behind "+N more".
  if (!expanded && currentIdx >= DEFAULT_VISIBLE_VERSIONS) {
    shown = [...shown, versions[currentIdx]]
  }
  const hidden = versions.length - shown.length

  // Chips live inside the row's <label>, so a click must be stopped from also
  // toggling the checkbox.
  const stop = (e: React.MouseEvent) => {
    e.preventDefault()
    e.stopPropagation()
  }

  // Nothing to attribute an override to without at least one real repository
  // — true for an unpinned row the "Show only selected" panel hasn't
  // resolved yet (versions is empty there by design), and would also catch
  // the caller's own empty-versions fallback synthesizing a single entry
  // with a real version but no repository.
  const noRepoKnown = versions.every((v) => !v.repository)
  // Floating and not yet known to differ from a specific version (either
  // there's no currentVersion to compare against, or nothing's pinned at
  // all) reads as "Latest" being the row's actual current state, not just
  // its default — worth highlighting the same way an explicit pin would be.
  const latestIsCurrent = pinned === '' || (currentIsFloating === true && currentVersion === undefined)

  return (
    <span className="mt-1.5 flex flex-wrap items-center gap-1.5">
      <button
        type="button"
        disabled={currentIsFloating || noRepoKnown}
        title={
          noRepoKnown
            ? 'Resolve this package to enable picking a version'
            : currentIsFloating
              ? "Already the template's own (unpinned) choice"
              : undefined
        }
        onClick={(e) => {
          stop(e)
          if (currentIsFloating || noRepoKnown) return
          onChoose({ version: '', repository: versions[0]?.repository ?? '' })
        }}
        className={chipClass({ active: latestIsCurrent, disabled: currentIsFloating || noRepoKnown })}
      >
        Latest
      </button>
      {shown.map((v) => {
        const isCurrent = v.version === currentVersion
        // Only truly redundant when the template already pins this exact
        // version itself — clicking it again couldn't do anything the union
        // merge doesn't already guarantee. When the template is merely
        // floating there today (currentIsFloating), pinning the same string
        // is a real, different action: it freezes today's resolution
        // against drift from a future repo update or newly-enabled repo,
        // where leaving it alone would keep following whatever's newest.
        const isRedundant = isCurrent && !currentIsFloating
        return (
          <button
            key={`${v.repository} ${v.version}`}
            type="button"
            disabled={isRedundant}
            title={
              isRedundant
                ? `${v.version} from ${repoLabelFor(v.repository)} — already pinned by the template`
                : isCurrent
                  ? `${v.version} from ${repoLabelFor(v.repository)} — what the template currently resolves to; pin it to freeze that against future changes`
                  : `${v.version} from ${repoLabelFor(v.repository)}`
            }
            onClick={(e) => {
              stop(e)
              if (isRedundant) return
              onChoose(v)
            }}
            className={chipClass({ active: pinned === v.version, disabled: isRedundant, redundant: isRedundant, ring: isCurrent })}
          >
            {v.version}
          </button>
        )
      })}
      {hidden > 0 && (
        <button
          type="button"
          onClick={(e) => {
            stop(e)
            setExpanded(!expanded)
          }}
          className="rounded-full px-2 py-0.5 text-[11px] font-medium text-slate-500 hover:text-[#0071c5] hover:underline"
        >
          {expanded ? 'show less' : `+${hidden} more`}
        </button>
      )}
    </span>
  )
}

// active: a real selection (or, for Latest, the row's actual current
// floating state) — full blue fill. disabled: not clickable; on an active
// chip this dims the same blue rather than switching to grey, since the
// information it's conveying is still true, just not actionable right now
// (e.g. Latest already being the template's own choice). redundant is a
// stronger, separate case — a chip that could never do anything regardless
// of active/disabled (the template already pins this exact version itself)
// — grey, ignoring every other flag. ring flags "this is what the row
// currently resolves to" on a chip that's neither active nor redundant.
function chipClass({
  active = false,
  disabled = false,
  redundant = false,
  ring = false,
}: {
  active?: boolean
  disabled?: boolean
  redundant?: boolean
  ring?: boolean
}): string {
  if (redundant) {
    return 'rounded-full px-2 py-0.5 font-mono text-[11px] font-medium bg-slate-200 text-slate-500 cursor-not-allowed ring-1 ring-slate-300'
  }
  const base = 'rounded-full px-2 py-0.5 font-mono text-[11px] font-medium '
  const fill = active ? 'bg-[#0071c5] text-white' : 'bg-[#e6f2fa] text-[#0071c5]'
  const hover = disabled || active ? '' : ' hover:bg-[#d3e9f8]'
  const cursor = disabled ? ' cursor-not-allowed opacity-70' : ''
  const ringClass = !active && ring ? ' ring-1 ring-[#0071c5]' : ''
  return base + fill + hover + cursor + ringClass
}
