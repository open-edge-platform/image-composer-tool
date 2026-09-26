// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import { useState } from 'react'
import { useStore } from '../store'
import type { EdgePack, EdgePackDomain, EdgePackPackage } from '../api/types'
import {
  allDomainPackages,
  canSelectDomains,
  domainLockReason,
  domainPackageNames,
  domainSelectable,
  groupSelectionState,
  groupToggleMode,
  isSelected,
  reposToEnable,
  strandedPackages,
  toAddedPackages,
  versionsOf,
} from '../lib/edgepack'
import { PackageRow } from './PackageRow'

interface EdgePackBrowserProps {
  pack: EdgePack
  // Display name for the pack's repository, for the "adds to <repo>" note and
  // the version-chip tooltips — the pack knows the repo id, not its label.
  repoLabel: string
  // Label lookup for any other repository id, for naming a domain's
  // prerequisite repositories. The pack carries ids; only the step knows labels.
  repoLabelFor: (id: string) => string
  // Label for the currently selected target, used when the pack's repository
  // isn't offered here at all.
  targetLabel: string
}

// EdgePackBrowser is the Packages step's Edge Pack tab: the capability view over
// the same packages the Repositories tab browses by provenance.
//
// Nothing about the selection lives here. Every checkbox's state is derived from
// the store's addedPackages on each render (see lib/edgepack.ts), which is what
// keeps this tab and the repository browser showing one shared truth — remove a
// package from the right-hand rail and its domain drops to partial immediately.
export function EdgePackBrowser({
  pack,
  repoLabel,
  repoLabelFor,
  targetLabel,
}: EdgePackBrowserProps) {
  // Which domain's package list is expanded, if any. Purely presentational, so
  // it is local state rather than store state — it must not survive a target
  // change the way a selection does.
  const [openDomain, setOpenDomain] = useState('')

  const addedPackages = useStore((s) => s.addedPackages)
  const setPackages = useStore((s) => s.setPackages)
  const removePackages = useStore((s) => s.removePackages)
  const setPackage = useStore((s) => s.setPackage)
  const removePackage = useStore((s) => s.removePackage)
  const setRepoEnabled = useStore((s) => s.setRepoEnabled)
  const enabledRepos = useStore((s) => s.enabledRepos)

  // Adding a package is a statement of interest in its repository, exactly as
  // it is in the search dropdown — without this the package would have nowhere
  // to resolve from at build time. The domains the selection touches are named
  // because a domain can need repositories beyond the pack's own: enabling only
  // the pack repo would leave its metapackage's dependencies unresolvable.
  //
  // Enabling only, never disabling: a repository may have been switched on for
  // reasons this tab knows nothing about, and clearing a domain must not take it
  // away from the rest of the template.
  const ensureReposEnabled = (domains: EdgePackDomain[]) => {
    for (const id of reposToEnable(pack, domains)) {
      if (!enabledRepos.includes(id)) setRepoEnabled(id, true)
    }
  }

  const addGroup = (packages: EdgePackPackage[], domains: EdgePackDomain[]) => {
    const toAdd = toAddedPackages(pack, packages, addedPackages)
    if (toAdd.length > 0) setPackages(toAdd)
    ensureReposEnabled(domains)
  }

  const removeGroup = (packages: EdgePackPackage[]) => {
    removePackages(packages.map((p) => p.name))
  }

  const setGroup = (packages: EdgePackPackage[], on: boolean, domains: EdgePackDomain[]) =>
    on ? addGroup(packages, domains) : removeGroup(packages)

  const gated = !canSelectDomains(pack, addedPackages)

  if (!pack.repoAvailable) {
    return (
      <p className="rounded-lg border border-dashed border-slate-300 px-4 py-10 text-center text-sm text-slate-500">
        <span className="font-semibold text-slate-600">
          {pack.displayName} is not published for {targetLabel}.
        </span>
        <br />
        Use the Repositories tab to pick packages for this target.
      </p>
    )
  }

  const domainPackages = allDomainPackages(pack)
  const packState = groupSelectionState(
    domainPackages.map((p) => p.name),
    addedPackages,
  )
  const packMode = groupToggleMode(!gated, packState.state)

  return (
    <>
      <BaseRuntimeSelector pack={pack} />

      <div className="overflow-hidden rounded-lg border border-slate-200">
        {/* Pack-level select-all. Its scope is the domains only — the base
            runtime above is a prerequisite the user set deliberately, and this
            checkbox must not clear it. */}
        <div className="flex items-center justify-between gap-3 border-b border-slate-200 bg-slate-50 px-3.5 py-2.5">
          <label
            className={
              'flex min-w-0 items-center gap-2 ' +
              (packMode === 'locked' ? 'cursor-not-allowed' : 'cursor-pointer')
            }
            title={
              packMode === 'locked'
                ? 'Select a base runtime first'
                : packMode === 'clear' && gated
                  ? 'Clear packages selected without a base runtime'
                  : undefined
            }
          >
            <input
              type="checkbox"
              checked={packState.state === 'full'}
              disabled={packMode === 'locked'}
              // Markup cannot express "some", so the mixed state is applied to
              // the live node. A ref callback rather than an effect: it runs on
              // every render with the current node, so the flag can never lag
              // the derived state by a frame.
              ref={(el) => {
                if (el) el.indeterminate = packState.state === 'partial'
              }}
              // The mode decides the action, not the checkbox's own new value:
              // a gated group that holds packages must empty on click even
              // though clicking an unchecked box reports `checked === true`.
              onChange={() => setGroup(domainPackages, packMode === 'add', pack.domains)}
              className="h-[15px] w-[15px] shrink-0 accent-[#0071c5] disabled:cursor-not-allowed"
            />
            <span className="text-[13px] font-bold text-[#00285a]">Domains</span>
            {pack.description && (
              <span className="truncate text-[11px] text-slate-500">{pack.description}</span>
            )}
          </label>
          <span
            className={
              'shrink-0 text-[11px] ' +
              (packState.selected ? 'font-semibold text-[#0071c5]' : 'text-slate-500')
            }
          >
            {packState.selected
              ? `${packState.selected} of ${packState.total} packages selected`
              : `${packState.total} packages in ${pack.domains.length} domains`}
          </span>
        </div>

        <div
          role="group"
          aria-label={`${pack.displayName} domains`}
          className="grid grid-cols-[repeat(auto-fill,minmax(200px,1fr))] gap-2.5 p-3.5"
        >
          {pack.domains.map((d) => (
            <DomainCard
              key={d.id}
              pack={pack}
              domain={d}
              open={openDomain === d.id}
              onToggleOpen={() => setOpenDomain(openDomain === d.id ? '' : d.id)}
              onSetSelected={(on) => {
                // Ticking a domain also opens it, so the packages it just
                // selected are visible. The pack checkbox above deliberately
                // leaves whatever is open alone.
                if (on) setOpenDomain(d.id)
                setGroup(d.packages, on, [d])
              }}
            />
          ))}
        </div>

        {/* The package list sits below the whole grid, not inside a card: the
            grid wraps to several columns, so an in-card list would shove its
            neighbours around. The open card stays highlighted to tie the two
            together. */}
        {pack.domains
          .filter((d) => d.id === openDomain)
          .map((d) => (
            <div key={d.id} className="border-t border-slate-200 bg-[#fbfdff] px-3.5 pb-3 pt-2.5">
              <div className="mb-1 flex flex-wrap items-baseline gap-2">
                <span className="text-[13px] font-bold text-[#00285a]">{d.displayName}</span>
                {d.description && (
                  <span className="text-[11px] text-slate-500">{d.description}</span>
                )}
              </div>
              {/* Stated up front, because picking a package here switches on a
                  repository the user never asked for. It is not optional — the
                  packages below depend on what it publishes — but it is a change
                  to the template's repository list, so it is said rather than
                  done quietly. */}
              {d.requiresRepos && d.requiresRepos.length > 0 && (
                <p className="mb-1.5 text-[11px] text-slate-500">
                  Also enables {d.requiresRepos.map(repoLabelFor).join(', ')} — where the
                  packages these depend on are published.
                </p>
              )}
              {d.packages.map((p) => (
                <PackageRow
                  key={p.name}
                  name={p.name}
                  version={p.version ?? ''}
                  description={p.description}
                  versions={versionsOf(p, pack.repo)}
                  repoLabelFor={repoLabelFor}
                  selection={addedPackages.find((x) => x.name === p.name)}
                  // Locked only while the package is NOT selected. The gate
                  // stops a package being added without a base runtime; it must
                  // not also trap one that is already selected, or removing the
                  // runtime would leave a row that cannot be unticked here.
                  disabled={
                    !domainSelectable(pack, d, addedPackages) &&
                    !isSelected(addedPackages, p.name)
                  }
                  disabledReason={domainLockReason(pack, d, addedPackages) ?? undefined}
                  onToggle={(checked) => {
                    if (!checked) {
                      removePackage(p.name)
                      return
                    }
                    setPackage({ name: p.name, version: '', repo: pack.repo })
                    ensureReposEnabled([d])
                  }}
                  onChooseVersion={(v) => {
                    // An unresolved package has no repository on its chips, so
                    // fall back to the pack's own — it is where every package
                    // in the pack comes from.
                    setPackage({
                      name: p.name,
                      version: v.version,
                      repo: v.repository || pack.repo,
                    })
                    ensureReposEnabled([d])
                  }}
                />
              ))}
            </div>
          ))}
      </div>

      <p className="mt-2 text-sm text-slate-500">
        Selection is not all-or-none: tick the header checkbox for every domain,
        a domain&apos;s checkbox for everything in that domain, or click a domain
        to see its packages and pick them individually. These are the same
        packages the Repositories tab lists under {repoLabel} — picking one here
        selects it there too, and it appears once in the list on the right.
      </p>
    </>
  )
}

