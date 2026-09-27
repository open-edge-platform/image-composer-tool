package isomaker

import (
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

func TestSynthesizeRawTemplate(t *testing.T) {
	isoTemplate := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:        "ubuntu",
			Dist:      "noble",
			Arch:      "x86_64",
			ImageType: "iso",
		},
		FullPkgList: []string{"pkg-a.deb", "pkg-b.deb"},
		Disk: config.DiskConfig{
			ExtendLastPartitionToFillDisk: true,
		},
		SystemConfig: config.SystemConfig{
			Users: []config.UserConfig{
				{Name: "root", StartupScript: unattendedInstallerStartupScript},
				{Name: "admin", StartupScript: "/usr/bin/bash"},
			},
		},
	}

	rawTemplate := synthesizeRawTemplate(isoTemplate)

	if rawTemplate.Target.ImageType != "raw" {
		t.Fatalf("Target.ImageType = %q, want raw", rawTemplate.Target.ImageType)
	}
	if rawTemplate.Target.OS != isoTemplate.Target.OS || rawTemplate.Target.Dist != isoTemplate.Target.Dist {
		t.Fatalf("synthesized template diverged on OS/Dist: got %+v, want to match %+v", rawTemplate.Target, isoTemplate.Target)
	}
	if len(rawTemplate.FullPkgList) != len(isoTemplate.FullPkgList) {
		t.Fatalf("FullPkgList not shared: got %v, want %v", rawTemplate.FullPkgList, isoTemplate.FullPkgList)
	}
	// live-installer's deploy path grows the target disk after writing the
	// payload; the payload raw's own first-boot auto-expand service must not
	// also be installed, or the two would race over the same partition.
	if rawTemplate.Disk.ExtendLastPartitionToFillDisk {
		t.Fatalf("synthesized raw template must force ExtendLastPartitionToFillDisk off, got true")
	}
	// The deployed target never has installation media to mount, so root's
	// installer-entrypoint startupScript must not carry over as its login shell.
	if got := rawTemplate.SystemConfig.Users[0].StartupScript; got != "" {
		t.Fatalf("root startupScript = %q, want cleared", got)
	}
	if got := rawTemplate.SystemConfig.Users[1].StartupScript; got != "/usr/bin/bash" {
		t.Fatalf("non-root user startupScript changed: got %q, want unchanged /usr/bin/bash", got)
	}
	// The original template must be untouched (shallow copy, not aliasing Target/Disk/Users).
	if isoTemplate.Target.ImageType != "iso" {
		t.Fatalf("synthesizeRawTemplate mutated the original template's ImageType: %q", isoTemplate.Target.ImageType)
	}
	if !isoTemplate.Disk.ExtendLastPartitionToFillDisk {
		t.Fatalf("synthesizeRawTemplate mutated the original template's Disk.ExtendLastPartitionToFillDisk")
	}
	if isoTemplate.SystemConfig.Users[0].StartupScript != unattendedInstallerStartupScript {
		t.Fatalf("synthesizeRawTemplate mutated the original template's root startupScript: %q",
			isoTemplate.SystemConfig.Users[0].StartupScript)
	}
}

func TestPayloadPassthroughConvert(t *testing.T) {
	c := payloadPassthroughConvert{}

	t.Run("no artifacts", func(t *testing.T) {
		tmpl := &config.ImageTemplate{}
		if err := c.ConvertImageFile("/tmp/payload.raw", tmpl); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	})

	t.Run("artifacts present are ignored, not applied", func(t *testing.T) {
		tmpl := &config.ImageTemplate{
			Disk: config.DiskConfig{
				Artifacts: []config.ArtifactInfo{{Type: "qcow2"}},
			},
		}
		if err := c.ConvertImageFile("/tmp/payload.raw", tmpl); err != nil {
			t.Fatalf("expected no error (artifacts must be ignored, not enforced), got: %v", err)
		}
	})
}

