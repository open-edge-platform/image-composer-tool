package oepinstaller

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

func firstBootTemplate(t *testing.T) *config.ImageTemplate {
	t.Helper()
	script := filepath.Join(t.TempDir(), "20-setup.sh")
	if err := os.WriteFile(script, []byte("#!/bin/bash\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &config.ImageTemplate{SystemConfig: config.SystemConfig{
		Users:    []config.UserConfig{{Name: "edge"}},
		Packages: []string{"curl"},
		Provisioning: config.ProvisioningConfig{Scripts: []config.ProvisioningScript{
			{Name: "setup", Local: script, Final: "/usr/local/sbin/setup.sh", Order: 20},
		}},
		OEPInstaller: config.OEPInstallerConfig{
			URL:     "https://raw.githubusercontent.com/org/oep-installer-cli/8d24c5b/rendered/openedge-cli-federal-aerospace",
			Modules: []string{"handheld_blueprint"},
			User:    "edge",
			Args:    []string{"--edgepack"},
		},
	}}
}

func TestAddFirstBoot(t *testing.T) {
	t.Parallel()
	tmpl := firstBootTemplate(t)
	if err := AddFirstBoot(tmpl, []byte("#!/bin/bash\n# cli\n"), t.TempDir()); err != nil {
		t.Fatalf("AddFirstBoot: %v", err)
	}
	cliFinal := "/usr/local/share/oep-installer/openedge-cli-federal-aerospace"
	files := map[string]string{}
	for _, f := range tmpl.SystemConfig.AdditionalFiles {
		data, err := os.ReadFile(f.Local)
		if err != nil {
			t.Fatalf("reading %s: %v", f.Local, err)
		}
		files[f.Final] = string(data)
	}
	if files[cliFinal] != "#!/bin/bash\n# cli\n" {
		t.Errorf("CLI not copied to %s: %v", cliFinal, files)
	}
	if !strings.Contains(files[firstBootScript], "openedge-cli\" install \"$@\"") {
		t.Errorf("first-boot script not copied to %s", firstBootScript)
	}
	if files[sudoersTmpfiles] != tmpfilesContents {
		t.Errorf("tmpfiles rule = %q", files[sudoersTmpfiles])
	}
	scripts := tmpl.SystemConfig.Provisioning.Scripts
	got := scripts[len(scripts)-1]
	if got.Name != firstBootName || got.Final != firstBootScript || got.Order != 21 ||
		!slices.Equal(got.Args, []string{"handheld_blueprint", "--edgepack"}) ||
		got.Env[userEnv] != "edge" || got.Env[cliEnv] != cliFinal {
		t.Errorf("first-boot script = %+v", got)
	}
	if !slices.Contains(tmpl.SystemConfig.Packages, "sudo") {
		t.Errorf("sudo not added to packages: %v", tmpl.SystemConfig.Packages)
	}
}

func TestAddFirstBootRejects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		modify  func(*config.ImageTemplate)
		wantErr string
	}{
		{"URL without a file name", func(tm *config.ImageTemplate) {
			tm.SystemConfig.OEPInstaller.URL = "https://example.com/"
		}, "does not end in a file name"},
		{"script name already used", func(tm *config.ImageTemplate) {
			tm.SystemConfig.Provisioning.Scripts[0].Name = firstBootName
		}, "duplicate"},
		{"script path already used", func(tm *config.ImageTemplate) {
			tm.SystemConfig.AdditionalFiles = []config.AdditionalFileInfo{
				{Local: "/tmp/other", Final: firstBootScript}}
		}, firstBootScript},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmpl := firstBootTemplate(t)
			tt.modify(tmpl)
			err := AddFirstBoot(tmpl, []byte("cli"), t.TempDir())
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestNextScriptOrder(t *testing.T) {
	t.Parallel()
	if got := nextScriptOrder(nil); got != 0 {
		t.Errorf("nextScriptOrder(nil) = %d", got)
	}
	scripts := []config.ProvisioningScript{{Order: 30}, {Order: 10}}
	if got := nextScriptOrder(scripts); got != 31 {
		t.Errorf("nextScriptOrder = %d, want 31", got)
	}
}
