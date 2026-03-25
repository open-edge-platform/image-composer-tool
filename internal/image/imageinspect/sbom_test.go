package imageinspect

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalSPDXSHA256_StableAcrossOrder(t *testing.T) {
	first := []byte(`{
  "packages": [
    {
      "name": "zlib",
      "versionInfo": "1.2.13",
      "downloadLocation": "https://example.com/zlib.rpm",
      "checksum": [
        {"algorithm": "SHA1", "checksumValue": "bbb"},
        {"algorithm": "SHA256", "checksumValue": "aaa"}
      ]
    },
    {
      "name": "acl",
      "versionInfo": "2.3.1",
      "downloadLocation": "https://example.com/acl.rpm",
      "checksum": [
        {"algorithm": "SHA256", "checksumValue": "ccc"}
      ]
    }
  ]
}`)

	second := []byte(`{
  "packages": [
    {
      "name": "acl",
      "versionInfo": "2.3.1",
      "downloadLocation": "https://example.com/acl.rpm",
      "checksum": [
        {"algorithm": "SHA256", "checksumValue": "ccc"}
      ]
    },
    {
      "name": "zlib",
      "versionInfo": "1.2.13",
      "downloadLocation": "https://example.com/zlib.rpm",
      "checksum": [
        {"algorithm": "SHA256", "checksumValue": "aaa"},
        {"algorithm": "SHA1", "checksumValue": "bbb"}
      ]
    }
  ]
}`)

	h1, n1, err := canonicalSPDXSHA256(first)
	if err != nil {
		t.Fatalf("canonicalSPDXSHA256(first) error: %v", err)
	}
	h2, n2, err := canonicalSPDXSHA256(second)
	if err != nil {
		t.Fatalf("canonicalSPDXSHA256(second) error: %v", err)
	}

	if h1 != h2 {
		t.Fatalf("expected canonical hash to be stable across order, got %q != %q", h1, h2)
	}
	if n1 != 2 || n2 != 2 {
		t.Fatalf("expected package count 2, got %d and %d", n1, n2)
	}
}

func TestCanonicalSPDXSHA256_DetectsContentChange(t *testing.T) {
	base := []byte(`{"packages":[{"name":"acl","versionInfo":"2.3.1"}]}`)
	changed := []byte(`{"packages":[{"name":"acl","versionInfo":"2.3.2"}]}`)

	h1, _, err := canonicalSPDXSHA256(base)
	if err != nil {
		t.Fatalf("canonicalSPDXSHA256(base) error: %v", err)
	}
	h2, _, err := canonicalSPDXSHA256(changed)
	if err != nil {
		t.Fatalf("canonicalSPDXSHA256(changed) error: %v", err)
	}

	if h1 == h2 {
		t.Fatalf("expected canonical hash to differ after content change")
	}
}

func TestCompareImages_SBOMAdded(t *testing.T) {
	from := &ImageSummary{}
	to := &ImageSummary{
		SBOM: SBOMSummary{
			Present:         true,
			FileName:        "spdx_manifest_deb_demo_20260101_000000.json",
			Format:          "spdx",
			CanonicalSHA256: "abc",
		},
	}

	res := CompareImages(from, to)
	if !res.Summary.SBOMChanged {
		t.Fatalf("expected Summary.SBOMChanged=true")
	}
	if res.Diff.SBOM == nil || !res.Diff.SBOM.Changed || res.Diff.SBOM.Added == nil {
		t.Fatalf("expected SBOM added diff, got %+v", res.Diff.SBOM)
	}
}

