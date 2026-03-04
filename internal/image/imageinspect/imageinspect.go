package imageinspect

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/filesystem"
	"github.com/diskfs/go-diskfs/partition"
	"github.com/diskfs/go-diskfs/partition/gpt"
	"github.com/diskfs/go-diskfs/partition/mbr"
	"github.com/open-edge-platform/os-image-composer/internal/config"
	"github.com/open-edge-platform/os-image-composer/internal/image/imageconvert"
	"github.com/open-edge-platform/os-image-composer/internal/utils/file"
	"github.com/open-edge-platform/os-image-composer/internal/utils/logger"
	"go.uber.org/zap"
)

// ImageSummary holds the summary information about an inspected disk image.
type ImageSummary struct {
	File           string                `json:"file,omitempty"`
	SHA256         string                `json:"sha256,omitempty"`
	SizeBytes      int64                 `json:"sizeBytes,omitempty"`
	PartitionTable PartitionTableSummary `json:"partitionTable,omitempty"`
	Verity         *VerityInfo           `json:"verity,omitempty" yaml:"verity,omitempty"`
	// SBOM           SBOMSummary 		   `json:"sbom,omitempty"`
}

// VerityInfo holds dm-verity detection information.
type VerityInfo struct {
	Enabled       bool     `json:"enabled" yaml:"enabled"`
	Method        string   `json:"method,omitempty" yaml:"method,omitempty"` // "systemd-verity", "custom-initramfs", "unknown"
	RootDevice    string   `json:"rootDevice,omitempty" yaml:"rootDevice,omitempty"`
	HashPartition int      `json:"hashPartition,omitempty" yaml:"hashPartition,omitempty"` // partition index, 0 if none
	Notes         []string `json:"notes,omitempty" yaml:"notes,omitempty"`
}

// PartitionTableSummary holds information about the partition table of the disk image.
type PartitionTableSummary struct {
	Type               string
	DiskGUID           string `json:"diskGuid,omitempty" yaml:"diskGuid,omitempty"`
	LogicalSectorSize  int64
	PhysicalSectorSize int64
	ProtectiveMBR      bool
	Partitions         []PartitionSummary

	LargestFreeSpan      *FreeSpanSummary `json:"largestFreeSpan,omitempty" yaml:"largestFreeSpan,omitempty"`
	MisalignedPartitions []int            `json:"misalignedPartitions,omitempty" yaml:"misalignedPartitions,omitempty"`
}

// FreeSpanSummary captures the largest unallocated extent on disk (by LBA).
type FreeSpanSummary struct {
	StartLBA  uint64 `json:"startLba" yaml:"startLba"`
	EndLBA    uint64 `json:"endLba" yaml:"endLba"`
	SizeBytes uint64 `json:"sizeBytes" yaml:"sizeBytes"`
}

// PartitionSummary holds information about a single partition in the disk image.
type PartitionSummary struct {
	Index     int
	Name      string
	Type      string
	GUID      string `json:"guid,omitempty" yaml:"guid,omitempty"`
	StartLBA  uint64
	EndLBA    uint64
	SizeBytes uint64
	Flags     string

	// Raw GPT attributes plus common decoded flags (best-effort).
	AttrRaw                uint64 `json:"attrRaw,omitempty" yaml:"attrRaw,omitempty"`
	AttrRequired           bool   `json:"attrRequired,omitempty" yaml:"attrRequired,omitempty"`
	AttrLegacyBIOSBootable bool   `json:"attrLegacyBiosBootable,omitempty" yaml:"attrLegacyBiosBootable,omitempty"`
	AttrReadOnly           bool   `json:"attrReadOnly,omitempty" yaml:"attrReadOnly,omitempty"`

	// Needed for raw reads:
	LogicalSectorSize int                `json:"logicalSectorSize,omitempty" yaml:"logicalSectorSize,omitempty"`
	Filesystem        *FilesystemSummary `json:"filesystem,omitempty" yaml:"filesystem,omitempty"` // nil if unknown
}

