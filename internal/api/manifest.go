// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"embed"
	"fmt"
	"os"

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
// matching combination exists. SKU is matched only when provided in the request.
func (m *Manifest) findTemplate(vertical, sku, platform, os, imageType string) string {
	for _, c := range m.Combinations {
		if c.Vertical == vertical && c.Platform == platform &&
			c.OS == os && c.ImageType == imageType &&
			(sku == "" || c.SKU == sku) {
			return c.Template
		}
	}
	return ""
}