func TestCompareImages_SBOMCanonicalChanged(t *testing.T) {
	from := &ImageSummary{
		SBOM: SBOMSummary{
			Present:         true,
			FileName:        "spdx_manifest_rpm_demo_20260101_000000.json",
			Format:          "spdx",
			CanonicalSHA256: "aaa",
			PackageCount:    100,
		},
	}
	to := &ImageSummary{
		SBOM: SBOMSummary{
			Present:         true,
			FileName:        "spdx_manifest_rpm_demo_20260102_000000.json",
			Format:          "spdx",
			CanonicalSHA256: "bbb",
			PackageCount:    101,
		},
	}

	res := CompareImages(from, to)
	if res.Diff.SBOM == nil || !res.Diff.SBOM.Changed {
		t.Fatalf("expected SBOM diff, got %+v", res.Diff.SBOM)
	}
	if res.Diff.SBOM.CanonicalSHA256 == nil {
		t.Fatalf("expected canonical SBOM hash diff")
	}
	if res.Diff.SBOM.PackageCount == nil {
		t.Fatalf("expected SBOM package count diff")
	}
	if res.Equality.Class != EqualityDifferent {
		t.Fatalf("expected EqualityDifferent, got %s", res.Equality.Class)
	}
}

func TestCompareSPDXFiles(t *testing.T) {
	tmpDir := t.TempDir()
	fromPath := filepath.Join(tmpDir, "from.spdx.json")
	toPath := filepath.Join(tmpDir, "to.spdx.json")

	fromContent := `{"packages":[{"name":"acl","versionInfo":"2.3.1","downloadLocation":"https://example.com/acl.rpm"}]}`
	toContent := `{"packages":[{"name":"acl","versionInfo":"2.3.2","downloadLocation":"https://example.com/acl.rpm"}]}`

	if err := os.WriteFile(fromPath, []byte(fromContent), 0644); err != nil {
		t.Fatalf("write from SPDX file: %v", err)
	}
	if err := os.WriteFile(toPath, []byte(toContent), 0644); err != nil {
		t.Fatalf("write to SPDX file: %v", err)
	}

	result, err := CompareSPDXFiles(fromPath, toPath)
	if err != nil {
		t.Fatalf("CompareSPDXFiles error: %v", err)
	}

	if result.Equal {
		t.Fatalf("expected SPDX files to differ")
	}
	if result.FromPackageCount != 1 || result.ToPackageCount != 1 {
		t.Fatalf("expected package counts 1 and 1, got %d and %d", result.FromPackageCount, result.ToPackageCount)
	}
	if len(result.AddedPackages) != 1 || len(result.RemovedPackages) != 1 {
		t.Fatalf("expected one added and one removed package key, got added=%d removed=%d",
			len(result.AddedPackages), len(result.RemovedPackages))
	}
}

func TestPartitionStartOffset_DefaultSectorSize(t *testing.T) {
	pt := PartitionTableSummary{LogicalSectorSize: 512}
	part := PartitionSummary{StartLBA: 2048}

	off := partitionStartOffset(pt, part)
	if off != 2048*512 {
		t.Fatalf("unexpected offset: got %d, want %d", off, 2048*512)
	}
}

func TestPartitionStartOffset_PartitionSectorSizeOverride(t *testing.T) {
	pt := PartitionTableSummary{LogicalSectorSize: 512}
	part := PartitionSummary{StartLBA: 100, LogicalSectorSize: 4096}

	off := partitionStartOffset(pt, part)
	if off != 100*4096 {
		t.Fatalf("unexpected offset: got %d, want %d", off, 100*4096)
	}
}

func TestPartitionStartOffset_FallbackTo512(t *testing.T) {
	pt := PartitionTableSummary{LogicalSectorSize: 0}
	part := PartitionSummary{StartLBA: 33}

	off := partitionStartOffset(pt, part)
	if off != 33*512 {
		t.Fatalf("unexpected offset: got %d, want %d", off, 33*512)
	}
}

func TestPickSBOMFileNameFromFAT_PrefersSPDXManifest(t *testing.T) {
	entries := []fatDirEntry{
		{name: "notes.txt"},
		{name: "a.json"},
		{name: "spdx_manifest_demo_20260101_000000.json"},
		{name: "dir", isDir: true},
	}

	got, ok := pickSBOMFileNameFromFAT(entries)
	if !ok {
		t.Fatalf("expected to find sbom file")
	}
	if got != "spdx_manifest_demo_20260101_000000.json" {
		t.Fatalf("unexpected file: got %q", got)
	}
}

