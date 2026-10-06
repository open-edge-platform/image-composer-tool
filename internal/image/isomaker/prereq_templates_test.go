package isomaker

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

// removedPackages lists, per target dist, packages an installer environment must
// not request because the release no longer ships them: apt stops the ISO build
// with "Unable to locate package" after all packages are downloaded.
var removedPackages = map[string][]string{
	"ubuntu26": {"policykit-1"}, // replaced by polkitd and pkexec
}

// TestBasePlatformInstallerEnvironmentFiles checks that the unattended installer
// environment of each shipped base platform ISO template references files that
// resolve: the installer script must exist, and live-installer must point at
// <repo>/build/live-installer (it is a build output, so it may not exist yet).
// A relative path one level off fails every ISO build before package download.
func TestBasePlatformInstallerEnvironmentFiles(t *testing.T) {
	orig := config.Global()
	t.Cleanup(func() { config.SetGlobal(orig) })
	g := *orig
	g.ConfigDir = "../../../config"
	config.SetGlobal(&g)

	wantLiveInstaller, err := filepath.Abs("../../../build/live-installer")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"../../../image-templates/ubuntu24/ubuntu24-x86_64-edgepack-unattended-iso.yml",
		"../../../image-templates/ubuntu24/ubuntu24-x86_64-edgepack-server-unattended-iso.yml",
		"../../../image-templates/ubuntu26/ubuntu26-x86_64-edgepack-unattended-iso.yml",
		"../../../image-templates/ubuntu26/ubuntu26-x86_64-edgepack-server-unattended-iso.yml",
	} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			tmpl, err := config.LoadAndMergeTemplate(path)
			if err != nil {
				t.Fatalf("LoadAndMergeTemplate: %v", err)
			}
			initrdPath, err := tmpl.GetInitramfsTemplate()
			if err != nil {
				t.Fatalf("GetInitramfsTemplate: %v", err)
			}
			initrd, err := config.LoadAndMergeTemplate(initrdPath)
			if err != nil {
				t.Fatalf("loading installer environment %s: %v", initrdPath, err)
			}
			for _, pkg := range initrd.SystemConfig.Packages {
				if slices.Contains(removedPackages[tmpl.Target.Dist], pkg) {
					t.Errorf("installer environment %s requests %s, which %s no longer ships", initrdPath, pkg, tmpl.Target.Dist)
				}
			}
			for _, f := range initrd.SystemConfig.AdditionalFiles {
				if isLiveInstaller(f.Local) {
					got, err := filepath.Abs(filepath.Join(filepath.Dir(initrdPath), f.Local))
					if err != nil || got != wantLiveInstaller {
						t.Errorf("live-installer source %q resolves to %s, want %s", f.Local, got, wantLiveInstaller)
					}
					continue
				}
				if _, ok := config.ResolveTemplateRelativePath(initrd.PathList, f.Local); !ok {
					t.Errorf("installer environment file %q (-> %s) not found", f.Local, f.Final)
				}
			}
		})
	}
}
