package imageinspect

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
)

type TextOptions struct {
	// leave room for future knobs
	// e.g. ShowFSDetails bool
}

type objectCounts struct {
	added, removed, modified int
}

type CompareTextOptions struct {
	Mode string // "diff" | "summary" | "full"
}

func RenderCompareText(w io.Writer, r *ImageCompareResult, opts CompareTextOptions) error {
	if r == nil {
		return fmt.Errorf("RenderCompareText: result is nil")
	}

	mode := normalizeCompareMode(opts.Mode)

	// Header
	renderEqualityHeader(w, r)
	fmt.Fprintln(w)

	if mode == "summary" {
		s := r.Summary
		fmt.Fprintf(w, "Changed: %v\n", s.Changed)
		fmt.Fprintf(w, "PartitionTableChanged: %v\n", s.PartitionTableChanged)
		fmt.Fprintf(w, "PartitionsChanged: %v\n", s.PartitionsChanged)
		fmt.Fprintf(w, "EFIBinariesChanged: %v\n", s.EFIBinariesChanged)
		obj := computeObjectCountsFromDiff(r.Diff)
		fmt.Fprintf(w, "Counts (objects): +%d -%d ~%d\n", obj.added, obj.removed, obj.modified)

		if r.Equality.Class == EqualityDifferent {
			if r.Equality.VolatileDiffs > 0 || r.Equality.MeaningfulDiffs > 0 {
				fmt.Fprintf(w, "Counts (fields):  volatile=%d meaningful=%d\n",
					r.Equality.VolatileDiffs, r.Equality.MeaningfulDiffs)
			}
		}

		return nil
	}

	if !r.Summary.Changed {
		fmt.Fprintln(w, "No changes detected.")
		if mode == "full" {
			renderImagesBlock(w, r.From, r.To)
		}
		return nil
	}

	obj := computeObjectCountsFromDiff(r.Diff)
	fmt.Fprintf(w, "Counts (objects): +%d -%d ~%d\n", obj.added, obj.removed, obj.modified)

	if r.Equality.VolatileDiffs > 0 || r.Equality.MeaningfulDiffs > 0 {
		fmt.Fprintf(w, "Counts (fields):  volatile=%d meaningful=%d\n",
			r.Equality.VolatileDiffs, r.Equality.MeaningfulDiffs)
	}
	// Partition table diff
	if r.Diff.PartitionTable.Changed {
		pt := r.Diff.PartitionTable
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Partition table:")
		if pt.DiskGUID != nil {
			fmt.Fprintf(w, "  Disk GUID: %q -> %q\n", pt.DiskGUID.From, pt.DiskGUID.To)
		}
		if pt.Type != nil {
			fmt.Fprintf(w, "  Type: %q -> %q\n", pt.Type.From, pt.Type.To)
		}
		if pt.LogicalSectorSize != nil {
			fmt.Fprintf(w, "  LogicalSectorSize: %d -> %d\n", pt.LogicalSectorSize.From, pt.LogicalSectorSize.To)
		}
		if pt.PhysicalSectorSize != nil {
			fmt.Fprintf(w, "  PhysicalSectorSize: %d -> %d\n", pt.PhysicalSectorSize.From, pt.PhysicalSectorSize.To)
		}
		if pt.ProtectiveMBR != nil {
			fmt.Fprintf(w, "  ProtectiveMBR: %v -> %v\n", pt.ProtectiveMBR.From, pt.ProtectiveMBR.To)
		}
		if pt.LargestFreeSpan != nil {
			fmt.Fprintf(w, "  Largest free span: %s -> %s\n", freeSpanString(&pt.LargestFreeSpan.From), freeSpanString(&pt.LargestFreeSpan.To))
		}
		if pt.MisalignedParts != nil {
			fmt.Fprintf(w, "  Misaligned partitions: %v -> %v\n", pt.MisalignedParts.From, pt.MisalignedParts.To)
		}
	}

	// Partitions diff (added/removed/modified)
	pd := r.Diff.Partitions
	if len(pd.Added) > 0 || len(pd.Removed) > 0 || len(pd.Modified) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Partitions:")

		if len(pd.Added) > 0 {
			fmt.Fprintln(w, "  Added:")
			for _, p := range pd.Added {
				renderPartitionSummaryLine(w, "    +", p)
			}
		}
		if len(pd.Removed) > 0 {
			fmt.Fprintln(w, "  Removed:")
			for _, p := range pd.Removed {
				renderPartitionSummaryLine(w, "    -", p)
			}
		}
		if len(pd.Modified) > 0 {
			fmt.Fprintln(w, "  Modified:")
			for _, m := range pd.Modified {
				fmt.Fprintf(w, "    ~ %s\n", m.Key)
				for _, ch := range m.Changes {
					fmt.Fprintf(w, "      %s: %v -> %v\n", ch.Field, ch.From, ch.To)
				}

				// FS change (compact)
				if m.Filesystem != nil {
					renderFilesystemChangeText(w, m.Filesystem)
				}

				// EFI diff scoped to partition (optional)
				if m.EFIBinaries != nil && hasAnyEFIDiff(*m.EFIBinaries) {
					fmt.Fprintln(w, "      EFI:")
					renderEFIBinaryDiffText(w, *m.EFIBinaries, "        ")
				}
			}
		}
	}

	// Global EFI diff roll-up
	if hasAnyEFIDiff(r.Diff.EFIBinaries) {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "EFI binaries (global):")
		renderEFIBinaryDiffText(w, r.Diff.EFIBinaries, "  ")
	}

	// dm-verity diff
	if r.Diff.Verity != nil && r.Diff.Verity.Changed {
		fmt.Fprintln(w)
		renderVerityDiffText(w, r.Diff.Verity)
	}

	// Full mode: image metadata & volatile / meaningful remove reasons
	if mode == "full" {
		renderImagesBlock(w, r.From, r.To)
		renderEqualityReasonsBlock(w, r)
	}

	return nil
}

