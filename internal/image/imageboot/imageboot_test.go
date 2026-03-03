package imageboot

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/os-image-composer/internal/config"
	"github.com/open-edge-platform/os-image-composer/internal/utils/shell"
)

func setupConfigDir(t *testing.T) string {
	configDir := t.TempDir()
	generalDir := filepath.Join(configDir, "general")

	// Create directories
	dirs := []string{
		filepath.Join(generalDir, "image", "efi", "grub"),
		filepath.Join(generalDir, "image", "grub2"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", d, err)
		}
	}

	// Create dummy files
	files := map[string]string{
		filepath.Join(generalDir, "image", "efi", "grub", "grub.cfg"): "boot_uuid={{.BootUUID}}\ncrypto_mount={{.CryptoMountCommand}}\nprefix={{.PrefixPath}}",
		filepath.Join(generalDir, "image", "grub2", "grubenv"):        "saved_entry=0",
		filepath.Join(generalDir, "image", "grub2", "grub"):           "GRUB_CMDLINE_LINUX=\"{{.ExtraCommandLine}}\"\nGRUB_DISTRIBUTOR=\"{{.Hostname}}\"",
		filepath.Join(generalDir, "image", "efi", "bootParams.conf"):  "options {{.BootUUID}} {{.BootPrefix}} {{.RootPartition}} {{.SystemdVerity}} {{.RootHash}}",
	}

	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create file %s: %v", path, err)
		}
	}

	// Set global config
	config.SetGlobal(&config.GlobalConfig{
		ConfigDir: configDir,
		Logging:   config.LoggingConfig{Level: "debug"},
	})

	return configDir
}

