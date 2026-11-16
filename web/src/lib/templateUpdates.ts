// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import type { PackageSearchResult } from '../api/types'
import type { AddedPackage } from '../store'
import type { BaseLock } from './baseLock'

// The most packages a compose request accepts (maxPackages in
// internal/api/service/delta.go, maxItems in the OpenAPI spec).
export const MAX_SELECTED_PACKAGES = 256

export interface TemplateUpdatePlan {
  // Overrides to add, each pinning a template package to its newest version.
  pins: AddedPackage[]
  // Template pins kept as they are: the pinned version isn't among the
  // versions the looked-up repositories publish (it comes from another
  // repository), so whether the newest found is newer or older than it can't
  // be told here.
  unverifiable: string[]
}

// planTemplateUpdates decides which of the matched template's own packages
// "Update to latest version" pins, given the newest-version lookup `found`.
// An unpinned entry is pinned to the newest version, as an added "Latest"
// pick is. A pinned entry is overridden only when the looked-up repositories
// publish its pinned version and something newer: `versions` is ordered
// newest first by the server's own version rules, so the pin being listed
// means the first entry is not older than it.
export function planTemplateUpdates(
  names: string[],
  baseLock: BaseLock,
  found: Record<string, PackageSearchResult>,
): TemplateUpdatePlan {
  const pins: AddedPackage[] = []
  const unverifiable: string[] = []
  for (const name of names) {
    const hit = found[name]
    const info = baseLock.info(name)
    if (!hit || !info.locked) continue
    const newest = { name, version: hit.version, repo: hit.repository }
    if (info.currentIsFloating) {
      pins.push(newest)
      continue
    }
    const pinned = info.currentVersion
    if (pinned === undefined || sameVersion(hit.version, pinned)) continue
    const versions = hit.versions ?? [{ version: hit.version, repository: hit.repository }]
    if (versions.some((v) => sameVersion(v.version, pinned))) {
      pins.push(newest)
    } else {
      unverifiable.push(name)
    }
  }
  return { pins, unverifiable }
}

// A template may pin a version without the epoch the repository publishes
// (curl_8.5.0-2 for 1:8.5.0-2); the resolvers accept that, so it matches here.
function sameVersion(published: string, pinned: string): boolean {
  return published === pinned || published.replace(/^\d+:/, '') === pinned.replace(/^\d+:/, '')
}

// pinSources groups template pins by the repository that publishes the
// pinned version, given a lookup across the other repositories. Where several
// publish it, the one with the highest priority (as `priorities` gives them,
// a build's own tie-break) is credited. A name whose pinned version none of
// them publishes is grouped under ''.
export function pinSources(
  names: string[],
  baseLock: BaseLock,
  found: Record<string, PackageSearchResult>,
  priorities: Record<string, number> = {},
): Map<string, string[]> {
  const priorityOf = (repo: string) => priorities[repo] ?? 500
  const out = new Map<string, string[]>()
  for (const name of names) {
    const pinned = baseLock.info(name).currentVersion
    const hit = found[name]
    const versions = hit?.versions ?? (hit ? [{ version: hit.version, repository: hit.repository }] : [])
    const matches = pinned === undefined ? [] : versions.filter((v) => sameVersion(v.version, pinned))
    const source = matches.reduce<(typeof matches)[number] | undefined>(
      (best, v) => (!best || priorityOf(v.repository) > priorityOf(best.repository) ? v : best),
      undefined,
    )
    const repo = source?.repository ?? ''
    out.set(repo, [...(out.get(repo) ?? []), name])
  }
  return out
}

// pinnedElsewhere names the template pins among `rows`, the browsed repo's
// own entries for template packages, whose pinned version that repo doesn't
// publish. "Update to latest version" leaves them as they are there, since
// whether the repo's newest is newer or older than the pin can't be told.
export function pinnedElsewhere(rows: PackageSearchResult[], baseLock: BaseLock): string[] {
  return rows
    .filter((row) => {
      const info = baseLock.info(row.name)
      const pinned = info.currentVersion
      if (!info.locked || info.currentIsFloating || pinned === undefined) return false
      const versions = row.versions?.length ? row.versions : [{ version: row.version, repository: row.repository }]
      return !versions.some((v) => sameVersion(v.version, pinned))
    })
    .map((row) => row.name)
}

// pinnedElsewhereLabel is the short note beside "Update to latest version"
// for those pins: how many, and where from when that is one repository.
// `sources` is pinSources' answer, or undefined while it is still looked up.
export function pinnedElsewhereLabel(
  count: number,
  sources: Map<string, string[]> | undefined,
  repoLabelFor: (id: string) => string,
): string {
  const known = sources ? [...sources.keys()].filter((repo) => repo !== '') : []
  if (sources && known.length === 1 && !sources.has('')) return `${count} pinned from ${repoLabelFor(known[0])}`
  return known.length > 1 ? `${count} pinned from other repositories` : `${count} pinned from another repository`
}

// pinnedElsewhereNotice says which template pins "Update to latest version"
// doesn't update while browsing `browsed`, where their pinned versions come
// from, and so where to go to update them.
export function pinnedElsewhereNotice(
  names: string[],
  sources: Map<string, string[]> | undefined,
  browsed: string,
  repoLabelFor: (id: string) => string,
): string {
  const one = names.length === 1
  const list = names.slice(0, 5).join(', ') + (names.length > 5 ? ` and ${names.length - 5} more` : '')
  const head = `${names.length} template ${one ? 'pin' : 'pins'} (${list}) ${one ? "isn't" : "aren't"} updated here`
  const versions = one ? 'its pinned version' : 'their pinned versions'
  if (!sources) return `${head}: ${repoLabelFor(browsed)} doesn't publish ${versions}.`
  const known = [...sources].filter(([repo]) => repo !== '')
  const unknown = sources.get('')?.length ?? 0
  if (known.length === 0) return `${head}: no repository in use publishes ${versions}.`
  const from =
    known.length === 1
      ? repoLabelFor(known[0][0])
      : known.map(([repo, n]) => `${repoLabelFor(repo)} (${n.length})`).join(' and ')
  const where = known.length === 1 ? from : 'those repositories'
  let msg = `${head}: ${one ? 'its pinned version comes' : 'their pinned versions come'} from ${from}, not ${repoLabelFor(browsed)}. Browse ${where} to update ${one ? 'it' : 'them'}.`
  if (unknown > 0) {
    msg += ` ${unknown} of them ${unknown === 1 ? 'pins a version' : 'pin versions'} no repository in use publishes.`
  }
  return msg
}
