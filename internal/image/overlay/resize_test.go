package overlay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

// writeSizedFile creates a file of exactly n bytes and returns its path.
func writeSizedFile(t *testing.T, n int64) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "baseline.raw")
	f, err := os.Create(p)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer f.Close()
	if err := f.Truncate(n); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return p
}

func TestPlanResize_NoTargetSkips(t *testing.T) {
	p := writeSizedFile(t, 1<<20)
	plan, err := planResize(p, "", false)
	if err != nil {
		t.Fatalf("planResize: %v", err)
	}
	if plan.Grow {
		t.Errorf("empty target must not grow: %+v", plan)
	}
}

func TestPlanResize_SmallerOrEqualSkips(t *testing.T) {
	// 100 MiB file, request 50 MiB (smaller) and 100 MiB (equal): both no-ops.
	p := writeSizedFile(t, 100<<20)
	for _, target := range []string{"50MiB", "100MiB"} {
		plan, err := planResize(p, target, false)
		if err != nil {
			t.Fatalf("planResize(%s): %v", target, err)
		}
		if plan.Grow {
			t.Errorf("grow-only: target %s must not grow a 100MiB image: %+v", target, plan)
		}
		if plan.Reason == "" {
			t.Errorf("a skipped resize should carry a reason, got %+v", plan)
		}
	}
}

func TestPlanResize_LargerGrows(t *testing.T) {
	p := writeSizedFile(t, 100<<20)
	plan, err := planResize(p, "200MiB", true)
	if err != nil {
		t.Fatalf("planResize: %v", err)
	}
	if !plan.Grow {
		t.Fatalf("target 200MiB must grow a 100MiB image: %+v", plan)
	}
	if plan.CurrentBytes != 100<<20 || plan.TargetBytes != 200<<20 {
		t.Errorf("bytes = current %d target %d, want 100MiB/200MiB", plan.CurrentBytes, plan.TargetBytes)
	}
}

// A grow that is requested (disk.size larger than baseline) but not opted into
// must be a hard error, not a silent resize — overlay preserves the baseline
// layout unless the template explicitly allows growing it.
func TestPlanResize_LargerWithoutOptInErrors(t *testing.T) {
	p := writeSizedFile(t, 100<<20)
	_, err := planResize(p, "200MiB", false)
	if err == nil {
		t.Fatal("expected error when a grow is required but allowDiskResize is false")
	}
	if !strings.Contains(err.Error(), "allowDiskResize") {
		t.Errorf("error should name the allowDiskResize opt-in; got: %v", err)
	}
}

func TestPlanResize_InvalidSize(t *testing.T) {
	p := writeSizedFile(t, 1<<20)
	if _, err := planResize(p, "not-a-size", true); err == nil {
		t.Fatal("expected error for an unparseable size")
	}
}

func TestPlanResize_RejectsSizeAboveInt64Max(t *testing.T) {
	// A size above math.MaxInt64 would wrap negative when narrowed to int64 and be
	// misread as "smaller than current", silently skipping the grow. It must be a
	// hard error instead. 10000000000GiB parses to a uint64 well over MaxInt64.
	p := writeSizedFile(t, 1<<20)
	if _, err := planResize(p, "10000000000GiB", true); err == nil {
		t.Fatal("expected error for a size exceeding int64 range")
	}
}

// lastPartitionJSON is an lsblk --json layout in which rootDevice is the last
// partition on the disk (highest START). Used to satisfy the non-last-partition
// guard in the grow-sequence tests.
func lastPartitionJSON(rootDevice string) string {
	return `{"blockdevices":[{"name":"loop0","path":"/dev/loop0","type":"loop","children":[
	  {"name":"loop0p1","path":"/dev/loop0p1","start":2048,"type":"part"},
	  {"name":"root","path":"` + rootDevice + `","start":1050624,"type":"part"}
	]}]}`
}

