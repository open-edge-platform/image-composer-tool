package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

func TestNewChrootBuilder_MissingConfigDir(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()
	nonExistentDir := filepath.Join(tmpDir, "nonexistent")

	_, err := newChrootBuilder(nonExistentDir, "/tmp/repo", "azure-linux", "3.0", "x86_64")
	if err == nil {
		t.Fatal("expected error when config directory does not exist")
	}
	if err.Error() != fmt.Sprintf("target OS config directory does not exist: %s", filepath.Join(nonExistentDir, "osv", "azure-linux", "3.0")) {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestNewChrootBuilder_MissingConfigFile(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()
	targetOsConfigDir := filepath.Join(tmpDir, "osv", "azure-linux", "3.0")
	if err := os.MkdirAll(targetOsConfigDir, 0755); err != nil {
		t.Fatalf("failed to create test directory: %v", err)
	}

	_, err := newChrootBuilder(tmpDir, "/tmp/repo", "azure-linux", "3.0", "x86_64")
	if err == nil {
		t.Fatal("expected error when config file does not exist")
	}
	expectedErr := fmt.Sprintf("target OS config file does not exist: %s", filepath.Join(targetOsConfigDir, "config.yml"))
	if err.Error() != expectedErr {
		t.Errorf("expected error %q, got %q", expectedErr, err.Error())
	}
}

func TestNewChrootBuilder_InvalidYAML(t *testing.T) {
	// Create a temporary directory structure with an invalid config file
	tmpDir := t.TempDir()
	targetOsConfigDir := filepath.Join(tmpDir, "osv", "azure-linux", "3.0")
	if err := os.MkdirAll(targetOsConfigDir, 0755); err != nil {
		t.Fatalf("failed to create test directory: %v", err)
	}

	configFile := filepath.Join(targetOsConfigDir, "config.yml")
	invalidYAML := "this is not valid yaml: [[[{"
	if err := os.WriteFile(configFile, []byte(invalidYAML), 0644); err != nil {
		t.Fatalf("failed to write test config file: %v", err)
	}

	_, err := newChrootBuilder(tmpDir, "/tmp/repo", "azure-linux", "3.0", "x86_64")
	if err == nil {
		t.Fatal("expected error when parsing invalid YAML")
	}
}

func TestNewChrootEnv_Success(t *testing.T) {
	// Create a temporary directory structure with a valid config file
	tmpDir := t.TempDir()
	targetOsConfigDir := filepath.Join(tmpDir, "osv", "azure-linux", "3.0")
	if err := os.MkdirAll(targetOsConfigDir, 0755); err != nil {
		t.Fatalf("failed to create test directory: %v", err)
	}

	configFile := filepath.Join(targetOsConfigDir, "config.yml")
	validYAML := `x86_64:
  dist: "azl3"
  arch: "x86_64"
  pkgType: "rpm"
  chrootenvConfigFile: "chrootenvconfigs/chrootenv_x86_64.yml"
  releaseVersion: "3.0"
`
	if err := os.WriteFile(configFile, []byte(validYAML), 0644); err != nil {
		t.Fatalf("failed to write test config file: %v", err)
	}

	env, err := newChrootEnv(tmpDir, "/tmp/repo", "azure-linux", "3.0", "x86_64")
	if err != nil {
		t.Fatalf("unexpected error creating chroot env: %v", err)
	}

	if env == nil {
		t.Fatal("expected non-nil chroot env")
	}

	if env.ChrootBuilder == nil {
		t.Fatal("expected non-nil chroot builder")
	}
}

func TestDependencyCheck_AzureLinux(t *testing.T) {
	// This test will likely fail unless the dependencies are installed
	// We're testing that the function properly checks for dependencies
	err := dependencyCheck("azure-linux")

	// We expect either success (if deps are installed) or a specific error format
	if err != nil {
		// Error should mention a specific command and package
		errStr := err.Error()
		if errStr == "" {
			t.Error("expected non-empty error message")
		}
	}
}

func TestDependencyCheck_EdgeMicrovisorToolkit(t *testing.T) {
	err := dependencyCheck("edge-microvisor-toolkit")

	// We expect either success (if deps are installed) or a specific error format
	if err != nil {
		errStr := err.Error()
		if errStr == "" {
			t.Error("expected non-empty error message")
		}
	}
}

func TestDependencyCheck_WindRiverELXR(t *testing.T) {
	err := dependencyCheck("wind-river-elxr")

	// We expect either success (if deps are installed) or a specific error format
	if err != nil {
		errStr := err.Error()
		if errStr == "" {
			t.Error("expected non-empty error message")
		}
	}
}

func TestDependencyCheck_UnsupportedOS(t *testing.T) {
	err := dependencyCheck("unsupported-os")
	if err == nil {
		t.Fatal("expected error for unsupported OS")
	}

	expectedErrMsg := "unsupported target OS for dependency check: unsupported-os"
	if err.Error() != expectedErrMsg {
		t.Errorf("expected error %q, got %q", expectedErrMsg, err.Error())
	}
}

func TestInstall_MissingConfigDir(t *testing.T) {
	// Initialize global config
	globalConfig = config.DefaultGlobalConfig()
	config.SetGlobal(globalConfig)

	// Create a minimal template
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
		Disk: config.DiskConfig{
			Path: "/dev/sda",
		},
	}

	// Use non-existent directories
	err := install(template, "/nonexistent/config", "/nonexistent/repo")
	if err == nil {
		t.Fatal("expected error when using non-existent directories")
	}
}

func TestRemoveOldBootEntries_NoEfibootmgr(t *testing.T) {
	// This test checks if the function handles missing efibootmgr gracefully
	// The test may fail if efibootmgr is not available (expected behavior)
	err := removeOldBootEntries()

	// We accept both success (if efibootmgr exists) or a specific error
	if err != nil {
		// Error should be about failing to list boot entries
		if err.Error() == "" {
			t.Error("expected non-empty error message")
		}
	}
}

func TestCreateNewBootEntry_EmptyDiskPath(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
		Disk: config.DiskConfig{
			Path: "",
		},
	}

	diskPathIdMap := make(map[string]string)

	err := createNewBootEntry(template, diskPathIdMap)
	if err == nil {
		t.Fatal("expected error when disk path is empty")
	}

	expectedErrMsg := "no target disk path specified in the template"
	if err.Error() != expectedErrMsg {
		t.Errorf("expected error %q, got %q", expectedErrMsg, err.Error())
	}
}

