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

// groupSelectionState counts how many of a group's packages are selected.
// Names are deduplicated first, so a package listed twice within a group (or
// reached through two domains when the caller has merged them) is counted once
// and cannot push `selected` past `total`.
export function groupSelectionState(names: string[], added: AddedPackage[]): GroupSelection {
  const unique = [...new Set(names)]
  const selected = unique.filter((n) => isSelected(added, n)).length
  const state: GroupState = selected === 0 ? 'none' : selected === unique.length ? 'full' : 'partial'
  return { selected, total: unique.length, state }
}

// domainPackageNames is one domain's package names, in catalog order.
export function domainPackageNames(domain: EdgePackDomain): string[] {
  return domain.packages.map((p) => p.name)
}

// allDomainPackages is every package reachable through a domain, deduplicated.
//
// This is the scope of the pack-level checkbox and its count. Domains overlap —
// a package can belong to more than one — so this is a union, never a
// concatenation: summing the domains would report more packages than the pack
// actually contains.
//
// Base runtime packages are deliberately excluded. They are a prerequisite for
// the domains rather than part of them, and the pack checkbox must not select
// or clear a runtime the user chose independently.
export function allDomainPackages(pack: EdgePack): EdgePackPackage[] {
  const seen = new Set<string>()
  const out: EdgePackPackage[] = []
  for (const d of pack.domains) {
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
export function selectedBaseRuntime(pack: EdgePack, added: AddedPackage[]) {
  return pack.baseRuntimes.find((r) => isSelected(added, r.package.name))
}

// canSelectDomains gates every domain checkbox and every drill-in package
// checkbox. A domain's packages need a base runtime beneath them, so until one
// is chosen there is nothing valid to select.
//
// This is one-directional on purpose: choosing a runtime unlocks the domains,
// but clearing a domain never clears the runtime. The runtime is an independent
// choice the user made, not a side effect of the domain selection.
export function canSelectDomains(pack: EdgePack, added: AddedPackage[]): boolean {
  return selectedBaseRuntime(pack, added) != null
}

// domainSelectable reports whether a domain can be selected right now, which
// needs BOTH gates to pass: a base runtime chosen, and the domain published for
// this target. The two are distinct reasons and the UI states them separately —
// see domainLockReason.
export function domainSelectable(
  pack: EdgePack,
  domain: EdgePackDomain,
  added: AddedPackage[],
): boolean {
  return domain.available && canSelectDomains(pack, added)
}

// domainLockReason explains why a domain cannot be selected, or null when it
// can. Target-availability is reported first: it is a property of the target
// that choosing a runtime would not change, so telling the user to pick a
// runtime would send them down a path that cannot unlock this domain.
export function domainLockReason(
  pack: EdgePack,
  domain: EdgePackDomain,
  added: AddedPackage[],
): string | null {
  if (!domain.available) {
    return domain.unavailableReason ?? 'Not published for the selected target'
  }
  if (!canSelectDomains(pack, added)) {
    return 'Select a base runtime first'
  }
  return null
}

// ToggleMode is what a group's checkbox does when clicked right now. 'locked' is
// the only state in which it is disabled.
export type ToggleMode = 'add' | 'clear' | 'locked'

// groupToggleMode decides a group checkbox's action and whether it is operable.
//
// The base-runtime gate blocks ADDING, not clearing. Disabling the checkbox
// outright would strand a selection made before the runtime was removed:
// the domain would render checked and unclickable, and the packages it holds
// would reach the build with no runtime beneath them — precisely the state the
// gate exists to prevent. So a group that holds anything can always be emptied,
// whatever the gate says.
export function groupToggleMode(selectable: boolean, state: GroupState): ToggleMode {
  if (selectable) return state === 'full' ? 'clear' : 'add'
  return state === 'none' ? 'locked' : 'clear'
}

// strandedPackages is every domain package selected with no base runtime under
// it, which is a selection that cannot build. Empty whenever a runtime is
// chosen, so it doubles as the test of whether to warn at all.
export function strandedPackages(pack: EdgePack, added: AddedPackage[]): string[] {
  if (canSelectDomains(pack, added)) return []
  return allDomainPackages(pack)
    .map((p) => p.name)
    .filter((n) => isSelected(added, n))
}

// reposToEnable is every repository id a selection in these domains needs
// enabled: the pack's own, plus whatever the domains declare on top of it.
//
// The pack repo is always included because every pack package resolves from it.
// The extras exist because a domain's metapackage can depend on packages
// published elsewhere — enabling the pack repo alone would produce a template
// that cannot resolve at build time. Deduplicated and in a stable order, so two
// domains naming the same prerequisite enable it once.
export function reposToEnable(pack: EdgePack, domains: EdgePackDomain[]): string[] {
  const ids = [pack.repo]
  for (const d of domains) {
    for (const id of d.requiresRepos ?? []) {
      if (!ids.includes(id)) ids.push(id)
    }
  }
  return ids
}

// toAddedPackages turns pack packages into store records at the repository the
// pack resolves from, skipping any already selected.
//
// Skipping rather than overwriting is what lets a hand-pinned version survive a
// domain-level "select all": re-adding the package would reset it to the
// floating latest, silently discarding a deliberate choice.
export function toAddedPackages(
  pack: EdgePack,
  packages: EdgePackPackage[],
  added: AddedPackage[],
): AddedPackage[] {
  return packages
    .filter((p) => !isSelected(added, p.name))
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