func RenderSummaryText(w io.Writer, summary *ImageSummary, opts TextOptions) error {
	if summary == nil {
		return fmt.Errorf("RenderSummaryText: summary is nil")
	}

	// Header
	fmt.Fprintln(w, "OS Image Summary")
	fmt.Fprintln(w, "================")
	fmt.Fprintf(w, "Image:\t%s\n", summary.File)
	fmt.Fprintf(w, "Size:\t%s (%d bytes)\n", humanBytes(summary.SizeBytes), summary.SizeBytes)
	if strings.TrimSpace(summary.SHA256) != "" {
		fmt.Fprintf(w, "SHA256:\t%s\n", summary.SHA256)
	}
	// Partition table section
	renderPartitionTableHeader(w, summary.PartitionTable)

	// Partitions table (includes the “Partitions” header)
	renderPartitionTable(w, summary.PartitionTable)
	// dm-verity section
	if summary.Verity != nil && summary.Verity.Enabled {
		fmt.Fprintln(w)
		renderVerityInfo(w, summary.Verity)
	}
	// Detailed per-partition filesystem blocks (ONLY ONCE)
	for _, p := range summary.PartitionTable.Partitions {
		if p.Filesystem == nil || isFilesystemEmpty(p.Filesystem) {
			continue
		}
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Partition %d filesystem details\n", p.Index)
		fmt.Fprintln(w, "------------------------------")
		renderPartitionFilesystemDetails(w, p)
	}

	fmt.Fprintln(w)
	return nil
}

// renderPartitionSummaryLine prints a compact one-liner for a partition in compare output.
// NOTE: This is intentionally more compact than the inspect partition table.
func renderPartitionSummaryLine(w io.Writer, prefix string, p PartitionSummary) {
	fsType := "-"
	fsID := "-"

	if p.Filesystem != nil {
		if s := strings.TrimSpace(p.Filesystem.Type); s != "" {
			fsType = s
		}
		if strings.EqualFold(fsType, "vfat") && strings.TrimSpace(p.Filesystem.FATType) != "" {
			fsType = fmt.Sprintf("vfat(%s)", strings.TrimSpace(p.Filesystem.FATType))
		}

		lbl := strings.TrimSpace(p.Filesystem.Label)
		id := strings.TrimSpace(p.Filesystem.UUID)
		switch {
		case lbl != "" && id != "":
			fsID = fmt.Sprintf("%s (%s)", lbl, id)
		case lbl != "":
			fsID = lbl
		case id != "":
			fsID = id
		}
	}

	fmt.Fprintf(w, "%s idx=%d name=%q type=%q lba=%d-%d size=%s flags=%q fs=%s id=%q\n",
		prefix, p.Index, p.Name, p.Type, p.StartLBA, p.EndLBA, humanBytes(int64(p.SizeBytes)), p.Flags, fsType, fsID)
}

func renderFilesystemChangeText(w io.Writer, c *FilesystemChange) {
	if c == nil {
		return
	}
	if c.Added != nil {
		fmt.Fprintf(w, "      FS: added type=%s uuid=%s label=%s\n", c.Added.Type, c.Added.UUID, c.Added.Label)
		return
	}
	if c.Removed != nil {
		fmt.Fprintf(w, "      FS: removed type=%s uuid=%s label=%s\n", c.Removed.Type, c.Removed.UUID, c.Removed.Label)
		return
	}
	if c.Modified != nil {
		fmt.Fprintf(w, "      FS: modified %s(%s) -> %s(%s)\n",
			c.Modified.From.Type, c.Modified.From.UUID, c.Modified.To.Type, c.Modified.To.UUID)
		for _, ch := range c.Modified.Changes {
			fmt.Fprintf(w, "        %s: %v -> %v\n", ch.Field, ch.From, ch.To)
		}
	}
}

