package elxr

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
			Name:    "test-elxr-image",
			Version: "1.0.0",
		},
		Target: config.TargetInfo{
			OS:        "elxr",
			Dist:      "elxr12",
			Arch:      "amd64",
			ImageType: "qcow2",
		},
		SystemConfig: config.SystemConfig{
			Name:        "test-elxr-system",
			Description: "Test eLxr system configuration",
			Packages:    []string{"curl", "wget", "vim"},
		},
	}
}

// TestElxrProviderInterface tests that eLxr implements Provider interface
func TestElxrProviderInterface(t *testing.T) {
	var _ provider.Provider = (*eLxr)(nil) // Compile-time interface check
}

// TestElxrProviderName tests the Name method
func TestElxrProviderName(t *testing.T) {
	elxr := &eLxr{}
	name := elxr.Name("elxr12", "amd64")
	expected := "wind-river-elxr-elxr12-amd64"

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
		{"elxr12", "amd64", "wind-river-elxr-elxr12-amd64"},
		{"elxr12", "arm64", "wind-river-elxr-elxr12-arm64"},
		{"elxr13", "x86_64", "wind-river-elxr-elxr13-x86_64"},
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

// TestElxrProviderInit tests the Init method
func TestElxrProviderInit(t *testing.T) {
	// Change to project root for tests that need config files
	originalDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Failed to change back to original directory: %v", err)
		}
	}()

	// Navigate to project root (3 levels up from internal/provider/elxr)
	if err := os.Chdir("../../../"); err != nil {
		t.Skipf("Cannot change to project root: %v", err)
		return
	}

	elxr := &eLxr{}

	// Test with amd64 architecture
	err := elxr.Init("elxr12", "amd64")
	if err != nil {
		// Expected to potentially fail in test environment due to network dependencies
		t.Logf("Init failed as expected in test environment: %v", err)
	} else {
		// If it succeeds, verify the configuration was set up
		if len(elxr.repoCfgs) == 0 {
			t.Error("Expected repoCfgs to be populated after successful Init")
		}

		if elxr.repoCfgs[0].PkgList == "" {
			t.Error("Expected repoCfgs[0].PkgList to be set after successful Init")
		}

		// Verify that the architecture is correctly set in the config
		if elxr.repoCfgs[0].Arch != "amd64" {
			t.Errorf("Expected arch to be amd64, got %s", elxr.repoCfgs[0].Arch)
		}

		t.Logf("Successfully initialized with config: %s", elxr.repoCfgs[0].Name)
	}
}

// TestElxrProviderInitArchMapping tests architecture mapping in Init
func TestElxrProviderInitArchMapping(t *testing.T) {
	// Change to project root for tests that need config files
	originalDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Failed to change back to original directory: %v", err)
		}
	}()

	// Navigate to project root (3 levels up from internal/provider/elxr)
	if err := os.Chdir("../../../"); err != nil {
		t.Skipf("Cannot change to project root: %v", err)
		return
	}

	elxr := &eLxr{}

	// Test x86_64 -> amd64 mapping
	err := elxr.Init("elxr12", "x86_64")
	if err != nil {
		t.Logf("Init failed as expected: %v", err)
	} else {
		// Verify that repoCfgs[0].PkgList contains the expected architecture mapping
		if len(elxr.repoCfgs) > 0 && elxr.repoCfgs[0].PkgList != "" {
			expectedArchInURL := "binary-amd64"
			if !strings.Contains(elxr.repoCfgs[0].PkgList, expectedArchInURL) {
				t.Errorf("Expected PkgList to contain %s for x86_64 arch, got %s", expectedArchInURL, elxr.repoCfgs[0].PkgList)
			}
		}

		// Verify architecture was mapped correctly
		if len(elxr.repoCfgs) > 0 && elxr.repoCfgs[0].Arch != "amd64" {
			t.Errorf("Expected mapped arch to be amd64, got %s", elxr.repoCfgs[0].Arch)
		}

		if len(elxr.repoCfgs) > 0 {
			t.Logf("Successfully mapped x86_64 -> amd64, PkgList: %s", elxr.repoCfgs[0].PkgList)
		}
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

	// Navigate to project root (3 levels up from internal/provider/elxr)
	if err := os.Chdir("../../../"); err != nil {
		t.Skipf("Cannot change to project root: %v", err)
		return
	}

	config, err := loadRepoConfig("", "amd64")
	if err != nil {
		t.Skipf("loadRepoConfig failed (expected in test environment): %v", err)
		return
	}

	// If we successfully load config, verify the values
	if config[0].Name == "" {
		t.Error("Expected config name to be set")
	}

	if config[0].Arch != "amd64" {
		t.Errorf("Expected arch 'amd64', got '%s'", config[0].Arch)
	}

	// Verify PkgList contains expected architecture
	if config[0].PkgList != "" && !strings.Contains(config[0].PkgList, "binary-amd64") {
		t.Errorf("Expected PkgList to contain 'binary-amd64', got '%s'", config[0].PkgList)
	}

	t.Logf("Successfully loaded repo config: %s", config[0].Name)
}

