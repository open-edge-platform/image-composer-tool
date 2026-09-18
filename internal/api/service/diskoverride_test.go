// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"gopkg.in/yaml.v3"
)

// rawDisk is a minimal valid override modelled on
// image-templates/ubuntu24-x86_64-minimal-ptl-pv-raw.yml: an ESP, a swap, and a
// rootfs taking the remainder.
func rawDisk() *DiskOverride {
	return &DiskOverride{
		Name:               "minimal-desktop-ubuntu-ptl-pv",
		Size:               "32GiB",
		PartitionTableType: "gpt",
		Artifacts:          []DiskArtifactOverride{{Type: "raw", Compression: "gz"}},
		Partitions: []DiskPartitionOverride{
			{ID: "EFI", Type: "esp", FsType: "vfat", Start: "1MiB", End: "1025MiB", MountPoint: "/boot/efi"},
			{ID: "SWAP", Type: "linux-swap", FsType: "linux-swap", Start: "1025MiB", End: "3073MiB", MountPoint: "none"},
			{ID: "ROOT", Type: "linux-root-amd64", FsType: "ext4", Start: "3073MiB", End: "0", MountPoint: "/"},
		},
	}
}

func TestValidateDiskNilIsNotOverridden(t *testing.T) {
	if err := ValidateDisk(nil, "raw"); err != nil {
		t.Errorf("ValidateDisk(nil) = %v, want nil", err)
	}
}

func TestValidateDiskAcceptsATemplateShapedOverride(t *testing.T) {
	if err := ValidateDisk(rawDisk(), "raw"); err != nil {
		t.Errorf("ValidateDisk = %v, want nil", err)
	}
}

func TestValidateDiskTopLevelFields(t *testing.T) {
	cases := []struct {
		name  string
		mutit func(*DiskOverride)
		want  string // substring of the expected error; "" means valid
	}{
		{"name required", func(d *DiskOverride) { d.Name = "" }, "disk name is required"},
		{"name pattern", func(d *DiskOverride) { d.Name = "../evil" }, "must match"},
		{"name space", func(d *DiskOverride) { d.Name = "has space" }, "must match"},
		{"name too long", func(d *DiskOverride) { d.Name = strings.Repeat("a", 129) }, "exceeds"},
		{"name underscore ok", func(d *DiskOverride) { d.Name = "Default_ISO" }, ""},
		{"path empty ok", func(d *DiskOverride) { d.Path = "" }, ""},
		{"path dev ok", func(d *DiskOverride) { d.Path = "/dev/sda" }, ""},
		{"path traversal", func(d *DiskOverride) { d.Path = "../../etc/passwd" }, "/dev device path"},
		{"path absolute non-dev", func(d *DiskOverride) { d.Path = "/etc/shadow" }, "/dev device path"},
		// The builder's suffix table is stricter than it looks.
		{"size decimal", func(d *DiskOverride) { d.Size = "1.5GiB" }, "whole number"},
		{"size lowercase", func(d *DiskOverride) { d.Size = "32gib" }, "whole number"},
		{"size bare bytes", func(d *DiskOverride) { d.Size = "34359738368" }, "whole number"},
		{"size TiB", func(d *DiskOverride) { d.Size = "1TiB" }, "whole number"},
		{"size GB ok", func(d *DiskOverride) { d.Size = "32GB" }, ""},
		{"maxSize without size", func(d *DiskOverride) { d.Size = ""; d.MaxSize = "64GiB" }, "requires size"},
		{"table type", func(d *DiskOverride) { d.PartitionTableType = "apm" }, "partitionTableType"},
		{"table mbr ok", func(d *DiskOverride) { d.PartitionTableType = "mbr" }, ""},
		{"strategy", func(d *DiskOverride) {
			d.SelectionPolicy = &DiskPolicyOverride{Strategy: "smallest"}
		}, "strategy"},
		{"strategy ok", func(d *DiskOverride) {
			d.SelectionPolicy = &DiskPolicyOverride{Strategy: "largest"}
		}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := rawDisk()
			c.mutit(d)
			err := ValidateDisk(d, "raw")
			assertErrContains(t, err, c.want)
		})
	}
}

