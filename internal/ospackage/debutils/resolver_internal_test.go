package debutils

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/clearsign"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/runctx"
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
			name:     "literal percent in a Filename is escaped like apt does",
			filePath: "pool/unstable/e/edge-gfx-dkms/edge-gfx-dkms_7.0-260928T031409Z%2B1_all.deb",
			baseUrl:  "http://example.com/repo",
			expected: "http://example.com/repo/pool/unstable/e/edge-gfx-dkms/edge-gfx-dkms_7.0-260928T031409Z%252B1_all.deb",
		},
		{
			name:     "plus sign in a Filename is left unchanged",
			filePath: "pool/universe/o/onednn/libdnnl3.6_3.9.1+ds-2_amd64.deb",
			baseUrl:  "http://example.com",
			expected: "http://example.com/pool/universe/o/onednn/libdnnl3.6_3.9.1+ds-2_amd64.deb",
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
	if err := saveParsedPackageCache(
		filepath.Join(buildPath, "packages.parsed.json"), cacheChecksum, nil, cachePkgs,
	); err != nil {
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

// TestParseRepositoryMetadata_CacheIsKeyedByPackageFilter guards the parse
// cache against being reused under a different allowPackages filter. The cache
// stores the already-filtered package list, so a hit written under one filter
// must not answer a request made with another, even though the repository's
// Packages checksum is unchanged.
func TestParseRepositoryMetadata_CacheIsKeyedByPackageFilter(t *testing.T) {
	parse := func(t *testing.T, buildPath string, filter []string) []ospackage.PackageInfo {
		t.Helper()
		pkgs, err := ParseRepositoryMetadata("http://example.invalid:1/", "Packages.gz", "Release",
			"Release.gpg", "[trusted=yes]", buildPath, "amd64", filter)
		if err != nil {
			t.Fatalf("ParseRepositoryMetadata returned error: %v", err)
		}
		return pkgs
	}

	t.Run("same filter reuses the cache", func(t *testing.T) {
		buildPath := newMetadataFixture(t, "")
		pkgs := parse(t, buildPath, nil)
		if len(pkgs) != 1 || pkgs[0].Name != "cached-package" {
			t.Fatalf("expected the cached package for an unchanged filter, got %+v", pkgs)
		}
	})

	t.Run("different filter ignores the cache", func(t *testing.T) {
		buildPath := newMetadataFixture(t, "")
		pkgs := parse(t, buildPath, []string{"live-package"})
		if len(pkgs) != 1 || pkgs[0].Name != "live-package" {
			t.Fatalf("expected a fresh parse for a changed filter, got %+v", pkgs)
		}
	})

	t.Run("the fresh parse is cached under its own filter", func(t *testing.T) {
		buildPath := newMetadataFixture(t, "")
		_ = parse(t, buildPath, []string{"live-package"})
		cache, err := loadParsedPackageCache(filepath.Join(buildPath, "packages.parsed.json"))
		if err != nil {
			t.Fatalf("failed to reload parse cache: %v", err)
		}
		if !sameFilter(cache.Filter, []string{"live-package"}) {
			t.Fatalf("cache recorded filter %v, want [live-package]", cache.Filter)
		}
	})
}

// TestFilenameWithLiteralPercentRoundTrip ties getFullUrl and debFileName
// together: the URL requested for a Filename with a literal "%" must map back
// to that same Filename's basename.
func TestFilenameWithLiteralPercentRoundTrip(t *testing.T) {
	t.Parallel()

	const filename = "pool/unstable/e/edge-gfx-dkms/edge-gfx-dkms_7.0-260928T031409Z%2B1_all.deb"
	u, err := getFullUrl(filename, "http://example.com/repo")
	if err != nil {
		t.Fatalf("getFullUrl: %v", err)
	}
	if got, want := debFileName(u), filepath.Base(filename); got != want {
		t.Errorf("debFileName(getFullUrl(%q)) = %q, want %q", filename, got, want)
	}
}

func TestSameFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a, b []string
		want bool
	}{
		{"both empty", nil, []string{}, true},
		{"same order", []string{"a", "b"}, []string{"a", "b"}, true},
		{"different order", []string{"a", "b"}, []string{"b", "a"}, true},
		{"duplicates ignored", []string{"a", "a"}, []string{"a"}, true},
		{"subset", []string{"a"}, []string{"a", "b"}, false},
		{"empty vs filtered", nil, []string{"a"}, false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := sameFilter(tt.a, tt.b); got != tt.want {
				t.Errorf("sameFilter(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// TestParseRepositoryMetadata_InstalledSize confirms the Debian Installed-Size
// field (in KiB) is parsed into PackageInfo.InstalledSizeBytes (bytes; used to
// auto-size an overlay disk grow), and that a stanza without it reports 0.
func TestParseRepositoryMetadata_InstalledSize(t *testing.T) {
	buildPath := filepath.Join(t.TempDir(), "repo_main")
	if err := os.MkdirAll(buildPath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	stanzas := "Package: sized\nVersion: 1.0\nArchitecture: amd64\nInstalled-Size: 2048\n" +
		"Filename: pool/main/s/sized/sized_1.0_amd64.deb\n\n" +
		"Package: nosize\nVersion: 1.0\nArchitecture: amd64\n" +
		"Filename: pool/main/n/nosize/nosize_1.0_amd64.deb\n\n" +
		"Package: huge\nVersion: 1.0\nArchitecture: amd64\nInstalled-Size: 9223372036854775807\n" +
		"Filename: pool/main/h/huge/huge_1.0_amd64.deb\n\n" +
		"Package: zerosize\nVersion: 1.0\nArchitecture: amd64\nInstalled-Size: 0\n" +
		"Filename: pool/main/z/zerosize/zerosize_1.0_amd64.deb\n\n"

	pkggzPath := filepath.Join(buildPath, "Packages.gz")
	pkgFile, err := os.Create(pkggzPath)
	if err != nil {
		t.Fatalf("create Packages.gz: %v", err)
	}
	gzWriter := gzip.NewWriter(pkgFile)
	if _, err := gzWriter.Write([]byte(stanzas)); err != nil {
		t.Fatalf("write gzip: %v", err)
	}
	if err := gzWriter.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	if err := pkgFile.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}

	checksum, err := computeFileSHA256(pkggzPath)
	if err != nil {
		t.Fatalf("checksum: %v", err)
	}
	releaseContent := fmt.Sprintf("SHA256:\n %s 1 main/binary-amd64/Packages.gz\n", checksum)
	if err := os.WriteFile(filepath.Join(buildPath, "Release"), []byte(releaseContent), 0o644); err != nil {
		t.Fatalf("write Release: %v", err)
	}

	pkgs := parseFixtureMetadata(t, "http://example.invalid:1/", buildPath)
	got := map[string]int64{}
	hasSize := map[string]bool{}
	for _, p := range pkgs {
		got[p.Name] = p.InstalledSizeBytes
		hasSize[p.Name] = p.HasInstalledSize
	}
	if want := int64(2048 * 1024); got["sized"] != want || !hasSize["sized"] {
		t.Errorf("sized InstalledSizeBytes/HasInstalledSize = %d/%v, want %d/true", got["sized"], hasSize["sized"], want)
	}
	if got["nosize"] != 0 || hasSize["nosize"] {
		t.Errorf("nosize InstalledSizeBytes/HasInstalledSize = %d/%v, want 0/false (no Installed-Size)", got["nosize"], hasSize["nosize"])
	}
	// A KiB value whose ×1024 conversion would overflow int64 must be treated as
	// unknown, not silently wrapped negative.
	if got["huge"] != 0 || hasSize["huge"] {
		t.Errorf("huge InstalledSizeBytes/HasInstalledSize = %d/%v, want 0/false (overflow guarded)", got["huge"], hasSize["huge"])
	}
	// An explicit "Installed-Size: 0" is a real, reported footprint — distinct from
	// a stanza that omits the field entirely — and must be marked known.
	if got["zerosize"] != 0 || !hasSize["zerosize"] {
		t.Errorf("zerosize InstalledSizeBytes/HasInstalledSize = %d/%v, want 0/true (confirmed zero footprint)", got["zerosize"], hasSize["zerosize"])
	}
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
	refreshed, err := refreshRepoMetadata(
		dir,
		[]string{release},
		[]string{"Release"},
		func(string) error { return nil },
	)
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

func TestRefreshRepoMetadata_VerifiesBeforeCommit(t *testing.T) {
	dir := t.TempDir()
	releasePath := filepath.Join(dir, "Release")
	signPath := filepath.Join(dir, "Release.gpg")
	keyPath := filepath.Join(dir, "repo.gpg")

	entity, err := openpgp.NewEntity("Repository", "test", "repo@example.invalid", nil)
	if err != nil {
		t.Fatalf("creating signing entity: %v", err)
	}
	originalRelease := []byte("Suite: stable\n")
	var signature bytes.Buffer
	if err := openpgp.DetachSign(&signature, entity, bytes.NewReader(originalRelease), nil); err != nil {
		t.Fatalf("signing Release: %v", err)
	}
	var publicKey bytes.Buffer
	if err := entity.Serialize(&publicKey); err != nil {
		t.Fatalf("serializing public key: %v", err)
	}

	originalFiles := map[string][]byte{
		releasePath: originalRelease,
		signPath:    signature.Bytes(),
		keyPath:     publicKey.Bytes(),
	}
	for path, contents := range originalFiles {
		if err := os.WriteFile(path, contents, 0o644); err != nil {
			t.Fatalf("writing %s: %v", filepath.Base(path), err)
		}
	}

	tamperedRelease := []byte("Suite: attacker-controlled\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		responses := map[string][]byte{
			"Release":     tamperedRelease,
			"Release.gpg": signature.Bytes(),
			"repo.gpg":    publicKey.Bytes(),
		}
		response, ok := responses[filepath.Base(r.URL.Path)]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if _, err := w.Write(response); err != nil {
			t.Errorf("writing response for %s: %v", r.URL.Path, err)
		}
	}))
	t.Cleanup(server.Close)

	localFiles := []string{releasePath, signPath, keyPath}
	urls := []string{
		server.URL + "/Release",
		server.URL + "/Release.gpg",
		server.URL + "/repo.gpg",
	}
	verify := func(stageDir string) error {
		verified, verifyErr := VerifyRelease(
			filepath.Join(stageDir, "Release"),
			filepath.Join(stageDir, "Release.gpg"),
			filepath.Join(stageDir, "repo.gpg"),
		)
		if verifyErr != nil {
			return verifyErr
		}
		if !verified {
			return fmt.Errorf("release verification failed")
		}
		return nil
	}

	refreshed, err := refreshRepoMetadata(dir, localFiles, urls, verify)
	if err == nil {
		t.Fatal("refreshRepoMetadata succeeded with a tampered Release")
	}
	if refreshed {
		t.Error("refreshRepoMetadata reported unverified metadata as refreshed")
	}

	for path, want := range originalFiles {
		got, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("reading %s after rejected refresh: %v", filepath.Base(path), readErr)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s changed after rejected refresh", filepath.Base(path))
		}
	}
}

// TestParseRepositoryMetadata_FallbackReverifiesPersistentSet is a regression
// test for the review comment that a failed refresh must not proceed on an
// unverified on-disk set. A partial rename in refreshRepoMetadata can leave
// Release and Release.gpg mismatched; when the subsequent refresh fails, the
// fallback must re-verify the persistent set and refuse to use it when it no
// longer agrees, instead of parsing it.
func TestParseRepositoryMetadata_FallbackReverifiesPersistentSet(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "repo.gpg")

	entity, err := openpgp.NewEntity("Repository", "test", "repo@example.invalid", nil)
	if err != nil {
		t.Fatalf("creating signing entity: %v", err)
	}
	var publicKey bytes.Buffer
	if err := entity.Serialize(&publicKey); err != nil {
		t.Fatalf("serializing public key: %v", err)
	}
	if err := os.WriteFile(keyPath, publicKey.Bytes(), 0o644); err != nil {
		t.Fatalf("writing key: %v", err)
	}

	// Persist a Release that disagrees with its signature, standing in for a
	// mixed set left behind by a partial rename.
	var signature bytes.Buffer
	if err := openpgp.DetachSign(&signature, entity, bytes.NewReader([]byte("Suite: signed\n")), nil); err != nil {
		t.Fatalf("signing: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Release"), []byte("Suite: tampered\n"), 0o644); err != nil {
		t.Fatalf("writing Release: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Release.gpg"), signature.Bytes(), 0o644); err != nil {
		t.Fatalf("writing Release.gpg: %v", err)
	}

	// A 404 makes the refresh fail fast (FetchPackages does not retry 404), so
	// the fallback runs against the persistent, mismatched set.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	_, err = ParseRepositoryMetadata(
		server.URL+"/", "Packages.gz",
		server.URL+"/Release", server.URL+"/Release.gpg",
		keyPath, dir, "amd64", nil,
	)
	if err == nil {
		t.Fatal("expected an error: the persistent metadata does not verify and must not be used")
	}
	if !strings.Contains(err.Error(), "no longer verifies") {
		t.Errorf("expected a re-verification error, got: %v", err)
	}
}

