package overlay

import (
	"math"
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
	plan, err := planResize(1<<20, 0, false)
	if err != nil {
		t.Fatalf("planResize: %v", err)
	}
	if plan.Grow {
		t.Errorf("zero (unset) target must not grow: %+v", plan)
	}
}

func TestPlanResize_EqualSkips(t *testing.T) {
	// 100 MiB current, target exactly 100 MiB: a legitimate no-op, not a shrink.
	plan, err := planResize(100<<20, 100<<20, false)
	if err != nil {
		t.Fatalf("planResize(100MiB): %v", err)
	}
	if plan.Grow {
		t.Errorf("grow-only: an equal target must not grow a 100MiB image: %+v", plan)
	}
	if plan.Reason == "" {
		t.Errorf("a skipped resize should carry a reason, got %+v", plan)
	}
}

// A target smaller than the current image is a shrink request. Overlay resize is
// grow-only, so it must be a hard error with an actionable "shrink not supported"
// message rather than a silent no-op (which would mislead the user into thinking
// the image was shrunk).
func TestPlanResize_SmallerErrorsAsShrinkUnsupported(t *testing.T) {
	_, err := planResize(100<<20, 50<<20, false)
	if err == nil {
		t.Fatal("expected an error when the requested size is smaller than the baseline")
	}
	if !strings.Contains(err.Error(), "shrink not supported") {
		t.Errorf("error should say 'shrink not supported'; got: %v", err)
	}
	if !strings.Contains(err.Error(), "grow-only") {
		t.Errorf("error should explain overlay resize is grow-only; got: %v", err)
	}
}

