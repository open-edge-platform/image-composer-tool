// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// The Repositories tab's rail gives the base repo "always on, can't uncheck"
// treatment today; a repo in lockedRepoIds (an Edge Pack repo the matched
// template already draws a package from — see templateLockedRepos in
// lib/edgepack.ts) must get the same treatment, for the same reason: the
// build already needs it, whether or not the user has touched this tab.

import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { PackageRepoBrowser } from './PackageRepoBrowser'
import { useStore } from '../store'
import type { PackageRepo } from '../api/types'
import type { BaseLock } from '../lib/baseLock'

const mockApi = vi.hoisted(() => ({ searchPackages: vi.fn() }))
vi.mock('../api/client', () => ({ api: mockApi }))

const noLock: BaseLock = {
  info: () => ({ locked: false, currentIsFloating: false }),
  ensureResolved: () => {},
  concreteNames: [],
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
      <PackageRepoBrowser
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
      <PackageRepoBrowser
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
      <PackageRepoBrowser
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
      <PackageRepoBrowser
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
    render(<PackageRepoBrowser repos={repos} os="ubuntu" baseLock={noLock} />)

    const box = screen.getByRole('checkbox', { name: /Intel ECI/ }) as HTMLInputElement
    expect(box.checked).toBe(false)
    expect(box.disabled).toBe(false)
  })
})
