package overlay

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"unicode"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/image/imagedisc"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

// Disk-space estimation constants for the auto-sized overlay grow. The estimate is
// deliberately conservative: it is cheaper to over-provision (the disk.size ceiling,
// when set, still caps it) than to under-provision and fail the install with ENOSPC.
const (
	// overlayInstallOverheadNum/Den scale the summed Installed-Size to account for
	// per-file 4 KiB-cluster rounding (the ext4 block size the create-mode maker
	// uses) plus filesystem metadata (journal, inode tables, reserved blocks). The
	// package metadata reports only per-package totals, so exact rounding is not
	// derivable — a flat 1.30× factor approximates it.
	overlayInstallOverheadNum = 130
	overlayInstallOverheadDen = 100
	// overlayInstallMarginBytes is a fixed floor added on top of the scaled install
	// size to cover space the package manifests do not reflect: initramfs
	// regeneration (tens of MiB per kernel), apt/dpkg or rpm database growth, the
	// regenerated GRUB config, and package-manager caches.
	overlayInstallMarginBytes = 512 << 20 // 512 MiB
	// mibBytes is one MiB, used to round a computed grow target up to a whole MiB.
	mibBytes = int64(1 << 20)
)

// overlayFreeBytesFn reports the bytes currently available on the mounted baseline
// root filesystem. It is a package var so tests can stub it deterministically
// (mirroring the resizeExec / resizeToolExists seams). The default shells out to df
// through resizeExec — consistent with the rest of this file, which shells out for
// every disk operation — and reports the space available to an unprivileged user
// (df's Avail column excludes root-reserved blocks), biasing the estimate toward
// growing rather than under-provisioning.
var overlayFreeBytesFn = func(rootMount string) (int64, error) {
	out, err := resizeExec(fmt.Sprintf("df -B1 --output=avail %s", shell.QuoteArg(rootMount)))
	if err != nil {
		return 0, err
	}
	return parseDfAvail(out)
}

// parseDfAvail extracts the available-bytes number from `df -B1 --output=avail`
// output, which is a header line ("Avail") followed by the byte count. It returns
// the last field that parses as a non-negative integer, tolerating the leading
// header and any surrounding whitespace.
func parseDfAvail(out string) (int64, error) {
	fields := strings.Fields(out)
	for i := len(fields) - 1; i >= 0; i-- {
		if n, err := strconv.ParseInt(fields[i], 10, 64); err == nil && n >= 0 {
			return n, nil
		}
	}
	return 0, fmt.Errorf("overlay resize: could not parse available space from df output %q", out)
}

// roundUpToMiB rounds n up to the next whole MiB (partition/filesystem grows align
// naturally to large boundaries; a MiB-granular target avoids sub-block churn). A
// non-positive n rounds to 0.
func roundUpToMiB(n int64) int64 {
	if n <= 0 {
		return 0
	}
	return ((n + mibBytes - 1) / mibBytes) * mibBytes
}

// resizePlan is the deterministic, side-effect-free decision of whether and how
// far to grow the baseline image. It is the unit-tested core of the resize stage.
type resizePlan struct {
	// Grow is true when the target size is strictly larger than the current image.
	Grow bool
	// CurrentBytes is the current backing-file size.
	CurrentBytes int64
	// TargetBytes is the requested size; meaningful only when Grow is true.
	TargetBytes int64
	// Reason explains a skipped resize (for logging), empty when Grow is true.
	Reason string
}

// resizeExec is the indirection over the impure resize commands so the
// orchestration is unit-testable without a real loop device or filesystem. Each
// call runs one allowlisted command on the host and returns its output. Tests
// override it to record the command sequence.
var resizeExec = func(cmd string) (string, error) {
	return shell.ExecCmd(cmd, true, shell.HostPath, nil)
}

// resizeToolExists is the indirection over the host tool-availability probe so
// the pre-flight check is unit-testable without depending on which tools happen
// to be installed on the machine running the tests. Tests override it.
var resizeToolExists = func(cmd string) (bool, error) {
	return shell.IsCommandExist(cmd, shell.HostPath)
}

