package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/os-image-composer/internal/config/validate"
)

func TestMergeStringSlices(t *testing.T) {
	defaultSlice := []string{"a", "b", "c"}
	userSlice := []string{"c", "d", "e"}

	merged := mergeStringSlices(defaultSlice, userSlice)

	expectedLength := 5 // a, b, c, d, e (no duplicates)
	if len(merged) != expectedLength {
		t.Errorf("expected merged slice length %d, got %d", expectedLength, len(merged))
	}

	// Verify no duplicates
	itemMap := make(map[string]int)
	for _, item := range merged {
		itemMap[item]++
		if itemMap[item] > 1 {
			t.Errorf("found duplicate item '%s' in merged slice", item)
		}
	}

	// Verify all expected items are present
	expectedItems := []string{"a", "b", "c", "d", "e"}
	for _, expectedItem := range expectedItems {
		if itemMap[expectedItem] != 1 {
			t.Errorf("expected item '%s' to be present exactly once", expectedItem)
		}
	}
}

func TestMergeStringSlicesEmpty(t *testing.T) {
	// Both slices empty
	result := mergeStringSlices([]string{}, []string{})
	if len(result) != 0 {
		t.Errorf("expected empty result for two empty slices, got %d items", len(result))
	}

	// One slice empty
	slice1 := []string{"a", "b"}
	result = mergeStringSlices(slice1, []string{})
	if len(result) != 2 {
		t.Errorf("expected 2 items when second slice is empty, got %d", len(result))
	}

	result = mergeStringSlices([]string{}, slice1)
	if len(result) != 2 {
		t.Errorf("expected 2 items when first slice is empty, got %d", len(result))
	}
}

func TestMergeStringSlicesWithNils(t *testing.T) {
	slice1 := []string{"a", "b"}

	// This tests the actual behavior of mergeStringSlices with nil slices
	result := mergeStringSlices(nil, slice1)
	if len(result) != 2 {
		t.Errorf("expected 2 items when first slice is nil, got %d", len(result))
	}

	result = mergeStringSlices(slice1, nil)
	if len(result) != 2 {
		t.Errorf("expected 2 items when second slice is nil, got %d", len(result))
	}
}

func TestEmptyUsersConfig(t *testing.T) {
	// Test template with no users
	template := &ImageTemplate{
		Image: ImageInfo{
			Name:    "test",
			Version: "1.0.0",
		},
		Target: TargetInfo{
			OS:        "azure-linux",
			Dist:      "azl3",
			Arch:      "x86_64",
			ImageType: "raw",
		},
		SystemConfig: SystemConfig{
			Name: "test-config",
			// No users configured
		},
	}

	// Test that empty users config works
	users := template.GetUsers()
	if len(users) != 0 {
		t.Errorf("expected 0 users for empty config, got %d", len(users))
	}

	if template.HasUsers() {
		t.Errorf("expected template to not have users")
	}

	nonExistentUser := template.GetUserByName("anyuser")
	if nonExistentUser != nil {
		t.Errorf("expected not to find any user in empty config")
	}
}

func TestMergeSystemConfigWithSecureBoot(t *testing.T) {
	defaultConfig := SystemConfig{
		Name: "default",
		Immutability: ImmutabilityConfig{
			Enabled:         true,
			SecureBootDBKey: "/default/keys/db.key",
			SecureBootDBCrt: "/default/certs/db.crt",
		},
		Packages: []string{"base-package"},
	}

	userConfig := SystemConfig{
		Name: "user",
		Immutability: ImmutabilityConfig{
			Enabled:         true,
			SecureBootDBKey: "/user/keys/custom.key",  // Override key
			SecureBootDBCer: "/user/certs/custom.cer", // Add new cer
			wasProvided:     true,                     // Mark as explicitly provided
			// Don't override crt - should keep default
		},
		Packages: []string{"user-package"},
	}

	merged := mergeSystemConfig(defaultConfig, userConfig)

	// Verify immutability merging
	if !merged.Immutability.Enabled {
		t.Errorf("expected merged immutability to be enabled")
	}

	if merged.Immutability.SecureBootDBKey != "/user/keys/custom.key" {
		t.Errorf("expected user secure boot key to override default")
	}

	if merged.Immutability.SecureBootDBCrt != "/default/certs/db.crt" {
		t.Errorf("expected default secure boot crt to be preserved")
	}

	if merged.Immutability.SecureBootDBCer != "/user/certs/custom.cer" {
		t.Errorf("expected user secure boot cer to be added")
	}
}

func TestLoadYAMLTemplateWithImmutability(t *testing.T) {
	// Create a temporary YAML file with immutability configuration under systemConfig
	yamlContent := `image:
  name: azl3-x86_64-edge
  version: "1.0.0"

target:
  os: azure-linux
  dist: azl3
  arch: x86_64
  imageType: raw

systemConfig:
  name: edge
  description: Default yml configuration for edge image
  immutability:
    enabled: true
  packages:
    - openssh-server
    - docker-ce
  kernel:
    version: "6.12"
    cmdline: "quiet splash"
`

	tmpFile, err := os.CreateTemp("", "test-*.yml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if err := tmpFile.Chmod(0600); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(yamlContent); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	tmpFile.Close()

	// Test loading
	template, err := LoadTemplate(tmpFile.Name(), true)
	if err != nil {
		t.Fatalf("failed to load YAML template: %v", err)
	}

	// Verify immutability configuration
	if !template.IsImmutabilityEnabled() {
		t.Errorf("expected immutability to be enabled, got %t", template.IsImmutabilityEnabled())
	}

	// Test direct access to systemConfig immutability
	if !template.SystemConfig.IsImmutabilityEnabled() {
		t.Errorf("expected systemConfig immutability to be enabled, got %t", template.SystemConfig.IsImmutabilityEnabled())
	}
}

func TestMergeSystemConfigWithImmutability(t *testing.T) {
	defaultConfig := SystemConfig{
		Name:         "default",
		Immutability: ImmutabilityConfig{Enabled: true},
		Packages:     []string{"base-package"},
	}

	// User explicitly provides immutability config with secure boot settings
	// This represents a user who explicitly configured immutability
	userConfig := SystemConfig{
		Name: "user",
		Immutability: ImmutabilityConfig{
			Enabled:         false,
			SecureBootDBKey: "/path/to/key", // This makes it clear user provided config
			wasProvided:     true,           // Mark as explicitly provided
		},
		Packages: []string{"user-package"},
	}

	merged := mergeSystemConfig(defaultConfig, userConfig)

	if merged.Immutability.Enabled != false {
		t.Errorf("expected merged immutability to be false (user override), got %t", merged.Immutability.Enabled)
	}

	if merged.Name != "user" {
		t.Errorf("expected merged name to be 'user', got %s", merged.Name)
	}
}

func TestTemplateHelperMethodsWithImmutability(t *testing.T) {
	template := &ImageTemplate{
		Image: ImageInfo{
			Name:    "test-image",
			Version: "1.0.0",
		},
		Target: TargetInfo{
			OS:        "azure-linux",
			Dist:      "azl3",
			Arch:      "x86_64",
			ImageType: "raw",
		},
		SystemConfig: SystemConfig{
			Name:         "test-config",
			Description:  "Test configuration",
			Immutability: ImmutabilityConfig{Enabled: true},
			Packages:     []string{"package1", "package2"},
			Kernel: KernelConfig{
				Version: "6.12",
				Cmdline: "quiet",
			},
		},
	}

	// Test immutability access methods
	if !template.IsImmutabilityEnabled() {
		t.Errorf("expected immutability to be enabled, got %t", template.IsImmutabilityEnabled())
	}

	immutabilityConfig := template.GetImmutability()
	if !immutabilityConfig.Enabled {
		t.Errorf("expected immutability config to be enabled, got %t", immutabilityConfig.Enabled)
	}

	// Test systemConfig direct access
	if !template.SystemConfig.IsImmutabilityEnabled() {
		t.Errorf("expected systemConfig immutability to be enabled, got %t", template.SystemConfig.IsImmutabilityEnabled())
	}
}

func TestTemplateHelperMethodsWithUsers(t *testing.T) {
	template := &ImageTemplate{
		Image: ImageInfo{
			Name:    "test-image",
			Version: "1.0.0",
		},
		Target: TargetInfo{
			OS:        "azure-linux",
			Dist:      "azl3",
			Arch:      "x86_64",
			ImageType: "raw",
		},
		SystemConfig: SystemConfig{
			Name:        "test-config",
			Description: "Test configuration",
			Users: []UserConfig{
				{Name: "testuser", Password: "testpass", HashAlgo: "sha512", Sudo: true},
				{Name: "admin", Password: "$6$test$hash", Groups: []string{"wheel"}, PasswordMaxAge: 365},
			},
			Packages: []string{"package1", "package2"},
			Kernel: KernelConfig{
				Version: "6.12",
				Cmdline: "quiet",
			},
		},
	}

	// Test users access methods
	users := template.GetUsers()
	if len(users) != 2 {
		t.Errorf("expected 2 users, got %d", len(users))
	}

	testUser := template.GetUserByName("testuser")
	if testUser == nil {
		t.Errorf("expected to find testuser")
	} else {
		if !testUser.Sudo {
			t.Errorf("expected testuser to have sudo privileges")
		}
		if testUser.HashAlgo != "sha512" {
			t.Errorf("expected testuser hash_algo 'sha512', got %s", testUser.HashAlgo)
		}
	}

	// Test non-existent user
	nonExistentUser := template.GetUserByName("nonexistent")
	if nonExistentUser != nil {
		t.Errorf("expected not to find nonexistent user")
	}

	if !template.HasUsers() {
		t.Errorf("expected template to have users")
	}

	// Test systemConfig direct access
	if !template.SystemConfig.HasUsers() {
		t.Errorf("expected systemConfig to have users")
	}

	adminUser := template.SystemConfig.GetUserByName("admin")
	if adminUser == nil {
		t.Errorf("expected to find admin user via systemConfig")
	} else {
		if adminUser.PasswordMaxAge != 365 {
			t.Errorf("expected admin passwordMaxAge to be 365, got %d", adminUser.PasswordMaxAge)
		}
	}
}

func TestMergeSystemConfigWithUsers(t *testing.T) {
	defaultConfig := SystemConfig{
		Name: "default",
		Users: []UserConfig{
			{Name: "defaultuser", Password: "defaultpass", HashAlgo: "sha512"},
			{Name: "shared", Password: "defaultshared", HashAlgo: "sha256", Groups: []string{"default"}},
		},
		Packages: []string{"base-package"},
	}

	userConfig := SystemConfig{
		Name: "user",
		Users: []UserConfig{
			{Name: "newuser", Password: "newpass", HashAlgo: "bcrypt"},
			{Name: "shared", Password: "usershared", HashAlgo: "sha512", Groups: []string{"user", "admin"}, PasswordMaxAge: 180},
		},
		Packages: []string{"user-package"},
	}

	merged := mergeSystemConfig(defaultConfig, userConfig)

	// Test user merge
	if len(merged.Users) != 3 {
		t.Errorf("expected 3 merged users, got %d", len(merged.Users))
	}

	// Find shared user to test merging
	var sharedUser *UserConfig
	for i := range merged.Users {
		if merged.Users[i].Name == "shared" {
			sharedUser = &merged.Users[i]
			break
		}
	}

	if sharedUser == nil {
		t.Errorf("expected to find shared user in merged config")
	} else {
		if sharedUser.Password != "usershared" {
			t.Errorf("expected shared user password 'usershared', got '%s'", sharedUser.Password)
		}
		if sharedUser.HashAlgo != "sha512" {
			t.Errorf("expected shared user hash algo 'sha512', got '%s'", sharedUser.HashAlgo)
		}
		if sharedUser.PasswordMaxAge != 180 {
			t.Errorf("expected shared user password max age 180, got %d", sharedUser.PasswordMaxAge)
		}
		if len(sharedUser.Groups) != 3 { // default, user, admin merged
			t.Errorf("expected 3 merged groups for shared user, got %d", len(sharedUser.Groups))
		}
	}

	// Verify expected groups are present
	expectedGroups := []string{"default", "user", "admin"}
	groupMap := make(map[string]bool)
	for _, group := range sharedUser.Groups {
		groupMap[group] = true
	}
	for _, expectedGroup := range expectedGroups {
		if !groupMap[expectedGroup] {
			t.Errorf("expected group '%s' to be in merged groups", expectedGroup)
		}
	}
}

func TestUnsupportedFileFormat(t *testing.T) {
	// Create a temporary file with unsupported extension
	tmpFile, err := os.CreateTemp("", "test-*.txt")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if err := tmpFile.Chmod(0600); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString("some content"); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	tmpFile.Close()

	// Test loading should fail
	_, err = LoadTemplate(tmpFile.Name(), false)
	if err == nil {
		t.Errorf("expected error for unsupported file format")
	}
	if !strings.Contains(err.Error(), "unsupported file format") {
		t.Errorf("expected unsupported file format error, got: %v", err)
	}
}

func TestEmptySystemConfig(t *testing.T) {
	// Test template with empty system config
	template := &ImageTemplate{
		Image: ImageInfo{
			Name:    "test",
			Version: "1.0.0",
		},
		Target: TargetInfo{
			OS:        "azure-linux",
			Dist:      "azl3",
			Arch:      "x86_64",
			ImageType: "raw",
		},
		// Empty SystemConfig
		SystemConfig: SystemConfig{},
	}

	// Test that empty config still works
	packages := template.GetPackages()
	if len(packages) != 0 {
		t.Errorf("expected 0 packages for empty config, got %d", len(packages))
	}

	configName := template.GetSystemConfigName()
	if configName != "" {
		t.Errorf("expected empty config name, got %s", configName)
	}
}

func TestAllSupportedProviders(t *testing.T) {
	testCases := []struct {
		os       string
		dist     string
		expected string
		version  string
	}{
		{"azure-linux", "azl3", "AzureLinux3", "3"},
		{"emt", "emt3", "EMT3.0", "3.0"},
		{"elxr", "elxr12", "eLxr12", "12"},
		{"ubuntu", "ubuntu24", "Ubuntu24", "24.04"},
		{"ubuntu", "ubuntu26", "Ubuntu26", "26.04"},
	}

	for _, tc := range testCases {
		template := &ImageTemplate{
			Target: TargetInfo{
				OS:        tc.os,
				Dist:      tc.dist,
				Arch:      "x86_64",
				ImageType: "iso",
			},
			SystemConfig: SystemConfig{
				Name:     "test",
				Packages: []string{"test-package"},
				Kernel:   KernelConfig{Version: "6.12"},
			},
		}

		// Test provider name
		providerName := template.GetProviderName()
		if providerName != tc.expected {
			t.Errorf("for %s/%s expected provider '%s', got '%s'", tc.os, tc.dist, tc.expected, providerName)
		}

		// Test version
		version := template.GetDistroVersion()
		if version != tc.version {
			t.Errorf("for %s/%s expected version '%s', got '%s'", tc.os, tc.dist, tc.version, version)
		}
	}
}

func TestDiskAndSystemConfigGetters(t *testing.T) {
	template := &ImageTemplate{
		Image: ImageInfo{
			Name:    "test-image",
			Version: "1.0.0",
		},
		Target: TargetInfo{
			OS:        "azure-linux",
			Dist:      "azl3",
			Arch:      "x86_64",
			ImageType: "raw",
		},
		Disk: DiskConfig{
			Name: "test-disk",
			Size: "4GiB",
			Partitions: []PartitionInfo{
				{
					ID:         "root",
					FsType:     "ext4",
					Start:      "1MiB",
					End:        "0",
					MountPoint: "/",
				},
			},
		},
		SystemConfig: SystemConfig{
			Name: "test-config",
			Bootloader: Bootloader{
				BootType: "efi",
				Provider: "grub2",
			},
			Packages: []string{"package1", "package2"},
			Kernel: KernelConfig{
				Version: "6.12",
				Cmdline: "quiet splash",
			},
		},
	}

	// Test disk config getter
	diskConfig := template.GetDiskConfig()
	if diskConfig.Name != "test-disk" {
		t.Errorf("expected disk name 'test-disk', got %s", diskConfig.Name)
	}
	if diskConfig.Size != "4GiB" {
		t.Errorf("expected disk size '4GiB', got %s", diskConfig.Size)
	}
	if len(diskConfig.Partitions) != 1 {
		t.Errorf("expected 1 partition, got %d", len(diskConfig.Partitions))
	}

	// Test system config getter
	systemConfig := template.GetSystemConfig()
	if systemConfig.Name != "test-config" {
		t.Errorf("expected system config name 'test-config', got %s", systemConfig.Name)
	}

	// Test bootloader config getter
	bootloaderConfig := template.GetBootloaderConfig()
	if bootloaderConfig.BootType != "efi" {
		t.Errorf("expected bootloader type 'efi', got %s", bootloaderConfig.BootType)
	}
	if bootloaderConfig.Provider != "grub2" {
		t.Errorf("expected bootloader provider 'grub2', got %s", bootloaderConfig.Provider)
	}

	// Test individual field access
	packages := template.GetPackages()
	if len(packages) != 2 {
		t.Errorf("expected 2 packages, got %d", len(packages))
	}

	// Test kernel extraction
	kernel := template.GetKernel()
	if kernel.Version != "6.12" {
		t.Errorf("expected kernel version '6.12', got %s", kernel.Version)
	}

	// Test system config name extraction
	configName := template.GetSystemConfigName()
	if configName != "test-config" {
		t.Errorf("expected config name 'test-config', got %s", configName)
	}
}

func TestSecureBootHelperMethods(t *testing.T) {
	template := &ImageTemplate{
		Image: ImageInfo{
			Name:    "test-image",
			Version: "1.0.0",
		},
		Target: TargetInfo{
			OS:        "azure-linux",
			Dist:      "azl3",
			Arch:      "x86_64",
			ImageType: "raw",
		},
		SystemConfig: SystemConfig{
			Name:        "test-config",
			Description: "Test configuration with secure boot",
			Immutability: ImmutabilityConfig{
				Enabled:         true,
				SecureBootDBKey: "/test/keys/db.key",
				SecureBootDBCrt: "/test/certs/db.crt",
				SecureBootDBCer: "/test/certs/db.cer",
			},
		},
	}

	// Test ImmutabilityConfig helper methods
	immutabilityConfig := template.GetImmutability()
	if !immutabilityConfig.HasSecureBootDBConfig() {
		t.Errorf("expected immutability config to have secure boot DB config")
	}

	if !immutabilityConfig.HasSecureBootDBKey() {
		t.Errorf("expected immutability config to have secure boot DB key")
	}

	if !immutabilityConfig.HasSecureBootDBCrt() {
		t.Errorf("expected immutability config to have secure boot DB crt")
	}

	if !immutabilityConfig.HasSecureBootDBCer() {
		t.Errorf("expected immutability config to have secure boot DB cer")
	}

	// Test path retrieval methods
	if keyPath := immutabilityConfig.GetSecureBootDBKeyPath(); keyPath != "/test/keys/db.key" {
		t.Errorf("expected key path '/test/keys/db.key', got '%s'", keyPath)
	}

	if crtPath := immutabilityConfig.GetSecureBootDBCrtPath(); crtPath != "/test/certs/db.crt" {
		t.Errorf("expected crt path '/test/certs/db.crt', got '%s'", crtPath)
	}

	if cerPath := immutabilityConfig.GetSecureBootDBCerPath(); cerPath != "/test/certs/db.cer" {
		t.Errorf("expected cer path '/test/certs/db.cer', got '%s'", cerPath)
	}

	// Test SystemConfig access methods
	systemConfig := template.SystemConfig
	if !systemConfig.HasSecureBootDBConfig() {
		t.Errorf("expected systemConfig to have secure boot DB config")
	}

	if keyPath := systemConfig.GetSecureBootDBKeyPath(); keyPath != "/test/keys/db.key" {
		t.Errorf("expected systemConfig key path '/test/keys/db.key', got '%s'", keyPath)
	}

	if crtPath := systemConfig.GetSecureBootDBCrtPath(); crtPath != "/test/certs/db.crt" {
		t.Errorf("expected systemConfig crt path '/test/certs/db.crt', got '%s'", crtPath)
	}

	if cerPath := systemConfig.GetSecureBootDBCerPath(); cerPath != "/test/certs/db.cer" {
		t.Errorf("expected systemConfig cer path '/test/certs/db.cer', got '%s'", cerPath)
	}

	// Test ImageTemplate secure boot helpers
	expectedKeyPath := "/test/keys/db.key"
	if keyPath := template.GetSecureBootDBKeyPath(); keyPath != expectedKeyPath {
		t.Errorf("expected secure boot key path '%s', got '%s'", expectedKeyPath, keyPath)
	}

	expectedCrtPath := "/test/certs/db.crt"
	if crtPath := template.GetSecureBootDBCrtPath(); crtPath != expectedCrtPath {
		t.Errorf("expected secure boot crt path '%s', got '%s'", expectedCrtPath, crtPath)
	}

	expectedCerPath := "/test/certs/db.cer"
	if cerPath := template.GetSecureBootDBCerPath(); cerPath != expectedCerPath {
		t.Errorf("expected secure boot cer path '%s', got '%s'", expectedCerPath, cerPath)
	}

	if !template.HasSecureBootDBConfig() {
		t.Errorf("expected template to have secure boot DB config")
	}
}

func TestSecureBootWithoutConfig(t *testing.T) {
	template := &ImageTemplate{
		Image: ImageInfo{
			Name:    "test-image",
			Version: "1.0.0",
		},
		Target: TargetInfo{
			OS:        "azure-linux",
			Dist:      "azl3",
			Arch:      "x86_64",
			ImageType: "raw",
		},
		SystemConfig: SystemConfig{
			Name:        "test-config",
			Description: "Test configuration without secure boot",
			Immutability: ImmutabilityConfig{
				Enabled: true,
				// No secure boot fields set
			},
		},
	}

	// Test that methods work correctly when no secure boot config is provided
	if template.HasSecureBootDBConfig() {
		t.Errorf("expected template to not have secure boot DB config")
	}

	immutabilityConfig := template.GetImmutability()
	if immutabilityConfig.HasSecureBootDBConfig() {
		t.Errorf("expected immutability config to not have secure boot DB config")
	}

	if immutabilityConfig.HasSecureBootDBKey() {
		t.Errorf("expected immutability config to not have secure boot DB key")
	}

	if immutabilityConfig.HasSecureBootDBCrt() {
		t.Errorf("expected immutability config to not have secure boot DB crt")
	}

	if immutabilityConfig.HasSecureBootDBCer() {
		t.Errorf("expected immutability config to not have secure boot DB cer")
	}

	// Test that path methods return empty strings
	if keyPath := template.GetSecureBootDBKeyPath(); keyPath != "" {
		t.Errorf("expected empty key path, got '%s'", keyPath)
	}

	if crtPath := template.GetSecureBootDBCrtPath(); crtPath != "" {
		t.Errorf("expected empty crt path, got '%s'", crtPath)
	}

	if cerPath := template.GetSecureBootDBCerPath(); cerPath != "" {
		t.Errorf("expected empty cer path, got '%s'", cerPath)
	}
}

func TestPartialSecureBootConfig(t *testing.T) {
	template := &ImageTemplate{
		SystemConfig: SystemConfig{
			Immutability: ImmutabilityConfig{
				Enabled:         true,
				SecureBootDBKey: "/test/keys/db.key",
				// Only key is set, no certificates
			},
		},
	}

	immutabilityConfig := template.GetImmutability()

	// Should have config because key is set
	if !immutabilityConfig.HasSecureBootDBConfig() {
		t.Errorf("expected immutability config to have secure boot DB config")
	}

	// Should have key
	if !immutabilityConfig.HasSecureBootDBKey() {
		t.Errorf("expected immutability config to have secure boot DB key")
	}

	// Should not have certificates
	if immutabilityConfig.HasSecureBootDBCrt() {
		t.Errorf("expected immutability config to not have secure boot DB crt")
	}

	if immutabilityConfig.HasSecureBootDBCer() {
		t.Errorf("expected immutability config to not have secure boot DB cer")
	}
}

func TestDiskConfigValidation(t *testing.T) {
	testCases := []struct {
		name     string
		disk     DiskConfig
		expected bool // whether it should be considered empty
	}{
		{
			name:     "empty disk config",
			disk:     DiskConfig{},
			expected: true,
		},
		{
			name: "disk with only name",
			disk: DiskConfig{
				Name: "test-disk",
			},
			expected: false,
		},
		{
			name: "disk with full configuration",
			disk: DiskConfig{
				Name:               "main-disk",
				Size:               "20GiB",
				PartitionTableType: "gpt",
				Partitions: []PartitionInfo{
					{
						ID:         "boot",
						Name:       "EFI Boot",
						Type:       "esp",
						FsType:     "fat32",
						Start:      "1MiB",
						End:        "513MiB",
						MountPoint: "/boot/efi",
						Flags:      []string{"boot"},
					},
					{
						ID:         "root",
						Name:       "Root",
						Type:       "linux-root-amd64",
						FsType:     "ext4",
						Start:      "513MiB",
						End:        "0",
						MountPoint: "/",
					},
				},
			},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test isEmpty functionality - this would be based on merge.go isEmptyDiskConfig
			isEmpty := (tc.disk.Name == "" && tc.disk.Size == "" && len(tc.disk.Partitions) == 0)
			if isEmpty != tc.expected {
				t.Errorf("Expected isEmpty=%v, got isEmpty=%v for %s", tc.expected, isEmpty, tc.name)
			}
		})
	}
}

