// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"context"
	"errors"
	"io"
	"io/fs"
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
	// drained records that the caller read the stream to EOF, so the child's
	// exit status reflects the transfer rather than a caller-side early close.
	drained bool
}

func (a *artifactReadCloser) Read(p []byte) (int, error) {
	n, err := a.ReadCloser.Read(p)
	if errors.Is(err, io.EOF) {
		a.drained = true
	}
	return n, err
}

// Close closes the pipe and reaps the child, surfacing the child's exit status
// so a mid-stream failure (e.g. sudo denied by config, or cat hitting an I/O
// error after the header was already written) is observable to the caller
// rather than silently truncating the download. The pipe-close error takes
// precedence; the Wait error is only returned when closing itself succeeded.
//
// Closing the pipe before the child exits makes `cat` see EPIPE and die with a
// non-zero status, so an early caller-side close (client disconnect) would
// otherwise report a spurious error. We therefore ignore Wait's error when the
// stream was not read to completion.
func (a *artifactReadCloser) Close() error {
	err := a.ReadCloser.Close()
	werr := a.cmd.Wait()
	if err != nil {
		return err
	}
	if werr != nil && a.drained {
		return werr
	}
	return nil
}

// underDir reports whether path is dir itself or lives beneath it. Both
// arguments must already be absolute; comparison is lexical, so callers that
// care about symlinks must resolve them first.
func underDir(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

// OpenArtifact resolves a build artifact by name and returns a reader for its
// bytes plus the base filename to offer as the download name.
//
// The artifact must be in the build's recorded artifact list, and its path must
// resolve inside the build's work directory both lexically and after symlink
// resolution — arbitrary paths are rejected with a 403 *Error (artifact paths
// come from log parsing, so a poisoned entry must not escape the workspace).
// Unknown build/artifact, or a recorded artifact that has since been deleted,
// return a 404 *Error.
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
	if aerr != nil || werr != nil || !underDir(absArtifact, absWorkDir) {
		return nil, "", newError(http.StatusForbidden, "FORBIDDEN", "artifact path outside build workspace")
	}

	// The lexical check above can be defeated by a symlink inside the workspace
	// pointing outside it, so re-check the fully symlink-resolved path. Both sides
	// must be resolved: the work dir itself may sit under a symlinked parent (e.g.
	// /tmp -> /private/tmp), which would otherwise make every artifact look like
	// an escape.
	//
	// EvalSymlinks needs traverse permission on every path component. Under
	// --sudo, ICT creates the nested output dirs as root:root 0700, so the
	// unprivileged server gets EACCES here on exactly the paths it is meant to
	// serve. A permission error therefore is not evidence of an escape — we fall
	// back to the lexical result. A non-existent path is reported as 404: the
	// artifact was recorded at build time but has since been removed.
	resolvedArtifact, rerr := filepath.EvalSymlinks(absArtifact)
	switch {
	case rerr == nil:
		// The work dir is server-owned, so this resolve is expected to succeed;
		// if it doesn't, fail closed rather than trusting the lexical check.
		resolvedWorkDir, rwerr := filepath.EvalSymlinks(absWorkDir)
		if rwerr != nil || !underDir(resolvedArtifact, resolvedWorkDir) {
			return nil, "", newError(http.StatusForbidden, "FORBIDDEN", "artifact path outside build workspace")
		}
	case errors.Is(rerr, fs.ErrNotExist):
		return nil, "", newError(http.StatusNotFound, "NOT_FOUND", "artifact file no longer exists")
	case errors.Is(rerr, fs.ErrPermission):
		// Root-owned build output; the lexical guard above stands.
	default:
		return nil, "", newError(http.StatusForbidden, "FORBIDDEN", "cannot verify artifact path")
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
