package imageinspect

import (
	"strings"
	"testing"
)

func TestCompareImages_Equal_NoChanges(t *testing.T) {
	a := &ImageSummary{
		File:      "a.raw",
		SizeBytes: 100,
		PartitionTable: PartitionTableSummary{
			Type:               "gpt",
			LogicalSectorSize:  512,
			PhysicalSectorSize: 4096,
			ProtectiveMBR:      true,
			Partitions: []PartitionSummary{
				{
					Index:     1,
					Name:      "ESP",
					Type:      "efi",
					StartLBA:  2048,
					EndLBA:    4095,
					SizeBytes: 1024,
					Flags:     "",
					Filesystem: &FilesystemSummary{
						Type:  "vfat",
						Label: "EFI",
						UUID:  "UUID-ESP",
					},
				},
			},
		},
	}

	// Deep copy-ish by constructing again (enough for this test)
	b := &ImageSummary{
		File:           "a.raw",
		SizeBytes:      100,
		PartitionTable: a.PartitionTable,
	}

	res := CompareImages(a, b)

	// We expect EqualityUnverified because we are not hashing images
	if res.Equality.Class != EqualityUnverified {
		t.Fatalf("expected Equality.Class=EqualitySemantic, got %v", res.Equality.Class)
	}
	if res.Summary.Changed {
		t.Fatalf("expected Summary.Changed=false, got true")
	}
	if res.Diff.PartitionTable.Changed {
		t.Fatalf("expected no partition table change")
	}
	if len(res.Diff.Partitions.Added) != 0 || len(res.Diff.Partitions.Removed) != 0 || len(res.Diff.Partitions.Modified) != 0 {
		t.Fatalf("expected no partition changes")
	}
	if len(res.Diff.EFIBinaries.Added) != 0 || len(res.Diff.EFIBinaries.Removed) != 0 || len(res.Diff.EFIBinaries.Modified) != 0 {
		t.Fatalf("expected no efi changes")
	}
}

func TestCompareImages_PartitionTableChanged(t *testing.T) {
	a := &ImageSummary{
		File:      "a.raw",
		SizeBytes: 100,
		PartitionTable: PartitionTableSummary{
			Type:               "gpt",
			LogicalSectorSize:  512,
			PhysicalSectorSize: 4096,
			ProtectiveMBR:      true,
		},
	}
	b := &ImageSummary{
		File:      "b.raw",
		SizeBytes: 100,
		PartitionTable: PartitionTableSummary{
			Type:               "mbr",
			LogicalSectorSize:  512,
			PhysicalSectorSize: 512,
			ProtectiveMBR:      false,
		},
	}

	res := CompareImages(a, b)
	if res.Equality.Class != EqualityDifferent {
		t.Fatalf("expected Equality.Class=EqualityDifferent, got %v", res.Equality.Class)
	}
	if !res.Diff.PartitionTable.Changed {
		t.Fatalf("expected partition table changed")
	}
	if res.Diff.PartitionTable.Type == nil || res.Diff.PartitionTable.Type.From != "gpt" || res.Diff.PartitionTable.Type.To != "mbr" {
		t.Fatalf("expected type diff gpt->mbr, got %+v", res.Diff.PartitionTable.Type)
	}
	if res.Diff.PartitionTable.PhysicalSectorSize == nil {
		t.Fatalf("expected physical sector size diff")
	}
	if res.Diff.PartitionTable.ProtectiveMBR == nil {
		t.Fatalf("expected protective MBR diff")
	}
}

func TestCompareImages_PartitionTable_GuidAndFreeSpanAndMisaligned(t *testing.T) {
	a := &ImageSummary{
		File: "a.raw",
		PartitionTable: PartitionTableSummary{
			Type:                 "gpt",
			DiskGUID:             "AAA",
			LogicalSectorSize:    512,
			PhysicalSectorSize:   4096,
			ProtectiveMBR:        true,
			LargestFreeSpan:      &FreeSpanSummary{StartLBA: 100, EndLBA: 199, SizeBytes: 100 * 512},
			MisalignedPartitions: []int{2},
		},
	}
	b := &ImageSummary{
		File: "b.raw",
		PartitionTable: PartitionTableSummary{
			Type:                 "gpt",
			DiskGUID:             "BBB",
			LogicalSectorSize:    512,
			PhysicalSectorSize:   4096,
			ProtectiveMBR:        true,
			LargestFreeSpan:      &FreeSpanSummary{StartLBA: 50, EndLBA: 149, SizeBytes: 100 * 512},
			MisalignedPartitions: []int{1, 3},
		},
	}

	res := CompareImages(a, b)
	if res.Diff.PartitionTable.DiskGUID == nil || res.Diff.PartitionTable.DiskGUID.From != "AAA" || res.Diff.PartitionTable.DiskGUID.To != "BBB" {
		t.Fatalf("expected disk guid diff, got %+v", res.Diff.PartitionTable.DiskGUID)
	}
	if res.Diff.PartitionTable.LargestFreeSpan == nil || res.Diff.PartitionTable.LargestFreeSpan.From.StartLBA != 100 || res.Diff.PartitionTable.LargestFreeSpan.To.StartLBA != 50 {
		t.Fatalf("expected largest free span diff, got %+v", res.Diff.PartitionTable.LargestFreeSpan)
	}
	if res.Diff.PartitionTable.MisalignedParts == nil || len(res.Diff.PartitionTable.MisalignedParts.To) != 2 {
		t.Fatalf("expected misaligned partitions diff, got %+v", res.Diff.PartitionTable.MisalignedParts)
	}
}

