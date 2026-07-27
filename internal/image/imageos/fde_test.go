package imageos

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/file"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

// wireFDEBootTestExecutor mocks cryptsetup and performs file operations locally so
// wireFDEBoot tests do not depend on sudo/cat/cp in CI.
type wireFDEBootTestExecutor struct {
	luksUUID string
}

var shellQuotedArgPattern = regexp.MustCompile(`'([^']*)'|"([^"]*)"`)

func (e *wireFDEBootTestExecutor) ExecCmd(cmdStr string, sudo bool, chrootPath string, envVal []string) (string, error) {
	if out, ok, err := e.dispatch(cmdStr); ok {
		return out, err
	}
	return "", fmt.Errorf("unexpected command in wireFDEBoot test: %s", cmdStr)
}

func (e *wireFDEBootTestExecutor) ExecCmdSilent(cmdStr string, sudo bool, chrootPath string, envVal []string) (string, error) {
	return e.ExecCmd(cmdStr, sudo, chrootPath, envVal)
}

func (e *wireFDEBootTestExecutor) ExecCmdWithStream(cmdStr string, sudo bool, chrootPath string, envVal []string) (string, error) {
	return e.ExecCmd(cmdStr, sudo, chrootPath, envVal)
}

func (e *wireFDEBootTestExecutor) ExecCmdWithInput(inputStr string, cmdStr string, sudo bool, chrootPath string, envVal []string) (string, error) {
	return e.ExecCmd(cmdStr, sudo, chrootPath, envVal)
}

func (e *wireFDEBootTestExecutor) dispatch(cmdStr string) (output string, handled bool, err error) {
	switch {
	case strings.Contains(cmdStr, "cryptsetup status"):
		return "  device: /dev/loop0p2\n", true, nil
	case strings.Contains(cmdStr, "cryptsetup luksUUID"):
		return e.luksUUID + "\n", true, nil
	case strings.Contains(cmdStr, "cryptsetup luksAddKey"):
		return "", true, nil
	case strings.HasPrefix(strings.TrimSpace(cmdStr), "cat "):
		path := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(cmdStr), "cat "))
		paths := shellQuotedPaths(cmdStr)
		if len(paths) > 0 {
			path = paths[len(paths)-1]
		}
		if path == "" {
			return "", true, fmt.Errorf("cat missing path in %q", cmdStr)
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return "", true, readErr
		}
		return string(data), true, nil
	case strings.Contains(cmdStr, "mkdir"):
		paths := shellQuotedPaths(cmdStr)
		if len(paths) == 0 {
			return "", true, fmt.Errorf("mkdir missing path in %q", cmdStr)
		}
		return "", true, os.MkdirAll(paths[len(paths)-1], 0755)
	case strings.Contains(cmdStr, " cp ") || strings.HasPrefix(strings.TrimSpace(cmdStr), "cp "):
		paths := shellQuotedPaths(cmdStr)
		if len(paths) < 2 {
			return "", true, fmt.Errorf("cp expected two paths in %q", cmdStr)
		}
		src, dst := paths[len(paths)-2], paths[len(paths)-1]
		data, readErr := os.ReadFile(src)
		if readErr != nil {
			return "", true, readErr
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return "", true, err
		}
		return "", true, os.WriteFile(dst, data, 0644)
	default:
		return "", false, nil
	}
}

func shellQuotedPaths(cmdStr string) []string {
	var paths []string
	for _, m := range shellQuotedArgPattern.FindAllStringSubmatch(cmdStr, -1) {
		if m[1] != "" {
			paths = append(paths, m[1])
		} else if m[2] != "" {
			paths = append(paths, m[2])
		}
	}
	return paths
}

func setWireFDEBootTestShell(t *testing.T, luksUUID string) {
	t.Helper()
	originalExecutor := shell.Default
	shell.Default = &wireFDEBootTestExecutor{luksUUID: luksUUID}
	t.Cleanup(func() { shell.Default = originalExecutor })
}

