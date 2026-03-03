package rpmutils

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/open-edge-platform/os-image-composer/internal/config"
	"github.com/open-edge-platform/os-image-composer/internal/ospackage"
	"github.com/open-edge-platform/os-image-composer/internal/ospackage/resolvertest"
)

func TestRPMResolver(t *testing.T) {
	resolvertest.RunResolverTestsFunc(
		t,
		"rpmutils",
		ResolveDependencies, // directly passing your function
	)
}

func TestExtractBaseRequirement(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple requirement",
			input:    "bash",
			expected: "bash",
		},
		{
			name:     "requirement with version",
			input:    "glibc >= 2.30",
			expected: "glibc",
		},
		{
			name:     "complex requirement with parentheses",
			input:    "(libssl.so.1.1 >= 1.1.0)",
			expected: "libssl.so.1.1",
		},
		{
			name:     "requirement with 64bit suffix",
			input:    "libpthread.so.0()(64bit)",
			expected: "libpthread.so.0",
		},
		{
			name:     "complex requirement with multiple conditions",
			input:    "(gcc-c++ and make)",
			expected: "gcc-c++",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "whitespace only",
			input:    "   ",
			expected: "",
		},
		{
			name:     "requirement with complex versioning",
			input:    "python3-devel >= 3.8.0",
			expected: "python3-devel",
		},
		{
			name:     "parentheses with spaces",
			input:    "( openssl-libs )",
			expected: "openssl-libs",
		},
		{
			name:     "file path requirement",
			input:    "/bin/sh",
			expected: "/bin/sh",
		},
		{
			name:     "complex conditional dependency",
			input:    "((kernel-modules-extra-uname-r = 6.12.0-174.el10.x86_64) if kernel-modules-extra-matched)",
			expected: "kernel-modules-extra-uname-r",
		},
		{
			name:     "simple parentheses without spaces",
			input:    "(linux-firmware)",
			expected: "linux-firmware",
		},
		{
			name:     "simple parentheses with version constraint",
			input:    "(glibc >= 2.17)",
			expected: "glibc",
		},
		{
			name:     "complex conditional dependency with >= operator",
			input:    "((linux-firmware >= 20150904-56.git6ebf5d57) if linux-firmware)",
			expected: "linux-firmware",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractBaseRequirement(tt.input)
			if result != tt.expected {
				t.Errorf("extractBaseRequirement(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestGenerateDot(t *testing.T) {
	// Create temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "dot_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name        string
		packages    []ospackage.PackageInfo
		filename    string
		pkgSources  map[string]config.PackageSource
		expectError bool
	}{
		{
			name: "simple package with dependencies",
			packages: []ospackage.PackageInfo{
				{
					Name:     "bash",
					Requires: []string{"glibc", "ncurses"},
				},
				{
					Name:     "glibc",
					Requires: []string{},
				},
				{
					Name:     "ncurses",
					Requires: []string{"glibc"},
				},
			},
			filename:    filepath.Join(tmpDir, "simple.dot"),
			expectError: false,
		},
		{
			name:        "empty package list",
			packages:    []ospackage.PackageInfo{},
			filename:    filepath.Join(tmpDir, "empty.dot"),
			expectError: false,
		},
		{
			name: "package with no dependencies",
			packages: []ospackage.PackageInfo{
				{
					Name:     "standalone",
					Requires: []string{},
				},
			},
			filename:    filepath.Join(tmpDir, "standalone.dot"),
			expectError: false,
		},
		{
			name: "packages with special characters in names",
			packages: []ospackage.PackageInfo{
				{
					Name:     "package-with-dashes",
					Requires: []string{"lib.so.1"},
				},
				{
					Name:     "lib.so.1",
					Requires: []string{},
				},
			},
			filename:    filepath.Join(tmpDir, "special_chars.dot"),
			expectError: false,
		},
		{
			name: "with package source colors",
			packages: []ospackage.PackageInfo{
				{Name: "kernel", Requires: []string{}},
				{Name: "boot", Requires: []string{}},
			},
			filename: filepath.Join(tmpDir, "sources.dot"),
			pkgSources: map[string]config.PackageSource{
				"kernel": config.PackageSourceKernel,
				"boot":   config.PackageSourceBootloader,
			},
			expectError: false,
		},
		{
			name: "duplicate dependencies should be deduplicated",
			packages: []ospackage.PackageInfo{
				{
					Name:     "libstdc++",
					Requires: []string{"glibc", "glibc", "glibc", "libgcc", "libgcc"},
				},
				{
					Name:     "glibc",
					Requires: []string{},
				},
				{
					Name:     "libgcc",
					Requires: []string{"glibc"},
				},
			},
			filename:    filepath.Join(tmpDir, "dedup.dot"),
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := GenerateDot(tt.packages, tt.filename, tt.pkgSources)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			// Verify the file was created and has expected content
			content, err := os.ReadFile(tt.filename)
			if err != nil {
				t.Fatalf("Failed to read generated file: %v", err)
			}

			contentStr := string(content)

			// Check basic DOT structure
			if !strings.Contains(contentStr, "digraph G {") {
				t.Error("DOT file should start with 'digraph G {'")
			}
			if !strings.Contains(contentStr, "rankdir=LR;") {
				t.Error("DOT file should contain 'rankdir=LR;'")
			}
			if !strings.Contains(contentStr, "}") {
				t.Error("DOT file should end with '}'")
			}

			// Check that all packages are represented
			for _, pkg := range tt.packages {
				nodeDef := fmt.Sprintf("\"%s\";", pkg.Name)
				if !strings.Contains(contentStr, nodeDef) {
					t.Errorf("DOT file should contain node definition for %s", pkg.Name)
				}

				// Check dependencies - each unique edge should appear exactly once
				seenEdges := make(map[string]bool)
				for _, dep := range pkg.Requires {
					expectedEdge := fmt.Sprintf("\"%s\" -> \"%s\";", pkg.Name, dep)
					if !strings.Contains(contentStr, expectedEdge) {
						t.Errorf("DOT file should contain edge: %s", expectedEdge)
					}
					seenEdges[expectedEdge] = true
				}

				// For duplicate dependency test, verify each unique edge appears only once
				if tt.name == "duplicate dependencies should be deduplicated" {
					for edge := range seenEdges {
						count := strings.Count(contentStr, edge)
						if count != 1 {
							t.Errorf("Edge %s should appear exactly once, but appears %d times", edge, count)
						}
					}
				}
			}
		})
	}
}

