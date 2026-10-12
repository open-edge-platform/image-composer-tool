// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import type { ImportedTemplate } from '../lib/importedTemplate'

interface ImportedTemplateCardProps {
  template: ImportedTemplate
  heading?: string
}

// ImportedTemplateCard shows what an imported template declares, in the same
// shape as the selection-derived Image Configuration panel. Rows the document
// doesn't carry are dropped rather than rendered empty — an imported template
// may be a partial one that only overrides a few keys.
export function ImportedTemplateCard({
  template,
  heading = 'Image Configuration',
}: ImportedTemplateCardProps) {
  const s = template.summary
  const disk = [
    s.diskSize,
    s.partitionTable ? s.partitionTable.toUpperCase() : '',
    s.partitionCount ? `${s.partitionCount} partitions` : '',
  ]
    .filter(Boolean)
    .join(', ')

  const rows: ([string, string] | null)[] = [
    ['Template', template.displayName],
    // Only worth showing when it isn't already the row above.
    template.fileName !== template.displayName ? ['Source file', template.fileName] : null,
    s.imageName
      ? ['Image', `${s.imageName}${s.imageVersion ? ` (v${s.imageVersion})` : ''}`]
      : null,
    s.extends ? ['Extends', s.extends] : null,
    s.description ? ['Description', s.description] : null,
    s.os || s.dist ? ['Target', [s.dist, s.os].filter(Boolean).join(' / ')] : null,
    s.architecture ? ['Architecture', s.architecture] : null,
    s.imageType ? ['Image Type', s.imageType] : null,
    s.packageCount ? ['Packages', `${s.packageCount} packages`] : null,
    disk ? ['Disk', disk] : null,
    s.hostname ? ['Hostname', s.hostname] : null,
  ]

  return (
    <div className="max-w-md rounded-lg border border-slate-200 bg-slate-50 p-4 text-xs">
      <p className="mb-1.5 text-[10px] font-semibold uppercase tracking-wide text-slate-400">
        {heading}
      </p>
      <table className="w-full">
        <tbody>
          {rows
            .filter((r): r is [string, string] => r !== null)
            .map(([k, v]) => (
              <tr key={k}>
                <td className="w-24 py-0.5 pr-3 align-top font-semibold text-slate-500">{k}</td>
                <td className="py-0.5 break-words text-slate-700">{v}</td>
              </tr>
            ))}
        </tbody>
      </table>
    </div>
  )
}
