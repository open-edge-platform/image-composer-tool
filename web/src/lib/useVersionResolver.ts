// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Real version comparisons for an unpinned row — "what does this actually
// resolve to given the repositories currently checked" — fetched only on
// request rather than eagerly, since a review list can carry a template's
// whole package set and checking every one of them against every checked
// repo just to open the panel would be expensive. Shared by the package
// browser's "Show only selected" view and the Selected rail's "Already in
// template" section (PackageRepoBrowser.tsx and SelectedPackages.tsx).

import { useEffect, useRef, useState } from 'react'
import { api } from '../api/client'
import type { PackageSearchResult } from '../api/types'

export type VersionResolver = ReturnType<typeof useVersionResolver>

// What lookupNewest found. `found` is only safe to act on when `complete`.
export interface NewestLookup {
  found: Record<string, PackageSearchResult>
  // False when a lookup failed or the server couldn't read one of the
  // repos' indexes: the newest version may be in what's missing, so pinning
  // from `found` could pin an older version or even downgrade a pin.
  complete: boolean
  // Repos the server reported unreadable, for the caller's message.
  unavailableRepos: string[]
  // The target or checked repos changed while the lookups were in flight.
  stale: boolean
}

// Repository priority a build applies to a repo that declares none (see
// defaultRepoPriority in internal/api/service/repos.go).
const DEFAULT_REPO_PRIORITY = 500

// buildResolution is what a build installs for an unpinned name, given every
// version the checked repos publish: like apt, the highest-priority
// repository wins, and the newest version within it. `versions` is ordered
// newest first, so the first entry at the highest priority is the answer. A
// repo missing from `priorities` counts as the default.
export function buildResolution(hit: PackageSearchResult, priorities: Record<string, number>): PackageSearchResult {
  const versions = hit.versions?.length ? hit.versions : [{ version: hit.version, repository: hit.repository }]
  const priorityOf = (repo: string) => priorities[repo] ?? DEFAULT_REPO_PRIORITY
  let best = versions[0]
  for (const v of versions) {
    if (priorityOf(v.repository) > priorityOf(best.repository)) best = v
  }
  return { ...hit, version: best.version, repository: best.repository }
}

