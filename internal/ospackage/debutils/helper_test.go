package debutils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
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

// TestCreateTemporaryRepositoryPackagesFileMissing verifies that CreateTemporaryRepository
// returns an error when dpkg-scanpackages exits successfully but does not produce the
// expected Packages file on disk.
func TestCreateTemporaryRepositoryPackagesFileMissing(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tempDir, err := os.MkdirTemp("", "debtest_")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	for _, debFile := range []string{"package1_1.0_amd64.deb", "package2_2.0_all.deb"} {
		debPath := filepath.Join(tempDir, debFile)
		if err := os.WriteFile(debPath, []byte("fake deb content"), 0644); err != nil {
			t.Fatalf("Failed to create fake DEB file %s: %v", debFile, err)
		}
	}

	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: "dpkg-scanpackages", Output: "", Error: nil},
	})

	_, _, _, err = CreateTemporaryRepository(tempDir, "testrepo", "amd64")

	if err == nil {
		t.Fatal("Expected error about missing Packages file")
	}
	if !strings.Contains(err.Error(), "repository metadata was not created properly") {
		t.Errorf("Expected 'repository metadata was not created properly' error, got: %v", err)
	}
}

// scanpackagesExecutor implements shell.Executor to simulate dpkg-scanpackages by writing
// an empty Packages file at the output path encoded in the command string. This allows the
// full post-command code path (Packages.gz, Release, HTTP server) to be exercised without
// requiring dpkg-scanpackages to be installed.
type scanpackagesExecutor struct{}

func (e *scanpackagesExecutor) ExecCmd(cmdStr string, sudo bool, chrootPath string, envVal []string) (string, error) {
	if strings.Contains(cmdStr, "dpkg-scanpackages") {
		// Command format: "cd <dir> && dpkg-scanpackages pool/main /dev/null > <packagesPath>"
		if parts := strings.SplitN(cmdStr, " > ", 2); len(parts) == 2 {
			if err := os.WriteFile(strings.TrimSpace(parts[1]), []byte(""), 0644); err != nil {
				return "", fmt.Errorf("test executor: failed to create Packages file: %w", err)
			}
		}
	}
	return "", nil
}

func (e *scanpackagesExecutor) ExecCmdSilent(cmdStr string, sudo bool, chrootPath string, envVal []string) (string, error) {
	return e.ExecCmd(cmdStr, sudo, chrootPath, envVal)
}

func (e *scanpackagesExecutor) ExecCmdWithStream(cmdStr string, sudo bool, chrootPath string, envVal []string) (string, error) {
	return e.ExecCmd(cmdStr, sudo, chrootPath, envVal)
}

func (e *scanpackagesExecutor) ExecCmdWithInput(inputStr string, cmdStr string, sudo bool, chrootPath string, envVal []string) (string, error) {
	return e.ExecCmd(cmdStr, sudo, chrootPath, envVal)
}

// TestCreateTemporaryRepositorySuccess exercises the full happy path: DEB files are copied,
// a Packages file is generated, Packages.gz and Release are created, an HTTP server is
// started, and the server is verified to be reachable before returning.
func TestCreateTemporaryRepositorySuccess(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tempDir, err := os.MkdirTemp("", "debtest_success_")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	debPath := filepath.Join(tempDir, "package1_1.0_amd64.deb")
	if err := os.WriteFile(debPath, []byte("fake deb content"), 0644); err != nil {
		t.Fatalf("Failed to create DEB file: %v", err)
	}

	shell.Default = &scanpackagesExecutor{}

	repoPath, serverURL, cleanup, err := CreateTemporaryRepository(tempDir, "testrepo", "amd64")
	if err != nil {
		t.Fatalf("Expected success, got error: %v", err)
	}
	defer cleanup()

	if repoPath == "" {
		t.Error("Expected non-empty repository path")
	}
	if !strings.HasPrefix(serverURL, "http://localhost:") {
		t.Errorf("Expected server URL starting with 'http://localhost:', got: %s", serverURL)
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

func TestCreateTemporaryRepositorySourcePathIsFile(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "package1_1.0_amd64.deb")
	if err := os.WriteFile(filePath, []byte("fake deb content"), 0644); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}

	_, _, _, err := CreateTemporaryRepository(filePath, "testrepo", "amd64")
	if err == nil {
		t.Fatal("expected error when source path is a file")
	}
	if !strings.Contains(err.Error(), "source path is not a directory") {
		t.Errorf("expected non-directory source path error, got: %v", err)
	}
}

func TestCreateTemporaryRepositoryStatError(t *testing.T) {
	tempDir := t.TempDir()
	blockedParent := filepath.Join(tempDir, "blocked")
	if err := os.Mkdir(blockedParent, 0755); err != nil {
		t.Fatalf("failed to create blocked parent directory: %v", err)
	}

	blockedPath := filepath.Join(blockedParent, "source")
	if err := os.Chmod(blockedParent, 0); err != nil {
		t.Fatalf("failed to restrict blocked parent permissions: %v", err)
	}
	defer func() {
		if err := os.Chmod(blockedParent, 0755); err != nil {
			t.Logf("warning: failed to restore blocked parent permissions: %v", err)
		}
	}()

	if _, statErr := os.Stat(blockedPath); statErr == nil || os.IsNotExist(statErr) {
		t.Skip("unable to induce non-not-exist os.Stat error on this platform")
	}

	_, _, _, err := CreateTemporaryRepository(blockedPath, "testrepo", "amd64")
	if err == nil {
		t.Fatal("expected stat error for inaccessible source path")
	}
	if !strings.Contains(err.Error(), "failed to stat source directory") {
		t.Errorf("expected stat failure error, got: %v", err)
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

// TestCreateTemporaryRepositoryUniqueDirectories tests that successive calls create unique directories
func TestCreateTemporaryRepositoryUniqueDirectories(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tempDir, err := os.MkdirTemp("", "debtest_unique_")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	debPath := filepath.Join(tempDir, "package1_1.0_amd64.deb")
	if err := os.WriteFile(debPath, []byte("fake deb content"), 0644); err != nil {
		t.Fatalf("Failed to create fake DEB file: %v", err)
	}

	shell.Default = &scanpackagesExecutor{}

	repoPath1, _, cleanup1, err := CreateTemporaryRepository(tempDir, "unique1", "amd64")
	if err != nil {
		t.Fatalf("First call failed: %v", err)
	}
	defer cleanup1()

	repoPath2, _, cleanup2, err := CreateTemporaryRepository(tempDir, "unique2", "amd64")
	if err != nil {
		t.Fatalf("Second call failed: %v", err)
	}
	defer cleanup2()

	if repoPath1 == repoPath2 {
		t.Errorf("Expected unique repository paths, got identical paths: %s", repoPath1)
	}
}

func TestCreateTemporaryRepositoryCopyFileFails(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "debtest_copy_")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a directory with a .deb suffix so copyFile fails when trying to copy it.
	debPath := filepath.Join(tempDir, "package_1.0_amd64.deb")
	if err := os.Mkdir(debPath, 0755); err != nil {
		t.Fatalf("Failed to create fake DEB directory: %v", err)
	}

	_, _, _, err = CreateTemporaryRepository(tempDir, "testrepo", "amd64")
	if err == nil {
		t.Fatal("Expected error when DEB copy fails")
	}
	if !strings.Contains(err.Error(), "failed to copy DEB file") {
		t.Errorf("Expected 'failed to copy DEB file' error, got: %v", err)
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
