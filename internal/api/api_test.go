// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testManifest returns a small in-memory manifest for handler tests.
func testManifest() *Manifest {
	return &Manifest{
		Combinations: []Combination{
			{Vertical: "robotics", SKU: "amr", Platform: "wcl", OS: "ubuntu24", ImageType: "iso", Template: "robotics.yml"},
			{Vertical: "retail", SKU: "dv", Platform: "arl", OS: "debian13", ImageType: "iso", Template: "retail.yml"},
		},
		Verticals: []Option{{ID: "robotics", DisplayName: "Robotics"}, {ID: "retail", DisplayName: "Retail Edge"}},
		SKUs:      []Option{{ID: "amr", DisplayName: "AMR"}, {ID: "dv", DisplayName: "Desktop Virtualization"}},
		Platforms: []Option{{ID: "wcl", DisplayName: "WCL"}, {ID: "arl", DisplayName: "ARL"}},
		Targets:   []Target{{ID: "ubuntu24", DisplayName: "Ubuntu 24.04", OS: "ubuntu", Arch: "x86_64"}},
	}
}

// newTestServer builds a Server with the test manifest and a templates dir
// containing a dummy template file for each combination.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"robotics.yml", "retail.yml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("image:\n  name: dummy\n"), 0o644); err != nil {
			t.Fatalf("writing template %s: %v", name, err)
		}
	}
	return &Server{
		cfg:      Config{TemplatesDir: dir, ICTBinary: "/bin/true", WorkDir: t.TempDir()},
		manifest: testManifest(),
		tracker:  newBuildTracker(),
	}
}

// --- manifest ---

func TestLoadManifestEmbedded(t *testing.T) {
	m, err := loadManifest("")
	if err != nil {
		t.Fatalf("loadManifest embedded: %v", err)
	}
	if len(m.Combinations) == 0 || len(m.Verticals) == 0 {
		t.Fatalf("embedded manifest looks empty: %+v", m)
	}
}

func TestLoadManifestFromFileAndError(t *testing.T) {
	// From disk.
	p := filepath.Join(t.TempDir(), "m.yaml")
	os.WriteFile(p, []byte("combinations:\n  - {vertical: v, platform: p, os: o, imageType: raw, template: t.yml}\n"), 0o644)
	m, err := loadManifest(p)
	if err != nil {
		t.Fatalf("loadManifest file: %v", err)
	}
	if len(m.Combinations) != 1 || m.Combinations[0].Template != "t.yml" {
		t.Fatalf("unexpected parse: %+v", m.Combinations)
	}
	// Missing file errors.
	if _, err := loadManifest(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Fatal("expected error for missing manifest file")
	}
}

func TestFindTemplate(t *testing.T) {
	m := testManifest()
	if got := m.findTemplate("robotics", "amr", "wcl", "ubuntu24", "iso"); got != "robotics.yml" {
		t.Errorf("exact match = %q, want robotics.yml", got)
	}
	// SKU empty in request matches regardless of combination SKU.
	if got := m.findTemplate("robotics", "", "wcl", "ubuntu24", "iso"); got != "robotics.yml" {
		t.Errorf("empty-sku match = %q, want robotics.yml", got)
	}
	// No match.
	if got := m.findTemplate("robotics", "amr", "ptl", "ubuntu24", "iso"); got != "" {
		t.Errorf("no-match = %q, want empty", got)
	}
}

// --- read handlers ---

func TestHandleGetManifest(t *testing.T) {
	s := newTestServer(t)
	rr := httptest.NewRecorder()
	s.handleGetManifest(rr, httptest.NewRequest(http.MethodGet, "/api/v1/manifest", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var m Manifest
	if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(m.Combinations) != 2 {
		t.Errorf("combinations = %d, want 2", len(m.Combinations))
	}
}

func TestHandleComposeSuccess(t *testing.T) {
	s := newTestServer(t)
	rr := httptest.NewRecorder()
	body := `{"vertical":"robotics","sku":"amr","platform":"wcl","os":"ubuntu24","imageType":"iso"}`
	s.handleCompose(rr, httptest.NewRequest(http.MethodPost, "/api/v1/templates/compose", strings.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rr.Code, rr.Body)
	}
	var resp composeResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Template != "robotics.yml" || resp.YAML == "" {
		t.Errorf("unexpected compose response: %+v", resp)
	}
}

func TestHandleComposeErrors(t *testing.T) {
	s := newTestServer(t)
	cases := []struct {
		name, body string
		want       int
	}{
		{"bad json", `{`, http.StatusBadRequest},
		{"missing fields", `{"vertical":"robotics"}`, http.StatusBadRequest},
		{"no match", `{"vertical":"robotics","platform":"ptl","os":"ubuntu24","imageType":"iso"}`, http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			s.handleCompose(rr, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(c.body)))
			if rr.Code != c.want {
				t.Errorf("status = %d, want %d", rr.Code, c.want)
			}
		})
	}
}

