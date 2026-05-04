package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/config/version"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/file"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/security"
)

// Constants used for SDPX metadata generation
const (
	SPDXVersion       = "SPDX-2.3"
	SPDXDataLicense   = "CC0-1.0"
	SPDXDocumentID    = "SPDXRef-DOCUMENT"
	SPDXNamespaceBase = "https://spdx.openedge.dev/docs"
	DefaultSupplier   = "Organization: UNKNOWN"
	DefaultLicense    = "NOASSERTION"
	// Path where SBOM will be stored inside the image filesystem
	ImageSBOMPath = "/usr/share/sbom"
)

var DefaultSPDXFile = "spdx_manifest.json"

// SoftwarePackageManifest represents the structure of the manifest file.
type SoftwarePackageManifest struct {
	SchemaVersion     string `json:"schema_version"`
	ImageVersion      string `json:"image_version"`
	BuiltAt           string `json:"built_at"`
	Arch              string `json:"arch"`
	SizeBytes         int64  `json:"size_bytes"`
	Hash              string `json:"hash"`
	HashAlg           string `json:"hash_alg"`
	Signature         string `json:"signature"`
	SigAlg            string `json:"sig_alg"`
	MinCurrentVersion string `json:"min_current_version"`
}

// Holds the SPDX Document header information
type SPDXDocument struct {
	SPDXVersion       string        `json:"spdxVersion"`
	DataLicense       string        `json:"dataLicense"`
	SPDXID            string        `json:"SPDXID"`
	DocumentName      string        `json:"name"`
	DocumentNamespace string        `json:"documentNamespace"`
	CreationInfo      CreationInfo  `json:"creationInfo"`
	Packages          []SPDXPackage `json:"packages"`
}

// Time stamp and creation information
type CreationInfo struct {
	Created  string   `json:"created"`
	Creators []string `json:"creators"`
}

// Holds an SBOM instance in the SPDX document
type SPDXPackage struct {
	SPDXID           string         `json:"SPDXID"`
	Name             string         `json:"name"`
	Type             string         `json:"type,omitempty"` // e.g., "deb", "rpm"
	VersionInfo      string         `json:"versionInfo,omitempty"`
	DownloadLocation string         `json:"downloadLocation"`
	FilesAnalyzed    bool           `json:"filesAnalyzed"`
	LicenseDeclared  string         `json:"licenseDeclared"`
	LicenseConcluded string         `json:"licenseConcluded"`
	Supplier         string         `json:"supplier,omitempty"`
	Checksum         []SPDXChecksum `json:"checksum,omitempty"`
	Description      string         `json:"description,omitempty"`
}

// Holds the checksum value for an SBOM instance item
type SPDXChecksum struct {
	Algorithm     string `json:"algorithm"`
	ChecksumValue string `json:"checksumValue"`
}

var log = logger.Logger()

// WriteManifestToFile writes the manifest to the specified output file.
func WriteManifestToFile(manifest SoftwarePackageManifest, outputFile string) error {

	log.Infof("Writing the Image Manifest to the file: %s", outputFile)

	// Marshal the manifest struct to JSON
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		log.Errorf("Error marshaling manifest to JSON: %v", err)
		return fmt.Errorf("error marshaling manifest to JSON: %w", err)
	}

	// Create or open the output file with restrictive permissions and symlink protection
	file, err := security.SafeOpenFile(outputFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600, security.RejectSymlinks)
	if err != nil {
		// Don't expose the full file path in error messages
		log.Errorf("Failed to create manifest file: %v", err)
		return fmt.Errorf("error creating manifest file: file access denied: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			log.Warnf("Failed to close manifest file: %v", closeErr)
		}
	}()

	// Write the JSON data to the file
	_, err = file.Write(manifestJSON)
	if err != nil {
		log.Errorf("Failed to write manifest data: %v", err)
		return fmt.Errorf("error writing manifest data: %w", err)
	}

	return nil
}

