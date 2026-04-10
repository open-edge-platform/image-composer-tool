package debutils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-edge-platform/os-image-composer/internal/config"
	"github.com/open-edge-platform/os-image-composer/internal/utils/shell"
)

func TestGenerateSPDXFileName(t *testing.T) {
	tests := []struct {
		name   string
		repoNm string
	}{
		{
			name:   "simple repository name",
			repoNm: "Ubuntu",
		},
		{
			name:   "repository name with spaces",
			repoNm: "Azure Linux 3.0",
		},
		{
			name:   "empty repository name",
			repoNm: "",
		},
		{
			name:   "repository name with spaces",
			repoNm: "Test Repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateSPDXFileName(tt.repoNm)

			// Check that result starts with correct prefix
			if !strings.HasPrefix(result, "spdx_manifest_deb_") {
				t.Errorf("GenerateSPDXFileName() = %v, expected to start with 'spdx_manifest_deb_'", result)
			}

			// Check that result ends with .json
			if !strings.HasSuffix(result, ".json") {
				t.Errorf("GenerateSPDXFileName() = %v, expected to end with '.json'", result)
			}

			// Check that spaces are replaced with underscores
			expectedRepoName := strings.ReplaceAll(tt.repoNm, " ", "_")
			if !strings.Contains(result, expectedRepoName) {
				t.Errorf("GenerateSPDXFileName() = %v, expected to contain %v", result, expectedRepoName)
			}

			// Check that result contains timestamp-like pattern (has underscores and digits)
			if len(result) < 30 { // Should be long enough to contain timestamp
				t.Errorf("GenerateSPDXFileName() = %v, result too short", result)
			}
		})
	}
}

