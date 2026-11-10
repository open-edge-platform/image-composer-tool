package overlay

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

func TestRetainedDependencyFailures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		requirement string
		resolved    []ResolvedPackage
		removals    []PlannedAction
		wantFailure bool
	}{
		{name: "exact library upgrade", requirement: "library (= 1)",
			resolved: []ResolvedPackage{{Name: "library", Version: "2"}}, wantFailure: true},
		{name: "satisfied minimum", requirement: "library (>= 1)",
			resolved: []ResolvedPackage{{Name: "library", Version: "2"}}},
		{name: "alternative remains", requirement: "library (= 1) | helper",
			resolved: []ResolvedPackage{{Name: "library", Version: "2"}}},
		{name: "virtual provider remains", requirement: "library (= 1) | virtual-helper",
			resolved: []ResolvedPackage{{Name: "library", Version: "2"}}},
		{name: "preexisting failure", requirement: "library (= 0)",
			resolved: []ResolvedPackage{{Name: "library", Version: "2"}}},
		{name: "upgraded dependent", requirement: "library (= 1)",
			resolved: []ResolvedPackage{{Name: "library", Version: "2"}, {Name: "consumer", Version: "2"}}},
		{name: "removed dependent", requirement: "library (= 1)",
			resolved: []ResolvedPackage{{Name: "library", Version: "2"}},
			removals: []PlannedAction{{Type: ActionRemove, Package: "consumer"}}},
		{name: "removed dependency", requirement: "library",
			removals: []PlannedAction{{Type: ActionRemove, Package: "library"}}, wantFailure: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			baseline := []BaselinePackage{
				{Name: "consumer", Version: "1", Installed: true,
					Requirements: parseDebDependsField("consumer", test.requirement)},
				{Name: "library", Version: "1", Installed: true},
				{Name: "helper", Version: "1", Installed: true, Provides: []string{"virtual-helper"}},
			}

			failures := retainedDependencyFailures(PackageManagerAPT, baseline, test.resolved, test.removals, "amd64")

			if (len(failures) > 0) != test.wantFailure {
				t.Fatalf("failures = %+v, wantFailure %v", failures, test.wantFailure)
			}
			if len(failures) > 0 && (failures[0].Package != "consumer" || failures[0].Type != ActionUnsatisfiedDep) {
				t.Errorf("unexpected failure action: %+v", failures[0])
			}
			if failures := retainedDependencyFailures(PackageManagerDNF, baseline, test.resolved, test.removals, "x86_64"); len(failures) != 0 {
				t.Errorf("Debian repair must not change RPM planning: %+v", failures)
			}
		})
	}
}

func TestRetainedUpgradeTargets(t *testing.T) {
	t.Parallel()
	failures := []PlannedAction{
		{Package: "z-consumer"}, {Package: "a-consumer"}, {Package: "z-consumer"},
		{Package: "pinned"}, {Package: "qualified"}, {Package: "forced"},
		{Package: "grub-efi-amd64"}, {Package: "linux-image-generic"},
		{Package: "foreign-consumer", Arch: "i386"},
	}

	targets := retainedUpgradeTargets(failures, []string{"pinned_1", "qualified:amd64"}, map[string]bool{"forced": true}, "amd64")

	if !reflect.DeepEqual(targets, []string{"a-consumer", "z-consumer"}) {
		t.Fatalf("targets = %v, want [a-consumer z-consumer] without duplicates or pin overrides", targets)
	}
}

