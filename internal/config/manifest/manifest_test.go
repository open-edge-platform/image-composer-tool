package manifest

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/config/version"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

func TestWriteSPDXToFile(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := t.TempDir()

	// Create the output file path directly in tmpDir (no subdirectory)
	outFile := filepath.Join(tmpDir, "sbom.spdx.json")

	pkgs := []ospackage.PackageInfo{
		{
			Name:        "samplepkg",
			Type:        "rpm",
			Version:     "1.0.0",
			URL:         "https://openedgeplatform.com/samplepkg.rpm",
			Description: "Sample package",
			License:     "Apache-2.0",
			Origin:      "Intel",
			Checksums: []ospackage.Checksum{
				{Algorithm: "sha256", Value: "abcd1234abcd1234abcd1234"},
			},
		},
	}

	err := WriteSPDXToFile(pkgs, outFile)
	if err != nil {
		t.Fatalf("WriteSPDXToFile failed: %v", err)
	}

	// Verify file exists
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("Failed to read SPDX output: %v", err)
	}

	// Unmarshal to validate structure
	var doc SPDXDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("Failed to parse SPDX JSON: %v", err)
	}

	if len(doc.Packages) != 1 {
		t.Errorf("Expected 1 package, got %d", len(doc.Packages))
	}

	p := doc.Packages[0]
	if p.Name != "samplepkg" {
		t.Errorf("Expected package name 'samplepkg', got %q", p.Name)
	}
	if p.Type != "rpm" {
		t.Errorf("Expected type 'rpm', got %q", p.Type)
	}
	if !strings.HasPrefix(doc.DocumentName, version.Toolname) {
		t.Errorf("Expected document name to start with tool name prefix, got %q", doc.DocumentName)
	}
	if len(p.Checksum) != 1 || p.Checksum[0].Algorithm != "SHA256" {
		t.Errorf("Expected SHA256 checksum, got %+v", p.Checksum)
	}
}

// Alternative test that creates subdirectories to match the original behavior
func TestWriteSPDXToFile_WithSubdirectory(t *testing.T) {
	tmpDir := t.TempDir()

	// Create output file path with subdirectory (like the original test)
	outFile := filepath.Join(tmpDir, "subdir", "sbom.spdx.json")

	pkgs := []ospackage.PackageInfo{
		{
			Name:        "testpkg",
			Type:        "deb",
			Version:     "2.0.0",
			URL:         "https://example.com/testpkg.deb",
			Description: "Test package with subdirectory",
			License:     "MIT",
			Origin:      "Test Organization",
			Checksums: []ospackage.Checksum{
				{Algorithm: "md5", Value: "d41d8cd98f00b204e9800998ecf8427e"},
			},
		},
	}

	err := WriteSPDXToFile(pkgs, outFile)
	if err != nil {
		t.Fatalf("WriteSPDXToFile with subdirectory failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(outFile); err != nil {
		t.Fatalf("Output file was not created: %v", err)
	}

	// Verify content
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("Failed to read SPDX output: %v", err)
	}

	var doc SPDXDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("Failed to parse SPDX JSON: %v", err)
	}

	if len(doc.Packages) != 1 {
		t.Errorf("Expected 1 package, got %d", len(doc.Packages))
	}

	p := doc.Packages[0]
	if p.Name != "testpkg" {
		t.Errorf("Expected package name 'testpkg', got %q", p.Name)
	}
}
func TestFallbackToDefault(t *testing.T) {
	tests := []struct {
		val      string
		fallback string
		want     string
	}{
		{"", "fallback", "fallback"},
		{"value", "fallback", "value"},
	}
	for _, tt := range tests {
		got := fallbackToDefault(tt.val, tt.fallback)
		if got != tt.want {
			t.Errorf("fallbackToDefault(%q, %q) = %q; want %q", tt.val, tt.fallback, got, tt.want)
		}
	}
}

func TestGenerateDocumentNamespace(t *testing.T) {
	ns1 := generateDocumentNamespace()
	ns2 := generateDocumentNamespace()
	if ns1 == ns2 {
		t.Errorf("Expected different namespaces, got %q and %q", ns1, ns2)
	}
	if !strings.HasPrefix(ns1, SPDXNamespaceBase+"/") {
		t.Errorf("Namespace does not start with SPDXNamespaceBase: %q", ns1)
	}
}

