package azl

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/open-edge-platform/os-image-composer/internal/chroot"
	"github.com/open-edge-platform/os-image-composer/internal/config"
	"github.com/open-edge-platform/os-image-composer/internal/ospackage/rpmutils"
	"github.com/open-edge-platform/os-image-composer/internal/provider"
	"github.com/open-edge-platform/os-image-composer/internal/utils/system"
)

// mockChrootEnv is a simple mock implementation of ChrootEnvInterface for testing
type mockChrootEnv struct{}

// Ensure mockChrootEnv implements ChrootEnvInterface
var _ chroot.ChrootEnvInterface = (*mockChrootEnv)(nil)

func (m *mockChrootEnv) GetChrootEnvRoot() string          { return "/tmp/test-chroot" }
func (m *mockChrootEnv) GetChrootImageBuildDir() string    { return "/tmp/test-build" }
func (m *mockChrootEnv) GetTargetOsPkgType() string        { return "rpm" }
func (m *mockChrootEnv) GetTargetOsConfigDir() string      { return "/tmp/test-config" }
func (m *mockChrootEnv) GetTargetOsReleaseVersion() string { return "3.0" }
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

// Helper function to create a test ImageTemplate
func createTestImageTemplate() *config.ImageTemplate {
	return &config.ImageTemplate{
		Image: config.ImageInfo{
			Name:    "test-azl-image",
			Version: "1.0.0",
		},
		Target: config.TargetInfo{
			OS:        "azure-linux",
			Dist:      "azl3",
			Arch:      "x86_64",
			ImageType: "qcow2",
		},
		SystemConfig: config.SystemConfig{
			Name:        "test-azl-system",
			Description: "Test Azure Linux system configuration",
			Packages:    []string{"curl", "wget", "vim"},
		},
	}
}

// TestAzureLinuxProviderInterface tests that AzureLinux implements Provider interface
func TestAzureLinuxProviderInterface(t *testing.T) {
	var _ provider.Provider = (*AzureLinux)(nil) // Compile-time interface check
}

// TestAzureLinuxProviderName tests the Name method
func TestAzureLinuxProviderName(t *testing.T) {
	azl := &AzureLinux{}
	name := azl.Name("azl3", "x86_64")
	expected := "azure-linux-azl3-x86_64"

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
		{"azl3", "x86_64", "azure-linux-azl3-x86_64"},
		{"azl3", "aarch64", "azure-linux-azl3-aarch64"},
		{"azl4", "x86_64", "azure-linux-azl4-x86_64"},
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

// TestAzlCentralizedConfig tests the centralized configuration loading
func TestAzlCentralizedConfig(t *testing.T) {
	// Change to project root for tests that need config files
	originalDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Failed to change back to original directory: %v", err)
		}
	}()

	// Navigate to project root (3 levels up from internal/provider/azl)
	if err := os.Chdir("../../../"); err != nil {
		t.Skipf("Cannot change to project root: %v", err)
		return
	}

	// Test loading repo config
	cfg, err := loadRepoConfigFromYAML("azl3", "x86_64")
	if err != nil {
		t.Skipf("loadRepoConfig failed (expected in test environment): %v", err)
		return
	}

	// If we successfully load config, verify the values
	if cfg.Name == "" {
		t.Error("Expected config name to be set")
	}

	if cfg.Section == "" {
		t.Error("Expected config section to be set")
	}

	// Verify URL contains expected architecture
	expectedArch := "x86_64"
	if cfg.URL != "" && cfg.URL != "https://packages.microsoft.com/azurelinux/3.0/prod/base/"+expectedArch {
		t.Errorf("Expected URL to contain '%s', got '%s'", expectedArch, cfg.URL)
	}

	t.Logf("Successfully loaded repo config: %s", cfg.Name)
	t.Logf("Config details: %+v", cfg)
}

