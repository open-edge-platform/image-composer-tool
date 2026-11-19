// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import { useState } from 'react'
import { canSaveInPlace, saveFile, saveFileAs, type SavedFile } from '../lib/saveFile'

// The Review step in Author mode: the whole template with the user's changes,
// and Save / Save As to keep it on the user's machine.
export function AuthorReview({
  templateName,
  text,
  original,
  removed,
  saved,
  onSaved,
}: {
  // The template's file name, offered as the name to save under.
  templateName: string
  // The edited template.
  text: string
  // The template as it was before any change.
  original: string
  // Template packages the user removed.
  removed: string[]
  // The file saved last this session, and the text saved to it.
  saved: { file: SavedFile; text: string } | null
  onSaved: (saved: { file: SavedFile; text: string }) => void
}) {
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const file = saved?.file ?? null
  const suggestedName = templateName.split('/').pop() || 'template.yml'
  const changed = text !== original
  const unsaved = saved ? text !== saved.text : changed

  const run = async (save: () => Promise<SavedFile | null>) => {
    setSaving(true)
    setError(null)
    try {
      const result = await save()
      if (result) onSaved({ file: result, text })
    } catch (e) {
      setError(`Could not save: ${(e as Error).message}`)
    } finally {
      setSaving(false)
    }
  }

  const button =
    'rounded-md border border-slate-300 bg-white px-5 py-2.5 font-semibold text-[#00285a] hover:border-slate-400 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-50'

  return (
    <div>
      <p className="mb-2 text-xs text-slate-500">
        {changed ? 'Your changes are applied to the template below.' : 'No changes yet. The template below is as written.'}
        {removed.length > 0 && <> Removed: {removed.join(', ')}.</>}
      </p>
      <pre
        aria-label="Edited template"
        className="mb-4 max-h-96 overflow-auto rounded-lg border border-slate-200 bg-slate-50 p-3 font-mono text-[11px] leading-relaxed text-slate-700"
      >
        {text}
      </pre>

      <div className="flex flex-wrap items-center gap-3">
        <button
          type="button"
          className={button}
          disabled={saving}
          onClick={() => run(() => saveFile(text, file, suggestedName))}
          title={file?.handle ? `Overwrite ${file.name}` : 'Choose where to save the template'}
        >
          Save Template
        </button>
        <button
          type="button"
          className={button}
          disabled={saving}
          onClick={() => run(() => saveFileAs(text, file?.name ?? suggestedName))}
        >
          Save As…
        </button>
        <span className="text-xs text-slate-500" role="status">
          {saving
            ? 'Saving…'
            : file
              ? `${file.handle ? 'Saved to' : 'Downloaded'} ${file.name}${unsaved ? ' · unsaved changes' : ''}`
              : ''}
        </span>
      </div>
      {!canSaveInPlace() && (
        <p className="mt-2 text-xs text-slate-400">
          This browser can't write to a file you choose, so Save downloads a copy. Chrome and Edge can save in place.
        </p>
      )}
      {error && <div className="mt-3 rounded bg-red-50 p-3 text-sm text-red-700">{error}</div>}
    </div>
  )
}
