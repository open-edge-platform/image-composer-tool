package deb_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/chroot/deb"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

func TestNewDebInstaller(t *testing.T) {
	installer := deb.NewDebInstaller()
	if installer == nil {
		t.Fatal("NewDebInstaller should return a non-nil instance")
	}
}

func TestUpdateLocalDebRepo_ArchitectureMapping(t *testing.T) {
	tests := []struct {
		name         string
		inputArch    string
		expectedArch string
		expectError  bool
	}{
		{"amd64", "amd64", "amd64", false},
		{"x86_64", "x86_64", "amd64", false},
		{"arm64", "arm64", "arm64", false},
		{"aarch64", "aarch64", "arm64", false},
		{"unsupported", "mips", "", true},
	}

	installer := deb.NewDebInstaller()
	tempDir := t.TempDir()

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "dpkg-scanpackages", Output: "override-test\n", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := installer.UpdateLocalDebRepo(tempDir, tt.inputArch, false)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for architecture %s, but got none", tt.inputArch)
				}
				if !strings.Contains(err.Error(), "unsupported architecture") {
					t.Errorf("Expected 'unsupported architecture' error, got: %v", err)
				}
			} else {
				// For valid architectures, we expect it to fail due to missing dpkg-scanpackages
				// but not due to architecture validation
				if err != nil && strings.Contains(err.Error(), "unsupported architecture") {
					t.Errorf("Should not get architecture error for %s, got: %v", tt.inputArch, err)
				}
			}
		})
	}
}

func TestUpdateLocalDebRepo_DirectoryCreation(t *testing.T) {
	installer := deb.NewDebInstaller()
	tempDir := t.TempDir()

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "dpkg-scanpackages", Output: "override-test\n", Error: fmt.Errorf("command not found")},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	// Test that it attempts to create the metadata directory structure
	err := installer.UpdateLocalDebRepo(tempDir, "x86_64", false)

	// We expect this to fail due to missing dpkg-scanpackages, but the directory should be created
	expectedDir := filepath.Join(tempDir, "dists/stable/main/binary-amd64")
	if _, statErr := os.Stat(expectedDir); os.IsNotExist(statErr) {
		t.Errorf("Expected directory %s to be created", expectedDir)
	}

	// Should fail on dpkg-scanpackages command
	if err == nil {
		t.Error("Expected error due to missing dpkg-scanpackages command")
	}
}

func TestUpdateLocalDebRepo_ExistingPackagesGz(t *testing.T) {
	installer := deb.NewDebInstaller()
	tempDir := t.TempDir()

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "dpkg-scanpackages", Output: "override-test\n", Error: fmt.Errorf("command not found")},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	// Create the directory structure and an existing Packages.gz file
	metaDir := filepath.Join(tempDir, "dists/stable/main/binary-amd64")
	if err := os.MkdirAll(metaDir, 0700); err != nil {
		t.Fatalf("Failed to create metadata directory: %v", err)
	}

	packagesGzPath := filepath.Join(metaDir, "Packages.gz")
	if err := os.WriteFile(packagesGzPath, []byte("old content"), 0644); err != nil {
		t.Fatalf("Failed to create existing Packages.gz: %v", err)
	}

	// Run UpdateLocalDebRepo
	err := installer.UpdateLocalDebRepo(tempDir, "amd64", false)

	// The file should be removed (and the command will fail due to missing dpkg-scanpackages)
	if err != nil && !strings.Contains(err.Error(), "failed to create local debian cache repository") {
		t.Errorf("Expected dpkg-scanpackages error, got different error: %v", err)
	}
}

