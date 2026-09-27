package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/image/imagedisc"
	"github.com/open-edge-platform/image-composer-tool/internal/image/isopayload"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/mount"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/security"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

// Testability seam vars, matching the repo's established package-var DI pattern
// (e.g. initProvider in cmd/image-composer-tool/build.go). Tests replace these
// to assert on the recorded command sequence without touching a real disk.
var (
	resolveInstallDiskFn        = imagedisc.ResolveInstallDiskPath
	systemBlockDevicesFn        = imagedisc.SystemBlockDevices
	resolveExistingPartitionsFn = imagedisc.ResolveExistingPartitions
	canonicalDiskPathFn         = imagedisc.CanonicalDiskPath
	execCmdFn                   = shell.ExecCmd
	execStreamFn                = shell.ExecCmdWithStream
	execCmdWithInputFn          = shell.ExecCmdWithInput
	mountFn                     = mount.MountPath
	umountAndDeleteFn           = mount.UmountAndDeletePath
)

// decompressCmd maps a payload compression algorithm to the streaming
// decompressor invocation piped into dd. "none" is handled separately since it
// has no decompressor stage.
var decompressCmd = map[string]string{
	config.PayloadCompressionZstd: "zstd -d -c",
	config.PayloadCompressionXz:   "xz -d -c",
	config.PayloadCompressionGz:   "gzip -d -c",
}

