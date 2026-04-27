package imagedisc

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/os-image-composer/internal/config"
	"github.com/open-edge-platform/os-image-composer/internal/utils/shell"
)

func intPtr(v int) *int { return &v }

func TestIsDigit(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"valid_digits", "12345", true},
		{"single_digit", "7", true},
		{"empty_string", "", false},
		{"contains_letters", "123abc", false},
		{"contains_special_chars", "123-456", false},
		{"only_letters", "abc", false},
		{"zero", "0", true},
		{"leading_zero", "0123", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsDigit(tt.input)
			if result != tt.expected {
				t.Errorf("IsDigit(%s) = %v, expected %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestVerifyFileSize(t *testing.T) {
	tests := []struct {
		name        string
		input       interface{}
		expected    string
		expectError bool
		errorMsg    string
	}{
		{"valid_int", 100, "100MiB", false, ""},
		{"zero_string", "0", "0", false, ""},
		{"valid_mib", "500MiB", "500MiB", false, ""},
		{"valid_gib", "2GiB", "2GiB", false, ""},
		{"valid_kb", "1024KB", "1024KB", false, ""},
		{"invalid_suffix", "100XB", "", true, "file size suffix incorrect"},
		{"invalid_number", "abcMiB", "", true, "file size format incorrect"},
		{"invalid_format", "invalid", "", true, "file size format incorrect"},
		{"unsupported_type", 12.5, "", true, "unsupported fileSize type"},
		{"empty_string", "", "", true, "file size format incorrect"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := VerifyFileSize(tt.input)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for input %v, but got none", tt.input)
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s', but got: %v", tt.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for input %v, but got: %v", tt.input, err)
				}
				if result != tt.expected {
					t.Errorf("Expected %s, but got %s", tt.expected, result)
				}
			}
		})
	}
}

func TestTranslateSizeStrToBytes(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    uint64
		expectError bool
		errorMsg    string
	}{
		{"mib_conversion", "1MiB", 1048576, false, ""},
		{"gib_conversion", "1GiB", 1073741824, false, ""},
		{"kib_conversion", "1KiB", 1024, false, ""},
		{"mb_conversion", "1MB", 1000000, false, ""},
		{"gb_conversion", "1GB", 1000000000, false, ""},
		{"large_number", "100MiB", 104857600, false, ""},
		{"invalid_suffix", "1XB", 0, true, "file size suffix incorrect"},
		{"invalid_format", "invalid", 0, true, "size format incorrect"},
		{"no_number", "MiB", 0, true, "size format incorrect"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := TranslateSizeStrToBytes(tt.input)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for input %s, but got none", tt.input)
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s', but got: %v", tt.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for input %s, but got: %v", tt.input, err)
				}
				if result != tt.expected {
					t.Errorf("Expected %d, but got %d", tt.expected, result)
				}
			}
		})
	}
}

func TestCreateRawFile(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name         string
		filePath     string
		fileSize     string
		mockCommands []shell.MockCommand
		expectError  bool
		errorMsg     string
		shouldExist  bool
	}{
		{
			name:     "successful_creation",
			filePath: "/tmp/test/disk.img",
			fileSize: "100MiB",
			mockCommands: []shell.MockCommand{
				{Pattern: "fallocate", Output: "", Error: nil},
			},
			expectError: false,
			shouldExist: true,
		},
		{
			name:     "invalid_file_size",
			filePath: "/tmp/test/disk.img",
			fileSize: "invalidsize",
			mockCommands: []shell.MockCommand{
				{Pattern: "fallocate", Output: "", Error: nil},
			},
			expectError: true,
			errorMsg:    "file size format incorrect",
		},
		{
			name:     "fallocate_failure",
			filePath: "/tmp/test/disk.img",
			fileSize: "100MiB",
			mockCommands: []shell.MockCommand{
				{Pattern: "fallocate", Output: "", Error: fmt.Errorf("fallocate failed")},
			},
			expectError: true,
			errorMsg:    "failed to create raw file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			// Ensure temp directory exists
			tempDir := t.TempDir()
			testFilePath := filepath.Join(tempDir, "disk.img")

			err := CreateRawFile(testFilePath, tt.fileSize, false)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, but got none")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s', but got: %v", tt.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				}
			}
		})
	}
}

