// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// "Update to latest version", above the Repositories tab's package list: every added
// package, floating or pinned and from any repo, becomes an exact pin on the
// newest version the checked repos offer, so the template records that
// version instead of resolving "Latest" at build time. The matched template's
// own packages are pinned the same way, as added overrides.

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

function Browser({ os = 'ubuntu24', baseLock = noLock }: { os?: string; baseLock?: BaseLock }) {
  const enabledRepos = useStore((s) => s.enabledRepos)
  const enabledRepoIds = repos.filter((r) => r.enabledByDefault || enabledRepos.includes(r.id)).map((r) => r.id)
  const resolver = useVersionResolver(os, enabledRepoIds)
  return <PackageRepoBrowser repos={repos} os={os} baseLock={baseLock} resolver={resolver} />
}

// A template listing vim unpinned and curl pinned to an older version than
// the checked repos' newest.
function templateLock(pins: Record<string, string>, bare: string[]): BaseLock {
  return {
    info: (name) =>
      name in pins
        ? { locked: true, currentVersion: pins[name], currentIsFloating: false }
        : bare.includes(name)
          ? { locked: true, currentIsFloating: true }
          : { locked: false, currentIsFloating: false },
    ensureResolved: () => {},
    concreteNames: [...bare, ...Object.keys(pins)],
  }
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
    // Opening the repo looks its picks up in that repo alone; only the
    // button's own request is under test here.
    mockApi.lookupPackages.mockClear()
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

  it('discards the answer when the step unmounts while the lookup was in flight', async () => {
    // Next → Back unmounts and remounts the Packages step; an answer from the
    // old mount must not land after a repo was disabled in the new one.
    useStore.setState({ addedPackages: [{ name: 'curl', version: '', repo: 'base' }] })
    let unmount!: () => void
    await act(async () => {
      unmount = render(<Browser />).unmount
    })
    const release = gateLookups()
    await act(async () => fireEvent.click(pinButton()))

    unmount()
    act(() => useStore.setState({ enabledRepos: [] }))
    await release()

    expect(useStore.getState().addedPackages).toEqual([{ name: 'curl', version: '', repo: 'base' }])
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

describe('PackageRepoBrowser — Update to latest version, template packages', () => {
  const versioned = (name: string, ...versions: string[]): PackageSearchResult => ({
    name,
    version: versions[0],
    repository: 'base',
    versions: versions.map((version) => ({ version, repository: 'base' })),
  })
  const renderWith = async (baseLock: BaseLock) => {
    await act(async () => {
      render(<Browser baseLock={baseLock} />)
    })
  }

  it('counts and pins the template packages too, as overrides', async () => {
    mockApi.lookupPackages.mockResolvedValue({
      packages: [versioned('vim', '2:9.1.0016-1'), versioned('curl', '8.5.0-2', '8.4.0-1')],
      unavailableRepos: [],
    })
    useStore.setState({ addedPackages: [{ name: 'htop', version: '', repo: 'base' }] })
    await renderWith(templateLock({ curl: '8.4.0-1' }, ['vim']))
    expect(pinButton().textContent).toBe('Update to latest version (3)')
    await act(async () => fireEvent.click(pinButton()))

    expect(mockApi.lookupPackages).toHaveBeenCalledWith(expect.objectContaining({ names: ['htop', 'vim', 'curl'] }))
    const byName = Object.fromEntries(useStore.getState().addedPackages.map((p) => [p.name, p]))
    expect(byName.vim).toEqual({ name: 'vim', version: '2:9.1.0016-1', repo: 'base' })
    expect(byName.curl).toEqual({ name: 'curl', version: '8.5.0-2', repo: 'base' })
  })

  it('is enabled with only template packages, and skips a name already added', async () => {
    mockApi.lookupPackages.mockResolvedValue({ packages: [versioned('vim', '2:9.1.0016-1')], unavailableRepos: [] })
    useStore.setState({ addedPackages: [{ name: 'vim', version: '2:9.0-1', repo: 'base' }] })
    await renderWith(templateLock({}, ['vim']))
    // vim is already overridden, so it counts once.
    expect(pinButton().textContent).toBe('Update to latest version (1)')

    useStore.setState({ addedPackages: [] })
    await act(async () => {})
    expect(pinButton().disabled).toBe(false)
  })

  it("keeps a template pin no checked repo publishes, and says so", async () => {
    mockApi.lookupPackages.mockResolvedValue({ packages: [versioned('curl', '8.3.0-1')], unavailableRepos: [] })
    await renderWith(templateLock({ curl: '8.4.0-1ppa1' }, []))
    await act(async () => fireEvent.click(pinButton()))

    expect(useStore.getState().addedPackages).toEqual([])
    expect(screen.getByRole('status').textContent).toMatch(/Kept the template's own pinned version of curl/)
  })

  it("doesn't pin template packages past the build's package limit", async () => {
    const bare = Array.from({ length: 300 }, (_, i) => `pkg${i}`)
    mockApi.lookupPackages.mockResolvedValue({ packages: bare.map((n) => versioned(n, '1.0')), unavailableRepos: [] })
    useStore.setState({ addedPackages: [{ name: 'htop', version: '', repo: 'base' }] })
    newest.htop = { name: 'htop', version: '3.3.0-4', repository: 'base' }
    try {
      mockApi.lookupPackages.mockImplementation(() =>
        Promise.resolve({ packages: [...bare.map((n) => versioned(n, '1.0')), newest.htop], unavailableRepos: [] }),
      )
      await renderWith(templateLock({}, bare))
      await act(async () => fireEvent.click(pinButton()))
    } finally {
      delete newest.htop
    }

    expect(useStore.getState().addedPackages).toEqual([{ name: 'htop', version: '3.3.0-4', repo: 'base' }])
    expect(screen.getByRole('status').textContent).toMatch(/over the 256 a build accepts/)
  })
})

describe('PackageRepoBrowser — Update to latest version, per repository', () => {
  it("counts and pins only what's selected from the browsed repo", async () => {
    useStore.setState({
      addedPackages: [
        { name: 'vim', version: '', repo: 'base' },
        { name: 'curl', version: '7.88.1-10', repo: 'intel-eci' },
      ],
    })
    await renderBrowser()
    // Browsing base: vim is selected from here, curl from Intel ECI.
    expect(screen.getByText(/\(1 selected\)/)).toBeTruthy()
    expect(pinButton().textContent).toBe('Update to latest version (1)')
    mockApi.lookupPackages.mockClear()
    await act(async () => fireEvent.click(pinButton()))

    expect(mockApi.lookupPackages).toHaveBeenCalledWith(expect.objectContaining({ names: ['vim'] }))
    const byName = Object.fromEntries(useStore.getState().addedPackages.map((p) => [p.name, p]))
    expect(byName.vim.version).toBe('2:9.1.0016-1')
    expect(byName.curl.version).toBe('7.88.1-10')
  })
})

describe('PackageRepoBrowser — Show only selected', () => {
  it("offers every version the browsed repo has for a pinned pick, not just the pinned one", async () => {
    mockApi.lookupPackages.mockResolvedValue({
      packages: [
        {
          name: 'vim',
          version: '2:9.1.0016-1',
          repository: 'base',
          versions: [
            { version: '2:9.1.0016-1', repository: 'base' },
            { version: '2:9.1.0000-1', repository: 'base' },
          ],
        },
      ],
      unavailableRepos: [],
    })
    useStore.setState({ addedPackages: [{ name: 'vim', version: '2:9.1.0016-1', repo: 'base' }] })
    await renderBrowser()
    await act(async () => fireEvent.click(screen.getByRole('checkbox', { name: /Show only selected/ })))

    expect(screen.getByRole('button', { name: /9\.1\.0000-1/ })).toBeTruthy()
  })

  it('puts Show frequently used last on the controls row', async () => {
    const curated = repos.map((r) => (r.id === 'base' ? { ...r, hasCuratedPackages: true } : r))
    function CuratedBrowser() {
      const resolver = useVersionResolver('ubuntu24', ['base'])
      return <PackageRepoBrowser repos={curated} os="ubuntu24" baseLock={noLock} resolver={resolver} />
    }
    await act(async () => {
      render(<CuratedBrowser />)
    })
    const frequent = screen.getByRole('checkbox', { name: /Show frequently used/ })
    const row = frequent.closest('div')!
    const labels = Array.from(row.querySelectorAll('label, button')).map((el) => el.textContent ?? '')
    expect(labels[labels.length - 1]).toMatch(/Show frequently used/)
  })
})
