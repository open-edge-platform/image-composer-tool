// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import "net/http"

type artifactList struct {
	BuildID   string     `json:"buildId"`
	Status    string     `json:"status"`
	Artifacts []artifact `json:"artifacts"`
}

// handleBuildArtifacts returns the output artifacts for a build.
func (s *Server) handleBuildArtifacts(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	b, ok := s.tracker.get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "build not found")
		return
	}
	b.mu.Lock()
	arts := make([]artifact, len(b.artifacts))
	copy(arts, b.artifacts)
	status := string(b.Status)
	b.mu.Unlock()

	writeJSON(w, http.StatusOK, artifactList{BuildID: id, Status: status, Artifacts: arts})
}