// FilesystemSummary holds information about a filesystem found on a partition.
type FilesystemSummary struct {
	Type string `json:"type" yaml:"type"`

	Label string `json:"label,omitempty" yaml:"label,omitempty"`
	UUID  string `json:"uuid,omitempty" yaml:"uuid,omitempty"` // ext4 UUID, VFAT volume ID normalized, etc.

	// Common “evidence” fields
	BlockSize uint32   `json:"blockSize,omitempty" yaml:"blockSize,omitempty"`
	Features  []string `json:"features,omitempty" yaml:"features,omitempty"`
	Notes     []string `json:"notes,omitempty" yaml:"notes,omitempty"`

	// VFAT-specific
	FATType           string `json:"fatType,omitempty" yaml:"fatType,omitempty"` // FAT16/FAT32
	BytesPerSector    uint16 `json:"bytesPerSector,omitempty" yaml:"bytesPerSector,omitempty"`
	SectorsPerCluster uint8  `json:"sectorsPerCluster,omitempty" yaml:"sectorsPerCluster,omitempty"`
	ClusterCount      uint32 `json:"clusterCount,omitempty" yaml:"clusterCount,omitempty"`

	// Squashfs-specific
	Compression string   `json:"compression,omitempty" yaml:"compression,omitempty"`
	Version     string   `json:"version,omitempty" yaml:"version,omitempty"`
	FsFlags     []string `json:"fsFlags,omitempty" yaml:"fsFlags,omitempty"`

	// EFI/UKI evidence (VFAT/ESP)
	HasShim     bool                `json:"hasShim,omitempty" yaml:"hasShim,omitempty"`
	HasUKI      bool                `json:"hasUki,omitempty" yaml:"hasUki,omitempty"`
	EFIBinaries []EFIBinaryEvidence `json:"efiBinaries,omitempty" yaml:"efiBinaries,omitempty"`
}

// KeyValue represents a simple key-value pair.
type KeyValue struct {
	Key   string `json:"key" yaml:"key"`
	Value string `json:"value" yaml:"value"`
}

// EFIBinaryEvidence holds evidence extracted from an EFI binary (PE format).
type EFIBinaryEvidence struct {
	Path   string `json:"path" yaml:"path"`
	Size   int64  `json:"size" yaml:"size"`
	SHA256 string `json:"sha256" yaml:"sha256"`

	Arch string         `json:"arch,omitempty" yaml:"arch,omitempty"`
	Kind BootloaderKind `json:"kind,omitempty" yaml:"kind,omitempty"`

	// Secure Boot “evidence”
	Signed        bool `json:"signed,omitempty" yaml:"signed,omitempty"`
	SignatureSize int  `json:"signatureSize,omitempty" yaml:"signatureSize,omitempty"`
	HasSBAT       bool `json:"hasSbat,omitempty" yaml:"hasSbat,omitempty"`

	// PE section info
	Sections []string `json:"sections,omitempty" yaml:"sections,omitempty"`

	// UKI-specific evidence (if Kind == uki)
	IsUKI                   bool              `json:"isUki,omitempty" yaml:"isUki,omitempty"`
	Cmdline                 string            `json:"cmdline,omitempty" yaml:"cmdline,omitempty"`
	CmdlineNormalizedSHA256 string            `json:"cmdlineNormalizedSha256,omitempty" yaml:"cmdlineNormalizedSha256,omitempty"`
	Uname                   string            `json:"uname,omitempty" yaml:"uname,omitempty"`
	OSReleaseRaw            string            `json:"osReleaseRaw,omitempty" yaml:"osReleaseRaw,omitempty"`
	OSRelease               map[string]string `json:"osRelease,omitempty" yaml:"osRelease,omitempty"`
	OSReleaseSorted         []KeyValue        `json:"osReleaseSorted,omitempty" yaml:"osReleaseSorted,omitempty"`

	// Payload hashes (high value for diffs)
	SectionSHA256 map[string]string `json:"sectionSha256,omitempty" yaml:"sectionSha256,omitempty"`
	KernelSHA256  string            `json:"kernelSha256,omitempty" yaml:"kernelSha256,omitempty"` // .linux
	InitrdSHA256  string            `json:"initrdSha256,omitempty" yaml:"initrdSha256,omitempty"` // .initrd
	CmdlineSHA256 string            `json:"cmdlineSha256,omitempty" yaml:"cmdlineSha256,omitempty"`
	OSRelSHA256   string            `json:"osrelSha256,omitempty" yaml:"osrelSha256,omitempty"`
	UnameSHA256   string            `json:"unameSha256,omitempty" yaml:"unameSha256,omitempty"`

	// Bootloader configuration (for GRUB, systemd-boot, etc.)
	BootConfig *BootloaderConfig `json:"bootConfig,omitempty" yaml:"bootConfig,omitempty"`

	Notes []string `json:"notes,omitempty" yaml:"notes,omitempty"`
}

