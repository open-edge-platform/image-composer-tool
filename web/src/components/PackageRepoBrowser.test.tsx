// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// The Repositories tab's rail gives the base repo "always on, can't uncheck"
// treatment today; a repo in lockedRepoIds (an Edge Pack repo the matched
// template already draws a package from — see templateLockedRepos in
// lib/edgepack.ts) must get the same treatment, for the same reason: the
// build already needs it, whether or not the user has touched this tab.

import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { PackageRepoBrowser } from './PackageRepoBrowser'
import { useStore } from '../store'
import { useVersionResolver } from '../lib/useVersionResolver'
import type { PackageRepo } from '../api/types'
import type { BaseLock } from '../lib/baseLock'

const mockApi = vi.hoisted(() => ({ searchPackages: vi.fn() }))
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
})

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

    const box = screen.getByRole('checkbox', { name: /Intel ECI/ }) as HTMLInputElement
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

    const box = screen.getByRole('checkbox', { name: /^Docker$/ }) as HTMLInputElement
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

    const rail = screen.getByRole('group', { name: 'Package repositories' })
    const order = Array.from(rail.querySelectorAll('input[type="checkbox"]')).map((el) => el.id)
    expect(order).toEqual(['repo-base', 'repo-intel-eci', 'repo-intel-graphics', 'repo-docker'])
  })

  it('defaults to no locked repos when the prop is omitted', () => {
    render(<Browser repos={repos} os="ubuntu" baseLock={noLock} />)

    const box = screen.getByRole('checkbox', { name: /Intel ECI/ }) as HTMLInputElement
    expect(box.checked).toBe(false)
    expect(box.disabled).toBe(false)
  })
})

describe('clicking a repository browses it, independent of its checkbox', () => {
  it('fetches the base repo by default, without any repo checked', async () => {
    render(<Browser repos={repos} os="ubuntu" baseLock={noLock} />)

    await waitFor(() =>
      expect(mockApi.searchPackages).toHaveBeenCalledWith(
        expect.objectContaining({ repos: ['base'] }),
      ),
    )
  })

  it('switches the fetched repo when a different rail row is clicked', async () => {
    render(<Browser repos={repos} os="ubuntu" baseLock={noLock} />)
    await waitFor(() => expect(mockApi.searchPackages).toHaveBeenCalled())
    mockApi.searchPackages.mockClear()

    fireEvent.click(screen.getByText('Docker'))

    await waitFor(() =>
      expect(mockApi.searchPackages).toHaveBeenCalledWith(
        expect.objectContaining({ repos: ['docker'] }),
      ),
    )
  })

  it('does not change the browsed repo when only the checkbox is clicked', async () => {
    render(<Browser repos={repos} os="ubuntu" baseLock={noLock} />)
    await waitFor(() => expect(mockApi.searchPackages).toHaveBeenCalled())
    mockApi.searchPackages.mockClear()

    fireEvent.click(screen.getByRole('checkbox', { name: 'Docker' }))

    expect(useStore.getState().enabledRepos).toContain('docker')
    await waitFor(() =>
      expect(mockApi.searchPackages).not.toHaveBeenCalledWith(
        expect.objectContaining({ repos: ['docker'] }),
      ),
    )
  })

  it('lets browsing an unchecked repo without affecting enabledRepos', () => {
    render(<Browser repos={repos} os="ubuntu" baseLock={noLock} />)

    fireEvent.click(screen.getByText('Docker'))

    expect(useStore.getState().enabledRepos).not.toContain('docker')
  })

  // Regression: Space on the rail row itself still browses it (keyboard
  // parity with the row's own click), but the same Space bubbling up from
  // the nested checkbox must not be hijacked into browsing instead — that
  // would leave keyboard users with no way to toggle an optional repo.
  it('does not browse the repo when Space is pressed on its checkbox', async () => {
    render(<Browser repos={repos} os="ubuntu" baseLock={noLock} />)
    await waitFor(() => expect(mockApi.searchPackages).toHaveBeenCalled())
    mockApi.searchPackages.mockClear()

    fireEvent.keyDown(screen.getByRole('checkbox', { name: 'Docker' }), { key: ' ' })

    await waitFor(() =>
      expect(mockApi.searchPackages).not.toHaveBeenCalledWith(
        expect.objectContaining({ repos: ['docker'] }),
      ),
    )
  })

  it('browses the repo when Space is pressed on the rail row itself', async () => {
    render(<Browser repos={repos} os="ubuntu" baseLock={noLock} />)
    await waitFor(() => expect(mockApi.searchPackages).toHaveBeenCalled())
    mockApi.searchPackages.mockClear()

    fireEvent.keyDown(screen.getByRole('button', { name: /Docker/ }), { key: ' ' })

    await waitFor(() =>
      expect(mockApi.searchPackages).toHaveBeenCalledWith(
        expect.objectContaining({ repos: ['docker'] }),
      ),
    )
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

    fireEvent.click(screen.getByText('Intel ECI'))

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

    fireEvent.click(screen.getByRole('checkbox', { name: 'Show only selected' }))
    await screen.findByText('curl')

    mockApi.searchPackages.mockClear()
    fireEvent.click(screen.getByRole('checkbox', { name: 'Show only selected' }))

    await waitFor(() => expect(mockApi.searchPackages).toHaveBeenCalledWith(expect.objectContaining({ offset: 0 })))
  })

  it('includes template-locked packages when browsing the base repo', async () => {
    const baseLockWithCurl: BaseLock = {
      info: (name) => (name === 'curl' ? { locked: true, currentVersion: '8.5', currentIsFloating: false } : { locked: false, currentIsFloating: false }),
      ensureResolved: () => {},
      concreteNames: ['curl'],
    }
    mockApi.searchPackages.mockResolvedValue({ packages: [], total: 0 })
    render(<Browser repos={repos} os="ubuntu" baseLock={baseLockWithCurl} />)

    fireEvent.click(screen.getByRole('checkbox', { name: 'Show only selected' }))

    await screen.findByText('curl')
  })
})

