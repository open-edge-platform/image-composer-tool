package overlay

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

// fakeLoopDev is an in-memory LoopDevManager for unit tests.
type fakeLoopDev struct {
	attachedPath    string // image path passed to AttachImageToLoopDev
	loopDevPath     string
	partitions      []string
	failAttach      bool
	leakOnAttach    bool // return a non-empty path alongside the attach error
	detachCallCount int
	detachedPath    string
	failDetach      bool
}

func (f *fakeLoopDev) AttachImageToLoopDev(imagePath string) (string, []string, error) {
	f.attachedPath = imagePath
	if f.failAttach {
		if f.leakOnAttach {
			// Simulate a leaked loop device: a device was created but detach
			// failed, so the path is returned even though the call errored.
			return f.loopDevPath, nil, fmt.Errorf("fake attach failure (leaked %s)", f.loopDevPath)
		}
		return "", nil, fmt.Errorf("fake attach failure")
	}
	return f.loopDevPath, f.partitions, nil
}

func (f *fakeLoopDev) LoopSetupDelete(loopDevPath string) error {
	f.detachCallCount++
	f.detachedPath = loopDevPath
	if f.failDetach {
		return fmt.Errorf("fake detach failure")
	}
	return nil
}

// newTestIngestor builds an Ingestor wired to a fake loop device and a workspace
// under t.TempDir(), bypassing NewIngestor's global-config dependency.
func newTestIngestor(t *testing.T, src *config.BaselineSource, loop LoopDevManager, retain bool) *Ingestor {
	t.Helper()
	tmpl := &config.ImageTemplate{
		Baseline: &config.Baseline{
			Mode:   config.BaselineModeOverlay,
			Source: src,
		},
	}
	return &Ingestor{
		template:   tmpl,
		loopDev:    loop,
		workDir:    filepath.Join(t.TempDir(), "overlay"),
		retainCopy: retain,
	}
}

// writeSourceImage creates a fake baseline RAW source file with known content.
func writeSourceImage(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "source.raw")
	if err := os.WriteFile(src, []byte("baseline-bytes"), 0644); err != nil {
		t.Fatalf("write source image: %v", err)
	}
	return src
}

