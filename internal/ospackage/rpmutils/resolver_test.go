package rpmutils

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/klauspost/compress/zstd"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage/resolvertest"
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
					Name:             "bash",
					PkgName:          "bash",
					Requires:         []string{"glibc", "ncurses"},
					RequiresPkgNames: []string{"glibc", "ncurses"},
				},
				{
					Name:             "glibc",
					PkgName:          "glibc",
					Requires:         []string{},
					RequiresPkgNames: []string{},
				},
				{
					Name:             "ncurses",
					PkgName:          "ncurses",
					Requires:         []string{"glibc"},
					RequiresPkgNames: []string{"glibc"},
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
					Name:             "standalone",
					PkgName:          "standalone",
					Requires:         []string{},
					RequiresPkgNames: []string{},
				},
			},
			filename:    filepath.Join(tmpDir, "standalone.dot"),
			expectError: false,
		},
		{
			name: "packages with special characters in names",
			packages: []ospackage.PackageInfo{
				{
					Name:             "package-with-dashes",
					PkgName:          "package-with-dashes",
					Requires:         []string{"lib.so.1"},
					RequiresPkgNames: []string{"lib.so.1"},
				},
				{
					Name:             "lib.so.1",
					PkgName:          "lib.so.1",
					Requires:         []string{},
					RequiresPkgNames: []string{},
				},
			},
			filename:    filepath.Join(tmpDir, "special_chars.dot"),
			expectError: false,
		},
		{
			name: "with package source colors",
			packages: []ospackage.PackageInfo{
				{Name: "kernel", PkgName: "kernel", Requires: []string{}, RequiresPkgNames: []string{}},
				{Name: "boot", PkgName: "boot", Requires: []string{}, RequiresPkgNames: []string{}},
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
					Name:             "libstdc++",
					PkgName:          "libstdc++",
					Requires:         []string{"glibc", "glibc", "glibc", "libgcc", "libgcc"},
					RequiresPkgNames: []string{"glibc", "glibc", "glibc", "libgcc", "libgcc"},
				},
				{
					Name:             "glibc",
					PkgName:          "glibc",
					Requires:         []string{},
					RequiresPkgNames: []string{},
				},
				{
					Name:             "libgcc",
					PkgName:          "libgcc",
					Requires:         []string{"glibc"},
					RequiresPkgNames: []string{"glibc"},
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

// TestParsePrimary_InstalledSize confirms the rpm-md <size installed="…"/>
// element is parsed into PackageInfo.InstalledSizeBytes (used to auto-size an
// overlay disk grow), and that a package without a <size> element reports 0.
func TestParsePrimary_InstalledSize(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?><metadata xmlns="http://linux.duke.edu/metadata/common" xmlns:rpm="http://linux.duke.edu/metadata/rpm" packages="3">` +
		`<package type="rpm"><name>bash</name><arch>x86_64</arch><size package="1000" installed="524288" archive="2000"/><location href="bash.rpm"/><format></format></package>` +
		`<package type="rpm"><name>nosize</name><arch>x86_64</arch><location href="nosize.rpm"/><format></format></package>` +
		`<package type="rpm"><name>zerosize</name><arch>x86_64</arch><size package="1000" installed="0" archive="2000"/><location href="zerosize.rpm"/><format></format></package>` +
		`</metadata>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(compressGzip(t, xml))
	}))
	defer server.Close()

	packages, err := ParseRepositoryMetadata(server.URL+"/", "primary.xml.gz", nil)
	if err != nil {
		t.Fatalf("ParseRepositoryMetadata: %v", err)
	}
	// Key on PkgName (the canonical <name>): <location> overwrites Name with the
	// rpm filename basename.
	got := map[string]int64{}
	hasSize := map[string]bool{}
	for _, p := range packages {
		got[p.PkgName] = p.InstalledSizeBytes
		hasSize[p.PkgName] = p.HasInstalledSize
	}
	if got["bash"] != 524288 || !hasSize["bash"] {
		t.Errorf("bash InstalledSizeBytes/HasInstalledSize = %d/%v, want 524288/true", got["bash"], hasSize["bash"])
	}
	if got["nosize"] != 0 || hasSize["nosize"] {
		t.Errorf("nosize InstalledSizeBytes/HasInstalledSize = %d/%v, want 0/false (no <size> element)", got["nosize"], hasSize["nosize"])
	}
	// An explicit installed="0" is a real, reported footprint — distinct from a
	// package that omits <size> entirely — and must be marked known.
	if got["zerosize"] != 0 || !hasSize["zerosize"] {
		t.Errorf("zerosize InstalledSizeBytes/HasInstalledSize = %d/%v, want 0/true (confirmed zero footprint)", got["zerosize"], hasSize["zerosize"])
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

func compressZstd(t *testing.T, content string) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

func setRPMMetadataTestCache(t *testing.T) string {
	t.Helper()

	origCfg := config.Global()
	updatedCfg := origCfg
	updatedCfg.CacheDir = t.TempDir()
	config.SetGlobal(updatedCfg)
	t.Cleanup(func() { config.SetGlobal(origCfg) })

	return updatedCfg.CacheDir
}

func testPrimaryXML(packageName string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<metadata xmlns="http://linux.duke.edu/metadata/common" xmlns:rpm="http://linux.duke.edu/metadata/rpm" packages="1">
  <package type="rpm">
	<location href="%s-1.0-1.x86_64.rpm"/>
    <name>%s</name>
    <arch>x86_64</arch>
    <version epoch="0" ver="1.0" rel="1"/>
    <checksum type="sha256">abc</checksum>
    <format><rpm:provides><rpm:entry name="%s"/></rpm:provides></format>
  </package>
</metadata>`, packageName, packageName, packageName)
}

func testRepomd(primaryHref string) []byte {
	return testRepomdWithPrimary(rpmPrimaryReference{Href: primaryHref})
}

func testRepomdWithPrimary(primary rpmPrimaryReference) []byte {
	size := ""
	if primary.Size > 0 {
		size = fmt.Sprintf("\n    <size>%d</size>", primary.Size)
	}
	checksum := ""
	if primary.Checksum != "" {
		checksum = fmt.Sprintf("\n    <checksum type=\"%s\">%s</checksum>", primary.ChecksumType, primary.Checksum)
	}
	return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<repomd xmlns="http://linux.duke.edu/metadata/repo">
  <data type="primary">
    <location href="%s"/>%s%s
  </data>
</repomd>`, primary.Href, checksum, size))
}

