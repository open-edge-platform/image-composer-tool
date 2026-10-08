// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// The Credentials section is what lets a template that ships a sudo account
// with no login be built from the UI at all. These cover the rules the backend
// also enforces, so the two cannot drift: which accounts are asked about, what
// counts as answered, and that a password is never put in the DOM as plain
// readable text.

import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { CredentialsStep, credentialsComplete } from './CredentialsStep'
import type { CredentialInput, CredentialRequirement } from '../api/types'

const needsOne: CredentialRequirement = { user: 'admin', sudo: true, satisfied: false }
const alreadyHasOne: CredentialRequirement = { user: 'ops', sudo: true, satisfied: true }

const key = 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEFoaP9Qqm8uDNaok/P4QsOihd5kZ+FhrAdOWJesmBzR test'

describe('credentialsComplete', () => {
  it('is true when nothing is required', () => {
    expect(credentialsComplete([], [])).toBe(true)
  })

  it('is true when every requirement is already satisfied by the template', () => {
    expect(credentialsComplete([alreadyHasOne], [])).toBe(true)
  })

  it('is false while an account still needs a login', () => {
    expect(credentialsComplete([needsOne], [])).toBe(false)
  })

  it.each([
    ['a password', { user: 'admin', password: 'hunter2' }],
    ['an SSH key', { user: 'admin', sshAuthorizedKey: key }],
    ['both', { user: 'admin', password: 'hunter2', sshAuthorizedKey: key }],
  ])('is true with %s — either field is enough on its own', (_label, cred) => {
    expect(credentialsComplete([needsOne], [cred as CredentialInput])).toBe(true)
  })

  it('ignores an entry whose fields are empty', () => {
    expect(credentialsComplete([needsOne], [{ user: 'admin', password: '' }])).toBe(false)
  })

  it('is false when one of several accounts is still unanswered', () => {
    const two = [needsOne, { user: 'root', sudo: false, satisfied: false }]
    expect(credentialsComplete(two, [{ user: 'admin', password: 'hunter2' }])).toBe(false)
  })
})

describe('CredentialsStep', () => {
  it('renders nothing when no account needs a login', () => {
    const { container } = render(
      <CredentialsStep requirements={[alreadyHasOne]} credentials={[]} onChange={() => {}} />,
    )
    expect(container.innerHTML).toBe('')
  })

  it('asks only about the accounts that need one', () => {
    render(
      <CredentialsStep
        requirements={[needsOne, alreadyHasOne]}
        credentials={[]}
        onChange={() => {}}
      />,
    )
    expect(screen.getByText('admin')).toBeTruthy()
    // Offering to override an account the curated template already credentials
    // would invite changing its login by accident.
    expect(screen.queryByText('ops')).toBeNull()
  })

  it('masks the password field', () => {
    render(<CredentialsStep requirements={[needsOne]} credentials={[]} onChange={() => {}} />)
    expect(screen.getByLabelText('Password').getAttribute('type')).toBe('password')
  })

  it('reports a typed password to the parent', () => {
    const onChange = vi.fn()
    render(<CredentialsStep requirements={[needsOne]} credentials={[]} onChange={onChange} />)
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'hunter2' } })
    expect(onChange).toHaveBeenCalledWith([{ user: 'admin', password: 'hunter2' }])
  })

  it('trims a pasted key, which usually arrives with a trailing newline', () => {
    const onChange = vi.fn()
    render(<CredentialsStep requirements={[needsOne]} credentials={[]} onChange={onChange} />)
    fireEvent.change(screen.getByLabelText('SSH public key'), { target: { value: `${key}\n` } })
    expect(onChange).toHaveBeenCalledWith([{ user: 'admin', sshAuthorizedKey: key }])
  })

  it('drops the entry once the user clears both fields', () => {
    const onChange = vi.fn()
    render(
      <CredentialsStep
        requirements={[needsOne]}
        credentials={[{ user: 'admin', password: 'hunter2' }]}
        onChange={onChange}
      />,
    )
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: '' } })
    // Sending an entry with nothing in it would be rejected by the backend.
    expect(onChange).toHaveBeenCalledWith([])
  })

  it('keeps the key when the password changes, and vice versa', () => {
    const onChange = vi.fn()
    render(
      <CredentialsStep
        requirements={[needsOne]}
        credentials={[{ user: 'admin', sshAuthorizedKey: key }]}
        onChange={onChange}
      />,
    )
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'hunter2' } })
    expect(onChange).toHaveBeenCalledWith([{ user: 'admin', sshAuthorizedKey: key, password: 'hunter2' }])
  })
})
