package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/ai/provider"
	"github.com/open-edge-platform/image-composer-tool/internal/ai/rag"
)

// isProviderUnavailable reports whether err (or anything it wraps) indicates
// the AI provider could not be reached. It uses typed-error matching rather
// than fragile string inspection so that DNS, TLS, timeout, and
// connection-refused failures are all classified consistently.
func isProviderUnavailable(err error) bool {
	return errors.Is(err, provider.ErrProviderUnavailable)
}

// queryRequest matches the OpenAPI QueryRequest schema.
type queryRequest struct {
	Query     string `json:"query"`
	SessionID string `json:"session_id,omitempty"`
}

// queryResponse matches the OpenAPI QueryResponse schema (Phase 1 subset).
// Fields that require session support (changes, full validation) are omitted
// for now and will be added in Phase 3 without breaking changes.
type queryResponse struct {
	YAML             string             `json:"yaml"`
	SearchResults    []searchResultJSON `json:"search_results"`
	SourceTemplates  []string           `json:"source_templates"`
	GenerationTimeMs int64              `json:"generation_time_ms"`
}

// searchResponse matches the OpenAPI SearchResponse schema.
type searchResponse struct {
	Results []searchResultJSON `json:"results"`
	Query   string             `json:"query"`
}

// searchResultJSON matches the OpenAPI SearchResult schema.
type searchResultJSON struct {
	Template      templateInfoJSON `json:"template"`
	Score         float64          `json:"score"`
	SemanticScore float64          `json:"semantic_score"`
	KeywordScore  float64          `json:"keyword_score"`
	PackageScore  float64          `json:"package_score"`
}

// templateInfoJSON matches the OpenAPI TemplateInfo schema.
type templateInfoJSON struct {
	FileName     string           `json:"file_name"`
	ImageName    string           `json:"image_name"`
	ImageVersion string           `json:"image_version"`
	Distribution string           `json:"distribution"`
	Architecture string           `json:"architecture"`
	OS           string           `json:"os"`
	ImageType    string           `json:"image_type"`
	Packages     []string         `json:"packages"`
	Metadata     templateMetaJSON `json:"metadata"`
}

// templateMetaJSON matches the OpenAPI TemplateMetadata schema.
type templateMetaJSON struct {
	Description    string   `json:"description,omitempty"`
	UseCases       []string `json:"use_cases,omitempty"`
	Keywords       []string `json:"keywords,omitempty"`
	Capabilities   []string `json:"capabilities,omitempty"`
	RecommendedFor []string `json:"recommended_for,omitempty"`
}

// handleQuery handles non-streaming AI template generation.
// POST /api/v1/ai/query
//
// Accepts a natural language query and returns the generated YAML template.
// In Phase 1, session_id is accepted but not acted upon (sessions are Phase 3).
func handleQuery(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req queryRequest
		if !decodeJSON(w, r, &req) {
			return
		}

		// Validate query is present and within length limits.
		query := strings.TrimSpace(req.Query)
		if query == "" {
			respondError(w, http.StatusBadRequest, ErrCodeQueryRequired,
				"A query string is required", nil)
			return
		}
		if len(query) > maxQueryLength {
			respondError(w, http.StatusBadRequest, ErrCodeQueryTooLong,
				"Query exceeds 2000 characters",
				map[string]any{"max_length": maxQueryLength})
			return
		}

		ctx := r.Context()

		// Call the existing Go library directly — no CLI, no subprocess.
		result, err := s.engine.GenerateWithContext(ctx, query)
		if err != nil {
			// Determine if this is a provider connectivity issue or a
			// generation failure using typed-error matching.
			if isProviderUnavailable(err) {
				respondError(w, http.StatusServiceUnavailable, ErrCodeProviderUnavail,
					"AI provider not reachable: "+err.Error(), nil)
				return
			}
			respondError(w, http.StatusBadGateway, ErrCodeGenerationFailed,
				"Template generation failed: "+err.Error(), nil)
			return
		}

		// Convert the RAG search context into the OpenAPI JSON shape so the
		// non-streaming response carries the same source information the
		// streaming path exposes.
		jsonResults := make([]searchResultJSON, 0, len(result.SearchResults))
		for _, sr := range result.SearchResults {
			jsonResults = append(jsonResults, convertSearchResult(sr))
		}
		sourceTemplates := result.SourceTemplates
		if sourceTemplates == nil {
			sourceTemplates = []string{}
		}

		resp := queryResponse{
			YAML:            result.YAML,
			SearchResults:   jsonResults,
			SourceTemplates: sourceTemplates,
		}

		respondJSON(w, http.StatusOK, resp)
	}
}

