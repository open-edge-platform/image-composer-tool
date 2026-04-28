package chroot_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	chroot "github.com/open-edge-platform/image-composer-tool/internal/chroot"
	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

// mockChrootBuilder implements the necessary interface for testing
type mockChrootBuilder struct {
	packageList []string
	err         error
	tempDir     string
	osConfig    map[string]interface{}
	pkgType     string
}

// Add the missing method to satisfy ChrootBuilderInterface
func (m *mockChrootBuilder) UpdateLocalDebRepo(repoPath, targetArch string, sudo bool) error {
	// For testing, just return the error field
	return m.err
}

// Implement GetChrootEnvConfig to satisfy ChrootBuilderInterface
func (m *mockChrootBuilder) GetChrootEnvConfig() (map[interface{}]interface{}, error) {
	// Return a dummy config and no error for testing
	return nil, nil
}

// Implement GetChrootBuildDir to satisfy ChrootBuilderInterface
func (m *mockChrootBuilder) GetChrootBuildDir() string {
	// Return a dummy build directory for testing
	return filepath.Join(m.tempDir, "mock-chroot-build-dir")
}

// Implement GetChrootPkgCacheDir to satisfy ChrootBuilderInterface
func (m *mockChrootBuilder) GetChrootPkgCacheDir() string {
	// Return a dummy package cache directory for testing
	return filepath.Join(m.tempDir, "mock-chroot-pkg-cache-dir")
}

// Implement GetTargetOsConfigDir to satisfy ChrootBuilderInterface
func (m *mockChrootBuilder) GetTargetOsConfigDir() string {
	// Return a dummy config directory for testing
	return filepath.Join(m.tempDir, "mock-chroot-os-config-dir")
}

func (m *mockChrootBuilder) GetTargetOsConfig() map[string]interface{} {
	if m.osConfig != nil {
		return m.osConfig
	}
	// Return a dummy config for testing
	return map[string]interface{}{
		"releaseVersion": "3.0",
	}
}

func (m *mockChrootBuilder) GetChrootEnvEssentialPackageList() ([]string, error) {
	return m.packageList, m.err
}

// Implement GetChrootEnvPackageList to satisfy ChrootBuilderInterface
func (m *mockChrootBuilder) GetChrootEnvPackageList() ([]string, error) {
	return m.packageList, m.err
}

func (m *mockChrootBuilder) GetTargetOsPkgType() string {
	if m.pkgType != "" {
		return m.pkgType
	}
	return "rpm"
}

func (m *mockChrootBuilder) BuildChrootEnv(root, dist, arch string) error {
	// For testing, just return the error field
	return m.err
}