func TestSpdxSupplier(t *testing.T) {
	tests := []struct {
		origin string
		want   string
	}{
		{"", "NOASSERTION"},
		{"Intel", "Organization: Intel"},
		{"John Doe <john@example.com>", "Person: John Doe (john@example.com)"},
		{"Acme Corp", "Organization: Acme Corp"},
		{"Jane <jane@corp.com>", "Person: Jane (jane@corp.com)"},
		{"  ", "NOASSERTION"},
	}
	for _, tt := range tests {
		got := spdxSupplier(tt.origin)
		if got != tt.want {
			t.Errorf("spdxSupplier(%q) = %q; want %q", tt.origin, got, tt.want)
		}
	}
}

func TestWriteManifestToFile_Success(t *testing.T) {
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "manifest.json")
	manifest := SoftwarePackageManifest{
		SchemaVersion:     "1.0",
		ImageVersion:      "v1.2.3",
		BuiltAt:           "2024-01-01T00:00:00Z",
		Arch:              "amd64",
		SizeBytes:         123456,
		Hash:              "deadbeef",
		HashAlg:           "sha256",
		Signature:         "sig",
		SigAlg:            "rsa",
		MinCurrentVersion: "v1.0.0",
	}
	err := WriteManifestToFile(manifest, outFile)
	if err != nil {
		t.Fatalf("WriteManifestToFile failed: %v", err)
	}
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("Failed to read manifest file: %v", err)
	}
	var got SoftwarePackageManifest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Failed to unmarshal manifest: %v", err)
	}
	if got.ImageVersion != manifest.ImageVersion {
		t.Errorf("Expected ImageVersion %q, got %q", manifest.ImageVersion, got.ImageVersion)
	}
}

func TestWriteManifestToFile_InvalidPath(t *testing.T) {
	// Try to write to a directory that doesn't exist and can't be created
	// On Unix, /root/ is usually not writable by non-root users
	badPath := "/root/should_not_exist/manifest.json"
	manifest := SoftwarePackageManifest{}
	err := WriteManifestToFile(manifest, badPath)
	if err == nil {
		t.Errorf("Expected error when writing to unwritable path")
	}
}

func TestWriteSPDXToFile_InvalidChecksumAlgorithm(t *testing.T) {
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "sbom.spdx.json")
	pkgs := []ospackage.PackageInfo{
		{
			Name:        "pkg",
			Type:        "deb",
			Version:     "1.0",
			URL:         "https://example.com/pkg.deb",
			Description: "desc",
			License:     "MIT",
			Origin:      "Org",
			Checksums: []ospackage.Checksum{
				{Algorithm: "sha512", Value: "notused"},
				{Algorithm: "sha256", Value: "used"},
			},
		},
	}
	err := WriteSPDXToFile(pkgs, outFile)
	if err != nil {
		t.Fatalf("WriteSPDXToFile failed: %v", err)
	}
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("Failed to read SPDX output: %v", err)
	}
	var doc SPDXDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("Failed to parse SPDX JSON: %v", err)
	}
	if len(doc.Packages) != 1 {
		t.Fatalf("Expected 1 package, got %d", len(doc.Packages))
	}
	if len(doc.Packages[0].Checksum) != 1 {
		t.Errorf("Expected only 1 valid checksum, got %d", len(doc.Packages[0].Checksum))
	}
	if doc.Packages[0].Checksum[0].Algorithm != "SHA256" {
		t.Errorf("Expected SHA256 checksum, got %q", doc.Packages[0].Checksum[0].Algorithm)
	}
}

func TestWriteSPDXToFile_MissingFields(t *testing.T) {
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "sbom.spdx.json")
	pkgs := []ospackage.PackageInfo{
		{
			Name: "empty",
		},
	}
	err := WriteSPDXToFile(pkgs, outFile)
	if err != nil {
		t.Fatalf("WriteSPDXToFile failed: %v", err)
	}
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("Failed to read SPDX output: %v", err)
	}
	var doc SPDXDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("Failed to parse SPDX JSON: %v", err)
	}
	if len(doc.Packages) != 1 {
		t.Fatalf("Expected 1 package, got %d", len(doc.Packages))
	}
	p := doc.Packages[0]
	if p.LicenseDeclared != "NOASSERTION" {
		t.Errorf("Expected LicenseDeclared to be NOASSERTION, got %q", p.LicenseDeclared)
	}
	if p.Supplier != "NOASSERTION" {
		t.Errorf("Expected Supplier to be NOASSERTION, got %q", p.Supplier)
	}
}

