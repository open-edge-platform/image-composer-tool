package config

import "testing"

func TestEffectiveSourceRepository(t *testing.T) {
	t.Parallel()

	repo := PackageRepository{
		Codename: "resolute", URL: "https://binary.example/snapshot", Component: "main restricted",
		PKey:             "https://binary.example/key.gpg",
		SourceProvenance: &SourceProvenanceConfig{Enabled: true},
	}
	effective := repo.EffectiveSourceRepository()
	if effective.URL != repo.URL || effective.Codename != repo.Codename || effective.Component != repo.Component || effective.PKey != repo.PKey {
		t.Errorf("effective repository = %+v, want binary repository inheritance", effective)
	}

	repo.SourceProvenance = &SourceProvenanceConfig{
		Enabled: true, URL: "https://source.example/snapshot", Codename: "other", Component: "sources", PKey: "https://source.example/key.gpg",
	}
	effective = repo.EffectiveSourceRepository()
	if effective.URL != "https://source.example/snapshot" || effective.Codename != "other" || effective.Component != "sources" || effective.PKey != "https://source.example/key.gpg" {
		t.Errorf("effective override = %+v", effective)
	}

	repo.SourceProvenance = &SourceProvenanceConfig{Enabled: true, URL: "https://source.example/snapshot"}
	effective = repo.EffectiveSourceRepository()
	if effective.PKey != "" || len(effective.PKeys) != 0 {
		t.Errorf("different-origin repository inherited signing keys: %+v", effective)
	}
}

func TestValidateSourceProvenancePolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		policy  *SourceProvenancePolicy
		wantErr bool
	}{
		{"nil", nil, false},
		{"advisory", &SourceProvenancePolicy{Mode: "advisory"}, false},
		{"strict all", &SourceProvenancePolicy{Mode: "strict-all"}, false},
		{"strict critical", &SourceProvenancePolicy{Mode: "strict-critical", CriticalPackages: []string{"linux-image-*", "mesa-*"}}, false},
		{"unknown mode", &SourceProvenancePolicy{Mode: "strict-unknown"}, true},
		{"missing critical", &SourceProvenancePolicy{Mode: "strict-critical"}, true},
		{"empty critical", &SourceProvenancePolicy{Mode: "strict-critical", CriticalPackages: []string{" "}}, true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			template := &ImageTemplate{SourceProvenancePolicy: tt.policy}
			err := template.validateSourceProvenancePolicy()
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateSourceProvenancePolicy = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateSourceProvenanceRPMTarget(t *testing.T) {
	t.Parallel()

	template := &ImageTemplate{
		Target:              TargetInfo{OS: "azure-linux"},
		PackageRepositories: []PackageRepository{{Codename: "main", URL: "https://example.org/repo", PKey: "https://example.org/key.gpg", SourceProvenance: &SourceProvenanceConfig{Enabled: true}}},
	}
	if err := template.validateSourceProvenanceTarget(); err == nil {
		t.Fatal("RPM target accepted source provenance, want rejection")
	}
}

func TestValidateSourceProvenanceRepository(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		repo    PackageRepository
		wantErr bool
	}{
		{"disabled", PackageRepository{Codename: "main", URL: "https://example.org/repo", PKey: "https://example.org/key.gpg", SourceProvenance: &SourceProvenanceConfig{Enabled: false}}, false},
		{"enabled inherited", PackageRepository{Codename: "main", URL: "https://example.org/repo", PKey: "https://example.org/key.gpg", SourceProvenance: &SourceProvenanceConfig{Enabled: true}}, false},
		{"local path", PackageRepository{Codename: "main", Path: "/srv/repo", SourceProvenance: &SourceProvenanceConfig{Enabled: true}}, true},
		{"http URL", PackageRepository{Codename: "main", URL: "http://example.org/repo", PKey: "https://example.org/key.gpg", SourceProvenance: &SourceProvenanceConfig{Enabled: true}}, true},
		{"credential URL", PackageRepository{Codename: "main", URL: "https://user:secret@example.org/repo", PKey: "https://example.org/key.gpg", SourceProvenance: &SourceProvenanceConfig{Enabled: true}}, true},
		{"missing trust", PackageRepository{Codename: "main", URL: "https://example.org/repo", SourceProvenance: &SourceProvenanceConfig{Enabled: true}}, true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.repo.ValidatePackageRepository()
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidatePackageRepository = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
