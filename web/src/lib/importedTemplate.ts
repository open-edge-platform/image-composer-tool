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
  hostname: string
  os: string
  dist: string
  architecture: string
  imageType: string
  extends: string
  packageCount: number
  diskSize: string
  partitionTable: string
  partitionCount: number
}

export interface ImportedTemplate {
  // Original file name, kept so the user can tell which file they picked.
  fileName: string
  // What this template is called throughout the UI. Mirrors the name the server
  // records for the build (service.postedTemplateName), so the Basic tab and the
  // build history don't label the same build differently.
  displayName: string
  // Verbatim document text. Kept unparsed so the server sees exactly the bytes
  // the user supplied — re-emitting our parse would silently drop comments,
  // anchors, and any key this preview doesn't model.
  yaml: string
  // systemConfig.packages as written, each entry unsplit. A pinned entry is
  // `name_version`, but rpm names may themselves contain `_`, so splitting one
  // back apart is guesswork — the entry is carried whole and shown as-is.
  packages: string[]
  summary: ImportedTemplateSummary
}

// imageNameRe mirrors the schema pattern for image.name, as transcribed in
// service/delta.go. Only used to decide whether image.name is presentable as a
// name; the server re-derives and re-checks it regardless.
const imageNameRe = /^[a-zA-Z0-9]([a-zA-Z0-9\-_]*[a-zA-Z0-9])?$/
const MAX_IMAGE_NAME_LEN = 64

function displayNameFor(imageName: string, fileName: string): string {
  if (imageName.length > 0 && imageName.length <= MAX_IMAGE_NAME_LEN && imageNameRe.test(imageName)) {
    return imageName + '.yml'
  }
  return fileName
}

export class TemplateParseError extends Error {}

// Shape of the parts of os-image-template.schema.json this preview reads.
interface RawTemplate {
  image?: { name?: unknown; version?: unknown }
  target?: { os?: unknown; dist?: unknown; arch?: unknown; imageType?: unknown }
  extends?: unknown
  systemConfig?: { description?: unknown; hostname?: unknown; packages?: unknown }
  disk?: { size?: unknown; partitionTableType?: unknown; partitions?: unknown }
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
  // A document declaring `extends` depends on a relative parent file this
  // import flow has no access to — even one that also carries its own image/
  // target/systemConfig keys would build against the wrong (missing) parent
  // if imported standalone.
  if (t.extends != null) {
    throw new TemplateParseError(
      'This template extends another file and cannot be imported on its own. Import a complete, self-contained template instead.',
    )
  }
  if (!t.image && !t.target && !t.systemConfig) {
    throw new TemplateParseError(
      'This file does not look like an image template — expected an `image`, `target`, or `systemConfig` key.',
    )
  }

  const packages = Array.isArray(t.systemConfig?.packages) ? t.systemConfig.packages : []
  const partitions = Array.isArray(t.disk?.partitions) ? t.disk.partitions : []
  const imageName = str(t.image?.name)

  return {
    fileName,
    displayName: displayNameFor(imageName, fileName),
    yaml: text,
    packages: packages.map(str).filter((p) => p !== ''),
    summary: {
      imageName,
      imageVersion: str(t.image?.version),
      description: str(t.systemConfig?.description),
      hostname: str(t.systemConfig?.hostname),
      os: str(t.target?.os),
      dist: str(t.target?.dist),
      architecture: str(t.target?.arch),
      imageType: str(t.target?.imageType),
      extends: str(t.extends),
      packageCount: packages.length,
      diskSize: str(t.disk?.size),
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
