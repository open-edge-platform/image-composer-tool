package imageos

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

func TestInstallRootfsRpmHappyPath(t *testing.T) {
	originalExecutor := shell.Default
	t.Cleanup(func() { shell.Default = originalExecutor })

	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: `rpm --root`, Output: "", Error: nil},
		{Pattern: `rpm -qa`, Output: "pkg-a\npkg-b", Error: nil},
		{Pattern: `.*`, Output: "", Error: nil},
	})

	installRoot := t.TempDir()
	template := createTestImageTemplate()
	template.Target.OS = "redhat-compatible-distro"
	template.Target.ImageType = "raw"
	template.SystemConfig.Network.Backend = "netplan"
	template.FullPkgListBom = nil

	mockChroot := &MockChrootEnv{
		chrootImageBuildDir: installRoot,
		pkgType:             "rpm",
		chrootPath:          "/chroot/install",
		chrootRoot:          t.TempDir(),
	}

	imageOs := &ImageOs{
		installRoot: installRoot,
		chrootEnv:   mockChroot,
		template:    template,
	}

	gotRoot, versionInfo, err := imageOs.InstallRootfs()
	if err != nil {
		t.Fatalf("InstallRootfs() err = %v", err)
	}
	if gotRoot != installRoot {
		t.Errorf("InstallRootfs() root = %q, want %q", gotRoot, installRoot)
	}
	if versionInfo == "" {
		t.Error("InstallRootfs() expected non-empty version info from mock chroot release")
	}
}

func TestAddImageConfigs(t *testing.T) {
	originalExecutor := shell.Default
	t.Cleanup(func() { shell.Default = originalExecutor })

	installRoot := t.TempDir()
	template := &config.ImageTemplate{
		SystemConfig: config.SystemConfig{
			Configurations: []config.ConfigurationInfo{
				{Cmd: "echo configured"},
			},
		},
	}

	t.Run("no custom configs", func(t *testing.T) {
		empty := &config.ImageTemplate{}
		if err := addImageConfigs(installRoot, empty); err != nil {
			t.Fatalf("addImageConfigs empty: %v", err)
		}
	})

	t.Run("executes chroot command", func(t *testing.T) {
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{
			{Pattern: `chroot .*/bin/bash -c`, Output: "", Error: nil},
		})
		if err := addImageConfigs(installRoot, template); err != nil {
			t.Fatalf("addImageConfigs() err = %v", err)
		}
	})

	t.Run("command failure", func(t *testing.T) {
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{
			{Pattern: `chroot`, Output: "", Error: os.ErrInvalid},
		})
		err := addImageConfigs(installRoot, template)
		if err == nil {
			t.Fatal("addImageConfigs() expected error")
		}
		if !strings.Contains(err.Error(), "echo configured") {
			t.Errorf("addImageConfigs() err = %v, want command in message", err)
		}
	})
}

func TestUpdateRootfsConfigMinimal(t *testing.T) {
	originalExecutor := shell.Default
	t.Cleanup(func() { shell.Default = originalExecutor })

	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: `.*`, Output: "", Error: nil},
	})

	installRoot := t.TempDir()
	template := createTestImageTemplate()
	template.SystemConfig.HostName = "test-host"
	template.SystemConfig.Network.Backend = "netplan"

	if err := updateRootfsConfig(installRoot, template); err != nil {
		t.Fatalf("updateRootfsConfig() err = %v", err)
	}
}

func TestInstalledPackageNamesAsSBOMMetadataDirect(t *testing.T) {
	t.Parallel()

	imageOs := &ImageOs{}
	got := imageOs.installedPackageNamesAsSBOMMetadata(
		[]string{"", "curl:amd64", "vim"},
		"deb",
	)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Name != "curl" || got[0].Type != "deb" {
		t.Errorf("first pkg = %+v", got[0])
	}
	if got[1].Name != "vim" {
		t.Errorf("second pkg name = %q", got[1].Name)
	}
}