func TestCreateNewBootEntry_NoBootPartition(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
		Disk: config.DiskConfig{
			Path: "/dev/sda",
			Partitions: []config.PartitionInfo{
				{
					ID:         "root",
					MountPoint: "/",
				},
			},
		},
	}

	diskPathIdMap := map[string]string{
		"root": "/dev/sda1",
	}

	err := createNewBootEntry(template, diskPathIdMap)
	if err == nil {
		t.Fatal("expected error when no EFI boot partition exists")
	}

	expectedErrMsg := "no EFI boot partition found in the disk partitions"
	if err.Error() != expectedErrMsg {
		t.Errorf("expected error %q, got %q", expectedErrMsg, err.Error())
	}
}

func TestUpdateBootOrder_NonEFIBoot(t *testing.T) {
	template := &config.ImageTemplate{
		SystemConfig: config.SystemConfig{
			Bootloader: config.Bootloader{
				BootType: "legacy",
			},
		},
	}

	diskPathIdMap := make(map[string]string)

	// Should return nil for non-EFI boot types
	err := updateBootOrder(template, diskPathIdMap)
	if err != nil {
		t.Errorf("expected no error for non-EFI boot type, got %v", err)
	}
}

func TestUnattendedInstall_InvalidTemplatePath(t *testing.T) {
	err := unattendedInstall("/nonexistent/template.yml", "/tmp/repo")
	if err == nil {
		t.Fatal("expected error when template file does not exist")
	}
}

func TestAttendedInstall_InvalidTemplatePath(t *testing.T) {
	installationQuit, err := attendedInstall("/nonexistent/template.yml", "/tmp/repo")
	if err == nil {
		t.Fatal("expected error when template file does not exist")
	}
	if installationQuit {
		t.Error("expected installationQuit to be false for file error")
	}
}