func TestPickSBOMFileNameFromNames_FallbackAndSorted(t *testing.T) {
	names := []string{"z.json", "b.json", "README.md"}

	got, ok := pickSBOMFileNameFromNames(names)
	if !ok {
		t.Fatalf("expected fallback json file")
	}
	if got != "b.json" {
		t.Fatalf("unexpected fallback file: got %q, want %q", got, "b.json")
	}
}

func TestPickSBOMFileNameFromNames_NoJSON(t *testing.T) {
	names := []string{"README.md", "manifest.txt"}

	_, ok := pickSBOMFileNameFromNames(names)
	if ok {
		t.Fatalf("expected no file match")
	}
}

func TestPickSBOMFileNameFromFS_PrefersSPDXManifest(t *testing.T) {
	tmp := t.TempDir()
	spdxFile := filepath.Join(tmp, "spdx_manifest_pkg.json")
	fallbackFile := filepath.Join(tmp, "a.json")
	dirPath := filepath.Join(tmp, "subdir")

	if err := os.WriteFile(spdxFile, []byte("{}"), 0644); err != nil {
		t.Fatalf("write spdx file: %v", err)
	}
	if err := os.WriteFile(fallbackFile, []byte("{}"), 0644); err != nil {
		t.Fatalf("write fallback file: %v", err)
	}
	if err := os.Mkdir(dirPath, 0755); err != nil {
		t.Fatalf("create subdir: %v", err)
	}

	spdxInfo, err := os.Stat(spdxFile)
	if err != nil {
		t.Fatalf("stat spdx file: %v", err)
	}
	fallbackInfo, err := os.Stat(fallbackFile)
	if err != nil {
		t.Fatalf("stat fallback file: %v", err)
	}
	dirInfo, err := os.Stat(dirPath)
	if err != nil {
		t.Fatalf("stat subdir: %v", err)
	}

	got, ok := pickSBOMFileNameFromFS([]os.FileInfo{dirInfo, fallbackInfo, spdxInfo})
	if !ok {
		t.Fatalf("expected sbom file from fs")
	}
	if got != "spdx_manifest_pkg.json" {
		t.Fatalf("unexpected file: got %q", got)
	}
}

func TestRankRootPartitionCandidates_PrefersRootAndExt4(t *testing.T) {
	pt := PartitionTableSummary{
		Partitions: []PartitionSummary{
			{
				Index: 1, Name: "efi", SizeBytes: 100,
				Filesystem: &FilesystemSummary{Type: "vfat"},
			},
			{
				Index: 2, Name: "rootfs", SizeBytes: 1000,
				Filesystem: &FilesystemSummary{Type: "ext4"},
			},
			{
				Index: 3, Name: "data", SizeBytes: 2000,
				Filesystem: &FilesystemSummary{Type: "xfs"},
			},
		},
	}

	got := rankRootPartitionCandidates(pt)
	if len(got) != 3 {
		t.Fatalf("unexpected candidate count: got %d", len(got))
	}
	if got[0] != 1 {
		t.Fatalf("expected rootfs partition index position 1 to rank first, got %d", got[0])
	}
}

func TestRankRootPartitionCandidates_TieBreakBySizeThenIndex(t *testing.T) {
	pt := PartitionTableSummary{
		Partitions: []PartitionSummary{
			{Index: 5, Name: "system", SizeBytes: 100, Filesystem: &FilesystemSummary{Type: "ext4"}},
			{Index: 2, Name: "system", SizeBytes: 200, Filesystem: &FilesystemSummary{Type: "ext4"}},
			{Index: 1, Name: "system", SizeBytes: 200, Filesystem: &FilesystemSummary{Type: "ext4"}},
		},
	}

	got := rankRootPartitionCandidates(pt)
	if len(got) != 3 {
		t.Fatalf("unexpected candidate count: got %d", len(got))
	}

	if got[0] != 2 || got[1] != 1 || got[2] != 0 {
		t.Fatalf("unexpected ordering: got %v, want [2 1 0]", got)
	}
}

