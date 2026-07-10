// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import "net/http"

// routes registers all API endpoints on a Go 1.22+ ServeMux.
func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()

	// Read path
	mux.HandleFunc("GET /api/v1/manifest", s.handleGetManifest)
	mux.HandleFunc("POST /api/v1/templates/compose", s.handleCompose)

	// Build path (implemented in builds.go)
	mux.HandleFunc("POST /api/v1/builds", s.handleStartBuild)
	mux.HandleFunc("GET /api/v1/builds/{id}/logs", s.handleBuildLogs)
	mux.HandleFunc("GET /api/v1/builds/{id}/artifacts", s.handleBuildArtifacts)

	return mux
}