// TestRefreshRepoMetadataWithRetry_RecoversFromTransientMismatch is a
// regression test for CI jobs that failed when a mirror served Release and
// Release.gpg from backend nodes that had briefly fallen out of sync: the
// first fetch pairs a stale signature with the current Release, so
// verification fails even though the repository itself is fine. A retry that
// re-fetches (landing on a synced pair) must recover without the caller
// treating it as a hard failure.
func TestRefreshRepoMetadataWithRetry_RecoversFromTransientMismatch(t *testing.T) {
	originalDelay := metadataRefreshRetryDelay
	t.Cleanup(func() { metadataRefreshRetryDelay = originalDelay })
	metadataRefreshRetryDelay = time.Millisecond

	dir := t.TempDir()
	release := filepath.Join(dir, "Release")
	const original = "SHA256:\n deadbeef 1 main/binary-amd64/Packages.gz\n"
	if err := os.WriteFile(release, []byte(original), 0644); err != nil {
		t.Fatalf("writing Release: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("Suite: stable\n"))
	}))
	t.Cleanup(server.Close)

	var verifyCalls atomic.Int32
	verify := func(string) error {
		if verifyCalls.Add(1) < 2 {
			return fmt.Errorf("simulated transient mirror signature mismatch")
		}
		return nil
	}

	refreshed, err := refreshRepoMetadataWithRetry(dir, []string{release}, []string{server.URL + "/Release"}, verify)
	if err != nil {
		t.Fatalf("refreshRepoMetadataWithRetry did not recover from a transient mismatch: %v", err)
	}
	if !refreshed {
		t.Error("expected refreshed=true once the retry succeeds")
	}
	if verifyCalls.Load() < 2 {
		t.Errorf("expected a retry after the first verification failure, got %d verify call(s)", verifyCalls.Load())
	}
}