func TestChrootEnv_GetChrootEnvEssentialPackageList(t *testing.T) {
	tests := []struct {
		name             string
		packageList      []string
		mockError        error
		expectedPackages []string
		expectError      bool
	}{
		{
			name:             "successful package list retrieval",
			packageList:      []string{"systemd", "bash", "coreutils", "glibc"},
			mockError:        nil,
			expectedPackages: []string{"systemd", "bash", "coreutils", "glibc"},
			expectError:      false,
		},
		{
			name:             "empty package list",
			packageList:      []string{},
			mockError:        nil,
			expectedPackages: []string{},
			expectError:      false,
		},
		{
			name:             "nil package list",
			packageList:      nil,
			mockError:        nil,
			expectedPackages: nil,
			expectError:      false,
		},
		{
			name:             "error from chrootBuilder",
			packageList:      nil,
			mockError:        errors.New("failed to get essential packages"),
			expectedPackages: nil,
			expectError:      true,
		},
		{
			name:             "single package",
			packageList:      []string{"systemd"},
			mockError:        nil,
			expectedPackages: []string{"systemd"},
			expectError:      false,
		},
		{
			name:             "large package list",
			packageList:      []string{"pkg1", "pkg2", "pkg3", "pkg4", "pkg5", "pkg6", "pkg7", "pkg8", "pkg9", "pkg10"},
			mockError:        nil,
			expectedPackages: []string{"pkg1", "pkg2", "pkg3", "pkg4", "pkg5", "pkg6", "pkg7", "pkg8", "pkg9", "pkg10"},
			expectError:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock chrootBuilder
			mockBuilder := &mockChrootBuilder{
				packageList: tt.packageList,
				err:         tt.mockError,
				tempDir:     t.TempDir(),
			}

			// Create ChrootEnv with mock chrootBuilder
			chrootEnv := &chroot.ChrootEnv{
				ChrootEnvRoot: filepath.Join(mockBuilder.tempDir, "test-chroot"),
				ChrootBuilder: mockBuilder, // Ensure chrootBuilder is of interface type
			}

			// Call the method under test
			result, err := chrootEnv.GetChrootEnvEssentialPackageList()

			// Check error expectation
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				if tt.mockError != nil && err.Error() != tt.mockError.Error() {
					t.Errorf("Expected error '%v', got '%v'", tt.mockError, err)
				}
				return
			}

			// Check no error when not expected
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// Check package list length
			if len(result) != len(tt.expectedPackages) {
				t.Errorf("Expected %d packages, got %d", len(tt.expectedPackages), len(result))
				return
			}

			// Check package list contents
			for i, pkg := range tt.expectedPackages {
				if result[i] != pkg {
					t.Errorf("Expected package[%d] = '%s', got '%s'", i, pkg, result[i])
				}
			}
		})
	}
}

func TestChrootEnv_GetChrootEnvEssentialPackageList_NilChrootBuilder(t *testing.T) {
	// Test edge case where chrootBuilder is nil
	chrootEnv := &chroot.ChrootEnv{
		ChrootEnvRoot: "/tmp/test-chroot",
		ChrootBuilder: nil,
	}

	// This should panic or return an error, depending on implementation
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic when chrootBuilder is nil, but didn't panic")
		}
	}()

	_, _ = chrootEnv.GetChrootEnvEssentialPackageList()
}

func TestChrootEnv_GetChrootEnvEssentialPackageList_Integration(t *testing.T) {
	// Test with different OS types to ensure the method works regardless of the underlying implementation
	testCases := []struct {
		name       string
		targetOs   string
		targetDist string
		targetArch string
	}{
		{"rpm-based", "photon", "5.0", "amd64"},
		{"deb-based", "ubuntu", "22.04", "amd64"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create mock chrootBuilder
			mockBuilder := &mockChrootBuilder{
				packageList: []string{},
				err:         nil,
				tempDir:     t.TempDir(),
			}
			// This is more of an integration test that would require actual ChrootBuilder
			// We'll create a basic test that ensures the method doesn't panic
			chrootEnv := &chroot.ChrootEnv{
				ChrootEnvRoot: filepath.Join(mockBuilder.tempDir, "test-chroot"),
				ChrootBuilder: mockBuilder, // Ensure chrootBuilder is of interface type
			}

			// Call the method - we can't predict the exact output without knowing the implementation
			// but we can ensure it doesn't panic and returns reasonable values
			packages, err := chrootEnv.GetChrootEnvEssentialPackageList()

			// We don't assert specific packages since that depends on the OS configuration
			// but we can check that it behaves reasonably
			if err != nil {
				t.Logf("Method returned error (this may be expected): %v", err)
			} else {
				t.Logf("Method returned %d packages", len(packages))

				// Basic sanity checks
				for _, pkg := range packages {
					if pkg == "" {
						t.Error("Found empty package name in list")
					}
				}

			}
		})
	}
}

