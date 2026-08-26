package overlay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/image/imagedisc"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

// requireMountTooling skips the test unless it is running as root with the
// loop-device and partition/mkfs tooling needed to build and mount a real image.
func requireMountTooling(t *testing.T, bins ...string) {
	t.Helper()
	// These tests attach loop devices, mount filesystems, and may hit the network
	// (mmdebstrap), so they are slow and environment-sensitive. Skip them under
	// `go test -short` so a root host with the tooling installed (e.g. CI running
	// the default suite) can opt out consistently.
	if testing.Short() {
		t.Skip("skipping root/loop-device integration test in -short mode")
	}
	if os.Geteuid() != 0 {
		t.Skip("requires root to attach loop devices and mount filesystems")
	}
	if _, err := os.Stat("/dev/loop-control"); err != nil {
		t.Skipf("loop devices unavailable: %v", err)
	}
	// The code under test (WithMountedLayout via internal/utils/mount and
	// shell.ExecCmd(..., sudo=true)) unconditionally prefixes commands with sudo,
	// so require the sudo binary too — minimal root-run CI containers often lack
	// it, and without this check the tests would fail rather than skip.
	bins = append(bins, "sudo")
	for _, b := range bins {
		if ok, err := shell.IsCommandExist(b, shell.HostPath); err != nil || !ok {
			t.Skipf("%s not available on host", b)
		}
	}
}

// buildGPTImage creates a RAW image with a GPT table, a small vfat ESP, and an
// ext4 root, then returns its path. It uses the shell allowlist throughout.
func buildGPTImage(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	img := filepath.Join(dir, "baseline.raw")

	if _, err := shell.ExecCmd("dd if=/dev/zero of="+shell.QuoteArg(img)+" bs=1M count=64", false, shell.HostPath, nil); err != nil {
		t.Fatalf("create image: %v", err)
	}
	// GPT: p1 ESP (EF00) 1-16MiB, p2 Linux root for the remainder.
	for _, c := range []string{
		"sgdisk -n 1:1MiB:16MiB -t 1:EF00 -c 1:EFI " + shell.QuoteArg(img),
		"sgdisk -n 2:16MiB:0 -t 2:8300 -c 2:root " + shell.QuoteArg(img),
	} {
		if _, err := shell.ExecCmd(c, false, shell.HostPath, nil); err != nil {
			t.Fatalf("partition (%s): %v", c, err)
		}
	}
	return img
}