func TestPlanResize_LargerGrows(t *testing.T) {
	plan, err := planResize(100<<20, 200<<20, true)
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

// A grow that is required (target larger than baseline) but not opted into must be
// a hard error, not a silent resize — overlay preserves the baseline layout unless
// the template explicitly allows growing it.
func TestPlanResize_LargerWithoutOptInErrors(t *testing.T) {
	_, err := planResize(100<<20, 200<<20, false)
	if err == nil {
		t.Fatal("expected error when a grow is required but allowDiskResize is false")
	}
	if !strings.Contains(err.Error(), "allowDiskResize") {
		t.Errorf("error should name the allowDiskResize opt-in; got: %v", err)
	}
}

func TestResolveSizeBytes(t *testing.T) {
	if b, err := resolveSizeBytes("disk.size", ""); err != nil || b != 0 {
		t.Errorf("unset size = (%d, %v), want (0, nil)", b, err)
	}
	if b, err := resolveSizeBytes("disk.size", "200MiB"); err != nil || b != 200<<20 {
		t.Errorf("resolveSizeBytes(200MiB) = (%d, %v), want (%d, nil)", b, err, 200<<20)
	}
	if _, err := resolveSizeBytes("disk.size", "not-a-size"); err == nil {
		t.Fatal("expected error for an unparseable size")
	}
	// A size above math.MaxInt64 would wrap negative when narrowed to int64 and be
	// misread as "smaller than current", silently skipping the grow. Reject it.
	if _, err := resolveSizeBytes("disk.size", "10000000000GiB"); err == nil {
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

	if err := ResizeBaseline(tmpl, ctx, layout, nil); err != nil {
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
	// Equal target: a legitimate no-op (not a shrink), so no resize commands run.
	tmpl := &config.ImageTemplate{Disk: config.DiskConfig{Size: "100MiB"}}
	ctx := &Context{BaselineCopyPath: p, LoopDevPath: "/dev/loop0"}
	layout := &Layout{RootDevice: "/dev/loop0p2", RootFSType: "ext4", RootMount: "/mnt/root"}

	if err := ResizeBaseline(tmpl, ctx, layout, nil); err != nil {
		t.Fatalf("ResizeBaseline: %v", err)
	}
	if ran {
		t.Error("an equal-size resize must run no commands (no grow needed)")
	}
}

// A disk.size ceiling smaller than the baseline is an impossible constraint and
// must fail the resize up front, before any disk mutation.
func TestResizeBaseline_ShrinkErrorsAndRunsNothing(t *testing.T) {
	origExec := resizeExec
	defer func() { resizeExec = origExec }()
	ran := false
	resizeExec = func(string) (string, error) { ran = true; return "", nil }

	p := writeSizedFile(t, 100<<20)
	tmpl := &config.ImageTemplate{Disk: config.DiskConfig{Size: "50MiB"}} // smaller than baseline
	ctx := &Context{BaselineCopyPath: p, LoopDevPath: "/dev/loop0"}
	layout := &Layout{RootDevice: "/dev/loop0p2", RootFSType: "ext4", RootMount: "/mnt/root"}

	err := ResizeBaseline(tmpl, ctx, layout, nil)
	if err == nil {
		t.Fatal("expected a shrink to be rejected")
	}
	if !strings.Contains(err.Error(), "smaller than the baseline image") {
		t.Errorf("error should name the disk.size ceiling as smaller than the baseline; got: %v", err)
	}
	if ran {
		t.Error("a rejected shrink must run no commands (fail before any disk mutation)")
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

	err := ResizeBaseline(tmpl, ctx, layout, nil)
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

	if err := ResizeBaseline(tmpl, ctx, layout, nil); err != nil {
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

	err := ResizeBaseline(tmpl, ctx, layout, nil)
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

	err := ResizeBaseline(tmpl, ctx, layout, nil)
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
	if err := ResizeBaseline(nil, &Context{}, &Layout{}, nil); err == nil {
		t.Error("expected error for nil template")
	}
	if err := ResizeBaseline(&config.ImageTemplate{}, nil, &Layout{}, nil); err == nil {
		t.Error("expected error for nil context")
	}
	if err := ResizeBaseline(&config.ImageTemplate{}, &Context{}, nil, nil); err == nil {
		t.Error("expected error for nil layout")
	}
}

func TestRoundUpToMiB(t *testing.T) {
	const mib = int64(1 << 20)
	tests := []struct{ in, want int64 }{
		{0, 0},
		{-5, 0},
		{1, mib},
		{mib, mib},
		{mib + 1, 2 * mib},
		{3*mib - 1, 3 * mib},
	}
	for _, tt := range tests {
		if got := roundUpToMiB(tt.in); got != tt.want {
			t.Errorf("roundUpToMiB(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestParseDfAvail(t *testing.T) {
	// The default `df -B1 --output=avail` form: an "Avail" header then the byte count.
	if n, err := parseDfAvail("    Avail\n123456789\n"); err != nil || n != 123456789 {
		t.Errorf("parseDfAvail(header+number) = (%d, %v), want (123456789, nil)", n, err)
	}
	// A bare number (no header) must still parse.
	if n, err := parseDfAvail("42"); err != nil || n != 42 {
		t.Errorf("parseDfAvail(bare) = (%d, %v), want (42, nil)", n, err)
	}
	// No numeric field anywhere is an error, not a silent 0 (which would make the
	// estimate think the disk is full and over-grow, or empty and skip a needed grow).
	if _, err := parseDfAvail("Avail\n"); err == nil {
		t.Error("expected error when df output carries no numeric field")
	}
}

// stubOverlayFree overrides the free-space probe to report a fixed byte count and
// restores it when the test ends.
func stubOverlayFree(t *testing.T, free int64) {
	t.Helper()
	orig := overlayFreeBytesFn
	t.Cleanup(func() { overlayFreeBytesFn = orig })
	overlayFreeBytesFn = func(string) (int64, error) { return free, nil }
}

func planWithSizes(sizes ...int64) *ResolutionPlan {
	pkgs := make([]ResolvedPackage, len(sizes))
	for i, s := range sizes {
		// 0 stands in for "unknown" throughout these tests (no HasInstalledSize),
		// matching every other size's HasInstalledSize=true.
		pkgs[i] = ResolvedPackage{Name: "p", InstalledSizeBytes: s, HasInstalledSize: s > 0}
	}
	return &ResolutionPlan{ToInstall: pkgs}
}

// planWithKnownSizes builds a plan where every package has HasInstalledSize=true,
// even one whose reported size is a confirmed real zero (distinct from
// planWithSizes, where a 0 entry means "unknown").
func planWithKnownSizes(sizes ...int64) *ResolutionPlan {
	pkgs := make([]ResolvedPackage, len(sizes))
	for i, s := range sizes {
		pkgs[i] = ResolvedPackage{Name: "p", InstalledSizeBytes: s, HasInstalledSize: true}
	}
	return &ResolutionPlan{ToInstall: pkgs}
}

func TestComputeOverlayTarget_NoMetadataFallsBackToCeiling(t *testing.T) {
	// Auto-size, no per-package sizes: fall back to the disk.size ceiling.
	got, err := computeOverlayTarget(planWithSizes(0, 0), 100<<20, "/mnt/root", "300MiB")
	if err != nil {
		t.Fatalf("computeOverlayTarget: %v", err)
	}
	if got != 300<<20 {
		t.Errorf("no-metadata target = %d, want ceiling %d", got, 300<<20)
	}
	// And 0 (no resize) when disk.size is also unset.
	if got, err := computeOverlayTarget(nil, 100<<20, "/mnt/root", ""); err != nil || got != 0 {
		t.Errorf("no-metadata, no-ceiling = (%d, %v), want (0, nil)", got, err)
	}
}

func TestComputeOverlayTarget_EnoughFreeSpaceNoGrow(t *testing.T) {
	// Plenty of free space for the estimated install → target stays at current.
	stubOverlayFree(t, 10<<30)
	got, err := computeOverlayTarget(planWithSizes(100<<20), 1<<30, "/mnt/root", "8GiB")
	if err != nil {
		t.Fatalf("computeOverlayTarget: %v", err)
	}
	if got != 1<<30 {
		t.Errorf("with ample free space target = %d, want current %d (no grow)", got, 1<<30)
	}
}

func TestComputeOverlayTarget_ShortfallGrows(t *testing.T) {
	// 500 MiB of packages, ~0 free: required = 500MiB*1.30 + 512MiB margin = 1162 MiB,
	// shortfall ~= required (free 0), target = current + roundUp(shortfall).
	stubOverlayFree(t, 0)
	current := int64(1 << 30) // 1 GiB
	sum := int64(500 << 20)
	got, err := computeOverlayTarget(planWithSizes(sum), current, "/mnt/root", "")
	if err != nil {
		t.Fatalf("computeOverlayTarget: %v", err)
	}
	wantRequired := sum*overlayInstallOverheadNum/overlayInstallOverheadDen + overlayInstallMarginBytes
	want := current + roundUpToMiB(wantRequired)
	if got != want {
		t.Errorf("shortfall target = %d, want %d", got, want)
	}
	if got <= current {
		t.Errorf("target %d must exceed current %d when there is a shortfall", got, current)
	}
}

func TestComputeOverlayTarget_CapsAtCeiling(t *testing.T) {
	// A large install with no free space would need far more than the ceiling; the
	// target must be capped at disk.size (and may then be insufficient — that is
	// the user's declared cap).
	stubOverlayFree(t, 0)
	current := int64(1 << 30)
	got, err := computeOverlayTarget(planWithSizes(5<<30), current, "/mnt/root", "1200MiB")
	if err != nil {
		t.Fatalf("computeOverlayTarget: %v", err)
	}
	if got != 1200<<20 {
		t.Errorf("capped target = %d, want ceiling %d", got, 1200<<20)
	}
}

func TestComputeOverlayTarget_InvalidSize(t *testing.T) {
	if _, err := computeOverlayTarget(planWithSizes(1<<20), 1<<20, "/mnt/root", "not-a-size"); err == nil {
		t.Fatal("expected error for an unparseable disk.size ceiling")
	}
}

// A disk.size ceiling smaller than the baseline is an impossible constraint and
// must be rejected up front, regardless of whether package metadata is present.
func TestComputeOverlayTarget_CeilingBelowBaselineRejected(t *testing.T) {
	_, err := computeOverlayTarget(nil, 200<<20, "/mnt/root", "100MiB")
	if err == nil {
		t.Fatal("expected error for a disk.size ceiling smaller than the baseline")
	}
	if !strings.Contains(err.Error(), "smaller than the baseline image") {
		t.Errorf("error should name the baseline mismatch; got: %v", err)
	}
}

// disk.size: "0MiB" is an explicitly configured (if useless) ceiling, distinct
// from leaving disk.size unset — it must be rejected the same as any other
// ceiling smaller than the baseline, not silently treated as "no ceiling".
func TestComputeOverlayTarget_ExplicitZeroCeilingRejected(t *testing.T) {
	if _, err := computeOverlayTarget(nil, 200<<20, "/mnt/root", "0MiB"); err == nil {
		t.Fatal("expected error for an explicit disk.size: 0MiB smaller than the baseline")
	}
	// An unset (empty) ceiling is not the same thing: no metadata + no ceiling is a
	// legitimate no-resize, not an error.
	got, err := computeOverlayTarget(nil, 200<<20, "/mnt/root", "")
	if err != nil || got != 0 {
		t.Errorf("unset ceiling = (%d, %v), want (0, nil)", got, err)
	}
}

// Summing resolved packages' installed sizes must fail closed on overflow rather
// than silently wrap negative and misread as "nothing to install".
func TestComputeOverlayTarget_SumOverflowRejected(t *testing.T) {
	if _, err := computeOverlayTarget(planWithSizes(math.MaxInt64, 1), 1<<20, "/mnt/root", ""); err == nil {
		t.Fatal("expected error when summing package sizes overflows int64")
	}
}

// Scaling the summed installed size by the overhead factor must also fail closed
// on overflow.
func TestComputeOverlayTarget_ScaleOverflowRejected(t *testing.T) {
	if _, err := computeOverlayTarget(planWithSizes(math.MaxInt64), 1<<20, "/mnt/root", ""); err == nil {
		t.Fatal("expected error when scaling the estimated install size overflows int64")
	}
}

// The overhead scaling must round up (ceiling division), not truncate, so the
// estimate never under-shoots the real conservative-sizing intent.
func TestEstimateRequiredBytes_RoundsUp(t *testing.T) {
	// 7 * 130 / 100 = 9.1, truncating division would give 9; ceiling gives 10.
	got, err := estimateRequiredBytes(7)
	if err != nil {
		t.Fatalf("estimateRequiredBytes: %v", err)
	}
	want := int64(10) + overlayInstallMarginBytes
	if got != want {
		t.Errorf("estimateRequiredBytes(7) = %d, want %d (ceiling division)", got, want)
	}
}

// When some (but not all) packages report a size, summing only the known ones
// understates the real need. With no disk.size ceiling to fall back on, that
// incomplete estimate must fail closed rather than silently under-provision.
func TestComputeOverlayTarget_PartialUnknownSizeNoCeilingErrors(t *testing.T) {
	if _, err := computeOverlayTarget(planWithSizes(500<<20, 0), 1<<30, "/mnt/root", ""); err == nil {
		t.Fatal("expected error when some packages report no installed-size metadata and no ceiling is set")
	}
}

// The same partial-metadata case, but with a disk.size ceiling set: grow straight
// to the ceiling (the safer, user-declared cap) instead of the incomplete
// estimate, and without even measuring free space.
func TestComputeOverlayTarget_PartialUnknownSizeFallsBackToCeiling(t *testing.T) {
	orig := overlayFreeBytesFn
	t.Cleanup(func() { overlayFreeBytesFn = orig })
	overlayFreeBytesFn = func(string) (int64, error) {
		t.Fatal("partial-metadata fallback to the ceiling must not measure free space")
		return 0, nil
	}
	got, err := computeOverlayTarget(planWithSizes(500<<20, 0), 1<<30, "/mnt/root", "2GiB")
	if err != nil {
		t.Fatalf("computeOverlayTarget: %v", err)
	}
	if got != 2<<30 {
		t.Errorf("partial-metadata target = %d, want ceiling %d", got, 2<<30)
	}
}

// A resolved plan known to have nothing left to install (e.g. every requested
// package is already present) must never grow the image, even with a disk.size
// ceiling set — the ceiling caps an auto-sized grow, it does not itself request
// one. Distinct from a nil plan (no information), which still falls back to the
// ceiling.
func TestComputeOverlayTarget_EmptyPlanNeverGrows(t *testing.T) {
	orig := overlayFreeBytesFn
	t.Cleanup(func() { overlayFreeBytesFn = orig })
	overlayFreeBytesFn = func(string) (int64, error) {
		t.Fatal("an empty plan must not measure free space")
		return 0, nil
	}
	current := int64(1 << 30)
	got, err := computeOverlayTarget(&ResolutionPlan{}, current, "/mnt/root", "8GiB")
	if err != nil {
		t.Fatalf("computeOverlayTarget: %v", err)
	}
	if got != current {
		t.Errorf("empty-plan target = %d, want current %d (no grow)", got, current)
	}
}

// A package with a repository-confirmed zero installed size (HasInstalledSize
// true, InstalledSizeBytes 0) is a complete data point, not "unknown" — it must
// not trigger the no-metadata ceiling fallback, and free space is still measured
// like any other complete estimate (just against a margin-only requirement).
func TestComputeOverlayTarget_ConfirmedZeroSizeIsNotUnknown(t *testing.T) {
	stubOverlayFree(t, 10<<30)
	current := int64(1 << 30)
	got, err := computeOverlayTarget(planWithKnownSizes(0), current, "/mnt/root", "")
	if err != nil {
		t.Fatalf("computeOverlayTarget: %v", err)
	}
	if got != current {
		t.Errorf("confirmed-zero package with ample free space = %d, want current %d (no grow)", got, current)
	}
}

// A mix of a confirmed zero and a normally-sized package must sum to just the
// non-zero package's contribution and proceed normally — not fall back to the
// ceiling or fail as incomplete metadata, since the zero entry is known, not
// unknown.
func TestComputeOverlayTarget_ConfirmedZeroDoesNotTriggerIncompleteFallback(t *testing.T) {
	stubOverlayFree(t, 0)
	if _, err := computeOverlayTarget(planWithKnownSizes(500<<20, 0), 1<<30, "/mnt/root", ""); err != nil {
		t.Errorf("computeOverlayTarget: %v (a confirmed-zero package must not be treated as incomplete metadata)", err)
	}
}
