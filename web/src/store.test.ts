// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useStore, orphanedRepos, confirmRepoRelease } from './store'
import { parseDiskFromYaml } from './lib/disk'
import { minimalPtlPvRaw, roboticsJazzyIso } from './lib/fixtures'

const raw = () => parseDiskFromYaml(minimalPtlPvRaw)!
const iso = () => parseDiskFromYaml(roboticsJazzyIso)!

describe('disk slice', () => {
  beforeEach(() => {
    useStore.setState({ disk: null, diskEdited: false, diskSeed: null })
  })

  it('seeds from a compose until the user edits', () => {
    useStore.getState().seedDisk(raw())
    expect(useStore.getState().disk?.name).toBe('minimal-desktop-ubuntu-ptl-pv')
    expect(useStore.getState().diskEdited).toBe(false)

    useStore.getState().setDisk({ ...raw(), name: 'mine' })
    expect(useStore.getState().diskEdited).toBe(true)

    // A later compose (e.g. the debounced image-name override) must not stomp
    // the user's edit.
    useStore.getState().seedDisk(raw())
    expect(useStore.getState().disk?.name).toBe('mine')
  })

  it('ignores a compose that resolves no disk block', () => {
    useStore.getState().seedDisk(raw())
    useStore.getState().seedDisk(null)
    expect(useStore.getState().disk?.name).toBe('minimal-desktop-ubuntu-ptl-pv')
  })

  // Regression: resetDisk used to clear the model outright. Nothing re-fires a
  // compose on reset, so the step went blank until the selection changed —
  // reproduced in the browser before this was fixed.
  it('restores the seeded layout on reset instead of blanking it', () => {
    useStore.getState().seedDisk(raw())
    useStore.getState().setDisk({ ...raw(), name: 'mine', partitionTableType: 'mbr' })
    useStore.getState().resetDisk()

    const { disk, diskEdited } = useStore.getState()
    expect(disk?.name).toBe('minimal-desktop-ubuntu-ptl-pv')
    expect(disk?.partitionTableType).toBe('gpt')
    expect(diskEdited).toBe(false)
  })

  it('keeps the seed current while the user is editing', () => {
    useStore.getState().seedDisk(raw())
    useStore.getState().setDisk({ ...raw(), name: 'mine' })
    // A fresh compose lands while edited: the visible model stays, but Reset
    // should return to the newest template, not a stale one.
    useStore.getState().seedDisk(iso())
    expect(useStore.getState().disk?.name).toBe('mine')
    useStore.getState().resetDisk()
    expect(useStore.getState().disk?.name).toBe('Default_ISO')
  })

  it('drops the layout when the selection changes to another template', () => {
    useStore.setState({ manifest: null })
    useStore.getState().seedDisk(raw())
    useStore.getState().setDisk({ ...raw(), name: 'mine' })

    useStore.getState().setField('vertical', 'robotics')

    const { disk, diskEdited, diskSeed } = useStore.getState()
    expect(disk).toBeNull()
    expect(diskSeed).toBeNull()
    expect(diskEdited).toBe(false)
  })
})

