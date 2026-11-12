// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// "Update to latest version", above the Repositories tab's package list: every added
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

const mockApi = vi.hoisted(() => ({ searchPackages: vi.fn(), lookupPackages: vi.fn() }))
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

function Browser({ os = 'ubuntu24' }: { os?: string }) {
  const enabledRepos = useStore((s) => s.enabledRepos)
  const enabledRepoIds = repos.filter((r) => r.enabledByDefault || enabledRepos.includes(r.id)).map((r) => r.id)
  const resolver = useVersionResolver(os, enabledRepoIds)
  return <PackageRepoBrowser repos={repos} os={os} baseLock={noLock} resolver={resolver} />
}

const newest: Record<string, PackageSearchResult> = {
  vim: { name: 'vim', version: '2:9.1.0016-1', repository: 'base' },
  curl: { name: 'curl', version: '8.5.0-2', repository: 'intel-eci' },
}

// The catalog fetch comes back empty; the by-name lookup finds `newest`.
const answer = ({ names }: { names: string[] }, unavailableRepos: string[] = []) => ({
  packages: names.filter((n) => newest[n]).map((n) => newest[n]),
  unavailableRepos,
})

beforeEach(() => {
  useStore.setState({ addedPackages: [], enabledRepos: ['intel-eci'] })
  mockApi.searchPackages.mockReset()
  mockApi.searchPackages.mockResolvedValue({ query: '', total: 0, packages: [] })
  mockApi.lookupPackages.mockReset()
  mockApi.lookupPackages.mockImplementation((args: { names: string[] }) => Promise.resolve(answer(args)))
})

const pinButton = () => screen.getByRole('button', { name: /^Update to latest version/ }) as HTMLButtonElement

const renderBrowser = async () => {
  await act(async () => {
    render(<Browser />)
  })
}

