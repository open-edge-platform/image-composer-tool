// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Package api implements the HTTP backend for the ICT web UI. It serves a
// configuration manifest, resolves pre-authored templates, and triggers image
// builds via the ICT binary — see docs/architecture/adr-web-ui-tech-stack.md.
package api

import (
	"net/http"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
)

// Config holds the server's runtime configuration.
type Config struct {
	Addr         string // listen address, e.g. ":8080"
	TemplatesDir string // directory containing pre-authored templates
	ICTBinary    string // path to the image-composer-tool binary for builds
	WorkDir      string // base directory for per-build work/output directories
	Sudo         bool   // run builds under `sudo -n` (ICT needs root for chroot)
}

// Server holds the API's dependencies and shared state.
type Server struct {
	cfg      Config
	manifest *Manifest
	tracker  *buildTracker
}

// New constructs a Server, loading and validating the embedded manifest.
func New(cfg Config) (*Server, error) {
	m, err := loadManifest()
	if err != nil {
		return nil, err
	}
	if cfg.TemplatesDir == "" {
		cfg.TemplatesDir = "image-templates"
	}
	if cfg.ICTBinary == "" {
		cfg.ICTBinary = "./image-composer-tool"
	}
	if cfg.WorkDir == "" {
		cfg.WorkDir = "webui-workspace"
	}
	return &Server{cfg: cfg, manifest: m, tracker: newBuildTracker()}, nil
}

// Start registers routes and blocks serving HTTP.
func (s *Server) Start() error {
	log := logger.Logger()
	mux := s.routes()
	handler := withMiddleware(mux)

	srv := &http.Server{
		Addr:              s.cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Infof("ICT web UI API listening on %s", s.cfg.Addr)
	return srv.ListenAndServe()
}
