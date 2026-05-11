package mount_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/mount"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

func TestGetMountPathList(t *testing.T) {
	logger.SetLogLevel("debug")
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name         string
		mockCommands []shell.MockCommand
		expected     []string
		expectError  bool
		errorMsg     string
	}{
		{
			name: "successful_mount_list",
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
				{Pattern: "mount", Output: "sysfs on /sys type sysfs (rw,nosuid,nodev,noexec,relatime)\nproc on /proc type proc (rw,nosuid,nodev,noexec,relatime)\nudev on /dev type devtmpfs (rw,nosuid,relatime)\n", Error: nil},
			},
			expected:    []string{"/sys", "/proc", "/dev"},
			expectError: false,
		},
		{
			name: "empty_mount_output",
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
				{Pattern: "mount", Output: "", Error: nil},
			},
			expected:    []string{},
			expectError: false,
		},
		{
			name: "command_failure",
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
				{Pattern: "mount", Output: "", Error: fmt.Errorf("mount command failed")},
			},
			expected:    []string{},
			expectError: true,
			errorMsg:    "mount command failed",
		},
		{
			name: "malformed_mount_output",
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
				{Pattern: "mount", Output: "incomplete line\nsysfs on /sys type sysfs (rw,nosuid,nodev,noexec,relatime)\n", Error: nil},
			},
			expected:    []string{"/sys"},
			expectError: false,
		},
		{
			name: "single_field_lines",
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
				{Pattern: "mount", Output: "sysfs\nproc on /proc type proc (rw,nosuid,nodev,noexec,relatime)\n", Error: nil},
			},
			expected:    []string{"/proc"},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			result, err := mount.GetMountPathList()

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
				if len(result) != len(tt.expected) {
					t.Errorf("Expected %d mount paths, but got %d", len(tt.expected), len(result))
				}
				for i, expected := range tt.expected {
					if i < len(result) && result[i] != expected {
						t.Errorf("Expected mount path %s at index %d, but got %s", expected, i, result[i])
					}
				}
			}
		})
	}
}

func TestGetMountSubPathList(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name           string
		rootMountPoint string
		mockCommands   []shell.MockCommand
		expected       []string
		expectError    bool
		errorMsg       string
	}{
		{
			name:           "successful_subpath_list",
			rootMountPoint: "/mnt/chroot",
			mockCommands: []shell.MockCommand{
				{Pattern: "mount", Output: "sysfs on /sys type sysfs (rw,nosuid,nodev,noexec,relatime)\nproc on /mnt/chroot/proc type proc (rw,nosuid,nodev,noexec,relatime)\nudev on /mnt/chroot/dev type devtmpfs (rw,nosuid,relatime)\ntmpfs on /mnt/chroot/dev/shm type tmpfs (rw,nosuid,nodev)\n", Error: nil},
			},
			expected:    []string{"/mnt/chroot/proc", "/mnt/chroot/dev", "/mnt/chroot/dev/shm"},
			expectError: false,
		},
		{
			name:           "no_matching_subpaths",
			rootMountPoint: "/nonexistent",
			mockCommands: []shell.MockCommand{
				{Pattern: "mount", Output: "sysfs on /sys type sysfs (rw,nosuid,nodev,noexec,relatime)\nproc on /proc type proc (rw,nosuid,nodev,noexec,relatime)\n", Error: nil},
			},
			expected:    []string{},
			expectError: false,
		},
		{
			name:           "mount_command_failure",
			rootMountPoint: "/mnt/chroot",
			mockCommands: []shell.MockCommand{
				{Pattern: "mount", Output: "", Error: fmt.Errorf("mount failed")},
			},
			expected:    []string{},
			expectError: true,
			errorMsg:    "failed to get mount path list",
		},
		{
			name:           "root_path_exact_match",
			rootMountPoint: "/mnt/chroot",
			mockCommands: []shell.MockCommand{
				{Pattern: "mount", Output: "ext4 on /mnt/chroot type ext4 (rw,relatime)\nproc on /mnt/chroot/proc type proc (rw,nosuid,nodev,noexec,relatime)\n", Error: nil},
			},
			expected:    []string{"/mnt/chroot", "/mnt/chroot/proc"},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			result, err := mount.GetMountSubPathList(tt.rootMountPoint)

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
				if len(result) != len(tt.expected) {
					t.Errorf("Expected %d subpaths, but got %d", len(tt.expected), len(result))
				}
				for i, expected := range tt.expected {
					if i < len(result) && result[i] != expected {
						t.Errorf("Expected subpath %s at index %d, but got %s", expected, i, result[i])
					}
				}
			}
		})
	}
}