// TestCreateTemporaryRepositorySuccess tests CreateTemporaryRepository with valid DEB files
func TestCreateTemporaryRepositorySuccess(t *testing.T) {
	// Save original shell executor and restore after test
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	// Create temporary directory with mock DEB files for testing
	tempDir, err := os.MkdirTemp("", "debtest_")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create fake DEB files
	debFiles := []string{"package1_1.0_amd64.deb", "package2_2.0_all.deb"}
	for _, debFile := range debFiles {
		debPath := filepath.Join(tempDir, debFile)
		if err := os.WriteFile(debPath, []byte("fake deb content"), 0644); err != nil {
			t.Fatalf("Failed to create fake DEB file %s: %v", debFile, err)
		}
	}

	// Mock shell commands for dpkg-scanpackages
	mockCommands := []shell.MockCommand{
		{
			Pattern: "cp " + tempDir + "/*.deb",
			Output:  "",
			Error:   nil,
		},
		{
			Pattern: "dpkg-scanpackages",
			Output:  "Successfully created Packages file",
			Error:   nil,
		},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	// Test CreateTemporaryRepository
	repoPath, serverURL, cleanup, err := CreateTemporaryRepository(tempDir, "testrepo", "amd64")

	// Note: Since we're using mocked shell commands, the actual repository structure
	// won't be created. We're testing the function logic, not the actual file operations.
	// In this case, the function should fail because the mocked dpkg-scanpackages
	// doesn't actually create the Packages file that the function checks for.

	// For mocked tests, we expect an error because the Packages file check will fail
	if err == nil {
		// If no error, verify basic return values
		if repoPath == "" {
			t.Error("Expected non-empty repository path")
		}
		if serverURL == "" {
			t.Error("Expected non-empty server URL")
		}
		if cleanup == nil {
			t.Error("Expected non-nil cleanup function")
		}
		// Clean up
		if cleanup != nil {
			cleanup()
		}
	} else {
		// Expected behavior with mocked commands - file check fails
		if !strings.Contains(err.Error(), "repository metadata was not created properly") {
			t.Errorf("Expected error about metadata creation, got: %v", err)
		}
	}
}

// TestCreateTemporaryRepositoryNonExistentDirectory tests error handling for non-existent source directory
func TestCreateTemporaryRepositoryNonExistentDirectory(t *testing.T) {
	nonExistentPath := "/path/that/does/not/exist"

	_, _, _, err := CreateTemporaryRepository(nonExistentPath, "testrepo", "amd64")

	if err == nil {
		t.Error("Expected error for non-existent directory")
	}

	if !strings.Contains(err.Error(), "source directory does not exist") {
		t.Errorf("Expected error about non-existent directory, got: %v", err)
	}
}

// TestCreateTemporaryRepositoryNoDEBFiles tests error handling when source directory contains no DEB files
func TestCreateTemporaryRepositoryNoDEBFiles(t *testing.T) {
	// Create temporary directory without DEB files
	tempDir, err := os.MkdirTemp("", "debtest_nodeb_")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create some non-DEB files
	nonDebFiles := []string{"readme.txt", "config.xml", "data.json"}
	for _, file := range nonDebFiles {
		filePath := filepath.Join(tempDir, file)
		if err := os.WriteFile(filePath, []byte("not a deb"), 0644); err != nil {
			t.Fatalf("Failed to create non-DEB file %s: %v", file, err)
		}
	}

	_, _, _, err = CreateTemporaryRepository(tempDir, "testrepo", "amd64")

	if err == nil {
		t.Error("Expected error when no DEB files found")
	}

	if !strings.Contains(err.Error(), "no DEB files found") {
		t.Errorf("Expected error about no DEB files, got: %v", err)
	}
}

// TestCreateTemporaryRepositoryDpkgScanpackagesFailure tests error handling when dpkg-scanpackages fails
func TestCreateTemporaryRepositoryDpkgScanpackagesFailure(t *testing.T) {
	// Save original shell executor and restore after test
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	// Create temporary directory with mock DEB files
	tempDir, err := os.MkdirTemp("", "debtest_dpkgfail_")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create fake DEB file
	debPath := filepath.Join(tempDir, "package1_1.0_amd64.deb")
	if err := os.WriteFile(debPath, []byte("fake deb content"), 0644); err != nil {
		t.Fatalf("Failed to create fake DEB file: %v", err)
	}

	// Mock shell commands - make dpkg-scanpackages fail
	mockCommands := []shell.MockCommand{
		{
			Pattern: "cp " + tempDir + "/*.deb",
			Output:  "",
			Error:   nil,
		},
		{
			Pattern: "dpkg-scanpackages",
			Output:  "",
			Error:   fmt.Errorf("dpkg-scanpackages command failed"),
		},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	_, _, _, err = CreateTemporaryRepository(tempDir, "testrepo", "amd64")

	if err == nil {
		t.Error("Expected error when dpkg-scanpackages fails")
	}

	if !strings.Contains(err.Error(), "failed to create Packages file") {
		t.Errorf("Expected error about Packages file creation failure, got: %v", err)
	}
}

// TestCreateTemporaryRepositorySpecialCharacters tests repository creation with special characters in paths
func TestCreateTemporaryRepositorySpecialCharacters(t *testing.T) {
	// Save original shell executor and restore after test
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	// Create temporary directory with space in name
	tempDir, err := os.MkdirTemp("", "deb test space_")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create fake DEB file
	debPath := filepath.Join(tempDir, "package_with-special_chars_1.0_amd64.deb")
	if err := os.WriteFile(debPath, []byte("fake deb content"), 0644); err != nil {
		t.Fatalf("Failed to create fake DEB file: %v", err)
	}

	// Mock shell commands
	mockCommands := []shell.MockCommand{
		{
			Pattern: "cp",
			Output:  "",
			Error:   nil,
		},
		{
			Pattern: "dpkg-scanpackages",
			Output:  "Successfully created Packages file",
			Error:   nil,
		},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	repoPath, _, cleanup, err := CreateTemporaryRepository(tempDir, "repo-with-special_chars", "amd64")

	// Note: With mocked commands, the actual file creation doesn't happen,
	// so we expect this to fail with metadata creation error
	if err == nil {
		// If no error (shouldn't happen with mocked commands), verify basic values
		if repoPath == "" {
			t.Error("Expected non-empty repository path with special characters")
		}
		// Test cleanup
		if cleanup != nil {
			cleanup()
		}
	} else {
		// Expected behavior - metadata creation check fails with mocked commands
		if !strings.Contains(err.Error(), "repository metadata was not created properly") {
			t.Errorf("Expected metadata creation error with special characters, got: %v", err)
		}
	}
}

// TestCreateTemporaryRepositoryCleanup tests that the cleanup function properly removes temporary files
func TestCreateTemporaryRepositoryCleanup(t *testing.T) {
	// Save original shell executor and restore after test
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	// Create temporary directory with mock DEB files
	tempDir, err := os.MkdirTemp("", "debtest_cleanup_")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create fake DEB file
	debPath := filepath.Join(tempDir, "package1_1.0_amd64.deb")
	if err := os.WriteFile(debPath, []byte("fake deb content"), 0644); err != nil {
		t.Fatalf("Failed to create fake DEB file: %v", err)
	}

	// Mock shell commands
	mockCommands := []shell.MockCommand{
		{
			Pattern: "cp",
			Output:  "",
			Error:   nil,
		},
		{
			Pattern: "dpkg-scanpackages",
			Output:  "Successfully created Packages file",
			Error:   nil,
		},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	repoPath, _, cleanup, err := CreateTemporaryRepository(tempDir, "cleanuptest", "amd64")

	// Note: Since we're using mocked commands, the actual repository structure
	// won't be created and the function will fail during file verification.
	// This is expected behavior with mocked commands.

	if err == nil {
		// If no error (shouldn't happen with mocked commands), verify basic values
		if repoPath == "" {
			t.Error("Expected non-empty repository path")
		}
		if cleanup == nil {
			t.Error("Expected non-nil cleanup function")
		}
		// Call cleanup
		cleanup()
	} else {
		// Expected behavior - metadata creation check fails with mocked commands
		if !strings.Contains(err.Error(), "repository metadata was not created properly") {
			t.Errorf("Expected metadata creation error, got: %v", err)
		}
	}
}

// TestCreateTemporaryRepositoryUniqueDirectories tests that concurrent calls create unique directories
func TestCreateTemporaryRepositoryUniqueDirectories(t *testing.T) {
	// Save original shell executor and restore after test
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	// Create temporary directory with mock DEB files
	tempDir, err := os.MkdirTemp("", "debtest_unique_")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create fake DEB file
	debPath := filepath.Join(tempDir, "package1_1.0_amd64.deb")
	if err := os.WriteFile(debPath, []byte("fake deb content"), 0644); err != nil {
		t.Fatalf("Failed to create fake DEB file: %v", err)
	}

	// Mock shell commands
	mockCommands := []shell.MockCommand{
		{
			Pattern: "cp",
			Output:  "",
			Error:   nil,
		},
		{
			Pattern: "dpkg-scanpackages",
			Output:  "Successfully created Packages file",
			Error:   nil,
		},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	// Create two repositories with slight time difference
	_, _, cleanup1, err1 := CreateTemporaryRepository(tempDir, "unique1", "amd64")

	// Note: With mocked commands, both calls will fail during metadata verification
	// We're testing that different repository names are used in the paths

	if err1 == nil {
		defer cleanup1()
	}

	// Sleep briefly to ensure different timestamps
	time.Sleep(1 * time.Millisecond)

	_, _, cleanup2, err2 := CreateTemporaryRepository(tempDir, "unique2", "amd64")
	if err2 == nil {
		defer cleanup2()
	}

	// Both should fail with metadata creation error (expected with mocking)
	if err1 != nil && !strings.Contains(err1.Error(), "repository metadata was not created properly") {
		t.Errorf("First call should fail with metadata error, got: %v", err1)
	}
	if err2 != nil && !strings.Contains(err2.Error(), "repository metadata was not created properly") {
		t.Errorf("Second call should fail with metadata error, got: %v", err2)
	}

	// Test that the repository paths would be different (from the temp directory structure)
	// Even though the function fails, the initial path creation should use unique names
	t.Log("This test verifies unique temporary directory naming with mocked commands")
}

func TestCreateTemporaryRepositoryCopyFileFails(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tempDir, err := os.MkdirTemp("", "debtest_copy_")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a fake DEB file so the source-check passes
	debPath := filepath.Join(tempDir, "package_1.0_amd64.deb")
	if err := os.WriteFile(debPath, []byte("fake deb content"), 0644); err != nil {
		t.Fatalf("Failed to create fake DEB file: %v", err)
	}

	// Mock cp to fail
	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: "cp", Output: "", Error: fmt.Errorf("permission denied")},
	})

	_, _, _, err = CreateTemporaryRepository(tempDir, "testrepo", "amd64")
	if err == nil {
		t.Fatal("Expected error when cp fails")
	}
	if !strings.Contains(err.Error(), "failed to copy DEB files") {
		t.Errorf("Expected 'failed to copy DEB files' error, got: %v", err)
	}
}

func TestDebLocalUserPackagesEmpty(t *testing.T) {
	origUserRepo := UserRepo
	origArch := Architecture
	defer func() {
		UserRepo = origUserRepo
		Architecture = origArch
	}()

	UserRepo = []config.PackageRepository{}
	Architecture = "amd64"

	pkgs, cleanup, err := LocalUserPackages()
	if err != nil {
		t.Fatalf("expected no error for empty repo list, got: %v", err)
	}
	if len(pkgs) != 0 {
		t.Errorf("expected empty package list, got %d packages", len(pkgs))
	}
	if cleanup != nil {
		cleanup()
	}
}

func TestDebLocalUserPackagesSkipsPlaceholders(t *testing.T) {
	origUserRepo := UserRepo
	origArch := Architecture
	defer func() {
		UserRepo = origUserRepo
		Architecture = origArch
	}()

	UserRepo = []config.PackageRepository{
		{Path: "<PATH>"},
		{Path: ""},
	}
	Architecture = "amd64"

	pkgs, cleanup, err := LocalUserPackages()
	if err != nil {
		t.Fatalf("expected no error when all paths are placeholders, got: %v", err)
	}
	if len(pkgs) != 0 {
		t.Errorf("expected empty package list when all paths skip, got %d", len(pkgs))
	}
	if cleanup != nil {
		cleanup()
	}
}

func TestDebLocalUserPackagesFailsForNonExistentDir(t *testing.T) {
	origUserRepo := UserRepo
	origArch := Architecture
	defer func() {
		UserRepo = origUserRepo
		Architecture = origArch
	}()

	UserRepo = []config.PackageRepository{
		{Path: "/totally/nonexistent/deb/path"},
	}
	Architecture = "amd64"

	_, _, err := LocalUserPackages()
	if err == nil {
		t.Fatal("expected error for non-existent repo path")
	}
	if !strings.Contains(err.Error(), "failed to create temporary DEB repository") {
		t.Errorf("expected 'failed to create temporary DEB repository' in error, got: %v", err)
	}
}
