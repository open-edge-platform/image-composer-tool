package overlay

import (
	"testing"
)

func TestComputePackageStats(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		baseline []BaselinePackage
		plan     *ResolutionPlan
		want     *PackageStats
	}{
		{
			name:     "nil plan",
			baseline: []BaselinePackage{{Name: "pkg1", Installed: true}},
			plan:     nil,
			want:     &PackageStats{},
		},
		{
			name:     "empty baseline and plan",
			baseline: []BaselinePackage{},
			plan:     &ResolutionPlan{ToInstall: []ResolvedPackage{}},
			want: &PackageStats{
				TotalBaseline: 0,
				Unchanged:     []string{},
				Added:         []PackageAdd{},
				Upgraded:      []PackageUpgrade{},
			},
		},
		{
			name: "only new packages added",
			baseline: []BaselinePackage{
				{Name: "base1", Version: "1.0", Installed: true},
				{Name: "base2", Version: "2.0", Installed: true},
			},
			plan: &ResolutionPlan{
				ToInstall: []ResolvedPackage{
					{Name: "new1", Version: "1.0"},
					{Name: "new2", Version: "2.0"},
				},
			},
			want: &PackageStats{
				TotalBaseline: 2,
				Unchanged:     []string{"base1", "base2"},
				Added:         []PackageAdd{{Name: "new1", Version: "1.0"}, {Name: "new2", Version: "2.0"}},
				Upgraded:      []PackageUpgrade{},
			},
		},
		{
			name: "packages upgraded",
			baseline: []BaselinePackage{
				{Name: "pkg1", Version: "1.0", Installed: true},
				{Name: "pkg2", Version: "2.0", Installed: true},
				{Name: "pkg3", Version: "3.0", Installed: true},
			},
			plan: &ResolutionPlan{
				ToInstall: []ResolvedPackage{
					{Name: "pkg1", Version: "1.5"},
					{Name: "pkg2", Version: "2.5"},
				},
			},
			want: &PackageStats{
				TotalBaseline: 3,
				Unchanged:     []string{"pkg3"},
				Added:         []PackageAdd{},
				Upgraded: []PackageUpgrade{
					{Name: "pkg1", BaselineVersion: "1.0", NewVersion: "1.5"},
					{Name: "pkg2", BaselineVersion: "2.0", NewVersion: "2.5"},
				},
			},
		},
		{
			name: "mixed: added, upgraded, unchanged",
			baseline: []BaselinePackage{
				{Name: "unchanged1", Version: "1.0", Installed: true},
				{Name: "upgraded1", Version: "1.0", Installed: true},
				{Name: "unchanged2", Version: "2.0", Installed: true},
			},
			plan: &ResolutionPlan{
				ToInstall: []ResolvedPackage{
					{Name: "upgraded1", Version: "2.0"},
					{Name: "newpkg1", Version: "1.0"},
					{Name: "newpkg2", Version: "2.0"},
				},
			},
			want: &PackageStats{
				TotalBaseline: 3,
				Unchanged:     []string{"unchanged1", "unchanged2"},
				Added:         []PackageAdd{{Name: "newpkg1", Version: "1.0"}, {Name: "newpkg2", Version: "2.0"}},
				Upgraded: []PackageUpgrade{
					{Name: "upgraded1", BaselineVersion: "1.0", NewVersion: "2.0"},
				},
			},
		},
		{
			name: "same-version reinstall counts as unchanged, not upgraded",
			baseline: []BaselinePackage{
				{Name: "pkg1", Version: "1.0", Installed: true},
				{Name: "pkg2", Version: "2.0", Installed: true},
			},
			plan: &ResolutionPlan{
				ToInstall: []ResolvedPackage{
					// Same version as baseline: a no-op reinstall.
					{Name: "pkg1", Version: "1.0"},
					// Genuine upgrade.
					{Name: "pkg2", Version: "2.5"},
				},
			},
			want: &PackageStats{
				TotalBaseline: 2,
				Unchanged:     []string{"pkg1"},
				Added:         []PackageAdd{},
				Upgraded: []PackageUpgrade{
					{Name: "pkg2", BaselineVersion: "2.0", NewVersion: "2.5"},
				},
			},
		},
		{
			name: "uninstalled baseline packages ignored",
			baseline: []BaselinePackage{
				{Name: "installed1", Version: "1.0", Installed: true},
				{Name: "notinstalled", Version: "1.0", Installed: false},
				{Name: "installed2", Version: "2.0", Installed: true},
			},
			plan: &ResolutionPlan{
				ToInstall: []ResolvedPackage{
					{Name: "newpkg", Version: "1.0"},
				},
			},
			want: &PackageStats{
				TotalBaseline: 2, // only installed packages
				Unchanged:     []string{"installed1", "installed2"},
				Added:         []PackageAdd{{Name: "newpkg", Version: "1.0"}},
				Upgraded:      []PackageUpgrade{},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ComputePackageStats(tt.baseline, tt.plan)

			if got.TotalBaseline != tt.want.TotalBaseline {
				t.Errorf("TotalBaseline = %d, want %d", got.TotalBaseline, tt.want.TotalBaseline)
			}

			if len(got.Unchanged) != len(tt.want.Unchanged) {
				t.Errorf("len(Unchanged) = %d, want %d", len(got.Unchanged), len(tt.want.Unchanged))
			} else {
				for i := range got.Unchanged {
					if got.Unchanged[i] != tt.want.Unchanged[i] {
						t.Errorf("Unchanged[%d] = %q, want %q", i, got.Unchanged[i], tt.want.Unchanged[i])
					}
				}
			}

			if len(got.Added) != len(tt.want.Added) {
				t.Errorf("len(Added) = %d, want %d", len(got.Added), len(tt.want.Added))
			} else {
				for i := range got.Added {
					if got.Added[i].Name != tt.want.Added[i].Name {
						t.Errorf("Added[%d].Name = %q, want %q", i, got.Added[i].Name, tt.want.Added[i].Name)
					}
					if got.Added[i].Version != tt.want.Added[i].Version {
						t.Errorf("Added[%d].Version = %q, want %q", i, got.Added[i].Version, tt.want.Added[i].Version)
					}
				}
			}

			if len(got.Upgraded) != len(tt.want.Upgraded) {
				t.Errorf("len(Upgraded) = %d, want %d", len(got.Upgraded), len(tt.want.Upgraded))
			} else {
				for i := range got.Upgraded {
					if got.Upgraded[i].Name != tt.want.Upgraded[i].Name {
						t.Errorf("Upgraded[%d].Name = %q, want %q", i, got.Upgraded[i].Name, tt.want.Upgraded[i].Name)
					}
					if got.Upgraded[i].BaselineVersion != tt.want.Upgraded[i].BaselineVersion {
						t.Errorf("Upgraded[%d].BaselineVersion = %q, want %q", i, got.Upgraded[i].BaselineVersion, tt.want.Upgraded[i].BaselineVersion)
					}
					if got.Upgraded[i].NewVersion != tt.want.Upgraded[i].NewVersion {
						t.Errorf("Upgraded[%d].NewVersion = %q, want %q", i, got.Upgraded[i].NewVersion, tt.want.Upgraded[i].NewVersion)
					}
				}
			}
		})
	}
}

