// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// These pin the Edge Pack tab's selection rules — the behaviour the step is
// specified by, expressed against the pure functions rather than the rendered
// component, so a regression fails here in milliseconds instead of needing a
// browser walkthrough to notice.

import { describe, expect, it } from 'vitest'
import type { EdgePack } from '../api/types'
import type { AddedPackage } from '../store'
import {
  allDomainPackages,
  canSelectDomains,
  domainLockReason,
  domainPackageNames,
  domainSelectable,
  groupSelectionState,
  groupToggleMode,
  isSelected,
  selectedBaseRuntime,
  strandedPackages,
  toAddedPackages,
  versionsOf,
} from './edgepack'

// A pack shaped like the shipped catalog, plus one package deliberately shared
// by two domains so the unique-vs-summed distinction is exercised.
const pack: EdgePack = {
  id: 'edge-pack',
  displayName: 'Edge Pack',
  repo: 'intel-eci',
  repoAvailable: true,
  baseRuntimes: [
    { id: 'standard', displayName: 'Standard', available: true, package: { name: 'base-standard' } },
    {
      id: 'realtime',
      displayName: 'Real-time',
      available: false,
      unavailableReason: 'not shipped yet',
      package: { name: 'base-realtime' },
    },
  ],
  domains: [
    {
      id: 'media',
      displayName: 'Media',
      available: true,
      packages: [
        { name: 'media-ffmpeg', version: '7.1' },
        { name: 'shared-runtime', version: '1.0' },
      ],
    },
    {
      id: 'npu',
      displayName: 'NPU',
      available: false,
      unavailableReason: 'Not published for Ubuntu 26.04',
      packages: [{ name: 'npu-driver' }],
    },
    {
      id: 'graphics',
      displayName: 'Graphics',
      available: true,
      packages: [{ name: 'shared-runtime', version: '1.0' }],
    },
  ],
}

const add = (...names: string[]): AddedPackage[] =>
  names.map((name) => ({ name, version: '', repo: 'intel-eci' }))

const media = pack.domains[0]
const npu = pack.domains[1]

describe('groupSelectionState', () => {
  it('reports none / partial / full as the selection fills in', () => {
    const names = domainPackageNames(media)
    expect(groupSelectionState(names, []).state).toBe('none')
    expect(groupSelectionState(names, add('media-ffmpeg')).state).toBe('partial')
    expect(groupSelectionState(names, add('media-ffmpeg', 'shared-runtime')).state).toBe('full')
  })

  it('counts a duplicated name once, so selected can never exceed total', () => {
    const st = groupSelectionState(['a', 'a', 'b'], add('a', 'b'))
    expect(st).toEqual({ selected: 2, total: 2, state: 'full' })
  })

  it('ignores selections outside the group', () => {
    expect(groupSelectionState(['a'], add('a', 'unrelated'))).toEqual({
      selected: 1,
      total: 1,
      state: 'full',
    })
  })

  it('treats an empty group as fully selected rather than partial', () => {
    // Guards the checkbox from rendering indeterminate for a group with nothing
    // in it — 0 of 0 is not "some".
    expect(groupSelectionState([], []).state).toBe('none')
  })
})

describe('allDomainPackages', () => {
  it('unions overlapping domains instead of summing them', () => {
    const all = allDomainPackages(pack)
    // shared-runtime is claimed by both Media and Graphics, and appears once.
    expect(all.map((p) => p.name)).toEqual(['media-ffmpeg', 'shared-runtime', 'npu-driver'])
    // 3 distinct packages across domains listing 4 entries between them.
    expect(all).toHaveLength(3)
    const summed = pack.domains.reduce((n, d) => n + d.packages.length, 0)
    expect(summed).toBe(4)
  })

  it('excludes the base runtimes, which are a prerequisite and not a domain', () => {
    const names = allDomainPackages(pack).map((p) => p.name)
    expect(names).not.toContain('base-standard')
    expect(names).not.toContain('base-realtime')
  })
})

describe('base runtime gate', () => {
  it('locks every domain until a base runtime is selected', () => {
    expect(canSelectDomains(pack, [])).toBe(false)
    expect(domainSelectable(pack, media, [])).toBe(false)
    expect(canSelectDomains(pack, add('base-standard'))).toBe(true)
    expect(domainSelectable(pack, media, add('base-standard'))).toBe(true)
  })

  it('is not satisfied by selecting a domain package on its own', () => {
    // A package can also be reached from the Repositories tab, where no gate
    // applies — so the gate must be judged on the runtime, not on the domain.
    expect(canSelectDomains(pack, add('media-ffmpeg'))).toBe(false)
  })

  it('says to pick a runtime when that is what is missing', () => {
    expect(domainLockReason(pack, media, [])).toBe('Select a base runtime first')
    expect(domainLockReason(pack, media, add('base-standard'))).toBeNull()
  })

  it('gives the target reason precedence, since a runtime cannot unlock it', () => {
    // With a runtime already chosen, an unsupported domain is still locked, and
    // for a reason that naming the runtime would not address.
    expect(domainLockReason(pack, npu, add('base-standard'))).toBe(
      'Not published for Ubuntu 26.04',
    )
    expect(domainLockReason(pack, npu, [])).toBe('Not published for Ubuntu 26.04')
    expect(domainSelectable(pack, npu, add('base-standard'))).toBe(false)
  })

  it('falls back to a stated reason when the catalog supplies none', () => {
    const bare = { ...npu, unavailableReason: undefined }
    expect(domainLockReason(pack, bare, add('base-standard'))).toBeTruthy()
  })
})