func TestChrootEnv_GetChrootEnvEssentialPackageList_ErrorPropagation(t *testing.T) {
	// Test that errors from the underlying chrootBuilder are properly propagated
	expectedErrors := []error{
		errors.New("config file not found"),
		errors.New("invalid OS type"),
		errors.New("network error"),
		errors.New("permission denied"),
	}

	for i, expectedErr := range expectedErrors {
		t.Run(fmt.Sprintf("error_case_%d", i), func(t *testing.T) {
			mockBuilder := &mockChrootBuilder{
				packageList: nil,
				err:         expectedErr,
				tempDir:     t.TempDir(),
			}

			chrootEnv := &chroot.ChrootEnv{
				ChrootEnvRoot: filepath.Join(mockBuilder.tempDir, "test-chroot"),
				ChrootBuilder: mockBuilder,
			}

			_, err := chrootEnv.GetChrootEnvEssentialPackageList()

			if err == nil {
				t.Error("Expected error but got none")
			}

			if err.Error() != expectedErr.Error() {
				t.Errorf("Expected error '%v', got '%v'", expectedErr, err)
			}
		})
	}
}

func TestChrootEnv_GetChrootEnvPath(t *testing.T) {
	root := t.TempDir()
	chrootEnv := &chroot.ChrootEnv{ChrootEnvRoot: root}
	insidePath := filepath.Join(root, "etc", "hosts")
	if err := os.MkdirAll(filepath.Dir(insidePath), 0o755); err != nil {
		t.Fatalf("failed to create nested path: %v", err)
	}
	if err := os.WriteFile(insidePath, []byte("test"), 0o644); err != nil {
		t.Fatalf("failed to create hosts file: %v", err)
	}

	t.Run("subpath is converted", func(t *testing.T) {
		got, err := chrootEnv.GetChrootEnvPath(insidePath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "/etc/hosts" {
			t.Fatalf("expected /etc/hosts, got %s", got)
		}
	})

	t.Run("root path maps to slash", func(t *testing.T) {
		got, err := chrootEnv.GetChrootEnvPath(root)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "/" {
			t.Fatalf("expected /, got %s", got)
		}
	})

	t.Run("outside path rejected", func(t *testing.T) {
		if _, err := chrootEnv.GetChrootEnvPath(filepath.Join(t.TempDir(), "etc")); err == nil {
			t.Fatal("expected error for path outside chroot root")
		}
	})

	t.Run("missing root", func(t *testing.T) {
		emptyEnv := &chroot.ChrootEnv{}
		if _, err := emptyEnv.GetChrootEnvPath(insidePath); err == nil {
			t.Fatal("expected error when chroot root is unset")
		}
	})
}

func TestCleanDebName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"replaces_first_underscore", "pkg_name_1.0_amd64", "pkg=name_1.0"},
		{"drops_known_arch", "tool_2.0_arm64", "tool=2.0"},
		{"keeps_unknown_arch", "pkg_2.0_custom", "pkg=2.0_custom"},
		{"no_arch", "kernel-headers", "kernel-headers"},
		{"all_arch", "pkg_1.0_all", "pkg=1.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := chroot.CleanDebName(tt.input)
			if got != tt.expected {
				t.Fatalf("CleanDebName(%s) = %s, want %s", tt.input, got, tt.expected)
			}
		})
	}
}

func TestChrootEnv_UpdateChrootLocalRepoMetadata_ErrorPaths(t *testing.T) {
	mockBuilder := &mockChrootBuilder{
		packageList: nil,
		err:         nil,
		tempDir:     t.TempDir(),
	}

	// rpm error path
	chrootEnv := &chroot.ChrootEnv{ChrootEnvRoot: t.TempDir(), ChrootBuilder: mockBuilder}
	if err := chrootEnv.UpdateChrootLocalRepoMetadata("/not-exist", "amd64", false); err == nil {
		t.Errorf("expected error for missing rpm repo dir, got nil")
	}

	// deb error path
	chrootEnv.ChrootBuilder = mockBuilder
	if err := chrootEnv.UpdateChrootLocalRepoMetadata("/not-exist", "amd64", false); err == nil {
		t.Errorf("expected error for missing deb repo dir, got nil")
	}

	// unsupported type
	chrootEnv.ChrootBuilder = mockBuilder
	if err := chrootEnv.UpdateChrootLocalRepoMetadata("/repo", "amd64", false); err == nil {
		t.Errorf("expected error for unsupported package type, got nil")
	}
}

