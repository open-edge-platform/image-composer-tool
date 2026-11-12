// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// The Repositories tab's toolbar gives the base repo "always on, can't
// uncheck" treatment; a repo in lockedRepoIds (an Edge Pack repo the matched
// template already draws a package from — see templateLockedRepos in
// lib/edgepack.ts) must get the same treatment, for the same reason: the
// build already needs it, whether or not the user has touched this tab.

import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { PackageRepoBrowser } from './PackageRepoBrowser'
import { useStore } from '../store'
import { useVersionResolver } from '../lib/useVersionResolver'
import type { PackageRepo } from '../api/types'
import type { BaseLock } from '../lib/baseLock'

const mockApi = vi.hoisted(() => ({ searchPackages: vi.fn(), lookupPackages: vi.fn() }))
vi.mock('../api/client', () => ({ api: mockApi }))

const noLock: BaseLock = {
  info: () => ({ locked: false, currentIsFloating: false }),
  ensureResolved: () => {},
  concreteNames: [],
}

// PackageRepoBrowser no longer owns its own resolver — PackagesStep.tsx
// lifts one shared useVersionResolver and passes it down. This wrapper
// reproduces that lift for these tests, the same way the real caller does.
function Browser({
  repos,
  os,
  baseLock,
  lockedRepoIds,
}: {
  repos: PackageRepo[]
  os: string
  baseLock: BaseLock
  lockedRepoIds?: Set<string>
}) {
  const enabledRepos = useStore((s) => s.enabledRepos)
  const enabledRepoIds = repos.filter((r) => r.enabledByDefault || enabledRepos.includes(r.id)).map((r) => r.id)
  const resolver = useVersionResolver(os, enabledRepoIds)
  return <PackageRepoBrowser repos={repos} os={os} baseLock={baseLock} resolver={resolver} lockedRepoIds={lockedRepoIds} />
}

const repos: PackageRepo[] = [
  { id: 'base', displayName: 'Ubuntu Base', url: 'http://example/base', enabledByDefault: true },
  { id: 'intel-eci', displayName: 'Intel ECI', url: 'http://example/eci', enabledByDefault: false },
  { id: 'intel-graphics', displayName: 'Intel Graphics', url: 'http://example/graphics', enabledByDefault: false },
  { id: 'docker', displayName: 'Docker', url: 'http://example/docker', enabledByDefault: false },
]

beforeEach(() => {
  useStore.setState({ addedPackages: [], enabledRepos: [] })
  mockApi.searchPackages.mockResolvedValue({ packages: [], total: 0 })
  mockApi.lookupPackages.mockReset()
  mockApi.lookupPackages.mockResolvedValue([])
})

// browse picks a repository from the toolbar dropdown, the same way a user does.
function browse(repoId: string) {
  fireEvent.change(screen.getByRole('combobox', { name: /Repository/ }), { target: { value: repoId } })
}

function useInBuilds(repoName: string) {
  return screen.getByRole('checkbox', { name: `Use ${repoName} in builds` }) as HTMLInputElement
}

describe('a repo the matched template already draws a package from', () => {
  it('renders checked and disabled, same as the base repo', () => {
    render(
      <Browser
        repos={repos}
        os="ubuntu"
        baseLock={noLock}
        lockedRepoIds={new Set(['intel-eci'])}
      />,
    )

    browse('intel-eci')
    const box = useInBuilds('Intel ECI')
    expect(box.checked).toBe(true)
    expect(box.disabled).toBe(true)
    expect(screen.getByText('In template — always on')).toBeTruthy()
  })

  it('is not written into enabledRepos — it is checked by lockedRepoIds alone', () => {
    render(
      <Browser
        repos={repos}
        os="ubuntu"
        baseLock={noLock}
        lockedRepoIds={new Set(['intel-eci'])}
      />,
    )

    expect(useStore.getState().enabledRepos).not.toContain('intel-eci')
  })

  it('leaves an ordinary optional repo unchecked and enabled', () => {
    render(
      <Browser
        repos={repos}
        os="ubuntu"
        baseLock={noLock}
        lockedRepoIds={new Set(['intel-eci'])}
      />,
    )

    browse('docker')
    const box = useInBuilds('Docker')
    expect(box.checked).toBe(false)
    expect(box.disabled).toBe(false)
  })

  it('sorts locked repos to the top, after the base repo', () => {
    render(
      <Browser
        repos={repos}
        os="ubuntu"
        baseLock={noLock}
        lockedRepoIds={new Set(['intel-eci'])}
      />,
    )

    const order = screen.getAllByRole('option').map((el) => (el as HTMLOptionElement).value)
    expect(order).toEqual(['base', 'intel-eci', 'intel-graphics', 'docker'])
    const always = screen.getByRole('group', { name: 'Always on' })
    expect(Array.from(always.querySelectorAll('option')).map((o) => o.value)).toEqual(['base', 'intel-eci'])
  })

  it('defaults to no locked repos when the prop is omitted', () => {
    render(<Browser repos={repos} os="ubuntu" baseLock={noLock} />)

    browse('intel-eci')
    const box = useInBuilds('Intel ECI')
    expect(box.checked).toBe(false)
    expect(box.disabled).toBe(false)
  })
})

