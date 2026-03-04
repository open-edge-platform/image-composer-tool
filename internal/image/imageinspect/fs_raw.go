package imageinspect

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
)

// FAT filesystem reader implementation (for raw reads from disk images)
type fatKind int

// FAT kinds
const (
	fatUnknown fatKind = iota
	fat12
	fat16
	fat32
)

// fatVol represents an opened FAT volume.
type fatVol struct {
	r       io.ReaderAt
	baseOff int64 // partition start offset in bytes

	kind fatKind

	// BPB common
	bytsPerSec uint16
	secPerClus uint8
	rsvdSecCnt uint16
	numFATs    uint8
	rootEntCnt uint16
	totSec     uint32

	// FAT16
	fatSz16 uint16

	// FAT32
	fatSz32  uint32
	rootClus uint32

	// derived
	fatStart       int64
	rootDirStart   int64 // FAT16 fixed root
	rootDirSectors uint32
	dataStart      int64
	clusterSize    uint32
}

// fatDirEntry represents a directory entry in a FAT filesystem.
type fatDirEntry struct {
	name         string
	isDir        bool
	firstCluster uint32
	size         uint32
}

// scanAndHashEFIFromRawFAT scans the FAT filesystem at the given partition offset
// within the provided ReaderAt, looking for EFI binaries under /EFI, hashing them,
// and populating the provided FilesystemSummary with the findings.
func scanAndHashEFIFromRawFAT(r io.ReaderAt, partOff int64, out *FilesystemSummary) error {
	v, err := openFAT(r, partOff)
	if err != nil {
		return err
	}

	if _, err := v.findPath("EFI"); err != nil {
		return nil
	}

	type item struct{ dir string }
	stack := []item{{dir: "EFI"}}

	var (
		hasShim bool
		hasUKI  bool
	)

	out.EFIBinaries = nil

	seen := map[string]struct{}{} // optional dedupe

	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		ents, err := v.listDir(cur.dir)
		if err != nil {
			continue
		}

		for _, de := range ents {
			full := path.Join(cur.dir, de.name)

			if de.isDir {
				stack = append(stack, item{dir: full})
				continue
			}

			nameLower := strings.ToLower(de.name)
			if !strings.HasSuffix(nameLower, ".efi") {
				continue
			}

			fullLower := strings.ToLower(full)
			if _, ok := seen[fullLower]; ok {
				continue
			}
			seen[fullLower] = struct{}{}

			if strings.HasPrefix(fullLower, "efi/linux/") {
				hasUKI = true
			}
			if strings.Contains(nameLower, "shim") {
				hasShim = true
			}

			b, sz, err := v.readFileByEntry(&de)
			if err != nil {
				out.Notes = append(out.Notes, fmt.Sprintf("read %s failed: %v", full, err))
				continue
			}

			peEv, err := ParsePEFromBytes(full, b)
			if err != nil {
				out.Notes = append(out.Notes, fmt.Sprintf("PE parse %s failed: %v", full, err))
				continue
			}

			peEv.Size = sz
			out.EFIBinaries = append(out.EFIBinaries, peEv)
		}
	}

	inheritBootloaderKindBySHA(out.EFIBinaries)
	sort.Slice(out.EFIBinaries, func(i, j int) bool { return out.EFIBinaries[i].Path < out.EFIBinaries[j].Path })

	out.HasShim = out.HasShim || hasShim
	out.HasUKI = out.HasUKI || hasUKI

	// Extract bootloader configuration for known bootloader types
	for i := range out.EFIBinaries {
		efi := &out.EFIBinaries[i]
		switch efi.Kind {
		case BootloaderGrub, BootloaderSystemdBoot:
			// Try to extract config files
			efi.BootConfig = extractBootloaderConfigFromFAT(v, efi.Kind)
			// For systemd-boot on UKI systems, also synthesize boot config from UKI
			if efi.Kind == BootloaderSystemdBoot && out.HasUKI && efi.BootConfig != nil && len(efi.BootConfig.ConfigFiles) == 0 {
				// No loader.conf found on UKI system; synthesize from UKI cmdline
				for _, uki := range out.EFIBinaries {
					if uki.IsUKI && uki.Cmdline != "" {
						// Create synthetic boot config from UKI
						efi.BootConfig = synthesizeBootConfigFromUKI(&uki)
						break
					}
				}
			}
		}
	}

	return nil
}

