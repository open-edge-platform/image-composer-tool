package debutils

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/os-image-composer/internal/config"
	"github.com/open-edge-platform/os-image-composer/internal/ospackage"
)

// TestPackages tests the Packages function
func TestPackages(t *testing.T) {
	// Save original values
	origRepoCfg := RepoCfg
	origGzHref := GzHref
	defer func() {
		RepoCfg = origRepoCfg
		GzHref = origGzHref
	}()

	tests := []struct {
		name          string
		setupRepo     func() error
		expectError   bool
		errorContains string
		expectedCount int
	}{
		{
			name: "empty package list URL",
			setupRepo: func() error {
				RepoCfg = RepoConfig{
					PkgList:     "",
					PkgPrefix:   "http://example.com",
					ReleaseFile: "http://example.com/Release",
					ReleaseSign: "http://example.com/Release.gpg",
					PbGPGKey:    "dummy-key",
					BuildPath:   "./test-build",
					Arch:        "amd64",
				}
				GzHref = ""
				return nil
			},
			expectError:   true,
			errorContains: "parsing default repo failed",
		},
		{
			name: "invalid URL format",
			setupRepo: func() error {
				RepoCfg = RepoConfig{
					PkgList:     "invalid-url",
					PkgPrefix:   "invalid-prefix",
					ReleaseFile: "invalid-release",
					ReleaseSign: "invalid-sign",
					PbGPGKey:    "dummy-key",
					BuildPath:   "./test-build",
					Arch:        "amd64",
				}
				GzHref = "invalid-gz-href"
				return nil
			},
			expectError:   true,
			errorContains: "parsing default repo failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.setupRepo(); err != nil {
				t.Fatalf("Failed to setup test repo: %v", err)
			}

			packages, err := Packages()

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain %q, got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
				if len(packages) != tt.expectedCount {
					t.Errorf("Expected %d packages, got %d", tt.expectedCount, len(packages))
				}
			}
		})
	}
}

// TestPackagesFromMultipleRepos tests the PackagesFromMultipleRepos function
func TestPackagesFromMultipleRepos(t *testing.T) {
	// Save original values
	origRepoCfgs := RepoCfgs
	origRepoCfg := RepoCfg
	defer func() {
		RepoCfgs = origRepoCfgs
		RepoCfg = origRepoCfg
	}()

	tests := []struct {
		name          string
		setupRepos    func()
		expectError   bool
		errorContains string
	}{
		{
			name: "no multiple repositories configured - fallback to single",
			setupRepos: func() {
				RepoCfgs = []RepoConfig{}
				// Setup single repo with invalid config to trigger error
				RepoCfg = RepoConfig{
					PkgList: "invalid-url",
				}
			},
			expectError:   true,
			errorContains: "parsing default repo failed",
		},
		{
			name: "all repositories fail",
			setupRepos: func() {
				RepoCfgs = []RepoConfig{
					{
						Name:    "repo1",
						PkgList: "invalid-url1",
					},
					{
						Name:    "repo2",
						PkgList: "invalid-url2",
					},
				}
			},
			expectError:   true,
			errorContains: "all 2 repositories failed to parse",
		},
		{
			name: "some repositories fail but continue with others",
			setupRepos: func() {
				// This will fail because ParseRepositoryMetadata is not mocked
				RepoCfgs = []RepoConfig{
					{
						Name:        "repo1",
						PkgList:     "invalid-url1",
						PkgPrefix:   "http://example.com",
						ReleaseFile: "http://example.com/Release",
						ReleaseSign: "http://example.com/Release.gpg",
					},
					{
						Name:        "repo2",
						PkgList:     "invalid-url2",
						PkgPrefix:   "http://example2.com",
						ReleaseFile: "http://example2.com/Release",
						ReleaseSign: "http://example2.com/Release.gpg",
					},
				}
			},
			expectError:   true, // Will fail because ParseRepositoryMetadata is not mocked
			errorContains: "all 2 repositories failed to parse",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupRepos()

			packages, err := PackagesFromMultipleRepos()

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain %q, got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
				if packages == nil {
					t.Error("Expected packages to be non-nil")
				}
			}
		})
	}
}

