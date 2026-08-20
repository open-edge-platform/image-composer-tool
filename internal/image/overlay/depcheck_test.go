package overlay

import (
	"reflect"
	"strings"
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

func TestParseDebConflictsField(t *testing.T) {
	// A realistic Conflicts line: a bare conflict, a versioned conflict, and a
	// multiarch-qualified one. Conflicts/Breaks carry no "a | b" alternatives.
	field := "oldpkg, libfoo (<< 2.0), bar:amd64 (= 1.0)"
	conflicts := parseDebConflictsField("mypkg", field)
	if len(conflicts) != 3 {
		t.Fatalf("got %d conflicts, want 3: %+v", len(conflicts), conflicts)
	}
	if conflicts[0].Package != "mypkg" || conflicts[0].Conflicts.Name != "oldpkg" || conflicts[0].Conflicts.Constraint != nil {
		t.Errorf("conflict[0] = %+v, want bare oldpkg", conflicts[0])
	}
	if conflicts[1].Conflicts.Name != "libfoo" || conflicts[1].Conflicts.Constraint == nil ||
		conflicts[1].Conflicts.Constraint.Op != "<<" || conflicts[1].Conflicts.Constraint.Ver != "2.0" {
		t.Errorf("conflict[1] = %+v, want libfoo (<< 2.0)", conflicts[1])
	}
	if conflicts[2].Conflicts.Name != "bar" || conflicts[2].Conflicts.Constraint == nil {
		t.Errorf("conflict[2] = %+v, want bar (= 1.0) with arch stripped", conflicts[2])
	}
}

func TestParseRPMConflicts(t *testing.T) {
	out := "oldpkg\nlibfoo < 2.0\n/some/file\nrpmlib(Something) <= 4.0\n"
	conflicts := parseRPMConflicts("mypkg", out)
	// The bare name and the versioned conflict are kept; the file and rpmlib
	// entries are skipped.
	if len(conflicts) != 2 {
		t.Fatalf("got %d conflicts, want 2: %+v", len(conflicts), conflicts)
	}
	if conflicts[0].Conflicts.Name != "oldpkg" || conflicts[0].Conflicts.Constraint != nil {
		t.Errorf("conflict[0] = %+v, want bare oldpkg", conflicts[0])
	}
	if conflicts[1].Conflicts.Name != "libfoo" || conflicts[1].Conflicts.Constraint == nil ||
		conflicts[1].Conflicts.Constraint.Op != "<" {
		t.Errorf("conflict[1] = %+v, want libfoo < 2.0", conflicts[1])
	}
}

// TestClassifyConflicts covers the pure conflict classifier: a declared conflict
// against a present baseline package fires (bare and versioned-in-range), and a
// conflict against an absent package or a versioned range that excludes the
// baseline version does not.
func TestClassifyConflicts(t *testing.T) {
	sliceA := baselineVersionIndex([]BaselinePackage{
		installedDeb("oldpkg", "1.0"),
		installedDeb("libfoo", "1.5"),
		installedDeb("libbar", "3.0"),
	})

	tests := []struct {
		name         string
		resolved     []ResolvedPackage // the overlay's to-install set (post-install state)
		conflicts    []ArtifactConflict
		wantCount    int
		wantTarget   string
		wantConflict string // the declaring artifact (ConflictWith)
		wantVersion  string // the reported CurrentVersion, when asserted
	}{
		{
			name:         "bare conflict on present package fires",
			conflicts:    []ArtifactConflict{{Package: "newpkg", Conflicts: DependencyAlternative{Name: "oldpkg"}}},
			wantCount:    1,
			wantTarget:   "oldpkg",
			wantConflict: "newpkg",
		},
		{
			name:      "conflict on absent package is skipped",
			conflicts: []ArtifactConflict{{Package: "newpkg", Conflicts: DependencyAlternative{Name: "absent-pkg"}}},
			wantCount: 0,
		},
		{
			name:         "versioned conflict in range fires",
			conflicts:    []ArtifactConflict{{Package: "newpkg", Conflicts: DependencyAlternative{Name: "libfoo", Constraint: &VersionConstraint{"<<", "2.0"}}}},
			wantCount:    1,
			wantTarget:   "libfoo",
			wantConflict: "newpkg",
		},
		{
			name:      "versioned conflict out of range is skipped",
			conflicts: []ArtifactConflict{{Package: "newpkg", Conflicts: DependencyAlternative{Name: "libbar", Constraint: &VersionConstraint{"<<", "2.0"}}}},
			wantCount: 0,
		},
		{
			// The overlay upgrades the target past the break range in the same batch,
			// so the versioned break no longer covers the post-install version and must
			// not be flagged (mirrors vim-runtime "Breaks: vim-tiny (<< X)" while the
			// overlay upgrades vim-tiny to X).
			name:      "versioned break resolved by a same-batch upgrade is skipped",
			resolved:  []ResolvedPackage{{Name: "libfoo", Version: "2.0"}},
			conflicts: []ArtifactConflict{{Package: "newpkg", Conflicts: DependencyAlternative{Name: "libfoo", Constraint: &VersionConstraint{"<<", "2.0"}}}},
			wantCount: 0,
		},
		{
			// Two packages the overlay adds in the SAME batch conflict. The target is
			// absent from the baseline but present in the to-install set, so it must be
			// gated here rather than slip through to dpkg unpack (the free vs non-free
			// intel-media-va-driver case).
			name:         "conflict on a co-added (new) package fires",
			resolved:     []ResolvedPackage{{Name: "intel-media-va-driver", Version: "24.1.0"}},
			conflicts:    []ArtifactConflict{{Package: "intel-media-va-driver-non-free", Conflicts: DependencyAlternative{Name: "intel-media-va-driver"}}},
			wantCount:    1,
			wantTarget:   "intel-media-va-driver",
			wantConflict: "intel-media-va-driver-non-free",
		},
		{
			// A versioned conflict on a co-added package whose to-install version is out
			// of the declared range is not a clash.
			name:      "versioned conflict on a co-added package out of range is skipped",
			resolved:  []ResolvedPackage{{Name: "newlib", Version: "3.0"}},
			conflicts: []ArtifactConflict{{Package: "otherpkg", Conflicts: DependencyAlternative{Name: "newlib", Constraint: &VersionConstraint{"<<", "2.0"}}}},
			wantCount: 0,
		},
		{
			// The target is in the baseline AND upgraded in the same batch to a version
			// that still falls in the conflict range, so the clash fires — and it must
			// report the upgraded (post-install) version, not the superseded baseline one.
			name:         "conflict on an upgraded baseline target reports the upgraded version",
			resolved:     []ResolvedPackage{{Name: "libfoo", Version: "1.9"}},
			conflicts:    []ArtifactConflict{{Package: "newpkg", Conflicts: DependencyAlternative{Name: "libfoo", Constraint: &VersionConstraint{"<<", "2.0"}}}},
			wantCount:    1,
			wantTarget:   "libfoo",
			wantConflict: "newpkg",
			wantVersion:  "1.9",
		},
		{
			// Two co-added packages both Provide and Conflict the same virtual name (the
			// classic "only one mail-transport-agent" pattern). Neither is literally named
			// the virtual target, so the clash is visible only via the provider index; it
			// must fire against the other REAL provider (reported by its package name so a
			// removal-enabled policy targets a package that exists), not the virtual name.
			name: "conflict on a co-added provided virtual name fires",
			resolved: []ResolvedPackage{
				{Name: "postfix", Version: "3.8", Provides: []string{"mail-transport-agent"}},
				{Name: "exim4", Version: "4.97", Provides: []string{"mail-transport-agent"}},
			},
			conflicts:    []ArtifactConflict{{Package: "postfix", Conflicts: DependencyAlternative{Name: "mail-transport-agent"}}},
			wantCount:    1,
			wantTarget:   "exim4",
			wantConflict: "postfix",
			wantVersion:  "4.97",
		},
		{
			// A VERSIONED conflict against a virtual name provided (unversioned) by a
			// co-added package is not matched: this resolver carries only unversioned
			// Provides, and per Debian policy a versioned conflict does not match an
			// unversioned virtual provider, so it must not be flagged.
			name: "versioned conflict on a co-added provided virtual name is skipped",
			resolved: []ResolvedPackage{
				{Name: "postfix", Version: "3.8", Provides: []string{"mail-transport-agent"}},
				{Name: "exim4", Version: "4.97", Provides: []string{"mail-transport-agent"}},
			},
			conflicts: []ArtifactConflict{{Package: "postfix", Conflicts: DependencyAlternative{Name: "mail-transport-agent", Constraint: &VersionConstraint{"<<", "5.0"}}}},
			wantCount: 0,
		},
		{
			// A lone package that Provides and Conflicts the same virtual name does not
			// clash with itself (Debian policy), so it must not be flagged.
			name: "self-provided virtual conflict is skipped",
			resolved: []ResolvedPackage{
				{Name: "postfix", Version: "3.8", Provides: []string{"mail-transport-agent"}},
			},
			conflicts: []ArtifactConflict{{Package: "postfix", Conflicts: DependencyAlternative{Name: "mail-transport-agent"}}},
			wantCount: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actions := classifyConflicts(PackageManagerAPT, sliceA, tt.resolved, tt.conflicts)
			if len(actions) != tt.wantCount {
				t.Fatalf("got %d actions, want %d: %+v", len(actions), tt.wantCount, actions)
			}
			if tt.wantCount == 0 {
				return
			}
			if actions[0].Type != ActionConflict {
				t.Errorf("type = %s, want %s", actions[0].Type, ActionConflict)
			}
			if actions[0].Package != tt.wantTarget {
				t.Errorf("target = %q, want %q", actions[0].Package, tt.wantTarget)
			}
			if actions[0].ConflictWith != tt.wantConflict {
				t.Errorf("conflictWith = %q, want %q", actions[0].ConflictWith, tt.wantConflict)
			}
			if tt.wantVersion != "" && actions[0].CurrentVersion != tt.wantVersion {
				t.Errorf("currentVersion = %q, want %q", actions[0].CurrentVersion, tt.wantVersion)
			}
		})
	}
}

