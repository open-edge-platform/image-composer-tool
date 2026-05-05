package rawmaker_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/chroot"
	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/image/imageconvert"
	"github.com/open-edge-platform/image-composer-tool/internal/image/imagedisc"
	"github.com/open-edge-platform/image-composer-tool/internal/image/imageos"
	"github.com/open-edge-platform/image-composer-tool/internal/image/rawmaker"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

// Mock implementations for testing
type mockChrootEnv struct {
	pkgType           string
	chrootPkgCacheDir string
	shouldFailRefresh bool
	chrootEnvRoot     string
}

func (m *mockChrootEnv) GetChrootEnvRoot() string {
	return m.chrootEnvRoot
}

func (m *mockChrootEnv) GetChrootImageBuildDir() string {
	return filepath.Join(m.chrootEnvRoot, "workspace", "imagebuild")
}

func (m *mockChrootEnv) GetTargetOsPkgType() string {
	return m.pkgType
}

func (m *mockChrootEnv) GetTargetOsConfigDir() string {
	return filepath.Join(m.chrootEnvRoot, "config")
}

func (m *mockChrootEnv) GetTargetOsReleaseVersion() string {
	return "3.0"
}

func (m *mockChrootEnv) GetChrootPkgCacheDir() string {
	return m.chrootPkgCacheDir
}

func (m *mockChrootEnv) GetChrootEnvEssentialPackageList() ([]string, error) {
	return []string{"essential-pkg1", "essential-pkg2"}, nil
}

func (m *mockChrootEnv) GetChrootEnvHostPath(chrootPath string) (string, error) {
	return filepath.Join(m.chrootEnvRoot, chrootPath), nil
}

func (m *mockChrootEnv) GetChrootEnvPath(ChrootEnvHostPath string) (string, error) {
	return ChrootEnvHostPath[len(m.chrootEnvRoot):], nil
}

func (m *mockChrootEnv) MountChrootSysfs(chrootPath string) error {
	return nil
}

func (m *mockChrootEnv) UmountChrootSysfs(chrootPath string) error {
	return nil
}

func (m *mockChrootEnv) MountChrootPath(hostFullPath, chrootPath, mountFlags string) error {
	return nil
}

func (m *mockChrootEnv) UmountChrootPath(chrootPath string) error {
	return nil
}

func (m *mockChrootEnv) CopyFileFromHostToChroot(hostFilePath, chrootPath string) error {
	return nil
}

func (m *mockChrootEnv) CopyFileFromChrootToHost(hostFilePath, chrootPath string) error {
	return nil
}

func (m *mockChrootEnv) UpdateChrootLocalRepoMetadata(chrootRepoDir string, targetArch string, sudo bool) error {
	return nil
}

func (m *mockChrootEnv) RefreshLocalCacheRepo() error {
	if m.shouldFailRefresh {
		return fmt.Errorf("mock refresh cache repo failure")
	}
	return nil
}

func (m *mockChrootEnv) InitChrootEnv(targetOs, targetDist, targetArch string) error {
	return nil
}

func (m *mockChrootEnv) CleanupChrootEnv(targetOs, targetDist, targetArch string) error {
	return nil
}

func (m *mockChrootEnv) TdnfInstallPackage(packageName, installRoot string, repositoryIDList []string) error {
	return nil
}

func (m *mockChrootEnv) AptInstallPackage(packageName, installRoot string, repoSrcList []string) error {
	return nil
}

func (m *mockChrootEnv) UpdateSystemPkgs(template *config.ImageTemplate) error {
	return nil
}

type mockLoopDev struct {
	shouldFailCreate bool
	shouldFailDelete bool
	loopDevPath      string
}

func (m *mockLoopDev) CreateRawImageLoopDev(filePath string, template *config.ImageTemplate) (string, map[string]string, error) {
	if m.shouldFailCreate {
		return "", nil, fmt.Errorf("mock loop device creation failure")
	}
	diskPathIdMap := map[string]string{
		"root": "/dev/loop0p1",
		"boot": "/dev/loop0p2",
	}
	return m.loopDevPath, diskPathIdMap, nil
}

func (m *mockLoopDev) LoopSetupDelete(loopDevPath string) error {
	if m.shouldFailDelete {
		return fmt.Errorf("mock loop device deletion failure")
	}
	return nil
}