// BaseRuntimeSelector sits above the domain grid rather than inside the pack
// card: it is a prerequisite for every domain, not one more thing the pack
// groups. Each runtime is a plain, independent checkbox — checking or clearing a
// domain never touches it — but the domains stay locked until one is set,
// because a domain's packages need a runtime underneath them.
function BaseRuntimeSelector({ pack }: { pack: EdgePack }) {
  const addedPackages = useStore((s) => s.addedPackages)
  const setPackage = useStore((s) => s.setPackage)
  const removePackage = useStore((s) => s.removePackage)
  const setRepoEnabled = useStore((s) => s.setRepoEnabled)
  const enabledRepos = useStore((s) => s.enabledRepos)

  const anySelected = canSelectDomains(pack, addedPackages)
  // Non-empty only when the gate is shut and packages are selected anyway,
  // which is reachable by clearing a runtime after selecting a domain. Saying
  // "select a runtime to enable selection" there would describe a step the user
  // has already taken and misreport a selection that cannot build as inert.
  const stranded = strandedPackages(pack, addedPackages)

  const toggle = (name: string, on: boolean) => {
    if (!on) {
      removePackage(name)
      return
    }
    setPackage({ name, version: '', repo: pack.repo })
    if (!enabledRepos.includes(pack.repo)) setRepoEnabled(pack.repo, true)
  }

  return (
    <div className="mb-3 rounded-lg border border-slate-200 bg-white px-3.5 py-3">
      <div className="text-[10px] font-bold uppercase tracking-[0.6px] text-[#0071c5]">
        Base Runtime
      </div>
      <div className="mt-1.5 flex flex-wrap gap-2">
        {pack.baseRuntimes.map((r) => {
          const checked = isSelected(addedPackages, r.package.name)
          return (
            <label
              key={r.id}
              title={r.available ? r.package.name : r.unavailableReason}
              className={
                'flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-[13px] ' +
                (!r.available
                  ? 'cursor-not-allowed border-slate-200 text-slate-400'
                  : checked
                    ? 'cursor-pointer border-[#0071c5] bg-[#e6f2fa] font-semibold text-[#0071c5]'
                    : 'cursor-pointer border-slate-300 text-slate-700 hover:border-slate-400')
              }
            >
              <input
                type="checkbox"
                checked={checked}
                disabled={!r.available}
                onChange={(e) => toggle(r.package.name, e.target.checked)}
                className="h-[14px] w-[14px] accent-[#0071c5] disabled:cursor-not-allowed"
              />
              {r.displayName}
              {!r.available && (
                <span className="text-[11px] italic text-slate-400">not yet available</span>
              )}
            </label>
          )
        })}
      </div>
      {/* Stated rather than left to be inferred from the greyed-out domains —
          a locked control with no reason reads as a broken one. */}
      {!anySelected && (
        <p className="mt-1.5 text-[12px] text-amber-700">
          {stranded.length === 0
            ? 'Select a base runtime to enable domain selection below.'
            : `${stranded.length} package${stranded.length === 1 ? '' : 's'} ` +
              `${stranded.length === 1 ? 'is' : 'are'} selected with no base runtime ` +
              `beneath ${stranded.length === 1 ? 'it' : 'them'}, which will not build. ` +
              `Select a runtime above, or clear ${stranded.length === 1 ? 'it' : 'them'} below.`}
        </p>
      )}
    </div>
  )
}

