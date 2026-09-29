package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
)

func TestBuildAndWriteCompositionManifest(t *testing.T) {
	t.Parallel()
	enabled := true
	dir := t.TempDir()
	script := filepath.Join(dir, "setup.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho hi\n"), 0755); err != nil {
		t.Fatal(err)
	}
	tmpl := &config.ImageTemplate{
		Image:  config.ImageInfo{Name: "fedaero", Version: "24.04"},
		Target: config.TargetInfo{OS: "ubuntu", Dist: "ubuntu24", Arch: "x86_64", ImageType: "iso"},
		PackageRepositories: []config.PackageRepository{
			{ID: "fedaero", Codename: "fedaero", URL: "https://deploy:tok3n@ppa.example/ubuntu?x=1", Priority: 600},
		},
		FullPkgListBom: []ospackage.PackageInfo{
			{Name: "zlib1g_1.3.deb", PkgName: "zlib1g", Version: "1:1.3", Arch: "amd64",
				Checksums: []ospackage.Checksum{{Algorithm: "SHA256", Value: "abc"}}},
			{Name: "bash.deb", Version: "5.2"},
		},
		SystemConfig: config.SystemConfig{
			Kernel:          config.KernelConfig{Packages: []string{"linux-image-generic-hwe-24.04"}, Cmdline: "quiet"},
			AdditionalFiles: []config.AdditionalFileInfo{{Local: script, Final: "/usr/local/sbin/setup.sh"}},
			Proxy:           config.ProxyConfig{HTTPProxy: "http://proxy.example:3128"},
			CloudInit:       config.CloudInitConfig{Enabled: &enabled},
		},
	}
	base := []config.ProviderRepoConfig{{Name: "noble", BaseURL: "http://archive.ubuntu.com/ubuntu", Component: "main"}}

	m, err := BuildCompositionManifest(tmpl, base, "fedaero-24.04.iso", "spdx.json")
	if err != nil {
		t.Fatalf("BuildCompositionManifest: %v", err)
	}
	if m.Packages[0].Name != "bash.deb" || m.Packages[1].Name != "zlib1g" || m.Packages[1].SHA256 != "abc" {
		t.Errorf("packages = %+v", m.Packages)
	}
	if len(m.Artifacts) != 1 || m.Artifacts[0].Source != "setup.sh" || len(m.Artifacts[0].SHA256) != 64 {
		t.Errorf("artifacts = %+v", m.Artifacts)
	}
	if len(m.BaseRepositories) != 1 || m.PackageRepositories[0].Priority != 600 || !m.Provisioning.CloudInit {
		t.Errorf("manifest = %+v", m)
	}

	out := filepath.Join(dir, CompositionManifestFileName("fedaero-24.04"))
	if err := WriteCompositionManifest(m, out); err != nil {
		t.Fatalf("WriteCompositionManifest: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"os": "ubuntu"`) || !strings.Contains(string(data), `"name": "fedaero"`) {
		t.Errorf("manifest must use lower-case JSON keys:\n%s", data)
	}
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "tok3n") {
		t.Error("proxy credentials leaked into the composition manifest")
	}
	var decoded CompositionManifest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("manifest is not valid JSON: %v", err)
	}
	if decoded.SchemaVersion != CompositionSchemaVersion || decoded.SBOM != "spdx.json" ||
		decoded.Provisioning.ProxyHosts[0] != "proxy.example:3128" || decoded.Target.OS != "ubuntu" {
		t.Errorf("decoded manifest = %+v", decoded)
	}
}

func TestBuildCompositionManifestSkipsMissingAdditionalFiles(t *testing.T) {
	t.Parallel()
	tmpl := &config.ImageTemplate{SystemConfig: config.SystemConfig{
		AdditionalFiles: []config.AdditionalFileInfo{{Local: "/nonexistent/file", Final: "/x"}},
	}}
	// GetAdditionalFileInfo drops missing files, so the manifest lists none.
	m, err := BuildCompositionManifest(tmpl, nil, "", "")
	if err != nil || len(m.Artifacts) != 0 {
		t.Errorf("m.Artifacts = %+v, err = %v", m.Artifacts, err)
	}
}

func TestCompositionArtifactsHashError(t *testing.T) {
	t.Parallel()
	_, err := compositionArtifacts([]config.AdditionalFileInfo{{Local: "/nonexistent/file", Final: "/x"}})
	if err == nil || !strings.Contains(err.Error(), "/nonexistent/file") {
		t.Errorf("err = %v", err)
	}
}

func TestRedactURL(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"https://user:tok@repo.example.com/apt?sig=abc#frag": "https://repo.example.com/apt",
		"http://archive.ubuntu.com/ubuntu":                   "http://archive.ubuntu.com/ubuntu",
		"https://repo.example.com/pool/a.deb?":               "https://repo.example.com/pool/a.deb",
		"":                                                   "",
		"::bad":                                              "",
		// An opaque URL keeps its userinfo in Opaque, which clearing User misses.
		"https:user:token@repo.example.com/path": "",
	}
	for in, want := range tests {
		if got := redactURL(in); got != want {
			t.Errorf("redactURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSPDXPackageRedactsDownloadLocation(t *testing.T) {
	t.Parallel()
	pkg := buildSPDXPackage(ospackage.PackageInfo{Name: "a", URL: "https://u:tok@repo.example.com/a.deb?sig=1"})
	if pkg.DownloadLocation != "https://repo.example.com/a.deb" {
		t.Errorf("DownloadLocation = %q", pkg.DownloadLocation)
	}
	if got := buildSPDXPackage(ospackage.PackageInfo{Name: "b"}).DownloadLocation; got != "NOASSERTION" {
		t.Errorf("empty URL must be NOASSERTION, got %q", got)
	}
}