// synthesizeBootConfigFromUKI creates a BootloaderConfig from a UKI binary's cmdline.
// This is used for UKI-based systems that don't have a separate loader.conf file.
func synthesizeBootConfigFromUKI(uki *EFIBinaryEvidence) *BootloaderConfig {
	cfg := &BootloaderConfig{
		ConfigFiles:      make(map[string]string),
		ConfigRaw:        make(map[string]string),
		KernelReferences: []KernelReference{},
		BootEntries:      []BootEntry{},
		UUIDReferences:   []UUIDReference{},
		Notes:            []string{},
	}

	if uki == nil || uki.Cmdline == "" {
		cfg.Notes = append(cfg.Notes, "No UKI cmdline available for boot config synthesis")
		return cfg
	}

	// Store the UKI cmdline as ConfigRaw
	cfg.ConfigRaw["uki_cmdline"] = uki.Cmdline

	// Parse the UKI cmdline to extract boot parameters
	// Create a synthetic boot entry for the UKI
	entry := BootEntry{
		Name:      "UKI Boot Entry",
		Kernel:    uki.Path,
		Cmdline:   uki.Cmdline,
		IsDefault: true,
		UKIPath:   uki.Path,
	}

	// Extract root= and UUIDs from cmdline
	for _, token := range strings.Fields(uki.Cmdline) {
		if strings.HasPrefix(token, "root=") {
			entry.RootDevice = strings.TrimPrefix(token, "root=")
			entry.RootDevice = strings.Trim(entry.RootDevice, `"'`)
			// Extract UUID if present
			for _, u := range extractUUIDsFromString(entry.RootDevice) {
				entry.PartitionUUID = u
				cfg.UUIDReferences = append(cfg.UUIDReferences, UUIDReference{UUID: u, Context: "uki_cmdline"})
			}
		} else if strings.HasPrefix(token, "boot_uuid=") {
			// boot_uuid parameter points to the root filesystem UUID
			id := strings.TrimPrefix(token, "boot_uuid=")
			id = strings.Trim(id, `"'`)
			for _, u := range extractUUIDsFromString(id) {
				cfg.UUIDReferences = append(cfg.UUIDReferences, UUIDReference{UUID: u, Context: "uki_boot_uuid"})
			}
		}
	}

	cfg.BootEntries = append(cfg.BootEntries, entry)

	// Create kernel reference
	kernRef := KernelReference{
		Path:      uki.Path,
		BootEntry: entry.Name,
		RootUUID:  entry.RootDevice,
	}
	if entry.PartitionUUID != "" {
		kernRef.PartitionUUID = entry.PartitionUUID
	}
	cfg.KernelReferences = append(cfg.KernelReferences, kernRef)

	// Note that this is a synthesized config
	cfg.Notes = append(cfg.Notes, fmt.Sprintf("Boot configuration extracted from UKI binary %s (no loader.conf found)", uki.Path))

	return cfg
}

