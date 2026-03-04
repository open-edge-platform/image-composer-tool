package imageinspect

import (
	"strings"
	"testing"
)

func TestParseGrubConfig(t *testing.T) {
	grubContent := `
menuentry 'Ubuntu 24.04 LTS (5.15.0-105-generic)' {
	search --no-floppy --label BOOT --set root
	echo	'Loading Ubuntu 24.04 LTS (5.15.0-105-generic)'
	linux	/vmlinuz-5.15.0-105-generic root=UUID=550e8400-e29b-41d4-a716-446655440000 ro quiet splash
	echo	'Loading initial ramdisk'
	initrd	/initrd.img-5.15.0-105-generic
}

menuentry 'Ubuntu 24.04 LTS (5.14.0-104-generic)' {
	search --no-floppy --label BOOT --set root
	echo	'Loading Ubuntu 24.04 LTS (5.14.0-104-generic)'
	linux	/vmlinuz-5.14.0-104-generic root=UUID=550e8400-e29b-41d4-a716-446655440000 ro quiet splash
	echo	'Loading initial ramdisk'
	initrd	/initrd.img-5.14.0-104-generic
}
`

	cfg := parseGrubConfigContent(grubContent)

	// Verify boot entries were parsed
	if len(cfg.BootEntries) != 2 {
		t.Errorf("Expected 2 boot entries, got %d", len(cfg.BootEntries))
	}

	// Verify kernel references were extracted
	if len(cfg.KernelReferences) != 2 {
		t.Errorf("Expected 2 kernel references, got %d", len(cfg.KernelReferences))
	}

	// Verify UUIDs were extracted
	if len(cfg.UUIDReferences) == 0 {
		t.Errorf("Expected UUID references, got none")
	}

	// Check specific entry
	if cfg.BootEntries[0].Name != "Ubuntu 24.04 LTS (5.15.0-105-generic)" {
		t.Errorf("Boot entry name mismatch: %s", cfg.BootEntries[0].Name)
	}

	if cfg.BootEntries[0].Kernel != "/vmlinuz-5.15.0-105-generic" {
		t.Errorf("Kernel path mismatch: %s", cfg.BootEntries[0].Kernel)
	}

	if cfg.BootEntries[0].Initrd != "/initrd.img-5.15.0-105-generic" {
		t.Errorf("Initrd path mismatch: %s", cfg.BootEntries[0].Initrd)
	}

	if cfg.BootEntries[0].RootDevice == "" {
		t.Errorf("Root device not extracted")
	}
}

func TestExtractUUIDs(t *testing.T) {
	testCases := []struct {
		input    string
		expected int
	}{
		{
			input:    "UUID=550e8400-e29b-41d4-a716-446655440000",
			expected: 1,
		},
		{
			input:    "PARTUUID=550e8400-e29b-41d4-a716-446655440000 root=/dev/vda2",
			expected: 1,
		},
		{
			input:    "UUID=550e8400-e29b-41d4-a716-446655440000 and UUID=550e8400-e29b-41d4-a716-446655440001",
			expected: 2,
		},
		{
			input:    "no uuids here",
			expected: 0,
		},
	}

	for _, tc := range testCases {
		uuids := extractUUIDsFromString(tc.input)
		if len(uuids) != tc.expected {
			t.Errorf("Input: %q - Expected %d UUIDs, got %d", tc.input, tc.expected, len(uuids))
		}
	}
}

