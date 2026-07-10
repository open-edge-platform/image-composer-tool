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
	res := b.snapshot()
	arts := res.artifacts
	if arts == nil {
		arts = []artifact{}
	}
	writeJSON(w, http.StatusOK, artifactList{BuildID: id, Status: string(res.status), Artifacts: arts})
}