func TestWithBaseline_CopiesAttachesDetachesAndRemoves(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	shell.Default = shell.NewMockExecutor(nil) // real cp/rm against tmp dirs

	srcPath := writeSourceImage(t)
	loop := &fakeLoopDev{loopDevPath: "/dev/loop7", partitions: []string{"/dev/loop7p1"}}
	ing := newTestIngestor(t, &config.BaselineSource{Path: srcPath}, loop, false)

	var seenCopyPath string
	err := ing.WithBaseline(func(ctx *Context) error {
		seenCopyPath = ctx.BaselineCopyPath

		// Source RAW is copied (not symlinked, not moved) into the workspace.
		if _, statErr := os.Stat(srcPath); statErr != nil {
			t.Errorf("source image must not be moved/removed: %v", statErr)
		}
		info, statErr := os.Lstat(ctx.BaselineCopyPath)
		if statErr != nil {
			t.Fatalf("workspace copy missing: %v", statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			t.Errorf("workspace copy must be a real file, not a symlink")
		}
		got, readErr := os.ReadFile(ctx.BaselineCopyPath)
		if readErr != nil {
			t.Fatalf("read workspace copy: %v", readErr)
		}
		if string(got) != "baseline-bytes" {
			t.Errorf("copy content = %q, want %q", got, "baseline-bytes")
		}

		// Loop device path + partitions stored in context for downstream stages.
		if ctx.LoopDevPath != "/dev/loop7" {
			t.Errorf("LoopDevPath = %q, want /dev/loop7", ctx.LoopDevPath)
		}
		if len(ctx.Partitions) != 1 || ctx.Partitions[0] != "/dev/loop7p1" {
			t.Errorf("Partitions = %v, want [/dev/loop7p1]", ctx.Partitions)
		}
		// Loop attach received the workspace copy, never the original source.
		if loop.attachedPath != ctx.BaselineCopyPath {
			t.Errorf("attached %q, want workspace copy %q", loop.attachedPath, ctx.BaselineCopyPath)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithBaseline: %v", err)
	}

	// defer-based cleanup detaches the loop device exactly once.
	if loop.detachCallCount != 1 {
		t.Errorf("detach call count = %d, want 1", loop.detachCallCount)
	}
	if loop.detachedPath != "/dev/loop7" {
		t.Errorf("detached %q, want /dev/loop7", loop.detachedPath)
	}
	// Workspace copy removed on full build success.
	if _, statErr := os.Stat(seenCopyPath); !os.IsNotExist(statErr) {
		t.Errorf("workspace copy should be removed on success, stat err = %v", statErr)
	}
}

func TestWithBaseline_RetainsCopyWhenConfigured(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	shell.Default = shell.NewMockExecutor(nil)

	srcPath := writeSourceImage(t)
	loop := &fakeLoopDev{loopDevPath: "/dev/loop3"}
	ing := newTestIngestor(t, &config.BaselineSource{Path: srcPath}, loop, true) // retain

	var copyPath string
	if err := ing.WithBaseline(func(ctx *Context) error {
		copyPath = ctx.BaselineCopyPath
		return nil
	}); err != nil {
		t.Fatalf("WithBaseline: %v", err)
	}

	if loop.detachCallCount != 1 {
		t.Errorf("detach call count = %d, want 1", loop.detachCallCount)
	}
	// Copy retained for debugging even after success.
	if _, statErr := os.Stat(copyPath); statErr != nil {
		t.Errorf("workspace copy should be retained, stat err = %v", statErr)
	}
}

func TestWithBaseline_DetachesAndRemovesCopyOnError(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	shell.Default = shell.NewMockExecutor(nil)

	srcPath := writeSourceImage(t)
	loop := &fakeLoopDev{loopDevPath: "/dev/loop1"}
	ing := newTestIngestor(t, &config.BaselineSource{Path: srcPath}, loop, false)

	wantErr := fmt.Errorf("downstream stage failed")
	var copyPath string
	err := ing.WithBaseline(func(ctx *Context) error {
		copyPath = ctx.BaselineCopyPath
		return wantErr
	})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	// Loop device detached even on failure.
	if loop.detachCallCount != 1 {
		t.Errorf("detach call count = %d, want 1", loop.detachCallCount)
	}
	// On a plain fn failure with a clean detach (and retention off) the large
	// baseline copy is removed so repeated failures don't accumulate disk usage.
	if _, statErr := os.Stat(copyPath); !os.IsNotExist(statErr) {
		t.Errorf("workspace copy should be removed on error, stat err = %v", statErr)
	}
}

func TestWithBaseline_RetainsCopyOnErrorWhenDebugRetention(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	shell.Default = shell.NewMockExecutor(nil)

	srcPath := writeSourceImage(t)
	loop := &fakeLoopDev{loopDevPath: "/dev/loop1"}
	// retainCopy=true simulates debug mode: the copy must survive a failure for
	// postmortem inspection even though the detach was clean.
	ing := newTestIngestor(t, &config.BaselineSource{Path: srcPath}, loop, true)

	var copyPath string
	err := ing.WithBaseline(func(ctx *Context) error {
		copyPath = ctx.BaselineCopyPath
		return fmt.Errorf("downstream stage failed")
	})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if loop.detachCallCount != 1 {
		t.Errorf("detach call count = %d, want 1", loop.detachCallCount)
	}
	if _, statErr := os.Stat(copyPath); statErr != nil {
		t.Errorf("workspace copy should be retained on error under debug retention, stat err = %v", statErr)
	}
}

func TestWithBaseline_DetachesOnPanic(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	shell.Default = shell.NewMockExecutor(nil)

	srcPath := writeSourceImage(t)
	loop := &fakeLoopDev{loopDevPath: "/dev/loop9"}
	ing := newTestIngestor(t, &config.BaselineSource{Path: srcPath}, loop, false)

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("expected panic to propagate")
			}
		}()
		// The return value is unreachable (fn panics), but capture and assert on
		// it anyway so errcheck stays satisfied.
		if err := ing.WithBaseline(func(ctx *Context) error {
			panic("boom")
		}); err != nil {
			t.Errorf("unexpected error return: %v", err)
		}
	}()

	// Loop device detached even on panic.
	if loop.detachCallCount != 1 {
		t.Errorf("detach call count = %d, want 1 (cleanup must run on panic)", loop.detachCallCount)
	}

	// A panic is an unsuccessful run: the workspace copy must be retained (not
	// removed as if the build had fully succeeded) so the failure can be
	// debugged. This guards the recover()-based panic handling in WithBaseline.
	copyPath := filepath.Join(ing.workDir, baselineCopyName)
	if _, err := os.Stat(copyPath); err != nil {
		t.Errorf("workspace baseline copy must be retained after panic, but Stat(%q) failed: %v", copyPath, err)
	}
}

func TestWithBaseline_LoopAttachFailureRemovesCopy(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	shell.Default = shell.NewMockExecutor(nil)

	srcPath := writeSourceImage(t)
	loop := &fakeLoopDev{failAttach: true}
	ing := newTestIngestor(t, &config.BaselineSource{Path: srcPath}, loop, false)

	fnCalled := false
	err := ing.WithBaseline(func(ctx *Context) error {
		fnCalled = true
		return nil
	})
	if err == nil {
		t.Fatalf("expected attach failure error, got nil")
	}
	if fnCalled {
		t.Errorf("fn must not be called when loop attach fails")
	}
	// No detach attempted (nothing was attached).
	if loop.detachCallCount != 0 {
		t.Errorf("detach call count = %d, want 0", loop.detachCallCount)
	}
	// Copy removed since attach failed (no partial workspace state).
	copyPath := filepath.Join(ing.workDir, baselineCopyName)
	if _, statErr := os.Stat(copyPath); !os.IsNotExist(statErr) {
		t.Errorf("workspace copy should be removed on attach failure, stat err = %v", statErr)
	}
}

