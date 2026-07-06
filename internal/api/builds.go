// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
)

// buildStatus is the lifecycle state of a build.
type buildStatus string

const (
	statusRunning buildStatus = "running"
	statusSuccess buildStatus = "success"
	statusFailed  buildStatus = "failed"
)

// artifact describes one output file (image or SBOM).
type artifact struct {
	Name string `json:"name"`
	Type string `json:"type"` // "image" | "sbom"
	Path string `json:"path"`
}

// build is the in-memory record of a single build (MVP-1: no persistence).
type build struct {
	ID       string
	Status   buildStatus
	WorkDir  string
	Template string

	mu        sync.Mutex
	logLines  []string      // buffered log history for late log subscribers
	done      chan struct{} // closed when the build finishes
	artifacts []artifact
	errMsg    string
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
	workDir := filepath.Join(s.cfg.WorkDir, "builds", id)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "WORKDIR", "cannot create build work directory")
		return
	}

	// Resolve the template path to build.
	templatePath, templateName, err := s.resolveBuildTemplate(&req, workDir)
	if err != nil {
		writeError(w, http.StatusBadRequest, "NO_MATCH", err.Error())
		return
	}

	b := &build{
		ID:       id,
		Status:   statusRunning,
		WorkDir:  workDir,
		Template: templateName,
		done:     make(chan struct{}),
	}
	s.tracker.add(b)

	go s.runBuild(b, templatePath)

	writeJSON(w, http.StatusAccepted, buildAccepted{
		BuildID: id,
		Status:  string(statusRunning),
		LogsURL: fmt.Sprintf("/api/v1/builds/%s/logs", id),
	})
}

// resolveBuildTemplate returns the on-disk template path to build. For {compose}
// it looks up the manifest; for {yaml} it writes the body to the work dir.
func (s *Server) resolveBuildTemplate(req *buildRequest, workDir string) (path, name string, err error) {
	if req.YAML != "" {
		p := filepath.Join(workDir, "template.yml")
		if werr := os.WriteFile(p, []byte(req.YAML), 0o644); werr != nil {
			return "", "", fmt.Errorf("writing template: %w", werr)
		}
		return p, "template.yml", nil
	}
	if req.Compose == nil {
		return "", "", fmt.Errorf("provide either compose or yaml")
	}
	c := req.Compose
	tmpl := s.manifest.findTemplate(c.Vertical, c.SKU, c.Platform, c.OS, c.ImageType)
	if tmpl == "" {
		return "", "", fmt.Errorf("no template maps to the selected combination")
	}
	return filepath.Join(s.cfg.TemplatesDir, tmpl), tmpl, nil
}

// buildCommand assembles the argv for an ICT build, prefixing sudo when
// configured (ICT builds require root for chroot/mount operations).
func (s *Server) buildCommand(templatePath, workDir string) (name string, args []string) {
	ictArgs := []string{"build", templatePath, "--work-dir", workDir}
	if s.cfg.Sudo {
		// -n: never prompt; fail fast if passwordless sudo isn't configured.
		return "sudo", append([]string{"-n", s.cfg.ICTBinary}, ictArgs...)
	}
	return s.cfg.ICTBinary, ictArgs
}

// runBuild executes the ICT binary, streams its output into the build's log
// buffer, and records the terminal status + artifacts.
func (s *Server) runBuild(b *build, templatePath string) {
	log := logger.Logger()
	defer close(b.done)

	// ICT builds require root (chroot, mounts), so the build runs under sudo, and
	// from the repo root since ICT resolves config/osv/... relative to cwd. The
	// per-build --work-dir keeps outputs isolated.
	name, cmdArgs := s.buildCommand(templatePath, b.WorkDir)
	cmd := exec.Command(name, cmdArgs...)
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = cmd.Stdout // merge streams

	if err := cmd.Start(); err != nil {
		b.appendLog(fmt.Sprintf("failed to start build: %v", err))
		b.setResult(statusFailed, err.Error())
		return
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		b.appendLog(scanner.Text())
	}

	if err := cmd.Wait(); err != nil {
		log.Warnf("build %s failed: %v", b.ID, err)
		b.setResult(statusFailed, err.Error())
		return
	}
	b.artifacts = discoverArtifacts(b.WorkDir)
	b.setResult(statusSuccess, "")
}

func (b *build) setResult(status buildStatus, errMsg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Status = status
	b.errMsg = errMsg
}

// discoverArtifacts scans the build work dir for image + SBOM outputs.
func discoverArtifacts(workDir string) []artifact {
	var out []artifact
	_ = filepath.WalkDir(workDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		lower := strings.ToLower(name)
		switch {
		case strings.Contains(lower, "sbom") || strings.HasSuffix(lower, ".spdx.json"):
			out = append(out, artifact{Name: name, Type: "sbom", Path: path})
		case strings.HasSuffix(lower, ".raw"), strings.HasSuffix(lower, ".raw.gz"),
			strings.HasSuffix(lower, ".iso"), strings.HasSuffix(lower, ".qcow2"):
			out = append(out, artifact{Name: name, Type: "image", Path: path})
		}
		return nil
	})
	return out
}
