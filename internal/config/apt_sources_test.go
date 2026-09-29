package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Test helper to resolve relative paths
func resolveTestPath(relativePath string) (string, error) {
	if filepath.IsAbs(relativePath) {
		return relativePath, nil
	}

	// If the path starts with ../../../../../../tmp/, extract filename and look in ./tmp
	if strings.HasPrefix(relativePath, filepath.Join("..", "..", "..", "..", "..", "..", "tmp")) {
		// Get current working directory
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}

		// Extract just the filename from the relative path
		filename := filepath.Base(relativePath)
		// Return path in local ./tmp relative to cwd
		return filepath.Join(wd, "tmp", filename), nil
	}

	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Clean(filepath.Join(wd, relativePath)), nil
}

func TestGenerateAptSourcesContent(t *testing.T) {
	tests := []struct {
		name     string
		repos    []PackageRepository
		expected []string // Expected to contain these lines
	}{
		{
			name:     "empty repositories",
			repos:    []PackageRepository{},
			expected: []string{},
		},
		{
			name: "single repository with basic config",
			repos: []PackageRepository{
				{
					ID:        "intel-repo",
					Codename:  "noble",
					URL:       "https://apt.repos.intel.com/openvino/2025",
					Component: "main",
				},
			},
			expected: []string{
				"deb https://apt.repos.intel.com/openvino/2025 noble main",
			},
		},
		{
			name: "repository with GPG key",
			repos: []PackageRepository{
				{
					ID:        "sed-repo",
					Codename:  "noble",
					URL:       "https://eci.intel.com/sed-repos/noble",
					PKey:      "https://eci.intel.com/sed-repos/gpg-keys/GPG-PUB-KEY-INTEL-SED.gpg",
					Component: "main",
					Priority:  1000,
				},
			},
			expected: []string{
				"deb https://eci.intel.com/sed-repos/noble noble main",
			},
		},
		{
			name: "multiple repositories",
			repos: []PackageRepository{
				{
					Codename:  "stable",
					URL:       "https://repo1.example.com",
					Component: "main contrib",
				},
				{
					Codename: "testing",
					URL:      "https://repo2.example.com",
					// Component defaults to "main"
				},
			},
			expected: []string{
				"deb https://repo1.example.com stable main contrib",
				"deb https://repo2.example.com testing main",
			},
		},
		{
			name: "repository missing essential fields",
			repos: []PackageRepository{
				{
					ID:  "incomplete",
					URL: "", // Missing URL
				},
				{
					ID:       "valid",
					Codename: "stable",
					URL:      "https://valid.example.com",
				},
			},
			expected: []string{
				"deb https://valid.example.com stable main",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := generateAptSourcesContent(tt.repos)

			for _, expectedLine := range tt.expected {
				if !strings.Contains(content, expectedLine) {
					t.Errorf("Expected content to contain: %q\nActual content:\n%s", expectedLine, content)
				}
			}
		})
	}
}

