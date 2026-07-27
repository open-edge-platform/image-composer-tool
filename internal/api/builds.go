// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
)

// buildStatus is the lifecycle state of a build.
type buildStatus string

const (
	statusNotStarted buildStatus = "not-started"
	statusRunning    buildStatus = "running"
	statusCancelling buildStatus = "cancelling"
	statusCancelled  buildStatus = "cancelled"
	statusSuccess    buildStatus = "success" // surfaced as "completed" in the UI
	statusFailed     buildStatus = "failed"
)

// artifact describes one output file (image or SBOM).
type artifact struct {
	Name string `json:"name"`
	Type string `json:"type"` // "image" | "sbom"
	Path string `json:"path"`
	Size string `json:"size,omitempty"` // human-readable, e.g. "1.13 GB" (from ICT output)
}

// build is the in-memory record of a single build (MVP-1: no persistence).
//
// All mutable fields are guarded by mu. ID, WorkDir, Template, and done are set
// once at construction and are safe to read without the lock.
type build struct {
	ID           string
	RootDir      string // per-build root (parent of work/ and cache/)
	WorkDir      string
	CacheDir     string
	Template     string          // template file name (for display)
	TemplatePath string          // resolved on-disk path (for download)
	Command      string          // exact command run, for the UI's troubleshoot panel
	Summary      *composeSummary // image configuration summary, nil for YAML builds
	CreatedAt    time.Time       // when the compose was started
	LogFile      string          // on-disk log file path, written at finish
	done         chan struct{}   // closed when the build finishes

	mu        sync.Mutex
	status    buildStatus
	logLines  []string // buffered log history for late log subscribers
	artifacts []artifact
	errMsg    string
	// pgid is the process-group id of the running ICT build (0 until started).
	// The build is spawned as its own group leader, so a cancel can signal the
	// whole child tree (sudo → ICT → its helpers) rather than only the immediate
	// child.
	pgid int
	// cancelRequested records that a user cancel was issued, so the terminal
	// classification can prefer "cancelled" over a bare failure.
	cancelRequested bool
	// residual, when non-nil, describes teardown trouble that left the machine in
	// a state a user may need to fix by hand: either the cancel signal itself
	// failed (cancellation-failure) or ICT reported leftover mounts/loop devices
	// during its own cleanup (cleanup-failure). Surfaced to the API/UI.
	residual *residualIssue
}

// residualKind classifies why teardown may have left residue.
type residualKind string

const (
	// residualCancellation: the cancel signal could not be delivered (e.g. the
	// `sudo -n kill` call failed), so ICT may never have started its teardown.
	residualCancellation residualKind = "cancellation-failure"
	// residualCleanup: ICT ran but reported leftover mounts/loop devices at ERROR
	// (with a `mount | grep` / `losetup -l` hint) — its own cleanup didn't fully
	// succeed.
	residualCleanup residualKind = "cleanup-failure"
)

// residualIssue carries enough detail for a user to remediate leftover state
// manually. Detail holds the specific hint (the failing kill error, or the
// mount/loop lines ICT logged).
type residualIssue struct {
	Kind   residualKind `json:"kind"`
	Detail string       `json:"detail"`
}

// result is an immutable snapshot of a build's terminal state.
type result struct {
	status    buildStatus
	artifacts []artifact
	errMsg    string
	logFile   string
	residual  *residualIssue
}

// snapshot returns the build's current status, artifacts, error, and log-file
// path under lock. logFile is included in the snapshot because it is written
// under b.mu in finish(); reading it directly off the struct from a handler
// would race a concurrently finishing build.
func (b *build) snapshot() result {
	b.mu.Lock()
	defer b.mu.Unlock()
	arts := make([]artifact, len(b.artifacts))
	copy(arts, b.artifacts)
	return result{status: b.status, artifacts: arts, errMsg: b.errMsg, logFile: b.LogFile, residual: b.residual}
}

// buildTracker holds all builds for the process lifetime.
type buildTracker struct {
	mu     sync.Mutex
	builds map[string]*build
}

func newBuildTracker() *buildTracker {
	return &buildTracker{builds: make(map[string]*build)}
}

func (t *buildTracker) add(b *build) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.builds[b.ID] = b
}

func (t *buildTracker) get(id string) (*build, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	b, ok := t.builds[id]
	return b, ok
}

