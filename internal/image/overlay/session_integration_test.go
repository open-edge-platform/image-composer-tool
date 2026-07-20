package overlay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/image/imagedisc"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

// TestOverlayBuilder_FullFlowRealRawBaseline exercises the entire overlay build
// lifecycle end-to-end through the Builder, from an overlay-mode template to a
// final RAW artifact: it builds a real GPT/ext4 RAW image, bootstraps a minimal
// Debian rootfs into it (which provides dpkg), then drives Preprocess → Build →
// Postprocess. Package resolution is stubbed to a locally-built .deb so the test
// needs no network for resolution; every other stage (mount, detect, install,
// boot-regen, resize, SBOM, emit, cleanup) runs for real against the baseline.
//
// It asserts the package landed in the baseline, the SBOM was embedded, the RAW
// artifact was emitted under the image build dir named by version, and the loop
// device / mounts were released. It requires root plus loop/partition/mkfs/
// mmdebstrap/dpkg tooling (and network for the bootstrap) and skips otherwise.
func TestOverlayBuilder_FullFlowRealRawBaseline(t *testing.T) {
	// The skip guard lists the binaries this test actually shells out to, so a host
	// missing any of them skips rather than fails: it formats via the "mkfs" front-end
	// (bootstrapDebianBaseline), builds the probe via "dpkg --build" (buildProbeDeb),
	// and asserts the mount released with "findmnt". mkfs.ext4/dpkg-deb are NOT listed:
	// they are not invoked directly (their "mkfs"/"dpkg" front-ends are) and are not on
	// the shell allowlist.
	requireMountTooling(t, "losetup", "lsblk", "sgdisk", "mkfs", "mount", "umount", "mmdebstrap", "dpkg", "findmnt", "chroot")

	// 1. Build the approved overlay artifact (a trivial, dependency-free .deb).
	cacheDir := t.TempDir()
	buildProbeDeb(t, cacheDir)

	// 2. Build a real RAW GPT image with a single ext4 root partition and bootstrap
	//    a minimal Debian baseline into it (provides the dpkg the install drives).
	imgDir := t.TempDir()
	srcImg := filepath.Join(imgDir, "source-baseline.raw")
	if _, err := shell.ExecCmd("dd if=/dev/zero of="+shell.QuoteArg(srcImg)+" bs=1M count=1024", false, shell.HostPath, nil); err != nil {
		t.Fatalf("create image: %v", err)
	}
	if _, err := shell.ExecCmd("sgdisk -n 1:1MiB:0 -t 1:8300 -c 1:root "+shell.QuoteArg(srcImg), false, shell.HostPath, nil); err != nil {
		t.Fatalf("partition: %v", err)
	}
	bootstrapDebianBaseline(t, srcImg)

	// 3. Point the global work/cache/temp dirs at the test sandbox so the emitted
	//    artifact and SBOM land somewhere we can assert on (and clean up).
	workDir := t.TempDir()
	restoreGlobal := withTestGlobalDirs(t, workDir, cacheDir)
	defer restoreGlobal()

	tmpl := &config.ImageTemplate{
		Image:  config.ImageInfo{Name: "overlaytest", Version: "1.0"},
		Target: config.TargetInfo{OS: "debian", Dist: "debian12", Arch: "amd64", ImageType: "raw"},
		Baseline: &config.Baseline{
			Mode:   config.BaselineModeOverlay,
			Source: &config.BaselineSource{Path: srcImg, Format: config.BaselineFormatRaw},
		},
		OverlayPolicy: &config.OverlayPolicy{PackageOperation: config.OverlayPackageOpAdditiveOnly},
		SystemConfig:  config.SystemConfig{Name: "default", Packages: []string{"overlay-probe"}},
	}

	// 4. Stub resolution (no network): hand the Builder the local artifact directly.
	defer saveBuilderSeams().restore()
	builderResolveFn = func(_ *config.ImageTemplate, _ *BaselineInfo, _ []BaselinePackage) (*ResolutionPlan, error) {
		return &ResolutionPlan{
			Requested:   []string{"overlay-probe"},
			DownloadDir: cacheDir,
			ToInstall: []ResolvedPackage{
				{Name: "overlay-probe", Version: "1.0", Arch: "all", URL: "file://" + filepath.Join(cacheDir, "overlay-probe_1.0_all.deb")},
			},
		}, nil
	}

	builder, err := NewBuilder(tmpl)
	if err != nil {
		t.Fatalf("NewBuilder: %v", err)
	}

	// 5. Drive the three phases exactly as the provider pipeline does.
	if err := builder.Preprocess(); err != nil {
		t.Fatalf("Preprocess: %v", err)
	}
	rootMount := builder.layout.RootMount

	if err := builder.Build(); err != nil {
		// Ensure cleanup runs even if a later assertion path is skipped.
		_ = builder.Postprocess(err)
		t.Fatalf("Build: %v", err)
	}

	// The package marker must exist on the baseline root while it is still mounted.
	marker := filepath.Join(rootMount, "usr", "share", "overlay-probe", "installed")
	if _, serr := os.Stat(marker); serr != nil {
		t.Errorf("expected installed marker at %s: %v", marker, serr)
	}

	if err := builder.Postprocess(nil); err != nil {
		t.Fatalf("Postprocess: %v", err)
	}

	// 6. The final artifact and its SBOM sidecar must exist under the build dir.
	finalImg := filepath.Join(workDir, "debian-debian12-amd64", "imagebuild", "default", "overlaytest-1.0.raw")
	if _, serr := os.Stat(finalImg); serr != nil {
		t.Errorf("expected emitted artifact at %s: %v", finalImg, serr)
	}
	sbom := filepath.Join(filepath.Dir(finalImg), "spdx_manifest.json")
	if _, serr := os.Stat(sbom); serr != nil {
		t.Errorf("expected SBOM sidecar at %s: %v", sbom, serr)
	}

	// 7. The loop device and root mount must have been released by Postprocess.
	if builder.ctx.LoopDevPath != "" {
		t.Errorf("loop device not released: %q", builder.ctx.LoopDevPath)
	}
	// findmnt exits non-zero when the target is not mounted, which is the success
	// case here. Treat a non-empty match as the failure and surface the execution
	// error only when findmnt produced output we can't trust (matching the sibling
	// overlay integration tests) so a missing/denied findmnt cannot pass silently.
	out, ferr := shell.ExecCmd("findmnt -n "+shell.QuoteArg(rootMount), true, shell.HostPath, nil)
	if trimmed := strings.TrimSpace(out); trimmed != "" {
		t.Errorf("root mount %s still mounted after postprocess: %q (findmnt err: %v)", rootMount, trimmed, ferr)
	}
}

