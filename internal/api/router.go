package api

import "net/http"

// NewRouter creates the HTTP request multiplexer with all registered routes.
// Uses Go 1.22+ ServeMux with method-based routing as mandated by the ADR.
//
// The function signature follows the ADR's router setup pattern.
// Routes are organized by domain (health, AI, templates) matching the
// OpenAPI tag structure for clarity.
//
// Future phases add routes here without restructuring:
//   - Phase 1 (later): POST /templates, PUT/DELETE /templates/{name},
//     POST /templates/validate, DELETE /engine/cache
//   - Phase 2: GET /ai/stream (SSE streaming)
//   - Phase 3: POST/GET/DELETE /sessions/{id}
//   - Phase 5: POST/GET /builds, GET /builds/{id}, GET /builds/{id}/logs
func NewRouter(s *Server) http.Handler {
	mux := http.NewServeMux()

	// ── Engine / Health ─────────────────────────────────────────────────
	mux.HandleFunc("GET /api/v1/health", handleHealth(s))
	mux.HandleFunc("GET /api/v1/engine/stats", handleStats(s))

	// ── AI ──────────────────────────────────────────────────────────────
	mux.HandleFunc("POST /api/v1/ai/query", handleQuery(s))
	mux.HandleFunc("GET /api/v1/ai/search", handleSearch(s))

	// ── Templates ───────────────────────────────────────────────────────
	mux.HandleFunc("GET /api/v1/templates", handleListTemplates(s))
	mux.HandleFunc("GET /api/v1/templates/{name}", handleGetTemplate(s))

	// ── Future: Sessions (Phase 3) ──────────────────────────────────────
	// mux.HandleFunc("POST /api/v1/sessions", handleCreateSession(s))
	// mux.HandleFunc("GET /api/v1/sessions/{id}", handleGetSession(s))
	// mux.HandleFunc("DELETE /api/v1/sessions/{id}", handleDeleteSession(s))

	// ── SSE Streaming (Phase 2) ─────────────────────────────────────────
	mux.HandleFunc("GET /api/v1/ai/stream", handleStream(s))

	// ── Future: Template CRUD (Phase 1, later) ──────────────────────────
	// mux.HandleFunc("POST /api/v1/templates", handleCreateTemplate(s))
	// mux.HandleFunc("PUT /api/v1/templates/{name}", handleUpdateTemplate(s))
	// mux.HandleFunc("DELETE /api/v1/templates/{name}", handleDeleteTemplate(s))
	// mux.HandleFunc("POST /api/v1/templates/validate", handleValidate(s))

	// ── Future: Builds (Phase 5) ────────────────────────────────────────
	// mux.HandleFunc("POST /api/v1/builds", handleStartBuild(s))
	// mux.HandleFunc("GET /api/v1/builds", handleListBuilds(s))
	// mux.HandleFunc("GET /api/v1/builds/{id}", handleGetBuild(s))
	// mux.HandleFunc("GET /api/v1/builds/{id}/logs", handleBuildLogs(s))

	// ── Future: Cache management (Phase 1, later) ───────────────────────
	// mux.HandleFunc("DELETE /api/v1/engine/cache", handleClearCache(s))

	return mux
}
