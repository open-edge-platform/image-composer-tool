package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestCORS(t *testing.T) {
	server, tmpDir := setupTestServer(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Test preflight (OPTIONS) request
	req := httptest.NewRequest("OPTIONS", "/api/v1/health", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "GET")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type")

	rr := httptest.NewRecorder()

	// Apply CORS middleware to the router
	handler := withCORS(NewRouter(server), server.config.CORS)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected status NoContent (204) for preflight, got %v", rr.Code)
	}

	origin := rr.Header().Get("Access-Control-Allow-Origin")
	if origin != "http://localhost:3000" {
		t.Errorf("expected CORS origin 'http://localhost:3000', got '%s'", origin)
	}

	methods := rr.Header().Get("Access-Control-Allow-Methods")
	if methods == "" {
		t.Error("expected Access-Control-Allow-Methods header to be set")
	}

	// Test regular request with Origin
	reqGet := httptest.NewRequest("GET", "/api/v1/health", nil)
	reqGet.Header.Set("Origin", "https://example.com")

	rrGet := httptest.NewRecorder()
	handler.ServeHTTP(rrGet, reqGet)

	if rrGet.Code != http.StatusOK {
		t.Errorf("expected status OK, got %v", rrGet.Code)
	}

	originGet := rrGet.Header().Get("Access-Control-Allow-Origin")
	if originGet != "https://example.com" {
		t.Errorf("expected CORS origin 'https://example.com', got '%s'", originGet)
	}
}
