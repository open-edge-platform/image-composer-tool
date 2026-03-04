package imageinspect

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// ImageCompareResult represents the result of comparing two images.
type ImageCompareResult struct {
	SchemaVersion string `json:"schemaVersion,omitempty"`

	From ImageSummary `json:"from"`
	To   ImageSummary `json:"to"`

	Equality Equality       `json:"equality" yaml:"equality"`
	Summary  CompareSummary `json:"summary,omitempty"`
	Diff     ImageDiff      `json:"diff,omitempty"`
}

// CompareSummary provides a high-level summary of differences between two images.
type CompareSummary struct {
	Changed bool `json:"changed,omitempty"`

	PartitionTableChanged bool `json:"partitionTableChanged,omitempty"`
	PartitionsChanged     bool `json:"partitionsChanged,omitempty"`
	FilesystemsChanged    bool `json:"filesystemsChanged,omitempty"`
	EFIBinariesChanged    bool `json:"efiBinariesChanged,omitempty"`

	AddedCount    int `json:"addedCount,omitempty"`
	RemovedCount  int `json:"removedCount,omitempty"`
	ModifiedCount int `json:"modifiedCount,omitempty"`
}

// ImageDiff represents the differences between two ImageSummary objects.
type ImageDiff struct {
	Image          MetaDiff           `json:"image,omitempty"`
	PartitionTable PartitionTableDiff `json:"partitionTable,omitempty"`
	Partitions     PartitionDiff      `json:"partitions,omitempty"`
	EFIBinaries    EFIBinaryDiff      `json:"efiBinaries,omitempty"`
	Verity         *VerityDiff        `json:"verity,omitempty" yaml:"verity,omitempty"`
}

// MetaDiff represents differences in image-level metadata.
type MetaDiff struct {
	SizeBytes *ValueDiff[int64] `json:"sizeBytes,omitempty"`
}

