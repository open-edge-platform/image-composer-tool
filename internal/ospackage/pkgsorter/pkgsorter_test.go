package pkgsorter

import (
	"reflect"
	"sort"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
)

// Helper function to extract just the names from a slice of PackageInfo
// for easier comparison in tests.
func getNames(pkgs []ospackage.PackageInfo) []string {
	names := make([]string, len(pkgs))
	for i, p := range pkgs {
		names[i] = p.Name
	}
	return names
}

func TestSortPackages(t *testing.T) {

	testCases := []struct {
		name          string
		input         []ospackage.PackageInfo
		want          []string
		wantErr       bool
		isOrderStrict bool // If true, the exact order of `want` is checked.
	}{
		{
			name: "Simple Chain Dependency",
			input: []ospackage.PackageInfo{
				{Name: "C", Requires: []string{"B"}},
				{Name: "B", Requires: []string{"A"}},
				{Name: "A"},
			},
			want:          []string{"A", "B", "C"},
			wantErr:       false,
			isOrderStrict: true,
		},
		{
			name: "Complex Diamond Dependency",
			input: []ospackage.PackageInfo{
				{Name: "app", Requires: []string{"lib-a", "lib-b"}},
				{Name: "lib-a", Requires: []string{"lib-c"}},
				{Name: "lib-b", Requires: []string{"lib-c"}},
				{Name: "lib-c"},
			},
			// We will check this case specially since the order of lib-a and lib-b is not guaranteed.
			want:          []string{"lib-c", "lib-a", "lib-b", "app"},
			wantErr:       false,
			isOrderStrict: false,
		},
		{
			name: "Circular Dependency",
			input: []ospackage.PackageInfo{
				{Name: "A", Requires: []string{"B"}},
				{Name: "B", Requires: []string{"A"}},
			},
			// The SCC sort handles cycles. The order within the cycle is alphabetical.
			want:          []string{"A", "B"},
			wantErr:       false, // The SCC sorter should NOT error on cycles.
			isOrderStrict: true,
		},
		{
			name: "Multi-Package Cycle",
			input: []ospackage.PackageInfo{
				{Name: "C", Requires: []string{"B"}},
				{Name: "B", Requires: []string{"A"}},
				{Name: "A", Requires: []string{"C"}},
				{Name: "D", Requires: []string{"A"}}, // Depends on the cycle
			},
			// A, B, C are in a cycle and will be sorted alphabetically. D depends on them.
			want:          []string{"A", "B", "C", "D"},
			wantErr:       false,
			isOrderStrict: true,
		},
		{
			name: "Already Resolved Dependency",
			input: []ospackage.PackageInfo{
				{Name: "app", Requires: []string{"nginx"}},
				{Name: "nginx"},
			},
			want:          []string{"nginx", "app"},
			wantErr:       false,
			isOrderStrict: true,
		},
		{
			name:          "Empty Input Slice",
			input:         []ospackage.PackageInfo{},
			want:          []string{},
			wantErr:       false,
			isOrderStrict: true,
		},
		{
			name: "Single Package",
			input: []ospackage.PackageInfo{
				{Name: "single-package"},
			},
			want:          []string{"single-package"},
			wantErr:       false,
			isOrderStrict: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SortPackages(tc.input)

			if (err != nil) != tc.wantErr {
				t.Fatalf("SortPackages() error = %v, wantErr %v", err, tc.wantErr)
			}

			if tc.wantErr {
				return
			}

			gotNames := getNames(got)

			// Special check for Diamond Dependency, where lib-a and lib-b can be swapped.
			if tc.name == "Complex Diamond Dependency" {
				if len(gotNames) != 4 || gotNames[0] != "lib-c" || gotNames[3] != "app" {
					t.Errorf("SortPackages() = %v, want 'lib-c' first and 'app' last", gotNames)
				}
				middle := gotNames[1:3]
				sort.Strings(middle) // Sort to make comparison deterministic
				if !reflect.DeepEqual(middle, []string{"lib-a", "lib-b"}) {
					t.Errorf("SortPackages() middle elements = %v, want ['lib-a', 'lib-b'] in any order", gotNames[1:3])
				}
			} else if tc.isOrderStrict {
				// For all other cases, the order must be exactly as expected.
				if !reflect.DeepEqual(gotNames, tc.want) {
					t.Errorf("SortPackages() = %v, want %v", gotNames, tc.want)
				}
			}
		})
	}
}