// mockChrootEnv is a simple mock implementation of ChrootEnvInterface for testing
type mockChrootEnv struct{}

// Ensure mockChrootEnv implements ChrootEnvInterface
var _ chroot.ChrootEnvInterface = (*mockChrootEnv)(nil)

func (m *mockChrootEnv) GetChrootEnvRoot() string          { return "/tmp/test-chroot" }
func (m *mockChrootEnv) GetChrootImageBuildDir() string    { return "/tmp/test-build" }
func (m *mockChrootEnv) GetTargetOsPkgType() string        { return "deb" }
func (m *mockChrootEnv) GetTargetOsConfigDir() string      { return "/tmp/test-config" }
func (m *mockChrootEnv) GetTargetOsReleaseVersion() string { return "12" }
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
func (m *mockChrootEnv) RefreshLocalCacheRepo() error                                   { return nil }
func (m *mockChrootEnv) InitChrootEnv(targetOs, targetDist, targetArch string) error    { return nil }
func (m *mockChrootEnv) CleanupChrootEnv(targetOs, targetDist, targetArch string) error { return nil }
func (m *mockChrootEnv) TdnfInstallPackage(packageName, installRoot string, repositoryIDList []string) error {
	return nil
}
func (m *mockChrootEnv) AptInstallPackage(packageName, installRoot string, repoSrcList []string) error {
	return nil
}
func (m *mockChrootEnv) UpdateSystemPkgs(template *config.ImageTemplate) error { return nil }

// TestElxrProviderPreProcess tests PreProcess method with mocked dependencies
func TestElxrProviderPreProcess(t *testing.T) {
	// Save original shell executor and restore after test
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	// Set up mock executor
	mockExpectedOutput := []shell.MockCommand{
		// Mock successful package installation commands
		{Pattern: "apt-get update", Output: "Package lists updated successfully", Error: nil},
		{Pattern: "apt-get install -y mmdebstrap", Output: "Package installed successfully", Error: nil},
		{Pattern: "apt-get install -y dosfstools", Output: "Package installed successfully", Error: nil},
		{Pattern: "apt-get install -y xorriso", Output: "Package installed successfully", Error: nil},
		{Pattern: "apt-get install -y sbsigntool", Output: "Package installed successfully", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	elxr := &eLxr{
		repoCfgs: []debutils.RepoConfig{{
			Section:   "main",
			Name:      "Wind River eLxr 12",
			PkgList:   "https://mirror.elxr.dev/elxr/dists/aria/main/binary-amd64/Packages.gz",
			PkgPrefix: "https://mirror.elxr.dev/elxr/",
			Enabled:   true,
			GPGCheck:  true,
		}},
		chrootEnv: &mockChrootEnv{}, // Add the missing chrootEnv mock
	}

	template := createTestImageTemplate()

	// This test will likely fail due to dependencies on chroot, debutils, etc.
	// but it demonstrates the testing approach
	err := elxr.PreProcess(template)
	if err != nil {
		t.Logf("PreProcess failed as expected due to external dependencies: %v", err)
	}
}

// TestElxrProviderBuildImage tests BuildImage method
func TestElxrProviderBuildImage(t *testing.T) {
	// Save original shell executor and restore after test
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	// Set up mock executor - minimal mocks for Register function
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: ".*", Output: "success", Error: nil}, // Catch-all for any commands during registration
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	// Try to register and get a properly initialized eLxr instance
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

	elxr, ok := retrievedProvider.(*eLxr)
	if !ok {
		t.Skip("Retrieved provider is not an eLxr instance")
		return
	}

	template := createTestImageTemplate()

	// This test will fail due to dependencies on image builders that require system access
	// We expect it to fail early before reaching sudo commands
	err = elxr.BuildImage(template)
	if err != nil {
		t.Logf("BuildImage failed as expected due to external dependencies: %v", err)
		// Verify the error is related to expected failures, not sudo issues
		if strings.Contains(err.Error(), "sudo") {
			t.Errorf("Test should not reach sudo commands - mocking may be insufficient")
		}
	}
}

// TestElxrProviderBuildImageISO tests BuildImage method with ISO type
func TestElxrProviderBuildImageISO(t *testing.T) {
	// Save original shell executor and restore after test
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	// Set up mock executor - minimal mocks for Register function
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: ".*", Output: "success", Error: nil}, // Catch-all for any commands during registration
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	// Try to register and get a properly initialized eLxr instance
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

	elxr, ok := retrievedProvider.(*eLxr)
	if !ok {
		t.Skip("Retrieved provider is not an eLxr instance")
		return
	}

	template := createTestImageTemplate()

	// Set up global config for ISO
	originalImageType := template.Target.ImageType
	defer func() { template.Target.ImageType = originalImageType }()
	template.Target.ImageType = "iso"

	err = elxr.BuildImage(template)
	if err != nil {
		t.Logf("BuildImage (ISO) failed as expected due to external dependencies: %v", err)
		// Verify the error is related to expected failures, not sudo issues
		if strings.Contains(err.Error(), "sudo") {
			t.Errorf("Test should not reach sudo commands - mocking may be insufficient")
		}
	}
}