func TestWithBaseline_RetainsCopyWhenAttachLeaksLoopDevice(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	shell.Default = shell.NewMockExecutor(nil)

	srcPath := writeSourceImage(t)
	// Attach fails but returns a non-empty device path: a loop device was created
	// and could not be detached, so it still references the backing file.
	loop := &fakeLoopDev{failAttach: true, leakOnAttach: true, loopDevPath: "/dev/loop8"}
	ing := newTestIngestor(t, &config.BaselineSource{Path: srcPath}, loop, false)

	err := ing.WithBaseline(func(ctx *Context) error {
		t.Errorf("fn must not be called when loop attach fails")
		return nil
	})
	if err == nil {
		t.Fatalf("expected attach failure error, got nil")
	}
	// The copy must be retained: removing it would unlink a file the leaked loop
	// device still points at, making recovery/debugging harder.
	copyPath := filepath.Join(ing.workDir, baselineCopyName)
	if _, statErr := os.Stat(copyPath); statErr != nil {
		t.Errorf("workspace copy should be retained when a loop device leaks, stat err = %v", statErr)
	}
}

func TestNewIngestor_DefaultsEmptySystemConfigName(t *testing.T) {
	// systemConfig.name is schema-optional; an otherwise-valid overlay template
	// that omits it must not be rejected. NewIngestor should fall back to the
	// default workspace segment instead.
	tmpl := &config.ImageTemplate{
		Image:  config.ImageInfo{Name: "img"},
		Target: config.TargetInfo{OS: "ubuntu", Dist: "ubuntu24", Arch: "amd64"},
		Baseline: &config.Baseline{
			Mode:   config.BaselineModeOverlay,
			Source: &config.BaselineSource{Path: "/some/baseline.raw"},
		},
		// SystemConfig.Name intentionally left empty.
	}
	ing, err := NewIngestor(tmpl)
	if err != nil {
		t.Fatalf("NewIngestor with empty systemConfig.name should succeed, got %v", err)
	}
	if !strings.HasSuffix(ing.workDir, filepath.Join("overlay", defaultSysConfigName)) {
		t.Errorf("workDir = %q, want it to end with overlay/%s", ing.workDir, defaultSysConfigName)
	}
}

func TestWithBaseline_MissingSourceFileFails(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	shell.Default = shell.NewMockExecutor(nil)

	loop := &fakeLoopDev{}
	ing := newTestIngestor(t, &config.BaselineSource{Path: "/nonexistent/baseline.raw"}, loop, false)

	err := ing.WithBaseline(func(ctx *Context) error {
		t.Errorf("fn must not run when copy fails")
		return nil
	})
	if err == nil {
		t.Fatalf("expected copy failure error, got nil")
	}
	if loop.detachCallCount != 0 {
		t.Errorf("detach call count = %d, want 0", loop.detachCallCount)
	}
}

func TestWithBaseline_NilFnReturnsError(t *testing.T) {
	loop := &fakeLoopDev{}
	ing := newTestIngestor(t, &config.BaselineSource{Path: "/does/not/matter"}, loop, false)

	if err := ing.WithBaseline(nil); err == nil {
		t.Fatalf("expected error for nil fn, got nil")
	}
	// Nothing should have been acquired when fn is nil.
	if loop.detachCallCount != 0 {
		t.Errorf("detach call count = %d, want 0", loop.detachCallCount)
	}
}

func TestWithBaseline_DetachFailureOnSuccessFailsRun(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	shell.Default = shell.NewMockExecutor(nil)

	srcPath := writeSourceImage(t)
	loop := &fakeLoopDev{loopDevPath: "/dev/loop4", failDetach: true}
	ing := newTestIngestor(t, &config.BaselineSource{Path: srcPath}, loop, false)

	var copyPath string
	err := ing.WithBaseline(func(ctx *Context) error {
		copyPath = ctx.BaselineCopyPath
		return nil // fn succeeds; only detach fails
	})
	if err == nil {
		t.Fatalf("expected detach failure to be surfaced, got nil")
	}
	if loop.detachCallCount != 1 {
		t.Errorf("detach call count = %d, want 1", loop.detachCallCount)
	}
	// A failed detach marks the run unsuccessful, so the copy is retained.
	if _, statErr := os.Stat(copyPath); statErr != nil {
		t.Errorf("workspace copy should be retained when detach fails, stat err = %v", statErr)
	}
}

func TestWithBaseline_DetachFailureSurfacesAlongsideFnError(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	shell.Default = shell.NewMockExecutor(nil)

	srcPath := writeSourceImage(t)
	loop := &fakeLoopDev{loopDevPath: "/dev/loop4", failDetach: true}
	ing := newTestIngestor(t, &config.BaselineSource{Path: srcPath}, loop, false)

	fnErr := fmt.Errorf("downstream boom")
	err := ing.WithBaseline(func(ctx *Context) error {
		return fnErr
	})
	if err == nil {
		t.Fatalf("expected combined error, got nil")
	}
	// Both the fn error and the detach failure must reach the caller so a leaked
	// loop device is never hidden behind the downstream error.
	if !errors.Is(err, fnErr) {
		t.Errorf("returned error must wrap fn error, got %v", err)
	}
	if !strings.Contains(err.Error(), "fake detach failure") {
		t.Errorf("returned error must include detach failure, got %v", err)
	}
}