// extractBootloaderConfigFromFAT attempts to read and parse bootloader config files
// from the FAT filesystem for the given bootloader kind.
func extractBootloaderConfigFromFAT(v *fatVol, kind BootloaderKind) *BootloaderConfig {
	cfg := &BootloaderConfig{
		ConfigFiles:      make(map[string]string),
		ConfigRaw:        make(map[string]string),
		KernelReferences: []KernelReference{},
		BootEntries:      []BootEntry{},
		UUIDReferences:   []UUIDReference{},
		Notes:            []string{},
	}

	// Generate candidate config paths based on filesystem layout and bootloader kind
	configPaths := generateBootloaderConfigPaths(v, kind)
	if len(configPaths) == 0 {
		return nil
	}

	// Try to read each config file
	for _, cfgPath := range configPaths {
		content, err := readFileFromFAT(v, cfgPath)
		if err == nil && content != "" {
			// Calculate hash
			hash := hashBytesHex([]byte(content))
			cfg.ConfigFiles[cfgPath] = hash

			// Store raw content (truncated if large)
			if len(content) > 10240 {
				cfg.ConfigRaw[cfgPath] = content[:10240] + "\n[truncated...]"
			} else {
				cfg.ConfigRaw[cfgPath] = content
			}

			// Parse based on bootloader kind
			switch kind {
			case BootloaderGrub:
				parsed := parseGrubConfigContent(content)
				cfg.BootEntries = parsed.BootEntries
				cfg.KernelReferences = parsed.KernelReferences
				cfg.UUIDReferences = parsed.UUIDReferences
				cfg.Notes = append(cfg.Notes, parsed.Notes...)
				// Don't overwrite ConfigRaw since we set it above
				cfg.DefaultEntry = parsed.DefaultEntry
			case BootloaderSystemdBoot:
				parsed := parseSystemdBootEntries(content)
				cfg.BootEntries = parsed.BootEntries
				cfg.DefaultEntry = parsed.DefaultEntry
				cfg.UUIDReferences = parsed.UUIDReferences
				cfg.Notes = append(cfg.Notes, parsed.Notes...)
			}

			break // Found config file, stop trying alternatives
		} else if err != nil {
			if !strings.Contains(err.Error(), "does not exist") && !strings.Contains(err.Error(), "not found") {
				// Only report non-file-not-found errors
				cfg.Notes = append(cfg.Notes, fmt.Sprintf("Failed to read %s: %v", cfgPath, err))
			}
		}
	}

	// If no config found with hardcoded paths, try dynamic search in /EFI/*/grub.cfg
	if len(cfg.ConfigFiles) == 0 && kind == BootloaderGrub {
		if dynamicPath, dynamicContent := searchBootloaderConfigInEFI(v, "grub.cfg"); dynamicPath != "" && dynamicContent != "" {
			hash := hashBytesHex([]byte(dynamicContent))
			cfg.ConfigFiles[dynamicPath] = hash

			if len(dynamicContent) > 10240 {
				cfg.ConfigRaw[dynamicPath] = dynamicContent[:10240] + "\n[truncated...]"
			} else {
				cfg.ConfigRaw[dynamicPath] = dynamicContent
			}

			parsed := parseGrubConfigContent(dynamicContent)
			cfg.BootEntries = parsed.BootEntries
			cfg.KernelReferences = parsed.KernelReferences
			cfg.UUIDReferences = parsed.UUIDReferences
			cfg.Notes = append(cfg.Notes, parsed.Notes...)
			cfg.DefaultEntry = parsed.DefaultEntry
		}
	}

	// If no config file found, add note (not an error, might be acceptable for minimal configs)
	if len(cfg.ConfigFiles) == 0 {
		switch kind {
		case BootloaderSystemdBoot:
			cfg.Notes = append(cfg.Notes, "No systemd-boot configuration file found (may be normal for UKI-based systems)")
		case BootloaderGrub:
			cfg.Notes = append(cfg.Notes, "No GRUB configuration file found on ESP. Some distributions may store GRUB config on the root partition (/boot/grub/grub.cfg)")
		}
	}

	return cfg
}

// searchBootloaderConfigInEFI dynamically searches for a bootloader config file
// in any subdirectory of /EFI/, including nested directories.
// Returns the path and content if found, or empty strings if not found.
func searchBootloaderConfigInEFI(v *fatVol, filename string) (string, string) {
	// First try /EFI/ root level
	if content, err := readFileFromFAT(v, fmt.Sprintf("/EFI/%s", filename)); err == nil && content != "" {
		return fmt.Sprintf("/EFI/%s", filename), content
	}

	// List /EFI directory
	efiEntries, err := v.listDir("EFI")
	if err != nil {
		return "", ""
	}

	// Try each subdirectory in /EFI/ (one level deep)
	for _, entry := range efiEntries {
		if !entry.isDir || entry.name == "." || entry.name == ".." {
			continue
		}

		// Try to read the file in this subdirectory
		testPath := fmt.Sprintf("/EFI/%s/%s", entry.name, filename)
		if content, err := readFileFromFAT(v, testPath); err == nil && content != "" {
			return testPath, content
		}

		// Also search nested directories within /EFI/[name]/
		// This handles cases like /EFI/BOOT/x64-efi/grub.cfg
		if nestedPath, nestedContent := searchBootloaderConfigInNestedDir(v, fmt.Sprintf("EFI/%s", entry.name), filename); nestedPath != "" {
			return nestedPath, nestedContent
		}
	}

	// If no grub.cfg found, try alternative names like grub-efi.cfg or grubx64.cfg.signed
	if strings.Contains(filename, "grub") {
		alternativeNames := []string{"grub-efi.cfg", "grubx64.cfg", "grub.cfg.signed"}
		for _, altName := range alternativeNames {
			// Try in each subdirectory
			for _, entry := range efiEntries {
				if !entry.isDir || entry.name == "." || entry.name == ".." {
					continue
				}
				testPath := fmt.Sprintf("/EFI/%s/%s", entry.name, altName)
				if content, err := readFileFromFAT(v, testPath); err == nil && content != "" {
					return testPath, content
				}
				// Also search nested
				if nestedPath, nestedContent := searchBootloaderConfigInNestedDir(v, fmt.Sprintf("EFI/%s", entry.name), altName); nestedPath != "" {
					return nestedPath, nestedContent
				}
			}
		}
	}

	return "", ""
}

