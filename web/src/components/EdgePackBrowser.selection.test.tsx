// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Two rules of the Edge Pack tab that only hold once the component is wired to
// the store, so lib/edgepack.test.ts cannot reach them: what clearing a group
// does to the enabled repositories, and what a key press on a domain checkbox
// does to the card wrapped around it.

import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'
import { EdgePackBrowser } from './EdgePackBrowser'
import { useStore } from '../store'
import type { EdgePack } from '../api/types'

const pack: EdgePack = {
  id: 'edge-pack',
  displayName: 'Edge Pack',
  repo: 'intel-eci',
  repoAvailable: true,
  baseRuntimes: [
    {
      id: 'standard',
      displayName: 'Standard',
      available: true,
      requiresRepos: ['intel-graphics'],
      package: { name: 'base-standard' },
    },
    {
      id: 'realtime',
      displayName: 'Real-time',
      available: false,
      unavailableReason: 'No qualified SKU yet',
      package: { name: 'base-realtime' },
    },
  ],
  domains: [
    {
      id: 'media',
      displayName: 'Media',
      available: true,
      packages: [{ name: 'media-ffmpeg', version: '7.1' }],
    },
    {
      id: 'npu',
      displayName: 'NPU',
      available: false,
      unavailableReason: 'Not published for this target',
      packages: [{ name: 'npu-driver' }],
    },
  ],
}

const browser = () => (
  <EdgePackBrowser
    pack={pack}
    repoLabel="Intel ECI"
    repoLabelFor={(id) => id}
    targetLabel="Ubuntu 24.04"
  />
)

beforeEach(() => {
  useStore.setState({ addedPackages: [], enabledRepos: [] })
})

describe('clearing a selection and the enabled repositories', () => {
  it('leaves the pack repository enabled after a domain is cleared', () => {
    // The tab's contract is enabling-only: it switches a repository on when a
    // selection needs it and never switches one off, because the repository
    // may have been enabled on the Repositories tab for something else.
    // Nothing else is selected from intel-eci, so this is the case that
    // actually reaches the store's orphaned-repository cleanup. With another
    // package still resolving from the repo the cleanup is skipped anyway and
    // the test would pass whatever removeGroup asked for.
    useStore.setState({
      addedPackages: [{ name: 'media-ffmpeg', version: '', repo: 'intel-eci' }],
      enabledRepos: ['intel-eci'],
    })
    render(browser())

    fireEvent.click(screen.getByRole('checkbox', { name: 'Clear all Media packages' }))

    expect(useStore.getState().addedPackages).toEqual([])
    expect(useStore.getState().enabledRepos).toContain('intel-eci')
  })

  it('leaves it enabled after the base runtime is unticked too', () => {
    useStore.setState({
      addedPackages: [{ name: 'base-standard', version: '', repo: 'intel-eci' }],
      enabledRepos: ['intel-eci'],
    })
    render(browser())

    fireEvent.click(screen.getByRole('checkbox', { name: /Standard/ }))

    expect(useStore.getState().addedPackages).toEqual([])
    expect(useStore.getState().enabledRepos).toContain('intel-eci')
  })
})

describe('the pack-level checkbox and unavailable domains', () => {
  it('adds only the domains this target publishes', () => {
    useStore.setState({
      addedPackages: [{ name: 'base-standard', version: '', repo: 'intel-eci' }],
      enabledRepos: ['intel-eci'],
    })
    render(browser())

    // Named by its own label text, unlike the per-domain boxes.
    fireEvent.click(screen.getByRole('checkbox', { name: /Domains/ }))

    const names = useStore.getState().addedPackages.map((p) => p.name)
    expect(names).toContain('media-ffmpeg')
    // NPU's own checkbox is locked for this target; the pack-level one must not
    // be a way around that.
    expect(names).not.toContain('npu-driver')
  })
})

describe('keyboard on a domain card', () => {
  it('does not expand the card when Space is pressed on its checkbox', () => {
    useStore.setState({
      addedPackages: [{ name: 'base-standard', version: '', repo: 'intel-eci' }],
      enabledRepos: ['intel-eci'],
    })
    render(browser())

    const card = screen.getByRole('button', { expanded: false, name: /Media/ })
    const box = screen.getByRole('checkbox', { name: 'Select all Media packages' })

    const handled = fireEvent.keyDown(box, { key: ' ', code: 'Space', bubbles: true })

    // Not cancelled, so the browser still performs the checkbox's native
    // toggle; and the card it bubbled through stayed shut.
    expect(handled).toBe(true)
    expect(card.getAttribute('aria-expanded')).toBe('false')
  })

  it('still expands when the card itself takes the key press', () => {
    useStore.setState({
      addedPackages: [{ name: 'base-standard', version: '', repo: 'intel-eci' }],
      enabledRepos: ['intel-eci'],
    })
    render(browser())

    const card = screen.getByRole('button', { expanded: false, name: /Media/ })
    fireEvent.keyDown(card, { key: 'Enter', code: 'Enter' })

    expect(card.getAttribute('aria-expanded')).toBe('true')
  })
})

describe('the base runtime and its prerequisite repositories', () => {
  it('enables the runtime prerequisite alongside the pack repository', () => {
    // The base metapackage pulls in profiles published elsewhere, so a
    // selection of nothing but the runtime still has to switch that repository
    // on — otherwise the build fails resolving what the base installs.
    render(browser())

    fireEvent.click(screen.getByRole('checkbox', { name: /Standard/ }))

    expect(useStore.getState().enabledRepos).toEqual(
      expect.arrayContaining(['intel-eci', 'intel-graphics']),
    )
  })

  it('leaves the prerequisite enabled after the runtime is unticked', () => {
    // Enabling-only, as everywhere else on this tab: the repository may have
    // been switched on from the Repositories tab for something this tab cannot
    // see.
    useStore.setState({
      addedPackages: [{ name: 'base-standard', version: '', repo: 'intel-eci' }],
      enabledRepos: ['intel-eci', 'intel-graphics'],
    })
    render(browser())

    fireEvent.click(screen.getByRole('checkbox', { name: /Standard/ }))

    expect(useStore.getState().enabledRepos).toContain('intel-graphics')
  })

  it('names the prerequisite before anything is picked', () => {
    render(browser())
    expect(screen.getByText(/Also enables intel-graphics/)).toBeTruthy()
  })
})

describe('an unavailable base runtime', () => {
  it('states its reason in the visible text, not only the tooltip', () => {
    render(browser())
    expect(screen.getByText('No qualified SKU yet')).toBeTruthy()
  })

  it('does not satisfy the domain gate when selected from another surface', () => {
    // Real-time is disabled here but is an ordinary package elsewhere.
    useStore.setState({
      addedPackages: [{ name: 'base-realtime', version: '', repo: 'intel-eci' }],
      enabledRepos: ['intel-eci'],
    })
    render(browser())

    const box = screen.getByRole('checkbox', { name: 'Select all Media packages' }) as HTMLInputElement
    expect(box.disabled).toBe(true)
  })
})