// TestWithMountedLayout_RealGPTImage builds a real GPT image, attaches it, mounts
// the layout, asserts the root/ESP are mounted, and verifies deferred unmount.
func TestWithMountedLayout_RealGPTImage(t *testing.T) {
	requireMountTooling(t, "losetup", "lsblk", "sgdisk", "mkfs", "mount", "umount", "findmnt")
	if ok, err := shell.IsCommandExist("mkfs.ext4", shell.HostPath); err != nil || !ok {
		t.Skip("mkfs.ext4 not available")
	}
	if ok, err := shell.IsCommandExist("mkfs.vfat", shell.HostPath); err != nil || !ok {
		t.Skip("mkfs.vfat not available")
	}

	img := buildGPTImage(t)

	loop := imagedisc.NewLoopDev()
	loopDev, parts, _, err := loop.AttachImageToLoopDev(img)
	if err != nil {
		t.Fatalf("attach loop: %v", err)
	}
	defer func() {
		if derr := loop.LoopSetupDelete(loopDev); derr != nil {
			t.Logf("detach cleanup: %v", derr)
		}
	}()
	if len(parts) < 2 {
		t.Fatalf("expected >=2 partitions, got %v", parts)
	}

	// Format ESP (p1) and root (p2). Invoke via "mkfs -t <fs>" so the first
	// command token stays the allowlisted "mkfs" (mkfs.vfat/mkfs.ext4 are not
	// commandMap keys and would be rejected by the shell allowlist).
	if _, err := shell.ExecCmd("mkfs -t vfat "+shell.QuoteArg(parts[0]), true, shell.HostPath, nil); err != nil {
		t.Fatalf("mkfs -t vfat: %v", err)
	}
	if _, err := shell.ExecCmd("mkfs -t ext4 -F "+shell.QuoteArg(parts[1]), true, shell.HostPath, nil); err != nil {
		t.Fatalf("mkfs -t ext4: %v", err)
	}

	insp := NewInspector(filepath.Join(t.TempDir(), "overlay"))
	var rootMount, espMount string
	err = insp.WithMountedLayout(loopDev, func(l *Layout) error {
		rootMount, espMount = l.RootMount, l.ESPMount
		if l.RootFSType != "ext4" {
			t.Errorf("root fs = %s, want ext4", l.RootFSType)
		}
		if _, serr := os.Stat(l.RootMount); serr != nil {
			t.Errorf("root mount missing: %v", serr)
		}
		// Writing under the mounted root must succeed (overlay needs write access).
		probe := filepath.Join(l.RootMount, "overlay-probe")
		if _, werr := shell.ExecCmd("touch "+shell.QuoteArg(probe), true, shell.HostPath, nil); werr != nil {
			t.Errorf("root not writable: %v", werr)
		}
		if l.ESPDevice == "" {
			t.Errorf("expected an ESP to be detected")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithMountedLayout: %v", err)
	}

	// After cleanup, neither mount point is still mounted.
	for _, mp := range []string{espMount, rootMount} {
		if mp == "" {
			continue
		}
		// findmnt exits non-zero when the target is not mounted, which is the
		// success case here. Treat a non-empty match as the failure and only
		// surface an execution error when findmnt produced output we can't trust.
		out, ferr := shell.ExecCmd("findmnt -n "+shell.QuoteArg(mp), true, shell.HostPath, nil)
		if trimmed := strings.TrimSpace(out); trimmed != "" {
			t.Errorf("%s still mounted after cleanup: %q (findmnt err: %v)", mp, trimmed, ferr)
		}
	}
}

// TestWithMountedLayout_RejectsRealLUKS formats a partition as LUKS and confirms
// the layout analyzer rejects it before any mount is attempted.
func TestWithMountedLayout_RejectsRealLUKS(t *testing.T) {
	requireMountTooling(t, "losetup", "lsblk", "sgdisk", "cryptsetup")

	dir := t.TempDir()
	img := filepath.Join(dir, "luks.raw")
	if _, err := shell.ExecCmd("dd if=/dev/zero of="+shell.QuoteArg(img)+" bs=1M count=48", false, shell.HostPath, nil); err != nil {
		t.Fatalf("create image: %v", err)
	}
	if _, err := shell.ExecCmd("sgdisk -n 1:1MiB:0 -t 1:8300 -c 1:root "+shell.QuoteArg(img), false, shell.HostPath, nil); err != nil {
		t.Fatalf("partition: %v", err)
	}

	loop := imagedisc.NewLoopDev()
	loopDev, parts, _, err := loop.AttachImageToLoopDev(img)
	if err != nil {
		t.Fatalf("attach loop: %v", err)
	}
	defer func() {
		if derr := loop.LoopSetupDelete(loopDev); derr != nil {
			t.Logf("detach cleanup: %v", derr)
		}
	}()
	if len(parts) < 1 {
		t.Fatalf("expected a partition, got %v", parts)
	}

	// luksFormat non-interactively with a throwaway key.
	fmtCmd := "cryptsetup luksFormat --batch-mode --key-file /dev/urandom --keyfile-size 32 " + shell.QuoteArg(parts[0])
	if _, err := shell.ExecCmd(fmtCmd, true, shell.HostPath, nil); err != nil {
		t.Skipf("cryptsetup luksFormat unavailable in this environment: %v", err)
	}
	// Re-read so lsblk reports the new crypto_LUKS signature. Use partx (which is
	// on the shell allowlist; partprobe is not) to refresh the kernel's partition
	// view. Best-effort: a probe failure here is non-fatal (the layout check below
	// is the real assertion), but log it rather than discarding the error so a
	// flaky probe is diagnosable.
	if _, perr := shell.ExecCmd("partx -u "+shell.QuoteArg(loopDev), true, shell.HostPath, nil); perr != nil {
		t.Logf("partx -u (non-fatal): %v", perr)
	}

	insp := NewInspector(filepath.Join(t.TempDir(), "overlay"))
	err = insp.WithMountedLayout(loopDev, func(l *Layout) error {
		t.Errorf("fn must not run for a LUKS layout")
		return nil
	})
	if err == nil {
		t.Fatal("expected LUKS rejection, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "luks") {
		t.Errorf("error %q should mention LUKS", err)
	}
}

// TestWithMountedLayout_RejectsRealDMVerity creates a GPT image with a dm-verity
// root-hash partition type and confirms it is rejected up front.
func TestWithMountedLayout_RejectsRealDMVerity(t *testing.T) {
	requireMountTooling(t, "losetup", "lsblk", "sgdisk")

	dir := t.TempDir()
	img := filepath.Join(dir, "verity.raw")
	if _, err := shell.ExecCmd("dd if=/dev/zero of="+shell.QuoteArg(img)+" bs=1M count=48", false, shell.HostPath, nil); err != nil {
		t.Fatalf("create image: %v", err)
	}
	// p1 plain root, p2 typed as x86-64 root verity (GUID 2c7357ed-...).
	for _, c := range []string{
		"sgdisk -n 1:1MiB:24MiB -t 1:8300 -c 1:root " + shell.QuoteArg(img),
		"sgdisk -n 2:24MiB:0 -t 2:2C7357ED-EBD2-46D9-AEC1-23D437EC2BF5 -c 2:root-verity " + shell.QuoteArg(img),
	} {
		if _, err := shell.ExecCmd(c, false, shell.HostPath, nil); err != nil {
			t.Fatalf("partition (%s): %v", c, err)
		}
	}

	loop := imagedisc.NewLoopDev()
	loopDev, _, _, err := loop.AttachImageToLoopDev(img)
	if err != nil {
		t.Fatalf("attach loop: %v", err)
	}
	defer func() {
		if derr := loop.LoopSetupDelete(loopDev); derr != nil {
			t.Logf("detach cleanup: %v", derr)
		}
	}()

	insp := NewInspector(filepath.Join(t.TempDir(), "overlay"))
	err = insp.WithMountedLayout(loopDev, func(l *Layout) error {
		t.Errorf("fn must not run for a dm-verity layout")
		return nil
	})
	if err == nil {
		t.Fatal("expected dm-verity rejection, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "verity") {
		t.Errorf("error %q should mention verity", err)
	}
}