func TestCompareImages_PartitionsAddedRemovedModified_ByFSUUIDKey(t *testing.T) {
	// A has ESP + root
	a := &ImageSummary{
		File:      "a.raw",
		SizeBytes: 100,
		PartitionTable: PartitionTableSummary{
			Type:              "gpt",
			LogicalSectorSize: 512,
			Partitions: []PartitionSummary{
				{
					Index:     1,
					Name:      "ESP",
					Type:      "efi",
					StartLBA:  2048,
					EndLBA:    4095,
					SizeBytes: 1024,
					Filesystem: &FilesystemSummary{
						Type:  "vfat",
						UUID:  "UUID-ESP",
						Label: "EFI",
					},
				},
				{
					Index:     2,
					Name:      "root",
					Type:      "linux",
					StartLBA:  4096,
					EndLBA:    8191,
					SizeBytes: 2048,
					Filesystem: &FilesystemSummary{
						Type:  "ext4",
						UUID:  "UUID-ROOT",
						Label: "rootfs",
					},
				},
			},
		},
	}

	// B removes root, modifies ESP label, adds data
	b := &ImageSummary{
		File:      "b.raw",
		SizeBytes: 100,
		PartitionTable: PartitionTableSummary{
			Type:              "gpt",
			LogicalSectorSize: 512,
			Partitions: []PartitionSummary{
				{
					Index:     1,
					Name:      "ESP",
					Type:      "efi",
					StartLBA:  2048,
					EndLBA:    4095,
					SizeBytes: 1024,
					Filesystem: &FilesystemSummary{
						Type:  "vfat",
						UUID:  "UUID-ESP",
						Label: "EFI-NEW",
					},
				},
				{
					Index:     3,
					Name:      "data",
					Type:      "linux",
					StartLBA:  9000,
					EndLBA:    9999,
					SizeBytes: 4096,
					Filesystem: &FilesystemSummary{
						Type:  "ext4",
						UUID:  "UUID-DATA",
						Label: "data",
					},
				},
			},
		},
	}

	res := CompareImages(a, b)

	if res.Equality.Class != EqualityDifferent {
		t.Fatalf("expected Equality.Class=EqualityDifferent, got %v", res.Equality.Class)
	}
	// Added: data
	if len(res.Diff.Partitions.Added) != 1 {
		t.Fatalf("expected 1 added partition, got %d", len(res.Diff.Partitions.Added))
	}
	// Removed: root
	if len(res.Diff.Partitions.Removed) != 1 {
		t.Fatalf("expected 1 removed partition, got %d", len(res.Diff.Partitions.Removed))
	}
	// Modified: ESP
	if len(res.Diff.Partitions.Modified) != 1 {
		t.Fatalf("expected 1 modified partition, got %d", len(res.Diff.Partitions.Modified))
	}
	if res.Diff.Partitions.Modified[0].Filesystem == nil || res.Diff.Partitions.Modified[0].Filesystem.Modified == nil {
		t.Fatalf("expected filesystem modified diff for ESP")
	}
	if res.Diff.Partitions.Modified[0].Filesystem.Modified.From.Label != "EFI" ||
		res.Diff.Partitions.Modified[0].Filesystem.Modified.To.Label != "EFI-NEW" {
		t.Fatalf("expected FS label change EFI->EFI-NEW, got %+v", res.Diff.Partitions.Modified[0].Filesystem.Modified)
	}
}

func TestCompareImages_EFIBinaries_ModifiedAndUKIDiff(t *testing.T) {
	a := &ImageSummary{
		File:      "a.raw",
		SizeBytes: 100,
		PartitionTable: PartitionTableSummary{
			Type:              "gpt",
			LogicalSectorSize: 512,
			Partitions: []PartitionSummary{
				{
					Index:     1,
					Name:      "ESP",
					Type:      "efi",
					StartLBA:  2048,
					EndLBA:    4095,
					SizeBytes: 1024,
					Filesystem: &FilesystemSummary{
						Type:  "vfat",
						UUID:  "UUID-ESP",
						Label: "EFI",
						EFIBinaries: []EFIBinaryEvidence{
							{
								Path:         "EFI/BOOT/BOOTX64.EFI",
								SHA256:       "aaa",
								Kind:         BootloaderUnknown,
								Arch:         "x86_64",
								Signed:       false,
								IsUKI:        true,
								KernelSHA256: "k1",
								InitrdSHA256: "i1",
							},
							{
								Path:   "EFI/BOOT/grubx64.efi",
								SHA256: "bbb",
								Kind:   BootloaderGrub,
								Arch:   "x86_64",
							},
						},
					},
				},
			},
		},
	}

	// Build b as a deep-enough copy of the partition/fs we will mutate.
	// Copy top-level
	b := &ImageSummary{
		File:      "b.raw",
		SizeBytes: a.SizeBytes,
		PartitionTable: PartitionTableSummary{
			Type:               a.PartitionTable.Type,
			LogicalSectorSize:  a.PartitionTable.LogicalSectorSize,
			PhysicalSectorSize: a.PartitionTable.PhysicalSectorSize,
			ProtectiveMBR:      a.PartitionTable.ProtectiveMBR,
			Partitions:         make([]PartitionSummary, len(a.PartitionTable.Partitions)),
		},
	}

	// Copy the single partition
	b.PartitionTable.Partitions[0] = a.PartitionTable.Partitions[0]

	// Copy the filesystem struct (not pointer)
	afs := a.PartitionTable.Partitions[0].Filesystem
	if afs == nil {
		t.Fatalf("expected filesystem in test setup")
	}
	fsCopy := *afs

	// Replace EFIBinaries in b only
	fsCopy.EFIBinaries = []EFIBinaryEvidence{
		{
			Path:   "EFI/BOOT/grubx64.efi",
			SHA256: "bbb",
			Kind:   BootloaderGrub,
			Arch:   "x86_64",
		},
		{
			Path:         "EFI/BOOT/BOOTX64.EFI",
			SHA256:       "ccc",
			Kind:         BootloaderUKI,
			Arch:         "x86_64",
			Signed:       false,
			IsUKI:        true,
			KernelSHA256: "k2",
			InitrdSHA256: "i1",
		},
	}

	b.PartitionTable.Partitions[0].Filesystem = &fsCopy

	res := CompareImages(a, b)

	if res.Equality.Class != EqualityDifferent {
		t.Fatalf("expected Equality.Class=EqualityDifferent, got %v", res.Equality.Class)
	}

	efi := res.Diff.EFIBinaries
	if len(efi.Modified) != 1 {
		t.Fatalf("expected 1 modified efi binary, got %d", len(efi.Modified))
	}
	if efi.Modified[0].Key != "EFI/BOOT/BOOTX64.EFI" {
		t.Fatalf("expected modified key BOOTX64, got %s", efi.Modified[0].Key)
	}
	if efi.Modified[0].From.Kind != BootloaderUnknown || efi.Modified[0].To.Kind != BootloaderUKI {
		t.Fatalf("expected kind unknown->uki, got %s -> %s", efi.Modified[0].From.Kind, efi.Modified[0].To.Kind)
	}
	if efi.Modified[0].UKI == nil || !efi.Modified[0].UKI.Changed {
		t.Fatalf("expected UKI diff present and changed")
	}
	if efi.Modified[0].UKI.KernelSHA256 == nil || efi.Modified[0].UKI.KernelSHA256.From != "k1" || efi.Modified[0].UKI.KernelSHA256.To != "k2" {
		t.Fatalf("expected kernel hash k1->k2, got %+v", efi.Modified[0].UKI.KernelSHA256)
	}
}