func TestCompareBootloaderConfigs(t *testing.T) {
	cfgFrom := &BootloaderConfig{
		ConfigFiles: map[string]string{
			"/boot/grub/grub.cfg": "abc123",
		},
		BootEntries: []BootEntry{
			{
				Name:   "Linux (old)",
				Kernel: "/vmlinuz-5.14",
				Initrd: "/initrd-5.14",
			},
		},
		KernelReferences: []KernelReference{
			{
				Path: "/vmlinuz-5.14",
			},
		},
	}

	cfgTo := &BootloaderConfig{
		ConfigFiles: map[string]string{
			"/boot/grub/grub.cfg": "def456", // Changed
		},
		BootEntries: []BootEntry{
			{
				Name:   "Linux (old)",
				Kernel: "/vmlinuz-5.15", // Changed
				Initrd: "/initrd-5.15",  // Changed
			},
			{
				Name:   "Linux (new)",
				Kernel: "/vmlinuz-5.16",
				Initrd: "/initrd-5.16",
			},
		},
		KernelReferences: []KernelReference{
			{
				Path: "/vmlinuz-5.15",
			},
			{
				Path: "/vmlinuz-5.16",
			},
		},
	}

	diff := compareBootloaderConfigs(cfgFrom, cfgTo)

	if diff == nil {
		t.Fatalf("Expected diff, got nil")
	}

	// Should detect config file change
	if len(diff.ConfigFileChanges) != 1 || diff.ConfigFileChanges[0].Status != "modified" {
		t.Errorf("Expected 1 modified config file, got %d changes", len(diff.ConfigFileChanges))
	}

	// Should detect boot entry modification
	if len(diff.BootEntryChanges) != 2 {
		t.Errorf("Expected 2 boot entry changes (1 modified, 1 added), got %d", len(diff.BootEntryChanges))
	}

	modifiedFound := false
	for _, change := range diff.BootEntryChanges {
		if change.Status != "modified" || change.Name != "Linux (old)" {
			continue
		}
		modifiedFound = true
		if change.InitrdFrom != "/initrd-5.14" || change.InitrdTo != "/initrd-5.15" {
			t.Errorf("Expected initrd change /initrd-5.14 -> /initrd-5.15, got %q -> %q", change.InitrdFrom, change.InitrdTo)
		}
	}
	if !modifiedFound {
		t.Errorf("Expected modified boot entry change for Linux (old)")
	}

	// Should detect kernel reference changes
	// Old vmlinuz-5.14 removed, vmlinuz-5.15 modified, vmlinuz-5.16 added = 3 changes
	if len(diff.KernelRefChanges) != 3 {
		t.Errorf("Expected 3 kernel reference changes, got %d", len(diff.KernelRefChanges))
	}
}

func TestUUIDResolution(t *testing.T) {
	pt := PartitionTableSummary{
		Partitions: []PartitionSummary{
			{
				Index: 1,
				GUID:  "550e8400-e29b-41d4-a716-446655440000",
			},
			{
				Index: 2,
				GUID:  "550e8400-e29b-41d4-a716-446655440001",
				Filesystem: &FilesystemSummary{
					UUID: "550e8400-e29b-41d4-a716-446655440002",
				},
			},
		},
	}

	uuidRefs := []UUIDReference{
		{
			UUID: "550e8400-e29b-41d4-a716-446655440000",
		},
		{
			UUID: "550e8400-e29b-41d4-a716-446655440002",
		},
		{
			UUID: "99999999-9999-9999-9999-999999999999", // Non-existent
		},
	}

	resolved := resolveUUIDsToPartitions(uuidRefs, pt)

	if len(resolved) != 2 {
		t.Errorf("Expected 2 resolved UUIDs, got %d", len(resolved))
	}

	if part, ok := resolved["550e8400-e29b-41d4-a716-446655440000"]; !ok || part != 1 {
		t.Errorf("First UUID should resolve to partition 1")
	}

	if part, ok := resolved["550e8400-e29b-41d4-a716-446655440002"]; !ok || part != 2 {
		t.Errorf("Second UUID should resolve to partition 2 (filesystem UUID)")
	}
}

func TestValidateBootloaderConfig(t *testing.T) {
	pt := PartitionTableSummary{
		Partitions: []PartitionSummary{
			{
				Index: 1,
				GUID:  "550e8400-e29b-41d4-a716-446655440000",
			},
		},
	}

	cfg := &BootloaderConfig{
		BootEntries: []BootEntry{
			{
				Name:   "Test",
				Kernel: "", // Empty kernel - should trigger issue
			},
		},
		UUIDReferences: []UUIDReference{
			{
				UUID:    "99999999-9999-9999-9999-999999999999", // Invalid UUID
				Context: "test",
			},
		},
	}

	ValidateBootloaderConfig(cfg, pt)

	if len(cfg.Notes) == 0 {
		t.Errorf("Expected validation notes, got none")
	}

	// Should have note about missing kernel path
	hasKernelIssue := false
	hasMismatchIssue := false

	for _, note := range cfg.Notes {
		if len(note) > 0 {
			if note[0] == 'B' { // "Boot entry..."
				hasKernelIssue = true
			}
			if note[0] == 'U' { // "UUID..."
				hasMismatchIssue = true
			}
		}
	}

	if !hasKernelIssue {
		t.Errorf("Expected kernel issue not found")
	}

	if !hasMismatchIssue {
		t.Errorf("Expected UUID mismatch issue not found")
	}
}

