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
