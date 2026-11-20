// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Saving a text file to the user's machine. Where the browser has the File
// System Access API (Chrome and Edge), Save As opens the system save dialog and
// Save writes back to the file chosen there. Elsewhere both download the file.

interface Writable {
  write(data: string): Promise<void>
  close(): Promise<void>
}

export interface FileHandle {
  name: string
  createWritable(): Promise<Writable>
}

type SaveFilePicker = (options: {
  suggestedName: string
  types: { description: string; accept: Record<string, string[]> }[]
}) => Promise<FileHandle>

// A file saved this session: its name, and the handle to write it again when
// the browser gave one.
export interface SavedFile {
  name: string
  handle?: FileHandle
}

async function write(handle: FileHandle, text: string) {
  const w = await handle.createWritable()
  await w.write(text)
  await w.close()
}

function download(text: string, name: string) {
  const a = document.createElement('a')
  a.href = URL.createObjectURL(new Blob([text], { type: 'text/yaml' }))
  a.download = name
  a.click()
  URL.revokeObjectURL(a.href)
}

// Whether Save can write back to the same file, rather than download a copy.
export function canSaveInPlace(): boolean {
  return typeof (window as unknown as { showSaveFilePicker?: unknown }).showSaveFilePicker === 'function'
}

// saveFileAs asks where to save and saves there. Returns null when the user
// cancels the dialog.
export async function saveFileAs(text: string, suggestedName: string): Promise<SavedFile | null> {
  const picker = (window as unknown as { showSaveFilePicker?: SaveFilePicker }).showSaveFilePicker
  if (typeof picker !== 'function') {
    download(text, suggestedName)
    return { name: suggestedName }
  }
  try {
    const handle = await picker({
      suggestedName,
      types: [{ description: 'YAML template', accept: { 'text/yaml': ['.yml', '.yaml'] } }],
    })
    await write(handle, text)
    return { name: handle.name, handle }
  } catch (e) {
    if ((e as DOMException).name === 'AbortError') return null
    throw e
  }
}

// saveFile writes back to the file saved last, or asks where to save the
// first time.
export async function saveFile(text: string, current: SavedFile | null, suggestedName: string): Promise<SavedFile | null> {
  if (current?.handle) {
    await write(current.handle, text)
    return current
  }
  return saveFileAs(text, current?.name ?? suggestedName)
}