// searchBootloaderConfigInNestedDir recursively searches within a directory for a config file
func searchBootloaderConfigInNestedDir(v *fatVol, dirPath, filename string) (string, string) {
	// Avoid infinite recursion - limit to 3 levels deep
	depth := strings.Count(dirPath, "/")
	if depth > 4 { // EFI is level 1, subdirs are 2+
		return "", ""
	}

	entries, err := v.listDir(dirPath)
	if err != nil {
		return "", ""
	}

	for _, entry := range entries {
		if entry.name == "." || entry.name == ".." {
			continue
		}

		if !entry.isDir {
			// Check if this is our target file (case-insensitive)
			if strings.EqualFold(entry.name, filename) {
				testPath := fmt.Sprintf("/%s/%s", dirPath, entry.name)
				if content, err := readFileFromFAT(v, testPath); err == nil && content != "" {
					return testPath, content
				}
			}
			continue
		}

		// Recursively search in subdirectories
		nestedPath := fmt.Sprintf("%s/%s", dirPath, entry.name)
		if foundPath, foundContent := searchBootloaderConfigInNestedDir(v, nestedPath, filename); foundPath != "" {
			return foundPath, foundContent
		}
	}

	return "", ""
}

// readFileFromFAT reads a file from the FAT filesystem by path.
// Returns the file content as a string, or an error if the file cannot be read.
func readFileFromFAT(v *fatVol, filePath string) (string, error) {
	// Normalize path
	filePath = strings.TrimPrefix(filePath, "/")
	filePath = strings.ReplaceAll(filePath, "\\", "/")

	parts := strings.Split(filePath, "/")
	if len(parts) == 0 {
		return "", fmt.Errorf("invalid file path")
	}

	// Navigate through directory structure
	for i, part := range parts {
		if part == "" {
			continue
		}

		// Determine which directory to list
		// For the first part, list the root; for subsequent parts, list the path so far
		var dirPath string
		if i == 0 {
			dirPath = "" // List root to find first part
		} else {
			dirPath = strings.Join(parts[:i], "/") // List accumulated path
		}

		entries, err := v.listDir(dirPath)
		if err != nil {
			return "", err
		}

		// Find matching entry (case-insensitive)
		found := false
		var entry fatDirEntry
		for _, e := range entries {
			if strings.EqualFold(e.name, part) {
				entry = e
				found = true
				break
			}
		}

		if !found {
			return "", fmt.Errorf("file not found: %s", filePath)
		}

		// Last part - if this is a file, read it
		if i == len(parts)-1 && !entry.isDir {
			content, _, err := v.readFileByEntry(&entry)
			if err != nil {
				return "", err
			}
			return string(content), nil
		}
	}

	return "", fmt.Errorf("file not found: %s", filePath)
}

