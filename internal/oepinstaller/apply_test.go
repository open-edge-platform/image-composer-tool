package oepinstaller

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

const handheldCmdline = "console=tty0 xe.max_vfs=7 xe.force_probe=* modprobe.blacklist=i915 udmabuf.list_limit=8192"

func parseBlueprintFixture(t *testing.T) *Output {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "handheld_blueprint.yaml"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	out, err := Parse(data, []string{"handheld_blueprint"}, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return out
}

func blueprintTemplate() *config.ImageTemplate {
	return &config.ImageTemplate{
		PackageRepositories: []config.PackageRepository{
			{Codename: "noble", URL: "http://edge.example.com/edgepack-test/", Component: "unstable"},
			{Codename: "noble", URL: "https://plain.example.com/repo", Component: "main"},
		},
		SystemConfig: config.SystemConfig{
			Packages: []string{"curl", "intel-edge-base-standard"},
			Kernel:   config.KernelConfig{Cmdline: handheldCmdline},
		},
	}
}

func TestApply(t *testing.T) {
	t.Parallel()
	tmpl := blueprintTemplate()
	if err := Apply(tmpl, parseBlueprintFixture(t)); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := []string{"curl", "intel-edge-base-standard_1.0*", "ca-certificates", "gcc-13", "g++-13",
		"linux-headers-7.0.0-38-generic", "linux-tools-common", "linux-tools-7.0.0-38-generic", "intel-edge-npu_1.0*",
		"ubuntu-server"}
	if got := tmpl.SystemConfig.Packages; !slices.Equal(got, want) {
		t.Errorf("packages = %v\nwant       %v", got, want)
	}
	repos := tmpl.PackageRepositories
	if len(repos) != 3 || repos[0].URL != "https://plain.example.com/repo" {
		t.Fatalf("repositories = %+v", repos)
	}
	ppa, edge := repos[1], repos[2]
	if ppa.ListFile != "/etc/apt/sources.list.d/kobuk-ppa.list" || ppa.Codename != "noble" || ppa.Component != "main" ||
		ppa.Priority != 0 || ppa.Preferences != "" {
		t.Errorf("PPA repository = %+v", ppa)
	}
	if edge.URL != "http://edge.example.com/edgepack-test" || edge.Component != "unstable" || edge.Priority != 1001 ||
		edge.KeyFile != "/etc/apt/keyrings/intel-edge-pack.gpg" ||
		edge.PKey != "http://edge.example.com/edgepack-test-publickey.asc" ||
		edge.PreferencesFile != "/etc/apt/preferences.d/intel-edge-pack.pref" {
		t.Errorf("EdgePack repository = %+v", edge)
	}
	wantPref := "Package: *\nPin: origin edge.example.com\nPin-Priority: 1001\n\n" +
		"Package: intel-edge-npu\nPin: version 1.0*\nPin-Priority: 1002\n"
	if edge.Preferences != wantPref {
		t.Errorf("EdgePack preferences = %q", edge.Preferences)
	}
}

func TestApplyRejects(t *testing.T) {
	t.Parallel()
	repo := Repository{GPGKey: "https://k", KeyFile: "/etc/apt/keyrings/k.gpg", ListFile: "/etc/apt/sources.list.d/k.list",
		Source: "deb [signed-by=/etc/apt/keyrings/k.gpg] https://h/a noble main", URI: "https://h/a", Suite: "noble",
		Component: "main"}
	other := repo
	other.Source, other.URI = "deb [signed-by=/etc/apt/keyrings/k.gpg] https://h/b noble main", "https://h/b"
	tests := []struct {
		name    string
		modify  func(*config.ImageTemplate)
		out     *Output
		wantErr string
	}{
		{"missing kernel parameter", func(t *config.ImageTemplate) { t.SystemConfig.Kernel.Cmdline = "quiet" },
			&Output{KernelCmdline: []string{"xe.max_vfs=7"}}, "xe.max_vfs=7"},
		{"removal of a template package",
			func(t *config.ImageTemplate) { t.SystemConfig.Packages = []string{"ubuntu-desktop"} },
			&Output{Packages: []Package{classifyPackage("-ubuntu-desktop")}}, "not to be installed"},
		{"incompatible versions", func(t *config.ImageTemplate) { t.SystemConfig.Packages = []string{"libtbb12_2021.5-1"} },
			&Output{Packages: []Package{classifyPackage("libtbb12=2021.11.0-2")}}, "incompatible versions"},
		{"conflicting repositories", func(*config.ImageTemplate) {},
			&Output{Repositories: []Repository{repo, other}}, "two different repositories"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmpl := blueprintTemplate()
			tt.modify(tmpl)
			err := Apply(tmpl, tt.out)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestCombineVersions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		a, b, want string
		ok         bool
	}{
		{"curl", "curl", "curl", true},
		{"pkg", "pkg_1.0*", "pkg_1.0*", true},
		{"pkg_1.0.2-1", "pkg", "pkg_1.0.2-1", true},
		{"pkg_1.0.2-1", "pkg_1.0*", "pkg_1.0.2-1", true},
		{"pkg_1:1.0.2-1", "pkg_1.0*", "pkg_1:1.0.2-1", true},
		{"pkg_1.0*", "pkg_1.0.2-1", "pkg_1.0.2-1", true},
		{"pkg_1.0*", "pkg_1.0.3*", "pkg_1.0.3*", true},
		{"pkg_1.0.2-1", "pkg_1.0.3-1", "", false},
		{"pkg_2.0.1-1", "pkg_1.0*", "", false},
		{"pkg_1.0*", "pkg_2.0*", "", false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.a+"+"+tt.b, func(t *testing.T) {
			t.Parallel()
			got, ok := combineVersions(tt.a, tt.b)
			if ok != tt.ok || (ok && got != tt.want) {
				t.Errorf("combineVersions(%q, %q) = %q, %v; want %q, %v", tt.a, tt.b, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestCheckKernel(t *testing.T) {
	t.Parallel()
	pinned := []string{"linux-image-7.0.0-38-generic"}
	tests := []struct {
		name    string
		kernel  []string
		entries []string
		wantErr string
	}{
		{"published handheld entries", pinned,
			[]string{"linux-image-generic-7.0", "linux-headers-7.0.0-38-generic", "linux-tools-7.0.0-38-generic",
				"linux-tools-common", "curl"}, ""},
		{"exact image", pinned, []string{"linux-image-7.0.0-38-generic"}, ""},
		{"meta-package without a series", pinned, []string{"linux-generic-hwe-24.04"}, ""},
		{"headers for another release", pinned, []string{"linux-headers-7.0.0-40-generic"}, "is for kernel 7.0.0-40"},
		{"another kernel image", pinned, []string{"linux-image-6.18-intel"}, "not a kernel package of the template"},
		{"meta-package of another series", pinned, []string{"linux-image-generic-6.8"}, "needs a 6.8.x kernel"},
		{"template without a kernel", nil, []string{"linux-image-generic"}, "needs a matching kernel"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out := &Output{}
			for _, e := range tt.entries {
				out.Packages = append(out.Packages, classifyPackage(e))
			}
			err := CheckKernel(out, tt.kernel)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}
