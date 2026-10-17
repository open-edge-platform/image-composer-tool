// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Selection logic for the Packages step's Edge Pack tab.
//
// Every function here DERIVES its answer from the store's addedPackages list on
// each call; nothing about a domain's or the pack's selection state is stored.
// That is deliberate: Edge Pack and the repository browser are two views of one
// selection, so removing a package from the right-hand rail has to show its
// domain as partial immediately. A cached "this domain is selected" flag would
// be a second copy of the truth, and the two would drift the moment a package
// was removed from the other surface.

import type { EdgePack, EdgePackDomain, EdgePackPackage, PackageVersion } from '../api/types'
import type { AddedPackage } from '../store'
import type { BaseLock } from './baseLock'

// GroupState is a group's selection state. 'partial' is what drives the
// checkbox's indeterminate flag — markup cannot express "some", so it has to be
// set on the DOM node.
export type GroupState = 'none' | 'partial' | 'full'

export interface GroupSelection {
  selected: number
  total: number
  state: GroupState
}

// isSelected reports whether a package name is in the current selection. The
// store keys addedPackages by name, so a package reachable from both the Edge
// Pack tab and the Repositories tab is one entry either way — which is why a
// name is the whole question here, and the repo is not part of it.
export function isSelected(added: AddedPackage[], name: string): boolean {
  return added.some((p) => p.name === name)
}

// isEffectivelySelected is isSelected plus the matched template's own package
// list: a name the template already carries reads as selected here even
// though it was never written into addedPackages — the same "checked because
// the template installs it" treatment PackageRow already gives a single row
// via its `locked` prop (see lib/baseLock.ts).
export function isEffectivelySelected(name: string, added: AddedPackage[], baseLock: BaseLock): boolean {
  return baseLock.info(name).locked || isSelected(added, name)
}

// groupSelectionState counts how many of a group's packages are selected,
// counting a template-locked name (see isEffectivelySelected) the same as an
// explicit pick. Names are deduplicated first, so a package listed twice
// within a group (or reached through two domains when the caller has merged
// them) is counted once and cannot push `selected` past `total`.
export function groupSelectionState(
  names: string[],
  added: AddedPackage[],
  baseLock: BaseLock,
): GroupSelection {
  const unique = [...new Set(names)]
  const selected = unique.filter((n) => isEffectivelySelected(n, added, baseLock)).length
  const state: GroupState = selected === 0 ? 'none' : selected === unique.length ? 'full' : 'partial'
  return { selected, total: unique.length, state }
}

// clearableNames is the subset of `names` an explicit "clear" action can
// actually remove: real addedPackages entries only. A template-locked name is
// excluded — it cannot be unchecked here, exactly as a single PackageRow's
// locked checkbox cannot be unchecked.
export function clearableNames(names: string[], added: AddedPackage[]): string[] {
  return [...new Set(names)].filter((n) => isSelected(added, n))
}

// domainPackageNames is one domain's package names, in catalog order.
export function domainPackageNames(domain: EdgePackDomain): string[] {
  return domain.packages.map((p) => p.name)
}

// allDomainPackages is every package reachable through a domain, deduplicated.
//
// This is what the pack-level checkbox may CLEAR, which is deliberately wider
// than what it may add — see addableDomainPackages. Domains overlap — a package
// can belong to more than one — so this is a union, never a concatenation:
// summing the domains would report more packages than the pack actually
// contains.
//
// Base runtime packages are deliberately excluded. They are a prerequisite for
// the domains rather than part of them, and the pack checkbox must not select
// or clear a runtime the user chose independently.
export function allDomainPackages(pack: EdgePack): EdgePackPackage[] {
  return unionPackages(pack.domains)
}

// addableDomainPackages is every package the pack-level checkbox may ADD: the
// union over the domains this target actually publishes.
//
// An unavailable domain renders its own checkbox locked, so the pack-level
// control must not become a way around that gate — ticking "Domains" would
// otherwise put packages into the template that the domain's own checkbox
// refuses to add, and that no repository offered here publishes.
export function addableDomainPackages(pack: EdgePack): EdgePackPackage[] {
  return unionPackages(pack.domains.filter((d) => d.available))
}

function unionPackages(domains: EdgePackDomain[]): EdgePackPackage[] {
  const seen = new Set<string>()
  const out: EdgePackPackage[] = []
  for (const d of domains) {
    for (const p of d.packages) {
      if (seen.has(p.name)) continue
      seen.add(p.name)
      out.push(p)
    }
  }
  return out
}

