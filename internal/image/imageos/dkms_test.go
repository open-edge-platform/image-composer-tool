package imageos

import (
	"testing"
)

func TestSplitDkmsModule(t *testing.T) {
	tests := []struct {
		name        string
		module      string
		wantName    string
		wantVersion string
		wantErr     bool
	}{
		{name: "valid", module: "edge-gfx/1.0", wantName: "edge-gfx", wantVersion: "1.0"},
		{name: "no slash", module: "edge-gfx", wantErr: true},
		{name: "empty name", module: "/1.0", wantErr: true},
		{name: "empty version", module: "edge-gfx/", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, version, err := splitDkmsModule(tt.module)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for module %q, got nil", tt.module)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for module %q: %v", tt.module, err)
			}
			if name != tt.wantName || version != tt.wantVersion {
				t.Errorf("splitDkmsModule(%q) = (%q, %q), want (%q, %q)",
					tt.module, name, version, tt.wantName, tt.wantVersion)
			}
		})
	}
}

func TestVerifyDkmsModulesInstalled(t *testing.T) {
	statusOutput := "edge-gfx/1.0, 6.14.0-generic, x86_64: installed\n" +
		"edge-edac/1.0, 6.14.0-generic, x86_64: installed\n" +
		"edge-issei/1.0, 6.14.0-generic, x86_64: added\n"

	tests := []struct {
		name     string
		expected []string
		wantErr  bool
	}{
		{name: "no assertion list, at least one installed", expected: nil, wantErr: false},
		{name: "all expected modules installed", expected: []string{"edge-gfx/1.0", "edge-edac/1.0"}, wantErr: false},
		{name: "an expected module is not installed", expected: []string{"edge-issei/1.0"}, wantErr: true},
		{name: "an expected module is missing entirely", expected: []string{"edge-npu/1.0"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifyDkmsModulesInstalled(statusOutput, tt.expected, "6.14.0-generic")
			if tt.wantErr && err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestVerifyDkmsModulesInstalled_NoneInstalled(t *testing.T) {
	// Nothing installed for the target kernel at all.
	statusOutput := "edge-gfx/1.0, 5.15.0-other, x86_64: installed\n"

	if err := verifyDkmsModulesInstalled(statusOutput, nil, "6.14.0-generic"); err == nil {
		t.Fatal("expected an error when no modules are installed for the target kernel")
	}
}

func TestKernelAbiFromVersion(t *testing.T) {
	tests := []struct {
		version string
		want    string
	}{
		{version: "6.14.0-29-generic", want: "6.14.0-29"},
		{version: "6.14.0-generic", want: "6.14.0"},
		{version: "noflavour", want: ""},
	}

	for _, tt := range tests {
		if got := kernelAbiFromVersion(tt.version); got != tt.want {
			t.Errorf("kernelAbiFromVersion(%q) = %q, want %q", tt.version, got, tt.want)
		}
	}
}

func TestChrootRelative(t *testing.T) {
	installRoot := "/var/lib/ict/workspace/root"

	got, err := chrootRelative(installRoot+"/lib/modules/6.14.0-generic/updates/dkms/edge_gfx.ko", installRoot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "/lib/modules/6.14.0-generic/updates/dkms/edge_gfx.ko"
	if got != want {
		t.Errorf("chrootRelative() = %q, want %q", got, want)
	}

	if _, err := chrootRelative("/etc/passwd", installRoot); err == nil {
		t.Fatal("expected an error for a path outside installRoot")
	}
}