// all returns a snapshot slice of the currently tracked (in-memory) builds.
func (t *buildTracker) all() []*build {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]*build, 0, len(t.builds))
	for _, b := range t.builds {
		out = append(out, b)
	}
	return out
}

// appendLog records a log line and is safe for concurrent use.
func (b *build) appendLog(line string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.logLines = append(b.logLines, line)
}

func (b *build) snapshotLogs() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, len(b.logLines))
	copy(out, b.logLines)
	return out
}

// beginCancel marks the build as cancel-requested and transitions running →
// cancelling. It reports whether the transition happened (false if the build is
// not currently running — already terminal or already cancelling) and returns
// the process-group id to signal. Idempotent: a second cancel on an
// already-cancelling build returns cancelling=false so the handler can 409.
func (b *build) beginCancel() (transitioned bool, pgid int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.status != statusRunning {
		return false, b.pgid
	}
	b.cancelRequested = true
	b.status = statusCancelling
	return true, b.pgid
}

// setPgidCheckCancel records the running child's process-group id and reports,
// atomically under the same lock, whether a cancel was already requested while
// pgid was still unset (the start-race window). When pending is true the caller
// must deliver the cancel signal now, since the earlier handleCancelBuild call
// transitioned to cancelling but had no group to signal. Returns the recorded
// pgid so the caller can signal without re-taking the lock.
func (b *build) setPgidCheckCancel(pgid int) (recorded int, pending bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pgid = pgid
	// A cancel is pending for us to service only if one was requested and the
	// build is still in the cancelling state (not already terminal).
	return pgid, b.cancelRequested && b.status == statusCancelling
}

// wasCancelRequested reports whether a user cancel was issued for this build.
func (b *build) wasCancelRequested() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cancelRequested
}

// setResidual records a teardown-residue issue (cancellation-failure or
// cleanup-failure) so the terminal snapshot can surface it to the UI. The first
// issue recorded wins: a failed cancel signal (cancellation-failure) is the root
// cause and shouldn't be overwritten by a later cleanup observation.
func (b *build) setResidual(kind residualKind, detail string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.residual == nil {
		b.residual = &residualIssue{Kind: kind, Detail: detail}
	}
}

// --- request/response bodies ---

type buildRequest struct {
	Compose *composeRequest `json:"compose"`
	YAML    string          `json:"yaml"`
}

type buildAccepted struct {
	BuildID string `json:"buildId"`
	Status  string `json:"status"`
	LogsURL string `json:"logsUrl"`
}

// handleStartBuild resolves the template, starts an os/exec build, and returns a
// build id. Basic sends {compose}; Advanced would send {yaml} (not used by the
// Basic slice but accepted for forward-compat).
func (s *Server) handleStartBuild(w http.ResponseWriter, r *http.Request) {
	var req buildRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return
	}

	id := uuid.NewString()

	// Claim the single-build slot before doing any setup. If a build is already
	// in flight (including one still tearing down mounts/loops after a cancel or
	// failure), reject with 409 so a second compose can't race the teardown. The
	// slot is released by runBuild only after the ICT child fully exits; on the
	// error paths below we release it explicitly since no child was spawned.
	if ok, activeID := s.tryAcquireBuildSlot(id); !ok {
		writeError(w, http.StatusConflict, "BUILD_IN_PROGRESS",
			fmt.Sprintf("a build is already in progress (%s); only one compose runs at a time", activeID))
		return
	}

	buildRoot := filepath.Join(s.cfg.WorkDir, "builds", id)
	workDir := filepath.Join(buildRoot, "work")
	cacheDir := filepath.Join(buildRoot, "cache")
	createdAt := time.Now()
	// 0700: build logs and artifact metadata may be sensitive; keep them private.
	for _, d := range []string{workDir, cacheDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			s.releaseBuildSlot(id) // no child spawned; free the slot for the next start
			writeError(w, http.StatusInternalServerError, "WORKDIR", "cannot create build work directory")
			return
		}
	}

	// Resolve the template path to build. Client errors (bad input, no match)
	// return 400; server errors (e.g. writing the inline template) return 500.
	templatePath, templateName, err := s.resolveBuildTemplate(&req, workDir)
	if err != nil {
		s.releaseBuildSlot(id) // no child spawned; free the slot for the next start
		if errors.Is(err, errBadBuildRequest) {
			writeError(w, http.StatusBadRequest, "NO_MATCH", err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, "TEMPLATE_RESOLVE", err.Error())
		}
		return
	}

	// Build the image summary. Best-effort: a merge failure here doesn't block
	// the build (compose already validated the template; this is for display only).
	var summary *composeSummary
	if req.Compose != nil {
		if merged, err := config.LoadAndMergeTemplate(templatePath); err == nil {
			s := buildComposeSummary(*req.Compose, merged)
			summary = &s
		}
	}

	name, cmdArgs := s.buildCommand(templatePath, workDir, cacheDir)
	b := &build{
		ID:           id,
		status:       statusRunning,
		RootDir:      buildRoot,
		WorkDir:      workDir,
		CacheDir:     cacheDir,
		Template:     templateName,
		TemplatePath: templatePath,
		Command:      name + " " + strings.Join(cmdArgs, " "),
		Summary:      summary,
		CreatedAt:    createdAt,
		done:         make(chan struct{}),
	}
	s.tracker.add(b)

	// Persist the initial record so the build shows in history immediately (and
	// survives a restart mid-build as a stale "running" entry).
	if err := b.writeMeta(); err != nil {
		logger.Logger().Warnf("build %s: writing initial meta: %v", id, err)
	}

	go s.runBuild(b, name, cmdArgs)

	writeJSON(w, http.StatusAccepted, buildAccepted{
		BuildID: id,
		Status:  string(statusRunning),
		LogsURL: fmt.Sprintf("/api/v1/builds/%s/logs", id),
	})
}