describe('the dropdown browses a repository, independent of using it in builds', () => {
  it('fetches the base repo by default, without any repo checked', async () => {
    render(<Browser repos={repos} os="ubuntu" baseLock={noLock} />)

    await waitFor(() =>
      expect(mockApi.searchPackages).toHaveBeenCalledWith(
        expect.objectContaining({ repos: ['base'] }),
      ),
    )
  })

  it('shows the base repo as always on and not uncheckable', () => {
    render(<Browser repos={repos} os="ubuntu" baseLock={noLock} />)

    const box = useInBuilds('Ubuntu Base')
    expect(box.checked).toBe(true)
    expect(box.disabled).toBe(true)
    expect(screen.getByText('Base — always on')).toBeTruthy()
  })

  it('switches the fetched repo when a different repository is picked', async () => {
    render(<Browser repos={repos} os="ubuntu" baseLock={noLock} />)
    await waitFor(() => expect(mockApi.searchPackages).toHaveBeenCalled())
    mockApi.searchPackages.mockClear()

    browse('docker')

    await waitFor(() =>
      expect(mockApi.searchPackages).toHaveBeenCalledWith(
        expect.objectContaining({ repos: ['docker'] }),
      ),
    )
  })

  it('lets browsing an unchecked repo without affecting enabledRepos', () => {
    render(<Browser repos={repos} os="ubuntu" baseLock={noLock} />)

    browse('docker')

    expect(useStore.getState().enabledRepos).not.toContain('docker')
  })

  it('enables the browsed repo from "Use in builds" without changing what is browsed', async () => {
    render(<Browser repos={repos} os="ubuntu" baseLock={noLock} />)
    browse('docker')
    await waitFor(() =>
      expect(mockApi.searchPackages).toHaveBeenCalledWith(expect.objectContaining({ repos: ['docker'] })),
    )
    mockApi.searchPackages.mockClear()

    fireEvent.click(useInBuilds('Docker'))

    expect(useStore.getState().enabledRepos).toContain('docker')
    expect((screen.getByRole('combobox', { name: /Repository/ }) as HTMLSelectElement).value).toBe('docker')
    expect(mockApi.searchPackages).not.toHaveBeenCalled()
  })

  // Only the browsed repo's checkbox is on screen, so the option labels and
  // the count are what still show every repo's state at a glance.
  it('marks optional repos in use in the dropdown and counts them', () => {
    useStore.setState({ enabledRepos: ['docker'] })
    render(<Browser repos={repos} os="ubuntu" baseLock={noLock} />)

    expect(screen.getByRole('option', { name: '✓ Docker' })).toBeTruthy()
    expect(screen.getByRole('option', { name: 'Intel ECI' })).toBeTruthy()
    expect(screen.getByText('2 of 4 repositories in use')).toBeTruthy()
  })
})

describe('a package already selected from a different repository than the one being browsed', () => {
  const multiRepos: PackageRepo[] = [
    { id: 'base', displayName: 'Ubuntu Base', url: 'http://example/base', enabledByDefault: true },
    { id: 'intel-eci', displayName: 'Intel ECI', url: 'http://example/eci', enabledByDefault: false },
  ]

  it('shows no active chip and a note naming the actual pin', async () => {
    useStore.setState({ addedPackages: [{ name: 'vim', version: '9.1', repo: 'intel-eci' }] })
    mockApi.searchPackages.mockResolvedValue({
      packages: [{ name: 'vim', version: '9.0', repository: 'base', versions: [{ version: '9.0', repository: 'base' }] }],
      total: 1,
    })
    render(<Browser repos={multiRepos} os="ubuntu" baseLock={noLock} />)

    await screen.findByText('vim')
    expect(screen.getByText(/Pinned to/)).toBeTruthy()
    expect(screen.getByText('9.1')).toBeTruthy()
    expect(screen.getByText('Intel ECI')).toBeTruthy()
    const chip = screen.getByRole('button', { name: '9.0' })
    expect(chip.className).not.toContain('bg-[#0071c5]')
  })

  it('shows the matching chip active once browsing the repo the pin actually came from', async () => {
    useStore.setState({ addedPackages: [{ name: 'vim', version: '9.1', repo: 'intel-eci' }] })
    mockApi.searchPackages.mockResolvedValue({
      packages: [
        { name: 'vim', version: '9.1', repository: 'intel-eci', versions: [{ version: '9.1', repository: 'intel-eci' }] },
      ],
      total: 1,
    })
    render(<Browser repos={multiRepos} os="ubuntu" baseLock={noLock} />)

    browse('intel-eci')

    await screen.findByText('vim')
    expect(screen.queryByText(/Pinned to/)).toBeNull()
    const chip = screen.getByRole('button', { name: '9.1' })
    expect(chip.className).toContain('bg-[#0071c5]')
  })

  it('shows "Latest" as selected in every repo once picked as the unpinned choice', async () => {
    useStore.setState({ addedPackages: [{ name: 'vim', version: '', repo: 'intel-eci' }] })
    mockApi.searchPackages.mockResolvedValue({
      packages: [{ name: 'vim', version: '9.0', repository: 'base', versions: [{ version: '9.0', repository: 'base' }] }],
      total: 1,
    })
    render(<Browser repos={multiRepos} os="ubuntu" baseLock={noLock} />)

    await screen.findByText('vim')
    const latest = screen.getByRole('button', { name: 'Latest' })
    expect(latest.className).toContain('bg-[#0071c5]')
  })
})

