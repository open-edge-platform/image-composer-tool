// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// History row labelling tests.
//
// The regression these guard: the row label was built from use case / platform /
// OS / image type and omitted the SKU entirely, so the two Fed Aero blueprints —
// which differ by nothing else — rendered the same string and could not be told
// apart in the sidebar.
import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'
import { HistorySidebar } from './HistorySidebar'
import { useStore } from '../store'
import type { ComposeSummary, HistoryItem, Manifest } from '../api/types'

// The shipped Basic-tab manifest's labels, trimmed to what a row reads from
// (internal/api/service/data/manifest.yaml). `combinations` is unused here — the
// sidebar only labels ids, it does not re-derive the cascade.
const manifest: Manifest = {
  combinations: [],
  verticals: [{ id: 'fed-aero', displayName: 'Fed Aero' }],
  skus: [
    { id: 'generic-handheld-blueprint', displayName: 'Generic Handheld Blueprint' },
    {
      id: 'generic-companion-os-server-blueprint',
      displayName: 'Generic Companion OS Server Blueprint',
    },
  ],
  platforms: [{ id: 'ptl', displayName: 'PTL (Panther Lake)' }],
  targets: [{ id: 'ubuntu24', displayName: 'Ubuntu 24.04', os: 'ubuntu', arch: 'x86_64' }],
}

// Only the five selection fields are read by the sidebar; the template-derived
// rest of the summary is filled in so the fixture satisfies the type.
function summary(sku: string): ComposeSummary {
  return {
    vertical: 'fed-aero',
    sku,
    platform: 'ptl',
    os: 'ubuntu24',
    imageType: 'raw',
    imageName: 'generic-host-os',
    imageVersion: '1.0.0',
    description: '',
    architecture: 'x86_64',
    kernelVersion: '',
    packageCount: 0,
    diskSize: '16GiB',
    partitionCount: 3,
    partitionTable: 'gpt',
    hostname: 'edge',
  }
}

function item(id: string, over: Partial<HistoryItem> = {}): HistoryItem {
  return {
    id,
    status: 'success',
    template: `${id}-template.yml`,
    createdAt: new Date().toISOString(),
    summary: summary('generic-handheld-blueprint'),
    ...over,
  }
}

// The row is one button, so its accessible name is the whole of its text —
// title, subtext and relative time concatenated.
function rows(): string[] {
  return screen.getAllByRole('button').map((b) => b.textContent ?? '')
}

function show(items: HistoryItem[]) {
  render(
    <HistorySidebar items={items} selectedId={items[0]?.id ?? null} onSelect={() => {}} />,
  )
}

describe('history row labels', () => {
  beforeEach(() => {
    useStore.setState({ manifest })
  })

  it('distinguishes builds that differ only by SKU', () => {
    show([
      item('a', { summary: summary('generic-handheld-blueprint') }),
      item('b', { summary: summary('generic-companion-os-server-blueprint') }),
    ])

    const [a, b] = rows()
    expect(a).toContain('Generic Handheld Blueprint')
    expect(b).toContain('Generic Companion OS Server Blueprint')
    expect(a).not.toBe(b)
  })

  it('shows the rest of the selection as subtext, as display names', () => {
    show([item('a')])

    const [row] = rows()
    expect(row).toContain('Fed Aero')
    expect(row).toContain('Ubuntu 24.04')
    expect(row).toContain('RAW')
  })

  // The glossed platform name ("PTL (Panther Lake)") costs enough of the row's
  // width to truncate the image type away, so the subtext carries the acronym
  // and the tooltip keeps the full name.
  it('abbreviates the platform to its acronym in the subtext only', () => {
    show([item('a')])

    const button = screen.getAllByRole('button')[0]
    const subtext = button.querySelectorAll('div')[1].textContent ?? ''
    expect(subtext).toContain('PTL')
    expect(subtext).not.toContain('Panther Lake')
    expect(button.getAttribute('title')).toContain('Platform: PTL (Panther Lake)')
  })

  it('carries the full selection in the row tooltip, since both lines truncate', () => {
    show([item('a')])

    const title = screen.getAllByRole('button')[0].getAttribute('title') ?? ''
    expect(title).toContain('SKU: Generic Handheld Blueprint')
    expect(title).toContain('Use Case: Fed Aero')
    expect(title).toContain('Platform: PTL (Panther Lake)')
    expect(title).toContain('OS: Ubuntu 24.04')
    expect(title).toContain('Image Type: RAW')
  })

  it('promotes the use case to the title when the build recorded no SKU', () => {
    show([item('a', { summary: { ...summary(''), sku: '' } })])

    const [row] = rows()
    expect(row).toContain('Fed Aero')
    expect(row).not.toContain('SKU')
  })

  it('falls back to the template name for a build with no summary', () => {
    show([item('a', { summary: undefined })])

    expect(rows()[0]).toContain('a-template.yml')
  })

  it('renders raw ids rather than blanks before the manifest has loaded', () => {
    useStore.setState({ manifest: null })
    show([item('a')])

    const [row] = rows()
    expect(row).toContain('generic-handheld-blueprint')
    expect(row).toContain('fed-aero')
  })

  it('renders a retired SKU id the manifest no longer lists', () => {
    show([item('a', { summary: summary('edge-node-infrastructure-blueprint-bkc') })])

    expect(rows()[0]).toContain('edge-node-infrastructure-blueprint-bkc')
  })
})
