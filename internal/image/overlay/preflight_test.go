package overlay

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

// installedDeb is a small helper for building a Slice A baseline entry.
func installedDeb(name, version string) BaselinePackage {
	return BaselinePackage{Name: name, Version: version, Arch: "amd64", Installed: true}
}

// TestEvaluatePreflight_PolicyPaths is the table-driven core: each row drives one
// classified action through the policy gate and asserts whether it is blocked,
// the action class, and the rule cited on a block.
func TestEvaluatePreflight_PolicyPaths(t *testing.T) {
	tests := []struct {
		name       string
		family     PackageManager
		baseline   []BaselinePackage
		resolved   []ResolvedPackage
		simulated  []PlannedAction
		policy     config.OverlayPolicy
		wantAction ActionType
		wantBlock  bool
		wantRule   string
	}{
		{
			name:       "pure add is always allowed",
			baseline:   []BaselinePackage{installedDeb("libc6", "2.34")},
			resolved:   []ResolvedPackage{{Name: "curl", Version: "8.0", Arch: "amd64"}},
			wantAction: ActionAdd,
			wantBlock:  false,
		},
		{
			name:       "upgrade blocked when allowUpgrade is false (additive-only default)",
			baseline:   []BaselinePackage{installedDeb("curl", "7.0")},
			resolved:   []ResolvedPackage{{Name: "curl", Version: "8.0", Arch: "amd64"}},
			wantAction: ActionUpgrade,
			wantBlock:  true,
			wantRule:   ruleAllowUpgrade,
		},
		{
			name:       "upgrade allowed when allowUpgrade is true",
			baseline:   []BaselinePackage{installedDeb("curl", "7.0")},
			resolved:   []ResolvedPackage{{Name: "curl", Version: "8.0", Arch: "amd64"}},
			policy:     config.OverlayPolicy{AllowUpgrade: true},
			wantAction: ActionUpgrade,
			wantBlock:  false,
		},
		{
			name:       "same version yields no action",
			baseline:   []BaselinePackage{installedDeb("curl", "8.0")},
			resolved:   []ResolvedPackage{{Name: "curl", Version: "8.0", Arch: "amd64"}},
			wantAction: "", // no action emitted
			wantBlock:  false,
		},
		{
			name:       "downgrade blocked when allowDowngrade is false",
			baseline:   []BaselinePackage{installedDeb("curl", "8.0")},
			resolved:   []ResolvedPackage{{Name: "curl", Version: "7.0", Arch: "amd64"}},
			policy:     config.OverlayPolicy{AllowDowngrade: false},
			wantAction: ActionDowngrade,
			wantBlock:  true,
			wantRule:   ruleAllowDowngrade,
		},
		{
			name:       "downgrade allowed when allowDowngrade is true",
			baseline:   []BaselinePackage{installedDeb("curl", "8.0")},
			resolved:   []ResolvedPackage{{Name: "curl", Version: "7.0", Arch: "amd64"}},
			policy:     config.OverlayPolicy{AllowDowngrade: true},
			wantAction: ActionDowngrade,
			wantBlock:  false,
		},
		{
			name:       "removal blocked when allowRemoval is false",
			baseline:   []BaselinePackage{installedDeb("oldpkg", "1.0")},
			simulated:  []PlannedAction{{Type: ActionRemove, Package: "oldpkg"}},
			policy:     config.OverlayPolicy{AllowRemoval: false},
			wantAction: ActionRemove,
			wantBlock:  true,
			wantRule:   ruleAllowRemoval,
		},
		{
			name:       "removal allowed when allowRemoval is true",
			baseline:   []BaselinePackage{installedDeb("oldpkg", "1.0")},
			simulated:  []PlannedAction{{Type: ActionRemove, Package: "oldpkg"}},
			policy:     config.OverlayPolicy{AllowRemoval: true},
			wantAction: ActionRemove,
			wantBlock:  false,
		},
		{
			name:       "conflict blocked under fail policy",
			simulated:  []PlannedAction{{Type: ActionConflict, Package: "foo", ConflictWith: "bar"}},
			policy:     config.OverlayPolicy{ConflictPolicy: config.OverlayConflictPolicyFail},
			wantAction: ActionConflict,
			wantBlock:  true,
			wantRule:   ruleConflictPolicyFail,
		},
		{
			name:       "conflict blocked under defaulted (empty) policy",
			simulated:  []PlannedAction{{Type: ActionConflict, Package: "foo"}},
			policy:     config.OverlayPolicy{}, // empty conflictPolicy defaults to fail
			wantAction: ActionConflict,
			wantBlock:  true,
			wantRule:   ruleConflictPolicyFail,
		},
		{
			name:       "conflict allowed under allow-explicit policy",
			simulated:  []PlannedAction{{Type: ActionConflict, Package: "foo"}},
			policy:     config.OverlayPolicy{ConflictPolicy: config.OverlayConflictPolicyAllowExplicit},
			wantAction: ActionConflict,
			wantBlock:  false,
		},
		{
			name:       "simulate-sourced conflict allowed under explicit policy with versions reported",
			baseline:   []BaselinePackage{installedDeb("foo", "1.0")},
			simulated:  []PlannedAction{{Type: ActionConflict, Package: "foo", RequestedVersion: "2.0", ConflictWith: "bar"}},
			policy:     config.OverlayPolicy{ConflictPolicy: config.OverlayConflictPolicyAllowExplicit},
			wantAction: ActionConflict,
			wantBlock:  false,
		},
		{
			name:       "bootloader upgrade blocked even when versions bump cleanly",
			baseline:   []BaselinePackage{installedDeb("grub-efi-amd64", "2.06")},
			resolved:   []ResolvedPackage{{Name: "grub-efi-amd64", Version: "2.12", Arch: "amd64"}},
			policy:     config.OverlayPolicy{AllowDowngrade: true, AllowRemoval: true},
			wantAction: ActionUpgrade,
			wantBlock:  true,
			wantRule:   ruleBootloaderImmutable,
		},
		{
			name:       "bootloader removal blocked even when allowRemoval is true",
			baseline:   []BaselinePackage{installedDeb("shim-signed", "1.0")},
			simulated:  []PlannedAction{{Type: ActionRemove, Package: "shim-signed"}},
			policy:     config.OverlayPolicy{AllowRemoval: true},
			wantAction: ActionRemove,
			wantBlock:  true,
			wantRule:   ruleBootloaderImmutable,
		},
		{
			name:       "bootloader add is allowed (additive, does not replace)",
			resolved:   []ResolvedPackage{{Name: "grub-common", Version: "2.12", Arch: "amd64"}},
			policy:     config.OverlayPolicy{},
			wantAction: ActionAdd,
			wantBlock:  false,
		},
		{
			// Purpose of this case is the rpm version comparator (2.36-1 < 2.38-1),
			// so allow upgrades to isolate classification from the additive-only gate.
			name:       "rpm upgrade classified with rpm comparator",
			family:     PackageManagerDNF,
			baseline:   []BaselinePackage{{Name: "glibc", Version: "2.36-1", Arch: "x86_64", Installed: true}},
			resolved:   []ResolvedPackage{{Name: "glibc", Version: "2.38-1", Arch: "x86_64"}},
			policy:     config.OverlayPolicy{AllowUpgrade: true},
			wantAction: ActionUpgrade,
			wantBlock:  false,
		},
		{
			name:       "rpm downgrade classified and blocked",
			family:     PackageManagerDNF,
			baseline:   []BaselinePackage{{Name: "glibc", Version: "2.38-1", Arch: "x86_64", Installed: true}},
			resolved:   []ResolvedPackage{{Name: "glibc", Version: "2.36-1", Arch: "x86_64"}},
			policy:     config.OverlayPolicy{},
			wantAction: ActionDowngrade,
			wantBlock:  true,
			wantRule:   ruleAllowDowngrade,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			family := tt.family
			if family == "" {
				family = PackageManagerAPT
			}
			report := EvaluatePreflight(PreflightInput{
				Family:           family,
				Baseline:         tt.baseline,
				Resolved:         tt.resolved,
				SimulatedActions: tt.simulated,
				Policy:           tt.policy,
			})

			if report.Blocked != tt.wantBlock {
				t.Fatalf("Blocked = %v, want %v (actions=%+v violations=%+v)",
					report.Blocked, tt.wantBlock, report.Actions, report.Violations)
			}

			if tt.wantAction == "" {
				if len(report.Actions) != 0 {
					t.Fatalf("expected no action, got %+v", report.Actions)
				}
				return
			}

			if len(report.Actions) != 1 {
				t.Fatalf("expected exactly one action, got %+v", report.Actions)
			}
			if report.Actions[0].Type != tt.wantAction {
				t.Errorf("action type = %s, want %s", report.Actions[0].Type, tt.wantAction)
			}

			if tt.wantBlock {
				if len(report.Violations) != 1 {
					t.Fatalf("expected one violation, got %+v", report.Violations)
				}
				if report.Violations[0].Rule != tt.wantRule {
					t.Errorf("rule = %s, want %s", report.Violations[0].Rule, tt.wantRule)
				}
			} else if len(report.Violations) != 0 {
				t.Errorf("expected no violations, got %+v", report.Violations)
			}
		})
	}
}

