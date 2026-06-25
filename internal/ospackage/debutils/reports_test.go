package debutils_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage/debutils"
)

func readMissingReport(t *testing.T, reportPath string) debutils.MissingReport {
	t.Helper()

	reportBytes, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v", reportPath, err)
	}

	var report debutils.MissingReport
	if err := json.Unmarshal(reportBytes, &report); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", reportPath, err)
	}

	return report
}

func TestBuildDependencyChains(t *testing.T) {
	originalReportPath := debutils.ReportPath
	debutils.ReportPath = t.TempDir()
	t.Cleanup(func() {
		debutils.ReportPath = originalReportPath
	})

	testCases := []struct {
		name                string
		pairs               [][]ospackage.PackageInfo
		expectNonEmptyPath  bool
		expectedMissingName string
	}{
		{
			name: "simple parent-child chain",
			pairs: [][]ospackage.PackageInfo{
				{
					{Name: "parent", Version: "1.0"},
					{Name: "child", Version: "1.0"},
				},
			},
			expectNonEmptyPath: true,
		},
		{
			name: "missing dependency chain",
			pairs: [][]ospackage.PackageInfo{
				{
					{Name: "parent", Version: "1.0"},
					{Name: "missing-child(missing)", Version: ""},
				},
			},
			expectNonEmptyPath:  true,
			expectedMissingName: "missing-child",
		},
		{
			name: "missing dependency does not merge with resolved package of same name",
			pairs: [][]ospackage.PackageInfo{
				{
					{Name: "root", Version: "1.0"},
					{Name: "mesa-libgallium", Version: "25.3.4"},
				},
				{
					{Name: "consumer", Version: "1.0"},
					{Name: "mesa-libgallium(missing)"},
				},
				{
					{Name: "mesa-libgallium", Version: "25.3.4"},
					{Name: "libllvm18", Version: "18.1.3"},
				},
			},
			expectNonEmptyPath:  true,
			expectedMissingName: "mesa-libgallium",
		},
		{
			name:               "empty pairs",
			pairs:              [][]ospackage.PackageInfo{},
			expectNonEmptyPath: true, // Function should still create a file
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := debutils.BuildDependencyChains(tc.pairs)

			if tc.expectNonEmptyPath && result == "" {
				t.Errorf("expected non-empty path, got empty string")
			}

			if tc.expectedMissingName == "" {
				return
			}

			report := readMissingReport(t, result)
			chains, ok := report.Missing[tc.expectedMissingName]
			if !ok {
				t.Fatalf("report.Missing[%q] not found; got %v", tc.expectedMissingName, report.Missing)
			}

			if len(chains) == 0 {
				t.Fatalf("report.Missing[%q] is empty", tc.expectedMissingName)
			}
		})
	}
}

func TestAddParentChildPair(t *testing.T) {
	var pairs [][]ospackage.PackageInfo

	parent := ospackage.PackageInfo{Name: "parent", Version: "1.0"}
	child := ospackage.PackageInfo{Name: "child", Version: "2.0"}

	debutils.AddParentChildPair(parent, child, &pairs)

	if len(pairs) != 1 {
		t.Errorf("expected 1 pair, got %d", len(pairs))
		return
	}

	if len(pairs[0]) != 2 {
		t.Errorf("expected pair to have 2 elements, got %d", len(pairs[0]))
		return
	}

	if pairs[0][0].Name != "parent" {
		t.Errorf("expected parent name 'parent', got %q", pairs[0][0].Name)
	}

	if pairs[0][1].Name != "child" {
		t.Errorf("expected child name 'child', got %q", pairs[0][1].Name)
	}
}

func TestAddParentMissingChildPair(t *testing.T) {
	var pairs [][]ospackage.PackageInfo

	parent := ospackage.PackageInfo{Name: "parent", Version: "1.0"}
	missingChildName := "missing-dep(missing)"

	debutils.AddParentMissingChildPair(parent, missingChildName, &pairs)

	if len(pairs) != 1 {
		t.Errorf("expected 1 pair, got %d", len(pairs))
		return
	}

	if len(pairs[0]) != 2 {
		t.Errorf("expected pair to have 2 elements, got %d", len(pairs[0]))
		return
	}

	if pairs[0][0].Name != "parent" {
		t.Errorf("expected parent name 'parent', got %q", pairs[0][0].Name)
	}

	if pairs[0][1].Name != missingChildName {
		t.Errorf("expected child name %q, got %q", missingChildName, pairs[0][1].Name)
	}
}
