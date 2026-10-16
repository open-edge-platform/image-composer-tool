// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// These pin the Edge Pack tab's selection rules — the behaviour the step is
// specified by, expressed against the pure functions rather than the rendered
// component, so a regression fails here in milliseconds instead of needing a
// browser walkthrough to notice.

import { describe, expect, it } from 'vitest'
import type { EdgePack } from '../api/types'
import type { AddedPackage } from '../store'
import type { BaseLock, BaseLockInfo } from './baseLock'
import {
  addableDomainPackages,
  allDomainPackages,
  canSelectDomains,
  clearableNames,
  domainLockReason,
  domainPackageNames,
  domainSelectable,
  groupSelectionState,
  groupToggleMode,
  isSelected,
  packToggleMode,
  reposToEnable,
  selectedBaseRuntime,
  strandedPackages,
  templateLockedRepos,
  toAddedPackages,
  versionsOf,
} from './edgepack'

// A BaseLock that locks nothing — the default for tests exercising ordinary,
// non-template-matched selection.
const noLock: BaseLock = {
  info: () => ({ locked: false, currentIsFloating: false }),
  ensureResolved: () => {},
  concreteNames: [],
}

// Builds a BaseLock that locks exactly the given names, as if the matched
// template carried them unpinned.
const lockOf = (...names: string[]): BaseLock => ({
  info: (name): BaseLockInfo =>
    names.includes(name) ? { locked: true, currentIsFloating: true } : { locked: false, currentIsFloating: false },
  ensureResolved: () => {},
  concreteNames: names,
})

// A pack shaped like the shipped catalog, plus one package deliberately shared
// by two domains so the unique-vs-summed distinction is exercised.
const pack: EdgePack = {
  id: 'edge-pack',
  displayName: 'Edge Pack',
  repo: 'intel-eci',
  repoAvailable: true,
  baseRuntimes: [
    {
      id: 'standard',
      displayName: 'Standard',
      available: true,
      // As in the shipped catalog: the base metapackage pulls in the NPU
      // profile, whose dependencies the pack repository does not carry.
      requiresRepos: ['intel-graphics'],
      package: { name: 'base-standard' },
    },
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
      requiresRepos: ['intel-graphics'],
      packages: [{ name: 'npu-driver' }],
    },
    {
      // Names the same prerequisite as NPU, so the dedup is exercised.
      id: 'graphics',
      displayName: 'Graphics',
      available: true,
      requiresRepos: ['intel-graphics'],
      packages: [{ name: 'shared-runtime', version: '1.0' }],
    },
  ],
}

const add = (...names: string[]): AddedPackage[] =>
  names.map((name) => ({ name, version: '', repo: 'intel-eci' }))

const media = pack.domains[0]
const npu = pack.domains[1]
const standard = pack.baseRuntimes[0]