// TestEvaluatePreflight_Counts confirms the per-class counters add up across a
// mixed plan and that ordering is deterministic.
func TestEvaluatePreflight_Counts(t *testing.T) {
	report := EvaluatePreflight(PreflightInput{
		Family: PackageManagerAPT,
		Baseline: []BaselinePackage{
			installedDeb("curl", "7.0"),   // -> upgrade
			installedDeb("wget", "2.0"),   // -> downgrade
			installedDeb("zlib", "1.0"),   // unchanged in resolved -> no action
			installedDeb("oldpkg", "1.0"), // -> remove (via simulate)
		},
		Resolved: []ResolvedPackage{
			{Name: "curl", Version: "8.0", Arch: "amd64"},
			{Name: "wget", Version: "1.0", Arch: "amd64"},
			{Name: "zlib", Version: "1.0", Arch: "amd64"},
			{Name: "vim", Version: "9.0", Arch: "amd64"}, // -> add
		},
		SimulatedActions: []PlannedAction{
			{Type: ActionRemove, Package: "oldpkg"},
			{Type: ActionConflict, Package: "foo", ConflictWith: "bar"},
		},
		Policy: config.OverlayPolicy{AllowRemoval: true, AllowDowngrade: true, AllowUpgrade: true, ConflictPolicy: config.OverlayConflictPolicyAllowExplicit},
	})

	if report.Adds != 1 || report.Upgrades != 1 || report.Downgrades != 1 || report.Removes != 1 || report.Conflicts != 1 {
		t.Errorf("counts add=%d up=%d down=%d rm=%d conflict=%d, want 1 each",
			report.Adds, report.Upgrades, report.Downgrades, report.Removes, report.Conflicts)
	}
	if report.Blocked {
		t.Errorf("expected not blocked under permissive policy, violations=%+v", report.Violations)
	}

	// Actions are sorted by type, then package: add < conflict < downgrade < remove < upgrade.
	wantOrder := []struct {
		typ ActionType
		pkg string
	}{
		{ActionAdd, "vim"},
		{ActionConflict, "foo"},
		{ActionDowngrade, "wget"},
		{ActionRemove, "oldpkg"},
		{ActionUpgrade, "curl"},
	}
	if len(report.Actions) != len(wantOrder) {
		t.Fatalf("got %d actions, want %d: %+v", len(report.Actions), len(wantOrder), report.Actions)
	}
	for i, w := range wantOrder {
		if report.Actions[i].Type != w.typ || report.Actions[i].Package != w.pkg {
			t.Errorf("action[%d] = %s/%s, want %s/%s", i, report.Actions[i].Type, report.Actions[i].Package, w.typ, w.pkg)
		}
	}
}

