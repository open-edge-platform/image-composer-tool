package overlay

import (
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

func TestAnalyzeLayout_GPTRootAndESP(t *testing.T) {
	parts := []partition{
		{Path: "/dev/loop0p1", FSType: "vfat", PartType: espTypeGUID, Size: 512 << 20},
		{Path: "/dev/loop0p2", FSType: "ext4", PartType: "4f68bce3-e8cd-4db1-96e7-fbcaf984b709", Size: 8 << 30},
	}
	layout, err := analyzeLayout(partitionTableGPT, parts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if layout.RootDevice != "/dev/loop0p2" || layout.RootFSType != "ext4" {
		t.Errorf("root = %s/%s, want /dev/loop0p2/ext4", layout.RootDevice, layout.RootFSType)
	}
	if layout.ESPDevice != "/dev/loop0p1" {
		t.Errorf("ESP = %s, want /dev/loop0p1", layout.ESPDevice)
	}
	if layout.PartitionTable != partitionTableGPT {
		t.Errorf("table = %s, want gpt", layout.PartitionTable)
	}
}

func TestAnalyzeLayout_XFSRootSupported(t *testing.T) {
	parts := []partition{
		{Path: "/dev/loop0p1", FSType: "xfs", PartLabel: "root", Size: 4 << 30},
	}
	layout, err := analyzeLayout(partitionTableGPT, parts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if layout.RootFSType != "xfs" || layout.RootDevice != "/dev/loop0p1" {
		t.Errorf("root = %s/%s, want /dev/loop0p1/xfs", layout.RootDevice, layout.RootFSType)
	}
	if layout.ESPDevice != "" {
		t.Errorf("ESP = %s, want empty", layout.ESPDevice)
	}
}

func TestAnalyzeLayout_MBRWithVfatESPFallback(t *testing.T) {
	// MBR image: ESP marked by type byte 0xef, root has no type GUID.
	parts := []partition{
		{Path: "/dev/loop0p1", FSType: "vfat", PartType: espTypeMBR, Size: 256 << 20},
		{Path: "/dev/loop0p2", FSType: "ext4", Size: 6 << 30},
	}
	layout, err := analyzeLayout(partitionTableDOS, parts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if layout.ESPDevice != "/dev/loop0p1" {
		t.Errorf("ESP = %s, want /dev/loop0p1", layout.ESPDevice)
	}
	if layout.RootDevice != "/dev/loop0p2" {
		t.Errorf("root = %s, want /dev/loop0p2", layout.RootDevice)
	}
}

func TestAnalyzeLayout_PicksLargestFilesystemAsRoot(t *testing.T) {
	// No type GUID, no "root" label: largest FS-carrying partition wins, and a
	// tiny ext2 /boot must not outrank the real root.
	parts := []partition{
		{Path: "/dev/loop0p1", FSType: "vfat", PartType: espTypeGUID, Size: 512 << 20},
		{Path: "/dev/loop0p2", FSType: "ext2", Size: 512 << 20}, // /boot
		{Path: "/dev/loop0p3", FSType: "ext4", Size: 10 << 30},  // root
	}
	layout, err := analyzeLayout(partitionTableGPT, parts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if layout.RootDevice != "/dev/loop0p3" {
		t.Errorf("root = %s, want /dev/loop0p3 (largest fs)", layout.RootDevice)
	}
}

func TestAnalyzeLayout_SwapNotChosenAsRoot(t *testing.T) {
	// No type GUID and no "root" label: a large swap partition must not outrank
	// the smaller ext4 root and cause a spurious unsupported-filesystem failure.
	parts := []partition{
		{Path: "/dev/loop0p1", FSType: "vfat", PartType: espTypeGUID, Size: 512 << 20},
		{Path: "/dev/loop0p2", FSType: "swap", Size: 16 << 30},
		{Path: "/dev/loop0p3", FSType: "ext4", Size: 8 << 30},
	}
	layout, err := analyzeLayout(partitionTableGPT, parts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if layout.RootDevice != "/dev/loop0p3" || layout.RootFSType != "ext4" {
		t.Errorf("root = %s/%s, want /dev/loop0p3/ext4", layout.RootDevice, layout.RootFSType)
	}
}

func TestAnalyzeLayout_RejectsLUKS(t *testing.T) {
	parts := []partition{
		{Path: "/dev/loop0p1", FSType: "vfat", PartType: espTypeGUID, Size: 512 << 20},
		{Path: "/dev/loop0p2", FSType: fsTypeLUKS, Size: 8 << 30},
	}
	_, err := analyzeLayout(partitionTableGPT, parts)
	if err == nil {
		t.Fatal("expected LUKS rejection, got nil")
	}
	assertActionable(t, err, "crypto_LUKS")
}

func TestAnalyzeLayout_RejectsDMVerityByGUID(t *testing.T) {
	parts := []partition{
		{Path: "/dev/loop0p1", FSType: "ext4", PartType: "4f68bce3-e8cd-4db1-96e7-fbcaf984b709", Size: 8 << 30},
		{Path: "/dev/loop0p2", PartType: "2c7357ed-ebd2-46d9-aec1-23d437ec2bf5", Size: 256 << 20},
	}
	_, err := analyzeLayout(partitionTableGPT, parts)
	if err == nil {
		t.Fatal("expected dm-verity rejection, got nil")
	}
	assertActionable(t, err, "dm-verity")
}

func TestAnalyzeLayout_RejectsDMVerityByLabel(t *testing.T) {
	parts := []partition{
		{Path: "/dev/loop0p1", FSType: "ext4", Size: 8 << 30},
		{Path: "/dev/loop0p2", PartLabel: "root-verity", Size: 256 << 20},
	}
	if _, err := analyzeLayout(partitionTableGPT, parts); err == nil {
		t.Fatal("expected dm-verity rejection by label, got nil")
	}
}

func TestAnalyzeLayout_RejectsUnknownRootFilesystem(t *testing.T) {
	parts := []partition{
		{Path: "/dev/loop0p1", FSType: "vfat", PartType: espTypeGUID, Size: 512 << 20},
		{Path: "/dev/loop0p2", FSType: "btrfs", Size: 8 << 30},
	}
	_, err := analyzeLayout(partitionTableGPT, parts)
	if err == nil {
		t.Fatal("expected unknown-fs rejection, got nil")
	}
	assertActionable(t, err, "btrfs")
}

func TestAnalyzeLayout_RejectsRootWithNoFilesystem(t *testing.T) {
	parts := []partition{
		{Path: "/dev/loop0p1", FSType: "", Size: 8 << 30},
	}
	if _, err := analyzeLayout(partitionTableGPT, parts); err == nil {
		t.Fatal("expected rejection for root with no filesystem, got nil")
	}
}

func TestAnalyzeLayout_RejectsNoPartitions(t *testing.T) {
	if _, err := analyzeLayout(partitionTableGPT, nil); err == nil {
		t.Fatal("expected rejection for empty partition list, got nil")
	}
}

func TestUnsupportedLayoutError_IncludesAllThreeParts(t *testing.T) {
	err := &unsupportedLayoutError{
		detected:    "thing X",
		reason:      "because Y",
		remediation: "do Z",
	}
	msg := err.Error()
	for _, want := range []string{"thing X", "because Y", "do Z", "detected", "remediation"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q missing %q", msg, want)
		}
	}
}

func TestIsDMVerity(t *testing.T) {
	tests := []struct {
		name string
		p    partition
		want bool
	}{
		{"guid", partition{PartType: "df3300ce-d69f-4c92-978c-9bfb0f38d820"}, true},
		{"label", partition{PartLabel: "usr-verity"}, true},
		{"fstype", partition{FSType: "dm_verity"}, true},
		{"plain ext4", partition{FSType: "ext4", PartLabel: "root"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDMVerity(tt.p); got != tt.want {
				t.Errorf("isDMVerity(%+v) = %v, want %v", tt.p, got, tt.want)
			}
		})
	}
}

func TestDetectPartitionTable(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	t.Run("gpt via lsblk", func(t *testing.T) {
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{
			{Pattern: "lsblk -dno PTTYPE '/dev/loop0'", Output: "gpt\n"},
		})
		got, err := detectPartitionTable("/dev/loop0")
		if err != nil || got != partitionTableGPT {
			t.Fatalf("got %q, err %v; want gpt", got, err)
		}
	})

	t.Run("dos via lsblk", func(t *testing.T) {
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{
			{Pattern: "lsblk -dno PTTYPE '/dev/loop1'", Output: "dos\n"},
		})
		got, err := detectPartitionTable("/dev/loop1")
		if err != nil || got != partitionTableDOS {
			t.Fatalf("got %q, err %v; want dos", got, err)
		}
	})

	t.Run("falls back to blkid", func(t *testing.T) {
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{
			{Pattern: "lsblk -dno PTTYPE '/dev/loop2'", Output: "", Error: nil},
			{Pattern: "blkid -p -s PTTYPE -o value '/dev/loop2'", Output: "gpt\n"},
		})
		got, err := detectPartitionTable("/dev/loop2")
		if err != nil || got != partitionTableGPT {
			t.Fatalf("got %q, err %v; want gpt", got, err)
		}
	})

	t.Run("rejects no partition table", func(t *testing.T) {
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{
			{Pattern: "lsblk -dno PTTYPE '/dev/loop3'", Output: "\n"},
			{Pattern: "blkid -p -s PTTYPE -o value '/dev/loop3'", Output: ""},
		})
		if _, err := detectPartitionTable("/dev/loop3"); err == nil {
			t.Fatal("expected rejection for missing table")
		}
	})

	t.Run("rejects unsupported table type", func(t *testing.T) {
		shell.Default = shell.NewMockExecutor([]shell.MockCommand{
			{Pattern: "lsblk -dno PTTYPE '/dev/loop4'", Output: "atari\n"},
		})
		if _, err := detectPartitionTable("/dev/loop4"); err == nil {
			t.Fatal("expected rejection for unsupported table type")
		}
	})
}

func TestProbePartitions(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	lsblkJSON := `{
	  "blockdevices": [
	    {"name":"loop0","path":"/dev/loop0","type":"loop","children":[
	      {"name":"loop0p1","path":"/dev/loop0p1","fstype":"vfat","parttype":"c12a7328-f81f-11d2-ba4b-00a0c93ec93b","label":"ESP","partlabel":"EFI","size":536870912,"type":"part"},
	      {"name":"loop0p2","path":"/dev/loop0p2","fstype":"ext4","parttype":"4f68bce3-e8cd-4db1-96e7-fbcaf984b709","label":"root","partlabel":"root","size":8589934592,"type":"part"}
	    ]}
	  ]
	}`

	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		// Mock udevadm so the pre-lsblk settle stays hermetic; otherwise the
		// MockExecutor falls back to running the real command via sudo.
		{Pattern: "udevadm settle", Output: ""},
		{Pattern: "lsblk -b --json .* '/dev/loop0'", Output: lsblkJSON},
	})
	parts, err := probePartitions("/dev/loop0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("got %d partitions, want 2", len(parts))
	}
	if parts[0].Path != "/dev/loop0p1" || parts[0].FSType != "vfat" {
		t.Errorf("part0 = %+v", parts[0])
	}
	if parts[1].Size != 8589934592 {
		t.Errorf("part1 size = %d, want 8589934592", parts[1].Size)
	}
}

