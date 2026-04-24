package cache

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

func configureTempGlobal(t *testing.T) (cacheDir, workDir string, restore func()) {
	t.Helper()

	tmp := t.TempDir()
	cacheDir = filepath.Join(tmp, "cache")
	workDir = filepath.Join(tmp, "workspace")

	prev := *config.Global()
	cfg := config.DefaultGlobalConfig()
	cfg.CacheDir = cacheDir
	cfg.WorkDir = workDir
	cfg.ConfigDir = filepath.Join(tmp, "config")
	cfg.TempDir = filepath.Join(tmp, "tmp")
	config.SetGlobal(cfg)

	return cacheDir, workDir, func() {
		config.SetGlobal(&prev)
	}
}

func TestClean_RemovesPackageCacheEntries(t *testing.T) {
	cacheDir, _, restore := configureTempGlobal(t)
	defer restore()

	providerDir := filepath.Join(cacheDir, "pkgCache", "azure-linux-azl3-x86_64")
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatalf("mkdir provider cache: %v", err)
	}
	if err := os.WriteFile(filepath.Join(providerDir, "pkg.rpm"), []byte("data"), 0o644); err != nil {
		t.Fatalf("write cache file: %v", err)
	}

	result, err := Clean(CleanOptions{CleanPackages: true})
	if err != nil {
		t.Fatalf("clean packages: %v", err)
	}

	expectedRemoved := []string{providerDir}
	if !reflect.DeepEqual(result.RemovedPaths, expectedRemoved) {
		t.Fatalf("removed paths mismatch\nwant: %v\ngot:  %v", expectedRemoved, result.RemovedPaths)
	}

	if _, err := os.Stat(providerDir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected provider cache to be removed, stat error: %v", err)
	}
}

func TestClean_RemovesWorkspaceChrootForProvider(t *testing.T) {
	_, workDir, restore := configureTempGlobal(t)
	defer restore()

	providerID := "azure-linux-azl3-x86_64"
	chrootEnv := filepath.Join(workDir, providerID, "chrootenv")
	chrootBuild := filepath.Join(workDir, providerID, "chrootbuild")

	for _, dir := range []string{chrootEnv, chrootBuild} {
		if err := os.MkdirAll(filepath.Join(dir, "dummy"), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	imageBuild := filepath.Join(workDir, providerID, "imagebuild")
	if err := os.MkdirAll(imageBuild, 0o755); err != nil {
		t.Fatalf("mkdir imagebuild: %v", err)
	}

	result, err := Clean(CleanOptions{CleanWorkspace: true, ProviderID: providerID})
	if err != nil {
		t.Fatalf("clean workspace: %v", err)
	}

	expectedRemoved := []string{chrootBuild, chrootEnv}
	if !reflect.DeepEqual(result.RemovedPaths, expectedRemoved) {
		t.Fatalf("removed paths mismatch\nwant: %v\ngot:  %v", expectedRemoved, result.RemovedPaths)
	}

	for _, dir := range expectedRemoved {
		if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("expected %s to be removed, stat error: %v", dir, err)
		}
	}

	if _, err := os.Stat(imageBuild); err != nil {
		t.Fatalf("expected imagebuild to remain, stat error: %v", err)
	}
}

func TestClean_DryRunDoesNotDelete(t *testing.T) {
	cacheDir, _, restore := configureTempGlobal(t)
	defer restore()

	providerDir := filepath.Join(cacheDir, "pkgCache", "azure-linux-azl3-x86_64")
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatalf("mkdir provider cache: %v", err)
	}

	result, err := Clean(CleanOptions{CleanPackages: true, DryRun: true})
	if err != nil {
		t.Fatalf("clean packages dry run: %v", err)
	}

	expectedRemoved := []string{providerDir}
	if !reflect.DeepEqual(result.RemovedPaths, expectedRemoved) {
		t.Fatalf("removed paths mismatch\nwant: %v\ngot:  %v", expectedRemoved, result.RemovedPaths)
	}

	if _, err := os.Stat(providerDir); err != nil {
		t.Fatalf("expected provider cache to remain, stat error: %v", err)
	}
}