// cancelAccepted is the response to a successful cancel request. The build is
// now cancelling; the terminal state (cancelled/failed) arrives later over SSE
// once the ICT child has torn down and exited.
type cancelAccepted struct {
	BuildID string `json:"buildId"`
	Status  string `json:"status"`
}

// handleCancelBuild transitions a running build to cancelling and signals the
// ICT process group with SIGTERM, letting ICT perform its own teardown (mounts,
// loop devices) and exit. We do not reimplement a mount/loop reconciler here.
//
// Terminal classification and the single-build slot release happen on runBuild's
// wait path, not here: this handler only requests the cancel and returns 202.
func (s *Server) handleCancelBuild(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	b, ok := s.getBuild(id)
	if !ok {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "build not found")
		return
	}

	transitioned, pgid := b.beginCancel()
	if !transitioned {
		// Already terminal, or a cancel is already in flight. 409 so the caller
		// knows the request didn't change anything.
		writeError(w, http.StatusConflict, "NOT_CANCELLABLE",
			"build is not running (already finished or cancelling)")
		return
	}
	b.appendLog("• cancel requested — signalling build to stop and clean up")

	if err := s.signalCancel(pgid); err != nil {
		// The signal never reached the process group, so ICT may never start its
		// teardown. Record a cancellation-failure with the underlying error so the
		// UI can tell the user the kill itself failed (distinct from ICT running
		// but leaving residue). The build stays in cancelling; if the child exits
		// on its own the wait path still classifies it, otherwise it will hang as
		// cancelling — which correctly reflects that we lost control of it.
		detail := fmt.Sprintf("failed to signal build process group %d: %v", pgid, err)
		b.setResidual(residualCancellation, detail)
		b.appendLog("ERROR " + detail)
		logger.Logger().Errorf("build %s: %s", id, detail)
		writeJSON(w, http.StatusAccepted, cancelAccepted{BuildID: id, Status: string(statusCancelling)})
		return
	}

	writeJSON(w, http.StatusAccepted, cancelAccepted{BuildID: id, Status: string(statusCancelling)})
}