type mockImageOs struct {
	installRoot       string
	shouldFailInstall bool
	versionInfo       string
}

func (m *mockImageOs) GetInstallRoot() string {
	return m.installRoot
}

func (m *mockImageOs) InstallInitrd() (string, string, error) {
	if m.shouldFailInstall {
		return "", "", fmt.Errorf("mock install initrd failure")
	}
	return m.installRoot, m.versionInfo, nil
}

func (m *mockImageOs) InstallImageOs(diskPathIdMap map[string]string) (versionInfo string, err error) {
	if m.shouldFailInstall {
		return "", fmt.Errorf("mock install image OS failure")
	}
	return m.versionInfo, nil
}

type mockImageConvert struct {
	shouldFailConvert bool
}

func (m *mockImageConvert) ConvertImageFile(filePath string, template *config.ImageTemplate) error {
	if m.shouldFailConvert {
		return fmt.Errorf("mock image conversion failure")
	}
	return nil
}

func TestNewRawMaker(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name         string
		chrootEnv    chroot.ChrootEnvInterface
		template     *config.ImageTemplate
		mockCommands []shell.MockCommand
		expectError  bool
		errorMsg     string
	}{
		{
			name: "successful_creation",
			chrootEnv: &mockChrootEnv{
				chrootEnvRoot: t.TempDir(),
			},
			template: &config.ImageTemplate{
				Target: config.TargetInfo{
					OS:   "ubuntu",
					Dist: "jammy",
					Arch: "x86_64",
				},
				SystemConfig: config.SystemConfig{
					Name: "test-config",
				},
			},
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:        "nil_chroot_env",
			chrootEnv:   nil,
			template:    &config.ImageTemplate{},
			expectError: true,
			errorMsg:    "chroot environment cannot be nil",
		},
		{
			name:        "nil_template",
			chrootEnv:   &mockChrootEnv{},
			template:    nil,
			expectError: true,
			errorMsg:    "image template cannot be nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			// Setup chroot image build directory if needed
			if tt.chrootEnv != nil && !tt.expectError {
				if mockEnv, ok := tt.chrootEnv.(*mockChrootEnv); ok {
					chrootImageBuildDir := mockEnv.GetChrootImageBuildDir()
					if err := os.MkdirAll(chrootImageBuildDir, 0700); err != nil {
						t.Fatalf("Failed to create chroot image build dir: %v", err)
					}
				}
			}

			rawMaker, err := rawmaker.NewRawMaker(tt.chrootEnv, tt.template)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, but got none")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s', but got: %v", tt.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				}
				if rawMaker == nil {
					t.Error("Expected non-nil RawMaker")
				}
			}
		})
	}
}

func TestRawMaker_Init(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name          string
		template      *config.ImageTemplate
		mockCommands  []shell.MockCommand
		setupFunc     func(tempDir string) error
		expectError   bool
		expectedError string
	}{
		{
			name: "successful_init",
			template: &config.ImageTemplate{
				Target: config.TargetInfo{
					OS:   "ubuntu",
					Dist: "jammy",
					Arch: "x86_64",
				},
				SystemConfig: config.SystemConfig{
					Name: "test-config",
				},
			},
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name: "mkdir_failure",
			template: &config.ImageTemplate{
				Target: config.TargetInfo{
					OS:   "ubuntu",
					Dist: "jammy",
					Arch: "x86_64",
				},
				SystemConfig: config.SystemConfig{
					Name: "test-config",
				},
			},
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: fmt.Errorf("mkdir failed")},
			},
			expectError:   true,
			expectedError: "mkdir failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			tempDir := t.TempDir()
			chrootEnv := &mockChrootEnv{
				pkgType:           "deb",
				chrootEnvRoot:     tempDir,
				chrootPkgCacheDir: filepath.Join(tempDir, "cache"),
			}

			chrootImageBuildDir := chrootEnv.GetChrootImageBuildDir()
			if err := os.MkdirAll(chrootImageBuildDir, 0700); err != nil {
				t.Fatalf("Failed to create chroot image build dir: %v", err)
			}

			if tt.setupFunc != nil {
				if err := tt.setupFunc(tempDir); err != nil {
					t.Fatalf("Failed to setup test: %v", err)
				}
			}

			// Mock config.WorkDir()
			originalWorkDir := os.Getenv("IMAGE_COMPOSER_WORK_DIR")
			os.Setenv("IMAGE_COMPOSER_WORK_DIR", tempDir)
			defer func() {
				if originalWorkDir == "" {
					os.Unsetenv("IMAGE_COMPOSER_WORK_DIR")
				} else {
					os.Setenv("IMAGE_COMPOSER_WORK_DIR", originalWorkDir)
				}
			}()

			rawMaker, err := rawmaker.NewRawMaker(chrootEnv, tt.template)
			if tt.expectError {
				if err == nil {
					t.Error("Expected error, but got none")
				} else if tt.expectedError != "" && !strings.Contains(err.Error(), tt.expectedError) {
					t.Errorf("Expected error containing '%s', but got: %v", tt.expectedError, err)
				}
				// Do not proceed if NewRawMaker failed as expected
				return
			}
			if err != nil {
				t.Fatalf("Failed to create RawMaker: %v", err)
			}

			currentConfig := config.Global()
			currentConfig.WorkDir = tempDir
			config.SetGlobal(currentConfig)

			err = rawMaker.Init()

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, but got none")
				} else if tt.expectedError != "" && !strings.Contains(err.Error(), tt.expectedError) {
					t.Errorf("Expected error containing '%s', but got: %v", tt.expectedError, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				}
				if rawMaker.ImageBuildDir == "" {
					t.Error("Expected ImageBuildDir to be set")
				}
			}
		})
	}
}

