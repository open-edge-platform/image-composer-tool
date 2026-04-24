package initrdmaker_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/chroot"
	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/image/initrdmaker"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

// Custom mock executor that creates files when cpio commands are executed
type CustomMockExecutorWithFileCreation struct {
	*shell.MockExecutor
	ImageBuildDir string
}

func (c *CustomMockExecutorWithFileCreation) ExecCmdWithStream(cmdStr string, sudo bool, chrootPath string, envVal []string) (string, error) {
	// If this is a cpio command, create the expected output file
	if strings.Contains(cmdStr, "cpio") && strings.Contains(cmdStr, "gzip") {
		// Extract output file from command: "cd ... && sudo find . | sudo cpio -o -H newc | sudo gzip > /path/to/file"
		parts := strings.Split(cmdStr, " > ")
		if len(parts) > 1 {
			outputFile := strings.TrimSpace(parts[1])
			// Create the file to simulate successful cpio command
			if err := os.MkdirAll(filepath.Dir(outputFile), 0700); err != nil {
				return "", err
			}
			if err := os.WriteFile(outputFile, []byte("mock initrd content"), 0644); err != nil {
				return "", err
			}
		}
	}

	// Call the parent's ExecCmdWithStream
	return c.MockExecutor.ExecCmdWithStream(cmdStr, sudo, chrootPath, envVal)
}

// Mock implementations for testing
type mockChrootEnv struct {
	pkgType             string
	chrootPkgCacheDir   string
	chrootImageBuildDir string
	err                 error
	chrootEnvRoot       string // Add field for chroot env root
}

func NewMockChrootEnv(pkgType, tempDir string, err error) *mockChrootEnv {
	chrootEnvRoot := filepath.Join(tempDir, "mock-chroot-env-root")
	chrootPkgCacheDir := filepath.Join(chrootEnvRoot, "mock-chroot-pkg-cache-dir")
	chrootImageBuildDir := filepath.Join(chrootEnvRoot, "workspace", "imagebuild")
	if err := os.MkdirAll(chrootPkgCacheDir, 0700); err != nil {
		panic(fmt.Errorf("failed to create mock chroot pkg cache dir: %v", err))
	}
	if err := os.MkdirAll(chrootImageBuildDir, 0700); err != nil {
		panic(fmt.Errorf("failed to create mock chroot image build dir: %v", err))
	}

	return &mockChrootEnv{
		pkgType:             pkgType,
		chrootPkgCacheDir:   chrootPkgCacheDir,
		chrootImageBuildDir: chrootImageBuildDir,
		err:                 err,
		chrootEnvRoot:       chrootEnvRoot,
	}
}

func (m *mockChrootEnv) GetChrootEnvRoot() string {
	return m.chrootEnvRoot
}

func (m *mockChrootEnv) GetChrootImageBuildDir() string {
	return m.chrootImageBuildDir
}

func (m *mockChrootEnv) GetTargetOsPkgType() string {
	return m.pkgType
}

func (m *mockChrootEnv) GetTargetOsConfigDir() string {
	// Mock implementation: return a default config dir
	return filepath.Join(m.chrootEnvRoot, "config")
}

func (m *mockChrootEnv) GetTargetOsReleaseVersion() string {
	// Mock implementation: return a default release version
	return "3.0"
}

func (m *mockChrootEnv) GetChrootPkgCacheDir() string {
	return m.chrootPkgCacheDir
}

func (m *mockChrootEnv) GetChrootEnvEssentialPackageList() ([]string, error) {
	// Mock implementation: return a sample list
	return []string{"essential-pkg1", "essential-pkg2"}, m.err
}

func (m *mockChrootEnv) GetChrootEnvHostPath(chrootPath string) (string, error) {
	// Mock implementation: return the host path or a default value
	return filepath.Join(m.chrootEnvRoot, chrootPath), m.err
}