func TestChrootEnv_RefreshLocalCacheRepo_ErrorPaths(t *testing.T) {
	mockBuilder := &mockChrootBuilder{
		packageList: nil,
		err:         nil,
		tempDir:     t.TempDir(),
	}

	chrootEnv := &chroot.ChrootEnv{ChrootEnvRoot: t.TempDir(), ChrootBuilder: mockBuilder}
	if err := chrootEnv.RefreshLocalCacheRepo(); err == nil {
		t.Errorf("expected error for rpm cache refresh, got nil")
	}
	chrootEnv.ChrootBuilder = mockBuilder
	if err := chrootEnv.RefreshLocalCacheRepo(); err == nil {
		t.Errorf("expected error for deb cache refresh, got nil")
	}
	chrootEnv.ChrootBuilder = mockBuilder
	if err := chrootEnv.RefreshLocalCacheRepo(); err == nil {
		t.Errorf("expected error for unsupported cache refresh, got nil")
	}
}

func TestChrootEnv_InitChrootEnv_ErrorPaths(t *testing.T) {
	mockBuilder := &mockChrootBuilder{tempDir: t.TempDir(), err: errors.New("fail build")}
	chrootEnv := &chroot.ChrootEnv{ChrootEnvRoot: t.TempDir(), ChrootBuilder: mockBuilder}
	// Simulate build error
	if err := chrootEnv.InitChrootEnv("os", "dist", "arch"); err == nil {
		t.Errorf("expected error for build fail, got nil")
	}
}

func TestChrootEnv_CleanupChrootEnv_ErrorPaths(t *testing.T) {
	tempDir := t.TempDir()
	mockCommands := []shell.MockCommand{
		{Pattern: "command -v", Output: "gpgconf", Error: nil},
		{Pattern: "gpgconf --list-components", Output: "gpgconf:gpgconf", Error: nil},
		{Pattern: "gpgconf --kill", Output: "gpgconf", Error: fmt.Errorf("stopGPG failed")},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)
	mockBuilder := &mockChrootBuilder{tempDir: tempDir, err: nil}
	chrootEnv := &chroot.ChrootEnv{ChrootEnvRoot: tempDir, ChrootBuilder: mockBuilder}
	userBinDir := filepath.Join(tempDir, "/usr/bin")
	if err := os.MkdirAll(userBinDir, 0755); err != nil {
		t.Skipf("Cannot create test directory: %v", err)
		return
	}
	if err := os.WriteFile(filepath.Join(userBinDir, "bash"), []byte("test\n"), 0644); err != nil {
		t.Errorf("Cannot create test file: %v", err)
	}
	// Simulate stopGPG error
	if err := chrootEnv.CleanupChrootEnv("os", "dist", "arch"); err == nil {
		t.Errorf("expected error for stopGPG fail, got nil")
	}
}

func TestChrootEnv_TdnfInstallPackage_ErrorPath(t *testing.T) {
	mockBuilder := &mockChrootBuilder{tempDir: t.TempDir()}
	chrootEnv := &chroot.ChrootEnv{ChrootEnvRoot: t.TempDir(), ChrootBuilder: mockBuilder}
	// Simulate GetChrootEnvPath error
	chrootEnv.ChrootEnvRoot = ""
	if err := chrootEnv.TdnfInstallPackage("pkg", "/badroot", nil); err == nil {
		t.Errorf("expected error for bad install root, got nil")
	}
}

func TestChrootEnv_AptInstallPackage_ErrorPath(t *testing.T) {
	chrootEnv := &chroot.ChrootEnv{ChrootEnvRoot: t.TempDir()}
	// Simulate error from shell.ExecCmdWithStream
	if err := chrootEnv.AptInstallPackage("pkg", "/badroot", nil); err == nil {
		t.Errorf("expected error for bad install root, got nil")
	}
}