describe('package slice', () => {
  beforeEach(() => {
    useStore.setState({ addedPackages: [], enabledRepos: [] })
  })

  it('drops a repo once removing a package leaves nothing else depending on it', () => {
    useStore.setState({ enabledRepos: ['repo-a'] })
    useStore.getState().setPackage({ name: 'curl', version: '', repo: 'repo-a' })
    useStore.getState().removePackage('curl')
    expect(useStore.getState().enabledRepos).not.toContain('repo-a')
  })

  it('keeps a repo enabled while another package still depends on it', () => {
    useStore.setState({ enabledRepos: ['repo-a'] })
    useStore.getState().setPackage({ name: 'curl', version: '', repo: 'repo-a' })
    useStore.getState().setPackage({ name: 'wget', version: '', repo: 'repo-a' })
    useStore.getState().removePackage('curl')
    expect(useStore.getState().enabledRepos).toContain('repo-a')
  })

  it('never touches a repo the user enabled that has no packages at all', () => {
    useStore.setState({ enabledRepos: ['repo-a', 'repo-b'] })
    useStore.getState().setPackage({ name: 'curl', version: '', repo: 'repo-a' })
    useStore.getState().removePackage('curl')
    // repo-b never had a package, so removing curl (from repo-a) must not
    // sweep it away too.
    expect(useStore.getState().enabledRepos).toContain('repo-b')
  })

  it('drops the old repo when re-pinning a package moves it to a different one', () => {
    useStore.setState({ enabledRepos: ['repo-a'] })
    useStore.getState().setPackage({ name: 'curl', version: '1.0', repo: 'repo-a' })
    useStore.getState().setPackage({ name: 'curl', version: '2.0', repo: 'repo-b' })
    const { enabledRepos, addedPackages } = useStore.getState()
    expect(enabledRepos).not.toContain('repo-a')
    expect(addedPackages.find((p) => p.name === 'curl')?.repo).toBe('repo-b')
  })

  it('drops every now-unused repo on Clear, but not one with no packages', () => {
    useStore.setState({ enabledRepos: ['repo-a', 'repo-b', 'repo-c'] })
    useStore.getState().setPackage({ name: 'curl', version: '', repo: 'repo-a' })
    useStore.getState().setPackage({ name: 'wget', version: '', repo: 'repo-b' })
    useStore.getState().clearPackages()
    const { enabledRepos, addedPackages } = useStore.getState()
    expect(addedPackages).toHaveLength(0)
    expect(enabledRepos).toEqual(['repo-c'])
  })

  it('drops a repo via bulk removePackages the same way as a single removePackage', () => {
    useStore.setState({ enabledRepos: ['repo-a', 'repo-b'] })
    useStore.getState().setPackages([
      { name: 'curl', version: '', repo: 'repo-a' },
      { name: 'wget', version: '', repo: 'repo-b' },
    ])
    useStore.getState().removePackages(['curl'])
    const { enabledRepos, addedPackages } = useStore.getState()
    expect(enabledRepos).toEqual(['repo-b'])
    expect(addedPackages.map((p) => p.name)).toEqual(['wget'])
  })

  // releaseRepo: false is an explicit escape hatch a caller can pass (e.g.
  // after confirmRepoRelease's prompt was declined) to keep a repo checked
  // even though nothing added still depends on it.
  it('leaves an orphaned repo checked when removePackage passes releaseRepo: false', () => {
    useStore.setState({ enabledRepos: ['repo-a'] })
    useStore.getState().setPackage({ name: 'curl', version: '', repo: 'repo-a' })
    useStore.getState().removePackage('curl', { releaseRepo: false })
    expect(useStore.getState().enabledRepos).toContain('repo-a')
  })

  it('leaves the old repo checked when setPackage re-pins with releaseRepo: false', () => {
    useStore.setState({ enabledRepos: ['repo-a'] })
    useStore.getState().setPackage({ name: 'curl', version: '1.0', repo: 'repo-a' })
    useStore.getState().setPackage({ name: 'curl', version: '2.0', repo: 'repo-b' }, { releaseRepo: false })
    expect(useStore.getState().enabledRepos).toContain('repo-a')
  })

  it('leaves an orphaned repo checked via bulk removePackages with releaseRepo: false', () => {
    useStore.setState({ enabledRepos: ['repo-a', 'repo-b'] })
    useStore.getState().setPackages([
      { name: 'curl', version: '', repo: 'repo-a' },
      { name: 'wget', version: '', repo: 'repo-b' },
    ])
    useStore.getState().removePackages(['curl'], { releaseRepo: false })
    expect(useStore.getState().enabledRepos).toEqual(['repo-a', 'repo-b'])
  })
})

describe('orphanedRepos', () => {
  it('reports a repo whose only dependent is being removed', () => {
    const packages = [{ name: 'curl', version: '', repo: 'repo-a' }]
    expect(orphanedRepos(packages, ['curl'])).toEqual(['repo-a'])
  })

  it('omits a repo another remaining package still needs', () => {
    const packages = [
      { name: 'curl', version: '', repo: 'repo-a' },
      { name: 'wget', version: '', repo: 'repo-a' },
    ]
    expect(orphanedRepos(packages, ['curl'])).toEqual([])
  })

  it('covers every repo orphaned by a batch removal', () => {
    const packages = [
      { name: 'curl', version: '', repo: 'repo-a' },
      { name: 'wget', version: '', repo: 'repo-b' },
      { name: 'vim', version: '', repo: 'repo-c' },
    ]
    expect(orphanedRepos(packages, ['curl', 'wget'])).toEqual(['repo-a', 'repo-b'])
  })
})

describe('confirmRepoRelease', () => {
  const labelFor = (id: string) => id

  it('returns true without prompting when nothing would be orphaned', () => {
    const confirmMock = vi.fn()
    vi.stubGlobal('confirm', confirmMock)
    const packages = [
      { name: 'curl', version: '', repo: 'repo-a' },
      { name: 'wget', version: '', repo: 'repo-a' },
    ]
    expect(confirmRepoRelease(packages, labelFor, ['curl'])).toBe(true)
    expect(confirmMock).not.toHaveBeenCalled()
    vi.unstubAllGlobals()
  })

  it('prompts and returns the user answer when a repo would be orphaned', () => {
    const confirmMock = vi.fn().mockReturnValue(false)
    vi.stubGlobal('confirm', confirmMock)
    const packages = [{ name: 'curl', version: '', repo: 'repo-a' }]
    expect(confirmRepoRelease(packages, labelFor, ['curl'])).toBe(false)
    expect(confirmMock).toHaveBeenCalledOnce()
    vi.unstubAllGlobals()
  })

  // A base (enabledByDefault) repo's checkbox is never actually driven by
  // enabledRepos membership, so asking to "uncheck" it would be both
  // meaningless and confusing — the base template needs it regardless.
  it('never prompts for a repo isBaseRepo reports as always-on', () => {
    const confirmMock = vi.fn()
    vi.stubGlobal('confirm', confirmMock)
    const packages = [{ name: 'ffmpeg', version: '7:6.1.1-3ubuntu5', repo: 'ubuntu-noble-base' }]
    const result = confirmRepoRelease(packages, labelFor, ['ffmpeg'], (repo) => repo === 'ubuntu-noble-base')
    expect(result).toBe(true)
    expect(confirmMock).not.toHaveBeenCalled()
    vi.unstubAllGlobals()
  })
})
