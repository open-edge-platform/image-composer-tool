package pkgfetcher

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/open-edge-platform/os-image-composer/internal/utils/network"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type partialErrorReader struct {
	content []byte
	read    bool
}

func (r *partialErrorReader) Read(p []byte) (int, error) {
	if !r.read {
		r.read = true
		n := copy(p, r.content)
		return n, nil
	}
	return 0, errors.New("simulated stream failure")
}

func (r *partialErrorReader) Close() error {
	return nil
}

// TestFetchPackages_Success tests successful package downloads
func TestFetchPackages_Success(t *testing.T) {
	// Create temporary directory for downloads
	tempDir, err := os.MkdirTemp("", "pkgfetcher_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Serve different content based on the path
		switch r.URL.Path {
		case "/package1.rpm":
			w.Header().Set("Content-Type", "application/x-rpm")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("mock package1 content"))
		case "/package2.deb":
			w.Header().Set("Content-Type", "application/x-deb")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("mock package2 content"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Test URLs
	urls := []string{
		server.URL + "/package1.rpm",
		server.URL + "/package2.deb",
	}

	// Call FetchPackages
	err = FetchPackages(urls, tempDir, 2)
	if err != nil {
		t.Fatalf("FetchPackages failed: %v", err)
	}

	// Verify files were downloaded
	expectedFiles := []string{"package1.rpm", "package2.deb"}
	for _, filename := range expectedFiles {
		filePath := filepath.Join(tempDir, filename)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			t.Errorf("Expected file %s was not downloaded", filename)
		}

		// Check file content
		content, err := os.ReadFile(filePath)
		if err != nil {
			t.Errorf("Failed to read downloaded file %s: %v", filename, err)
		}

		expectedContent := fmt.Sprintf("mock %s content", strings.TrimSuffix(filename, filepath.Ext(filename)))
		if string(content) != expectedContent {
			t.Errorf("File %s content mismatch. Got: %s, Expected: %s", filename, string(content), expectedContent)
		}
	}
}

// TestFetchPackages_EmptyURLs tests behavior with empty URL list
func TestFetchPackages_EmptyURLs(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pkgfetcher_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	err = FetchPackages([]string{}, tempDir, 1)
	if err != nil {
		t.Errorf("FetchPackages with empty URLs should not return error, got: %v", err)
	}
}

// TestFetchPackages_HTTPErrors tests handling of HTTP errors
func TestFetchPackages_HTTPErrors(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pkgfetcher_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test HTTP server that returns errors
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/notfound.rpm":
			w.WriteHeader(http.StatusNotFound)
		case "/server_error.rpm":
			w.WriteHeader(http.StatusInternalServerError)
		case "/success.rpm":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("success content"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	urls := []string{
		server.URL + "/notfound.rpm",
		server.URL + "/server_error.rpm",
		server.URL + "/success.rpm",
	}

	// This should return an error due to HTTP failures
	err = FetchPackages(urls, tempDir, 1)
	if err == nil {
		t.Errorf("FetchPackages should return error for HTTP failures, got nil")
	}

	// Check that successful download still happened
	successFile := filepath.Join(tempDir, "success.rpm")
	if _, err := os.Stat(successFile); os.IsNotExist(err) {
		t.Errorf("Expected successful file was not downloaded")
	}

	// Check that failed downloads don't create files or create empty files
	notFoundFile := filepath.Join(tempDir, "notfound.rpm")
	if info, err := os.Stat(notFoundFile); err == nil && info.Size() > 0 {
		t.Errorf("Failed download should not create non-empty file")
	}
}

// TestFetchPackages_ExistingFiles tests behavior when files already exist
func TestFetchPackages_ExistingFiles(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pkgfetcher_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test HTTP server
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("new content"))
	}))
	defer server.Close()

	url := server.URL + "/existing.rpm"
	filePath := filepath.Join(tempDir, "existing.rpm")

	// Pre-create a file with content
	err = os.WriteFile(filePath, []byte("existing content"), 0644)
	if err != nil {
		t.Fatalf("Failed to create existing file: %v", err)
	}

	// Call FetchPackages - should skip existing file
	err = FetchPackages([]string{url}, tempDir, 1)
	if err != nil {
		t.Fatalf("FetchPackages failed: %v", err)
	}

	// Check that file was not re-downloaded
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	if string(content) != "existing content" {
		t.Errorf("Existing file should not be overwritten. Got: %s", string(content))
	}

	// Server should not have been called since file already exists
	if requestCount > 0 {
		t.Errorf("Server should not have been called for existing file, but got %d requests", requestCount)
	}
}

