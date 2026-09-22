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
		// Substring-containing but non-canonical output must be rejected: the
		// value is later interpolated into privileged shell commands, so only a
		// bare "/dev/loopN" path is accepted.
		{name: "loop path with trailing garbage", output: "/dev/loop7; rm -rf /", expectError: true, errorContains: "failed to create loopback device"},
		{name: "loop path with partition suffix", output: "/dev/loop7p1", expectError: true, errorContains: "failed to create loopback device"},
		{name: "loop substring inside other text", output: "prefix /dev/loop7", expectError: true, errorContains: "failed to create loopback device"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor([]shell.MockCommand{
				{Pattern: "losetup -l --json", Output: `{"loopdevices":[]}`, Error: nil},
				{Pattern: "losetup --direct-io=on --show -f -P", Output: tt.output, Error: tt.cmdErr},
			})

			got, _, err := loopSetupCreate("/tmp/test.raw")
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

func TestDetachStaleLoopDevices(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	t.Run("no devices attached", func(t *testing.T) {
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{
			{Pattern: "losetup -l --json", Output: `{"loopdevices":[]}`},
		})
		got := detachStaleLoopDevices(&LoopDev{}, "/tmp/test.raw")
		if len(got) != 0 {
			t.Fatalf("expected no detached devices, got %v", got)
		}
	})

	t.Run("non-matching back-file is left alone", func(t *testing.T) {
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{
			{Pattern: "losetup -l --json", Output: `{"loopdevices":[{"name":"/dev/loop98","back-file":"/tmp/other.raw"}]}`},
		})
		got := detachStaleLoopDevices(&LoopDev{}, "/tmp/test.raw")
		if len(got) != 0 {
			t.Fatalf("expected no detached devices, got %v", got)
		}
	})

	t.Run("back-file whitespace is preserved, not trimmed", func(t *testing.T) {
		// A blanket TrimSpace would incorrectly normalize this distinct,
		// whitespace-suffixed backing file into a false match against imagePath.
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{
			{Pattern: "losetup -l --json", Output: `{"loopdevices":[{"name":"/dev/loop95","back-file":"/tmp/test.raw "}]}`},
		})
		got := detachStaleLoopDevices(&LoopDev{}, "/tmp/test.raw")
		if len(got) != 0 {
			t.Fatalf("expected back-file with trailing whitespace not to match a distinct path, got %v", got)
		}
	})

	t.Run("stale device with deleted back-file is detached", func(t *testing.T) {
		// Both paths live under t.TempDir() rather than a hardcoded /tmp path
		// so the test doesn't depend on nothing at that literal host path
		// existing: resolveBackFile explicitly stats the suffixed back-file.
		dir := t.TempDir()
		imgPath := filepath.Join(dir, "test.raw")
		deletedBackFile := imgPath + deletedBackFileSuffix

		shell.Default = shell.NewMockExecutor([]shell.MockCommand{
			{Pattern: "losetup -l --json", Output: fmt.Sprintf(`{"loopdevices":[{"name":"/dev/loop97","back-file":%q}]}`, deletedBackFile)},
			{Pattern: `lsblk -o NAME,MOUNTPOINT /dev/loop97 -J`, Output: `{"blockdevices":[{"name":"loop97","mountpoint":null}]}`},
			// LoopSetupDelete's disableSwapPartitions scan; mock it (no swap
			// partitions) so the test stays hermetic.
			{Pattern: `lsblk -o NAME,FSTYPE /dev/loop97 -J`, Output: `{"blockdevices":[]}`},
			{Pattern: "losetup -d /dev/loop97", Output: ""},
		})
		got := detachStaleLoopDevices(&LoopDev{}, imgPath)
		if len(got) != 1 || got[0] != "/dev/loop97" {
			t.Fatalf("expected [/dev/loop97] detached, got %v", got)
		}
	})

	t.Run("actively mounted device is left attached", func(t *testing.T) {
		// A matching back-file alone doesn't prove the device is abandoned:
		// a concurrent process could be actively using it. An active
		// mountpoint on any partition must block the detach.
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{
			{Pattern: "losetup -l --json", Output: `{"loopdevices":[{"name":"/dev/loop91","back-file":"/tmp/test.raw"}]}`},
			{Pattern: `lsblk -o NAME,MOUNTPOINT /dev/loop91 -J`, Output: `{"blockdevices":[{"name":"loop91","children":[{"name":"loop91p1","mountpoint":"/mnt/live-build"}]}]}`},
		})
		got := detachStaleLoopDevices(&LoopDev{}, "/tmp/test.raw")
		if len(got) != 0 {
			t.Fatalf("expected an actively mounted device not to be detached, got %v", got)
		}
	})

	t.Run("mount-state lookup failure causes device to be skipped", func(t *testing.T) {
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{
			{Pattern: "losetup -l --json", Output: `{"loopdevices":[{"name":"/dev/loop90","back-file":"/tmp/test.raw"}]}`},
			{Pattern: `lsblk -o NAME,MOUNTPOINT /dev/loop90 -J`, Error: fmt.Errorf("lsblk failed")},
		})
		got := detachStaleLoopDevices(&LoopDev{}, "/tmp/test.raw")
		if len(got) != 0 {
			t.Fatalf("expected device to be skipped when mount state can't be determined, got %v", got)
		}
	})

	t.Run("relative image path matches an absolute back-file", func(t *testing.T) {
		dir := t.TempDir()
		imgPath := filepath.Join(dir, "test.raw")
		if err := os.WriteFile(imgPath, []byte("x"), 0600); err != nil {
			t.Fatalf("failed to create test image: %v", err)
		}
		resolvedImgPath, err := filepath.EvalSymlinks(imgPath)
		if err != nil {
			t.Fatalf("failed to resolve test image path: %v", err)
		}

		origWD, err := os.Getwd()
		if err != nil {
			t.Fatalf("failed to get working directory: %v", err)
		}
		if err := os.Chdir(dir); err != nil {
			t.Fatalf("failed to chdir into %s: %v", dir, err)
		}
		defer func() { _ = os.Chdir(origWD) }()

		// losetup reports back-file as the canonical absolute path even
		// though the caller (loopSetupCreate) may pass a relative one.
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{
			{Pattern: "losetup -l --json", Output: fmt.Sprintf(`{"loopdevices":[{"name":"/dev/loop94","back-file":%q}]}`, resolvedImgPath)},
			{Pattern: `lsblk -o NAME,MOUNTPOINT /dev/loop94 -J`, Output: `{"blockdevices":[{"name":"loop94","mountpoint":null}]}`},
			{Pattern: `lsblk -o NAME,FSTYPE /dev/loop94 -J`, Output: `{"blockdevices":[]}`},
			{Pattern: "losetup -d /dev/loop94", Output: ""},
		})

		got := detachStaleLoopDevices(&LoopDev{}, "test.raw")
		if len(got) != 1 || got[0] != "/dev/loop94" {
			t.Fatalf("expected [/dev/loop94] detached via relative-path match, got %v", got)
		}
	})

	t.Run("literal filename ending in the deleted suffix is preserved when it exists", func(t *testing.T) {
		dir := t.TempDir()
		weirdPath := filepath.Join(dir, "test.raw (deleted)")
		if err := os.WriteFile(weirdPath, []byte("x"), 0600); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		shell.Default = shell.NewMockExecutor([]shell.MockCommand{
			{Pattern: "losetup -l --json", Output: fmt.Sprintf(`{"loopdevices":[{"name":"/dev/loop93","back-file":%q}]}`, weirdPath)},
		})

		// imagePath is the file without the suffix: a distinct, real file
		// from weirdPath. The literal " (deleted)"-suffixed back-file exists
		// on disk, so it must not be treated as losetup's annotation and
		// must not match imagePath.
		got := detachStaleLoopDevices(&LoopDev{}, filepath.Join(dir, "test.raw"))
		if len(got) != 0 {
			t.Fatalf("expected literal back-file not to match a distinct path, got %v", got)
		}
	})

	t.Run("non-not-exist stat error causes device to be skipped", func(t *testing.T) {
		dir := t.TempDir()
		regularFile := filepath.Join(dir, "notadir")
		if err := os.WriteFile(regularFile, []byte("x"), 0600); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}
		// A parent path component is a regular file, not a directory, so
		// stat fails with ENOTDIR rather than "not exist".
		badPath := filepath.Join(regularFile, "test.raw (deleted)")

		shell.Default = shell.NewMockExecutor([]shell.MockCommand{
			{Pattern: "losetup -l --json", Output: fmt.Sprintf(`{"loopdevices":[{"name":"/dev/loop92","back-file":%q}]}`, badPath)},
		})

		got := detachStaleLoopDevices(&LoopDev{}, "/tmp/test.raw")
		if len(got) != 0 {
			t.Fatalf("expected device with an unresolvable back-file to be skipped, got %v", got)
		}
	})

	t.Run("detach failure is logged and not reported as detached", func(t *testing.T) {
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{
			{Pattern: "losetup -l --json", Output: `{"loopdevices":[{"name":"/dev/loop96","back-file":"/tmp/test.raw"}]}`},
			{Pattern: `lsblk -o NAME,MOUNTPOINT /dev/loop96 -J`, Output: `{"blockdevices":[{"name":"loop96","mountpoint":null}]}`},
			{Pattern: `lsblk -o NAME,FSTYPE /dev/loop96 -J`, Output: `{"blockdevices":[]}`},
			{Pattern: "losetup -d /dev/loop96", Error: fmt.Errorf("detach failed")},
		})
		got := detachStaleLoopDevices(&LoopDev{}, "/tmp/test.raw")
		if len(got) != 0 {
			t.Fatalf("expected no successfully detached devices, got %v", got)
		}
	})

	t.Run("enumeration failure is non-fatal", func(t *testing.T) {
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{
			{Pattern: "losetup -l --json", Error: fmt.Errorf("losetup failed")},
		})
		got := detachStaleLoopDevices(&LoopDev{}, "/tmp/test.raw")
		if len(got) != 0 {
			t.Fatalf("expected no detached devices, got %v", got)
		}
	})
}