func TestParseCryptsetupStatusDevice(t *testing.T) {
	t.Parallel()

	status := `/dev/mapper/rootfs is active.
  type:    LUKS2
  device:  /dev/loop0p2
  keysize: 512 bits
`
	got := parseCryptsetupStatusDevice(status)
	if got != "/dev/loop0p2" {
		t.Errorf("parseCryptsetupStatusDevice() = %q, want /dev/loop0p2", got)
	}
	if parseCryptsetupStatusDevice("") != "" {
		t.Error("expected empty for empty status output")
	}
}

func TestFdeCrypttabEntry(t *testing.T) {
	t.Parallel()

	got := fdeCrypttabEntry("rootfs", "uuid-1", "/etc/cryptsetup-keys.d/fde.key")
	want := "rootfs UUID=uuid-1 /etc/cryptsetup-keys.d/fde.key luks,discard"
	if got != want {
		t.Errorf("fdeCrypttabEntry() = %q, want %q", got, want)
	}
}

func TestFdeLuksCmdlineParams(t *testing.T) {
	t.Parallel()

	manual := fdeLuksCmdlineParams("uuid-1", "rootfs", "/etc/cryptsetup-keys.d/fde.key", false)
	if len(manual) != 2 {
		t.Fatalf("manual params len = %d, want 2", len(manual))
	}
	if strings.Contains(strings.Join(manual, " "), "rd.luks.key=") {
		t.Error("manual mode should not include rd.luks.key")
	}

	auto := fdeLuksCmdlineParams("uuid-1", "rootfs", "/etc/cryptsetup-keys.d/fde.key", true)
	if len(auto) != 3 {
		t.Fatalf("auto params len = %d, want 3", len(auto))
	}
	if !strings.Contains(auto[2], "rd.luks.key=/etc/cryptsetup-keys.d/fde.key") {
		t.Errorf("auto params missing key path: %v", auto)
	}
}

func TestFdeRewriteCmdlineFields(t *testing.T) {
	t.Parallel()

	fields := []string{
		"root=/dev/mapper/root",
		"console=ttyS0",
	}
	got, err := fdeRewriteCmdlineFields(fields, fdeCmdlineRewriteConfig{
		rootMapper:   "/dev/mapper/rootfs",
		immutability: false,
		luksParams:   []string{"rd.luks.uuid=abc"},
	})
	if err != nil {
		t.Fatalf("fdeRewriteCmdlineFields: %v", err)
	}
	if got[0] != "rd.luks.uuid=abc" {
		t.Errorf("first field = %q, want rd.luks prepended", got[0])
	}
	if got[1] != "root=/dev/mapper/rootfs" {
		t.Errorf("root field = %q", got[1])
	}

	verityFields := []string{
		"systemd.verity_root_data=PARTUUID=old",
		"roothash=/dev/loop0p2-/dev/loop0p4",
	}
	got, err = fdeRewriteCmdlineFields(verityFields, fdeCmdlineRewriteConfig{
		rootMapper:   "/dev/mapper/rootfs",
		hashDev:      "/dev/loop0p4",
		immutability: true,
		luksParams:   []string{"rd.luks.uuid=abc"},
	})
	if err != nil {
		t.Fatalf("fdeRewriteCmdlineFields verity: %v", err)
	}
	if !strings.Contains(strings.Join(got, " "), "systemd.verity_root_data=/dev/mapper/rootfs") {
		t.Errorf("verity data not rewritten: %v", got)
	}
	if !strings.Contains(strings.Join(got, " "), "roothash=/dev/mapper/rootfs-/dev/loop0p4") {
		t.Errorf("roothash not rewritten: %v", got)
	}

	_, err = fdeRewriteCmdlineFields([]string{"roothash=x-y"}, fdeCmdlineRewriteConfig{
		rootMapper:   "/dev/mapper/rootfs",
		immutability: true,
	})
	if err == nil {
		t.Fatal("expected error when verity enabled without hash device")
	}
}

func TestFdeRenderCrypttab(t *testing.T) {
	t.Parallel()

	got := fdeRenderCrypttab([]string{"line1", "line2"})
	if got != "line1\nline2\n" {
		t.Errorf("fdeRenderCrypttab() = %q", got)
	}
}