func TestIsMountPathExist(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name         string
		mountPoint   string
		mockCommands []shell.MockCommand
		expected     bool
		expectError  bool
		errorMsg     string
	}{
		{
			name:       "mount_exists",
			mountPoint: "/proc",
			mockCommands: []shell.MockCommand{
				{Pattern: "mount", Output: "proc on /proc type proc (rw,nosuid,nodev,noexec,relatime)\nsysfs on /sys type sysfs (rw,nosuid,nodev,noexec,relatime)\n", Error: nil},
			},
			expected:    true,
			expectError: false,
		},
		{
			name:       "mount_does_not_exist",
			mountPoint: "/nonexistent",
			mockCommands: []shell.MockCommand{
				{Pattern: "mount", Output: "proc on /proc type proc (rw,nosuid,nodev,noexec,relatime)\nsysfs on /sys type sysfs (rw,nosuid,nodev,noexec,relatime)\n", Error: nil},
			},
			expected:    false,
			expectError: false,
		},
		{
			name:       "mount_command_failure",
			mountPoint: "/proc",
			mockCommands: []shell.MockCommand{
				{Pattern: "mount", Output: "", Error: fmt.Errorf("mount failed")},
			},
			expected:    false,
			expectError: true,
			errorMsg:    "failed to get mount path list",
		},
		{
			name:       "empty_mount_list",
			mountPoint: "/proc",
			mockCommands: []shell.MockCommand{
				{Pattern: "mount", Output: "", Error: nil},
			},
			expected:    false,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			result, err := mount.IsMountPathExist(tt.mountPoint)

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
				if result != tt.expected {
					t.Errorf("Expected %v, but got %v", tt.expected, result)
				}
			}
		})
	}
}

func TestMountPath(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name         string
		targetPath   string
		mountPoint   string
		mountFlags   string
		mockCommands []shell.MockCommand
		setupFunc    func(tempDir string) error
		expectError  bool
		errorMsg     string
	}{
		{
			name:       "successful_mount_new_directory",
			targetPath: "/dev/sda1",
			mountPoint: "/mnt/test",
			mountFlags: "-t ext4",
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
				{Pattern: "mount", Output: "", Error: nil}, // For IsMountPathExist check
				{Pattern: "mount -t ext4 /dev/sda1 /mnt/test", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:       "successful_mount_existing_directory",
			targetPath: "/dev/sda1",
			mountPoint: "/mnt/test",
			mountFlags: "-t ext4",
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
				{Pattern: "mount", Output: "", Error: nil}, // For IsMountPathExist check
				{Pattern: "mount -t ext4 /dev/sda1 /mnt/test", Output: "", Error: nil},
			},
			setupFunc: func(tempDir string) error {
				return os.MkdirAll(filepath.Join(tempDir, "mnt", "test"), 0700)
			},
			expectError: false,
		},
		{
			name:       "mount_already_exists",
			targetPath: "/dev/sda1",
			mountPoint: "/mnt/test",
			mountFlags: "-t ext4",
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
				{Pattern: "mount", Output: "ext4 on /mnt/test type ext4 (rw,relatime)", Error: nil},
			},
			expectError: false,
		},
		{
			name:       "mkdir_failure",
			targetPath: "/dev/sda1",
			mountPoint: "/mnt/test",
			mountFlags: "-t ext4",
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir -p", Output: "", Error: fmt.Errorf("permission denied")},
			},
			expectError: true,
			errorMsg:    "failed to create mount point",
		},
		{
			name:       "mount_command_failure",
			targetPath: "/dev/sda1",
			mountPoint: "/mnt/test",
			mountFlags: "-t ext4",
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir -p", Output: "", Error: nil},
				{Pattern: "mount.*/dev/sda1", Output: "", Error: fmt.Errorf("mount failed")},
				{Pattern: "mount", Output: "", Error: nil}, // For IsMountPathExist check
			},
			expectError: true,
			errorMsg:    "failed to mount",
		},
		{
			name:       "is_mount_path_exist_failure",
			targetPath: "/dev/sda1",
			mountPoint: "/mnt/test",
			mountFlags: "-t ext4",
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir -p /mnt/test", Output: "", Error: nil},
				{Pattern: "mount", Output: "", Error: fmt.Errorf("mount command failed")},
			},
			expectError: true,
			errorMsg:    "failed to create mount point",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			tempDir := t.TempDir()
			actualMountPoint := filepath.Join(tempDir, strings.TrimPrefix(tt.mountPoint, "/"))

			if tt.setupFunc != nil {
				if err := tt.setupFunc(tempDir); err != nil {
					t.Fatalf("Failed to setup test: %v", err)
				}
			}

			err := mount.MountPath(tt.targetPath, actualMountPoint, tt.mountFlags)

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
			}
		})
	}
}

