package debutils

import (
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// newMetadataFixture builds a repo metadata dir holding a Packages.gz naming
// "live-package", a Release recording its real SHA256, and a parse cache naming
// "cached-package". cacheChecksum decides whether the cache looks current: pass
// "" for the Packages.gz checksum the Release advertises (fresh), or any other
// value to simulate an index the repository has since replaced (stale).
//
// The metadata URLs the callers pass are not real URLs, so the refresh attempted
// on every run fails and the code falls back to these on-disk files — which is
// the offline path being exercised.
func newMetadataFixture(t *testing.T, cacheChecksum string) string {
	t.Helper()

	buildPath := filepath.Join(t.TempDir(), "repo_main")
	if err := os.MkdirAll(buildPath, 0755); err != nil {
		t.Fatalf("failed to create build path: %v", err)
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

	if cacheChecksum == "" {
		cacheChecksum = checksum
	}
	cachePkgs := []ospackage.PackageInfo{{Name: "cached-package", Version: "9.9.9", Type: "deb"}}
	if err := saveParsedPackageCache(filepath.Join(buildPath, "packages.parsed.json"), cacheChecksum, cachePkgs); err != nil {
		t.Fatalf("failed to write parsed package cache: %v", err)
	}

	releaseContent := fmt.Sprintf("SHA256:\n %s 1 main/binary-amd64/Packages.gz\n", checksum)
	if err := os.WriteFile(filepath.Join(buildPath, "Release"), []byte(releaseContent), 0644); err != nil {
		t.Fatalf("failed to write Release file: %v", err)
	}

	return buildPath
}

// parseFixtureMetadata runs ParseRepositoryMetadata over a fixture dir with the
// trusted-repo shape (no signature or key files to stage).
func parseFixtureMetadata(t *testing.T, baseURL, buildPath string) []ospackage.PackageInfo {
	t.Helper()
	pkgs, err := ParseRepositoryMetadata(
		baseURL,
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
	return pkgs
}

func TestParseRepositoryMetadata_ParsedCacheBypassForLoopback(t *testing.T) {
	tests := []struct {
		name        string
		baseURL     string
		expectedPkg string
	}{
		// A cache whose checksum matches the Release is served without re-parsing.
		{name: "non-loopback uses parsed cache", baseURL: "http://example.com:123", expectedPkg: "cached-package"},
		{name: "localhost bypasses parsed cache", baseURL: "http://localhost:123", expectedPkg: "live-package"},
		{name: "127001 bypasses parsed cache", baseURL: "http://127.0.0.1:123", expectedPkg: "live-package"},
		{name: "ipv6 loopback bypasses parsed cache", baseURL: "http://[::1]:123", expectedPkg: "live-package"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buildPath := newMetadataFixture(t, "")
			pkgs := parseFixtureMetadata(t, tt.baseURL, buildPath)
			if pkgs[0].Name != tt.expectedPkg {
				t.Errorf("first package name = %q, want %q", pkgs[0].Name, tt.expectedPkg)
			}
		})
	}
}

// A parse cache keyed to an index the repository has replaced must be discarded,
// not served. Serving it pins every later build to package versions that may
// already be deleted from the pool, which surfaces only as a 404 on a single
// .deb — the failure this whole validation path exists to prevent.
func TestParseRepositoryMetadata_StaleParsedCacheIsRejected(t *testing.T) {
	buildPath := newMetadataFixture(t, "checksum-from-a-superseded-index")

	pkgs := parseFixtureMetadata(t, "http://example.com:123", buildPath)

	if pkgs[0].Name != "live-package" {
		t.Errorf("first package name = %q, want %q (stale cache must be re-parsed, not reused)",
			pkgs[0].Name, "live-package")
	}
}

// Having re-parsed a stale cache, the new result must be written back keyed to
// the current checksum — otherwise every subsequent build repeats the download
// and parse, and the cache never converges.
func TestParseRepositoryMetadata_RewritesStaleParsedCache(t *testing.T) {
	buildPath := newMetadataFixture(t, "checksum-from-a-superseded-index")
	cacheFile := filepath.Join(buildPath, "packages.parsed.json")

	parseFixtureMetadata(t, "http://example.com:123", buildPath)

	updated, err := loadParsedPackageCache(cacheFile)
	if err != nil {
		t.Fatalf("loading rewritten cache: %v", err)
	}
	wantChecksum, err := computeFileSHA256(filepath.Join(buildPath, "Packages.gz"))
	if err != nil {
		t.Fatalf("computing Packages.gz checksum: %v", err)
	}
	if !strings.EqualFold(updated.Checksum, wantChecksum) {
		t.Errorf("rewritten cache checksum = %q, want %q", updated.Checksum, wantChecksum)
	}
	if len(updated.Packages) == 0 || updated.Packages[0].Name != "live-package" {
		t.Errorf("rewritten cache packages = %+v, want the freshly parsed live-package", updated.Packages)
	}

	// And the rewritten cache is now considered current: a second run serves it.
	pkgs := parseFixtureMetadata(t, "http://example.com:123", buildPath)
	if pkgs[0].Name != "live-package" {
		t.Errorf("second run first package = %q, want %q", pkgs[0].Name, "live-package")
	}
}

// A failed refresh must leave the existing metadata usable: an offline build has
// nothing else to fall back to, so a partially-written Release would turn a
// working offline build into a verification failure.
func TestRefreshRepoMetadata_FailureLeavesExistingFilesIntact(t *testing.T) {
	dir := t.TempDir()
	release := filepath.Join(dir, "Release")
	const original = "SHA256:\n deadbeef 1 main/binary-amd64/Packages.gz\n"
	if err := os.WriteFile(release, []byte(original), 0644); err != nil {
		t.Fatalf("writing Release: %v", err)
	}

	// "Release" is not a URL, so the fetch fails on every attempt.
	refreshed, err := refreshRepoMetadata(dir, []string{release}, []string{"Release"})
	if err == nil {
		t.Fatal("refreshRepoMetadata succeeded, want an error for an unfetchable URL")
	}
	if refreshed {
		t.Error("refreshRepoMetadata reported files replaced despite failing")
	}

	got, readErr := os.ReadFile(release)
	if readErr != nil {
		t.Fatalf("reading Release after failed refresh: %v", readErr)
	}
	if string(got) != original {
		t.Errorf("Release was modified by a failed refresh:\n got %q\nwant %q", got, original)
	}

	// The staging directory must not be left behind either.
	entries, dirErr := os.ReadDir(dir)
	if dirErr != nil {
		t.Fatalf("reading metadata dir: %v", dirErr)
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), ".meta-refresh-") {
			t.Errorf("staging directory %s was left behind", e.Name())
		}
	}
}
