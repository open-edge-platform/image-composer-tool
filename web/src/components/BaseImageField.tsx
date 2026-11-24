// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import { useStore } from '../store'

// BaseImageField edits an overlay template's base image: the installed RAW
// image the build layers packages onto, as a path on the server. Shown right
// under the selection on the Basic tab and the Advanced Target step, and in
// the Compose dialog when it is still wrong at Compose time.
export function BaseImageField({ issue, id = 'base-image' }: { issue: string | null; id?: string }) {
  const baseImage = useStore((s) => s.baseImage)
  const setBaseImage = useStore((s) => s.setBaseImage)
  return (
    <div className="mb-4">
      <label htmlFor={id} className="mb-1 block text-sm font-semibold text-[#00285a]">
        Base Image <span className="text-red-600">*</span>
      </label>
      <input
        id={id}
        className={
          'w-full rounded-md border px-3 py-2 font-mono text-sm ' + (issue ? 'border-red-400 bg-red-50' : 'border-slate-300')
        }
        placeholder="/images/ubuntu-26.04-base.raw"
        value={baseImage}
        onChange={(e) => setBaseImage(e.target.value)}
      />
      {issue ? (
        <p className="mt-1 text-xs text-red-700" role="alert">
          {issue}
        </p>
      ) : (
        <p className="mt-1 text-xs text-slate-400">
          This template builds on an installed image. Give its path on the server that runs the build.
        </p>
      )}
    </div>
  )
}