// BootloaderKind represents the kind of bootloader detected in an EFI binary.
type BootloaderKind string

// Possible BootloaderKind values
const (
	BootloaderUnknown      BootloaderKind = "unknown"
	BootloaderUKI          BootloaderKind = "uki"
	BootloaderShim         BootloaderKind = "shim"
	BootloaderGrub         BootloaderKind = "grub"
	BootloaderSystemdBoot  BootloaderKind = "systemd-boot"
	BootloaderMokManager   BootloaderKind = "mok-manager"
	BootloaderLinuxEFIStub BootloaderKind = "linux-efi-stub" // optional
)

// BootloaderConfig captures bootloader configuration data and kernel references.
type BootloaderConfig struct {
	// Configuration file paths and hashes
	ConfigFiles map[string]string `json:"configFiles,omitempty" yaml:"configFiles,omitempty"` // path -> SHA256
	ConfigRaw   map[string]string `json:"configRaw,omitempty" yaml:"configRaw,omitempty"`     // path -> raw content (truncated if large)

	// Kernel location references extracted from config
	KernelReferences []KernelReference `json:"kernelReferences,omitempty" yaml:"kernelReferences,omitempty"`

	// Boot entries (GRUB/systemd-boot/EFI boot order)
	BootEntries []BootEntry `json:"bootEntries,omitempty" yaml:"bootEntries,omitempty"`

	// UUID resolution: UUIDs found in config and whether they match partition table
	UUIDReferences []UUIDReference `json:"uuidReferences,omitempty" yaml:"uuidReferences,omitempty"`

	// Default boot target/entry
	DefaultEntry string `json:"defaultEntry,omitempty" yaml:"defaultEntry,omitempty"`

	// Configuration issues detected during parsing
	Notes []string `json:"notes,omitempty" yaml:"notes,omitempty"`
}

// KernelReference represents a kernel file reference found in bootloader config.
type KernelReference struct {
	Path          string `json:"path" yaml:"path"`                                       // Kernel path as specified in config
	PartitionUUID string `json:"partitionUuid,omitempty" yaml:"partitionUuid,omitempty"` // UUID reference if present
	RootUUID      string `json:"rootUuid,omitempty" yaml:"rootUuid,omitempty"`           // root device UUID reference if present
	BootEntry     string `json:"bootEntry,omitempty" yaml:"bootEntry,omitempty"`         // Which boot entry this references
}

// BootEntry represents a single boot entry (GRUB menu item, systemd-boot entry, etc.).
type BootEntry struct {
	Name          string `json:"name" yaml:"name"`                                       // Entry name/title
	Kernel        string `json:"kernel" yaml:"kernel"`                                   // Kernel path
	Initrd        string `json:"initrd,omitempty" yaml:"initrd,omitempty"`               // Initrd path
	Cmdline       string `json:"cmdline,omitempty" yaml:"cmdline,omitempty"`             // Kernel cmdline
	IsDefault     bool   `json:"isDefault,omitempty" yaml:"isDefault,omitempty"`         // Whether this is default
	PartitionUUID string `json:"partitionUuid,omitempty" yaml:"partitionUuid,omitempty"` // Root partition UUID
	RootDevice    string `json:"rootDevice,omitempty" yaml:"rootDevice,omitempty"`       // Root device reference
	UKIPath       string `json:"ukiPath,omitempty" yaml:"ukiPath,omitempty"`             // For systemd-boot unified kernel image
}

// UUIDReference tracks UUIDs found in bootloader config and partition table resolution.
type UUIDReference struct {
	UUID                string `json:"uuid" yaml:"uuid"`
	Context             string `json:"context" yaml:"context"`                                             // Where found: "kernel_cmdline", "root_device", "boot_entry", etc.
	ReferencedPartition int    `json:"referencedPartition,omitempty" yaml:"referencedPartition,omitempty"` // Partition index (1-based) if resolved
	Mismatch            bool   `json:"mismatch" yaml:"mismatch"`                                           // True if UUID not found in partition table
}

// File system constants
const (
	unrealisticSectorSize = 65535
)

type diskAccessorFS interface {
	GetPartitionTable() (partition.Table, error)
	GetFilesystem(partitionNumber int) (filesystem.FileSystem, error)
}

type DiskfsInspector struct {
	HashImages bool
	logger     *zap.SugaredLogger
}

