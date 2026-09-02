package rpmutils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

func TestResult(t *testing.T) {
	tests := []struct {
		name     string
		result   Result
		expected Result
	}{
		{
			name: "successful verification",
			result: Result{
				Path:     "/path/to/test.rpm",
				OK:       true,
				Duration: time.Millisecond * 100,
				Error:    nil,
			},
			expected: Result{
				Path:     "/path/to/test.rpm",
				OK:       true,
				Duration: time.Millisecond * 100,
				Error:    nil,
			},
		},
		{
			name: "failed verification",
			result: Result{
				Path:     "/path/to/bad.rpm",
				OK:       false,
				Duration: time.Millisecond * 50,
				Error:    fmt.Errorf("verification failed"),
			},
			expected: Result{
				Path:     "/path/to/bad.rpm",
				OK:       false,
				Duration: time.Millisecond * 50,
				Error:    fmt.Errorf("verification failed"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.result.Path != tt.expected.Path {
				t.Errorf("Path = %v, want %v", tt.result.Path, tt.expected.Path)
			}
			if tt.result.OK != tt.expected.OK {
				t.Errorf("OK = %v, want %v", tt.result.OK, tt.expected.OK)
			}
			if tt.result.Duration != tt.expected.Duration {
				t.Errorf("Duration = %v, want %v", tt.result.Duration, tt.expected.Duration)
			}
			if (tt.result.Error == nil) != (tt.expected.Error == nil) {
				t.Errorf("Error = %v, want %v", tt.result.Error, tt.expected.Error)
			}
		})
	}
}
func TestVerifyAll(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "verify_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test files
	rpmFile1 := filepath.Join(tmpDir, "test1.rpm")
	rpmFile2 := filepath.Join(tmpDir, "test2.rpm")
	invalidPubkeyFile := filepath.Join(tmpDir, "invalid_pubkey.gpg")

	if err := os.WriteFile(rpmFile1, []byte("fake rpm content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rpmFile2, []byte("fake rpm content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(invalidPubkeyFile, []byte("invalid key content"), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		paths      []string
		pubkeyPath string
		workers    int
		wantLen    int
	}{
		{
			name:       "empty paths",
			paths:      []string{},
			pubkeyPath: invalidPubkeyFile,
			workers:    1,
			wantLen:    0,
		},
		{
			name:       "single file",
			paths:      []string{rpmFile1},
			pubkeyPath: invalidPubkeyFile,
			workers:    1,
			wantLen:    1,
		},
		{
			name:       "multiple files",
			paths:      []string{rpmFile1, rpmFile2},
			pubkeyPath: invalidPubkeyFile,
			workers:    2,
			wantLen:    2,
		},
		{
			name:       "multiple workers",
			paths:      []string{rpmFile1, rpmFile2},
			pubkeyPath: invalidPubkeyFile,
			workers:    5,
			wantLen:    2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := VerifyAll(tt.paths, []string{tt.pubkeyPath}, tt.workers)

			if len(results) != tt.wantLen {
				t.Errorf("VerifyAll() returned %d results, want %d", len(results), tt.wantLen)
			}

			// Check that all results have the correct paths and expected failures
			for i, result := range results {
				if i < len(tt.paths) && result.Path != tt.paths[i] {
					t.Errorf("Result[%d].Path = %v, want %v", i, result.Path, tt.paths[i])
				}

				// All our test files will fail verification (fake content)
				if result.OK {
					t.Errorf("Result[%d].OK = true, expected false for fake RPM", i)
				}

				if result.Error == nil {
					t.Errorf("Result[%d].Error = nil, expected error for fake RPM", i)
				}

				if result.Duration < 0 {
					t.Errorf("Result[%d].Duration = %v, expected non-negative duration", i, result.Duration)
				}
			}
		})
	}
}

func TestVerifyAllWithNonExistentFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "verify_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	invalidPubkeyFile := filepath.Join(tmpDir, "invalid_pubkey.gpg")
	if err := os.WriteFile(invalidPubkeyFile, []byte("invalid key content"), 0644); err != nil {
		t.Fatal(err)
	}

	nonExistentFile := filepath.Join(tmpDir, "nonexistent.rpm")
	results := VerifyAll([]string{nonExistentFile}, []string{invalidPubkeyFile}, 1)

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result.OK {
		t.Error("Expected OK to be false for non-existent file")
	}
	if result.Error == nil {
		t.Error("Expected error for non-existent file")
	}
	// The error could be about opening rpm or loading keyring, both are acceptable for a non-existent file
	if !strings.Contains(result.Error.Error(), "opening rpm") && !strings.Contains(result.Error.Error(), "loading keyring") {
		t.Errorf("Expected error about opening rpm or loading keyring, got: %v", result.Error)
	}
}

func TestVerifyAllWithInvalidPubkey(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "verify_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	rpmFile := filepath.Join(tmpDir, "test.rpm")
	invalidPubkeyFile := filepath.Join(tmpDir, "invalid_pubkey.gpg")

	if err := os.WriteFile(rpmFile, []byte("fake rpm content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(invalidPubkeyFile, []byte("invalid key content"), 0644); err != nil {
		t.Fatal(err)
	}

	results := VerifyAll([]string{rpmFile}, []string{invalidPubkeyFile}, 1)

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result.OK {
		t.Error("Expected OK to be false for invalid pubkey")
	}
	if result.Error == nil {
		t.Error("Expected error for invalid pubkey")
	}
	if !strings.Contains(result.Error.Error(), "loading keyring") {
		t.Errorf("Expected error about loading keyring, got: %v", result.Error)
	}
}

func TestVerifyWithGoRpm(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "verify_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name          string
		setupRPM      func() string
		setupPubkey   func() string
		expectedError string
	}{
		{
			name: "non-existent rpm file",
			setupRPM: func() string {
				return filepath.Join(tmpDir, "nonexistent.rpm")
			},
			setupPubkey: func() string {
				pubkeyFile := filepath.Join(tmpDir, "invalid_pubkey.gpg")
				_ = os.WriteFile(pubkeyFile, []byte("invalid key content"), 0644)
				return pubkeyFile
			},
			expectedError: "loading keyring", // Will fail at keyring loading first
		},
		{
			name: "non-existent pubkey file",
			setupRPM: func() string {
				rpmFile := filepath.Join(tmpDir, "test.rpm")
				_ = os.WriteFile(rpmFile, []byte("fake rpm"), 0644)
				return rpmFile
			},
			setupPubkey: func() string {
				return filepath.Join(tmpDir, "nonexistent.gpg")
			},
			expectedError: "opening public key",
		},
		{
			name: "invalid pubkey content",
			setupRPM: func() string {
				rpmFile := filepath.Join(tmpDir, "test.rpm")
				_ = os.WriteFile(rpmFile, []byte("fake rpm"), 0644)
				return rpmFile
			},
			setupPubkey: func() string {
				pubkeyFile := filepath.Join(tmpDir, "invalid.gpg")
				_ = os.WriteFile(pubkeyFile, []byte("invalid content"), 0644)
				return pubkeyFile
			},
			expectedError: "loading keyring",
		},
		{
			name: "invalid rpm content",
			setupRPM: func() string {
				rpmFile := filepath.Join(tmpDir, "invalid.rpm")
				_ = os.WriteFile(rpmFile, []byte("not an rpm file"), 0644)
				return rpmFile
			},
			setupPubkey: func() string {
				pubkeyFile := filepath.Join(tmpDir, "invalid_pubkey.gpg")
				_ = os.WriteFile(pubkeyFile, []byte("invalid key content"), 0644)
				return pubkeyFile
			},
			expectedError: "loading keyring", // Will fail at keyring loading first
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rpmPath := tt.setupRPM()
			pubkeyPath := tt.setupPubkey()

			err := verifyWithGoRpm(rpmPath, pubkeyPath)

			if err == nil {
				t.Error("Expected error, got nil")
				return
			}

			if !strings.Contains(err.Error(), tt.expectedError) {
				t.Errorf("Expected error containing %q, got: %v", tt.expectedError, err)
			}
		})
	}
}

