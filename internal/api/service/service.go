// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Package service holds the business logic behind the ICT web UI API: manifest
// lookup, template composition, build tracking/execution, compose history, phase
// detection, and artifact discovery. It is deliberately free of HTTP types — the
// api package decodes requests, calls this service, and encodes the generated
// contract types (internal/api/http). Errors that carry an HTTP status/code are
// modelled by *Error so the api layer can map them without importing net/http
// concepts into the domain.
package service

import (
	"os"
	"os/exec"
	"path/filepath"
)

// Config holds the service's runtime configuration.
type Config struct {
	TemplatesDir string // directory containing pre-authored templates
	ICTBinary    string // path to the image-composer-tool binary for builds
	WorkDir      string // base directory for per-build work/output directories
	Sudo         bool   // run builds under `sudo -n` (ICT needs root for chroot)
	ManifestPath string // optional manifest file; empty uses the embedded copy
}

// Service holds the API's dependencies and shared state.
type Service struct {
	cfg      Config
	manifest *Manifest
	tracker  *buildTracker
}

// New constructs a Service, loading and validating the manifest and applying
// defaults for any unset config fields.
func New(cfg Config) (*Service, error) {
	m, err := loadManifest(cfg.ManifestPath)
	if err != nil {
		return nil, err
	}
	if cfg.TemplatesDir == "" {
		cfg.TemplatesDir = "image-templates"
	}
	if cfg.ICTBinary == "" {
		cfg.ICTBinary = discoverICTBinary()
	}
	// Resolve the ICT binary to an absolute path. `sudo` matches the command in
	// its sudoers rule literally, so a relative path (e.g. ./build/...) would not
	// match an absolute NOPASSWD rule and would fall through to a password prompt.
	if abs, err := filepath.Abs(cfg.ICTBinary); err == nil {
		cfg.ICTBinary = abs
	}
	if cfg.WorkDir == "" {
		cfg.WorkDir = "webui-workspace"
	}
	return &Service{cfg: cfg, manifest: m, tracker: newBuildTracker()}, nil
}

// UseSudo reports whether builds and privileged file reads run under `sudo -n`.
func (s *Service) UseSudo() bool { return s.cfg.Sudo }

// discoverICTBinary picks the image-composer-tool binary to invoke when the
// operator doesn't pass --ict-binary. We don't know whether they built with
// `earthly +build` (outputs ./build/) or a plain `go build` (often the repo
// root), so probe both, preferring ./build/, then fall back to a PATH lookup.
func discoverICTBinary() string {
	candidates := []string{"./build/image-composer-tool", "./image-composer-tool"}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return c
		}
	}
	if p, err := exec.LookPath("image-composer-tool"); err == nil {
		return p
	}
	// Nothing found; return the conventional path so the eventual build failure
	// names a sensible location.
	return "./build/image-composer-tool"
}

// Error is a domain error carrying an HTTP status, a stable machine code, and a
// human-readable message. The api layer maps it directly onto the error response
// envelope; any non-*Error is treated as an opaque 500.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

// newError constructs a domain *Error.
func newError(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}