func TestWriteMergedSPDXToFile_AddsUpgradesAndPreservesBaseline(t *testing.T) {
	tmpDir := t.TempDir()

	// A baseline SBOM with two packages, one carrying rich metadata (checksum,
	// supplier) the merge must preserve for untouched entries.
	baselineDoc := SPDXDocument{
		SPDXVersion:       SPDXVersion,
		DataLicense:       SPDXDataLicense,
		SPDXID:            SPDXDocumentID,
		DocumentName:      "baseline-doc",
		DocumentNamespace: "https://example.com/baseline-namespace",
		Packages: []SPDXPackage{
			{
				SPDXID:      "SPDXRef-Package-libc6",
				Name:        "libc6",
				Type:        "deb",
				VersionInfo: "2.39-0ubuntu8",
				Supplier:    "Organization: Ubuntu",
				Checksum:    []SPDXChecksum{{Algorithm: "SHA256", ChecksumValue: "baselinelibc"}},
			},
			{
				SPDXID:      "SPDXRef-Package-curl",
				Name:        "curl",
				Type:        "deb",
				VersionInfo: "8.5.0-1", // older; the overlay upgrades this
			},
		},
	}
	baselineData, err := json.Marshal(baselineDoc)
	if err != nil {
		t.Fatalf("marshal baseline: %v", err)
	}

	overlayPkgs := []ospackage.PackageInfo{
		// Upgrade: same name as a baseline package, newer version.
		{Name: "curl", Type: "deb", Version: "8.5.0-2ubuntu10.10", URL: "https://x/curl.deb"},
		// Addition: a name not in the baseline.
		{Name: "cups", Type: "deb", Version: "2.4.7-1.2ubuntu7.14", URL: "https://x/cups.deb"},
	}

	outFile := filepath.Join(tmpDir, "merged.json")
	if err := WriteMergedSPDXToFile(baselineData, overlayPkgs, outFile); err != nil {
		t.Fatalf("WriteMergedSPDXToFile failed: %v", err)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read merged SBOM: %v", err)
	}
	var doc SPDXDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse merged SBOM: %v", err)
	}

	// baseline header preserved (lineage unchanged).
	if doc.DocumentName != "baseline-doc" || doc.DocumentNamespace != "https://example.com/baseline-namespace" {
		t.Errorf("baseline header not preserved: name=%q namespace=%q", doc.DocumentName, doc.DocumentNamespace)
	}

	// 2 baseline + 1 addition (curl upgraded in place, not duplicated) = 3.
	if len(doc.Packages) != 3 {
		t.Fatalf("expected 3 packages after merge, got %d: %+v", len(doc.Packages), doc.Packages)
	}

	byName := make(map[string]SPDXPackage, len(doc.Packages))
	for _, p := range doc.Packages {
		byName[p.Name] = p
	}

	// Untouched baseline package keeps its full metadata.
	if lc := byName["libc6"]; lc.VersionInfo != "2.39-0ubuntu8" || lc.Supplier != "Organization: Ubuntu" ||
		len(lc.Checksum) != 1 || lc.Checksum[0].ChecksumValue != "baselinelibc" {
		t.Errorf("baseline libc6 metadata not preserved: %+v", lc)
	}

	// Upgraded package reflects the overlay's version and URL, exactly once.
	if c := byName["curl"]; c.VersionInfo != "8.5.0-2ubuntu10.10" || c.DownloadLocation != "https://x/curl.deb" {
		t.Errorf("curl not upgraded to overlay version: %+v", c)
	}

	// Added package present.
	if _, ok := byName["cups"]; !ok {
		t.Errorf("added package cups missing from merged SBOM")
	}
}