// ResizeBaseline performs an optional, GROW-ONLY resize of the overlaid baseline
// so it has room for the packages being installed. The target is auto-sized from
// the resolved packages' installed-size metadata measured against the baseline's
// real free space; disk.size, when set, is a ceiling on that auto-sized grow, not
// an exact target — the final image may end up smaller than disk.size. It never
// shrinks: an unset or computed target no larger than the baseline is a no-op,
// while a configured disk.size ceiling smaller than the baseline is rejected up
// front as an invalid configuration, before any grow is planned. It grows the
// existing root partition and filesystem in place — it never repartitions or
// relocates the bootloader, preserving the overlay immutability contract.
//
// The sequence, when a grow is needed, is: extend the backing file, refresh the
// loop device capacity, fix the GPT backup header, re-read the partition table,
// grow the root partition, then grow its filesystem. ext{2,3,4} and xfs roots are
// supported (matching the layouts the inspector accepts).
func ResizeBaseline(template *config.ImageTemplate, ctx *Context, layout *Layout, resolvePlan *ResolutionPlan) error {
	if template == nil || ctx == nil || layout == nil {
		return fmt.Errorf("overlay resize: template, context, and layout are required")
	}

	fi, err := os.Stat(ctx.BaselineCopyPath)
	if err != nil {
		return fmt.Errorf("overlay resize: failed to stat baseline copy %s: %w", ctx.BaselineCopyPath, err)
	}
	current := fi.Size()

	dc := template.GetDiskConfig()
	sizeCeiling := strings.TrimSpace(dc.Size)
	allowResize := template.OverlayPolicy != nil && template.OverlayPolicy.AllowDiskResize

	// Derive the effective target: the space the resolved package set needs
	// (estimated from repo metadata, measured against the baseline's real free
	// space), capped at disk.size when it is set.
	targetBytes, err := computeOverlayTarget(resolvePlan, current, layout.RootMount, sizeCeiling)
	if err != nil {
		return err
	}

	rp, err := planResize(current, targetBytes, allowResize)
	if err != nil {
		return err
	}
	if !rp.Grow {
		log.Infof("Overlay resize: skipping (%s)", rp.Reason)
		return nil
	}

	disk, partNum, err := splitPartitionDevice(layout.RootDevice)
	if err != nil {
		return err
	}

	// Pre-flight: confirm every host tool the resize sequence shells out to is
	// present before mutating anything, so a missing tool fails cleanly up front
	// instead of half-way through (e.g. after the backing file has already been
	// grown). Checked here rather than in planResize so a no-op build never
	// requires the resize toolchain.
	if err := checkResizeToolsAvailable(layout); err != nil {
		return err
	}

	// Primary safety guard: growpart only extends a partition into free space that
	// immediately follows it, so the root partition MUST be the last partition on
	// the disk. Growing a non-last root would extend it into space owned by a
	// following partition and corrupt the table. Reject before any mutation.
	if err := assertRootIsLastPartition(ctx.LoopDevPath, layout.RootDevice); err != nil {
		return err
	}

	log.Infof("Overlay resize: growing image from %d to %d bytes (root %s on %s part %s)",
		rp.CurrentBytes, rp.TargetBytes, layout.RootDevice, disk, partNum)

	// 1. Extend the backing file to the target size, in-process rather than via a
	//    shell `truncate` (avoids shell parsing of the workspace path and drops a
	//    host-tool dependency for a trivial file op). This is grow-only: os.Truncate
	//    never shrinks here because planResize already guaranteed target > current.
	//    The copy is user-owned, so no sudo is needed.
	if err := os.Truncate(ctx.BaselineCopyPath, rp.TargetBytes); err != nil {
		return fmt.Errorf("overlay resize: failed to grow backing file: %w", err)
	}

	// 2. Tell the loop device its backing file grew.
	if _, err := resizeExec(fmt.Sprintf("losetup -c %s", shell.QuoteArg(ctx.LoopDevPath))); err != nil {
		return fmt.Errorf("overlay resize: failed to refresh loop device capacity: %w", err)
	}

	// 3. On GPT, move the backup header to the new end of disk so the table spans
	//    the grown device; harmless to skip on MBR.
	if layout.PartitionTable == partitionTableGPT {
		if _, err := resizeExec(fmt.Sprintf("sgdisk -e %s", shell.QuoteArg(ctx.LoopDevPath))); err != nil {
			return fmt.Errorf("overlay resize: failed to relocate GPT backup header: %w", err)
		}
	}

	// 4. Re-read the (now larger) partition table on the loop device.
	if _, err := resizeExec(fmt.Sprintf("partx -u %s", shell.QuoteArg(ctx.LoopDevPath))); err != nil {
		return fmt.Errorf("overlay resize: failed to re-read partition table: %w", err)
	}

	// 5. Grow the root partition to fill the freed space.
	if _, err := resizeExec(fmt.Sprintf("growpart %s %s", shell.QuoteArg(disk), shell.QuoteArg(partNum))); err != nil {
		return fmt.Errorf("overlay resize: failed to grow root partition: %w", err)
	}
	if _, err := resizeExec(fmt.Sprintf("partx -u %s", shell.QuoteArg(ctx.LoopDevPath))); err != nil {
		return fmt.Errorf("overlay resize: failed to re-read partition table after growpart: %w", err)
	}

	// 6. Grow the filesystem to fill the enlarged partition.
	if err := growFilesystem(layout); err != nil {
		return err
	}

	log.Infof("Overlay resize: grew root filesystem to fill %d bytes", rp.TargetBytes)
	return nil
}

