package rpm_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/chroot/rpm"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

func TestNewRpmInstaller(t *testing.T) {
	installer := rpm.NewRpmInstaller()
	if installer == nil {
		t.Fatal("NewRpmInstaller should return a non-nil instance")
	}
}

func TestInstallRpmPkg_ParameterValidation(t *testing.T) {
	installer := rpm.NewRpmInstaller()
	tempDir := t.TempDir()

	tests := []struct {
		name              string
		targetOs          string
		chrootEnvPath     string
		chrootPkgCacheDir string
		allPkgsList       []string
		setupFiles        func(string) error
		expectedError     string
	}{
		{
			name:              "empty package list",
			targetOs:          "azure-linux",
			chrootEnvPath:     filepath.Join(tempDir, "chroot1"),
			chrootPkgCacheDir: filepath.Join(tempDir, "cache1"),
			allPkgsList:       []string{},
			setupFiles:        nil,
			expectedError:     "",
		},
		{
			name:              "nil package list",
			targetOs:          "azure-linux",
			chrootEnvPath:     filepath.Join(tempDir, "chroot2"),
			chrootPkgCacheDir: filepath.Join(tempDir, "cache2"),
			allPkgsList:       nil,
			setupFiles:        nil,
			expectedError:     "",
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "mkdir", Output: "", Error: nil},
		{Pattern: "mount", Output: "", Error: fmt.Errorf("mount failed")},
		{Pattern: "umount", Output: "", Error: nil},
		{Pattern: "rm", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setupFiles != nil {
				if err := tt.setupFiles(tempDir); err != nil {
					t.Fatalf("Failed to setup test files: %v", err)
				}
			}

			err := installer.InstallRpmPkg(tt.targetOs, tt.chrootEnvPath, tt.chrootPkgCacheDir, tt.allPkgsList)

			// For empty/nil package lists, it should attempt to process but fail on mount
			if err != nil && !strings.Contains(err.Error(), "failed to mount system directories") {
				t.Errorf("Expected mount error, got: %v", err)
			}
		})
	}
}

func TestInstallRpmPkg_PackageNotFound(t *testing.T) {
	installer := rpm.NewRpmInstaller()
	tempDir := t.TempDir()

	chrootEnvPath := filepath.Join(tempDir, "chroot")
	chrootPkgCacheDir := filepath.Join(tempDir, "cache")

	// Create cache directory but don't create the package file
	if err := os.MkdirAll(chrootPkgCacheDir, 0700); err != nil {
		t.Fatalf("Failed to create cache directory: %v", err)
	}

	allPkgsList := []string{"nonexistent-package.rpm"}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "mkdir", Output: "", Error: nil},
		{Pattern: "mount", Output: "", Error: nil},
		{Pattern: "umount", Output: "", Error: nil},
		{Pattern: "chmod", Output: "", Error: nil},
		{Pattern: "rm", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	err := installer.InstallRpmPkg("azure-linux", chrootEnvPath, chrootPkgCacheDir, allPkgsList)

	if err == nil {
		t.Error("Expected error when package does not exist")
	}

	if !strings.Contains(err.Error(), "does not exist in cache directory") {
		t.Errorf("Expected package not found error, got: %v", err)
	}
}

func TestInstallRpmPkg_SuccessfulInstallation(t *testing.T) {
	installer := rpm.NewRpmInstaller()
	tempDir := t.TempDir()

	chrootEnvPath := filepath.Join(tempDir, "chroot")
	chrootPkgCacheDir := filepath.Join(tempDir, "cache")

	// Create cache directory and package files
	if err := os.MkdirAll(chrootPkgCacheDir, 0700); err != nil {
		t.Fatalf("Failed to create cache directory: %v", err)
	}

	pkgFile := filepath.Join(chrootPkgCacheDir, "test-package.rpm")
	if err := os.WriteFile(pkgFile, []byte("fake rpm content"), 0644); err != nil {
		t.Fatalf("Failed to create test package: %v", err)
	}

	allPkgsList := []string{"test-package.rpm"}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "mkdir", Output: "", Error: nil},
		{Pattern: "mount", Output: "", Error: nil},
		{Pattern: "umount", Output: "", Error: nil},
		{Pattern: "chmod", Output: "", Error: nil},
		{Pattern: "rm", Output: "", Error: nil},
		{Pattern: "rpm -i", Output: "Installing test-package.rpm", Error: nil},
		{Pattern: "rpm -E", Output: "sqlite", Error: nil},
		{Pattern: "rpm --initdb", Output: "", Error: nil},
		{Pattern: "rpm -q -l", Output: "/etc/pki/rpm-gpg/RPM-GPG-KEY-test", Error: nil},
		{Pattern: "rpm --import", Output: "", Error: nil},
		{Pattern: "command -v", Output: "", Error: fmt.Errorf("command not found")},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	err := installer.InstallRpmPkg("azure-linux", chrootEnvPath, chrootPkgCacheDir, allPkgsList)

	// Should succeed with proper mocking
	if err != nil {
		t.Errorf("Expected successful installation, got error: %v", err)
	}
}