// TestBuildRepoConfigs tests the BuildRepoConfigs function
func TestBuildRepoConfigs(t *testing.T) {
	tests := []struct {
		name          string
		userRepoList  []Repository
		arch          string
		expectError   bool
		errorContains string
		expectedCount int
	}{
		{
			name:          "empty repository list",
			userRepoList:  []Repository{},
			arch:          "amd64",
			expectError:   false,
			expectedCount: 0,
		},
		{
			name: "single repository with default component",
			userRepoList: []Repository{
				{
					ID:        "test-repo",
					Codename:  "stable",
					URL:       "http://example.com",
					PKey:      "test-key",
					Component: "", // Should default to "main"
				},
			},
			arch:          "amd64",
			expectError:   true, // Will fail because GetPackagesNames is not mocked
			errorContains: "fail connecting to repository",
		},
		{
			name: "single repository with specified component",
			userRepoList: []Repository{
				{
					ID:        "test-repo",
					Codename:  "stable",
					URL:       "http://example.com",
					PKey:      "test-key",
					Component: "contrib",
				},
			},
			arch:          "amd64",
			expectError:   true, // Will fail because GetPackagesNames is not mocked
			errorContains: "fail connecting to repository",
		},
		{
			name: "multiple components",
			userRepoList: []Repository{
				{
					ID:        "test-repo",
					Codename:  "stable",
					URL:       "http://example.com",
					PKey:      "test-key",
					Component: "main contrib non-free",
				},
			},
			arch:          "amd64",
			expectError:   true, // Will fail because GetPackagesNames is not mocked
			errorContains: "fail connecting to repository",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoConfigs, err := BuildRepoConfigs(tt.userRepoList, tt.arch)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain %q, got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
				if len(repoConfigs) != tt.expectedCount {
					t.Errorf("Expected %d repo configs, got %d", tt.expectedCount, len(repoConfigs))
				}
			}
		})
	}
}

// TestUserPackages tests the UserPackages function
func TestUserPackages(t *testing.T) {
	// Save original values
	origUserRepo := UserRepo
	origArch := Architecture
	defer func() {
		UserRepo = origUserRepo
		Architecture = origArch
	}()

	tests := []struct {
		name          string
		setupUserRepo func()
		expectError   bool
		errorContains string
	}{
		{
			name: "empty user repo",
			setupUserRepo: func() {
				UserRepo = nil
				Architecture = "amd64"
			},
			expectError: false,
		},
		{
			name: "placeholder URLs - should return empty package list",
			setupUserRepo: func() {
				UserRepo = []config.PackageRepository{
					{
						URL:      "<URL>",
						Codename: "stable",
						PKey:     "dummy-key",
					},
					{
						URL:      "",
						Codename: "stable",
						PKey:     "dummy-key",
					},
				}
				Architecture = "amd64"
			},
			expectError: false,
		},
		{
			name: "invalid user repo URL - should fail",
			setupUserRepo: func() {
				UserRepo = []config.PackageRepository{
					{
						URL:      "http://invalid-url.example.com",
						Codename: "stable",
						PKey:     "dummy-key",
					},
				}
				Architecture = "amd64"
			},
			expectError:   true,
			errorContains: "building user repo configs failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupUserRepo()

			packages, err := UserPackages()

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain %q, got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
				// packages can be nil or empty slice, both are acceptable for empty repos
				_ = packages // Use the variable to avoid "declared and not used" error
			}
		})
	}
}