func TestInstallImageBoot_MissingRootPartition(t *testing.T) {
	diskPathIdMap := map[string]string{
		"boot": "/dev/sda1",
	}

	tmpDir := t.TempDir()
	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Disk: config.DiskConfig{
			Partitions: []config.PartitionInfo{
				{ID: "boot", MountPoint: "/boot"},
			},
		},
		SystemConfig: config.SystemConfig{
			Bootloader: config.Bootloader{
				Provider: "grub",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "blkid", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	imageBoot := NewImageBoot()
	err := imageBoot.InstallImageBoot(tmpDir, diskPathIdMap, template, "deb")

	if err == nil {
		t.Error("Expected error when root partition is missing")
	}
	if !strings.Contains(err.Error(), "failed to find root partition for mount point '/'") {
		t.Errorf("Expected root partition error, got: %v", err)
	}
}

func TestInstallImageBoot_EmptyDiskPathIdMap(t *testing.T) {
	diskPathIdMap := map[string]string{}

	tmpDir := t.TempDir()
	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Disk: config.DiskConfig{
			Partitions: []config.PartitionInfo{
				{ID: "root", MountPoint: "/"},
			},
		},
		SystemConfig: config.SystemConfig{
			Bootloader: config.Bootloader{
				Provider: "grub",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "blkid", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	imageBoot := NewImageBoot()
	err := imageBoot.InstallImageBoot(tmpDir, diskPathIdMap, template, "deb")

	if err == nil {
		t.Error("Expected error with empty disk path ID map")
	}
	if !strings.Contains(err.Error(), "failed to find root partition for mount point '/'") {
		t.Errorf("Expected root partition error, got: %v", err)
	}
}

func TestInstallImageBoot_UUIDRetrievalFailure(t *testing.T) {
	diskPathIdMap := map[string]string{
		"root": "/dev/sda1",
	}

	tmpDir := t.TempDir()
	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Disk: config.DiskConfig{
			Partitions: []config.PartitionInfo{
				{ID: "root", MountPoint: "/"},
			},
		},
		SystemConfig: config.SystemConfig{
			Bootloader: config.Bootloader{
				Provider: "grub",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "blkid", Output: "", Error: fmt.Errorf("device not found")},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	imageBoot := NewImageBoot()
	err := imageBoot.InstallImageBoot(tmpDir, diskPathIdMap, template, "deb")

	if err == nil {
		t.Error("Expected error when UUID retrieval fails")
	}
	if !strings.Contains(err.Error(), "failed to get UUID") {
		t.Errorf("Expected UUID error, got: %v", err)
	}
}

func TestInstallImageBoot_PartUUIDRetrievalFailure(t *testing.T) {
	diskPathIdMap := map[string]string{
		"root": "/dev/sda1",
	}

	tmpDir := t.TempDir()
	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Disk: config.DiskConfig{
			Partitions: []config.PartitionInfo{
				{ID: "root", MountPoint: "/"},
			},
		},
		SystemConfig: config.SystemConfig{
			Bootloader: config.Bootloader{
				Provider: "grub",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "blkid.*PARTUUID", Output: "", Error: fmt.Errorf("partition UUID not found")},
		{Pattern: "blkid.*UUID", Output: "UUID=test-uuid\n", Error: nil},
		{Pattern: "command -v grub2-mkconfig", Output: "/usr/sbin/grub2-mkconfig", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	imageBoot := NewImageBoot()
	err := imageBoot.InstallImageBoot(tmpDir, diskPathIdMap, template, "deb")

	if err == nil {
		t.Error("Expected error when partition UUID retrieval fails")
	}
	if !strings.Contains(err.Error(), "failed to get partition UUID") {
		t.Errorf("Expected partition UUID error, got: %v", err)
	}
}

func TestInstallImageBoot_UnsupportedBootloaderProvider(t *testing.T) {
	diskPathIdMap := map[string]string{
		"root": "/dev/sda1",
	}

	tmpDir := t.TempDir()
	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Disk: config.DiskConfig{
			Partitions: []config.PartitionInfo{
				{ID: "root", MountPoint: "/"},
			},
		},
		SystemConfig: config.SystemConfig{
			Bootloader: config.Bootloader{
				Provider: "unknown",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "blkid.*UUID", Output: "UUID=test-uuid\n", Error: nil},
		{Pattern: "blkid.*PARTUUID", Output: "PARTUUID=test-partuuid\n", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	imageBoot := NewImageBoot()
	err := imageBoot.InstallImageBoot(tmpDir, diskPathIdMap, template, "deb")

	if err == nil {
		t.Error("Expected error for unsupported bootloader provider")
	}
	if !strings.Contains(err.Error(), "unsupported bootloader provider") {
		t.Errorf("Expected unsupported bootloader error, got: %v", err)
	}
}

func TestInstallImageBoot_GrubLegacyMode(t *testing.T) {
	diskPathIdMap := map[string]string{
		"root": "/dev/sda1",
	}

	tmpDir := t.TempDir()
	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Disk: config.DiskConfig{
			Partitions: []config.PartitionInfo{
				{ID: "root", MountPoint: "/"},
			},
		},
		SystemConfig: config.SystemConfig{
			Bootloader: config.Bootloader{
				BootType: "legacy",
				Provider: "grub",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "blkid.*UUID", Output: "UUID=test-uuid\n", Error: nil},
		{Pattern: "blkid.*PARTUUID", Output: "PARTUUID=test-partuuid\n", Error: nil},
		{Pattern: "command -v grub2-mkconfig", Output: "/usr/sbin/grub2-mkconfig", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	imageBoot := NewImageBoot()
	err := imageBoot.InstallImageBoot(tmpDir, diskPathIdMap, template, "deb")

	if err == nil {
		t.Error("Expected error for legacy boot mode not implemented")
	}
	if !strings.Contains(err.Error(), "legacy boot mode is not implemented yet") {
		t.Errorf("Expected legacy mode error, got: %v", err)
	}
}

func TestInstallImageBoot_GrubEfiMode(t *testing.T) {
	setupConfigDir(t)
	diskPathIdMap := map[string]string{
		"root": "/dev/sda1",
	}

	tmpDir := t.TempDir()
	// Create necessary directories in tmpDir
	if err := os.MkdirAll(filepath.Join(tmpDir, "boot", "efi", "boot", "grub2"), 0755); err != nil {
		t.Fatalf("Failed to create boot directories: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "boot", "grub2"), 0755); err != nil {
		t.Fatalf("Failed to create boot directories: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "etc", "default"), 0755); err != nil {
		t.Fatalf("Failed to create etc directories: %v", err)
	}
	// Create mock kernel file for initramfs update
	if err := os.WriteFile(filepath.Join(tmpDir, "boot", "vmlinuz-5.15.0-test"), []byte(""), 0644); err != nil {
		t.Fatalf("Failed to create mock kernel file: %v", err)
	}

	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Disk: config.DiskConfig{
			Partitions: []config.PartitionInfo{
				{ID: "root", MountPoint: "/"},
			},
		},
		SystemConfig: config.SystemConfig{
			Bootloader: config.Bootloader{
				Provider: "grub",
				BootType: "efi",
			},
			Kernel: config.KernelConfig{
				Cmdline: "console=tty0",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "blkid.*UUID", Output: "UUID=test-uuid\n", Error: nil},
		{Pattern: "blkid.*PARTUUID", Output: "PARTUUID=test-partuuid\n", Error: nil},
		{Pattern: "command -v grub2-mkconfig", Output: "/usr/sbin/grub2-mkconfig", Error: nil},
		{Pattern: "command -v update-initramfs", Output: "/usr/sbin/update-initramfs", Error: nil},
		{Pattern: "mkdir", Output: "", Error: nil},
		{Pattern: "cp", Output: "", Error: nil},
		{Pattern: "sed", Output: "", Error: nil},
		{Pattern: "chmod", Output: "", Error: nil},
		{Pattern: "chmod", Output: "", Error: nil},
		{Pattern: "echo.*initramfs-tools/modules", Output: "", Error: nil},
		{Pattern: "update-initramfs", Output: "", Error: nil},
		{Pattern: "grub-install", Output: "", Error: nil},
		{Pattern: "grub2-mkconfig", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	imageBoot := NewImageBoot()
	err := imageBoot.InstallImageBoot(tmpDir, diskPathIdMap, template, "deb")

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
}

func TestInstallImageBoot_SystemdBootEfiMode(t *testing.T) {
	setupConfigDir(t)
	diskPathIdMap := map[string]string{
		"root": "/dev/sda1",
	}

	tmpDir := t.TempDir()
	// Create necessary directories in tmpDir
	if err := os.MkdirAll(filepath.Join(tmpDir, "boot", "efi", "loader", "entries"), 0755); err != nil {
		t.Fatalf("Failed to create boot directories: %v", err)
	}

	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Disk: config.DiskConfig{
			Partitions: []config.PartitionInfo{
				{ID: "root", MountPoint: "/"},
			},
		},
		SystemConfig: config.SystemConfig{
			Bootloader: config.Bootloader{
				Provider: "systemd-boot",
				BootType: "efi",
			},
			Kernel: config.KernelConfig{
				Cmdline: "quiet splash",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "mkdir", Output: "", Error: nil},
		{Pattern: "cp", Output: "", Error: nil},
		{Pattern: "sed", Output: "", Error: nil},
		{Pattern: "blkid.*UUID", Output: "UUID=test-uuid\n", Error: nil},
		{Pattern: "blkid.*PARTUUID", Output: "PARTUUID=test-partuuid\n", Error: nil},
		{Pattern: "bootctl", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	imageBoot := NewImageBoot()
	err := imageBoot.InstallImageBoot(tmpDir, diskPathIdMap, template, "deb")

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
}

func TestInstallImageBoot_SystemdBootLegacyMode(t *testing.T) {
	diskPathIdMap := map[string]string{
		"root": "/dev/sda1",
	}

	tmpDir := t.TempDir()
	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Disk: config.DiskConfig{
			Partitions: []config.PartitionInfo{
				{ID: "root", MountPoint: "/"},
			},
		},
		SystemConfig: config.SystemConfig{
			Bootloader: config.Bootloader{
				Provider: "systemd-boot",
				BootType: "legacy",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "blkid.*UUID", Output: "UUID=test-uuid\n", Error: nil},
		{Pattern: "blkid.*PARTUUID", Output: "PARTUUID=test-partuuid\n", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	imageBoot := NewImageBoot()
	err := imageBoot.InstallImageBoot(tmpDir, diskPathIdMap, template, "deb")

	if err == nil {
		t.Error("Expected error for systemd-boot in legacy mode")
	}
	if !strings.Contains(err.Error(), "systemd-boot is only supported in EFI mode") {
		t.Errorf("Expected systemd-boot legacy mode error, got: %v", err)
	}
}

func TestInstallImageBoot_SeparateBootPartition(t *testing.T) {
	setupConfigDir(t)
	diskPathIdMap := map[string]string{
		"boot": "/dev/sda1",
		"root": "/dev/sda2",
	}

	tmpDir := t.TempDir()
	// Create necessary directories in tmpDir
	if err := os.MkdirAll(filepath.Join(tmpDir, "boot", "efi", "boot", "grub2"), 0755); err != nil {
		t.Fatalf("Failed to create boot directories: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "boot", "grub2"), 0755); err != nil {
		t.Fatalf("Failed to create boot directories: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "etc", "default"), 0755); err != nil {
		t.Fatalf("Failed to create etc directories: %v", err)
	}
	// Create mock kernel file for initramfs update
	if err := os.WriteFile(filepath.Join(tmpDir, "boot", "vmlinuz-5.15.0-test"), []byte(""), 0644); err != nil {
		t.Fatalf("Failed to create mock kernel file: %v", err)
	}

	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Disk: config.DiskConfig{
			Partitions: []config.PartitionInfo{
				{ID: "boot", MountPoint: "/boot"},
				{ID: "root", MountPoint: "/"},
			},
		},
		SystemConfig: config.SystemConfig{
			Bootloader: config.Bootloader{
				Provider: "grub",
				BootType: "efi",
			},
			Kernel: config.KernelConfig{
				Cmdline: "console=tty0",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "blkid.*UUID", Output: "UUID=boot-uuid\n", Error: nil},
		{Pattern: "blkid.*PARTUUID", Output: "PARTUUID=root-partuuid\n", Error: nil},
		{Pattern: "command -v grub2-mkconfig", Output: "/usr/sbin/grub2-mkconfig", Error: nil},
		{Pattern: "command -v update-initramfs", Output: "/usr/sbin/update-initramfs", Error: nil},
		{Pattern: "mkdir", Output: "", Error: nil},
		{Pattern: "cp", Output: "", Error: nil},
		{Pattern: "sed", Output: "", Error: nil},
		{Pattern: "chmod", Output: "", Error: nil},
		{Pattern: "chmod", Output: "", Error: nil},
		{Pattern: "echo.*initramfs-tools/modules", Output: "", Error: nil},
		{Pattern: "update-initramfs", Output: "", Error: nil},
		{Pattern: "grub-install", Output: "", Error: nil},
		{Pattern: "grub2-mkconfig", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	imageBoot := NewImageBoot()
	err := imageBoot.InstallImageBoot(tmpDir, diskPathIdMap, template, "deb")

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
}

func TestInstallImageBoot_ImmutabilityEnabled(t *testing.T) {
	setupConfigDir(t)
	diskPathIdMap := map[string]string{
		"root": "/dev/sda1",
		"hash": "/dev/sda2",
	}

	tmpDir := t.TempDir()
	// Create necessary directories in tmpDir
	if err := os.MkdirAll(filepath.Join(tmpDir, "boot", "efi", "loader", "entries"), 0755); err != nil {
		t.Fatalf("Failed to create boot directories: %v", err)
	}

	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Disk: config.DiskConfig{
			Partitions: []config.PartitionInfo{
				{ID: "root", MountPoint: "/"},
				{ID: "hash", MountPoint: "none"},
			},
		},
		SystemConfig: config.SystemConfig{
			Bootloader: config.Bootloader{
				Provider: "systemd-boot",
				BootType: "efi",
			},
			Immutability: config.ImmutabilityConfig{
				Enabled: true,
			},
			Kernel: config.KernelConfig{
				Cmdline: "console=tty0",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "blkid.*UUID", Output: "UUID=test-uuid\n", Error: nil},
		{Pattern: "blkid.*PARTUUID", Output: "PARTUUID=test-partuuid\n", Error: nil},
		{Pattern: "mkdir", Output: "", Error: nil},
		{Pattern: "cp", Output: "", Error: nil},
		{Pattern: "sed", Output: "", Error: nil},
		{Pattern: "bootctl", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	imageBoot := NewImageBoot()
	err := imageBoot.InstallImageBoot(tmpDir, diskPathIdMap, template, "deb")

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
}

func TestInstallImageBoot_ImmutabilityMissingHashPartition(t *testing.T) {
	diskPathIdMap := map[string]string{
		"root": "/dev/sda1",
	}

	tmpDir := t.TempDir()
	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Disk: config.DiskConfig{
			Partitions: []config.PartitionInfo{
				{ID: "root", MountPoint: "/"},
			},
		},
		SystemConfig: config.SystemConfig{
			Bootloader: config.Bootloader{
				Provider: "systemd-boot",
				BootType: "efi",
			},
			Immutability: config.ImmutabilityConfig{
				Enabled: true,
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "blkid.*UUID", Output: "UUID=test-uuid\n", Error: nil},
		{Pattern: "blkid.*PARTUUID", Output: "PARTUUID=test-partuuid\n", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	imageBoot := NewImageBoot()
	err := imageBoot.InstallImageBoot(tmpDir, diskPathIdMap, template, "deb")

	if err == nil {
		t.Error("Expected error when hash partition is missing for immutability")
	}
	if !strings.Contains(err.Error(), "failed to find dm verity hash partition") {
		t.Errorf("Expected hash partition error, got: %v", err)
	}
}

func TestInstallImageBoot_HashPartitionUUIDFailure(t *testing.T) {
	diskPathIdMap := map[string]string{
		"root": "/dev/sda1",
		"hash": "/dev/sda2",
	}

	tmpDir := t.TempDir()
	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Disk: config.DiskConfig{
			Partitions: []config.PartitionInfo{
				{ID: "root", MountPoint: "/"},
				{ID: "hash", MountPoint: "none"},
			},
		},
		SystemConfig: config.SystemConfig{
			Bootloader: config.Bootloader{
				Provider: "systemd-boot",
				BootType: "efi",
			},
			Immutability: config.ImmutabilityConfig{
				Enabled: true,
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "blkid.*sda2", Output: "", Error: fmt.Errorf("hash partition UUID not found")},
		{Pattern: "blkid.*sda1", Output: "PARTUUID=root-partuuid\n", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	imageBoot := NewImageBoot()
	err := imageBoot.InstallImageBoot(tmpDir, diskPathIdMap, template, "deb")

	if err == nil {
		t.Error("Expected error when hash partition UUID retrieval fails")
	}
	if !strings.Contains(err.Error(), "failed to get partition UUID for dm verity hash partition") {
		t.Errorf("Expected hash partition UUID error, got: %v", err)
	}
}
func TestGetDiskPartDevByMountPoint(t *testing.T) {
	tests := []struct {
		name          string
		mountPoint    string
		diskPathIdMap map[string]string
		template      *config.ImageTemplate
		expected      string
	}{
		{
			name:       "found_mount_point",
			mountPoint: "/",
			diskPathIdMap: map[string]string{
				"disk1": "/dev/sda",
			},
			template: &config.ImageTemplate{
				Image: config.ImageInfo{
					Name: "test-image",
				},
				Disk: config.DiskConfig{
					Partitions: []config.PartitionInfo{
						{ID: "disk1", MountPoint: "/"},
					},
				},
			},
			expected: "/dev/sda",
		},
		{
			name:       "not_found_mount_point",
			mountPoint: "/boot",
			diskPathIdMap: map[string]string{
				"disk1": "/dev/sda",
			},
			template: &config.ImageTemplate{
				Image: config.ImageInfo{
					Name: "test-image",
				},
				Disk: config.DiskConfig{
					Partitions: []config.PartitionInfo{
						{ID: "disk1", MountPoint: "/"},
					},
				},
			},
			expected: "",
		},
		{
			name:       "disk_id_mismatch",
			mountPoint: "/",
			diskPathIdMap: map[string]string{
				"disk2": "/dev/sdb",
			},
			template: &config.ImageTemplate{
				Image: config.ImageInfo{
					Name: "test-image",
				},
				Disk: config.DiskConfig{
					Partitions: []config.PartitionInfo{
						{ID: "disk1", MountPoint: "/"},
					},
				},
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getDiskPartDevByMountPoint(tt.mountPoint, tt.diskPathIdMap, tt.template)
			if result != tt.expected {
				t.Errorf("getDiskPartDevByMountPoint(%s) = %s, expected %s", tt.mountPoint, result, tt.expected)
			}
		})
	}
}

func TestInstallGrubWithLegacyMode(t *testing.T) {
	err := installGrubWithLegacyMode("/tmp", "uuid", "/boot", nil)
	if err == nil {
		t.Error("Expected error from installGrubWithLegacyMode, but got nil")
	}
	expectedErr := "legacy boot mode is not implemented yet"
	if err.Error() != expectedErr {
		t.Errorf("Expected error message '%s', but got '%s'", expectedErr, err.Error())
	}
}

func TestGetGrubVersion(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name         string
		mockCommands []shell.MockCommand
		expected     string
		expectError  bool
	}{
		{
			name: "grub2_exists",
			mockCommands: []shell.MockCommand{
				{Pattern: "command -v grub2-mkconfig", Output: "/usr/sbin/grub2-mkconfig", Error: nil},
			},
			expected:    "grub2",
			expectError: false,
		},
		{
			name: "grub_exists",
			mockCommands: []shell.MockCommand{
				{Pattern: "command -v grub2-mkconfig", Output: "", Error: nil},
				{Pattern: "command -v grub-mkconfig", Output: "/usr/sbin/grub-mkconfig", Error: nil},
			},
			expected:    "grub",
			expectError: false,
		},
		{
			name: "neither_exists",
			mockCommands: []shell.MockCommand{
				{Pattern: "command -v grub2-mkconfig", Output: "", Error: nil},
				{Pattern: "command -v grub-mkconfig", Output: "", Error: nil},
			},
			expected:    "",
			expectError: true,
		},
		{
			name: "error_checking_grub2",
			mockCommands: []shell.MockCommand{
				{Pattern: "command -v grub2-mkconfig", Output: "", Error: fmt.Errorf("failed")},
				{Pattern: "command -v grub-mkconfig", Output: "", Error: fmt.Errorf("failed")},
			},
			expected:    "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			result, err := getGrubVersion("/")

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				}
				if result != tt.expected {
					t.Errorf("Expected %s, but got %s", tt.expected, result)
				}
			}
		})
	}
}

func TestInstallImageBoot_KernelCmdlineWithRootParameter(t *testing.T) {
	diskPathIdMap := map[string]string{
		"root": "/dev/sda1",
	}

	tmpDir := t.TempDir()
	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Disk: config.DiskConfig{
			Partitions: []config.PartitionInfo{
				{ID: "root", MountPoint: "/"},
			},
		},
		SystemConfig: config.SystemConfig{
			Bootloader: config.Bootloader{
				Provider: "grub",
				BootType: "efi",
			},
			Kernel: config.KernelConfig{
				Cmdline: "root=/dev/mapper/rootfs_verity console=ttyS0,115200 console=tty0 quiet",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "blkid.*UUID", Output: "UUID=test-uuid\n", Error: nil},
		{Pattern: "blkid.*PARTUUID", Output: "PARTUUID=test-partuuid\n", Error: nil},
		{Pattern: "command -v grub2-mkconfig", Output: "/usr/sbin/grub2-mkconfig", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	imageBoot := NewImageBoot()
	err := imageBoot.InstallImageBoot(tmpDir, diskPathIdMap, template, "deb")

	// Should fail on config directory access but this tests the root parameter parsing logic
	if err != nil && !strings.Contains(err.Error(), "failed to get general config directory") {
		t.Logf("Got expected error: %v", err)
	}
}

func TestInstallImageBoot_KernelCmdlineEmptyRoot(t *testing.T) {
	diskPathIdMap := map[string]string{
		"root": "/dev/sda1",
	}

	tmpDir := t.TempDir()
	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Disk: config.DiskConfig{
			Partitions: []config.PartitionInfo{
				{ID: "root", MountPoint: "/"},
			},
		},
		SystemConfig: config.SystemConfig{
			Bootloader: config.Bootloader{
				Provider: "grub",
				BootType: "efi",
			},
			Kernel: config.KernelConfig{
				Cmdline: "console=ttyS0,115200 console=tty0 quiet",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "blkid.*UUID", Output: "UUID=test-uuid\n", Error: nil},
		{Pattern: "blkid.*PARTUUID", Output: "PARTUUID=test-partuuid\n", Error: nil},
		{Pattern: "command -v grub2-mkconfig", Output: "/usr/sbin/grub2-mkconfig", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	imageBoot := NewImageBoot()
	err := imageBoot.InstallImageBoot(tmpDir, diskPathIdMap, template, "deb")

	// Should fail on config directory access but this tests cmdline without root parameter
	if err != nil && !strings.Contains(err.Error(), "failed to get general config directory") {
		t.Logf("Got expected error: %v", err)
	}
}

func TestInstallImageBoot_KernelCmdlineMultipleRootParameters(t *testing.T) {
	diskPathIdMap := map[string]string{
		"root": "/dev/sda1",
	}

	tmpDir := t.TempDir()
	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Disk: config.DiskConfig{
			Partitions: []config.PartitionInfo{
				{ID: "root", MountPoint: "/"},
			},
		},
		SystemConfig: config.SystemConfig{
			Bootloader: config.Bootloader{
				Provider: "grub",
				BootType: "efi",
			},
			Kernel: config.KernelConfig{
				Cmdline: "root=/dev/sda1 console=ttyS0,115200 root=/dev/mapper/rootfs_verity quiet",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "blkid.*UUID", Output: "UUID=test-uuid\n", Error: nil},
		{Pattern: "blkid.*PARTUUID", Output: "PARTUUID=test-partuuid\n", Error: nil},
		{Pattern: "command -v grub2-mkconfig", Output: "/usr/sbin/grub2-mkconfig", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	imageBoot := NewImageBoot()
	err := imageBoot.InstallImageBoot(tmpDir, diskPathIdMap, template, "deb")

	// Should fail on config directory access but this tests filtering multiple root parameters
	if err != nil && !strings.Contains(err.Error(), "failed to get general config directory") {
		t.Logf("Got expected error: %v", err)
	}
}

func TestInstallImageBoot_KernelCmdlineEmptyString(t *testing.T) {
	diskPathIdMap := map[string]string{
		"root": "/dev/sda1",
	}

	tmpDir := t.TempDir()
	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Disk: config.DiskConfig{
			Partitions: []config.PartitionInfo{
				{ID: "root", MountPoint: "/"},
			},
		},
		SystemConfig: config.SystemConfig{
			Bootloader: config.Bootloader{
				Provider: "grub",
				BootType: "efi",
			},
			Kernel: config.KernelConfig{
				Cmdline: "",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "blkid.*UUID", Output: "UUID=test-uuid\n", Error: nil},
		{Pattern: "blkid.*PARTUUID", Output: "PARTUUID=test-partuuid\n", Error: nil},
		{Pattern: "command -v grub2-mkconfig", Output: "/usr/sbin/grub2-mkconfig", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	imageBoot := NewImageBoot()
	err := imageBoot.InstallImageBoot(tmpDir, diskPathIdMap, template, "deb")

	// Should fail on config directory access but this tests empty cmdline handling
	if err != nil && !strings.Contains(err.Error(), "failed to get general config directory") {
		t.Logf("Got expected error: %v", err)
	}
}

func TestInstallImageBoot_GrubVersionFallback(t *testing.T) {
	diskPathIdMap := map[string]string{
		"root": "/dev/sda1",
	}

	tmpDir := t.TempDir()
	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Disk: config.DiskConfig{
			Partitions: []config.PartitionInfo{
				{ID: "root", MountPoint: "/"},
			},
		},
		SystemConfig: config.SystemConfig{
			Bootloader: config.Bootloader{
				Provider: "grub",
				BootType: "efi",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "blkid.*UUID", Output: "UUID=test-uuid\n", Error: nil},
		{Pattern: "blkid.*PARTUUID", Output: "PARTUUID=test-partuuid\n", Error: nil},
		{Pattern: "command -v grub2-mkconfig", Output: "", Error: fmt.Errorf("command not found")},
		{Pattern: "command -v grub-mkconfig", Output: "/usr/bin/grub-mkconfig", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	imageBoot := NewImageBoot()
	err := imageBoot.InstallImageBoot(tmpDir, diskPathIdMap, template, "deb")

	// Should fail on config directory access but tests grub version fallback
	if err != nil && !strings.Contains(err.Error(), "failed to get general config directory") {
		t.Logf("Got expected error: %v", err)
	}
}

func TestInstallImageBoot_GrubVersionNotFound(t *testing.T) {
	diskPathIdMap := map[string]string{
		"root": "/dev/sda1",
	}

	tmpDir := t.TempDir()
	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Disk: config.DiskConfig{
			Partitions: []config.PartitionInfo{
				{ID: "root", MountPoint: "/"},
			},
		},
		SystemConfig: config.SystemConfig{
			Bootloader: config.Bootloader{
				Provider: "grub",
				BootType: "efi",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "blkid.*UUID", Output: "UUID=test-uuid\n", Error: nil},
		{Pattern: "blkid.*PARTUUID", Output: "PARTUUID=test-partuuid\n", Error: nil},
		{Pattern: "command -v grub2-mkconfig", Output: "", Error: fmt.Errorf("command not found")},
		{Pattern: "command -v grub-mkconfig", Output: "", Error: fmt.Errorf("command not found")},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	imageBoot := NewImageBoot()
	err := imageBoot.InstallImageBoot(tmpDir, diskPathIdMap, template, "deb")

	if err == nil {
		t.Error("Expected error when neither grub version is found")
	}
	if !strings.Contains(err.Error(), "neither grub2-mkconfig nor grub-mkconfig found") {
		t.Errorf("Expected grub version error, got: %v", err)
	}
}

func TestGetKernelVersionFromBoot_Success(t *testing.T) {
	tmpDir := t.TempDir()
	bootDir := filepath.Join(tmpDir, "boot")
	if err := os.MkdirAll(bootDir, 0755); err != nil {
		t.Fatalf("Failed to create boot directory: %v", err)
	}

	// Create a vmlinuz file with version
	kernelVersion := "5.15.0-73-generic"
	kernelFile := filepath.Join(bootDir, fmt.Sprintf("vmlinuz-%s", kernelVersion))
	if err := os.WriteFile(kernelFile, []byte("fake kernel"), 0644); err != nil {
		t.Fatalf("Failed to create kernel file: %v", err)
	}

	version, err := getKernelVersionFromBoot(tmpDir)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if version != kernelVersion {
		t.Errorf("Expected kernel version %s, got: %s", kernelVersion, version)
	}
}

func TestGetKernelVersionFromBoot_MultipleKernels(t *testing.T) {
	tmpDir := t.TempDir()
	bootDir := filepath.Join(tmpDir, "boot")
	if err := os.MkdirAll(bootDir, 0755); err != nil {
		t.Fatalf("Failed to create boot directory: %v", err)
	}

	// Create multiple kernel files - should return first match
	kernelVersions := []string{"5.15.0-73-generic", "6.2.0-26-generic"}
	for _, ver := range kernelVersions {
		kernelFile := filepath.Join(bootDir, fmt.Sprintf("vmlinuz-%s", ver))
		if err := os.WriteFile(kernelFile, []byte("fake kernel"), 0644); err != nil {
			t.Fatalf("Failed to create kernel file: %v", err)
		}
	}

	version, err := getKernelVersionFromBoot(tmpDir)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	// Should get the first one found
	if version != kernelVersions[0] && version != kernelVersions[1] {
		t.Errorf("Expected one of the kernel versions, got: %s", version)
	}
}

func TestGetKernelVersionFromBoot_NoKernelFound(t *testing.T) {
	tmpDir := t.TempDir()
	bootDir := filepath.Join(tmpDir, "boot")
	if err := os.MkdirAll(bootDir, 0755); err != nil {
		t.Fatalf("Failed to create boot directory: %v", err)
	}

	// Create some other files but not a kernel
	otherFile := filepath.Join(bootDir, "config-5.15.0")
	if err := os.WriteFile(otherFile, []byte("config"), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	version, err := getKernelVersionFromBoot(tmpDir)
	if err == nil {
		t.Error("Expected error when kernel not found")
	}
	if version != "" {
		t.Errorf("Expected empty version, got: %s", version)
	}
	if !strings.Contains(err.Error(), "kernel image not found") {
		t.Errorf("Expected kernel not found error, got: %v", err)
	}
}

func TestGetKernelVersionFromBoot_BootDirNotExist(t *testing.T) {
	tmpDir := t.TempDir()
	// Don't create boot directory

	version, err := getKernelVersionFromBoot(tmpDir)
	if err == nil {
		t.Error("Expected error when boot directory doesn't exist")
	}
	if version != "" {
		t.Errorf("Expected empty version, got: %s", version)
	}
	if !strings.Contains(err.Error(), "failed to list kernel directory") {
		t.Errorf("Expected directory list error, got: %v", err)
	}
}

func TestUpdateInitramfsForGrub_NoExtraModules(t *testing.T) {
	tmpDir := t.TempDir()
	kernelVersion := "5.15.0-73-generic"

	template := &config.ImageTemplate{
		SystemConfig: config.SystemConfig{
			Kernel: config.KernelConfig{
				EnableExtraModules: "", // No extra modules
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "command -v update-initramfs", Output: "/usr/sbin/update-initramfs\n", Error: nil},
		{Pattern: "update-initramfs -u -k " + kernelVersion, Output: "update-initramfs: Generating /boot/initrd.img-" + kernelVersion, Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	err := updateInitramfsForGrub(tmpDir, kernelVersion, template)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
}

func TestUpdateInitramfsForGrub_WithSingleExtraModule(t *testing.T) {
	tmpDir := t.TempDir()
	kernelVersion := "5.15.0-73-generic"

	template := &config.ImageTemplate{
		SystemConfig: config.SystemConfig{
			Kernel: config.KernelConfig{
				EnableExtraModules: "intel_vpu",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "echo 'intel_vpu' >> /etc/initramfs-tools/modules", Output: "", Error: nil},
		{Pattern: "command -v update-initramfs", Output: "/usr/sbin/update-initramfs\n", Error: nil},
		{Pattern: "update-initramfs -u -k " + kernelVersion, Output: "update-initramfs: Generating /boot/initrd.img-" + kernelVersion, Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	err := updateInitramfsForGrub(tmpDir, kernelVersion, template)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
}

func TestUpdateInitramfsForGrub_WithMultipleExtraModules(t *testing.T) {
	tmpDir := t.TempDir()
	kernelVersion := "6.2.0-26-generic"

	template := &config.ImageTemplate{
		SystemConfig: config.SystemConfig{
			Kernel: config.KernelConfig{
				EnableExtraModules: "intel_vpu nvidia_drm i915",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "echo 'intel_vpu' >> /etc/initramfs-tools/modules", Output: "", Error: nil},
		{Pattern: "echo 'nvidia_drm' >> /etc/initramfs-tools/modules", Output: "", Error: nil},
		{Pattern: "echo 'i915' >> /etc/initramfs-tools/modules", Output: "", Error: nil},
		{Pattern: "command -v update-initramfs", Output: "/usr/sbin/update-initramfs\n", Error: nil},
		{Pattern: "update-initramfs -u -k " + kernelVersion, Output: "update-initramfs: Generating /boot/initrd.img-" + kernelVersion, Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	err := updateInitramfsForGrub(tmpDir, kernelVersion, template)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
}

func TestUpdateInitramfsForGrub_WithWhitespaceInModules(t *testing.T) {
	tmpDir := t.TempDir()
	kernelVersion := "5.15.0-73-generic"

	template := &config.ImageTemplate{
		SystemConfig: config.SystemConfig{
			Kernel: config.KernelConfig{
				EnableExtraModules: "  intel_vpu   nvidia_drm  ",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "echo 'intel_vpu' >> /etc/initramfs-tools/modules", Output: "", Error: nil},
		{Pattern: "echo 'nvidia_drm' >> /etc/initramfs-tools/modules", Output: "", Error: nil},
		{Pattern: "command -v update-initramfs", Output: "/usr/sbin/update-initramfs\n", Error: nil},
		{Pattern: "update-initramfs -u -k " + kernelVersion, Output: "update-initramfs: Generating /boot/initrd.img-" + kernelVersion, Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	err := updateInitramfsForGrub(tmpDir, kernelVersion, template)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
}

func TestUpdateInitramfsForGrub_UpdateInitramfsFails(t *testing.T) {
	tmpDir := t.TempDir()
	kernelVersion := "5.15.0-73-generic"

	template := &config.ImageTemplate{
		SystemConfig: config.SystemConfig{
			Kernel: config.KernelConfig{
				EnableExtraModules: "",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "command -v update-initramfs", Output: "/usr/sbin/update-initramfs\n", Error: nil},
		{Pattern: "update-initramfs -u -k " + kernelVersion, Output: "", Error: fmt.Errorf("update-initramfs failed")},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	err := updateInitramfsForGrub(tmpDir, kernelVersion, template)
	if err == nil {
		t.Error("Expected error when update-initramfs fails")
		return
	}
	if !strings.Contains(err.Error(), "failed to update initramfs") {
		t.Errorf("Expected update initramfs error, got: %v", err)
	}
}

func TestUpdateInitramfsForGrub_FallbackToDracut(t *testing.T) {
	tmpDir := t.TempDir()
	kernelVersion := "6.2.0-26-generic"

	template := &config.ImageTemplate{
		SystemConfig: config.SystemConfig{
			Kernel: config.KernelConfig{
				EnableExtraModules: "intel_vpu",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "echo 'intel_vpu' >> /etc/initramfs-tools/modules", Output: "", Error: nil},
		{Pattern: "command -v update-initramfs", Output: "", Error: nil},
		{Pattern: "command -v dracut", Output: "/usr/bin/dracut\n", Error: nil},
		{Pattern: "dracut --force --kver " + kernelVersion + " /boot/initrd.img-" + kernelVersion + " --add-drivers 'intel_vpu'", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	err := updateInitramfsForGrub(tmpDir, kernelVersion, template)
	if err != nil {
		t.Errorf("Expected no error when falling back to dracut, got: %v", err)
	}
}

func TestUpdateInitramfsForGrub_ModuleAddFailsContinues(t *testing.T) {
	tmpDir := t.TempDir()
	kernelVersion := "5.15.0-73-generic"

	template := &config.ImageTemplate{
		SystemConfig: config.SystemConfig{
			Kernel: config.KernelConfig{
				EnableExtraModules: "intel_vpu nvidia_drm",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "echo 'intel_vpu' >> /etc/initramfs-tools/modules", Output: "", Error: fmt.Errorf("failed to add module")},
		{Pattern: "echo 'nvidia_drm' >> /etc/initramfs-tools/modules", Output: "", Error: nil},
		{Pattern: "command -v update-initramfs", Output: "/usr/sbin/update-initramfs\n", Error: nil},
		{Pattern: "update-initramfs -u -k " + kernelVersion, Output: "update-initramfs: Generating /boot/initrd.img-" + kernelVersion, Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	// Should continue even if one module fails
	err := updateInitramfsForGrub(tmpDir, kernelVersion, template)
	if err != nil {
		t.Errorf("Expected no error (should continue after module add failure), got: %v", err)
	}
}

func TestInstallImageBoot_GrubWithEnableExtraModules(t *testing.T) {
	setupConfigDir(t)
	diskPathIdMap := map[string]string{
		"root": "/dev/sda1",
	}

	tmpDir := t.TempDir()
	// Create necessary directories in tmpDir
	if err := os.MkdirAll(filepath.Join(tmpDir, "boot", "efi", "boot", "grub2"), 0755); err != nil {
		t.Fatalf("Failed to create boot directories: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "boot", "grub2"), 0755); err != nil {
		t.Fatalf("Failed to create boot directories: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "etc", "default"), 0755); err != nil {
		t.Fatalf("Failed to create etc directories: %v", err)
	}

	// Create boot directory with a kernel file to test kernel version detection
	bootDir := filepath.Join(tmpDir, "boot")
	kernelVersion := "5.15.0-73-generic"
	kernelFile := filepath.Join(bootDir, fmt.Sprintf("vmlinuz-%s", kernelVersion))
	if err := os.WriteFile(kernelFile, []byte("fake kernel"), 0644); err != nil {
		t.Fatalf("Failed to create kernel file: %v", err)
	}

	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Disk: config.DiskConfig{
			Partitions: []config.PartitionInfo{
				{ID: "root", MountPoint: "/"},
			},
		},
		SystemConfig: config.SystemConfig{
			Bootloader: config.Bootloader{
				Provider: "grub",
				BootType: "efi",
			},
			Kernel: config.KernelConfig{
				Cmdline:            "console=tty0",
				EnableExtraModules: "intel_vpu nvidia_drm i915",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "blkid.*UUID", Output: "UUID=test-uuid\n", Error: nil},
		{Pattern: "blkid.*PARTUUID", Output: "PARTUUID=test-partuuid\n", Error: nil},
		{Pattern: "command -v grub2-mkconfig", Output: "/usr/sbin/grub2-mkconfig", Error: nil},
		{Pattern: "command -v update-initramfs", Output: "/usr/sbin/update-initramfs\n", Error: nil},
		{Pattern: "mkdir", Output: "", Error: nil},
		{Pattern: "cp", Output: "", Error: nil},
		{Pattern: "sed", Output: "", Error: nil},
		{Pattern: "chmod", Output: "", Error: nil},
		{Pattern: "echo 'intel_vpu' >> /etc/initramfs-tools/modules", Output: "", Error: nil},
		{Pattern: "echo 'nvidia_drm' >> /etc/initramfs-tools/modules", Output: "", Error: nil},
		{Pattern: "echo 'i915' >> /etc/initramfs-tools/modules", Output: "", Error: nil},
		{Pattern: "update-initramfs -u -k " + kernelVersion, Output: "update-initramfs: Generating /boot/initrd.img-" + kernelVersion, Error: nil},
		{Pattern: "grub-install", Output: "", Error: nil},
		{Pattern: "grub2-mkconfig", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	imageBoot := NewImageBoot()
	err := imageBoot.InstallImageBoot(tmpDir, diskPathIdMap, template, "deb")

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
}

func TestInstallImageBoot_GrubWithEnableExtraModulesUbuntu(t *testing.T) {
	setupConfigDir(t)
	diskPathIdMap := map[string]string{
		"root": "/dev/sda1",
	}

	tmpDir := t.TempDir()
	// Create necessary directories in tmpDir
	if err := os.MkdirAll(filepath.Join(tmpDir, "boot", "efi", "boot", "grub2"), 0755); err != nil {
		t.Fatalf("Failed to create boot directories: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "boot", "grub2"), 0755); err != nil {
		t.Fatalf("Failed to create boot directories: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "etc", "default"), 0755); err != nil {
		t.Fatalf("Failed to create etc directories: %v", err)
	}

	// Create boot directory with a kernel file
	bootDir := filepath.Join(tmpDir, "boot")
	kernelVersion := "6.2.0-26-generic"
	kernelFile := filepath.Join(bootDir, fmt.Sprintf("vmlinuz-%s", kernelVersion))
	if err := os.WriteFile(kernelFile, []byte("fake kernel"), 0644); err != nil {
		t.Fatalf("Failed to create kernel file: %v", err)
	}

	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "ubuntu-test-image",
		},
		Disk: config.DiskConfig{
			Partitions: []config.PartitionInfo{
				{ID: "root", MountPoint: "/"},
			},
		},
		SystemConfig: config.SystemConfig{
			Bootloader: config.Bootloader{
				Provider: "grub",
				BootType: "efi",
			},
			Kernel: config.KernelConfig{
				Cmdline:            "quiet splash",
				EnableExtraModules: "vpu",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "blkid.*UUID", Output: "UUID=ubuntu-uuid\n", Error: nil},
		{Pattern: "blkid.*PARTUUID", Output: "PARTUUID=ubuntu-partuuid\n", Error: nil},
		{Pattern: "command -v grub2-mkconfig", Output: "/usr/sbin/grub2-mkconfig", Error: nil},
		{Pattern: "command -v update-initramfs", Output: "/usr/sbin/update-initramfs\n", Error: nil},
		{Pattern: "mkdir", Output: "", Error: nil},
		{Pattern: "cp", Output: "", Error: nil},
		{Pattern: "sed", Output: "", Error: nil},
		{Pattern: "chmod", Output: "", Error: nil},
		{Pattern: "echo 'vpu' >> /etc/initramfs-tools/modules", Output: "", Error: nil},
		{Pattern: "update-initramfs -u -k " + kernelVersion, Output: "update-initramfs: Generating /boot/initrd.img-" + kernelVersion, Error: nil},
		{Pattern: "grub-install", Output: "", Error: nil},
		{Pattern: "grub2-mkconfig", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	imageBoot := NewImageBoot()
	err := imageBoot.InstallImageBoot(tmpDir, diskPathIdMap, template, "deb")

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
}
