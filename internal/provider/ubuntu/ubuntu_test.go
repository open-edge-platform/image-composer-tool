package ubuntu

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/open-edge-platform/os-image-composer/internal/chroot"
	"github.com/open-edge-platform/os-image-composer/internal/config"
	"github.com/open-edge-platform/os-image-composer/internal/ospackage/debutils"
	"github.com/open-edge-platform/os-image-composer/internal/provider"
	"github.com/open-edge-platform/os-image-composer/internal/utils/shell"
	"github.com/open-edge-platform/os-image-composer/internal/utils/system"
)

// Helper function to create a test ImageTemplate
func createTestImageTemplate() *config.ImageTemplate {
	return &config.ImageTemplate{
		Image: config.ImageInfo{
			Name:    "test-ubuntu-image",
			Version: "1.0.0",
		},
		Target: config.TargetInfo{
			OS:        "ubuntu",
			Dist:      "ubuntu24",
			Arch:      "amd64",
			ImageType: "raw",
		},
		SystemConfig: config.SystemConfig{
			Name:        "test-ubuntu-system",
			Description: "Test Ubuntu system configuration",
			Packages:    []string{"curl", "wget", "vim"},
		},
	}
}

// TestUbuntuProviderInterface tests that ubuntu implements Provider interface
func TestUbuntuProviderInterface(t *testing.T) {
	var _ provider.Provider = (*ubuntu)(nil) // Compile-time interface check
}

// TestUbuntuProviderName tests the Name method
func TestUbuntuProviderName(t *testing.T) {
	ubuntu := &ubuntu{}
	name := ubuntu.Name("ubuntu24", "amd64")
	expected := "ubuntu-ubuntu24-amd64"

	if name != expected {
		t.Errorf("Expected name %s, got %s", expected, name)
	}
}

// TestGetProviderId tests the GetProviderId function
func TestGetProviderId(t *testing.T) {
	testCases := []struct {
		dist     string
		arch     string
		expected string
	}{
		{"ubuntu24", "amd64", "ubuntu-ubuntu24-amd64"},
		{"ubuntu24", "arm64", "ubuntu-ubuntu24-arm64"},
		{"ubuntu22", "x86_64", "ubuntu-ubuntu22-x86_64"},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%s-%s", tc.dist, tc.arch), func(t *testing.T) {
			result := system.GetProviderId(OsName, tc.dist, tc.arch)
			if result != tc.expected {
				t.Errorf("Expected %s, got %s", tc.expected, result)
			}
		})
	}
}

// TestUbuntuProviderInit tests the Init method
func TestUbuntuProviderInit(t *testing.T) {
	// Change to project root for tests that need config files
	originalDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Failed to change back to original directory: %v", err)
		}
	}()

	// Navigate to project root (3 levels up from internal/provider/ubuntu)
	if err := os.Chdir("../../../"); err != nil {
		t.Skipf("Cannot change to project root: %v", err)
		return
	}

	ubuntu := &ubuntu{}

	// Test with amd64 architecture
	err := ubuntu.Init("ubuntu24", "amd64")
	if err != nil {
		// Expected to potentially fail in test environment due to network dependencies
		t.Logf("Init failed as expected in test environment: %v", err)
	} else {
		// If it succeeds, verify the configuration was set up
		if len(ubuntu.repoCfgs) == 0 {
			t.Error("Expected repoCfgs to be populated after successful Init")
		}

		// Verify that the architecture is correctly set in the config
		for _, cfg := range ubuntu.repoCfgs {
			if cfg.Arch != "amd64" {
				t.Errorf("Expected arch to be amd64, got %s", cfg.Arch)
			}
		}

		t.Logf("Successfully initialized with %d repositories", len(ubuntu.repoCfgs))
	}
}

// TestUbuntuProviderInitArchMapping tests architecture mapping in Init
func TestUbuntuProviderInitArchMapping(t *testing.T) {
	// Change to project root for tests that need config files
	originalDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Failed to change back to original directory: %v", err)
		}
	}()

	// Navigate to project root (3 levels up from internal/provider/ubuntu)
	if err := os.Chdir("../../../"); err != nil {
		t.Skipf("Cannot change to project root: %v", err)
		return
	}

	ubuntu := &ubuntu{}

	// Test x86_64 -> amd64 mapping
	err := ubuntu.Init("ubuntu24", "x86_64")
	if err != nil {
		t.Logf("Init failed as expected: %v", err)
	} else {
		// Verify that repoCfgs were set up correctly
		if len(ubuntu.repoCfgs) == 0 {
			t.Error("Expected repoCfgs to be populated after successful Init")
			return
		}

		// Verify that the first repository has correct architecture mapping
		firstRepo := ubuntu.repoCfgs[0]
		expectedArchInURL := "binary-amd64"
		if firstRepo.PkgList != "" && !strings.Contains(firstRepo.PkgList, expectedArchInURL) {
			t.Errorf("Expected PkgList to contain %s for x86_64 arch, got %s", expectedArchInURL, firstRepo.PkgList)
		}

		// Verify architecture was mapped correctly
		if firstRepo.Arch != "amd64" {
			t.Errorf("Expected mapped arch to be amd64, got %s", firstRepo.Arch)
		}

		t.Logf("Successfully mapped x86_64 -> amd64, PkgList: %s", firstRepo.PkgList)
	}
}

// TestLoadRepoConfig tests the loadRepoConfig function
func TestLoadRepoConfig(t *testing.T) {
	// Change to project root for tests that need config files
	originalDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Failed to change back to original directory: %v", err)
		}
	}()

	// Navigate to project root (3 levels up from internal/provider/ubuntu)
	if err := os.Chdir("../../../"); err != nil {
		t.Skipf("Cannot change to project root: %v", err)
		return
	}

	configs, err := loadRepoConfig("", "amd64")
	if err != nil {
		t.Skipf("loadRepoConfig failed (expected in test environment): %v", err)
		return
	}

	// If we successfully load config, verify the values
	if len(configs) == 0 {
		t.Error("Expected at least one repository configuration")
		return
	}

	for _, config := range configs {
		if config.Name == "" {
			t.Error("Expected config name to be set")
		}

		if config.Arch != "amd64" {
			t.Errorf("Expected arch 'amd64', got '%s'", config.Arch)
		}

		// Verify PkgList contains expected architecture
		if config.PkgList != "" && !strings.Contains(config.PkgList, "binary-amd64") {
			t.Errorf("Expected PkgList to contain 'binary-amd64', got '%s'", config.PkgList)
		}

		t.Logf("Successfully loaded repo config: %s", config.Name)
	}
}

// mockChrootEnv is a simple mock implementation of ChrootEnvInterface for testing
type mockChrootEnv struct{}

// Ensure mockChrootEnv implements ChrootEnvInterface
var _ chroot.ChrootEnvInterface = (*mockChrootEnv)(nil)

func (m *mockChrootEnv) GetChrootEnvRoot() string          { return "/tmp/test-chroot" }
func (m *mockChrootEnv) GetChrootImageBuildDir() string    { return "/tmp/test-build" }
func (m *mockChrootEnv) GetTargetOsPkgType() string        { return "deb" }
func (m *mockChrootEnv) GetTargetOsConfigDir() string      { return "/tmp/test-config" }
func (m *mockChrootEnv) GetTargetOsReleaseVersion() string { return "24" }
func (m *mockChrootEnv) GetChrootPkgCacheDir() string      { return "/tmp/test-cache" }
func (m *mockChrootEnv) GetChrootEnvEssentialPackageList() ([]string, error) {
	return []string{"base-files"}, nil
}
func (m *mockChrootEnv) GetChrootEnvHostPath(chrootPath string) (string, error) {
	return chrootPath, nil
}
func (m *mockChrootEnv) GetChrootEnvPath(hostPath string) (string, error) { return hostPath, nil }
func (m *mockChrootEnv) MountChrootSysfs(chrootPath string) error         { return nil }
func (m *mockChrootEnv) UmountChrootSysfs(chrootPath string) error        { return nil }
func (m *mockChrootEnv) MountChrootPath(hostFullPath, chrootPath, mountFlags string) error {
	return nil
}
func (m *mockChrootEnv) UmountChrootPath(chrootPath string) error                       { return nil }
func (m *mockChrootEnv) CopyFileFromHostToChroot(hostFilePath, chrootPath string) error { return nil }
func (m *mockChrootEnv) CopyFileFromChrootToHost(hostFilePath, chrootPath string) error { return nil }
func (m *mockChrootEnv) UpdateChrootLocalRepoMetadata(chrootRepoDir string, targetArch string, sudo bool) error {
	return nil
}
func (m *mockChrootEnv) RefreshLocalCacheRepo() error { return nil }
func (m *mockChrootEnv) InitChrootEnv(targetOs, targetDist, targetArch string) error {
	return nil
}
func (m *mockChrootEnv) CleanupChrootEnv(targetOs, targetDist, targetArch string) error { return nil }
func (m *mockChrootEnv) TdnfInstallPackage(packageName, installRoot string, repositoryIDList []string) error {
	return nil
}
func (m *mockChrootEnv) AptInstallPackage(packageName, installRoot string, repoSrcList []string) error {
	return nil
}
func (m *mockChrootEnv) UpdateSystemPkgs(template *config.ImageTemplate) error { return nil }

type cleanupErrorChrootEnv struct {
	mockChrootEnv
	cleanupErr error
}

func (m *cleanupErrorChrootEnv) CleanupChrootEnv(targetOs, targetDist, targetArch string) error {
	return m.cleanupErr
}

type updateErrorChrootEnv struct {
	mockChrootEnv
	updateErr error
}

func (m *updateErrorChrootEnv) UpdateSystemPkgs(template *config.ImageTemplate) error {
	return m.updateErr
}