// TestRefreshRepoMetadataWithRetry_PersistentFailureStillErrors ensures a
// verification failure that never clears (not a transient mismatch) still
// fails the build after the bounded attempts are exhausted, rather than
// silently falling back to unverified metadata.
func TestRefreshRepoMetadataWithRetry_PersistentFailureStillErrors(t *testing.T) {
	originalDelay := metadataRefreshRetryDelay
	t.Cleanup(func() { metadataRefreshRetryDelay = originalDelay })
	metadataRefreshRetryDelay = time.Millisecond

	dir := t.TempDir()
	release := filepath.Join(dir, "Release")
	const original = "SHA256:\n deadbeef 1 main/binary-amd64/Packages.gz\n"
	if err := os.WriteFile(release, []byte(original), 0644); err != nil {
		t.Fatalf("writing Release: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("Suite: stable\n"))
	}))
	t.Cleanup(server.Close)

	var verifyCalls atomic.Int32
	verify := func(string) error {
		verifyCalls.Add(1)
		return fmt.Errorf("persistently untrusted signature")
	}

	refreshed, err := refreshRepoMetadataWithRetry(dir, []string{release}, []string{server.URL + "/Release"}, verify)
	if err == nil {
		t.Fatal("refreshRepoMetadataWithRetry succeeded for a persistent failure, want an error")
	}
	if refreshed {
		t.Error("expected refreshed=false after every attempt fails")
	}
	if verifyCalls.Load() != maxMetadataRefreshAttempts {
		t.Errorf("expected %d verify calls, got %d", maxMetadataRefreshAttempts, verifyCalls.Load())
	}

	got, readErr := os.ReadFile(release)
	if readErr != nil {
		t.Fatalf("reading Release after persistent failure: %v", readErr)
	}
	if string(got) != original {
		t.Errorf("Release was modified despite persistent verification failure:\n got %q\nwant %q", got, original)
	}
}