// TestFetchPackages_ZeroSizeFile tests re-download of zero-size files
func TestFetchPackages_ZeroSizeFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pkgfetcher_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("new content"))
	}))
	defer server.Close()

	url := server.URL + "/zero_size.rpm"
	filePath := filepath.Join(tempDir, "zero_size.rpm")

	// Pre-create a zero-size file
	err = os.WriteFile(filePath, []byte{}, 0644)
	if err != nil {
		t.Fatalf("Failed to create zero-size file: %v", err)
	}

	// Call FetchPackages - should re-download zero-size file
	err = FetchPackages([]string{url}, tempDir, 1)
	if err != nil {
		t.Fatalf("FetchPackages failed: %v", err)
	}

	// Check that file was re-downloaded
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	if string(content) != "new content" {
		t.Errorf("Zero-size file should be re-downloaded. Got: %s", string(content))
	}
}

// TestFetchPackages_MultipleWorkers tests concurrent downloads
func TestFetchPackages_MultipleWorkers(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pkgfetcher_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test HTTP server with artificial delay
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Small delay to test concurrency
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fmt.Sprintf("content for %s", r.URL.Path)))
	}))
	defer server.Close()

	// Generate multiple URLs
	var urls []string
	for i := 0; i < 5; i++ {
		urls = append(urls, fmt.Sprintf("%s/package%d.rpm", server.URL, i))
	}

	// Test with multiple workers
	start := time.Now()
	err = FetchPackages(urls, tempDir, 3)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("FetchPackages failed: %v", err)
	}

	// Verify all files were downloaded
	for i := 0; i < 5; i++ {
		filename := fmt.Sprintf("package%d.rpm", i)
		filePath := filepath.Join(tempDir, filename)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			t.Errorf("Expected file %s was not downloaded", filename)
		}
	}

	// With 3 workers and 5 files, it should be faster than sequential
	// This is a rough check - actual timing may vary
	expectedMinTime := 10 * time.Millisecond  // at least one request time
	expectedMaxTime := 100 * time.Millisecond // much less than 5 * 10ms

	if duration < expectedMinTime {
		t.Errorf("Duration too short, may not have actually downloaded: %v", duration)
	}
	if duration > expectedMaxTime {
		t.Logf("Duration longer than expected (may be due to system load): %v", duration)
	}
}

// TestFetchPackages_InvalidDestDir tests handling of invalid destination directory
func TestFetchPackages_InvalidDestDir(t *testing.T) {
	// Use a path that cannot be created (e.g., under a file instead of directory)
	tempFile, err := os.CreateTemp("", "pkgfetcher_test_file")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())
	tempFile.Close()

	invalidDestDir := filepath.Join(tempFile.Name(), "subdir")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("test content"))
	}))
	defer server.Close()

	urls := []string{server.URL + "/test.rpm"}

	// This should not panic and should handle the error gracefully
	err = FetchPackages(urls, invalidDestDir, 1)
	if err != nil {
		t.Errorf("FetchPackages should not return error for mkdir failures, got: %v", err)
	}
}

// TestFetchPackages_NetworkError tests handling of network errors
func TestFetchPackages_NetworkError(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pkgfetcher_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Use an invalid URL that will cause network error
	urls := []string{
		"http://non-existent-server-12345.example.com/package.rpm",
	}

	// This should return an error due to network failure
	err = FetchPackages(urls, tempDir, 1)
	if err == nil {
		t.Errorf("FetchPackages should return error for network failures, got nil")
	}
}

// TestFetchPackages_SlowServer tests timeout behavior (if any)
func TestFetchPackages_SlowServer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow test in short mode")
	}

	tempDir, err := os.MkdirTemp("", "pkgfetcher_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create server with very slow response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow server - but not too slow to make test unbearable
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("slow content"))
	}))
	defer server.Close()

	urls := []string{server.URL + "/slow.rpm"}

	start := time.Now()
	err = FetchPackages(urls, tempDir, 1)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("FetchPackages failed: %v", err)
	}

	// Should still complete successfully
	filePath := filepath.Join(tempDir, "slow.rpm")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Errorf("Expected file was not downloaded")
	}

	// Should take at least the delay time
	if duration < 100*time.Millisecond {
		t.Errorf("Download completed too quickly: %v", duration)
	}
}

// TestFetchPackages_RetryOnTransientError verifies retries for transient HTTP failures.
func TestFetchPackages_RetryOnTransientError(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pkgfetcher_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("retry-success"))
	}))
	defer server.Close()

	url := server.URL + "/retry-package.rpm"

	err = FetchPackages([]string{url}, tempDir, 1)
	if err != nil {
		t.Fatalf("FetchPackages failed unexpectedly after retries: %v", err)
	}

	if requestCount != 3 {
		t.Errorf("Expected 3 attempts (2 failures + 1 success), got %d", requestCount)
	}

	filePath := filepath.Join(tempDir, "retry-package.rpm")
	if _, statErr := os.Stat(filePath); os.IsNotExist(statErr) {
		t.Fatalf("Expected file was not downloaded after retries")
	}
}

