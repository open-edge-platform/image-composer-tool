// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"sigs.k8s.io/yaml"
)

//go:embed data/manifest.yaml
var manifestFS embed.FS

// Combination maps one UI selection to a pre-authored template file.
type Combination struct {
	Vertical  string `json:"vertical"`
	SKU       string `json:"sku,omitempty"`
	Platform  string `json:"platform"`
	OS        string `json:"os"`
	ImageType string `json:"imageType"`
	Template  string `json:"template"`
}

// Option is a generic {id, displayName} pair used for dropdown labels.
type Option struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

// Target is an OS option with its family and architecture.
type Target struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
}

// Manifest is the full configuration payload served to the UI.
type Manifest struct {
	Combinations []Combination `json:"combinations"`
	Verticals    []Option      `json:"verticals"`
	SKUs         []Option      `json:"skus"`
	Platforms    []Option      `json:"platforms"`
	Targets      []Target      `json:"targets"`
}

// Manifest returns the loaded configuration manifest.
func (s *Service) Manifest() *Manifest { return s.manifest }

// loadManifest parses the manifest. When path is non-empty it reads that file
// from disk (live-editable, no rebuild needed); otherwise it uses the copy
// embedded at build time (the single-binary default).
func loadManifest(path string) (*Manifest, error) {
	var raw []byte
	var err error
	if path != "" {
		raw, err = os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading manifest %q: %w", path, err)
		}
		logger.Logger().Infof("loaded manifest from file: %s", path)
	} else {
		raw, err = manifestFS.ReadFile("data/manifest.yaml")
		if err != nil {
			return nil, fmt.Errorf("reading embedded manifest: %w", err)
		}
	}
	var m Manifest
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}
	return &m, nil
}

// findTemplate returns the template filename for a given selection, or "" if no
// matching combination exists. When sku is empty it acts as a wildcard, but only
// resolves if the match is unambiguous — multiple combinations differing only by
// SKU return "" rather than an order-dependent guess.
func (m *Manifest) findTemplate(vertical, sku, platform, os, imageType string) string {
	var match string
	found := 0
	for _, c := range m.Combinations {
		if c.Vertical == vertical && c.Platform == platform &&
			c.OS == os && c.ImageType == imageType &&
			(sku == "" || c.SKU == sku) {
			if sku != "" {
				return c.Template // exact SKU match is always unambiguous
			}
			match = c.Template
			found++
		}
	}
	if found == 1 {
		return match
	}
	return "" // no match, or ambiguous (multiple SKUs) with no SKU specified
}

// safeTemplatePath resolves a manifest-provided template name to an absolute
// path within templatesDir, rejecting absolute paths and "../" traversal. The
// manifest can be operator-supplied (via --manifest), so a bad or malicious
// entry must not be able to read files outside the templates directory.
func safeTemplatePath(templatesDir, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("empty template name")
	}
	if filepath.IsAbs(name) {
		return "", fmt.Errorf("template path must be relative: %q", name)
	}
	base, err := filepath.Abs(templatesDir)
	if err != nil {
		return "", fmt.Errorf("resolving templates dir: %w", err)
	}
	full := filepath.Join(base, name)
	// full must be base itself (unlikely) or strictly under base/.
	if full != base && !strings.HasPrefix(full, base+string(os.PathSeparator)) {
		return "", fmt.Errorf("template path escapes templates directory: %q", name)
	}
	return full, nil
}