func TestExtractGPGKeyFilename(t *testing.T) {
	tests := []struct {
		name     string
		keyURL   string
		expected string
	}{
		{
			name:     "Intel GPG key - extract filename first",
			keyURL:   "https://eci.intel.com/sed-repos/gpg-keys/GPG-PUB-KEY-INTEL-SED.gpg",
			expected: "/etc/apt/trusted.gpg.d/GPG-PUB-KEY-INTEL-SED.gpg",
		},
		{
			name:     "generic GPG key",
			keyURL:   "https://example.com/keys/repo-key.gpg",
			expected: "/etc/apt/trusted.gpg.d/repo-key.gpg",
		},
		{
			name:     "ASC key converted to GPG",
			keyURL:   "https://example.com/keys/repo-key.asc",
			expected: "/etc/apt/trusted.gpg.d/repo-key.gpg",
		},
		{
			name:     "no extension",
			keyURL:   "https://example.com/keys/mykey",
			expected: "/etc/apt/trusted.gpg.d/mykey.gpg",
		},
		{
			name:     "another intel key",
			keyURL:   "https://apt.repos.intel.com/intel-gpg-keys/GPG-PUB-KEY-INTEL-SW-PRODUCTS.PUB",
			expected: "/etc/apt/trusted.gpg.d/GPG-PUB-KEY-INTEL-SW-PRODUCTS.PUB.gpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractGPGKeyFilename(tt.keyURL)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestIsDEBBasedTarget(t *testing.T) {
	tests := []struct {
		targetOS string
		expected bool
	}{
		{"ubuntu", true},
		{"debian", true},
		{"elxr", true},
		{"azl", false},
		{"emt", false},
		{"centos", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.targetOS, func(t *testing.T) {
			result := isDEBBasedTarget(tt.targetOS)
			if result != tt.expected {
				t.Errorf("isDEBBasedTarget(%q) = %v, expected %v", tt.targetOS, result, tt.expected)
			}
		})
	}
}

func TestCreateTempAptSourcesFile(t *testing.T) {
	// Ensure temp directory exists for test
	tempDir := "./tmp"
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}

	content := `# Test content
deb https://example.com stable main
`

	tempFile, err := createTempAptSourcesFile(content)
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	// Resolve the relative path
	tempFileAbs, err := resolveTestPath(tempFile)
	if err != nil {
		t.Fatalf("Failed to resolve temp file path: %v", err)
	}

	// Clean up
	defer os.Remove(tempFileAbs)

	// Verify file exists and has correct content
	if _, err := os.Stat(tempFileAbs); os.IsNotExist(err) {
		t.Errorf("Temp file was not created: %s (resolved to %s)", tempFile, tempFileAbs)
	}

	fileContent, err := os.ReadFile(tempFileAbs)
	if err != nil {
		t.Fatalf("Failed to read temp file: %v", err)
	}

	if string(fileContent) != content {
		t.Errorf("File content mismatch. Expected:\n%s\nGot:\n%s", content, string(fileContent))
	}
}

// TestDownloadAndAddGPGKeys_TrustedYes verifies that [trusted=yes] is properly skipped
func TestDownloadAndAddGPGKeys_TrustedYes(t *testing.T) {
	template := &ImageTemplate{
		Target: TargetInfo{
			OS: "ubuntu",
		},
		PackageRepositories: []PackageRepository{
			{
				ID:       "trusted-repo",
				Codename: "noble",
				URL:      "https://example.com/repo",
				PKey:     "[trusted=yes]",
			},
		},
		SystemConfig: SystemConfig{
			AdditionalFiles: []AdditionalFileInfo{},
		},
	}

	// Should not attempt to download or add any GPG keys
	err := template.downloadAndAddGPGKeys(template.PackageRepositories)
	if err != nil {
		t.Errorf("downloadAndAddGPGKeys should succeed with [trusted=yes], got error: %v", err)
	}

	// Verify no additional files were added (no GPG key downloaded)
	if len(template.SystemConfig.AdditionalFiles) != 0 {
		t.Errorf("Expected no additional files for [trusted=yes], got %d", len(template.SystemConfig.AdditionalFiles))
	}
}

// TestDownloadAndAddGPGKeys_PlaceholderURL verifies that placeholder URLs are properly skipped
func TestDownloadAndAddGPGKeys_PlaceholderURL(t *testing.T) {
	template := &ImageTemplate{
		Target: TargetInfo{
			OS: "ubuntu",
		},
		PackageRepositories: []PackageRepository{
			{
				ID:       "placeholder-repo",
				Codename: "noble",
				URL:      "https://example.com/repo",
				PKey:     "<PUBLIC_KEY_URL>",
			},
		},
		SystemConfig: SystemConfig{
			AdditionalFiles: []AdditionalFileInfo{},
		},
	}

	// Should not attempt to download or add any GPG keys
	err := template.downloadAndAddGPGKeys(template.PackageRepositories)
	if err != nil {
		t.Errorf("downloadAndAddGPGKeys should succeed with placeholder URL, got error: %v", err)
	}

	// Verify no additional files were added (no GPG key downloaded)
	if len(template.SystemConfig.AdditionalFiles) != 0 {
		t.Errorf("Expected no additional files for placeholder URL, got %d", len(template.SystemConfig.AdditionalFiles))
	}
}

func TestGenerateAptSourcesFromRepositories(t *testing.T) {
	// Create test template
	template := &ImageTemplate{
		Target: TargetInfo{
			OS: "ubuntu",
		},
		PackageRepositories: []PackageRepository{
			{
				ID:        "test-repo",
				Codename:  "noble",
				URL:       "https://example.com/repo",
				Component: "main",
			},
		},
		SystemConfig: SystemConfig{
			AdditionalFiles: []AdditionalFileInfo{},
		},
	}

	// Test the function
	err := template.GenerateAptSourcesFromRepositories()
	if err != nil {
		t.Fatalf("Failed to generate apt sources: %v", err)
	}

	// Check that additional files were added (sources + preferences)
	if len(template.SystemConfig.AdditionalFiles) != 2 {
		t.Errorf("Expected 2 additional files (sources + preferences), got %d", len(template.SystemConfig.AdditionalFiles))
	}

	// Clean up temp files
	defer func() {
		for _, file := range template.SystemConfig.AdditionalFiles {
			os.Remove(file.Local)
		}
	}()

	if len(template.SystemConfig.AdditionalFiles) >= 2 {
		// Find sources and preferences files
		var sourcesFile, preferencesFile *AdditionalFileInfo
		for i := range template.SystemConfig.AdditionalFiles {
			file := &template.SystemConfig.AdditionalFiles[i]
			if strings.HasPrefix(file.Final, "/etc/apt/sources.list.d/") {
				sourcesFile = file
			} else if strings.HasPrefix(file.Final, "/etc/apt/preferences.d/") {
				preferencesFile = file
			}
		}

		if sourcesFile == nil {
			t.Error("Sources file not found")
		} else if sourcesFile.Final != "/etc/apt/sources.list.d/package-repositories.list" {
			t.Errorf("Expected sources final path to be /etc/apt/sources.list.d/package-repositories.list, got %s", sourcesFile.Final)
		}

		if preferencesFile == nil {
			t.Error("Preferences file not found")
		} else if !strings.HasPrefix(preferencesFile.Final, "/etc/apt/preferences.d/") {
			t.Errorf("Expected preferences file in /etc/apt/preferences.d/, got %s", preferencesFile.Final)
		}
	}
}

func TestGenerateAptSourcesFromRepositories_NonDEBSystem(t *testing.T) {
	// Create test template for non-DEB system
	template := &ImageTemplate{
		Target: TargetInfo{
			OS: "azl", // RPM-based system
		},
		PackageRepositories: []PackageRepository{
			{
				ID:       "test-repo",
				Codename: "stable",
				URL:      "https://example.com/repo",
			},
		},
		SystemConfig: SystemConfig{
			AdditionalFiles: []AdditionalFileInfo{},
		},
	}

	// Test the function
	err := template.GenerateAptSourcesFromRepositories()
	if err != nil {
		t.Fatalf("Failed to generate apt sources: %v", err)
	}

	// Check that no additional file was added (since it's not a DEB system)
	if len(template.SystemConfig.AdditionalFiles) != 0 {
		t.Errorf("Expected 0 additional files for non-DEB system, got %d", len(template.SystemConfig.AdditionalFiles))
	}
}

func TestGenerateAptSourcesFromRepositories_PathOnlyRepo(t *testing.T) {
	template := &ImageTemplate{
		Target: TargetInfo{
			OS: "ubuntu",
		},
		PackageRepositories: []PackageRepository{
			{
				Codename:  "localdeb",
				Path:      "/data/image-composer-tool/localdeb",
				PKey:      "[trusted=yes]",
				Component: "main",
			},
		},
		SystemConfig: SystemConfig{
			AdditionalFiles: []AdditionalFileInfo{},
		},
	}

	err := template.GenerateAptSourcesFromRepositories()
	if err != nil {
		t.Fatalf("GenerateAptSourcesFromRepositories() failed for path-only repo: %v", err)
	}

	if len(template.SystemConfig.AdditionalFiles) != 0 {
		t.Errorf("expected no apt additional files for path-only repo, got %d", len(template.SystemConfig.AdditionalFiles))
	}
}

func TestGenerateAptSourcesRejectsScriptOwnedDestination(t *testing.T) {
	template := &ImageTemplate{
		Target: TargetInfo{OS: "ubuntu"},
		PackageRepositories: []PackageRepository{
			{ID: "test-repo", Codename: "noble", URL: "https://example.com/repo", Component: "main"},
		},
		SystemConfig: SystemConfig{
			Provisioning: ProvisioningConfig{Scripts: []ProvisioningScript{
				{Name: "s", Local: "/tmp/s.sh", Final: "/etc/apt/sources.list.d/package-repositories.list"},
			}},
		},
	}
	err := template.GenerateAptSourcesFromRepositories()
	if err == nil || !strings.Contains(err.Error(), "generates") {
		t.Errorf("err = %v, want script/generated file collision", err)
	}
}

func TestAddUniqueAdditionalFile(t *testing.T) {
	template := &ImageTemplate{
		SystemConfig: SystemConfig{
			AdditionalFiles: []AdditionalFileInfo{
				{Local: "/tmp/existing", Final: "/etc/existing"},
			},
		},
	}

	// Test adding new file
	newFile := AdditionalFileInfo{Local: "/tmp/new", Final: "/etc/new"}
	template.addUniqueAdditionalFile(newFile)

	if len(template.SystemConfig.AdditionalFiles) != 2 {
		t.Errorf("Expected 2 files after adding new, got %d", len(template.SystemConfig.AdditionalFiles))
	}

	// Test replacing existing file
	replacementFile := AdditionalFileInfo{Local: "/tmp/replacement", Final: "/etc/existing"}
	template.addUniqueAdditionalFile(replacementFile)

	if len(template.SystemConfig.AdditionalFiles) != 2 {
		t.Errorf("Expected 2 files after replacement, got %d", len(template.SystemConfig.AdditionalFiles))
	}

	// Verify replacement happened
	found := false
	for _, file := range template.SystemConfig.AdditionalFiles {
		if file.Final == "/etc/existing" && file.Local == "/tmp/replacement" {
			found = true
			break
		}
	}
	if !found {
		t.Error("File replacement did not work correctly")
	}
}

func TestExtractOriginFromURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "HTTPS URL",
			url:      "https://eci.intel.com/sed-repos/noble",
			expected: "eci.intel.com",
		},
		{
			name:     "HTTP URL",
			url:      "http://apt.repos.intel.com/openvino/2025",
			expected: "apt.repos.intel.com",
		},
		{
			name:     "uppercase scheme",
			url:      "HTTPS://Repo.Example.com/apt",
			expected: "repo.example.com",
		},
		{
			name:     "scheme-looking text after the authority is not a scheme",
			url:      "example.com/redirect?u=http://other.example",
			expected: "example.com",
		},
		{
			name:     "URL without protocol",
			url:      "example.com/repo/path",
			expected: "example.com",
		},
		{
			name:     "URL with port",
			url:      "https://repo.example.com:8080/path",
			expected: "repo.example.com:8080",
		},
		{
			name:     "URL with query and no path",
			url:      "https://repo.example.com?channel=stable",
			expected: "repo.example.com",
		},
		{
			name:     "URL with fragment after port",
			url:      "https://repo.example.com:8080#frag",
			expected: "repo.example.com:8080",
		},
		{
			name:     "URL with credentials",
			url:      "https://user:token@repo.example.com:8080/path",
			expected: "repo.example.com:8080",
		},
		{
			name:     "Invalid URL",
			url:      "///invalid",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractOriginFromURL(tt.url)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestGenerateAptPreferencesContent(t *testing.T) {
	tests := []struct {
		name     string
		origin   string
		priority int
		expected []string // Expected to contain these lines
	}{
		{
			name:     "Force install priority >1000",
			origin:   "eci.intel.com",
			priority: 1100,
			expected: []string{
				"# Priority >1000: Force install even downgrade",
				"Package: *",
				"Pin: origin eci.intel.com",
				"Pin-Priority: 1100",
			},
		},
		{
			name:     "Install even if lower version priority 1000",
			origin:   "eci.intel.com",
			priority: 1000,
			expected: []string{
				"# Priority 1000: Install even if version is lower than installed",
				"Package: *",
				"Pin: origin eci.intel.com",
				"Pin-Priority: 1000",
			},
		},
		{
			name:     "Preferred priority 990",
			origin:   "apt.repos.intel.com",
			priority: 990,
			expected: []string{
				"# Priority 990: Preferred",
				"Package: *",
				"Pin: origin apt.repos.intel.com",
				"Pin-Priority: 990",
			},
		},
		{
			name:     "Default priority 500",
			origin:   "example.com",
			priority: 500,
			expected: []string{
				"# Priority 500: Default",
				"Package: *",
				"Pin: origin example.com",
				"Pin-Priority: 500",
			},
		},
		{
			name:     "Never install priority <0",
			origin:   "blocked.com",
			priority: -1,
			expected: []string{
				"# Priority <0: Never install",
				"Package: *",
				"Pin: origin blocked.com",
				"Pin-Priority: -1",
			},
		},
		{
			name:     "Custom priority",
			origin:   "custom.com",
			priority: 750,
			expected: []string{
				"# Priority 750: Custom priority",
				"Package: *",
				"Pin: origin custom.com",
				"Pin-Priority: 750",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateAptPreferencesContent(tt.origin, tt.priority)

			for _, expectedLine := range tt.expected {
				if !strings.Contains(result, expectedLine) {
					t.Errorf("Expected content to contain: %q\nActual content:\n%s", expectedLine, result)
				}
			}
		})
	}
}

func TestNormalizeRepositoryPriorities(t *testing.T) {
	tests := []struct {
		name     string
		repos    []PackageRepository
		expected []PackageRepository
	}{
		{
			name: "repositories without priority get default 500",
			repos: []PackageRepository{
				{ID: "repo1", Codename: "stable", URL: "https://example.com"},
				{ID: "repo2", Codename: "testing", URL: "https://test.com"},
			},
			expected: []PackageRepository{
				{ID: "repo1", Codename: "stable", URL: "https://example.com", Priority: 500},
				{ID: "repo2", Codename: "testing", URL: "https://test.com", Priority: 500},
			},
		},
		{
			name: "repositories with explicit priority unchanged",
			repos: []PackageRepository{
				{ID: "high-priority", Codename: "stable", URL: "https://example.com", Priority: 1000},
				{ID: "low-priority", Codename: "testing", URL: "https://test.com", Priority: 100},
			},
			expected: []PackageRepository{
				{ID: "high-priority", Codename: "stable", URL: "https://example.com", Priority: 1000},
				{ID: "low-priority", Codename: "testing", URL: "https://test.com", Priority: 100},
			},
		},
		{
			name: "mixed priorities - some explicit, some default",
			repos: []PackageRepository{
				{ID: "explicit", Codename: "stable", URL: "https://example.com", Priority: 990},
				{ID: "default", Codename: "testing", URL: "https://test.com", Priority: 0},
				{ID: "never", Codename: "blocked", URL: "https://blocked.com", Priority: -1},
			},
			expected: []PackageRepository{
				{ID: "explicit", Codename: "stable", URL: "https://example.com", Priority: 990},
				{ID: "default", Codename: "testing", URL: "https://test.com", Priority: 500},
				{ID: "never", Codename: "blocked", URL: "https://blocked.com", Priority: -1},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeRepositoryPriorities(tt.repos)

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d repositories, got %d", len(tt.expected), len(result))
				return
			}

			for i, expected := range tt.expected {
				if result[i].Priority != expected.Priority {
					t.Errorf("Repository %s: expected priority %d, got %d",
						result[i].ID, expected.Priority, result[i].Priority)
				}
			}
		})
	}
}