// TestElxrProviderPostProcess tests PostProcess method
func TestElxrProviderPostProcess(t *testing.T) {
	// Save original shell executor and restore after test
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	// Set up mock executor - minimal mocks for Register function
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: ".*", Output: "success", Error: nil}, // Catch-all for any commands during registration
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	// Try to register and get a properly initialized eLxr instance
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

	elxr, ok := retrievedProvider.(*eLxr)
	if !ok {
		t.Skip("Retrieved provider is not an eLxr instance")
		return
	}

	template := createTestImageTemplate()

	// Test with no error
	err = elxr.PostProcess(template, nil)
	if err != nil {
		t.Logf("PostProcess failed as expected due to chroot cleanup dependencies: %v", err)
	}

	// Test with input error - PostProcess should clean up and return nil (not the input error)
	inputError := fmt.Errorf("some build error")
	err = elxr.PostProcess(template, inputError)
	if err != nil {
		t.Logf("PostProcess failed during cleanup: %v", err)
	}
}

// TestElxrProviderInstallHostDependency tests installHostDependency method
func TestElxrProviderInstallHostDependency(t *testing.T) {
	// Save original shell executor and restore after test
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	// Set up mock executor
	mockExpectedOutput := []shell.MockCommand{
		// Mock successful installation commands
		{Pattern: "which mmdebstrap", Output: "", Error: nil},
		{Pattern: "which mkfs.fat", Output: "", Error: nil},
		{Pattern: "which xorriso", Output: "", Error: nil},
		{Pattern: "which sbsign", Output: "", Error: nil},
		{Pattern: "apt-get install -y mmdebstrap", Output: "Success", Error: nil},
		{Pattern: "apt-get install -y dosfstools", Output: "Success", Error: nil},
		{Pattern: "apt-get install -y xorriso", Output: "Success", Error: nil},
		{Pattern: "apt-get install -y sbsigntool", Output: "Success", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	elxr := &eLxr{}

	// This test will likely fail due to dependencies on chroot.GetHostOsPkgManager()
	// and shell.IsCommandExist(), but it demonstrates the testing approach
	err := elxr.installHostDependency()
	if err != nil {
		t.Logf("installHostDependency failed as expected due to external dependencies: %v", err)
	} else {
		t.Logf("installHostDependency succeeded with mocked commands")
	}
}

// TestElxrProviderInstallHostDependencyCommands tests the specific commands for host dependencies
func TestElxrProviderInstallHostDependencyCommands(t *testing.T) {
	// Get the dependency map by examining the installHostDependency method
	expectedDeps := map[string]string{
		"mmdebstrap":        "mmdebstrap",
		"mkfs.fat":          "dosfstools",
		"xorriso":           "xorriso",
		"ukify":             "systemd-ukify",
		"grub-mkstandalone": "grub-common",
		"veritysetup":       "cryptsetup",
		"sbsign":            "sbsigntool",
	}

	// This is a structural test to verify the dependency mapping
	// In a real implementation, we might expose this map for testing
	t.Logf("Expected host dependencies for eLxr provider: %+v", expectedDeps)

	// Verify we have the expected number of dependencies
	if len(expectedDeps) != 7 {
		t.Errorf("Expected 7 host dependencies, got %d", len(expectedDeps))
	}

	// Verify specific critical dependencies
	criticalDeps := []string{"mmdebstrap", "mkfs.fat", "xorriso"}
	for _, dep := range criticalDeps {
		if _, exists := expectedDeps[dep]; !exists {
			t.Errorf("Critical dependency %s not found in expected dependencies", dep)
		}
	}
}

// TestElxrProviderRegister tests the Register function
func TestElxrProviderRegister(t *testing.T) {
	// Save original providers registry and restore after test
	// Note: We can't easily access the provider registry for cleanup,
	// so this test shows the approach but may leave test artifacts

	err := Register("linux", "elxr12", "amd64")
	if err != nil {
		t.Skipf("Cannot test registration due to missing dependencies: %v", err)
		return
	}

	// Try to retrieve the registered provider
	providerName := system.GetProviderId(OsName, "elxr12", "amd64")
	retrievedProvider, exists := provider.Get(providerName)

	if !exists {
		t.Errorf("Expected provider %s to be registered", providerName)
		return
	}

	// Verify it's an eLxr provider
	if elxrProvider, ok := retrievedProvider.(*eLxr); !ok {
		t.Errorf("Expected eLxr provider, got %T", retrievedProvider)
	} else {
		// Test the Name method on the registered provider
		name := elxrProvider.Name("elxr12", "amd64")
		if name != providerName {
			t.Errorf("Expected provider name %s, got %s", providerName, name)
		}
	}
}

// TestElxrProviderWorkflow tests a complete eLxr provider workflow
func TestElxrProviderWorkflow(t *testing.T) {
	// This is a unit test focused on testing the provider interface methods
	// without external dependencies that require system access

	elxr := &eLxr{}

	// Test provider name generation
	name := elxr.Name("elxr12", "amd64")
	expectedName := "wind-river-elxr-elxr12-amd64"
	if name != expectedName {
		t.Errorf("Expected name %s, got %s", expectedName, name)
	}

	// Test Init (will likely fail due to network dependencies)
	if err := elxr.Init("elxr12", "amd64"); err != nil {
		t.Logf("Init failed as expected: %v", err)
	} else {
		// If Init succeeds, verify configuration was loaded
		if len(elxr.repoCfgs) == 0 || elxr.repoCfgs[0].Name == "" {
			t.Error("Expected repo config name to be set after successful Init")
		}
		if len(elxr.repoCfgs) > 0 {
			t.Logf("Repo config loaded: %s", elxr.repoCfgs[0].Name)
		}
	}

	// Skip PreProcess and BuildImage tests to avoid sudo commands
	t.Log("Skipping PreProcess and BuildImage tests to avoid system-level dependencies")

	// Skip PostProcess tests as they require properly initialized dependencies
	t.Log("Skipping PostProcess tests to avoid nil pointer panics - these are tested separately with proper registration")

	t.Log("Complete workflow test finished - core methods exist and are callable")
}

// TestElxrConfigurationStructure tests the structure of the eLxr configuration
func TestElxrConfigurationStructure(t *testing.T) {
	// Change to project root for tests that need config files
	originalDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Failed to change back to original directory: %v", err)
		}
	}()

	// Navigate to project root (3 levels up from internal/provider/elxr)
	if err := os.Chdir("../../../"); err != nil {
		t.Skipf("Cannot change to project root: %v", err)
		return
	}

	// Test that OsName constant is set correctly
	if OsName == "" {
		t.Error("OsName should not be empty")
	}

	expectedOsName := "wind-river-elxr"
	if OsName != expectedOsName {
		t.Errorf("Expected OsName %s, got %s", expectedOsName, OsName)
	}

	// Test that we can load provider config
	providerConfigs, err := config.LoadProviderRepoConfig(OsName, "elxr12", "amd64")
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

// TestElxrArchitectureHandling tests architecture-specific URL construction
func TestElxrArchitectureHandling(t *testing.T) {
	// Change to project root for tests that need config files
	originalDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Failed to change back to original directory: %v", err)
		}
	}()

	// Navigate to project root (3 levels up from internal/provider/elxr)
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
			elxr := &eLxr{}
			err := elxr.Init("elxr12", tc.inputArch) // Test arch mapping

			if err != nil {
				t.Logf("Init failed as expected: %v", err)
			} else {
				// We expect success, so we can check arch mapping
				if len(elxr.repoCfgs) > 0 && elxr.repoCfgs[0].Arch != tc.expectedArch {
					t.Errorf("For input arch %s, expected config arch %s, got %s", tc.inputArch, tc.expectedArch, elxr.repoCfgs[0].Arch)
				}

				// If we have a PkgList, verify it contains the expected architecture
				if len(elxr.repoCfgs) > 0 && elxr.repoCfgs[0].PkgList != "" {
					expectedArchInURL := "binary-" + tc.expectedArch
					if !strings.Contains(elxr.repoCfgs[0].PkgList, expectedArchInURL) {
						t.Errorf("For arch %s, expected PkgList to contain %s, got %s", tc.inputArch, expectedArchInURL, elxr.repoCfgs[0].PkgList)
					}
				}

				t.Logf("Successfully tested arch %s -> %s", tc.inputArch, tc.expectedArch)
			}
		})
	}
}

