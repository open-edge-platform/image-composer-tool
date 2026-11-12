// What GET /server reports, fetched once and shared: the build backend's phases
// shape the stepper, and the auth mode decides whether to show who is signed in.
// A server that predates the endpoint, or a fetch failure, yields null and every
// consumer falls back to the local defaults.

import { useEffect, useState } from 'react'
import { api } from '../api/client'
import type { ServerInfo } from '../api/types'

let cached: Promise<ServerInfo | null> | null = null

export function fetchServerInfo(): Promise<ServerInfo | null> {
  if (!cached) {
    cached = api.getServerInfo().catch(() => null)
  }
  return cached
}

// resetServerInfo forgets the cached answer; tests use it between cases.
export function resetServerInfo() {
  cached = null
}

export function useServerInfo(): ServerInfo | null {
  const [info, setInfo] = useState<ServerInfo | null>(null)
  useEffect(() => {
    let cancelled = false
    fetchServerInfo().then((i) => {
      if (!cancelled) setInfo(i)
    })
    return () => {
      cancelled = true
    }
  }, [])
  return info
}
