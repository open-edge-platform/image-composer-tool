package overlay

import (
	"os"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

// This file is the consolidated negative-path suite for overlay builds. Each of
// the unsupported scenarios the overlay pipeline must reject is exercised here
// through its real (pure) decision function, and every rejection is asserted to
// produce a clear, actionable error: it names what was DETECTED, and gives the
// user something to change (the EXPECTED/allowed set or a REMEDIATION). The
// per-subsystem tests (layout_test.go, detect_test.go, preflight_test.go,
// resize_test.go, overlay_test.go) still own the fine-grained behavior; this file
// guarantees blanket coverage and a consistent error UX across all scenarios in
// one place.

// assertActionableError fails unless err is non-nil and its message contains every
// required substring (case-insensitive). It is the general-purpose sibling of
// layout_test.go's assertActionable, which additionally requires the typed
// *unsupportedLayoutError; the ingestion/preflight/resize paths use plain errors,
// so this checks message content only.
func assertActionableError(t *testing.T, err error, wantSubstrings ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error, got nil")
	}
	msg := strings.ToLower(err.Error())
	for _, want := range wantSubstrings {
		if !strings.Contains(msg, strings.ToLower(want)) {
			t.Errorf("error %q does not mention %q", err.Error(), want)
		}
	}
}

// TestNegative_LayoutScenarios covers the baseline-topology rejections that flow
// through analyzeLayout: LUKS, dm-verity, unknown/unsupported filesystem, no
// recognizable filesystem, and LVM. Every one must be a typed
// *unsupportedLayoutError carrying detected/reason/remediation (asserted via
// assertActionable), so the failure names the exact layout and how to fix it.
func TestNegative_LayoutScenarios(t *testing.T) {
	rootGUID := "4f68bce3-e8cd-4db1-96e7-fbcaf984b709" // x86-64 Linux root
	verityGUID := "2c7357ed-ebd2-46d9-aec1-23d437ec2bf5"

	tests := []struct {
		name         string
		table        string
		parts        []partition
		wantDetected string // substring, matched case-insensitively by assertActionable
	}{
		{
			name:         "LUKS encrypted root",
			table:        partitionTableGPT,
			parts:        []partition{{Path: "/dev/loop0p2", FSType: fsTypeLUKS, Size: 8 << 30}},
			wantDetected: "crypto_LUKS",
		},
		{
			name:  "dm-verity protected root",
			table: partitionTableGPT,
			parts: []partition{
				{Path: "/dev/loop0p1", FSType: "ext4", PartType: rootGUID, Size: 8 << 30},
				{Path: "/dev/loop0p2", PartType: verityGUID, Size: 256 << 20},
			},
			wantDetected: "dm-verity",
		},
		{
			name:         "unsupported filesystem (btrfs)",
			table:        partitionTableGPT,
			parts:        []partition{{Path: "/dev/loop0p1", FSType: "btrfs", PartType: rootGUID, Size: 8 << 30}},
			wantDetected: "btrfs",
		},
		{
			name:         "no recognizable filesystem",
			table:        partitionTableGPT,
			parts:        []partition{{Path: "/dev/loop0p1", FSType: "", PartType: rootGUID, Size: 8 << 30}},
			wantDetected: "no recognizable filesystem",
		},
		{
			name:         "LVM-managed root",
			table:        partitionTableGPT,
			parts:        []partition{{Path: "/dev/loop0p2", FSType: fsTypeLVMMember, Size: 8 << 30}},
			wantDetected: "lvm",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := analyzeLayout(tt.table, tt.parts)
			// assertActionable (layout_test.go) checks the typed error, the
			// "remediation" keyword, and the detected substring in one shot.
			assertActionable(t, err, tt.wantDetected)
			// The unsupported-filesystem case must also state the supported set so the
			// user knows what IS allowed.
			if tt.name == "unsupported filesystem (btrfs)" {
				assertActionableError(t, err, "ext4")
			}
		})
	}
}

