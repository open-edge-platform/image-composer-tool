package manifest

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/security"
)

const ProvenanceManifestSchema = "ict.source-provenance/1.0"

const (
	ProvenanceStatusResolved             = "resolved"
	ProvenanceStatusDisabled             = "disabled"
	ProvenanceStatusOriginUnknown        = "origin-unknown"
	ProvenanceStatusSourceIndexMissing   = "source-index-missing"
	ProvenanceStatusSourceVersionMissing = "source-version-missing"
	ProvenanceStatusNetworkError         = "network-error"
	ProvenanceStatusVerificationFailed   = "verification-failed"
	ProvenanceStatusUnsupported          = "unsupported"
	ProvenanceStatusContradictory        = "contradictory-declaration"
	ProvenanceStatusUnavailable          = "unavailable"
	ProvenanceArtifactNotRequested       = "not-requested"
)

type ProvenanceTarget struct {
	ImageName    string `json:"imageName,omitempty"`
	ImageVersion string `json:"imageVersion,omitempty"`
	OS           string `json:"os,omitempty"`
	Dist         string `json:"dist,omitempty"`
	Arch         string `json:"arch,omitempty"`
	ImageType    string `json:"imageType,omitempty"`
}

type ProvenanceArtifact struct {
	Filename string `json:"filename"`
	URL      string `json:"url,omitempty"`
	Size     uint64 `json:"size,omitempty"`
	SHA256   string `json:"sha256,omitempty"`
}

type ProvenanceVCS struct {
	InputRole               string `json:"inputRole,omitempty"`
	Repository              string `json:"repository,omitempty"`
	Revision                string `json:"revision,omitempty"`
	Ref                     string `json:"ref,omitempty"`
	ProducerDeclaredVersion string `json:"producerDeclaredPackageVersion,omitempty"`
	CompletenessAttestation string `json:"completenessAttestation,omitempty"`
	EvidenceSource          string `json:"evidenceSource,omitempty"`
	ExactVersionMatch       *bool  `json:"exactVersionMatch,omitempty"`
}

type ProvenanceSource struct {
	Name                  string               `json:"name"`
	Version               string               `json:"version"`
	RepositoryID          string               `json:"repositoryId,omitempty"`
	ReleaseDigest         string               `json:"releaseDigest,omitempty"`
	SourceIndexDigest     string               `json:"sourceIndexDigest,omitempty"`
	SigningKeyFingerprint string               `json:"signingKeyFingerprint,omitempty"`
	RepositoryStatus      string               `json:"repositoryMetadataVerification,omitempty"`
	MappingStatus         string               `json:"mappingStatus,omitempty"`
	ArtifactRetrieval     string               `json:"artifactRetrieval"`
	VCSStatus             string               `json:"vcsStatus,omitempty"`
	Artifacts             []ProvenanceArtifact `json:"artifacts,omitempty"`
	VCS                   []ProvenanceVCS      `json:"vcs,omitempty"`
}

type ProvenanceBinary struct {
	Name           string `json:"name"`
	Version        string `json:"version"`
	Arch           string `json:"arch,omitempty"`
	MultiArch      string `json:"multiArch,omitempty"`
	SourceName     string `json:"sourceName,omitempty"`
	SourceVersion  string `json:"sourceVersion,omitempty"`
	RepositoryID   string `json:"repositoryId,omitempty"`
	InventoryClass string `json:"inventoryClass"`
	MappingStatus  string `json:"mappingStatus,omitempty"`
	CoverageGap    bool   `json:"coverageGap,omitempty"`
}

type ProvenanceManifest struct {
	SchemaVersion  string             `json:"schemaVersion"`
	GeneratedAt    string             `json:"generatedAt"`
	CoverageMode   string             `json:"coverageMode"`
	Target         ProvenanceTarget   `json:"target"`
	ImageSHA256    string             `json:"imageSha256,omitempty"`
	BinaryPackages []ProvenanceBinary `json:"binaryPackages"`
	SourcePackages []ProvenanceSource `json:"sourcePackages"`
	CoverageGaps   []string           `json:"coverageGaps,omitempty"`
}

func NewProvenanceManifest(target ProvenanceTarget, coverageMode string, binaries []ProvenanceBinary, sources []ProvenanceSource) ProvenanceManifest {
	return ProvenanceManifest{
		SchemaVersion:  ProvenanceManifestSchema,
		GeneratedAt:    time.Now().UTC().Format(time.RFC3339),
		CoverageMode:   coverageMode,
		Target:         target,
		BinaryPackages: binaries,
		SourcePackages: sources,
	}
}

func (manifest ProvenanceManifest) Validate() error {
	if manifest.SchemaVersion != ProvenanceManifestSchema {
		return fmt.Errorf("unsupported provenance schema version")
	}
	if manifest.CoverageMode != "full-inventory" && manifest.CoverageMode != "delta" {
		return fmt.Errorf("invalid provenance coverage mode")
	}
	for _, source := range manifest.SourcePackages {
		if source.ArtifactRetrieval != ProvenanceArtifactNotRequested {
			return fmt.Errorf("Phase 1 provenance does not permit source artifact retrieval")
		}
		for _, artifact := range source.Artifacts {
			if artifact.URL != "" {
				parsed, err := url.Parse(artifact.URL)
				if err != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.User != nil {
					return fmt.Errorf("provenance artifact URL contains credentials or a query/fragment")
				}
			}
		}
	}
	if manifest.ImageSHA256 != "" && len(manifest.ImageSHA256) != 64 {
		return fmt.Errorf("invalid final image SHA256")
	}
	return nil
}

func WriteProvenanceManifest(path string, manifest ProvenanceManifest) error {
	if err := manifest.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling provenance manifest: %w", err)
	}
	data = append(data, '\n')
	if err := security.SafeWriteFile(path, data, 0640, security.RejectSymlinks); err != nil {
		return fmt.Errorf("writing provenance manifest: %w", err)
	}
	return nil
}

func (manifest ProvenanceManifest) WithFinalImageSHA256(imageSHA256 string) (ProvenanceManifest, error) {
	value := strings.TrimSpace(strings.ToLower(imageSHA256))
	if len(value) != 64 {
		return ProvenanceManifest{}, fmt.Errorf("invalid final image SHA256")
	}
	manifest.ImageSHA256 = value
	if err := manifest.Validate(); err != nil {
		return ProvenanceManifest{}, err
	}
	return manifest, nil
}
