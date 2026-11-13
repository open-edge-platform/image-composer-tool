// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Adding a package from a repository the matched template already installs
// from must not write that repository into enabledRepos: the build already
// reads it, and the Repositories tab shows it locked, so an entry there could
// never be unticked again.

import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { PackagesStep } from './PackagesStep'
import { useStore } from '../store'

const mockApi = vi.hoisted(() => ({
  listPackageRepos: vi.fn(),
  getEdgePack: vi.fn(),
  searchPackages: vi.fn(),
  searchStreamUrl: () => '/search',
}))
vi.mock('../api/client', () => ({ api: mockApi }))

// jsdom has no EventSource; the search only needs its hits/done events.
class FakeEventSource {
  static last: FakeEventSource | null = null
  listeners = new Map<string, (e: MessageEvent) => void>()
  constructor() {
    FakeEventSource.last = this
  }
  addEventListener(type: string, fn: (e: MessageEvent) => void) {
    this.listeners.set(type, fn)
  }
  close() {}
  emit(type: string, data: unknown) {
    this.listeners.get(type)?.({ data: JSON.stringify(data) } as MessageEvent)
  }
}

beforeEach(() => {
  vi.stubGlobal('EventSource', FakeEventSource)
  vi.useFakeTimers()
  useStore.setState({ addedPackages: [], enabledRepos: [] })
  mockApi.listPackageRepos.mockResolvedValue({
    repos: [
      { id: 'ubuntu-noble-base', displayName: 'Ubuntu 24.04', url: 'https://u', enabledByDefault: true },
      { id: 'ros2-jazzy', displayName: 'ROS 2 Jazzy', url: 'https://r', enabledByDefault: false },
    ],
  })
  mockApi.getEdgePack.mockResolvedValue(null)
  mockApi.searchPackages.mockResolvedValue({ packages: [], total: 0 })
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

async function addFromSearch(templateRepos: string[]) {
  render(<PackagesStep os="ubuntu24" active basePackages={[]} templateRepos={templateRepos} />)
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0)
  })
  fireEvent.change(screen.getByPlaceholderText('Search packages across all repositories…'), {
    target: { value: 'ros' },
  })
  await act(async () => {
    await vi.advanceTimersByTimeAsync(300)
  })
  act(() => {
    FakeEventSource.last!.emit('hits', {
      repo: 'ros2-jazzy',
      packages: [{ name: 'ros-jazzy-desktop', version: '1.0', repository: 'ros2-jazzy' }],
    })
  })
  fireEvent.click(screen.getByRole('checkbox', { name: /ros-jazzy-desktop/ }))
}

describe('adding a search hit', () => {
  it("does not enable a repository the template already installs from", async () => {
    await addFromSearch(['ros2-jazzy'])
    expect(useStore.getState().addedPackages.map((p) => p.name)).toEqual(['ros-jazzy-desktop'])
    expect(useStore.getState().enabledRepos).toEqual([])
  })

  it('still enables a repository the template does not use', async () => {
    await addFromSearch([])
    expect(useStore.getState().enabledRepos).toEqual(['ros2-jazzy'])
  })
})
