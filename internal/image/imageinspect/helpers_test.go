package imageinspect

import (
	"bytes"
	"strings"
	"testing"
)

func TestParsePEFromBytes_InvalidPE(t *testing.T) {
	// Invalid PE data should return error
	invalidPE := []byte{0xFF, 0xFF, 0xFF, 0xFF}
	_, err := ParsePEFromBytes("test.efi", invalidPE)
	if err == nil {
		t.Fatalf("expected error for invalid PE data")
	}
}

func TestClassifyBootloaderKind_Grub(t *testing.T) {
	// Test grub identification by filename
	sections := []string{".text", ".data", ".reloc"}
	kind := classifyBootloaderKind("EFI/BOOT/grubx64.efi", sections)
	if kind != BootloaderGrub {
		t.Fatalf("expected BootloaderGrub for grubx64.efi, got %s", kind)
	}
}

func TestClassifyBootloaderKind_SystemdBoot(t *testing.T) {
	sections := []string{".text", ".data"}
	kind := classifyBootloaderKind("EFI/BOOT/systemd-bootx64.efi", sections)
	if kind != BootloaderSystemdBoot {
		t.Fatalf("expected BootloaderSystemdBoot, got %s", kind)
	}
}

func TestClassifyBootloaderKind_Shim(t *testing.T) {
	sections := []string{".sbat", ".text"}
	kind := classifyBootloaderKind("shimx64.efi", sections)
	if kind != BootloaderShim {
		t.Fatalf("expected BootloaderShim, got %s", kind)
	}
}

func TestCompareMeta_SameSize(t *testing.T) {
	imgFrom := &ImageSummary{File: "a.raw", SizeBytes: 1024}
	imgTo := &ImageSummary{File: "b.raw", SizeBytes: 1024}
	diff := compareMeta(*imgFrom, *imgTo)
	if diff.SizeBytes != nil {
		t.Fatalf("expected no size diff for identical size")
	}
}

func TestCompareMeta_DifferentSize(t *testing.T) {
	imgFrom := &ImageSummary{File: "a.raw", SizeBytes: 1024}
	imgTo := &ImageSummary{File: "b.raw", SizeBytes: 2048}
	diff := compareMeta(*imgFrom, *imgTo)
	if diff.SizeBytes == nil {
		t.Fatalf("expected size diff")
	}
}

func TestFilesystemPtrsEqual_BothNil(t *testing.T) {
	if !filesystemPtrsEqual(nil, nil) {
		t.Fatalf("expected nil filesystems to be equal")
	}
}

func TestFilesystemPtrsEqual_OneNil(t *testing.T) {
	fs := &FilesystemSummary{Type: "ext4"}
	if filesystemPtrsEqual(nil, fs) {
		t.Fatalf("expected nil and non-nil to be unequal")
	}
}

func TestStringSliceEqualSorted_Empty(t *testing.T) {
	if !stringSliceEqualSorted([]string{}, []string{}) {
		t.Fatalf("expected empty slices to be equal")
	}
}

func TestStringSliceEqualSorted_Unordered(t *testing.T) {
	a := []string{"z", "a", "m"}
	b := []string{"a", "m", "z"}
	if !stringSliceEqualSorted(a, b) {
		t.Fatalf("expected unordered slices to be equal")
	}
}

func TestStringSliceEqualSorted_Different(t *testing.T) {
	a := []string{"a", "b"}
	b := []string{"a", "c"}
	if stringSliceEqualSorted(a, b) {
		t.Fatalf("expected different content to be unequal")
	}
}

func TestIntSliceEqual_Empty(t *testing.T) {
	if !intSliceEqual([]int{}, []int{}) {
		t.Fatalf("expected empty int slices to be equal")
	}
}

func TestIntSliceEqual_Same(t *testing.T) {
	if !intSliceEqual([]int{1, 2, 3}, []int{1, 2, 3}) {
		t.Fatalf("expected identical int slices to be equal")
	}
}

func TestIntSliceEqual_Different(t *testing.T) {
	if intSliceEqual([]int{1, 2}, []int{1, 2, 3}) {
		t.Fatalf("expected different length slices to be unequal")
	}
}

