package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/ai/rag"
)

// Config holds the web server configuration.
// Default values match the ADR configuration section.
type Config struct {
	// Host is the address to bind to. Default: "0.0.0.0".
	Host string

	// Port is the port to listen on. Default: 8080.
	Port int

	// CORS holds cross-origin resource sharing settings.
	CORS CORSConfig

	// TemplatesDir is the path to the image-templates/ directory.
	TemplatesDir string
}

// DefaultConfig returns the default server configuration
// as specified in the ADR configuration section.
func DefaultServerConfig() Config {
	return Config{
		Host:         "0.0.0.0",
		Port:         8080,
		CORS:         DefaultCORSConfig(),
		TemplatesDir: "image-templates",
	}
}

// Server is the HTTP API server for the Image Composer Tool.
// It holds references to shared dependencies (the RAG engine, config, etc.)
// and delegates all business logic to the internal/ai/ library.
//
// The Server struct is designed for the full API spec from day one.
// Future phases add new fields and handlers without restructuring.
type Server struct {
	// engine is the RAG engine for AI search and generation.
	// Initialized once at startup, shared across all requests.
	engine *rag.Engine

	// config holds server settings (host, port, CORS, etc.).
	config Config

	// httpServer is the underlying Go HTTP server.
	httpServer *http.Server

	// ── Future fields (uncomment when those phases are implemented) ──
	// sessionMgr  *session.Manager    // Phase 3: conversation sessions
	// buildMgr    *build.Manager      // Phase 5: build tracking
}

// NewServer creates a new API server with the given dependencies.
// It builds the router, applies middleware, and prepares the server
// for starting.
func NewServer(engine *rag.Engine, config Config) *Server {
	s := &Server{
		engine: engine,
		config: config,
	}

	// Build the router with all registered routes.
	handler := NewRouter(s)

	// Apply CORS middleware as the outermost wrapper.
	handler = withCORS(handler, config.CORS)

	addr := fmt.Sprintf("%s:%d", config.Host, config.Port)
	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 300 * time.Second, // 5 minutes — local LLMs (e.g. llama3.1:8b) can be slow
		IdleTimeout:  120 * time.Second,
	}

	return s
}

// Start begins listening for HTTP requests. This call blocks until
// the server is shut down.
func (s *Server) Start() error {
	addr := s.httpServer.Addr
	log.Printf("Starting ICT API server on %s", addr)
	log.Printf("Templates directory: %s", s.config.TemplatesDir)
	log.Printf("CORS allowed origins: %v", s.config.CORS.AllowedOrigins)

	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server failed: %w", err)
	}
	return nil
}

// Shutdown gracefully stops the server, allowing in-flight requests
// to complete within the given context deadline.
func (s *Server) Shutdown(ctx context.Context) error {
	log.Println("Shutting down API server...")
	return s.httpServer.Shutdown(ctx)
}
