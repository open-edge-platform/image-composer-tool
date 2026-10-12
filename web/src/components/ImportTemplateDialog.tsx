// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useRef, useState } from 'react'
import { api } from '../api/client'
import type { ValidationResponse } from '../api/types'
import { ImportedTemplateCard } from './ImportedTemplateCard'
import {
  TEMPLATE_FILE_ACCEPT,
  TemplateParseError,
  parseImportedTemplate,
  readTemplateFile,
  type ImportedTemplate,
} from '../lib/importedTemplate'

interface ImportTemplateDialogProps {
  open: boolean
  onCancel: () => void
  onImport: (template: ImportedTemplate) => void
}

// ImportTemplateDialog takes a template document from the user — dropped, picked
// from disk, or pasted — parses it client-side, and shows what it found before
// the user commits. Nothing leaves the browser here; validating and building the
// imported template is a separate step.
export function ImportTemplateDialog({ open, onCancel, onImport }: ImportTemplateDialogProps) {
  const dialogRef = useRef<HTMLElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const onCancelRef = useRef(onCancel)
  onCancelRef.current = onCancel

  const [mode, setMode] = useState<'file' | 'paste'>('file')
  const [dragging, setDragging] = useState(false)
  const [pasted, setPasted] = useState('')
  const [parsed, setParsed] = useState<ImportedTemplate | null>(null)
  const [error, setError] = useState<string | null>(null)
  // Server verdict for `parsed`. The client parse only proves the document is
  // YAML shaped like a template; this is what the build would actually enforce.
  const [validation, setValidation] = useState<ValidationResponse | null>(null)
  const [validating, setValidating] = useState(false)

  // Each opening starts from scratch: a template left over from a cancelled
  // import must not be silently re-applied by the next one.
  useEffect(() => {
    if (!open) return
    setMode('file')
    setDragging(false)
    setPasted('')
    setParsed(null)
    setError(null)
    setValidation(null)
    setValidating(false)
  }, [open])

  // Validate whatever is currently parsed. Keyed on the document text so a
  // re-pick of the same file doesn't refetch, and guarded against out-of-order
  // responses — pasting types a new document on every keystroke.
  const yamlText = parsed?.yaml
  useEffect(() => {
    if (!yamlText) {
      setValidation(null)
      setValidating(false)
      return
    }
    let cancelled = false
    setValidating(true)
    api
      .validateTemplate(yamlText)
      .then((v) => {
        if (cancelled) return
        setValidation(v)
        setValidating(false)
      })
      .catch((e) => {
        if (cancelled) return
        // The import isn't blocked on an unreachable validator — the build
        // re-runs the same checks and would reject it there anyway.
        setValidation(null)
        setValidating(false)
        setError(`Could not validate the template: ${(e as Error).message}`)
      })
    return () => {
      cancelled = true
    }
  }, [yamlText])

  useEffect(() => {
    if (!open) return
    const trigger = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onCancelRef.current()
    }
    window.addEventListener('keydown', onKeyDown)
    return () => {
      window.removeEventListener('keydown', onKeyDown)
      trigger?.focus()
    }
  }, [open])

  if (!open) return null

  const fail = (e: unknown) => {
    setParsed(null)
    setError(e instanceof TemplateParseError ? e.message : (e as Error).message)
  }

  const takeFile = async (file: File | undefined) => {
    if (!file) return
    try {
      setParsed(await readTemplateFile(file))
      setError(null)
    } catch (e) {
      fail(e)
    }
  }

  const takePaste = (text: string) => {
    setPasted(text)
    if (text.trim() === '') {
      setParsed(null)
      setError(null)
      return
    }
    try {
      setParsed(parseImportedTemplate('pasted-template.yml', text))
      setError(null)
    } catch (e) {
      fail(e)
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/45 p-4"
      onClick={(event) => {
        if (event.target === event.currentTarget) onCancel()
      }}
    >
      <section
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="import-template-dialog-title"
        tabIndex={-1}
        className="max-h-[90vh] w-full max-w-2xl overflow-y-auto rounded-lg bg-white p-6 shadow-xl"
      >
        <div className="mb-4 flex items-start justify-between gap-4">
          <div>
            <h2 id="import-template-dialog-title" className="text-lg font-semibold text-[#00285a]">
              Import Template
            </h2>
            <p className="mt-1 text-sm text-slate-600">
              Use your own image template instead of the pre-configured selections.
              Supported formats: <code>.yml</code>, <code>.yaml</code>.
            </p>
          </div>
          <button
            type="button"
            aria-label="Close import template dialog"
            className="rounded p-1 text-xl leading-none text-slate-500 hover:bg-slate-100 hover:text-slate-800"
            onClick={onCancel}
          >
            ×
          </button>
        </div>

        <div className="mb-4 flex gap-1 border-b border-slate-200 text-sm">
          {(['file', 'paste'] as const).map((m) => (
            <button
              key={m}
              type="button"
              className={
                '-mb-px border-b-2 px-3 py-2 font-medium ' +
                (mode === m
                  ? 'border-[#0071c5] text-[#0071c5]'
                  : 'border-transparent text-slate-500 hover:text-slate-700')
              }
              onClick={() => setMode(m)}
            >
              {m === 'file' ? 'Upload file' : 'Paste YAML'}
            </button>
          ))}
        </div>

        {mode === 'file' ? (
          <div
            onDragOver={(e) => {
              e.preventDefault()
              setDragging(true)
            }}
            onDragLeave={() => setDragging(false)}
            onDrop={(e) => {
              e.preventDefault()
              setDragging(false)
              void takeFile(e.dataTransfer.files?.[0])
            }}
            className={
              'rounded-lg border-2 border-dashed p-8 text-center ' +
              (dragging ? 'border-[#0071c5] bg-blue-50' : 'border-slate-300 bg-slate-50')
            }
          >
            <p className="text-sm text-slate-600">Drag and drop a template file here</p>
            <p className="my-2 text-xs uppercase tracking-wide text-slate-400">or</p>
            <button
              type="button"
              className="rounded-md border border-slate-300 bg-white px-4 py-2 text-sm font-semibold text-[#00285a] hover:bg-slate-50"
              onClick={() => fileInputRef.current?.click()}
            >
              Browse files
            </button>
            <input
              ref={fileInputRef}
              type="file"
              accept={TEMPLATE_FILE_ACCEPT}
              className="hidden"
              onChange={(e) => {
                void takeFile(e.target.files?.[0])
                // Lets the same file be re-picked after a parse failure.
                e.target.value = ''
              }}
            />
          </div>
        ) : (
          <textarea
            className="h-56 w-full resize-y rounded-md border border-slate-300 p-3 font-mono text-xs text-slate-800 focus:border-[#0071c5] focus:outline-none"
            placeholder={'image:\n  name: my-image\n  version: 1.0.0\ntarget:\n  os: …'}
            spellCheck={false}
            value={pasted}
            onChange={(e) => takePaste(e.target.value)}
          />
        )}

        {error && (
          <div role="alert" className="mt-4 rounded bg-red-50 p-3 text-sm text-red-700">
            {error}
          </div>
        )}

        {parsed && (
          <div className="mt-4">
            <ImportedTemplateCard template={parsed} heading="Imported Template" />
          </div>
        )}

        {parsed && <ValidationReport validating={validating} validation={validation} />}

        <div className="mt-5 flex justify-end gap-3">
          <button
            type="button"
            className="rounded-md border border-slate-300 bg-white px-4 py-2 font-semibold text-[#00285a] hover:bg-slate-50"
            onClick={onCancel}
          >
            Cancel
          </button>
          <button
            type="button"
            className="rounded-md bg-[#0071c5] px-4 py-2 font-semibold text-white hover:bg-[#00285a] disabled:cursor-not-allowed disabled:opacity-50"
            disabled={!parsed || validating || validation?.valid === false}
            onClick={() => parsed && onImport(parsed)}
          >
            {validating ? 'Checking…' : 'Use Template'}
          </button>
        </div>
      </section>
    </div>
  )
}