func TestPayloadGraftPathspecs(t *testing.T) {
	tests := []struct {
		name   string
		maker  IsoMaker
		want   int
		substr []string
	}{
		{
			name:  "no payload files staged",
			maker: IsoMaker{},
			want:  0,
		},
		{
			name: "all three payload files staged",
			maker: IsoMaker{
				payloadCompressedPath: "/build/payload/payload.raw.zst",
				payloadManifestPath:   "/build/payload/payload.manifest.yaml",
				payloadSBOMPath:       "/build/payload/payload.sbom.json",
			},
			want: 3,
			substr: []string{
				"/payload/payload.raw.zst=/build/payload/payload.raw.zst",
				"/payload/payload.manifest.yaml=/build/payload/payload.manifest.yaml",
				"/payload/payload.sbom.json=/build/payload/payload.sbom.json",
			},
		},
		{
			name: "sbom snapshot failed, only two files staged",
			maker: IsoMaker{
				payloadCompressedPath: "/build/payload/payload.raw.zst",
				payloadManifestPath:   "/build/payload/payload.manifest.yaml",
			},
			want: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.maker.payloadGraftPathspecs()
			if len(got) != tt.want {
				t.Fatalf("payloadGraftPathspecs() = %v, want %d entries", got, tt.want)
			}
			for _, want := range tt.substr {
				found := false
				for _, g := range got {
					if g == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected pathspec %q in %v", want, got)
				}
			}
		})
	}
}

func TestBuildXorrisoCommand(t *testing.T) {
	base := xorrisoArgs{
		isoRoot:          "/build/isoroot",
		isoFilePath:      "/build/out.iso",
		isoLabel:         "ICT_CDROM",
		efiFatImgRelPath: "/EFI/BOOT/efi.img",
		efiFatImgPath:    "/build/isoroot/EFI/BOOT/efi.img",
	}

	t.Run("UEFI only", func(t *testing.T) {
		cmd := buildXorrisoCommand(base)
		if !strings.Contains(cmd, "-iso-level 3") {
			t.Errorf("missing -iso-level 3 in UEFI-only command: %s", cmd)
		}
		if !strings.Contains(cmd, "--efi-boot") {
			t.Errorf("expected UEFI-only branch, got: %s", cmd)
		}
		if strings.Contains(cmd, "-b ") {
			t.Errorf("UEFI-only command should not include a BIOS el-torito boot flag: %s", cmd)
		}
	})

	t.Run("hybrid BIOS+UEFI", func(t *testing.T) {
		a := base
		a.biosImgRelPath = "boot/grub/i386-pc/eltorito.img"
		cmd := buildXorrisoCommand(a)
		if !strings.Contains(cmd, "-iso-level 3") {
			t.Errorf("missing -iso-level 3 in hybrid command: %s", cmd)
		}
		if !strings.Contains(cmd, "-b boot/grub/i386-pc/eltorito.img") {
			t.Errorf("expected BIOS el-torito boot flag: %s", cmd)
		}
		if !strings.Contains(cmd, "--grub2-mbr") {
			t.Errorf("expected grub2-mbr flag in hybrid command: %s", cmd)
		}
	})

	t.Run("graft pathspecs appended when present", func(t *testing.T) {
		a := base
		a.graftPathspecs = []string{"/payload/payload.raw.zst=/build/payload/payload.raw.zst"}
		cmd := buildXorrisoCommand(a)
		if !strings.Contains(cmd, "/payload/payload.raw.zst=/build/payload/payload.raw.zst") {
			t.Errorf("expected graft pathspec in command: %s", cmd)
		}
	})

	t.Run("no graft pathspecs when absent", func(t *testing.T) {
		cmd := buildXorrisoCommand(base)
		if strings.Contains(cmd, "/payload/") {
			t.Errorf("unexpected /payload/ reference with no graft pathspecs: %s", cmd)
		}
	})

	t.Run("no double pipe in either branch", func(t *testing.T) {
		for _, biosPath := range []string{"", "boot/grub/i386-pc/eltorito.img"} {
			a := base
			a.biosImgRelPath = biosPath
			cmd := buildXorrisoCommand(a)
			if strings.Contains(cmd, "||") {
				t.Errorf("emitted command must never contain ||  (shell allowlist trap): %s", cmd)
			}
		}
	})
}

