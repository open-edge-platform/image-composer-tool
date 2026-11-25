// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Author mode edits the template file itself. The YAML is parsed only to find
// where things are; each edit then replaces just its own span of the original
// text. Everything the user didn't touch keeps its exact bytes (comments,
// spacing, quoting), so a saved template differs from the original only where
// the user changed it.

import { isMap, isScalar, isSeq, parseDocument, stringify, type Document, type Node, type Pair, type Scalar } from 'yaml'
import { encodePackage, type AddedPackage } from '../store'

export interface AuthorEdits {
  // The user's package picks: a new package, or a template package given a
  // different version (an empty version leaves it unpinned).
  packages: AddedPackage[]
  // Template packages to drop, by name.
  removed: string[]
  // Set only when the user edited it.
  imageName?: string
  // An overlay template's base image, set only when the user edited it: a
  // path (baseline.source.path) or an https URL (baseline.source.url).
  baseImage?: string
  // The complete disk block, set only when the user edited it.
  disk?: Record<string, unknown>
  // packageRepositories entries the added packages need. One whose url and
  // codename the template already lists is not added again.
  repos?: Record<string, unknown>[]
  // The curated template this one is based on, recorded as metadata.basedOn
  // so that opening the file again selects the same use case.
  basedOn?: string
}

// basedOnOf returns a template's metadata.basedOn, or undefined.
export function basedOnOf(text: string): string | undefined {
  const doc = parseDocument(text)
  const v = doc.getIn(['metadata', 'basedOn'])
  return typeof v === 'string' && v ? v : undefined
}

