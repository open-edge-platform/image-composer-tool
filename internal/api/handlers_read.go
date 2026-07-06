// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

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
	ImageName      string `json:"imageName"`
	Vertical       string `json:"vertical"`
	SKU            string `json:"sku"`
	Platform       string `json:"platform"`
	OS             string `json:"os"`
	ImageType      string `json:"imageType"`
	PackageCount   int    `json:"packageCount"`
	DiskSize       string `json:"diskSize"`
	PartitionCount int    `json:"partitionCount"`
}

type composeResponse struct {
	Template string         `json:"template"` // resolved template filename
	YAML     string         `json:"yaml"`     // matched template YAML, verbatim
	Summary  composeSummary `json:"summary"`
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

	path := filepath.Join(s.cfg.TemplatesDir, tmpl)
	raw, err := os.ReadFile(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "TEMPLATE_MISSING", "matched template file not found on disk")
		return
	}

	// Parse+merge for an accurate summary (reuses ICT's own logic).
	summary := composeSummary{
		Vertical: req.Vertical, SKU: req.SKU, Platform: req.Platform,
		OS: req.OS, ImageType: req.ImageType,
	}
	if merged, err := config.LoadAndMergeTemplate(path); err == nil {
		summary.ImageName = merged.Image.Name
		summary.PackageCount = len(merged.SystemConfig.Packages)
		summary.DiskSize = merged.Disk.Size
		summary.PartitionCount = len(merged.Disk.Partitions)
	}

	writeJSON(w, http.StatusOK, composeResponse{
		Template: tmpl,
		YAML:     string(raw),
		Summary:  summary,
	})
}
