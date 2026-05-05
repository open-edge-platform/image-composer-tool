package imagedisc

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

func TestNewLoopDev(t *testing.T) {
	if NewLoopDev() == nil {
		t.Fatal("expected non-nil loop device")
	}
}

func TestLoopSetupCreate(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name          string
		output        string
		cmdErr        error
		expectError   bool
		errorContains string
	}{
		{name: "success", output: "/dev/loop7\n"},
		{name: "command error", cmdErr: fmt.Errorf("losetup failed"), expectError: true, errorContains: "losetup failed"},
		{name: "unexpected output", output: "not-a-loop-device", expectError: true, errorContains: "failed to create loopback device"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor([]shell.MockCommand{
				{Pattern: "losetup --direct-io=on --show -f -P", Output: tt.output, Error: tt.cmdErr},
			})

			got, err := loopSetupCreate("/tmp/test.raw")
			if tt.expectError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Fatalf("expected error to contain %q, got %v", tt.errorContains, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != "/dev/loop7" {
				t.Fatalf("expected /dev/loop7, got %q", got)
			}
		})
	}
}

func TestLoopSetupCreateEmptyRawDisk(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	t.Run("invalid size", func(t *testing.T) {
		_, err := loopSetupCreateEmptyRawDisk(filepath.Join(t.TempDir(), "disk.raw"), "bad-size")
		if err == nil {
			t.Fatal("expected error for invalid size")
		}
	})

	t.Run("missing file after create", func(t *testing.T) {
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{{Pattern: "fallocate", Output: "", Error: nil}})
		_, err := loopSetupCreateEmptyRawDisk(filepath.Join(t.TempDir(), "disk.raw"), "16MiB")
		if err == nil || !strings.Contains(err.Error(), "can't find") {
			t.Fatalf("expected can't find error, got %v", err)
		}
	})
}

func TestLoopSetupDelete(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	t.Run("success", func(t *testing.T) {
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{{Pattern: "losetup -d /dev/loop7", Output: "", Error: nil}})
		ld := &LoopDev{}
		if err := ld.LoopSetupDelete("/dev/loop7"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("command error", func(t *testing.T) {
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{{Pattern: "losetup -d /dev/loop8", Output: "", Error: fmt.Errorf("delete failed")}})
		ld := &LoopDev{}
		err := ld.LoopSetupDelete("/dev/loop8")
		if err == nil || !strings.Contains(err.Error(), "failed to delete loop device") {
			t.Fatalf("expected wrapped delete error, got %v", err)
		}
	})
}

func TestLoopDevGetInfoAndHelpers(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	t.Run("get info success", func(t *testing.T) {
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{{
			Pattern: "losetup -l /dev/loop1 --json",
			Output:  `{"loopdevices":[{"name":"/dev/loop1","back-file":"/tmp/a.raw"}]}`,
			Error:   nil,
		}})

		info, err := LoopDevGetInfo("/dev/loop1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if info["name"] != "/dev/loop1" {
			t.Fatalf("unexpected info payload: %#v", info)
		}
	})

	t.Run("get info invalid json", func(t *testing.T) {
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{{Pattern: "losetup -l /dev/loop2 --json", Output: "not-json", Error: nil}})
		if _, err := LoopDevGetInfo("/dev/loop2"); err == nil {
			t.Fatal("expected JSON error")
		}
	})

	t.Run("get info no devices", func(t *testing.T) {
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{{Pattern: "losetup -l /dev/loop3 --json", Output: `{"loopdevices":[]}`, Error: nil}})
		_, err := LoopDevGetInfo("/dev/loop3")
		if err == nil || !strings.Contains(err.Error(), "no loop device info found") {
			t.Fatalf("expected no info error, got %v", err)
		}
	})

	t.Run("back-file success", func(t *testing.T) {
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{{
			Pattern: "losetup -l /dev/loop4 --json",
			Output:  `{"loopdevices":[{"back-file":"/tmp/b.raw"}]}`,
			Error:   nil,
		}})

		backFile, err := LoopDevGetBackFile("/dev/loop4")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if backFile != "/tmp/b.raw" {
			t.Fatalf("unexpected back-file: %q", backFile)
		}
	})

	t.Run("back-file missing", func(t *testing.T) {
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{{
			Pattern: "losetup -l /dev/loop5 --json",
			Output:  `{"loopdevices":[{"name":"/dev/loop5"}]}`,
			Error:   nil,
		}})

		_, err := LoopDevGetBackFile("/dev/loop5")
		if err == nil || !strings.Contains(err.Error(), "back-file not found") {
			t.Fatalf("expected back-file error, got %v", err)
		}
	})

	t.Run("get all info", func(t *testing.T) {
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{{
			Pattern: "losetup -l --json",
			Output:  `{"loopdevices":[{"name":"/dev/loop1"},{"name":"/dev/loop2"}]}`,
			Error:   nil,
		}})

		list, err := LoopDevGetInfoAll()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(list) != 2 {
			t.Fatalf("expected 2 loop devices, got %d", len(list))
		}
	})

	t.Run("get all info invalid json", func(t *testing.T) {
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{{Pattern: "losetup -l --json", Output: "bad-json", Error: nil}})
		if _, err := LoopDevGetInfoAll(); err == nil {
			t.Fatal("expected JSON error")
		}
	})
}