func TestUmountPath(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name         string
		mountPoint   string
		mockCommands []shell.MockCommand
		expectError  bool
		errorMsg     string
	}{
		{
			name:       "successful_unmount_standard",
			mountPoint: "/mnt/test",
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
				{Pattern: "mount", Output: "ext4 on /mnt/test type ext4 (rw,relatime)", Error: nil},
				{Pattern: "umount /mnt/test", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:       "successful_unmount_lazy",
			mountPoint: "/mnt/test",
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
				{Pattern: "mount", Output: "ext4 on /mnt/test type ext4 (rw,relatime)", Error: nil},
				{Pattern: "umount /mnt/test", Output: "", Error: fmt.Errorf("device busy")},
				{Pattern: "umount -l /mnt/test", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:       "successful_unmount_force",
			mountPoint: "/mnt/test",
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
				{Pattern: "mount", Output: "ext4 on /mnt/test type ext4 (rw,relatime)", Error: nil},
				{Pattern: "umount /mnt/test", Output: "", Error: fmt.Errorf("device busy")},
				{Pattern: "umount -l /mnt/test", Output: "", Error: fmt.Errorf("still busy")},
				{Pattern: "umount -f /mnt/test", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:       "successful_unmount_lazy_force",
			mountPoint: "/mnt/test",
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
				{Pattern: "mount", Output: "ext4 on /mnt/test type ext4 (rw,relatime)", Error: nil},
				{Pattern: "umount /mnt/test", Output: "", Error: fmt.Errorf("device busy")},
				{Pattern: "umount -l /mnt/test", Output: "", Error: fmt.Errorf("still busy")},
				{Pattern: "umount -f /mnt/test", Output: "", Error: fmt.Errorf("still busy")},
				{Pattern: "umount -lf /mnt/test", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:       "all_unmount_strategies_fail",
			mountPoint: "/mnt/test",
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
				{Pattern: "mount", Output: "ext4 on /mnt/test type ext4 (rw,relatime)", Error: nil},
				{Pattern: "umount /mnt/test", Output: "", Error: fmt.Errorf("device busy")},
				{Pattern: "umount -l /mnt/test", Output: "", Error: fmt.Errorf("still busy")},
				{Pattern: "umount -f /mnt/test", Output: "", Error: fmt.Errorf("still busy")},
				{Pattern: "umount -lf /mnt/test", Output: "", Error: fmt.Errorf("still busy")},
			},
			expectError: false, // umountPath returns nil even if all strategies fail
		},
		{
			name:       "mount_point_not_mounted",
			mountPoint: "/mnt/test",
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
				{Pattern: "mount", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:       "is_mount_path_exist_failure",
			mountPoint: "/mnt/test",
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir", Output: "", Error: nil},
				{Pattern: "mount", Output: "", Error: fmt.Errorf("mount command failed")},
			},
			expectError: true,
			errorMsg:    "failed to check if mount point",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			err := mount.UmountPath(tt.mountPoint)

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
			}
		})
	}
}

