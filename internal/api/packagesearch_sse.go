// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/api/service"
)

// searchStreamBudget bounds how long a streaming search stays open. A search
// fans out over the whole catalog, and an unreachable mirror only fails once it
// has burned its TCP dial timeout — far longer than a user will watch an
// autocomplete spinner. Past this the stream closes with truncated=true rather
// than tracking the slowest repository, and the lookups still in flight keep
// running (pkgindex detaches them) so they warm the cache for the next search.
const searchStreamBudget = 8 * time.Second

// handleSearchPackageStream streams a cross-repository package search as
// Server-Sent Events, emitting each repository's matches as it responds.
//
// This is intentionally outside the generated JSON ServerInterface, for the same
// reason handleBuildLogs is: oapi-codegen does not model text/event-stream
// cleanly, so the stream stays a hand-written handler registered in routes().
func (s *Server) handleSearchPackageStream(w http.ResponseWriter, r *http.Request) {
	p, err := parseSearchStreamParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "NO_STREAM", "streaming unsupported")
		return
	}
	// Validate through the service before any SSE header or body is written: a
	// status cannot be changed once the stream has started, so a bad query has
	// to fail as plain JSON here rather than as an event.
	if err := s.svc.ValidateSearchQuery(p.query); err != nil {
		writeServiceError(w, err)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush() // establish the stream before the first repository answers

	ctx, cancel := context.WithTimeout(r.Context(), searchStreamBudget)
	defer cancel()

	var responded, failed atomic.Int64
	planned, err := s.svc.SearchPackagesStream(ctx, p.osID, p.query, p.repos, p.limit,
		func(b service.PackageSearchBatch) {
			responded.Add(1)
			if b.Err != nil {
				failed.Add(1)
			}
			sendBatch(w, flusher, b)
		})
	// An error here can only be a rejected query, already screened above. It is
	// reported as a stream that covered nothing rather than as a status, since
	// the headers have been sent and the status can no longer be changed.
	if err != nil {
		planned = 0
	}
	sendEvent(w, "done", map[string]any{
		"repos":     planned,
		"failed":    failed.Load(),
		"truncated": err != nil || int(responded.Load()) < planned,
	})
	flusher.Flush()
}

// searchStreamParams is one parsed streaming-search request.
type searchStreamParams struct {
	osID  string
	query string
	repos []string
	limit int
}

func parseSearchStreamParams(r *http.Request) (searchStreamParams, error) {
	q := r.URL.Query()
	p := searchStreamParams{osID: q.Get("os"), query: q.Get("q"), repos: q["repos"]}
	if p.osID == "" {
		return p, errors.New("query argument os is required")
	}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return p, errors.New("limit must be an integer")
		}
		p.limit = n
	}
	return p, nil
}

// sendBatch writes one repository's outcome to the stream.
func sendBatch(w http.ResponseWriter, flusher http.Flusher, b service.PackageSearchBatch) {
	switch {
	case b.Err != nil:
		sendEvent(w, "repoError", map[string]string{"repo": b.RepoID, "message": b.Err.Error()})
	case len(b.Hits) == 0:
		// A repository with nothing to say is left out rather than sent as an
		// empty batch: a catalog-wide search matches in a handful of
		// repositories, so most batches would otherwise be noise.
		return
	default:
		sendEvent(w, "hits", map[string]any{
			"repo":     b.RepoID,
			"packages": fromPackageSearchHits(b.Hits),
		})
	}
	flusher.Flush()
}
