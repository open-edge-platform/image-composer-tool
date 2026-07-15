import { useEffect, useRef, useState } from 'react'
import { api } from '../api/client'
import type { Artifact, BuildDetails } from '../api/types'

type BuildStatus = 'idle' | 'running' | 'success' | 'failed'

interface BuildViewProps {
  buildId: string
  onRetry: () => Promise<void>
  retrying: boolean
  onStatusChange: (s: BuildStatus) => void
  // The active build streams live logs. A history build (isActive=false) shows a
  // downloadable log file + artifacts instead of a live log text area.
  isActive: boolean
}

// Full MVP-1 build lifecycle. "loading" is a transient state while a history
// build's persisted data is fetched; the others are the actual build states.
type Status = 'loading' | 'running' | 'cancelling' | 'cancelled' | 'success' | 'failed'

export function BuildView({ buildId, onRetry, retrying, onStatusChange, isActive }: BuildViewProps) {
  const [logs, setLogs] = useState<string[]>([])
  const [status, setStatus] = useState<Status>(isActive ? 'running' : 'loading')
  const [artifacts, setArtifacts] = useState<Artifact[]>([])
  const [errorMsg, setErrorMsg] = useState<string>('')
  const [details, setDetails] = useState<BuildDetails | null>(null)
  const [detailsOpen, setDetailsOpen] = useState(false)
  const logRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    setLogs([])
    setStatus(isActive ? 'running' : 'loading')
    setArtifacts([])
    setErrorMsg('')
    setDetails(null)
    setDetailsOpen(false)

    // Fetch the command + resolved template paths for the troubleshoot panel.
    // Best-effort: a failure here shouldn't disrupt the log stream.
    api.buildDetails(buildId).then(setDetails).catch(() => {})

    // A history build (not active) doesn't stream logs — pull its final status,
    // artifacts, and persisted log text from the disk-backed endpoints instead.
    if (!isActive) {
      Promise.all([
        api.buildDetails(buildId),
        api.buildArtifacts(buildId),
        api.logFileText(buildId).catch(() => ''), // log file may not exist
      ])
        .then(([d, arts, logText]) => {
          setStatus((d.status as Status) ?? 'success')
          if (d.status === 'failed') setErrorMsg(d.errMsg ?? '')
          setArtifacts(arts)
          if (logText) setLogs(logText.split('\n'))
        })
        .catch(() => setStatus('failed'))
      return
    }

    const es = new EventSource(api.logsUrl(buildId))

    es.addEventListener('log', (e) => {
      const { message } = JSON.parse((e as MessageEvent).data)
      setLogs((prev) => [...prev, message])
    })
    es.addEventListener('complete', (e) => {
      const data = JSON.parse((e as MessageEvent).data)
      const s = data.status === 'cancelled' ? 'cancelled' : 'success'
      setStatus(s)
      setArtifacts(data.artifacts ?? [])
      onStatusChange(s === 'success' ? 'success' : 'idle')
      // Refresh details so the log-file download link appears post-completion.
      api.buildDetails(buildId).then(setDetails).catch(() => {})
      es.close()
    })
    es.addEventListener('error', (e) => {
      const raw = (e as MessageEvent).data
      if (raw) {
        try {
          const data = JSON.parse(raw)
          const s = data.status === 'cancelled' ? 'cancelled' : 'failed'
          setStatus(s)
          if (data.message) setErrorMsg(data.message)
          onStatusChange('failed')
        } catch {
          setStatus('failed')
          onStatusChange('failed')
        }
      }
      es.close()
    })

    return () => es.close()
  }, [buildId, isActive])

  // Auto-scroll to the newest log line.
  useEffect(() => {
    logRef.current?.scrollTo(0, logRef.current.scrollHeight)
  }, [logs])

  const copyLogs = () => navigator.clipboard.writeText(logs.join('\n'))
  const copyPath = (path: string) => navigator.clipboard.writeText(path)
  const copyCommand = () => details && navigator.clipboard.writeText(details.command)

  return (
    <div className="mt-6">
      <div className="mb-2 flex items-center gap-3">
        <h2 className="text-sm font-semibold text-[#00285a]">Compose Status</h2>
        <StatusBadge status={status} />
        {/* Cancel button wired in Story 3 (build cancellation + cleanup) */}
        {(status === 'failed' || status === 'cancelled') && (
          <button
            className="ml-auto rounded border border-[#0071c5] px-3 py-1 text-xs font-medium text-[#0071c5] hover:bg-blue-50 disabled:cursor-not-allowed disabled:opacity-50"
            disabled={retrying}
            onClick={onRetry}
          >
            {retrying ? 'Starting…' : '↺ Retry compose'}
          </button>
        )}
      </div>

      {status === 'failed' && errorMsg && (
        <div className="mb-2 rounded bg-red-50 p-2 text-xs text-red-700">Compose failed: {errorMsg}</div>
      )}

      {/* Collapsible troubleshoot panel: the exact command, the resolved template
          (downloadable), and the per-build work/cache directories. Collapsed by
          default so it doesn't compete with the log for space. */}
      {details && (
        <div className="mb-2 rounded-md border border-slate-200 bg-slate-50">
          <button
            className="flex w-full items-center gap-2 px-3 py-2 text-left text-xs font-semibold text-slate-600 hover:bg-slate-100"
            onClick={() => setDetailsOpen((o) => !o)}
            aria-expanded={detailsOpen}
          >
            <span className="text-slate-400">{detailsOpen ? '▼' : '▶'}</span>
            Compose details
            <span className="font-normal text-slate-400">— command, template, paths</span>
          </button>
          {detailsOpen && (
            <div className="space-y-4 border-t border-slate-200 px-3 py-3 text-xs">

              {/* Selection + image configuration summary */}
              {details.summary && (
                <div className="grid grid-cols-2 gap-3">
                  <div className="rounded bg-white p-3 shadow-sm">
                    <p className="mb-1.5 text-[10px] font-semibold uppercase tracking-wide text-slate-400">Your Selection</p>
                    <table className="w-full">
                      <tbody>
                        {([
                          ['Vertical', details.summary.vertical],
                          details.summary.sku ? ['SKU', details.summary.sku] : null,
                          ['Platform', details.summary.platform],
                          ['OS', details.summary.os],
                          ['Image Type', details.summary.imageType.toUpperCase()],
                        ] as ([string, string] | null)[]).filter((r): r is [string, string] => r !== null).map(([k, v]) => (
                          <tr key={k}>
                            <td className="py-0.5 pr-3 font-semibold text-slate-500 w-24">{k}</td>
                            <td className="py-0.5 text-slate-700">{v}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                  <div className="rounded bg-white p-3 shadow-sm">
                    <p className="mb-1.5 text-[10px] font-semibold uppercase tracking-wide text-slate-400">Image Configuration</p>
                    <table className="w-full">
                      <tbody>
                        {([
                          ['Image', `${details.summary.imageName}${details.summary.imageVersion ? ` (v${details.summary.imageVersion})` : ''}`],
                          details.summary.baseImage ? ['Base Image', details.summary.baseImage] : null,
                          details.summary.description ? ['Description', details.summary.description] : null,
                          ['Architecture', details.summary.architecture],
                          details.summary.kernelVersion ? ['Kernel', details.summary.kernelVersion] : null,
                          ['Packages', `${details.summary.packageCount} packages`],
                          details.summary.diskSize ? ['Disk', `${details.summary.diskSize}${details.summary.partitionTable ? `, ${details.summary.partitionTable.toUpperCase()}` : ''}${details.summary.partitionCount ? `, ${details.summary.partitionCount} partitions` : ''}`] : null,
                          details.summary.hostname ? ['Hostname', details.summary.hostname] : null,
                        ] as ([string, string] | null)[]).filter((r): r is [string, string] => r !== null).map(([k, v]) => (
                          <tr key={k}>
                            <td className="py-0.5 pr-3 font-semibold text-slate-500 w-24">{k}</td>
                            <td className="py-0.5 text-slate-700">{v}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </div>
              )}

              {/* Command */}
              <div>
                <div className="mb-1 flex items-center gap-2">
                  <span className="font-semibold text-slate-600">Command</span>
                  <button
                    className="ml-auto flex items-center gap-1 rounded-md border border-slate-300 bg-white px-2 py-0.5 text-[11px] text-slate-600 hover:bg-slate-100 hover:text-slate-900"
                    title="Copy command to clipboard"
                    onClick={copyCommand}
                  >
                    <CopyIcon className="h-3.5 w-3.5" />
                    Copy
                  </button>
                </div>
                <pre className="overflow-x-auto rounded bg-[#00285a] p-2 font-mono text-[11px] leading-relaxed text-slate-100">
                  {details.command}
                </pre>
              </div>

              {/* Template */}
              <div className="flex items-center gap-2">
                <span className="font-semibold text-slate-600">Template</span>
                <span className="font-mono text-slate-700">{details.template}</span>
                <a
                  className="rounded p-1 text-slate-500 hover:bg-slate-200 hover:text-slate-700"
                  href={api.templateUrl(buildId)}
                  download={details.template}
                  title="Download template"
                >
                  <DownloadIcon className="h-4 w-4" />
                </a>
              </div>
            </div>
          )}
        </div>
      )}

      {/* Log output — copy/download icons at the top-right corner (always
          visible). Streams live for the active build; shows persisted text for
          history. */}
      <div className="group relative">
        <div className="absolute right-5 top-2 z-10 flex gap-1 opacity-70 transition hover:opacity-100 group-hover:opacity-100">
          <button
            className="rounded-md border border-slate-300 bg-white p-1.5 text-slate-600 shadow-sm hover:bg-slate-100 hover:text-slate-900 disabled:opacity-40"
            disabled={logs.length === 0}
            onClick={copyLogs}
            title="Copy logs to clipboard"
          >
            <CopyIcon className="h-4 w-4" />
          </button>
          <a
            className="rounded-md border border-slate-300 bg-white p-1.5 text-slate-600 shadow-sm hover:bg-slate-100 hover:text-slate-900"
            href={api.logFileUrl(buildId)}
            download={`compose-${buildId}.log`}
            title="Download logs as a file"
          >
            <DownloadIcon className="h-4 w-4" />
          </a>
        </div>
        <div
          ref={logRef}
          className="h-[32rem] overflow-auto rounded-md bg-[#00285a] p-3 font-mono text-xs leading-relaxed text-slate-100"
        >
          {status === 'loading' && <div className="text-slate-400">Loading log…</div>}
          {status !== 'loading' && logs.length === 0 && (
            <div className="text-slate-400">
              {isActive ? 'Waiting for compose output…' : 'No log available for this compose.'}
            </div>
          )}
          {logs.map((line, i) => {
            const clean = cleanLine(line)
            if (clean === '') return null
            return (
              <div key={i} className="whitespace-pre">
                {clean}
              </div>
            )
          })}
        </div>
      </div>

      {artifacts.length > 0 && (
        <div className="mt-4">
          <h3 className="mb-2 text-sm font-semibold text-[#00285a]">Artifacts</h3>
          <table className="w-full border-collapse text-sm">
            <thead>
              <tr className="bg-[#e6f2fa] text-left">
                <th className="px-3 py-2">Name</th>
                <th className="px-3 py-2">Type</th>
                <th className="px-3 py-2">Path</th>
                <th className="px-3 py-2 text-right">Actions</th>
              </tr>
            </thead>
            <tbody>
              {artifacts.map((a) => (
                <tr key={a.path} className="border-b border-slate-200">
                  <td className="px-3 py-2 font-mono text-xs">{a.name}</td>
                  <td className="px-3 py-2 uppercase">{a.type}</td>
                  <td className="px-3 py-2">
                    {/* Click path (or hover copy icon) to copy to clipboard */}
                    <button
                      className="group flex max-w-full items-center gap-1.5 text-left font-mono text-xs text-slate-500 hover:text-slate-800"
                      title="Click to copy path"
                      onClick={() => copyPath(a.path)}
                    >
                      <span className="break-all">{a.path}</span>
                      <CopyIcon className="h-3.5 w-3.5 shrink-0 opacity-0 transition group-hover:opacity-100" />
                    </button>
                  </td>
                  <td className="px-3 py-2 text-right">
                    <a
                      className="inline-flex rounded p-1 text-slate-500 hover:bg-slate-100 hover:text-slate-700"
                      title="Download artifact"
                      href={`/api/v1/builds/${buildId}/artifacts/${encodeURIComponent(a.name)}`}
                      download={a.name}
                    >
                      <DownloadIcon className="h-4 w-4" />
                    </a>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

function StatusBadge({ status }: { status: Status }) {
  const cls: Record<Status, string> = {
    loading: 'bg-slate-200 text-slate-600',
    running: 'bg-amber-100 text-amber-800',
    cancelling: 'bg-amber-100 text-amber-800',
    cancelled: 'bg-slate-200 text-slate-700',
    success: 'bg-green-100 text-green-800',
    failed: 'bg-red-100 text-red-800',
  }
  const label: Record<Status, string> = {
    loading: 'Loading…',
    running: 'Composing…',
    cancelling: 'Cancelling…',
    cancelled: '⊘ Cancelled',
    success: '✓ Completed',
    failed: '✗ Failed',
  }
  return <span className={`rounded px-2 py-0.5 text-xs font-medium ${cls[status]}`}>{label[status]}</span>
}

function CopyIcon({ className }: { className?: string }) {
  return (
    <svg xmlns="http://www.w3.org/2000/svg" className={className ?? 'h-4 w-4'} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <rect x="9" y="9" width="13" height="13" rx="2" />
      <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
    </svg>
  )
}

function DownloadIcon({ className }: { className?: string }) {
  return (
    <svg xmlns="http://www.w3.org/2000/svg" className={className ?? 'h-4 w-4'} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
      <polyline points="7 10 12 15 17 10" />
      <line x1="12" y1="15" x2="12" y2="3" />
    </svg>
  )
}

// Clean a raw log line for display:
// 1. Strip all ANSI/VT100 escape sequences (color, cursor movement, line-clear, etc.)
// 2. Handle carriage returns the way a terminal would — keep only what follows
//    the last \r, so progress-bar overwrites show their final state rather than
//    producing a blank line after the content.
function cleanLine(s: string): string {
  // eslint-disable-next-line no-control-regex
  const stripped = s.replace(/\x1b\[[0-9;]*[A-Za-z]/g, '').replace(/\x1b[^[]/g, '').replace(/\x1b/g, '')
  const cr = stripped.lastIndexOf('\r')
  return cr >= 0 ? stripped.slice(cr + 1) : stripped
}