func TestParsePrimary(t *testing.T) {
	tests := []struct {
		name          string
		xmlContent    string
		filename      string
		expectedError bool
		expectedCount int
		expectedNames []string
	}{
		{
			name:          "simple gzipped XML",
			xmlContent:    `<?xml version="1.0" encoding="UTF-8"?><metadata xmlns="http://linux.duke.edu/metadata/common" xmlns:rpm="http://linux.duke.edu/metadata/rpm" packages="2"><package type="rpm"><name>bash</name><arch>x86_64</arch><location href="bash-5.1-8.el9.x86_64.rpm"/><format><rpm:license>GPLv3+</rpm:license><rpm:vendor>Red Hat, Inc.</rpm:vendor><rpm:provides><rpm:entry name="bash"/></rpm:provides><rpm:requires><rpm:entry name="glibc"/></rpm:requires></format></package><package type="rpm"><name>glibc</name><arch>x86_64</arch><location href="glibc-2.32-1.el9.x86_64.rpm"/><format><rpm:license>LGPLv2+</rpm:license><rpm:vendor>Red Hat, Inc.</rpm:vendor><rpm:provides><rpm:entry name="glibc"/></rpm:provides></format></package></metadata>`,
			filename:      "primary.xml.gz",
			expectedError: false,
			expectedCount: 2,
			expectedNames: []string{"bash-5.1-8.el9.x86_64.rpm", "glibc-2.32-1.el9.x86_64.rpm"},
		},
		{
			name:          "empty metadata",
			xmlContent:    `<?xml version="1.0" encoding="UTF-8"?><metadata xmlns="http://linux.duke.edu/metadata/common" packages="0"></metadata>`,
			filename:      "empty.xml.gz",
			expectedError: false,
			expectedCount: 0,
			expectedNames: []string{},
		},
		{
			name:          "invalid compression",
			xmlContent:    "dummy content",
			filename:      "primary.xml.bz2",
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test server that serves the XML content
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Set appropriate content type and serve compressed content
				if strings.HasSuffix(tt.filename, ".gz") {
					w.Header().Set("Content-Type", "application/gzip")
					// Compress the content properly
					content := compressGzip(t, tt.xmlContent)
					_, _ = w.Write(content)
				} else {
					w.Header().Set("Content-Type", "text/xml")
					_, _ = w.Write([]byte(tt.xmlContent))
				}
			}))
			defer server.Close()

			// Test ParseRepositoryMetadata
			packages, err := ParseRepositoryMetadata(server.URL+"/", tt.filename, nil)

			if tt.expectedError {
				if err == nil {
					t.Error("Expected error, but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if len(packages) != tt.expectedCount {
				t.Errorf("Expected %d packages, got %d", tt.expectedCount, len(packages))
			}

			// Check that expected packages are present
			foundNames := make(map[string]bool)
			for _, pkg := range packages {
				foundNames[pkg.Name] = true

				// Verify the package has the expected fields
				if pkg.Type != "rpm" {
					t.Errorf("Package %s should have type 'rpm', got %s", pkg.Name, pkg.Type)
				}
				if pkg.URL == "" {
					t.Errorf("Package %s should have URL set", pkg.Name)
				}
			}

			for _, expectedName := range tt.expectedNames {
				if !foundNames[expectedName] {
					t.Errorf("Expected package %s not found", expectedName)
				}
			}

			// Additional checks for the first test case
			if tt.name == "simple gzipped XML" && len(packages) >= 2 {
				// Check bash package details
				var bashPkg *ospackage.PackageInfo
				for _, pkg := range packages {
					if pkg.Name == "bash-5.1-8.el9.x86_64.rpm" {
						bashPkg = &pkg
						break
					}
				}
				if bashPkg == nil {
					t.Fatal("bash-5.1-8.el9.x86_64.rpm package not found")
				}

				if bashPkg.License != "GPLv3+" {
					t.Errorf("bash license should be 'GPLv3+', got %s", bashPkg.License)
				}
				if bashPkg.Origin != "Red Hat, Inc." {
					t.Errorf("bash origin should be 'Red Hat, Inc.', got %s", bashPkg.Origin)
				}
			}
		})
	}
}

