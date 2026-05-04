package imageinspect

import (
	"encoding/binary"
	"fmt"
	"io"
	"strings"

	"github.com/diskfs/go-diskfs/filesystem"
)

// InspectFileSystemsFromHandles inspects the filesystems of the partitions in
// the given partition table summary, using the provided disk accessor to get
// filesystem handles.
func InspectFileSystemsFromHandles(
	img io.ReaderAt,
	disk diskAccessorFS,
	pt PartitionTableSummary,
) ([]PartitionSummary, error) {

	if len(pt.Partitions) == 0 {
		return pt.Partitions, nil
	}
	if pt.LogicalSectorSize <= 0 {
		return nil, fmt.Errorf("invalid LogicalSectorSize in partition table summary: %d", pt.LogicalSectorSize)
	}

	partitions := make([]PartitionSummary, 0, len(pt.Partitions))
	for _, p := range pt.Partitions {
		ps := p

		if ps.LogicalSectorSize == 0 {
			ps.LogicalSectorSize = int(pt.LogicalSectorSize)
		}

		pn, ok := diskfsPartitionNumberForSummary(disk, ps)
		if ok {
			if fs, err := disk.GetFilesystem(pn); err == nil && fs != nil {
				if ps.Filesystem == nil {
					ps.Filesystem = &FilesystemSummary{}
				}
				ps.Filesystem.Type = filesystemTypeLabel(fs.Type())
				ps.Filesystem.Label = strings.TrimSpace(fs.Label())
			} else {
				if ps.Filesystem == nil {
					ps.Filesystem = &FilesystemSummary{}
				}
				if !(ps.Filesystem != nil && strings.EqualFold(ps.Filesystem.FATType, "FAT16")) {
					ps.Filesystem.Notes = append(ps.Filesystem.Notes,
						fmt.Sprintf("diskfs GetFilesystem(%d) failed: %v", pn, err),
					)
				}
			}
		} else {
			if ps.Filesystem == nil {
				ps.Filesystem = &FilesystemSummary{}
			}
			ps.Filesystem.Notes = append(ps.Filesystem.Notes, "could not map partition summary to diskfs partition number")
		}

		if err := enrichFilesystemFromRaw(img, &ps, pt); err != nil {
			ps.Filesystem.Notes = append(ps.Filesystem.Notes, err.Error())
		}

		partitions = append(partitions, ps)
	}

	return partitions, nil
}

// enrichFilesystemFromRaw reads additional filesystem details directly from the raw image.
func enrichFilesystemFromRaw(img io.ReaderAt, p *PartitionSummary, pt PartitionTableSummary) error {
	if p.Filesystem == nil {
		p.Filesystem = &FilesystemSummary{}
	}

	sectorSize := int64(p.LogicalSectorSize)
	if sectorSize <= 0 {
		sectorSize = pt.LogicalSectorSize
	}
	if sectorSize <= 0 {
		return fmt.Errorf("missing logical sector size for partition %d", p.Index)
	}

	partOff := int64(p.StartLBA) * sectorSize

	fsType := strings.ToLower(strings.TrimSpace(p.Filesystem.Type))

	// Always sniff filesystem type from raw magic bytes to cross-check
	// the diskfs library classification, which can misidentify ext4 as vfat
	// (e.g., when an ext4 partition happens to have 0x55AA at offset 510).
	sniffed, sniffErr := sniffFilesystemType(img, partOff)
	if sniffErr == nil && sniffed != "" && sniffed != "unknown" {
		if fsType == "" || fsType == "unknown" {
			// No prior classification — use sniffed type
			fsType = sniffed
			p.Filesystem.Type = sniffed
		} else if fsType != sniffed && !isSameFSFamily(fsType, sniffed) {
			// diskfs and raw magic disagree — trust raw magic bytes
			p.Filesystem.Notes = append(p.Filesystem.Notes,
				fmt.Sprintf("filesystem type corrected from %q to %q (raw magic)", fsType, sniffed))
			fsType = sniffed
			p.Filesystem.Type = sniffed
		}
	}

	switch fsType {
	case "ext4", "ext3", "ext2":
		p.Filesystem.Type = "ext4"
		return readExtSuperblock(img, partOff, p.Filesystem)

	case "vfat", "fat", "msdos":
		p.Filesystem.Type = "vfat"
		_ = readFATBootSector(img, partOff, p.Filesystem)
		if strings.EqualFold(p.Filesystem.Type, "vfat") && isESPPartition(*p) {
			if err := scanAndHashEFIFromRawFAT(img, partOff, p.Filesystem); err != nil {
				p.Filesystem.Notes = append(p.Filesystem.Notes, fmt.Sprintf("EFI raw scan failed: %v", err))
			}
		}
		return nil

	case "squashfs":
		p.Filesystem.Type = "squashfs"
		return readSquashfsSuperblock(img, partOff, p.Filesystem)

	default:
		return nil
	}
}

