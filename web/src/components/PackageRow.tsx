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
  // True for a package the matched template already ships. Outside Author
  // mode (see removable) the checkbox stays checked+disabled either way — the extends merge unions package
  // lists, so unchecking it could never actually remove it — but version
  // chips stay live: picking a different one is a real, meaningful override
  // on top of what the template already includes, not a no-op.
  locked?: boolean
  // Author mode, where the template itself is edited: a locked row's checkbox
  // is live, and unchecking it removes the package from the template.
  removable?: boolean
  // A template package removed in Author mode. It shows as an unchecked row
  // marked Removed; checking it again puts it back.
  removed?: boolean
  // Drops the user's version change on a template package, so the template's
  // own entry applies again. Defaults to onToggle(false), which does the same
  // outside Author mode; in Author mode unchecking removes the package instead.
  onRevert?: () => void
  // The version already in effect from the template itself, when knowable
  // (see BaseLock in lib/baseLock.ts). Its matching chip renders disabled
  // rather than selectable, since picking it again wouldn't change anything.
  currentVersion?: string
  // True when the template's own entry is unpinned, meaning "Latest" is
  // just as redundant a pick as currentVersion's chip — the template
  // already floats to whatever's newest.
  currentIsFloating?: boolean
  // For a locked row whose template pin is a version this row's repository
  // doesn't publish: where the pinned version comes from (`repoLabel`, or
  // undefined while that is still looked up), and how to go and browse it.
  // The row says so, and this repository's chips warn that choosing one
  // replaces the template's build.
  pinnedFrom?: { repoLabel?: string; onBrowse?: () => void }
  // The repository being browsed, on a surface that lists one repository's
  // catalog. "Via" never names it: every row there comes from it, so it
  // would say nothing. Unset where rows come from several repositories.
  browsedRepoId?: string
  // Switches the browsed repository, so "via" can link to the repository a
  // version comes from. Unset where there's nothing to switch.
  onBrowseRepo?: (repoId: string) => void
  // Offered only by the Changes rail, for a row that's unpinned (a
  // locked-but-floating template entry, or a floating user pick) and hasn't
  // been checked against the currently-checked repos yet. Fires a real
  // lookup so the row can show what it actually resolves to today instead
  // of a bare "Latest".
  onResolve?: () => void
  resolving?: boolean
  // What this row's name actually resolves to right now, checked against
  // every currently-checked repository (not just the one this row happens
  // to be shown from) — the shared live resolver's result. Distinct from
  // currentVersion, which is the matched template's own pin and only ever
  // set on a locked row; this applies to an explicit (non-locked) floating
  // selection instead, which otherwise has no way to show what it's really
  // about to resolve to.
  resolvedVersion?: string
  resolvedRepo?: string
  // Blocks selecting this row, with `disabledReason` as its tooltip. Used by
  // the Edge Pack tab, where a package cannot be picked until a base runtime is
  // chosen. Version chips are disabled alongside the checkbox: choosing a
  // version is a form of selecting, so leaving them live would offer a way past
  // the gate.
  //
  // Orthogonal to `locked`: that one is checked-and-permanent because the
  // template already ships the package, this one is unselectable because a
  // precondition has not been met. No surface passes both.
  disabled?: boolean
  disabledReason?: string
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
  locked: lockedProp,
  removable = false,
  removed = false,
  onRevert,
  currentVersion,
  currentIsFloating,
  pinnedFrom,
  browsedRepoId,
  onBrowseRepo,
  onResolve,
  resolving,
  resolvedVersion,
  resolvedRepo,
  disabled = false,
  disabledReason,
}: PackageRowProps) {
  // A removed template package reads as a package the template doesn't ship.
  const locked = !!lockedProp && !removed
  const checked = locked || selection != null
  // Whether the package is pinned, and the version a build would install:
  // the user's own pick first, since it overrides the template, then the
  // template's own entry. Undefined for a catalog row nothing selects.
  const pin =
    selection != null && selection.version !== ''
      ? { version: selection.version, repo: selection.repo as string | undefined }
      : locked && selection == null && currentVersion && !currentIsFloating
        ? { version: currentVersion, repo: undefined }
        : undefined
  const unpinned = (selection != null && selection.version === '') || (locked && selection == null && !!currentIsFloating)
  // What Unpinned currently gives: the version it resolves to, once known.
  const resolvesTo = !unpinned
    ? undefined
    : selection != null
      ? resolvedVersion
        ? { version: resolvedVersion, repo: resolvedRepo }
        : undefined
      : currentVersion
        ? { version: currentVersion, repo: resolvedRepo }
        : undefined
  const tag = pin ? 'Pinned' : unpinned ? 'Unpinned' : undefined
  // The version a build would install, shown beside the tag. It's also
  // marked among the row's own chips when it's one of them.
  const effective = pin ?? resolvesTo
  const templatePinElsewhere = !!(locked && selection == null && pinnedFrom && pin)
  // "Via" for a catalog row nothing selects: its own repositories, never the
  // one being browsed, which every row there comes from.
  const repoNames = [...new Set(versions.map((v) => v.repository).filter(Boolean))]
    .filter((id) => id !== browsedRepoId)
    .map(repoLabelFor)
  // An override only ever exists on a locked row once the user has
  // explicitly picked a version — the template's own inclusion never
  // becomes a real addedPackages entry on its own.
  const overridden = locked && selection != null

  // Pinned to a specific version (not Latest) from a repository that isn't
  // among this row's own versions at all — true whenever the package is
  // being browsed from a different repository than the one it's actually
  // pinned to. No chip here can represent that pin, so it needs its own
  // note rather than silently looking unselected.
  const pinnedElsewhere =
    selection != null &&
    selection.version !== '' &&
    !versions.some((v) => v.repository === selection.repo && v.version === selection.version)

  // Clicking the chip that's already the current pick removes it via the
  // same path the checkbox uses, rather than re-adding the identical entry
  // (a no-op) and leaving the only way back "not selected" a trip to the
  // Changes rail. For a locked row this falls back to the template's own
  // (unoverridden) state rather than removing anything real. Latest is
  // repo-agnostic on purpose — it isn't pinned to any one repository's
  // version list, so a version match alone is enough regardless of which
  // repo it's being re-clicked from.
  // Choosing the pinned chip again unpins the package, which stays added: a
  // package with no version chosen is unpinned, so there's no separate
  // Unpinned chip. On a template package it drops the override instead, so
  // the template's own entry applies again. (Its own pin can't be unpinned
  // from here: an unpinned entry in a delta leaves the template's pin in
  // place.) Removing the package is the checkbox's job.
  //
  // Author mode edits the template itself, so there a pinned template package
  // can be unpinned: choosing the selected chip again leaves it unpinned (or,
  // where the template already leaves it unpinned, drops the override).
  const chooseVersion = (v: PackageVersion) => {
    const sameAsSelection = selection != null && selection.version === v.version && selection.repo === v.repository
    if (sameAsSelection) {
      if (locked && removable && !currentIsFloating) onChooseVersion({ version: '', repository: selection.repo })
      else if (locked) (onRevert ?? (() => onToggle(false)))()
      else onChooseVersion({ version: '', repository: selection.repo })
      return
    }
    onChooseVersion(v)
  }

  return (
    <label
      title={disabled ? disabledReason : undefined}
      className={
        'flex items-start gap-2.5 border-b border-slate-100 px-3 py-2.5 last:border-b-0 ' +
        (disabled
          ? 'cursor-not-allowed opacity-60'
          : locked
            ? 'cursor-pointer bg-slate-50'
            : 'cursor-pointer hover:bg-[#eef4fb]')
      }
    >
      <input
        type="checkbox"
        checked={checked}
        disabled={(locked && !removable) || disabled}
        onChange={(e) => onToggle(e.target.checked)}
        className="mt-0.5 h-[15px] w-[15px] shrink-0 accent-[#0071c5] disabled:cursor-not-allowed"
        title={
          locked
            ? removable
              ? 'In the template. Uncheck it to remove it from the template.'
              : "Already included by the matched template — this can't be unchecked"
            : undefined
        }
      />
      {/* A container, not a viewport, breakpoint: this row is shared by the
          full-width repo browser and narrower surfaces (the search dropdown,
          Edge Pack domains), and only the space the row itself gets decides
          whether the version chips fit beside the name instead of under it. */}
      <span className="@container min-w-0 flex-1">
        <span className="flex flex-col">
          <span className="min-w-0 flex-1">
            <span className="flex flex-wrap items-baseline gap-1.5">
              <span className={'text-[13px] font-semibold ' + (locked ? 'text-slate-500' : 'text-slate-700')}>
                {name}
              </span>
              {removed && (
                <span
                  className="rounded bg-red-100 px-1 py-px text-[10px] font-semibold uppercase tracking-wide text-red-700"
                  title="Removed from the template. Check it to put it back."
                >
                  Removed
                </span>
              )}
              {tag && (
                <span className="rounded bg-slate-100 px-1 py-px text-[10px] font-semibold uppercase tracking-wide text-slate-500">
                  {tag}
                </span>
              )}
              {overridden && (
                // A template package the user changed. The greyed checkbox
                // already says it's in the template; this says it differs.
                <span
                  className="rounded bg-amber-100 px-1 py-px text-[10px] font-semibold uppercase tracking-wide text-amber-800"
                  title={`Template: ${
                    currentIsFloating ? 'unpinned' : currentVersion ? `pinned to ${currentVersion}` : 'version resolved at build time'
                  } · Yours: ${selection?.version || 'unpinned'}`}
                >
                  Modified
                </span>
              )}
              {!tag && <span className="font-mono text-[11px] text-slate-500">{version}</span>}
              {effective && (
                // The version a build would install: plain bold text, not a
                // chip, so it doesn't read as one more version to choose.
                <span
                  className="font-mono text-[11px] font-bold text-slate-700"
                  title={pin ? 'Pinned version' : 'The version Unpinned currently resolves to'}
                >
                  {effective.version}
                </span>
              )}
              {effective ? (
                <ViaRepo
                  label={
                    templatePinElsewhere
                      ? pinnedFrom?.repoLabel
                      : effective.repo && effective.repo !== browsedRepoId
                        ? repoLabelFor(effective.repo)
                        : undefined
                  }
                  onBrowse={
                    templatePinElsewhere
                      ? pinnedFrom?.onBrowse
                      : effective.repo && onBrowseRepo
                        ? () => onBrowseRepo(effective.repo!)
                        : undefined
                  }
                />
              ) : (
                !tag &&
                repoNames.length > 0 && <span className="text-[11px] text-slate-400">via {repoNames.join(', ')}</span>
              )}
            </span>
            {description && (
              <span className="mt-0.5 block truncate text-[11px] text-slate-500" title={description}>
                {description}
              </span>
            )}
            {pinnedElsewhere && (
              <span className="mt-0.5 block text-[11px] text-slate-500">
                Picking a version here pins {repoLabelFor(versions[0]?.repository ?? '')} instead of{' '}
                {repoLabelFor(selection!.repo)}.
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
          <VersionChips
            versions={versions}
            pinned={selection?.version}
            pinnedRepo={selection?.repo}
            onChoose={chooseVersion}
            repoLabelFor={repoLabelFor}
            currentVersion={locked ? currentVersion : undefined}
            currentIsFloating={locked ? currentIsFloating : undefined}
            replaces={
              locked && pinnedFrom && currentVersion
                ? `${currentVersion} from ${pinnedFrom.repoLabel ?? 'another repository'}`
                : undefined
            }
            overridden={overridden}
            canUnpin={removable}
            onRevert={onRevert ?? (() => onToggle(false))}
            resolvesTo={resolvesTo}
            disabled={disabled}
          />
        </span>
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
  pinnedRepo,
  onChoose,
  repoLabelFor,
  currentVersion,
  currentIsFloating,
  overridden = false,
  canUnpin = false,
  onRevert,
  replaces,
  resolvesTo,
  disabled,
}: {
  versions: PackageVersion[]
  pinned: string | undefined
  // Which repository `pinned` actually came from. A specific-version chip
  // only reads as selected when both match — two repositories can publish
  // the same version string, and that isn't the same pin.
  pinnedRepo: string | undefined
  onChoose: (v: PackageVersion) => void
  repoLabelFor: (repoId: string) => string
  currentVersion?: string
  currentIsFloating?: boolean
  // A locked row the user has picked a version for. The template's own
  // choice is then no longer redundant: choosing it drops the override
  // (onRevert), so the template's entry applies again.
  overridden?: boolean
  // Author mode: the template's own pin chip is live, and choosing it unpins
  // the package.
  canUnpin?: boolean
  onRevert?: () => void
  // The template's own build, when its pinned version comes from another
  // repository: each chip here would replace it, so says so.
  replaces?: string
  // What Unpinned currently resolves to, while it's the row's state: that
  // chip is marked dashed, beside the selected Unpinned chip.
  resolvesTo?: { version: string; repo?: string }
  disabled: boolean
}) {
  const [expanded, setExpanded] = useState(false)
  const currentIdx = currentVersion !== undefined ? versions.findIndex((v) => v.version === currentVersion) : -1
  let shown = expanded ? versions : versions.slice(0, DEFAULT_VISIBLE_VERSIONS)
  // The template's own version and the user's own pin are the chips a row
  // most needs visible, since the header leaves a pin to its chip — don't
  // let either hide behind "+N more".
  const pinnedIdx = pinned ? versions.findIndex((v) => v.version === pinned && v.repository === pinnedRepo) : -1
  if (!expanded) {
    for (const idx of [currentIdx, pinnedIdx]) {
      if (idx >= DEFAULT_VISIBLE_VERSIONS && !shown.includes(versions[idx])) shown = [...shown, versions[idx]]
    }
  }
  const hidden = versions.length - shown.length

  // A package the Edge Pack catalog names but whose repository index could not
  // be read has no versions at all. Offering a bare "Latest" chip there would
  // point at nothing the client can name, so the whole strip is omitted — the
  // row's own checkbox still adds the package, which is what Latest means.
  if (versions.length === 0) return null

  // Chips live inside the row's <label>, so a click must be stopped from also
  // toggling the checkbox.
  const stop = (e: React.MouseEvent) => {
    e.preventDefault()
    e.stopPropagation()
  }

  // Nothing to attribute an override to without at least one real repository
  // — true for an unpinned row the Changes rail hasn't resolved yet
  // (versions is empty there by design), and would also catch the caller's
  // own empty-versions fallback synthesizing a single entry with a real
  // version but no repository.
  const noRepoKnown = versions.every((v) => !v.repository)
  return (
    <span className="mt-1.5 flex flex-wrap items-center gap-1.5">
      {shown.map((v) => {
        const isCurrent = v.version === currentVersion
        // Only truly redundant when the template already pins this exact
        // version itself — clicking it again couldn't do anything the union
        // merge doesn't already guarantee. When the template is merely
        // floating there today (currentIsFloating), pinning the same string
        // is a real, different action: it freezes today's resolution
        // against drift from a future repo update or newly-enabled repo,
        // where leaving it alone would keep following whatever's newest.
        //
        // With an override in place it is the way back to the template's own
        // pin instead, so it drops the override rather than being disabled.
        const templatePin = isCurrent && !currentIsFloating && !overridden
        const isRedundant = templatePin && !canUnpin
        const unpinsTemplate = templatePin && canUnpin
        const revertsToTemplate = isCurrent && !currentIsFloating && overridden
        const selectedHere = pinned === v.version && pinnedRepo === v.repository
        return (
          <button
            key={`${v.repository} ${v.version}`}
            type="button"
            disabled={disabled || isRedundant || noRepoKnown}
            title={
              noRepoKnown
                ? 'Resolve this package to enable picking a version'
                : selectedHere
                  ? `${v.version} from ${repoLabelFor(v.repository)} — ${overridden ? 'your override; choose it again to drop it' : 'pinned; choose it again to unpin'}`
                  : unpinsTemplate
                    ? `${v.version} from ${repoLabelFor(v.repository)} — pinned by the template; choose it again to unpin`
                  : isRedundant
                ? `${v.version} from ${repoLabelFor(v.repository)} — already pinned by the template`
                : revertsToTemplate
                  ? `${v.version} from ${repoLabelFor(v.repository)} — the template's own pin; choose it to drop your override`
                  : isCurrent
                    ? `${v.version} from ${repoLabelFor(v.repository)} — what the template currently resolves to; pin it to freeze that against future changes`
                    : replaces
                      ? `${v.version} from ${repoLabelFor(v.repository)} — replaces the template's own build, ${replaces}`
                      : `${v.version} from ${repoLabelFor(v.repository)}`
            }
            onClick={(e) => {
              stop(e)
              if (disabled || isRedundant || noRepoKnown) return
              if (revertsToTemplate) {
                onRevert?.()
                return
              }
              if (unpinsTemplate) {
                onChoose({ version: '', repository: v.repository })
                return
              }
              onChoose(v)
            }}
            aria-pressed={(pinned === v.version && pinnedRepo === v.repository) || templatePin}
            className={chipClass({
              // The template's own pin reads as selected, the same as a pick
              // of the user's; it just isn't clickable, as choosing it changes
              // nothing.
              active: (pinned === v.version && pinnedRepo === v.repository) || templatePin,
              disabled,
              dashed:
                !!resolvesTo &&
                v.version === resolvesTo.version &&
                (resolvesTo.repo === undefined || v.repository === resolvesTo.repo),
            })}
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

// active: the chip in effect — a real selection, the template's own choice
// (its pin, or Unpinned when it leaves the package unpinned), or Unpinned
// when nothing is picked — full blue fill. dashed: the version Unpinned
// currently resolves to, while it's in effect — a dashed outline, the same
// mark the Selected panel uses for an unpinned version. disabled: not
// clickable; on an active chip this dims the same blue rather than
// switching to grey.
function chipClass({
  active = false,
  disabled = false,
  dashed = false,
}: {
  active?: boolean
  disabled?: boolean
  dashed?: boolean
}): string {
  const base = 'rounded-full px-2 py-0.5 font-mono text-[11px] font-medium '
  const fill = active
    ? 'bg-[#0071c5] text-white'
    : dashed
      ? 'border border-dashed border-[#0071c5] bg-white text-[#0071c5]'
      : 'bg-[#e6f2fa] text-[#0071c5]'
  const hover = disabled || active ? '' : ' hover:bg-[#d3e9f8]'
  const cursor = disabled ? ' cursor-not-allowed opacity-70' : ''
  return base + fill + hover + cursor
}

// ViaRepo names the repository a version shown beside a package's name
// comes from, as a link that browses it when there's a way to.
function ViaRepo({ label, onBrowse }: { label?: string; onBrowse?: () => void }) {
  if (!label) return null
  return (
    <span className="text-[11px] text-slate-400">
      via{' '}
      {onBrowse ? (
        <button
          type="button"
          onClick={(e) => {
            // Inside the row's <label>: don't toggle the checkbox.
            e.preventDefault()
            e.stopPropagation()
            onBrowse()
          }}
          title={`Browse ${label}, where this version comes from`}
          className="text-[#0071c5] hover:underline"
        >
          {label}
        </button>
      ) : (
        label
      )}
    </span>
  )
}
