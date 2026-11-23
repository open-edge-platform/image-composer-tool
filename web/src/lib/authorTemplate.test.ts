// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest'
import { applyAuthorEdits, authoringProblem, packageEntryName } from './authorTemplate'

// The NVL BKC starter template as shipped, and a trimmed copy of the robotics
// template's package list (comments, pins and a glob).
const nvl = `# Starter overlay-mode template for the Nova Lake (NVL) BKC (Best Known
# Configuration) image: layers packages onto an installed Ubuntu 26.04 RAW
# image instead of building one from scratch.
#
# The full NVL BKC package set (Intel kernel, vendor userspace pins, firmware)
# is maintained outside this repository and replaces this file, under the same
# name, wherever it is deployed. This starter keeps the Web UI's
# Platform Enablement / Upstream BKC selection buildable until then.
#
# The baseline path below is a placeholder: point baseline.source at an
# installed Ubuntu 26.04 RAW image on your build host before building.

# AI-searchable metadata for template discovery
metadata:
  description: Overlay an installed Ubuntu 26.04 RAW image with the Nova Lake (NVL) BKC software stack
  use_cases:
    - Enable NVL on an installed Ubuntu 26.04 baseline without rebuilding the OS
  keywords:
    - overlay
    - baseline
    - bkc
    - nvl
    - ubuntu
    - raw

image:
  name: ubuntu26-nvl-bkc-overlay
  version: "26.04"

target:
  os: ubuntu
  dist: ubuntu26
  arch: x86_64
  imageType: raw

# The source image is copied into the build workspace first and is never
# modified in place.
baseline:
  mode: overlay
  source:
    path: /path/to/ubuntu-26.04-base.raw
    format: raw

overlayPolicy:
  packageOperation: additive-only
  conflictPolicy: fail

disk:
  name: nvl-bkc-overlay
  artifacts:
    - type: raw

systemConfig:
  name: nvl-bkc-overlay
  description: NVL BKC enablement on an Ubuntu 26.04 baseline
  packages:
    - vim
`
const robotics = `# Robotics template, trimmed to its package list.
image:
  name: robotics
  version: "24.04"

systemConfig:
  packages:
    # ── Base system ──────────────────────────────────────────────────
    - ubuntu-minimal
    - vim

    # ── Intel OneAPI runtime (pinned to 2025.3.x) ───────────────────
    # Listed before ros-jazzy-desktop so the resolver locks these versions
    # first, preventing transitive deps from pulling 2026.0.0.
    - intel-oneapi-runtime-compilers_2025.3.3-30
    - intel-oneapi-runtime-compilers-common_2025.3.3-30
    - intel-oneapi-runtime-opencl_2025.3.3-30
    - linux-firmware-*  # every firmware package
`
const none = { packages: [], removed: [] }

describe('packageEntryName', () => {
  it('matches the Go rule: split at the first _ followed by a digit', () => {
    expect(packageEntryName('vim')).toBe('vim')
    expect(packageEntryName('openvino_2025.3.0.19807')).toBe('openvino')
    expect(packageEntryName('librealsense2_2.56.5-0~realsense.17055')).toBe('librealsense2')
    expect(packageEntryName('linux-image-*')).toBeNull()
  })
})

describe('applyAuthorEdits', () => {
  it('leaves an unedited template exactly as written', () => {
    expect(applyAuthorEdits(nvl, none)).toBe(nvl)
    expect(applyAuthorEdits(robotics, none)).toBe(robotics)
  })

  it('removes, re-pins and adds packages in place, keeping comments', () => {
    const out = applyAuthorEdits(robotics, {
      packages: [
        { name: 'intel-oneapi-runtime-opencl', version: '2025.3.4-1', repo: 'r' },
        { name: 'htop', version: '', repo: 'r' },
      ],
      removed: ['vim', 'intel-oneapi-runtime-compilers'],
    })
    expect(out).toContain('- intel-oneapi-runtime-opencl_2025.3.4-1')
    expect(out).not.toContain('intel-oneapi-runtime-opencl_2025.3.3-30')
    expect(out).not.toMatch(/- vim\n/)
    expect(out).not.toContain('- intel-oneapi-runtime-compilers_')
    // A package whose name only starts with a removed one stays.
    expect(out).toContain('- intel-oneapi-runtime-compilers-common_2025.3.3-30')
    expect(out).toMatch(/- htop\n/)
    // Every comment line of the original survives.
    for (const line of robotics.split('\n').filter((l) => l.trim().startsWith('#'))) {
      expect(out).toContain(line.trim())
    }
  })

  it('unpins a template package when its version is cleared', () => {
    const out = applyAuthorEdits(robotics, {
      packages: [{ name: 'intel-oneapi-runtime-opencl', version: '', repo: 'r' }],
      removed: [],
    })
    expect(out).toMatch(/- intel-oneapi-runtime-opencl\n/)
  })

  it('sets the image name and disk only when edited', () => {
    const out = applyAuthorEdits(nvl, {
      ...none,
      imageName: 'my-bkc',
      disk: { name: 'd', artifacts: [{ type: 'qcow2' }] },
    })
    expect(out).toContain('name: my-bkc')
    expect(out).toContain('type: qcow2')
    expect(out).toContain('# Starter overlay-mode template')
  })

  it('changes only the edited lines, keeping inline comment spacing', () => {
    const out = applyAuthorEdits(robotics, { packages: [], removed: ['vim'] })
    expect(out).toBe(robotics.replace('    - vim\n', ''))
    expect(out).toContain('- linux-firmware-*  # every firmware package')
  })

  it('keeps a quoted pin quoted', () => {
    const t = 'systemConfig:\n  packages:\n    - "foo_1.0"\n'
    expect(applyAuthorEdits(t, { packages: [{ name: 'foo', version: '2.0', repo: 'r' }], removed: [] })).toBe(
      'systemConfig:\n  packages:\n    - "foo_2.0"\n',
    )
  })

  it('falls back to rewriting the document for a flow-style list', () => {
    const t = 'systemConfig:\n  packages: [vim, curl]\n'
    const out = applyAuthorEdits(t, { packages: [{ name: 'htop', version: '', repo: 'r' }], removed: ['vim'] })
    expect(out).toContain('curl')
    expect(out).toContain('htop')
    expect(out).not.toContain('vim')
  })

  it('adds a repository once, and not when the template lists it', () => {
    const repo = { url: 'https://example.com/apt', codename: 'noble', component: 'main' }
    const once = applyAuthorEdits(nvl, { ...none, repos: [repo, repo] })
    expect(once.match(/https:\/\/example.com\/apt/g)).toHaveLength(1)
    expect(applyAuthorEdits(once, { ...none, repos: [repo] })).toBe(once)
  })
})

describe('authoringProblem', () => {
  it('accepts a standalone template and refuses extends', () => {
    expect(authoringProblem(nvl)).toBeNull()
    expect(authoringProblem('extends: base.yml\nimage:\n  name: x\n')).toMatch(/extends another one/)
    expect(authoringProblem(undefined)).toMatch(/did not send/)
  })
})

describe('baseImage', () => {
  it("replaces the overlay template's base image path in place", () => {
    const out = applyAuthorEdits(nvl, { ...none, baseImage: '/images/ubuntu-26.04-base.raw' })
    expect(out).toBe(nvl.replace('path: /path/to/ubuntu-26.04-base.raw', 'path: /images/ubuntu-26.04-base.raw'))
  })
})
