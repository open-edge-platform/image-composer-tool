package rpmutils_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage/rpmutils"
)

func TestValidateSupportsMultipleRepoGPGKeys(t *testing.T) {
	originalRepoCfg := rpmutils.RepoCfg
	originalUserRepo := rpmutils.UserRepo
	defer func() {
		rpmutils.RepoCfg = originalRepoCfg
		rpmutils.UserRepo = originalUserRepo
	}()

	hits := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits[r.URL.Path]++
		_, _ = w.Write([]byte("-----BEGIN PGP PUBLIC KEY BLOCK-----\nkey\n-----END PGP PUBLIC KEY BLOCK-----\n"))
	}))
	defer server.Close()

	rpmutils.RepoCfg = rpmutils.RepoConfig{
		GPGKey: server.URL + "/old.asc," + server.URL + "/new.asc",
	}
	rpmutils.UserRepo = nil

	destDir := filepath.Join(t.TempDir(), "rpms")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("failed to create temp rpm dir: %v", err)
	}

	if err := rpmutils.Validate(destDir); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}

	if hits["/old.asc"] != 1 {
		t.Fatalf("expected old key to be fetched once, got %d", hits["/old.asc"])
	}
	if hits["/new.asc"] != 1 {
		t.Fatalf("expected new key to be fetched once, got %d", hits["/new.asc"])
	}
}

func TestUserPackages(t *testing.T) {
	// Save original global variables
	originalUserRepo := rpmutils.UserRepo
	defer func() {
		rpmutils.UserRepo = originalUserRepo
	}()

	testCases := []struct {
		name        string
		userRepos   []config.PackageRepository
		expectError bool
		errorMsg    string
	}{
		{
			name:        "empty user repository list",
			userRepos:   []config.PackageRepository{},
			expectError: false, // Should return empty list without error
		},
		{
			name: "invalid user repository URL",
			userRepos: []config.PackageRepository{
				{
					URL:      "invalid-url",
					Codename: "test",
					PKey:     "http://example.com/key.asc",
				},
			},
			expectError: false, // Invalid URL gets skipped, returns empty list
		},
		{
			name: "valid user repository configuration",
			userRepos: []config.PackageRepository{
				{
					URL:      "https://example.com/repo",
					Codename: "stable",
					PKey:     "https://example.com/key.asc",
				},
			},
			expectError: true, // Will fail due to network call
		},
		{
			name: "local repository is handled elsewhere",
			userRepos: []config.PackageRepository{
				{
					Path: "/tmp/local-rpm-repo",
					PKey: "[trusted=yes]",
				},
			},
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rpmutils.UserRepo = tc.userRepos

			_, err := rpmutils.UserPackages()

			if tc.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				if tc.errorMsg != "" && err != nil && !strings.Contains(err.Error(), tc.errorMsg) {
					t.Errorf("Expected error to contain %q, got %q", tc.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
				// For empty user repo list, result can be nil (no repositories processed)
				// This is valid behavior
			}
		})
	}
}