func TestUpdateRpmDB_SameBackend(t *testing.T) {
	installer := rpm.NewRpmInstaller()
	tempDir := t.TempDir()

	chrootEnvBuildPath := filepath.Join(tempDir, "chroot")
	chrootPkgCacheDir := filepath.Join(tempDir, "cache")
	rpmList := []string{"test-package.rpm"}

	// Create cache directory and package files
	if err := os.MkdirAll(chrootPkgCacheDir, 0700); err != nil {
		t.Fatalf("Failed to create cache directory: %v", err)
	}

	pkgFile := filepath.Join(chrootPkgCacheDir, "test-package.rpm")
	if err := os.WriteFile(pkgFile, []byte("fake rpm content"), 0644); err != nil {
		t.Fatalf("Failed to create test package: %v", err)
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "mkdir", Output: "", Error: nil},
		{Pattern: "mount", Output: "", Error: nil},
		{Pattern: "umount", Output: "", Error: nil},
		{Pattern: "chmod", Output: "", Error: nil},
		{Pattern: "rm", Output: "", Error: nil},
		{Pattern: "rpm -i", Output: "Installing test-package.rpm", Error: nil},
		{Pattern: "rpm -E", Output: "sqlite", Error: nil},
		{Pattern: "rpm --initdb", Output: "", Error: fmt.Errorf("should not be called")},
		{Pattern: "rpm -q -l", Output: "/etc/pki/rpm-gpg/RPM-GPG-KEY-test", Error: nil},
		{Pattern: "rpm --import", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	// Use reflection to call private method
	err := installer.InstallRpmPkg("azure-linux", chrootEnvBuildPath, chrootPkgCacheDir, rpmList)

	// Should not rebuild database when backends match
	// We expect this to fail earlier due to missing package file, but not due to DB rebuild
	if err != nil && strings.Contains(err.Error(), "failed to remove RPM database") {
		t.Error("Should not attempt to rebuild database when backends match")
	}
}

func TestUpdateRpmDB_DifferentBackend(t *testing.T) {
	installer := rpm.NewRpmInstaller()
	tempDir := t.TempDir()

	chrootEnvBuildPath := filepath.Join(tempDir, "chroot")
	chrootPkgCacheDir := filepath.Join(tempDir, "cache")

	// Create package cache directory and test package
	if err := os.MkdirAll(chrootPkgCacheDir, 0700); err != nil {
		t.Fatalf("Failed to create cache directory: %v", err)
	}

	pkgFile := filepath.Join(chrootPkgCacheDir, "test-package.rpm")
	if err := os.WriteFile(pkgFile, []byte("fake rpm"), 0644); err != nil {
		t.Fatalf("Failed to create test package: %v", err)
	}

	rpmList := []string{"test-package.rpm"}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "mkdir", Output: "", Error: nil},
		{Pattern: "mount", Output: "", Error: nil},
		{Pattern: "umount", Output: "", Error: nil},
		{Pattern: "chmod", Output: "", Error: nil},
		{Pattern: "rm", Output: "", Error: nil},
		{Pattern: "chroot .+ rpm -E", Output: "bdb", Error: nil},
		{Pattern: "rpm -E", Output: "sqlite", Error: nil},
		{Pattern: "rpm -i", Output: "Installing package", Error: nil},
		{Pattern: "rpm --initdb", Output: "", Error: nil},
		{Pattern: "rpm -q -l", Output: "/etc/pki/rpm-gpg/RPM-GPG-KEY-test", Error: nil},
		{Pattern: "rpm --import", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	err := installer.InstallRpmPkg("azure-linux", chrootEnvBuildPath, chrootPkgCacheDir, rpmList)

	// Should attempt to rebuild database when backends differ
	if err != nil && !strings.Contains(err.Error(), "failed to mount") &&
		!strings.Contains(err.Error(), "failed to stop GPG components") {
		t.Errorf("Unexpected error during database rebuild: %v", err)
	}
}

func TestUpdateRpmDB_FailedHostBackendQuery(t *testing.T) {
	installer := rpm.NewRpmInstaller()
	tempDir := t.TempDir()

	chrootEnvBuildPath := filepath.Join(tempDir, "chroot")
	chrootPkgCacheDir := filepath.Join(tempDir, "cache")
	rpmList := []string{"test-package.rpm"}

	// Create package cache directory and test package
	if err := os.MkdirAll(chrootPkgCacheDir, 0700); err != nil {
		t.Fatalf("Failed to create cache directory: %v", err)
	}

	pkgFile := filepath.Join(chrootPkgCacheDir, "test-package.rpm")
	if err := os.WriteFile(pkgFile, []byte("fake rpm"), 0644); err != nil {
		t.Fatalf("Failed to create test package: %v", err)
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "mkdir", Output: "", Error: nil},
		{Pattern: "mount", Output: "", Error: nil},
		{Pattern: "umount", Output: "", Error: nil},
		{Pattern: "chmod", Output: "", Error: nil},
		{Pattern: "rm", Output: "", Error: nil},
		{Pattern: "rpm -E", Output: "sqlite", Error: fmt.Errorf("rpm command failed")},
		{Pattern: "rpm -i", Output: "Installing package", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	err := installer.InstallRpmPkg("azure-linux", chrootEnvBuildPath, chrootPkgCacheDir, rpmList)

	if err == nil {
		t.Error("Expected error when host RPM DB backend query fails")
	}

	if !strings.Contains(err.Error(), "failed to get host RPM DB backend") {
		t.Errorf("Expected host RPM DB backend error, got: %v", err)
	}
}

func TestUpdateRpmDB_FailedChrootBackendQuery(t *testing.T) {
	installer := rpm.NewRpmInstaller()
	tempDir := t.TempDir()

	chrootEnvBuildPath := filepath.Join(tempDir, "chroot")
	chrootPkgCacheDir := filepath.Join(tempDir, "cache")
	rpmList := []string{"test-package.rpm"}

	// Create package cache directory and test package
	if err := os.MkdirAll(chrootPkgCacheDir, 0700); err != nil {
		t.Fatalf("Failed to create cache directory: %v", err)
	}

	pkgFile := filepath.Join(chrootPkgCacheDir, "test-package.rpm")
	if err := os.WriteFile(pkgFile, []byte("fake rpm"), 0644); err != nil {
		t.Fatalf("Failed to create test package: %v", err)
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "mkdir", Output: "", Error: nil},
		{Pattern: "mount", Output: "", Error: nil},
		{Pattern: "umount", Output: "", Error: nil},
		{Pattern: "chmod", Output: "", Error: nil},
		{Pattern: "rm", Output: "", Error: nil},
		{Pattern: "rpm -i", Output: "Installing package", Error: nil},
		{Pattern: "chroot .+ rpm -E", Output: "", Error: fmt.Errorf("rpm command failed")},
		{Pattern: "rpm -E", Output: "sqlite", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	err := installer.InstallRpmPkg("azure-linux", chrootEnvBuildPath, chrootPkgCacheDir, rpmList)

	if err == nil {
		t.Error("Expected error when chroot RPM DB backend query fails")
	}

	if !strings.Contains(err.Error(), "failed to get chroot RPM DB backend") {
		t.Errorf("Expected chroot RPM DB backend error, got: %v", err)
	}
}

func TestUpdateRpmDB_DatabaseRebuildFailure(t *testing.T) {
	installer := rpm.NewRpmInstaller()
	tempDir := t.TempDir()

	chrootEnvBuildPath := filepath.Join(tempDir, "chroot")
	chrootPkgCacheDir := filepath.Join(tempDir, "cache")

	// Create package file
	if err := os.MkdirAll(chrootPkgCacheDir, 0700); err != nil {
		t.Fatalf("Failed to create cache directory: %v", err)
	}

	pkgFile := filepath.Join(chrootPkgCacheDir, "test-package.rpm")
	if err := os.WriteFile(pkgFile, []byte("fake rpm"), 0644); err != nil {
		t.Fatalf("Failed to create test package: %v", err)
	}

	rpmList := []string{"test-package.rpm"}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "mkdir", Output: "", Error: nil},
		{Pattern: "mount", Output: "", Error: nil},
		{Pattern: "umount", Output: "", Error: nil},
		{Pattern: "chmod", Output: "", Error: nil},
		{Pattern: "rm -rf /var/lib/rpm/*", Output: "", Error: fmt.Errorf("failed to remove RPM database")},
		{Pattern: "rm", Output: "", Error: nil},
		{Pattern: "rpm -i", Output: "Installing package", Error: nil},
		{Pattern: "chroot .+ rpm -E", Output: "bdb", Error: nil},
		{Pattern: "rpm -E", Output: "sqlite", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	err := installer.InstallRpmPkg("azure-linux", chrootEnvBuildPath, chrootPkgCacheDir, rpmList)

	if err == nil {
		t.Error("Expected error when RPM database removal fails")
	}

	if !strings.Contains(err.Error(), "failed to remove RPM database") {
		t.Errorf("Expected RPM database removal error, got: %v", err)
	}
}

func TestImportGpgKeys_EdgeMicrovisorToolkit(t *testing.T) {
	tempDir := t.TempDir()
	chrootEnvBuildPath := filepath.Join(tempDir, "chroot")

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "mkdir", Output: "", Error: nil},
		{Pattern: "mount", Output: "", Error: nil},
		{Pattern: "umount", Output: "", Error: nil},
		{Pattern: "chmod", Output: "", Error: nil},
		{Pattern: "rm", Output: "", Error: nil},
		{Pattern: "rpm -i", Output: "Installing package", Error: nil},
		{Pattern: "rpm -E", Output: "sqlite", Error: nil},
		{Pattern: "rpm -q -l edge-repos-shared", Output: "/etc/pki/rpm-gpg/RPM-GPG-KEY-edge\n/usr/share/doc/edge-repos", Error: nil},
		{Pattern: "rpm --import", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	// Test through public InstallRpmPkg method
	installer := rpm.NewRpmInstaller()

	// Create a package file to pass initial validation
	cacheDir := filepath.Join(tempDir, "cache")
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		t.Fatalf("Failed to create cache directory: %v", err)
	}

	pkgFile := filepath.Join(cacheDir, "test.rpm")
	if err := os.WriteFile(pkgFile, []byte("fake"), 0644); err != nil {
		t.Fatalf("Failed to create test package: %v", err)
	}

	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	err := installer.InstallRpmPkg("edge-microvisor-toolkit", chrootEnvBuildPath, cacheDir, []string{"test.rpm"})

	// Should not fail on GPG key import
	if err != nil && strings.Contains(err.Error(), "failed to import GPG keys") {
		t.Errorf("GPG key import should succeed for edge-microvisor-toolkit, got: %v", err)
	}
}

func TestImportGpgKeys_AzureLinux(t *testing.T) {
	tempDir := t.TempDir()
	chrootEnvBuildPath := filepath.Join(tempDir, "chroot")

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "mkdir", Output: "", Error: nil},
		{Pattern: "mount", Output: "", Error: nil},
		{Pattern: "umount", Output: "", Error: nil},
		{Pattern: "chmod", Output: "", Error: nil},
		{Pattern: "rm", Output: "", Error: nil},
		{Pattern: "rpm -i", Output: "Installing package", Error: nil},
		{Pattern: "rpm -E", Output: "sqlite", Error: nil},
		{Pattern: "rpm -q -l azurelinux-repos-shared", Output: "/etc/pki/rpm-gpg/RPM-GPG-KEY-azurelinux\n/usr/share/doc/azurelinux-repos-shared", Error: nil},
		{Pattern: "rpm --import", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	installer := rpm.NewRpmInstaller()

	// Create test package
	cacheDir := filepath.Join(tempDir, "cache")
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		t.Fatalf("Failed to create cache directory: %v", err)
	}

	pkgFile := filepath.Join(cacheDir, "test.rpm")
	if err := os.WriteFile(pkgFile, []byte("fake"), 0644); err != nil {
		t.Fatalf("Failed to create test package: %v", err)
	}

	err := installer.InstallRpmPkg("azure-linux", chrootEnvBuildPath, cacheDir, []string{"test.rpm"})

	// Should not fail on GPG key import
	if err != nil && strings.Contains(err.Error(), "failed to import GPG keys") {
		t.Errorf("GPG key import should succeed for azure-linux, got: %v", err)
	}
}

func TestImportGpgKeys_NoGpgKeysFound(t *testing.T) {
	tempDir := t.TempDir()
	chrootEnvBuildPath := filepath.Join(tempDir, "chroot")

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "mkdir", Output: "", Error: nil},
		{Pattern: "mount", Output: "", Error: nil},
		{Pattern: "umount", Output: "", Error: nil},
		{Pattern: "chmod", Output: "", Error: nil},
		{Pattern: "rm", Output: "", Error: nil},
		{Pattern: "rpm -i", Output: "Installing package", Error: nil},
		{Pattern: "rpm -E", Output: "sqlite", Error: nil},
		{Pattern: "rpm -q -l azurelinux-repos-shared", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	installer := rpm.NewRpmInstaller()

	// Create test package
	cacheDir := filepath.Join(tempDir, "cache")
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		t.Fatalf("Failed to create cache directory: %v", err)
	}

	pkgFile := filepath.Join(cacheDir, "test.rpm")
	if err := os.WriteFile(pkgFile, []byte("fake"), 0644); err != nil {
		t.Fatalf("Failed to create test package: %v", err)
	}

	err := installer.InstallRpmPkg("azure-linux", chrootEnvBuildPath, cacheDir, []string{"test.rpm"})

	if err == nil {
		t.Error("Expected error when no GPG keys found")
	}

	if !strings.Contains(err.Error(), "no GPG keys found") {
		t.Errorf("Expected 'no GPG keys found' error, got: %v", err)
	}
}

