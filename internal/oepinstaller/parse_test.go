package oepinstaller

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var handheldModules = []string{"handheld_multi_modal"}

func readFixture(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "handheld_multi_modal.yaml"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	return string(data)
}

func TestParseFixture(t *testing.T) {
	t.Parallel()
	out, err := Parse([]byte(readFixture(t)), handheldModules, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if out.CLIVersion != "main" || !slices.Equal(out.Modules, handheldModules) || out.Arguments != nil ||
		out.CLIURL != "https://github.com/example/oep-installer-cli/blob/main/rendered/openedge-cli" {
		t.Errorf("header = %q %q %v %v", out.CLIVersion, out.CLIURL, out.Modules, out.Arguments)
	}
	if len(out.Packages) != 16 {
		t.Errorf("got %d packages, want 16: %+v", len(out.Packages), out.Packages)
	}
	want := []string{"xe.max_vfs=7", "xe.force_probe=*", "modprobe.blacklist=i915", "udmabuf.list_limit=8192"}
	if !slices.Equal(out.KernelCmdline, want) {
		t.Errorf("KernelCmdline = %v, want %v", out.KernelCmdline, want)
	}
	if len(out.Repositories) != 1 {
		t.Fatalf("got %d repositories, want 1", len(out.Repositories))
	}
	repo := out.Repositories[0]
	if repo.URI != "https://repositories.intel.com/gpu/ubuntu" || repo.Suite != "noble" || repo.Component != "unified" ||
		!slices.Equal(repo.Architectures, []string{"amd64"}) {
		t.Errorf("apt-source not parsed: %+v", repo)
	}
	if repo.Name != "repositories.intel.com" || repo.KeyFile != "/usr/share/keyrings/intel-graphics.gpg" ||
		repo.ListFile != "/etc/apt/sources.list.d/intel-graphics.list" || len(repo.Preferences) != 3 ||
		repo.PreferencesFile != "/etc/apt/preferences.d/intel-graphics.pref" {
		t.Errorf("repository fields not parsed: %+v", repo)
	}
}

func TestParseHeaderOnly(t *testing.T) {
	t.Parallel()
	data := "\"schema-version\": \"1\"\n\"cli-version\": \"v2026.3.0\"\n\"module-list\": [\"a\", \"b\"]\n---\n"
	out, err := Parse([]byte(data), []string{"a", "b"}, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(out.Packages) != 0 || len(out.Repositories) != 0 {
		t.Errorf("expected no packages or repositories, got %+v", out)
	}
}

func TestParseInstallCmdline(t *testing.T) {
	t.Parallel()
	const header = "\"schema-version\": \"1\"\n\"cli-version\": \"8d24c5b\"\n" +
		"\"module-list\": [\"handheld_blueprint\"]\n"
	modules := []string{"handheld_blueprint"}
	tests := []struct {
		name, cmdline string
		args          []string
		wantArgs      []string
		wantErr       string
	}{
		{"matching arguments", `["openedge-cli","install","handheld_blueprint","--edgepack"]`,
			[]string{"--edgepack"}, []string{"--edgepack"}, ""},
		{"no arguments", `["openedge-cli","install","handheld_blueprint"]`, nil, []string{}, ""},
		{"different arguments", `["openedge-cli","install","handheld_blueprint","--edgepack"]`,
			[]string{"--k3s"}, nil, "do not match"},
		{"template without arguments", `["openedge-cli","install","handheld_blueprint","--edgepack"]`,
			nil, nil, "do not match"},
		{"not an install command", `["openedge-cli","list","handheld_blueprint"]`, nil, nil, "must be the CLI"},
		{"other modules", `["openedge-cli","install","uav_blueprint"]`, nil, nil, "must be the CLI"},
		{"too short", `["openedge-cli"]`, nil, nil, "must be the CLI"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, err := Parse([]byte(header+"\"install-cmdline\": "+tt.cmdline+"\n"), modules, tt.args)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || !slices.Equal(out.Arguments, tt.wantArgs) || out.Arguments == nil {
				t.Fatalf("Parse = %+v, %v; want arguments %v", out, err, tt.wantArgs)
			}
		})
	}
}

func TestParseRejects(t *testing.T) {
	t.Parallel()
	const header = "\"schema-version\": \"1\"\n\"cli-version\": \"main\"\n\"module-list\": [\"handheld_multi_modal\"]\n"
	const repo = "\"gpg-key\": \"https://h/k.asc\"\n\"key-file\": \"/etc/apt/keyrings/k.gpg\"\n" +
		"\"apt-list-file\": \"/etc/apt/sources.list.d/k.list\"\n"
	tests := []struct {
		name, data, wantErr string
	}{
		{"empty output", "", "first document must be the header"},
		{"no header", "\"pkg-list\": [\"curl\"]\n", "first document must be the header"},
		{"second header", header + "---\n" + header, "only the first document may be a header"},
		{"wrong module list", strings.Replace(header, "handheld_multi_modal", "other", 1), "does not match"},
		{"unsupported schema version", strings.Replace(header, "\"1\"", "\"2\"", 1), "schema validation"},
		{"invalid YAML", header + "---\n\"pkg-list\": [\n", "invalid YAML"},
		{"package with a space", header + "---\n\"pkg-list\": [\"bad name\"]\n", "schema validation"},
		{"incomplete repository", header + "---\n\"repo-name\": \"r\"\n\"pkg-list\": [\"docker-ce\"]\n",
			"schema validation"},
		{"signed-by mismatch", header + "---\n" + repo +
			"\"apt-source\": \"deb [signed-by=/usr/share/keyrings/other.gpg] https://h/r noble main\"\n",
			"must equal key-file"},
		{"unsupported apt option", header + "---\n" + repo +
			"\"apt-source\": \"deb [trusted=yes signed-by=/etc/apt/keyrings/k.gpg] https://h/r noble main\"\n",
			"unsupported option"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse([]byte(tt.data), handheldModules, nil)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestClassifyPackage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		entry, name, version string
		kind                 PackageKind
	}{
		{"curl", "curl", "", PackageName},
		{"libtbb12=2021.11.0-2ubuntu2", "libtbb12", "2021.11.0-2ubuntu2", PackageExactVersion},
		{"intel-edge-base-standard=2026.3*", "intel-edge-base-standard", "2026.3", PackageVersionPrefix},
		{"intel-oneapi-runtime-*", "intel-oneapi-runtime-*", "", PackageGlob},
		{"https://example.com/a.deb", "", "", PackageURL},
		{"http://internal.example/b.tar.gz", "", "", PackageURL},
		{"-ubuntu-desktop", "ubuntu-desktop", "", PackageRemoval},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.entry, func(t *testing.T) {
			t.Parallel()
			got := classifyPackage(tt.entry)
			if got.Name != tt.name || got.Version != tt.version || got.Kind != tt.kind || got.Entry != tt.entry {
				t.Errorf("classifyPackage(%q) = %+v", tt.entry, got)
			}
		})
	}
}

func TestPackageIsKernel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		entry string
		want  bool
	}{
		{"linux-image-6.18-intel", true},
		{"linux-headers-6.18-intel=6.18.1-1", true},
		{"linux-modules-*", true},
		{"linux-generic-hwe-24.04", true},
		{"linux-firmware", false},
		{"-linux-image-generic", false},
		{"https://example.com/linux-image-x.deb", false},
		{"curl", false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.entry, func(t *testing.T) {
			t.Parallel()
			if got := classifyPackage(tt.entry).IsKernel(); got != tt.want {
				t.Errorf("IsKernel(%q) = %v, want %v", tt.entry, got, tt.want)
			}
		})
	}
}