// TestCheckFileExists tests the checkFileExists function
func TestCheckFileExists(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/exists":
			w.WriteHeader(http.StatusOK)
		case "/notfound":
			w.WriteHeader(http.StatusNotFound)
		case "/servererror":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	tests := []struct {
		name     string
		url      string
		expected bool
	}{
		{
			name:     "file exists",
			url:      server.URL + "/exists",
			expected: true,
		},
		{
			name:     "file not found",
			url:      server.URL + "/notfound",
			expected: false,
		},
		{
			name:     "server error",
			url:      server.URL + "/servererror",
			expected: false,
		},
		{
			name:     "invalid URL",
			url:      "invalid-url",
			expected: false,
		},
		{
			name:     "empty URL",
			url:      "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, _ := checkFileExists(tt.url)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

// TestValidate tests the Validate function
func TestValidate(t *testing.T) {
	// Save original PkgChecksum
	origPkgChecksum := PkgChecksum
	defer func() {
		PkgChecksum = origPkgChecksum
	}()

	tests := []struct {
		name          string
		setupFiles    func(tempDir string) error
		setupChecksum func()
		expectError   bool
		errorContains string
	}{
		{
			name: "no DEB files",
			setupFiles: func(tempDir string) error {
				// Create an empty directory
				return nil
			},
			setupChecksum: func() {
				PkgChecksum = nil
			},
			expectError: false,
		},
		{
			name: "valid DEB file",
			setupFiles: func(tempDir string) error {
				// Create a test DEB file
				debPath := filepath.Join(tempDir, "test.deb")
				content := []byte("test deb content")
				return os.WriteFile(debPath, content, 0644)
			},
			setupChecksum: func() {
				// This will fail because we don't have the actual checksum
				PkgChecksum = []pkgChecksum{
					{
						Name:     "test.deb",
						Checksum: "wrongchecksum",
					},
				}
			},
			expectError:   true,
			errorContains: "failed verification",
		},
		{
			name: "DEB file without checksum",
			setupFiles: func(tempDir string) error {
				debPath := filepath.Join(tempDir, "missing.deb")
				content := []byte("missing deb content")
				return os.WriteFile(debPath, content, 0644)
			},
			setupChecksum: func() {
				PkgChecksum = []pkgChecksum{
					{
						Name:     "other.deb",
						Checksum: "somechecksum",
					},
				}
			},
			expectError:   true,
			errorContains: "failed verification",
		},
		{
			name: "invalid directory",
			setupFiles: func(tempDir string) error {
				// Don't create the directory
				return nil
			},
			setupChecksum: func() {
				PkgChecksum = nil
			},
			expectError: false, // Glob on non-existent directory returns empty, which is valid
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir, err := os.MkdirTemp("", "validate_test")
			if err != nil {
				t.Fatalf("Failed to create temp directory: %v", err)
			}
			defer os.RemoveAll(tempDir)

			if err := tt.setupFiles(tempDir); err != nil {
				t.Fatalf("Failed to setup test files: %v", err)
			}

			tt.setupChecksum()

			files, err := filepath.Glob(filepath.Join(tempDir, "*.deb"))
			if err != nil {
				t.Fatalf("Failed to glob deb files: %v", err)
			}
			var debFiles []string
			for _, f := range files {
				debFiles = append(debFiles, filepath.Base(f))
			}

			err = Validate(tempDir, debFiles)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain %q, got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

// TestResolve tests the Resolve function
func TestResolve(t *testing.T) {
	// Save original PkgChecksum
	origPkgChecksum := PkgChecksum
	defer func() {
		PkgChecksum = origPkgChecksum
	}()

	tests := []struct {
		name          string
		req           []ospackage.PackageInfo
		all           []ospackage.PackageInfo
		expectError   bool
		errorContains string
	}{
		{
			name: "empty request",
			req:  []ospackage.PackageInfo{},
			all: []ospackage.PackageInfo{
				{
					Name: "test-pkg",
					URL:  "http://example.com/test-pkg.deb",
					Checksums: []ospackage.Checksum{
						{Algorithm: "SHA256", Value: "abc123"},
					},
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset PkgChecksum for each test
			PkgChecksum = []pkgChecksum{}

			resolved, err := Resolve(tt.req, tt.all)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain %q, got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
				if resolved == nil {
					t.Error("Expected resolved packages to be non-nil")
				}
				// Verify that PkgChecksum was populated
				if len(PkgChecksum) != len(tt.all) {
					t.Errorf("Expected PkgChecksum length %d, got %d", len(tt.all), len(PkgChecksum))
				}
			}
		})
	}
}

// TestMatchRequested tests the MatchRequested function
func TestMatchRequested(t *testing.T) {
	tests := []struct {
		name          string
		requests      []string
		all           []ospackage.PackageInfo
		expectError   bool
		errorContains string
		expectedCount int
	}{
		{
			name:     "empty request",
			requests: []string{},
			all: []ospackage.PackageInfo{
				{Name: "pkg1", URL: "http://example.com/pkg1.deb"},
			},
			expectError:   false,
			expectedCount: 0,
		},
		{
			name:     "found package",
			requests: []string{"pkg1"},
			all: []ospackage.PackageInfo{
				{Name: "pkg1", URL: "http://example.com/pkg1.deb"},
				{Name: "pkg2", URL: "http://example.com/pkg2.deb"},
			},
			expectError:   false,
			expectedCount: 1,
		},
		{
			name:     "missing package",
			requests: []string{"nonexistent"},
			all: []ospackage.PackageInfo{
				{Name: "pkg1", URL: "http://example.com/pkg1.deb"},
			},
			expectError:   true,
			errorContains: "not found",
		},
		{
			name:     "mixed found and missing",
			requests: []string{"pkg1", "nonexistent"},
			all: []ospackage.PackageInfo{
				{Name: "pkg1", URL: "http://example.com/pkg1.deb"},
			},
			expectError:   true,
			errorContains: "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matched, err := MatchRequested(tt.requests, tt.all)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain %q, got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
				if len(matched) != tt.expectedCount {
					t.Errorf("Expected %d matched packages, got %d", tt.expectedCount, len(matched))
				}
			}
		})
	}
}

func TestMatchRequestedKernelWildcardVersion(t *testing.T) {
	ConfigureKernelSelection([]string{"linux-image-generic*"}, "6.14")
	defer ConfigureKernelSelection(nil, "")

	all := []ospackage.PackageInfo{
		{
			Name:    "linux-image-generic-hwe-24.04",
			Version: "6.13.0-10.10~24.04.1",
			URL:     "pool/main/l/linux-meta-hwe-6.13/linux-image-generic-hwe-24.04_6.13.0-10.10~24.04.1_amd64.deb",
		},
		{
			Name:    "linux-image-generic-hwe-24.04",
			Version: "6.14.2.1.0",
			URL:     "pool/main/l/linux-meta-hwe-6.14/linux-image-generic-hwe-24.04_6.14.2.1.0_amd64.deb",
		},
	}

	matched, err := MatchRequested([]string{"linux-image-generic*"}, all)
	if err != nil {
		t.Fatalf("expected wildcard kernel request to resolve, got error: %v", err)
	}

	if len(matched) != 1 {
		t.Fatalf("expected one resolved package, got %d", len(matched))
	}

	if matched[0].Version != "6.14.2.1.0" {
		t.Fatalf("expected kernel version-compatible package, got %q", matched[0].Version)
	}
}

// TestWriteArrayToFile tests the WriteArrayToFile function
func TestWriteArrayToFile(t *testing.T) {
	// Save original ReportPath
	origReportPath := ReportPath
	defer func() {
		ReportPath = origReportPath
	}()

	tests := []struct {
		name          string
		arr           []string
		title         string
		setupDir      func() (string, error)
		expectError   bool
		errorContains string
	}{
		{
			name:  "empty array",
			arr:   []string{},
			title: "Test Report",
			setupDir: func() (string, error) {
				tempDir, err := os.MkdirTemp("", "report_test")
				if err != nil {
					return "", err
				}
				ReportPath = tempDir
				return tempDir, nil
			},
			expectError: false,
		},
		{
			name:  "single item",
			arr:   []string{"missing-pkg"},
			title: "Missing Packages",
			setupDir: func() (string, error) {
				tempDir, err := os.MkdirTemp("", "report_test")
				if err != nil {
					return "", err
				}
				ReportPath = tempDir
				return tempDir, nil
			},
			expectError: false,
		},
		{
			name:  "multiple items",
			arr:   []string{"pkg1", "pkg2", "pkg3"},
			title: "Test Items",
			setupDir: func() (string, error) {
				tempDir, err := os.MkdirTemp("", "report_test")
				if err != nil {
					return "", err
				}
				ReportPath = tempDir
				return tempDir, nil
			},
			expectError: false,
		},
		{
			name:  "title with spaces",
			arr:   []string{"test"},
			title: "Title With Spaces",
			setupDir: func() (string, error) {
				tempDir, err := os.MkdirTemp("", "report_test")
				if err != nil {
					return "", err
				}
				ReportPath = tempDir
				return tempDir, nil
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir, err := tt.setupDir()
			if err != nil {
				t.Fatalf("Failed to setup test directory: %v", err)
			}
			if tempDir != "" {
				defer os.RemoveAll(tempDir)
			}

			filename, err := WriteArrayToFile(tt.arr, tt.title)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				if tt.errorContains != "" && err != nil && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain %q, got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
				if filename == "" {
					t.Error("Expected non-empty filename")
				}

				// Verify the file was created
				if _, statErr := os.Stat(filename); os.IsNotExist(statErr) {
					t.Errorf("File %s was not created", filename)
				} else {
					// Verify the file content
					content, readErr := os.ReadFile(filename)
					if readErr != nil {
						t.Errorf("Failed to read created file: %v", readErr)
					} else {
						var report struct {
							ReportType string   `json:"report_type"`
							Missing    []string `json:"missing"`
						}
						if jsonErr := json.Unmarshal(content, &report); jsonErr != nil {
							t.Errorf("Failed to parse JSON: %v", jsonErr)
						} else {
							if report.ReportType != "missing_packages_report" {
								t.Errorf("Expected report_type 'missing_packages_report', got %s", report.ReportType)
							}
							if len(report.Missing) != len(tt.arr) {
								t.Errorf("Expected %d missing items, got %d", len(tt.arr), len(report.Missing))
							}
							for i, item := range tt.arr {
								if i < len(report.Missing) && report.Missing[i] != item {
									t.Errorf("Expected missing item %s, got %s", item, report.Missing[i])
								}
							}
						}
					}
				}

				// Verify filename format
				expectedPrefix := strings.ReplaceAll(tt.title, " ", "_")
				if !strings.Contains(filename, expectedPrefix) {
					t.Errorf("Expected filename to contain %s, got %s", expectedPrefix, filename)
				}
				if !strings.HasSuffix(filename, ".json") {
					t.Errorf("Expected filename to end with .json, got %s", filename)
				}
			}
		})
	}
}

// TestDownloadPackagesComplete tests the DownloadPackagesComplete function
func TestDownloadPackagesComplete(t *testing.T) {
	// Save original values
	origRepoCfg := RepoCfg
	origRepoCfgs := RepoCfgs
	origUserRepo := UserRepo
	origPkgChecksum := PkgChecksum
	origGzHref := GzHref
	defer func() {
		RepoCfg = origRepoCfg
		RepoCfgs = origRepoCfgs
		UserRepo = origUserRepo
		PkgChecksum = origPkgChecksum
		GzHref = origGzHref
	}()

	tests := []struct {
		name          string
		pkgList       []string
		setup         func() error
		expectError   bool
		errorContains string
	}{
		{
			name:    "empty package list with multiple repos",
			pkgList: []string{},
			setup: func() error {
				// Setup multiple repos with invalid config
				RepoCfgs = []RepoConfig{
					{Name: "repo1", PkgList: "invalid-url1"},
					{Name: "repo2", PkgList: "invalid-url2"},
				}
				UserRepo = nil
				return nil
			},
			expectError:   true,
			errorContains: "getting packages",
		},
		{
			name:    "empty package list with single repo fallback",
			pkgList: []string{},
			setup: func() error {
				// No multiple repos configured, should fallback to single
				RepoCfgs = []RepoConfig{}
				RepoCfg = RepoConfig{
					PkgList: "invalid-url",
				}
				UserRepo = nil
				GzHref = "invalid-gz"
				return nil
			},
			expectError:   true,
			errorContains: "getting packages",
		},
		{
			name:    "valid package list but invalid configuration",
			pkgList: []string{"test-pkg"},
			setup: func() error {
				RepoCfgs = []RepoConfig{}
				RepoCfg = RepoConfig{
					PkgList: "invalid-url",
				}
				UserRepo = nil
				return nil
			},
			expectError:   true,
			errorContains: "getting packages",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir, err := os.MkdirTemp("", "download_complete_test")
			if err != nil {
				t.Fatalf("Failed to create temp directory: %v", err)
			}
			defer os.RemoveAll(tempDir)

			if err := tt.setup(); err != nil {
				t.Fatalf("Failed to setup test: %v", err)
			}

			downloadList, packageInfos, err := DownloadPackagesComplete(tt.pkgList, tempDir, "", nil, false)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain %q, got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
				if downloadList == nil {
					t.Error("Expected download list to be non-nil")
				}
				if packageInfos == nil {
					t.Error("Expected package infos to be non-nil")
				}
			}
		})
	}
}

// TestDownloadPackages tests the DownloadPackages function
func TestDownloadPackages(t *testing.T) {
	// Save original values
	origRepoCfg := RepoCfg
	origRepoCfgs := RepoCfgs
	origUserRepo := UserRepo
	origPkgChecksum := PkgChecksum
	origGzHref := GzHref
	defer func() {
		RepoCfg = origRepoCfg
		RepoCfgs = origRepoCfgs
		UserRepo = origUserRepo
		PkgChecksum = origPkgChecksum
		GzHref = origGzHref
	}()

	tests := []struct {
		name          string
		pkgList       []string
		setup         func() error
		expectError   bool
		errorContains string
	}{
		{
			name:    "empty package list with multiple repos",
			pkgList: []string{},
			setup: func() error {
				// Set up multiple repos with invalid configuration
				RepoCfgs = []RepoConfig{
					{Name: "repo1", PkgList: "invalid-url1"},
				}
				UserRepo = nil
				return nil
			},
			expectError:   true,
			errorContains: "getting packages",
		},
		{
			name:    "empty package list with single repo fallback",
			pkgList: []string{},
			setup: func() error {
				// No multiple repos configured - should fallback to single repo
				RepoCfgs = []RepoConfig{}
				RepoCfg = RepoConfig{}
				UserRepo = nil
				GzHref = ""
				return nil
			},
			expectError:   true,
			errorContains: "getting packages",
		},
		{
			name:    "invalid configuration with multiple repos",
			pkgList: []string{"test-pkg"},
			setup: func() error {
				RepoCfgs = []RepoConfig{
					{
						Name:    "repo1",
						PkgList: "invalid-url",
					},
				}
				UserRepo = nil
				return nil
			},
			expectError:   true,
			errorContains: "getting packages",
		},
		{
			name:    "invalid configuration with single repo fallback",
			pkgList: []string{"test-pkg"},
			setup: func() error {
				RepoCfgs = []RepoConfig{}
				RepoCfg = RepoConfig{
					PkgList: "invalid-url",
				}
				UserRepo = nil
				GzHref = "invalid-gz"
				return nil
			},
			expectError:   true,
			errorContains: "getting packages",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir, err := os.MkdirTemp("", "download_test")
			if err != nil {
				t.Fatalf("Failed to create temp directory: %v", err)
			}
			defer os.RemoveAll(tempDir)

			if err := tt.setup(); err != nil {
				t.Fatalf("Failed to setup test: %v", err)
			}

			downloadList, err := DownloadPackages(tt.pkgList, tempDir, "", nil, false)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain %q, got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
				if downloadList == nil {
					t.Error("Expected download list to be non-nil")
				}
			}
		})
	}
}

