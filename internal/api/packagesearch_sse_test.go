// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// searchStreamServer returns a live server routing through the real mux and
// middleware, so these tests exercise the registered path rather than calling
// the handler directly — the stream path shares a prefix with the generated
// /packages/search route and must still win.
func searchStreamServer(t *testing.T) *httptest.Server {
	t.Helper()
	s, err := New(Config{TemplatesDir: t.TempDir(), WorkDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv := httptest.NewServer(withMiddleware(s.routes()))
	t.Cleanup(srv.Close)
	return srv
}

// sseEvent is one parsed `event:`/`data:` pair.
type sseEvent struct {
	name string
	data map[string]any
}

// readSSE reads the whole stream and returns its events in order.
func readSSE(t *testing.T, body *bufio.Scanner) []sseEvent {
	t.Helper()
	var events []sseEvent
	var name string
	for body.Scan() {
		line := body.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			var data map[string]any
			raw := strings.TrimPrefix(line, "data: ")
			if err := json.Unmarshal([]byte(raw), &data); err != nil {
				t.Fatalf("event %q data is not JSON: %v (%q)", name, err, raw)
			}
			events = append(events, sseEvent{name: name, data: data})
		}
	}
	return events
}

func TestSearchStreamRejectsBadQueryBeforeStreaming(t *testing.T) {
	srv := searchStreamServer(t)

	tests := []struct {
		name, query string
		wantCode    string
	}{
		{name: "single character", query: "?os=ubuntu24&q=r", wantCode: "BAD_REQUEST"},
		{name: "missing os", query: "?q=ros", wantCode: "BAD_REQUEST"},
		{name: "non-numeric limit", query: "?os=ubuntu24&q=ros&limit=lots", wantCode: "BAD_REQUEST"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Get(srv.URL + "/api/v1/packages/search/stream" + tc.query)
			if err != nil {
				t.Fatalf("GET: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", resp.StatusCode)
			}
			// The rejection must be JSON, not an SSE event: once the stream is
			// open the status has already been sent and cannot be corrected.
			if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
				t.Errorf("Content-Type = %q, want JSON", ct)
			}
			var body struct {
				Error struct{ Code string } `json:"error"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.Error.Code != tc.wantCode {
				t.Errorf("error code = %q, want %q", body.Error.Code, tc.wantCode)
			}
		})
	}
}

func TestSearchStreamEndsWithExactlyOneDone(t *testing.T) {
	srv := searchStreamServer(t)

	// An OS the manifest does not know has no repositories, so the stream has
	// nothing to look up and terminates immediately — no network involved.
	resp, err := http.Get(srv.URL + "/api/v1/packages/search/stream?os=no-such-os&q=ros")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	events := readSSE(t, bufio.NewScanner(resp.Body))
	if len(events) == 0 {
		t.Fatal("stream produced no events")
	}
	var dones int
	for _, e := range events {
		if e.name == "done" {
			dones++
		}
	}
	if dones != 1 {
		t.Errorf("got %d done events, want exactly 1: %+v", dones, events)
	}
	last := events[len(events)-1]
	if last.name != "done" {
		t.Errorf("last event is %q, want done", last.name)
	}
	// Nothing to search means nothing failed and nothing was cut short.
	if got := last.data["repos"]; got != float64(0) {
		t.Errorf("done.repos = %v, want 0", got)
	}
	if got := last.data["truncated"]; got != false {
		t.Errorf("done.truncated = %v, want false", got)
	}
}

// TestSearchStreamRouteWinsOverSearch guards the mux precedence directly: the
// streaming path is a suffix of the generated /packages/search route, and if
// that route matched first the stream would be served as JSON.
func TestSearchStreamRouteWinsOverSearch(t *testing.T) {
	srv := searchStreamServer(t)
	resp, err := http.Get(srv.URL + "/api/v1/packages/search/stream?os=no-such-os")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("stream path served as %q; the JSON route matched instead", ct)
	}
}
