package debutils

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
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"

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
