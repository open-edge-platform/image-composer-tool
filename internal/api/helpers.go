// Package api provides the HTTP API layer for the Image Composer Tool.
// It is a thin wrapper over the shared Go library (internal/ai/) — handling
// only routing, CORS, JSON serialization, and SSE connection management.
// No business logic lives here.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
)

// requestCounter is used to generate unique request IDs.
var requestCounter uint64

// generateRequestID returns a unique request ID for error tracing.
func generateRequestID() string {
	id := atomic.AddUint64(&requestCounter, 1)
	return fmt.Sprintf("req_%06d", id)
}

// pathWithinDir reports whether target resolves to a location inside dir
// (or dir itself). Both paths are resolved to absolute, cleaned form before
// comparison so that "../" segments cannot escape the directory. It is used
// as defense in depth against path traversal in filesystem-backed handlers.
func pathWithinDir(dir, target string) bool {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return false
	}

	rel, err := filepath.Rel(absDir, absTarget)
	if err != nil {
		return false
	}
	// rel must not climb out of dir (no leading "..") and must not be
	// an absolute path on another volume.
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// respondJSON writes a JSON response with the given status code.
// It sets the Content-Type header to application/json.
func respondJSON(w http.ResponseWriter, statusCode int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if data == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(data); err != nil {
		// Headers are already written; just log the failure server-side.
		logger.Logger().Errorf("failed to encode JSON response: %v", err)
	}
}

// ErrorResponse is the standard error envelope used by all API error responses.
// Matches the OpenAPI ErrorResponse schema exactly.
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail contains the error information.
// Matches the OpenAPI Error schema exactly.
type ErrorDetail struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details"`
	RequestID string         `json:"request_id"`
}

// respondError writes a standardized error response matching the OpenAPI spec.
// The error envelope format is:
//
//	{
//	  "error": {
//	    "code": "VALIDATION_FAILED",
//	    "message": "Template validation failed with 2 errors",
//	    "details": {},
//	    "request_id": "req_abc123"
//	  }
//	}
func respondError(w http.ResponseWriter, statusCode int, code, message string, details map[string]any) {
	if details == nil {
		details = map[string]any{}
	}

	resp := ErrorResponse{
		Error: ErrorDetail{
			Code:      code,
			Message:   message,
			Details:   details,
			RequestID: generateRequestID(),
		},
	}

	respondJSON(w, statusCode, resp)
}

// Stable error code constants from the OpenAPI spec and ADR error table.
// These are part of the API stability contract and will not change within
// a version.
const (
	ErrCodeSessionNotFound   = "SESSION_NOT_FOUND"
	ErrCodeSessionExpired    = "SESSION_EXPIRED"
	ErrCodeQueryRequired     = "QUERY_REQUIRED"
	ErrCodeQueryTooLong      = "QUERY_TOO_LONG"
	ErrCodeSearchFailed      = "SEARCH_FAILED"
	ErrCodeEngineUnavailable = "ENGINE_UNAVAILABLE"
	ErrCodeValidationFailed  = "VALIDATION_FAILED"
	ErrCodeTemplateNotFound  = "TEMPLATE_NOT_FOUND"
	ErrCodeTemplateExists    = "TEMPLATE_EXISTS"
	ErrCodeGenerationFailed  = "GENERATION_FAILED"
	ErrCodeProviderUnavail   = "PROVIDER_UNAVAILABLE"
	ErrCodeBuildFailed       = "BUILD_FAILED"
	ErrCodeBuildNotFound     = "BUILD_NOT_FOUND"
	ErrCodeRateLimited       = "RATE_LIMITED"
)

// maxQueryLength is the maximum allowed query length (from OpenAPI spec).
const maxQueryLength = 2000

// decodeJSON reads and decodes a JSON request body into the given target.
// Returns false and sends an error response if decoding fails.
func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	if r.Body == nil {
		respondError(w, http.StatusBadRequest, ErrCodeQueryRequired,
			"Request body is required", nil)
		return false
	}

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(target); err != nil {
		respondError(w, http.StatusBadRequest, ErrCodeQueryRequired,
			"Invalid JSON in request body: "+err.Error(), nil)
		return false
	}

	return true
}