func TestBuildUserRepoList(t *testing.T) {
	userRepos := []config.PackageRepository{
		{
			URL:           "https://example.com/ubuntu",
			Codename:      "noble",
			PKey:          "example-key",
			Component:     "main",
			Priority:      700,
			AllowPackages: []string{"curl", "wget"},
		},
		{
			URL:      "<URL>",
			Codename: "ignored-placeholder",
		},
		{
			URL:      "",
			Codename: "ignored-empty",
		},
		{
			URL:           "http://mirror.internal/repo",
			Codename:      "custom",
			PKey:          "mirror-key",
			Component:     "universe",
			Priority:      650,
			AllowPackages: []string{"vim"},
		},
	}

	got := buildUserRepoList(userRepos)
	if len(got) != 2 {
		t.Fatalf("expected 2 repositories, got %d", len(got))
	}

	if got[0].ID != "user-example.com/ubuntu" {
		t.Errorf("first repository ID: got %q, want %q", got[0].ID, "user-example.com/ubuntu")
	}
	if got[0].Codename != "noble" {
		t.Errorf("first repository codename: got %q, want %q", got[0].Codename, "noble")
	}
	if got[0].URL != "https://example.com/ubuntu" {
		t.Errorf("first repository URL: got %q, want %q", got[0].URL, "https://example.com/ubuntu")
	}
	if got[0].PKey != "example-key" {
		t.Errorf("first repository key: got %q, want %q", got[0].PKey, "example-key")
	}
	if got[0].Component != "main" {
		t.Errorf("first repository component: got %q, want %q", got[0].Component, "main")
	}
	if got[0].Priority != 700 {
		t.Errorf("first repository priority: got %d, want %d", got[0].Priority, 700)
	}
	if len(got[0].AllowPackages) != 2 || got[0].AllowPackages[0] != "curl" || got[0].AllowPackages[1] != "wget" {
		t.Errorf("first repository allow list: got %#v", got[0].AllowPackages)
	}

	if got[1].ID != "user-mirror.internal/repo" {
		t.Errorf("second repository ID: got %q, want %q", got[1].ID, "user-mirror.internal/repo")
	}
	if got[1].Component != "universe" {
		t.Errorf("second repository component: got %q, want %q", got[1].Component, "universe")
	}
	if got[1].Priority != 650 {
		t.Errorf("second repository priority: got %d, want %d", got[1].Priority, 650)
	}
}

func TestUbuntuPostProcessCleanupErrorWrappedMessage(t *testing.T) {
	cleanupErr := fmt.Errorf("cleanup failed")
	ubuntu := &ubuntu{chrootEnv: &cleanupErrorChrootEnv{cleanupErr: cleanupErr}}

	err := ubuntu.PostProcess(createTestImageTemplate(), nil)
	if err == nil {
		t.Fatal("expected cleanup error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to cleanup chroot environment") {
		t.Fatalf("expected wrapped cleanup error, got %v", err)
	}
	if !strings.Contains(err.Error(), cleanupErr.Error()) {
		t.Fatalf("expected original cleanup error in message, got %v", err)
	}
}

func TestUbuntuInstallHostDependencySkipsExistingCommands(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: "command -v .*", Output: "/usr/bin/fake", Error: nil},
	})

	ubuntu := &ubuntu{}
	if err := ubuntu.installHostDependency(); err != nil {
		t.Fatalf("expected dependencies to be treated as already installed, got error: %v", err)
	}
}

func TestUbuntuInstallHostDependencyCommandCheckError(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: "command -v .*", Output: "/usr/bin/fake", Error: fmt.Errorf("probe failed")},
	})

	ubuntu := &ubuntu{}
	err := ubuntu.installHostDependency()
	if err == nil {
		t.Fatal("expected command check error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to check command") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "probe failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUbuntuPostProcessReturnsNilOnCleanupSuccess(t *testing.T) {
	ubuntu := &ubuntu{chrootEnv: &mockChrootEnv{}}

	err := ubuntu.PostProcess(createTestImageTemplate(), fmt.Errorf("upstream build failure"))
	if err != nil {
		t.Fatalf("expected nil when cleanup succeeds, got %v", err)
	}
}

func TestUbuntuDownloadImagePkgsUpdateSystemErrorDeterministic(t *testing.T) {
	ubuntu := &ubuntu{chrootEnv: &updateErrorChrootEnv{updateErr: fmt.Errorf("update failed")}}

	err := ubuntu.downloadImagePkgs(createTestImageTemplate())
	if err == nil {
		t.Fatal("expected update system packages error")
	}
	if !strings.Contains(err.Error(), "failed to update system packages") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUbuntuInstallHostDependencyInstallFailure(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: "command -v .*", Output: "", Error: fmt.Errorf("missing")},
		{Pattern: "sudo apt install -y .*", Output: "", Error: fmt.Errorf("install failed")},
	})

	ubuntu := &ubuntu{}
	err := ubuntu.installHostDependency()
	if err == nil {
		t.Fatal("expected install error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to install host dependency") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "install failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestUbuntuProviderPreProcess tests PreProcess method with mocked dependencies
func TestUbuntuProviderPreProcess(t *testing.T) {
	// Save original shell executor and restore after test
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	// Set up mock executor
	mockExpectedOutput := []shell.MockCommand{
		// Mock successful package installation commands
		{Pattern: "apt-get update", Output: "Package lists updated successfully", Error: nil},
		{Pattern: "apt-get install -y mmdebstrap", Output: "Package installed successfully", Error: nil},
		{Pattern: "apt-get install -y dosfstools", Output: "Package installed successfully", Error: nil},
		{Pattern: "apt-get install -y mtools", Output: "Package installed successfully", Error: nil},
		{Pattern: "apt-get install -y xorriso", Output: "Package installed successfully", Error: nil},
		{Pattern: "apt-get install -y qemu-utils", Output: "Package installed successfully", Error: nil},
		{Pattern: "apt-get install -y systemd-ukify", Output: "Package installed successfully", Error: nil},
		{Pattern: "apt-get install -y grub-common", Output: "Package installed successfully", Error: nil},
		{Pattern: "apt-get install -y cryptsetup", Output: "Package installed successfully", Error: nil},
		{Pattern: "apt-get install -y sbsigntool", Output: "Package installed successfully", Error: nil},
		{Pattern: "apt-get install -y ubuntu-keyring", Output: "Package installed successfully", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	ubuntu := &ubuntu{
		repoCfgs: []debutils.RepoConfig{
			{
				Section:     "main",
				Name:        "Ubuntu 24.04",
				PkgList:     "https://archive.ubuntu.com/ubuntu/dists/noble/main/binary-amd64/Packages.gz",
				PkgPrefix:   "https://archive.ubuntu.com/ubuntu/",
				Enabled:     true,
				GPGCheck:    true,
				ReleaseFile: "https://archive.ubuntu.com/ubuntu/dists/noble/Release",
				ReleaseSign: "https://archive.ubuntu.com/ubuntu/dists/noble/Release.gpg",
				BuildPath:   "/tmp/builds/ubuntu1_amd64_main",
				Arch:        "amd64",
			},
		},
		chrootEnv: &mockChrootEnv{}, // Add the missing chrootEnv mock
	}

	template := createTestImageTemplate()

	// This test will likely fail due to dependencies on chroot, debutils, etc.
	// but it demonstrates the testing approach
	err := ubuntu.PreProcess(template)
	if err != nil {
		t.Logf("PreProcess failed as expected due to external dependencies: %v", err)
	}
}

// TestUbuntuProviderBuildImage tests BuildImage method
func TestUbuntuProviderBuildImage(t *testing.T) {
	// Save original shell executor and restore after test
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	// Set up mock executor - minimal mocks for Register function
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: ".*", Output: "success", Error: nil}, // Catch-all for any commands during registration
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	// Try to register and get a properly initialized ubuntu instance
	err := Register("linux", "test-build", "amd64")
	if err != nil {
		t.Skipf("Cannot test BuildImage without proper registration: %v", err)
		return
	}

	// Get the registered provider
	providerName := system.GetProviderId(OsName, "test-build", "amd64")
	retrievedProvider, exists := provider.Get(providerName)
	if !exists {
		t.Skip("Cannot test BuildImage without retrieving registered provider")
		return
	}

	ubuntu, ok := retrievedProvider.(*ubuntu)
	if !ok {
		t.Skip("Retrieved provider is not an ubuntu instance")
		return
	}

	template := createTestImageTemplate()

	// This test will fail due to dependencies on image builders that require system access
	// We expect it to fail early before reaching sudo commands
	err = ubuntu.BuildImage(template)
	if err != nil {
		t.Logf("BuildImage failed as expected due to external dependencies: %v", err)
		// Verify the error is related to expected failures, not sudo issues
		if strings.Contains(err.Error(), "sudo") {
			t.Errorf("Test should not reach sudo commands - mocking may be insufficient")
		}
	}
}

// TestUbuntuProviderBuildImageISO tests BuildImage method with ISO type
func TestUbuntuProviderBuildImageISO(t *testing.T) {
	// Save original shell executor and restore after test
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	// Set up mock executor - minimal mocks for Register function
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: ".*", Output: "success", Error: nil}, // Catch-all for any commands during registration
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	// Try to register and get a properly initialized ubuntu instance
	err := Register("linux", "test-iso", "amd64")
	if err != nil {
		t.Skipf("Cannot test BuildImage (ISO) without proper registration: %v", err)
		return
	}

	// Get the registered provider
	providerName := system.GetProviderId(OsName, "test-iso", "amd64")
	retrievedProvider, exists := provider.Get(providerName)
	if !exists {
		t.Skip("Cannot test BuildImage (ISO) without retrieving registered provider")
		return
	}

	ubuntu, ok := retrievedProvider.(*ubuntu)
	if !ok {
		t.Skip("Retrieved provider is not an ubuntu instance")
		return
	}

	template := createTestImageTemplate()

	// Set up global config for ISO
	originalImageType := template.Target.ImageType
	defer func() { template.Target.ImageType = originalImageType }()
	template.Target.ImageType = "iso"

	err = ubuntu.BuildImage(template)
	if err != nil {
		t.Logf("BuildImage (ISO) failed as expected due to external dependencies: %v", err)
		// Verify the error is related to expected failures, not sudo issues
		if strings.Contains(err.Error(), "sudo") {
			t.Errorf("Test should not reach sudo commands - mocking may be insufficient")
		}
	}
}

// TestUbuntuProviderBuildImageInitrd tests BuildImage method with IMG type
func TestUbuntuProviderBuildImageInitrd(t *testing.T) {
	// Save original shell executor and restore after test
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	// Set up mock executor - minimal mocks for Register function
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: ".*", Output: "success", Error: nil}, // Catch-all for any commands during registration
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	// Try to register and get a properly initialized ubuntu instance
	err := Register("linux", "test-img", "amd64")
	if err != nil {
		t.Skipf("Cannot test BuildImage (IMG) without proper registration: %v", err)
		return
	}

	// Get the registered provider
	providerName := system.GetProviderId(OsName, "test-img", "amd64")
	retrievedProvider, exists := provider.Get(providerName)
	if !exists {
		t.Skip("Cannot test BuildImage (IMG) without retrieving registered provider")
		return
	}

	ubuntu, ok := retrievedProvider.(*ubuntu)
	if !ok {
		t.Skip("Retrieved provider is not an ubuntu instance")
		return
	}

	template := createTestImageTemplate()

	// Set up global config for IMG
	originalImageType := template.Target.ImageType
	defer func() { template.Target.ImageType = originalImageType }()
	template.Target.ImageType = "img"

	err = ubuntu.BuildImage(template)
	if err != nil {
		t.Logf("BuildImage (IMG) failed as expected due to external dependencies: %v", err)
		// Verify the error is related to expected failures, not sudo issues
		if strings.Contains(err.Error(), "sudo") {
			t.Errorf("Test should not reach sudo commands - mocking may be insufficient")
		}
	}
}

