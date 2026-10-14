// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// The Add-a-user section is what lets Basic mode create an account the
// template does not declare. These cover the client-side gate only: the server
// re-checks every rule, but catching a bad entry here is what keeps the
// Compose button from starting a build that is rejected immediately.

import { describe, it, expect } from 'vitest'
import { newUserError, newUsersValid } from './NewUsersSection'
import type { CredentialRequirement, NewUserInput } from '../api/types'

const existing: CredentialRequirement[] = [
  { user: 'admin', sudo: true, required: true, satisfied: true },
]

const valid: NewUserInput = { user: 'operator', password: 'hunter2' }

describe('newUserError', () => {
  it('accepts a named account with a password', () => {
    expect(newUserError(valid, [])).toBeNull()
  })

  it('accepts an account with only an SSH key', () => {
    expect(newUserError({ user: 'operator', sshAuthorizedKey: 'ssh-ed25519 AAAA' }, [])).toBeNull()
  })

  it('rejects an empty name', () => {
    expect(newUserError({ user: '' }, [])).toMatch(/user name/i)
  })

  it('rejects a name the schema would not accept', () => {
    expect(newUserError({ ...valid, user: '-nope' }, [])).toMatch(/letters/i)
  })

  it('rejects a name already taken', () => {
    expect(newUserError({ ...valid, user: 'admin' }, ['admin'])).toMatch(/already taken/i)
  })

  it('rejects an account with no login at all', () => {
    expect(newUserError({ user: 'operator' }, [])).toMatch(/password or add an SSH/i)
  })

  it('rejects a malformed group name', () => {
    expect(newUserError({ ...valid, groups: ['bad group'] }, [])).toMatch(/group/i)
  })

  it('rejects a relative startup script', () => {
    expect(newUserError({ ...valid, startupScript: 'kiosk.sh' }, [])).toMatch(/absolute path/i)
  })
})

describe('newUsersValid', () => {
  it('is true with nothing added', () => {
    expect(newUsersValid([], existing)).toBe(true)
  })

  it('is false when an added account collides with a template account', () => {
    expect(newUsersValid([{ user: 'admin', password: 'hunter2' }], existing)).toBe(false)
  })

  it('is false when two added accounts share a name', () => {
    expect(newUsersValid([valid, { ...valid }], existing)).toBe(false)
  })

  it('is true for distinct, complete accounts', () => {
    expect(newUsersValid([valid, { user: 'kiosk', password: 'hunter2' }], existing)).toBe(true)
  })
})
