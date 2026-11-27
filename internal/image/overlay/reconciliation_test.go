package overlay

import "testing"

func TestReconcileFinalInventory(t *testing.T) {
	t.Parallel()

	baseline := []BaselinePackage{
		{Name: "retained", Version: "1", Arch: "amd64", MultiArch: "allowed", Installed: true,
			SourceName: "retained-src", SourceVersion: "1"},
		{Name: "removed", Version: "1", Arch: "amd64", Installed: true,
			SourceName: "removed-src", SourceVersion: "1"},
		{Name: "libmulti", Version: "1", Arch: "amd64", MultiArch: "same", Installed: true},
		{Name: "libmulti", Version: "1", Arch: "i386", MultiArch: "same", Installed: true},
	}
	final := []BaselinePackage{
		{Name: "retained", Version: "1", Arch: "amd64", MultiArch: "allowed", Installed: true,
			SourceName: "retained-src", SourceVersion: "1"},
		{Name: "libmulti", Version: "1", Arch: "amd64", MultiArch: "same", Installed: true},
		{Name: "libmulti", Version: "1", Arch: "i386", MultiArch: "same", Installed: true},
		{Name: "upgraded", Version: "2", Arch: "amd64", Installed: true,
			SourceName: "dpkg-source", SourceVersion: "2-dpkg"},
		{Name: "scripted", Version: "1", Arch: "amd64", Installed: true,
			SourceName: "script-dpkg-field", SourceVersion: "1"},
	}
	plan := &ResolutionPlan{ToInstall: []ResolvedPackage{
		{Name: "upgraded", Version: "2", Arch: "amd64", SourceName: "vendor-upgrade", SourceVersion: "2-source", SourceRepository: "repo-a"},
	}}

	report := ReconcileFinalInventory(baseline, final, plan)
	got := make(map[string]ReconciledPackage)
	for _, pkg := range report.Packages {
		got[pkg.Name+":"+pkg.Arch+":"+pkg.Version+":"+string(pkg.Class)] = pkg
	}

	retained := got["retained:amd64:1:"+string(ReconciliationBaselineRetained)]
	if retained.SourceName != "retained-src" || retained.SourceVersion != "1" {
		t.Errorf("retained package = %+v, want dpkg source identity", retained)
	}
	removed := got["removed:amd64:1:"+string(ReconciliationRemoved)]
	if removed.SourceName != "removed-src" {
		t.Errorf("removed package = %+v, want retained source identity", removed)
	}
	upgraded := got["upgraded:amd64:2:"+string(ReconciliationResolvedAndInstalled)]
	if upgraded.SourceName != "vendor-upgrade" || upgraded.SourceVersion != "2-source" || upgraded.RepositoryID != "repo-a" {
		t.Errorf("resolved package = %+v, want exact source repository mapping", upgraded)
	}
	for _, arch := range []string{"amd64", "i386"} {
		if _, found := got["libmulti:"+arch+":1:"+string(ReconciliationBaselineRetained)]; !found {
			t.Errorf("missing baseline multiarch entry for %s", arch)
		}
	}
	scripted := got["scripted:amd64:1:"+string(ReconciliationScriptInstalled)]
	if scripted.Class != ReconciliationScriptInstalled || !scripted.CoverageGap || scripted.SourceName != "script-dpkg-field" {
		t.Errorf("script package = %+v, want origin unknown coverage gap", scripted)
	}
	if !report.OriginUnknownGap() {
		t.Error("report should expose an origin-unknown coverage gap")
	}
}

func TestReconcileFinalInventoryOverlayAddedAndRemoved(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		baseline  []BaselinePackage
		final     []BaselinePackage
		plan      *ResolutionPlan
		wantClass ReconciliationClass
	}{
		{
			name:      "overlay added",
			final:     []BaselinePackage{{Name: "added", Version: "1", Arch: "amd64", Installed: true}},
			plan:      &ResolutionPlan{ToInstall: []ResolvedPackage{{Name: "added", Version: "1", Arch: "amd64"}}},
			wantClass: ReconciliationResolvedAndInstalled,
		},
		{
			name:      "baseline removed",
			baseline:  []BaselinePackage{{Name: "old", Version: "1", Arch: "amd64", Installed: true}},
			wantClass: ReconciliationRemoved,
		},
		{
			name:      "script local package",
			final:     []BaselinePackage{{Name: "local", Version: "1", Arch: "amd64", Installed: true}},
			wantClass: ReconciliationScriptInstalled,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			report := ReconcileFinalInventory(tt.baseline, tt.final, tt.plan)
			if len(report.Packages) != 1 || report.Packages[0].Class != tt.wantClass {
				t.Fatalf("report = %+v, want class %q", report.Packages, tt.wantClass)
			}
		})
	}
}