func TestGetDiskNameFromDiskPath(t *testing.T) {
	tests := []struct {
		name        string
		diskPath    string
		expected    string
		expectError bool
	}{
		{"valid_sda", "/dev/sda", "sda", false},
		{"valid_nvme", "/dev/nvme0n1", "nvme0n1", false},
		{"valid_loop", "/dev/loop0", "loop0", false},
		{"invalid_path", "/invalid/path", "", true},
		{"no_dev_prefix", "sda", "", true},
		{"empty_path", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := GetDiskNameFromDiskPath(tt.diskPath)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for path %s, but got none", tt.diskPath)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for path %s, but got: %v", tt.diskPath, err)
				}
				if result != tt.expected {
					t.Errorf("Expected %s, but got %s", tt.expected, result)
				}
			}
		})
	}
}

func TestDiskGetHwSectorSize(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name         string
		diskName     string
		mockCommands []shell.MockCommand
		expected     int
		expectError  bool
	}{
		{
			name:     "successful_read",
			diskName: "sda",
			mockCommands: []shell.MockCommand{
				{Pattern: "cat /sys/block/sda/queue/hw_sector_size", Output: "512\n", Error: nil},
			},
			expected:    512,
			expectError: false,
		},
		{
			name:     "command_failure",
			diskName: "sda",
			mockCommands: []shell.MockCommand{
				{Pattern: "cat /sys/block/sda/queue/hw_sector_size", Output: "", Error: fmt.Errorf("file not found")},
			},
			expected:    0,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			result, err := DiskGetHwSectorSize(tt.diskName)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				}
				if result != tt.expected {
					t.Errorf("Expected %d, but got %d", tt.expected, result)
				}
			}
		})
	}
}

func TestDiskGetPhysicalBlockSize(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name         string
		diskName     string
		mockCommands []shell.MockCommand
		expected     int
		expectError  bool
	}{
		{
			name:     "successful_read",
			diskName: "sda",
			mockCommands: []shell.MockCommand{
				{Pattern: "cat /sys/block/sda/queue/physical_block_size", Output: "4096\n", Error: nil},
			},
			expected:    4096,
			expectError: false,
		},
		{
			name:     "command_failure",
			diskName: "sda",
			mockCommands: []shell.MockCommand{
				{Pattern: "cat /sys/block/sda/queue/physical_block_size", Output: "", Error: fmt.Errorf("file not found")},
			},
			expected:    0,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			result, err := DiskGetPhysicalBlockSize(tt.diskName)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				}
				if result != tt.expected {
					t.Errorf("Expected %d, but got %d", tt.expected, result)
				}
			}
		})
	}
}

func TestDiskGetDevInfo(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name         string
		diskPath     string
		mockCommands []shell.MockCommand
		expectError  bool
		errorMsg     string
	}{
		{
			name:     "successful_read",
			diskPath: "/dev/sda",
			mockCommands: []shell.MockCommand{
				{Pattern: "lsblk /dev/sda", Output: `{"blockdevices":[{"name":"sda","path":"/dev/sda","type":"disk"}]}`, Error: nil},
			},
			expectError: false,
		},
		{
			name:     "command_failure",
			diskPath: "/dev/sda",
			mockCommands: []shell.MockCommand{
				{Pattern: "lsblk /dev/sda", Output: "", Error: fmt.Errorf("lsblk failed")},
			},
			expectError: true,
			errorMsg:    "lsblk failed",
		},
		{
			name:     "invalid_json",
			diskPath: "/dev/sda",
			mockCommands: []shell.MockCommand{
				{Pattern: "lsblk /dev/sda", Output: "invalid json", Error: nil},
			},
			expectError: true,
			errorMsg:    "invalid character",
		},
		{
			name:     "device_not_found",
			diskPath: "/dev/sda",
			mockCommands: []shell.MockCommand{
				{Pattern: "lsblk /dev/sda", Output: `{"blockdevices":[{"name":"sdb","path":"/dev/sdb","type":"disk"}]}`, Error: nil},
			},
			expectError: true,
			errorMsg:    "device not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			result, err := DiskGetDevInfo(tt.diskPath)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, but got none")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s', but got: %v", tt.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				}
				if result == nil {
					t.Error("Expected non-nil result")
				}
			}
		})
	}
}

