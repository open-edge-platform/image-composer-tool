package imageinspect

import (
	"fmt"
	"regexp"
	"strings"
)

// uuidRegex matches UUID format: 8-4-4-4-12 hex digits
var uuidRegex = regexp.MustCompile(`[0-9a-fA-F]{8}[-_]?[0-9a-fA-F]{4}[-_]?[0-9a-fA-F]{4}[-_]?[0-9a-fA-F]{4}[-_]?[0-9a-fA-F]{12}`)

// extractUUIDsFromString finds all UUIDs in a string and returns them normalized
func extractUUIDsFromString(s string) []string {
	if s == "" {
		return nil
	}
	matches := uuidRegex.FindAllString(s, -1)
	if matches == nil {
		return nil
	}
	// Deduplicate and normalize
	seen := make(map[string]struct{})
	var result []string
	for _, m := range matches {
		normalized := normalizeUUID(m)
		if _, ok := seen[normalized]; !ok {
			seen[normalized] = struct{}{}
			result = append(result, normalized)
		}
	}
	return result
}

// normalizeUUID removes hyphens and converts to lowercase
func normalizeUUID(uuid string) string {
	return strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(uuid, "-", ""), "_", ""))
}

// parseGrubConfigContent extracts boot entries and kernel references from grub.cfg content.
func parseGrubConfigContent(content string) BootloaderConfig {
	cfg := BootloaderConfig{
		ConfigRaw:        make(map[string]string),
		KernelReferences: []KernelReference{},
		BootEntries:      []BootEntry{},
		UUIDReferences:   []UUIDReference{},
		Notes:            []string{},
	}

	if content == "" {
		cfg.Notes = append(cfg.Notes, "grub.cfg is empty")
		return cfg
	}

	// Store raw content (truncated if too large)
	if len(content) > 10240 { // 10KB limit
		cfg.ConfigRaw["grub.cfg"] = content[:10240] + "\n[truncated...]"
	} else {
		cfg.ConfigRaw["grub.cfg"] = content
	}

	// Extract critical metadata from the config
	lines := strings.Split(content, "\n")
	var configfilePath string
	var grubPrefix string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Capture GRUB device notation like (hd0,gpt2) or (hd0,msdos1)
		if strings.Contains(trimmed, "(hd") {
			// find all occurrences of gptN or msdosN inside parentheses
			// crude scan: look for "gpt" or "msdos" and digits following
			parts := strings.FieldsFunc(trimmed, func(r rune) bool { return r == '(' || r == ')' || r == ',' || r == ' ' })
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if strings.HasPrefix(strings.ToLower(p), "gpt") || strings.HasPrefix(strings.ToLower(p), "msdos") {
					// extract trailing digits
					var num string
					for i := len(p) - 1; i >= 0; i-- {
						if p[i] < '0' || p[i] > '9' {
							num = p[i+1:]
							break
						}
						if i == 0 {
							num = p
						}
					}
					if num != "" {
						// store as a reference like gpt2 or msdos1
						id := strings.ToLower(strings.TrimSpace(p))
						cfg.UUIDReferences = append(cfg.UUIDReferences, UUIDReference{UUID: id, Context: "grub_root_hd"})
					}
				}
			}
		}

		// Extract set prefix value: set prefix=($root)"/boot/grub2"
		if strings.HasPrefix(trimmed, "set prefix") {
			parts := strings.Split(trimmed, "=")
			if len(parts) == 2 {
				prefixVal := strings.TrimSpace(parts[1])
				prefixVal = strings.Trim(prefixVal, `"'`)
				// If it contains ($root), we'll expand it when we find the root value
				if strings.HasPrefix(prefixVal, "(") && strings.Contains(prefixVal, ")") {
					// Extract the path part: ($root)"/boot/grub2" -> /boot/grub2
					if idx := strings.Index(prefixVal, ")"); idx >= 0 {
						grubPrefix = strings.Trim(prefixVal[idx+1:], `"'`)
					}
				} else {
					grubPrefix = prefixVal
				}
			}
		}

		// Look for configfile directive (loads external config)
		if strings.HasPrefix(trimmed, "configfile") {
			parts := strings.Fields(trimmed)
			if len(parts) > 1 {
				configfilePath = strings.Trim(parts[1], `"'`)
				// Remove variable prefix if present
				if strings.HasPrefix(configfilePath, "(") {
					// Format like ($root)"/boot/grub2/grub.cfg"
					if idx := strings.Index(configfilePath, ")"); idx >= 0 {
						configfilePath = configfilePath[idx+1:]
						configfilePath = strings.Trim(configfilePath, `"'`)
					}
				} else if strings.HasPrefix(configfilePath, "$prefix") {
					// Expand $prefix variable
					if grubPrefix != "" {
						configfilePath = strings.Replace(configfilePath, "$prefix", grubPrefix, 1)
					}
				}
			}
		}

		// Look for search commands which may reference partition UUIDs
		if strings.HasPrefix(trimmed, "search") {
			// pick up any UUID-like token on the line
			for _, token := range strings.Fields(trimmed) {
				if strings.HasPrefix(token, "PARTUUID=") || strings.HasPrefix(token, "UUID=") {
					val := token
					if idx := strings.Index(val, "="); idx >= 0 {
						val = val[idx+1:]
					}
					val = strings.Trim(val, `"'`)
					for _, u := range extractUUIDsFromString(val) {
						cfg.UUIDReferences = append(cfg.UUIDReferences, UUIDReference{UUID: u, Context: "grub_search"})
					}
				} else {
					// also check raw tokens for UUIDs
					for _, u := range extractUUIDsFromString(token) {
						cfg.UUIDReferences = append(cfg.UUIDReferences, UUIDReference{UUID: u, Context: "grub_search"})
					}
				}
			}
		}
	}

	// If this is a stub config (has configfile), add metadata note
	if configfilePath != "" {
		note := fmt.Sprintf("Configuration note: This is a UEFI stub config that loads the main GRUB configuration from the root partition at '%s'. The actual boot entries are defined in that file.", configfilePath)
		cfg.Notes = append(cfg.Notes, note)

		// Add a synthetic entry showing where the config is
		stubEntry := BootEntry{
			Name:   "[External config] " + configfilePath,
			Kernel: configfilePath,
		}
		cfg.BootEntries = append(cfg.BootEntries, stubEntry)
	}

	// Simple parsing of menuentry blocks (for cases where config is inline)
	var currentEntry *BootEntry
	var inMenuEntry bool

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Detect menuentry start
		if strings.HasPrefix(trimmed, "menuentry") {
			if currentEntry != nil {
				cfg.BootEntries = append(cfg.BootEntries, *currentEntry)
			}
			currentEntry = parseGrubMenuEntry(trimmed)
			inMenuEntry = true
			continue
		}

		if inMenuEntry && currentEntry != nil {
			// Parse commonbootloader options
			if strings.HasPrefix(trimmed, "linux") || strings.HasPrefix(trimmed, "vmlinuz") {
				parts := strings.Fields(trimmed)
				if len(parts) > 1 {
					currentEntry.Kernel = parts[1]
					if len(parts) > 2 {
						currentEntry.Cmdline = strings.Join(parts[2:], " ")
						// Parse kernel cmdline tokens for root=PARTUUID=/UUID= references
						for _, tok := range strings.Fields(currentEntry.Cmdline) {
							if strings.HasPrefix(tok, "root=") {
								val := strings.TrimPrefix(tok, "root=")
								val = strings.Trim(val, `"'`)
								currentEntry.RootDevice = val
								// PARTUUID= or UUID= forms
								if strings.HasPrefix(val, "PARTUUID=") || strings.HasPrefix(val, "UUID=") {
									if idx := strings.Index(val, "="); idx >= 0 {
										id := val[idx+1:]
										id = strings.Trim(id, `"'`)
										for _, u := range extractUUIDsFromString(id) {
											currentEntry.PartitionUUID = u
											cfg.UUIDReferences = append(cfg.UUIDReferences, UUIDReference{UUID: u, Context: "kernel_cmdline"})
										}
									}
								} else {
									// bare UUIDs or device paths may still include UUIDs
									for _, u := range extractUUIDsFromString(val) {
										currentEntry.PartitionUUID = u
										cfg.UUIDReferences = append(cfg.UUIDReferences, UUIDReference{UUID: u, Context: "kernel_cmdline"})
									}
								}
							}
						}
					}
				}
			}
			if strings.HasPrefix(trimmed, "initrd") {
				parts := strings.Fields(trimmed)
				if len(parts) > 1 {
					currentEntry.Initrd = parts[1]
				}
			}

			// Check for root device reference
			if strings.Contains(trimmed, "root=") {
				if idx := strings.Index(trimmed, "root="); idx >= 0 {
					rest := trimmed[idx+5:]
					// Extract the device/UUID value (up to next space)
					if spaceIdx := strings.IndexByte(rest, ' '); spaceIdx >= 0 {
						currentEntry.RootDevice = rest[:spaceIdx]
					} else {
						currentEntry.RootDevice = rest
					}
				}
			}

			// End of entry (closing brace or next menuentry)
			if strings.HasPrefix(trimmed, "}") {
				inMenuEntry = false
			}
		}
	}

	// Add last entry if exists
	if currentEntry != nil {
		cfg.BootEntries = append(cfg.BootEntries, *currentEntry)
	}

	// Extract kernel references
	for _, entry := range cfg.BootEntries {
		if entry.Kernel != "" {
			ref := KernelReference{
				Path:      entry.Kernel,
				BootEntry: entry.Name,
			}
			if entry.RootDevice != "" {
				ref.RootUUID = entry.RootDevice
			}
			if entry.PartitionUUID != "" {
				ref.PartitionUUID = entry.PartitionUUID
			}
			cfg.KernelReferences = append(cfg.KernelReferences, ref)
		}
	}

	return cfg
}