func TestLoopSetupCreateEmptyRawDisk(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	t.Run("invalid size", func(t *testing.T) {
		_, _, err := loopSetupCreateEmptyRawDisk(filepath.Join(t.TempDir(), "disk.raw"), "bad-size")
		if err == nil {
			t.Fatal("expected error for invalid size")
		}
	})

	t.Run("missing file after create", func(t *testing.T) {
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{{Pattern: "fallocate", Output: "", Error: nil}})
		_, _, err := loopSetupCreateEmptyRawDisk(filepath.Join(t.TempDir(), "disk.raw"), "16MiB")
		if err == nil || !strings.Contains(err.Error(), "can't find") {
			t.Fatalf("expected can't find error, got %v", err)
		}
	})
}

func TestLoopSetupDelete(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	t.Run("success", func(t *testing.T) {
		// LoopSetupDelete first runs disableSwapPartitions, which shells out to
		// lsblk. Mock it (no swap partitions) so the test stays hermetic and cannot
		// fall back to a real sudo lsblk on the host.
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{
			{Pattern: `lsblk -o NAME,FSTYPE /dev/loop7 -J`, Output: `{"blockdevices":[]}`},
			{Pattern: "losetup -d /dev/loop7", Output: "", Error: nil},
		})
		ld := &LoopDev{}
		if err := ld.LoopSetupDelete("/dev/loop7"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("command error", func(t *testing.T) {
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{
			{Pattern: `lsblk -o NAME,FSTYPE /dev/loop8 -J`, Output: `{"blockdevices":[]}`},
			{Pattern: "losetup -d /dev/loop8", Output: "", Error: fmt.Errorf("delete failed")},
		})
		ld := &LoopDev{}
		err := ld.LoopSetupDelete("/dev/loop8")
		if err == nil || !strings.Contains(err.Error(), "failed to delete loop device") {
			t.Fatalf("expected wrapped delete error, got %v", err)
		}
	})
}

func TestLoopDevPartitions(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	t.Run("excludes base device and blanks", func(t *testing.T) {
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{{
			Pattern: `lsblk -prno NAME '/dev/loop0'`,
			Output:  "/dev/loop0\n/dev/loop0p1\n/dev/loop0p2\n\n",
		}})
		got, err := loopDevPartitions("/dev/loop0")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"/dev/loop0p1", "/dev/loop0p2"}
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("partitions = %v, want %v", got, want)
		}
	})

	t.Run("command error", func(t *testing.T) {
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{{
			Pattern: `lsblk -prno NAME '/dev/loop1'`,
			Error:   fmt.Errorf("lsblk failed"),
		}})
		if _, err := loopDevPartitions("/dev/loop1"); err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestAttachImageToLoopDev(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	makeImage := func(t *testing.T) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "baseline.raw")
		if err := os.WriteFile(path, []byte("img"), 0644); err != nil {
			t.Fatalf("write image: %v", err)
		}
		return path
	}

	t.Run("missing image", func(t *testing.T) {
		ld := &LoopDev{}
		if _, _, _, err := ld.AttachImageToLoopDev("/nonexistent/baseline.raw"); err == nil {
			t.Fatal("expected error for missing image")
		}
	})

	t.Run("success returns partitions", func(t *testing.T) {
		img := makeImage(t)
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{
			{Pattern: "losetup -l --json", Output: `{"loopdevices":[]}`},
			{Pattern: "losetup --direct-io=on --show -f -P", Output: "/dev/loop6\n"},
			{Pattern: `lsblk -prno NAME '/dev/loop6'`, Output: "/dev/loop6\n/dev/loop6p1\n"},
		})
		ld := &LoopDev{}
		dev, parts, unregister, err := ld.AttachImageToLoopDev(img)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer unregister()
		if dev != "/dev/loop6" {
			t.Fatalf("dev = %q, want /dev/loop6", dev)
		}
		if len(parts) != 1 || parts[0] != "/dev/loop6p1" {
			t.Fatalf("parts = %v, want [/dev/loop6p1]", parts)
		}
	})

	t.Run("detaches on partition enumeration failure", func(t *testing.T) {
		img := makeImage(t)
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{
			{Pattern: "losetup -l --json", Output: `{"loopdevices":[]}`},
			{Pattern: "losetup --direct-io=on --show -f -P", Output: "/dev/loop5\n"},
			{Pattern: `lsblk -prno NAME '/dev/loop5'`, Error: fmt.Errorf("lsblk failed")},
			// Cleanup (LoopSetupDelete -> disableSwapPartitions) runs this lsblk
			// before the detach. Mock it (no swap partitions) so the test stays
			// hermetic and never falls back to a real sudo lsblk on the host.
			{Pattern: `lsblk -o NAME,FSTYPE /dev/loop5 -J`, Output: `{"blockdevices":[]}`},
			{Pattern: "losetup -d /dev/loop5", Output: ""}, // cleanup detach must run
		})
		ld := &LoopDev{}
		if _, _, _, err := ld.AttachImageToLoopDev(img); err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("surfaces detach failure alongside enumeration failure", func(t *testing.T) {
		img := makeImage(t)
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{
			{Pattern: "losetup -l --json", Output: `{"loopdevices":[]}`},
			{Pattern: "losetup --direct-io=on --show -f -P", Output: "/dev/loop2\n"},
			{Pattern: `lsblk -prno NAME '/dev/loop2'`, Error: fmt.Errorf("lsblk failed")},
			// swapoff scan during detach is best-effort; mock its lsblk (no swap
			// partitions) so cleanup stays hermetic and cannot fall back to a real
			// sudo lsblk on the host. The detach itself then fails as intended.
			{Pattern: `lsblk -o NAME,FSTYPE /dev/loop2 -J`, Output: `{"blockdevices":[]}`},
			{Pattern: "losetup -d /dev/loop2", Error: fmt.Errorf("detach failed")},
		})
		ld := &LoopDev{}
		dev, _, _, err := ld.AttachImageToLoopDev(img)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		// The leaked loop device path must be returned (not "") so the caller can
		// retain the backing file and operators can reclaim the device.
		if dev != "/dev/loop2" {
			t.Errorf("leaked device path = %q, want /dev/loop2", dev)
		}
		// Both the enumeration error and the detach failure (leaked loop device)
		// must reach the caller so the leak is never silently swallowed.
		if !strings.Contains(err.Error(), "lsblk failed") {
			t.Errorf("error must include enumeration failure, got %v", err)
		}
		if !strings.Contains(err.Error(), "detach failed") {
			t.Errorf("error must include detach failure, got %v", err)
		}
		if !strings.Contains(err.Error(), "/dev/loop2") {
			t.Errorf("error must be annotated with leaked device path, got %v", err)
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
		_, _, _, err := ld.CreateRawImageLoopDev(filepath.Join(t.TempDir(), "x.raw"), template)
		if err == nil || !strings.Contains(err.Error(), "failed to create loop device") {
			t.Fatalf("expected wrapped create loop error, got %v", err)
		}
	})

	t.Run("successful create with empty partition list", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "disk.raw")

		gptDiskInfo := `Disk /dev/loop7: 1 MiB, 1048576 bytes, 2048 sectors
Units: sectors of 1 * 512 = 512 bytes
Sector size (logical/physical): 512 bytes / 4096 bytes
Disklabel type: gpt`

		// Use shell mocks for all external commands touched in this path.
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{
			{Pattern: "sudo fallocate -l 1MiB", Output: "", Error: nil},
			{Pattern: "sudo losetup -l --json", Output: `{"loopdevices":[]}`, Error: nil},
			{Pattern: "sudo losetup --direct-io=on --show -f -P", Output: "/dev/loop7\n", Error: nil},
			{Pattern: "sudo fdisk -l /dev/loop7", Output: gptDiskInfo, Error: nil},
			{Pattern: "sudo cat /sys/block/loop7/queue/hw_sector_size", Output: "512", Error: nil},
			{Pattern: "sudo cat /sys/block/loop7/queue/physical_block_size", Output: "4096", Error: nil},
			{Pattern: "echo 'label: gpt'.*sudo sfdisk", Output: "", Error: nil},
			{Pattern: "sudo sync", Output: "", Error: nil},
			{Pattern: "sudo partx -u /dev/loop7", Output: "", Error: nil},
		})

		// Ensure the file exists for loopSetupCreateEmptyRawDisk stat check.
		if err := os.WriteFile(filePath, []byte("raw"), 0600); err != nil {
			t.Fatalf("failed to create placeholder raw file: %v", err)
		}

		ld := &LoopDev{}
		template := &config.ImageTemplate{Disk: config.DiskConfig{Size: "1MiB", PartitionTableType: "gpt", Partitions: []config.PartitionInfo{}}}

		loopPath, partMap, unregister, err := ld.CreateRawImageLoopDev(filePath, template)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer unregister()
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