func TestNewIngestor_RejectsNonOverlayTemplate(t *testing.T) {
	tmpl := &config.ImageTemplate{} // create mode (no baseline)
	if _, err := NewIngestor(tmpl); err == nil {
		t.Fatalf("expected error for non-overlay template")
	}
}

func TestNewIngestor_RejectsNilTemplate(t *testing.T) {
	if _, err := NewIngestor(nil); err == nil {
		t.Fatalf("expected error for nil template")
	}
}

func TestNewIngestor_RejectsUnsafeImageName(t *testing.T) {
	// A programmatic template can carry an image.name that never passed schema
	// validation. Since it is later joined into the emitted artifact path, a name
	// with traversal/separators must be rejected up front.
	tmpl := &config.ImageTemplate{
		Image:        config.ImageInfo{Name: "../../etc/evil"},
		Target:       config.TargetInfo{OS: "ubuntu", Dist: "ubuntu24", Arch: "amd64"},
		SystemConfig: config.SystemConfig{Name: "valid"}, // valid, so image.name is the failure point
		Baseline: &config.Baseline{
			Mode:   config.BaselineModeOverlay,
			Source: &config.BaselineSource{Path: "/some/baseline.raw"},
		},
	}
	_, err := NewIngestor(tmpl)
	if err == nil {
		t.Fatalf("expected error for image name with path separators")
	}
	if !strings.Contains(err.Error(), "invalid image name") {
		t.Errorf("error = %v, want it to name the image-name validation", err)
	}
}

func TestValidatePathSegment(t *testing.T) {
	tests := []struct {
		name    string
		segment string
		wantErr bool
	}{
		{"plain name", "my-config", false},
		{"name with dot", "config.v2", false},
		{"underscore and digits", "cfg_01", false},
		{"empty", "", true},
		{"whitespace only", "   ", true},
		{"internal whitespace", "a b", true},
		{"dot", ".", true},
		{"dotdot", "..", true},
		{"parent traversal", "../etc", true},
		{"nested traversal", "a/../../b", true},
		{"absolute path", "/etc/passwd", true},
		{"embedded separator", "a/b", true},
		{"trailing separator", "a/", true},
		{"semicolon", "a;rm -rf", true},
		{"command substitution", "$(id)", true},
		{"backtick", "a`id`", true},
		{"ampersand", "a&b", true},
		{"pipe", "a|b", true},
		{"glob", "a*", true},
		{"quote", "a'b", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePathSegment(tt.segment)
			if tt.wantErr && err == nil {
				t.Errorf("validatePathSegment(%q) = nil, want error", tt.segment)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("validatePathSegment(%q) = %v, want nil", tt.segment, err)
			}
		})
	}
}

// TestCopyLocalFile_RoundTripsContentWithZeroRuns verifies that the sparse-aware
// copy reproduces the source byte-for-byte and at the exact size even when the
// content contains large zero runs (which the copy turns into holes). It asserts
// correctness of the copied bytes/size, not that the destination is physically
// sparse — hole allocation depends on the underlying filesystem.
func TestCopyLocalFile_RoundTripsContentWithZeroRuns(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.raw")
	dst := filepath.Join(dir, "dst.raw")

	// Build content that exercises the zero-run detection: a leading hole
	// spanning multiple chunks, real data, an interior hole, more data, and a
	// trailing hole (which must be preserved by the final Truncate).
	var want bytes.Buffer
	want.Write(make([]byte, sparseCopyChunk*2+123)) // leading zeros, non-aligned
	want.WriteString("hello-baseline")
	want.Write(make([]byte, sparseCopyChunk+7)) // interior hole
	want.WriteString("more-data")
	want.Write(make([]byte, sparseCopyChunk*3)) // trailing hole
	if err := os.WriteFile(src, want.Bytes(), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	if err := copyLocalFile(src, dst); err != nil {
		t.Fatalf("copyLocalFile: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if !bytes.Equal(got, want.Bytes()) {
		t.Errorf("copied content differs: got %d bytes, want %d bytes", len(got), want.Len())
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat dst: %v", err)
	}
	if info.Size() != int64(want.Len()) {
		t.Errorf("dst size = %d, want %d", info.Size(), want.Len())
	}
}

// TestPrepareDestination_BreaksHardlinkWithoutTruncatingVictim verifies that a
// pre-existing hardlink at dst is unlinked (its name removed) rather than
// truncated, so the sibling file sharing the inode keeps its contents. This is the
// vector a plain O_TRUNC open would clobber.
func TestPrepareDestination_BreaksHardlinkWithoutTruncatingVictim(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	dst := filepath.Join(dir, "baseline.raw")

	const payload = "do-not-lose-me"
	if err := os.WriteFile(victim, []byte(payload), 0600); err != nil {
		t.Fatalf("write victim: %v", err)
	}
	if err := os.Link(victim, dst); err != nil {
		t.Fatalf("hardlink dst -> victim: %v", err)
	}

	if err := prepareDestination(dst); err != nil {
		t.Fatalf("prepareDestination: %v", err)
	}

	// dst's name is gone; the victim (and its data) survive untouched.
	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		t.Errorf("dst should be unlinked, lstat err = %v", err)
	}
	got, err := os.ReadFile(victim)
	if err != nil {
		t.Fatalf("read victim: %v", err)
	}
	if string(got) != payload {
		t.Errorf("victim content = %q, want %q (must not be truncated)", got, payload)
	}
}

// TestPrepareDestination_DropsSymlinkWithoutFollowing verifies that a symlink at
// dst is removed without touching the file it points at.
func TestPrepareDestination_DropsSymlinkWithoutFollowing(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	dst := filepath.Join(dir, "baseline.raw")

	const payload = "symlink-target-contents"
	if err := os.WriteFile(target, []byte(payload), 0600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.Symlink(target, dst); err != nil {
		t.Fatalf("symlink dst -> target: %v", err)
	}

	if err := prepareDestination(dst); err != nil {
		t.Fatalf("prepareDestination: %v", err)
	}

	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		t.Errorf("symlink dst should be removed, lstat err = %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(got) != payload {
		t.Errorf("symlink target content = %q, want %q (must not be followed)", got, payload)
	}
}

// TestPrepareDestination_RefusesDirectory verifies a directory at dst is rejected
// rather than removed, since that signals a misconfigured workDir.
func TestPrepareDestination_RefusesDirectory(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "baseline.raw")
	if err := os.Mkdir(dst, 0700); err != nil {
		t.Fatalf("mkdir dst: %v", err)
	}
	if err := prepareDestination(dst); err == nil {
		t.Error("prepareDestination should reject a directory destination")
	}
	if _, err := os.Stat(dst); err != nil {
		t.Errorf("directory should be left intact, stat err = %v", err)
	}
}

// TestPrepareDestination_MissingIsNoop verifies the normal case: a non-existent
// dst is accepted without error and nothing is created.
func TestPrepareDestination_MissingIsNoop(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "baseline.raw")
	if err := prepareDestination(dst); err != nil {
		t.Fatalf("prepareDestination on missing dst: %v", err)
	}
	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		t.Errorf("prepareDestination must not create dst, lstat err = %v", err)
	}
}

