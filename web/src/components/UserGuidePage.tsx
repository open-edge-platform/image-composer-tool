// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useRef } from 'react'
import { guideHref, guideTopics } from '../guide/topics'

interface UserGuidePageProps {
  topicId: string
}

// UserGuidePage shows one guide topic, with every topic listed alongside and
// Previous/Next links, so the guide can be read straight through or dipped into.
export function UserGuidePage({ topicId }: UserGuidePageProps) {
  const index = Math.max(0, guideTopics.findIndex((t) => t.id === topicId))
  const topic = guideTopics[index]
  const prev = guideTopics[index - 1]
  const next = guideTopics[index + 1]

  const headingRef = useRef<HTMLHeadingElement>(null)

  // A new topic starts at its top, not wherever the previous one was scrolled
  // to, and keyboard focus moves to its heading: otherwise it stays on the link
  // that was activated (or is lost when that link disappears, as Next does on
  // the last topic), leaving a keyboard or screen-reader user behind the new
  // content with no cue that the page changed.
  useEffect(() => {
    window.scrollTo?.(0, 0)
    headingRef.current?.focus({ preventScroll: true })
  }, [topic.id])

  return (
    <div className="mx-auto flex max-w-screen-xl flex-col gap-8 px-10 py-8 md:flex-row">
      <aside className="md:sticky md:top-6 md:w-64 md:shrink-0 md:self-start">
        <p className="mb-1 text-xl font-bold text-[#00285a]">User Guide</p>
        <p className="mb-4 text-xs text-slate-500">How to use Image Composer Tool</p>
        <nav aria-label="User Guide topics" className="flex flex-col gap-1">
          {guideTopics.map((t, i) => (
            <a
              key={t.id}
              href={guideHref(t.id)}
              aria-current={t.id === topic.id ? 'page' : undefined}
              className={
                'flex items-center gap-3 rounded px-3 py-2 text-left text-sm ' +
                (t.id === topic.id
                  ? 'bg-[#0071c5] font-semibold text-white'
                  : 'text-[#00285a] hover:bg-slate-100')
              }
            >
              <span
                className={
                  'flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-xs ' +
                  (t.id === topic.id ? 'bg-white text-[#0071c5]' : 'bg-slate-200 text-slate-600')
                }
              >
                {i + 1}
              </span>
              {t.title}
            </a>
          ))}
        </nav>
      </aside>

      <article className="max-w-3xl flex-1">
        <p className="mb-1 text-xs font-semibold uppercase tracking-wide text-slate-400">
          Page {index + 1} of {guideTopics.length}
        </p>
        <h1 ref={headingRef} tabIndex={-1} className="mb-1 text-2xl font-bold text-[#00285a] outline-none">
          {topic.title}
        </h1>
        <p className="mb-6 text-sm text-slate-500">{topic.summary}</p>
        {topic.body}

        <div className="mt-10 flex justify-between gap-4 border-t border-slate-200 pt-4">
          {prev ? (
            <a
              href={guideHref(prev.id)}
              className="rounded-md border border-slate-300 bg-white px-4 py-2 text-sm font-semibold text-[#00285a] hover:bg-slate-50"
            >
              ← {prev.title}
            </a>
          ) : (
            <span />
          )}
          {next && (
            <a
              href={guideHref(next.id)}
              className="rounded-md bg-[#0071c5] px-4 py-2 text-sm font-semibold text-white hover:bg-[#00285a]"
            >
              Next: {next.title} →
            </a>
          )}
        </div>
      </article>
    </div>
  )
}
