// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"
	"os"
	"path/filepath"
)

// buildDetails carries the reproducibility/troubleshooting metadata the UI shows
// in its collapsible "Build details" panel: the exact command, the resolved
// template, and the per-build work/cache directories.
type buildDetails struct {
	BuildID     string `json:"buildId"`
	Status      string `json:"status"`
	Command     string `json:"command"`
	Template    string `json:"template"`
	TemplateURL string `json:"templateUrl"`
	WorkDir     string `json:"workDir"`
	CacheDir    string `json:"cacheDir"`
}

// handleBuildDetails returns the command and paths for a build so the UI can show
// exactly what ran and offer the template for download.
func (s *Server) handleBuildDetails(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	b, ok := s.tracker.get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "build not found")
		return
	}
	res := b.snapshot()
	writeJSON(w, http.StatusOK, buildDetails{
		BuildID:     id,
		Status:      string(res.status),
		Command:     b.Command,
		Template:    b.Template,
		TemplateURL: "/api/v1/builds/" + id + "/template",
		WorkDir:     b.WorkDir,
		CacheDir:    b.CacheDir,
	})
}

// handleBuildTemplate serves the exact template file that was built, as a
// download, so the operator can inspect or reuse the resolved YAML.
func (s *Server) handleBuildTemplate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	b, ok := s.tracker.get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "build not found")
		return
	}
	if b.TemplatePath == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "no template recorded for this build")
		return
	}
	data, err := os.ReadFile(b.TemplatePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "TEMPLATE_READ", "cannot read template file")
		return
	}
	name := b.Template
	if name == "" {
		name = filepath.Base(b.TemplatePath)
	}
	w.Header().Set("Content-Type", "application/yaml")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+name+"\"")
	_, _ = w.Write(data)
}
