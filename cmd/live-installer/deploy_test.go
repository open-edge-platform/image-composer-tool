package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/image/imagedisc"
	"github.com/open-edge-platform/image-composer-tool/internal/image/isopayload"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

// recordedCmd captures one call through the execCmdFn/execStreamFn/execCmdWithInputFn seams.
type recordedCmd struct {
	input string
	cmd   string
}

func resetDeploySeams(t *testing.T) {
	t.Helper()
	origResolveInstallDisk := resolveInstallDiskFn
	origSystemBlockDevices := systemBlockDevicesFn
	origResolveExistingPartitions := resolveExistingPartitionsFn
	origCanonicalDiskPath := canonicalDiskPathFn
	origExecCmd := execCmdFn
	origExecStream := execStreamFn
	origExecCmdWithInput := execCmdWithInputFn
	origMount := mountFn
	origUmount := umountAndDeleteFn
	// Default to an identity canonicalization so tests that don't care about
	// disk-path aliasing don't need to mock the underlying lsblk call.
	canonicalDiskPathFn = func(diskPath string) (string, error) { return diskPath, nil }
	t.Cleanup(func() {
		resolveInstallDiskFn = origResolveInstallDisk
		systemBlockDevicesFn = origSystemBlockDevices
		resolveExistingPartitionsFn = origResolveExistingPartitions
		canonicalDiskPathFn = origCanonicalDiskPath
		execCmdFn = origExecCmd
		execStreamFn = origExecStream
		execCmdWithInputFn = origExecCmdWithInput
		mountFn = origMount
		umountAndDeleteFn = origUmount
	})
}

func TestParseDDBytesCopied(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		output      string
		want        int64
		expectError bool
	}{
		{name: "simple", output: "1048576 bytes copied, 0.1 s, 10 MB/s", want: 1048576},
		{name: "with_parens_and_progress_lines", output: "512 bytes copied\n4194304 bytes (4.2 MB, 4.0 MiB) copied, 0.5 s, 8.4 MB/s", want: 4194304},
		{name: "no_match", output: "no useful output here", expectError: true},
		{name: "empty", output: "", expectError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseDDBytesCopied(tt.output)
			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error, got %d", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGrowablePartition(t *testing.T) {
	t.Parallel()
	partitions := []config.PartitionInfo{
		{ID: "esp", End: "512MiB"},
		{ID: "root", End: "0"},
	}

	got, err := growablePartition(partitions)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "root" {
		t.Fatalf("got partition %q, want root", got.ID)
	}

	_, err = growablePartition([]config.PartitionInfo{{ID: "esp", End: "512MiB"}})
	if err == nil {
		t.Fatal("expected error when no partition declares end: \"0\"")
	}
}

func TestPartitionByMountPoint(t *testing.T) {
	t.Parallel()
	partitions := []config.PartitionInfo{
		{ID: "esp", MountPoint: "/boot/efi"},
		{ID: "root", MountPoint: "/"},
	}

	got, err := partitionByMountPoint(partitions, "/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "root" {
		t.Fatalf("got partition %q, want root", got.ID)
	}

	_, err = partitionByMountPoint(partitions, "/var")
	if err == nil {
		t.Fatal("expected error when no partition has the requested mountPoint")
	}
}

func TestResetIdentityFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	mustMkdirAll(t, filepath.Join(root, "etc", "ssh"))
	mustMkdirAll(t, filepath.Join(root, "var", "lib", "dbus"))
	mustMkdirAll(t, filepath.Join(root, "var", "lib", "systemd"))

	mustWriteFile(t, filepath.Join(root, "etc", "machine-id"), "abc123\n")
	mustWriteFile(t, filepath.Join(root, "var", "lib", "dbus", "machine-id"), "abc123\n")
	mustWriteFile(t, filepath.Join(root, "var", "lib", "systemd", "random-seed"), "seed")
	mustWriteFile(t, filepath.Join(root, "etc", "ssh", "ssh_host_rsa_key"), "key")
	mustWriteFile(t, filepath.Join(root, "etc", "ssh", "ssh_host_rsa_key.pub"), "pub")

	if err := resetIdentityFiles(root); err != nil {
		t.Fatalf("resetIdentityFiles: %v", err)
	}

	machineID, err := os.ReadFile(filepath.Join(root, "etc", "machine-id"))
	if err != nil {
		t.Fatalf("expected etc/machine-id to still exist (truncated), got: %v", err)
	}
	if len(machineID) != 0 {
		t.Fatalf("expected etc/machine-id to be truncated, got %q", machineID)
	}

	for _, p := range []string{
		filepath.Join(root, "var", "lib", "dbus", "machine-id"),
		filepath.Join(root, "var", "lib", "systemd", "random-seed"),
		filepath.Join(root, "etc", "ssh", "ssh_host_rsa_key"),
		filepath.Join(root, "etc", "ssh", "ssh_host_rsa_key.pub"),
	} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, stat err = %v", p, err)
		}
	}
}

