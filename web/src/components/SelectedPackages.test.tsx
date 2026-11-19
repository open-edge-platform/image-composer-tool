// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// The Selected rail lists only what the Advanced tab adds or changes, not the
// matched template's own packages.

import { act, fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { SelectedPackages } from './SelectedPackages'
import { useStore } from '../store'
import { useVersionResolver } from '../lib/useVersionResolver'
import type { PackageRepo } from '../api/types'

const mockApi = vi.hoisted(() => ({ searchPackages: vi.fn() }))
vi.mock('../api/client', () => ({ api: mockApi }))

// SelectedPackages doesn't own its resolver — PackagesStep.tsx lifts one
// shared useVersionResolver and passes it down, so a floating pick resolves
// the same way everywhere it's shown. This wrapper reproduces that lift.
function Selected({ repos, lockedRepoIds }: { repos: PackageRepo[]; lockedRepoIds?: Set<string> }) {
  const enabledRepos = useStore((s) => s.enabledRepos)
  const enabledRepoIds = repos.filter((r) => r.enabledByDefault || enabledRepos.includes(r.id)).map((r) => r.id)
  const resolver = useVersionResolver('ubuntu', enabledRepoIds)
  return <SelectedPackages repos={repos} resolver={resolver} lockedRepoIds={lockedRepoIds} />
}

const repos: PackageRepo[] = [
  { id: 'base', displayName: 'Ubuntu Base', url: 'http://example/base', enabledByDefault: true },
  { id: 'intel-eci', displayName: 'Intel ECI', url: 'http://example/eci', enabledByDefault: false },
]

beforeEach(() => {
  useStore.setState({ addedPackages: [], enabledRepos: [] })
  mockApi.searchPackages.mockReset()
  mockApi.searchPackages.mockResolvedValue({ packages: [], total: 0 })
})

describe('SelectedPackages', () => {
  it('lists only added packages, with their count, and none of the template’s own', () => {
    useStore.getState().setPackage({ name: 'vim', version: '9.2', repo: 'base' })
    render(<Selected repos={repos} />)

    expect(screen.getByText('Selected (1)')).toBeTruthy()
    expect(screen.queryByText('Already in template')).toBeNull()
    expect(screen.getByLabelText('Remove vim')).toBeTruthy()
  })

  it('says nothing is added yet when nothing is', () => {
    render(<Selected repos={repos} />)
    expect(screen.getByText('No packages added yet.')).toBeTruthy()
  })

  it('Clear removes every added package', () => {
    useStore.getState().setPackage({ name: 'media-ffmpeg', version: '', repo: 'base' })
    render(<Selected repos={repos} />)

    act(() => {
      fireEvent.click(screen.getByRole('button', { name: 'Clear' }))
    })
    expect(useStore.getState().addedPackages).toHaveLength(0)
  })

  // Regression: a repo the matched template's own packages still need (not
  // visible in addedPackages at all) used to be dropped from enabledRepos once
  // the last explicitly-added package from it was removed.
  it('Clear does not release a repo the matched template still locks', () => {
    useStore.setState({ enabledRepos: ['intel-eci'] })
    useStore.getState().setPackage({ name: 'media-ffmpeg', version: '', repo: 'intel-eci' })
    render(<Selected repos={repos} lockedRepoIds={new Set(['intel-eci'])} />)

    fireEvent.click(screen.getByRole('button', { name: 'Clear' }))

    expect(useStore.getState().addedPackages).toHaveLength(0)
    expect(useStore.getState().enabledRepos).toContain('intel-eci')
  })
})