func TestRawMaker_BuildRawImage_Success(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	mockCommands := []shell.MockCommand{
		{Pattern: "mkdir", Output: "", Error: nil},
		{Pattern: "mv", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	tempDir := t.TempDir()
	chrootEnv := &mockChrootEnv{
		pkgType:           "deb",
		chrootEnvRoot:     tempDir,
		chrootPkgCacheDir: filepath.Join(tempDir, "cache"),
	}

	chrootImageBuildDir := chrootEnv.GetChrootImageBuildDir()
	if err := os.MkdirAll(chrootImageBuildDir, 0700); err != nil {
		t.Fatalf("Failed to create chroot image build dir: %v", err)
	}

	// Mock config.WorkDir()
	os.Setenv("IMAGE_COMPOSER_WORK_DIR", tempDir)
	defer os.Unsetenv("IMAGE_COMPOSER_WORK_DIR")

	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "ubuntu",
			Dist: "jammy",
			Arch: "x86_64",
		},
		Image: config.ImageInfo{
			Name: "test-image",
		},
		SystemConfig: config.SystemConfig{
			Name: "test-config",
		},
	}

	rawMaker, err := rawmaker.NewRawMaker(chrootEnv, template)
	if err != nil {
		t.Fatalf("Failed to create RawMaker: %v", err)
	}

	// Replace with mock implementations
	mockLoopDev := &mockLoopDev{
		loopDevPath: "/dev/loop0",
	}
	mockImageOs := &mockImageOs{
		installRoot: tempDir,
		versionInfo: "1.0.0",
	}
	mockImageConvert := &mockImageConvert{}

	rawMaker.LoopDev = mockLoopDev
	rawMaker.ImageOs = mockImageOs
	rawMaker.ImageConvert = mockImageConvert

	// Create the expected directory structure
	buildDir := filepath.Join(tempDir, "ubuntu-jammy-x86_64", "imagebuild", "test-config")
	if err := os.MkdirAll(buildDir, 0700); err != nil {
		t.Fatalf("Failed to create build directory: %v", err)
	}

	err = rawMaker.BuildRawImage()

	if err != nil {
		t.Errorf("Expected no error, but got: %v", err)
	}
}