func TestRetainedDependencyFailuresNVLCohorts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		consumer string
		library  string
		old      string
		new      string
	}{
		{"util-linux", "libmount1", "2.41.3-3ubuntu2", "2.41.3-3ubuntu2.2"},
		{"mount", "libblkid1", "2.41.3-3ubuntu2", "2.41.3-3ubuntu2.2"},
		{"libegl-mesa0", "libgbm1", "26.0.3-1ubuntu1", "26.0.8-1ppa1~resolute1"},
		{"systemd-oomd", "libsystemd-shared", "259.5-0ubuntu3", "259.9-0ubuntu0.1"},
		{"gir1.2-girepository-3.0", "gir1.2-glib-2.0", "2.88.0-1", "2.88.0-1ubuntu0.1"},
		{"gpg-wks-client", "dirmngr", "2.4.8-4ubuntu3", "2.4.8-4ubuntu3.1"},
		{"libx11-xcb1", "libx11-6", "2:1.8.13-1", "2:1.8.13-1ubuntu0.1"},
	}
	for _, test := range tests {
		t.Run(test.consumer, func(t *testing.T) {
			t.Parallel()
			baseline := []BaselinePackage{
				{Name: test.consumer, Version: test.old, Installed: true,
					Requirements: parseDebDependsField(test.consumer, test.library+" (= "+test.old+")")},
				{Name: test.library, Version: test.old, Installed: true},
			}
			resolved := []ResolvedPackage{{Name: test.library, Version: test.new}}

			report := EvaluatePreflight(PreflightInput{Family: PackageManagerAPT, Baseline: baseline,
				Resolved: resolved, Policy: config.OverlayPolicy{AllowUpgrade: true, AllowPackageRemoval: true}})

			if !report.Blocked {
				t.Fatal("retained exact-version dependency must block even when removals are authorized")
			}
			failures := retainedDependencyFailures(PackageManagerAPT, baseline, resolved, nil, "amd64")
			if len(failures) != 1 || !strings.Contains(failures[0].Detail, test.new) {
				t.Fatalf("failures = %+v, want one diagnostic containing proposed version %s", failures, test.new)
			}
		})
	}
}

func TestRetainedDependencyArchitecturesAndProvides(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		requirement string
		arch        string
		provided    string
		wantFailure bool
	}{
		{name: "explicit foreign arch unchanged", requirement: "library:i386 (= 1)", arch: "amd64"},
		{name: "foreign consumer broken", requirement: "library (= 1)", arch: "i386", wantFailure: true},
		{name: "any arch alternative", requirement: "library:any (= 1)", arch: "amd64"},
		{name: "versioned virtual alternative", requirement: "library (= 1) | virtual (= 3)", arch: "amd64",
			provided: "virtual (= 3)"},
		{name: "unversioned provider cannot satisfy version", requirement: "library (= 1) | virtual (= 3)",
			arch: "amd64", provided: "virtual", wantFailure: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			baseline := []BaselinePackage{
				{Name: "consumer", Version: "1", Arch: test.arch, Installed: true,
					Requirements: parseDebDependsField("consumer", test.requirement)},
				{Name: "library", Version: "1", Arch: "amd64", Installed: true, MultiArch: "allowed"},
				{Name: "library", Version: "1", Arch: "i386", Installed: true, MultiArch: "allowed"},
				{Name: "provider", Version: "7", Arch: "amd64", Installed: true,
					ProvidedRelations: parseDebProvidedRelations(test.provided)},
			}

			failures := retainedDependencyFailures(PackageManagerAPT, baseline,
				[]ResolvedPackage{{Name: "library", Version: "2", Arch: test.arch, MultiArch: "allowed"}}, nil, "amd64")

			if (len(failures) > 0) != test.wantFailure {
				t.Fatalf("failures = %+v, wantFailure %v", failures, test.wantFailure)
			}
		})
	}
}