// parseGrubMenuEntry extracts title/name from a menuentry line.
func parseGrubMenuEntry(menuLine string) *BootEntry {
	entry := &BootEntry{}

	// Extract text between quotes: menuentry 'Title' { or menuentry "Title" {
	for _, q := range []rune{'\'', '"'} {
		start := strings.IndexRune(menuLine, q)
		if start >= 0 {
			end := strings.IndexRune(menuLine[start+1:], q)
			if end >= 0 {
				entry.Name = menuLine[start+1 : start+1+end]
				return entry
			}
		}
	}

	// Fallback: extract whatever is between "menuentry" and "{", if safely available.
	prefix := "menuentry"
	if strings.HasPrefix(menuLine, prefix) {
		if idx := strings.Index(menuLine, "{"); idx > len(prefix) {
			entry.Name = strings.TrimSpace(menuLine[len(prefix):idx])
		}
	}

	return entry
}

// parseSystemdBootEntries extracts boot entries from systemd-boot loader config.
func parseSystemdBootEntries(content string) BootloaderConfig {
	cfg := BootloaderConfig{
		ConfigRaw:        make(map[string]string),
		KernelReferences: []KernelReference{},
		BootEntries:      []BootEntry{},
		UUIDReferences:   []UUIDReference{},
		Notes:            []string{},
	}

	if content == "" {
		cfg.Notes = append(cfg.Notes, "loader.conf is empty")
		return cfg
	}

	if len(content) > 10240 {
		cfg.ConfigRaw["loader.conf"] = content[:10240] + "\n[truncated...]"
	} else {
		cfg.ConfigRaw["loader.conf"] = content
	}

	// Extract UUIDs from config
	uuids := extractUUIDsFromString(content)
	for _, uuid := range uuids {
		cfg.UUIDReferences = append(cfg.UUIDReferences, UUIDReference{
			UUID:    uuid,
			Context: "systemd_boot_config",
		})
	}

	// Parse simple key=value pairs
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if strings.HasPrefix(trimmed, "default") {
			parts := strings.SplitN(trimmed, "=", 2)
			if len(parts) == 2 {
				cfg.DefaultEntry = strings.TrimSpace(parts[1])
			}
		}
	}

	return cfg
}

