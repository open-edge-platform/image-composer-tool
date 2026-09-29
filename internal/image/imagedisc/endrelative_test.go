package imagedisc

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

func TestEndRelativeSector(t *testing.T) {
	t.Parallel()
	const gib = uint64(1) << 30
	const mib = uint64(1) << 20
	tests := []struct {
		name       string
		disk       uint64
		offset     uint64
		sectorSize uint64
		want       uint64
		wantErr    bool
	}{
		{"aligned 60GiB disk, 512B sectors", 60 * gib, 20 * gib, 512, 40 * gib / 512, false},
		{"4K sectors", 60 * gib, 4 * gib, 4096, 56 * gib / 4096, false},
		{"unaligned disk rounds down to 1MiB", 60*gib + 12345, 4 * gib, 512, 56 * gib / 512, false},
		{"offset equals disk", 20 * gib, 20 * gib, 512, 0, true},
		{"offset larger than disk", 10 * gib, 20 * gib, 512, 0, true},
		{"zero offset", 10 * gib, 0, 512, 0, true},
		{"zero sector size", 10 * gib, mib, 0, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := endRelativeSector(tt.disk, tt.offset, tt.sectorSize)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Errorf("endRelativeSector = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestParseBoundary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		raw, wantSize string
		wantFromEnd   bool
		wantErr       bool
	}{
		{"-16GiB", "16GiB", true, false},
		{" -1MiB", "1MiB", true, false},
		{"513MiB", "513MiB", false, false},
		{"-lots", "", true, true},
		{"-0", "", true, true},
		{"-0GiB", "", true, true},
	}
	for _, tt := range tests {
		size, fromEnd, err := parseBoundary(tt.raw)
		if (err != nil) != tt.wantErr || (err == nil && (size != tt.wantSize || fromEnd != tt.wantFromEnd)) {
			t.Errorf("parseBoundary(%q) = %q, %t, %v; want %q, %t, err=%t",
				tt.raw, size, fromEnd, err, tt.wantSize, tt.wantFromEnd, tt.wantErr)
		}
	}
}

func TestRequiredInstallDiskBytesEndRelative(t *testing.T) {
	t.Parallel()
	const mib = uint64(1) << 20
	const gib = uint64(1) << 30
	tests := []struct {
		name       string
		partitions []config.PartitionInfo
		want       uint64
		wantErr    bool
	}{
		{
			name: "absolute layout",
			partitions: []config.PartitionInfo{
				{ID: "boot", Start: "1MiB", End: "513MiB"},
				{ID: "root", Start: "513MiB", End: "0"},
			},
			want: 513 * mib,
		},
		{
			name: "root with trailing data and swap",
			partitions: []config.PartitionInfo{
				{ID: "boot", Start: "1MiB", End: "513MiB"},
				{ID: "root", Start: "513MiB", End: "-20GiB"},
				{ID: "data", Start: "-20GiB", End: "-4GiB"},
				{ID: "swap", Start: "-4GiB", End: "0"},
			},
			want: 513*mib + 20*gib + mib, // one aligned unit so root is not empty
		},
		{
			name:       "invalid boundary",
			partitions: []config.PartitionInfo{{ID: "root", Start: "-lots", End: "0"}},
			wantErr:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := requiredInstallDiskBytes(tt.partitions)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Errorf("requiredInstallDiskBytes = %d, want %d", got, tt.want)
			}
		})
	}
}

// fakeSysfs serves /sys reads for readFile from a map.
func fakeSysfs(t *testing.T, files map[string]string) {
	t.Helper()
	orig := readFile
	readFile = func(name string) ([]byte, error) {
		if v, ok := files[name]; ok {
			return []byte(v), nil
		}
		return nil, os.ErrNotExist
	}
	t.Cleanup(func() { readFile = orig })
}

func TestBoundarySectorEndRelative(t *testing.T) {
	origShell := shell.Default
	t.Cleanup(func() { shell.Default = origShell })
	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: `cat /sys/block/vda/queue/hw_sector_size`, Output: "512\n"},
		{Pattern: `cat /sys/block/vda/queue/physical_block_size`, Output: "512\n"},
		{Pattern: `.*`, Error: os.ErrPermission},
	})
	// 60 GiB disk = 125829120 512-byte units.
	fakeSysfs(t, map[string]string{"/sys/block/vda/size": "125829120\n"})

	got, err := boundarySector("vda", "20GiB", true)
	if err != nil {
		t.Fatalf("boundarySector: %v", err)
	}
	if want := uint64(40) << 30 / 512; got != want {
		t.Errorf("end-relative sector = %d, want %d", got, want)
	}
	got, err = boundarySector("vda", "513MiB", false)
	if err != nil {
		t.Fatalf("boundarySector absolute: %v", err)
	}
	if want := uint64(513) << 20 / 512; got != want {
		t.Errorf("absolute sector = %d, want %d", got, want)
	}
}