describe('selected-count badge', () => {
  it('counts picks from the browsed repo out of its catalog, and opens the review on click', async () => {
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

    fireEvent.click(await screen.findByRole('button', { name: '1 of 250 selected' }))

    expect((screen.getByRole('checkbox', { name: 'Show only selected' }) as HTMLInputElement).checked).toBe(true)
    await screen.findByText('curl')
  })

  it('shows zero, disabled, when nothing is picked from the browsed repo', async () => {
    useStore.setState({ addedPackages: [{ name: 'docker-ce', version: '', repo: 'docker' }] })
    mockApi.searchPackages.mockResolvedValue({
      packages: [{ name: 'zebra', version: '1.0', repository: 'base', versions: [{ version: '1.0', repository: 'base' }] }],
      total: 250,
    })
    render(<Browser repos={repos} os="ubuntu" baseLock={noLock} />)

    expect((await screen.findByRole('button', { name: '0 of 250 selected' }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('still shows zero when the full catalog size never arrives', async () => {
    // The full-catalog request is cancelled by ticking "Show frequently used"
    // before it lands, and a curated response never reports the full size.
    const curatedRepos = repos.map((r) => (r.id === 'base' ? { ...r, hasCuratedPackages: true } : r))
    useStore.setState({ addedPackages: [{ name: 'docker-ce', version: '', repo: 'docker' }] })
    mockApi.searchPackages.mockImplementation(({ curated }: { curated?: boolean }) =>
      curated
        ? Promise.resolve({
            packages: [{ name: 'zebra', version: '1.0', repository: 'base', versions: [{ version: '1.0', repository: 'base' }] }],
            total: 1,
          })
        : new Promise(() => {}),
    )
    render(<Browser repos={curatedRepos} os="ubuntu" baseLock={noLock} />)

    fireEvent.click(screen.getByRole('checkbox', { name: 'Show frequently used' }))
    await screen.findByText('zebra')

    expect((screen.getByRole('button', { name: '0 selected' }) as HTMLButtonElement).disabled).toBe(true)
  })
})