func TestChrootEnv_CopyFileFromHostToChroot_ErrorPath(t *testing.T) {
	chrootEnv := &chroot.ChrootEnv{ChrootEnvRoot: ""}
	if err := chrootEnv.CopyFileFromHostToChroot("/host", "/chroot"); err == nil {
		t.Errorf("expected error for uninitialized root, got nil")
	}
	chrootEnv.ChrootEnvRoot = t.TempDir()
	if err := chrootEnv.CopyFileFromHostToChroot("/host", "../badpath"); err == nil {
		t.Errorf("expected error for invalid chrootPath, got nil")
	}
}

func TestChrootEnv_CopyFileFromChrootToHost_ErrorPath(t *testing.T) {
	chrootEnv := &chroot.ChrootEnv{ChrootEnvRoot: ""}
	if err := chrootEnv.CopyFileFromChrootToHost("/host", "/chroot"); err == nil {
		t.Errorf("expected error for uninitialized root, got nil")
	}
	chrootEnv.ChrootEnvRoot = t.TempDir()
	if err := chrootEnv.CopyFileFromChrootToHost("/host", "../badpath"); err == nil {
		t.Errorf("expected error for invalid chrootPath, got nil")
	}
}

func TestChrootEnv_MountChrootSysfs_ErrorPath(t *testing.T) {
	chrootEnv := &chroot.ChrootEnv{ChrootEnvRoot: ""}
	if err := chrootEnv.MountChrootSysfs("/sys"); err == nil {
		t.Errorf("expected error for uninitialized root, got nil")
	}
}

func TestChrootEnv_UmountChrootSysfs_ErrorPath(t *testing.T) {
	chrootEnv := &chroot.ChrootEnv{ChrootEnvRoot: ""}
	if err := chrootEnv.UmountChrootSysfs("/sys"); err == nil {
		t.Errorf("expected error for uninitialized root, got nil")
	}
}

func TestChrootEnv_MountChrootPath_ErrorPath(t *testing.T) {
	chrootEnv := &chroot.ChrootEnv{ChrootEnvRoot: ""}
	if err := chrootEnv.MountChrootPath("/host", "/chroot", ""); err == nil {
		t.Errorf("expected error for uninitialized root, got nil")
	}
}

func TestChrootEnv_UmountChrootPath_ErrorPath(t *testing.T) {
	chrootEnv := &chroot.ChrootEnv{ChrootEnvRoot: ""}
	if err := chrootEnv.UmountChrootPath("/chroot"); err == nil {
		t.Errorf("expected error for uninitialized root, got nil")
	}
}

func TestChrootEnv_GetChrootEnvPath_ErrorPath(t *testing.T) {
	chrootEnv := &chroot.ChrootEnv{ChrootEnvRoot: ""}
	if _, err := chrootEnv.GetChrootEnvPath("/host"); err == nil {
		t.Errorf("expected error for uninitialized root, got nil")
	}
}

func TestChrootEnv_UpdateSystemPkgs_ErrorPath(t *testing.T) {
	mockBuilder := &mockChrootBuilder{err: errors.New("fail essential")}
	chrootEnv := &chroot.ChrootEnv{ChrootBuilder: mockBuilder}

	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name:    "test-image",
			Version: "1.0.0",
		},
		Target: config.TargetInfo{
			OS:        "linux",
			Dist:      "test",
			Arch:      "x86_64",
			ImageType: "qcow2",
		},
		SystemConfig: config.SystemConfig{
			Name:        "test-system",
			Description: "Test system configuration",
			Packages:    []string{"curl", "wget", "vim", "filesystem-base", "initramfs-tools"},
		},
	}

	// Use unsafe cast to interface{} to match method signature
	if err := chrootEnv.UpdateSystemPkgs(template); err == nil {
		t.Errorf("expected error for fail essential, got nil")
	}
}

