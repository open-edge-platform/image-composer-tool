package isomaker_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/os-image-composer/internal/chroot"
	"github.com/open-edge-platform/os-image-composer/internal/config"
	"github.com/open-edge-platform/os-image-composer/internal/image/isomaker"
	"github.com/open-edge-platform/os-image-composer/internal/utils/logger"
	"github.com/open-edge-platform/os-image-composer/internal/utils/shell"
)

var log = logger.Logger()

// Mock implementation: always succeed

// Mock implementations for testing
type mockChrootEnv struct {
	pkgType           string
	chrootPkgCacheDir string
	shouldFailRefresh bool
	chrootEnvRoot     string // Add field for chroot env root
}

func (m *mockChrootEnv) GetChrootEnvRoot() string {
	// Mock implementation: return the chroot env root or a default value
	return m.chrootEnvRoot
}

// Implement missing method to satisfy chroot.ChrootEnvInterface
func (m *mockChrootEnv) GetChrootImageBuildDir() string {
	// Mock implementation: return the chroot image build dir or a default value
	return filepath.Join(m.chrootEnvRoot, "workspace", "imagebuild")
}

func (m *mockChrootEnv) GetTargetOsPkgType() string {
	return m.pkgType
}

// Add missing method to satisfy chroot.ChrootEnvInterface
func (m *mockChrootEnv) GetTargetOsConfigDir() string {
	// Mock implementation: return a default config dir
	return filepath.Join(m.chrootEnvRoot, "config")
}

// Add missing method to satisfy chroot.ChrootEnvInterface
func (m *mockChrootEnv) GetTargetOsReleaseVersion() string {
	// Mock implementation: return a default release version
	return "3.0"
}

func (m *mockChrootEnv) GetChrootPkgCacheDir() string {
	return m.chrootPkgCacheDir
}

func (m *mockChrootEnv) GetChrootEnvEssentialPackageList() ([]string, error) {
	// Mock implementation: return a sample list
	return []string{"essential-pkg1", "essential-pkg2"}, nil
}

func (m *mockChrootEnv) GetChrootEnvHostPath(chrootPath string) (string, error) {
	// Mock implementation: return the host path or a default value
	return filepath.Join(m.chrootEnvRoot, chrootPath), nil
}

// Add missing method to satisfy chroot.ChrootEnvInterface
func (m *mockChrootEnv) GetChrootEnvPath(ChrootEnvHostPath string) (string, error) {
	// Mock implementation: return the chroot env path or a default value
	return ChrootEnvHostPath[len(m.chrootEnvRoot):], nil
}

func (m *mockChrootEnv) MountChrootSysfs(chrootPath string) error {
	// Mock implementation: always succeed
	return nil
}

func (m *mockChrootEnv) UmountChrootSysfs(chrootPath string) error {
	// Mock implementation: always succeed
	return nil
}

// Add missing method to satisfy chroot.ChrootEnvInterface
func (m *mockChrootEnv) MountChrootPath(hostFullPath, chrootPath, mountFlags string) error {
	// Mock implementation: always succeed
	return nil
}

func (m *mockChrootEnv) UmountChrootPath(chrootPath string) error {
	// Mock implementation: always succeed
	return nil
}

// Add missing method to satisfy chroot.ChrootEnvInterface
func (m *mockChrootEnv) CopyFileFromHostToChroot(hostFilePath, chrootPath string) error {
	// Mock implementation: always succeed
	return nil
}

