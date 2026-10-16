// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
)

// buildMeta is the persisted record written to <build-root>/meta.json. It lets
// the compose history survive server restarts and be reconstructed from disk.
// The full compose log is persisted separately to <build-root>/compose.log at
// completion; meta.json records its path in LogFile so past builds can offer it
// for download alongside their configuration summary and artifacts.
type buildMeta struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Template string `json:"template"`
	// TemplatePath is the on-disk template the build ran against. Persisted so a
	// build reconstructed after a restart can still serve it for download and
	// report its location — without it TemplateFile() had nothing to resolve, and
	// the template download 404'd for every past build.
	TemplatePath string          `json:"templatePath,omitempty"`
	Command      string          `json:"command"`
	CreatedAt    time.Time       `json:"createdAt"`
	Summary      *ComposeSummary `json:"summary,omitempty"`
	Artifacts    []Artifact      `json:"artifacts,omitempty"`
	ErrMsg       string          `json:"errMsg,omitempty"`
	LogFile      string          `json:"logFile,omitempty"`
	// Residual outlives the process on purpose: leftover mounts and loop devices
	// survive a server restart, so the remediation hint must too.
	Residual *ResidualIssue `json:"residual,omitempty"`
	// Retry is what this build was resolved from (credentials/new-user passwords
	// already hashed), persisted so RetryBuild still works on a build reconstructed
	// after a restart instead of reporting it as predating retry support. Written
	// with the same 0600 meta.json carries everything else sensitive under.
	Retry *retryInput `json:"retry,omitempty"`
}

// metaPath returns the meta.json path for a build root directory.
func metaPath(rootDir string) string {
	return filepath.Join(rootDir, "meta.json")
}

// writeMeta persists the build's current state to <root>/meta.json. Best-effort:
// failures are returned for logging but never block a build. A build with no
// RootDir has nowhere to persist, so this is a no-op in that case.
//
// Serialized with every other persist for this build (see persistMu) so
// concurrent writers cannot interleave or land out of order.
func (b *build) writeMeta() error {
	b.persistMu.Lock()
	defer b.persistMu.Unlock()
	return b.writeMetaLocked()
}

