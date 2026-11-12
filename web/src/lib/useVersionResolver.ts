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

export function useVersionResolver(os: string, repoIds: string[]) {
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
        if (exact) setLiveResolved((prev) => ({ ...prev, [name]: exact }))
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
  const lookupNewest = (names: string[]): Promise<NewestLookup> => {
    const scope = repoIdsKey
    return Promise.allSettled(names.map((name) => api.searchPackages({ os, repos: repoIds, q: name, limit: 20 }))).then(
      (settled) => {
        const found: Record<string, PackageSearchResult> = {}
        const unavailable = new Set<string>()
        let failed = false
        settled.forEach((s, i) => {
          if (s.status === 'rejected') {
            failed = true
            return
          }
          s.value.unavailableRepos?.forEach((r) => unavailable.add(r))
          const exact = s.value.packages.find((p) => p.name === names[i])
          if (exact) found[exact.name] = exact
        })
        const stale = scopeRef.current !== scope
        const complete = !failed && unavailable.size === 0
        if (complete && !stale) setLiveResolved((prev) => ({ ...prev, ...found }))
        return { found, complete, unavailableRepos: [...unavailable].sort(), stale }
      },
    )
  }

  return { liveResolved, resolvingNames, resolveVersion, resolveAll, bulkResolving, attemptedNames, lookupNewest }
}
