package overlay

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/config/manifest"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/security"
)

const provenanceEmbeddedFilename = "ict.provenance.json"

type overlayProvenanceArtifacts struct {
	embeddedPath  string
	buildSidePath string
}

func stageOverlayProvenanceArtifacts(template *config.ImageTemplate, info *BaselineInfo, rootMount string, plan *ResolutionPlan, report *FinalInventoryReport) (*overlayProvenanceArtifacts, error) {
	if template == nil {
		return nil, fmt.Errorf("overlay provenance: image template cannot be nil")
	}
	binaryPackages := make([]manifest.ProvenanceBinary, 0)
	if report != nil {
		for _, pkg := range report.Packages {
			if pkg.Class == ReconciliationRemoved {
				continue
			}
			mappingStatus := manifest.ProvenanceStatusOriginUnknown
			if pkg.CoverageGap {
				mappingStatus = manifest.ProvenanceStatusOriginUnknown
			} else if pkg.Class == ReconciliationResolvedAndInstalled && pkg.SourceName != "" {
				mappingStatus = manifest.ProvenanceStatusResolved
			} else if pkg.Class == ReconciliationBaselineRetained {
				mappingStatus = manifest.ProvenanceStatusOriginUnknown
			}
			binaryPackages = append(binaryPackages, manifest.ProvenanceBinary{
				Name: pkg.Name, Version: pkg.Version, Arch: pkg.Arch, MultiArch: pkg.MultiArch,
				SourceName: pkg.SourceName, SourceVersion: pkg.SourceVersion, RepositoryID: pkg.RepositoryID,
				InventoryClass: string(pkg.Class), MappingStatus: mappingStatus, CoverageGap: pkg.CoverageGap,
			})
		}
	}

	sourceMap := make(map[string]manifest.ProvenanceSource)
	if plan != nil {
		for _, pkg := range append(append([]ResolvedPackage{}, plan.Closure...), plan.ToInstall...) {
			if pkg.SourceName == "" {
				continue
			}
			key := pkg.SourceRepository + "\x00" + pkg.SourceName + "\x00" + pkg.SourceVersion
			sourceMap[key] = manifest.ProvenanceSource{
				Name: pkg.SourceName, Version: pkg.SourceVersion, RepositoryID: pkg.SourceRepository,
				MappingStatus:     manifest.ProvenanceStatusResolved,
				ArtifactRetrieval: manifest.ProvenanceArtifactNotRequested,
			}
		}
	}
	sources := make([]manifest.ProvenanceSource, 0, len(sourceMap))
	for _, source := range sourceMap {
		sources = append(sources, source)
	}
	sort.Slice(sources, func(i, j int) bool {
		if sources[i].RepositoryID != sources[j].RepositoryID {
			return sources[i].RepositoryID < sources[j].RepositoryID
		}
		if sources[i].Name != sources[j].Name {
			return sources[i].Name < sources[j].Name
		}
		return sources[i].Version < sources[j].Version
	})

	coverageGaps := make([]string, 0)
	if report != nil {
		for _, pkg := range report.Packages {
			if pkg.CoverageGap {
				coverageGaps = append(coverageGaps, pkg.Name+"@"+pkg.Version+": origin unknown")
			}
		}
	}

	target := manifest.ProvenanceTarget{
		ImageName: template.GetImageName(), ImageVersion: template.Image.Version,
		OS: template.Target.OS, Dist: template.Target.Dist, Arch: template.Target.Arch, ImageType: template.Target.ImageType,
	}
	if info != nil {
		target.OS = info.OS
		target.Arch = info.Arch
	}
	provenance := manifest.NewProvenanceManifest(target, "full-inventory", binaryPackages, sources)
	provenance.CoverageGaps = coverageGaps

	tempDir := config.TempDir()
	embeddedPath := filepath.Join(tempDir, provenanceEmbeddedFilename)
	if err := manifest.WriteProvenanceManifest(embeddedPath, provenance); err != nil {
		return nil, fmt.Errorf("writing embedded provenance manifest: %w", err)
	}

	embedDest := filepath.Join(manifest.ImageSBOMPath, provenanceEmbeddedFilename)
	if err := assertNoSymlinkInChrootPath(rootMount, embedDest); err != nil {
		return nil, fmt.Errorf("refusing to embed provenance manifest: %w", err)
	}
	data, err := os.ReadFile(embeddedPath)
	if err != nil {
		return nil, fmt.Errorf("reading staged provenance manifest: %w", err)
	}
	destination := filepath.Join(rootMount, filepath.FromSlash(embedDest))
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return nil, fmt.Errorf("creating provenance manifest directory: %w", err)
	}
	if err := security.SafeWriteFile(destination, data, 0640, security.RejectSymlinks); err != nil {
		return nil, fmt.Errorf("embedding provenance manifest: %w", err)
	}

	buildSidePath := filepath.Join(tempDir, "build-provenance.json")
	if err := manifest.WriteProvenanceManifest(buildSidePath, provenance); err != nil {
		return nil, fmt.Errorf("writing build-side provenance manifest: %w", err)
	}
	return &overlayProvenanceArtifacts{embeddedPath: embeddedPath, buildSidePath: buildSidePath}, nil
}

func finalizeOverlayProvenance(buildSidePath, destination, imageSHA256 string) error {
	data, err := os.ReadFile(buildSidePath)
	if err != nil {
		return fmt.Errorf("reading staged build-side provenance: %w", err)
	}
	var provenance manifest.ProvenanceManifest
	if err := json.Unmarshal(data, &provenance); err != nil {
		return fmt.Errorf("decoding staged build-side provenance: %w", err)
	}
	finalized, err := provenance.WithFinalImageSHA256(imageSHA256)
	if err != nil {
		return err
	}
	if err := manifest.WriteProvenanceManifest(destination, finalized); err != nil {
		return err
	}
	return nil
}