func TestImportGpgKeys_GpgQueryFailure(t *testing.T) {
	tempDir := t.TempDir()
	chrootEnvBuildPath := filepath.Join(tempDir, "chroot")

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "mkdir", Output: "", Error: nil},
		{Pattern: "mount", Output: "", Error: nil},
		{Pattern: "umount", Output: "", Error: nil},
		{Pattern: "chmod", Output: "", Error: nil},
		{Pattern: "rm", Output: "", Error: nil},
		{Pattern: "rpm -i", Output: "Installing package", Error: nil},
		{Pattern: "rpm -E", Output: "sqlite", Error: nil},
		{Pattern: "rpm -q -l azurelinux-repos-shared", Output: "", Error: fmt.Errorf("package not found")},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	installer := rpm.NewRpmInstaller()

	// Create test package
	cacheDir := filepath.Join(tempDir, "cache")
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		t.Fatalf("Failed to create cache directory: %v", err)
	}

	pkgFile := filepath.Join(cacheDir, "test.rpm")
	if err := os.WriteFile(pkgFile, []byte("fake"), 0644); err != nil {
		t.Fatalf("Failed to create test package: %v", err)
	}

	err := installer.InstallRpmPkg("azure-linux", chrootEnvBuildPath, cacheDir, []string{"test.rpm"})

	if err == nil {
		t.Error("Expected error when GPG key query fails")
	}

	if !strings.Contains(err.Error(), "failed to get GPG keys") {
		t.Errorf("Expected 'failed to get GPG keys' error, got: %v", err)
	}
}

