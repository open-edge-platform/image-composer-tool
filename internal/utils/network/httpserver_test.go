package network

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestServeRepositoryHTTP_Success(t *testing.T) {
	// Create a temporary repository directory
	repoDir := t.TempDir()

	// Create some test files
	testFiles := map[string]string{
		"repodata/repomd.xml": `<?xml version="1.0" encoding="UTF-8"?>
<repomd xmlns="http://linux.duke.edu/metadata/repo" xmlns:rpm="http://linux.duke.edu/metadata/rpm">
  <revision>1234567890</revision>
</repomd>`,
		"Packages/test-package.rpm": "fake rpm content",
		"index.html":                "<html><body>Repository Index</body></html>",
	}

	for filePath, content := range testFiles {
		fullPath := filepath.Join(repoDir, filePath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("failed to create directory for %s: %v", filePath, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to create test file %s: %v", filePath, err)
		}
	}

	// Start the HTTP server
	serverURL, cleanup, err := ServeRepositoryHTTP(repoDir)
	if err != nil {
		t.Fatalf("ServeRepositoryHTTP failed: %v", err)
	}
	defer cleanup()

	// Verify server URL format
	if !strings.HasPrefix(serverURL, "http://localhost:") {
		t.Errorf("expected serverURL to start with 'http://localhost:', got %s", serverURL)
	}

	// Test fetching files
	tests := []struct {
		name         string
		path         string
		expectedCode int
		contains     string
	}{
		{
			name:         "repomd.xml",
			path:         "/repodata/repomd.xml",
			expectedCode: 200,
			contains:     "<repomd",
		},
		{
			name:         "rpm package",
			path:         "/Packages/test-package.rpm",
			expectedCode: 200,
			contains:     "fake rpm content",
		},
		{
			name:         "index file",
			path:         "/index.html",
			expectedCode: 200,
			contains:     "Repository Index",
		},
		{
			name:         "non-existent file",
			path:         "/nonexistent.txt",
			expectedCode: 404,
			contains:     "",
		},
	}

	client := &http.Client{Timeout: 5 * time.Second}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := client.Get(serverURL + tt.path)
			if err != nil {
				t.Fatalf("failed to make HTTP request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.expectedCode {
				t.Errorf("expected status code %d, got %d", tt.expectedCode, resp.StatusCode)
			}

			if tt.contains != "" {
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatalf("failed to read response body: %v", err)
				}

				if !strings.Contains(string(body), tt.contains) {
					t.Errorf("expected response to contain '%s', got: %s", tt.contains, string(body))
				}
			}
		})
	}
}

func TestServeRepositoryHTTP_InvalidDirectory(t *testing.T) {
	nonExistentDir := "/nonexistent/directory/path"

	_, _, err := ServeRepositoryHTTP(nonExistentDir)
	if err == nil {
		t.Errorf("expected error for non-existent directory, got nil")
	}
}

func TestServeRepositoryHTTP_PathIsFile(t *testing.T) {
	tempFile, err := os.CreateTemp(t.TempDir(), "repo-file-*.txt")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if err := tempFile.Close(); err != nil {
		t.Fatalf("failed to close temp file: %v", err)
	}

	_, _, err = ServeRepositoryHTTP(tempFile.Name())
	if err == nil {
		t.Fatalf("expected error for file path, got nil")
	}

	if !strings.Contains(err.Error(), "repository path is not a directory") {
		t.Fatalf("expected non-directory error, got: %v", err)
	}
}

