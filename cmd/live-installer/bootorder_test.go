package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

const sampleEfibootmgr = `BootCurrent: 0001
Timeout: 0 seconds
BootOrder: 0001,0000,0003,0004
Boot0000* UiApp	FvVol(7cb8bdc9-f8eb-4f34-aaea-3ee4af6516a1)/FvFile(462caa21-7614-4503-836e-8ab6f4662331)
Boot0001* UEFI QEMU DVD-ROM QM00003 	PciRoot(0x0)/Pci(0x1,0x1)/Ata(Secondary,Master,0x0){auto_created_boot_option}
Boot0003* ICT	HD(1,GPT,5d6c4f1e-0000-0000-0000-000000000000,0x800,0x100000)/File(\EFI\BOOT\BOOTX64.EFI)
Boot0004* ICT backup tool	HD(2,GPT,aa,0x800,0x100)/File(\EFI\tool.efi)
Boot0005  Windows Boot Manager	HD(1,GPT,bb,0x800,0x100)/File(\EFI\Microsoft\Boot\bootmgfw.efi)
`

func TestParseEfibootmgr(t *testing.T) {
	t.Parallel()
	st := parseEfibootmgr(sampleEfibootmgr)
	if want := []string{"0001", "0000", "0003", "0004"}; !reflect.DeepEqual(st.order, want) {
		t.Errorf("order = %v, want %v", st.order, want)
	}
	want := []efiBootEntry{
		{"0000", "UiApp"}, {"0001", "UEFI QEMU DVD-ROM QM00003"}, {"0003", "ICT"},
		{"0004", "ICT backup tool"}, {"0005", "Windows Boot Manager"},
	}
	if !reflect.DeepEqual(st.entries, want) {
		t.Errorf("entries = %+v, want %+v", st.entries, want)
	}
}

func TestBootOrderFor(t *testing.T) {
	t.Parallel()
	current := []string{"0001", "0000", "0003", "0004"}
	if got, want := bootOrderFor(config.BootEntryPolicyPreserve, "0003", current),
		[]string{"0003", "0001", "0000", "0004"}; !reflect.DeepEqual(got, want) {
		t.Errorf("preserve order = %v, want %v", got, want)
	}
	got := bootOrderFor(config.BootEntryPolicyExclusive, "0003", current)
	if !reflect.DeepEqual(got, []string{"0003"}) {
		t.Errorf("exclusive order = %v", got)
	}
}

func TestEspPartitionPath(t *testing.T) {
	t.Parallel()
	diskMap := map[string]string{"efi": "/dev/sda1", "legacy": "/dev/sda2"}
	byType := []config.PartitionInfo{
		{ID: "legacy", MountPoint: "/boot/efi"}, {ID: "efi", Type: "esp", MountPoint: "/efi"},
	}
	if got := espPartitionPath(byType, diskMap); got != "/dev/sda1" {
		t.Errorf("type=esp should win, got %q", got)
	}
	byMount := []config.PartitionInfo{{ID: "legacy", MountPoint: "/boot/efi"}}
	if got := espPartitionPath(byMount, diskMap); got != "/dev/sda2" {
		t.Errorf("mount point fallback, got %q", got)
	}
}

// recordEfiCmds scripts efibootmgr: listing returns output, every other
// command is recorded and succeeds.
func recordEfiCmds(t *testing.T, output string) *[]string {
	t.Helper()
	var cmds []string
	orig := execCmdFn
	execCmdFn = func(cmd string, _ bool, _ string, _ []string) (string, error) {
		if cmd == "efibootmgr" {
			return output, nil
		}
		cmds = append(cmds, cmd)
		return "", nil
	}
	t.Cleanup(func() { execCmdFn = orig })
	return &cmds
}

func TestRemoveOldBootEntriesExactLabel(t *testing.T) {
	cmds := recordEfiCmds(t, sampleEfibootmgr)
	if err := removeOldBootEntries(); err != nil {
		t.Fatalf("removeOldBootEntries: %v", err)
	}
	if want := []string{"efibootmgr --delete-bootnum --bootnum 0003"}; !reflect.DeepEqual(*cmds, want) {
		t.Errorf("commands = %v, want %v (entries merely containing ICT must be kept)", *cmds, want)
	}
}