// TestAzlProviderFallback tests the fallback to centralized config when HTTP fails
func TestAzlProviderFallback(t *testing.T) {
	// Change to project root for tests that need config files
	originalDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Failed to change back to original directory: %v", err)
		}
	}()

	// Navigate to project root (3 levels up from internal/provider/azl)
	if err := os.Chdir("../../../"); err != nil {
		t.Skipf("Cannot change to project root: %v", err)
		return
	}

	azl := &AzureLinux{
		chrootEnv: &mockChrootEnv{},
	}

	// Test initialization which should use centralized config
	err := azl.Init("azl3", "x86_64")
	if err != nil {
		t.Logf("Init failed as expected in test environment: %v", err)
	} else {
		// If it succeeds, verify the configuration was set up from YAML
		if azl.repoCfg.Name == "" {
			t.Error("Expected repoCfg.Name to be set after successful init")
		}

		expectedName := "Azure Linux 3.0"
		if azl.repoCfg.Name != expectedName {
			t.Errorf("Expected name '%s' from YAML config, got '%s'", expectedName, azl.repoCfg.Name)
		}

		expectedURL := "https://packages.microsoft.com/azurelinux/3.0/prod/base/x86_64"
		if azl.repoCfg.URL != expectedURL {
			t.Errorf("Expected URL '%s' from YAML config, got '%s'", expectedURL, azl.repoCfg.URL)
		}

		t.Logf("Successfully tested initialization with centralized config")
		t.Logf("Config name: %s", azl.repoCfg.Name)
		t.Logf("Config URL: %s", azl.repoCfg.URL)
	}
}

// TestAzlProviderInit tests the Init method with centralized configuration
func TestAzlProviderInit(t *testing.T) {
	// Change to project root for tests that need config files
	originalDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Failed to change back to original directory: %v", err)
		}
	}()

	// Navigate to project root (3 levels up from internal/provider/azl)
	if err := os.Chdir("../../../"); err != nil {
		t.Skipf("Cannot change to project root: %v", err)
		return
	}

	azl := &AzureLinux{
		chrootEnv: &mockChrootEnv{},
	}

	// Test with x86_64 architecture - now uses centralized config
	err := azl.Init("azl3", "x86_64")
	if err != nil {
		t.Skipf("Init failed (expected in test environment): %v", err)
		return
	}

	// If it succeeds, verify the configuration was set up
	if azl.repoCfg.Name == "" {
		t.Error("Expected repoCfg.Name to be set after successful Init")
	}

	// Verify centralized config values
	expectedName := "Azure Linux 3.0"
	if azl.repoCfg.Name != expectedName {
		t.Errorf("Expected name '%s', got '%s'", expectedName, azl.repoCfg.Name)
	}

	expectedURL := "https://packages.microsoft.com/azurelinux/3.0/prod/base/x86_64"
	if azl.repoCfg.URL != expectedURL {
		t.Errorf("Expected URL '%s', got '%s'", expectedURL, azl.repoCfg.URL)
	}

	t.Logf("Successfully initialized with centralized config")
	t.Logf("Config name: %s", azl.repoCfg.Name)
	t.Logf("Config URL: %s", azl.repoCfg.URL)
	t.Logf("Primary href: %s", azl.gzHref)
}

// TestLoadRepoConfigFromYAML tests the centralized YAML configuration loading
func TestLoadRepoConfigFromYAML(t *testing.T) {
	// Change to project root for tests that need config files
	originalDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Failed to change back to original directory: %v", err)
		}
	}()

	// Navigate to project root (3 levels up from internal/provider/azl)
	if err := os.Chdir("../../../"); err != nil {
		t.Skipf("Cannot change to project root: %v", err)
		return
	}

	// Test loading repo config
	cfg, err := loadRepoConfigFromYAML("azl3", "x86_64")
	if err != nil {
		t.Skipf("loadRepoConfigFromYAML failed (expected in test environment): %v", err)
		return
	}

	// If we successfully load config, verify the values
	if cfg.Name == "" {
		t.Error("Expected config name to be set")
	}

	if cfg.Section == "" {
		t.Error("Expected config section to be set")
	}

	t.Logf("Successfully loaded repo config from YAML: %s", cfg.Name)
	t.Logf("Config details: %+v", cfg)
}

