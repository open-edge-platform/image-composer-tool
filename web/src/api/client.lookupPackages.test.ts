// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// lookupPackages asks for a whole name list in as few requests as possible
// (the per-name searches it replaces queued the browser's few connections per
// host), and only ever returns the names it asked for.

import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from './client'

const ok = (packages: { name: string }[]) =>
  Promise.resolve(new Response(JSON.stringify({ query: '', total: packages.length, packages }), { status: 200 }))

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('api.lookupPackages', () => {
  it('sends every name in one request, repeating the names and repos keys', async () => {
    const fetchMock = vi.fn(() => ok([{ name: 'curl' }]))
    vi.stubGlobal('fetch', fetchMock)

    const got = await api.lookupPackages({ os: 'ubuntu24', repos: ['base', 'eci'], names: ['curl', 'vim', 'curl'] })

    expect(fetchMock).toHaveBeenCalledTimes(1)
    const url = new URL(String((fetchMock.mock.calls[0] as unknown[])[0]), 'http://x')
    expect(url.pathname).toBe('/api/v1/packages/search')
    expect(url.searchParams.get('os')).toBe('ubuntu24')
    expect(url.searchParams.getAll('repos')).toEqual(['base', 'eci'])
    expect(url.searchParams.getAll('names')).toEqual(['curl', 'vim'])
    expect(got.packages.map((p) => p.name)).toEqual(['curl'])
    expect(got.unavailableRepos).toEqual([])
  })

  it('splits a long list into batches of 200', async () => {
    const fetchMock = vi.fn(() => ok([]))
    vi.stubGlobal('fetch', fetchMock)

    await api.lookupPackages({ os: 'ubuntu24', repos: ['base'], names: Array.from({ length: 450 }, (_, i) => `pkg-${i}`) })

    const sizes = fetchMock.mock.calls.map((c) => new URL(String((c as unknown[])[0]), 'http://x').searchParams.getAll('names').length)
    expect(sizes).toEqual([200, 200, 50])
  })

  it('drops anything it did not ask for, e.g. a browse page from a backend without `names`', async () => {
    vi.stubGlobal('fetch', vi.fn(() => ok([{ name: '0ad' }, { name: '0ad-data' }, { name: 'vim' }])))

    const got = await api.lookupPackages({ os: 'ubuntu24', repos: ['base'], names: ['vim'] })

    expect(got.packages.map((p) => p.name)).toEqual(['vim'])
  })

  it('reports every repo any batch could not read, once each', async () => {
    const replies = [['eci'], ['eci', 'base']]
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          new Response(JSON.stringify({ query: '', total: 0, packages: [], unavailableRepos: replies.shift() }), { status: 200 }),
        ),
      ),
    )

    const got = await api.lookupPackages({ os: 'ubuntu24', repos: ['base', 'eci'], names: Array.from({ length: 300 }, (_, i) => `p${i}`) })

    expect(got.unavailableRepos).toEqual(['base', 'eci'])
  })
})
