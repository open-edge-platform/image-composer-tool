// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package manualpartitionwidget

import (
	"testing"

	"github.com/rivo/tview"

	"github.com/open-edge-platform/image-composer-tool/cmd/live-installer/texture-ui/uitext"
	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/image/imagedisc"
)

func TestPopulateTable_IncludesLabelColumn(t *testing.T) {
	mp := &ManualPartitionWidget{
		bootType: imagedisc.EFIPartitionType,
		systemDevices: []imagedisc.SystemBlockDevice{{
			DevicePath:  "/dev/sda",
			RawDiskSize: 8 * 1024 * 1024 * 1024,
		}},
		partitionTable: tview.NewTable(),
		spaceLeftText:  tview.NewTextView(),
	}

	if err := mp.populateTable(); err != nil {
		t.Fatalf("populateTable failed: %v", err)
	}

	cell := mp.partitionTable.GetCell(tableHeaderRow, labelColumn)
	if cell == nil {
		t.Fatal("expected label header cell to be present")
	}
	if cell.Text != uitext.DiskLabelLabel {
		t.Fatalf("expected label header %q, got %q", uitext.DiskLabelLabel, cell.Text)
	}
}

func TestUnmarshalPartitionTable_SetsFsLabel(t *testing.T) {
	mp := &ManualPartitionWidget{
		bootType: imagedisc.EFIPartitionType,
		systemDevices: []imagedisc.SystemBlockDevice{{
			DevicePath:  "/dev/sda",
			RawDiskSize: 8 * 1024 * 1024 * 1024,
		}},
		partitionTable: tview.NewTable(),
		spaceLeftText:  tview.NewTextView(),
		template:       &config.ImageTemplate{},
	}

	if err := mp.populateTable(); err != nil {
		t.Fatalf("populateTable failed: %v", err)
	}

	if err := mp.addPartitionToTable("rootfs", "root-label", "1024MiB", "ext4", "/"); err != nil {
		t.Fatalf("addPartitionToTable failed: %v", err)
	}

	if err := mp.unmarshalPartitionTable(); err != nil {
		t.Fatalf("unmarshalPartitionTable failed: %v", err)
	}

	if len(mp.template.Disk.Partitions) != 2 {
		t.Fatalf("expected 2 partitions, got %d", len(mp.template.Disk.Partitions))
	}

	if got := mp.template.Disk.Partitions[1].FsLabel; got != "root-label" {
		t.Fatalf("expected root FsLabel to be %q, got %q", "root-label", got)
	}
}
