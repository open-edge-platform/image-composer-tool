// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"

	httpapi "github.com/open-edge-platform/image-composer-tool/internal/api/http"
	"github.com/open-edge-platform/image-composer-tool/internal/config/version"
)

// GetServerInfo handles GET /server.
func (s *Server) GetServerInfo(w http.ResponseWriter, _ *http.Request) {
	var info httpapi.ServerInfo
	info.Version = version.Version
	info.Commit = version.CommitSHA
	info.BuildDate = version.BuildDate
	info.Executor.Name = s.svc.ExecutorName()
	info.Executor.MaxConcurrent = s.svc.MaxConcurrent()
	if phases := s.svc.ExecutorPhases(); len(phases) > 0 {
		out := make([]httpapi.Phase, len(phases))
		for i, p := range phases {
			out[i] = httpapi.Phase{Id: p.ID, Label: p.Label}
		}
		info.Executor.Phases = &out
	}
	info.Auth.Mode = httpapi.ServerInfoAuthMode(s.auth.Mode())
	writeJSON(w, http.StatusOK, info)
}
