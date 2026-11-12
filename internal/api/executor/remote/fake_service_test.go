// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package remote

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/api/service"
)

// fakeService is an in-memory remote build service that follows
// api/v1/remote-build-protocol.yaml. Tests drive each build by hand: push log
// lines, phases and metadata, then finish it.
type fakeService struct {
	mu          sync.Mutex
	info        Info
	builds      map[string]*fakeBuild
	submissions []submission
	cancels     []string
	token       string
	// dropStreamAfter, when > 0, closes every event stream after that many
	// log lines without a done event, to exercise reconnection.
	dropStreamAfter int
	srv             *httptest.Server
}

type fakeBuild struct {
	state buildState
	lines []string
	done  bool
	wake  chan struct{}
}

func newFakeService() *fakeService {
	f := &fakeService{
		info: Info{
			Name: "fake-farm", Protocol: Protocol, MaxConcurrent: 3,
			Phases: []service.Phase{{ID: "dispatching", Label: "Preparing infra"}},
		},
		builds: map[string]*fakeBuild{},
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	return f
}

func (f *fakeService) URL() string { return f.srv.URL }
func (f *fakeService) Close()      { f.srv.Close() }

func (f *fakeService) get(id string) *fakeBuild {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.builds[id]
}

func (f *fakeService) notify(b *fakeBuild) {
	close(b.wake)
	b.wake = make(chan struct{})
}

// log appends a line and wakes streams.
func (f *fakeService) log(id string, lines ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b := f.builds[id]
	b.lines = append(b.lines, lines...)
	b.state.Status = "running"
	f.notify(b)
}

func (f *fakeService) phase(id, pid, label string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b := f.builds[id]
	b.state.Phase = &service.Phase{ID: pid, Label: label}
	f.notify(b)
}

func (f *fakeService) meta(id, k, v string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b := f.builds[id]
	if b.state.Meta == nil {
		b.state.Meta = map[string]string{}
	}
	b.state.Meta[k] = v
	f.notify(b)
}

func (f *fakeService) finish(id, status, errMsg string, arts ...artifact) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b := f.builds[id]
	b.state.Status = status
	b.state.Error = errMsg
	b.state.Artifacts = arts
	b.done = true
	f.notify(b)
}

// seed registers a build the service already knows about, as if submitted by
// an earlier ICT instance.
func (f *fakeService) seed(id, status string, lines ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.builds[id] = &fakeBuild{
		state: buildState{ID: id, Status: status}, lines: lines, done: isTerminal(status), wake: make(chan struct{}),
	}
}

func isTerminal(s string) bool { return s == "succeeded" || s == "failed" || s == "cancelled" }

func (f *fakeService) serve(w http.ResponseWriter, r *http.Request) {
	if f.token != "" && r.Header.Get("Authorization") != "Bearer "+f.token {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "token required")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/v1")
	switch {
	case path == "/info" && r.Method == http.MethodGet:
		f.mu.Lock()
		info := f.info
		f.mu.Unlock()
		writeJSON(w, http.StatusOK, info)
	case path == "/builds" && r.Method == http.MethodPost:
		var sub submission
		if err := json.NewDecoder(r.Body).Decode(&sub); err != nil || sub.ID == "" || sub.Template == "" {
			writeErr(w, http.StatusBadRequest, "BAD_REQUEST", "id and template are required")
			return
		}
		f.mu.Lock()
		f.submissions = append(f.submissions, sub)
		b, exists := f.builds[sub.ID]
		if !exists {
			b = &fakeBuild{state: buildState{ID: sub.ID, Status: "queued"}, wake: make(chan struct{})}
			f.builds[sub.ID] = b
		}
		state := b.state
		f.mu.Unlock()
		writeJSON(w, http.StatusAccepted, state)
	case strings.HasPrefix(path, "/builds/"):
		rest := strings.TrimPrefix(path, "/builds/")
		id, action, _ := strings.Cut(rest, "/")
		b := f.get(id)
		if b == nil {
			writeErr(w, http.StatusNotFound, "NOT_FOUND", "no such build")
			return
		}
		switch {
		case action == "" && r.Method == http.MethodGet:
			f.mu.Lock()
			state := b.state
			f.mu.Unlock()
			writeJSON(w, http.StatusOK, state)
		case action == "cancel" && r.Method == http.MethodPost:
			f.mu.Lock()
			f.cancels = append(f.cancels, id)
			done := b.done
			if !done {
				b.state.Status = "cancelling"
				f.notify(b)
			}
			f.mu.Unlock()
			if done {
				writeErr(w, http.StatusConflict, "FINISHED", "build already finished")
				return
			}
			w.WriteHeader(http.StatusAccepted)
		case action == "events" && r.Method == http.MethodGet:
			f.stream(w, r, b)
		default:
			writeErr(w, http.StatusNotFound, "NOT_FOUND", "no such route")
		}
	default:
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "no such route")
	}
}

func (f *fakeService) stream(w http.ResponseWriter, r *http.Request, b *fakeBuild) {
	from, _ := strconv.Atoi(r.URL.Query().Get("from"))
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	fl := w.(http.Flusher)
	send := func(name string, v any) {
		data, _ := json.Marshal(v)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, data)
		fl.Flush()
	}
	fmt.Fprint(w, ": hello\n\n")
	sentThisConn := 0
	for {
		f.mu.Lock()
		state := b.state
		lines := b.lines
		done := b.done
		wake := b.wake
		f.mu.Unlock()
		for k, v := range state.Meta {
			send("meta", metaEvent{Key: k, Value: v})
		}
		if state.Phase != nil {
			send("phase", state.Phase)
		}
		for from < len(lines) {
			send("log", logEvent{N: int64(from), Line: lines[from]})
			from++
			sentThisConn++
			if f.dropStreamAfter > 0 && sentThisConn >= f.dropStreamAfter && !done {
				return // simulate a broken connection
			}
		}
		if done {
			send("done", doneEvent{Status: state.Status, Error: state.Error, Artifacts: state.Artifacts})
			return
		}
		select {
		case <-wake:
		case <-r.Context().Done():
			return
		case <-time.After(200 * time.Millisecond):
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		}
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, c, msg string) {
	writeJSON(w, code, map[string]any{"error": map[string]string{"code": c, "message": msg}})
}

// openTestStream GETs an event stream and returns its body for readEvents.
func openTestStream(url string) (io.ReadCloser, error) {
	resp, err := http.Get(url) //nolint:gosec // test helper against an httptest server
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return resp.Body, nil
}