func TestCollectJSONFilesFromDir_RecursiveAndSorted(t *testing.T) {
	tmp := t.TempDir()
	nested := filepath.Join(tmp, "nested")
	if err := os.Mkdir(nested, 0755); err != nil {
		t.Fatalf("create nested dir: %v", err)
	}

	jsonA := filepath.Join(tmp, "b.json")
	jsonB := filepath.Join(nested, "a.json")
	txt := filepath.Join(tmp, "ignore.txt")

	for _, filePath := range []string{jsonA, jsonB, txt} {
		if err := os.WriteFile(filePath, []byte("x"), 0644); err != nil {
			t.Fatalf("write test file %s: %v", filePath, err)
		}
	}

	got := collectJSONFilesFromDir(tmp)
	if len(got) != 2 {
		t.Fatalf("unexpected json file count: got %d, want %d", len(got), 2)
	}
	seen := map[string]bool{}
	for _, filePath := range got {
		seen[filepath.Base(filePath)] = true
	}
	if !seen["a.json"] || !seen["b.json"] {
		t.Fatalf("unexpected json files: %v", got)
	}
}

func TestFindSBOMFileInRawPartition_UnsupportedFilesystem(t *testing.T) {
	_, _, err := findSBOMFileInRawPartition(bytes.NewReader(nil), 0, 0, "ntfs")
	if err == nil {
		t.Fatalf("expected error for unsupported filesystem")
	}
	if !strings.Contains(err.Error(), "not supported for raw SPDX extraction yet") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFindSBOMFileInRawPartition_VFATOpenError(t *testing.T) {
	img := bytes.NewReader(make([]byte, 512)) // invalid FAT boot sector signature

	_, _, err := findSBOMFileInRawPartition(img, 0, 512, "vfat")
	if err == nil {
		t.Fatalf("expected FAT open error")
	}
	if !strings.Contains(err.Error(), "open FAT volume") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadFileFromRawPartition_UnsupportedFilesystem(t *testing.T) {
	_, err := readFileFromRawPartition(bytes.NewReader(nil), 0, 0, "unknownfs", "/usr/share/sbom/file.json")
	if err == nil {
		t.Fatalf("expected error for unsupported filesystem")
	}
	if !strings.Contains(err.Error(), "not supported for raw file reads") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadFileFromRawPartition_VFATOpenError(t *testing.T) {
	img := bytes.NewReader(make([]byte, 512)) // invalid FAT boot sector signature

	_, err := readFileFromRawPartition(img, 0, 512, "vfat", "/usr/share/sbom/spdx_manifest.json")
	if err == nil {
		t.Fatalf("expected FAT open error")
	}
	if !strings.Contains(err.Error(), "open FAT volume") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadSBOMFromRawPartition_UnsupportedFilesystem(t *testing.T) {
	_, _, _, err := readSBOMFromRawPartition(bytes.NewReader(nil), 0, 0, "squashfs")
	if err == nil {
		t.Fatalf("expected error for unsupported filesystem")
	}
	if !strings.Contains(err.Error(), "not supported for raw SPDX extraction yet") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecode83Name_NoExtension(t *testing.T) {
	got := decode83Name([]byte("KERNEL  " + "   "))
	if got != "KERNEL" {
		t.Fatalf("unexpected 8.3 decode: got %q", got)
	}
}

func TestIsEOC_Branches(t *testing.T) {
	v32 := &fatVol{kind: fat32}
	if v32.isEOC(0x0FFFFFF7) {
		t.Fatalf("did not expect EOC for FAT32 pre-threshold")
	}
	if !v32.isEOC(0x0FFFFFF8) {
		t.Fatalf("expected EOC for FAT32 threshold")
	}

	v16 := &fatVol{kind: fat16}
	if v16.isEOC(0xFFF7) {
		t.Fatalf("did not expect EOC for FAT16 pre-threshold")
	}
	if !v16.isEOC(0xFFF8) {
		t.Fatalf("expected EOC for FAT16 threshold")
	}
}

func TestClusterOff_Branches(t *testing.T) {
	v := &fatVol{dataStart: 4096, clusterSize: 1024}
	if got := v.clusterOff(0); got != 4096 {
		t.Fatalf("unexpected offset for cluster <2: got %d", got)
	}
	if got := v.clusterOff(2); got != 4096 {
		t.Fatalf("unexpected offset for cluster 2: got %d", got)
	}
	if got := v.clusterOff(5); got != 4096+3*1024 {
		t.Fatalf("unexpected offset for cluster 5: got %d", got)
	}
}

func TestFatEntry_FAT16AndFAT32(t *testing.T) {
	buf := make([]byte, 128)

	// FAT16 entry for cluster 3 at offset 6
	binary.LittleEndian.PutUint16(buf[6:8], 0x1234)
	v16 := &fatVol{kind: fat16, fatStart: 0, r: bytes.NewReader(buf)}
	entry16, err := v16.fatEntry(3)
	if err != nil {
		t.Fatalf("fat16 entry error: %v", err)
	}
	if entry16 != 0x1234 {
		t.Fatalf("unexpected fat16 entry: got 0x%x", entry16)
	}

	// FAT32 entry for cluster 4 at offset 16; upper 4 bits must be masked
	binary.LittleEndian.PutUint32(buf[16:20], 0xF2345678)
	v32 := &fatVol{kind: fat32, fatStart: 0, r: bytes.NewReader(buf)}
	entry32, err := v32.fatEntry(4)
	if err != nil {
		t.Fatalf("fat32 entry error: %v", err)
	}
	if entry32 != 0x02345678 {
		t.Fatalf("unexpected fat32 masked entry: got 0x%x", entry32)
	}
}

func TestParseDirEntries_SkipsDeletedVolumeAndDots(t *testing.T) {
	buf := make([]byte, 32*5)

	// deleted entry
	buf[0] = 0xE5

	// volume label entry (should be skipped)
	copy(buf[32:43], []byte("VOLLABEL   "))
	buf[32+11] = 0x08

	// dot entry (should be skipped)
	copy(buf[64:75], []byte(".          "))
	buf[64+11] = 0x10

	// valid file entry
	copy(buf[96:107], []byte("README  TXT"))
	buf[96+11] = 0x20
	binary.LittleEndian.PutUint16(buf[96+26:96+28], 7)
	binary.LittleEndian.PutUint32(buf[96+28:96+32], 42)

	// end marker
	buf[128] = 0x00

	entries, err := parseDirEntries(buf)
	if err != nil {
		t.Fatalf("parseDirEntries error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("unexpected entry count: got %d", len(entries))
	}
	if entries[0].name != "README.TXT" {
		t.Fatalf("unexpected entry name: %q", entries[0].name)
	}
	if entries[0].size != 42 {
		t.Fatalf("unexpected entry size: %d", entries[0].size)
	}
}

func TestReadRootDir_FAT16(t *testing.T) {
	buf := make([]byte, 512)
	copy(buf[0:11], []byte("CONFIG  CFG"))
	buf[11] = 0x20
	binary.LittleEndian.PutUint16(buf[26:28], 3)
	binary.LittleEndian.PutUint32(buf[28:32], 11)

	v := &fatVol{
		r:              bytes.NewReader(buf),
		kind:           fat16,
		bytsPerSec:     512,
		rootDirStart:   0,
		rootDirSectors: 1,
	}

	entries, err := v.readRootDir()
	if err != nil {
		t.Fatalf("readRootDir error: %v", err)
	}
	if len(entries) != 1 || entries[0].name != "CONFIG.CFG" {
		t.Fatalf("unexpected root dir entries: %+v", entries)
	}
}

func TestReadDirFromCluster_DetectsLoop(t *testing.T) {
	buf := make([]byte, 256)
	// FAT16 cluster 2 -> 2 (self-loop) at fat offset 4
	binary.LittleEndian.PutUint16(buf[4:6], 2)

	v := &fatVol{
		r:           bytes.NewReader(buf),
		kind:        fat16,
		fatStart:    0,
		dataStart:   128,
		clusterSize: 32,
	}

	_, err := v.readDirFromCluster(2)
	if err == nil {
		t.Fatalf("expected FAT loop detection error")
	}
	if !strings.Contains(err.Error(), "FAT loop detected") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInspectSBOMFromImageRaw_NoRootCandidate(t *testing.T) {
	summary := inspectSBOMFromImageRaw(bytes.NewReader(nil), PartitionTableSummary{})
	if summary.Present {
		t.Fatalf("expected no SBOM present")
	}
	if len(summary.Notes) == 0 || !strings.Contains(summary.Notes[0], "root partition candidate not found") {
		t.Fatalf("unexpected notes: %v", summary.Notes)
	}
}

func TestInspectSBOMFromImageRaw_UnsupportedFilesystem(t *testing.T) {
	pt := PartitionTableSummary{
		LogicalSectorSize: 512,
		Partitions: []PartitionSummary{
			{
				Index:     1,
				Name:      "rootfs",
				StartLBA:  2048,
				SizeBytes: 4096,
				Filesystem: &FilesystemSummary{
					Type: "unknownfs",
				},
			},
		},
	}

	summary := inspectSBOMFromImageRaw(bytes.NewReader(nil), pt)
	if summary.Present {
		t.Fatalf("expected no SBOM present")
	}
	if len(summary.Notes) < 2 {
		t.Fatalf("expected at least two notes, got %v", summary.Notes)
	}
	if !strings.Contains(summary.Notes[0], "failed to read SBOM from partition") {
		t.Fatalf("unexpected first note: %q", summary.Notes[0])
	}
	if !strings.Contains(summary.Notes[len(summary.Notes)-1], "SBOM not found") {
		t.Fatalf("unexpected final note: %q", summary.Notes[len(summary.Notes)-1])
	}
}

func TestCompareSPDXFiles_Equal(t *testing.T) {
	tmpDir := t.TempDir()
	fromPath := filepath.Join(tmpDir, "from.spdx.json")
	toPath := filepath.Join(tmpDir, "to.spdx.json")

	content := `{"packages":[{"name":"acl","versionInfo":"2.3.1","downloadLocation":"https://example.com/acl.rpm"}]}`
	if err := os.WriteFile(fromPath, []byte(content), 0644); err != nil {
		t.Fatalf("write from SPDX file: %v", err)
	}
	if err := os.WriteFile(toPath, []byte(content), 0644); err != nil {
		t.Fatalf("write to SPDX file: %v", err)
	}

	result, err := CompareSPDXFiles(fromPath, toPath)
	if err != nil {
		t.Fatalf("CompareSPDXFiles error: %v", err)
	}
	if !result.Equal {
		t.Fatalf("expected SPDX files to be equal")
	}
	if len(result.AddedPackages) != 0 || len(result.RemovedPackages) != 0 {
		t.Fatalf("expected no package deltas, got added=%d removed=%d", len(result.AddedPackages), len(result.RemovedPackages))
	}
}

func TestCompareSPDXFiles_FromReadError(t *testing.T) {
	_, err := CompareSPDXFiles("/definitely/missing/from.json", "/definitely/missing/to.json")
	if err == nil {
		t.Fatalf("expected read error")
	}
	if !strings.Contains(err.Error(), "read from SPDX file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCanonicalSPDXSHA256_InvalidJSON(t *testing.T) {
	_, _, err := canonicalSPDXSHA256([]byte("not-json"))
	if err == nil {
		t.Fatalf("expected canonicalization error for invalid json")
	}
}