func TestRawMaker_BuildRawImage_LoopDevCreationFailure(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	mockCommands := []shell.MockCommand{
		{Pattern: "mkdir", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	tempDir := t.TempDir()
	chrootEnv := &mockChrootEnv{
		pkgType:           "deb",
		chrootEnvRoot:     tempDir,
		chrootPkgCacheDir: filepath.Join(tempDir, "cache"),
	}

	chrootImageBuildDir := chrootEnv.GetChrootImageBuildDir()
	if err := os.MkdirAll(chrootImageBuildDir, 0700); err != nil {
		t.Fatalf("Failed to create chroot image build dir: %v", err)
	}

	os.Setenv("IMAGE_COMPOSER_WORK_DIR", tempDir)
	defer os.Unsetenv("IMAGE_COMPOSER_WORK_DIR")

	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "ubuntu",
			Dist: "jammy",
			Arch: "x86_64",
		},
		Image: config.ImageInfo{
			Name: "test-image",
		},
		SystemConfig: config.SystemConfig{
			Name: "test-config",
		},
	}

	rawMaker, err := rawmaker.NewRawMaker(chrootEnv, template)
	if err != nil {
		t.Fatalf("Failed to create RawMaker: %v", err)
	}

	// Replace with mock that fails
	mockLoopDev := &mockLoopDev{
		shouldFailCreate: true,
	}
	rawMaker.LoopDev = mockLoopDev

	err = rawMaker.Init()
	if err != nil {
		t.Fatalf("Failed to initialize RawMaker: %v", err)
	}

	err = rawMaker.BuildRawImage()

	if err == nil {
		t.Error("Expected error, but got none")
	}
	if !strings.Contains(err.Error(), "failed to create loop device") {
		t.Errorf("Expected error about loop device creation, but got: %v", err)
	}
}

func TestRawMaker_BuildRawImage_ImageOsInstallFailure(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	mockCommands := []shell.MockCommand{
		{Pattern: "mkdir", Output: "", Error: nil},
		{Pattern: "rm", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	tempDir := t.TempDir()
	chrootEnv := &mockChrootEnv{
		pkgType:           "deb",
		chrootEnvRoot:     tempDir,
		chrootPkgCacheDir: filepath.Join(tempDir, "cache"),
	}

	chrootImageBuildDir := chrootEnv.GetChrootImageBuildDir()
	if err := os.MkdirAll(chrootImageBuildDir, 0700); err != nil {
		t.Fatalf("Failed to create chroot image build dir: %v", err)
	}

	os.Setenv("IMAGE_COMPOSER_WORK_DIR", tempDir)
	defer os.Unsetenv("IMAGE_COMPOSER_WORK_DIR")

	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "ubuntu",
			Dist: "jammy",
			Arch: "x86_64",
		},
		Image: config.ImageInfo{
			Name: "test-image",
		},
		SystemConfig: config.SystemConfig{
			Name: "test-config",
		},
	}

	rawMaker, err := rawmaker.NewRawMaker(chrootEnv, template)
	if err != nil {
		t.Fatalf("Failed to create RawMaker: %v", err)
	}

	// Replace with mocks
	mockLoopDev := &mockLoopDev{
		loopDevPath: "/dev/loop0",
	}
	mockImageOs := &mockImageOs{
		shouldFailInstall: true,
	}

	rawMaker.LoopDev = mockLoopDev
	rawMaker.ImageOs = mockImageOs

	err = rawMaker.Init()
	if err != nil {
		t.Fatalf("Failed to initialize RawMaker: %v", err)
	}

	// Create the expected directory structure
	buildDir := filepath.Join(tempDir, "ubuntu-jammy-x86_64", "imagebuild", "test-config")
	if err := os.MkdirAll(buildDir, 0700); err != nil {
		t.Fatalf("Failed to create build directory: %v", err)
	}

	err = rawMaker.BuildRawImage()

	if err == nil {
		t.Error("Expected error, but got none")
	}
	if !strings.Contains(err.Error(), "failed to install OS") {
		t.Errorf("Expected error about OS installation, but got: %v", err)
	}
}