func (m *mockChrootEnv) GetChrootEnvPath(ChrootEnvHostPath string) (string, error) {
	// Mock implementation: return the chroot env path or a default value
	return ChrootEnvHostPath[len(m.chrootEnvRoot):], m.err
}

func (m *mockChrootEnv) MountChrootSysfs(chrootPath string) error {
	return m.err
}

func (m *mockChrootEnv) UmountChrootSysfs(chrootPath string) error {
	return m.err
}

func (m *mockChrootEnv) MountChrootPath(hostFullPath, chrootPath, mountFlags string) error {
	return m.err
}

func (m *mockChrootEnv) UmountChrootPath(chrootPath string) error {
	return m.err
}

func (m *mockChrootEnv) CopyFileFromHostToChroot(hostFilePath, chrootPath string) error {
	return m.err
}

func (m *mockChrootEnv) CopyFileFromChrootToHost(hostFilePath, chrootPath string) error {
	return m.err
}

func (m *mockChrootEnv) UpdateChrootLocalRepoMetadata(chrootRepoDir string, targetArch string, sudo bool) error {
	return m.err
}

func (m *mockChrootEnv) RefreshLocalCacheRepo() error {
	return m.err
}

func (m *mockChrootEnv) InitChrootEnv(targetOs, targetDist, targetArch string) error {
	return m.err
}

func (m *mockChrootEnv) CleanupChrootEnv(targetOs, targetDist, targetArch string) error {
	return m.err
}

func (m *mockChrootEnv) TdnfInstallPackage(packageName, installRoot string, repositoryIDList []string) error {
	return m.err
}

func (m *mockChrootEnv) AptInstallPackage(packageName, installRoot string, repoSrcList []string) error {
	return m.err
}

func (m *mockChrootEnv) UpdateSystemPkgs(template *config.ImageTemplate) error {
	return m.err
}

type mockImageOs struct {
	tempDir     string
	installRoot string
	err         error
	versionInfo string
}

func NewMockImageOs(tempDir, versionInfo string, err error) *mockImageOs {
	installRoot := filepath.Join(tempDir, "install-root")
	if err := os.MkdirAll(installRoot, 0700); err != nil {
		panic(fmt.Errorf("failed to create mock install root dir: %v", err))
	}

	return &mockImageOs{
		tempDir:     tempDir,
		installRoot: installRoot,
		err:         err,
		versionInfo: versionInfo,
	}
}

func (m *mockImageOs) GetInstallRoot() string {
	return m.installRoot
}

func (m *mockImageOs) InstallInitrd() (string, string, error) {
	return m.installRoot, m.versionInfo, m.err
}

func (m *mockImageOs) InstallImageOs(diskPathIdMap map[string]string) (versionInfo string, err error) {
	return m.versionInfo, m.err
}

func TestNewInitrdMaker(t *testing.T) {
	tests := []struct {
		name        string
		chrootEnv   chroot.ChrootEnvInterface
		expectError bool
	}{
		{
			name:        "successful_creation",
			chrootEnv:   NewMockChrootEnv("deb", t.TempDir(), nil),
			expectError: false,
		},
		{
			name:        "nil_chroot_env",
			chrootEnv:   nil,
			expectError: true,
		},
	}

	mockCommands := []shell.MockCommand{
		{Pattern: "mkdir", Output: "", Error: nil},
	}

	shell.Default = shell.NewMockExecutor(mockCommands)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			template := &config.ImageTemplate{
				Image: config.ImageInfo{
					Name: "test-image",
				},
			}
			initrdMaker, err := initrdmaker.NewInitrdMaker(tt.chrootEnv, template)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				}
				if initrdMaker == nil {
					t.Error("Expected non-nil InitrdMaker")
				} else {
					if tt.chrootEnv != nil {
						if initrdMaker.ChrootEnv != tt.chrootEnv {
							t.Error("Expected ChrootEnv to be set correctly")
						}
					}
				}
			}
		})
	}
}