describe('groupSelectionState', () => {
  it('reports none / partial / full as the selection fills in', () => {
    const names = domainPackageNames(media)
    expect(groupSelectionState(names, [], noLock).state).toBe('none')
    expect(groupSelectionState(names, add('media-ffmpeg'), noLock).state).toBe('partial')
    expect(groupSelectionState(names, add('media-ffmpeg', 'shared-runtime'), noLock).state).toBe('full')
  })

  it('counts a duplicated name once, so selected can never exceed total', () => {
    const st = groupSelectionState(['a', 'a', 'b'], add('a', 'b'), noLock)
    expect(st).toEqual({ selected: 2, total: 2, state: 'full' })
  })

  it('ignores selections outside the group', () => {
    expect(groupSelectionState(['a'], add('a', 'unrelated'), noLock)).toEqual({
      selected: 1,
      total: 1,
      state: 'full',
    })
  })

  it('treats an empty group as fully selected rather than partial', () => {
    // Guards the checkbox from rendering indeterminate for a group with nothing
    // in it — 0 of 0 is not "some".
    expect(groupSelectionState([], [], noLock).state).toBe('none')
  })

  it('counts a template-locked name as selected with nothing in addedPackages', () => {
    const names = domainPackageNames(media)
    expect(groupSelectionState(names, [], lockOf('media-ffmpeg')).state).toBe('partial')
    expect(
      groupSelectionState(names, [], lockOf('media-ffmpeg', 'shared-runtime')).state,
    ).toBe('full')
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
    expect(canSelectDomains(pack, [], noLock)).toBe(false)
    expect(domainSelectable(pack, media, [], noLock)).toBe(false)
    expect(canSelectDomains(pack, add('base-standard'), noLock)).toBe(true)
    expect(domainSelectable(pack, media, add('base-standard'), noLock)).toBe(true)
  })

  it('is not satisfied by selecting a domain package on its own', () => {
    // A package can also be reached from the Repositories tab, where no gate
    // applies — so the gate must be judged on the runtime, not on the domain.
    expect(canSelectDomains(pack, add('media-ffmpeg'), noLock)).toBe(false)
  })

  it('says to pick a runtime when that is what is missing', () => {
    expect(domainLockReason(pack, media, [], noLock)).toBe('Select a base runtime first')
    expect(domainLockReason(pack, media, add('base-standard'), noLock)).toBeNull()
  })

  it('gives the target reason precedence, since a runtime cannot unlock it', () => {
    // With a runtime already chosen, an unsupported domain is still locked, and
    // for a reason that naming the runtime would not address.
    expect(domainLockReason(pack, npu, add('base-standard'), noLock)).toBe(
      'Not published for Ubuntu 26.04',
    )
    expect(domainLockReason(pack, npu, [], noLock)).toBe('Not published for Ubuntu 26.04')
    expect(domainSelectable(pack, npu, add('base-standard'), noLock)).toBe(false)
  })

  it('falls back to a stated reason when the catalog supplies none', () => {
    const bare = { ...npu, unavailableReason: undefined }
    expect(domainLockReason(pack, bare, add('base-standard'), noLock)).toBeTruthy()
  })

  it('unlocks domains when the base runtime is locked in by the template alone', () => {
    expect(canSelectDomains(pack, [], lockOf('base-standard'))).toBe(true)
    expect(domainSelectable(pack, media, [], lockOf('base-standard'))).toBe(true)
  })
})

describe('groupToggleMode', () => {
  it('adds when the gate is open and something addable remains', () => {
    expect(groupToggleMode(true, 2, 0)).toBe('add')
    expect(groupToggleMode(true, 1, 0)).toBe('add')
  })

  it('clears a group with anything explicitly added, even if also addable', () => {
    expect(groupToggleMode(true, 0, 2)).toBe('clear')
  })

  it('locks an empty group when the gate is shut', () => {
    expect(groupToggleMode(false, 0, 0)).toBe('locked')
  })

  it('still clears a gated group that holds packages', () => {
    // The gate blocks adding, never emptying. Without this, clearing the base
    // runtime after selecting a domain would leave the domain checked and
    // unclickable, and its packages would reach the build with no runtime
    // under them — the state the gate exists to prevent.
    expect(groupToggleMode(false, 0, 2)).toBe('clear')
    expect(groupToggleMode(false, 1, 1)).toBe('clear')
  })

  it('locks a group fully covered by the template, with nothing explicit to clear', () => {
    // Reached when every name in the group is already in basePackages: there is
    // nothing left to add (all locked) and nothing explicitly in addedPackages
    // to clear — the checkbox renders checked but inert, same as a single
    // locked PackageRow.
    expect(groupToggleMode(true, 0, 0)).toBe('locked')
  })
})

