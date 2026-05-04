package imageinspect

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderBootloaderConfigDiffText_IncludesInitrdAndSections(t *testing.T) {
	diff := &BootloaderConfigDiff{
		ConfigFileChanges: []ConfigFileChange{{
			Path:     "/boot/grub/grub.cfg",
			Status:   "modified",
			HashFrom: strings.Repeat("a", 64),
			HashTo:   strings.Repeat("b", 64),
		}},
		BootEntryChanges: []BootEntryChange{{
			Name:        "UKI Boot Entry",
			Status:      "modified",
			KernelFrom:  "/vmlinuz-old",
			KernelTo:    "/vmlinuz-new",
			InitrdFrom:  "/initrd-old",
			InitrdTo:    "/initrd-new",
			CmdlineFrom: "root=UUID=11111111-1111-1111-1111-111111111111 ro",
			CmdlineTo:   "root=UUID=22222222-2222-2222-2222-222222222222 rw",
		}},
		KernelRefChanges: []KernelRefChange{{
			Path:     "EFI/Linux/linux.efi",
			Status:   "modified",
			UUIDFrom: "olduuid",
			UUIDTo:   "newuuid",
		}},
		UUIDReferenceChanges: []UUIDRefChange{{
			UUID:       "33333333-3333-3333-3333-333333333333",
			Status:     "modified",
			MismatchTo: true,
			ContextTo:  "kernel_cmdline",
		}},
		NotesAdded:   []string{"new issue"},
		NotesRemoved: []string{"fixed issue"},
	}

	var buf bytes.Buffer
	renderBootloaderConfigDiffText(&buf, diff, "  ")
	out := buf.String()

	wants := []string{
		"Config files:",
		"Boot entries:",
		"kernel: /vmlinuz-old -> /vmlinuz-new",
		"initrd: /initrd-old -> /initrd-new",
		"Kernel references:",
		"UUID validation:",
		"CRITICAL: 33333333-3333-3333-3333-333333333333 not found in partition table",
		"New issues:",
		"Resolved issues:",
	}

	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestRenderPartitionSummaryLine_AndFilesystemChangeText(t *testing.T) {
	var partBuf bytes.Buffer
	renderPartitionSummaryLine(&partBuf, "  +", PartitionSummary{
		Index:     1,
		Name:      "ESP",
		Type:      "efi",
		StartLBA:  2048,
		EndLBA:    4095,
		SizeBytes: 1024 * 1024,
		Flags:     "",
		Filesystem: &FilesystemSummary{
			Type:    "vfat",
			FATType: "FAT32",
			Label:   "EFI",
			UUID:    "ABCD-1234",
		},
	})

	partOut := partBuf.String()
	if !strings.Contains(partOut, "idx=1") || !strings.Contains(partOut, "fs=vfat(FAT32)") {
		t.Fatalf("unexpected partition summary output: %s", partOut)
	}

	var fsBuf bytes.Buffer
	renderFilesystemChangeText(&fsBuf, &FilesystemChange{Added: &FilesystemSummary{Type: "ext4", UUID: "u1", Label: "rootfs"}})
	renderFilesystemChangeText(&fsBuf, &FilesystemChange{Removed: &FilesystemSummary{Type: "vfat", UUID: "u2", Label: "EFI"}})
	renderFilesystemChangeText(&fsBuf, &FilesystemChange{Modified: &ModifiedFilesystemSummary{
		From: FilesystemSummary{Type: "ext4", UUID: "u-old"},
		To:   FilesystemSummary{Type: "ext4", UUID: "u-new"},
		Changes: []FieldChange{{
			Field: "uuid",
			From:  "u-old",
			To:    "u-new",
		}},
	}})

	fsOut := fsBuf.String()
	if !strings.Contains(fsOut, "FS: added type=ext4") {
		t.Fatalf("expected added FS line, got: %s", fsOut)
	}
	if !strings.Contains(fsOut, "FS: removed type=vfat") {
		t.Fatalf("expected removed FS line, got: %s", fsOut)
	}
	if !strings.Contains(fsOut, "FS: modified ext4(u-old) -> ext4(u-new)") {
		t.Fatalf("expected modified FS line, got: %s", fsOut)
	}
}

func TestRenderEFIBinaryDiffText_FullBranches(t *testing.T) {
	var buf bytes.Buffer

	diff := EFIBinaryDiff{
		Added: []EFIBinaryEvidence{{
			Path:   "EFI/BOOT/NEW.EFI",
			Kind:   BootloaderShim,
			Arch:   "x86_64",
			Signed: true,
			SHA256: strings.Repeat("1", 64),
		}},
		Removed: []EFIBinaryEvidence{{
			Path:   "EFI/BOOT/OLD.EFI",
			Kind:   BootloaderGrub,
			Arch:   "x86_64",
			Signed: false,
			SHA256: strings.Repeat("2", 64),
		}},
		Modified: []ModifiedEFIBinaryEvidence{{
			Key: "EFI/BOOT/BOOTX64.EFI",
			From: EFIBinaryEvidence{
				Kind:         BootloaderGrub,
				SHA256:       strings.Repeat("a", 64),
				Signed:       true,
				Cmdline:      "root=UUID=11111111-1111-1111-1111-111111111111 ro",
				OSReleaseRaw: "ID=old",
			},
			To: EFIBinaryEvidence{
				Kind:         BootloaderSystemdBoot,
				SHA256:       strings.Repeat("b", 64),
				Signed:       false,
				Cmdline:      "root=UUID=22222222-2222-2222-2222-222222222222 rw",
				OSReleaseRaw: "ID=new",
			},
			UKI: &UKIDiff{
				Changed:       true,
				KernelSHA256:  &ValueDiff[string]{From: strings.Repeat("c", 64), To: strings.Repeat("d", 64)},
				InitrdSHA256:  &ValueDiff[string]{From: strings.Repeat("e", 64), To: strings.Repeat("f", 64)},
				CmdlineSHA256: &ValueDiff[string]{From: strings.Repeat("3", 64), To: strings.Repeat("4", 64)},
				OSRelSHA256:   &ValueDiff[string]{From: strings.Repeat("5", 64), To: strings.Repeat("6", 64)},
				UnameSHA256:   &ValueDiff[string]{From: strings.Repeat("7", 64), To: strings.Repeat("8", 64)},
			},
			BootConfig: &BootloaderConfigDiff{
				BootEntryChanges: []BootEntryChange{{
					Name:       "entry",
					Status:     "modified",
					InitrdFrom: "/initrd-old",
					InitrdTo:   "/initrd-new",
				}},
			},
		}},
	}

	renderEFIBinaryDiffText(&buf, diff, "  ")
	out := buf.String()

	wants := []string{
		"Added:",
		"Removed:",
		"Modified:",
		"kind: grub -> systemd-boot",
		"sha256:",
		"cmdline:",
		"signed: true -> false",
		"UKI payload:",
		"kernel:",
		"initrd:",
		"cmdline:",
		"osrel:",
		"uname:",
		"os release raw:",
		"Bootloader config:",
		"initrd: /initrd-old -> /initrd-new",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestRenderCompareText_NilResult(t *testing.T) {
	var buf bytes.Buffer
	err := RenderCompareText(&buf, nil, CompareTextOptions{Mode: "diff"})
	if err == nil {
		t.Fatalf("expected error for nil compare result")
	}
}

func TestRenderCompareText_Modes(t *testing.T) {
	rich := &ImageCompareResult{
		From: ImageSummary{File: "from.img", SizeBytes: 1024},
		To:   ImageSummary{File: "to.img", SizeBytes: 2048},
		Equality: Equality{
			Class:           EqualityDifferent,
			VolatileDiffs:   1,
			MeaningfulDiffs: 2,
		},
		Summary: CompareSummary{
			Changed:               true,
			PartitionTableChanged: true,
			PartitionsChanged:     true,
			EFIBinariesChanged:    true,
			SBOMChanged:           true,
		},
		Diff: ImageDiff{
			Image: MetaDiff{SizeBytes: &ValueDiff[int64]{From: 1024, To: 2048}},
			PartitionTable: PartitionTableDiff{
				Changed: true,
				Type:    &ValueDiff[string]{From: "gpt", To: "mbr"},
			},
			Partitions: PartitionDiff{
				Added:   []PartitionSummary{{Index: 2, Name: "data", Type: "linux", StartLBA: 2048, EndLBA: 4095, SizeBytes: 1024}},
				Removed: []PartitionSummary{{Index: 1, Name: "root", Type: "linux", StartLBA: 34, EndLBA: 2047, SizeBytes: 1024}},
				Modified: []ModifiedPartitionSummary{{
					Key: "idx=3",
					Changes: []FieldChange{{
						Field: "name",
						From:  "old",
						To:    "new",
					}},
				}},
			},
			Verity: &VerityDiff{
				Changed:    true,
				Method:     &ValueDiff[string]{From: "systemd-verity", To: "custom-initramfs"},
				RootDevice: &ValueDiff[string]{From: "/dev/sda2", To: "/dev/sda3"},
			},
			SBOM: &SBOMDiff{
				Changed:         true,
				FileName:        &ValueDiff[string]{From: "spdx_a.json", To: "spdx_b.json"},
				PackageCount:    &ValueDiff[int]{From: 1, To: 2},
				CanonicalSHA256: &ValueDiff[string]{From: strings.Repeat("a", 64), To: strings.Repeat("b", 64)},
			},
		},
	}

	var summaryBuf bytes.Buffer
	if err := RenderCompareText(&summaryBuf, rich, CompareTextOptions{Mode: "summary"}); err != nil {
		t.Fatalf("summary mode render error: %v", err)
	}
	summaryOut := summaryBuf.String()
	for _, want := range []string{"Changed:", "PartitionTableChanged:", "SBOMChanged:", "Counts (objects):"} {
		if !strings.Contains(summaryOut, want) {
			t.Fatalf("summary output missing %q:\n%s", want, summaryOut)
		}
	}

	var diffBuf bytes.Buffer
	if err := RenderCompareText(&diffBuf, rich, CompareTextOptions{Mode: "diff"}); err != nil {
		t.Fatalf("diff mode render error: %v", err)
	}
	diffOut := diffBuf.String()
	for _, want := range []string{"Partition table:", "Partitions:", "dm-verity:", "SBOM:"} {
		if !strings.Contains(diffOut, want) {
			t.Fatalf("diff output missing %q:\n%s", want, diffOut)
		}
	}

	unchanged := &ImageCompareResult{
		From:     ImageSummary{File: "from.img"},
		To:       ImageSummary{File: "to.img"},
		Equality: Equality{Class: EqualitySemantic},
		Summary:  CompareSummary{Changed: false},
	}

	var fullBuf bytes.Buffer
	if err := RenderCompareText(&fullBuf, unchanged, CompareTextOptions{Mode: "full"}); err != nil {
		t.Fatalf("full mode render error: %v", err)
	}
	fullOut := fullBuf.String()
	if !strings.Contains(fullOut, "No changes detected.") {
		t.Fatalf("expected unchanged full output, got:\n%s", fullOut)
	}
}

func TestRenderSummaryText_AndSPDXCompareText(t *testing.T) {
	var summaryBuf bytes.Buffer
	summary := &ImageSummary{
		File:      "image.raw",
		SizeBytes: 4096,
		SHA256:    strings.Repeat("a", 64),
		PartitionTable: PartitionTableSummary{
			Type:               "gpt",
			LogicalSectorSize:  512,
			PhysicalSectorSize: 4096,
			ProtectiveMBR:      true,
			Partitions: []PartitionSummary{{
				Index:     1,
				Name:      "root",
				Type:      "linux",
				StartLBA:  2048,
				EndLBA:    4095,
				SizeBytes: 1024,
				Filesystem: &FilesystemSummary{
					Type:  "ext4",
					Label: "rootfs",
					UUID:  "uuid-1",
				},
			}},
		},
		Verity: &VeritySummary{
			Enabled:       true,
			Method:        "systemd-verity",
			RootDevice:    "/dev/sda2",
			HashPartition: 3,
		},
		SBOM: SBOMSummary{
			Present:         true,
			Path:            "/usr/share/sbom/spdx_manifest.json",
			Format:          "spdx",
			SizeBytes:       123,
			SHA256:          strings.Repeat("b", 64),
			CanonicalSHA256: strings.Repeat("c", 64),
			PackageCount:    10,
		},
	}

	if err := RenderSummaryText(&summaryBuf, summary, TextOptions{}); err != nil {
		t.Fatalf("RenderSummaryText error: %v", err)
	}
	summaryOut := summaryBuf.String()
	for _, want := range []string{"OS Image Summary", "Partition Table", "dm-verity", "SBOM", "Partition 1 filesystem details"} {
		if !strings.Contains(summaryOut, want) {
			t.Fatalf("summary output missing %q:\n%s", want, summaryOut)
		}
	}

	if err := RenderSummaryText(&summaryBuf, nil, TextOptions{}); err == nil {
		t.Fatalf("expected nil summary error")
	}

	var spdxBuf bytes.Buffer
	spdx := &SPDXCompareResult{
		FromPath:          "from.json",
		ToPath:            "to.json",
		Equal:             false,
		FromPackageCount:  1,
		ToPackageCount:    2,
		FromCanonicalHash: strings.Repeat("d", 64),
		ToCanonicalHash:   strings.Repeat("e", 64),
		AddedPackages:     []string{"pkg-new"},
		RemovedPackages:   []string{"pkg-old"},
	}
	if err := RenderSPDXCompareText(&spdxBuf, spdx); err != nil {
		t.Fatalf("RenderSPDXCompareText error: %v", err)
	}
	spdxOut := spdxBuf.String()
	for _, want := range []string{"SPDX Compare", "Added packages:", "Removed packages:"} {
		if !strings.Contains(spdxOut, want) {
			t.Fatalf("spdx compare output missing %q:\n%s", want, spdxOut)
		}
	}

	if err := RenderSPDXCompareText(&spdxBuf, nil); err == nil {
		t.Fatalf("expected nil SPDX compare error")
	}
}
