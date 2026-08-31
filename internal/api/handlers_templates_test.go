package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"os"
)

func TestHandleValidate_MissingYAML(t *testing.T) {
	config := DefaultServerConfig()
	s := NewServer(nil, config)

	body := []byte(`{"yaml": ""}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/templates/validate", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	router := NewRouter(s)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var res validationResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if res.Valid {
		t.Errorf("expected Valid to be false")
	}
	if len(res.Errors) == 0 {
		t.Errorf("expected validation errors")
	}
}

func TestHandleValidate_InvalidYAMLSyntax(t *testing.T) {
	config := DefaultServerConfig()
	s := NewServer(nil, config)

	body := []byte(`{"yaml": "invalid: yaml: syntax\n  foo"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/templates/validate", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	router := NewRouter(s)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var res validationResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if res.Valid {
		t.Errorf("expected Valid to be false")
	}
	if len(res.Errors) == 0 || res.Errors[0].Message == "" {
		t.Errorf("expected invalid YAML syntax error")
	}
}

func TestHandleValidate_SchemaFailure(t *testing.T) {
	config := DefaultServerConfig()
	s := NewServer(nil, config)

	// Missing 'target' or 'image' which are required by JSON schema.
	yamlStr := `
image:
  name: my-custom-image
`
	reqBody, _ := json.Marshal(templateValidateRequest{YAML: yamlStr})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/templates/validate", bytes.NewBuffer(reqBody))
	req.Header.Set("Content-Type", "application/json")

	router := NewRouter(s)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var res validationResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if res.Valid {
		t.Errorf("expected Valid to be false (schema failure)")
	}
	if len(res.Errors) == 0 {
		t.Errorf("expected schema validation errors")
	}
}

func TestHandleValidate_Success(t *testing.T) {
	config := DefaultServerConfig()
	s := NewServer(nil, config)

	// A minimal valid YAML that satisfies the ICT JSON schema.
	yamlStr := `
image:
  name: test-image
  version: "1.0"
target:
  os: wind-river-elxr
  dist: elxr12
  arch: x86_64
  imageType: raw
`
	reqBody, _ := json.Marshal(templateValidateRequest{YAML: yamlStr})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/templates/validate", bytes.NewBuffer(reqBody))
	req.Header.Set("Content-Type", "application/json")

	router := NewRouter(s)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var res validationResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !res.Valid {
		t.Errorf("expected Valid to be true, got false. Errors: %v", res.Errors)
	}
}

func TestTemplatesHandlers(t *testing.T) {
	server, tmpDir := setupTestServer(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

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

func TestConvertYAMLToJSONCompatible(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		checkFn func(t *testing.T, result interface{})
	}{
		{
			name: "map[string]interface{}",
			input: map[string]interface{}{
				"name":    "test",
				"version": "1.0",
			},
			checkFn: func(t *testing.T, result interface{}) {
				m, ok := result.(map[string]interface{})
				if !ok {
					t.Fatalf("expected map[string]interface{}, got %T", result)
				}
				if m["name"] != "test" {
					t.Errorf("expected name=test, got %v", m["name"])
				}
			},
		},
		{
			name: "map[interface{}]interface{} converted to string keys",
			input: map[interface{}]interface{}{
				"image": map[interface{}]interface{}{
					"name": "nested",
					42:     "numeric-key",
				},
			},
			checkFn: func(t *testing.T, result interface{}) {
				m, ok := result.(map[string]interface{})
				if !ok {
					t.Fatalf("expected map[string]interface{}, got %T", result)
				}
				inner, ok := m["image"].(map[string]interface{})
				if !ok {
					t.Fatalf("expected nested map[string]interface{}, got %T", m["image"])
				}
				if inner["name"] != "nested" {
					t.Errorf("expected name=nested, got %v", inner["name"])
				}
				if inner["42"] != "numeric-key" {
					t.Errorf("expected 42=numeric-key, got %v", inner["42"])
				}
			},
		},
		{
			name:  "[]interface{} with mixed types",
			input: []interface{}{"nginx", "curl", 42},
			checkFn: func(t *testing.T, result interface{}) {
				arr, ok := result.([]interface{})
				if !ok {
					t.Fatalf("expected []interface{}, got %T", result)
				}
				if len(arr) != 3 {
					t.Fatalf("expected 3 elements, got %d", len(arr))
				}
				if arr[0] != "nginx" {
					t.Errorf("expected first element 'nginx', got %v", arr[0])
				}
			},
		},
		{
			name:  "scalar passthrough",
			input: "hello",
			checkFn: func(t *testing.T, result interface{}) {
				if result != "hello" {
					t.Errorf("expected 'hello', got %v", result)
				}
			},
		},
		{
			name:  "nil passthrough",
			input: nil,
			checkFn: func(t *testing.T, result interface{}) {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertYAMLToJSONCompatible(tt.input)
			tt.checkFn(t, result)
		})
	}
}

func TestHandleValidate_BadJSON(t *testing.T) {
	config := DefaultServerConfig()
	s := NewServer(nil, config)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/templates/validate", bytes.NewBuffer([]byte("{bad json")))
	req.Header.Set("Content-Type", "application/json")

	router := NewRouter(s)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d for bad JSON, got %d", http.StatusBadRequest, w.Code)
	}
}