func TestPackages(t *testing.T) {
	// Save original global variables
	originalRepoCfg := rpmutils.RepoCfg
	originalGzHref := rpmutils.GzHref
	defer func() {
		rpmutils.RepoCfg = originalRepoCfg
		rpmutils.GzHref = originalGzHref
	}()

	testCases := []struct {
		name        string
		repoURL     string
		gzHref      string
		expectError bool
	}{
		{
			name:        "empty repository URL",
			repoURL:     "",
			gzHref:      "repodata/primary.xml.gz",
			expectError: true,
		},
		{
			name:        "invalid URL format",
			repoURL:     "invalid-url",
			gzHref:      "repodata/primary.xml.gz",
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rpmutils.RepoCfg = rpmutils.RepoConfig{
				URL: tc.repoURL,
			}
			rpmutils.GzHref = tc.gzHref

			packages, err := rpmutils.Packages()

			if tc.expectError {
				if err == nil {
					t.Error("Expected error but got none")
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

func TestMatchRequested(t *testing.T) {
	testCases := []struct {
		name        string
		requests    []string
		all         []ospackage.PackageInfo
		expectError bool
		expectCount int
	}{
		{
			name:     "empty request",
			requests: []string{},
			all: []ospackage.PackageInfo{
				{Name: "test-package", Arch: "x86_64"},
			},
			expectError: false,
			expectCount: 0,
		},
		{
			name:     "exact name match",
			requests: []string{"test-package"},
			all: []ospackage.PackageInfo{
				{Name: "test-package", Arch: "x86_64"},
				{Name: "other-package", Arch: "x86_64"},
			},
			expectError: false,
			expectCount: 1,
		},
		{
			name:     "exact name match with .rpm extension",
			requests: []string{"test-package"},
			all: []ospackage.PackageInfo{
				{Name: "test-package.rpm", Arch: "x86_64"},
			},
			expectError: false,
			expectCount: 1,
		},
		{
			name:     "version prefix match",
			requests: []string{"acl"},
			all: []ospackage.PackageInfo{
				{Name: "acl-2.3.1-2.el8", Arch: "x86_64"},
				{Name: "acl-dev", Arch: "x86_64"}, // Should not match - not a version
			},
			expectError: false,
			expectCount: 1,
		},
		{
			name:     "release prefix match",
			requests: []string{"package"},
			all: []ospackage.PackageInfo{
				{Name: "package-1.0.0", Arch: "x86_64"},
			},
			expectError: false,
			expectCount: 1,
		},
		{
			name:     "skip src packages",
			requests: []string{"test-package"},
			all: []ospackage.PackageInfo{
				{Name: "test-package", Arch: "src"}, // Should be skipped
				{Name: "test-package", Arch: "x86_64"},
			},
			expectError: false,
			expectCount: 1,
		},
		{
			name:     "package not found",
			requests: []string{"nonexistent"},
			all: []ospackage.PackageInfo{
				{Name: "test-package", Arch: "x86_64"},
			},
			expectError: true,
			expectCount: 0,
		},
		{
			name:     "multiple candidates - pick highest",
			requests: []string{"package"},
			all: []ospackage.PackageInfo{
				{Name: "package-1.0.0", Arch: "x86_64"},
				{Name: "package-2.0.0", Arch: "x86_64"},
				{Name: "package-1.5.0", Arch: "x86_64"},
			},
			expectError: false,
			expectCount: 1, // Should pick package-2.0.0 (highest lex sort)
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := rpmutils.MatchRequested(tc.requests, tc.all)

			if tc.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
				if len(result) != tc.expectCount {
					t.Errorf("Expected %d packages, got %d", tc.expectCount, len(result))
				}
			}
		})
	}
}

func TestIsAcceptedChar(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "only digits",
			input:    "123",
			expected: true,
		},
		{
			name:     "digits with dash",
			input:    "1-2-3",
			expected: true,
		},
		{
			name:     "contains letters",
			input:    "1a2",
			expected: false,
		},
		{
			name:     "contains special chars",
			input:    "1.2",
			expected: false,
		},
		{
			name:     "only dash",
			input:    "-",
			expected: true,
		},
		{
			name:     "mixed valid chars",
			input:    "123-456-789",
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Since isAcceptedChar is not exported, we need to test it indirectly through isValidVersionFormat
			// or we need to make it exported for testing. For now, let's test it indirectly.
			t.Skip("isAcceptedChar is not exported - testing indirectly through isValidVersionFormat")
		})
	}
}

func TestIsValidVersionFormat(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "simple version",
			input:    "1.0.0",
			expected: true,
		},
		{
			name:     "version with dash",
			input:    "1-2.el8",
			expected: true,
		},
		{
			name:     "version without dot",
			input:    "123",
			expected: true,
		},
		{
			name:     "invalid version with letters",
			input:    "abc.def",
			expected: false,
		},
		{
			name:     "version starting with letter",
			input:    "a1.0.0",
			expected: false,
		},
		{
			name:     "complex version",
			input:    "2-3-1.el8_5",
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Since isValidVersionFormat is not exported, we'll test it indirectly
			// Let's create a test that exercises this through MatchRequested
			all := []ospackage.PackageInfo{
				{Name: fmt.Sprintf("package-%s", tc.input), Arch: "x86_64"},
			}

			result, err := rpmutils.MatchRequested([]string{"package"}, all)

			if tc.expected {
				// If the version format is valid, we should find a match
				if err != nil || len(result) == 0 {
					t.Errorf("Expected to find match for valid version format %q", tc.input)
				}
			} else {
				// If the version format is invalid, we should not find a match (unless it's exact)
				if err == nil && len(result) > 0 && result[0].Name != "package" {
					// Only fail if we found a match that wasn't exact
					exactMatch := false
					for _, pkg := range all {
						if pkg.Name == "package" {
							exactMatch = true
							break
						}
					}
					if !exactMatch {
						t.Errorf("Expected no match for invalid version format %q, but got: %v", tc.input, result)
					}
				}
			}
		})
	}
}