describe('strandedPackages', () => {
  it('reports nothing while a base runtime is selected', () => {
    expect(strandedPackages(pack, add('base-standard', 'media-ffmpeg'), noLock)).toEqual([])
  })

  it('reports nothing when the gate is shut but nothing is selected', () => {
    expect(strandedPackages(pack, [], noLock)).toEqual([])
  })

  it('names the domain packages left without a runtime', () => {
    // Reached by ticking Standard, ticking Media, then unticking Standard.
    expect(strandedPackages(pack, add('media-ffmpeg', 'shared-runtime'), noLock)).toEqual([
      'media-ffmpeg',
      'shared-runtime',
    ])
  })

  it('counts a package shared by two domains once', () => {
    expect(strandedPackages(pack, add('shared-runtime'), noLock)).toEqual(['shared-runtime'])
  })

  it('ignores selections that are not pack domain packages', () => {
    expect(strandedPackages(pack, add('unrelated'), noLock)).toEqual([])
  })

  it('reports nothing when the runtime is locked in by the template alone', () => {
    expect(strandedPackages(pack, add('media-ffmpeg'), lockOf('base-standard'))).toEqual([])
  })

  it('never reports a template-locked package as stranded, even with the gate shut', () => {
    expect(strandedPackages(pack, [], lockOf('media-ffmpeg'))).toEqual([])
  })
})

describe('reposToEnable', () => {
  it('always includes the pack repo, which every package resolves from', () => {
    expect(reposToEnable(pack, [media])).toEqual(['intel-eci'])
  })

  it('adds a domain prerequisite, so its dependencies can resolve', () => {
    // Without this the NPU metapackage would be selected with only the pack
    // repository enabled, and the build would fail resolving its dependencies.
    expect(reposToEnable(pack, [npu])).toEqual(['intel-eci', 'intel-graphics'])
  })

  it('names a shared prerequisite once', () => {
    expect(reposToEnable(pack, pack.domains)).toEqual(['intel-eci', 'intel-graphics'])
  })

  it('adds a base runtime prerequisite, which no domain need be selected for', () => {
    // The standard base metapackage pulls in the NPU, IPU and
    // compute-essentials profiles itself, so a template that selects nothing
    // but the runtime still needs the repository their dependencies come from.
    expect(reposToEnable(pack, [standard])).toEqual(['intel-eci', 'intel-graphics'])
  })

  it('names a prerequisite shared by a runtime and a domain once', () => {
    expect(reposToEnable(pack, [standard, ...pack.domains])).toEqual([
      'intel-eci',
      'intel-graphics',
    ])
  })

  it('needs nothing beyond the pack repo for no selection at all', () => {
    expect(reposToEnable(pack, [])).toEqual(['intel-eci'])
  })
})

describe('templateLockedRepos', () => {
  it('reports nothing when the template carries none of this pack’s packages', () => {
    expect(templateLockedRepos(pack, noLock)).toEqual([])
  })

  it('includes the pack repo for a locked domain package', () => {
    expect(templateLockedRepos(pack, lockOf('media-ffmpeg'))).toEqual(['intel-eci'])
  })

  it('includes a locked domain’s own prerequisite, same as an explicit pick', () => {
    // shared-runtime belongs to NPU (unavailable, requires intel-graphics) and
    // Graphics (available, requires intel-graphics) — either locks the same
    // prerequisite.
    expect(templateLockedRepos(pack, lockOf('shared-runtime'))).toEqual(['intel-eci', 'intel-graphics'])
  })

  it('includes a locked base runtime’s prerequisite', () => {
    expect(templateLockedRepos(pack, lockOf('base-standard'))).toEqual(['intel-eci', 'intel-graphics'])
  })

  it('does not double-count a prerequisite shared by a locked runtime and a locked domain', () => {
    expect(templateLockedRepos(pack, lockOf('base-standard', 'media-ffmpeg'))).toEqual([
      'intel-eci',
      'intel-graphics',
    ])
  })
})

describe('selectedBaseRuntime', () => {
  it('reports which runtime is under the domains, or none', () => {
    expect(selectedBaseRuntime(pack, [], noLock)).toBeUndefined()
    expect(selectedBaseRuntime(pack, add('base-standard'), noLock)?.id).toBe('standard')
  })

  it('survives a domain being cleared — clearing a domain never clears it', () => {
    // Deselecting a domain removes only that domain's packages, so the runtime
    // is untouched by construction. This asserts the shape that guarantee rests
    // on: the runtime is not a member of any domain.
    const afterClear = add('base-standard') // media's packages removed
    expect(selectedBaseRuntime(pack, afterClear, noLock)?.id).toBe('standard')
    expect(canSelectDomains(pack, afterClear, noLock)).toBe(true)
    expect(allDomainPackages(pack).map((p) => p.name)).not.toContain('base-standard')
  })

  it('counts a runtime locked in by the template alone', () => {
    expect(selectedBaseRuntime(pack, [], lockOf('base-standard'))?.id).toBe('standard')
  })
})

