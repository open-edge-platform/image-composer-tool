package cache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSetupIsolated verifies that SetupIsolated creates fresh unique cache/workspace
// directories adjacent to the provided ones and that cleanup removes them again.
func TestSetupIsolated(t *testing.T) {
	tmp := t.TempDir()
	origCacheDir := filepath.Join(tmp, "cache")
	origWorkDir := filepath.Join(tmp, "workspace")

	iso, cleanup, err := SetupIsolated(origCacheDir, origWorkDir)
	if err != nil {
		t.Fatalf("SetupIsolated failed: %v", err)
	}

	// Unique dirs should have been created.
	if _, err := os.Stat(iso.CacheDir); err != nil {
		t.Errorf("unique cache dir should exist: %v", err)
	}
	if _, err := os.Stat(iso.WorkDir); err != nil {
		t.Errorf("unique work dir should exist: %v", err)
	}

	// They must differ from the provided dirs but share the same parent.
	if iso.CacheDir == origCacheDir {
		t.Error("unique cache dir should differ from configured cache dir")
	}
	if iso.WorkDir == origWorkDir {
		t.Error("unique work dir should differ from configured work dir")
	}
	if got, want := filepath.Dir(iso.CacheDir), filepath.Dir(origCacheDir); got != want {
		t.Errorf("unique cache dir parent = %q, want %q", got, want)
	}
	if got, want := filepath.Dir(iso.WorkDir), filepath.Dir(origWorkDir); got != want {
		t.Errorf("unique work dir parent = %q, want %q", got, want)
	}

	// origWorkDir must capture the provided configured workspace (copy-out target).
	if iso.origWorkDir != origWorkDir {
		t.Errorf("origWorkDir = %q, want %q", iso.origWorkDir, origWorkDir)
	}

	// Cleanup should remove both unique dirs.
	cleanup()
	if _, err := os.Stat(iso.CacheDir); !os.IsNotExist(err) {
		t.Errorf("unique cache dir should be removed after cleanup, stat err: %v", err)
	}
	if _, err := os.Stat(iso.WorkDir); !os.IsNotExist(err) {
		t.Errorf("unique work dir should be removed after cleanup, stat err: %v", err)
	}
}

// TestSetupIsolated_CleanupKeepsWorkspaceOnFlag verifies that after KeepWorkspace is
// called (e.g. output copy-out failed), cleanup removes the cache but preserves the
// workspace.
func TestSetupIsolated_CleanupKeepsWorkspaceOnFlag(t *testing.T) {
	tmp := t.TempDir()
	iso, cleanup, err := SetupIsolated(filepath.Join(tmp, "cache"), filepath.Join(tmp, "workspace"))
	if err != nil {
		t.Fatalf("SetupIsolated failed: %v", err)
	}

	iso.KeepWorkspace()
	cleanup()

	if _, err := os.Stat(iso.CacheDir); !os.IsNotExist(err) {
		t.Errorf("cache dir should be removed even when workspace is kept")
	}
	if _, err := os.Stat(iso.WorkDir); err != nil {
		t.Errorf("workspace should be preserved when KeepWorkspace was called: %v", err)
	}
}

// TestSetupIsolated_ErrorWhenCacheParentMissing verifies SetupIsolated surfaces an
// error when the unique cache directory cannot be created.
func TestSetupIsolated_ErrorWhenCacheParentMissing(t *testing.T) {
	origCacheDir := filepath.Join(t.TempDir(), "does-not-exist", "cache")
	origWorkDir := filepath.Join(t.TempDir(), "workspace")
	if _, _, err := SetupIsolated(origCacheDir, origWorkDir); err == nil {
		t.Fatal("expected SetupIsolated to fail when the cache parent directory is missing")
	}
}

// TestSetupIsolated_ErrorWhenWorkParentMissing verifies that when creating the unique
// workspace fails, the already-created unique cache directory is cleaned up.
func TestSetupIsolated_ErrorWhenWorkParentMissing(t *testing.T) {
	tmp := t.TempDir()
	origCacheDir := filepath.Join(tmp, "cache")                      // parent (tmp) exists
	origWorkDir := filepath.Join(tmp, "missing-parent", "workspace") // parent missing

	if _, _, err := SetupIsolated(origCacheDir, origWorkDir); err == nil {
		t.Fatal("expected SetupIsolated to fail when the workspace parent directory is missing")
	}

	// The unique cache dir created before the failure must have been removed.
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatalf("failed to read temp dir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "ict-nocache-cache-") {
			t.Errorf("leftover unique cache dir was not cleaned up: %s", entry.Name())
		}
	}
}

// TestIsolated_PreserveOutput verifies that the built image directory is copied from the
// isolated workspace back to the originally configured workspace.
func TestIsolated_PreserveOutput(t *testing.T) {
	const providerID = "azure-linux-azl3-x86_64"
	const configName = "edge"

	t.Run("CopiesImageOutput", func(t *testing.T) {
		tmp := t.TempDir()
		uniqueWork := filepath.Join(tmp, "unique-workspace")
		origWork := filepath.Join(tmp, "orig-workspace")

		srcImageDir := filepath.Join(uniqueWork, providerID, "imagebuild", configName)
		if err := os.MkdirAll(srcImageDir, 0o700); err != nil {
			t.Fatalf("failed to create src image dir: %v", err)
		}
		imageContent := []byte("fake image bytes")
		if err := os.WriteFile(filepath.Join(srcImageDir, "image.raw"), imageContent, 0o644); err != nil {
			t.Fatalf("failed to write fake image: %v", err)
		}

		iso := &Isolated{WorkDir: uniqueWork, origWorkDir: origWork}
		if err := iso.PreserveOutput(providerID, configName); err != nil {
			t.Fatalf("PreserveOutput failed: %v", err)
		}

		dstImage := filepath.Join(origWork, providerID, "imagebuild", configName, "image.raw")
		got, err := os.ReadFile(dstImage)
		if err != nil {
			t.Fatalf("expected copied image at %s: %v", dstImage, err)
		}
		if string(got) != string(imageContent) {
			t.Errorf("copied image content mismatch: got %q, want %q", got, imageContent)
		}

		// The preserved output directory tree should be created with 0700 permissions
		// (matching the image build directory) rather than CopyDir's mkdir -p default.
		imageBuildDir := filepath.Join(origWork, providerID, "imagebuild")
		info, err := os.Stat(imageBuildDir)
		if err != nil {
			t.Fatalf("stat preserved image build dir: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o700 {
			t.Errorf("preserved image build dir perm = %o, want 0700", perm)
		}
	})

	t.Run("NoOutputDirIsNoOp", func(t *testing.T) {
		tmp := t.TempDir()
		iso := &Isolated{
			WorkDir:     filepath.Join(tmp, "unique-workspace"),
			origWorkDir: filepath.Join(tmp, "orig-workspace"),
		}
		// No imagebuild dir was produced -> should be a no-op, not an error.
		if err := iso.PreserveOutput(providerID, configName); err != nil {
			t.Errorf("expected no error when image dir is missing, got: %v", err)
		}
		dst := filepath.Join(iso.origWorkDir, providerID, "imagebuild", configName)
		if _, err := os.Stat(dst); !os.IsNotExist(err) {
			t.Errorf("destination should not exist when there is no output, stat err: %v", err)
		}
	})
}
