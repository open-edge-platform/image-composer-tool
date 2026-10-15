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
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/api/service"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
)

// Shutdown grace windows, used when ctx is cancelled (see Start). Both are
// best-effort: they bound how long Start takes to return, not what actually
// happens to the build — a deployment's own termination grace period
// (Kubernetes terminationGracePeriodSeconds, `docker stop -t`) is what
// ultimately decides whether a SIGKILL cuts this short. Keep their sum below
// that grace period so there's time left to exit cleanly after the wait.
const (
	// httpShutdownTimeout bounds how long Start waits for in-flight HTTP
	// requests (including open SSE log streams) to finish after the listener
	// stops accepting new connections.
	httpShutdownTimeout = 10 * time.Second
	// buildDrainTimeout bounds how long Start waits for an in-flight build to
	// reach a terminal state (and archive its merged template) before giving up.
	buildDrainTimeout = 20 * time.Second
)

// Config holds the server's runtime configuration.
type Config struct {
	Addr         string // listen address, e.g. ":8080"
	TemplatesDir string // directory containing pre-authored templates
	ICTBinary    string // path to the image-composer-tool binary for builds
	WorkDir      string // base directory for per-build work/output directories
	Sudo         bool   // run builds under `sudo -n` (ICT needs root for chroot)
	ManifestPath string // optional manifest file; empty uses the embedded copy
	// PackageReposPath is an optional repository-catalog file; empty uses the
	// embedded copy.
	PackageReposPath string
	// EdgePackPath is an optional Edge Pack catalog file; empty uses the
	// embedded copy.
	EdgePackPath string
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

		PackageReposPath: cfg.PackageReposPath,
		EdgePackPath:     cfg.EdgePackPath,
	})
	if err != nil {
		return nil, err
	}
	return &Server{addr: cfg.Addr, svc: svc}, nil
}

// Start registers routes and serves HTTP until ctx is cancelled or the
// listener fails.
//
// On cancellation (ctx is the signal-aware context main.go installs for
// SIGINT/SIGTERM) it stops accepting new connections, gives in-flight HTTP
// requests httpShutdownTimeout to finish, then gives any build still running
// buildDrainTimeout to reach a terminal state — so a build that's mid-teardown
// when a container orchestrator sends SIGTERM gets a real chance to merge and
// archive its template (see archiveMergedTemplate/archiveAndCleanupDelta)
// before the process exits, instead of being torn down with it.
func (s *Server) Start(ctx context.Context) error {
	log := logger.Logger()
	handler := withMiddleware(s.routes())

	srv := &http.Server{
		Addr:              s.addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	service.LogCancelSupport(s.svc.UseSudo())
	log.Infof("ICT web UI API listening on %s", s.addr)

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}

	log.Infof("shutdown requested — draining connections (up to %s) and any in-flight build (up to %s)",
		httpShutdownTimeout, buildDrainTimeout)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), httpShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Warnf("HTTP server shutdown: %v", err)
	}
	s.svc.WaitForInFlightBuilds(buildDrainTimeout)

	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