func NewDiskfsInspector(hash bool) *DiskfsInspector {
	return &DiskfsInspector{HashImages: hash, logger: logger.Logger()}
}

func (d *DiskfsInspector) Inspect(imagePath string) (*ImageSummary, error) {
	d.logger.Infof("Inspecting image: %s, hashImages=%v", imagePath, d.HashImages)

	fi, err := os.Stat(imagePath)
	if err != nil {
		return nil, fmt.Errorf("stat image: %w", err)
	}

	// Detect image format and convert to RAW if needed
	format, err := imageconvert.DetectImageFormat(imagePath)
	if err != nil {
		d.logger.Warnf("Failed to detect image format, assuming raw: %v", err)
		format = "raw"
	}

	actualImagePath := imagePath
	var cleanupPath string
	defer func() {
		if cleanupPath != "" {
			if err := os.Remove(cleanupPath); err != nil {
				d.logger.Warnf("Failed to cleanup temporary converted image: %v", err)
			}
		}
	}()

	if format != "raw" {
		d.logger.Infof("Image format is %s, converting to RAW for inspection", format)

		// Get temp directory from config
		tmpDir, err := config.EnsureTempDir("image-inspect")
		if err != nil {
			return nil, fmt.Errorf("failed to create temp directory: %w", err)
		}

		// Check available disk space before conversion
		if err := file.CheckDiskSpace(tmpDir, fi.Size(), 0.20); err != nil {
			return nil, fmt.Errorf("insufficient disk space for image conversion: %w", err)
		}

		// Convert image to RAW format
		convertedPath, err := imageconvert.ConvertImageToRaw(imagePath, tmpDir)
		if err != nil {
			return nil, fmt.Errorf("failed to convert image to RAW format: %w", err)
		}

		// Mark for cleanup if we created a new file
		if convertedPath != imagePath {
			cleanupPath = convertedPath
			actualImagePath = convertedPath
			fi, err = os.Stat(actualImagePath)
			if err != nil {
				return nil, fmt.Errorf("stat converted image: %w", err)
			}
		}
	}

	img, err := os.Open(actualImagePath)
	if err != nil {
		return nil, fmt.Errorf("open image file: %w", err)
	}
	defer img.Close()

	sha := ""
	// Optional SHA256 hash computation
	if d.HashImages {
		d.logger.Infof("Computing SHA256 for image: %s", actualImagePath)
		sha, err = computeFileSHA256(img)
		if err != nil {
			return nil, fmt.Errorf("sha256 image: %w", err)
		}
	}

	disk, err := diskfs.Open(actualImagePath)
	if err != nil {
		return nil, fmt.Errorf("open disk image: %w", err)
	}
	defer disk.Close()

	// Use original path in the summary, not the temporary converted path
	return d.inspectCore(img, disk, disk.LogicalBlocksize, imagePath, fi.Size(), sha)
}

// inspectCoreNoHash is a helper that calls inspectCore without SHA256 computation.
func (d *DiskfsInspector) inspectCoreNoHash(
	img io.ReaderAt,
	disk diskAccessorFS,
	logicalBlockSize int64,
	imagePath string,
	sizeBytes int64,
) (*ImageSummary, error) {
	return d.inspectCore(img, disk, logicalBlockSize, imagePath, sizeBytes, "")
}

// inspectCore performs the core inspection logic given a disk accessor.
func (d *DiskfsInspector) inspectCore(
	img io.ReaderAt,
	disk diskAccessorFS,
	logicalBlockSize int64,
	imagePath string,
	sizeBytes int64,
	sha256sum string,
) (*ImageSummary, error) {

	if logicalBlockSize <= 0 || sizeBytes <= 0 || logicalBlockSize > unrealisticSectorSize {
		return nil, fmt.Errorf("invalid block or image size: logicalBlockSize=%d, sizeBytes=%d", logicalBlockSize, sizeBytes)
	}
	pt, err := disk.GetPartitionTable()
	if err != nil {
		return nil, fmt.Errorf("get partition table: %w", err)
	}

	ptSummary, err := summarizePartitionTable(pt, logicalBlockSize, sizeBytes)
	if err != nil {
		return nil, err
	}

	partitionsWithFS, err := InspectFileSystemsFromHandles(img, disk, ptSummary)
	if err != nil {
		return nil, fmt.Errorf("inspect filesystems: %w", err)
	}
	ptSummary.Partitions = partitionsWithFS

	// Detect dm-verity configuration
	verityInfo := detectVerity(ptSummary)

	return &ImageSummary{
		File:           imagePath,
		SizeBytes:      sizeBytes,
		PartitionTable: ptSummary,
		SHA256:         sha256sum,
		Verity:         verityInfo,
	}, nil
}

