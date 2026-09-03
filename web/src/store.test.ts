// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import { beforeEach, describe, expect, it } from 'vitest'
import { useStore } from './store'
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