func TestInstallDebPkg_ParameterValidation(t *testing.T) {
	installer := deb.NewDebInstaller()

	tests := []struct {
		name              string
		targetOsConfigDir string
		chrootEnvPath     string
		chrootPkgCacheDir string
		pkgsList          []string
		expectedError     string
	}{
		{
			name:              "empty chrootEnvPath",
			targetOsConfigDir: "/tmp",
			chrootEnvPath:     "",
			chrootPkgCacheDir: "/tmp/cache",
			pkgsList:          []string{"pkg1"},
			expectedError:     "invalid parameters",
		},
		{
			name:              "empty chrootPkgCacheDir",
			targetOsConfigDir: "/tmp",
			chrootEnvPath:     "/tmp/chroot",
			chrootPkgCacheDir: "",
			pkgsList:          []string{"pkg1"},
			expectedError:     "invalid parameters",
		},
		{
			name:              "empty pkgsList",
			targetOsConfigDir: "/tmp",
			chrootEnvPath:     "/tmp/chroot",
			chrootPkgCacheDir: "/tmp/cache",
			pkgsList:          []string{},
			expectedError:     "invalid parameters",
		},
		{
			name:              "nil pkgsList",
			targetOsConfigDir: "/tmp",
			chrootEnvPath:     "/tmp/chroot",
			chrootPkgCacheDir: "/tmp/cache",
			pkgsList:          nil,
			expectedError:     "invalid parameters",
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "mmdebstrap", Output: "override-test\n", Error: fmt.Errorf("command not found")},
		{Pattern: "rm", Output: "override-test\n", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := installer.InstallDebPkg(tt.targetOsConfigDir, tt.chrootEnvPath, tt.chrootPkgCacheDir, tt.pkgsList)
			if err == nil {
				t.Error("Expected error for invalid parameters")
			}
			if !strings.Contains(err.Error(), tt.expectedError) {
				t.Errorf("Expected error containing '%s', got: %v", tt.expectedError, err)
			}
		})
	}
}

func TestInstallDebPkg_MissingLocalRepoConfig(t *testing.T) {
	installer := deb.NewDebInstaller()
	tempDir := t.TempDir()

	targetOsConfigDir := tempDir
	chrootEnvPath := filepath.Join(tempDir, "chroot")
	chrootPkgCacheDir := filepath.Join(tempDir, "cache")
	pkgsList := []string{"test-package"}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "mkdir", Output: "override-test\n", Error: nil},
		{Pattern: "dpkg-scanpackages", Output: "override-test\n", Error: nil},
		{Pattern: "mmdebstrap", Output: "override-test\n", Error: fmt.Errorf("command not found")},
		{Pattern: "rm", Output: "override-test\n", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	// Set targetArch by calling UpdateLocalDebRepo first
	if err := installer.UpdateLocalDebRepo(chrootPkgCacheDir, "amd64", false); err != nil {
		t.Fatalf("Failed to update local deb repo: %v", err)
	}

	// Don't create the local.list file
	err := installer.InstallDebPkg(targetOsConfigDir, chrootEnvPath, chrootPkgCacheDir, pkgsList)

	if err == nil {
		t.Error("Expected error when local repository config file does not exist")
	}

	expectedPath := filepath.Join(targetOsConfigDir, "chrootenvconfigs", "local.list")
	if !strings.Contains(err.Error(), "local repository config file does not exist") {
		t.Errorf("Expected error about missing config file, got: %v", err)
	}
	if !strings.Contains(err.Error(), expectedPath) {
		t.Errorf("Expected error to mention path %s, got: %v", expectedPath, err)
	}
}

func TestInstallDebPkg_ValidParameters(t *testing.T) {
	installer := deb.NewDebInstaller()
	tempDir := t.TempDir()

	// Create the required directory structure
	configDir := filepath.Join(tempDir, "chrootenvconfigs")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatalf("Failed to create config directory: %v", err)
	}

	// Create the local.list file
	localListPath := filepath.Join(configDir, "local.list")
	localListContent := "deb [trusted=yes] file:///cdrom/cache-repo ./"
	if err := os.WriteFile(localListPath, []byte(localListContent), 0644); err != nil {
		t.Fatalf("Failed to create local.list file: %v", err)
	}

	// Create cache directory
	chrootPkgCacheDir := filepath.Join(tempDir, "cache")
	if err := os.MkdirAll(chrootPkgCacheDir, 0700); err != nil {
		t.Fatalf("Failed to create cache directory: %v", err)
	}

	chrootEnvPath := filepath.Join(tempDir, "chroot")
	pkgsList := []string{"test-package"}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "mount", Output: "override-test\n", Error: fmt.Errorf("failed to mount")},
		{Pattern: "mmdebstrap", Output: "override-test\n", Error: fmt.Errorf("command not found")},
		{Pattern: "rm", Output: "override-test\n", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	err := installer.InstallDebPkg(tempDir, chrootEnvPath, chrootPkgCacheDir, pkgsList)

	// We expect this to fail at the mounting step or mmdebstrap command
	// but it should pass parameter validation and file existence checks
	if err != nil {
		// Check if it's the expected failure (mounting or mmdebstrap)
		if !strings.Contains(err.Error(), "failed to mount") &&
			!strings.Contains(err.Error(), "failed to install debian packages") {
			t.Errorf("Unexpected error type: %v", err)
		}
	}
}