// selectedBaseRuntime is whichever base runtime is currently selected, if any.
// Only one is expected at a time in practice, but this returns the first match
// rather than asserting that: the runtimes are ordinary packages, so nothing
// stops the repository browser from adding a second one, and the gate below
// only cares whether some runtime is under the domains.
//
// Unavailable runtimes do not count. They are ordinary packages too, so the
// Repositories tab and the search dropdown can add one even though this tab
// renders its checkbox disabled — and if that satisfied the gate, the
// shown-but-locked Real-time option would unlock every domain from the other
// surface. Availability is a property of the target, so a runtime that is not
// published for it cannot be underneath anything the build produces.
export function selectedBaseRuntime(pack: EdgePack, added: AddedPackage[], baseLock: BaseLock) {
  return pack.baseRuntimes.find((r) => r.available && isEffectivelySelected(r.package.name, added, baseLock))
}

// canSelectDomains gates every domain checkbox and every drill-in package
// checkbox. A domain's packages need a base runtime beneath them, so until one
// is chosen there is nothing valid to select.
//
// This is one-directional on purpose: choosing a runtime unlocks the domains,
// but clearing a domain never clears the runtime. The runtime is an independent
// choice the user made, not a side effect of the domain selection.
export function canSelectDomains(pack: EdgePack, added: AddedPackage[], baseLock: BaseLock): boolean {
  return selectedBaseRuntime(pack, added, baseLock) != null
}

// domainSelectable reports whether a domain can be selected right now, which
// needs BOTH gates to pass: a base runtime chosen, and the domain published for
// this target. The two are distinct reasons and the UI states them separately —
// see domainLockReason.
export function domainSelectable(
  pack: EdgePack,
  domain: EdgePackDomain,
  added: AddedPackage[],
  baseLock: BaseLock,
): boolean {
  return domain.available && canSelectDomains(pack, added, baseLock)
}

// domainLockReason explains why a domain cannot be selected, or null when it
// can. Target-availability is reported first: it is a property of the target
// that choosing a runtime would not change, so telling the user to pick a
// runtime would send them down a path that cannot unlock this domain.
export function domainLockReason(
  pack: EdgePack,
  domain: EdgePackDomain,
  added: AddedPackage[],
  baseLock: BaseLock,
): string | null {
  if (!domain.available) {
    return domain.unavailableReason ?? 'Not published for the selected target'
  }
  if (!canSelectDomains(pack, added, baseLock)) {
    return 'Select a base runtime first'
  }
  return null
}

// ToggleMode is what a group's checkbox does when clicked right now. 'locked'
// covers two different reasons a group cannot be toggled: nothing selected and
// the gate shut, or everything in it already came from the matched template
// (addable and clearable are both empty) — either way there's nothing a click
// could do.
export type ToggleMode = 'add' | 'clear' | 'locked'

// groupToggleMode decides a group checkbox's action and whether it is operable,
// from the two counts the caller already has: `addableCount` (names neither
// template-locked nor already added) and `clearableCount` (names actually in
// addedPackages — see clearableNames). A template-locked-only name is in
// neither set, so a fully-locked group reports 'locked' even though its
// checkbox renders checked.
//
// Adding takes priority while the gate is open and something addable remains —
// an indeterminate group (some names already added, more still addable)
// completes the selection first; clicking again then clears it. Only once
// nothing is left to add does the gate stop mattering: the base-runtime gate
// blocks ADDING, not clearing, so a gated group with anything explicitly
// clearable can still be emptied. Disabling the checkbox outright in that case
// would strand a selection made before the runtime was removed — the domain
// would render checked and unclickable, and the packages it holds would reach
// the build with no runtime beneath them, precisely the state the gate exists
// to prevent.
export function groupToggleMode(
  selectable: boolean,
  addableCount: number,
  clearableCount: number,
): ToggleMode {
  if (selectable && addableCount > 0) return 'add'
  return clearableCount > 0 ? 'clear' : 'locked'
}

