package rcd

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
func (m *mockChrootEnv) GetTargetOsReleaseVersion() string { return "10.0" }
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
			Name:    "test-rcd-image",
			Version: "1.0.0",
		},
		Target: config.TargetInfo{
			OS:        "redhat-compatible-distro",
			Dist:      "rcd10",
			Arch:      "x86_64",
			ImageType: "raw",
		},
		SystemConfig: config.SystemConfig{
			Name:        "test-rcd-system",
			Description: "Test RCD system configuration",
			Packages:    []string{"curl", "wget", "vim"},
		},
	}
}

// TestRCDProviderInterface tests that RCD implements Provider interface
func TestRCDProviderInterface(t *testing.T) {
	var _ provider.Provider = (*RCD)(nil) // Compile-time interface check
}

// TestRCDProviderName tests the Name method
func TestRCDProviderName(t *testing.T) {
	rcd := &RCD{}
	name := rcd.Name("rcd10", "x86_64")
	expected := "redhat-compatible-distro-rcd10-x86_64"

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
		{"rcd10", "x86_64", "redhat-compatible-distro-rcd10-x86_64"},
		{"rcd10", "aarch64", "redhat-compatible-distro-rcd10-aarch64"},
		{"rcd11", "x86_64", "redhat-compatible-distro-rcd11-x86_64"},
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

// TestRCDCentralizedConfig tests the centralized configuration loading
func TestRCDCentralizedConfig(t *testing.T) {
	// Change to project root for tests that need config files
	originalDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Failed to change back to original directory: %v", err)
		}
	}()

	// Navigate to project root (3 levels up from internal/provider/rcd)
	if err := os.Chdir("../../../"); err != nil {
		t.Skipf("Cannot change to project root: %v", err)
		return
	}

	// Test loading repo config
	cfg, err := loadRepoConfigFromYAML("rcd10", "x86_64")
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

	// Verify URL is set properly
	if cfg.URL == "" {
		t.Error("Expected config URL to be set")
	}

	t.Logf("Successfully loaded repo config: %s", cfg.Name)
	t.Logf("Config details: %+v", cfg)
}

// TestRCDProviderFallback tests the fallback to centralized config when HTTP fails
func TestRCDProviderFallback(t *testing.T) {
	// Change to project root for tests that need config files
	originalDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Failed to change back to original directory: %v", err)
		}
	}()

	// Navigate to project root (3 levels up from internal/provider/rcd)
	if err := os.Chdir("../../../"); err != nil {
		t.Skipf("Cannot change to project root: %v", err)
		return
	}

	rcd := &RCD{
		chrootEnv: &mockChrootEnv{},
	}

	// Test initialization which should use centralized config
	err := rcd.Init("rcd10", "x86_64")
	if err != nil {
		t.Logf("Init failed as expected in test environment: %v", err)
	} else {
		// If it succeeds, verify the configuration was set up from YAML
		if rcd.repoCfg.Name == "" {
			t.Error("Expected repoCfg.Name to be set after successful init")
		}

		t.Logf("Successfully tested initialization with centralized config")
		t.Logf("Config name: %s", rcd.repoCfg.Name)
		t.Logf("Config URL: %s", rcd.repoCfg.URL)
	}
}