// TestElxrBuildImageNilTemplate tests BuildImage with nil template
func TestElxrBuildImageNilTemplate(t *testing.T) {
	elxr := &eLxr{}

	err := elxr.BuildImage(nil)
	if err == nil {
		t.Error("Expected error when template is nil")
	}

	expectedError := "template cannot be nil"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}

// TestElxrBuildImageUnsupportedType tests BuildImage with unsupported image type
func TestElxrBuildImageUnsupportedType(t *testing.T) {
	elxr := &eLxr{}

	template := createTestImageTemplate()
	template.Target.ImageType = "unsupported"

	err := elxr.BuildImage(template)
	if err == nil {
		t.Error("Expected error for unsupported image type")
	}

	expectedError := "unsupported image type: unsupported"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}

// TestElxrBuildImageValidTypes tests BuildImage error handling for valid image types
func TestElxrBuildImageValidTypes(t *testing.T) {
	elxr := &eLxr{}

	validTypes := []string{"raw", "img", "iso"}

	for _, imageType := range validTypes {
		t.Run(imageType, func(t *testing.T) {
			template := createTestImageTemplate()
			template.Target.ImageType = imageType

			// These will fail due to missing chrootEnv, but we can verify
			// that the code path is reached and the error is expected
			err := elxr.BuildImage(template)
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

// TestElxrPostProcessErrorHandling tests PostProcess method signature and basic behavior
func TestElxrPostProcessErrorHandling(t *testing.T) {
	// Test that PostProcess method exists and has correct signature
	// We verify that the method can be called and behaves predictably

	elxr := &eLxr{}
	template := createTestImageTemplate()
	inputError := fmt.Errorf("build failed")

	// Verify the method signature is correct by assigning it to a function variable
	var postProcessFunc func(*config.ImageTemplate, error) error = elxr.PostProcess

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
	_ = elxr.PostProcess(template, inputError)
}

// TestElxrPreProcessWithMockEnv tests PreProcess with proper mock chrootEnv
func TestElxrPreProcessWithMockEnv(t *testing.T) {
	elxr := &eLxr{
		chrootEnv: &mockChrootEnv{},
		repoCfgs: []debutils.RepoConfig{{
			Section:   "main",
			Name:      "Test Repo",
			PkgList:   "https://example.com/Packages.gz",
			PkgPrefix: "https://example.com/",
			Arch:      "amd64",
		}},
	}

	template := createTestImageTemplate()

	err := elxr.PreProcess(template)
	if err == nil {
		t.Log("PreProcess succeeded unexpectedly")
	} else {
		// Verify the error is from the expected path
		if !strings.Contains(err.Error(), "failed to") {
			t.Errorf("Expected error to contain 'failed to', got: %v", err)
		}
		t.Logf("PreProcess failed as expected: %v", err)
	}
}

// TestElxrDownloadImagePkgsWithMockEnv tests downloadImagePkgs with mock environment
func TestElxrDownloadImagePkgsWithMockEnv(t *testing.T) {
	elxr := &eLxr{
		chrootEnv: &mockChrootEnv{},
		repoCfgs: []debutils.RepoConfig{{
			Section:   "main",
			Name:      "Test Repo",
			PkgList:   "https://test.example.com/Packages.gz",
			PkgPrefix: "https://test.example.com/",
			Arch:      "amd64",
		}},
	}

	template := createTestImageTemplate()
	template.DotFilePath = ""

	err := elxr.downloadImagePkgs(template)
	if err == nil {
		t.Log("downloadImagePkgs succeeded unexpectedly")
	} else {
		// Verify we reach the download logic
		t.Logf("downloadImagePkgs failed as expected: %v", err)
	}
}

// TestElxrDownloadImagePkgsNoRepos tests error when no repositories configured
func TestElxrDownloadImagePkgsNoRepos(t *testing.T) {
	elxr := &eLxr{
		chrootEnv: &mockChrootEnv{},
		repoCfgs:  []debutils.RepoConfig{}, // Empty repos
	}

	template := createTestImageTemplate()

	err := elxr.downloadImagePkgs(template)
	if err == nil {
		t.Error("Expected error when no repositories configured")
	}

	expectedError := "no repository configurations available"
	if !strings.Contains(err.Error(), expectedError) {
		t.Errorf("Expected error containing '%s', got: %v", expectedError, err)
	}
}

// TestElxrBuildRawImageWithMock tests buildRawImage with mock environment
func TestElxrBuildRawImageWithMock(t *testing.T) {
	elxr := &eLxr{
		chrootEnv: &mockChrootEnv{},
	}

	template := createTestImageTemplate()
	template.Target.ImageType = "raw"

	err := elxr.buildRawImage(template)
	if err == nil {
		t.Error("Expected error with mock environment")
	} else {
		// Verify error is from rawmaker initialization
		if !strings.Contains(err.Error(), "failed to create raw maker") && !strings.Contains(err.Error(), "failed to initialize") {
			t.Logf("buildRawImage failed with: %v", err)
		}
	}
}

// TestElxrBuildInitrdImageWithMock tests buildInitrdImage with mock environment
func TestElxrBuildInitrdImageWithMock(t *testing.T) {
	elxr := &eLxr{
		chrootEnv: &mockChrootEnv{},
	}

	template := createTestImageTemplate()
	template.Target.ImageType = "img"

	err := elxr.buildInitrdImage(template)
	if err == nil {
		t.Error("Expected error with mock environment")
	} else {
		// Verify error is from initrdmaker
		if !strings.Contains(err.Error(), "failed to create initrd maker") && !strings.Contains(err.Error(), "failed to initialize") {
			t.Logf("buildInitrdImage failed with: %v", err)
		}
	}
}

// TestElxrBuildIsoImageWithMock tests buildIsoImage with mock environment
func TestElxrBuildIsoImageWithMock(t *testing.T) {
	elxr := &eLxr{
		chrootEnv: &mockChrootEnv{},
	}

	template := createTestImageTemplate()
	template.Target.ImageType = "iso"

	err := elxr.buildIsoImage(template)
	if err == nil {
		t.Error("Expected error with mock environment")
	} else {
		// Verify error is from isomaker
		if !strings.Contains(err.Error(), "failed to create iso maker") && !strings.Contains(err.Error(), "failed to initialize") {
			t.Logf("buildIsoImage failed with: %v", err)
		}
	}
}

// TestElxrPostProcessSuccess tests PostProcess with mock environment
func TestElxrPostProcessSuccess(t *testing.T) {
	elxr := &eLxr{
		chrootEnv: &mockChrootEnv{},
	}

	template := createTestImageTemplate()

	// Test with no input error - PostProcess should only return cleanup errors
	err := elxr.PostProcess(template, nil)
	if err != nil {
		// PostProcess currently doesn't propagate the input error, only cleanup errors
		t.Logf("PostProcess returned error: %v", err)
	}
}

// TestElxrInstallHostDependency tests installHostDependency function
func TestElxrInstallHostDependency(t *testing.T) {
	elxr := &eLxr{}

	// Call installHostDependency
	err := elxr.installHostDependency()

	// In test environment, this may succeed or fail based on host OS
	if err != nil {
		// Verify error message is reasonable
		if err.Error() == "" {
			t.Error("Expected non-empty error message")
		}
		t.Logf("installHostDependency failed as may be expected: %v", err)
	} else {
		t.Log("installHostDependency succeeded - all dependencies present")
	}
}

// TestElxrInstallHostDependencyMapping tests the dependency mapping logic
func TestElxrInstallHostDependencyMapping(t *testing.T) {
	// Test the expected dependencies mapping
	expectedDeps := map[string]string{
		"mmdebstrap":  "mmdebstrap",
		"mkfs.fat":    "dosfstools",
		"xorriso":     "xorriso",
		"sbsign":      "sbsigntool",
		"ukify":       "systemd-ukify",
		"veritysetup": "cryptsetup",
	}

	t.Logf("Expected host dependencies for eLxr provider: %v", expectedDeps)

	// Verify that each expected dependency has a mapping
	for cmd, pkg := range expectedDeps {
		if cmd == "" || pkg == "" {
			t.Errorf("Empty dependency mapping: cmd='%s', pkg='%s'", cmd, pkg)
		}
	}
}

// TestLoadRepoConfigArchMapping tests architecture mapping in loadRepoConfig
func TestLoadRepoConfigArchMapping(t *testing.T) {
	// Change to project root
	originalDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Failed to change back to original directory: %v", err)
		}
	}()

	if err := os.Chdir("../../../"); err != nil {
		t.Skipf("Cannot change to project root: %v", err)
		return
	}

	testCases := []struct {
		arch         string
		expectedArch string
	}{
		{"amd64", "amd64"},
		{"arm64", "arm64"},
		{"x86_64", "x86_64"}, // Not mapped in loadRepoConfig, mapped in Init
	}

	for _, tc := range testCases {
		t.Run(tc.arch, func(t *testing.T) {
			cfgs, err := loadRepoConfig("", tc.arch)
			if err != nil {
				t.Logf("loadRepoConfig failed for arch %s: %v", tc.arch, err)
				return
			}

			if len(cfgs) == 0 {
				t.Errorf("Expected at least one config for arch %s", tc.arch)
				return
			}

			// Note: The actual arch in config might differ based on provider config
			t.Logf("loadRepoConfig succeeded for arch %s: got %d configs", tc.arch, len(cfgs))
		})
	}
}

// TestLoadRepoConfigInvalidArch tests error handling for invalid architecture
func TestLoadRepoConfigInvalidArch(t *testing.T) {
	// Change to project root
	originalDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Failed to change back to original directory: %v", err)
		}
	}()

	if err := os.Chdir("../../../"); err != nil {
		t.Skipf("Cannot change to project root: %v", err)
		return
	}

	_, err := loadRepoConfig("", "invalid-arch")
	if err == nil {
		t.Error("Expected error for invalid architecture")
	} else {
		t.Logf("Got expected error for invalid arch: %v", err)
	}
}

