package dotfilter_test

import (
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage/dotfilter"
)

func TestFilterPackagesForDot_SystemOnly(t *testing.T) {
	pkgs := []ospackage.PackageInfo{
		{Name: "sys-a", Requires: []string{"lib-a", "lib-b"}},
		{Name: "lib-a"},
		{Name: "lib-b", Requires: []string{"lib-c"}},
		{Name: "lib-c"},
		{Name: "essential-x"},
	}

	sources := map[string]config.PackageSource{
		"sys-a":       config.PackageSourceSystem,
		"essential-x": config.PackageSourceEssential,
	}

	filtered := dotfilter.FilterPackagesForDot(pkgs, sources, true)
	want := []string{"sys-a", "lib-a", "lib-b", "lib-c"}

	if len(filtered) != len(want) {
		t.Fatalf("expected %d packages, got %d", len(want), len(filtered))
	}
	for i, name := range want {
		if filtered[i].Name != name {
			t.Fatalf("expected package %q at index %d, got %q", name, i, filtered[i].Name)
		}
	}
}

func TestFilterPackagesForDot_NoSystemRoots(t *testing.T) {
	pkgs := []ospackage.PackageInfo{{Name: "foo"}}
	sources := map[string]config.PackageSource{"foo": config.PackageSourceEssential}

	filtered := dotfilter.FilterPackagesForDot(pkgs, sources, true)
	if len(filtered) != 0 {
		t.Fatalf("expected empty slice when no system roots, got %d", len(filtered))
	}
}

func TestFilterPackagesForDot_Disabled(t *testing.T) {
	pkgs := []ospackage.PackageInfo{{Name: "foo"}, {Name: "bar"}}
	sources := map[string]config.PackageSource{"foo": config.PackageSourceSystem}

	filtered := dotfilter.FilterPackagesForDot(pkgs, sources, false)
	if len(filtered) != len(pkgs) {
		t.Fatalf("expected %d packages, got %d", len(pkgs), len(filtered))
	}
}