func TestInitrdMaker_Init(t *testing.T) {
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
			},
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:     "nil_template",
			template: nil,
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
			},
			expectError:   true,
			expectedError: "image template cannot be nil",
		},
		{
			name: "mkdir_failure",
			template: &config.ImageTemplate{
				Target: config.TargetInfo{
					OS:   "ubuntu",
					Dist: "jammy",
					Arch: "x86_64",
				},
			},
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: fmt.Errorf("mkdir failed")},
			},
			expectError:   true,
			expectedError: "failed to create directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			tempDir := t.TempDir()
			chrootEnv := NewMockChrootEnv("deb", tempDir, nil)

			if tt.setupFunc != nil {
				if err := tt.setupFunc(tempDir); err != nil {
					t.Fatalf("Failed to setup test: %v", err)
				}
			}

			currentConfig := config.Global()
			currentConfig.WorkDir = tempDir
			config.SetGlobal(currentConfig)

			initrdMaker, err := initrdmaker.NewInitrdMaker(chrootEnv, tt.template)

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
				if initrdMaker.ImageOs == nil {
					t.Error("Expected ImageOs to be initialized")
				}
			}
		})
	}
}

func TestInitrdMaker_DownloadInitrdPkgs(t *testing.T) {
	tests := []struct {
		name          string
		pkgType       string
		template      *config.ImageTemplate
		err           error
		expectError   bool
		expectedError string
	}{
		{
			name:    "unsupported_pkg_type",
			pkgType: "unknown",
			template: &config.ImageTemplate{
				Target: config.TargetInfo{
					Arch: "x86_64",
				},
				SystemConfig: config.SystemConfig{
					Packages: []string{"package1"},
				},
			},
			expectError: false, // Function doesn't fail for unknown pkg types
		},
	}

	mockCommands := []shell.MockCommand{
		{Pattern: "mkdir", Output: "", Error: nil},
	}

	shell.Default = shell.NewMockExecutor(mockCommands)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			chrootEnv := NewMockChrootEnv(tt.pkgType, tempDir, tt.err)

			initrdMaker, err := initrdmaker.NewInitrdMaker(chrootEnv, tt.template)
			if err != nil {
				t.Fatalf("Failed to create InitrdMaker: %v", err)
			}

			err = initrdMaker.DownloadInitrdPkgs()

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

func TestInitrdMaker_BuildInitrdImage(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tempDir := t.TempDir()

	tests := []struct {
		name          string
		template      *config.ImageTemplate
		mockCommands  []shell.MockCommand
		setupFunc     func(tempDir string) error
		imageOs       *mockImageOs
		expectError   bool
		expectedError string
	}{
		{
			name: "successful_build",
			template: &config.ImageTemplate{
				Image: config.ImageInfo{
					Name: "test-image",
				},
				SystemConfig: config.SystemConfig{
					Name: "test-config",
				},
			},
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
				{Pattern: "mount", Output: "", Error: nil},
				{Pattern: "ls", Output: "vmlinuz", Error: nil},
				{Pattern: "cp", Output: "", Error: nil},
				{Pattern: "cd.*cpio.*gzip", Output: "", Error: nil},
			},
			setupFunc: func(tempDir string) error {
				// Create general config directory and rc.local file
				generalConfigDir := filepath.Join(tempDir, "config", "general")
				isolinuxDir := filepath.Join(generalConfigDir, "isolinux")
				imageBuildDir := filepath.Join(tempDir, "install-root")
				if err := os.MkdirAll(isolinuxDir, 0700); err != nil {
					return err
				}
				rcLocalPath := filepath.Join(isolinuxDir, "rc.local")
				if err := os.WriteFile(rcLocalPath, []byte("#!/bin/bash\necho 'init script'"), 0755); err != nil {
					return err
				}
				bootDir := filepath.Join(imageBuildDir, "boot")
				if err := os.MkdirAll(bootDir, 0700); err != nil {
					return err
				}
				vmlinuzPath := filepath.Join(bootDir, "vmlinuz")
				return os.WriteFile(vmlinuzPath, []byte("mock kernel"), 0755)
			},
			imageOs:     NewMockImageOs(tempDir, "1.0.0", nil),
			expectError: false,
		},
		{
			name: "install_initrd_failure",
			template: &config.ImageTemplate{
				Image: config.ImageInfo{
					Name: "test-image",
				},
				SystemConfig: config.SystemConfig{
					Name: "test-config",
				},
			},
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
			},
			imageOs:       NewMockImageOs(tempDir, "1.0.0", fmt.Errorf("failed to install initrd")),
			expectError:   true,
			expectedError: "failed to install initrd",
		},
		{
			name: "missing_rc_local",
			template: &config.ImageTemplate{
				Image: config.ImageInfo{
					Name: "test-image",
				},
				SystemConfig: config.SystemConfig{
					Name: "test-config",
				},
			},
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
			},
			setupFunc: func(tempDir string) error {
				generalConfigDir := filepath.Join(tempDir, "config", "general")
				isolinuxDir := filepath.Join(generalConfigDir, "isolinux")
				if err := os.MkdirAll(isolinuxDir, 0700); err != nil {
					return err
				}
				rcLocalPath := filepath.Join(isolinuxDir, "rc.local")
				if err := os.Remove(rcLocalPath); err != nil && !os.IsNotExist(err) {
					return err
				}
				return nil
			},
			imageOs:       NewMockImageOs(tempDir, "1.0.0", nil),
			expectError:   true,
			expectedError: "failed to add init scripts to initrd",
		},
		{
			name: "cpio_command_failure",
			template: &config.ImageTemplate{
				Image: config.ImageInfo{
					Name: "test-image",
				},
				SystemConfig: config.SystemConfig{
					Name: "test-config",
				},
			},
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
				{Pattern: "ls", Output: "vmlinuz", Error: nil},
				{Pattern: "cd.*cpio.*gzip", Output: "", Error: fmt.Errorf("cpio failed")},
				{Pattern: "cp", Output: "", Error: nil},
			},
			setupFunc: func(tempDir string) error {
				// Create general config directory and rc.local file
				generalConfigDir := filepath.Join(tempDir, "config", "general")
				isolinuxDir := filepath.Join(generalConfigDir, "isolinux")
				imageBuildDir := filepath.Join(tempDir, "install-root")
				if err := os.MkdirAll(isolinuxDir, 0700); err != nil {
					return err
				}
				rcLocalPath := filepath.Join(isolinuxDir, "rc.local")
				if err := os.WriteFile(rcLocalPath, []byte("#!/bin/bash\necho 'init script'"), 0755); err != nil {
					return err
				}
				bootDir := filepath.Join(imageBuildDir, "boot")
				if err := os.MkdirAll(bootDir, 0700); err != nil {
					return err
				}
				vmlinuzPath := filepath.Join(bootDir, "vmlinuz")
				return os.WriteFile(vmlinuzPath, []byte("mock kernel"), 0755)
			},
			imageOs:       NewMockImageOs(tempDir, "1.0.0", nil),
			expectError:   true,
			expectedError: "failed to create initrd image",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			chrootEnv := NewMockChrootEnv("deb", tempDir, nil)

			if tt.setupFunc != nil {
				if err := tt.setupFunc(tempDir); err != nil {
					t.Fatalf("Failed to setup test: %v", err)
				}
			}

			// Mock config directories
			currentConfig := config.Global()
			currentConfig.WorkDir = tempDir
			currentConfig.ConfigDir = filepath.Join(tempDir, "config")
			config.SetGlobal(currentConfig)

			initrdMaker, err := initrdmaker.NewInitrdMaker(chrootEnv, tt.template)
			if err != nil {
				t.Fatalf("Failed to create InitrdMaker: %v", err)
			}

			// Replace with mock ImageOs if provided
			if tt.imageOs != nil {
				initrdMaker.ImageOs = tt.imageOs
			}

			// Set up the image build directory
			initrdMaker.ImageBuildDir = filepath.Join(tempDir, "imagebuild")
			if err := os.MkdirAll(initrdMaker.ImageBuildDir, 0700); err != nil {
				t.Fatalf("Failed to create image build directory: %v", err)
			}

			// Create initrd rootfs directory structure if imageOs is set
			if tt.imageOs != nil && tt.imageOs.installRoot != "" {
				initrdRootfs := tt.imageOs.installRoot
				etcRcDir := filepath.Join(initrdRootfs, "etc", "rc.d")
				if err := os.MkdirAll(etcRcDir, 0700); err != nil {
					t.Fatalf("Failed to create initrd rootfs structure: %v", err)
				}
			}

			// For successful build test, use a custom mock executor that creates the file
			if tt.name == "successful_build" {
				customMockExecutor := &CustomMockExecutorWithFileCreation{
					MockExecutor:  shell.NewMockExecutor(tt.mockCommands),
					ImageBuildDir: initrdMaker.ImageBuildDir,
				}
				shell.Default = customMockExecutor
			}

			err = initrdMaker.BuildInitrdImage()

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
				if initrdMaker.GetInitrdFilePath() == "" {
					t.Error("Expected InitrdFilePath to be set")
				}
				if initrdMaker.GetInitrdRootfsPath() == "" {
					t.Error("Expected InitrdRootfsPath to be set")
				}
				if initrdMaker.GetInitrdVersion() == "" {
					t.Error("Expected InitrdVersion to be set")
				}
			}
		})
	}
}