func TestRetainedTransactionRegressions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		requirement string
		ownerArch   string
		oldArch     string
		newArch     string
		multiArch   string
		provided    string
		newProvided []string
		removals    []PlannedAction
		wantFailure bool
	}{
		{name: "mixed upgrade and removal", requirement: "library (= 1) | helper (= 1)",
			removals: []PlannedAction{{Type: ActionRemove, Package: "helper"}}, wantFailure: true},
		{name: "versioned provider upgrade", requirement: "virtual (= 3)",
			provided: "virtual (= 3)", newProvided: []string{"virtual (= 3)"}},
		{name: "changed virtual capability", requirement: "virtual (= 3)",
			provided: "virtual (= 3)", newProvided: []string{"virtual (= 4)"}, wantFailure: true},
		{name: "foreign helper", requirement: "library (= 1)", ownerArch: "i386",
			oldArch: "amd64", newArch: "amd64", multiArch: "foreign", wantFailure: true},
		{name: "native is not consumer arch", requirement: "library:native (= 1)", ownerArch: "i386",
			oldArch: "amd64", newArch: "amd64", wantFailure: true},
		{name: "architecture becomes all", requirement: "library (= 1)", ownerArch: "amd64",
			oldArch: "amd64", newArch: "all", wantFailure: true},
		{name: "all becomes architecture", requirement: "library (= 1)", ownerArch: "amd64",
			oldArch: "all", newArch: "amd64", wantFailure: true},
		{name: "all is not implicitly foreign", requirement: "library (>= 1)", ownerArch: "i386",
			oldArch: "i386", newArch: "all", wantFailure: true},
		{name: "all does not match explicit foreign arch", requirement: "library:i386 (>= 1)", ownerArch: "amd64",
			oldArch: "i386", newArch: "all", wantFailure: true},
		{name: "all foreign satisfies foreign consumer", requirement: "library (>= 1)", ownerArch: "i386",
			oldArch: "i386", newArch: "all", multiArch: "foreign"},
		{name: "all satisfies native qualifier", requirement: "library:native (>= 1)", ownerArch: "i386",
			oldArch: "amd64", newArch: "all"},
		{name: "all satisfies explicit native arch", requirement: "library:amd64 (>= 1)", ownerArch: "i386",
			oldArch: "amd64", newArch: "all"},
		{name: "all allowed satisfies any qualifier", requirement: "library:any (>= 1)", ownerArch: "i386",
			oldArch: "i386", newArch: "all", multiArch: "allowed"},
		{name: "all virtual provider is not implicitly foreign", requirement: "virtual (>= 3)", ownerArch: "i386",
			oldArch: "i386", newArch: "all", provided: "virtual (= 3)",
			newProvided: []string{"virtual (= 3)"}, wantFailure: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			in := PreflightInput{Family: PackageManagerAPT, NativeArch: "amd64",
				Policy: config.OverlayPolicy{AllowPackageRemoval: true},
				Baseline: []BaselinePackage{
					{Name: "consumer", Version: "1", Arch: test.ownerArch, Installed: true,
						Requirements: parseDebDependsField("consumer", test.requirement)},
					{Name: "library", Version: "1", Arch: test.oldArch, Installed: true,
						MultiArch: test.multiArch, ProvidedRelations: parseDebProvidedRelations(test.provided)},
					{Name: "helper", Version: "0", Installed: true},
				},
				Resolved: []ResolvedPackage{{Name: "library", Version: "2", Arch: test.newArch,
					MultiArch: test.multiArch, ProvidesVer: test.newProvided}},
			}

			failures := retainedPreflightFailures(in, test.removals)

			if (len(failures) > 0) != test.wantFailure {
				t.Fatalf("failures = %+v, wantFailure %v", failures, test.wantFailure)
			}
		})
	}
}

func TestRetainedArchMatchesArchitectureAll(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		required  string
		ownerArch string
		multiArch string
		want      bool
	}{
		{name: "native consumer", ownerArch: "amd64", want: true},
		{name: "all consumer", ownerArch: "all", want: true},
		{name: "foreign consumer", ownerArch: "i386"},
		{name: "foreign helper", ownerArch: "i386", multiArch: "foreign", want: true},
		{name: "allowed is not foreign", ownerArch: "i386", multiArch: "allowed"},
		{name: "explicit native", required: "amd64", ownerArch: "i386", want: true},
		{name: "explicit foreign", required: "i386", ownerArch: "i386"},
		{name: "foreign cannot override explicit arch", required: "i386", ownerArch: "i386", multiArch: "foreign"},
		{name: "native qualifier", required: "native", ownerArch: "i386", want: true},
		{name: "any requires allowed", required: "any", ownerArch: "i386"},
		{name: "any allowed", required: "any", ownerArch: "i386", multiArch: "allowed", want: true},
		{name: "foreign cannot satisfy any", required: "any", ownerArch: "i386", multiArch: "foreign"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			owner := BaselinePackage{Arch: test.ownerArch}
			candidate := BaselinePackage{Arch: "all", MultiArch: test.multiArch}

			got := retainedArchMatches(test.required, owner, candidate, "amd64")

			if got != test.want {
				t.Errorf("retainedArchMatches(%q, %+v, %+v, amd64) = %v, want %v",
					test.required, owner, candidate, got, test.want)
			}
		})
	}
}

func TestRetainedPreflightRemovalPolicy(t *testing.T) {
	t.Parallel()
	for _, allow := range []bool{false, true} {
		t.Run(fmt.Sprint(allow), func(t *testing.T) {
			t.Parallel()
			in := PreflightInput{Family: PackageManagerAPT,
				Baseline: []BaselinePackage{
					{Name: "consumer", Version: "1", Installed: true,
						Requirements: parseDebDependsField("consumer", "removed")},
					{Name: "removed", Version: "1", Installed: true},
				}, Policy: config.OverlayPolicy{AllowPackageRemoval: allow},
			}

			failures := retainedPreflightFailures(in, []PlannedAction{{Type: ActionRemove, Package: "removed"}})

			if (len(failures) > 0) == allow {
				t.Errorf("failures = %+v, want failure only when allowPackageRemoval is false", failures)
			}
		})
	}
}
