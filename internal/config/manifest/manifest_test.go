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
	if err := WriteMergedSPDXToFile(baselineData, overlayPkgs, nil, outFile); err != nil {
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

// TestWriteMergedSPDXToFile_DropsRemovedPackages asserts that packages named in
// removedNames are dropped from the merged (complete) inventory, so a
// conflict-driven removal is reflected rather than the pre-removal baseline.
func TestWriteMergedSPDXToFile_DropsRemovedPackages(t *testing.T) {
	tmpDir := t.TempDir()

	baselineDoc := SPDXDocument{
		SPDXVersion: SPDXVersion, DataLicense: SPDXDataLicense, SPDXID: SPDXDocumentID,
		DocumentName: "baseline-doc", DocumentNamespace: "https://example.com/ns",
		Packages: []SPDXPackage{
			{SPDXID: "SPDXRef-Package-initramfs-tools", Name: "initramfs-tools", Type: "deb", VersionInfo: "0.142"},
			{SPDXID: "SPDXRef-Package-libc6", Name: "libc6", Type: "deb", VersionInfo: "2.39"},
		},
	}
	baselineData, err := json.Marshal(baselineDoc)
	if err != nil {
		t.Fatalf("marshal baseline: %v", err)
	}

	// dracut is added; initramfs-tools is removed (the conflict-driven removal).
	overlayPkgs := []ospackage.PackageInfo{{Name: "dracut", Type: "deb", Version: "060"}}
	outFile := filepath.Join(tmpDir, "merged.json")
	if err := WriteMergedSPDXToFile(baselineData, overlayPkgs, []string{"initramfs-tools"}, outFile); err != nil {
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
	names := make(map[string]bool, len(doc.Packages))
	for _, p := range doc.Packages {
		names[p.Name] = true
	}
	if names["initramfs-tools"] {
		t.Error("removed package initramfs-tools must not appear in the complete SBOM")
	}
	if !names["libc6"] || !names["dracut"] {
		t.Errorf("expected libc6 (kept) and dracut (added) in the complete SBOM, got %v", names)
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
	if err := WriteMergedSPDXToFile(baselineData, overlayPkgs, nil, outFile); err != nil {
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

	err := WriteMergedSPDXToFile([]byte("not json"), nil, nil, outFile)
	if err == nil {
		t.Fatalf("expected error for malformed baseline SBOM")
	}
	if _, statErr := os.Stat(outFile); statErr == nil {
		t.Errorf("no output file should be written when the baseline is unparseable")
	}
}

// TestWriteMergedSPDXToFile_RejectsNonSPDXBase asserts that a base which is valid
// JSON but NOT SPDX (a CycloneDX document, an empty object, or one declaring a
// non-2.x spdxVersion) is rejected rather than silently merged into an all-zero
// document — so the caller runs its documented external -> inherited -> delta-only
// fallback instead of emitting a bogus "complete" SBOM containing only the overlay
// packages. No output file is written on rejection.
func TestWriteMergedSPDXToFile_RejectsNonSPDXBase(t *testing.T) {
	tmpDir := t.TempDir()
	overlayPkgs := []ospackage.PackageInfo{{Name: "curl", Version: "8.0", Type: "deb"}}
	cases := map[string]string{
		"empty object":          `{}`,
		"cyclonedx-like":        `{"bomFormat":"CycloneDX","specVersion":"1.5","components":[{"name":"libc"}]}`,
		"packages without name": `{"packages":[{"versionInfo":"1.0"}]}`,
		"wrong spdxVersion":     `{"spdxVersion":"SPDX-3.0","packages":[{"name":"libc"}]}`,
		// Malformed 2.x versions that a prefix match would wrongly accept: only concrete
		// published SPDX 2.x versions are allowed, so these are rejected.
		"malformed 2.x version (SPDX-2.bad)":   `{"spdxVersion":"SPDX-2.bad","packages":[{"name":"libc"}]}`,
		"unpublished 2.x version (SPDX-2.999)": `{"spdxVersion":"SPDX-2.999","packages":[{"name":"libc"}]}`,
		// An SPDX-2.x header does NOT excuse a nameless inventory: the package-name
		// check runs regardless of the version header.
		"spdx2 header but nameless packages": `{"spdxVersion":"SPDX-2.3","packages":[{"versionInfo":"1"}]}`,
		"spdx2 header but no packages":       `{"spdxVersion":"SPDX-2.3","packages":[]}`,
		// EVERY package must be named: one valid package plus one nameless record is
		// still rejected (an SPDX package name is mandatory).
		"one named plus one nameless": `{"spdxVersion":"SPDX-2.3","packages":[{"name":"libc6"},{"versionInfo":"9"}]}`,
	}
	for name, base := range cases {
		t.Run(name, func(t *testing.T) {
			outFile := filepath.Join(tmpDir, "merged-"+strings.ReplaceAll(name, " ", "-")+".json")
			err := WriteMergedSPDXToFile([]byte(base), overlayPkgs, nil, outFile)
			if err == nil {
				t.Fatalf("expected a non-SPDX base to be rejected: %s", base)
			}
			if _, statErr := os.Stat(outFile); statErr == nil {
				t.Error("no output file should be written when the base is not usable SPDX")
			}
		})
	}
}

// TestWriteMergedSPDXToFile_SynthesizesEmptyPackageIDs asserts that a preserved
// base package with an EMPTY SPDXID is given a stable, non-empty, spec-valid ID
// before the DESCRIBES relationships are generated, so no relationship carries an
// empty relatedSpdxElement.
func TestWriteMergedSPDXToFile_SynthesizesEmptyPackageIDs(t *testing.T) {
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "merged.json")

	// A valid SPDX base (spdxVersion present) whose package omits SPDXID.
	base := []byte(`{"spdxVersion":"SPDX-2.3","packages":[{"name":"libc6","versionInfo":"2.39","downloadLocation":"NOASSERTION"}]}`)
	if err := WriteMergedSPDXToFile(base, nil, nil, outFile); err != nil {
		t.Fatalf("WriteMergedSPDXToFile failed: %v", err)
	}
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read merged SBOM: %v", err)
	}
	var doc SPDXDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("merged SBOM is not valid JSON: %v", err)
	}
	for _, p := range doc.Packages {
		if strings.TrimSpace(p.SPDXID) == "" {
			t.Errorf("package %q has an empty SPDXID after merge", p.Name)
		}
	}
	// No DESCRIBES relationship may dangle to an empty element.
	for _, r := range doc.Relationships {
		if strings.TrimSpace(r.RelatedSPDXElement) == "" {
			t.Errorf("relationship has an empty relatedSpdxElement: %+v", r)
		}
	}
}

// TestWriteMergedSPDXToFile_NormalizesRequiredPackageFields asserts that a base
// package supplying only name/version (no downloadLocation or license fields — as
// the integration fixture does) is emitted with the SPDX-required fields filled to
// NOASSERTION, so the merged document validates as SPDX 2.3 rather than carrying
// empty required fields.
func TestWriteMergedSPDXToFile_NormalizesRequiredPackageFields(t *testing.T) {
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "merged.json")

	base := []byte(`{"spdxVersion":"SPDX-2.3","packages":[{"name":"libc6","versionInfo":"2.39"}]}`)
	if err := WriteMergedSPDXToFile(base, nil, nil, outFile); err != nil {
		t.Fatalf("WriteMergedSPDXToFile failed: %v", err)
	}
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read merged SBOM: %v", err)
	}
	var doc SPDXDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("merged SBOM is not valid JSON: %v", err)
	}
	if len(doc.Packages) != 1 {
		t.Fatalf("expected 1 package, got %d", len(doc.Packages))
	}
	p := doc.Packages[0]
	if p.DownloadLocation != "NOASSERTION" {
		t.Errorf("downloadLocation = %q, want NOASSERTION", p.DownloadLocation)
	}
	if p.LicenseConcluded != "NOASSERTION" {
		t.Errorf("licenseConcluded = %q, want NOASSERTION", p.LicenseConcluded)
	}
	if p.LicenseDeclared != "NOASSERTION" {
		t.Errorf("licenseDeclared = %q, want NOASSERTION", p.LicenseDeclared)
	}
	if strings.TrimSpace(p.SPDXID) == "" {
		t.Error("SPDXID must be synthesized, not empty")
	}
}

