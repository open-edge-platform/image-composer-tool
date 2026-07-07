import { useEffect, useRef, useState } from 'react'
import { api } from '../api/client'
import type { Artifact } from '../api/types'

interface BuildViewProps {
  buildId: string
}

type Status = 'running' | 'success' | 'failed'

export function BuildView({ buildId }: BuildViewProps) {
  const [logs, setLogs] = useState<string[]>([])
  const [status, setStatus] = useState<Status>('running')
  const [artifacts, setArtifacts] = useState<Artifact[]>([])
  const logRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    setLogs([])
    setStatus('running')
    setArtifacts([])

    const es = new EventSource(api.logsUrl(buildId))

    es.addEventListener('log', (e) => {
      const { message } = JSON.parse((e as MessageEvent).data)
      setLogs((prev) => [...prev, message])
    })
    es.addEventListener('complete', (e) => {
      const data = JSON.parse((e as MessageEvent).data)
      setStatus('success')
      setArtifacts(data.artifacts ?? [])
      es.close()
    })
    es.addEventListener('error', (e) => {
      // Distinguish a build-failure event (has data) from a transport error.
      const msg = (e as MessageEvent).data
      if (msg) {
        setStatus('failed')
      }
      es.close()
    })

    return () => es.close()
  }, [buildId])

  // Auto-scroll to the newest log line.
  useEffect(() => {
    logRef.current?.scrollTo(0, logRef.current.scrollHeight)
  }, [logs])

  const copyPath = (path: string) => navigator.clipboard.writeText(path)

  return (
    <div className="mt-6">
      <div className="mb-2 flex items-center gap-3">
        <h2 className="text-sm font-semibold text-[#00285a]">Build Status</h2>
        <StatusBadge status={status} />
      </div>

      <div
        ref={logRef}
        className="h-80 overflow-auto rounded-md bg-[#00285a] p-3 font-mono text-xs leading-relaxed text-slate-100"
      >
        {logs.length === 0 && <div className="text-slate-400">Waiting for build output…</div>}
        {logs.map((line, i) => (
          <div key={i} className="whitespace-pre-wrap">
            {stripAnsi(line)}
          </div>
        ))}
      </div>

      {artifacts.length > 0 && (
        <div className="mt-4">
          <h3 className="mb-2 text-sm font-semibold text-[#00285a]">Artifacts</h3>
          <table className="w-full border-collapse text-sm">
            <thead>
              <tr className="bg-[#e6f2fa] text-left">
                <th className="px-3 py-2">Name</th>
                <th className="px-3 py-2">Type</th>
                <th className="px-3 py-2 text-right">Path</th>
              </tr>
            </thead>
            <tbody>
              {artifacts.map((a) => (
                <tr key={a.path} className="border-b border-slate-200">
                  <td className="px-3 py-2">{a.name}</td>
                  <td className="px-3 py-2 uppercase">{a.type}</td>
                  <td className="px-3 py-2 text-right">
                    <button
                      className="rounded border border-slate-300 px-2 py-1 text-xs hover:bg-slate-100"
                      title="Copy path"
                      onClick={() => copyPath(a.path)}
                    >
                      📋 Copy path
                    </button>
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
  const map: Record<Status, string> = {
    running: 'bg-amber-100 text-amber-800',
    success: 'bg-green-100 text-green-800',
    failed: 'bg-red-100 text-red-800',
  }
  const label = { running: 'Building…', success: '✓ Passed', failed: '✗ Failed' }[status]
  return <span className={`rounded px-2 py-0.5 text-xs font-medium ${map[status]}`}>{label}</span>
}

// The Go logger emits ANSI color codes; strip them for clean display.
function stripAnsi(s: string): string {
  // eslint-disable-next-line no-control-regex
  return s.replace(/\[[0-9;]*m/g, '')
}
