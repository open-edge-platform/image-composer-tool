// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
)

// buildDetails carries the reproducibility/troubleshooting metadata the UI shows
// in its collapsible "Build details" panel: the exact command, the resolved
// template, and the per-build work/cache directories.
type buildDetails struct {
	BuildID     string          `json:"buildId"`
	Status      string          `json:"status"`
	Command     string          `json:"command"`
	Template    string          `json:"template"`
	TemplateURL string          `json:"templateUrl"`
	WorkDir     string          `json:"workDir"`
	CacheDir    string          `json:"cacheDir"`
	Summary     *composeSummary `json:"summary,omitempty"`
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
		Summary:     b.Summary,
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

// handleBuildArtifactDownload serves a single build artifact by name as a
// download. The artifact must be in the build's recorded artifact list —
// arbitrary paths are not accepted.
//
// Artifact files are owned by root (ICT builds run under sudo). When --sudo is
// configured we stream via `sudo -n cat`; otherwise we read directly (dev env).
func (s *Server) handleBuildArtifactDownload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	name := r.PathValue("name")
	b, ok := s.tracker.get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "build not found")
		return
	}
	res := b.snapshot()
	var artifactPath string
	for _, a := range res.artifacts {
		if a.Name == name {
			artifactPath = a.Path
			break
		}
	}
	if artifactPath == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "artifact not found")
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(artifactPath)))
	w.Header().Set("Content-Type", "application/octet-stream")

	if s.cfg.Sudo {
		// Stream via `sudo cat` so large ISOs don't require buffering the whole
		// file in memory. StdoutPipe gives us a reader we can io.Copy directly
		// to the response writer, chunk by chunk.
		cmd := exec.CommandContext(r.Context(), "sudo", "-n", "cat", artifactPath)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			http.Error(w, "failed to open artifact stream", http.StatusInternalServerError)
			return
		}
		if err := cmd.Start(); err != nil {
			http.Error(w, "failed to read artifact", http.StatusInternalServerError)
			return
		}
		_, _ = io.Copy(w, stdout)
		_ = cmd.Wait()
		return
	}

	f, err := os.Open(artifactPath)
	if err != nil {
		http.Error(w, "cannot read artifact file", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	_, _ = io.Copy(w, f)
}