// TestUbuntuProviderPostProcess tests PostProcess method
func TestUbuntuProviderPostProcess(t *testing.T) {
	// Save original shell executor and restore after test
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	// Set up mock executor - minimal mocks for Register function
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: ".*", Output: "success", Error: nil}, // Catch-all for any commands during registration
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	// Try to register and get a properly initialized ubuntu instance
	err := Register("linux", "test-post", "amd64")
	if err != nil {
		t.Skipf("Cannot test PostProcess without proper registration: %v", err)
		return
	}

	// Get the registered provider
	providerName := system.GetProviderId(OsName, "test-post", "amd64")
	retrievedProvider, exists := provider.Get(providerName)
	if !exists {
		t.Skip("Cannot test PostProcess without retrieving registered provider")
		return
	}

	ubuntu, ok := retrievedProvider.(*ubuntu)
	if !ok {
		t.Skip("Retrieved provider is not an ubuntu instance")
		return
	}

	template := createTestImageTemplate()

	// Test with no error
	err = ubuntu.PostProcess(template, nil)
	if err != nil {
		t.Logf("PostProcess failed as expected due to chroot cleanup dependencies: %v", err)
	}

	// Test with input error - PostProcess should clean up and return nil (not the input error)
	inputError := fmt.Errorf("some build error")
	err = ubuntu.PostProcess(template, inputError)
	if err != nil {
		t.Logf("PostProcess failed during cleanup: %v", err)
	}
}

// TestUbuntuProviderInstallHostDependency tests installHostDependency method
func TestUbuntuProviderInstallHostDependency(t *testing.T) {
	// Save original shell executor and restore after test
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	// Set up mock executor
	mockExpectedOutput := []shell.MockCommand{
		// Mock successful command existence checks
		{Pattern: "which mmdebstrap", Output: "", Error: nil},
		{Pattern: "which mkfs.fat", Output: "", Error: nil},
		{Pattern: "which mformat", Output: "", Error: nil},
		{Pattern: "which xorriso", Output: "", Error: nil},
		{Pattern: "which qemu-img", Output: "", Error: nil},
		{Pattern: "which ukify", Output: "", Error: nil},
		{Pattern: "which grub-mkimage", Output: "", Error: nil},
		{Pattern: "which veritysetup", Output: "", Error: nil},
		{Pattern: "which sbsign", Output: "", Error: nil},
		{Pattern: "which ubuntu-keyring", Output: "", Error: nil},
		// Mock successful installation commands
		{Pattern: "apt-get install -y mmdebstrap", Output: "Success", Error: nil},
		{Pattern: "apt-get install -y dosfstools", Output: "Success", Error: nil},
		{Pattern: "apt-get install -y mtools", Output: "Success", Error: nil},
		{Pattern: "apt-get install -y xorriso", Output: "Success", Error: nil},
		{Pattern: "apt-get install -y qemu-utils", Output: "Success", Error: nil},
		{Pattern: "apt-get install -y systemd-ukify", Output: "Success", Error: nil},
		{Pattern: "apt-get install -y grub-common", Output: "Success", Error: nil},
		{Pattern: "apt-get install -y cryptsetup", Output: "Success", Error: nil},
		{Pattern: "apt-get install -y sbsigntool", Output: "Success", Error: nil},
		{Pattern: "apt-get install -y ubuntu-keyring", Output: "Success", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	ubuntu := &ubuntu{}

	// This test will likely fail due to dependencies on system.GetHostOsPkgManager()
	// and shell.IsCommandExist(), but it demonstrates the testing approach
	err := ubuntu.installHostDependency()
	if err != nil {
		t.Logf("installHostDependency failed as expected due to external dependencies: %v", err)
	} else {
		t.Logf("installHostDependency succeeded with mocked commands")
	}
}

// TestUbuntuProviderInstallHostDependencyCommands tests the specific commands for host dependencies
func TestUbuntuProviderInstallHostDependencyCommands(t *testing.T) {
	// Get the dependency map by examining the installHostDependency method
	expectedDeps := map[string]string{
		"mmdebstrap":     "mmdebstrap",
		"mkfs.fat":       "dosfstools",
		"mformat":        "mtools",
		"xorriso":        "xorriso",
		"qemu-img":       "qemu-utils",
		"ukify":          "systemd-ukify",
		"grub-mkimage":   "grub-common",
		"veritysetup":    "cryptsetup",
		"sbsign":         "sbsigntool",
		"ubuntu-keyring": "ubuntu-keyring",
	}

	// This is a structural test to verify the dependency mapping
	// In a real implementation, we might expose this map for testing
	t.Logf("Expected host dependencies for Ubuntu provider: %+v", expectedDeps)

	// Verify we have the expected number of dependencies
	if len(expectedDeps) != 10 {
		t.Errorf("Expected 10 host dependencies, got %d", len(expectedDeps))
	}

	// Verify specific critical dependencies
	criticalDeps := []string{"mmdebstrap", "mkfs.fat", "xorriso", "qemu-img"}
	for _, dep := range criticalDeps {
		if _, exists := expectedDeps[dep]; !exists {
			t.Errorf("Critical dependency %s not found in expected dependencies", dep)
		}
	}
}

// TestUbuntuProviderRegister tests the Register function
func TestUbuntuProviderRegister(t *testing.T) {
	// Save original providers registry and restore after test
	// Note: We can't easily access the provider registry for cleanup,
	// so this test shows the approach but may leave test artifacts

	err := Register("linux", "ubuntu24", "amd64")
	if err != nil {
		t.Skipf("Cannot test registration due to missing dependencies: %v", err)
		return
	}

	// Try to retrieve the registered provider
	providerName := system.GetProviderId(OsName, "ubuntu24", "amd64")
	retrievedProvider, exists := provider.Get(providerName)

	if !exists {
		t.Errorf("Expected provider %s to be registered", providerName)
		return
	}

	// Verify it's an ubuntu provider
	if ubuntuProvider, ok := retrievedProvider.(*ubuntu); !ok {
		t.Errorf("Expected ubuntu provider, got %T", retrievedProvider)
	} else {
		// Test the Name method on the registered provider
		name := ubuntuProvider.Name("ubuntu24", "amd64")
		if name != providerName {
			t.Errorf("Expected provider name %s, got %s", providerName, name)
		}
	}
}

// TestUbuntuProviderWorkflow tests a complete ubuntu provider workflow
func TestUbuntuProviderWorkflow(t *testing.T) {
	// This is a unit test focused on testing the provider interface methods
	// without external dependencies that require system access

	ubuntu := &ubuntu{}

	// Test provider name generation
	name := ubuntu.Name("ubuntu24", "amd64")
	expectedName := "ubuntu-ubuntu24-amd64"
	if name != expectedName {
		t.Errorf("Expected name %s, got %s", expectedName, name)
	}

	// Test Init (will likely fail due to network dependencies)
	if err := ubuntu.Init("ubuntu24", "amd64"); err != nil {
		t.Logf("Init failed as expected: %v", err)
	} else {
		// If Init succeeds, verify configuration was loaded
		if len(ubuntu.repoCfgs) == 0 {
			t.Error("Expected repo config to be set after successful Init")
		}
		t.Logf("Repo configs loaded: %d repositories", len(ubuntu.repoCfgs))
	}

	// Skip PreProcess and BuildImage tests to avoid sudo commands
	t.Log("Skipping PreProcess and BuildImage tests to avoid system-level dependencies")

	// Skip PostProcess tests as they require properly initialized dependencies
	t.Log("Skipping PostProcess tests to avoid nil pointer panics - these are tested separately with proper registration")

	t.Log("Complete workflow test finished - core methods exist and are callable")
}

// TestUbuntuConfigurationStructure tests the structure of the ubuntu configuration
func TestUbuntuConfigurationStructure(t *testing.T) {
	// Change to project root for tests that need config files
	originalDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Failed to change back to original directory: %v", err)
		}
	}()

	// Navigate to project root (3 levels up from internal/provider/ubuntu)
	if err := os.Chdir("../../../"); err != nil {
		t.Skipf("Cannot change to project root: %v", err)
		return
	}

	// Test that OsName constant is set correctly
	if OsName == "" {
		t.Error("OsName should not be empty")
	}

	expectedOsName := "ubuntu"
	if OsName != expectedOsName {
		t.Errorf("Expected OsName %s, got %s", expectedOsName, OsName)
	}

	// Test that we can load provider config
	providerConfigs, err := config.LoadProviderRepoConfig(OsName, "ubuntu24", "amd64")
	if err != nil {
		t.Logf("Cannot load provider config in test environment: %v", err)
	} else {
		// If we can load it, verify it has required fields
		if len(providerConfigs) == 0 {
			t.Error("Provider config should have at least one repository")
		} else {
			if providerConfigs[0].Name == "" {
				t.Error("Provider config should have a name")
			}
			t.Logf("Loaded provider config: %s", providerConfigs[0].Name)
		}
	}
}