// TestWriteBoundedBody_WritesWithMatchingContentLength verifies the happy path:
// a body whose length matches the declared Content-Length is written verbatim.
// testCap is a small size cap used by the writeBoundedBody tests so they can
// exercise the over-limit path without streaming gigabytes.
const testCap = 1024

func TestWriteBoundedBody_WritesWithMatchingContentLength(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "baseline.raw")
	payload := []byte("baseline-image-bytes")
	if err := writeBoundedBody(dst, int64(len(payload)), bytes.NewReader(payload), testCap); err != nil {
		t.Fatalf("writeBoundedBody: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("written content = %q, want %q", got, payload)
	}
}

// TestWriteBoundedBody_UnknownLengthWritesFully verifies a body with an unknown
// Content-Length (-1) under the cap is written in full (no premature truncation).
func TestWriteBoundedBody_UnknownLengthWritesFully(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "baseline.raw")
	payload := []byte("streamed-without-content-length")
	if err := writeBoundedBody(dst, -1, bytes.NewReader(payload), testCap); err != nil {
		t.Fatalf("writeBoundedBody: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("written content = %q, want %q", got, payload)
	}
}

// TestWriteBoundedBody_RejectsDeclaredOversize verifies an image whose declared
// Content-Length exceeds the cap is rejected before any bytes are written.
func TestWriteBoundedBody_RejectsDeclaredOversize(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "baseline.raw")
	err := writeBoundedBody(dst, testCap+1, strings.NewReader("x"), testCap)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected too-large error, got %v", err)
	}
	// Rejected before opening the file: nothing must be created.
	if _, statErr := os.Lstat(dst); !os.IsNotExist(statErr) {
		t.Errorf("dst must not be created on early rejection, lstat err = %v", statErr)
	}
}

// TestWriteBoundedBody_RejectsUndeclaredOversize verifies an undeclared-length
// body that streams past the cap is rejected via the io.LimitReader bound rather
// than silently truncated.
func TestWriteBoundedBody_RejectsUndeclaredOversize(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "baseline.raw")
	// A reader that always yields data would be unbounded; cap the source at
	// testCap+2 bytes so the test stays finite while still exceeding the limit.
	oversize := io.LimitReader(neverEnding{}, testCap+2)
	err := writeBoundedBody(dst, -1, oversize, testCap)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected too-large error, got %v", err)
	}
}

// TestWriteBoundedBody_RejectsShortBody verifies a body shorter than a declared
// Content-Length is rejected as an incomplete download rather than accepted as a
// corrupt baseline.
func TestWriteBoundedBody_RejectsShortBody(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "baseline.raw")
	payload := []byte("only-8b0")
	err := writeBoundedBody(dst, int64(len(payload)+100), bytes.NewReader(payload), testCap)
	if err == nil || !strings.Contains(err.Error(), "incomplete download") {
		t.Fatalf("expected incomplete-download error, got %v", err)
	}
}

// neverEnding is an io.Reader that yields an endless stream of zero bytes. Wrap it
// in an io.LimitReader to bound it in tests.
type neverEnding struct{}

func (neverEnding) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

// normalizeSeams snapshots the baseline-format normalization seams so a test can
// override them and restore the originals via defer, matching the resizeExec
// save/restore pattern. These are package globals, so tests using them must not run
// in parallel.
type normalizeSeams struct {
	detect    func(string) (string, error)
	convert   func(string, string, string) error
	qemuAvail func() bool
}