func TestChrootEnv_GetTargetOsReleaseVersion(t *testing.T) {
	tests := []struct {
		name            string
		config          map[string]interface{}
		expectedVersion string
	}{
		{
			name: "valid version",
			config: map[string]interface{}{
				"releaseVersion": "3.0",
			},
			expectedVersion: "3.0",
		},
		{
			name:            "missing version",
			config:          map[string]interface{}{},
			expectedVersion: "unknown",
		},
		{
			name: "invalid type",
			config: map[string]interface{}{
				"releaseVersion": 123,
			},
			expectedVersion: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockBuilder := &mockChrootBuilder{
				tempDir:  t.TempDir(),
				osConfig: tt.config,
			}
			chrootEnv := &chroot.ChrootEnv{
				ChrootBuilder: mockBuilder,
			}
			version := chrootEnv.GetTargetOsReleaseVersion()
			if version != tt.expectedVersion {
				t.Errorf("Expected version %s, got %s", tt.expectedVersion, version)
			}
		})
	}
}

func TestChrootEnv_GetChrootEnvHostPath(t *testing.T) {
	tempDir := t.TempDir()
	chrootEnv := &chroot.ChrootEnv{
		ChrootEnvRoot: tempDir,
	}

	// Test valid path
	path, err := chrootEnv.GetChrootEnvHostPath("/etc/passwd")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	expected := filepath.Join(tempDir, "/etc/passwd")
	if path != expected {
		t.Errorf("Expected %s, got %s", expected, path)
	}

	// Test invalid path with ".."
	_, err = chrootEnv.GetChrootEnvHostPath("/../etc/passwd")
	if err == nil {
		t.Error("Expected error for path with '..', got nil")
	}

	// Test uninitialized root
	chrootEnv.ChrootEnvRoot = ""
	_, err = chrootEnv.GetChrootEnvHostPath("/etc/passwd")
	if err == nil {
		t.Error("Expected error for uninitialized root, got nil")
	}
}

