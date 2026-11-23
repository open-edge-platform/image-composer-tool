// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// An overlay template builds on a base image: baseline.source.path, a RAW
// image on the server that runs the build. baseImageProblem says what's wrong
// with the path on screen, or returns null when a build can use it.

// The placeholder the shipped overlay templates carry until a real path is set.
const PLACEHOLDER = '/path/to/'

export function baseImageProblem(path: string, missingOnServer: boolean): string | null {
  const p = path.trim()
  if (!p) return 'Enter the path of the base image on the server.'
  if (/^https?:\/\//.test(p)) return null
  if (!p.startsWith('/')) return 'Use an absolute path on the server, such as /images/ubuntu-26.04-base.raw.'
  if (p.startsWith(PLACEHOLDER)) {
    return "This is the template's placeholder. Enter the path of the installed baseline RAW image on the server."
  }
  if (missingOnServer) return 'Not found on the server. Check the path, and that the image is mounted into the container.'
  return null
}
