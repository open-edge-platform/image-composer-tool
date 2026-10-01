// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// templateWithMetadata mirrors how the curated templates in image-templates/ are
// authored: a metadata discovery block first, then image/target/…/systemConfig.
const templateWithMetadata = `metadata:
  description: Debian 13 raw image with initrd hello hook
  use_cases:
    - Validating custom dracut module scripts
  keywords:
    - debian
    - raw
image:
  name: bb-os-image-debian-dracut
  version: "13.1"
target:
  os: debian
  dist: debian13
  arch: x86_64
  imageType: raw
packageRepositories:
  - name: debian-main
    url: https://deb.debian.org/debian
disk:
  name: Minimal_Raw
  partitionTableType: gpt
systemConfig:
  name: minimal
  packages:
    - bash
  kernel:
    version: "6.12"
`

// topLevelKeys returns the emitted document's top-level keys in order. A
// yaml.Node is used rather than a map because a map loses the ordering that is
// the whole point of this test.
func topLevelKeys(t *testing.T, data []byte) []string {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parsing emitted YAML: %v", err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		t.Fatalf("emitted YAML is not a mapping:\n%s", data)
	}
	var keys []string
	for i := 0; i < len(doc.Content[0].Content); i += 2 {
		keys = append(keys, doc.Content[0].Content[i].Value)
	}
	return keys
}

// TestTemplateMetadataRoundTrips guards the discovery block the curated
// template opens with: it has no effect on a build, so nothing else would notice
// it being dropped — but a resolved template that silently loses its parent's
// description is not the template the user authored.
func TestTemplateMetadataRoundTrips(t *testing.T) {
	var tpl ImageTemplate
	if err := yaml.Unmarshal([]byte(templateWithMetadata), &tpl); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if tpl.Metadata == nil {
		t.Fatal("metadata block was dropped on unmarshal")
	}
	if got, want := tpl.Metadata.Description, "Debian 13 raw image with initrd hello hook"; got != want {
		t.Errorf("description = %q, want %q", got, want)
	}
	if got, want := len(tpl.Metadata.UseCases), 1; got != want {
		t.Errorf("use_cases length = %d, want %d", got, want)
	}
	if got, want := strings.Join(tpl.Metadata.Keywords, ","), "debian,raw"; got != want {
		t.Errorf("keywords = %q, want %q", got, want)
	}

	data, err := MarshalTemplateYAML(&tpl)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round ImageTemplate
	if err := yaml.Unmarshal(data, &round); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if round.Metadata == nil || round.Metadata.Description != tpl.Metadata.Description {
		t.Errorf("metadata did not survive marshal round-trip: %+v", round.Metadata)
	}
}

// TestTemplateMetadataDoesNotInherit covers the merge contract stated in
// image-templates/COMPOSITION.md: metadata is read per file, so a layer gets
// whatever it declares and nothing when it declares none. MergeConfigurations
// starts from a copy of the parent, so this only holds because the assignment
// is unconditional — a guard would silently reintroduce inheritance.
func TestTemplateMetadataDoesNotInherit(t *testing.T) {
	parent := &ImageTemplate{
		Metadata: &TemplateMetadata{
			Description: "parent description",
			UseCases:    []string{"parent use case"},
			Keywords:    []string{"parent"},
		},
	}

	t.Run("child_without_metadata_emits_none", func(t *testing.T) {
		merged, err := MergeConfigurations(&ImageTemplate{}, parent)
		if err != nil {
			t.Fatalf("merge: %v", err)
		}
		if merged.Metadata != nil {
			t.Errorf("metadata = %+v, want nil — a child that declares none inherits none", merged.Metadata)
		}
	})

	t.Run("child_with_metadata_replaces_parent", func(t *testing.T) {
		child := &ImageTemplate{Metadata: &TemplateMetadata{Description: "child description"}}
		merged, err := MergeConfigurations(child, parent)
		if err != nil {
			t.Fatalf("merge: %v", err)
		}
		if merged.Metadata == nil || merged.Metadata.Description != "child description" {
			t.Fatalf("description = %+v, want the child's", merged.Metadata)
		}
		// Replacement is whole-block, so fields the child omitted are empty
		// rather than filled in from the parent.
		if len(merged.Metadata.UseCases) != 0 || len(merged.Metadata.Keywords) != 0 {
			t.Errorf("use_cases=%v keywords=%v, want both empty — the block is replaced, not merged",
				merged.Metadata.UseCases, merged.Metadata.Keywords)
		}
	})

	t.Run("parent_metadata_survives_when_no_child_layer", func(t *testing.T) {
		// A single template resolved against an OS default (which carries no
		// metadata) keeps its own block: it is the user layer here.
		merged, err := MergeConfigurations(parent, &ImageTemplate{})
		if err != nil {
			t.Fatalf("merge: %v", err)
		}
		if merged.Metadata == nil || merged.Metadata.Description != "parent description" {
			t.Errorf("metadata = %+v, want the declaring template's own", merged.Metadata)
		}
	})
}

