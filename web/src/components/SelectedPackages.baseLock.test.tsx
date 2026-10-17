// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// What the matched template ships on its own now surfaces in the rail by
// default (previously only reachable via the browser's "Show only selected"
// checkbox) — this file covers that section specifically.

import { act, fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { SelectedPackages } from './SelectedPackages'
import { useStore } from '../store'
import { useVersionResolver } from '../lib/useVersionResolver'
import type { PackageRepo } from '../api/types'
import type { BaseLock } from '../lib/baseLock'

const mockApi = vi.hoisted(() => ({ searchPackages: vi.fn() }))
vi.mock('../api/client', () => ({ api: mockApi }))

// SelectedPackages no longer owns its own resolver — PackagesStep.tsx lifts
// one shared useVersionResolver and passes it down, so a floating pick
// resolves the same way everywhere it's shown. This wrapper reproduces that
// lift for these tests, the same way the real caller does.
function Selected({
  repos,
  os,
  baseLock,
  lockedRepoIds,
}: {
  repos: PackageRepo[]
  os: string
  baseLock?: BaseLock
  lockedRepoIds?: Set<string>
}) {
  const enabledRepos = useStore((s) => s.enabledRepos)
  const enabledRepoIds = repos.filter((r) => r.enabledByDefault || enabledRepos.includes(r.id)).map((r) => r.id)
  const resolver = useVersionResolver(os, enabledRepoIds)
  return <SelectedPackages repos={repos} os={os} baseLock={baseLock} resolver={resolver} lockedRepoIds={lockedRepoIds} />
}

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
  })
  mockApi.searchPackages.mockReset()
  mockApi.searchPackages.mockResolvedValue({ packages: [], total: 0 })
})

