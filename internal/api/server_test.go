package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/ai"
	"github.com/open-edge-platform/image-composer-tool/internal/ai/rag"
)

// setupTestServer helper sets up a server instance with a temporary templates directory
func setupTestServer(t *testing.T) (*Server, string) {
	t.Helper()

	// Create temp directory for templates
	tmpDir, err := os.MkdirTemp("", "api-test-templates-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	config := ai.DefaultConfig()
	config.Provider = ai.ProviderOllama
	config.Cache.Enabled = false
	config.TemplatesDir = tmpDir

	engine, err := rag.NewEngine(config)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create rag engine: %v", err)
	}

	serverConfig := DefaultServerConfig()
	serverConfig.TemplatesDir = tmpDir
	serverConfig.CORS.AllowedOrigins = []string{"http://localhost:3000", "https://example.com"}

	s := NewServer(engine, serverConfig)
	return s, tmpDir
}

func TestHealthHandler(t *testing.T) {
	server, tmpDir := setupTestServer(t)
	defer os.RemoveAll(tmpDir)

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
	defer os.RemoveAll(tmpDir)

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

func TestCORS(t *testing.T) {
	server, tmpDir := setupTestServer(t)
	defer os.RemoveAll(tmpDir)

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

func TestTemplatesHandlers(t *testing.T) {
	server, tmpDir := setupTestServer(t)
	defer os.RemoveAll(tmpDir)

	// 1. Initially, templates list should be empty
	reqList := httptest.NewRequest("GET", "/api/v1/templates", nil)
	rrList := httptest.NewRecorder()

	handler := NewRouter(server)
	handler.ServeHTTP(rrList, reqList)

	if rrList.Code != http.StatusOK {
		t.Fatalf("expected status OK, got %v", rrList.Code)
	}

	var listResp templateListResponse
	if err := json.Unmarshal(rrList.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("failed to decode list response: %v", err)
	}

	if listResp.Count != 0 || len(listResp.Templates) != 0 {
		t.Errorf("expected empty templates list, got count %d", listResp.Count)
	}

	// 2. Create a dummy template file
	dummyYAML := `image:
  name: test-web-image
  version: "1.2.3"
target:
  os: rhel
  dist: rhel9
  arch: aarch64
  imageType: qcow2
systemConfig:
  name: test-config
  description: "A test template for web interface unit testing"
  packages:
    - tmux
    - curl
`
	filename := "test-web-image.yml"
	err := os.WriteFile(filepath.Join(tmpDir, filename), []byte(dummyYAML), 0644)
	if err != nil {
		t.Fatalf("failed to write test template: %v", err)
	}

	// 3. Request templates list again
	rrList2 := httptest.NewRecorder()
	handler.ServeHTTP(rrList2, reqList)

	if rrList2.Code != http.StatusOK {
		t.Fatalf("expected status OK, got %v", rrList2.Code)
	}

	var listResp2 templateListResponse
	if err := json.Unmarshal(rrList2.Body.Bytes(), &listResp2); err != nil {
		t.Fatalf("failed to decode list response: %v", err)
	}

	if listResp2.Count != 1 || len(listResp2.Templates) != 1 {
		t.Fatalf("expected 1 template, got count %d", listResp2.Count)
	}

	info := listResp2.Templates[0]
	if info.FileName != filename {
		t.Errorf("expected filename '%s', got '%s'", filename, info.FileName)
	}
	if info.ImageName != "test-web-image" {
		t.Errorf("expected image name 'test-web-image', got '%s'", info.ImageName)
	}
	if info.Distribution != "rhel9" {
		t.Errorf("expected distribution 'rhel9', got '%s'", info.Distribution)
	}

	// 4. Request the specific template details (GET /api/v1/templates/{name})
	// Go 1.22 path values can be simulated via httptest by setting the path correctly,
	// but ServeMux uses request path values. Let's make sure we test it.
	reqGet := httptest.NewRequest("GET", "/api/v1/templates/test-web-image", nil)
	rrGet := httptest.NewRecorder()
	handler.ServeHTTP(rrGet, reqGet)

	if rrGet.Code != http.StatusOK {
		t.Fatalf("expected status OK, got %v, body: %s", rrGet.Code, rrGet.Body.String())
	}

	var detailResp templateDetailResponse
	if err := json.Unmarshal(rrGet.Body.Bytes(), &detailResp); err != nil {
		t.Fatalf("failed to decode detail response: %v", err)
	}

	if detailResp.FileName != filename {
		t.Errorf("expected filename '%s', got '%s'", filename, detailResp.FileName)
	}
	if detailResp.YAML != dummyYAML {
		t.Errorf("expected YAML content to match, got: %s", detailResp.YAML)
	}

	// Test template not found
	reqGetNotFound := httptest.NewRequest("GET", "/api/v1/templates/nonexistent", nil)
	rrGetNotFound := httptest.NewRecorder()
	handler.ServeHTTP(rrGetNotFound, reqGetNotFound)

	if rrGetNotFound.Code != http.StatusNotFound {
		t.Errorf("expected status NotFound (404), got %v", rrGetNotFound.Code)
	}

	var errResp ErrorResponse
	if err := json.Unmarshal(rrGetNotFound.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	if errResp.Error.Code != ErrCodeTemplateNotFound {
		t.Errorf("expected error code '%s', got '%s'", ErrCodeTemplateNotFound, errResp.Error.Code)
	}
}

func TestQueryAndSearchValidation(t *testing.T) {
	server, tmpDir := setupTestServer(t)
	defer os.RemoveAll(tmpDir)

	handler := NewRouter(server)

	// Test Search without query parameter
	reqSearchEmpty := httptest.NewRequest("GET", "/api/v1/ai/search", nil)
	rrSearchEmpty := httptest.NewRecorder()
	handler.ServeHTTP(rrSearchEmpty, reqSearchEmpty)

	if rrSearchEmpty.Code != http.StatusBadRequest {
		t.Errorf("expected status BadRequest (400) for empty search query, got %v", rrSearchEmpty.Code)
	}

	// Test Query without query body
	reqQueryEmpty := httptest.NewRequest("POST", "/api/v1/ai/query", bytes.NewReader([]byte("{}")))
	rrQueryEmpty := httptest.NewRecorder()
	handler.ServeHTTP(rrQueryEmpty, reqQueryEmpty)

	if rrQueryEmpty.Code != http.StatusBadRequest {
		t.Errorf("expected status BadRequest (400) for empty body query, got %v", rrQueryEmpty.Code)
	}
}