// generateBootloaderConfigPaths builds a prioritized list of candidate configuration
// file paths for a given `kind` on the provided FAT volume. It prefers files
// under /EFI/* (inspecting actual subdirectories) and falls back to common
// /boot locations. Returned paths are normalized (leading '/') and deduplicated
// while preserving order.
func generateBootloaderConfigPaths(v *fatVol, kind BootloaderKind) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(p string) {
		if p == "" {
			return
		}
		// normalize
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}

	// Inspect /EFI directory to discover vendor-specific subdirs
	var efiSubdirs []string
	if ents, err := v.listDir("EFI"); err == nil {
		for _, e := range ents {
			if e.isDir {
				efiSubdirs = append(efiSubdirs, e.name)
			}
		}
	}

	switch kind {
	case BootloaderGrub:
		// Prefer vendor-specific locations under /EFI/<vendor>/*.cfg
		for _, d := range efiSubdirs {
			// common candidate names
			add(path.Join("EFI", d, "grub.cfg"))
			add(path.Join("EFI", d, "grub-efi.cfg"))
			add(path.Join("EFI", d, "grubx64.cfg"))
			add(path.Join("EFI", d, "grub.cfg.signed"))
			// nested possibilities
			add(path.Join("EFI", d, "grub", "grub.cfg"))
			add(path.Join("EFI", d, "boot", "grub.cfg"))
		}
		// Common ESP-wide locations
		add("EFI/BOOT/grub.cfg")
		add("EFI/boot/grub.cfg")
		add("EFI/grub/grub.cfg")

		// Fallbacks on possible non-ESP /boot locations
		add("/grub/grub.cfg")
		add("/boot/grub/grub.cfg")
		add("/boot/grub2/grub.cfg")

	case BootloaderSystemdBoot:
		// Prefer loader.conf under /loader and vendor dirs
		add("loader/loader.conf")
		add("EFI/systemd/loader.conf")
		for _, d := range efiSubdirs {
			add(path.Join("EFI", d, "loader.conf"))
			add(path.Join("EFI", d, "loader", "loader.conf"))
		}
		add("/boot/loader.conf")
	default:
		// Unknown bootloader
	}

	return out
}

func (v *fatVol) readFileByEntry(e *fatDirEntry) ([]byte, int64, error) {
	remaining := int64(e.size)
	var out []byte
	out = make([]byte, 0, remaining)

	c := e.firstCluster
	seen := map[uint32]bool{}

	for c >= 2 && !v.isEOC(c) && remaining > 0 {
		if seen[c] {
			return nil, 0, fmt.Errorf("FAT loop detected at cluster %d", c)
		}
		seen[c] = true

		off := v.clusterOff(c)
		chunk := make([]byte, v.clusterSize)
		if _, err := v.r.ReadAt(chunk, off); err != nil && err != io.EOF {
			return nil, 0, err
		}

		n := int64(len(chunk))
		if remaining < n {
			n = remaining
		}
		out = append(out, chunk[:n]...)
		remaining -= n

		next, err := v.fatEntry(c)
		if err != nil {
			return nil, 0, err
		}
		c = next
	}

	return out, int64(e.size), nil
}

// listDir lists the directory entries at the given path within the FAT volume.
func (v *fatVol) listDir(dir string) ([]fatDirEntry, error) {
	dir = strings.Trim(dir, "/")
	if dir == "" {
		return v.readRootDir()
	}

	e, err := v.findPath(dir)
	if err != nil {
		return nil, err
	}
	if !e.isDir {
		return nil, fmt.Errorf("not a directory: %s", dir)
	}

	return v.readDirFromCluster(e.firstCluster)
}

// findPath finds the directory entry for the given path within the FAT volume.
func (v *fatVol) findPath(p string) (*fatDirEntry, error) {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil, fmt.Errorf("empty path")
	}
	parts := strings.Split(p, "/")

	ents, err := v.readRootDir()
	if err != nil {
		return nil, err
	}

	for i, part := range parts {
		var match *fatDirEntry
		for _, e := range ents {
			if strings.EqualFold(e.name, part) {
				tmp := e
				match = &tmp
				break
			}
		}
		if match == nil {
			return nil, os.ErrNotExist
		}
		if i == len(parts)-1 {
			return match, nil
		}
		if !match.isDir {
			return nil, fmt.Errorf("not a directory: %s", part)
		}
		ents, err = v.readDirFromCluster(match.firstCluster)
		if err != nil {
			return nil, err
		}
	}

	return nil, os.ErrNotExist
}

// readDirFromCluster reads directory entries starting from the given cluster.
func (v *fatVol) readDirFromCluster(startCluster uint32) ([]fatDirEntry, error) {
	var all []byte
	c := startCluster
	seen := map[uint32]bool{}

	for c >= 2 && !v.isEOC(c) {
		if seen[c] {
			return nil, fmt.Errorf("FAT loop detected at cluster %d", c)
		}
		seen[c] = true

		off := v.clusterOff(c)
		chunk := make([]byte, v.clusterSize)
		if _, err := v.r.ReadAt(chunk, off); err != nil && err != io.EOF {
			return nil, err
		}
		all = append(all, chunk...)

		next, err := v.fatEntry(c)
		if err != nil {
			return nil, err
		}
		c = next
	}

	return parseDirEntries(all)
}