// signalCancel delivers SIGTERM to the build's process group (negative pid).
// The child is its own group leader (Setpgid at spawn), so -pgid reaches the
// whole tree (sudo → ICT → helpers), giving ICT the chance to tear down mounts
// and loop devices before exiting.
//
// Cross-sudo caveat: the server runs non-root and the build runs under `sudo`,
// so the process group is root-owned. A non-root process can't signal it, so
// under --sudo we deliver the signal as root via `sudo -n kill`. This requires a
// scoped sudoers rule for the kill (see serve.go's --sudo help). Without --sudo
// (dev/root), we signal the group directly.
func (s *Server) signalCancel(pgid int) error {
	if pgid <= 0 {
		return fmt.Errorf("no process group recorded for build")
	}
	if s.cfg.Sudo {
		// `kill -TERM -<pgid>`: the leading `-` on the pid makes kill target the
		// process group. -n never prompts; a missing sudoers rule fails fast.
		out, err := exec.Command("sudo", "-n", "kill", "-TERM", fmt.Sprintf("-%d", pgid)).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	// Direct group signal (dev/root): negative pid = process group.
	return syscall.Kill(-pgid, syscall.SIGTERM)
}

// errBadBuildRequest marks resolution failures caused by client input (bad
// request shape or an unmatched combination) so the handler can return 400;
// other errors are treated as server-side (500).
var errBadBuildRequest = errors.New("bad build request")

// resolveBuildTemplate returns the on-disk template path to build. For {compose}
// it looks up the manifest; for {yaml} it writes the body to the work dir.
func (s *Server) resolveBuildTemplate(req *buildRequest, workDir string) (path, name string, err error) {
	if req.YAML != "" {
		p := filepath.Join(workDir, "template.yml")
		if werr := os.WriteFile(p, []byte(req.YAML), 0o600); werr != nil {
			return "", "", fmt.Errorf("writing template: %w", werr) // server-side
		}
		return p, "template.yml", nil
	}
	if req.Compose == nil {
		return "", "", fmt.Errorf("%w: provide either compose or yaml", errBadBuildRequest)
	}
	c := req.Compose
	tmpl := s.manifest.findTemplate(c.Vertical, c.SKU, c.Platform, c.OS, c.ImageType)
	if tmpl == "" {
		return "", "", fmt.Errorf("%w: no template maps to the selected combination", errBadBuildRequest)
	}
	full, perr := safeTemplatePath(s.cfg.TemplatesDir, tmpl)
	if perr != nil {
		return "", "", fmt.Errorf("resolving template path: %w", perr) // server-side (bad manifest)
	}
	return full, tmpl, nil
}

// buildCommand assembles the argv for an ICT build, prefixing sudo when
// configured (ICT builds require root for chroot/mount operations).
//
// This intentionally uses os/exec directly rather than internal/utils/shell:
// the shell package runs commands synchronously via `bash -c` and returns the
// captured output as a string, whereas the API needs to hold the *exec.Cmd to
// stream stdout line-by-line to SSE subscribers and (in the cancellation story)
// signal the process group. The command surface here is fixed and minimal — a
// single hard-coded `image-composer-tool build` invocation with no user-derived
// arguments on the command line (selections are only manifest lookup keys) — so
// the allowlist the shell package provides adds no safety here.
func (s *Server) buildCommand(templatePath, workDir, cacheDir string) (name string, args []string) {
	// Per-build --work-dir and --cache-dir keep each build's scratch and package
	// cache isolated under the build root, so concurrent/repeat builds don't share
	// (root-owned) state and cleanup is a single directory removal.
	ictArgs := []string{"build", templatePath, "--work-dir", workDir, "--cache-dir", cacheDir}
	if s.cfg.Sudo {
		// -n: never prompt; fail fast if passwordless sudo isn't configured.
		return "sudo", append([]string{"-n", s.cfg.ICTBinary}, ictArgs...)
	}
	return s.cfg.ICTBinary, ictArgs
}

// runBuild executes the ICT binary, streams its output into the build's log
// buffer, and records the terminal status + artifacts.
func (s *Server) runBuild(b *build, name string, cmdArgs []string) {
	log := logger.Logger()
	// Release the single-build slot only once this function returns — which
	// happens after cmd.Wait() observes the ICT child fully exit (post-teardown),
	// or after a failed cmd.Start where no child was spawned. Either way the next
	// compose can't start while mounts/loop devices may still be tearing down.
	// Ordered to run after close(b.done) fires (defers run LIFO), so a waiter that
	// unblocks on b.done and immediately re-POSTs still sees the slot as taken
	// until this returns.
	defer s.releaseBuildSlot(b.ID)
	defer close(b.done)

	// ICT builds require root (chroot, mounts), so the build runs under sudo, and
	// from the repo root since ICT resolves config/osv/... relative to cwd. The
	// per-build --work-dir/--cache-dir keep outputs isolated.
	//
	// Echo the exact command so the operator can reproduce it on the shell. This
	// is both logged server-side and pushed as the first build-log line so it
	// surfaces in the UI's Build page over SSE.
	log.Infof("build %s command: %s", b.ID, b.Command)
	b.appendLog("$ " + b.Command)

	cmd := exec.Command(name, cmdArgs...)
	// Run the build in its own process group so a later cancel can signal the
	// whole child tree (sudo → ICT → helpers) with a single kill to -pgid, not
	// just the immediate child.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Merge stdout+stderr into a single stream via one pipe writer shared by both
	// fds. os/exec guards concurrent writes to the same *os.File writer with a
	// lock, so interleaving is safe — unlike assigning cmd.Stderr = cmd.Stdout,
	// which hands both fds the same pipe end and races. A pipe (not
	// CombinedOutput) is required so logs stream line-by-line to SSE as the build
	// runs rather than buffering until it exits.
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		b.appendLog(fmt.Sprintf("failed to start build: %v", err))
		b.finish(statusFailed, nil, err.Error())
		_ = pw.Close()
		_ = pr.Close()
		return
	}
	// Record the process-group id now that the child is running. With Setpgid the
	// child is its own group leader, so the group id equals its pid. A cancel
	// signals -pgid to reach the whole tree.
	//
	// A cancel can arrive in the window between the build being tracked as running
	// (in handleStartBuild) and this point, when pgid was still 0. beginCancel
	// would have transitioned to cancelling but signalCancel(0) could not signal
	// anything. setPgidCheckCancel records the pgid and, atomically under b.mu,
	// reports whether such an early cancel is pending so we can deliver the signal
	// now that we finally have a group to target.
	if pgid, pending := b.setPgidCheckCancel(cmd.Process.Pid); pending {
		if err := s.signalCancel(pgid); err != nil {
			detail := fmt.Sprintf("failed to signal build process group %d: %v", pgid, err)
			b.setResidual(residualCancellation, detail)
			b.appendLog("ERROR " + detail)
			log.Errorf("build %s: %s", b.ID, detail)
		} else {
			b.appendLog("• cancel requested before start completed — signalling now")
		}
	}
	// Wait for the process in a goroutine and close the pipe writer when it
	// exits, so the scanner below sees EOF. The exit error is delivered on
	// waitCh (cmd.Wait is called exactly once, here).
	waitCh := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		_ = pw.Close()
		waitCh <- err
	}()

	scanner := bufio.NewScanner(pr)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		b.appendLog(scanner.Text())
	}
	// A scanner error (read failure, or a token exceeding the buffer) means the
	// captured log is truncated; record it so a build isn't silently marked
	// successful with incomplete output.
	scanErr := scanner.Err()
	if scanErr != nil {
		b.appendLog(fmt.Sprintf("warning: build log stream error: %v", scanErr))
	}

	waitErr := <-waitCh
	if waitErr != nil {
		// Classify the terminal state. A cancel was requested → the child was
		// signalled, so map the signal-terminated / signal-code exits to cancelled;
		// otherwise a non-zero exit is a genuine build failure. We gate on
		// cancelRequested (not the code alone) because we wait on `sudo`, not ICT
		// directly, and sudo doesn't always translate the child's exit cleanly.
		status := classifyExit(waitErr, b.wasCancelRequested())
		log.Warnf("build %s ended (%s): %v", b.ID, status, waitErr)
		// If ICT ran its own teardown but reported leftover mounts/loop devices,
		// surface that as a cleanup-failure so the UI can point the user at manual
		// remediation. (A cancellation-failure — the signal itself failing — is
		// recorded earlier, in the cancel handler, and takes precedence.)
		if detail := scanResidualCleanup(b.snapshotLogs()); detail != "" {
			b.setResidual(residualCleanup, detail)
		}
		// Surface any partial outputs left on disk so the UI can point at their
		// location on a failed or cancelled compose.
		partial := discoverArtifacts(b.WorkDir)
		b.finish(status, partial, waitErr.Error())
		return
	}
	if scanErr != nil {
		b.finish(statusFailed, nil, fmt.Sprintf("log stream error: %v", scanErr))
		return
	}

	// Prefer artifacts parsed from ICT's own output (authoritative name+path,
	// and immune to the root-owned output dirs the build creates under sudo).
	// Fall back to scanning the work dir if the output block wasn't found.
	arts := parseArtifacts(b.snapshotLogs())
	if len(arts) == 0 {
		arts = discoverArtifacts(b.WorkDir)
	}

	b.finish(statusSuccess, arts, "")
}