// planResize decides whether to grow a baseline image of size `current` to
// `targetBytes`. It is a pure, grow-only decision: a zero (unset) or
// exactly-equal target yields Grow=false with a reason; a smaller (nonzero)
// target is rejected as an unsupported shrink; a larger target requires the
// opt-in below. The byte target is computed by the caller (computeOverlayTarget)
// so this stays free of I/O and unit-testable directly.
//
// allowResize is the explicit opt-in (overlayPolicy.allowDiskResize). When a grow
// would be required but the caller has not opted in, planResize returns an error so
// the build fails with a clear message rather than silently changing the baseline
// partition layout. A target that is unset or exactly equal to the current image
// never needs the opt-in: it is a no-op regardless. A smaller target is rejected
// as an unsupported shrink regardless of the opt-in too, since resize is
// grow-only.
func planResize(current, targetBytes int64, allowResize bool) (resizePlan, error) {
	if targetBytes == 0 {
		return resizePlan{CurrentBytes: current, Reason: "no disk.size requested and no package sizes to compute one"}, nil
	}

	// A target smaller than the current image is a shrink request. Overlay resize
	// is grow-only — shrinking a disk image safely requires shrinking the
	// filesystem and partition first, which overlay mode does not do — so reject it
	// with an actionable error rather than silently ignoring the smaller size (which
	// would leave the user believing the image was shrunk). This also fires when a
	// disk.size ceiling is set below the baseline size.
	if targetBytes < current {
		return resizePlan{}, fmt.Errorf(
			"overlay resize: shrink not supported: requested/computed size %d bytes is smaller than the "+
				"current baseline image (%d bytes); overlay resize is grow-only. Remove or raise disk.size to "+
				"at least the baseline size to proceed",
			targetBytes, current)
	}

	// An equal target needs no resize: it is a legitimate no-op, not a shrink.
	if targetBytes == current {
		return resizePlan{
			CurrentBytes: current,
			Reason:       fmt.Sprintf("requested size %d == current size %d (no resize needed)", targetBytes, current),
		}, nil
	}

	// A grow is required. Overlay mode preserves the baseline layout unless the
	// user has explicitly opted in, so reject rather than resize behind their back.
	if !allowResize {
		return resizePlan{}, fmt.Errorf(
			"overlay resize: computed/requested size %d bytes is larger than the baseline image (%d bytes), "+
				"but growing the baseline is not permitted; set overlayPolicy.allowDiskResize: true "+
				"to allow the overlay to grow the disk, or reduce the packages being installed to fit within "+
				"the current baseline size",
			targetBytes, current)
	}

	return resizePlan{
		Grow:         true,
		CurrentBytes: current,
		TargetBytes:  targetBytes,
	}, nil
}