func TestFdeTargetPartitionIDs(t *testing.T) {
	t.Parallel()

	template := &config.ImageTemplate{
		Disk: config.DiskConfig{
			Partitions: []config.PartitionInfo{
				{ID: "boot", MountPoint: "/boot/efi"},
				{ID: "rootfs", MountPoint: "/"},
			},
		},
		SystemConfig: config.SystemConfig{
			FDE: config.FDEConfig{Enabled: true, Partitions: []string{"userdata"}},
		},
	}
	if ids := fdeTargetPartitionIDs(template); len(ids) != 1 || ids[0] != "userdata" {
		t.Errorf("explicit partitions = %v, want [userdata]", ids)
	}

	template.SystemConfig.FDE.Partitions = nil
	if ids := fdeTargetPartitionIDs(template); len(ids) != 1 || ids[0] != "rootfs" {
		t.Errorf("default root = %v, want [rootfs]", ids)
	}

	template.Disk.Partitions = []config.PartitionInfo{{ID: "boot", MountPoint: "/boot"}}
	template.SystemConfig.FDE.Partitions = nil
	if ids := fdeTargetPartitionIDs(template); ids != nil {
		t.Errorf("no root partition = %v, want nil", ids)
	}
}

func TestFdeRootPartitionID(t *testing.T) {
	t.Parallel()

	template := &config.ImageTemplate{
		Disk: config.DiskConfig{
			Partitions: []config.PartitionInfo{
				{ID: "esp", MountPoint: "/boot/efi"},
				{ID: "rootfs", MountPoint: "/"},
			},
		},
	}
	if got := fdeRootPartitionID(template); got != "rootfs" {
		t.Errorf("fdeRootPartitionID() = %q, want rootfs", got)
	}
}

func TestFdeHashPartitionDev(t *testing.T) {
	t.Parallel()

	diskMap := map[string]string{
		"roothashmap": "/dev/loop0p4",
		"other":       "/dev/loop0p5",
	}
	template := &config.ImageTemplate{
		Disk: config.DiskConfig{
			Partitions: []config.PartitionInfo{
				{ID: "roothashmap", MountPoint: "none"},
				{ID: "hash", MountPoint: "none"},
			},
		},
	}
	if got := fdeHashPartitionDev(diskMap, template); got != "/dev/loop0p4" {
		t.Errorf("roothashmap dev = %q", got)
	}

	template.Disk.Partitions = []config.PartitionInfo{{ID: "hash", MountPoint: "none"}}
	diskMap = map[string]string{"hash": "/dev/loop0p5"}
	if got := fdeHashPartitionDev(diskMap, template); got != "/dev/loop0p5" {
		t.Errorf("hash id dev = %q", got)
	}

	template.Disk.Partitions = []config.PartitionInfo{
		{ID: "swap", MountPoint: "none"},
	}
	diskMap = map[string]string{"swap": "/dev/loop0p3"}
	if got := fdeHashPartitionDev(diskMap, template); got != "/dev/loop0p3" {
		t.Errorf("none mount dev = %q", got)
	}
	if got := fdeHashPartitionDev(map[string]string{}, template); got != "" {
		t.Errorf("missing dev = %q, want empty", got)
	}
}

func TestFdeWriteCrypttab(t *testing.T) {
	installRoot := t.TempDir()
	if err := fdeWriteCrypttab(installRoot, nil); err != nil {
		t.Fatalf("empty lines: %v", err)
	}
}

func TestGenerateFDEKeyfile(t *testing.T) {
	installRoot := t.TempDir()
	chrootPath, hostPath, err := generateFDEKeyfile(installRoot, "test-passphrase")
	if err != nil {
		t.Fatalf("generateFDEKeyfile: %v", err)
	}
	if chrootPath != fdeKeyfilePath {
		t.Errorf("chroot path = %q", chrootPath)
	}
	info, err := os.Stat(hostPath)
	if err != nil {
		t.Fatalf("stat keyfile: %v", err)
	}
	if info.Mode().Perm() != 0400 {
		t.Errorf("keyfile mode = %o, want 0400", info.Mode().Perm())
	}
}