// sniffFilesystemType attempts to identify the filesystem type by reading magic
// numbers from the partition start.
func sniffFilesystemType(r io.ReaderAt, partOff int64) (string, error) {
	// Squashfs magic at start: "hsqs" (little endian) or "sqsh" variant
	head := make([]byte, 4096)
	if _, err := r.ReadAt(head, partOff); err != nil && err != io.EOF {
		return "", err
	}
	if len(head) >= 4 {
		if string(head[0:4]) == "hsqs" || string(head[0:4]) == "sqsh" {
			return "squashfs", nil
		}
	}

	// ext magic 0xEF53 at offset 1024+56
	extMagic := make([]byte, 2)
	if _, err := r.ReadAt(extMagic, partOff+1024+56); err == nil {
		if extMagic[0] == 0x53 && extMagic[1] == 0xEF {
			return "ext4", nil
		}
	}

	// FAT boot sig 0x55AA at 510
	sig := make([]byte, 2)
	if _, err := r.ReadAt(sig, partOff+510); err == nil {
		if sig[0] == 0x55 && sig[1] == 0xAA {
			return "vfat", nil
		}
	}

	return "unknown", nil
}

// readExtSuperblock reads the ext filesystem superblock and fills in details.
func readExtSuperblock(r io.ReaderAt, partOff int64, out *FilesystemSummary) error {
	sb := make([]byte, 1024)
	if _, err := r.ReadAt(sb, partOff+1024); err != nil && err != io.EOF {
		return fmt.Errorf("read ext superblock: %w", err)
	}

	magic := binary.LittleEndian.Uint16(sb[56:58])
	if magic != 0xEF53 {
		return fmt.Errorf("ext superblock magic mismatch: 0x%x", magic)
	}

	// UUID at offset 104, 16 bytes
	out.UUID = formatUUID(sb[104:120])

	// Label at offset 120, 16 bytes (null-terminated)
	out.Label = strings.TrimRight(string(sb[120:136]), "\x00 ")

	// block size: 1024 << s_log_block_size at offset 24
	logBlockSize := binary.LittleEndian.Uint32(sb[24:28])
	out.BlockSize = uint32(1024 << logBlockSize)

	// feature flags: compat/incompat/ro_compat at offsets 92/96/100
	compat := binary.LittleEndian.Uint32(sb[92:96])
	incompat := binary.LittleEndian.Uint32(sb[96:100])
	ro := binary.LittleEndian.Uint32(sb[100:104])
	out.Features = append(out.Features, extFeatureStrings(compat, incompat, ro)...)

	return nil
}

