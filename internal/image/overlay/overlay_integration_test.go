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
	if ok, err := shell.IsCommandExist("dd", shell.HostPath); err != nil || !ok {
		t.Skip("dd not available on host")
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

// TestWithBaseline_RealQcow2Baseline exercises the full normalization path against a
// real QCOW2 source: it generates a RAW image, converts it to QCOW2 with qemu-img,
// then runs ingestion with baseline.source.format="qcow2". The overlay converts the
// QCOW2 back to RAW and attaches it via losetup, proving the conversion produced a
// valid, attachable whole-disk image; cleanup then detaches it.
//
// Requires root (losetup) plus dd and qemu-img. Skips otherwise so the suite stays
// green on unprivileged dev machines and CI sandboxes.
func TestWithBaseline_RealQcow2Baseline(t *testing.T) {
	// Root + losetup + qemu-img + dd make this slow and environment-sensitive, so
	// skip under `go test -short` — consistent with the other overlay integration
	// tests (e.g. layout_integration_test.go's requireMountTooling) so CI/devs can
	// opt out of root-dependent tests uniformly.
	if testing.Short() {
		t.Skip("skipping root/loop-device integration test in -short mode")
	}
	if os.Geteuid() != 0 {
		t.Skip("requires root to attach a real loop device")
	}
	if _, err := os.Stat("/dev/loop-control"); err != nil {
		t.Skipf("loop devices unavailable: %v", err)
	}
	for _, bin := range []string{"losetup", "dd", "qemu-img"} {
		if ok, err := shell.IsCommandExist(bin, shell.HostPath); err != nil || !ok {
			t.Skipf("%s not available on host", bin)
		}
	}

	// Generate a RAW image, then convert it to QCOW2 with qemu-img so the source is
	// genuinely a non-RAW format the overlay must normalize.
	srcDir := t.TempDir()
	rawPath := filepath.Join(srcDir, "source.raw")
	qcow2Path := filepath.Join(srcDir, "source.qcow2")
	ddCmd := fmt.Sprintf("dd if=/dev/zero of=%s bs=1M count=8", shell.QuoteArg(rawPath))
	if _, err := shell.ExecCmd(ddCmd, false, shell.HostPath, nil); err != nil {
		t.Fatalf("create raw source image: %v", err)
	}
	convCmd := fmt.Sprintf("qemu-img convert -O qcow2 %s %s", shell.QuoteArg(rawPath), shell.QuoteArg(qcow2Path))
	if _, err := shell.ExecCmd(convCmd, false, shell.HostPath, nil); err != nil {
		t.Fatalf("convert raw to qcow2: %v", err)
	}

	tmpl := &config.ImageTemplate{
		Baseline: &config.Baseline{
			Mode:   config.BaselineModeOverlay,
			Source: &config.BaselineSource{Path: qcow2Path, Format: config.BaselineFormatQcow2},
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

		// The QCOW2 source is untouched; the workspace holds a converted RAW copy.
		if _, statErr := os.Stat(qcow2Path); statErr != nil {
			t.Errorf("qcow2 source must remain after conversion: %v", statErr)
		}
		if !strings.HasSuffix(ctx.BaselineCopyPath, baselineCopyName) {
			t.Errorf("baseline copy path = %q, want suffix %q", ctx.BaselineCopyPath, baselineCopyName)
		}
		if _, statErr := os.Stat(ctx.BaselineCopyPath); statErr != nil {
			t.Errorf("converted workspace copy missing: %v", statErr)
		}
		// The staging file must be gone once conversion completed.
		if _, statErr := os.Stat(filepath.Join(ing.workDir, baselineStageName)); !os.IsNotExist(statErr) {
			t.Errorf("staging file should be removed after conversion, stat err = %v", statErr)
		}
		if _, statErr := os.Stat(ctx.LoopDevPath); statErr != nil {
			t.Errorf("loop device %s should exist: %v", ctx.LoopDevPath, statErr)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithBaseline: %v", err)
	}

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
