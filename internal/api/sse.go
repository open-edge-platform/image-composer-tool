// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// handleBuildLogs streams a build's logs as Server-Sent Events. It replays any
// buffered lines, follows new ones until the build finishes, then emits a
// terminal `complete` or `error` event.
func (s *Server) handleBuildLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	b, ok := s.tracker.get(id)
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
	emit := func() {
		lines := b.snapshotLogs()
		for ; sent < len(lines); sent++ {
			sendEvent(w, "log", map[string]string{"message": lines[sent]})
		}
		flusher.Flush()
	}

	emit() // replay buffered history

	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			emit()
		case <-b.done:
			emit() // drain remaining lines
			if b.Status == statusSuccess {
				arts := b.artifacts
				if arts == nil {
					arts = []artifact{}
				}
				sendEvent(w, "complete", map[string]any{
					"status":    string(statusSuccess),
					"artifacts": arts,
				})
			} else {
				sendEvent(w, "error", map[string]any{
					"status":  string(statusFailed),
					"message": b.errMsg,
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
