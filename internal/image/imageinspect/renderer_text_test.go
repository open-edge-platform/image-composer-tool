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