func TestPartitionNumber(t *testing.T) {
	fakeSysfs(t, map[string]string{
		"/sys/class/block/nvme0n1p12/partition": "12\n",
		"/sys/class/block/sda1/partition":       "1\n",
		"/sys/class/block/sdb1/partition":       "x\n",
	})
	tests := []struct {
		part, want string
		wantErr    bool
	}{
		{"/dev/nvme0n1p12", "12", false},
		{"/dev/sda1", "1", false},
		{"/dev/sdb1", "", true},
		{"/dev/sdc1", "", true},
	}
	for _, tt := range tests {
		got, err := PartitionNumber(tt.part)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("PartitionNumber(%q) = %q, %v; want %q, err=%t", tt.part, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestResolveInstallDiskPathCanonicalisesExplicitPath(t *testing.T) {
	orig := evalSymlinks
	t.Cleanup(func() { evalSymlinks = orig })
	evalSymlinks = func(p string) (string, error) {
		if p == "/dev/disk/by-id/nvme-X" {
			return "/dev/nvme0n1", nil
		}
		return "", os.ErrNotExist
	}
	for in, want := range map[string]string{
		"/dev/disk/by-id/nvme-X": "/dev/nvme0n1",
		"/dev/sda":               "/dev/sda",
	} {
		got, err := ResolveInstallDiskPath(config.DiskConfig{Path: in})
		if err != nil || got != want {
			t.Errorf("ResolveInstallDiskPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

// fakeDiskGeometry mocks the sector and physical block size queries for the
// given disks.
func fakeDiskGeometry(t *testing.T, hw, phys string, disks ...string) {
	t.Helper()
	origShell := shell.Default
	t.Cleanup(func() { shell.Default = origShell })
	var cmds []shell.MockCommand
	for _, d := range disks {
		cmds = append(cmds,
			shell.MockCommand{Pattern: `cat /sys/block/` + d + `/queue/hw_sector_size`, Output: hw + "\n"},
			shell.MockCommand{Pattern: `cat /sys/block/` + d + `/queue/physical_block_size`, Output: phys + "\n"})
	}
	cmds = append(cmds, shell.MockCommand{Pattern: `.*`, Error: os.ErrPermission})
	shell.Default = shell.NewMockExecutor(cmds)
}

func TestCheckEndRelativeLayoutFits(t *testing.T) {
	layout := []config.PartitionInfo{
		{ID: "boot", Start: "1MiB", End: "513MiB"},
		{ID: "root", Start: "513MiB", End: "-20GiB"},
		{ID: "swap", Start: "-20GiB", End: "0"},
	}
	// 513 MiB + 20 GiB exactly: the root partition would be empty.
	exact := (uint64(513)<<20 + uint64(20)<<30) / 512
	fakeDiskGeometry(t, "512", "512", "vda", "vdb")
	fakeSysfs(t, map[string]string{
		"/sys/block/vda/size": strconv.FormatUint(exact, 10),
		"/sys/block/vdb/size": strconv.FormatUint(exact+2048, 10), // +1 MiB
	})
	if err := checkEndRelativeLayoutFits("/dev/vda", layout); err == nil {
		t.Error("a disk leaving the root partition empty must be rejected before partitioning")
	}
	if err := checkEndRelativeLayoutFits("/dev/vdb", layout); err != nil {
		t.Errorf("a disk with room for the layout must pass: %v", err)
	}
	absolute := []config.PartitionInfo{{ID: "root", Start: "1MiB", End: "0"}}
	if err := checkEndRelativeLayoutFits("/dev/unknown", absolute); err != nil {
		t.Errorf("absolute layouts are not checked here: %v", err)
	}
}

func TestCheckEndRelativeLayoutOrdering(t *testing.T) {
	// 60 GiB disk.
	fakeDiskGeometry(t, "512", "512", "vdc")
	fakeSysfs(t, map[string]string{"/sys/block/vdc/size": strconv.FormatUint(uint64(60)<<30/512, 10)})
	tests := []struct {
		name    string
		layout  []config.PartitionInfo
		wantErr string
	}{
		{"valid", []config.PartitionInfo{
			{ID: "boot", Start: "1MiB", End: "513MiB"},
			{ID: "root", Start: "513MiB", End: "-20GiB"},
			{ID: "swap", Start: "-20GiB", End: "0"},
		}, ""},
		{"reversed range", []config.PartitionInfo{
			{ID: "root", Start: "1MiB", End: "-20GiB"},
			{ID: "bad", Start: "-4GiB", End: "-20GiB"},
		}, "must be before end"},
		{"overlap", []config.PartitionInfo{
			{ID: "root", Start: "1MiB", End: "-4GiB"},
			{ID: "data", Start: "-20GiB", End: "0"},
		}, "overlaps"},
	}
	for _, tt := range tests {
		err := checkEndRelativeLayoutFits("/dev/vdc", tt.layout)
		ok := err == nil
		if tt.wantErr != "" {
			ok = err != nil && strings.Contains(err.Error(), tt.wantErr)
		}
		if !ok {
			t.Errorf("%s: err = %v, want %q", tt.name, err, tt.wantErr)
		}
	}
}

func TestCheckEndRelativeLayoutUsesCreationRounding(t *testing.T) {
	// 4 KiB physical blocks round the 5KiB and 6KiB boundaries up to 8KiB,
	// leaving the partition empty; raw byte offsets would have accepted it.
	fakeDiskGeometry(t, "512", "4096", "vdd")
	fakeSysfs(t, map[string]string{"/sys/block/vdd/size": strconv.FormatUint(uint64(60)<<30/512, 10)})
	layout := []config.PartitionInfo{
		{ID: "tiny", Start: "5KiB", End: "6KiB"},
		{ID: "root", Start: "6KiB", End: "-4GiB"},
		{ID: "swap", Start: "-4GiB", End: "0"},
	}
	err := checkEndRelativeLayoutFits("/dev/vdd", layout)
	if err == nil || !strings.Contains(err.Error(), "must be before end") {
		t.Errorf("err = %v, want the rounded-empty partition rejected before partitioning", err)
	}
}

func TestResolveInstallDiskPathRejectsPartition(t *testing.T) {
	fakeSysfs(t, map[string]string{"/sys/class/block/sda1/partition": "1\n"})
	if _, err := ResolveInstallDiskPath(config.DiskConfig{Path: "/dev/sda1"}); err == nil ||
		!strings.Contains(err.Error(), "whole disk") {
		t.Errorf("err = %v, want a partition rejection", err)
	}
	if got, err := ResolveInstallDiskPath(config.DiskConfig{Path: "/dev/sda"}); err != nil || got != "/dev/sda" {
		t.Errorf("whole disk: got %q, %v", got, err)
	}
}

func TestPartitionSectorsZeroSentinel(t *testing.T) {
	fakeDiskGeometry(t, "512", "512", "vde")
	fakeSysfs(t, map[string]string{"/sys/block/vde/size": strconv.FormatUint(uint64(60)<<30/512, 10)})
	tests := []struct {
		name       string
		p          config.PartitionInfo
		wantStart  uint64
		wantEnd    uint64
		wantErrMsg string
	}{
		{"plain zero end", config.PartitionInfo{Start: "1MiB", End: "0"}, 2048, 0, ""},
		{"padded zero end", config.PartitionInfo{Start: "1MiB", End: " 0"}, 2048, 0, ""},
		{"padded zero start", config.PartitionInfo{Start: " 0 ", End: "1MiB"}, 0, 2047, ""},
		{"zero-sized end", config.PartitionInfo{Start: "1MiB", End: "0GiB"}, 0, 0, "start of the disk"},
	}
	for _, tt := range tests {
		start, end, err := partitionSectors("vde", tt.p)
		if tt.wantErrMsg != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErrMsg) {
				t.Errorf("%s: err = %v, want %q", tt.name, err, tt.wantErrMsg)
			}
			continue
		}
		if err != nil || start != tt.wantStart || end != tt.wantEnd {
			t.Errorf("%s: got %d, %d, %v; want %d, %d", tt.name, start, end, err, tt.wantStart, tt.wantEnd)
		}
	}
}