func TestMatchesPackageFilter(t *testing.T) {
	tests := []struct {
		name    string
		pkgName string
		filter  []string
		want    bool
	}{
		{
			name:    "empty filter allows all",
			pkgName: "any-package",
			filter:  nil,
			want:    true,
		},
		{
			name:    "exact match",
			pkgName: "qemu-common",
			filter:  []string{"qemu-common"},
			want:    true,
		},
		{
			name:    "prefix version match",
			pkgName: "kernel-drivers-gpu-6.17.11-1.emt3.x86_64",
			filter:  []string{"kernel-drivers-gpu-6.17.11"},
			want:    true,
		},
		{
			name:    "glob wildcard wayland",
			pkgName: "wayland-protocols-devel",
			filter:  []string{"wayland*"},
			want:    true,
		},
		{
			name:    "glob wildcard libva",
			pkgName: "libva-intel-media-driver",
			filter:  []string{"libva*"},
			want:    true,
		},
		{
			name:    "glob wildcard no match",
			pkgName: "mesa-libEGL",
			filter:  []string{"wayland*", "libva*"},
			want:    false,
		},
		{
			name:    "invalid glob does not match and does not fail",
			pkgName: "wayland",
			filter:  []string{"wayland["},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesPackageFilter(tt.pkgName, tt.filter)
			if got != tt.want {
				t.Errorf("matchesPackageFilter(%q, %v) = %v, want %v", tt.pkgName, tt.filter, got, tt.want)
			}
		})
	}
}