func TestValidateDiskPartitionFields(t *testing.T) {
	cases := []struct {
		name  string
		mutit func(*DiskOverride)
		want  string
	}{
		{"fsType required", func(d *DiskOverride) { d.Partitions[0].FsType = "" }, "fsType is required"},
		{"fsType unlisted", func(d *DiskOverride) { d.Partitions[0].FsType = "btrfs" }, "fsType \"btrfs\""},
		{"start decimal", func(d *DiskOverride) { d.Partitions[0].Start = "1.5MiB" }, "start \"1.5MiB\""},
		{"end bare bytes", func(d *DiskOverride) { d.Partitions[0].End = "1048576" }, "end \"1048576\""},
		// "0" is the rest-of-disk sentinel and only legal on the last entry.
		{"interior rest", func(d *DiskOverride) { d.Partitions[0].End = "0" }, "only the last partition"},
		{"last rest ok", func(d *DiskOverride) { d.Partitions[2].End = "0" }, ""},
		{"index zero", func(d *DiskOverride) { i := 0; d.Partitions[0].Index = &i }, "index 0"},
		{"index ok", func(d *DiskOverride) { i := 1; d.Partitions[0].Index = &i }, ""},
		{"field too long", func(d *DiskOverride) {
			d.Partitions[0].MountPoint = "/" + strings.Repeat("a", 300)
		}, "mountPoint exceeds"},
		{"too many flags", func(d *DiskOverride) {
			d.Partitions[0].Flags = []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}
		}, "too many flags"},
		{"empty flag", func(d *DiskOverride) { d.Partitions[0].Flags = []string{""} }, "non-empty"},
		// An unrecognised type only degrades the build, so it must not be rejected.
		{"unknown type allowed", func(d *DiskOverride) { d.Partitions[0].Type = "linux-root-riscv64" }, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := rawDisk()
			c.mutit(d)
			assertErrContains(t, ValidateDisk(d, "raw"), c.want)
		})
	}
}

func TestValidateDiskTooManyPartitions(t *testing.T) {
	d := rawDisk()
	d.Partitions = make([]DiskPartitionOverride, maxDiskPartitions+1)
	if err := ValidateDisk(d, "raw"); err == nil || !strings.Contains(err.Error(), "too many partitions") {
		t.Errorf("ValidateDisk = %v, want a too-many-partitions error", err)
	}
}

// The artifact rules are the ones the template schema gets wrong: its enums
// include values no converter or compressor implements.
func TestValidateDiskArtifactsByImageType(t *testing.T) {
	cases := []struct {
		name      string
		imageType string
		arts      []DiskArtifactOverride
		want      string
	}{
		{"raw qcow2+xz", "raw", []DiskArtifactOverride{{Type: "qcow2", Compression: "xz"}}, ""},
		{"raw no compression", "raw", []DiskArtifactOverride{{Type: "vhdx"}}, ""},
		{"raw empty list", "raw", nil, ""},
		// convertImageFile has no `tar` case, despite the schema listing it.
		{"raw tar", "raw", []DiskArtifactOverride{{Type: "tar", Compression: "gz"}}, "type \"tar\""},
		// CompressFile implements neither, despite the schema listing both.
		{"raw bz2", "raw", []DiskArtifactOverride{{Type: "raw", Compression: "bz2"}}, "compression \"bz2\""},
		{"raw gzip", "raw", []DiskArtifactOverride{{Type: "raw", Compression: "gzip"}}, "compression \"gzip\""},
		{"raw unknown type", "raw", []DiskArtifactOverride{{Type: "squashfs"}}, "type \"squashfs\""},
		{"raw missing type", "raw", []DiskArtifactOverride{{Compression: "gz"}}, "type \"\""},
		// wsl2maker requires exactly tar+gz, and requires it present.
		{"wsl2 tar+gz", "wsl2", []DiskArtifactOverride{{Type: "tar", Compression: "gz"}}, ""},
		{"wsl2 tar+gzip", "wsl2", []DiskArtifactOverride{{Type: "tar", Compression: "gzip"}}, ""},
		{"wsl2 none", "wsl2", nil, "requires one tar artifact"},
		{"wsl2 raw", "wsl2", []DiskArtifactOverride{{Type: "raw", Compression: "gz"}}, "type \"raw\""},
		{"wsl2 no compression", "wsl2", []DiskArtifactOverride{{Type: "tar"}}, "compression is required"},
		// iso/img never call ConvertImageFile, so the list is ignored rather
		// than rejected — failing a request over a field with no effect would
		// be worse than carrying it.
		{"iso ignores anything", "iso", []DiskArtifactOverride{{Type: "tar", Compression: "bz2"}}, ""},
		{"img ignores anything", "img", []DiskArtifactOverride{{Type: "squashfs"}}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := rawDisk()
			d.Artifacts = c.arts
			assertErrContains(t, ValidateDisk(d, c.imageType), c.want)
		})
	}
}