func TestCompareImages_EFIBinaries_AddedRemoved(t *testing.T) {
	a := &ImageSummary{
		File:      "a.raw",
		SizeBytes: 100,
		PartitionTable: PartitionTableSummary{
			Type:              "gpt",
			LogicalSectorSize: 512,
			Partitions: []PartitionSummary{
				{
					Index: 1,
					Name:  "ESP",
					Type:  "efi",
					Filesystem: &FilesystemSummary{
						Type: "vfat",
						UUID: "UUID-ESP",
						EFIBinaries: []EFIBinaryEvidence{
							{Path: "EFI/BOOT/grubx64.efi", SHA256: "bbb", Kind: BootloaderGrub},
						},
					},
				},
			},
		},
	}
	b := &ImageSummary{
		File:      "b.raw",
		SizeBytes: 100,
		PartitionTable: PartitionTableSummary{
			Type:              "gpt",
			LogicalSectorSize: 512,
			Partitions: []PartitionSummary{
				{
					Index: 1,
					Name:  "ESP",
					Type:  "efi",
					Filesystem: &FilesystemSummary{
						Type: "vfat",
						UUID: "UUID-ESP",
						EFIBinaries: []EFIBinaryEvidence{
							{Path: "EFI/BOOT/systemd-bootx64.efi", SHA256: "sss", Kind: BootloaderSystemdBoot},
						},
					},
				},
			},
		},
	}

	res := CompareImages(a, b)

	if len(res.Diff.EFIBinaries.Added) != 1 {
		t.Fatalf("expected 1 added efi, got %d", len(res.Diff.EFIBinaries.Added))
	}
	if len(res.Diff.EFIBinaries.Removed) != 1 {
		t.Fatalf("expected 1 removed efi, got %d", len(res.Diff.EFIBinaries.Removed))
	}
	if res.Diff.EFIBinaries.Added[0].Path != "EFI/BOOT/systemd-bootx64.efi" {
		t.Fatalf("unexpected added path: %s", res.Diff.EFIBinaries.Added[0].Path)
	}
	if res.Diff.EFIBinaries.Removed[0].Path != "EFI/BOOT/grubx64.efi" {
		t.Fatalf("unexpected removed path: %s", res.Diff.EFIBinaries.Removed[0].Path)
	}
}

func TestDiffStringMap_NilSafeAndDiffs(t *testing.T) {
	// Nil inputs should yield nil fields for omitempty friendliness.
	empty := diffStringMap(nil, nil)
	if empty.Added != nil || empty.Removed != nil || empty.Modified != nil {
		t.Fatalf("expected all nil fields for nil inputs, got %+v", empty)
	}

	from := map[string]string{"a": "1", "b": "1"}
	to := map[string]string{"b": "2", "c": "3"}

	d := diffStringMap(from, to)
	if len(d.Added) != 1 || d.Added["c"] != "3" {
		t.Fatalf("expected added c=3, got %+v", d.Added)
	}
	if len(d.Removed) != 1 || d.Removed["a"] != "1" {
		t.Fatalf("expected removed a=1, got %+v", d.Removed)
	}
	if len(d.Modified) != 1 || d.Modified["b"].From != "1" || d.Modified["b"].To != "2" {
		t.Fatalf("expected modified b 1->2, got %+v", d.Modified)
	}
}

