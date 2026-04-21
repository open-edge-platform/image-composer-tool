package imageinspect

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config/manifest"
)

func inspectSBOMFromImageRaw(img io.ReaderAt, pt PartitionTableSummary) SBOMSummary {
	summary := SBOMSummary{Format: "spdx"}
	rootCandidates := rankRootPartitionCandidates(pt)
	if len(rootCandidates) == 0 {
		summary.Notes = append(summary.Notes, "SBOM root partition candidate not found")
		return summary
	}

	for _, candidateIndex := range rootCandidates {
		partitionSummary := pt.Partitions[candidateIndex]
		fsType := ""
		if partitionSummary.Filesystem != nil {
			fsType = strings.ToLower(strings.TrimSpace(partitionSummary.Filesystem.Type))
		}

		partOff := partitionStartOffset(pt, partitionSummary)
		var (
			sbomData     []byte
			sbomFileName string
			sbomPath     string
			err          error
		)

		sbomData, sbomFileName, sbomPath, err = readSBOMFromRawPartition(img, partOff, partitionSummary.SizeBytes, fsType)

		if err != nil {
			summary.Notes = append(summary.Notes,
				fmt.Sprintf("failed to read SBOM from partition %d (%s): %v",
					partitionSummary.Index, partitionSummary.Name, err))
			continue
		}

		summary.Present = true
		summary.Path = sbomPath
		summary.FileName = sbomFileName
		summary.SizeBytes = int64(len(sbomData))
		summary.SHA256 = sha256Hex(sbomData)
		summary.Content = append([]byte(nil), sbomData...)

		canonicalSHA, pkgCount, canonicalErr := canonicalSPDXSHA256(sbomData)
		if canonicalErr != nil {
			summary.Notes = append(summary.Notes, "SBOM SPDX parse failed; compare falls back to raw hash")
			return summary
		}

		summary.CanonicalSHA256 = canonicalSHA
		summary.PackageCount = pkgCount
		return summary
	}

	summary.Notes = append(summary.Notes, "SBOM not found at /usr/share/sbom")
	return summary
}

func inspectSBOMFromImageFilesystem(disk diskAccessorFS, pt PartitionTableSummary) SBOMSummary {
	summary := SBOMSummary{Format: "spdx"}
	rootCandidates := rankRootPartitionCandidates(pt)
	if len(rootCandidates) == 0 {
		summary.Notes = append(summary.Notes, "SBOM root partition candidate not found")
		return summary
	}

	dirCandidates := []string{manifest.ImageSBOMPath, strings.TrimPrefix(manifest.ImageSBOMPath, "/")}

	for _, candidateIndex := range rootCandidates {
		partitionSummary := pt.Partitions[candidateIndex]
		partitionNumber, ok := diskfsPartitionNumberForSummary(disk, partitionSummary)
		if !ok {
			continue
		}

		filesystemHandle, err := disk.GetFilesystem(partitionNumber)
		if err != nil || filesystemHandle == nil {
			continue
		}

		for _, sbomDir := range dirCandidates {
			sbomDir = strings.TrimSpace(sbomDir)
			if sbomDir == "" {
				continue
			}

			sbomDirEntries, readDirErr := filesystemHandle.ReadDir(sbomDir)
			if readDirErr != nil {
				continue
			}

			sbomFileName, found := pickSBOMFileNameFromFS(sbomDirEntries)
			if !found {
				summary.Notes = append(summary.Notes, "SBOM directory present but no SPDX JSON file found")
				continue
			}

			fileCandidates := []string{
				path.Join(sbomDir, sbomFileName),
				path.Join(strings.TrimPrefix(sbomDir, "/"), sbomFileName),
			}

			for _, sbomFilePath := range fileCandidates {
				sbomFile, openErr := filesystemHandle.OpenFile(sbomFilePath, os.O_RDONLY)
				if openErr != nil {
					continue
				}

				sbomData, readErr := io.ReadAll(sbomFile)
				if closeErr := sbomFile.Close(); closeErr != nil {
					summary.Notes = append(summary.Notes, fmt.Sprintf("failed to close SBOM file %s", sbomFilePath))
				}
				if readErr != nil {
					summary.Notes = append(summary.Notes, fmt.Sprintf("failed to read SBOM file %s", sbomFilePath))
					continue
				}

				summary.Present = true
				summary.Path = path.Join(manifest.ImageSBOMPath, sbomFileName)
				summary.FileName = sbomFileName
				summary.SizeBytes = int64(len(sbomData))
				summary.SHA256 = sha256Hex(sbomData)
				summary.Content = append([]byte(nil), sbomData...)

				canonicalSHA, pkgCount, canonicalErr := canonicalSPDXSHA256(sbomData)
				if canonicalErr != nil {
					summary.Notes = append(summary.Notes, "SBOM SPDX parse failed; compare falls back to raw hash")
					return summary
				}

				summary.CanonicalSHA256 = canonicalSHA
				summary.PackageCount = pkgCount
				return summary
			}
		}
	}

	summary.Notes = append(summary.Notes, "SBOM not found at /usr/share/sbom")
	return summary
}

