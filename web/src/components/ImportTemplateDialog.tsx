// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useRef, useState } from 'react'
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

  // Each opening starts from scratch: a template left over from a cancelled
  // import must not be silently re-applied by the next one.
  useEffect(() => {
    if (!open) return
    setMode('file')
    setDragging(false)
    setPasted('')
    setParsed(null)
    setError(null)
  }, [open])

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

        {parsed && <ImportPreview template={parsed} />}

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
            disabled={!parsed}
            onClick={() => parsed && onImport(parsed)}
          >
            Use Template
          </button>
        </div>
      </section>
    </div>
  )
}

function ImportPreview({ template }: { template: ImportedTemplate }) {
  const s = template.summary
  const rows: ([string, string] | null)[] = [
    ['File', template.fileName],
    s.imageName
      ? ['Image', `${s.imageName}${s.imageVersion ? ` (v${s.imageVersion})` : ''}`]
      : null,
    s.description ? ['Description', s.description] : null,
    s.extends ? ['Extends', s.extends] : null,
    s.os || s.dist ? ['Target', [s.dist, s.os].filter(Boolean).join(' / ')] : null,
    s.architecture ? ['Architecture', s.architecture] : null,
    s.imageType ? ['Image Type', s.imageType] : null,
    s.packageCount ? ['Packages', `${s.packageCount} packages`] : null,
    s.partitionCount || s.partitionTable
      ? [
          'Disk',
          [
            s.partitionTable ? s.partitionTable.toUpperCase() : '',
            s.partitionCount ? `${s.partitionCount} partitions` : '',
          ]
            .filter(Boolean)
            .join(', '),
        ]
      : null,
  ]

  return (
    <div className="mt-4 rounded-lg border border-slate-200 bg-slate-50 p-4 text-xs">
      <p className="mb-1.5 text-[10px] font-semibold uppercase tracking-wide text-slate-400">
        Imported Template
      </p>
      <table className="w-full">
        <tbody>
          {rows
            .filter((r): r is [string, string] => r !== null)
            .map(([k, v]) => (
              <tr key={k}>
                <td className="w-28 py-0.5 pr-3 font-semibold text-slate-500">{k}</td>
                <td className="py-0.5 break-all text-slate-700">{v}</td>
              </tr>
            ))}
        </tbody>
      </table>
      <p className="mt-3 text-[11px] text-slate-500">
        The template is checked against the server when you compose it.
      </p>
    </div>
  )
}