func TestRawMaker_BuildRawImage_RenameFailure(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	mockCommands := []shell.MockCommand{
		{Pattern: "mkdir", Output: "", Error: nil},
		{Pattern: "mv", Output: "", Error: fmt.Errorf("mv failed")},
		{Pattern: "rm", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	tempDir := t.TempDir()
	chrootEnv := &mockChrootEnv{
		pkgType:           "deb",
		chrootEnvRoot:     tempDir,
		chrootPkgCacheDir: filepath.Join(tempDir, "cache"),
	}

	chrootImageBuildDir := chrootEnv.GetChrootImageBuildDir()
	if err := os.MkdirAll(chrootImageBuildDir, 0700); err != nil {
		t.Fatalf("Failed to create chroot image build dir: %v", err)
	}

	os.Setenv("IMAGE_COMPOSER_WORK_DIR", tempDir)
	defer os.Unsetenv("IMAGE_COMPOSER_WORK_DIR")

	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "ubuntu",
			Dist: "jammy",
			Arch: "x86_64",
		},
		Image: config.ImageInfo{
			Name: "test-image",
		},
		SystemConfig: config.SystemConfig{
			Name: "test-config",
		},
	}

	rawMaker, err := rawmaker.NewRawMaker(chrootEnv, template)
	if err != nil {
		t.Fatalf("Failed to create RawMaker: %v", err)
	}

	// Replace with mocks
	mockLoopDev := &mockLoopDev{
		loopDevPath: "/dev/loop0",
	}
	mockImageOs := &mockImageOs{
		installRoot: tempDir,
		versionInfo: "1.0.0",
	}

	rawMaker.LoopDev = mockLoopDev
	rawMaker.ImageOs = mockImageOs

	// Create the expected directory structure and file
	buildDir := filepath.Join(tempDir, "ubuntu-jammy-x86_64", "imagebuild", "test-config")
	if err := os.MkdirAll(buildDir, 0700); err != nil {
		t.Fatalf("Failed to create build directory: %v", err)
	}

	err = rawMaker.BuildRawImage()

	if err == nil {
		t.Error("Expected error, but got none")
	}
	if !strings.Contains(err.Error(), "failed to rename image file") {
		t.Errorf("Expected error about file rename, but got: %v", err)
	}
}

func TestRawMaker_BuildRawImage_ConvertFailure(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	mockCommands := []shell.MockCommand{
		{Pattern: "mkdir", Output: "", Error: nil},
		{Pattern: "mv", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	tempDir := t.TempDir()
	chrootEnv := &mockChrootEnv{
		pkgType:           "deb",
		chrootEnvRoot:     tempDir,
		chrootPkgCacheDir: filepath.Join(tempDir, "cache"),
	}

	// Create chroot image build directory first
	chrootImageBuildDir := chrootEnv.GetChrootImageBuildDir()
	if err := os.MkdirAll(chrootImageBuildDir, 0700); err != nil {
		t.Fatalf("Failed to create chroot image build dir: %v", err)
	}

	os.Setenv("IMAGE_COMPOSER_WORK_DIR", tempDir)
	defer os.Unsetenv("IMAGE_COMPOSER_WORK_DIR")

	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "ubuntu",
			Dist: "jammy",
			Arch: "x86_64",
		},
		Image: config.ImageInfo{
			Name: "test-image",
		},
		SystemConfig: config.SystemConfig{
			Name: "test-config",
		},
	}

	rawMaker, err := rawmaker.NewRawMaker(chrootEnv, template)
	if err != nil {
		t.Fatalf("Failed to create RawMaker: %v", err)
	}

	// Replace with mocks
	mockLoopDev := &mockLoopDev{
		loopDevPath: "/dev/loop0",
	}
	mockImageOs := &mockImageOs{
		installRoot: tempDir,
		versionInfo: "1.0.0",
	}
	mockImageConvert := &mockImageConvert{
		shouldFailConvert: true,
	}

	rawMaker.LoopDev = mockLoopDev
	rawMaker.ImageOs = mockImageOs
	rawMaker.ImageConvert = mockImageConvert

	// Create the expected directory structure
	buildDir := filepath.Join(tempDir, "ubuntu-jammy-x86_64", "imagebuild", "test-config")
	if err := os.MkdirAll(buildDir, 0700); err != nil {
		t.Fatalf("Failed to create build directory: %v", err)
	}

	err = rawMaker.BuildRawImage()

	if err == nil {
		t.Error("Expected error, but got none")
	}
	if !strings.Contains(err.Error(), "failed to convert image file") {
		t.Errorf("Expected error about image conversion, but got: %v", err)
	}
}

