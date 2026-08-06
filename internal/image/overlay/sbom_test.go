package overlay

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/config/manifest"
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

func TestBaselineExternalSBOMPath(t *testing.T) {
	cases := []struct {
		name string
		tmpl *config.ImageTemplate
		want string
	}{
		{"nil template", nil, ""},
		{"nil baseline", &config.ImageTemplate{}, ""},
		{"nil source", &config.ImageTemplate{Baseline: &config.Baseline{}}, ""},
		{"unset sbomPath", &config.ImageTemplate{Baseline: &config.Baseline{Source: &config.BaselineSource{Path: "/b.raw"}}}, ""},
		{"trimmed sbomPath", &config.ImageTemplate{Baseline: &config.Baseline{Source: &config.BaselineSource{SBOMPath: "  /base.spdx.json  "}}}, "/base.spdx.json"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := baselineExternalSBOMPath(c.tmpl); got != c.want {
				t.Errorf("baselineExternalSBOMPath = %q, want %q", got, c.want)
			}
		})
	}
}

func TestReadExternalBaseSBOM(t *testing.T) {
	dir := t.TempDir()

	present := filepath.Join(dir, "base.json")
	if err := os.WriteFile(present, []byte(`{"packages":[]}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	empty := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatalf("write empty: %v", err)
	}

	if data, ok := readExternalBaseSBOM(present); !ok || string(data) != `{"packages":[]}` {
		t.Errorf("present: got (%q,%v), want the file contents and true", data, ok)
	}
	if _, ok := readExternalBaseSBOM(empty); ok {
		t.Error("empty file must report ok=false")
	}
	if _, ok := readExternalBaseSBOM(filepath.Join(dir, "missing.json")); ok {
		t.Error("absent file must report ok=false")
	}
}

// spdxNames parses an SPDX document's bytes and returns the package names it lists.
func spdxNames(t *testing.T, data []byte) []string {
	t.Helper()
	var doc struct {
		Packages []struct {
			Name string `json:"name"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse spdx: %v", err)
	}
	names := make([]string, 0, len(doc.Packages))
	for _, p := range doc.Packages {
		names = append(names, p.Name)
	}
	return names
}

// TestResolveOverlayBaseSBOM_ExternalWins verifies that when baseline.source.sbomPath
// is set and readable, its bytes are chosen as the merge base — while the output
// name still tracks the inherited SBOM so the merged doc replaces it in place.
func TestResolveOverlayBaseSBOM_ExternalWins(t *testing.T) {
	const inheritedName = "spdx_manifest_base.json"
	root := mkSBOMDir(t, map[string]string{inheritedName: `{"packages":[{"name":"bash"}]}`})

	extPath := filepath.Join(t.TempDir(), "external.spdx.json")
	if err := os.WriteFile(extPath, []byte(`{"packages":[{"name":"libc6"}]}`), 0o644); err != nil {
		t.Fatalf("write external: %v", err)
	}
	tmpl := &config.ImageTemplate{Baseline: &config.Baseline{Source: &config.BaselineSource{SBOMPath: extPath}}}

	name, data, found := resolveOverlayBaseSBOM(tmpl, root)
	if !found {
		t.Fatal("expected a base SBOM to be found")
	}
	if name != inheritedName {
		t.Errorf("output name = %q, want inherited name %q (replace in place)", name, inheritedName)
	}
	if got := spdxNames(t, data); len(got) != 1 || got[0] != "libc6" {
		t.Errorf("base SBOM = %v, want the external one [libc6]", got)
	}
}

// TestResolveOverlayBaseSBOM_FallsBackToInheritedWhenExternalMissing verifies that a
// set-but-unreadable external path does not error and falls back to the inherited SBOM.
func TestResolveOverlayBaseSBOM_FallsBackToInheritedWhenExternalMissing(t *testing.T) {
	const inheritedName = "spdx_manifest_base.json"
	root := mkSBOMDir(t, map[string]string{inheritedName: `{"packages":[{"name":"bash"}]}`})

	tmpl := &config.ImageTemplate{Baseline: &config.Baseline{Source: &config.BaselineSource{SBOMPath: filepath.Join(t.TempDir(), "missing.json")}}}

	name, data, found := resolveOverlayBaseSBOM(tmpl, root)
	if !found {
		t.Fatal("expected fallback to the inherited SBOM")
	}
	if name != inheritedName {
		t.Errorf("output name = %q, want %q", name, inheritedName)
	}
	if got := spdxNames(t, data); len(got) != 1 || got[0] != "bash" {
		t.Errorf("base SBOM = %v, want the inherited one [bash]", got)
	}
}

// TestResolveOverlayBaseSBOM_NoBaseAvailable verifies that with no external field and
// no inherited SBOM, found is false and the default filename is returned (delta-only).
func TestResolveOverlayBaseSBOM_NoBaseAvailable(t *testing.T) {
	root := t.TempDir() // no usr/share/sbom
	tmpl := &config.ImageTemplate{Baseline: &config.Baseline{Source: &config.BaselineSource{Path: "/b.raw"}}}

	name, _, found := resolveOverlayBaseSBOM(tmpl, root)
	if found {
		t.Error("expected no base SBOM available")
	}
	if name != manifest.DefaultSPDXFile {
		t.Errorf("output name = %q, want default %q", name, manifest.DefaultSPDXFile)
	}
}

// TestResolveOverlayBaseSBOM_BackwardCompatibleWhenFieldAbsent confirms that omitting
// the new field keeps the exact prior behavior: the inherited SBOM is used as-is.
func TestResolveOverlayBaseSBOM_BackwardCompatibleWhenFieldAbsent(t *testing.T) {
	const inheritedName = "spdx_manifest_base.json"
	root := mkSBOMDir(t, map[string]string{inheritedName: `{"packages":[{"name":"bash"}]}`})

	tmpl := &config.ImageTemplate{Baseline: &config.Baseline{Source: &config.BaselineSource{Path: "/b.raw"}}}

	name, data, found := resolveOverlayBaseSBOM(tmpl, root)
	if !found || name != inheritedName {
		t.Fatalf("resolve = (%q,%v), want inherited %q,true", name, found, inheritedName)
	}
	if got := spdxNames(t, data); len(got) != 1 || got[0] != "bash" {
		t.Errorf("base SBOM = %v, want [bash]", got)
	}
}