// stubResizeToolsPresent overrides the tool-availability probe so all tools
// report present, and restores it when the test ends.
func stubResizeToolsPresent(t *testing.T) {
	t.Helper()
	orig := resizeToolExists
	t.Cleanup(func() { resizeToolExists = orig })
	resizeToolExists = func(string) (bool, error) { return true, nil }
}

// recordingExec returns a resizeExec stub that records every command and answers
// the lsblk START-offset probe (the non-last-partition guard) with a layout in
// which rootDevice is last. All other commands return empty output.
func recordingExec(cmds *[]string, rootDevice string) func(string) (string, error) {
	return func(cmd string) (string, error) {
		*cmds = append(*cmds, cmd)
		if strings.Contains(cmd, "lsblk") {
			return lastPartitionJSON(rootDevice), nil
		}
		return "", nil
	}
}

func TestResizeBaseline_GrowRunsExpectedSequence(t *testing.T) {
	origExec := resizeExec
	defer func() { resizeExec = origExec }()
	stubResizeToolsPresent(t)
	var cmds []string
	resizeExec = recordingExec(&cmds, "/dev/loop0p2")

	p := writeSizedFile(t, 100<<20)
	tmpl := &config.ImageTemplate{
		Disk:          config.DiskConfig{Size: "200MiB"},
		OverlayPolicy: &config.OverlayPolicy{AllowDiskResize: true},
	}
	ctx := &Context{BaselineCopyPath: p, LoopDevPath: "/dev/loop0"}
	layout := &Layout{RootDevice: "/dev/loop0p2", RootFSType: "ext4", RootMount: "/mnt/root", PartitionTable: partitionTableGPT}

	if err := ResizeBaseline(tmpl, ctx, layout); err != nil {
		t.Fatalf("ResizeBaseline: %v", err)
	}

	joined := strings.Join(cmds, "\n")
	// Device/mount paths are single-quoted via shell.QuoteArg before interpolation.
	for _, want := range []string{
		"losetup -c '/dev/loop0'",
		"sgdisk -e '/dev/loop0'",
		"growpart '/dev/loop0' '2'",
		"resize2fs '/dev/loop0p2'",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("resize sequence missing %q; got:\n%s", want, joined)
		}
	}
	// The backing file is grown in-process (os.Truncate), not via a shell command,
	// so assert on the resulting file size rather than the command sequence.
	if strings.Contains(joined, "truncate") {
		t.Errorf("backing-file grow must not shell out to truncate; got:\n%s", joined)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat grown backing file: %v", err)
	}
	if fi.Size() != 200<<20 {
		t.Errorf("backing file size = %d, want %d (grown in-process)", fi.Size(), 200<<20)
	}
}

func TestResizeBaseline_NoGrowRunsNothing(t *testing.T) {
	origExec := resizeExec
	defer func() { resizeExec = origExec }()
	ran := false
	resizeExec = func(string) (string, error) { ran = true; return "", nil }

	p := writeSizedFile(t, 100<<20)
	tmpl := &config.ImageTemplate{Disk: config.DiskConfig{Size: "50MiB"}} // smaller
	ctx := &Context{BaselineCopyPath: p, LoopDevPath: "/dev/loop0"}
	layout := &Layout{RootDevice: "/dev/loop0p2", RootFSType: "ext4", RootMount: "/mnt/root"}

	if err := ResizeBaseline(tmpl, ctx, layout); err != nil {
		t.Fatalf("ResizeBaseline: %v", err)
	}
	if ran {
		t.Error("a grow-only resize must run no commands when the target is not larger")
	}
}