// priorities maps each checked repo id to its priority, so what a name
// "resolves to" is what a build would install (see buildResolution), not
// merely the newest version any checked repo offers.
export function useVersionResolver(os: string, repoIds: string[], priorities: Record<string, number> = {}) {
  const [liveResolved, setLiveResolved] = useState<Record<string, PackageSearchResult>>({})
  const [resolvingNames, setResolvingNames] = useState<Set<string>>(new Set())
  const [bulkResolving, setBulkResolving] = useState(false)
  // Names a lookup has already been dispatched for in the current repo
  // scope, whether or not it found a match. Without this, a failed/no-match
  // name looks identical to one never tried — "not in liveResolved, not
  // resolving" — and an automatic caller (e.g. SelectedPackages' resolve-on-
  // add effect) would redispatch it on every render indefinitely instead of
  // leaving the manual Resolve action as the only way back in.
  const [attemptedNames, setAttemptedNames] = useState<Set<string>>(new Set())

  // The target is part of the scope too: the same repo ids can carry other
  // versions for another OS, so an answer for one must never land on the other.
  const repoIdsKey = `${os}|${repoIds.join(',')}`
  // Mirrors repoIdsKey for an in-flight request to check against once it
  // settles — state alone can't do this, since an effect's setState calls
  // below are what update it, and a request already waiting on the network
  // needs to compare against whatever scope is current *then*, not whatever
  // it captured at dispatch time.
  const scopeRef = useRef(repoIdsKey)

  // A comparison is only ever a snapshot against the repos checked at the
  // time it was made, so a change to that set invalidates every resolved
  // (or in-flight) name rather than leaving a silently stale comparison on
  // screen.
  useEffect(() => {
    scopeRef.current = repoIdsKey
    setLiveResolved({})
    setResolvingNames(new Set())
    setAttemptedNames(new Set())
    // Unmounting ends the scope too (Next unmounts the Packages step), so an
    // answer still in flight reads as stale. Otherwise it would keep matching
    // the old key and could pin into a repo disabled after coming Back. ''
    // never equals a real key, which always contains '|'.
    return () => {
      scopeRef.current = ''
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- repoIdsKey stands in for repoIds
  }, [repoIdsKey])

  // Returns a promise that settles once the fetch does, so resolveAll (which
  // fires one of these per unresolved name) can wait for all of them rather
  // than just for their synchronous dispatch.
  const resolveVersion = (name: string): Promise<void> => {
    if (name in liveResolved || resolvingNames.has(name)) return Promise.resolve()
    const scope = repoIdsKey
    setResolvingNames((prev) => new Set(prev).add(name))
    return api
      .searchPackages({ os, repos: repoIds, q: name, limit: 20 })
      .then((r) => {
        // The repo scope changed while this was in flight — an answer
        // checked against repos that are no longer the current set would
        // silently overwrite a fresher (or still-pending) result for the
        // same name under the new scope.
        if (scopeRef.current !== scope) return
        const exact = r.packages.find((p) => p.name === name)
        if (exact) setLiveResolved((prev) => ({ ...prev, [name]: buildResolution(exact, priorities) }))
      })
      .catch(() => {
        // Leave it unresolved on failure — resolveVersion's own in-progress
        // guard lets a retry (another click) try again.
      })
      .finally(() => {
        if (scopeRef.current !== scope) return
        setResolvingNames((prev) => {
          const next = new Set(prev)
          next.delete(name)
          return next
        })
        setAttemptedNames((prev) => (prev.has(name) ? prev : new Set(prev).add(name)))
      })
  }

  const resolveAll = (names: string[]) => {
    if (names.length === 0) return
    setBulkResolving(true)
    Promise.allSettled(names.map((name) => resolveVersion(name))).finally(() => {
      setBulkResolving(false)
    })
  }

  // The newest version of each name across the checked repos, for callers
  // that act on the answers rather than just display them — "Update to
  // latest version" turns each one into an exact pin. Unlike resolveVersion
  // this looks up pinned names too, since a pin says nothing about whether a
  // newer version exists. Every name is fetched fresh rather than reused from
  // liveResolved, which is a display snapshot that can be arbitrarily old,
  // and the shared cache is refreshed only from an answer that is complete
  // and still in scope. A name with no exact match is absent from `found`.
  //
  // `within` narrows the lookup to those repos, for "the newest version this
  // one repo offers"; that answer isn't what a name resolves to across the
  // checked repos, so it never refreshes the shared cache.
  const lookupNewest = (names: string[], within?: string[]): Promise<NewestLookup> => {
    const scope = repoIdsKey
    // One request for the whole list (see api.lookupPackages), not one per name.
    return api
      .lookupPackages({ os, repos: within ?? repoIds, names })
      .then(
        (r) => ({ packages: r.packages, unavailableRepos: r.unavailableRepos, failed: false }),
        () => ({ packages: [] as PackageSearchResult[], unavailableRepos: [] as string[], failed: true }),
      )
      .then(({ packages, unavailableRepos, failed }) => {
        const found: Record<string, PackageSearchResult> = {}
        for (const hit of packages) found[hit.name] = hit
        const stale = scopeRef.current !== scope
        const complete = !failed && unavailableRepos.length === 0
        if (complete && !stale && !within) {
          const resolved = Object.fromEntries(Object.entries(found).map(([n, hit]) => [n, buildResolution(hit, priorities)]))
          setLiveResolved((prev) => ({ ...prev, ...resolved }))
        }
        return { found, complete, unavailableRepos, stale }
      })
  }

  return { liveResolved, resolvingNames, resolveVersion, resolveAll, bulkResolving, attemptedNames, lookupNewest }
}