// TestRefreshRepoMetadataWithRetry_DialsFreshConnectionPerAttempt confirms the
// retry loop drops pooled keep-alive connections between attempts, so each
// attempt opens a new connection instead of reusing the one that just served a
// mismatched Release/Release.gpg pair. Without CloseIdleConnections the shared
// secure client would reuse the pooled connection to the same backend, and the
// server would observe a single connection across all attempts.
func TestRefreshRepoMetadataWithRetry_DialsFreshConnectionPerAttempt(t *testing.T) {
	originalDelay := metadataRefreshRetryDelay
	t.Cleanup(func() { metadataRefreshRetryDelay = originalDelay })
	metadataRefreshRetryDelay = time.Millisecond

	dir := t.TempDir()
	release := filepath.Join(dir, "Release")
	if err := os.WriteFile(release, []byte("Suite: stable\n"), 0644); err != nil {
		t.Fatalf("writing Release: %v", err)
	}

	var newConns atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("Suite: stable\n"))
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			newConns.Add(1)
		}
	}
	server.Start()
	t.Cleanup(server.Close)

	// Fail verify on every attempt so all maxMetadataRefreshAttempts run and
	// each re-fetches from the server.
	verify := func(string) error { return fmt.Errorf("simulated persistent mismatch") }

	refreshed, err := refreshRepoMetadataWithRetry(dir, []string{release}, []string{server.URL + "/Release"}, verify)
	if err == nil {
		t.Fatal("expected an error after every verification attempt fails")
	}
	if refreshed {
		t.Error("expected refreshed=false when every attempt fails verification")
	}

	if got := newConns.Load(); got < maxMetadataRefreshAttempts {
		t.Errorf("expected a fresh connection per attempt (>= %d), got %d; "+
			"retries may be reusing pooled connections to the same backend",
			maxMetadataRefreshAttempts, got)
	}
}

