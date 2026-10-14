// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// importedBase is a complete document of the shape the Import Template flow
// posts: no manifest combination maps to it, so it can only be composed against
// directly.
const importedBase = `image:
  name: imported-image
  version: "1.0.0"
target:
  os: ubuntu
  dist: ubuntu24
  arch: x86_64
  imageType: raw
systemConfig:
  name: imported
  hostname: imported
  # Kept to prove an edit preserves the rest of the document.
  packages:
    - vim
    - bluez
    - openssh-server
`

// TestComposeAgainstSuppliedDocument is the whole point of baseYaml: an
// imported template reaches the same compose result a matched one does, so the
// Review step, the package rail and the build all work unchanged.
func TestComposeAgainstSuppliedDocument(t *testing.T) {
	s := newTestService(t)

	res, err := s.Compose(Selection{BaseYAML: importedBase})
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	// The scratch file it was materialized to is an implementation detail.
	if res.Template != "imported-image.yml" {
		t.Errorf("template = %q, want the document's own name", res.Template)
	}
	if res.Summary.ImageName != "imported-image" {
		t.Errorf("summary.imageName = %q, want imported-image", res.Summary.ImageName)
	}
	for _, want := range []string{"vim", "bluez", "openssh-server"} {
		if !slices.Contains(res.BasePackages, want) {
			t.Errorf("basePackages = %v, want it to contain %q", res.BasePackages, want)
		}
	}
	if res.YAML == "" {
		t.Error("no resolved YAML returned")
	}
}

// Overrides apply to a supplied document exactly as they do to a matched
// template — through a generated delta that extends it.
func TestComposeAgainstSuppliedDocumentWithOverrides(t *testing.T) {
	s := newTestService(t)

	res, err := s.Compose(Selection{
		BaseYAML:  importedBase,
		ImageName: "renamed-image",
		Packages:  []string{"htop"},
	})
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if res.DeltaYAML == "" {
		t.Fatal("no delta returned for a selection carrying overrides")
	}
	if !strings.Contains(res.DeltaYAML, "htop") {
		t.Errorf("delta is missing the added package:\n%s", res.DeltaYAML)
	}
	if res.Summary.ImageName != "renamed-image" {
		t.Errorf("summary.imageName = %q, want the override applied", res.Summary.ImageName)
	}
	// The union of the document's packages and the added one.
	if !strings.Contains(res.YAML, "vim") || !strings.Contains(res.YAML, "htop") {
		t.Error("resolved YAML should carry both the document's packages and the added one")
	}
}

// Removal is the one edit that cannot ride in a delta, so it has to change the
// document the compose runs against.
func TestComposeRemovesPackagesFromSuppliedDocument(t *testing.T) {
	s := newTestService(t)

	res, err := s.Compose(Selection{BaseYAML: importedBase, RemovePackages: []string{"bluez"}})
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if slices.Contains(res.BasePackages, "bluez") {
		t.Errorf("basePackages = %v, want bluez removed", res.BasePackages)
	}
	for _, want := range []string{"vim", "openssh-server"} {
		if !slices.Contains(res.BasePackages, want) {
			t.Errorf("basePackages = %v, want %q kept", res.BasePackages, want)
		}
	}
}

// A compose must not leave its materialized parent behind in TemplatesDir —
// the same guarantee the generated delta carries.
func TestComposeCleansUpTheMaterializedDocument(t *testing.T) {
	s := newTestService(t)

	before := templatesDirEntries(t, s.cfg.TemplatesDir)
	if _, err := s.Compose(Selection{BaseYAML: importedBase, Packages: []string{"htop"}}); err != nil {
		t.Fatalf("Compose: %v", err)
	}
	after := templatesDirEntries(t, s.cfg.TemplatesDir)
	if len(after) != len(before) {
		t.Errorf("templates dir went from %v to %v; a generated file was left behind", before, after)
	}
}

// Without a document the lookup fields are still what identifies a template, so
// their absence must keep failing.
func TestComposeStillRequiresSelectionWithoutDocument(t *testing.T) {
	s := newTestService(t)

	if _, err := s.Compose(Selection{}); err == nil {
		t.Fatal("expected an empty selection with no document to be rejected")
	}
}

func TestWithoutPackages(t *testing.T) {
	t.Parallel()

	t.Run("preserves comments and unrelated keys", func(t *testing.T) {
		t.Parallel()
		out, err := withoutPackages([]byte(importedBase), []string{"bluez"})
		if err != nil {
			t.Fatalf("withoutPackages: %v", err)
		}
		got := string(out)
		if strings.Contains(got, "bluez") {
			t.Errorf("bluez survived the edit:\n%s", got)
		}
		if !strings.Contains(got, "Kept to prove an edit preserves the rest of the document") {
			t.Errorf("the comment was dropped:\n%s", got)
		}
		for _, want := range []string{"vim", "openssh-server", "hostname: imported", "arch: x86_64"} {
			if !strings.Contains(got, want) {
				t.Errorf("edit dropped %q:\n%s", want, got)
			}
		}
	})

	t.Run("a document with no packages block is returned unchanged", func(t *testing.T) {
		t.Parallel()
		doc := []byte("image:\n  name: x\n")
		out, err := withoutPackages(doc, []string{"vim"})
		if err != nil {
			t.Fatalf("withoutPackages: %v", err)
		}
		if string(out) != string(doc) {
			t.Errorf("document was rewritten:\n%s", out)
		}
	})

	t.Run("an unparseable document is an error, not a silent pass-through", func(t *testing.T) {
		t.Parallel()
		if _, err := withoutPackages([]byte("image:\n\tname: tabs\n"), []string{"vim"}); err == nil {
			t.Fatal("expected a parse error")
		}
	})

	// The browser expands an alias and offers each package for removal, so
	// returning the document unchanged would build the package the user
	// removed. Editing the anchor instead would change every other use of it.
	t.Run("an aliased package list is rejected rather than ignored", func(t *testing.T) {
		t.Parallel()
		doc := []byte("base: &base\n  - vim\n  - bluez\nsystemConfig:\n  packages: *base\n")
		if _, err := withoutPackages(doc, []string{"bluez"}); err == nil {
			t.Fatal("expected an aliased package list to be rejected")
		}
	})
}

func templatesDirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading templates dir: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, filepath.Base(e.Name()))
	}
	slices.Sort(names)
	return names
}
