// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// History paging tests.
//
// The regression these guard: the sidebar rendered every compose ever recorded,
// so a long-lived install grew an unbounded list. It now shows
// HISTORY_PAGE_SIZE rows per page, newest first, at a fixed height.
import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'
import { HISTORY_PAGE_SIZE, HistorySidebar } from './HistorySidebar'
import { useStore } from '../store'
import type { HistoryItem } from '../api/types'

// No summary, so each row is labelled by its template name — enough to tell the
// rows apart without a manifest.
function items(n: number): HistoryItem[] {
  return Array.from({ length: n }, (_, i) => ({
    id: `b${i}`,
    status: 'success',
    template: `build-${i}.yml`,
    createdAt: new Date(Date.now() - i * 60_000).toISOString(),
  }))
}

// Each history row is a list item; the pager sits outside the list. Returns the
// rows' titles, which here are their template names.
function rowTemplates(): string[] {
  return screen
    .getAllByRole('listitem')
    .map((li) => li.querySelector('span.truncate')?.textContent ?? '')
}

const newer = () => screen.getByRole('button', { name: 'Newer builds' })
const older = () => screen.getByRole('button', { name: 'Older builds' })

function show(list: HistoryItem[], selectedId: string | null = list[0]?.id ?? null) {
  return render(<HistorySidebar items={list} selectedId={selectedId} onSelect={() => {}} />)
}

describe('history paging', () => {
  beforeEach(() => {
    useStore.setState({ manifest: null })
  })

  it('shows every row and no pager when one page holds them all', () => {
    show(items(HISTORY_PAGE_SIZE))

    expect(rowTemplates()).toHaveLength(HISTORY_PAGE_SIZE)
    expect(screen.queryByRole('navigation')).toBeNull()
  })

  it('shows the newest page first', () => {
    show(items(25))

    expect(rowTemplates()).toEqual(items(10).map((it) => it.template))
    expect(screen.getByText('1–10 of 25')).toBeTruthy()
    expect((newer() as HTMLButtonElement).disabled).toBe(true)
    expect((older() as HTMLButtonElement).disabled).toBe(false)
  })

  // The point of paging over "show more": the list never grows past a page.
  it('replaces the rows on each page rather than appending', () => {
    show(items(25))

    fireEvent.click(older())
    expect(rowTemplates()).toHaveLength(HISTORY_PAGE_SIZE)
    expect(rowTemplates()[0]).toBe('build-10.yml')
    expect(screen.getByText('11–20 of 25')).toBeTruthy()

    fireEvent.click(older())
    expect(rowTemplates()).toHaveLength(5)
    expect(screen.getByText('21–25 of 25')).toBeTruthy()
    expect((older() as HTMLButtonElement).disabled).toBe(true)

    fireEvent.click(newer())
    expect(rowTemplates()[0]).toBe('build-10.yml')
  })

  // On first load the active build is selected before the list arrives, and a
  // history build may be selected that is not on page 1.
  it('opens on the page holding the selected build', () => {
    const list = items(25)
    const { rerender } = render(
      <HistorySidebar items={[]} selectedId="b14" onSelect={() => {}} />,
    )
    rerender(<HistorySidebar items={list} selectedId="b14" onSelect={() => {}} />)

    expect(rowTemplates()).toContain('build-14.yml')
    expect(screen.getByText('11–20 of 25')).toBeTruthy()
  })

  // A new compose is prepended and selected by the parent; the list jumps back
  // to page 1 so the build being shown on the right is visible.
  it('returns to the first page when a new compose is selected', () => {
    const list = items(25)
    const { rerender } = show(list)
    fireEvent.click(older())

    const fresh: HistoryItem = { ...list[0], id: 'new', template: 'new.yml' }
    rerender(<HistorySidebar items={[fresh, ...list]} selectedId="new" onSelect={() => {}} />)

    expect(rowTemplates()[0]).toBe('new.yml')
    expect(screen.getByText('1–10 of 26')).toBeTruthy()
  })

  // Polling refreshes the list every few seconds while a build runs; that must
  // not yank the user back from an older page they are browsing.
  it('stays on the browsed page across list refreshes', () => {
    const list = items(25)
    const { rerender } = show(list)
    fireEvent.click(older())

    rerender(<HistorySidebar items={[...list]} selectedId="b0" onSelect={() => {}} />)

    expect(rowTemplates()[0]).toBe('build-10.yml')
  })

  it('clamps to the last page when the list shrinks', () => {
    const { rerender } = show(items(25))
    fireEvent.click(older())
    fireEvent.click(older())

    rerender(<HistorySidebar items={items(12)} selectedId="b0" onSelect={() => {}} />)

    expect(rowTemplates()).toEqual(['build-10.yml', 'build-11.yml'])
    expect(screen.getByText('11–12 of 12')).toBeTruthy()
  })
})
