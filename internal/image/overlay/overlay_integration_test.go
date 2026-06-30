package overlay

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/image/imagedisc"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

// TestWithBaseline_RealLoopDevice exercises the full ingestion path against a
// real loop device: it copies a generated RAW image into the workspace, attaches
// it via losetup -fP, verifies the loop device exists, and confirms the deferred
// cleanup detaches it and removes the workspace copy.
//
// Requires root (losetup needs privilege) and the losetup binary. Skips otherwise
// so the suite stays green on unprivileged dev machines and CI sandboxes.
func TestWithBaseline_RealLoopDevice(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to attach a real loop device")
	}
	if _, err := os.Stat("/dev/loop-control"); err != nil {
		t.Skipf("loop devices unavailable: %v", err)
	}
	if ok, err := shell.IsCommandExist("losetup", shell.HostPath); err != nil || !ok {
		t.Skip("losetup not available on host")
	}

	// Generate a small RAW source image (8 MiB) — large enough for losetup.
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "source.raw")
	// shell.ExecCmd runs via bash -c, so single-quote the temp path (via
	// shell.QuoteArg) in case TMPDIR contains spaces or shell metacharacters.
	// Single quotes also suppress $()/backtick/${} expansion that double-quoting
	// would still allow.
	ddCmd := fmt.Sprintf("dd if=/dev/zero of=%s bs=1M count=8", shell.QuoteArg(srcPath))
	if _, err := shell.ExecCmd(ddCmd, false, shell.HostPath, nil); err != nil {
		t.Fatalf("create source image: %v", err)
	}

	tmpl := &config.ImageTemplate{
		Baseline: &config.Baseline{
			Mode:   config.BaselineModeOverlay,
			Source: &config.BaselineSource{Path: srcPath},
		},
	}
	ing := &Ingestor{
		template:   tmpl,
		loopDev:    imagedisc.NewLoopDev(),
		workDir:    filepath.Join(t.TempDir(), "overlay"),
		retainCopy: false,
	}

	var copyPath, loopDevPath string
	err := ing.WithBaseline(func(ctx *Context) error {
		copyPath = ctx.BaselineCopyPath
		loopDevPath = ctx.LoopDevPath

		// Source image untouched; workspace has a real copy.
		if _, statErr := os.Stat(srcPath); statErr != nil {
			t.Errorf("source image must remain after copy: %v", statErr)
		}
		if _, statErr := os.Stat(ctx.BaselineCopyPath); statErr != nil {
			t.Errorf("workspace copy missing: %v", statErr)
		}
		// Loop device node exists while attached.
		if _, statErr := os.Stat(ctx.LoopDevPath); statErr != nil {
			t.Errorf("loop device %s should exist: %v", ctx.LoopDevPath, statErr)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithBaseline: %v", err)
	}

	// After cleanup the loop device is detached and the workspace copy removed.
	// The /dev/loopX node typically persists after detach (it just loses its
	// backing file), so assert on losetup's active-device list rather than the
	// node's existence. The test already runs as root, so invoke losetup without
	// sudo — minimal root containers often lack the sudo binary.
	activeLoops, lsErr := shell.ExecCmd("losetup -a", false, shell.HostPath, nil)
	if lsErr != nil {
		t.Fatalf("losetup -a: %v", lsErr)
	}
	if strings.Contains(activeLoops, loopDevPath+":") {
		t.Errorf("loop device %s should be detached after cleanup, still listed by losetup -a:\n%s", loopDevPath, activeLoops)
	}
	if _, statErr := os.Stat(copyPath); !os.IsNotExist(statErr) {
		t.Errorf("workspace copy %s should be removed after success, stat err = %v", copyPath, statErr)
	}
}
