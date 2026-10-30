package config

import (
	"slices"
	"strings"
	"testing"
)

const (
	testOEPSHA256     = "3f1c0e5a9b7d2c4e6f8a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0de9"
	testOEPSBOMSHA256 = "a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
	testOEPSBOMURL    = "https://raw.githubusercontent.com/example/oep-installer-cli/0123abc/rendered/" +
		"sbom-handheld-blueprint-ubuntu-24.04.5.yaml"
)

const oepInstallerTemplateYAML = `image:
  name: handheld
  version: "1.0"
target:
  os: ubuntu
  dist: ubuntu24
  arch: x86_64
  imageType: iso
systemConfig:
  name: handheld
  users:
    - name: edge
  oepInstaller:
    url: https://raw.githubusercontent.com/example/oep-installer-cli/0123abc/rendered/openedge-cli
    sha256: ` + testOEPSHA256 + `
    sbomUrl: ` + testOEPSBOMURL + `
    sbomSha256: ` + testOEPSBOMSHA256 + `
    modules:
      - handheld_multi_modal
    user: edge
    args: ["--gpu"]
`

func validOEPInstaller() OEPInstallerConfig {
	return OEPInstallerConfig{
		URL:        "https://raw.githubusercontent.com/example/oep-installer-cli/0123abc/rendered/openedge-cli",
		SHA256:     testOEPSHA256,
		SBOMURL:    testOEPSBOMURL,
		SBOMSHA256: testOEPSBOMSHA256,
		Modules:    []string{"handheld_multi_modal"},
		User:       "edge",
	}
}

func TestParseOEPInstaller(t *testing.T) {
	t.Parallel()
	tmpl, err := parseYAMLTemplate([]byte(oepInstallerTemplateYAML), false)
	if err != nil {
		t.Fatalf("parseYAMLTemplate: %v", err)
	}
	o := tmpl.SystemConfig.OEPInstaller
	if o.SHA256 != testOEPSHA256 || o.SBOMURL != testOEPSBOMURL || o.SBOMSHA256 != testOEPSBOMSHA256 ||
		o.User != "edge" || !slices.Equal(o.Modules, []string{"handheld_multi_modal"}) ||
		!slices.Equal(o.Args, []string{"--gpu"}) || !strings.HasSuffix(o.URL, "/rendered/openedge-cli") {
		t.Errorf("oepInstaller not parsed: %+v", o)
	}
}

func TestOEPInstallerSchemaRejects(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, find, replace string }{
		{"unknown field", "user: edge", "owner: edge"},
		{"http url", "url: https://", "url: http://"},
		{"short sha256", "sha256: " + testOEPSHA256, "sha256: abc123"},
		{"module with a space", "- handheld_multi_modal", "- \"handheld multi\""},
		{"arg with a space", `args: ["--gpu"]`, `args: ["--gpu --npu"]`},
		{"missing user", "    user: edge\n", ""},
		{"missing sbomUrl", "    sbomUrl: " + testOEPSBOMURL + "\n", ""},
		{"short sbomSha256", "sbomSha256: " + testOEPSBOMSHA256, "sbomSha256: abc"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			yaml := strings.Replace(oepInstallerTemplateYAML, tt.find, tt.replace, 1)
			if yaml == oepInstallerTemplateYAML {
				t.Fatalf("test input %q not found in the template", tt.find)
			}
			if _, err := parseYAMLTemplate([]byte(yaml), false); err == nil {
				t.Fatal("expected template to be rejected")
			}
		})
	}
}