func TestDiskGetPartitionsInfo(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name         string
		diskPath     string
		mockCommands []shell.MockCommand
		expectError  bool
		expectedLen  int
	}{
		{
			name:     "with_partitions",
			diskPath: "/dev/sda",
			mockCommands: []shell.MockCommand{
				{Pattern: "lsblk /dev/sda", Output: `{"blockdevices":[{"name":"sda1","path":"/dev/sda1","type":"part"},{"name":"sda2","path":"/dev/sda2","type":"part"}]}`, Error: nil},
			},
			expectError: false,
			expectedLen: 2,
		},
		{
			name:     "no_partitions",
			diskPath: "/dev/sda",
			mockCommands: []shell.MockCommand{
				{Pattern: "lsblk /dev/sda", Output: `{"blockdevices":[{"name":"sda","path":"/dev/sda","type":"disk"}]}`, Error: nil},
			},
			expectError: false,
			expectedLen: 0,
		},
		{
			name:     "command_failure",
			diskPath: "/dev/sda",
			mockCommands: []shell.MockCommand{
				{Pattern: "lsblk /dev/sda", Output: "", Error: fmt.Errorf("lsblk failed")},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			result, err := DiskGetPartitionsInfo(tt.diskPath)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				}
				if len(result) != tt.expectedLen {
					t.Errorf("Expected %d partitions, but got %d", tt.expectedLen, len(result))
				}
			}
		})
	}
}

func TestPartitionTypeStrToGUID(t *testing.T) {
	tests := []struct {
		name          string
		partitionType string
		expectedGUID  string
		expectError   bool
	}{
		{"linux_type", "linux", "0fc63daf-8483-4772-8e79-3d69d8477de4", false},
		{"esp_type", "esp", "c12a7328-f81f-11d2-ba4b-00a0c93ec93b", false},
		{"bios_type", "bios", "21686148-6449-6e6f-744e-656564454649", false},
		{"invalid_type", "invalid", "", true},
		{"empty_type", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := PartitionTypeStrToGUID(tt.partitionType)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for type %s, but got none", tt.partitionType)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for type %s, but got: %v", tt.partitionType, err)
				}
				if result != tt.expectedGUID {
					t.Errorf("Expected GUID %s, but got %s", tt.expectedGUID, result)
				}
			}
		})
	}
}

func TestPartitionGUIDToTypeStr(t *testing.T) {
	tests := []struct {
		name          string
		partitionGUID string
		expectedType  string
		expectError   bool
	}{
		{"linux_guid", "0fc63daf-8483-4772-8e79-3d69d8477de4", "linux", false},
		{"esp_guid", "c12a7328-f81f-11d2-ba4b-00a0c93ec93b", "esp", false},
		{"invalid_guid", "invalid-guid", "", true},
		{"empty_guid", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := PartitionGUIDToTypeStr(tt.partitionGUID)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for GUID %s, but got none", tt.partitionGUID)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for GUID %s, but got: %v", tt.partitionGUID, err)
				}
				if result != tt.expectedType {
					t.Errorf("Expected type %s, but got %s", tt.expectedType, result)
				}
			}
		})
	}
}

func TestIsDiskPartitionExist(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name         string
		diskPath     string
		mockCommands []shell.MockCommand
		expected     bool
		expectError  bool
	}{
		{
			name:     "has_partitions",
			diskPath: "/dev/sda",
			mockCommands: []shell.MockCommand{
				{Pattern: "fdisk -l /dev/sda", Output: "Disk /dev/sda: 372.61 GiB, 400088457216 bytes, 781422768 sectors\n/dev/sda1 * 2048 204799 202752 99M EFI System", Error: nil},
			},
			expected:    true,
			expectError: false,
		},
		{
			name:     "no_partitions",
			diskPath: "/dev/sda",
			mockCommands: []shell.MockCommand{
				{Pattern: "fdisk -l /dev/sda", Output: "Disk /dev/sda: 372.61 GiB, 400088457216 bytes, 781422768 sectors\nSector size (logical/physical): 512 bytes / 512 bytes", Error: nil},
			},
			expected:    false,
			expectError: false,
		},
		{
			name:     "command_failure",
			diskPath: "/dev/sda",
			mockCommands: []shell.MockCommand{
				{Pattern: "fdisk -l /dev/sda", Output: "", Error: fmt.Errorf("fdisk failed")},
			},
			expected:    false,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			result, err := IsDiskPartitionExist(tt.diskPath)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				}
				if result != tt.expected {
					t.Errorf("Expected %v, but got %v", tt.expected, result)
				}
			}
		})
	}
}