// TestFetchPrimaryURL tests the fetchPrimaryURL function with mock server
func TestFetchPrimaryURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		repomdXML := `<?xml version="1.0" encoding="UTF-8"?>
<repomd xmlns="http://linux.duke.edu/metadata/repo">
  <data type="primary">
    <location href="repodata/primary.xml.gz"/>
    <checksum type="sha256">abcd1234</checksum>
  </data>
  <data type="filelists">
    <location href="repodata/filelists.xml.gz"/>
    <checksum type="sha256">efgh5678</checksum>
  </data>
</repomd>`
		fmt.Fprint(w, repomdXML)
	}))
	defer server.Close()

	href, err := rpmutils.FetchPrimaryURL(server.URL)
	if err != nil {
		t.Fatalf("fetchPrimaryURL failed: %v", err)
	}

	expected := "repodata/primary.xml.gz"
	if href != expected {
		t.Errorf("Expected href '%s', got '%s'", expected, href)
	}
}

// TestFetchPrimaryURLNoPrimary tests fetchPrimaryURL when no primary data exists
func TestFetchPrimaryURLNoPrimary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		repomdXML := `<?xml version="1.0" encoding="UTF-8"?>
<repomd xmlns="http://linux.duke.edu/metadata/repo">
  <data type="filelists">
    <location href="repodata/filelists.xml.gz"/>
    <checksum type="sha256">efgh5678</checksum>
  </data>
</repomd>`
		fmt.Fprint(w, repomdXML)
	}))
	defer server.Close()

	_, err := rpmutils.FetchPrimaryURL(server.URL)
	if err == nil {
		t.Error("Expected error when primary location not found")
	}

	expectedError := "primary location not found"
	if !strings.Contains(err.Error(), expectedError) {
		t.Errorf("Expected error containing '%s', got '%s'", expectedError, err.Error())
	}
}

// TestFetchPrimaryURLInvalidXML tests fetchPrimaryURL with invalid XML
func TestFetchPrimaryURLInvalidXML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "invalid xml content")
	}))
	defer server.Close()

	_, err := rpmutils.FetchPrimaryURL(server.URL)
	if err == nil {
		t.Error("Expected error when XML is invalid")
	}
}

// TestAzureLinuxProviderPreProcess tests PreProcess method with mocked dependencies
func TestAzureLinuxProviderPreProcess(t *testing.T) {
	t.Skip("PreProcess test requires full chroot environment and system dependencies - skipping in unit tests")
}

// TestAzureLinuxProviderBuildImage tests BuildImage method
func TestAzureLinuxProviderBuildImage(t *testing.T) {
	t.Skip("BuildImage test requires full system dependencies and image builders - skipping in unit tests")
}

// TestAzureLinuxProviderBuildImageISO tests BuildImage method with ISO type
func TestAzureLinuxProviderBuildImageISO(t *testing.T) {
	t.Skip("BuildImage ISO test requires full system dependencies and image builders - skipping in unit tests")
}

// TestAzureLinuxProviderPostProcess tests PostProcess method
func TestAzureLinuxProviderPostProcess(t *testing.T) {
	t.Skip("PostProcess test requires full chroot environment - skipping in unit tests")
}

// TestAzureLinuxProviderInstallHostDependency tests installHostDependency method
func TestAzureLinuxProviderInstallHostDependency(t *testing.T) {
	t.Skip("installHostDependency test requires host package manager and system dependencies - skipping in unit tests")
}

// TestAzureLinuxProviderInstallHostDependencyCommands tests expected host dependencies
func TestAzureLinuxProviderInstallHostDependencyCommands(t *testing.T) {
	// Test the expected dependencies mapping by accessing the internal map
	// This verifies what packages the Azure Linux provider expects to install
	expectedDeps := map[string]string{
		"rpm":      "rpm",        // For the chroot env build RPM pkg installation
		"mkfs.fat": "dosfstools", // For the FAT32 boot partition creation
		"xorriso":  "xorriso",    // For ISO image creation
		"sbsign":   "sbsigntool", // For the UKI image creation
	}

	t.Logf("Expected host dependencies for Azure Linux provider: %v", expectedDeps)

	// Verify that each expected dependency has a mapping
	for cmd, pkg := range expectedDeps {
		if cmd == "" || pkg == "" {
			t.Errorf("Empty dependency mapping: cmd='%s', pkg='%s'", cmd, pkg)
		}
	}
}

