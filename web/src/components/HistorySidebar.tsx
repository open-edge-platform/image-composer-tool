import { useEffect, useState } from 'react'
import { labelFor, useStore } from '../store'
import type { HistoryItem, Manifest } from '../api/types'

interface HistorySidebarProps {
  items: HistoryItem[]
  selectedId: string | null
  onSelect: (id: string) => void
  clockOffsetMs?: number
}

// Left-hand compose history list inside the Compose Image tab. Newest first;
// each row shows a status dot and SKU, the rest of the selection as subtext, and
// a relative time.
export function HistorySidebar({
  items,
  selectedId,
  onSelect,
  clockOffsetMs = 0,
}: HistorySidebarProps) {
  const manifest = useStore((s) => s.manifest)
  const [nowMs, setNowMs] = useState(() => Date.now())
  useEffect(() => {
    if (items.length === 0) return
    const t = setInterval(() => setNowMs(Date.now()), 1000)
    return () => clearInterval(t)
  }, [items.length])
  return (
    <div className="w-72 shrink-0 border-r border-slate-200 pr-3">
      <p className="mb-2 text-[10px] font-semibold uppercase tracking-wide text-slate-400">
        History
      </p>
      {items.length === 0 ? (
        <p className="text-xs text-slate-400">No composes yet.</p>
      ) : (
        <ul className="space-y-1">
          {items.map((it) => {
            const { title, details, tooltip } = rowLabels(it, manifest)
            return (
              <li key={it.id}>
                <button
                  onClick={() => onSelect(it.id)}
                  title={tooltip}
                  className={
                    'w-full rounded-md px-2 py-1.5 text-left text-xs transition ' +
                    (it.id === selectedId
                      ? 'bg-[#e6f2fa] text-[#00285a]'
                      : 'hover:bg-slate-100 text-slate-700')
                  }
                >
                  <div className="flex items-center gap-1.5">
                    <StatusDot status={it.status} />
                    <span className="truncate font-medium">{title}</span>
                  </div>
                  {details && (
                    <div className="mt-0.5 truncate pl-3 text-[11px] text-slate-400">
                      {details}
                    </div>
                  )}
                  <div className="mt-0.5 pl-3 text-[11px] text-slate-400">
                    {relativeTime(it.createdAt, nowMs + clockOffsetMs)}
                  </div>
                </button>
              </li>
            )
          })}
        </ul>
      )}
    </div>
  )
}

// rowLabels splits a history row's selection into a title and a subtext line.
//
// The SKU leads, because it is the one dimension the rest of the selection can be
// identical across: the two Fed Aero blueprints share use case, platform, OS and
// image type, so a label built from those alone made them indistinguishable.
//
// Values are the manifest's display names rather than the raw ids the server
// records, so a row reads in the same vocabulary as the dropdowns that produced
// it. labelFor falls back to the id when the manifest has not loaded yet or no
// longer lists the value, so a row degrades to slugs rather than blanks.
//
// Both rendered lines truncate, hence `tooltip` carrying the full text.
function rowLabels(
  it: HistoryItem,
  manifest: Manifest | null,
): { title: string; details: string; tooltip: string } {
  const s = it.summary
  // No summary at all: an older record, or one whose template failed to merge.
  if (!s) return { title: it.template, details: '', tooltip: it.template }

  const vertical = labelFor(manifest?.verticals ?? [], s.vertical)
  const imageType = s.imageType?.toUpperCase() ?? ''

  // The platform is the one dimension the subtext abbreviates to its id: the
  // manifest's display name glosses the codename ("PTL (Panther Lake)"), which
  // is what a dropdown wants but costs a third of the row's width here — enough
  // to truncate the image type away on the Fed Aero rows. The acronym already
  // identifies the platform, and the tooltip below still carries the full name.
  const platformShort = s.platform.toUpperCase()
  const platform = labelFor(manifest?.platforms ?? [], s.platform)
  const os = labelFor(manifest?.targets ?? [], s.os)

  // SKU is optional — a vertical that offers only one combination records none.
  // Promote the use case to the title there rather than leaving it empty.
  const sku = s.sku ? labelFor(manifest?.skus ?? [], s.sku) : ''
  const title = sku || vertical || it.template
  const rest = sku
    ? [vertical, platformShort, os, imageType]
    : [platformShort, os, imageType]

  const tooltip = [
    sku && `SKU: ${sku}`,
    vertical && `Use Case: ${vertical}`,
    platform && `Platform: ${platform}`,
    os && `OS: ${os}`,
    imageType && `Image Type: ${imageType}`,
  ]
    .filter(Boolean)
    .join('\n')

  return { title, details: rest.filter(Boolean).join(' · '), tooltip }
}

// One dot per server-side build state. Cancelling and cancelled need their own
// colours: without them a cancelled build looked identical to an unknown status
// and a cancel in progress looked like a finished one.
const dotStyles: Record<string, string> = {
  'not-started': 'bg-yellow-400 animate-pulse',
  running: 'bg-yellow-400 animate-pulse',
  cancelling: 'bg-amber-500 animate-pulse',
  cancelled: 'bg-slate-400',
  success: 'bg-green-400',
  failed: 'bg-red-500',
}

function StatusDot({ status }: { status: string }) {
  const cls = dotStyles[status] ?? 'bg-slate-300'
  return <span title={status} className={`h-2 w-2 shrink-0 rounded-full ${cls}`} />
}

// relativeTime renders a compact "just now / 5m ago / 2h ago / 3d ago" label.
function relativeTime(iso: string, nowMs: number): string {
  const then = new Date(iso).getTime()
  if (Number.isNaN(then)) return ''
  const secs = Math.max(0, Math.floor((nowMs - then) / 1000))
  if (secs < 60) return 'just now'
  const mins = Math.floor(secs / 60)
  if (mins < 60) return `${mins}m ago`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.floor(hours / 24)
  return `${days}d ago`
}