func TestWipePartitions(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name         string
		diskPath     string
		mockCommands []shell.MockCommand
		expectError  bool
		errorMsg     string
	}{
		{
			name:     "successful_wipe",
			diskPath: "/dev/sda",
			mockCommands: []shell.MockCommand{
				{Pattern: "wipefs", Output: "", Error: nil},
				{Pattern: "sync", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:     "wipefs_failure",
			diskPath: "/dev/sda",
			mockCommands: []shell.MockCommand{
				{Pattern: "wipefs", Output: "", Error: fmt.Errorf("wipefs failed")},
			},
			expectError: true,
			errorMsg:    "failed to wipe disk",
		},
		{
			name:     "sync_failure",
			diskPath: "/dev/sda",
			mockCommands: []shell.MockCommand{
				{Pattern: "wipefs", Output: "", Error: nil},
				{Pattern: "sync", Output: "", Error: fmt.Errorf("sync failed")},
			},
			expectError: true,
			errorMsg:    "failed to sync after wiping disk",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			err := WipePartitions(tt.diskPath)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, but got none")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s', but got: %v", tt.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				}
			}
		})
	}
}

func TestGetUUID(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name         string
		partPath     string
		mockCommands []shell.MockCommand
		expected     string
		expectError  bool
	}{
		{
			name:     "successful_uuid",
			partPath: "/dev/sda1",
			mockCommands: []shell.MockCommand{
				{Pattern: "blkid /dev/sda1 -s UUID -o value", Output: "12345678-1234-1234-1234-123456789abc\n", Error: nil},
			},
			expected:    "12345678-1234-1234-1234-123456789abc",
			expectError: false,
		},
		{
			name:     "command_failure",
			partPath: "/dev/sda1",
			mockCommands: []shell.MockCommand{
				{Pattern: "blkid /dev/sda1 -s UUID -o value", Output: "", Error: fmt.Errorf("blkid failed")},
			},
			expected:    "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			result, err := GetUUID(tt.partPath)

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

func TestGetPartUUID(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name         string
		partPath     string
		mockCommands []shell.MockCommand
		expected     string
		expectError  bool
	}{
		{
			name:     "successful_partuuid",
			partPath: "/dev/sda1",
			mockCommands: []shell.MockCommand{
				{Pattern: "blkid /dev/sda1 -s PARTUUID -o value", Output: "12345678-1234-1234-1234-123456789abc\n", Error: nil},
			},
			expected:    "12345678-1234-1234-1234-123456789abc",
			expectError: false,
		},
		{
			name:     "command_failure",
			partPath: "/dev/sda1",
			mockCommands: []shell.MockCommand{
				{Pattern: "blkid /dev/sda1 -s PARTUUID -o value", Output: "", Error: fmt.Errorf("blkid failed")},
			},
			expected:    "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			result, err := GetPartUUID(tt.partPath)

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

func TestDiskPartitionsCreate(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name               string
		diskPath           string
		partitionsList     []config.PartitionInfo
		partitionTableType string
		mockCommands       []shell.MockCommand
		expectError        bool
		errorMsg           string
		expectedDevices    int
	}{
		{
			name:     "gpt_single_partition",
			diskPath: "/dev/sda",
			partitionsList: []config.PartitionInfo{
				{
					ID:     "root",
					Name:   "root",
					Start:  "1MiB",
					End:    "100MiB",
					FsType: "ext4",
					Type:   "linux",
					Index:  intPtr(1),
				},
			},
			partitionTableType: "gpt",
			mockCommands: []shell.MockCommand{
				{Pattern: ".*fdisk.*sda.*", Output: "Disk /dev/sda: 1 GiB", Error: nil},
				{Pattern: ".*label.*gpt.*sfdisk.*", Output: "", Error: nil},
				{Pattern: ".*hw_sector_size", Output: "512", Error: nil},
				{Pattern: ".*physical_block_size", Output: "4096", Error: nil},
				{Pattern: ".*sgdisk.*sda.*", Output: "", Error: nil},
				{Pattern: ".*partx.*sda.*", Output: "", Error: nil},
				{Pattern: ".*mkfs.*ext4.*sda.*", Output: "", Error: nil},
			},
			expectError:     false,
			expectedDevices: 1,
		},
		{
			name:     "gpt_partition_index",
			diskPath: "/dev/sda",
			partitionsList: []config.PartitionInfo{
				{
					ID:     "root",
					Name:   "root",
					Start:  "1MiB",
					End:    "100MiB",
					FsType: "ext4",
					Type:   "linux",
					Index:  intPtr(14),
				},
			},
			partitionTableType: "gpt",
			mockCommands: []shell.MockCommand{
				{Pattern: ".*fdisk.*sda.*", Output: "Disk /dev/sda: 1 GiB", Error: nil},
				{Pattern: ".*label.*gpt.*sfdisk.*", Output: "", Error: nil},
				{Pattern: ".*hw_sector_size", Output: "512", Error: nil},
				{Pattern: ".*physical_block_size", Output: "4096", Error: nil},
				{Pattern: ".*sgdisk.*sda.*", Output: "", Error: nil},
				{Pattern: ".*partx.*sda.*", Output: "", Error: nil},
				{Pattern: ".*mkfs.*ext4.*sda.*", Output: "", Error: nil},
			},
			expectError:     false,
			expectedDevices: 1,
		},
		{
			name:     "mbr_single_partition",
			diskPath: "/dev/sda",
			partitionsList: []config.PartitionInfo{
				{
					ID:     "root",
					Name:   "root",
					Start:  "1MiB",
					End:    "100MiB",
					FsType: "ext4",
				},
			},
			partitionTableType: "mbr",
			mockCommands: []shell.MockCommand{
				{Pattern: ".*fdisk.*sda.*", Output: "Disk /dev/sda: 1 GiB", Error: nil},
				{Pattern: ".*label.*dos.*sfdisk.*", Output: "", Error: nil},
				{Pattern: ".*hw_sector_size", Output: "512", Error: nil},
				{Pattern: ".*physical_block_size", Output: "4096", Error: nil},
				{Pattern: ".*sfdisk.*append.*sda.*", Output: "", Error: nil},
				{Pattern: ".*partx.*sda.*", Output: "", Error: nil},
				{Pattern: ".*mkfs.*ext4.*sda.*", Output: "", Error: nil},
			},
			expectError:     false,
			expectedDevices: 1,
		},
		{
			name:     "partition_creation_failure",
			diskPath: "/dev/sda",
			partitionsList: []config.PartitionInfo{
				{
					ID:     "root",
					Start:  "1MiB",
					End:    "100MiB",
					FsType: "ext4",
				},
			},
			partitionTableType: "gpt",
			mockCommands: []shell.MockCommand{
				{Pattern: ".*fdisk.*sda.*", Output: "Disk /dev/sda: 1 GiB", Error: nil},
				{Pattern: ".*label.*gpt.*sfdisk.*", Output: "", Error: fmt.Errorf("sgdisk failed")},
				{Pattern: ".*hw_sector_size", Output: "512", Error: nil},
				{Pattern: ".*physical_block_size", Output: "4096", Error: nil},
			},
			expectError: true,
			errorMsg:    "failed to create partition",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			result, err := DiskPartitionsCreate(tt.diskPath, tt.partitionsList, tt.partitionTableType)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, but got none")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s', but got: %v", tt.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				}
				if len(result) != tt.expectedDevices {
					t.Errorf("Expected %d devices, but got %d", tt.expectedDevices, len(result))
				}
			}
		})
	}
}

