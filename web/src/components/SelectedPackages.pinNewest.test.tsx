// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// "Pin to newest": every added package, floating or pinned, becomes an exact
// pin on the newest version the checked repos offer, so the template records
// that version instead of resolving "Latest" at build time.

import { act, fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { SelectedPackages } from './SelectedPackages'
import { useStore } from '../store'
import { useVersionResolver } from '../lib/useVersionResolver'
import type { PackageRepo, PackageSearchResult } from '../api/types'

const mockApi = vi.hoisted(() => ({ searchPackages: vi.fn() }))
vi.mock('../api/client', () => ({ api: mockApi }))

const repos: PackageRepo[] = [
  { id: 'base', displayName: 'Ubuntu Base', url: 'http://example/base', enabledByDefault: true },
  { id: 'intel-eci', displayName: 'Intel ECI', url: 'http://example/eci', enabledByDefault: false },
]

function Selected() {
  const enabledRepos = useStore((s) => s.enabledRepos)
  const enabledRepoIds = repos.filter((r) => r.enabledByDefault || enabledRepos.includes(r.id)).map((r) => r.id)
  const resolver = useVersionResolver('ubuntu24', enabledRepoIds)
  return <SelectedPackages repos={repos} resolver={resolver} />
}

const newest: Record<string, PackageSearchResult> = {
  vim: { name: 'vim', version: '2:9.1.0016-1', repository: 'base' },
  curl: { name: 'curl', version: '8.5.0-2', repository: 'intel-eci' },
}

beforeEach(() => {
  useStore.setState({ addedPackages: [], enabledRepos: ['intel-eci'] })
  mockApi.searchPackages.mockReset()
  mockApi.searchPackages.mockImplementation(({ q }: { q: string }) =>
    Promise.resolve({ query: q, total: 1, packages: newest[q] ? [newest[q]] : [] }),
  )
})

const pinClick = async () => {
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: 'Pin to newest' }))
  })
}

describe('SelectedPackages — Pin to newest', () => {
  it('is not offered with nothing added', () => {
    render(<Selected />)
    expect(screen.queryByRole('button', { name: 'Pin to newest' })).toBeNull()
  })

  it('pins a floating "Latest" pick and bumps an older pin to the newest version', async () => {
    useStore.setState({
      addedPackages: [
        { name: 'vim', version: '', repo: 'base' },
        { name: 'curl', version: '7.88.1-10', repo: 'base' },
      ],
    })
    render(<Selected />)
    await pinClick()

    const byName = Object.fromEntries(useStore.getState().addedPackages.map((p) => [p.name, p]))
    expect(byName.vim).toEqual({ name: 'vim', version: '2:9.1.0016-1', repo: 'base' })
    expect(byName.curl).toEqual({ name: 'curl', version: '8.5.0-2', repo: 'intel-eci' })
    // Moving curl to the repo its newest version lives in keeps the old repo
    // checked: releasing it would change the scope the lookup ran against.
    expect(useStore.getState().enabledRepos).toContain('intel-eci')
    expect(screen.getByTitle('pinned to 2:9.1.0016-1 from Ubuntu Base')).toBeTruthy()
  })

  it('looks up a name even when it is already pinned', async () => {
    useStore.setState({ addedPackages: [{ name: 'curl', version: '7.88.1-10', repo: 'base' }] })
    render(<Selected />)
    await pinClick()

    expect(mockApi.searchPackages).toHaveBeenCalledWith(
      expect.objectContaining({ q: 'curl', repos: ['base', 'intel-eci'] }),
    )
  })

  it('leaves a package unchanged when no checked repo has it', async () => {
    useStore.setState({ addedPackages: [{ name: 'not-there', version: '', repo: 'base' }] })
    render(<Selected />)
    await pinClick()

    expect(useStore.getState().addedPackages).toEqual([{ name: 'not-there', version: '', repo: 'base' }])
  })

  it('does not add back a package removed while the lookup was in flight', async () => {
    let release!: () => void
    const gate = new Promise<void>((r) => (release = r))
    mockApi.searchPackages.mockImplementation(({ q }: { q: string }) =>
      gate.then(() => ({ query: q, total: 1, packages: newest[q] ? [newest[q]] : [] })),
    )
    useStore.setState({
      addedPackages: [
        { name: 'vim', version: '', repo: 'base' },
        { name: 'curl', version: '', repo: 'base' },
      ],
    })
    render(<Selected />)
    await pinClick()
    expect(screen.getByRole('button', { name: 'Pinning…' })).toBeTruthy()

    act(() => useStore.getState().removePackage('curl', { releaseRepo: false }))
    await act(async () => release())

    expect(useStore.getState().addedPackages).toEqual([{ name: 'vim', version: '2:9.1.0016-1', repo: 'base' }])
  })
})