func primaryRefForData(primaryHref string, data []byte) rpmPrimaryReference {
	sum := sha256.Sum256(data)
	return rpmPrimaryReference{
		Href:         primaryHref,
		ChecksumType: "SHA256",
		Checksum:     hex.EncodeToString(sum[:]),
		Size:         int64(len(data)),
	}
}

func seedRPMRawMetadataCache(t *testing.T, baseURL, primaryHref string, data []byte) string {
	t.Helper()
	return seedRPMRawMetadataCacheWithPrimary(t, baseURL, primaryRefForData(primaryHref, data), data)
}

func seedRPMRawMetadataCacheWithPrimary(t *testing.T, baseURL string, primary rpmPrimaryReference, data []byte) string {
	t.Helper()

	cacheDir, err := rpmMetadataCacheDir(baseURL)
	if err != nil {
		t.Fatalf("rpmMetadataCacheDir() error = %v", err)
	}
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("failed to create metadata cache dir: %v", err)
	}
	if err := writeFileAtomic(filepath.Join(cacheDir, "repomd.xml"), testRepomdWithPrimary(primary), 0644); err != nil {
		t.Fatalf("failed to seed repomd cache: %v", err)
	}
	if err := saveRPMRawMetadataCache(cacheDir, primary.Href, rpmMetadataID(baseURL, primary.Href), primary, data); err != nil {
		t.Fatalf("failed to seed raw metadata cache: %v", err)
	}

	return cacheDir
}

func seedCorruptRPMRawMetadataCache(t *testing.T, baseURL string, primary rpmPrimaryReference, data []byte) string {
	t.Helper()

	cacheDir, err := rpmMetadataCacheDir(baseURL)
	if err != nil {
		t.Fatalf("rpmMetadataCacheDir() error = %v", err)
	}
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("failed to create metadata cache dir: %v", err)
	}
	if err := writeFileAtomic(filepath.Join(cacheDir, "repomd.xml"), testRepomdWithPrimary(primary), 0644); err != nil {
		t.Fatalf("failed to seed repomd cache: %v", err)
	}
	dataPath := rpmRawMetadataPayloadPath(cacheDir, primary.Href, primary)
	_, metaPath := rpmRawMetadataCachePaths(cacheDir, primary.Href)
	if err := writeFileAtomic(dataPath, data, 0644); err != nil {
		t.Fatalf("failed to seed corrupt raw metadata: %v", err)
	}
	cache := rpmRawMetadataCache{
		MetadataID: rpmMetadataID(baseURL, primary.Href),
		Primary:    primary,
		DataFile:   filepath.Base(dataPath),
	}
	metaData, err := json.Marshal(cache)
	if err != nil {
		t.Fatalf("failed to marshal corrupt raw metadata manifest: %v", err)
	}
	if err := writeFileAtomic(metaPath, metaData, 0644); err != nil {
		t.Fatalf("failed to seed corrupt raw metadata manifest: %v", err)
	}

	return cacheDir
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

func TestParseRepositoryMetadata_UsesCacheOffline(t *testing.T) {
	origCfg := config.Global()
	updatedCfg := origCfg
	updatedCfg.CacheDir = t.TempDir()
	config.SetGlobal(updatedCfg)
	defer config.SetGlobal(origCfg)

	var requestCount int32
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?><metadata xmlns="http://linux.duke.edu/metadata/common" xmlns:rpm="http://linux.duke.edu/metadata/rpm" packages="1"><package type="rpm"><name>bash</name><arch>x86_64</arch><location href="bash-5.1-8.el9.x86_64.rpm"/></package></metadata>`
	compressed := compressGzip(t, xmlContent)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(compressed)
	}))

	cacheDir := filepath.Join(updatedCfg.CacheDir, "rpm-metadata", generateRPMMetadataDir(server.URL+"/"))
	_ = os.RemoveAll(cacheDir)
	t.Cleanup(func() {
		_ = os.RemoveAll(cacheDir)
	})

	pkgs, err := ParseRepositoryMetadata(server.URL+"/", "primary.xml.gz", nil)
	if err != nil {
		t.Fatalf("first parse failed: %v", err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 package on first parse, got %d", len(pkgs))
	}
	if atomic.LoadInt32(&requestCount) != 1 {
		t.Fatalf("expected 1 network request on first parse, got %d", atomic.LoadInt32(&requestCount))
	}

	server.Close()

	pkgs, err = ParseRepositoryMetadata(server.URL+"/", "primary.xml.gz", nil)
	if err != nil {
		t.Fatalf("second parse should succeed from cache offline, got error: %v", err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 package from cached parse, got %d", len(pkgs))
	}
	if atomic.LoadInt32(&requestCount) != 1 {
		t.Fatalf("expected no additional network requests when using cache, got %d", atomic.LoadInt32(&requestCount))
	}
}

// A cache written before InstalledSizeBytes was added (no "version" key, so it
// unmarshals to the zero value) must be rejected as stale and re-fetched, rather
// than served with every package's installed size silently missing.
func TestParseRepositoryMetadata_StaleUnversionedCacheIsRejected(t *testing.T) {
	origCfg := config.Global()
	updatedCfg := origCfg
	updatedCfg.CacheDir = t.TempDir()
	config.SetGlobal(updatedCfg)
	defer config.SetGlobal(origCfg)

	var requestCount int32
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?><metadata xmlns="http://linux.duke.edu/metadata/common" xmlns:rpm="http://linux.duke.edu/metadata/rpm" packages="1"><package type="rpm"><name>bash</name><arch>x86_64</arch><location href="bash-5.1-8.el9.x86_64.rpm"/></package></metadata>`
	compressed := compressGzip(t, xmlContent)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(compressed)
	}))
	defer server.Close()

	cacheDir := filepath.Join(updatedCfg.CacheDir, "rpm-metadata", generateRPMMetadataDir(server.URL+"/"))
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(cacheDir) })

	// Pre-seed a pre-versioning cache: no "version" key at all.
	staleCache := `{"metadata_url":"` + server.URL + `/primary.xml.gz","packages":[{"PkgName":"stale-cached-package"}]}`
	if err := os.WriteFile(filepath.Join(cacheDir, "primary.parsed.json"), []byte(staleCache), 0600); err != nil {
		t.Fatalf("seed stale cache: %v", err)
	}

	pkgs, err := ParseRepositoryMetadata(server.URL+"/", "primary.xml.gz", nil)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(pkgs) != 1 || pkgs[0].PkgName != "bash" {
		t.Errorf("expected the freshly parsed package, got %+v (stale unversioned cache must not be served)", pkgs)
	}
	if atomic.LoadInt32(&requestCount) != 1 {
		t.Errorf("expected a network request when the cache is stale/unversioned, got %d", atomic.LoadInt32(&requestCount))
	}
}