// summarizePartitionTable creates a PartitionTableSummary from a diskfs partition.Table.
func summarizePartitionTable(pt partition.Table, logicalBlockSize int64, totalSizeBytes int64) (PartitionTableSummary, error) {
	ptSummary := PartitionTableSummary{
		Partitions: make([]PartitionSummary, 0),
	}

	switch t := pt.(type) {
	case *gpt.Table:
		ptSummary.Type = "gpt"
		ptSummary.DiskGUID = strings.ToUpper(t.GUID)
		ptSummary.PhysicalSectorSize = int64(t.PhysicalSectorSize)
		ptSummary.LogicalSectorSize = int64(t.LogicalSectorSize)
		ptSummary.ProtectiveMBR = t.ProtectiveMBR

		for _, p := range t.Partitions {
			if p.Start == 0 && p.End == 0 {
				continue
			}
			sizeBytes := (p.End - p.Start + 1) * uint64(logicalBlockSize)

			ptSummary.Partitions = append(ptSummary.Partitions, PartitionSummary{
				// Index will be assigned after sorting
				Name:      p.Name,
				Type:      string(p.Type),
				GUID:      strings.ToUpper(p.GUID),
				StartLBA:  p.Start,
				EndLBA:    p.End,
				SizeBytes: sizeBytes,
				Flags:     fmt.Sprintf("%v", p.Attributes),
				AttrRaw:   p.Attributes,
			})

			last := &ptSummary.Partitions[len(ptSummary.Partitions)-1]
			last.AttrRequired = (p.Attributes & 0x1) != 0
			last.AttrLegacyBIOSBootable = (p.Attributes & (1 << 2)) != 0
			last.AttrReadOnly = (p.Attributes & (1 << 60)) != 0
		}

		sort.Slice(ptSummary.Partitions, func(i, j int) bool {
			return ptSummary.Partitions[i].StartLBA < ptSummary.Partitions[j].StartLBA
		})

		for i := range ptSummary.Partitions {
			ptSummary.Partitions[i].Index = i + 1
		}

	case *mbr.Table:
		ptSummary.Type = "mbr"
		ptSummary.PhysicalSectorSize = int64(t.PhysicalSectorSize)
		ptSummary.LogicalSectorSize = int64(t.LogicalSectorSize)

		for _, p := range t.Partitions {
			sizeBytes := uint64(p.Size) * uint64(logicalBlockSize)
			ptSummary.Partitions = append(ptSummary.Partitions, PartitionSummary{
				// Index will be assigned after sorting (optional for MBR, but consistent)
				Type:      fmt.Sprintf("0x%02x", p.Type),
				StartLBA:  uint64(p.Start),
				EndLBA:    uint64(p.Start) + uint64(p.Size) - 1,
				SizeBytes: sizeBytes,
			})
		}

		sort.Slice(ptSummary.Partitions, func(i, j int) bool {
			return ptSummary.Partitions[i].StartLBA < ptSummary.Partitions[j].StartLBA
		})
		for i := range ptSummary.Partitions {
			ptSummary.Partitions[i].Index = i + 1
		}

	default:
		return PartitionTableSummary{}, fmt.Errorf("unsupported partition table type: %T", t)
	}

	ptSummary.LargestFreeSpan = computeLargestFreeSpan(ptSummary.Partitions, logicalBlockSize, totalSizeBytes)
	ptSummary.MisalignedPartitions = findMisalignedPartitions(ptSummary.Partitions, logicalBlockSize, ptSummary.PhysicalSectorSize)

	return ptSummary, nil
}