describe('toAddedPackages', () => {
  it('adds a group at the pack repository, floating to latest', () => {
    expect(toAddedPackages(pack, media.packages, [], noLock)).toEqual([
      { name: 'media-ffmpeg', version: '', repo: 'intel-eci' },
      { name: 'shared-runtime', version: '', repo: 'intel-eci' },
    ])
  })

  it('skips already-selected packages, so a hand-pinned version survives', () => {
    const pinned: AddedPackage[] = [
      { name: 'media-ffmpeg', version: '7.1', repo: 'intel-eci' },
    ]
    expect(toAddedPackages(pack, media.packages, pinned, noLock)).toEqual([
      { name: 'shared-runtime', version: '', repo: 'intel-eci' },
    ])
  })

  it('treats a package selected from the Repositories tab as already selected', () => {
    // Same name, added under the same repo by the other surface. Re-adding it
    // would reset a version pinned over there.
    const fromRepoTab: AddedPackage[] = [
      { name: 'shared-runtime', version: '1.0', repo: 'intel-eci' },
    ]
    expect(toAddedPackages(pack, media.packages, fromRepoTab, noLock).map((p) => p.name)).toEqual([
      'media-ffmpeg',
    ])
  })

  it('skips a package already carried by the matched template', () => {
    expect(toAddedPackages(pack, media.packages, [], lockOf('media-ffmpeg')).map((p) => p.name)).toEqual([
      'shared-runtime',
    ])
  })
})

describe('isSelected', () => {
  it('matches on name alone, so one package cannot appear twice', () => {
    // The store keys addedPackages by name. A package reachable from both
    // surfaces is therefore one entry, whichever one added it.
    const viaRepoTab: AddedPackage[] = [{ name: 'media-ffmpeg', version: '7.1', repo: 'intel-eci' }]
    expect(isSelected(viaRepoTab, 'media-ffmpeg')).toBe(true)
    expect(groupSelectionState(domainPackageNames(media), viaRepoTab, noLock).selected).toBe(1)
  })
})

