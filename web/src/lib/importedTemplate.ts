// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Client-side parsing for a user-supplied image template (the "Import Template"
// flow on the Basic tab).
//
// This only reads enough of the document to show the user what they picked
// before anything is sent anywhere — authoritative validation stays server-side
// (POST /templates/validate), because the browser has no view of the manifest's
// targets, the package repositories, or the schema's field-level rules.

import yaml from 'js-yaml'

// Guards against a mis-picked file (an ISO, a log, a disk image) being read
// into memory in full. Sized off the largest template this repo ships
// (image-templates/ubuntu24/generic-companion-os-server-template.yml, ~44 KB)
// with room to spare, so a legitimate hand-written template never trips it.
export const MAX_TEMPLATE_BYTES = 256 * 1024

export const TEMPLATE_FILE_ACCEPT = '.yml,.yaml'

// The subset of the template the preview renders. Everything is optional: an
// imported file may be a partial/delta template that only overrides a few keys.
export interface ImportedTemplateSummary {
  imageName: string
  imageVersion: string
  description: string
  os: string
  dist: string
  architecture: string
  imageType: string
  extends: string
  packageCount: number
  partitionTable: string
  partitionCount: number
}

export interface ImportedTemplate {
  // Original file name, shown in the UI and used as the default when the
  // template is later saved or re-exported.
  fileName: string
  // Verbatim document text. Kept unparsed so the server sees exactly the bytes
  // the user supplied — re-emitting our parse would silently drop comments,
  // anchors, and any key this preview doesn't model.
  yaml: string
  summary: ImportedTemplateSummary
}

export class TemplateParseError extends Error {}

// Shape of the parts of os-image-template.schema.json this preview reads.
interface RawTemplate {
  image?: { name?: unknown; version?: unknown }
  target?: { os?: unknown; dist?: unknown; arch?: unknown; imageType?: unknown }
  extends?: unknown
  systemConfig?: { description?: unknown; packages?: unknown }
  disk?: { partitionTableType?: unknown; partitions?: unknown }
}

function str(v: unknown): string {
  return typeof v === 'string' ? v : typeof v === 'number' ? String(v) : ''
}

// parseImportedTemplate turns raw file text into the preview model, throwing a
// TemplateParseError with a user-facing message when the text is not a usable
// template document.
export function parseImportedTemplate(fileName: string, text: string): ImportedTemplate {
  if (text.trim() === '') {
    throw new TemplateParseError('The file is empty.')
  }

  let doc: unknown
  try {
    doc = yaml.load(text)
  } catch (e) {
    throw new TemplateParseError(`Not valid YAML: ${(e as Error).message}`)
  }

  // A scalar or list parses fine but is not a template; say so here rather than
  // rendering an all-empty preview that looks like a successful import.
  if (doc === null || typeof doc !== 'object' || Array.isArray(doc)) {
    throw new TemplateParseError('This file does not look like an image template.')
  }

  const t = doc as RawTemplate
  // `extends` alone is enough: a delta template carries nothing else.
  if (!t.image && !t.target && !t.systemConfig && t.extends == null) {
    throw new TemplateParseError(
      'This file does not look like an image template — expected an `image`, `target`, `systemConfig`, or `extends` key.',
    )
  }

  const packages = Array.isArray(t.systemConfig?.packages) ? t.systemConfig.packages : []
  const partitions = Array.isArray(t.disk?.partitions) ? t.disk.partitions : []

  return {
    fileName,
    yaml: text,
    summary: {
      imageName: str(t.image?.name),
      imageVersion: str(t.image?.version),
      description: str(t.systemConfig?.description),
      os: str(t.target?.os),
      dist: str(t.target?.dist),
      architecture: str(t.target?.arch),
      imageType: str(t.target?.imageType),
      extends: str(t.extends),
      packageCount: packages.length,
      partitionTable: str(t.disk?.partitionTableType),
      partitionCount: partitions.length,
    },
  }
}

// readTemplateFile reads a picked/dropped file, rejecting anything too large or
// obviously not a template before its contents are loaded.
export async function readTemplateFile(file: File): Promise<ImportedTemplate> {
  if (!/\.ya?ml$/i.test(file.name)) {
    throw new TemplateParseError('Choose a .yml or .yaml template file.')
  }
  if (file.size > MAX_TEMPLATE_BYTES) {
    throw new TemplateParseError(
      `That file is ${Math.round(file.size / 1024)} KB — templates are expected to be under ${MAX_TEMPLATE_BYTES / 1024} KB.`,
    )
  }
  return parseImportedTemplate(file.name, await file.text())
}