// A stale/missing parsed cache must still be served offline when a raw primary
// XML from an earlier run is cached: reparse it instead of forcing a network
// fetch, so a parsed-cache version bump alone does not break an otherwise warm
// offline rebuild.
func TestParseRepositoryMetadata_ReparsesCachedRawXMLOffline(t *testing.T) {
	origCfg := config.Global()
	updatedCfg := origCfg
	updatedCfg.CacheDir = t.TempDir()
	config.SetGlobal(updatedCfg)
	defer config.SetGlobal(origCfg)

	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	baseURL := server.URL + "/"
	cacheDir := filepath.Join(updatedCfg.CacheDir, "rpm-metadata", generateRPMMetadataDir(baseURL))
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(cacheDir) })

	xmlContent := `<?xml version="1.0" encoding="UTF-8"?><metadata xmlns="http://linux.duke.edu/metadata/common" xmlns:rpm="http://linux.duke.edu/metadata/rpm" packages="1"><package type="rpm"><name>bash</name><arch>x86_64</arch><location href="bash-5.1-8.el9.x86_64.rpm"/></package></metadata>`
	saveOriginalXML(cacheDir, "primary.xml.gz", baseURL+"primary.xml.gz", compressGzip(t, xmlContent))

	// No parsed cache seeded at all — equivalent to a version mismatch/miss.
	pkgs, err := ParseRepositoryMetadata(baseURL, "primary.xml.gz", nil)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(pkgs) != 1 || pkgs[0].PkgName != "bash" {
		t.Errorf("expected the reparsed cached package, got %+v", pkgs)
	}
	if got := atomic.LoadInt32(&requestCount); got != 0 {
		t.Errorf("expected no network requests when raw metadata is cached offline, got %d", got)
	}

	// The parsed cache is rewritten at the current version after the reparse.
	cached, err := loadRPMParsedMetadataCache(filepath.Join(cacheDir, "primary.parsed.json"))
	if err != nil {
		t.Fatalf("loading rewritten parsed cache: %v", err)
	}
	if cached.Version != rpmParsedMetadataCacheVersion {
		t.Errorf("rewritten cache version = %d, want %d", cached.Version, rpmParsedMetadataCacheVersion)
	}
}

// A raw cache entry saved for one metadata href must not be reused when repomd
// later points at a different href sharing the same basename (e.g. the primary
// XML moves to a new repodata path) — reusing it would reparse stale content and
// silently corrupt the rewritten parsed cache under the new metadata URL.
func TestParseRepositoryMetadata_DoesNotReuseRawCacheAcrossHrefChange(t *testing.T) {
	origCfg := config.Global()
	updatedCfg := origCfg
	updatedCfg.CacheDir = t.TempDir()
	config.SetGlobal(updatedCfg)
	defer config.SetGlobal(origCfg)

	newXML := `<?xml version="1.0" encoding="UTF-8"?><metadata xmlns="http://linux.duke.edu/metadata/common" xmlns:rpm="http://linux.duke.edu/metadata/rpm" packages="1"><package type="rpm"><name>new-package</name><arch>x86_64</arch><location href="new-package-1.0.x86_64.rpm"/></package></metadata>`
	compressedNew := compressGzip(t, newXML)

	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(compressedNew)
	}))
	defer server.Close()

	baseURL := server.URL + "/"
	cacheDir := filepath.Join(updatedCfg.CacheDir, "rpm-metadata", generateRPMMetadataDir(baseURL))
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(cacheDir) })

	// Pre-seed raw cache content under the OLD href — same repo, different path,
	// same basename ("primary.xml.gz").
	oldXML := `<?xml version="1.0" encoding="UTF-8"?><metadata xmlns="http://linux.duke.edu/metadata/common" xmlns:rpm="http://linux.duke.edu/metadata/rpm" packages="1"><package type="rpm"><name>old-package</name><arch>x86_64</arch><location href="old-package-1.0.x86_64.rpm"/></package></metadata>`
	saveOriginalXML(cacheDir, "repodata-old/primary.xml.gz", baseURL+"repodata-old/primary.xml.gz", compressGzip(t, oldXML))

	// repomd now points at a different href for the same repo.
	pkgs, err := ParseRepositoryMetadata(baseURL, "repodata-new/primary.xml.gz", nil)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(pkgs) != 1 || pkgs[0].PkgName != "new-package" {
		t.Errorf("expected the new package fetched over the network, got %+v "+
			"(must not reuse the raw cache saved for a different href)", pkgs)
	}
	if got := atomic.LoadInt32(&requestCount); got != 1 {
		t.Errorf("expected exactly one network fetch (no raw cache matches the new href), got %d", got)
	}
}