// TestRepoConfig tests the RepoConfig struct
func TestRepoConfig(t *testing.T) {
	config := RepoConfig{
		Section:      "[main]",
		Name:         "Main Repository",
		PkgList:      "http://example.com/packages.gz",
		PkgPrefix:    "http://example.com",
		GPGCheck:     true,
		RepoGPGCheck: true,
		Enabled:      true,
		PbGPGKey:     "test-key",
		ReleaseFile:  "http://example.com/Release",
		ReleaseSign:  "http://example.com/Release.gpg",
		BuildPath:    filepath.Join(config.TempDir(), "builds", "main"),
		Arch:         "amd64",
	}

	// Verify all fields are set correctly
	if config.Section != "[main]" {
		t.Errorf("Expected Section '[main]', got %s", config.Section)
	}
	if config.Name != "Main Repository" {
		t.Errorf("Expected Name 'Main Repository', got %s", config.Name)
	}
	if config.PkgList != "http://example.com/packages.gz" {
		t.Errorf("Expected PkgList 'http://example.com/packages.gz', got %s", config.PkgList)
	}
	if !config.GPGCheck {
		t.Error("Expected GPGCheck to be true")
	}
	if !config.RepoGPGCheck {
		t.Error("Expected RepoGPGCheck to be true")
	}
	if !config.Enabled {
		t.Error("Expected Enabled to be true")
	}
}

