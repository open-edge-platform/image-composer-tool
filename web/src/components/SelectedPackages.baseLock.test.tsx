// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// What the matched template ships on its own now surfaces in the rail by
// default (previously only reachable via the browser's "Show only selected"
// checkbox) — this file covers that section specifically, distinct from
// SelectedPackages.imported.test.tsx which covers the unrelated
// drag-drop-import feature.

import { act, fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { SelectedPackages } from './SelectedPackages'
import { useStore } from '../store'
import type { PackageRepo } from '../api/types'
import type { BaseLock } from '../lib/baseLock'

const mockApi = vi.hoisted(() => ({ searchPackages: vi.fn() }))
vi.mock('../api/client', () => ({ api: mockApi }))

const repos: PackageRepo[] = [
  { id: 'base', displayName: 'Ubuntu Base', url: 'http://example/base', enabledByDefault: true },
]

const multiRepos: PackageRepo[] = [
  { id: 'base', displayName: 'Ubuntu Base', url: 'http://example/base', enabledByDefault: true },
  { id: 'intel-eci', displayName: 'Intel ECI', url: 'http://example/eci', enabledByDefault: false },
]

const lockOf = (names: Record<string, { currentVersion?: string; currentIsFloating?: boolean }>): BaseLock => ({
  info: (name) => ({ locked: name in names, currentIsFloating: false, ...names[name] }),
  ensureResolved: () => {},
  concreteNames: Object.keys(names),
})

beforeEach(() => {
  useStore.setState({
    addedPackages: [],
    enabledRepos: [],
    importedTemplate: null,
    removedBasePackages: [],
  })
  mockApi.searchPackages.mockResolvedValue({ packages: [], total: 0 })
})

describe('SelectedPackages — already in template', () => {
  it('lists template packages by default, with no checkbox needed', () => {
    const baseLock = lockOf({ vim: { currentVersion: '9.1' }, bluez: { currentVersion: '5.77' } })
    render(<SelectedPackages repos={repos} os="ubuntu" baseLock={baseLock} />)

    expect(screen.getByText('Already in template')).toBeTruthy()
    expect(screen.getByTitle('vim')).toBeTruthy()
    expect(screen.getByTitle('bluez')).toBeTruthy()
    expect(screen.getByText('Selected (2)')).toBeTruthy()
  })

  it('does not offer a remove control for a template-locked package', () => {
    const baseLock = lockOf({ vim: { currentVersion: '9.1' } })
    render(<SelectedPackages repos={repos} os="ubuntu" baseLock={baseLock} />)

    expect(screen.queryByLabelText('Remove vim')).toBeNull()
  })

  it('moves a package out of the template section once a version override adds it', () => {
    const baseLock = lockOf({ vim: { currentVersion: '9.1' } })
    const { rerender } = render(<SelectedPackages repos={repos} os="ubuntu" baseLock={baseLock} />)
    expect(screen.getByText('Already in template')).toBeTruthy()

    act(() => useStore.getState().setPackage({ name: 'vim', version: '9.2', repo: 'base' }))
    rerender(<SelectedPackages repos={repos} os="ubuntu" baseLock={baseLock} />)

    expect(screen.queryByText('Already in template')).toBeNull()
    expect(screen.getByLabelText('Remove vim')).toBeTruthy()
  })

  it('Clear removes only explicitly-added packages, leaving template-locked ones in place', () => {
    const baseLock = lockOf({ vim: { currentVersion: '9.1' } })
    useStore.getState().setPackage({ name: 'media-ffmpeg', version: '', repo: 'base' })
    const { rerender } = render(<SelectedPackages repos={repos} os="ubuntu" baseLock={baseLock} />)
    expect(screen.getByText('Already in template')).toBeTruthy()
    expect(screen.getByTitle('vim')).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Clear' }))
    rerender(<SelectedPackages repos={repos} os="ubuntu" baseLock={baseLock} />)

    expect(screen.getByText('Already in template')).toBeTruthy()
    expect(screen.getByTitle('vim')).toBeTruthy()
    expect(screen.queryByTitle('media-ffmpeg')).toBeNull()
  })

  // Regression: a repo the matched template's own locked packages still need
  // (not visible in addedPackages at all) used to be dropped from
  // enabledRepos once the last explicitly-added package from it was removed.
  it('Clear does not release a repo the matched template still locks', () => {
    const baseLock = lockOf({ vim: { currentVersion: '9.1' } })
    useStore.setState({ enabledRepos: ['intel-eci'] })
    useStore.getState().setPackage({ name: 'media-ffmpeg', version: '', repo: 'intel-eci' })
    render(
      <SelectedPackages
        repos={multiRepos}
        os="ubuntu"
        baseLock={baseLock}
        lockedRepoIds={new Set(['intel-eci'])}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Clear' }))

    expect(useStore.getState().addedPackages).toHaveLength(0)
    expect(useStore.getState().enabledRepos).toContain('intel-eci')
  })

  it('resolves a floating template package version on request', async () => {
    const baseLock = lockOf({ vim: { currentIsFloating: true } })
    mockApi.searchPackages.mockResolvedValue({ packages: [{ name: 'vim', version: '9.1', repo: 'base' }], total: 1 })
    render(<SelectedPackages repos={repos} os="ubuntu" baseLock={baseLock} />)

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Resolve' }))
    })

    expect(mockApi.searchPackages).toHaveBeenCalledWith({ os: 'ubuntu', repos: ['base'], q: 'vim', limit: 20 })
    expect(screen.getByText('9.1')).toBeTruthy()
  })

  it('bulk-resolves every unresolved template package', async () => {
    const baseLock = lockOf({ vim: { currentIsFloating: true }, bluez: { currentIsFloating: true } })
    mockApi.searchPackages.mockImplementation(({ q }: { q: string }) =>
      Promise.resolve({ packages: [{ name: q, version: `${q}-v1`, repo: 'base' }], total: 1 }),
    )
    render(<SelectedPackages repos={repos} os="ubuntu" baseLock={baseLock} />)

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Resolve (2)' }))
    })

    expect(screen.getByText('vim-v1')).toBeTruthy()
    expect(screen.getByText('bluez-v1')).toBeTruthy()
  })
})