// bootstrapDebianBaseline formats img's root partition ext4 and bootstraps a
// minimal Debian rootfs into it via mmdebstrap, then releases the mount and loop.
// It skips the test when the network-dependent bootstrap is unavailable.
func bootstrapDebianBaseline(t *testing.T, img string) {
	t.Helper()
	loop := imagedisc.NewLoopDev()
	loopDev, parts, err := loop.AttachImageToLoopDev(img)
	if err != nil {
		t.Fatalf("attach loop: %v", err)
	}
	defer func() {
		if derr := loop.LoopSetupDelete(loopDev); derr != nil {
			t.Logf("bootstrap detach cleanup: %v", derr)
		}
	}()
	if len(parts) < 1 {
		t.Fatalf("expected a root partition, got %v", parts)
	}
	// Format via "mkfs -t ext4" so the command token stays the allowlisted "mkfs"
	// (mkfs.ext4 is not on the shell allowlist), matching the sibling overlay tests.
	if _, err := shell.ExecCmd("mkfs -t ext4 -F "+shell.QuoteArg(parts[0]), true, shell.HostPath, nil); err != nil {
		t.Fatalf("mkfs -t ext4: %v", err)
	}

	insp := NewInspector(filepath.Join(t.TempDir(), "bootstrap"))
	err = insp.WithMountedLayout(loopDev, func(l *Layout) error {
		bootstrap := "mmdebstrap --variant=essential bookworm " + l.RootMount
		if _, berr := shell.ExecCmdWithStream(bootstrap, true, shell.HostPath, nil); berr != nil {
			t.Skipf("mmdebstrap bootstrap unavailable in this environment (needs network): %v", berr)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("bootstrap WithMountedLayout: %v", err)
	}
}

// withTestGlobalDirs points the global work/cache/temp directories at the test
// sandbox and returns a restore function. The temp dir (where the SBOM is staged)
// is nested under workDir so it is cleaned up with the rest of the sandbox.
func withTestGlobalDirs(t *testing.T, workDir, cacheDir string) func() {
	t.Helper()
	prev := config.Global()
	cfg := *prev
	cfg.WorkDir = workDir
	cfg.CacheDir = cacheDir
	cfg.TempDir = filepath.Join(workDir, "tmp")
	config.SetGlobal(&cfg)
	return func() { config.SetGlobal(prev) }
}
