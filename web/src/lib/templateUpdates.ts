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
  // versions the checked repositories publish (it comes from a repository the
  // catalog doesn't list), so whether the newest found is newer or older than
  // it can't be told here.
  unverifiable: string[]
}

// planTemplateUpdates decides which of the matched template's own packages
// "Update to latest version" pins, given the newest-version lookup `found`.
// An unpinned entry is pinned to the newest version, as an added "Latest"
// pick is. A pinned entry is overridden only when the checked repositories
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
