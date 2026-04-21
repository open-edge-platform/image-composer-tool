package resolvertest

import (
	"reflect"
	"sort"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
)

// Resolver is interface both rpmutil & debutil satisfy.
type Resolver interface {
	Resolve(
		requested []ospackage.PackageInfo,
		all []ospackage.PackageInfo,
	) ([]ospackage.PackageInfo, error)
}

// helper to extract and sort names from PackageInfo slice
func names(ps []ospackage.PackageInfo) []string {
	var outs []string
	for _, p := range ps {
		outs = append(outs, p.Name)
	}
	sort.Strings(outs)
	return outs
}

var TestCases = []struct {
	Name      string
	Requested []ospackage.PackageInfo
	All       []ospackage.PackageInfo
	Want      []string
	WantErr   bool
}{
	{
		Name: "SimpleChain",
		All: []ospackage.PackageInfo{
			{Name: "C", Version: "1.0.0", URL: "https://repo.example.com/rpm/C-1.0.0-1.el9.x86_64.rpm", Provides: []string{"C"}, Requires: []string{}, RequiresVer: []string{}},
			{Name: "B", Version: "2.1.0", URL: "https://repo.example.com/rpm/B-2.1.0-1.el9.x86_64.rpm", Provides: []string{"B"}, Requires: []string{"C"}, RequiresVer: []string{"C >= 1.0.0"}},
			{Name: "A", Version: "3.2.1", URL: "https://repo.example.com/rpm/A-3.2.1-1.el9.x86_64.rpm", Provides: []string{"A"}, Requires: []string{"B"}, RequiresVer: []string{"B >= 2.0.0"}},
		},
		Requested: []ospackage.PackageInfo{
			{Name: "A", Version: "3.2.1", URL: "https://repo.example.com/rpm/A-3.2.1-1.el9.x86_64.rpm", Provides: []string{"A"}, Requires: []string{"B"}, RequiresVer: []string{"B >= 2.0.0"}},
		},
		Want:    []string{"A", "B", "C"},
		WantErr: false,
	},
	{
		Name: "MultipleProviders",
		All: []ospackage.PackageInfo{
			{Name: "Y", Version: "1.5.0", URL: "https://repo.example.com/rpm/Y-1.5.0-1.el9.x86_64.rpm", Provides: []string{"Y"}, Requires: []string{}, RequiresVer: []string{}},
			{Name: "P1", Version: "1.0.0", URL: "https://repo.example.com/rpm/P1-1.0.0-1.el9.x86_64.rpm", Provides: []string{"X"}, Requires: []string{}, RequiresVer: []string{}},
			{Name: "P2", Version: "2.0.0", URL: "https://repo.example.com/rpm/P2-2.0.0-1.el9.x86_64.rpm", Provides: []string{"X"}, Requires: []string{"Y"}, RequiresVer: []string{"Y >= 1.0.0"}},
			{Name: "A", Version: "1.2.3", URL: "https://repo.example.com/rpm/A-1.2.3-1.el9.x86_64.rpm", Provides: []string{"A"}, Requires: []string{"X"}, RequiresVer: []string{"X >= 1.0.0"}},
		},
		Requested: []ospackage.PackageInfo{
			{Name: "A", Version: "1.2.3", URL: "https://repo.example.com/rpm/A-1.2.3-1.el9.x86_64.rpm", Provides: []string{"A"}, Requires: []string{"X"}, RequiresVer: []string{"X >= 1.0.0"}},
		},
		Want:    []string{"A", "P2", "Y"},
		WantErr: false,
	},
	{
		Name: "NoDependencies",
		All: []ospackage.PackageInfo{
			{Name: "X", Version: "1.0.0", URL: "https://repo.example.com/rpm/X-1.0.0-1.el9.x86_64.rpm", Provides: []string{"X"}, Requires: []string{}, RequiresVer: []string{}},
		},
		Requested: []ospackage.PackageInfo{
			{Name: "X", Version: "1.0.0", URL: "https://repo.example.com/rpm/X-1.0.0-1.el9.x86_64.rpm", Provides: []string{"A"}, Requires: []string{"X"}, RequiresVer: []string{"X >= 1.0.0"}},
		},
		Want:    []string{"X"},
		WantErr: false,
	},
	{
		Name: "MissingRequested",
		All: []ospackage.PackageInfo{
			{Name: "A", Version: "1.0.0", URL: "https://repo.example.com/rpm/A-1.0.0-1.el9.x86_64.rpm", Provides: []string{"A"}, Requires: []string{}, RequiresVer: []string{}},
		},
		Requested: []ospackage.PackageInfo{
			{Name: "B", Version: "1.0.0", URL: "https://repo.example.com/rpm/B-1.0.0-1.el9.x86_64.rpm", Provides: []string{"B"}, Requires: []string{""}, RequiresVer: []string{""}},
		},
		Want:    []string{},
		WantErr: true,
	},
}

// RunResolverTestsFunc drives a bare function through your table.
func RunResolverTestsFunc(
	t *testing.T,
	prefix string,
	resolverFunc func(requested, all []ospackage.PackageInfo) ([]ospackage.PackageInfo, error),
) {

	t.Helper()
	for _, tc := range TestCases {
		t.Run(prefix+"/"+tc.Name, func(t *testing.T) {
			got, err := resolverFunc(tc.Requested, tc.All)
			if (err != nil) != tc.WantErr {
				t.Fatalf("err = %v, wantErr? %v", err, tc.WantErr)
			}

			if !tc.WantErr && !reflect.DeepEqual(names(got), tc.Want) {
				t.Errorf("ResolvePackageInfos [%v] = %v; want %v", tc.Name, names(got), tc.Want)
			}
		})
	}
}