// TestPkgChecksum tests the pkgChecksum struct
func TestPkgChecksum(t *testing.T) {
	checksum := pkgChecksum{
		Name:     "test-package.deb",
		Checksum: "abc123def456",
	}

	if checksum.Name != "test-package.deb" {
		t.Errorf("Expected Name 'test-package.deb', got %s", checksum.Name)
	}
	if checksum.Checksum != "abc123def456" {
		t.Errorf("Expected Checksum 'abc123def456', got %s", checksum.Checksum)
	}
}

// TestRepository tests the Repository struct
func TestRepository(t *testing.T) {
	repo := Repository{
		ID:        "test-repo-1",
		Codename:  "stable",
		URL:       "http://example.com/debian",
		PKey:      "test-gpg-key",
		Component: "main contrib",
	}

	// Verify all fields are set correctly
	if repo.ID != "test-repo-1" {
		t.Errorf("Expected ID 'test-repo-1', got %s", repo.ID)
	}
	if repo.Codename != "stable" {
		t.Errorf("Expected Codename 'stable', got %s", repo.Codename)
	}
	if repo.URL != "http://example.com/debian" {
		t.Errorf("Expected URL 'http://example.com/debian', got %s", repo.URL)
	}
	if repo.PKey != "test-gpg-key" {
		t.Errorf("Expected PKey 'test-gpg-key', got %s", repo.PKey)
	}
	if repo.Component != "main contrib" {
		t.Errorf("Expected Component 'main contrib', got %s", repo.Component)
	}
}