func TestHandleComposeTemplateMissingOnDisk(t *testing.T) {
	s := newTestServer(t)
	// Manifest references a template file that isn't on disk.
	s.manifest.Combinations = append(s.manifest.Combinations, Combination{
		Vertical: "ghost", Platform: "wcl", OS: "ubuntu24", ImageType: "raw", Template: "missing.yml",
	})
	rr := httptest.NewRecorder()
	body := `{"vertical":"ghost","platform":"wcl","os":"ubuntu24","imageType":"raw"}`
	s.handleCompose(rr, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body)))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}
}

// --- build tracker / state ---

func TestBuildTrackerAddGet(t *testing.T) {
	tr := newBuildTracker()
	b := &build{ID: "b1", status: statusRunning, done: make(chan struct{})}
	tr.add(b)
	got, ok := tr.get("b1")
	if !ok || got != b {
		t.Fatal("get did not return the added build")
	}
	if _, ok := tr.get("nope"); ok {
		t.Fatal("get returned a missing build")
	}
}

func TestBuildSnapshotAndFinish(t *testing.T) {
	b := &build{ID: "b", status: statusRunning, done: make(chan struct{})}
	if s := b.snapshot(); s.status != statusRunning {
		t.Fatalf("initial status = %q", s.status)
	}
	arts := []artifact{{Name: "img.raw.gz", Type: "image", Path: "/o/img.raw.gz"}}
	b.finish(statusSuccess, arts, "")
	s := b.snapshot()
	if s.status != statusSuccess || len(s.artifacts) != 1 || s.artifacts[0].Name != "img.raw.gz" {
		t.Fatalf("post-finish snapshot = %+v", s)
	}
	// snapshot returns a copy — mutating it must not affect the build.
	s.artifacts[0].Name = "mutated"
	if b.snapshot().artifacts[0].Name != "img.raw.gz" {
		t.Fatal("snapshot did not return an independent copy")
	}
}

func TestBuildAppendAndSnapshotLogs(t *testing.T) {
	b := &build{ID: "b", done: make(chan struct{})}
	b.appendLog("line1")
	b.appendLog("line2")
	logs := b.snapshotLogs()
	if len(logs) != 2 || logs[0] != "line1" {
		t.Fatalf("logs = %v", logs)
	}
	logs[0] = "x" // mutating the copy must not affect the build
	if b.snapshotLogs()[0] != "line1" {
		t.Fatal("snapshotLogs did not return an independent copy")
	}
}

func TestFinishFailure(t *testing.T) {
	b := &build{ID: "b", status: statusRunning, done: make(chan struct{})}
	b.finish(statusFailed, nil, "boom")
	s := b.snapshot()
	if s.status != statusFailed || s.errMsg != "boom" {
		t.Fatalf("snapshot = %+v", s)
	}
}

// --- build command / template resolution (no real exec) ---

func TestBuildCommand(t *testing.T) {
	s := &Server{cfg: Config{ICTBinary: "/opt/ict"}}
	name, args := s.buildCommand("/tmp/t.yml", "/tmp/wd")
	if name != "/opt/ict" || args[0] != "build" || args[1] != "/tmp/t.yml" {
		t.Fatalf("non-sudo cmd = %s %v", name, args)
	}

	s.cfg.Sudo = true
	name, args = s.buildCommand("/tmp/t.yml", "/tmp/wd")
	if name != "sudo" || args[0] != "-n" || args[1] != "/opt/ict" || args[2] != "build" {
		t.Fatalf("sudo cmd = %s %v", name, args)
	}
}

