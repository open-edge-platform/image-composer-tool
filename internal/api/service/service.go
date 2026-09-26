// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Package service holds the business logic behind the ICT web UI API: manifest
// lookup, template composition, build tracking/execution, compose history, phase
// detection, and artifact discovery.
//
// The package handles no HTTP request/response types: it never sees an
// http.Request, an http.ResponseWriter, headers, or the generated contract
// types. The api package decodes requests, calls this service, and encodes the
// generated types (internal/api/http). That keeps every behaviour here testable
// by calling a method directly, with no server or recorder.
//
// It does depend on net/http for status *constants*. Failure modes like
// "template not found" or "artifact outside workspace" carry the status the api
// layer should return, modelled by *Error. Re-deriving those from a private
// error taxonomy would add a mapping table in the transport layer without
// making this package any less coupled to the fact that its caller speaks HTTP.
//
// SearchPackages is the one exception to "no network I/O": it reads package
// indexes over HTTP through pkgindexCache, which owns its own caching and
// concurrency so this package doesn't have to.
package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/open-edge-platform/image-composer-tool/internal/api/service/pkgindex"
)

// Config holds the service's runtime configuration.
type Config struct {
	TemplatesDir string // directory containing pre-authored templates
	ICTBinary    string // path to the image-composer-tool binary for builds
	WorkDir      string // base directory for per-build work/output directories
	Sudo         bool   // run builds under `sudo -n` (ICT needs root for chroot)
	ManifestPath string // optional manifest file; empty uses the embedded copy
	// PackageReposPath is an optional repository-catalog file; empty uses the
	// embedded copy.
	PackageReposPath string
	// PkgIndex tunes the package-index cache SearchPackages reads through. A
	// zero value is usable; see pkgindex.Config for its defaults.
	PkgIndex pkgindex.Config
}

// Service holds the API's dependencies and shared state.
type Service struct {
	cfg      Config
	manifest *Manifest
	// repos is the Advanced tab's repository catalog, loaded once at
	// construction. Read-only after New, so it needs no lock.
	repos   []PackageRepo
	tracker *buildTracker
	// pkgindexCache reads and caches the package indexes SearchPackages
	// searches. Built once at construction; safe for concurrent use.
	pkgindexCache *pkgindex.Cache

	// buildMu serializes compose starts so at most one build runs at a time.
	// activeBuildID names the in-flight build (empty when idle). The slot is held
	// from the moment StartBuild spawns the child until runBuild observes the ICT
	// process fully exit (post-teardown), so a second compose can't start while
	// mounts/loop devices from the previous one may still be tearing down.
	buildMu       sync.Mutex
	activeBuildID string

	// signalGroup delivers SIGTERM to a build's process group. It defaults to
	// signalCancel; tests override it to exercise the cancel paths without shelling
	// out to a real `sudo kill` or signalling the test process.
	signalGroup func(pgid int) error
}

// tryAcquireBuildSlot claims the single-build slot for id. It returns false (and
// the id currently holding the slot) if a build is already in flight, so the
// caller can reject the concurrent start.
func (s *Service) tryAcquireBuildSlot(id string) (ok bool, activeID string) {
	s.buildMu.Lock()
	defer s.buildMu.Unlock()
	if s.activeBuildID != "" {
		return false, s.activeBuildID
	}
	s.activeBuildID = id
	return true, ""
}

// releaseBuildSlot frees the single-build slot. It is a no-op if id is not the
// current holder (defensive: only the owning build should release it).
func (s *Service) releaseBuildSlot(id string) {
	s.buildMu.Lock()
	defer s.buildMu.Unlock()
	if s.activeBuildID == id {
		s.activeBuildID = ""
	}
}

// New constructs a Service, loading and validating the manifest and the package
// repository catalog, and applying defaults for any unset config fields.
func New(cfg Config) (*Service, error) {
	m, err := loadManifest(cfg.ManifestPath)
	if err != nil {
		return nil, err
	}
	// Fail construction on a bad catalog rather than at first request: an
	// operator-supplied file is a startup misconfiguration, and surfacing it now
	// beats a server that boots and then 500s on the Advanced tab.
	repos, err := loadPackageRepos(cfg.PackageReposPath)
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
	svc := &Service{
		cfg:           cfg,
		manifest:      m,
		repos:         repos,
		tracker:       newBuildTracker(),
		pkgindexCache: pkgindex.New(cfg.PkgIndex),
	}
	svc.signalGroup = svc.signalCancel
	return svc, nil
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
