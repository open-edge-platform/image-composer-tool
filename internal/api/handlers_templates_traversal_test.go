package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/ai"
	"github.com/open-edge-platform/image-composer-tool/internal/ai/rag"
)

// TestGetTemplatePathTraversal ensures that GET /api/v1/templates/{name}
// cannot be used to read files outside the configured templates directory,
// including via URL-encoded traversal sequences.
func TestGetTemplatePathTraversal(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "trav-templates-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Plant a "secret" YAML file one level above the templates directory.
	secret := filepath.Join(filepath.Dir(tmpDir), "trav-secret.yml")
	if err := os.WriteFile(secret, []byte("image:\n  name: SECRET\n"), 0o644); err != nil {
		t.Fatalf("failed to write secret file: %v", err)
	}
	defer os.Remove(secret)

	config := ai.DefaultConfig()
	config.Provider = ai.ProviderOllama
	config.Cache.Enabled = false
	config.TemplatesDir = tmpDir

	engine, err := rag.NewEngine(config)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	serverConfig := DefaultServerConfig()
	serverConfig.TemplatesDir = tmpDir
	s := NewServer(engine, serverConfig)
	handler := NewRouter(s)

	// filepath.Base(secret) == "trav-secret" once the .yml suffix is trimmed.
	secretName := "trav-secret"

	traversalPaths := []string{
		"/api/v1/templates/" + secretName, // control: does not exist inside dir
		"/api/v1/templates/%2e%2e%2f" + secretName,
		"/api/v1/templates/..%2f" + secretName,
		"/api/v1/templates/%2e%2e/" + secretName,
	}

	for _, p := range traversalPaths {
		req := httptest.NewRequest("GET", p, nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code == http.StatusOK {
			t.Errorf("path %q unexpectedly returned 200 (possible traversal): body=%s",
				p, rr.Body.String())
		}
	}
}
