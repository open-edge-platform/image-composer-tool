// Typed API client for the ICT web UI backend.

import type {
  Manifest,
  ComposeRequest,
  ComposeResponse,
  BuildAccepted,
  BuildDetails,
  CancelAccepted,
  HistoryItem,
  Artifact,
} from './types'

const BASE = '/api/v1'

async function jsonFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await jsonFetchRaw(path, init)
  return res.json() as Promise<T>
}

// jsonFetchRaw is jsonFetch but returns the raw Response so callers can inspect headers.
async function jsonFetchRaw(path: string, init?: RequestInit): Promise<Response> {
  const res = await fetch(BASE + path, {
    ...init,
    headers: { 'Content-Type': 'application/json', ...(init?.headers || {}) },
  })
  if (!res.ok) {
    let msg = `${res.status} ${res.statusText}`
    try {
      const body = await res.json()
      if (body?.error?.message) msg = body.error.message
    } catch {
      /* ignore */
    }
    throw new Error(msg)
  }
  return res
}

export const api = {
  getManifest: () => jsonFetch<Manifest>('/manifest'),

  compose: (req: ComposeRequest) =>
    jsonFetch<ComposeResponse>('/templates/compose', {
      method: 'POST',
      body: JSON.stringify(req),
    }),

  startBuild: (req: ComposeRequest) =>
    jsonFetch<BuildAccepted>('/builds', {
      method: 'POST',
      body: JSON.stringify({ compose: req }),
    }),

  // Compose history, newest-first (merges live builds + on-disk meta records).
  // Also returns the server↔browser clock offset for correcting elapsed-time labels.
  listBuilds: async (): Promise<{ builds: HistoryItem[]; clockOffsetMs: number }> => {
    const res = await jsonFetchRaw('/builds')
    const dateHeader = res.headers.get('Date')
    const serverNow = dateHeader ? new Date(dateHeader).getTime() : NaN
    const clockOffsetMs = Number.isFinite(serverNow) ? serverNow - Date.now() : 0
    const body = (await res.json()) as { builds: HistoryItem[] }
    return { builds: body.builds, clockOffsetMs }
  },

  // Cancel an in-flight build. Returns 202 with the cancelling status; the
  // terminal state (cancelled/failed) then arrives over SSE. 409 if the build is
  // not running (already finished, or a cancel is already in flight). A 202 whose
  // body carries `residual` means the cancel was accepted but the signal could
  // not be delivered — the caller must surface that, since no terminal SSE event
  // may follow.
  cancelBuild: (buildId: string) =>
    jsonFetch<CancelAccepted>(`/builds/${buildId}/cancel`, {
      method: 'POST',
    }),

  // Build command + resolved paths for the troubleshoot panel.
  buildDetails: (buildId: string) =>
    jsonFetch<BuildDetails>(`/builds/${buildId}/details`),

  // Output artifacts for a build (used for history builds not streaming logs).
  buildArtifacts: (buildId: string) =>
    jsonFetch<{ artifacts: Artifact[] }>(`/builds/${buildId}/artifacts`).then(
      (r) => r.artifacts,
    ),

  // SSE log stream URL for a build.
  logsUrl: (buildId: string) => `${BASE}/builds/${buildId}/logs`,

  // Download URL for the exact template that was built.
  templateUrl: (buildId: string) => `${BASE}/builds/${buildId}/template`,

  // Download URL for the persisted compose log (available after completion).
  logFileUrl: (buildId: string) => `${BASE}/builds/${buildId}/logfile`,

  // Fetch the persisted compose log as text (for displaying past builds' logs).
  logFileText: (buildId: string) =>
    fetch(`${BASE}/builds/${buildId}/logfile`).then((r) =>
      r.ok ? r.text() : Promise.reject(new Error(`${r.status}`)),
    ),
}