func TestWriteMergedSPDXToFile_AmbiguousMultiEntryNameNotReplaced(t *testing.T) {
	tmpDir := t.TempDir()

	// A baseline with two entries sharing the name "libc6" (a multiarch install:
	// amd64 + i386). There is no unique entry to upgrade, so the merge must NOT
	// arbitrarily overwrite one of them; it must append the overlay package and
	// keep both baseline entries intact.
	baselineDoc := SPDXDocument{
		SPDXVersion:       SPDXVersion,
		DataLicense:       SPDXDataLicense,
		SPDXID:            SPDXDocumentID,
		DocumentName:      "baseline-doc",
		DocumentNamespace: "https://example.com/baseline-namespace",
		Packages: []SPDXPackage{
			{
				SPDXID:           "SPDXRef-Package-libc6-amd64",
				Name:             "libc6",
				Type:             "deb",
				VersionInfo:      "2.39-0ubuntu8",
				DownloadLocation: "https://x/libc6_amd64.deb",
			},
			{
				SPDXID:           "SPDXRef-Package-libc6-i386",
				Name:             "libc6",
				Type:             "deb",
				VersionInfo:      "2.39-0ubuntu8",
				DownloadLocation: "https://x/libc6_i386.deb",
			},
		},
	}
	baselineData, err := json.Marshal(baselineDoc)
	if err != nil {
		t.Fatalf("marshal baseline: %v", err)
	}

	overlayPkgs := []ospackage.PackageInfo{
		{Name: "libc6", Type: "deb", Version: "2.40-1", URL: "https://x/libc6_new.deb"},
	}

	outFile := filepath.Join(tmpDir, "merged.json")
	if err := WriteMergedSPDXToFile(baselineData, overlayPkgs, outFile); err != nil {
		t.Fatalf("WriteMergedSPDXToFile failed: %v", err)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read merged SBOM: %v", err)
	}
	var doc SPDXDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse merged SBOM: %v", err)
	}

	// Both baseline entries preserved + the overlay package appended = 3. If the
	// merge had collapsed the ambiguous name it would show 2 (one silently lost).
	if len(doc.Packages) != 3 {
		t.Fatalf("expected 3 packages (2 baseline libc6 + 1 appended), got %d: %+v", len(doc.Packages), doc.Packages)
	}

	var libc6Count int
	baselineLocations := map[string]bool{}
	for _, p := range doc.Packages {
		if p.Name == "libc6" {
			libc6Count++
			baselineLocations[p.DownloadLocation] = true
		}
	}
	if libc6Count != 3 {
		t.Errorf("expected 3 libc6 entries after append, got %d", libc6Count)
	}
	// Both original download locations must survive (neither overwritten).
	if !baselineLocations["https://x/libc6_amd64.deb"] || !baselineLocations["https://x/libc6_i386.deb"] {
		t.Errorf("a baseline libc6 entry was overwritten: %+v", baselineLocations)
	}
	if !baselineLocations["https://x/libc6_new.deb"] {
		t.Errorf("overlay libc6 entry was not appended: %+v", baselineLocations)
	}
}

func TestWriteMergedSPDXToFile_RejectsMalformedBaseline(t *testing.T) {
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "merged.json")

	err := WriteMergedSPDXToFile([]byte("not json"), nil, outFile)
	if err == nil {
		t.Fatalf("expected error for malformed baseline SBOM")
	}
	if _, statErr := os.Stat(outFile); statErr == nil {
		t.Errorf("no output file should be written when the baseline is unparseable")
	}
}

func TestCopySBOMToChroot_Success(t *testing.T) {
	// Create temporary chroot directory
	chrootDir := t.TempDir()

	// Use config.TempDir() to get the actual temp directory where SBOM is expected
	tempDir := config.TempDir()

	// Ensure temp directory exists
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}

	// Create source SBOM file in the expected location
	srcSBOM := filepath.Join(tempDir, DefaultSPDXFile)
	testData := []byte(`{"test": "data"}`)
	if err := os.WriteFile(srcSBOM, testData, 0644); err != nil {
		t.Fatalf("Failed to create source SBOM: %v", err)
	}
	// Clean up the source SBOM after test
	defer os.Remove(srcSBOM)

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "mkdir", Output: "override-test\n", Error: nil},
		{Pattern: "cp", Output: "override-test\n", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	// Call the function
	err := CopySBOMToChroot(chrootDir)
	if err != nil {
		t.Fatalf("CopySBOMToChroot failed: %v", err)
	}
}