// A stale (version-mismatched) parsed cache whose MetadataURL matches fullURL is
// trusted evidence that a legacy, baseURL-only-keyed raw cache (saved by a
// binary built before the fullURL key existed) belongs to this exact URL: it
// must be reparsed offline instead of forcing a network fetch, then migrated to
// the current key so future lookups don't need the legacy fallback.
func TestParseRepositoryMetadata_MigratesLegacyRawCacheOffline(t *testing.T) {
	origCfg := config.Global()
	updatedCfg := origCfg
	updatedCfg.CacheDir = t.TempDir()
	config.SetGlobal(updatedCfg)
	defer config.SetGlobal(origCfg)

	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	baseURL := server.URL + "/"
	fullURL := baseURL + "primary.xml.gz"
	cacheDir := filepath.Join(updatedCfg.CacheDir, "rpm-metadata", generateRPMMetadataDir(baseURL))
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(cacheDir) })

	// Legacy raw cache, as a pre-fullURL-key binary would have saved it: keyed by
	// baseURL only.
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?><metadata xmlns="http://linux.duke.edu/metadata/common" xmlns:rpm="http://linux.duke.edu/metadata/rpm" packages="1"><package type="rpm"><name>bash</name><arch>x86_64</arch><location href="bash-5.1-8.el9.x86_64.rpm"/></package></metadata>`
	saveOriginalXML(cacheDir, "primary.xml.gz", baseURL, compressGzip(t, xmlContent))

	// A stale (older-version) parsed cache confirming this legacy raw file was
	// fetched for this exact fullURL.
	staleCache := fmt.Sprintf(`{"version":%d,"metadata_url":%q,"packages":[{"PkgName":"stale-cached-package"}]}`,
		rpmParsedMetadataCacheVersion-1, fullURL)
	if err := os.WriteFile(filepath.Join(cacheDir, "primary.parsed.json"), []byte(staleCache), 0600); err != nil {
		t.Fatalf("seed stale cache: %v", err)
	}

	pkgs, err := ParseRepositoryMetadata(baseURL, "primary.xml.gz", nil)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(pkgs) != 1 || pkgs[0].PkgName != "bash" {
		t.Errorf("expected the reparsed legacy-cached package, got %+v", pkgs)
	}
	if got := atomic.LoadInt32(&requestCount); got != 0 {
		t.Errorf("expected no network requests when a legacy raw cache is confirmed by the stale parsed cache, got %d", got)
	}

	// The raw file must now also exist under the current (fullURL-keyed) name.
	if _, findErr := findLatestCachedRawMetadata(cacheDir, "primary.xml.gz", fullURL); findErr != nil {
		t.Errorf("expected the legacy raw cache to be migrated to the current key, lookup error: %v", findErr)
	}
}

// A cached raw file that fails to decompress/parse (e.g. truncated by an
// interrupted earlier write) must not abort an otherwise-online build: it is
// treated as a cache miss and the build falls through to a real network fetch.
func TestParseRepositoryMetadata_CorruptedRawCacheFallsBackToNetwork(t *testing.T) {
	origCfg := config.Global()
	updatedCfg := origCfg
	updatedCfg.CacheDir = t.TempDir()
	config.SetGlobal(updatedCfg)
	defer config.SetGlobal(origCfg)

	goodXML := `<?xml version="1.0" encoding="UTF-8"?><metadata xmlns="http://linux.duke.edu/metadata/common" xmlns:rpm="http://linux.duke.edu/metadata/rpm" packages="1"><package type="rpm"><name>bash</name><arch>x86_64</arch><location href="bash-5.1-8.el9.x86_64.rpm"/></package></metadata>`
	compressedGood := compressGzip(t, goodXML)

	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(compressedGood)
	}))
	defer server.Close()

	baseURL := server.URL + "/"
	fullURL := baseURL + "primary.xml.gz"
	cacheDir := filepath.Join(updatedCfg.CacheDir, "rpm-metadata", generateRPMMetadataDir(baseURL))
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(cacheDir) })

	// Cache a truncated (corrupted) raw file under the current key.
	saveOriginalXML(cacheDir, "primary.xml.gz", fullURL, compressedGood[:len(compressedGood)/2])

	pkgs, err := ParseRepositoryMetadata(baseURL, "primary.xml.gz", nil)
	if err != nil {
		t.Fatalf("expected a fallback to the network, not a hard failure: %v", err)
	}
	if len(pkgs) != 1 || pkgs[0].PkgName != "bash" {
		t.Errorf("expected the network-fetched package, got %+v", pkgs)
	}
	if got := atomic.LoadInt32(&requestCount); got != 1 {
		t.Errorf("expected exactly one network fetch after the corrupted cache was rejected, got %d", got)
	}
}

// The raw-metadata cache must not grow one timestamped file per fetch forever:
// saving a new file for a key prunes every older file cached under that key.
func TestSaveOriginalXML_PrunesOlderFilesForSameKey(t *testing.T) {
	cacheDir := t.TempDir()
	metadataHref := "primary.xml.gz"
	fullURL := "https://example.com/repo/primary.xml.gz"

	urlHash := sha256.Sum256([]byte(fullURL))
	urlHashStr := hex.EncodeToString(urlHash[:])[:8]
	pattern := fmt.Sprintf("primary.xml_%s_*.gz", urlHashStr)

	// Seed a "stale" file with an old timestamp, matching the naming scheme
	// saveOriginalXML uses, so a fresh save has something to prune.
	oldPath := filepath.Join(cacheDir, fmt.Sprintf("primary.xml_%s_2000-01-01_00-00-00.gz", urlHashStr))
	if err := os.WriteFile(oldPath, []byte("stale"), 0644); err != nil {
		t.Fatalf("seed stale raw cache file: %v", err)
	}

	saveOriginalXML(cacheDir, metadataHref, fullURL, []byte("fresh"))

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("expected the old cached raw file to be pruned, stat error: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(cacheDir, pattern))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected exactly 1 cached raw file after pruning, got %d: %v", len(matches), matches)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read surviving cached file: %v", err)
	}
	if string(data) != "fresh" {
		t.Errorf("surviving cached file content = %q, want %q", data, "fresh")
	}
}

func TestFetchPrimaryURL_UsesCacheOffline(t *testing.T) {
	origCfg := config.Global()
	updatedCfg := origCfg
	updatedCfg.CacheDir = t.TempDir()
	config.SetGlobal(updatedCfg)
	defer config.SetGlobal(origCfg)

	var requestCount int32
	repomd := `<?xml version="1.0" encoding="UTF-8"?>