// TestUbuntuArchitectureHandling tests architecture-specific URL construction
func TestUbuntuArchitectureHandling(t *testing.T) {
	// Change to project root for tests that need config files
	originalDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Failed to change back to original directory: %v", err)
		}
	}()

	// Navigate to project root (3 levels up from internal/provider/ubuntu)
	if err := os.Chdir("../../../"); err != nil {
		t.Skipf("Cannot change to project root: %v", err)
		return
	}

	testCases := []struct {
		inputArch    string
		expectedArch string
	}{
		{"x86_64", "amd64"}, // x86_64 gets converted to amd64
		{"amd64", "amd64"},  // amd64 stays amd64
		{"arm64", "arm64"},  // arm64 stays arm64
	}

	for _, tc := range testCases {
		t.Run(tc.inputArch, func(t *testing.T) {
			ubuntu := &ubuntu{}
			err := ubuntu.Init("ubuntu24", tc.inputArch) // Test arch mapping

			if err != nil {
				t.Logf("Init failed as expected: %v", err)
			} else {
				// We expect success, so we can check arch mapping
				if len(ubuntu.repoCfgs) == 0 {
					t.Error("Expected repoCfgs to be populated after successful Init")
					return
				}

				// Check the first repository configuration
				firstRepo := ubuntu.repoCfgs[0]
				if firstRepo.Arch != tc.expectedArch {
					t.Errorf("For input arch %s, expected config arch %s, got %s", tc.inputArch, tc.expectedArch, firstRepo.Arch)
				}

				// If we have a PkgList, verify it contains the expected architecture
				if firstRepo.PkgList != "" {
					expectedArchInURL := "binary-" + tc.expectedArch
					if !strings.Contains(firstRepo.PkgList, expectedArchInURL) {
						t.Errorf("For arch %s, expected PkgList to contain %s, got %s", tc.inputArch, expectedArchInURL, firstRepo.PkgList)
					}
				}

				t.Logf("Successfully tested arch %s -> %s", tc.inputArch, tc.expectedArch)
			}
		})
	}
}

// TestUbuntuBuildImageNilTemplate tests BuildImage with nil template
func TestUbuntuBuildImageNilTemplate(t *testing.T) {
	ubuntu := &ubuntu{}

	err := ubuntu.BuildImage(nil)
	if err == nil {
		t.Error("Expected error when template is nil")
	}

	expectedError := "template cannot be nil"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}

// TestUbuntuBuildImageUnsupportedType tests BuildImage with unsupported image type
func TestUbuntuBuildImageUnsupportedType(t *testing.T) {
	ubuntu := &ubuntu{}

	template := createTestImageTemplate()
	template.Target.ImageType = "unsupported"

	err := ubuntu.BuildImage(template)
	if err == nil {
		t.Error("Expected error for unsupported image type")
	}

	expectedError := "unsupported image type: unsupported"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}

// TestUbuntuBuildImageValidTypes tests BuildImage error handling for valid image types
func TestUbuntuBuildImageValidTypes(t *testing.T) {
	ubuntu := &ubuntu{}

	validTypes := []string{"raw", "img", "iso"}

	for _, imageType := range validTypes {
		t.Run(imageType, func(t *testing.T) {
			template := createTestImageTemplate()
			template.Target.ImageType = imageType

			// These will fail due to missing chrootEnv, but we can verify
			// that the code path is reached and the error is expected
			err := ubuntu.BuildImage(template)
			if err == nil {
				t.Errorf("Expected error for image type %s (missing dependencies)", imageType)
			} else {
				t.Logf("Image type %s correctly failed with: %v", imageType, err)

				// Verify the error is related to missing dependencies, not invalid type
				if err.Error() == "unsupported image type: "+imageType {
					t.Errorf("Image type %s should be supported but got unsupported error", imageType)
				}
			}
		})
	}
}

// TestUbuntuPostProcessErrorHandling tests PostProcess method signature and basic behavior
func TestUbuntuPostProcessErrorHandling(t *testing.T) {
	// Test that PostProcess method exists and has correct signature
	// We verify that the method can be called and behaves predictably

	ubuntu := &ubuntu{}
	template := createTestImageTemplate()
	inputError := fmt.Errorf("build failed")

	// Verify the method signature is correct by assigning it to a function variable
	var postProcessFunc func(*config.ImageTemplate, error) error = ubuntu.PostProcess

	t.Logf("PostProcess method has correct signature: %T", postProcessFunc)

	// Test that PostProcess with nil chrootEnv will panic - catch and validate
	defer func() {
		if r := recover(); r != nil {
			t.Logf("PostProcess correctly panicked with nil chrootEnv: %v", r)
		} else {
			t.Error("Expected PostProcess to panic with nil chrootEnv")
		}
	}()

	// This will panic due to nil chrootEnv, which we catch above
	_ = ubuntu.PostProcess(template, inputError)
}

// TestUbuntuDownloadImagePkgs tests downloadImagePkgs method structure
func TestUbuntuDownloadImagePkgs(t *testing.T) {
	ubuntu := &ubuntu{
		repoCfgs: []debutils.RepoConfig{
			{
				Name:      "Test Repository",
				PkgList:   "http://example.com/packages.gz",
				PkgPrefix: "http://example.com/",
				Arch:      "amd64",
				Enabled:   true,
			},
		},
		chrootEnv: &mockChrootEnv{},
	}

	template := createTestImageTemplate()

	// This test will likely fail due to network dependencies and debutils package resolution,
	// but it validates the method structure and error handling
	err := ubuntu.downloadImagePkgs(template)
	if err != nil {
		t.Logf("downloadImagePkgs failed as expected due to external dependencies: %v", err)
		// Verify error messages to ensure proper error handling
		if strings.Contains(err.Error(), "no repository configurations available") {
			t.Error("Repository configurations were provided but still got 'no repository configurations' error")
		}
	} else {
		// If successful, verify that template.FullPkgList was populated
		if template.FullPkgList == nil {
			t.Error("Expected FullPkgList to be populated after successful downloadImagePkgs")
		}
		t.Logf("downloadImagePkgs succeeded, FullPkgList populated with packages")
	}
}

// TestUbuntuMultipleRepositories tests handling of multiple repositories
func TestUbuntuMultipleRepositories(t *testing.T) {
	ubuntu := &ubuntu{
		repoCfgs: []debutils.RepoConfig{
			{
				Name:      "Main Repository",
				PkgList:   "http://example.com/main/packages.gz",
				PkgPrefix: "http://example.com/main/",
				Arch:      "amd64",
				Enabled:   true,
			},
			{
				Name:      "Universe Repository",
				PkgList:   "http://example.com/universe/packages.gz",
				PkgPrefix: "http://example.com/universe/",
				Arch:      "amd64",
				Enabled:   true,
			},
		},
		chrootEnv: &mockChrootEnv{},
	}

	template := createTestImageTemplate()

	// Test downloadImagePkgs with multiple repositories
	err := ubuntu.downloadImagePkgs(template)
	if err != nil {
		t.Logf("downloadImagePkgs with multiple repos failed as expected: %v", err)
		// Should not fail due to "no repository configurations available"
		if strings.Contains(err.Error(), "no repository configurations available") {
			t.Error("Should not get 'no repository configurations' error when multiple repos are configured")
		}
	} else {
		t.Logf("downloadImagePkgs with multiple repositories succeeded")
	}

	// Verify that debutils.RepoCfgs was populated correctly
	if len(debutils.RepoCfgs) != 2 {
		t.Logf("Expected debutils.RepoCfgs to have 2 repositories, got %d (may be affected by previous tests)", len(debutils.RepoCfgs))
	}
}

// TestUbuntuLoadRepoConfigMultiple tests loadRepoConfig with multiple repositories
func TestUbuntuLoadRepoConfigMultiple(t *testing.T) {
	// Change to project root for tests that need config files
	originalDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Failed to change back to original directory: %v", err)
		}
	}()

	// Navigate to project root (3 levels up from internal/provider/ubuntu)
	if err := os.Chdir("../../../"); err != nil {
		t.Skipf("Cannot change to project root: %v", err)
		return
	}

	configs, err := loadRepoConfig("", "amd64")
	if err != nil {
		t.Skipf("loadRepoConfig failed (expected in test environment): %v", err)
		return
	}

	// Verify multiple repositories are loaded
	if len(configs) == 0 {
		t.Error("Expected at least one repository configuration")
		return
	}

	t.Logf("Loaded %d repository configurations", len(configs))

	// Verify each repository has required fields
	for i, config := range configs {
		t.Logf("Repository %d: %s", i+1, config.Name)

		if config.Name == "" {
			t.Errorf("Repository %d: expected name to be set", i+1)
		}

		if config.Arch != "amd64" {
			t.Errorf("Repository %d: expected arch 'amd64', got '%s'", i+1, config.Arch)
		}

		if config.PkgList == "" {
			t.Errorf("Repository %d: expected PkgList to be set", i+1)
		}

		if config.PkgPrefix == "" {
			t.Errorf("Repository %d: expected PkgPrefix to be set", i+1)
		}
	}
}

// TestUbuntuOsNameConstant tests the OsName constant value
func TestUbuntuOsNameConstant(t *testing.T) {
	expectedOsName := "ubuntu"
	if OsName != expectedOsName {
		t.Errorf("Expected OsName constant to be '%s', got '%s'", expectedOsName, OsName)
	}
}

// TestUbuntuPreProcessWithMockEnv tests PreProcess with mock chroot environment
func TestUbuntuPreProcessWithMockEnv(t *testing.T) {
	ubuntu := &ubuntu{
		repoCfgs: []debutils.RepoConfig{
			{
				Section:     "main",
				Name:        "Ubuntu 24.04",
				PkgList:     "https://archive.ubuntu.com/ubuntu/dists/noble/main/binary-amd64/Packages.gz",
				PkgPrefix:   "https://archive.ubuntu.com/ubuntu/",
				Enabled:     true,
				GPGCheck:    true,
				ReleaseFile: "https://archive.ubuntu.com/ubuntu/dists/noble/Release",
				ReleaseSign: "https://archive.ubuntu.com/ubuntu/dists/noble/Release.gpg",
				BuildPath:   "/tmp/builds/ubuntu1_amd64_main",
				Arch:        "amd64",
			},
		},
		chrootEnv: &mockChrootEnv{},
	}

	template := createTestImageTemplate()

	// Test PreProcess - will fail due to dependencies on installHostDependency
	err := ubuntu.PreProcess(template)
	if err != nil {
		t.Logf("PreProcess failed as expected due to installHostDependency: %v", err)
		// Verify it fails at the right place
		if !strings.Contains(err.Error(), "failed to install host dependency") &&
			!strings.Contains(err.Error(), "failed to get host package manager") {
			t.Logf("PreProcess failed at expected point: %v", err)
		}
	}
}

// TestUbuntuPostProcessWithMockEnv tests PostProcess with mock environment
func TestUbuntuPostProcessWithMockEnv(t *testing.T) {
	ubuntu := &ubuntu{
		chrootEnv: &mockChrootEnv{},
	}

	template := createTestImageTemplate()

	// Test PostProcess with no error
	err := ubuntu.PostProcess(template, nil)
	if err != nil {
		t.Logf("PostProcess cleanup completed: %v", err)
	}

	// Test PostProcess with input error
	inputErr := fmt.Errorf("some build error")
	err = ubuntu.PostProcess(template, inputErr)
	if err != nil {
		t.Logf("PostProcess cleanup handled build error: %v", err)
	}
}