func WriteSPDXToFile(pkgs []ospackage.PackageInfo, outFile string) error {

	log.Infof("Generating SPDX manifest for %d packages", len(pkgs))

	// SPDX allows only specific checksum algorithms: SHA1, SHA256, MD5
	validSPDXAlgos := map[string]bool{
		"SHA1":   true,
		"SHA256": true,
		"MD5":    true,
	}

	spdx := SPDXDocument{
		SPDXVersion:       SPDXVersion,
		DataLicense:       SPDXDataLicense,
		SPDXID:            SPDXDocumentID,
		DocumentName:      fmt.Sprintf("%s-%s", version.Toolname, time.Now().UTC().Format("20060102T150405Z")),
		DocumentNamespace: generateDocumentNamespace(),
		CreationInfo: CreationInfo{
			Created: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
			Creators: []string{
				fmt.Sprintf("Tool: %s %s", version.Toolname, version.Version),
				fmt.Sprintf("Organization: %s", version.Organization),
			},
		},
		Packages: make([]SPDXPackage, 0, len(pkgs)),
	}

	for _, pkg := range pkgs {
		spdxPkg := SPDXPackage{
			SPDXID:           fmt.Sprintf("SPDXRef-Package-%s", pkg.Name),
			Name:             pkg.Name,
			Type:             pkg.Type,
			VersionInfo:      pkg.Version,
			DownloadLocation: pkg.URL,
			FilesAnalyzed:    false,
			LicenseDeclared:  fallbackToDefault(pkg.License, "NOASSERTION"),
			LicenseConcluded: "NOASSERTION",
			Description:      pkg.Description,
		}

		// If the supplier is not specified, use a default value, for
		// anything that appears as an email, use the Person form otherwise
		// use the Organization form
		spdxPkg.Supplier = spdxSupplier(pkg.Origin)

		// If the checksum is not specified or missing, leave field out
		// Valid values according to SPDX spec: SHA1, SHA256, MD5
		var spdxChecksums []SPDXChecksum
		for _, c := range pkg.Checksums {
			algo := strings.ToUpper(c.Algorithm)
			if validSPDXAlgos[algo] {
				spdxChecksums = append(spdxChecksums, SPDXChecksum{
					Algorithm:     algo,
					ChecksumValue: c.Value,
				})
			}
		}

		if len(spdxChecksums) > 0 {
			spdxPkg.Checksum = spdxChecksums
		}

		spdx.Packages = append(spdx.Packages, spdxPkg)
	}

	if err := os.MkdirAll(filepath.Dir(outFile), 0700); err != nil {
		log.Errorf("Failed to create SPDX output directory: %v", err)
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Use SafeWriteFile instead of SafeOpenFile for simpler file creation with symlink protection
	jsonData, err := json.MarshalIndent(spdx, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal SPDX JSON: %w", err)
	}

	// Write file with symlink protection
	if err := security.SafeWriteFile(outFile, jsonData, 0600, security.RejectSymlinks); err != nil {
		log.Errorf("Failed to write SPDX file: %v", err)
		return fmt.Errorf("failed to create SPDX output file: %w", err)
	}
	log.Infof("SPDX manifest written to staging %s", outFile)

	return nil
}

func fallbackToDefault(val, fallback string) string {
	if val == "" {
		log.Debugf("Value is empty, using fallback: %s", fallback)
		return fallback
	}
	return val
}

func generateDocumentNamespace() string {
	return fmt.Sprintf("%s/%s-%s", SPDXNamespaceBase, version.Toolname, uuid.New().String())
}

func spdxSupplier(origin string) string {
	o := strings.TrimSpace(origin)
	if o == "" {
		return "NOASSERTION"
	}
	// If it looks like "Name <email>", emit Person form
	if strings.Contains(o, "<") && strings.Contains(o, ">") {
		parts := strings.Split(o, "<")
		if len(parts) >= 2 {
			name := strings.TrimSpace(parts[0])
			emailParts := strings.Split(parts[1], ">")
			if len(emailParts) >= 1 {
				email := strings.Trim(emailParts[0], " ")
				if name != "" && email != "" {
					return fmt.Sprintf("Person: %s (%s)", name, email)
				}
			}
		}
	}
	return fmt.Sprintf("Organization: %s", o)
}

// CopySBOMToImageBuildDir copies the SBOM from temp directory to the image build directory
// This ensures the SBOM is packaged alongside the final image artifact
func CopySBOMToImageBuildDir(imageBuildDir string) error {
	log.Infof("Copying SBOM to image build directory: %s", imageBuildDir)

	// Source: SBOM in temp directory (same location where it was generated)
	srcSBOM := filepath.Join(config.TempDir(), DefaultSPDXFile)

	// Destination: SBOM in image build directory
	dstSBOM := filepath.Join(imageBuildDir, DefaultSPDXFile)

	// Check if source SBOM exists
	if _, err := os.Stat(srcSBOM); os.IsNotExist(err) {
		log.Warnf("SBOM file not found at %s, skipping copy", srcSBOM)
		return nil
	}

	// Read source SBOM with security checks
	data, err := security.SafeReadFile(srcSBOM, security.RejectSymlinks)
	if err != nil {
		log.Errorf("Failed to read SBOM file: %v", err)
		return fmt.Errorf("failed to read SBOM file: %w", err)
	}

	// Write to destination with security checks
	if err := security.SafeWriteFile(dstSBOM, data, 0644, security.RejectSymlinks); err != nil {
		log.Errorf("Failed to write SBOM to image build directory: %v", err)
		return fmt.Errorf("failed to write SBOM to image build directory: %w", err)
	}

	log.Infof("Successfully copied SBOM to: %s", dstSBOM)
	return nil
}

// CopySBOMToChroot copies the SBOM from temp directory into the image's filesystem at /usr/share/sbom/
// This embeds the SBOM inside the image for CVE scanning and compliance tools
func CopySBOMToChroot(chrootPath string) error {
	log.Infof("Copying SBOM into image filesystem at %s", ImageSBOMPath)

	// Source: SBOM in temp directory (same location where it was generated)
	srcSBOM := filepath.Join(config.TempDir(), DefaultSPDXFile)

	// Destination: SBOM inside the chroot filesystem
	dstSBOM := filepath.Join(chrootPath, ImageSBOMPath, DefaultSPDXFile)

	// Check if source SBOM exists
	if _, err := os.Stat(srcSBOM); os.IsNotExist(err) {
		log.Warnf("SBOM file not found at %s, skipping copy to chroot", srcSBOM)
		return nil
	}

	if err := file.CopyFile(srcSBOM, dstSBOM, "--preserve=mode", true); err != nil {
		log.Errorf("Failed to copy SBOM into image filesystem: %v", err)
		return fmt.Errorf("failed to copy SBOM into image filesystem: %w", err)
	}

	log.Infof("Successfully copied SBOM into image filesystem at: %s", dstSBOM)
	return nil
}