// TestWriteMergedSPDXToFile_NormalizesHeaderLightBaseline asserts that a
// parseable-but-header-light base SBOM (only a `packages` array, with no
// spdxVersion/dataLicense/SPDXID/namespace/creationInfo) yields a VALID SPDX 2.3
// document: the merge backfills every required header field rather than emitting a
// document that claims SPDX 2.3 with empty headers.
func TestWriteMergedSPDXToFile_NormalizesHeaderLightBaseline(t *testing.T) {
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "merged.json")

	// A base carrying only packages — every document-header field is absent.
	headerLight := []byte(`{"packages":[{"SPDXID":"SPDXRef-Package-libc6","name":"libc6","versionInfo":"2.39","downloadLocation":"NOASSERTION"}]}`)
	overlayPkgs := []ospackage.PackageInfo{{Name: "curl", Version: "8.0", Type: "deb"}}

	if err := WriteMergedSPDXToFile(headerLight, overlayPkgs, nil, outFile); err != nil {
		t.Fatalf("WriteMergedSPDXToFile failed: %v", err)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read merged SBOM: %v", err)
	}
	var doc SPDXDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("merged SBOM is not valid JSON: %v", err)
	}
	if doc.SPDXVersion != SPDXVersion {
		t.Errorf("spdxVersion = %q, want %q", doc.SPDXVersion, SPDXVersion)
	}
	if doc.DataLicense != SPDXDataLicense {
		t.Errorf("dataLicense = %q, want %q", doc.DataLicense, SPDXDataLicense)
	}
	if doc.SPDXID != SPDXDocumentID {
		t.Errorf("SPDXID = %q, want %q", doc.SPDXID, SPDXDocumentID)
	}
	if strings.TrimSpace(doc.DocumentNamespace) == "" {
		t.Error("documentNamespace must be filled")
	}
	if strings.TrimSpace(doc.CreationInfo.Created) == "" || len(doc.CreationInfo.Creators) == 0 {
		t.Errorf("creationInfo must be filled, got %+v", doc.CreationInfo)
	}
	// Both the base and the overlay package survive the merge.
	if len(doc.Packages) != 2 {
		t.Errorf("expected 2 packages (base libc6 + overlay curl), got %d", len(doc.Packages))
	}
}