func saveNormalizeSeams() normalizeSeams {
	return normalizeSeams{detect: detectBaselineFormatFn, convert: convertBaselineToRawFn, qemuAvail: qemuImgAvailableFn}
}

func (s normalizeSeams) restore() {
	detectBaselineFormatFn = s.detect
	convertBaselineToRawFn = s.convert
	qemuImgAvailableFn = s.qemuAvail
}

// TestNormalizeBaseline_RawPassthroughNoConvert verifies a declared-raw source with
// qemu-img present is copied verbatim and never converted.
func TestNormalizeBaseline_RawPassthroughNoConvert(t *testing.T) {
	defer saveNormalizeSeams().restore()
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	shell.Default = shell.NewMockExecutor(nil)

	qemuImgAvailableFn = func() bool { return true }
	detectBaselineFormatFn = func(string) (string, error) { return "raw", nil }
	convertCalled := false
	convertBaselineToRawFn = func(string, string, string) error { convertCalled = true; return nil }

	srcPath := writeSourceImage(t)
	loop := &fakeLoopDev{loopDevPath: "/dev/loop7"}
	ing := newTestIngestor(t, &config.BaselineSource{Path: srcPath, Format: "raw"}, loop, false)

	ctx, err := ing.acquire()
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer func() { _ = ing.detach(ctx) }()

	if convertCalled {
		t.Errorf("convert must not run for a raw source")
	}
	got, readErr := os.ReadFile(ctx.BaselineCopyPath)
	if readErr != nil {
		t.Fatalf("read baseline.raw: %v", readErr)
	}
	if string(got) != "baseline-bytes" {
		t.Errorf("baseline.raw content = %q, want verbatim source bytes", got)
	}
	if loop.attachedPath != ctx.BaselineCopyPath {
		t.Errorf("attached %q, want baseline.raw %q", loop.attachedPath, ctx.BaselineCopyPath)
	}
}

// TestNormalizeBaseline_RawNoQemuImgSkipsDetection verifies the legacy path: a
// declared-raw source on a host without qemu-img is copied verbatim and detection
// is never invoked (backward compatibility).
func TestNormalizeBaseline_RawNoQemuImgSkipsDetection(t *testing.T) {
	defer saveNormalizeSeams().restore()
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	shell.Default = shell.NewMockExecutor(nil)

	qemuImgAvailableFn = func() bool { return false }
	detectCalled := false
	detectBaselineFormatFn = func(string) (string, error) { detectCalled = true; return "raw", nil }
	convertBaselineToRawFn = func(string, string, string) error { return errors.New("must not convert") }

	srcPath := writeSourceImage(t)
	loop := &fakeLoopDev{loopDevPath: "/dev/loop1"}
	ing := newTestIngestor(t, &config.BaselineSource{Path: srcPath, Format: "raw"}, loop, false)

	ctx, err := ing.acquire()
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer func() { _ = ing.detach(ctx) }()

	if detectCalled {
		t.Errorf("detection must not run when qemu-img is absent for a raw source")
	}
	got, readErr := os.ReadFile(ctx.BaselineCopyPath)
	if readErr != nil {
		t.Fatalf("read baseline.raw: %v", readErr)
	}
	if string(got) != "baseline-bytes" {
		t.Errorf("baseline.raw content = %q, want verbatim source bytes", got)
	}
}

// TestNormalizeBaseline_Qcow2TriggersConvert verifies a declared-qcow2 source is
// staged, verified, converted to baseline.raw, and the staging file is removed.
func TestNormalizeBaseline_Qcow2TriggersConvert(t *testing.T) {
	defer saveNormalizeSeams().restore()
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	shell.Default = shell.NewMockExecutor(nil)

	qemuImgAvailableFn = func() bool { return true }
	detectBaselineFormatFn = func(string) (string, error) { return "qcow2", nil }
	var gotSrc, gotDst, gotFormat string
	convertBaselineToRawFn = func(src, dst, srcFormat string) error {
		gotSrc, gotDst, gotFormat = src, dst, srcFormat
		// Simulate qemu-img producing the RAW output at dst.
		return os.WriteFile(dst, []byte("converted-raw"), 0600)
	}

	srcPath := writeSourceImage(t)
	loop := &fakeLoopDev{loopDevPath: "/dev/loop2"}
	ing := newTestIngestor(t, &config.BaselineSource{Path: srcPath, Format: "qcow2"}, loop, false)

	ctx, err := ing.acquire()
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer func() { _ = ing.detach(ctx) }()

	stagePath := filepath.Join(ing.workDir, baselineStageName)
	if gotSrc != stagePath {
		t.Errorf("convert src = %q, want staging path %q", gotSrc, stagePath)
	}
	if gotDst != ctx.BaselineCopyPath {
		t.Errorf("convert dst = %q, want baseline.raw %q", gotDst, ctx.BaselineCopyPath)
	}
	// The verified qemu-img-detected format is passed through to -f so qemu-img
	// does not re-probe the source independently of the detection above.
	if gotFormat != "qcow2" {
		t.Errorf("convert srcFormat = %q, want detected format %q", gotFormat, "qcow2")
	}
	if _, statErr := os.Stat(stagePath); !os.IsNotExist(statErr) {
		t.Errorf("staging file must be removed, stat err = %v", statErr)
	}
	if loop.attachedPath != ctx.BaselineCopyPath {
		t.Errorf("attached %q, want baseline.raw %q", loop.attachedPath, ctx.BaselineCopyPath)
	}
	got, _ := os.ReadFile(ctx.BaselineCopyPath)
	if string(got) != "converted-raw" {
		t.Errorf("baseline.raw content = %q, want converted output", got)
	}
}