func TestInstallImagePkgsDebSpecialPackages(t *testing.T) {
	originalExecutor := shell.Default
	t.Cleanup(func() { shell.Default = originalExecutor })

	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: `apt-get update`, Output: "", Error: nil},
		{Pattern: `apt-get install`, Output: "EFI Boot variable not set", Error: fmt.Errorf("apt failed")},
		{Pattern: `dpkg-divert`, Output: "", Error: fmt.Errorf("divert unavailable")},
		{Pattern: `.*`, Output: "", Error: nil},
	})

	testDir := t.TempDir()
	installRoot := filepath.Join(testDir, "install-root")
	if err := os.MkdirAll(filepath.Join(installRoot, "etc", "apt", "sources.list.d"), 0755); err != nil {
		t.Fatalf("mkdir apt: %v", err)
	}
	apparmorParser := filepath.Join(installRoot, "usr", "sbin", "apparmor_parser")
	if err := os.MkdirAll(filepath.Dir(apparmorParser), 0755); err != nil {
		t.Fatalf("mkdir sbin: %v", err)
	}
	if err := os.WriteFile(apparmorParser, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("write apparmor_parser: %v", err)
	}

	configDir := filepath.Join(testDir, "config", "chrootenvconfigs")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	localListPath := filepath.Join(configDir, "local.list")
	if err := os.WriteFile(localListPath, []byte("deb file:///repo bookworm main\n"), 0644); err != nil {
		t.Fatalf("write local.list: %v", err)
	}

	template := createTestImageTemplate()
	template.Target.OS = "ubuntu"
	template.SystemConfig.Packages = []string{"apparmor", "curl"}
	template.BootloaderPkgList = []string{"systemd-boot"}

	mockChroot := &cleanupCountMockChrootEnv{
		MockChrootEnv: MockChrootEnv{
			pkgType:    "deb",
			chrootPath: installRoot,
		},
		targetConfigDir: filepath.Join(testDir, "config"),
		cacheDir:        filepath.Join(testDir, "cache"),
	}

	imageOs := &ImageOs{
		installRoot: installRoot,
		chrootEnv:   mockChroot,
		template:    template,
	}

	if err := imageOs.installImagePkgs(installRoot, template); err != nil {
		t.Fatalf("installImagePkgs: %v", err)
	}
	if mockChroot.umountCalls != 1 {
		t.Fatalf("umount calls = %d, want 1", mockChroot.umountCalls)
	}
}

func TestUpdateInitramfsEMT(t *testing.T) {
	originalExecutor := shell.Default
	t.Cleanup(func() { shell.Default = originalExecutor })

	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: `dracut`, Output: "", Error: nil},
	})

	template := createTestImageTemplate()
	template.Target.OS = "edge-microvisor-toolkit"

	if err := updateInitramfs(t.TempDir(), "6.8.0", template); err != nil {
		t.Fatalf("updateInitramfs EMT: %v", err)
	}
}

func TestWireFDEBootMissingCmdline(t *testing.T) {
	originalExecutor := shell.Default
	t.Cleanup(func() { shell.Default = originalExecutor })

	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: `cryptsetup status`, Output: "device: /dev/loop0p2\n", Error: nil},
		{Pattern: `cryptsetup luksUUID`, Output: "uuid-1\n", Error: nil},
	})

	template := fdeTestTemplateManual()
	err := wireFDEBoot(t.TempDir(), map[string]string{"rootfs": "/dev/mapper/rootfs"}, template)
	if err == nil || !strings.Contains(err.Error(), "failed to read cmdline") {
		t.Fatalf("wireFDEBoot err = %v", err)
	}
}

func TestGetImageVersionInfoFromChrootRelease(t *testing.T) {
	mockChroot := &MockChrootEnv{}
	imageOs := &ImageOs{
		installRoot: t.TempDir(),
		chrootEnv:   mockChroot,
		template:    createTestImageTemplate(),
	}

	version, err := imageOs.getImageVersionInfo(imageOs.installRoot, imageOs.template)
	if err != nil {
		t.Fatalf("getImageVersionInfo: %v", err)
	}
	if version != "1.0" {
		t.Errorf("version = %q, want 1.0", version)
	}
}

func TestCloseLuksError(t *testing.T) {
	originalExecutor := shell.Default
	t.Cleanup(func() { shell.Default = originalExecutor })

	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: `cryptsetup close`, Output: "", Error: fmt.Errorf("close failed")},
	})

	if err := closeLuks("missing"); err == nil {
		t.Fatal("expected closeLuks error")
	}
}

func TestUpdateInitramfsDracutFailure(t *testing.T) {
	originalExecutor := shell.Default
	t.Cleanup(func() { shell.Default = originalExecutor })

	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: `dracut`, Output: "dracut failed", Error: fmt.Errorf("dracut error")},
	})

	template := createTestImageTemplate()
	err := updateInitramfs(t.TempDir(), "6.8.0", template)
	if err == nil {
		t.Fatal("expected dracut failure")
	}
	if !strings.Contains(err.Error(), "failed to update initramfs") {
		t.Errorf("err = %v", err)
	}
}

func TestGetUkifyStubPathUnsupportedArch(t *testing.T) {
	t.Parallel()

	if _, err := getUkifyStubPath("riscv64"); err == nil {
		t.Fatal("expected unsupported architecture error")
	}
}

func TestGetUkifyStubPathCandidates(t *testing.T) {
	t.Parallel()

	candidates, err := getUkifyStubPathCandidates("x86_64")
	if err != nil {
		t.Fatalf("getUkifyStubPathCandidates: %v", err)
	}
	if len(candidates) != 2 || !strings.HasSuffix(candidates[1], ".signed") {
		t.Errorf("candidates = %v", candidates)
	}
}