// deployInstallerPayload writes a pre-built raw disk image staged on the ISO
// at <isoRoot>/payload/ onto the target disk, block-for-block, instead of
// reinstalling packages. It is the installer-payload counterpart of install().
//
// Every step up to (but not including) the pre-write wipefs is fail-before-write:
// any error there exits non-zero with the target disk completely untouched.
// The wipefs call itself is the start of the destructive phase — it runs
// before dd so that no stale GPT backup header survives at the tail of a
// disk larger than the payload — so from there on, a failure wipes the
// target's partition signatures (wipeDiskOnFailure) so it cannot be left
// presenting a half-written bootable disk, and does not retry or roll back —
// a whole-disk write is not reversible.
func deployInstallerPayload(template *config.ImageTemplate, isoRoot string) error {
	payloadPath, err := resolvePayloadFile(isoRoot, template.PayloadCompression())
	if err != nil {
		return fmt.Errorf("failed to resolve payload file: %w", err)
	}
	manifestPath := filepath.Join(isoRoot, "payload", "payload.manifest.yaml")

	manifest, err := isopayload.Load(manifestPath)
	if err != nil {
		return fmt.Errorf("failed to load payload manifest %s: %w", manifestPath, err)
	}
	if err := manifest.Validate(); err != nil {
		return fmt.Errorf("payload manifest %s is invalid: %w", manifestPath, err)
	}

	log.Infof("Verifying payload checksum before touching any target disk: %s", payloadPath)
	actualSha256, err := isopayload.FileSHA256(payloadPath)
	if err != nil {
		return fmt.Errorf("failed to checksum payload %s: %w", payloadPath, err)
	}
	if actualSha256 != manifest.CompressedSha256 {
		return fmt.Errorf("payload checksum mismatch for %s: manifest expects %s, got %s; refusing to write to any disk",
			payloadPath, manifest.CompressedSha256, actualSha256)
	}

	if manifest.Compression != config.PayloadCompressionNone {
		if _, ok := decompressCmd[manifest.Compression]; !ok {
			return fmt.Errorf("no decompressor mapped for payload compression %q; refusing to write to any disk", manifest.Compression)
		}
	}

	diskInfo := template.GetDiskConfig()
	diskPath, err := resolveInstallDiskFn(diskInfo)
	if err != nil {
		return fmt.Errorf("failed to resolve target disk: %w", err)
	}
	// disk.path (explicit or policy-resolved) may be an alias such as
	// /dev/disk/by-id/..., but SystemBlockDevices and every disk command below
	// (wipefs, dd, partx, sfdisk, lsblk) expect the canonical /dev/sdX form.
	// Canonicalize once here so it stays consistent everywhere downstream,
	// including the EFI boot entry updateBootOrder creates from template.Disk.Path.
	diskPath, err = canonicalDiskPathFn(diskPath)
	if err != nil {
		return fmt.Errorf("failed to canonicalize target disk path: %w", err)
	}
	log.Infof("Using target disk: %s", diskPath)
	// createNewBootEntry reads the disk path back off the template, and
	// diskInfo above is a value copy from GetDiskConfig - it must be written
	// back here (mirroring install.go's unattendedInstall) or updateBootOrder
	// fails with "no target disk path specified" whenever disk.path was empty
	// for policy-based selection, which installer-payload mode also supports.
	template.Disk.Path = diskPath

	if err := checkDiskCapacity(diskPath, manifest.RawBytes); err != nil {
		return fmt.Errorf("target disk capacity check failed: %w", err)
	}

	log.Infof("Waiting for disk %s to stabilize...", diskPath)
	if err := waitForDiskQuiescence(diskPath); err != nil {
		log.Warnf("Disk quiescence check failed (continuing anyway): %v", err)
	}

	if _, err := execCmdFn(fmt.Sprintf("wipefs -a -f %s", shell.QuoteArg(diskPath)), true, shell.HostPath, nil); err != nil {
		// wipefs may have partially cleared signatures before failing, so the
		// disk can no longer be assumed untouched — treat this the same as any
		// other post-boundary failure rather than returning as if nothing happened.
		wipeDiskOnFailure(diskPath)
		return fmt.Errorf("failed to clear existing signatures on %s before write: %w", diskPath, err)
	}

	log.Infof("Writing installer payload to %s (%d raw bytes, %s compression)", diskPath, manifest.RawBytes, manifest.Compression)
	if err := writePayloadToDisk(payloadPath, diskPath, manifest); err != nil {
		wipeDiskOnFailure(diskPath)
		return fmt.Errorf("failed to write payload to disk %s: %w", diskPath, err)
	}

	if err := refreshPartitionTable(diskPath, "after payload write"); err != nil {
		wipeDiskOnFailure(diskPath)
		return err
	}

	diskPathIdMap, err := resolveExistingPartitionsFn(diskPath, diskInfo.Partitions)
	if err != nil {
		wipeDiskOnFailure(diskPath)
		return fmt.Errorf("failed to resolve written partitions on %s: %w", diskPath, err)
	}

	if err := growInstalledDisk(diskPath, diskInfo, diskPathIdMap); err != nil {
		wipeDiskOnFailure(diskPath)
		return fmt.Errorf("failed to grow installed disk %s: %w", diskPath, err)
	}

	if template.ResetInstanceIdentity() {
		if err := resetInstanceIdentity(diskInfo, diskPathIdMap); err != nil {
			wipeDiskOnFailure(diskPath)
			return fmt.Errorf("failed to reset instance identity on %s: %w", diskPath, err)
		}
	}

	// A boot-order update failure does not corrupt the already fully-written,
	// grown, identity-reset disk, so it is not treated as a write failure and
	// does not trigger wipeDiskOnFailure.
	if err := updateBootOrder(template, diskPathIdMap); err != nil {
		return fmt.Errorf("failed to update boot order: %w", err)
	}

	log.Infof("Installer payload deployed successfully to %s", diskPath)
	return nil
}

// resolvePayloadFile locates the compressed payload file isomaker grafted
// onto the ISO under <isoRoot>/payload/, from the manifest's own compression
// field, without duplicating isopayload's unexported extension table.
func resolvePayloadFile(isoRoot, compression string) (string, error) {
	fileName, err := isopayload.PayloadFileName(compression)
	if err != nil {
		return "", err
	}
	return filepath.Join(isoRoot, "payload", fileName), nil
}