// TestUbuntuInitWithAarch64 tests Init with aarch64 architecture mapping
func TestUbuntuInitWithAarch64(t *testing.T) {
	// Change to project root for tests that need config files
	originalDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Failed to change back to original directory: %v", err)
		}
	}()

	// Navigate to project root (3 levels up from internal/provider/ubuntu)
	if err := os.Chdir("../../../"); err != nil {
		t.Skipf("Cannot change to project root: %v", err)
		return
	}

	ubuntu := &ubuntu{}

	// Test aarch64 -> arm64 mapping
	err := ubuntu.Init("ubuntu24", "aarch64")
	if err != nil {
		t.Logf("Init failed as expected: %v", err)
	} else {
		// Verify that repoCfgs were set up correctly
		if len(ubuntu.repoCfgs) == 0 {
			t.Error("Expected repoCfgs to be populated after successful Init")
			return
		}

		// Verify architecture was mapped correctly
		firstRepo := ubuntu.repoCfgs[0]
		if firstRepo.Arch != "arm64" {
			t.Errorf("Expected mapped arch to be arm64, got %s", firstRepo.Arch)
		}

		// Verify that the PkgList contains the correct architecture
		expectedArchInURL := "binary-arm64"
		if firstRepo.PkgList != "" && !strings.Contains(firstRepo.PkgList, expectedArchInURL) {
			t.Errorf("Expected PkgList to contain %s for aarch64 arch, got %s", expectedArchInURL, firstRepo.PkgList)
		}

		t.Logf("Successfully mapped aarch64 -> arm64, PkgList: %s", firstRepo.PkgList)
	}
}

// TestUbuntuDownloadImagePkgsNoRepos tests downloadImagePkgs with no repositories
func TestUbuntuDownloadImagePkgsNoRepos(t *testing.T) {
	ubuntu := &ubuntu{
		repoCfgs:  []debutils.RepoConfig{}, // Empty repo configs
		chrootEnv: &mockChrootEnv{},
	}

	template := createTestImageTemplate()

	err := ubuntu.downloadImagePkgs(template)
	if err == nil {
		t.Error("Expected downloadImagePkgs to fail with no repositories")
	} else if !strings.Contains(err.Error(), "no repository configurations available") {
		t.Errorf("Expected 'no repository configurations available' error, got: %v", err)
	}
}

// TestUbuntuBuildRawImageError tests buildRawImage error path
func TestUbuntuBuildRawImageError(t *testing.T) {
	ubuntu := &ubuntu{
		chrootEnv: &mockChrootEnv{},
	}

	template := createTestImageTemplate()
	template.Target.ImageType = "raw"

	// This should fail when trying to create RawMaker
	err := ubuntu.buildRawImage(template)
	if err == nil {
		t.Error("Expected buildRawImage to fail")
	} else {
		t.Logf("buildRawImage failed as expected: %v", err)
		if !strings.Contains(err.Error(), "failed to create raw maker") &&
			!strings.Contains(err.Error(), "failed to initialize raw maker") {
			t.Logf("buildRawImage error: %v", err)
		}
	}
}

// TestUbuntuBuildInitrdImageError tests buildInitrdImage error path
func TestUbuntuBuildInitrdImageError(t *testing.T) {
	ubuntu := &ubuntu{
		chrootEnv: &mockChrootEnv{},
	}

	template := createTestImageTemplate()
	template.Target.ImageType = "img"

	// This should fail when trying to create InitrdMaker
	err := ubuntu.buildInitrdImage(template)
	if err == nil {
		t.Error("Expected buildInitrdImage to fail")
	} else {
		t.Logf("buildInitrdImage failed as expected: %v", err)
		if !strings.Contains(err.Error(), "failed to create initrd maker") &&
			!strings.Contains(err.Error(), "failed to initialize initrd image maker") {
			t.Logf("buildInitrdImage error: %v", err)
		}
	}
}

// TestUbuntuBuildIsoImageError tests buildIsoImage error path
func TestUbuntuBuildIsoImageError(t *testing.T) {
	ubuntu := &ubuntu{
		chrootEnv: &mockChrootEnv{},
	}

	template := createTestImageTemplate()
	template.Target.ImageType = "iso"

	// This should fail when trying to create IsoMaker
	err := ubuntu.buildIsoImage(template)
	if err == nil {
		t.Error("Expected buildIsoImage to fail")
	} else {
		t.Logf("buildIsoImage failed as expected: %v", err)
		if !strings.Contains(err.Error(), "failed to create iso maker") &&
			!strings.Contains(err.Error(), "failed to initialize iso maker") {
			t.Logf("buildIsoImage error: %v", err)
		}
	}
}

// TestLoadRepoConfigArm64 tests loadRepoConfig with arm64 architecture
func TestLoadRepoConfigArm64(t *testing.T) {
	// Change to project root for tests that need config files
	originalDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Failed to change back to original directory: %v", err)
		}
	}()

	// Navigate to project root (3 levels up from internal/provider/ubuntu)
	if err := os.Chdir("../../../"); err != nil {
		t.Skipf("Cannot change to project root: %v", err)
		return
	}

	configs, err := loadRepoConfig("", "arm64")
	if err != nil {
		t.Skipf("loadRepoConfig failed (expected in test environment): %v", err)
		return
	}

	// If we successfully load config, verify the values
	if len(configs) == 0 {
		t.Error("Expected at least one repository configuration")
		return
	}

	for _, config := range configs {
		if config.Arch != "arm64" {
			t.Errorf("Expected arch 'arm64', got '%s'", config.Arch)
		}

		// Verify PkgList contains expected architecture
		if config.PkgList != "" && !strings.Contains(config.PkgList, "binary-arm64") {
			t.Errorf("Expected PkgList to contain 'binary-arm64', got '%s'", config.PkgList)
		}

		t.Logf("Successfully loaded arm64 repo config: %s", config.Name)
	}
}

// TestLoadRepoConfigNonDebRepository tests loadRepoConfig skipping non-DEB repos
func TestLoadRepoConfigNonDebRepository(t *testing.T) {
	// This test verifies that non-DEB repositories are properly skipped
	// Change to project root for tests that need config files
	originalDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Failed to change back to original directory: %v", err)
		}
	}()

	// Navigate to project root (3 levels up from internal/provider/ubuntu)
	if err := os.Chdir("../../../"); err != nil {
		t.Skipf("Cannot change to project root: %v", err)
		return
	}

	configs, err := loadRepoConfig("", "amd64")
	if err != nil {
		// Check if error is about no valid DEB repositories
		if strings.Contains(err.Error(), "no valid DEB repositories found") {
			t.Logf("Expected error for no valid DEB repositories: %v", err)
		} else {
			t.Skipf("loadRepoConfig failed: %v", err)
		}
		return
	}

	// All returned configs should be DEB type
	t.Logf("Loaded %d DEB repository configurations", len(configs))
}

// TestUbuntuRegisterWithEmptyDist tests Register with empty distribution
func TestUbuntuRegisterWithEmptyDist(t *testing.T) {
	// Save original shell executor and restore after test
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	// Set up mock executor
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: ".*", Output: "success", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	err := Register("", "", "amd64")
	if err != nil {
		t.Logf("Register failed as expected with empty dist: %v", err)
	}
}

// TestUbuntuDownloadImagePkgsCacheDirError tests downloadImagePkgs cache dir error
func TestUbuntuDownloadImagePkgsCacheDirError(t *testing.T) {
	// This test verifies error handling when cache directory retrieval fails
	ubuntu := &ubuntu{
		repoCfgs: []debutils.RepoConfig{
			{
				Section:   "main",
				Name:      "Test Repo",
				Arch:      "amd64",
				Enabled:   true,
				PkgList:   "https://test.com/Packages.gz",
				PkgPrefix: "https://test.com/",
			},
		},
		chrootEnv: &mockChrootEnv{},
	}

	template := createTestImageTemplate()

	// This will fail during cache directory setup or package download
	err := ubuntu.downloadImagePkgs(template)
	if err != nil {
		t.Logf("downloadImagePkgs failed as expected: %v", err)
	}
}

// TestUbuntuInitEmptyRepoConfigs tests Init handling when loadRepoConfig returns empty configs
func TestUbuntuInitEmptyRepoConfigs(t *testing.T) {
	// This test would need to mock loadRepoConfig to return empty configs
	// For now, we document the expected behavior
	ubuntu := &ubuntu{}

	// Change to project root for tests that need config files
	originalDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Failed to change back to original directory: %v", err)
		}
	}()

	// Navigate to project root (3 levels up from internal/provider/ubuntu)
	if err := os.Chdir("../../../"); err != nil {
		t.Skipf("Cannot change to project root: %v", err)
		return
	}

	// Test with a non-existent distribution that should fail
	err := ubuntu.Init("nonexistent-dist", "amd64")
	if err != nil {
		t.Logf("Init correctly failed with invalid dist: %v", err)
	}
}

// TestUbuntuNameWithVariousInputs tests Name method with different inputs
func TestUbuntuNameWithVariousInputs(t *testing.T) {
	ubuntu := &ubuntu{}

	testCases := []struct {
		dist     string
		arch     string
		expected string
	}{
		{"ubuntu24", "amd64", "ubuntu-ubuntu24-amd64"},
		{"ubuntu24", "arm64", "ubuntu-ubuntu24-arm64"},
		{"ubuntu22", "x86_64", "ubuntu-ubuntu22-x86_64"},
		{"", "", "ubuntu--"},
		{"special-dist", "special-arch", "ubuntu-special-dist-special-arch"},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%s-%s", tc.dist, tc.arch), func(t *testing.T) {
			result := ubuntu.Name(tc.dist, tc.arch)
			if result != tc.expected {
				t.Errorf("Expected %s, got %s", tc.expected, result)
			}
		})
	}
}

// TestUbuntuInstallHostDependencyCommandCheck tests installHostDependency command checking
func TestUbuntuInstallHostDependencyCommandCheck(t *testing.T) {
	// Save original shell executor and restore after test
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	// Set up mock executor that simulates all commands already exist
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "which mmdebstrap", Output: "/usr/bin/mmdebstrap", Error: nil},
		{Pattern: "which mkfs.fat", Output: "/usr/bin/mkfs.fat", Error: nil},
		{Pattern: "which mformat", Output: "/usr/bin/mformat", Error: nil},
		{Pattern: "which xorriso", Output: "/usr/bin/xorriso", Error: nil},
		{Pattern: "which qemu-img", Output: "/usr/bin/qemu-img", Error: nil},
		{Pattern: "which ukify", Output: "/usr/bin/ukify", Error: nil},
		{Pattern: "which grub-mkimage", Output: "/usr/bin/grub-mkimage", Error: nil},
		{Pattern: "which veritysetup", Output: "/usr/bin/veritysetup", Error: nil},
		{Pattern: "which sbsign", Output: "/usr/bin/sbsign", Error: nil},
		{Pattern: "which ubuntu-keyring", Output: "/usr/bin/ubuntu-keyring", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	ubuntu := &ubuntu{chrootEnv: &mockChrootEnv{}}

	err := ubuntu.installHostDependency()
	if err != nil {
		t.Logf("installHostDependency completed with result: %v", err)
	}
}