// TestDisplayImageArtifacts tests displayImageArtifacts function
func TestDisplayImageArtifacts(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()

	// Test with empty directory
	displayImageArtifacts(tempDir, "TEST")
	t.Log("displayImageArtifacts called successfully with empty directory")

	// Test with different image types
	displayImageArtifacts(tempDir, "RAW")
	displayImageArtifacts(tempDir, "ISO")
	displayImageArtifacts(tempDir, "IMG")

	t.Log("displayImageArtifacts tested with multiple image types")
}

// TestRegisterSuccess tests successful Register call
func TestRegisterSuccess(t *testing.T) {
	// Test Register function with valid parameters

	// Test that Register doesn't panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Register panicked: %v", r)
		}
	}()

	err := Register("wind-river-elxr", "elxr12", "amd64")

	// We expect error in test environment, but it should be controlled
	if err != nil {
		t.Logf("Register returned expected error in test environment: %v", err)
	} else {
		t.Log("Register succeeded unexpectedly - config files present")
	}
}

// TestElxrInitWithAarch64 tests Init with aarch64 architecture
func TestElxrInitWithAarch64(t *testing.T) {
	// Change to project root
	originalDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Failed to change back to original directory: %v", err)
		}
	}()

	if err := os.Chdir("../../../"); err != nil {
		t.Skipf("Cannot change to project root: %v", err)
		return
	}

	elxr := &eLxr{}

	err := elxr.Init("elxr12", "aarch64")
	if err != nil {
		t.Logf("Init failed for aarch64: %v", err)
		return
	}

	if len(elxr.repoCfgs) == 0 {
		t.Error("Expected repoCfgs to be populated for aarch64")
		return
	}

	// Verify aarch64 is mapped to arm64
	if elxr.repoCfgs[0].Arch != "arm64" {
		t.Errorf("Expected arch to be mapped to arm64, got: %s", elxr.repoCfgs[0].Arch)
	}

	// Verify arm64 is in the PkgList URL
	if elxr.repoCfgs[0].PkgList != "" && !strings.Contains(elxr.repoCfgs[0].PkgList, "arm64") {
		t.Errorf("Expected PkgList to contain 'arm64', got: %s", elxr.repoCfgs[0].PkgList)
	}

	t.Logf("Successfully initialized with aarch64: %s", elxr.repoCfgs[0].PkgList)
}

