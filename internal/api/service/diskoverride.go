// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"fmt"
	"regexp"
	"strings"
)

// DiskOverride is a complete replacement for the matched template's `disk`
// block, as edited by the Advanced tab's Disk step. It doubles as the on-disk
// shape a generated delta declares — hence the yaml tags — because the two are
// identical and a third representation would only be somewhere for them to
// drift apart.
//
// **Complete, not a diff.** `extends` merging replaces `disk` wholesale
// (config/merge.go: `if !isEmptyDiskConfig(userTemplate.Disk)`), unlike
// systemConfig where package lists are unioned. A partial block silently drops
// whatever it omits, the parent's partition table included.
type DiskOverride struct {
	Name                          string                  `yaml:"name"`
	Path                          string                  `yaml:"path,omitempty"`
	SelectionPolicy               *DiskPolicyOverride     `yaml:"selectionPolicy,omitempty"`
	Size                          string                  `yaml:"size,omitempty"`
	MaxSize                       string                  `yaml:"maxSize,omitempty"`
	PartitionTableType            string                  `yaml:"partitionTableType,omitempty"`
	ExtendLastPartitionToFillDisk bool                    `yaml:"extendLastPartitionToFillDisk,omitempty"`
	Artifacts                     []DiskArtifactOverride  `yaml:"artifacts,omitempty"`
	Partitions                    []DiskPartitionOverride `yaml:"partitions,omitempty"`
}

// DiskPolicyOverride mirrors config.DiskSelectionPolicy. The bools stay
// pointers so "unset" survives the round trip — both default to true in the
// builder, so writing false explicitly means something different from omitting
// it.
type DiskPolicyOverride struct {
	Strategy         string `yaml:"strategy,omitempty"`
	ExcludeRemovable *bool  `yaml:"excludeRemovable,omitempty"`
	RequireEmpty     *bool  `yaml:"requireEmpty,omitempty"`
}

// DiskArtifactOverride is one entry of disk.artifacts[].
type DiskArtifactOverride struct {
	Type        string `yaml:"type"`
	Compression string `yaml:"compression,omitempty"`
}

// DiskPartitionOverride is one entry of disk.partitions[]. Field order matches
// the hand-written templates in image-templates/, so a reviewer diffing a
// generated delta against one of those sees only real differences.
type DiskPartitionOverride struct {
	ID           string   `yaml:"id,omitempty"`
	Index        *int     `yaml:"index,omitempty"`
	Name         string   `yaml:"name,omitempty"`
	Type         string   `yaml:"type,omitempty"`
	TypeUUID     string   `yaml:"typeUUID,omitempty"`
	Flags        []string `yaml:"flags,omitempty"`
	FsType       string   `yaml:"fsType,omitempty"`
	FsLabel      string   `yaml:"fsLabel,omitempty"`
	Start        string   `yaml:"start,omitempty"`
	End          string   `yaml:"end,omitempty"`
	MountPoint   string   `yaml:"mountPoint,omitempty"`
	MountOptions string   `yaml:"mountOptions,omitempty"`
}

// The constraints below duplicate what the OpenAPI spec declares, for the same
// reason ValidateImageName and ValidatePackages do: **nothing validates request
// bodies against the spec at runtime.** The handlers decode straight into the
// generated structs (`json.NewDecoder(r.Body).Decode(&req)`), so a spec
// `pattern`, `enum`, `maxLength` or `additionalProperties: false` is
// documentation. Unknown keys are dropped by the typed decode; everything else
// is enforced here or not at all.
//
// The value sets are transcribed from the builder, not from the template schema
// — the schema types nearly every Disk and Partition field as an unconstrained
// string. Each is named with the source it came from. (The size-suffix table is
// already transcribed twice on the Go side, in config.go and
// config/validate/validate.go, both for the same import-cycle reason; this is a
// third, in a package that can import neither.)
var (
	// imagedisc.go diskPartitionCreate rejects anything else outright.
	diskFsTypes = []string{"fat32", "fat16", "vfat", "ext2", "ext3", "ext4", "xfs", "linux-swap"}
	// schema $defs.Disk.partitionTableType, and imagedisc.go's own constants.
	diskTableTypes = []string{"gpt", "mbr"}
	// schema $defs.Disk.selectionPolicy.strategy.
	diskStrategies = []string{"first", "largest", "fastest"}
	// imageconvert.go convertImageFile — the qemu-img conversions that exist.
	diskArtifactTypes = []string{"raw", "qcow2", "vhd", "vhdx", "vmdk", "vdi"}
	// compression.go CompressFile ∩ the schema's enum. `gzip` and `bz2` are in
	// the schema and implemented nowhere; tar.gz/tar.xz are implemented and not
	// in the schema.
	diskCompressions = []string{"gz", "xz", "zstd"}
	// wsl2maker.go archiveFormat requires exactly this, and requires it present.
	wsl2ArtifactTypes = []string{"tar"}
	wsl2Compressions  = []string{"gz", "gzip"}

	// imagedisc.go sizeSuffixesList, enforced by VerifyFileSize's `^(\d+)(.*)$`:
	// whole numbers only, exact case, no bare byte counts, no TiB.
	diskSizeRe   = regexp.MustCompile(`^\d+(KiB|MiB|GiB|K|M|G|KB|MB|GB)$`)
	diskOffsetRe = regexp.MustCompile(`^(0|\d+(KiB|MiB|GiB|K|M|G|KB|MB|GB))$`)
	diskNameRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	diskPathRe   = regexp.MustCompile(`^(|/dev/[A-Za-z0-9/._-]+)$`)
)