func readFATBootSector(r io.ReaderAt, partOff int64, out *FilesystemSummary) error {
	bs := make([]byte, 512)
	if _, err := r.ReadAt(bs, partOff); err != nil && err != io.EOF {
		return fmt.Errorf("read fat boot sector: %w", err)
	}
	if bs[510] != 0x55 || bs[511] != 0xAA {
		return fmt.Errorf("fat boot sector missing 0x55AA signature")
	}

	// Common BPB fields
	bytesPerSec := binary.LittleEndian.Uint16(bs[11:13])
	secPerClus := bs[13]
	rsvdSecCnt := binary.LittleEndian.Uint16(bs[14:16])
	numFATs := uint32(bs[16])
	rootEntCnt := binary.LittleEndian.Uint16(bs[17:19])
	totSec16 := binary.LittleEndian.Uint16(bs[19:21])
	fatSz16 := binary.LittleEndian.Uint16(bs[22:24])
	totSec32 := binary.LittleEndian.Uint32(bs[32:36])
	fatSz32 := binary.LittleEndian.Uint32(bs[36:40]) // BPB_FATSz32 (FAT32)

	out.Type = "vfat"
	out.BytesPerSector = bytesPerSec
	out.SectorsPerCluster = secPerClus

	// Basic sanity checks to avoid bogus classification
	switch bytesPerSec {
	case 512, 1024, 2048, 4096:
		// ok
	default:
		return fmt.Errorf("invalid BPB: bytesPerSec=%d", bytesPerSec)
	}
	if secPerClus == 0 {
		return fmt.Errorf("invalid BPB: sectorsPerCluster=0")
	}
	if numFATs == 0 {
		return fmt.Errorf("invalid BPB: numFATs=0")
	}

	// Total sectors is either TotSec16 or TotSec32
	totalSectors := uint32(totSec16)
	if totalSectors == 0 {
		totalSectors = totSec32
	}
	if totalSectors == 0 {
		return fmt.Errorf("invalid BPB: totalSectors=0")
	}

	// Canonical FAT32 detection:
	// - RootEntCnt must be 0 for FAT32
	// - FATSz16 must be 0 for FAT32
	// - FATSz32 should be non-zero for FAT32 (but we won't *require* it to avoid false negatives on odd images)
	isFAT32 := (rootEntCnt == 0) && (fatSz16 == 0) && (fatSz32 != 0)

	if isFAT32 {
		out.FATType = "FAT32"
		out.UUID = fmt.Sprintf("%08x", binary.LittleEndian.Uint32(bs[67:71]))
		out.Label = strings.TrimRight(string(bs[71:82]), " \x00")

		// cluster count for FAT32 (root dir is in data area)
		rootDirSectors := uint32(0)
		fatSectors := fatSz32
		dataSectors := totalSectors - (uint32(rsvdSecCnt) + (numFATs * fatSectors) + rootDirSectors)
		out.ClusterCount = dataSectors / uint32(secPerClus)
		return nil
	}

	// FAT12/16-style: classify via cluster count
	rootDirSectors := ((uint32(rootEntCnt) * 32) + (uint32(bytesPerSec) - 1)) / uint32(bytesPerSec)
	fatSectors := uint32(fatSz16)

	// If FAT16 fields suggest nonsense, note it (helps debug wrong offsets/sector)
	if fatSectors == 0 {
		out.Notes = append(out.Notes, fmt.Sprintf("BPB_FATSz16=0 but not detected as FAT32 (RootEntCnt=%d FATSz32=%d)", rootEntCnt, fatSz32))
	}

	dataSectors := totalSectors - (uint32(rsvdSecCnt) + (numFATs * fatSectors) + rootDirSectors)
	clusterCount := dataSectors / uint32(secPerClus)

	out.ClusterCount = clusterCount

	switch {
	case clusterCount < 4085:
		out.FATType = "FAT12"
	case clusterCount < 65525:
		out.FATType = "FAT16"
	default:
		// If cluster count is huge, it's overwhelmingly likely FAT32.
		out.FATType = "FAT32"
	}

	// FAT12/16 Extended BPB: VolID @ 39..43, Label @ 43..54
	out.UUID = fmt.Sprintf("%08x", binary.LittleEndian.Uint32(bs[39:43]))
	out.Label = strings.TrimRight(string(bs[43:54]), " \x00")

	out.Notes = append(out.Notes, fmt.Sprintf(
		"FAT BPB: BytsPerSec=%d SecPerClus=%d Rsvd=%d NumFATs=%d RootEntCnt=%d TotSec16=%d TotSec32=%d FATSz16=%d FATSz32=%d",
		bytesPerSec, secPerClus, rsvdSecCnt, numFATs, rootEntCnt, totSec16, totSec32, fatSz16, fatSz32,
	))

	return nil
}

// readSquashfsSuperblock reads the squashfs superblock and fills in details.
func readSquashfsSuperblock(r io.ReaderAt, partOff int64, out *FilesystemSummary) error {
	sb := make([]byte, 96)
	if _, err := r.ReadAt(sb, partOff); err != nil && err != io.EOF {
		return fmt.Errorf("read squashfs superblock: %w", err)
	}

	if string(sb[0:4]) != "hsqs" && string(sb[0:4]) != "sqsh" {
		return fmt.Errorf("squashfs magic mismatch: %q", string(sb[0:4]))
	}

	out.BlockSize = binary.LittleEndian.Uint32(sb[12:16])

	flags := binary.LittleEndian.Uint16(sb[16:18])
	out.FsFlags = squashFlagStrings(flags)

	compID := binary.LittleEndian.Uint16(sb[20:22])
	out.Compression = squashCompressionName(compID)

	major := binary.LittleEndian.Uint16(sb[28:30])
	minor := binary.LittleEndian.Uint16(sb[30:32])
	out.Version = fmt.Sprintf("%d.%d", major, minor)

	return nil
}