func TestResizeBaseline_GrowWithoutOptInErrorsAndRunsNothing(t *testing.T) {
	origExec := resizeExec
	defer func() { resizeExec = origExec }()
	ran := false
	resizeExec = func(string) (string, error) { ran = true; return "", nil }

	p := writeSizedFile(t, 100<<20)
	// Larger target but no OverlayPolicy opt-in: must error before touching the disk.
	tmpl := &config.ImageTemplate{Disk: config.DiskConfig{Size: "200MiB"}}
	ctx := &Context{BaselineCopyPath: p, LoopDevPath: "/dev/loop0"}
	layout := &Layout{RootDevice: "/dev/loop0p2", RootFSType: "ext4", RootMount: "/mnt/root", PartitionTable: partitionTableGPT}

	err := ResizeBaseline(tmpl, ctx, layout)
	if err == nil {
		t.Fatal("expected ResizeBaseline to error when a grow is required but not opted into")
	}
	if !strings.Contains(err.Error(), "allowDiskResize") {
		t.Errorf("error should name the allowDiskResize opt-in; got: %v", err)
	}
	if ran {
		t.Error("no resize commands must run when the grow is rejected")
	}
	// The backing file must be untouched (not grown) when the resize is rejected.
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat baseline file: %v", err)
	}
	if fi.Size() != 100<<20 {
		t.Errorf("backing file size = %d, want %d (must not grow on rejection)", fi.Size(), 100<<20)
	}
}

func TestResizeBaseline_XFSUsesGrowfsByMount(t *testing.T) {
	origExec := resizeExec
	defer func() { resizeExec = origExec }()
	stubResizeToolsPresent(t)
	var cmds []string
	// Single-partition disk: the xfs root is trivially the last partition.
	resizeExec = func(cmd string) (string, error) {
		cmds = append(cmds, cmd)
		if strings.Contains(cmd, "lsblk") {
			return `{"blockdevices":[{"name":"loop0","path":"/dev/loop0","type":"loop","children":[
			  {"name":"loop0p1","path":"/dev/loop0p1","start":2048,"type":"part"}
			]}]}`, nil
		}
		return "", nil
	}

	p := writeSizedFile(t, 100<<20)
	tmpl := &config.ImageTemplate{
		Disk:          config.DiskConfig{Size: "200MiB"},
		OverlayPolicy: &config.OverlayPolicy{AllowDiskResize: true},
	}
	ctx := &Context{BaselineCopyPath: p, LoopDevPath: "/dev/loop0"}
	layout := &Layout{RootDevice: "/dev/loop0p1", RootFSType: "xfs", RootMount: "/mnt/root", PartitionTable: partitionTableDOS}

	if err := ResizeBaseline(tmpl, ctx, layout); err != nil {
		t.Fatalf("ResizeBaseline: %v", err)
	}
	joined := strings.Join(cmds, "\n")
	if !strings.Contains(joined, "xfs_growfs '/mnt/root'") {
		t.Errorf("xfs root must grow by mount point; got:\n%s", joined)
	}
	// MBR table: no sgdisk backup-header relocation.
	if strings.Contains(joined, "sgdisk") {
		t.Errorf("MBR resize must not run sgdisk; got:\n%s", joined)
	}
}

func TestSplitPartitionDevice(t *testing.T) {
	tests := []struct {
		dev      string
		wantDisk string
		wantPart string
		wantErr  bool
	}{
		{"/dev/loop0p2", "/dev/loop0", "2", false},
		{"/dev/loop12p3", "/dev/loop12", "3", false},
		{"/dev/nvme0n1p1", "/dev/nvme0n1", "1", false},
		{"/dev/mmcblk0p2", "/dev/mmcblk0", "2", false},
		{"/dev/sda2", "/dev/sda", "2", false},
		{"/dev/sdb15", "/dev/sdb", "15", false},
		{"/dev/sda", "", "", true}, // no partition number
		{"", "", "", true},
	}
	for _, tt := range tests {
		disk, part, err := splitPartitionDevice(tt.dev)
		if tt.wantErr {
			if err == nil {
				t.Errorf("splitPartitionDevice(%q): expected error", tt.dev)
			}
			continue
		}
		if err != nil {
			t.Errorf("splitPartitionDevice(%q): %v", tt.dev, err)
			continue
		}
		if disk != tt.wantDisk || part != tt.wantPart {
			t.Errorf("splitPartitionDevice(%q) = %q/%q, want %q/%q", tt.dev, disk, part, tt.wantDisk, tt.wantPart)
		}
	}
}