func TestUpdateInitramfsFDE(t *testing.T) {
	originalExecutor := shell.Default
	t.Cleanup(func() { shell.Default = originalExecutor })

	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: `dracut`, Output: "", Error: nil},
	})

	template := createTestImageTemplate()
	template.SystemConfig.FDE = config.FDEConfig{Enabled: true}

	if err := updateInitramfs(t.TempDir(), "6.8.0", template); err != nil {
		t.Fatalf("updateInitramfs FDE: %v", err)
	}
}

func TestUpdateInitramfsImmutabilityAndAutoUnlock(t *testing.T) {
	originalExecutor := shell.Default
	t.Cleanup(func() { shell.Default = originalExecutor })

	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: `dracut`, Output: "", Error: nil},
	})

	template := createTestImageTemplate()
	template.SystemConfig.Immutability.Enabled = true
	template.SystemConfig.FDE = config.FDEConfig{Enabled: true, Unlock: "auto"}

	if err := updateInitramfs(t.TempDir(), "6.8.0", template); err != nil {
		t.Fatalf("updateInitramfs immutability: %v", err)
	}
}

func TestInstallImagePkgsUnsupportedType(t *testing.T) {
	imageOs := &ImageOs{
		installRoot: t.TempDir(),
		chrootEnv:   &MockChrootEnv{pkgType: "apk"},
		template:    createTestImageTemplate(),
	}
	if err := imageOs.installImagePkgs(imageOs.installRoot, imageOs.template); err == nil {
		t.Fatal("expected unsupported package type error")
	}
}

func TestIsNonMountablePartitionSwap(t *testing.T) {
	t.Parallel()

	part := config.PartitionInfo{MountPoint: "", FsType: "linux-swap"}
	if !isNonMountablePartition(part) {
		t.Error("swap partition should be non-mountable")
	}
}

func TestInstallInitrdRpmHappyPath(t *testing.T) {
	originalExecutor := shell.Default
	t.Cleanup(func() { shell.Default = originalExecutor })

	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: `rpm --root`, Output: "", Error: nil},
		{Pattern: `.*`, Output: "", Error: nil},
	})

	installRoot := t.TempDir()
	template := createTestImageTemplate()
	template.Target.OS = "redhat-compatible-distro"
	template.SystemConfig.Network.Backend = "netplan"

	mockChroot := &MockChrootEnv{
		chrootImageBuildDir: installRoot,
		pkgType:             "rpm",
		chrootPath:          "/chroot/install",
		chrootRoot:          t.TempDir(),
	}

	imageOs := &ImageOs{
		installRoot: installRoot,
		chrootEnv:   mockChroot,
		template:    template,
	}

	gotRoot, versionInfo, err := imageOs.InstallInitrd()
	if err != nil {
		t.Fatalf("InstallInitrd() err = %v", err)
	}
	if gotRoot != installRoot {
		t.Errorf("InstallInitrd() root = %q, want %q", gotRoot, installRoot)
	}
	if versionInfo == "" {
		t.Error("InstallInitrd() expected version info")
	}
}

func TestCopyBootloader(t *testing.T) {
	originalExecutor := shell.Default
	t.Cleanup(func() { shell.Default = originalExecutor })

	installRoot := t.TempDir()
	src := filepath.Join(installRoot, "src.efi")
	dst := filepath.Join(installRoot, "dst.efi")
	if err := os.WriteFile(src, []byte("efi"), 0644); err != nil {
		t.Fatalf("write src: %v", err)
	}

	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: `cp `, Output: "", Error: nil},
	})

	if err := copyBootloader(installRoot, src, dst); err != nil {
		t.Fatalf("copyBootloader: %v", err)
	}
}

func TestBuildImageUKISkipsNonSystemdBoot(t *testing.T) {
	template := createTestImageTemplate()
	template.SystemConfig.Bootloader = config.Bootloader{Provider: "grub"}
	if err := buildImageUKI(t.TempDir(), template); err != nil {
		t.Fatalf("buildImageUKI skip: %v", err)
	}
}

func TestPrepareInitramfsBinariesForDebInstall(t *testing.T) {
	originalExecutor := shell.Default
	t.Cleanup(func() { shell.Default = originalExecutor })

	installRoot := t.TempDir()
	binDir := filepath.Join(installRoot, "usr", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	binaryPath := filepath.Join(binDir, "dracut")
	if err := os.WriteFile(binaryPath, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: `dpkg-divert`, Output: "", Error: fmt.Errorf("divert unavailable")},
		{Pattern: `chmod`, Output: "", Error: nil},
	})

	backups, diverted := prepareInitramfsBinariesForDebInstall(
		installRoot,
		[]string{"/usr/bin/dracut"},
	)
	if len(diverted) != 0 {
		t.Errorf("diverted = %v", diverted)
	}
	if len(backups) != 1 {
		t.Fatalf("backups = %v", backups)
	}
	restoreInitramfsBinariesAfterDebInstall(installRoot, backups, diverted)
}
