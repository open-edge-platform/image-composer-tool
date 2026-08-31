package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"os"

	"github.com/open-edge-platform/image-composer-tool/internal/ai"
	"github.com/open-edge-platform/image-composer-tool/internal/ai/rag"
)

func TestHandleCreateSession(t *testing.T) {
	config := DefaultServerConfig()
	s := NewServer(nil, config)
	defer s.sessionMgr.Stop()

	router := NewRouter(s)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, w.Code)
	}

	var session Session
	if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if session.ID == "" {
		t.Errorf("expected non-empty session ID")
	}

	// Verify session ID format: "s_" + 8 hex chars.
	if len(session.ID) != 10 || session.ID[:2] != "s_" {
		t.Errorf("expected session ID format 's_XXXXXXXX', got '%s'", session.ID)
	}

	if session.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
	if session.LastActiveAt.IsZero() {
		t.Error("expected LastActiveAt to be set")
	}
	if session.History == nil {
		t.Error("expected History to be non-nil (empty slice)")
	}
}

// TestHandleCreateSession_MaxSessions verifies the 503 when max sessions is reached.
func TestHandleCreateSession_MaxSessions(t *testing.T) {
	config := DefaultServerConfig()
	config.Session.MaxSessions = 1
	s := NewServer(nil, config)
	defer s.sessionMgr.Stop()

	router := NewRouter(s)

	// First create should succeed.
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", nil)
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	if w1.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, w1.Code)
	}

	// Second create should fail with 503 (rate limited).
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w2.Code)
	}

	var errResp ErrorResponse
	if err := json.Unmarshal(w2.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.Error.Code != ErrCodeRateLimited {
		t.Errorf("expected error code %q, got %q", ErrCodeRateLimited, errResp.Error.Code)
	}
}

func TestHandleGetSession(t *testing.T) {
	config := DefaultServerConfig()
	s := NewServer(nil, config)
	defer s.sessionMgr.Stop()

	session, _ := s.sessionMgr.Create()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+session.ID, nil)
	router := NewRouter(s)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var retrieved Session
	if err := json.Unmarshal(w.Body.Bytes(), &retrieved); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if retrieved.ID != session.ID {
		t.Errorf("expected ID %s, got %s", session.ID, retrieved.ID)
	}
}

func TestHandleGetSession_NotFound(t *testing.T) {
	config := DefaultServerConfig()
	s := NewServer(nil, config)
	defer s.sessionMgr.Stop()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/s_nonexist", nil)
	router := NewRouter(s)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}

	var errResp ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.Error.Code != ErrCodeSessionNotFound {
		t.Errorf("expected error code '%s', got '%s'", ErrCodeSessionNotFound, errResp.Error.Code)
	}
}

func TestHandleGetSession_Expired(t *testing.T) {
	config := DefaultServerConfig()
	config.Session.Timeout = 1 * time.Millisecond // expire instantly
	s := NewServer(nil, config)
	defer s.sessionMgr.Stop()

	session, _ := s.sessionMgr.Create()
	time.Sleep(10 * time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+session.ID, nil)
	router := NewRouter(s)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusGone {
		t.Errorf("expected status %d, got %d", http.StatusGone, w.Code)
	}
}

func TestHandleDeleteSession(t *testing.T) {
	config := DefaultServerConfig()
	s := NewServer(nil, config)
	defer s.sessionMgr.Stop()

	session, _ := s.sessionMgr.Create()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/"+session.ID, nil)
	router := NewRouter(s)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status %d, got %d", http.StatusNoContent, w.Code)
	}

	// Verify it's actually deleted.
	_, err := s.sessionMgr.Get(session.ID)
	if err == nil {
		t.Errorf("expected session to be deleted")
	}

	// Verify getting a deleted session returns 404.
	reqGet := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+session.ID, nil)
	wGet := httptest.NewRecorder()
	router.ServeHTTP(wGet, reqGet)
	if wGet.Code != http.StatusNotFound {
		t.Errorf("expected status 404 after deletion, got %d", wGet.Code)
	}
}

func TestHandleDeleteSession_NotFound(t *testing.T) {
	config := DefaultServerConfig()
	s := NewServer(nil, config)
	defer s.sessionMgr.Stop()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/s_nonexist", nil)
	router := NewRouter(s)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestSessionExpiry(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "api-test-session-expiry-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	config := ai.DefaultConfig()
	config.Provider = ai.ProviderOllama
	config.Cache.Enabled = false
	config.TemplatesDir = tmpDir

	engine, err := rag.NewEngine(config)
	if err != nil {
		t.Fatalf("failed to create rag engine: %v", err)
	}

	serverConfig := DefaultServerConfig()
	serverConfig.TemplatesDir = tmpDir
	// Short timeout so the session expires quickly.
	serverConfig.Session.Timeout = 50 * time.Millisecond
	// Very long cleanup interval — we don't want it to run during this test.
	serverConfig.Session.CleanupInterval = 1 * time.Hour

	server := NewServer(engine, serverConfig)
	defer server.sessionMgr.Stop()

	handler := NewRouter(server)

	// Create a session
	reqCreate := httptest.NewRequest("POST", "/api/v1/sessions", nil)
	rrCreate := httptest.NewRecorder()
	handler.ServeHTTP(rrCreate, reqCreate)

	if rrCreate.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %v", rrCreate.Code)
	}

	var created Session
	if err := json.Unmarshal(rrCreate.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to decode create response: %v", err)
	}

	// Wait for the session to expire (but cleanup goroutine won't run)
	time.Sleep(100 * time.Millisecond)

	// Try to get the expired session — should return 410 Gone
	reqGet := httptest.NewRequest("GET", "/api/v1/sessions/"+created.ID, nil)
	rrGet := httptest.NewRecorder()
	handler.ServeHTTP(rrGet, reqGet)

	if rrGet.Code != http.StatusGone {
		t.Fatalf("expected status 410 Gone for expired session, got %v, body: %s",
			rrGet.Code, rrGet.Body.String())
	}

	var errResp ErrorResponse
	if err := json.Unmarshal(rrGet.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	if errResp.Error.Code != ErrCodeSessionExpired {
		t.Errorf("expected error code '%s', got '%s'", ErrCodeSessionExpired, errResp.Error.Code)
	}
}