// resolveSizeBytes parses a disk size string (disk.size) into bytes, returning 0
// when it is unset. Parsing is delegated to the shared imagedisc translator so
// units match the rest of the tool ("4GiB", "8GB", ...). A value above
// math.MaxInt64 is rejected: narrowed to the int64 used for file sizes it would
// wrap negative and be misread as a shrink, silently skipping a requested grow.
// field names the value ("disk.size") for error messages.
func resolveSizeBytes(field, sizeStr string) (int64, error) {
	if strings.TrimSpace(sizeStr) == "" {
		return 0, nil
	}
	b, err := imagedisc.TranslateSizeStrToBytes(sizeStr)
	if err != nil {
		return 0, fmt.Errorf("overlay resize: invalid %s %q: %w", field, sizeStr, err)
	}
	if b > math.MaxInt64 {
		return 0, fmt.Errorf("overlay resize: requested %s %q (%d bytes) is too large", field, sizeStr, b)
	}
	return int64(b), nil
}

// sumInstalledSizes sums the resolved packages' installed sizes, checked for
// int64 overflow, and reports how many packages (if any) had no usable size —
// distinguishing a complete estimate from a partial one for the caller. A
// package's HasInstalledSize flag, not InstalledSizeBytes == 0, decides whether
// its size is known: the metadata may legitimately report a real zero footprint,
// which is not the same as not reporting a size at all. total is 0 for a nil
// plan, letting the caller tell "no information" from "confirmed zero".
func sumInstalledSizes(resolvePlan *ResolutionPlan) (sum int64, unknownCount, total int, err error) {
	if resolvePlan == nil {
		return 0, 0, 0, nil
	}
	total = len(resolvePlan.ToInstall)
	for i := range resolvePlan.ToInstall {
		pkg := resolvePlan.ToInstall[i]
		if !pkg.HasInstalledSize || pkg.InstalledSizeBytes < 0 {
			unknownCount++
			continue
		}
		s := pkg.InstalledSizeBytes
		if sum > math.MaxInt64-s {
			return 0, 0, 0, fmt.Errorf("overlay resize: sum of resolved packages' installed sizes overflows int64; " +
				"repository package metadata reports an implausibly large size")
		}
		sum += s
	}
	return sum, unknownCount, total, nil
}

// fallbackForIncompleteMetadata handles a resolved plan where only some packages
// report an installed size: summing just the known ones would understate the
// real need, so it is never treated as a complete estimate. It falls back to the
// disk.size ceiling (the safer, user-declared cap) when one is set, or fails
// closed when not. applies is true when the caller should return (target, err)
// immediately without computing an estimate.
func fallbackForIncompleteMetadata(unknownCount, total int, ceilingSet bool, ceilingBytes int64) (target int64, applies bool, err error) {
	if unknownCount == 0 {
		return 0, false, nil
	}
	if !ceilingSet {
		return 0, true, fmt.Errorf(
			"overlay resize: %d of %d packages being installed report no installed-size metadata; "+
				"the auto-size estimate would be incomplete and could under-provision the grow; set disk.size "+
				"as a ceiling to grow to a safe fixed cap instead", unknownCount, total)
	}
	log.Warnf("Overlay resize: %d of %d packages being installed report no installed-size metadata; "+
		"the estimate would be incomplete, so growing straight to the disk.size ceiling (%d bytes) instead",
		unknownCount, total, ceilingBytes)
	return ceilingBytes, true, nil
}

// estimateRequiredBytes scales the summed installed size by the conservative
// overhead factor and adds the fixed margin, checked for int64 overflow. The
// scaling uses ceiling (round-up) division so the estimate never under-shoots
// due to integer truncation, keeping it conservative as intended.
func estimateRequiredBytes(sumInstalled int64) (int64, error) {
	// The guard covers both the ×num multiplication and the +(den-1) rounding
	// term added below, so the ceiling division itself cannot overflow.
	if sumInstalled > (math.MaxInt64-(overlayInstallOverheadDen-1))/overlayInstallOverheadNum {
		return 0, fmt.Errorf("overlay resize: estimated install size overflows int64 scaling by the overhead factor; " +
			"repository package metadata reports an implausibly large size")
	}
	scaled := (sumInstalled*overlayInstallOverheadNum + (overlayInstallOverheadDen - 1)) / overlayInstallOverheadDen
	if scaled > math.MaxInt64-overlayInstallMarginBytes {
		return 0, fmt.Errorf("overlay resize: estimated install size overflows int64 after adding the margin; " +
			"repository package metadata reports an implausibly large size")
	}
	return scaled + overlayInstallMarginBytes, nil
}

