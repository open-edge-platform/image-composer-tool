package cache

import (
	"fmt"
	"os"
	"path/filepath"

	fileutil "github.com/open-edge-platform/image-composer-tool/internal/utils/file"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
)

// Isolated holds the fresh, unique cache and workspace directories created for a
// from-scratch (--nocache) build, so the final image can be copied out and the
// directories removed once the build finishes.
type Isolated struct {
	// CacheDir and WorkDir are the freshly created unique directories the caller
	// should point the build at (e.g. by overriding the global config).
	CacheDir string
	WorkDir  string

	originalWorkDir string // caller's originally configured workspace (copy-out target)
	keepWorkDir     bool   // preserve the workspace when copy-out or PostProcess fails
}

// SetupIsolated creates fresh, unique cache and workspace directories adjacent to the
// provided (already-resolved, absolute) configured directories — same filesystem, so
// multi-GB builds stay safe — and returns the Isolated context plus a cleanup function
// that removes them.
//
// It deliberately does not touch global state: pointing the build at the returned
// directories, and restoring afterwards, is the caller's responsibility.
func SetupIsolated(originalCacheDir, originalWorkDir string) (*Isolated, func(), error) {
	uniqueCacheDir, err := os.MkdirTemp(filepath.Dir(originalCacheDir), "ict-nocache-cache-*")
	if err != nil {
		return nil, nil, fmt.Errorf("creating unique cache directory: %w", err)
	}
	uniqueWorkDir, err := os.MkdirTemp(filepath.Dir(originalWorkDir), "ict-nocache-workspace-*")
	if err != nil {
		if removeErr := os.RemoveAll(uniqueCacheDir); removeErr != nil {
			logger.Logger().Warnf("failed to remove unique cache directory %s: %v", uniqueCacheDir, removeErr)
		}
		return nil, nil, fmt.Errorf("creating unique workspace directory: %w", err)
	}

	isolated := &Isolated{
		CacheDir:        uniqueCacheDir,
		WorkDir:         uniqueWorkDir,
		originalWorkDir: originalWorkDir,
	}

	log := logger.Logger()
	log.Infof("--nocache: building in fresh cache %s and workspace %s", uniqueCacheDir, uniqueWorkDir)

	cleanup := func() {
		if err := os.RemoveAll(uniqueCacheDir); err != nil {
			log.Warnf("failed to remove --nocache cache directory %s: %v", uniqueCacheDir, err)
		}
		if isolated.keepWorkDir {
			log.Infof("--nocache: preserving workspace %s for recovery", uniqueWorkDir)
			return
		}
		if err := os.RemoveAll(uniqueWorkDir); err != nil {
			log.Warnf("failed to remove --nocache workspace directory %s: %v", uniqueWorkDir, err)
		}
	}

	return isolated, cleanup, nil
}

// KeepWorkspace marks the isolated workspace to be preserved by cleanup — e.g. when a
// PostProcess step or the output copy-out fails and the partial build is needed for
// recovery.
func (isolated *Isolated) KeepWorkspace() {
	isolated.keepWorkDir = true
}

// PreserveOutput copies the built image directory from the isolated workspace back to
// the originally configured workspace so it survives cleanup. It is a no-op when no
// image build directory was produced.
func (isolated *Isolated) PreserveOutput(providerID, configName string) error {
	sourceImageDir := filepath.Join(isolated.WorkDir, providerID, "imagebuild", configName)
	if _, err := os.Stat(sourceImageDir); err != nil {
		if os.IsNotExist(err) {
			return nil // no image build directory produced; nothing to preserve
		}
		return fmt.Errorf("checking image output directory: %w", err)
	}

	destinationImageDir := filepath.Join(isolated.originalWorkDir, providerID, "imagebuild", configName)
	// Pre-create the destination with 0700 to match the image build directory
	// permissions; CopyDir's mkdir -p would otherwise use more permissive defaults.
	if err := os.MkdirAll(destinationImageDir, 0700); err != nil {
		return fmt.Errorf("creating image output directory %s: %w", destinationImageDir, err)
	}
	// The build runs as root (image files are root-owned) and the copy happens in the
	// same process, so no privilege escalation is needed here.
	if err := fileutil.CopyDir(sourceImageDir, destinationImageDir, "-p", false); err != nil {
		return fmt.Errorf("copying image output to %s: %w", destinationImageDir, err)
	}

	logger.Logger().Infof("--nocache: build output copied to %s", destinationImageDir)
	return nil
}
