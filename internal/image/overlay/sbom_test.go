package overlay

import (
	"os"
	"path/filepath"
	"testing"
)

// mkSBOMDir creates <root>/usr/share/sbom populated with the named files (name ->
// contents) and returns the root. It backs the baseline-SBOM discovery tests,
// which exercise the fix for the shadowing bug where a delta-only spdx_manifest
// was picked over the baseline's real inventory.
func mkSBOMDir(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	sbomDir := filepath.Join(root, "usr", "share", "sbom")
	if err := os.MkdirAll(sbomDir, 0o755); err != nil {
		t.Fatalf("mkdir sbom dir: %v", err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(sbomDir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return root
}

func TestReadBaselineSBOM_ReadsTimestampedInventory(t *testing.T) {
	const wantName = "spdx_manifest_deb_ubuntu_20260101_120000.json"
	const wantBody = `{"packages":[{"name":"libc6"}]}`
	root := mkSBOMDir(t, map[string]string{wantName: wantBody})

	name, data, ok := readBaselineSBOM(root)
	if !ok {
		t.Fatalf("expected to find baseline SBOM")
	}
	if name != wantName {
		t.Errorf("name = %q, want %q", name, wantName)
	}
	if string(data) != wantBody {
		t.Errorf("data = %q, want %q", data, wantBody)
	}
}

func TestReadBaselineSBOM_AbsentWhenNoSBOMDir(t *testing.T) {
	root := t.TempDir() // no usr/share/sbom
	if _, _, ok := readBaselineSBOM(root); ok {
		t.Fatalf("expected no baseline SBOM when the directory is absent")
	}
}

func TestReadBaselineSBOM_AbsentWhenNoJSON(t *testing.T) {
	root := mkSBOMDir(t, map[string]string{"README.txt": "not json"})
	if _, _, ok := readBaselineSBOM(root); ok {
		t.Fatalf("expected no baseline SBOM when the directory holds no JSON")
	}
}

func TestPickBaselineSBOMName_PrefersSpdxManifestOverOtherJSON(t *testing.T) {
	// A create-mode timestamped manifest sits beside an unrelated JSON; the
	// spdx_manifest* file must win regardless of lexical order.
	root := mkSBOMDir(t, map[string]string{
		"aaa-other.json": "{}",
		"spdx_manifest_deb_ubuntu_20260101_120000.json": "{}",
	})
	entries, err := os.ReadDir(filepath.Join(root, "usr", "share", "sbom"))
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	name, ok := pickBaselineSBOMName(entries)
	if !ok || name != "spdx_manifest_deb_ubuntu_20260101_120000.json" {
		t.Fatalf("pick = (%q,%v), want the spdx_manifest file", name, ok)
	}
}

func TestPickBaselineSBOMName_DeterministicAmongSpdxManifests(t *testing.T) {
	// If multiple spdx_manifest* files exist, the pick is the lexicographically
	// smallest so the choice is stable across runs.
	root := mkSBOMDir(t, map[string]string{
		"spdx_manifest_b.json": "{}",
		"spdx_manifest_a.json": "{}",
	})
	entries, err := os.ReadDir(filepath.Join(root, "usr", "share", "sbom"))
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	name, ok := pickBaselineSBOMName(entries)
	if !ok || name != "spdx_manifest_a.json" {
		t.Fatalf("pick = (%q,%v), want spdx_manifest_a.json", name, ok)
	}
}
