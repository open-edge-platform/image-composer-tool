// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import { useStore, type AdvancedMode } from '../store'

const MODES: { id: AdvancedMode; label: string; hint: string }[] = [
  { id: 'customize', label: 'Customize', hint: 'Add your changes on top of the selected template' },
  { id: 'author', label: 'Author', hint: 'Edit the whole template and save it to your machine' },
]

// Lets a user with the authoring capability choose between Customize and
// Author. Renders nothing for everyone else, who only ever customize.
export function AdvancedModeSwitch() {
  const authoring = useStore((s) => s.capabilities.authoring)
  const mode = useStore((s) => s.advancedMode)
  const setMode = useStore((s) => s.setAdvancedMode)
  if (!authoring) return null

  return (
    <div className="mb-6 flex flex-col items-center gap-2">
      <div role="radiogroup" aria-label="Advanced mode" className="inline-flex rounded-md border border-slate-300 bg-white p-0.5">
        {MODES.map((m) => (
          <button
            key={m.id}
            type="button"
            role="radio"
            aria-checked={mode === m.id}
            title={m.hint}
            onClick={() => setMode(m.id)}
            className={
              'rounded px-4 py-1.5 text-sm font-semibold ' +
              (mode === m.id ? 'bg-[#0071c5] text-white' : 'text-[#00285a] hover:bg-slate-100')
            }
          >
            {m.label}
          </button>
        ))}
      </div>
      {mode === 'author' && (
        <p className="text-xs text-slate-500">
          Author mode edits the whole template: you can also remove its packages, and
          save it to your machine from the Review step.
        </p>
      )}
    </div>
  )
}