func TestPrintPackageStats(t *testing.T) {
	// This test just ensures PrintPackageStats doesn't panic
	tests := []struct {
		name  string
		stats *PackageStats
	}{
		{
			name:  "nil stats",
			stats: nil,
		},
		{
			name: "empty stats",
			stats: &PackageStats{
				TotalBaseline: 0,
				Unchanged:     []string{},
				Added:         []PackageAdd{},
				Upgraded:      []PackageUpgrade{},
			},
		},
		{
			name: "populated stats",
			stats: &PackageStats{
				TotalBaseline: 10,
				Unchanged:     []string{"pkg1", "pkg2"},
				Added:         []PackageAdd{{Name: "newpkg1", Version: "3.0"}},
				Upgraded: []PackageUpgrade{
					{Name: "pkg3", BaselineVersion: "1.0", NewVersion: "2.0"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Just ensure it doesn't panic
			PrintPackageStats(tt.stats)
		})
	}
}

func TestPrintVerboseUnchangedPackages(t *testing.T) {
	// This test just ensures PrintVerboseUnchangedPackages doesn't panic
	tests := []struct {
		name  string
		stats *PackageStats
	}{
		{
			name:  "nil stats",
			stats: nil,
		},
		{
			name: "empty unchanged",
			stats: &PackageStats{
				Unchanged: []string{},
			},
		},
		{
			name: "with unchanged packages",
			stats: &PackageStats{
				Unchanged: []string{"pkg1", "pkg2", "pkg3"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Just ensure it doesn't panic
			PrintVerboseUnchangedPackages(tt.stats)
		})
	}
}