// TestNormalizeBaseline_FormatMismatchErrors verifies a declared format that does
// not match the detected format fails, without converting or attaching.
func TestNormalizeBaseline_FormatMismatchErrors(t *testing.T) {
	defer saveNormalizeSeams().restore()
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	shell.Default = shell.NewMockExecutor(nil)

	qemuImgAvailableFn = func() bool { return true }
	detectBaselineFormatFn = func(string) (string, error) { return "raw", nil } // actual != declared
	convertCalled := false
	convertBaselineToRawFn = func(string, string, string) error { convertCalled = true; return nil }

	srcPath := writeSourceImage(t)
	loop := &fakeLoopDev{loopDevPath: "/dev/loop3"}
	ing := newTestIngestor(t, &config.BaselineSource{Path: srcPath, Format: "qcow2"}, loop, false)

	_, err := ing.acquire()
	if err == nil || !strings.Contains(err.Error(), "does not match declared") {
		t.Fatalf("expected format-mismatch error, got %v", err)
	}
	if convertCalled {
		t.Errorf("convert must not run on a format mismatch")
	}
	if loop.attachedPath != "" {
		t.Errorf("loop device must not be attached on a format mismatch")
	}
	if _, statErr := os.Stat(filepath.Join(ing.workDir, baselineStageName)); !os.IsNotExist(statErr) {
		t.Errorf("staging file must be cleaned up on error")
	}
	if _, statErr := os.Stat(filepath.Join(ing.workDir, baselineCopyName)); !os.IsNotExist(statErr) {
		t.Errorf("baseline.raw must be cleaned up on error")
	}
}

// TestNormalizeBaseline_NonRawRequiresQemuImg verifies a non-raw source on a host
// without qemu-img fails clearly before any copy or attach.
func TestNormalizeBaseline_NonRawRequiresQemuImg(t *testing.T) {
	defer saveNormalizeSeams().restore()
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	shell.Default = shell.NewMockExecutor(nil)

	qemuImgAvailableFn = func() bool { return false }
	detectBaselineFormatFn = func(string) (string, error) { return "", errors.New("must not detect") }
	convertBaselineToRawFn = func(string, string, string) error { return errors.New("must not convert") }

	srcPath := writeSourceImage(t)
	loop := &fakeLoopDev{loopDevPath: "/dev/loop4"}
	ing := newTestIngestor(t, &config.BaselineSource{Path: srcPath, Format: "qcow2"}, loop, false)

	_, err := ing.acquire()
	if err == nil || !strings.Contains(err.Error(), "requires qemu-img") {
		t.Fatalf("expected qemu-img-required error, got %v", err)
	}
	if loop.attachedPath != "" {
		t.Errorf("loop device must not be attached when qemu-img is missing")
	}
	if _, statErr := os.Stat(filepath.Join(ing.workDir, baselineStageName)); !os.IsNotExist(statErr) {
		t.Errorf("no staging file should be created when qemu-img is missing")
	}
}

// TestNormalizeBaseline_UnsupportedFormatRejected verifies a declared format
// outside the supported set is rejected up front — without copying, detecting, or
// converting — so programmatically-built templates get the same guard that config
// validation applies to YAML-loaded ones.
func TestNormalizeBaseline_UnsupportedFormatRejected(t *testing.T) {
	defer saveNormalizeSeams().restore()
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	shell.Default = shell.NewMockExecutor(nil)

	qemuImgAvailableFn = func() bool { return true }
	detectBaselineFormatFn = func(string) (string, error) { return "", errors.New("must not detect") }
	convertBaselineToRawFn = func(string, string, string) error { return errors.New("must not convert") }

	srcPath := writeSourceImage(t)
	loop := &fakeLoopDev{loopDevPath: "/dev/loop6"}
	ing := newTestIngestor(t, &config.BaselineSource{Path: srcPath, Format: "vmdk"}, loop, false)

	_, err := ing.acquire()
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected unsupported-format error, got %v", err)
	}
	if loop.attachedPath != "" {
		t.Errorf("loop device must not be attached for an unsupported format")
	}
	if _, statErr := os.Stat(filepath.Join(ing.workDir, baselineStageName)); !os.IsNotExist(statErr) {
		t.Errorf("no staging file should be created for an unsupported format")
	}
}

