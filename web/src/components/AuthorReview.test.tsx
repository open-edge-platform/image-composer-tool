// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Save writes back to the file saved last; the first Save, and every Save As,
// asks where. A browser without the save dialog downloads instead.

import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useState } from 'react'
import { AuthorReview } from './AuthorReview'
import type { SavedFile } from '../lib/saveFile'

function Harness({ text }: { text: string }) {
  const [saved, setSaved] = useState<{ file: SavedFile; text: string } | null>(null)
  return (
    <AuthorReview
      templateName="ubuntu26/nvl.yml"
      text={text}
      original="a: 1\n"
      saved={saved}
      onSaved={setSaved}
    />
  )
}

function fakePicker(names: string[]) {
  const written: Record<string, string[]> = {}
  const picker = vi.fn(async () => {
    const name = names.shift() ?? 'x.yml'
    return {
      name,
      createWritable: async () => ({
        write: async (data: string) => {
          ;(written[name] ??= []).push(data)
        },
        close: async () => {},
      }),
    }
  })
  ;(window as unknown as { showSaveFilePicker?: unknown }).showSaveFilePicker = picker
  return { picker, written }
}

afterEach(() => {
  delete (window as unknown as { showSaveFilePicker?: unknown }).showSaveFilePicker
})

describe('AuthorReview', () => {
  it('keeps the edited template in a section that opens', () => {
    render(<Harness text={'a: 2\n'} />)
    expect(screen.queryByLabelText('Edited template')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /Template.*nvl\.yml/ }))
    expect(screen.getByLabelText('Edited template').textContent).toBe('a: 2\n')
  })

  it('asks where on the first Save, then saves in place', async () => {
    const { picker, written } = fakePicker(['mine.yml'])
    render(<Harness text={'a: 2\n'} />)

    fireEvent.click(screen.getByRole('button', { name: 'Save Template' }))
    await waitFor(() => expect(screen.getByRole('status').textContent).toBe('Saved to mine.yml'))
    expect(picker).toHaveBeenCalledWith(expect.objectContaining({ suggestedName: 'nvl.yml' }))

    fireEvent.click(screen.getByRole('button', { name: 'Save Template' }))
    await waitFor(() => expect(written['mine.yml']).toHaveLength(2))
    expect(picker).toHaveBeenCalledTimes(1)
  })

  it('Save As always asks, and later saves go to the new file', async () => {
    const { picker, written } = fakePicker(['one.yml', 'two.yml'])
    render(<Harness text={'a: 2\n'} />)

    fireEvent.click(screen.getByRole('button', { name: 'Save Template' }))
    await waitFor(() => expect(written['one.yml']).toHaveLength(1))
    fireEvent.click(screen.getByRole('button', { name: 'Save As…' }))
    await waitFor(() => expect(screen.getByRole('status').textContent).toBe('Saved to two.yml'))
    fireEvent.click(screen.getByRole('button', { name: 'Save Template' }))
    await waitFor(() => expect(written['two.yml']).toHaveLength(2))
    expect(picker).toHaveBeenCalledTimes(2)
  })

  it('downloads when the browser has no save dialog', async () => {
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    URL.createObjectURL = vi.fn(() => 'blob:x')
    URL.revokeObjectURL = vi.fn()
    render(<Harness text={'a: 2\n'} />)

    fireEvent.click(screen.getByRole('button', { name: 'Save Template' }))
    await waitFor(() => expect(screen.getByRole('status').textContent).toBe('Downloaded nvl.yml'))
    expect(click).toHaveBeenCalled()
    expect(screen.getByText(/Save downloads a copy/)).toBeTruthy()
    click.mockRestore()
  })
})