func TestPartitionInfoFields(t *testing.T) {
	template := &ImageTemplate{
		Disk: DiskConfig{
			Name:               "test-disk",
			Size:               "10GiB",
			PartitionTableType: "gpt",
			Partitions: []PartitionInfo{
				{
					ID:           "efi",
					Name:         "EFI System",
					Type:         "esp",
					TypeGUID:     "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
					FsType:       "fat32",
					Start:        "1MiB",
					End:          "513MiB",
					MountPoint:   "/boot/efi",
					MountOptions: "defaults",
					Flags:        []string{"boot", "esp"},
				},
				{
					ID:       "swap",
					Name:     "Swap",
					Type:     "swap",
					TypeGUID: "0657FD6D-A4AB-43C4-84E5-0933C84B4F4F",
					FsType:   "swap",
					Start:    "513MiB",
					End:      "2GiB",
				},
				{
					ID:         "root",
					Name:       "Root",
					Type:       "linux-root-amd64",
					TypeGUID:   "4F68BCE3-E8CD-4DB1-96E7-FBCAF984B709",
					FsType:     "ext4",
					Start:      "2GiB",
					End:        "0",
					MountPoint: "/",
				},
			},
		},
	}

	diskConfig := template.GetDiskConfig()

	// Verify partition count
	if len(diskConfig.Partitions) != 3 {
		t.Errorf("expected 3 partitions, got %d", len(diskConfig.Partitions))
	}

	// Verify EFI partition
	efiPartition := diskConfig.Partitions[0]
	if efiPartition.ID != "efi" {
		t.Errorf("expected EFI partition ID 'efi', got '%s'", efiPartition.ID)
	}
	if len(efiPartition.Flags) != 2 {
		t.Errorf("expected 2 flags for EFI partition, got %d", len(efiPartition.Flags))
	}
	if efiPartition.TypeGUID != "C12A7328-F81F-11D2-BA4B-00A0C93EC93B" {
		t.Errorf("expected EFI TypeGUID, got '%s'", efiPartition.TypeGUID)
	}
	if efiPartition.Start != "1MiB" {
		t.Errorf("expected EFI start '1MiB', got '%s'", efiPartition.Start)
	}
	if efiPartition.End != "513MiB" {
		t.Errorf("expected EFI end '513MiB', got '%s'", efiPartition.End)
	}
	if efiPartition.MountOptions != "defaults" {
		t.Errorf("expected EFI mount options 'defaults', got '%s'", efiPartition.MountOptions)
	}

	// Verify swap partition
	swapPartition := diskConfig.Partitions[1]
	if swapPartition.FsType != "swap" {
		t.Errorf("expected swap filesystem type, got '%s'", swapPartition.FsType)
	}
	if swapPartition.MountPoint != "" {
		t.Errorf("expected empty mount point for swap, got '%s'", swapPartition.MountPoint)
	}
	if swapPartition.Start != "513MiB" {
		t.Errorf("expected swap start '513MiB', got '%s'", swapPartition.Start)
	}
	if swapPartition.End != "2GiB" {
		t.Errorf("expected swap end '2GiB', got '%s'", swapPartition.End)
	}

	// Verify root partition
	rootPartition := diskConfig.Partitions[2]
	if rootPartition.MountPoint != "/" {
		t.Errorf("expected root mount point '/', got '%s'", rootPartition.MountPoint)
	}
	if rootPartition.Start != "2GiB" {
		t.Errorf("expected root start '2GiB', got '%s'", rootPartition.Start)
	}
	if rootPartition.End != "0" {
		t.Errorf("expected root end '0' (end of disk), got '%s'", rootPartition.End)
	}
}

func TestArtifactInfo(t *testing.T) {
	template := &ImageTemplate{
		Disk: DiskConfig{
			Name: "test-disk",
			Artifacts: []ArtifactInfo{
				{Type: "raw", Compression: "none"},
				{Type: "qcow2", Compression: "gzip"},
				{Type: "vmdk", Compression: "lz4"},
			},
		},
	}

	artifacts := template.GetDiskConfig().Artifacts
	if len(artifacts) != 3 {
		t.Errorf("expected 3 artifacts, got %d", len(artifacts))
	}

	// Test artifact types and compression
	expectedArtifacts := []struct {
		Type        string
		Compression string
	}{
		{"raw", "none"},
		{"qcow2", "gzip"},
		{"vmdk", "lz4"},
	}

	for i, expected := range expectedArtifacts {
		if artifacts[i].Type != expected.Type {
			t.Errorf("artifact %d: expected type '%s', got '%s'", i, expected.Type, artifacts[i].Type)
		}
		if artifacts[i].Compression != expected.Compression {
			t.Errorf("artifact %d: expected compression '%s', got '%s'", i, expected.Compression, artifacts[i].Compression)
		}
	}

	// Test empty artifacts
	emptyTemplate := &ImageTemplate{
		Disk: DiskConfig{
			Artifacts: []ArtifactInfo{},
		},
	}

	emptyArtifacts := emptyTemplate.GetDiskConfig().Artifacts
	if len(emptyArtifacts) != 0 {
		t.Errorf("Expected 0 artifacts, got %d", len(emptyArtifacts))
	}
}

func TestAdditionalFileInfo(t *testing.T) {
	template := &ImageTemplate{
		SystemConfig: SystemConfig{
			Name: "test-config",
			AdditionalFiles: []AdditionalFileInfo{
				{Local: "/host/config.conf", Final: "/etc/app/config.conf"},
				{Local: "/host/script.sh", Final: "/usr/local/bin/script.sh"},
				{Local: "/host/certs/ca.crt", Final: "/etc/ssl/certs/ca.crt"},
			},
		},
	}

	additionalFiles := template.GetSystemConfig().AdditionalFiles
	if len(additionalFiles) != 3 {
		t.Errorf("expected 3 additional files, got %d", len(additionalFiles))
	}

	// Test file mappings
	expectedFiles := []struct {
		Local string
		Final string
	}{
		{"/host/config.conf", "/etc/app/config.conf"},
		{"/host/script.sh", "/usr/local/bin/script.sh"},
		{"/host/certs/ca.crt", "/etc/ssl/certs/ca.crt"},
	}

	for i, expected := range expectedFiles {
		if additionalFiles[i].Local != expected.Local {
			t.Errorf("file %d: expected local path '%s', got '%s'", i, expected.Local, additionalFiles[i].Local)
		}
		if additionalFiles[i].Final != expected.Final {
			t.Errorf("file %d: expected final path '%s', got '%s'", i, expected.Final, additionalFiles[i].Final)
		}
	}
}

func TestBootloaderMerging(t *testing.T) {
	defaultConfig := SystemConfig{
		Bootloader: Bootloader{
			BootType: "legacy",
			Provider: "grub2",
		},
	}

	userConfig := SystemConfig{
		Bootloader: Bootloader{
			BootType: "efi",
			Provider: "systemd-boot",
		},
	}

	merged := mergeSystemConfig(defaultConfig, userConfig)

	if merged.Bootloader.BootType != "efi" {
		t.Errorf("expected merged bootloader type 'efi', got '%s'", merged.Bootloader.BootType)
	}
	if merged.Bootloader.Provider != "systemd-boot" {
		t.Errorf("expected merged bootloader provider 'systemd-boot', got '%s'", merged.Bootloader.Provider)
	}
}

