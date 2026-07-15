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
	"strings"
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
	HasLogFile  bool            `json:"hasLogFile"` // a downloadable log file exists on disk
	ErrMsg      string          `json:"errMsg,omitempty"`
}

// handleBuildDetails returns the command and paths for a build so the UI can show
// exactly what ran and offer the template for download.
func (s *Server) handleBuildDetails(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	b, ok := s.getBuild(id)
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
		HasLogFile:  b.LogFile != "" && fileExists(b.LogFile),
		ErrMsg:      res.errMsg,
	})
}

// fileExists reports whether path names an existing regular file.
func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

// handleBuildLogFile serves the persisted compose log as a download. Available
// once a build has finished (the log file is written at completion).
func (s *Server) handleBuildLogFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	b, ok := s.getBuild(id)
	if !ok {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "build not found")
		return
	}
	if b.LogFile == "" || !fileExists(b.LogFile) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "no log file for this build")
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", "compose-"+id+".log"))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	http.ServeFile(w, r, b.LogFile)
}

// handleBuildTemplate serves the exact template file that was built, as a
// download, so the operator can inspect or reuse the resolved YAML.
func (s *Server) handleBuildTemplate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	b, ok := s.getBuild(id)
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
	b, ok := s.getBuild(id)
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

	// Guard against a poisoned artifact entry escaping the per-build workspace.
	// Artifact paths are populated from log parsing; validate they stay inside
	// the build's work directory before serving. Resolve both to absolute paths
	// first: b.WorkDir is relative when the server runs with a relative
	// --work-dir (the default), while artifact paths are absolute — a raw
	// HasPrefix would then always fail.
	absArtifact, aerr := filepath.Abs(artifactPath)
	absWorkDir, werr := filepath.Abs(b.WorkDir)
	if aerr != nil || werr != nil ||
		(absArtifact != absWorkDir && !strings.HasPrefix(absArtifact, absWorkDir+string(filepath.Separator))) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "artifact path outside build workspace")
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(artifactPath)))
	w.Header().Set("Content-Type", "application/octet-stream")

	if s.cfg.Sudo {
		// Stream via `sudo cat` so large ISOs don't require buffering the whole
		// file in memory. StdoutPipe gives us a reader we can io.Copy directly
		// to the response writer, chunk by chunk.
		//
		// No `--` guard is needed: artifactPath is always an absolute,
		// filepath.Clean'd path validated to live under the build work dir (never
		// a `-`-prefixed string). Passing `--` would also add a second argument
		// that a scoped `cat <path-glob>` sudoers rule wouldn't match.
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
