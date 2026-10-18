// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Real version comparisons for an unpinned row — "what does this actually
// resolve to given the repositories currently checked" — fetched only on
// request rather than eagerly, since a review list can carry a template's
// whole package set and checking every one of them against every checked
// repo just to open the panel would be expensive. Shared by the package
// browser's "Show only selected" view and the Selected rail's "Already in
// template" section (PackageRepoBrowser.tsx and SelectedPackages.tsx).

import { useEffect, useState } from 'react'
import { api } from '../api/client'
import type { PackageSearchResult } from '../api/types'

export function useVersionResolver(os: string, repoIds: string[]) {
  const [liveResolved, setLiveResolved] = useState<Record<string, PackageSearchResult>>({})
  const [resolvingNames, setResolvingNames] = useState<Set<string>>(new Set())
  const [bulkResolving, setBulkResolving] = useState(false)

  const repoIdsKey = repoIds.join(',')

  // A comparison is only ever a snapshot against the repos checked at the
  // time it was made, so a change to that set invalidates every resolved
  // (or in-flight) name rather than leaving a silently stale comparison on
  // screen.
  useEffect(() => {
    setLiveResolved({})
    setResolvingNames(new Set())
    // eslint-disable-next-line react-hooks/exhaustive-deps -- repoIdsKey stands in for repoIds
  }, [repoIdsKey])

  // Returns a promise that settles once the fetch does, so resolveAll (which
  // fires one of these per unresolved name) can wait for all of them rather
  // than just for their synchronous dispatch.
  const resolveVersion = (name: string): Promise<void> => {
    if (name in liveResolved || resolvingNames.has(name)) return Promise.resolve()
    setResolvingNames((prev) => new Set(prev).add(name))
    return api
      .searchPackages({ os, repos: repoIds, q: name, limit: 20 })
      .then((r) => {
        const exact = r.packages.find((p) => p.name === name)
        if (exact) setLiveResolved((prev) => ({ ...prev, [name]: exact }))
      })
      .catch(() => {
        // Leave it unresolved on failure — resolveVersion's own in-progress
        // guard lets a retry (another click) try again.
      })
      .finally(() => {
        setResolvingNames((prev) => {
          const next = new Set(prev)
          next.delete(name)
          return next
        })
      })
  }

  const resolveAll = (names: string[]) => {
    if (names.length === 0) return
    setBulkResolving(true)
    Promise.allSettled(names.map((name) => resolveVersion(name))).finally(() => {
      setBulkResolving(false)
    })
  }

  return { liveResolved, resolvingNames, resolveVersion, resolveAll, bulkResolving }
}