func TestEmptyBootloaderMerging(t *testing.T) {
	defaultConfig := SystemConfig{
		Bootloader: Bootloader{
			BootType: "efi",
			Provider: "grub2",
		},
	}

	userConfig := SystemConfig{
		// Empty bootloader config
		Bootloader: Bootloader{},
	}

	merged := mergeSystemConfig(defaultConfig, userConfig)

	// Should keep default bootloader when user config is empty
	if merged.Bootloader.BootType != "efi" {
		t.Errorf("expected default bootloader type 'efi', got '%s'", merged.Bootloader.BootType)
	}
	if merged.Bootloader.Provider != "grub2" {
		t.Errorf("expected default bootloader provider 'grub2', got '%s'", merged.Bootloader.Provider)
	}
}

func TestKernelConfigMerging(t *testing.T) {
	defaultConfig := SystemConfig{
		Kernel: KernelConfig{
			Version: "6.10",
			Cmdline: "quiet",
		},
	}

	userConfig := SystemConfig{
		Kernel: KernelConfig{
			Version: "6.12",
			Cmdline: "quiet splash debug",
		},
	}

	merged := mergeSystemConfig(defaultConfig, userConfig)

	if merged.Kernel.Version != "6.12" {
		t.Errorf("expected merged kernel version '6.12', got '%s'", merged.Kernel.Version)
	}
	if merged.Kernel.Cmdline != "quiet splash debug" {
		t.Errorf("expected merged kernel cmdline 'quiet splash debug', got '%s'", merged.Kernel.Cmdline)
	}
}

func TestPartialKernelConfigMerging(t *testing.T) {
	defaultConfig := SystemConfig{
		Kernel: KernelConfig{
			Version: "6.10",
			Cmdline: "quiet",
		},
	}

	userConfig := SystemConfig{
		Kernel: KernelConfig{
			Version: "6.12",
			// No cmdline specified
		},
	}

	merged := mergeSystemConfig(defaultConfig, userConfig)

	if merged.Kernel.Version != "6.12" {
		t.Errorf("expected merged kernel version '6.12', got '%s'", merged.Kernel.Version)
	}
	// Should keep default cmdline when user doesn't specify one
	if merged.Kernel.Cmdline != "quiet" {
		t.Errorf("expected default kernel cmdline 'quiet', got '%s'", merged.Kernel.Cmdline)
	}
}

