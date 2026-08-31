package api

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

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
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("failed to create rag engine: %v", err)
	}

	serverConfig := DefaultServerConfig()
	serverConfig.TemplatesDir = tmpDir
	serverConfig.CORS.AllowedOrigins = []string{"http://localhost:3000", "https://example.com"}

	s := NewServer(engine, serverConfig)
	return s, tmpDir
}

func TestDefaultServerConfig(t *testing.T) {
	config := DefaultServerConfig()

	if config.Host != "127.0.0.1" {
		t.Errorf("expected host '127.0.0.1', got '%s'", config.Host)
	}
	if config.Port != 8080 {
		t.Errorf("expected port 8080, got %d", config.Port)
	}
	if config.TemplatesDir != "image-templates" {
		t.Errorf("expected templates dir 'image-templates', got '%s'", config.TemplatesDir)
	}
	if len(config.CORS.AllowedOrigins) == 0 {
		t.Error("expected default CORS allowed origins to be non-empty")
	}
	if config.Session.MaxSessions != 100 {
		t.Errorf("expected max sessions 100, got %d", config.Session.MaxSessions)
	}
}

func TestNewServer(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "server-test-*")
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
		t.Fatalf("failed to create engine: %v", err)
	}

	serverConfig := DefaultServerConfig()
	serverConfig.TemplatesDir = tmpDir

	s := NewServer(engine, serverConfig)
	defer s.sessionMgr.Stop()

	if s.engine != engine {
		t.Error("expected engine to be set")
	}
	if s.sessionMgr == nil {
		t.Error("expected session manager to be initialized")
	}
	if s.httpServer == nil {
		t.Error("expected HTTP server to be initialized")
	}
}

// TestServerStartAndShutdown verifies the Start and Shutdown lifecycle.
// It binds to port 0 (OS-assigned) so the test does not conflict with
// other services, starts the server in a goroutine, then gracefully shuts down.
func TestServerStartAndShutdown(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lifecycle-test-*")
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
		t.Fatalf("failed to create engine: %v", err)
	}

	serverConfig := DefaultServerConfig()
	serverConfig.Host = "127.0.0.1"
	serverConfig.Port = 0 // OS-assigned port
	serverConfig.TemplatesDir = tmpDir

	s := NewServer(engine, serverConfig)

	// Start in background.
	startErr := make(chan error, 1)
	go func() {
		startErr <- s.Start()
	}()

	// Give the server a moment to start listening.
	time.Sleep(50 * time.Millisecond)

	// Graceful shutdown.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	// Start should return nil (http.ErrServerClosed is swallowed).
	select {
	case err := <-startErr:
		if err != nil {
			t.Errorf("Start returned unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after Shutdown")
	}
}

// TestShutdown_NilSessionMgr verifies Shutdown handles a nil sessionMgr
// gracefully (defensive check in the source code).
func TestShutdown_NilSessionMgr(t *testing.T) {
	config := DefaultServerConfig()
	config.Host = "127.0.0.1"
	config.Port = 0

	// Construct a server manually with nil sessionMgr to test the nil guard.
	s := &Server{
		config: config,
		httpServer: &http.Server{
			Addr: "127.0.0.1:0",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Should not panic even with nil sessionMgr.
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}
}