func ExampleBootloaderConfig() {
	// Simulate extracting config from two images
	grubConfig1 := `
menuentry 'Linux' {
	linux /vmlinuz-5.14 root=UUID=550e8400-e29b-41d4-a716-446655440000 ro
	initrd /initrd-5.14
}
`

	grubConfig2 := `
menuentry 'Linux' {
	linux /vmlinuz-5.15 root=UUID=550e8400-e29b-41d4-a716-446655440000 ro
	initrd /initrd-5.15
}
`

	cfg1 := parseGrubConfigContent(grubConfig1)
	cfg2 := parseGrubConfigContent(grubConfig2)

	// Compare configurations
	diff := compareBootloaderConfigs(&cfg1, &cfg2)

	if diff != nil && len(diff.BootEntryChanges) > 0 {
		// Kernel was updated in the boot entry
		for _, change := range diff.BootEntryChanges {
			if change.Status == "modified" && change.KernelFrom != change.KernelTo {
				// Log that kernel version changed
			}
		}
	}

	// Check for UUID mismatches
	pt := PartitionTableSummary{
		Partitions: []PartitionSummary{
			{Index: 1, GUID: "550e8400-e29b-41d4-a716-446655440000"},
		},
	}

	ValidateBootloaderConfig(&cfg1, pt)
	// cfg1.Notes would contain any validation problems
}

// TestParseGrubConfigWithSearchPartuuid tests GRUB search directive with PARTUUID
func TestParseGrubConfigWithSearchPartuuid(t *testing.T) {
	grubCfg := `search --fs-uuid --no-floppy --set=root f4633aa1-3137-4424-ad60-c680a5016ee2
menuentry 'Linux' {
	linux /vmlinuz-5.14 root=PARTUUID=f4633aa1-3137-4424-ad60-c680a5016ee2 ro
	initrd /initrd-5.14
}`
	cfg := parseGrubConfigContent(grubCfg)

	if len(cfg.UUIDReferences) == 0 {
		t.Fatal("Expected UUID references extracted from search directive and kernel cmdline")
	}

	// Verify context annotation
	foundSearchUUID := false
	foundCmdlineUUID := false
	for _, ref := range cfg.UUIDReferences {
		if ref.Context == "grub_search" {
			foundSearchUUID = true
		}
		if ref.Context == "kernel_cmdline" {
			foundCmdlineUUID = true
		}
	}

	if !foundSearchUUID {
		t.Error("Expected UUID from search directive with context 'grub_search'")
	}
	if !foundCmdlineUUID {
		t.Error("Expected UUID from kernel cmdline with context 'kernel_cmdline'")
	}
}

// TestParseGrubConfigWithDeviceSpec tests GRUB device notation
func TestParseGrubConfigWithDeviceSpec(t *testing.T) {
	grubCfg := `menuentry 'Linux' {
	insmod gzio
	set root='(hd0,gpt2)'
	linux /vmlinuz
}`
	cfg := parseGrubConfigContent(grubCfg)

	// Verify gpt2 is captured as a reference
	foundGpt2 := false
	for _, ref := range cfg.UUIDReferences {
		if strings.Contains(ref.UUID, "gpt2") && ref.Context == "grub_root_hd" {
			foundGpt2 = true
			break
		}
	}

	if !foundGpt2 {
		t.Error("Expected 'gpt2' captured from device spec (hd0,gpt2) with context 'grub_root_hd'")
	}
}