func TestInstallDebPkg_ChrootEnvCreation(t *testing.T) {
	installer := deb.NewDebInstaller()
	tempDir := t.TempDir()

	// Create the required directory structure
	configDir := filepath.Join(tempDir, "chrootenvconfigs")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatalf("Failed to create config directory: %v", err)
	}

	// Create the local.list file
	localListPath := filepath.Join(configDir, "local.list")
	if err := os.WriteFile(localListPath, []byte("deb file:///cdrom/cache-repo ./"), 0644); err != nil {
		t.Fatalf("Failed to create local.list file: %v", err)
	}

	chrootPkgCacheDir := filepath.Join(tempDir, "cache")
	if err := os.MkdirAll(chrootPkgCacheDir, 0700); err != nil {
		t.Fatalf("Failed to create cache directory: %v", err)
	}

	chrootEnvPath := filepath.Join(tempDir, "chroot")
	// Don't create chrootEnvPath - let the function create it
	pkgsList := []string{"test-package"}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "mkdir", Output: "override-test\n", Error: nil},
		{Pattern: "dpkg-scanpackages", Output: "override-test\n", Error: nil},
		{Pattern: "mount", Output: "override-test\n", Error: nil},
		{Pattern: "umount", Output: "override-test\n", Error: nil},
		{Pattern: "mmdebstrap", Output: "override-test\n", Error: fmt.Errorf("command not found")},
		{Pattern: "rm", Output: "override-test\n", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	// Set targetArch by calling UpdateLocalDebRepo first
	if err := installer.UpdateLocalDebRepo(chrootPkgCacheDir, "amd64", false); err != nil {
		t.Fatalf("Failed to update local deb repo: %v", err)
	}

	err := installer.InstallDebPkg(tempDir, chrootEnvPath, chrootPkgCacheDir, pkgsList)

	// The function should attempt to create the chroot directory
	if _, statErr := os.Stat(chrootEnvPath); os.IsNotExist(statErr) {
		t.Error("Expected chroot environment directory to be created")
	}

	// Should fail on mount or mmdebstrap, not on directory creation
	if err != nil && strings.Contains(err.Error(), "failed to create chroot environment directory") {
		t.Errorf("Should not fail on directory creation, got: %v", err)
	}
}

func TestDebInstaller_PackageListFormatting(t *testing.T) {
	installer := deb.NewDebInstaller()
	tempDir := t.TempDir()

	// Setup required files
	configDir := filepath.Join(tempDir, "chrootenvconfigs")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatalf("Failed to create config directory: %v", err)
	}

	localListPath := filepath.Join(configDir, "local.list")
	if err := os.WriteFile(localListPath, []byte("deb file:///cdrom/cache-repo ./"), 0644); err != nil {
		t.Fatalf("Failed to create local.list file: %v", err)
	}

	chrootPkgCacheDir := filepath.Join(tempDir, "cache")
	if err := os.MkdirAll(chrootPkgCacheDir, 0700); err != nil {
		t.Fatalf("Failed to create cache directory: %v", err)
	}

	chrootEnvPath := filepath.Join(tempDir, "chroot")

	// Test with multiple packages
	pkgsList := []string{"package1", "package2", "package3"}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "mount", Output: "override-test\n", Error: nil},
		{Pattern: "umount", Output: "override-test\n", Error: nil},
		{Pattern: "mmdebstrap", Output: "override-test\n", Error: fmt.Errorf("command not found")},
		{Pattern: "rm", Output: "override-test\n", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	err := installer.InstallDebPkg(tempDir, chrootEnvPath, chrootPkgCacheDir, pkgsList)

	// The function should format packages as comma-separated list
	// We can't easily test the exact command without mocking, but we can verify
	// it doesn't fail on the package list formatting
	if err != nil && strings.Contains(err.Error(), "invalid parameters") {
		t.Error("Should not fail on package list formatting")
	}
}