// TestRefreshRepoMetadataWithRetry_DoesNotRetryFetchErrors confirms only a
// verification mismatch is retried. A deterministic fetch failure (HTTP 404,
// which FetchPackages does not itself retry) must return immediately without
// consuming the retry budget or running verify.
func TestRefreshRepoMetadataWithRetry_DoesNotRetryFetchErrors(t *testing.T) {
	originalDelay := metadataRefreshRetryDelay
	t.Cleanup(func() { metadataRefreshRetryDelay = originalDelay })
	metadataRefreshRetryDelay = time.Millisecond

	dir := t.TempDir()
	release := filepath.Join(dir, "Release")
	if err := os.WriteFile(release, []byte("Suite: stable\n"), 0644); err != nil {
		t.Fatalf("writing Release: %v", err)
	}

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	var verifyCalls atomic.Int32
	verify := func(string) error {
		verifyCalls.Add(1)
		return nil
	}

	refreshed, err := refreshRepoMetadataWithRetry(dir, []string{release}, []string{server.URL + "/Release"}, verify)
	if err == nil {
		t.Fatal("expected an error for a non-verify (fetch) failure")
	}
	if refreshed {
		t.Error("expected refreshed=false on a fetch failure")
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("a non-verify error must not be retried: expected exactly 1 fetch, got %d", got)
	}
	if got := verifyCalls.Load(); got != 0 {
		t.Errorf("verify must not run when the fetch fails, got %d call(s)", got)
	}
}

