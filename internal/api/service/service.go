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
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/api/service/pkgindex"
)

// Config holds the service's runtime configuration.
type Config struct {
	TemplatesDir string // directory containing pre-authored templates
	ICTBinary    string // path to the image-composer-tool binary for builds
	WorkDir      string // base directory for per-build work/output directories
	Sudo         bool   // run builds under `sudo -n` (ICT needs root for chroot)
	ManifestPath string // optional manifest file; empty uses the embedded copy
	// ConfigPath is the global configuration file the server loaded, passed to
	// every build with --config so builds use the same settings (such as
	// oep_installer.token_file) instead of discovering a file of their own,
	// which under sudo would search root's home directory. Empty passes none.
	ConfigPath string
	// PackageReposPath is an optional repository-catalog file; empty uses the
	// embedded copy.
	PackageReposPath string
	// EdgePackPath is an optional Edge Pack catalog file; empty uses the
	// embedded copy.
	EdgePackPath string
	// PkgIndex tunes the package-index cache SearchPackages reads through. A
	// zero value is usable; see pkgindex.Config for its defaults.
	PkgIndex pkgindex.Config
	// Executor, when set, runs builds somewhere other than this machine's ICT
	// binary — a build farm reached over the remote build protocol, say. Nil
	// keeps the local behaviour: runBuild spawns ICTBinary. See executor.go.
	Executor Executor
	// MaxConcurrent bounds builds in flight at once. Zero means 1, the local
	// behaviour: local builds share this machine's mounts and loop devices, so
	// they must not overlap. An Executor that dispatches to separate workers
	// can safely raise this.
	MaxConcurrent int
}

// Service holds the API's dependencies and shared state.
type Service struct {
	cfg      Config
	manifest *Manifest
	// repos is the Advanced tab's repository catalog, loaded once at
	// construction. Read-only after New, so it needs no lock.
	repos []PackageRepo
	// edgePack is the Advanced tab's capability catalog, grouping a subset of
	// `repos`' packages by domain. Loaded once at construction alongside repos
	// and read-only after New, for the same reason.
	edgePack *edgePackSpec
	tracker  *buildTracker
	// pkgindexCache reads and caches the package indexes SearchPackages
	// searches. Built once at construction; safe for concurrent use.
	pkgindexCache *pkgindex.Cache

	// buildMu serializes compose starts so at most MaxConcurrent builds run at
	// once. activeBuilds holds the in-flight build ids. A slot is held from the
	// moment StartBuild spawns the child until runBuild observes the build fully
	// finish (post-teardown), so a second compose can't start while mounts/loop
	// devices from the previous one may still be tearing down.
	buildMu      sync.Mutex
	activeBuilds map[string]struct{}
	// executorCancels holds the cancel func for each in-flight executor build.
	// The local path has no entry here; it signals a process group instead.
	executorCancels map[string]context.CancelFunc
	// admissionClosed is set once by CloseBuildAdmission at shutdown. It is read
	// under buildMu by tryAcquireBuildSlot, which is the single gate every start
	// passes through, so a start either claims a slot before admission closes
	// or is rejected — never both.
	admissionClosed bool
	// setupPending counts starts that hold a slot but have not yet registered
	// their build with the tracker (or bailed out); setupDone is closed when it
	// drops to zero and is nil while no start is mid-setup. Shutdown waits on it
	// so the cancel/wait snapshots cannot miss a build still being set up when
	// the signal arrived.
	setupPending int
	setupDone    chan struct{}

	// signalGroup delivers SIGTERM to a build's process group. It defaults to
	// signalCancel; tests override it to exercise the cancel paths without shelling
	// out to a real `sudo kill` or signalling the test process.
	signalGroup func(pgid int) error
}