func TestGetPartitionLabel(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name         string
		diskPartDev  string
		mockCommands []shell.MockCommand
		expected     string
		expectError  bool
	}{
		{
			name:        "successful_label",
			diskPartDev: "/dev/sda1",
			mockCommands: []shell.MockCommand{
				{Pattern: "blkid /dev/sda1 -s PARTLABEL -o value", Output: "EFI System\n", Error: nil},
			},
			expected:    "EFI System",
			expectError: false,
		},
		{
			name:        "command_failure",
			diskPartDev: "/dev/sda1",
			mockCommands: []shell.MockCommand{
				{Pattern: "blkid /dev/sda1 -s PARTLABEL -o value", Output: "", Error: fmt.Errorf("blkid failed")},
			},
			expected:    "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			result, err := GetPartitionLabel(tt.diskPartDev)

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

func TestTranslateBytesToSizeStr(t *testing.T) {
	tests := []struct {
		name     string
		input    uint64
		expected string
	}{
		{"bytes", 500, "500B"},
		{"kib", 1024, "1.02KB"},
		{"mib", 1048576, "1.05MB"},
		{"gib", 1073741824, "1.07GB"},
		{"mixed_mib", 1572864, "1.57MB"},
		{"zero", 0, "0B"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TranslateBytesToSizeStr(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %s, but got %s", tt.expected, result)
			}
		})
	}
}

func TestCheckDiskIOStats(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name         string
		diskPath     string
		mockCommands []shell.MockCommand
		expected     bool
		expectError  bool
	}{
		{
			name:     "io_busy",
			diskPath: "/dev/sda",
			mockCommands: []shell.MockCommand{
				{Pattern: "cat /proc/diskstats", Output: "   8       0 sda 100 0 200 50 0 0 0 0 1 100 100\n", Error: nil},
			},
			expected:    true,
			expectError: false,
		},
		{
			name:     "io_idle",
			diskPath: "/dev/sda",
			mockCommands: []shell.MockCommand{
				{Pattern: "cat /proc/diskstats", Output: "   8       0 sda 100 0 200 50 0 0 0 0 0 100 100\n", Error: nil},
			},
			expected:    false,
			expectError: false,
		},
		{
			name:     "command_failure",
			diskPath: "/dev/sda",
			mockCommands: []shell.MockCommand{
				{Pattern: "cat /proc/diskstats", Output: "", Error: fmt.Errorf("cat failed")},
			},
			expected:    false,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			result, err := CheckDiskIOStats(tt.diskPath)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				}
				if result != tt.expected {
					t.Errorf("Expected %v, but got %v", tt.expected, result)
				}
			}
		})
	}
}

