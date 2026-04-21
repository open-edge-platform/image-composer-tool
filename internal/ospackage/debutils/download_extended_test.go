package debutils_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage/debutils"
)

// TestPackagesFromMultipleRepos tests package retrieval from multiple repositories
func TestPackagesFromMultipleRepos(t *testing.T) {
	// Save original values
	origRepoCfgs := debutils.RepoCfgs
	defer func() {
		debutils.RepoCfgs = origRepoCfgs
	}()

	tests := []struct {
		name        string
		setupRepos  func()
		expectError bool
	}{
		{
			name: "no repositories configured",
			setupRepos: func() {
				debutils.RepoCfgs = []debutils.RepoConfig{}
			},
			expectError: true,
		},
		{
			name: "single repository configured",
			setupRepos: func() {
				debutils.RepoCfgs = []debutils.RepoConfig{
					{
						PkgList:     "http://example.com/Packages.gz",
						PkgPrefix:   "http://example.com",
						ReleaseFile: "http://example.com/Release",
						ReleaseSign: "http://example.com/Release.gpg",
						PbGPGKey:    "dummy-key",
						BuildPath:   "./test-build-1",
						Arch:        "amd64",
						Name:        "repo1",
					},
				}
			},
			expectError: true, // Will fail due to network issues in test environment
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupRepos()

			packages, err := debutils.PackagesFromMultipleRepos()

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Expected no error but got: %v", err)
				return
			}

			if packages == nil {
				t.Error("Expected non-nil packages")
			}
		})
	}
}

// TestGetPackagesNames tests package list URL determination
func TestGetPackagesNames(t *testing.T) {
	// Create a test server that responds to specific URLs
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/dists/stable/main/binary-amd64/Packages.gz":
			w.WriteHeader(http.StatusOK)
		case "/dists/stable/main/binary-amd64/Packages.xz":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	tests := []struct {
		name        string
		baseURL     string
		codename    string
		arch        string
		component   string
		expectFound bool
	}{
		{
			name:        "finds Packages.gz",
			baseURL:     server.URL,
			codename:    "stable",
			arch:        "amd64",
			component:   "main",
			expectFound: true,
		},
		{
			name:        "no packages file found",
			baseURL:     server.URL,
			codename:    "nonexistent",
			arch:        "amd64",
			component:   "main",
			expectFound: false,
		},
		{
			name:        "invalid URL",
			baseURL:     "invalid-url",
			codename:    "stable",
			arch:        "amd64",
			component:   "main",
			expectFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := debutils.GetPackagesNames(tt.baseURL, tt.codename, tt.arch, tt.component)

			if !tt.expectFound {
				if err == nil && result != "" {
					t.Error("Expected no result but got one")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if result == "" {
				t.Error("Expected non-empty result")
			}
		})
	}
}

// TestUserPackagesWithConfig tests UserPackages with various configurations
func TestUserPackagesWithConfig(t *testing.T) {
	// Save original values
	origUserRepo := debutils.UserRepo
	origArch := debutils.Architecture
	defer func() {
		debutils.UserRepo = origUserRepo
		debutils.Architecture = origArch
	}()

	tests := []struct {
		name        string
		setupConfig func()
		expectError bool
	}{
		{
			name: "placeholder URL is skipped",
			setupConfig: func() {
				debutils.UserRepo = []config.PackageRepository{
					{
						URL:      "<URL>", // Placeholder URL should be skipped
						Codename: "stable",
						PKey:     "dummy-key",
					},
				}
				debutils.Architecture = "amd64"
			},
			expectError: false, // Placeholder should be skipped without error
		},
		{
			name: "valid URL but network will fail",
			setupConfig: func() {
				debutils.UserRepo = []config.PackageRepository{
					{
						URL:      "http://example.com",
						Codename: "stable",
						PKey:     "dummy-key",
					},
				}
				debutils.Architecture = "amd64"
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupConfig()

			packages, err := debutils.UserPackages()

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Expected no error but got: %v", err)
				return
			}

			// For successful cases, packages can be nil or empty
			_ = packages
		})
	}
}