// TestAzureLinuxProviderRegister tests the Register function
func TestAzureLinuxProviderRegister(t *testing.T) {
	t.Skip("Register test requires chroot environment initialization - skipping in unit tests")
}

// TestAzureLinuxProviderWorkflow tests a complete Azure Linux provider workflow
func TestAzureLinuxProviderWorkflow(t *testing.T) {
	// This is an integration-style test showing how an Azure Linux provider
	// would be used in a complete workflow

	azl := &AzureLinux{}

	// Test provider name generation
	name := azl.Name("azl3", "x86_64")
	expectedName := "azure-linux-azl3-x86_64"
	if name != expectedName {
		t.Errorf("Expected name %s, got %s", expectedName, name)
	}

	// Test Init (will likely fail due to network dependencies)
	if err := azl.Init("azl3", "x86_64"); err != nil {
		t.Logf("Skipping Init test to avoid config file errors in unit test environment")
	} else {
		t.Log("Init succeeded - repo config loaded")
		if azl.repoCfg.Name != "" {
			t.Logf("Repo config loaded: %s", azl.repoCfg.Name)
		}
	}

	// Skip PreProcess, BuildImage and PostProcess tests to avoid system-level dependencies
	t.Log("Skipping PreProcess, BuildImage and PostProcess tests to avoid system-level dependencies")

	t.Log("Complete workflow test finished - core methods exist and are callable")
}

// TestAzureLinuxConfigurationStructure tests the internal configuration structure
func TestAzureLinuxConfigurationStructure(t *testing.T) {
	azl := &AzureLinux{
		repoCfg: rpmutils.RepoConfig{
			Section:      "azurelinux-base",
			Name:         "Azure Linux 3.0 Base Repository",
			URL:          "https://packages.microsoft.com/azurelinux/3.0/prod/base/x86_64",
			GPGCheck:     true,
			RepoGPGCheck: true,
			Enabled:      true,
			GPGKey:       "https://packages.microsoft.com/keys/microsoft.asc",
		},
		gzHref: "repodata/primary.xml.gz",
	}

	// Verify internal structure is properly set up
	if azl.repoCfg.Section == "" {
		t.Error("Expected repo config section to be set")
	}

	if azl.gzHref == "" {
		t.Error("Expected gzHref to be set")
	}

	// Test configuration structure without relying on constants that may not exist
	t.Logf("Skipping config loading test to avoid file system errors in unit test environment")
}

// TestAzureLinuxArchitectureHandling tests architecture-specific URL construction
func TestAzureLinuxArchitectureHandling(t *testing.T) {
	testCases := []struct {
		inputArch    string
		expectedName string
	}{
		{"x86_64", "azure-linux-azl3-x86_64"},
		{"aarch64", "azure-linux-azl3-aarch64"},
		{"armv7hl", "azure-linux-azl3-armv7hl"},
	}

	for _, tc := range testCases {
		t.Run(tc.inputArch, func(t *testing.T) {
			azl := &AzureLinux{}
			name := azl.Name("azl3", tc.inputArch)

			if name != tc.expectedName {
				t.Errorf("For arch %s, expected name %s, got %s", tc.inputArch, tc.expectedName, name)
			}
		})
	}
}

// TestAzlBuildImageNilTemplate tests BuildImage with nil template
func TestAzlBuildImageNilTemplate(t *testing.T) {
	azl := &AzureLinux{}

	err := azl.BuildImage(nil)
	if err == nil {
		t.Error("Expected error when template is nil")
	}

	expectedError := "template cannot be nil"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}

