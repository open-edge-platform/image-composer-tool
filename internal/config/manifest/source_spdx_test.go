package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
)

func TestWriteMergedSPDXWithSources(t *testing.T) {
	t.Parallel()

	baseline := SPDXDocument{
		SPDXVersion: SPDXVersion, DataLicense: SPDXDataLicense, SPDXID: SPDXDocumentID,
		DocumentName: "baseline", DocumentNamespace: "https://example.org/ns/baseline",
		Packages:      []SPDXPackage{{SPDXID: "SPDXRef-Package-baseline", Name: "baseline", VersionInfo: "1", DownloadLocation: "NOASSERTION", LicenseDeclared: "NOASSERTION", LicenseConcluded: "NOASSERTION"}},
		Relationships: []SPDXRelationship{{SPDXElementID: "SPDXRef-Package-baseline", RelationshipType: "CONTAINS", RelatedSPDXElement: "SPDXRef-ExternalFile"}},
	}
	baselineData, err := json.Marshal(baseline)
	if err != nil {
		t.Fatalf("marshal baseline: %v", err)
	}
	checksum := strings.Repeat("a", 64)
	sources := []ProvenanceSource{{
		Name: "gstreamer1.0", Version: "1.28.2", RepositoryID: "repo-a",
		ArtifactRetrieval: ProvenanceArtifactNotRequested,
		Artifacts:         []ProvenanceArtifact{{Filename: "gstreamer1.0.dsc", URL: "https://example.org/pool/gstreamer1.0.dsc", Size: 10, SHA256: checksum}},
	}}
	overlay := []ospackage.PackageInfo{{
		Name: "gstreamer1.0-tools", PkgName: "gstreamer1.0-tools", Type: "deb", Version: "1.28.2", Arch: "amd64",
		SourceName: "gstreamer1.0", SourceVersion: "1.28.2", SourceRepository: "repo-a", SourceMappingStatus: ProvenanceStatusResolved,
	}}

	out := filepath.Join(t.TempDir(), "complete.json")
	if err := WriteMergedSPDXWithSources(baselineData, overlay, nil, sources, out); err != nil {
		t.Fatalf("WriteMergedSPDXWithSources: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var doc SPDXDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decode output: %v", err)
	}

	var sourcePackage SPDXPackage
	foundSource := false
	for _, pkg := range doc.Packages {
		if pkg.Name == "gstreamer1.0" && pkg.Type == "source" {
			sourcePackage = pkg
			foundSource = true
		}
	}
	if !foundSource || sourcePackage.SPDXID == "" || sourcePackage.DownloadLocation != "https://example.org/pool/gstreamer1.0.dsc" || len(sourcePackage.Checksum) != 1 {
		t.Fatalf("source package = %+v, found=%v", sourcePackage, foundSource)
	}

	var generated bool
	for _, relationship := range doc.Relationships {
		if relationship.RelationshipType == "CONTAINS" && relationship.SPDXElementID == "SPDXRef-Package-baseline" {
			t.Errorf("baseline package relationship should be remapped or removed, got %+v", relationship)
		}
		if relationship.RelationshipType == "GENERATED_FROM" {
			generated = true
			if relationship.SPDXElementID != sourcePackage.SPDXID {
				t.Errorf("GENERATED_FROM element = %q, want source %q", relationship.SPDXElementID, sourcePackage.SPDXID)
			}
		}
	}
	if !generated {
		t.Error("resolved exact mapping did not emit GENERATED_FROM")
	}

	if first, err := os.ReadFile(out); err != nil || len(first) == 0 {
		t.Fatalf("stable output unavailable: %v", err)
	}
}