describe('SelectedPackages — already in template', () => {
  it('lists template packages by default, with no checkbox needed', () => {
    const baseLock = lockOf({ vim: { currentVersion: '9.1' }, bluez: { currentVersion: '5.77' } })
    render(<Selected repos={repos} os="ubuntu" baseLock={baseLock} />)

    expect(screen.getByText('Already in template')).toBeTruthy()
    expect(screen.getByTitle('vim')).toBeTruthy()
    expect(screen.getByTitle('bluez')).toBeTruthy()
    expect(screen.getByText('Selected (2)')).toBeTruthy()
  })

  it('does not offer a remove control for a template-locked package', () => {
    const baseLock = lockOf({ vim: { currentVersion: '9.1' } })
    render(<Selected repos={repos} os="ubuntu" baseLock={baseLock} />)

    expect(screen.queryByLabelText('Remove vim')).toBeNull()
  })

  it('moves a package out of the template section once a version override adds it', () => {
    const baseLock = lockOf({ vim: { currentVersion: '9.1' } })
    const { rerender } = render(<Selected repos={repos} os="ubuntu" baseLock={baseLock} />)
    expect(screen.getByText('Already in template')).toBeTruthy()

    act(() => useStore.getState().setPackage({ name: 'vim', version: '9.2', repo: 'base' }))
    rerender(<Selected repos={repos} os="ubuntu" baseLock={baseLock} />)

    expect(screen.queryByText('Already in template')).toBeNull()
    expect(screen.getByLabelText('Remove vim')).toBeTruthy()
  })

  it('Clear removes only explicitly-added packages, leaving template-locked ones in place', () => {
    const baseLock = lockOf({ vim: { currentVersion: '9.1' } })
    useStore.getState().setPackage({ name: 'media-ffmpeg', version: '', repo: 'base' })
    const { rerender } = render(<Selected repos={repos} os="ubuntu" baseLock={baseLock} />)
    expect(screen.getByText('Already in template')).toBeTruthy()
    expect(screen.getByTitle('vim')).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Clear' }))
    rerender(<Selected repos={repos} os="ubuntu" baseLock={baseLock} />)

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
      <Selected
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

  it('resolves a floating template package version on request, not automatically', async () => {
    const baseLock = lockOf({ vim: { currentIsFloating: true } })
    mockApi.searchPackages.mockResolvedValue({ packages: [{ name: 'vim', version: '9.1', repository: 'base' }], total: 1 })
    render(<Selected repos={repos} os="ubuntu" baseLock={baseLock} />)

    // A template's own unpinned package list can run into the hundreds —
    // nothing here should be fetched until the user actually asks for it.
    expect(mockApi.searchPackages).not.toHaveBeenCalled()

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Resolve' }))
    })

    expect(mockApi.searchPackages).toHaveBeenCalledWith({ os: 'ubuntu', repos: ['base'], q: 'vim', limit: 20 })
    expect(screen.getByText('9.1')).toBeTruthy()
  })

  // Regression: useBaseLock resolves a floating entry's version against the
  // default repo only (to mark its chip disabled elsewhere), but that is not
  // the cross-repository version this rail reports — a checked optional repo
  // can resolve it differently. Treating the default-repo value as already
  // resolved here would hide the Resolve control before the real lookup runs.
  it('still offers Resolve for a floating package that already has a default-repo-only version', () => {
    const baseLock = lockOf({ vim: { currentIsFloating: true, currentVersion: '9.0-default-only' } })
    render(<Selected repos={repos} os="ubuntu" baseLock={baseLock} />)

    expect(screen.queryByText('9.0-default-only')).toBeNull()
    expect(screen.getByRole('button', { name: 'Resolve' })).toBeTruthy()
  })

  it('bulk-resolves every unresolved template package on request', async () => {
    const baseLock = lockOf({ vim: { currentIsFloating: true }, bluez: { currentIsFloating: true } })
    mockApi.searchPackages.mockImplementation(({ q }: { q: string }) =>
      Promise.resolve({ packages: [{ name: q, version: `${q}-v1`, repository: 'base' }], total: 1 }),
    )
    render(<Selected repos={repos} os="ubuntu" baseLock={baseLock} />)

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Resolve (2)' }))
    })

    expect(screen.getByText('vim-v1')).toBeTruthy()
    expect(screen.getByText('bluez-v1')).toBeTruthy()
  })
})

describe('SelectedPackages — already in template, grouped by repository', () => {
  it('groups a pinned/default-repo package under the base repo header', () => {
    const baseLock = lockOf({ vim: { currentVersion: '9.1' } })
    render(<Selected repos={multiRepos} os="ubuntu" baseLock={baseLock} />)

    const header = screen.getByText('Ubuntu Base')
    expect(header).toBeTruthy()
    expect(header.compareDocumentPosition(screen.getByTitle('vim')) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('groups a resolved floating package under the repo it actually resolved from', async () => {
    const baseLock = lockOf({ vim: { currentIsFloating: true } })
    mockApi.searchPackages.mockResolvedValue({
      packages: [{ name: 'vim', version: '9.1', repository: 'intel-eci' }],
      total: 1,
    })
    render(<Selected repos={multiRepos} os="ubuntu" baseLock={baseLock} />)

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Resolve' }))
    })

    const header = screen.getByText('Intel ECI')
    expect(header).toBeTruthy()
    expect(header.compareDocumentPosition(screen.getByTitle('vim')) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('puts a still-unresolved package under an "Unresolved" header rather than a repo name', () => {
    const baseLock = lockOf({ vim: { currentIsFloating: true } })
    render(<Selected repos={multiRepos} os="ubuntu" baseLock={baseLock} />)

    expect(screen.getByText('Unresolved')).toBeTruthy()
  })

  it('omits the "Unresolved" header when nothing is unresolved', () => {
    const baseLock = lockOf({ vim: { currentVersion: '9.1' } })
    render(<Selected repos={multiRepos} os="ubuntu" baseLock={baseLock} />)

    expect(screen.queryByText('Unresolved')).toBeNull()
  })
})