func TestNormalizeRepositoryPriorities_NilInput(t *testing.T) {
	result := normalizeRepositoryPriorities(nil)
	if len(result) != 0 {
		t.Errorf("expected empty result for nil input, got len=%d", len(result))
	}
}

func TestGetRepositoryName(t *testing.T) {
	tests := []struct {
		name     string
		repo     PackageRepository
		expected string
	}{
		{
			name:     "repository with ID",
			repo:     PackageRepository{ID: "my-repo", Codename: "stable", URL: "https://example.com"},
			expected: "my-repo",
		},
		{
			name:     "repository without ID, use codename",
			repo:     PackageRepository{Codename: "testing", URL: "https://example.com"},
			expected: "testing",
		},
		{
			name:     "repository without ID and codename, use URL",
			repo:     PackageRepository{URL: "https://example.com/repo"},
			expected: "https://example.com/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getRepositoryName(tt.repo)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestGeneratePreferencesFilename(t *testing.T) {
	tests := []struct {
		name     string
		repo     PackageRepository
		expected string
	}{
		{
			name:     "Repository with ID",
			repo:     PackageRepository{ID: "sed-repo", Codename: "noble"},
			expected: "sed-repo",
		},
		{
			name:     "Repository without ID",
			repo:     PackageRepository{Codename: "ubuntu24"},
			expected: "ubuntu24",
		},
		{
			name:     "Repository with spaces in ID",
			repo:     PackageRepository{ID: "Intel SED Repo", Codename: "noble"},
			expected: "intel-sed-repo",
		},
		{
			name:     "Repository with invalid characters",
			repo:     PackageRepository{ID: "repo/with/slashes", Codename: "stable"},
			expected: "repo-with-slashes",
		},
		{
			name:     "Repository with empty ID and codename",
			repo:     PackageRepository{},
			expected: "repository",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generatePreferencesFilename(tt.repo)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestCreateTempAptPreferencesFile(t *testing.T) {
	repo := PackageRepository{
		ID:       "test-repo",
		Codename: "stable",
	}
	content := "Package: *\nPin: origin example.com\nPin-Priority: 1000\n"

	tempFile, err := createTempAptPreferencesFile(repo, content)
	if err != nil {
		t.Fatalf("Failed to create temp preferences file: %v", err)
	}

	// Resolve the relative path
	tempFileAbs, err := resolveTestPath(tempFile)
	if err != nil {
		t.Fatalf("Failed to resolve temp preferences file path: %v", err)
	}

	// Clean up
	defer os.Remove(tempFileAbs)

	// Verify file exists and has correct content
	if _, err := os.Stat(tempFileAbs); os.IsNotExist(err) {
		t.Errorf("Temp preferences file was not created: %s", tempFile)
	}

	fileContent, err := os.ReadFile(tempFileAbs)
	if err != nil {
		t.Fatalf("Failed to read temp preferences file: %v", err)
	}

	if string(fileContent) != content {
		t.Errorf("File content mismatch. Expected:\n%s\nGot:\n%s", content, string(fileContent))
	}
}

func TestGPGKeyCacheFilePath_StableForSameURL(t *testing.T) {
	oldCacheDir := Global().CacheDir
	Global().CacheDir = t.TempDir()
	t.Cleanup(func() {
		Global().CacheDir = oldCacheDir
	})

	keyURL := "https://example.com/keys/repo-key.gpg"

	path1, err := gpgKeyCacheFilePath(keyURL)
	if err != nil {
		t.Fatalf("gpgKeyCacheFilePath failed: %v", err)
	}

	path2, err := gpgKeyCacheFilePath(keyURL)
	if err != nil {
		t.Fatalf("gpgKeyCacheFilePath failed on second call: %v", err)
	}

	if path1 != path2 {
		t.Errorf("Expected stable cache path, got %q and %q", path1, path2)
	}

	if !strings.HasSuffix(path1, ".gpg") {
		t.Errorf("Expected cache path to end with .gpg, got %q", path1)
	}
}

func TestGetCachedOrDownloadGPGKey_UsesCacheOnHit(t *testing.T) {
	oldCacheDir := Global().CacheDir
	Global().CacheDir = t.TempDir()
	t.Cleanup(func() {
		Global().CacheDir = oldCacheDir
	})

	keyURL := "https://invalid.example.test/non-routable-key.gpg"
	want := []byte("cached-key-data")

	cachePath, err := gpgKeyCacheFilePath(keyURL)
	if err != nil {
		t.Fatalf("gpgKeyCacheFilePath failed: %v", err)
	}

	if err := os.WriteFile(cachePath, want, 0644); err != nil {
		t.Fatalf("failed to seed cached key: %v", err)
	}

	got, err := getCachedOrDownloadGPGKey(keyURL)
	if err != nil {
		t.Fatalf("getCachedOrDownloadGPGKey should use cache hit, got error: %v", err)
	}

	if string(got) != string(want) {
		t.Errorf("cache hit returned wrong key data: got %q want %q", string(got), string(want))
	}
}

func TestGenerateAptPreferencesAppliesAptPolicy(t *testing.T) {
	repos := []PackageRepository{
		{ID: "stack", Codename: "stack", URL: "https://ppa.example.com/stack", Priority: 990},
		{ID: "vendor", Codename: "vendor", URL: "https://vendor.example.com:8443/apt", Priority: 990},
		{ID: "blocked", Codename: "blocked", URL: "https://blocked.example.com/apt", Priority: -10},
		{ID: "unset", Codename: "unset", URL: "https://unset.example.com/apt"},
		{ID: "low", Codename: "low", URL: "https://low.example.com/apt", Priority: 10},
	}
	tests := []struct {
		name      string
		immutable bool
		allowed   []string
		want      map[string]int // origin -> Pin-Priority
	}{
		{"no policy keeps priorities", false, nil,
			map[string]int{"ppa.example.com": 990, "vendor.example.com:8443": 990}},
		{"unlisted repo cannot upgrade", false, []string{"stack"},
			map[string]int{"ppa.example.com": 990, "vendor.example.com:8443": NoUpgradePinPriority,
				"blocked.example.com": -10, "unset.example.com": NoUpgradePinPriority, "low.example.com": 10}},
		{"immutable pins every repo", true, []string{"stack"},
			map[string]int{"ppa.example.com": NoUpgradePinPriority, "vendor.example.com:8443": NoUpgradePinPriority,
				"blocked.example.com": -10}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpl := &ImageTemplate{
				PackageRepositories: repos,
				SystemConfig: SystemConfig{
					Immutability: ImmutabilityConfig{Enabled: tt.immutable},
					AptPolicy:    AptPolicy{UpgradeAllowedRepos: tt.allowed},
				},
			}
			if err := tmpl.generateAptPreferencesFromRepositories(); err != nil {
				t.Fatalf("generateAptPreferencesFromRepositories: %v", err)
			}
			got := map[string]int{}
			for _, f := range tmpl.SystemConfig.AdditionalFiles {
				data, err := os.ReadFile(filepath.Join(TempDir(), filepath.Base(f.Local)))
				if err != nil {
					t.Fatalf("reading %s: %v", f.Local, err)
				}
				var origin string
				var prio int
				for _, line := range strings.Split(string(data), "\n") {
					if v, ok := strings.CutPrefix(line, "Pin: origin "); ok {
						origin = v
					}
					if v, ok := strings.CutPrefix(line, "Pin-Priority: "); ok {
						n, err := strconv.Atoi(v)
						if err != nil {
							t.Fatalf("bad Pin-Priority %q: %v", v, err)
						}
						prio = n
					}
				}
				got[origin] = prio
			}
			for origin, want := range tt.want {
				if got[origin] != want {
					t.Errorf("origin %s priority = %d, want %d (all: %v)", origin, got[origin], want, got)
				}
			}
		})
	}
}

func TestGenerateAptSourcesAppliesAptPolicyForDebian(t *testing.T) {
	tmpl := &ImageTemplate{
		Target: TargetInfo{OS: "debian"},
		PackageRepositories: []PackageRepository{
			{ID: "stack", Codename: "stack", URL: "https://ppa.example.com/stack", Priority: 990},
			{ID: "vendor", Codename: "vendor", URL: "https://vendor.example.com/apt", Priority: 990},
		},
		SystemConfig: SystemConfig{AptPolicy: AptPolicy{UpgradeAllowedRepos: []string{"stack"}}},
	}
	if err := tmpl.GenerateAptSourcesFromRepositories(); err != nil {
		t.Fatalf("GenerateAptSourcesFromRepositories: %v", err)
	}
	var sources, vendorPin bool
	for _, f := range tmpl.SystemConfig.AdditionalFiles {
		if f.Final == "/etc/apt/sources.list.d/package-repositories.list" {
			sources = true
		}
		if !strings.HasPrefix(f.Final, "/etc/apt/preferences.d/") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(TempDir(), filepath.Base(f.Local)))
		if err != nil {
			t.Fatalf("reading %s: %v", f.Local, err)
		}
		if strings.Contains(string(data), "origin vendor.example.com") &&
			strings.Contains(string(data), "Pin-Priority: "+strconv.Itoa(NoUpgradePinPriority)) {
			vendorPin = true
		}
	}
	if !sources || !vendorPin {
		t.Errorf("Debian must get sources (%v) and a no-upgrade pin for the unlisted repository (%v)", sources, vendorPin)
	}
}
