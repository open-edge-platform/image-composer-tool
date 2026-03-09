package emt

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/open-edge-platform/os-image-composer/internal/chroot"
	"github.com/open-edge-platform/os-image-composer/internal/config"
	"github.com/open-edge-platform/os-image-composer/internal/utils/shell"
	"github.com/open-edge-platform/os-image-composer/internal/utils/system"
)

type mockEmtChrootEnv struct {
	updateSystemPkgsErr error
	initChrootErr       error
	cleanupErr          error

	updateSystemPkgsCalls int
	initChrootCalls       int
	cleanupCalls          int
}

var _ chroot.ChrootEnvInterface = (*mockEmtChrootEnv)(nil)

func (m *mockEmtChrootEnv) GetChrootEnvRoot() string          { return "/tmp/test-chroot" }
func (m *mockEmtChrootEnv) GetChrootImageBuildDir() string    { return "/tmp/test-build" }
func (m *mockEmtChrootEnv) GetTargetOsPkgType() string        { return "rpm" }
func (m *mockEmtChrootEnv) GetTargetOsConfigDir() string      { return "/tmp/test-config" }
func (m *mockEmtChrootEnv) GetTargetOsReleaseVersion() string { return "3.0" }
func (m *mockEmtChrootEnv) GetChrootPkgCacheDir() string      { return "/tmp/test-cache" }

func (m *mockEmtChrootEnv) GetChrootEnvEssentialPackageList() ([]string, error) {
	return []string{"base-files"}, nil
}

func (m *mockEmtChrootEnv) GetChrootEnvHostPath(chrootPath string) (string, error) {
	return chrootPath, nil
}
func (m *mockEmtChrootEnv) GetChrootEnvPath(hostPath string) (string, error) { return hostPath, nil }
func (m *mockEmtChrootEnv) MountChrootSysfs(chrootPath string) error         { return nil }
func (m *mockEmtChrootEnv) UmountChrootSysfs(chrootPath string) error        { return nil }

func (m *mockEmtChrootEnv) MountChrootPath(hostFullPath, chrootPath, mountFlags string) error {
	return nil
}

func (m *mockEmtChrootEnv) UmountChrootPath(chrootPath string) error { return nil }

func (m *mockEmtChrootEnv) CopyFileFromHostToChroot(hostFilePath, chrootPath string) error {
	return nil
}

func (m *mockEmtChrootEnv) CopyFileFromChrootToHost(hostFilePath, chrootPath string) error {
	return nil
}

func (m *mockEmtChrootEnv) UpdateChrootLocalRepoMetadata(chrootRepoDir string, targetArch string, sudo bool) error {
	return nil
}

func (m *mockEmtChrootEnv) RefreshLocalCacheRepo() error { return nil }

func (m *mockEmtChrootEnv) InitChrootEnv(targetOs, targetDist, targetArch string) error {
	m.initChrootCalls++
	return m.initChrootErr
}

func (m *mockEmtChrootEnv) CleanupChrootEnv(targetOs, targetDist, targetArch string) error {
	m.cleanupCalls++
	return m.cleanupErr
}

func (m *mockEmtChrootEnv) TdnfInstallPackage(packageName, installRoot string, repositoryIDList []string) error {
	return nil
}

func (m *mockEmtChrootEnv) AptInstallPackage(packageName, installRoot string, repoSrcList []string) error {
	return nil
}

func (m *mockEmtChrootEnv) UpdateSystemPkgs(template *config.ImageTemplate) error {
	m.updateSystemPkgsCalls++
	return m.updateSystemPkgsErr
}

func setTestGlobalConfig(t *testing.T, cfg *config.GlobalConfig) {
	t.Helper()

	old := *config.Global()
	config.SetGlobal(cfg)

	t.Cleanup(func() {
		oldCopy := old
		config.SetGlobal(&oldCopy)
	})
}

func setTestOSReleaseFile(t *testing.T, name, version string) {
	t.Helper()

	osRelease := filepath.Join(t.TempDir(), "os-release")
	content := "NAME=\"" + name + "\"\nVERSION_ID=\"" + version + "\"\n"
	if err := os.WriteFile(osRelease, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create test os-release file: %v", err)
	}

	old := system.OsReleaseFile
	system.OsReleaseFile = osRelease
	t.Cleanup(func() {
		system.OsReleaseFile = old
	})
}

func setMockShellExecutor(t *testing.T, commands []shell.MockCommand) {
	t.Helper()

	old := shell.Default
	shell.Default = shell.NewMockExecutor(commands)
	t.Cleanup(func() {
		shell.Default = old
	})
}

func setTempGlobalDirs(t *testing.T) {
	t.Helper()

	tmp := t.TempDir()
	cfg := config.DefaultGlobalConfig()
	cfg.CacheDir = filepath.Join(tmp, "cache")
	cfg.WorkDir = filepath.Join(tmp, "work")
	cfg.TempDir = filepath.Join(tmp, "tmp")

	setTestGlobalConfig(t, cfg)
}