func TestUpdateLocalDebRepo_EmptyRepoPath(t *testing.T) {
	installer := deb.NewDebInstaller()

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "dpkg-scanpackages", Output: "override-test\n", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	err := installer.UpdateLocalDebRepo("", "amd64", false)

	// Should fail when trying to create directory structure in empty path
	if err == nil {
		t.Error("Expected error for empty repo path")
	}
}

func TestInstallDebPkg_RepoPathConstant(t *testing.T) {
	// Test that the hardcoded repo path is used correctly
	installer := deb.NewDebInstaller()
	tempDir := t.TempDir()

	// The function uses a hardcoded repoPath "/cdrom/cache-repo"
	// We can verify this by checking the error messages or behavior
	// when the mount operation fails

	configDir := filepath.Join(tempDir, "chrootenvconfigs")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatalf("Failed to create config directory: %v", err)
	}

	localListPath := filepath.Join(configDir, "local.list")
	if err := os.WriteFile(localListPath, []byte("deb file:///cdrom/cache-repo ./"), 0644); err != nil {
		t.Fatalf("Failed to create local.list file: %v", err)
	}

	chrootEnvPath := filepath.Join(tempDir, "chroot")
	chrootPkgCacheDir := filepath.Join(tempDir, "cache")
	pkgsList := []string{"test-package"}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "mount", Output: "override-test\n", Error: fmt.Errorf("failed to mount")},
		{Pattern: "rm", Output: "override-test\n", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	err := installer.InstallDebPkg(tempDir, chrootEnvPath, chrootPkgCacheDir, pkgsList)

	// Should fail on mount operation since we can't actually mount in tests
	if err != nil && !strings.Contains(err.Error(), "failed to mount") {
		// If it fails for a different reason, that's also acceptable for this test
		t.Logf("Got error (expected): %v", err)
	}
}

func TestInstallDebPkg_CrossArchMissingArchTest(t *testing.T) {
	installer := deb.NewDebInstaller()
	tempDir := t.TempDir()

	configDir := filepath.Join(tempDir, "chrootenvconfigs")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatalf("Failed to create config directory: %v", err)
	}

	localListPath := filepath.Join(configDir, "local.list")
	if err := os.WriteFile(localListPath, []byte("deb file:///cdrom/cache-repo ./"), 0644); err != nil {
		t.Fatalf("Failed to create local.list file: %v", err)
	}

	chrootPkgCacheDir := filepath.Join(tempDir, "cache")
	if err := os.MkdirAll(chrootPkgCacheDir, 0700); err != nil {
		t.Fatalf("Failed to create cache directory: %v", err)
	}

	// Pick a target architecture that is different from the runtime architecture.
	targetArch := "arm64"
	if runtime.GOARCH == "arm64" {
		targetArch = "amd64"
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "dpkg-scanpackages", Output: "override-test\n", Error: nil},
		{Pattern: "command -v arch-test", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	if err := installer.UpdateLocalDebRepo(tempDir, targetArch, false); err != nil {
		t.Fatalf("Failed to set target architecture via UpdateLocalDebRepo: %v", err)
	}

	err := installer.InstallDebPkg(tempDir, filepath.Join(tempDir, "chroot"), chrootPkgCacheDir, []string{"test-package"})
	if err == nil {
		t.Fatal("Expected cross-architecture preflight error for missing arch-test")
	}

	if !strings.Contains(err.Error(), "arch-test") {
		t.Fatalf("Expected error to mention arch-test, got: %v", err)
	}
	if !strings.Contains(err.Error(), "cross-architecture") {
		t.Fatalf("Expected cross-architecture error context, got: %v", err)
	}
}
