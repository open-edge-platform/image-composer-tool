// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import type { EdgePack } from '../api/types'
import type { BaseLock } from './baseLock'
import { templateLockedRepos } from './edgepack'

// templateInUseRepos is every repository the matched template already installs
// from: each one in its packageRepositories, through extends and the default
// config (ComposeResponse.templateRepos), and each one it draws an Edge Pack
// package from. The Packages step shows these as always on, like the target's
// base repository, rather than as optional repos to tick — and never writes
// them into enabledRepos, since the build already reads them.
export function templateInUseRepos(
  templateRepos: string[],
  pack: EdgePack | null | undefined,
  baseLock: BaseLock,
): Set<string> {
  return new Set([...templateRepos, ...(pack ? templateLockedRepos(pack, baseLock) : [])])
}