// validPayloadDiskConfig returns a minimal disk config that satisfies
// validateInstallerPayloadDiskLayout: gpt table, a growable (end: "0") root
// partition on a resizable filesystem.
func validPayloadDiskConfig(size string) config.DiskConfig {
	return config.DiskConfig{
		Size:               size,
		PartitionTableType: "gpt",
		Partitions: []config.PartitionInfo{
			{ID: "root", End: "0", FsType: "ext4", MountPoint: "/"},
		},
	}
}

func TestValidateInstallerPayloadPrerequisites(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: "df", Output: "999999999999", Error: nil},
	})

	unattendedRootUser := []config.UserConfig{{Name: "root", StartupScript: unattendedInstallerStartupScript}}
	initrdWithDecompressor := &config.ImageTemplate{
		SystemConfig: config.SystemConfig{
			Packages: []string{"zstd", "e2fsprogs", "util-linux", "fdisk"},
			Users:    unattendedRootUser,
		},
	}
	initrdWithoutDecompressor := &config.ImageTemplate{
		SystemConfig: config.SystemConfig{
			Packages: []string{"e2fsprogs", "util-linux", "fdisk"},
			Users:    unattendedRootUser,
		},
	}

	tests := []struct {
		name          string
		template      *config.ImageTemplate
		initrd        *config.ImageTemplate
		expectError   bool
		errorContains string
	}{
		{
			name: "missing disk size",
			template: &config.ImageTemplate{
				Disk: config.DiskConfig{},
			},
			initrd:        initrdWithDecompressor,
			expectError:   true,
			errorContains: "disk.size to be set",
		},
		{
			name: "invalid disk size format",
			template: &config.ImageTemplate{
				Disk: config.DiskConfig{Size: "not-a-size"},
			},
			initrd:        initrdWithDecompressor,
			expectError:   true,
			errorContains: "invalid disk.size",
		},
		{
			name: "missing decompressor package",
			template: &config.ImageTemplate{
				Disk: config.DiskConfig{Size: "20G"},
				SystemConfig: config.SystemConfig{
					InstallerPayload: &config.InstallerPayload{Enabled: true, Compression: "zstd"},
				},
			},
			initrd:        initrdWithoutDecompressor,
			expectError:   true,
			errorContains: "requires package \"zstd\"",
		},
		{
			name: "valid with zstd decompressor present",
			template: &config.ImageTemplate{
				Disk: validPayloadDiskConfig("20G"),
				SystemConfig: config.SystemConfig{
					InstallerPayload: &config.InstallerPayload{Enabled: true, Compression: "zstd"},
				},
			},
			initrd:      initrdWithDecompressor,
			expectError: false,
		},
		{
			name: "immutability enabled rejected",
			template: &config.ImageTemplate{
				Disk: validPayloadDiskConfig("20G"),
				SystemConfig: config.SystemConfig{
					InstallerPayload: &config.InstallerPayload{Enabled: true, Compression: "none"},
					Immutability:     config.ImmutabilityConfig{Enabled: true},
				},
			},
			initrd:        initrdWithoutDecompressor,
			expectError:   true,
			errorContains: "immutability",
		},
		{
			name: "attended initrd rejected",
			template: &config.ImageTemplate{
				Disk: validPayloadDiskConfig("20G"),
				SystemConfig: config.SystemConfig{
					InstallerPayload: &config.InstallerPayload{Enabled: true, Compression: "none"},
				},
			},
			initrd: &config.ImageTemplate{
				SystemConfig: config.SystemConfig{
					Users: []config.UserConfig{{Name: "root", StartupScript: attendedInstallerStartupScript}},
				},
			},
			expectError:   true,
			errorContains: "unattended initrd",
		},
		{
			name: "initrd with no root user rejected",
			template: &config.ImageTemplate{
				Disk: validPayloadDiskConfig("20G"),
				SystemConfig: config.SystemConfig{
					InstallerPayload: &config.InstallerPayload{Enabled: true, Compression: "none"},
				},
			},
			initrd: &config.ImageTemplate{
				SystemConfig: config.SystemConfig{Users: []config.UserConfig{{Name: "admin"}}},
			},
			expectError:   true,
			errorContains: "unattended initrd",
		},
		{
			name: "initrd with unrelated root startupScript rejected",
			template: &config.ImageTemplate{
				Disk: validPayloadDiskConfig("20G"),
				SystemConfig: config.SystemConfig{
					InstallerPayload: &config.InstallerPayload{Enabled: true, Compression: "none"},
				},
			},
			initrd: &config.ImageTemplate{
				SystemConfig: config.SystemConfig{
					Users: []config.UserConfig{{Name: "root", StartupScript: "/root/some-other-script"}},
				},
			},
			expectError:   true,
			errorContains: "unattended initrd",
		},
		{
			name: "missing util-linux package rejected",
			template: &config.ImageTemplate{
				Disk: validPayloadDiskConfig("20G"),
				SystemConfig: config.SystemConfig{
					InstallerPayload: &config.InstallerPayload{Enabled: true, Compression: "zstd"},
				},
			},
			initrd: &config.ImageTemplate{
				SystemConfig: config.SystemConfig{
					Packages: []string{"zstd", "e2fsprogs"},
					Users:    unattendedRootUser,
				},
			},
			expectError:   true,
			errorContains: "\"util-linux\"",
		},
		{
			name: "missing sfdisk-providing package rejected",
			template: &config.ImageTemplate{
				Disk: validPayloadDiskConfig("20G"),
				SystemConfig: config.SystemConfig{
					InstallerPayload: &config.InstallerPayload{Enabled: true, Compression: "zstd"},
				},
			},
			initrd: &config.ImageTemplate{
				SystemConfig: config.SystemConfig{
					// util-linux alone doesn't carry sfdisk on Debian-family
					// systems (it moved to the separate "fdisk" package).
					Packages: []string{"zstd", "e2fsprogs", "util-linux"},
					Users:    unattendedRootUser,
				},
			},
			expectError:   true,
			errorContains: "\"fdisk\"",
		},
		{
			name: "missing growth tool package rejected",
			template: &config.ImageTemplate{
				Disk: validPayloadDiskConfig("20G"),
				SystemConfig: config.SystemConfig{
					InstallerPayload: &config.InstallerPayload{Enabled: true, Compression: "zstd"},
				},
			},
			initrd: &config.ImageTemplate{
				SystemConfig: config.SystemConfig{
					Packages: []string{"zstd", "util-linux", "fdisk"},
					Users:    unattendedRootUser,
				},
			},
			expectError:   true,
			errorContains: "\"e2fsprogs\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateInstallerPayloadPrerequisites(tt.template, tt.initrd)
			if tt.expectError {
				if err == nil {
					t.Fatal("expected error, got none")
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Fatalf("expected error containing %q, got: %v", tt.errorContains, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
		})
	}
}

func TestValidateInstallerPayloadDiskLayout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                  string
		diskCfg               config.DiskConfig
		resetInstanceIdentity bool
		expectError           bool
		errorContains         string
	}{
		{
			name:        "valid gpt with growable root",
			diskCfg:     validPayloadDiskConfig("20G"),
			expectError: false,
		},
		{
			name: "mbr rejected",
			diskCfg: config.DiskConfig{
				PartitionTableType: "mbr",
				Partitions:         []config.PartitionInfo{{ID: "root", End: "0", FsType: "ext4", MountPoint: "/"}},
			},
			expectError:   true,
			errorContains: "partitionTableType",
		},
		{
			name: "lvm partition rejected",
			diskCfg: config.DiskConfig{
				PartitionTableType: "gpt",
				Partitions: []config.PartitionInfo{
					{ID: "pv", Type: "linux-lvm", End: "0"},
				},
			},
			expectError:   true,
			errorContains: "LVM",
		},
		{
			name: "missing growable partition rejected",
			diskCfg: config.DiskConfig{
				PartitionTableType: "gpt",
				Partitions:         []config.PartitionInfo{{ID: "root", End: "10GiB", FsType: "ext4", MountPoint: "/"}},
			},
			expectError:   true,
			errorContains: `end: "0"`,
		},
		{
			name: "btrfs growable partition rejected",
			diskCfg: config.DiskConfig{
				PartitionTableType: "gpt",
				Partitions:         []config.PartitionInfo{{ID: "root", End: "0", FsType: "btrfs", MountPoint: "/"}},
			},
			expectError:   true,
			errorContains: "btrfs",
		},
		{
			name: "fat32 growable partition rejected",
			diskCfg: config.DiskConfig{
				PartitionTableType: "gpt",
				Partitions:         []config.PartitionInfo{{ID: "root", End: "0", FsType: "fat32", MountPoint: "/"}},
			},
			expectError:   true,
			errorContains: "fat32",
		},
		{
			name: "swap growable partition allowed",
			diskCfg: config.DiskConfig{
				PartitionTableType: "gpt",
				Partitions: []config.PartitionInfo{
					{ID: "root", End: "10GiB", FsType: "ext4", MountPoint: "/"},
					{ID: "swap", End: "0", FsType: "swap"},
				},
			},
			expectError: false,
		},
		{
			name: "separate /etc partition rejected when identity reset enabled",
			diskCfg: config.DiskConfig{
				PartitionTableType: "gpt",
				Partitions: []config.PartitionInfo{
					{ID: "root", End: "10GiB", FsType: "ext4", MountPoint: "/"},
					{ID: "etc", End: "0", FsType: "ext4", MountPoint: "/etc"},
				},
			},
			resetInstanceIdentity: true,
			expectError:           true,
			errorContains:         "resetInstanceIdentity",
		},
		{
			name: "separate /var/lib partition rejected when identity reset enabled",
			diskCfg: config.DiskConfig{
				PartitionTableType: "gpt",
				Partitions: []config.PartitionInfo{
					{ID: "root", End: "10GiB", FsType: "ext4", MountPoint: "/"},
					{ID: "varlib", End: "0", FsType: "ext4", MountPoint: "/var/lib"},
				},
			},
			resetInstanceIdentity: true,
			expectError:           true,
			errorContains:         "resetInstanceIdentity",
		},
		{
			name: "separate /etc partition allowed when identity reset disabled",
			diskCfg: config.DiskConfig{
				PartitionTableType: "gpt",
				Partitions: []config.PartitionInfo{
					{ID: "root", End: "10GiB", FsType: "ext4", MountPoint: "/"},
					{ID: "etc", End: "0", FsType: "ext4", MountPoint: "/etc"},
				},
			},
			resetInstanceIdentity: false,
			expectError:           false,
		},
		{
			name: "missing root partition rejected when identity reset enabled",
			diskCfg: config.DiskConfig{
				PartitionTableType: "gpt",
				Partitions:         []config.PartitionInfo{{ID: "data", End: "0", FsType: "ext4"}},
			},
			resetInstanceIdentity: true,
			expectError:           true,
			errorContains:         "resetInstanceIdentity",
		},
		{
			name: "missing root partition allowed when identity reset disabled",
			diskCfg: config.DiskConfig{
				PartitionTableType: "gpt",
				Partitions:         []config.PartitionInfo{{ID: "data", End: "0", FsType: "ext4"}},
			},
			resetInstanceIdentity: false,
			expectError:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateInstallerPayloadDiskLayout(tt.diskCfg, tt.resetInstanceIdentity)
			if tt.expectError {
				if err == nil {
					t.Fatal("expected error, got none")
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Fatalf("expected error containing %q, got: %v", tt.errorContains, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
		})
	}
}
