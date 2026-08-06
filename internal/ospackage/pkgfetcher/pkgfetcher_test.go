package pkgfetcher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/network"
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
	err = FetchPackages(context.Background(), urls, tempDir, 2)
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

	err = FetchPackages(context.Background(), []string{}, tempDir, 1)
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
	err = FetchPackages(context.Background(), urls, tempDir, 1)
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
	err = FetchPackages(context.Background(), []string{url}, tempDir, 1)
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
	err = FetchPackages(context.Background(), []string{url}, tempDir, 1)
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
	err = FetchPackages(context.Background(), urls, tempDir, 3)
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
	err = FetchPackages(context.Background(), urls, invalidDestDir, 1)
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
	err = FetchPackages(context.Background(), urls, tempDir, 1)
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
	err = FetchPackages(context.Background(), urls, tempDir, 1)
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

	err = FetchPackages(context.Background(), []string{url}, tempDir, 1)
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

	err = FetchPackages(context.Background(), []string{server.URL + "/missing.rpm"}, tempDir, 1)
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

	err = downloadWithRetry(context.Background(), client, server.URL+"/retry-direct.rpm", destPath, 0)
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

	err = downloadWithRetry(context.Background(), client, server.URL+"/empty-body.rpm", destPath, 1)
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
	err = downloadWithRetry(context.Background(), client, "http://example.test/content-length-mismatch.rpm", destPath, -1)
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
	err = downloadWithRetry(context.Background(), client, "http://example.test/partial-file.rpm", destPath, -1)
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

// TestFetchPackages_PlusEncodedAsPercentTwoBInURL verifies that FetchPackages
// encodes '+' as '%2B' in the HTTP request URL (S3/CloudFront treats literal
// '+' as space), while the destination filename retains the original '+'.
func TestFetchPackages_PlusEncodedAsPercentTwoBInURL(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pkgfetcher_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	var receivedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.RawPath
		if receivedPath == "" {
			// RawPath is empty when the path has no encoded characters after Go
			// normalisation, so fall back to RequestURI which preserves encoding.
			receivedPath = r.RequestURI
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("eci-package-content"))
	}))
	defer server.Close()

	// Simulate an ECI package filename containing '+' characters.
	filename := "systemd_255.4-1ubuntu8.12-ecir8+etf+taprio_amd64.deb"
	inputURL := server.URL + "/pool/main/s/systemd/" + filename

	err = FetchPackages(context.Background(), []string{inputURL}, tempDir, 1)
	if err != nil {
		t.Fatalf("FetchPackages failed: %v", err)
	}

	// The server must have received '%2B' instead of '+'.
	if !strings.Contains(receivedPath, "%2B") {
		t.Errorf("server received path with literal '+' instead of '%%2B': %s", receivedPath)
	}
	if strings.Contains(receivedPath, "+") {
		t.Errorf("server path still contains literal '+': %s", receivedPath)
	}

	// The destination file must retain the original '+' in its name.
	destPath := filepath.Join(tempDir, filename)
	if _, statErr := os.Stat(destPath); os.IsNotExist(statErr) {
		t.Fatalf("expected destination file with '+' in name to exist: %s", destPath)
	}

	content, readErr := os.ReadFile(destPath)
	if readErr != nil {
		t.Fatalf("failed to read downloaded file: %v", readErr)
	}
	if string(content) != "eci-package-content" {
		t.Fatalf("unexpected file content: %q", string(content))
	}
}

