// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Package api implements the HTTP transport for the ICT web UI. It is a thin
// adapter over internal/api/service: handlers decode requests into the generated
// contract types (internal/api/http), call the service, and encode the result.
// The service holds all business logic and no HTTP types. See
// docs/architecture-decision-record/adr-web-ui-tech-stack.md.
//
// The JSON endpoints implement the oapi-codegen-generated ServerInterface. The
// build-log SSE stream and the template/log/artifact file downloads don't fit
// that JSON interface, so they are hand-written handlers registered on the same
// mux (see sse.go and downloads.go).
package api

import (
	"net/http"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/api/service"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
)

// Config holds the server's runtime configuration.
type Config struct {
	Addr         string // listen address, e.g. ":8080"
	TemplatesDir string // directory containing pre-authored templates
	ICTBinary    string // path to the image-composer-tool binary for builds
	WorkDir      string // base directory for per-build work/output directories
	Sudo         bool   // run builds under `sudo -n` (ICT needs root for chroot)
	ManifestPath string // optional manifest file; empty uses the embedded copy
}

// Server holds the HTTP server's dependencies: its listen address and the
// service that implements the API's behavior.
type Server struct {
	addr string
	svc  *service.Service
}

// New constructs a Server, building the underlying service (which loads and
// validates the manifest and applies config defaults).
func New(cfg Config) (*Server, error) {
	svc, err := service.New(service.Config{
		TemplatesDir: cfg.TemplatesDir,
		ICTBinary:    cfg.ICTBinary,
		WorkDir:      cfg.WorkDir,
		Sudo:         cfg.Sudo,
		ManifestPath: cfg.ManifestPath,
	})
	if err != nil {
		return nil, err
	}
	return &Server{addr: cfg.Addr, svc: svc}, nil
}

// Start registers routes and blocks serving HTTP.
func (s *Server) Start() error {
	log := logger.Logger()
	handler := withMiddleware(s.routes())

	srv := &http.Server{
		Addr:              s.addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Infof("ICT web UI API listening on %s", s.addr)
	return srv.ListenAndServe()
}
