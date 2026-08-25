// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"gopkg.in/yaml.v3"
)

func TestValidateImageName(t *testing.T) {
	cases := []struct {
		name string
		want bool // true = valid
	}{
		{"", true}, // empty means "not overridden"
		{"my-custom-name", true},
		{"MyImage123", true},
		{"a", true},
		{"-leading-dash", false},
		{"trailing-dash-", false},
		{"has space", false},
		{"has/slash", false},
		{"../evil", false},
		{"has.dot", false},
		{strings.Repeat("a", 65), false}, // over maxImageNameLen
		{strings.Repeat("a", 64), true},  // exactly at the cap
		{"café", false},                  // non-ASCII rejected by the pattern
	}
	for i, c := range cases {
		t.Run(fmt.Sprintf("%d_%q", i, c.name), func(t *testing.T) {
			err := ValidateImageName(c.name)
			if c.want && err != nil {
				t.Errorf("ValidateImageName(%q) = %v, want valid", c.name, err)
			}
			if !c.want && err == nil {
				t.Errorf("ValidateImageName(%q) = nil, want an error", c.name)
			}
		})
	}
}

func TestBuildDeltaDeterministicAndValid(t *testing.T) {
	parentImage := config.ImageInfo{Name: "robotics-jazzy-ubuntu24", Version: "24.04"}
	parentTarget := config.TargetInfo{OS: "ubuntu", Dist: "ubuntu24", Arch: "x86_64", ImageType: "raw"}
	sel := Selection{ImageName: "my-custom-name"}

	data1, err := buildDelta("robotics.yml", parentImage, parentTarget, sel)
	if err != nil {
		t.Fatalf("buildDelta: %v", err)
	}
	data2, err := buildDelta("robotics.yml", parentImage, parentTarget, sel)
	if err != nil {
		t.Fatalf("buildDelta (second call): %v", err)
	}
	if string(data1) != string(data2) {
		t.Fatalf("buildDelta is not deterministic:\n--- first ---\n%s\n--- second ---\n%s", data1, data2)
	}

	var d deltaTemplate
	if err := yaml.Unmarshal(data1, &d); err != nil {
		t.Fatalf("generated delta does not parse: %v", err)
	}
	if d.Extends != "robotics.yml" {
		t.Errorf("extends = %q, want robotics.yml", d.Extends)
	}
	if d.Image.Name != "my-custom-name" {
		t.Errorf("image.name = %q, want my-custom-name", d.Image.Name)
	}
	if d.Image.Version != "24.04" {
		t.Errorf("image.version = %q, want the parent's 24.04", d.Image.Version)
	}
	if d.Target != parentTarget {
		t.Errorf("target = %+v, want the parent's target verbatim %+v", d.Target, parentTarget)
	}
}

// Without an override, the delta's image.name falls back to the parent's own
// name rather than emitting an empty override — image.name is schema-required
// on every template, so this must never produce an unresolvable name.
func TestBuildDeltaNoOverrideKeepsParentName(t *testing.T) {
	parentImage := config.ImageInfo{Name: "robotics-jazzy-ubuntu24", Version: "24.04"}
	parentTarget := config.TargetInfo{OS: "ubuntu", Dist: "ubuntu24", Arch: "x86_64", ImageType: "raw"}

	data, err := buildDelta("robotics.yml", parentImage, parentTarget, Selection{})
	if err != nil {
		t.Fatalf("buildDelta: %v", err)
	}
	var d deltaTemplate
	if err := yaml.Unmarshal(data, &d); err != nil {
		t.Fatalf("generated delta does not parse: %v", err)
	}
	if d.Image.Name != parentImage.Name {
		t.Errorf("image.name = %q, want parent's name %q", d.Image.Name, parentImage.Name)
	}
}

func TestWriteDeltaWritesIntoTemplatesDirAndCleansUp(t *testing.T) {
	dir := t.TempDir()
	s := &Service{cfg: Config{TemplatesDir: dir}}

	path, cleanup, err := s.writeDelta([]byte("extends: x.yml\n"))
	if err != nil {
		t.Fatalf("writeDelta: %v", err)
	}
	if filepath.Dir(path) != dir {
		t.Fatalf("delta written to %q, want directly under TemplatesDir %q", path, dir)
	}
	if !strings.HasPrefix(filepath.Base(path), ".ict-adv-") {
		t.Errorf("delta filename %q is not server-generated (missing .ict-adv- prefix)", filepath.Base(path))
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("delta file not written: %v", err)
	}

	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cleanup did not remove the delta file: err=%v", err)
	}
}

// --- Compose() with the imageName override, exercising the full path used by
// the Review step ---

func TestComposeWithImageNameOverride(t *testing.T) {
	s := newTestService(t)

	res, err := s.Compose(Selection{
		Vertical: "robotics", SKU: "amr", Platform: "wcl", OS: "ubuntu24", ImageType: "iso",
		ImageName: "my-custom-name",
	})
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if res.Summary.ImageName != "my-custom-name" {
		t.Errorf("summary.ImageName = %q, want my-custom-name", res.Summary.ImageName)
	}
	if !strings.Contains(res.YAML, "my-custom-name") {
		t.Errorf("resolved YAML does not reflect the override:\n%s", res.YAML)
	}
	// The curated parent's own name must never appear as the resolved name —
	// i.e. the override actually took effect rather than being ignored.
	if strings.Contains(res.YAML, "name: test-image\n") {
		t.Errorf("resolved YAML still carries the parent's unoverridden name:\n%s", res.YAML)
	}

	assertNoLeakedDeltas(t, s.cfg.TemplatesDir)
}