const (
	// Request-size guards, matching the OpenAPI maxItems/maxLength. Not build
	// limits: the curated templates stay far under them.
	maxDiskPartitions = 32
	maxDiskArtifacts  = 8
	maxDiskFlags      = 8
	maxDiskNameLen    = 128
	maxDiskFieldLen   = 256
	maxDiskSizeLen    = 32
	maxPartIndex      = 128
)

// ValidateDisk reports whether d is a legal disk override for imageType. A nil
// override is valid (not overridden).
//
// This is not the only gate — the generated delta is also run through the
// template schema and the loader's own semantic checks (buildDelta ->
// validate.ValidateUserTemplateIssues), which is what catches maxSize > size and
// the auto-expand rules. It is the gate that attributes a problem to the field
// that caused it, and the only one that covers the builder's allowlists, which
// live in neither the schema nor the loader.
func ValidateDisk(d *DiskOverride, imageType string) error {
	if d == nil {
		return nil
	}
	if err := validateDiskTop(d); err != nil {
		return err
	}
	if len(d.Partitions) > maxDiskPartitions {
		return fmt.Errorf("too many partitions: %d exceeds the limit of %d",
			len(d.Partitions), maxDiskPartitions)
	}
	for i := range d.Partitions {
		if err := validateDiskPartition(&d.Partitions[i], i, i == len(d.Partitions)-1); err != nil {
			return err
		}
	}
	if err := validateDiskArtifacts(d.Artifacts, imageType); err != nil {
		return err
	}
	return validateDiskAutoExpand(d, imageType)
}

// validateDiskTop covers the disk-level fields.
func validateDiskTop(d *DiskOverride) error {
	switch {
	case d.Name == "":
		return fmt.Errorf("disk name is required")
	case len(d.Name) > maxDiskNameLen:
		return fmt.Errorf("disk name exceeds %d characters", maxDiskNameLen)
	case !diskNameRe.MatchString(d.Name):
		return fmt.Errorf("disk name %q must match %s", d.Name, diskNameRe.String())
	}
	if len(d.Path) > maxDiskFieldLen || !diskPathRe.MatchString(d.Path) {
		return fmt.Errorf("disk path %q must be empty or a /dev device path", d.Path)
	}
	for field, v := range map[string]string{"size": d.Size, "maxSize": d.MaxSize} {
		if v == "" {
			continue
		}
		if len(v) > maxDiskSizeLen || !diskSizeRe.MatchString(v) {
			return fmt.Errorf("disk %s %q must be a whole number with one of "+
				"KiB, MiB, GiB, K, M, G, KB, MB, GB (exact case, no decimals)", field, v)
		}
	}
	if d.MaxSize != "" && d.Size == "" {
		return fmt.Errorf("disk maxSize requires size to be set")
	}
	if d.PartitionTableType != "" && !contains(diskTableTypes, d.PartitionTableType) {
		return fmt.Errorf("partitionTableType %q must be one of %s",
			d.PartitionTableType, strings.Join(diskTableTypes, ", "))
	}
	if p := d.SelectionPolicy; p != nil && p.Strategy != "" && !contains(diskStrategies, p.Strategy) {
		return fmt.Errorf("selectionPolicy strategy %q must be one of %s",
			p.Strategy, strings.Join(diskStrategies, ", "))
	}
	return nil
}