// DomainCard carries two separate affordances: the checkbox selects the
// domain's packages, the card body expands its package list. They are siblings
// rather than nested — the prototype nests the checkbox inside the clickable
// card, which leaves the expand action unreachable by keyboard.
function DomainCard({
  pack,
  domain,
  open,
  onToggleOpen,
  onSetSelected,
}: {
  pack: EdgePack
  domain: EdgePackDomain
  open: boolean
  onToggleOpen: () => void
  onSetSelected: (on: boolean) => void
}) {
  const addedPackages = useStore((s) => s.addedPackages)
  const state = groupSelectionState(domainPackageNames(domain), addedPackages)
  const selectable = domainSelectable(pack, domain, addedPackages)
  const mode = groupToggleMode(selectable, state.state)
  const lockReason = domainLockReason(pack, domain, addedPackages)

  // An unsupported domain says so where the count would go; that is the more
  // useful thing to know about it than how many packages it would have had.
  const count = !domain.available
    ? (domain.unavailableReason ?? 'Not available for this target')
    : state.selected
      ? `${state.selected} of ${state.total} selected`
      : `${state.total} package${state.total === 1 ? '' : 's'}`

  return (
    <div
      role="button"
      tabIndex={0}
      aria-expanded={open}
      title={domain.description}
      onClick={onToggleOpen}
      onKeyDown={(e) => {
        if (e.key !== 'Enter' && e.key !== ' ') return
        e.preventDefault()
        onToggleOpen()
      }}
      className={
        'cursor-pointer rounded-md border px-3 py-2.5 transition-colors focus-visible:outline-2 ' +
        'focus-visible:outline-offset-2 focus-visible:outline-[#0071c5] ' +
        (open
          ? 'border-[#0071c5] bg-[#e6f2fa]'
          : 'border-slate-200 bg-white hover:border-slate-400') +
        (domain.available ? '' : ' opacity-60')
      }
    >
      <div className="flex items-center gap-2">
        <input
          type="checkbox"
          checked={state.state === 'full'}
          disabled={mode === 'locked'}
          ref={(el) => {
            if (el) el.indeterminate = state.state === 'partial'
          }}
          title={
            mode === 'locked'
              ? (lockReason ?? undefined)
              : mode === 'clear' && !selectable
                ? `Clear ${domain.displayName} — selected without a base runtime`
                : undefined
          }
          aria-label={
            mode === 'clear'
              ? `Clear all ${domain.displayName} packages`
              : `Select all ${domain.displayName} packages`
          }
          // The card is clickable, so a click on the checkbox must not also
          // expand the list.
          onClick={(e) => e.stopPropagation()}
          onChange={() => onSetSelected(mode === 'add')}
          className="h-[15px] w-[15px] shrink-0 accent-[#0071c5] disabled:cursor-not-allowed"
        />
        {/* A plain span, not a <label>: clicking the name should open the card,
            which a label would pre-empt by toggling the checkbox instead. */}
        <span
          className={
            'truncate text-[13px] font-semibold ' +
            (open ? 'text-[#0071c5]' : 'text-slate-700')
          }
        >
          {domain.displayName}
        </span>
      </div>
      <div
        className={
          'mt-1.5 pl-[23px] text-[11px] ' +
          (!domain.available
            ? 'text-amber-700'
            : state.selected
              ? 'font-semibold text-[#0071c5]'
              : 'text-slate-500')
        }
      >
        {count}
      </div>
    </div>
  )
}
