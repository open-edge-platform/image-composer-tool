// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import { useStore } from '../store'

// UndoRedo undoes the last change to packages (removals and Ask AI changes included), repositories, the disk, the
// image name or the base image, and redoes it. One step each way: changes
// made in quick succession (typing a name, Reset) count as one.
export function UndoRedo() {
  const canUndo = useStore((s) => s.undoSnapshot !== null)
  const canRedo = useStore((s) => s.redoSnapshot !== null)
  const undo = useStore((s) => s.undo)
  const redo = useStore((s) => s.redo)
  // Sized like the Back button beside it in the wizard's bottom bar.
  const button =
    'flex items-center gap-1.5 rounded-md border border-slate-300 bg-white px-3 py-2 text-sm font-semibold text-[#00285a] hover:border-slate-400 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-40'
  return (
    <div className="flex items-center gap-2 border-l border-slate-200 pl-3">
      <button type="button" className={button} onClick={undo} disabled={!canUndo} title="Undo your last change">
        <span aria-hidden className="text-base leading-none">
          ↶
        </span>
        Undo last change
      </button>
      <button type="button" className={button} onClick={redo} disabled={!canRedo} title="Redo the change you just undid">
        <span aria-hidden className="text-base leading-none">
          ↷
        </span>
        Redo
      </button>
    </div>
  )
}
