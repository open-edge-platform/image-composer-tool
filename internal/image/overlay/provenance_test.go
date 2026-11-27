package overlay

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/config/manifest"
)

func TestStageOverlayProvenanceArtifacts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	template := &config.ImageTemplate{
		Image:  config.ImageInfo{Name: "edge", Version: "1.0"},
		Target: config.TargetInfo{OS: "ubuntu", Dist: "24.04", Arch: "amd64", ImageType: "raw"},
	}
	plan := &ResolutionPlan{ToInstall: []ResolvedPackage{{
		Name: "gstreamer1.0-tools", Version: "1.28.2", Arch: "amd64",
		SourceName: "gstreamer1.0", SourceVersion: "1.28.2", SourceRepository: "repo-a",
	}}}
	report := &FinalInventoryReport{Packages: []ReconciledPackage{{
		Name: "gstreamer1.0-tools", Version: "1.28.2", Arch: "amd64",
		SourceName: "gstreamer1.0", SourceVersion: "1.28.2", RepositoryID: "repo-a",
		Class: ReconciliationResolvedAndInstalled,
	}}}

	artifacts, err := stageOverlayProvenanceArtifacts(template, &BaselineInfo{OS: "ubuntu", Arch: "amd64"}, root, plan, report)
	if err != nil {
		t.Fatalf("stageOverlayProvenanceArtifacts: %v", err)
	}
	embeddedPath := filepath.Join(root, "usr", "share", "sbom", provenanceEmbeddedFilename)
	data, err := os.ReadFile(embeddedPath)
	if err != nil {
		t.Fatalf("read embedded provenance: %v", err)
	}
	var embedded manifest.ProvenanceManifest
	if err := json.Unmarshal(data, &embedded); err != nil {
		t.Fatalf("decode embedded provenance: %v", err)
	}
	if embedded.ImageSHA256 != "" {
		t.Errorf("embedded provenance contains image digest %q", embedded.ImageSHA256)
	}
	if len(embedded.BinaryPackages) != 1 || embedded.BinaryPackages[0].SourceName != "gstreamer1.0" ||
		embedded.BinaryPackages[0].MappingStatus != manifest.ProvenanceStatusResolved {
		t.Errorf("unexpected embedded binary records: %+v", embedded.BinaryPackages)
	}
	if len(embedded.SourcePackages) != 1 || embedded.SourcePackages[0].ArtifactRetrieval != manifest.ProvenanceArtifactNotRequested {
		t.Errorf("unexpected source records: %+v", embedded.SourcePackages)
	}
	if artifacts.embeddedPath == "" || artifacts.buildSidePath == "" {
		t.Errorf("missing staged provenance paths: %+v", artifacts)
	}
}

func TestFinalizeOverlayProvenance(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	buildSide := filepath.Join(dir, "build-provenance.json")
	finalSide := filepath.Join(dir, "edge-1.0.provenance.json")
	provenance := manifest.NewProvenanceManifest(manifest.ProvenanceTarget{ImageName: "edge", ImageVersion: "1.0"}, "full-inventory", nil, nil)
	if err := manifest.WriteProvenanceManifest(buildSide, provenance); err != nil {
		t.Fatalf("write build-side source: %v", err)
	}

	digest := strings.Repeat("b", 64)
	if err := finalizeOverlayProvenance(buildSide, finalSide, digest); err != nil {
		t.Fatalf("finalizeOverlayProvenance: %v", err)
	}
	data, err := os.ReadFile(finalSide)
	if err != nil {
		t.Fatalf("read final provenance: %v", err)
	}
	var finalManifest manifest.ProvenanceManifest
	if err := json.Unmarshal(data, &finalManifest); err != nil {
		t.Fatalf("decode final provenance: %v", err)
	}
	if finalManifest.ImageSHA256 != digest {
		t.Errorf("final provenance digest = %q, want %q", finalManifest.ImageSHA256, digest)
	}
}
