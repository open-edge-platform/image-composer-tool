package debutils

import (
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
)

func TestVersionConstraintAlternatives(t *testing.T) {
	testCases := []struct {
		name        string
		reqVers     []string
		depName     string
		expected    []VersionConstraint
		expectFound bool
	}{
		{
			name:    "simple alternative with version constraint",
			reqVers: []string{"logsave | e2fsprogs (<< 1.45.3-1~)"},
			depName: "e2fsprogs",
			expected: []VersionConstraint{
				{
					Op:          "<<",
					Ver:         "1.45.3-1~",
					Alternative: "logsave",
				},
			},
			expectFound: true,
		},
		{
			name:    "simple alternative without version constraint",
			reqVers: []string{"logsave | e2fsprogs"},
			depName: "e2fsprogs",
			expected: []VersionConstraint{
				{
					Alternative: "logsave",
				},
			},
			expectFound: true,
		},
		{
			name:    "multiple alternatives with version constraint",
			reqVers: []string{"pkg1 | pkg2 | pkg3 (>= 1.0.0)"},
			depName: "pkg3",
			expected: []VersionConstraint{
				{
					Op:          ">=",
					Ver:         "1.0.0",
					Alternative: "pkg1|pkg2",
				},
			},
			expectFound: true,
		},
		{
			name:    "first alternative matches",
			reqVers: []string{"logsave | e2fsprogs (<< 1.45.3-1~)"},
			depName: "logsave",
			expected: []VersionConstraint{
				{
					Alternative: "e2fsprogs",
				},
			},
			expectFound: true,
		},
		{
			name:        "no match",
			reqVers:     []string{"logsave | e2fsprogs (<< 1.45.3-1~)"},
			depName:     "otherpkg",
			expected:    nil,
			expectFound: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			constraints, found := extractVersionRequirement(tc.reqVers, tc.depName)

			if found != tc.expectFound {
				t.Errorf("expected found=%v, got %v", tc.expectFound, found)
			}

			if len(constraints) != len(tc.expected) {
				t.Errorf("expected %d constraints, got %d", len(tc.expected), len(constraints))
				return
			}

			for i, expected := range tc.expected {
				if i >= len(constraints) {
					break
				}
				actual := constraints[i]
				if actual.Op != expected.Op {
					t.Errorf("constraint %d: expected Op=%q, got %q", i, expected.Op, actual.Op)
				}
				if actual.Ver != expected.Ver {
					t.Errorf("constraint %d: expected Ver=%q, got %q", i, expected.Ver, actual.Ver)
				}
				if actual.Alternative != expected.Alternative {
					t.Errorf("constraint %d: expected Alternative=%q, got %q", i, expected.Alternative, actual.Alternative)
				}
			}
		})
	}
}

