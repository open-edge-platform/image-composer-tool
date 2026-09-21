// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Shared logic behind "is this row already in the matched template, and at
// what version" — used by both the cross-repository search dropdown and the
// repository browser's merged pane (PackagesStep.tsx and
// PackageRepoBrowser.tsx), so it lives here rather than in either component
// to avoid the two importing from one another.

import { useEffect, useMemo, useRef, useState } from 'react'
import { api } from '../api/client'
import type { PackageRepo } from '../api/types'

export interface BaseLockInfo {
  locked: boolean
  // The version already in effect from the template itself: either its own
  // explicit pin, or — for an unpinned entry — the version the target's
  // default repo publishes, once resolved. Undefined while unresolved or if
  // the default repo doesn't carry the name at all (e.g. a package the
  // template only reaches through a non-default repo it bundles).
  currentVersion?: string
  // True when the template's own entry is unpinned, meaning "Latest" is
  // just as redundant a pick as currentVersion's own chip would be — the
  // template already floats to whatever's newest.
  currentIsFloating: boolean
}

export interface BaseLock {
  info: (name: string) => BaseLockInfo
  // Kicks off a lookup for an unpinned locked name's default-repo version if
  // one isn't already resolved or in flight. Safe to call redundantly.
  ensureResolved: (name: string) => void
  // Every concrete (non-glob) name the template's own package list carries,
  // pinned or bare. A glob entry (e.g. "libva*") isn't included — there's no
  // way to enumerate its matches without a full catalog scan, which is
  // exactly the cost the "review what's already in the template" list
  // (PackageRepoBrowser's "Show only selected") is meant to avoid.
  concreteNames: string[]
}

// useBaseLock answers "is this row already in the matched template, and at
// what version" against ComposeResponse.basePackages. A pinned entry
// (`name_version`) answers instantly. An unpinned entry only says *that* the
// template carries it — which repo it actually resolves from can depend on
// which optional repos are enabled (a higher-priority repo publishing a
// newer version silently wins), so this deliberately does not try to
// replicate that. Instead it reports the version the target's own default
// repo publishes, which is what the template resolves to before any
// optional repo is added — a name the template only reaches through a
// non-default repo it bundles (never surfaced by the picker without that
// repo checked) simply has no currentVersion. This is enough to mark the
// right chip disabled without reimplementing priority-based resolution.
export function useBaseLock(os: string, repos: PackageRepo[] | null, basePackages: string[]): BaseLock {
  const defaultRepoId = repos?.find((r) => r.enabledByDefault)?.id

  const { pinned, bareNames, globs } = useMemo(() => {
    const pinned = new Map<string, string>()
    const bareNames = new Set<string>()
    const globs: string[] = []
    for (const entry of basePackages) {
      if (isGlobPattern(entry)) {
        globs.push(entry)
        continue
      }
      const sep = entry.indexOf('_')
      if (sep === -1) {
        bareNames.add(entry)
        continue
      }
      // name_version, matching the pin convention encodePackage (store.ts)
      // also uses.
      pinned.set(entry.slice(0, sep), entry.slice(sep + 1))
    }
    return { pinned, bareNames, globs }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- join() stands in for the array reference, which is new on every compose response
  }, [basePackages.join(',')])

  // Default-repo version lookups for unpinned locked names, keyed by name.
  // null means "looked up, the default repo doesn't carry it".
  const [resolved, setResolved] = useState<Record<string, string | null>>({})
  const pending = useRef(new Set<string>())

  // A different target means a different default repo — whatever was
  // resolved against the previous one no longer applies.
  useEffect(() => {
    setResolved({})
    pending.current.clear()
  }, [os, defaultRepoId])

  const info = (name: string): BaseLockInfo => {
    const pinnedVersion = pinned.get(name)
    if (pinnedVersion !== undefined) {
      return { locked: true, currentVersion: pinnedVersion, currentIsFloating: false }
    }
    if (bareNames.has(name) || globs.some((g) => globMatch(g, name))) {
      return { locked: true, currentVersion: resolved[name] ?? undefined, currentIsFloating: true }
    }
    return { locked: false, currentIsFloating: false }
  }

  const ensureResolved = (name: string) => {
    if (!defaultRepoId || name.length < 2) return
    if (name in resolved || pending.current.has(name)) return
    pending.current.add(name)
    api
      .searchPackages({ os, repos: [defaultRepoId], q: name, limit: 20 })
      .then((r) => {
        const exact = r.packages.find((p) => p.name === name)
        setResolved((prev) => ({ ...prev, [name]: exact ? exact.version : null }))
      })
      .catch(() => {
        setResolved((prev) => ({ ...prev, [name]: null }))
      })
      .finally(() => {
        pending.current.delete(name)
      })
  }

  const concreteNames = useMemo(
    () => [...pinned.keys(), ...bareNames],
    [pinned, bareNames],
  )

  return { info, ensureResolved, concreteNames }
}

// ensureBaseVersions kicks off a default-repo lookup for every rendered row
// that's locked to an unpinned template entry and not yet resolved — called
// from a useEffect so it runs after render, not during it.
export function ensureBaseVersions(baseLock: BaseLock, names: string[]): void {
  for (const name of names) {
    const info = baseLock.info(name)
    if (info.locked && info.currentIsFloating && info.currentVersion === undefined) {
      baseLock.ensureResolved(name)
    }
  }
}

function isGlobPattern(s: string): boolean {
  return s.includes('*') || s.includes('?') || s.includes('[')
}

function globMatch(pattern: string, name: string): boolean {
  const escaped = pattern.replace(/[.+^${}()|\\]/g, '\\$&')
  const regexSource = escaped.replace(/\*/g, '.*').replace(/\?/g, '.')
  return new RegExp(`^${regexSource}$`).test(name)
}