func TestExitCodeAbove(t *testing.T) {
	t.Parallel()

	if exitCodeAbove(nil, 2) {
		t.Error("nil error should not be above threshold")
	}
	if !exitCodeAbove(errors.New("plain error"), 2) {
		t.Error("non-exit error should be treated as above threshold")
	}

	cmd := exec.Command("sh", "-c", "exit 1")
	if err := cmd.Run(); err != nil {
		if exitCodeAbove(err, 2) {
			t.Error("exit 1 should not be above max 2")
		}
		if !exitCodeAbove(err, 0) {
			t.Error("exit 1 should be above max 0")
		}
	} else {
		t.Fatal("expected sh exit 1")
	}
}

func TestWireFDEBootDisabled(t *testing.T) {
	template := &config.ImageTemplate{}
	if err := wireFDEBoot(t.TempDir(), nil, template); err != nil {
		t.Fatalf("wireFDEBoot disabled: %v", err)
	}
}

func fdeTestTemplateManual() *config.ImageTemplate {
	return &config.ImageTemplate{
		Image: config.ImageInfo{Name: "fde-test"},
		Disk: config.DiskConfig{
			Partitions: []config.PartitionInfo{
				{ID: "rootfs", MountPoint: "/", FsType: "ext4"},
			},
		},
		SystemConfig: config.SystemConfig{
			FDE: config.FDEConfig{
				Enabled:    true,
				Passphrase: "secret",
				Unlock:     "manual",
				Partitions: []string{"rootfs"},
			},
		},
	}
}

func TestWireFDEBootManualUnlock(t *testing.T) {
	setWireFDEBootTestShell(t, "luks-uuid-123")

	installRoot := t.TempDir()
	cmdlinePath := filepath.Join(installRoot, "boot", "cmdline.conf")
	if err := os.MkdirAll(filepath.Dir(cmdlinePath), 0755); err != nil {
		t.Fatalf("mkdir boot: %v", err)
	}
	if err := os.WriteFile(cmdlinePath, []byte("root=/dev/mapper/old console=ttyS0\n"), 0644); err != nil {
		t.Fatalf("write cmdline: %v", err)
	}

	template := fdeTestTemplateManual()
	diskMap := map[string]string{"rootfs": "/dev/mapper/rootfs"}

	if err := wireFDEBoot(installRoot, diskMap, template); err != nil {
		t.Fatalf("wireFDEBoot: %v", err)
	}

	content, err := file.Read(cmdlinePath)
	if err != nil {
		t.Fatalf("read cmdline: %v", err)
	}
	if !strings.Contains(content, "rd.luks.uuid=luks-uuid-123") {
		t.Errorf("cmdline missing LUKS uuid: %q", content)
	}
	if !strings.Contains(content, "root=/dev/mapper/rootfs") {
		t.Errorf("cmdline missing rewritten root: %q", content)
	}
}

func TestWireFDEBootAutoUnlock(t *testing.T) {
	setWireFDEBootTestShell(t, "luks-uuid-auto")

	installRoot := t.TempDir()
	cmdlinePath := filepath.Join(installRoot, "boot", "cmdline.conf")
	if err := os.MkdirAll(filepath.Dir(cmdlinePath), 0755); err != nil {
		t.Fatalf("mkdir boot: %v", err)
	}
	if err := os.WriteFile(cmdlinePath, []byte("root=/dev/mapper/old\n"), 0644); err != nil {
		t.Fatalf("write cmdline: %v", err)
	}

	template := fdeTestTemplateManual()
	template.SystemConfig.FDE.Unlock = "auto"

	if err := wireFDEBoot(installRoot, map[string]string{"rootfs": "/dev/mapper/rootfs"}, template); err != nil {
		t.Fatalf("wireFDEBoot auto: %v", err)
	}

	written, err := file.Read(cmdlinePath)
	if err != nil {
		t.Fatalf("read cmdline: %v", err)
	}
	if !strings.Contains(written, "rd.luks.key=/etc/cryptsetup-keys.d/fde.key") {
		t.Errorf("cmdline missing auto key: %q", written)
	}
}

