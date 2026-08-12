package overlay

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

// This file is the consolidated cleanup/rollback matrix for the overlay Builder.
// It injects a failure at EVERY pipeline stage and asserts the mount/loop
// lifecycle fully unwinds (teardown + detach exactly once, or zero when nothing
// was set up yet) and that no workspace copy or output artifact leaks. The
// per-behavior tests in session_test.go still own the fine-grained assertions;
// this guarantees blanket coverage across the whole stage list in one table.

// stageFailure describes one row of the failure matrix: which stage's seam is made
// to fail, the phase the failure surfaces in, and the expected teardown/detach/
// removeCopy counts after the full Preprocess→Build→Postprocess drive.
type stageFailure struct {
	name string
	// inject sets the failure on the recorder.
	inject func(*builderRecorder)
	// failsIn is "preprocess" or "build" (the phase whose call returns the error);
	// SBOM/emit/inspect/convert failures surface from Postprocess and use "post".
	failsIn string
	// wantTeardowns/wantDetaches are the expected cleanup counts after the whole
	// lifecycle has been driven (including Postprocess).
	wantTeardowns int
	wantDetaches  int
	// wantRemoveCopy is the expected number of workspace-copy force-removals.
	wantRemoveCopy int
}

// TestBuilder_CleanupMatrix_EveryStageFailureUnwinds drives a failure at each stage
// and asserts the lifecycle unwinds with no leaked mount, loop device, or workspace
// copy. It exercises the stages that previously had no dedicated failure-injection
// test (mount, resolve, resize, boot regen, additional files, SBOM) alongside the
// ones that did, so the whole pipeline is covered uniformly.
func TestBuilder_CleanupMatrix_EveryStageFailureUnwinds(t *testing.T) {
	boom := errors.New("injected failure")

	tests := []stageFailure{
		// Preprocess stages. acquire fails before anything is set up (0/0, and with a
		// nil ctx there is no copy to remove). Every later preprocess failure unwinds
		// the mount+loop opened by acquire, and the subsequent Postprocess(preErr)
		// force-removes the orphaned workspace copy once the loop is released
		// (removeCopy==1). mount failure sets no teardown (the mount never succeeded)
		// but the loop is still detached.
		{"acquire", func(r *builderRecorder) { r.acquireErr = boom }, "preprocess", 0, 0, 0},
		{"mount", func(r *builderRecorder) { r.mountErr = boom }, "preprocess", 0, 1, 1},
		{"detect", func(r *builderRecorder) { r.detectErr = boom }, "preprocess", 1, 1, 1},
		{"resolve", func(r *builderRecorder) { r.resolveErr = boom }, "preprocess", 1, 1, 1},
		{"preflight", func(r *builderRecorder) {
			r.preflightErr = boom
			r.report = &PreflightReport{Blocked: true}
		}, "preprocess", 1, 1, 1},

		// Build stages. Build never tears down itself (mounts span into Postprocess);
		// the unwind happens when Postprocess(buildErr) runs. A failed build never
		// emits, so the orphaned workspace copy is force-removed once (removeCopy==1).
		{"resize", func(r *builderRecorder) { r.resizeErr = boom }, "build", 1, 1, 1},
		{"install", func(r *builderRecorder) { r.installErr = boom }, "build", 1, 1, 1},
		{"configure", func(r *builderRecorder) { r.configureErr = boom }, "build", 1, 1, 1},
		{"boot-regen", func(r *builderRecorder) { r.regenErr = boom }, "build", 1, 1, 1},
		{"grub-regen", func(r *builderRecorder) { r.grubRegenErr = boom }, "build", 1, 1, 1},
		{"additional-files", func(r *builderRecorder) { r.addFilesErr = boom }, "build", 1, 1, 1},

		// Postprocess finalization stages. SBOM fails before the pre-emit release, so
		// the deferred cleanup does the single teardown/detach and removes the copy.
		{"sbom", func(r *builderRecorder) { r.sbomErr = boom }, "post", 1, 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer saveBuilderSeams().restore()
			r := &builderRecorder{}
			tt.inject(r)
			b := installOverlayTestBuilder(t, r)

			preErr := b.Preprocess()
			var buildErr error
			if preErr == nil {
				buildErr = b.Build()
			}
			// Postprocess always runs (mirroring the provider's goto-post flow),
			// threaded with whatever error occurred so far.
			phaseErr := preErr
			if phaseErr == nil {
				phaseErr = buildErr
			}
			postErr := b.Postprocess(phaseErr)

			// The failure must have surfaced in the expected phase.
			switch tt.failsIn {
			case "preprocess":
				if preErr == nil {
					t.Fatalf("expected %s failure to surface from Preprocess", tt.name)
				}
			case "build":
				if buildErr == nil {
					t.Fatalf("expected %s failure to surface from Build", tt.name)
				}
			case "post":
				// A post-phase (e.g. SBOM) failure must surface FROM Postprocess itself —
				// explicitly require it, so the test cannot pass if Postprocess ever
				// swallowed the injected error (the cleanup counts alone would not catch that).
				if postErr == nil {
					t.Fatalf("expected %s failure to surface from Postprocess", tt.name)
				}
			}

			if r.teardowns != tt.wantTeardowns || r.detaches != tt.wantDetaches {
				t.Errorf("%s: teardown/detach = %d/%d, want %d/%d",
					tt.name, r.teardowns, r.detaches, tt.wantTeardowns, tt.wantDetaches)
			}
			if r.removeCopies != tt.wantRemoveCopy {
				t.Errorf("%s: removeCopies = %d, want %d", tt.name, r.removeCopies, tt.wantRemoveCopy)
			}
		})
	}
}

