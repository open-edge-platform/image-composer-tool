// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Author mode is offered only when the server grants the authoring capability.

import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'
import { AdvancedModeSwitch } from './AdvancedModeSwitch'
import { useStore } from '../store'

beforeEach(() => {
  useStore.setState({ capabilities: { authoring: false }, advancedMode: 'customize' })
})

describe('AdvancedModeSwitch', () => {
  it('is hidden without the authoring capability', () => {
    render(<AdvancedModeSwitch />)
    expect(screen.queryByRole('radiogroup')).toBeNull()
  })

  it('switches between Customize and Author when authoring is granted', () => {
    useStore.getState().setCapabilities({ authoring: true })
    render(<AdvancedModeSwitch />)

    expect(screen.getByRole('radio', { name: 'Customize' }).getAttribute('aria-checked')).toBe('true')
    fireEvent.click(screen.getByRole('radio', { name: 'Author' }))
    expect(useStore.getState().advancedMode).toBe('author')
    expect(screen.getByRole('radio', { name: 'Author' }).getAttribute('aria-checked')).toBe('true')
    expect(screen.getByText(/Author mode edits the whole template/)).toBeTruthy()
  })
})

describe('advanced mode in the store', () => {
  it('refuses Author mode without the capability', () => {
    useStore.getState().setAdvancedMode('author')
    expect(useStore.getState().advancedMode).toBe('customize')
  })

  it('drops back to Customize when the capability is withdrawn', () => {
    useStore.getState().setCapabilities({ authoring: true })
    useStore.getState().setAdvancedMode('author')
    useStore.getState().setCapabilities({ authoring: false })
    expect(useStore.getState().advancedMode).toBe('customize')
  })
})
