package isomaker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/chroot"
	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

// stubChrootEnv embeds the interface (nil) and overrides only the methods
// copyConfigFilesToIso actually calls, avoiding a 19-method mock for one test.
type stubChrootEnv struct {
	chroot.ChrootEnvInterface
	targetOsConfigDir string
}

func (s *stubChrootEnv) GetTargetOsConfigDir() string { return s.targetOsConfigDir }

func TestCopyConfigFilesToIso_BasenameCollision(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: "mkdir", Output: "", Error: nil},
		{Pattern: "cp", Output: "", Error: nil},
	})

	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	original := config.Global()
	saved := *original
	config.SetGlobal(&config.GlobalConfig{
		ConfigDir: filepath.Join(repoRoot, "config"),
		CacheDir:  saved.CacheDir,
		WorkDir:   saved.WorkDir,
		Logging:   saved.Logging,
	})
	defer config.SetGlobal(&saved)

	osvConfigSrc := t.TempDir()
	installRoot := t.TempDir()

	fileA := filepath.Join(t.TempDir(), "same-name.txt")
	fileB := filepath.Join(t.TempDir(), "same-name.txt")
	if err := os.WriteFile(fileA, []byte("a"), 0644); err != nil {
		t.Fatalf("write fileA: %v", err)
	}
	if err := os.WriteFile(fileB, []byte("b"), 0644); err != nil {
		t.Fatalf("write fileB: %v", err)
	}

	isoMaker := &IsoMaker{
		ImageBuildDir: t.TempDir(),
		ChrootEnv:     &stubChrootEnv{targetOsConfigDir: osvConfigSrc},
	}
	template := &config.ImageTemplate{
		Target: config.TargetInfo{OS: "ubuntu", Dist: "ubuntu24"},
		SystemConfig: config.SystemConfig{
			AdditionalFiles: []config.AdditionalFileInfo{
				{Local: fileA, Final: "/opt/a/same-name.txt"},
				{Local: fileB, Final: "/opt/b/same-name.txt"},
			},
		},
	}

	err = isoMaker.copyConfigFilesToIso(template, installRoot)
	if err == nil {
		t.Fatal("expected a basename collision error, got none")
	}
	if !strings.Contains(err.Error(), "both resolve to basename") {
		t.Fatalf("expected basename collision error, got: %v", err)
	}
}

// TestCopyConfigFilesToIso_SameSourceMultipleFinalsAllowed is a regression
// test: mergeAdditionalFiles keys by Final path, so the same Local source
// intentionally installed at two different Final destinations is a valid,
// supported pattern and must not be rejected as a basename clobber.
func TestCopyConfigFilesToIso_SameSourceMultipleFinalsAllowed(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: "mkdir", Output: "", Error: nil},
		{Pattern: "cp", Output: "", Error: nil},
	})

	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	original := config.Global()
	saved := *original
	config.SetGlobal(&config.GlobalConfig{
		ConfigDir: filepath.Join(repoRoot, "config"),
		CacheDir:  saved.CacheDir,
		WorkDir:   saved.WorkDir,
		Logging:   saved.Logging,
	})
	defer config.SetGlobal(&saved)

	osvConfigSrc := t.TempDir()
	installRoot := t.TempDir()

	sharedFile := filepath.Join(t.TempDir(), "shared.txt")
	if err := os.WriteFile(sharedFile, []byte("shared"), 0644); err != nil {
		t.Fatalf("write sharedFile: %v", err)
	}

	isoMaker := &IsoMaker{
		ImageBuildDir: t.TempDir(),
		ChrootEnv:     &stubChrootEnv{targetOsConfigDir: osvConfigSrc},
	}
	template := &config.ImageTemplate{
		Target: config.TargetInfo{OS: "ubuntu", Dist: "ubuntu24"},
		SystemConfig: config.SystemConfig{
			AdditionalFiles: []config.AdditionalFileInfo{
				{Local: sharedFile, Final: "/opt/a/shared.txt"},
				{Local: sharedFile, Final: "/opt/b/shared.txt"},
			},
		},
	}

	if err := isoMaker.copyConfigFilesToIso(template, installRoot); err != nil {
		t.Fatalf("expected no error for the same source at two final paths, got: %v", err)
	}
}