// growTargetBytes rounds shortfall up to a whole MiB and adds it to current,
// checked for int64 overflow at each step.
func growTargetBytes(current, shortfall int64) (int64, error) {
	if shortfall > math.MaxInt64-(mibBytes-1) {
		return 0, fmt.Errorf("overlay resize: computed shortfall overflows int64 rounding up to a MiB boundary; " +
			"repository package metadata reports an implausibly large size")
	}
	rounded := roundUpToMiB(shortfall)
	if current > math.MaxInt64-rounded {
		return 0, fmt.Errorf("overlay resize: computed grow target overflows int64; " +
			"repository package metadata reports an implausibly large size")
	}
	return current + rounded, nil
}

// capAtCeiling caps target at ceilingBytes when a ceiling is set and target
// exceeds it, warning that the install may then run out of room.
func capAtCeiling(target, ceilingBytes int64, ceilingSet bool) int64 {
	if !ceilingSet || target <= ceilingBytes {
		return target
	}
	log.Warnf("Overlay resize: computed disk need (%d bytes) exceeds the disk.size ceiling (%d bytes); "+
		"capping at the ceiling — the package install may fail with 'no space left on device'", target, ceilingBytes)
	return ceilingBytes
}

// computeOverlayTarget derives the backing-file size the overlay should grow to:
// it estimates the space the packages being installed need (summed
// Installed-Size × overhead + a fixed margin), measures the baseline's real free
// space, and returns current + the rounded-up shortfall, capped at disk.size when
// it is set (disk.size is a ceiling, not an exact target, in overlay mode).
// Returns `current` (a planResize no-op) when the baseline already has room, or
// when a resolved plan has nothing left to install — a ceiling caps a grow, it
// never requests one on its own. Falls back to the disk.size ceiling (or 0 = no
// resize) when no package in the plan has a known size at all (distinct from
// every package confirming a real zero footprint, which is a complete estimate
// and proceeds normally). When only some packages report a size, the partial sum
// would understate the real need, so it falls back to the ceiling (if set) or
// fails closed (if not) rather than auto-sizing from an incomplete estimate. All
// arithmetic on repo-reported package sizes is checked for int64 overflow and
// fails closed rather than silently wrapping negative.
func computeOverlayTarget(resolvePlan *ResolutionPlan, current int64, rootMount, sizeCeiling string) (int64, error) {
	ceilingBytes, err := resolveSizeBytes("disk.size", sizeCeiling)
	if err != nil {
		return 0, err
	}
	// A size string is distinct from an unset one, so an explicit "disk.size: 0"
	// is treated as a real (if useless) ceiling rather than "no ceiling".
	ceilingSet := strings.TrimSpace(sizeCeiling) != ""

	// A configured ceiling smaller than the baseline is an impossible constraint:
	// reject it here, before any of the branches below, rather than risk one of
	// them silently returning `current` (no resize) and masking the mistake.
	if ceilingSet && ceilingBytes < current {
		return 0, fmt.Errorf(
			"overlay resize: disk.size ceiling (%d bytes) is smaller than the baseline image (%d bytes); "+
				"remove or raise disk.size, or leave it unset to auto-size with no cap", ceilingBytes, current)
	}

	// A resolved plan with nothing to install (e.g. every requested package is
	// already present) needs no headroom at all — a configured ceiling caps an
	// auto-sized grow, it does not by itself request one. A nil plan is distinct:
	// it means no information, not "known to be empty", so it still falls through
	// to the no-metadata ceiling fallback below.
	if resolvePlan != nil && len(resolvePlan.ToInstall) == 0 {
		return current, nil
	}

	sumInstalled, unknownCount, total, err := sumInstalledSizes(resolvePlan)
	if err != nil {
		return 0, err
	}

	// No package in the plan reports a usable size at all (including a nil plan,
	// total == 0): fall back to the disk.size ceiling (grow to the declared cap).
	// Returns 0 when disk.size is also unset, which planResize treats as "no
	// resize". A plan whose packages are all confirmed zero-footprint (total > 0,
	// unknownCount == 0, sumInstalled == 0) is a complete estimate, not this case,
	// and falls through to the normal margin-only estimate below.
	if unknownCount == total {
		return ceilingBytes, nil
	}

	if target, applies, err := fallbackForIncompleteMetadata(unknownCount, total, ceilingSet, ceilingBytes); applies {
		return target, err
	}

	required, err := estimateRequiredBytes(sumInstalled)
	if err != nil {
		return 0, err
	}

	free, err := overlayFreeBytesFn(rootMount)
	if err != nil {
		return 0, fmt.Errorf("overlay resize: failed to measure free space on %s: %w", rootMount, err)
	}

	shortfall := required - free
	if shortfall <= 0 {
		log.Infof("Overlay resize: baseline has %d bytes free; estimated install needs %d — no grow required", free, required)
		return current, nil
	}

	target, err := growTargetBytes(current, shortfall)
	if err != nil {
		return 0, err
	}
	target = capAtCeiling(target, ceilingBytes, ceilingSet)
	log.Infof("Overlay resize: estimated install %d bytes (incl. overhead), %d free; growing image to %d bytes",
		required, free, target)
	return target, nil
}

