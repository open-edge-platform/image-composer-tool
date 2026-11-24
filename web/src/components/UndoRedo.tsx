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
    'flex items-center gap-1 rounded-md border border-slate-300 bg-white px-2.5 py-1 text-xs font-medium text-slate-600 hover:border-slate-400 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-40'
  return (
    <div className="flex gap-2">
      <button type="button" className={button} onClick={undo} disabled={!canUndo} title="Undo the last change">
        <span aria-hidden>↶</span> Undo
      </button>
      <button type="button" className={button} onClick={redo} disabled={!canRedo} title="Redo the change just undone">
        <span aria-hidden>↷</span> Redo
      </button>
    </div>
  )
}