<repomd xmlns="http://linux.duke.edu/metadata/repo">
  <data type="primary">
    <location href="repodata/primary.xml.gz"/>
  </data>
</repomd>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(repomd))
	}))

	baseURL := server.URL
	cacheDir := filepath.Join(updatedCfg.CacheDir, "rpm-metadata", generateRPMMetadataDir(baseURL))
	_ = os.RemoveAll(cacheDir)
	t.Cleanup(func() {
		_ = os.RemoveAll(cacheDir)
	})

	repomdURL := baseURL + "/repodata/repomd.xml"
	href, err := FetchPrimaryURL(repomdURL)
	if err != nil {
		t.Fatalf("first FetchPrimaryURL failed: %v", err)
	}
	if href != "repodata/primary.xml.gz" {
		t.Fatalf("unexpected primary href: %s", href)
	}
	if atomic.LoadInt32(&requestCount) != 1 {
		t.Fatalf("expected 1 network request on first FetchPrimaryURL, got %d", atomic.LoadInt32(&requestCount))
	}

	server.Close()

	href, err = FetchPrimaryURL(repomdURL)
	if err != nil {
		t.Fatalf("second FetchPrimaryURL should succeed from cache offline, got error: %v", err)
	}
	if href != "repodata/primary.xml.gz" {
		t.Fatalf("unexpected cached primary href: %s", href)
	}
	if atomic.LoadInt32(&requestCount) != 1 {
		t.Fatalf("expected no additional network requests when using cached primary href, got %d", atomic.LoadInt32(&requestCount))
	}
}

func TestFetchPrimaryURL_UsesCachedRepomdWhenPrimaryLocationMissing(t *testing.T) {
	origCfg := config.Global()
	updatedCfg := origCfg
	updatedCfg.CacheDir = t.TempDir()
	config.SetGlobal(updatedCfg)
	defer config.SetGlobal(origCfg)

	var requestCount int32
	repomd := `<?xml version="1.0" encoding="UTF-8"?>
<repomd xmlns="http://linux.duke.edu/metadata/repo">
  <data type="primary">
    <location href="repodata/primary.xml.gz"/>
  </data>
</repomd>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(repomd))
	}))

	baseURL := server.URL
	cacheDir := filepath.Join(updatedCfg.CacheDir, "rpm-metadata", generateRPMMetadataDir(baseURL))
	_ = os.RemoveAll(cacheDir)
	t.Cleanup(func() {
		_ = os.RemoveAll(cacheDir)
	})

	repomdURL := baseURL + "/repodata/repomd.xml"
	href, err := FetchPrimaryURL(repomdURL)
	if err != nil {
		t.Fatalf("first FetchPrimaryURL failed: %v", err)
	}
	if href != "repodata/primary.xml.gz" {
		t.Fatalf("unexpected primary href: %s", href)
	}

	primaryLocationCacheFile := filepath.Join(cacheDir, "primary.location.json")
	if err := os.Remove(primaryLocationCacheFile); err != nil {
		t.Fatalf("failed to remove primary location cache file: %v", err)
	}

	server.Close()

	href, err = FetchPrimaryURL(repomdURL)
	if err != nil {
		t.Fatalf("FetchPrimaryURL should succeed from cached repomd offline, got error: %v", err)
	}
	if href != "repodata/primary.xml.gz" {
		t.Fatalf("unexpected href from cached repomd: %s", href)
	}
	if atomic.LoadInt32(&requestCount) != 1 {
		t.Fatalf("expected no additional network requests when using cached repomd, got %d", atomic.LoadInt32(&requestCount))
	}
}

func TestFetchPrimaryURL_DoesNotCacheMalformedRepomd(t *testing.T) {
	setRPMMetadataTestCache(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<repomd><data type="primary">`))
	}))
	defer server.Close()

	_, err := FetchPrimaryURL(server.URL + "/repodata/repomd.xml")
	if err == nil {
		t.Fatalf("expected malformed repomd fetch to fail")
	}

	cacheDir, err := rpmMetadataCacheDir(server.URL)
	if err != nil {
		t.Fatalf("rpmMetadataCacheDir() error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(cacheDir, "repomd.xml")); !os.IsNotExist(statErr) {
		t.Fatalf("malformed repomd.xml was published to stable cache, stat error: %v", statErr)
	}
}

func TestFetchPrimaryURL_PrefersStableRepomdOverStaleLocationCache(t *testing.T) {
	setRPMMetadataTestCache(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cacheDir, err := rpmMetadataCacheDir(server.URL)
	if err != nil {
		t.Fatalf("rpmMetadataCacheDir() error = %v", err)
	}
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("failed to create cache dir: %v", err)
	}
	newPrimary := rpmPrimaryReference{Href: "repodata/new-primary.xml.gz"}
	if err := writeFileAtomic(filepath.Join(cacheDir, "repomd.xml"), testRepomdWithPrimary(newPrimary), 0644); err != nil {
		t.Fatalf("failed to seed stable repomd cache: %v", err)
	}
	oldPrimary := rpmPrimaryReference{Href: "repodata/old-primary.xml.gz"}
	if err := saveRPMPrimaryLocationCache(filepath.Join(cacheDir, "primary.location.json"), oldPrimary); err != nil {
		t.Fatalf("failed to seed stale primary location cache: %v", err)
	}

	href, err := FetchPrimaryURL(server.URL + "/repodata/repomd.xml")
	if err != nil {
		t.Fatalf("FetchPrimaryURL() should use stable repomd cache: %v", err)
	}
	if href != newPrimary.Href {
		t.Fatalf("FetchPrimaryURL() href = %q, want stable repomd href %q", href, newPrimary.Href)
	}
}