// parseDirEntries parses raw directory entry bytes into a slice of fatDirEntry.
func parseDirEntries(buf []byte) ([]fatDirEntry, error) {
	var out []fatDirEntry
	var lfnParts []string

	for off := 0; off+32 <= len(buf); off += 32 {
		e := buf[off : off+32]
		if e[0] == 0x00 {
			break
		}
		if e[0] == 0xE5 {
			lfnParts = nil
			continue
		}

		attr := e[11]
		if attr == 0x0F {
			part := decodeLFNPart(e)
			if part != "" {
				lfnParts = append(lfnParts, part)
			}
			continue
		}

		// volume label entry?
		if attr&0x08 != 0 {
			lfnParts = nil
			continue
		}

		name := ""
		if len(lfnParts) > 0 {
			for i, j := 0, len(lfnParts)-1; i < j; i, j = i+1, j-1 {
				lfnParts[i], lfnParts[j] = lfnParts[j], lfnParts[i]
			}
			name = strings.Join(lfnParts, "")
		} else {
			name = decode83Name(e[0:11])
		}
		lfnParts = nil

		isDir := (attr & 0x10) != 0

		// FAT32 stores high 16 bits in e[20:22]
		clusHi := binary.LittleEndian.Uint16(e[20:22])
		clusLo := binary.LittleEndian.Uint16(e[26:28])
		firstClus := (uint32(clusHi) << 16) | uint32(clusLo)

		size := binary.LittleEndian.Uint32(e[28:32])

		if name == "." || name == ".." {
			continue
		}

		out = append(out, fatDirEntry{
			name:         name,
			isDir:        isDir,
			firstCluster: firstClus,
			size:         size,
		})
	}

	return out, nil
}

// readRootDir reads the root directory entries of the FAT volume.
func (v *fatVol) readRootDir() ([]fatDirEntry, error) {
	if v.kind == fat32 {
		return v.readDirFromCluster(v.rootClus)
	}

	// FAT16 root is fixed region
	sizeBytes := int64(v.rootDirSectors) * int64(v.bytsPerSec)
	buf := make([]byte, sizeBytes)
	if _, err := v.r.ReadAt(buf, v.rootDirStart); err != nil && err != io.EOF {
		return nil, err
	}
	return parseDirEntries(buf)
}

// decode83Name decodes an 8.3 filename from a byte slice.
func decode83Name(b []byte) string {
	base := strings.TrimRight(string(b[0:8]), " ")
	ext := strings.TrimRight(string(b[8:11]), " ")
	if ext != "" {
		return base + "." + ext
	}
	return base
}

// decodeLFNPart decodes a single LFN part from a directory entry.
func decodeLFNPart(e []byte) string {
	// 13 UTF-16LE chars in 3 ranges
	chars := make([]uint16, 0, 13)
	readU16 := func(i int) uint16 { return binary.LittleEndian.Uint16(e[i : i+2]) }

	for _, i := range []int{1, 3, 5, 7, 9} {
		chars = append(chars, readU16(i))
	}
	for _, i := range []int{14, 16, 18, 20, 22, 24} {
		chars = append(chars, readU16(i))
	}
	for _, i := range []int{28, 30} {
		chars = append(chars, readU16(i))
	}

	// convert until 0x0000 or 0xFFFF
	var sb strings.Builder
	for _, c := range chars {
		if c == 0x0000 || c == 0xFFFF {
			break
		}
		sb.WriteRune(rune(c))
	}
	return sb.String()
}

// isEOC checks if the given cluster number indicates end-of-chain.
func (v *fatVol) isEOC(c uint32) bool {
	switch v.kind {
	case fat32:
		return c >= 0x0FFFFFF8
	default: // fat16 (and fat12 if you ever add)
		return c >= 0xFFF8
	}
}

// clusterOff returns the byte offset of the given cluster within the FAT volume.
func (v *fatVol) clusterOff(cluster uint32) int64 {
	// data clusters start at 2
	if cluster < 2 {
		return v.dataStart
	}
	dataClusterIndex := cluster - 2
	return v.dataStart + int64(dataClusterIndex)*int64(v.clusterSize)
}