func TestInstallRpmPkg_MultiplePackages(t *testing.T) {
	installer := rpm.NewRpmInstaller()
	tempDir := t.TempDir()

	chrootEnvPath := filepath.Join(tempDir, "chroot")
	chrootPkgCacheDir := filepath.Join(tempDir, "cache")

	// Create cache directory and multiple package files
	if err := os.MkdirAll(chrootPkgCacheDir, 0700); err != nil {
		t.Fatalf("Failed to create cache directory: %v", err)
	}

	packages := []string{"package1.rpm", "package2.rpm", "package3.rpm"}
	for _, pkg := range packages {
		pkgFile := filepath.Join(chrootPkgCacheDir, pkg)
		if err := os.WriteFile(pkgFile, []byte("fake rpm content"), 0644); err != nil {
			t.Fatalf("Failed to create test package %s: %v", pkg, err)
		}
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "mkdir", Output: "", Error: nil},
		{Pattern: "mount", Output: "", Error: nil},
		{Pattern: "umount", Output: "", Error: nil},
		{Pattern: "chmod", Output: "", Error: nil},
		{Pattern: "rm", Output: "", Error: nil},
		{Pattern: "rpm -i", Output: "Installing package", Error: nil},
		{Pattern: "rpm -E", Output: "sqlite", Error: nil},
		{Pattern: "rpm --initdb", Output: "", Error: nil},
		{Pattern: "rpm -q -l", Output: "/etc/pki/rpm-gpg/RPM-GPG-KEY-test", Error: nil},
		{Pattern: "rpm --import", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	err := installer.InstallRpmPkg("azure-linux", chrootEnvPath, chrootPkgCacheDir, packages)

	// Should handle multiple packages successfully
	if err != nil && !strings.Contains(err.Error(), "failed to stop GPG components") {
		t.Errorf("Expected successful installation of multiple packages or GPG component error, got: %v", err)
	}
}