func renderEFIBinaryDiffText(w io.Writer, d EFIBinaryDiff, indent string) {
	if len(d.Added) > 0 {
		fmt.Fprintf(w, "%sAdded:\n", indent)
		for _, e := range d.Added {
			fmt.Fprintf(w, "%s  + %s kind=%s arch=%s signed=%v sha=%s\n",
				indent, e.Path, e.Kind, e.Arch, e.Signed, shortHash(e.SHA256))
		}
	}
	if len(d.Removed) > 0 {
		fmt.Fprintf(w, "%sRemoved:\n", indent)
		for _, e := range d.Removed {
			fmt.Fprintf(w, "%s  - %s kind=%s arch=%s signed=%v sha=%s\n",
				indent, e.Path, e.Kind, e.Arch, e.Signed, shortHash(e.SHA256))
		}
	}
	if len(d.Modified) > 0 {
		fmt.Fprintf(w, "%sModified:\n", indent)
		for _, m := range d.Modified {
			fmt.Fprintf(w, "%s  ~ %s\n", indent, m.Key)

			if m.From.Kind != m.To.Kind {
				fmt.Fprintf(w, "%s    kind: %s -> %s\n", indent, m.From.Kind, m.To.Kind)
			}
			if m.From.SHA256 != m.To.SHA256 {
				fmt.Fprintf(w, "%s    sha256: %s -> %s\n", indent, shortHash(m.From.SHA256), shortHash(m.To.SHA256))
				// If cmdline hash changed and we have raw strings, show them
				if m.From.Cmdline != "" || m.To.Cmdline != "" {
					if m.From.Cmdline != m.To.Cmdline {
						fmt.Fprintf(w, "%s    cmdline:\n", indent)
						fmt.Fprintf(w, "%s      from: %q\n", indent, m.From.Cmdline)
						fmt.Fprintf(w, "%s      to:   %q\n", indent, m.To.Cmdline)
					}
				}

			}
			if m.From.Signed != m.To.Signed {
				fmt.Fprintf(w, "%s    signed: %v -> %v\n", indent, m.From.Signed, m.To.Signed)
			}

			// UKI payload hashes (high-value)
			if m.UKI != nil && m.UKI.Changed {
				fmt.Fprintf(w, "%s    UKI payload:\n", indent)
				if m.UKI.KernelSHA256 != nil {
					fmt.Fprintf(w, "%s      kernel:  %s -> %s\n", indent,
						shortHash(m.UKI.KernelSHA256.From), shortHash(m.UKI.KernelSHA256.To))
				}
				if m.UKI.InitrdSHA256 != nil {
					fmt.Fprintf(w, "%s      initrd:  %s -> %s\n", indent,
						shortHash(m.UKI.InitrdSHA256.From), shortHash(m.UKI.InitrdSHA256.To))
				}
				if m.UKI.CmdlineSHA256 != nil {
					fmt.Fprintf(w, "%s      cmdline: %s -> %s\n", indent,
						shortHash(m.UKI.CmdlineSHA256.From), shortHash(m.UKI.CmdlineSHA256.To))
				}
				if m.UKI.OSRelSHA256 != nil {
					fmt.Fprintf(w, "%s      osrel:   %s -> %s\n", indent,
						shortHash(m.UKI.OSRelSHA256.From), shortHash(m.UKI.OSRelSHA256.To))
					// If os release raw changed and we have raw strings, show them
					if m.From.OSReleaseRaw != "" || m.To.OSReleaseRaw != "" {
						if m.From.OSReleaseRaw != m.To.OSReleaseRaw {
							fmt.Fprintf(w, "%s    os release raw:\n", indent)
							fmt.Fprintf(w, "%s      from: %q\n", indent, m.From.OSReleaseRaw)
							fmt.Fprintf(w, "%s      to:   %q\n", indent, m.To.OSReleaseRaw)
						}
					}
				}
				if m.UKI.UnameSHA256 != nil {
					fmt.Fprintf(w, "%s      uname:   %s -> %s\n", indent,
						shortHash(m.UKI.UnameSHA256.From), shortHash(m.UKI.UnameSHA256.To))
				}
			}

			// Bootloader configuration diff (if present)
			if m.BootConfig != nil {
				fmt.Fprintf(w, "%s    Bootloader config:\n", indent)
				renderBootloaderConfigDiffText(w, m.BootConfig, indent+"      ")
			}
		}
	}
}

func normalizeCompareMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "diff":
		return "diff"
	case "summary":
		return "summary"
	case "full":
		return "full"
	default:
		return "diff" // or return error; see below
	}
}

func hasAnyEFIDiff(d EFIBinaryDiff) bool {
	return len(d.Added) > 0 || len(d.Removed) > 0 || len(d.Modified) > 0
}

func computeObjectCountsFromDiff(d ImageDiff) objectCounts {
	var c objectCounts

	// Partitions
	c.added += len(d.Partitions.Added)
	c.removed += len(d.Partitions.Removed)
	c.modified += len(d.Partitions.Modified)

	// Global EFI binaries
	c.added += len(d.EFIBinaries.Added)
	c.removed += len(d.EFIBinaries.Removed)
	c.modified += len(d.EFIBinaries.Modified)

	// Optional: count filesystem changes as objects too
	for _, mp := range d.Partitions.Modified {
		if mp.Filesystem == nil {
			continue
		}
		switch {
		case mp.Filesystem.Added != nil:
			c.added++
		case mp.Filesystem.Removed != nil:
			c.removed++
		case mp.Filesystem.Modified != nil:
			c.modified++
		}
		// Optional: per-partition EFI diff as objects
		if mp.EFIBinaries != nil {
			c.added += len(mp.EFIBinaries.Added)
			c.removed += len(mp.EFIBinaries.Removed)
			c.modified += len(mp.EFIBinaries.Modified)
		}
	}

	return c
}