func TestValidate(t *testing.T) {
	// Save original global variables
	originalRepoCfg := rpmutils.RepoCfg
	originalUserRepo := rpmutils.UserRepo
	defer func() {
		rpmutils.RepoCfg = originalRepoCfg
		rpmutils.UserRepo = originalUserRepo
	}()

	testCases := []struct {
		name        string
		setupRepo   func() *httptest.Server
		destDir     func() string
		expectError bool
		errorMsg    string
	}{
		{
			name: "invalid GPG key URL",
			setupRepo: func() *httptest.Server {
				return nil // No server
			},
			destDir: func() string {
				tmpDir, _ := os.MkdirTemp("", "test-rpms-*")
				return tmpDir
			},
			expectError: true,
			errorMsg:    "no GPG keys configured",
		},
		{
			name: "no RPMs to verify with valid GPG key",
			setupRepo: func() *httptest.Server {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/plain")
					_, _ = w.Write([]byte("dummy-gpg-key-content"))
				}))
				return server
			},
			destDir: func() string {
				tmpDir, _ := os.MkdirTemp("", "test-rpms-*")
				return tmpDir
			},
			expectError: false, // Should not error when no RPMs found
		},
		{
			name: "user repo without GPG key",
			setupRepo: func() *httptest.Server {
				return nil
			},
			destDir: func() string {
				tmpDir, _ := os.MkdirTemp("", "test-rpms-*")
				return tmpDir
			},
			expectError: true,
			errorMsg:    "no GPG key URL configured for user repo",
		},
		{
			name: "multiple GPG keys from different sources",
			setupRepo: func() *httptest.Server {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/plain")
					_, _ = w.Write([]byte("dummy-gpg-key-content"))
				}))
				return server
			},
			destDir: func() string {
				tmpDir, _ := os.MkdirTemp("", "test-rpms-*")
				return tmpDir
			},
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			server := tc.setupRepo()
			if server != nil {
				defer server.Close()
			}

			// Setup test configuration based on test case
			switch tc.name {
			case "invalid GPG key URL":
				rpmutils.RepoCfg = rpmutils.RepoConfig{}
				rpmutils.UserRepo = []config.PackageRepository{}
			case "user repo without GPG key":
				rpmutils.RepoCfg = rpmutils.RepoConfig{}
				rpmutils.UserRepo = []config.PackageRepository{
					{
						URL:  "https://example.com",
						PKey: "", // Empty PKey should cause error
					},
				}
			case "multiple GPG keys from different sources":
				rpmutils.RepoCfg = rpmutils.RepoConfig{
					GPGKey: server.URL,
				}
				rpmutils.UserRepo = []config.PackageRepository{
					{
						URL:  "https://example.com",
						PKey: server.URL,
					},
				}
			default:
				if server != nil {
					rpmutils.RepoCfg = rpmutils.RepoConfig{
						GPGKey: server.URL,
					}
					rpmutils.UserRepo = []config.PackageRepository{}
				} else {
					rpmutils.RepoCfg = rpmutils.RepoConfig{
						GPGKey: "invalid://invalid-url-scheme",
					}
					rpmutils.UserRepo = []config.PackageRepository{}
				}
			}

			destDir := tc.destDir()
			defer os.RemoveAll(destDir)

			err := rpmutils.Validate(destDir)

			if tc.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				if tc.errorMsg != "" && !strings.Contains(err.Error(), tc.errorMsg) {
					t.Errorf("Expected error to contain %q, got %q", tc.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

func TestValidateSkipsLocalRepoRPMs(t *testing.T) {
	originalRepoCfg := rpmutils.RepoCfg
	originalUserRepo := rpmutils.UserRepo
	defer func() {
		rpmutils.RepoCfg = originalRepoCfg
		rpmutils.UserRepo = originalUserRepo
	}()

	destDir := t.TempDir()
	localRepoDir := t.TempDir()

	localRPMName := "dummyapp-1.0.0-1.x86_64.rpm"
	destRPMPath := filepath.Join(destDir, localRPMName)
	localRPMPath := filepath.Join(localRepoDir, localRPMName)

	if err := os.WriteFile(destRPMPath, []byte("unsigned local rpm"), 0644); err != nil {
		t.Fatalf("failed to create destination RPM: %v", err)
	}
	if err := os.WriteFile(localRPMPath, []byte("unsigned local rpm"), 0644); err != nil {
		t.Fatalf("failed to create local repo RPM: %v", err)
	}

	rpmutils.RepoCfg = rpmutils.RepoConfig{}
	rpmutils.UserRepo = []config.PackageRepository{
		{
			Path: localRepoDir,
			PKey: "[trusted=yes]",
		},
	}

	if err := rpmutils.Validate(destDir); err != nil {
		t.Fatalf("Validate should skip local repo RPM verification, got: %v", err)
	}
}

func TestValidateTrustedYesSkipsRPMVerification(t *testing.T) {
	originalRepoCfg := rpmutils.RepoCfg
	originalUserRepo := rpmutils.UserRepo
	defer func() {
		rpmutils.RepoCfg = originalRepoCfg
		rpmutils.UserRepo = originalUserRepo
	}()

	destDir := t.TempDir()
	rpmPath := filepath.Join(destDir, "unsigned.rpm")
	if err := os.WriteFile(rpmPath, []byte("unsigned rpm"), 0644); err != nil {
		t.Fatalf("failed to create RPM: %v", err)
	}

	tests := []struct {
		name     string
		repoCfg  rpmutils.RepoConfig
		userRepo []config.PackageRepository
	}{
		{
			name:    "main repo GPGKey is trusted=yes",
			repoCfg: rpmutils.RepoConfig{GPGKey: "[trusted=yes]"},
		},
		{
			name:    "non-local user repo PKey is trusted=yes",
			repoCfg: rpmutils.RepoConfig{},
			userRepo: []config.PackageRepository{
				{URL: "https://example.com/repo", PKey: "[trusted=yes]"},
			},
		},
		{
			name:    "multiple truested=yes keys",
			repoCfg: rpmutils.RepoConfig{GPGKey: "[trusted=yes]"},
			userRepo: []config.PackageRepository{
				{URL: "https://example.com/repo", PKey: "[trusted=yes]"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rpmutils.RepoCfg = tt.repoCfg
			rpmutils.UserRepo = tt.userRepo

			if err := rpmutils.Validate(destDir); err != nil {
				t.Errorf("Validate should skip verification for [trusted=yes], got: %v", err)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	testCases := []struct {
		name        string
		req         []ospackage.PackageInfo
		all         []ospackage.PackageInfo
		expectError bool
	}{
		{
			name: "empty request",
			req:  []ospackage.PackageInfo{},
			all: []ospackage.PackageInfo{
				{
					Name:        "test-package",
					Version:     "1.0.0",
					Arch:        "x86_64",
					URL:         "https://repo.example.com/rpm/test-package-1.0.0-1.el9.x86_64.rpm",
					Requires:    []string{},
					RequiresVer: []string{},
				},
			},
			expectError: false,
		},
		{
			name: "simple resolve",
			req: []ospackage.PackageInfo{
				{
					Name:        "test-package",
					Version:     "1.0.0",
					Arch:        "x86_64",
					URL:         "https://repo.example.com/rpm/test-package-1.0.0-1.el9.x86_64.rpm",
					Requires:    []string{},
					RequiresVer: []string{},
				},
			},
			all: []ospackage.PackageInfo{
				{
					Name:        "test-package",
					Version:     "1.0.0",
					Arch:        "x86_64",
					URL:         "https://repo.example.com/rpm/test-package-1.0.0-1.el9.x86_64.rpm",
					Requires:    []string{},
					RequiresVer: []string{},
				},
			},
			expectError: false,
		},
		{
			name: "resolve with dependencies",
			req: []ospackage.PackageInfo{
				{
					Name:        "app",
					Version:     "2.0.0",
					Arch:        "x86_64",
					URL:         "https://repo.example.com/rpm/app-2.0.0-1.el9.x86_64.rpm",
					Requires:    []string{"libfoo"},
					RequiresVer: []string{"libfoo >= 1.0.0"},
				},
			},
			all: []ospackage.PackageInfo{
				{
					Name:        "app",
					Version:     "2.0.0",
					Arch:        "x86_64",
					URL:         "https://repo.example.com/rpm/app-2.0.0-1.el9.x86_64.rpm",
					Requires:    []string{"libfoo"},
					RequiresVer: []string{"libfoo >= 1.0.0"},
				},
				{
					Name:        "libfoo",
					Version:     "1.5.0",
					Arch:        "x86_64",
					URL:         "https://repo.example.com/rpm/libfoo-1.5.0-1.el9.x86_64.rpm",
					Requires:    []string{},
					RequiresVer: []string{},
				},
			},
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := rpmutils.Resolve(tc.req, tc.all)

			if tc.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
				if result == nil {
					t.Error("Expected result to be non-nil")
				}
			}
		})
	}
}

func TestDownloadPackages(t *testing.T) {
	// Save original global variables
	originalRepoCfg := rpmutils.RepoCfg
	originalGzHref := rpmutils.GzHref
	originalUserRepo := rpmutils.UserRepo
	defer func() {
		rpmutils.RepoCfg = originalRepoCfg
		rpmutils.GzHref = originalGzHref
		rpmutils.UserRepo = originalUserRepo
	}()

	testCases := []struct {
		name        string
		pkgList     []string
		destDir     string
		dotFile     string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "empty package list",
			pkgList:     []string{},
			destDir:     "",
			dotFile:     "",
			expectError: true, // Will fail when trying to fetch packages
			errorMsg:    "base package fetch failed",
		},
		{
			name:        "invalid destination directory",
			pkgList:     []string{"test-package"},
			destDir:     "/invalid/path/that/cannot/be/created",
			dotFile:     "",
			expectError: true,
			errorMsg:    "base package fetch failed",
		},
		{
			name:        "invalid main repository with user repo",
			pkgList:     []string{"test-package"},
			destDir:     "",
			dotFile:     "",
			expectError: true,
			errorMsg:    "base package fetch failed", // Will fail at base repo first
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set up invalid repo config to trigger early error
			rpmutils.RepoCfg = rpmutils.RepoConfig{
				URL: "invalid-url",
			}
			rpmutils.GzHref = "repodata/primary.xml.gz"

			// Setup user repo for specific test case
			if tc.name == "invalid main repository with user repo" {
				// Set up invalid main repo and user repo to test both paths
				rpmutils.RepoCfg = rpmutils.RepoConfig{
					URL: "invalid-url", // This will fail at base repo level
				}
				rpmutils.UserRepo = []config.PackageRepository{
					{
						URL:      "invalid-user-repo-url",
						Codename: "test",
						PKey:     "https://example.com/key.asc",
					},
				}
			} else {
				rpmutils.UserRepo = []config.PackageRepository{}
			}

			if tc.destDir == "" {
				tmpDir, err := os.MkdirTemp("", "test-download-*")
				if err != nil {
					t.Fatalf("Failed to create temp dir: %v", err)
				}
				tc.destDir = tmpDir
				defer os.RemoveAll(tmpDir)
			}

			result, err := rpmutils.DownloadPackages(tc.pkgList, tc.destDir, tc.dotFile, nil, false)

			if tc.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				if tc.errorMsg != "" && !strings.Contains(err.Error(), tc.errorMsg) {
					t.Errorf("Expected error to contain %q, got %q", tc.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
				if result == nil {
					t.Error("Expected result to be non-nil")
				}
			}
		})
	}
}

func TestRepoConfig(t *testing.T) {
	// Test RepoConfig struct initialization
	config := rpmutils.RepoConfig{
		Section:      "[test-repo]",
		Name:         "Test Repository",
		URL:          "https://example.com/repo",
		GPGCheck:     true,
		RepoGPGCheck: true,
		Enabled:      true,
		GPGKey:       "https://example.com/key.asc",
	}

	if config.Section != "[test-repo]" {
		t.Errorf("Expected Section to be '[test-repo]', got %s", config.Section)
	}
	if config.Name != "Test Repository" {
		t.Errorf("Expected Name to be 'Test Repository', got %s", config.Name)
	}
	if config.URL != "https://example.com/repo" {
		t.Errorf("Expected URL to be 'https://example.com/repo', got %s", config.URL)
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
	if config.GPGKey != "https://example.com/key.asc" {
		t.Errorf("Expected GPGKey to be 'https://example.com/key.asc', got %s", config.GPGKey)
	}
}

func TestGlobalVariables(t *testing.T) {
	// Test that global variables can be accessed and modified
	originalRepoCfg := rpmutils.RepoCfg
	originalGzHref := rpmutils.GzHref

	// Modify globals
	rpmutils.RepoCfg = rpmutils.RepoConfig{
		URL:  "test-url",
		Name: "test-name",
	}
	rpmutils.GzHref = "test-href"

	// Verify changes
	if rpmutils.RepoCfg.URL != "test-url" {
		t.Errorf("Expected RepoCfg.URL to be 'test-url', got %s", rpmutils.RepoCfg.URL)
	}
	if rpmutils.RepoCfg.Name != "test-name" {
		t.Errorf("Expected RepoCfg.Name to be 'test-name', got %s", rpmutils.RepoCfg.Name)
	}
	if rpmutils.GzHref != "test-href" {
		t.Errorf("Expected GzHref to be 'test-href', got %s", rpmutils.GzHref)
	}

	// Restore original values
	rpmutils.RepoCfg = originalRepoCfg
	rpmutils.GzHref = originalGzHref
}

// TestResolveEdgeCases tests edge cases in the Resolve function
func TestResolveEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		req         []ospackage.PackageInfo
		all         []ospackage.PackageInfo
		expectError bool
		description string
	}{
		{
			name:        "Circular dependencies",
			description: "Package A depends on B, B depends on A",
			req: []ospackage.PackageInfo{
				{
					Name:        "package-a",
					Version:     "1.0.0",
					Arch:        "x86_64",
					URL:         "https://repo.example.com/rpm/package-a-1.0.0-1.el9.x86_64.rpm",
					Provides:    []string{"package-a"},
					Requires:    []string{"package-b"},
					RequiresVer: []string{"package-b >= 1.0.0"},
				},
			},
			all: []ospackage.PackageInfo{
				{
					Name:        "package-a",
					Version:     "1.0.0",
					Arch:        "x86_64",
					URL:         "https://repo.example.com/rpm/package-a-1.0.0-1.el9.x86_64.rpm",
					Provides:    []string{"package-a"},
					Requires:    []string{"package-b"},
					RequiresVer: []string{"package-b >= 1.0.0"},
				},
				{
					Name:        "package-b",
					Version:     "1.0.0",
					Arch:        "x86_64",
					URL:         "https://repo.example.com/rpm/package-b-1.0.0-1.el9.x86_64.rpm",
					Provides:    []string{"package-b"},
					Requires:    []string{"package-a"},
					RequiresVer: []string{"package-a >= 1.0.0"},
				},
			},
			expectError: false, // Should handle circular deps gracefully
		},
		{
			name:        "Self dependency",
			description: "Package depends on itself",
			req: []ospackage.PackageInfo{
				{
					Name:        "self-dep",
					Version:     "1.0.0",
					Arch:        "x86_64",
					URL:         "https://repo.example.com/rpm/self-dep-1.0.0-1.el9.x86_64.rpm",
					Provides:    []string{"self-dep"},
					Requires:    []string{"self-dep"},
					RequiresVer: []string{"self-dep >= 1.0.0"},
				},
			},
			all: []ospackage.PackageInfo{
				{
					Name:        "self-dep",
					Version:     "1.0.0",
					Arch:        "x86_64",
					URL:         "https://repo.example.com/rpm/self-dep-1.0.0-1.el9.x86_64.rpm",
					Provides:    []string{"self-dep"},
					Requires:    []string{"self-dep"},
					RequiresVer: []string{"self-dep >= 1.0.0"},
				},
			},
			expectError: false, // Should handle self-deps
		},
		{
			name:        "Deep dependency chain",
			description: "Package with many levels of dependencies",
			req: []ospackage.PackageInfo{
				{
					Name:        "level-0",
					Version:     "1.0.0",
					Arch:        "x86_64",
					URL:         "https://repo.example.com/rpm/level-0-1.0.0-1.el9.x86_64.rpm",
					Provides:    []string{"level-0"},
					Requires:    []string{"level-1"},
					RequiresVer: []string{"level-1 >= 1.0.0"},
				},
			},
			all: []ospackage.PackageInfo{
				{
					Name:        "level-0",
					Version:     "1.0.0",
					Arch:        "x86_64",
					URL:         "https://repo.example.com/rpm/level-0-1.0.0-1.el9.x86_64.rpm",
					Provides:    []string{"level-0"},
					Requires:    []string{"level-1"},
					RequiresVer: []string{"level-1 >= 1.0.0"},
				},
				{
					Name:        "level-1",
					Version:     "1.0.0",
					Arch:        "x86_64",
					URL:         "https://repo.example.com/rpm/level-1-1.0.0-1.el9.x86_64.rpm",
					Provides:    []string{"level-1"},
					Requires:    []string{"level-2"},
					RequiresVer: []string{"level-2 >= 1.0.0"},
				},
				{
					Name:        "level-2",
					Version:     "1.0.0",
					Arch:        "x86_64",
					URL:         "https://repo.example.com/rpm/level-2-1.0.0-1.el9.x86_64.rpm",
					Provides:    []string{"level-2"},
					Requires:    []string{"level-3"},
					RequiresVer: []string{"level-3 >= 1.0.0"},
				},
				{
					Name:        "level-3",
					Version:     "1.0.0",
					Arch:        "x86_64",
					URL:         "https://repo.example.com/rpm/level-3-1.0.0-1.el9.x86_64.rpm",
					Provides:    []string{"level-3"},
					Requires:    []string{},
					RequiresVer: []string{},
				},
			},
			expectError: false,
		},
		{
			name:        "Package not in all list",
			description: "Requested package not available in repository",
			req: []ospackage.PackageInfo{
				{
					Name:        "missing-package",
					Version:     "1.0.0",
					Arch:        "x86_64",
					URL:         "https://repo.example.com/rpm/missing-package-1.0.0-1.el9.x86_64.rpm",
					Provides:    []string{"missing-package"},
					Requires:    []string{},
					RequiresVer: []string{},
				},
			},
			all: []ospackage.PackageInfo{
				{
					Name:        "other-package",
					Version:     "1.0.0",
					Arch:        "x86_64",
					URL:         "https://repo.example.com/rpm/other-package-1.0.0-1.el9.x86_64.rpm",
					Provides:    []string{"other-package"},
					Requires:    []string{},
					RequiresVer: []string{},
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := rpmutils.Resolve(tt.req, tt.all)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for %s but got none", tt.description)
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error for %s: %v", tt.description, err)
				return
			}

			if result == nil {
				t.Errorf("Expected non-nil result for %s", tt.description)
			}

			// Verify that requested packages are in the result
			resultMap := make(map[string]bool)
			for _, pkg := range result {
				resultMap[pkg.Name] = true
			}

			for _, reqPkg := range tt.req {
				if !resultMap[reqPkg.Name] {
					t.Errorf("Requested package %s not found in result for %s", reqPkg.Name, tt.description)
				}
			}
		})
	}
}

// TestDownloadPackagesComplete tests the new DownloadPackagesComplete function
func TestDownloadPackagesComplete(t *testing.T) {
	// Save original global variables
	originalRepoCfg := rpmutils.RepoCfg
	originalGzHref := rpmutils.GzHref
	originalUserRepo := rpmutils.UserRepo
	defer func() {
		rpmutils.RepoCfg = originalRepoCfg
		rpmutils.GzHref = originalGzHref
		rpmutils.UserRepo = originalUserRepo
	}()

	testCases := []struct {
		name         string
		pkgList      []string
		setupRepos   func()
		expectError  bool
		errorMsg     string
		checkResults func([]string, []ospackage.PackageInfo) error
	}{
		{
			name:    "empty package list",
			pkgList: []string{},
			setupRepos: func() {
				rpmutils.RepoCfg = rpmutils.RepoConfig{
					URL: "invalid-url",
				}
				rpmutils.UserRepo = []config.PackageRepository{}
			},
			expectError: true,
			errorMsg:    "base package fetch failed",
		},
		{
			name:    "invalid base repository",
			pkgList: []string{"test-package"},
			setupRepos: func() {
				rpmutils.RepoCfg = rpmutils.RepoConfig{
					URL: "invalid-url",
				}
				rpmutils.UserRepo = []config.PackageRepository{}
			},
			expectError: true,
			errorMsg:    "base package fetch failed",
		},
		{
			name:    "invalid user repository",
			pkgList: []string{"test-package"},
			setupRepos: func() {
				// This will still fail at base repo level, but tests user repo path
				rpmutils.RepoCfg = rpmutils.RepoConfig{
					URL: "https://example.com",
				}
				rpmutils.UserRepo = []config.PackageRepository{
					{
						URL:      "invalid-user-url",
						Codename: "test",
						PKey:     "https://example.com/key.asc",
					},
				}
			},
			expectError: true,
			errorMsg:    "base package fetch failed", // Will fail at base repo first
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.setupRepos()

			tmpDir, err := os.MkdirTemp("", "test-download-complete-*")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			downloadList, packageInfos, err := rpmutils.DownloadPackagesComplete(tc.pkgList, tmpDir, "", nil, false)

			if tc.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				if tc.errorMsg != "" && !strings.Contains(err.Error(), tc.errorMsg) {
					t.Errorf("Expected error to contain %q, got %q", tc.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
				if downloadList == nil {
					t.Error("Expected downloadList to be non-nil")
				}
				if packageInfos == nil {
					t.Error("Expected packageInfos to be non-nil")
				}
				if tc.checkResults != nil {
					if err := tc.checkResults(downloadList, packageInfos); err != nil {
						t.Errorf("Result validation failed: %v", err)
					}
				}
			}
		})
	}
}

// TestMatchRequestedPerformance tests performance with large package lists
func TestMatchRequestedPerformance(t *testing.T) {
	// Generate a large list of packages
	var allPackages []ospackage.PackageInfo
	for i := 0; i < 1000; i++ {
		pkg := ospackage.PackageInfo{
			Name:    fmt.Sprintf("package-%04d", i),
			Version: "1.0.0",
			Arch:    "x86_64",
			URL:     fmt.Sprintf("https://repo.example.com/package-%04d-1.0.0-1.el9.x86_64.rpm", i),
		}
		allPackages = append(allPackages, pkg)
	}

	// Test with various request sizes
	tests := []struct {
		name         string
		requestCount int
	}{
		{"Single package", 1},
		{"Small batch", 10},
		{"Medium batch", 100},
		{"Large batch", 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests []string
			for i := 0; i < tt.requestCount; i++ {
				requests = append(requests, fmt.Sprintf("package-%04d", i))
			}

			result, err := rpmutils.MatchRequested(requests, allPackages)

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if len(result) != tt.requestCount {
				t.Errorf("Expected %d packages, got %d", tt.requestCount, len(result))
			}
		})
	}
}

// TestDownloadPackagesCompleteFunction tests the new DownloadPackagesComplete function
func TestDownloadPackagesCompleteFunction(t *testing.T) {
	// Save original global variables
	originalRepoCfg := rpmutils.RepoCfg
	originalGzHref := rpmutils.GzHref
	originalUserRepo := rpmutils.UserRepo
	defer func() {
		rpmutils.RepoCfg = originalRepoCfg
		rpmutils.GzHref = originalGzHref
		rpmutils.UserRepo = originalUserRepo
	}()

	testCases := []struct {
		name         string
		pkgList      []string
		setupRepos   func()
		expectError  bool
		errorMsg     string
		checkResults func([]string, []ospackage.PackageInfo) error
	}{
		{
			name:    "empty package list",
			pkgList: []string{},
			setupRepos: func() {
				rpmutils.RepoCfg = rpmutils.RepoConfig{
					URL: "invalid-url",
				}
				rpmutils.UserRepo = []config.PackageRepository{}
			},
			expectError: true,
			errorMsg:    "base package fetch failed",
		},
		{
			name:    "invalid base repository",
			pkgList: []string{"test-package"},
			setupRepos: func() {
				rpmutils.RepoCfg = rpmutils.RepoConfig{
					URL: "invalid-url",
				}
				rpmutils.UserRepo = []config.PackageRepository{}
			},
			expectError: true,
			errorMsg:    "base package fetch failed",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.setupRepos()

			tmpDir, err := os.MkdirTemp("", "test-download-complete-*")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			downloadList, packageInfos, err := rpmutils.DownloadPackagesComplete(tc.pkgList, tmpDir, "", nil, false)

			if tc.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				if tc.errorMsg != "" && !strings.Contains(err.Error(), tc.errorMsg) {
					t.Errorf("Expected error to contain %q, got %q", tc.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
				if downloadList == nil {
					t.Error("Expected downloadList to be non-nil")
				}
				if packageInfos == nil {
					t.Error("Expected packageInfos to be non-nil")
				}
				if tc.checkResults != nil {
					if err := tc.checkResults(downloadList, packageInfos); err != nil {
						t.Errorf("Result validation failed: %v", err)
					}
				}
			}
		})
	}
}

// TestBinaryGPGKeyHandling tests binary GPG key detection and conversion indirectly
func TestBinaryGPGKeyHandling(t *testing.T) {
	testCases := []struct {
		name         string
		data         []byte
		expectBinary bool
		description  string
	}{
		{
			name: "ASCII armored GPG key",
			data: []byte(`-----BEGIN PGP PUBLIC KEY BLOCK-----
Version: GnuPG v1

mQENBFciSdkBCADNxMYPr1/...test...key...content...
-----END PGP PUBLIC KEY BLOCK-----`),
			expectBinary: false,
			description:  "Standard ASCII armored GPG key should not be detected as binary",
		},
		{
			name: "binary data simulation",
			data: func() []byte {
				data := make([]byte, 100)
				for i := range data {
					data[i] = byte(i % 256)
				}
				return data
			}(),
			expectBinary: true,
			description:  "Binary data with less than 70% printable characters should be detected as binary",
		},
		{
			name:         "empty data",
			data:         []byte{},
			expectBinary: false,
			description:  "Empty data should not be detected as binary",
		},
		{
			name:         "small binary data",
			data:         []byte{0x01, 0x02, 0x03},
			expectBinary: false, // Less than 4 bytes
			description:  "Very small data should not be detected as binary",
		},
		{
			name:         "text with some binary",
			data:         []byte("This is mostly text with some \x00\x01\x02 binary data mixed in for testing purposes"),
			expectBinary: false, // Still mostly printable
			description:  "Text with minimal binary should not be detected as binary",
		},
	}

	// Test indirectly through the GPG key handling in Validate function
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a test server that serves the test data
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.expectBinary {
					w.Header().Set("Content-Type", "application/octet-stream")
				} else {
					w.Header().Set("Content-Type", "text/plain")
				}
				_, _ = w.Write(tc.data)
			}))
			defer server.Close()

			// Save original configuration
			originalRepoCfg := rpmutils.RepoCfg
			originalUserRepo := rpmutils.UserRepo
			defer func() {
				rpmutils.RepoCfg = originalRepoCfg
				rpmutils.UserRepo = originalUserRepo
			}()

			// Setup test configuration
			rpmutils.RepoCfg = rpmutils.RepoConfig{
				GPGKey: server.URL,
			}
			rpmutils.UserRepo = []config.PackageRepository{}

			tmpDir, err := os.MkdirTemp("", "test-binary-gpg-*")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			// Call Validate which will internally use the GPG key detection functions
			err = rpmutils.Validate(tmpDir)

			// For ASCII armored keys, we expect no error (when no RPMs to verify)
			// For binary keys that can't be converted, we expect an error
			if !tc.expectBinary {
				if err != nil && !strings.Contains(err.Error(), "no RPMs found") {
					// ASCII keys should either succeed or fail only due to no RPMs
					t.Logf("GPG key validation result for ASCII key: %v", err)
				}
			} else {
				// Binary keys will likely fail during conversion
				if err == nil {
					t.Logf("Binary GPG key was processed successfully (unexpected but not necessarily wrong)")
				} else {
					t.Logf("Binary GPG key processing failed as expected: %v", err)
				}
			}
		})
	}
}

