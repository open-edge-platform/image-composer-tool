// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest'
import { baseImageProblem } from './baseImage'

describe('baseImageProblem', () => {
  it('accepts an absolute path the server has, or a URL', () => {
    expect(baseImageProblem('/images/ubuntu-26.04-base.raw', false)).toBeNull()
    expect(baseImageProblem('https://example.com/base.raw', false)).toBeNull()
  })

  it('names what is wrong otherwise', () => {
    expect(baseImageProblem('  ', false)).toMatch(/^Enter the path/)
    expect(baseImageProblem('images/base.raw', false)).toMatch(/absolute path/)
    expect(baseImageProblem('/path/to/ubuntu-26.04-base.raw', false)).toMatch(/placeholder/)
    expect(baseImageProblem('/images/gone.raw', true)).toMatch(/^Not found on the server/)
  })
})