func TestResetIdentityFiles_ToleratesMissingOptionalFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustMkdirAll(t, filepath.Join(root, "etc"))
	mustWriteFile(t, filepath.Join(root, "etc", "machine-id"), "abc123\n")

	if err := resetIdentityFiles(root); err != nil {
		t.Fatalf("expected missing optional identity files to be tolerated, got: %v", err)
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// writeFixturePayload writes rawContent to <isoRoot>/payload/payload.raw and a
// matching manifest to <isoRoot>/payload/payload.manifest.yaml, returning the
// manifest actually written. mutate, if non-nil, is applied to the manifest
// after its digests/sizes are computed from rawContent and before it is
// written, letting a test spoil a specific field (a wrong checksum, an
// inflated RawBytes) to exercise a deploy failure path.
func writeFixturePayload(t *testing.T, isoRoot string, rawContent []byte, mutate func(*isopayload.Manifest)) isopayload.Manifest {
	t.Helper()
	payloadDir := filepath.Join(isoRoot, "payload")
	mustMkdirAll(t, payloadDir)
	payloadPath := filepath.Join(payloadDir, "payload.raw")
	mustWriteFile(t, payloadPath, string(rawContent))

	sha, err := isopayload.FileSHA256(payloadPath)
	if err != nil {
		t.Fatalf("checksum fixture payload: %v", err)
	}
	m := isopayload.Manifest{
		SchemaVersion:    isopayload.ManifestSchemaVersion,
		Compression:      config.PayloadCompressionNone,
		CompressedSha256: sha,
		CompressedBytes:  int64(len(rawContent)),
		RawSha256:        sha,
		RawBytes:         int64(len(rawContent)),
	}
	if mutate != nil {
		mutate(&m)
	}
	if err := isopayload.Write(m, filepath.Join(payloadDir, "payload.manifest.yaml")); err != nil {
		t.Fatalf("write fixture manifest: %v", err)
	}
	return m
}

func TestCheckDiskCapacity(t *testing.T) {
	resetDeploySeams(t)

	tests := []struct {
		name          string
		diskPath      string
		requiredBytes int64
		devices       []imagedisc.SystemBlockDevice
		listErr       error
		expectError   bool
	}{
		{
			name:          "disk_large_enough",
			diskPath:      "/dev/sda",
			requiredBytes: 1000,
			devices:       []imagedisc.SystemBlockDevice{{DevicePath: "/dev/sda", RawDiskSize: 2000}},
		},
		{
			name:          "disk_too_small",
			diskPath:      "/dev/sda",
			requiredBytes: 3000,
			devices:       []imagedisc.SystemBlockDevice{{DevicePath: "/dev/sda", RawDiskSize: 2000}},
			expectError:   true,
		},
		{
			name:          "disk_not_found",
			diskPath:      "/dev/sdb",
			requiredBytes: 100,
			devices:       []imagedisc.SystemBlockDevice{{DevicePath: "/dev/sda", RawDiskSize: 2000}},
			expectError:   true,
		},
		{
			name:          "enumeration_failure",
			diskPath:      "/dev/sda",
			requiredBytes: 100,
			listErr:       fmt.Errorf("lsblk failed"),
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			systemBlockDevicesFn = func() ([]imagedisc.SystemBlockDevice, error) {
				return tt.devices, tt.listErr
			}

			err := checkDiskCapacity(tt.diskPath, tt.requiredBytes)
			if tt.expectError {
				if err == nil {
					t.Fatal("expected error, got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestWritePayloadToDisk(t *testing.T) {
	resetDeploySeams(t)

	tests := []struct {
		name           string
		compression    string
		wantPipe       bool
		wantDecompress string
	}{
		{name: "none", compression: config.PayloadCompressionNone, wantPipe: false},
		{name: "zstd", compression: config.PayloadCompressionZstd, wantPipe: true, wantDecompress: "zstd -d -c"},
		{name: "xz", compression: config.PayloadCompressionXz, wantPipe: true, wantDecompress: "xz -d -c"},
		{name: "gz", compression: config.PayloadCompressionGz, wantPipe: true, wantDecompress: "gzip -d -c"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var streamed []string
			var synced bool

			execStreamFn = func(cmd string, sudo bool, chrootPath string, env []string) (string, error) {
				streamed = append(streamed, cmd)
				return "1024 bytes (1.0 kB) copied, 0.01 s, 100 kB/s", nil
			}
			execCmdFn = func(cmd string, sudo bool, chrootPath string, env []string) (string, error) {
				if cmd == "sync" {
					synced = true
				}
				return "", nil
			}

			m := isopayload.Manifest{Compression: tt.compression, RawBytes: 1024}
			err := writePayloadToDisk("/payload/payload.raw", "/dev/sda", m)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !synced {
				t.Fatal("expected sync to be called after the write")
			}
			if len(streamed) != 1 {
				t.Fatalf("expected exactly one streamed command, got %d: %v", len(streamed), streamed)
			}
			cmd := streamed[0]

			if strings.Contains(cmd, "||") {
				t.Fatalf("emitted command must never contain '||': %q", cmd)
			}
			if strings.Contains(cmd, "|") != tt.wantPipe {
				t.Fatalf("cmd %q: pipe presence = %v, want %v", cmd, strings.Contains(cmd, "|"), tt.wantPipe)
			}
			if tt.wantDecompress != "" && !strings.HasPrefix(cmd, tt.wantDecompress) {
				t.Fatalf("cmd %q does not start with expected decompressor %q", cmd, tt.wantDecompress)
			}
			if !strings.Contains(cmd, "of=/dev/sda") && !strings.Contains(cmd, "of='/dev/sda'") {
				t.Fatalf("cmd %q does not target the disk", cmd)
			}
		})
	}
}

func TestWritePayloadToDisk_ShortWriteDetected(t *testing.T) {
	resetDeploySeams(t)

	execStreamFn = func(cmd string, sudo bool, chrootPath string, env []string) (string, error) {
		return "512 bytes (512 B) copied, 0.01 s, 50 kB/s", nil
	}
	execCmdFn = func(cmd string, sudo bool, chrootPath string, env []string) (string, error) {
		return "", nil
	}

	m := isopayload.Manifest{Compression: config.PayloadCompressionNone, RawBytes: 1024}
	err := writePayloadToDisk("/payload/payload.raw", "/dev/sda", m)
	if err == nil {
		t.Fatal("expected error on short write")
	}
	if !strings.Contains(err.Error(), "short") {
		t.Fatalf("expected a short-write error, got: %v", err)
	}
}

func TestWritePayloadToDisk_StreamFailurePropagates(t *testing.T) {
	resetDeploySeams(t)

	execStreamFn = func(cmd string, sudo bool, chrootPath string, env []string) (string, error) {
		return "", fmt.Errorf("dd failed")
	}
	execCmdFn = func(cmd string, sudo bool, chrootPath string, env []string) (string, error) {
		t.Fatal("sync should not be reached after a streaming failure")
		return "", nil
	}

	m := isopayload.Manifest{Compression: config.PayloadCompressionNone, RawBytes: 1024}
	err := writePayloadToDisk("/payload/payload.raw", "/dev/sda", m)
	if err == nil {
		t.Fatal("expected error to propagate from a failed streamed write")
	}
}

func TestWipeDiskOnFailure(t *testing.T) {
	resetDeploySeams(t)

	var got []string
	execCmdFn = func(cmd string, sudo bool, chrootPath string, env []string) (string, error) {
		got = append(got, cmd)
		return "", nil
	}

	wipeDiskOnFailure("/dev/sda")

	if len(got) != 1 || !strings.Contains(got[0], "wipefs -a -f '/dev/sda'") {
		t.Fatalf("expected a wipefs command targeting /dev/sda, got: %v", got)
	}
}

func TestGrowInstalledDisk_SkipsNonGPT(t *testing.T) {
	resetDeploySeams(t)

	execCmdFn = func(cmd string, sudo bool, chrootPath string, env []string) (string, error) {
		t.Fatalf("no commands expected for a non-gpt disk, got: %q", cmd)
		return "", nil
	}
	execCmdWithInputFn = func(input, cmd string, sudo bool, chrootPath string, env []string) (string, error) {
		t.Fatalf("no commands expected for a non-gpt disk, got: %q", cmd)
		return "", nil
	}

	diskInfo := config.DiskConfig{
		PartitionTableType: "mbr",
		Partitions:         []config.PartitionInfo{{ID: "root", End: "0", FsType: "ext4"}},
	}
	if err := growInstalledDisk("/dev/sda", diskInfo, map[string]string{"root": "/dev/sda2"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGrowInstalledDisk_GPT_Ext4(t *testing.T) {
	resetDeploySeams(t)

	var growInput, growCmd string
	var streamedCmds []string
	execCmdWithInputFn = func(input, cmd string, sudo bool, chrootPath string, env []string) (string, error) {
		growInput, growCmd = input, cmd
		return "", nil
	}
	execCmdFn = func(cmd string, sudo bool, chrootPath string, env []string) (string, error) {
		if strings.HasPrefix(cmd, "lsblk -no PARTN") {
			return "2\n", nil
		}
		return "", nil
	}
	execStreamFn = func(cmd string, sudo bool, chrootPath string, env []string) (string, error) {
		streamedCmds = append(streamedCmds, cmd)
		return "", nil
	}

	diskInfo := config.DiskConfig{
		PartitionTableType: "gpt",
		Partitions:         []config.PartitionInfo{{ID: "root", End: "0", FsType: "ext4"}},
	}
	diskPathIdMap := map[string]string{"root": "/dev/sda2"}

	if err := growInstalledDisk("/dev/sda", diskInfo, diskPathIdMap); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if growInput != ", +\n" {
		t.Fatalf("unexpected sfdisk stdin: %q", growInput)
	}
	if !strings.Contains(growCmd, "sfdisk --no-reread --force -N 2 '/dev/sda'") {
		t.Fatalf("unexpected sfdisk command: %q", growCmd)
	}
	if len(streamedCmds) != 1 || !strings.Contains(streamedCmds[0], "resize2fs /dev/sda2") {
		t.Fatalf("expected a resize2fs streamed command, got: %v", streamedCmds)
	}
}

func TestGrowInstalledDisk_MissingGrowablePartitionErrors(t *testing.T) {
	resetDeploySeams(t)

	diskInfo := config.DiskConfig{
		PartitionTableType: "gpt",
		Partitions:         []config.PartitionInfo{{ID: "esp", End: "512MiB"}},
	}
	if err := growInstalledDisk("/dev/sda", diskInfo, map[string]string{"esp": "/dev/sda1"}); err == nil {
		t.Fatal("expected error when no partition is growable")
	}
}

func TestGrowInstalledDisk_BtrfsUnsupported(t *testing.T) {
	resetDeploySeams(t)

	execCmdWithInputFn = func(input, cmd string, sudo bool, chrootPath string, env []string) (string, error) {
		return "", nil
	}
	execCmdFn = func(cmd string, sudo bool, chrootPath string, env []string) (string, error) {
		if strings.HasPrefix(cmd, "lsblk -no PARTN") {
			return "2\n", nil
		}
		return "", nil
	}

	diskInfo := config.DiskConfig{
		PartitionTableType: "gpt",
		Partitions:         []config.PartitionInfo{{ID: "root", End: "0", FsType: "btrfs"}},
	}
	err := growInstalledDisk("/dev/sda", diskInfo, map[string]string{"root": "/dev/sda2"})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected an unsupported-btrfs error, got: %v", err)
	}
}

func exitErrorWithCode(t *testing.T, code int) error {
	t.Helper()
	err := exec.Command("sh", "-c", fmt.Sprintf("exit %d", code)).Run()
	if err == nil {
		t.Fatalf("exec.Command exited 0, expected a nonzero exit to build an *exec.ExitError")
	}
	return err
}

func TestResizeFilesystem_Ext4_E2fsckCorrectedIssuesContinues(t *testing.T) {
	resetDeploySeams(t)

	var streamed []string
	execCmdFn = func(cmd string, sudo bool, chrootPath string, env []string) (string, error) {
		if strings.HasPrefix(cmd, "e2fsck") {
			return "", exitErrorWithCode(t, 1) // 1 = errors found and corrected
		}
		return "", nil
	}
	execStreamFn = func(cmd string, sudo bool, chrootPath string, env []string) (string, error) {
		streamed = append(streamed, cmd)
		return "", nil
	}

	if err := resizeFilesystem("/dev/sda2", "ext4"); err != nil {
		t.Fatalf("expected a corrected e2fsck exit code to continue to resize2fs, got: %v", err)
	}
	if len(streamed) != 1 || !strings.Contains(streamed[0], "resize2fs /dev/sda2") {
		t.Fatalf("expected resize2fs to run after a corrected e2fsck, got: %v", streamed)
	}
}

func TestResizeFilesystem_Ext4_E2fsckUncorrectedAborts(t *testing.T) {
	resetDeploySeams(t)

	execCmdFn = func(cmd string, sudo bool, chrootPath string, env []string) (string, error) {
		if strings.HasPrefix(cmd, "e2fsck") {
			return "", exitErrorWithCode(t, 4) // >2 = uncorrected errors / operational failure
		}
		return "", nil
	}
	execStreamFn = func(cmd string, sudo bool, chrootPath string, env []string) (string, error) {
		t.Fatalf("resize2fs must not run against an uncorrected filesystem, got: %q", cmd)
		return "", nil
	}

	err := resizeFilesystem("/dev/sda2", "ext4")
	if err == nil || !strings.Contains(err.Error(), "uncorrected") {
		t.Fatalf("expected an uncorrected e2fsck error, got: %v", err)
	}
}

func TestResizeFilesystem_Swap(t *testing.T) {
	resetDeploySeams(t)

	var got []string
	execCmdFn = func(cmd string, sudo bool, chrootPath string, env []string) (string, error) {
		got = append(got, cmd)
		return "", nil
	}

	if err := resizeFilesystem("/dev/sda3", "swap"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || !strings.Contains(got[0], "mkswap -f /dev/sda3") {
		t.Fatalf("expected an mkswap command, got: %v", got)
	}
}

func TestResizeFilesystem_UnknownFsTypeSkipsWithoutError(t *testing.T) {
	resetDeploySeams(t)

	execCmdFn = func(cmd string, sudo bool, chrootPath string, env []string) (string, error) {
		t.Fatalf("no commands expected for an unknown fstype, got: %q", cmd)
		return "", nil
	}

	if err := resizeFilesystem("/dev/sda4", "reiserfs"); err != nil {
		t.Fatalf("expected unknown fstype to be skipped without error, got: %v", err)
	}
}

func TestResizeFilesystem_XFS_MountsAndGrows(t *testing.T) {
	resetDeploySeams(t)

	var mountedDev, mountedPoint string
	var umounted string
	var streamed []string

	mountFn = func(targetPath, mountPoint, flags string) error {
		mountedDev, mountedPoint = targetPath, mountPoint
		return nil
	}
	umountAndDeleteFn = func(mountPoint string) error {
		umounted = mountPoint
		return nil
	}
	execStreamFn = func(cmd string, sudo bool, chrootPath string, env []string) (string, error) {
		streamed = append(streamed, cmd)
		return "", nil
	}

	if err := resizeFilesystem("/dev/sda2", "xfs"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mountedDev != "/dev/sda2" {
		t.Fatalf("expected /dev/sda2 to be mounted, got %q", mountedDev)
	}
	if mountedPoint == "" || mountedPoint != umounted {
		t.Fatalf("expected the mounted point to be unmounted: mounted=%q umounted=%q", mountedPoint, umounted)
	}
	if len(streamed) != 1 || !strings.Contains(streamed[0], "xfs_growfs") {
		t.Fatalf("expected an xfs_growfs command, got: %v", streamed)
	}
}

func TestResetInstanceIdentity(t *testing.T) {
	resetDeploySeams(t)

	var mountedPoint string
	mountFn = func(targetPath, mountPoint, flags string) error {
		mountedPoint = mountPoint
		mustMkdirAll(t, filepath.Join(mountPoint, "etc", "ssh"))
		mustWriteFile(t, filepath.Join(mountPoint, "etc", "machine-id"), "abc123\n")
		return nil
	}
	var umounted string
	umountAndDeleteFn = func(mountPoint string) error {
		umounted = mountPoint
		return nil
	}

	diskInfo := config.DiskConfig{
		Partitions: []config.PartitionInfo{{ID: "root", MountPoint: "/"}},
	}
	if err := resetInstanceIdentity(diskInfo, map[string]string{"root": "/dev/sda2"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mountedPoint == "" || mountedPoint != umounted {
		t.Fatalf("expected the mount point to be unmounted: mounted=%q umounted=%q", mountedPoint, umounted)
	}

	machineID, err := os.ReadFile(filepath.Join(mountedPoint, "etc", "machine-id"))
	if err != nil {
		t.Fatalf("expected machine-id to still exist: %v", err)
	}
	if len(machineID) != 0 {
		t.Fatalf("expected machine-id to be truncated, got %q", machineID)
	}
}

func TestResetInstanceIdentity_NoRootPartitionErrors(t *testing.T) {
	resetDeploySeams(t)

	diskInfo := config.DiskConfig{
		Partitions: []config.PartitionInfo{{ID: "esp", MountPoint: "/boot/efi"}},
	}
	if err := resetInstanceIdentity(diskInfo, map[string]string{"esp": "/dev/sda1"}); err == nil {
		t.Fatal("expected error when no root partition is declared")
	}
}

// TestDeployInstallerPayload_EndToEnd exercises the full orchestrator with every
// external dependency faked, proving the ordering contract: the payload's
// checksum is verified, the disk is checked for capacity, and only then is a
// sequence of destructive commands issued - all before any partition growth
// or identity reset.
func TestDeployInstallerPayload_EndToEnd(t *testing.T) {
	resetDeploySeams(t)
	origShell := shell.Default
	t.Cleanup(func() { shell.Default = origShell })
	// waitForDiskQuiescence goes through imagedisc.CheckDiskIOStats, which is
	// not behind a deploy.go seam; stub the underlying shell call instead so
	// the test does not depend on /proc/diskstats contents on the test host.
	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: "diskstats", Output: "", Error: nil},
	})

	isoRoot := t.TempDir()
	rawContent := []byte("fake raw disk image contents")
	writeFixturePayload(t, isoRoot, rawContent, nil)

	resolveInstallDiskFn = func(config.DiskConfig) (string, error) { return "/dev/sda", nil }
	systemBlockDevicesFn = func() ([]imagedisc.SystemBlockDevice, error) {
		return []imagedisc.SystemBlockDevice{{DevicePath: "/dev/sda", RawDiskSize: uint64(len(rawContent)) * 10}}, nil
	}
	resolveExistingPartitionsFn = func(diskPath string, partitions []config.PartitionInfo) (map[string]string, error) {
		return map[string]string{"root": "/dev/sda1"}, nil
	}

	var commands []recordedCmd
	execCmdFn = func(cmd string, sudo bool, chrootPath string, env []string) (string, error) {
		commands = append(commands, recordedCmd{cmd: cmd})
		if strings.HasPrefix(cmd, "lsblk -no PARTN") {
			return "1\n", nil
		}
		return "", nil
	}
	execStreamFn = func(cmd string, sudo bool, chrootPath string, env []string) (string, error) {
		commands = append(commands, recordedCmd{cmd: cmd})
		return fmt.Sprintf("%d bytes copied", len(rawContent)), nil
	}
	execCmdWithInputFn = func(input, cmd string, sudo bool, chrootPath string, env []string) (string, error) {
		commands = append(commands, recordedCmd{input: input, cmd: cmd})
		return "", nil
	}
	mountFn = func(targetPath, mountPoint, flags string) error {
		mustMkdirAll(t, filepath.Join(mountPoint, "etc"))
		mustWriteFile(t, filepath.Join(mountPoint, "etc", "machine-id"), "abc\n")
		return nil
	}
	umountAndDeleteFn = func(mountPoint string) error { return os.RemoveAll(mountPoint) }

	template := &config.ImageTemplate{
		SystemConfig: config.SystemConfig{
			InstallerPayload: &config.InstallerPayload{Enabled: true, Compression: config.PayloadCompressionNone},
			Bootloader:       config.Bootloader{BootType: "legacy"},
		},
		Disk: config.DiskConfig{
			PartitionTableType: "gpt",
			Partitions:         []config.PartitionInfo{{ID: "root", End: "0", FsType: "ext4", MountPoint: "/"}},
		},
	}

	if err := deployInstallerPayload(template, isoRoot); err != nil {
		t.Fatalf("deployInstallerPayload: %v", err)
	}

	for _, rc := range commands {
		if strings.Contains(rc.cmd, "||") {
			t.Fatalf("no emitted command may contain '||': %q", rc.cmd)
		}
	}

	sawWipefsBeforeWrite := false
	sawWrite := false
	for _, rc := range commands {
		if strings.Contains(rc.cmd, "wipefs -a -f '/dev/sda'") && !sawWrite {
			sawWipefsBeforeWrite = true
		}
		if strings.Contains(rc.cmd, "of=/dev/sda") || strings.Contains(rc.cmd, "of='/dev/sda'") {
			sawWrite = true
		}
	}
	if !sawWipefsBeforeWrite {
		t.Fatal("expected wipefs to run before the payload write")
	}
	if !sawWrite {
		t.Fatal("expected a dd write command targeting the disk")
	}
}

// TestDeployInstallerPayload_PolicySelectedDiskUpdatesBootOrder is a regression
// test for a bug where deployInstallerPayload never wrote the disk path
// resolveInstallDiskFn resolved back onto template.Disk.Path (install.go's
// unattendedInstall does this before calling the same updateBootOrder). With
// disk.path left empty for policy-based selection, updateBootOrder's
// createNewBootEntry read the template's still-empty Disk.Path and failed
// with "no target disk path specified in the template" even though the disk
// write itself had already succeeded.
func TestDeployInstallerPayload_PolicySelectedDiskUpdatesBootOrder(t *testing.T) {
	resetDeploySeams(t)
	origShell := shell.Default
	t.Cleanup(func() { shell.Default = origShell })
	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: "diskstats", Output: "", Error: nil},
		{Pattern: "efibootmgr", Output: "", Error: nil},
	})

	isoRoot := t.TempDir()
	rawContent := []byte("fake raw disk image contents")
	writeFixturePayload(t, isoRoot, rawContent, nil)

	resolveInstallDiskFn = func(config.DiskConfig) (string, error) { return "/dev/sda", nil }
	systemBlockDevicesFn = func() ([]imagedisc.SystemBlockDevice, error) {
		return []imagedisc.SystemBlockDevice{{DevicePath: "/dev/sda", RawDiskSize: uint64(len(rawContent)) * 10}}, nil
	}
	resolveExistingPartitionsFn = func(diskPath string, partitions []config.PartitionInfo) (map[string]string, error) {
		return map[string]string{"boot": "/dev/sda1", "root": "/dev/sda2"}, nil
	}
	execCmdFn = func(cmd string, sudo bool, chrootPath string, env []string) (string, error) {
		if strings.HasPrefix(cmd, "lsblk -no PARTN") {
			return "2\n", nil
		}
		return "", nil
	}
	execStreamFn = func(cmd string, sudo bool, chrootPath string, env []string) (string, error) {
		return fmt.Sprintf("%d bytes copied", len(rawContent)), nil
	}
	execCmdWithInputFn = func(input, cmd string, sudo bool, chrootPath string, env []string) (string, error) {
		return "", nil
	}
	mountFn = func(targetPath, mountPoint, flags string) error {
		mustMkdirAll(t, filepath.Join(mountPoint, "etc"))
		mustWriteFile(t, filepath.Join(mountPoint, "etc", "machine-id"), "abc\n")
		return nil
	}
	umountAndDeleteFn = func(mountPoint string) error { return os.RemoveAll(mountPoint) }

	// disk.path left empty, exactly as a policy-based selectionPolicy template does.
	template := &config.ImageTemplate{
		SystemConfig: config.SystemConfig{
			InstallerPayload: &config.InstallerPayload{Enabled: true, Compression: config.PayloadCompressionNone},
			Bootloader:       config.Bootloader{BootType: "efi"},
		},
		Disk: config.DiskConfig{
			PartitionTableType: "gpt",
			Partitions: []config.PartitionInfo{
				{ID: "boot", End: "513MiB", FsType: "fat32", MountPoint: "/boot/efi"},
				{ID: "root", End: "0", FsType: "ext4", MountPoint: "/"},
			},
		},
	}

	if err := deployInstallerPayload(template, isoRoot); err != nil {
		t.Fatalf("deployInstallerPayload: %v", err)
	}
	if template.Disk.Path != "/dev/sda" {
		t.Fatalf("expected template.Disk.Path to be updated to the resolved disk, got %q", template.Disk.Path)
	}
}

func TestDeployInstallerPayload_ChecksumMismatchAbortsBeforeAnyDiskCommand(t *testing.T) {
	resetDeploySeams(t)

	isoRoot := t.TempDir()
	writeFixturePayload(t, isoRoot, []byte("actual content"), func(m *isopayload.Manifest) {
		// deliberately wrong, to abort before any disk command runs
		m.CompressedSha256 = strings.Repeat("0", 64)
		m.RawSha256 = strings.Repeat("0", 64)
	})

	resolveInstallDiskFn = func(config.DiskConfig) (string, error) {
		t.Fatal("disk resolution must not be reached after a checksum mismatch")
		return "", nil
	}
	execCmdFn = func(cmd string, sudo bool, chrootPath string, env []string) (string, error) {
		t.Fatalf("no shell command may run after a checksum mismatch, got: %q", cmd)
		return "", nil
	}
	execStreamFn = func(cmd string, sudo bool, chrootPath string, env []string) (string, error) {
		t.Fatalf("no shell command may run after a checksum mismatch, got: %q", cmd)
		return "", nil
	}

	template := &config.ImageTemplate{
		SystemConfig: config.SystemConfig{
			InstallerPayload: &config.InstallerPayload{Enabled: true, Compression: config.PayloadCompressionNone},
		},
	}

	err := deployInstallerPayload(template, isoRoot)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected a checksum mismatch error, got: %v", err)
	}
}

func TestDeployInstallerPayload_DiskTooSmallAbortsBeforeAnyWrite(t *testing.T) {
	resetDeploySeams(t)

	isoRoot := t.TempDir()
	rawContent := []byte("fake raw disk image contents")
	// manifest claims a much larger raw image than the disk actually reports
	writeFixturePayload(t, isoRoot, rawContent, func(m *isopayload.Manifest) {
		m.RawBytes *= 100
	})

	resolveInstallDiskFn = func(config.DiskConfig) (string, error) { return "/dev/sda", nil }
	systemBlockDevicesFn = func() ([]imagedisc.SystemBlockDevice, error) {
		return []imagedisc.SystemBlockDevice{{DevicePath: "/dev/sda", RawDiskSize: uint64(len(rawContent))}}, nil
	}
	execCmdFn = func(cmd string, sudo bool, chrootPath string, env []string) (string, error) {
		t.Fatalf("no shell command may run once the disk is known to be too small, got: %q", cmd)
		return "", nil
	}
	execStreamFn = func(cmd string, sudo bool, chrootPath string, env []string) (string, error) {
		t.Fatalf("no shell command may run once the disk is known to be too small, got: %q", cmd)
		return "", nil
	}

	template := &config.ImageTemplate{
		SystemConfig: config.SystemConfig{
			InstallerPayload: &config.InstallerPayload{Enabled: true, Compression: config.PayloadCompressionNone},
		},
	}

	err := deployInstallerPayload(template, isoRoot)
	if err == nil || !strings.Contains(err.Error(), "too small") {
		t.Fatalf("expected a disk-too-small error, got: %v", err)
	}
}

func TestDeployInstallerPayload_WriteFailureWipesDisk(t *testing.T) {
	resetDeploySeams(t)
	origShell := shell.Default
	t.Cleanup(func() { shell.Default = origShell })
	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: "diskstats", Output: "", Error: nil},
	})

	isoRoot := t.TempDir()
	rawContent := []byte("fake raw disk image contents")
	writeFixturePayload(t, isoRoot, rawContent, nil)

	resolveInstallDiskFn = func(config.DiskConfig) (string, error) { return "/dev/sda", nil }
	systemBlockDevicesFn = func() ([]imagedisc.SystemBlockDevice, error) {
		return []imagedisc.SystemBlockDevice{{DevicePath: "/dev/sda", RawDiskSize: uint64(len(rawContent)) * 10}}, nil
	}

	var wipefsCount int
	execCmdFn = func(cmd string, sudo bool, chrootPath string, env []string) (string, error) {
		if strings.Contains(cmd, "wipefs") {
			wipefsCount++
		}
		return "", nil
	}
	execStreamFn = func(cmd string, sudo bool, chrootPath string, env []string) (string, error) {
		return "", fmt.Errorf("simulated dd failure")
	}

	template := &config.ImageTemplate{
		SystemConfig: config.SystemConfig{
			InstallerPayload: &config.InstallerPayload{Enabled: true, Compression: config.PayloadCompressionNone},
		},
	}

	err := deployInstallerPayload(template, isoRoot)
	if err == nil {
		t.Fatal("expected the simulated write failure to propagate")
	}
	// Once before the write (clearing stale signatures) and once again after
	// the write fails (wipeDiskOnFailure).
	if wipefsCount != 2 {
		t.Fatalf("expected wipefs to run twice (pre-write + on-failure), got %d", wipefsCount)
	}
}