func TestResolveDependenciesWithAlternatives(t *testing.T) {
	// Create test packages
	packages := []ospackage.PackageInfo{
		{
			Name:        "main-pkg",
			Version:     "1.0.0",
			URL:         "http://example.com/pool/main/m/main-pkg/main-pkg_1.0.0_amd64.deb",
			Type:        "deb",
			Requires:    []string{"logsave", "e2fsprogs"},
			RequiresVer: []string{"logsave | e2fsprogs (<< 1.45.3-1~)"},
		},
		{
			Name:    "logsave",
			Version: "1.0.0",
			URL:     "http://example.com/pool/main/l/logsave/logsave_1.0.0_amd64.deb",
			Type:    "deb",
		},
		{
			Name:    "e2fsprogs",
			Version: "1.45.2-1",
			URL:     "http://example.com/pool/main/e/e2fsprogs/e2fsprogs_1.45.2-1_amd64.deb",
			Type:    "deb",
		},
		{
			Name:    "e2fsprogs",
			Version: "1.45.4-1", // This version doesn't satisfy << 1.45.3-1~
			URL:     "http://example.com/pool/main/e/e2fsprogs/e2fsprogs_1.45.4-1_amd64.deb",
			Type:    "deb",
		},
		{
			Name:    "e2fsprogs",
			Version: "1.47.0-2.4~exp1ubuntu4", // Latest version, doesn't satisfy << 1.45.3-1~
			URL:     "http://example.com/pool/main/e/e2fsprogs/e2fsprogs_1.47.0-2.4~exp1ubuntu4_amd64.deb",
			Type:    "deb",
		},
	}

	requested := []ospackage.PackageInfo{
		packages[0], // main-pkg
	}

	resolved, err := ResolveDependencies(requested, packages)
	if err != nil {
		t.Fatalf("ResolveDependencies failed: %v", err)
	}

	// Should resolve main-pkg, and either logsave or e2fsprogs (1.45.2-1)
	foundMainPkg := false
	foundLogsave := false
	foundE2fsprogs := false

	t.Logf("Resolved packages:")
	for _, pkg := range resolved {
		t.Logf("  %s_%s", pkg.Name, pkg.Version)
		switch pkg.Name {
		case "main-pkg":
			foundMainPkg = true
		case "logsave":
			foundLogsave = true
		case "e2fsprogs":
			foundE2fsprogs = true
			if pkg.Version == "1.45.4-1" {
				t.Errorf("Should not resolve e2fsprogs 1.45.4-1 as it doesn't satisfy version constraint")
			}
		}
	}

	if !foundMainPkg {
		t.Error("main-pkg not found in resolved dependencies")
	}

	// Should have either logsave or e2fsprogs (version 1.45.2-1)
	if !foundLogsave && !foundE2fsprogs {
		t.Error("Neither logsave nor e2fsprogs found in resolved dependencies")
	}
}

func TestResolveDependenciesWithDirectAndAlternativeDeps(t *testing.T) {
	// Test case where e2fsprogs is both a direct dependency and part of an alternative constraint
	// Direct dependency should take priority, ignoring version constraints from alternatives
	packages := []ospackage.PackageInfo{
		{
			Name:        "main-pkg",
			Version:     "1.0.0",
			URL:         "http://example.com/pool/main/m/main-pkg/main-pkg_1.0.0_amd64.deb",
			Type:        "deb",
			Requires:    []string{"e2fsprogs", "logsave"},
			RequiresVer: []string{"logsave | e2fsprogs (<< 1.45.3-1~)"},
		},
		{
			Name:    "logsave",
			Version: "1.0.0",
			URL:     "http://example.com/pool/main/l/logsave/logsave_1.0.0_amd64.deb",
			Type:    "deb",
		},
		{
			Name:    "e2fsprogs",
			Version: "1.45.2-1",
			URL:     "http://example.com/pool/main/e/e2fsprogs/e2fsprogs_1.45.2-1_amd64.deb",
			Type:    "deb",
		},
		{
			Name:    "e2fsprogs",
			Version: "1.45.4-1",
			URL:     "http://example.com/pool/main/e/e2fsprogs/e2fsprogs_1.45.4-1_amd64.deb",
			Type:    "deb",
		},
		{
			Name:    "e2fsprogs",
			Version: "1.47.0-2.4~exp1ubuntu4", // Latest version - should be selected despite constraint
			URL:     "http://example.com/pool/main/e/e2fsprogs/e2fsprogs_1.47.0-2.4~exp1ubuntu4_amd64.deb",
			Type:    "deb",
		},
	}

	requested := []ospackage.PackageInfo{
		packages[0], // main-pkg
	}

	resolved, err := ResolveDependencies(requested, packages)
	if err != nil {
		t.Fatalf("ResolveDependencies failed: %v", err)
	}

	// Should resolve main-pkg, logsave, and e2fsprogs with latest version (direct dependency takes priority)
	foundMainPkg := false
	foundLogsave := false
	foundE2fsprogs := false
	e2fsprogsVersion := ""

	t.Logf("Resolved packages (direct + alternative dependencies):")
	for _, pkg := range resolved {
		t.Logf("  %s_%s", pkg.Name, pkg.Version)
		switch pkg.Name {
		case "main-pkg":
			foundMainPkg = true
		case "logsave":
			foundLogsave = true
		case "e2fsprogs":
			foundE2fsprogs = true
			e2fsprogsVersion = pkg.Version
		}
	}

	if !foundMainPkg {
		t.Error("main-pkg not found in resolved dependencies")
	}

	if !foundLogsave {
		t.Error("logsave not found in resolved dependencies")
	}

	if !foundE2fsprogs {
		t.Error("e2fsprogs not found in resolved dependencies")
	} else {
		// Should pick the latest version since e2fsprogs is a direct dependency without constraints
		if e2fsprogsVersion != "1.47.0-2.4~exp1ubuntu4" {
			t.Errorf("Expected e2fsprogs version 1.47.0-2.4~exp1ubuntu4 (latest), got %s", e2fsprogsVersion)
		}
	}
}

