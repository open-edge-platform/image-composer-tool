package overlay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/image/imagedisc"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

// seedGrubBaseline lays down, under a mounted root, the minimal boot artifacts a
// GRUB2 baseline carries: a /boot/grub directory (so detectBootloader classifies
// it grub2), an /etc/default/grub with a GRUB_CMDLINE_LINUX line, and a fake
// added kernel (vmlinuz image + module dir) at newKernel. It uses sudo-routed
// shell writes because the mounted root is root-owned.
func seedGrubBaseline(t *testing.T, rootMount, newKernel string) {
	t.Helper()
	dirs := []string{
		filepath.Join(rootMount, "boot", "grub"),
		filepath.Join(rootMount, "etc", "default"),
		filepath.Join(rootMount, "lib", "modules", newKernel),
	}
	for _, d := range dirs {
		if _, err := shell.ExecCmd("mkdir -p "+shell.QuoteArg(d), true, shell.HostPath, nil); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	// A representative /etc/default/grub with the lines the apply step rewrites.
	grubDefaults := "GRUB_TIMEOUT=5\n" +
		"GRUB_DEFAULT=0\n" +
		"GRUB_CMDLINE_LINUX=\"quiet splash\"\n" +
		"GRUB_CMDLINE_LINUX_DEFAULT=\"\"\n"
	staged := filepath.Join(t.TempDir(), "grub")
	if err := os.WriteFile(staged, []byte(grubDefaults), 0o644); err != nil {
		t.Fatalf("stage grub defaults: %v", err)
	}
	dst := filepath.Join(rootMount, grubDefaultsRelPath)
	if _, err := shell.ExecCmd("cp "+shell.QuoteArg(staged)+" "+shell.QuoteArg(dst), true, shell.HostPath, nil); err != nil {
		t.Fatalf("install grub defaults: %v", err)
	}
	vmlinuz := filepath.Join(rootMount, "boot", "vmlinuz-"+newKernel)
	if _, err := shell.ExecCmd("touch "+shell.QuoteArg(vmlinuz), true, shell.HostPath, nil); err != nil {
		t.Fatalf("touch vmlinuz: %v", err)
	}
}

// TestRegenerateGrub_RealMountedBaseline exercises the cmdline apply against a real
// mounted ext4 root: it builds a RAW GPT image, mounts the layout, seeds a GRUB2
// baseline with an "added" kernel, and runs RegenerateGrub with a kernelCmdline
// override. It asserts the on-disk /etc/default/grub GRUB_CMDLINE_LINUX line was
// full-replaced. This bare (non-bootstrapped) root ships no GRUB generator, so
// RegenerateGrub returns a hard error (a new kernel + overrides mean there IS work
// to do); the defaults-file apply runs BEFORE the generator probe, so the on-disk
// rewrite this test primarily asserts still happens and is verified after the error.
//
// It requires root plus loop-device/partition/mkfs tooling and skips otherwise, so
// the suite stays green on unprivileged dev machines and CI sandboxes.
func TestRegenerateGrub_RealMountedBaseline(t *testing.T) {
	requireMountTooling(t, "losetup", "lsblk", "sgdisk", "mkfs", "mkfs.ext4", "mount", "umount", "findmnt")

	// Build a RAW GPT image with a single ext4 root partition.
	imgDir := t.TempDir()
	img := filepath.Join(imgDir, "baseline.raw")
	if _, err := shell.ExecCmd("dd if=/dev/zero of="+shell.QuoteArg(img)+" bs=1M count=256", false, shell.HostPath, nil); err != nil {
		t.Fatalf("create image: %v", err)
	}
	if _, err := shell.ExecCmd("sgdisk -n 1:1MiB:0 -t 1:8300 -c 1:root "+shell.QuoteArg(img), false, shell.HostPath, nil); err != nil {
		t.Fatalf("partition: %v", err)
	}

	loop := imagedisc.NewLoopDev()
	loopDev, parts, unregister, err := loop.AttachImageToLoopDev(img)
	if err != nil {
		t.Fatalf("attach loop: %v", err)
	}
	defer func() {
		if derr := loop.LoopSetupDelete(loopDev); derr != nil {
			t.Logf("detach cleanup: %v", derr)
			return
		}
		unregister()
	}()
	if len(parts) < 1 {
		t.Fatalf("expected a root partition, got %v", parts)
	}
	if _, err := shell.ExecCmd("mkfs -t ext4 -F "+shell.QuoteArg(parts[0]), true, shell.HostPath, nil); err != nil {
		t.Fatalf("mkfs -t ext4: %v", err)
	}

	const newKernel = "6.8.0-99-generic"
	insp := NewInspector(filepath.Join(t.TempDir(), "overlay"))
	err = insp.WithMountedLayout(loopDev, func(l *Layout) error {
		seedGrubBaseline(t, l.RootMount, newKernel)

		// The baseline has no kernel pre-install (empty set); the seeded vmlinuz is
		// the "added" kernel, and both the cmdline and GRUB_DEFAULT overrides are set.
		info := &BaselineInfo{Bootloader: "grub2", PackageManager: PackageManagerAPT, Kernels: nil}
		const wantCmdline = "console=ttyS0,115200n8 i915.force_probe=*"
		const wantDefault = "Advanced options for Ubuntu>Ubuntu, with Linux " + newKernel
		tmpl := grubTemplateFull(wantCmdline, wantDefault)

		// This bare root ships no GRUB generator, and a new kernel + overrides mean
		// there IS work to do, so RegenerateGrub hard-errors. The defaults-file apply
		// runs before the generator probe, so the on-disk rewrite (asserted below) still
		// takes effect; we require the error rather than tolerate a silent stale-config.
		rerr := RegenerateGrub(tmpl, info, l.RootMount)
		if rerr == nil || !strings.Contains(rerr.Error(), "no GRUB config generator") {
			t.Fatalf("bare root with work to do must hard-error on the missing generator, got %v", rerr)
		}

		// The on-disk defaults file must show the full-line replacements.
		out, rerr := shell.ExecCmd("cat "+shell.QuoteArg(filepath.Join(l.RootMount, grubDefaultsRelPath)), true, shell.HostPath, nil)
		if rerr != nil {
			t.Fatalf("read grub defaults: %v", rerr)
		}
		if !strings.Contains(out, "GRUB_CMDLINE_LINUX=\""+wantCmdline+"\"") {
			t.Errorf("GRUB_CMDLINE_LINUX not full-replaced on disk:\n%s", out)
		}
		if strings.Contains(out, "quiet splash") {
			t.Errorf("stale GRUB_CMDLINE_LINUX value survived:\n%s", out)
		}
		if !strings.Contains(out, "GRUB_CMDLINE_LINUX_DEFAULT=\"\"") {
			t.Errorf("GRUB_CMDLINE_LINUX_DEFAULT must be preserved:\n%s", out)
		}
		if !strings.Contains(out, "GRUB_DEFAULT=\""+wantDefault+"\"") {
			t.Errorf("GRUB_DEFAULT not pinned on disk:\n%s", out)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithMountedLayout: %v", err)
	}
}

// TestRegenerateGrub_RealGrubMkconfig runs the full GRUB2 regeneration end-to-end:
// it bootstraps a minimal Debian-family rootfs that ships grub-mkconfig, seeds an
// added kernel, and confirms RegenerateGrub both rewrites the cmdline and produces
// a /boot/grub/grub.cfg. It is the heaviest form of AC #10 and needs network for
// the bootstrap, so it skips when mmdebstrap or the generator is unavailable.
//
// Note: a full real `linux-image-*` package install (postinst-driven initrd +
// menu entry) is even heavier and left as a CI-gated follow-up; here the added
// kernel is simulated on the filesystem so the GRUB stage's own behavior is what
// is under test.
func TestRegenerateGrub_RealGrubMkconfig(t *testing.T) {
	requireMountTooling(t, "losetup", "lsblk", "sgdisk", "mkfs", "mkfs.ext4", "mount", "umount", "findmnt", "mmdebstrap", "chroot")

	imgDir := t.TempDir()
	img := filepath.Join(imgDir, "baseline.raw")
	if _, err := shell.ExecCmd("dd if=/dev/zero of="+shell.QuoteArg(img)+" bs=1M count=1024", false, shell.HostPath, nil); err != nil {
		t.Fatalf("create image: %v", err)
	}
	if _, err := shell.ExecCmd("sgdisk -n 1:1MiB:0 -t 1:8300 -c 1:root "+shell.QuoteArg(img), false, shell.HostPath, nil); err != nil {
		t.Fatalf("partition: %v", err)
	}

	loop := imagedisc.NewLoopDev()
	loopDev, parts, unregister, err := loop.AttachImageToLoopDev(img)
	if err != nil {
		t.Fatalf("attach loop: %v", err)
	}
	defer func() {
		if derr := loop.LoopSetupDelete(loopDev); derr != nil {
			t.Logf("detach cleanup: %v", derr)
			return
		}
		unregister()
	}()
	if len(parts) < 1 {
		t.Fatalf("expected a root partition, got %v", parts)
	}
	if _, err := shell.ExecCmd("mkfs -t ext4 -F "+shell.QuoteArg(parts[0]), true, shell.HostPath, nil); err != nil {
		t.Fatalf("mkfs -t ext4: %v", err)
	}

	const newKernel = "6.8.0-99-generic"
	insp := NewInspector(filepath.Join(t.TempDir(), "overlay"))
	err = insp.WithMountedLayout(loopDev, func(l *Layout) error {
		// grub-common provides grub-mkconfig; without --variant the bootstrap is
		// minimal but includes a working shell + coreutils grub-mkconfig relies on.
		bootstrap := "mmdebstrap --variant=essential --include=grub-common stable " + shell.QuoteArg(l.RootMount)
		if _, berr := shell.ExecCmdWithStream(bootstrap, true, shell.HostPath, nil); berr != nil {
			t.Skipf("mmdebstrap bootstrap unavailable in this environment (needs network): %v", berr)
		}

		// Seed an added kernel and a defaults file on top of the bootstrapped root.
		seedGrubBaseline(t, l.RootMount, newKernel)

		if ok, _ := shell.IsCommandExist("grub-mkconfig", l.RootMount); !ok {
			t.Skip("grub-mkconfig not present in bootstrapped root")
		}

		info := &BaselineInfo{Bootloader: "grub2", PackageManager: PackageManagerAPT, Kernels: nil}
		const wantCmdline = "console=ttyS0,115200n8"
		if rerr := RegenerateGrub(grubTemplate(wantCmdline), info, l.RootMount); rerr != nil {
			t.Fatalf("RegenerateGrub: %v", rerr)
		}

		// grub.cfg must now exist on the writable root (never the ESP).
		grubCfg := filepath.Join(l.RootMount, "boot", "grub", "grub.cfg")
		if _, serr := os.Stat(grubCfg); serr != nil {
			t.Errorf("expected regenerated grub.cfg at %s: %v", grubCfg, serr)
		}
		out, rerr := shell.ExecCmd("cat "+shell.QuoteArg(filepath.Join(l.RootMount, grubDefaultsRelPath)), true, shell.HostPath, nil)
		if rerr != nil {
			t.Fatalf("read grub defaults: %v", rerr)
		}
		if !strings.Contains(out, "GRUB_CMDLINE_LINUX=\""+wantCmdline+"\"") {
			t.Errorf("GRUB_CMDLINE_LINUX not full-replaced on disk:\n%s", out)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithMountedLayout: %v", err)
	}
}
