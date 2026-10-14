// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// materializeBase writes a caller-supplied document into TemplatesDir so the
// rest of the compose pipeline can treat it as the parent template — the same
// role a manifest-matched curated template plays.
//
// It has to live in TemplatesDir rather than a temp directory for the reason
// writeDelta documents: the extends containment guard
// (resolveExtendsParentPath) requires a parent to resolve at or below the
// child's own directory, and the generated delta is written at TemplatesDir's
// root. The name is server-chosen and unique per request, so two concurrent
// composes of different documents cannot collide.
//
// The returned cleanup removes the file; callers must invoke it once the
// compose or build no longer needs the parent on disk.
func (s *Service) materializeBase(sel Selection) (name, path string, cleanup func(), err error) {
	doc := []byte(sel.BaseYAML)
	if len(sel.RemovePackages) > 0 {
		doc, err = withoutPackages(doc, sel.RemovePackages)
		if err != nil {
			return "", "", nil, err
		}
	}
	base, absErr := filepath.Abs(s.cfg.TemplatesDir)
	if absErr != nil {
		return "", "", nil, fmt.Errorf("resolving templates dir: %w", absErr)
	}
	name = ".ict-base-" + uuid.NewString() + ".yml"
	full := filepath.Join(base, name)
	if werr := os.WriteFile(full, doc, 0o600); werr != nil {
		return "", "", nil, fmt.Errorf("writing base template: %w", werr)
	}
	return name, full, func() { _ = os.Remove(full) }, nil
}

// withoutPackages removes entries from a document's systemConfig.packages.
//
// Deletion is the one edit a delta cannot express: the extends merge unions
// package lists and can never subtract, so dropping a package the imported
// document declares has to happen in the document itself. Everything the user
// *adds* still goes through the normal delta, so this stays the narrowest
// possible edit.
//
// Done against the parsed node tree rather than by unmarshalling into a map and
// re-marshalling, so the rest of the document — comments, key order, anchors —
// survives an edit that only concerns one sequence.
func withoutPackages(doc []byte, remove []string) ([]byte, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(doc, &root); err != nil {
		return nil, fmt.Errorf("parsing template: %w", err)
	}
	if len(root.Content) == 0 {
		return doc, nil
	}
	packages := mappingValue(mappingValue(root.Content[0], "systemConfig"), "packages")
	// An alias (`packages: *base`) is a list the browser expands and offers for
	// removal, but editing the anchor it points at would silently change every
	// other use of it. Rejected rather than ignored: returning the document
	// unchanged would build the package the user asked to remove.
	if packages != nil && packages.Kind == yaml.AliasNode {
		return nil, fmt.Errorf("systemConfig.packages is a YAML alias; " +
			"removing a package from it would change every other use of that anchor")
	}
	if packages == nil || packages.Kind != yaml.SequenceNode {
		return doc, nil
	}

	drop := make(map[string]bool, len(remove))
	for _, r := range remove {
		drop[r] = true
	}
	kept := make([]*yaml.Node, 0, len(packages.Content))
	for _, entry := range packages.Content {
		// An aliased entry resolves to a scalar elsewhere in the document, so
		// its own Value is empty and it can never match a name to drop. Kept as
		// it is, which is what the user sees listed as "already included".
		if entry.Kind == yaml.ScalarNode && drop[entry.Value] {
			continue
		}
		kept = append(kept, entry)
	}
	packages.Content = kept

	out, err := yaml.Marshal(&root)
	if err != nil {
		return nil, fmt.Errorf("re-rendering template: %w", err)
	}
	return out, nil
}

// mappingValue returns the value node for key in a mapping node, or nil when
// the node is not a mapping or has no such key.
func mappingValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	// Mapping content alternates key, value.
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}
