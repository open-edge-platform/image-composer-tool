// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import { fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { describe, expect, it } from 'vitest'
import { CredentialsDialog } from './CredentialsDialog'
import type { CredentialRequirement } from '../api/types'

const requirements: CredentialRequirement[] = [
  { user: 'admin', sudo: true, required: true, satisfied: false },
]

function DialogHarness() {
  const [open, setOpen] = useState(false)

  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>Open credentials</button>
      <CredentialsDialog
        open={open}
        requirements={requirements}
        credentials={[]}
        onChange={() => {}}
        credentialsReady={false}
        busy={false}
        buildInProgress={false}
        error={null}
        onCancel={() => setOpen(false)}
        onCompose={() => {}}
      />
    </>
  )
}

describe('CredentialsDialog', () => {
  it('focuses inside, traps Tab, and restores focus to its trigger', () => {
    render(<DialogHarness />)
    const trigger = screen.getByRole('button', { name: 'Open credentials' })
    trigger.focus()
    fireEvent.click(trigger)

    const close = screen.getByRole('button', { name: 'Close credentials dialog' })
    const cancel = screen.getByRole('button', { name: 'Cancel' })
    expect(document.activeElement).toBe(close)

    fireEvent.keyDown(document, { key: 'Tab', shiftKey: true })
    expect(document.activeElement).toBe(cancel)
    fireEvent.keyDown(document, { key: 'Tab' })
    expect(document.activeElement).toBe(close)

    fireEvent.click(cancel)
    expect(document.activeElement).toBe(trigger)
  })

  it('announces compose errors as alerts', () => {
    render(
      <CredentialsDialog
        open
        requirements={requirements}
        credentials={[]}
        onChange={() => {}}
        credentialsReady={false}
        busy={false}
        buildInProgress={false}
        error="Build could not be started"
        onCancel={() => {}}
        onCompose={() => {}}
      />,
    )

    expect(screen.getByRole('alert').textContent).toBe('Build could not be started')
  })
})