func TestCopySBOMToChroot_MissingSourceSBOM(t *testing.T) {
	// Create temporary chroot directory
	chrootDir := t.TempDir()

	// Ensure source SBOM does NOT exist by checking and removing if present
	srcSBOM := filepath.Join(config.TempDir(), DefaultSPDXFile)
	os.Remove(srcSBOM) // Remove if it exists from previous tests

	// Should not fail, just log warning and return nil
	err := CopySBOMToChroot(chrootDir)
	if err != nil {
		t.Errorf("CopySBOMToChroot should not fail when source SBOM is missing, got error: %v", err)
	}

	// Verify no SBOM was created in chroot
	dstSBOM := filepath.Join(chrootDir, ImageSBOMPath, DefaultSPDXFile)
	if _, err := os.Stat(dstSBOM); !os.IsNotExist(err) {
		t.Errorf("SBOM should not exist in chroot when source is missing")
	}
}

func TestCopySBOMToChroot_InvalidChrootPath(t *testing.T) {
	// Skip this test if running as root (e.g., in Docker/Earthly containers)
	// because root can write to read-only directories
	if os.Geteuid() == 0 {
		t.Skip("Skipping test when running as root (permission checks don't apply)")
	}

	// Skip this test if user has passwordless sudo capabilities
	// because CopySBOMToChroot uses sudo for file operations
	cmd := exec.Command("sudo", "-n", "true")
	if err := cmd.Run(); err == nil {
		t.Skip("Skipping test when user has passwordless sudo (permission checks don't apply)")
	}

	// Create a directory and make it read-only to simulate permission issues
	tmpDir := t.TempDir()
	invalidPath := filepath.Join(tmpDir, "readonly")

	// Create the directory
	if err := os.MkdirAll(invalidPath, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Make it read-only (no write permissions)
	if err := os.Chmod(invalidPath, 0555); err != nil {
		t.Fatalf("Failed to change directory permissions: %v", err)
	}
	// Restore permissions after test for cleanup
	defer func() {
		_ = os.Chmod(invalidPath, 0755) // Ignore error on cleanup
	}()

	// Create source SBOM file
	tempDir := config.TempDir()

	// Ensure temp directory exists
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}

	srcSBOM := filepath.Join(tempDir, DefaultSPDXFile)
	if err := os.WriteFile(srcSBOM, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create source SBOM: %v", err)
	}
	defer os.Remove(srcSBOM)

	// Should return an error when trying to create subdirectory in read-only dir
	err := CopySBOMToChroot(invalidPath)
	if err == nil {
		t.Errorf("Expected error when copying to read-only chroot path, got nil")
	}
}

func TestImageSBOMPathConstant(t *testing.T) {
	// Verify the constant is set correctly
	expectedPath := "/usr/share/sbom"
	if ImageSBOMPath != expectedPath {
		t.Errorf("Expected ImageSBOMPath to be %q, got %q", expectedPath, ImageSBOMPath)
	}
}
func TestCopySBOMToImageBuildDir(t *testing.T) {
	// Setup temp dirs
	tempDir := t.TempDir()
	buildDir := t.TempDir()

	// Save original global config
	originalGlobal := config.Global()
	defer config.SetGlobal(originalGlobal)

	// Set new global config with temp dir
	newGlobal := config.DefaultGlobalConfig()
	newGlobal.TempDir = tempDir
	config.SetGlobal(newGlobal)

	// Create dummy SBOM in temp dir
	sbomPath := filepath.Join(tempDir, DefaultSPDXFile)
	if err := os.WriteFile(sbomPath, []byte("{}"), 0644); err != nil {
		t.Fatalf("Failed to create dummy SBOM: %v", err)
	}

	// Test success case
	if err := CopySBOMToImageBuildDir(buildDir); err != nil {
		t.Fatalf("CopySBOMToImageBuildDir failed: %v", err)
	}

	// Verify file exists in build dir
	dstPath := filepath.Join(buildDir, DefaultSPDXFile)
	if _, err := os.Stat(dstPath); os.IsNotExist(err) {
		t.Errorf("SBOM not copied to build dir")
	}

	// Test missing source SBOM
	os.Remove(sbomPath)
	if err := CopySBOMToImageBuildDir(buildDir); err != nil {
		t.Fatalf("CopySBOMToImageBuildDir failed with missing source: %v", err)
	}
	// Should just log warning and return nil
}