// Helper function to compress content with gzip
func compressGzip(t *testing.T, content string) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	_, err := writer.Write([]byte(content))
	if err != nil {
		t.Fatal(err)
	}
	err = writer.Close()
	if err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

// TestMatchRequestedAdvanced tests advanced scenarios for MatchRequested function
func TestMatchRequestedAdvanced(t *testing.T) {
	testPackages := []ospackage.PackageInfo{
		{
			Name:    "curl",
			Version: "8.8.0-2.azl3",
			Arch:    "x86_64",
			URL:     "https://repo.example.com/curl-8.8.0-2.azl3.x86_64.rpm",
		},
		{
			Name:    "curl-devel",
			Version: "8.8.0-2.azl3",
			Arch:    "x86_64",
			URL:     "https://repo.example.com/curl-devel-8.8.0-2.azl3.x86_64.rpm",
		},
		{
			Name:    "curl",
			Version: "7.8.0-1.azl3",
			Arch:    "x86_64",
			URL:     "https://repo.example.com/curl-7.8.0-1.azl3.x86_64.rpm",
		},
		{
			Name:    "libcurl",
			Version: "8.8.0-2.azl3",
			Arch:    "x86_64",
			URL:     "https://repo.example.com/libcurl-8.8.0-2.azl3.x86_64.rpm",
		},
		{
			Name:    "python3-curl",
			Version: "1.0-1.azl3",
			Arch:    "noarch",
			URL:     "https://repo.example.com/python3-curl-1.0-1.azl3.noarch.rpm",
		},
		{
			Name:    "package-with-src",
			Version: "1.0-1",
			Arch:    "src",
			URL:     "https://repo.example.com/package-with-src-1.0-1.src.rpm",
		},
		{
			Name:    "wayland",
			Version: "1.20.0-1.azl3",
			Arch:    "x86_64",
			URL:     "https://repo.example.com/wayland-1.20.0-1.azl3.x86_64.rpm",
		},
		{
			Name:    "wayland-devel",
			Version: "1.20.0-1.azl3",
			Arch:    "x86_64",
			URL:     "https://repo.example.com/wayland-devel-1.20.0-1.azl3.x86_64.rpm",
		},
	}

	tests := []struct {
		name          string
		requests      []string
		expectError   bool
		expectedCount int
		expectedNames []string
		expectedArch  string
	}{
		{
			name:          "Multiple package requests",
			requests:      []string{"curl", "libcurl"},
			expectError:   false,
			expectedCount: 2,
			expectedNames: []string{"curl", "libcurl"},
		},
		{
			name:          "Request with devel package",
			requests:      []string{"curl-devel"},
			expectError:   false,
			expectedCount: 1,
			expectedNames: []string{"curl-devel"},
		},
		{
			name:          "Request latest version (should pick 8.8.0)",
			requests:      []string{"curl"},
			expectError:   false,
			expectedCount: 1,
			expectedNames: []string{"curl"},
		},
		{
			name:          "Request nonexistent package",
			requests:      []string{"nonexistent-package"},
			expectError:   true,
			expectedCount: 0,
		},
		{
			name:          "Request package that exists only as src",
			requests:      []string{"package-with-src"},
			expectError:   false,
			expectedCount: 1,
			expectedNames: []string{"package-with-src"},
			expectedArch:  "src", // Should still find src packages
		},
		{
			name:          "Mixed existing and nonexistent",
			requests:      []string{"curl", "nonexistent"},
			expectError:   true,
			expectedCount: 0,
		},
		{
			name:          "Wildcard request expands to multiple packages",
			requests:      []string{"wayland*"},
			expectError:   false,
			expectedCount: 2,
			expectedNames: []string{"wayland", "wayland-devel"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := MatchRequested(tt.requests, testPackages)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if len(result) != tt.expectedCount {
				t.Errorf("Expected %d packages, got %d", tt.expectedCount, len(result))
				return
			}

			for i, expectedName := range tt.expectedNames {
				if i < len(result) {
					if !strings.Contains(result[i].Name, expectedName) {
						t.Errorf("Expected package name to contain %q, got %q", expectedName, result[i].Name)
					}
				}
			}

			if tt.expectedArch != "" && len(result) > 0 {
				if result[0].Arch != tt.expectedArch {
					t.Errorf("Expected arch %q, got %q", tt.expectedArch, result[0].Arch)
				}
			}
		})
	}
}

