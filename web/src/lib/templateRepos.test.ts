// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest'
import type { EdgePack } from '../api/types'
import type { BaseLock } from './baseLock'
import { templateInUseRepos } from './templateRepos'

const pack: EdgePack = {
  id: 'edge-pack',
  displayName: 'Edge Pack',
  repo: 'edgepack',
  repoAvailable: true,
  baseRuntimes: [{ id: 'standard', displayName: 'Standard', available: true, package: { name: 'base-standard' } }],
  domains: [],
}

const lockOf = (...names: string[]): BaseLock => ({
  info: (name) => ({ locked: names.includes(name), currentIsFloating: false }),
  ensureResolved: () => {},
  concreteNames: names,
})

describe('templateInUseRepos', () => {
  it("includes every repository in the template's packageRepositories", () => {
    expect([...templateInUseRepos(['ros2-jazzy', 'gazebo'], null, lockOf())].sort()).toEqual(['gazebo', 'ros2-jazzy'])
  })

  it('adds the repository the template draws an Edge Pack package from', () => {
    expect([...templateInUseRepos(['ros2-jazzy'], pack, lockOf('base-standard'))].sort()).toEqual([
      'edgepack',
      'ros2-jazzy',
    ])
  })

  it('is empty for a template that uses no repository beyond the base', () => {
    expect(templateInUseRepos([], pack, lockOf()).size).toBe(0)
  })
})