func TestInitrdMaker_CleanInitrdRootfs(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name          string
		setupFunc     func(*initrdmaker.InitrdMaker, string) error
		mockCommands  []shell.MockCommand
		expectError   bool
		expectedError string
	}{
		{
			name: "successful_cleanup",
			setupFunc: func(im *initrdmaker.InitrdMaker, tempDir string) error {
				rootfsPath := filepath.Join(tempDir, "initrd-rootfs")
				if err := os.MkdirAll(rootfsPath, 0700); err != nil {
					return err
				}
				im.InitrdRootfsPath = rootfsPath
				return nil
			},
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
				{Pattern: "mount", Output: "", Error: nil},
				{Pattern: "rm -rf", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name: "empty_rootfs_path",
			setupFunc: func(im *initrdmaker.InitrdMaker, tempDir string) error {
				im.InitrdRootfsPath = ""
				return nil
			},
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name: "nonexistent_rootfs_path",
			setupFunc: func(im *initrdmaker.InitrdMaker, tempDir string) error {
				im.InitrdRootfsPath = filepath.Join(tempDir, "nonexistent")
				return nil
			},
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name: "umount_failure",
			setupFunc: func(im *initrdmaker.InitrdMaker, tempDir string) error {
				rootfsPath := filepath.Join(tempDir, "initrd-rootfs")
				if err := os.MkdirAll(rootfsPath, 0700); err != nil {
					return err
				}
				im.InitrdRootfsPath = rootfsPath
				return nil
			},
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
				{Pattern: "mount", Output: "", Error: fmt.Errorf("umount failed")},
			},
			expectError:   true,
			expectedError: "umount failed",
		},
		{
			name: "rm_failure",
			setupFunc: func(im *initrdmaker.InitrdMaker, tempDir string) error {
				rootfsPath := filepath.Join(tempDir, "initrd-rootfs")
				if err := os.MkdirAll(rootfsPath, 0700); err != nil {
					return err
				}
				im.InitrdRootfsPath = rootfsPath
				return nil
			},
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
				{Pattern: "mount", Output: "", Error: nil},
				{Pattern: "rm -rf", Output: "", Error: fmt.Errorf("rm failed")},
			},
			expectError:   true,
			expectedError: "failed to remove initrd rootfs directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			tempDir := t.TempDir()
			chrootEnv := NewMockChrootEnv("deb", tempDir, nil)
			template := &config.ImageTemplate{
				Image: config.ImageInfo{
					Name: "test-image",
				},
			}

			initrdMaker, err := initrdmaker.NewInitrdMaker(chrootEnv, template)
			if err != nil {
				t.Fatalf("Failed to create InitrdMaker: %v", err)
			}

			if tt.setupFunc != nil {
				if err := tt.setupFunc(initrdMaker, tempDir); err != nil {
					t.Fatalf("Failed to setup test: %v", err)
				}
			}

			err = initrdMaker.CleanInitrdRootfs()

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

func TestInitrdMaker_GetterMethods(t *testing.T) {
	tempDir := t.TempDir()
	chrootEnv := NewMockChrootEnv("deb", tempDir, nil)
	mockCommands := []shell.MockCommand{
		{Pattern: "mkdir", Output: "", Error: nil},
	}

	shell.Default = shell.NewMockExecutor(mockCommands)
	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
	}
	initrdMaker, err := initrdmaker.NewInitrdMaker(chrootEnv, template)
	if err != nil {
		t.Fatalf("Failed to create InitrdMaker: %v", err)
	}

	// Test initial state
	if initrdMaker.GetInitrdVersion() != "" {
		t.Error("Expected empty version initially")
	}
	if initrdMaker.GetInitrdFilePath() != "" {
		t.Error("Expected empty file path initially")
	}
	if initrdMaker.GetInitrdRootfsPath() != "" {
		t.Error("Expected empty rootfs path initially")
	}

	// Set values and test
	initrdMaker.VersionInfo = "1.0.0"
	initrdMaker.InitrdFilePath = "/path/to/initrd.img"
	initrdMaker.InitrdRootfsPath = "/path/to/rootfs"

	if initrdMaker.GetInitrdVersion() != "1.0.0" {
		t.Errorf("Expected version '1.0.0', got '%s'", initrdMaker.GetInitrdVersion())
	}
	if initrdMaker.GetInitrdFilePath() != "/path/to/initrd.img" {
		t.Errorf("Expected file path '/path/to/initrd.img', got '%s'", initrdMaker.GetInitrdFilePath())
	}
	if initrdMaker.GetInitrdRootfsPath() != "/path/to/rootfs" {
		t.Errorf("Expected rootfs path '/path/to/rootfs', got '%s'", initrdMaker.GetInitrdRootfsPath())
	}
}