// checkDiskCapacity fails fast if the selected target disk is smaller than
// the payload it must hold. This is a distinct gate from disk *selection*:
// imagedisc's requiredInstallDiskBytes floor skips partitions with end: "0"
// (the common case for a payload's grown root partition), so the built-in
// selection floor alone is not sufficient here.
func checkDiskCapacity(diskPath string, requiredBytes int64) error {
	if requiredBytes < 0 {
		return fmt.Errorf("invalid required payload size for %s: %d bytes", diskPath, requiredBytes)
	}
	devices, err := systemBlockDevicesFn()
	if err != nil {
		return fmt.Errorf("failed to enumerate system block devices: %w", err)
	}
	for _, dev := range devices {
		if dev.DevicePath != diskPath {
			continue
		}
		if dev.RawDiskSize < uint64(requiredBytes) {
			return fmt.Errorf("target disk %s is too small for the payload: disk is %d bytes, payload requires %d bytes",
				diskPath, dev.RawDiskSize, requiredBytes)
		}
		return nil
	}
	return fmt.Errorf("target disk %s not found among system block devices", diskPath)
}

// writePayloadToDisk streams the (optionally compressed) payload directly
// onto the target block device and verifies dd's own reported byte count
// against the manifest. This is a residual check, not the primary gate: dd is
// the last stage of the pipe, so a mid-stream decompressor failure (e.g. OOM)
// can still exit 0 on a truncated disk; the pre-write compressed-sha256 check
// is what actually guarantees the input bytes are correct.
func writePayloadToDisk(payloadPath, diskPath string, m isopayload.Manifest) error {
	var ddCmd string
	if m.Compression == config.PayloadCompressionNone {
		ddCmd = fmt.Sprintf("dd if=%s of=%s bs=4M conv=fsync oflag=direct status=progress",
			shell.QuoteArg(payloadPath), shell.QuoteArg(diskPath))
	} else {
		decompressor, ok := decompressCmd[m.Compression]
		if !ok {
			return fmt.Errorf("no decompressor mapped for payload compression %q", m.Compression)
		}
		// A single "|" here is a genuine shell pipe: both sides are validated
		// independently against the allowlist. Never emit "||" in this command -
		// verifyCmdWithFullPath's separator scan checks "|" before "||" and, at
		// the same match index, "|" always wins, silently mis-splitting the command.
		ddCmd = fmt.Sprintf("%s %s | dd of=%s bs=4M conv=fsync oflag=direct status=progress",
			decompressor, shell.QuoteArg(payloadPath), shell.QuoteArg(diskPath))
	}

	output, err := execStreamFn(ddCmd, true, shell.HostPath, nil)
	if err != nil {
		return fmt.Errorf("failed to write payload to %s: %w", diskPath, err)
	}

	if _, err := execCmdFn("sync", true, shell.HostPath, nil); err != nil {
		return fmt.Errorf("sync after payload write to %s failed: %w", diskPath, err)
	}

	copiedBytes, err := parseDDBytesCopied(output)
	if err != nil {
		return fmt.Errorf("failed to verify payload write size: %w", err)
	}
	if copiedBytes != m.RawBytes {
		return fmt.Errorf("payload write short: dd reported %d bytes copied, manifest expects %d", copiedBytes, m.RawBytes)
	}

	return nil
}

// ddBytesCopiedPattern matches dd's "N bytes (...) copied" status line.
// status=progress emits this repeatedly as the write proceeds; the last match
// in the command's combined output is the final, authoritative count.
var ddBytesCopiedPattern = regexp.MustCompile(`(\d+) bytes(?: \([^)]*\))? copied`)