describe('"Show only selected"', () => {
  it('refetches page 1 on uncheck instead of the stale page the catalog was left on', async () => {
    useStore.setState({ addedPackages: [{ name: 'curl', version: '', repo: 'base' }] })
    mockApi.searchPackages.mockResolvedValue({
      packages: [{ name: 'zebra', version: '1.0', repository: 'base', versions: [{ version: '1.0', repository: 'base' }] }],
      total: 250,
    })
    render(<Browser repos={repos} os="ubuntu" baseLock={noLock} />)

    await screen.findByText('zebra')
    fireEvent.click(screen.getAllByRole('button', { name: 'Next' })[0])
    await waitFor(() => expect(mockApi.searchPackages).toHaveBeenLastCalledWith(expect.objectContaining({ offset: 100 })))

    fireEvent.click(screen.getByRole('checkbox', { name: /^Show only selected/ }))
    await screen.findByText('curl')

    mockApi.searchPackages.mockClear()
    fireEvent.click(screen.getByRole('checkbox', { name: /^Show only selected/ }))

    await waitFor(() => expect(mockApi.searchPackages).toHaveBeenCalledWith(expect.objectContaining({ offset: 0 })))
  })

  // Template package names are bare, so which repo they belong to is
  // whichever publishes them: the same rule the normal view uses to show
  // them checked. Each repo's own entry for the name is looked up by name.
  const templateLock = (names: string[]): BaseLock => ({
    info: (name) =>
      names.includes(name) ? { locked: true, currentIsFloating: true } : { locked: false, currentIsFloating: false },
    ensureResolved: () => {},
    concreteNames: names,
  })
  // `publishes` maps repo id to the names it carries; the catalog browse is
  // left empty, so only the by-name lookup can put a template row on screen.
  const mockRepos = (publishes: Record<string, string[]>) =>
    mockApi.lookupPackages.mockImplementation(({ repos: ids, names }: { repos: string[]; names: string[] }) => {
      const repo = ids[0]
      return Promise.resolve(
        names
          .filter((n) => (publishes[repo] ?? []).includes(n))
          .map((n) => ({ name: n, version: '1.0', repository: repo, versions: [{ version: '1.0', repository: repo }] })),
      )
    })
  const browse = (label: string) =>
    fireEvent.change(screen.getByRole('combobox', { name: 'Repository' }), {
      target: { value: repos.find((r) => r.displayName === label)!.id },
    })

  it('includes a template package when browsing the base repo that publishes it', async () => {
    mockRepos({ base: ['curl'] })
    render(<Browser repos={repos} os="ubuntu" baseLock={templateLock(['curl'])} />)

    fireEvent.click(await screen.findByRole('checkbox', { name: 'Show only selected (1 selected)' }))

    await screen.findByText('curl')
  })

  it('includes a template package under a non-base repo that publishes it', async () => {
    mockRepos({ base: ['curl'], 'intel-eci': ['librealsense2'] })
    useStore.setState({ enabledRepos: ['intel-eci'] })
    render(<Browser repos={repos} os="ubuntu" baseLock={templateLock(['curl', 'librealsense2'])} />)
    browse('Intel ECI')

    fireEvent.click(await screen.findByRole('checkbox', { name: 'Show only selected (1 selected)' }))

    await screen.findByText('librealsense2')
    expect(screen.queryByText('curl')).toBeNull()
  })

  it('leaves out a template package the browsed repo does not publish, even on the base repo', async () => {
    mockRepos({ base: ['curl'], 'intel-eci': ['librealsense2'] })
    render(<Browser repos={repos} os="ubuntu" baseLock={templateLock(['curl', 'librealsense2'])} />)

    fireEvent.click(await screen.findByRole('checkbox', { name: 'Show only selected (1 selected)' }))

    await screen.findByText('curl')
    expect(screen.queryByText('librealsense2')).toBeNull()
  })

  it('reads "checking…" until the template lookups for the browsed repo land', async () => {
    let release!: () => void
    const gate = new Promise<void>((r) => (release = r))
    mockApi.lookupPackages.mockImplementation(({ names }: { names: string[] }) =>
      gate.then(() => names.map((n) => ({ name: n, version: '1.0', repository: 'base', versions: [] }))),
    )
    render(<Browser repos={repos} os="ubuntu" baseLock={templateLock(['curl'])} />)

    await screen.findByRole('checkbox', { name: 'Show only selected (checking…)' })
    await act(async () => release())
    await screen.findByRole('checkbox', { name: 'Show only selected (1 selected)' })
  })

  it('looks up every template name for the browsed repo in one request, once per repo', async () => {
    mockRepos({ base: ['curl', 'vim'] })
    render(<Browser repos={repos} os="ubuntu" baseLock={templateLock(['curl', 'vim', 'not-there'])} />)

    await screen.findByRole('checkbox', { name: 'Show only selected (2 selected)' })
    expect(mockApi.lookupPackages).toHaveBeenCalledTimes(1)
    expect(mockApi.lookupPackages).toHaveBeenCalledWith({ os: 'ubuntu', repos: ['base'], names: ['curl', 'vim', 'not-there'] })
    // No per-name searches: those queued ahead of the catalog fetch.
    expect(mockApi.searchPackages.mock.calls.every(([args]) => !args.q)).toBe(true)

    // Back to a repo already looked up: answered from the cache.
    fireEvent.change(screen.getByRole('combobox', { name: 'Repository' }), { target: { value: 'intel-eci' } })
    await screen.findByRole('checkbox', { name: 'Show only selected (0 selected)' })
    fireEvent.change(screen.getByRole('combobox', { name: 'Repository' }), { target: { value: 'base' } })
    await screen.findByRole('checkbox', { name: 'Show only selected (2 selected)' })
    expect(mockApi.lookupPackages).toHaveBeenCalledTimes(2)
  })

  it('counts nothing from a repo whose lookup failed, without retrying in a loop', async () => {
    mockApi.lookupPackages.mockRejectedValue(new Error('offline'))
    render(<Browser repos={repos} os="ubuntu" baseLock={templateLock(['curl'])} />)

    await screen.findByRole('checkbox', { name: 'Show only selected (0 selected)' })
    await new Promise((r) => setTimeout(r, 50))
    expect(mockApi.lookupPackages).toHaveBeenCalledTimes(1)
  })

  it('lists a template package the user also added only once', async () => {
    mockRepos({ base: ['curl'] })
    useStore.setState({ addedPackages: [{ name: 'curl', version: '8.5', repo: 'base' }] })
    render(<Browser repos={repos} os="ubuntu" baseLock={templateLock(['curl'])} />)

    fireEvent.click(await screen.findByRole('checkbox', { name: 'Show only selected (1 selected)' }))

    expect(await screen.findAllByText('curl')).toHaveLength(1)
  })
})

describe('selected count', () => {
  it('counts picks from the browsed repo in the "Show only selected" label', async () => {
    useStore.setState({
      addedPackages: [
        { name: 'curl', version: '', repo: 'base' },
        { name: 'docker-ce', version: '', repo: 'docker' },
      ],
    })
    mockApi.searchPackages.mockResolvedValue({
      packages: [{ name: 'zebra', version: '1.0', repository: 'base', versions: [{ version: '1.0', repository: 'base' }] }],
      total: 250,
    })
    render(<Browser repos={repos} os="ubuntu" baseLock={noLock} />)

    await screen.findByRole('checkbox', { name: 'Show only selected (1 selected)' })
  })

  it('shows zero when nothing is picked from the browsed repo', async () => {
    useStore.setState({ addedPackages: [{ name: 'docker-ce', version: '', repo: 'docker' }] })
    mockApi.searchPackages.mockResolvedValue({
      packages: [{ name: 'zebra', version: '1.0', repository: 'base', versions: [{ version: '1.0', repository: 'base' }] }],
      total: 250,
    })
    render(<Browser repos={repos} os="ubuntu" baseLock={noLock} />)

    await screen.findByRole('checkbox', { name: 'Show only selected (0 selected)' })
  })
})