// TestGetRepoMetaDataURL tests URL construction for repository metadata
func TestGetRepoMetaDataURL(t *testing.T) {
	tests := []struct {
		name            string
		baseURL         string
		repoMetaXmlPath string
		expected        string
	}{
		{
			name:            "Standard repository URL",
			baseURL:         "https://repo.example.com/rpm/",
			repoMetaXmlPath: "repodata/repomd.xml",
			expected:        "https://repo.example.com/rpm/repodata/repomd.xml",
		},
		{
			name:            "Base URL without trailing slash",
			baseURL:         "https://repo.example.com/rpm",
			repoMetaXmlPath: "repodata/repomd.xml",
			expected:        "https://repo.example.com/rpm/repodata/repomd.xml",
		},
		{
			name:            "Empty base URL",
			baseURL:         "",
			repoMetaXmlPath: "repodata/repomd.xml",
			expected:        "", // Function returns empty string for non-http URLs
		},
		{
			name:            "Path with leading slash",
			baseURL:         "https://repo.example.com/rpm/",
			repoMetaXmlPath: "/repodata/repomd.xml",
			expected:        "https://repo.example.com/rpm//repodata/repomd.xml", // Function creates double slash
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetRepoMetaDataURL(tt.baseURL, tt.repoMetaXmlPath)
			if result != tt.expected {
				t.Errorf("GetRepoMetaDataURL(%q, %q) = %q, want %q",
					tt.baseURL, tt.repoMetaXmlPath, result, tt.expected)
			}
		})
	}
}

func TestParseRepositoryMetadata_RetryTransientFailure(t *testing.T) {
	var requestCount int32
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?><metadata xmlns="http://linux.duke.edu/metadata/common" xmlns:rpm="http://linux.duke.edu/metadata/rpm" packages="1"><package type="rpm"><name>bash</name><arch>x86_64</arch><location href="bash-5.1-8.el9.x86_64.rpm"/></package></metadata>`
	compressed := compressGzip(t, xmlContent)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&requestCount, 1)
		if current <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(compressed)
	}))
	defer server.Close()

	pkgs, err := ParseRepositoryMetadata(server.URL+"/", "primary.xml.gz", nil)
	if err != nil {
		t.Fatalf("expected retry to succeed, got error: %v", err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 package, got %d", len(pkgs))
	}
	if atomic.LoadInt32(&requestCount) != 3 {
		t.Fatalf("expected 3 requests, got %d", atomic.LoadInt32(&requestCount))
	}
}

func TestFetchPrimaryURL_NoRetryOnPermanentFailure(t *testing.T) {
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, err := FetchPrimaryURL(server.URL + "/repodata/repomd.xml")
	if err == nil {
		t.Fatalf("expected error for permanent 404 response")
	}
	if atomic.LoadInt32(&requestCount) != 1 {
		t.Fatalf("expected 1 request for permanent error, got %d", atomic.LoadInt32(&requestCount))
	}
}