// TestNegative_BaselineFileUnusable covers a missing or unreadable local baseline
// image. copyLocalFile is the first place the (intentionally un-stat'd) baseline
// path is opened, so its error must name the offending path and point at the
// config field / permissions rather than surfacing a bare os.PathError.
func TestNegative_BaselineFileUnusable(t *testing.T) {
	t.Run("missing file names the path and config field", func(t *testing.T) {
		missing := "/nonexistent/baseline.raw"
		err := copyLocalFile(missing, t.TempDir()+"/dst.raw")
		assertActionableError(t, err, "not found", missing, "baseline.source.path")
	})

	t.Run("unreadable file reports a permission error with the path", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("running as root: file mode 0000 is still readable, cannot exercise the permission path")
		}
		dir := t.TempDir()
		src := dir + "/noperm.raw"
		if err := os.WriteFile(src, []byte("data"), 0o000); err != nil {
			t.Fatalf("write unreadable file: %v", err)
		}
		err := copyLocalFile(src, dir+"/dst.raw")
		assertActionableError(t, err, "not readable", src)
	})
}

// TestNegative_TargetMismatch covers OS/arch/version mismatches between the
// detected baseline and the template target. Each error must show BOTH the
// detected value and what the target expected.
func TestNegative_TargetMismatch(t *testing.T) {
	base := BaselineInfo{OS: "ubuntu", DistroID: "ubuntu", Version: "24.04", Arch: "x86_64"}

	tests := []struct {
		name           string
		info           BaselineInfo
		target         config.TargetInfo
		wantSubstrings []string
	}{
		{
			name:           "OS mismatch shows detected vs expected",
			info:           base,
			target:         config.TargetInfo{OS: "debian", Dist: "debian13", Arch: "x86_64"},
			wantSubstrings: []string{"OS mismatch", "ubuntu", "debian"},
		},
		{
			name:           "arch mismatch shows detected vs expected",
			info:           base,
			target:         config.TargetInfo{OS: "ubuntu", Dist: "ubuntu24", Arch: "aarch64"},
			wantSubstrings: []string{"architecture mismatch", "x86_64", "aarch64"},
		},
		{
			name:           "distro version mismatch shows detected vs expected",
			info:           base,
			target:         config.TargetInfo{OS: "ubuntu", Dist: "ubuntu22", Arch: "x86_64"},
			wantSubstrings: []string{"version mismatch", "24.04", "ubuntu22"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAgainstTarget(&tt.info, tt.target)
			assertActionableError(t, err, tt.wantSubstrings...)
		})
	}
}