// growFilesystem grows the root filesystem in place to fill its (already enlarged)
// partition, dispatching on the detected filesystem type. ext{2,3,4} is grown by
// device with resize2fs; xfs is grown by mount point with xfs_growfs.
func growFilesystem(layout *Layout) error {
	switch layout.RootFSType {
	case "ext2", "ext3", "ext4":
		if _, err := resizeExec(fmt.Sprintf("resize2fs %s", shell.QuoteArg(layout.RootDevice))); err != nil {
			return fmt.Errorf("overlay resize: resize2fs on %s failed: %w", layout.RootDevice, err)
		}
	case "xfs":
		if _, err := resizeExec(fmt.Sprintf("xfs_growfs %s", shell.QuoteArg(layout.RootMount))); err != nil {
			return fmt.Errorf("overlay resize: xfs_growfs on %s failed: %w", layout.RootMount, err)
		}
	default:
		return fmt.Errorf("overlay resize: unsupported root filesystem %q for grow", layout.RootFSType)
	}
	return nil
}

// resizeToolsForFS returns the host commands the resize sequence shells out to
// for the given root filesystem type. losetup/partx/growpart are always needed;
// sgdisk is only used on GPT; resize2fs vs xfs_growfs depends on the FS. The
// backing-file grow uses os.Truncate (no tool), so it is intentionally omitted.
func resizeToolsForFS(fsType, table string) []string {
	tools := []string{"losetup", "partx", "growpart"}
	if table == partitionTableGPT {
		tools = append(tools, "sgdisk")
	}
	switch fsType {
	case "ext2", "ext3", "ext4":
		tools = append(tools, "resize2fs")
	case "xfs":
		tools = append(tools, "xfs_growfs")
	}
	return tools
}