// TestNormalizeBaseline_ConvertFailureCleansUp verifies a conversion failure
// propagates and leaves neither the staging file nor baseline.raw behind.
func TestNormalizeBaseline_ConvertFailureCleansUp(t *testing.T) {
	defer saveNormalizeSeams().restore()
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	shell.Default = shell.NewMockExecutor(nil)

	qemuImgAvailableFn = func() bool { return true }
	detectBaselineFormatFn = func(string) (string, error) { return "vhd", nil }
	convertBaselineToRawFn = func(_, dst, _ string) error {
		// Simulate qemu-img writing a partial output then failing.
		_ = os.WriteFile(dst, []byte("partial"), 0600)
		return errors.New("fake convert failure")
	}

	srcPath := writeSourceImage(t)
	loop := &fakeLoopDev{loopDevPath: "/dev/loop5"}
	ing := newTestIngestor(t, &config.BaselineSource{Path: srcPath, Format: "vhd"}, loop, false)

	_, err := ing.acquire()
	if err == nil || !strings.Contains(err.Error(), "convert") {
		t.Fatalf("expected convert-failure error, got %v", err)
	}
	if loop.attachedPath != "" {
		t.Errorf("loop device must not be attached after a convert failure")
	}
	if _, statErr := os.Stat(filepath.Join(ing.workDir, baselineStageName)); !os.IsNotExist(statErr) {
		t.Errorf("staging file must be cleaned up after a convert failure")
	}
	if _, statErr := os.Stat(filepath.Join(ing.workDir, baselineCopyName)); !os.IsNotExist(statErr) {
		t.Errorf("partial baseline.raw must be cleaned up after a convert failure")
	}
}

// TestNormalizeBaseline_MislabeledRawCaught verifies a source declared raw but
// detected as a known non-raw format is rejected (guards the stricter raw branch).
func TestNormalizeBaseline_MislabeledRawCaught(t *testing.T) {
	defer saveNormalizeSeams().restore()
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	shell.Default = shell.NewMockExecutor(nil)

	qemuImgAvailableFn = func() bool { return true }
	detectBaselineFormatFn = func(string) (string, error) { return "qcow2", nil } // mislabeled

	srcPath := writeSourceImage(t)
	loop := &fakeLoopDev{loopDevPath: "/dev/loop6"}
	ing := newTestIngestor(t, &config.BaselineSource{Path: srcPath, Format: "raw"}, loop, false)

	_, err := ing.acquire()
	if err == nil || !strings.Contains(err.Error(), "does not match declared") {
		t.Fatalf("expected mislabeled-raw mismatch error, got %v", err)
	}
	if loop.attachedPath != "" {
		t.Errorf("loop device must not be attached for a mislabeled raw source")
	}
}

// TestNormalizeBaseline_RawDetectionErrorProceeds verifies that for a declared-raw
// source a detection error is advisory: the copy proceeds and attaches.
func TestNormalizeBaseline_RawDetectionErrorProceeds(t *testing.T) {
	defer saveNormalizeSeams().restore()
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	shell.Default = shell.NewMockExecutor(nil)

	qemuImgAvailableFn = func() bool { return true }
	detectBaselineFormatFn = func(string) (string, error) { return "", errors.New("qemu-img blew up") }

	srcPath := writeSourceImage(t)
	loop := &fakeLoopDev{loopDevPath: "/dev/loop8"}
	ing := newTestIngestor(t, &config.BaselineSource{Path: srcPath, Format: "raw"}, loop, false)

	ctx, err := ing.acquire()
	if err != nil {
		t.Fatalf("acquire should proceed on advisory raw detection error, got %v", err)
	}
	defer func() { _ = ing.detach(ctx) }()

	if loop.attachedPath != ctx.BaselineCopyPath {
		t.Errorf("attached %q, want baseline.raw %q", loop.attachedPath, ctx.BaselineCopyPath)
	}
}

func TestRedactURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "presigned query params redacted",
			in:   "https://bucket.s3.amazonaws.com/image.raw?X-Amz-Credential=AKIA123&X-Amz-Signature=deadbeef",
			want: "https://bucket.s3.amazonaws.com/image.raw?[REDACTED]",
		},
		{
			name: "userinfo password masked",
			in:   "https://user:secret@example.com/image.raw",
			want: "https://user:xxxxx@example.com/image.raw",
		},
		{
			name: "fragment dropped",
			in:   "https://example.com/image.raw#token=abc",
			want: "https://example.com/image.raw",
		},
		{
			name: "plain url unchanged",
			in:   "https://example.com/path/image.raw",
			want: "https://example.com/path/image.raw",
		},
		{
			name: "unparseable url strips query tail",
			in:   "https://exa mple.com/image.raw?sig=secret",
			want: "https://exa mple.com/image.raw?[REDACTED]",
		},
		{
			name: "unparseable url preserves fragment delimiter",
			in:   "https://exa mple.com/image.raw#tok=secret",
			want: "https://exa mple.com/image.raw#[REDACTED]",
		},
		{
			name: "unparseable url masks userinfo password",
			in:   "https://user:secret@exa mple.com/image.raw",
			want: "https://user:xxxxx@exa mple.com/image.raw",
		},
		{
			name: "unparseable url masks userinfo and strips query tail",
			in:   "https://user:secret@exa mple.com/image.raw?sig=abc",
			want: "https://user:xxxxx@exa mple.com/image.raw?[REDACTED]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := redactURL(tt.in); got != tt.want {
				t.Errorf("redactURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