func TestRPMMetadataCacheHitUsesRawPrimaryWithoutHTTP(t *testing.T) {
	setRPMMetadataTestCache(t)

	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	primaryHref := "repodata/primary.xml.gz"
	cacheDir := seedRPMRawMetadataCache(t, server.URL, primaryHref, compressGzip(t, testPrimaryXML("bash")))
	if err := os.Remove(filepath.Join(cacheDir, "primary.parsed.json")); err != nil && !os.IsNotExist(err) {
		t.Fatalf("failed to remove parsed cache: %v", err)
	}
	if err := os.Remove(filepath.Join(cacheDir, "primary.location.json")); err != nil && !os.IsNotExist(err) {
		t.Fatalf("failed to remove location cache: %v", err)
	}

	href, err := FetchPrimaryURL(server.URL + "/repodata/repomd.xml")
	if err != nil {
		t.Fatalf("FetchPrimaryURL() from cached repomd error = %v", err)
	}
	if href != primaryHref {
		t.Fatalf("FetchPrimaryURL() href = %q, want %q", href, primaryHref)
	}

	pkgs, err := ParseRepositoryMetadata(server.URL, href, nil)
	if err != nil {
		t.Fatalf("ParseRepositoryMetadata() from raw cache error = %v", err)
	}
	if len(pkgs) != 1 || pkgs[0].PkgName != "bash" {
		t.Fatalf("cached packages = %+v, want bash", pkgs)
	}
	if atomic.LoadInt32(&requestCount) != 0 {
		t.Fatalf("HTTP requests = %d, want 0", atomic.LoadInt32(&requestCount))
	}
}

func TestRPMMetadataOnlinePopulateThenOfflineRawCacheReuse(t *testing.T) {
	cacheRoot := setRPMMetadataTestCache(t)

	var requestCount int32
	primaryHref := "repodata/primary.xml.gz"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		switch r.URL.Path {
		case "/repodata/repomd.xml":
			_, _ = w.Write(testRepomd(primaryHref))
		case "/repodata/primary.xml.gz":
			_, _ = w.Write(compressGzip(t, testPrimaryXML("bash")))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	href, err := FetchPrimaryURL(server.URL + "/repodata/repomd.xml")
	if err != nil {
		t.Fatalf("first FetchPrimaryURL() error = %v", err)
	}
	pkgs, err := ParseRepositoryMetadata(server.URL, href, nil)
	if err != nil {
		t.Fatalf("first ParseRepositoryMetadata() error = %v", err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("first package count = %d, want 1", len(pkgs))
	}

	cacheDir := filepath.Join(cacheRoot, "rpm-metadata", generateRPMMetadataDir(server.URL))
	if _, err := os.Stat(filepath.Join(cacheDir, "repomd.xml")); err != nil {
		t.Fatalf("stable repomd cache missing: %v", err)
	}
	_, metaPath := rpmRawMetadataCachePaths(cacheDir, primaryHref)
	if _, err := os.Stat(metaPath); err != nil {
		t.Fatalf("stable primary cache manifest %s missing: %v", metaPath, err)
	}
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read stable primary cache manifest: %v", err)
	}
	var rawCache rpmRawMetadataCache
	if err := json.Unmarshal(metaData, &rawCache); err != nil {
		t.Fatalf("unmarshal stable primary cache manifest: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, rawCache.DataFile)); err != nil {
		t.Fatalf("stable primary cache payload %s missing: %v", rawCache.DataFile, err)
	}
	fullURLRawHash := sha256.Sum256([]byte(server.URL + "/" + primaryHref))
	fullURLRawPattern := fmt.Sprintf("primary.xml_%s_*.gz", hex.EncodeToString(fullURLRawHash[:])[:8])
	fullURLRawMatches, err := filepath.Glob(filepath.Join(cacheDir, fullURLRawPattern))
	if err != nil {
		t.Fatalf("glob fullURL raw metadata cache: %v", err)
	}
	if len(fullURLRawMatches) != 1 {
		t.Fatalf("fullURL-keyed raw cache matches = %v, want 1", fullURLRawMatches)
	}
	if atomic.LoadInt32(&requestCount) != 2 {
		t.Fatalf("online request count = %d, want 2", atomic.LoadInt32(&requestCount))
	}

	server.Close()
	if err := os.Remove(filepath.Join(cacheDir, "primary.parsed.json")); err != nil {
		t.Fatalf("failed to remove parsed cache: %v", err)
	}

	href, err = FetchPrimaryURL(server.URL + "/repodata/repomd.xml")
	if err != nil {
		t.Fatalf("offline FetchPrimaryURL() error = %v", err)
	}
	pkgs, err = ParseRepositoryMetadata(server.URL, href, nil)
	if err != nil {
		t.Fatalf("offline ParseRepositoryMetadata() error = %v", err)
	}
	if len(pkgs) != 1 || pkgs[0].PkgName != "bash" {
		t.Fatalf("offline packages = %+v, want bash", pkgs)
	}
	if atomic.LoadInt32(&requestCount) != 2 {
		t.Fatalf("offline request count = %d, want still 2", atomic.LoadInt32(&requestCount))
	}
}

func TestRPMMetadataMissingPrimaryCacheFetchesOnline(t *testing.T) {
	setRPMMetadataTestCache(t)

	var requestCount int32
	primaryHref := "repodata/primary.xml.gz"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		if r.URL.Path != "/repodata/primary.xml.gz" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write(compressGzip(t, testPrimaryXML("bash")))
	}))
	defer server.Close()

	cacheDir, err := rpmMetadataCacheDir(server.URL)
	if err != nil {
		t.Fatalf("rpmMetadataCacheDir() error = %v", err)
	}
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("failed to create cache dir: %v", err)
	}
	if err := writeFileAtomic(filepath.Join(cacheDir, "repomd.xml"), testRepomd(primaryHref), 0644); err != nil {
		t.Fatalf("failed to seed repomd cache: %v", err)
	}

	href, err := FetchPrimaryURL(server.URL + "/repodata/repomd.xml")
	if err != nil {
		t.Fatalf("FetchPrimaryURL() error = %v", err)
	}
	pkgs, err := ParseRepositoryMetadata(server.URL, href, nil)
	if err != nil {
		t.Fatalf("ParseRepositoryMetadata() error = %v", err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("package count = %d, want 1", len(pkgs))
	}
	if atomic.LoadInt32(&requestCount) != 1 {
		t.Fatalf("HTTP requests = %d, want 1 primary fetch", atomic.LoadInt32(&requestCount))
	}
}

