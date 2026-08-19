package debutils

import (
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
)

// TestFindAllCandidates_VirtualProviderPicksNewest covers the fix for the
// virtual-package resolution bug: when a dependency names a VIRTUAL package that
// several versions of the same real package Provide (e.g. libssl3 provided by
// libssl3t64 at 3.0.13-0ubuntu3.3 and 3.0.13-0ubuntu3.11), the resolver must pick
// the NEWEST provider, not whichever the Packages file listed first. Previously
// provides matches were left in stable (file) order, so the oldest won and a later
// exact "= <newer>" pin on the real package became unsatisfiable.
func TestFindAllCandidates_VirtualProviderPicksNewest(t *testing.T) {
	all := []ospackage.PackageInfo{
		{
			Name:     "libssl3t64",
			Version:  "3.0.13-0ubuntu3.3", // older provider listed FIRST
			Provides: []string{"libssl3"},
			URL:      "http://example.com/pool/main/o/openssl/libssl3t64_3.0.13-0ubuntu3.3_amd64.deb",
			Type:     "deb",
		},
		{
			Name:     "libssl3t64",
			Version:  "3.0.13-0ubuntu3.11", // newer provider listed SECOND
			Provides: []string{"libssl3"},
			URL:      "http://example.com/pool/main/o/openssl/libssl3t64_3.0.13-0ubuntu3.11_amd64.deb",
			Type:     "deb",
		},
	}

	got := findAllCandidates("libssl3", all)
	if len(got) == 0 {
		t.Fatal("expected at least one candidate for virtual package libssl3, got none")
	}
	if got[0].Version != "3.0.13-0ubuntu3.11" {
		t.Errorf("virtual libssl3 resolved to provider version %q; want the newest 3.0.13-0ubuntu3.11", got[0].Version)
	}
}

// TestResolveDependencies_OrDepPrefersSelectedAlternative covers the fix for the
// OR-dependency bug: "a | b" must be treated as satisfied when b is already
// selected (requested), instead of blindly pulling the first literal alternative
// a. This is the va-driver-all case: it Depends
// "intel-media-va-driver | intel-media-va-driver-non-free"; the non-free driver
// was requested, yet the resolver pulled the conflicting free driver.
func TestResolveDependencies_OrDepPrefersSelectedAlternative(t *testing.T) {
	all := []ospackage.PackageInfo{
		{
			Name:        "va-driver-all",
			Version:     "2.20.0",
			Requires:    []string{"intel-media-va-driver"}, // parser keeps only the first alternative
			RequiresVer: []string{"intel-media-va-driver | intel-media-va-driver-non-free"},
			URL:         "http://example.com/pool/main/v/va-driver-all/va-driver-all_2.20.0_amd64.deb",
			Type:        "deb",
		},
		{
			Name:    "intel-media-va-driver", // FREE driver — must NOT be pulled
			Version: "24.1.0",
			URL:     "http://example.com/pool/main/i/intel-media-va-driver/intel-media-va-driver_24.1.0_amd64.deb",
			Type:    "deb",
		},
		{
			Name:    "intel-media-va-driver-non-free", // requested alternative
			Version: "26.2.3",
			URL:     "http://example.com/pool/main/i/intel-media-va-driver-non-free/intel-media-va-driver-non-free_26.2.3_amd64.deb",
			Type:    "deb",
		},
	}

	requested := []ospackage.PackageInfo{all[0], all[2]} // va-driver-all + the non-free driver

	resolved, err := ResolveDependencies(requested, all)
	if err != nil {
		t.Fatalf("ResolveDependencies failed: %v", err)
	}

	var freeSeen, nonFreeSeen bool
	for _, p := range resolved {
		switch p.Name {
		case "intel-media-va-driver":
			freeSeen = true
		case "intel-media-va-driver-non-free":
			nonFreeSeen = true
		}
	}
	if freeSeen {
		t.Error("free intel-media-va-driver was pulled in; the already-requested non-free alternative should satisfy the OR-dependency")
	}
	if !nonFreeSeen {
		t.Error("requested intel-media-va-driver-non-free is missing from the resolved set")
	}
}

