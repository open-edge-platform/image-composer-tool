// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Byte-size parsing and formatting for the Advanced tab's Disk step.
//
// ICT templates express sizes as unit-suffixed strings ("4GiB", "32GB",
// "513MiB") and partition offsets in MiB. The schema keeps them as free-form
// strings, so the UI has to do its own arithmetic to translate a size-oriented
// editor into the start/end offsets the builder consumes.

// Binary units are powers of 1024, decimal units powers of 1000 — the same
// distinction the templates make (`4GiB` and `4GB` are different sizes, and
// image-templates/*.yml uses both).
const UNIT_BYTES: Record<string, number> = {
  b: 1,
  kib: 1024,
  mib: 1024 ** 2,
  gib: 1024 ** 3,
  tib: 1024 ** 4,
  kb: 1000,
  mb: 1000 ** 2,
  gb: 1000 ** 3,
  tb: 1000 ** 4,
  // The builder also accepts the short forms; treat them as binary, matching
  // `4G` in the templates' own "4G, 4GB, 4096 MiB also valid" comment.
  k: 1024,
  m: 1024 ** 2,
  g: 1024 ** 3,
  t: 1024 ** 4,
}

// Units offered in the Disk Size dropdown — exactly the three the #822
// prototype offers, in its order.
export const SIZE_UNITS = ['GiB', 'GB', 'MiB'] as const
export type SizeUnit = (typeof SIZE_UNITS)[number]

export const MIB = UNIT_BYTES.mib

// parseSize converts a template size string to bytes. Returns null when the
// string is empty or unparseable, so callers can distinguish "not set" (which
// is legitimate — ISO templates carry no disk.size) from zero.
export function parseSize(value: string | number | null | undefined): number | null {
  if (value === null || value === undefined) return null
  if (typeof value === 'number') return Number.isFinite(value) ? value : null

  const trimmed = value.trim()
  if (trimmed === '') return null

  const m = /^([0-9]*\.?[0-9]+)\s*([a-zA-Z]*)$/.exec(trimmed)
  if (!m) return null

  const amount = Number(m[1])
  if (!Number.isFinite(amount)) return null

  const unit = m[2].toLowerCase()
  // A bare number is bytes, matching `end: "0"` meaning zero rather than 0GiB.
  if (unit === '') return amount

  const factor = UNIT_BYTES[unit]
  return factor === undefined ? null : amount * factor
}

// parseMiB converts a template size string to whole MiB. Partition offsets in
// this repo are always MiB-aligned; a value that isn't is rounded up rather
// than silently truncated, so a computed partition never starts inside the
// previous one.
export function parseMiB(value: string | number | null | undefined): number | null {
  const bytes = parseSize(value)
  return bytes === null ? null : Math.ceil(bytes / MIB)
}

export function unitBytes(unit: SizeUnit): number {
  return UNIT_BYTES[unit.toLowerCase()]
}

// unitOf reports the unit a template size string was written in, so editing
// "32GiB" doesn't silently rewrite it as "32768MiB". Falls back to GiB, which
// is what every disk.size in the repo uses.
export function unitOf(value: string, fallback: SizeUnit = 'GiB'): SizeUnit {
  const m = /^[0-9]*\.?[0-9]+\s*([a-zA-Z]+)$/.exec(value.trim())
  if (!m) return fallback
  const found = SIZE_UNITS.find((u) => u.toLowerCase() === m[1].toLowerCase())
  return found ?? fallback
}

// amountOf reports the numeric part of a template size string, in its own unit.
export function amountOf(value: string): number | null {
  const m = /^([0-9]*\.?[0-9]+)/.exec(value.trim())
  if (!m) return null
  const n = Number(m[1])
  return Number.isFinite(n) ? n : null
}

// formatSize renders an amount + unit back into a template size string.
// Whole numbers lose their trailing ".0" so a round-trip of an untouched
// template value is byte-identical to what the server sent.
export function formatSize(amount: number, unit: SizeUnit): string {
  const rounded = Math.round(amount * 1000) / 1000
  return `${rounded}${unit}`
}
