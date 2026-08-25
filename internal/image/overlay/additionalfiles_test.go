package overlay

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

// overlayAddFilesTemplate builds a template whose additionalFiles entries all use
// absolute Local paths (so GetAdditionalFileInfo keeps them without needing a
// template search-path context) pointing at real files under a temp dir.
func overlayAddFilesTemplate(t *testing.T, entries ...config.AdditionalFileInfo) *config.ImageTemplate {
	t.Helper()
	return &config.ImageTemplate{
		SystemConfig: config.SystemConfig{AdditionalFiles: entries},
	}
}

// writeTempSource creates a real file so GetAdditionalFileInfo's os.Stat gate keeps
// the entry, and returns its absolute path.
func writeTempSource(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("content"), 0o644); err != nil {
		t.Fatalf("writing temp source %s: %v", p, err)
	}
	return p
}

func TestRunOverlayAdditionalFiles_NoFilesIsNoOp(t *testing.T) {
	// No additionalFiles: must return nil without invoking the copy seam, so a build
	// that does not use the feature pays nothing. A panic in the (unset here) copy
	// seam would fail this test.
	orig := copyAdditionalFileFn
	defer func() { copyAdditionalFileFn = orig }()
	copyAdditionalFileFn = func(string, string, string, bool) error {
		t.Fatal("copy seam must not be called when there are no additional files")
		return nil
	}
	if err := RunOverlayAdditionalFiles(overlayAddFilesTemplate(t), "/wd/mnt/root", config.AdditionalFileStageDefault); err != nil {
		t.Fatalf("expected no-op for empty additionalFiles, got %v", err)
	}
}

func TestRunOverlayAdditionalFiles_Validation(t *testing.T) {
	if err := RunOverlayAdditionalFiles(nil, "/wd/mnt/root", config.AdditionalFileStageDefault); err == nil {
		t.Error("expected error for nil template")
	}
	src := writeTempSource(t, "f")
	tmpl := overlayAddFilesTemplate(t, config.AdditionalFileInfo{Local: src, Final: "/etc/f"})
	if err := RunOverlayAdditionalFiles(tmpl, "   ", config.AdditionalFileStageDefault); err == nil {
		t.Error("expected error for empty root mount")
	}
}

func TestRunOverlayAdditionalFiles_CopiesEachEntryUnderMount(t *testing.T) {
	orig := copyAdditionalFileFn
	defer func() { copyAdditionalFileFn = orig }()

	type copyCall struct{ src, dst, flags string }
	var calls []copyCall
	copyAdditionalFileFn = func(src, dst, flags string, sudo bool) error {
		if !sudo {
			t.Errorf("expected sudo copy, got sudo=false for %s", src)
		}
		calls = append(calls, copyCall{src, dst, flags})
		return nil
	}

	initrd := writeTempSource(t, "initrd.img")
	conf := writeTempSource(t, "app.conf")
	tmpl := overlayAddFilesTemplate(t,
		config.AdditionalFileInfo{Local: initrd, Final: "/boot/initrd.img-custom"},
		config.AdditionalFileInfo{Local: conf, Final: "/etc/app/app.conf"},
	)

	if err := RunOverlayAdditionalFiles(tmpl, "/wd/mnt/root", config.AdditionalFileStageDefault); err != nil {
		t.Fatalf("RunOverlayAdditionalFiles: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 copies, got %d: %+v", len(calls), calls)
	}
	// Final path must be joined onto the mount, and -p preserves metadata like create mode.
	wantDst := []string{"/wd/mnt/root/boot/initrd.img-custom", "/wd/mnt/root/etc/app/app.conf"}
	for i, c := range calls {
		if c.dst != wantDst[i] {
			t.Errorf("copy[%d] dst = %q, want %q", i, c.dst, wantDst[i])
		}
		if c.flags != "-p" {
			t.Errorf("copy[%d] flags = %q, want -p", i, c.flags)
		}
	}
}