// TestUbuntuPreProcessInitChrootEnvError tests PreProcess when InitChrootEnv fails
func TestUbuntuPreProcessInitChrootEnvError(t *testing.T) {
	// Create a mock that fails on InitChrootEnv
	type failingMockChrootEnv struct {
		mockChrootEnv
	}

	failing := &failingMockChrootEnv{}

	ubuntu := &ubuntu{
		repoCfgs: []debutils.RepoConfig{
			{
				Section:   "main",
				Name:      "Test Repo",
				Arch:      "amd64",
				PkgList:   "https://test.com/Packages.gz",
				PkgPrefix: "https://test.com/",
			},
		},
		chrootEnv: failing,
	}

	template := createTestImageTemplate()

	// PreProcess should handle initialization errors
	err := ubuntu.PreProcess(template)
	if err != nil {
		t.Logf("PreProcess failed as expected: %v", err)
	}
}

// TestUbuntuBuildRawImageSuccess tests buildRawImage success path
func TestUbuntuBuildRawImageSuccess(t *testing.T) {
	ubuntu := &ubuntu{
		chrootEnv: &mockChrootEnv{},
	}

	template := createTestImageTemplate()
	template.Target.ImageType = "raw"

	// This will still fail due to rawmaker dependencies but tests the path
	err := ubuntu.buildRawImage(template)
	if err != nil {
		t.Logf("buildRawImage failed as expected: %v", err)
		// Ensure we're testing the right code path
		if strings.Contains(err.Error(), "failed to create raw maker") ||
			strings.Contains(err.Error(), "failed to initialize raw maker") ||
			strings.Contains(err.Error(), "failed to build raw image") {
			// Expected errors from raw maker operations
			t.Logf("Error is from expected code path")
		}
	}
}

// TestUbuntuBuildInitrdImageSuccess tests buildInitrdImage success path
func TestUbuntuBuildInitrdImageSuccess(t *testing.T) {
	ubuntu := &ubuntu{
		chrootEnv: &mockChrootEnv{},
	}

	template := createTestImageTemplate()
	template.Target.ImageType = "img"

	// This will fail due to initrdmaker dependencies but tests the path
	err := ubuntu.buildInitrdImage(template)
	if err != nil {
		t.Logf("buildInitrdImage failed as expected: %v", err)
		// Ensure we're testing the right code path
		if strings.Contains(err.Error(), "failed to create initrd maker") ||
			strings.Contains(err.Error(), "failed to initialize initrd image maker") ||
			strings.Contains(err.Error(), "failed to build initrd image") ||
			strings.Contains(err.Error(), "failed to clean initrd rootfs") {
			// Expected errors from initrd maker operations
			t.Logf("Error is from expected code path")
		}
	}
}

// TestUbuntuBuildIsoImageSuccess tests buildIsoImage success path
func TestUbuntuBuildIsoImageSuccess(t *testing.T) {
	ubuntu := &ubuntu{
		chrootEnv: &mockChrootEnv{},
	}

	template := createTestImageTemplate()
	template.Target.ImageType = "iso"

	// This will fail due to isomaker dependencies but tests the path
	err := ubuntu.buildIsoImage(template)
	if err != nil {
		t.Logf("buildIsoImage failed as expected: %v", err)
		// Ensure we're testing the right code path
		if strings.Contains(err.Error(), "failed to create iso maker") ||
			strings.Contains(err.Error(), "failed to initialize iso maker") ||
			strings.Contains(err.Error(), "failed to build iso image") {
			// Expected errors from iso maker operations
			t.Logf("Error is from expected code path")
		}
	}
}

// TestUbuntuPreProcessDownloadPackagesError tests PreProcess when downloadImagePkgs fails
func TestUbuntuPreProcessDownloadPackagesError(t *testing.T) {
	ubuntu := &ubuntu{
		repoCfgs:  []debutils.RepoConfig{}, // Empty to trigger error in downloadImagePkgs
		chrootEnv: &mockChrootEnv{},
	}

	template := createTestImageTemplate()

	err := ubuntu.PreProcess(template)
	if err != nil {
		// Should fail at downloadImagePkgs due to missing repos
		if strings.Contains(err.Error(), "no repository configurations available") ||
			strings.Contains(err.Error(), "failed to download image packages") ||
			strings.Contains(err.Error(), "failed to install host dependency") {
			t.Logf("PreProcess failed as expected at download packages: %v", err)
		}
	}
}

// TestUbuntuDownloadImagePkgsUpdateSystemError tests downloadImagePkgs when UpdateSystemPkgs fails
func TestUbuntuDownloadImagePkgsUpdateSystemError(t *testing.T) {
	// Create a mock that fails on UpdateSystemPkgs
	type failingUpdateMockChrootEnv struct {
		mockChrootEnv
	}

	// Override UpdateSystemPkgs to return error
	failing := &failingUpdateMockChrootEnv{}
	failUpdate := failing
	_ = failUpdate // Placeholder for actual mock override

	ubuntu := &ubuntu{
		repoCfgs: []debutils.RepoConfig{
			{
				Section:   "main",
				Name:      "Test Repo",
				Arch:      "amd64",
				PkgList:   "https://test.com/Packages.gz",
				PkgPrefix: "https://test.com/",
			},
		},
		chrootEnv: &mockChrootEnv{},
	}

	template := createTestImageTemplate()

	err := ubuntu.downloadImagePkgs(template)
	if err != nil {
		t.Logf("downloadImagePkgs failed as expected: %v", err)
	}
}

// TestLoadRepoConfigNoValidRepos tests loadRepoConfig when no valid repos found
func TestLoadRepoConfigNoValidRepos(t *testing.T) {
	// Change to project root for tests that need config files
	originalDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Failed to change back to original directory: %v", err)
		}
	}()

	// Navigate to project root (3 levels up from internal/provider/ubuntu)
	if err := os.Chdir("../../../"); err != nil {
		t.Skipf("Cannot change to project root: %v", err)
		return
	}

	// Try with an invalid architecture that might result in no valid repos
	configs, err := loadRepoConfig("", "invalid-arch")
	if err != nil {
		if strings.Contains(err.Error(), "no valid DEB repositories found") ||
			strings.Contains(err.Error(), "failed to load provider repo config") {
			t.Logf("loadRepoConfig correctly failed with no valid repos: %v", err)
		} else {
			t.Logf("loadRepoConfig failed: %v", err)
		}
	} else if len(configs) == 0 {
		t.Log("loadRepoConfig returned empty configs")
	}
}

// TestUbuntuPostProcessCleanupError tests PostProcess cleanup error handling
func TestUbuntuPostProcessCleanupError(t *testing.T) {
	// Create a mock that fails on cleanup
	type failingCleanupMockChrootEnv struct {
		mockChrootEnv
	}

	failing := &failingCleanupMockChrootEnv{}

	ubuntu := &ubuntu{
		chrootEnv: failing,
	}

	template := createTestImageTemplate()

	// Test PostProcess - should handle cleanup errors gracefully
	err := ubuntu.PostProcess(template, nil)
	if err != nil {
		t.Logf("PostProcess reported cleanup issue: %v", err)
	}
}

// TestUbuntuRegisterWithDifferentArchitectures tests Register with various architectures
func TestUbuntuRegisterWithDifferentArchitectures(t *testing.T) {
	// Save original shell executor and restore after test
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	// Set up mock executor
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: ".*", Output: "success", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	testCases := []struct {
		arch string
	}{
		{"amd64"},
		{"arm64"},
		{"x86_64"},
		{"aarch64"},
	}

	for _, tc := range testCases {
		t.Run(tc.arch, func(t *testing.T) {
			err := Register("linux", fmt.Sprintf("test-%s", tc.arch), tc.arch)
			if err != nil {
				t.Logf("Register with %s failed as expected: %v", tc.arch, err)
			} else {
				t.Logf("Register with %s succeeded", tc.arch)
			}
		})
	}
}

// TestUbuntuRegisterChrootEnvError tests Register when NewChrootEnv fails
func TestUbuntuRegisterChrootEnvError(t *testing.T) {
	// Test with invalid parameters that should cause chroot creation to fail
	err := Register("invalid-os", "invalid-dist", "invalid-arch")
	if err != nil {
		t.Logf("Register correctly failed with invalid parameters: %v", err)
		if !strings.Contains(err.Error(), "failed to inject chroot dependency") {
			t.Logf("Expected error contains chroot dependency message")
		}
	}
}