// fatEntry reads the FAT entry for the given cluster number.
func (v *fatVol) fatEntry(cluster uint32) (uint32, error) {
	switch v.kind {
	case fat32:
		off := v.fatStart + int64(cluster)*4
		b := make([]byte, 4)
		if _, err := v.r.ReadAt(b, off); err != nil && err != io.EOF {
			return 0, err
		}
		// FAT32 uses only low 28 bits
		return binary.LittleEndian.Uint32(b) & 0x0FFFFFFF, nil
	case fat12:
		// optional: implement later; ESP won’t be FAT12
		return 0, fmt.Errorf("FAT12 not supported")
	default: // fat16
		off := v.fatStart + int64(cluster)*2
		b := make([]byte, 2)
		if _, err := v.r.ReadAt(b, off); err != nil && err != io.EOF {
			return 0, err
		}
		return uint32(binary.LittleEndian.Uint16(b)), nil
	}
}

// openFAT parses BPB, classifies FAT12/16/32, and fills derived layout offsets.
func openFAT(r io.ReaderAt, baseOff int64) (*fatVol, error) {
	bs := make([]byte, 512)
	if _, err := r.ReadAt(bs, baseOff); err != nil && err != io.EOF {
		return nil, fmt.Errorf("read boot sector: %w", err)
	}
	if bs[510] != 0x55 || bs[511] != 0xAA {
		return nil, fmt.Errorf("invalid boot sector signature")
	}

	v := &fatVol{r: r, baseOff: baseOff}

	// BPB common
	v.bytsPerSec = binary.LittleEndian.Uint16(bs[11:13])
	v.secPerClus = bs[13]
	v.rsvdSecCnt = binary.LittleEndian.Uint16(bs[14:16])
	v.numFATs = bs[16]
	v.rootEntCnt = binary.LittleEndian.Uint16(bs[17:19])

	totSec16 := binary.LittleEndian.Uint16(bs[19:21])
	v.fatSz16 = binary.LittleEndian.Uint16(bs[22:24])
	totSec32 := binary.LittleEndian.Uint32(bs[32:36])

	v.totSec = uint32(totSec16)
	if v.totSec == 0 {
		v.totSec = totSec32
	}

	if v.bytsPerSec == 0 || v.secPerClus == 0 || v.rsvdSecCnt == 0 || v.numFATs == 0 {
		return nil, fmt.Errorf("invalid BPB fields")
	}
	v.clusterSize = uint32(v.bytsPerSec) * uint32(v.secPerClus)

	// FAT32 fields (only meaningful for FAT32)
	v.fatSz32 = binary.LittleEndian.Uint32(bs[36:40])
	v.rootClus = binary.LittleEndian.Uint32(bs[44:48])

	// Derived layout
	v.fatStart = v.baseOff + int64(v.rsvdSecCnt)*int64(v.bytsPerSec)

	// Determine FAT32 vs FAT12/16 using canonical BPB conditions
	isFAT32 := (v.rootEntCnt == 0) && (v.fatSz16 == 0) && (v.fatSz32 != 0)

	if isFAT32 {
		v.kind = fat32
		// FAT32: root directory is a cluster chain starting at rootClus
		// rootDirStart/rootDirSectors are unused in FAT32 (keep 0)
		v.dataStart = v.fatStart + int64(v.numFATs)*int64(v.fatSz32)*int64(v.bytsPerSec)
		return v, nil
	}

	// FAT12/16: need cluster count to classify accurately
	if v.fatSz16 == 0 {
		return nil, fmt.Errorf("invalid FAT16 BPB: fatSz16=0 and not FAT32")
	}

	v.rootDirSectors = ((uint32(v.rootEntCnt) * 32) + (uint32(v.bytsPerSec) - 1)) / uint32(v.bytsPerSec)
	v.rootDirStart = v.fatStart + int64(v.numFATs)*int64(v.fatSz16)*int64(v.bytsPerSec)
	v.dataStart = v.rootDirStart + int64(v.rootDirSectors)*int64(v.bytsPerSec)

	// Data sectors:
	dataSectors := v.totSec - (uint32(v.rsvdSecCnt) + (uint32(v.numFATs) * uint32(v.fatSz16)) + v.rootDirSectors)
	clusterCount := dataSectors / uint32(v.secPerClus)

	switch {
	case clusterCount < 4085:
		v.kind = fat12
	case clusterCount < 65525:
		v.kind = fat16
	default:
		// rare but possible; treat as FAT32-ish, but BPB wasn't FAT32; keep fat16 to avoid misreading FAT entries.
		v.kind = fat16
	}

	return v, nil
}
