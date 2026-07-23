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

	httpapi "github.com/open-edge-platform/image-composer-tool/internal/api/http"
	"github.com/open-edge-platform/image-composer-tool/internal/api/service"
)

// minimalTemplate is a schema-valid user template; see the service tests for the
// rationale. Duplicated here so the api tests are self-contained.
const minimalTemplate = `image:
  name: test-image
  version: "1.0"
target:
  os: ubuntu
  dist: ubuntu24
  arch: x86_64
  imageType: raw
`

// fakeICT is a shell script standing in for the image-composer-tool binary. It
// emits one ICT-style artifact block (bullet line + path line) so parseArtifacts
// records a single image artifact, then exits 0 — letting handler tests seed a
// completed, successful build without a real image build.
const fakeICT = `#!/bin/sh
echo "	INFO	display/display.go:79	    • image.raw.gz (1.13 GB)"
echo "	INFO	display/display.go:80	      /output/image.raw.gz"
exit 0
`

// newTestServer builds a Server backed by a real service with a temp templates
// dir, manifest file, and a fake ICT binary, so handler tests exercise the full
// decode→service→encode path through the generated routes.
func newTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	tdir := t.TempDir()
	for _, name := range []string{"robotics.yml", "retail.yml"} {
		if err := os.WriteFile(filepath.Join(tdir, name), []byte(minimalTemplate), 0o644); err != nil {
			t.Fatalf("writing template %s: %v", name, err)
		}
	}
	manifest := `combinations:
  - {vertical: robotics, sku: amr, platform: wcl, os: ubuntu24, imageType: iso, template: robotics.yml}
  - {vertical: retail, sku: dv, platform: arl, os: debian13, imageType: iso, template: retail.yml}
verticals:
  - {id: robotics, displayName: Robotics}
platforms:
  - {id: wcl, displayName: WCL}
targets:
  - {id: ubuntu24, displayName: "Ubuntu 24.04", os: ubuntu, arch: x86_64}
`
	mpath := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := os.WriteFile(mpath, []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	ictBin := filepath.Join(t.TempDir(), "fake-ict")
	if err := os.WriteFile(ictBin, []byte(fakeICT), 0o755); err != nil {
		t.Fatalf("write fake ICT: %v", err)
	}
	srv, err := New(Config{
		TemplatesDir: tdir,
		ICTBinary:    ictBin,
		WorkDir:      t.TempDir(),
		ManifestPath: mpath,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return srv, tdir
}

// do routes a request through the full mux + middleware and returns the recorder.
func (s *Server) do(req *http.Request) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	withMiddleware(s.routes()).ServeHTTP(rr, req)
	return rr
}

// seedBuild starts a real build (via the fake ICT binary) and waits for it to
// finish, returning its id. The build completes successfully with one image
// artifact, so it is ready to be queried by the details/artifacts/history
// handlers.
func (s *Server) seedBuild(t *testing.T) string {
	t.Helper()
	acc, err := s.svc.StartBuild(service.BuildRequest{Compose: &service.Selection{
		Vertical: "robotics", SKU: "amr", Platform: "wcl", OS: "ubuntu24", ImageType: "iso",
	}})
	if err != nil {
		t.Fatalf("StartBuild: %v", err)
	}
	h, ok := s.svc.Build(acc.BuildID)
	if !ok {
		t.Fatalf("build %s not tracked", acc.BuildID)
	}
	<-h.Done() // fake ICT exits immediately
	return acc.BuildID
}

// --- manifest ---

func TestHandleGetManifest(t *testing.T) {
	s, _ := newTestServer(t)
	rr := s.do(httptest.NewRequest(http.MethodGet, "/api/v1/manifest", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var m httpapi.Manifest
	if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(m.Combinations) != 2 {
		t.Errorf("combinations = %d, want 2", len(m.Combinations))
	}
	// Wire-format check: optional sku present, required camelCase fields.
	if m.Combinations[0].Sku == nil || *m.Combinations[0].Sku != "amr" {
		t.Errorf("combination sku not serialized: %+v", m.Combinations[0])
	}
}

// --- compose ---

func TestHandleComposeSuccess(t *testing.T) {
	s, _ := newTestServer(t)
	body := `{"vertical":"robotics","sku":"amr","platform":"wcl","os":"ubuntu24","imageType":"iso"}`
	rr := s.do(httptest.NewRequest(http.MethodPost, "/api/v1/templates/compose", strings.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rr.Code, rr.Body)
	}
	var resp httpapi.ComposeResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Template != "robotics.yml" || resp.Yaml == "" {
		t.Errorf("unexpected compose response: %+v", resp)
	}
	if resp.Summary.Vertical != "robotics" {
		t.Errorf("summary vertical = %q, want robotics", resp.Summary.Vertical)
	}
}

func TestHandleComposeErrors(t *testing.T) {
	s, _ := newTestServer(t)
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
			rr := s.do(httptest.NewRequest(http.MethodPost, "/api/v1/templates/compose", strings.NewReader(c.body)))
			if rr.Code != c.want {
				t.Errorf("status = %d, want %d (body: %s)", rr.Code, c.want, rr.Body)
			}
			// Error responses carry the {error:{code,message}} envelope.
			if rr.Code >= 400 {
				var eb httpapi.Error
				if err := json.Unmarshal(rr.Body.Bytes(), &eb); err != nil {
					t.Fatalf("error decode: %v", err)
				}
				if eb.Error.Code == "" || eb.Error.Message == "" {
					t.Errorf("error envelope incomplete: %s", rr.Body)
				}
			}
		})
	}
}