// packageEntryName returns the package a systemConfig.packages entry names,
// matching packageEntryName in internal/config/merge.go: `name_version` splits
// at the first `_` followed by a digit. A glob names no single package and
// returns null.
export function packageEntryName(entry: string): string | null {
  if (/[*?[]/.test(entry)) return null
  const m = /^(.+?)_(?=\d)/.exec(entry)
  return m ? m[1] : entry
}

// authoringProblem says why a template can't be edited in Author mode, or
// returns null when it can.
export function authoringProblem(text: string | undefined): string | null {
  if (!text) return 'The server did not send the template file, so it cannot be edited here.'
  const doc = parseDocument(text)
  if (doc.errors.length > 0) return `The template is not valid YAML: ${doc.errors[0].message}`
  if (doc.has('extends')) {
    return 'This template extends another one. Author mode edits only templates without extends for now; use Customize instead.'
  }
  return null
}

interface Splice {
  start: number
  end: number
  text: string
}

// The offset where the line holding `pos` starts.
const lineStart = (text: string, pos: number) => text.lastIndexOf('\n', pos - 1) + 1

// A block-style node with a known source range, or null.
function blockNode(node: unknown): (Node & { range: [number, number, number] }) | null {
  if (!node || !(isMap(node) || isSeq(node)) || node.flow || !node.range) return null
  return node as Node & { range: [number, number, number] }
}

// The top-level pair for `key`, or undefined.
function topPair(doc: Document, key: string): Pair<Scalar, unknown> | undefined {
  if (!isMap(doc.contents)) return undefined
  return doc.contents.items.find((p) => isScalar(p.key) && p.key.value === key) as Pair<Scalar, unknown> | undefined
}

// A scalar written back in the style it was written in, so a pin stays quoted
// when it was quoted.
function scalarText(node: Scalar, value: string): string {
  if (node.type === 'QUOTE_DOUBLE') return JSON.stringify(value)
  if (node.type === 'QUOTE_SINGLE') return `'${value.replace(/'/g, "''")}'`
  return /^[\w.+~:@/-]+$/.test(value) && !/^[-:]/.test(value) ? value : JSON.stringify(value)
}

// A YAML block for `value`, each line indented by `indent` spaces.
function block(value: unknown, indent: number): string {
  const pad = ' '.repeat(indent)
  return stringify(value, { lineWidth: 0 })
    .split('\n')
    .map((l) => (l ? pad + l : l))
    .join('\n')
}

// The splices that make the edits, or null when the template's layout needs
// the fallback (a flow-style list, say, or a section that doesn't exist yet).
function splices(text: string, doc: Document, edits: AuthorEdits): Splice[] | null {
  const out: Splice[] = []

  const removed = new Set(edits.removed)
  const picks = new Map(edits.packages.map((p) => [p.name, p]))
  if (removed.size > 0 || picks.size > 0) {
    const seq = blockNode(doc.getIn(['systemConfig', 'packages'], true))
    if (!seq || !isSeq(seq) || seq.items.length === 0) return null
    for (const item of seq.items) {
      if (!isScalar(item) || typeof item.value !== 'string' || !item.range) continue
      const name = packageEntryName(item.value)
      if (name === null) continue
      if (removed.has(name)) {
        // The whole line goes, inline comment included. A comment line above
        // stays: it usually heads a group, and now heads the next entry.
        out.push({ start: lineStart(text, item.range[0]), end: item.range[2], text: '' })
        continue
      }
      const pick = picks.get(name)
      if (pick) {
        out.push({ start: item.range[0], end: item.range[1], text: scalarText(item, encodePackage(pick)) })
        picks.delete(name)
      }
    }
    const added = [...picks.values()].filter((p) => !removed.has(p.name))
    const last = seq.items[seq.items.length - 1] as Node
    if (added.length > 0 && last.range) {
      // New entries go after the last one, written with its "    - " prefix.
      const prefix = text.slice(lineStart(text, last.range[0]), last.range[0])
      if (!/^ *- $/.test(prefix)) return null
      const at = last.range[2]
      const lead = text[at - 1] === '\n' ? '' : '\n'
      out.push({ start: at, end: at, text: lead + added.map((p) => `${prefix}${encodePackage(p)}\n`).join('') })
    }
  }

  if (edits.imageName !== undefined) {
    const node = doc.getIn(['image', 'name'], true)
    if (!isScalar(node) || !node.range) return null
    out.push({ start: node.range[0], end: node.range[1], text: scalarText(node, edits.imageName) })
  }

  if (edits.baseImage !== undefined) {
    // A source sets exactly one of path or url, so going from one to the other
    // rewrites that key's line rather than adding a second.
    const isURL = edits.baseImage.includes('://')
    const src = doc.getIn(['baseline', 'source'], true)
    if (!isMap(src)) return null
    const pairOf = (key: string) => src.items.find((p) => isScalar(p.key) && p.key.value === key)
    const want = pairOf(isURL ? 'url' : 'path')
    const other = pairOf(isURL ? 'path' : 'url')
    if (want && !other && isScalar(want.value) && want.value.range) {
      out.push({ start: want.value.range[0], end: want.value.range[1], text: scalarText(want.value, edits.baseImage) })
    } else if (!want && other && isScalar(other.key) && other.key.range && isScalar(other.value) && other.value.range) {
      out.push({
        start: other.key.range[0],
        end: other.value.range[1],
        text: `${isURL ? 'url' : 'path'}: ${scalarText(other.value, edits.baseImage)}`,
      })
    } else {
      return null
    }
  }

  if (edits.disk !== undefined) {
    const pair = topPair(doc, 'disk')
    const value = blockNode(pair?.value)
    if (!pair?.key.range || !value) return null
    // The whole block is rewritten: the disk editor emits the complete block.
    out.push({ start: lineStart(text, pair.key.range[0]), end: value.range[2], text: stringify({ disk: edits.disk }, { lineWidth: 0 }) })
  }

  if (edits.repos?.length) {
    const pair = topPair(doc, 'packageRepositories')
    if (!pair) {
      // A template with no repositories of its own gets the section at its end.
      const lead = text.endsWith('\n') ? '\n' : '\n\n'
      const repos = edits.repos.filter((r, i, all) => all.findIndex((o) => o.url === r.url && o.codename === r.codename) === i)
      out.push({ start: text.length, end: text.length, text: lead + 'packageRepositories:\n' + block(repos, 2) })
      return out
    }
    const seq = blockNode(pair.value)
    if (!seq || !isSeq(seq) || seq.items.length === 0) return null
    const key = (r: { url?: unknown; codename?: unknown }) => `${r.url ?? ''} ${r.codename ?? ''}`
    const have = new Set(seq.items.filter(isMap).map((m) => key({ url: m.get('url'), codename: m.get('codename') })))
    const fresh: Record<string, unknown>[] = []
    for (const repo of edits.repos) {
      if (have.has(key(repo))) continue
      have.add(key(repo))
      fresh.push(repo)
    }
    const first = seq.items[0] as Node
    if (fresh.length > 0 && first.range) {
      const dash = first.range[0] - lineStart(text, first.range[0]) - 2
      const at = seq.range[2]
      const lead = text[at - 1] === '\n' ? '' : '\n'
      out.push({ start: at, end: at, text: lead + block(fresh, dash) })
    }
  }

  if (edits.basedOn !== undefined) {
    const pair = topPair(doc, 'metadata')
    if (!pair) {
      // No metadata yet: a new block goes above the first top-level key.
      const first = isMap(doc.contents) ? (doc.contents.items[0]?.key as Node | undefined) : undefined
      if (!first?.range) return null
      const at = lineStart(text, first.range[0])
      out.push({ start: at, end: at, text: `metadata:\n  basedOn: ${edits.basedOn}\n\n` })
    } else {
      const map = blockNode(pair.value)
      if (!map || !isMap(map) || map.items.length === 0) return null
      const node = map.get('basedOn', true)
      if (isScalar(node) && node.range) {
        if (node.value !== edits.basedOn) {
          out.push({ start: node.range[0], end: node.range[1], text: scalarText(node, edits.basedOn) })
        }
      } else {
        const firstKey = map.items[0].key as Node
        if (!firstKey.range) return null
        const indent = ' '.repeat(firstKey.range[0] - lineStart(text, firstKey.range[0]))
        const at = map.range[2]
        const lead = text[at - 1] === '\n' ? '' : '\n'
        out.push({ start: at, end: at, text: `${lead}${indent}basedOn: ${edits.basedOn}\n` })
      }
    }
  }

  return out
}

// applyViaDocument is the fallback: it edits the parsed document and prints it
// again. Comments survive, but spacing can change throughout.
function applyViaDocument(doc: Document, edits: AuthorEdits): string {
  const removed = new Set(edits.removed)
  const picks = new Map(edits.packages.map((p) => [p.name, p]))
  if (removed.size > 0 || picks.size > 0) {
    if (!isSeq(doc.getIn(['systemConfig', 'packages'], true))) doc.setIn(['systemConfig', 'packages'], doc.createNode([]))
    const seq = doc.getIn(['systemConfig', 'packages'], true)
    if (isSeq(seq)) {
      seq.items = seq.items.filter((item) => {
        const name = isScalar(item) && typeof item.value === 'string' ? packageEntryName(item.value) : null
        if (name === null) return true
        if (removed.has(name)) return false
        const pick = picks.get(name)
        if (pick && isScalar(item)) {
          item.value = encodePackage(pick)
          picks.delete(name)
        }
        return true
      })
      for (const pick of picks.values()) if (!removed.has(pick.name)) seq.add(doc.createNode(encodePackage(pick)))
    }
  }
  if (edits.repos?.length) {
    if (!isSeq(doc.get('packageRepositories', true))) doc.set('packageRepositories', doc.createNode([]))
    const seq = doc.get('packageRepositories', true)
    if (isSeq(seq)) {
      const key = (r: { url?: unknown; codename?: unknown }) => `${r.url ?? ''} ${r.codename ?? ''}`
      const have = new Set(seq.items.filter(isMap).map((m) => key({ url: m.get('url'), codename: m.get('codename') })))
      for (const repo of edits.repos) {
        if (have.has(key(repo))) continue
        have.add(key(repo))
        seq.add(doc.createNode(repo))
      }
    }
  }
  if (edits.imageName !== undefined) doc.setIn(['image', 'name'], edits.imageName)
  if (edits.baseImage !== undefined) {
    const isURL = edits.baseImage.includes('://')
    doc.setIn(['baseline', 'source', isURL ? 'url' : 'path'], edits.baseImage)
    doc.deleteIn(['baseline', 'source', isURL ? 'path' : 'url'])
  }
  if (edits.disk !== undefined) doc.set('disk', doc.createNode(edits.disk))
  if (edits.basedOn !== undefined) doc.setIn(['metadata', 'basedOn'], edits.basedOn)
  return doc.toString({ lineWidth: 0 })
}

// applyAuthorEdits returns the template text with the edits applied.
export function applyAuthorEdits(text: string, edits: AuthorEdits): string {
  const doc = parseDocument(text)
  if (doc.errors.length > 0) throw new Error(doc.errors[0].message)
  const list = splices(text, doc, edits)
  if (list === null) return applyViaDocument(doc, edits)
  // Last first, so each splice's offsets still hold when it is applied.
  return list
    .sort((a, b) => b.start - a.start)
    .reduce((t, s) => t.slice(0, s.start) + s.text + t.slice(s.end), text)
}