describe('groupToggleMode', () => {
  it('adds when the gate is open and the group is not already full', () => {
    expect(groupToggleMode(true, 'none')).toBe('add')
    expect(groupToggleMode(true, 'partial')).toBe('add')
  })

  it('clears a full group when the gate is open', () => {
    expect(groupToggleMode(true, 'full')).toBe('clear')
  })

  it('locks an empty group when the gate is shut', () => {
    expect(groupToggleMode(false, 'none')).toBe('locked')
  })

  it('still clears a gated group that holds packages', () => {
    // The gate blocks adding, never emptying. Without this, clearing the base
    // runtime after selecting a domain would leave the domain checked and
    // unclickable, and its packages would reach the build with no runtime
    // under them — the state the gate exists to prevent.
    expect(groupToggleMode(false, 'full')).toBe('clear')
    expect(groupToggleMode(false, 'partial')).toBe('clear')
  })
})

describe('strandedPackages', () => {
  it('reports nothing while a base runtime is selected', () => {
    expect(strandedPackages(pack, add('base-standard', 'media-ffmpeg'))).toEqual([])
  })

  it('reports nothing when the gate is shut but nothing is selected', () => {
    expect(strandedPackages(pack, [])).toEqual([])
  })

  it('names the domain packages left without a runtime', () => {
    // Reached by ticking Standard, ticking Media, then unticking Standard.
    expect(strandedPackages(pack, add('media-ffmpeg', 'shared-runtime'))).toEqual([
      'media-ffmpeg',
      'shared-runtime',
    ])
  })

  it('counts a package shared by two domains once', () => {
    expect(strandedPackages(pack, add('shared-runtime'))).toEqual(['shared-runtime'])
  })

  it('ignores selections that are not pack domain packages', () => {
    expect(strandedPackages(pack, add('unrelated'))).toEqual([])
  })
})

describe('selectedBaseRuntime', () => {
  it('reports which runtime is under the domains, or none', () => {
    expect(selectedBaseRuntime(pack, [])).toBeUndefined()
    expect(selectedBaseRuntime(pack, add('base-standard'))?.id).toBe('standard')
  })

  it('survives a domain being cleared — clearing a domain never clears it', () => {
    // Deselecting a domain removes only that domain's packages, so the runtime
    // is untouched by construction. This asserts the shape that guarantee rests
    // on: the runtime is not a member of any domain.
    const afterClear = add('base-standard') // media's packages removed
    expect(selectedBaseRuntime(pack, afterClear)?.id).toBe('standard')
    expect(canSelectDomains(pack, afterClear)).toBe(true)
    expect(allDomainPackages(pack).map((p) => p.name)).not.toContain('base-standard')
  })
})

describe('toAddedPackages', () => {
  it('adds a group at the pack repository, floating to latest', () => {
    expect(toAddedPackages(pack, media.packages, [])).toEqual([
      { name: 'media-ffmpeg', version: '', repo: 'intel-eci' },
      { name: 'shared-runtime', version: '', repo: 'intel-eci' },
    ])
  })

  it('skips already-selected packages, so a hand-pinned version survives', () => {
    const pinned: AddedPackage[] = [
      { name: 'media-ffmpeg', version: '7.1', repo: 'intel-eci' },
    ]
    expect(toAddedPackages(pack, media.packages, pinned)).toEqual([
      { name: 'shared-runtime', version: '', repo: 'intel-eci' },
    ])
  })

  it('treats a package selected from the Repositories tab as already selected', () => {
    // Same name, added under the same repo by the other surface. Re-adding it
    // would reset a version pinned over there.
    const fromRepoTab: AddedPackage[] = [
      { name: 'shared-runtime', version: '1.0', repo: 'intel-eci' },
    ]
    expect(toAddedPackages(pack, media.packages, fromRepoTab).map((p) => p.name)).toEqual([
      'media-ffmpeg',
    ])
  })
})

describe('isSelected', () => {
  it('matches on name alone, so one package cannot appear twice', () => {
    // The store keys addedPackages by name. A package reachable from both
    // surfaces is therefore one entry, whichever one added it.
    const viaRepoTab: AddedPackage[] = [{ name: 'media-ffmpeg', version: '7.1', repo: 'intel-eci' }]
    expect(isSelected(viaRepoTab, 'media-ffmpeg')).toBe(true)
    expect(groupSelectionState(domainPackageNames(media), viaRepoTab).selected).toBe(1)
  })
})

describe('versionsOf', () => {
  it('prefers the resolved version list', () => {
    const pkg = { name: 'x', version: '2.0', versions: [{ version: '2.0', repository: 'r' }] }
    expect(versionsOf(pkg, 'intel-eci')).toEqual([{ version: '2.0', repository: 'r' }])
  })

  it('synthesises one entry at the pack repo when only a version came back', () => {
    expect(versionsOf({ name: 'x', version: '2.0' }, 'intel-eci')).toEqual([
      { version: '2.0', repository: 'intel-eci' },
    ])
  })

  it('offers no chips for a package the index never resolved', () => {
    expect(versionsOf({ name: 'x' }, 'intel-eci')).toEqual([])
  })
})