// TestFetchPackages_NoRetryOnPermanentError verifies no retries for permanent HTTP failures.
func TestFetchPackages_NoRetryOnPermanentError(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pkgfetcher_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	err = FetchPackages([]string{server.URL + "/missing.rpm"}, tempDir, 1)
	if err == nil {
		t.Fatalf("Expected FetchPackages to fail for permanent HTTP error")
	}

	if requestCount != 1 {
		t.Errorf("Expected exactly 1 attempt for 404 response, got %d", requestCount)
	}
}

func TestDownloadWithRetry_TransientThenSuccess(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pkgfetcher_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&requestCount, 1)
		if current <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	destPath := filepath.Join(tempDir, "retry-direct.rpm")
	client := network.GetSecureHTTPClient()

	err = downloadWithRetry(client, server.URL+"/retry-direct.rpm", destPath, 0)
	if err != nil {
		t.Fatalf("downloadWithRetry should succeed after transient failures: %v", err)
	}

	if got := atomic.LoadInt32(&requestCount); got != 3 {
		t.Fatalf("expected 3 attempts, got %d", got)
	}

	if _, statErr := os.Stat(destPath); os.IsNotExist(statErr) {
		t.Fatalf("expected file to be created")
	}
}

func TestDownloadWithRetry_EmptyBodyFailsAfterRetries(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pkgfetcher_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	destPath := filepath.Join(tempDir, "empty-body.rpm")
	client := network.GetSecureHTTPClient()

	err = downloadWithRetry(client, server.URL+"/empty-body.rpm", destPath, 1)
	if err == nil {
		t.Fatalf("expected error when response body is empty")
	}

	if got := atomic.LoadInt32(&requestCount); got != int32(maxDownloadAttempts) {
		t.Fatalf("expected %d attempts, got %d", maxDownloadAttempts, got)
	}
}

func TestDownloadWithRetry_RetryOnContentLengthMistmatch(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pkgfetcher_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	var requestCount int32
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempt := atomic.AddInt32(&requestCount, 1)
			if attempt == 1 {
				return &http.Response{
					StatusCode:    http.StatusOK,
					Status:        "200 OK",
					Body:          io.NopCloser(strings.NewReader("abc")),
					ContentLength: 10,
					Header:        make(http.Header),
					Request:       req,
				}, nil
			}

			return &http.Response{
				StatusCode:    http.StatusOK,
				Status:        "200 OK",
				Body:          io.NopCloser(strings.NewReader("0123456789")),
				ContentLength: 10,
				Header:        make(http.Header),
				Request:       req,
			}, nil
		}),
	}

	destPath := filepath.Join(tempDir, "content-length-mismatch.rpm")
	err = downloadWithRetry(client, "http://example.test/content-length-mismatch.rpm", destPath, -1)
	if err != nil {
		t.Fatalf("expected retry to recover from content-length mismatch, got: %v", err)
	}

	if got := atomic.LoadInt32(&requestCount); got != 2 {
		t.Fatalf("expected 2 attempts (mismatch + success), got %d", got)
	}

	content, readErr := os.ReadFile(destPath)
	if readErr != nil {
		t.Fatalf("failed to read downloaded file: %v", readErr)
	}
	if string(content) != "0123456789" {
		t.Fatalf("unexpected final content: %q", string(content))
	}
}

func TestDownloadWithRetry_RemovaPartialFileOnError(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pkgfetcher_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	var requestCount int32
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			atomic.AddInt32(&requestCount, 1)
			return &http.Response{
				StatusCode:    http.StatusOK,
				Status:        "200 OK",
				Body:          &partialErrorReader{content: []byte("partial")},
				ContentLength: -1,
				Header:        make(http.Header),
				Request:       req,
			}, nil
		}),
	}

	destPath := filepath.Join(tempDir, "partial-file.rpm")
	err = downloadWithRetry(client, "http://example.test/partial-file.rpm", destPath, -1)
	if err == nil {
		t.Fatalf("expected error when stream fails after partial write")
	}

	if got := atomic.LoadInt32(&requestCount); got != int32(maxDownloadAttempts) {
		t.Fatalf("expected %d attempts, got %d", maxDownloadAttempts, got)
	}

	if _, statErr := os.Stat(destPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected partial file to be removed, statErr=%v", statErr)
	}
}