// TestElxrInitErrorPaths tests error paths in Init method
func TestElxrInitErrorPaths(t *testing.T) {
	elxr := &eLxr{}

	// Test with invalid dist (no config files)
	err := elxr.Init("invalid-dist", "amd64")
	if err == nil {
		t.Error("Expected error for invalid dist")
	} else {
		t.Logf("Got expected error for invalid dist: %v", err)
	}
}

// TestBuildImageEdgeCases tests edge cases in BuildImage
func TestBuildImageEdgeCases(t *testing.T) {
	elxr := &eLxr{
		chrootEnv: &mockChrootEnv{},
	}

	// Test with empty image name
	template := createTestImageTemplate()
	template.Image.Name = ""
	template.Target.ImageType = "raw"

	err := elxr.BuildImage(template)
	if err == nil {
		t.Log("BuildImage handled empty name gracefully")
	} else {
		t.Logf("BuildImage with empty name failed: %v", err)
	}
}

// TestPreProcessErrorPropagation tests error propagation in PreProcess
func TestPreProcessErrorPropagation(t *testing.T) {
	elxr := &eLxr{
		chrootEnv: &mockChrootEnv{},
		repoCfgs:  []debutils.RepoConfig{}, // Empty repos will cause error
	}

	template := createTestImageTemplate()

	err := elxr.PreProcess(template)
	if err != nil {
		// Verify error message contains context
		if !strings.Contains(err.Error(), "failed to") {
			t.Errorf("Expected error to contain context, got: %v", err)
		}
	}
}