// TestFindAllCandidates_VirtualProviderNewestWithInterleavedProvider guards the
// TOTAL-ordering property of the provides comparator: when a DIFFERENT provider is
// interleaved between two versions of the same real provider (e.g.
// [libssl3t64@old, otherlib, libssl3t64@new] all providing "libssl3"), the newest
// same-named provider must still win. A non-transitive comparator (treating
// different names as equal while ordering same names by version) could leave the
// oldest first, so this exercises the interleaving the two-candidate test cannot.
func TestFindAllCandidates_VirtualProviderNewestWithInterleavedProvider(t *testing.T) {
	all := []ospackage.PackageInfo{
		{
			Name:     "libssl3t64",
			Version:  "3.0.13-0ubuntu3.3", // older provider listed FIRST
			Provides: []string{"libssl3"},
			URL:      "http://example.com/pool/main/o/openssl/libssl3t64_3.0.13-0ubuntu3.3_amd64.deb",
			Type:     "deb",
		},
		{
			Name:     "otherlib", // a different provider interleaved between the two builds
			Version:  "1.0",
			Provides: []string{"libssl3"},
			URL:      "http://example.com/pool/main/o/otherlib/otherlib_1.0_amd64.deb",
			Type:     "deb",
		},
		{
			Name:     "libssl3t64",
			Version:  "3.0.13-0ubuntu3.11", // newer provider listed LAST
			Provides: []string{"libssl3"},
			URL:      "http://example.com/pool/main/o/openssl/libssl3t64_3.0.13-0ubuntu3.11_amd64.deb",
			Type:     "deb",
		},
	}

	got := findAllCandidates("libssl3", all)
	if len(got) == 0 {
		t.Fatal("expected at least one candidate for virtual package libssl3, got none")
	}
	if got[0].Name != "libssl3t64" || got[0].Version != "3.0.13-0ubuntu3.11" {
		t.Errorf("virtual libssl3 resolved to %s %s; want the newest provider libssl3t64 3.0.13-0ubuntu3.11",
			got[0].Name, got[0].Version)
	}
}

// TestAlternativeAlreadySelected_DistinctEdgesSharingFirstAlt guards against
// conflating two OR-edges that share the same first alternative. With
// "a | b" and "a | c" and only b selected, edge "a | c" is NOT satisfied, so a must
// still be pulled; the helper must not report a as skippable off the satisfied
// "a | b" edge alone.
func TestAlternativeAlreadySelected_DistinctEdgesSharingFirstAlt(t *testing.T) {
	reqVers := []string{"a | b", "a | c"}

	// Only b selected: "a | c" is unmet, so a is still required (not skippable).
	if alternativeAlreadySelected(reqVers, "a", func(name string) (string, bool) {
		if name == "b" {
			return "1.0", true
		}
		return "", false
	}) {
		t.Error("a reported skippable, but the 'a | c' edge is unsatisfied and needs a")
	}

	// Both b and c selected: every edge with first alternative a is met, so a is skippable.
	if !alternativeAlreadySelected(reqVers, "a", func(name string) (string, bool) {
		if name == "b" || name == "c" {
			return "1.0", true
		}
		return "", false
	}) {
		t.Error("a reported required, but both 'a | b' and 'a | c' are already satisfied")
	}
}

// TestAlternativeAlreadySelected_MandatoryDirectDepNotSkipped guards that a package
// listed BOTH as a bare direct dependency and as an OR first alternative
// ("Depends: a, a | b") is never skipped: even when b satisfies the "a | b" edge,
// the mandatory bare "a" term still requires a.
func TestAlternativeAlreadySelected_MandatoryDirectDepNotSkipped(t *testing.T) {
	reqVers := []string{"a", "a | b"}

	// b selected satisfies the OR edge, but the bare "a" term makes a mandatory.
	if alternativeAlreadySelected(reqVers, "a", func(name string) (string, bool) {
		if name == "b" {
			return "1.0", true
		}
		return "", false
	}) {
		t.Error("a reported skippable, but it is a mandatory direct dependency (\"Depends: a, a | b\")")
	}
}

// TestAlternativeAlreadySelected_HonoursVersionConstraint verifies the helper only
// treats a versioned alternative as satisfied when the selected version actually
// meets the constraint — the reason a bare name match is not enough.
func TestAlternativeAlreadySelected_HonoursVersionConstraint(t *testing.T) {
	reqVers := []string{"logsave | e2fsprogs (<< 1.45.3-1~)"}

	// e2fsprogs selected at 1.47.0 does NOT satisfy "<< 1.45.3-1~", so the "logsave"
	// edge is NOT satisfied and logsave must still be taken.
	if alternativeAlreadySelected(reqVers, "logsave", func(name string) (string, bool) {
		if name == "e2fsprogs" {
			return "1.47.0-2.4~exp1ubuntu4", true
		}
		return "", false
	}) {
		t.Error("edge reported satisfied, but the selected e2fsprogs version is outside the alternative's constraint")
	}

	// e2fsprogs selected at 1.45.2 DOES satisfy "<< 1.45.3-1~", so the edge is met.
	if !alternativeAlreadySelected(reqVers, "logsave", func(name string) (string, bool) {
		if name == "e2fsprogs" {
			return "1.45.2-1", true
		}
		return "", false
	}) {
		t.Error("edge reported unsatisfied, but the selected e2fsprogs version meets the alternative's constraint")
	}
}