func TestInitrdMaker_ErrorHandling(t *testing.T) {
	tests := []struct {
		name         string
		setupFunc    func() (*initrdmaker.InitrdMaker, *config.ImageTemplate, error)
		expectError  bool
		errorMessage string
	}{
		{
			name: "nil_template_init",
			setupFunc: func() (*initrdmaker.InitrdMaker, *config.ImageTemplate, error) {
				tempDir := t.TempDir()
				chrootEnv := NewMockChrootEnv("deb", tempDir, nil)
				chrootImageBuildDir := chrootEnv.GetChrootImageBuildDir()
				if err := os.MkdirAll(chrootImageBuildDir, 0700); err != nil {
					t.Fatalf("Failed to create chroot image build dir: %v", err)
				}
				initrdMaker, err := initrdmaker.NewInitrdMaker(chrootEnv, nil)
				return initrdMaker, nil, err
			},
			expectError:  true,
			errorMessage: "image template cannot be nil",
		},
	}

	mockCommands := []shell.MockCommand{
		{Pattern: "mkdir", Output: "", Error: nil},
	}

	shell.Default = shell.NewMockExecutor(mockCommands)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := tt.setupFunc()

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, but got none")
				} else if tt.errorMessage != "" && !strings.Contains(err.Error(), tt.errorMessage) {
					t.Errorf("Expected error containing '%s', but got: %v", tt.errorMessage, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				}
			}
		})
	}
}