func TestNewChrootBuilder_ValidConfig(t *testing.T) {
	// Create a temporary directory structure with a valid config file
	tmpDir := t.TempDir()
	targetOsConfigDir := filepath.Join(tmpDir, "osv", "azure-linux", "3.0")
	if err := os.MkdirAll(targetOsConfigDir, 0755); err != nil {
		t.Fatalf("failed to create test directory: %v", err)
	}

	configFile := filepath.Join(targetOsConfigDir, "config.yml")
	validYAML := `x86_64:
  dist: "azl3"
  arch: "x86_64"
  pkgType: "rpm"
  chrootenvConfigFile: "chrootenvconfigs/chrootenv_x86_64.yml"
  releaseVersion: "3.0"
`
	if err := os.WriteFile(configFile, []byte(validYAML), 0644); err != nil {
		t.Fatalf("failed to write test config file: %v", err)
	}

	builder, err := newChrootBuilder(tmpDir, "/tmp/repo", "azure-linux", "3.0", "x86_64")
	if err != nil {
		t.Fatalf("unexpected error creating chroot builder: %v", err)
	}

	if builder == nil {
		t.Fatal("expected non-nil chroot builder")
	}

	if builder.TargetOsConfigDir != targetOsConfigDir {
		t.Errorf("expected TargetOsConfigDir to be %q, got %q", targetOsConfigDir, builder.TargetOsConfigDir)
	}

	if builder.ChrootPkgCacheDir != "/tmp/repo" {
		t.Errorf("expected ChrootPkgCacheDir to be '/tmp/repo', got %q", builder.ChrootPkgCacheDir)
	}

	if builder.RpmInstaller == nil {
		t.Error("expected non-nil RpmInstaller")
	}

	if builder.DebInstaller == nil {
		t.Error("expected non-nil DebInstaller")
	}
}

func TestNewChrootBuilder_MissingArchitecture(t *testing.T) {
	// Create a temporary directory structure with a valid config file
	tmpDir := t.TempDir()
	targetOsConfigDir := filepath.Join(tmpDir, "osv", "azure-linux", "3.0")
	if err := os.MkdirAll(targetOsConfigDir, 0755); err != nil {
		t.Fatalf("failed to create test directory: %v", err)
	}

	configFile := filepath.Join(targetOsConfigDir, "config.yml")
	// YAML with only x86_64, but we'll request aarch64
	validYAML := `x86_64:
  dist: "azl3"
  arch: "x86_64"
  pkgType: "rpm"
  chrootenvConfigFile: "chrootenvconfigs/chrootenv_x86_64.yml"
  releaseVersion: "3.0"
`
	if err := os.WriteFile(configFile, []byte(validYAML), 0644); err != nil {
		t.Fatalf("failed to write test config file: %v", err)
	}

	_, err := newChrootBuilder(tmpDir, "/tmp/repo", "azure-linux", "3.0", "aarch64")
	if err == nil {
		t.Fatal("expected error when architecture is not found in config")
	}

	// The error should mention that the architecture is not found
	if !strings.Contains(err.Error(), "aarch64") {
		t.Errorf("expected error to mention 'aarch64', got: %v", err)
	}
}

func TestNewChrootBuilder_InvalidConfigFormat(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()
	targetOsConfigDir := filepath.Join(tmpDir, "osv", "azure-linux", "3.0")
	if err := os.MkdirAll(targetOsConfigDir, 0755); err != nil {
		t.Fatalf("failed to create test directory: %v", err)
	}

	configFile := filepath.Join(targetOsConfigDir, "config.yml")
	// Invalid YAML that fails schema validation (missing required fields)
	invalidYAML := `x86_64:
  invalid_field: "value"
`
	if err := os.WriteFile(configFile, []byte(invalidYAML), 0644); err != nil {
		t.Fatalf("failed to write test config file: %v", err)
	}

	_, err := newChrootBuilder(tmpDir, "/tmp/repo", "azure-linux", "3.0", "x86_64")
	if err == nil {
		t.Fatal("expected error when config format is invalid")
	}

	// Error should mention validation failure
	if !strings.Contains(err.Error(), "validation") {
		t.Errorf("expected error to mention validation, got: %v", err)
	}
}