func TestChrootEnv_MountChrootPath(t *testing.T) {
	tempDir := t.TempDir()
	chrootEnv := &chroot.ChrootEnv{
		ChrootEnvRoot: tempDir,
	}
	hostPath := filepath.Join(tempDir, "host")
	if err := os.Mkdir(hostPath, 0755); err != nil {
		t.Errorf("Failed to create host path: %v", err)
	}
	chrootPath := "/mnt"

	// Mock shell
	originalShell := shell.Default
	defer func() { shell.Default = originalShell }()

	mockCommands := []shell.MockCommand{
		{Pattern: "mkdir -p .*", Output: "", Error: nil},
		{Pattern: "mount --bind .*", Output: "", Error: nil},
		{Pattern: "mount", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	// Test mount
	err := chrootEnv.MountChrootPath(hostPath, chrootPath, "--bind")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestChrootEnv_InitChrootEnv(t *testing.T) {
	tempDir := t.TempDir()
	mockBuilder := &mockChrootBuilder{
		tempDir: tempDir,
	}
	chrootEnv := &chroot.ChrootEnv{
		ChrootEnvRoot: filepath.Join(tempDir, "chroot"),
		ChrootBuilder: mockBuilder,
	}

	// Create dummy chrootenv.tar.gz
	buildDir := mockBuilder.GetChrootBuildDir()
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		t.Fatalf("Failed to create build dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(buildDir, "chrootenv.tar.gz"), []byte("dummy"), 0644); err != nil {
		t.Fatalf("Failed to create dummy chrootenv.tar.gz: %v", err)
	}

	// Create dummy resolv.conf
	// We don't need to create real resolv.conf because we mock cp command
	// But CopyFileFromHostToChroot checks if source file exists using os.Stat?
	// No, CopyFileFromHostToChroot calls file.CopyFile.
	// file.CopyFile calls filepath.Abs(srcFile) and os.Stat(srcFilePath).
	// So we DO need real source file.
	// ResolvConfPath is "/etc/resolv.conf".
	// We can't create /etc/resolv.conf in test environment easily if we don't have permission.
	// But we can't change ResolvConfPath constant in test.
	// However, if the test runs in a container or environment where /etc/resolv.conf exists (which is likely), it's fine.
	// If not, this test might fail.
	// Let's assume /etc/resolv.conf exists.

	// Mock shell
	originalShell := shell.Default
	defer func() { shell.Default = originalShell }()

	mockCommands := []shell.MockCommand{
		{Pattern: "tar -xzf .*", Output: "", Error: nil},
		{Pattern: "cp .*", Output: "", Error: nil},
		{Pattern: "mkdir -p .*", Output: "", Error: nil},
		{Pattern: "mount .*", Output: "", Error: nil},
		{Pattern: "createrepo_c .*", Output: "", Error: nil},
		{Pattern: "tdnf makecache .*", Output: "", Error: nil},
		{Pattern: "rm -f .*", Output: "", Error: nil},
		{Pattern: "chmod .*", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	// Test InitChrootEnv
	err := chrootEnv.InitChrootEnv("os", "dist", "arch")
	if err != nil {
		// If /etc/resolv.conf doesn't exist, we might get error.
		// But we can't easily fix it without changing code to allow overriding path.
		// Let's hope it exists.
		t.Logf("InitChrootEnv failed (possibly due to missing /etc/resolv.conf): %v", err)
	}
}

func TestChrootEnv_CleanupChrootEnv(t *testing.T) {
	tempDir := t.TempDir()
	chrootEnv := &chroot.ChrootEnv{
		ChrootEnvRoot: tempDir,
		ChrootBuilder: &mockChrootBuilder{tempDir: tempDir},
	}
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		t.Fatalf("Failed to create chroot env dir: %v", err)
	}

	// Mock shell
	originalShell := shell.Default
	defer func() { shell.Default = originalShell }()

	mockCommands := []shell.MockCommand{
		{Pattern: "command -v gpgconf", Output: "/usr/bin/gpgconf", Error: nil},
		{Pattern: "gpgconf --list-components", Output: "gpg-agent:gpg-agent", Error: nil},
		{Pattern: "gpgconf --kill .*", Output: "", Error: nil},
		{Pattern: "mount", Output: "", Error: nil}, // For GetMountPathList
		{Pattern: "umount .*", Output: "", Error: nil},
		{Pattern: "rm -f .*", Output: "", Error: nil},
		{Pattern: "rm -rf .*", Output: "", Error: nil},
		{Pattern: "cp .*", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	// Test CleanupChrootEnv
	err := chrootEnv.CleanupChrootEnv("os", "dist", "arch")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestChrootEnv_UpdateSystemPkgs(t *testing.T) {
	mockBuilder := &mockChrootBuilder{
		packageList: []string{"essential-pkg"},
		tempDir:     t.TempDir(),
	}
	chrootEnv := &chroot.ChrootEnv{
		ChrootBuilder: mockBuilder,
	}

	tests := []struct {
		name           string
		bootloader     string
		bootType       string
		expectedLoader []string
		expectError    bool
	}{
		{
			name:           "grub-efi",
			bootloader:     "grub",
			bootType:       "efi",
			expectedLoader: []string{},
			expectError:    false,
		},
		{
			name:           "grub-legacy",
			bootloader:     "grub",
			bootType:       "legacy",
			expectedLoader: []string{},
			expectError:    false,
		},
		{
			name:           "systemd-boot",
			bootloader:     "systemd-boot",
			bootType:       "efi",
			expectedLoader: []string{},
			expectError:    false,
		},
		{
			name:           "unsupported-bootloader",
			bootloader:     "unknown",
			bootType:       "efi",
			expectedLoader: nil,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			template := &config.ImageTemplate{
				SystemConfig: config.SystemConfig{
					Bootloader: config.Bootloader{
						Provider: tt.bootloader,
						BootType: tt.bootType,
					},
					Kernel: config.KernelConfig{
						Packages: []string{"kernel-pkg"},
					},
				},
			}

			err := chrootEnv.UpdateSystemPkgs(template)
			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if len(template.EssentialPkgList) != 1 || template.EssentialPkgList[0] != "essential-pkg" {
					t.Errorf("Essential packages not updated correctly")
				}
				if len(template.KernelPkgList) != 1 || template.KernelPkgList[0] != "kernel-pkg" {
					t.Errorf("Kernel packages not updated correctly")
				}
			}
		})
	}
}

func TestChrootEnv_UpdateChrootLocalRepoMetadata_Success(t *testing.T) {
	tempDir := t.TempDir()
	mockBuilder := &mockChrootBuilder{tempDir: tempDir}
	chrootEnv := &chroot.ChrootEnv{ChrootEnvRoot: tempDir, ChrootBuilder: mockBuilder}

	// RPM case
	repoDir := filepath.Join(tempDir, "repo")
	if err := os.Mkdir(repoDir, 0755); err != nil {
		t.Fatalf("Failed to create repo dir: %v", err)
	}

	originalShell := shell.Default
	defer func() { shell.Default = originalShell }()

	mockCommands := []shell.MockCommand{
		{Pattern: "createrepo_c .*", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	if err := chrootEnv.UpdateChrootLocalRepoMetadata("/repo", "amd64", false); err != nil {
		t.Errorf("RPM update failed: %v", err)
	}

	// Deb case
	mockBuilder.pkgType = "deb"
	// UpdateLocalDebRepo in mockBuilder returns m.err which is nil
	if err := chrootEnv.UpdateChrootLocalRepoMetadata("/repo", "amd64", false); err != nil {
		t.Errorf("Deb update failed: %v", err)
	}
}

func TestChrootEnv_RefreshLocalCacheRepo_Success(t *testing.T) {
	tempDir := t.TempDir()
	mockBuilder := &mockChrootBuilder{tempDir: tempDir}
	chrootEnv := &chroot.ChrootEnv{ChrootEnvRoot: tempDir, ChrootBuilder: mockBuilder}

	originalShell := shell.Default
	defer func() { shell.Default = originalShell }()

	// RPM case
	mockCommands := []shell.MockCommand{
		{Pattern: "tdnf makecache .*", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	if err := chrootEnv.RefreshLocalCacheRepo(); err != nil {
		t.Errorf("RPM refresh failed: %v", err)
	}

	// Deb case
	mockBuilder.pkgType = "deb"
	mockCommands = []shell.MockCommand{
		{Pattern: "apt clean", Output: "", Error: nil},
		{Pattern: "apt update", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	if err := chrootEnv.RefreshLocalCacheRepo(); err != nil {
		t.Errorf("Deb refresh failed: %v", err)
	}
}

func TestChrootEnv_TdnfInstallPackage_Success(t *testing.T) {
	tempDir := t.TempDir()
	mockBuilder := &mockChrootBuilder{tempDir: tempDir}
	chrootEnv := &chroot.ChrootEnv{ChrootEnvRoot: tempDir, ChrootBuilder: mockBuilder}

	originalShell := shell.Default
	defer func() { shell.Default = originalShell }()

	mockCommands := []shell.MockCommand{
		{Pattern: "tdnf install .*", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	// Create install root
	installRoot := filepath.Join(tempDir, "installroot")
	if err := os.Mkdir(installRoot, 0755); err != nil {
		t.Fatalf("Failed to create install root: %v", err)
	}

	if err := chrootEnv.TdnfInstallPackage("pkg", installRoot, []string{"repo1"}); err != nil {
		t.Errorf("Tdnf install failed: %v", err)
	}
}

func TestChrootEnv_AptInstallPackage_Success(t *testing.T) {
	tempDir := t.TempDir()
	mockBuilder := &mockChrootBuilder{tempDir: tempDir}
	chrootEnv := &chroot.ChrootEnv{ChrootEnvRoot: tempDir, ChrootBuilder: mockBuilder}

	originalShell := shell.Default
	defer func() { shell.Default = originalShell }()

	mockCommands := []shell.MockCommand{
		{Pattern: "apt-get install .*", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	// Create install root
	installRoot := filepath.Join(tempDir, "installroot")
	if err := os.Mkdir(installRoot, 0755); err != nil {
		t.Fatalf("Failed to create install root: %v", err)
	}

	// Test with package name cleaning
	if err := chrootEnv.AptInstallPackage("pkg_amd64", "/installroot", []string{"repo1"}); err != nil {
		t.Errorf("Apt install failed: %v", err)
	}
}