// writeMetaLocked is writeMeta's body, for callers already holding persistMu.
//
// The record is snapshotted here rather than by the caller, under that same
// lock, which is what keeps a late writer from publishing a stale record: a
// persist that blocks while another completes re-reads the build on the way
// through, so it can only ever write state at least as current as the write it
// queued behind. That is what stops a shutdown snapshot from overwriting the
// terminal record finish() just wrote.
func (b *build) writeMetaLocked() error {
	if b.RootDir == "" {
		return nil
	}
	res := b.snapshot()
	m := buildMeta{
		ID:           b.ID,
		Status:       string(res.Status),
		Template:     b.Template,
		TemplatePath: res.TemplatePath,
		Command:      b.Command,
		CreatedAt:    b.CreatedAt,
		Summary:      b.Summary,
		Artifacts:    res.Artifacts,
		ErrMsg:       res.ErrMsg,
		LogFile:      res.LogFile,
		Residual:     res.Residual,
		Retry:        b.retry,
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	// Published atomically: the history endpoints read meta.json without
	// coordinating with these writes, and an in-place rewrite would expose a
	// truncated file as a corrupt record.
	return writeFileAtomic(metaPath(b.RootDir), data, 0o600)
}

// writeLogLocked persists the build's buffered log lines to <root>/compose.log
// and records the path on the build. Callers must hold persistMu.
func (b *build) writeLogLocked() {
	if b.RootDir == "" {
		return
	}
	logPath := filepath.Join(b.RootDir, "compose.log")
	content := strings.Join(b.snapshotLogs(), "\n")
	if err := writeFileAtomic(logPath, []byte(content), 0o600); err != nil {
		logger.Logger().Warnf("build %s: writing log file: %v", b.ID, err)
		return
	}
	b.mu.Lock()
	b.LogFile = logPath
	b.mu.Unlock()
}

// persist writes the build's log buffer and metadata to disk as one serialized
// unit, without changing its status.
//
// Used both by finish() to publish the final record and by the shutdown cancel
// path to save diagnostics for a build that will never reach finish() — a build
// the process is about to abandon still has a recorded cancellation and the
// reason it failed, and both belong on disk before we exit. Because every
// persist goes through the same lock and re-snapshots inside it, a shutdown
// persist racing a normal completion cannot regress the record.
func (b *build) persist() {
	b.persistMu.Lock()
	defer b.persistMu.Unlock()
	b.writeLogLocked()
	if err := b.writeMetaLocked(); err != nil {
		logger.Logger().Warnf("build %s: writing meta: %v", b.ID, err)
	}
}

// buildFromMeta reconstructs an in-memory build from a persisted meta record so
// past builds (e.g. after a server restart) can be served by the existing
// detail/logs/artifacts handlers. done is pre-closed since the build is over.
// The in-memory logLines buffer is left empty — we don't repopulate it from
// disk here; the persisted log is still downloadable via the logfile endpoint
// (m.LogFile points at <build-root>/compose.log).
func buildFromMeta(rootDir string, m buildMeta) *build {
	done := make(chan struct{})
	close(done)
	status, errMsg := reconcileInterrupted(BuildStatus(m.Status), m.ErrMsg)
	b := &build{
		ID:           m.ID,
		RootDir:      rootDir,
		WorkDir:      filepath.Join(rootDir, "work"),
		CacheDir:     filepath.Join(rootDir, "cache"),
		Template:     m.Template,
		TemplatePath: templatePathFromMeta(rootDir, m),
		Command:      m.Command,
		Summary:      m.Summary,
		CreatedAt:    m.CreatedAt,
		LogFile:      m.LogFile,
		done:         done,
		status:       status,
		artifacts:    m.Artifacts,
		errMsg:       errMsg,
		residual:     m.Residual,
		retry:        m.Retry,
	}
	return b
}

// templatePathFromMeta resolves the template path for a reconstructed build.
//
// meta.json records it directly for anything written since that field existed.
// An older record has it empty, so fall back to the archived copy an
// override build leaves at <root>/template.yml. A build against a curated
// template has no archive and keeps an empty path, which TemplateFile() then
// reports as "no template recorded" — the honest answer, since the curated file
// it ran against may since have moved or changed.
func templatePathFromMeta(rootDir string, m buildMeta) string {
	if m.TemplatePath != "" {
		return m.TemplatePath
	}
	archived := filepath.Join(rootDir, "template.yml")
	if fileExists(archived) {
		return archived
	}
	return ""
}

// reconcileInterrupted maps a persisted non-terminal status to a terminal one.
//
// A build is only ever reconstructed from meta.json when it has no live record in
// the tracker, which means the process behind it is gone: the wait goroutine that
// would have written a terminal status died with the previous server. Replaying
// not-started/running/cancelling verbatim makes a dead build look live — the UI
// adopts it as the in-flight compose, disables Compose, and offers a Cancel that
// can only fail (there is no process group to signal). Reporting it as failed
// instead is both accurate (the build did not produce an image) and actionable.
//
// The distinction matters for the work dir: an interrupted build may have left
// mounts or loop devices behind, because nothing ran its teardown. That is a
// cleanup-failure in spirit, but we deliberately do not synthesise a residual —
// we have no evidence either way, and inventing one would undermine the residual
// signal where it is real. The message points at the host instead.
func reconcileInterrupted(status BuildStatus, errMsg string) (BuildStatus, string) {
	switch status {
	case StatusNotStarted, StatusRunning, StatusCancelling:
		const detail = "build was interrupted by a server restart; its process did not " +
			"report an outcome, so any mounts or loop devices it held may still be present"
		if errMsg != "" {
			return StatusFailed, errMsg + " (" + detail + ")"
		}
		return StatusFailed, detail
	default:
		return status, errMsg
	}
}

// buildsRoot is the directory holding one subdirectory per build.
func (s *Service) buildsRoot() string {
	return filepath.Join(s.cfg.WorkDir, "builds")
}

// getBuild returns a build by id, preferring the live in-memory record and
// falling back to reconstructing one from its persisted meta.json (so past
// builds remain viewable/downloadable after a restart). The bool is false only
// when neither an in-memory record nor a meta.json exists.
func (s *Service) getBuild(id string) (*build, bool) {
	if b, ok := s.tracker.get(id); ok {
		return b, true
	}
	rootDir := filepath.Join(s.buildsRoot(), id)
	data, err := os.ReadFile(metaPath(rootDir))
	if err != nil {
		return nil, false
	}
	var m buildMeta
	if err := json.Unmarshal(data, &m); err != nil || m.ID == "" {
		return nil, false
	}
	return buildFromMeta(rootDir, m), true
}

// HistoryItem is one row in the compose history list. It carries the key
// selection fields (via Summary) so the UI can label a row by its composed
// combination rather than the (opaque) template filename.
type HistoryItem struct {
	ID        string
	Status    BuildStatus
	Template  string
	CreatedAt time.Time
	Summary   *ComposeSummary
}

// ListBuilds returns the compose history newest-first. It merges live in-memory
// builds (authoritative for status) with meta.json records on disk (so history
// survives restarts).
func (s *Service) ListBuilds() []HistoryItem {
	seen := make(map[string]HistoryItem)

	// Disk records first (may be stale), then overlay live in-memory builds.
	entries, _ := os.ReadDir(s.buildsRoot())
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		rootDir := filepath.Join(s.buildsRoot(), e.Name())
		data, err := os.ReadFile(metaPath(rootDir))
		if err != nil {
			continue // no meta.json (e.g. a build dir from before this feature)
		}
		var m buildMeta
		if err := json.Unmarshal(data, &m); err != nil || m.ID == "" {
			continue
		}
		// A disk record still marked non-terminal belongs to a build the previous
		// server was running; report it as interrupted rather than live. Live builds
		// are overlaid below, so a genuinely running build is not affected by this.
		// Without it the UI adopts a dead record as the in-flight compose.
		status, _ := reconcileInterrupted(BuildStatus(m.Status), m.ErrMsg)
		seen[m.ID] = HistoryItem{
			ID:        m.ID,
			Status:    status,
			Template:  m.Template,
			CreatedAt: m.CreatedAt,
			Summary:   m.Summary,
		}
	}

	// Overlay live builds — their status/artifacts are authoritative.
	for _, b := range s.tracker.all() {
		res := b.snapshot()
		seen[b.ID] = HistoryItem{
			ID:        b.ID,
			Status:    res.Status,
			Template:  b.Template,
			CreatedAt: b.CreatedAt,
			Summary:   b.Summary,
		}
	}

	items := make([]HistoryItem, 0, len(seen))
	for _, it := range seen {
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items
}

// BuildDetails carries the reproducibility/troubleshooting metadata the UI shows
// in its collapsible "Build details" panel: the exact command, the resolved
// template, and the per-build work/cache directories.
type BuildDetails struct {
	BuildID      string
	Status       BuildStatus
	Command      string
	Template     string
	TemplateURL  string
	TemplatePath string // on-disk path of the template this build ran against
	WorkDir      string
	CacheDir     string
	Summary      *ComposeSummary
	HasLogFile   bool // a downloadable log file exists on disk
	ErrMsg       string
	Artifacts    []Artifact     // partial outputs on fail/cancel (with on-disk path)
	Residual     *ResidualIssue // teardown-residue warning for manual remediation
}

// BuildDetails returns the command and paths for a build so the UI can show
// exactly what ran and offer the template for download. Returns a 404 *Error
// when the build is unknown.
func (s *Service) BuildDetails(id string) (*BuildDetails, error) {
	b, ok := s.getBuild(id)
	if !ok {
		return nil, newError(http.StatusNotFound, "NOT_FOUND", "build not found")
	}
	res := b.snapshot()
	return &BuildDetails{
		BuildID:      id,
		Status:       res.Status,
		Command:      b.Command,
		Template:     b.Template,
		TemplateURL:  "/api/v1/builds/" + id + "/template",
		TemplatePath: res.TemplatePath,
		WorkDir:      b.WorkDir,
		CacheDir:     b.CacheDir,
		Summary:      b.Summary,
		HasLogFile:   res.LogFile != "" && fileExists(res.LogFile),
		ErrMsg:       res.ErrMsg,
		Artifacts:    res.Artifacts,
		Residual:     res.Residual,
	}, nil
}

// ArtifactList is the artifacts response for a build.
type ArtifactList struct {
	BuildID   string
	Status    BuildStatus
	Artifacts []Artifact
}

// BuildArtifacts returns the output artifacts for a build. Returns a 404 *Error
// when the build is unknown. The artifact slice is always non-nil.
func (s *Service) BuildArtifacts(id string) (*ArtifactList, error) {
	b, ok := s.getBuild(id)
	if !ok {
		return nil, newError(http.StatusNotFound, "NOT_FOUND", "build not found")
	}
	res := b.snapshot()
	arts := res.Artifacts
	if arts == nil {
		arts = []Artifact{}
	}
	return &ArtifactList{BuildID: id, Status: res.Status, Artifacts: arts}, nil
}

// fileExists reports whether path names an existing regular file.
func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}
