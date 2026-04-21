package debutils_test

import (
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage/debutils"
)

func TestBuildDependencyChains(t *testing.T) {
	testCases := []struct {
		name               string
		pairs              [][]ospackage.PackageInfo
		expectNonEmptyPath bool
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
			expectNonEmptyPath: true,
		},
		{
			name:               "empty pairs",
			pairs:              [][]ospackage.PackageInfo{},
			expectNonEmptyPath: true, // Function should still create a file
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := debutils.BuildDependencyChains(tc.pairs)

			if tc.expectNonEmptyPath && result == "" {
				t.Errorf("expected non-empty path, got empty string")
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
