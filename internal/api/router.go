package api

import "net/http"

// NewRouter creates the HTTP request multiplexer with all registered routes.
// Routes are organized by domain (health, AI, templates) matching the
// OpenAPI tag structure for clarity.
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

	// ── Sessions ─────────────────────────────────────────────────────────
	mux.HandleFunc("POST /api/v1/sessions", handleCreateSession(s))
	mux.HandleFunc("GET /api/v1/sessions/{id}", handleGetSession(s))
	mux.HandleFunc("DELETE /api/v1/sessions/{id}", handleDeleteSession(s))

	// ── SSE Streaming ───────────────────────────────────────────────────
	mux.HandleFunc("GET /api/v1/ai/stream", handleStream(s))

	// ── Future: Template CRUD ───────────────────────────────────────────
	// mux.HandleFunc("POST /api/v1/templates", handleCreateTemplate(s))
	// mux.HandleFunc("PUT /api/v1/templates/{name}", handleUpdateTemplate(s))
	// mux.HandleFunc("DELETE /api/v1/templates/{name}", handleDeleteTemplate(s))
	mux.HandleFunc("POST /api/v1/templates/validate", handleValidate(s))

	// ── Future: Builds ──────────────────────────────────────────────────
	// mux.HandleFunc("POST /api/v1/builds", handleStartBuild(s))
	// mux.HandleFunc("GET /api/v1/builds", handleListBuilds(s))
	// mux.HandleFunc("GET /api/v1/builds/{id}", handleGetBuild(s))
	// mux.HandleFunc("GET /api/v1/builds/{id}/logs", handleBuildLogs(s))

	// ── Future: Cache management ────────────────────────────────────────
	// mux.HandleFunc("DELETE /api/v1/engine/cache", handleClearCache(s))

	return mux
}