func TestSetBootOrder(t *testing.T) {
	cmds := recordEfiCmds(t, sampleEfibootmgr)
	if err := setBootOrder(config.BootEntryPolicyPreserve); err != nil {
		t.Fatalf("setBootOrder: %v", err)
	}
	if want := []string{"efibootmgr --bootorder 0003,0001,0000,0004"}; !reflect.DeepEqual(*cmds, want) {
		t.Errorf("commands = %v, want %v", *cmds, want)
	}
}

func TestSetBootOrderMissingEntry(t *testing.T) {
	recordEfiCmds(t, "BootOrder: 0000\nBoot0000* UiApp\n")
	err := setBootOrder(config.BootEntryPolicyPreserve)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %v", err)
	}
}

func TestReadEfiStateError(t *testing.T) {
	orig := execCmdFn
	execCmdFn = func(string, bool, string, []string) (string, error) { return "", fmt.Errorf("no efivars") }
	t.Cleanup(func() { execCmdFn = orig })
	if err := removeOldBootEntries(); err == nil {
		t.Error("expected listing failure to be reported")
	}
}

func TestCreateNewBootEntryUnknownArch(t *testing.T) {
	t.Parallel()
	tmpl := &config.ImageTemplate{
		Target: config.TargetInfo{Arch: "riscv64"},
		Disk: config.DiskConfig{Path: "/dev/sda", Partitions: []config.PartitionInfo{
			{ID: "boot", Type: "esp", MountPoint: "/boot/efi"},
		}},
	}
	err := createNewBootEntry(tmpl, map[string]string{"boot": "/dev/sda1"})
	if err == nil || !strings.Contains(err.Error(), "riscv64") {
		t.Errorf("err = %v", err)
	}
}

func TestCreateNewBootEntry(t *testing.T) {
	var streamed []string
	origStream, origPart := execStreamFn, partitionNumberFn
	execStreamFn = func(cmd string, _ bool, _ string, _ []string) (string, error) {
		streamed = append(streamed, cmd)
		return "", nil
	}
	partitionNumberFn = func(part string) (string, error) {
		if part != "/dev/nvme0n1p1" {
			return "", fmt.Errorf("unexpected partition %s", part)
		}
		return "1", nil
	}
	t.Cleanup(func() { execStreamFn, partitionNumberFn = origStream, origPart })

	for arch, loader := range map[string]string{"x86_64": "BOOTX64.EFI", "aarch64": "BOOTAA64.EFI"} {
		streamed = nil
		tmpl := &config.ImageTemplate{
			Target: config.TargetInfo{Arch: arch},
			Disk: config.DiskConfig{Path: "/dev/nvme0n1", Partitions: []config.PartitionInfo{
				{ID: "boot", Type: "esp", MountPoint: "/boot/efi"},
			}},
		}
		if err := createNewBootEntry(tmpl, map[string]string{"boot": "/dev/nvme0n1p1"}); err != nil {
			t.Fatalf("%s: createNewBootEntry: %v", arch, err)
		}
		want := "efibootmgr --create --disk /dev/nvme0n1 --part 1 --loader /EFI/BOOT/" + loader +
			" --label 'ICT' --verbose"
		if len(streamed) != 1 || streamed[0] != want {
			t.Errorf("%s: commands = %v, want [%s]", arch, streamed, want)
		}
	}
}

func TestSetBootOrderRejectsUnknownPolicy(t *testing.T) {
	// No firmware access may happen: the policy is checked first.
	orig := execCmdFn
	t.Cleanup(func() { execCmdFn = orig })
	execCmdFn = func(string, bool, string, []string) (string, error) {
		t.Error("firmware must not be queried for an unknown policy")
		return "", nil
	}
	if err := setBootOrder("exclusiv"); err == nil {
		t.Error("a mistyped policy must be an error, not silently treated as preserve")
	}
}