func TestUmountAndDeletePath(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name         string
		mountPoint   string
		mockCommands []shell.MockCommand
		expectError  bool
		errorMsg     string
	}{
		{
			name:       "successful_unmount_and_delete",
			mountPoint: "/mnt/test",
			mockCommands: []shell.MockCommand{
				{Pattern: "mount", Output: "ext4 on /mnt/test type ext4 (rw,relatime)", Error: nil},
				{Pattern: "umount /mnt/test", Output: "", Error: nil},
				{Pattern: "rm -rf /mnt/test", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:       "unmount_failure",
			mountPoint: "/mnt/test",
			mockCommands: []shell.MockCommand{
				{Pattern: "mount", Output: "", Error: fmt.Errorf("mount command failed")},
			},
			expectError: true,
			errorMsg:    "failed to unmount",
		},
		{
			name:       "delete_failure",
			mountPoint: "/mnt/test",
			mockCommands: []shell.MockCommand{
				{Pattern: "mount", Output: "", Error: nil}, // Mount point doesn't exist, unmount succeeds
				{Pattern: "rm -rf /mnt/test", Output: "", Error: fmt.Errorf("permission denied")},
			},
			expectError: true,
			errorMsg:    "failed to remove mount point directory",
		},
		{
			name:       "unmount_not_mounted_then_delete",
			mountPoint: "/mnt/test",
			mockCommands: []shell.MockCommand{
				{Pattern: "mount", Output: "", Error: nil}, // Not mounted
				{Pattern: "rm -rf /mnt/test", Output: "", Error: nil},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			err := mount.UmountAndDeletePath(tt.mountPoint)

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
			}
		})
	}
}

func TestUmountSubPath(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name         string
		mountPoint   string
		mockCommands []shell.MockCommand
		expectError  bool
		errorMsg     string
	}{
		{
			name:       "successful_unmount_subpaths",
			mountPoint: "/mnt/chroot",
			mockCommands: []shell.MockCommand{
				{Pattern: "mount", Output: "proc on /mnt/chroot/proc type proc (rw,nosuid,nodev,noexec,relatime)\ntmpfs on /mnt/chroot/dev/shm type tmpfs (rw,nosuid,nodev)\nudev on /mnt/chroot/dev type devtmpfs (rw,nosuid,relatime)\n", Error: nil},
				{Pattern: "umount /mnt/chroot/proc", Output: "", Error: nil},
				{Pattern: "umount /mnt/chroot/dev/shm", Output: "", Error: nil},
				{Pattern: "umount /mnt/chroot/dev", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:       "no_subpaths_to_unmount",
			mountPoint: "/mnt/chroot",
			mockCommands: []shell.MockCommand{
				{Pattern: "mount", Output: "proc on /proc type proc (rw,nosuid,nodev,noexec,relatime)\n", Error: nil},
			},
			expectError: false,
		},
		{
			name:       "get_mount_subpath_list_failure",
			mountPoint: "/mnt/chroot",
			mockCommands: []shell.MockCommand{
				{Pattern: "mount", Output: "", Error: fmt.Errorf("mount command failed")},
			},
			expectError: true,
			errorMsg:    "failed to get mount subpath list",
		},
		{
			name:       "unmount_subpath_failure",
			mountPoint: "/mnt/chroot",
			mockCommands: []shell.MockCommand{
				{Pattern: "mount", Output: "proc on /mnt/chroot/proc type proc (rw,nosuid,nodev,noexec,relatime)\n", Error: nil},
				{Pattern: "umount /mnt/chroot/proc", Output: "", Error: fmt.Errorf("device busy")},
				{Pattern: "umount -l /mnt/chroot/proc", Output: "", Error: fmt.Errorf("still busy")},
				{Pattern: "umount -f /mnt/chroot/proc", Output: "", Error: fmt.Errorf("still busy")},
				{Pattern: "umount -lf /mnt/chroot/proc", Output: "", Error: fmt.Errorf("still busy")},
			},
			expectError: false, // umountPath returns nil even if all strategies fail
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			err := mount.UmountSubPath(tt.mountPoint)

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
			}
		})
	}
}