// A grow must be refused, with no disk mutation, when the root is not the last
// partition on the disk: growpart would otherwise extend it into a following
// partition and corrupt the table. This is the primary safety guard.
func TestResizeBaseline_RejectsNonLastRootPartition(t *testing.T) {
	origExec := resizeExec
	defer func() { resizeExec = origExec }()
	stubResizeToolsPresent(t)
	var cmds []string
	// Layout where the root (loop0p2) is followed by a later partition (loop0p3).
	resizeExec = func(cmd string) (string, error) {
		cmds = append(cmds, cmd)
		if strings.Contains(cmd, "lsblk") {
			return `{"blockdevices":[{"name":"loop0","path":"/dev/loop0","type":"loop","children":[
			  {"name":"loop0p1","path":"/dev/loop0p1","start":2048,"type":"part"},
			  {"name":"loop0p2","path":"/dev/loop0p2","start":1050624,"type":"part"},
			  {"name":"loop0p3","path":"/dev/loop0p3","start":9439232,"type":"part"}
			]}]}`, nil
		}
		return "", nil
	}

	p := writeSizedFile(t, 100<<20)
	tmpl := &config.ImageTemplate{
		Disk:          config.DiskConfig{Size: "200MiB"},
		OverlayPolicy: &config.OverlayPolicy{AllowDiskResize: true},
	}
	ctx := &Context{BaselineCopyPath: p, LoopDevPath: "/dev/loop0"}
	layout := &Layout{RootDevice: "/dev/loop0p2", RootFSType: "ext4", RootMount: "/mnt/root", PartitionTable: partitionTableGPT}

	err := ResizeBaseline(tmpl, ctx, layout)
	if err == nil {
		t.Fatal("expected rejection when root is not the last partition")
	}
	if !strings.Contains(err.Error(), "last partition") {
		t.Errorf("error should explain the last-partition requirement; got: %v", err)
	}
	// No mutating command may have run, and the backing file must be untouched.
	joined := strings.Join(cmds, "\n")
	for _, forbidden := range []string{"growpart", "sgdisk", "resize2fs", "losetup -c"} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("no mutation must run on rejection; saw %q in:\n%s", forbidden, joined)
		}
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat backing file: %v", err)
	}
	if fi.Size() != 100<<20 {
		t.Errorf("backing file size = %d, want %d (must not grow on rejection)", fi.Size(), 100<<20)
	}
}

// A required resize tool missing on the build host must abort the grow up front,
// before any disk mutation, with a message naming the missing tool.
func TestResizeBaseline_RejectsWhenToolMissing(t *testing.T) {
	origExec := resizeExec
	defer func() { resizeExec = origExec }()
	ran := false
	resizeExec = func(string) (string, error) { ran = true; return "", nil }

	origTool := resizeToolExists
	defer func() { resizeToolExists = origTool }()
	resizeToolExists = func(cmd string) (bool, error) { return cmd != "growpart", nil }

	p := writeSizedFile(t, 100<<20)
	tmpl := &config.ImageTemplate{
		Disk:          config.DiskConfig{Size: "200MiB"},
		OverlayPolicy: &config.OverlayPolicy{AllowDiskResize: true},
	}
	ctx := &Context{BaselineCopyPath: p, LoopDevPath: "/dev/loop0"}
	layout := &Layout{RootDevice: "/dev/loop0p2", RootFSType: "ext4", RootMount: "/mnt/root", PartitionTable: partitionTableGPT}

	err := ResizeBaseline(tmpl, ctx, layout)
	if err == nil {
		t.Fatal("expected rejection when a required resize tool is missing")
	}
	if !strings.Contains(err.Error(), "growpart") {
		t.Errorf("error should name the missing tool; got: %v", err)
	}
	if ran {
		t.Error("no resize command must run when a required tool is missing")
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat backing file: %v", err)
	}
	if fi.Size() != 100<<20 {
		t.Errorf("backing file size = %d, want %d (must not grow on rejection)", fi.Size(), 100<<20)
	}
}

