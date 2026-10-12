// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest'
import { TemplateParseError, parseImportedTemplate } from './importedTemplate'

const minimal = 'image:\n  name: minimal-desktop-ubuntu\n  version: "24.04"\n'

describe('parseImportedTemplate', () => {
  it('extracts the fields the summary panel shows', () => {
    const t = parseImportedTemplate(
      'generic-handheld-os-template.yml',
      [
        'image:',
        '  name: minimal-desktop-ubuntu',
        '  version: "24.04"',
        'target:',
        '  os: ubuntu',
        '  dist: ubuntu24',
        '  arch: x86_64',
        '  imageType: raw',
        'disk:',
        '  size: 32GiB',
        '  partitionTableType: gpt',
        '  partitions:',
        '    - id: EFI',
        '    - id: rootfs',
        'systemConfig:',
        '  description: Minimal desktop ubuntu image',
        '  hostname: minimal-desktop-ubuntu',
        '  packages:',
        '    - vim',
        '    - bluez',
        '',
      ].join('\n'),
    )
    expect(t.summary).toEqual({
      imageName: 'minimal-desktop-ubuntu',
      imageVersion: '24.04',
      description: 'Minimal desktop ubuntu image',
      hostname: 'minimal-desktop-ubuntu',
      os: 'ubuntu',
      dist: 'ubuntu24',
      architecture: 'x86_64',
      imageType: 'raw',
      extends: '',
      packageCount: 2,
      diskSize: '32GiB',
      partitionTable: 'gpt',
      partitionCount: 2,
    })
  })

  it('keeps the document verbatim', () => {
    const text = '# a comment\n' + minimal
    expect(parseImportedTemplate('t.yml', text).yaml).toBe(text)
  })

  it('accepts a delta template carrying only extends', () => {
    expect(() => parseImportedTemplate('d.yml', 'extends: robotics.yml\n')).not.toThrow()
  })

  it.each([
    ['empty', ''],
    ['not YAML', 'image:\n\tname: tabs-are-invalid\n'],
    ['a bare scalar', 'just a string\n'],
    ['a list', '- one\n- two\n'],
    ['an unrelated document', 'repos:\n  - id: docker\n'],
  ])('rejects %s', (_label, text) => {
    expect(() => parseImportedTemplate('t.yml', text)).toThrow(TemplateParseError)
  })
})

// These mirror service.postedTemplateName's cases: the server derives the build's
// recorded name the same way, and the two labelling the same build differently is
// exactly the bug this guards.
describe('displayName', () => {
  it('derives from image.name', () => {
    expect(parseImportedTemplate('uploaded.yml', minimal).displayName).toBe(
      'minimal-desktop-ubuntu.yml',
    )
  })

  it.each([
    ['no image block', 'target:\n  os: ubuntu\n'],
    ['an empty name', 'image:\n  name: ""\ntarget:\n  os: ubuntu\n'],
    ['a traversal segment', 'image:\n  name: ../../etc/passwd\n'],
    ['a quote', `image:\n  name: 'evil"; drop'\n`],
    ['an over-long name', `image:\n  name: ${'a'.repeat(65)}\n`],
  ])('falls back to the file name given %s', (_label, text) => {
    expect(parseImportedTemplate('uploaded.yml', text).displayName).toBe('uploaded.yml')
  })
})
