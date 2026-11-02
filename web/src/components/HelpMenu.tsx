// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useId, useRef, useState } from 'react'
import { api } from '../api/client'
import type { VersionInfo } from '../api/types'
import { guideHref, guideTopics } from '../guide/topics'

// The help pages, in menu order. Each is an ordinary link, so adding a page to
// the menu is adding an entry here.
const links = [{ label: 'User Guide', href: guideHref(guideTopics[0].id) }]

interface HelpMenuProps {
  // True while one of the help pages is showing, so the button reads as the
  // current location the way the active tab does.
  active: boolean
}

// The value an unstamped development build reports for commit and buildDate.
const unstamped = 'unknown'

// buildLine joins the commit and build date for display, leaving out whichever
// the build did not stamp; null when it stamped neither.
export function buildLine(v: VersionInfo): string | null {
  const parts = [v.commit, v.buildDate].filter((p) => p && p !== unstamped)
  return parts.length > 0 ? `Build ${parts.join(' · ')}` : null
}

// HelpMenu is the "?" button at the right of the nav bar. It is a disclosure (a
// button that shows and hides a list of links), not an ARIA menu: these are
// links to pages, and a menu role would promise arrow-key handling they lack.
export function HelpMenu({ active }: HelpMenuProps) {
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)
  const buttonRef = useRef<HTMLButtonElement>(null)
  const listId = useId()
  // Fetched once, up front, so it is already there when the menu opens. On
  // failure the version line is simply left out: a help menu is no place for an
  // error, and the rest of the app reports an unreachable server already.
  const [version, setVersion] = useState<VersionInfo | null>(null)

  useEffect(() => {
    let cancelled = false
    api
      .getVersion()
      .then((v) => {
        if (!cancelled) setVersion(v)
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [])

  // Close on Escape or a press anywhere outside. Escape returns focus to the
  // button so a keyboard user is not left on a link that just disappeared.
  useEffect(() => {
    if (!open) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      setOpen(false)
      buttonRef.current?.focus()
    }
    const onPointerDown = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false)
    }
    window.addEventListener('keydown', onKeyDown)
    window.addEventListener('pointerdown', onPointerDown)
    return () => {
      window.removeEventListener('keydown', onKeyDown)
      window.removeEventListener('pointerdown', onPointerDown)
    }
  }, [open])

  return (
    <div ref={rootRef} className="relative">
      <button
        ref={buttonRef}
        type="button"
        aria-label="Help"
        title="Help"
        aria-expanded={open}
        aria-controls={listId}
        onClick={() => setOpen((o) => !o)}
        className={
          'flex h-8 w-8 items-center justify-center rounded-full border text-sm font-bold ' +
          (active
            ? 'border-[#0071c5] bg-[#0071c5] text-white'
            : 'border-white/60 text-white hover:bg-white/10')
        }
      >
        ?
      </button>
      {open && (
        <nav
          id={listId}
          aria-label="Help"
          className="absolute right-0 z-40 mt-2 w-56 rounded-lg border border-slate-200 bg-white py-1 shadow-lg"
        >
          <ul>
            {links.map((link) => (
              <li key={link.href}>
                <a
                  href={link.href}
                  onClick={() => setOpen(false)}
                  className="block px-4 py-2 text-sm font-semibold text-[#00285a] hover:bg-slate-100"
                >
                  {link.label}
                </a>
              </li>
            ))}
          </ul>
          {version && (
            // Lets a user of a hosted instance tell, and report, exactly which
            // release they are on.
            <div className="mt-1 border-t border-slate-100 px-4 pb-1 pt-2 text-xs text-slate-500">
              <p>Version {version.version}</p>
              {buildLine(version) && <p>{buildLine(version)}</p>}
            </div>
          )}
        </nav>
      )}
    </div>
  )
}
