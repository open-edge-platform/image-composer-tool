// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"fmt"
	"io"
	"net/http"
	"os"
)

// This file holds the file/binary download endpoints. Like the SSE log stream,
// they are outside the generated JSON ServerInterface — oapi-codegen models
// application/yaml, text/plain, and application/octet-stream downloads poorly —
// so they are hand-written handlers registered on the mux in routes().

// handleBuildTemplate serves the exact template file that was built, as a
// download, so the operator can inspect or reuse the resolved YAML.
func (s *Server) handleBuildTemplate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	b, ok := s.svc.Build(id)
	if !ok {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "build not found")
		return
	}
	path, name, err := b.TemplateFile()
	if err != nil {
		writeServiceError(w, err)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "TEMPLATE_READ", "cannot read template file")
		return
	}
	w.Header().Set("Content-Type", "application/yaml")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+name+"\"")
	_, _ = w.Write(data)
}

// handleBuildLogFile serves the persisted compose log as a download. Available
// once a build has finished (the log file is written at completion).
func (s *Server) handleBuildLogFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	b, ok := s.svc.Build(id)
	if !ok {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "build not found")
		return
	}
	logFile, ok := b.LogFilePath()
	if !ok {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "no log file for this build")
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", "compose-"+id+".log"))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	http.ServeFile(w, r, logFile)
}

// handleBuildArtifactDownload serves a single build artifact by name as a
// download. The artifact must be in the build's recorded artifact list and
// resolve inside the build's work directory — arbitrary paths are rejected by
// the service. Large files stream chunk-by-chunk rather than buffering.
func (s *Server) handleBuildArtifactDownload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	name := r.PathValue("name")
	rc, filename, err := s.svc.OpenArtifact(r.Context(), id, name)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	defer func() { _ = rc.Close() }()

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = io.Copy(w, rc)
}
