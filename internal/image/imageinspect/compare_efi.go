package imageinspect

import (
	"regexp"
	"sort"
	"strings"
)

// Regular expressions for normalizing UUIDs in kernel cmdline
var (
	reKeyedUUID = regexp.MustCompile(
		`(?i)(^|\s)([a-z0-9_.-]+)=([0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12})(\s|$)`)
	reUUIDSpec = regexp.MustCompile(
		`(?i)\b(UUID|PARTUUID)=([0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12})\b`)
)

func normalizeKernelCmdline(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}

	// Normalize whitespace
	s = strings.Join(strings.Fields(s), " ")

	// Normalize key=<uuid> tokens (boot_uuid, rd.luks.uuid, etc.)
	s = reKeyedUUID.ReplaceAllString(s, `$1$2=<uuid>$4`)

	// Normalize UUID= and PARTUUID= forms inside other values
	s = reUUIDSpec.ReplaceAllString(s, `$1=<uuid>`)

	return s
}

func flattenEFIBinaries(pt PartitionTableSummary) []EFIBinaryEvidence {
	var out []EFIBinaryEvidence
	for _, p := range pt.Partitions {
		out = append(out, flattenEFIBinariesFromPartition(p)...)
	}
	// Ensure deterministic order
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func flattenEFIBinariesFromPartition(p PartitionSummary) []EFIBinaryEvidence {
	if p.Filesystem == nil {
		return nil
	}
	// Evidence only exists if the inspector populated it
	if len(p.Filesystem.EFIBinaries) == 0 {
		return nil
	}
	// Copy to avoid accidental sharing/mutation
	out := append([]EFIBinaryEvidence(nil), p.Filesystem.EFIBinaries...)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func compareEFIBinaries(from, to []EFIBinaryEvidence) EFIBinaryDiff {
	out := EFIBinaryDiff{}

	fm := make(map[string]EFIBinaryEvidence, len(from))
	tm := make(map[string]EFIBinaryEvidence, len(to))

	for _, e := range from {
		fm[e.Path] = e
	}
	for _, e := range to {
		tm[e.Path] = e
	}

	keys := make([]string, 0, len(fm)+len(tm))
	seen := map[string]struct{}{}
	for k := range fm {
		seen[k] = struct{}{}
		keys = append(keys, k)
	}
	for k := range tm {
		if _, ok := seen[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	for _, k := range keys {
		f, fok := fm[k]
		t, tok := tm[k]

		switch {
		case fok && !tok:
			out.Removed = append(out.Removed, f)
		case !fok && tok:
			out.Added = append(out.Added, t)
		case fok && tok:
			if efiEvidenceEqual(f, t) {
				continue
			}
			mod := ModifiedEFIBinaryEvidence{
				Key:  k,
				From: f,
				To:   t,
			}
			mod.Changes = appendEFIBinaryFieldChanges(nil, f, t)
			mod.UKI = buildUKIDiffIfRelevant(f, t)
			mod.BootConfig = compareBootloaderConfigs(f.BootConfig, t.BootConfig)
			out.Modified = append(out.Modified, mod)
		}
	}

	return out
}

func appendEFIBinaryFieldChanges(dst []FieldChange, a, b EFIBinaryEvidence) []FieldChange {
	add := func(field string, from, to any) {
		dst = append(dst, FieldChange{Field: field, From: from, To: to})
	}
	if a.SHA256 != b.SHA256 {
		add("sha256", a.SHA256, b.SHA256)
	}
	if a.Size != b.Size {
		add("size", a.Size, b.Size)
	}
	if a.Arch != b.Arch {
		add("arch", a.Arch, b.Arch)
	}
	if a.Kind != b.Kind {
		add("kind", a.Kind, b.Kind)
	}
	if a.Signed != b.Signed {
		add("signed", a.Signed, b.Signed)
	}
	if a.SignatureSize != b.SignatureSize {
		add("signatureSize", a.SignatureSize, b.SignatureSize)
	}
	if a.HasSBAT != b.HasSBAT {
		add("hasSbat", a.HasSBAT, b.HasSBAT)
	}
	if a.IsUKI != b.IsUKI {
		add("isUki", a.IsUKI, b.IsUKI)
	}

	// UKI payload hashes (high value)
	if a.KernelSHA256 != b.KernelSHA256 {
		add("kernelSha256", a.KernelSHA256, b.KernelSHA256)
	}
	if a.InitrdSHA256 != b.InitrdSHA256 {
		add("initrdSha256", a.InitrdSHA256, b.InitrdSHA256)
	}
	if a.CmdlineSHA256 != b.CmdlineSHA256 {
		add("cmdlineSha256", a.CmdlineSHA256, b.CmdlineSHA256)
	}
	if a.OSRelSHA256 != b.OSRelSHA256 {
		add("osrelSha256", a.OSRelSHA256, b.OSRelSHA256)
	}
	if a.UnameSHA256 != b.UnameSHA256 {
		add("unameSha256", a.UnameSHA256, b.UnameSHA256)
	}

	return dst
}

func buildUKIDiffIfRelevant(a, b EFIBinaryEvidence) *UKIDiff {
	// Only include if either side is a UKI OR has UKI payload hashes
	if !(a.IsUKI || b.IsUKI || a.Kind == BootloaderUKI || b.Kind == BootloaderUKI) {
		// Still could be useful if hashes exist but IsUKI not set.
		if a.KernelSHA256 == "" && b.KernelSHA256 == "" &&
			a.InitrdSHA256 == "" && b.InitrdSHA256 == "" &&
			a.CmdlineSHA256 == "" && b.CmdlineSHA256 == "" &&
			a.OSRelSHA256 == "" && b.OSRelSHA256 == "" &&
			a.UnameSHA256 == "" && b.UnameSHA256 == "" &&
			len(a.SectionSHA256) == 0 && len(b.SectionSHA256) == 0 {
			return nil
		}
	}

	d := &UKIDiff{}
	if a.KernelSHA256 != b.KernelSHA256 {
		d.KernelSHA256 = &ValueDiff[string]{From: a.KernelSHA256, To: b.KernelSHA256}
		d.Changed = true
	}
	if a.InitrdSHA256 != b.InitrdSHA256 {
		d.InitrdSHA256 = &ValueDiff[string]{From: a.InitrdSHA256, To: b.InitrdSHA256}
		d.Changed = true
	}
	if a.CmdlineSHA256 != b.CmdlineSHA256 {
		d.CmdlineSHA256 = &ValueDiff[string]{From: a.CmdlineSHA256, To: b.CmdlineSHA256}
		d.Changed = true
	}
	if a.OSRelSHA256 != b.OSRelSHA256 {
		d.OSRelSHA256 = &ValueDiff[string]{From: a.OSRelSHA256, To: b.OSRelSHA256}
		d.Changed = true
	}
	if a.UnameSHA256 != b.UnameSHA256 {
		d.UnameSHA256 = &ValueDiff[string]{From: a.UnameSHA256, To: b.UnameSHA256}
		d.Changed = true
	}

	// Map diff for SectionSHA256
	sd := diffStringMap(a.SectionSHA256, b.SectionSHA256)
	if len(sd.Added) > 0 || len(sd.Removed) > 0 || len(sd.Modified) > 0 {
		d.SectionSHA256 = sd
		d.Changed = true
	}

	if !d.Changed {
		return nil
	}
	return d
}

func diffStringMap(a, b map[string]string) SectionMapDiff {
	out := SectionMapDiff{
		Added:    map[string]string{},
		Removed:  map[string]string{},
		Modified: map[string]ValueDiff[string]{},
	}

	// nil-safe
	if a == nil {
		a = map[string]string{}
	}
	if b == nil {
		b = map[string]string{}
	}

	seen := map[string]struct{}{}
	for k, av := range a {
		seen[k] = struct{}{}
		if bv, ok := b[k]; !ok {
			out.Removed[k] = av
		} else if bv != av {
			out.Modified[k] = ValueDiff[string]{From: av, To: bv}
		}
	}
	for k, bv := range b {
		if _, ok := seen[k]; ok {
			continue
		}
		out.Added[k] = bv
	}

	// Normalize empties to nil for omitempty friendliness
	if len(out.Added) == 0 {
		out.Added = nil
	}
	if len(out.Removed) == 0 {
		out.Removed = nil
	}
	if len(out.Modified) == 0 {
		out.Modified = nil
	}

	return out
}

// compareBootloaderConfigs compares bootloader configuration between two EFI binaries.
// It returns a BootloaderConfigDiff containing detected changes.
func compareBootloaderConfigs(a, b *BootloaderConfig) *BootloaderConfigDiff {
	if a == nil && b == nil {
		return nil
	}

	diff := &BootloaderConfigDiff{
		ConfigFileChanges:    []ConfigFileChange{},
		BootEntryChanges:     []BootEntryChange{},
		KernelRefChanges:     []KernelRefChange{},
		UUIDReferenceChanges: []UUIDRefChange{},
		NotesAdded:           []string{},
		NotesRemoved:         []string{},
	}

	// Handle nil cases
	if a == nil {
		a = &BootloaderConfig{}
	}
	if b == nil {
		b = &BootloaderConfig{}
	}

	// Compare config files
	diff.ConfigFileChanges = compareConfigFiles(a.ConfigFiles, b.ConfigFiles)

	// Compare boot entries
	diff.BootEntryChanges = compareBootEntries(a.BootEntries, b.BootEntries)

	// Compare kernel references
	diff.KernelRefChanges = compareKernelReferences(a.KernelReferences, b.KernelReferences)

	// Compare UUID references
	diff.UUIDReferenceChanges = compareUUIDReferences(a.UUIDReferences, b.UUIDReferences)

	// Compare issues
	diff.NotesRemoved = findRemovedStrings(a.Notes, b.Notes)
	diff.NotesAdded = findRemovedStrings(b.Notes, a.Notes)

	// Check if anything actually changed
	if len(diff.ConfigFileChanges) == 0 &&
		len(diff.BootEntryChanges) == 0 &&
		len(diff.KernelRefChanges) == 0 &&
		len(diff.UUIDReferenceChanges) == 0 &&
		len(diff.NotesAdded) == 0 &&
		len(diff.NotesRemoved) == 0 {
		return nil
	}

	return diff
}

// compareConfigFiles compares bootloader config file hashes.
func compareConfigFiles(a, b map[string]string) []ConfigFileChange {
	changes := []ConfigFileChange{}

	pathsA := sortedMapKeys(a)
	pathsB := sortedMapKeys(b)

	allPaths := mergeStrings(pathsA, pathsB)

	for _, path := range allPaths {
		hashA := a[path]
		hashB := b[path]

		switch {
		case hashA != "" && hashB == "":
			changes = append(changes, ConfigFileChange{
				Path:     path,
				Status:   "removed",
				HashFrom: hashA,
			})
		case hashA == "" && hashB != "":
			changes = append(changes, ConfigFileChange{
				Path:   path,
				Status: "added",
				HashTo: hashB,
			})
		case hashA != "" && hashB != "" && hashA != hashB:
			changes = append(changes, ConfigFileChange{
				Path:     path,
				Status:   "modified",
				HashFrom: hashA,
				HashTo:   hashB,
			})
		}
	}

	return changes
}

// compareBootEntries compares boot menu entries.
func compareBootEntries(a, b []BootEntry) []BootEntryChange {
	changes := []BootEntryChange{}

	aMap := bootEntryMapByName(a)
	bMap := bootEntryMapByName(b)

	allNames := mergeStrings(sortedMapKeys(aMap), sortedMapKeys(bMap))

	for _, name := range allNames {
		entryA := aMap[name]
		entryB := bMap[name]

		switch {
		case entryA != nil && entryB == nil:
			changes = append(changes, BootEntryChange{
				Name:        name,
				Status:      "removed",
				KernelFrom:  entryA.Kernel,
				InitrdFrom:  entryA.Initrd,
				CmdlineFrom: entryA.Cmdline,
			})
		case entryA == nil && entryB != nil:
			changes = append(changes, BootEntryChange{
				Name:      name,
				Status:    "added",
				KernelTo:  entryB.Kernel,
				InitrdTo:  entryB.Initrd,
				CmdlineTo: entryB.Cmdline,
			})
		case entryA != nil && entryB != nil:
			// Check for changes within the entry
			if entryA.Kernel != entryB.Kernel || entryA.Initrd != entryB.Initrd || entryA.Cmdline != entryB.Cmdline {
				changes = append(changes, BootEntryChange{
					Name:        name,
					Status:      "modified",
					KernelFrom:  entryA.Kernel,
					KernelTo:    entryB.Kernel,
					InitrdFrom:  entryA.Initrd,
					InitrdTo:    entryB.Initrd,
					CmdlineFrom: entryA.Cmdline,
					CmdlineTo:   entryB.Cmdline,
				})
			}
		}
	}

	return changes
}

// compareKernelReferences compares kernel references in bootloader config.
func compareKernelReferences(a, b []KernelReference) []KernelRefChange {
	changes := []KernelRefChange{}

	aMap := kernelRefMapByPath(a)
	bMap := kernelRefMapByPath(b)

	allPaths := mergeStrings(sortedMapKeys(aMap), sortedMapKeys(bMap))

	for _, path := range allPaths {
		refA := aMap[path]
		refB := bMap[path]

		switch {
		case refA != nil && refB == nil:
			changes = append(changes, KernelRefChange{
				Path:     path,
				Status:   "removed",
				UUIDFrom: refA.PartitionUUID,
			})
		case refA == nil && refB != nil:
			changes = append(changes, KernelRefChange{
				Path:   path,
				Status: "added",
				UUIDTo: refB.PartitionUUID,
			})
		case refA != nil && refB != nil:
			if refA.PartitionUUID != refB.PartitionUUID {
				changes = append(changes, KernelRefChange{
					Path:     path,
					Status:   "modified",
					UUIDFrom: refA.PartitionUUID,
					UUIDTo:   refB.PartitionUUID,
				})
			}
		}
	}

	return changes
}

// compareUUIDReferences compares UUID references in bootloader config.
func compareUUIDReferences(a, b []UUIDReference) []UUIDRefChange {
	changes := []UUIDRefChange{}

	aMap := uuidRefMapByUUID(a)
	bMap := uuidRefMapByUUID(b)

	allUUIDs := mergeStrings(sortedMapKeys(aMap), sortedMapKeys(bMap))

	for _, uuid := range allUUIDs {
		refA := aMap[uuid]
		refB := bMap[uuid]

		switch {
		case refA != nil && refB == nil:
			changes = append(changes, UUIDRefChange{
				UUID:         uuid,
				Status:       "removed",
				ContextFrom:  refA.Context,
				MismatchFrom: refA.Mismatch,
			})
		case refA == nil && refB != nil:
			changes = append(changes, UUIDRefChange{
				UUID:       uuid,
				Status:     "added",
				ContextTo:  refB.Context,
				MismatchTo: refB.Mismatch,
			})
		case refA != nil && refB != nil:
			if refA.Mismatch != refB.Mismatch || refA.Context != refB.Context {
				changes = append(changes, UUIDRefChange{
					UUID:         uuid,
					Status:       "modified",
					ContextFrom:  refA.Context,
					ContextTo:    refB.Context,
					MismatchFrom: refA.Mismatch,
					MismatchTo:   refB.Mismatch,
				})
			}
		}
	}

	return changes
}

// Helper functions for comparison

func bootEntryMapByName(entries []BootEntry) map[string]*BootEntry {
	m := make(map[string]*BootEntry)
	for i := range entries {
		m[entries[i].Name] = &entries[i]
	}
	return m
}

func kernelRefMapByPath(refs []KernelReference) map[string]*KernelReference {
	m := make(map[string]*KernelReference)
	for i := range refs {
		m[refs[i].Path] = &refs[i]
	}
	return m
}

func uuidRefMapByUUID(refs []UUIDReference) map[string]*UUIDReference {
	m := make(map[string]*UUIDReference)
	for i := range refs {
		m[refs[i].UUID] = &refs[i]
	}
	return m
}

func sortedMapKeys(m interface{}) []string {
	switch v := m.(type) {
	case map[string]string:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return keys
	case map[string]*BootEntry:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return keys
	case map[string]*KernelReference:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return keys
	case map[string]*UUIDReference:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return keys
	}
	return nil
}

func mergeStrings(slices ...[]string) []string {
	seen := make(map[string]struct{})
	var result []string
	for _, slice := range slices {
		for _, s := range slice {
			if _, ok := seen[s]; !ok {
				seen[s] = struct{}{}
				result = append(result, s)
			}
		}
	}
	sort.Strings(result)
	return result
}

func findRemovedStrings(old, new []string) []string {
	newSet := make(map[string]struct{})
	for _, s := range new {
		newSet[s] = struct{}{}
	}

	var removed []string
	for _, s := range old {
		if _, ok := newSet[s]; !ok {
			removed = append(removed, s)
		}
	}
	return removed
}
