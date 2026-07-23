// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"io/fs"
	"net/http"

	httpapi "github.com/open-edge-platform/image-composer-tool/internal/api/http"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/webui"
)

// routes registers all API endpoints on a Go 1.22+ ServeMux.
//
// The JSON endpoints are registered from the generated OpenAPI server
// (httpapi.HandlerFromMux), which maps each path/method to a ServerInterface
// method that Server implements in handlers.go. The streaming SSE endpoint and
// the file downloads are registered directly here — they don't fit the JSON
// interface and stay hand-written (sse.go, downloads.go).
func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()

	// Generated JSON routes: GET /manifest, POST /templates/compose,
	// POST /builds, GET /builds, GET /builds/{id}/{details,artifacts}.
	httpapi.HandlerFromMuxWithBaseURL(s, mux, "/api/v1")

	// Hand-written endpoints outside the generated JSON interface: SSE log stream
	// (text/event-stream) and file/binary downloads (YAML / text / octet-stream).
	mux.HandleFunc("GET /api/v1/builds/{id}/logs", s.handleBuildLogs)
	mux.HandleFunc("GET /api/v1/builds/{id}/template", s.handleBuildTemplate)
	mux.HandleFunc("GET /api/v1/builds/{id}/logfile", s.handleBuildLogFile)
	mux.HandleFunc("GET /api/v1/builds/{id}/artifacts/{name}", s.handleBuildArtifactDownload)

	// Web UI: serve the embedded SPA at the root. API routes above are more
	// specific and take precedence in the mux. Only mounted when a real build is
	// embedded; otherwise the UI is served by the Vite dev server.
	if webui.HasRealBuild() {
		if assets, err := webui.Assets(); err == nil {
			mux.Handle("GET /", spaHandler(assets))
		} else {
			logger.Logger().Warnf("web UI assets unavailable: %v", err)
		}
	} else {
		logger.Logger().Info("no embedded web UI build; serve the UI via `cd web && npm run dev`")
	}

	return mux
}

// spaHandler serves static assets from the embedded file system and falls back
// to index.html for unknown paths, so client-side routes resolve correctly.
func spaHandler(assets fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Serve the file if it exists; otherwise hand back index.html (SPA route).
		p := r.URL.Path
		if p != "/" {
			if _, err := fs.Stat(assets, p[1:]); err != nil {
				r = r.Clone(r.Context())
				r.URL.Path = "/"
			}
		}
		fileServer.ServeHTTP(w, r)
	})
}
