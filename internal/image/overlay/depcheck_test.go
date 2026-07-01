package overlay

import (
	"reflect"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

func TestParseConstraint(t *testing.T) {
	tests := []struct {
		in     string
		wantOp string
		wantV  string
		wantOK bool
	}{
		{"= 1.2-3", "=", "1.2-3", true},
		{">= 2.34", ">=", "2.34", true},
		{"<=1.0", "<=", "1.0", true}, // no space
		{">> 9", ">>", "9", true},    // deb strictly-greater
		{"<< 9", "<<", "9", true},    // deb strictly-less
		{">1.0", ">", "1.0", true},   // rpm single-char
		{"", "", "", false},          // empty
		{"1.2.3", "", "", false},     // no operator
		{">=", "", "", false},        // operator but no version
	}
	for _, tt := range tests {
		got, ok := parseConstraint(tt.in)
		if ok != tt.wantOK {
			t.Errorf("parseConstraint(%q) ok = %v, want %v", tt.in, ok, tt.wantOK)
			continue
		}
		if ok && (got.Op != tt.wantOp || got.Ver != tt.wantV) {
			t.Errorf("parseConstraint(%q) = %s/%s, want %s/%s", tt.in, got.Op, got.Ver, tt.wantOp, tt.wantV)
		}
	}
}

func TestParseDebAlternative(t *testing.T) {
	tests := []struct {
		in       string
		wantName string
		wantCon  *VersionConstraint
		wantOK   bool
	}{
		{"libsystemd-shared (= 255.4-1ubuntu8.16)", "libsystemd-shared", &VersionConstraint{"=", "255.4-1ubuntu8.16"}, true},
		{"libc6 (>= 2.34)", "libc6", &VersionConstraint{">=", "2.34"}, true},
		{"libc6:amd64 (>= 2.34)", "libc6", &VersionConstraint{">=", "2.34"}, true}, // multiarch qualifier stripped
		{"perl:any", "perl", nil, true},      // arch qualifier, no version
		{"curl", "curl", nil, true},          // bare name
		{"foo <!nocheck>", "foo", nil, true}, // build profile ignored
		{"", "", nil, false},                 // empty
	}
	for _, tt := range tests {
		got, ok := parseDebAlternative(tt.in)
		if ok != tt.wantOK {
			t.Errorf("parseDebAlternative(%q) ok = %v, want %v", tt.in, ok, tt.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if got.Name != tt.wantName {
			t.Errorf("parseDebAlternative(%q) name = %q, want %q", tt.in, got.Name, tt.wantName)
		}
		if !reflect.DeepEqual(got.Constraint, tt.wantCon) {
			t.Errorf("parseDebAlternative(%q) constraint = %+v, want %+v", tt.in, got.Constraint, tt.wantCon)
		}
	}
}

func TestParseDebDependsField(t *testing.T) {
	// A realistic Depends line: a versioned pin, an alternative term, and a bare dep.
	field := "libsystemd-shared (= 255.4-1ubuntu8.16), libc6 (>= 2.34) | libc6-udeb, tree"
	edges := parseDebDependsField("systemd-boot", field)
	if len(edges) != 3 {
		t.Fatalf("got %d edges, want 3: %+v", len(edges), edges)
	}
	// First edge: single versioned alternative.
	if edges[0].Package != "systemd-boot" || len(edges[0].Alternatives) != 1 ||
		edges[0].Alternatives[0].Name != "libsystemd-shared" || edges[0].Alternatives[0].Constraint == nil {
		t.Errorf("edge[0] = %+v, want systemd-boot -> libsystemd-shared (=...)", edges[0])
	}
	// Second edge: two alternatives (libc6 versioned | libc6-udeb bare).
	if len(edges[1].Alternatives) != 2 {
		t.Errorf("edge[1] alternatives = %+v, want 2", edges[1].Alternatives)
	}
	// Third edge: bare dep, no constraint.
	if edges[2].Alternatives[0].Name != "tree" || edges[2].Alternatives[0].Constraint != nil {
		t.Errorf("edge[2] = %+v, want bare tree", edges[2])
	}
}

func TestParseRPMRequires(t *testing.T) {
	out := "glibc = 2.38-1\n/bin/sh\nrpmlib(FileDigests) <= 4.6.0-1\nlibfoo.so.1()(64bit)\nbar >= 1.0\n"
	edges := parseRPMRequires("mypkg", out)
	// Only "glibc = 2.38-1" and "bar >= 1.0" are versioned name deps; the file dep,
	// rpmlib feature, and bare soname capability are skipped.
	if len(edges) != 2 {
		t.Fatalf("got %d edges, want 2: %+v", len(edges), edges)
	}
	if edges[0].Alternatives[0].Name != "glibc" || edges[0].Alternatives[0].Constraint.Op != "=" {
		t.Errorf("edge[0] = %+v, want glibc =", edges[0])
	}
	if edges[1].Alternatives[0].Name != "bar" || edges[1].Alternatives[0].Constraint.Op != ">=" {
		t.Errorf("edge[1] = %+v, want bar >=", edges[1])
	}
}

// TestEvaluatePreflight_UnsatisfiedVersionPin is the core regression for the
// systemd-boot failure: a to-install package pins an exact version of a baseline
// package that is present at a different version. Additive-only install cannot
// upgrade it, so the preflight must block with the unsatisfiable-dep rule.
func TestEvaluatePreflight_UnsatisfiedVersionPin(t *testing.T) {
	report := EvaluatePreflight(PreflightInput{
		Family:   PackageManagerAPT,
		Baseline: []BaselinePackage{installedDeb("libsystemd-shared", "255.4-1ubuntu8.12")},
		Resolved: []ResolvedPackage{{Name: "systemd-boot", Version: "255.4-1ubuntu8.16", Arch: "amd64"}},
		ArtifactDeps: []ArtifactDependency{{
			Package:      "systemd-boot",
			Alternatives: []DependencyAlternative{{Name: "libsystemd-shared", Constraint: &VersionConstraint{"=", "255.4-1ubuntu8.16"}}},
		}},
		Policy: config.OverlayPolicy{},
	})
	if !report.Blocked || report.UnsatisfiedDeps != 1 {
		t.Fatalf("expected one unsatisfiable-dep block, got %+v (violations=%+v)", report, report.Violations)
	}
	if report.Violations[0].Rule != ruleUnsatisfiedDep {
		t.Errorf("rule = %s, want %s", report.Violations[0].Rule, ruleUnsatisfiedDep)
	}
	if report.Violations[0].Action.ConflictWith != "libsystemd-shared" {
		t.Errorf("offending dep = %q, want libsystemd-shared", report.Violations[0].Action.ConflictWith)
	}
}

// TestEvaluatePreflight_VersionPinSatisfiedByBaseline confirms no false positive
// when the baseline version already satisfies the pin.
func TestEvaluatePreflight_VersionPinSatisfiedByBaseline(t *testing.T) {
	report := EvaluatePreflight(PreflightInput{
		Family:   PackageManagerAPT,
		Baseline: []BaselinePackage{installedDeb("libsystemd-shared", "255.4-1ubuntu8.16")},
		Resolved: []ResolvedPackage{{Name: "systemd-boot", Version: "255.4-1ubuntu8.16", Arch: "amd64"}},
		ArtifactDeps: []ArtifactDependency{{
			Package:      "systemd-boot",
			Alternatives: []DependencyAlternative{{Name: "libsystemd-shared", Constraint: &VersionConstraint{"=", "255.4-1ubuntu8.16"}}},
		}},
		Policy: config.OverlayPolicy{},
	})
	if report.Blocked || report.UnsatisfiedDeps != 0 {
		t.Errorf("expected no block when the pin is satisfied, got %+v", report)
	}
}

// TestEvaluatePreflight_VersionPinSatisfiedByCoInstall confirms a pin met by
// another to-install package (not the baseline) is not flagged.
func TestEvaluatePreflight_VersionPinSatisfiedByCoInstall(t *testing.T) {
	// The co-installed libsystemd-shared bumps an existing baseline version, which is
	// an upgrade; this test targets the version-pin/co-install logic, so allow
	// upgrades to isolate it from the additive-only upgrade gate.
	report := EvaluatePreflight(PreflightInput{
		Family:   PackageManagerAPT,
		Baseline: []BaselinePackage{installedDeb("libsystemd-shared", "255.4-1ubuntu8.12")},
		Resolved: []ResolvedPackage{
			{Name: "systemd-boot", Version: "255.4-1ubuntu8.16", Arch: "amd64"},
			// The matching libsystemd-shared is co-installed in the same plan.
			{Name: "libsystemd-shared", Version: "255.4-1ubuntu8.16", Arch: "amd64"},
		},
		ArtifactDeps: []ArtifactDependency{{
			Package:      "systemd-boot",
			Alternatives: []DependencyAlternative{{Name: "libsystemd-shared", Constraint: &VersionConstraint{"=", "255.4-1ubuntu8.16"}}},
		}},
		Policy: config.OverlayPolicy{AllowUpgrade: true},
	})
	if report.UnsatisfiedDeps != 0 {
		t.Errorf("expected the pin to be met by a co-installed package (no unsatisfied deps), got %+v", report)
	}
}

// TestEvaluatePreflight_AbsentDepNotFlagged confirms a versioned pin on a package
// that is entirely absent is NOT flagged (it may be satisfied via a Provides the
// artifact metadata does not expose here), avoiding false positives.
func TestEvaluatePreflight_AbsentDepNotFlagged(t *testing.T) {
	report := EvaluatePreflight(PreflightInput{
		Family:   PackageManagerAPT,
		Baseline: []BaselinePackage{installedDeb("libc6", "2.34")},
		Resolved: []ResolvedPackage{{Name: "somepkg", Version: "1.0", Arch: "amd64"}},
		ArtifactDeps: []ArtifactDependency{{
			Package:      "somepkg",
			Alternatives: []DependencyAlternative{{Name: "virtual-thing", Constraint: &VersionConstraint{"=", "9.9"}}},
		}},
		Policy: config.OverlayPolicy{},
	})
	if report.Blocked || report.UnsatisfiedDeps != 0 {
		t.Errorf("expected no block for an absent (possibly virtual) dependency, got %+v", report)
	}
}

// TestEvaluatePreflight_UnsatisfiedDepAlternativeRescues confirms an edge with a
// failing versioned alternative is NOT flagged when another alternative holds.
func TestEvaluatePreflight_UnsatisfiedDepAlternativeRescues(t *testing.T) {
	report := EvaluatePreflight(PreflightInput{
		Family:   PackageManagerAPT,
		Baseline: []BaselinePackage{installedDeb("libold", "1.0"), installedDeb("libnew", "3.0")},
		Resolved: []ResolvedPackage{{Name: "app", Version: "1.0", Arch: "amd64"}},
		ArtifactDeps: []ArtifactDependency{{
			Package: "app",
			Alternatives: []DependencyAlternative{
				{Name: "libold", Constraint: &VersionConstraint{"=", "2.0"}},  // present but wrong version
				{Name: "libnew", Constraint: &VersionConstraint{">=", "2.0"}}, // present and satisfies
			},
		}},
		Policy: config.OverlayPolicy{},
	})
	if report.Blocked || report.UnsatisfiedDeps != 0 {
		t.Errorf("expected no block when an alternative satisfies the edge, got %+v", report)
	}
}
