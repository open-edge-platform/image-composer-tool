package overlay

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/mount"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

// Partition-table types reported by lsblk/blkid.
const (
	partitionTableGPT = "gpt"
	partitionTableDOS = "dos" // MBR
)

// EFI System Partition type identifiers: the GPT type GUID and the MBR type byte.
const (
	espTypeGUID = "c12a7328-f81f-11d2-ba4b-00a0c93ec93b"
	espTypeMBR  = "0xef"
)

// Filesystem reported by lsblk/blkid for a LUKS container.
const fsTypeLUKS = "crypto_luks"

// Filesystem reported by lsblk/blkid for a swap partition. Swap is never a root
// candidate, so it is excluded from the size-based root fallback.
const fsTypeSwap = "swap"

// linuxRootTypeGUIDs are the arch-specific GPT type GUIDs that unambiguously mark
// a partition as the Linux root filesystem (systemd Discoverable Partitions Spec).
// A match here wins over size/label heuristics when picking the root partition.
var linuxRootTypeGUIDs = map[string]bool{
	"4f68bce3-e8cd-4db1-96e7-fbcaf984b709": true, // x86-64
	"b921b045-1df0-41c3-af44-4c6f280d3fae": true, // arm64
	"44479540-f297-41b2-9af7-d131d5f0458a": true, // x86
	"72ec70a6-cf74-40e6-bd49-4bda08e8f224": true, // riscv64
}

// dmVerityTypeGUIDs are the GPT type GUIDs for dm-verity (root) hash partitions.
// Their presence means the root is integrity-protected and cannot be overlaid.
var dmVerityTypeGUIDs = map[string]bool{
	"2c7357ed-ebd2-46d9-aec1-23d437ec2bf5": true, // x86-64 root verity
	"df3300ce-d69f-4c92-978c-9bfb0f38d820": true, // arm64 root verity
	"d13c5d3b-b5d1-422a-b29f-9454fdc89d76": true, // x86 root verity
	"ae0253be-1167-4007-ac68-43926c14c5de": true, // riscv64 root verity
}

// supportedRootFilesystems is the set of root filesystem types overlay mode can
// mount and modify. Anything else is rejected up front.
var supportedRootFilesystems = map[string]bool{
	"ext4": true,
	"ext3": true,
	"ext2": true,
	"xfs":  true,
}

// partition is the subset of lsblk fields used to classify a partition node.
type partition struct {
	Path      string // device node, e.g. /dev/loop0p1
	FSType    string // lower-cased filesystem type, e.g. "ext4", "vfat", "crypto_luks"
	PartType  string // lower-cased partition type GUID (GPT) or type byte (MBR)
	Label     string // filesystem label
	PartLabel string // GPT partition label
	Size      int64  // size in bytes
}

// Layout describes the baseline filesystem layout detected on (and mounted from)
// an attached loop device. It is consumed by downstream overlay stages.
type Layout struct {
	// PartitionTable is the detected table type, "gpt" or "dos".
	PartitionTable string
	// RootDevice is the root partition node, e.g. "/dev/loop0p2".
	RootDevice string
	// RootFSType is the root filesystem type, e.g. "ext4" or "xfs".
	RootFSType string
	// RootMount is the directory the root filesystem is mounted at.
	RootMount string
	// ESPDevice is the EFI System Partition node, empty if the image has none.
	ESPDevice string
	// ESPMount is the directory the ESP is mounted at (read-only), empty if no ESP.
	ESPMount string
	// MountBase is the workspace directory under which mounts are rooted.
	MountBase string
}

// unsupportedLayoutError reports a baseline layout overlay mode cannot handle.
// It carries what was detected, why it is unsupported, and a suggested
// remediation so the failure is actionable.
type unsupportedLayoutError struct {
	detected    string
	reason      string
	remediation string
}

func (e *unsupportedLayoutError) Error() string {
	return fmt.Sprintf("unsupported baseline layout: detected %s; %s; remediation: %s",
		e.detected, e.reason, e.remediation)
}

// Inspector mounts and inspects the baseline filesystem layout on an attached
// loop device. Mounts are rooted under the build workspace.
type Inspector struct {
	// mountBase is the workspace directory under which root/ESP are mounted.
	mountBase string
}

// NewInspector returns an Inspector that roots its mounts under workDir/mnt.
func NewInspector(workDir string) *Inspector {
	return &Inspector{mountBase: filepath.Join(workDir, "mnt")}
}

