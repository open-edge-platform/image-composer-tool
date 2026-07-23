// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"net/http"
	"os"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

// Selection is the set of UI choices that identify one combination. The service
// uses it to look up the matching pre-authored template in the manifest.
type Selection struct {
	Vertical  string
	SKU       string
	Platform  string
	OS        string
	ImageType string
}

// ComposeSummary is the human-readable summary shown in the Review panel and
// stored on the build record for the Build Details panel.
type ComposeSummary struct {
	// Selection echo — what the user chose
	Vertical  string
	SKU       string
	Platform  string
	OS        string
	ImageType string

	// Template-derived — info the user can't see from the dropdowns
	ImageName      string
	ImageVersion   string
	Description    string
	Architecture   string
	KernelVersion  string
	PackageCount   int
	DiskSize       string
	PartitionCount int
	PartitionTable string
	Hostname       string

	// Overlay-mode only: the baseline image the packages are layered onto.
	BaseImage string
}

// ComposeResult is the outcome of resolving a Selection to a template.
type ComposeResult struct {
	Template string // resolved template filename
	YAML     string // matched template YAML, verbatim
	Summary  ComposeSummary
}

// Compose resolves the selections to a template and returns its YAML plus a
// summary. It is a lookup — the service never synthesizes a template. Input and
// lookup failures return a *Error carrying the appropriate HTTP status/code.
func (s *Service) Compose(sel Selection) (*ComposeResult, error) {
	if sel.Vertical == "" || sel.Platform == "" || sel.OS == "" || sel.ImageType == "" {
		return nil, newError(http.StatusBadRequest, "BAD_REQUEST",
			"vertical, platform, os, and imageType are required")
	}

	tmpl := s.manifest.findTemplate(sel.Vertical, sel.SKU, sel.Platform, sel.OS, sel.ImageType)
	if tmpl == "" {
		return nil, newError(http.StatusBadRequest, "NO_MATCH",
			"no template maps to the selected combination")
	}

	path, err := safeTemplatePath(s.cfg.TemplatesDir, tmpl)
	if err != nil {
		return nil, newError(http.StatusInternalServerError, "TEMPLATE_INVALID",
			"manifest template path is invalid")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, newError(http.StatusInternalServerError, "TEMPLATE_MISSING",
			"matched template file not found on disk")
	}

	// Parse+merge for an accurate summary (reuses ICT's own logic). A merge
	// failure means the matched template is invalid — surface it now rather than
	// returning a misleading success that fails at build time.
	merged, err := config.LoadAndMergeTemplate(path)
	if err != nil {
		return nil, newError(http.StatusUnprocessableEntity, "TEMPLATE_INVALID",
			"matched template failed to load/validate: "+err.Error())
	}

	return &ComposeResult{
		Template: tmpl,
		YAML:     string(raw),
		Summary:  buildComposeSummary(sel, merged),
	}, nil
}

// buildComposeSummary constructs a ComposeSummary from a selection and a merged
// template. Shared by Compose and StartBuild (the latter stores the summary on
// the build record for the Build Details panel).
func buildComposeSummary(sel Selection, merged *config.ImageTemplate) ComposeSummary {
	// For overlay-mode templates, surface the baseline image the packages are
	// layered onto (local path or URL). Empty for from-scratch builds.
	var baseImage string
	if merged.Baseline != nil && merged.Baseline.Mode == config.BaselineModeOverlay && merged.Baseline.Source != nil {
		if merged.Baseline.Source.Path != "" {
			baseImage = merged.Baseline.Source.Path
		} else {
			baseImage = merged.Baseline.Source.URL
		}
	}

	return ComposeSummary{
		Vertical:  sel.Vertical,
		SKU:       sel.SKU,
		Platform:  sel.Platform,
		OS:        sel.OS,
		ImageType: sel.ImageType,

		ImageName:      merged.Image.Name,
		ImageVersion:   merged.Image.Version,
		Description:    merged.SystemConfig.Description,
		Architecture:   merged.Target.Arch,
		KernelVersion:  merged.SystemConfig.Kernel.Version,
		PackageCount:   len(merged.SystemConfig.Packages),
		DiskSize:       merged.Disk.Size,
		PartitionCount: len(merged.Disk.Partitions),
		PartitionTable: merged.Disk.PartitionTableType,
		Hostname:       merged.SystemConfig.HostName,
		BaseImage:      baseImage,
	}
}