// TestNormalizeSPDXHeader_PreservesExistingHeader asserts the backfill only fills
// empties: a document with a real header keeps its own lineage untouched.
func TestNormalizeSPDXHeader_PreservesExistingHeader(t *testing.T) {
	doc := SPDXDocument{
		SPDXVersion:       "SPDX-2.3",
		DataLicense:       "CC0-1.0",
		SPDXID:            "SPDXRef-DOCUMENT",
		DocumentName:      "my-existing-doc",
		DocumentNamespace: "https://example.com/my-namespace",
		CreationInfo:      CreationInfo{Created: "2020-01-01T00:00:00Z", Creators: []string{"Tool: existing"}},
	}
	normalizeSPDXHeader(&doc)
	if doc.DocumentName != "my-existing-doc" || doc.DocumentNamespace != "https://example.com/my-namespace" {
		t.Errorf("normalize must not overwrite an existing header, got name=%q ns=%q", doc.DocumentName, doc.DocumentNamespace)
	}
	if doc.CreationInfo.Created != "2020-01-01T00:00:00Z" || len(doc.CreationInfo.Creators) != 1 {
		t.Errorf("normalize must not overwrite existing creationInfo, got %+v", doc.CreationInfo)
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

func TestSanitizeSPDXID(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"libc6", "libc6"},
		{"libstdc++6", "libstdc--6"},
		{"g++", "g--"},
		{"lib.name-1", "lib.name-1"},
		{"weird name", "weird-name"},
	}
	for _, tc := range cases {
		if got := sanitizeSPDXID(tc.name); got != tc.want {
			t.Errorf("sanitizeSPDXID(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestBuildSPDXPackage_SanitizesSPDXIDAndEnrichesMetadata(t *testing.T) {
	pkg := ospackage.PackageInfo{
		Name:        "libstdc++6",
		Type:        "deb",
		Version:     "14.2.0-4ubuntu2",
		URL:         "http://archive.ubuntu.com/ubuntu/pool/main/g/gcc-14/libstdc++6_14.2.0-4ubuntu2_amd64.deb",
		Description: "GNU Standard C++ Library v3",
		Origin:      "Ubuntu Developers <ubuntu-devel-discuss@lists.ubuntu.com>",
		Checksums: []ospackage.Checksum{
			{Algorithm: "sha256", Value: "abc123"},
			{Algorithm: "SHA1", Value: "def456"},
		},
	}

	spdxPkg := buildSPDXPackage(pkg)

	if spdxPkg.SPDXID != "SPDXRef-Package-libstdc--6" {
		t.Errorf("SPDXID = %q, want spec-valid SPDXRef-Package-libstdc--6", spdxPkg.SPDXID)
	}
	if spdxPkg.Supplier != "Person: Ubuntu Developers (ubuntu-devel-discuss@lists.ubuntu.com)" {
		t.Errorf("Supplier = %q, want the Person form derived from Origin", spdxPkg.Supplier)
	}
	if spdxPkg.Description != "GNU Standard C++ Library v3" {
		t.Errorf("Description = %q, want the package description", spdxPkg.Description)
	}
	// Both checksums (algorithm normalized to upper-case) are carried.
	if len(spdxPkg.Checksum) != 2 {
		t.Fatalf("expected 2 checksums, got %d: %+v", len(spdxPkg.Checksum), spdxPkg.Checksum)
	}
}

func TestWriteMergedSPDXToFile_DisambiguatesAppendedSameNameSPDXID(t *testing.T) {
	tmpDir := t.TempDir()

	// A baseline whose single "tree" entry uses the canonical SPDXID an overlay
	// addition would also generate. Because the name carries a single entry, the
	// overlay "tree" upgrades it in place (no duplicate). To force an APPEND with a
	// colliding id, the baseline holds TWO entries for the name (ambiguous), so the
	// overlay package is appended and must receive a distinct SPDXID.
	baselineDoc := SPDXDocument{
		SPDXVersion:       SPDXVersion,
		DataLicense:       SPDXDataLicense,
		SPDXID:            SPDXDocumentID,
		DocumentName:      "baseline-doc",
		DocumentNamespace: "https://example.com/ns",
		Packages: []SPDXPackage{
			{SPDXID: "SPDXRef-Package-tree", Name: "tree", Type: "deb", VersionInfo: "2.1.1-1", DownloadLocation: "https://x/tree_amd64.deb"},
			{SPDXID: "SPDXRef-Package-tree-2", Name: "tree", Type: "deb", VersionInfo: "2.1.1-1", DownloadLocation: "https://x/tree_i386.deb"},
		},
	}
	baselineData, err := json.Marshal(baselineDoc)
	if err != nil {
		t.Fatalf("marshal baseline: %v", err)
	}

	overlayPkgs := []ospackage.PackageInfo{
		{Name: "tree", Type: "deb", Version: "2.1.1-2", URL: "https://x/tree_new.deb"},
	}

	outFile := filepath.Join(tmpDir, "merged.json")
	if err := WriteMergedSPDXToFile(baselineData, overlayPkgs, nil, outFile); err != nil {
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

	// Every SPDXID in the final document must be unique (valid SPDX).
	seen := make(map[string]bool, len(doc.Packages))
	for _, p := range doc.Packages {
		if seen[p.SPDXID] {
			t.Errorf("duplicate SPDXID %q in merged document: %+v", p.SPDXID, doc.Packages)
		}
		seen[p.SPDXID] = true
	}
	if len(doc.Packages) != 3 {
		t.Fatalf("expected 3 packages, got %d", len(doc.Packages))
	}
}

// TestWriteMergedSPDXToFile_SanitizesLegacyBaselineSPDXIDs asserts a baseline
// document written by an older tool version — whose SPDXID was built from the raw
// package name and so can contain invalid characters (e.g. "libstdc++6") — is
// normalized into the element-ID grammar at the write choke point, not just for
// the freshly built overlay packages. Without this the merged output stays
// non-conformant even after deduping.
func TestWriteMergedSPDXToFile_SanitizesLegacyBaselineSPDXIDs(t *testing.T) {
	tmpDir := t.TempDir()

	baselineDoc := SPDXDocument{
		SPDXVersion:       SPDXVersion,
		DataLicense:       SPDXDataLicense,
		SPDXID:            SPDXDocumentID,
		DocumentName:      "baseline-doc",
		DocumentNamespace: "https://example.com/ns",
		Packages: []SPDXPackage{
			{SPDXID: "SPDXRef-Package-libstdc++6", Name: "libstdc++6", Type: "deb", VersionInfo: "14.2.0-4", DownloadLocation: "https://x/libstdc++6.deb"},
			{SPDXID: "SPDXRef-Package-g++", Name: "g++", Type: "deb", VersionInfo: "4:14.2.0", DownloadLocation: "https://x/g++.deb"},
		},
	}
	baselineData, err := json.Marshal(baselineDoc)
	if err != nil {
		t.Fatalf("marshal baseline: %v", err)
	}

	outFile := filepath.Join(tmpDir, "merged.json")
	if err := WriteMergedSPDXToFile(baselineData, nil, nil, outFile); err != nil {
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

	// Every baseline SPDXID must be in the element-ID grammar; no "+" may survive.
	for _, p := range doc.Packages {
		if p.SPDXID != sanitizeSPDXID(p.SPDXID) {
			t.Errorf("baseline SPDXID %q was not sanitized into the element-ID grammar", p.SPDXID)
		}
	}
	byID := make(map[string]bool, len(doc.Packages))
	for _, p := range doc.Packages {
		byID[p.SPDXID] = true
	}
	if !byID["SPDXRef-Package-libstdc--6"] || !byID["SPDXRef-Package-g--"] {
		t.Errorf("expected sanitized IDs SPDXRef-Package-libstdc--6 and SPDXRef-Package-g--, got %+v", doc.Packages)
	}
}

// TestWriteSPDXToFile_EmitsDescribesRelationships asserts the written document
// carries one DESCRIBES relationship from the document root to every package,
// referencing the final (post-dedupe) package IDs — the minimum required for a
// spec-conformant SPDX document.
func TestWriteSPDXToFile_EmitsDescribesRelationships(t *testing.T) {
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "sbom.spdx.json")

	pkgs := []ospackage.PackageInfo{
		{Name: "alpha", Type: "deb", Version: "1.0", URL: "https://x/alpha.deb"},
		{Name: "beta", Type: "deb", Version: "2.0", URL: "https://x/beta.deb"},
	}

	if err := WriteSPDXToFile(pkgs, outFile); err != nil {
		t.Fatalf("WriteSPDXToFile failed: %v", err)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read SBOM: %v", err)
	}
	var doc SPDXDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse SBOM: %v", err)
	}

	if len(doc.Relationships) != len(doc.Packages) {
		t.Fatalf("expected %d relationships, got %d", len(doc.Packages), len(doc.Relationships))
	}
	for i, rel := range doc.Relationships {
		if rel.SPDXElementID != doc.SPDXID {
			t.Errorf("relationship %d: spdxElementId = %q, want document root %q", i, rel.SPDXElementID, doc.SPDXID)
		}
		if rel.RelationshipType != "DESCRIBES" {
			t.Errorf("relationship %d: type = %q, want DESCRIBES", i, rel.RelationshipType)
		}
		if rel.RelatedSPDXElement != doc.Packages[i].SPDXID {
			t.Errorf("relationship %d: relatedSpdxElement = %q, want %q", i, rel.RelatedSPDXElement, doc.Packages[i].SPDXID)
		}
	}
}

// TestWriteMergedSPDXToFile_RelationshipsReferenceDedupedIDs guards the ordering
// contract: relationships are built AFTER dedupeSPDXIDs, so a colliding overlay
// addition that gets a disambiguated ID must be the target of a relationship
// pointing at that final ID, never the pre-dedupe one.
func TestWriteMergedSPDXToFile_RelationshipsReferenceDedupedIDs(t *testing.T) {
	tmpDir := t.TempDir()

	baselineDoc := SPDXDocument{
		SPDXVersion:       SPDXVersion,
		DataLicense:       SPDXDataLicense,
		SPDXID:            SPDXDocumentID,
		DocumentName:      "baseline-doc",
		DocumentNamespace: "https://example.com/ns",
		Packages: []SPDXPackage{
			{SPDXID: "SPDXRef-Package-tree", Name: "tree", Type: "deb", VersionInfo: "2.1.1-1", DownloadLocation: "https://x/tree_amd64.deb"},
			{SPDXID: "SPDXRef-Package-tree-2", Name: "tree", Type: "deb", VersionInfo: "2.1.1-1", DownloadLocation: "https://x/tree_i386.deb"},
		},
	}
	baselineData, err := json.Marshal(baselineDoc)
	if err != nil {
		t.Fatalf("marshal baseline: %v", err)
	}

	overlayPkgs := []ospackage.PackageInfo{
		{Name: "tree", Type: "deb", Version: "2.1.1-2", URL: "https://x/tree_new.deb"},
	}

	outFile := filepath.Join(tmpDir, "merged.json")
	if err := WriteMergedSPDXToFile(baselineData, overlayPkgs, nil, outFile); err != nil {
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

	// One relationship per package, and every relationship must target an ID that
	// actually exists in the final package set (no dangling reference to a
	// pre-dedupe id).
	if len(doc.Relationships) != len(doc.Packages) {
		t.Fatalf("expected %d relationships, got %d", len(doc.Packages), len(doc.Relationships))
	}
	pkgIDs := make(map[string]bool, len(doc.Packages))
	for _, p := range doc.Packages {
		pkgIDs[p.SPDXID] = true
	}
	for i, rel := range doc.Relationships {
		if !pkgIDs[rel.RelatedSPDXElement] {
			t.Errorf("relationship %d targets %q which is not a package ID in the final document", i, rel.RelatedSPDXElement)
		}
	}
}
