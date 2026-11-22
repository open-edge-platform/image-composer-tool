// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// The Changes rail lists how the user's packages differ from the matched
// template: Added, Modified and (Author mode) Removed, never the template's
// own unchanged packages.

import { act, fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { SelectedPackages } from './SelectedPackages'
import { useStore, noAIChanges } from '../store'
import { useVersionResolver } from '../lib/useVersionResolver'
import type { PackageRepo } from '../api/types'
import type { BaseLock } from '../lib/baseLock'

const mockApi = vi.hoisted(() => ({ searchPackages: vi.fn() }))
vi.mock('../api/client', () => ({ api: mockApi }))

// SelectedPackages doesn't own its resolver — PackagesStep.tsx lifts one
// shared useVersionResolver and passes it down, so a floating pick resolves
// the same way everywhere it's shown. This wrapper reproduces that lift.
function Selected({
  repos,
  lockedRepoIds,
  baseLock,
  authoring,
}: {
  repos: PackageRepo[]
  lockedRepoIds?: Set<string>
  baseLock?: BaseLock
  authoring?: boolean
}) {
  const enabledRepos = useStore((s) => s.enabledRepos)
  const enabledRepoIds = repos.filter((r) => r.enabledByDefault || enabledRepos.includes(r.id)).map((r) => r.id)
  const resolver = useVersionResolver('ubuntu', enabledRepoIds)
  return (
    <SelectedPackages repos={repos} resolver={resolver} lockedRepoIds={lockedRepoIds} baseLock={baseLock} authoring={authoring} />
  )
}

const repos: PackageRepo[] = [
  { id: 'base', displayName: 'Ubuntu Base', url: 'http://example/base', enabledByDefault: true },
  { id: 'intel-eci', displayName: 'Intel ECI', url: 'http://example/eci', enabledByDefault: false },
]

beforeEach(() => {
  useStore.setState({ addedPackages: [], enabledRepos: [], removedPackages: [], aiChanges: noAIChanges })
  mockApi.searchPackages.mockReset()
  mockApi.searchPackages.mockResolvedValue({ packages: [], total: 0 })
})

describe('SelectedPackages', () => {
  it('tags the changes Ask AI made, while they stay as it made them', () => {
    const baseLock = {
      info: (name: string) => ({ locked: name === 'snapd', currentIsFloating: true }),
      ensureResolved: () => {},
      concreteNames: ['snapd'],
    } as unknown as BaseLock
    const docker = { name: 'docker.io', version: '', repo: 'base' }
    useStore.getState().setPackage(docker)
    useStore.getState().setPackage({ name: 'htop', version: '3.3', repo: 'base' })
    useStore.getState().setPackageRemoved('snapd', true)
    useStore.getState().recordAIChanges({
      packages: {
        'docker.io': { after: docker, before: null, wasRemoved: false },
        snapd: { after: 'removed', before: null, wasRemoved: false },
      },
      repos: [],
    })

    render(<Selected repos={repos} baseLock={baseLock} authoring />)
    expect(screen.getAllByTitle('Suggested by Ask AI')).toHaveLength(2)

    act(() => useStore.getState().setPackage({ name: 'docker.io', version: '27.5.1', repo: 'base' }))
    expect(screen.getAllByTitle('Suggested by Ask AI')).toHaveLength(1)

    // Undo AI changes puts snapd back and keeps the hand edits.
    fireEvent.click(screen.getByRole('button', { name: 'Undo AI changes' }))
    expect(useStore.getState().removedPackages).toEqual([])
    expect(useStore.getState().addedPackages.map((p) => p.name).sort()).toEqual(['docker.io', 'htop'])
    expect(screen.queryByRole('button', { name: 'Undo AI changes' })).toBeNull()
  })

  it('lists only added packages, with their count, and none of the template’s own', () => {
    useStore.getState().setPackage({ name: 'vim', version: '9.2', repo: 'base' })
    render(<Selected repos={repos} />)

    expect(screen.getByText('Changes (1)')).toBeTruthy()
    expect(screen.queryByText('Already in template')).toBeNull()
    expect(screen.getByLabelText('Remove vim')).toBeTruthy()
  })

  it('says there are no changes yet, with Reset disabled', () => {
    render(<Selected repos={repos} />)
    expect(screen.getByText('No changes yet.')).toBeTruthy()
    expect((screen.getByRole('button', { name: 'Reset to template defaults' }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('groups changes into Added, Modified and, in Author mode, Removed', () => {
    // The template ships curl pinned to 8.0 and vim unpinned.
    const baseLock = {
      info: (name: string) =>
        name === 'curl'
          ? { locked: true, currentVersion: '8.0', currentIsFloating: false }
          : name === 'vim'
            ? { locked: true, currentIsFloating: true }
            : { locked: false, currentIsFloating: false },
      ensureResolved: () => {},
      concreteNames: ['curl', 'vim'],
    } as unknown as BaseLock
    useStore.getState().setPackage({ name: 'htop', version: '3.3', repo: 'base' })
    useStore.getState().setPackage({ name: 'curl', version: '8.5', repo: 'base' })
    useStore.getState().setPackageRemoved('vim', true)

    const { unmount } = render(<Selected repos={repos} baseLock={baseLock} authoring />)
    expect(screen.getByText('Changes (3)')).toBeTruthy()
    expect(screen.getByText('Added (1)')).toBeTruthy()
    expect(screen.getByText('Modified (1)')).toBeTruthy()
    expect(screen.getByTitle('Template: 8.0 · Yours: 8.5')).toBeTruthy()
    expect(screen.getByText('Removed (1)')).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Put back vim' }))
    expect(useStore.getState().removedPackages).toEqual([])
    fireEvent.click(screen.getByRole('button', { name: 'Undo change to curl' }))
    expect(useStore.getState().addedPackages.map((p) => p.name)).toEqual(['htop'])
    unmount()

    // Outside Author mode removals don't apply, so they aren't listed.
    useStore.getState().setPackageRemoved('vim', true)
    render(<Selected repos={repos} baseLock={baseLock} />)
    expect(screen.queryByText(/^Removed/)).toBeNull()
  })

  it('Reset to template defaults undoes every change', () => {
    useStore.getState().setPackageRemoved('vim', true)
    useStore.getState().setPackage({ name: 'media-ffmpeg', version: '', repo: 'base' })
    render(<Selected repos={repos} />)

    act(() => {
      fireEvent.click(screen.getByRole('button', { name: 'Reset to template defaults' }))
    })
    expect(useStore.getState().addedPackages).toHaveLength(0)
    expect(useStore.getState().removedPackages).toHaveLength(0)
  })

  // Regression: a repo the matched template's own packages still need (not
  // visible in addedPackages at all) used to be dropped from enabledRepos once
  // the last explicitly-added package from it was removed.
  it('Reset does not release a repo the matched template still locks', () => {
    useStore.setState({ enabledRepos: ['intel-eci'] })
    useStore.getState().setPackage({ name: 'media-ffmpeg', version: '', repo: 'intel-eci' })
    render(<Selected repos={repos} lockedRepoIds={new Set(['intel-eci'])} />)

    fireEvent.click(screen.getByRole('button', { name: 'Reset to template defaults' }))

    expect(useStore.getState().addedPackages).toHaveLength(0)
    expect(useStore.getState().enabledRepos).toContain('intel-eci')
  })
})
