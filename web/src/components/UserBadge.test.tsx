// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import { act, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { resetServerInfo } from '../lib/serverInfo'
import { UserBadge } from './UserBadge'

const getServerInfo = vi.fn()
const me = vi.fn()

vi.mock('../api/client', () => ({
  api: {
    getServerInfo: (...a: unknown[]) => getServerInfo(...a),
    me: (...a: unknown[]) => me(...a),
  },
}))

const info = (mode: string) => ({
  version: '1',
  commit: 'c',
  buildDate: 'd',
  executor: { name: 'local', maxConcurrent: 1 },
  auth: { mode },
})

describe('UserBadge', () => {
  beforeEach(() => {
    resetServerInfo()
    me.mockReset()
  })

  // One implicit local user is nobody to greet.
  it('renders nothing when the server identifies nobody', async () => {
    getServerInfo.mockResolvedValue(info('none'))
    await act(async () => {
      render(<UserBadge />)
    })
    expect(screen.queryByText(/Hello/)).toBeNull()
    expect(me).not.toHaveBeenCalled()
  })

  it('greets the user the server reports behind an authenticating proxy', async () => {
    getServerInfo.mockResolvedValue(info('trusted-header'))
    me.mockResolvedValue({ owner: 'abc123', displayName: 'Alice B', email: 'alice@example.com' })
    await act(async () => {
      render(<UserBadge />)
    })
    expect(await screen.findByText('Alice B')).toBeTruthy()
    expect(screen.getByText(/Hello/)).toBeTruthy()
  })

  // A server without GET /server is an older one: nothing to show.
  it('renders nothing when the server has no /server endpoint', async () => {
    getServerInfo.mockRejectedValue(new Error('404'))
    await act(async () => {
      render(<UserBadge />)
    })
    expect(screen.queryByText(/Hello/)).toBeNull()
  })
})