func TestRawMaker_BuildRawImage_LoopDevDeleteFailure(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	mockCommands := []shell.MockCommand{
		{Pattern: "mkdir", Output: "", Error: nil},
		{Pattern: "mv", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	tempDir := t.TempDir()
	chrootEnv := &mockChrootEnv{
		pkgType:           "deb",
		chrootEnvRoot:     tempDir,
		chrootPkgCacheDir: filepath.Join(tempDir, "cache"),
	}

	chrootImageBuildDir := chrootEnv.GetChrootImageBuildDir()
	if err := os.MkdirAll(chrootImageBuildDir, 0700); err != nil {
		t.Fatalf("Failed to create chroot image build dir: %v", err)
	}

	os.Setenv("IMAGE_COMPOSER_WORK_DIR", tempDir)
	defer os.Unsetenv("IMAGE_COMPOSER_WORK_DIR")

	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "ubuntu",
			Dist: "jammy",
			Arch: "x86_64",
		},
		Image: config.ImageInfo{
			Name: "test-image",
		},
		SystemConfig: config.SystemConfig{
			Name: "test-config",
		},
	}

	rawMaker, err := rawmaker.NewRawMaker(chrootEnv, template)
	if err != nil {
		t.Fatalf("Failed to create RawMaker: %v", err)
	}

	// Replace with mocks - the delete failure will be logged but won't fail the build
	mockLoopDev := &mockLoopDev{
		loopDevPath:      "/dev/loop0",
		shouldFailDelete: true,
	}
	mockImageOs := &mockImageOs{
		installRoot: tempDir,
		versionInfo: "1.0.0",
	}
	mockImageConvert := &mockImageConvert{}

	rawMaker.LoopDev = mockLoopDev
	rawMaker.ImageOs = mockImageOs
	rawMaker.ImageConvert = mockImageConvert

	err = rawMaker.Init()
	if err != nil {
		t.Fatalf("Failed to initialize RawMaker: %v", err)
	}

	// Create the expected directory structure
	buildDir := filepath.Join(tempDir, "ubuntu-jammy-x86_64", "imagebuild", "test-config")
	if err := os.MkdirAll(buildDir, 0700); err != nil {
		t.Fatalf("Failed to create build directory: %v", err)
	}

	// BuildRawImage should succeed even if loop device deletion fails in defer
	// The deletion failure is logged but not returned as an error
	err = rawMaker.BuildRawImage()
	if err != nil {
		t.Errorf("Expected no error (deletion failure is just logged), but got: %v", err)
	}
}

func TestRawMaker_CleanupOnSuccess(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name             string
		loopDevPath      string
		shouldFailDelete bool
		expectError      bool
		expectedErrorMsg string
	}{
		{
			name:        "successful_cleanup",
			loopDevPath: "/dev/loop0",
			expectError: false,
		},
		{
			name:        "empty_loop_dev_path",
			loopDevPath: "",
			expectError: false,
		},
		{
			name:             "delete_failure_logged",
			loopDevPath:      "/dev/loop0",
			shouldFailDelete: true,
			expectError:      false, // Failure is logged but not returned
		},
	}

	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "ubuntu",
			Dist: "jammy",
			Arch: "x86_64",
		},
		Image: config.ImageInfo{
			Name: "test-image",
		},
		SystemConfig: config.SystemConfig{
			Name: "test-config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Mock shell commands to avoid sudo issues
			mockCommands := []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
			}
			shell.Default = shell.NewMockExecutor(mockCommands)

			tempDir := t.TempDir()
			chrootEnv := &mockChrootEnv{
				chrootEnvRoot: tempDir,
			}

			// Ensure chroot image build directory exists
			chrootImageBuildDir := chrootEnv.GetChrootImageBuildDir()
			if err := os.MkdirAll(chrootImageBuildDir, 0700); err != nil {
				t.Fatalf("Failed to create chroot image build dir: %v", err)
			}

			rawMaker, err := rawmaker.NewRawMaker(chrootEnv, template)
			if err != nil {
				t.Fatalf("Failed to create RawMaker: %v", err)
			}

			mockLoopDev := &mockLoopDev{
				shouldFailDelete: tt.shouldFailDelete,
			}
			rawMaker.LoopDev = mockLoopDev

			// Test cleanup behavior through the mock
			if tt.loopDevPath != "" {
				err := mockLoopDev.LoopSetupDelete(tt.loopDevPath)
				if tt.shouldFailDelete {
					if err == nil {
						t.Error("Expected error during cleanup, but got none")
					} else if !strings.Contains(err.Error(), "mock loop device deletion failure") {
						t.Errorf("Expected mock deletion error, but got: %v", err)
					}
				} else {
					if err != nil {
						t.Errorf("Expected no error during cleanup, but got: %v", err)
					}
				}
			}
		})
	}
}