func TestTranslateSectorToBytes(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name         string
		diskName     string
		sectorOffset int
		mockCommands []shell.MockCommand
		expected     int
		expectError  bool
	}{
		{
			name:         "valid_translation",
			diskName:     "sda",
			sectorOffset: 100,
			mockCommands: []shell.MockCommand{
				{Pattern: "cat /sys/block/sda/queue/hw_sector_size", Output: "512\n", Error: nil},
			},
			expected:    51200,
			expectError: false,
		},
		{
			name:         "command_failure",
			diskName:     "sda",
			sectorOffset: 100,
			mockCommands: []shell.MockCommand{
				{Pattern: "cat /sys/block/sda/queue/hw_sector_size", Output: "", Error: fmt.Errorf("failed")},
			},
			expected:    0,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			result, err := TranslateSectorToBytes(tt.diskName, tt.sectorOffset)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				}
				if result != tt.expected {
					t.Errorf("Expected %d, but got %d", tt.expected, result)
				}
			}
		})
	}
}

func TestGetAlignedSectorOffset(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name         string
		diskName     string
		sectorOffset int
		mockCommands []shell.MockCommand
		expected     int
		expectError  bool
	}{
		{
			name:         "aligned",
			diskName:     "sda",
			sectorOffset: 8,
			mockCommands: []shell.MockCommand{
				{Pattern: "cat /sys/block/sda/queue/hw_sector_size", Output: "512\n", Error: nil},
				{Pattern: "cat /sys/block/sda/queue/physical_block_size", Output: "4096\n", Error: nil},
			},
			expected:    8,
			expectError: false,
		},
		{
			name:         "unaligned",
			diskName:     "sda",
			sectorOffset: 1,
			mockCommands: []shell.MockCommand{
				{Pattern: "cat /sys/block/sda/queue/hw_sector_size", Output: "512\n", Error: nil},
				{Pattern: "cat /sys/block/sda/queue/physical_block_size", Output: "4096\n", Error: nil},
			},
			expected:    8,
			expectError: false,
		},
		{
			name:         "same_size",
			diskName:     "sda",
			sectorOffset: 10,
			mockCommands: []shell.MockCommand{
				{Pattern: "cat /sys/block/sda/queue/hw_sector_size", Output: "512\n", Error: nil},
				{Pattern: "cat /sys/block/sda/queue/physical_block_size", Output: "512\n", Error: nil},
			},
			expected:    10,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			result, err := GetAlignedSectorOffset(tt.diskName, tt.sectorOffset)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				}
				if result != tt.expected {
					t.Errorf("Expected %d, but got %d", tt.expected, result)
				}
			}
		})
	}
}