func TestFlattenEFIBinaries_SortedAndCopied(t *testing.T) {
	pt := PartitionTableSummary{
		Partitions: []PartitionSummary{
			{
				Name: "p1",
				Filesystem: &FilesystemSummary{EFIBinaries: []EFIBinaryEvidence{
					{Path: "EFI/BOOT/b.efi", SHA256: "2"},
					{Path: "EFI/BOOT/a.efi", SHA256: "1"},
				}},
			},
			{
				Name: "p2",
				Filesystem: &FilesystemSummary{EFIBinaries: []EFIBinaryEvidence{
					{Path: "EFI/BOOT/c.efi", SHA256: "3"},
				}},
			},
		},
	}

	out := flattenEFIBinaries(pt)
	if len(out) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(out))
	}
	if out[0].Path != "EFI/BOOT/a.efi" || out[1].Path != "EFI/BOOT/b.efi" || out[2].Path != "EFI/BOOT/c.efi" {
		t.Fatalf("expected sorted paths, got %+v", out)
	}

	// Mutating the flattened slice must not affect the source summaries.
	out[0].Path = "EFI/BOOT/z.efi"
	if pt.Partitions[0].Filesystem.EFIBinaries[0].Path != "EFI/BOOT/b.efi" {
		t.Fatalf("expected source slice untouched, got %s", pt.Partitions[0].Filesystem.EFIBinaries[0].Path)
	}
}

func TestCompareEFIBinaries_SectionDiffsProduceUKIDiff(t *testing.T) {
	from := []EFIBinaryEvidence{{
		Path:          "EFI/BOOT/BOOTX64.EFI",
		IsUKI:         true,
		SectionSHA256: map[string]string{"linux": "a", "osrel": "o"},
	}}
	to := []EFIBinaryEvidence{{
		Path:          "EFI/BOOT/BOOTX64.EFI",
		IsUKI:         true,
		SectionSHA256: map[string]string{"linux": "b", "cmdline": "c"},
	}}

	d := compareEFIBinaries(from, to)
	if len(d.Modified) != 1 {
		t.Fatalf("expected 1 modified entry, got %d", len(d.Modified))
	}
	uki := d.Modified[0].UKI
	if uki == nil || !uki.Changed {
		t.Fatalf("expected UKI diff with changed=true, got %+v", uki)
	}
	if uki.SectionSHA256.Added["cmdline"] != "c" {
		t.Fatalf("expected added cmdline=c, got %+v", uki.SectionSHA256.Added)
	}
	if uki.SectionSHA256.Removed["osrel"] != "o" {
		t.Fatalf("expected removed osrel=o, got %+v", uki.SectionSHA256.Removed)
	}
	if diff := uki.SectionSHA256.Modified["linux"]; diff.From != "a" || diff.To != "b" {
		t.Fatalf("expected linux hash a->b, got %+v", diff)
	}
}

func TestCompareImages_EmptyPartitionLists(t *testing.T) {
	a := &ImageSummary{
		File:      "a.raw",
		SizeBytes: 100,
		PartitionTable: PartitionTableSummary{
			Type:              "gpt",
			LogicalSectorSize: 512,
			Partitions:        []PartitionSummary{}, // Empty
		},
	}
	b := &ImageSummary{
		File:      "b.raw",
		SizeBytes: 100,
		PartitionTable: PartitionTableSummary{
			Type:              "gpt",
			LogicalSectorSize: 512,
			Partitions:        []PartitionSummary{}, // Empty
		},
	}

	res := CompareImages(a, b)
	if res.Summary.Changed {
		t.Fatalf("expected no changes for empty partition lists, got %+v", res.Summary)
	}
	if len(res.Diff.Partitions.Added) != 0 || len(res.Diff.Partitions.Removed) != 0 {
		t.Fatalf("expected no partition diffs, got added=%d, removed=%d", len(res.Diff.Partitions.Added), len(res.Diff.Partitions.Removed))
	}
}

func TestCompareImages_VeryLargeSizeChange(t *testing.T) {
	a := &ImageSummary{
		File:      "a.raw",
		SizeBytes: 1, // 1 byte
		PartitionTable: PartitionTableSummary{
			Type:              "gpt",
			LogicalSectorSize: 512,
		},
	}
	b := &ImageSummary{
		File:      "b.raw",
		SizeBytes: 1099511627776, // 1 TB
		PartitionTable: PartitionTableSummary{
			Type:              "gpt",
			LogicalSectorSize: 512,
		},
	}

	res := CompareImages(a, b)
	if res.Diff.Image.SizeBytes == nil {
		t.Fatalf("expected size change detected")
	}
	if res.Diff.Image.SizeBytes.From != 1 || res.Diff.Image.SizeBytes.To != 1099511627776 {
		t.Fatalf("expected size 1->1TB, got %+v", res.Diff.Image.SizeBytes)
	}
}

func TestCompareImages_PartitionWithoutFilesystem_vs_WithFilesystem(t *testing.T) {
	a := &ImageSummary{
		File:      "a.raw",
		SizeBytes: 100,
		PartitionTable: PartitionTableSummary{
			Type:              "gpt",
			LogicalSectorSize: 512,
			Partitions: []PartitionSummary{
				{
					Index:      1,
					Name:       "data",
					Type:       "linux",
					Filesystem: nil, // No filesystem
				},
			},
		},
	}
	b := &ImageSummary{
		File:      "b.raw",
		SizeBytes: 100,
		PartitionTable: PartitionTableSummary{
			Type:              "gpt",
			LogicalSectorSize: 512,
			Partitions: []PartitionSummary{
				{
					Index: 1,
					Name:  "data",
					Type:  "linux",
					Filesystem: &FilesystemSummary{
						Type:  "ext4",
						UUID:  "UUID-DATA",
						Label: "data",
					},
				},
			},
		},
	}

	res := CompareImages(a, b)
	if res.Equality.Class != EqualityDifferent {
		t.Fatalf("expected change when filesystem added, got %v", res.Equality.Class)
	}
	if len(res.Diff.Partitions.Modified) != 1 {
		t.Fatalf("expected partition marked as modified, got %d", len(res.Diff.Partitions.Modified))
	}
	if res.Diff.Partitions.Modified[0].Filesystem == nil {
		t.Fatalf("expected filesystem diff info")
	}
}