function ValidationReport({
  validating,
  validation,
}: {
  validating: boolean
  validation: ValidationResponse | null
}) {
  if (validating) {
    return <p className="mt-3 text-sm text-slate-500">Validating against the server…</p>
  }
  if (!validation) return null

  const errors = validation.errors ?? []
  const warnings = validation.warnings ?? []

  return (
    <div className="mt-3 space-y-3">
      {errors.length > 0 && (
        <div role="alert" className="rounded bg-red-50 p-3 text-sm text-red-700">
          <p className="font-semibold">
            This template cannot be built ({errors.length} {errors.length === 1 ? 'error' : 'errors'}):
          </p>
          <ul className="mt-1.5 space-y-1">
            {errors.map((i, n) => (
              <li key={`${i.path}-${n}`}>
                {i.path && <code className="font-semibold">{i.path}</code>} {i.message}
              </li>
            ))}
          </ul>
        </div>
      )}
      {warnings.length > 0 && (
        <div className="rounded bg-amber-50 p-3 text-sm text-amber-800">
          <p className="font-semibold">
            {warnings.length} {warnings.length === 1 ? 'warning' : 'warnings'}:
          </p>
          <ul className="mt-1.5 space-y-1">
            {warnings.map((i, n) => (
              <li key={`${i.path}-${n}`}>
                {i.path && <code className="font-semibold">{i.path}</code>} {i.message}
              </li>
            ))}
          </ul>
        </div>
      )}
      {validation.valid && errors.length === 0 && warnings.length === 0 && (
        <p className="text-sm text-green-700">Template is valid.</p>
      )}
    </div>
  )
}
