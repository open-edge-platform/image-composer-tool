// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { EdgePack } from '../api/types'
import { PackagesStep } from './PackagesStep'
import { useStore } from '../store'

const mockApi = vi.hoisted(() => ({
  listPackageRepos: vi.fn(),
  getEdgePack: vi.fn(),
  searchPackages: vi.fn(),
}))

vi.mock('../api/client', () => ({ api: mockApi }))

const UNAVAILABLE = 'Edge Pack is unavailable. Use the Repositories tab to pick packages.'

const pack = (version: string | undefined, versionsPending?: boolean): EdgePack => ({
  id: 'edge-pack',
  displayName: 'Edge Pack',
  repo: 'intel-eci',
  repoAvailable: true,
  baseRuntimes: [{ id: 'standard', displayName: 'Standard', available: true, package: { name: 'base-standard' } }],
  domains: [{ id: 'media', displayName: 'Media', available: true, packages: [{ name: 'media-ffmpeg', version }] }],
  versionsPending,
})

const advance = (ms: number) =>
  act(async () => {
    await vi.advanceTimersByTimeAsync(ms)
  })

const mediaCard = () => screen.queryByRole('button', { expanded: false, name: /Media/ })

beforeEach(() => {
  useStore.setState({ addedPackages: [], enabledRepos: [] })
  vi.useFakeTimers()
  vi.resetAllMocks()
  mockApi.listPackageRepos.mockResolvedValue({
    repos: [{ id: 'intel-eci', displayName: 'Intel ECI', url: 'https://example.com', enabledByDefault: false }],
  })
  mockApi.searchPackages.mockResolvedValue({ packages: [], total: 0 })
})

afterEach(() => {
  vi.useRealTimers()
})

describe('PackagesStep Edge Pack', () => {
  it('shows the loading state until the pack arrives', async () => {
    let resolvePack!: (p: EdgePack) => void
    mockApi.getEdgePack.mockReturnValueOnce(new Promise<EdgePack>((r) => (resolvePack = r)))

    render(<PackagesStep os="ubuntu24" active basePackages={[]} />)
    await advance(0)
    expect(screen.getByText('Loading Edge Pack…')).toBeTruthy()
    expect(mediaCard()).toBeNull()

    await act(async () => {
      resolvePack(pack('7.1'))
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(screen.queryByText('Loading Edge Pack…')).toBeNull()
    expect(mediaCard()).toBeTruthy()
  })

  it('asks again while versions are pending, keeping the pack on screen', async () => {
    mockApi.getEdgePack.mockResolvedValueOnce(pack(undefined, true)).mockResolvedValue(pack('7.1'))

    render(<PackagesStep os="ubuntu24" active basePackages={[]} />)
    await advance(0)
    expect(mockApi.getEdgePack).toHaveBeenCalledTimes(1)
    expect(screen.queryByText('Loading Edge Pack…')).toBeNull()

    await advance(5000)
    expect(mockApi.getEdgePack).toHaveBeenCalledTimes(2)
    fireEvent.click(mediaCard()!)
    expect(screen.getAllByText('7.1').length).toBeGreaterThan(0)

    // The second answer had no pending versions, so nothing more is asked.
    await advance(30000)
    expect(mockApi.getEdgePack).toHaveBeenCalledTimes(2)
  })

  it('keeps the pack on screen when a retry fails', async () => {
    mockApi.getEdgePack.mockResolvedValueOnce(pack(undefined, true)).mockRejectedValue(new Error('offline'))

    render(<PackagesStep os="ubuntu24" active basePackages={[]} />)
    await advance(0)
    await advance(5000)
    expect(mockApi.getEdgePack).toHaveBeenCalledTimes(2)
    expect(mediaCard()).toBeTruthy()
    expect(screen.queryByText(UNAVAILABLE)).toBeNull()
  })

  it('stops asking after the retry limit', async () => {
    mockApi.getEdgePack.mockResolvedValue(pack(undefined, true))

    render(<PackagesStep os="ubuntu24" active basePackages={[]} />)
    await advance(0)
    await advance(5000 * 40)
    expect(mockApi.getEdgePack).toHaveBeenCalledTimes(25)
  })

  it('stops asking once the step unmounts', async () => {
    mockApi.getEdgePack.mockResolvedValue(pack(undefined, true))

    const { unmount } = render(<PackagesStep os="ubuntu24" active basePackages={[]} />)
    await advance(0)
    unmount()
    await advance(30000)
    expect(mockApi.getEdgePack).toHaveBeenCalledTimes(1)
  })

  it('lists what the chosen runtime includes, collapsed and read-only', async () => {
    const p = pack('7.1')
    p.baseRuntimes[0].includes = [
      {
        name: 'gfx-core',
        version: '1.0',
        description: 'graphics core',
        includes: [{ name: 'libmesa' }, { name: 'edge-gfx-dkms', version: '7.0' }],
      },
      { name: 'media-core', includes: [{ name: 'gfx-core' }, { name: 'libva2' }] },
    ]
    mockApi.getEdgePack.mockResolvedValue(p)

    render(<PackagesStep os="ubuntu24" active basePackages={[]} />)
    await advance(0)
    // Nothing is listed until the runtime is actually chosen.
    expect(screen.queryByRole('button', { name: /Included with Standard/ })).toBeNull()

    fireEvent.click(screen.getByRole('checkbox', { name: 'Standard' }))
    // gfx-core appears twice in the tree but is counted once.
    const toggle = screen.getByRole('button', { name: 'Included with Standard (5 packages)' })
    expect(toggle.getAttribute('aria-expanded')).toBe('false')
    expect(screen.queryByRole('checkbox', { name: 'gfx-core' })).toBeNull()

    fireEvent.click(toggle)
    const row = screen.getByRole('checkbox', { name: 'gfx-core' }) as HTMLInputElement
    expect(row.checked).toBe(true)
    expect(row.disabled).toBe(true)
    expect(screen.queryByRole('checkbox', { name: 'libmesa' })).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'Expand gfx-core' }))
    expect(screen.getByRole('checkbox', { name: 'libmesa' })).toBeTruthy()
    expect(screen.getByText('7.0')).toBeTruthy()
  })

  it('labels the included list when the runtime is pinned to another version', async () => {
    const p = pack('7.1')
    p.baseRuntimes[0].package.version = '1.1'
    p.baseRuntimes[0].includes = [{ name: 'gfx-core' }]
    mockApi.getEdgePack.mockResolvedValue(p)

    render(<PackagesStep os="ubuntu24" active basePackages={[]} />)
    await advance(0)
    act(() => {
      useStore.getState().setPackage({ name: 'base-standard', version: '1.0', repo: 'intel-eci' })
    })
    fireEvent.click(screen.getByRole('button', { name: /Included with Standard/ }))
    expect(screen.getByText(/pinned to 1\.0, which may include a different set/)).toBeTruthy()

    // Pinned to the newest version, or unpinned, the list is exact — no note.
    act(() => {
      useStore.getState().setPackage({ name: 'base-standard', version: '1.1', repo: 'intel-eci' })
    })
    expect(screen.queryByText(/which may include a different set/)).toBeNull()
  })

  it('reports Edge Pack unavailable when the first request fails', async () => {
    mockApi.getEdgePack.mockRejectedValue(new Error('404'))

    render(<PackagesStep os="ubuntu24" active basePackages={[]} />)
    await advance(0)
    expect(screen.getByText(UNAVAILABLE)).toBeTruthy()
    expect(screen.queryByText('Loading Edge Pack…')).toBeNull()
  })
})
