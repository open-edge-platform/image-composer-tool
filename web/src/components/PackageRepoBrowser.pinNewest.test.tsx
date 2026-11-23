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
  const priorities = Object.fromEntries(repos.filter((r) => r.priority != null).map((r) => [r.id, r.priority!]))
  const resolver = useVersionResolver(os, enabledRepoIds, priorities)
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

// The catalog fetch comes back empty; the by-name lookup finds `newest`, in
// whichever of the asked-for repos publishes it.
const answer = ({ names, repos: asked }: { names: string[]; repos?: string[] }, unavailableRepos: string[] = []) => ({
  packages: names.filter((n) => newest[n] && (!asked || asked.includes(newest[n].repository))).map((n) => newest[n]),
  unavailableRepos,
})

beforeEach(() => {
  useStore.setState({ addedPackages: [], enabledRepos: ['intel-eci'] })
  mockApi.searchPackages.mockReset()
  mockApi.searchPackages.mockResolvedValue({ query: '', total: 0, packages: [] })
  mockApi.lookupPackages.mockReset()
  mockApi.lookupPackages.mockImplementation((args: { names: string[]; repos?: string[] }) => Promise.resolve(answer(args)))
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

  it('pins a floating "Latest" pick to the newest version the browsed repo offers', async () => {
    useStore.setState({ addedPackages: [{ name: 'vim', version: '', repo: 'base' }] })
    await renderBrowser()
    expect(pinButton().textContent).toBe('Update to latest version (1)')
    await act(async () => fireEvent.click(pinButton()))

    expect(useStore.getState().addedPackages).toEqual([{ name: 'vim', version: '2:9.1.0016-1', repo: 'base' }])
  })

  it("doesn't put the always-on base repo into enabledRepos when pinning from it", async () => {
    useStore.setState({ addedPackages: [{ name: 'vim', version: '', repo: 'base' }], enabledRepos: [] })
    await renderBrowser()
    await act(async () => fireEvent.click(pinButton()))

    expect(useStore.getState().addedPackages).toEqual([{ name: 'vim', version: '2:9.1.0016-1', repo: 'base' }])
    expect(useStore.getState().enabledRepos).toEqual([])
  })

  it("doesn't pin a newer version another checked repo offers", async () => {
    // curl's newest, 8.5.0-2, is in Intel ECI; browsing base, it stays put.
    useStore.setState({ addedPackages: [{ name: 'curl', version: '7.88.1-10', repo: 'base' }] })
    await renderBrowser()
    await act(async () => fireEvent.click(pinButton()))

    expect(useStore.getState().addedPackages).toEqual([{ name: 'curl', version: '7.88.1-10', repo: 'base' }])
  })

  it('looks up every added name, pinned ones too, in the browsed repo in one request', async () => {
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
      repos: ['base'],
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

  // libnss-libvirt in the NVL BKC template: unpinned, browsed in one repo,
  // but a build takes it from another. Here Intel ECI outranks base.
  const elsewhereAnswer = ({ repos: asked }: { repos?: string[] }) =>
    Promise.resolve({
      packages:
        asked?.length === 1 && asked[0] === 'base'
          ? [versioned('vim', '2:9.1.0016-1')]
          : [
              {
                name: 'vim',
                version: '2:9.2-1',
                repository: 'intel-eci',
                versions: [
                  { version: '2:9.2-1', repository: 'intel-eci' },
                  { version: '2:9.1.0016-1', repository: 'base' },
                ],
              },
            ],
      unavailableRepos: [],
    })

  it('shows an unpinned template package at the version a build installs, via its repository', async () => {
    repos[1].priority = 1001
    try {
      mockApi.lookupPackages.mockImplementation(elsewhereAnswer)
      await renderWith(templateLock({}, ['vim']))
      await act(async () => fireEvent.click(screen.getByRole('checkbox', { name: /Show only selected/ })))

      expect(screen.getByTitle('The version Unpinned currently resolves to').textContent).toBe('2:9.2-1')
      expect(screen.getByRole('button', { name: 'Intel ECI' }).closest('span')?.textContent).toBe('via Intel ECI')
    } finally {
      delete repos[1].priority
    }
  })

  it("keeps an unpinned template package unpinned when a build takes it from another repo, and says so", async () => {
    repos[1].priority = 1001
    try {
      mockApi.lookupPackages.mockImplementation(elsewhereAnswer)
      await renderWith(templateLock({}, ['vim']))
      await act(async () => fireEvent.click(screen.getByRole('checkbox', { name: /Show only selected/ })))
      expect(pinButton().textContent).toBe('Update to latest version (0)')

      await act(async () => fireEvent.click(pinButton()))
      expect(useStore.getState().addedPackages).toEqual([])
    } finally {
      delete repos[1].priority
    }
  })

  it('checks where an unpinned template package resolves before pinning it, even before the row knows', async () => {
    repos[1].priority = 1001
    try {
      // Not listing the template's packages, the rows haven't looked vim up,
      // so the button counts it; its own check finds where it comes from.
      mockApi.lookupPackages.mockImplementation(elsewhereAnswer)
      await renderWith(templateLock({}, ['vim']))
      expect(pinButton().textContent).toBe('Update to latest version (1)')
      await act(async () => fireEvent.click(pinButton()))

      expect(useStore.getState().addedPackages).toEqual([])
      expect(screen.getByText(/Kept vim unpinned: a build installs it from Intel ECI/)).toBeTruthy()
    } finally {
      delete repos[1].priority
    }
  })

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

  it("leaves out a template pin the browsed repo doesn't publish, and says where it comes from", async () => {
    // Base has curl 8.3.0-1; the template's 8.4.0-1ppa1 is Intel ECI's.
    mockApi.lookupPackages.mockImplementation(({ repos: asked }: { repos?: string[] }) =>
      Promise.resolve({
        packages: asked?.includes('base')
          ? [versioned('curl', '8.3.0-1'), versioned('vim', '2:9.1.0016-1')]
          : [{ name: 'curl', version: '8.4.0-1ppa1', repository: 'intel-eci', versions: [{ version: '8.4.0-1ppa1', repository: 'intel-eci' }] }],
        unavailableRepos: [],
      }),
    )
    await renderWith(templateLock({ curl: '8.4.0-1ppa1' }, ['vim']))
    await act(async () => {})

    // vim is updated here; curl isn't, and the note says where it's from.
    expect(pinButton().textContent).toBe('Update to latest version (1)')
    const note = screen.getByText('1 pinned from Intel ECI')
    expect(note.getAttribute('title')).toBe(
      "1 template pin (curl) isn't updated here: its pinned version comes from Intel ECI, not Ubuntu Base. Browse Intel ECI to update it.",
    )
    await act(async () => fireEvent.click(pinButton()))
    expect(useStore.getState().addedPackages.map((p) => p.name)).toEqual(['vim'])
  })

  it('links the row’s “via” to where a template pin comes from, and warns on its chips', async () => {
    mockApi.lookupPackages.mockImplementation(({ repos: asked }: { repos?: string[] }) =>
      Promise.resolve({
        packages: asked?.includes('base')
          ? [versioned('curl', '8.3.0-1')]
          : [{ name: 'curl', version: '8.4.0-1ppa1', repository: 'intel-eci', versions: [{ version: '8.4.0-1ppa1', repository: 'intel-eci' }] }],
        unavailableRepos: [],
      }),
    )
    await renderWith(templateLock({ curl: '8.4.0-1ppa1' }, []))
    await act(async () => {})
    await act(async () => fireEvent.click(screen.getByRole('checkbox', { name: /Show only selected/ })))

    // The header's version is the template's pin, so its "via" links to
    // where that pin is from, not the repo being browsed.
    expect(screen.getByText('Pinned', { selector: 'span' })).toBeTruthy()
    expect(screen.getByTitle('Pinned version').textContent).toBe('8.4.0-1ppa1')
    expect(screen.queryByText(/via Ubuntu Base/)).toBeNull()
    expect(screen.getByRole('button', { name: '8.3.0-1' }).getAttribute('title')).toBe(
      "8.3.0-1 from Ubuntu Base — replaces the template's own build, 8.4.0-1ppa1 from Intel ECI",
    )
    const repoPicker = () => (screen.getByRole('combobox') as HTMLSelectElement).value
    expect(repoPicker()).toBe('base')
    await act(async () => fireEvent.click(screen.getByRole('button', { name: 'Intel ECI' })))
    expect(repoPicker()).toBe('intel-eci')
  })

  it('makes the note a link to the one repo the pins come from', async () => {
    mockApi.lookupPackages.mockImplementation(({ repos: asked }: { repos?: string[] }) =>
      Promise.resolve({
        packages: asked?.includes('base')
          ? [versioned('curl', '8.3.0-1')]
          : [{ name: 'curl', version: '8.4.0-1ppa1', repository: 'intel-eci', versions: [{ version: '8.4.0-1ppa1', repository: 'intel-eci' }] }],
        unavailableRepos: [],
      }),
    )
    await renderWith(templateLock({ curl: '8.4.0-1ppa1' }, []))
    await act(async () => {})
    await act(async () => fireEvent.click(screen.getByRole('button', { name: '1 pinned from Intel ECI' })))
    expect((screen.getByRole('combobox') as HTMLSelectElement).value).toBe('intel-eci')
  })

  it("doesn't name a repo the build doesn't read, even one publishing the pinned version", async () => {
    useStore.setState({ enabledRepos: [] })
    mockApi.lookupPackages.mockImplementation(({ repos: asked }: { repos?: string[] }) =>
      Promise.resolve({
        packages: asked?.includes('intel-eci')
          ? [{ name: 'curl', version: '8.4.0-1ppa1', repository: 'intel-eci', versions: [{ version: '8.4.0-1ppa1', repository: 'intel-eci' }] }]
          : asked?.includes('base')
            ? [versioned('curl', '8.3.0-1')]
            : [],
        unavailableRepos: [],
      }),
    )
    await renderWith(templateLock({ curl: '8.4.0-1ppa1' }, []))
    await act(async () => {})

    expect(pinButton().disabled).toBe(true)
    expect(screen.getByText('1 pinned from another repository')).toBeTruthy()
    expect(mockApi.lookupPackages).not.toHaveBeenCalledWith(expect.objectContaining({ repos: ['intel-eci'] }))
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

  it("doesn't offer Show frequently used, even for a repo with a curated list", async () => {
    const curated = repos.map((r) => (r.id === 'base' ? { ...r, hasCuratedPackages: true } : r))
    function CuratedBrowser() {
      const resolver = useVersionResolver('ubuntu24', ['base'])
      return <PackageRepoBrowser repos={curated} os="ubuntu24" baseLock={noLock} resolver={resolver} />
    }
    await act(async () => {
      render(<CuratedBrowser />)
    })
    expect(screen.queryByRole('checkbox', { name: /Show frequently used/ })).toBeNull()
    expect(mockApi.searchPackages).not.toHaveBeenCalledWith(expect.objectContaining({ curated: true }))
  })
})

describe('PackageRepoBrowser — Update to latest version puts the browsed repo in use', () => {
  const browseEci = async (baseLock: BaseLock) => {
    await act(async () => {
      render(<Browser baseLock={baseLock} />)
    })
    await act(async () => fireEvent.change(screen.getByRole('combobox'), { target: { value: 'intel-eci' } }))
    await act(async () => {})
  }

  it('turns on an optional repo it pins packages from', async () => {
    // curl is unpinned in the template; Intel ECI, not yet in use, has 8.5.0-2.
    useStore.setState({ addedPackages: [], enabledRepos: [] })
    await browseEci(templateLock({}, ['curl']))
    expect(pinButton().textContent).toBe('Update to latest version (1)')
    await act(async () => fireEvent.click(pinButton()))

    expect(useStore.getState().addedPackages).toEqual([{ name: 'curl', version: '8.5.0-2', repo: 'intel-eci' }])
    expect(useStore.getState().enabledRepos).toEqual(['intel-eci'])
  })

  it('leaves the repo off when nothing is pinned from it', async () => {
    // The template's pin is already Intel ECI's newest, so nothing changes.
    useStore.setState({ addedPackages: [], enabledRepos: [] })
    await browseEci(templateLock({ curl: '8.5.0-2' }, []))
    await act(async () => fireEvent.click(pinButton()))

    expect(useStore.getState().addedPackages).toEqual([])
    expect(useStore.getState().enabledRepos).toEqual([])
  })
})
