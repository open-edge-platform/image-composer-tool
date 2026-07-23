// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// testManifest returns a small in-memory manifest for service tests.
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

// newTestService builds a Service with the test manifest and a templates dir
// containing a schema-valid template file for each combination.
func newTestService(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"robotics.yml", "retail.yml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(minimalTemplate), 0o644); err != nil {
			t.Fatalf("writing template %s: %v", name, err)
		}
	}
	return &Service{
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

// --- compose ---

func TestComposeSuccess(t *testing.T) {
	s := newTestService(t)
	res, err := s.Compose(Selection{
		Vertical: "robotics", SKU: "amr", Platform: "wcl", OS: "ubuntu24", ImageType: "iso",
	})
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if res.Template != "robotics.yml" || res.YAML == "" {
		t.Errorf("unexpected compose result: %+v", res)
	}
	if res.Summary.Vertical != "robotics" {
		t.Errorf("summary echo wrong: %+v", res.Summary)
	}
}

func TestComposeErrors(t *testing.T) {
	s := newTestService(t)
	cases := []struct {
		name string
		sel  Selection
		want int
	}{
		{"missing fields", Selection{Vertical: "robotics"}, 400},
		{"no match", Selection{Vertical: "robotics", Platform: "ptl", OS: "ubuntu24", ImageType: "iso"}, 400},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := s.Compose(c.sel)
			se, ok := err.(*Error)
			if !ok {
				t.Fatalf("err = %v, want *Error", err)
			}
			if se.Status != c.want {
				t.Errorf("status = %d, want %d", se.Status, c.want)
			}
		})
	}
}