func TestResizeToolsForFS(t *testing.T) {
	gptExt := resizeToolsForFS("ext4", partitionTableGPT)
	if !contains(gptExt, "sgdisk") || !contains(gptExt, "resize2fs") {
		t.Errorf("GPT ext4 tools = %v, want sgdisk + resize2fs", gptExt)
	}
	if contains(gptExt, "xfs_growfs") {
		t.Errorf("GPT ext4 tools must not include xfs_growfs: %v", gptExt)
	}
	mbrXFS := resizeToolsForFS("xfs", partitionTableDOS)
	if contains(mbrXFS, "sgdisk") {
		t.Errorf("MBR must not require sgdisk: %v", mbrXFS)
	}
	if !contains(mbrXFS, "xfs_growfs") {
		t.Errorf("xfs root must require xfs_growfs: %v", mbrXFS)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func TestParsePartitionStarts(t *testing.T) {
	js := `{"blockdevices":[{"name":"loop0","path":"/dev/loop0","type":"loop","start":null,"children":[
	  {"name":"loop0p1","path":"/dev/loop0p1","start":2048,"type":"part"},
	  {"name":"loop0p2","path":"/dev/loop0p2","start":"1050624","type":"part"}
	]}]}`
	starts, err := parsePartitionStarts(js)
	if err != nil {
		t.Fatalf("parsePartitionStarts: %v", err)
	}
	if len(starts) != 2 {
		t.Fatalf("got %d partitions, want 2: %+v", len(starts), starts)
	}
	if starts["/dev/loop0p1"] != 2048 {
		t.Errorf("p1 start = %d, want 2048", starts["/dev/loop0p1"])
	}
	// Quoted-string START (older lsblk) must parse too.
	if starts["/dev/loop0p2"] != 1050624 {
		t.Errorf("p2 start = %d, want 1050624", starts["/dev/loop0p2"])
	}
	// The whole-disk loop node (type "loop") must be excluded.
	if _, ok := starts["/dev/loop0"]; ok {
		t.Error("whole-disk node must not be counted as a partition")
	}
}

// A partition row whose START is missing/null/unparseable must fail closed: the
// guard cannot default it to 0 (which would make a later partition look like it
// precedes root and let an unsafe growpart through).
func TestParsePartitionStarts_FailsClosedOnMissingStart(t *testing.T) {
	cases := map[string]string{
		"null start": `{"blockdevices":[{"path":"/dev/loop0","type":"loop","children":[
		  {"path":"/dev/loop0p1","start":2048,"type":"part"},
		  {"path":"/dev/loop0p2","start":null,"type":"part"}
		]}]}`,
		"missing start key": `{"blockdevices":[{"path":"/dev/loop0","type":"loop","children":[
		  {"path":"/dev/loop0p1","start":2048,"type":"part"},
		  {"path":"/dev/loop0p2","type":"part"}
		]}]}`,
		"non-numeric start": `{"blockdevices":[{"path":"/dev/loop0","type":"loop","children":[
		  {"path":"/dev/loop0p1","start":"notanumber","type":"part"}
		]}]}`,
	}
	for name, js := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parsePartitionStarts(js); err == nil {
				t.Error("expected error for missing/unparseable partition START, got nil")
			}
		})
	}
}

func TestResizeBaseline_NilGuards(t *testing.T) {
	if err := ResizeBaseline(nil, &Context{}, &Layout{}); err == nil {
		t.Error("expected error for nil template")
	}
	if err := ResizeBaseline(&config.ImageTemplate{}, nil, &Layout{}); err == nil {
		t.Error("expected error for nil context")
	}
	if err := ResizeBaseline(&config.ImageTemplate{}, &Context{}, nil); err == nil {
		t.Error("expected error for nil layout")
	}
}
