package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/ai/rag"
	"github.com/open-edge-platform/image-composer-tool/internal/ai/template"

	"errors"
	"fmt"

	"github.com/open-edge-platform/image-composer-tool/internal/ai/provider"
)

type responseRecorderWithFlusher struct {
	*httptest.ResponseRecorder
	flushed bool
}

func (r *responseRecorderWithFlusher) Flush() {
	r.flushed = true
	r.ResponseRecorder.Flush()
}

func TestConvertSearchResult(t *testing.T) {
	sr := rag.SearchResult{
		Template: &template.TemplateInfo{
			FileName:     "test.yml",
			ImageName:    "img",
			ImageVersion: "1.0",
		},
		Score: 0.95,
	}
	res := convertSearchResult(sr)
	if res.Score != 0.95 || res.Template.FileName != "test.yml" {
		t.Errorf("convertSearchResult mismatch: %+v", res)
	}
}

func TestWriteSSEEvent(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &responseRecorderWithFlusher{ResponseRecorder: rec}

	data := map[string]string{"msg": "hello"}
	err := writeSSEEvent(rw, rw, "token", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !rw.flushed {
		t.Error("expected Flusher.Flush to be called")
	}

	expected := "event: token\ndata: {\"msg\":\"hello\"}\n\n"
	if rec.Body.String() != expected {
		t.Errorf("expected %q, got %q", expected, rec.Body.String())
	}
}

func TestHandleQuery_InvalidJSON(t *testing.T) {
	server, tmpDir := setupTestServer(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/query", bytes.NewBuffer([]byte("{bad json")))
	rec := httptest.NewRecorder()
	NewRouter(server).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON, got %d", rec.Code)
	}
}

func TestHandleQuery_EmptyQuery(t *testing.T) {
	server, tmpDir := setupTestServer(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	body, _ := json.Marshal(queryRequest{Query: ""})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/query", bytes.NewBuffer(body))
	rec := httptest.NewRecorder()
	NewRouter(server).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty query, got %d", rec.Code)
	}
}

func TestHandleQuery_EngineExecution(t *testing.T) {
	server, tmpDir := setupTestServer(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	body, _ := json.Marshal(queryRequest{Query: "test"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/query", bytes.NewBuffer(body))
	rec := httptest.NewRecorder()
	NewRouter(server).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable && rec.Code != http.StatusInternalServerError && rec.Code != http.StatusBadGateway {
		t.Errorf("expected engine error status, got %d", rec.Code)
	}
}

func TestHandleSearch_MissingQuery(t *testing.T) {
	server, tmpDir := setupTestServer(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/search", nil)
	rec := httptest.NewRecorder()
	NewRouter(server).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleSearch_EngineExecution(t *testing.T) {
	server, tmpDir := setupTestServer(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/search?query=test", nil)
	rec := httptest.NewRecorder()
	NewRouter(server).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable && rec.Code != http.StatusInternalServerError && rec.Code != http.StatusBadGateway {
		t.Errorf("expected engine error status, got %d", rec.Code)
	}
}

func TestHandleStream_MissingQuery(t *testing.T) {
	server, tmpDir := setupTestServer(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/stream", nil)
	rec := httptest.NewRecorder()
	NewRouter(server).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing query, got %d", rec.Code)
	}
}

func TestHandleStream_EngineExecution(t *testing.T) {
	server, tmpDir := setupTestServer(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/stream?query=test", nil)
	rec := httptest.NewRecorder()
	NewRouter(server).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK && rec.Code != http.StatusServiceUnavailable && rec.Code != http.StatusInternalServerError && rec.Code != http.StatusBadGateway {
		t.Errorf("unexpected status code %d", rec.Code)
	}
}

// TestHandleQuery_QueryTooLong verifies the maxQueryLength validation branch.
func TestHandleQuery_QueryTooLong(t *testing.T) {
	server, tmpDir := setupTestServer(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	longQuery := strings.Repeat("a", maxQueryLength+1)
	body, _ := json.Marshal(queryRequest{Query: longQuery})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/query", bytes.NewBuffer(body))
	rec := httptest.NewRecorder()
	NewRouter(server).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for query too long, got %d", rec.Code)
	}

	var errResp ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.Error.Code != ErrCodeQueryTooLong {
		t.Errorf("expected error code %q, got %q", ErrCodeQueryTooLong, errResp.Error.Code)
	}
}

// TestHandleSearch_QueryTooLong verifies the maxQueryLength validation branch.
func TestHandleSearch_QueryTooLong(t *testing.T) {
	server, tmpDir := setupTestServer(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	longQuery := strings.Repeat("a", maxQueryLength+1)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/search?query="+longQuery, nil)
	rec := httptest.NewRecorder()
	NewRouter(server).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for search query too long, got %d", rec.Code)
	}

	var errResp ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.Error.Code != ErrCodeQueryTooLong {
		t.Errorf("expected error code %q, got %q", ErrCodeQueryTooLong, errResp.Error.Code)
	}
}

// TestHandleStream_QueryTooLong verifies the maxQueryLength validation branch.
func TestHandleStream_QueryTooLong(t *testing.T) {
	server, tmpDir := setupTestServer(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	longQuery := strings.Repeat("a", maxQueryLength+1)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/stream?query="+longQuery, nil)
	rec := httptest.NewRecorder()
	NewRouter(server).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for stream query too long, got %d", rec.Code)
	}
}

// ── Session CRUD Tests (Phase 3) ────────────────────────────────────────

// TODO: Convert to a deterministic unit test once an Engine interface is
// extracted. Currently depends on a running Ollama server and will skip
// if the provider is unavailable.
func TestQueryWithSessionAndRefinement(t *testing.T) {
	server, tmpDir := setupTestServer(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	handler := NewRouter(server)

	// 1. Create a session
	reqCreate := httptest.NewRequest("POST", "/api/v1/sessions", nil)
	rrCreate := httptest.NewRecorder()
	handler.ServeHTTP(rrCreate, reqCreate)

	if rrCreate.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %v", rrCreate.Code)
	}

	var session Session
	if err := json.Unmarshal(rrCreate.Body.Bytes(), &session); err != nil {
		t.Fatalf("failed to decode create response: %v", err)
	}

	// 2. Initial query (fresh generation, no template yet)
	initialQuery := queryRequest{
		Query:     "create a simple nginx image",
		SessionID: session.ID,
	}
	body, _ := json.Marshal(initialQuery)
	reqQuery := httptest.NewRequest("POST", "/api/v1/ai/query", bytes.NewReader(body))
	rrQuery := httptest.NewRecorder()
	handler.ServeHTTP(rrQuery, reqQuery)

	if rrQuery.Code != http.StatusOK {
		// If Ollama is not running, this might fail with 503/502. We'll skip if provider unavailable.
		if rrQuery.Code == http.StatusServiceUnavailable || rrQuery.Code == http.StatusBadGateway {
			t.Skipf("Provider unavailable, skipping query test. Err: %s", rrQuery.Body.String())
		}
		t.Fatalf("expected status 200 OK, got %v, body: %s", rrQuery.Code, rrQuery.Body.String())
	}

	var resp queryResponse
	if err := json.Unmarshal(rrQuery.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode query response: %v", err)
	}
	if resp.SessionID != session.ID {
		t.Errorf("expected SessionID %s, got %s", session.ID, resp.SessionID)
	}

	// 3. Verify session was updated
	reqGet := httptest.NewRequest("GET", "/api/v1/sessions/"+session.ID, nil)
	rrGet := httptest.NewRecorder()
	handler.ServeHTTP(rrGet, reqGet)

	var updatedSession Session
	if err := json.Unmarshal(rrGet.Body.Bytes(), &updatedSession); err != nil {
		t.Fatalf("failed to decode get response: %v", err)
	}

	if updatedSession.CurrentTemplate == nil {
		t.Fatal("expected CurrentTemplate to be set after initial query")
	}
	if updatedSession.CurrentTemplate.YAML == "" {
		t.Error("expected CurrentTemplate.YAML to be non-empty")
	}
	if len(updatedSession.History) != 2 {
		t.Errorf("expected 2 messages in history, got %d", len(updatedSession.History))
	}

	// 4. Refinement query (this should hit the refinement path bypassing RAG)
	refineQuery := queryRequest{
		Query:     "now add the curl package",
		SessionID: session.ID,
	}
	bodyRefine, _ := json.Marshal(refineQuery)
	reqRefine := httptest.NewRequest("POST", "/api/v1/ai/query", bytes.NewReader(bodyRefine))
	rrRefine := httptest.NewRecorder()
	handler.ServeHTTP(rrRefine, reqRefine)

	if rrRefine.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK for refinement, got %v, body: %s", rrRefine.Code, rrRefine.Body.String())
	}

	var respRefine queryResponse
	if err := json.Unmarshal(rrRefine.Body.Bytes(), &respRefine); err != nil {
		t.Fatalf("failed to decode refine response: %v", err)
	}
	if respRefine.SessionID != session.ID {
		t.Errorf("expected SessionID %s, got %s", session.ID, respRefine.SessionID)
	}

	// Verify history grew
	reqGet2 := httptest.NewRequest("GET", "/api/v1/sessions/"+session.ID, nil)
	rrGet2 := httptest.NewRecorder()
	handler.ServeHTTP(rrGet2, reqGet2)

	var finalSession Session
	if err := json.Unmarshal(rrGet2.Body.Bytes(), &finalSession); err != nil {
		t.Fatalf("failed to decode get-session response: %v", err)
	}
	if len(finalSession.History) != 4 {
		t.Errorf("expected 4 messages in history, got %d", len(finalSession.History))
	}
}

func TestQueryWithInvalidSession(t *testing.T) {
	server, tmpDir := setupTestServer(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	handler := NewRouter(server)

	reqQuery := queryRequest{
		Query:     "hello",
		SessionID: "s_nonexist",
	}
	body, _ := json.Marshal(reqQuery)
	req := httptest.NewRequest("POST", "/api/v1/ai/query", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 Not Found for invalid session, got %v", rr.Code)
	}
}

func TestIsProviderUnavailable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "wrapped provider unavailable",
			err:  fmt.Errorf("failed to call Ollama API: %w: connection refused", provider.ErrProviderUnavailable),
			want: true,
		},
		{
			name: "doubly wrapped",
			err:  fmt.Errorf("generate: %w", fmt.Errorf("%w: dial tcp", provider.ErrProviderUnavailable)),
			want: true,
		},
		{
			name: "generation failure is not unavailable",
			err:  errors.New("no relevant templates found for query"),
			want: false,
		},
		{
			name: "message mentioning connect is not misclassified",
			err:  errors.New("failed to connect the generated pipeline stage"),
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isProviderUnavailable(tc.err); got != tc.want {
				t.Errorf("isProviderUnavailable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