// resolveUUIDsToPartitions matches UUIDs in bootloader config against partition GUIDs.
// It returns a map of UUID -> partition index.
func resolveUUIDsToPartitions(uuidRefs []UUIDReference, pt PartitionTableSummary) map[string]int {
	result := make(map[string]int)

	for _, ref := range uuidRefs {
		// If the token is a GPT/MSDOS partition spec like 'gpt2' or 'msdos1', map directly
		low := strings.ToLower(ref.UUID)
		if strings.HasPrefix(low, "gpt") || strings.HasPrefix(low, "msdos") {
			// Extract trailing number
			digits := ""
			for i := len(low) - 1; i >= 0; i-- {
				if low[i] < '0' || low[i] > '9' {
					digits = low[i+1:]
					break
				}
				if i == 0 {
					digits = low
				}
			}
			if digits != "" {
				// convert to int
				var idx int
				if _, err := fmt.Sscanf(digits, "%d", &idx); err == nil {
					if idx > 0 {
						result[ref.UUID] = idx
						continue
					}
				}
			}
		}

		// Otherwise, try to match GUIDs (partition GUIDs) or filesystem UUIDs
		normalized := normalizeUUID(ref.UUID)
		for _, p := range pt.Partitions {
			if normalizeUUID(p.GUID) == normalized {
				result[ref.UUID] = p.Index
				break
			}
			// Also check filesystem UUID
			if p.Filesystem != nil && normalizeUUID(p.Filesystem.UUID) == normalized {
				result[ref.UUID] = p.Index
				break
			}
		}
	}

	return result
}

// ValidateBootloaderConfig checks for common configuration issues.
func ValidateBootloaderConfig(cfg *BootloaderConfig, pt PartitionTableSummary) {
	if cfg == nil {
		return
	}

	// Check for missing config files
	if len(cfg.ConfigFiles) == 0 && len(cfg.ConfigRaw) == 0 {
		cfg.Notes = append(cfg.Notes, "No bootloader configuration files found")
	}

	// Resolve UUIDs and check for mismatches
	uuidMap := resolveUUIDsToPartitions(cfg.UUIDReferences, pt)
	for i, uuidRef := range cfg.UUIDReferences {
		if _, found := uuidMap[uuidRef.UUID]; found {
			cfg.UUIDReferences[i].ReferencedPartition = uuidMap[uuidRef.UUID]
		} else {
			cfg.UUIDReferences[i].Mismatch = true
			cfg.Notes = append(cfg.Notes,
				fmt.Sprintf("UUID %s referenced in %s not found in partition table", uuidRef.UUID, uuidRef.Context))
		}
	}

	// Check for kernel references without valid paths
	for _, kernRef := range cfg.KernelReferences {
		if kernRef.Path == "" {
			cfg.Notes = append(cfg.Notes, fmt.Sprintf("Boot entry %s has no kernel path", kernRef.BootEntry))
		}
	}

	// Check for boot entries without kernel
	for _, entry := range cfg.BootEntries {
		if entry.Kernel == "" {
			cfg.Notes = append(cfg.Notes, fmt.Sprintf("Boot entry '%s' has no kernel path", entry.Name))
		}
	}
}
