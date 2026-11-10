package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBootloaderTimeoutSecParsing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		extra   string // lines added under systemConfig.bootloader
		want    *int
		wantErr bool
	}{
		{"unset keeps the default", "", nil, false},
		{"zero is kept, not treated as unset", "timeoutSec: 0", intPtr(0), false},
		{"positive value", "timeoutSec: 3", intPtr(3), false},
		{"grub provider", "provider: grub\n    timeoutSec: 1", intPtr(1), false},
		{"negative value", "timeoutSec: -1", nil, true},
		{"not an integer", "timeoutSec: soon", nil, true},
		{"fractional value", "timeoutSec: 1.5", nil, true},
		{"systemd-boot has no equivalent", "provider: systemd-boot\n    timeoutSec: 3", nil, true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			yaml := provisioningTemplateYAML
			if tt.extra != "" {
				yaml = strings.Replace(yaml, "bootEntryPolicy: exclusive",
					"bootEntryPolicy: exclusive\n    "+tt.extra, 1)
			}
			tmpl, err := parseYAMLTemplate([]byte(yaml), false)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected template to be rejected")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseYAMLTemplate: %v", err)
			}
			got := tmpl.SystemConfig.Bootloader.TimeoutSec
			switch {
			case tt.want == nil && got != nil:
				t.Errorf("TimeoutSec = %d, want unset", *got)
			case tt.want != nil && got == nil:
				t.Errorf("TimeoutSec unset, want %d", *tt.want)
			case tt.want != nil && *got != *tt.want:
				t.Errorf("TimeoutSec = %d, want %d", *got, *tt.want)
			}
		})
	}
}

func TestMergeBootloaderTimeoutSec(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		defaultValue *int
		userValue    *int
		want         *int
	}{
		{"neither set", nil, nil, nil},
		{"default kept when the user sets nothing", intPtr(5), nil, intPtr(5)},
		{"user value applies", nil, intPtr(2), intPtr(2)},
		{"user value overrides the default", intPtr(5), intPtr(1), intPtr(1)},
		{"user zero overrides a non-zero default", intPtr(5), intPtr(0), intPtr(0)},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			merged := mergeBootloader(
				Bootloader{Provider: "grub", TimeoutSec: tt.defaultValue},
				Bootloader{TimeoutSec: tt.userValue})
			if (merged.TimeoutSec == nil) != (tt.want == nil) ||
				(tt.want != nil && *merged.TimeoutSec != *tt.want) {
				t.Errorf("merged TimeoutSec = %v, want %v", merged.TimeoutSec, tt.want)
			}
			if merged.Provider != "grub" {
				t.Errorf("provider = %q, want it preserved", merged.Provider)
			}
		})
	}
}

func TestMergeBootloaderTimeoutSecIsCopied(t *testing.T) {
	t.Parallel()
	user := Bootloader{TimeoutSec: intPtr(2)}
	merged := mergeBootloader(Bootloader{}, user)
	*user.TimeoutSec = 99
	if *merged.TimeoutSec != 2 {
		t.Errorf("merged TimeoutSec changed to %d after the source was modified", *merged.TimeoutSec)
	}
}

func TestIsEmptyBootloaderWithTimeoutSec(t *testing.T) {
	t.Parallel()
	if isEmptyBootloader(Bootloader{TimeoutSec: intPtr(0)}) {
		t.Error("a bootloader with only timeoutSec set must not count as empty")
	}
	if !isEmptyBootloader(Bootloader{}) {
		t.Error("a bootloader with nothing set must count as empty")
	}
}

// writeBootloaderTemplate writes a minimal template whose systemConfig.bootloader
// holds bootloaderYAML, optionally extending a parent.
func writeBootloaderTemplate(t *testing.T, path, name, extends, bootloaderYAML string) {
	t.Helper()
	var b strings.Builder
	if extends != "" {
		b.WriteString("extends: \"" + extends + "\"\n")
	}
	b.WriteString("image:\n  name: " + name + "\n  version: \"1.0.0\"\n")
	b.WriteString("target:\n  os: ubuntu\n  dist: ubuntu24\n  arch: x86_64\n  imageType: raw\n")
	b.WriteString("systemConfig:\n  name: " + name + "-config\n  bootloader:\n")
	for _, line := range strings.Split(bootloaderYAML, "\n") {
		b.WriteString("    " + line + "\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("failed to write template %s: %v", path, err)
	}
}

func TestResolveExtendsChainChecksTimeoutAgainstMergedProvider(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		parent  string
		child   string
		wantErr bool
	}{
		{"parent systemd-boot, child timeout", "provider: systemd-boot", "timeoutSec: 3", true},
		{"parent timeout, child systemd-boot", "timeoutSec: 3", "provider: systemd-boot", true},
		{"parent timeout, child grub", "timeoutSec: 3", "provider: grub", false},
		{"parent grub, child timeout", "provider: grub", "timeoutSec: 3", false},
		{"parent systemd-boot, child grub and timeout", "provider: systemd-boot", "provider: grub\ntimeoutSec: 3", false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeBootloaderTemplate(t, filepath.Join(dir, "parent.yml"), "parent", "", tt.parent)
			childPath := filepath.Join(dir, "child.yml")
			writeBootloaderTemplate(t, childPath, "child", "parent.yml", tt.child)

			_, _, err := ResolveAndMergeExtendsChain(childPath, nil)
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "timeoutSec") {
					t.Fatalf("expected a timeoutSec error, got: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