// TestBuilder_PostEmitFailureRemovesPartialOutput asserts the no-partial-state
// contract for the OUTPUT directory: when the artifact is emitted but a later
// stage (convert) fails, the emitted RAW and its sidecars are removed so nothing
// from an unsuccessful build lingers in the build output directory.
func TestBuilder_PostEmitFailureRemovesPartialOutput(t *testing.T) {
	defer saveBuilderSeams().restore()

	outDir := t.TempDir()
	rawPath := filepath.Join(outDir, "img-1.0.raw")

	r := &builderRecorder{convertErr: errors.New("qemu-img failed")}
	b := installOverlayTestBuilder(t, r)

	// Make emit actually create the output artifact + sidecars in outDir, so the
	// post-emit cleanup has real files to remove.
	sidecars := []string{
		rawPath,
		filepath.Join(outDir, "img-1.0"+overlayCompleteSBOMSuffix),
		filepath.Join(outDir, "img-1.0"+overlayDeltaSBOMSuffix),
	}
	builderEmitFn = func(_ *config.ImageTemplate, _, version string, _ *overlaySBOMArtifacts) (string, error) {
		r.note("emit:" + version)
		for _, p := range sidecars {
			if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
				t.Fatalf("seed output artifact %s: %v", p, err)
			}
		}
		return rawPath, nil
	}

	if err := b.Preprocess(); err != nil {
		t.Fatalf("Preprocess: %v", err)
	}
	if err := b.Build(); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := b.Postprocess(nil); err == nil {
		t.Fatal("expected convert failure to surface from Postprocess")
	}

	// Every seeded output artifact must have been removed by the failure-path cleanup.
	for _, p := range sidecars {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("partial output artifact %s must be removed on a post-emit failure (stat err = %v)", p, err)
		}
	}
}

// TestBuilder_PanicInStageIsRecoveredAndCleansUp asserts cleanup never panics:
// a stage that panics during Postprocess finalization is recovered, the mount/loop
// release still runs, and the panic is surfaced as an error rather than crashing.
func TestBuilder_PanicInStageIsRecoveredAndCleansUp(t *testing.T) {
	defer saveBuilderSeams().restore()

	r := &builderRecorder{}
	b := installOverlayTestBuilder(t, r)

	// Panic inside the SBOM stage (the first Postprocess finalization stage), after
	// the mount lifecycle is open, to prove the deferred cleanup recovers and unwinds.
	builderSBOMFn = func(*config.ImageTemplate, *BaselineInfo, string, *ResolutionPlan, *PreflightReport) (*overlaySBOMArtifacts, error) {
		r.note("sbom")
		panic("boom in sbom stage")
	}

	if err := b.Preprocess(); err != nil {
		t.Fatalf("Preprocess: %v", err)
	}
	if err := b.Build(); err != nil {
		t.Fatalf("Build: %v", err)
	}

	err := b.Postprocess(nil)
	if err == nil {
		t.Fatal("expected the recovered panic to surface as an error")
	}
	if !strings.Contains(err.Error(), "panic during finalization") {
		t.Errorf("expected the error to name the recovered panic, got: %v", err)
	}
	// Despite the panic, the mount and loop device were released exactly once.
	if r.teardowns != 1 || r.detaches != 1 {
		t.Errorf("panic path must still unwind mounts once: teardown=%d detach=%d", r.teardowns, r.detaches)
	}
}

