package overlay

import (
	"reflect"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
)

// TestBreaksDrivenUpgradeTargets covers the pure Breaks-scan that decides which
// baseline packages must be upgraded (rather than removed) to clear a versioned
// Breaks: declared by a to-install package.
func TestBreaksDrivenUpgradeTargets(t *testing.T) {
	// vim-runtime (being installed) Breaks vim-tiny (<< 2:9.1); the baseline vim-tiny
	// (2:9.0) falls inside that range, so it must be upgraded.
	closure := []ospackage.PackageInfo{
		{PkgName: "vim-runtime", Version: "2:9.2", Breaks: []string{"vim-tiny (<< 2:9.1)"}},
	}
	toInstall := &ResolutionPlan{ToInstall: []ResolvedPackage{{Name: "vim-runtime", Version: "2:9.2"}}}
	baseInRange := map[string]BaselinePackage{"vim-tiny": {Name: "vim-tiny", Version: "2:9.0", Installed: true}}

	if got := breaksDrivenUpgradeTargets(PackageManagerAPT, closure, toInstall, baseInRange, nil); !reflect.DeepEqual(got, []string{"vim-tiny"}) {
		t.Errorf("targets = %v, want [vim-tiny]", got)
	}

	// Baseline vim-tiny already newer than the break range → nothing to force.
	baseOutOfRange := map[string]BaselinePackage{"vim-tiny": {Name: "vim-tiny", Version: "2:9.5", Installed: true}}
	if got := breaksDrivenUpgradeTargets(PackageManagerAPT, closure, toInstall, baseOutOfRange, nil); len(got) != 0 {
		t.Errorf("targets = %v, want none (baseline already out of break range)", got)
	}

	// A target already forced in a prior pass is not re-added (loop termination).
	if got := breaksDrivenUpgradeTargets(PackageManagerAPT, closure, toInstall, baseInRange, map[string]bool{"vim-tiny": true}); len(got) != 0 {
		t.Errorf("targets = %v, want none (already forced)", got)
	}

	// An UNVERSIONED break cannot be cleared by an upgrade (it breaks every version),
	// so it is not actionable here — preflight handles it via removal.
	unversioned := []ospackage.PackageInfo{{PkgName: "vim-runtime", Version: "2:9.2", Breaks: []string{"vim-tiny"}}}
	if got := breaksDrivenUpgradeTargets(PackageManagerAPT, unversioned, toInstall, baseInRange, nil); len(got) != 0 {
		t.Errorf("targets = %v, want none (unversioned break)", got)
	}

	// The break target is not a baseline package → nothing to upgrade.
	if got := breaksDrivenUpgradeTargets(PackageManagerAPT, closure, toInstall, map[string]BaselinePackage{}, nil); len(got) != 0 {
		t.Errorf("targets = %v, want none (target absent from baseline)", got)
	}

	// A break declared by a package that is NOT being installed is ignored.
	notInstalled := &ResolutionPlan{ToInstall: nil}
	if got := breaksDrivenUpgradeTargets(PackageManagerAPT, closure, notInstalled, baseInRange, nil); len(got) != 0 {
		t.Errorf("targets = %v, want none (breaker not in to-install set)", got)
	}

	// Breaks is a deb-only field; the rpm family is a no-op.
	if got := breaksDrivenUpgradeTargets(PackageManagerDNF, closure, toInstall, baseInRange, nil); len(got) != 0 {
		t.Errorf("targets = %v, want none (rpm family)", got)
	}
}

// breaksBackend is a stateful resolverBackend stub: before vim-tiny is forced into
// the seed it returns only the breaker (vim-runtime); once vim-tiny is seeded it
// also returns the newer vim-tiny that escapes the break range.
type breaksBackend struct {
	fam   PackageManager
	calls int
}

func (b *breaksBackend) family() PackageManager { return b.fam }

func (b *breaksBackend) resolveAndDownload(req resolveRequest) ([]ospackage.PackageInfo, []string, error) {
	b.calls++
	breaker := ospackage.PackageInfo{PkgName: "vim-runtime", Version: "2:9.2", Arch: "amd64", URL: "https://r/vim-runtime.deb", Breaks: []string{"vim-tiny (<< 2:9.1)"}}
	forced := false
	for _, s := range req.seed {
		if s == "vim-tiny" {
			forced = true
		}
	}
	if !forced {
		return []ospackage.PackageInfo{breaker}, []string{"vim-runtime.deb"}, nil
	}
	vimTiny := ospackage.PackageInfo{PkgName: "vim-tiny", Version: "2:9.2", Arch: "amd64", URL: "https://r/vim-tiny.deb"}
	return []ospackage.PackageInfo{breaker, vimTiny}, []string{"vim-runtime.deb", "vim-tiny.deb"}, nil
}

// TestResolveOverlayPackages_BreaksDrivenUpgrade is the end-to-end regression: a
// to-install package Breaks a baseline package via a versioned range, and the
// overlay must AUTOMATICALLY upgrade that baseline package (re-resolving to pull
// its newer version) instead of leaving the break to be resolved only by removal.
func TestResolveOverlayPackages_BreaksDrivenUpgrade(t *testing.T) {
	backend := &breaksBackend{fam: PackageManagerAPT}
	template := &config.ImageTemplate{
		Target: config.TargetInfo{OS: "ubuntu", Dist: "ubuntu24", Arch: "amd64"},
		SystemConfig: config.SystemConfig{
			Packages: []string{"vim-runtime"}, // the breaker; vim-tiny is NOT listed
		},
		OverlayPolicy: &config.OverlayPolicy{
			PackageOperation: config.OverlayPackageOpAdditiveAndUpgrade,
			AllowUpgrade:     true,
		},
	}
	info := &BaselineInfo{OS: "ubuntu", Arch: "amd64", PackageManager: PackageManagerAPT, PackageType: pkgTypeDeb}
	baseline := []BaselinePackage{
		{Name: "vim-tiny", Version: "2:9.0", Arch: "amd64", Installed: true}, // inside the break range
	}

	var plan *ResolutionPlan
	withStubbedResolution(t, backend, []config.ProviderRepoConfig{debProviderRepo()}, nil, func() {
		var err error
		plan, err = ResolveOverlayPackages(template, info, baseline)
		if err != nil {
			t.Fatalf("ResolveOverlayPackages: %v", err)
		}
	})

	if backend.calls != 2 {
		t.Errorf("backend resolve calls = %d, want 2 (initial + one Breaks-driven re-resolve)", backend.calls)
	}

	install := map[string]string{}
	for _, p := range plan.ToInstall {
		install[p.Name] = p.Version
	}
	if _, ok := install["vim-runtime"]; !ok {
		t.Errorf("vim-runtime (requested breaker) missing from ToInstall: %v", install)
	}
	if v := install["vim-tiny"]; v != "2:9.2" {
		t.Errorf("vim-tiny upgrade = %q, want 2:9.2 auto-added to clear the versioned Breaks", v)
	}

	// The forced vim-tiny upgrade is internal; plan.Requested reports only the
	// template-requested set, not packages pulled in by the Breaks-driven re-resolve.
	if !reflect.DeepEqual(plan.Requested, []string{"vim-runtime"}) {
		t.Errorf("plan.Requested = %v, want [vim-runtime] (forced Breaks upgrade must not appear as a template request)", plan.Requested)
	}
}
