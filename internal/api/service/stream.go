// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// BuildHandle is a read-only view of a tracked (or disk-hydrated) build, handed
// to the api layer's streaming and file-download handlers. Those endpoints don't
// fit the generated JSON ServerInterface (SSE, YAML/text/binary downloads), so
// they live in the api package but reach build state only through this handle —
// the internal *build type stays unexported.
type BuildHandle struct {
	svc *Service
	b   *build
}

// Build returns a handle to a build by id (live or hydrated from disk), or false
// when neither exists.
func (s *Service) Build(id string) (*BuildHandle, bool) {
	b, ok := s.getBuild(id)
	if !ok {
		return nil, false
	}
	return &BuildHandle{svc: s, b: b}, true
}

// SnapshotLogs returns a copy of the build's buffered log lines.
func (h *BuildHandle) SnapshotLogs() []string { return h.b.snapshotLogs() }

// Done returns a channel closed when the build finishes. For disk-hydrated
// (already-complete) builds it is closed immediately.
func (h *BuildHandle) Done() <-chan struct{} { return h.b.done }

// Result returns an immutable snapshot of the build's status, artifacts, error,
// and log-file path.
func (h *BuildHandle) Result() Result { return h.b.snapshot() }

// TemplateFile returns the on-disk path and display name of the resolved
// template that was built, for the template-download handler. Returns a 404
// *Error when no template was recorded for the build.
func (h *BuildHandle) TemplateFile() (path, name string, err error) {
	if h.b.TemplatePath == "" {
		return "", "", newError(http.StatusNotFound, "NOT_FOUND", "no template recorded for this build")
	}
	name = h.b.Template
	if name == "" {
		name = filepath.Base(h.b.TemplatePath)
	}
	return h.b.TemplatePath, name, nil
}

// LogFilePath returns the persisted compose-log path and whether it exists on
// disk. The path is read from a snapshot (finish() writes it under the build
// lock), so this is safe against a concurrently finishing build.
func (h *BuildHandle) LogFilePath() (string, bool) {
	logFile := h.b.snapshot().LogFile
	if logFile == "" || !fileExists(logFile) {
		return "", false
	}
	return logFile, true
}

// artifactReadCloser wraps the sudo `cat` stream so Close reaps the child
// process after the response has been written.
type artifactReadCloser struct {
	io.ReadCloser
	cmd *exec.Cmd
}

func (a *artifactReadCloser) Close() error {
	err := a.ReadCloser.Close()
	_ = a.cmd.Wait()
	return err
}

// OpenArtifact resolves a build artifact by name and returns a reader for its
// bytes plus the base filename to offer as the download name.
//
// The artifact must be in the build's recorded artifact list, and its path must
// resolve inside the build's work directory — arbitrary paths are rejected with
// a 403 *Error (artifact paths come from log parsing, so a poisoned entry must
// not escape the workspace). Unknown build/artifact return a 404 *Error.
//
// Artifact files are owned by root (ICT builds run under sudo). When sudo is
// configured we stream via `sudo -n cat`; otherwise we read directly (dev env).
func (s *Service) OpenArtifact(ctx context.Context, id, name string) (io.ReadCloser, string, error) {
	b, ok := s.getBuild(id)
	if !ok {
		return nil, "", newError(http.StatusNotFound, "NOT_FOUND", "build not found")
	}
	res := b.snapshot()
	var artifactPath string
	for _, a := range res.Artifacts {
		if a.Name == name {
			artifactPath = a.Path
			break
		}
	}
	if artifactPath == "" {
		return nil, "", newError(http.StatusNotFound, "NOT_FOUND", "artifact not found")
	}

	// Guard against a poisoned artifact entry escaping the per-build workspace.
	// Resolve both to absolute paths first: b.WorkDir is relative when the server
	// runs with a relative --work-dir (the default), while artifact paths are
	// absolute — a raw HasPrefix would then always fail.
	absArtifact, aerr := filepath.Abs(artifactPath)
	absWorkDir, werr := filepath.Abs(b.WorkDir)
	if aerr != nil || werr != nil ||
		(absArtifact != absWorkDir && !strings.HasPrefix(absArtifact, absWorkDir+string(filepath.Separator))) {
		return nil, "", newError(http.StatusForbidden, "FORBIDDEN", "artifact path outside build workspace")
	}

	filename := filepath.Base(artifactPath)

	if s.cfg.Sudo {
		// Stream via `sudo cat` so large ISOs don't require buffering the whole
		// file in memory. StdoutPipe gives us a reader we can copy directly to the
		// response writer, chunk by chunk.
		//
		// No `--` guard is needed: artifactPath is always an absolute,
		// filepath.Clean'd path validated to live under the build work dir (never
		// a `-`-prefixed string). Passing `--` would also add a second argument
		// that a scoped `cat <path-glob>` sudoers rule wouldn't match.
		cmd := exec.CommandContext(ctx, "sudo", "-n", "cat", artifactPath)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, "", newError(http.StatusInternalServerError, "ARTIFACT_STREAM", "failed to open artifact stream")
		}
		if err := cmd.Start(); err != nil {
			return nil, "", newError(http.StatusInternalServerError, "ARTIFACT_READ", "failed to read artifact")
		}
		return &artifactReadCloser{ReadCloser: stdout, cmd: cmd}, filename, nil
	}

	f, err := os.Open(artifactPath)
	if err != nil {
		return nil, "", newError(http.StatusInternalServerError, "ARTIFACT_READ", "cannot read artifact file")
	}
	return f, filename, nil
}