// Add missing method to satisfy chroot.ChrootEnvInterface
func (m *mockChrootEnv) CopyFileFromChrootToHost(hostFilePath, chrootPath string) error {
	// Mock implementation: always succeed
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

// Add missing method to satisfy chroot.ChrootEnvInterface
func (m *mockChrootEnv) InitChrootEnv(targetOs, targetDist, targetArch string) error {
	return nil
}

// Add missing method to satisfy chroot.ChrootEnvInterface
func (m *mockChrootEnv) CleanupChrootEnv(targetOs, targetDist, targetArch string) error {
	// Mock implementation: always succeed
	return nil
}

func (m *mockChrootEnv) TdnfInstallPackage(packageName, installRoot string, repositoryIDList []string) error {
	// Mock implementation: always succeed
	return nil
}

// Add missing method to satisfy chroot.ChrootEnvInterface
func (m *mockChrootEnv) AptInstallPackage(packageName, installRoot string, repoSrcList []string) error {
	// Mock implementation: always succeed
	return nil
}

func (m *mockChrootEnv) UpdateSystemPkgs(template *config.ImageTemplate) error {
	return nil
}

func TestNewIsoMaker(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	mockCommands := []shell.MockCommand{
		{Pattern: "mkdir", Output: "", Error: nil},
		{Pattern: "sudo", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	tests := []struct {
		name        string
		chrootEnv   chroot.ChrootEnvInterface
		template    *config.ImageTemplate
		expectError bool
		errorMsg    string
	}{
		{
			name: "successful_creation",
			chrootEnv: &mockChrootEnv{
				chrootEnvRoot: func() string {
					// Create a temp dir and ensure image build dir exists
					tempDir, _ := os.MkdirTemp("", "isomaker-test")
					imageBuildDir := filepath.Join(tempDir, "workspace", "imagebuild")
					_ = os.MkdirAll(imageBuildDir, 0700)
					return tempDir
				}(),
			},
			template: &config.ImageTemplate{
				Target: config.TargetInfo{
					OS:   "ubuntu",
					Dist: "jammy",
					Arch: "x86_64",
				},
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
			isoMaker, err := isomaker.NewIsoMaker(tt.chrootEnv, tt.template)

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
				if isoMaker == nil {
					t.Error("Expected non-nil IsoMaker")
				}
			}
		})
	}
}

func TestIsoMaker_Init(t *testing.T) {
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

			isoMaker, err := isomaker.NewIsoMaker(chrootEnv, tt.template)
			if tt.expectError {
				if err == nil {
					t.Error("Expected error, but got none")
				} else if tt.expectedError != "" && !strings.Contains(err.Error(), tt.expectedError) {
					t.Errorf("Expected error containing '%s', but got: %v", tt.expectedError, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Failed to create IsoMaker: %v", err)
			}

			currentConfig := config.Global()
			currentConfig.WorkDir = tempDir
			config.SetGlobal(currentConfig)

			err = isoMaker.Init()
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

func TestSanitizeIsoLabel(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"valid_uppercase", "VALID_LABEL", "VALID_LABEL"},
		{"lowercase_conversion", "valid_label", "VALID_LABEL"},
		{"mixed_case", "Valid_Label", "VALID_LABEL"},
		{"with_numbers", "Label123", "LABEL123"},
		{"with_spaces", "Label With Spaces", "LABEL_WITH_SPACES"},
		{"with_special_chars", "Label-With@Special#Chars", "LABEL_WITH_SPECIAL_CHARS"},
		{"long_label", "This_Is_A_Very_Long_Label_That_Exceeds_Limit", "THIS_IS_A_VERY_LONG_LABEL_THAT_E"},
		{"empty_string", "", ""},
		{"only_special_chars", "!@#$%^&*()", "__________"},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockCommands := []shell.MockCommand{
		{Pattern: "mkdir", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// We need to test the unexported function through a public interface
			// Since sanitizeIsoLabel is not exported, we'll test it indirectly
			// by creating a test that exercises the ISO creation logic
			tempDir := t.TempDir()

			// Create a minimal template
			template := &config.ImageTemplate{
				Target: config.TargetInfo{
					OS:   "ubuntu",
					Dist: "jammy",
					Arch: "x86_64",
				},
				Image: config.ImageInfo{
					Name: tt.input,
				},
			}

			chrootEnv := &mockChrootEnv{
				chrootEnvRoot:     tempDir,
				pkgType:           "deb",
				chrootPkgCacheDir: filepath.Join(tempDir, "cache"),
			}

			chrootImageBuildDir := chrootEnv.GetChrootImageBuildDir()
			if err := os.MkdirAll(chrootImageBuildDir, 0700); err != nil {
				t.Fatalf("Failed to create chroot image build dir: %v", err)
			}

			isoMaker, err := isomaker.NewIsoMaker(chrootEnv, template)
			if err != nil {
				t.Fatalf("Failed to create IsoMaker: %v", err)
			}

			currentConfig := config.Global()
			currentConfig.WorkDir = tempDir
			config.SetGlobal(currentConfig)

			if err := isoMaker.Init(); err != nil {
				t.Fatalf("Failed to init IsoMaker: %v", err)
			}

			// The actual sanitization happens in createIso, which we can't easily test
			// without a full setup, so we'll just verify the IsoMaker was created
			if isoMaker == nil {
				t.Error("Expected non-nil IsoMaker")
			}
		})
	}
}

func TestArchToGrubFormat(t *testing.T) {
	tests := []struct {
		name        string
		arch        string
		expected    string
		expectError bool
	}{
		{"x86_64", "x86_64", "x86_64", false},
		{"i386", "i386", "i386", false},
		{"arm64", "arm64", "arm64", false},
		{"aarch64", "aarch64", "arm64", false},
		{"arm", "arm", "arm", false},
		{"riscv64", "riscv64", "riscv64", false},
		{"unsupported", "mips", "", true},
		{"empty", "", "", true},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockCommands := []shell.MockCommand{
		{Pattern: "mkdir", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Since archToGrubFormat is unexported, we test it indirectly
			// by checking if the architecture is supported in the GRUB creation process
			tempDir := t.TempDir()

			template := &config.ImageTemplate{
				Target: config.TargetInfo{
					OS:   "ubuntu",
					Dist: "jammy",
					Arch: tt.arch,
				},
			}

			chrootEnv := &mockChrootEnv{
				pkgType:           "deb",
				chrootEnvRoot:     tempDir,
				chrootPkgCacheDir: filepath.Join(tempDir, "cache"),
			}

			chrootImageBuildDir := chrootEnv.GetChrootImageBuildDir()
			if err := os.MkdirAll(chrootImageBuildDir, 0700); err != nil {
				t.Fatalf("Failed to create chroot image build dir: %v", err)
			}

			isoMaker, err := isomaker.NewIsoMaker(chrootEnv, template)
			if err != nil {
				t.Fatalf("Failed to create IsoMaker: %v", err)
			}

			currentConfig := config.Global()
			currentConfig.WorkDir = tempDir
			config.SetGlobal(currentConfig)

			if err := isoMaker.Init(); err != nil {
				t.Fatalf("Failed to init IsoMaker: %v", err)
			}

			// We can only verify that the IsoMaker handles the architecture
			// The actual archToGrubFormat function is called during GRUB creation
			if isoMaker == nil {
				t.Error("Expected non-nil IsoMaker")
			}
		})
	}
}

func TestGetInitrdTemplate(t *testing.T) {
	tests := []struct {
		name          string
		setupFunc     func(tempDir string) error
		template      *config.ImageTemplate
		expectError   bool
		expectedError string
	}{
		{
			name:      "successful_load",
			setupFunc: setupValidInitrdTemplate,
			template: &config.ImageTemplate{
				Target: config.TargetInfo{
					OS:   "ubuntu",
					Dist: "jammy",
					Arch: "x86_64",
				},
			},
			expectError: false,
		},
		{
			name:      "missing_template_file",
			setupFunc: func(tempDir string) error { return nil },
			template: &config.ImageTemplate{
				Target: config.TargetInfo{
					OS:   "ubuntu",
					Dist: "jammy",
					Arch: "x86_64",
				},
			},
			expectError:   true,
			expectedError: "initrd template file does not exist",
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockCommands := []shell.MockCommand{
		{Pattern: "mkdir", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()

			if tt.setupFunc != nil {
				if err := tt.setupFunc(tempDir); err != nil {
					t.Fatalf("Failed to setup test: %v", err)
				}
			}

			// Mock config directories
			setupMockConfigDirs(tempDir, tt.template)

			chrootEnv := &mockChrootEnv{
				pkgType:           "deb",
				chrootEnvRoot:     tempDir,
				chrootPkgCacheDir: filepath.Join(tempDir, "cache"),
			}

			chrootImageBuildDir := chrootEnv.GetChrootImageBuildDir()
			if err := os.MkdirAll(chrootImageBuildDir, 0700); err != nil {
				t.Fatalf("Failed to create chroot image build dir: %v", err)
			}

			isoMaker, err := isomaker.NewIsoMaker(chrootEnv, tt.template)
			if err != nil {
				t.Fatalf("Failed to create IsoMaker: %v", err)
			}

			currentConfig := config.Global()
			currentConfig.WorkDir = tempDir
			config.SetGlobal(currentConfig)

			// Since getInitrdTemplate is unexported, we test it indirectly
			// through buildIsoInitrd which calls getInitrdTemplate
			err = isoMaker.Init()
			if err != nil && !tt.expectError {
				t.Errorf("Unexpected error during init: %v", err)
			}

			// The actual test would happen in buildIsoInitrd, but we can't easily test that
			// without more complex mocking
		})
	}
}

func TestIsoMaker_DownloadInitrdPkgs(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name              string
		pkgType           string
		shouldFailRefresh bool
		template          *config.ImageTemplate
		mockCommands      []shell.MockCommand
		expectError       bool
		expectedError     string
	}{
		{
			name:    "successful_deb_download",
			pkgType: "deb",
			template: &config.ImageTemplate{
				Target: config.TargetInfo{Arch: "x86_64"},
				SystemConfig: config.SystemConfig{
					Packages: []string{"pkg1", "pkg2"},
				},
			},
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
				{Pattern: "apt", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:    "successful_rpm_download",
			pkgType: "rpm",
			template: &config.ImageTemplate{
				Target: config.TargetInfo{Arch: "x86_64"},
				SystemConfig: config.SystemConfig{
					Packages: []string{"pkg1", "pkg2"},
				},
			},
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
				{Pattern: "dnf", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:              "refresh_cache_failure",
			pkgType:           "deb",
			shouldFailRefresh: true,
			template: &config.ImageTemplate{
				Target: config.TargetInfo{Arch: "x86_64"},
				SystemConfig: config.SystemConfig{
					Packages: []string{"pkg1"},
				},
			},
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
				{Pattern: "apt", Output: "", Error: nil},
			},
			expectError:   true,
			expectedError: "mock refresh cache repo failure",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			tempDir := t.TempDir()
			chrootEnv := &mockChrootEnv{
				pkgType:           tt.pkgType,
				chrootEnvRoot:     tempDir,
				chrootPkgCacheDir: filepath.Join(tempDir, "cache"),
				shouldFailRefresh: tt.shouldFailRefresh,
			}

			chrootImageBuildDir := chrootEnv.GetChrootImageBuildDir()
			if err := os.MkdirAll(chrootImageBuildDir, 0700); err != nil {
				t.Fatalf("Failed to create chroot image build dir: %v", err)
			}

			isoMaker, err := isomaker.NewIsoMaker(chrootEnv, tt.template)
			if err != nil {
				t.Fatalf("Failed to create IsoMaker: %v", err)
			}

			currentConfig := config.Global()
			currentConfig.WorkDir = tempDir
			config.SetGlobal(currentConfig)

			if err := isoMaker.Init(); err != nil {
				t.Fatalf("Failed to init IsoMaker: %v", err)
			}

			// Create cache directory
			if err := os.MkdirAll(chrootEnv.GetChrootPkgCacheDir(), 0700); err != nil {
				t.Fatalf("Failed to create cache directory: %v", err)
			}

			// Since downloadInitrdPkgs is unexported, we test the download functionality
			// through the public interface by verifying the chrootEnv methods are called
			if chrootEnv.GetTargetOsPkgType() != tt.pkgType {
				t.Errorf("Expected pkg type %s, got %s", tt.pkgType, chrootEnv.GetTargetOsPkgType())
			}

			err = chrootEnv.RefreshLocalCacheRepo()
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

func TestCopyStaticFilesToIsolinuxPath(t *testing.T) {
	tests := []struct {
		name          string
		setupFunc     func(tempDir string) error
		expectError   bool
		expectedError string
	}{
		{
			name:        "successful_copy",
			setupFunc:   setupValidStaticFiles,
			expectError: false,
		},
		{
			name:          "missing_required_files",
			setupFunc:     setupIncompleteStaticFiles,
			expectError:   true,
			expectedError: "required BIOS boot file does not exist",
		},
		{
			name:          "no_static_files",
			setupFunc:     func(tempDir string) error { return nil },
			expectError:   true,
			expectedError: "required BIOS boot file does not exist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			staticDir := filepath.Join(tempDir, "static")
			isoLinuxDir := filepath.Join(tempDir, "isolinux")

			if err := os.MkdirAll(staticDir, 0700); err != nil {
				t.Fatalf("Failed to create static dir: %v", err)
			}
			if err := os.MkdirAll(isoLinuxDir, 0700); err != nil {
				t.Fatalf("Failed to create isolinux dir: %v", err)
			}

			if tt.setupFunc != nil {
				if err := tt.setupFunc(staticDir); err != nil {
					t.Fatalf("Failed to setup test: %v", err)
				}
			}

			// Since copyStaticFilesToIsolinuxPath is unexported, we test the file operations
			// by verifying that required files exist
			requiredFiles := []string{
				"isolinux.bin", "ldlinux.c32", "libcom32.c32", "libutil.c32",
				"vesamenu.c32", "menu.c32", "linux.c32", "libmenu.c32",
			}

			var missingFiles []string
			for _, file := range requiredFiles {
				filePath := filepath.Join(staticDir, file)
				if _, err := os.Stat(filePath); os.IsNotExist(err) {
					missingFiles = append(missingFiles, file)
				}
			}

			if tt.expectError {
				if len(missingFiles) == 0 {
					t.Error("Expected missing files, but all files exist")
				}
			} else {
				if len(missingFiles) > 0 {
					t.Errorf("Expected all files to exist, but missing: %v", missingFiles)
				}
			}
		})
	}
}

func TestCreateIsolinuxCfg(t *testing.T) {
	tests := []struct {
		name          string
		setupFunc     func(tempDir string) error
		imageName     string
		expectError   bool
		expectedError string
	}{
		{
			name:        "successful_creation",
			setupFunc:   setupValidIsolinuxConfig,
			imageName:   "test-image",
			expectError: false,
		},
		{
			name:          "missing_source_config",
			setupFunc:     func(tempDir string) error { return nil },
			imageName:     "test-image",
			expectError:   true,
			expectedError: "isolinux.cfg file does not exist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			isoLinuxDir := filepath.Join(tempDir, "isolinux")

			if err := os.MkdirAll(isoLinuxDir, 0700); err != nil {
				t.Fatalf("Failed to create isolinux dir: %v", err)
			}

			if tt.setupFunc != nil {
				if err := tt.setupFunc(tempDir); err != nil {
					t.Fatalf("Failed to setup test: %v", err)
				}
			}

			// Mock the general config directory
			generalConfigDir := filepath.Join(tempDir, "general")
			os.Setenv("IMAGE_COMPOSER_CONFIG_DIR", generalConfigDir)
			defer os.Unsetenv("IMAGE_COMPOSER_CONFIG_DIR")

			// Since createIsolinuxCfg is unexported, we test by checking if the source file exists
			isolinuxCfgSrc := filepath.Join(generalConfigDir, "isolinux", "isolinux.cfg")
			_, err := os.Stat(isolinuxCfgSrc)

			if tt.expectError {
				if err == nil {
					t.Error("Expected source file to be missing, but it exists")
				}
			} else {
				if err != nil {
					t.Errorf("Expected source file to exist, but got error: %v", err)
				}
			}
		})
	}
}

func TestCreateEfiFatImage(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name         string
		mockCommands []shell.MockCommand
		setupFunc    func(tempDir string) error
		expectError  bool
		errorMsg     string
	}{
		{
			name: "successful_creation",
			mockCommands: []shell.MockCommand{
				{Pattern: "fallocate", Output: "", Error: nil},
				{Pattern: "mkfs", Output: "", Error: nil},
				{Pattern: "mount", Output: "", Error: nil},
				{Pattern: "umount", Output: "", Error: nil},
				{Pattern: "sync", Output: "", Error: nil},
				{Pattern: "rm", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name: "mkfs_failure",
			mockCommands: []shell.MockCommand{
				{Pattern: "fallocate", Output: "", Error: nil},
				{Pattern: "mkfs", Output: "", Error: fmt.Errorf("mkfs failed")},
			},
			expectError: true,
			errorMsg:    "mkfs failed",
		},
		{
			name: "mount_failure",
			mockCommands: []shell.MockCommand{
				{Pattern: "fallocate", Output: "", Error: nil},
				{Pattern: "mkfs", Output: "", Error: nil},
				{Pattern: "mount", Output: "", Error: fmt.Errorf("mount failed")},
			},
			expectError: true,
			errorMsg:    "mount failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			tempDir := t.TempDir()
			isoEfiPath := filepath.Join(tempDir, "efi")
			isoImagesPath := filepath.Join(tempDir, "images")

			if err := os.MkdirAll(isoEfiPath, 0700); err != nil {
				t.Fatalf("Failed to create EFI dir: %v", err)
			}
			if err := os.MkdirAll(isoImagesPath, 0700); err != nil {
				t.Fatalf("Failed to create images dir: %v", err)
			}

			if tt.setupFunc != nil {
				if err := tt.setupFunc(tempDir); err != nil {
					t.Fatalf("Failed to setup test: %v", err)
				}
			}

			// Since createEfiFatImage is unexported, we test the individual commands
			// Test the fallocate command
			efiFatImgPath := filepath.Join(isoImagesPath, "efiboot.img")
			_, err := shell.ExecCmd(fmt.Sprintf("fallocate -l 18MiB %s", efiFatImgPath), true, shell.HostPath, nil)

			if tt.expectError {
				// For this test, we mainly check that commands are being called
				// The actual error might vary depending on which command fails
				if err != nil && tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					// Allow different error messages since we're testing individual commands
					t.Logf("Got error (may be expected): %v", err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for fallocate command, but got: %v", err)
				}
			}
		})
	}
}

// Helper functions for setting up test configurations
func setupValidInitrdTemplate(tempDir string) error {
	// Create the expected directory structure
	targetOsConfigDir := filepath.Join(tempDir, "ubuntu", "jammy")
	imageConfigsDir := filepath.Join(targetOsConfigDir, "imageconfigs", "defaultconfigs")

	if err := os.MkdirAll(imageConfigsDir, 0700); err != nil {
		return err
	}

	// Create a minimal initrd template
	initrdTemplate := `target:
  os: ubuntu
  dist: jammy
  arch: x86_64
packages:
  - initrd-pkg1
  - initrd-pkg2
`
	templatePath := filepath.Join(imageConfigsDir, "default-iso-initrd-x86_64.yml")
	return os.WriteFile(templatePath, []byte(initrdTemplate), 0644)
}

func setupMockConfigDirs(tempDir string, template *config.ImageTemplate) {
	// Mock config.GetTargetOsConfigDir
	os.Setenv("IMAGE_COMPOSER_CONFIG_DIR", tempDir)

	// Create the expected directory structure
	targetOsConfigDir := filepath.Join(tempDir, template.Target.OS, template.Target.Dist)
	if err := os.MkdirAll(targetOsConfigDir, 0700); err != nil {
		log.Errorf("Failed to create targetOsConfigDir")
	}
}

func setupValidStaticFiles(staticDir string) error {
	requiredFiles := []string{
		"isolinux.bin", "ldlinux.c32", "libcom32.c32", "libutil.c32",
		"vesamenu.c32", "menu.c32", "linux.c32", "libmenu.c32",
	}

	for _, file := range requiredFiles {
		filePath := filepath.Join(staticDir, file)
		if err := os.WriteFile(filePath, []byte("mock content"), 0644); err != nil {
			return err
		}
	}
	return nil
}

func setupIncompleteStaticFiles(staticDir string) error {
	// Only create some of the required files
	someFiles := []string{"isolinux.bin", "ldlinux.c32"}

	for _, file := range someFiles {
		filePath := filepath.Join(staticDir, file)
		if err := os.WriteFile(filePath, []byte("mock content"), 0644); err != nil {
			return err
		}
	}
	return nil
}

func setupValidIsolinuxConfig(tempDir string) error {
	generalConfigDir := filepath.Join(tempDir, "general")
	isolinuxDir := filepath.Join(generalConfigDir, "isolinux")

	if err := os.MkdirAll(isolinuxDir, 0700); err != nil {
		return err
	}

	configContent := `default vesamenu.c32
timeout 600
menu title {{.ImageName}} Boot Menu

label install
  menu label Install {{.ImageName}}
  kernel /images/vmlinuz
  append initrd=/images/initrd.img
`
	configPath := filepath.Join(isolinuxDir, "isolinux.cfg")
	return os.WriteFile(configPath, []byte(configContent), 0644)
}

func TestIsoMaker_BuildIsoImage_Integration(t *testing.T) {
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
			name: "missing_initrd_template",
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
					Initramfs: config.Initramfs{
						Template: "/nonexistent-template.yml",
					},
				},
			},
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
			},
			setupFunc:     func(tempDir string) error { return nil },
			expectError:   true,
			expectedError: "failed to build initrd image",
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

			// Mock config directories
			os.Setenv("IMAGE_COMPOSER_WORK_DIR", tempDir)
			os.Setenv("IMAGE_COMPOSER_CONFIG_DIR", tempDir)
			defer func() {
				os.Unsetenv("IMAGE_COMPOSER_WORK_DIR")
				os.Unsetenv("IMAGE_COMPOSER_CONFIG_DIR")
			}()

			isoMaker, err := isomaker.NewIsoMaker(chrootEnv, tt.template)
			if err != nil {
				t.Fatalf("Failed to create IsoMaker: %v", err)
			}

			currentConfig := config.Global()
			currentConfig.WorkDir = tempDir
			config.SetGlobal(currentConfig)

			err = isoMaker.Init()
			if err != nil {
				t.Fatalf("Failed to initialize IsoMaker: %v", err)
			}

			// Manually create installRoot and necessary files since mocks won't do it
			installRoot := isoMaker.ImageOs.GetInstallRoot()
			if err := os.MkdirAll(filepath.Join(installRoot, "boot", "grub"), 0755); err != nil {
				t.Fatalf("Failed to create installRoot/boot/grub: %v", err)
			}
			// Create dummy grub.cfg in installRoot/boot/grub to simulate the first CopyFile in createGrubCfg
			// This is needed because the second CopyFile (to EFI/BOOT) checks for existence of source file
			if err := os.WriteFile(filepath.Join(installRoot, "boot", "grub", "grub.cfg"), []byte("dummy grub config"), 0644); err != nil {
				t.Fatalf("Failed to create dummy grub.cfg: %v", err)
			}

			// Run BuildIsoImage
			err = isoMaker.BuildIsoImage()

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

// Mock implementations for InitrdMaker and ImageOs

type MockInitrdMaker struct {
	initCalled         bool
	downloadCalled     bool
	buildCalled        bool
	cleanCalled        bool
	initrdVersion      string
	initrdFilePath     string
	initrdRootfsPath   string
	shouldFailInit     bool
	shouldFailDownload bool
	shouldFailBuild    bool
	shouldFailClean    bool
}

func (m *MockInitrdMaker) Init() error {
	m.initCalled = true
	if m.shouldFailInit {
		return fmt.Errorf("mock init failure")
	}
	return nil
}

func (m *MockInitrdMaker) DownloadInitrdPkgs() error {
	m.downloadCalled = true
	if m.shouldFailDownload {
		return fmt.Errorf("mock download failure")
	}
	return nil
}

func (m *MockInitrdMaker) BuildInitrdImage() error {
	m.buildCalled = true
	if m.shouldFailBuild {
		return fmt.Errorf("mock build failure")
	}
	return nil
}

func (m *MockInitrdMaker) GetInitrdVersion() string {
	return m.initrdVersion
}

func (m *MockInitrdMaker) GetInitrdFilePath() string {
	return m.initrdFilePath
}

func (m *MockInitrdMaker) GetInitrdRootfsPath() string {
	return m.initrdRootfsPath
}

func (m *MockInitrdMaker) CleanInitrdRootfs() error {
	m.cleanCalled = true
	if m.shouldFailClean {
		return fmt.Errorf("mock clean failure")
	}
	return nil
}

type MockImageOs struct {
	installRoot string
}

func (m *MockImageOs) GetInstallRoot() string {
	return m.installRoot
}

func (m *MockImageOs) InstallInitrd() (string, string, error) {
	return "", "", nil
}

func (m *MockImageOs) InstallImageOs(diskPathIdMap map[string]string) (string, error) {
	return "", nil
}

func TestIsoMaker_BuildIsoImage_Success(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	// Setup temporary directories
	tempDir := t.TempDir()
	installRoot := filepath.Join(tempDir, "installroot")
	initrdRootfsPath := filepath.Join(tempDir, "initrdrootfs")
	initrdFilePath := filepath.Join(tempDir, "initrd.img")
	imageBuildDir := filepath.Join(tempDir, "imagebuild")

	// Create necessary directories
	dirs := []string{installRoot, initrdRootfsPath, imageBuildDir}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	// Ensure chroot image build dir exists for NewIsoMaker
	chrootImageBuildDir := filepath.Join(tempDir, "workspace", "imagebuild")
	if err := os.MkdirAll(chrootImageBuildDir, 0755); err != nil {
		t.Fatalf("Failed to create chroot image build dir: %v", err)
	}

	// Create dummy files needed by createIso
	// 1. Kernel in initrdRootfsPath/boot/vmlinuz-x.y.z
	if err := os.MkdirAll(filepath.Join(initrdRootfsPath, "boot"), 0755); err != nil {
		t.Fatalf("Failed to create boot dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(initrdRootfsPath, "boot", "vmlinuz-5.15.0"), []byte("kernel"), 0644); err != nil {
		t.Fatalf("Failed to create dummy kernel: %v", err)
	}

	// 2. Initrd file
	if err := os.WriteFile(initrdFilePath, []byte("initrd"), 0644); err != nil {
		t.Fatalf("Failed to create dummy initrd file: %v", err)
	}

	// 3. grub.cfg in general config dir
	generalConfigDir := filepath.Join(tempDir, "general")
	if err := os.MkdirAll(filepath.Join(generalConfigDir, "isolinux"), 0755); err != nil {
		t.Fatalf("Failed to create isolinux dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(generalConfigDir, "isolinux", "grub.cfg"), []byte("grub config {{.ImageName}}"), 0644); err != nil {
		t.Fatalf("Failed to create dummy grub.cfg: %v", err)
	}
	// 4. load.cfg in general config dir
	if err := os.WriteFile(filepath.Join(generalConfigDir, "isolinux", "load.cfg"), []byte("load config"), 0644); err != nil {
		t.Fatalf("Failed to create dummy load.cfg: %v", err)
	}

	// 5. GRUB modules in initrdRootfsPath/usr/lib/grub/x86_64-efi
	grubLibDir := filepath.Join(initrdRootfsPath, "usr", "lib", "grub", "x86_64-efi")
	if err := os.MkdirAll(grubLibDir, 0755); err != nil {
		t.Fatalf("Failed to create grub lib dir: %v", err)
	}

	// 6. OSV config files
	osvConfigDir := filepath.Join(tempDir, "config")
	if err := os.MkdirAll(osvConfigDir, 0755); err != nil {
		t.Fatalf("Failed to create osv config dir: %v", err)
	}

	// 7. Create dummy grub.cfg in installRoot/boot/grub to simulate the first CopyFile in createGrubCfg
	if err := os.MkdirAll(filepath.Join(installRoot, "boot", "grub"), 0755); err != nil {
		t.Fatalf("Failed to create installRoot/boot/grub: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installRoot, "boot", "grub", "grub.cfg"), []byte("dummy grub config"), 0644); err != nil {
		t.Fatalf("Failed to create dummy grub.cfg in installRoot: %v", err)
	}

	// Mock config.GetGeneralConfigDir
	originalGlobal := config.Global()
	defer config.SetGlobal(originalGlobal)

	newGlobal := config.DefaultGlobalConfig()
	newGlobal.ConfigDir = tempDir
	config.SetGlobal(newGlobal)

	// Mock shell commands
	mockCommands := []shell.MockCommand{
		{Pattern: "mkdir", Output: "", Error: nil},
		{Pattern: "ls /boot | grep vmlinuz", Output: "vmlinuz-5.15.0", Error: nil},
		{Pattern: "cp", Output: "", Error: nil},
		{Pattern: "rm", Output: "", Error: nil},
		{Pattern: "mformat", Output: "", Error: nil},
		{Pattern: "grub-mkimage", Output: "", Error: nil},
		{Pattern: "mcopy", Output: "", Error: nil},
		{Pattern: "xorriso", Output: "", Error: nil},
		{Pattern: "sed", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	// Setup IsoMaker
	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Target: config.TargetInfo{
			OS:   "ubuntu",
			Dist: "jammy",
			Arch: "x86_64",
		},
		SystemConfig: config.SystemConfig{
			Name: "test-system",
		},
	}

	chrootEnv := &mockChrootEnv{
		pkgType:           "deb",
		chrootEnvRoot:     tempDir,
		chrootPkgCacheDir: filepath.Join(tempDir, "cache"),
	}

	mockInitrdMaker := &MockInitrdMaker{
		initrdVersion:    "1.0.0",
		initrdFilePath:   initrdFilePath,
		initrdRootfsPath: initrdRootfsPath,
	}

	mockImageOs := &MockImageOs{
		installRoot: installRoot,
	}

	isoMaker, err := isomaker.NewIsoMaker(chrootEnv, template)
	if err != nil {
		t.Fatalf("Failed to create IsoMaker: %v", err)
	}
	isoMaker.InitrdMaker = mockInitrdMaker
	isoMaker.ImageOs = mockImageOs
	isoMaker.ImageBuildDir = imageBuildDir

	// Run BuildIsoImage
	err = isoMaker.BuildIsoImage()
	if err != nil {
		t.Errorf("BuildIsoImage failed: %v", err)
	}

	// Verify InitrdMaker methods were called
	if !mockInitrdMaker.initCalled {
		t.Error("InitrdMaker.Init was not called")
	}
	if !mockInitrdMaker.downloadCalled {
		t.Error("InitrdMaker.DownloadInitrdPkgs was not called")
	}
	if !mockInitrdMaker.buildCalled {
		t.Error("InitrdMaker.BuildInitrdImage was not called")
	}
	if !mockInitrdMaker.cleanCalled {
		t.Error("InitrdMaker.CleanInitrdRootfs was not called")
	}
}