// WithMountedLayout detects the partition layout on loopDevPath, rejects
// unsupported layouts (encrypted root, dm-verity, unknown filesystem) up front,
// mounts the root filesystem (and ESP read-only if present) under the workspace,
// and invokes fn with the resulting Layout. All mounts are torn down in reverse
// order on success, failure, or panic.
//
// The ESP is mounted read-only so overlay stages cannot mutate the bootloader.
func (insp *Inspector) WithMountedLayout(loopDevPath string, fn func(*Layout) error) (err error) {
	if fn == nil {
		return fmt.Errorf("WithMountedLayout: fn callback must not be nil")
	}

	table, err := detectPartitionTable(loopDevPath)
	if err != nil {
		return err
	}

	parts, err := probePartitions(loopDevPath)
	if err != nil {
		return err
	}

	layout, err := analyzeLayout(table, parts)
	if err != nil {
		return err
	}
	layout.MountBase = insp.mountBase
	layout.RootMount = filepath.Join(insp.mountBase, "root")

	// defer-based teardown: unmount in reverse order on any return path,
	// including the error returns below and a panic inside fn.
	var mounted []string
	defer func() {
		for i := len(mounted) - 1; i >= 0; i-- {
			if uerr := mount.UmountPath(mounted[i]); uerr != nil {
				log.Errorf("Failed to unmount %s during overlay cleanup: %v", mounted[i], uerr)
			} else {
				log.Debugf("Unmounted %s", mounted[i])
			}
		}
	}()

	if err = mount.MountPath(layout.RootDevice, layout.RootMount, "-t "+layout.RootFSType); err != nil {
		return fmt.Errorf("failed to mount root filesystem %s (%s) at %s: %w",
			layout.RootDevice, layout.RootFSType, layout.RootMount, err)
	}
	mounted = append(mounted, layout.RootMount)
	log.Infof("Mounted root filesystem %s (%s) at %s", layout.RootDevice, layout.RootFSType, layout.RootMount)

	if layout.ESPDevice != "" {
		// Nest the ESP at the conventional /boot/efi under the mounted root so
		// chroot operations see it where the baseline expects it.
		layout.ESPMount = filepath.Join(layout.RootMount, "boot", "efi")
		if err = mount.MountPath(layout.ESPDevice, layout.ESPMount, "-o ro"); err != nil {
			return fmt.Errorf("failed to mount ESP %s at %s: %w", layout.ESPDevice, layout.ESPMount, err)
		}
		mounted = append(mounted, layout.ESPMount)
		log.Infof("Mounted ESP %s read-only at %s", layout.ESPDevice, layout.ESPMount)
	}

	if err = fn(layout); err != nil {
		return fmt.Errorf("overlay layout callback failed: %w", err)
	}
	return nil
}

// detectPartitionTable returns the partition-table type ("gpt" or "dos") of the
// whole loop device. It prefers lsblk and falls back to blkid. An image with no
// recognizable partition table is rejected as an unsupported layout.
func detectPartitionTable(loopDevPath string) (string, error) {
	out, err := shell.ExecCmd(fmt.Sprintf("lsblk -dno PTTYPE %s", shell.QuoteArg(loopDevPath)), true, shell.HostPath, nil)
	table := strings.ToLower(strings.TrimSpace(out))
	if err != nil || table == "" {
		// Fall back to blkid before giving up.
		if bout, berr := shell.ExecCmd(
			fmt.Sprintf("blkid -p -s PTTYPE -o value %s", shell.QuoteArg(loopDevPath)), true, shell.HostPath, nil); berr == nil {
			table = strings.ToLower(strings.TrimSpace(bout))
		}
	}

	switch table {
	case partitionTableGPT, partitionTableDOS:
		return table, nil
	case "":
		return "", &unsupportedLayoutError{
			detected:    fmt.Sprintf("no partition table on %s", loopDevPath),
			reason:      "overlay mode requires a partitioned baseline image (GPT or MBR)",
			remediation: "provide a baseline RAW image containing a GPT or MBR partition table",
		}
	default:
		return "", &unsupportedLayoutError{
			detected:    fmt.Sprintf("partition table type %q on %s", table, loopDevPath),
			reason:      "overlay mode supports only GPT and MBR (dos) partition tables",
			remediation: "provide a baseline image partitioned with GPT or MBR",
		}
	}
}

