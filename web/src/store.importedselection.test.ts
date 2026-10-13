// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// What the Target step shows after an import. A template states its target and
// image type but never a use case, SKU, or platform, so those can only be
// adopted when the manifest leaves no choice about them.

import { describe, expect, it } from 'vitest'
import { selectionFromTemplate } from './store'
import { parseImportedTemplate } from './lib/importedTemplate'
import type { Manifest } from './api/types'

const manifest: Manifest = {
  verticals: [{ id: 'robotics', displayName: 'Robotics' }],
  skus: [{ id: 'amr', displayName: 'AMR' }],
  platforms: [{ id: 'wcl', displayName: 'WCL' }],
  targets: [
    { id: 'ubuntu24', displayName: 'Ubuntu 24.04', os: 'ubuntu', arch: 'x86_64' },
    { id: 'debian13', displayName: 'Debian 13', os: 'debian', arch: 'x86_64' },
  ],
  combinations: [
    { vertical: 'robotics', sku: 'amr', platform: 'wcl', os: 'ubuntu24', imageType: 'raw', template: 'r.yml' },
    { vertical: 'retail', sku: 'dv', platform: 'arl', os: 'debian13', imageType: 'raw', template: 'x.yml' },
    { vertical: 'robotics', sku: 'amr', platform: 'wcl', os: 'debian13', imageType: 'raw', template: 'y.yml' },
  ],
}

const template = (target: string) =>
  parseImportedTemplate('t.yml', `image:\n  name: img\ntarget:\n${target}`)

describe('selectionFromTemplate', () => {
  it('matches the target by dist, which is the id the manifest uses', () => {
    const got = selectionFromTemplate(manifest, template('  os: ubuntu\n  dist: ubuntu24\n  imageType: raw\n'))
    expect(got.os).toBe('ubuntu24')
    expect(got.imageType).toBe('raw')
  })

  it('falls back to the OS family when the document states no dist', () => {
    const got = selectionFromTemplate(manifest, template('  os: debian\n  imageType: raw\n'))
    expect(got.os).toBe('debian13')
  })

  it('adopts the use case when exactly one combination matches', () => {
    const got = selectionFromTemplate(manifest, template('  os: ubuntu\n  dist: ubuntu24\n  imageType: raw\n'))
    expect(got.vertical).toBe('robotics')
    expect(got.sku).toBe('amr')
    expect(got.platform).toBe('wcl')
  })

  // Two combinations claim debian13/raw with different use cases. Picking one
  // would present a guess as a fact, so the dropdowns are left to the user.
  it('leaves the use case blank when the manifest is ambiguous', () => {
    const got = selectionFromTemplate(manifest, template('  os: debian\n  dist: debian13\n  imageType: raw\n'))
    expect(got.os).toBe('debian13')
    expect(got.vertical).toBeUndefined()
    expect(got.platform).toBeUndefined()
  })

  it('states nothing for a target the manifest does not offer', () => {
    expect(selectionFromTemplate(manifest, template('  os: plan9\n  dist: plan9\n'))).toEqual({})
  })

  it('states nothing before the manifest has loaded', () => {
    expect(selectionFromTemplate(null, template('  os: ubuntu\n  dist: ubuntu24\n'))).toEqual({})
  })
})