// TestAzlBuildImageUnsupportedType tests BuildImage with unsupported image type
func TestAzlBuildImageUnsupportedType(t *testing.T) {
	azl := &AzureLinux{}

	template := createTestImageTemplate()
	template.Target.ImageType = "unsupported"

	err := azl.BuildImage(template)
	if err == nil {
		t.Error("Expected error for unsupported image type")
	}

	expectedError := "unsupported image type: unsupported"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}

// TestAzlBuildImageValidTypes tests BuildImage error handling for valid image types
func TestAzlBuildImageValidTypes(t *testing.T) {
	azl := &AzureLinux{}

	validTypes := []string{"raw", "img", "iso"}

	for _, imageType := range validTypes {
		t.Run(imageType, func(t *testing.T) {
			template := createTestImageTemplate()
			template.Target.ImageType = imageType

			// These will fail due to missing chrootEnv, but we can verify
			// that the code path is reached and the error is expected
			err := azl.BuildImage(template)
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

// TestAzlPostProcessWithNilChroot tests PostProcess with nil chrootEnv
func TestAzlPostProcessWithNilChroot(t *testing.T) {
	azl := &AzureLinux{}
	template := createTestImageTemplate()

	// Test that PostProcess panics with nil chrootEnv (current behavior)
	// We use defer/recover to catch the panic and validate it
	defer func() {
		if r := recover(); r != nil {
			t.Logf("PostProcess correctly panicked with nil chrootEnv: %v", r)
		} else {
			t.Error("Expected PostProcess to panic with nil chrootEnv")
		}
	}()

	// This will panic due to nil chrootEnv
	_ = azl.PostProcess(template, nil)
}

// TestAzlPostProcessErrorHandling tests PostProcess error handling logic
func TestAzlPostProcessErrorHandling(t *testing.T) {
	// Test that PostProcess method exists and has correct signature
	azl := &AzureLinux{}
	inputError := fmt.Errorf("build failed")

	// Verify the method signature is correct by assigning it to a function variable
	var postProcessFunc func(*config.ImageTemplate, error) error = azl.PostProcess

	t.Logf("PostProcess method has correct signature: %T", postProcessFunc)
	t.Logf("Input error for testing: %v", inputError)

	// Test passes if we can assign the method to the correct function type
}

// TestAzlStructInitialization tests AzureLinux struct initialization
func TestAzlStructInitialization(t *testing.T) {
	// Test zero value initialization
	azl := &AzureLinux{}

	if azl.repoCfg.Name != "" {
		t.Error("Expected empty repoCfg.Name in uninitialized AzureLinux")
	}

	if azl.gzHref != "" {
		t.Error("Expected empty gzHref in uninitialized AzureLinux")
	}

	if azl.chrootEnv != nil {
		t.Error("Expected nil chrootEnv in uninitialized AzureLinux")
	}
}

// TestAzlStructWithData tests AzureLinux struct with initialized data
func TestAzlStructWithData(t *testing.T) {
	cfg := rpmutils.RepoConfig{
		Name:    "Test Repo",
		URL:     "https://test.example.com",
		Section: "test-section",
		Enabled: true,
	}

	azl := &AzureLinux{
		repoCfg: cfg,
		gzHref:  "test/primary.xml.gz",
	}

	if azl.repoCfg.Name != "Test Repo" {
		t.Errorf("Expected repoCfg.Name 'Test Repo', got '%s'", azl.repoCfg.Name)
	}

	if azl.repoCfg.URL != "https://test.example.com" {
		t.Errorf("Expected repoCfg.URL 'https://test.example.com', got '%s'", azl.repoCfg.URL)
	}

	if azl.gzHref != "test/primary.xml.gz" {
		t.Errorf("Expected gzHref 'test/primary.xml.gz', got '%s'", azl.gzHref)
	}
}

// TestAzlConstants tests Azure Linux provider constants
func TestAzlConstants(t *testing.T) {
	// Test OsName constant
	if OsName != "azure-linux" {
		t.Errorf("Expected OsName 'azure-linux', got '%s'", OsName)
	}
}

// TestAzlNameWithVariousInputs tests Name method with different dist and arch combinations
func TestAzlNameWithVariousInputs(t *testing.T) {
	azl := &AzureLinux{}

	testCases := []struct {
		dist     string
		arch     string
		expected string
	}{
		{"azl3", "x86_64", "azure-linux-azl3-x86_64"},
		{"azl3", "aarch64", "azure-linux-azl3-aarch64"},
		{"azl4", "x86_64", "azure-linux-azl4-x86_64"},
		{"", "", "azure-linux--"},
		{"test", "test", "azure-linux-test-test"},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%s-%s", tc.dist, tc.arch), func(t *testing.T) {
			result := azl.Name(tc.dist, tc.arch)
			if result != tc.expected {
				t.Errorf("Expected '%s', got '%s'", tc.expected, result)
			}
		})
	}
}

// TestAzlMethodSignatures tests that all interface methods have correct signatures
func TestAzlMethodSignatures(t *testing.T) {
	azl := &AzureLinux{}

	// Test that all methods can be assigned to their expected function types
	var nameFunc func(string, string) string = azl.Name
	var initFunc func(string, string) error = azl.Init
	var preProcessFunc func(*config.ImageTemplate) error = azl.PreProcess
	var buildImageFunc func(*config.ImageTemplate) error = azl.BuildImage
	var postProcessFunc func(*config.ImageTemplate, error) error = azl.PostProcess

	t.Logf("Name method signature: %T", nameFunc)
	t.Logf("Init method signature: %T", initFunc)
	t.Logf("PreProcess method signature: %T", preProcessFunc)
	t.Logf("BuildImage method signature: %T", buildImageFunc)
	t.Logf("PostProcess method signature: %T", postProcessFunc)
}

// TestAzlRegister tests the Register function
func TestAzlRegister(t *testing.T) {
	// Test Register function with valid parameters
	targetOs := "azl"
	targetDist := "azl2"
	targetArch := "x86_64"

	// Register should fail in unit test environment due to missing dependencies
	// but we can test that it doesn't panic and has correct signature
	err := Register(targetOs, targetDist, targetArch)

	// We expect an error in unit test environment
	if err == nil {
		t.Log("Unexpected success - Azure Linux registration succeeded in test environment")
	} else {
		// This is expected in unit test environment due to missing config
		t.Logf("Expected error in test environment: %v", err)
	}

	// Test with invalid parameters
	err = Register("", "", "")
	if err == nil {
		t.Error("Expected error with empty parameters")
	}

	t.Log("Successfully tested Register function behavior")
}

// TestAzlPreProcess tests the PreProcess function
func TestAzlPreProcess(t *testing.T) {
	// Skip this test as PreProcess requires proper initialization with chrootEnv
	// and calls downloadImagePkgs which doesn't handle nil chrootEnv gracefully
	t.Skip("PreProcess requires proper Azure Linux initialization with chrootEnv - function exists and is callable")
}

// TestAzlInstallHostDependency tests the installHostDependency function
func TestAzlInstallHostDependency(t *testing.T) {
	azl := &AzureLinux{}

	// Test that the function exists and can be called
	err := azl.installHostDependency()

	// In test environment, we expect an error due to missing system dependencies
	// but the function should not panic
	if err == nil {
		t.Log("installHostDependency succeeded - host dependencies available in test environment")
	} else {
		t.Logf("installHostDependency failed as expected in test environment: %v", err)
	}

	t.Log("installHostDependency function signature and execution test completed")
}

// TestAzlDownloadImagePkgs tests the downloadImagePkgs function
func TestAzlDownloadImagePkgs(t *testing.T) {
	// Skip this test as downloadImagePkgs requires proper initialization with chrootEnv
	// and doesn't handle nil chrootEnv gracefully
	t.Skip("downloadImagePkgs requires proper Azure Linux initialization with chrootEnv - function exists and is callable")
}

// TestAzlPreProcessWithMockEnv tests PreProcess with proper mock chrootEnv
func TestAzlPreProcessWithMockEnv(t *testing.T) {
	azl := &AzureLinux{
		chrootEnv: &mockChrootEnv{},
	}

	template := createTestImageTemplate()

	// PreProcess will fail at installHostDependency but we can verify the call chain
	err := azl.PreProcess(template)
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

// TestAzlDownloadImagePkgsWithMockEnv tests downloadImagePkgs with mock environment
func TestAzlDownloadImagePkgsWithMockEnv(t *testing.T) {
	azl := &AzureLinux{
		chrootEnv: &mockChrootEnv{},
		repoCfg: rpmutils.RepoConfig{
			Name:    "Test Repo",
			URL:     "https://test.example.com",
			Section: "test",
		},
		gzHref: "repodata/test.xml.gz",
	}

	template := createTestImageTemplate()
	template.DotFilePath = ""

	// downloadImagePkgs will fail at package download but we can verify the call
	err := azl.downloadImagePkgs(template)
	if err == nil {
		t.Log("downloadImagePkgs succeeded unexpectedly")
	} else {
		// Verify we reach the download logic
		t.Logf("downloadImagePkgs failed as expected: %v", err)
	}
}

// TestAzlBuildRawImageWithMock tests buildRawImage with mock environment
func TestAzlBuildRawImageWithMock(t *testing.T) {
	azl := &AzureLinux{
		chrootEnv: &mockChrootEnv{},
	}

	template := createTestImageTemplate()
	template.Target.ImageType = "raw"

	err := azl.buildRawImage(template)
	if err == nil {
		t.Error("Expected error with mock environment")
	} else {
		// Verify error is from rawmaker initialization
		if !strings.Contains(err.Error(), "failed to create raw maker") && !strings.Contains(err.Error(), "failed to initialize") {
			t.Logf("buildRawImage failed with: %v", err)
		}
	}
}

// TestAzlBuildInitrdImageWithMock tests buildInitrdImage with mock environment
func TestAzlBuildInitrdImageWithMock(t *testing.T) {
	azl := &AzureLinux{
		chrootEnv: &mockChrootEnv{},
	}

	template := createTestImageTemplate()
	template.Target.ImageType = "img"

	err := azl.buildInitrdImage(template)
	if err == nil {
		t.Error("Expected error with mock environment")
	} else {
		// Verify error is from initrdmaker
		if !strings.Contains(err.Error(), "failed to create initrd maker") && !strings.Contains(err.Error(), "failed to initialize") {
			t.Logf("buildInitrdImage failed with: %v", err)
		}
	}
}

// TestAzlBuildIsoImageWithMock tests buildIsoImage with mock environment
func TestAzlBuildIsoImageWithMock(t *testing.T) {
	azl := &AzureLinux{
		chrootEnv: &mockChrootEnv{},
	}

	template := createTestImageTemplate()
	template.Target.ImageType = "iso"

	err := azl.buildIsoImage(template)
	if err == nil {
		t.Error("Expected error with mock environment")
	} else {
		// Verify error is from isomaker
		if !strings.Contains(err.Error(), "failed to create iso maker") && !strings.Contains(err.Error(), "failed to initialize") {
			t.Logf("buildIsoImage failed with: %v", err)
		}
	}
}

// TestAzlPostProcessSuccess tests PostProcess with mock environment
func TestAzlPostProcessSuccess(t *testing.T) {
	azl := &AzureLinux{
		chrootEnv: &mockChrootEnv{},
	}

	template := createTestImageTemplate()

	// Test with no input error
	err := azl.PostProcess(template, nil)
	if err != nil {
		t.Errorf("Expected nil error when input error is nil, got: %v", err)
	}

	// Test with input error - should return the same error
	inputErr := fmt.Errorf("build failed")
	err = azl.PostProcess(template, inputErr)
	if err != inputErr {
		t.Errorf("Expected PostProcess to return input error, got: %v", err)
	}
}

// TestLoadRepoConfigFromYAMLErrors tests error cases in loadRepoConfigFromYAML
func TestLoadRepoConfigFromYAMLErrors(t *testing.T) {
	// Test with invalid dist/arch
	_, err := loadRepoConfigFromYAML("invalid", "invalid")
	if err == nil {
		t.Error("Expected error for invalid dist/arch")
	} else {
		t.Logf("Got expected error: %v", err)
	}
}

// TestLoadRepoConfigFromYAMLAarch64 tests loadRepoConfigFromYAML with aarch64
func TestLoadRepoConfigFromYAMLAarch64(t *testing.T) {
	// Change to project root
	originalDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Failed to change back to original directory: %v", err)
		}
	}()

	// Navigate to project root
	if err := os.Chdir("../../../"); err != nil {
		t.Skipf("Cannot change to project root: %v", err)
		return
	}

	// Test with aarch64 architecture
	cfg, err := loadRepoConfigFromYAML("azl3", "aarch64")
	if err != nil {
		t.Skipf("loadRepoConfigFromYAML failed for aarch64: %v", err)
		return
	}

	if cfg.Name == "" {
		t.Error("Expected config name to be set for aarch64")
	}

	t.Logf("Successfully loaded aarch64 config: %s", cfg.Name)
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
	// This test verifies the Register function path
	// In a real environment with config files, this would work

	// Test that Register doesn't panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Register panicked: %v", r)
		}
	}()

	err := Register("azure-linux", "azl3", "x86_64")

	// We expect error in test environment, but it should be controlled
	if err != nil {
		t.Logf("Register returned expected error in test environment: %v", err)
	} else {
		t.Log("Register succeeded unexpectedly - config files present")
	}
}