describe('PackageRepoBrowser — Update to latest version', () => {
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
    expect(pinButton().textContent).toBe('Update to latest version (2)')
    await act(async () => fireEvent.click(pinButton()))

    const byName = Object.fromEntries(useStore.getState().addedPackages.map((p) => [p.name, p]))
    expect(byName.vim).toEqual({ name: 'vim', version: '2:9.1.0016-1', repo: 'base' })
    expect(byName.curl).toEqual({ name: 'curl', version: '8.5.0-2', repo: 'intel-eci' })
    // Moving curl to the repo its newest version lives in keeps the old repo
    // checked: releasing it would change the scope the lookup ran against.
    expect(useStore.getState().enabledRepos).toContain('intel-eci')
  })

  it('looks up every added name, pinned ones too, across every checked repo in one request', async () => {
    useStore.setState({
      addedPackages: [
        { name: 'vim', version: '', repo: 'base' },
        { name: 'curl', version: '7.88.1-10', repo: 'base' },
      ],
    })
    await renderBrowser()
    await act(async () => fireEvent.click(pinButton()))

    expect(mockApi.lookupPackages).toHaveBeenCalledTimes(1)
    expect(mockApi.lookupPackages).toHaveBeenCalledWith({
      os: 'ubuntu24',
      repos: ['base', 'intel-eci'],
      names: ['vim', 'curl'],
    })
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
    mockApi.lookupPackages.mockImplementation((args: { names: string[] }) => gate.then(() => answer(args)))
    const button = pinButton()
    await act(async () => fireEvent.click(button))
    expect(button.textContent).toBe('Updating…')
    expect(button.disabled).toBe(true)

    act(() => useStore.getState().removePackage('curl', { releaseRepo: false }))
    await act(async () => release())

    expect(useStore.getState().addedPackages).toEqual([{ name: 'vim', version: '2:9.1.0016-1', repo: 'base' }])
  })

  // Holds every lookup until release() so the selection can change meanwhile.
  const gateLookups = () => {
    let release!: () => void
    const gate = new Promise<void>((r) => (release = r))
    mockApi.lookupPackages.mockImplementation((args: { names: string[] }) => gate.then(() => answer(args)))
    return () => act(async () => release())
  }

  it('keeps a version picked while the lookup was in flight', async () => {
    useStore.setState({ addedPackages: [{ name: 'vim', version: '', repo: 'base' }] })
    await renderBrowser()
    const release = gateLookups()
    await act(async () => fireEvent.click(pinButton()))

    act(() => useStore.getState().setPackage({ name: 'vim', version: '2:9.0.0-1', repo: 'base' }, { releaseRepo: false }))
    await release()

    expect(useStore.getState().addedPackages).toEqual([{ name: 'vim', version: '2:9.0.0-1', repo: 'base' }])
  })

  it('leaves alone a package removed and added again while the lookup was in flight', async () => {
    useStore.setState({ addedPackages: [{ name: 'vim', version: '', repo: 'base' }] })
    await renderBrowser()
    const release = gateLookups()
    await act(async () => fireEvent.click(pinButton()))

    act(() => {
      useStore.getState().removePackage('vim', { releaseRepo: false })
      useStore.getState().setPackage({ name: 'vim', version: '', repo: 'base' }, { releaseRepo: false })
    })
    await release()

    expect(useStore.getState().addedPackages).toEqual([{ name: 'vim', version: '', repo: 'base' }])
  })

  it('discards the answer when the target changes while the lookup was in flight', async () => {
    useStore.setState({ addedPackages: [{ name: 'vim', version: '', repo: 'base' }] })
    let rerender!: (ui: React.ReactElement) => void
    await act(async () => {
      rerender = render(<Browser />).rerender
    })
    const release = gateLookups()
    await act(async () => fireEvent.click(pinButton()))

    // Same repo ids on another OS: the answer for ubuntu24 must not land here.
    await act(async () => rerender(<Browser os="ubuntu26" />))
    await release()

    expect(useStore.getState().addedPackages).toEqual([{ name: 'vim', version: '', repo: 'base' }])
    expect(screen.getByRole('status').textContent).toMatch(/changed while checking, so nothing was updated/)
  })

  it('changes nothing, and says which repo, when a repo index could not be read', async () => {
    useStore.setState({
      addedPackages: [
        { name: 'vim', version: '', repo: 'base' },
        { name: 'curl', version: '7.88.1-10', repo: 'base' },
      ],
    })
    mockApi.lookupPackages.mockImplementation((args: { names: string[] }) => Promise.resolve(answer(args, ['intel-eci'])))
    await renderBrowser()
    await act(async () => fireEvent.click(pinButton()))

    expect(useStore.getState().addedPackages).toEqual([
      { name: 'vim', version: '', repo: 'base' },
      { name: 'curl', version: '7.88.1-10', repo: 'base' },
    ])
    expect(screen.getByRole('status').textContent).toMatch(/Couldn't read Intel ECI right now, so nothing was updated/)
  })

  it('changes nothing when a lookup fails outright', async () => {
    useStore.setState({ addedPackages: [{ name: 'vim', version: '', repo: 'base' }] })
    mockApi.lookupPackages.mockRejectedValue(new Error('offline'))
    await renderBrowser()
    await act(async () => fireEvent.click(pinButton()))

    expect(useStore.getState().addedPackages).toEqual([{ name: 'vim', version: '', repo: 'base' }])
    expect(screen.getByRole('status').textContent).toMatch(/Couldn't check for newer versions/)
  })

  it('fetches every name afresh rather than reusing an earlier answer', async () => {
    useStore.setState({ addedPackages: [{ name: 'vim', version: '', repo: 'base' }] })
    await renderBrowser()
    await act(async () => fireEvent.click(pinButton()))
    expect(useStore.getState().addedPackages[0].version).toBe('2:9.1.0016-1')

    // A newer build lands upstream; the same repos are checked, so a cached
    // answer would still say 2:9.1.0016-1.
    newest.vim = { name: 'vim', version: '2:9.1.0016-2', repository: 'base' }
    try {
      mockApi.lookupPackages.mockClear()
      await act(async () => fireEvent.click(pinButton()))

      expect(mockApi.lookupPackages).toHaveBeenCalledWith(expect.objectContaining({ names: ['vim'] }))
      expect(useStore.getState().addedPackages[0].version).toBe('2:9.1.0016-2')
    } finally {
      newest.vim = { name: 'vim', version: '2:9.1.0016-1', repository: 'base' }
    }
  })
})