// TestRemoveEmittedArtifacts removes the RAW and all deterministic sidecars off the
// same base name, tolerates missing files, and leaves unrelated files untouched.
func TestRemoveEmittedArtifacts(t *testing.T) {
	dir := t.TempDir()
	raw := filepath.Join(dir, "myimage-1.0.raw")

	// A template declaring a converted artifact plus a compressed variant, so the
	// cleanup covers the configured output suffixes (".qcow2" and ".qcow2.zst"),
	// not just the hardcoded defaults.
	template := &config.ImageTemplate{
		Disk: config.DiskConfig{
			Artifacts: []config.ArtifactInfo{
				{Type: "qcow2", Compression: "zst"},
			},
		},
	}

	// A representative set: the RAW, a converted format, its compressed variant,
	// both SBOM sidecars, the inspection report, plus an unrelated file that must
	// survive.
	present := []string{
		raw,
		filepath.Join(dir, "myimage-1.0.qcow2"),
		filepath.Join(dir, "myimage-1.0.qcow2.zst"),
		filepath.Join(dir, "myimage-1.0"+overlayCompleteSBOMSuffix),
		filepath.Join(dir, "myimage-1.0"+overlayDeltaSBOMSuffix),
		filepath.Join(dir, "myimage-1.0"+overlayInspectReportSuffix),
	}
	unrelated := filepath.Join(dir, "other-2.0.raw")
	for _, p := range append(append([]string{}, present...), unrelated) {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	removeEmittedArtifacts(raw, template)

	for _, p := range present {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("expected %s removed, stat err = %v", p, err)
		}
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Errorf("unrelated artifact %s must not be removed: %v", unrelated, err)
	}

	// Idempotent / tolerant of already-absent files: a second call is a no-op.
	removeEmittedArtifacts(raw, template)
	// Empty path and nil template are safe no-ops.
	removeEmittedArtifacts("", nil)
}

// TestRemoveStaleArtifact removes an existing deterministic artifact, tolerates an
// already-absent path, and is a safe no-op on an empty path.
func TestRemoveStaleArtifact(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "img-1.0.complete.spdx.json")
	if err := os.WriteFile(stale, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write stale: %v", err)
	}
	removeStaleArtifact(stale)
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("expected stale artifact removed, stat err = %v", err)
	}
	// Absent path and empty path are safe no-ops (must not panic or error).
	removeStaleArtifact(stale)
	removeStaleArtifact("")
}

// TestRemoveEmittedOutputs removes each named output and tolerates missing/empty entries.
func TestRemoveEmittedOutputs(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "img-1.0.raw")
	b := filepath.Join(dir, "img-1.0.delta.spdx.json")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	// Include an already-absent path and an empty string among the inputs.
	removeEmittedOutputs(a, b, filepath.Join(dir, "missing"), "")
	for _, p := range []string{a, b} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("expected %s removed, stat err = %v", p, err)
		}
	}
}

// TestMoveFile_SameDeviceRenameSucceeds asserts the common same-filesystem path:
// moveFile renames src to dst in place, so dst exists afterward and src is gone.
func TestMoveFile_SameDeviceRenameSucceeds(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.raw")
	dst := filepath.Join(dir, "dst.raw")
	if err := os.WriteFile(src, []byte("data"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	if err := moveFile(src, dst); err != nil {
		t.Fatalf("moveFile: %v", err)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Errorf("dst should exist after move: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("src should be gone after move: stat err = %v", err)
	}
}
