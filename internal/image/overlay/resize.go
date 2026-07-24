package overlay

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"unicode"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/image/imagedisc"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

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

// ResizeBaseline performs an optional, GROW-ONLY resize of the overlaid baseline so
// the final image matches a larger disk.size requested in the template. It never
// shrinks (a smaller or unset target is a no-op) and it grows the existing root
// partition and filesystem in place — it never repartitions or relocates the
// bootloader, preserving the overlay immutability contract.
//
// The sequence, when a grow is needed, is: extend the backing file, refresh the
// loop device capacity, fix the GPT backup header, re-read the partition table,
// grow the root partition, then grow its filesystem. ext{2,3,4} and xfs roots are
// supported (matching the layouts the inspector accepts).
func ResizeBaseline(template *config.ImageTemplate, ctx *Context, layout *Layout) error {
	if template == nil || ctx == nil || layout == nil {
		return fmt.Errorf("overlay resize: template, context, and layout are required")
	}

	target := strings.TrimSpace(template.GetDiskConfig().Size)
	allowResize := template.OverlayPolicy != nil && template.OverlayPolicy.AllowDiskResize
	plan, err := planResize(ctx.BaselineCopyPath, target, allowResize)
	if err != nil {
		return err
	}
	if !plan.Grow {
		log.Infof("Overlay resize: skipping (%s)", plan.Reason)
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
		plan.CurrentBytes, plan.TargetBytes, layout.RootDevice, disk, partNum)

	// 1. Extend the backing file to the target size, in-process rather than via a
	//    shell `truncate` (avoids shell parsing of the workspace path and drops a
	//    host-tool dependency for a trivial file op). This is grow-only: os.Truncate
	//    never shrinks here because planResize already guaranteed target > current.
	//    The copy is user-owned, so no sudo is needed.
	if err := os.Truncate(ctx.BaselineCopyPath, plan.TargetBytes); err != nil {
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

	log.Infof("Overlay resize: grew root filesystem to fill %d bytes", plan.TargetBytes)
	return nil
}

// planResize decides whether to grow the image at copyPath to the requested target
// size. It is grow-only: an unset, unparseable-as-smaller, or not-larger target
// yields Grow=false with a reason. It performs only a stat (to read the current
// size); the size parsing is delegated to the shared imagedisc translator so units
// match the rest of the tool ("4GiB", "8GB", ...).
//
// allowResize is the explicit opt-in (overlayPolicy.allowDiskResize). When
// a grow would be required but the caller has not opted in, planResize returns an
// error so the build fails with a clear message rather than silently changing the
// baseline partition layout. A target that is unset or not larger than the current
// image never needs the opt-in: it is a no-op regardless.
func planResize(copyPath, target string, allowResize bool) (resizePlan, error) {
	if target == "" {
		return resizePlan{Reason: "no disk.size requested"}, nil
	}

	fi, err := os.Stat(copyPath)
	if err != nil {
		return resizePlan{}, fmt.Errorf("overlay resize: failed to stat baseline copy %s: %w", copyPath, err)
	}
	current := fi.Size()

	targetBytes, err := imagedisc.TranslateSizeStrToBytes(target)
	if err != nil {
		return resizePlan{}, fmt.Errorf("overlay resize: invalid disk.size %q: %w", target, err)
	}
	// TranslateSizeStrToBytes returns a uint64; guard before narrowing to the int64
	// used for the file size, since a value above math.MaxInt64 would wrap to a
	// negative number and be misread as "smaller than current" (silently skipping a
	// requested grow). Such a size is nonsensical for a disk image, so reject it.
	if targetBytes > math.MaxInt64 {
		return resizePlan{}, fmt.Errorf("overlay resize: requested disk.size %q (%d bytes) is too large", target, targetBytes)
	}

	if int64(targetBytes) <= current {
		return resizePlan{
			CurrentBytes: current,
			Reason:       fmt.Sprintf("requested size %d <= current size %d (overlay resize is grow-only)", targetBytes, current),
		}, nil
	}

	// A grow is required. Overlay mode preserves the baseline layout unless the
	// user has explicitly opted in, so reject rather than resize behind their back.
	if !allowResize {
		return resizePlan{}, fmt.Errorf(
			"overlay resize: disk.size %q (%d bytes) is larger than the baseline image (%d bytes), "+
				"but growing the baseline is not permitted; set overlayPolicy.allowDiskResize: true "+
				"to allow the overlay to grow the disk, or remove/lower disk.size to keep the baseline layout",
			target, targetBytes, current)
	}

	return resizePlan{
		Grow:         true,
		CurrentBytes: current,
		TargetBytes:  int64(targetBytes),
	}, nil
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
				"resize2fs in e2fsprogs, xfs_growfs in xfsprogs, losetup/partx in util-linux) "+
				"or remove/lower disk.size (optionally also set overlayPolicy.allowDiskResize: false) "+
				"to keep the baseline layout and skip the resize",
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
					"remove/lower disk.size (or set overlayPolicy.allowDiskResize: false) to keep the baseline layout",
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
