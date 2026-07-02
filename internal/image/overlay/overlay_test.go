package overlay

import (
	"fmt"
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
	detachCallCount int
	detachedPath    string
	failDetach      bool
}

func (f *fakeLoopDev) AttachImageToLoopDev(imagePath string) (string, []string, error) {
	f.attachedPath = imagePath
	if f.failAttach {
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

func TestWithBaseline_DetachesAndRetainsCopyOnError(t *testing.T) {
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
	// On failure the copy is retained for debugging (not removed).
	if _, statErr := os.Stat(copyPath); statErr != nil {
		t.Errorf("workspace copy should be retained on error, stat err = %v", statErr)
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
