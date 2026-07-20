// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

// handleGetManifest returns the full configuration manifest.
func (s *Server) handleGetManifest(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.manifest)
}

// composeRequest is the body of POST /templates/compose.
type composeRequest struct {
	Vertical  string `json:"vertical"`
	SKU       string `json:"sku"`
	Platform  string `json:"platform"`
	OS        string `json:"os"`
	ImageType string `json:"imageType"`
}

// composeSummary is the human-readable summary shown in the Review panel.
type composeSummary struct {
	// Selection echo — what the user chose
	Vertical  string `json:"vertical"`
	SKU       string `json:"sku"`
	Platform  string `json:"platform"`
	OS        string `json:"os"`
	ImageType string `json:"imageType"`

	// Template-derived — info the user can't see from the dropdowns
	ImageName      string `json:"imageName"`
	ImageVersion   string `json:"imageVersion"`
	Description    string `json:"description"`
	Architecture   string `json:"architecture"`
	KernelVersion  string `json:"kernelVersion"`
	PackageCount   int    `json:"packageCount"`
	DiskSize       string `json:"diskSize"`
	PartitionCount int    `json:"partitionCount"`
	PartitionTable string `json:"partitionTable"`
	Hostname       string `json:"hostname"`
}

type composeResponse struct {
	Template string         `json:"template"` // resolved template filename
	YAML     string         `json:"yaml"`     // matched template YAML, verbatim
	Summary  composeSummary `json:"summary"`
}

// buildComposeSummary constructs a composeSummary from a request and a merged
// template. Extracted so handleStartBuild can reuse it to store the summary on
// the build record for display in the Build Details panel.
func buildComposeSummary(req composeRequest, merged *config.ImageTemplate) composeSummary {
	return composeSummary{
		Vertical:  req.Vertical,
		SKU:       req.SKU,
		Platform:  req.Platform,
		OS:        req.OS,
		ImageType: req.ImageType,

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
	}
}

// handleCompose resolves the selections to a template and returns its YAML plus
// a summary. It is a lookup — the backend never synthesizes a template.
func (s *Server) handleCompose(w http.ResponseWriter, r *http.Request) {
	var req composeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return
	}
	if req.Vertical == "" || req.Platform == "" || req.OS == "" || req.ImageType == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "vertical, platform, os, and imageType are required")
		return
	}

	tmpl := s.manifest.findTemplate(req.Vertical, req.SKU, req.Platform, req.OS, req.ImageType)
	if tmpl == "" {
		writeError(w, http.StatusBadRequest, "NO_MATCH", "no template maps to the selected combination")
		return
	}

	path, err := safeTemplatePath(s.cfg.TemplatesDir, tmpl)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "TEMPLATE_INVALID", "manifest template path is invalid")
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "TEMPLATE_MISSING", "matched template file not found on disk")
		return
	}

	// Parse+merge for an accurate summary (reuses ICT's own logic). A merge
	// failure means the matched template is invalid — surface it now rather than
	// returning a misleading success that fails at build time.
	merged, err := config.LoadAndMergeTemplate(path)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "TEMPLATE_INVALID",
			"matched template failed to load/validate: "+err.Error())
		return
	}
	summary := buildComposeSummary(req, merged)

	writeJSON(w, http.StatusOK, composeResponse{
		Template: tmpl,
		YAML:     string(raw),
		Summary:  summary,
	})
}