func TestRPMMetadataCorruptRawCacheRefreshesOnlineAndFailsOffline(t *testing.T) {
	setRPMMetadataTestCache(t)

	var requestCount int32
	primaryHref := "repodata/primary.xml.gz"
	goodData := compressGzip(t, testPrimaryXML("bash"))
	primary := primaryRefForData(primaryHref, goodData)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		_, _ = w.Write(goodData)
	}))

	cacheDir := seedCorruptRPMRawMetadataCache(t, server.URL, primary, []byte("truncated gzip"))
	pkgs, err := ParseRepositoryMetadata(server.URL, primaryHref, nil)
	if err != nil {
		t.Fatalf("ParseRepositoryMetadata() should refresh corrupt cache online: %v", err)
	}
	if len(pkgs) != 1 || pkgs[0].PkgName != "bash" {
		t.Fatalf("refreshed packages = %+v, want bash", pkgs)
	}
	if atomic.LoadInt32(&requestCount) != 1 {
		t.Fatalf("HTTP requests = %d, want 1 refresh", atomic.LoadInt32(&requestCount))
	}

	if err := os.Remove(filepath.Join(cacheDir, "primary.parsed.json")); err != nil {
		t.Fatalf("failed to remove parsed cache: %v", err)
	}
	seedCorruptRPMRawMetadataCache(t, server.URL, primary, []byte("truncated gzip"))
	server.Close()

	_, err = ParseRepositoryMetadata(server.URL, primaryHref, nil)
	if err == nil {
		t.Fatalf("expected corrupt offline metadata to fail")
	}
	if !strings.Contains(err.Error(), "invalid cache artifact") {
		t.Fatalf("error = %q, want invalid cache artifact context", err.Error())
	}
}

func TestRPMMetadataChecksumMismatchRejectsValidXMLCache(t *testing.T) {
	setRPMMetadataTestCache(t)

	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	primaryHref := "repodata/primary.xml.gz"
	goodData := compressGzip(t, testPrimaryXML("bash"))
	alteredData := compressGzip(t, testPrimaryXML("curl"))
	primary := primaryRefForData(primaryHref, goodData)
	primary.Size = 0
	seedCorruptRPMRawMetadataCache(t, server.URL, primary, alteredData)

	_, err := ParseRepositoryMetadata(server.URL, primaryHref, nil)
	if err == nil {
		t.Fatalf("expected checksum mismatch to reject cached primary metadata")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("error = %q, want checksum mismatch context", err.Error())
	}
	if atomic.LoadInt32(&requestCount) == 0 {
		t.Fatalf("expected online refresh attempt after checksum mismatch")
	}
}

func TestRPMParsedCacheSourceMismatchUsesVerifiedRawMetadata(t *testing.T) {
	setRPMMetadataTestCache(t)

	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	primaryHref := "repodata/primary.xml.gz"
	goodData := compressGzip(t, testPrimaryXML("bash"))
	cacheDir := seedRPMRawMetadataCache(t, server.URL, primaryHref, goodData)
	parsedFromOtherSource := rpmParsedMetadataCache{
		MetadataID: rpmMetadataID(server.URL, primaryHref),
		Primary:    primaryRefForData(primaryHref, compressGzip(t, testPrimaryXML("coreutils"))),
		Packages:   []ospackage.PackageInfo{{Name: "coreutils", PkgName: "coreutils", Type: "rpm"}},
	}
	parsedData, err := json.Marshal(parsedFromOtherSource)
	if err != nil {
		t.Fatalf("failed to marshal parsed cache fixture: %v", err)
	}
	if err := writeFileAtomic(filepath.Join(cacheDir, "primary.parsed.json"), parsedData, 0600); err != nil {
		t.Fatalf("failed to write parsed cache fixture: %v", err)
	}

	pkgs, err := ParseRepositoryMetadata(server.URL, primaryHref, nil)
	if err != nil {
		t.Fatalf("ParseRepositoryMetadata() should fall back to verified raw metadata: %v", err)
	}
	if len(pkgs) != 1 || pkgs[0].PkgName != "bash" {
		t.Fatalf("packages = %+v, want verified raw bash metadata", pkgs)
	}
	if atomic.LoadInt32(&requestCount) != 0 {
		t.Fatalf("HTTP requests = %d, want 0", atomic.LoadInt32(&requestCount))
	}
}

func TestRPMMetadataCacheDoesNotPersistOrFormatCredentials(t *testing.T) {
	setRPMMetadataTestCache(t)

	baseURL := "https://user:secret-token@example.invalid/repo"
	primaryHref := "repodata/primary.xml.gz"
	cacheDir := seedRPMRawMetadataCache(t, baseURL, primaryHref, compressGzip(t, testPrimaryXML("bash")))
	_, metaPath := rpmRawMetadataCachePaths(cacheDir, primaryHref)
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("failed to read raw metadata manifest: %v", err)
	}
	for _, secret := range []string{"user", "secret-token"} {
		if strings.Contains(string(metaData), secret) {
			t.Fatalf("raw metadata manifest contains credential fragment %q: %s", secret, string(metaData))
		}
	}

	redacted := redactURLForLog("https://user:secret-token@example.invalid/repo?token=abc123#frag")
	for _, secret := range []string{"user", "secret-token", "token=abc123", "frag"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("redacted URL %q still contains secret fragment %q", redacted, secret)
		}
	}
	if got := redactURLForLog("%zz://user:secret-token@example.invalid/repo?token=abc123#frag"); got != "<redacted-url>" {
		t.Fatalf("redactURLForLog() malformed URL = %q, want placeholder", got)
	}
}