describe('clearableNames', () => {
  it('includes only names explicitly in addedPackages', () => {
    expect(clearableNames(['a', 'b'], add('a'))).toEqual(['a'])
  })

  it('excludes a name even when the template locks it, since it was never added', () => {
    expect(clearableNames(['a'], [])).toEqual([])
  })

  it('deduplicates its input', () => {
    expect(clearableNames(['a', 'a'], add('a'))).toEqual(['a'])
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

describe('the base-runtime gate and unavailable runtimes', () => {
  // Real-time is shown but disabled on this tab. It is still an ordinary
  // package, so the Repositories tab and the search dropdown can add it — and
  // if that counted as a runtime, the disabled option would unlock every
  // domain from the other surface.
  it('does not accept an unavailable runtime as the selected one', () => {
    expect(selectedBaseRuntime(pack, add('base-realtime'), noLock)).toBeUndefined()
    expect(canSelectDomains(pack, add('base-realtime'), noLock)).toBe(false)
  })

  it('still accepts the available runtime', () => {
    expect(selectedBaseRuntime(pack, add('base-standard'), noLock)?.id).toBe('standard')
    expect(canSelectDomains(pack, add('base-standard'), noLock)).toBe(true)
  })

  it('leaves domains locked when only the unavailable runtime is selected', () => {
    expect(domainSelectable(pack, media, add('base-realtime'), noLock)).toBe(false)
    expect(domainLockReason(pack, media, add('base-realtime'), noLock)).toBe(
      'Select a base runtime first',
    )
  })

  it('counts domain packages as stranded under an unavailable runtime', () => {
    // They would reach the build with nothing underneath them, which is the
    // exact condition the warning exists to report.
    expect(strandedPackages(pack, add('base-realtime', 'media-ffmpeg'), noLock)).toEqual([
      'media-ffmpeg',
    ])
  })
})

describe('addableDomainPackages', () => {
  it('omits an unavailable domain, which the pack checkbox must not add', () => {
    expect(addableDomainPackages(pack).map((p) => p.name)).toEqual([
      'media-ffmpeg',
      'shared-runtime',
    ])
  })

  it('still counts the unavailable domain as clearable', () => {
    // allDomainPackages is the wider set: what is already selected has to
    // remain removable however it got there.
    expect(allDomainPackages(pack).map((p) => p.name)).toContain('npu-driver')
  })

  it('deduplicates a package two available domains share', () => {
    const names = addableDomainPackages(pack).map((p) => p.name)
    expect(names.filter((n) => n === 'shared-runtime')).toHaveLength(1)
  })
})

describe('packToggleMode', () => {
  const addableCount = (added: AddedPackage[], baseLock: BaseLock = noLock) =>
    addableDomainPackages(pack)
      .map((p) => p.name)
      .filter((n) => !(baseLock.info(n).locked || isSelected(added, n))).length
  const clearableCount = (added: AddedPackage[]) =>
    clearableNames(allDomainPackages(pack).map((p) => p.name), added).length
  const mode = (selectable: boolean, added: AddedPackage[], baseLock: BaseLock = noLock) =>
    packToggleMode(selectable, addableCount(added, baseLock), clearableCount(added))

  it('is locked while the gate is shut and nothing is selected', () => {
    expect(mode(false, [])).toBe('locked')
  })

  it('adds once a runtime is chosen', () => {
    expect(mode(true, [])).toBe('add')
  })

  it('reaches full on the addable set alone, without the unavailable domain', () => {
    // The unselectable npu-driver must not hold the count below full forever;
    // otherwise the checkbox is stuck at mixed no matter what the user clicks.
    const all = add('media-ffmpeg', 'shared-runtime')
    expect(addableCount(all)).toBe(0)
    expect(mode(true, all)).toBe('clear')
  })

  it('still offers add while an available domain has anything left to select', () => {
    // Selecting only the unavailable domain's package does not exhaust the
    // addable set — Media is still there — so this is an 'add', not a 'clear'.
    expect(mode(true, add('base-standard', 'npu-driver'))).toBe('add')
  })

  it('clears rather than locks when no domain here is selectable at all', () => {
    // Reachable by selecting NPU and then switching to a target that publishes
    // none of the domains. Nothing can be added, but the leftover selection
    // must not become permanent — that is the stranding the domain-level gate
    // exists to avoid, arrived at from the other direction.
    const noneAvailable: EdgePack = {
      ...pack,
      domains: pack.domains.map((d) => ({ ...d, available: false })),
    }
    const addableOf = (added: AddedPackage[]) =>
      addableDomainPackages(noneAvailable)
        .map((p) => p.name)
        .filter((n) => !isSelected(added, n)).length
    const clearableOf = (added: AddedPackage[]) =>
      clearableNames(allDomainPackages(noneAvailable).map((p) => p.name), added).length
    const added = add('base-standard', 'npu-driver')
    expect(addableDomainPackages(noneAvailable)).toHaveLength(0)
    expect(packToggleMode(true, addableOf(added), clearableOf(added))).toBe('clear')
    // And genuinely locked once that leftover is gone.
    const empty = add('base-standard')
    expect(packToggleMode(true, addableOf(empty), clearableOf(empty))).toBe('locked')
  })

  it('clears a stranded selection even while the gate is shut', () => {
    expect(mode(false, add('media-ffmpeg'))).toBe('clear')
  })

  it('locks once everything addable is already covered by the template', () => {
    expect(mode(true, [], lockOf('media-ffmpeg', 'shared-runtime'))).toBe('locked')
  })
})