// computeLargestFreeSpan returns the largest unallocated extent, if any, using LBAs.
// If totalSizeBytes is zero or no gaps exist, it returns nil.
func computeLargestFreeSpan(parts []PartitionSummary, logicalBlockSize int64, totalSizeBytes int64) *FreeSpanSummary {
	if logicalBlockSize <= 0 || totalSizeBytes <= 0 {
		return nil
	}

	totalSectors := uint64(totalSizeBytes / logicalBlockSize)
	if totalSectors == 0 {
		return nil
	}

	if len(parts) == 0 {
		return &FreeSpanSummary{StartLBA: 0, EndLBA: totalSectors - 1, SizeBytes: uint64(totalSizeBytes)}
	}

	// Parts are already sorted by StartLBA.
	var best *FreeSpanSummary
	prevEnd := uint64(0)

	for i, p := range parts {
		if i == 0 {
			if p.StartLBA > 0 {
				gap := buildSpan(0, p.StartLBA-1, logicalBlockSize)
				best = pickLarger(best, gap)
			}
		} else {
			if p.StartLBA > prevEnd+1 {
				gap := buildSpan(prevEnd+1, p.StartLBA-1, logicalBlockSize)
				best = pickLarger(best, gap)
			}
		}
		if p.EndLBA > prevEnd {
			prevEnd = p.EndLBA
		}
	}

	// Tail gap to end of disk
	if prevEnd+1 < totalSectors {
		gap := buildSpan(prevEnd+1, totalSectors-1, logicalBlockSize)
		best = pickLarger(best, gap)
	}

	return best
}

func buildSpan(start, end uint64, logicalBlockSize int64) *FreeSpanSummary {
	if end < start {
		return nil
	}
	size := (end - start + 1) * uint64(logicalBlockSize)
	return &FreeSpanSummary{StartLBA: start, EndLBA: end, SizeBytes: size}
}

func pickLarger(cur, cand *FreeSpanSummary) *FreeSpanSummary {
	if cand == nil {
		return cur
	}
	if cur == nil || cand.SizeBytes > cur.SizeBytes {
		return cand
	}
	return cur
}

// findMisalignedPartitions returns partition indexes (1-based) that are not aligned
// to the physical sector size or a 1MiB boundary (whichever is stricter).
func findMisalignedPartitions(parts []PartitionSummary, logicalBlockSize int64, physicalSectorSize int64) []int {
	if len(parts) == 0 || logicalBlockSize <= 0 {
		return nil
	}

	alignBytes := physicalSectorSize
	if alignBytes <= 0 {
		alignBytes = 4096 // best-effort default
	}

	var out []int
	for _, p := range parts {
		startBytes := int64(p.StartLBA) * logicalBlockSize
		misaligned := (startBytes%alignBytes != 0) || (startBytes%(1024*1024) != 0)
		if misaligned {
			out = append(out, p.Index)
		}
	}
	return out
}

// diskfsPartitionNumberForSummary maps a PartitionSummary back to a diskfs partition number.
func diskfsPartitionNumberForSummary(d diskAccessorFS, ps PartitionSummary) (int, bool) {
	if ps.StartLBA == 0 && ps.EndLBA == 0 {
		return 0, false
	}

	pt, err := d.GetPartitionTable()
	if err != nil || pt == nil {
		return 0, false
	}

	try := func(pn int) (int, bool) {
		if pn < 0 {
			return 0, false
		}
		fs, err := d.GetFilesystem(pn)
		if err != nil || fs == nil {
			return 0, false
		}
		return pn, true
	}

	switch t := pt.(type) {
	case *gpt.Table:
		for i, p := range t.Partitions {
			// skip empty GPT entries
			if p.Start == 0 && p.End == 0 {
				continue
			}
			if p.Start == ps.StartLBA && p.End == ps.EndLBA {
				// In practice diskfs can be either 0-based index OR 1-based GPT partition number.
				if pn, ok := try(i); ok {
					return pn, true
				}
				if pn, ok := try(i + 1); ok {
					return pn, true
				}
				// Fall back to returning something deterministic even if probing fails.
				// Prefer i+1 for GPT
				return i + 1, true
			}
		}

	case *mbr.Table:
		for i, p := range t.Partitions {
			start := uint64(p.Start)
			end := start + uint64(p.Size) - 1
			if start == ps.StartLBA && end == ps.EndLBA {
				// MBR is also ambiguous across libs; probe both.
				if pn, ok := try(i); ok {
					return pn, true
				}
				if pn, ok := try(i + 1); ok {
					return pn, true
				}
				return i + 1, true
			}
		}
	}

	return 0, false
}

