package rpmutils

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/open-edge-platform/os-image-composer/internal/config"
)

func TestIsRPMPackageCacheOutdated(t *testing.T) {
	tmpDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(tmpDir, "bash-1.0-1.x86_64.rpm"), []byte("x"), 0644); err != nil {
		t.Fatalf("failed to write cached rpm: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "coreutils-1.0-1.x86_64.rpm"), []byte("x"), 0644); err != nil {
		t.Fatalf("failed to write cached rpm: %v", err)
	}

	outdated, missing, _, err := isRPMPackageCacheOutdated([]string{"bash", "coreutils"}, tmpDir)
	if err != nil {
		t.Fatalf("isRPMPackageCacheOutdated returned error: %v", err)
	}
	if outdated {
		t.Fatalf("expected cache to be up-to-date, missing=%v", missing)
	}

	outdated, missing, _, err = isRPMPackageCacheOutdated([]string{"bash", "curl"}, tmpDir)
	if err != nil {
		t.Fatalf("isRPMPackageCacheOutdated returned error: %v", err)
	}
	if !outdated {
		t.Fatalf("expected cache to be outdated")
	}
	if len(missing) != 1 || missing[0] != "curl" {
		t.Fatalf("expected missing=[curl], got %v", missing)
	}

	outdated, missing, _, err = isRPMPackageCacheOutdated([]string{"kernel-drivers-gpu-6.12.55"}, tmpDir)
	if err != nil {
		t.Fatalf("isRPMPackageCacheOutdated returned error: %v", err)
	}
	if !outdated {
		t.Fatalf("expected cache to be outdated for missing kernel-drivers-gpu")
	}
	if len(missing) != 1 || missing[0] != "kernel-drivers-gpu-6.12.55" {
		t.Fatalf("expected missing=[kernel-drivers-gpu-6.12.55], got %v", missing)
	}

	if err := os.WriteFile(filepath.Join(tmpDir, "kernel-drivers-gpu-6.12.55-2.emt3.x86_64.rpm"), []byte("x"), 0644); err != nil {
		t.Fatalf("failed to write cached rpm: %v", err)
	}
	outdated, missing, _, err = isRPMPackageCacheOutdated([]string{"kernel-drivers-gpu-6.12.55"}, tmpDir)
	if err != nil {
		t.Fatalf("isRPMPackageCacheOutdated returned error: %v", err)
	}
	if outdated {
		t.Fatalf("expected version-pinned package to be satisfied from cache, missing=%v", missing)
	}
}

func TestClearRPMPackageCache(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(dir string)
		wantLeft []string // non-.rpm files that must survive
	}{
		{
			name: "clears all rpm files",
			setup: func(dir string) {
				_ = os.WriteFile(filepath.Join(dir, "bash-1.0-1.x86_64.rpm"), []byte("x"), 0644)
				_ = os.WriteFile(filepath.Join(dir, "curl-1.0-1.x86_64.rpm"), []byte("x"), 0644)
			},
		},
		{
			name: "leaves non-rpm files untouched",
			setup: func(dir string) {
				_ = os.WriteFile(filepath.Join(dir, "bash-1.0-1.x86_64.rpm"), []byte("x"), 0644)
				_ = os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0644)
			},
			wantLeft: []string{"notes.txt"},
		},
		{
			name:  "empty cache dir is a no-op",
			setup: func(dir string) {},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.setup(dir)

			if err := clearRPMPackageCache(dir); err != nil {
				t.Fatalf("clearRPMPackageCache() unexpected error: %v", err)
			}

			rpms, _ := filepath.Glob(filepath.Join(dir, "*.rpm"))
			if len(rpms) != 0 {
				t.Errorf("expected no .rpm files after clear, got %v", rpms)
			}

			for _, name := range tt.wantLeft {
				if _, statErr := os.Stat(filepath.Join(dir, name)); os.IsNotExist(statErr) {
					t.Errorf("expected file %s to survive cache clear, but it was removed", name)
				}
			}
		})
	}
}

func TestClearRPMMetadataCache(t *testing.T) {
	origCfg := config.Global()
	updatedCfg := origCfg
	updatedCfg.CacheDir = t.TempDir()
	config.SetGlobal(updatedCfg)
	defer config.SetGlobal(origCfg)

	origRepoCfg := RepoCfg
	defer func() { RepoCfg = origRepoCfg }()

	testURL := "https://example.com/rpms/"
	RepoCfg = RepoConfig{URL: testURL}

	metaDirName := generateRPMMetadataDir(testURL)
	metaDir := filepath.Join(updatedCfg.CacheDir, "rpm-metadata", metaDirName)
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		t.Fatalf("failed to create metadata dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(metaDir) })

	for _, name := range []string{"primary.parsed.json", "primary.location.json"} {
		if err := os.WriteFile(filepath.Join(metaDir, name), []byte("{}"), 0644); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}

	pkgDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(pkgDir, "bash-1.0-1.x86_64.rpm"), []byte("x"), 0644); err != nil {
		t.Fatalf("failed to write rpm: %v", err)
	}

	if err := clearRPMPackageCache(pkgDir); err != nil {
		t.Fatalf("clearRPMPackageCache() unexpected error: %v", err)
	}

	for _, name := range []string{"primary.parsed.json", "primary.location.json"} {
		f := filepath.Join(metaDir, name)
		if _, statErr := os.Stat(f); !os.IsNotExist(statErr) {
			t.Errorf("expected %s to be removed after cache clear", name)
		}
	}
}