func TestHandleComposeInvalidTemplate(t *testing.T) {
	s, tdir := newTestServer(t)
	// Overwrite a matched template with schema-invalid content so load/merge
	// fails; compose must surface 422.
	if err := os.WriteFile(filepath.Join(tdir, "robotics.yml"), []byte("image:\n  name: broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	body := `{"vertical":"robotics","sku":"amr","platform":"wcl","os":"ubuntu24","imageType":"iso"}`
	rr := s.do(httptest.NewRequest(http.MethodPost, "/api/v1/templates/compose", strings.NewReader(body)))
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (body: %s)", rr.Code, rr.Body)
	}
}

// --- start build (error paths only; no real exec) ---

func TestHandleStartBuildErrors(t *testing.T) {
	s, _ := newTestServer(t)
	cases := []struct {
		name, body string
		want       int
	}{
		{"bad json", `{`, http.StatusBadRequest},
		{"no match", `{"compose":{"vertical":"robotics","platform":"ptl","os":"ubuntu24","imageType":"iso"}}`, http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rr := s.do(httptest.NewRequest(http.MethodPost, "/api/v1/builds", strings.NewReader(c.body)))
			if rr.Code != c.want {
				t.Errorf("status = %d, want %d", rr.Code, c.want)
			}
		})
	}
}

// TestHandleStartBuildAccepted starts a real build against /bin/true, which
// exits 0 immediately, so the handler returns 202 with the accepted envelope.
func TestHandleStartBuildAccepted(t *testing.T) {
	s, _ := newTestServer(t)
	body := `{"compose":{"vertical":"robotics","sku":"amr","platform":"wcl","os":"ubuntu24","imageType":"iso"}}`
	rr := s.do(httptest.NewRequest(http.MethodPost, "/api/v1/builds", strings.NewReader(body)))
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (body: %s)", rr.Code, rr.Body)
	}
	var acc httpapi.BuildAccepted
	if err := json.Unmarshal(rr.Body.Bytes(), &acc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if acc.BuildId == "" || acc.Status != "running" ||
		!strings.HasPrefix(acc.LogsUrl, "/api/v1/builds/") || !strings.HasSuffix(acc.LogsUrl, "/logs") {
		t.Errorf("unexpected accepted envelope: %+v", acc)
	}
}

// --- artifacts / details handlers (through the mux, with path params) ---

func TestHandleBuildArtifacts(t *testing.T) {
	s, _ := newTestServer(t)
	id := s.seedBuild(t)

	rr := s.do(httptest.NewRequest(http.MethodGet, "/api/v1/builds/"+id+"/artifacts", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var out httpapi.ArtifactList
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Status != "success" || len(out.Artifacts) != 1 {
		t.Errorf("artifacts response = %+v", out)
	}
	if out.Artifacts[0].Name != "image.raw.gz" {
		t.Errorf("artifact name = %q, want image.raw.gz", out.Artifacts[0].Name)
	}

	// missing build -> 404
	rr2 := s.do(httptest.NewRequest(http.MethodGet, "/api/v1/builds/nope/artifacts", nil))
	if rr2.Code != http.StatusNotFound {
		t.Errorf("missing build status = %d, want 404", rr2.Code)
	}
}

func TestHandleBuildDetails(t *testing.T) {
	s, _ := newTestServer(t)
	id := s.seedBuild(t)

	rr := s.do(httptest.NewRequest(http.MethodGet, "/api/v1/builds/"+id+"/details", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rr.Code, rr.Body)
	}
	var out httpapi.BuildDetails
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.BuildId != id || out.TemplateUrl != "/api/v1/builds/"+id+"/template" {
		t.Errorf("details mismatch: %+v", out)
	}

	// missing build -> 404
	rr2 := s.do(httptest.NewRequest(http.MethodGet, "/api/v1/builds/nope/details", nil))
	if rr2.Code != http.StatusNotFound {
		t.Errorf("missing build status = %d, want 404", rr2.Code)
	}
}

// --- history list ---

func TestHandleListBuilds(t *testing.T) {
	s, _ := newTestServer(t)
	s.seedBuild(t)

	rr := s.do(httptest.NewRequest(http.MethodGet, "/api/v1/builds", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var out httpapi.BuildList
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Builds) != 1 {
		t.Fatalf("builds = %d, want 1", len(out.Builds))
	}
}