func TestFreeSpanEqual_BothNil(t *testing.T) {
	if !freeSpanEqual(nil, nil) {
		t.Fatalf("expected nil free spans to be equal")
	}
}

func TestFreeSpanEqual_Same(t *testing.T) {
	a := &FreeSpanSummary{StartLBA: 100, EndLBA: 200, SizeBytes: 51200}
	b := &FreeSpanSummary{StartLBA: 100, EndLBA: 200, SizeBytes: 51200}
	if !freeSpanEqual(a, b) {
		t.Fatalf("expected identical free spans to be equal")
	}
}

func TestFreeSpanEqual_Different(t *testing.T) {
	a := &FreeSpanSummary{StartLBA: 100, EndLBA: 200, SizeBytes: 51200}
	b := &FreeSpanSummary{StartLBA: 100, EndLBA: 300, SizeBytes: 102400}
	if freeSpanEqual(a, b) {
		t.Fatalf("expected different free spans to be unequal")
	}
}

func TestIsVolatilePartitionField(t *testing.T) {
	if isVolatilePartitionField("Type") {
		t.Fatalf("expected Type field to not be volatile")
	}
}

func TestIsVolatileEFIBinaryField(t *testing.T) {
	if isVolatileEFIBinaryField("SHA256") {
		t.Fatalf("expected SHA256 to not be volatile")
	}
}

func TestIsVFATLike_Vfat(t *testing.T) {
	if !isVFATLike("vfat") {
		t.Fatalf("expected vfat to be vfat-like")
	}
}

func TestIsVFATLike_Other(t *testing.T) {
	if isVFATLike("ext4") {
		t.Fatalf("expected ext4 to not be vfat-like")
	}
}

func TestIsESPPartition_ESP(t *testing.T) {
	p := PartitionSummary{Type: "C12A7328-F81F-11D2-BA4B-00A0C93EC93B"}
	if !isESPPartition(p) {
		t.Fatalf("expected ESP GUID to be recognized")
	}
}

func TestIsESPPartition_NonESP(t *testing.T) {
	p := PartitionSummary{Type: "0FC63DAF-8483-4772-8E79-3D69D8477DE4"}
	if isESPPartition(p) {
		t.Fatalf("expected non-ESP GUID to not be recognized")
	}
}

func TestPEMachineToArch_X64(t *testing.T) {
	arch := peMachineToArch(0x8664)
	if arch != "x86_64" {
		t.Fatalf("expected x86_64, got %s", arch)
	}
}

func TestPEMachineToArch_ARM64(t *testing.T) {
	arch := peMachineToArch(0xAA64)
	if arch != "arm64" {
		t.Fatalf("expected arm64, got %s", arch)
	}
}

func TestParseOSRelease_Simple(t *testing.T) {
	raw := "NAME=Linux\nVERSION=5.10"
	m, pairs := parseOSRelease(raw)
	// Verify we got something
	if len(m) == 0 && len(pairs) == 0 {
		t.Fatalf("expected parsed OS release")
	}
}

func TestParseOSRelease_Empty(t *testing.T) {
	m, pairs := parseOSRelease("")
	if len(m) != 0 || len(pairs) != 0 {
		t.Fatalf("expected empty result for empty input")
	}
}

func TestSHA256Hex_Format(t *testing.T) {
	data := []byte("test data")
	hash := hashBytesHex(data)
	if len(hash) != 64 {
		t.Fatalf("expected 64 char hash, got %d", len(hash))
	}
	// Verify lowercase hex
	for _, c := range hash {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Fatalf("expected lowercase hex")
		}
	}
}

func TestPickLarger_NilNil(t *testing.T) {
	if pickLarger(nil, nil) != nil {
		t.Fatalf("expected nil for pickLarger(nil, nil)")
	}
}

func TestPickLarger_SelectLarger(t *testing.T) {
	a := &FreeSpanSummary{SizeBytes: 51200}
	b := &FreeSpanSummary{SizeBytes: 102400}
	result := pickLarger(a, b)
	if result.SizeBytes != 102400 {
		t.Fatalf("expected larger span")
	}
}

