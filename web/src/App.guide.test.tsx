// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App'
import { guideHref, guideTopics } from './guide/topics'

// The manifest never arrives, so no tab page renders: these tests cover only
// how the address and history move between the tabs and the guide.
vi.mock('./api/client', () => ({
  api: { getManifest: () => new Promise(() => {}), listBuilds: () => new Promise(() => {}) },
}))

// history.back() in jsdom fires popstate/hashchange asynchronously; wait for it.
const back = async () => {
  await act(async () => {
    window.history.back()
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
}

const activeTab = () =>
  screen
    .getAllByRole('button')
    .find((b) => b.className.includes('bg-[#0071c5]') && b.textContent !== '?')?.textContent

const openGuide = async () => {
  await act(async () => {
    window.location.hash = guideHref(guideTopics[0].id)
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
}

describe('App guide history', () => {
  beforeEach(() => window.history.replaceState(null, '', '/'))
  afterEach(() => window.history.replaceState(null, '', '/'))

  it('restores the tab of the history entry Back lands on, not the tab selected last', async () => {
    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: 'Advanced' }))
    await openGuide()
    expect(screen.getByRole('heading', { level: 1, name: guideTopics[0].title })).toBeTruthy()

    // Advanced → guide → Basic → Back → Back must land on Advanced.
    fireEvent.click(screen.getByRole('button', { name: 'Basic' }))
    expect(activeTab()).toBe('Basic')
    await back()
    expect(screen.getByRole('heading', { level: 1, name: guideTopics[0].title })).toBeTruthy()
    await back()
    expect(activeTab()).toBe('Advanced')
  })
})