// TestTemplateMetadataDoesNotInheritThroughChain is the same contract through
// the real extends fold rather than a single merge, since that is what a
// template with `extends` actually goes through.
func TestTemplateMetadataDoesNotInheritThroughChain(t *testing.T) {
	grandparent := &ImageTemplate{Metadata: &TemplateMetadata{Description: "grandparent"}}
	middle := &ImageTemplate{}
	leaf := &ImageTemplate{}

	merged, err := foldChain(grandparent, []*ImageTemplate{middle, leaf})
	if err != nil {
		t.Fatalf("foldChain: %v", err)
	}
	if merged.Metadata != nil {
		t.Errorf("metadata = %+v, want nil — neither layer above the base declared one", merged.Metadata)
	}

	leafWithOwn := &ImageTemplate{Metadata: &TemplateMetadata{Description: "leaf"}}
	merged, err = foldChain(grandparent, []*ImageTemplate{middle, leafWithOwn})
	if err != nil {
		t.Fatalf("foldChain: %v", err)
	}
	if merged.Metadata == nil || merged.Metadata.Description != "leaf" {
		t.Errorf("metadata = %+v, want the leaf's own", merged.Metadata)
	}
}

// TestTemplateKeyOrderMatchesCuratedTemplates pins the emitted key order to the
// order image-templates/ are hand-authored in, so a generated or resolved
// template reads like one of them. Struct field order is what produces this, so
// reordering ImageTemplate's fields will fail here.
func TestTemplateKeyOrderMatchesCuratedTemplates(t *testing.T) {
	var tpl ImageTemplate
	if err := yaml.Unmarshal([]byte(templateWithMetadata), &tpl); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data, err := MarshalTemplateYAML(&tpl)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	want := []string{"metadata", "image", "target", "packageRepositories", "disk", "systemConfig"}
	got := topLevelKeys(t, data)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("top-level key order = %v, want %v\nemitted:\n%s", got, want, data)
	}
}

// TestCuratedTemplatesShareKeyOrder is the other half of the contract: it reads
// the shipped templates and asserts the order above is really how they are
// written, so the expectation cannot drift away from the files it describes.
//
// Only the keys the corpus agrees on are ordered here. `packageRepositories` and
// `overlayPolicy` are deliberately left out: the shipped templates genuinely
// disagree about where those two go (packageRepositories sits before `disk` in 9
// of them and after it — in two cases after `systemConfig` — in 7), so there is
// no order to assert. The emitted position of both is fixed by struct order and
// pinned by TestTemplateKeyOrderMatchesCuratedTemplates instead.
func TestCuratedTemplatesShareKeyOrder(t *testing.T) {
	// Canonical position of each ordered top-level key. A template uses a subset
	// of these, but never out of this relative order.
	position := map[string]int{
		"metadata": 0, "extends": 1, "image": 2, "target": 3,
		"baseline": 4, "disk": 5, "systemConfig": 6,
	}
	// Known keys whose position varies across the corpus — checked for validity,
	// skipped for ordering.
	unordered := map[string]bool{"packageRepositories": true, "overlayPolicy": true}

	roots := []string{
		filepath.Join("..", "..", "image-templates"),
	}
	var checked int
	for _, root := range roots {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".yml") {
				return nil //nolint:nilerr // an unreadable template is not this test's concern
			}
			// Skip generated Advanced-mode deltas left in the tree.
			if strings.HasPrefix(filepath.Base(path), ".") {
				return nil
			}
			data, readErr := os.ReadFile(path) //nolint:gosec // test-local, walking a repo dir
			if readErr != nil {
				return nil //nolint:nilerr // unreadable file, not a failure of ordering
			}
			var doc yaml.Node
			if yaml.Unmarshal(data, &doc) != nil || len(doc.Content) == 0 {
				return nil
			}
			if doc.Content[0].Kind != yaml.MappingNode {
				return nil
			}
			checked++
			last, lastKey := -1, ""
			for i := 0; i < len(doc.Content[0].Content); i += 2 {
				key := doc.Content[0].Content[i].Value
				if unordered[key] {
					continue
				}
				pos, known := position[key]
				if !known {
					t.Errorf("%s: unexpected top-level key %q", filepath.Base(path), key)
					continue
				}
				if pos < last {
					t.Errorf("%s: key %q appears after %q, which breaks the canonical order %v",
						filepath.Base(path), key, lastKey, position)
				}
				last, lastKey = pos, key
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", root, err)
		}
	}
	if checked == 0 {
		t.Fatal("no curated templates were checked — the template directory moved?")
	}
	t.Logf("checked %d curated templates", checked)
}