func testRepoRoot(t *testing.T) string {
	t.Helper()

	_, filePath, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to get current file path")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(filePath), "../../.."))
}

func TestLoadRepoConfigFromYAMLSuccess(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.DefaultGlobalConfig()
	cfg.ConfigDir = filepath.Join(testRepoRoot(t), "config")
	cfg.CacheDir = filepath.Join(tmp, "cache")
	cfg.WorkDir = filepath.Join(tmp, "work")
	cfg.TempDir = filepath.Join(tmp, "tmp")
	setTestGlobalConfig(t, cfg)

	repoCfg, err := loadRepoConfigFromYAML("emt3", "x86_64")
	if err != nil {
		t.Fatalf("loadRepoConfigFromYAML returned error: %v", err)
	}

	if repoCfg.Section != "emt3.0-base" {
		t.Errorf("expected section emt3.0-base, got %q", repoCfg.Section)
	}
	if repoCfg.URL == "" {
		t.Error("expected non-empty URL")
	}
	if !repoCfg.GPGCheck || !repoCfg.RepoGPGCheck || !repoCfg.Enabled {
		t.Errorf("expected gpg and enabled flags to be true, got %+v", repoCfg)
	}
}

func TestLoadRepoConfigFromYAMLRejectsNonRPMRepository(t *testing.T) {
	tmp := t.TempDir()
	providerConfigDir := filepath.Join(tmp, "config", "osv", OsName, "emt-unit", "providerconfigs")
	if err := os.MkdirAll(providerConfigDir, 0755); err != nil {
		t.Fatalf("failed to create provider config dir: %v", err)
	}

	repoConfig := `name: "Unit Test Repo"
type: "deb"
baseURL: "https://example.invalid/repo"
component: "main"
gpgCheck: true
repoGPGCheck: true
enabled: true
gpgKey: "https://example.invalid/key"
`
	if err := os.WriteFile(filepath.Join(providerConfigDir, "x86_64_repo.yml"), []byte(repoConfig), 0644); err != nil {
		t.Fatalf("failed to write test repo config: %v", err)
	}

	cfg := config.DefaultGlobalConfig()
	cfg.ConfigDir = filepath.Join(tmp, "config")
	setTestGlobalConfig(t, cfg)

	_, err := loadRepoConfigFromYAML("emt-unit", "x86_64")
	if err == nil {
		t.Fatal("expected error for non-rpm repository type")
	}

	if !strings.Contains(err.Error(), "expected RPM repository type") {
		t.Errorf("expected non-rpm type error, got %v", err)
	}
}

func TestEmtInstallHostDependencySuccess(t *testing.T) {
	setTestOSReleaseFile(t, "Ubuntu", "24.04")
	setMockShellExecutor(t, []shell.MockCommand{
		{Pattern: "uname -m", Output: "x86_64", Error: nil},
		{Pattern: "command -v .*", Output: "", Error: nil},
		{Pattern: "apt install -y .*", Output: "installed", Error: nil},
	})

	if err := (&Emt{}).installHostDependency(); err != nil {
		t.Fatalf("installHostDependency returned error: %v", err)
	}
}

func TestEmtInstallHostDependencyUnsupportedHostOS(t *testing.T) {
	setTestOSReleaseFile(t, "Unsupported Linux", "1.0")
	setMockShellExecutor(t, []shell.MockCommand{
		{Pattern: "uname -m", Output: "x86_64", Error: nil},
	})

	err := (&Emt{}).installHostDependency()
	if err == nil {
		t.Fatal("expected unsupported host os error")
	}

	if !strings.Contains(err.Error(), "unsupported host OS") {
		t.Errorf("expected unsupported host os error, got %v", err)
	}
}

func TestEmtInstallHostDependencyCommandCheckFailure(t *testing.T) {
	setTestOSReleaseFile(t, "Ubuntu", "24.04")
	setMockShellExecutor(t, []shell.MockCommand{
		{Pattern: "uname -m", Output: "x86_64", Error: nil},
		{Pattern: "command -v .*", Output: "command output", Error: errors.New("exec failure")},
	})

	err := (&Emt{}).installHostDependency()
	if err == nil {
		t.Fatal("expected command existence check error")
	}

	if !strings.Contains(err.Error(), "failed to check command") {
		t.Errorf("expected command check error, got %v", err)
	}
}

func TestEmtInstallHostDependencyInstallFailure(t *testing.T) {
	setTestOSReleaseFile(t, "Ubuntu", "24.04")
	setMockShellExecutor(t, []shell.MockCommand{
		{Pattern: "uname -m", Output: "x86_64", Error: nil},
		{Pattern: "command -v .*", Output: "", Error: nil},
		{Pattern: "apt install -y .*", Output: "", Error: errors.New("install failed")},
	})

	err := (&Emt{}).installHostDependency()
	if err == nil {
		t.Fatal("expected install failure")
	}

	if !strings.Contains(err.Error(), "failed to install host dependency") {
		t.Errorf("expected install host dependency error, got %v", err)
	}
}

