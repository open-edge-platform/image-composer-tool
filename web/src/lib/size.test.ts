// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest'
import { MIB, amountOf, formatSize, parseMiB, parseSize, unitOf } from './size'

describe('parseSize', () => {
  it('distinguishes binary from decimal units', () => {
    // The templates use both, and they are different sizes — 4GiB is the one
    // image-templates/*.yml means when it says "4G, 4GB, 4096 MiB also valid".
    expect(parseSize('4GiB')).toBe(4 * 1024 ** 3)
    expect(parseSize('4GB')).toBe(4 * 1000 ** 3)
    expect(parseSize('4G')).toBe(4 * 1024 ** 3)
    expect(parseSize('512MiB')).toBe(512 * 1024 ** 2)
  })

  it('is case- and whitespace-insensitive', () => {
    expect(parseSize(' 32 gib ')).toBe(32 * 1024 ** 3)
    expect(parseSize('32GIB')).toBe(32 * 1024 ** 3)
  })

  it('treats a bare number as bytes so end: "0" is zero, not 0GiB', () => {
    expect(parseSize('0')).toBe(0)
    expect(parseSize('1048576')).toBe(MIB)
  })

  it('returns null for unset or unparseable values, never 0', () => {
    // ISO templates legitimately carry size: "" — the caller has to be able to
    // tell "no size" from "zero bytes".
    expect(parseSize('')).toBeNull()
    expect(parseSize('   ')).toBeNull()
    expect(parseSize(null)).toBeNull()
    expect(parseSize(undefined)).toBeNull()
    expect(parseSize('big')).toBeNull()
    expect(parseSize('4 quatloos')).toBeNull()
  })
})

describe('parseMiB', () => {
  it('converts every unit family to whole MiB', () => {
    expect(parseMiB('1MiB')).toBe(1)
    expect(parseMiB('513MiB')).toBe(513)
    expect(parseMiB('32GiB')).toBe(32 * 1024)
  })

  it('rounds up so a computed partition never starts inside the previous one', () => {
    expect(parseMiB('1500KiB')).toBe(2)
    expect(parseMiB('1MB')).toBe(1)
  })
})

describe('unitOf / amountOf / formatSize', () => {
  it('round-trips a template size string without rewriting its unit', () => {
    for (const value of ['32GiB', '24GiB', '4GiB', '512MiB', '8GB']) {
      expect(formatSize(amountOf(value)!, unitOf(value))).toBe(value)
    }
  })

  it('falls back to GiB when there is no unit to read', () => {
    expect(unitOf('')).toBe('GiB')
    expect(unitOf('32')).toBe('GiB')
    // TiB is a valid size but not one of the three units the picker offers.
    expect(unitOf('1TiB')).toBe('GiB')
  })

  it('drops a trailing .0 so whole sizes stay whole', () => {
    expect(formatSize(32, 'GiB')).toBe('32GiB')
    expect(formatSize(1.5, 'GiB')).toBe('1.5GiB')
  })
})