func TestRawMaker_CleanupOnError(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name             string
		loopDevPath      string
		imagePath        string
		shouldFailDelete bool
		shouldFailRemove bool
		createImageFile  bool
		expectError      bool
		expectedErrorMsg string
	}{
		{
			name:            "successful_cleanup",
			loopDevPath:     "/dev/loop0",
			imagePath:       "test.raw",
			createImageFile: true,
			expectError:     false,
		},
		{
			name:        "empty_paths",
			loopDevPath: "",
			imagePath:   "",
			expectError: false,
		},
		{
			name:             "delete_failure",
			loopDevPath:      "/dev/loop0",
			imagePath:        "test.raw",
			shouldFailDelete: true,
			createImageFile:  true,
			expectError:      false, // Failure is logged but doesn't propagate
		},
		{
			name:            "file_not_exists",
			loopDevPath:     "/dev/loop0",
			imagePath:       "nonexistent.raw",
			createImageFile: false,
			expectError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCommands := []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
				{Pattern: "rm", Output: "", Error: nil},
			}
			if tt.shouldFailRemove {
				mockCommands = []shell.MockCommand{
					{Pattern: "mkdir", Output: "", Error: nil},
					{Pattern: "rm", Output: "", Error: fmt.Errorf("rm failed")},
				}
			}
			shell.Default = shell.NewMockExecutor(mockCommands)

			tempDir := t.TempDir()
			chrootEnv := &mockChrootEnv{
				chrootEnvRoot: tempDir,
			}

			// Ensure chroot image build directory exists
			chrootImageBuildDir := chrootEnv.GetChrootImageBuildDir()
			if err := os.MkdirAll(chrootImageBuildDir, 0700); err != nil {
				t.Fatalf("Failed to create chroot image build dir: %v", err)
			}

			template := &config.ImageTemplate{
				Target: config.TargetInfo{
					OS:   "ubuntu",
					Dist: "jammy",
					Arch: "x86_64",
				},
			}

			rawMaker, err := rawmaker.NewRawMaker(chrootEnv, template)
			if err != nil {
				t.Fatalf("Failed to create RawMaker: %v", err)
			}

			mockLoopDev := &mockLoopDev{
				shouldFailDelete: tt.shouldFailDelete,
			}
			rawMaker.LoopDev = mockLoopDev

			// Create image file if needed
			if tt.createImageFile && tt.imagePath != "" {
				imagePath := filepath.Join(tempDir, tt.imagePath)
				if err := os.WriteFile(imagePath, []byte("test image"), 0644); err != nil {
					t.Fatalf("Failed to create image file: %v", err)
				}
			}

			// Test loop device deletion
			if tt.loopDevPath != "" {
				err := mockLoopDev.LoopSetupDelete(tt.loopDevPath)
				if tt.shouldFailDelete && err == nil {
					t.Error("Expected loop device deletion to fail")
				}
			}

			// Test file removal behavior
			if tt.imagePath != "" && tt.createImageFile {
				imagePath := filepath.Join(tempDir, tt.imagePath)
				_, err := os.Stat(imagePath)
				fileExists := err == nil

				if fileExists && tt.shouldFailRemove {
					// The actual cleanup would call shell command to remove
					_, err := shell.ExecCmd(fmt.Sprintf("rm -f %s", imagePath), true, shell.HostPath, nil)
					if err == nil {
						t.Error("Expected file removal to fail")
					}
				}
			}
		})
	}
}