// TestRefreshRepoMetadataWithRetry_BackoffIsCancellable is a regression test for
// the review comment that the retry backoff must observe context cancellation.
// With the run context cancelled mid-backoff, the helper must return promptly
// instead of sleeping out the full delay before the next fetch notices.
func TestRefreshRepoMetadataWithRetry_BackoffIsCancellable(t *testing.T) {
	originalDelay := metadataRefreshRetryDelay
	t.Cleanup(func() { metadataRefreshRetryDelay = originalDelay })
	originalWait := metadataRefreshWait
	t.Cleanup(func() { metadataRefreshWait = originalWait })
	// Long enough that an unbroken sleep would dominate the elapsed time.
	metadataRefreshRetryDelay = 2 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	restore := runctx.SetContext(ctx)
	t.Cleanup(restore)

	dir := t.TempDir()
	release := filepath.Join(dir, "Release")
	if err := os.WriteFile(release, []byte("Suite: stable\n"), 0644); err != nil {
		t.Fatalf("writing Release: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("Suite: stable\n"))
	}))
	t.Cleanup(server.Close)

	backoffEntered := make(chan struct{})
	verify := func(string) error { return fmt.Errorf("simulated transient mismatch") }
	metadataRefreshWait = func(time.Duration) <-chan time.Time {
		close(backoffEntered)
		return make(chan time.Time)
	}
	go func() {
		<-backoffEntered
		cancel()
	}()

	start := time.Now()
	_, err := refreshRepoMetadataWithRetry(dir, []string{release}, []string{server.URL + "/Release"}, verify)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error when the run context is cancelled during backoff")
	}
	if elapsed >= time.Second {
		t.Errorf("backoff ignored cancellation: returned after %s, want prompt cancellation (delay was %s)",
			elapsed, metadataRefreshRetryDelay)
	}
}

