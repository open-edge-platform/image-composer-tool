package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateRequestID(t *testing.T) {
	id1 := generateRequestID()
	id2 := generateRequestID()

	if id1 == id2 {
		t.Errorf("expected unique request IDs, got %q twice", id1)
	}

	if !strings.HasPrefix(id1, "req_") {
		t.Errorf("expected request ID to start with 'req_', got %q", id1)
	}
}

func TestPathWithinDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pathtest-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	tests := []struct {
		name   string
		dir    string
		target string
		want   bool
	}{
		{
			name:   "child file",
			dir:    tmpDir,
			target: filepath.Join(tmpDir, "file.yml"),
			want:   true,
		},
		{
			name:   "nested child",
			dir:    tmpDir,
			target: filepath.Join(tmpDir, "sub", "file.yml"),
			want:   true,
		},
		{
			name:   "dir itself",
			dir:    tmpDir,
			target: tmpDir,
			want:   true,
		},
		{
			name:   "parent traversal",
			dir:    tmpDir,
			target: filepath.Join(tmpDir, "..", "secret.yml"),
			want:   false,
		},
		{
			name:   "double traversal",
			dir:    tmpDir,
			target: filepath.Join(tmpDir, "..", "..", "etc", "passwd"),
			want:   false,
		},
		{
			name:   "sibling directory",
			dir:    filepath.Join(tmpDir, "templates"),
			target: filepath.Join(tmpDir, "secrets", "key.pem"),
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pathWithinDir(tt.dir, tt.target)
			if got != tt.want {
				t.Errorf("pathWithinDir(%q, %q) = %v, want %v", tt.dir, tt.target, got, tt.want)
			}
		})
	}
}

func TestRespondJSON(t *testing.T) {
	t.Run("with data", func(t *testing.T) {
		rec := httptest.NewRecorder()
		data := map[string]string{"key": "value"}

		respondJSON(rec, http.StatusOK, data)

		if rec.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type 'application/json', got %q", ct)
		}

		var result map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if result["key"] != "value" {
			t.Errorf("expected key=value, got key=%s", result["key"])
		}
	})

	t.Run("nil data", func(t *testing.T) {
		rec := httptest.NewRecorder()

		respondJSON(rec, http.StatusNoContent, nil)

		if rec.Code != http.StatusNoContent {
			t.Errorf("expected status %d, got %d", http.StatusNoContent, rec.Code)
		}
		if rec.Body.Len() != 0 {
			t.Errorf("expected empty body for nil data, got %q", rec.Body.String())
		}
	})
}

func TestRespondError(t *testing.T) {
	t.Run("with details", func(t *testing.T) {
		rec := httptest.NewRecorder()
		details := map[string]any{"max_length": 2000}

		respondError(rec, http.StatusBadRequest, ErrCodeQueryTooLong, "Query too long", details)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
		}

		var errResp ErrorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
			t.Fatalf("failed to decode error response: %v", err)
		}
		if errResp.Error.Code != ErrCodeQueryTooLong {
			t.Errorf("expected error code %q, got %q", ErrCodeQueryTooLong, errResp.Error.Code)
		}
		if errResp.Error.Message != "Query too long" {
			t.Errorf("expected message 'Query too long', got %q", errResp.Error.Message)
		}
		if errResp.Error.RequestID == "" {
			t.Error("expected non-empty request ID")
		}
		if errResp.Error.Details["max_length"] == nil {
			t.Error("expected details to contain max_length")
		}
	})

	t.Run("nil details defaults to empty map", func(t *testing.T) {
		rec := httptest.NewRecorder()

		respondError(rec, http.StatusNotFound, ErrCodeSessionNotFound, "not found", nil)

		var errResp ErrorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
			t.Fatalf("failed to decode error response: %v", err)
		}
		if errResp.Error.Details == nil {
			t.Error("expected details to be non-nil empty map, got nil")
		}
	})
}

func TestDecodeJSON(t *testing.T) {
	t.Run("valid JSON", func(t *testing.T) {
		body := strings.NewReader(`{"query": "test"}`)
		req := httptest.NewRequest(http.MethodPost, "/", body)
		rec := httptest.NewRecorder()

		var target queryRequest
		ok := decodeJSON(rec, req, &target)

		if !ok {
			t.Fatal("expected decodeJSON to return true")
		}
		if target.Query != "test" {
			t.Errorf("expected query 'test', got %q", target.Query)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		body := strings.NewReader("{bad json")
		req := httptest.NewRequest(http.MethodPost, "/", body)
		rec := httptest.NewRecorder()

		var target queryRequest
		ok := decodeJSON(rec, req, &target)

		if ok {
			t.Fatal("expected decodeJSON to return false for invalid JSON")
		}
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})

	t.Run("nil body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		// http.NewRequest sets Body to http.NoBody, which is not nil.
		// Explicitly set to nil to test the nil branch.
		req.Body = nil
		rec := httptest.NewRecorder()

		var target queryRequest
		ok := decodeJSON(rec, req, &target)

		if ok {
			t.Fatal("expected decodeJSON to return false for nil body")
		}
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})
}
