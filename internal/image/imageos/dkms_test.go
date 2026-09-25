package imageos

import (
	"os"
	"path/filepath"
	"strconv"
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

func TestDkmsBuiltModuleNames(t *testing.T) {
	conf := `PACKAGE_NAME="edge-mei-dkms"
PACKAGE_VERSION="7.0"
BUILT_MODULE_NAME[0]="mei"
BUILT_MODULE_NAME[1]="mei-me"
BUILT_MODULE_NAME[2]="mei_hdcp"
DEST_MODULE_LOCATION[0]="/updates/dkms"
`
	got := dkmsBuiltModuleNames(conf)
	want := []string{"mei", "mei-me", "mei_hdcp"}
	if len(got) != len(want) {
		t.Fatalf("dkmsBuiltModuleNames = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("dkmsBuiltModuleNames[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	// Plain (non-indexed) form.
	if got := dkmsBuiltModuleNames(`BUILT_MODULE_NAME="igen6_edac"`); len(got) != 1 || got[0] != "igen6_edac" {
		t.Errorf("plain form: got %v, want [igen6_edac]", got)
	}
}

func TestKoModuleName(t *testing.T) {
	tests := map[string]string{
		"mei.ko":           "mei",
		"mei-me.ko":        "mei-me",
		"xe.ko.zst":        "xe",
		"virtio-gpu.ko.xz": "virtio-gpu",
		"igen6_edac.ko.gz": "igen6_edac",
	}
	for in, want := range tests {
		if got := koModuleName(in); got != want {
			t.Errorf("koModuleName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMissingBuiltModules(t *testing.T) {
	tests := []struct {
		name     string
		declared []string
		built    []string
		want     []string
	}{
		{
			name:     "all built (incl. built-not-installed)",
			declared: []string{"mei", "mei-me", "mei_hdcp"},
			built:    []string{"mei", "mei-me", "mei_hdcp"},
			want:     nil,
		},
		{
			name:     "one module failed to build",
			declared: []string{"mei", "mei-me", "mei_hdcp"},
			built:    []string{"mei", "mei-me"},
			want:     []string{"mei_hdcp"},
		},
		{
			name:     "dash/underscore spelling mismatch still matches",
			declared: []string{"mei-me"},
			built:    []string{"mei_me"},
			want:     nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := missingBuiltModules(tt.declared, tt.built)
			if len(got) != len(tt.want) {
				t.Fatalf("missingBuiltModules = %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("missingBuiltModules[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// makeDkmsSource lays out a fake source in an install root: its dkms.conf under
// /usr/src/<src>-<ver> declaring builtNames, and a compiled .ko under the
// per-kernel build tree for each name in koNames.
func makeDkmsSource(t *testing.T, root, src, ver, kernel string, builtNames, koNames []string) {
	t.Helper()
	srcDir := filepath.Join(root, "usr", "src", src+"-"+ver)
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	conf := ""
	for i, name := range builtNames {
		conf += "BUILT_MODULE_NAME[" + strconv.Itoa(i) + "]=\"" + name + "\"\n"
	}
	if err := os.WriteFile(filepath.Join(srcDir, "dkms.conf"), []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	modDir := filepath.Join(root, "var", "lib", "dkms", src, ver, kernel, "x86_64", "module")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, ko := range koNames {
		if err := os.WriteFile(filepath.Join(modDir, ko+".ko"), []byte("stub"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestVerifyDkmsModulesBuilt(t *testing.T) {
	kernel := "7.0.0-34-generic"

	t.Run("all declared modules built", func(t *testing.T) {
		root := t.TempDir()
		// Mirrors edge-mei-dkms: 3 modules built but only some installed - the
		// build tree is what matters, so all count as built.
		makeDkmsSource(t, root, "edge-mei-dkms", "7.0", kernel,
			[]string{"mei", "mei-me", "mei_hdcp"}, []string{"mei", "mei-me", "mei_hdcp"})
		makeDkmsSource(t, root, "edge-gfx-dkms", "7.0", kernel,
			[]string{"xe", "virtio-gpu"}, []string{"xe", "virtio-gpu"})
		if err := verifyDkmsModulesBuilt(root, kernel); err != nil {
			t.Fatalf("expected all modules built, got error: %v", err)
		}
	})

	t.Run("a declared module failed to build", func(t *testing.T) {
		root := t.TempDir()
		makeDkmsSource(t, root, "edge-mei-dkms", "7.0", kernel,
			[]string{"mei", "mei-me", "mei_hdcp"}, []string{"mei", "mei-me"}) // mei_hdcp missing
		if err := verifyDkmsModulesBuilt(root, kernel); err == nil {
			t.Fatal("expected an error when a declared module did not build")
		}
	})

	t.Run("no dkms sources is a no-op", func(t *testing.T) {
		if err := verifyDkmsModulesBuilt(t.TempDir(), kernel); err != nil {
			t.Fatalf("expected no error for empty dkms tree, got: %v", err)
		}
	})
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