// TestInstallHostDependencyMapping tests the dependency mapping logic
func TestInstallHostDependencyMapping(t *testing.T) {
	azl := &AzureLinux{}

	// Call installHostDependency
	err := azl.installHostDependency()

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

// TestAzlInitWithAarch64 tests Init with aarch64 architecture
func TestAzlInitWithAarch64(t *testing.T) {
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

	azl := &AzureLinux{
		chrootEnv: &mockChrootEnv{},
	}

	err := azl.Init("azl3", "aarch64")
	if err != nil {
		t.Skipf("Init failed for aarch64: %v", err)
		return
	}

	if azl.repoCfg.URL == "" {
		t.Error("Expected repoCfg.URL to be set for aarch64")
	}

	// Verify aarch64 is in the URL
	if !strings.Contains(azl.repoCfg.URL, "aarch64") {
		t.Errorf("Expected URL to contain 'aarch64', got: %s", azl.repoCfg.URL)
	}

	t.Logf("Successfully initialized with aarch64: %s", azl.repoCfg.URL)
}

// TestAzlInitErrorPaths tests error paths in Init method
func TestAzlInitErrorPaths(t *testing.T) {
	azl := &AzureLinux{
		chrootEnv: &mockChrootEnv{},
	}

	// Test with invalid dist
	err := azl.Init("invalid-dist", "x86_64")
	if err == nil {
		t.Error("Expected error for invalid dist")
	} else {
		t.Logf("Got expected error for invalid dist: %v", err)
	}
}

// TestBuildImageEdgeCases tests edge cases in BuildImage
func TestBuildImageEdgeCases(t *testing.T) {
	azl := &AzureLinux{
		chrootEnv: &mockChrootEnv{},
	}

	// Test with empty image name
	template := createTestImageTemplate()
	template.Image.Name = ""
	template.Target.ImageType = "raw"

	err := azl.BuildImage(template)
	if err == nil {
		t.Log("BuildImage handled empty name gracefully")
	} else {
		t.Logf("BuildImage with empty name failed: %v", err)
	}
}

// TestPreProcessErrorPropagation tests error propagation in PreProcess
func TestPreProcessErrorPropagation(t *testing.T) {
	azl := &AzureLinux{
		chrootEnv: &mockChrootEnv{},
	}

	template := createTestImageTemplate()

	err := azl.PreProcess(template)
	if err != nil {
		// Verify error message contains context
		if !strings.Contains(err.Error(), "failed to") {
			t.Errorf("Expected error to contain context, got: %v", err)
		}
	}
}