func TestServeRepositoryHTTP_Cleanup(t *testing.T) {
	repoDir := t.TempDir()

	// Create a test file
	testFile := filepath.Join(repoDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	serverURL, cleanup, err := ServeRepositoryHTTP(repoDir)
	if err != nil {
		t.Fatalf("ServeRepositoryHTTP failed: %v", err)
	}

	// Verify server is working
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(serverURL + "/test.txt")
	if err != nil {
		t.Fatalf("failed to make HTTP request before cleanup: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected status 200 before cleanup, got %d", resp.StatusCode)
	}

	// Call cleanup
	cleanup()

	// Give the server time to shut down
	time.Sleep(100 * time.Millisecond)

	// Verify server is no longer accessible
	_, err = client.Get(serverURL + "/test.txt")
	if err == nil {
		t.Errorf("expected error after cleanup, but server still accessible")
	}
}

func TestServeRepositoryHTTP_MultipleServers(t *testing.T) {
	var servers []struct {
		url     string
		cleanup func()
	}
	defer func() {
		// Cleanup all servers
		for _, server := range servers {
			server.cleanup()
		}
	}()

	// Start multiple servers
	for i := 0; i < 3; i++ {
		repoDir := t.TempDir()

		// Create unique test content for each server
		content := fmt.Sprintf("Server %d content", i)
		testFile := filepath.Join(repoDir, "server.txt")
		if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
			t.Fatalf("failed to create test file for server %d: %v", i, err)
		}

		serverURL, cleanup, err := ServeRepositoryHTTP(repoDir)
		if err != nil {
			t.Fatalf("failed to start server %d: %v", i, err)
		}

		servers = append(servers, struct {
			url     string
			cleanup func()
		}{serverURL, cleanup})
	}

	// Verify all servers are working and serving different content
	client := &http.Client{Timeout: 5 * time.Second}
	for i, server := range servers {
		resp, err := client.Get(server.url + "/server.txt")
		if err != nil {
			t.Fatalf("failed to access server %d: %v", i, err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("failed to read response from server %d: %v", i, err)
		}

		expectedContent := fmt.Sprintf("Server %d content", i)
		if string(body) != expectedContent {
			t.Errorf("server %d returned wrong content: expected '%s', got '%s'", i, expectedContent, string(body))
		}
	}

	// Verify all servers have different URLs (different ports)
	urls := make(map[string]bool)
	for i, server := range servers {
		if urls[server.url] {
			t.Errorf("server %d has duplicate URL: %s", i, server.url)
		}
		urls[server.url] = true
	}
}

func TestServeRepositoryHTTP_ConcurrentAccess(t *testing.T) {
	repoDir := t.TempDir()

	// Create test file
	testContent := "concurrent test content"
	testFile := filepath.Join(repoDir, "concurrent.txt")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	serverURL, cleanup, err := ServeRepositoryHTTP(repoDir)
	if err != nil {
		t.Fatalf("ServeRepositoryHTTP failed: %v", err)
	}
	defer cleanup()

	// Make concurrent requests
	const numRequests = 10
	var wg sync.WaitGroup
	errChan := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(requestID int) {
			defer wg.Done()

			client := &http.Client{Timeout: 5 * time.Second}
			resp, err := client.Get(serverURL + "/concurrent.txt")
			if err != nil {
				errChan <- fmt.Errorf("request %d failed: %v", requestID, err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != 200 {
				errChan <- fmt.Errorf("request %d got status %d", requestID, resp.StatusCode)
				return
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				errChan <- fmt.Errorf("request %d failed to read body: %v", requestID, err)
				return
			}

			if string(body) != testContent {
				errChan <- fmt.Errorf("request %d got wrong content", requestID)
				return
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	// Check for any errors
	for err := range errChan {
		t.Error(err)
	}
}

func TestServeRepositoryHTTP_DirectoryListing(t *testing.T) {
	repoDir := t.TempDir()

	// Create subdirectories and files
	subdirs := []string{"repodata", "Packages", "SRPMS"}
	for _, subdir := range subdirs {
		if err := os.MkdirAll(filepath.Join(repoDir, subdir), 0755); err != nil {
			t.Fatalf("failed to create subdirectory %s: %v", subdir, err)
		}
	}

	serverURL, cleanup, err := ServeRepositoryHTTP(repoDir)
	if err != nil {
		t.Fatalf("ServeRepositoryHTTP failed: %v", err)
	}
	defer cleanup()

	// Test directory listing
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(serverURL + "/")
	if err != nil {
		t.Fatalf("failed to make HTTP request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected status 200 for directory listing, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	bodyStr := string(body)
	for _, subdir := range subdirs {
		if !strings.Contains(bodyStr, subdir) {
			t.Errorf("directory listing should contain '%s', but it doesn't. Body: %s", subdir, bodyStr)
		}
	}
}