// finish records the build's terminal status, artifacts, and error under lock,
// persists the buffered logs to a file (so past builds can offer a log download
// without keeping logs in memory), then writes meta.json so the compose history
// reflects the final outcome across restarts.
func (b *build) finish(status buildStatus, arts []artifact, errMsg string) {
	b.mu.Lock()
	b.status = status
	b.artifacts = arts
	b.errMsg = errMsg
	b.mu.Unlock()

	// Persist logs to <root>/compose.log for later download.
	if b.RootDir != "" {
		logPath := filepath.Join(b.RootDir, "compose.log")
		content := strings.Join(b.snapshotLogs(), "\n")
		if err := os.WriteFile(logPath, []byte(content), 0o600); err != nil {
			logger.Logger().Warnf("build %s: writing log file: %v", b.ID, err)
		} else {
			b.mu.Lock()
			b.LogFile = logPath
			b.mu.Unlock()
		}
	}

	if err := b.writeMeta(); err != nil {
		logger.Logger().Warnf("build %s: writing final meta: %v", b.ID, err)
	}
}

// exitCode returns the process exit code carried by a cmd.Wait error, or -1 if
// the error is not an *exec.ExitError (e.g. the process was never started).
func exitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// classifyExit maps a non-nil cmd.Wait error to a terminal status. When a cancel
// was requested, an exit consistent with signal termination is treated as
// cancelled; otherwise every non-zero exit is a failure.
//
// We watch a set of codes rather than only 130 because we wait on `sudo`, not
// ICT directly: ICT's own SIGINT handler exits 130, but if it (or sudo) is
// terminated by the signal instead we see 143 (128+SIGTERM) or 137 (128+SIGKILL),
// and a signal-terminated child reports via WaitStatus.Signaled() with no code
// at all. Any of these, under a requested cancel, is a clean cancellation.
func classifyExit(waitErr error, cancelRequested bool) buildStatus {
	if !cancelRequested {
		return statusFailed
	}
	switch exitCode(waitErr) {
	case 130, 143, 137: // 128 + SIGINT / SIGTERM / SIGKILL
		return statusCancelled
	}
	// A child killed by a signal (not exiting with a code) surfaces as Signaled().
	var ee *exec.ExitError
	if errors.As(waitErr, &ee) {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return statusCancelled
		}
	}
	// Cancel was requested but the child failed for an unrelated reason before the
	// signal took effect — report the real failure.
	return statusFailed
}