func TestVerifyAllConcurrency(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "verify_concurrent_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create multiple test files
	numFiles := 10
	paths := make([]string, numFiles)
	for i := 0; i < numFiles; i++ {
		rpmFile := filepath.Join(tmpDir, fmt.Sprintf("test%d.rpm", i))
		if err := os.WriteFile(rpmFile, []byte(fmt.Sprintf("fake rpm content %d", i)), 0644); err != nil {
			t.Fatal(err)
		}
		paths[i] = rpmFile
	}

	invalidPubkeyFile := filepath.Join(tmpDir, "invalid_pubkey.gpg")
	if err := os.WriteFile(invalidPubkeyFile, []byte("invalid key content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Test with different worker counts
	workerCounts := []int{1, 3, 5, 10}

	for _, workers := range workerCounts {
		t.Run(fmt.Sprintf("workers_%d", workers), func(t *testing.T) {
			start := time.Now()
			results := VerifyAll(paths, []string{invalidPubkeyFile}, workers)
			duration := time.Since(start)

			if len(results) != numFiles {
				t.Errorf("Expected %d results, got %d", numFiles, len(results))
			}

			// Verify all files were processed
			processedPaths := make(map[string]bool)
			for _, result := range results {
				processedPaths[result.Path] = true

				// All should fail (fake content)
				if result.OK {
					t.Errorf("Expected file %s to fail verification", result.Path)
				}
				if result.Error == nil {
					t.Errorf("Expected error for file %s", result.Path)
				}
			}

			for _, path := range paths {
				if !processedPaths[path] {
					t.Errorf("File %s was not processed", path)
				}
			}

			t.Logf("Processed %d files with %d workers in %v", numFiles, workers, duration)
		})
	}
}

func TestVerifyAllResultsOrder(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "verify_order_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test files with specific names
	paths := []string{
		filepath.Join(tmpDir, "a.rpm"),
		filepath.Join(tmpDir, "b.rpm"),
		filepath.Join(tmpDir, "c.rpm"),
	}

	for _, path := range paths {
		if err := os.WriteFile(path, []byte("fake rpm"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	invalidPubkeyFile := filepath.Join(tmpDir, "invalid_pubkey.gpg")
	if err := os.WriteFile(invalidPubkeyFile, []byte("invalid key content"), 0644); err != nil {
		t.Fatal(err)
	}

	results := VerifyAll(paths, []string{invalidPubkeyFile}, 2)

	if len(results) != len(paths) {
		t.Fatalf("Expected %d results, got %d", len(paths), len(results))
	}

	// Results should maintain the same order as input paths
	for i, result := range results {
		if result.Path != paths[i] {
			t.Errorf("Result[%d].Path = %s, want %s", i, result.Path, paths[i])
		}
	}
}

// TestVerifyResultStructure tests the Result struct functionality
func TestVerifyResultStructure(t *testing.T) {
	tests := []struct {
		name        string
		result      Result
		expectValid bool
		description string
	}{
		{
			name: "Valid successful result",
			result: Result{
				Path:     "/path/to/package.rpm",
				OK:       true,
				Duration: time.Second,
				Error:    nil,
			},
			expectValid: true,
			description: "Standard successful verification",
		},
		{
			name: "Valid failed result",
			result: Result{
				Path:     "/path/to/package.rpm",
				OK:       false,
				Duration: time.Second,
				Error:    fmt.Errorf("signature verification failed"),
			},
			expectValid: true,
			description: "Standard failed verification",
		},
		{
			name: "Empty path",
			result: Result{
				Path:     "",
				OK:       true,
				Duration: time.Second,
				Error:    nil,
			},
			expectValid: false,
			description: "Result with empty path",
		},
		{
			name: "Negative duration",
			result: Result{
				Path:     "/path/to/package.rpm",
				OK:       true,
				Duration: -time.Second,
				Error:    nil,
			},
			expectValid: false,
			description: "Result with negative duration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test that we can access all fields
			if tt.result.Path == "" && tt.expectValid {
				t.Errorf("Expected valid result but path is empty for %s", tt.description)
			}

			if tt.result.Duration < 0 && tt.expectValid {
				t.Errorf("Expected valid result but duration is negative for %s", tt.description)
			}

			// Test error handling
			if tt.result.Error != nil && tt.result.OK {
				t.Errorf("Result marked as OK but has error: %v", tt.result.Error)
			}
		})
	}
}

// TestVerifyAllWithMixedFiles tests verification with a mix of file types
func TestVerifyAllWithMixedFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "verify_mixed_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create various test files
	rpmFile := filepath.Join(tmpDir, "test.rpm")
	txtFile := filepath.Join(tmpDir, "readme.txt")
	noExtFile := filepath.Join(tmpDir, "noextension")
	subDir := filepath.Join(tmpDir, "subdir")
	subRpm := filepath.Join(subDir, "nested.rpm")

	// Create the files
	if err := os.WriteFile(rpmFile, []byte("fake rpm content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(txtFile, []byte("some text content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(noExtFile, []byte("no extension content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(subRpm, []byte("nested rpm content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create invalid pubkey
	invalidPubkeyFile := filepath.Join(tmpDir, "invalid_pubkey.gpg")
	if err := os.WriteFile(invalidPubkeyFile, []byte("invalid key content"), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name          string
		paths         []string
		expectResults int
		description   string
	}{
		{
			name:          "Single RPM file",
			paths:         []string{rpmFile},
			expectResults: 1,
			description:   "Verify single RPM file",
		},
		{
			name:          "Multiple files including non-RPM",
			paths:         []string{rpmFile, txtFile, noExtFile},
			expectResults: 3, // VerifyAll processes all given paths
			description:   "Mix of RPM and non-RPM files",
		},
		{
			name:          "Non-existent file",
			paths:         []string{filepath.Join(tmpDir, "nonexistent.rpm")},
			expectResults: 1, // Should still return result with error
			description:   "Non-existent RPM file",
		},
		{
			name:          "Empty paths list",
			paths:         []string{},
			expectResults: 0,
			description:   "Empty file list",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := VerifyAll(tt.paths, []string{invalidPubkeyFile}, 2)

			if len(results) != tt.expectResults {
				t.Errorf("Expected %d results for %s, got %d",
					tt.expectResults, tt.description, len(results))
			}

			// Verify all results have required fields
			for i, result := range results {
				if result.Path == "" {
					t.Errorf("Result %d has empty path for %s", i, tt.description)
				}
				if result.Duration < 0 {
					t.Errorf("Result %d has negative duration for %s", i, tt.description)
				}
			}
		})
	}
}

// TestVerifyAllConcurrencyDetailed tests concurrent verification with detailed checks
func TestVerifyAllConcurrencyDetailed(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "verify_concurrency_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create multiple RPM files
	numFiles := 10
	var rpmFiles []string
	for i := 0; i < numFiles; i++ {
		rpmFile := filepath.Join(tmpDir, fmt.Sprintf("test%d.rpm", i))
		content := fmt.Sprintf("fake rpm content %d", i)
		if err := os.WriteFile(rpmFile, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		rpmFiles = append(rpmFiles, rpmFile)
	}

	// Create invalid pubkey
	invalidPubkeyFile := filepath.Join(tmpDir, "invalid_pubkey.gpg")
	if err := os.WriteFile(invalidPubkeyFile, []byte("invalid key content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Run verification multiple times to test consistency
	for attempt := 0; attempt < 3; attempt++ {
		t.Run(fmt.Sprintf("Attempt_%d", attempt+1), func(t *testing.T) {
			results := VerifyAll(rpmFiles, []string{invalidPubkeyFile}, 4)

			if len(results) != numFiles {
				t.Errorf("Expected %d results, got %d", numFiles, len(results))
			}

			// Check that all files are represented in results
			resultPaths := make(map[string]bool)
			for _, result := range results {
				resultPaths[result.Path] = true

				// Verify result structure
				if result.Path == "" {
					t.Error("Found result with empty path")
				}
				if result.Duration < 0 {
					t.Error("Found result with negative duration")
				}
				if result.OK {
					t.Error("Expected all verifications to fail with invalid pubkey")
				}
				if result.Error == nil {
					t.Error("Expected error due to invalid pubkey but got none")
				}
			}

			// Ensure all input files are in results
			for _, rpmFile := range rpmFiles {
				if !resultPaths[rpmFile] {
					t.Errorf("File %s not found in results", rpmFile)
				}
			}
		})
	}
}

// TestVerifyWithGoRpmEdgeCases tests edge cases in verifyWithGoRpm
func TestVerifyWithGoRpmEdgeCases(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "verify_gorpm_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name        string
		setupTest   func() (string, string)
		expectError bool
		description string
	}{
		{
			name: "Empty RPM file",
			setupTest: func() (string, string) {
				rpmFile := filepath.Join(tmpDir, "empty.rpm")
				pubkeyFile := filepath.Join(tmpDir, "pubkey.gpg")

				_ = os.WriteFile(rpmFile, []byte(""), 0644)
				_ = os.WriteFile(pubkeyFile, []byte("fake pubkey"), 0644)

				return rpmFile, pubkeyFile
			},
			expectError: true,
			description: "Empty RPM file",
		},
		{
			name: "Binary data as RPM",
			setupTest: func() (string, string) {
				rpmFile := filepath.Join(tmpDir, "binary.rpm")
				pubkeyFile := filepath.Join(tmpDir, "pubkey.gpg")

				// Write some binary data
				binaryData := make([]byte, 1024)
				for i := range binaryData {
					binaryData[i] = byte(i % 256)
				}
				_ = os.WriteFile(rpmFile, binaryData, 0644)
				_ = os.WriteFile(pubkeyFile, []byte("fake pubkey"), 0644)

				return rpmFile, pubkeyFile
			},
			expectError: true,
			description: "Binary data as RPM",
		},
		{
			name: "File with special characters in name",
			setupTest: func() (string, string) {
				rpmFile := filepath.Join(tmpDir, "special-chars@#$.rpm")
				pubkeyFile := filepath.Join(tmpDir, "pubkey.gpg")

				_ = os.WriteFile(rpmFile, []byte("fake rpm"), 0644)
				_ = os.WriteFile(pubkeyFile, []byte("fake pubkey"), 0644)

				return rpmFile, pubkeyFile
			},
			expectError: true,
			description: "File with special characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rpmFile, pubkeyFile := tt.setupTest()

			err := verifyWithGoRpm(rpmFile, pubkeyFile)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for %s but got none", tt.description)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for %s: %v", tt.description, err)
				}
			}
		})
	}
}

// TestVerifyAllWorkerCount tests verification with different worker counts
func TestVerifyAllWorkerCount(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "verify_workers_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test files
	numFiles := 5
	var rpmFiles []string
	for i := 0; i < numFiles; i++ {
		rpmFile := filepath.Join(tmpDir, fmt.Sprintf("test%d.rpm", i))
		if err := os.WriteFile(rpmFile, []byte("fake rpm content"), 0644); err != nil {
			t.Fatal(err)
		}
		rpmFiles = append(rpmFiles, rpmFile)
	}

	// Create invalid pubkey
	invalidPubkeyFile := filepath.Join(tmpDir, "invalid_pubkey.gpg")
	if err := os.WriteFile(invalidPubkeyFile, []byte("invalid key content"), 0644); err != nil {
		t.Fatal(err)
	}

	workerCounts := []int{1, 2, 4, 8}
	for _, workers := range workerCounts {
		t.Run(fmt.Sprintf("Workers_%d", workers), func(t *testing.T) {
			results := VerifyAll(rpmFiles, []string{invalidPubkeyFile}, workers)

			if len(results) != numFiles {
				t.Errorf("Expected %d results with %d workers, got %d",
					numFiles, workers, len(results))
			}

			// Check that all results have the expected structure
			for i, result := range results {
				if result.Path != rpmFiles[i] {
					t.Errorf("Result %d path mismatch: expected %s, got %s",
						i, rpmFiles[i], result.Path)
				}
				if result.OK {
					t.Errorf("Expected verification to fail for result %d", i)
				}
				if result.Error == nil {
					t.Errorf("Expected error for result %d", i)
				}
			}
		})
	}
}
// TestCreateTempGPGKeyFiles_ReusesCachedKeyOffline reproduces the offline
// rebuild failure where the RPM signature verification path fetched GPG keys
// from public URLs every time. Once the first online build has populated the
// persistent GPG cache, a subsequent offline build must materialize the temp
// key files purely from disk with zero HTTP requests.
func TestCreateTempGPGKeyFiles_ReusesCachedKeyOffline(t *testing.T) {
	origCacheDir := config.Global().CacheDir
	updatedCfg := config.Global()
	updatedCfg.CacheDir = t.TempDir()
	config.SetGlobal(updatedCfg)
	t.Cleanup(func() {
		cur := config.Global()
		cur.CacheDir = origCacheDir
		config.SetGlobal(cur)
	})

	keyURL := "https://offline.invalid.example/RPM-GPG-KEY"
	want := []byte("-----BEGIN PGP PUBLIC KEY BLOCK-----\ncached-key-data\n-----END PGP PUBLIC KEY BLOCK-----\n")

	cachePath, err := config.GPGKeyCacheFilePath(keyURL)
	if err != nil {
		t.Fatalf("GPGKeyCacheFilePath failed: %v", err)
	}
	if err := os.WriteFile(cachePath, want, 0644); err != nil {
		t.Fatalf("failed to seed cached key: %v", err)
	}

	paths, cleanup, err := createTempGPGKeyFiles([]string{keyURL})
	if err != nil {
		t.Fatalf("createTempGPGKeyFiles should hit the offline cache, got error: %v", err)
	}
	t.Cleanup(cleanup)

	if len(paths) != 1 {
		t.Fatalf("expected 1 temp key file, got %d", len(paths))
	}
	got, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatalf("failed to read materialized temp key: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("temp key content mismatch: got %q want %q", string(got), string(want))
	}
}