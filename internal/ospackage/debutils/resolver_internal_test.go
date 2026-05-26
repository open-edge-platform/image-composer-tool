package debutils

import (
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
)

func TestIsGlobPattern(t *testing.T) {
	tests := []struct {
		pattern  string
		expected bool
	}{
		{"*.deb", true},
		{"package-?", true},
		{"[abc]pkg", true},
		{"pkg]", true},
		{"normal-package", false},
		{"package-1.0.0", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			result := isGlobPattern(tt.pattern)
			if result != tt.expected {
				t.Errorf("isGlobPattern(%q) = %v, want %v", tt.pattern, result, tt.expected)
			}
		})
	}
}

func TestMatchesPackageFilter(t *testing.T) {
	tests := []struct {
		name     string
		pkgName  string
		filter   []string
		expected bool
	}{
		{
			name:     "empty filter allows all packages",
			pkgName:  "curl",
			filter:   []string{},
			expected: true,
		},
		{
			name:     "exact match",
			pkgName:  "curl",
			filter:   []string{"curl"},
			expected: true,
		},
		{
			name:     "prefix with dash match",
			pkgName:  "curl-dev",
			filter:   []string{"curl"},
			expected: true,
		},
		{
			name:     "glob wildcard match",
			pkgName:  "libssl1.1",
			filter:   []string{"libssl*"},
			expected: true,
		},
		{
			name:     "no match returns false",
			pkgName:  "wget",
			filter:   []string{"curl", "git"},
			expected: false,
		},
		{
			name:     "multiple filters - first matches",
			pkgName:  "curl",
			filter:   []string{"curl", "wget"},
			expected: true,
		},
		{
			name:     "multiple filters - second matches",
			pkgName:  "wget",
			filter:   []string{"curl", "wget"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesPackageFilter(tt.pkgName, tt.filter)
			if result != tt.expected {
				t.Errorf("matchesPackageFilter(%q, %v) = %v, want %v", tt.pkgName, tt.filter, result, tt.expected)
			}
		})
	}
}

func TestGetFullUrl(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		baseUrl  string
		expected string
	}{
		{
			name:     "already a full HTTP URL is returned as-is",
			filePath: "http://example.com/pool/main/curl.deb",
			baseUrl:  "http://other.com",
			expected: "http://example.com/pool/main/curl.deb",
		},
		{
			name:     "already a full HTTPS URL is returned as-is",
			filePath: "https://example.com/pool/main/curl.deb",
			baseUrl:  "https://other.com",
			expected: "https://example.com/pool/main/curl.deb",
		},
		{
			name:     "relative path is joined with base URL",
			filePath: "pool/main/curl.deb",
			baseUrl:  "http://example.com",
			expected: "http://example.com/pool/main/curl.deb",
		},
		{
			name:     "base URL trailing slash is trimmed before joining",
			filePath: "pool/main/curl.deb",
			baseUrl:  "http://example.com/",
			expected: "http://example.com/pool/main/curl.deb",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := getFullUrl(tt.filePath, tt.baseUrl)
			if err != nil {
				t.Fatalf("getFullUrl() returned unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("getFullUrl(%q, %q) = %q, want %q", tt.filePath, tt.baseUrl, result, tt.expected)
			}
		})
	}
}

func TestShouldBypassParsedPackageCache(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		expected bool
	}{
		{name: "localhost", baseURL: "http://localhost:123", expected: true},
		{name: "localhost uppercase", baseURL: "http://LOCALHOST:123", expected: true},
		{name: "ipv4 loopback", baseURL: "http://127.0.0.1:123", expected: true},
		{name: "ipv6 loopback", baseURL: "http://[::1]:123", expected: true},
		{name: "non-loopback", baseURL: "http://example.com:123", expected: false},
		{name: "invalid url", baseURL: "://bad", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldBypassParsedPackageCache(tt.baseURL)
			if got != tt.expected {
				t.Errorf("shouldBypassParsedPackageCache(%q) = %v, want %v", tt.baseURL, got, tt.expected)
			}
		})
	}
}

func TestParseRepositoryMetadata_ParsedCacheBypassForLoopback(t *testing.T) {
	makeFixture := func(t *testing.T) string {
		t.Helper()

		buildPath := filepath.Join(t.TempDir(), "repo_main")
		if err := os.MkdirAll(buildPath, 0755); err != nil {
			t.Fatalf("failed to create build path: %v", err)
		}

		cachePkgs := []ospackage.PackageInfo{{Name: "cached-package", Version: "9.9.9", Type: "deb"}}
		if err := saveParsedPackageCache(filepath.Join(buildPath, "packages.parsed.json"), "cached-checksum", cachePkgs); err != nil {
			t.Fatalf("failed to write parsed package cache: %v", err)
		}

		pkggzPath := filepath.Join(buildPath, "Packages.gz")
		pkgFile, err := os.Create(pkggzPath)
		if err != nil {
			t.Fatalf("failed to create Packages.gz: %v", err)
		}

		gzWriter := gzip.NewWriter(pkgFile)
		packagesContent := "Package: live-package\nVersion: 1.2.3\nArchitecture: amd64\nFilename: pool/main/l/live-package/live-package_1.2.3_amd64.deb\n\n"
		if _, err := gzWriter.Write([]byte(packagesContent)); err != nil {
			_ = pkgFile.Close()
			t.Fatalf("failed to write gzip content: %v", err)
		}
		if err := gzWriter.Close(); err != nil {
			_ = pkgFile.Close()
			t.Fatalf("failed to close gzip writer: %v", err)
		}
		if err := pkgFile.Close(); err != nil {
			t.Fatalf("failed to close Packages.gz file: %v", err)
		}

		checksum, err := computeFileSHA256(pkggzPath)
		if err != nil {
			t.Fatalf("failed to compute Packages.gz checksum: %v", err)
		}

		releaseContent := fmt.Sprintf("SHA256:\n %s 1 main/binary-amd64/Packages.gz\n", checksum)
		if err := os.WriteFile(filepath.Join(buildPath, "Release"), []byte(releaseContent), 0644); err != nil {
			t.Fatalf("failed to write Release file: %v", err)
		}

		return buildPath
	}

	tests := []struct {
		name        string
		baseURL     string
		expectedPkg string
	}{
		{name: "non-loopback uses parsed cache", baseURL: "http://example.com:123", expectedPkg: "cached-package"},
		{name: "localhost bypasses parsed cache", baseURL: "http://localhost:123", expectedPkg: "live-package"},
		{name: "127001 bypasses parsed cache", baseURL: "http://127.0.0.1:123", expectedPkg: "live-package"},
		{name: "ipv6 loopback bypasses parsed cache", baseURL: "http://[::1]:123", expectedPkg: "live-package"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buildPath := makeFixture(t)

			pkgs, err := ParseRepositoryMetadata(
				tt.baseURL,
				"Packages.gz",
				"Release",
				"Release.gpg",
				"[trusted=yes]",
				buildPath,
				"amd64",
				nil,
			)
			if err != nil {
				t.Fatalf("ParseRepositoryMetadata returned error: %v", err)
			}
			if len(pkgs) == 0 {
				t.Fatalf("expected at least one package, got none")
			}
			if pkgs[0].Name != tt.expectedPkg {
				t.Errorf("first package name = %q, want %q", pkgs[0].Name, tt.expectedPkg)
			}
		})
	}
}