// TestFetchPackages_CancelledContextExitsPromptly verifies that when the
// ambient ctx is cancelled after workers have started but before the queue
// drains, FetchPackages returns within a bounded time with a
// context.Canceled-wrapping error rather than blocking on the retry loops.
// Regression guard for the "SIGINT during download hangs the process for
// 5+ minutes" bug caught in manual smoke testing.
func TestFetchPackages_CancelledContextExitsPromptly(t *testing.T) {
	// Server that never responds — the goal is to sit in client.Do until
	// ctx cancels the request.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	tempDir := t.TempDir()

	// Enqueue more URLs than workers so the jobs channel has depth; if the
	// worker loop's ctx-gate is wrong, extra URLs would keep the pool busy
	// after the first cancel.
	urls := make([]string, 20)
	for i := range urls {
		urls[i] = fmt.Sprintf("%s/slow-%d.rpm", server.URL, i)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := FetchPackages(ctx, urls, tempDir, 4)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected error after context cancel, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected wrapped context.Canceled, got: %v", err)
	}
	// Generous ceiling: workers should exit within a couple of retry-backoff
	// windows (initialRetryBackoff=500ms, exponential). We allow 10s so CI
	// jitter and the http.RoundTripper's own graceful-close doesn't flake this.
	if elapsed > 10*time.Second {
		t.Fatalf("FetchPackages took %s to exit after cancel; expected <10s", elapsed)
	}
}

// --- failure reporting ---

// TestFetchPackages_ErrorNamesFailures covers the diagnosability gap behind a
// real build failure: 858 packages were requested, exactly one 404'd (a stale
// cached version the mirror had superseded), and the returned error said only
// "one or more downloads failed" — leaving the actionable detail buried thousands
// of progress-bar lines deep in the log. The error must name the package and the
// reason.
func TestFetchPackages_ErrorNamesFailures(t *testing.T) {
	tempDir := t.TempDir()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/good.deb" {
			_, _ = w.Write([]byte("content"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	err := FetchPackages(context.Background(), []string{
		server.URL + "/good.deb",
		server.URL + "/ntfs-3g_2022.10.3-5+deb13u1_amd64.deb",
	}, tempDir, 1)
	if err == nil {
		t.Fatal("expected an error when a package 404s")
	}
	// The count locates the failure against the whole set: 1 of 2 is a bad package,
	// 2 of 2 would be a bad network.
	if !strings.Contains(err.Error(), "1 of 2 package downloads failed") {
		t.Errorf("error should count failures against the total, got: %v", err)
	}
	// The offending package must be named — this is the whole point.
	if !strings.Contains(err.Error(), "ntfs-3g_2022.10.3-5") {
		t.Errorf("error should name the failed package, got: %v", err)
	}
	// ...along with why it failed, so a 404 (stale metadata) is distinguishable
	// from a connection error (bad proxy).
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error should carry the underlying reason, got: %v", err)
	}
	// The package that succeeded must not be listed as a failure.
	if strings.Contains(err.Error(), "good.deb") {
		t.Errorf("error should not name the successful download, got: %v", err)
	}
}

// A dead proxy fails every package. The error stays readable by truncating, but
// must say how many it omitted — a silent cut would understate the scale and
// suggest a single bad package rather than a broken network.
func TestFetchPackages_ErrorTruncatesManyFailures(t *testing.T) {
	tempDir := t.TempDir()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	total := maxReportedFailures + 3
	urls := make([]string, 0, total)
	for i := 0; i < total; i++ {
		urls = append(urls, fmt.Sprintf("%s/pkg%d.deb", server.URL, i))
	}

	err := FetchPackages(context.Background(), urls, tempDir, 4)
	if err == nil {
		t.Fatal("expected an error when every package 404s")
	}
	msg := err.Error()
	if !strings.Contains(msg, fmt.Sprintf("%d of %d package downloads failed", total, total)) {
		t.Errorf("error should report every package as failed, got: %v", err)
	}
	if !strings.Contains(msg, "and 3 more") {
		t.Errorf("error should report the omitted count, got: %v", err)
	}
	// Only maxReportedFailures items are listed.
	if got := strings.Count(msg, "\n  - "); got != maxReportedFailures {
		t.Errorf("listed %d failures, want %d", got, maxReportedFailures)
	}
}

// A cancelled fetch must keep reporting cancellation rather than being reframed
// as a pile of download failures: the two need different responses from the user.
func TestFetchPackages_CancelNotReportedAsFailures(t *testing.T) {
	tempDir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := FetchPackages(ctx, []string{"http://example.invalid/pkg.deb"}, tempDir, 1)
	if err == nil {
		t.Fatal("expected an error for a cancelled fetch")
	}
	if !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("cancellation should be surfaced as such, got: %v", err)
	}
	if strings.Contains(err.Error(), "package downloads failed") {
		t.Errorf("cancellation must not be reported as download failures, got: %v", err)
	}
}

