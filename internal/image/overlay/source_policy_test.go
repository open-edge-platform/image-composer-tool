package overlay

import (
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

func TestEvaluateSourceProvenancePolicy(t *testing.T) {
	t.Parallel()

	report := FinalInventoryReport{Packages: []ReconciledPackage{
		{Name: "linux-image-1", Version: "1", Arch: "amd64", SourceName: "linux", SourceVersion: "1", RepositoryID: "repo-a", Class: ReconciliationResolvedAndInstalled},
		{Name: "mesa-dri", Version: "26", Arch: "amd64", Class: ReconciliationBaselineRetained},
		{Name: "scripted", Version: "1", Arch: "amd64", Class: ReconciliationScriptInstalled, CoverageGap: true},
		{Name: "old", Version: "1", Arch: "amd64", Class: ReconciliationRemoved},
	}}

	tests := []struct {
		name         string
		policy       *config.SourceProvenancePolicy
		wantFailures int
		wantAbsent   int
		wantErr      bool
	}{
		{"nil is advisory", nil, 0, 0, false},
		{"advisory", &config.SourceProvenancePolicy{Mode: "advisory"}, 0, 0, false},
		{"strict all", &config.SourceProvenancePolicy{Mode: "strict-all"}, 2, 0, true},
		{"strict critical mesa", &config.SourceProvenancePolicy{Mode: "strict-critical", CriticalPackages: []string{"mesa-*"}}, 1, 0, true},
		{"strict critical absent", &config.SourceProvenancePolicy{Mode: "strict-critical", CriticalPackages: []string{"ffmpeg-*"}}, 0, 1, false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			policyReport, err := EvaluateSourceProvenancePolicy(tt.policy, report)
			if (err != nil) != tt.wantErr {
				t.Fatalf("EvaluateSourceProvenancePolicy error = %v, wantErr %v", err, tt.wantErr)
			}
			if len(policyReport.Failures) != tt.wantFailures || len(policyReport.Absent) != tt.wantAbsent {
				t.Errorf("policy report = %+v, want failures=%d absent=%d", policyReport, tt.wantFailures, tt.wantAbsent)
			}
		})
	}
}

func TestStrictCriticalSelectorNoInstalledMatch(t *testing.T) {
	t.Parallel()

	report := FinalInventoryReport{Packages: []ReconciledPackage{{Name: "curl", Version: "1", Arch: "amd64", Class: ReconciliationResolvedAndInstalled, SourceName: "curl", SourceVersion: "1", RepositoryID: "repo-a"}}}
	policy := &config.SourceProvenancePolicy{Mode: "strict-critical", CriticalPackages: []string{"linux-image-*", "mesa-*"}}
	policyReport, err := EvaluateSourceProvenancePolicy(policy, report)
	if err != nil {
		t.Fatalf("absent selectors should not fail: %v", err)
	}
	if len(policyReport.Absent) != 2 {
		t.Errorf("absent selectors = %v, want 2", policyReport.Absent)
	}
}
