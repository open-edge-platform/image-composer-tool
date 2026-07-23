// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"bufio"
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
	"time"

	"github.com/google/uuid"
	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
)

// BuildStatus is the lifecycle state of a build.
type BuildStatus string

const (
	StatusRunning BuildStatus = "running"
	StatusSuccess BuildStatus = "success"
	StatusFailed  BuildStatus = "failed"
)

// Artifact describes one output file (image or SBOM).
type Artifact struct {
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
	Summary      *ComposeSummary // image configuration summary, nil for YAML builds
	CreatedAt    time.Time       // when the compose was started
	LogFile      string          // on-disk log file path, written at finish
	done         chan struct{}   // closed when the build finishes

	mu        sync.Mutex
	status    BuildStatus
	logLines  []string // buffered log history for late log subscribers
	artifacts []Artifact
	errMsg    string
}

// Result is an immutable snapshot of a build's terminal state.
type Result struct {
	Status    BuildStatus
	Artifacts []Artifact
	ErrMsg    string
	LogFile   string
}

// snapshot returns the build's current status, artifacts, error, and log-file
// path under lock. logFile is included in the snapshot because it is written
// under b.mu in finish(); reading it directly off the struct from a handler
// would race a concurrently finishing build.
func (b *build) snapshot() Result {
	b.mu.Lock()
	defer b.mu.Unlock()
	arts := make([]Artifact, len(b.artifacts))
	copy(arts, b.artifacts)
	return Result{Status: b.status, Artifacts: arts, ErrMsg: b.errMsg, LogFile: b.LogFile}
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

// BuildRequest starts a build. It carries exactly one of Compose (Basic-tab
// selections) or YAML (the complete edited template from the Advanced tab).
type BuildRequest struct {
	Compose *Selection
	YAML    string
}

// BuildAccepted is returned when a build is accepted and started.
type BuildAccepted struct {
	BuildID string
	Status  BuildStatus
	LogsURL string
}

// StartBuild resolves the template, starts an os/exec build, and returns a build
// id. Client errors (bad input, no match) return a 400 *Error; server errors
// (workspace creation, template write) return a 500 *Error.
func (s *Service) StartBuild(req BuildRequest) (*BuildAccepted, error) {
	id := uuid.NewString()
	buildRoot := filepath.Join(s.cfg.WorkDir, "builds", id)
	workDir := filepath.Join(buildRoot, "work")
	cacheDir := filepath.Join(buildRoot, "cache")
	createdAt := time.Now()
	// 0700: build logs and artifact metadata may be sensitive; keep them private.
	for _, d := range []string{workDir, cacheDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, newError(http.StatusInternalServerError, "WORKDIR",
				"cannot create build work directory")
		}
	}

	// Resolve the template path to build. Client errors (bad input, no match)
	// return 400; server errors (e.g. writing the inline template) return 500.
	templatePath, templateName, err := s.resolveBuildTemplate(&req, workDir)
	if err != nil {
		if errors.Is(err, errBadBuildRequest) {
			return nil, newError(http.StatusBadRequest, "NO_MATCH", err.Error())
		}
		return nil, newError(http.StatusInternalServerError, "TEMPLATE_RESOLVE", err.Error())
	}

	// Build the image summary. Best-effort: a merge failure here doesn't block
	// the build (compose already validated the template; this is for display only).
	var summary *ComposeSummary
	if req.Compose != nil {
		if merged, err := config.LoadAndMergeTemplate(templatePath); err == nil {
			sum := buildComposeSummary(*req.Compose, merged)
			summary = &sum
		}
	}

	name, cmdArgs := s.buildCommand(templatePath, workDir, cacheDir)
	b := &build{
		ID:           id,
		status:       StatusRunning,
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

	return &BuildAccepted{
		BuildID: id,
		Status:  StatusRunning,
		LogsURL: fmt.Sprintf("/api/v1/builds/%s/logs", id),
	}, nil
}

// errBadBuildRequest marks resolution failures caused by client input (bad
// request shape or an unmatched combination) so StartBuild can return 400;
// other errors are treated as server-side (500).
var errBadBuildRequest = errors.New("bad build request")

// resolveBuildTemplate returns the on-disk template path to build. For {yaml} it
// writes the body to the work dir; for {compose} it looks up the manifest.
func (s *Service) resolveBuildTemplate(req *BuildRequest, workDir string) (path, name string, err error) {
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
func (s *Service) buildCommand(templatePath, workDir, cacheDir string) (name string, args []string) {
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
func (s *Service) runBuild(b *build, name string, cmdArgs []string) {
	log := logger.Logger()
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
		b.finish(StatusFailed, nil, err.Error())
		_ = pw.Close()
		_ = pr.Close()
		return
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
		log.Warnf("build %s failed: %v", b.ID, waitErr)
		b.finish(StatusFailed, nil, waitErr.Error())
		return
	}
	if scanErr != nil {
		b.finish(StatusFailed, nil, fmt.Sprintf("log stream error: %v", scanErr))
		return
	}

	// Prefer artifacts parsed from ICT's own output (authoritative name+path,
	// and immune to the root-owned output dirs the build creates under sudo).
	// Fall back to scanning the work dir if the output block wasn't found.
	arts := parseArtifacts(b.snapshotLogs())
	if len(arts) == 0 {
		arts = discoverArtifacts(b.WorkDir)
	}

	b.finish(StatusSuccess, arts, "")
}

// finish records the build's terminal status, artifacts, and error under lock,
// persists the buffered logs to a file (so past builds can offer a log download
// without keeping logs in memory), then writes meta.json so the compose history
// reflects the final outcome across restarts.
func (b *build) finish(status BuildStatus, arts []Artifact, errMsg string) {
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

// parseArtifacts extracts the artifact list from ICT's build output. ICT prints
// each artifact as a bullet line with "name (size)" followed by a line holding
// the absolute path:
//
//   - minimal-os-image-ubuntu-26.04.raw.gz (1.13 GB)
//     /home/.../minimal/minimal-os-image-ubuntu-26.04.raw.gz
//
// Log lines carry a leading "<timestamp> INFO ..." prefix from the logger, so we
// match on the bullet and on a path segment rather than line position.
func parseArtifacts(logs []string) []Artifact {
	var out []Artifact
	var pending *Artifact // artifact awaiting its path line

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
				out = append(out, Artifact{Name: name, Type: classifyArtifact(name), Size: size})
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
func discoverArtifacts(workDir string) []Artifact {
	var out []Artifact
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
		out = append(out, Artifact{Name: name, Type: typ, Path: path, Size: size})
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