func TestGetLoopDevPathFromLoopDevPart(t *testing.T) {
	tests := []struct {
		input       string
		expected    string
		expectError bool
	}{
		{input: "/dev/loop0p1", expected: "/dev/loop0"},
		{input: "/dev/loop12p8", expected: "/dev/loop12"},
		{input: "/dev/sda1", expectError: true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := GetLoopDevPathFromLoopDevPart(tt.input)
			if tt.expectError {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestCreateRawImageLoopDev(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	t.Run("create loop device failure", func(t *testing.T) {
		ld := &LoopDev{}
		template := &config.ImageTemplate{Disk: config.DiskConfig{Size: "bad-size"}}
		_, _, err := ld.CreateRawImageLoopDev(filepath.Join(t.TempDir(), "x.raw"), template)
		if err == nil || !strings.Contains(err.Error(), "failed to create loop device") {
			t.Fatalf("expected wrapped create loop error, got %v", err)
		}
	})

	t.Run("successful create with empty partition list", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "disk.raw")

		// Use shell mocks for all external commands touched in this path.
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{
			{Pattern: "fallocate -l 1MiB", Output: "", Error: nil},
			{Pattern: "losetup --direct-io=on --show -f -P", Output: "/dev/loop7\n", Error: nil},
			{Pattern: "fdisk -l /dev/loop7", Output: "Disk /dev/loop7: 1 GiB", Error: nil},
			{Pattern: "label: gpt", Output: "", Error: nil},
		})

		// Ensure the file exists for loopSetupCreateEmptyRawDisk stat check.
		if err := os.WriteFile(filePath, []byte("raw"), 0600); err != nil {
			t.Fatalf("failed to create placeholder raw file: %v", err)
		}

		ld := &LoopDev{}
		template := &config.ImageTemplate{Disk: config.DiskConfig{Size: "1MiB", PartitionTableType: "gpt", Partitions: []config.PartitionInfo{}}}

		loopPath, partMap, err := ld.CreateRawImageLoopDev(filePath, template)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if loopPath != "/dev/loop7" {
			t.Fatalf("expected /dev/loop7, got %q", loopPath)
		}
		if len(partMap) != 0 {
			t.Fatalf("expected empty partition map, got %#v", partMap)
		}
	})
}

func TestDiskPartitionDelete(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	t.Run("invalid index", func(t *testing.T) {
		if err := diskPartitionDelete("/dev/sda", 0); err == nil {
			t.Fatal("expected invalid partition number error")
		}
	})

	t.Run("delete command failure", func(t *testing.T) {
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{{Pattern: "sfdisk --delete /dev/sda 1", Output: "", Error: fmt.Errorf("delete failed")}})
		err := diskPartitionDelete("/dev/sda", 1)
		if err == nil || !strings.Contains(err.Error(), "failed to delete partition 1") {
			t.Fatalf("expected wrapped delete error, got %v", err)
		}
	})

	t.Run("success with non-fatal partx failure", func(t *testing.T) {
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{
			{Pattern: "sfdisk --delete /dev/sdb 2", Output: "", Error: nil},
			{Pattern: "partx -d --nr 2 /dev/sdb", Output: "", Error: fmt.Errorf("already removed")},
		})
		if err := diskPartitionDelete("/dev/sdb", 2); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})
}