func TestComposeInvalidTemplate(t *testing.T) {
	s := newTestService(t)
	// Overwrite a matched template with schema-invalid content so the load/merge
	// step fails; compose must surface 422, not a misleading success.
	if err := os.WriteFile(filepath.Join(s.cfg.TemplatesDir, "robotics.yml"), []byte("image:\n  name: broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := s.Compose(Selection{Vertical: "robotics", SKU: "amr", Platform: "wcl", OS: "ubuntu24", ImageType: "iso"})
	se, ok := err.(*Error)
	if !ok || se.Status != 422 {
		t.Fatalf("err = %v, want *Error status 422", err)
	}
}

func TestComposeTemplateMissingOnDisk(t *testing.T) {
	s := newTestService(t)
	// Manifest references a template file that isn't on disk.
	s.manifest.Combinations = append(s.manifest.Combinations, Combination{
		Vertical: "ghost", Platform: "wcl", OS: "ubuntu24", ImageType: "raw", Template: "missing.yml",
	})
	_, err := s.Compose(Selection{Vertical: "ghost", Platform: "wcl", OS: "ubuntu24", ImageType: "raw"})
	se, ok := err.(*Error)
	if !ok || se.Status != 500 {
		t.Fatalf("err = %v, want *Error status 500", err)
	}
}

// --- build tracker / state ---

func TestBuildTrackerAddGet(t *testing.T) {
	tr := newBuildTracker()
	b := &build{ID: "b1", status: StatusRunning, done: make(chan struct{})}
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
	b := &build{ID: "b", status: StatusRunning, done: make(chan struct{})}
	if s := b.snapshot(); s.Status != StatusRunning {
		t.Fatalf("initial status = %q", s.Status)
	}
	arts := []Artifact{{Name: "img.raw.gz", Type: "image", Path: "/o/img.raw.gz"}}
	b.finish(StatusSuccess, arts, "")
	s := b.snapshot()
	if s.Status != StatusSuccess || len(s.Artifacts) != 1 || s.Artifacts[0].Name != "img.raw.gz" {
		t.Fatalf("post-finish snapshot = %+v", s)
	}
	// snapshot returns a copy — mutating it must not affect the build.
	s.Artifacts[0].Name = "mutated"
	if b.snapshot().Artifacts[0].Name != "img.raw.gz" {
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
	b := &build{ID: "b", status: StatusRunning, done: make(chan struct{})}
	b.finish(StatusFailed, nil, "boom")
	s := b.snapshot()
	if s.Status != StatusFailed || s.ErrMsg != "boom" {
		t.Fatalf("snapshot = %+v", s)
	}
}

// --- build command / template resolution (no real exec) ---

func TestBuildCommand(t *testing.T) {
	s := &Service{cfg: Config{ICTBinary: "/opt/ict"}}
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
	s := newTestService(t)
	wd := t.TempDir()

	// compose path -> manifest lookup
	path, name, err := s.resolveBuildTemplate(&BuildRequest{Compose: &Selection{
		Vertical: "robotics", SKU: "amr", Platform: "wcl", OS: "ubuntu24", ImageType: "iso",
	}}, wd)
	if err != nil || name != "robotics.yml" || !strings.HasSuffix(path, "robotics.yml") {
		t.Fatalf("compose resolve = %q %q %v", path, name, err)
	}

	// yaml path -> writes template.yml into the work dir
	path, name, err = s.resolveBuildTemplate(&BuildRequest{YAML: "image:\n  name: x\n"}, wd)
	if err != nil || name != "template.yml" {
		t.Fatalf("yaml resolve = %q %q %v", path, name, err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("yaml template not written: %v", statErr)
	}

	// neither -> error
	if _, _, err := s.resolveBuildTemplate(&BuildRequest{}, wd); err == nil {
		t.Fatal("expected error when neither compose nor yaml provided")
	}
	// no match -> error
	if _, _, err := s.resolveBuildTemplate(&BuildRequest{Compose: &Selection{
		Vertical: "robotics", Platform: "ptl", OS: "ubuntu24", ImageType: "iso",
	}}, wd); err == nil {
		t.Fatal("expected error for unmatched combination")
	}
}

// --- artifacts query ---

func TestBuildArtifacts(t *testing.T) {
	s := newTestService(t)
	b := &build{ID: "b1", done: make(chan struct{})}
	b.finish(StatusSuccess, []Artifact{{Name: "img", Type: "image", Path: "/o/img"}}, "")
	s.tracker.add(b)

	l, err := s.BuildArtifacts("b1")
	if err != nil {
		t.Fatalf("BuildArtifacts: %v", err)
	}
	if l.Status != StatusSuccess || len(l.Artifacts) != 1 {
		t.Errorf("artifacts result = %+v", l)
	}

	// missing build -> 404 *Error
	_, err = s.BuildArtifacts("nope")
	if se, ok := err.(*Error); !ok || se.Status != 404 {
		t.Errorf("missing build err = %v, want 404 *Error", err)
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

// --- build details ---

func TestBuildDetails(t *testing.T) {
	s := newTestService(t)
	b := &build{
		ID:           "d1",
		WorkDir:      "/tmp/work",
		CacheDir:     "/tmp/cache",
		Template:     "robotics.yml",
		TemplatePath: "/tmp/robotics.yml",
		Command:      "sudo -n ict build robotics.yml",
		done:         make(chan struct{}),
	}
	b.finish(StatusSuccess, nil, "")
	s.tracker.add(b)

	d, err := s.BuildDetails("d1")
	if err != nil {
		t.Fatalf("BuildDetails: %v", err)
	}
	if d.Command != b.Command || d.WorkDir != b.WorkDir {
		t.Errorf("details mismatch: %+v", d)
	}
	if d.TemplateURL != "/api/v1/builds/d1/template" {
		t.Errorf("templateURL = %q", d.TemplateURL)
	}

	// missing build -> 404 *Error
	if _, err := s.BuildDetails("nope"); err == nil {
		t.Error("missing build = nil error, want 404")
	}
}

// --- artifact opening (path-guard + download) ---

func TestOpenArtifact(t *testing.T) {
	s := newTestService(t)
	workDir := t.TempDir()
	artifactFile := filepath.Join(workDir, "image.iso")
	if err := os.WriteFile(artifactFile, []byte("fake-iso-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := &build{ID: "a1", RootDir: workDir, WorkDir: workDir, done: make(chan struct{})}
	b.finish(StatusSuccess, []Artifact{{Name: "image.iso", Type: "image", Path: artifactFile}}, "")
	s.tracker.add(b)

	rc, name, err := s.OpenArtifact(t.Context(), "a1", "image.iso")
	if err != nil {
		t.Fatalf("OpenArtifact: %v", err)
	}
	defer rc.Close()
	if name != "image.iso" {
		t.Errorf("filename = %q, want image.iso", name)
	}
	data, _ := io.ReadAll(rc)
	if string(data) != "fake-iso-content" {
		t.Errorf("content = %q", data)
	}

	// unknown artifact -> 404
	if _, _, err := s.OpenArtifact(t.Context(), "a1", "nope.iso"); err == nil {
		t.Error("unknown artifact = nil error, want 404")
	}
	// missing build -> 404
	if _, _, err := s.OpenArtifact(t.Context(), "nope", "image.iso"); err == nil {
		t.Error("missing build = nil error, want 404")
	}
}

// TestOpenArtifactRelativeWorkDir guards the regression where a relative WorkDir
// (the default `webui-workspace`) made the absolute-vs-relative prefix check
// always fail, wrongly rejecting a legitimate artifact.
func TestOpenArtifactRelativeWorkDir(t *testing.T) {
	s := newTestService(t)
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
	b.finish(StatusSuccess, []Artifact{{Name: "image.iso", Type: "image", Path: artifactFile}}, "")
	s.tracker.add(b)

	rc, _, err := s.OpenArtifact(t.Context(), "rel1", "image.iso")
	if err != nil {
		t.Fatalf("OpenArtifact: %v", err)
	}
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if string(data) != "iso-bytes" {
		t.Errorf("content = %q, want iso-bytes", data)
	}
}

// TestOpenArtifactPathEscape rejects an artifact whose recorded path resolves
// outside the build's work directory (a poisoned artifact entry).
func TestOpenArtifactPathEscape(t *testing.T) {
	s := newTestService(t)
	workDir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "escape.iso")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := &build{ID: "esc", RootDir: workDir, WorkDir: workDir, done: make(chan struct{})}
	b.finish(StatusSuccess, []Artifact{{Name: "escape.iso", Type: "image", Path: outside}}, "")
	s.tracker.add(b)

	if _, _, err := s.OpenArtifact(t.Context(), "esc", "escape.iso"); err == nil {
		t.Fatal("path-escape artifact = nil error, want 403")
	} else if se, ok := err.(*Error); !ok || se.Status != 403 {
		t.Errorf("err = %v, want 403 *Error", err)
	}
}

// --- template file lookup ---

func TestTemplateFile(t *testing.T) {
	s := newTestService(t)
	tmpFile := filepath.Join(t.TempDir(), "robotics.yml")
	if err := os.WriteFile(tmpFile, []byte(minimalTemplate), 0o644); err != nil {
		t.Fatal(err)
	}
	b := &build{ID: "t1", Template: "robotics.yml", TemplatePath: tmpFile, done: make(chan struct{})}
	close(b.done)
	s.tracker.add(b)

	h, ok := s.Build("t1")
	if !ok {
		t.Fatal("Build not found")
	}
	path, name, err := h.TemplateFile()
	if err != nil || path != tmpFile || name != "robotics.yml" {
		t.Fatalf("TemplateFile = %q %q %v", path, name, err)
	}

	// build with no recorded template -> 404
	b2 := &build{ID: "t2", done: make(chan struct{})}
	close(b2.done)
	s.tracker.add(b2)
	h2, _ := s.Build("t2")
	if _, _, err := h2.TemplateFile(); err == nil {
		t.Error("no-template build = nil error, want 404")
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

// --- compose history (list + disk hydration) ---

func TestHistoryListAndHydrate(t *testing.T) {
	s := newTestService(t)

	// A live in-memory build (running).
	live := &build{
		ID:        "live1",
		RootDir:   filepath.Join(s.cfg.WorkDir, "builds", "live1"),
		Template:  "robotics.yml",
		Summary:   &ComposeSummary{Vertical: "robotics"},
		CreatedAt: time.Now(),
		done:      make(chan struct{}),
	}
	live.status = StatusRunning
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
		Summary:   &ComposeSummary{Vertical: "retail"},
		Artifacts: []Artifact{{Name: "img.iso", Type: "image", Path: filepath.Join(pastRoot, "img.iso")}},
	}
	data, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(metaPath(pastRoot), data, 0o600); err != nil {
		t.Fatal(err)
	}

	// List returns both, newest-first (live is newer).
	items := s.ListBuilds()
	if len(items) != 2 {
		t.Fatalf("builds = %d, want 2: %+v", len(items), items)
	}
	if items[0].ID != "live1" || items[1].ID != "past1" {
		t.Errorf("order = %s,%s want live1,past1", items[0].ID, items[1].ID)
	}

	// getBuild hydrates the past build from disk (not in tracker).
	if _, ok := s.tracker.get("past1"); ok {
		t.Fatal("past1 should not be in the tracker")
	}
	hb, ok := s.getBuild("past1")
	if !ok {
		t.Fatal("getBuild past1 = not found, want hydrated from disk")
	}
	if hb.Template != "retail.yml" || hb.snapshot().Status != StatusSuccess {
		t.Errorf("hydrated build wrong: %+v", hb)
	}

	// Unknown build id is not found.
	if _, ok := s.getBuild("does-not-exist"); ok {
		t.Error("getBuild unknown id = found, want not found")
	}
}

func TestNormalizeSize(t *testing.T) {
	cases := map[string]string{
		"0.01 MB": "10 KB",   // small file now shows in KB, not "0.01 MB"
		"1.13 GB": "1.13 GB", // large unchanged
		"512 B":   "512 B",
		"2048 KB": "2.05 MB", // rolls up to MB
		"4 MB":    "4.00 MB",
		"":        "", // unparseable → unchanged
		"weird":   "weird",
	}
	for in, want := range cases {
		if got := normalizeSize(in); got != want {
			t.Errorf("normalizeSize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDetectPhase(t *testing.T) {
	// Empty logs → preparing.
	if got := DetectPhase(nil); got != "preparing" {
		t.Errorf("empty logs phase = %q, want preparing", got)
	}
	// Furthest marker wins regardless of order of earlier lines. Resolve and
	// download collapse into the single "packages" phase.
	logs := []string{
		"2026 INFO Loaded image template from x.yml",
		"2026 INFO downloading 239 packages to /cache using 8 workers",
		"2026 INFO resolving dependencies for 239 DEBIANs",
		"2026 INFO Chroot environment build completed successfully",
		"2026 INFO Image package installation...",
	}
	if got := DetectPhase(logs); got != "installing" {
		t.Errorf("phase = %q, want installing", got)
	}
	// Terminal marker.
	if got := DetectPhase([]string{"2026 INFO image build completed successfully"}); got != "done" {
		t.Errorf("done phase = %q, want done", got)
	}
	// Resolve/download → the merged "packages" phase (RPM wording too).
	if got := DetectPhase([]string{"2026 INFO resolving dependencies for 100 RPMs"}); got != "packages" {
		t.Errorf("rpm resolve phase = %q, want packages", got)
	}
	// Regression: chroot build and the initrd download round that FOLLOWS it are
	// all still the "packages" phase — nothing goes green until install starts.
	seq := []string{
		"2026 INFO Chroot environment build completed successfully",
		"2026 INFO Building image: debian13-x86_64-desktop-virtualization",
		"2026 INFO downloading 239 packages to /cache using 8 workers",
		"2026 INFO resolving dependencies for 54 DEBIANs",
		"2026 INFO all downloads complete",
	}
	if got := DetectPhase(seq); got != "packages" {
		t.Errorf("chroot + post-chroot download phase = %q, want packages (not installing)", got)
	}
	// Install marker advances the stepper; ISO assembly marks generating.
	if got := DetectPhase(append(seq, "2026 INFO Image package installation...", "2026 INFO Creating ISO image...")); got != "generating" {
		t.Errorf("iso-assembly phase = %q, want generating", got)
	}
}

func TestInstallProgress(t *testing.T) {
	logs := []string{
		"2026 INFO Installing package 1/128: curl",
		"2026 INFO Installing package 42/128: vim",
	}
	if d, tot := InstallProgress(logs); d != 42 || tot != 128 {
		t.Errorf("installProgress = %d/%d, want 42/128", d, tot)
	}
	if d, tot := InstallProgress([]string{"no counter here"}); d != 0 || tot != 0 {
		t.Errorf("installProgress(none) = %d/%d, want 0/0", d, tot)
	}
}