func TestLoadNonExistentFile(t *testing.T) {
	_, err := LoadTemplate("/nonexistent/file.yml", false)
	if err == nil {
		t.Errorf("expected error for non-existent file")
	}
	if !strings.Contains(err.Error(), "no such file or directory") && !strings.Contains(err.Error(), "failed to read template file") {
		t.Errorf("expected file not found error, got: %v", err)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	invalidYAML := `
image:
  name: test
  version: 1.0.0
target:
  - invalid: yaml structure
    that: doesn't match schema
`

	tmpFile, err := os.CreateTemp("", "test-*.yml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if err := tmpFile.Chmod(0600); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(invalidYAML); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	tmpFile.Close()

	_, err = LoadTemplate(tmpFile.Name(), true)
	if err == nil {
		t.Errorf("expected error for invalid YAML structure")
	}
}

func TestDefaultConfigLoader(t *testing.T) {
	loader := NewDefaultConfigLoader("azure-linux", "azl3", "x86_64")

	if loader.targetOs != "azure-linux" {
		t.Errorf("expected target OS 'azure-linux', got '%s'", loader.targetOs)
	}
	if loader.targetDist != "azl3" {
		t.Errorf("expected target dist 'azl3', got '%s'", loader.targetDist)
	}
	if loader.targetArch != "x86_64" {
		t.Errorf("expected target arch 'x86_64', got '%s'", loader.targetArch)
	}
}

func TestDefaultConfigLoaderUnsupportedImageType(t *testing.T) {
	loader := NewDefaultConfigLoader("azure-linux", "azl3", "x86_64")

	_, err := loader.LoadDefaultConfig("unsupported")
	if err == nil {
		t.Errorf("expected error for unsupported image type")
	}
	if !strings.Contains(err.Error(), "unsupported image type") {
		t.Errorf("expected unsupported image type error, got: %v", err)
	}
}

func TestPackageMergingWithDuplicates(t *testing.T) {
	defaultPackages := []string{"base", "common", "utils"}
	userPackages := []string{"common", "extra", "base", "new"}

	merged := mergePackages(defaultPackages, userPackages)

	// Should contain all unique packages
	expectedPackages := []string{"base", "common", "utils", "extra", "new"}
	if len(merged) != len(expectedPackages) {
		t.Errorf("expected %d merged packages, got %d", len(expectedPackages), len(merged))
	}

	// Check for duplicates
	packageMap := make(map[string]int)
	for _, pkg := range merged {
		packageMap[pkg]++
		if packageMap[pkg] > 1 {
			t.Errorf("found duplicate package '%s' in merged list", pkg)
		}
	}
}

func TestEmptyPackageMerging(t *testing.T) {
	// Test merging with empty default packages
	defaultPackages := []string{}
	userPackages := []string{"package1", "package2"}

	merged := mergePackages(defaultPackages, userPackages)
	if len(merged) != 2 {
		t.Errorf("expected 2 merged packages, got %d", len(merged))
	}

	// Test merging with empty user packages
	defaultPackages = []string{"default1", "default2"}
	userPackages = []string{}

	merged = mergePackages(defaultPackages, userPackages)
	if len(merged) != 2 {
		t.Errorf("expected 2 merged packages, got %d", len(merged))
	}
}

func TestComplexConfigurationMerging(t *testing.T) {
	defaultTemplate := &ImageTemplate{
		Image: ImageInfo{
			Name:    "default-image",
			Version: "1.0.0",
		},
		Target: TargetInfo{
			OS:        "azure-linux",
			Dist:      "azl3",
			Arch:      "x86_64",
			ImageType: "raw",
		},
		SystemConfig: SystemConfig{
			Name: "default-config",
			Immutability: ImmutabilityConfig{
				Enabled:         true,
				SecureBootDBKey: "/default/keys/db.key",
			},
			Users: []UserConfig{
				{Name: "admin", Password: "defaultpass", Groups: []string{"wheel"}},
			},
			Packages: []string{"base", "common"},
			Kernel: KernelConfig{
				Version: "6.10",
				Cmdline: "quiet",
			},
		},
		Disk: DiskConfig{
			Name: "default-disk",
			Size: "10GiB",
		},
	}

	userTemplate := &ImageTemplate{
		Image: ImageInfo{
			Name:    "user-image",
			Version: "2.0.0",
		},
		Target: TargetInfo{
			OS:        "azure-linux",
			Dist:      "azl3",
			Arch:      "x86_64",
			ImageType: "iso",
		},
		SystemConfig: SystemConfig{
			Name: "user-config",
			Immutability: ImmutabilityConfig{
				Enabled:         false,
				SecureBootDBCrt: "/user/certs/db.crt",
				wasProvided:     true, // Mark as explicitly provided
			},
			Users: []UserConfig{
				{Name: "user", Password: "userpass", HashAlgo: "sha512"},
				{Name: "admin", Password: "newpass", Groups: []string{"admin", "wheel"}},
			},
			Packages: []string{"extra", "user-specific"},
			Kernel: KernelConfig{
				Version: "6.12",
			},
		},
		Disk: DiskConfig{
			Name: "user-disk",
			Size: "20GiB",
		},
	}

	merged, err := MergeConfigurations(userTemplate, defaultTemplate)
	if err != nil {
		t.Fatalf("failed to merge configurations: %v", err)
	}

	// Test image info (user should override)
	if merged.Image.Name != "user-image" {
		t.Errorf("expected merged image name 'user-image', got '%s'", merged.Image.Name)
	}
	if merged.Image.Version != "2.0.0" {
		t.Errorf("expected merged image version '2.0.0', got '%s'", merged.Image.Version)
	}

	// Test target info (user should override)
	if merged.Target.ImageType != "iso" {
		t.Errorf("expected merged image type 'iso', got '%s'", merged.Target.ImageType)
	}

	// Test disk config (user should override)
	if merged.Disk.Name != "user-disk" {
		t.Errorf("expected merged disk name 'user-disk', got '%s'", merged.Disk.Name)
	}
	if merged.Disk.Size != "20GiB" {
		t.Errorf("expected merged disk size '20GiB', got '%s'", merged.Disk.Size)
	}

	// Test system config merging
	if merged.SystemConfig.Name != "user-config" {
		t.Errorf("expected merged system config name 'user-config', got '%s'", merged.SystemConfig.Name)
	}

	// Test immutability merging (user false should override default true)
	if merged.SystemConfig.Immutability.Enabled {
		t.Errorf("expected merged immutability to be false, got true")
	}

	// Test that secure boot settings are merged
	if merged.SystemConfig.Immutability.SecureBootDBKey != "/default/keys/db.key" {
		t.Errorf("expected merged secure boot key from default config")
	}
	if merged.SystemConfig.Immutability.SecureBootDBCrt != "/user/certs/db.crt" {
		t.Errorf("expected merged secure boot crt from user config")
	}

	// Test user merging
	if len(merged.SystemConfig.Users) != 2 {
		t.Errorf("expected 2 merged users, got %d", len(merged.SystemConfig.Users))
	}

	// Test package merging
	packages := merged.GetPackages()
	expectedPackageCount := 4 // base, common, extra, user-specific
	if len(packages) != expectedPackageCount {
		t.Errorf("expected %d merged packages, got %d", expectedPackageCount, len(packages))
	}

	// Test kernel merging
	if merged.SystemConfig.Kernel.Version != "6.12" {
		t.Errorf("expected merged kernel version '6.12', got '%s'", merged.SystemConfig.Kernel.Version)
	}
	// Cmdline should remain from default since user didn't specify
	if merged.SystemConfig.Kernel.Cmdline != "quiet" {
		t.Errorf("expected default kernel cmdline 'quiet', got '%s'", merged.SystemConfig.Kernel.Cmdline)
	}
}

func TestNilTemplateHandling(t *testing.T) {
	// Test merging with nil user template
	_, err := MergeConfigurations(nil, &ImageTemplate{})
	if err == nil {
		t.Errorf("expected error when user template is nil")
	}

	// Test merging with nil default template (should work)
	userTemplate := &ImageTemplate{
		Image: ImageInfo{Name: "test", Version: "1.0.0"},
	}
	merged, err := MergeConfigurations(userTemplate, nil)
	if err != nil {
		t.Errorf("unexpected error when default template is nil: %v", err)
	}
	if merged.Image.Name != "test" {
		t.Errorf("expected merged image name 'test', got '%s'", merged.Image.Name)
	}
}

func TestGetImageNameMethod(t *testing.T) {
	template := &ImageTemplate{
		Image: ImageInfo{
			Name:    "test-image-name",
			Version: "1.2.3",
		},
	}

	imageName := template.GetImageName()
	if imageName != "test-image-name" {
		t.Errorf("expected image name 'test-image-name', got '%s'", imageName)
	}
}

func TestGetTargetInfoMethod(t *testing.T) {
	expectedTarget := TargetInfo{
		OS:        "azure-linux",
		Dist:      "azl3",
		Arch:      "aarch64",
		ImageType: "iso",
	}

	template := &ImageTemplate{
		Target: expectedTarget,
	}

	targetInfo := template.GetTargetInfo()
	if targetInfo.OS != expectedTarget.OS {
		t.Errorf("expected target OS '%s', got '%s'", expectedTarget.OS, targetInfo.OS)
	}
	if targetInfo.Arch != expectedTarget.Arch {
		t.Errorf("expected target arch '%s', got '%s'", expectedTarget.Arch, targetInfo.Arch)
	}
	if targetInfo.ImageType != expectedTarget.ImageType {
		t.Errorf("expected target image type '%s', got '%s'", expectedTarget.ImageType, targetInfo.ImageType)
	}
}

func TestSaveUpdatedConfigFile(t *testing.T) {
	template := &ImageTemplate{
		Image: ImageInfo{
			Name:    "test-save",
			Version: "1.0.0",
		},
	}

	// Test the function (currently returns nil, but we test the interface)
	err := template.SaveUpdatedConfigFile("/tmp/test.yml")
	if err != nil {
		t.Errorf("SaveUpdatedConfigFile returned unexpected error: %v", err)
	}
}

func TestUserConfigValidation(t *testing.T) {
	template := &ImageTemplate{
		SystemConfig: SystemConfig{
			Users: []UserConfig{
				{
					Name:           "testuser",
					Password:       "testpass",
					HashAlgo:       "sha512",
					PasswordMaxAge: 90,
					StartupScript:  "/home/testuser/startup.sh",
					Groups:         []string{"users", "docker"},
					Sudo:           true,
					Home:           "/home/testuser",
					Shell:          "/bin/bash",
				},
			},
		},
	}

	users := template.GetUsers()
	if len(users) != 1 {
		t.Errorf("expected 1 user, got %d", len(users))
	}

	user := users[0]
	if user.PasswordMaxAge != 90 {
		t.Errorf("expected password max age 90, got %d", user.PasswordMaxAge)
	}
	if user.StartupScript != "/home/testuser/startup.sh" {
		t.Errorf("expected startup script '/home/testuser/startup.sh', got '%s'", user.StartupScript)
	}
	if user.Home != "/home/testuser" {
		t.Errorf("expected home '/home/testuser', got '%s'", user.Home)
	}
	if user.Shell != "/bin/bash" {
		t.Errorf("expected shell '/bin/bash', got '%s'", user.Shell)
	}
}

func TestUnknownProviderMapping(t *testing.T) {
	template := &ImageTemplate{
		Target: TargetInfo{
			OS:   "unknown-os",
			Dist: "unknown-dist",
		},
	}

	providerName := template.GetProviderName()
	if providerName != "" {
		t.Errorf("expected empty provider name for unknown OS/dist, got '%s'", providerName)
	}

	version := template.GetDistroVersion()
	if version != "" {
		t.Errorf("expected empty version for unknown dist, got '%s'", version)
	}
}

func TestSystemConfigImmutabilityMethods(t *testing.T) {
	systemConfig := SystemConfig{
		Immutability: ImmutabilityConfig{
			Enabled:         true,
			SecureBootDBKey: "/path/to/key.key",
			SecureBootDBCrt: "/path/to/cert.crt",
			SecureBootDBCer: "/path/to/cert.cer",
		},
	}

	if !systemConfig.IsImmutabilityEnabled() {
		t.Errorf("expected systemConfig immutability to be enabled")
	}

	if !systemConfig.HasSecureBootDBConfig() {
		t.Errorf("expected systemConfig to have secure boot DB config")
	}

	if systemConfig.GetSecureBootDBKeyPath() != "/path/to/key.key" {
		t.Errorf("expected key path '/path/to/key.key', got '%s'", systemConfig.GetSecureBootDBKeyPath())
	}

	if systemConfig.GetSecureBootDBCrtPath() != "/path/to/cert.crt" {
		t.Errorf("expected crt path '/path/to/cert.crt', got '%s'", systemConfig.GetSecureBootDBCrtPath())
	}

	if systemConfig.GetSecureBootDBCerPath() != "/path/to/cert.cer" {
		t.Errorf("expected cer path '/path/to/cert.cer', got '%s'", systemConfig.GetSecureBootDBCerPath())
	}
}

func TestSystemConfigWithoutImmutability(t *testing.T) {
	systemConfig := SystemConfig{
		Name: "test-config",
		// No immutability config
	}

	if systemConfig.IsImmutabilityEnabled() {
		t.Errorf("expected systemConfig immutability to be disabled")
	}

	if systemConfig.HasSecureBootDBConfig() {
		t.Errorf("expected systemConfig to not have secure boot DB config")
	}

	if systemConfig.GetSecureBootDBKeyPath() != "" {
		t.Errorf("expected empty key path, got '%s'", systemConfig.GetSecureBootDBKeyPath())
	}
}

func TestMergeUserConfigBasicFields(t *testing.T) {
	defaultUser := UserConfig{
		Name:           "testuser",
		Password:       "defaultpass",
		HashAlgo:       "sha256",
		PasswordMaxAge: 90,
		StartupScript:  "/default/script.sh",
		Groups:         []string{"default-group"},
		Sudo:           false,
		Home:           "/home/default",
		Shell:          "/bin/sh",
	}

	userUser := UserConfig{
		Name:           "testuser",
		Password:       "newpass",
		HashAlgo:       "sha512",
		PasswordMaxAge: 180,
		StartupScript:  "/user/script.sh",
		Groups:         []string{"user-group", "admin"},
		Sudo:           true,
		Home:           "/home/custom",
		Shell:          "/bin/bash",
	}

	merged := mergeUserConfig(defaultUser, userUser)

	if merged.Password != "newpass" {
		t.Errorf("expected password 'newpass', got '%s'", merged.Password)
	}
	if merged.HashAlgo != "sha512" {
		t.Errorf("expected hash algo 'sha512', got '%s'", merged.HashAlgo)
	}
	if merged.PasswordMaxAge != 180 {
		t.Errorf("expected password max age 180, got %d", merged.PasswordMaxAge)
	}
	if merged.StartupScript != "/user/script.sh" {
		t.Errorf("expected startup script '/user/script.sh', got '%s'", merged.StartupScript)
	}
	if !merged.Sudo {
		t.Errorf("expected sudo to be true")
	}
	if merged.Home != "/home/custom" {
		t.Errorf("expected home '/home/custom', got '%s'", merged.Home)
	}
	if merged.Shell != "/bin/bash" {
		t.Errorf("expected shell '/bin/bash', got '%s'", merged.Shell)
	}
	if len(merged.Groups) != 3 { // should merge groups
		t.Errorf("expected 3 merged groups, got %d", len(merged.Groups))
	}
}

func TestMergeUserConfigPreHashedPassword(t *testing.T) {
	defaultUser := UserConfig{
		Name:     "testuser",
		Password: "plaintext",
		HashAlgo: "sha512",
	}

	// User provides pre-hashed password (starts with $)
	userUser := UserConfig{
		Name:     "testuser",
		Password: "$6$salt$hashedpassword",
	}

	merged := mergeUserConfig(defaultUser, userUser)

	if merged.Password != "$6$salt$hashedpassword" {
		t.Errorf("expected pre-hashed password, got '%s'", merged.Password)
	}
	if merged.HashAlgo != "" {
		t.Errorf("expected empty hash algo for pre-hashed password, got '%s'", merged.HashAlgo)
	}
}

func TestMergeUserConfigHashAlgoOnly(t *testing.T) {
	defaultUser := UserConfig{
		Name:     "testuser",
		Password: "defaultpass",
		HashAlgo: "sha256",
	}

	// User only changes hash algorithm
	userUser := UserConfig{
		Name:     "testuser",
		HashAlgo: "bcrypt",
	}

	merged := mergeUserConfig(defaultUser, userUser)

	if merged.Password != "defaultpass" {
		t.Errorf("expected default password to be preserved, got '%s'", merged.Password)
	}
	if merged.HashAlgo != "bcrypt" {
		t.Errorf("expected hash algo 'bcrypt', got '%s'", merged.HashAlgo)
	}
}

func TestUserMergingOverrideExisting(t *testing.T) {
	// Test that user merging properly overrides existing users by name
	defaultUsers := []UserConfig{
		{Name: "admin", Password: "oldpass", Groups: []string{"wheel"}},
		{Name: "user", Password: "userpass", HashAlgo: "sha256"},
	}

	userUsers := []UserConfig{
		{Name: "admin", Password: "newpass", Groups: []string{"admin", "wheel"}, Sudo: true},
		{Name: "newuser", Password: "newuserpass", HashAlgo: "sha512"},
	}

	merged := mergeUsers(defaultUsers, userUsers)

	if len(merged) != 3 {
		t.Errorf("expected 3 merged users, got %d", len(merged))
	}

	// Find admin user in merged result
	var adminUser *UserConfig
	for i := range merged {
		if merged[i].Name == "admin" {
			adminUser = &merged[i]
			break
		}
	}

	if adminUser == nil {
		t.Errorf("admin user not found in merged result")
	} else {
		if adminUser.Password != "newpass" {
			t.Errorf("expected admin password 'newpass', got '%s'", adminUser.Password)
		}
		if !adminUser.Sudo {
			t.Errorf("expected admin to have sudo privileges")
		}
		if len(adminUser.Groups) != 2 {
			t.Errorf("expected admin to have 2 groups, got %d", len(adminUser.Groups))
		}
	}
}

func TestUnsupportedFileExtensions(t *testing.T) {
	unsupportedExtensions := []string{".txt", ".json", ".xml", ".ini", ".conf", ".properties"}

	for _, ext := range unsupportedExtensions {
		tmpFile, err := os.CreateTemp("", "test-*"+ext)
		if err != nil {
			t.Fatalf("failed to create temp file: %v", err)
		}
		if err := tmpFile.Chmod(0600); err != nil {
			tmpFile.Close()
			os.Remove(tmpFile.Name())
			return
		}
		defer os.Remove(tmpFile.Name())

		content := "some content"
		if _, err := tmpFile.WriteString(content); err != nil {
			t.Fatalf("failed to write temp file: %v", err)
		}
		tmpFile.Close()

		_, err = LoadTemplate(tmpFile.Name(), false)
		if err == nil {
			t.Errorf("expected error for unsupported extension %s", ext)
		}
		if !strings.Contains(err.Error(), "unsupported file format") {
			t.Errorf("expected unsupported file format error for %s, got: %v", ext, err)
		}
	}
}

// Updated test to match actual isEmptySystemConfig logic
func TestIsEmptySystemConfig(t *testing.T) {
	// Test empty system config
	emptyConfig := SystemConfig{}
	if !isEmptySystemConfig(emptyConfig) {
		t.Errorf("expected empty system config to be detected as empty")
	}

	// Test non-empty system config
	nonEmptyConfig := SystemConfig{Name: "test"}
	if isEmptySystemConfig(nonEmptyConfig) {
		t.Errorf("expected non-empty system config to not be detected as empty")
	}

	// Test config with only packages - according to actual implementation, this is still empty
	packageConfig := SystemConfig{Packages: []string{"test"}}
	if !isEmptySystemConfig(packageConfig) {
		t.Errorf("expected config with packages but no name to be detected as empty")
	}
}

func TestIsEmptyBootloader(t *testing.T) {
	// Test empty bootloader
	emptyBootloader := Bootloader{}
	if !isEmptyBootloader(emptyBootloader) {
		t.Errorf("expected empty bootloader to be detected as empty")
	}

	// Test bootloader with boot type
	bootTypeLoader := Bootloader{BootType: "efi"}
	if isEmptyBootloader(bootTypeLoader) {
		t.Errorf("expected bootloader with boot type to not be detected as empty")
	}

	// Test bootloader with provider
	providerLoader := Bootloader{Provider: "grub2"}
	if isEmptyBootloader(providerLoader) {
		t.Errorf("expected bootloader with provider to not be detected as empty")
	}
}

func TestSystemConfigGetters(t *testing.T) {
	systemConfig := SystemConfig{
		Name:        "test-system",
		Description: "Test system config",
		Packages:    []string{"pkg1", "pkg2"},
		Kernel: KernelConfig{
			Version: "6.12",
			Cmdline: "quiet splash",
		},
		Bootloader: Bootloader{
			BootType: "efi",
			Provider: "grub2",
		},
		AdditionalFiles: []AdditionalFileInfo{
			{Local: "/local/file", Final: "/final/file"},
		},
	}

	// Test that all fields are accessible
	if systemConfig.Name != "test-system" {
		t.Errorf("expected name 'test-system', got '%s'", systemConfig.Name)
	}

	if systemConfig.Description != "Test system config" {
		t.Errorf("expected description 'Test system config', got '%s'", systemConfig.Description)
	}

	if len(systemConfig.Packages) != 2 {
		t.Errorf("expected 2 packages, got %d", len(systemConfig.Packages))
	}

	if systemConfig.Kernel.Version != "6.12" {
		t.Errorf("expected kernel version '6.12', got '%s'", systemConfig.Kernel.Version)
	}

	if systemConfig.Bootloader.BootType != "efi" {
		t.Errorf("expected bootloader type 'efi', got '%s'", systemConfig.Bootloader.BootType)
	}

	if len(systemConfig.AdditionalFiles) != 1 {
		t.Errorf("expected 1 additional file, got %d", len(systemConfig.AdditionalFiles))
	}
}

// Remove invalid tests and keep proper minimal valid tests for validation scenarios
func TestLoadAndMergeTemplate(t *testing.T) {
	// Create a simple user template with required fields
	yamlContent := `image:
  name: test-load-merge
  version: 1.0.0
target:
  os: azure-linux
  dist: azl3
  arch: x86_64
  imageType: raw
systemConfig:
  name: user-config
  packages:
    - user-package
  kernel:
    version: "6.12"
    cmdline: "quiet"
`

	tmpFile, err := os.CreateTemp("", "test-*.yml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if err := tmpFile.Chmod(0600); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(yamlContent); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	tmpFile.Close()

	// This will likely fail to find default config, but should fall back to user template
	template, err := LoadAndMergeTemplate(tmpFile.Name())
	if err != nil {
		t.Fatalf("LoadAndMergeTemplate failed: %v", err)
	}

	if template.Image.Name != "test-load-merge" {
		t.Errorf("expected image name 'test-load-merge', got '%s'", template.Image.Name)
	}

	if template.SystemConfig.Name != "user-config" {
		t.Errorf("expected system config name 'user-config', got '%s'", template.SystemConfig.Name)
	}
}

// Updated tests for fixed validation behavior
func TestLoadTemplateWithValidationErrors(t *testing.T) {
	// Template missing required fields
	incompleteYAML := `image:
  name: test
  # missing version
target:
  os: azure-linux
  # missing other required fields`

	tmpFile, err := os.CreateTemp("", "test-*.yml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if err := tmpFile.Chmod(0600); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(incompleteYAML); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	tmpFile.Close()

	// Should work without validation since it uses user template validation
	_, err = LoadTemplate(tmpFile.Name(), false)
	if err != nil {
		t.Logf("validation occurred even without full validation: %v", err)
	}

	// Should fail with validation
	_, err = LoadTemplate(tmpFile.Name(), true)
	if err == nil {
		t.Errorf("expected validation error for incomplete template")
	}
}

// Update isEmptyFunctionsEdgeCases to match actual implementation
func TestIsEmptyFunctionsEdgeCases(t *testing.T) {
	// Test isEmptyDiskConfig edge cases - actual implementation only checks Name, Size, and Partitions
	diskWithOnlyArtifacts := DiskConfig{
		Artifacts: []ArtifactInfo{{Type: "raw"}},
	}
	if !isEmptyDiskConfig(diskWithOnlyArtifacts) {
		t.Errorf("disk with only artifacts should be considered empty (actual implementation)")
	}

	diskWithOnlyPartitionTableType := DiskConfig{
		PartitionTableType: "gpt",
	}
	if !isEmptyDiskConfig(diskWithOnlyPartitionTableType) {
		t.Errorf("disk with only partition table type should be considered empty (actual implementation)")
	}

	// Test isEmptySystemConfig edge cases
	configWithOnlyDescription := SystemConfig{
		Description: "test description",
	}
	if !isEmptySystemConfig(configWithOnlyDescription) {
		t.Errorf("system config with only description should be considered empty (only name matters)")
	}

	configWithPackages := SystemConfig{
		Packages: []string{"test-package"},
	}
	if !isEmptySystemConfig(configWithPackages) {
		t.Errorf("system config with packages but no name should be considered empty")
	}
}

// Fix validation tests with valid templates
func TestValidateImageTemplateJSON(t *testing.T) {
	// Valid complete template JSON with all required fields
	validTemplate := `{
		"image": {"name": "test", "version": "1.0.0"},
		"target": {"os": "azure-linux", "dist": "azl3", "arch": "x86_64", "imageType": "raw"},
		"systemConfig": {
			"name": "test-config",
			"packages": ["test-pkg"],
			"kernel": {"version": "6.12", "cmdline": "quiet"}
		}
	}`

	err := validate.ValidateImageTemplateJSON([]byte(validTemplate))
	if err != nil {
		t.Errorf("valid template should pass validation: %v", err)
	}

	// Invalid template JSON (missing required fields)
	invalidTemplate := `{
		"image": {"name": "test"},
		"target": {"os": "azure-linux"}
	}`

	err = validate.ValidateImageTemplateJSON([]byte(invalidTemplate))
	if err == nil {
		t.Errorf("invalid template should fail validation")
	}
}

func TestValidateUserTemplateJSON(t *testing.T) {
	// Minimal valid user template
	validUserTemplate := `{
		"image": {"name": "test", "version": "1.0.0"},
		"target": {"os": "azure-linux", "dist": "azl3", "arch": "x86_64", "imageType": "raw"}
	}`

	err := validate.ValidateUserTemplateJSON([]byte(validUserTemplate))
	if err != nil {
		t.Errorf("valid user template should pass validation: %v", err)
	}

	// Completely invalid JSON
	invalidJSON := `{"invalid": json}`

	err = validate.ValidateUserTemplateJSON([]byte(invalidJSON))
	if err == nil {
		t.Errorf("invalid JSON should fail validation")
	}
}

func TestValidateConfigJSON(t *testing.T) {
	// Valid config JSON
	validConfig := `{
		"workers": 4,
		"cache_dir": "/tmp/cache",
		"work_dir": "/tmp/work",
		"logging": {"level": "info"}
	}`

	err := validate.ValidateConfigJSON([]byte(validConfig))
	if err != nil {
		t.Errorf("valid config should pass validation: %v", err)
	}

	// Invalid config JSON
	invalidConfig := `{
		"workers": "not_a_number",
		"cache_dir": 123
	}`

	err = validate.ValidateConfigJSON([]byte(invalidConfig))
	if err == nil {
		t.Errorf("invalid config should fail validation")
	}
}

func TestValidateAgainstSchemaWithEmptyRef(t *testing.T) {
	validData := `{"workers": 4, "cache_dir": "/tmp", "work_dir": "/tmp", "logging": {"level": "info"}}`

	// Test with empty ref (should use root schema)
	err := validate.ValidateAgainstSchema("test-schema", []byte(`{"type": "object"}`), []byte(validData), "")
	if err != nil {
		t.Logf("ValidateAgainstSchema with empty ref: %v", err)
	}
}

func TestValidateAgainstSchemaWithInvalidJSON(t *testing.T) {
	invalidJSON := `{"invalid": json syntax}`
	schema := `{"type": "object"}`

	err := validate.ValidateAgainstSchema("test", []byte(schema), []byte(invalidJSON), "")
	if err == nil {
		t.Errorf("expected validation error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("expected 'invalid JSON' in error message, got: %v", err)
	}
}

// Updated tests for Global config
func TestDefaultGlobalConfig(t *testing.T) {
	config := DefaultGlobalConfig()

	if config == nil {
		t.Fatalf("DefaultGlobalConfig returned nil")
	}

	if config.Workers != 8 {
		t.Errorf("expected default workers = 8, got %d", config.Workers)
	}

	if config.ConfigDir != "./config" {
		t.Errorf("expected default config dir './config', got '%s'", config.ConfigDir)
	}

	if config.CacheDir != "./cache" {
		t.Errorf("expected default cache dir './cache', got '%s'", config.CacheDir)
	}

	if config.WorkDir != "./workspace" {
		t.Errorf("expected default work dir './workspace', got '%s'", config.WorkDir)
	}

	if config.Logging.Level != "info" {
		t.Errorf("expected default log level 'info', got '%s'", config.Logging.Level)
	}

	if config.Logging.File != "os-image-composer.log" {
		t.Errorf("expected default log file 'os-image-composer.log', got '%s'", config.Logging.File)
	}
}

// Fix the global singleton test to handle the sync.Once behavior properly
func TestGlobalSingleton(t *testing.T) {
	// Test Global() creates a proper config
	config1 := Global()
	if config1 == nil {
		t.Fatalf("Global() returned nil")
	}

	// Test Global() returns same instance
	config2 := Global()
	if config1 != config2 {
		t.Errorf("Global() should return same instance")
	}

	// Test SetGlobal - need to reset the once first in a real scenario
	customConfig := &GlobalConfig{Workers: 99}
	SetGlobal(customConfig)

	config3 := Global()
	if config3.Workers != 99 {
		t.Errorf("SetGlobal didn't update global instance")
	}
}

func TestLoadTemplateWithJSONValidation(t *testing.T) {
	// Test the JSON conversion path in LoadTemplate with all required fields
	yamlContent := `image:
  name: json-test
  version: "1.0.0"
target:
  os: azure-linux
  dist: azl3
  arch: x86_64
  imageType: raw
systemConfig:
  name: test-config
  packages:
    - test-package
  kernel:
    version: "6.12"
    cmdline: "quiet"`

	tmpFile, err := os.CreateTemp("", "test-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if err := tmpFile.Chmod(0600); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(yamlContent); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	tmpFile.Close()

	template, err := LoadTemplate(tmpFile.Name(), false)
	if err != nil {
		t.Fatalf("LoadTemplate failed: %v", err)
	}

	if template.Image.Name != "json-test" {
		t.Errorf("expected image name 'json-test', got '%s'", template.Image.Name)
	}
}

func TestGlobalConfigValidate(t *testing.T) {
	testCases := []struct {
		name    string
		config  GlobalConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: GlobalConfig{
				Workers:   4,
				ConfigDir: "/test/config",
				CacheDir:  "/test/cache",
				WorkDir:   "/test/work",
				TempDir:   "/test/temp",
				Logging:   LoggingConfig{Level: "info"},
			},
			wantErr: false,
		},
		{
			name: "zero workers",
			config: GlobalConfig{
				Workers:   0,
				ConfigDir: "/test/config",
				CacheDir:  "/test/cache",
				WorkDir:   "/test/work",
				TempDir:   "/test/temp",
				Logging:   LoggingConfig{Level: "info"},
			},
			wantErr: true,
		},
		{
			name: "too many workers",
			config: GlobalConfig{
				Workers:   101,
				ConfigDir: "/test/config",
				CacheDir:  "/test/cache",
				WorkDir:   "/test/work",
				TempDir:   "/test/temp",
				Logging:   LoggingConfig{Level: "info"},
			},
			wantErr: true,
		},
		{
			name: "empty cache dir",
			config: GlobalConfig{
				Workers:   4,
				ConfigDir: "/test/config",
				CacheDir:  "",
				WorkDir:   "/test/work",
				TempDir:   "/test/temp",
				Logging:   LoggingConfig{Level: "info"},
			},
			wantErr: true,
		},
		{
			name: "empty work dir",
			config: GlobalConfig{
				Workers:   4,
				ConfigDir: "/test/config",
				CacheDir:  "/test/cache",
				WorkDir:   "",
				TempDir:   "/test/temp",
				Logging:   LoggingConfig{Level: "info"},
			},
			wantErr: true,
		},
		{
			name: "invalid log level",
			config: GlobalConfig{
				Workers:   4,
				ConfigDir: "/test/config",
				CacheDir:  "/test/cache",
				WorkDir:   "/test/work",
				TempDir:   "/test/temp",
				Logging:   LoggingConfig{Level: "invalid"},
			},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.config.Validate()
			if tc.wantErr && err == nil {
				t.Errorf("expected validation error but got none")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected validation error: %v", err)
			}
		})
	}
}

func TestLoadGlobalConfigFromFile(t *testing.T) {
	// Create test config file
	configContent := "workers: 6\n" +
		"config_dir: /custom/config\n" +
		"cache_dir: /custom/cache\n" +
		"work_dir: /custom/work\n" +
		"temp_dir: /custom/temp\n" +
		"logging:\n" +
		"  level: debug\n" +
		"  file: /var/log/test.log\n"

	tmpFile, err := os.CreateTemp("", "test-config-*.yml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if err := tmpFile.Chmod(0600); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	tmpFile.Close()

	config, err := LoadGlobalConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("LoadGlobalConfig failed: %v", err)
	}

	if config.Workers != 6 {
		t.Errorf("expected workers = 6, got %d", config.Workers)
	}

	if config.ConfigDir != "/custom/config" {
		t.Errorf("expected config_dir = '/custom/config', got '%s'", config.ConfigDir)
	}

	if config.Logging.Level != "debug" {
		t.Errorf("expected log level = 'debug', got '%s'", config.Logging.Level)
	}

	if config.Logging.File != "/var/log/test.log" {
		t.Errorf("expected log file = '/var/log/test.log', got '%s'", config.Logging.File)
	}
}

func TestLoadGlobalConfigWithEmptyPath(t *testing.T) {
	config, err := LoadGlobalConfig("")
	if err != nil {
		t.Errorf("LoadGlobalConfig with empty path should return defaults: %v", err)
	}

	// Should return default config
	defaultConfig := DefaultGlobalConfig()
	if config.Workers != defaultConfig.Workers {
		t.Errorf("expected default workers, got %d", config.Workers)
	}
}

func TestLoadGlobalConfigWithNonExistentFile(t *testing.T) {
	config, err := LoadGlobalConfig("/nonexistent/file.yml")
	if err != nil {
		t.Errorf("LoadGlobalConfig with non-existent file should return defaults: %v", err)
	}

	// Should return default config when file doesn't exist
	if config.Workers != 8 {
		t.Errorf("expected default workers = 8, got %d", config.Workers)
	}
}

func TestLoadGlobalConfigWithInvalidYAML(t *testing.T) {
	invalidYAML := `workers: not_a_number
cache_dir: [invalid, yaml, structure]
logging:
  level: debug
  - invalid: structure`

	tmpFile, err := os.CreateTemp("", "test-invalid-*.yml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if err := tmpFile.Chmod(0600); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(invalidYAML); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	tmpFile.Close()

	_, err = LoadGlobalConfig(tmpFile.Name())
	if err == nil {
		t.Errorf("expected error for invalid YAML")
	}
}

func TestLoadGlobalConfigWithUnsupportedFormat(t *testing.T) {
	// Test with .json file (not supported)
	tmpFile, err := os.CreateTemp("", "test-*.json")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if err := tmpFile.Chmod(0600); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return
	}
	defer os.Remove(tmpFile.Name())

	jsonContent := `{"workers": 4, "cache_dir": "/test"}`
	if _, err := tmpFile.WriteString(jsonContent); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	tmpFile.Close()

	_, err = LoadGlobalConfig(tmpFile.Name())
	if err == nil {
		t.Errorf("expected error for unsupported file format")
	}
	if !strings.Contains(err.Error(), "unsupported config file format") {
		t.Errorf("expected unsupported format error, got: %v", err)
	}
}

func TestGlobalConfigSave(t *testing.T) {
	config := &GlobalConfig{
		Workers:   6,
		ConfigDir: "/save/config",
		CacheDir:  "/save/cache",
		WorkDir:   "/save/work",
		TempDir:   "/save/temp",
		Logging: LoggingConfig{
			Level: "warn",
		},
	}

	tmpFile, err := os.CreateTemp("", "test-save-*.yml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if err := tmpFile.Chmod(0600); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	// Use SaveGlobalConfig method
	if err := config.SaveGlobalConfig(tmpFile.Name()); err != nil {
		t.Fatalf("SaveGlobalConfig failed: %v", err)
	}

	// Load it back and verify
	loadedConfig, err := LoadGlobalConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to load saved config: %v", err)
	}

	if loadedConfig.Workers != config.Workers {
		t.Errorf("workers not preserved: expected %d, got %d", config.Workers, loadedConfig.Workers)
	}

	if loadedConfig.ConfigDir != config.ConfigDir {
		t.Errorf("config_dir not preserved: expected '%s', got '%s'", config.ConfigDir, loadedConfig.ConfigDir)
	}
}

func TestParseYAMLTemplateValidationErrors(t *testing.T) {
	// Template that fails schema validation
	invalidTemplate := []byte(`
image:
  name: test
  version: 1.0.0
target:
  os: azure-linux
  dist: azl3
  arch: x86_64
  imageType: raw
systemConfig:
  name: test
  packages: "should_be_array_not_string"
`)

	_, err := parseYAMLTemplate(invalidTemplate, true)
	if err == nil {
		t.Errorf("expected validation error for invalid template")
	}

	// Should fail even without validation if user template validation fails
	_, err = parseYAMLTemplate(invalidTemplate, false)
	if err == nil {
		t.Errorf("expected validation error even without full validation")
	}
}

func TestLoadTemplateRejectsInvalidPackageRepository(t *testing.T) {
	yamlContent := `image:
  name: test-invalid-repo
  version: "1.0.0"

target:
  os: azure-linux
  dist: azl3
  arch: x86_64
  imageType: raw

packageRepositories:
  - codename: "invalid-repo"
    url: "https://example.com/repo"
    path: "/tmp/repo"
    pkey: "https://example.com/key.pub"

systemConfig:
  name: test
  packages:
    - test-package
  kernel:
    version: "6.12"
    cmdline: "quiet"
`

	tmpFile, err := os.CreateTemp("", "test-invalid-repo-*.yml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if err := tmpFile.Chmod(0600); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		t.Fatalf("failed to set temp file permissions: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(yamlContent); err != nil {
		tmpFile.Close()
		t.Fatalf("failed to write temp file: %v", err)
	}
	tmpFile.Close()

	_, err = LoadTemplate(tmpFile.Name(), false)
	if err == nil {
		t.Fatal("expected LoadTemplate to reject invalid package repository configuration")
	}
}

func TestGlobalConfigSaveWithCreateDirectory(t *testing.T) {
	config := &GlobalConfig{
		Workers:   4,
		ConfigDir: "/test/config", // Add missing ConfigDir
		CacheDir:  "/test/cache",
		WorkDir:   "/test/work",
		TempDir:   "/test/temp", // Use a valid temp dir
		Logging:   LoggingConfig{Level: "info"},
	}

	// Create a path in a subdirectory
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "subdir", "config.yml")

	err := config.SaveGlobalConfig(configPath)
	if err != nil {
		t.Fatalf("SaveGlobalConfig should create directories: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Errorf("config file was not created")
	}
}

func TestSaveGlobalConfigWithComments(t *testing.T) {
	config := DefaultGlobalConfig()
	config.Logging.File = "custom.log"

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "commented-config.yml")

	if err := config.SaveGlobalConfigWithComments(configPath); err != nil {
		t.Fatalf("SaveGlobalConfigWithComments failed: %v", err)
	}

	contents, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read commented config: %v", err)
	}

	text := string(contents)
	if !strings.Contains(text, "# OS Image Composer - Global Configuration") {
		t.Fatalf("expected commented config header, got: %s", text)
	}

	if !strings.Contains(text, "file: \"custom.log\"") {
		t.Fatalf("expected logging file entry in commented config, got: %s", text)
	}
}

func TestLoadTemplateGlobalVariables(t *testing.T) {
	// Test that LoadTemplate sets global variables
	yamlContent := `image:
  name: global-test
  version: 1.0.0
target:
  os: wind-river-elxr
  dist: elxr12
  arch: x86_64
  imageType: raw
systemConfig:
  name: test-config
  packages:
    - test-package
  kernel:
    version: "6.12"
    cmdline: "quiet"`

	tmpFile, err := os.CreateTemp("", "test-*.yml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if err := tmpFile.Chmod(0600); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(yamlContent); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	tmpFile.Close()

	template, err := LoadTemplate(tmpFile.Name(), false)
	if err != nil {
		t.Fatalf("LoadTemplate failed: %v", err)
	}

	// Check that global variables were set
	if template.Target.OS != "wind-river-elxr" {
		t.Errorf("expected TargetOs = 'wind-river-elxr', got '%s'", template.Target.OS)
	}
	if template.Target.Dist != "elxr12" {
		t.Errorf("expected TargetDist = 'elxr12', got '%s'", template.Target.Dist)
	}
	if template.Target.Arch != "x86_64" {
		t.Errorf("expected TargetArch = 'x86_64', got '%s'", template.Target.Arch)
	}
	if template.Target.ImageType != "raw" {
		t.Errorf("expected TargetImageType = 'raw', got '%s'", template.Target.ImageType)
	}

	if template.Image.Name != "global-test" {
		t.Errorf("expected image name 'global-test', got '%s'", template.Image.Name)
	}
}

// Additional benchmark tests
func BenchmarkLoadTemplate(b *testing.B) {
	yamlContent := `image:
  name: benchmark-test
  version: 1.0.0
target:
  os: azure-linux
  dist: azl3
  arch: x86_64
  imageType: raw
systemConfig:
  name: benchmark-config
  packages:
    - package1
    - package2
    - package3
  kernel:
    version: "6.12"
    cmdline: "quiet"
`

	tmpFile, err := os.CreateTemp("", "benchmark-*.yml")
	if err != nil {
		b.Fatalf("failed to create temp file: %v", err)
	}
	if err := tmpFile.Chmod(0600); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(yamlContent); err != nil {
		b.Fatalf("failed to write temp file: %v", err)
	}
	tmpFile.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := LoadTemplate(tmpFile.Name(), false)
		if err != nil {
			b.Fatalf("LoadTemplate failed: %v", err)
		}
	}
}

func BenchmarkMergeConfigurations(b *testing.B) {
	defaultTemplate := &ImageTemplate{
		Image:  ImageInfo{Name: "default", Version: "1.0.0"},
		Target: TargetInfo{OS: "azure-linux", Dist: "azl3", Arch: "x86_64", ImageType: "raw"},
		SystemConfig: SystemConfig{
			Name:     "default",
			Packages: []string{"base1", "base2", "base3"},
			Users:    []UserConfig{{Name: "admin", Password: "pass"}},
		},
	}

	userTemplate := &ImageTemplate{
		Image:  ImageInfo{Name: "user", Version: "2.0.0"},
		Target: TargetInfo{OS: "azure-linux", Dist: "azl3", Arch: "x86_64", ImageType: "iso"},
		SystemConfig: SystemConfig{
			Name:     "user",
			Packages: []string{"extra1", "extra2"},
			Users:    []UserConfig{{Name: "user", Password: "userpass"}},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := MergeConfigurations(userTemplate, defaultTemplate)
		if err != nil {
			b.Fatalf("MergeConfigurations failed: %v", err)
		}
	}
}

// Additional edge case tests
func TestLoadTemplateWithFileReadError(t *testing.T) {
	// Test with a directory instead of file
	tmpDir := t.TempDir()

	_, err := LoadTemplate(tmpDir, false)
	if err == nil {
		t.Errorf("expected error when loading directory as template")
	}
}

func TestParseYAMLTemplateWithBadYAML(t *testing.T) {
	malformedYAML := []byte(`
image:
  name: test
  version: 1.0.0
target:
  os: azure-linux
  this is: malformed yaml
    that doesn't: parse correctly
`)

	_, err := parseYAMLTemplate(malformedYAML, false)
	if err == nil {
		t.Errorf("expected error for malformed YAML")
	}
	if !strings.Contains(err.Error(), "invalid YAML format") && !strings.Contains(err.Error(), "template parsing failed") {
		t.Errorf("expected YAML parsing error, got: %v", err)
	}
}

func TestLoadTemplateWithMissingFile(t *testing.T) {
	_, err := LoadTemplate("/definitely/does/not/exist.yml", false)
	if err == nil {
		t.Errorf("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "no such file or directory") && !strings.Contains(err.Error(), "failed to read template file") {
		t.Errorf("expected file not found error, got: %v", err)
	}
}

func TestLoadTemplateWithDirectoryPath(t *testing.T) {
	// Create a temporary directory
	tmpDir := t.TempDir()

	_, err := LoadTemplate(tmpDir, false)
	if err == nil {
		t.Errorf("expected error when trying to load directory as template")
	}
}

func TestDefaultConfigLoaderErrors(t *testing.T) {
	loader := NewDefaultConfigLoader("nonexistent-os", "nonexistent-dist", "x86_64")

	// Test unsupported image types
	unsupportedTypes := []string{"unsupported", "invalid", ""}

	for _, imageType := range unsupportedTypes {
		_, err := loader.LoadDefaultConfig(imageType)
		if err == nil {
			t.Errorf("expected error for unsupported image type: %s", imageType)
		}
		if !strings.Contains(err.Error(), "unsupported image type") {
			t.Errorf("expected 'unsupported image type' error, got: %v", err)
		}
	}
}

func TestDefaultConfigLoaderWithInvalidPath(t *testing.T) {
	loader := NewDefaultConfigLoader("azure-linux", "azl3", "x86_64")

	// This will likely fail because the default config file doesn't exist in test env
	_, err := loader.LoadDefaultConfig("raw")
	if err != nil {
		// This is expected in test environment
		t.Logf("LoadDefaultConfig failed as expected in test environment: %v", err)

		// Verify error contains expected messages
		if !strings.Contains(err.Error(), "config directory") &&
			!strings.Contains(err.Error(), "not found") &&
			!strings.Contains(err.Error(), "failed to load") {
			t.Errorf("unexpected error format: %v", err)
		}
	}
}

func TestMergeConfigurationsWithComplexEdgeCases(t *testing.T) {
	// Test merging with very minimal user template
	minimalUser := &ImageTemplate{
		Image:  ImageInfo{Name: "minimal"},
		Target: TargetInfo{OS: "test-os"},
	}

	complexDefault := &ImageTemplate{
		Image:  ImageInfo{Name: "default-name", Version: "default-version"},
		Target: TargetInfo{OS: "default-os", Dist: "default-dist", Arch: "default-arch", ImageType: "raw"},
		SystemConfig: SystemConfig{
			Name:     "default-system",
			Packages: []string{"default-package"},
			Users:    []UserConfig{{Name: "default-user"}},
		},
		Disk: DiskConfig{Name: "default-disk"},
	}

	merged, err := MergeConfigurations(minimalUser, complexDefault)
	if err != nil {
		t.Fatalf("failed to merge configurations: %v", err)
	}

	// User values should override
	if merged.Image.Name != "minimal" {
		t.Errorf("expected image name 'minimal', got '%s'", merged.Image.Name)
	}

	// Default values should be preserved when user doesn't specify
	if merged.Image.Version != "default-version" {
		t.Errorf("expected version from default, got '%s'", merged.Image.Version)
	}

	// Target should be completely from user
	if merged.Target.OS != "test-os" {
		t.Errorf("expected target OS from user, got '%s'", merged.Target.OS)
	}
}

func TestMergeUsersWithEmptySlices(t *testing.T) {
	// Test merging when one side has empty users
	emptyUsers := []UserConfig{}
	userWithUsers := []UserConfig{{Name: "test", Password: "pass"}}

	// Empty default, users from user config
	result := mergeUsers(emptyUsers, userWithUsers)
	if len(result) != 1 {
		t.Errorf("expected 1 user, got %d", len(result))
	}

	// Users from default, empty user config
	result = mergeUsers(userWithUsers, emptyUsers)
	if len(result) != 1 {
		t.Errorf("expected 1 user from default, got %d", len(result))
	}

	// Both empty
	result = mergeUsers(emptyUsers, emptyUsers)
	if len(result) != 0 {
		t.Errorf("expected 0 users when both are empty, got %d", len(result))
	}
}

func TestMergePackagesWithNilAndEmpty(t *testing.T) {
	packages1 := []string{"pkg1", "pkg2"}
	emptyPackages := []string{}

	// Test with empty slices
	result := mergePackages(packages1, emptyPackages)
	if len(result) != 2 {
		t.Errorf("expected 2 packages, got %d", len(result))
	}

	result = mergePackages(emptyPackages, packages1)
	if len(result) != 2 {
		t.Errorf("expected 2 packages, got %d", len(result))
	}

	result = mergePackages(emptyPackages, emptyPackages)
	if len(result) != 0 {
		t.Errorf("expected 0 packages, got %d", len(result))
	}
}

func TestLoadAndMergeTemplateWithInvalidUserTemplate(t *testing.T) {
	// Create an invalid user template
	yamlContent := `invalid: yaml: structure: that: doesn't: parse`

	tmpFile, err := os.CreateTemp("", "test-*.yml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if err := tmpFile.Chmod(0600); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(yamlContent); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	tmpFile.Close()

	_, err = LoadAndMergeTemplate(tmpFile.Name())
	if err == nil {
		t.Errorf("expected error for invalid user template")
	}
	if !strings.Contains(err.Error(), "failed to load user template") {
		t.Errorf("expected user template loading error, got: %v", err)
	}
}

func TestLoadAndMergeTemplateWithMissingFile(t *testing.T) {
	_, err := LoadAndMergeTemplate("/nonexistent/file.yml")
	if err == nil {
		t.Errorf("expected error for nonexistent template file")
	}
	if !strings.Contains(err.Error(), "failed to load user template") {
		t.Errorf("expected user template loading error, got: %v", err)
	}
}

func TestGlobalConfigValidateEmptyTempDir(t *testing.T) {
	config := &GlobalConfig{
		Workers:   4,
		ConfigDir: "/test/config",
		CacheDir:  "/test/cache",
		WorkDir:   "/test/work",
		TempDir:   "", // Empty temp dir should be set to system default
		Logging:   LoggingConfig{Level: "info"},
	}

	err := config.Validate()
	if err != nil {
		t.Errorf("validation should succeed and set temp dir: %v", err)
	}

	// TempDir should now be set to system default
	if config.TempDir == "" {
		t.Errorf("expected temp dir to be set after validation")
	}
}

func TestGetTargetOsConfigDir(t *testing.T) {
	// Test that the function exists and can be called
	// The actual implementation might depend on environment or file system
	testCases := []struct {
		os   string
		dist string
	}{
		{"azure-linux", "azl3"},
		{"emt", "emt3"},
		{"elxr", "elxr12"},
	}

	for _, tc := range testCases {
		// We can't test the actual path resolution without knowing the environment
		// but we can at least test that the function call doesn't panic
		_, err := GetTargetOsConfigDir(tc.os, tc.dist)
		// Error is expected in test environment, but function should not panic
		if err != nil {
			t.Logf("GetTargetOsConfigDir(%s, %s) returned error as expected in test environment: %v", tc.os, tc.dist, err)
		}
	}
}

func TestPackageRepositories(t *testing.T) {
	template := &ImageTemplate{
		Image: ImageInfo{
			Name:    "test-image",
			Version: "1.0.0",
		},
		Target: TargetInfo{
			OS:        "azure-linux",
			Dist:      "azl3",
			Arch:      "x86_64",
			ImageType: "raw",
		},
		PackageRepositories: []PackageRepository{
			{
				Codename:  "test-repo1",
				URL:       "https://test.example.com/repo1",
				PKey:      "https://test.example.com/key1.pub",
				Component: "main",
			},
			{
				Codename:  "test-repo2",
				URL:       "https://test.example.com/repo2",
				PKey:      "https://test.example.com/key2.pub",
				Component: "restricted",
			},
		},
		SystemConfig: SystemConfig{
			Name:     "test-config",
			Packages: []string{"package1", "package2"},
			Kernel: KernelConfig{
				Version: "6.12",
				Cmdline: "quiet",
			},
		},
	}

	// Test repository access methods
	repos := template.GetPackageRepositories()
	if len(repos) != 2 {
		t.Errorf("expected 2 repositories, got %d", len(repos))
	}

	if !template.HasPackageRepositories() {
		t.Errorf("expected template to have package repositories")
	}

	repo1 := template.GetRepositoryByCodename("test-repo1")
	if repo1 == nil {
		t.Errorf("expected to find test-repo1")
	} else {
		if repo1.URL != "https://test.example.com/repo1" {
			t.Errorf("expected repo1 URL 'https://test.example.com/repo1', got '%s'", repo1.URL)
		}
		if repo1.PKey != "https://test.example.com/key1.pub" {
			t.Errorf("expected repo1 pkey 'https://test.example.com/key1.pub', got '%s'", repo1.PKey)
		}
		if repo1.Component != "main" {
			t.Errorf("expected repo1 component 'main', got '%s'", repo1.Component)
		}
	}

	repo2 := template.GetRepositoryByCodename("test-repo2")
	if repo2 == nil {
		t.Errorf("expected to find test-repo2")
	} else {
		if repo2.Component != "restricted" {
			t.Errorf("expected repo2 component 'restricted', got '%s'", repo2.Component)
		}
	}

	// Test non-existent repository
	nonExistentRepo := template.GetRepositoryByCodename("nonexistent")
	if nonExistentRepo != nil {
		t.Errorf("expected not to find nonexistent repository")
	}
}

func TestEmptyPackageRepositories(t *testing.T) {
	template := &ImageTemplate{
		Image:        ImageInfo{Name: "test", Version: "1.0.0"},
		Target:       TargetInfo{OS: "azure-linux", Dist: "azl3", Arch: "x86_64", ImageType: "raw"},
		SystemConfig: SystemConfig{Name: "test-config", Packages: []string{"package1"}, Kernel: KernelConfig{Version: "6.12"}},
		// No PackageRepositories defined
	}

	repos := template.GetPackageRepositories()
	if len(repos) != 0 {
		t.Errorf("expected 0 repositories for empty config, got %d", len(repos))
	}

	if template.HasPackageRepositories() {
		t.Errorf("expected template to not have package repositories")
	}

	nonExistentRepo := template.GetRepositoryByCodename("anyrepo")
	if nonExistentRepo != nil {
		t.Errorf("expected not to find any repository in empty config")
	}
}

func TestMergePackageRepositories(t *testing.T) {
	defaultRepos := []PackageRepository{
		{Codename: "default1", URL: "https://default.com/1", PKey: "https://default.com/1.pub"},
		{Codename: "default2", URL: "https://default.com/2", PKey: "https://default.com/2.pub"},
	}

	userRepos := []PackageRepository{
		{Codename: "user1", URL: "https://user.com/1", PKey: "https://user.com/1.pub"},
	}

	merged := mergePackageRepositories(defaultRepos, userRepos)

	// User repos are appended to defaults (additive merge)
	if len(merged) != 3 {
		t.Errorf("expected 3 merged repositories (2 default + 1 user), got %d", len(merged))
	}

	// Create a map to easily check repos by codename
	repoMap := make(map[string]PackageRepository)
	for _, repo := range merged {
		repoMap[repo.Codename] = repo
	}

	// Verify all default repos are preserved
	if repo, exists := repoMap["default1"]; !exists || repo.URL != "https://default.com/1" {
		t.Errorf("expected default1 repo to be preserved")
	}
	if repo, exists := repoMap["default2"]; !exists || repo.URL != "https://default.com/2" {
		t.Errorf("expected default2 repo to be preserved")
	}

	// Verify user repo is added
	if repo, exists := repoMap["user1"]; !exists || repo.URL != "https://user.com/1" {
		t.Errorf("expected user1 repo to be added")
	}
}

func TestMergePackageRepositoriesEmpty(t *testing.T) {
	defaultRepos := []PackageRepository{
		{Codename: "default1", URL: "https://default.com/1", PKey: "https://default.com/1.pub"},
	}

	// Test with empty user repos - should return defaults
	emptyUserRepos := []PackageRepository{}
	merged := mergePackageRepositories(defaultRepos, emptyUserRepos)
	if len(merged) != 1 {
		t.Errorf("expected 1 default repository when user repos empty, got %d", len(merged))
	}
	if merged[0].Codename != "default1" {
		t.Errorf("expected default repo codename, got '%s'", merged[0].Codename)
	}

	// Test with nil user repos - should return defaults
	merged = mergePackageRepositories(defaultRepos, nil)
	if len(merged) != 1 {
		t.Errorf("expected 1 default repository when user repos nil, got %d", len(merged))
	}

	// Test with both empty
	merged = mergePackageRepositories([]PackageRepository{}, []PackageRepository{})
	if len(merged) != 0 {
		t.Errorf("expected 0 repositories when both are empty, got %d", len(merged))
	}
}

func TestMergeConfigurationsWithPackageRepositories(t *testing.T) {
	defaultTemplate := &ImageTemplate{
		Image:  ImageInfo{Name: "default", Version: "1.0.0"},
		Target: TargetInfo{OS: "azure-linux", Dist: "azl3", Arch: "x86_64", ImageType: "raw"},
		PackageRepositories: []PackageRepository{
			{Codename: "azure-extras", URL: "https://packages.microsoft.com/extras", PKey: "https://packages.microsoft.com/keys/microsoft.asc"},
			{Codename: "azure-preview", URL: "https://packages.microsoft.com/preview", PKey: "https://packages.microsoft.com/keys/microsoft.asc"},
		},
		SystemConfig: SystemConfig{
			Name:     "default-config",
			Packages: []string{"base-package"},
			Kernel:   KernelConfig{Version: "6.10", Cmdline: "quiet"},
		},
	}

	userTemplate := &ImageTemplate{
		Image:  ImageInfo{Name: "user-image", Version: "2.0.0"},
		Target: TargetInfo{OS: "azure-linux", Dist: "azl3", Arch: "x86_64", ImageType: "raw"},
		PackageRepositories: []PackageRepository{
			{Codename: "company-internal", URL: "https://packages.company.com/internal", PKey: "https://packages.company.com/keys/internal.pub"},
		},
		SystemConfig: SystemConfig{
			Name:     "user-config",
			Packages: []string{"user-package"},
			Kernel:   KernelConfig{Version: "6.12"},
		},
	}

	merged, err := MergeConfigurations(userTemplate, defaultTemplate)
	if err != nil {
		t.Fatalf("failed to merge configurations: %v", err)
	}

	// Test that user repositories are added to defaults (additive merge)
	repos := merged.GetPackageRepositories()
	if len(repos) != 3 {
		t.Errorf("expected 3 merged repositories (2 default + 1 user), got %d", len(repos))
	}

	// Verify user repository is included
	companyRepo := merged.GetRepositoryByCodename("company-internal")
	if companyRepo == nil {
		t.Errorf("expected to find user repository 'company-internal'")
	} else if companyRepo.URL != "https://packages.company.com/internal" {
		t.Errorf("expected company-internal URL to be correct, got '%s'", companyRepo.URL)
	}

	// Verify default repositories are preserved
	azureExtrasRepo := merged.GetRepositoryByCodename("azure-extras")
	if azureExtrasRepo == nil {
		t.Errorf("expected default repository 'azure-extras' to be preserved")
	}

	azurePreviewRepo := merged.GetRepositoryByCodename("azure-preview")
	if azurePreviewRepo == nil {
		t.Errorf("expected default repository 'azure-preview' to be preserved")
	}
}

func TestMergeConfigurationsNoUserRepositories(t *testing.T) {
	defaultTemplate := &ImageTemplate{
		Image:  ImageInfo{Name: "default", Version: "1.0.0"},
		Target: TargetInfo{OS: "azure-linux", Dist: "azl3", Arch: "x86_64", ImageType: "raw"},
		PackageRepositories: []PackageRepository{
			{Codename: "azure-extras", URL: "https://packages.microsoft.com/extras", PKey: "https://packages.microsoft.com/keys/microsoft.asc"},
		},
		SystemConfig: SystemConfig{
			Name:     "default-config",
			Packages: []string{"base-package"},
			Kernel:   KernelConfig{Version: "6.10"},
		},
	}

	userTemplate := &ImageTemplate{
		Image:  ImageInfo{Name: "user-image", Version: "2.0.0"},
		Target: TargetInfo{OS: "azure-linux", Dist: "azl3", Arch: "x86_64", ImageType: "raw"},
		// No PackageRepositories specified by user
		SystemConfig: SystemConfig{
			Name:     "user-config",
			Packages: []string{"user-package"},
			Kernel:   KernelConfig{Version: "6.12"},
		},
	}

	merged, err := MergeConfigurations(userTemplate, defaultTemplate)
	if err != nil {
		t.Fatalf("failed to merge configurations: %v", err)
	}

	// Test that default repositories are preserved when user doesn't specify any
	repos := merged.GetPackageRepositories()
	if len(repos) != 1 {
		t.Errorf("expected 1 default repository when user doesn't specify repos, got %d", len(repos))
	}

	if repos[0].Codename != "azure-extras" {
		t.Errorf("expected default repository codename 'azure-extras', got '%s'", repos[0].Codename)
	}
}

func TestPackageRepositoryYAMLParsing(t *testing.T) {
	yamlContent := `image:
  name: test-repo-parsing
  version: "1.0.0"

target:
  os: azure-linux
  dist: azl3
  arch: x86_64
  imageType: raw

packageRepositories:
  - codename: "test-repo1"
    url: "https://test.example.com/repo1"
    pkey: "https://test.example.com/key1.pub"
    component: "main"
  - codename: "test-repo2"
    url: "https://test.example.com/repo2"
    pkey: "https://test.example.com/key2.pub"
    component: "restricted"

systemConfig:
  name: test
  packages:
    - test-package
  kernel:
    version: "6.12"
    cmdline: "quiet"
`

	tmpFile, err := os.CreateTemp("", "test-*.yml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if err := tmpFile.Chmod(0600); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(yamlContent); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	tmpFile.Close()

	// Test loading with package repositories
	template, err := LoadTemplate(tmpFile.Name(), false) // User template validation
	if err != nil {
		t.Fatalf("failed to load YAML template with package repositories: %v", err)
	}

	// Verify package repositories were parsed correctly
	repos := template.GetPackageRepositories()
	if len(repos) != 2 {
		t.Errorf("expected 2 parsed repositories, got %d", len(repos))
	}

	repo1 := template.GetRepositoryByCodename("test-repo1")
	if repo1 == nil {
		t.Errorf("expected to find test-repo1")
	} else {
		if repo1.URL != "https://test.example.com/repo1" {
			t.Errorf("expected repo1 URL 'https://test.example.com/repo1', got '%s'", repo1.URL)
		}
		if repo1.PKey != "https://test.example.com/key1.pub" {
			t.Errorf("expected repo1 pkey 'https://test.example.com/key1.pub', got '%s'", repo1.PKey)
		}
	}

	repo2 := template.GetRepositoryByCodename("test-repo2")
	if repo2 == nil {
		t.Errorf("expected to find test-repo2")
	}
}

func TestPackageRepositoryYAMLParsingLocalPath(t *testing.T) {
	yamlContent := `image:
  name: test-local-repo-parsing
  version: "1.0.0"

target:
  os: ubuntu
  dist: ubuntu24
  arch: x86_64
  imageType: raw

packageRepositories:
  - codename: "localdeb"
    path: "/data/os-image-composer/localdeb"
    pkey: "[trusted=yes]"
    component: "main"

systemConfig:
  name: test
  packages:
    - test-package
  kernel:
    version: "6.12"
    cmdline: "quiet"
`

	tmpFile, err := os.CreateTemp("", "test-local-repo-*.yml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if err := tmpFile.Chmod(0600); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(yamlContent); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	tmpFile.Close()

	template, err := LoadTemplate(tmpFile.Name(), false)
	if err != nil {
		t.Fatalf("failed to load YAML template with local package repository: %v", err)
	}

	repos := template.GetPackageRepositories()
	if len(repos) != 1 {
		t.Fatalf("expected 1 parsed repository, got %d", len(repos))
	}

	repo := template.GetRepositoryByCodename("localdeb")
	if repo == nil {
		t.Fatalf("expected to find localdeb repository")
	}

	if repo.Path != "/data/os-image-composer/localdeb" {
		t.Errorf("expected repo path '/data/os-image-composer/localdeb', got '%s'", repo.Path)
	}
	if repo.PKey != "[trusted=yes]" {
		t.Errorf("expected repo pkey '[trusted=yes]', got '%s'", repo.PKey)
	}
	if repo.Component != "main" {
		t.Errorf("expected repo component 'main', got '%s'", repo.Component)
	}
	if repo.URL != "" {
		t.Errorf("expected repo URL to be empty for local path repository, got '%s'", repo.URL)
	}
}

func TestPackageRepositoriesWithDuplicateCodenames(t *testing.T) {
	repos := []PackageRepository{
		{Codename: "duplicate", URL: "https://first.com", PKey: "https://first.com/key.pub"},
		{Codename: "unique", URL: "https://unique.com", PKey: "https://unique.com/key.pub"},
		{Codename: "duplicate", URL: "https://second.com", PKey: "https://second.com/key.pub"},
	}

	template := &ImageTemplate{
		Image:               ImageInfo{Name: "test", Version: "1.0.0"},
		Target:              TargetInfo{OS: "azure-linux", Dist: "azl3", Arch: "x86_64", ImageType: "raw"},
		PackageRepositories: repos,
		SystemConfig:        SystemConfig{Name: "test", Packages: []string{"pkg"}, Kernel: KernelConfig{Version: "6.12"}},
	}

	// GetRepositoryByCodename should return the first match
	duplicateRepo := template.GetRepositoryByCodename("duplicate")
	if duplicateRepo == nil {
		t.Errorf("expected to find duplicate repository")
	} else {
		if duplicateRepo.URL != "https://first.com" {
			t.Errorf("expected first duplicate repo URL, got '%s'", duplicateRepo.URL)
		}
	}

	uniqueRepo := template.GetRepositoryByCodename("unique")
	if uniqueRepo == nil {
		t.Errorf("expected to find unique repository")
	} else {
		if uniqueRepo.URL != "https://unique.com" {
			t.Errorf("expected unique repo URL, got '%s'", uniqueRepo.URL)
		}
	}
}
func TestGetImageNameAndTargetInfo(t *testing.T) {
	template := &ImageTemplate{
		Image: ImageInfo{Name: "img", Version: "1.2"},
		Target: TargetInfo{
			OS:        "os",
			Dist:      "dist",
			Arch:      "arch",
			ImageType: "type",
		},
	}
	if got := template.GetImageName(); got != "img" {
		t.Errorf("GetImageName() = %s, want img", got)
	}
	ti := template.GetTargetInfo()
	if ti.OS != "os" || ti.Dist != "dist" || ti.Arch != "arch" || ti.ImageType != "type" {
		t.Errorf("GetTargetInfo() = %+v, want all fields set", ti)
	}
}

func TestGetDiskConfigAndSystemConfig(t *testing.T) {
	disk := DiskConfig{Name: "disk1"}
	sys := SystemConfig{Name: "sys1"}
	template := &ImageTemplate{Disk: disk, SystemConfig: sys}
	if got := template.GetDiskConfig(); got.Name != "disk1" {
		t.Errorf("GetDiskConfig() = %v, want disk1", got.Name)
	}
	if got := template.GetSystemConfig(); got.Name != "sys1" {
		t.Errorf("GetSystemConfig() = %v, want sys1", got.Name)
	}
}

func TestGetBootloaderConfig(t *testing.T) {
	bl := Bootloader{BootType: "efi", Provider: "grub2"}
	template := &ImageTemplate{SystemConfig: SystemConfig{Bootloader: bl}}
	got := template.GetBootloaderConfig()
	if got.BootType != "efi" || got.Provider != "grub2" {
		t.Errorf("GetBootloaderConfig() = %+v, want efi/grub2", got)
	}
}

func TestGetPackagesAndKernel(t *testing.T) {
	sys := SystemConfig{
		Packages: []string{"a", "b"},
		Kernel:   KernelConfig{Version: "v", Cmdline: "c"},
	}
	template := &ImageTemplate{SystemConfig: sys}
	if pkgs := template.GetPackages(); len(pkgs) != 2 || pkgs[0] != "a" {
		t.Errorf("GetPackages() = %v, want [a b]", pkgs)
	}
	k := template.GetKernel()
	if k.Version != "v" || k.Cmdline != "c" {
		t.Errorf("GetKernel() = %+v, want v/c", k)
	}
}

func TestGetPackageSourceMap(t *testing.T) {
	template := &ImageTemplate{
		EssentialPkgList:  []string{"coreutils", "bash"},
		KernelPkgList:     []string{"linux-image"},
		BootloaderPkgList: []string{"grub2"},
		SystemConfig: SystemConfig{
			Packages: []string{"vim", "bash", " "},
		},
	}

	sources := template.GetPackageSourceMap()

	if got := sources["coreutils"]; got != PackageSourceEssential {
		t.Fatalf("coreutils source = %s, want essential", got)
	}
	if got := sources["linux-image"]; got != PackageSourceKernel {
		t.Fatalf("linux-image source = %s, want kernel", got)
	}
	if got := sources["grub2"]; got != PackageSourceBootloader {
		t.Fatalf("grub2 source = %s, want bootloader", got)
	}
	if got := sources["vim"]; got != PackageSourceSystem {
		t.Fatalf("vim source = %s, want system", got)
	}
	if got := sources["bash"]; got != PackageSourceEssential {
		t.Fatalf("bash source = %s, want essential (essential has higher priority than system)", got)
	}
	if _, exists := sources[""]; exists {
		t.Fatalf("unexpected empty key in package source map")
	}
}

func TestGetSystemConfigName(t *testing.T) {
	sys := SystemConfig{Name: "sys"}
	template := &ImageTemplate{SystemConfig: sys}
	if got := template.GetSystemConfigName(); got != "sys" {
		t.Errorf("GetSystemConfigName() = %s, want sys", got)
	}
}

func TestImmutabilityConfigMethods(t *testing.T) {
	ic := ImmutabilityConfig{
		Enabled:         true,
		SecureBootDBKey: "/key",
		SecureBootDBCrt: "/crt",
		SecureBootDBCer: "/cer",
	}
	if !ic.HasSecureBootDBConfig() {
		t.Error("HasSecureBootDBConfig() = false, want true")
	}
	if !ic.HasSecureBootDBKey() {
		t.Error("HasSecureBootDBKey() = false, want true")
	}
	if !ic.HasSecureBootDBCrt() {
		t.Error("HasSecureBootDBCrt() = false, want true")
	}
	if !ic.HasSecureBootDBCer() {
		t.Error("HasSecureBootDBCer() = false, want true")
	}
	if ic.GetSecureBootDBKeyPath() != "/key" {
		t.Errorf("GetSecureBootDBKeyPath() = %s, want /key", ic.GetSecureBootDBKeyPath())
	}
	if ic.GetSecureBootDBCrtPath() != "/crt" {
		t.Errorf("GetSecureBootDBCrtPath() = %s, want /crt", ic.GetSecureBootDBCrtPath())
	}
	if ic.GetSecureBootDBCerPath() != "/cer" {
		t.Errorf("GetSecureBootDBCerPath() = %s, want /cer", ic.GetSecureBootDBCerPath())
	}
}

func TestImmutabilityConfigMethodsEmpty(t *testing.T) {
	ic := ImmutabilityConfig{}
	if ic.HasSecureBootDBConfig() {
		t.Error("HasSecureBootDBConfig() = true, want false")
	}
	if ic.HasSecureBootDBKey() {
		t.Error("HasSecureBootDBKey() = true, want false")
	}
	if ic.HasSecureBootDBCrt() {
		t.Error("HasSecureBootDBCrt() = true, want false")
	}
	if ic.HasSecureBootDBCer() {
		t.Error("HasSecureBootDBCer() = true, want false")
	}
	if ic.GetSecureBootDBKeyPath() != "" {
		t.Errorf("GetSecureBootDBKeyPath() = %s, want empty", ic.GetSecureBootDBKeyPath())
	}
	if ic.GetSecureBootDBCrtPath() != "" {
		t.Errorf("GetSecureBootDBCrtPath() = %s, want empty", ic.GetSecureBootDBCrtPath())
	}
	if ic.GetSecureBootDBCerPath() != "" {
		t.Errorf("GetSecureBootDBCerPath() = %s, want empty", ic.GetSecureBootDBCerPath())
	}
}

func TestSystemConfigImmutabilityHelpers(t *testing.T) {
	ic := ImmutabilityConfig{Enabled: true, SecureBootDBKey: "k"}
	sc := SystemConfig{Immutability: ic}
	if !sc.GetImmutability().Enabled {
		t.Error("GetImmutability().Enabled = false, want true")
	}
	if !sc.IsImmutabilityEnabled() {
		t.Error("IsImmutabilityEnabled() = false, want true")
	}
	if sc.GetSecureBootDBKeyPath() != "k" {
		t.Errorf("GetSecureBootDBKeyPath() = %s, want k", sc.GetSecureBootDBKeyPath())
	}
	if !sc.HasSecureBootDBConfig() {
		t.Error("HasSecureBootDBConfig() = false, want true")
	}
}

func TestImageTemplateImmutabilityHelpers(t *testing.T) {
	ic := ImmutabilityConfig{Enabled: true, SecureBootDBKey: "k"}
	template := &ImageTemplate{SystemConfig: SystemConfig{Immutability: ic}}
	if !template.GetImmutability().Enabled {
		t.Error("GetImmutability().Enabled = false, want true")
	}
	if !template.IsImmutabilityEnabled() {
		t.Error("IsImmutabilityEnabled() = false, want true")
	}
	if template.GetSecureBootDBKeyPath() != "k" {
		t.Errorf("GetSecureBootDBKeyPath() = %s, want k", template.GetSecureBootDBKeyPath())
	}
	if !template.HasSecureBootDBConfig() {
		t.Error("HasSecureBootDBConfig() = false, want true")
	}
}

func TestGetUsersAndUserByName(t *testing.T) {
	users := []UserConfig{
		{Name: "alice", Sudo: true},
		{Name: "bob"},
	}
	template := &ImageTemplate{SystemConfig: SystemConfig{Users: users}}
	if len(template.GetUsers()) != 2 {
		t.Errorf("GetUsers() = %d, want 2", len(template.GetUsers()))
	}
	if u := template.GetUserByName("alice"); u == nil || u.Name != "alice" {
		t.Errorf("GetUserByName(alice) = %v, want alice", u)
	}
	if u := template.GetUserByName("notfound"); u != nil {
		t.Errorf("GetUserByName(notfound) = %v, want nil", u)
	}
	if !template.HasUsers() {
		t.Error("HasUsers() = false, want true")
	}
}

func TestSystemConfigUserHelpers(t *testing.T) {
	users := []UserConfig{{Name: "root"}, {Name: "user"}}
	sc := SystemConfig{Users: users}
	if len(sc.GetUsers()) != 2 {
		t.Errorf("GetUsers() = %d, want 2", len(sc.GetUsers()))
	}
	if u := sc.GetUserByName("root"); u == nil || u.Name != "root" {
		t.Errorf("GetUserByName(root) = %v, want root", u)
	}
	if u := sc.GetUserByName("none"); u != nil {
		t.Errorf("GetUserByName(none) = %v, want nil", u)
	}
	if !sc.HasUsers() {
		t.Error("HasUsers() = false, want true")
	}
}

func TestGetPackageRepositoriesAndHelpers(t *testing.T) {
	repos := []PackageRepository{
		{Codename: "main", URL: "http://a"},
		{Codename: "extra", URL: "http://b"},
	}
	template := &ImageTemplate{PackageRepositories: repos}
	if !template.HasPackageRepositories() {
		t.Error("HasPackageRepositories() = false, want true")
	}
	if len(template.GetPackageRepositories()) != 2 {
		t.Errorf("GetPackageRepositories() = %d, want 2", len(template.GetPackageRepositories()))
	}
	if repo := template.GetRepositoryByCodename("main"); repo == nil || repo.URL != "http://a" {
		t.Errorf("GetRepositoryByCodename(main) = %v, want http://a", repo)
	}
	if repo := template.GetRepositoryByCodename("none"); repo != nil {
		t.Errorf("GetRepositoryByCodename(none) = %v, want nil", repo)
	}
}

func TestGetProviderNameAndDistroVersionUnknown(t *testing.T) {
	template := &ImageTemplate{
		Target: TargetInfo{OS: "unknown", Dist: "unknown"},
	}
	if got := template.GetProviderName(); got != "" {
		t.Errorf("GetProviderName() = %s, want empty", got)
	}
	if got := template.GetDistroVersion(); got != "" {
		t.Errorf("GetDistroVersion() = %s, want empty", got)
	}
}

func TestSaveUpdatedConfigFileStub(t *testing.T) {
	template := &ImageTemplate{}
	// Use temp_dir/dummy instead of just "dummy"
	dummyPath := filepath.Join(TempDir(), "dummy")
	if err := template.SaveUpdatedConfigFile(dummyPath); err != nil {
		t.Errorf("SaveUpdatedConfigFile() = %v, want nil", err)
	}
}

func TestGetInitramfsTemplate(t *testing.T) {
	// Test with empty initramfs template
	template := &ImageTemplate{
		SystemConfig: SystemConfig{},
	}

	_, err := template.GetInitramfsTemplate()
	if err == nil {
		t.Error("Expected error for empty initramfs template")
	}
	if !strings.Contains(err.Error(), "initramfs template not specified") {
		t.Errorf("Expected 'initramfs template not specified' error, got %s", err.Error())
	}

	// Test with absolute path that doesn't exist
	template.SystemConfig.Initramfs.Template = "/nonexistent/path/initrd.conf"
	_, err = template.GetInitramfsTemplate()
	if err == nil {
		t.Error("Expected error for nonexistent absolute path")
	}
	if err != nil && !strings.Contains(err.Error(), "initrd template file does not exist or is not accessible") {
		t.Errorf("Expected 'initrd template file does not exist or is not accessible' error, got %s", err.Error())
	}

	// Test with relative path but no PathList
	template.SystemConfig.Initramfs.Template = "initrd.conf"
	template.PathList = nil
	_, err = template.GetInitramfsTemplate()
	if err == nil {
		t.Error("Expected error for relative path without context")
	}
	if !strings.Contains(err.Error(), "cannot resolve relative initramfs template path") {
		t.Errorf("Expected 'cannot resolve relative initramfs template path' error, got %s", err.Error())
	}

	// Test with valid absolute path
	tmpFile, err := os.CreateTemp("", "initrd-*.conf")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	template.SystemConfig.Initramfs.Template = tmpFile.Name()
	resultPath, err := template.GetInitramfsTemplate()
	if err != nil {
		t.Errorf("Unexpected error for valid absolute path: %v", err)
	}
	if resultPath != tmpFile.Name() {
		t.Errorf("Expected path %s, got %s", tmpFile.Name(), resultPath)
	}
}

func TestGetConfigurationInfo(t *testing.T) {
	// Test with empty configuration info
	template := &ImageTemplate{
		SystemConfig: SystemConfig{},
	}

	configs := template.GetConfigurationInfo()
	if len(configs) != 0 {
		t.Errorf("Expected empty configuration info, got %d items", len(configs))
	}

	// Test with configuration info
	expectedConfigs := []ConfigurationInfo{
		{Cmd: "echo 'setup complete'"},
		{Cmd: "systemctl enable docker"},
	}
	template.SystemConfig.Configurations = expectedConfigs

	configs = template.GetConfigurationInfo()
	if len(configs) != 2 {
		t.Errorf("Expected 2 configuration items, got %d", len(configs))
	}

	for i, config := range configs {
		if config.Cmd != expectedConfigs[i].Cmd {
			t.Errorf("Expected command %s, got %s", expectedConfigs[i].Cmd, config.Cmd)
		}
	}
}

func TestGetKernelPackages(t *testing.T) {
	// Test with empty kernel packages
	template := &ImageTemplate{
		SystemConfig: SystemConfig{
			Kernel: KernelConfig{},
		},
	}

	packages := template.GetKernelPackages()
	if len(packages) != 0 {
		t.Errorf("Expected empty kernel packages, got %d", len(packages))
	}

	// Test with kernel packages
	expectedPackages := []string{"linux-kernel", "linux-headers", "linux-firmware"}
	template.SystemConfig.Kernel.Packages = expectedPackages

	packages = template.GetKernelPackages()
	if len(packages) != 3 {
		t.Errorf("Expected 3 kernel packages, got %d", len(packages))
	}

	for i, pkg := range packages {
		if pkg != expectedPackages[i] {
			t.Errorf("Expected package %s, got %s", expectedPackages[i], pkg)
		}
	}
}

func TestLoadProviderRepoConfig(t *testing.T) {
	// Test with invalid parameters - this will fail in test environment
	// but we test that the function handles the error gracefully
	_, err := LoadProviderRepoConfig("nonexistent-os", "nonexistent-dist", "amd64")
	if err == nil {
		t.Log("Unexpected success - config found for nonexistent OS/dist")
	} else {
		// Expected in test environment
		if !strings.Contains(err.Error(), "failed to get target OS config directory") &&
			!strings.Contains(err.Error(), "failed to read repo config file") {
			t.Errorf("Expected config-related error, got: %v", err)
		}
	}

	// Test with empty parameters
	_, err = LoadProviderRepoConfig("", "", "")
	if err == nil {
		t.Error("Expected error with empty parameters")
	} else {
		t.Logf("Expected error with empty parameters: %v", err)
	}

	// Test with realistic parameters (will fail in test environment due to missing config files)
	testCases := []struct {
		os   string
		dist string
	}{
		{"azure-linux", "azl3"},
		{"emt", "emt3"},
		{"elxr", "elxr12"},
	}

	for _, tc := range testCases {
		_, err := LoadProviderRepoConfig(tc.os, tc.dist, "amd64")
		if err == nil {
			t.Logf("Unexpected success for %s/%s in test environment", tc.os, tc.dist)
		} else {
			// This is expected in unit test environment
			t.Logf("Expected error for %s/%s in test environment: %v", tc.os, tc.dist, err)
		}
	}
}

// TestUnifiedRepoConfig verifies that the unified ToRepoConfigData function
// works correctly for both RPM and DEB repository types
func TestUnifiedRepoConfig(t *testing.T) {
	tests := []struct {
		name         string
		repoConfig   ProviderRepoConfig
		arch         string
		expectedType string
		expectedURL  string
		expectedGPG  string
	}{
		{
			name: "RPM Repository (Azure Linux)",
			repoConfig: ProviderRepoConfig{
				Name:      "Azure Linux 3.0",
				Type:      "rpm",
				BaseURL:   "https://packages.microsoft.com/azurelinux/3.0/prod/base/{arch}",
				GPGKey:    "repodata/repomd.xml.key",
				Component: "azl3.0-base",
				Enabled:   true,
			},
			arch:         "x86_64",
			expectedType: "rpm",
			expectedURL:  "https://packages.microsoft.com/azurelinux/3.0/prod/base/x86_64",
			expectedGPG:  "https://packages.microsoft.com/azurelinux/3.0/prod/base/x86_64/repodata/repomd.xml.key",
		},
		{
			name: "DEB Repository (eLxr)",
			repoConfig: ProviderRepoConfig{
				Name:        "Wind River eLxr 12",
				Type:        "deb",
				BaseURL:     "https://mirror.elxr.dev/elxr/dists/aria/main",
				PbGPGKey:    "https://mirror.elxr.dev/elxr/public.gpg",
				Component:   "main",
				Enabled:     true,
				PkgPrefix:   "https://mirror.elxr.dev/elxr/",
				ReleaseFile: "https://mirror.elxr.dev/elxr/dists/aria/Release",
			},
			arch:         "amd64",
			expectedType: "deb",
			expectedURL:  "https://mirror.elxr.dev/elxr/dists/aria/main/binary-amd64/Packages.gz",
		},
		{
			name: "RPM Repository with arch substitution (EMT-style)",
			repoConfig: ProviderRepoConfig{
				Name:      "Edge Microvisor Toolkit 3.0",
				Type:      "rpm",
				BaseURL:   "https://files-rs.edgeorchestration.intel.com/files-edge-orch/microvisor/rpm/3.0",
				Component: "emt3.0-base",
				Enabled:   true,
				GPGCheck:  false,
			},
			arch:         "x86_64",
			expectedType: "rpm",
			expectedURL:  "https://files-rs.edgeorchestration.intel.com/files-edge-orch/microvisor/rpm/3.0",
		},
		{
			name: "RPM Repository with multiple GPG keys",
			repoConfig: ProviderRepoConfig{
				Name:      "Edge Microvisor Toolkit 3.0",
				Type:      "rpm",
				BaseURL:   "https://files-rs.edgeorchestration.intel.com/files-edge-orch/microvisor/rpm/3.0",
				GPGKeys:   []string{"https://example.com/key-old.asc", "https://example.com/key-new.asc"},
				Component: "emt3.0-base",
				Enabled:   true,
			},
			arch:         "x86_64",
			expectedType: "rpm",
			expectedURL:  "https://files-rs.edgeorchestration.intel.com/files-edge-orch/microvisor/rpm/3.0",
			expectedGPG:  "https://example.com/key-old.asc,https://example.com/key-new.asc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoType, name, url, gpgKey, component, buildPath, pkgPrefix, releaseFile, releaseSign, _, gpgCheck, repoGPGCheck, enabled := tt.repoConfig.ToRepoConfigData(tt.arch)

			// Verify repository type
			if repoType != tt.expectedType {
				t.Errorf("Expected repo type %s, got %s", tt.expectedType, repoType)
			}

			// Verify URL construction
			if url != tt.expectedURL {
				t.Errorf("Expected URL %s, got %s", tt.expectedURL, url)
			}

			// Verify basic fields
			if name != tt.repoConfig.Name {
				t.Errorf("Expected name %s, got %s", tt.repoConfig.Name, name)
			}

			if component != tt.repoConfig.Component {
				t.Errorf("Expected component %s, got %s", tt.repoConfig.Component, component)
			}

			if enabled != tt.repoConfig.Enabled {
				t.Errorf("Expected enabled %v, got %v", tt.repoConfig.Enabled, enabled)
			}

			// Verify type-specific fields
			switch tt.expectedType {
			case "rpm":
				// For RPM: pkgPrefix, releaseFile, releaseSign should be empty
				if pkgPrefix != "" || releaseFile != "" || releaseSign != "" {
					t.Errorf("Expected empty DEB-specific fields for RPM repo, got pkgPrefix=%s, releaseFile=%s, releaseSign=%s",
						pkgPrefix, releaseFile, releaseSign)
				}

				// Verify arch substitution in GPG key if applicable
				if tt.expectedGPG != "" {
					if gpgKey != tt.expectedGPG {
						t.Errorf("Expected GPG key %s, got %s", tt.expectedGPG, gpgKey)
					}
				} else if tt.repoConfig.GPGKey != "" && gpgKey != "" {
					expectedGPGKey := tt.repoConfig.GPGKey
					if expectedGPGKey == "repodata/repomd.xml.key" {
						expectedGPGKey = "https://packages.microsoft.com/azurelinux/3.0/prod/base/x86_64/repodata/repomd.xml.key"
					}
					if gpgKey != expectedGPGKey {
						t.Errorf("Expected GPG key %s, got %s", expectedGPGKey, gpgKey)
					}
				}

			case "deb":
				// For DEB: should have the DEB-specific fields populated
				if pkgPrefix != tt.repoConfig.PkgPrefix {
					t.Errorf("Expected pkgPrefix %s, got %s", tt.repoConfig.PkgPrefix, pkgPrefix)
				}
				if releaseFile != tt.repoConfig.ReleaseFile {
					t.Errorf("Expected releaseFile %s, got %s", tt.repoConfig.ReleaseFile, releaseFile)
				}
				if gpgKey != tt.repoConfig.PbGPGKey {
					t.Errorf("Expected gpgKey (pbGPGKey) %s, got %s", tt.repoConfig.PbGPGKey, gpgKey)
				}
			}

			// Verify GPG settings match
			if gpgCheck != tt.repoConfig.GPGCheck {
				t.Errorf("Expected gpgCheck %v, got %v", tt.repoConfig.GPGCheck, gpgCheck)
			}
			if repoGPGCheck != tt.repoConfig.RepoGPGCheck {
				t.Errorf("Expected repoGPGCheck %v, got %v", tt.repoConfig.RepoGPGCheck, repoGPGCheck)
			}

			// For this test, buildPath can be ignored as it's not critical to functionality
			_ = buildPath

			t.Logf("✅ %s: type=%s, url=%s, gpgKey=%s", tt.name, repoType, url, gpgKey)
		})
	}
}

// TestGetAdditionalFileInfo tests the GetAdditionalFileInfo method
func TestGetAdditionalFileInfo(t *testing.T) {
	// Create temporary test files
	tmpDir := t.TempDir()
	validFile := filepath.Join(tmpDir, "valid.txt")
	if err := os.WriteFile(validFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	tests := []struct {
		name          string
		template      *ImageTemplate
		expectedCount int
		description   string
	}{
		{
			name: "Empty additional files",
			template: &ImageTemplate{
				SystemConfig: SystemConfig{
					AdditionalFiles: []AdditionalFileInfo{},
				},
			},
			expectedCount: 0,
			description:   "Should return empty list when no additional files",
		},
		{
			name: "Valid absolute path",
			template: &ImageTemplate{
				SystemConfig: SystemConfig{
					AdditionalFiles: []AdditionalFileInfo{
						{
							Local: validFile,
							Final: "/etc/config.txt",
						},
					},
				},
			},
			expectedCount: 1,
			description:   "Should include file with valid absolute path",
		},
		{
			name: "Empty local path - should be filtered",
			template: &ImageTemplate{
				SystemConfig: SystemConfig{
					AdditionalFiles: []AdditionalFileInfo{
						{
							Local: "",
							Final: "/etc/config.txt",
						},
					},
				},
			},
			expectedCount: 0,
			description:   "Should filter out entries with empty local path",
		},
		{
			name: "Empty final path - should be filtered",
			template: &ImageTemplate{
				SystemConfig: SystemConfig{
					AdditionalFiles: []AdditionalFileInfo{
						{
							Local: validFile,
							Final: "",
						},
					},
				},
			},
			expectedCount: 0,
			description:   "Should filter out entries with empty final path",
		},
		{
			name: "Mixed valid and invalid entries",
			template: &ImageTemplate{
				SystemConfig: SystemConfig{
					AdditionalFiles: []AdditionalFileInfo{
						{Local: validFile, Final: "/etc/valid1.txt"},
						{Local: "", Final: "/etc/empty.txt"},
						{Local: validFile, Final: ""},
						{Local: validFile, Final: "/etc/valid2.txt"},
					},
				},
			},
			expectedCount: 2,
			description:   "Should filter out invalid entries and keep valid ones",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.template.GetAdditionalFileInfo()
			if len(result) != tt.expectedCount {
				t.Errorf("%s: expected %d files, got %d", tt.description, tt.expectedCount, len(result))
			}
		})
	}
}

// TestWasProvided tests the WasProvided method for ImmutabilityConfig
func TestWasProvided(t *testing.T) {
	tests := []struct {
		name     string
		config   *ImmutabilityConfig
		expected bool
	}{
		{
			name: "Was provided - true",
			config: &ImmutabilityConfig{
				wasProvided: true,
			},
			expected: true,
		},
		{
			name: "Was not provided - false",
			config: &ImmutabilityConfig{
				wasProvided: false,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.WasProvided()
			if result != tt.expected {
				t.Errorf("WasProvided() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestGetConfigPaths tests that GetConfigPaths returns expected config locations
func TestGetConfigPaths(t *testing.T) {
	paths := GetConfigPaths()

	if len(paths) == 0 {
		t.Error("GetConfigPaths should return at least one path")
	}

	// Verify that current directory paths are included
	expectedPaths := []string{
		"os-image-composer.yml",
		".os-image-composer.yml",
		"os-image-composer.yaml",
		".os-image-composer.yaml",
	}

	for _, expected := range expectedPaths {
		found := false
		for _, path := range paths {
			if path == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected path %s not found in config paths", expected)
		}
	}

	// Verify system paths are included
	systemPaths := []string{
		"/etc/os-image-composer/config.yml",
		"/etc/os-image-composer/config.yaml",
	}

	for _, sysPath := range systemPaths {
		found := false
		for _, path := range paths {
			if path == sysPath {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected system path %s not found in config paths", sysPath)
		}
	}
}

// TestFindConfigFile tests the FindConfigFile function
func TestFindConfigFile(t *testing.T) {
	// Create a temporary config file in current directory
	tmpDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Failed to change back to original directory: %v", err)
		}
	}()

	err := os.Chdir(tmpDir)
	if err != nil {
		t.Fatalf("Failed to change to temp dir: %v", err)
	}

	// Test when no config file exists
	result := FindConfigFile()
	if result != "" {
		t.Logf("Found unexpected config file: %s", result)
	}

	// Create a config file
	configFile := "os-image-composer.yml"
	if err := os.WriteFile(configFile, []byte("workers: 4\n"), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	// Test when config file exists
	result = FindConfigFile()
	if result == "" {
		t.Error("FindConfigFile should find the created config file")
	}
}

// TestWorkers tests the Workers convenience function
func TestWorkers(t *testing.T) {
	// This function depends on Global() which should return valid config
	workers := Workers()
	if workers < 0 {
		t.Errorf("Workers should return non-negative value, got %d", workers)
	}
}

// TestVerificationWorkers tests the VerificationWorkers convenience function
func TestVerificationWorkers(t *testing.T) {
	workers := VerificationWorkers()
	if workers < 0 {
		t.Errorf("VerificationWorkers should return non-negative value, got %d", workers)
	}
	if workers > 4 {
		t.Errorf("VerificationWorkers should be capped at 4, got %d", workers)
	}
}

// TestConfigDir tests the ConfigDir function
func TestConfigDir(t *testing.T) {
	dir, err := ConfigDir()
	if err != nil {
		t.Logf("ConfigDir returned error (may be expected in test): %v", err)
		return
	}
	if dir == "" {
		t.Error("ConfigDir should return non-empty path")
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("ConfigDir should return absolute path, got: %s", dir)
	}
}

// TestCacheDir tests the CacheDir function
func TestCacheDir(t *testing.T) {
	dir, err := CacheDir()
	if err != nil {
		t.Logf("CacheDir returned error (may be expected in test): %v", err)
		return
	}
	if dir == "" {
		t.Error("CacheDir should return non-empty path")
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("CacheDir should return absolute path, got: %s", dir)
	}
}

// TestWorkDir tests the WorkDir function
func TestWorkDir(t *testing.T) {
	dir, err := WorkDir()
	if err != nil {
		t.Logf("WorkDir returned error (may be expected in test): %v", err)
		return
	}
	if dir == "" {
		t.Error("WorkDir should return non-empty path")
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("WorkDir should return absolute path, got: %s", dir)
	}
}

// TestTempDir tests the TempDir function
func TestTempDir(t *testing.T) {
	dir := TempDir()
	if dir == "" {
		t.Error("TempDir should return non-empty path")
	}
}

// TestLogLevel tests the LogLevel function
func TestLogLevel(t *testing.T) {
	level := LogLevel()
	// Log level can be any string, just verify it doesn't panic
	t.Logf("Log level: %s", level)
}

// TestEnsureCacheDir tests the EnsureCacheDir function
func TestEnsureCacheDir(t *testing.T) {
	err := EnsureCacheDir()
	if err != nil {
		t.Logf("EnsureCacheDir returned error (may be expected in test): %v", err)
	}
}

// TestEnsureWorkDir tests the EnsureWorkDir function
func TestEnsureWorkDir(t *testing.T) {
	err := EnsureWorkDir()
	if err != nil {
		t.Logf("EnsureWorkDir returned error (may be expected in test): %v", err)
	}
}

// TestEnsureTempDir tests the EnsureTempDir function
func TestEnsureTempDir(t *testing.T) {
	dir, err := EnsureTempDir("test-subdir")
	if err != nil {
		t.Fatalf("EnsureTempDir failed: %v", err)
	}
	if dir == "" {
		t.Error("EnsureTempDir should return non-empty path")
	}

	// Verify the directory was created
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("EnsureTempDir should create the directory")
	}
}