func TestCompareImages_EmptyEFIBinariesList(t *testing.T) {
	a := &ImageSummary{
		File:      "a.raw",
		SizeBytes: 100,
		PartitionTable: PartitionTableSummary{
			Type:              "gpt",
			LogicalSectorSize: 512,
			Partitions: []PartitionSummary{
				{
					Index: 1,
					Name:  "ESP",
					Type:  "efi",
					Filesystem: &FilesystemSummary{
						Type:        "vfat",
						UUID:        "UUID-ESP",
						EFIBinaries: []EFIBinaryEvidence{}, // Empty
					},
				},
			},
		},
	}
	b := &ImageSummary{
		File:      "b.raw",
		SizeBytes: 100,
		PartitionTable: PartitionTableSummary{
			Type:              "gpt",
			LogicalSectorSize: 512,
			Partitions: []PartitionSummary{
				{
					Index: 1,
					Name:  "ESP",
					Type:  "efi",
					Filesystem: &FilesystemSummary{
						Type:        "vfat",
						UUID:        "UUID-ESP",
						EFIBinaries: []EFIBinaryEvidence{}, // Empty
					},
				},
			},
		},
	}

	res := CompareImages(a, b)
	if res.Summary.Changed {
		t.Fatalf("expected no changes for empty EFI lists, got %+v", res.Summary)
	}
	if len(res.Diff.EFIBinaries.Added) != 0 || len(res.Diff.EFIBinaries.Removed) != 0 {
		t.Fatalf("expected no EFI diffs for empty lists")
	}
}

func TestCompareImages_EFIBinarySamePathDifferentHash(t *testing.T) {
	a := &ImageSummary{
		File:      "a.raw",
		SizeBytes: 100,
		PartitionTable: PartitionTableSummary{
			Type:              "gpt",
			LogicalSectorSize: 512,
			Partitions: []PartitionSummary{
				{
					Index: 1,
					Name:  "ESP",
					Type:  "efi",
					Filesystem: &FilesystemSummary{
						Type: "vfat",
						UUID: "UUID-ESP",
						EFIBinaries: []EFIBinaryEvidence{
							{
								Path:   "EFI/BOOT/BOOTX64.EFI",
								SHA256: "hash_v1",
								Kind:   BootloaderGrub,
							},
						},
					},
				},
			},
		},
	}
	b := &ImageSummary{
		File:      "b.raw",
		SizeBytes: 100,
		PartitionTable: PartitionTableSummary{
			Type:              "gpt",
			LogicalSectorSize: 512,
			Partitions: []PartitionSummary{
				{
					Index: 1,
					Name:  "ESP",
					Type:  "efi",
					Filesystem: &FilesystemSummary{
						Type: "vfat",
						UUID: "UUID-ESP",
						EFIBinaries: []EFIBinaryEvidence{
							{
								Path:   "EFI/BOOT/BOOTX64.EFI",
								SHA256: "hash_v2",
								Kind:   BootloaderGrub,
							},
						},
					},
				},
			},
		},
	}

	res := CompareImages(a, b)
	if res.Equality.Class != EqualityDifferent {
		t.Fatalf("expected change for different hash on same path, got %v", res.Equality.Class)
	}
	if len(res.Diff.EFIBinaries.Modified) != 1 {
		t.Fatalf("expected 1 modified EFI binary, got %d", len(res.Diff.EFIBinaries.Modified))
	}
	mod := res.Diff.EFIBinaries.Modified[0]
	if mod.From.SHA256 != "hash_v1" || mod.To.SHA256 != "hash_v2" {
		t.Fatalf("expected hash_v1->hash_v2, got %s->%s", mod.From.SHA256, mod.To.SHA256)
	}
}

func TestCompareImages_UKISectionHashChangesComplex(t *testing.T) {
	from := []EFIBinaryEvidence{{
		Path:  "EFI/BOOT/BOOTX64.EFI",
		IsUKI: true,
		SectionSHA256: map[string]string{
			"linux":   "old_linux",
			"initrd":  "old_initrd",
			"osrel":   "unchanged_osrel",
			"cmdline": "old_cmdline",
		},
	}}
	to := []EFIBinaryEvidence{{
		Path:  "EFI/BOOT/BOOTX64.EFI",
		IsUKI: true,
		SectionSHA256: map[string]string{
			"linux":  "new_linux",
			"initrd": "new_initrd",
			"osrel":  "unchanged_osrel",
			"splash": "new_splash",
		},
	}}

	d := compareEFIBinaries(from, to)
	if len(d.Modified) != 1 {
		t.Fatalf("expected 1 modified entry, got %d", len(d.Modified))
	}
	uki := d.Modified[0].UKI
	if uki == nil {
		t.Fatalf("expected UKI diff")
	}

	// Check added section
	if uki.SectionSHA256.Added["splash"] != "new_splash" {
		t.Fatalf("expected added splash section, got %+v", uki.SectionSHA256.Added)
	}

	// Check removed section
	if uki.SectionSHA256.Removed["cmdline"] != "old_cmdline" {
		t.Fatalf("expected removed cmdline section, got %+v", uki.SectionSHA256.Removed)
	}

	// Check modified sections
	if len(uki.SectionSHA256.Modified) != 2 {
		t.Fatalf("expected 2 modified sections, got %d", len(uki.SectionSHA256.Modified))
	}
	if uki.SectionSHA256.Modified["linux"].From != "old_linux" || uki.SectionSHA256.Modified["linux"].To != "new_linux" {
		t.Fatalf("expected linux hash diff, got %+v", uki.SectionSHA256.Modified["linux"])
	}
}

