package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	ai "github.com/open-edge-platform/image-composer-tool/internal/ai"
	"github.com/open-edge-platform/image-composer-tool/internal/ai/rag"
	"github.com/open-edge-platform/image-composer-tool/internal/api"
)

// createServeCommand builds the "serve" subcommand for starting the web
// API server. This is just the entry point — once the server is running,
// all interaction happens over HTTP using the Go libraries directly.
func createServeCommand() *cobra.Command {
	var port int
	var host string
	var templatesDir string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the web API server",
		Long: `Start the ICT web API server. The server provides a REST API
for AI-powered template generation, template management, and image builds.

All operations call the Go libraries in internal/ai/ directly. The only
exception is image builds which spawn a subprocess (not in this phase).

Example:
  image-composer-tool serve
  image-composer-tool serve --port 9090
  image-composer-tool serve --templates-dir /path/to/templates`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(host, port, templatesDir)
		},
	}

	cmd.Flags().IntVar(&port, "port", 8080, "Port to listen on")
	cmd.Flags().StringVar(&host, "host", "127.0.0.1", "Host address to bind to (use 0.0.0.0 to expose on all interfaces; note the API is currently unauthenticated)")
	cmd.Flags().StringVar(&templatesDir, "templates-dir", "", "Path to image-templates/ directory (default: auto-detect)")

	return cmd
}

// runServe initializes the RAG engine and starts the API server.
func runServe(host string, port int, templatesDir string) error {
	// ── Step 1: Build AI config ─────────────────────────────────────────
	config := ai.DefaultConfig()

	// Override templates directory if specified via CLI flag.
	if templatesDir != "" {
		config.TemplatesDir = templatesDir
	}

	log.Printf("Using AI provider: %s", config.Provider)
	log.Printf("Using templates directory: %s", config.TemplatesDir)

	// ── Step 2: Create the RAG engine (Go library, not CLI) ─────────────
	engine, err := rag.NewEngine(config)
	if err != nil {
		return err
	}

	// ── Step 3: Initialize (index templates into memory — one-time) ─────
	ctx := context.Background()
	log.Println("Initializing RAG engine (indexing templates)...")
	if err := engine.Initialize(ctx); err != nil {
		return err
	}
	log.Println("RAG engine initialized successfully")

	// ── Step 4: Configure and create the API server ─────────────────────
	serverConfig := api.DefaultServerConfig()
	serverConfig.Host = host
	serverConfig.Port = port
	serverConfig.TemplatesDir = config.TemplatesDir

	server := api.NewServer(engine, serverConfig)

	// ── Step 5: Handle graceful shutdown on SIGINT/SIGTERM ───────────────
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	// Run the server in a goroutine so we can listen for shutdown signals.
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start()
	}()

	// Wait for either a shutdown signal or a server error.
	select {
	case sig := <-shutdown:
		log.Printf("Received signal %v, shutting down...", sig)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
