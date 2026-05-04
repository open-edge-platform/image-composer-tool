package network

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
)

const (
	serverReadHeaderTimeout = 10 * time.Second
	serverReadTimeout       = 30 * time.Second
)

// ServeRepositoryHTTP starts a temporary HTTP server to serve the repository
// Returns the server URL, cleanup function, and error
func ServeRepositoryHTTP(repoPath string) (serverURL string, cleanup func(), err error) {
	log := logger.Logger()

	// Validate repository path exists
	repoInfo, err := os.Stat(repoPath)
	if os.IsNotExist(err) {
		return "", nil, fmt.Errorf("repository directory does not exist: %s", repoPath)
	}
	if err != nil {
		return "", nil, fmt.Errorf("failed to access repository directory: %w", err)
	}
	if !repoInfo.IsDir() {
		return "", nil, fmt.Errorf("repository path is not a directory: %s", repoPath)
	}

	// Bind to localhost with an ephemeral port and keep the socket open to avoid port races.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, fmt.Errorf("failed to find available port: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	// Create HTTP server with file handler
	mux := http.NewServeMux()
	fileHandler := http.FileServer(http.Dir(repoPath))
	mux.Handle("/", fileHandler)

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: serverReadHeaderTimeout,
		ReadTimeout:       serverReadTimeout,
	}

	// Start server in goroutine
	serverErrChan := make(chan error, 1)
	go func() {
		log.Infof("starting HTTP server for repository at http://localhost:%d", port)
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			serverErrChan <- err
		}
		close(serverErrChan)
	}()

	if err := waitForHTTPServerReady(serverErrChan, port); err != nil {
		_ = server.Close()
		return "", nil, err
	}

	serverURL = fmt.Sprintf("http://localhost:%d", port)
	log.Infof("repository available at: %s", serverURL)

	// Cleanup function
	cleanup = func() {
		log.Infof("shutting down HTTP server for repository")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			log.Errorf("error shutting down HTTP server: %v", err)
		}
	}

	return serverURL, cleanup, nil
}

func waitForHTTPServerReady(serverErrChan <-chan error, port int) error {
	const (
		readinessTimeout  = 5 * time.Second
		readinessInterval = 50 * time.Millisecond
		probeTimeout      = 500 * time.Millisecond
	)

	client := &http.Client{Timeout: probeTimeout}
	probeURL := fmt.Sprintf("http://localhost:%d/", port)
	timeout := time.NewTimer(readinessTimeout)
	defer timeout.Stop()
	ticker := time.NewTicker(readinessInterval)
	defer ticker.Stop()

	var lastProbeErr error
	for {
		resp, err := client.Get(probeURL)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			return nil
		}
		lastProbeErr = err

		select {
		case serveErr, ok := <-serverErrChan:
			if ok && serveErr != nil {
				return fmt.Errorf("failed to start HTTP server: %w", serveErr)
			}
			return fmt.Errorf("HTTP server exited before becoming ready")
		case <-ticker.C:
		case <-timeout.C:
			if lastProbeErr != nil {
				return fmt.Errorf("timed out waiting for HTTP server readiness: %w", lastProbeErr)
			}
			return fmt.Errorf("timed out waiting for HTTP server readiness")
		}
	}
}
