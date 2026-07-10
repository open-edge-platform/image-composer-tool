package imageinspect

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	diskfs "github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/partition/gpt"
)

// buildExt4ImageWithSBOM creates a small GPT disk image with a single ext4
// partition that contains an SBOM at /usr/share/sbom/<fileName>, and returns its
// path.
//
// The ext4 partition is built with `mke2fs -d` (populate-from-directory), which
// needs no root and no loop mount, and the GPT wrapper is written with go-diskfs.
// This exercises exactly the in-place reader path the inspector now prefers:
// GetFilesystem on a partitioned disk, then ReadDir/OpenFile within it. It skips
// (rather than fails) when mke2fs is unavailable so the suite stays hermetic on
// hosts without e2fsprogs.
func buildExt4ImageWithSBOM(t *testing.T, fileName string, sbom []byte) string {
	t.Helper()

	mke2fs, err := exec.LookPath("mke2fs")
	if err != nil {
		t.Skipf("mke2fs not available: %v", err)
	}

	const (
		sectorSize     = int64(512)
		partStartLBA   = int64(2048)
		partSizeBytes  = int64(32 * 1024 * 1024) // 32 MiB ext4 partition
		gptTailSectors = int64(34)               // secondary GPT reservation
		diskSize       = partStartLBA*sectorSize + partSizeBytes + gptTailSectors*sectorSize
	)

	tmp := t.TempDir()

	// Populate an ext4 image from a staging directory tree, no mount required.
	srcDir := filepath.Join(tmp, "src")
	if err := os.MkdirAll(filepath.Join(srcDir, "usr", "share", "sbom"), 0o755); err != nil {
		t.Fatalf("stage sbom dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "usr", "share", "sbom", fileName), sbom, 0o644); err != nil {
		t.Fatalf("stage sbom file: %v", err)
	}

	partImg := filepath.Join(tmp, "part.ext4")
	cmd := exec.Command(mke2fs, "-q", "-t", "ext4", "-F", "-d", srcDir, partImg, "32M")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("mke2fs: %v\n%s", err, out)
	}
	partBytes, err := os.ReadFile(partImg)
	if err != nil {
		t.Fatalf("read ext4 partition image: %v", err)
	}

	// Assemble the full GPT disk: zeroed backing file of the right size, a GPT
	// with one Linux-filesystem partition, then the ext4 bytes at the partition
	// offset.
	imgPath := filepath.Join(tmp, "disk.img")
	if err := os.WriteFile(imgPath, make([]byte, diskSize), 0o644); err != nil {
		t.Fatalf("create backing file: %v", err)
	}

	d, err := diskfs.Open(imgPath)
	if err != nil {
		t.Fatalf("open disk: %v", err)
	}
	table := &gpt.Table{
		LogicalSectorSize:  int(sectorSize),
		PhysicalSectorSize: int(sectorSize),
		Partitions: []*gpt.Partition{
			{
				Start: uint64(partStartLBA),
				End:   uint64(partStartLBA + partSizeBytes/sectorSize - 1),
				Type:  gpt.LinuxFilesystem,
				Name:  "ROOT",
			},
		},
	}
	if err := d.Partition(table); err != nil {
		_ = d.Close()
		t.Fatalf("write GPT: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("close disk after partition: %v", err)
	}

	// Splice the ext4 partition content into the partition region.
	f, err := os.OpenFile(imgPath, os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("reopen disk for splice: %v", err)
	}
	if _, err := f.WriteAt(partBytes, partStartLBA*sectorSize); err != nil {
		_ = f.Close()
		t.Fatalf("splice ext4 into partition: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close disk after splice: %v", err)
	}

	return imgPath
}

// TestInspectSBOM_ReadsExt4InPlace verifies the inspector extracts an embedded
// SBOM from an ext4 root partition WITHOUT copying the whole partition to a temp
// file — the go-diskfs in-place reader is the primary path. This is the exact
// scenario that previously failed with "no space left on device" when the raw
// path ran first on a multi-GB root.
func TestInspectSBOM_ReadsExt4InPlace(t *testing.T) {
	const fileName = "spdx_manifest.json"
	sbom := []byte(`{"packages":[{"name":"curl","versionInfo":"8.5.0","downloadLocation":"https://x/curl.deb"}]}`)

	imgPath := buildExt4ImageWithSBOM(t, fileName, sbom)

	summary, err := NewDiskfsInspectorWithOptions(false, true).Inspect(imgPath)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}

	if !summary.SBOM.Present {
		t.Fatalf("expected SBOM to be found via the in-place reader; notes=%v", summary.SBOM.Notes)
	}
	if summary.SBOM.FileName != fileName {
		t.Errorf("SBOM file name = %q, want %q", summary.SBOM.FileName, fileName)
	}
	if string(summary.SBOM.Content) != string(sbom) {
		t.Errorf("SBOM content = %q, want %q", summary.SBOM.Content, sbom)
	}
	if summary.SBOM.PackageCount != 1 {
		t.Errorf("SBOM package count = %d, want 1", summary.SBOM.PackageCount)
	}
}