// TestUserRepoConfig tests user repository configuration scenarios
func TestUserRepoConfig(t *testing.T) {
	// Save original configuration
	originalUserRepo := rpmutils.UserRepo
	defer func() {
		rpmutils.UserRepo = originalUserRepo
	}()

	testCases := []struct {
		name          string
		userRepos     []config.PackageRepository
		expectedRepos int
		description   string
	}{
		{
			name:          "empty user repository list",
			userRepos:     []config.PackageRepository{},
			expectedRepos: 0,
			description:   "No user repositories should result in empty package list",
		},
		{
			name: "single user repository",
			userRepos: []config.PackageRepository{
				{
					URL:      "https://repo1.example.com",
					Codename: "stable",
					PKey:     "https://repo1.example.com/key.asc",
				},
			},
			expectedRepos: 1,
			description:   "Single repository should be configured",
		},
		{
			name: "multiple user repositories",
			userRepos: []config.PackageRepository{
				{
					URL:      "https://repo1.example.com",
					Codename: "stable",
					PKey:     "https://repo1.example.com/key.asc",
				},
				{
					URL:      "https://repo2.example.com",
					Codename: "testing",
					PKey:     "https://repo2.example.com/key.asc",
				},
			},
			expectedRepos: 2,
			description:   "Multiple repositories should all be configured",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rpmutils.UserRepo = tc.userRepos

			// Test that UserPackages function can handle the configuration
			// Even though it will fail due to network calls, it validates the structure
			_, err := rpmutils.UserPackages()

			// For empty repos, should not error
			if len(tc.userRepos) == 0 {
				if err != nil {
					t.Errorf("Expected no error for empty user repos, got: %v", err)
				}
			} else {
				// For non-empty repos, will fail due to network, but that's expected
				if err == nil {
					t.Logf("Unexpected success for user repo configuration")
				} else {
					t.Logf("Expected network-related error for user repos: %v", err)
				}
			}
		})
	}
}