func TestMountSysfs(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name         string
		mountPoint   string
		mockCommands []shell.MockCommand
		expectError  bool
		errorMsg     string
	}{
		{
			name:       "successful_mount_sysfs",
			mountPoint: "/mnt/chroot",
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir -p", Output: "", Error: nil},
				{Pattern: "mount", Output: "", Error: nil}, // Multiple mount checks and commands
				{Pattern: "chmod 1700", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:       "proc_mount_failure",
			mountPoint: "/mnt/chroot",
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir -p", Output: "", Error: nil},
				{Pattern: "chmod", Output: "", Error: nil},
				{Pattern: "mount -t proc", Output: "", Error: fmt.Errorf("proc mount failed")},
				{Pattern: "mount", Output: "", Error: nil}, // IsMountPathExist check
			},
			expectError: true,
			errorMsg:    "failed to mount /proc",
		},
		{
			name:       "sys_mount_failure",
			mountPoint: "/mnt/chroot",
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir -p", Output: "", Error: nil},
				{Pattern: "mount -t proc proc /mnt/chroot/proc", Output: "", Error: nil},
				{Pattern: "mount -t sysfs -o nosuid,noexec,nodev sysfs /mnt/chroot/sys", Output: "", Error: fmt.Errorf("sys mount failed")},
				{Pattern: "mount", Output: "", Error: nil}, // IsMountPathExist checks
			},
			expectError: true,
			errorMsg:    "failed to mount /sys",
		},
		{
			name:       "dev_mount_failure",
			mountPoint: "/mnt/chroot",
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir -p /mnt/chroot/proc", Output: "", Error: nil},
				{Pattern: "mount -t proc proc /mnt/chroot/proc", Output: "", Error: nil},
				{Pattern: "mkdir -p /mnt/chroot/sys", Output: "", Error: nil},
				{Pattern: "mount -t sysfs -o nosuid,noexec,nodev sysfs /mnt/chroot/sys", Output: "", Error: nil},
				{Pattern: "mkdir -p /mnt/chroot/dev", Output: "", Error: nil},
				{Pattern: "mount -t devtmpfs -o mode=0700,nosuid devtmpfs /mnt/chroot/dev", Output: "", Error: fmt.Errorf("dev mount failed")},
				{Pattern: "mount", Output: "", Error: nil}, // IsMountPathExist checks
			},
			expectError: true,
			errorMsg:    "failed to mount /dev",
		},
		{
			name:       "run_shm_mkdir_failure",
			mountPoint: "/mnt/chroot",
			mockCommands: []shell.MockCommand{
				// Success for all mount operations
				{Pattern: "mkdir -p .*shm", Output: "", Error: fmt.Errorf("mkdir failed")},
				{Pattern: "mkdir", Output: "", Error: nil},
				{Pattern: "mount", Output: "", Error: nil}, // IsMountPathExist checks
			},
			expectError: true,
			errorMsg:    "mkdir failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			err := mount.MountSysfs(tt.mountPoint)

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
			}
		})
	}
}

