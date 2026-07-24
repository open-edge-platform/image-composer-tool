// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// buildMeta is the persisted record written to <build-root>/meta.json. It lets
// the compose history survive server restarts and be reconstructed from disk.
// The full compose log is persisted separately to <build-root>/compose.log at
// completion; meta.json records its path in LogFile so past builds can offer it
// for download alongside their configuration summary and artifacts.
type buildMeta struct {
	ID        string          `json:"id"`
	Status    string          `json:"status"`
	Template  string          `json:"template"`
	Command   string          `json:"command"`
	CreatedAt time.Time       `json:"createdAt"`
	Summary   *composeSummary `json:"summary,omitempty"`
	Artifacts []artifact      `json:"artifacts,omitempty"`
	ErrMsg    string          `json:"errMsg,omitempty"`
	LogFile   string          `json:"logFile,omitempty"`
}

// metaPath returns the meta.json path for a build root directory.
func metaPath(rootDir string) string {
	return filepath.Join(rootDir, "meta.json")
}

// writeMeta persists the build's current state to <root>/meta.json. Best-effort:
// failures are returned for logging but never block a build. A build with no
// RootDir has nowhere to persist, so this is a no-op in that case.
func (b *build) writeMeta() error {
	if b.RootDir == "" {
		return nil
	}
	res := b.snapshot()
	m := buildMeta{
		ID:        b.ID,
		Status:    string(res.status),
		Template:  b.Template,
		Command:   b.Command,
		CreatedAt: b.CreatedAt,
		Summary:   b.Summary,
		Artifacts: res.artifacts,
		ErrMsg:    res.errMsg,
		LogFile:   res.logFile,
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath(b.RootDir), data, 0o600)
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
	b := &build{
		ID:        m.ID,
		RootDir:   rootDir,
		WorkDir:   filepath.Join(rootDir, "work"),
		CacheDir:  filepath.Join(rootDir, "cache"),
		Template:  m.Template,
		Command:   m.Command,
		Summary:   m.Summary,
		CreatedAt: m.CreatedAt,
		LogFile:   m.LogFile,
		done:      done,
		status:    buildStatus(m.Status),
		artifacts: m.Artifacts,
		errMsg:    m.ErrMsg,
	}
	return b
}

// buildsRoot is the directory holding one subdirectory per build.
func (s *Server) buildsRoot() string {
	return filepath.Join(s.cfg.WorkDir, "builds")
}

// getBuild returns a build by id, preferring the live in-memory record and
// falling back to reconstructing one from its persisted meta.json (so past
// builds remain viewable/downloadable after a restart). The bool is false only
// when neither an in-memory record nor a meta.json exists.
func (s *Server) getBuild(id string) (*build, bool) {
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

// historyItem is one row in the compose history list. It carries the key
// selection fields so the UI can label a row by its composed combination rather
// than the (opaque) template filename.
type historyItem struct {
	ID        string          `json:"id"`
	Status    string          `json:"status"`
	Template  string          `json:"template"`
	CreatedAt time.Time       `json:"createdAt"`
	Summary   *composeSummary `json:"summary,omitempty"`
}

// handleListBuilds returns the compose history newest-first. It merges live
// in-memory builds (authoritative for status) with meta.json records on disk
// (so history survives restarts).
func (s *Server) handleListBuilds(w http.ResponseWriter, r *http.Request) {
	seen := make(map[string]historyItem)

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
		seen[m.ID] = historyItem{
			ID:        m.ID,
			Status:    m.Status,
			Template:  m.Template,
			CreatedAt: m.CreatedAt,
			Summary:   m.Summary,
		}
	}

	// Overlay live builds — their status/artifacts are authoritative.
	for _, b := range s.tracker.all() {
		res := b.snapshot()
		seen[b.ID] = historyItem{
			ID:        b.ID,
			Status:    string(res.status),
			Template:  b.Template,
			CreatedAt: b.CreatedAt,
			Summary:   b.Summary,
		}
	}

	items := make([]historyItem, 0, len(seen))
	for _, it := range seen {
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})

	writeJSON(w, http.StatusOK, map[string]any{"builds": items})
}