// handleSearch handles semantic template search.
// GET /api/v1/ai/search?query=...
//
// Performs a hybrid search (semantic + keyword + package) against the
// indexed template library and returns the top 5 results.
func handleSearch(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := strings.TrimSpace(r.URL.Query().Get("query"))

		if query == "" {
			respondError(w, http.StatusBadRequest, ErrCodeQueryRequired,
				"A query string is required", nil)
			return
		}
		if len(query) > maxQueryLength {
			respondError(w, http.StatusBadRequest, ErrCodeQueryTooLong,
				"Query exceeds 2000 characters",
				map[string]any{"max_length": maxQueryLength})
			return
		}

		ctx := r.Context()

		// Call the existing Go library directly — no CLI, no subprocess.
		results, err := s.engine.Search(ctx, query)
		if err != nil {
			if isProviderUnavailable(err) {
				respondError(w, http.StatusServiceUnavailable, ErrCodeProviderUnavail,
					"AI provider not reachable: "+err.Error(), nil)
				return
			}
			respondError(w, http.StatusBadGateway, ErrCodeSearchFailed,
				"Template search failed: "+err.Error(), nil)
			return
		}

		// Convert internal SearchResult structs to the OpenAPI JSON shape.
		jsonResults := make([]searchResultJSON, 0, len(results))
		for _, sr := range results {
			jsonResults = append(jsonResults, convertSearchResult(sr))
		}

		resp := searchResponse{
			Results: jsonResults,
			Query:   query,
		}

		respondJSON(w, http.StatusOK, resp)
	}
}

// convertSearchResult converts a rag.SearchResult to the OpenAPI JSON shape.
func convertSearchResult(sr rag.SearchResult) searchResultJSON {
	info := templateInfoJSON{
		FileName:     sr.Template.FileName,
		ImageName:    sr.Template.ImageName,
		ImageVersion: sr.Template.ImageVersion,
		Distribution: sr.Template.Distribution,
		Architecture: sr.Template.Architecture,
		OS:           sr.Template.OS,
		ImageType:    sr.Template.ImageType,
		Packages:     sr.Template.Packages,
		Metadata: templateMetaJSON{
			Description:    sr.Template.Metadata.Description,
			UseCases:       sr.Template.Metadata.UseCases,
			Keywords:       sr.Template.Metadata.Keywords,
			Capabilities:   sr.Template.Metadata.Capabilities,
			RecommendedFor: sr.Template.Metadata.RecommendedFor,
		},
	}

	// Ensure non-nil slices in JSON output ([] instead of null).
	if info.Packages == nil {
		info.Packages = []string{}
	}
	if info.Metadata.UseCases == nil {
		info.Metadata.UseCases = []string{}
	}
	if info.Metadata.Keywords == nil {
		info.Metadata.Keywords = []string{}
	}
	if info.Metadata.Capabilities == nil {
		info.Metadata.Capabilities = []string{}
	}
	if info.Metadata.RecommendedFor == nil {
		info.Metadata.RecommendedFor = []string{}
	}

	return searchResultJSON{
		Template:      info,
		Score:         sr.Score,
		SemanticScore: sr.SemanticScore,
		KeywordScore:  sr.KeywordScore,
		PackageScore:  sr.PackageScore,
	}
}

// ─── SSE Event Types ────────────────────────────────────────────────────
// These structs match the OpenAPI SSE event schemas exactly.

// sseSearchResults matches SSESearchResults (event: search_results).
type sseSearchResults struct {
	Results   []searchResultJSON `json:"results"`
	QueryType string             `json:"query_type"`
}

// sseGenerationStart matches SSEGenerationStart (event: generation_start).
type sseGenerationStart struct {
	SourceTemplates []string `json:"source_templates"`
}

// sseToken matches SSEToken (event: token). Field is "content" per spec.
type sseToken struct {
	Content string `json:"content"`
}

// sseGenerationComplete matches SSEGenerationComplete (event: generation_complete).
type sseGenerationComplete struct {
	YAML             string `json:"yaml"`
	GenerationTimeMs int64  `json:"generation_time_ms"`
}

// sseComplete matches SSEComplete (event: complete).
type sseComplete struct {
	SessionID  string      `json:"session_id"`
	YAML       string      `json:"yaml"`
	Validation interface{} `json:"validation,omitempty"`
	Changes    []struct{}  `json:"changes"`
}

