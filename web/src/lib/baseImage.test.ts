// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest'
import { baseImageProblem } from './baseImage'

describe('baseImageProblem', () => {
  it('accepts an absolute path the server has, or a URL', () => {
    expect(baseImageProblem('/images/ubuntu-26.04-base.raw', false)).toBeNull()
    // A deployment may mount the image at the placeholder path itself.
    expect(baseImageProblem('/path/to/ubuntu-26.04-base.raw', false)).toBeNull()
    expect(baseImageProblem('https://example.com/base.raw', false)).toBeNull()
    // Only the server's answer for a path counts; a URL is fetched at build time.
    expect(baseImageProblem('https://example.com/base.raw', true)).toBeNull()
  })

  it('names what is wrong otherwise', () => {
    expect(baseImageProblem('  ', false)).toMatch(/^Enter the path/)
    expect(baseImageProblem('images/base.raw', false)).toMatch(/absolute path/)
    expect(baseImageProblem('http://example.com/base.raw', false)).toMatch(/must use https/)
    expect(baseImageProblem('https://', false)).toMatch(/must use https and name a host/)
    expect(baseImageProblem('/path/to/ubuntu-26.04-base.raw', true)).toMatch(/placeholder/)
    expect(baseImageProblem('/images/gone.raw', true)).toMatch(/^Not found on the server/)
  })
})