func partitionStartOffset(pt PartitionTableSummary, partitionSummary PartitionSummary) int64 {
	logicalSectorSize := pt.LogicalSectorSize
	if partitionSummary.LogicalSectorSize > 0 {
		logicalSectorSize = int64(partitionSummary.LogicalSectorSize)
	}

	if logicalSectorSize <= 0 {
		logicalSectorSize = 512
	}

	return int64(partitionSummary.StartLBA) * logicalSectorSize
}

func pickSBOMFileNameFromFAT(entries []fatDirEntry) (string, bool) {
	var preferred []string
	var fallback []string

	for _, entry := range entries {
		if entry.isDir {
			continue
		}
		name := strings.TrimSpace(entry.name)
		if name == "" {
			continue
		}
		lowerName := strings.ToLower(name)
		if !strings.HasSuffix(lowerName, ".json") {
			continue
		}

		if strings.HasPrefix(lowerName, "spdx_manifest") {
			preferred = append(preferred, name)
			continue
		}
		fallback = append(fallback, name)
	}

	if len(preferred) > 0 {
		sort.Strings(preferred)
		return preferred[0], true
	}
	if len(fallback) > 0 {
		sort.Strings(fallback)
		return fallback[0], true
	}

	return "", false
}

func pickSBOMFileNameFromFS(entries []os.FileInfo) (string, bool) {
	fileNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry == nil || entry.IsDir() {
			continue
		}
		fileNames = append(fileNames, entry.Name())
	}

	return pickSBOMFileNameFromNames(fileNames)
}

func pickSBOMFileNameFromNames(fileNames []string) (string, bool) {
	var preferred []string
	var fallback []string

	for _, fileName := range fileNames {
		name := strings.TrimSpace(fileName)
		if name == "" {
			continue
		}

		lowerName := strings.ToLower(name)
		if !strings.HasSuffix(lowerName, ".json") {
			continue
		}

		if strings.HasPrefix(lowerName, "spdx_manifest") {
			preferred = append(preferred, name)
			continue
		}
		fallback = append(fallback, name)
	}

	if len(preferred) > 0 {
		sort.Strings(preferred)
		return preferred[0], true
	}
	if len(fallback) > 0 {
		sort.Strings(fallback)
		return fallback[0], true
	}

	return "", false
}

func rankRootPartitionCandidates(pt PartitionTableSummary) []int {
	indexes := make([]int, 0, len(pt.Partitions))
	for idx := range pt.Partitions {
		indexes = append(indexes, idx)
	}

	score := func(partitionSummary PartitionSummary) int {
		name := strings.ToLower(strings.TrimSpace(partitionSummary.Name))
		fsType := ""
		if partitionSummary.Filesystem != nil {
			fsType = strings.ToLower(strings.TrimSpace(partitionSummary.Filesystem.Type))
		}

		total := 0
		if strings.Contains(name, "root") || strings.Contains(name, "rootfs") {
			total += 200
		}
		if strings.Contains(name, "system") {
			total += 120
		}
		if fsType == "ext4" || fsType == "ext3" || fsType == "ext2" || fsType == "xfs" || fsType == "btrfs" {
			total += 100
		}
		if fsType == "squashfs" {
			total += 80
		}
		if isVFATLike(fsType) {
			total += 20
		}

		return total
	}

	sort.Slice(indexes, func(i, j int) bool {
		left := pt.Partitions[indexes[i]]
		right := pt.Partitions[indexes[j]]

		leftScore := score(left)
		rightScore := score(right)
		if leftScore != rightScore {
			return leftScore > rightScore
		}

		if left.SizeBytes != right.SizeBytes {
			return left.SizeBytes > right.SizeBytes
		}

		return left.Index < right.Index
	})

	return indexes
}

type spdxComparableDoc struct {
	Packages []spdxComparablePackage `json:"packages"`
}

type spdxComparablePackage struct {
	Name             string                 `json:"name,omitempty"`
	VersionInfo      string                 `json:"versionInfo,omitempty"`
	DownloadLocation string                 `json:"downloadLocation,omitempty"`
	Supplier         string                 `json:"supplier,omitempty"`
	LicenseDeclared  string                 `json:"licenseDeclared,omitempty"`
	LicenseConcluded string                 `json:"licenseConcluded,omitempty"`
	Checksum         []spdxComparableDigest `json:"checksum,omitempty"`
}

type spdxComparableDigest struct {
	Algorithm     string `json:"algorithm"`
	ChecksumValue string `json:"checksumValue"`
}