func TestSystemBlockDevices(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name         string
		mockCommands []shell.MockCommand
		expectedLen  int
		expectError  bool
	}{
		{
			name: "found_devices",
			mockCommands: []shell.MockCommand{
				{Pattern: "lsblk", Output: `{"blockdevices":[{"name":"sda","size":10737418240,"model":"Virtual Disk"}]}`, Error: nil},
			},
			expectedLen: 1,
			expectError: false,
		},
		{
			name: "no_devices",
			mockCommands: []shell.MockCommand{
				{Pattern: "lsblk", Output: `{"blockdevices":[]}`, Error: nil},
			},
			expectedLen: 0,
			expectError: true,
		},
		{
			name: "command_failure",
			mockCommands: []shell.MockCommand{
				{Pattern: "lsblk", Output: "", Error: fmt.Errorf("lsblk failed")},
			},
			expectedLen: 0,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			result, err := SystemBlockDevices()

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				}
				if len(result) != tt.expectedLen {
					t.Errorf("Expected %d devices, but got %d", tt.expectedLen, len(result))
				}
			}
		})
	}
}

func TestBootPartitionConfig(t *testing.T) {
	tests := []struct {
		name               string
		bootType           string
		partitionTableType string
		expectedMount      string
		expectError        bool
	}{
		{"efi", EFIPartitionType, "", "/boot/efi", false},
		{"legacy_gpt", LegacyPartitionType, PartitionTableTypeGpt, "", false},
		{"legacy_mbr", LegacyPartitionType, PartitionTableTypeMbr, "", false},
		{"unknown_boot", "unknown", "", "", true},
		{"unknown_table", LegacyPartitionType, "unknown", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mountPoint, _, _, err := BootPartitionConfig(tt.bootType, tt.partitionTableType)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				}
				if mountPoint != tt.expectedMount {
					t.Errorf("Expected mount point %s, but got %s", tt.expectedMount, mountPoint)
				}
			}
		})
	}
}

func TestGetSectorOffsetFromSize(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	// Mock commands for DiskGetHwSectorSize and DiskGetPhysicalBlockSize
	mockCommands := []shell.MockCommand{
		{Pattern: "cat /sys/block/sda/queue/hw_sector_size", Output: "512", Error: nil},
		{Pattern: "cat /sys/block/sda/queue/physical_block_size", Output: "512", Error: nil},
		{Pattern: "cat /sys/block/sdb/queue/hw_sector_size", Output: "512", Error: nil},
		{Pattern: "cat /sys/block/sdb/queue/physical_block_size", Output: "4096", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	tests := []struct {
		diskName string
		sizeStr  string
		expected uint64
		wantErr  bool
	}{
		{"sda", "1MiB", 2048, false}, // 1048576 / 512 = 2048
		{"sda", "1KiB", 2, false},    // 1024 / 512 = 2
		{"sdb", "1MiB", 2048, false}, // 1048576 / 512 = 2048 (aligned to 4096)
		{"sdb", "4KiB", 8, false},    // 4096 / 512 = 8
		{"sdb", "5KiB", 16, false},   // 5120 -> aligned to 8192 -> 8192 / 512 = 16
		{"sda", "invalid", 0, true},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s-%s", tt.diskName, tt.sizeStr), func(t *testing.T) {
			got, err := getSectorOffsetFromSize(tt.diskName, tt.sizeStr)
			if (err != nil) != tt.wantErr {
				t.Errorf("getSectorOffsetFromSize() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.expected {
				t.Errorf("getSectorOffsetFromSize() = %v, want %v", got, tt.expected)
			}
		})
	}
}