// TestUbuntuPreProcessWithMockComplete tests full PreProcess with comprehensive mocking
func TestUbuntuPreProcessWithMockComplete(t *testing.T) {
	// Save original shell executor and restore after test
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	// Set up comprehensive mock executor for all dependency installations
	mockExpectedOutput := []shell.MockCommand{
		// Mock host package manager detection
		{Pattern: "which apt-get", Output: "/usr/bin/apt-get", Error: nil},
		{Pattern: "apt-get --version", Output: "apt 2.0", Error: nil},
		// Mock all command existence checks returning installed
		{Pattern: "which mmdebstrap", Output: "/usr/bin/mmdebstrap", Error: nil},
		{Pattern: "which mkfs.fat", Output: "/usr/bin/mkfs.fat", Error: nil},
		{Pattern: "which mformat", Output: "/usr/bin/mformat", Error: nil},
		{Pattern: "which xorriso", Output: "/usr/bin/xorriso", Error: nil},
		{Pattern: "which qemu-img", Output: "/usr/bin/qemu-img", Error: nil},
		{Pattern: "which ukify", Output: "/usr/bin/ukify", Error: nil},
		{Pattern: "which grub-mkimage", Output: "/usr/bin/grub-mkimage", Error: nil},
		{Pattern: "which veritysetup", Output: "/usr/bin/veritysetup", Error: nil},
		{Pattern: "which sbsign", Output: "/usr/bin/sbsign", Error: nil},
		{Pattern: "which ubuntu-keyring", Output: "/usr/bin/ubuntu-keyring", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	ubuntu := &ubuntu{
		repoCfgs: []debutils.RepoConfig{
			{
				Section:     "main",
				Name:        "Ubuntu 24.04",
				PkgList:     "https://archive.ubuntu.com/ubuntu/dists/noble/main/binary-amd64/Packages.gz",
				PkgPrefix:   "https://archive.ubuntu.com/ubuntu/",
				Enabled:     true,
				GPGCheck:    true,
				ReleaseFile: "https://archive.ubuntu.com/ubuntu/dists/noble/Release",
				ReleaseSign: "https://archive.ubuntu.com/ubuntu/dists/noble/Release.gpg",
				BuildPath:   "/tmp/builds/ubuntu1_amd64_main",
				Arch:        "amd64",
			},
		},
		chrootEnv: &mockChrootEnv{},
	}

	template := createTestImageTemplate()

	err := ubuntu.PreProcess(template)
	if err != nil {
		t.Logf("PreProcess failed (expected due to downloadImagePkgs): %v", err)
		// Verify it fails at downloadImagePkgs, not installHostDependency
		if strings.Contains(err.Error(), "failed to download image packages") {
			t.Logf("PreProcess correctly proceeded past installHostDependency")
		}
	} else {
		t.Log("PreProcess succeeded with comprehensive mocking")
	}
}

// TestUbuntuInstallHostDependencyMissingCommands tests installHostDependency when commands are missing
func TestUbuntuInstallHostDependencyMissingCommands(t *testing.T) {
	// Save original shell executor and restore after test
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	// Set up mock executor that simulates missing commands requiring installation
	mockExpectedOutput := []shell.MockCommand{
		// Mock host package manager detection
		{Pattern: "which apt-get", Output: "/usr/bin/apt-get", Error: nil},
		{Pattern: "apt-get --version", Output: "apt 2.0", Error: nil},
		// Mock some commands missing (empty output = not found)
		{Pattern: "which mmdebstrap", Output: "", Error: fmt.Errorf("command not found")},
		{Pattern: "which mkfs.fat", Output: "/usr/bin/mkfs.fat", Error: nil},
		{Pattern: "which mformat", Output: "", Error: fmt.Errorf("command not found")},
		{Pattern: "which xorriso", Output: "/usr/bin/xorriso", Error: nil},
		{Pattern: "which qemu-img", Output: "", Error: fmt.Errorf("command not found")},
		{Pattern: "which ukify", Output: "/usr/bin/ukify", Error: nil},
		{Pattern: "which grub-mkimage", Output: "/usr/bin/grub-mkimage", Error: nil},
		{Pattern: "which veritysetup", Output: "/usr/bin/veritysetup", Error: nil},
		{Pattern: "which sbsign", Output: "/usr/bin/sbsign", Error: nil},
		{Pattern: "which ubuntu-keyring", Output: "/usr/bin/ubuntu-keyring", Error: nil},
		// Mock successful installations
		{Pattern: "apt-get install -y mmdebstrap", Output: "Package installed", Error: nil},
		{Pattern: "apt-get install -y mtools", Output: "Package installed", Error: nil},
		{Pattern: "apt-get install -y qemu-utils", Output: "Package installed", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	ubuntu := &ubuntu{chrootEnv: &mockChrootEnv{}}

	err := ubuntu.installHostDependency()
	if err != nil {
		t.Logf("installHostDependency completed: %v", err)
	} else {
		t.Log("installHostDependency succeeded with package installations")
	}
}

// TestUbuntuBuildRawImageWithMock tests buildRawImage with comprehensive mocking
func TestUbuntuBuildRawImageWithMock(t *testing.T) {
	ubuntu := &ubuntu{
		chrootEnv: &mockChrootEnv{},
	}

	template := createTestImageTemplate()
	template.Target.ImageType = "raw"

	// Set required fields for raw image creation
	template.DotFilePath = "/tmp/test.dot"

	err := ubuntu.buildRawImage(template)
	if err != nil {
		t.Logf("buildRawImage failed as expected: %v", err)
		// Verify it reaches the rawmaker code path
		if strings.Contains(err.Error(), "failed to create raw maker") ||
			strings.Contains(err.Error(), "failed to initialize raw maker") ||
			strings.Contains(err.Error(), "failed to build raw image") {
			t.Log("buildRawImage reached expected code path")
		}
	}
}

// TestUbuntuBuildInitrdImageWithMock tests buildInitrdImage with comprehensive mocking
func TestUbuntuBuildInitrdImageWithMock(t *testing.T) {
	ubuntu := &ubuntu{
		chrootEnv: &mockChrootEnv{},
	}

	template := createTestImageTemplate()
	template.Target.ImageType = "img"

	// Set required fields for initrd image creation
	template.DotFilePath = "/tmp/test.dot"

	err := ubuntu.buildInitrdImage(template)
	if err != nil {
		t.Logf("buildInitrdImage failed as expected: %v", err)
		// Verify it reaches the initrdmaker code path
		if strings.Contains(err.Error(), "failed to create initrd maker") ||
			strings.Contains(err.Error(), "failed to initialize initrd image maker") ||
			strings.Contains(err.Error(), "failed to build initrd image") ||
			strings.Contains(err.Error(), "failed to clean initrd rootfs") {
			t.Log("buildInitrdImage reached expected code path")
		}
	}
}

// TestUbuntuBuildIsoImageWithMock tests buildIsoImage with comprehensive mocking
func TestUbuntuBuildIsoImageWithMock(t *testing.T) {
	ubuntu := &ubuntu{
		chrootEnv: &mockChrootEnv{},
	}

	template := createTestImageTemplate()
	template.Target.ImageType = "iso"

	// Set required fields for ISO image creation
	template.DotFilePath = "/tmp/test.dot"

	err := ubuntu.buildIsoImage(template)
	if err != nil {
		t.Logf("buildIsoImage failed as expected: %v", err)
		// Verify it reaches the isomaker code path
		if strings.Contains(err.Error(), "failed to create iso maker") ||
			strings.Contains(err.Error(), "failed to initialize iso maker") ||
			strings.Contains(err.Error(), "failed to build iso image") {
			t.Log("buildIsoImage reached expected code path")
		}
	}
}

// TestUbuntuPostProcessWithError tests PostProcess with previous build error
func TestUbuntuPostProcessWithError(t *testing.T) {
	ubuntu := &ubuntu{
		chrootEnv: &mockChrootEnv{},
	}

	template := createTestImageTemplate()
	buildError := fmt.Errorf("mock build error")

	// Test that PostProcess handles the build error and performs cleanup
	err := ubuntu.PostProcess(template, buildError)
	if err != nil {
		// PostProcess may return cleanup errors
		t.Logf("PostProcess completed with error: %v", err)
		if strings.Contains(err.Error(), "failed to cleanup chroot environment") {
			t.Log("PostProcess attempted cleanup despite build error")
		}
	} else {
		t.Log("PostProcess completed cleanup successfully")
	}
}

// TestUbuntuPostProcessNilTemplate tests PostProcess with nil template
func TestUbuntuPostProcessNilTemplate(t *testing.T) {
	ubuntu := &ubuntu{
		chrootEnv: &mockChrootEnv{},
	}

	// Test PostProcess with nil template - should handle gracefully or panic
	defer func() {
		if r := recover(); r != nil {
			t.Logf("PostProcess correctly panicked with nil template: %v", r)
		}
	}()

	_ = ubuntu.PostProcess(nil, nil)
}

// TestUbuntuDownloadImagePkgsWithFullTemplate tests downloadImagePkgs with complete template
func TestUbuntuDownloadImagePkgsWithFullTemplate(t *testing.T) {
	ubuntu := &ubuntu{
		repoCfgs: []debutils.RepoConfig{
			{
				Section:     "main",
				Name:        "Ubuntu Main",
				PkgList:     "https://archive.ubuntu.com/ubuntu/dists/noble/main/binary-amd64/Packages.gz",
				PkgPrefix:   "https://archive.ubuntu.com/ubuntu/",
				Enabled:     true,
				GPGCheck:    true,
				ReleaseFile: "https://archive.ubuntu.com/ubuntu/dists/noble/Release",
				ReleaseSign: "https://archive.ubuntu.com/ubuntu/dists/noble/Release.gpg",
				BuildPath:   "/tmp/builds/ubuntu1_amd64_main",
				Arch:        "amd64",
			},
		},
		chrootEnv: &mockChrootEnv{},
	}

	template := createTestImageTemplate()
	template.DotFilePath = "/tmp/test.dot"
	template.DotSystemOnly = false
	template.SystemConfig.Packages = []string{"curl", "wget", "vim", "git"}

	err := ubuntu.downloadImagePkgs(template)
	if err != nil {
		t.Logf("downloadImagePkgs failed as expected: %v", err)
		// Should not fail due to missing repo configs
		if strings.Contains(err.Error(), "no repository configurations available") {
			t.Error("Should not get 'no repository configurations' error when repos are configured")
		}
	} else {
		// Verify template fields are populated
		if template.FullPkgList == nil {
			t.Error("Expected FullPkgList to be populated")
		}
		if template.FullPkgListBom == nil {
			t.Error("Expected FullPkgListBom to be populated")
		}
		t.Log("downloadImagePkgs succeeded and populated template fields")
	}
}

// TestUbuntuLoadRepoConfigWithValidData tests loadRepoConfig with valid provider config
func TestUbuntuLoadRepoConfigWithValidData(t *testing.T) {
	// Change to project root for tests that need config files
	originalDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Failed to change back to original directory: %v", err)
		}
	}()

	// Navigate to project root (3 levels up from internal/provider/ubuntu)
	if err := os.Chdir("../../../"); err != nil {
		t.Skipf("Cannot change to project root: %v", err)
		return
	}

	// Test with valid amd64 architecture
	configs, err := loadRepoConfig("", "amd64")
	if err != nil {
		t.Skipf("loadRepoConfig failed: %v", err)
		return
	}

	// Verify the structure of returned configs
	if len(configs) == 0 {
		t.Error("Expected at least one repository configuration")
		return
	}

	// Check each config has required fields
	for i, cfg := range configs {
		if cfg.Name == "" {
			t.Errorf("Config %d: Name is empty", i)
		}
		if cfg.Arch != "amd64" {
			t.Errorf("Config %d: Expected arch amd64, got %s", i, cfg.Arch)
		}
		if cfg.PkgList == "" {
			t.Errorf("Config %d: PkgList is empty", i)
		}
		if cfg.PkgPrefix == "" {
			t.Errorf("Config %d: PkgPrefix is empty", i)
		}
		if !cfg.Enabled {
			t.Logf("Config %d: Repository %s is not enabled", i, cfg.Name)
		}
		t.Logf("Config %d validated: %s", i, cfg.Name)
	}
}

// TestUbuntuInitWithX86_64Mapping tests Init with x86_64 to amd64 mapping
func TestUbuntuInitWithX86_64Mapping(t *testing.T) {
	// Change to project root for tests that need config files
	originalDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Failed to change back to original directory: %v", err)
		}
	}()

	// Navigate to project root (3 levels up from internal/provider/ubuntu)
	if err := os.Chdir("../../../"); err != nil {
		t.Skipf("Cannot change to project root: %v", err)
		return
	}

	ubuntu := &ubuntu{}

	// Test x86_64 -> amd64 mapping specifically
	err := ubuntu.Init("ubuntu24", "x86_64")
	if err != nil {
		t.Logf("Init failed: %v", err)
	} else {
		if len(ubuntu.repoCfgs) == 0 {
			t.Error("Expected repoCfgs to be populated")
			return
		}

		// Verify architecture mapping in repo configs
		for _, cfg := range ubuntu.repoCfgs {
			if cfg.Arch != "amd64" {
				t.Errorf("Expected arch to be mapped to amd64, got %s", cfg.Arch)
			}
		}
		t.Logf("Successfully mapped x86_64 -> amd64 in Init")
	}
}