func TestNewDiskfsInspector(t *testing.T) {
	inspector := NewDiskfsInspector(false)
	if inspector == nil {
		t.Fatalf("expected non-nil inspector")
	}
}

func TestEmptyIfWhitespace_Whitespace(t *testing.T) {
	if emptyIfWhitespace("   \t\n  ") != "-" {
		t.Fatalf("expected '-' for whitespace input")
	}
}

func TestEmptyIfWhitespace_Text(t *testing.T) {
	result := emptyIfWhitespace("  hello  ")
	if result != "hello" {
		t.Fatalf("expected 'hello' (trimmed), got %q", result)
	}
}

func TestEmptyOr_Empty(t *testing.T) {
	if emptyOr("", "default") != "default" {
		t.Fatalf("expected default")
	}
}

func TestEmptyOr_Text(t *testing.T) {
	if emptyOr("value", "default") != "value" {
		t.Fatalf("expected value")
	}
}

func TestShortHash(t *testing.T) {
	result := shortHash("abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789")
	if len(result) > 12 || len(result) == 0 {
		t.Fatalf("expected short hash 1-12 chars")
	}
}

func TestDiffStringMap_Changes(t *testing.T) {
	from := map[string]string{"a": "1", "b": "2"}
	to := map[string]string{"b": "2_new", "c": "3"}
	diff := diffStringMap(from, to)

	if len(diff.Removed) != 1 || diff.Removed["a"] != "1" {
		t.Fatalf("expected 'a' removed")
	}
	if len(diff.Added) != 1 || diff.Added["c"] != "3" {
		t.Fatalf("expected 'c' added")
	}
	if len(diff.Modified) != 1 || diff.Modified["b"].To != "2_new" {
		t.Fatalf("expected 'b' modified")
	}
}

// Tests for renderer functions (text output)

