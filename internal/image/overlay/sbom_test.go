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

// TestReadBaselineSBOM_RejectsSymlinkedDirChain guards against an untrusted baseline
// that points /usr/share/sbom (an ancestor of the manifest file) at a host directory:
// the read must refuse before os.ReadDir would list that host dir and ingest an
// arbitrary host JSON into the emitted SBOM.
func TestReadBaselineSBOM_RejectsSymlinkedDirChain(t *testing.T) {
	// A host directory holding a JSON the attacker wants ingested.
	hostDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(hostDir, "spdx_manifest.json"), []byte(`{"packages":[{"name":"host-secret"}]}`), 0o644); err != nil {
		t.Fatalf("write host json: %v", err)
	}
	// Baseline root whose usr/share/sbom is a symlink to the host dir.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "usr", "share"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(hostDir, filepath.Join(root, "usr", "share", "sbom")); err != nil {
		t.Fatalf("symlink sbom dir: %v", err)
	}
	if _, _, ok := readBaselineSBOM(root); ok {
		t.Fatal("must refuse to read the inherited SBOM through a symlinked directory chain")
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

func TestOverlayDeltaTempName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"spdx_manifest.json", "spdx_manifest.delta.json"},
		{"spdx_manifest_deb_repo_20260101_120000.json", "spdx_manifest_deb_repo_20260101_120000.delta.json"},
		{"noext", "noext.delta"},
	}
	for _, c := range cases {
		if got := overlayDeltaTempName(c.in); got != c.want {
			t.Errorf("overlayDeltaTempName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// withOverlaySBOMTempDir points config.TempDir() at a per-test sandbox (so staged
// SBOMs land somewhere writable) and restores the global config and the mutable
// manifest.DefaultSPDXFile afterward.
func withOverlaySBOMTempDir(t *testing.T) {
	t.Helper()
	prev := config.Global()
	prevDefault := manifest.DefaultSPDXFile
	cfg := *prev
	cfg.TempDir = filepath.Join(t.TempDir(), "tmp")
	config.SetGlobal(&cfg)
	t.Cleanup(func() {
		config.SetGlobal(prev)
		manifest.DefaultSPDXFile = prevDefault
	})
}

// TestStageOverlaySBOMArtifacts_WritesDeltaAndCompleteWithBase verifies that with a
// base SBOM available, BOTH the delta (contributed only) and complete (base+delta)
// SBOM documents are staged, with the expected contents, and DefaultSPDXFile is set
// to the complete SBOM's name for the in-image embed.
func TestStageOverlaySBOMArtifacts_WritesDeltaAndCompleteWithBase(t *testing.T) {
	withOverlaySBOMTempDir(t)

	const inheritedName = "spdx_manifest_base.json"
	root := mkSBOMDir(t, map[string]string{inheritedName: `{"packages":[{"name":"libc6","versionInfo":"2.39"}]}`})

	plan := &ResolutionPlan{ToInstall: []ResolvedPackage{{Name: "tree", Version: "2.1.1", Arch: "amd64", Type: "deb"}}}

	arts, err := stageOverlaySBOMArtifacts(&config.ImageTemplate{}, &BaselineInfo{PackageType: "deb"}, root, plan)
	if err != nil {
		t.Fatalf("stageOverlaySBOMArtifacts: %v", err)
	}

	// Delta = only the contributed package.
	deltaData, err := os.ReadFile(arts.deltaPath)
	if err != nil {
		t.Fatalf("read delta: %v", err)
	}
	if got := spdxNames(t, deltaData); len(got) != 1 || got[0] != "tree" {
		t.Errorf("delta SBOM names = %v, want just [tree]", got)
	}

	// Complete = base (libc6) + delta (tree).
	completeData, err := os.ReadFile(arts.completePath)
	if err != nil {
		t.Fatalf("read complete: %v", err)
	}
	got := spdxNames(t, completeData)
	if !contains(got, "libc6") || !contains(got, "tree") {
		t.Errorf("complete SBOM names = %v, want both libc6 (base) and tree (delta)", got)
	}
	// A real base+delta union: the complete sidecar must be emitted (not skipped).
	if arts.completeIsDeltaOnly {
		t.Error("completeIsDeltaOnly must be false when a base SBOM was merged")
	}

	// The complete SBOM is staged under the inherited name (replace in place), and
	// DefaultSPDXFile tracks it so the in-image embed / complete sidecar match.
	if filepath.Base(arts.completePath) != inheritedName {
		t.Errorf("complete path = %q, want base name %q", arts.completePath, inheritedName)
	}
	if manifest.DefaultSPDXFile != inheritedName {
		t.Errorf("DefaultSPDXFile = %q, want %q", manifest.DefaultSPDXFile, inheritedName)
	}
}

// TestStageOverlaySBOMArtifacts_DeltaOnlyWhenNoBase verifies that with no base SBOM
// both files are still staged (the complete SBOM degrades to the delta content and
// the build does not fail), and that completeIsDeltaOnly is set so the caller skips
// the redundant build-dir complete sidecar.
func TestStageOverlaySBOMArtifacts_DeltaOnlyWhenNoBase(t *testing.T) {
	withOverlaySBOMTempDir(t)

	root := t.TempDir() // no usr/share/sbom -> no inherited base
	plan := &ResolutionPlan{ToInstall: []ResolvedPackage{{Name: "tree", Version: "2.1.1", Type: "deb"}}}

	arts, err := stageOverlaySBOMArtifacts(&config.ImageTemplate{}, &BaselineInfo{PackageType: "deb"}, root, plan)
	if err != nil {
		t.Fatalf("stageOverlaySBOMArtifacts: %v", err)
	}

	for _, p := range []string{arts.deltaPath, arts.completePath} {
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			t.Fatalf("read %s: %v", p, rerr)
		}
		if got := spdxNames(t, data); len(got) != 1 || got[0] != "tree" {
			t.Errorf("%s names = %v, want just [tree]", filepath.Base(p), got)
		}
	}
	// With no base, the complete SBOM degraded to delta-only: the flag must be set so
	// the emit step skips the redundant complete sidecar.
	if !arts.completeIsDeltaOnly {
		t.Error("completeIsDeltaOnly must be true when no base SBOM is available")
	}
	// With no base, the complete SBOM uses the default name.
	if manifest.DefaultSPDXFile != filepath.Base(arts.completePath) {
		t.Errorf("DefaultSPDXFile = %q, want %q", manifest.DefaultSPDXFile, filepath.Base(arts.completePath))
	}
}

// TestEmitOverlayArtifact_SkipsCompleteSidecarWhenDeltaOnly verifies the emit step
// writes the delta sidecar but NOT the complete sidecar when the complete SBOM
// degraded to delta-only (no base SBOM), and still writes both when a real
// base+delta complete SBOM exists.
func TestEmitOverlayArtifact_SkipsCompleteSidecarWhenDeltaOnly(t *testing.T) {
	tests := []struct {
		name         string
		deltaOnly    bool
		wantComplete bool
	}{
		{"delta-only skips complete sidecar", true, false},
		{"real complete emits both", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Point WorkDir + TempDir at a per-test sandbox so the emit build dir and
			// staged files live somewhere writable.
			prev := config.Global()
			cfg := *prev
			sandbox := t.TempDir()
			cfg.WorkDir = filepath.Join(sandbox, "work")
			cfg.TempDir = filepath.Join(sandbox, "tmp")
			config.SetGlobal(&cfg)
			t.Cleanup(func() { config.SetGlobal(prev) })

			// Stage delta + complete SBOM files and a fake baseline copy to emit.
			staged := filepath.Join(sandbox, "staged")
			if err := os.MkdirAll(staged, 0o755); err != nil {
				t.Fatalf("mkdir staged: %v", err)
			}
			deltaPath := filepath.Join(staged, "delta.json")
			completePath := filepath.Join(staged, "complete.json")
			for _, p := range []string{deltaPath, completePath} {
				if err := os.WriteFile(p, []byte(`{"packages":[]}`), 0o644); err != nil {
					t.Fatalf("write %s: %v", p, err)
				}
			}
			copyPath := filepath.Join(sandbox, "baseline.raw")
			if err := os.WriteFile(copyPath, []byte("img"), 0o644); err != nil {
				t.Fatalf("write copy: %v", err)
			}

			tmpl := &config.ImageTemplate{
				Image:        config.ImageInfo{Name: "img", Version: "1.0"},
				Target:       config.TargetInfo{OS: "debian", Dist: "debian13", Arch: "x86_64"},
				SystemConfig: config.SystemConfig{Name: "overlay"},
			}
			sbom := &overlaySBOMArtifacts{deltaPath: deltaPath, completePath: completePath, completeIsDeltaOnly: tt.deltaOnly}

			artifact, err := emitOverlayArtifact(tmpl, copyPath, "1.0", sbom)
			if err != nil {
				t.Fatalf("emitOverlayArtifact: %v", err)
			}
			buildDir := filepath.Dir(artifact)

			// The delta sidecar is always emitted.
			if _, err := os.Stat(filepath.Join(buildDir, "img-1.0"+overlayDeltaSBOMSuffix)); err != nil {
				t.Errorf("delta sidecar must always be emitted: %v", err)
			}
			// The complete sidecar is emitted only when it is a real base+delta union.
			_, statErr := os.Stat(filepath.Join(buildDir, "img-1.0"+overlayCompleteSBOMSuffix))
			if tt.wantComplete && statErr != nil {
				t.Errorf("complete sidecar must be emitted for a real base+delta union: %v", statErr)
			}
			if !tt.wantComplete && !os.IsNotExist(statErr) {
				t.Errorf("complete sidecar must be skipped when delta-only (stat err = %v)", statErr)
			}
		})
	}
}

// sandboxDirs points WorkDir + TempDir at a per-test sandbox so the emit build dir
// and staged files live somewhere writable, restoring the global config after.
func sandboxDirs(t *testing.T) string {
	t.Helper()
	prev := config.Global()
	cfg := *prev
	sandbox := t.TempDir()
	cfg.WorkDir = filepath.Join(sandbox, "work")
	cfg.TempDir = filepath.Join(sandbox, "tmp")
	config.SetGlobal(&cfg)
	t.Cleanup(func() { config.SetGlobal(prev) })
	return sandbox
}

func overlayTestTemplate() *config.ImageTemplate {
	return &config.ImageTemplate{
		Image:        config.ImageInfo{Name: "img", Version: "1.0"},
		Target:       config.TargetInfo{OS: "debian", Dist: "debian13", Arch: "x86_64"},
		SystemConfig: config.SystemConfig{Name: "overlay"},
	}
}

// TestEmitOverlayArtifact_DeltaSidecarFailureIsFatal asserts that a failure to
// write the delta SBOM sidecar (a GUARANTEED artifact) fails the emit AND removes
// the just-moved image, so a "successful" build never silently omits the delta or
// leaves a partial image in the output directory.
func TestEmitOverlayArtifact_DeltaSidecarFailureIsFatal(t *testing.T) {
	sandbox := sandboxDirs(t)

	// Complete SBOM is staged, but the delta source does NOT exist, so reading it to
	// emit the delta sidecar fails.
	staged := filepath.Join(sandbox, "staged")
	if err := os.MkdirAll(staged, 0o755); err != nil {
		t.Fatalf("mkdir staged: %v", err)
	}
	completePath := filepath.Join(staged, "complete.json")
	if err := os.WriteFile(completePath, []byte(`{"packages":[]}`), 0o644); err != nil {
		t.Fatalf("write complete: %v", err)
	}
	missingDelta := filepath.Join(staged, "does-not-exist-delta.json")

	copyPath := filepath.Join(sandbox, "baseline.raw")
	if err := os.WriteFile(copyPath, []byte("img"), 0o644); err != nil {
		t.Fatalf("write copy: %v", err)
	}

	sbom := &overlaySBOMArtifacts{deltaPath: missingDelta, completePath: completePath}
	artifact, err := emitOverlayArtifact(overlayTestTemplate(), copyPath, "1.0", sbom)
	if err == nil {
		t.Fatalf("a delta sidecar write failure must fail the emit, got artifact=%q nil err", artifact)
	}
	if !strings.Contains(err.Error(), "delta SBOM") {
		t.Errorf("error should name the delta SBOM sidecar, got %v", err)
	}
	// The moved image must not be left behind in the output build dir.
	buildDir, derr := overlayImageBuildDir(overlayTestTemplate())
	if derr != nil {
		t.Fatalf("overlayImageBuildDir: %v", derr)
	}
	if _, statErr := os.Stat(filepath.Join(buildDir, "img-1.0.raw")); statErr == nil {
		t.Error("the emitted image must be removed when the delta sidecar fails, but it is still present")
	}
}

// TestEmitOverlayArtifact_CompleteSidecarFailureIsFatal asserts that when a base
// SBOM is available (completeIsDeltaOnly=false) a failure to write the documented
// complete sidecar fails the emit AND rolls back the image and delta sidecar, so a
// "successful" build never omits a promised artifact or leaves a partial output.
func TestEmitOverlayArtifact_CompleteSidecarFailureIsFatal(t *testing.T) {
	sandbox := sandboxDirs(t)

	staged := filepath.Join(sandbox, "staged")
	if err := os.MkdirAll(staged, 0o755); err != nil {
		t.Fatalf("mkdir staged: %v", err)
	}
	deltaPath := filepath.Join(staged, "delta.json")
	if err := os.WriteFile(deltaPath, []byte(`{"packages":[]}`), 0o644); err != nil {
		t.Fatalf("write delta: %v", err)
	}
	// The complete source is absent, so its sidecar write fails. Because a base SBOM
	// was available (completeIsDeltaOnly stays false), this is fatal.
	missingComplete := filepath.Join(staged, "does-not-exist-complete.json")

	copyPath := filepath.Join(sandbox, "baseline.raw")
	if err := os.WriteFile(copyPath, []byte("img"), 0o644); err != nil {
		t.Fatalf("write copy: %v", err)
	}

	sbom := &overlaySBOMArtifacts{deltaPath: deltaPath, completePath: missingComplete}
	artifact, err := emitOverlayArtifact(overlayTestTemplate(), copyPath, "1.0", sbom)
	if err == nil {
		t.Fatalf("a complete sidecar write failure must fail the emit, got artifact=%q nil err", artifact)
	}
	if !strings.Contains(err.Error(), "complete SBOM") {
		t.Errorf("error should name the complete SBOM sidecar, got %v", err)
	}
	// The image and the delta sidecar must both be rolled back.
	buildDir, derr := overlayImageBuildDir(overlayTestTemplate())
	if derr != nil {
		t.Fatalf("overlayImageBuildDir: %v", derr)
	}
	if _, statErr := os.Stat(filepath.Join(buildDir, "img-1.0.raw")); statErr == nil {
		t.Error("the emitted image must be rolled back when the complete sidecar fails")
	}
	if _, statErr := os.Stat(filepath.Join(buildDir, "img-1.0"+overlayDeltaSBOMSuffix)); statErr == nil {
		t.Error("the delta sidecar must be rolled back when the complete sidecar fails")
	}
}

// TestEmitOverlayArtifact_RemovesStaleCompleteSidecarWhenDeltaOnly asserts that a
// rebuild of the same name/version with NO base SBOM (completeIsDeltaOnly=true)
// removes a stale complete sidecar left by an earlier build, so the output never
// retains a full-inventory document describing the previous image.
func TestEmitOverlayArtifact_RemovesStaleCompleteSidecarWhenDeltaOnly(t *testing.T) {
	sandbox := sandboxDirs(t)

	staged := filepath.Join(sandbox, "staged")
	if err := os.MkdirAll(staged, 0o755); err != nil {
		t.Fatalf("mkdir staged: %v", err)
	}
	deltaPath := filepath.Join(staged, "delta.json")
	completePath := filepath.Join(staged, "complete.json")
	for _, p := range []string{deltaPath, completePath} {
		if err := os.WriteFile(p, []byte(`{"packages":[]}`), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	copyPath := filepath.Join(sandbox, "baseline.raw")
	if err := os.WriteFile(copyPath, []byte("img"), 0o644); err != nil {
		t.Fatalf("write copy: %v", err)
	}

	// Pre-seed a stale complete sidecar in the build dir, as a prior base-backed build would.
	buildDir, derr := overlayImageBuildDir(overlayTestTemplate())
	if derr != nil {
		t.Fatalf("overlayImageBuildDir: %v", derr)
	}
	if err := os.MkdirAll(buildDir, 0o700); err != nil {
		t.Fatalf("mkdir buildDir: %v", err)
	}
	staleComplete := filepath.Join(buildDir, "img-1.0"+overlayCompleteSBOMSuffix)
	if err := os.WriteFile(staleComplete, []byte(`{"packages":[{"name":"old"}]}`), 0o644); err != nil {
		t.Fatalf("seed stale complete: %v", err)
	}

	sbom := &overlaySBOMArtifacts{deltaPath: deltaPath, completePath: completePath, completeIsDeltaOnly: true}
	if _, err := emitOverlayArtifact(overlayTestTemplate(), copyPath, "1.0", sbom); err != nil {
		t.Fatalf("emitOverlayArtifact: %v", err)
	}
	if _, statErr := os.Stat(staleComplete); !os.IsNotExist(statErr) {
		t.Errorf("the stale complete sidecar must be removed on a delta-only rebuild, stat err = %v", statErr)
	}
}

// TestAssertNoSymlinkInChrootPath verifies the SBOM-embed symlink guard: a
// destination whose ancestor or final component is a symlink inside the (untrusted)
// baseline mount is rejected, while a plain-directory chain and a not-yet-existing
// tail are accepted.
func TestAssertNoSymlinkInChrootPath(t *testing.T) {
	root := t.TempDir()
	rel := "usr/share/sbom/spdx_manifest.json"

	// 1. Fully absent chain: nothing to traverse -> OK (cp -p / mkdir -p will create it).
	if err := assertNoSymlinkInChrootPath(root, rel); err != nil {
		t.Errorf("absent destination chain must be allowed, got %v", err)
	}

	// 2. Real directory chain with the file present and not a symlink -> OK.
	realDir := filepath.Join(root, "usr", "share", "sbom")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(realDir, "spdx_manifest.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := assertNoSymlinkInChrootPath(root, rel); err != nil {
		t.Errorf("plain directory+file chain must be allowed, got %v", err)
	}

	// 3. A symlinked ANCESTOR (usr/share -> /etc) must be rejected before traversal.
	root2 := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root2, "usr"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink("/etc", filepath.Join(root2, "usr", "share")); err != nil {
		t.Fatalf("symlink ancestor: %v", err)
	}
	if err := assertNoSymlinkInChrootPath(root2, rel); err == nil {
		t.Error("a symlinked ancestor directory must be rejected")
	}

	// 4. A symlinked TARGET file (pointing at a host path) must be rejected.
	root3 := t.TempDir()
	dir3 := filepath.Join(root3, "usr", "share", "sbom")
	if err := os.MkdirAll(dir3, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(dir3, "spdx_manifest.json")); err != nil {
		t.Fatalf("symlink target: %v", err)
	}
	if err := assertNoSymlinkInChrootPath(root3, rel); err == nil {
		t.Error("a symlinked target file must be rejected")
	}
}