// TestNegative_PreflightPolicyGate covers the solver-driven rejections: removal,
// downgrade, bootloader replacement, and bootable-kernel replacement. Each blocked
// action must surface the offending package name and the violated rule, and the
// top-level Preflight error must be actionable.
func TestNegative_PreflightPolicyGate(t *testing.T) {
	tests := []struct {
		name      string
		baseline  []BaselinePackage
		resolved  []ResolvedPackage
		simulated []PlannedAction
		policy    config.OverlayPolicy
		wantRule  string
		wantInMsg []string // substrings the rendered violation must contain
	}{
		{
			name:      "removal blocked lists the package and policy",
			baseline:  []BaselinePackage{installedDeb("oldpkg", "1.0")},
			simulated: []PlannedAction{{Type: ActionRemove, Package: "oldpkg"}},
			policy:    config.OverlayPolicy{AllowPackageRemoval: false},
			wantRule:  ruleAllowRemoval,
			wantInMsg: []string{"oldpkg", ruleAllowRemoval},
		},
		{
			name:      "downgrade blocked lists the package and policy",
			baseline:  []BaselinePackage{installedDeb("curl", "8.0")},
			resolved:  []ResolvedPackage{{Name: "curl", Version: "7.0", Arch: "amd64"}},
			policy:    config.OverlayPolicy{AllowDowngrade: false},
			wantRule:  ruleAllowDowngrade,
			wantInMsg: []string{"curl", ruleAllowDowngrade},
		},
		{
			name:      "bootloader replacement blocked with explanation",
			baseline:  []BaselinePackage{installedDeb("grub-efi-amd64", "2.06")},
			resolved:  []ResolvedPackage{{Name: "grub-efi-amd64", Version: "2.12", Arch: "amd64"}},
			policy:    config.OverlayPolicy{AllowUpgrade: true, AllowDowngrade: true, AllowPackageRemoval: true},
			wantRule:  ruleBootloaderImmutable,
			wantInMsg: []string{"grub-efi-amd64", "bootloader"},
		},
		{
			name:      "bootable kernel replacement blocked with explanation",
			baseline:  []BaselinePackage{installedDeb("linux-image-6.8.0-40-generic", "6.8.0-40")},
			resolved:  []ResolvedPackage{{Name: "linux-image-6.8.0-40-generic", Version: "6.8.0-41", Arch: "amd64"}},
			policy:    config.OverlayPolicy{AllowUpgrade: true},
			wantRule:  ruleKernelImmutable,
			wantInMsg: []string{"linux-image", "kernel"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := EvaluatePreflight(PreflightInput{
				Family:           PackageManagerAPT,
				Baseline:         tt.baseline,
				Resolved:         tt.resolved,
				SimulatedActions: tt.simulated,
				Policy:           tt.policy,
			})
			if !report.Blocked {
				t.Fatalf("expected the preflight to be blocked, got %+v", report)
			}
			// The violation must cite the exact rule.
			foundRule := false
			for _, v := range report.Violations {
				if v.Rule == tt.wantRule {
					foundRule = true
				}
			}
			if !foundRule {
				t.Errorf("expected a violation with rule %q, got %+v", tt.wantRule, report.Violations)
			}
			// The rendered diagnostic must be actionable (name the package + reason).
			msg := formatViolations(report.Violations)
			assertActionableError(t, errString(msg), tt.wantInMsg...)
		})
	}
}

// errString adapts a plain string into an error so it can flow through
// assertActionableError, which operates on error values.
func errString(s string) error { return &stringError{s} }

type stringError struct{ s string }

func (e *stringError) Error() string { return e.s }

// TestNegative_ResizeShrink covers the grow-only resize guard: a target smaller
// than the current image must be rejected with "shrink not supported", and a grow
// without the allowDiskResize opt-in must name that opt-in.
func TestNegative_ResizeShrink(t *testing.T) {
	current := int64(100 << 20)
	p := writeSizedFile(t, current)

	tests := []struct {
		name           string
		target         string
		allowResize    bool
		wantSubstrings []string
	}{
		{
			name:           "shrink is rejected",
			target:         "50MiB",
			allowResize:    false,
			wantSubstrings: []string{"shrink not supported", "grow-only"},
		},
		{
			name:           "grow without opt-in is rejected",
			target:         "200MiB",
			allowResize:    false,
			wantSubstrings: []string{"allowDiskResize"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := planResize(p, tt.target, tt.allowResize)
			assertActionableError(t, err, tt.wantSubstrings...)
		})
	}
}

// TestNegative_UnsupportedBaselineFormat covers the declared-format rejection in
// the ingestion path (normalizeBaseline re-checks the format for programmatically
// built templates that bypass schema validation). The message must say the format
// is "not supported in this release" and reference the backlog. The config-level
// validator's equivalent message is asserted in config/baseline_test.go.
func TestNegative_UnsupportedBaselineFormat(t *testing.T) {
	defer saveNormalizeSeams().restore()
	tmpl := &config.ImageTemplate{
		Image:  config.ImageInfo{Name: "img", Version: "1.0"},
		Target: config.TargetInfo{OS: "ubuntu", Dist: "ubuntu24", Arch: "amd64"},
		Baseline: &config.Baseline{
			Mode:   config.BaselineModeOverlay,
			Source: &config.BaselineSource{Path: "/tmp/u.raw", Format: "vmdk"},
		},
	}
	ing := &Ingestor{template: tmpl, workDir: t.TempDir()}
	err := ing.normalizeBaseline(&Context{BaselineCopyPath: t.TempDir() + "/baseline.raw"})
	assertActionableError(t, err, "not supported in this release", "backlog")
}
