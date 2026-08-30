package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestHealthHandler(t *testing.T) {
	server, tmpDir := setupTestServer(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	rr := httptest.NewRecorder()

	handler := NewRouter(server)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status OK, got %v", rr.Code)
	}

	var resp healthResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Status != "initializing" {
		t.Errorf("expected status 'initializing', got '%s'", resp.Status)
	}
}

func TestStatsHandler(t *testing.T) {
	server, tmpDir := setupTestServer(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	req := httptest.NewRequest("GET", "/api/v1/engine/stats", nil)
	rr := httptest.NewRecorder()

	handler := NewRouter(server)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status OK, got %v", rr.Code)
	}

	var resp engineStatsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Initialized {
		t.Error("expected initialized to be false")
	}

	if resp.Provider != "ollama" {
		t.Errorf("expected provider 'ollama', got '%s'", resp.Provider)
	}
}