// scanResidualCleanup looks for ICT's residual-cleanup ERROR lines — logged when
// its teardown couldn't remove all mounts/loop devices, with a `mount | grep` /
// `losetup -l` remediation hint. It returns the joined matching lines (trimmed of
// the logger prefix) as the remediation detail, or "" if teardown was clean.
func scanResidualCleanup(logs []string) string {
	var hits []string
	for _, line := range logs {
		low := strings.ToLower(line)
		if !strings.Contains(low, "error") {
			continue
		}
		// Match the residual-cleanup signature: an error line that mentions leftover
		// mounts / loop devices or the remediation commands ICT prints.
		if strings.Contains(low, "losetup") ||
			strings.Contains(low, "mount | grep") ||
			strings.Contains(low, "leftover") ||
			strings.Contains(low, "residual") ||
			(strings.Contains(low, "loop") && strings.Contains(low, "device")) {
			hits = append(hits, strings.TrimSpace(stripLogPrefix(line)))
		}
	}
	return strings.Join(hits, "\n")
}

// stripLogPrefix trims the logger's leading "<ts>\t<LEVEL>\t<source>\t" fields
// from a log line, leaving the message. The prefix is exactly three tab-
// separated fields, so we split off the first three and keep the remainder
// verbatim — including any tabs inside the message itself (LastIndex would drop
// everything up to the message's own last tab). Falls back to the whole line
// when the line has fewer than three tabs (not logger-formatted).
func stripLogPrefix(line string) string {
	if parts := strings.SplitN(line, "\t", 4); len(parts) == 4 {
		return parts[3]
	}
	return line
}