// checkResizeToolsAvailable verifies every host tool the resize sequence needs is
// present on the build host before any disk mutation, so a missing dependency
// fails with a clear, actionable message up front rather than mid-sequence. The
// partition-inspection guard also relies on lsblk, so it is included.
func checkResizeToolsAvailable(layout *Layout) error {
	tools := append([]string{"lsblk"}, resizeToolsForFS(layout.RootFSType, layout.PartitionTable)...)
	var missing []string
	for _, t := range tools {
		ok, err := resizeToolExists(t)
		if err != nil {
			return fmt.Errorf("overlay resize: failed to probe for required tool %q: %w", t, err)
		}
		if !ok {
			missing = append(missing, t)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf(
			"overlay resize: required tool(s) not found on the build host: %s; "+
				"install them (growpart is in cloud-guest-utils, sgdisk in gdisk, "+
				"resize2fs in e2fsprogs, xfs_growfs in xfsprogs, losetup/partx in util-linux), "+
				"or reduce the packages being installed (or use a baseline with enough free space) "+
				"so the grow is not needed",
			strings.Join(missing, ", "))
	}
	return nil
}

// assertRootIsLastPartition rejects a resize when the root partition is not the
// last partition (by on-disk start offset) on the loop device. growpart extends
// a partition only into the free space that immediately follows it, so growing a
// non-last root would run into — and corrupt — a following partition. It reads
// each partition's START sector via lsblk and confirms none starts after the
// root partition. It performs only reads; it never mutates the disk.
func assertRootIsLastPartition(loopDevPath, rootDevice string) error {
	cmd := fmt.Sprintf("lsblk -b --json -o PATH,START,TYPE %s", shell.QuoteArg(loopDevPath))
	out, err := resizeExec(cmd)
	if err != nil {
		return fmt.Errorf("overlay resize: failed to read partition layout of %s: %w", loopDevPath, err)
	}

	starts, err := parsePartitionStarts(out)
	if err != nil {
		return fmt.Errorf("overlay resize: %w", err)
	}

	rootStart, ok := starts[rootDevice]
	if !ok {
		return fmt.Errorf(
			"overlay resize: could not determine the start offset of root partition %s on %s; "+
				"refusing to grow", rootDevice, loopDevPath)
	}
	for path, start := range starts {
		if path == rootDevice {
			continue
		}
		if start > rootStart {
			return &unsupportedLayoutError{
				detected: fmt.Sprintf("root partition %s (start sector %d) is not the last partition on %s; "+
					"partition %s starts later (sector %d)", rootDevice, rootStart, loopDevPath, path, start),
				reason: "overlay grow-only resize can only extend the last partition on the disk into the " +
					"free space that follows it; growing a non-last root would corrupt the following partition",
				remediation: "use a baseline whose root filesystem is the last partition on the disk, or " +
					"reduce the packages being installed (or use a baseline with enough free space) so the grow " +
					"is not needed",
			}
		}
	}
	return nil
}

// parsePartitionStarts extracts the START sector offset of each partition node
// from `lsblk --json` output, keyed by device path. Only rows of TYPE "part"
// are included (the whole-disk/loop node and any nested LVM/crypt children carry
// no meaningful partition-table start offset). It tolerates the numeric and
// quoted-string forms lsblk emits across versions.
func parsePartitionStarts(lsblkJSON string) (map[string]int64, error) {
	var parsed struct {
		BlockDevices []map[string]interface{} `json:"blockdevices"`
	}
	if err := json.Unmarshal([]byte(lsblkJSON), &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse partition layout: %w", err)
	}

	starts := make(map[string]int64)
	var parseErr error
	var collect func(dev map[string]interface{})
	collect = func(dev map[string]interface{}) {
		if devType, _ := dev["type"].(string); devType == "part" {
			if path := stringField(dev, "path"); path != "" {
				// Fail closed: a partition whose START is missing or unparseable must
				// abort the guard, never default to 0. A silent 0 would make a later
				// partition look like it starts before root, so the non-last-partition
				// safety check could pass and let growpart corrupt a following partition.
				start, ok := int64FieldStrict(dev, "start")
				if !ok {
					if parseErr == nil {
						parseErr = fmt.Errorf(
							"missing or unparseable START offset for partition %s in lsblk output", path)
					}
					return
				}
				starts[path] = start
			}
		}
		if children, ok := dev["children"].([]interface{}); ok {
			for _, c := range children {
				if cm, ok := c.(map[string]interface{}); ok {
					collect(cm)
				}
			}
		}
	}
	for _, dev := range parsed.BlockDevices {
		collect(dev)
	}
	if parseErr != nil {
		return nil, parseErr
	}
	return starts, nil
}

// splitPartitionDevice splits a partition device node into its parent disk and
// partition number, handling both the loop/nvme/mmc "p"-suffixed form
// (/dev/loop0p2 -> /dev/loop0, "2") and the plain sd* form (/dev/sda2 ->
// /dev/sda, "2"). It is pure so the parsing is unit-tested directly.
func splitPartitionDevice(dev string) (disk, partNum string, err error) {
	d := strings.TrimSpace(dev)
	if d == "" {
		return "", "", fmt.Errorf("overlay resize: empty root device")
	}

	// The partition number is the trailing run of digits.
	i := len(d)
	for i > 0 && unicode.IsDigit(rune(d[i-1])) {
		i--
	}
	if i == len(d) {
		return "", "", fmt.Errorf("overlay resize: root device %q has no partition number", dev)
	}
	partNum = d[i:]
	disk = d[:i]

	// Devices whose names already end in a digit (loopN, nvmeNnN, mmcblkN) use a
	// "p" separator before the partition number; strip it from the disk path.
	if strings.HasSuffix(disk, "p") && len(disk) >= 2 && unicode.IsDigit(rune(disk[len(disk)-2])) {
		disk = disk[:len(disk)-1]
	}

	if disk == "" {
		return "", "", fmt.Errorf("overlay resize: could not derive parent disk from %q", dev)
	}
	return disk, partNum, nil
}
