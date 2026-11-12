// Shows who the server believes is calling, when the server identifies callers
// at all. With `auth.mode` "none" there is one implicit local user and nothing
// worth showing; behind an authenticating proxy the name comes from GET /me, so
// it reflects the identity build history is actually filtered by.

import { useEffect, useState } from 'react'
import { api } from '../api/client'
import type { Me } from '../api/types'
import { useServerInfo } from '../lib/serverInfo'

export function UserBadge() {
  const info = useServerInfo()
  const identified = !!info && info.auth.mode !== 'none'
  const [me, setMe] = useState<Me | null>(null)

  useEffect(() => {
    if (!identified) return
    let cancelled = false
    api
      .me()
      .then((m) => {
        if (!cancelled) setMe(m)
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [identified])

  if (!identified || !me) return null
  return (
    <span className="text-sm text-slate-200" title={me.email || me.owner}>
      Hello <span className="font-semibold text-white">{me.displayName}</span>
    </span>
  )
}