func TestFdeCollectBootParams(t *testing.T) {
	originalExecutor := shell.Default
	t.Cleanup(func() { shell.Default = originalExecutor })

	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: `cryptsetup status`, Output: "device: /dev/sda2\n", Error: nil},
		{Pattern: `cryptsetup luksUUID`, Output: "uuid-data\n", Error: nil},
	})

	diskMap := map[string]string{
		"rootfs": "/dev/mapper/rootfs",
		"data":   "/dev/mapper/data",
	}
	luks, crypttab, rootMapper, err := fdeCollectBootParams(
		[]string{"rootfs", "data"},
		"rootfs",
		false,
		"",
		"",
		"pass",
		diskMap,
	)
	if err != nil {
		t.Fatalf("fdeCollectBootParams: %v", err)
	}
	if rootMapper != "/dev/mapper/rootfs" {
		t.Errorf("rootMapper = %q", rootMapper)
	}
	if len(luks) != 2 {
		t.Errorf("luks params = %v", luks)
	}
	if len(crypttab) != 1 {
		t.Errorf("crypttab lines = %v, want one non-root entry", crypttab)
	}
}

func TestFdeDiscoverLuksUUID(t *testing.T) {
	originalExecutor := shell.Default
	t.Cleanup(func() { shell.Default = originalExecutor })

	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: `cryptsetup status`, Output: "device: /dev/loop7p1\n", Error: nil},
		{Pattern: `cryptsetup luksUUID`, Output: "  abc-def  \n", Error: nil},
	})

	backing, uuid, err := fdeDiscoverLuksUUID("rootfs")
	if err != nil {
		t.Fatalf("fdeDiscoverLuksUUID: %v", err)
	}
	if backing != "/dev/loop7p1" || uuid != "abc-def" {
		t.Errorf("backing=%q uuid=%q", backing, uuid)
	}
}

func TestCloseLuksMappers(t *testing.T) {
	originalExecutor := shell.Default
	t.Cleanup(func() { shell.Default = originalExecutor })

	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: `cryptsetup close`, Output: "", Error: nil},
	})

	if err := closeLuks("rootfs"); err != nil {
		t.Fatalf("closeLuks: %v", err)
	}
	closeLuksMappers([]string{"a", "b"})
}

func TestReencryptPartitionInPlace(t *testing.T) {
	originalExecutor := shell.Default
	t.Cleanup(func() { shell.Default = originalExecutor })

	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: `e2fsck`, Output: "", Error: nil},
		{Pattern: `resize2fs`, Output: "", Error: nil},
		{Pattern: `cryptsetup reencrypt`, Output: "", Error: nil},
		{Pattern: `cryptsetup open`, Output: "", Error: nil},
	})

	mapperPath, mapperName, err := reencryptPartitionInPlace("rootfs", "/dev/loop0p2", "pass")
	if err != nil {
		t.Fatalf("reencryptPartitionInPlace: %v", err)
	}
	if mapperName != "rootfs" || mapperPath != "/dev/mapper/rootfs" {
		t.Errorf("mapper path=%q name=%q", mapperPath, mapperName)
	}
}

