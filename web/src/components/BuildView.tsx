import { useEffect, useRef, useState } from 'react'
import { api } from '../api/client'
import type { Artifact, BuildDetails } from '../api/types'

interface BuildViewProps {
  buildId: string
  onRetry: () => Promise<void>
  retrying: boolean
}

// Full MVP-1 build lifecycle. "not-started" is represented by not rendering this
// component at all; once a build exists it moves through the states below.
type Status = 'running' | 'cancelling' | 'cancelled' | 'success' | 'failed'

export function BuildView({ buildId, onRetry, retrying }: BuildViewProps) {
  const [logs, setLogs] = useState<string[]>([])
  const [status, setStatus] = useState<Status>('running')
  const [artifacts, setArtifacts] = useState<Artifact[]>([])
  const [errorMsg, setErrorMsg] = useState<string>('')
  const [details, setDetails] = useState<BuildDetails | null>(null)
  const [detailsOpen, setDetailsOpen] = useState(false)
  const logRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    setLogs([])
    setStatus('running')
    setArtifacts([])
    setErrorMsg('')
    setDetails(null)
    setDetailsOpen(false)

    // Fetch the command + resolved template paths for the troubleshoot panel.
    // Best-effort: a failure here shouldn't disrupt the log stream.
    api.buildDetails(buildId).then(setDetails).catch(() => {})

    const es = new EventSource(api.logsUrl(buildId))

    es.addEventListener('log', (e) => {
      const { message } = JSON.parse((e as MessageEvent).data)
      setLogs((prev) => [...prev, message])
    })
    es.addEventListener('complete', (e) => {
      const data = JSON.parse((e as MessageEvent).data)
      // A build cancelled server-side still terminates via complete/error; honor
      // an explicit cancelled status if the backend reports one.
      setStatus(data.status === 'cancelled' ? 'cancelled' : 'success')
      setArtifacts(data.artifacts ?? [])
      es.close()
    })
    es.addEventListener('error', (e) => {
      const raw = (e as MessageEvent).data
      if (raw) {
        try {
          const data = JSON.parse(raw)
          setStatus(data.status === 'cancelled' ? 'cancelled' : 'failed')
          if (data.message) setErrorMsg(data.message)
        } catch {
          setStatus('failed')
        }
      }
      // A transport error with no data (connection drop) also ends the stream.
      es.close()
    })

    return () => es.close()
  }, [buildId])

  // Auto-scroll to the newest log line.
  useEffect(() => {
    logRef.current?.scrollTo(0, logRef.current.scrollHeight)
  }, [logs])

  const copyLogs = () => navigator.clipboard.writeText(logs.join('\n'))
  const downloadLogs = () => {
    const blob = new Blob([logs.join('\n')], { type: 'text/plain' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `build-${buildId}.log`
    a.click()
    URL.revokeObjectURL(url)
  }
  const copyPath = (path: string) => navigator.clipboard.writeText(path)
  const copyCommand = () => details && navigator.clipboard.writeText(details.command)

  return (
    <div className="mt-6">
      <div className="mb-2 flex items-center gap-3">
        <h2 className="text-sm font-semibold text-[#00285a]">Build Status</h2>
        <StatusBadge status={status} />
        {/* Cancel button wired in Story 3 (build cancellation + cleanup) */}
        {(status === 'failed' || status === 'cancelled') && (
          <button
            className="ml-auto rounded border border-[#0071c5] px-3 py-1 text-xs font-medium text-[#0071c5] hover:bg-blue-50 disabled:cursor-not-allowed disabled:opacity-50"
            disabled={retrying}
            onClick={onRetry}
          >
            {retrying ? 'Starting…' : '↺ Retry build'}
          </button>
        )}
      </div>

      {status === 'failed' && errorMsg && (
        <div className="mb-2 rounded bg-red-50 p-2 text-xs text-red-700">Build failed: {errorMsg}</div>
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
            Build details
            <span className="font-normal text-slate-400">— command, template, paths</span>
          </button>
          {detailsOpen && (
            <div className="space-y-3 border-t border-slate-200 px-3 py-3 text-xs">
              <div>
                <div className="mb-1 flex items-center gap-2">
                  <span className="font-semibold text-slate-600">Command</span>
                  <button
                    className="rounded border border-slate-300 px-1.5 py-0.5 text-[11px] hover:bg-white"
                    onClick={copyCommand}
                  >
                    📋 Copy
                  </button>
                </div>
                <pre className="overflow-x-auto rounded bg-[#00285a] p-2 font-mono text-[11px] leading-relaxed text-slate-100">
                  {details.command}
                </pre>
              </div>
              <div className="flex items-center gap-2">
                <span className="font-semibold text-slate-600">Template</span>
                <span className="font-mono text-slate-700">{details.template}</span>
                <a
                  className="rounded border border-slate-300 px-1.5 py-0.5 text-[11px] hover:bg-white"
                  href={api.templateUrl(buildId)}
                  download={details.template}
                >
                  ⬇ Download
                </a>
              </div>
              <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 font-mono text-slate-600">
                <dt className="font-sans font-semibold">Work dir</dt>
                <dd className="break-all">{details.workDir}</dd>
                <dt className="font-sans font-semibold">Cache dir</dt>
                <dd className="break-all">{details.cacheDir}</dd>
              </dl>
            </div>
          )}
        </div>
      )}

      <div className="mb-2 flex gap-2">
        <button
          className="flex items-center gap-1.5 rounded border border-slate-300 px-2 py-1 text-xs hover:bg-slate-100 disabled:opacity-50"
          disabled={logs.length === 0}
          onClick={copyLogs}
          title="Copy logs to clipboard"
        >
          <svg xmlns="http://www.w3.org/2000/svg" className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
          Copy logs
        </button>
        <button
          className="flex items-center gap-1.5 rounded border border-slate-300 px-2 py-1 text-xs hover:bg-slate-100 disabled:opacity-50"
          disabled={logs.length === 0}
          onClick={downloadLogs}
          title="Download logs as a file"
        >
          <svg xmlns="http://www.w3.org/2000/svg" className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
          Download logs
        </button>
      </div>

      <div
        ref={logRef}
        className="h-[32rem] overflow-auto rounded-md bg-[#00285a] p-3 font-mono text-xs leading-relaxed text-slate-100"
      >
        {logs.length === 0 && <div className="text-slate-400">Waiting for build output…</div>}
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
                  <td className="px-3 py-2 font-mono text-xs text-slate-500 break-all">{a.path}</td>
                  <td className="px-3 py-2 text-right">
                    <div className="flex items-center justify-end gap-1">
                      <button
                        className="flex items-center gap-1 rounded border border-slate-300 px-2 py-1 text-xs hover:bg-slate-100"
                        title="Copy path to clipboard"
                        onClick={() => copyPath(a.path)}
                      >
                        <svg xmlns="http://www.w3.org/2000/svg" className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
                        Copy path
                      </button>
                      <a
                        className="flex items-center gap-1 rounded border border-slate-300 px-2 py-1 text-xs hover:bg-slate-100"
                        title="Download artifact"
                        href={`/api/v1/builds/${buildId}/artifacts/${encodeURIComponent(a.name)}`}
                        download={a.name}
                      >
                        <svg xmlns="http://www.w3.org/2000/svg" className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
                        Download
                      </a>
                    </div>
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
    running: 'bg-amber-100 text-amber-800',
    cancelling: 'bg-amber-100 text-amber-800',
    cancelled: 'bg-slate-200 text-slate-700',
    success: 'bg-green-100 text-green-800',
    failed: 'bg-red-100 text-red-800',
  }
  const label: Record<Status, string> = {
    running: 'Building…',
    cancelling: 'Cancelling…',
    cancelled: '⊘ Cancelled',
    success: '✓ Completed',
    failed: '✗ Failed',
  }
  return <span className={`rounded px-2 py-0.5 text-xs font-medium ${cls[status]}`}>{label[status]}</span>
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