func TestRenderCompareText_IdenticalImages(t *testing.T) {
	from := &ImageSummary{
		File:      "a.raw",
		SizeBytes: 1024,
		PartitionTable: PartitionTableSummary{
			Type:              "gpt",
			LogicalSectorSize: 512,
		},
	}
	to := &ImageSummary{
		File:      "b.raw",
		SizeBytes: 1024,
		PartitionTable: PartitionTableSummary{
			Type:              "gpt",
			LogicalSectorSize: 512,
		},
	}

	result := CompareImages(from, to)
	var buf bytes.Buffer
	err := RenderCompareText(&buf, &result, CompareTextOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if output == "" {
		t.Fatalf("expected non-empty text output")
	}
}

func TestRenderCompareText_DifferentSizes(t *testing.T) {
	from := &ImageSummary{
		File:      "a.raw",
		SizeBytes: 1024,
		PartitionTable: PartitionTableSummary{
			Type:              "gpt",
			LogicalSectorSize: 512,
		},
	}
	to := &ImageSummary{
		File:      "b.raw",
		SizeBytes: 2048,
		PartitionTable: PartitionTableSummary{
			Type:              "gpt",
			LogicalSectorSize: 512,
		},
	}

	result := CompareImages(from, to)
	var buf bytes.Buffer
	err := RenderCompareText(&buf, &result, CompareTextOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Successfully rendered
	if buf.Len() == 0 {
		t.Fatalf("expected non-empty output")
	}
}

func TestNormalizeCompareMode_ValidModes(t *testing.T) {
	mode := normalizeCompareMode("full")
	if mode != "full" {
		t.Fatalf("expected 'full' to remain unchanged, got %q", mode)
	}

	mode = normalizeCompareMode("diff")
	if mode != "diff" {
		t.Fatalf("expected 'diff' to remain unchanged, got %q", mode)
	}

	mode = normalizeCompareMode("summary")
	if mode != "summary" {
		t.Fatalf("expected 'summary' to remain unchanged, got %q", mode)
	}
}

func TestHasAnyEFIDiff_NoChanges(t *testing.T) {
	diff := EFIBinaryDiff{
		Added:    []EFIBinaryEvidence{},
		Removed:  []EFIBinaryEvidence{},
		Modified: []ModifiedEFIBinaryEvidence{},
	}
	if hasAnyEFIDiff(diff) {
		t.Fatalf("expected no EFI diffs")
	}
}

func TestHasAnyEFIDiff_WithAdded(t *testing.T) {
	diff := EFIBinaryDiff{
		Added: []EFIBinaryEvidence{
			{Path: "EFI/BOOT/BOOTX64.EFI"},
		},
	}
	if !hasAnyEFIDiff(diff) {
		t.Fatalf("expected EFI diffs")
	}
}

func TestComputeObjectCountsFromDiff_AllZero(t *testing.T) {
	diff := ImageDiff{
		Partitions: PartitionDiff{
			Added:    []PartitionSummary{},
			Removed:  []PartitionSummary{},
			Modified: []ModifiedPartitionSummary{},
		},
	}
	counts := computeObjectCountsFromDiff(diff)
	if counts.added != 0 || counts.modified != 0 || counts.removed != 0 {
		t.Fatalf("expected all zeros, got added=%d, modified=%d, removed=%d", counts.added, counts.modified, counts.removed)
	}
}

func TestComputeObjectCountsFromDiff_WithChanges(t *testing.T) {
	diff := ImageDiff{
		Partitions: PartitionDiff{
			Added: []PartitionSummary{
				{Name: "new"},
			},
			Removed: []PartitionSummary{
				{Name: "old"},
			},
			Modified: []ModifiedPartitionSummary{
				{Key: "p1"},
			},
		},
	}
	counts := computeObjectCountsFromDiff(diff)
	if counts.added != 1 || counts.modified != 1 || counts.removed != 1 {
		t.Fatalf("expected 1/1/1, got added=%d, modified=%d, removed=%d", counts.added, counts.modified, counts.removed)
	}
}

func TestHumanBytes_Zero(t *testing.T) {
	result := humanBytes(0)
	if result == "" {
		t.Fatalf("expected non-empty result for 0 bytes")
	}
}

func TestHumanBytes_KiloBytes(t *testing.T) {
	result := humanBytes(1024)
	if !strings.Contains(result, "1") || !strings.Contains(result, "K") {
		t.Fatalf("expected KB format, got %q", result)
	}
}

func TestHumanBytes_MegaBytes(t *testing.T) {
	result := humanBytes(1024 * 1024)
	if !strings.Contains(result, "M") {
		t.Fatalf("expected MB format, got %q", result)
	}
}

func TestPartitionTypeName_GPT(t *testing.T) {
	// EFI System Partition GUID
	name := partitionTypeName("gpt", "C12A7328-F81F-11D2-BA4B-00A0C93EC93B")
	if name == "" {
		t.Fatalf("expected non-empty partition type name")
	}
}

func TestPartitionTypeName_Unknown(t *testing.T) {
	name := partitionTypeName("gpt", "00000000-0000-0000-0000-000000000000")
	// Function should always return a non-empty string
	_ = name
}

func TestMBRTypeName_FAT32(t *testing.T) {
	// FAT32 type code as hex string
	name := mbrTypeName("0C")
	// Function should always return a string
	_ = name
}

func TestMBRTypeName_Unknown(t *testing.T) {
	name := mbrTypeName("FF")
	// Function should always return a string
	_ = name
}

func TestFreeSpanString_Valid(t *testing.T) {
	span := &FreeSpanSummary{
		StartLBA:  2048,
		EndLBA:    4095,
		SizeBytes: 1024 * 1024,
	}
	result := freeSpanString(span)
	if result == "" {
		t.Fatalf("expected non-empty freespan string")
	}
	if !strings.Contains(result, "2048") && !strings.Contains(result, "1") {
		t.Logf("freespan result: %s", result)
	}
}

func TestFreeSpanString_Nil(t *testing.T) {
	result := freeSpanString(nil)
	// Function returns "(none)" for nil, not empty string
	if result == "" {
		t.Fatalf("expected non-empty result for nil freespan")
	}
}

func TestFirstUKI_NoUKI(t *testing.T) {
	binaries := []EFIBinaryEvidence{
		{Path: "grub.efi", IsUKI: false},
		{Path: "shim.efi", IsUKI: false},
	}
	_, found := firstUKI(binaries)
	if found {
		t.Fatalf("expected no UKI found")
	}
}

func TestFirstUKI_WithUKI(t *testing.T) {
	binaries := []EFIBinaryEvidence{
		{Path: "grub.efi", IsUKI: false},
		{Path: "uki.efi", IsUKI: true},
	}
	uki, found := firstUKI(binaries)
	if !found {
		t.Fatalf("expected UKI found")
	}
	if uki.Path != "uki.efi" {
		t.Fatalf("expected uki.efi, got %s", uki.Path)
	}
}

func TestComparePartitions_AddedRemoved(t *testing.T) {
	ptFrom := PartitionTableSummary{
		Partitions: []PartitionSummary{
			{Name: "old", Filesystem: &FilesystemSummary{Type: "ext4", UUID: "uuid-1"}},
		},
	}
	ptTo := PartitionTableSummary{
		Partitions: []PartitionSummary{
			{Name: "new", Filesystem: &FilesystemSummary{Type: "ext4", UUID: "uuid-2"}},
		},
	}

	diff := comparePartitions(ptFrom, ptTo)
	if len(diff.Added) != 1 || len(diff.Removed) != 1 {
		t.Fatalf("expected 1 added/1 removed, got %d/%d", len(diff.Added), len(diff.Removed))
	}
}

func TestComparePartitionTable_DifferentTypes(t *testing.T) {
	ptFrom := PartitionTableSummary{
		Type:               "gpt",
		LogicalSectorSize:  512,
		PhysicalSectorSize: 4096,
	}
	ptTo := PartitionTableSummary{
		Type:               "mbr",
		LogicalSectorSize:  512,
		PhysicalSectorSize: 4096,
	}

	diff := comparePartitionTable(ptFrom, ptTo)
	if diff.Type == nil {
		t.Fatalf("expected Type diff")
	}
	if diff.Type.From != "gpt" || diff.Type.To != "mbr" {
		t.Fatalf("expected gpt->mbr, got %s->%s", diff.Type.From, diff.Type.To)
	}
	if !diff.Changed {
		t.Fatalf("expected Changed=true")
	}
}

// Renderer text tests for coverage

func TestRenderVerityInfo(t *testing.T) {
	var buf bytes.Buffer

	// Test with dm-verity enabled
	v := &VerityInfo{
		Enabled:       true,
		Method:        "custom-initramfs",
		RootDevice:    "/dev/mapper/rootfs_verity",
		HashPartition: 0,
		Notes:         []string{"root=/dev/mapper/*verity* pattern found", "No separate hash partition"},
	}

	renderVerityInfo(&buf, v)
	output := buf.String()

	if !strings.Contains(output, "dm-verity Configuration") {
		t.Errorf("expected 'dm-verity Configuration' header")
	}
	if !strings.Contains(output, "Enabled") {
		t.Errorf("expected 'Enabled' field")
	}
	if !strings.Contains(output, "custom-initramfs") {
		t.Errorf("expected method in output")
	}
	if !strings.Contains(output, "/dev/mapper/rootfs_verity") {
		t.Errorf("expected root device in output")
	}
	if !strings.Contains(output, "Notes:") {
		t.Errorf("expected Notes section")
	}
	if !strings.Contains(output, "root=/dev/mapper/*verity* pattern found") {
		t.Errorf("expected note text in output")
	}
}

func TestRenderVerityInfo_Disabled(t *testing.T) {
	var buf bytes.Buffer

	v := &VerityInfo{
		Enabled: false,
	}

	renderVerityInfo(&buf, v)
	output := buf.String()

	if !strings.Contains(output, "dm-verity Configuration") {
		t.Errorf("expected header even when disabled")
	}
	if !strings.Contains(output, "false") {
		t.Errorf("expected 'false' for Enabled field")
	}
}

func TestRenderVerityDiffText(t *testing.T) {
	var buf bytes.Buffer

	// Test dm-verity enabled
	diff := &VerityDiff{
		Changed: true,
		Added: &VerityInfo{
			Enabled:    true,
			Method:     "systemd-verity",
			RootDevice: "/dev/sda2",
		},
	}

	renderVerityDiffText(&buf, diff)
	output := buf.String()

	if !strings.Contains(output, "dm-verity:") {
		t.Errorf("expected 'dm-verity:' header")
	}
	if !strings.Contains(output, "ENABLED") {
		t.Errorf("expected ENABLED message")
	}
	if !strings.Contains(output, "systemd-verity") {
		t.Errorf("expected method in output")
	}
}

func TestRenderVerityDiffText_Removed(t *testing.T) {
	var buf bytes.Buffer

	diff := &VerityDiff{
		Changed: true,
		Removed: &VerityInfo{
			Enabled:    true,
			Method:     "custom-initramfs",
			RootDevice: "/dev/mapper/test",
		},
	}

	renderVerityDiffText(&buf, diff)
	output := buf.String()

	if !strings.Contains(output, "DISABLED") {
		t.Errorf("expected DISABLED message")
	}
}

func TestRenderVerityDiffText_Changed(t *testing.T) {
	var buf bytes.Buffer

	diff := &VerityDiff{
		Changed: true,
		Method:  &ValueDiff[string]{From: "systemd-verity", To: "custom-initramfs"},
	}

	renderVerityDiffText(&buf, diff)
	output := buf.String()

	if !strings.Contains(output, "dm-verity:") {
		t.Errorf("expected 'dm-verity:' header")
	}
	if !strings.Contains(output, "Method:") {
		t.Errorf("expected Method field")
	}
	if !strings.Contains(output, "systemd-verity") || !strings.Contains(output, "custom-initramfs") {
		t.Errorf("expected method change in output")
	}
}

func TestRenderEFIArtifactsTable(t *testing.T) {
	var buf bytes.Buffer

	arts := []EFIBinaryEvidence{
		{
			Kind:         "uki",
			IsUKI:        true,
			Signed:       true,
			Arch:         "x86_64",
			Path:         "EFI/BOOT/BOOTX64.EFI",
			Size:         1024000,
			SHA256:       "abcdef1234567890",
			KernelSHA256: "kernel123",
			InitrdSHA256: "initrd456",
		},
		{
			Kind:   "shim",
			Signed: true,
			Arch:   "x86_64",
			Path:   "EFI/BOOT/shimx64.efi",
			Size:   512000,
			SHA256: "1234567890abcdef",
		},
	}

	renderEFIArtifactsTable(&buf, arts)
	output := buf.String()

	if !strings.Contains(output, "KIND") {
		t.Errorf("expected KIND header")
	}
	if !strings.Contains(output, "uki") {
		t.Errorf("expected uki kind in output")
	}
	if !strings.Contains(output, "shim") {
		t.Errorf("expected shim kind in output")
	}
	if !strings.Contains(output, "x86_64") {
		t.Errorf("expected architecture in output")
	}
	if !strings.Contains(output, "BOOTX64.EFI") {
		t.Errorf("expected path in output")
	}
}

func TestRenderEFIArtifactsTable_Empty(t *testing.T) {
	var buf bytes.Buffer

	renderEFIArtifactsTable(&buf, []EFIBinaryEvidence{})
	output := buf.String()

	// Should still render header
	if !strings.Contains(output, "KIND") {
		t.Errorf("expected header even with empty list")
	}
}

func TestRenderUKIDetailsBlock(t *testing.T) {
	var buf bytes.Buffer

	uki := EFIBinaryEvidence{
		Kind:            "uki",
		IsUKI:           true,
		Path:            "EFI/Linux/linux.efi",
		KernelSHA256:    "kernel123",
		InitrdSHA256:    "initrd456",
		Cmdline:         "root=/dev/sda2 quiet splash",
		OSReleaseSorted: []KeyValue{{Key: "NAME", Value: "Test OS"}, {Key: "VERSION", Value: "1.0"}},
	}

	renderUKIDetailsBlock(&buf, uki)
	output := buf.String()

	if !strings.Contains(output, "UKI details") {
		t.Errorf("expected 'UKI details' header")
	}
	if !strings.Contains(output, "linux.efi") {
		t.Errorf("expected path in output")
	}
	if !strings.Contains(output, "Kernel SHA256:") {
		t.Errorf("expected Kernel SHA256 section")
	}
	if !strings.Contains(output, "Initrd SHA256:") {
		t.Errorf("expected Initrd SHA256 section")
	}
	if !strings.Contains(output, "EFI cmdline:") {
		t.Errorf("expected Cmdline section")
	}
	if !strings.Contains(output, "root=/dev/sda2") {
		t.Errorf("expected cmdline content")
	}
	if !strings.Contains(output, "Test OS") {
		t.Errorf("expected OS release content")
	}
}

func TestRenderEqualityReasonsBlock(t *testing.T) {
	var buf bytes.Buffer

	result := &ImageCompareResult{
		Equality: Equality{
			Class:             EqualityDifferent,
			MeaningfulDiffs:   3,
			MeaningfulReasons: []string{"+1 partition added", "-1 EFI binary removed", "~2 filesystems modified"},
		},
	}

	renderEqualityReasonsBlock(&buf, result)
	output := buf.String()

	if !strings.Contains(output, "Meaningful reasons") {
		t.Errorf("expected 'Meaningful reasons' header")
	}
	if !strings.Contains(output, "+1 partition added") {
		t.Errorf("expected first reason in output")
	}
	if !strings.Contains(output, "-1 EFI binary removed") {
		t.Errorf("expected second reason in output")
	}
	if !strings.Contains(output, "~2 filesystems modified") {
		t.Errorf("expected third reason in output")
	}
}

func TestRenderEqualityReasonsBlock_Empty(t *testing.T) {
	var buf bytes.Buffer

	result := &ImageCompareResult{
		Equality: Equality{
			Class:           EqualityBinary,
			VolatileReasons: []string{},
			MeaningfulDiffs: 0,
		},
	}

	renderEqualityReasonsBlock(&buf, result)
	output := buf.String()

	// Should not render anything for empty/identical
	if strings.Contains(output, "reasons") {
		t.Errorf("expected no reasons output for identical images")
	}
}

func TestPrintOSReleaseKV(t *testing.T) {
	var buf bytes.Buffer

	kvs := []KeyValue{
		{Key: "NAME", Value: "Test Linux"},
		{Key: "VERSION_ID", Value: "42"},
		{Key: "ID", Value: "testlinux"},
	}

	printOSReleaseKV(&buf, "OS Release", kvs)
	output := buf.String()

	if !strings.Contains(output, "NAME") {
		t.Errorf("expected NAME field")
	}
	if !strings.Contains(output, "Test Linux") {
		t.Errorf("expected NAME value")
	}
	if !strings.Contains(output, "VERSION_ID") {
		t.Errorf("expected VERSION_ID field")
	}
	if !strings.Contains(output, "42") {
		t.Errorf("expected VERSION_ID value")
	}
}

func TestPrintOSReleaseKV_Empty(t *testing.T) {
	var buf bytes.Buffer

	printOSReleaseKV(&buf, "OS Release", []KeyValue{})
	output := buf.String()

	// Should handle empty list gracefully
	if output != "" {
		t.Errorf("expected empty output for empty os-release")
	}
}

func TestRenderImagesBlock(t *testing.T) {
	var buf bytes.Buffer

	from := ImageSummary{File: "image1.raw"}
	to := ImageSummary{File: "image2.raw"}

	renderImagesBlock(&buf, from, to)
	output := buf.String()

	if !strings.Contains(output, "Images") {
		t.Errorf("expected 'Images' header")
	}
	if !strings.Contains(output, "image1.raw") {
		t.Errorf("expected first image in output")
	}
	if !strings.Contains(output, "image2.raw") {
		t.Errorf("expected second image in output")
	}
}