func TestCompareImages_BootloaderKindChanges(t *testing.T) {
	a := &ImageSummary{
		File:      "a.raw",
		SizeBytes: 100,
		PartitionTable: PartitionTableSummary{
			Type:              "gpt",
			LogicalSectorSize: 512,
			Partitions: []PartitionSummary{
				{
					Index: 1,
					Name:  "ESP",
					Type:  "efi",
					Filesystem: &FilesystemSummary{
						Type: "vfat",
						UUID: "UUID-ESP",
						EFIBinaries: []EFIBinaryEvidence{
							{
								Path: "EFI/BOOT/BOOTX64.EFI",
								Kind: BootloaderGrub,
							},
						},
					},
				},
			},
		},
	}
	b := &ImageSummary{
		File:      "b.raw",
		SizeBytes: 100,
		PartitionTable: PartitionTableSummary{
			Type:              "gpt",
			LogicalSectorSize: 512,
			Partitions: []PartitionSummary{
				{
					Index: 1,
					Name:  "ESP",
					Type:  "efi",
					Filesystem: &FilesystemSummary{
						Type: "vfat",
						UUID: "UUID-ESP",
						EFIBinaries: []EFIBinaryEvidence{
							{
								Path: "EFI/BOOT/BOOTX64.EFI",
								Kind: BootloaderSystemdBoot,
							},
						},
					},
				},
			},
		},
	}

	res := CompareImages(a, b)
	if res.Equality.Class != EqualityDifferent {
		t.Fatalf("expected change for bootloader kind change, got %v", res.Equality.Class)
	}
	if len(res.Diff.EFIBinaries.Modified) != 1 {
		t.Fatalf("expected 1 modified EFI binary, got %d", len(res.Diff.EFIBinaries.Modified))
	}
	if res.Diff.EFIBinaries.Modified[0].From.Kind != BootloaderGrub || res.Diff.EFIBinaries.Modified[0].To.Kind != BootloaderSystemdBoot {
		t.Fatalf("expected kind grub->systemdboot, got %s->%s", res.Diff.EFIBinaries.Modified[0].From.Kind, res.Diff.EFIBinaries.Modified[0].To.Kind)
	}
}

func TestCompareImages_EFIArchitectureChanges(t *testing.T) {
	a := &ImageSummary{
		File:      "a.raw",
		SizeBytes: 100,
		PartitionTable: PartitionTableSummary{
			Type:              "gpt",
			LogicalSectorSize: 512,
			Partitions: []PartitionSummary{
				{
					Index: 1,
					Name:  "ESP",
					Type:  "efi",
					Filesystem: &FilesystemSummary{
						Type: "vfat",
						UUID: "UUID-ESP",
						EFIBinaries: []EFIBinaryEvidence{
							{
								Path: "EFI/BOOT/BOOTX64.EFI",
								Arch: "x86_64",
								Kind: BootloaderGrub,
							},
						},
					},
				},
			},
		},
	}
	b := &ImageSummary{
		File:      "b.raw",
		SizeBytes: 100,
		PartitionTable: PartitionTableSummary{
			Type:              "gpt",
			LogicalSectorSize: 512,
			Partitions: []PartitionSummary{
				{
					Index: 1,
					Name:  "ESP",
					Type:  "efi",
					Filesystem: &FilesystemSummary{
						Type: "vfat",
						UUID: "UUID-ESP",
						EFIBinaries: []EFIBinaryEvidence{
							{
								Path: "EFI/BOOT/BOOTX64.EFI",
								Arch: "arm64",
								Kind: BootloaderGrub,
							},
						},
					},
				},
			},
		},
	}

	res := CompareImages(a, b)
	if res.Equality.Class != EqualityDifferent {
		t.Fatalf("expected change for arch change, got %v", res.Equality.Class)
	}
	if len(res.Diff.EFIBinaries.Modified) != 1 {
		t.Fatalf("expected 1 modified EFI binary, got %d", len(res.Diff.EFIBinaries.Modified))
	}
	if res.Diff.EFIBinaries.Modified[0].From.Arch == res.Diff.EFIBinaries.Modified[0].To.Arch {
		t.Fatalf("expected architecture diff")
	}
	if res.Diff.EFIBinaries.Modified[0].From.Arch != "x86_64" || res.Diff.EFIBinaries.Modified[0].To.Arch != "arm64" {
		t.Fatalf("expected arch x86_64->arm64, got %s->%s", res.Diff.EFIBinaries.Modified[0].From.Arch, res.Diff.EFIBinaries.Modified[0].To.Arch)
	}
}