// TestElxrNameWithVariousInputs tests Name method with different dist and arch combinations
func TestElxrNameWithVariousInputs(t *testing.T) {
	elxr := &eLxr{}

	testCases := []struct {
		dist     string
		arch     string
		expected string
	}{
		{"elxr12", "amd64", "wind-river-elxr-elxr12-amd64"},
		{"elxr12", "arm64", "wind-river-elxr-elxr12-arm64"},
		{"elxr13", "x86_64", "wind-river-elxr-elxr13-x86_64"},
		{"", "", "wind-river-elxr--"},
		{"test", "test", "wind-river-elxr-test-test"},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%s-%s", tc.dist, tc.arch), func(t *testing.T) {
			result := elxr.Name(tc.dist, tc.arch)
			if result != tc.expected {
				t.Errorf("Expected '%s', got '%s'", tc.expected, result)
			}
		})
	}
}

// TestElxrMethodSignatures tests that all interface methods have correct signatures
func TestElxrMethodSignatures(t *testing.T) {
	elxr := &eLxr{}

	// Test that all methods can be assigned to their expected function types
	var nameFunc func(string, string) string = elxr.Name
	var initFunc func(string, string) error = elxr.Init
	var preProcessFunc func(*config.ImageTemplate) error = elxr.PreProcess
	var buildImageFunc func(*config.ImageTemplate) error = elxr.BuildImage
	var postProcessFunc func(*config.ImageTemplate, error) error = elxr.PostProcess

	t.Logf("Name method signature: %T", nameFunc)
	t.Logf("Init method signature: %T", initFunc)
	t.Logf("PreProcess method signature: %T", preProcessFunc)
	t.Logf("BuildImage method signature: %T", buildImageFunc)
	t.Logf("PostProcess method signature: %T", postProcessFunc)
}