// summarizeFailures is called on the error path only, but a downloadError set
// without a recorded URL (the ctx-drain branch) must not produce a dangling
// "failed:" with nothing after it.
func TestSummarizeFailuresEmpty(t *testing.T) {
	if got := summarizeFailures(nil); got != "" {
		t.Errorf("summarizeFailures(nil) = %q, want empty", got)
	}
}

// blockingReader returns firstChunk on the first Read, then blocks forever on
// the next Read until Close is called (which unblocks it with an error). It
// models a proxy that sends some body bytes and then goes silent mid-transfer —
// the exact stall that hung a real build (headers received, ~11MB streamed,
// then the connection sat ESTAB with no further data).
type blockingReader struct {
	firstChunk []byte
	sent       bool
	closed     chan struct{}
	once       sync.Once
}

func newBlockingReader(firstChunk []byte) *blockingReader {
	return &blockingReader{firstChunk: firstChunk, closed: make(chan struct{})}
}

func (b *blockingReader) Read(p []byte) (int, error) {
	if !b.sent {
		b.sent = true
		return copy(p, b.firstChunk), nil
	}
	<-b.closed // block until Close, mirroring a silent stalled socket
	return 0, errors.New("read after close")
}

func (b *blockingReader) Close() error {
	b.once.Do(func() { close(b.closed) })
	return nil
}

// TestIdleTimeoutReader_TripsOnStall verifies that a body which delivers some
// bytes and then goes silent is aborted once no data arrives for the idle
// window, surfacing a "stalled" error rather than blocking forever. This is the
// direct regression guard for the hung-build bug: bodyIdleTimeout is 60s in
// production, so the mechanism is exercised here with a short window.
func TestIdleTimeoutReader_TripsOnStall(t *testing.T) {
	br := newBlockingReader([]byte("first"))
	r := newIdleTimeoutReader(br, 100*time.Millisecond)
	defer r.stop()

	start := time.Now()
	_, err := io.ReadAll(r)
	if err == nil {
		t.Fatal("expected a stall error, got nil")
	}
	if !strings.Contains(err.Error(), "stalled") {
		t.Fatalf("expected a stalled error, got: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("reader took too long to trip: %s", elapsed)
	}
}

// TestIdleTimeoutReader_ProgressDoesNotTrip verifies that a body which keeps
// delivering bytes — even slowly, in gaps shorter than the idle window — is
// never aborted, so a slow-but-progressing large package download is not
// killed. Each read resets the idle timer.
func TestIdleTimeoutReader_ProgressDoesNotTrip(t *testing.T) {
	// Feed 20 chunks with a 50ms gap against a 1s idle window; each gap sits
	// well under the window (20x headroom, so CI scheduling jitter cannot
	// falsely trip it) while the total time (~1s) still exceeds the window,
	// proving the timer tracks idleness, not total duration.
	const chunks = 20
	pr, pw := io.Pipe()
	go func() {
		for i := 0; i < chunks; i++ {
			time.Sleep(50 * time.Millisecond)
			if _, err := pw.Write([]byte("chunk")); err != nil {
				return
			}
		}
		_ = pw.Close()
	}()

	r := newIdleTimeoutReader(pr, 1*time.Second)
	defer r.stop()

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("progressing reader should not trip the idle timeout: %v", err)
	}
	if got, want := len(data), len("chunk")*chunks; got != want {
		t.Fatalf("read %d bytes, want %d", got, want)
	}
}