// isESPPartition determines if a partition is an EFI System Partition (ESP).
func isESPPartition(p PartitionSummary) bool {
	return strings.EqualFold(p.Type, "C12A7328-F81F-11D2-BA4B-00A0C93EC93B") || // GPT ESP
		strings.EqualFold(p.Name, "boot") || // optional heuristic
		(p.Filesystem != nil && strings.EqualFold(p.Filesystem.Type, "vfat"))
}

// isVFATLike determines if a filesystem type string corresponds to a VFAT-like filesystem.
func isVFATLike(t string) bool {
	t = strings.ToLower(strings.TrimSpace(t))
	return t == "vfat" || t == "fat" || t == "msdos" || t == "dos" || t == "fat16" || t == "fat32"
}

// isExtLike determines if a filesystem type string corresponds to an ext-family filesystem.
func isExtLike(t string) bool {
	t = strings.ToLower(strings.TrimSpace(t))
	return t == "ext4" || t == "ext3" || t == "ext2"
}

// isSameFSFamily returns true if the two filesystem type strings belong to the same
// filesystem family (e.g., ext2/ext3/ext4 are all ext, vfat/fat/msdos are all FAT).
func isSameFSFamily(a, b string) bool {
	if isExtLike(a) && isExtLike(b) {
		return true
	}
	if isVFATLike(a) && isVFATLike(b) {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// filesystemTypeLabel maps a diskfs filesystem.Type to a string label.
func filesystemTypeLabel(fsType filesystem.Type) string {
	switch fsType {
	case filesystem.TypeFat32:
		return "vfat"
	case filesystem.TypeISO9660:
		return "iso9660"
	case filesystem.TypeSquashfs:
		return "squashfs"
	case filesystem.TypeExt4:
		return "ext4"
	default:
		return "unknown"
	}
}

// squashCompressionName maps a squashfs compression ID to a human-readable name.
func squashCompressionName(id uint16) string {
	switch id {
	case 1:
		return "gzip"
	case 2:
		return "lzma"
	case 3:
		return "lzo"
	case 4:
		return "xz"
	case 5:
		return "lz4"
	case 6:
		return "zstd"
	default:
		return fmt.Sprintf("unknown(%d)", id)
	}
}

// squashFlagStrings converts squashfs filesystem flags to human-readable strings.
func squashFlagStrings(flags uint16) []string {
	out := []string{}
	const (
		noInodes    = 0x0001
		noData      = 0x0002
		noFragments = 0x0008
		noXattrs    = 0x0080
	)
	if flags&noFragments != 0 {
		out = append(out, "no_fragments")
	}
	if flags&noXattrs != 0 {
		out = append(out, "no_xattrs")
	}
	if flags&noInodes != 0 {
		out = append(out, "no_inodes")
	}
	if flags&noData != 0 {
		out = append(out, "no_data")
	}
	return out
}

// extFeatureStrings converts ext filesystem feature flags to human-readable strings.
func extFeatureStrings(compat, incompat, ro uint32) []string {
	feats := make([]string, 0, 16)

	// High-signal subset (extend later)
	const (
		compatHasJournal = 0x0004
		compatDirIndex   = 0x0020
	)
	const (
		incompatExtents  = 0x0040
		incompat64bit    = 0x0080
		incompatMetaCsum = 0x0400
	)
	const (
		roCompatHugeFile = 0x0008
		roCompatGdtCsum  = 0x0010
		roCompatMetaBG   = 0x0020
	)

	if (compat & compatHasJournal) != 0 {
		feats = append(feats, "has_journal")
	}
	if (compat & compatDirIndex) != 0 {
		feats = append(feats, "dir_index")
	}

	if (incompat & incompatExtents) != 0 {
		feats = append(feats, "extents")
	}
	if (incompat & incompat64bit) != 0 {
		feats = append(feats, "64bit")
	}
	if (incompat & incompatMetaCsum) != 0 {
		feats = append(feats, "metadata_csum")
	}

	if (ro & roCompatHugeFile) != 0 {
		feats = append(feats, "huge_file")
	}
	if (ro & roCompatGdtCsum) != 0 {
		feats = append(feats, "gdt_csum")
	}
	if (ro & roCompatMetaBG) != 0 {
		feats = append(feats, "meta_bg")
	}

	return feats
}

// formatUUID formats a 16-byte UUID into standard string representation.
func formatUUID(b []byte) string {
	if len(b) != 16 {
		return ""
	}
	return fmt.Sprintf("%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		b[0], b[1], b[2], b[3],
		b[4], b[5],
		b[6], b[7],
		b[8], b[9],
		b[10], b[11], b[12], b[13], b[14], b[15],
	)
}