func TestCompareImages_EFISigningStatusChanges(t *testing.T) {
	a := &ImageSummary{
		File:      "a.raw",
		SizeBytes: 100,
		PartitionTable: PartitionTableSummary{
			Type:              "gpt",
			LogicalSectorSize: 512,
			Partitions: []PartitionSummary{
				{
					Index: 1,
					Name:  "ESP",
					Type:  "efi",
					Filesystem: &FilesystemSummary{
						Type: "vfat",
						UUID: "UUID-ESP",
						EFIBinaries: []EFIBinaryEvidence{
							{
								Path:   "EFI/BOOT/BOOTX64.EFI",
								Signed: false,
								Kind:   BootloaderGrub,
							},
						},
					},
				},
			},
		},
	}
	b := &ImageSummary{
		File:      "b.raw",
		SizeBytes: 100,
		PartitionTable: PartitionTableSummary{
			Type:              "gpt",
			LogicalSectorSize: 512,
			Partitions: []PartitionSummary{
				{
					Index: 1,
					Name:  "ESP",
					Type:  "efi",
					Filesystem: &FilesystemSummary{
						Type: "vfat",
						UUID: "UUID-ESP",
						EFIBinaries: []EFIBinaryEvidence{
							{
								Path:   "EFI/BOOT/BOOTX64.EFI",
								Signed: true,
								Kind:   BootloaderGrub,
							},
						},
					},
				},
			},
		},
	}

	res := CompareImages(a, b)
	if res.Equality.Class != EqualityUnverified {
		t.Fatalf("expected change for signing status change, got %v", res.Equality.Class)
	}
	if len(res.Diff.EFIBinaries.Modified) != 1 {
		t.Fatalf("expected 1 modified EFI binary, got %d", len(res.Diff.EFIBinaries.Modified))
	}

	if res.Diff.EFIBinaries.Modified[0].From.Signed || !res.Diff.EFIBinaries.Modified[0].To.Signed {
		t.Fatalf("expected signed false->true, got %v->%v", res.Diff.EFIBinaries.Modified[0].From.Signed, res.Diff.EFIBinaries.Modified[0].To.Signed)
	}
}
func TestCompareUUIDReferences_Branches(t *testing.T) {
	from := []UUIDReference{
		{UUID: "00000000-0000-0000-0000-000000000001", Context: "kernel_cmdline", Mismatch: false}, // removed
		{UUID: "00000000-0000-0000-0000-000000000002", Context: "kernel_cmdline", Mismatch: false}, // mismatch changed
		{UUID: "00000000-0000-0000-0000-000000000003", Context: "grub_search", Mismatch: false},    // context changed only
		{UUID: "00000000-0000-0000-0000-000000000004", Context: "root_device", Mismatch: true},     // unchanged
	}
	to := []UUIDReference{
		{UUID: "00000000-0000-0000-0000-000000000002", Context: "root_device", Mismatch: true},
		{UUID: "00000000-0000-0000-0000-000000000003", Context: "kernel_cmdline", Mismatch: false},
		{UUID: "00000000-0000-0000-0000-000000000004", Context: "root_device", Mismatch: true},
		{UUID: "00000000-0000-0000-0000-000000000005", Context: "kernel_cmdline", Mismatch: false}, // added
	}

	changes := compareUUIDReferences(from, to)
	if len(changes) != 4 {
		t.Fatalf("expected 4 UUID reference changes, got %d: %+v", len(changes), changes)
	}

	byUUID := map[string]UUIDRefChange{}
	for _, ch := range changes {
		byUUID[ch.UUID] = ch
	}

	removed, ok := byUUID["00000000-0000-0000-0000-000000000001"]
	if !ok || removed.Status != "removed" || removed.ContextFrom != "kernel_cmdline" || removed.MismatchFrom {
		t.Fatalf("unexpected removed UUID change: %+v", removed)
	}

	added, ok := byUUID["00000000-0000-0000-0000-000000000005"]
	if !ok || added.Status != "added" || added.ContextTo != "kernel_cmdline" || added.MismatchTo {
		t.Fatalf("unexpected added UUID change: %+v", added)
	}

	mismatchChanged, ok := byUUID["00000000-0000-0000-0000-000000000002"]
	if !ok || mismatchChanged.Status != "modified" || mismatchChanged.MismatchFrom != false || mismatchChanged.MismatchTo != true {
		t.Fatalf("unexpected mismatch-changed UUID entry: %+v", mismatchChanged)
	}
	// When both mismatch and context change, the single entry must carry both pieces.
	if mismatchChanged.ContextFrom != "kernel_cmdline" || mismatchChanged.ContextTo != "root_device" {
		t.Fatalf("expected context change to be captured when mismatch also differs: %+v", mismatchChanged)
	}

	contextChanged, ok := byUUID["00000000-0000-0000-0000-000000000003"]
	if !ok || contextChanged.Status != "modified" || contextChanged.ContextFrom != "grub_search" || contextChanged.ContextTo != "kernel_cmdline" {
		t.Fatalf("unexpected context-changed UUID entry: %+v", contextChanged)
	}
}

