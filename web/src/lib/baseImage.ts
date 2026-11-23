// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// An overlay template builds on a base image: baseline.source.path, a RAW
// image on the server that runs the build. baseImageProblem says what's wrong
// with the path on screen, or returns null when a build can use it.
// missingOnServer is the server's answer for this path; until it has one, the
// path is taken as found.

// The placeholder the shipped overlay templates carry until a real path is set.
const PLACEHOLDER = '/path/to/'

export function baseImageProblem(path: string, missingOnServer: boolean): string | null {
  const p = path.trim()
  if (!p) return 'Enter the path of the base image on the server.'
  if (/^https?:\/\//.test(p)) return null
  if (!p.startsWith('/')) return 'Use an absolute path on the server, such as /images/ubuntu-26.04-base.raw.'
  // Whether the server has the file decides: a deployment may well mount the
  // image at the placeholder path itself.
  if (!missingOnServer) return null
  if (p.startsWith(PLACEHOLDER)) {
    return "This is the template's placeholder, and the server has no image there. Enter the path of the installed baseline RAW image on the server."
  }
  return 'Not found on the server. Check the path, and that the image is mounted into the container.'
}
