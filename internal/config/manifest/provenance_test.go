package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProvenanceManifestEmbeddedAndBuildSide(t *testing.T) {
	t.Parallel()

	target := ProvenanceTarget{ImageName: "edge", ImageVersion: "1.0", OS: "ubuntu", Arch: "amd64", ImageType: "raw"}
	binaries := []ProvenanceBinary{{Name: "gstreamer-tools", Version: "1.28.2", Arch: "amd64", SourceName: "gstreamer1.0", SourceVersion: "1.28.2", InventoryClass: "resolved-and-installed", MappingStatus: ProvenanceStatusResolved}}
	sources := []ProvenanceSource{{Name: "gstreamer1.0", Version: "1.28.2", RepositoryStatus: "resolved", MappingStatus: ProvenanceStatusResolved, ArtifactRetrieval: ProvenanceArtifactNotRequested,
		Artifacts: []ProvenanceArtifact{{Filename: "gstreamer1.0.dsc", URL: "https://example.org/pool/gstreamer1.0.dsc", Size: 10, SHA256: strings.Repeat("a", 64)}}}}
	embedded := NewProvenanceManifest(target, "full-inventory", binaries, sources)
	if err := embedded.Validate(); err != nil {
		t.Fatalf("embedded manifest: %v", err)
	}
	if embedded.ImageSHA256 != "" {
		t.Errorf("embedded manifest contains image digest %q", embedded.ImageSHA256)
	}

	buildSide, err := embedded.WithFinalImageSHA256(strings.Repeat("b", 64))
	if err != nil {
		t.Fatalf("build-side manifest: %v", err)
	}
	if buildSide.ImageSHA256 != strings.Repeat("b", 64) {
		t.Errorf("build-side digest = %q", buildSide.ImageSHA256)
	}
	if buildSide.SourcePackages[0].ArtifactRetrieval != ProvenanceArtifactNotRequested {
		t.Errorf("artifact retrieval = %q", buildSide.SourcePackages[0].ArtifactRetrieval)
	}
}

func TestProvenanceManifestRejectsUnsafeOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		manifest ProvenanceManifest
	}{
		{"bad schema", ProvenanceManifest{SchemaVersion: "other", CoverageMode: "full-inventory"}},
		{"bad coverage", ProvenanceManifest{SchemaVersion: ProvenanceManifestSchema, CoverageMode: "delta-only"}},
		{"artifact requested", ProvenanceManifest{SchemaVersion: ProvenanceManifestSchema, CoverageMode: "full-inventory", SourcePackages: []ProvenanceSource{{ArtifactRetrieval: "requested"}}}},
		{"credential URL", ProvenanceManifest{SchemaVersion: ProvenanceManifestSchema, CoverageMode: "full-inventory", SourcePackages: []ProvenanceSource{{ArtifactRetrieval: ProvenanceArtifactNotRequested, Artifacts: []ProvenanceArtifact{{URL: "https://user:secret@example.org/pkg.dsc"}}}}}},
		{"query URL", ProvenanceManifest{SchemaVersion: ProvenanceManifestSchema, CoverageMode: "full-inventory", SourcePackages: []ProvenanceSource{{ArtifactRetrieval: ProvenanceArtifactNotRequested, Artifacts: []ProvenanceArtifact{{URL: "https://example.org/pkg.dsc?token=secret"}}}}}},
		{"bad digest", ProvenanceManifest{SchemaVersion: ProvenanceManifestSchema, CoverageMode: "full-inventory", ImageSHA256: "short"}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.manifest.Validate(); err == nil {
				t.Fatal("Validate returned nil, want error")
			}
		})
	}
}

func TestWriteProvenanceManifest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "image.provenance.json")
	manifest := NewProvenanceManifest(ProvenanceTarget{ImageName: "edge", ImageVersion: "1"}, "delta", []ProvenanceBinary{{Name: "script", Version: "1", InventoryClass: "script-installed", MappingStatus: ProvenanceStatusOriginUnknown, CoverageGap: true}}, []ProvenanceSource{})
	manifest.CoverageGaps = []string{"script: origin unknown"}
	if err := WriteProvenanceManifest(path, manifest); err != nil {
		t.Fatalf("WriteProvenanceManifest: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var decoded ProvenanceManifest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if decoded.SchemaVersion != ProvenanceManifestSchema || decoded.CoverageMode != "delta" || len(decoded.BinaryPackages) != 1 || decoded.ImageSHA256 != "" {
		t.Fatalf("unexpected manifest: %+v", decoded)
	}
}