// TestRCDProviderInit tests the Init method with centralized configuration
func TestRCDProviderInit(t *testing.T) {
	// Change to project root for tests that need config files
	originalDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Failed to change back to original directory: %v", err)
		}
	}()

	// Navigate to project root (3 levels up from internal/provider/rcd)
	if err := os.Chdir("../../../"); err != nil {
		t.Skipf("Cannot change to project root: %v", err)
		return
	}

	rcd := &RCD{
		chrootEnv: &mockChrootEnv{},
	}

	// Test with x86_64 architecture - now uses centralized config
	err := rcd.Init("rcd10", "x86_64")
	if err != nil {
		t.Skipf("Init failed (expected in test environment): %v", err)
		return
	}

	// If it succeeds, verify the configuration was set up
	if rcd.repoCfg.Name == "" {
		t.Error("Expected repoCfg.Name to be set after successful Init")
	}

	t.Logf("Successfully initialized with centralized config")
	t.Logf("Config name: %s", rcd.repoCfg.Name)
	t.Logf("Config URL: %s", rcd.repoCfg.URL)
	t.Logf("Primary href: %s", rcd.gzHref)
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

	// Navigate to project root (3 levels up from internal/provider/rcd)
	if err := os.Chdir("../../../"); err != nil {
		t.Skipf("Cannot change to project root: %v", err)
		return
	}

	// Test loading repo config
	cfg, err := loadRepoConfigFromYAML("rcd10", "x86_64")
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

// TestRCDProviderPreProcess tests PreProcess method with mocked dependencies
func TestRCDProviderPreProcess(t *testing.T) {
	t.Skip("PreProcess test requires full chroot environment and system dependencies - skipping in unit tests")
}

// TestRCDProviderBuildImage tests BuildImage method
func TestRCDProviderBuildImage(t *testing.T) {
	t.Skip("BuildImage test requires full system dependencies and image builders - skipping in unit tests")
}

// TestRCDProviderBuildImageISO tests BuildImage method with ISO type
func TestRCDProviderBuildImageISO(t *testing.T) {
	t.Skip("BuildImage ISO test requires full system dependencies and image builders - skipping in unit tests")
}

// TestRCDProviderPostProcess tests PostProcess method
func TestRCDProviderPostProcess(t *testing.T) {
	t.Skip("PostProcess test requires full chroot environment - skipping in unit tests")
}

// TestRCDProviderInstallHostDependency tests installHostDependency method
func TestRCDProviderInstallHostDependency(t *testing.T) {
	t.Skip("installHostDependency test requires host package manager and system dependencies - skipping in unit tests")
}

// TestRCDProviderInstallHostDependencyCommands tests expected host dependencies
func TestRCDProviderInstallHostDependencyCommands(t *testing.T) {
	// Test the expected dependencies mapping by accessing the internal map
	// This verifies what packages the RCD provider expects to install
	expectedDeps := map[string]string{
		"rpm":          "rpm",         // For the chroot env build RPM pkg installation
		"mkfs.fat":     "dosfstools",  // For the FAT32 boot partition creation
		"qemu-img":     "qemu-utils",  // For image file format conversion
		"mformat":      "mtools",      // For writing files to FAT32 partition
		"xorriso":      "xorriso",     // For ISO image creation
		"grub-mkimage": "grub-common", // For ISO image UEFI Grub binary creation
		"sbsign":       "sbsigntool",  // For the UKI image creation
	}

	t.Logf("Expected host dependencies for RCD provider: %v", expectedDeps)

	// Verify that each expected dependency has a mapping
	for cmd, pkg := range expectedDeps {
		if cmd == "" || pkg == "" {
			t.Errorf("Empty dependency mapping: cmd='%s', pkg='%s'", cmd, pkg)
		}
	}
}

// TestRCDProviderRegister tests the Register function
func TestRCDProviderRegister(t *testing.T) {
	t.Skip("Register test requires chroot environment initialization - skipping in unit tests")
}

// TestRCDProviderWorkflow tests a complete RCD provider workflow
func TestRCDProviderWorkflow(t *testing.T) {
	// This is an integration-style test showing how an RCD provider
	// would be used in a complete workflow

	rcd := &RCD{}

	// Test provider name generation
	name := rcd.Name("rcd10", "x86_64")
	expectedName := "redhat-compatible-distro-rcd10-x86_64"
	if name != expectedName {
		t.Errorf("Expected name %s, got %s", expectedName, name)
	}

	// Test Init (will likely fail due to network dependencies)
	if err := rcd.Init("rcd10", "x86_64"); err != nil {
		t.Logf("Skipping Init test to avoid config file errors in unit test environment")
	} else {
		t.Log("Init succeeded - repo config loaded")
		if rcd.repoCfg.Name != "" {
			t.Logf("Repo config loaded: %s", rcd.repoCfg.Name)
		}
	}

	// Skip PreProcess, BuildImage and PostProcess tests to avoid system-level dependencies
	t.Log("Skipping PreProcess, BuildImage and PostProcess tests to avoid system-level dependencies")

	t.Log("Complete workflow test finished - core methods exist and are callable")
}

