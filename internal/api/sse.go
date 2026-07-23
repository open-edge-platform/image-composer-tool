// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/api/service"
)

// handleBuildLogs streams a build's logs as Server-Sent Events. It replays any
// buffered lines, follows new ones until the build finishes, then emits a
// terminal `complete` or `error` event.
//
// This is intentionally outside the generated JSON ServerInterface: oapi-codegen
// does not model text/event-stream responses cleanly, so the log stream stays a
// hand-written handler (registered on the mux in routes()).
func (s *Server) handleBuildLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	b, ok := s.svc.Build(id)
	if !ok {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "build not found")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "NO_STREAM", "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	sent := 0
	lastPhase := ""
	lastInstall := ""
	emit := func() {
		lines := b.SnapshotLogs()
		if len(lines) == sent {
			// No new lines since the last tick. Phase and install progress are
			// pure functions of the buffered lines, so they cannot have changed
			// either — skip the full-slice rescan (and the flush) entirely. This
			// keeps an idle 300ms tick O(1) instead of O(total log size), which
			// matters for long composes and multiple concurrent SSE clients.
			return
		}
		for ; sent < len(lines); sent++ {
			sendEvent(w, "log", map[string]string{"message": lines[sent]})
		}
		// Derive and emit the current build phase (+ install progress) when it
		// changes, so the UI stepper can advance. Best-effort, log-derived.
		phase := service.DetectPhase(lines)
		done, total := service.InstallProgress(lines)
		install := fmt.Sprintf("%d/%d", done, total)
		if phase != lastPhase || install != lastInstall {
			lastPhase, lastInstall = phase, install
			sendEvent(w, "phase", map[string]any{
				"phase":        phase,
				"installDone":  done,
				"installTotal": total,
			})
		}
		flusher.Flush()
	}

	emit()          // replay buffered history
	flusher.Flush() // establish the stream even if there were no buffered lines yet

	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			emit()
		case <-b.Done():
			emit() // drain remaining lines
			res := b.Result()
			if res.Status == service.StatusSuccess {
				arts := res.Artifacts
				if arts == nil {
					arts = []service.Artifact{}
				}
				// Ensure the stepper shows completion.
				sendEvent(w, "phase", map[string]any{"phase": "done"})
				sendEvent(w, "complete", map[string]any{
					"status":    string(service.StatusSuccess),
					"artifacts": arts,
				})
			} else {
				sendEvent(w, "error", map[string]any{
					"status":  string(service.StatusFailed),
					"message": res.ErrMsg,
				})
			}
			flusher.Flush()
			return
		}
	}
}

// sendEvent writes one SSE event with a JSON data payload.
func sendEvent(w http.ResponseWriter, event string, data any) {
	payload, err := json.Marshal(data)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload)
}