// validateDiskPartition covers one partition. `last` allows an end of "0"
// (rest of the disk), which earlier entries may not use: it would leave every
// partition after it with no offset to start from.
func validateDiskPartition(p *DiskPartitionOverride, i int, last bool) error {
	where := fmt.Sprintf("partition %d", i+1)
	if p.FsType == "" {
		return fmt.Errorf("%s: fsType is required", where)
	}
	if !contains(diskFsTypes, p.FsType) {
		return fmt.Errorf("%s: fsType %q must be one of %s", where, p.FsType,
			strings.Join(diskFsTypes, ", "))
	}
	for field, v := range map[string]string{"start": p.Start, "end": p.End} {
		if v == "" {
			continue
		}
		if len(v) > maxDiskSizeLen || !diskOffsetRe.MatchString(v) {
			return fmt.Errorf("%s: %s %q must be a whole number with one of "+
				"KiB, MiB, GiB, K, M, G, KB, MB, GB (exact case, no decimals), or 0",
				where, field, v)
		}
	}
	if p.End == "0" && !last {
		return fmt.Errorf("%s: only the last partition may end at 0 (rest of the disk)", where)
	}
	if p.Index != nil && (*p.Index < 1 || *p.Index > maxPartIndex) {
		return fmt.Errorf("%s: index %d must be between 1 and %d", where, *p.Index, maxPartIndex)
	}
	for field, v := range map[string]string{
		"id": p.ID, "name": p.Name, "type": p.Type, "typeUUID": p.TypeUUID,
		"fsLabel": p.FsLabel, "mountPoint": p.MountPoint, "mountOptions": p.MountOptions,
	} {
		if len(v) > maxDiskFieldLen {
			return fmt.Errorf("%s: %s exceeds %d characters", where, field, maxDiskFieldLen)
		}
	}
	if len(p.Flags) > maxDiskFlags {
		return fmt.Errorf("%s: too many flags: %d exceeds %d", where, len(p.Flags), maxDiskFlags)
	}
	for _, f := range p.Flags {
		if f == "" || len(f) > maxDiskFieldLen {
			return fmt.Errorf("%s: flag must be non-empty and under %d characters",
				where, maxDiskFieldLen)
		}
	}
	return nil
}

// validateDiskArtifacts enforces the artifact pipeline the image type actually
// runs. ISO and IMG never call ConvertImageFile, so their artifact list is
// carried and ignored rather than rejected — refusing it would fail a request
// over a field that has no effect.
func validateDiskArtifacts(arts []DiskArtifactOverride, imageType string) error {
	if len(arts) > maxDiskArtifacts {
		return fmt.Errorf("too many output artifacts: %d exceeds the limit of %d",
			len(arts), maxDiskArtifacts)
	}
	switch strings.ToLower(imageType) {
	case "iso", "img":
		return nil
	case "wsl2":
		if len(arts) == 0 {
			return fmt.Errorf("a wsl2 image requires one tar artifact with gz compression")
		}
		return validateArtifactSet(arts, wsl2ArtifactTypes, wsl2Compressions, true)
	default:
		return validateArtifactSet(arts, diskArtifactTypes, diskCompressions, false)
	}
}

func validateArtifactSet(arts []DiskArtifactOverride, types, compressions []string, needCompression bool) error {
	for i, a := range arts {
		where := fmt.Sprintf("output artifact %d", i+1)
		if !contains(types, a.Type) {
			return fmt.Errorf("%s: type %q must be one of %s", where, a.Type,
				strings.Join(types, ", "))
		}
		if a.Compression == "" {
			if needCompression {
				return fmt.Errorf("%s: compression is required (%s)", where,
					strings.Join(compressions, " or "))
			}
			continue
		}
		if !contains(compressions, a.Compression) {
			return fmt.Errorf("%s: compression %q must be one of %s", where, a.Compression,
				strings.Join(compressions, ", "))
		}
	}
	return nil
}

// validateDiskAutoExpand mirrors validateAutoExpandLastPartitionConstraints in
// config/validate/validate.go. The generated delta is checked against that too,
// so this is belt and braces — but it names the offending field, and it keeps
// the API honest for a caller that is not the UI.
func validateDiskAutoExpand(d *DiskOverride, imageType string) error {
	if !d.ExtendLastPartitionToFillDisk {
		return nil
	}
	switch strings.ToLower(imageType) {
	case "iso":
		return fmt.Errorf("extendLastPartitionToFillDisk is not supported for imageType=iso")
	case "raw":
		// The loader only applies the remaining checks to raw.
	default:
		return nil
	}
	if len(d.Partitions) == 0 {
		return fmt.Errorf("extendLastPartitionToFillDisk requires at least one partition")
	}
	if mp := d.Partitions[len(d.Partitions)-1].MountPoint; mp != "/" {
		return fmt.Errorf("extendLastPartitionToFillDisk requires the last partition to be "+
			"rootfs ('/'), got mountPoint=%q", mp)
	}
	return nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