func TestComposeInvalidImageNameOverride(t *testing.T) {
	s := newTestService(t)

	_, err := s.Compose(Selection{
		Vertical: "robotics", SKU: "amr", Platform: "wcl", OS: "ubuntu24", ImageType: "iso",
		ImageName: "-leading-dash",
	})
	code, _ := assertServiceError(t, err, http.StatusBadRequest)
	if code != "BAD_REQUEST" {
		t.Errorf("code = %q, want BAD_REQUEST", code)
	}

	assertNoLeakedDeltas(t, s.cfg.TemplatesDir)
}

// The load-bearing equivalence check from the design: with no override,
// Compose's YAML must be exactly what resolving the curated template directly
// (the same functions `resolve --full` uses) would produce.
func TestComposeNoOverrideMatchesDirectResolve(t *testing.T) {
	s := newTestService(t)

	res, err := s.Compose(Selection{
		Vertical: "robotics", SKU: "amr", Platform: "wcl", OS: "ubuntu24", ImageType: "iso",
	})
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}

	curatedPath := filepath.Join(s.cfg.TemplatesDir, "robotics.yml")
	merged, err := config.LoadAndMergeTemplate(curatedPath)
	if err != nil {
		t.Fatalf("LoadAndMergeTemplate: %v", err)
	}
	want, err := config.MarshalTemplateYAML(config.RedactSensitiveData(merged))
	if err != nil {
		t.Fatalf("MarshalTemplateYAML: %v", err)
	}

	if res.YAML != string(want) {
		t.Fatalf("compose YAML diverges from direct resolve --full equivalent:\n--- compose ---\n%s\n--- direct ---\n%s", res.YAML, want)
	}

	assertNoLeakedDeltas(t, s.cfg.TemplatesDir)
}

// --- resolveBuildTemplate with an override ---

func TestResolveBuildTemplateWithImageNameOverride(t *testing.T) {
	s := newTestService(t)
	wd := t.TempDir()

	path, name, err := s.resolveBuildTemplate(&BuildRequest{Compose: &Selection{
		Vertical: "robotics", SKU: "amr", Platform: "wcl", OS: "ubuntu24", ImageType: "iso",
		ImageName: "my-custom-name",
	}}, wd)
	if err != nil {
		t.Fatalf("resolveBuildTemplate: %v", err)
	}
	// Display name stays the curated parent's — history/build-details show what
	// the user chose, not the generated delta's server-side filename.
	if name != "robotics.yml" {
		t.Errorf("name = %q, want robotics.yml (display name unaffected by the override)", name)
	}
	// The build must run the generated delta, not the curated file directly.
	if filepath.Dir(path) != s.cfg.TemplatesDir || filepath.Base(path) == "robotics.yml" {
		t.Errorf("path = %q, want a generated delta under TemplatesDir", path)
	}
	defer os.Remove(path)

	merged, err := config.LoadAndMergeTemplate(path)
	if err != nil {
		t.Fatalf("LoadAndMergeTemplate(delta): %v", err)
	}
	if merged.Image.Name != "my-custom-name" {
		t.Errorf("merged image.name = %q, want my-custom-name", merged.Image.Name)
	}
}

func TestResolveBuildTemplateInvalidImageNameOverride(t *testing.T) {
	s := newTestService(t)
	wd := t.TempDir()

	_, _, err := s.resolveBuildTemplate(&BuildRequest{Compose: &Selection{
		Vertical: "robotics", SKU: "amr", Platform: "wcl", OS: "ubuntu24", ImageType: "iso",
		ImageName: "-leading-dash",
	}}, wd)
	if err == nil {
		t.Fatal("expected an error for an invalid image name override")
	}

	assertNoLeakedDeltas(t, s.cfg.TemplatesDir)
}

// --- archiveAndCleanupDelta, exercised directly the way TestBuildArtifacts
// exercises finish() — no real exec.Command involved ---

func TestArchiveAndCleanupDeltaArchivesAndRemovesDelta(t *testing.T) {
	s := newTestService(t)
	curatedPath := filepath.Join(s.cfg.TemplatesDir, "robotics.yml")
	parent, err := config.LoadTemplate(curatedPath, false)
	if err != nil {
		t.Fatalf("LoadTemplate(parent): %v", err)
	}
	data, err := buildDelta("robotics.yml", parent.Image, parent.Target, Selection{ImageName: "my-custom-name"})
	if err != nil {
		t.Fatalf("buildDelta: %v", err)
	}
	deltaPath, deltaCleanup, err := s.writeDelta(data)
	if err != nil {
		t.Fatalf("writeDelta: %v", err)
	}
	t.Cleanup(deltaCleanup) // no-op if archiveAndCleanupDelta already removed it

	root := t.TempDir()
	b := &build{ID: "b1", RootDir: root, DeltaPath: deltaPath, done: make(chan struct{})}
	b.archiveAndCleanupDelta()

	if _, err := os.Stat(deltaPath); !os.IsNotExist(err) {
		t.Fatalf("delta file %q was not removed: err=%v", deltaPath, err)
	}
	archivePath := filepath.Join(root, "template.yml")
	archived, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("archived template not written: %v", err)
	}
	if !strings.Contains(string(archived), "my-custom-name") {
		t.Errorf("archived template does not reflect the override:\n%s", archived)
	}
	if b.TemplatePath != archivePath {
		t.Errorf("TemplatePath = %q, want repointed to the archived copy %q", b.TemplatePath, archivePath)
	}
}

// assertNoLeakedDeltas fails the test if any generated delta file remains in
// dir — the invariant every override path must uphold regardless of success
// or failure.
func assertNoLeakedDeltas(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".ict-adv-*.yml"))
	if err != nil {
		t.Fatalf("glob for leaked deltas: %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("leaked delta file(s) in TemplatesDir: %v", matches)
	}
}