// TestParseGrubConfigWithMsdosDeviceSpec tests GRUB device notation with MBR
func TestParseGrubConfigWithMsdosDeviceSpec(t *testing.T) {
	grubCfg := `menuentry 'Windows' {
	insmod registry
	set root='(hd0,msdos1)'
	linux /vmlinuz
}`
	cfg := parseGrubConfigContent(grubCfg)

	foundMsdos1 := false
	for _, ref := range cfg.UUIDReferences {
		if strings.Contains(ref.UUID, "msdos1") && ref.Context == "grub_root_hd" {
			foundMsdos1 = true
			break
		}
	}

	if !foundMsdos1 {
		t.Error("Expected 'msdos1' captured from device spec (hd0,msdos1)")
	}
}

// TestParseGrubConfigMultipleMenuEntries tests multiple boot entries
func TestParseGrubConfigMultipleMenuEntries(t *testing.T) {
	grubCfg := `menuentry 'Linux First' {
	linux /vmlinuz-5.14 root=/dev/sda1 ro
}
menuentry 'Linux Second' {
	linux /vmlinuz-5.15 root=/dev/sda1 ro
}`
	cfg := parseGrubConfigContent(grubCfg)

	if len(cfg.BootEntries) != 2 {
		t.Errorf("Expected 2 boot entries, got %d", len(cfg.BootEntries))
	}
	if cfg.BootEntries[0].Name != "Linux First" {
		t.Errorf("Expected first entry 'Linux First', got %s", cfg.BootEntries[0].Name)
	}
	if cfg.BootEntries[1].Name != "Linux Second" {
		t.Errorf("Expected second entry 'Linux Second', got %s", cfg.BootEntries[1].Name)
	}
}

// TestParseGrubConfigWithExternalConfigfile tests stub config with config
func TestParseGrubConfigWithExternalConfigfile(t *testing.T) {
	grubCfg := `set prefix=($root)"/boot/grub2"
configfile ($root)"/boot/grub2/grub.cfg"`

	cfg := parseGrubConfigContent(grubCfg)

	if len(cfg.Notes) == 0 {
		t.Error("Expected note about external configfile")
	}

	found := false
	for _, note := range cfg.Notes {
		if strings.Contains(note, "stub config") && strings.Contains(note, "/boot/grub2/grub.cfg") {
			found = true
		}
	}
	if !found {
		t.Error("Expected note about UEFI stub config and external config file path")
	}
}

// TestParseSystemdBootConfig tests systemd-boot loader configuration
func TestParseSystemdBootConfig(t *testing.T) {
	loaderCfg := `timeout=5
default=linux
editor=no
auto-firmware=no`

	cfg := parseSystemdBootEntries(loaderCfg)

	if cfg.DefaultEntry != "linux" {
		t.Errorf("Expected default entry 'linux', got %s", cfg.DefaultEntry)
	}
	if len(cfg.ConfigRaw) == 0 {
		t.Error("Expected raw config to be stored")
	}
}

// TestResolvePartitionSpecGpt tests resolving gpt2 to partition index
func TestResolvePartitionSpecGpt(t *testing.T) {
	refs := []UUIDReference{{UUID: "gpt2", Context: "test"}}
	pt := PartitionTableSummary{
		Partitions: []PartitionSummary{
			{Index: 1, GUID: "ABC123"},
			{Index: 2, GUID: "DEF456"},
		},
	}

	result := resolveUUIDsToPartitions(refs, pt)

	if result["gpt2"] != 2 {
		t.Errorf("Expected partition 2 for 'gpt2', got %d", result["gpt2"])
	}
}

// TestResolvePartitionSpecMsdos tests resolving msdos1 to partition index
func TestResolvePartitionSpecMsdos(t *testing.T) {
	refs := []UUIDReference{{UUID: "msdos1", Context: "test"}}
	pt := PartitionTableSummary{
		Partitions: []PartitionSummary{
			{Index: 1, GUID: "ABC123"},
			{Index: 2, GUID: "DEF456"},
		},
	}

	result := resolveUUIDsToPartitions(refs, pt)

	if result["msdos1"] != 1 {
		t.Errorf("Expected partition 1 for 'msdos1', got %d", result["msdos1"])
	}
}