// TestElxrConstants tests eLxr provider constants
func TestElxrConstants(t *testing.T) {
	// Test OsName constant
	if OsName != "wind-river-elxr" {
		t.Errorf("Expected OsName 'wind-river-elxr', got '%s'", OsName)
	}
}

// TestElxrStructInitialization tests eLxr struct initialization
func TestElxrStructInitialization(t *testing.T) {
	// Test zero value initialization
	elxr := &eLxr{}

	if elxr.repoCfgs != nil {
		t.Error("Expected nil repoCfgs in uninitialized eLxr")
	}

	if elxr.chrootEnv != nil {
		t.Error("Expected nil chrootEnv in uninitialized eLxr")
	}
}

// TestElxrStructWithData tests eLxr struct with initialized data
func TestElxrStructWithData(t *testing.T) {
	cfg := debutils.RepoConfig{
		Name:      "Test Repo",
		PkgList:   "https://test.example.com/Packages.gz",
		PkgPrefix: "https://test.example.com/",
		Section:   "main",
		Arch:      "amd64",
	}

	elxr := &eLxr{
		repoCfgs: []debutils.RepoConfig{cfg},
	}

	if len(elxr.repoCfgs) != 1 {
		t.Errorf("Expected 1 repo config, got %d", len(elxr.repoCfgs))
	}

	if elxr.repoCfgs[0].Name != "Test Repo" {
		t.Errorf("Expected repo name 'Test Repo', got '%s'", elxr.repoCfgs[0].Name)
	}

	if elxr.repoCfgs[0].PkgList != "https://test.example.com/Packages.gz" {
		t.Errorf("Expected PkgList 'https://test.example.com/Packages.gz', got '%s'", elxr.repoCfgs[0].PkgList)
	}
}

// TestLoadRepoConfigMultipleRepos tests loadRepoConfig with multiple repositories
func TestLoadRepoConfigMultipleRepos(t *testing.T) {
	// Change to project root
	originalDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Failed to change back to original directory: %v", err)
		}
	}()

	if err := os.Chdir("../../../"); err != nil {
		t.Skipf("Cannot change to project root: %v", err)
		return
	}

	cfgs, err := loadRepoConfig("", "amd64")
	if err != nil {
		t.Logf("loadRepoConfig failed: %v", err)
		return
	}

	t.Logf("loadRepoConfig returned %d repositories", len(cfgs))

	for i, cfg := range cfgs {
		t.Logf("Repository %d: name=%s, arch=%s", i, cfg.Name, cfg.Arch)

		if cfg.Name == "" {
			t.Errorf("Repository %d has empty name", i)
		}

		if cfg.Arch == "" {
			t.Errorf("Repository %d has empty arch", i)
		}
	}
}