// packToggleMode decides the pack-level checkbox's action. It cannot reuse
// groupToggleMode's call signature because adding and clearing have different
// scopes there: the control may only add what the domains' own checkboxes
// would allow (addable), but it must be able to clear anything already
// selected (clearable), which is a wider set — so the caller passes counts
// computed over two different name lists, not one.
//
// The two diverge whenever a domain is unavailable for this target. Its
// packages can still be selected — through the Repositories tab, or before the
// target was changed to one that does not publish the domain — and a control
// that could select them but not deselect them would strand exactly the way
// the domain-level gate exists to prevent. So 'add' is offered only while
// something genuinely addable remains, and 'clear' covers anything explicitly
// in addedPackages across the whole union.
//
// 'locked' therefore means nothing to add AND nothing to remove — including
// when every addable package is already locked in by the matched template.
export function packToggleMode(
  selectable: boolean,
  addableCount: number,
  clearableCount: number,
): ToggleMode {
  if (selectable && addableCount > 0) return 'add'
  return clearableCount > 0 ? 'clear' : 'locked'
}

// strandedPackages is every domain package selected with no base runtime under
// it, which is a selection that cannot build. Empty whenever a runtime is
// chosen, so it doubles as the test of whether to warn at all. Deliberately
// plain isSelected, not isEffectivelySelected: a template-locked package came
// in via the base template, which is already a consistent, buildable state on
// its own, so it is never reported as stranded.
export function strandedPackages(pack: EdgePack, added: AddedPackage[], baseLock: BaseLock): string[] {
  if (canSelectDomains(pack, added, baseLock)) return []
  return allDomainPackages(pack)
    .map((p) => p.name)
    .filter((n) => isSelected(added, n))
}

// reposToEnable is every repository id a selection needs enabled: the pack's
// own, plus whatever the selected parts declare on top of it.
//
// The pack repo is always included because every pack package resolves from it.
// The extras exist because a metapackage can depend on packages published
// elsewhere — enabling the pack repo alone would produce a template that cannot
// resolve at build time. Deduplicated and in a stable order, so two selections
// naming the same prerequisite enable it once.
//
// Domains and base runtimes both declare prerequisites, and neither is more
// authoritative than the other, so this takes anything that carries the field
// rather than one of the two types.
export function reposToEnable(
  pack: EdgePack,
  selected: { requiresRepos?: string[] }[],
): string[] {
  const ids = [pack.repo]
  for (const s of selected) {
    for (const id of s.requiresRepos ?? []) {
      if (!ids.includes(id)) ids.push(id)
    }
  }
  return ids
}

// templateLockedRepos is every repo id the matched template already needs
// because it carries an Edge Pack base runtime or domain package on its own
// — the repo-level counterpart to isEffectivelySelected's package-level
// lock. Reuses reposToEnable so a locked domain's own prerequisites (e.g.
// intel-graphics under Standard) are included, same as an explicit pick.
export function templateLockedRepos(pack: EdgePack, baseLock: BaseLock): string[] {
  const lockedRuntimes = pack.baseRuntimes.filter((r) => baseLock.info(r.package.name).locked)
  const lockedDomains = pack.domains.filter((d) => d.packages.some((p) => baseLock.info(p.name).locked))
  if (lockedRuntimes.length === 0 && lockedDomains.length === 0) return []
  return reposToEnable(pack, [...lockedRuntimes, ...lockedDomains])
}

// toAddedPackages turns pack packages into store records at the repository the
// pack resolves from, skipping any already selected or already carried by the
// matched template.
//
// Skipping an already-selected name is what lets a hand-pinned version survive
// a domain-level "select all": re-adding the package would reset it to the
// floating latest, silently discarding a deliberate choice. Skipping a
// template-locked name keeps it out of addedPackages entirely — writing it in
// would turn an implicit template inclusion into an explicit override the
// user never asked for, and its checkbox is disabled precisely because
// clicking it must not do that.
export function toAddedPackages(
  pack: EdgePack,
  packages: EdgePackPackage[],
  added: AddedPackage[],
  baseLock: BaseLock,
): AddedPackage[] {
  return packages
    .filter((p) => !isEffectivelySelected(p.name, added, baseLock))
    .map((p) => ({ name: p.name, version: '', repo: pack.repo }))
}

// versionsOf normalises a pack package's version list into the shape PackageRow
// renders, mirroring the search dropdown's own versionsOf so a pack package
// offers the same chips as a searched one.
//
// An unresolved package — the pack's repository index was unreachable, or does
// not carry it — yields an empty list, so the row offers no pinnable versions
// rather than a chip for a version nothing confirmed exists. It stays
// selectable at latest, which is what an unpinned pick means anyway.
export function versionsOf(pkg: EdgePackPackage, repo: string): PackageVersion[] {
  if (pkg.versions?.length) return pkg.versions
  if (pkg.version) return [{ version: pkg.version, repository: repo }]
  return []
}