func canonicalSPDXSHA256(spdxData []byte) (string, int, error) {
	_, canonicalHash, pkgCount, err := parseAndCanonicalizeSPDX(spdxData)
	if err != nil {
		return "", 0, fmt.Errorf("failed to parse SPDX JSON: %w", err)
	}

	return canonicalHash, pkgCount, nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// SPDXCompareResult is the result of comparing two SPDX manifest files.
type SPDXCompareResult struct {
	FromPath          string   `json:"fromPath" yaml:"fromPath"`
	ToPath            string   `json:"toPath" yaml:"toPath"`
	Equal             bool     `json:"equal" yaml:"equal"`
	FromSHA256        string   `json:"fromSha256,omitempty" yaml:"fromSha256,omitempty"`
	ToSHA256          string   `json:"toSha256,omitempty" yaml:"toSha256,omitempty"`
	FromCanonicalHash string   `json:"fromCanonicalSha256,omitempty" yaml:"fromCanonicalSha256,omitempty"`
	ToCanonicalHash   string   `json:"toCanonicalSha256,omitempty" yaml:"toCanonicalSha256,omitempty"`
	FromPackageCount  int      `json:"fromPackageCount,omitempty" yaml:"fromPackageCount,omitempty"`
	ToPackageCount    int      `json:"toPackageCount,omitempty" yaml:"toPackageCount,omitempty"`
	AddedPackages     []string `json:"addedPackages,omitempty" yaml:"addedPackages,omitempty"`
	RemovedPackages   []string `json:"removedPackages,omitempty" yaml:"removedPackages,omitempty"`
}

// CompareSPDXFiles compares two SPDX JSON files using canonicalized package content.
func CompareSPDXFiles(fromPath, toPath string) (*SPDXCompareResult, error) {
	fromData, err := os.ReadFile(fromPath)
	if err != nil {
		return nil, fmt.Errorf("read from SPDX file: %w", err)
	}

	toData, err := os.ReadFile(toPath)
	if err != nil {
		return nil, fmt.Errorf("read to SPDX file: %w", err)
	}

	fromDoc, fromCanonicalHash, fromCount, err := parseAndCanonicalizeSPDX(fromData)
	if err != nil {
		return nil, fmt.Errorf("parse from SPDX file: %w", err)
	}

	toDoc, toCanonicalHash, toCount, err := parseAndCanonicalizeSPDX(toData)
	if err != nil {
		return nil, fmt.Errorf("parse to SPDX file: %w", err)
	}

	added, removed := diffSPDXPackages(fromDoc.Packages, toDoc.Packages)

	result := &SPDXCompareResult{
		FromPath:          fromPath,
		ToPath:            toPath,
		FromSHA256:        sha256Hex(fromData),
		ToSHA256:          sha256Hex(toData),
		FromCanonicalHash: fromCanonicalHash,
		ToCanonicalHash:   toCanonicalHash,
		FromPackageCount:  fromCount,
		ToPackageCount:    toCount,
		AddedPackages:     added,
		RemovedPackages:   removed,
	}

	result.Equal = result.FromCanonicalHash == result.ToCanonicalHash && len(added) == 0 && len(removed) == 0

	return result, nil
}

func parseAndCanonicalizeSPDX(spdxData []byte) (spdxComparableDoc, string, int, error) {
	var spdxDoc spdxComparableDoc
	if err := json.Unmarshal(spdxData, &spdxDoc); err != nil {
		return spdxComparableDoc{}, "", 0, err
	}

	for packageIndex := range spdxDoc.Packages {
		sort.Slice(spdxDoc.Packages[packageIndex].Checksum, func(i, j int) bool {
			left := spdxDoc.Packages[packageIndex].Checksum[i]
			right := spdxDoc.Packages[packageIndex].Checksum[j]
			if left.Algorithm == right.Algorithm {
				return left.ChecksumValue < right.ChecksumValue
			}
			return left.Algorithm < right.Algorithm
		})
	}

	sort.Slice(spdxDoc.Packages, func(i, j int) bool {
		left := spdxDoc.Packages[i]
		right := spdxDoc.Packages[j]

		if left.Name != right.Name {
			return left.Name < right.Name
		}
		if left.VersionInfo != right.VersionInfo {
			return left.VersionInfo < right.VersionInfo
		}
		if left.DownloadLocation != right.DownloadLocation {
			return left.DownloadLocation < right.DownloadLocation
		}
		return left.Supplier < right.Supplier
	})

	canonicalJSON, err := json.Marshal(spdxDoc)
	if err != nil {
		return spdxComparableDoc{}, "", 0, err
	}

	return spdxDoc, sha256Hex(canonicalJSON), len(spdxDoc.Packages), nil
}

func diffSPDXPackages(fromPkgs, toPkgs []spdxComparablePackage) ([]string, []string) {
	fromSet := make(map[string]struct{}, len(fromPkgs))
	toSet := make(map[string]struct{}, len(toPkgs))

	for _, pkg := range fromPkgs {
		fromSet[spdxPackageKey(pkg)] = struct{}{}
	}

	for _, pkg := range toPkgs {
		toSet[spdxPackageKey(pkg)] = struct{}{}
	}

	var added []string
	var removed []string

	for key := range toSet {
		if _, exists := fromSet[key]; !exists {
			added = append(added, key)
		}
	}

	for key := range fromSet {
		if _, exists := toSet[key]; !exists {
			removed = append(removed, key)
		}
	}

	sort.Strings(added)
	sort.Strings(removed)

	return added, removed
}

func spdxPackageKey(pkg spdxComparablePackage) string {
	return strings.Join([]string{pkg.Name, pkg.VersionInfo, pkg.DownloadLocation}, "|")
}