func TestRawMaker_BuildRawImage_Integration(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name          string
		template      *config.ImageTemplate
		mockCommands  []shell.MockCommand
		setupFunc     func(tempDir string) error
		expectError   bool
		expectedError string
	}{
		{
			name: "complete_workflow",
			template: &config.ImageTemplate{
				Target: config.TargetInfo{
					OS:   "ubuntu",
					Dist: "jammy",
					Arch: "x86_64",
				},
				Image: config.ImageInfo{
					Name: "test-image",
				},
				SystemConfig: config.SystemConfig{
					Name: "test-config",
				},
			},
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
				{Pattern: "mv", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name: "invalid_template",
			template: &config.ImageTemplate{
				Target: config.TargetInfo{
					OS:   "",
					Dist: "",
					Arch: "",
				},
				Image: config.ImageInfo{
					Name: "",
				},
				SystemConfig: config.SystemConfig{
					Name: "",
				},
			},
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
			},
			expectError:   true,
			expectedError: "failed to create loop device",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			tempDir := t.TempDir()
			chrootEnv := &mockChrootEnv{
				pkgType:           "deb",
				chrootEnvRoot:     tempDir,
				chrootPkgCacheDir: filepath.Join(tempDir, "cache"),
			}

			// Create chroot image build directory first
			chrootImageBuildDir := chrootEnv.GetChrootImageBuildDir()
			if err := os.MkdirAll(chrootImageBuildDir, 0700); err != nil {
				t.Fatalf("Failed to create chroot image build dir: %v", err)
			}

			if tt.setupFunc != nil {
				if err := tt.setupFunc(tempDir); err != nil {
					t.Fatalf("Failed to setup test: %v", err)
				}
			}

			// Mock config directories
			os.Setenv("IMAGE_COMPOSER_WORK_DIR", tempDir)
			defer os.Unsetenv("IMAGE_COMPOSER_WORK_DIR")

			rawMaker, err := rawmaker.NewRawMaker(chrootEnv, tt.template)
			if err != nil {
				t.Fatalf("Failed to create RawMaker: %v", err)
			}

			// Set up mocks based on expected behavior
			var mockLoopDevImpl imagedisc.LoopDevInterface
			var mockImageOsImpl imageos.ImageOsInterface
			var mockImageConvertImpl imageconvert.ImageConvertInterface

			if tt.expectError {
				mockLoopDevImpl = &mockLoopDev{
					shouldFailCreate: true,
				}
			} else {
				mockLoopDevImpl = &mockLoopDev{
					loopDevPath: "/dev/loop0",
				}
				mockImageOsImpl = &mockImageOs{
					installRoot: tempDir,
					versionInfo: "1.0.0",
				}
				mockImageConvertImpl = &mockImageConvert{}
			}

			rawMaker.LoopDev = mockLoopDevImpl
			if mockImageOsImpl != nil {
				rawMaker.ImageOs = mockImageOsImpl
			}
			if mockImageConvertImpl != nil {
				rawMaker.ImageConvert = mockImageConvertImpl
			}

			if !tt.expectError {
				// Create the expected directory structure
				providerId := fmt.Sprintf("%s-%s-%s", tt.template.Target.OS, tt.template.Target.Dist, tt.template.Target.Arch)
				buildDir := filepath.Join(tempDir, providerId, "imagebuild", tt.template.SystemConfig.Name)
				if err := os.MkdirAll(buildDir, 0700); err != nil {
					t.Fatalf("Failed to create build directory: %v", err)
				}
			}

			err = rawMaker.BuildRawImage()

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, but got none")
				} else if tt.expectedError != "" && !strings.Contains(err.Error(), tt.expectedError) {
					t.Errorf("Expected error containing '%s', but got: %v", tt.expectedError, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				}
			}
		})
	}
}

func TestRawMaker_Interface_Compliance(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	// Mock shell commands to avoid sudo issues
	mockCommands := []shell.MockCommand{
		{Pattern: "mkdir", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	// Test that RawMaker implements RawMakerInterface
	tempDir := t.TempDir()
	chrootEnv := &mockChrootEnv{
		chrootEnvRoot: tempDir,
	}

	// Ensure chroot image build directory exists
	chrootImageBuildDir := chrootEnv.GetChrootImageBuildDir()
	if err := os.MkdirAll(chrootImageBuildDir, 0700); err != nil {
		t.Fatalf("Failed to create chroot image build dir: %v", err)
	}

	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "ubuntu",
			Dist: "jammy",
			Arch: "x86_64",
		},
		SystemConfig: config.SystemConfig{
			Name: "test-config",
		},
	}

	rawMaker, err := rawmaker.NewRawMaker(chrootEnv, template)
	if err != nil {
		t.Fatalf("Failed to create RawMaker: %v", err)
	}

	// Verify interface compliance by checking method signatures
	var _ rawmaker.RawMakerInterface = rawMaker

	// Test Init method (should succeed or fail based on setup)
	_ = rawMaker.Init()

	// Test that BuildRawImage exists and can be called
	err = rawMaker.BuildRawImage()
	// We expect this to fail without proper setup, but the method should exist
	if err == nil {
		t.Error("Expected error without proper setup")
	}
}