// VerityDiff represents differences in dm-verity configuration.
type VerityDiff struct {
	Added   *VerityInfo `json:"added,omitempty" yaml:"added,omitempty"`
	Removed *VerityInfo `json:"removed,omitempty" yaml:"removed,omitempty"`
	Changed bool        `json:"changed,omitempty" yaml:"changed,omitempty"`

	Enabled       *ValueDiff[bool]   `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	Method        *ValueDiff[string] `json:"method,omitempty" yaml:"method,omitempty"`
	RootDevice    *ValueDiff[string] `json:"rootDevice,omitempty" yaml:"rootDevice,omitempty"`
	HashPartition *ValueDiff[int]    `json:"hashPartition,omitempty" yaml:"hashPartition,omitempty"`
}

// PartitionTableDiff represents differences in partition table-level fields.
type PartitionTableDiff struct {
	DiskGUID           *ValueDiff[string]          `json:"diskGuid,omitempty"`
	Type               *ValueDiff[string]          `json:"type,omitempty"`
	LogicalSectorSize  *ValueDiff[int64]           `json:"logicalSectorSize,omitempty"`
	PhysicalSectorSize *ValueDiff[int64]           `json:"physicalSectorSize,omitempty"`
	ProtectiveMBR      *ValueDiff[bool]            `json:"protectiveMbr,omitempty"`
	LargestFreeSpan    *ValueDiff[FreeSpanSummary] `json:"largestFreeSpan,omitempty"`
	MisalignedParts    *ValueDiff[[]int]           `json:"misalignedPartitions,omitempty"`

	Changed bool `json:"changed,omitempty"`
}

// EqualityClass represents the class of equality between two images.
type EqualityClass string

// Possible values for EqualityClass
const (
	EqualityBinary     EqualityClass = "binary_identical"
	EqualitySemantic   EqualityClass = "semantically_identical"
	EqualityUnverified EqualityClass = "semantically_identical_unverified"
	EqualityDifferent  EqualityClass = "different"
)

// Equality represents the equality assessment between two images.
type Equality struct {
	Class EqualityClass `json:"class" yaml:"class"`

	VolatileDiffs     int      `json:"volatileDiffs,omitempty" yaml:"volatileDiffs,omitempty"`
	MeaningfulDiffs   int      `json:"meaningfulDiffs,omitempty" yaml:"meaningfulDiffs,omitempty"`
	VolatileReasons   []string `json:"volatileReasons,omitempty" yaml:"volatileReasons,omitempty"`
	MeaningfulReasons []string `json:"meaningfulReasons,omitempty" yaml:"meaningfulReasons,omitempty"`
}

// ValueDiff represents a difference in a single value between two objects.
type ValueDiff[T any] struct {
	From T `json:"from"`
	To   T `json:"to"`
}

// diffTally helps tally volatile vs meaningful diffs.
type diffTally struct {
	volatile   int
	meaningful int

	// debug: record reasons
	vReasons []string
	mReasons []string
}

// Helper methods to add to the tally (volatile)
func (t *diffTally) addVolatile(n int, reason string) {
	t.volatile += n
	if reason != "" {
		t.vReasons = append(t.vReasons, fmt.Sprintf("+%d %s", n, reason))
	}
}

// Helper methods to add to the tally (meaningful)
func (t *diffTally) addMeaningful(n int, reason string) {
	t.meaningful += n
	if reason != "" {
		t.mReasons = append(t.mReasons, fmt.Sprintf("+%d %s", n, reason))
	}
}

// FieldChange represents a change in a single field between two objects.
type FieldChange struct {
	Field string `json:"field"`
	From  any    `json:"from,omitempty"`
	To    any    `json:"to,omitempty"`
}

// PartitionDiff represents added, removed, and modified partitions.
type PartitionDiff struct {
	Added    []PartitionSummary         `json:"added,omitempty"`
	Removed  []PartitionSummary         `json:"removed,omitempty"`
	Modified []ModifiedPartitionSummary `json:"modified,omitempty"`
}

// ModifiedPartitionSummary represents changes between two PartitionSummary objects.
type ModifiedPartitionSummary struct {
	Key     string           `json:"key"`
	From    PartitionSummary `json:"from"`
	To      PartitionSummary `json:"to"`
	Changes []FieldChange    `json:"changes,omitempty"`

	Filesystem  *FilesystemChange `json:"filesystem,omitempty"`
	EFIBinaries *EFIBinaryDiff    `json:"efiBinaries,omitempty"`
}

// Filesystem changes
type FilesystemChange struct {
	Added    *FilesystemSummary         `json:"added,omitempty"`
	Removed  *FilesystemSummary         `json:"removed,omitempty"`
	Modified *ModifiedFilesystemSummary `json:"modified,omitempty"`
}

// ModifiedFilesystemSummary represents changes between two FilesystemSummary objects.
type ModifiedFilesystemSummary struct {
	From    FilesystemSummary `json:"from"`
	To      FilesystemSummary `json:"to"`
	Changes []FieldChange     `json:"changes,omitempty"`
}

// EFI binaries
type EFIBinaryDiff struct {
	Added    []EFIBinaryEvidence         `json:"added,omitempty"`
	Removed  []EFIBinaryEvidence         `json:"removed,omitempty"`
	Modified []ModifiedEFIBinaryEvidence `json:"modified,omitempty"`
}

// ModifiedEFIBinaryEvidence represents a modified EFI binary evidence entry.
type ModifiedEFIBinaryEvidence struct {
	Key     string            `json:"key"`
	From    EFIBinaryEvidence `json:"from"`
	To      EFIBinaryEvidence `json:"to"`
	Changes []FieldChange     `json:"changes,omitempty"`

	UKI        *UKIDiff              `json:"uki,omitempty"`
	BootConfig *BootloaderConfigDiff `json:"bootConfig,omitempty"`
}

// UKIDiff represents differences in the UKI-related fields of an EFI binary.
type UKIDiff struct {
	KernelSHA256  *ValueDiff[string] `json:"kernelSha256,omitempty"`
	InitrdSHA256  *ValueDiff[string] `json:"initrdSha256,omitempty"`
	CmdlineSHA256 *ValueDiff[string] `json:"cmdlineSha256,omitempty"`
	OSRelSHA256   *ValueDiff[string] `json:"osrelSha256,omitempty"`
	UnameSHA256   *ValueDiff[string] `json:"unameSha256,omitempty"`

	SectionSHA256 SectionMapDiff `json:"sectionSha256,omitempty"`

	Changed bool `json:"changed,omitempty"`
}

// SectionMapDiff represents differences in a map of section names to their SHA256 hashes.
type SectionMapDiff struct {
	Added    map[string]string            `json:"added,omitempty"`
	Removed  map[string]string            `json:"removed,omitempty"`
	Modified map[string]ValueDiff[string] `json:"modified,omitempty"`
}

// BootloaderConfigDiff represents differences in bootloader configuration.
type BootloaderConfigDiff struct {
	ConfigFileChanges    []ConfigFileChange `json:"configFileChanges,omitempty"`
	BootEntryChanges     []BootEntryChange  `json:"bootEntryChanges,omitempty"`
	KernelRefChanges     []KernelRefChange  `json:"kernelRefChanges,omitempty"`
	UUIDReferenceChanges []UUIDRefChange    `json:"uuidReferenceChanges,omitempty"`
	NotesAdded           []string           `json:"notesAdded,omitempty"`
	NotesRemoved         []string           `json:"notesRemoved,omitempty"`
}

// ConfigFileChange represents a change to a bootloader config file.
type ConfigFileChange struct {
	Path     string `json:"path" yaml:"path"`
	Status   string `json:"status" yaml:"status"` // "added", "removed", "modified"
	HashFrom string `json:"hashFrom,omitempty" yaml:"hashFrom,omitempty"`
	HashTo   string `json:"hashTo,omitempty" yaml:"hashTo,omitempty"`
}

// BootEntryChange represents a change to a boot entry.
type BootEntryChange struct {
	Name        string `json:"name" yaml:"name"`
	Status      string `json:"status" yaml:"status"` // "added", "removed", "modified"
	KernelFrom  string `json:"kernelFrom,omitempty" yaml:"kernelFrom,omitempty"`
	KernelTo    string `json:"kernelTo,omitempty" yaml:"kernelTo,omitempty"`
	InitrdFrom  string `json:"initrdFrom,omitempty" yaml:"initrdFrom,omitempty"`
	InitrdTo    string `json:"initrdTo,omitempty" yaml:"initrdTo,omitempty"`
	CmdlineFrom string `json:"cmdlineFrom,omitempty" yaml:"cmdlineFrom,omitempty"`
	CmdlineTo   string `json:"cmdlineTo,omitempty" yaml:"cmdlineTo,omitempty"`
}

// KernelRefChange represents a change to a kernel reference.
type KernelRefChange struct {
	Path     string `json:"path" yaml:"path"`
	Status   string `json:"status" yaml:"status"` // "added", "removed", "modified"
	UUIDFrom string `json:"uuidFrom,omitempty" yaml:"uuidFrom,omitempty"`
	UUIDTo   string `json:"uuidTo,omitempty" yaml:"uuidTo,omitempty"`
}

// UUIDRefChange represents a change to a UUID reference.
type UUIDRefChange struct {
	UUID         string `json:"uuid" yaml:"uuid"`
	Status       string `json:"status" yaml:"status"` // "added", "removed", "modified"
	ContextFrom  string `json:"contextFrom,omitempty" yaml:"contextFrom,omitempty"`
	ContextTo    string `json:"contextTo,omitempty" yaml:"contextTo,omitempty"`
	MismatchFrom bool   `json:"mismatchFrom,omitempty" yaml:"mismatchFrom,omitempty"`
	MismatchTo   bool   `json:"mismatchTo,omitempty" yaml:"mismatchTo,omitempty"`
}

// CompareImages compares two ImageSummary objects and returns a structured diff.
func CompareImages(from, to *ImageSummary) ImageCompareResult {
	if from == nil || to == nil {
		return ImageCompareResult{
			SchemaVersion: "1",
			Equality:      Equality{Class: EqualityDifferent},
		}
	}

	res := ImageCompareResult{
		SchemaVersion: "1",
		From:          *from,
		To:            *to,
	}

	// --- image meta ---
	res.Diff.Image = compareMeta(*from, *to)
	if res.Diff.Image.SizeBytes != nil {
		res.Summary.ModifiedCount++
		res.Summary.Changed = true
	}

	// --- partition table ---
	res.Diff.PartitionTable = comparePartitionTable(from.PartitionTable, to.PartitionTable)
	if res.Diff.PartitionTable.Changed {
		res.Summary.PartitionTableChanged = true
		res.Summary.Changed = true
	}

	// --- partitions ---
	res.Diff.Partitions = comparePartitions(from.PartitionTable, to.PartitionTable)
	if len(res.Diff.Partitions.Added) > 0 || len(res.Diff.Partitions.Removed) > 0 || len(res.Diff.Partitions.Modified) > 0 {
		res.Summary.PartitionsChanged = true
		res.Summary.Changed = true
		res.Summary.AddedCount += len(res.Diff.Partitions.Added)
		res.Summary.RemovedCount += len(res.Diff.Partitions.Removed)
		res.Summary.ModifiedCount += len(res.Diff.Partitions.Modified)
	}

	// --- EFI roll-up ---
	res.Diff.EFIBinaries = compareEFIBinaries(flattenEFIBinaries(from.PartitionTable), flattenEFIBinaries(to.PartitionTable))
	if len(res.Diff.EFIBinaries.Added) > 0 || len(res.Diff.EFIBinaries.Removed) > 0 || len(res.Diff.EFIBinaries.Modified) > 0 {
		res.Summary.EFIBinariesChanged = true
		res.Summary.Changed = true
	}

	// --- dm-verity ---
	res.Diff.Verity = compareVerity(from.Verity, to.Verity)
	if res.Diff.Verity != nil && res.Diff.Verity.Changed {
		res.Summary.Changed = true
	}

	// Deterministic ordering for stable JSON
	normalizeCompareResult(&res)

	// Compute Equality (+ Equal for compatibility) as the *last* step
	res.Equality = computeEquality(from, to, res.Diff)

	return res
}

func computeEquality(from, to *ImageSummary, d ImageDiff) Equality {
	t := tallyDiffs(d)

	hashAvailable := from != nil && to != nil &&
		strings.TrimSpace(from.SHA256) != "" &&
		strings.TrimSpace(to.SHA256) != ""

	binaryIdentical := hashAvailable && from.SHA256 == to.SHA256

	eq := Equality{
		VolatileDiffs:     t.volatile,
		MeaningfulDiffs:   t.meaningful,
		MeaningfulReasons: t.mReasons,
		VolatileReasons:   t.vReasons,
	}

	switch {
	case binaryIdentical:
		eq.Class = EqualityBinary
	case t.meaningful == 0:
		if hashAvailable {
			eq.Class = EqualitySemantic // “semantic” but hash proved they differ (or hash differs)
		} else {
			eq.Class = EqualityUnverified // cannot prove binary identity
		}
	default:
		eq.Class = EqualityDifferent
	}

	return eq //, (eq.Class != EqualityDifferent)
}

func compareMeta(from, to ImageSummary) MetaDiff {
	var out MetaDiff

	if from.SizeBytes != to.SizeBytes {
		out.SizeBytes = &ValueDiff[int64]{From: from.SizeBytes, To: to.SizeBytes}
	}
	return out
}

func compareVerity(from, to *VerityInfo) *VerityDiff {
	// Both nil = no difference
	if from == nil && to == nil {
		return nil
	}

	diff := &VerityDiff{}

	// Added (to has verity, from doesn't)
	if from == nil && to != nil {
		diff.Added = to
		diff.Changed = true
		return diff
	}

	// Removed (from has verity, to doesn't)
	if from != nil && to == nil {
		diff.Removed = from
		diff.Changed = true
		return diff
	}

	// Both present
	if from.Enabled != to.Enabled {
		diff.Enabled = &ValueDiff[bool]{From: from.Enabled, To: to.Enabled}
		diff.Changed = true
	}

	if from.Method != to.Method {
		diff.Method = &ValueDiff[string]{From: from.Method, To: to.Method}
		diff.Changed = true
	}

	if from.RootDevice != to.RootDevice {
		diff.RootDevice = &ValueDiff[string]{From: from.RootDevice, To: to.RootDevice}
		diff.Changed = true
	}

	if from.HashPartition != to.HashPartition {
		diff.HashPartition = &ValueDiff[int]{From: from.HashPartition, To: to.HashPartition}
		diff.Changed = true
	}

	if !diff.Changed {
		return nil
	}

	return diff
}

// comparePartitionTable compares two PartitionTableSummary objects and returns a PartitionTableDiff.
func comparePartitionTable(from, to PartitionTableSummary) PartitionTableDiff {
	var d PartitionTableDiff

	if strings.TrimSpace(from.DiskGUID) != strings.TrimSpace(to.DiskGUID) {
		d.DiskGUID = &ValueDiff[string]{From: from.DiskGUID, To: to.DiskGUID}
	}
	if from.Type != to.Type {
		d.Type = &ValueDiff[string]{From: from.Type, To: to.Type}
	}
	if from.LogicalSectorSize != to.LogicalSectorSize {
		d.LogicalSectorSize = &ValueDiff[int64]{From: from.LogicalSectorSize, To: to.LogicalSectorSize}
	}
	if from.PhysicalSectorSize != to.PhysicalSectorSize {
		d.PhysicalSectorSize = &ValueDiff[int64]{From: from.PhysicalSectorSize, To: to.PhysicalSectorSize}
	}
	if from.ProtectiveMBR != to.ProtectiveMBR {
		d.ProtectiveMBR = &ValueDiff[bool]{From: from.ProtectiveMBR, To: to.ProtectiveMBR}
	}

	if !freeSpanEqual(from.LargestFreeSpan, to.LargestFreeSpan) {
		d.LargestFreeSpan = &ValueDiff[FreeSpanSummary]{From: derefFreeSpan(from.LargestFreeSpan), To: derefFreeSpan(to.LargestFreeSpan)}
	}

	if !intSliceEqual(from.MisalignedPartitions, to.MisalignedPartitions) {
		d.MisalignedParts = &ValueDiff[[]int]{From: from.MisalignedPartitions, To: to.MisalignedPartitions}
	}

	d.Changed = d.DiskGUID != nil || d.Type != nil || d.LogicalSectorSize != nil || d.PhysicalSectorSize != nil || d.ProtectiveMBR != nil || d.LargestFreeSpan != nil || d.MisalignedParts != nil
	return d
}

// comparePartitions compares two PartitionTableSummary objects and returns a PartitionDiff.
func comparePartitions(fromPT, toPT PartitionTableSummary) PartitionDiff {
	fromParts := indexPartitions(fromPT)
	toParts := indexPartitions(toPT)

	out := PartitionDiff{}

	// Keys union
	keys := make([]string, 0, len(fromParts)+len(toParts))
	seen := map[string]struct{}{}
	for k := range fromParts {
		seen[k] = struct{}{}
		keys = append(keys, k)
	}
	for k := range toParts {
		if _, ok := seen[k]; !ok {
			keys = append(keys, k)
		}
	}

	sort.Strings(keys)

	for _, k := range keys {
		f, fok := fromParts[k]
		t, tok := toParts[k]

		switch {
		case fok && !tok:
			out.Removed = append(out.Removed, f)
		case !fok && tok:
			out.Added = append(out.Added, t)
		case fok && tok:
			if partitionsEqual(f, t) {
				continue
			}

			mod := ModifiedPartitionSummary{
				Key:  k,
				From: f,
				To:   t,
			}

			// Optional human-friendly changes (keep minimal, add more later)
			mod.Changes = appendPartitionFieldChanges(nil, f, t)

			// Filesystem changes
			mod.Filesystem = compareFilesystemPtrs(f.Filesystem, t.Filesystem)

			// Per-partition EFI evidence diff if there is filesystem evidence on either side
			fEFI := flattenEFIBinariesFromPartition(f)
			tEFI := flattenEFIBinariesFromPartition(t)
			if len(fEFI) > 0 || len(tEFI) > 0 {
				efiDiff := compareEFIBinaries(fEFI, tEFI)
				// Only include if something actually changed
				if len(efiDiff.Added) > 0 || len(efiDiff.Removed) > 0 || len(efiDiff.Modified) > 0 {
					mod.EFIBinaries = &efiDiff
				}
			}

			out.Modified = append(out.Modified, mod)
		}
	}

	return out
}

func indexPartitions(pt PartitionTableSummary) map[string]PartitionSummary {
	out := make(map[string]PartitionSummary, len(pt.Partitions))

	for _, p := range pt.Partitions {
		key := partitionKey(pt.Type, p)

		// Ensure uniqueness even if names collide (rare but possible)
		if _, exists := out[key]; exists {
			key = fmt.Sprintf("%s#idx=%d", key, p.Index)
			if _, exists2 := out[key]; exists2 {
				key = fmt.Sprintf("%s#lba=%d-%d", key, p.StartLBA, p.EndLBA)
			}
		}

		out[key] = p
	}
	return out
}

func partitionKey(ptType string, p PartitionSummary) string {
	ptType = strings.ToLower(strings.TrimSpace(ptType))
	name := strings.ToLower(strings.TrimSpace(p.Name))

	// normalize empty-ish name
	if name == "" {
		name = "-"
	}

	switch ptType {
	case "gpt":
		// GPT type GUID is the strongest “role identity”.
		t := strings.ToUpper(strings.TrimSpace(p.Type))
		if t != "" {
			return fmt.Sprintf("gpt:%s:%s", t, name)
		}
		// fallback: name + index
		return fmt.Sprintf("gpt:%s:idx=%d", name, p.Index)

	case "mbr":
		t := strings.ToLower(strings.TrimSpace(p.Type)) // like 0x0c, 0x83
		if t == "" {
			t = "unknown"
		}
		// MBR doesn’t have GUID roles; index + type is usually stable enough
		return fmt.Sprintf("mbr:%s:%s:idx=%d", t, name, p.Index)

	default:
		// unknown PT: best effort
		t := strings.ToLower(strings.TrimSpace(p.Type))
		if t == "" {
			t = "unknown"
		}
		return fmt.Sprintf("pt:%s:%s:idx=%d", t, name, p.Index)
	}
}

// partitionsEqual checks if two PartitionSummary objects are equal.
func partitionsEqual(a, b PartitionSummary) bool {

	if a.Index != b.Index ||
		a.GUID != b.GUID ||
		a.Name != b.Name ||
		a.Type != b.Type ||
		a.StartLBA != b.StartLBA ||
		a.EndLBA != b.EndLBA ||
		a.SizeBytes != b.SizeBytes ||
		a.Flags != b.Flags ||
		a.AttrRaw != b.AttrRaw ||
		a.LogicalSectorSize != b.LogicalSectorSize {
		return false
	}

	// Filesystem pointer nil-ness / equality
	return filesystemPtrsEqual(a.Filesystem, b.Filesystem)
}

func filesystemPtrsEqual(a, b *FilesystemSummary) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return filesystemEqual(*a, *b)
}

func filesystemEqual(a, b FilesystemSummary) bool {
	// Compare high-value fields
	if a.Type != b.Type ||
		a.Label != b.Label ||
		a.UUID != b.UUID ||
		a.BlockSize != b.BlockSize ||
		a.HasShim != b.HasShim ||
		a.HasUKI != b.HasUKI ||
		a.FATType != b.FATType ||
		a.BytesPerSector != b.BytesPerSector ||
		a.SectorsPerCluster != b.SectorsPerCluster ||
		a.ClusterCount != b.ClusterCount ||
		a.Compression != b.Compression ||
		a.Version != b.Version {
		return false
	}

	// Slice comparisons (sorted for determinism)
	if !stringSliceEqualSorted(a.Features, b.Features) {
		return false
	}
	if !stringSliceEqualSorted(a.Notes, b.Notes) {
		return false
	}
	if !stringSliceEqualSorted(a.FsFlags, b.FsFlags) {
		return false
	}

	// EFI binaries evidence list
	return efiEvidenceListEqual(a.EFIBinaries, b.EFIBinaries)
}

func stringSliceEqualSorted(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	a2 := append([]string(nil), a...)
	b2 := append([]string(nil), b...)
	sort.Strings(a2)
	sort.Strings(b2)
	for i := range a2 {
		if a2[i] != b2[i] {
			return false
		}
	}
	return true
}

func intSliceEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	return slices.Equal(a, b)
}

func freeSpanEqual(a, b *FreeSpanSummary) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.StartLBA == b.StartLBA && a.EndLBA == b.EndLBA && a.SizeBytes == b.SizeBytes
}

func derefFreeSpan(fs *FreeSpanSummary) FreeSpanSummary {
	if fs == nil {
		return FreeSpanSummary{}
	}
	return *fs
}

func efiEvidenceListEqual(a, b []EFIBinaryEvidence) bool {
	if len(a) != len(b) {
		return false
	}
	// Compare by path (stable key)
	am := make(map[string]EFIBinaryEvidence, len(a))
	for _, e := range a {
		am[e.Path] = e
	}
	for _, e := range b {
		ae, ok := am[e.Path]
		if !ok {
			return false
		}
		if !efiEvidenceEqual(ae, e) {
			return false
		}
	}
	return true
}

func efiEvidenceEqual(a, b EFIBinaryEvidence) bool {

	// Compare high-value evidence.
	if a.Path != b.Path ||
		a.Size != b.Size ||
		a.SHA256 != b.SHA256 ||
		a.Arch != b.Arch ||
		a.Kind != b.Kind ||
		a.Signed != b.Signed ||
		a.SignatureSize != b.SignatureSize ||
		a.HasSBAT != b.HasSBAT ||
		a.IsUKI != b.IsUKI ||
		a.KernelSHA256 != b.KernelSHA256 ||
		a.InitrdSHA256 != b.InitrdSHA256 ||
		a.CmdlineSHA256 != b.CmdlineSHA256 ||
		a.OSRelSHA256 != b.OSRelSHA256 ||
		a.UnameSHA256 != b.UnameSHA256 {
		return false
	}

	if !stringSliceEqualSorted(a.Sections, b.Sections) {
		return false
	}

	// SectionSHA256 map compare
	if !stringMapEqual(a.SectionSHA256, b.SectionSHA256) {
		return false
	}

	// Notes not super important, but include if you want exactness
	if !stringSliceEqualSorted(a.Notes, b.Notes) {
		return false
	}

	// OSRelease map compare (optional)
	if !stringMapEqual(a.OSRelease, b.OSRelease) {
		return false
	}

	return true
}

func stringMapEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		if bv, ok := b[k]; !ok || bv != av {
			return false
		}
	}
	return true
}

func appendPartitionFieldChanges(dst []FieldChange, a, b PartitionSummary) []FieldChange {
	add := func(field string, from, to any) {
		dst = append(dst, FieldChange{Field: field, From: from, To: to})
	}

	if a.GUID != b.GUID {
		add("guid", a.GUID, b.GUID)
	}
	if a.Index != b.Index {
		add("index", a.Index, b.Index)
	}
	if a.Name != b.Name {
		add("name", a.Name, b.Name)
	}
	if a.Type != b.Type {
		add("type", a.Type, b.Type)
	}
	if a.StartLBA != b.StartLBA {
		add("startLBA", a.StartLBA, b.StartLBA)
	}
	if a.EndLBA != b.EndLBA {
		add("endLBA", a.EndLBA, b.EndLBA)
	}
	if a.SizeBytes != b.SizeBytes {
		add("sizeBytes", a.SizeBytes, b.SizeBytes)
	}
	if a.Flags != b.Flags {
		add("flags", a.Flags, b.Flags)
	}
	if a.AttrRaw != b.AttrRaw {
		add("attrRaw", a.AttrRaw, b.AttrRaw)
	}
	if a.LogicalSectorSize != b.LogicalSectorSize {
		add("logicalSectorSize", a.LogicalSectorSize, b.LogicalSectorSize)
	}

	return dst
}

func compareFilesystemPtrs(a, b *FilesystemSummary) *FilesystemChange {
	if a == nil && b == nil {
		return nil
	}
	if a == nil && b != nil {
		return &FilesystemChange{Added: b}
	}
	if a != nil && b == nil {
		return &FilesystemChange{Removed: a}
	}
	if a != nil && b != nil && filesystemEqual(*a, *b) {
		return nil
	}

	out := &FilesystemChange{
		Modified: &ModifiedFilesystemSummary{
			From: *a,
			To:   *b,
		},
	}
	out.Modified.Changes = appendFilesystemFieldChanges(nil, *a, *b)
	return out
}

func appendFilesystemFieldChanges(dst []FieldChange, a, b FilesystemSummary) []FieldChange {
	add := func(field string, from, to any) {
		dst = append(dst, FieldChange{Field: field, From: from, To: to})
	}
	if a.Type != b.Type {
		add("type", a.Type, b.Type)
	}
	if a.Label != b.Label {
		add("label", a.Label, b.Label)
	}
	if a.UUID != b.UUID {
		add("uuid", a.UUID, b.UUID)
	}
	if a.HasShim != b.HasShim {
		add("hasShim", a.HasShim, b.HasShim)
	}
	if a.HasUKI != b.HasUKI {
		add("hasUki", a.HasUKI, b.HasUKI)
	}
	return dst
}

func tallyDiffs(d ImageDiff) diffTally {
	var t diffTally

	if d.Image.SizeBytes != nil {
		t.addMeaningful(1, "image size changed")
	}

	if d.PartitionTable.DiskGUID != nil {
		t.addVolatile(1, "PT DiskGUID")
	}
	if d.PartitionTable.Type != nil {
		t.addMeaningful(1, "PT Type")
	}
	if d.PartitionTable.LogicalSectorSize != nil {
		t.addMeaningful(1, "PT LogicalSectorSize")
	}
	if d.PartitionTable.PhysicalSectorSize != nil {
		t.addMeaningful(1, "PT PhysicalSectorSize")
	}
	if d.PartitionTable.ProtectiveMBR != nil {
		t.addMeaningful(1, "PT ProtectiveMBR")
	}
	if d.PartitionTable.LargestFreeSpan != nil {
		t.addMeaningful(1, "PT LargestFreeSpan")
	}
	if d.PartitionTable.MisalignedParts != nil {
		t.addMeaningful(1, "PT MisalignedParts")
	}
	if len(d.Partitions.Added) > 0 {
		t.addMeaningful(len(d.Partitions.Added), "Partitions Added")
	}
	if len(d.Partitions.Removed) > 0 {
		t.addMeaningful(len(d.Partitions.Removed), "Partitions Removed")
	}

	for _, mp := range d.Partitions.Modified {
		// Partition field changes
		for _, ch := range mp.Changes {
			if isVolatilePartitionField(ch.Field) {
				t.addVolatile(1, "Partition "+mp.Key+" field "+ch.Field)
			} else {
				t.addMeaningful(1, "Partition "+mp.Key+" field "+ch.Field)
			}
		}

		tallyFilesystemChange(&t, mp.Filesystem)
	}

	tallyEFIBinaryDiff(&t, d.EFIBinaries)

	// dm-verity changes are meaningful (security-critical)
	if d.Verity != nil && d.Verity.Changed {
		if d.Verity.Added != nil {
			t.addMeaningful(1, "dm-verity enabled")
		} else if d.Verity.Removed != nil {
			t.addMeaningful(1, "dm-verity disabled")
		} else {
			// Field changes
			if d.Verity.Enabled != nil {
				t.addMeaningful(1, "dm-verity enabled status changed")
			}
			if d.Verity.Method != nil {
				t.addMeaningful(1, "dm-verity method changed")
			}
			if d.Verity.RootDevice != nil {
				t.addMeaningful(1, "dm-verity root device changed")
			}
			if d.Verity.HashPartition != nil {
				t.addMeaningful(1, "dm-verity hash partition changed")
			}
		}
	}

	return t
}

func isVolatilePartitionField(field string) bool {
	switch field {
	case "guid": // GPT partition GUID
		return true
	default:
		return false
	}
}

func isVolatileFilesystemField(field string) bool {
	switch field {
	case "uuid": // ext4 UUID or VFAT volume ID
		return true
	default:
		return false
	}
}

func isVolatileEFIBinaryField(field string) bool {
	switch field {
	case "signed", "signatureSize":
		return true
	default:
		return false
	}
}

func tallyEFIBinaryDiff(t *diffTally, d EFIBinaryDiff) {
	if len(d.Added) > 0 {
		t.addMeaningful(len(d.Added), "EFI Binaries Added")
	}
	if len(d.Removed) > 0 {
		t.addMeaningful(len(d.Removed), "EFI Binaries Removed")
	}

	for _, m := range d.Modified {
		equalCmdLine := normalizeKernelCmdline(m.From.Cmdline) == normalizeKernelCmdline(m.To.Cmdline)

		// Count field changes on the EFI evidence object
		for _, ch := range m.Changes {
			switch ch.Field {
			case "cmdline", "cmdlineSha256":
				if equalCmdLine {
					t.addVolatile(1, "EFI "+m.Key+" field "+ch.Field)
				} else {
					t.addMeaningful(1, "EFI "+m.Key+" field "+ch.Field)
				}

			case "sha256":
				v := false
				if m.From.IsUKI || m.To.IsUKI || m.From.Kind == BootloaderUKI || m.To.Kind == BootloaderUKI {
					v = ukiOnlyVolatile(m)
					if v {
						t.addVolatile(1, "EFI "+m.Key+" field "+ch.Field)
					} else {
						t.addMeaningful(1, "EFI "+m.Key+" field "+ch.Field)
					}
				} else {
					t.addMeaningful(1, "EFI "+m.Key+" field "+ch.Field)
				}
			case "size":
				if m.From.IsUKI || m.To.IsUKI || m.From.Kind == BootloaderUKI || m.To.Kind == BootloaderUKI {
					// If UKI differences are only volatile, treat size as volatile too.
					if ukiOnlyVolatile(m) {
						t.addVolatile(1, "EFI "+m.Key+" field "+ch.Field)
					} else {
						t.addMeaningful(1, "EFI "+m.Key+" field "+ch.Field)
					}
				} else {
					// Non-UKI: size change is meaningful.
					t.addMeaningful(1, "EFI "+m.Key+" field "+ch.Field)
				}
			case "initrdSha256":
				t.addVolatile(1, "EFI "+m.Key+" field "+ch.Field)
			default:
				if isVolatileEFIBinaryField(ch.Field) {
					t.addVolatile(1, "EFI "+m.Key+" field "+ch.Field)
				} else {
					t.addMeaningful(1, "EFI "+m.Key+" field "+ch.Field)
				}
			}
		}

		// UKI diffs
		if m.UKI != nil && m.UKI.Changed {
			if m.UKI.KernelSHA256 != nil {
				t.addMeaningful(1, "EFI "+m.Key+" UKI KernelSHA")
			}
			if m.UKI.OSRelSHA256 != nil {
				t.addMeaningful(1, "EFI "+m.Key+" UKI OSRelSHA")
			}
			if m.UKI.UnameSHA256 != nil {
				t.addMeaningful(1, "EFI "+m.Key+" UKI UnameSHA")
			}

			otherSectionChanged := false
			for sec := range m.UKI.SectionSHA256.Modified {
				secL := strings.ToLower(strings.TrimSpace(sec))
				if secL == ".cmdline" || secL == "cmdline" ||
					secL == ".initrd" || secL == "initrd" {
					continue
				}
				otherSectionChanged = true
				break
			}
			if otherSectionChanged {
				t.addMeaningful(1, "EFI "+m.Key+" UKI otherSectionChanged")
			}
		}

		// Bootloader config diffs
		if m.BootConfig != nil {
			tallyBootloaderConfigDiff(t, m.BootConfig, m.Key)
		}
	}
}

func ukiOnlyVolatile(m ModifiedEFIBinaryEvidence) bool {
	// If kernel changed -> meaningful
	if m.UKI != nil && m.UKI.KernelSHA256 != nil {
		return false
	}
	if m.UKI != nil && m.UKI.UnameSHA256 != nil {
		return false
	}

	// cmdline: meaningful only if normalized differs
	if m.UKI != nil && m.UKI.CmdlineSHA256 != nil {
		if normalizeKernelCmdline(m.From.Cmdline) != normalizeKernelCmdline(m.To.Cmdline) {
			return false
		}
	}

	// initrd: this is a TODO
	// If initrd hash changes every build due to timestamps/UUID baked-in, treat volatile.
	return true
}

func tallyFilesystemChange(t *diffTally, fs *FilesystemChange) {
	if fs == nil {
		return
	}

	// Added/removed filesystems -> meaningful
	if fs.Added != nil {
		t.addMeaningful(1, "Filesystem added")
		return
	}
	if fs.Removed != nil {
		t.addMeaningful(1, "Filesystem removed")
		return
	}

	if fs.Modified == nil {
		return
	}

	// Field-level classification: only count meaningful fields as meaningful.
	for _, ch := range fs.Modified.Changes {
		if isVolatileFilesystemField(ch.Field) {
			t.addVolatile(1, "Filesystem field "+ch.Field)
		} else {
			t.addMeaningful(1, "Filesystem field "+ch.Field)
		}
	}
}

func tallyBootloaderConfigDiff(t *diffTally, diff *BootloaderConfigDiff, efiKey string) {
	if diff == nil {
		return
	}

	// Config file changes are meaningful (actual bootloader configuration changed)
	for _, cf := range diff.ConfigFileChanges {
		switch cf.Status {
		case "added":
			t.addMeaningful(1, "BootConfig["+efiKey+"] config file added: "+cf.Path)
		case "removed":
			t.addMeaningful(1, "BootConfig["+efiKey+"] config file removed: "+cf.Path)
		case "modified":
			t.addMeaningful(1, "BootConfig["+efiKey+"] config file modified: "+cf.Path)
		}
	}

	// Boot entry changes are meaningful (boot menu changed)
	for _, be := range diff.BootEntryChanges {
		switch be.Status {
		case "added":
			t.addMeaningful(1, "BootConfig["+efiKey+"] boot entry added: "+be.Name)
		case "removed":
			t.addMeaningful(1, "BootConfig["+efiKey+"] boot entry removed: "+be.Name)
		case "modified":
			// Check if kernel path or cmdline actually changed (meaningful)
			// vs just the display name changed
			if be.KernelFrom != be.KernelTo {
				t.addMeaningful(1, "BootConfig["+efiKey+"] boot entry kernel changed: "+be.Name)
			} else if be.InitrdFrom != be.InitrdTo {
				t.addMeaningful(1, "BootConfig["+efiKey+"] boot entry initrd changed: "+be.Name)
			} else if normalizeKernelCmdline(be.CmdlineFrom) != normalizeKernelCmdline(be.CmdlineTo) {
				t.addMeaningful(1, "BootConfig["+efiKey+"] boot entry cmdline changed: "+be.Name)
			} else {
				// Only cosmetic/metadata changes
				t.addVolatile(1, "BootConfig["+efiKey+"] boot entry metadata changed: "+be.Name)
			}
		}
	}

	// Kernel reference changes
	for _, kr := range diff.KernelRefChanges {
		switch kr.Status {
		case "added":
			t.addMeaningful(1, "BootConfig["+efiKey+"] kernel ref added: "+kr.Path)
		case "removed":
			t.addMeaningful(1, "BootConfig["+efiKey+"] kernel ref removed: "+kr.Path)
		case "modified":
			// UUID change is typically volatile (regenerated each build)
			if kr.UUIDFrom != kr.UUIDTo {
				t.addVolatile(1, "BootConfig["+efiKey+"] kernel ref UUID changed: "+kr.Path)
			} else {
				t.addMeaningful(1, "BootConfig["+efiKey+"] kernel ref modified: "+kr.Path)
			}
		}
	}

	// UUID reference changes - typically volatile (UUIDs regenerate)
	for _, ur := range diff.UUIDReferenceChanges {
		switch ur.Status {
		case "added":
			// New UUID reference found - could be meaningful or volatile depending on context
			if ur.MismatchTo {
				// UUID mismatch is a potential issue - meaningful
				t.addMeaningful(1, "BootConfig["+efiKey+"] UUID ref mismatch added: "+ur.UUID)
			} else {
				t.addVolatile(1, "BootConfig["+efiKey+"] UUID ref added: "+ur.UUID)
			}
		case "removed":
			if ur.MismatchFrom {
				t.addMeaningful(1, "BootConfig["+efiKey+"] UUID ref mismatch removed: "+ur.UUID)
			} else {
				t.addVolatile(1, "BootConfig["+efiKey+"] UUID ref removed: "+ur.UUID)
			}
		case "modified":
			// UUID context changed - typically volatile unless introducing/fixing mismatch
			if ur.MismatchFrom != ur.MismatchTo {
				t.addMeaningful(1, "BootConfig["+efiKey+"] UUID ref mismatch status changed: "+ur.UUID)
			} else {
				t.addVolatile(1, "BootConfig["+efiKey+"] UUID ref context changed: "+ur.UUID)
			}
		}
	}

	// Notes changes are informational - count as volatile
	if len(diff.NotesAdded) > 0 {
		t.addVolatile(len(diff.NotesAdded), "BootConfig["+efiKey+"] notes added")
	}
	if len(diff.NotesRemoved) > 0 {
		t.addVolatile(len(diff.NotesRemoved), "BootConfig["+efiKey+"] notes removed")
	}
}
