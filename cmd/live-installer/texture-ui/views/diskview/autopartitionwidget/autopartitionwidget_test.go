// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package autopartitionwidget

import (
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/image/imagedisc"
	"github.com/rivo/tview"
)

func TestAutoPartitionWidget_MustUpdateConfiguration_UsesTemplatePartitionsIfAvailable(t *testing.T) {
	// Test that if template has partitions defined, they are preserved
	existingPartition := config.PartitionInfo{
		ID:         "existing",
		Type:       "linux-root-amd64",
		MountPoint: "/",
		FsType:     "ext4",
	}

	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			Arch: "x86_64",
		},
		Disk: config.DiskConfig{
			Path:               "/dev/old",
			PartitionTableType: "gpt",
			Partitions:         []config.PartitionInfo{existingPartition},
		},
	}

	systemDevices := []imagedisc.SystemBlockDevice{{
		DevicePath:  "/dev/sda",
		RawDiskSize: 100 * 1024 * 1024 * 1024,
		Model:       "Mock Disk",
	}}

	ap := New(systemDevices, "efi")
	app := tview.NewApplication()
	mockFunc := func() {}

	err := ap.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	ap.mustUpdateConfiguration(template)

	if template.Disk.Path != "/dev/sda" {
		t.Fatalf("expected disk path to be /dev/sda, got %q", template.Disk.Path)
	}

	if template.Disk.PartitionTableType != "gpt" {
		t.Fatalf("expected partition table type to be preserved as gpt, got %q", template.Disk.PartitionTableType)
	}

	// Should preserve the existing partition from template
	if len(template.Disk.Partitions) != 1 {
		t.Fatalf("expected 1 partition (from template), got %d", len(template.Disk.Partitions))
	}

	if template.Disk.Partitions[0].ID != "existing" {
		t.Fatalf("expected partition ID to be 'existing', got %q", template.Disk.Partitions[0].ID)
	}
}

func TestAutoPartitionWidget_MustUpdateConfiguration_CreatesDefaultsWhenTemplateHasNoPartitions(t *testing.T) {
	// Test that if template has no partitions, defaults are created
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			Arch: "x86_64",
		},
		Disk: config.DiskConfig{
			Path:               "/dev/old",
			PartitionTableType: "mbr",
			Partitions:         nil, // No partitions
		},
	}

	systemDevices := []imagedisc.SystemBlockDevice{{
		DevicePath:  "/dev/sda",
		RawDiskSize: 100 * 1024 * 1024 * 1024,
		Model:       "Mock Disk",
	}}

	ap := New(systemDevices, "efi")
	app := tview.NewApplication()
	mockFunc := func() {}

	err := ap.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	ap.mustUpdateConfiguration(template)

	if template.Disk.Path != "/dev/sda" {
		t.Fatalf("expected disk path to be /dev/sda, got %q", template.Disk.Path)
	}

	// Should default to GPT when creating partitions
	if template.Disk.PartitionTableType != "gpt" {
		t.Fatalf("expected partition table type to default to gpt, got %q", template.Disk.PartitionTableType)
	}

	// Should have 2 default partitions: boot and root
	if len(template.Disk.Partitions) != 2 {
		t.Fatalf("expected 2 default partitions (boot + root), got %d", len(template.Disk.Partitions))
	}

	// Verify boot partition
	bootPartition := template.Disk.Partitions[0]
	if bootPartition.ID != "boot" {
		t.Fatalf("expected first partition ID to be 'boot', got %q", bootPartition.ID)
	}

	// Verify root partition
	rootPartition := template.Disk.Partitions[1]
	if rootPartition.ID != "rootfs" {
		t.Fatalf("expected second partition ID to be 'rootfs', got %q", rootPartition.ID)
	}
}

func TestAutoPartitionWidget_MustUpdateConfiguration_CreatesDefaultAutoPartitions(t *testing.T) {
	// This test now verifies that existing partitions are preserved
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			Arch: "x86_64",
		},
		Disk: config.DiskConfig{
			Path:               "/dev/old",
			PartitionTableType: "mbr",
			Partitions: []config.PartitionInfo{{
				Name: "root",
				ID:   "root",
			}},
		},
	}

	systemDevices := []imagedisc.SystemBlockDevice{{
		DevicePath:  "/dev/sda",
		RawDiskSize: 100 * 1024 * 1024 * 1024,
		Model:       "Mock Disk",
	}}

	ap := New(systemDevices, "efi")
	app := tview.NewApplication()
	mockFunc := func() {}

	err := ap.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	ap.mustUpdateConfiguration(template)

	if template.Disk.Path != "/dev/sda" {
		t.Fatalf("expected disk path to be /dev/sda, got %q", template.Disk.Path)
	}

	// Should preserve the original partition table type from template
	if template.Disk.PartitionTableType != "mbr" {
		t.Fatalf("expected partition table type to be preserved as mbr, got %q", template.Disk.PartitionTableType)
	}

	// Should preserve the existing partition from template (not replace it with defaults)
	if len(template.Disk.Partitions) != 1 {
		t.Fatalf("expected 1 partition (existing from template), got %d", len(template.Disk.Partitions))
	}

	if template.Disk.Partitions[0].ID != "root" {
		t.Fatalf("expected partition ID to be 'root', got %q", template.Disk.Partitions[0].ID)
	}
}

func TestAutoPartitionWidget_MustUpdateConfiguration_RootPartitionTypeForAarch64(t *testing.T) {
	// Test that defaults are created with correct arch for aarch64 when template has no partitions
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			Arch: "aarch64",
		},
		Disk: config.DiskConfig{
			Path:               "/dev/old",
			PartitionTableType: "",
			Partitions:         nil, // No partitions
		},
	}

	systemDevices := []imagedisc.SystemBlockDevice{{
		DevicePath:  "/dev/sda",
		RawDiskSize: 100 * 1024 * 1024 * 1024,
		Model:       "Mock Disk",
	}}

	ap := New(systemDevices, "efi")
	app := tview.NewApplication()
	mockFunc := func() {}

	err := ap.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	ap.mustUpdateConfiguration(template)

	rootPartition := template.Disk.Partitions[1]
	if rootPartition.Type != "linux-root-aarch64" {
		t.Fatalf("expected root partition type to be linux-root-aarch64 for aarch64, got %q", rootPartition.Type)
	}
}