// TestEvaluatePreflight_Deterministic confirms reordered inputs produce an
// identical report.
func TestEvaluatePreflight_Deterministic(t *testing.T) {
	baseline := []BaselinePackage{installedDeb("a", "1.0"), installedDeb("b", "1.0")}
	run := func(resolved []ResolvedPackage) *PreflightReport {
		return EvaluatePreflight(PreflightInput{
			Family:   PackageManagerAPT,
			Baseline: baseline,
			Resolved: resolved,
			Policy:   config.OverlayPolicy{},
		})
	}
	a := run([]ResolvedPackage{{Name: "a", Version: "2.0"}, {Name: "c", Version: "1.0"}})
	b := run([]ResolvedPackage{{Name: "c", Version: "1.0"}, {Name: "a", Version: "2.0"}})
	if !reflect.DeepEqual(a, b) {
		t.Errorf("reports differ for reordered inputs:\n a=%+v\n b=%+v", a, b)
	}
}

// TestPreflight_BlockedErrorIsActionable confirms the orchestrator returns the
// report plus an error naming the offending package, both versions, and the rule.
func TestPreflight_BlockedErrorIsActionable(t *testing.T) {
	info := &BaselineInfo{OS: "ubuntu", Arch: "amd64", PackageManager: PackageManagerAPT}
	baseline := []BaselinePackage{installedDeb("curl", "8.0")}
	plan := &ResolutionPlan{
		// A downgrade reaching ToInstall (the set actually installed) must block.
		ToInstall: []ResolvedPackage{{Name: "curl", Version: "7.0", Arch: "amd64"}},
	}

	report, err := Preflight(info, baseline, plan, &config.OverlayPolicy{})
	if err == nil {
		t.Fatal("expected preflight to be blocked")
	}
	if report == nil || !report.Blocked {
		t.Fatalf("expected a blocked report, got %+v", report)
	}
	for _, want := range []string{"curl", "8.0", "7.0", ruleAllowDowngrade, "downgrade"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

// TestPreflight_AllowedReturnsNoError confirms a clean additive plan passes.
func TestPreflight_AllowedReturnsNoError(t *testing.T) {
	info := &BaselineInfo{OS: "ubuntu", Arch: "amd64", PackageManager: PackageManagerAPT}
	baseline := []BaselinePackage{installedDeb("libc6", "2.34")}
	plan := &ResolutionPlan{
		// libc6 is in the closure but already present, so it is NOT in ToInstall;
		// only the genuinely-added curl reaches the preflight gate.
		ToInstall: []ResolvedPackage{
			{Name: "curl", Version: "8.0", Arch: "amd64"},
		},
	}
	report, err := Preflight(info, baseline, plan, &config.OverlayPolicy{})
	if err != nil {
		t.Fatalf("unexpected preflight error: %v", err)
	}
	if report.Blocked || report.Adds != 1 {
		t.Errorf("expected one clean add, got %+v", report)
	}
}

// TestPreflight_SimulateAidContributesActions confirms the simulate seam feeds
// remove/conflict actions into the policy gate, and that a simulate failure is
// non-fatal (two-slice model still drives the decision).
func TestPreflight_SimulateAidContributesActions(t *testing.T) {
	info := &BaselineInfo{OS: "ubuntu", Arch: "amd64", PackageManager: PackageManagerAPT}
	baseline := []BaselinePackage{installedDeb("oldpkg", "1.0")}
	plan := &ResolutionPlan{ToInstall: []ResolvedPackage{{Name: "curl", Version: "8.0", Arch: "amd64"}}}

	orig := simulateOverlayInstall
	defer func() { simulateOverlayInstall = orig }()

	t.Run("simulate-reported removal is gated", func(t *testing.T) {
		simulateOverlayInstall = func(*BaselineInfo, *ResolutionPlan) ([]PlannedAction, error) {
			return []PlannedAction{{Type: ActionRemove, Package: "oldpkg"}}, nil
		}
		report, err := Preflight(info, baseline, plan, &config.OverlayPolicy{})
		if err == nil || !report.Blocked {
			t.Fatalf("expected removal to block, err=%v report=%+v", err, report)
		}
		if report.Violations[0].Rule != ruleAllowRemoval {
			t.Errorf("rule = %s, want %s", report.Violations[0].Rule, ruleAllowRemoval)
		}
		// The remove action carries the baseline version backfilled from Slice A.
		if report.Violations[0].Action.CurrentVersion != "1.0" {
			t.Errorf("current version = %q, want 1.0 (backfilled)", report.Violations[0].Action.CurrentVersion)
		}
	})

	t.Run("simulate failure is non-fatal", func(t *testing.T) {
		simulateOverlayInstall = func(*BaselineInfo, *ResolutionPlan) ([]PlannedAction, error) {
			return nil, errors.New("simulate unavailable")
		}
		report, err := Preflight(info, baseline, plan, &config.OverlayPolicy{})
		if err != nil {
			t.Fatalf("simulate failure must not fail preflight: %v", err)
		}
		if report.Blocked || report.Adds != 1 {
			t.Errorf("expected clean add via two-slice model, got %+v", report)
		}
	})
}

// TestEvaluatePreflight_SimulatedEmptyPackageDropped guards that simulator
// output with a blank or whitespace-padded package name cannot slip into the
// report: empty names are dropped, and a padded name is trimmed so baseline
// backfill still resolves.
func TestEvaluatePreflight_SimulatedEmptyPackageDropped(t *testing.T) {
	report := EvaluatePreflight(PreflightInput{
		Family:   PackageManagerAPT,
		Baseline: []BaselinePackage{installedDeb("oldpkg", "1.0")},
		SimulatedActions: []PlannedAction{
			{Type: ActionRemove, Package: ""},           // blank -> dropped
			{Type: ActionRemove, Package: "   "},        // whitespace-only -> dropped
			{Type: ActionRemove, Package: "  oldpkg  "}, // trimmed -> backfilled
		},
		Policy: config.OverlayPolicy{AllowRemoval: true},
	})

	if report.Removes != 1 {
		t.Fatalf("removes = %d, want 1 (empty names dropped); actions=%+v", report.Removes, report.Actions)
	}
	if got := report.Actions[0].Package; got != "oldpkg" {
		t.Errorf("package = %q, want %q (trimmed)", got, "oldpkg")
	}
	if got := report.Actions[0].CurrentVersion; got != "1.0" {
		t.Errorf("current version = %q, want 1.0 (backfilled after trim)", got)
	}
}

func TestIsBootloaderPackage(t *testing.T) {
	bootloader := []string{"grub", "grub2", "grub-efi-amd64", "grub2-efi-x64-modules", "shim", "shim-signed", "systemd-boot", "efibootmgr", "GRUB2"}
	for _, n := range bootloader {
		if !isBootloaderPackage(n) {
			t.Errorf("isBootloaderPackage(%q) = false, want true", n)
		}
	}
	// Packages that merely share a prefix's letters must NOT be flagged:
	// systemd-bootchart is a boot profiler, grubbish/shimmer are not bootloaders.
	for _, n := range []string{"curl", "libc6", "vim", "", "graphite2", "systemd-bootchart", "shimmer"} {
		if isBootloaderPackage(n) {
			t.Errorf("isBootloaderPackage(%q) = true, want false", n)
		}
	}
	// grubby (rpm GRUB tool) is explicitly listed and must be caught.
	if !isBootloaderPackage("grubby") {
		t.Error("isBootloaderPackage(grubby) = false, want true")
	}
}

// TestEvaluatePreflight_BootChartNotBlocked guards the systemd-bootchart false
// positive: a clean upgrade of a non-bootloader package that shares a bootloader
// prefix must pass.
func TestEvaluatePreflight_BootChartNotBlocked(t *testing.T) {
	// This case isolates the bootloader-prefix false positive, so allow upgrades:
	// the assertion is that systemd-bootchart is not misclassified as a bootloader
	// package, not that upgrades are permitted by default (they are not).
	report := EvaluatePreflight(PreflightInput{
		Family:   PackageManagerAPT,
		Baseline: []BaselinePackage{installedDeb("systemd-bootchart", "233")},
		Resolved: []ResolvedPackage{{Name: "systemd-bootchart", Version: "234", Arch: "amd64"}},
		Policy:   config.OverlayPolicy{AllowUpgrade: true},
	})
	if report.Blocked {
		t.Errorf("systemd-bootchart upgrade wrongly blocked: %+v", report.Violations)
	}
	if report.Upgrades != 1 {
		t.Errorf("expected one upgrade, got %+v", report.Actions)
	}
}

func TestIsKernelImagePackage(t *testing.T) {
	// Bootable kernel images that must be treated as immutable.
	kernels := []string{
		"linux-image-generic", "linux-image-6.8.0-40-generic", "linux-image-unsigned",
		"kernel", "kernel-5.14.0-427", "kernel-core", "kernel-image",
	}
	for _, n := range kernels {
		if !isKernelImagePackage(n) {
			t.Errorf("isKernelImagePackage(%q) = false, want true", n)
		}
	}
	// Userspace kernel-adjacent packages that carry no boot entry and MUST stay
	// upgradable — including the two the WW28.1 template upgrades.
	for _, n := range []string{
		"linux-libc-dev", "linux-tools-common", "linux-tools-6.8.0-40",
		"kernel-headers", "kernel-devel", "kernel-tools", "kernelshark",
		"curl", "vim", "",
	} {
		if isKernelImagePackage(n) {
			t.Errorf("isKernelImagePackage(%q) = true, want false", n)
		}
	}
}

// TestEvaluatePreflight_KernelUpgradeBlocked confirms an in-place kernel-image
// upgrade is blocked even under an allowUpgrade policy (boot regeneration cannot
// rewrite the bootloader menu for a changed kernel), while a brand-new kernel
// installed alongside the existing one is permitted.
func TestEvaluatePreflight_KernelUpgradeBlocked(t *testing.T) {
	report := EvaluatePreflight(PreflightInput{
		Family:   PackageManagerAPT,
		Baseline: []BaselinePackage{installedDeb("linux-image-generic", "6.8.0-40")},
		Resolved: []ResolvedPackage{{Name: "linux-image-generic", Version: "6.8.0-50", Arch: "amd64"}},
		Policy:   config.OverlayPolicy{AllowUpgrade: true},
	})
	if !report.Blocked {
		t.Fatalf("kernel upgrade should be blocked even with AllowUpgrade, got %+v", report.Actions)
	}
	if len(report.Violations) != 1 || report.Violations[0].Rule != ruleKernelImmutable {
		t.Errorf("expected kernel-immutable violation, got %+v", report.Violations)
	}

	// A new kernel added alongside (absent from the baseline) is an add, allowed.
	addReport := EvaluatePreflight(PreflightInput{
		Family:   PackageManagerAPT,
		Baseline: []BaselinePackage{installedDeb("linux-image-6.8.0-40-generic", "6.8.0-40")},
		Resolved: []ResolvedPackage{{Name: "linux-image-6.8.0-50-generic", Version: "6.8.0-50", Arch: "amd64"}},
		Policy:   config.OverlayPolicy{AllowUpgrade: true},
	})
	if addReport.Blocked {
		t.Errorf("adding a new kernel alongside should be allowed, got %+v", addReport.Violations)
	}
}

// TestEvaluatePreflight_KernelAdjacentUpgradeAllowed confirms the userspace
// kernel packages the WW28.1 overlay upgrades (linux-libc-dev, linux-tools-common)
// are NOT caught by the kernel-immutable gate.
func TestEvaluatePreflight_KernelAdjacentUpgradeAllowed(t *testing.T) {
	report := EvaluatePreflight(PreflightInput{
		Family: PackageManagerAPT,
		Baseline: []BaselinePackage{
			installedDeb("linux-libc-dev", "6.8.0-124.124"),
			installedDeb("linux-tools-common", "6.8.0-124.124"),
		},
		Resolved: []ResolvedPackage{
			{Name: "linux-libc-dev", Version: "6.8.0-134.134", Arch: "amd64"},
			{Name: "linux-tools-common", Version: "6.8.0-134.134", Arch: "amd64"},
		},
		Policy: config.OverlayPolicy{AllowUpgrade: true},
	})
	if report.Blocked {
		t.Errorf("kernel-adjacent userspace upgrades wrongly blocked: %+v", report.Violations)
	}
	if report.Upgrades != 2 {
		t.Errorf("expected 2 upgrades, got %+v", report.Actions)
	}
}

// TestEvaluatePreflight_ObsoletesRemovalGated confirms an rpm Obsoletes on a
// present baseline package is classified as a removal and blocked by the default
// AllowRemoval=false, closing the rpm -U silent-removal gap.
func TestEvaluatePreflight_ObsoletesRemovalGated(t *testing.T) {
	base := BaselinePackage{Name: "oldlib", Version: "1.0", Arch: "x86_64", Installed: true}
	in := PreflightInput{
		Family:      PackageManagerDNF,
		Baseline:    []BaselinePackage{base},
		Resolved:    []ResolvedPackage{{Name: "newlib", Version: "2.0", Arch: "x86_64"}},
		Obsoletions: []ArtifactObsoletion{{Package: "newlib", Obsoletes: DependencyAlternative{Name: "oldlib"}}},
		Policy:      config.OverlayPolicy{}, // AllowRemoval defaults false
	}
	report := EvaluatePreflight(in)
	if !report.Blocked || report.Removes != 1 {
		t.Fatalf("expected a blocked removal from the obsoletion, got removes=%d blocked=%v actions=%+v",
			report.Removes, report.Blocked, report.Actions)
	}
	if report.Violations[0].Rule != ruleAllowRemoval {
		t.Errorf("expected allowRemoval violation, got %+v", report.Violations)
	}

	// Same obsoletion is permitted once removal is explicitly allowed.
	in.Policy = config.OverlayPolicy{AllowRemoval: true}
	if r := EvaluatePreflight(in); r.Blocked {
		t.Errorf("obsoletion removal should pass with AllowRemoval=true, got %+v", r.Violations)
	}

	// An Obsoletes targeting a package absent from the baseline is a no-op.
	in.Policy = config.OverlayPolicy{}
	in.Obsoletions = []ArtifactObsoletion{{Package: "newlib", Obsoletes: DependencyAlternative{Name: "not-installed"}}}
	if r := EvaluatePreflight(in); r.Blocked || r.Removes != 0 {
		t.Errorf("obsoletion of an absent package must be a no-op, got %+v", r.Actions)
	}
}

// TestEvaluatePreflight_VersionedObsoletesOutOfRange confirms a versioned
// Obsoletes whose constraint the baseline version does NOT fall within is not
// treated as a removal.
func TestEvaluatePreflight_VersionedObsoletesOutOfRange(t *testing.T) {
	base := BaselinePackage{Name: "oldlib", Version: "3.0", Arch: "x86_64", Installed: true}
	// Obsoletes oldlib < 2.0; baseline has 3.0, which is outside the range.
	report := EvaluatePreflight(PreflightInput{
		Family:   PackageManagerDNF,
		Baseline: []BaselinePackage{base},
		Resolved: []ResolvedPackage{{Name: "newlib", Version: "2.0", Arch: "x86_64"}},
		Obsoletions: []ArtifactObsoletion{{
			Package:   "newlib",
			Obsoletes: DependencyAlternative{Name: "oldlib", Constraint: &VersionConstraint{Op: "<", Ver: "2.0"}},
		}},
		Policy: config.OverlayPolicy{},
	})
	if report.Blocked || report.Removes != 0 {
		t.Errorf("versioned obsoletion out of range must not remove, got %+v", report.Actions)
	}
}

func TestPreflight_NilGuards(t *testing.T) {
	if _, err := Preflight(nil, nil, &ResolutionPlan{}, nil); err == nil {
		t.Error("expected error for nil info")
	}
	if _, err := Preflight(&BaselineInfo{PackageManager: PackageManagerAPT}, nil, nil, nil); err == nil {
		t.Error("expected error for nil plan")
	}
}
