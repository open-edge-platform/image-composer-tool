// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest'
import { keptPinsNotice, pinSources, planTemplateUpdates } from './templateUpdates'
import type { BaseLock } from './baseLock'
import type { PackageSearchResult } from '../api/types'

// A template listing vim unpinned and ffmpeg/curl pinned.
const pinned: Record<string, string> = { ffmpeg: '7:9.0.1-1ppa1', curl: '8.5.0-2' }
const lock: BaseLock = {
  info: (name) =>
    name in pinned
      ? { locked: true, currentVersion: pinned[name], currentIsFloating: false }
      : name === 'vim'
        ? { locked: true, currentIsFloating: true }
        : { locked: false, currentIsFloating: false },
  ensureResolved: () => {},
  concreteNames: ['vim', 'ffmpeg', 'curl'],
}

const hit = (name: string, ...versions: string[]): PackageSearchResult => ({
  name,
  version: versions[0],
  repository: 'base',
  versions: versions.map((version) => ({ version, repository: 'base' })),
})

describe('planTemplateUpdates', () => {
  it('pins an unpinned template package to the newest version', () => {
    const plan = planTemplateUpdates(['vim'], lock, { vim: hit('vim', '2:9.1.0016-1') })
    expect(plan).toEqual({ pins: [{ name: 'vim', version: '2:9.1.0016-1', repo: 'base' }], unverifiable: [] })
  })

  it('bumps a template pin the checked repos publish alongside something newer', () => {
    const plan = planTemplateUpdates(['ffmpeg'], lock, { ffmpeg: hit('ffmpeg', '7:9.1.0-2', '7:9.0.1-1ppa1') })
    expect(plan.pins).toEqual([{ name: 'ffmpeg', version: '7:9.1.0-2', repo: 'base' }])
  })

  it('leaves a template pin that is already the newest', () => {
    const plan = planTemplateUpdates(['ffmpeg'], lock, { ffmpeg: hit('ffmpeg', '7:9.0.1-1ppa1', '7:8.0-1') })
    expect(plan).toEqual({ pins: [], unverifiable: [] })
  })

  it('matches a template pin written without the published epoch', () => {
    const plan = planTemplateUpdates(['curl'], lock, { curl: hit('curl', '1:8.5.0-2') })
    expect(plan).toEqual({ pins: [], unverifiable: [] })
  })

  it('keeps a template pin whose version no checked repo publishes, since it may be newer', () => {
    // Ubuntu's own 9.0.0 could be older than the vendor pin 9.0.1-1ppa1.
    const plan = planTemplateUpdates(['ffmpeg'], lock, { ffmpeg: hit('ffmpeg', '7:9.0.0-1') })
    expect(plan).toEqual({ pins: [], unverifiable: ['ffmpeg'] })
  })

  it('skips a template package no checked repo has, and a name the template does not list', () => {
    const plan = planTemplateUpdates(['vim', 'htop'], lock, { htop: hit('htop', '3.3.0-4') })
    expect(plan).toEqual({ pins: [], unverifiable: [] })
  })
})

describe('pinSources', () => {
  it('groups template pins by the repo publishing the pinned version, unknown under ""', () => {
    const found = {
      ffmpeg: { ...hit('ffmpeg', '7:9.1.0-2'), versions: [{ version: '7:9.0.1-1ppa1', repository: 'snapshot' }] },
      curl: hit('curl', '8.6.0-1'),
    }
    const got = pinSources(['ffmpeg', 'curl', 'vim'], lock, found)
    expect(Object.fromEntries(got)).toEqual({ snapshot: ['ffmpeg'], '': ['curl', 'vim'] })
  })
})

describe('keptPinsNotice', () => {
  const label = (id: string) => ({ base: 'Ubuntu Base', snapshot: 'Snapshot', ppa: 'PPA' })[id] ?? id
  const names = ['a', 'b', 'c', 'd', 'e', 'f', 'g']

  it('names the one repo the pins come from, and says to browse it', () => {
    expect(keptPinsNotice(names, new Map([['snapshot', names]]), 'base', label)).toBe(
      'Kept 7 template pins (a, b, c, d, e and 2 more): their pinned versions come from Snapshot, not Ubuntu Base. Browse Snapshot to update them.',
    )
  })

  it('counts per repo when they come from several, and notes those from none', () => {
    const sources = new Map([
      ['snapshot', ['a', 'b']],
      ['ppa', ['c']],
      ['', ['d']],
    ])
    expect(keptPinsNotice(['a', 'b', 'c', 'd'], sources, 'base', label)).toBe(
      'Kept 4 template pins (a, b, c, d): their pinned versions come from Snapshot (2) and PPA (1), not Ubuntu Base. Browse those repositories to update them. 1 of them pins a version no listed repository publishes, so it can\'t be updated here.',
    )
  })

  it('says so when no listed repo publishes the pinned version', () => {
    expect(keptPinsNotice(['a'], new Map([['', ['a']]]), 'base', label)).toBe(
      "Kept 1 template pin (a): no listed repository publishes its pinned version, so it can't be updated here.",
    )
  })
})