func computeFileSHA256(f *os.File) (string, error) {
	// Ensure we start from the beginning
	if _, err := f.Seek(0, 0); err != nil {
		return "", err
	}

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	// Restore position (optional; but nice hygiene)
	if _, err := f.Seek(0, 0); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// hashBytesHex computes SHA256 hash of a byte slice and returns hex string
func hashBytesHex(data []byte) string {
	h := sha256.New()
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// detectVerity inspects the partition table and UKI cmdline to detect dm-verity configuration
func detectVerity(pt PartitionTableSummary) *VerityInfo {
	info := &VerityInfo{}

	// Look for hash partition (common names/types)
	hashPartLoopIdx := -1
	for i, p := range pt.Partitions {
		name := strings.ToLower(p.Name)
		// Check for common hash partition names
		if strings.Contains(name, "hash") || name == "roothashmap" {
			hashPartLoopIdx = i
			info.HashPartition = p.Index
			info.Notes = append(info.Notes, fmt.Sprintf("Hash partition found: %s (partition %d)", p.Name, p.Index))
			break
		}
	}

	// Extract cmdline from UKI if present
	var cmdline string
	for _, p := range pt.Partitions {
		if p.Filesystem != nil && p.Filesystem.HasUKI {
			for _, efi := range p.Filesystem.EFIBinaries {
				if efi.IsUKI && efi.Cmdline != "" {
					cmdline = efi.Cmdline
					break
				}
			}
		}
		if cmdline != "" {
			break
		}
	}

	if cmdline == "" {
		// No cmdline found, dm-verity not detected
		if hashPartLoopIdx >= 0 {
			info.Notes = append(info.Notes, "Hash partition exists but no UKI cmdline found")
		}
		return nil
	}

	// 1. systemd.verity_* parameters (standard systemd-verity)
	if strings.Contains(cmdline, "systemd.verity_name=") ||
		strings.Contains(cmdline, "systemd.verity_root_data=") ||
		strings.Contains(cmdline, "systemd.verity_root_hash=") {
		info.Enabled = true
		info.Method = "systemd-verity"
		info.Notes = append(info.Notes, "systemd.verity_* parameters found in cmdline")

		// Extract root device from cmdline
		if strings.Contains(cmdline, "root=") {
			for _, part := range strings.Fields(cmdline) {
				if strings.HasPrefix(part, "root=") {
					info.RootDevice = strings.TrimPrefix(part, "root=")
					break
				}
			}
		}

		if hashPartLoopIdx >= 0 {
			info.Notes = append(info.Notes, fmt.Sprintf("Hash partition present at index %d", hashPartLoopIdx))
		} else {
			info.Notes = append(info.Notes, "WARNING: systemd.verity_* found but no hash partition detected")
		}
		return info
	}

	// 2. root=/dev/mapper/*verity* pattern (custom initramfs, e.g., EMT/EMF tpm-cryptsetup)
	if strings.Contains(cmdline, "root=/dev/mapper/") && strings.Contains(cmdline, "verity") {
		info.Enabled = true
		info.Method = "custom-initramfs"
		info.Notes = append(info.Notes, "root=/dev/mapper/*verity* pattern found in cmdline")

		// Extract the exact root device
		for _, part := range strings.Fields(cmdline) {
			if strings.HasPrefix(part, "root=") {
				info.RootDevice = strings.TrimPrefix(part, "root=")
				break
			}
		}

		if hashPartLoopIdx >= 0 {
			info.Notes = append(info.Notes, fmt.Sprintf("Hash partition present at index %d", hashPartLoopIdx))
			info.Notes = append(info.Notes, "Likely using separate hash partition for dm-verity")
		} else {
			info.Notes = append(info.Notes, "No separate hash partition detected")
			info.Notes = append(info.Notes, "Likely using custom initramfs (e.g., dracut tpm-cryptsetup module)")
			info.Notes = append(info.Notes, "Hash data may be: appended to rootfs, embedded in FDE, or managed by initramfs")
		}
		return info
	}

	// 3. Check for roothash= parameter (direct hash specification)
	if strings.Contains(cmdline, "roothash=") {
		info.Enabled = true
		info.Method = "roothash-parameter"
		info.Notes = append(info.Notes, "roothash= parameter found in cmdline")

		for _, part := range strings.Fields(cmdline) {
			if strings.HasPrefix(part, "root=") {
				info.RootDevice = strings.TrimPrefix(part, "root=")
				break
			}
		}

		if hashPartLoopIdx >= 0 {
			info.Notes = append(info.Notes, fmt.Sprintf("Hash partition present at index %d", hashPartLoopIdx))
		}
		return info
	}

	// No dm-verity detected
	return nil
}
