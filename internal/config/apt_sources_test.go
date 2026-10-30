package config

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
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
		{
			// keyserver.ubuntu.com "pks/lookup" URLs all share the same last
			// path segment ("lookup"); the query string's search= value (the
			// requested key ID/fingerprint) is what actually identifies the
			// key and must end up in the filename instead, both so the name
			// is clean and so two different keys from this keyserver can't
			// collide on the same destination file.
			name:     "keyserver.ubuntu.com pks/lookup URL",
			keyURL:   "https://keyserver.ubuntu.com/pks/lookup?op=get&search=0x0c0e6af955ce463c03fc51574d098d70afbe5e1f",
			expected: "/etc/apt/trusted.gpg.d/0c0e6af955ce463c03fc51574d098d70afbe5e1f.gpg",
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

// TestExtractGPGKeyFilenameNoCollision guards the two-different-keyserver-URLs
// case that a plain query-string-stripping fix would still get wrong: two
// repositories using the same keyserver "pks/lookup" path but no (or an
// unrecognized) search= parameter must still resolve to different filenames,
// or the second one's key file would silently overwrite the first's.
func TestExtractGPGKeyFilenameNoCollision(t *testing.T) {
	a := extractGPGKeyFilename("https://keyserver.example.com/pks/lookup?op=get&options=mr&fingerprint=on")
	b := extractGPGKeyFilename("https://keyserver.example.com/pks/lookup?op=get&options=mr&fingerprint=off")
	if a == b {
		t.Fatalf("expected different filenames for different keyserver URLs without a search= key ID, got %q for both", a)
	}
}

// TestExtractGPGKeyFilenameEmptyCandidateNoCollision covers a bare query
// string on the root path (no path segment at all to derive a name from):
// extractGPGKeyFilename must still disambiguate these by URL, not just fall
// through to the same generic default filename for every one of them.
func TestExtractGPGKeyFilenameEmptyCandidateNoCollision(t *testing.T) {
	a := extractGPGKeyFilename("https://keys.example.com/?id=A")
	b := extractGPGKeyFilename("https://keys.example.com/?id=B")
	if a == b {
		t.Fatalf("expected different filenames for different empty-path-segment URLs, got %q for both", a)
	}
}

// TestExtractGPGKeyFilenameNonHexSearchNoCollision covers a keyserver
// "search=" value that is not a hex key ID/fingerprint (HKP also allows
// searching by name or email): two such URLs sanitizing to the same string
// must not collide, so only a recognized hex key ID/fingerprint skips the
// disambiguating hash.
func TestExtractGPGKeyFilenameNonHexSearchNoCollision(t *testing.T) {
	a := extractGPGKeyFilename("https://keyserver.example.com/pks/lookup?op=get&search=alice%2Bbuild%40example.com")
	b := extractGPGKeyFilename("https://keyserver.example.com/pks/lookup?op=get&search=alice-build%40example.com")
	if a == b {
		t.Fatalf("expected different filenames for different non-hex search values, got %q for both", a)
	}
}

// TestExtractGPGKeyFilenameSanitizationNoCollision covers two query-free URLs
// whose last path segments only differ in a character the filename
// sanitizer rewrites to "-" (e.g. "+"): without hashing, both would sanitize
// down to the identical "repo-signing.gpg" and the second key would silently
// overwrite the first's file.
func TestExtractGPGKeyFilenameSanitizationNoCollision(t *testing.T) {
	a := extractGPGKeyFilename("https://keys.example.com/repo+signing.gpg")
	b := extractGPGKeyFilename("https://keys.example.com/repo-signing.gpg")
	if a == b {
		t.Fatalf("expected different filenames for URLs that sanitize to the same base name, got %q for both", a)
	}
}

// TestExtractGPGKeyFilenameEncodedSlashNoCollision covers a path segment
// with a percent-encoded "/" ("%2F"): url.Parse decodes it in parsed.Path,
// so splitting on the decoded path would treat it as a literal separator and
// fold a distinct request path ("team%2Frelease.gpg") down to the same last
// segment ("release.gpg") as an unrelated URL, silently overwriting its key.
func TestExtractGPGKeyFilenameEncodedSlashNoCollision(t *testing.T) {
	a := extractGPGKeyFilename("https://keys.example.com/team%2Frelease.gpg")
	b := extractGPGKeyFilename("https://keys.example.com/release.gpg")
	if a == b {
		t.Fatalf("expected different filenames for an encoded-slash path and its decoded last segment, got %q for both", a)
	}
}

// TestExtractGPGKeyFilenameTrailingSlashNoCollision covers a path and its
// trailing-slash variant: both are distinct request paths that can serve
// different keys, but naive trimming would collapse ".../release" and
// ".../release/" onto the same last segment "release".
func TestExtractGPGKeyFilenameTrailingSlashNoCollision(t *testing.T) {
	a := extractGPGKeyFilename("https://keys.example.com/release")
	b := extractGPGKeyFilename("https://keys.example.com/release/")
	if a == b {
		t.Fatalf("expected different filenames for a path and its trailing-slash variant, got %q for both", a)
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

func TestGenerateAptSourcesRejectsScriptAtOverlappingPath(t *testing.T) {
	for _, final := range []string{
		"/etc/apt/sources.list.d/package-repositories.list/setup.sh",
		"/etc/apt/sources.list.d",
	} {
		template := &ImageTemplate{
			Target: TargetInfo{OS: "ubuntu"},
			PackageRepositories: []PackageRepository{
				{ID: "test-repo", Codename: "noble", URL: "https://example.com/repo", Component: "main"},
			},
			SystemConfig: SystemConfig{
				Provisioning: ProvisioningConfig{Scripts: []ProvisioningScript{
					{Name: "s", Local: "/tmp/s.sh", Final: final},
				}},
			},
		}
		err := template.GenerateAptSourcesFromRepositories()
		if err == nil || !strings.Contains(err.Error(), "generates") {
			t.Errorf("final %q: err = %v, want script/generated path collision", final, err)
		}
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
			expected: "repo.example.com",
		},
		{
			name:     "URL with query and no path",
			url:      "https://repo.example.com?channel=stable",
			expected: "repo.example.com",
		},
		{
			name:     "URL with fragment after port",
			url:      "https://repo.example.com:8080#frag",
			expected: "repo.example.com",
		},
		{
			name:     "URL with credentials",
			url:      "https://user:token@repo.example.com:8080/path",
			expected: "repo.example.com",
		},
		{
			name:     "IPv6 literal with port",
			url:      "http://[2001:db8::1]:8443/repo",
			expected: "[2001:db8::1]",
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

// testGPGPublicKey is a throwaway OpenPGP public key used only to exercise
// code paths that now validate GPG key data (getCachedOrDownloadGPGKey,
// downloadGPGKey): plain placeholder bytes no longer pass validation, so
// tests that need "some valid-looking key data" use this instead.
const testGPGPublicKey = `-----BEGIN PGP PUBLIC KEY BLOCK-----

mQENBGrR3dcBCADq0OeOQGYT7Q45yCeD2z44TF4RAzinISEvmLEEpiDIvHwErbpO
ETXxhV53mqqPC+Cay9EXuKCbkrf3+mRPh9d90E9Mzj8W429X5awL8wn5O0rvXOxo
m/rAvnzJVvByPRRGYNnrepvF95K/l1QjZn3k7UXnuMP3CdmGQq8tm7lL8aLftoUu
30zjkcLrjwZVeOpN25OZbGtqrpgoESDlwD3K0RCLt2ByfNDpzydI4cA8Blayr8m1
41w8nFe85RePVXhZIjgQgvsatULKlFVt0wHOSy4rXz08WCwncwF7SHmpqYTbTeKx
xCDOzCM8HKBEwO5p1awXlQMNo05Zs94Twx4LABEBAAG0G1Rlc3QgS2V5IDx0ZXN0
QGV4YW1wbGUuY29tPokBVwQTAQoAQRYhBE1TFzCtj6Omk/S9Xoo1RHbnrfGlBQJq
0d3XAhsDBQkAAVGABQsJCAcCAiICBhUKCQgLAgQWAgMBAh4HAheAAAoJEIo1RHbn
rfGlCrsH/23SO6uq1s+jetQCanQZWbx/sn2J86ziq9EPyv50sqyiCFV//of7c+Gy
v31ysCQrKWbgbb6cx/OXZW9Bc2+UxOel18gkQOEd9CArkrNK2BM9IolKUu4SEM54
t9UPMv60q9SQHow1JAV5ywJzOXHlt+3M+BVLjqea/a8NiZ2VUipgl4MV6nGY3GiG
9+OMlSiYpbmG3mAgjJPjX+sm8gES14NxZZb6IPO1oC7zkFmui/6/pjvbo/ZFwOcF
PmpA3/f8d1A2aXPeok0h1ow5adOxRq4p0CWb9DVo6vFUlYfWPVSQXJ6g4bqYDkTx
/NFv8LTmiUZyg8FDGZSkjI7GuLzVbN4=
=xoVT
-----END PGP PUBLIC KEY BLOCK-----
`

func TestGetCachedOrDownloadGPGKey_UsesCacheOnHit(t *testing.T) {
	oldCacheDir := Global().CacheDir
	Global().CacheDir = t.TempDir()
	t.Cleanup(func() {
		Global().CacheDir = oldCacheDir
	})

	keyURL := "https://invalid.example.test/non-routable-key.gpg"
	want := []byte(testGPGPublicKey)

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

// TestGetCachedOrDownloadGPGKey_InvalidCacheIsRefetched covers the bug this
// validation fixes: a cache entry that isn't a real OpenPGP key (e.g. from a
// past truncated/HTML-error download) must be treated as a cache miss, not
// trusted and reused forever, and the re-download must actually replace it
// on success. Uses a local httptest server (rather than a non-routable host)
// so the test is isolated, fast, and can assert the success path: a request
// was made, the returned bytes are the real key, and the poisoned cache file
// was overwritten with them.
func TestGetCachedOrDownloadGPGKey_InvalidCacheIsRefetched(t *testing.T) {
	if _, err := exec.LookPath("gpg"); err != nil {
		// Without gpg, validateGPGKeyData accepts any nonempty data (see
		// TestValidateGPGKeyDataNoGPGFallback), so the seeded HTML below
		// would pass as a cache hit instead of exercising this test's point.
		t.Skip("gpg not available; cache validation falls back to accepting any nonempty data")
	}

	oldCacheDir := Global().CacheDir
	Global().CacheDir = t.TempDir()
	t.Cleanup(func() {
		Global().CacheDir = oldCacheDir
	})

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write([]byte(testGPGPublicKey))
	}))
	defer server.Close()

	keyURL := server.URL + "/non-routable-key.gpg"

	cachePath, err := gpgKeyCacheFilePath(keyURL)
	if err != nil {
		t.Fatalf("gpgKeyCacheFilePath failed: %v", err)
	}
	if err := os.WriteFile(cachePath, []byte("<html>not a key</html>"), 0644); err != nil {
		t.Fatalf("failed to seed cached key: %v", err)
	}

	// The invalid cache entry must be rejected, forcing a real re-download
	// rather than silently returning the bad cached bytes.
	got, err := getCachedOrDownloadGPGKey(keyURL)
	if err != nil {
		t.Fatalf("expected the invalid cache entry to be rejected and re-downloaded, got error: %v", err)
	}
	if requests != 1 {
		t.Errorf("expected exactly one HTTP request to re-fetch the key, got %d", requests)
	}
	if string(got) != testGPGPublicKey {
		t.Errorf("re-downloaded key data = %q, want %q", string(got), testGPGPublicKey)
	}

	cached, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("failed to read cache file after refetch: %v", err)
	}
	if string(cached) != testGPGPublicKey {
		t.Errorf("poisoned cache entry was not replaced: cache file = %q, want %q", string(cached), testGPGPublicKey)
	}
}

func TestValidateGPGKeyData(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		wantErr     bool
		requiresGPG bool // needs gpg to actually parse/reject the data
	}{
		{name: "valid armored key", data: []byte(testGPGPublicKey), wantErr: false, requiresGPG: true},
		{name: "empty", data: []byte(""), wantErr: true},
		{name: "whitespace only", data: []byte("   \n\t  "), wantErr: true},
		{name: "HTML error page", data: []byte("<html><body>503 Service Unavailable</body></html>"), wantErr: true, requiresGPG: true},
		{name: "truncated garbage", data: []byte("not a key at all"), wantErr: true, requiresGPG: true},
	}

	gpgAvailable := true
	if _, err := exec.LookPath("gpg"); err != nil {
		gpgAvailable = false
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.requiresGPG && !gpgAvailable {
				t.Skip("gpg not available; see TestValidateGPGKeyDataNoGPGFallback for the no-gpg behavior")
			}
			err := validateGPGKeyData(tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateGPGKeyData(%q) error = %v, wantErr %v", tt.name, err, tt.wantErr)
			}
		})
	}
}