func TestUkiOnlyVolatile_Branches(t *testing.T) {
	tests := []struct {
		name string
		mod  ModifiedEFIBinaryEvidence
		want bool
	}{
		{
			name: "kernel hash changed is meaningful",
			mod: ModifiedEFIBinaryEvidence{
				UKI: &UKIDiff{KernelSHA256: &ValueDiff[string]{From: "k1", To: "k2"}, Changed: true},
			},
			want: false,
		},
		{
			name: "uname hash changed is meaningful",
			mod: ModifiedEFIBinaryEvidence{
				UKI: &UKIDiff{UnameSHA256: &ValueDiff[string]{From: "u1", To: "u2"}, Changed: true},
			},
			want: false,
		},
		{
			name: "cmdline changed only by UUID is volatile",
			mod: ModifiedEFIBinaryEvidence{
				From: EFIBinaryEvidence{Cmdline: "root=UUID=11111111-1111-1111-1111-111111111111 ro quiet"},
				To:   EFIBinaryEvidence{Cmdline: "root=UUID=22222222-2222-2222-2222-222222222222 ro quiet"},
				UKI:  &UKIDiff{CmdlineSHA256: &ValueDiff[string]{From: "c1", To: "c2"}, Changed: true},
			},
			want: true,
		},
		{
			name: "cmdline semantic change is meaningful",
			mod: ModifiedEFIBinaryEvidence{
				From: EFIBinaryEvidence{Cmdline: "root=UUID=11111111-1111-1111-1111-111111111111 ro quiet"},
				To:   EFIBinaryEvidence{Cmdline: "root=UUID=22222222-2222-2222-2222-222222222222 rw quiet"},
				UKI:  &UKIDiff{CmdlineSHA256: &ValueDiff[string]{From: "c1", To: "c2"}, Changed: true},
			},
			want: false,
		},
		{
			name: "no uki details defaults volatile",
			mod:  ModifiedEFIBinaryEvidence{},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ukiOnlyVolatile(tc.mod)
			if got != tc.want {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}
func TestCompareVerity_NilCasesAndFieldChanges(t *testing.T) {
	if got := compareVerity(nil, nil); got != nil {
		t.Fatalf("expected nil diff when both verity values are nil")
	}

	added := compareVerity(nil, &VerityInfo{Enabled: true, Method: "systemd-verity", RootDevice: "/dev/vda2", HashPartition: 3})
	if added == nil || !added.Changed || added.Added == nil {
		t.Fatalf("expected added verity diff")
	}

	removed := compareVerity(&VerityInfo{Enabled: true, Method: "systemd-verity"}, nil)
	if removed == nil || !removed.Changed || removed.Removed == nil {
		t.Fatalf("expected removed verity diff")
	}

	from := &VerityInfo{
		Enabled:       true,
		Method:        "systemd-verity",
		RootDevice:    "/dev/vda2",
		HashPartition: 3,
	}
	to := &VerityInfo{
		Enabled:       false,
		Method:        "custom-initramfs",
		RootDevice:    "/dev/vda3",
		HashPartition: 4,
	}

	changed := compareVerity(from, to)
	if changed == nil || !changed.Changed {
		t.Fatalf("expected changed verity diff")
	}
	if changed.Enabled == nil || changed.Enabled.From != true || changed.Enabled.To != false {
		t.Fatalf("expected enabled diff to be set")
	}
	if changed.Method == nil || changed.Method.From != "systemd-verity" || changed.Method.To != "custom-initramfs" {
		t.Fatalf("expected method diff to be set")
	}
	if changed.RootDevice == nil || changed.RootDevice.From != "/dev/vda2" || changed.RootDevice.To != "/dev/vda3" {
		t.Fatalf("expected root device diff to be set")
	}
	if changed.HashPartition == nil || changed.HashPartition.From != 3 || changed.HashPartition.To != 4 {
		t.Fatalf("expected hash partition diff to be set")
	}
}

func TestTallyBootloaderConfigDiff_ClassificationCoverage(t *testing.T) {
	tally := &diffTally{}
	d := &BootloaderConfigDiff{
		ConfigFileChanges: []ConfigFileChange{
			{Path: "/boot/grub/grub.cfg", Status: "added"},
			{Path: "/loader/entries/old.conf", Status: "removed"},
			{Path: "/loader/entries/default.conf", Status: "modified"},
		},
		BootEntryChanges: []BootEntryChange{
			{Name: "entry-added", Status: "added"},
			{Name: "entry-removed", Status: "removed"},
			{Name: "entry-kernel", Status: "modified", KernelFrom: "/vmlinuz-a", KernelTo: "/vmlinuz-b"},
			{Name: "entry-initrd", Status: "modified", KernelFrom: "/vmlinuz", KernelTo: "/vmlinuz", InitrdFrom: "/initrd-a", InitrdTo: "/initrd-b"},
			{Name: "entry-cmdline", Status: "modified", KernelFrom: "/vmlinuz", KernelTo: "/vmlinuz", InitrdFrom: "/initrd", InitrdTo: "/initrd", CmdlineFrom: "root=UUID=11111111-1111-1111-1111-111111111111 ro", CmdlineTo: "root=UUID=22222222-2222-2222-2222-222222222222 rw"},
			{Name: "entry-meta", Status: "modified", KernelFrom: "/vmlinuz", KernelTo: "/vmlinuz", InitrdFrom: "/initrd", InitrdTo: "/initrd", CmdlineFrom: " root=UUID=aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa  ro ", CmdlineTo: "root=UUID=bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb ro"},
		},
		KernelRefChanges: []KernelRefChange{
			{Path: "EFI/Linux/new.efi", Status: "added"},
			{Path: "EFI/Linux/old.efi", Status: "removed"},
			{Path: "EFI/Linux/uuid.efi", Status: "modified", UUIDFrom: "u1", UUIDTo: "u2"},
			{Path: "EFI/Linux/meta.efi", Status: "modified", UUIDFrom: "same", UUIDTo: "same"},
		},
		UUIDReferenceChanges: []UUIDRefChange{
			{UUID: "a", Status: "added", MismatchTo: true},
			{UUID: "b", Status: "added", MismatchTo: false},
			{UUID: "c", Status: "removed", MismatchFrom: true},
			{UUID: "d", Status: "removed", MismatchFrom: false},
			{UUID: "e", Status: "modified", MismatchFrom: false, MismatchTo: true},
			{UUID: "f", Status: "modified", MismatchFrom: false, MismatchTo: false},
		},
		NotesAdded:   []string{"note-a", "note-b"},
		NotesRemoved: []string{"note-c"},
	}

	tallyBootloaderConfigDiff(tally, d, "EFI/BOOT/BOOTX64.EFI")

	if tally.meaningful != 14 {
		t.Fatalf("expected meaningful=14, got %d (reasons=%v)", tally.meaningful, tally.mReasons)
	}
	if tally.volatile != 8 {
		t.Fatalf("expected volatile=8, got %d (reasons=%v)", tally.volatile, tally.vReasons)
	}

	meaningfulJoined := strings.Join(tally.mReasons, "\n")
	volatileJoined := strings.Join(tally.vReasons, "\n")

	if !strings.Contains(meaningfulJoined, "boot entry initrd changed: entry-initrd") {
		t.Fatalf("expected initrd change to be classified meaningful, reasons=%v", tally.mReasons)
	}
	if !strings.Contains(volatileJoined, "boot entry metadata changed: entry-meta") {
		t.Fatalf("expected metadata-only boot entry change to be classified volatile, reasons=%v", tally.vReasons)
	}
}