// TestDownloadPackagesBasic tests basic download scenarios
func TestDownloadPackagesBasic(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Save original values
	origRepoCfg := debutils.RepoCfg
	defer func() {
		debutils.RepoCfg = origRepoCfg
	}()

	tempDir, err := os.MkdirTemp("", "download_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name        string
		pkgList     []string
		setup       func()
		expectError bool
	}{
		{
			name:    "empty package list",
			pkgList: []string{},
			setup: func() {
				debutils.RepoCfg = debutils.RepoConfig{}
			},
			expectError: true, // Will fail due to invalid repo configuration
		},
		{
			name:    "invalid repository configuration",
			pkgList: []string{"test-pkg"},
			setup: func() {
				debutils.RepoCfg = debutils.RepoConfig{
					PkgList: "invalid-url",
				}
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup()

			downloadList, err := debutils.DownloadPackages(tt.pkgList, tempDir, "", nil, false)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Expected no error but got: %v", err)
				return
			}

			if downloadList == nil {
				t.Error("Expected download list to be non-nil")
			}
		})
	}
}

// TestWriteArrayToFileBasic tests basic report writing functionality
func TestWriteArrayToFileBasic(t *testing.T) {
	// Save original ReportPath
	origReportPath := debutils.ReportPath
	defer func() {
		debutils.ReportPath = origReportPath
	}()

	tempDir, err := os.MkdirTemp("", "report_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	debutils.ReportPath = tempDir

	tests := []struct {
		name        string
		arr         []string
		title       string
		expectError bool
	}{
		{
			name:        "normal case",
			arr:         []string{"pkg1", "pkg2", "pkg3"},
			title:       "Test Report",
			expectError: false,
		},
		{
			name:        "empty array",
			arr:         []string{},
			title:       "Empty Report",
			expectError: false,
		},
		{
			name:        "array with special characters",
			arr:         []string{"pkg-with-dashes", "pkg_with_underscores"},
			title:       "Special Chars Report",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filename, err := debutils.WriteArrayToFile(tt.arr, tt.title)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Expected no error but got: %v", err)
				return
			}

			if filename == "" {
				t.Error("Expected non-empty filename")
				return
			}

			// Verify file exists
			if _, err := os.Stat(filename); os.IsNotExist(err) {
				t.Errorf("File %s does not exist", filename)
			}
		})
	}
}

// TestBuildRepoConfigsBasic tests BuildRepoConfigs function
func TestBuildRepoConfigsBasic(t *testing.T) {
	tests := []struct {
		name         string
		repositories []debutils.Repository
		arch         string
		expectError  bool
	}{
		{
			name:         "empty repositories list",
			repositories: []debutils.Repository{},
			arch:         "amd64",
			expectError:  false,
		},
		{
			name: "single valid repository",
			repositories: []debutils.Repository{
				{
					ID:       "test-repo",
					Codename: "stable",
					URL:      "http://example.com",
					PKey:     "dummy-key",
				},
			},
			arch:        "amd64",
			expectError: true, // Will fail due to network issues
		},
		{
			name: "multiple repositories",
			repositories: []debutils.Repository{
				{
					ID:       "test-repo-1",
					Codename: "stable",
					URL:      "http://example1.com",
					PKey:     "dummy-key-1",
				},
				{
					ID:       "test-repo-2",
					Codename: "testing",
					URL:      "http://example2.com",
					PKey:     "dummy-key-2",
				},
			},
			arch:        "amd64",
			expectError: true, // Will fail due to network issues
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configs, err := debutils.BuildRepoConfigs(tt.repositories, tt.arch)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Expected no error but got: %v", err)
				return
			}

			// For empty input, configs can be nil or empty slice
			if configs == nil && len(tt.repositories) > 0 {
				t.Error("Expected non-nil configs for non-empty repositories")
			}

			if len(configs) != len(tt.repositories) {
				t.Errorf("Expected %d configs, got %d", len(tt.repositories), len(configs))
			}
		})
	}
}