func TestRPMRawMetadataCacheWriteFailureIsReported(t *testing.T) {
	setRPMMetadataTestCache(t)

	primaryHref := "repodata/primary.xml.gz"
	data := compressGzip(t, testPrimaryXML("bash"))
	primary := primaryRefForData(primaryHref, data)
	cacheRoot := t.TempDir()
	cacheDir := filepath.Join(cacheRoot, "not-a-directory")
	if err := os.WriteFile(cacheDir, []byte("x"), 0644); err != nil {
		t.Fatalf("failed to create file in place of cache directory: %v", err)
	}

	err := saveRPMRawMetadataCache(cacheDir, primaryHref, rpmMetadataID("https://example.invalid/repo", primaryHref), primary, data)
	if err == nil {
		t.Fatalf("expected cache write failure")
	}
	if !strings.Contains(err.Error(), "failed to write rpm raw metadata") {
		t.Fatalf("error = %q, want raw metadata write context", err.Error())
	}
}

func TestRPMRawMetadataConcurrentWritersKeepCoherentCache(t *testing.T) {
	setRPMMetadataTestCache(t)

	baseURL := "https://repo.example.invalid/base"
	primaryHref := "repodata/primary.xml.gz"
	dataA := compressGzip(t, testPrimaryXML("bash"))
	primaryA := primaryRefForData(primaryHref, dataA)
	dataB := compressGzip(t, testPrimaryXML("coreutils"))
	primaryB := primaryRefForData(primaryHref, dataB)
	cacheDir, err := rpmMetadataCacheDir(baseURL)
	if err != nil {
		t.Fatalf("rpmMetadataCacheDir() error = %v", err)
	}
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("failed to create cache dir: %v", err)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				errCh <- saveRPMRawMetadataCache(cacheDir, primaryHref, rpmMetadataID(baseURL, primaryHref), primaryA, dataA)
				return
			}
			errCh <- saveRPMRawMetadataCache(cacheDir, primaryHref, rpmMetadataID(baseURL, primaryHref), primaryB, dataB)
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent cache save failed: %v", err)
		}
	}

	_, metaPath := rpmRawMetadataCachePaths(cacheDir, primaryHref)
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read raw metadata manifest after concurrent writes: %v", err)
	}
	var cache rpmRawMetadataCache
	if err := json.Unmarshal(metaData, &cache); err != nil {
		t.Fatalf("unmarshal raw metadata manifest after concurrent writes: %v", err)
	}
	got, _, err := loadRPMRawMetadataCache(cacheDir, primaryHref, rpmMetadataID(baseURL, primaryHref), cache.Primary)
	if err != nil {
		t.Fatalf("loadRPMRawMetadataCache() after concurrent writes error = %v", err)
	}
	if !bytes.Equal(got, dataA) && !bytes.Equal(got, dataB) {
		t.Fatalf("cached data does not match any concurrently written revision")
	}
}

func TestRPMMetadataRawCacheSupportsZstd(t *testing.T) {
	setRPMMetadataTestCache(t)

	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	primaryHref := "repodata/primary.xml.zst"
	seedRPMRawMetadataCache(t, server.URL, primaryHref, compressZstd(t, testPrimaryXML("bash")))

	pkgs, err := ParseRepositoryMetadata(server.URL, primaryHref, nil)
	if err != nil {
		t.Fatalf("ParseRepositoryMetadata() zstd cache error = %v", err)
	}
	if len(pkgs) != 1 || pkgs[0].PkgName != "bash" {
		t.Fatalf("zstd cached packages = %+v, want bash", pkgs)
	}
	if atomic.LoadInt32(&requestCount) != 0 {
		t.Fatalf("HTTP requests = %d, want 0", atomic.LoadInt32(&requestCount))
	}
}

func TestRPMMetadataCacheSeparatesRepositoriesWithSameBasename(t *testing.T) {
	setRPMMetadataTestCache(t)

	repos := []struct {
		baseURL string
		pkgName string
	}{
		{baseURL: "https://repo.example.invalid/base", pkgName: "bash"},
		{baseURL: "https://repo.example.invalid/updates", pkgName: "coreutils"},
	}
	primaryHref := "repodata/primary.xml.gz"
	for _, repo := range repos {
		seedRPMRawMetadataCache(t, repo.baseURL, primaryHref, compressGzip(t, testPrimaryXML(repo.pkgName)))
	}

	for _, repo := range repos {
		pkgs, err := ParseRepositoryMetadata(repo.baseURL, primaryHref, nil)
		if err != nil {
			t.Fatalf("ParseRepositoryMetadata(%s) error = %v", repo.baseURL, err)
		}
		if len(pkgs) != 1 || pkgs[0].PkgName != repo.pkgName {
			t.Fatalf("packages for %s = %+v, want %s", repo.baseURL, pkgs, repo.pkgName)
		}
	}
}

func TestDownloadPackagesCompleteUsesMetadataAndPackageCachesWithoutHTTP(t *testing.T) {
	setRPMMetadataTestCache(t)

	origRepoCfg := RepoCfg
	origGzHref := GzHref
	origUserRepo := UserRepo
	t.Cleanup(func() {
		RepoCfg = origRepoCfg
		GzHref = origGzHref
		UserRepo = origUserRepo
	})

	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	primaryHref := "repodata/primary.xml.gz"
	seedRPMRawMetadataCache(t, server.URL, primaryHref, compressGzip(t, testPrimaryXML("bash")))
	RepoCfg = RepoConfig{URL: server.URL}
	GzHref = primaryHref
	UserRepo = nil

	pkgCacheDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(pkgCacheDir, "bash-1.0-1.x86_64.rpm"), []byte("cached rpm"), 0644); err != nil {
		t.Fatalf("failed to seed RPM payload cache: %v", err)
	}

	downloaded, infos, err := DownloadPackagesComplete([]string{"bash"}, pkgCacheDir, "", nil, false)
	if err != nil {
		t.Fatalf("DownloadPackagesComplete() cache hit error = %v", err)
	}
	if len(downloaded) != 1 || downloaded[0] != "bash-1.0-1.x86_64.rpm" {
		t.Fatalf("downloaded = %v, want cached bash rpm", downloaded)
	}
	if len(infos) != 1 || infos[0].Name != "bash" {
		t.Fatalf("infos = %+v, want cached bash info", infos)
	}
	if atomic.LoadInt32(&requestCount) != 0 {
		t.Fatalf("HTTP requests = %d, want 0", atomic.LoadInt32(&requestCount))
	}
}
