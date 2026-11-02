// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { HelpMenu, versionLabel, versionReport } from './HelpMenu'
import { UserGuidePage } from './UserGuidePage'
import { guideHref, guideTopicFromHash, guideTopics } from '../guide/topics'

const mockApi = vi.hoisted(() => ({ getVersion: vi.fn() }))
vi.mock('../api/client', () => ({ api: mockApi }))

beforeEach(() => {
  mockApi.getVersion.mockReset()
  mockApi.getVersion.mockResolvedValue({ version: 'ict-2026.3-rc1.1', commit: 'a1b2c3d', buildDate: '2026-11-02' })
})

const pageTitle = (name: string) => screen.getByRole('heading', { level: 1, name })

describe('HelpMenu', () => {
  it('shows and hides a link to the User Guide', () => {
    render(<HelpMenu active={false} />)
    const toggle = screen.getByRole('button', { name: 'Help' })
    expect(toggle.getAttribute('aria-expanded')).toBe('false')
    expect(screen.queryByRole('link')).toBeNull()

    fireEvent.click(toggle)
    expect(toggle.getAttribute('aria-expanded')).toBe('true')
    // A disclosure of links, not an ARIA menu: no arrow-key contract to break.
    expect(screen.queryByRole('menu')).toBeNull()
    const link = screen.getByRole('link', { name: 'User Guide' })
    expect(link.getAttribute('href')).toBe(guideHref(guideTopics[0].id))

    fireEvent.click(link)
    expect(screen.queryByRole('link')).toBeNull()
  })

  it('closes on Escape, returning focus to the button', () => {
    render(<HelpMenu active={false} />)
    const toggle = screen.getByRole('button', { name: 'Help' })
    fireEvent.click(toggle)
    screen.getByRole('link', { name: 'User Guide' }).focus()

    fireEvent.keyDown(window, { key: 'Escape' })
    expect(screen.queryByRole('link')).toBeNull()
    expect(document.activeElement).toBe(toggle)
  })

  it('closes on a press outside', () => {
    render(<HelpMenu active={false} />)
    fireEvent.click(screen.getByRole('button', { name: 'Help' }))
    fireEvent.pointerDown(document.body)
    expect(screen.queryByRole('link')).toBeNull()
  })
})

describe('HelpMenu version', () => {
  const release = { version: 'ict-2026.3-rc1.1', commit: 'a1b2c3d', buildDate: '2026-11-02' }
  const dev = { version: '0.1.0', commit: 'unknown', buildDate: 'unknown' }
  const writeText = vi.fn(async () => {})

  beforeEach(() => {
    writeText.mockClear()
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
  })
  afterEach(() => {
    Object.defineProperty(navigator, 'clipboard', { value: undefined, configurable: true })
  })

  it('formats the badge, dropping the tag prefix', () => {
    expect(versionLabel(release)).toBe('v2026.3-rc1.1')
    expect(versionLabel({ ...release, version: 'v1.2.0' })).toBe('v1.2.0')
    expect(versionReport(release)).toBe('Image Composer Tool v2026.3-rc1.1 (a1b2c3d, 2026-11-02)')
  })

  it('labels an unstamped build as a development build', () => {
    expect(versionLabel(dev)).toBe('Development build')
    expect(versionReport(dev)).toBe('Image Composer Tool v0.1.0 (development build)')
    expect(versionReport({ ...dev, commit: 'a1b2c3d' })).toBe('Image Composer Tool v0.1.0 (a1b2c3d)')
  })

  it('shows a version footer with details on hover, and copies the report', async () => {
    render(<HelpMenu active={false} />)
    fireEvent.click(screen.getByRole('button', { name: 'Help' }))
    const badge = await screen.findByText('v2026.3-rc1.1')
    expect(badge.getAttribute('title')).toBe('a1b2c3d · 2026-11-02')
    expect(screen.getByText('Version')).toBeTruthy()

    const copy = screen.getByRole('button', { name: 'Copy version details' })
    await act(async () => fireEvent.click(copy))
    expect(writeText).toHaveBeenCalledWith('Image Composer Tool v2026.3-rc1.1 (a1b2c3d, 2026-11-02)')
    expect(copy.textContent).toBe('Copied')
    expect(screen.getByText('Version details copied')).toBeTruthy()
  })

  it('offers no copy button where the clipboard is unavailable', async () => {
    Object.defineProperty(navigator, 'clipboard', { value: undefined, configurable: true })
    render(<HelpMenu active={false} />)
    fireEvent.click(screen.getByRole('button', { name: 'Help' }))
    expect(await screen.findByText('v2026.3-rc1.1')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Copy version details' })).toBeNull()
  })

  it('still offers the guide when the version cannot be fetched', async () => {
    mockApi.getVersion.mockRejectedValue(new Error('offline'))
    render(<HelpMenu active={false} />)
    fireEvent.click(screen.getByRole('button', { name: 'Help' }))
    await act(async () => {})
    expect(screen.getByRole('link', { name: 'User Guide' })).toBeTruthy()
    expect(screen.queryByText('Version')).toBeNull()
  })
})

describe('UserGuidePage', () => {
  it('falls back to the first topic for an unknown id', () => {
    render(<UserGuidePage topicId="no-such-topic" />)
    expect(pageTitle(guideTopics[0].title)).toBeTruthy()
  })

  it('links every topic, marking the current one', () => {
    const current = guideTopics[1]
    render(<UserGuidePage topicId={current.id} />)
    const nav = screen.getByRole('navigation', { name: 'User Guide topics' })
    for (const t of guideTopics) {
      const link = nav.querySelector(`a[href="${guideHref(t.id)}"]`)
      expect(link?.getAttribute('aria-current')).toBe(t.id === current.id ? 'page' : null)
    }
  })

  it('links the previous and next topics', () => {
    render(<UserGuidePage topicId={guideTopics[1].id} />)
    expect(
      screen.getByRole('link', { name: `← ${guideTopics[0].title}` }).getAttribute('href'),
    ).toBe(guideHref(guideTopics[0].id))
    expect(
      screen.getByRole('link', { name: `Next: ${guideTopics[2].title} →` }).getAttribute('href'),
    ).toBe(guideHref(guideTopics[2].id))
  })

  it('offers no Next link on the last topic', () => {
    render(<UserGuidePage topicId={guideTopics[guideTopics.length - 1].id} />)
    expect(screen.queryByRole('link', { name: /^Next:/ })).toBeNull()
  })
})

describe('guideTopicFromHash', () => {
  it('reads the topic from a guide address and rejects anything else', () => {
    expect(guideTopicFromHash(guideHref('basic'))).toBe('basic')
    expect(guideTopicFromHash('#guide/')).toBe('')
    expect(guideTopicFromHash('')).toBeNull()
    expect(guideTopicFromHash('#other')).toBeNull()
  })
})