func TestEnablingFDEValidationErrors(t *testing.T) {
	installRoot := t.TempDir()
	baseTemplate := func() *config.ImageTemplate {
		return &config.ImageTemplate{
			Disk: config.DiskConfig{
				Partitions: []config.PartitionInfo{
					{ID: "rootfs", MountPoint: "/"},
				},
			},
			SystemConfig: config.SystemConfig{
				FDE: config.FDEConfig{
					Enabled:    true,
					Passphrase: "secret",
					Partitions: []string{"rootfs"},
				},
			},
		}
	}

	imageOs := &ImageOs{
		installRoot: installRoot,
		chrootEnv:   &MockChrootEnv{},
	}

	t.Run("disabled", func(t *testing.T) {
		template := baseTemplate()
		template.SystemConfig.FDE.Enabled = false
		imageOs.template = template
		list, opened, err := imageOs.enablingFDE(installRoot, nil, nil)
		if err != nil || len(opened) != 0 {
			t.Fatalf("disabled: list=%v opened=%v err=%v", list, opened, err)
		}
	})

	t.Run("missing passphrase from file", func(t *testing.T) {
		template := baseTemplate()
		template.SystemConfig.FDE.Passphrase = ""
		imageOs.template = template
		_, _, err := imageOs.enablingFDE(installRoot, nil, nil)
		if err == nil {
			t.Fatal("expected error for missing passphrase loaded from file")
		}
	})

	t.Run("no targets", func(t *testing.T) {
		template := baseTemplate()
		template.SystemConfig.FDE.Partitions = []string{}
		template.Disk.Partitions = nil
		imageOs.template = template
		_, _, err := imageOs.enablingFDE(installRoot, nil, nil)
		if err == nil {
			t.Fatal("expected error for no targets")
		}
	})

	t.Run("missing disk map entry", func(t *testing.T) {
		originalExecutor := shell.Default
		t.Cleanup(func() { shell.Default = originalExecutor })
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{
			{Pattern: `umount`, Output: "", Error: nil},
		})

		template := baseTemplate()
		imageOs.template = template
		mounts := []map[string]string{{"MountPoint": installRoot}}
		_, _, err := imageOs.enablingFDE(installRoot, map[string]string{}, mounts)
		if err == nil || !strings.Contains(err.Error(), "no device in the disk map") {
			t.Fatalf("expected missing device error, got %v", err)
		}
	})
}

func TestAddFDEKeyToDevice(t *testing.T) {
	originalExecutor := shell.Default
	t.Cleanup(func() { shell.Default = originalExecutor })

	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: `cryptsetup luksAddKey`, Output: "", Error: nil},
	})

	keyfile := filepath.Join(t.TempDir(), "fde.key")
	if err := os.WriteFile(keyfile, []byte("key"), 0400); err != nil {
		t.Fatalf("write keyfile: %v", err)
	}
	if err := addFDEKeyToDevice("/dev/loop0p2", keyfile, "passphrase"); err != nil {
		t.Fatalf("addFDEKeyToDevice: %v", err)
	}
}

func TestWireFDEBootErrors(t *testing.T) {
	template := fdeTestTemplateManual()
	template.SystemConfig.FDE.Unlock = "auto"
	template.SystemConfig.FDE.Passphrase = ""

	installRoot := t.TempDir()
	err := wireFDEBoot(installRoot, map[string]string{"rootfs": "/dev/mapper/rootfs"}, template)
	if err == nil || !strings.Contains(err.Error(), "auto unlock requires a passphrase loaded from systemConfig.fde.passphraseFile") {
		t.Fatalf("wireFDEBoot err = %v", err)
	}

	template.SystemConfig.FDE.Passphrase = "x"
	template.SystemConfig.FDE.Partitions = []string{}
	template.Disk.Partitions = nil
	err = wireFDEBoot(installRoot, nil, template)
	if err == nil {
		t.Fatal("expected no target partition error")
	}
}

func TestFdeCollectBootParamsMissingDevice(t *testing.T) {
	t.Parallel()

	_, _, _, err := fdeCollectBootParams(
		[]string{"missing"},
		"missing",
		false,
		"",
		"",
		"",
		map[string]string{},
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no device in the disk map") {
		t.Errorf("err = %v", err)
	}
}

func TestFdeDiscoverLuksUUIDErrors(t *testing.T) {
	originalExecutor := shell.Default
	t.Cleanup(func() { shell.Default = originalExecutor })

	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: `cryptsetup status`, Output: "type: LUKS2\n", Error: nil},
	})
	if _, _, err := fdeDiscoverLuksUUID("rootfs"); err == nil {
		t.Fatal("expected error when backing device missing")
	}

	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: `cryptsetup status`, Output: "", Error: fmt.Errorf("status failed")},
	})
	if _, _, err := fdeDiscoverLuksUUID("rootfs"); err == nil {
		t.Fatal("expected status error")
	}
}