// TestValidateGPGKeyDataNoGPGFallback exercises the "gpg unavailable" branch
// directly and portably: PATH is overridden so exec.LookPath("gpg") (the
// only thing that decides this branch) fails regardless of whether this host
// actually has gpg installed. Nonempty data must then be accepted outright;
// empty data must still be rejected (that check runs before gpg is involved).
func TestValidateGPGKeyDataNoGPGFallback(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	if err := validateGPGKeyData([]byte("not a key, but gpg isn't available to say so")); err != nil {
		t.Errorf("expected nonempty data to be accepted when gpg is unavailable, got error: %v", err)
	}
	if err := validateGPGKeyData([]byte("")); err == nil {
		t.Error("expected empty data to still be rejected even when gpg is unavailable")
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
			map[string]int{"ppa.example.com": 990, "vendor.example.com": 990}},
		{"unlisted repo cannot upgrade", false, []string{"stack"},
			map[string]int{"ppa.example.com": 990, "vendor.example.com": NoUpgradePinPriority,
				"blocked.example.com": -10, "unset.example.com": NoUpgradePinPriority, "low.example.com": 10}},
		{"immutable pins every repo", true, []string{"stack"},
			map[string]int{"ppa.example.com": NoUpgradePinPriority, "vendor.example.com": NoUpgradePinPriority,
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

// TestGenerateAptSourcesOwnFilesRepository checks that a repository naming
// its own apt files (as the OEP Installer integration sets them) is written at
// those paths with the given content, and kept out of the shared sources list
// and ICT's generated pins.
func TestGenerateAptSourcesOwnFilesRepository(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "edge.gpg")
	if err := os.WriteFile(keyPath, []byte("binary-keyring"), 0o644); err != nil {
		t.Fatal(err)
	}
	ownLine := "deb [signed-by=/etc/apt/keyrings/intel-edge-pack.gpg] https://edge.example.com/edgepack noble unstable"
	template := &ImageTemplate{
		Target: TargetInfo{OS: "ubuntu"},
		PackageRepositories: []PackageRepository{
			{ID: "plain", Codename: "noble", URL: "https://plain.example.com/repo", Component: "main"},
			{
				Codename: "noble", URL: "https://edge.example.com/edgepack", Component: "unstable", PKey: keyPath,
				ListFile:        "/etc/apt/sources.list.d/intel-edge-pack.list",
				SourceLines:     []string{ownLine},
				KeyFile:         "/etc/apt/keyrings/intel-edge-pack.gpg",
				PreferencesFile: "/etc/apt/preferences.d/intel-edge-pack.pref",
				Preferences:     "Package: *\nPin: origin edge.example.com\nPin-Priority: 1001\n",
			},
		},
	}
	if err := template.GenerateAptSourcesFromRepositories(); err != nil {
		t.Fatalf("GenerateAptSourcesFromRepositories: %v", err)
	}
	contents := map[string]string{}
	for _, f := range template.SystemConfig.AdditionalFiles {
		data, err := os.ReadFile(filepath.Join(TempDir(), filepath.Base(f.Local)))
		if err != nil {
			t.Fatalf("reading %s: %v", f.Final, err)
		}
		contents[f.Final] = string(data)
		t.Cleanup(func() { os.Remove(filepath.Join(TempDir(), filepath.Base(f.Local))) })
	}
	if got := contents["/etc/apt/sources.list.d/intel-edge-pack.list"]; got != ownLine+"\n" {
		t.Errorf("own list file = %q", got)
	}
	shared := contents["/etc/apt/sources.list.d/package-repositories.list"]
	if strings.Contains(shared, "edge.example.com") || !strings.Contains(shared, "plain.example.com") {
		t.Errorf("shared sources list = %q", shared)
	}
	if got := contents["/etc/apt/keyrings/intel-edge-pack.gpg"]; got == "" {
		t.Error("keyring not written at the repository's key file")
	}
	if got := contents["/etc/apt/preferences.d/intel-edge-pack.pref"]; !strings.Contains(got, "Pin-Priority: 1001") {
		t.Errorf("own preferences file = %q", got)
	}
	for final, content := range contents {
		if strings.HasPrefix(final, "/etc/apt/preferences.d/") && final != "/etc/apt/preferences.d/intel-edge-pack.pref" &&
			strings.Contains(content, "edge.example.com") {
			t.Errorf("ICT generated a pin for the repository with its own files: %s", final)
		}
	}
}
