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
	tempDir := t.TempDir()
	originalCacheDir := filepath.Join(tempDir, "cache")
	originalWorkDir := filepath.Join(tempDir, "workspace")

	isolated, cleanup, err := SetupIsolated(originalCacheDir, originalWorkDir)
	if err != nil {
		t.Fatalf("SetupIsolated failed: %v", err)
	}

	// Unique directories should have been created.
	if _, err := os.Stat(isolated.CacheDir); err != nil {
		t.Errorf("unique cache directory should exist: %v", err)
	}
	if _, err := os.Stat(isolated.WorkDir); err != nil {
		t.Errorf("unique work directory should exist: %v", err)
	}

	// They must differ from the provided directories but share the same parent.
	if isolated.CacheDir == originalCacheDir {
		t.Error("unique cache directory should differ from configured cache directory")
	}
	if isolated.WorkDir == originalWorkDir {
		t.Error("unique work directory should differ from configured work directory")
	}
	if got, want := filepath.Dir(isolated.CacheDir), filepath.Dir(originalCacheDir); got != want {
		t.Errorf("unique cache directory parent = %q, want %q", got, want)
	}
	if got, want := filepath.Dir(isolated.WorkDir), filepath.Dir(originalWorkDir); got != want {
		t.Errorf("unique work directory parent = %q, want %q", got, want)
	}

	// originalWorkDir must capture the provided configured workspace (copy-out target).
	if isolated.originalWorkDir != originalWorkDir {
		t.Errorf("originalWorkDir = %q, want %q", isolated.originalWorkDir, originalWorkDir)
	}

	// Cleanup should remove both unique directories.
	cleanup()
	if _, err := os.Stat(isolated.CacheDir); !os.IsNotExist(err) {
		t.Errorf("unique cache directory should be removed after cleanup, stat err: %v", err)
	}
	if _, err := os.Stat(isolated.WorkDir); !os.IsNotExist(err) {
		t.Errorf("unique work directory should be removed after cleanup, stat err: %v", err)
	}
}

// TestSetupIsolated_CleanupKeepsWorkspaceOnFlag verifies that after KeepWorkspace is
// called (e.g. output copy-out failed), cleanup removes the cache but preserves the
// workspace.
func TestSetupIsolated_CleanupKeepsWorkspaceOnFlag(t *testing.T) {
	tempDir := t.TempDir()
	isolated, cleanup, err := SetupIsolated(filepath.Join(tempDir, "cache"), filepath.Join(tempDir, "workspace"))
	if err != nil {
		t.Fatalf("SetupIsolated failed: %v", err)
	}

	isolated.KeepWorkspace()
	cleanup()

	if _, err := os.Stat(isolated.CacheDir); !os.IsNotExist(err) {
		t.Errorf("cache directory should be removed even when workspace is kept")
	}
	if _, err := os.Stat(isolated.WorkDir); err != nil {
		t.Errorf("workspace should be preserved when KeepWorkspace was called: %v", err)
	}
}

// TestSetupIsolated_ErrorWhenCacheParentMissing verifies SetupIsolated surfaces an
// error when the unique cache directory cannot be created.
func TestSetupIsolated_ErrorWhenCacheParentMissing(t *testing.T) {
	originalCacheDir := filepath.Join(t.TempDir(), "does-not-exist", "cache")
	originalWorkDir := filepath.Join(t.TempDir(), "workspace")
	if _, _, err := SetupIsolated(originalCacheDir, originalWorkDir); err == nil {
		t.Fatal("expected SetupIsolated to fail when the cache parent directory is missing")
	}
}

// TestSetupIsolated_ErrorWhenWorkParentMissing verifies that when creating the unique
// workspace fails, the already-created unique cache directory is cleaned up.
func TestSetupIsolated_ErrorWhenWorkParentMissing(t *testing.T) {
	tempDir := t.TempDir()
	originalCacheDir := filepath.Join(tempDir, "cache")                      // parent (tempDir) exists
	originalWorkDir := filepath.Join(tempDir, "missing-parent", "workspace") // parent missing

	if _, _, err := SetupIsolated(originalCacheDir, originalWorkDir); err == nil {
		t.Fatal("expected SetupIsolated to fail when the workspace parent directory is missing")
	}

	// The unique cache directory created before the failure must have been removed.
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("failed to read temp directory: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "ict-nocache-cache-") {
			t.Errorf("leftover unique cache directory was not cleaned up: %s", entry.Name())
		}
	}
}

// TestIsolated_PreserveOutput verifies that the built image directory is copied from the
// isolated workspace back to the originally configured workspace.
func TestIsolated_PreserveOutput(t *testing.T) {
	const providerID = "azure-linux-azl3-x86_64"
	const configName = "edge"

	t.Run("CopiesImageOutput", func(t *testing.T) {
		tempDir := t.TempDir()
		uniqueWorkspace := filepath.Join(tempDir, "unique-workspace")
		originalWorkspace := filepath.Join(tempDir, "orig-workspace")

		sourceImageDir := filepath.Join(uniqueWorkspace, providerID, "imagebuild", configName)
		if err := os.MkdirAll(sourceImageDir, 0o700); err != nil {
			t.Fatalf("failed to create source image directory: %v", err)
		}
		imageContent := []byte("fake image bytes")
		if err := os.WriteFile(filepath.Join(sourceImageDir, "image.raw"), imageContent, 0o644); err != nil {
			t.Fatalf("failed to write fake image: %v", err)
		}

		isolated := &Isolated{WorkDir: uniqueWorkspace, originalWorkDir: originalWorkspace}
		if err := isolated.PreserveOutput(providerID, configName); err != nil {
			t.Fatalf("PreserveOutput failed: %v", err)
		}

		destinationImagePath := filepath.Join(originalWorkspace, providerID, "imagebuild", configName, "image.raw")
		got, err := os.ReadFile(destinationImagePath)
		if err != nil {
			t.Fatalf("expected copied image at %s: %v", destinationImagePath, err)
		}
		if string(got) != string(imageContent) {
			t.Errorf("copied image content mismatch: got %q, want %q", got, imageContent)
		}

		// The preserved output directory tree should be created with 0700 permissions
		// (matching the image build directory) rather than CopyDir's mkdir -p default.
		imageBuildDir := filepath.Join(originalWorkspace, providerID, "imagebuild")
		fileInfo, err := os.Stat(imageBuildDir)
		if err != nil {
			t.Fatalf("stat preserved image build directory: %v", err)
		}
		if permissions := fileInfo.Mode().Perm(); permissions != 0o700 {
			t.Errorf("preserved image build directory perm = %o, want 0700", permissions)
		}
	})

	t.Run("NoOutputDirIsNoOp", func(t *testing.T) {
		tempDir := t.TempDir()
		isolated := &Isolated{
			WorkDir:         filepath.Join(tempDir, "unique-workspace"),
			originalWorkDir: filepath.Join(tempDir, "orig-workspace"),
		}
		// No imagebuild directory was produced -> should be a no-op, not an error.
		if err := isolated.PreserveOutput(providerID, configName); err != nil {
			t.Errorf("expected no error when image directory is missing, got: %v", err)
		}
		destinationPath := filepath.Join(isolated.originalWorkDir, providerID, "imagebuild", configName)
		if _, err := os.Stat(destinationPath); !os.IsNotExist(err) {
			t.Errorf("destination should not exist when there is no output, stat err: %v", err)
		}
	})
}