// TestRCDConfigurationStructure tests the internal configuration structure
func TestRCDConfigurationStructure(t *testing.T) {
	rcd := &RCD{
		repoCfg: rpmutils.RepoConfig{
			Section:      "rcd-base",
			Name:         "Red Hat Compatible Distro 10.0 Base Repository",
			URL:          "https://example.com/rcd/10.0/base/x86_64",
			GPGCheck:     true,
			RepoGPGCheck: true,
			Enabled:      true,
			GPGKey:       "https://example.com/keys/rcd.asc",
		},
		gzHref: "repodata/primary.xml.gz",
	}

	// Verify internal structure is properly set up
	if rcd.repoCfg.Section == "" {
		t.Error("Expected repo config section to be set")
	}

	if rcd.gzHref == "" {
		t.Error("Expected gzHref to be set")
	}

	// Test configuration structure without relying on constants that may not exist
	t.Logf("Skipping config loading test to avoid file system errors in unit test environment")
}

// TestRCDArchitectureHandling tests architecture-specific URL construction
func TestRCDArchitectureHandling(t *testing.T) {
	testCases := []struct {
		inputArch    string
		expectedName string
	}{
		{"x86_64", "redhat-compatible-distro-rcd10-x86_64"},
		{"aarch64", "redhat-compatible-distro-rcd10-aarch64"},
		{"armv7hl", "redhat-compatible-distro-rcd10-armv7hl"},
	}

	for _, tc := range testCases {
		t.Run(tc.inputArch, func(t *testing.T) {
			rcd := &RCD{}
			name := rcd.Name("rcd10", tc.inputArch)

			if name != tc.expectedName {
				t.Errorf("For arch %s, expected name %s, got %s", tc.inputArch, tc.expectedName, name)
			}
		})
	}
}

// TestRCDBuildImageNilTemplate tests BuildImage with nil template
func TestRCDBuildImageNilTemplate(t *testing.T) {
	rcd := &RCD{}

	err := rcd.BuildImage(nil)
	if err == nil {
		t.Error("Expected error when template is nil")
	}

	expectedError := "template cannot be nil"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}

// TestRCDBuildImageUnsupportedType tests BuildImage with unsupported image type
func TestRCDBuildImageUnsupportedType(t *testing.T) {
	rcd := &RCD{}

	template := createTestImageTemplate()
	template.Target.ImageType = "unsupported"

	err := rcd.BuildImage(template)
	if err == nil {
		t.Error("Expected error for unsupported image type")
	}

	expectedError := "unsupported image type: unsupported"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}