// probePartitions enumerates the partition nodes on loopDevPath via lsblk JSON.
//
// lsblk reports FSTYPE/PARTTYPE from the udev database, which is frequently not
// yet populated on a just-attached loop device. We first wait for udev to settle,
// then fall back to a direct blkid probe for any partition whose filesystem or
// partition type came back empty, so a real ext4/xfs root is never misread as
// "no recognizable filesystem" purely because of a probe race.
func probePartitions(loopDevPath string) ([]partition, error) {
	// Best-effort: let udev finish populating the just-attached loop partitions so
	// lsblk's FSTYPE/PARTTYPE columns are populated. A timeout/failure is not fatal
	// because the per-partition blkid fallback below probes the device directly.
	if _, err := shell.ExecCmd("udevadm settle --timeout=10", true, shell.HostPath, nil); err != nil {
		log.Warnf("udevadm settle before partition probe failed (continuing): %v", err)
	}

	cmd := fmt.Sprintf("lsblk -b --json -o NAME,PATH,FSTYPE,PARTTYPE,LABEL,PARTLABEL,SIZE,TYPE %s", shell.QuoteArg(loopDevPath))
	out, err := shell.ExecCmd(cmd, true, shell.HostPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list partitions for %s: %w", loopDevPath, err)
	}

	var parsed struct {
		BlockDevices []map[string]interface{} `json:"blockdevices"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse lsblk output for %s: %w", loopDevPath, err)
	}

	var parts []partition
	var collect func(dev map[string]interface{})
	collect = func(dev map[string]interface{}) {
		if devType, _ := dev["type"].(string); devType == "part" {
			parts = append(parts, partition{
				Path:      stringField(dev, "path"),
				FSType:    strings.ToLower(stringField(dev, "fstype")),
				PartType:  strings.ToLower(stringField(dev, "parttype")),
				Label:     stringField(dev, "label"),
				PartLabel: stringField(dev, "partlabel"),
				Size:      int64Field(dev, "size"),
			})
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

	// Fill in any FSTYPE/PARTTYPE lsblk left blank with a direct blkid probe.
	for i := range parts {
		if parts[i].Path == "" {
			continue
		}
		if parts[i].FSType == "" {
			if fs := probePartitionValue(parts[i].Path, "TYPE"); fs != "" {
				log.Debugf("blkid resolved filesystem type %q for %s (lsblk reported none)", fs, parts[i].Path)
				parts[i].FSType = fs
			}
		}
		if parts[i].PartType == "" {
			if pt := probePartitionValue(parts[i].Path, "PART_ENTRY_TYPE"); pt != "" {
				parts[i].PartType = pt
			}
		}
	}

	return parts, nil
}

// probePartitionValue reads a single blkid tag (e.g. "TYPE" for the filesystem
// type or "PART_ENTRY_TYPE" for the partition type GUID/byte) directly from a
// partition device, bypassing the udev cache. It returns the lower-cased value,
// or "" when blkid cannot determine it (the caller then leaves the field empty).
func probePartitionValue(devPath, tag string) string {
	out, err := shell.ExecCmd(
		fmt.Sprintf("blkid -p -s %s -o value %s", tag, shell.QuoteArg(devPath)), true, shell.HostPath, nil)
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(out))
}

// analyzeLayout classifies the probed partitions into a Layout. It rejects, with
// an actionable error, any layout overlay mode cannot handle: a LUKS-encrypted
// partition, a dm-verity protected root, an unidentifiable root, or a root whose
// filesystem type is unsupported. It performs no I/O and is the unit-tested core.
func analyzeLayout(table string, parts []partition) (*Layout, error) {
	if len(parts) == 0 {
		return nil, &unsupportedLayoutError{
			detected:    "no partitions on the baseline image",
			reason:      "overlay requires a mountable Linux root filesystem",
			remediation: "verify the baseline RAW image contains a partitioned Linux root filesystem",
		}
	}

	// Reject integrity/encryption layouts before attempting to pick a root.
	for _, p := range parts {
		if p.FSType == fsTypeLUKS {
			return nil, &unsupportedLayoutError{
				detected: fmt.Sprintf("encrypted partition %s (filesystem type crypto_LUKS)", p.Path),
				reason:   "overlay mode cannot modify a LUKS-encrypted filesystem in place",
				remediation: "provide an unencrypted baseline image, or unlock and re-encrypt the " +
					"volume out of band",
			}
		}
		if isDMVerity(p) {
			return nil, &unsupportedLayoutError{
				detected: fmt.Sprintf("dm-verity partition %s (type %q)", p.Path, p.PartType),
				reason: "overlay mode cannot modify a dm-verity protected root because changes " +
					"would invalidate the verity hash tree",
				remediation: "provide a baseline image without dm-verity, or rebuild the verity " +
					"tree out of band after the overlay",
			}
		}
	}

	layout := &Layout{PartitionTable: table}

	// Identify the ESP (optional). GUID/type match is authoritative. A bare vfat
	// partition is treated as the ESP only as an MBR fallback: GPT images always
	// tag the ESP with its type GUID, so on GPT a stray vfat (e.g. /boot, an OEM
	// partition) must not be misclassified as the ESP and excluded from root
	// selection.
	var espFallback *partition
	for i := range parts {
		p := parts[i]
		if p.PartType == espTypeGUID || p.PartType == espTypeMBR {
			layout.ESPDevice = p.Path
			break
		}
		if table == partitionTableDOS && p.FSType == "vfat" && espFallback == nil {
			espFallback = &parts[i]
		}
	}
	if layout.ESPDevice == "" && espFallback != nil {
		layout.ESPDevice = espFallback.Path
	}

	// Identify the root partition among the non-ESP partitions.
	root, err := selectRootPartition(parts, layout.ESPDevice)
	if err != nil {
		return nil, err
	}

	if !supportedRootFilesystems[root.FSType] {
		detected := fmt.Sprintf("root partition %s has filesystem type %q", root.Path, root.FSType)
		if root.FSType == "" {
			detected = fmt.Sprintf("root partition %s has no recognizable filesystem", root.Path)
		}
		return nil, &unsupportedLayoutError{
			detected:    detected,
			reason:      "overlay mode supports only ext4/ext3/ext2 and xfs root filesystems",
			remediation: "use a baseline image with an ext4 or xfs root filesystem",
		}
	}

	layout.RootDevice = root.Path
	layout.RootFSType = root.FSType
	return layout, nil
}

// selectRootPartition chooses the root partition from parts, excluding the ESP.
// Preference order: Linux-root type GUID, then a "root" label, then the largest
// candidate carrying a filesystem.
func selectRootPartition(parts []partition, espDevice string) (partition, error) {
	var candidates []partition
	for _, p := range parts {
		if p.Path == espDevice {
			continue
		}
		candidates = append(candidates, p)
	}
	if len(candidates) == 0 {
		return partition{}, &unsupportedLayoutError{
			detected:    "no non-ESP partition found",
			reason:      "overlay requires a mountable Linux root filesystem",
			remediation: "verify the baseline RAW image contains a Linux root partition",
		}
	}

	// 1. Authoritative Linux-root type GUID.
	for _, p := range candidates {
		if linuxRootTypeGUIDs[p.PartType] {
			return p, nil
		}
	}

	// 2. A partition explicitly labelled "root".
	for _, p := range candidates {
		if strings.EqualFold(p.PartLabel, "root") || strings.EqualFold(p.Label, "root") {
			return p, nil
		}
	}

	// 3. The largest partition that carries a mountable filesystem (so a tiny
	//    /boot does not outrank the real root). swap is skipped: a large swap
	//    partition must not outrank an untyped/unlabelled ext4 root and turn a
	//    supported image into a spurious "unsupported filesystem" failure.
	//    Falls back to the overall largest if none report a filesystem, letting
	//    the unsupported-FS check report the reason.
	best := -1
	for i, p := range candidates {
		if p.FSType == "" || p.FSType == fsTypeSwap {
			continue
		}
		if best == -1 || p.Size > candidates[best].Size {
			best = i
		}
	}
	if best == -1 {
		for i, p := range candidates {
			if best == -1 || p.Size > candidates[best].Size {
				best = i
			}
		}
	}
	return candidates[best], nil
}

// isDMVerity reports whether a partition is a dm-verity hash partition, detected
// by its GPT type GUID, partition label, or filesystem signature.
func isDMVerity(p partition) bool {
	if dmVerityTypeGUIDs[p.PartType] {
		return true
	}
	if strings.Contains(strings.ToLower(p.PartLabel), "verity") ||
		strings.Contains(strings.ToLower(p.Label), "verity") {
		return true
	}
	return strings.Contains(p.FSType, "verity")
}

// stringField reads a string-valued field from an lsblk device map.
func stringField(dev map[string]interface{}, key string) string {
	if v, ok := dev[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// int64Field reads a numeric lsblk field, tolerating both JSON numbers and the
// quoted-string form older lsblk versions emit.
func int64Field(dev map[string]interface{}, key string) int64 {
	switch v := dev[key].(type) {
	case float64:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n
	default:
		return 0
	}
}