// renderPartitionTable prints a table of partitions in the partition table.
func renderPartitionTable(w io.Writer, pt PartitionTableSummary) {

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Partitions")
	fmt.Fprintln(w, "----------")

	if len(pt.Partitions) == 0 {
		fmt.Fprintln(w, "(none)")
		return
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "IDX\tNAME\tROLE\tPTYPE\tPTYPE_NAME\tSTART(LBA)\tEND(LBA)\tSIZE\tFLAGS\tFS\tLABEL/ID")
	for _, p := range pt.Partitions {
		fsType := "-"
		fsLabelOrID := "-"

		if p.Filesystem != nil {
			if s := strings.TrimSpace(p.Filesystem.Type); s != "" {
				fsType = s
			}
			// Show FAT type inline (vfat(FAT16/FAT32))
			if strings.EqualFold(fsType, "vfat") && strings.TrimSpace(p.Filesystem.FATType) != "" {
				fsType = fmt.Sprintf("vfat(%s)", strings.TrimSpace(p.Filesystem.FATType))
			}

			lbl := strings.TrimSpace(p.Filesystem.Label)
			id := strings.TrimSpace(p.Filesystem.UUID)
			switch {
			case lbl != "" && id != "":
				fsLabelOrID = fmt.Sprintf("%s (%s)", lbl, id)
			case lbl != "":
				fsLabelOrID = lbl
			case id != "":
				fsLabelOrID = id
			}
		}

		ptypeName := "-"
		if s := partitionTypeName(pt.Type, p.Type); s != "" {
			ptypeName = s
		}
		role := partitionRole(p)

		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%d\t%d\t%s\t%s\t%s\t%s\n",
			p.Index,
			emptyIfWhitespace(p.Name),
			role,
			emptyIfWhitespace(p.Type),
			ptypeName,
			p.StartLBA,
			p.EndLBA,
			humanBytes(int64(p.SizeBytes)),
			emptyIfWhitespace(p.Flags),
			fsType,
			fsLabelOrID,
		)
	}
	_ = tw.Flush()
}

func renderPartitionFilesystemDetails(w io.Writer, p PartitionSummary) {

	fs := p.Filesystem
	if fs == nil {
		return
	}

	// Key/value lines
	kv := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintf(kv, "FS type:\t%s\n", emptyIfWhitespace(fs.Type))
	if strings.TrimSpace(fs.Label) != "" {
		fmt.Fprintf(kv, "Label:\t%s\n", fs.Label)
	}
	if strings.TrimSpace(fs.UUID) != "" {
		fmt.Fprintf(kv, "UUID/ID:\t%s\n", fs.UUID)
	}
	if fs.BlockSize > 0 {
		fmt.Fprintf(kv, "Block size:\t%d\n", fs.BlockSize)
	}
	if len(fs.Features) > 0 {
		fmt.Fprintf(kv, "Features:\t%s\n", strings.Join(fs.Features, ", "))
	}

	// VFAT-specific
	if isVFATLike(fs.Type) {
		if fs.FATType != "" {
			fmt.Fprintf(kv, "FAT type:\t%s\n", fs.FATType)
		}
		if fs.BytesPerSector != 0 {
			fmt.Fprintf(kv, "Bytes/sector:\t%d\n", fs.BytesPerSector)
		}
		if fs.SectorsPerCluster != 0 {
			fmt.Fprintf(kv, "Sectors/cluster:\t%d\n", fs.SectorsPerCluster)
		}
		if fs.ClusterCount != 0 {
			fmt.Fprintf(kv, "Clusters:\t%d\n", fs.ClusterCount)
		}
		if fs.BytesPerSector != 0 && fs.SectorsPerCluster != 0 {
			clusterSize := uint64(fs.BytesPerSector) * uint64(fs.SectorsPerCluster)
			fmt.Fprintf(kv, "Cluster size:\t%s (%d bytes)\n", humanBytes(int64(clusterSize)), clusterSize)
		}
	}

	// Print shim/UKI flags only if true (avoid noise)
	if fs.HasShim {
		fmt.Fprintf(kv, "Shim detected:\t%t\n", fs.HasShim)
	}
	if fs.HasUKI {
		fmt.Fprintf(kv, "UKI detected:\t%t\n", fs.HasUKI)
	}
	_ = kv.Flush()

	// EFI artifacts table (preferred)
	if isVFATLike(fs.Type) && len(fs.EFIBinaries) > 0 {

		renderEFIArtifactsTable(w, fs.EFIBinaries)

		// Sort by path for stable output
		arts := append([]EFIBinaryEvidence(nil), fs.EFIBinaries...)
		sort.Slice(arts, func(i, j int) bool { return arts[i].Path < arts[j].Path })

		// Print a focused UKI block for the first UKI found
		if uki, ok := firstUKI(arts); ok {
			renderUKIDetailsBlock(w, uki)
		}

		// Print bootloader config details if present
		for _, art := range arts {
			if art.BootConfig != nil {
				renderBootloaderConfigDetails(w, art.Path, art.BootConfig)
			}
		}
	}

	// Squashfs-specific
	if strings.EqualFold(fs.Type, "squashfs") {
		fmt.Fprintln(w)
		kv3 := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
		if fs.Compression != "" {
			fmt.Fprintf(kv3, "Compression:\t%s\n", fs.Compression)
		}
		if fs.Version != "" {
			fmt.Fprintf(kv3, "Version:\t%s\n", fs.Version)
		}
		if len(fs.FsFlags) > 0 {
			fmt.Fprintf(kv3, "Flags:\t%s\n", strings.Join(fs.FsFlags, ", "))
		}
		_ = kv3.Flush()
	}

	// Notes
	if len(fs.Notes) > 0 {
		fmt.Fprintln(w, "Notes:")
		for _, note := range fs.Notes {
			fmt.Fprintf(w, "  - %s\n", note)
		}
	}
}