// parseArtifacts extracts the artifact list from ICT's build output. ICT prints
// each artifact as a bullet line with "name (size)" followed by a line holding
// the absolute path:
//
//   - minimal-os-image-ubuntu-26.04.raw.gz (1.13 GB)
//     /home/.../minimal/minimal-os-image-ubuntu-26.04.raw.gz
//
// Log lines carry a leading "<timestamp> INFO ..." prefix from the logger, so we
// match on the bullet and on a path segment rather than line position.
func parseArtifacts(logs []string) []artifact {
	var out []artifact
	var pending *artifact // artifact awaiting its path line

	for _, line := range logs {
		if idx := strings.Index(line, "• "); idx >= 0 {
			rest := strings.TrimSpace(line[idx+len("• "):])
			// Split a trailing " (size)" suffix: keep the filename and the size.
			// ICT prints sizes like "0.01 MB"; normalize so small files show in KB.
			name, size := rest, ""
			if p := strings.LastIndex(rest, " ("); p >= 0 && strings.HasSuffix(rest, ")") {
				name = strings.TrimSpace(rest[:p])
				size = normalizeSize(strings.TrimSpace(rest[p+2 : len(rest)-1]))
			}
			if name != "" {
				out = append(out, artifact{Name: name, Type: classifyArtifact(name), Size: size})
				pending = &out[len(out)-1]
			}
			continue
		}
		// The path line for the most recent bullet: an absolute path ending in
		// that artifact's name.
		if pending != nil {
			if p := extractPath(line); p != "" && strings.HasSuffix(p, pending.Name) {
				pending.Path = p
				pending = nil
			}
		}
	}
	return out
}

// extractPath returns the trailing absolute path on a log line, or "".
func extractPath(line string) string {
	// Logger lines are tab-separated: "<ts>\t<LEVEL>\t<source:line>\t<message>".
	// The path lives in the final field; taking the first "/" would wrongly match
	// the "display/display.go:80" source prefix. Fall back to the whole line when
	// there are no tabs (a bare path line).
	if i := strings.LastIndex(line, "\t"); i >= 0 {
		line = line[i+1:]
	}
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "/") {
		return ""
	}
	return line
}

// classifyArtifact labels an output file as "sbom" or "image" by name.
func classifyArtifact(name string) string {
	lower := strings.ToLower(name)
	if strings.Contains(lower, "sbom") || strings.Contains(lower, "spdx") {
		return "sbom"
	}
	return "image"
}

// discoverArtifacts scans the build work dir for image + SBOM outputs. Used as a
// fallback when ICT's artifact block cannot be parsed from the logs.
func discoverArtifacts(workDir string) []artifact {
	var out []artifact
	_ = filepath.WalkDir(workDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		lower := strings.ToLower(name)
		var typ string
		switch {
		case strings.Contains(lower, "sbom") || strings.HasSuffix(lower, ".spdx.json"):
			typ = "sbom"
		case strings.HasSuffix(lower, ".raw"), strings.HasSuffix(lower, ".raw.gz"),
			strings.HasSuffix(lower, ".iso"), strings.HasSuffix(lower, ".qcow2"):
			typ = "image"
		default:
			return nil
		}
		size := ""
		if fi, statErr := d.Info(); statErr == nil {
			size = humanSize(fi.Size())
		}
		out = append(out, artifact{Name: name, Type: typ, Path: path, Size: size})
		return nil
	})
	return out
}

// humanSize formats a byte count as a short human-readable string, choosing the
// largest unit under which the value is >= 1 (e.g. 12 KB, 4.3 MB, 1.13 GB).
func humanSize(n int64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	val := float64(n) / float64(div)
	// Whole KB read better without a trailing ".00" (e.g. "12 KB" not "12.00 KB").
	if exp == 0 && val == float64(int64(val)) {
		return fmt.Sprintf("%d KB", int64(val))
	}
	return fmt.Sprintf("%.2f %cB", val, "KMGTPE"[exp])
}

// normalizeSize re-formats a size string that ICT already printed (e.g.
// "0.01 MB", "1.13 GB", "512 B") into a sensible unit, so tiny artifacts don't
// show as "0.01 MB". Unparseable input is returned unchanged.
func normalizeSize(s string) string {
	fields := strings.Fields(s)
	if len(fields) != 2 {
		return s
	}
	val, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return s
	}
	mult := map[string]float64{
		"B": 1, "KB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12,
		"KIB": 1024, "MIB": 1 << 20, "GIB": 1 << 30, "TIB": 1 << 40,
	}
	m, ok := mult[strings.ToUpper(fields[1])]
	if !ok {
		return s
	}
	return humanSize(int64(val * m))
}