// TestResolveUUIDAgainstPartitionGUID tests UUID resolution against partition GUID
func TestResolveUUIDAgainstPartitionGUID(t *testing.T) {
	refs := []UUIDReference{{UUID: "f4633aa1-3137-4424-ad60-c680a5016ee2", Context: "test"}}
	pt := PartitionTableSummary{
		Partitions: []PartitionSummary{
			{Index: 1, GUID: "11111111-1111-1111-1111-111111111111"},
			{Index: 2, GUID: "f4633aa1-3137-4424-ad60-c680a5016ee2"},
		},
	}

	result := resolveUUIDsToPartitions(refs, pt)

	if result["f4633aa1-3137-4424-ad60-c680a5016ee2"] != 2 {
		t.Errorf("Expected partition 2, got %d", result["f4633aa1-3137-4424-ad60-c680a5016ee2"])
	}
}

// TestResolveUUIDAgainstFilesystemUUID tests UUID resolution against filesystem UUID
func TestResolveUUIDAgainstFilesystemUUID(t *testing.T) {
	refs := []UUIDReference{{UUID: "f4633aa1-3137-4424-ad60-c680a5016ee2", Context: "test"}}
	pt := PartitionTableSummary{
		Partitions: []PartitionSummary{
			{
				Index: 2,
				GUID:  "11111111-1111-1111-1111-111111111111",
				Filesystem: &FilesystemSummary{
					UUID: "f4633aa1-3137-4424-ad60-c680a5016ee2",
				},
			},
		},
	}

	result := resolveUUIDsToPartitions(refs, pt)

	if result["f4633aa1-3137-4424-ad60-c680a5016ee2"] != 2 {
		t.Errorf("Expected partition 2 from filesystem UUID, got %d", result["f4633aa1-3137-4424-ad60-c680a5016ee2"])
	}
}

// TestSynthesizeBootConfigFromUKI_BootUUID tests boot_uuid extraction from UKI cmdline
func TestSynthesizeBootConfigFromUKI_BootUUID(t *testing.T) {
	uki := &EFIBinaryEvidence{
		Path:    "EFI/Linux/test.efi",
		Cmdline: "root=/dev/mapper/root boot_uuid=f4633aa1-3137-4424-ad60-c680a5016ee2 other=value",
	}
	cfg := synthesizeBootConfigFromUKI(uki)

	// Check that boot_uuid is extracted with proper context
	foundBootUUID := false
	for _, ref := range cfg.UUIDReferences {
		if ref.Context == "uki_boot_uuid" {
			foundBootUUID = true
			break
		}
	}
	if !foundBootUUID {
		t.Error("Expected boot_uuid extracted to UUIDReferences with context 'uki_boot_uuid'")
	}
}

// TestSynthesizeBootConfigFromUKI_RootDevice tests root device extraction from UKI
func TestSynthesizeBootConfigFromUKI_RootDevice(t *testing.T) {
	uki := &EFIBinaryEvidence{
		Path:    "EFI/Linux/test.efi",
		Cmdline: "root=/dev/mapper/rootfs_verity quiet splash",
	}
	cfg := synthesizeBootConfigFromUKI(uki)

	if len(cfg.BootEntries) != 1 {
		t.Fatal("Expected 1 synthesized boot entry")
	}

	entry := cfg.BootEntries[0]
	if entry.RootDevice != "/dev/mapper/rootfs_verity" {
		t.Errorf("Expected root device '/dev/mapper/rootfs_verity', got %s", entry.RootDevice)
	}
}

// TestSynthesizeBootConfigFromUKI_Empty tests edge case with empty UKI cmdline
func TestSynthesizeBootConfigFromUKI_Empty(t *testing.T) {
	uki := &EFIBinaryEvidence{
		Path:    "EFI/Linux/test.efi",
		Cmdline: "",
	}
	cfg := synthesizeBootConfigFromUKI(uki)

	if len(cfg.Notes) == 0 {
		t.Error("Expected note about empty UKI cmdline")
	}
}

// TestValidateBootloaderConfig_UUIDMismatch tests detection of UUID mismatches
func TestValidateBootloaderConfig_UUIDMismatch(t *testing.T) {
	cfg := &BootloaderConfig{
		UUIDReferences: []UUIDReference{
			{UUID: "99999999-9999-9999-9999-999999999999", Context: "test"},
		},
		Notes: []string{},
	}
	pt := PartitionTableSummary{
		Partitions: []PartitionSummary{
			{Index: 1, GUID: "11111111-1111-1111-1111-111111111111"},
		},
	}

	ValidateBootloaderConfig(cfg, pt)

	// Should have marked UUID as mismatch
	if !cfg.UUIDReferences[0].Mismatch {
		t.Error("Expected UUID mismatch to be detected")
	}
	if len(cfg.Notes) == 0 {
		t.Error("Expected notes about mismatched UUID")
	}
}

