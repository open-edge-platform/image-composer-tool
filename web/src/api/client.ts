// Typed API client for the ICT web UI backend.

import type {
  Manifest,
  ComposeRequest,
  ComposeResponse,
  BuildAccepted,
  BuildDetails,
  HistoryItem,
  Artifact,
} from './types'

const BASE = '/api/v1'

async function jsonFetch<T>(path: string, init?: RequestInit): Promise<T> {
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
  return res.json() as Promise<T>
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
  listBuilds: () =>
    jsonFetch<{ builds: HistoryItem[] }>('/builds').then((r) => r.builds),

  // Cancel an in-flight build. The endpoint arrives with Story 3; until then the
  // backend returns 404 and the caller surfaces that as a cancel failure.
  cancelBuild: (buildId: string) =>
    jsonFetch<void>(`/builds/${buildId}/cancel`, { method: 'POST' }),

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
