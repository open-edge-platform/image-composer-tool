package overlay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/image/imagedisc"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

// buildProbeDeb builds a trivial, dependency-free .deb ("overlay-probe") into
// dir with dpkg-deb and returns its path. The package ships a single marker file
// so the install can be verified both via dpkg-query and on the filesystem.
func buildProbeDeb(t *testing.T, dir string) string {
	t.Helper()
	pkgRoot := filepath.Join(dir, "overlay-probe-pkg")
	debianDir := filepath.Join(pkgRoot, "DEBIAN")
	markerDir := filepath.Join(pkgRoot, "usr", "share", "overlay-probe")
	for _, d := range []string{debianDir, markerDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	control := "Package: overlay-probe\n" +
		"Version: 1.0\n" +
		"Architecture: all\n" +
		"Maintainer: overlay-test <test@example.com>\n" +
		"Description: overlay end-to-end install probe\n"
	if err := os.WriteFile(filepath.Join(debianDir, "control"), []byte(control), 0o644); err != nil {
		t.Fatalf("write control: %v", err)
	}
	if err := os.WriteFile(filepath.Join(markerDir, "installed"), []byte("ok\n"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	// Build with "dpkg --build" (not "dpkg-deb --build"): dpkg is on the shell
	// allowlist and dpkg-deb is not, and `dpkg --build` is an equivalent front-end
	// for the same operation.
	debPath := filepath.Join(dir, "overlay-probe_1.0_all.deb")
	if _, err := shell.ExecCmd("dpkg --build "+shell.QuoteArg(pkgRoot)+" "+shell.QuoteArg(debPath), false, shell.HostPath, nil); err != nil {
		t.Fatalf("dpkg --build: %v", err)
	}
	return debPath
}

// TestInstallOverlayPackages_RealRawBaseline is the end-to-end install test: it
// builds a real RAW GPT/ext4 baseline, bootstraps a minimal Debian-family rootfs
// into it (which provides dpkg), then extends that baseline with a locally-built
// .deb through the full InstallOverlayPackages path and confirms the package is
// installed in the chroot's package database and present on the root filesystem.
//
// It requires root plus loop-device, partition, mkfs, mmdebstrap and dpkg tooling
// (and network for the bootstrap), and skips otherwise so the suite stays green
// on unprivileged dev machines and CI sandboxes.
func TestInstallOverlayPackages_RealRawBaseline(t *testing.T) {
	// The skip guard must list the binaries this test actually shells out to, so
	// a host missing any of them skips rather than fails: it formats via the
	// "mkfs" front-end (which needs mkfs.ext4 underneath), builds the probe via
	// "dpkg --build" (dpkg-deb underneath), and asserts bind-mount cleanup with
	// "findmnt". mkfs.ext4/dpkg-deb are kept to also assert the helpers exist.
	requireMountTooling(t, "losetup", "lsblk", "sgdisk", "mkfs", "mkfs.ext4", "mount", "umount", "mmdebstrap", "dpkg", "dpkg-deb", "findmnt", "chroot")

	// 1. Build a locally-prepared .deb artifact — the approved package to add.
	cacheDir := t.TempDir()
	buildProbeDeb(t, cacheDir)

	// 2. Build and attach a real RAW GPT image with a single ext4 root partition.
	imgDir := t.TempDir()
	img := filepath.Join(imgDir, "baseline.raw")
	if _, err := shell.ExecCmd("dd if=/dev/zero of="+shell.QuoteArg(img)+" bs=1M count=1024", false, shell.HostPath, nil); err != nil {
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
		t.Fatalf("expected a root partition, got %v", parts)
	}
	// Format via "mkfs -t ext4" so the command token stays the allowlisted "mkfs"
	// (mkfs.ext4 is not on the shell allowlist), matching the sibling layout test.
	if _, err := shell.ExecCmd("mkfs -t ext4 -F "+shell.QuoteArg(parts[0]), true, shell.HostPath, nil); err != nil {
		t.Fatalf("mkfs -t ext4: %v", err)
	}

	// 3. Mount the layout and run the bootstrap + overlay install under it.
	insp := NewInspector(filepath.Join(t.TempDir(), "overlay"))
	err = insp.WithMountedLayout(loopDev, func(l *Layout) error {
		// Bootstrap a minimal Debian-family baseline into the mounted root. This
		// provides a working dpkg, the package manager the install backend drives.
		bootstrap := "mmdebstrap --variant=essential --include=ca-certificates stable " + shell.QuoteArg(l.RootMount)
		if _, berr := shell.ExecCmdWithStream(bootstrap, true, shell.HostPath, nil); berr != nil {
			t.Skipf("mmdebstrap bootstrap unavailable in this environment (needs network): %v", berr)
		}

		info := &BaselineInfo{OS: "debian", Arch: "amd64", PackageManager: PackageManagerAPT, PackageType: pkgTypeDeb}
		plan := &ResolutionPlan{
			Requested:   []string{"overlay-probe"},
			DownloadDir: cacheDir,
			ToInstall: []ResolvedPackage{
				{Name: "overlay-probe", Version: "1.0", Arch: "all", URL: "file://" + filepath.Join(cacheDir, "overlay-probe_1.0_all.deb")},
			},
		}

		result, ierr := InstallOverlayPackages(info, l.RootMount, plan, passedReport())
		if ierr != nil {
			t.Fatalf("InstallOverlayPackages: %v", ierr)
		}
		if len(result.Installed) != 1 || result.Installed[0] != "overlay-probe" {
			t.Errorf("installed = %v, want [overlay-probe]", result.Installed)
		}

		// The package's marker file must now exist on the baseline root.
		marker := filepath.Join(l.RootMount, "usr", "share", "overlay-probe", "installed")
		if _, serr := os.Stat(marker); serr != nil {
			t.Errorf("expected installed marker at %s: %v", marker, serr)
		}

		// dpkg inside the chroot must report it installed.
		out, qerr := shell.ExecCmd("dpkg -s overlay-probe", true, l.RootMount, nil)
		if qerr != nil || !strings.Contains(out, "install ok installed") {
			t.Errorf("dpkg -s status = %q (err %v), want 'install ok installed'", out, qerr)
		}

		// The artifact bind mount must have been cleaned up by InstallOverlayPackages.
		artifactMount := filepath.Join(l.RootMount, "run", "overlay-pkgs")
		if mout, _ := shell.ExecCmd("findmnt -n "+shell.QuoteArg(artifactMount), true, shell.HostPath, nil); strings.TrimSpace(mout) != "" {
			t.Errorf("artifact mount %s still mounted after install: %q", artifactMount, mout)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithMountedLayout: %v", err)
	}
}
