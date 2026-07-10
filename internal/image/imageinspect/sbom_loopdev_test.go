package imageinspect

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeLoopAttacher stands in for imagedisc.LoopDev. AttachImageToLoopDev returns
// pre-canned partition nodes (which the test points at real ext4 fixture files,
// so debugfs runs against them) and records detach so the test can assert the
// device is always released.
type fakeLoopAttacher struct {
	loopDevPath    string
	partitionNodes []string
	attachErr      error
	detached       bool
}

func (f *fakeLoopAttacher) AttachImageToLoopDev(string) (string, []string, error) {
	if f.attachErr != nil {
		return "", nil, f.attachErr
	}
	return f.loopDevPath, f.partitionNodes, nil
}

func (f *fakeLoopAttacher) LoopSetupDelete(string) error {
	f.detached = true
	return nil
}

// buildExt4FixtureFile builds a standalone ext4 filesystem image populated with
// an SBOM at /usr/share/sbom/<fileName>, at the given path. Unlike the GPT disk
// in sbom_fsread_test.go, this is the bare partition (what a loop partition node
// exposes), so debugfs can read it directly. Skips when mke2fs is unavailable.
func buildExt4FixtureFile(t *testing.T, outPath, fileName string, sbom []byte) {
	t.Helper()

	mke2fs, err := exec.LookPath("mke2fs")
	if err != nil {
		t.Skipf("mke2fs not available: %v", err)
	}

	srcDir := filepath.Join(t.TempDir(), "src")
	if err := os.MkdirAll(filepath.Join(srcDir, "usr", "share", "sbom"), 0o755); err != nil {
		t.Fatalf("stage sbom dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "usr", "share", "sbom", fileName), sbom, 0o644); err != nil {
		t.Fatalf("stage sbom file: %v", err)
	}

	cmd := exec.Command(mke2fs, "-q", "-t", "ext4", "-F", "-d", srcDir, outPath, "32M")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("mke2fs: %v\n%s", err, out)
	}
}

func rootPartitionTable(index int) PartitionTableSummary {
	return PartitionTableSummary{
		Type:              "gpt",
		LogicalSectorSize: 512,
		Partitions: []PartitionSummary{
			{
				Index:      index,
				Name:       "ROOT",
				StartLBA:   2048,
				EndLBA:     2048 + 65535,
				Filesystem: &FilesystemSummary{Type: "ext4"},
			},
		},
	}
}

// TestInspectSBOMFromImageLoopDev_ReadsExt4ViaDebugfs exercises the loop-device
// reader end to end (minus the privileged losetup call): the fake attacher hands
// back a partition node that is a real ext4 file, and debugfs extracts the SBOM
// in place with no partition copy. Requires debugfs+mke2fs and pretends to be
// root via the geteuidForSBOM seam.
func TestInspectSBOMFromImageLoopDev_ReadsExt4ViaDebugfs(t *testing.T) {
	if _, err := exec.LookPath("debugfs"); err != nil {
		t.Skipf("debugfs not available: %v", err)
	}

	const fileName = "spdx_manifest.json"
	sbom := []byte(`{"packages":[{"name":"curl","versionInfo":"8.5.0","downloadLocation":"https://x/curl.deb"}]}`)

	// The partition node must end in "p3" to match root index 3 AND be a real
	// ext4 file debugfs can read, so name the fixture file accordingly.
	node := filepath.Join(t.TempDir(), "loop9p3")
	buildExt4FixtureFile(t, node, fileName, sbom)

	fake := &fakeLoopAttacher{loopDevPath: "/dev/loop9", partitionNodes: []string{node}}

	origNew, origEuid := newLoopDevForSBOM, geteuidForSBOM
	t.Cleanup(func() { newLoopDevForSBOM, geteuidForSBOM = origNew, origEuid })
	newLoopDevForSBOM = func() loopDevAttacher { return fake }
	geteuidForSBOM = func() int { return 0 }

	summary := inspectSBOMFromImageLoopDev("/does/not/matter.raw", rootPartitionTable(3))

	if !summary.Present {
		t.Fatalf("expected SBOM present via loop device; notes=%v", summary.Notes)
	}
	if summary.FileName != fileName {
		t.Errorf("file name = %q, want %q", summary.FileName, fileName)
	}
	if string(summary.Content) != string(sbom) {
		t.Errorf("content = %q, want %q", summary.Content, sbom)
	}
	if summary.PackageCount != 1 {
		t.Errorf("package count = %d, want 1", summary.PackageCount)
	}
	if !fake.detached {
		t.Errorf("expected loop device to be detached")
	}
}

func TestInspectSBOMFromImageLoopDev_SkipsWhenNotRoot(t *testing.T) {
	origNew, origEuid := newLoopDevForSBOM, geteuidForSBOM
	t.Cleanup(func() { newLoopDevForSBOM, geteuidForSBOM = origNew, origEuid })

	attached := false
	newLoopDevForSBOM = func() loopDevAttacher {
		attached = true
		return &fakeLoopAttacher{}
	}
	geteuidForSBOM = func() int { return 1000 }

	summary := inspectSBOMFromImageLoopDev("/img.raw", rootPartitionTable(3))

	if summary.Present {
		t.Fatalf("expected no SBOM when not root")
	}
	if attached {
		t.Fatalf("must not attach a loop device when not root")
	}
	if !containsNote(summary.Notes, "requires root") {
		t.Errorf("expected a requires-root note, got %v", summary.Notes)
	}
}

func TestInspectSBOMFromImageLoopDev_SkipsWhenNoExtRoot(t *testing.T) {
	origNew, origEuid := newLoopDevForSBOM, geteuidForSBOM
	t.Cleanup(func() { newLoopDevForSBOM, geteuidForSBOM = origNew, origEuid })

	attached := false
	newLoopDevForSBOM = func() loopDevAttacher {
		attached = true
		return &fakeLoopAttacher{}
	}
	geteuidForSBOM = func() int { return 0 }

	pt := PartitionTableSummary{
		Type:              "gpt",
		LogicalSectorSize: 512,
		Partitions: []PartitionSummary{
			{Index: 1, Name: "EFI", StartLBA: 2048, EndLBA: 4096, Filesystem: &FilesystemSummary{Type: "vfat"}},
		},
	}

	summary := inspectSBOMFromImageLoopDev("/img.raw", pt)

	if summary.Present {
		t.Fatalf("expected no SBOM for a non-ext root set")
	}
	if attached {
		t.Fatalf("must not attach a loop device when there is no ext root candidate")
	}
	if !containsNote(summary.Notes, "no ext2/3/4 root candidate") {
		t.Errorf("expected a no-ext-candidate note, got %v", summary.Notes)
	}
}

func TestPartitionNodeForIndex(t *testing.T) {
	nodes := []string{"/dev/loop0p1", "/dev/loop0p2", "/dev/loop0p3"}
	got, ok := partitionNodeForIndex(nodes, 3)
	if !ok || got != "/dev/loop0p3" {
		t.Fatalf("index 3 -> (%q,%v), want /dev/loop0p3,true", got, ok)
	}
	// p1 must not shadow p10-style suffixes: exact "pN" suffix only.
	if _, ok := partitionNodeForIndex(nodes, 4); ok {
		t.Fatalf("index 4 should not match any node")
	}
}

func containsNote(notes []string, substr string) bool {
	for _, n := range notes {
		if strings.Contains(n, substr) {
			return true
		}
	}
	return false
}