// sseError matches SSEError (event: error).
type sseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Retry   bool   `json:"retry"`
}

// writeSSEEvent writes a single Server-Sent Event to the response.
// Format: "event: <type>\ndata: <json>\n\n"
func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, eventType string, data any) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal SSE data: %w", err)
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, jsonData)
	if err != nil {
		return fmt.Errorf("failed to write SSE event: %w", err)
	}
	flusher.Flush()
	return nil
}

// handleStream handles SSE streaming AI template generation.
// GET /api/v1/ai/stream?query=...&session_id=...
//
// Emits SSE events in order per the OpenAPI spec:
//
//	search_results → generation_start → token (×N) → generation_complete → complete
//
// On error, emits a single "error" event and closes the stream.
// session_id is accepted but ignored in Phase 2 (sessions are Phase 3).
func handleStream(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := strings.TrimSpace(r.URL.Query().Get("query"))
		sessionID := r.URL.Query().Get("session_id") // Accepted, ignored in Phase 2

		// Validate query is present and within length limits.
		if query == "" {
			respondError(w, http.StatusBadRequest, ErrCodeQueryRequired,
				"A query string is required", nil)
			return
		}
		if len(query) > maxQueryLength {
			respondError(w, http.StatusBadRequest, ErrCodeQueryTooLong,
				"Query exceeds 2000 characters",
				map[string]any{"max_length": maxQueryLength})
			return
		}

		// Verify the response writer supports flushing (required for SSE).
		flusher, ok := w.(http.Flusher)
		if !ok {
			respondError(w, http.StatusInternalServerError, ErrCodeEngineUnavailable,
				"Streaming not supported by server transport", nil)
			return
		}

		// Set SSE headers before writing any events.
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		ctx := r.Context()
		startTime := time.Now()

		// Start streaming generation (search is synchronous).
		result, err := s.engine.GenerateStream(ctx, query)
		if err != nil {
			code := ErrCodeGenerationFailed
			if isProviderUnavailable(err) {
				code = ErrCodeProviderUnavail
			}
			_ = writeSSEEvent(w, flusher, "error", sseError{
				Code:    code,
				Message: err.Error(),
				Retry:   true,
			})
			return
		}

		// Event 1: search_results — RAG search results with scores.
		jsonResults := make([]searchResultJSON, 0, len(result.SearchResults))
		for _, sr := range result.SearchResults {
			jsonResults = append(jsonResults, convertSearchResult(sr))
		}
		if err := writeSSEEvent(w, flusher, "search_results", sseSearchResults{
			Results: jsonResults,
			// TODO(phase-query-classifier): report the real query type once
			// the query classifier lands. Hardcoded to "semantic" until then.
			QueryType: "semantic",
		}); err != nil {
			return // Client disconnected
		}

		// Event 2: generation_start — which templates are used as context.
		if err := writeSSEEvent(w, flusher, "generation_start", sseGenerationStart{
			SourceTemplates: result.SourceTemplates,
		}); err != nil {
			return
		}

		// Events 3…N: token — individual LLM output tokens.
		var fullYAML strings.Builder
		for token := range result.TokenChan {
			fullYAML.WriteString(token)
			if err := writeSSEEvent(w, flusher, "token", sseToken{
				Content: token,
			}); err != nil {
				return // Client disconnected, context cancellation will clean up
			}
		}

		// After tokenChan closes, check for streaming errors (non-blocking).
		var streamErr error
		select {
		case streamErr = <-result.ErrChan:
		default:
		}

		if streamErr != nil {
			_ = writeSSEEvent(w, flusher, "error", sseError{
				Code:    ErrCodeGenerationFailed,
				Message: streamErr.Error(),
				Retry:   true,
			})
			return
		}

		generationTimeMs := time.Since(startTime).Milliseconds()
		cleanedYAML := rag.CleanYAMLResponse(fullYAML.String())

		// Event N+1: generation_complete — full YAML and timing.
		if err := writeSSEEvent(w, flusher, "generation_complete", sseGenerationComplete{
			YAML:             cleanedYAML,
			GenerationTimeMs: generationTimeMs,
		}); err != nil {
			return
		}

		// Event N+2: complete — final result with session info.
		// session_id is empty in Phase 2; will be populated when sessions
		// are implemented in Phase 3.
		_ = writeSSEEvent(w, flusher, "complete", sseComplete{
			SessionID:  sessionID,
			YAML:       cleanedYAML,
			Validation: nil,
			Changes:    []struct{}{},
		})
	}
}