func renderPartitionTableHeader(w io.Writer, pt PartitionTableSummary) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Partition Table")
	fmt.Fprintln(w, "---------------")

	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintf(tw, "Type:\t%s\n", strings.ToUpper(emptyIfWhitespace(pt.Type)))
	if strings.EqualFold(pt.Type, "gpt") && strings.TrimSpace(pt.DiskGUID) != "" {
		fmt.Fprintf(tw, "Disk GUID:\t%s\n", strings.ToUpper(strings.TrimSpace(pt.DiskGUID)))
	}
	if pt.LogicalSectorSize > 0 {
		fmt.Fprintf(tw, "Logical sector size:\t%d bytes\n", pt.LogicalSectorSize)
	}
	if pt.PhysicalSectorSize > 0 {
		fmt.Fprintf(tw, "Physical sector size:\t%d bytes\n", pt.PhysicalSectorSize)
	}
	if strings.EqualFold(pt.Type, "gpt") {
		fmt.Fprintf(tw, "Protective MBR:\t%t\n", pt.ProtectiveMBR)
	}
	if pt.LargestFreeSpan != nil {
		fmt.Fprintf(tw, "Largest free span:\t%s\n", freeSpanString(pt.LargestFreeSpan))
	}
	if len(pt.MisalignedPartitions) > 0 {
		fmt.Fprintf(tw, "Misaligned partitions:\t%v\n", pt.MisalignedPartitions)
	}
	_ = tw.Flush()
}

func renderVerityInfo(w io.Writer, v *VerityInfo) {
	fmt.Fprintln(w, "dm-verity Configuration")
	fmt.Fprintln(w, "-----------------------")

	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintf(tw, "Enabled:\t%t\n", v.Enabled)
	if v.Method != "" {
		fmt.Fprintf(tw, "Method:\t%s\n", v.Method)
	}
	if v.RootDevice != "" {
		fmt.Fprintf(tw, "Root device:\t%s\n", v.RootDevice)
	}
	if v.HashPartition > 0 {
		fmt.Fprintf(tw, "Hash partition:\t%d\n", v.HashPartition)
	}
	_ = tw.Flush()

	if len(v.Notes) > 0 {
		fmt.Fprintln(w, "Notes:")
		for _, note := range v.Notes {
			fmt.Fprintf(w, "  - %s\n", note)
		}
	}
}

func renderVerityDiffText(w io.Writer, d *VerityDiff) {
	fmt.Fprintln(w, "dm-verity:")

	if d.Added != nil {
		fmt.Fprintln(w, "  dm-verity ENABLED:")
		tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
		fmt.Fprintf(tw, "    Method:\t%s\n", d.Added.Method)
		if d.Added.RootDevice != "" {
			fmt.Fprintf(tw, "    Root device:\t%s\n", d.Added.RootDevice)
		}
		if d.Added.HashPartition > 0 {
			fmt.Fprintf(tw, "    Hash partition:\t%d\n", d.Added.HashPartition)
		}
		_ = tw.Flush()
		return
	}

	if d.Removed != nil {
		fmt.Fprintln(w, "  dm-verity DISABLED")
		fmt.Fprintf(w, "    Previous method: %s\n", d.Removed.Method)
		return
	}

	// Field changes
	if d.Enabled != nil {
		fmt.Fprintf(w, "  Enabled: %t -> %t\n", d.Enabled.From, d.Enabled.To)
	}
	if d.Method != nil {
		fmt.Fprintf(w, "  Method: %s -> %s\n", d.Method.From, d.Method.To)
	}
	if d.RootDevice != nil {
		fmt.Fprintf(w, "  Root device: %s -> %s\n", d.RootDevice.From, d.RootDevice.To)
	}
	if d.HashPartition != nil {
		fmt.Fprintf(w, "  Hash partition: %d -> %d\n", d.HashPartition.From, d.HashPartition.To)
	}
}

func renderEFIArtifactsTable(w io.Writer, arts []EFIBinaryEvidence) {

	fmt.Fprintln(w)
	fmt.Fprintf(w, "EFI artifacts:\t%d\n", len(arts))

	tw2 := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw2, "KIND\tSIGNED\tARCH\tPATH\tSIZE\tSHA256\tKERNEL\tINITRD")
	for _, a := range arts {
		kernel := "-"
		initrd := "-"
		if a.KernelSHA256 != "" {
			kernel = shortHash(a.KernelSHA256)
		}
		if a.InitrdSHA256 != "" {
			initrd = shortHash(a.InitrdSHA256)
		}
		fmt.Fprintf(tw2, "%s\t%t\t%s\t%s\t%s\t%s\t%s\t%s\n",
			emptyOr(string(a.Kind), "unknown"),
			a.Signed,
			emptyOr(a.Arch, "-"),
			emptyIfWhitespace(a.Path),
			humanBytes(a.Size),
			shortHash(a.SHA256),
			kernel,
			initrd,
		)
	}
	_ = tw2.Flush()
}

func renderUKIDetailsBlock(w io.Writer, uki EFIBinaryEvidence) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "UKI details")
	fmt.Fprintln(w, "-----------")
	kv2 := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintf(kv2, "Path:\t%s\n", uki.Path)
	if uki.Arch != "" {
		fmt.Fprintf(kv2, "Architecture:\t%s\n", uki.Arch)
	}
	if uki.Uname != "" {
		fmt.Fprintf(kv2, "EFI uname:\t%s\n", uki.Uname)
	}
	if uki.Cmdline != "" {
		fmt.Fprintf(kv2, "EFI cmdline:\t%s\n", uki.Cmdline)
	}
	if uki.KernelSHA256 != "" {
		fmt.Fprintf(kv2, "Kernel SHA256:\t%s\n", uki.KernelSHA256)
	}
	if uki.InitrdSHA256 != "" {
		fmt.Fprintf(kv2, "Initrd SHA256:\t%s\n", uki.InitrdSHA256)
	}
	if len(uki.OSReleaseSorted) > 0 {
		_ = kv2.Flush()

		printOSReleaseKV(w, "EFI OS release:", uki.OSReleaseSorted)

	} else if uki.OSReleaseRaw != "" {
		// fallback: raw only if we couldn't parse anything
		_ = kv2.Flush()
		fmt.Fprintln(w)
		fmt.Fprintln(w, "EFI OS release:")
		fmt.Fprintln(w, uki.OSReleaseRaw)

	} else {
		_ = kv2.Flush()
	}
}

