package config

import "testing"

func TestMergeSourceProvenancePartialOverride(t *testing.T) {
	t.Parallel()

	defaults := []PackageRepository{{
		Codename: "main", URL: "https://binary.example/repo", Component: "main",
		PKey:             "https://binary.example/key.gpg",
		SourceProvenance: &SourceProvenanceConfig{Enabled: true, Codename: "main", Component: "main"},
	}}
	users := []PackageRepository{{
		Codename: "main", URL: "https://binary.example/repo",
		SourceProvenance: &SourceProvenanceConfig{Enabled: true, Component: "restricted"},
	}}
	merged := mergePackageRepositories(defaults, users)
	if len(merged) != 1 || merged[0].SourceProvenance == nil {
		t.Fatalf("merged repositories = %+v", merged)
	}
	if merged[0].SourceProvenance.Enabled != true || merged[0].SourceProvenance.Codename != "main" || merged[0].SourceProvenance.Component != "restricted" {
		t.Errorf("merged source provenance = %+v", merged[0].SourceProvenance)
	}
}

func TestSourceProvenanceDisabledDoesNotRequireTrust(t *testing.T) {
	t.Parallel()

	repo := PackageRepository{Codename: "main", URL: "https://example.org/repo", SourceProvenance: &SourceProvenanceConfig{Enabled: false}}
	if err := repo.ValidatePackageRepository(); err != nil {
		t.Fatalf("disabled provenance should be accepted: %v", err)
	}
}