func TestResolveBuildTemplate(t *testing.T) {
	s := newTestServer(t)
	wd := t.TempDir()

	// compose path -> manifest lookup
	path, name, err := s.resolveBuildTemplate(&buildRequest{Compose: &composeRequest{
		Vertical: "robotics", SKU: "amr", Platform: "wcl", OS: "ubuntu24", ImageType: "iso",
	}}, wd)
	if err != nil || name != "robotics.yml" || !strings.HasSuffix(path, "robotics.yml") {
		t.Fatalf("compose resolve = %q %q %v", path, name, err)
	}

	// yaml path -> writes template.yml into the work dir
	path, name, err = s.resolveBuildTemplate(&buildRequest{YAML: "image:\n  name: x\n"}, wd)
	if err != nil || name != "template.yml" {
		t.Fatalf("yaml resolve = %q %q %v", path, name, err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("yaml template not written: %v", statErr)
	}

	// neither -> error
	if _, _, err := s.resolveBuildTemplate(&buildRequest{}, wd); err == nil {
		t.Fatal("expected error when neither compose nor yaml provided")
	}
	// no match -> error
	if _, _, err := s.resolveBuildTemplate(&buildRequest{Compose: &composeRequest{
		Vertical: "robotics", Platform: "ptl", OS: "ubuntu24", ImageType: "iso",
	}}, wd); err == nil {
		t.Fatal("expected error for unmatched combination")
	}
}

// --- artifacts handler ---

func TestHandleBuildArtifacts(t *testing.T) {
	s := newTestServer(t)
	b := &build{ID: "b1", done: make(chan struct{})}
	b.finish(statusSuccess, []artifact{{Name: "img", Type: "image", Path: "/o/img"}}, "")
	s.tracker.add(b)

	// present
	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/b1/artifacts", nil)
	req.SetPathValue("id", "b1")
	rr := httptest.NewRecorder()
	s.handleBuildArtifacts(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var out artifactList
	json.Unmarshal(rr.Body.Bytes(), &out)
	if out.Status != "success" || len(out.Artifacts) != 1 {
		t.Errorf("artifacts response = %+v", out)
	}

	// missing build -> 404
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/builds/nope/artifacts", nil)
	req2.SetPathValue("id", "nope")
	rr2 := httptest.NewRecorder()
	s.handleBuildArtifacts(rr2, req2)
	if rr2.Code != http.StatusNotFound {
		t.Errorf("missing build status = %d, want 404", rr2.Code)
	}
}

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
	json.Unmarshal(rr2.Body.Bytes(), &eb)
	if eb.Error.Code != "CODE" || eb.Error.Message != "message" {
		t.Fatalf("error body = %+v", eb)
	}
}

func TestCORSPreflight(t *testing.T) {
	h := withMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodOptions, "/api/v1/manifest", nil))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS status = %d, want 204", rr.Code)
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("missing CORS header")
	}
}

func TestRecoverPanic(t *testing.T) {
	h := withMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("panic recovery status = %d, want 500", rr.Code)
	}
}

// --- server construction + routing ---

func TestNewAndRoutes(t *testing.T) {
	s, err := New(Config{TemplatesDir: t.TempDir(), WorkDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s.manifest == nil || s.tracker == nil {
		t.Fatal("server not fully initialized")
	}
	// New applies defaults.
	if s.cfg.ICTBinary == "" {
		t.Error("ICTBinary default not applied")
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

// --- discoverArtifacts (filesystem fallback) ---

func TestDiscoverArtifacts(t *testing.T) {
	dir := t.TempDir()
	// nested layout mirroring ICT output
	sub := filepath.Join(dir, "ubuntu-x86_64", "imagebuild", "minimal")
	os.MkdirAll(sub, 0o755)
	for _, f := range []string{
		"minimal.raw.gz",    // image
		"minimal.sbom.json", // sbom (name contains "sbom")
		"notes.txt",         // ignored
	} {
		os.WriteFile(filepath.Join(sub, f), []byte("x"), 0o644)
	}
	got := discoverArtifacts(dir)
	var img, sbom int
	for _, a := range got {
		switch a.Type {
		case "image":
			img++
		case "sbom":
			sbom++
		}
	}
	if img != 1 || sbom != 1 {
		t.Fatalf("discoverArtifacts found image=%d sbom=%d (%+v)", img, sbom, got)
	}
}

// --- handleStartBuild early-return error paths (no build spawned) ---

func TestHandleStartBuildErrors(t *testing.T) {
	s := newTestServer(t)
	cases := []struct {
		name, body string
		want       int
	}{
		{"bad json", `{`, http.StatusBadRequest},
		{"no match", `{"compose":{"vertical":"robotics","platform":"ptl","os":"ubuntu24","imageType":"iso"}}`, http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			s.handleStartBuild(rr, httptest.NewRequest(http.MethodPost, "/api/v1/builds", strings.NewReader(c.body)))
			if rr.Code != c.want {
				t.Errorf("status = %d, want %d", rr.Code, c.want)
			}
		})
	}
}

func TestHandleBuildLogsMissingBuild(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/nope/logs", nil)
	req.SetPathValue("id", "nope")
	rr := httptest.NewRecorder()
	s.handleBuildLogs(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

// handleBuildLogs streams an already-finished build's buffered logs plus a
// terminal complete event, then returns.
func TestHandleBuildLogsCompletedBuild(t *testing.T) {
	s := newTestServer(t)
	b := &build{ID: "done", done: make(chan struct{})}
	b.appendLog("building...")
	b.finish(statusSuccess, []artifact{{Name: "img", Type: "image", Path: "/o/img"}}, "")
	close(b.done) // build already finished
	s.tracker.add(b)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/done/logs", nil)
	req.SetPathValue("id", "done")
	rr := httptest.NewRecorder()
	s.handleBuildLogs(rr, req)

	out := rr.Body.String()
	if !strings.Contains(out, "event: log") || !strings.Contains(out, "building...") {
		t.Errorf("missing log event: %q", out)
	}
	if !strings.Contains(out, "event: complete") {
		t.Errorf("missing complete event: %q", out)
	}
}