// TestRunOverlayAdditionalFiles_FiltersByStage confirms each pass copies ONLY the
// entries whose Stage matches the requested stage: the default pass copies unmarked
// (and only unmarked) entries, and the pre-initramfs pass copies only
// stage: pre-initramfs entries. This is what lets a dracut module land before the
// initramfs is rebuilt while ordinary files still land at the end.
func TestRunOverlayAdditionalFiles_FiltersByStage(t *testing.T) {
	orig := copyAdditionalFileFn
	defer func() { copyAdditionalFileFn = orig }()

	var copied []string
	copyAdditionalFileFn = func(src, dst, flags string, sudo bool) error {
		copied = append(copied, dst)
		return nil
	}

	dracutMod := writeTempSource(t, "99-custom.conf")
	appConf := writeTempSource(t, "app.conf")
	tmpl := overlayAddFilesTemplate(t,
		config.AdditionalFileInfo{Local: dracutMod, Final: "/usr/lib/dracut/modules.d/99custom/module-setup.sh", Stage: config.AdditionalFileStagePreInitramfs},
		config.AdditionalFileInfo{Local: appConf, Final: "/etc/app/app.conf"}, // default stage (unmarked)
	)

	// Pre-initramfs pass copies only the dracut module.
	copied = nil
	if err := RunOverlayAdditionalFiles(tmpl, "/wd/mnt/root", config.AdditionalFileStagePreInitramfs); err != nil {
		t.Fatalf("pre-initramfs pass: %v", err)
	}
	if len(copied) != 1 || copied[0] != "/wd/mnt/root/usr/lib/dracut/modules.d/99custom/module-setup.sh" {
		t.Errorf("pre-initramfs pass copied %v, want only the dracut module", copied)
	}

	// Default pass copies only the unmarked entry.
	copied = nil
	if err := RunOverlayAdditionalFiles(tmpl, "/wd/mnt/root", config.AdditionalFileStageDefault); err != nil {
		t.Fatalf("default pass: %v", err)
	}
	if len(copied) != 1 || copied[0] != "/wd/mnt/root/etc/app/app.conf" {
		t.Errorf("default pass copied %v, want only the unmarked app.conf", copied)
	}
}

func TestRunOverlayAdditionalFiles_CopyErrorFailsBuild(t *testing.T) {
	orig := copyAdditionalFileFn
	defer func() { copyAdditionalFileFn = orig }()
	copyAdditionalFileFn = func(string, string, string, bool) error {
		return errors.New("disk full")
	}
	src := writeTempSource(t, "f")
	tmpl := overlayAddFilesTemplate(t, config.AdditionalFileInfo{Local: src, Final: "/etc/f"})
	if err := RunOverlayAdditionalFiles(tmpl, "/wd/mnt/root", config.AdditionalFileStageDefault); err == nil {
		t.Fatal("expected error when a copy fails, got nil")
	}
}

// TestRunOverlayAdditionalFiles_RejectsTraversalAndSymlink guards the untrusted-
// destination hardening: a `final` that escapes the mount via `..`, and a symlinked
// destination ancestor inside the mount, must both be rejected before any copy runs.
func TestRunOverlayAdditionalFiles_RejectsTraversalAndSymlink(t *testing.T) {
	orig := copyAdditionalFileFn
	defer func() { copyAdditionalFileFn = orig }()
	copied := false
	copyAdditionalFileFn = func(string, string, string, bool) error { copied = true; return nil }

	src := writeTempSource(t, "payload")

	t.Run("dot-dot traversal escapes the mount", func(t *testing.T) {
		copied = false
		tmpl := overlayAddFilesTemplate(t, config.AdditionalFileInfo{Local: src, Final: "../../etc/passwd"})
		err := RunOverlayAdditionalFiles(tmpl, "/wd/mnt/root", config.AdditionalFileStageDefault)
		if err == nil {
			t.Fatal("expected a traversal escape to be rejected")
		}
		if copied {
			t.Error("no copy may run when the destination escapes the mount")
		}
	})

	t.Run("symlinked destination ancestor is rejected", func(t *testing.T) {
		copied = false
		// Real mount whose /etc is a symlink to a host dir.
		root := t.TempDir()
		hostDir := t.TempDir()
		if err := os.Symlink(hostDir, filepath.Join(root, "etc")); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		tmpl := overlayAddFilesTemplate(t, config.AdditionalFileInfo{Local: src, Final: "/etc/app.conf"})
		err := RunOverlayAdditionalFiles(tmpl, root, config.AdditionalFileStageDefault)
		if err == nil {
			t.Fatal("expected a symlinked destination ancestor to be rejected")
		}
		if copied {
			t.Error("no copy may run when the destination chain contains a symlink")
		}
	})
}