func parseDDBytesCopied(output string) (int64, error) {
	matches := ddBytesCopiedPattern.FindAllStringSubmatch(output, -1)
	if len(matches) == 0 {
		return 0, fmt.Errorf("could not find a dd byte count in command output")
	}
	last := matches[len(matches)-1]
	n, err := strconv.ParseInt(last[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid dd byte count %q: %w", last[1], err)
	}
	return n, nil
}

// refreshPartitionTable re-reads diskPath's partition table into the kernel
// after a write that changed it (payload write or partition growth), standing
// in for partprobe, which is not shell-allowlisted. context is included in the
// error message to identify which call site failed.
func refreshPartitionTable(diskPath, context string) error {
	if _, err := execCmdFn(fmt.Sprintf("partx -u %s", shell.QuoteArg(diskPath)), true, shell.HostPath, nil); err != nil {
		return fmt.Errorf("partx -u failed on %s %s: %w", diskPath, context, err)
	}
	if _, err := execCmdFn("udevadm settle --timeout=10", true, shell.HostPath, nil); err != nil {
		log.Warnf("udevadm settle failed %s (continuing): %v", context, err)
	}
	return nil
}

// wipeDiskOnFailure clears partition table signatures on diskPath after a
// failure that occurred once the payload write had already begun, so the
// target cannot be left presenting a half-written bootable disk. It is
// best-effort: it logs but does not return an error, since the caller is
// already propagating the failure that triggered it.
func wipeDiskOnFailure(diskPath string) {
	log.Errorf("Wiping target disk %s after a payload deploy failure to avoid leaving a half-written bootable disk", diskPath)
	if _, err := execCmdFn(fmt.Sprintf("wipefs -a -f %s", shell.QuoteArg(diskPath)), true, shell.HostPath, nil); err != nil {
		log.Errorf("Failed to wipe target disk %s after deploy failure: %v", diskPath, err)
	}
}

// growInstalledDisk grows the payload's designated last partition (end: "0")
// to fill whatever target disk was actually selected, then resizes its
// filesystem. This deliberately duplicates the fstype dispatch in
// config/osv/common/imageconfigs/firstboot/ict-auto-expand-last-partition.sh
// (the raw-image first-boot auto-expand path) rather than reusing it: that
// script runs inside the fully-booted target OS against its own mounted root,
// while this runs from the installer initrd against an unmounted disk it just
// wrote, so mounting is only needed for the xfs case.
func growInstalledDisk(diskPath string, diskInfo config.DiskConfig, diskPathIdMap map[string]string) error {
	if strings.ToLower(strings.TrimSpace(diskInfo.PartitionTableType)) != "gpt" {
		log.Infof("Skipping post-write partition growth: partitionTableType=%s (only gpt is grown)", diskInfo.PartitionTableType)
		return nil
	}

	growable, err := growablePartition(diskInfo.Partitions)
	if err != nil {
		return err
	}
	devPath, ok := diskPathIdMap[growable.ID]
	if !ok {
		return fmt.Errorf("resolved partition map has no device path for growable partition %q", growable.ID)
	}

	partNum, err := partitionNumber(devPath)
	if err != nil {
		return fmt.Errorf("failed to determine partition number for %s: %w", devPath, err)
	}

	// Mirrors the firstboot script's growth step: feeding sfdisk the same
	// ", +" stdin line extends the named partition to fill the remaining disk
	// space (relocating the GPT backup header as needed). partx -u + udevadm
	// settle stand in for partprobe, which is not shell-allowlisted.
	growCmd := fmt.Sprintf("sfdisk --no-reread --force -N %d %s", partNum, shell.QuoteArg(diskPath))
	if _, err := execCmdWithInputFn(", +\n", growCmd, true, shell.HostPath, nil); err != nil {
		return fmt.Errorf("failed to grow partition %d on %s: %w", partNum, diskPath, err)
	}

	if err := refreshPartitionTable(diskPath, "after partition growth"); err != nil {
		return err
	}

	if err := resizeFilesystem(devPath, growable.FsType); err != nil {
		return fmt.Errorf("failed to resize filesystem on %s: %w", devPath, err)
	}

	if _, err := execCmdFn("sync", true, shell.HostPath, nil); err != nil {
		log.Warnf("sync after filesystem resize failed (continuing): %v", err)
	}

	return nil
}

// growablePartition returns the partition declaring end: "0" - the sole
// candidate for post-write growth, matching the template semantics used
// throughout the rest of ICT (imagedisc.requiredInstallDiskBytes,
// createPartitionTable, etc).
func growablePartition(partitions []config.PartitionInfo) (config.PartitionInfo, error) {
	for _, p := range partitions {
		if strings.TrimSpace(p.End) == "0" {
			return p, nil
		}
	}
	return config.PartitionInfo{}, fmt.Errorf(`no partition with end: "0" found in disk config; nothing to grow`)
}

// partitionNumber resolves devPath's 1-based partition number via lsblk,
// rather than assuming template declaration order, since a partition's
// "index" field can renumber it relative to its position in the template.
func partitionNumber(devPath string) (int, error) {
	out, err := execCmdFn(fmt.Sprintf("lsblk -no PARTN %s", shell.QuoteArg(devPath)), true, shell.HostPath, nil)
	if err != nil {
		return 0, fmt.Errorf("lsblk -no PARTN %s: %w", devPath, err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("unexpected lsblk PARTN output %q for %s: %w", strings.TrimSpace(out), devPath, err)
	}
	return n, nil
}

// exitCodeAbove reports whether err represents a command that exited with a
// code greater than max, treating any non-exit error (e.g. the command never
// ran) as exceeding every bound. Mirrors internal/image/imageos/fde.go's
// helper of the same name for the same e2fsck exit-code convention (1 and 2
// are corrected conditions, not failures); duplicated rather than shared
// since that package builds images and this one deploys to a live disk.
func exitCodeAbove(err error, max int) bool {
	if err == nil {
		return false
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode() > max
	}
	return true
}

// resizeFilesystem grows the filesystem on devPath in place to match its
// just-grown partition. btrfs is explicitly unsupported (R9): growing it
// requires the btrfs command, which is not shell-allowlisted, and LVM/LUKS
// payloads are a different problem than this v1 addresses.
func resizeFilesystem(devPath, fsType string) error {
	switch strings.ToLower(strings.TrimSpace(fsType)) {
	case "ext2", "ext3", "ext4":
		if _, err := execCmdFn(fmt.Sprintf("e2fsck -fp %s", devPath), true, shell.HostPath, nil); err != nil {
			// e2fsck exit codes 1 and 2 mean errors were found and corrected,
			// which is fine to grow past; codes above 2 (or a non-exit error)
			// mean uncorrected errors or an operational failure, so resize2fs
			// must not run against a still-corrupt filesystem.
			if exitCodeAbove(err, 2) {
				return fmt.Errorf("e2fsck -fp %s: uncorrected filesystem errors or failure: %w", devPath, err)
			}
			log.Warnf("e2fsck -fp reported corrected issues on %s (continuing to resize2fs): %v", devPath, err)
		}
		if _, err := execStreamFn(fmt.Sprintf("resize2fs %s", devPath), true, shell.HostPath, nil); err != nil {
			return fmt.Errorf("resize2fs %s: %w", devPath, err)
		}
		return nil
	case "xfs":
		return resizeXFS(devPath)
	case "linux-swap", "swap":
		if _, err := execCmdFn(fmt.Sprintf("mkswap -f %s", devPath), true, shell.HostPath, nil); err != nil {
			return fmt.Errorf("mkswap -f %s: %w", devPath, err)
		}
		return nil
	case "btrfs":
		return fmt.Errorf("growing a btrfs payload partition is not supported")
	default:
		log.Warnf("Skipping filesystem growth for unsupported/unknown fsType %q on %s", fsType, devPath)
		return nil
	}
}

// withMountedDevice mounts devPath onto a fresh temp directory (named with
// tempDirPrefix), runs fn against the resulting mount point, and always
// unmounts and removes the temp directory afterward - shared by every
// deploy-path step that needs the just-written target mounted (xfs growth,
// identity reset).
func withMountedDevice(devPath, tempDirPrefix string, fn func(mountPoint string) error) (err error) {
	mountPoint, err := os.MkdirTemp("", tempDirPrefix)
	if err != nil {
		return fmt.Errorf("failed to create temp mount point for %s: %w", devPath, err)
	}
	if err := mountFn(devPath, mountPoint, ""); err != nil {
		_ = os.RemoveAll(mountPoint)
		return fmt.Errorf("failed to mount %s: %w", devPath, err)
	}
	defer func() {
		// A failed unmount must surface even when fn succeeded: otherwise the
		// caller reports a successful deploy while the target root is still
		// mounted at this temporary path.
		if umountErr := umountAndDeleteFn(mountPoint); umountErr != nil {
			log.Errorf("failed to unmount mount point %s: %v", mountPoint, umountErr)
			if err == nil {
				err = fmt.Errorf("failed to unmount %s: %w", mountPoint, umountErr)
			}
		}
	}()

	return fn(mountPoint)
}

// resizeXFS grows an xfs filesystem, which (unlike resize2fs) operates on a
// mount point rather than the block device directly.
func resizeXFS(devPath string) error {
	return withMountedDevice(devPath, "ict-payload-grow", func(mountPoint string) error {
		if _, err := execStreamFn(fmt.Sprintf("xfs_growfs %s", shell.QuoteArg(mountPoint)), true, shell.HostPath, nil); err != nil {
			return fmt.Errorf("xfs_growfs %s: %w", mountPoint, err)
		}
		return nil
	})
}

// resetInstanceIdentity clears the identity artifacts that must not be cloned
// verbatim from the payload build host onto every deployed instance:
// machine-id, the D-Bus machine-id copy, SSH host keys, and the systemd
// random seed. It operates via plain Go file ops on the mounted root
// partition rather than a chroot, since none of these are package operations;
// systemd regenerates machine-id and the random seed itself at first boot,
// and the ict-regenerate-ssh-host-keys first-boot service (installed by
// imageos.configureFirstBootSSHHostKeyRegen whenever this is enabled)
// regenerates the SSH host keys.
func resetInstanceIdentity(diskInfo config.DiskConfig, diskPathIdMap map[string]string) error {
	rootPartition, err := partitionByMountPoint(diskInfo.Partitions, "/")
	if err != nil {
		return err
	}
	rootDevPath, ok := diskPathIdMap[rootPartition.ID]
	if !ok {
		return fmt.Errorf("no resolved device path for root partition %q", rootPartition.ID)
	}

	return withMountedDevice(rootDevPath, "ict-payload-identity-reset", resetIdentityFiles)
}

func partitionByMountPoint(partitions []config.PartitionInfo, mountPoint string) (config.PartitionInfo, error) {
	for _, p := range partitions {
		if p.MountPoint == mountPoint {
			return p, nil
		}
	}
	return config.PartitionInfo{}, fmt.Errorf("no partition with mountPoint %q found in disk config", mountPoint)
}

func resetIdentityFiles(rootMount string) error {
	machineID := filepath.Join(rootMount, "etc", "machine-id")
	if err := security.SafeWriteFile(machineID, nil, 0444, security.RejectSymlinks); err != nil {
		return fmt.Errorf("failed to truncate %s: %w", machineID, err)
	}

	removeIfExists := []string{
		filepath.Join(rootMount, "var", "lib", "dbus", "machine-id"),
		filepath.Join(rootMount, "var", "lib", "systemd", "random-seed"),
	}
	for _, p := range removeIfExists {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove %s: %w", p, err)
		}
	}

	hostKeys, err := filepath.Glob(filepath.Join(rootMount, "etc", "ssh", "ssh_host_*"))
	if err != nil {
		return fmt.Errorf("failed to glob ssh host keys under %s: %w", rootMount, err)
	}
	for _, p := range hostKeys {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove %s: %w", p, err)
		}
	}

	return nil
}
