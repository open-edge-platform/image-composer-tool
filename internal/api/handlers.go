// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"errors"
	"net/http"

	httpapi "github.com/open-edge-platform/image-composer-tool/internal/api/http"
	"github.com/open-edge-platform/image-composer-tool/internal/api/service"
)

// Server implements the generated httpapi.ServerInterface. Each method decodes
// the request into the generated types, calls the service, maps the result back
// to the generated response type, and encodes it — no business logic here.
var _ httpapi.ServerInterface = (*Server)(nil)

// GetManifest handles GET /manifest.
func (s *Server) GetManifest(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, fromManifest(s.svc.Manifest()))
}

// ComposeTemplate handles POST /templates/compose.
func (s *Server) ComposeTemplate(w http.ResponseWriter, r *http.Request) {
	var req httpapi.ComposeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return
	}
	res, err := s.svc.Compose(toSelection(req))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, fromComposeResult(res))
}

// StartBuild handles POST /builds.
func (s *Server) StartBuild(w http.ResponseWriter, r *http.Request) {
	var req httpapi.BuildRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return
	}
	acc, err := s.svc.StartBuild(toBuildRequest(req))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, fromBuildAccepted(acc))
}

// ListBuilds handles GET /builds.
func (s *Server) ListBuilds(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, fromHistory(s.svc.ListBuilds()))
}

// GetBuildDetails handles GET /builds/{id}/details.
func (s *Server) GetBuildDetails(w http.ResponseWriter, _ *http.Request, id httpapi.BuildId) {
	d, err := s.svc.BuildDetails(id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, fromBuildDetails(d))
}

// ListBuildArtifacts handles GET /builds/{id}/artifacts.
func (s *Server) ListBuildArtifacts(w http.ResponseWriter, _ *http.Request, id httpapi.BuildId) {
	l, err := s.svc.BuildArtifacts(id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, fromArtifactList(l))
}

// writeServiceError maps a service error onto the JSON error envelope. Domain
// *service.Error carry an HTTP status and machine code; any other error is an
// opaque 500 so internal details don't leak to clients.
func writeServiceError(w http.ResponseWriter, err error) {
	var se *service.Error
	if errors.As(err, &se) {
		writeError(w, se.Status, se.Code, se.Message)
		return
	}
	writeError(w, http.StatusInternalServerError, "INTERNAL", "internal server error")
}
