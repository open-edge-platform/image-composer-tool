// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useVersionResolver } from './useVersionResolver'

const mockApi = vi.hoisted(() => ({ searchPackages: vi.fn() }))
vi.mock('../api/client', () => ({ api: mockApi }))

beforeEach(() => {
  mockApi.searchPackages.mockReset()
})

describe('useVersionResolver', () => {
  it('discards a result that lands after the repo scope has already changed', async () => {
    let resolveFirst!: (v: { packages: { name: string; version: string; repository: string }[]; total: number }) => void
    mockApi.searchPackages.mockImplementationOnce(
      () => new Promise((resolve) => { resolveFirst = resolve }),
    )
    const { result, rerender } = renderHook(({ repoIds }) => useVersionResolver('ubuntu', repoIds), {
      initialProps: { repoIds: ['base'] },
    })

    act(() => {
      result.current.resolveVersion('vim')
    })

    // The repo scope changes while that lookup is still in flight.
    rerender({ repoIds: ['base', 'intel-eci'] })

    await act(async () => {
      resolveFirst({ packages: [{ name: 'vim', version: '9.0-stale', repository: 'base' }], total: 1 })
    })

    expect(result.current.liveResolved.vim).toBeUndefined()
    expect(result.current.resolvingNames.has('vim')).toBe(false)
  })

  it('marks a failed lookup as attempted so an automatic caller does not retry it forever', async () => {
    mockApi.searchPackages.mockResolvedValue({ packages: [], total: 0 })
    const { result } = renderHook(() => useVersionResolver('ubuntu', ['base']))

    await act(async () => {
      await result.current.resolveVersion('vim')
    })

    expect(result.current.liveResolved.vim).toBeUndefined()
    expect(result.current.attemptedNames.has('vim')).toBe(true)

    // A manual retry (another click) still goes through — attemptedNames
    // only guards an automatic caller, not resolveVersion's own guard.
    mockApi.searchPackages.mockResolvedValueOnce({
      packages: [{ name: 'vim', version: '9.1', repository: 'base' }],
      total: 1,
    })
    await act(async () => {
      await result.current.resolveVersion('vim')
    })
    expect(result.current.liveResolved.vim?.version).toBe('9.1')
  })

  it('resets attemptedNames when the repo scope changes', async () => {
    mockApi.searchPackages.mockResolvedValue({ packages: [], total: 0 })
    const { result, rerender } = renderHook(({ repoIds }) => useVersionResolver('ubuntu', repoIds), {
      initialProps: { repoIds: ['base'] },
    })

    await act(async () => {
      await result.current.resolveVersion('vim')
    })
    expect(result.current.attemptedNames.has('vim')).toBe(true)

    rerender({ repoIds: ['base', 'intel-eci'] })
    await waitFor(() => expect(result.current.attemptedNames.has('vim')).toBe(false))
  })
})