// TestGlobalVariables tests global variable initialization
func TestGlobalVariables(t *testing.T) {
	// Test that global variables can be modified
	origReportPath := ReportPath
	origRepoCfgs := RepoCfgs
	defer func() {
		ReportPath = origReportPath
		RepoCfgs = origRepoCfgs
	}()

	// Test default value
	if ReportPath != "builds" {
		t.Errorf("Expected default ReportPath 'builds', got %s", ReportPath)
	}

	// Test modification
	ReportPath = "custom-builds"
	if ReportPath != "custom-builds" {
		t.Errorf("Expected ReportPath 'custom-builds', got %s", ReportPath)
	}

	// Test that slices can be modified
	origPkgChecksum := PkgChecksum
	PkgChecksum = []pkgChecksum{
		{Name: "test1.deb", Checksum: "hash1"},
		{Name: "test2.deb", Checksum: "hash2"},
	}

	if len(PkgChecksum) != 2 {
		t.Errorf("Expected PkgChecksum length 2, got %d", len(PkgChecksum))
	}

	PkgChecksum = origPkgChecksum

	// Test RepoCfgs slice
	RepoCfgs = []RepoConfig{
		{
			Name:    "repo1",
			PkgList: "http://example.com/packages1",
			Arch:    "amd64",
		},
		{
			Name:    "repo2",
			PkgList: "http://example.com/packages2",
			Arch:    "arm64",
		},
	}

	if len(RepoCfgs) != 2 {
		t.Errorf("Expected RepoCfgs length 2, got %d", len(RepoCfgs))
	}

	if RepoCfgs[0].Name != "repo1" {
		t.Errorf("Expected RepoCfgs[0].Name 'repo1', got %s", RepoCfgs[0].Name)
	}

	if RepoCfgs[1].Arch != "arm64" {
		t.Errorf("Expected RepoCfgs[1].Arch 'arm64', got %s", RepoCfgs[1].Arch)
	}
}