func TestValidateDiskTooManyArtifacts(t *testing.T) {
	d := rawDisk()
	d.Artifacts = make([]DiskArtifactOverride, maxDiskArtifacts+1)
	if err := ValidateDisk(d, "raw"); err == nil ||
		!strings.Contains(err.Error(), "too many output artifacts") {
		t.Errorf("ValidateDisk = %v, want a too-many-artifacts error", err)
	}
}

// Mirrors validateAutoExpandLastPartitionConstraints in
// internal/config/validate/validate.go — rules the JSON schema does not carry.
func TestValidateDiskAutoExpand(t *testing.T) {
	cases := []struct {
		name      string
		imageType string
		lastMount string
		want      string
	}{
		{"iso rejected", "iso", "/", "not supported for imageType=iso"},
		{"raw needs rootfs last", "raw", "/data", "rootfs ('/')"},
		{"raw rootfs last ok", "raw", "/", ""},
		{"other types untouched", "img", "/data", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := rawDisk()
			d.ExtendLastPartitionToFillDisk = true
			d.Partitions[len(d.Partitions)-1].MountPoint = c.lastMount
			assertErrContains(t, ValidateDisk(d, c.imageType), c.want)
		})
	}

	t.Run("no partitions", func(t *testing.T) {
		d := rawDisk()
		d.ExtendLastPartitionToFillDisk = true
		d.Partitions = nil
		assertErrContains(t, ValidateDisk(d, "raw"), "at least one partition")
	})

	t.Run("flag off imposes nothing", func(t *testing.T) {
		d := rawDisk()
		d.Partitions[len(d.Partitions)-1].MountPoint = "/data"
		assertErrContains(t, ValidateDisk(d, "raw"), "")
	})
}

// The delta's disk block is what the build actually merges, so its shape matters
// as much as its validity.
func TestBuildDeltaEmitsDiskVerbatim(t *testing.T) {
	parentImage := config.ImageInfo{Name: "minimal-desktop-ubuntu-ptl-pv", Version: "1.0.0"}
	parentTarget := config.TargetInfo{OS: "ubuntu", Dist: "ubuntu24", Arch: "x86_64", ImageType: "raw"}

	data, err := buildDelta("parent.yml", parentImage, parentTarget,
		Selection{Disk: rawDisk()}, nil)
	if err != nil {
		t.Fatalf("buildDelta: %v", err)
	}

	var got struct {
		Disk *DiskOverride `yaml:"disk"`
	}
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshaling delta: %v", err)
	}
	if got.Disk == nil {
		t.Fatal("delta has no disk block")
	}
	if got.Disk.Name != "minimal-desktop-ubuntu-ptl-pv" || len(got.Disk.Partitions) != 3 {
		t.Errorf("disk = %+v, want the override's own name and 3 partitions", got.Disk)
	}
	if got.Disk.Partitions[2].End != "0" {
		t.Errorf("last partition end = %q, want \"0\"", got.Disk.Partitions[2].End)
	}
	// omitempty throughout: an unset field must not appear as a zero value, or
	// the delta would assert things the user never chose.
	for _, unwanted := range []string{"path:", "maxSize:", "extendLastPartitionToFillDisk:", "index:", "fsLabel:"} {
		if strings.Contains(string(data), unwanted) {
			t.Errorf("delta contains %q for an unset field:\n%s", unwanted, data)
		}
	}
}

func TestBuildDeltaWithoutDiskOmitsTheBlock(t *testing.T) {
	data, err := buildDelta("parent.yml",
		config.ImageInfo{Name: "img", Version: "1.0.0"},
		config.TargetInfo{OS: "ubuntu", Dist: "ubuntu24", Arch: "x86_64", ImageType: "raw"},
		Selection{ImageName: "renamed"}, nil)
	if err != nil {
		t.Fatalf("buildDelta: %v", err)
	}
	if strings.Contains(string(data), "disk:") {
		t.Errorf("delta declares a disk block without an override:\n%s", data)
	}
}

func TestSelectionHasOverridesCountsDisk(t *testing.T) {
	if (Selection{}).hasOverrides() {
		t.Error("empty selection reports overrides")
	}
	if !(Selection{Disk: rawDisk()}).hasOverrides() {
		t.Error("a disk override is not counted as an override, so no delta would be generated")
	}
}

func assertErrContains(t *testing.T, err error, want string) {
	t.Helper()
	if want == "" {
		if err != nil {
			t.Errorf("got error %v, want nil", err)
		}
		return
	}
	if err == nil {
		t.Errorf("got nil, want an error containing %q", want)
		return
	}
	if !strings.Contains(err.Error(), want) {
		t.Errorf("got error %q, want it to contain %q", err.Error(), want)
	}
}