func TestWriteMergedSPDXWithSourcesNoUncertainGeneratedFrom(t *testing.T) {
	t.Parallel()

	baseline, err := json.Marshal(SPDXDocument{
		SPDXVersion: SPDXVersion, DataLicense: SPDXDataLicense, SPDXID: SPDXDocumentID,
		DocumentName: "baseline", DocumentNamespace: "https://example.org/ns/baseline",
		Packages: []SPDXPackage{{SPDXID: "SPDXRef-Package-base", Name: "base", DownloadLocation: "NOASSERTION", LicenseDeclared: "NOASSERTION", LicenseConcluded: "NOASSERTION"}},
	})
	if err != nil {
		t.Fatalf("marshal baseline: %v", err)
	}
	sources := []ProvenanceSource{{Name: "vendor-source", Version: "1", RepositoryID: "repo-a", ArtifactRetrieval: ProvenanceArtifactNotRequested}}
	overlay := []ospackage.PackageInfo{{Name: "base", PkgName: "base", Type: "deb", Version: "2", SourceName: "vendor-source", SourceVersion: "1", SourceRepository: "repo-a", SourceMappingStatus: ProvenanceStatusOriginUnknown}}

	out := filepath.Join(t.TempDir(), "uncertain.json")
	if err := WriteMergedSPDXWithSources(baseline, overlay, nil, sources, out); err != nil {
		t.Fatalf("WriteMergedSPDXWithSources: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var doc SPDXDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	for _, relationship := range doc.Relationships {
		if relationship.RelationshipType == "GENERATED_FROM" {
			t.Fatalf("uncertain mapping emitted GENERATED_FROM: %+v", relationship)
		}
	}
}

func TestWriteMergedSPDXWithSourcesDeduplicatesAndPreservesMultiarch(t *testing.T) {
	t.Parallel()

	baseline, err := json.Marshal(SPDXDocument{
		SPDXVersion: SPDXVersion, DataLicense: SPDXDataLicense, SPDXID: SPDXDocumentID,
		DocumentName: "baseline", DocumentNamespace: "https://example.org/ns/multiarch",
		Packages: []SPDXPackage{
			{SPDXID: "SPDXRef-Package-mesa-amd64", Name: "mesa-amd64", DownloadLocation: "NOASSERTION", LicenseDeclared: "NOASSERTION", LicenseConcluded: "NOASSERTION"},
			{SPDXID: "SPDXRef-Package-mesa-i386", Name: "mesa-i386", DownloadLocation: "NOASSERTION", LicenseDeclared: "NOASSERTION", LicenseConcluded: "NOASSERTION"},
		},
	})
	if err != nil {
		t.Fatalf("marshal baseline: %v", err)
	}
	sources := []ProvenanceSource{{Name: "mesa", Version: "26.0", RepositoryID: "repo-a", ArtifactRetrieval: ProvenanceArtifactNotRequested}}
	overlay := []ospackage.PackageInfo{
		{Name: "mesa-amd64", PkgName: "mesa-amd64", Type: "deb", Arch: "amd64", Version: "26.0", SourceName: "mesa", SourceVersion: "26.0", SourceRepository: "repo-a", SourceMappingStatus: ProvenanceStatusResolved},
		{Name: "mesa-i386", PkgName: "mesa-i386", Type: "deb", Arch: "i386", Version: "26.0", SourceName: "mesa", SourceVersion: "26.0", SourceRepository: "repo-a", SourceMappingStatus: ProvenanceStatusResolved},
	}
	out := filepath.Join(t.TempDir(), "mesa.json")
	if err := WriteMergedSPDXWithSources(baseline, overlay, nil, sources, out); err != nil {
		t.Fatalf("WriteMergedSPDXWithSources: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var doc SPDXDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	sourceCount, generatedCount := 0, 0
	for _, pkg := range doc.Packages {
		if pkg.Type == "source" {
			sourceCount++
		}
	}
	for _, relationship := range doc.Relationships {
		if relationship.RelationshipType == "GENERATED_FROM" {
			generatedCount++
		}
	}
	if sourceCount != 1 || generatedCount != 2 {
		t.Fatalf("source count=%d generated relationships=%d, want 1 and 2", sourceCount, generatedCount)
	}
}

func TestSourceSPDXIDStable(t *testing.T) {
	t.Parallel()

	first := sourceSPDXID("repo-a", "gstreamer1.0", "1.28.2")
	second := sourceSPDXID("repo-a", "gstreamer1.0", "1.28.2")
	if first != second || !strings.HasPrefix(first, "SPDXRef-Source-") {
		t.Errorf("source IDs = %q, %q", first, second)
	}
	if sourceSPDXID("repo-b", "gstreamer1.0", "1.28.2") == first {
		t.Error("different repository snapshots produced the same source ID")
	}
}