// TestValidateBootloaderConfig_MultipleIssues tests detection of multiple issues
func TestValidateBootloaderConfig_MultipleIssues(t *testing.T) {
	cfg := &BootloaderConfig{
		BootEntries: []BootEntry{
			{Name: "Boot1", Kernel: ""}, // Missing kernel
			{Name: "Boot2", Kernel: "/vmlinuz"},
		},
		KernelReferences: []KernelReference{
			{Path: "", BootEntry: "BadEntry"}, // Missing path
		},
		Notes: []string{},
	}
	pt := PartitionTableSummary{Partitions: []PartitionSummary{}}

	ValidateBootloaderConfig(cfg, pt)

	// Should have multiple notes
	if len(cfg.Notes) < 2 {
		t.Errorf("Expected at least 2 notes, got %d", len(cfg.Notes))
	}
}

// TestValidateBootloaderConfig_NoConfigFiles tests detection of missing config
func TestValidateBootloaderConfig_NoConfigFiles(t *testing.T) {
	cfg := &BootloaderConfig{
		ConfigFiles: map[string]string{},
		ConfigRaw:   map[string]string{},
		Notes:       []string{},
	}
	pt := PartitionTableSummary{Partitions: []PartitionSummary{}}

	ValidateBootloaderConfig(cfg, pt)

	if len(cfg.Notes) == 0 || !strings.Contains(cfg.Notes[0], "No bootloader configuration") {
		t.Error("Expected note about missing config files")
	}
}

// TestExtractUUIDsFromString tests UUID extraction and normalization
func TestExtractUUIDsFromString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
		contains string
	}{
		{
			name:     "Standard UUID",
			input:    "f4633aa1-3137-4424-ad60-c680a5016ee2",
			expected: 1,
			contains: "f4633aa13137442",
		},
		{
			name:     "Multiple UUIDs",
			input:    "root=f4633aa1-3137-4424-ad60-c680a5016ee2 boot=11111111-1111-1111-1111-111111111111",
			expected: 2,
			contains: "f4633aa13137442",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: 0,
			contains: "",
		},
		{
			name:     "No UUIDs",
			input:    "root=/dev/sda1 boot=/boot",
			expected: 0,
			contains: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractUUIDsFromString(tt.input)
			if len(result) != tt.expected {
				t.Errorf("Expected %d UUIDs, got %d", tt.expected, len(result))
			}
			if tt.expected > 0 && !strings.Contains(result[0], tt.contains) {
				t.Errorf("Expected UUID containing %s, got %s", tt.contains, result[0])
			}
		})
	}
}

// TestNormalizeUUID tests UUID normalization
func TestNormalizeUUID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"F4633AA1-3137-4424-AD60-C680A5016EE2", "f4633aa13137442"},
		{"f4633aa1_3137_4424_ad60_c680a5016ee2", "f4633aa13137442"},
		{"f4633aa13137442ad60c680a5016ee2", "f4633aa13137442"},
	}

	for _, tt := range tests {
		result := normalizeUUID(tt.input)
		if !strings.HasPrefix(result, tt.expected) {
			t.Errorf("Expected %s, got %s", tt.expected, result)
		}
	}
}

// TestParseGrubMenuEntry tests menu entry name extraction
func TestParseGrubMenuEntry(t *testing.T) {
	tests := []struct {
		menuLine string
		expected string
	}{
		{`menuentry 'Ubuntu 20.04' {`, "Ubuntu 20.04"},
		{`menuentry "Fedora System" {`, "Fedora System"},
		{`menuentry Ubuntu-20.04 {`, "Ubuntu-20.04"},
	}

	for _, tt := range tests {
		entry := parseGrubMenuEntry(tt.menuLine)
		if entry.Name != tt.expected {
			t.Errorf("Expected %s, got %s", tt.expected, entry.Name)
		}
	}
}