func TestParseRepositoryMetadata_InReleaseOnly(t *testing.T) {
	signer, err := openpgp.NewEntity("Repo Signer", "test", "signer@example.invalid", nil)
	if err != nil {
		t.Fatalf("NewEntity: %v", err)
	}
	var pubKey bytes.Buffer
	pubWriter, err := armor.Encode(&pubKey, openpgp.PublicKeyType, nil)
	if err != nil {
		t.Fatalf("armor.Encode: %v", err)
	}
	if err := signer.Serialize(pubWriter); err != nil {
		t.Fatalf("signer.Serialize: %v", err)
	}
	if err := pubWriter.Close(); err != nil {
		t.Fatalf("armor close: %v", err)
	}
	keyPath := filepath.Join(t.TempDir(), "signer.pub")
	if err := os.WriteFile(keyPath, pubKey.Bytes(), 0644); err != nil {
		t.Fatalf("write key: %v", err)
	}

	packagesContent := "Package: edgepack-demo\nVersion: 1.0\nArchitecture: amd64\n" +
		"Filename: pool/main/e/edgepack-demo/edgepack-demo_1.0_amd64.deb\n\n"
	var pkggzBuf bytes.Buffer
	gzWriter := gzip.NewWriter(&pkggzBuf)
	if _, err := gzWriter.Write([]byte(packagesContent)); err != nil {
		t.Fatalf("write gzip: %v", err)
	}
	if err := gzWriter.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	pkggzChecksum := fmt.Sprintf("%x", sha256.Sum256(pkggzBuf.Bytes()))

	releaseContent := fmt.Sprintf("SHA256:\n %s 1 main/binary-amd64/Packages.gz\n", pkggzChecksum)
	var inRelease bytes.Buffer
	clearsignWriter, err := clearsign.Encode(&inRelease, signer.PrivateKey, nil)
	if err != nil {
		t.Fatalf("clearsign.Encode: %v", err)
	}
	if _, err := clearsignWriter.Write([]byte(releaseContent)); err != nil {
		t.Fatalf("write InRelease body: %v", err)
	}
	if err := clearsignWriter.Close(); err != nil {
		t.Fatalf("close clearsign writer: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/dists/noble/InRelease", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(inRelease.Bytes())
	})
	mux.HandleFunc("/dists/noble/main/binary-amd64/Packages.gz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(pkggzBuf.Bytes())
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	buildPath := t.TempDir()
	pkgs, err := ParseRepositoryMetadata(
		server.URL,
		server.URL+"/dists/noble/main/binary-amd64/Packages.gz",
		server.URL+"/dists/noble/InRelease",
		inReleaseSentinel,
		keyPath,
		buildPath,
		"amd64",
		nil,
	)
	if err != nil {
		t.Fatalf("ParseRepositoryMetadata returned error: %v", err)
	}
	if len(pkgs) != 1 || pkgs[0].Name != "edgepack-demo" {
		t.Fatalf("expected exactly the edgepack-demo package, got %+v", pkgs)
	}
}

// TestParseRepositoryMetadata_DerivesPlaintextFromCachedInReleaseOffline is a
// regression test for the offline-cache-recovery branch: a cache holding a
// valid InRelease file and a matching Packages.gz must derive the plaintext
// locally when it is missing or stale, rather than parse stale data after a
// refresh failure.
func TestParseRepositoryMetadata_DerivesPlaintextFromCachedInReleaseOffline(t *testing.T) {
	signer, err := openpgp.NewEntity("Repo Signer", "test", "signer@example.invalid", nil)
	if err != nil {
		t.Fatalf("NewEntity: %v", err)
	}
	var pubKey bytes.Buffer
	pubWriter, err := armor.Encode(&pubKey, openpgp.PublicKeyType, nil)
	if err != nil {
		t.Fatalf("armor.Encode: %v", err)
	}
	if err := signer.Serialize(pubWriter); err != nil {
		t.Fatalf("signer.Serialize: %v", err)
	}
	if err := pubWriter.Close(); err != nil {
		t.Fatalf("armor close: %v", err)
	}
	keyPath := filepath.Join(t.TempDir(), "signer.pub")
	if err := os.WriteFile(keyPath, pubKey.Bytes(), 0644); err != nil {
		t.Fatalf("write key: %v", err)
	}

	packagesContent := "Package: edgepack-demo\nVersion: 1.0\nArchitecture: amd64\n" +
		"Filename: pool/main/e/edgepack-demo/edgepack-demo_1.0_amd64.deb\n\n"
	var pkggzBuf bytes.Buffer
	gzWriter := gzip.NewWriter(&pkggzBuf)
	if _, err := gzWriter.Write([]byte(packagesContent)); err != nil {
		t.Fatalf("write gzip: %v", err)
	}
	if err := gzWriter.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	pkggzChecksum := fmt.Sprintf("%x", sha256.Sum256(pkggzBuf.Bytes()))

	releaseContent := fmt.Sprintf("SHA256:\n %s 1 main/binary-amd64/Packages.gz\n", pkggzChecksum)
	var inRelease bytes.Buffer
	clearsignWriter, err := clearsign.Encode(&inRelease, signer.PrivateKey, nil)
	if err != nil {
		t.Fatalf("clearsign.Encode: %v", err)
	}
	if _, err := clearsignWriter.Write([]byte(releaseContent)); err != nil {
		t.Fatalf("write InRelease body: %v", err)
	}
	if err := clearsignWriter.Close(); err != nil {
		t.Fatalf("close clearsign writer: %v", err)
	}

	// Seed the cache directory as if a previous run had already fetched and
	// verified InRelease and Packages.gz, but predates the derived ".plain"
	// cache file — conspicuously absent here.
	buildPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(buildPath, "InRelease"), inRelease.Bytes(), 0644); err != nil {
		t.Fatalf("seed InRelease: %v", err)
	}
	if err := os.WriteFile(filepath.Join(buildPath, "InRelease.plain"), []byte("stale Release\n"), 0644); err != nil {
		t.Fatalf("seed stale InRelease.plain: %v", err)
	}
	if err := os.WriteFile(filepath.Join(buildPath, "Packages.gz"), pkggzBuf.Bytes(), 0644); err != nil {
		t.Fatalf("seed Packages.gz: %v", err)
	}

	// The repository itself is unreachable, so any refresh attempt must fail
	// and ParseRepositoryMetadata must fall back entirely to the seeded cache.
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()

	pkgs, err := ParseRepositoryMetadata(
		server.URL,
		server.URL+"/dists/noble/main/binary-amd64/Packages.gz",
		server.URL+"/dists/noble/InRelease",
		inReleaseSentinel,
		keyPath,
		buildPath,
		"amd64",
		nil,
	)
	if err != nil {
		t.Fatalf("ParseRepositoryMetadata returned error: %v", err)
	}
	if len(pkgs) != 1 || pkgs[0].Name != "edgepack-demo" {
		t.Fatalf("expected exactly the edgepack-demo package, got %+v", pkgs)
	}

	plaintext, err := os.ReadFile(filepath.Join(buildPath, "InRelease.plain"))
	if err != nil {
		t.Fatalf("reading regenerated InRelease.plain: %v", err)
	}
	if !bytes.Contains(plaintext, []byte(releaseContent)) {
		t.Errorf("InRelease.plain was not regenerated from verified InRelease: %q", plaintext)
	}
}

