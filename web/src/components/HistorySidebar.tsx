import { useEffect, useState } from 'react'
import type { HistoryItem } from '../api/types'

interface HistorySidebarProps {
  items: HistoryItem[]
  selectedId: string | null
  onSelect: (id: string) => void
  clockOffsetMs?: number
}

// Left-hand compose history list inside the Compose Image tab. Newest first;
// each row shows a status dot, template name, and vertical · relative time.
export function HistorySidebar({
  items,
  selectedId,
  onSelect,
  clockOffsetMs = 0,
}: HistorySidebarProps) {
  const [nowMs, setNowMs] = useState(() => Date.now())
  useEffect(() => {
    if (items.length === 0) return
    const t = setInterval(() => setNowMs(Date.now()), 1000)
    return () => clearInterval(t)
  }, [items.length])
  return (
    <div className="w-64 shrink-0 border-r border-slate-200 pr-3">
      <p className="mb-2 text-[10px] font-semibold uppercase tracking-wide text-slate-400">
        History
      </p>
      {items.length === 0 ? (
        <p className="text-xs text-slate-400">No composes yet.</p>
      ) : (
        <ul className="space-y-1">
          {items.map((it) => (
            <li key={it.id}>
              <button
                onClick={() => onSelect(it.id)}
                className={
                  'w-full rounded-md px-2 py-1.5 text-left text-xs transition ' +
                  (it.id === selectedId
                    ? 'bg-[#e6f2fa] text-[#00285a]'
                    : 'hover:bg-slate-100 text-slate-700')
                }
              >
                <div className="flex items-center gap-1.5">
                  <StatusDot status={it.status} />
                  <span className="truncate font-medium">{combinationLabel(it)}</span>
                </div>
                <div className="mt-0.5 pl-3 text-[11px] text-slate-400">
                  {relativeTime(it.createdAt, nowMs + clockOffsetMs)}
                </div>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

// combinationLabel summarizes the composed selection (vertical / platform / OS /
// image type) for a history row, falling back to the template filename when no
// summary is available (e.g. very old builds).
function combinationLabel(it: HistoryItem): string {
  const s = it.summary
  if (!s) return it.template
  const parts = [s.vertical, s.platform, s.os, s.imageType?.toUpperCase()].filter(Boolean)
  return parts.join(' / ')
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