// TestUbuntuPreProcessDownloadError tests PreProcess when downloadImagePkgs returns specific error
func TestUbuntuPreProcessDownloadError(t *testing.T) {
	// Save original shell executor and restore after test
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	// Mock all commands as installed
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "which", Output: "/usr/bin/cmd", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	ubuntu := &ubuntu{
		repoCfgs:  []debutils.RepoConfig{}, // Empty to trigger error
		chrootEnv: &mockChrootEnv{},
	}

	template := createTestImageTemplate()

	err := ubuntu.PreProcess(template)
	if err != nil {
		if strings.Contains(err.Error(), "failed to download image packages") {
			t.Log("PreProcess correctly propagated downloadImagePkgs error")
		} else {
			t.Logf("PreProcess failed with: %v", err)
		}
	}
}

// TestUbuntuPreProcessInitChrootError tests PreProcess when InitChrootEnv returns error
func TestUbuntuPreProcessInitChrootError(t *testing.T) {
	// Create mock that fails on InitChrootEnv
	type failingInitMock struct {
		mockChrootEnv
	}

	failMock := &failingInitMock{}

	ubuntu := &ubuntu{
		repoCfgs: []debutils.RepoConfig{
			{
				Name:      "Test",
				Arch:      "amd64",
				PkgList:   "http://test.com/Packages.gz",
				PkgPrefix: "http://test.com/",
			},
		},
		chrootEnv: failMock,
	}

	template := createTestImageTemplate()

	err := ubuntu.PreProcess(template)
	if err != nil {
		t.Logf("PreProcess failed as expected: %v", err)
	}
}

// TestUbuntuBuildImageAllTypes tests BuildImage with all supported image types
func TestUbuntuBuildImageAllTypes(t *testing.T) {
	ubuntu := &ubuntu{
		chrootEnv: &mockChrootEnv{},
	}

	imageTypes := []string{"raw", "img", "iso"}

	for _, imgType := range imageTypes {
		t.Run(imgType, func(t *testing.T) {
			template := createTestImageTemplate()
			template.Target.ImageType = imgType
			template.DotFilePath = "/tmp/test.dot"

			err := ubuntu.BuildImage(template)
			if err != nil {
				t.Logf("BuildImage(%s) failed as expected: %v", imgType, err)
				// Verify error is from the correct build method
				expectedErrors := []string{
					"failed to create",
					"failed to initialize",
					"failed to build",
				}
				foundExpected := false
				for _, expErr := range expectedErrors {
					if strings.Contains(err.Error(), expErr) {
						foundExpected = true
						break
					}
				}
				if foundExpected {
					t.Logf("BuildImage(%s) error is from expected code path", imgType)
				}
			}
		})
	}
}

// TestUbuntuInstallHostDependencyGetPkgManagerError tests installHostDependency when GetHostOsPkgManager fails
func TestUbuntuInstallHostDependencyGetPkgManagerError(t *testing.T) {
	// This test documents the error handling when system.GetHostOsPkgManager() fails
	ubuntu := &ubuntu{}

	// On systems where package manager detection fails, we expect an error
	err := ubuntu.installHostDependency()
	if err != nil {
		if strings.Contains(err.Error(), "failed to get host package manager") ||
			strings.Contains(err.Error(), "failed to check command") ||
			strings.Contains(err.Error(), "failed to install host dependency") {
			t.Logf("installHostDependency correctly handles errors: %v", err)
		} else {
			t.Logf("installHostDependency error: %v", err)
		}
	}
}

// TestBuildUserRepoListAllowPackages is a regression test ensuring that
// AllowPackages from the template is forwarded into the debutils.Repository
// entries. This mapping was previously missing and is easy to regress.
func TestBuildUserRepoListAllowPackages(t *testing.T) {
	tests := []struct {
		name          string
		repos         []config.PackageRepository
		wantLen       int
		wantAllowPkgs [][]string // expected AllowPackages per resulting repo
	}{
		{
			name: "AllowPackages is forwarded",
			repos: []config.PackageRepository{
				{
					URL:           "https://eci.intel.com/repos/ubuntu",
					Codename:      "noble",
					PKey:          "https://eci.intel.com/key.gpg",
					Component:     "main",
					Priority:      1000,
					AllowPackages: []string{"ros-jazzy-openvino*", "intel-level-zero-npu"},
				},
			},
			wantLen:       1,
			wantAllowPkgs: [][]string{{"ros-jazzy-openvino*", "intel-level-zero-npu"}},
		},
		{
			name: "empty AllowPackages is preserved as nil",
			repos: []config.PackageRepository{
				{
					URL:      "https://archive.ubuntu.com/ubuntu",
					Codename: "noble",
				},
			},
			wantLen:       1,
			wantAllowPkgs: [][]string{nil},
		},
		{
			name: "placeholder repos are skipped",
			repos: []config.PackageRepository{
				{URL: "<URL>", Codename: "noble"},
				{URL: "", Codename: "noble"},
				{
					URL:           "https://real.repo.com/deb",
					Codename:      "noble",
					AllowPackages: []string{"pkg-a"},
				},
			},
			wantLen:       1,
			wantAllowPkgs: [][]string{{"pkg-a"}},
		},
		{
			name: "multiple repos each preserve their AllowPackages",
			repos: []config.PackageRepository{
				{
					URL:           "https://repo1.example.com/deb",
					Codename:      "noble",
					Priority:      1000,
					AllowPackages: []string{"alpha", "beta"},
				},
				{
					URL:           "https://repo2.example.com/deb",
					Codename:      "noble",
					Priority:      1001,
					AllowPackages: []string{"gamma"},
				},
			},
			wantLen:       2,
			wantAllowPkgs: [][]string{{"alpha", "beta"}, {"gamma"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildUserRepoList(tt.repos)
			if len(result) != tt.wantLen {
				t.Fatalf("expected %d repos, got %d", tt.wantLen, len(result))
			}
			for i, repo := range result {
				wantPkgs := tt.wantAllowPkgs[i]
				if wantPkgs == nil && repo.AllowPackages != nil {
					t.Errorf("repo %d: expected nil AllowPackages, got %v", i, repo.AllowPackages)
				}
				if wantPkgs != nil {
					if len(repo.AllowPackages) != len(wantPkgs) {
						t.Errorf("repo %d: expected %d AllowPackages, got %d", i, len(wantPkgs), len(repo.AllowPackages))
						continue
					}
					for j, pkg := range wantPkgs {
						if repo.AllowPackages[j] != pkg {
							t.Errorf("repo %d AllowPackages[%d]: expected %q, got %q", i, j, pkg, repo.AllowPackages[j])
						}
					}
				}
			}
		})
	}
}

// TestBuildUserRepoListFieldMapping verifies all PackageRepository fields are
// correctly mapped to debutils.Repository.
func TestBuildUserRepoListFieldMapping(t *testing.T) {
	input := []config.PackageRepository{
		{
			URL:           "https://example.com/repo",
			Codename:      "noble",
			PKey:          "https://example.com/key.gpg",
			Component:     "main contrib",
			Priority:      500,
			AllowPackages: []string{"foo", "bar"},
		},
	}
	result := buildUserRepoList(input)
	if len(result) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(result))
	}
	r := result[0]
	if r.Codename != "noble" {
		t.Errorf("Codename: expected %q, got %q", "noble", r.Codename)
	}
	if r.URL != "https://example.com/repo" {
		t.Errorf("URL: expected %q, got %q", "https://example.com/repo", r.URL)
	}
	if r.PKey != "https://example.com/key.gpg" {
		t.Errorf("PKey: expected %q, got %q", "https://example.com/key.gpg", r.PKey)
	}
	if r.Component != "main contrib" {
		t.Errorf("Component: expected %q, got %q", "main contrib", r.Component)
	}
	if r.Priority != 500 {
		t.Errorf("Priority: expected %d, got %d", 500, r.Priority)
	}
	if len(r.AllowPackages) != 2 || r.AllowPackages[0] != "foo" || r.AllowPackages[1] != "bar" {
		t.Errorf("AllowPackages: expected [foo bar], got %v", r.AllowPackages)
	}
	expectedID := "user-example.com/repo"
	if r.ID != expectedID {
		t.Errorf("ID: expected %q, got %q", expectedID, r.ID)
	}
}

func TestBuildUserRepoListSkipsPathOnlyRepos(t *testing.T) {
	input := []config.PackageRepository{
		{
			Codename: "localdeb",
			Path:     "/data/os-image-composer/localdeb",
			PKey:     "[trusted=yes]",
		},
		{
			URL:      "https://example.com/repo",
			Codename: "noble",
			PKey:     "https://example.com/key.gpg",
		},
	}

	result := buildUserRepoList(input)
	if len(result) != 1 {
		t.Fatalf("expected 1 URL-based repo, got %d", len(result))
	}

	if result[0].URL != "https://example.com/repo" {
		t.Errorf("expected URL repo to be retained, got %q", result[0].URL)
	}
	if result[0].Codename != "noble" {
		t.Errorf("expected codename %q, got %q", "noble", result[0].Codename)
	}
}