// TestParseRepositoryMetadata_InstalledSize confirms the Debian Installed-Size
// field (in KiB) is parsed into PackageInfo.InstalledSizeBytes (bytes; used to
// auto-size an overlay disk grow), and that a stanza without it reports 0.

// versioned term).
func TestParseRepositoryMetadata_ParsesVersionedProvides(t *testing.T) {
	buildPath := filepath.Join(t.TempDir(), "repo_main")
	if err := os.MkdirAll(buildPath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	stanza := "Package: libqt6core6t64\nVersion: 6.4.2+dfsg-21.1build5\nArchitecture: amd64\n" +
		"Provides: qt6-base-abi (= 6.4.2)\n" +
		"Filename: pool/universe/q/qt6-base/libqt6core6t64_6.4.2+dfsg-21.1build5_amd64.deb\n\n"

	pkggzPath := filepath.Join(buildPath, "Packages.gz")
	pkgFile, err := os.Create(pkggzPath)
	if err != nil {
		t.Fatalf("create Packages.gz: %v", err)
	}
	gzWriter := gzip.NewWriter(pkgFile)
	if _, err := gzWriter.Write([]byte(stanza)); err != nil {
		t.Fatalf("write gzip: %v", err)
	}
	if err := gzWriter.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	if err := pkgFile.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}

	checksum, err := computeFileSHA256(pkggzPath)
	if err != nil {
		t.Fatalf("checksum: %v", err)
	}
	releaseContent := fmt.Sprintf("SHA256:\n %s 1 main/binary-amd64/Packages.gz\n", checksum)
	if err := os.WriteFile(filepath.Join(buildPath, "Release"), []byte(releaseContent), 0o644); err != nil {
		t.Fatalf("write Release: %v", err)
	}

	pkgs := parseFixtureMetadata(t, "http://example.invalid:1/", buildPath)
	if len(pkgs) != 1 {
		t.Fatalf("expected exactly one package, got %d: %+v", len(pkgs), pkgs)
	}
	pkg := pkgs[0]
	if len(pkg.Provides) != 1 || pkg.Provides[0] != "qt6-base-abi" {
		t.Errorf("Provides = %v, want [qt6-base-abi]", pkg.Provides)
	}
	if len(pkg.ProvidesVer) != 1 || pkg.ProvidesVer[0] != "qt6-base-abi (= 6.4.2)" {
		t.Errorf("ProvidesVer = %v, want [qt6-base-abi (= 6.4.2)]", pkg.ProvidesVer)
	}
}