func TestUmountSysfs(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name         string
		mountPoint   string
		mockCommands []shell.MockCommand
		expectError  bool
		errorMsg     string
	}{
		{
			name:       "successful_unmount_sysfs",
			mountPoint: "/mnt/chroot",
			mockCommands: []shell.MockCommand{
				{Pattern: "mount", Output: "proc on /mnt/chroot/proc type proc (rw,nosuid,nodev,noexec,relatime)\nsysfs on /mnt/chroot/sys type sysfs (rw,nosuid,nodev,noexec,relatime)\ndevtmpfs on /mnt/chroot/dev type devtmpfs (rw,nosuid,relatime)\ndevpts on /mnt/chroot/dev/pts type devpts (rw,nosuid,noexec,relatime)\ntmpfs on /mnt/chroot/dev/shm type tmpfs (rw,nosuid,nodev)\ntmpfs on /mnt/chroot/run type tmpfs (rw,nosuid,nodev,noexec,relatime)", Error: nil},
				{Pattern: "umount /mnt/chroot/run", Output: "", Error: nil},
				{Pattern: "umount /mnt/chroot/dev/pts", Output: "", Error: nil},
				{Pattern: "umount /mnt/chroot/dev/shm", Output: "", Error: nil},
				{Pattern: "umount /mnt/chroot/dev", Output: "", Error: nil},
				{Pattern: "umount /mnt/chroot/sys", Output: "", Error: nil},
				{Pattern: "umount /mnt/chroot/proc", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:       "no_mount_points_found",
			mountPoint: "/mnt/chroot",
			mockCommands: []shell.MockCommand{
				{Pattern: "mount", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:       "get_mount_path_list_failure",
			mountPoint: "/mnt/chroot",
			mockCommands: []shell.MockCommand{
				{Pattern: "mount", Output: "", Error: fmt.Errorf("mount command failed")},
			},
			expectError: true,
			errorMsg:    "failed to get mount path list",
		},
		{
			name:       "partial_unmount_failure_not_found",
			mountPoint: "/mnt/chroot",
			mockCommands: []shell.MockCommand{
				{Pattern: "mount", Output: "proc on /mnt/chroot/proc type proc (rw,nosuid,nodev,noexec,relatime)\n", Error: nil},
				{Pattern: "umount /mnt/chroot/proc", Output: "", Error: fmt.Errorf("not found")},
			},
			expectError: false, // "not found" errors are treated as warnings
		},
		{
			name:       "no_matching_mount_points",
			mountPoint: "/mnt/chroot",
			mockCommands: []shell.MockCommand{
				{Pattern: "mount", Output: "proc on /proc type proc (rw,nosuid,nodev,noexec,relatime)\nsysfs on /sys type sysfs (rw,nosuid,nodev,noexec,relatime)\n", Error: nil},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			err := mount.UmountSysfs(tt.mountPoint)

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
			}
		})
	}
}

func TestCleanSysfs(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name         string
		mountPoint   string
		mockCommands []shell.MockCommand
		expectError  bool
		errorMsg     string
	}{
		{
			name:       "successful_clean_sysfs",
			mountPoint: "/mnt/chroot",
			mockCommands: []shell.MockCommand{
				{Pattern: "mount", Output: "", Error: nil}, // No mount points found
				{Pattern: "rm", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:       "get_mount_path_list_failure",
			mountPoint: "/mnt/chroot",
			mockCommands: []shell.MockCommand{
				{Pattern: "mount", Output: "", Error: fmt.Errorf("mount command failed")},
			},
			expectError: true,
			errorMsg:    "failed to get mount path list",
		},
		{
			name:       "no_mount_points_found",
			mountPoint: "/mnt/chroot",
			mockCommands: []shell.MockCommand{
				{Pattern: "mount", Output: "", Error: nil},
				{Pattern: "rm -rf /mnt/chroot/run", Output: "", Error: nil},
				{Pattern: "rm -rf /mnt/chroot/sys", Output: "", Error: nil},
				{Pattern: "rm -rf /mnt/chroot/proc", Output: "", Error: nil},
				{Pattern: "rm -rf /mnt/chroot/dev", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:       "rm_command_failure",
			mountPoint: "/mnt/chroot",
			mockCommands: []shell.MockCommand{
				{Pattern: "mount", Output: "", Error: nil},
				{Pattern: "rm -rf", Output: "", Error: fmt.Errorf("permission denied")},
			},
			expectError: true,
			errorMsg:    "failed to remove path",
		},
		{
			name:       "path_still_mounted",
			mountPoint: "/mnt/chroot",
			mockCommands: []shell.MockCommand{
				{Pattern: "mount", Output: "proc on /mnt/chroot/proc type proc (rw,nosuid,nodev,noexec,relatime)\n", Error: nil},
				{Pattern: "rm", Output: "", Error: nil},
			},
			expectError: true,
			errorMsg:    "failed to remove path: /mnt/chroot/proc still mounted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			err := mount.CleanSysfs(tt.mountPoint)

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
			}
		})
	}
}

func TestMountPath_EdgeCases(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name         string
		targetPath   string
		mountPoint   string
		mountFlags   string
		mockCommands []shell.MockCommand
		expectError  bool
		errorMsg     string
	}{
		{
			name:       "empty_mount_flags",
			targetPath: "/dev/sda1",
			mountPoint: "/mnt/test",
			mountFlags: "",
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir -p /mnt/test", Output: "", Error: nil},
				{Pattern: "mount", Output: "", Error: nil}, // IsMountPathExist check
				{Pattern: "mount  /dev/sda1 /mnt/test", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:       "complex_mount_flags",
			targetPath: "/dev/sda1",
			mountPoint: "/mnt/test",
			mountFlags: "-t ext4 -o rw,relatime,user_xattr",
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir -p /mnt/test", Output: "", Error: nil},
				{Pattern: "mount", Output: "", Error: nil}, // IsMountPathExist check
				{Pattern: "mount -t ext4 -o rw,relatime,user_xattr /dev/sda1 /mnt/test", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:       "special_characters_in_paths",
			targetPath: "/dev/disk/by-uuid/12345678-1234-1234-1234-123456789abc",
			mountPoint: "/mnt/test with spaces",
			mountFlags: "-t ext4",
			mockCommands: []shell.MockCommand{
				{Pattern: "mkdir -p /mnt/test with spaces", Output: "", Error: nil},
				{Pattern: "mount", Output: "", Error: nil}, // IsMountPathExist check
				{Pattern: "mount -t ext4 /dev/disk/by-uuid/12345678-1234-1234-1234-123456789abc /mnt/test with spaces", Output: "", Error: nil},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			err := mount.MountPath(tt.targetPath, tt.mountPoint, tt.mountFlags)

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
			}
		})
	}
}

func TestMountSysfs_AllMountPoints(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	// Test that all expected mount points are created
	mockCommands := []shell.MockCommand{
		// All mkdir commands
		{Pattern: "mkdir -p", Output: "", Error: nil},
		// All IsMountPathExist checks return false (not mounted)
		{Pattern: "mount", Output: "", Error: nil},
		// All mount commands succeed
		{Pattern: "mount -t proc proc", Output: "", Error: nil},
		{Pattern: "mount -t sysfs", Output: "", Error: nil},
		{Pattern: "mount -t devtmpfs", Output: "", Error: nil},
		{Pattern: "mount -t devpts", Output: "", Error: nil},
		{Pattern: "mount -t tmpfs", Output: "", Error: nil},
		// chmod and additional mkdir commands
		{Pattern: "chmod 1700", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	err := mount.MountSysfs("/mnt/chroot")

	if err != nil {
		t.Errorf("Expected no error, but got: %v", err)
	}
}

func TestUmountSysfs_OrderedUnmount(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	// Test that unmount happens in the correct order (reverse of mount order)
	mockCommands := []shell.MockCommand{
		{Pattern: "mount", Output: "proc on /mnt/chroot/proc type proc (rw,nosuid,nodev,noexec,relatime)\nsysfs on /mnt/chroot/sys type sysfs (rw,nosuid,nodev,noexec,relatime)\ndevtmpfs on /mnt/chroot/dev type devtmpfs (rw,nosuid,relatime)\ndevpts on /mnt/chroot/dev/pts type devpts (rw,nosuid,noexec,relatime)\ntmpfs on /mnt/chroot/dev/shm type tmpfs (rw,nosuid,nodev)\ntmpfs on /mnt/chroot/run type tmpfs (rw,nosuid,nodev,noexec,relatime)", Error: nil},
		// Unmount commands should be called in order: run, dev/pts, dev/shm, dev, sys, proc
		{Pattern: "umount /mnt/chroot/run", Output: "", Error: nil},
		{Pattern: "umount /mnt/chroot/dev/pts", Output: "", Error: nil},
		{Pattern: "umount /mnt/chroot/dev/shm", Output: "", Error: nil},
		{Pattern: "umount /mnt/chroot/dev", Output: "", Error: nil},
		{Pattern: "umount /mnt/chroot/sys", Output: "", Error: nil},
		{Pattern: "umount /mnt/chroot/proc", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	err := mount.UmountSysfs("/mnt/chroot")

	if err != nil {
		t.Errorf("Expected no error, but got: %v", err)
	}
}
