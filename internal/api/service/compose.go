// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"fmt"
	"net/http"
	"os"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

// Selection is the set of UI choices that identify one combination, plus any
// Advanced-mode overrides. The service uses the first five fields to look up
// the matching pre-authored template in the manifest; ImageName never
// participates in that lookup (findTemplate ignores it) — it is applied to
// the resolved template afterward via a generated extends delta.
type Selection struct {
	Vertical  string
	SKU       string
	Platform  string
	OS        string
	ImageType string

	// ImageName overrides the matched template's image.name. Empty means
	// "not overridden".
	ImageName string
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
	Template string // resolved template filename (the curated parent, even when overridden)
	YAML     string // the resolved final template — what a build from this selection would run
	Summary  ComposeSummary
}

// Compose resolves the selections to a template, applies any Advanced-mode
// overrides, and returns the resulting resolved template plus a summary.
// Input and lookup failures return a *Error carrying the appropriate HTTP
// status/code.
//
// With no overrides, the returned YAML is exactly what
// `image-composer-tool resolve --full` prints for the matched curated
// template — the same functions are used. With an override, a generated
// extends delta is resolved instead, so the returned YAML is what building
// this exact selection would produce. Either way, this is the review path:
// it never mutates the curated template on disk.
func (s *Service) Compose(sel Selection) (*ComposeResult, error) {
	if sel.Vertical == "" || sel.Platform == "" || sel.OS == "" || sel.ImageType == "" {
		return nil, newError(http.StatusBadRequest, "BAD_REQUEST",
			"vertical, platform, os, and imageType are required")
	}
	if err := ValidateImageName(sel.ImageName); err != nil {
		return nil, newError(http.StatusBadRequest, "BAD_REQUEST", err.Error())
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
	// A manifest entry pointing at a file that isn't on disk is a server-side
	// configuration error, not "this template is invalid" — surface it as its
	// own code/status rather than letting it fall through to the generic
	// load/merge failure below. Distinguish "not found" from other stat
	// failures (permissions, transient IO) so the latter isn't misreported as
	// a missing file and its cause isn't lost.
	if _, statErr := os.Stat(path); statErr != nil {
		if os.IsNotExist(statErr) {
			return nil, newError(http.StatusInternalServerError, "TEMPLATE_MISSING",
				"matched template file not found on disk")
		}
		return nil, newError(http.StatusInternalServerError, "TEMPLATE_STAT_FAILED",
			fmt.Sprintf("checking matched template file: %v", statErr))
	}

	resolvePath := path
	if sel.ImageName != "" {
		deltaPath, cleanup, derr := s.deltaForOverride(tmpl, path, sel)
		if derr != nil {
			return nil, newError(http.StatusInternalServerError, "TEMPLATE_INVALID", derr.Error())
		}
		defer cleanup()
		resolvePath = deltaPath
	}

	// Parse+merge (reuses ICT's own logic) for both the summary and the
	// resolved YAML. A merge failure means the matched template (or, with an
	// override, the generated delta) is invalid — surface it now rather than
	// returning a misleading success that fails at build time.
	merged, err := config.LoadAndMergeTemplate(resolvePath)
	if err != nil {
		return nil, newError(http.StatusUnprocessableEntity, "TEMPLATE_INVALID",
			"matched template failed to load/validate: "+err.Error())
	}

	yamlBytes, err := config.MarshalTemplateYAML(config.RedactSensitiveData(merged))
	if err != nil {
		return nil, newError(http.StatusInternalServerError, "TEMPLATE_INVALID",
			"resolved template failed to marshal: "+err.Error())
	}

	return &ComposeResult{
		Template: tmpl,
		YAML:     string(yamlBytes),
		Summary:  buildComposeSummary(sel, merged),
	}, nil
}

// deltaForOverride loads the curated parent's image/target info, renders a
// delta applying sel's overrides, and writes it via writeDelta.
//
// parentTmpl is the manifest-provided name (relative to TemplatesDir, as
// returned by findTemplate/safeTemplatePath) — it is used verbatim as the
// delta's `extends` value, since the delta is always written at the root of
// TemplatesDir and extends is resolved relative to the child's own directory.
// Using filepath.Base(parentPath) instead would break for a curated template
// that lives in a subdirectory of TemplatesDir.
//
// Split out of Compose so StartBuild's build-time delta generation
// (builds.go) can share the exact same rendering path.
func (s *Service) deltaForOverride(parentTmpl, parentPath string, sel Selection) (path string, cleanup func(), err error) {
	parent, err := config.LoadTemplate(parentPath, false)
	if err != nil {
		return "", nil, fmt.Errorf("loading parent template: %w", err)
	}
	data, err := buildDelta(parentTmpl, parent.Image, parent.Target, sel)
	if err != nil {
		return "", nil, err
	}
	return s.writeDelta(data)
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

	// Overlay mode doesn't populate systemConfig.kernel; when a kernel swap is
	// configured, surface its (descriptive-only) version instead.
	kernelVersion := merged.SystemConfig.Kernel.Version
	if merged.OverlayPolicy != nil && merged.OverlayPolicy.ReplaceKernel != nil && merged.OverlayPolicy.ReplaceKernel.Version != "" {
		kernelVersion = merged.OverlayPolicy.ReplaceKernel.Version
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
		KernelVersion:  kernelVersion,
		PackageCount:   len(merged.SystemConfig.Packages),
		DiskSize:       merged.Disk.Size,
		PartitionCount: len(merged.Disk.Partitions),
		PartitionTable: merged.Disk.PartitionTableType,
		Hostname:       merged.SystemConfig.HostName,
		BaseImage:      baseImage,
	}
}
