// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { HelpMenu } from './HelpMenu'
import { UserGuidePage } from './UserGuidePage'
import { guideHref, guideTopicFromHash, guideTopics } from '../guide/topics'

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
