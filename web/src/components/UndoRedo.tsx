// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import { useStore } from '../store'

// UndoRedo undoes the last change to packages (removals included), repositories, the disk, the
// image name or the base image, and redoes it. One step each way: changes
// made in quick succession (typing a name, Reset) count as one.
export function UndoRedo() {
  const canUndo = useStore((s) => s.undoSnapshot !== null)
  const canRedo = useStore((s) => s.redoSnapshot !== null)
  const undo = useStore((s) => s.undo)
  const redo = useStore((s) => s.redo)
  const button =
    'flex items-center gap-1.5 rounded-md border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-[#00285a] shadow-sm hover:border-[#0071c5] hover:bg-[#eef4fb] disabled:cursor-not-allowed disabled:opacity-40 disabled:shadow-none'
  return (
    <div className="flex items-center gap-2">
      <button type="button" className={button} onClick={undo} disabled={!canUndo} title="Undo your last change">
        <span aria-hidden className="text-sm leading-none">
          ↶
        </span>
        Undo last change
      </button>
      <button type="button" className={button} onClick={redo} disabled={!canRedo} title="Redo the change you just undid">
        <span aria-hidden className="text-sm leading-none">
          ↷
        </span>
        Redo
      </button>
    </div>
  )
}
