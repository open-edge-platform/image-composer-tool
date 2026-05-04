package version

import "testing"

func TestBuildMetadataDefaults(t *testing.T) {
	if Version == "" {
		t.Fatal("Version must not be empty")
	}
	if Toolname == "" {
		t.Fatal("Toolname must not be empty")
	}
	if Organization == "" {
		t.Fatal("Organization must not be empty")
	}
	if BuildDate == "" {
		t.Fatal("BuildDate must not be empty")
	}
	if CommitSHA == "" {
		t.Fatal("CommitSHA must not be empty")
	}

	if Version != "0.1.0" {
		t.Errorf("Version: got %q, want %q", Version, "0.1.0")
	}
	if Toolname != "Image-Composer-Tool-dev" {
		t.Errorf("Toolname: got %q, want %q", Toolname, "Image-Composer-Tool-dev")
	}
	if Organization != "unknown" {
		t.Errorf("Organization: got %q, want %q", Organization, "unknown")
	}
	if BuildDate != "unknown" {
		t.Errorf("BuildDate: got %q, want %q", BuildDate, "unknown")
	}
	if CommitSHA != "unknown" {
		t.Errorf("CommitSHA: got %q, want %q", CommitSHA, "unknown")
	}
}
