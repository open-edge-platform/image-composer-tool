// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// "Pin to newest", above the Repositories tab's package list: every added
// package, floating or pinned and from any repo, becomes an exact pin on the
// newest version the checked repos offer, so the template records that
// version instead of resolving "Latest" at build time.

import { act, fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { PackageRepoBrowser } from './PackageRepoBrowser'
import { useStore } from '../store'
import { useVersionResolver } from '../lib/useVersionResolver'
import type { PackageRepo, PackageSearchResult } from '../api/types'
import type { BaseLock } from '../lib/baseLock'

const mockApi = vi.hoisted(() => ({ searchPackages: vi.fn() }))
vi.mock('../api/client', () => ({ api: mockApi }))

const noLock: BaseLock = {
  info: () => ({ locked: false, currentIsFloating: false }),
  ensureResolved: () => {},
  concreteNames: [],
}

const repos: PackageRepo[] = [
  { id: 'base', displayName: 'Ubuntu Base', url: 'http://example/base', enabledByDefault: true },
  { id: 'intel-eci', displayName: 'Intel ECI', url: 'http://example/eci', enabledByDefault: false },
]

function Browser() {
  const enabledRepos = useStore((s) => s.enabledRepos)
  const enabledRepoIds = repos.filter((r) => r.enabledByDefault || enabledRepos.includes(r.id)).map((r) => r.id)
  const resolver = useVersionResolver('ubuntu24', enabledRepoIds)
  return <PackageRepoBrowser repos={repos} os="ubuntu24" baseLock={noLock} resolver={resolver} />
}

const newest: Record<string, PackageSearchResult> = {
  vim: { name: 'vim', version: '2:9.1.0016-1', repository: 'base' },
  curl: { name: 'curl', version: '8.5.0-2', repository: 'intel-eci' },
}

// The catalog fetch (no q) comes back empty; a by-name lookup finds `newest`.
const answer = ({ q }: { q?: string }) => ({
  query: q ?? '',
  total: q && newest[q] ? 1 : 0,
  packages: q && newest[q] ? [newest[q]] : [],
})

beforeEach(() => {
  useStore.setState({ addedPackages: [], enabledRepos: ['intel-eci'] })
  mockApi.searchPackages.mockReset()
  mockApi.searchPackages.mockImplementation((args: { q?: string }) => Promise.resolve(answer(args)))
})

const pinButton = () => screen.getByRole('button', { name: /^Pin to newest/ }) as HTMLButtonElement

const renderBrowser = async () => {
  await act(async () => {
    render(<Browser />)
  })
}

describe('PackageRepoBrowser — Pin to newest', () => {
  it('is shown but disabled with nothing added', async () => {
    await renderBrowser()
    expect(pinButton().disabled).toBe(true)
  })

  it('pins a floating "Latest" pick and bumps an older pin to the newest version', async () => {
    useStore.setState({
      addedPackages: [
        { name: 'vim', version: '', repo: 'base' },
        { name: 'curl', version: '7.88.1-10', repo: 'base' },
      ],
    })
    await renderBrowser()
    expect(pinButton().textContent).toBe('Pin to newest (2)')
    await act(async () => fireEvent.click(pinButton()))

    const byName = Object.fromEntries(useStore.getState().addedPackages.map((p) => [p.name, p]))
    expect(byName.vim).toEqual({ name: 'vim', version: '2:9.1.0016-1', repo: 'base' })
    expect(byName.curl).toEqual({ name: 'curl', version: '8.5.0-2', repo: 'intel-eci' })
    // Moving curl to the repo its newest version lives in keeps the old repo
    // checked: releasing it would change the scope the lookup ran against.
    expect(useStore.getState().enabledRepos).toContain('intel-eci')
  })

  it('looks up a name across every checked repo, even when it is already pinned', async () => {
    useStore.setState({ addedPackages: [{ name: 'curl', version: '7.88.1-10', repo: 'base' }] })
    await renderBrowser()
    await act(async () => fireEvent.click(pinButton()))

    expect(mockApi.searchPackages).toHaveBeenCalledWith(
      expect.objectContaining({ q: 'curl', repos: ['base', 'intel-eci'] }),
    )
  })

  it('leaves a package unchanged when no checked repo has it', async () => {
    useStore.setState({ addedPackages: [{ name: 'not-there', version: '', repo: 'base' }] })
    await renderBrowser()
    await act(async () => fireEvent.click(pinButton()))

    expect(useStore.getState().addedPackages).toEqual([{ name: 'not-there', version: '', repo: 'base' }])
  })

  it('does not add back a package removed while the lookup was in flight', async () => {
    useStore.setState({
      addedPackages: [
        { name: 'vim', version: '', repo: 'base' },
        { name: 'curl', version: '', repo: 'base' },
      ],
    })
    await renderBrowser()
    let release!: () => void
    const gate = new Promise<void>((r) => (release = r))
    mockApi.searchPackages.mockImplementation((args: { q?: string }) => gate.then(() => answer(args)))
    const button = pinButton()
    await act(async () => fireEvent.click(button))
    expect(button.textContent).toBe('Pinning…')
    expect(button.disabled).toBe(true)

    act(() => useStore.getState().removePackage('curl', { releaseRepo: false }))
    await act(async () => release())

    expect(useStore.getState().addedPackages).toEqual([{ name: 'vim', version: '2:9.1.0016-1', repo: 'base' }])
  })
})