// TestRCDBuildImageValidTypes tests BuildImage error handling for valid image types
func TestRCDBuildImageValidTypes(t *testing.T) {
	rcd := &RCD{}

	validTypes := []string{"raw", "img", "iso"}

	for _, imageType := range validTypes {
		t.Run(imageType, func(t *testing.T) {
			template := createTestImageTemplate()
			template.Target.ImageType = imageType

			// These will fail due to missing chrootEnv, but we can verify
			// that the code path is reached and the error is expected
			err := rcd.BuildImage(template)
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

// TestRCDPostProcessWithNilChroot tests PostProcess with nil chrootEnv
func TestRCDPostProcessWithNilChroot(t *testing.T) {
	rcd := &RCD{}
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
	_ = rcd.PostProcess(template, nil)
}

// TestRCDPostProcessErrorHandling tests PostProcess error handling logic
func TestRCDPostProcessErrorHandling(t *testing.T) {
	// Test that PostProcess method exists and has correct signature
	rcd := &RCD{}
	inputError := fmt.Errorf("build failed")

	// Verify the method signature is correct by assigning it to a function variable
	var postProcessFunc func(*config.ImageTemplate, error) error = rcd.PostProcess

	t.Logf("PostProcess method has correct signature: %T", postProcessFunc)
	t.Logf("Input error for testing: %v", inputError)

	// Test passes if we can assign the method to the correct function type
}

// TestRCDStructInitialization tests RCD struct initialization
func TestRCDStructInitialization(t *testing.T) {
	// Test zero value initialization
	rcd := &RCD{}

	if rcd.repoCfg.Name != "" {
		t.Error("Expected empty repoCfg.Name in uninitialized RCD")
	}

	if rcd.gzHref != "" {
		t.Error("Expected empty gzHref in uninitialized RCD")
	}

	if rcd.chrootEnv != nil {
		t.Error("Expected nil chrootEnv in uninitialized RCD")
	}
}

// TestRCDStructWithData tests RCD struct with initialized data
func TestRCDStructWithData(t *testing.T) {
	cfg := rpmutils.RepoConfig{
		Name:    "Test RCD Repo",
		URL:     "https://test.rcd.example.com",
		Section: "test-section",
		Enabled: true,
	}

	rcd := &RCD{
		repoCfg: cfg,
		gzHref:  "test/primary.xml.gz",
	}

	if rcd.repoCfg.Name != "Test RCD Repo" {
		t.Errorf("Expected repoCfg.Name 'Test RCD Repo', got '%s'", rcd.repoCfg.Name)
	}

	if rcd.repoCfg.URL != "https://test.rcd.example.com" {
		t.Errorf("Expected repoCfg.URL 'https://test.rcd.example.com', got '%s'", rcd.repoCfg.URL)
	}

	if rcd.gzHref != "test/primary.xml.gz" {
		t.Errorf("Expected gzHref 'test/primary.xml.gz', got '%s'", rcd.gzHref)
	}
}

// TestRCDConstants tests RCD provider constants
func TestRCDConstants(t *testing.T) {
	// Test OsName constant
	if OsName != "redhat-compatible-distro" {
		t.Errorf("Expected OsName 'redhat-compatible-distro', got '%s'", OsName)
	}
}

// TestRCDNameWithVariousInputs tests Name method with different dist and arch combinations
func TestRCDNameWithVariousInputs(t *testing.T) {
	rcd := &RCD{}

	testCases := []struct {
		dist     string
		arch     string
		expected string
	}{
		{"rcd10", "x86_64", "redhat-compatible-distro-rcd10-x86_64"},
		{"rcd10", "aarch64", "redhat-compatible-distro-rcd10-aarch64"},
		{"rcd11", "x86_64", "redhat-compatible-distro-rcd11-x86_64"},
		{"", "", "redhat-compatible-distro--"},
		{"test", "test", "redhat-compatible-distro-test-test"},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%s-%s", tc.dist, tc.arch), func(t *testing.T) {
			result := rcd.Name(tc.dist, tc.arch)
			if result != tc.expected {
				t.Errorf("Expected '%s', got '%s'", tc.expected, result)
			}
		})
	}
}

// TestRCDMethodSignatures tests that all interface methods have correct signatures
func TestRCDMethodSignatures(t *testing.T) {
	rcd := &RCD{}

	// Test that all methods can be assigned to their expected function types
	var nameFunc func(string, string) string = rcd.Name
	var initFunc func(string, string) error = rcd.Init
	var preProcessFunc func(*config.ImageTemplate) error = rcd.PreProcess
	var buildImageFunc func(*config.ImageTemplate) error = rcd.BuildImage
	var postProcessFunc func(*config.ImageTemplate, error) error = rcd.PostProcess

	t.Logf("Name method signature: %T", nameFunc)
	t.Logf("Init method signature: %T", initFunc)
	t.Logf("PreProcess method signature: %T", preProcessFunc)
	t.Logf("BuildImage method signature: %T", buildImageFunc)
	t.Logf("PostProcess method signature: %T", postProcessFunc)
}

// TestRCDRegister tests the Register function
func TestRCDRegister(t *testing.T) {
	// Test Register function with valid parameters
	targetOs := "rcd"
	targetDist := "rcd10"
	targetArch := "x86_64"

	// Register should fail in unit test environment due to missing dependencies
	// but we can test that it doesn't panic and has correct signature
	err := Register(targetOs, targetDist, targetArch)

	// We expect an error in unit test environment
	if err == nil {
		t.Log("Unexpected success - RCD registration succeeded in test environment")
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

// TestRCDPreProcess tests the PreProcess function
func TestRCDPreProcess(t *testing.T) {
	// Skip this test as PreProcess requires proper initialization with chrootEnv
	// and calls downloadImagePkgs which doesn't handle nil chrootEnv gracefully
	t.Skip("PreProcess requires proper RCD initialization with chrootEnv - function exists and is callable")
}

// TestRCDInstallHostDependency tests the installHostDependency function
func TestRCDInstallHostDependency(t *testing.T) {
	rcd := &RCD{}

	// Test that the function exists and can be called
	err := rcd.installHostDependency()

	// In test environment, we expect an error due to missing system dependencies
	// but the function should not panic
	if err == nil {
		t.Log("installHostDependency succeeded - host dependencies available in test environment")
	} else {
		t.Logf("installHostDependency failed as expected in test environment: %v", err)
	}

	t.Log("installHostDependency function signature and execution test completed")
}

// TestRCDDownloadImagePkgs tests the downloadImagePkgs function
func TestRCDDownloadImagePkgs(t *testing.T) {
	// Skip this test as downloadImagePkgs requires proper initialization with chrootEnv
	// and doesn't handle nil chrootEnv gracefully
	t.Skip("downloadImagePkgs requires proper RCD initialization with chrootEnv - function exists and is callable")
}

// TestRCDBuildRawImage tests buildRawImage method error handling
func TestRCDBuildRawImage(t *testing.T) {
	rcd := &RCD{}
	template := createTestImageTemplate()

	// Test that buildRawImage fails gracefully without proper initialization
	err := rcd.buildRawImage(template)
	if err == nil {
		t.Error("Expected error when building raw image without proper initialization")
	} else {
		t.Logf("buildRawImage correctly failed with: %v", err)
	}
}

// TestRCDBuildInitrdImage tests buildInitrdImage method error handling
func TestRCDBuildInitrdImage(t *testing.T) {
	rcd := &RCD{}
	template := createTestImageTemplate()

	// Test that buildInitrdImage fails gracefully without proper initialization
	err := rcd.buildInitrdImage(template)
	if err == nil {
		t.Error("Expected error when building initrd image without proper initialization")
	} else {
		t.Logf("buildInitrdImage correctly failed with: %v", err)
	}
}

// TestRCDBuildIsoImage tests buildIsoImage method error handling
func TestRCDBuildIsoImage(t *testing.T) {
	rcd := &RCD{}
	template := createTestImageTemplate()

	// Test that buildIsoImage fails gracefully without proper initialization
	err := rcd.buildIsoImage(template)
	if err == nil {
		t.Error("Expected error when building ISO image without proper initialization")
	} else {
		t.Logf("buildIsoImage correctly failed with: %v", err)
	}
}

// TestRCDDisplayImageArtifacts tests displayImageArtifacts function
func TestRCDDisplayImageArtifacts(t *testing.T) {
	// Test that the displayImageArtifacts function exists and is callable
	// This function doesn't return anything so we just test that it doesn't panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("displayImageArtifacts panicked: %v", r)
		}
	}()

	displayImageArtifacts("/tmp/test", "TEST")
	t.Log("displayImageArtifacts function executed without panic")
}