func TestEmtDownloadImagePkgsUpdateError(t *testing.T) {
	provider := &Emt{
		chrootEnv: &mockEmtChrootEnv{updateSystemPkgsErr: errors.New("update failed")},
	}

	err := provider.downloadImagePkgs(createTestImageTemplate())
	if err == nil {
		t.Fatal("expected update system packages error")
	}

	if !strings.Contains(err.Error(), "failed to update system packages") {
		t.Errorf("expected update system packages error, got %v", err)
	}
}

func TestEmtDownloadImagePkgsDownloadFailure(t *testing.T) {
	setTempGlobalDirs(t)

	mockEnv := &mockEmtChrootEnv{}
	provider := &Emt{chrootEnv: mockEnv}

	err := provider.downloadImagePkgs(createTestImageTemplate())
	if err == nil {
		t.Fatal("expected package download error")
	}

	if !strings.Contains(err.Error(), "failed to download packages") {
		t.Errorf("expected download packages error, got %v", err)
	}
	if mockEnv.updateSystemPkgsCalls != 1 {
		t.Errorf("expected UpdateSystemPkgs to be called once, got %d", mockEnv.updateSystemPkgsCalls)
	}
}

func TestEmtPreProcessInstallDependencyError(t *testing.T) {
	setTestOSReleaseFile(t, "Ubuntu", "24.04")
	setMockShellExecutor(t, []shell.MockCommand{
		{Pattern: "uname -m", Output: "x86_64", Error: nil},
		{Pattern: "command -v .*", Output: "command output", Error: errors.New("exec failure")},
	})

	mockEnv := &mockEmtChrootEnv{}
	provider := &Emt{chrootEnv: mockEnv}

	err := provider.PreProcess(createTestImageTemplate())
	if err == nil {
		t.Fatal("expected preprocess dependency error")
	}

	if !strings.Contains(err.Error(), "failed to install host dependencies") {
		t.Errorf("expected host dependency error, got %v", err)
	}
	if mockEnv.updateSystemPkgsCalls != 0 {
		t.Errorf("expected UpdateSystemPkgs to not be called, got %d", mockEnv.updateSystemPkgsCalls)
	}
}

func TestEmtPreProcessDownloadError(t *testing.T) {
	setTempGlobalDirs(t)
	setTestOSReleaseFile(t, "Ubuntu", "24.04")
	setMockShellExecutor(t, []shell.MockCommand{
		{Pattern: "uname -m", Output: "x86_64", Error: nil},
		{Pattern: "command -v .*", Output: "/usr/bin/tool", Error: nil},
	})

	mockEnv := &mockEmtChrootEnv{}
	provider := &Emt{chrootEnv: mockEnv}

	err := provider.PreProcess(createTestImageTemplate())
	if err == nil {
		t.Fatal("expected preprocess download error")
	}

	if !strings.Contains(err.Error(), "failed to download image packages") {
		t.Errorf("expected preprocess download error, got %v", err)
	}
	if mockEnv.updateSystemPkgsCalls != 1 {
		t.Errorf("expected UpdateSystemPkgs to be called once, got %d", mockEnv.updateSystemPkgsCalls)
	}
	if mockEnv.initChrootCalls != 0 {
		t.Errorf("expected InitChrootEnv to not be called, got %d", mockEnv.initChrootCalls)
	}
}

func TestEmtPostProcessBehavior(t *testing.T) {
	template := createTestImageTemplate()
	inputErr := errors.New("build failed")

	tests := []struct {
		name        string
		cleanupErr  error
		inputErr    error
		wantContain string
		wantNil     bool
		wantInput   bool
	}{
		{name: "returns input error on cleanup success", cleanupErr: nil, inputErr: inputErr, wantInput: true},
		{name: "returns nil on full success", cleanupErr: nil, inputErr: nil, wantNil: true},
		{
			name:        "returns cleanup error when cleanup fails",
			cleanupErr:  errors.New("cleanup failed"),
			inputErr:    inputErr,
			wantContain: "failed to cleanup chroot environment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockEnv := &mockEmtChrootEnv{cleanupErr: tt.cleanupErr}
			provider := &Emt{chrootEnv: mockEnv}

			err := provider.PostProcess(template, tt.inputErr)

			if tt.wantContain != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tt.wantContain)
				}
				if !strings.Contains(err.Error(), tt.wantContain) {
					t.Fatalf("expected error containing %q, got %v", tt.wantContain, err)
				}
			} else if tt.wantNil {
				if err != nil {
					t.Fatalf("expected nil error, got %v", err)
				}
			} else if tt.wantInput {
				if !errors.Is(err, tt.inputErr) {
					t.Fatalf("expected input error to be returned, got %v", err)
				}
			}

			if mockEnv.cleanupCalls != 1 {
				t.Errorf("expected CleanupChrootEnv to be called once, got %d", mockEnv.cleanupCalls)
			}
		})
	}
}
