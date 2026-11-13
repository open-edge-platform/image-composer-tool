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

  // The account that blocks a build is usually scrolled out of view, so the
  // reason has to be stated next to the button that is refusing to work.
  it('names the account and the added user that block composing', () => {
    render(
      <CredentialsDialog
        open
        requirements={requirements}
        credentials={[]}
        onChange={() => {}}
        newUsers={[{ user: 'tester' }]}
        onNewUsersChange={() => {}}
        credentialsReady={false}
        busy={false}
        buildInProgress={false}
        error={null}
        onCancel={() => {}}
        onCompose={() => {}}
      />,
    )

    expect(screen.getByText(/admin needs a password or an SSH public key/i)).toBeTruthy()
    expect(screen.getByText(/New user tester: Set a password or add an SSH public key/i)).toBeTruthy()
  })

  it('asks for a GitHub token only for a template that uses the OEP Installer', () => {
    const props = {
      open: true,
      requirements,
      credentials: [],
      onChange: () => {},
      onOEPInstallerTokenChange: () => {},
      credentialsReady: true,
      busy: false,
      buildInProgress: false,
      error: null,
      onCancel: () => {},
      onCompose: () => {},
    }
    const { rerender } = render(<CredentialsDialog {...props} oepInstaller={{ uses: false, serverToken: false }} />)
    expect(screen.queryByLabelText('GitHub token for the OEP Installer')).toBeNull()

    rerender(<CredentialsDialog {...props} oepInstaller={{ uses: true, serverToken: false }} />)
    const field = screen.getByLabelText<HTMLInputElement>('GitHub token for the OEP Installer')
    expect(field.type).toBe('password')
    expect(field.autocomplete).toBe('off')
    expect(screen.getByText(/private repository/)).toBeTruthy()

    rerender(<CredentialsDialog {...props} oepInstaller={{ uses: true, serverToken: true }} />)
    expect(screen.getByText(/token the server is configured with/)).toBeTruthy()
  })
})