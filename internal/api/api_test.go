// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
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

// minimalTemplate is a schema-valid user template (image + target are the only
// required top-level fields). LoadAndMergeTemplate validates the user template
// and, when OS defaults can't be loaded (as in a unit-test cwd), returns it
// as-is — so compose succeeds without the repo's config/osv tree present.
const minimalTemplate = `image:
  name: test-image
  version: "1.0"
target:
  os: ubuntu
  dist: ubuntu24
  arch: x86_64
  imageType: raw
`

// newTestServer builds a Server with the test manifest and a templates dir
// containing a schema-valid template file for each combination.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"robotics.yml", "retail.yml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(minimalTemplate), 0o644); err != nil {
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
	if err := os.WriteFile(p, []byte("combinations:\n  - {vertical: v, platform: p, os: o, imageType: raw, template: t.yml}\n"), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
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

func TestFindTemplateAmbiguousSKU(t *testing.T) {
	// Two combinations differ only by SKU; an omitted SKU is ambiguous and must
	// not resolve to an order-dependent guess.
	m := &Manifest{Combinations: []Combination{
		{Vertical: "v", SKU: "a", Platform: "p", OS: "o", ImageType: "raw", Template: "a.yml"},
		{Vertical: "v", SKU: "b", Platform: "p", OS: "o", ImageType: "raw", Template: "b.yml"},
	}}
	if got := m.findTemplate("v", "", "p", "o", "raw"); got != "" {
		t.Errorf("ambiguous no-SKU match = %q, want empty", got)
	}
	// With the SKU specified it resolves unambiguously.
	if got := m.findTemplate("v", "b", "p", "o", "raw"); got != "b.yml" {
		t.Errorf("explicit-SKU match = %q, want b.yml", got)
	}
}

func TestSafeTemplatePath(t *testing.T) {
	dir := t.TempDir()

	// Valid relative name resolves under dir.
	got, err := safeTemplatePath(dir, "robotics.yml")
	if err != nil || filepath.Dir(got) != dir {
		t.Errorf("valid name: got %q err %v", got, err)
	}

	// Traversal and absolute paths are rejected.
	for _, bad := range []string{"../escape.yml", "../../etc/passwd", "/etc/passwd", "", "sub/../../x"} {
		if _, err := safeTemplatePath(dir, bad); err == nil {
			t.Errorf("safeTemplatePath(%q) = nil error, want rejection", bad)
		}
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

func TestHandleComposeInvalidTemplate(t *testing.T) {
	s := newTestServer(t)
	// Overwrite a matched template with schema-invalid content so the
	// load/merge step fails; compose must surface 422, not a misleading 200.
	if err := os.WriteFile(filepath.Join(s.cfg.TemplatesDir, "robotics.yml"), []byte("image:\n  name: broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	body := `{"vertical":"robotics","sku":"amr","platform":"wcl","os":"ubuntu24","imageType":"iso"}`
	s.handleCompose(rr, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body)))
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (body: %s)", rr.Code, rr.Body)
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
	name, args := s.buildCommand("/tmp/t.yml", "/tmp/wd", "/tmp/cd")
	if name != "/opt/ict" || args[0] != "build" || args[1] != "/tmp/t.yml" {
		t.Fatalf("non-sudo cmd = %s %v", name, args)
	}
	if !slices.Contains(args, "--cache-dir") || !slices.Contains(args, "/tmp/cd") {
		t.Fatalf("missing --cache-dir in %v", args)
	}

	s.cfg.Sudo = true
	name, args = s.buildCommand("/tmp/t.yml", "/tmp/wd", "/tmp/cd")
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
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
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
	if err := json.Unmarshal(rr2.Body.Bytes(), &eb); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if eb.Error.Code != "CODE" || eb.Error.Message != "message" {
		t.Fatalf("error body = %+v", eb)
	}
}

func TestCORSLocalhostOnly(t *testing.T) {
	h := withMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, f := range []string{
		"minimal.raw.gz",    // image
		"minimal.sbom.json", // sbom (name contains "sbom")
		"notes.txt",         // ignored
	} {
		if err := os.WriteFile(filepath.Join(sub, f), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", f, err)
		}
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

// --- ICT binary discovery ---

func TestDiscoverICTBinary(t *testing.T) {
	// Prefers ./build/image-composer-tool when present.
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll("build", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("build/image-composer-tool", []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := discoverICTBinary(); got != "./build/image-composer-tool" {
		t.Errorf("with build/ present, got %q, want ./build/image-composer-tool", got)
	}

	// Falls back to the repo-root binary when ./build/ has none.
	dir2 := t.TempDir()
	t.Chdir(dir2)
	if err := os.WriteFile("image-composer-tool", []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := discoverICTBinary(); got != "./image-composer-tool" {
		t.Errorf("with only root binary, got %q, want ./image-composer-tool", got)
	}

	// With neither present (and not on PATH), returns the conventional path.
	dir3 := t.TempDir()
	t.Chdir(dir3)
	if got := discoverICTBinary(); got != "./build/image-composer-tool" {
		// Note: if a real image-composer-tool is on the test host's PATH, this
		// branch returns that instead — accept an absolute path too.
		if !filepath.IsAbs(got) {
			t.Errorf("with nothing present, got %q, want ./build/... or an abs PATH hit", got)
		}
	}
}

// --- build details / template / artifact-download handlers ---

func TestHandleBuildDetails(t *testing.T) {
	s := newTestServer(t)
	b := &build{
		ID:           "d1",
		WorkDir:      "/tmp/work",
		CacheDir:     "/tmp/cache",
		Template:     "robotics.yml",
		TemplatePath: "/tmp/robotics.yml",
		Command:      "sudo -n ict build robotics.yml",
		done:         make(chan struct{}),
	}
	b.finish(statusSuccess, nil, "")
	s.tracker.add(b)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/d1/details", nil)
	req.SetPathValue("id", "d1")
	rr := httptest.NewRecorder()
	s.handleBuildDetails(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var out buildDetails
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Command != b.Command || out.WorkDir != b.WorkDir {
		t.Errorf("details mismatch: %+v", out)
	}

	// missing build -> 404
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/builds/nope/details", nil)
	req2.SetPathValue("id", "nope")
	rr2 := httptest.NewRecorder()
	s.handleBuildDetails(rr2, req2)
	if rr2.Code != http.StatusNotFound {
		t.Errorf("missing build status = %d, want 404", rr2.Code)
	}
}

func TestHandleBuildTemplate(t *testing.T) {
	s := newTestServer(t)
	// Write a real template file so the handler can read it.
	tmpFile := filepath.Join(t.TempDir(), "robotics.yml")
	if err := os.WriteFile(tmpFile, []byte(minimalTemplate), 0o644); err != nil {
		t.Fatal(err)
	}
	b := &build{
		ID:           "t1",
		Template:     "robotics.yml",
		TemplatePath: tmpFile,
		done:         make(chan struct{}),
	}
	close(b.done)
	s.tracker.add(b)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/t1/template", nil)
	req.SetPathValue("id", "t1")
	rr := httptest.NewRecorder()
	s.handleBuildTemplate(rr, req)
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
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/builds/nope/template", nil)
	req2.SetPathValue("id", "nope")
	rr2 := httptest.NewRecorder()
	s.handleBuildTemplate(rr2, req2)
	if rr2.Code != http.StatusNotFound {
		t.Errorf("missing build status = %d, want 404", rr2.Code)
	}
}

func TestHandleBuildArtifactDownload(t *testing.T) {
	s := newTestServer(t)
	// Artifact must live inside WorkDir to pass the path validation guard.
	workDir := t.TempDir()
	artifactFile := filepath.Join(workDir, "image.iso")
	if err := os.WriteFile(artifactFile, []byte("fake-iso-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := &build{
		ID:      "a1",
		RootDir: workDir, // so finish()'s writeMeta stays in the temp dir
		WorkDir: workDir,
		done:    make(chan struct{}),
	}
	b.finish(statusSuccess, []artifact{{Name: "image.iso", Type: "image", Path: artifactFile}}, "")
	s.tracker.add(b)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/a1/artifacts/image.iso", nil)
	req.SetPathValue("id", "a1")
	req.SetPathValue("name", "image.iso")
	rr := httptest.NewRecorder()
	s.handleBuildArtifactDownload(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rr.Code, rr.Body)
	}
	if rr.Body.String() != "fake-iso-content" {
		t.Errorf("body = %q, want fake-iso-content", rr.Body.String())
	}

	// unknown artifact name -> 404
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/builds/a1/artifacts/nope.iso", nil)
	req2.SetPathValue("id", "a1")
	req2.SetPathValue("name", "nope.iso")
	rr2 := httptest.NewRecorder()
	s.handleBuildArtifactDownload(rr2, req2)
	if rr2.Code != http.StatusNotFound {
		t.Errorf("unknown artifact status = %d, want 404", rr2.Code)
	}

	// missing build -> 404
	req3 := httptest.NewRequest(http.MethodGet, "/api/v1/builds/nope/artifacts/image.iso", nil)
	req3.SetPathValue("id", "nope")
	req3.SetPathValue("name", "image.iso")
	rr3 := httptest.NewRecorder()
	s.handleBuildArtifactDownload(rr3, req3)
	if rr3.Code != http.StatusNotFound {
		t.Errorf("missing build status = %d, want 404", rr3.Code)
	}
}

// TestHandleBuildArtifactRelativeWorkDir guards the regression where a relative
// WorkDir (the default `webui-workspace`) made the absolute-vs-relative prefix
// check always fail, wrongly returning 403 for a legitimate artifact.
func TestHandleBuildArtifactRelativeWorkDir(t *testing.T) {
	s := newTestServer(t)

	// Relative work dir under a temp cwd, with a deeply nested artifact like a
	// real ICT build produces.
	t.Chdir(t.TempDir())
	relWorkDir := filepath.Join("webui-workspace", "builds", "rel1", "work")
	nested := filepath.Join(relWorkDir, "debian-debian13-x86_64", "imagebuild", "out")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	artifactFile := filepath.Join(nested, "image.iso")
	if err := os.WriteFile(artifactFile, []byte("iso-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	b := &build{ID: "rel1", RootDir: "webui-workspace/builds/rel1", WorkDir: relWorkDir, done: make(chan struct{})}
	b.finish(statusSuccess, []artifact{{Name: "image.iso", Type: "image", Path: artifactFile}}, "")
	s.tracker.add(b)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/rel1/artifacts/image.iso", nil)
	req.SetPathValue("id", "rel1")
	req.SetPathValue("name", "image.iso")
	rr := httptest.NewRecorder()
	s.handleBuildArtifactDownload(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rr.Code, rr.Body)
	}
	if rr.Body.String() != "iso-bytes" {
		t.Errorf("body = %q, want iso-bytes", rr.Body.String())
	}
}

// --- compose history (list + disk hydration) ---

func TestHistoryListAndHydrate(t *testing.T) {
	s := newTestServer(t)

	// A live in-memory build (running).
	live := &build{
		ID:        "live1",
		RootDir:   filepath.Join(s.cfg.WorkDir, "builds", "live1"),
		Template:  "robotics.yml",
		Summary:   &composeSummary{Vertical: "robotics"},
		CreatedAt: time.Now(),
		done:      make(chan struct{}),
	}
	live.status = statusRunning
	s.tracker.add(live)

	// A past build present only on disk via meta.json (not in memory).
	pastRoot := filepath.Join(s.cfg.WorkDir, "builds", "past1")
	if err := os.MkdirAll(pastRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	meta := buildMeta{
		ID:        "past1",
		Status:    "success",
		Template:  "retail.yml",
		Command:   "ict build retail.yml",
		CreatedAt: time.Now().Add(-time.Hour),
		Summary:   &composeSummary{Vertical: "retail"},
		Artifacts: []artifact{{Name: "img.iso", Type: "image", Path: filepath.Join(pastRoot, "img.iso")}},
	}
	data, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(metaPath(pastRoot), data, 0o600); err != nil {
		t.Fatal(err)
	}

	// List returns both, newest-first (live is newer).
	rr := httptest.NewRecorder()
	s.handleListBuilds(rr, httptest.NewRequest(http.MethodGet, "/api/v1/builds", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var out struct {
		Builds []historyItem `json:"builds"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Builds) != 2 {
		t.Fatalf("builds = %d, want 2: %+v", len(out.Builds), out.Builds)
	}
	if out.Builds[0].ID != "live1" || out.Builds[1].ID != "past1" {
		t.Errorf("order = %s,%s want live1,past1", out.Builds[0].ID, out.Builds[1].ID)
	}

	// getBuild hydrates the past build from disk (not in tracker).
	if _, ok := s.tracker.get("past1"); ok {
		t.Fatal("past1 should not be in the tracker")
	}
	hb, ok := s.getBuild("past1")
	if !ok {
		t.Fatal("getBuild past1 = not found, want hydrated from disk")
	}
	if hb.Template != "retail.yml" || hb.snapshot().status != statusSuccess {
		t.Errorf("hydrated build wrong: %+v", hb)
	}

	// Unknown build id is not found.
	if _, ok := s.getBuild("does-not-exist"); ok {
		t.Error("getBuild unknown id = found, want not found")
	}
}