func TestProbePartitions_StringSize(t *testing.T) {
	// Older lsblk emits SIZE as a quoted string even with --json -b.
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	lsblkJSON := `{"blockdevices":[{"name":"loop0","type":"loop","children":[
	  {"name":"loop0p1","path":"/dev/loop0p1","fstype":"ext4","size":"8589934592","type":"part"}
	]}]}`
	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		// Mock udevadm so the pre-lsblk settle stays hermetic; otherwise the
		// MockExecutor falls back to running the real command via sudo.
		{Pattern: "udevadm settle", Output: ""},
		{Pattern: "lsblk -b --json .* '/dev/loop0'", Output: lsblkJSON},
	})
	parts, err := probePartitions("/dev/loop0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(parts) != 1 || parts[0].Size != 8589934592 {
		t.Fatalf("parts = %+v, want one part of size 8589934592", parts)
	}
}

func TestProbePartitions_BlkidFallback(t *testing.T) {
	// lsblk returns empty FSTYPE/PARTTYPE (e.g. udev not yet settled). The direct
	// blkid probes must run and fill both fields so a real ext4 root is not misread
	// as "no recognizable filesystem" purely because of a probe race.
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	lsblkJSON := `{"blockdevices":[{"name":"loop0","type":"loop","children":[
	  {"name":"loop0p1","path":"/dev/loop0p1","fstype":"","parttype":"","size":8589934592,"type":"part"}
	]}]}`
	shell.Default = shell.NewMockExecutor([]shell.MockCommand{
		{Pattern: "udevadm settle", Output: ""},
		{Pattern: "lsblk -b --json .* '/dev/loop0'", Output: lsblkJSON},
		{Pattern: "blkid -p -s TYPE -o value '/dev/loop0p1'", Output: "ext4\n"},
		{Pattern: "blkid -p -s PART_ENTRY_TYPE -o value '/dev/loop0p1'", Output: "4F68BCE3-E8CD-4DB1-96E7-FBCAF984B709\n"},
	})
	parts, err := probePartitions("/dev/loop0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("got %d partitions, want 1", len(parts))
	}
	// blkid values are lower-cased by probePartitionValue.
	if parts[0].FSType != "ext4" {
		t.Errorf("fstype = %q, want ext4 (from blkid fallback)", parts[0].FSType)
	}
	if parts[0].PartType != "4f68bce3-e8cd-4db1-96e7-fbcaf984b709" {
		t.Errorf("parttype = %q, want the Linux-root GUID (from blkid fallback)", parts[0].PartType)
	}
}

func assertActionable(t *testing.T, err error, wantDetected string) {
	t.Helper()
	var ule *unsupportedLayoutError
	msg := err.Error()
	if !strings.Contains(msg, "remediation") {
		t.Errorf("error %q missing remediation", msg)
	}
	if !strings.Contains(strings.ToLower(msg), strings.ToLower(wantDetected)) {
		t.Errorf("error %q does not mention %q", msg, wantDetected)
	}
	// Confirm the typed error is used so callers can branch on it if needed.
	if !asUnsupportedLayout(err, &ule) {
		t.Errorf("error is not *unsupportedLayoutError: %T", err)
	}
}

// asUnsupportedLayout is a tiny errors.As shim kept local to the test to avoid
// importing errors solely for assertions.
func asUnsupportedLayout(err error, target **unsupportedLayoutError) bool {
	if e, ok := err.(*unsupportedLayoutError); ok {
		*target = e
		return true
	}
	return false
}