func TestResolveDependenciesWithAlternativesNoLogsave(t *testing.T) {
	// Create test packages without logsave to test e2fsprogs version selection
	packages := []ospackage.PackageInfo{
		{
			Name:        "main-pkg",
			Version:     "1.0.0",
			URL:         "http://example.com/pool/main/m/main-pkg/main-pkg_1.0.0_amd64.deb",
			Type:        "deb",
			Requires:    []string{"logsave"}, // Primary dependency (will fall back to alternative)
			RequiresVer: []string{"logsave | e2fsprogs (<< 1.45.3-1~)"},
		},
		{
			Name:    "e2fsprogs",
			Version: "1.45.2-1", // This satisfies << 1.45.3-1~
			URL:     "http://example.com/pool/main/e/e2fsprogs/e2fsprogs_1.45.2-1_amd64.deb",
			Type:    "deb",
		},
		{
			Name:    "e2fsprogs",
			Version: "1.45.4-1", // This version doesn't satisfy << 1.45.3-1~
			URL:     "http://example.com/pool/main/e/e2fsprogs/e2fsprogs_1.45.4-1_amd64.deb",
			Type:    "deb",
		},
		{
			Name:    "e2fsprogs",
			Version: "1.47.0-2.4~exp1ubuntu4", // Latest version, doesn't satisfy << 1.45.3-1~
			URL:     "http://example.com/pool/main/e/e2fsprogs/e2fsprogs_1.47.0-2.4~exp1ubuntu4_amd64.deb",
			Type:    "deb",
		},
	}

	requested := []ospackage.PackageInfo{
		packages[0], // main-pkg
	}

	resolved, err := ResolveDependencies(requested, packages)
	if err != nil {
		t.Fatalf("ResolveDependencies failed: %v", err)
	}

	// Should resolve main-pkg and e2fsprogs version 1.45.2-1 (highest version satisfying constraint)
	foundMainPkg := false
	foundE2fsprogs := false
	e2fsprogsVersion := ""

	t.Logf("Resolved packages (no logsave available):")
	for _, pkg := range resolved {
		t.Logf("  %s_%s", pkg.Name, pkg.Version)
		switch pkg.Name {
		case "main-pkg":
			foundMainPkg = true
		case "e2fsprogs":
			foundE2fsprogs = true
			e2fsprogsVersion = pkg.Version
			// Should not resolve versions that don't satisfy the constraint
			if pkg.Version == "1.45.4-1" || pkg.Version == "1.47.0-2.4~exp1ubuntu4" {
				t.Errorf("Should not resolve e2fsprogs %s as it doesn't satisfy version constraint << 1.45.3-1~", pkg.Version)
			}
		}
	}

	if !foundMainPkg {
		t.Error("main-pkg not found in resolved dependencies")
	}

	if !foundE2fsprogs {
		t.Error("e2fsprogs not found in resolved dependencies")
	} else {
		// Should pick version 1.45.2-1 as it's the highest version satisfying the constraint
		if e2fsprogsVersion != "1.45.2-1" {
			t.Errorf("Expected e2fsprogs version 1.45.2-1, got %s", e2fsprogsVersion)
		}
	}
}
