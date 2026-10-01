// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBuildDetailsReportsTemplatePath covers the field the UI lists the template
// with in the artifacts table.
func TestBuildDetailsReportsTemplatePath(t *testing.T) {
	s := newTestService(t)
	b := &build{
		ID:           "tp1",
		Template:     "robotics.yml",
		TemplatePath: "/tmp/templates/robotics.yml",
		done:         make(chan struct{}),
	}
	b.finish(StatusSuccess, nil, "")
	s.tracker.add(b)

	d, err := s.BuildDetails("tp1")
	if err != nil {
		t.Fatalf("BuildDetails: %v", err)
	}
	if d.TemplatePath != "/tmp/templates/robotics.yml" {
		t.Errorf("templatePath = %q, want %q", d.TemplatePath, "/tmp/templates/robotics.yml")
	}
}

// TestTemplatePathSurvivesMetaRoundTrip is the regression guard for the template
// download: meta.json did not record the path, so every build read back from
// disk — which is every build after a server restart — resolved no template and
// 404'd on download.
func TestTemplatePathSurvivesMetaRoundTrip(t *testing.T) {
	root := t.TempDir()
	templatePath := filepath.Join(root, "template.yml")
	if err := os.WriteFile(templatePath, []byte("image:\n  name: x\n"), 0o600); err != nil {
		t.Fatalf("write template: %v", err)
	}

	b := &build{
		ID:           "tp2",
		RootDir:      root,
		Template:     "robotics.yml",
		TemplatePath: templatePath,
		done:         make(chan struct{}),
	}
	b.finish(StatusSuccess, nil, "")
	if err := b.writeMeta(); err != nil {
		t.Fatalf("writeMeta: %v", err)
	}

	s := newTestService(t)
	reloaded, ok := loadMetaInto(t, s, root, "tp2")
	if !ok {
		t.Fatal("build could not be reconstructed from meta.json")
	}
	if reloaded.TemplatePath != templatePath {
		t.Errorf("reconstructed templatePath = %q, want %q", reloaded.TemplatePath, templatePath)
	}

	// The download handler resolves through TemplateFile, so check the path it
	// would actually serve rather than only the recorded field.
	h := &BuildHandle{b: reloaded}
	gotPath, gotName, err := h.TemplateFile()
	if err != nil {
		t.Fatalf("TemplateFile after reload: %v", err)
	}
	if gotPath != templatePath || gotName != "robotics.yml" {
		t.Errorf("TemplateFile = (%q, %q), want (%q, %q)", gotPath, gotName, templatePath, "robotics.yml")
	}
}

// TestTemplatePathFallsBackToArchivedCopy covers a meta.json written before the
// field existed: an override build's archived template.yml sits beside it and is
// the file that build ran against, so it is used rather than reporting nothing.
func TestTemplatePathFallsBackToArchivedCopy(t *testing.T) {
	root := t.TempDir()
	archived := filepath.Join(root, "template.yml")
	if err := os.WriteFile(archived, []byte("image:\n  name: x\n"), 0o600); err != nil {
		t.Fatalf("write archived template: %v", err)
	}

	// A legacy record: no templatePath key at all.
	got := templatePathFromMeta(root, buildMeta{ID: "tp3", Template: "robotics.yml"})
	if got != archived {
		t.Errorf("templatePathFromMeta = %q, want the archived copy %q", got, archived)
	}
}

// TestTemplatePathEmptyWithoutArchive keeps the honest answer for a legacy
// curated-template build: nothing was archived, so nothing is claimed.
func TestTemplatePathEmptyWithoutArchive(t *testing.T) {
	got := templatePathFromMeta(t.TempDir(), buildMeta{ID: "tp4", Template: "robotics.yml"})
	if got != "" {
		t.Errorf("templatePathFromMeta = %q, want empty", got)
	}
}

// loadMetaInto reconstructs a build from the meta.json under root, using the
// service's own lookup path so the test exercises what the handlers do.
func loadMetaInto(t *testing.T, s *Service, root, id string) (*build, bool) {
	t.Helper()
	dest := filepath.Join(s.buildsRoot(), id)
	if err := os.MkdirAll(dest, 0o750); err != nil {
		t.Fatalf("mkdir builds root: %v", err)
	}
	data, err := os.ReadFile(metaPath(root))
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}
	if err := os.WriteFile(metaPath(dest), data, 0o600); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	return s.getBuild(id)
}
