// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import { act, fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'
import { UndoRedo } from './UndoRedo'
import { useStore } from '../store'

beforeEach(() => {
  useStore.setState({ addedPackages: [], enabledRepos: [], undoSnapshot: null, redoSnapshot: null, lastEditAt: 0 })
})

describe('UndoRedo', () => {
  it('is disabled until there is a change, then undoes and redoes it', () => {
    render(<UndoRedo />)
    const undo = () => screen.getByRole<HTMLButtonElement>('button', { name: /Undo last change/ })
    const redo = () => screen.getByRole<HTMLButtonElement>('button', { name: /Redo/ })
    expect(undo().disabled).toBe(true)
    expect(redo().disabled).toBe(true)

    fireEvent.click(undo()) // no-op while disabled
    act(() => useStore.getState().setPackage({ name: 'vim', version: '', repo: 'base' }))
    expect(undo().disabled).toBe(false)

    fireEvent.click(undo())
    expect(useStore.getState().addedPackages).toEqual([])
    expect(redo().disabled).toBe(false)
    fireEvent.click(redo())
    expect(useStore.getState().addedPackages.map((p) => p.name)).toEqual(['vim'])
  })
})

describe('Advanced mode', () => {
  it('is Author whenever authoring is allowed, and Customize otherwise; nothing switches it', () => {
    useStore.getState().setCapabilities({ authoring: true })
    expect(useStore.getState().advancedMode).toBe('author')
    useStore.getState().setCapabilities({ authoring: false })
    expect(useStore.getState().advancedMode).toBe('customize')
    expect('setAdvancedMode' in useStore.getState()).toBe(false)
  })
})
