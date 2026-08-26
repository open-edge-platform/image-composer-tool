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

// TestSetupIsolated_CreatesCacheParentIfMissing verifies that SetupIsolated creates
// missing parent directories for the cache directory before calling MkdirTemp.
func TestSetupIsolated_CreatesCacheParentIfMissing(t *testing.T) {
	originalCacheDir := filepath.Join(t.TempDir(), "does-not-exist", "cache")
	originalWorkDir := filepath.Join(t.TempDir(), "workspace")

	isolated, cleanup, err := SetupIsolated(originalCacheDir, originalWorkDir)
	if err != nil {
		t.Fatalf("SetupIsolated should succeed when parent is missing (it creates it): %v", err)
	}
	defer cleanup()

	// The unique cache directory should have been created under the (now-existing) parent.
	if _, statErr := os.Stat(isolated.CacheDir); statErr != nil {
		t.Errorf("unique cache directory should exist: %v", statErr)
	}
	if got, want := filepath.Dir(isolated.CacheDir), filepath.Dir(originalCacheDir); got != want {
		t.Errorf("unique cache directory parent = %q, want %q", got, want)
	}
}

// TestSetupIsolated_CreatesWorkParentIfMissing verifies that SetupIsolated creates
// missing parent directories for the workspace directory before calling MkdirTemp.
func TestSetupIsolated_CreatesWorkParentIfMissing(t *testing.T) {
	tempDir := t.TempDir()
	originalCacheDir := filepath.Join(tempDir, "cache")
	originalWorkDir := filepath.Join(tempDir, "missing-parent", "workspace")

	isolated, cleanup, err := SetupIsolated(originalCacheDir, originalWorkDir)
	if err != nil {
		t.Fatalf("SetupIsolated should succeed when workspace parent is missing (it creates it): %v", err)
	}
	defer cleanup()

	// The unique workspace directory should have been created under the (now-existing) parent.
	if _, statErr := os.Stat(isolated.WorkDir); statErr != nil {
		t.Errorf("unique workspace directory should exist: %v", statErr)
	}
	if got, want := filepath.Dir(isolated.WorkDir), filepath.Dir(originalWorkDir); got != want {
		t.Errorf("unique workspace directory parent = %q, want %q", got, want)
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

	t.Run("RejectsTraversalInProviderID", func(t *testing.T) {
		tempDir := t.TempDir()
		isolated := &Isolated{
			WorkDir:         filepath.Join(tempDir, "unique-workspace"),
			originalWorkDir: filepath.Join(tempDir, "orig-workspace"),
		}

		err := isolated.PreserveOutput("../../etc", configName)

		if err == nil {
			t.Fatal("expected error when providerID contains path traversal")
		}
		if !strings.Contains(err.Error(), "escapes") {
			t.Errorf("error message should mention 'escapes', got: %v", err)
		}
		// No directory should have been created inside unique-workspace as a side effect.
		if _, statErr := os.Stat(isolated.WorkDir); statErr == nil {
			t.Errorf("unique-workspace should not have been created before the error")
		}
	})

	t.Run("RejectsTraversalInConfigName", func(t *testing.T) {
		tempDir := t.TempDir()
		isolated := &Isolated{
			WorkDir:         filepath.Join(tempDir, "unique-workspace"),
			originalWorkDir: filepath.Join(tempDir, "orig-workspace"),
		}

		err := isolated.PreserveOutput(providerID, "../../../etc/passwd")

		if err == nil {
			t.Fatal("expected error when configName contains path traversal")
		}
		if !strings.Contains(err.Error(), "escapes") {
			t.Errorf("error message should mention 'escapes', got: %v", err)
		}
		// No directory should have been created inside orig-workspace as a side effect.
		if _, statErr := os.Stat(isolated.originalWorkDir); statErr == nil {
			t.Errorf("orig-workspace should not have been created before the error")
		}
	})
}
