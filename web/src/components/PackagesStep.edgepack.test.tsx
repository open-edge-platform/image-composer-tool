// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { EdgePack } from '../api/types'
import { PackagesStep } from './PackagesStep'

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

  it('reports Edge Pack unavailable when the first request fails', async () => {
    mockApi.getEdgePack.mockRejectedValue(new Error('404'))

    render(<PackagesStep os="ubuntu24" active basePackages={[]} />)
    await advance(0)
    expect(screen.getByText(UNAVAILABLE)).toBeTruthy()
    expect(screen.queryByText('Loading Edge Pack…')).toBeNull()
  })
})
