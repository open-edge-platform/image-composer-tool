// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- middleware / helpers ---

func TestWriteJSONAndError(t *testing.T) {
	rr := httptest.NewRecorder()
	writeJSON(rr, http.StatusTeapot, map[string]string{"k": "v"})
	if rr.Code != http.StatusTeapot || rr.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("writeJSON code=%d ct=%q", rr.Code, rr.Header().Get("Content-Type"))
	}

	rr2 := httptest.NewRecorder()
	writeError(rr2, http.StatusBadRequest, "CODE", "message")
	var eb errorBody
	if err := json.Unmarshal(rr2.Body.Bytes(), &eb); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if eb.Error.Code != "CODE" || eb.Error.Message != "message" {
		t.Fatalf("error body = %+v", eb)
	}
}

func TestCORSLocalhostOnly(t *testing.T) {
	h := withMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Localhost origin is echoed back on a preflight.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/manifest", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS status = %d, want 204", rr.Code)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Errorf("ACAO = %q, want echoed localhost origin", got)
	}

	// A non-localhost origin must NOT receive a CORS allow header.
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodOptions, "/api/v1/manifest", nil)
	req2.Header.Set("Origin", "https://evil.example.com")
	h.ServeHTTP(rr2, req2)
	if got := rr2.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("ACAO = %q, want empty for non-localhost origin", got)
	}
}

func TestIsLocalhostOrigin(t *testing.T) {
	cases := map[string]bool{
		"http://localhost:5173":    true,
		"http://127.0.0.1:8080":    true,
		"http://[::1]:3000":        true,
		"https://evil.example.com": false,
		"":                         false,
		"::not a url::":            false,
	}
	for origin, want := range cases {
		if got := isLocalhostOrigin(origin); got != want {
			t.Errorf("isLocalhostOrigin(%q) = %v, want %v", origin, got, want)
		}
	}
}

func TestRecoverPanic(t *testing.T) {
	h := withMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("boom")
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("panic recovery status = %d, want 500", rr.Code)
	}
}

// --- server construction + routing ---

func TestNewAppliesDefaults(t *testing.T) {
	s, err := New(Config{TemplatesDir: t.TempDir(), WorkDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s.svc == nil {
		t.Fatal("service not initialized")
	}
	// Routed manifest request works end to end through the mux + middleware.
	srv := httptest.NewServer(withMiddleware(s.routes()))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/v1/manifest")
	if err != nil {
		t.Fatalf("GET manifest: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("routed manifest status = %d", resp.StatusCode)
	}
}

// --- sse event formatting ---

func TestSendEvent(t *testing.T) {
	rr := httptest.NewRecorder()
	sendEvent(rr, "log", map[string]string{"message": "hello"})
	out := rr.Body.String()
	if !strings.Contains(out, "event: log\n") || !strings.Contains(out, `"message":"hello"`) {
		t.Fatalf("sse output = %q", out)
	}
	if !strings.HasSuffix(out, "\n\n") {
		t.Fatalf("sse event must end with blank line: %q", out)
	}
}

// --- sse log stream ---

func TestHandleBuildLogsMissingBuild(t *testing.T) {
	s, _ := newTestServer(t)
	rr := s.do(httptest.NewRequest(http.MethodGet, "/api/v1/builds/nope/logs", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

// handleBuildLogs streams a completed build's buffered logs plus a terminal
// complete event, then returns.
func TestHandleBuildLogsCompletedBuild(t *testing.T) {
	s, _ := newTestServer(t)
	id := s.seedBuild(t)

	rr := s.do(httptest.NewRequest(http.MethodGet, "/api/v1/builds/"+id+"/logs", nil))
	out := rr.Body.String()
	if !strings.Contains(out, "event: log") {
		t.Errorf("missing log event: %q", out)
	}
	if !strings.Contains(out, "event: complete") {
		t.Errorf("missing complete event: %q", out)
	}
}

// --- template / logfile / artifact downloads ---

func TestHandleBuildTemplate(t *testing.T) {
	s, _ := newTestServer(t)
	id := s.seedBuild(t)

	rr := s.do(httptest.NewRequest(http.MethodGet, "/api/v1/builds/"+id+"/template", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rr.Code, rr.Body)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/yaml" {
		t.Errorf("Content-Type = %q, want application/yaml", ct)
	}
	if !strings.Contains(rr.Body.String(), "name: test-image") {
		t.Errorf("body missing template content: %q", rr.Body.String())
	}

	// missing build -> 404
	rr2 := s.do(httptest.NewRequest(http.MethodGet, "/api/v1/builds/nope/template", nil))
	if rr2.Code != http.StatusNotFound {
		t.Errorf("missing build status = %d, want 404", rr2.Code)
	}
}

func TestHandleBuildLogFile(t *testing.T) {
	s, _ := newTestServer(t)
	id := s.seedBuild(t)

	rr := s.do(httptest.NewRequest(http.MethodGet, "/api/v1/builds/"+id+"/logfile", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if !strings.HasPrefix(rr.Header().Get("Content-Type"), "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", rr.Header().Get("Content-Type"))
	}

	// missing build -> 404
	rr2 := s.do(httptest.NewRequest(http.MethodGet, "/api/v1/builds/nope/logfile", nil))
	if rr2.Code != http.StatusNotFound {
		t.Errorf("missing build status = %d, want 404", rr2.Code)
	}
}

func TestHandleBuildArtifactDownloadMissing(t *testing.T) {
	s, _ := newTestServer(t)
	id := s.seedBuild(t)

	// The seeded artifact path (/output/image.raw.gz) is outside the build work
	// dir, so the service rejects the download with 403 — exercises the guard.
	rr := s.do(httptest.NewRequest(http.MethodGet, "/api/v1/builds/"+id+"/artifacts/image.raw.gz", nil))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body: %s)", rr.Code, rr.Body)
	}

	// unknown artifact name -> 404
	rr2 := s.do(httptest.NewRequest(http.MethodGet, "/api/v1/builds/"+id+"/artifacts/nope.iso", nil))
	if rr2.Code != http.StatusNotFound {
		t.Errorf("unknown artifact status = %d, want 404", rr2.Code)
	}

	// missing build -> 404
	rr3 := s.do(httptest.NewRequest(http.MethodGet, "/api/v1/builds/nope/artifacts/x.iso", nil))
	if rr3.Code != http.StatusNotFound {
		t.Errorf("missing build status = %d, want 404", rr3.Code)
	}
}
