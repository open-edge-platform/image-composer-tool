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
import type { PackageRepo } from '../api/types'

const mockApi = vi.hoisted(() => ({
  listPackageRepos: vi.fn(),
  getEdgePack: vi.fn(),
  searchPackages: vi.fn(),
  lookupPackages: vi.fn(),
  searchStreamUrl: vi.fn((_: { repos?: string[] }) => '/search'),
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
  mockApi.lookupPackages.mockResolvedValue({ packages: [], unavailableRepos: [] })
  mockApi.searchStreamUrl.mockClear()
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

async function addFromSearch(templateRepos: string[], templateOnlyRepos: PackageRepo[] = [], repo = 'ros2-jazzy') {
  render(
    <PackagesStep
      os="ubuntu24"
      active
      basePackages={[]}
      templateRepos={templateRepos}
      templateOnlyRepos={templateOnlyRepos}
    />,
  )
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
      repo,
      packages: [{ name: 'ros-jazzy-desktop', version: '1.0', repository: repo }],
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

// A repository only the matched template declares, which GET /package-repos
// doesn't list (ComposeResponse.templateOnlyRepos).
const snapshot: PackageRepo = {
  id: 'template-0123456789ab',
  displayName: 'af01p-png.devtools.intel.com …/resolute/20261023-0019 (resolute)',
  url: 'https://af01p-png.devtools.intel.com/artifactory/hspe-edge-png-local/ubuntu/resolute/resolute/20261023-0019',
  enabledByDefault: false,
}

describe('a repository only the template declares', () => {
  it('is searched by id, and a hit from it does not enable it', async () => {
    await addFromSearch([snapshot.id], [snapshot], snapshot.id)
    expect(mockApi.searchStreamUrl).toHaveBeenCalledWith(
      expect.objectContaining({ repos: ['ubuntu-noble-base', 'ros2-jazzy', snapshot.id] }),
    )
    expect(useStore.getState().addedPackages).toEqual([{ name: 'ros-jazzy-desktop', version: '', repo: snapshot.id }])
    expect(useStore.getState().enabledRepos).toEqual([])
  })

  it('is listed under Already included in the Repositories tab', async () => {
    render(
      <PackagesStep os="ubuntu24" active basePackages={[]} templateRepos={[snapshot.id]} templateOnlyRepos={[snapshot]} />,
    )
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    fireEvent.click(screen.getByRole('tab', { name: /Repositories/ }))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    const included = screen.getByRole('group', { name: 'Already included' })
    const labels = Array.from(included.querySelectorAll('option')).map((o) => o.textContent)
    expect(labels).toContain(snapshot.displayName)
  })
})