func TestValidateOEPInstaller(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		os      string
		modify  func(*OEPInstallerConfig)
		wantErr string
	}{
		{"valid", "ubuntu", func(*OEPInstallerConfig) {}, ""},
		{"unsupported os", "azure-linux", func(*OEPInstallerConfig) {}, "only supported for ubuntu"},
		{"missing url", "ubuntu", func(o *OEPInstallerConfig) { o.URL = "" }, "url is required"},
		{"http url", "ubuntu", func(o *OEPInstallerConfig) { o.URL = "http://example.com/cli" }, "https:// URL"},
		{"url with credentials", "ubuntu", func(o *OEPInstallerConfig) { o.URL = "https://u:p@example.com/cli" },
			"credentials"},
		{"github blob page", "ubuntu", func(o *OEPInstallerConfig) {
			o.URL = "https://github.com/intel-sandbox/oep-installer-cli/blob/main/rendered/openedge-cli-federal-aerospace"
		}, "https://raw.githubusercontent.com/intel-sandbox/oep-installer-cli/main/rendered/openedge-cli-federal-aerospace"},
		{"github tree page", "ubuntu", func(o *OEPInstallerConfig) {
			o.URL = "https://github.com/intel-sandbox/oep-installer-cli/tree/v2026.3.0-rc1/rendered/openedge-cli"
		}, "GitHub web page"},
		{"github release download", "ubuntu", func(o *OEPInstallerConfig) {
			o.URL = "https://github.com/intel-sandbox/oep-installer-cli/releases/download/v2026.3.0/openedge-cli"
		}, ""},
		{"missing sbomUrl", "ubuntu", func(o *OEPInstallerConfig) { o.SBOMURL = "" }, "sbomUrl is required"},
		{"sbomUrl github page", "ubuntu", func(o *OEPInstallerConfig) {
			o.SBOMURL = "https://github.com/intel-sandbox/oep-installer-cli/blob/main/rendered/sbom-handheld.yaml"
		}, "systemConfig.oepInstaller.sbomUrl"},
		{"bad sbomSha256", "ubuntu", func(o *OEPInstallerConfig) { o.SBOMSHA256 = "xyz" }, "sbomSha256"},
		{"bad sha256", "ubuntu", func(o *OEPInstallerConfig) { o.SHA256 = "xyz" }, "sha256"},
		{"no modules", "ubuntu", func(o *OEPInstallerConfig) { o.Modules = nil }, "at least one"},
		{"duplicate module", "ubuntu", func(o *OEPInstallerConfig) { o.Modules = []string{"a", "a"} }, "duplicate"},
		{"invalid module", "ubuntu", func(o *OEPInstallerConfig) { o.Modules = []string{"a;b"} }, "invalid"},
		{"missing user", "ubuntu", func(o *OEPInstallerConfig) { o.User = "" }, "user is required"},
		{"root user", "ubuntu", func(o *OEPInstallerConfig) { o.User = "root" }, "must not be root"},
		{"undefined user", "ubuntu", func(o *OEPInstallerConfig) { o.User = "nobody" }, "not defined"},
		{"invalid arg", "ubuntu", func(o *OEPInstallerConfig) { o.Args = []string{"$(id)"} }, "args[0]"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			o := validOEPInstaller()
			tt.modify(&o)
			tmpl := &ImageTemplate{
				Target:       TargetInfo{OS: tt.os},
				SystemConfig: SystemConfig{Users: []UserConfig{{Name: "edge"}}, OEPInstaller: o},
			}
			err := tmpl.validateOEPInstaller()
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

func TestValidateOEPInstallerEmptyIsNoOp(t *testing.T) {
	t.Parallel()
	tmpl := &ImageTemplate{Target: TargetInfo{OS: "azure-linux"}}
	if err := tmpl.validateOEPInstaller(); err != nil {
		t.Fatalf("an absent section must not be validated: %v", err)
	}
}

func TestMergeOEPInstaller(t *testing.T) {
	t.Parallel()
	parent := SystemConfig{OEPInstaller: validOEPInstaller()}
	childSection := OEPInstallerConfig{URL: "https://example.com/cli", SHA256: testOEPSHA256,
		Modules: []string{"edge_pack"}, User: "edge"}

	inherited := mergeSystemConfig(parent, SystemConfig{Packages: []string{"curl"}})
	if !slices.Equal(inherited.OEPInstaller.Modules, []string{"handheld_multi_modal"}) {
		t.Errorf("a child without the section must inherit it, got %+v", inherited.OEPInstaller)
	}
	replaced := mergeSystemConfig(parent, SystemConfig{OEPInstaller: childSection})
	if replaced.OEPInstaller.URL != childSection.URL ||
		!slices.Equal(replaced.OEPInstaller.Modules, []string{"edge_pack"}) {
		t.Errorf("a child section must replace the inherited one, got %+v", replaced.OEPInstaller)
	}
	if isEmptySystemConfig(SystemConfig{OEPInstaller: childSection}) {
		t.Error("a system config with only oepInstaller must not be empty")
	}
}

func TestValidateOverlaySystemConfigRejectsOEPInstaller(t *testing.T) {
	t.Parallel()
	tmpl := &ImageTemplate{SystemConfig: SystemConfig{OEPInstaller: validOEPInstaller()}}
	err := tmpl.validateOverlaySystemConfig()
	if err == nil || !strings.Contains(err.Error(), "systemConfig.oepInstaller") {
		t.Fatalf("expected overlay validation to reject systemConfig.oepInstaller, got %v", err)
	}
}