// renderEqualityReasonsBlock prints the equality reasons.
func renderEqualityReasonsBlock(w io.Writer, r *ImageCompareResult) {
	if r == nil {
		return
	}

	if len(r.Equality.VolatileReasons) == 0 && len(r.Equality.MeaningfulReasons) == 0 {
		return
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Equality reasons")
	fmt.Fprintln(w, "----------------")

	if len(r.Equality.VolatileReasons) > 0 {
		fmt.Fprintf(w, "Volatile reasons (%d):\n", len(r.Equality.VolatileReasons))
		for _, reason := range r.Equality.VolatileReasons {
			fmt.Fprintf(w, "  - %s\n", reason)
		}
	}

	if len(r.Equality.MeaningfulReasons) > 0 {
		fmt.Fprintf(w, "\nMeaningful reasons (%d):\n", len(r.Equality.MeaningfulReasons))
		for _, reason := range r.Equality.MeaningfulReasons {
			fmt.Fprintf(w, "  - %s\n", reason)
		}
	}
}

// renderBootloaderConfigDetails renders bootloader configuration details for a single image.
func renderBootloaderConfigDetails(w io.Writer, efiPath string, cfg *BootloaderConfig) {
	if cfg == nil {
		return
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Bootloader configuration")
	fmt.Fprintln(w, "------------------------")

	// Config file hashes and content preview
	if len(cfg.ConfigFiles) > 0 {
		fmt.Fprintf(w, "EFI Path: %s\n", efiPath)
		fmt.Fprintln(w, "Config files:")

		// Collect and sort paths for deterministic output
		paths := make([]string, 0, len(cfg.ConfigFiles))
		for path := range cfg.ConfigFiles {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			hash := cfg.ConfigFiles[path]
			fmt.Fprintf(w, "  %s: %s\n", path, shortHash(hash))

			// Show raw config content (first 30 lines or up to 2KB)
			if raw, ok := cfg.ConfigRaw[path]; ok && raw != "" {
				lines := strings.Split(raw, "\n")
				maxLines := 30
				if len(lines) > maxLines {
					lines = lines[:maxLines]
				}

				fmt.Fprintln(w, "    Content preview:")
				for _, line := range lines {
					if strings.TrimSpace(line) != "" {
						// Truncate very long lines
						if len(line) > 100 {
							fmt.Fprintf(w, "      %s...\n", line[:97])
						} else {
							fmt.Fprintf(w, "      %s\n", line)
						}
					}
				}
				if len(cfg.ConfigRaw[path]) > 2048 {
					fmt.Fprintf(w, "      [... config truncated, total size: %d bytes]\n", len(cfg.ConfigRaw[path]))
				}
			}
		}
	}

	// Display boot entries
	if len(cfg.BootEntries) > 0 {
		fmt.Fprintln(w, "Boot entries:")
		for i, entry := range cfg.BootEntries {
			mark := " "
			if entry.IsDefault {
				mark = "*"
			}
			fmt.Fprintf(w, "  %s [%d] %s\n", mark, i+1, entry.Name)
			if entry.Kernel != "" {
				fmt.Fprintf(w, "       kernel: %s\n", entry.Kernel)
			}
			if entry.Initrd != "" {
				fmt.Fprintf(w, "       initrd: %s\n", entry.Initrd)
			}
			if entry.RootDevice != "" {
				fmt.Fprintf(w, "       root:   %s\n", entry.RootDevice)
			}
			if entry.Cmdline != "" {
				if len(entry.Cmdline) > 100 {
					fmt.Fprintf(w, "       cmdline: %s...\n", entry.Cmdline[:97])
				} else {
					fmt.Fprintf(w, "       cmdline: %s\n", entry.Cmdline)
				}
			}
		}
		if cfg.DefaultEntry != "" {
			fmt.Fprintf(w, "  Default: %s\n", cfg.DefaultEntry)
		}
	}

	// Display kernel references
	if len(cfg.KernelReferences) > 0 {
		fmt.Fprintln(w, "Kernel references:")
		for _, ref := range cfg.KernelReferences {
			fmt.Fprintf(w, "  %s\n", ref.Path)
			if ref.PartitionUUID != "" {
				fmt.Fprintf(w, "    partition uuid: %s\n", ref.PartitionUUID)
			}
			if ref.RootUUID != "" {
				fmt.Fprintf(w, "    root uuid:      %s\n", ref.RootUUID)
			}
			if ref.BootEntry != "" {
				fmt.Fprintf(w, "    boot entry:     %s\n", ref.BootEntry)
			}
		}
	}

	// Display UUID references with validation status
	if len(cfg.UUIDReferences) > 0 {
		hasIssues := false
		for _, ref := range cfg.UUIDReferences {
			if ref.Mismatch {
				hasIssues = true
				break
			}
		}

		if hasIssues {
			fmt.Fprintln(w, "UUID validation:")
			for _, ref := range cfg.UUIDReferences {
				status := "✓"
				if ref.Mismatch {
					status = "✗ MISMATCH"
				}
				fmt.Fprintf(w, "  [%s] %s (%s)\n", status, ref.UUID, ref.Context)
				if ref.ReferencedPartition > 0 {
					fmt.Fprintf(w, "       -> partition %d\n", ref.ReferencedPartition)
				}
			}
		}
	}

	// Display validation notes
	if len(cfg.Notes) > 0 {
		fmt.Fprintln(w, "Notes:")
		for _, note := range cfg.Notes {
			fmt.Fprintf(w, "  - %s\n", note)
		}
	}
}

// renderBootloaderConfigDiffText renders bootloader configuration differences in a comparison.
func renderBootloaderConfigDiffText(w io.Writer, diff *BootloaderConfigDiff, indent string) {
	if diff == nil {
		return
	}

	// Config file changes
	if len(diff.ConfigFileChanges) > 0 {
		fmt.Fprintf(w, "%sConfig files:\n", indent)
		for _, change := range diff.ConfigFileChanges {
			status := change.Status
			fmt.Fprintf(w, "%s  %s: %s\n", indent, status, change.Path)
			if status == "modified" {
				fmt.Fprintf(w, "%s    hash: %s -> %s\n", indent, shortHash(change.HashFrom), shortHash(change.HashTo))
			}
		}
	}

	// Boot entry changes
	if len(diff.BootEntryChanges) > 0 {
		fmt.Fprintf(w, "%sBoot entries:\n", indent)
		for _, change := range diff.BootEntryChanges {
			fmt.Fprintf(w, "%s  %s: %s\n", indent, change.Status, change.Name)
			if change.Status == "modified" {
				if change.KernelFrom != change.KernelTo {
					fmt.Fprintf(w, "%s    kernel: %s -> %s\n", indent, change.KernelFrom, change.KernelTo)
				}
				if change.InitrdFrom != change.InitrdTo {
					fmt.Fprintf(w, "%s    initrd: %s -> %s\n", indent, change.InitrdFrom, change.InitrdTo)
				}
				if change.CmdlineFrom != change.CmdlineTo {
					fromCmdline := change.CmdlineFrom
					toCmdline := change.CmdlineTo
					if len(fromCmdline) > 80 {
						fromCmdline = fromCmdline[:77] + "..."
					}
					if len(toCmdline) > 80 {
						toCmdline = toCmdline[:77] + "..."
					}
					fmt.Fprintf(w, "%s    cmdline: %s -> %s\n", indent, fromCmdline, toCmdline)
				}
			}
		}
	}

	// Kernel reference changes
	if len(diff.KernelRefChanges) > 0 {
		fmt.Fprintf(w, "%sKernel references:\n", indent)
		for _, change := range diff.KernelRefChanges {
			fmt.Fprintf(w, "%s  %s: %s\n", indent, change.Status, change.Path)
			if change.Status == "modified" && change.UUIDFrom != change.UUIDTo {
				fmt.Fprintf(w, "%s    uuid: %s -> %s\n", indent, change.UUIDFrom, change.UUIDTo)
			}
		}
	}

	// UUID reference changes
	if len(diff.UUIDReferenceChanges) > 0 {
		hasMismatch := false
		for _, change := range diff.UUIDReferenceChanges {
			if change.MismatchTo {
				hasMismatch = true
				break
			}
		}

		if hasMismatch {
			fmt.Fprintf(w, "%sUUID validation:\n", indent)
			for _, change := range diff.UUIDReferenceChanges {
				if change.MismatchTo {
					fmt.Fprintf(w, "%s  ✗ CRITICAL: %s not found in partition table\n", indent, change.UUID)
					if change.ContextTo != "" {
						fmt.Fprintf(w, "%s    context: %s\n", indent, change.ContextTo)
					}
				}
			}
		}
	}

	// Configuration notes
	if len(diff.NotesAdded) > 0 {
		fmt.Fprintf(w, "%sNew issues:\n", indent)
		for _, note := range diff.NotesAdded {
			fmt.Fprintf(w, "%s  - %s\n", indent, note)
		}
	}

	if len(diff.NotesRemoved) > 0 {
		fmt.Fprintf(w, "%sResolved issues:\n", indent)
		for _, note := range diff.NotesRemoved {
			fmt.Fprintf(w, "%s  - %s\n", indent, note)
		}
	}
}

func humanBytes(n int64) string {
	if n < 0 {
		return fmt.Sprintf("%d B", n)
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func freeSpanString(fs *FreeSpanSummary) string {
	if fs == nil {
		return "(none)"
	}
	return fmt.Sprintf("lba=%d-%d size=%s", fs.StartLBA, fs.EndLBA, humanBytes(int64(fs.SizeBytes)))
}

func partitionTypeName(ptType, pType string) string {
	ptType = strings.ToLower(strings.TrimSpace(ptType))
	switch ptType {
	case "gpt":
		return gptTypeName(pType)
	case "mbr":
		return mbrTypeName(pType)
	default:
		return ""
	}
}

func gptTypeName(guid string) string {
	switch strings.ToUpper(strings.TrimSpace(guid)) {
	case "C12A7328-F81F-11D2-BA4B-00A0C93EC93B":
		return "EFI System Partition"
	case "4F68BCE3-E8CD-4DB1-96E7-FBCAF984B709":
		return "Linux root (x86-64)"
	case "0FC63DAF-8483-4772-8E79-3D69D8477DE4":
		return "Linux filesystem"
	case "21686148-6449-6E6F-744E-656564454649":
		return "BIOS boot partition"
	default:
		return ""
	}
}

func mbrTypeName(code string) string {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "0x07":
		return "HPFS/NTFS/exFAT"
	case "0x0b":
		return "W95 FAT32"
	case "0x0c":
		return "W95 FAT32 (LBA)"
	case "0x0e":
		return "W95 FAT16 (LBA)"
	case "0x82":
		return "Linux swap"
	case "0x83":
		return "Linux filesystem"
	case "0x8e":
		return "Linux LVM"
	case "0xaf":
		return "Apple HFS/HFS+"
	default:
		return ""
	}
}
func partitionRole(p PartitionSummary) string {

	// Light heuristic: prefer GPT type, then FS type.
	if name := gptTypeName(p.Type); name != "" {
		if name == "EFI System Partition" {
			return "ESP"
		}
		if strings.HasPrefix(name, "Linux root") {
			return "ROOT"
		}
		if name == "Linux filesystem" {
			// you can specialize based on name
			if strings.Contains(strings.ToLower(p.Name), "userdata") {
				return "DATA"
			}
			return "FS"
		}
		return name
	}

	if p.Filesystem != nil {
		switch strings.ToLower(p.Filesystem.Type) {
		case "vfat":
			return "ESP?"
		case "ext4":
			return "FS"
		case "squashfs":
			return "SQUASHFS"
		}
	}
	return "-"
}

func isFilesystemEmpty(fs *FilesystemSummary) bool {
	if fs == nil {
		return true
	}
	// If these are all empty/zero, there’s nothing worth printing.
	return strings.TrimSpace(fs.Type) == "" &&
		strings.TrimSpace(fs.Label) == "" &&
		strings.TrimSpace(fs.UUID) == "" &&
		fs.BlockSize == 0 &&
		len(fs.Features) == 0 &&
		len(fs.Notes) == 0 &&
		fs.FATType == "" &&
		fs.BytesPerSector == 0 &&
		fs.SectorsPerCluster == 0 &&
		fs.Compression == "" &&
		fs.Version == "" &&
		len(fs.FsFlags) == 0 &&
		!fs.HasShim &&
		!fs.HasUKI
}

func emptyIfWhitespace(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return strings.TrimSpace(s)
}

func emptyOr(s, fallback string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return fallback
	}
	return s
}
func shortHash(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 12 {
		return s
	}
	return s[:12]
}

func firstUKI(arts []EFIBinaryEvidence) (EFIBinaryEvidence, bool) {
	for _, a := range arts {
		if a.IsUKI || a.Kind == BootloaderUKI {
			return a, true
		}
	}
	return EFIBinaryEvidence{}, false
}

func printOSReleaseKV(w io.Writer, title string, kvs []KeyValue) {
	if len(kvs) == 0 {
		return
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, title)

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, kv := range kvs {
		fmt.Fprintf(tw, "%s:\t%q\n", kv.Key, kv.Value)
	}
	_ = tw.Flush()
}

func renderEqualityHeader(w io.Writer, r *ImageCompareResult) {
	// Primary classification
	cls := strings.TrimSpace(string(r.Equality.Class))
	if cls == "" {
		cls = "(unset)"
	}

	label := map[EqualityClass]string{
		EqualityBinary:     "Binary identical",
		EqualitySemantic:   "Semantically identical",
		EqualityUnverified: "Semantically identical (unverified)",
		EqualityDifferent:  "Different",
	}[r.Equality.Class]

	if label == "" {
		label = cls
	}

	fmt.Fprintf(w, "Equality: %s (%s)\n", label, cls)

	// Warn if claiming binary identity without hashes
	if r.Equality.Class == EqualityUnverified {
		fmt.Fprintln(w, "Note: image SHA256 not available; enable --hash-images to prove binary identity.")
	}

	// Hint where to look when different
	if r.Equality.Class == EqualityDifferent {
		var areas []string

		if r.Summary.PartitionTableChanged {
			areas = append(areas, "partition table")
		}
		if r.Summary.PartitionsChanged {
			areas = append(areas, "partitions/filesystems")
		}
		if r.Summary.EFIBinariesChanged {
			areas = append(areas, "EFI binaries / boot artifacts")
		}

		if len(areas) > 0 {
			fmt.Fprintf(w, "Most likely differences in: %s\n", strings.Join(areas, ", "))
		}
	}

	// Hash display (or hint)
	fromS := strings.TrimSpace(r.From.SHA256)
	toS := strings.TrimSpace(r.To.SHA256)

	switch {
	case fromS != "" || toS != "":
		fromH := "-"
		toH := "-"
		if fromS != "" {
			fromH = shortHash(fromS)
		}
		if toS != "" {
			toH = shortHash(toS)
		}
		fmt.Fprintf(w, "Image SHA256: from=%s to=%s\n", fromH, toH)

	default:
		if r.Equality.Class == EqualityBinary || r.Equality.Class == EqualitySemantic {
			fmt.Fprintln(w, "Image SHA256: (not computed)")
		}
	}
}

func renderImagesBlock(w io.Writer, from, to ImageSummary) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Images:")
	fmt.Fprintf(w, "  From: %s (%s)\n", from.File, humanBytes(from.SizeBytes))
	if strings.TrimSpace(from.SHA256) != "" {
		fmt.Fprintf(w, "        SHA256: %s\n", from.SHA256)
	}
	fmt.Fprintf(w, "  To:   %s (%s)\n", to.File, humanBytes(to.SizeBytes))
	if strings.TrimSpace(to.SHA256) != "" {
		fmt.Fprintf(w, "        SHA256: %s\n", to.SHA256)
	}
}