// tryAcquireBuildSlot claims a build slot for id. It returns false (and the id
// of one build currently holding a slot) when MaxConcurrent builds are already
// in flight, so the caller can reject the concurrent start. closed reports that
// the server is shutting down and is no longer admitting builds, which the
// caller must distinguish from busy slots: one is a transient conflict worth
// retrying, the other means this process will never run the build.
//
// A successful claim also opens a setup window (see endBuildSetup): the build
// exists only as a slot holder until StartBuild registers it with the tracker,
// and shutdown must not snapshot the tracker while a start is in that gap.
func (s *Service) tryAcquireBuildSlot(id string) (ok bool, activeID string, closed bool) {
	s.buildMu.Lock()
	defer s.buildMu.Unlock()
	if s.admissionClosed {
		return false, "", true
	}
	if s.activeBuilds == nil {
		s.activeBuilds = make(map[string]struct{})
	}
	if len(s.activeBuilds) >= s.MaxConcurrent() {
		for active := range s.activeBuilds {
			return false, active, false
		}
	}
	s.activeBuilds[id] = struct{}{}
	if s.setupPending == 0 {
		s.setupDone = make(chan struct{})
	}
	s.setupPending++
	return true, "", false
}

// endBuildSetup closes one setup window opened by tryAcquireBuildSlot. StartBuild
// defers it, so it runs once the build is tracked and its goroutine is launched —
// or once an error path has released the slot without spawning anything. Either
// way, after this returns the start is no longer invisible to a tracker snapshot.
func (s *Service) endBuildSetup() {
	s.buildMu.Lock()
	defer s.buildMu.Unlock()
	if s.setupPending == 0 {
		return
	}
	s.setupPending--
	if s.setupPending == 0 && s.setupDone != nil {
		close(s.setupDone)
		s.setupDone = nil
	}
}

// CloseBuildAdmission permanently stops this Service from admitting new builds.
//
// Called from Server.Start on a shutdown signal, before the HTTP server begins
// draining. Draining alone does not close this door: http.Server.Shutdown stops
// accepting new connections but lets in-flight handlers run to completion, and
// it gives up on them when its timeout expires without stopping them. A
// POST /builds that was still decoding its body or resolving its template at
// that point would otherwise go on to spawn a child after shutdown had already
// taken its cancellation snapshot — a build nothing would ever signal, left to
// be killed with the process mid-chroot, holding mounts and loop devices.
//
// Closing admission is irreversible: the process is on its way out, so there is
// no state to restore.
func (s *Service) CloseBuildAdmission() {
	s.buildMu.Lock()
	defer s.buildMu.Unlock()
	s.admissionClosed = true
}

// waitForBuildSetup blocks until every start currently mid-setup has finished
// registering its build, or until timeout elapses.
//
// This closes the remaining gap between claiming the build slot and appearing in
// the tracker. A start that won the race against CloseBuildAdmission is already
// committed to spawning a child, so shutdown must let it become visible before
// snapshotting the tracker — otherwise the very build we failed to shut out is
// also the one we fail to cancel and fail to wait for. Bounded by timeout
// because setup resolves templates and touches the filesystem, and a shutdown
// path must not be able to block indefinitely on it.
func (s *Service) waitForBuildSetup(timeout time.Duration) {
	s.buildMu.Lock()
	pending := s.setupDone
	s.buildMu.Unlock()
	if pending == nil {
		return
	}
	select {
	case <-pending:
	case <-time.After(timeout):
	}
}

// releaseBuildSlot frees id's slot. It is a no-op if id holds none (defensive:
// only the owning build should release it).
func (s *Service) releaseBuildSlot(id string) {
	s.buildMu.Lock()
	defer s.buildMu.Unlock()
	delete(s.activeBuilds, id)
}

// MaxConcurrent reports how many builds may run at once, after defaults. The UI
// needs it to decide whether to offer another compose; without it the only way
// to discover the limit is to hit a 409.
func (s *Service) MaxConcurrent() int {
	if s.cfg.MaxConcurrent < 1 {
		return 1
	}
	return s.cfg.MaxConcurrent
}

// ExecutorName names the configured Executor, or "local" when builds run this
// machine's ICT binary.
func (s *Service) ExecutorName() string {
	if s.cfg.Executor == nil {
		return "local"
	}
	return s.cfg.Executor.Name()
}

// ExecutorPhases lists the build phases the configured Executor reports, in
// order, so the UI can draw its stepper from them. Nil for the local path, whose
// phases are the fixed, log-derived ones in phases.go.
func (s *Service) ExecutorPhases() []Phase {
	if p, ok := s.cfg.Executor.(PhaseReporter); ok {
		return p.Phases()
	}
	return nil
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
	pack, err := loadEdgePack(cfg.EdgePackPath)
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
		edgePack:      pack,
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
