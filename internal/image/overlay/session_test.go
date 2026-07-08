package overlay

import (
	"errors"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

// builderSeams snapshots every Builder-stage seam so a test can restore them.
type builderSeams struct {
	acquire     func(*Ingestor) (*Context, error)
	mountLayout func(*Inspector, string) (*Layout, func() error, error)
	detach      func(*Ingestor, *Context) error
	removeCopy  func(*Ingestor, *Context)
	detect      func(string, config.TargetInfo) (*BaselineInfo, []BaselinePackage, error)
	resolve     func(*config.ImageTemplate, *BaselineInfo, []BaselinePackage) (*ResolutionPlan, error)
	preflight   func(*BaselineInfo, []BaselinePackage, *ResolutionPlan, *config.OverlayPolicy) (*PreflightReport, error)
	install     func(*BaselineInfo, string, *ResolutionPlan, *PreflightReport) (*InstallResult, error)
	regenBoot   func(*BaselineInfo, string, *InstallResult, *ResolutionPlan) error
	resize      func(*config.ImageTemplate, *Context, *Layout) error
	sbom        func(*BaselineInfo, string, *ResolutionPlan) error
	emit        func(*config.ImageTemplate, string, string) (string, error)
}

func saveBuilderSeams() builderSeams {
	return builderSeams{
		acquire: builderAcquire, mountLayout: builderMountLayout, detach: builderDetach,
		removeCopy: builderRemoveCopy,
		detect:     builderDetectFn, resolve: builderResolveFn, preflight: builderPreflightFn,
		install: builderInstallFn, regenBoot: builderRegenBootFn, resize: builderResizeFn,
		sbom: builderSBOMFn, emit: builderEmitFn,
	}
}

func (s builderSeams) restore() {
	builderAcquire, builderMountLayout, builderDetach = s.acquire, s.mountLayout, s.detach
	builderRemoveCopy = s.removeCopy
	builderDetectFn, builderResolveFn, builderPreflightFn = s.detect, s.resolve, s.preflight
	builderInstallFn, builderRegenBootFn, builderResizeFn = s.install, s.regenBoot, s.resize
	builderSBOMFn, builderEmitFn = s.sbom, s.emit
}

// builderRecorder tracks calls through the seams and lets a test inject errors at
// any stage, so the phase ordering and the always-runs cleanup chain are testable
// without root, loops, or mounts.
type builderRecorder struct {
	calls        []string
	teardowns    int
	detaches     int
	removeCopies int

	acquireErr   error
	mountErr     error
	detachErr    error
	detectErr    error
	resolveErr   error
	preflightErr error
	installErr   error
	regenErr     error
	resizeErr    error
	sbomErr      error
	emitErr      error

	report    *PreflightReport
	installed *InstallResult
}

func (r *builderRecorder) note(name string) { r.calls = append(r.calls, name) }

// installOverlayTestBuilder builds a Builder wired entirely to a recorder, so no
// real stage runs. It returns the builder and the recorder.
func installOverlayTestBuilder(t *testing.T, r *builderRecorder) *Builder {
	t.Helper()
	tmpl := &config.ImageTemplate{
		Image:  config.ImageInfo{Name: "img", Version: "1.0"},
		Target: config.TargetInfo{OS: "ubuntu", Dist: "ubuntu24", Arch: "amd64"},
		Baseline: &config.Baseline{
			Mode:   config.BaselineModeOverlay,
			Source: &config.BaselineSource{Path: "/some/baseline.raw"},
		},
	}
	b := &Builder{template: tmpl, ingestor: &Ingestor{template: tmpl}, inspector: NewInspector("/wd")}

	builderAcquire = func(*Ingestor) (*Context, error) {
		r.note("acquire")
		if r.acquireErr != nil {
			return nil, r.acquireErr
		}
		return &Context{BaselineCopyPath: "/wd/baseline.raw", LoopDevPath: "/dev/loop0", Partitions: []string{"/dev/loop0p1"}}, nil
	}
	builderMountLayout = func(*Inspector, string) (*Layout, func() error, error) {
		r.note("mount")
		if r.mountErr != nil {
			return nil, nil, r.mountErr
		}
		teardown := func() error { r.teardowns++; return nil }
		return &Layout{RootMount: "/wd/mnt/root", RootDevice: "/dev/loop0p2", RootFSType: "ext4", PartitionTable: partitionTableGPT}, teardown, nil
	}
	builderDetach = func(*Ingestor, *Context) error { r.detaches++; return r.detachErr }
	builderRemoveCopy = func(*Ingestor, *Context) { r.removeCopies++ }
	builderDetectFn = func(string, config.TargetInfo) (*BaselineInfo, []BaselinePackage, error) {
		r.note("detect")
		if r.detectErr != nil {
			return nil, nil, r.detectErr
		}
		return &BaselineInfo{OS: "ubuntu", Arch: "x86_64", PackageManager: PackageManagerAPT, PackageType: pkgTypeDeb, Version: "24.04"}, nil, nil
	}
	builderResolveFn = func(*config.ImageTemplate, *BaselineInfo, []BaselinePackage) (*ResolutionPlan, error) {
		r.note("resolve")
		if r.resolveErr != nil {
			return nil, r.resolveErr
		}
		return &ResolutionPlan{ToInstall: []ResolvedPackage{{Name: "curl", URL: "https://r/curl.deb"}}}, nil
	}
	builderPreflightFn = func(*BaselineInfo, []BaselinePackage, *ResolutionPlan, *config.OverlayPolicy) (*PreflightReport, error) {
		r.note("preflight")
		if r.preflightErr != nil {
			return r.report, r.preflightErr
		}
		return &PreflightReport{Blocked: false}, nil
	}
	builderInstallFn = func(*BaselineInfo, string, *ResolutionPlan, *PreflightReport) (*InstallResult, error) {
		r.note("install")
		if r.installErr != nil {
			return nil, r.installErr
		}
		if r.installed != nil {
			return r.installed, nil
		}
		return &InstallResult{Installed: []string{"curl"}}, nil
	}
	builderRegenBootFn = func(*BaselineInfo, string, *InstallResult, *ResolutionPlan) error {
		r.note("regenBoot")
		return r.regenErr
	}
	builderResizeFn = func(*config.ImageTemplate, *Context, *Layout) error {
		r.note("resize")
		return r.resizeErr
	}
	builderSBOMFn = func(*BaselineInfo, string, *ResolutionPlan) error {
		r.note("sbom")
		return r.sbomErr
	}
	builderEmitFn = func(_ *config.ImageTemplate, _, version string) (string, error) {
		r.note("emit:" + version)
		if r.emitErr != nil {
			return "", r.emitErr
		}
		return "/out/img-" + version + ".raw", nil
	}
	return b
}

func TestBuilder_HappyPathOrdersStagesAndCleansUp(t *testing.T) {
	defer saveBuilderSeams().restore()
	r := &builderRecorder{}
	b := installOverlayTestBuilder(t, r)

	if err := b.Preprocess(); err != nil {
		t.Fatalf("Preprocess: %v", err)
	}
	if err := b.Build(); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := b.Postprocess(nil); err != nil {
		t.Fatalf("Postprocess: %v", err)
	}

	want := []string{"acquire", "mount", "detect", "resolve", "preflight", "install", "regenBoot", "resize", "sbom", "emit:1.0"}
	if !equalStrings(r.calls, want) {
		t.Errorf("stage order = %v, want %v", r.calls, want)
	}
	// Single mount lifecycle: one teardown, one detach, exactly once each.
	if r.teardowns != 1 || r.detaches != 1 {
		t.Errorf("teardown/detach = %d/%d, want 1/1", r.teardowns, r.detaches)
	}
	// On success the copy is moved out by emit, so it must NOT also be removed.
	if r.removeCopies != 0 {
		t.Errorf("workspace copy must not be removed on the success path (emit moves it): removeCopies=%d", r.removeCopies)
	}
	// A fully successful build records one timing row per pipeline stage, in
	// execution order, so the caller can render an overlay timing table.
	wantStages := []string{
		"Acquire & Mount Baseline", "Inspect Baseline", "Resolve Packages", "Preflight",
		"Install Packages", "Boot Regeneration", "Resize", "Generate SBOM", "Emit Artifact",
	}
	gotStages := make([]string, 0, len(b.Timings()))
	for _, ts := range b.Timings() {
		gotStages = append(gotStages, ts.Stage)
	}
	if !equalStrings(gotStages, wantStages) {
		t.Errorf("timing stages = %v, want %v", gotStages, wantStages)
	}
}

func TestBuilder_TimingStopsAtFailedStage(t *testing.T) {
	defer saveBuilderSeams().restore()
	r := &builderRecorder{installErr: errors.New("dpkg failed")}
	b := installOverlayTestBuilder(t, r)

	if err := b.Preprocess(); err != nil {
		t.Fatalf("Preprocess: %v", err)
	}
	if err := b.Build(); err == nil {
		t.Fatal("expected install failure to propagate")
	}
	// The failing stage is still recorded (timeStage times fn even on error), but no
	// later stage runs, so the table reflects exactly the pipeline that executed.
	got := make([]string, 0, len(b.Timings()))
	for _, ts := range b.Timings() {
		got = append(got, ts.Stage)
	}
	want := []string{
		"Acquire & Mount Baseline", "Inspect Baseline", "Resolve Packages", "Preflight",
		"Install Packages",
	}
	if !equalStrings(got, want) {
		t.Errorf("timing stages after install failure = %v, want %v", got, want)
	}
}

func TestBuilder_TimingsReturnsDefensiveCopy(t *testing.T) {
	b := &Builder{timings: []StageTiming{{Stage: "Acquire & Mount Baseline"}}}

	got := b.Timings()
	// Mutating the returned slice must not corrupt the Builder's internal state.
	got[0].Stage = "mutated"
	got = append(got, StageTiming{Stage: "appended"})
	_ = got

	if b.timings[0].Stage != "Acquire & Mount Baseline" {
		t.Errorf("Builder timings mutated via returned slice: got %q", b.timings[0].Stage)
	}
	if len(b.timings) != 1 {
		t.Errorf("Builder timings length changed via returned slice: got %d, want 1", len(b.timings))
	}
}

func TestBuilder_PreprocessFailureUnwindsImmediately(t *testing.T) {
	defer saveBuilderSeams().restore()
	r := &builderRecorder{detectErr: errors.New("bad baseline")}
	b := installOverlayTestBuilder(t, r)

	if err := b.Preprocess(); err == nil {
		t.Fatal("expected detect failure to propagate")
	}
	// Mounts were established before detect, so they must be unwound at once.
	if r.teardowns != 1 || r.detaches != 1 {
		t.Errorf("failed preprocess must unwind mounts: teardown=%d detach=%d", r.teardowns, r.detaches)
	}
	// A later Postprocess must not double-clean.
	if err := b.Postprocess(errors.New("prior")); err != nil {
		t.Fatalf("Postprocess after failed preprocess: %v", err)
	}
	if r.teardowns != 1 || r.detaches != 1 {
		t.Errorf("cleanup must be idempotent: teardown=%d detach=%d", r.teardowns, r.detaches)
	}
}

func TestBuilder_AcquireFailureLeavesNothingToClean(t *testing.T) {
	defer saveBuilderSeams().restore()
	r := &builderRecorder{acquireErr: errors.New("attach failed")}
	b := installOverlayTestBuilder(t, r)

	if err := b.Preprocess(); err == nil {
		t.Fatal("expected acquire failure")
	}
	// Nothing was mounted or attached yet (acquire itself failed), so neither
	// teardown nor detach should have run.
	if r.teardowns != 0 || r.detaches != 0 {
		t.Errorf("no cleanup expected when acquire fails: teardown=%d detach=%d", r.teardowns, r.detaches)
	}
}

func TestBuilder_BlockedPreflightUnwinds(t *testing.T) {
	defer saveBuilderSeams().restore()
	r := &builderRecorder{
		preflightErr: errors.New("blocked: downgrade"),
		report:       &PreflightReport{Blocked: true},
	}
	b := installOverlayTestBuilder(t, r)

	if err := b.Preprocess(); err == nil {
		t.Fatal("expected blocked preflight to fail preprocess")
	}
	if r.teardowns != 1 || r.detaches != 1 {
		t.Errorf("blocked preflight must unwind mounts: teardown=%d detach=%d", r.teardowns, r.detaches)
	}
	// Build must refuse: preprocess never succeeded.
	if err := b.Build(); err == nil {
		t.Fatal("Build must refuse after a failed preprocess")
	}
}

func TestBuilder_BuildFailureStillCleansUpInPostprocess(t *testing.T) {
	defer saveBuilderSeams().restore()
	r := &builderRecorder{installErr: errors.New("dpkg failed")}
	b := installOverlayTestBuilder(t, r)

	if err := b.Preprocess(); err != nil {
		t.Fatalf("Preprocess: %v", err)
	}
	buildErr := b.Build()
	if buildErr == nil {
		t.Fatal("expected install failure to propagate")
	}
	// Build failure must NOT tear down (mounts span into postprocess).
	if r.teardowns != 0 || r.detaches != 0 {
		t.Errorf("build failure must not clean up early: teardown=%d detach=%d", r.teardowns, r.detaches)
	}
	// Postprocess with the build error: no finalize, but full cleanup.
	if err := b.Postprocess(buildErr); err != nil {
		t.Fatalf("Postprocess: %v", err)
	}
	for _, c := range r.calls {
		if c == "sbom" || strings.HasPrefix(c, "emit") {
			t.Errorf("finalization stage %q must not run on a failed build", c)
		}
	}
	if r.teardowns != 1 || r.detaches != 1 {
		t.Errorf("failed build must still clean up in postprocess: teardown=%d detach=%d", r.teardowns, r.detaches)
	}
	// A failed build never emits, so the workspace baseline copy would otherwise
	// accumulate; it must be removed once the loop device is released.
	if r.removeCopies != 1 {
		t.Errorf("failed build must remove the orphaned workspace copy: removeCopies=%d, want 1", r.removeCopies)
	}
}

// TestBuilder_FailedBuildWithDetachFailureRetainsCopy asserts that when the build
// failed AND the loop device could not be detached, the workspace copy is retained
// rather than removed: the still-attached loop device references the backing file,
// so unlinking it would hinder recovery.
func TestBuilder_FailedBuildWithDetachFailureRetainsCopy(t *testing.T) {
	defer saveBuilderSeams().restore()
	r := &builderRecorder{installErr: errors.New("dpkg failed"), detachErr: errors.New("device busy")}
	b := installOverlayTestBuilder(t, r)

	if err := b.Preprocess(); err != nil {
		t.Fatalf("Preprocess: %v", err)
	}
	buildErr := b.Build()
	if buildErr == nil {
		t.Fatal("expected install failure to propagate")
	}
	if err := b.Postprocess(buildErr); err == nil {
		t.Fatal("expected detach failure to surface from Postprocess")
	}
	// Detach failed, so the loop path is retained and the copy must NOT be removed.
	if r.removeCopies != 0 {
		t.Errorf("copy must be retained when detach fails (leaked device references it): removeCopies=%d", r.removeCopies)
	}
}

func TestBuilder_BuildBeforePreprocessRefused(t *testing.T) {
	defer saveBuilderSeams().restore()
	r := &builderRecorder{}
	b := installOverlayTestBuilder(t, r)
	if err := b.Build(); err == nil {
		t.Fatal("Build before Preprocess must be refused")
	}
}

func TestBuilder_EmitFailureSurfacesButStillCleansUp(t *testing.T) {
	defer saveBuilderSeams().restore()
	r := &builderRecorder{emitErr: errors.New("disk full")}
	b := installOverlayTestBuilder(t, r)

	if err := b.Preprocess(); err != nil {
		t.Fatalf("Preprocess: %v", err)
	}
	if err := b.Build(); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := b.Postprocess(nil); err == nil {
		t.Fatal("expected emit failure to surface")
	}
	// Emit runs after the explicit pre-emit cleanup, so teardown/detach already ran
	// once; the deferred cleanup must not run them a second time.
	if r.teardowns != 1 || r.detaches != 1 {
		t.Errorf("cleanup must be exactly once even when emit fails: teardown=%d detach=%d", r.teardowns, r.detaches)
	}
	// Emit failed, so the copy was never moved out; with the loop device already
	// released by the pre-emit cleanup, the orphaned copy must be removed.
	if r.removeCopies != 1 {
		t.Errorf("emit failure must remove the never-emitted workspace copy: removeCopies=%d, want 1", r.removeCopies)
	}
}

func TestBuilder_DetachFailureSurfacesFromPostprocess(t *testing.T) {
	defer saveBuilderSeams().restore()
	r := &builderRecorder{detachErr: errors.New("losetup -d failed")}
	b := installOverlayTestBuilder(t, r)

	if err := b.Preprocess(); err != nil {
		t.Fatalf("Preprocess: %v", err)
	}
	if err := b.Build(); err != nil {
		t.Fatalf("Build: %v", err)
	}
	// On the otherwise-successful path the pre-emit cleanupOnce detaches; a detach
	// failure there must abort the emit and surface as the Postprocess error rather
	// than being silently swallowed (a leaked loop device).
	err := b.Postprocess(nil)
	if err == nil {
		t.Fatal("expected detach failure to surface from Postprocess")
	}
	if !strings.Contains(err.Error(), "losetup -d failed") {
		t.Errorf("Postprocess error = %v, want it to wrap the detach failure", err)
	}
	// Emit must not have run: the pre-emit release failed.
	for _, c := range r.calls {
		if strings.HasPrefix(c, "emit") {
			t.Errorf("emit must not run when pre-emit detach fails; calls=%v", r.calls)
		}
	}
}

// TestBuilder_PostprocessSurfacesCleanupErrorOnFailedBuild asserts that when the
// build already failed, Postprocess still runs the cleanup chain AND surfaces a
// cleanup (unmount/detach) failure rather than swallowing it — otherwise a leaked
// mount or loop device could pass unnoticed on the failure path. It further asserts
// the ORIGINAL build failure is preserved (joined) alongside the cleanup failure, so
// build.go — which prioritizes the PostProcess error over the buildErr it passed in
// — does not drop the root cause when reporting a "post-processing failed" error.
func TestBuilder_PostprocessSurfacesCleanupErrorOnFailedBuild(t *testing.T) {
	defer saveBuilderSeams().restore()
	r := &builderRecorder{detachErr: errors.New("losetup -d failed")}
	b := installOverlayTestBuilder(t, r)

	if err := b.Preprocess(); err != nil {
		t.Fatalf("Preprocess: %v", err)
	}
	// Simulate a prior build failure threaded into Postprocess.
	buildErr := errors.New("prior build failure")
	err := b.Postprocess(buildErr)
	if err == nil {
		t.Fatal("expected cleanup failure to surface from Postprocess on the failed-build path")
	}
	if !strings.Contains(err.Error(), "losetup -d failed") {
		t.Errorf("Postprocess error = %v, want it to wrap the detach failure", err)
	}
	// The original build failure must be preserved (joined), not masked by the
	// cleanup error, so callers see both the root cause and the leak details.
	if !errors.Is(err, buildErr) {
		t.Errorf("Postprocess error = %v, want it to also wrap the original build failure %v", err, buildErr)
	}
	// Finalization must not run on a failed build.
	for _, c := range r.calls {
		if c == "sbom" || strings.HasPrefix(c, "emit") {
			t.Errorf("finalization stage %q must not run on a failed build", c)
		}
	}
}

// TestBuilder_DetachFailureRetainsLoopForRetry asserts that a failed detach does
// NOT clear the loop path, so a subsequent cleanup retries the detach (rather than
// treating a leaked loop device as released after the first failure).
func TestBuilder_DetachFailureRetainsLoopForRetry(t *testing.T) {
	defer saveBuilderSeams().restore()
	r := &builderRecorder{detachErr: errors.New("device busy")}
	b := installOverlayTestBuilder(t, r)

	if err := b.Preprocess(); err != nil {
		t.Fatalf("Preprocess: %v", err)
	}
	if err := b.Build(); err != nil {
		t.Fatalf("Build: %v", err)
	}
	// Success path: pre-emit cleanupOnce detaches (fails), aborting the emit; the
	// deferred cleanupOnce then RETRIES the detach because the loop path was
	// retained. Two detach attempts prove the retry is not short-circuited.
	if err := b.Postprocess(nil); err == nil {
		t.Fatal("expected detach failure to surface from Postprocess")
	}
	if r.detaches != 2 {
		t.Errorf("detach attempts = %d, want 2 (a failed detach must be retried, not skipped)", r.detaches)
	}
	// A later successful detach clears the path and stops retrying.
	r.detachErr = nil
	if err := b.cleanupOnce(); err != nil {
		t.Fatalf("cleanupOnce after clearing detachErr: %v", err)
	}
	if r.detaches != 3 {
		t.Errorf("detach attempts = %d, want 3 (retry once more, then succeed)", r.detaches)
	}
	if err := b.cleanupOnce(); err != nil {
		t.Fatalf("cleanupOnce after success: %v", err)
	}
	if r.detaches != 3 {
		t.Errorf("detach attempts = %d, want 3 (no detach after a successful release)", r.detaches)
	}
}

func TestBuilder_ImageVersionPrefersTemplateThenBaseline(t *testing.T) {
	b := &Builder{template: &config.ImageTemplate{Image: config.ImageInfo{Version: "2.5"}}}
	if got := b.imageVersion(); got != "2.5" {
		t.Errorf("version = %q, want template 2.5", got)
	}
	b.template.Image.Version = ""
	b.info = &BaselineInfo{Version: "24.04"}
	if got := b.imageVersion(); got != "24.04" {
		t.Errorf("version = %q, want baseline 24.04", got)
	}
	b.info = nil
	if got := b.imageVersion(); got != "overlay" {
		t.Errorf("version = %q, want fallback overlay", got)
	}
}

func TestNewBuilder_RejectsCreateMode(t *testing.T) {
	if _, err := NewBuilder(&config.ImageTemplate{}); err == nil {
		t.Fatal("expected NewBuilder to reject a non-overlay template")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
