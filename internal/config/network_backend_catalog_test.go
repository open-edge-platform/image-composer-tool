package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTemplateCatalogNetworkBackendOwnership guards the network.backend
// contract for every shipped template once it is merged with its OS default:
// the backend names the component that owns networking, so a template that
// installs NetworkManager, or relies on cloud-init (netplan) on a Debian-family
// OS, must not inherit the OS default's systemd-networkd backend. ICT enables
// systemd-networkd only for that backend, and enabling it next to another
// network owner leaves two managers fighting over the same interface.
func TestTemplateCatalogNetworkBackendOwnership(t *testing.T) {
	// Point ConfigDir at the repo's real config tree so templates merge with
	// their actual OS defaults.
	repoConfig, err := filepath.Abs(filepath.Join("..", "..", "config"))
	if err != nil {
		t.Fatalf("resolve repo config dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoConfig, "osv")); err != nil {
		t.Fatalf("repo config tree not found: %v", err)
	}
	prev := Global()
	cfg := *prev
	cfg.ConfigDir = repoConfig
	SetGlobal(&cfg)
	defer SetGlobal(prev)

	var templates []string
	for _, pattern := range []string{"*.yml", "*/*.yml"} {
		matches, err := filepath.Glob(filepath.Join("..", "..", "image-templates", pattern))
		if err != nil {
			t.Fatalf("glob templates: %v", err)
		}
		templates = append(templates, matches...)
	}
	if len(templates) == 0 {
		t.Fatal("no templates found")
	}

	for _, path := range templates {
		path := path
		t.Run(strings.TrimPrefix(filepath.ToSlash(path), "../../image-templates/"), func(t *testing.T) {
			tmpl, err := LoadAndMergeTemplate(path)
			if err != nil {
				t.Fatalf("LoadAndMergeTemplate: %v", err)
			}

			backend := tmpl.SystemConfig.Network.Backend
			// Every build-from-scratch image (not an overlay, not WSL2) must
			// resolve to an explicit owner, from its OS default or its own
			// network section, rather than leaving networking to chance.
			if backend == "" && !tmpl.IsOverlayMode() && tmpl.Target.ImageType != "wsl2" {
				t.Errorf("resolves to no network.backend; set network.backend in the template or its OS default")
			}
			switch backend {
			case "", NetworkBackendSystemdNetworkd, NetworkBackendNetplan, NetworkBackendNetworkManager, NetworkBackendNone:
			default:
				t.Fatalf("unknown network.backend %q", backend)
			}
			if backend != NetworkBackendSystemdNetworkd {
				return
			}

			hasPackage := func(names ...string) bool {
				for _, pkg := range tmpl.SystemConfig.Packages {
					for _, name := range names {
						if strings.EqualFold(pkg, name) {
							return true
						}
					}
				}
				return false
			}

			if hasPackage("network-manager", "NetworkManager") {
				t.Errorf("installs NetworkManager but resolves to network.backend %q; set network.backend: %s",
					backend, NetworkBackendNetworkManager)
			}
			switch tmpl.Target.OS {
			case "ubuntu", "debian", "wind-river-elxr":
				if hasPackage("cloud-init") || tmpl.SystemConfig.CloudInit.NetworkConfigFile != "" {
					t.Errorf("relies on cloud-init (netplan) but resolves to network.backend %q; set network.backend: %s",
						backend, NetworkBackendNetplan)
				}
			}
		})
	}
}
