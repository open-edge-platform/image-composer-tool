// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"io"
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

// fakeICTRealArtifact is like fakeICT but writes a real file inside the build's
// --work-dir and reports that path, so the artifact passes the service's
// "inside the workspace" guard and the download handler's success path runs.
// Args are: build <template> --work-dir <dir> --cache-dir <dir>, so $4 is the
// work dir.
const fakeICTRealArtifact = `#!/bin/sh
out="$4/image.raw.gz"
printf 'artifact-bytes' > "$out"
echo "	INFO	display/display.go:79	    • image.raw.gz (1.13 GB)"
echo "	INFO	display/display.go:80	      $out"
exit 0
`

// newTestServer builds a Server backed by a real service with a temp templates
// dir, manifest file, and a fake ICT binary, so handler tests exercise the full
// decode→service→encode path through the generated routes.
func newTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	return newTestServerWithICT(t, fakeICT)
}

// newTestServerWithICT is newTestServer with a caller-supplied fake ICT script.
func newTestServerWithICT(t *testing.T, ictScript string) (*Server, string) {
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
	if err := os.WriteFile(ictBin, []byte(ictScript), 0o755); err != nil {
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

// --- advanced-mode stubs (contract-only; replaced in later PRs) ---

// The remaining Advanced-mode endpoint is wired to the generated interface but
// not yet implemented — it returns 501 with the standard error envelope. This
// guards the contract-only behavior; the PR that implements it deletes this test.
// (POST /templates/validate and GET /package-repos are implemented — see
// TestHandleValidateTemplate and TestHandleListPackageRepos.)
func TestHandleAdvancedStubsNotImplemented(t *testing.T) {
	s, _ := newTestServer(t)
	cases := []struct {
		name, method, path, body string
	}{
		{"packages-search", http.MethodGet, "/api/v1/packages/search?q=doc&os=ubuntu24", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var body io.Reader
			if c.body != "" {
				body = strings.NewReader(c.body)
			}
			rr := s.do(httptest.NewRequest(c.method, c.path, body))
			if rr.Code != http.StatusNotImplemented {
				t.Fatalf("status = %d, want 501 (body: %s)", rr.Code, rr.Body)
			}
			var eb httpapi.Error
			if err := json.Unmarshal(rr.Body.Bytes(), &eb); err != nil {
				t.Fatalf("error decode: %v", err)
			}
			if eb.Error.Code != "NOT_IMPLEMENTED" || eb.Error.Message == "" {
				t.Errorf("error envelope = %+v, want code NOT_IMPLEMENTED with message", eb.Error)
			}
		})
	}
}

// --- validate template ---

// A valid template returns 200 with valid=true and no errors.
func TestHandleValidateTemplateValid(t *testing.T) {
	s, _ := newTestServer(t)
	body := `{"yaml":"image:\n  name: my-image\n  version: \"1.0\"\ntarget:\n  os: ubuntu\n  dist: ubuntu24\n  arch: x86_64\n  imageType: raw\n"}`
	rr := s.do(httptest.NewRequest(http.MethodPost, "/api/v1/templates/validate", strings.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rr.Code, rr.Body)
	}
	var resp httpapi.ValidationResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Valid {
		t.Errorf("valid = %v, want true (body: %s)", resp.Valid, rr.Body)
	}
	if resp.Errors != nil && len(*resp.Errors) != 0 {
		t.Errorf("expected no errors, got %+v", *resp.Errors)
	}
}

// An invalid template is a SUCCESSFUL call: 200 with valid=false and one issue
// per bad field, each carrying a usable path — not a 4xx.
func TestHandleValidateTemplateInvalidIs200(t *testing.T) {
	s, _ := newTestServer(t)
	body := `{"yaml":"image:\n  name: \"-bad\"\n  version: \"1.0\"\ntarget:\n  os: not-an-os\n  imageType: qcow2\n"}`
	rr := s.do(httptest.NewRequest(http.MethodPost, "/api/v1/templates/validate", strings.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (a failed validation is a successful call); body: %s", rr.Code, rr.Body)
	}
	var resp httpapi.ValidationResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Valid {
		t.Fatalf("valid = %v, want false", resp.Valid)
	}
	if resp.Errors == nil || len(*resp.Errors) == 0 {
		t.Fatal("expected field-level errors")
	}
	for _, e := range *resp.Errors {
		if e.Path == "" {
			t.Errorf("issue missing field path: %+v", e)
		}
		if e.Message == "" {
			t.Errorf("issue missing message: %+v", e)
		}
	}
}

// A malformed request body (not the template — the JSON envelope) is a 400.
func TestHandleValidateTemplateBadJSON(t *testing.T) {
	s, _ := newTestServer(t)
	rr := s.do(httptest.NewRequest(http.MethodPost, "/api/v1/templates/validate", strings.NewReader(`{`)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %s)", rr.Code, rr.Body)
	}
}

// --- package repos ---

// getRepos issues GET /package-repos (with an optional ?os=) and returns the
// decoded list, failing the test on any non-200 or undecodable response.
func (s *Server) getRepos(t *testing.T, query string) httpapi.PackageRepoList {
	t.Helper()
	rr := s.do(httptest.NewRequest(http.MethodGet, "/api/v1/package-repos"+query, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rr.Code, rr.Body)
	}
	var out httpapi.PackageRepoList
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

// The unfiltered list serves the embedded catalog. Assert the wire contract —
// required fields populated and priority always present — rather than specific
// repo ids, so editing the catalog doesn't break the test.
func TestHandleListPackageRepos(t *testing.T) {
	s, _ := newTestServer(t)
	out := s.getRepos(t, "")
	if len(out.Repos) == 0 {
		t.Fatal("repos is empty; want the embedded catalog")
	}
	for _, r := range out.Repos {
		if r.Id == "" || r.DisplayName == "" || r.Url == "" {
			t.Errorf("repo missing required field: %+v", r)
		}
		// Priority is always emitted: the service defaults unset entries, so a nil
		// here would read as "no priority" rather than "the default".
		if r.Priority == nil {
			t.Errorf("repo %q: priority not serialized", r.Id)
		} else if *r.Priority <= 0 {
			t.Errorf("repo %q: priority = %d, want > 0", r.Id, *r.Priority)
		}
	}
	// Highest priority first, so the repo that wins a package tie leads.
	for i := 1; i < len(out.Repos); i++ {
		if *out.Repos[i-1].Priority < *out.Repos[i].Priority {
			t.Errorf("repos not ordered by descending priority at %d: %d then %d",
				i, *out.Repos[i-1].Priority, *out.Repos[i].Priority)
		}
	}
}

func TestHandleListPackageReposOSFilter(t *testing.T) {
	s, _ := newTestServer(t)
	all := s.getRepos(t, "")

	// A known target filters to a non-empty subset of the full catalog.
	ubuntu := s.getRepos(t, "?os=ubuntu24")
	if len(ubuntu.Repos) == 0 {
		t.Fatal("os=ubuntu24 returned no repos")
	}
	if len(ubuntu.Repos) > len(all.Repos) {
		t.Errorf("filtered list (%d) larger than full catalog (%d)", len(ubuntu.Repos), len(all.Repos))
	}
	inAll := make(map[string]struct{}, len(all.Repos))
	for _, r := range all.Repos {
		inAll[r.Id] = struct{}{}
	}
	for _, r := range ubuntu.Repos {
		if _, ok := inAll[r.Id]; !ok {
			t.Errorf("filtered repo %q not present in the unfiltered catalog", r.Id)
		}
	}
	// Exactly one base repo is enabled by default per target — the OS's own.
	base := 0
	for _, r := range ubuntu.Repos {
		if r.EnabledByDefault {
			base++
		}
	}
	if base != 1 {
		t.Errorf("os=ubuntu24 has %d enabledByDefault repos, want exactly 1", base)
	}

	// A different target yields a different set (repos are OS-scoped).
	debian := s.getRepos(t, "?os=debian13")
	if len(debian.Repos) == 0 {
		t.Fatal("os=debian13 returned no repos")
	}
	if debian.Repos[0].Id == ubuntu.Repos[0].Id && len(debian.Repos) == len(ubuntu.Repos) {
		t.Errorf("debian13 and ubuntu24 returned the same repos; OS filter not applied")
	}

	// An unknown target is not an error: it just has nothing to offer, and the
	// empty list must serialize as [] rather than null so the UI can map over it.
	unknown := s.getRepos(t, "?os=no-such-os")
	if len(unknown.Repos) != 0 {
		t.Errorf("unknown os returned %d repos, want 0", len(unknown.Repos))
	}
	if unknown.Repos == nil {
		t.Error("empty repo list serialized as null, want []")
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

// TestHandleStartBuildAccepted starts a real build against the fake ICT binary,
// which exits 0 immediately, so the handler returns 202 with the accepted
// envelope. The status is the build's real state at the instant the response is
// snapshotted: StartBuild launches the build goroutine and then reads the
// status, so under load (few CPUs, e.g. GOMAXPROCS=1) the fast-exiting fake can
// race all the way to success before that read. Accept any of the states this
// happy path can legitimately be in — not-started, running, or success — rather
// than pinning an early one and flaking when the goroutine wins the race.
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
	if acc.BuildId == "" || !strings.HasPrefix(acc.LogsUrl, "/api/v1/builds/") ||
		!strings.HasSuffix(acc.LogsUrl, "/logs") {
		t.Errorf("unexpected accepted envelope: %+v", acc)
	}
	switch acc.Status {
	case httpapi.BuildStatus(service.StatusNotStarted),
		httpapi.BuildStatus(service.StatusRunning),
		httpapi.BuildStatus(service.StatusSuccess):
		// expected for a fast, successful build
	default:
		t.Errorf("status = %q, want not-started, running, or success", acc.Status)
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
