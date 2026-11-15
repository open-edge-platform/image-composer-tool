// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest'
import { planTemplateUpdates } from './templateUpdates'
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