// TestSimulateOverlayInstall_DeclaredConflictBlocked exercises the full seam:
// the default simulateOverlayInstall reads artifact conflicts (overridden here to
// avoid a real dpkg call) and feeds them through Preflight, which must block the
// declared conflict against a present baseline package under the default fail
// conflict policy — the end-to-end regression for the "conflict slips past the
// gate" gap.
func TestSimulateOverlayInstall_DeclaredConflictBlocked(t *testing.T) {
	origRead := readOverlayArtifactConflicts
	defer func() { readOverlayArtifactConflicts = origRead }()
	readOverlayArtifactConflicts = func(PackageManager, *ResolutionPlan) ([]ArtifactConflict, error) {
		return []ArtifactConflict{{Package: "newpkg", Conflicts: DependencyAlternative{Name: "oldpkg"}}}, nil
	}

	info := &BaselineInfo{OS: "ubuntu", Arch: "amd64", PackageManager: PackageManagerAPT}
	baseline := []BaselinePackage{installedDeb("oldpkg", "1.0")}
	plan := &ResolutionPlan{
		DownloadDir: "/tmp/does-not-matter", // the reader is stubbed
		ToInstall:   []ResolvedPackage{{Name: "newpkg", Version: "2.0", Arch: "amd64", URL: "https://x/newpkg.deb"}},
	}

	report, err := Preflight(info, baseline, plan, &config.OverlayPolicy{})
	if err == nil || report == nil || !report.Blocked {
		t.Fatalf("expected a blocked conflict, err=%v report=%+v", err, report)
	}
	if report.Conflicts != 1 {
		t.Fatalf("conflicts = %d, want 1: %+v", report.Conflicts, report.Actions)
	}
	if report.Violations[0].Rule != ruleConflictPolicyFail {
		t.Errorf("rule = %s, want %s", report.Violations[0].Rule, ruleConflictPolicyFail)
	}
	if report.Violations[0].Action.ConflictWith != "newpkg" {
		t.Errorf("conflicting artifact = %q, want newpkg", report.Violations[0].Action.ConflictWith)
	}
	if report.Violations[0].Action.CurrentVersion != "1.0" {
		t.Errorf("current version = %q, want 1.0 (from baseline)", report.Violations[0].Action.CurrentVersion)
	}
	for _, want := range []string{"newpkg", "oldpkg", "conflict"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

// TestClassifyIntraSetConflicts covers the pure intra-set conflict classifier:
// two DIFFERENT to-install packages that conflict with each other must be
// flagged even though neither is in the baseline, whether the conflict names a
// real package directly (libcurl4-gnutls-dev vs libcurl4-openssl-dev) or a
// virtual capability shared via Provides: (the ROS 2 univloc "-sse"/"-lze"
// hardware variants). A package's own Conflicts: is never flagged against
// itself, and a duplicate/bidirectional declaration reports the pair once.
func TestClassifyIntraSetConflicts(t *testing.T) {
	tests := []struct {
		name      string
		resolved  []ResolvedPackage
		conflicts []ArtifactConflict
		provides  []ArtifactProvides
		wantPairs [][2]string // unordered pairs, order within pair does not matter
	}{
		{
			name: "direct conflict between two to-install packages fires",
			resolved: []ResolvedPackage{
				{Name: "libcurl4-gnutls-dev", Version: "8.5.0-2ubuntu10.11"},
				{Name: "libcurl4-openssl-dev", Version: "8.5.0-2ubuntu10.11"},
			},
			conflicts: []ArtifactConflict{
				{Package: "libcurl4-gnutls-dev", Conflicts: DependencyAlternative{Name: "libcurl4-openssl-dev"}},
			},
			wantPairs: [][2]string{{"libcurl4-gnutls-dev", "libcurl4-openssl-dev"}},
		},
		{
			name: "conflict against a virtual name is resolved through Provides",
			resolved: []ResolvedPackage{
				{Name: "ros-jazzy-univloc-slam-sse", Version: "2.3-2"},
				{Name: "ros-jazzy-univloc-slam-lze", Version: "2.3-2"},
			},
			conflicts: []ArtifactConflict{
				{Package: "ros-jazzy-univloc-slam-lze", Conflicts: DependencyAlternative{Name: "ros-jazzy-univloc-slam"}},
			},
			provides: []ArtifactProvides{
				{Package: "ros-jazzy-univloc-slam-sse", Provides: []string{"ros-jazzy-univloc-slam"}},
			},
			wantPairs: [][2]string{{"ros-jazzy-univloc-slam-lze", "ros-jazzy-univloc-slam-sse"}},
		},
		{
			name: "a package never conflicts with itself",
			resolved: []ResolvedPackage{
				{Name: "solo-pkg", Version: "1.0"},
			},
			conflicts: []ArtifactConflict{
				{Package: "solo-pkg", Conflicts: DependencyAlternative{Name: "solo-pkg"}},
			},
			wantPairs: nil,
		},
		{
			name: "conflict against a package not in the to-install set is a no-op",
			resolved: []ResolvedPackage{
				{Name: "newpkg", Version: "1.0"},
			},
			conflicts: []ArtifactConflict{
				{Package: "newpkg", Conflicts: DependencyAlternative{Name: "unrelated-pkg"}},
			},
			wantPairs: nil,
		},
		{
			name: "a bidirectional declaration reports the pair once",
			resolved: []ResolvedPackage{
				{Name: "pkg-a", Version: "1.0"},
				{Name: "pkg-b", Version: "1.0"},
			},
			conflicts: []ArtifactConflict{
				{Package: "pkg-a", Conflicts: DependencyAlternative{Name: "pkg-b"}},
				{Package: "pkg-b", Conflicts: DependencyAlternative{Name: "pkg-a"}},
			},
			wantPairs: [][2]string{{"pkg-a", "pkg-b"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actions := classifyIntraSetConflicts(PackageManagerAPT, tt.resolved, tt.conflicts, tt.provides)
			if len(actions) != len(tt.wantPairs) {
				t.Fatalf("got %d action(s), want %d: %+v", len(actions), len(tt.wantPairs), actions)
			}
			gotPairs := make(map[[2]string]bool, len(actions))
			for _, a := range actions {
				if a.Type != ActionConflict {
					t.Errorf("type = %s, want %s", a.Type, ActionConflict)
				}
				pair := [2]string{a.Package, a.ConflictWith}
				if pair[0] > pair[1] {
					pair[0], pair[1] = pair[1], pair[0]
				}
				gotPairs[pair] = true
			}
			for _, want := range tt.wantPairs {
				if want[0] > want[1] {
					want[0], want[1] = want[1], want[0]
				}
				if !gotPairs[want] {
					t.Errorf("missing expected conflicting pair %v in %+v", want, actions)
				}
			}
		})
	}
}

// TestSimulateOverlayInstall_IntraSetConflictBlocked exercises the full seam
// end-to-end for the ITEP-95568 regression: an overlay that resolves two
// mutually exclusive ROS 2 univloc hardware variants (neither present in the
// baseline) must be blocked at preflight instead of failing mid-dpkg-unpack.
func TestSimulateOverlayInstall_IntraSetConflictBlocked(t *testing.T) {
	origConflicts, origProvides := readOverlayArtifactConflicts, readOverlayArtifactProvides
	defer func() {
		readOverlayArtifactConflicts = origConflicts
		readOverlayArtifactProvides = origProvides
	}()
	readOverlayArtifactConflicts = func(PackageManager, *ResolutionPlan) ([]ArtifactConflict, error) {
		return []ArtifactConflict{
			{Package: "ros-jazzy-univloc-slam-lze", Conflicts: DependencyAlternative{Name: "ros-jazzy-univloc-slam"}},
		}, nil
	}
	readOverlayArtifactProvides = func(PackageManager, *ResolutionPlan) ([]ArtifactProvides, error) {
		return []ArtifactProvides{
			{Package: "ros-jazzy-univloc-slam-sse", Provides: []string{"ros-jazzy-univloc-slam"}},
		}, nil
	}

	info := &BaselineInfo{OS: "ubuntu", Arch: "amd64", PackageManager: PackageManagerAPT}
	plan := &ResolutionPlan{
		DownloadDir: "/tmp/does-not-matter", // both readers are stubbed
		ToInstall: []ResolvedPackage{
			{Name: "ros-jazzy-univloc-slam-sse", Version: "2.3-2", Arch: "amd64", URL: "https://x/sse.deb"},
			{Name: "ros-jazzy-univloc-slam-lze", Version: "2.3-2", Arch: "amd64", URL: "https://x/lze.deb"},
		},
	}

	report, err := Preflight(info, nil, plan, &config.OverlayPolicy{})
	if err == nil || report == nil || !report.Blocked {
		t.Fatalf("expected the intra-set conflict to block, err=%v report=%+v", err, report)
	}
	if report.Conflicts != 1 {
		t.Fatalf("conflicts = %d, want 1: %+v", report.Conflicts, report.Actions)
	}
	if report.Violations[0].Rule != ruleConflictPolicyFail {
		t.Errorf("rule = %s, want %s", report.Violations[0].Rule, ruleConflictPolicyFail)
	}
}