func TestSplitAptSource(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, line string
		opts       map[string]string
		fields     []string
		wantErr    bool
	}{
		{"no options", "deb https://h/oneapi all main",
			map[string]string{}, []string{"https://h/oneapi", "all", "main"}, false},
		{"signed-by", "deb [signed-by=/k.gpg] https://h/r noble unified",
			map[string]string{"signed-by": "/k.gpg"}, []string{"https://h/r", "noble", "unified"}, false},
		{"not a deb line", "deb-src https://h/r noble main", nil, nil, true},
		{"unterminated options", "deb [signed-by=/k.gpg https://h/r noble main", nil, nil, true},
		{"unsupported option", "deb [trusted=yes] https://h/r noble main", nil, nil, true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts, fields, err := splitAptSource(tt.line)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(opts) != len(tt.opts) || opts["signed-by"] != tt.opts["signed-by"] || !slices.Equal(fields, tt.fields) {
				t.Errorf("splitAptSource(%q) = %v %v", tt.line, opts, fields)
			}
		})
	}
}

func TestPackageKernelRole(t *testing.T) {
	t.Parallel()
	tests := []struct {
		entry           string
		role            KernelRole
		release, series string
	}{
		{"linux-image-7.0.0-38-generic", KernelImage, "7.0.0-38-generic", ""},
		{"linux-image-6.18-intel", KernelImage, "6.18-intel", ""},
		{"linux-image-generic-7.0", KernelMeta, "", "7.0"},
		{"linux-image-generic", KernelMeta, "", ""},
		{"linux-image-generic-hwe-24.04", KernelMeta, "", ""},
		{"linux-generic-hwe-24.04", KernelMeta, "", ""},
		{"linux-headers-7.0.0-38-generic", KernelExtra, "7.0.0-38-generic", ""},
		{"linux-tools-7.0.0-38-generic", KernelExtra, "7.0.0-38-generic", ""},
		{"linux-modules-extra-7.0.0-38-generic", KernelExtra, "7.0.0-38-generic", ""},
		{"linux-headers-generic-7.0", KernelMeta, "", "7.0"},
		{"linux-tools-common", KernelNone, "", ""},
		{"linux-firmware", KernelNone, "", ""},
		{"-linux-image-generic", KernelNone, "", ""},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.entry, func(t *testing.T) {
			t.Parallel()
			p := classifyPackage(tt.entry)
			if p.KernelRole() != tt.role || p.KernelRelease() != tt.release || p.KernelSeries() != tt.series {
				t.Errorf("%q: role %d release %q series %q; want %d %q %q", tt.entry,
					p.KernelRole(), p.KernelRelease(), p.KernelSeries(), tt.role, tt.release, tt.series)
			}
		})
	}
}
