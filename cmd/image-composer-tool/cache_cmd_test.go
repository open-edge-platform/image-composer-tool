package main

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

func configureTempGlobalCLI(t *testing.T) (restore func(), cacheDir, workDir string) {
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

	return func() {
		config.SetGlobal(&prev)
	}, cacheDir, workDir
}

func TestCacheCommand_CleanDefaultsRemovePackages(t *testing.T) {
	restore, cacheDir, _ := configureTempGlobalCLI(t)
	defer restore()

	providerDir := filepath.Join(cacheDir, "pkgCache", "azure-linux-azl3-x86_64")
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatalf("mkdir provider cache: %v", err)
	}

	cmd := createCacheCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"clean"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute cache clean: %v", err)
	}

	if _, err := os.Stat(providerDir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected provider cache to be removed, stat error: %v", err)
	}
}

func TestCacheCommand_CleanWorkspaceForProvider(t *testing.T) {
	restore, _, workDir := configureTempGlobalCLI(t)
	defer restore()

	providerID := "azure-linux-azl3-x86_64"
	chrootEnv := filepath.Join(workDir, providerID, "chrootenv")
	chrootBuild := filepath.Join(workDir, providerID, "chrootbuild")

	for _, dir := range []string{chrootEnv, chrootBuild} {
		if err := os.MkdirAll(filepath.Join(dir, "dummy"), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	cmd := createCacheCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"clean", "--workspace", "--provider-id", providerID})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute cache clean workspace: %v", err)
	}

	for _, dir := range []string{chrootEnv, chrootBuild} {
		if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("expected %s to be removed, stat error: %v", dir, err)
		}
	}
}

func TestCacheCommand_DryRunDoesNotDelete(t *testing.T) {
	restore, cacheDir, _ := configureTempGlobalCLI(t)
	defer restore()

	providerDir := filepath.Join(cacheDir, "pkgCache", "azure-linux-azl3-x86_64")
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatalf("mkdir provider cache: %v", err)
	}

	cmd := createCacheCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"clean", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute cache clean dry run: %v", err)
	}

	if _, err := os.Stat(providerDir); err != nil {
		t.Fatalf("expected provider cache to remain, stat error: %v", err)
	}
}
