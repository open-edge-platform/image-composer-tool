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

// TestResolveDependencies_OrDepSatisfiedByVersionedProvide is a regression
// test for alternativeAlreadySelected's selectedVersion callback: an
// already-selected package that satisfies an OR-dependency's alternative only
// via a versioned Provides: (not by being literally named after the
// alternative) must be checked against its DECLARED provided version, not
// treated as "selected with unknown version" (which a versioned alternative
// can never satisfy) nor against the provider's own unrelated Version.
func TestResolveDependencies_OrDepSatisfiedByVersionedProvide(t *testing.T) {
	all := []ospackage.PackageInfo{
		{
			Name:        "consumer",
			Version:     "1.0",
			Requires:    []string{"bar"}, // parser keeps only the first alternative
			RequiresVer: []string{"bar | foo (= 3.0)"},
			URL:         "http://example.com/pool/main/c/consumer/consumer_1.0_amd64.deb",
		},
		{
			Name:    "bar", // must NOT be pulled: the alternative is satisfied below
			Version: "1.0",
			URL:     "http://example.com/pool/main/b/bar/bar_1.0_amd64.deb",
		},
		{
			Name:        "libfoo-provider", // requested directly; provides foo at the required version
			Version:     "9.9.9",           // deliberately would NOT satisfy "foo (= 3.0)" if compared directly
			Provides:    []string{"foo"},
			ProvidesVer: []string{"foo (= 3.0)"},
			URL:         "http://example.com/pool/main/l/libfoo-provider/libfoo-provider_9.9.9_amd64.deb",
		},
	}

	requested := []ospackage.PackageInfo{all[0], all[2]} // consumer + libfoo-provider

	resolved, err := ResolveDependencies(requested, all)
	if err != nil {
		t.Fatalf("ResolveDependencies failed: %v", err)
	}

	var barSeen, providerSeen bool
	for _, p := range resolved {
		switch p.Name {
		case "bar":
			barSeen = true
		case "libfoo-provider":
			providerSeen = true
		}
	}
	if barSeen {
		t.Error("bar was pulled in; the already-selected libfoo-provider's versioned Provides: foo (= 3.0) should satisfy the OR-dependency")
	}
	if !providerSeen {
		t.Error("requested libfoo-provider is missing from the resolved set")
	}
}

// TestResolveDependencies_ConstraintReplacementDropsStaleQueuedVersion is a
// regression test for the version-conflict "replacement" path in the main
// resolution loop: resolvedDeps is populated when a dependency is QUEUED, not
// when it is actually dequeued/processed, so the old (constraint-violating)
// package can still be sitting unprocessed in queue when a replacement is
// chosen. Without removing that stale queued copy, it wins the neededSet race
// when dequeued (added to result under the shared package name before the
// replacement is), which then makes the correct replacement get silently
// skipped as "already seen" — the resolved set ends up with the WRONG
// (constraint-violating) version instead of the replacement.
//
// Both parents' constraints (">= 1.0" and ">= 2.0") overlap at 2.0, so this is
// a legitimate replacement, not a genuine conflict — see
// TestResolveDependencies_ConflictingConstraintsAcrossParentsReported for that
// case.
func TestResolveDependencies_ConstraintReplacementDropsStaleQueuedVersion(t *testing.T) {
	all := []ospackage.PackageInfo{
		{
			Name:        "parent1",
			Version:     "1.0",
			Requires:    []string{"libfoo"},
			RequiresVer: []string{"libfoo (>= 1.0)"},
			URL:         "http://example.com/pool/main/p/parent1/parent1_1.0_amd64.deb",
		},
		{
			Name:        "parent2",
			Version:     "1.0",
			Requires:    []string{"libfoo"},
			RequiresVer: []string{"libfoo (>= 2.0)"},
			URL:         "http://example.com/pool/main/p/parent2/parent2_1.0_amd64.deb",
		},
		{
			Name:    "libfoo", // queued first (satisfies parent1's ">= 1.0"), then must be replaced
			Version: "1.0",
			URL:     "http://example.com/pool/main/l/libfoo/libfoo_1.0_amd64.deb",
		},
		{
			Name:    "libfoo", // the correct replacement once parent2's ">= 2.0" is seen
			Version: "2.0",
			URL:     "http://example.com/pool/main/l/libfoo/libfoo_2.0_amd64.deb",
		},
	}

	// parent1 is processed first, queuing libfoo 1.0 (unprocessed) before parent2
	// is dequeued and triggers the 1.0 -> 2.0 replacement.
	requested := []ospackage.PackageInfo{all[0], all[1]}

	resolved, err := ResolveDependencies(requested, all)
	if err != nil {
		t.Fatalf("ResolveDependencies failed: %v", err)
	}

	var libfooVersions []string
	for _, p := range resolved {
		if p.Name == "libfoo" {
			libfooVersions = append(libfooVersions, p.Version)
		}
	}
	if len(libfooVersions) != 1 || libfooVersions[0] != "2.0" {
		t.Errorf("resolved libfoo versions = %v, want exactly [\"2.0\"] (the constraint-satisfying replacement)", libfooVersions)
	}
}

// TestResolveDependencies_ConflictingConstraintsAcrossParentsReported is a
// regression test for the replacement path only checking the CURRENT parent's
// constraint: parent1 pins libfoo to exactly 1.0 and parent2 requires
// >= 2.0 — genuinely incompatible on the same real package name. The resolver
// must report a conflict, not silently replace 1.0 with 2.0 and leave
// parent1's exact pin unsatisfied.
func TestResolveDependencies_ConflictingConstraintsAcrossParentsReported(t *testing.T) {
	all := []ospackage.PackageInfo{
		{
			Name:        "parent1",
			Version:     "1.0",
			Requires:    []string{"libfoo"},
			RequiresVer: []string{"libfoo (= 1.0)"},
			URL:         "http://example.com/pool/main/p/parent1/parent1_1.0_amd64.deb",
		},
		{
			Name:        "parent2",
			Version:     "1.0",
			Requires:    []string{"libfoo"},
			RequiresVer: []string{"libfoo (>= 2.0)"},
			URL:         "http://example.com/pool/main/p/parent2/parent2_1.0_amd64.deb",
		},
		{
			Name:    "libfoo",
			Version: "1.0",
			URL:     "http://example.com/pool/main/l/libfoo/libfoo_1.0_amd64.deb",
		},
		{
			Name:    "libfoo",
			Version: "2.0",
			URL:     "http://example.com/pool/main/l/libfoo/libfoo_2.0_amd64.deb",
		},
	}

	requested := []ospackage.PackageInfo{all[0], all[1]}

	if _, err := ResolveDependencies(requested, all); err == nil {
		t.Fatal("expected a conflict error for incompatible libfoo constraints (= 1.0 vs >= 2.0), got none")
	}
}

// TestResolveDependencies_ExactConstraintReplacementNotBlockedIfCompatible is a
// regression test for treating "any recorded constraint is exact" as an
// automatic block on replacement: parentA's ">= 1.0" first selects libfoo 3.0
// (listed first, so it wins the no-conflict tie-break); parentB later requires
// exactly "= 2.0". Version 2.0 satisfies BOTH constraints, so this is a
// legitimate replacement — gating the search on "an exact constraint exists"
// would reject 2.0 before ever checking whether it actually works.
func TestResolveDependencies_ExactConstraintReplacementNotBlockedIfCompatible(t *testing.T) {
	all := []ospackage.PackageInfo{
		{
			Name:        "parentA",
			Version:     "1.0",
			Requires:    []string{"libfoo"},
			RequiresVer: []string{"libfoo (>= 1.0)"},
			URL:         "http://example.com/pool/main/p/parenta/parentA_1.0_amd64.deb",
		},
		{
			Name:        "parentB",
			Version:     "1.0",
			Requires:    []string{"libfoo"},
			RequiresVer: []string{"libfoo (= 2.0)"},
			URL:         "http://example.com/pool/main/p/parentb/parentB_1.0_amd64.deb",
		},
		{
			Name:    "libfoo", // listed first: satisfies ">= 1.0", so parentA's initial pick
			Version: "3.0",
			URL:     "http://example.com/pool/main/l/libfoo/libfoo_3.0_amd64.deb",
		},
		{
			Name:    "libfoo",
			Version: "1.0",
			URL:     "http://example.com/pool/main/l/libfoo/libfoo_1.0_amd64.deb",
		},
		{
			Name:    "libfoo", // satisfies both ">= 1.0" and "= 2.0"
			Version: "2.0",
			URL:     "http://example.com/pool/main/l/libfoo/libfoo_2.0_amd64.deb",
		},
	}

	requested := []ospackage.PackageInfo{all[0], all[1]}

	resolved, err := ResolveDependencies(requested, all)
	if err != nil {
		t.Fatalf("expected libfoo 2.0 to satisfy both parentA and parentB, got error: %v", err)
	}

	var libfooVersions []string
	for _, p := range resolved {
		if p.Name == "libfoo" {
			libfooVersions = append(libfooVersions, p.Version)
		}
	}
	if len(libfooVersions) != 1 || libfooVersions[0] != "2.0" {
		t.Errorf("resolved libfoo versions = %v, want exactly [\"2.0\"]", libfooVersions)
	}
}

// TestResolveDependencies_ReplacementPreservesIndependentlyRequiredProvider is
// a regression test for the constraint-driven replacement path unconditionally
// removing the replaced package from the resolved closure: old-provider
// Provides BOTH "shared-abi" and "old-only-capability". parentX depends
// directly on "old-only-capability" (resolved to old-provider) before
// parentY's stricter "shared-abi (>= 2.0)" forces replacing old-provider with
// new-provider for shared-abi. new-provider does not provide
// "old-only-capability", so old-provider must remain in the resolved set to
// keep satisfying parentX — removing it outright would silently leave
// parentX's dependency unsatisfied without ever re-resolving it.
func TestResolveDependencies_ReplacementPreservesIndependentlyRequiredProvider(t *testing.T) {
	all := []ospackage.PackageInfo{
		{
			Name:     "parentX",
			Version:  "1.0",
			Requires: []string{"old-only-capability"},
			URL:      "http://example.com/pool/main/p/parentx/parentX_1.0_amd64.deb",
		},
		{
			Name:        "parentZ",
			Version:     "1.0",
			Requires:    []string{"shared-abi"},
			RequiresVer: []string{"shared-abi (>= 1.0)"},
			URL:         "http://example.com/pool/main/p/parentz/parentZ_1.0_amd64.deb",
		},
		{
			Name:        "parentY",
			Version:     "1.0",
			Requires:    []string{"shared-abi"},
			RequiresVer: []string{"shared-abi (>= 2.0)"},
			URL:         "http://example.com/pool/main/p/parenty/parentY_1.0_amd64.deb",
		},
		{
			Name:        "old-provider",
			Version:     "1.0",
			Provides:    []string{"shared-abi", "old-only-capability"},
			ProvidesVer: []string{"shared-abi (= 1.0)", "old-only-capability (= 1.0)"},
			URL:         "http://example.com/pool/main/o/old-provider/old-provider_1.0_amd64.deb",
		},
		{
			Name:        "new-provider", // does NOT provide old-only-capability
			Version:     "1.0",
			Provides:    []string{"shared-abi"},
			ProvidesVer: []string{"shared-abi (= 2.0)"},
			URL:         "http://example.com/pool/main/n/new-provider/new-provider_1.0_amd64.deb",
		},
	}

	// parentX resolves old-only-capability (to old-provider) before parentZ and
	// parentY are processed and force the shared-abi replacement.
	requested := []ospackage.PackageInfo{all[0], all[1], all[2]}

	resolved, err := ResolveDependencies(requested, all)
	if err != nil {
		t.Fatalf("ResolveDependencies failed: %v", err)
	}

	var sawOldProvider, sawNewProvider bool
	for _, p := range resolved {
		switch p.Name {
		case "old-provider":
			sawOldProvider = true
		case "new-provider":
			sawNewProvider = true
		}
	}
	if !sawOldProvider {
		t.Error("old-provider is missing from the resolved set; parentX's dependency on old-only-capability must still be satisfied after old-provider was replaced for shared-abi")
	}
	if !sawNewProvider {
		t.Error("new-provider is missing from the resolved set; parentY's stricter shared-abi constraint must still be satisfied")
	}
}

// TestResolveDependencies_ReplacementRemovesProviderWithNoGenuineRequirement
// is a regression test for packageStillRequired treating
// rememberResolvedDependency's own-name bookkeeping alias as evidence of an
// independent requirement: that alias is set for EVERY selected provider
// regardless of whether anything ever actually depends on it by name. Here
// NOTHING depends on "old-provider" directly — only the virtual "shared-abi"
// capability is required, which a stricter later constraint reassigns to
// "new-provider" — so old-provider must be fully removed from the minimal
// closure, not incorrectly retained forever via its own bookkeeping alias.
func TestResolveDependencies_ReplacementRemovesProviderWithNoGenuineRequirement(t *testing.T) {
	all := []ospackage.PackageInfo{
		{
			Name:        "parent1",
			Version:     "1.0",
			Requires:    []string{"shared-abi"},
			RequiresVer: []string{"shared-abi (>= 1.0)"},
			URL:         "http://example.com/pool/main/p/parent1/parent1_1.0_amd64.deb",
		},
		{
			Name:        "parent2",
			Version:     "1.0",
			Requires:    []string{"shared-abi"},
			RequiresVer: []string{"shared-abi (>= 2.0)"},
			URL:         "http://example.com/pool/main/p/parent2/parent2_1.0_amd64.deb",
		},
		{
			Name:        "old-provider", // nothing depends on this name directly
			Version:     "1.0",
			Provides:    []string{"shared-abi"},
			ProvidesVer: []string{"shared-abi (= 1.0)"},
			URL:         "http://example.com/pool/main/o/old-provider/old-provider_1.0_amd64.deb",
		},
		{
			Name:        "new-provider",
			Version:     "1.0",
			Provides:    []string{"shared-abi"},
			ProvidesVer: []string{"shared-abi (= 2.0)"},
			URL:         "http://example.com/pool/main/n/new-provider/new-provider_1.0_amd64.deb",
		},
	}

	requested := []ospackage.PackageInfo{all[0], all[1]}

	resolved, err := ResolveDependencies(requested, all)
	if err != nil {
		t.Fatalf("ResolveDependencies failed: %v", err)
	}

	var sawOldProvider, sawNewProvider bool
	for _, p := range resolved {
		switch p.Name {
		case "old-provider":
			sawOldProvider = true
		case "new-provider":
			sawNewProvider = true
		}
	}
	if sawOldProvider {
		t.Error("old-provider is present in the resolved set, but nothing genuinely depends on it by name; only its own rememberResolvedDependency bookkeeping alias should not keep it")
	}
	if !sawNewProvider {
		t.Error("new-provider is missing from the resolved set; parent2's stricter shared-abi constraint must be satisfied")
	}
}

// TestResolveDependencies_ORSkipDoesNotMarkUnpulledAlternativeAsRequired is a
// regression test for recording depName as genuinely required BEFORE checking
// whether its OR edge is already satisfied by another alternative: for
// "old-provider | other-seed" with other-seed already selected (a directly
// requested seed), old-provider is never actually pulled in by this edge —
// other-seed is. Marking old-provider required anyway lets it survive a later,
// unrelated replacement (parent1/parent2's shared-abi tug-of-war, identical to
// TestResolveDependencies_ReplacementRemovesProviderWithNoGenuineRequirement)
// solely because of this bookkeeping-adjacent false positive, even though
// nothing genuinely depends on old-provider by name.
func TestResolveDependencies_ORSkipDoesNotMarkUnpulledAlternativeAsRequired(t *testing.T) {
	all := []ospackage.PackageInfo{
		{
			Name:        "parent1",
			Version:     "1.0",
			Requires:    []string{"shared-abi"},
			RequiresVer: []string{"shared-abi (>= 1.0)"},
			URL:         "http://example.com/pool/main/p/parent1/parent1_1.0_amd64.deb",
		},
		{
			Name:     "consumerD", // OR edge satisfied by other-seed, NOT old-provider
			Version:  "1.0",
			Requires: []string{"old-provider"},
			// parser keeps only the first alternative in Requires
			RequiresVer: []string{"old-provider | other-seed"},
			URL:         "http://example.com/pool/main/c/consumerd/consumerD_1.0_amd64.deb",
		},
		{
			Name:        "parent2",
			Version:     "1.0",
			Requires:    []string{"shared-abi"},
			RequiresVer: []string{"shared-abi (>= 2.0)"},
			URL:         "http://example.com/pool/main/p/parent2/parent2_1.0_amd64.deb",
		},
		{
			Name:    "other-seed", // requested directly, satisfies consumerD's OR edge
			Version: "1.0",
			URL:     "http://example.com/pool/main/o/other-seed/other-seed_1.0_amd64.deb",
		},
		{
			Name:        "old-provider", // nothing genuinely depends on this name directly
			Version:     "1.0",
			Provides:    []string{"shared-abi"},
			ProvidesVer: []string{"shared-abi (= 1.0)"},
			URL:         "http://example.com/pool/main/o/old-provider/old-provider_1.0_amd64.deb",
		},
		{
			Name:        "new-provider",
			Version:     "1.0",
			Provides:    []string{"shared-abi"},
			ProvidesVer: []string{"shared-abi (= 2.0)"},
			URL:         "http://example.com/pool/main/n/new-provider/new-provider_1.0_amd64.deb",
		},
	}

	// parent1 is processed first (selects old-provider for shared-abi), then
	// consumerD (whose OR edge on old-provider is skipped in favor of the
	// already-selected other-seed), then parent2 (forces the shared-abi
	// replacement that must not be blocked by consumerD's skipped edge).
	requested := []ospackage.PackageInfo{all[0], all[1], all[2], all[3]}

	resolved, err := ResolveDependencies(requested, all)
	if err != nil {
		t.Fatalf("ResolveDependencies failed: %v", err)
	}

	var sawOldProvider, sawNewProvider bool
	for _, p := range resolved {
		switch p.Name {
		case "old-provider":
			sawOldProvider = true
		case "new-provider":
			sawNewProvider = true
		}
	}
	if sawOldProvider {
		t.Error("old-provider is present in the resolved set; consumerD's OR edge was satisfied by other-seed, not old-provider, so old-provider must not be treated as genuinely required")
	}
	if !sawNewProvider {
		t.Error("new-provider is missing from the resolved set; parent2's stricter shared-abi constraint must be satisfied")
	}
}

// TestResolveDependencies_ReplacementDoesNotStrandUnrelatedAlias is a
// regression test for replaceQueuedAndAliasedDependency blindly repointing
// EVERY resolvedDeps alias of the replaced package: libfoo-old and libfoo-new
// both Provide the virtual "libfoo-abi" at different versions, so parent2's
// stricter constraint forces a replacement of libfoo-old with libfoo-new. A
// third package depends directly on "libfoo-old" by its real name (not the
// virtual capability) — libfoo-new does not provide that name, so the
// dependency must trigger a fresh resolution (pulling libfoo-old back in on
// its own merits) rather than being wrongly treated as already satisfied by
// libfoo-new.
func TestResolveDependencies_ReplacementDoesNotStrandUnrelatedAlias(t *testing.T) {
	all := []ospackage.PackageInfo{
		{
			Name:        "parent1",
			Version:     "1.0",
			Requires:    []string{"libfoo-abi"},
			RequiresVer: []string{"libfoo-abi (>= 1.0)"},
			URL:         "http://example.com/pool/main/p/parent1/parent1_1.0_amd64.deb",
		},
		{
			Name:        "parent2",
			Version:     "1.0",
			Requires:    []string{"libfoo-abi"},
			RequiresVer: []string{"libfoo-abi (>= 2.0)"},
			URL:         "http://example.com/pool/main/p/parent2/parent2_1.0_amd64.deb",
		},
		{
			Name:     "consumer3", // depends directly on the OLD provider's real name
			Version:  "1.0",
			Requires: []string{"libfoo-old"},
			URL:      "http://example.com/pool/main/c/consumer3/consumer3_1.0_amd64.deb",
		},
		{
			Name:        "libfoo-old",
			Version:     "1.0",
			Provides:    []string{"libfoo-abi"},
			ProvidesVer: []string{"libfoo-abi (= 1.0)"},
			URL:         "http://example.com/pool/main/l/libfoo-old/libfoo-old_1.0_amd64.deb",
		},
		{
			Name:        "libfoo-new",
			Version:     "1.0",
			Provides:    []string{"libfoo-abi"},
			ProvidesVer: []string{"libfoo-abi (= 2.0)"},
			URL:         "http://example.com/pool/main/l/libfoo-new/libfoo-new_1.0_amd64.deb",
		},
	}

	requested := []ospackage.PackageInfo{all[0], all[1], all[2]}

	resolved, err := ResolveDependencies(requested, all)
	if err != nil {
		t.Fatalf("ResolveDependencies failed: %v", err)
	}

	var sawOld bool
	for _, p := range resolved {
		if p.Name == "libfoo-old" {
			sawOld = true
		}
	}
	if !sawOld {
		t.Error("libfoo-old is missing from the resolved set; consumer3's direct dependency on it must still be satisfied after libfoo-abi was replaced with libfoo-new")
	}
}

// TestResolveDependencies_AlternativeResolutionRecordsConstraint is a
// regression test for the OR-dependency alternative-resolution fallback (used
// when the primary alternative has no candidates at all) not recording the
// chosen alternative's own version constraint: consumer1 depends on
// "bar | foo (<< 2.0)" — bar has no candidates whatsoever, so this reduces to
// requiring foo << 2.0, satisfied by picking foo 1.0. consumer2 separately
// requires foo (>= 2.0). No single foo version can satisfy both, so this must
// be reported as a conflict — not silently "resolved" by replacing foo 1.0
// with foo 3.0 (which satisfies consumer2 but leaves consumer1 unsatisfied)
// because consumer1's constraint was never recorded in the first place.
func TestResolveDependencies_AlternativeResolutionRecordsConstraint(t *testing.T) {
	all := []ospackage.PackageInfo{
		{
			Name:        "consumer1",
			Version:     "1.0",
			Requires:    []string{"bar"}, // bar has no real or virtual candidates at all
			RequiresVer: []string{"bar | foo (<< 2.0)"},
			URL:         "http://example.com/pool/main/c/consumer1/consumer1_1.0_amd64.deb",
		},
		{
			Name:        "consumer2",
			Version:     "1.0",
			Requires:    []string{"foo"},
			RequiresVer: []string{"foo (>= 2.0)"},
			URL:         "http://example.com/pool/main/c/consumer2/consumer2_1.0_amd64.deb",
		},
		{
			Name:    "foo",
			Version: "1.0",
			URL:     "http://example.com/pool/main/f/foo/foo_1.0_amd64.deb",
		},
		{
			Name:    "foo",
			Version: "3.0",
			URL:     "http://example.com/pool/main/f/foo/foo_3.0_amd64.deb",
		},
	}

	requested := []ospackage.PackageInfo{all[0], all[1]}

	if _, err := ResolveDependencies(requested, all); err == nil {
		t.Fatal("expected a conflict: consumer1's fallback alternative needs foo << 2.0, consumer2 needs foo >= 2.0, and no single foo version satisfies both")
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
	if alternativeAlreadySelected(reqVers, "a", func(name string) []string {
		if name == "b" {
			return []string{"1.0"}
		}
		return nil
	}) {
		t.Error("a reported skippable, but the 'a | c' edge is unsatisfied and needs a")
	}

	// Both b and c selected: every edge with first alternative a is met, so a is skippable.
	if !alternativeAlreadySelected(reqVers, "a", func(name string) []string {
		if name == "b" || name == "c" {
			return []string{"1.0"}
		}
		return nil
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
	if alternativeAlreadySelected(reqVers, "a", func(name string) []string {
		if name == "b" {
			return []string{"1.0"}
		}
		return nil
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
	if alternativeAlreadySelected(reqVers, "logsave", func(name string) []string {
		if name == "e2fsprogs" {
			return []string{"1.47.0-2.4~exp1ubuntu4"}
		}
		return nil
	}) {
		t.Error("edge reported satisfied, but the selected e2fsprogs version is outside the alternative's constraint")
	}

	// e2fsprogs selected at 1.45.2 DOES satisfy "<< 1.45.3-1~", so the edge is met.
	if !alternativeAlreadySelected(reqVers, "logsave", func(name string) []string {
		if name == "e2fsprogs" {
			return []string{"1.45.2-1"}
		}
		return nil
	}) {
		t.Error("edge reported unsatisfied, but the selected e2fsprogs version meets the alternative's constraint")
	}
}

// TestResolveMultiCandidates_PicksProviderByProvidedVersion covers the fix for
// resolveMultiCandidates deriving its version-constraint lookup key from
// candidates[0].Name instead of the real (possibly virtual) dependency name:
// when a parent has an exact-version constraint on a virtual capability that
// several different real packages Provide at different declared versions,
// the candidate whose *provided* version matches must be chosen — not
// whichever the constraint lookup happened to match by the providers' own,
// unrelated package names/versions.
func TestResolveMultiCandidates_PicksProviderByProvidedVersion(t *testing.T) {
	parent := ospackage.PackageInfo{
		Name:        "consumer",
		Version:     "1.0",
		RequiresVer: []string{"libfoo-abi (= 3.0)"},
		URL:         "http://example.com/pool/main/c/consumer/consumer_1.0_amd64.deb",
	}
	providerOld := ospackage.PackageInfo{
		Name:        "libfoo-old",
		Version:     "9.9.9", // deliberately "newer"-looking own version, but provides the WRONG abi version
		Provides:    []string{"libfoo-abi"},
		ProvidesVer: []string{"libfoo-abi (= 2.0)"},
		URL:         "http://example.com/pool/main/l/libfoo-old/libfoo-old_9.9.9_amd64.deb",
	}
	providerNew := ospackage.PackageInfo{
		Name:        "libfoo-new",
		Version:     "1.0.0", // deliberately "older"-looking own version, but provides the correct abi version
		Provides:    []string{"libfoo-abi"},
		ProvidesVer: []string{"libfoo-abi (= 3.0)"},
		URL:         "http://example.com/pool/main/l/libfoo-new/libfoo-new_1.0.0_amd64.deb",
	}

	chosen, err := resolveMultiCandidates(parent, "libfoo-abi", []ospackage.PackageInfo{providerOld, providerNew})
	if err != nil {
		t.Fatalf("expected a candidate satisfying libfoo-abi (= 3.0), got error: %v", err)
	}
	if chosen.Name != "libfoo-new" {
		t.Errorf("chosen provider = %q, want %q (the one actually providing libfoo-abi = 3.0)", chosen.Name, "libfoo-new")
	}
}

// TestResolveMultiCandidates_UnversionedProvideDoesNotSatisfyVersionedDep is a
// regression test: per Debian policy, an unversioned Provides (e.g. plain
// "Provides: libfoo-abi", no "(= X)") never satisfies a versioned dependency,
// even when the providing package's own Version would numerically pass the
// constraint. versionForDependency must report "no usable version" for this
// case rather than falling back to the provider's own, unrelated Version.
func TestResolveMultiCandidates_UnversionedProvideDoesNotSatisfyVersionedDep(t *testing.T) {
	parent := ospackage.PackageInfo{
		Name:        "consumer",
		Version:     "1.0",
		RequiresVer: []string{"libfoo-abi (>= 2.0)"},
		URL:         "http://example.com/pool/main/c/consumer/consumer_1.0_amd64.deb",
	}
	// Own Version (9.9.9) would satisfy ">= 2.0" numerically, but the
	// Provides: line for libfoo-abi carries no version at all.
	unversionedProvider := ospackage.PackageInfo{
		Name:        "libfoo-unversioned",
		Version:     "9.9.9",
		Provides:    []string{"libfoo-abi"},
		ProvidesVer: []string{"libfoo-abi"},
		URL:         "http://example.com/pool/main/l/libfoo-unversioned/libfoo-unversioned_9.9.9_amd64.deb",
	}

	if _, err := resolveMultiCandidates(parent, "libfoo-abi", []ospackage.PackageInfo{unversionedProvider}); err == nil {
		t.Fatal("expected an unversioned Provides to fail to satisfy a versioned dependency, got no error")
	}
}

// TestResolveDependencies_StaleOrEdgeConstraintDoesNotBlockUnrelatedReplacement
// is a regression test for the constraint-filtering loop enforcing an OR-edge
// constraint that has since become obsolete: consumer1 depends on
// "foo (= 1.0) | bar", which records an accumulated constraint on foo tagged
// with Alternative "bar" (foo's own initial selection ignores this OR
// constraint entirely and picks the newest candidate, 3.0, since a direct
// dependency's alternative-tagged constraints are not applied to its own
// pick). consumer2 independently depends directly on bar, which gets
// resolved on its own merits — satisfying consumer1's OR edge via the
// alternative, NOT via foo. consumer3 then separately requires
// "foo (= 2.0)" (nothing to do with consumer1's edge), forcing a real
// replacement of foo 3.0 with foo 2.0. Since consumer1's edge is already met
// by bar, its stale "= 1.0" constraint on foo must not be enforced against
// consumer3's unrelated requirement — foo must be replaced with 2.0, not
// reported as a conflict.
func TestResolveDependencies_StaleOrEdgeConstraintDoesNotBlockUnrelatedReplacement(t *testing.T) {
	all := []ospackage.PackageInfo{
		{
			Name:        "consumer1",
			Version:     "1.0",
			Requires:    []string{"foo"}, // parser keeps only the first alternative
			RequiresVer: []string{"foo (= 1.0) | bar"},
			URL:         "http://example.com/pool/main/c/consumer1/consumer1_1.0_amd64.deb",
		},
		{
			Name:     "consumer2",
			Version:  "1.0",
			Requires: []string{"bar"},
			URL:      "http://example.com/pool/main/c/consumer2/consumer2_1.0_amd64.deb",
		},
		{
			Name:        "consumer3",
			Version:     "1.0",
			Requires:    []string{"foo"},
			RequiresVer: []string{"foo (= 2.0)"},
			URL:         "http://example.com/pool/main/c/consumer3/consumer3_1.0_amd64.deb",
		},
		{
			Name:    "bar",
			Version: "1.0",
			URL:     "http://example.com/pool/main/b/bar/bar_1.0_amd64.deb",
		},
		{
			Name:    "foo", // the newest candidate: consumer1's unconstrained initial pick
			Version: "3.0",
			URL:     "http://example.com/pool/main/f/foo/foo_3.0_amd64.deb",
		},
		{
			Name:    "foo", // what consumer3 actually needs
			Version: "2.0",
			URL:     "http://example.com/pool/main/f/foo/foo_2.0_amd64.deb",
		},
	}

	// consumer1 is processed first (records the stale foo (= 1.0) | bar
	// constraint and initially selects foo 3.0), then consumer2 (resolves
	// bar, satisfying that OR edge), then consumer3 (forces the foo 3.0 ->
	// 2.0 replacement that must not be blocked by the now-obsolete
	// constraint).
	requested := []ospackage.PackageInfo{all[0], all[1], all[2]}

	resolved, err := ResolveDependencies(requested, all)
	if err != nil {
		t.Fatalf("expected foo to be replaceable with 2.0 once consumer1's OR edge is satisfied via bar, got error: %v", err)
	}

	var fooVersions []string
	for _, p := range resolved {
		if p.Name == "foo" {
			fooVersions = append(fooVersions, p.Version)
		}
	}
	if len(fooVersions) != 1 || fooVersions[0] != "2.0" {
		t.Errorf("resolved foo versions = %v, want exactly [\"2.0\"]", fooVersions)
	}
}

// TestResolveDependencies_StaleOrEdgeConstraintRecognizesVirtualAlternative is
// a regression test for the stale-OR-edge-constraint skip using a narrower
// "already selected" lookup than alternativeAlreadySelected: it only checked
// resolvedDeps/seedByName keyed directly by the alternative's own name, not
// packages that provide it virtually. Here "bar" is never resolved under its
// own name at all — only real-bar-pkg Provides it — so the constraint on foo
// tagged Alternative "bar" can only be recognized as obsolete via the same
// provider-aware lookup alternativeAlreadySelected uses. Otherwise consumer3's
// unrelated foo (= 2.0) requirement is falsely reported as conflicting.
func TestResolveDependencies_StaleOrEdgeConstraintRecognizesVirtualAlternative(t *testing.T) {
	all := []ospackage.PackageInfo{
		{
			Name:        "consumer1",
			Version:     "1.0",
			Requires:    []string{"foo"}, // parser keeps only the first alternative
			RequiresVer: []string{"foo (= 1.0) | bar"},
			URL:         "http://example.com/pool/main/c/consumer1/consumer1_1.0_amd64.deb",
		},
		{
			Name:     "consumer2",
			Version:  "1.0",
			Requires: []string{"real-bar-pkg"}, // resolves real-bar-pkg, never "bar" directly
			URL:      "http://example.com/pool/main/c/consumer2/consumer2_1.0_amd64.deb",
		},
		{
			Name:        "consumer3",
			Version:     "1.0",
			Requires:    []string{"foo"},
			RequiresVer: []string{"foo (= 2.0)"},
			URL:         "http://example.com/pool/main/c/consumer3/consumer3_1.0_amd64.deb",
		},
		{
			Name:     "real-bar-pkg", // provides the "bar" capability virtually
			Version:  "1.0",
			Provides: []string{"bar"},
			URL:      "http://example.com/pool/main/r/real-bar-pkg/real-bar-pkg_1.0_amd64.deb",
		},
		{
			Name:    "foo", // the newest candidate: consumer1's unconstrained initial pick
			Version: "3.0",
			URL:     "http://example.com/pool/main/f/foo/foo_3.0_amd64.deb",
		},
		{
			Name:    "foo", // what consumer3 actually needs
			Version: "2.0",
			URL:     "http://example.com/pool/main/f/foo/foo_2.0_amd64.deb",
		},
	}

	requested := []ospackage.PackageInfo{all[0], all[1], all[2]}

	resolved, err := ResolveDependencies(requested, all)
	if err != nil {
		t.Fatalf("expected foo to be replaceable with 2.0 once consumer1's OR edge is satisfied via real-bar-pkg's virtual bar, got error: %v", err)
	}

	var fooVersions []string
	for _, p := range resolved {
		if p.Name == "foo" {
			fooVersions = append(fooVersions, p.Version)
		}
	}
	if len(fooVersions) != 1 || fooVersions[0] != "2.0" {
		t.Errorf("resolved foo versions = %v, want exactly [\"2.0\"]", fooVersions)
	}
}

// TestResolveDependencies_ConstraintReplacementRespectsAlternativeVersionConstraint
// is a regression test for treating an OR edge as obsolete based on an
// alternative's bare NAME being selected, ignoring that alternative's OWN
// version constraint: "foo (= 1.0) | bar (>= 2.0)" is only satisfied by bar
// when the SELECTED bar actually meets ">= 2.0". Here bar is selected at 1.0,
// which does not satisfy ">= 2.0", so the edge remains unsatisfied and foo's
// "= 1.0" pin from that edge is still genuinely binding — consumer3's separate
// "foo (= 2.0)" requirement conflicts with it and must be reported as an
// error, not silently "resolved" by replacing foo with 2.0.
func TestResolveDependencies_ConstraintReplacementRespectsAlternativeVersionConstraint(t *testing.T) {
	all := []ospackage.PackageInfo{
		{
			Name:        "consumer1",
			Version:     "1.0",
			Requires:    []string{"foo"}, // parser keeps only the first alternative
			RequiresVer: []string{"foo (= 1.0) | bar (>= 2.0)"},
			URL:         "http://example.com/pool/main/c/consumer1/consumer1_1.0_amd64.deb",
		},
		{
			Name:     "consumer2",
			Version:  "1.0",
			Requires: []string{"bar"},
			URL:      "http://example.com/pool/main/c/consumer2/consumer2_1.0_amd64.deb",
		},
		{
			Name:        "consumer3",
			Version:     "1.0",
			Requires:    []string{"foo"},
			RequiresVer: []string{"foo (= 2.0)"},
			URL:         "http://example.com/pool/main/c/consumer3/consumer3_1.0_amd64.deb",
		},
		{
			Name:    "bar", // selected, but at a version that does NOT satisfy ">= 2.0"
			Version: "1.0",
			URL:     "http://example.com/pool/main/b/bar/bar_1.0_amd64.deb",
		},
		{
			Name:    "foo", // the newest candidate: consumer1's unconstrained initial pick
			Version: "3.0",
			URL:     "http://example.com/pool/main/f/foo/foo_3.0_amd64.deb",
		},
		{
			Name:    "foo",
			Version: "2.0",
			URL:     "http://example.com/pool/main/f/foo/foo_2.0_amd64.deb",
		},
	}

	requested := []ospackage.PackageInfo{all[0], all[1], all[2]}

	if _, err := ResolveDependencies(requested, all); err == nil {
		t.Fatal("expected a conflict: bar (1.0) does not satisfy \"bar (>= 2.0)\", so foo's \"= 1.0\" pin from that edge still conflicts with consumer3's \"foo (= 2.0)\"")
	}
}

// TestResolveDependencies_ReplacementPreservesRequestedSeedUnderDifferentName
// is a regression test for packageStillRequired ignoring explicitly requested
// seeds entirely: old-provider is requested directly BY NAME (a seed) and
// also Provides shared-abi (= 1.0). parent1 initially resolves shared-abi to
// old-provider; parent2's stricter "shared-abi (>= 2.0)" then forces a
// replacement with a DIFFERENTLY named new-provider. Nothing in
// requiredDepNames ever points at "old-provider" (it was never looked up as a
// real Requires edge, only pulled in as a seed), so old-provider must be kept
// specifically BECAUSE it is a requested seed, not via the requiredDepNames
// alias check alone.
func TestResolveDependencies_ReplacementPreservesRequestedSeedUnderDifferentName(t *testing.T) {
	all := []ospackage.PackageInfo{
		{
			Name:        "old-provider", // requested directly by name below
			Version:     "1.0",
			Provides:    []string{"shared-abi"},
			ProvidesVer: []string{"shared-abi (= 1.0)"},
			URL:         "http://example.com/pool/main/o/old-provider/old-provider_1.0_amd64.deb",
		},
		{
			Name:        "parent1",
			Version:     "1.0",
			Requires:    []string{"shared-abi"},
			RequiresVer: []string{"shared-abi (>= 1.0)"},
			URL:         "http://example.com/pool/main/p/parent1/parent1_1.0_amd64.deb",
		},
		{
			Name:        "parent2",
			Version:     "1.0",
			Requires:    []string{"shared-abi"},
			RequiresVer: []string{"shared-abi (>= 2.0)"},
			URL:         "http://example.com/pool/main/p/parent2/parent2_1.0_amd64.deb",
		},
		{
			Name:        "new-provider",
			Version:     "1.0",
			Provides:    []string{"shared-abi"},
			ProvidesVer: []string{"shared-abi (= 2.0)"},
			URL:         "http://example.com/pool/main/n/new-provider/new-provider_1.0_amd64.deb",
		},
	}

	// old-provider is requested directly (a seed), alongside parent1/parent2.
	requested := []ospackage.PackageInfo{all[0], all[1], all[2]}

	resolved, err := ResolveDependencies(requested, all)
	if err != nil {
		t.Fatalf("ResolveDependencies failed: %v", err)
	}

	var sawOldProvider, sawNewProvider bool
	for _, p := range resolved {
		switch p.Name {
		case "old-provider":
			sawOldProvider = true
		case "new-provider":
			sawNewProvider = true
		}
	}
	if !sawOldProvider {
		t.Error("old-provider is missing from the resolved set; it was explicitly requested by name and must not be dropped just because shared-abi was reassigned to a differently named provider")
	}
	if !sawNewProvider {
		t.Error("new-provider is missing from the resolved set; parent2's stricter shared-abi constraint must be satisfied")
	}
}

// TestResolveDependencies_ORDependencyChecksAllSelectedProvidersOfAlternative
// is a regression test for a selected-version lookup that collapses multiple
// selected providers of the same virtual name to a single arbitrary match
// (e.g. the first hit while ranging over a map, which has nondeterministic
// iteration order): for "bar | foo (= 3.0)", both foo-provider-v2 (Provides
// foo = 2.0) and foo-provider-v3 (Provides foo = 3.0) are already selected
// before consumer1 is processed. Only foo-provider-v3 actually satisfies
// "foo (= 3.0)", so the edge must be recognized as satisfied by checking
// EVERY selected provider of "foo", not whichever one is found first.
func TestResolveDependencies_ORDependencyChecksAllSelectedProvidersOfAlternative(t *testing.T) {
	all := []ospackage.PackageInfo{
		{
			Name:     "consumer2",
			Version:  "1.0",
			Requires: []string{"foo-provider-v2"},
			URL:      "http://example.com/pool/main/c/consumer2/consumer2_1.0_amd64.deb",
		},
		{
			Name:     "consumer3",
			Version:  "1.0",
			Requires: []string{"foo-provider-v3"},
			URL:      "http://example.com/pool/main/c/consumer3/consumer3_1.0_amd64.deb",
		},
		{
			Name:        "consumer1",
			Version:     "1.0",
			Requires:    []string{"bar"}, // parser keeps only the first alternative
			RequiresVer: []string{"bar | foo (= 3.0)"},
			URL:         "http://example.com/pool/main/c/consumer1/consumer1_1.0_amd64.deb",
		},
		{
			Name:        "foo-provider-v2",
			Version:     "1.0",
			Provides:    []string{"foo"},
			ProvidesVer: []string{"foo (= 2.0)"},
			URL:         "http://example.com/pool/main/f/foo-provider-v2/foo-provider-v2_1.0_amd64.deb",
		},
		{
			Name:        "foo-provider-v3",
			Version:     "1.0",
			Provides:    []string{"foo"},
			ProvidesVer: []string{"foo (= 3.0)"},
			URL:         "http://example.com/pool/main/f/foo-provider-v3/foo-provider-v3_1.0_amd64.deb",
		},
		{
			Name:    "bar", // must NOT be pulled: foo-provider-v3 already satisfies the edge
			Version: "1.0",
			URL:     "http://example.com/pool/main/b/bar/bar_1.0_amd64.deb",
		},
	}

	// consumer2 and consumer3 are processed first, so both foo providers are
	// already selected by the time consumer1's OR edge is evaluated.
	requested := []ospackage.PackageInfo{all[0], all[1], all[2]}

	resolved, err := ResolveDependencies(requested, all)
	if err != nil {
		t.Fatalf("ResolveDependencies failed: %v", err)
	}

	for _, p := range resolved {
		if p.Name == "bar" {
			t.Error("bar was pulled in; foo-provider-v3's selected foo (= 3.0) already satisfies the \"foo (= 3.0)\" alternative")
		}
	}
}

// TestResolveDependencies_ReplacementRefreshesStaleSeedVersion is a
// regression test for seedByName not being updated after a same-name
// replacement: parentA forces the requested seed bar (1.0) to be replaced
// with bar (2.0) — a legitimate same-name version bump. seedByName must be
// refreshed to bar (2.0), or a later "foo | bar (= 1.0)" edge would be
// wrongly considered satisfied by the STALE seed entry still reporting
// version 1.0, even though the actually resolved bar is now 2.0 — leaving
// foo unpulled and the returned closure missing a genuine requirement.
func TestResolveDependencies_ReplacementRefreshesStaleSeedVersion(t *testing.T) {
	all := []ospackage.PackageInfo{
		{
			Name:    "bar", // requested directly at 1.0 below
			Version: "1.0",
			URL:     "http://example.com/pool/main/b/bar/bar_1.0_amd64.deb",
		},
		{
			Name:        "parentA",
			Version:     "1.0",
			Requires:    []string{"bar"},
			RequiresVer: []string{"bar (>= 2.0)"}, // forces the same-name bar 1.0 -> 2.0 replacement
			URL:         "http://example.com/pool/main/p/parenta/parentA_1.0_amd64.deb",
		},
		{
			Name:        "consumerX",
			Version:     "1.0",
			Requires:    []string{"foo"}, // parser keeps only the first alternative
			RequiresVer: []string{"foo (= 1.0) | bar (= 1.0)"},
			URL:         "http://example.com/pool/main/c/consumerx/consumerX_1.0_amd64.deb",
		},
		{
			Name:    "bar",
			Version: "2.0",
			URL:     "http://example.com/pool/main/b/bar/bar_2.0_amd64.deb",
		},
		{
			Name:    "foo",
			Version: "1.0",
			URL:     "http://example.com/pool/main/f/foo/foo_1.0_amd64.deb",
		},
	}

	// bar (the seed) is processed first, then parentA replaces it with bar
	// 2.0, then consumerX's OR edge must see the CURRENT bar version.
	requested := []ospackage.PackageInfo{all[0], all[1], all[2]}

	resolved, err := ResolveDependencies(requested, all)
	if err != nil {
		t.Fatalf("ResolveDependencies failed: %v", err)
	}

	var sawFoo bool
	for _, p := range resolved {
		if p.Name == "foo" {
			sawFoo = true
		}
	}
	if !sawFoo {
		t.Error("foo is missing from the resolved set; the actually resolved bar is 2.0, which does not satisfy \"bar (= 1.0)\", so foo must be pulled instead of the edge being wrongly skipped off a stale seed entry")
	}
}

// TestResolveDependencies_ReplacementValidatesAliasVersionBeforeCovering is a
// regression test for treating a replacement as "covering" another alias by
// NAME alone: new-provider also Provides "old-only-capability", but only at
// (= 2.0) — it does NOT satisfy parentX's genuine "old-only-capability
// (= 1.0)" requirement, which only old-provider (= 1.0) actually meets.
// Coverage must be validated against the accumulated version constraints for
// that alias, or old-provider gets dropped (and the alias silently repointed
// at a version-incompatible new-provider) while parentX's real requirement
// goes unsatisfied.
func TestResolveDependencies_ReplacementValidatesAliasVersionBeforeCovering(t *testing.T) {
	all := []ospackage.PackageInfo{
		{
			Name:        "parentX",
			Version:     "1.0",
			Requires:    []string{"old-only-capability"},
			RequiresVer: []string{"old-only-capability (= 1.0)"},
			URL:         "http://example.com/pool/main/p/parentx/parentX_1.0_amd64.deb",
		},
		{
			Name:        "parentZ",
			Version:     "1.0",
			Requires:    []string{"shared-abi"},
			RequiresVer: []string{"shared-abi (>= 1.0)"},
			URL:         "http://example.com/pool/main/p/parentz/parentZ_1.0_amd64.deb",
		},
		{
			Name:        "parentY",
			Version:     "1.0",
			Requires:    []string{"shared-abi"},
			RequiresVer: []string{"shared-abi (>= 2.0)"},
			URL:         "http://example.com/pool/main/p/parenty/parentY_1.0_amd64.deb",
		},
		{
			Name:        "old-provider",
			Version:     "1.0",
			Provides:    []string{"shared-abi", "old-only-capability"},
			ProvidesVer: []string{"shared-abi (= 1.0)", "old-only-capability (= 1.0)"},
			URL:         "http://example.com/pool/main/o/old-provider/old-provider_1.0_amd64.deb",
		},
		{
			Name:        "new-provider", // provides old-only-capability, but at the WRONG version
			Version:     "1.0",
			Provides:    []string{"shared-abi", "old-only-capability"},
			ProvidesVer: []string{"shared-abi (= 2.0)", "old-only-capability (= 2.0)"},
			URL:         "http://example.com/pool/main/n/new-provider/new-provider_1.0_amd64.deb",
		},
	}

	requested := []ospackage.PackageInfo{all[0], all[1], all[2]}

	resolved, err := ResolveDependencies(requested, all)
	if err != nil {
		t.Fatalf("ResolveDependencies failed: %v", err)
	}

	var sawOldProvider, sawNewProvider bool
	for _, p := range resolved {
		switch p.Name {
		case "old-provider":
			sawOldProvider = true
		case "new-provider":
			sawNewProvider = true
		}
	}
	if !sawOldProvider {
		t.Error("old-provider is missing from the resolved set; new-provider's old-only-capability (= 2.0) does not satisfy parentX's old-only-capability (= 1.0), so old-provider must be kept")
	}
	if !sawNewProvider {
		t.Error("new-provider is missing from the resolved set; parentY's stricter shared-abi constraint must still be satisfied")
	}
}

// TestResolveDependencies_ReplacementRejectsCandidateViolatingOwnNameConstraint
// is a regression test for a same-name replacement candidate bypassing
// aliasVersionCovered entirely: parentX directly requires the REAL package
// "provider (= 1.0)". parentZ/parentY force a shared-abi replacement search
// (initially satisfied by other-provider); a DIFFERENT version of "provider"
// itself (2.0, which does not exist yet when provider = 1.0 is chosen for
// parentX) also provides shared-abi (= 2.0) and would otherwise satisfy that
// search — but provider (2.0) does NOT satisfy parentX's genuine
// "provider (= 1.0)" pin, and two versions of the SAME package name cannot
// coexist in the closure. This must be reported as a conflict, not silently
// resolved by picking provider (2.0) for shared-abi.
func TestResolveDependencies_ReplacementRejectsCandidateViolatingOwnNameConstraint(t *testing.T) {
	all := []ospackage.PackageInfo{
		{
			Name:        "parentX",
			Version:     "1.0",
			Requires:    []string{"provider"},
			RequiresVer: []string{"provider (= 1.0)"},
			URL:         "http://example.com/pool/main/p/parentx/parentX_1.0_amd64.deb",
		},
		{
			Name:        "parentZ",
			Version:     "1.0",
			Requires:    []string{"shared-abi"},
			RequiresVer: []string{"shared-abi (>= 1.0)"},
			URL:         "http://example.com/pool/main/p/parentz/parentZ_1.0_amd64.deb",
		},
		{
			Name:        "parentY",
			Version:     "1.0",
			Requires:    []string{"shared-abi"},
			RequiresVer: []string{"shared-abi (>= 2.0)"},
			URL:         "http://example.com/pool/main/p/parenty/parentY_1.0_amd64.deb",
		},
		{
			// resolveMultiCandidates' constraint-satisfying search sorts ALL
			// candidates by their own Version field (highest first) BEFORE
			// same-repo/priority tie-breaking, regardless of package name —
			// so other-provider's own Version must outrank provider (2.0)'s
			// for it to be parentZ's initial pick despite providing an OLDER
			// shared-abi.
			Name:        "other-provider",
			Version:     "9.0",
			Provides:    []string{"shared-abi"},
			ProvidesVer: []string{"shared-abi (= 1.0)"},
			URL:         "http://example.com/pool/main/o/other-provider/other-provider_9.0_amd64.deb",
		},
		{
			Name:    "provider", // satisfies parentX's genuine "= 1.0" pin; does NOT provide shared-abi
			Version: "1.0",
			URL:     "http://example.com/pool/main/p/provider/provider_1.0_amd64.deb",
		},
		{
			Name:        "provider", // same real name as above, but a DIFFERENT version
			Version:     "2.0",
			Provides:    []string{"shared-abi"},
			ProvidesVer: []string{"shared-abi (= 2.0)"},
			URL:         "http://example.com/pool/main/p/provider/provider_2.0_amd64.deb",
		},
	}

	requested := []ospackage.PackageInfo{all[0], all[1], all[2]}

	if _, err := ResolveDependencies(requested, all); err == nil {
		t.Fatal("expected a conflict: provider (2.0) would satisfy shared-abi (>= 2.0) but violates parentX's genuine provider (= 1.0) pin on the same real package name")
	}
}

// TestResolveDependencies_VersionedFirstAlternativeFallsBackWhenUnsatisfied is
// a regression test for hasDirectDependency's inability to distinguish a
// truly bare dependency from depName merely being an OR term's OWN first
// alternative: for "libfoo-abi (= 3) | fallback", Requires contains
// "libfoo-abi" (the parser keeps only the first alternative), so the old code
// stripped the "= 3" constraint entirely and picked libfoo-provider (which
// only provides libfoo-abi = 2) anyway — never trying "fallback" even though
// libfoo-provider does not satisfy the first alternative's own pin.
func TestResolveDependencies_VersionedFirstAlternativeFallsBackWhenUnsatisfied(t *testing.T) {
	all := []ospackage.PackageInfo{
		{
			Name:        "consumer",
			Version:     "1.0",
			Requires:    []string{"libfoo-abi"}, // parser keeps only the first alternative
			RequiresVer: []string{"libfoo-abi (= 3) | fallback"},
			URL:         "http://example.com/pool/main/c/consumer/consumer_1.0_amd64.deb",
		},
		{
			Name:        "libfoo-provider", // provides libfoo-abi, but at the WRONG version
			Version:     "1.0",
			Provides:    []string{"libfoo-abi"},
			ProvidesVer: []string{"libfoo-abi (= 2)"},
			URL:         "http://example.com/pool/main/l/libfoo-provider/libfoo-provider_1.0_amd64.deb",
		},
		{
			Name:    "fallback",
			Version: "1.0",
			URL:     "http://example.com/pool/main/f/fallback/fallback_1.0_amd64.deb",
		},
	}

	requested := []ospackage.PackageInfo{all[0]}

	resolved, err := ResolveDependencies(requested, all)
	if err != nil {
		t.Fatalf("expected fallback to be resolved once libfoo-provider fails to satisfy libfoo-abi (= 3), got error: %v", err)
	}

	var sawFallback, sawLibfooProvider bool
	for _, p := range resolved {
		switch p.Name {
		case "fallback":
			sawFallback = true
		case "libfoo-provider":
			sawLibfooProvider = true
		}
	}
	if !sawFallback {
		t.Error("fallback is missing from the resolved set; libfoo-provider does not satisfy libfoo-abi (= 3), so the resolver must fall back to the next alternative")
	}
	if sawLibfooProvider {
		t.Error("libfoo-provider was selected despite not satisfying libfoo-abi (= 3); its own first-alternative constraint must not be silently dropped")
	}
}

// TestResolveDependencies_MandatoryBareDepIgnoresUnrelatedORVersionPin is a
// regression test for the edge-mixing bug where a version pin carried by an OR
// term (e.g. "foo (= 1.0) | bar") was wrongly enforced against an already
// selected foo that is ALSO required unconditionally by a separate bare term.
// seed-both depends on both "foo" (mandatory) and "foo (= 1.0) | bar"; the
// mandatory foo is satisfied by the already-selected foo 2.0, and the OR edge
// is independently satisfiable by bar, so foo 2.0 must be retained rather than
// reported as conflicting with the unrelated "= 1.0" pin.
func TestResolveDependencies_MandatoryBareDepIgnoresUnrelatedORVersionPin(t *testing.T) {
	all := []ospackage.PackageInfo{
		{
			Name:     "seed-foo",
			Version:  "1.0",
			Requires: []string{"foo"},
			URL:      "http://example.com/pool/main/s/seed-foo/seed-foo_1.0_amd64.deb",
		},
		{
			Name:        "seed-both",
			Version:     "1.0",
			Requires:    []string{"foo"},
			RequiresVer: []string{"foo", "foo (= 1.0) | bar"},
			URL:         "http://example.com/pool/main/s/seed-both/seed-both_1.0_amd64.deb",
		},
		{
			Name:    "foo",
			Version: "2.0",
			URL:     "http://example.com/pool/main/f/foo/foo_2.0_amd64.deb",
		},
		{
			Name:    "bar",
			Version: "1.0",
			URL:     "http://example.com/pool/main/b/bar/bar_1.0_amd64.deb",
		},
	}

	// seed-foo pulls foo 2.0 first; then seed-both's "foo (= 1.0) | bar" term
	// must not reject the already-selected, mandatory foo 2.0 as conflicting.
	requested := []ospackage.PackageInfo{all[0], all[1]}

	resolved, err := ResolveDependencies(requested, all)
	if err != nil {
		t.Fatalf("mandatory bare foo must not conflict with an unrelated OR term's version pin, got error: %v", err)
	}

	var fooVersion string
	for _, p := range resolved {
		if p.Name == "foo" {
			fooVersion = p.Version
		}
	}
	if fooVersion != "2.0" {
		t.Errorf("foo should be retained at 2.0 (mandatory, unconstrained by the OR term); got %q", fooVersion)
	}
}

// TestHasBareMandatoryTerm covers the helper that distinguishes a standalone
// mandatory dependency term from depName merely being an OR term's first
// alternative.
func TestHasBareMandatoryTerm(t *testing.T) {
	tests := []struct {
		name    string
		reqVers []string
		depName string
		want    bool
	}{
		{"bare mandatory", []string{"foo"}, "foo", true},
		{"bare mandatory with version", []string{"foo (>= 1.0)"}, "foo", true},
		{"bare plus OR first alt", []string{"foo", "foo (= 1.0) | bar"}, "foo", true},
		{"only OR first alt", []string{"foo (= 1.0) | bar"}, "foo", false},
		{"only later alt", []string{"logsave | e2fsprogs (<< 1.45)"}, "e2fsprogs", false},
		{"absent", []string{"baz"}, "foo", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := hasBareMandatoryTerm(tc.reqVers, tc.depName); got != tc.want {
				t.Errorf("hasBareMandatoryTerm(%q, %q) = %v, want %v", tc.reqVers, tc.depName, got, tc.want)
			}
		})
	}
}

// TestUncoveredAliasOnSameNameReplacement covers the helper that detects a
// same-name replacement (a version bump of the SAME real package) that would
// drop a still-required capability only the older version provides. Because two
// versions of one package name cannot coexist in the closure, such a case is a
// genuine conflict rather than a package that can be retained alongside the
// replacement.
func TestUncoveredAliasOnSameNameReplacement(t *testing.T) {
	oldPkg := ospackage.PackageInfo{
		Name:        "provider",
		Version:     "1.0",
		Provides:    []string{"shared-abi", "old-capability"},
		ProvidesVer: []string{"shared-abi (= 1.0)", "old-capability (= 1.0)"},
	}
	newPkg := ospackage.PackageInfo{
		Name:        "provider",
		Version:     "2.0",
		Provides:    []string{"shared-abi"},
		ProvidesVer: []string{"shared-abi (= 2.0)"},
	}
	differentName := ospackage.PackageInfo{
		Name:        "other-provider",
		Version:     "2.0",
		Provides:    []string{"shared-abi"},
		ProvidesVer: []string{"shared-abi (= 2.0)"},
	}

	t.Run("same-name uncovered genuine alias is a conflict", func(t *testing.T) {
		resolvedDeps := map[string]ospackage.PackageInfo{
			"shared-abi":     oldPkg,
			"old-capability": oldPkg,
			"provider":       oldPkg,
		}
		requiredDepNames := map[string]struct{}{"old-capability": {}}
		alias, conflict := uncoveredAliasOnSameNameReplacement(oldPkg, newPkg, resolvedDeps, "shared-abi", requiredDepNames, nil)
		if !conflict || alias != "old-capability" {
			t.Errorf("expected conflict on alias old-capability, got alias=%q conflict=%v", alias, conflict)
		}
	})

	t.Run("different real name is not a same-name conflict", func(t *testing.T) {
		resolvedDeps := map[string]ospackage.PackageInfo{
			"shared-abi":     oldPkg,
			"old-capability": oldPkg,
			"provider":       oldPkg,
		}
		requiredDepNames := map[string]struct{}{"old-capability": {}}
		if _, conflict := uncoveredAliasOnSameNameReplacement(oldPkg, differentName, resolvedDeps, "shared-abi", requiredDepNames, nil); conflict {
			t.Error("a differently-named replacement can coexist with the old provider; must not be flagged as a same-name conflict")
		}
	})

	t.Run("non-genuine alias is not a conflict", func(t *testing.T) {
		resolvedDeps := map[string]ospackage.PackageInfo{
			"shared-abi":     oldPkg,
			"old-capability": oldPkg,
			"provider":       oldPkg,
		}
		// old-capability present only as a bookkeeping alias, nothing requires it.
		requiredDepNames := map[string]struct{}{}
		if _, conflict := uncoveredAliasOnSameNameReplacement(oldPkg, newPkg, resolvedDeps, "shared-abi", requiredDepNames, nil); conflict {
			t.Error("an alias nothing genuinely requires must not be treated as a conflict")
		}
	})
}

// TestResolveDependencies_SameNameReplacementUncoveredAliasIsConflict is a
// regression test for the same-name replacement path through
// packageStillRequired: provider 1.0 provides "old-capability" and is pulled in
// by parentX via that capability. parentY then depends directly on
// "provider (>= 2.0)", forcing a version bump to provider 2.0 — the SAME real
// package name — which no longer provides old-capability. The two versions
// cannot coexist under one package name, so keeping provider 1.0 (still needed
// for old-capability) while queuing provider 2.0 would silently drop provider
// 2.0 (skipped via neededSet) and ship the constraint-violating 1.0. The
// resolver must surface a conflict instead.
func TestResolveDependencies_SameNameReplacementUncoveredAliasIsConflict(t *testing.T) {
	all := []ospackage.PackageInfo{
		{
			Name:     "parentX",
			Version:  "1.0",
			Requires: []string{"old-capability"},
			URL:      "http://example.com/pool/main/p/parentx/parentX_1.0_amd64.deb",
		},
		{
			Name:        "parentY",
			Version:     "1.0",
			Requires:    []string{"provider"},
			RequiresVer: []string{"provider (>= 2.0)"},
			URL:         "http://example.com/pool/main/p/parenty/parentY_1.0_amd64.deb",
		},
		{
			Name:        "provider",
			Version:     "1.0",
			Provides:    []string{"old-capability"},
			ProvidesVer: []string{"old-capability (= 1.0)"},
			URL:         "http://example.com/pool/main/p/provider/provider_1.0_amd64.deb",
		},
		{
			Name:    "provider", // same real name, version bump; drops old-capability
			Version: "2.0",
			URL:     "http://example.com/pool/main/p/provider/provider_2.0_amd64.deb",
		},
	}

	// parentX resolves old-capability to provider 1.0 before parentY's direct
	// "provider (>= 2.0)" forces the same-name replacement.
	requested := []ospackage.PackageInfo{all[0], all[1]}

	resolved, err := ResolveDependencies(requested, all)
	if err == nil {
		var providerVer string
		for _, p := range resolved {
			if p.Name == "provider" {
				providerVer = p.Version
			}
		}
		t.Fatalf("expected a conflict: provider cannot be both 1.0 (for old-capability) and 2.0 (for provider >= 2.0); got provider %q with no error", providerVer)
	}
}

// TestResolveDependencies_UnmetORDependencyStillPullsAlternative is a
// regression test for a round-20 review comment on
// TestResolveDependencies_MandatoryBareDepIgnoresUnrelatedORVersionPin's fix:
// dropping the OR-tagged constraint so the mandatory bare foo isn't wrongly
// rejected as a version conflict must not also skip checking whether the OR
// edge itself is satisfied. For "Depends: foo, foo (= 1.0) | bar" with an
// already-resolved foo 2.0 (which does not satisfy "= 1.0"), bar is a genuinely
// separate dependency that must still be pulled in.
func TestResolveDependencies_UnmetORDependencyStillPullsAlternative(t *testing.T) {
	all := []ospackage.PackageInfo{
		{
			Name:     "seed-foo",
			Version:  "1.0",
			Requires: []string{"foo"},
			URL:      "http://example.com/pool/main/s/seed-foo/seed-foo_1.0_amd64.deb",
		},
		{
			Name:        "seed-both",
			Version:     "1.0",
			Requires:    []string{"foo"},
			RequiresVer: []string{"foo", "foo (= 1.0) | bar"},
			URL:         "http://example.com/pool/main/s/seed-both/seed-both_1.0_amd64.deb",
		},
		{
			Name:    "foo",
			Version: "2.0",
			URL:     "http://example.com/pool/main/f/foo/foo_2.0_amd64.deb",
		},
		{
			Name:    "bar",
			Version: "1.0",
			URL:     "http://example.com/pool/main/b/bar/bar_1.0_amd64.deb",
		},
	}

	// seed-foo pulls foo 2.0 first; seed-both's mandatory "foo" is satisfied by
	// it, but its separate "foo (= 1.0) | bar" edge is not — bar must be pulled.
	requested := []ospackage.PackageInfo{all[0], all[1]}

	resolved, err := ResolveDependencies(requested, all)
	if err != nil {
		t.Fatalf("expected resolution to succeed by pulling in bar for the unmet OR edge, got error: %v", err)
	}

	var sawFoo2, sawBar bool
	for _, p := range resolved {
		if p.Name == "foo" && p.Version == "2.0" {
			sawFoo2 = true
		}
		if p.Name == "bar" {
			sawBar = true
		}
	}
	if !sawFoo2 {
		t.Error("foo 2.0 (mandatory, unconstrained by the OR term) should be retained")
	}
	if !sawBar {
		t.Error(`bar is missing: foo 2.0 does not satisfy the OR term's "= 1.0" pin, so bar must be pulled in to satisfy that separate edge`)
	}
}

// TestResolveDependencies_DistinctOREdgesSharingFirstAltBothResolve is a
// regression test for a round-20 review comment: resolveAlternativeCandidate
// (now resolveAlternativeCandidates) used to aggregate every RequiresVer term
// whose first alternative was depName into one flat constraint list and return
// as soon as ONE alternative resolved. With two distinct OR terms sharing "foo"
// as their first alternative ("foo (= 1) | bar" and "foo (= 2) | baz") and no
// foo package available at all, both edges are independently unmet and both
// alternatives (bar AND baz) must be pulled in — not just the first (bar).
func TestResolveDependencies_DistinctOREdgesSharingFirstAltBothResolve(t *testing.T) {
	all := []ospackage.PackageInfo{
		{
			Name:        "consumer",
			Version:     "1.0",
			Requires:    []string{"foo", "foo"},
			RequiresVer: []string{"foo (= 1) | bar", "foo (= 2) | baz"},
			URL:         "http://example.com/pool/main/c/consumer/consumer_1.0_amd64.deb",
		},
		{
			Name:    "bar",
			Version: "1.0",
			URL:     "http://example.com/pool/main/b/bar/bar_1.0_amd64.deb",
		},
		{
			Name:    "baz",
			Version: "1.0",
			URL:     "http://example.com/pool/main/b/baz/baz_1.0_amd64.deb",
		},
	}

	requested := []ospackage.PackageInfo{all[0]}

	resolved, err := ResolveDependencies(requested, all)
	if err != nil {
		t.Fatalf("expected both independent OR edges to resolve via their own alternative, got error: %v", err)
	}

	var sawBar, sawBaz bool
	for _, p := range resolved {
		switch p.Name {
		case "bar":
			sawBar = true
		case "baz":
			sawBaz = true
		}
	}
	if !sawBar {
		t.Error(`bar is missing: "foo (= 1) | bar" has no satisfying foo, so bar must be resolved`)
	}
	if !sawBaz {
		t.Error(`baz is missing: "foo (= 2) | baz" has no satisfying foo, so baz must be resolved independently of the first edge's own alternative`)
	}
}

// TestResolveDependencies_SharedFallbackAlternativeResolvesPerEdge is a
// regression test for a round-22 review comment: resolveAlternativeForTerm
// used to pass the FULL parent package into resolveMultiCandidates when
// resolving a fallback alternative, so resolveMultiCandidates aggregated
// version constraints for that alternative name from EVERY RequiresVer term of
// the parent — not just the one OR term actually being resolved. For
// "foo | bar (= 1.0), baz | bar (= 2.0)" with foo absent and baz available,
// the first edge only needs bar to satisfy "= 1.0"; baz already satisfies the
// second edge directly, so its "= 2.0" pin on bar is irrelevant to the first
// edge. Aggregating both pins made bar (only available at 1.0) look
// unsatisfiable for the first edge even though it isn't.
func TestResolveDependencies_SharedFallbackAlternativeResolvesPerEdge(t *testing.T) {
	all := []ospackage.PackageInfo{
		{
			Name:        "consumer",
			Version:     "1.0",
			Requires:    []string{"foo", "baz"},
			RequiresVer: []string{"foo | bar (= 1.0)", "baz | bar (= 2.0)"},
			URL:         "http://example.com/pool/main/c/consumer/consumer_1.0_amd64.deb",
		},
		{
			Name:    "baz",
			Version: "1.0",
			URL:     "http://example.com/pool/main/b/baz/baz_1.0_amd64.deb",
		},
		{
			Name:    "bar",
			Version: "1.0",
			URL:     "http://example.com/pool/main/b/bar/bar_1.0_amd64.deb",
		},
	}

	requested := []ospackage.PackageInfo{all[0]}

	resolved, err := ResolveDependencies(requested, all)
	if err != nil {
		t.Fatalf("expected bar (= 1.0) to resolve the first edge independently of the second edge's own bar (= 2.0) pin, got error: %v", err)
	}

	var sawBaz, sawBar bool
	for _, p := range resolved {
		switch p.Name {
		case "baz":
			sawBaz = true
		case "bar":
			sawBar = true
			if p.Version != "1.0" {
				t.Errorf("bar should resolve to version 1.0 (satisfies \"foo | bar (= 1.0)\"), got %q", p.Version)
			}
		}
	}
	if !sawBaz {
		t.Error(`baz is missing: "baz | bar (= 2.0)" is satisfied directly by baz itself`)
	}
	if !sawBar {
		t.Error(`bar is missing: "foo | bar (= 1.0)" has no satisfying foo, so bar must be resolved as its fallback`)
	}
}

// TestResolveDependencies_FirstResolutionOfMandatoryBareDepStillPullsAlternative
// is a twin of TestResolveDependencies_UnmetORDependencyStillPullsAlternative for
// the OTHER code path that strips an OR term's version pin off a bare mandatory
// depName: here foo is resolved for the FIRST time (not already in resolvedDeps)
// via resolveMultiCandidates, which picks foo 2.0 while ignoring the "= 1.0" pin
// (justified by the separate bare "foo" term) — but "foo (= 1.0) | bar" is still
// a distinct edge that foo 2.0 does not satisfy, so bar must still be pulled in.
func TestResolveDependencies_FirstResolutionOfMandatoryBareDepStillPullsAlternative(t *testing.T) {
	all := []ospackage.PackageInfo{
		{
			Name:        "consumer",
			Version:     "1.0",
			Requires:    []string{"foo", "foo"},
			RequiresVer: []string{"foo", "foo (= 1.0) | bar"},
			URL:         "http://example.com/pool/main/c/consumer/consumer_1.0_amd64.deb",
		},
		{
			Name:    "foo",
			Version: "2.0",
			URL:     "http://example.com/pool/main/f/foo/foo_2.0_amd64.deb",
		},
		{
			Name:    "bar",
			Version: "1.0",
			URL:     "http://example.com/pool/main/b/bar/bar_1.0_amd64.deb",
		},
	}

	requested := []ospackage.PackageInfo{all[0]}

	resolved, err := ResolveDependencies(requested, all)
	if err != nil {
		t.Fatalf("expected resolution to succeed by pulling in bar for the unmet OR edge, got error: %v", err)
	}

	var sawFoo2, sawBar bool
	for _, p := range resolved {
		if p.Name == "foo" && p.Version == "2.0" {
			sawFoo2 = true
		}
		if p.Name == "bar" {
			sawBar = true
		}
	}
	if !sawFoo2 {
		t.Error("foo 2.0 (mandatory, unconstrained by the OR term) should be selected")
	}
	if !sawBar {
		t.Error(`bar is missing: foo 2.0 does not satisfy the OR term's "= 1.0" pin, so bar must be pulled in to satisfy that separate edge`)
	}
}

// TestResolveDependencies_PrimaryCandidateSatisfiesOneOfTwoDistinctOREdges is a
// regression test for a round-21 review comment: when resolveMultiCandidates
// fails because it aggregates ALL RequiresVer terms naming foo first into one
// candidate search (foo cannot be both "= 1" and "= 2" at once), the fallback
// must still let foo's own candidate satisfy whichever term it individually
// matches — not treat every unmet aggregate term as needing an alternative.
// With foo (= 1) available and no bar, "foo (= 1) | bar" is satisfied directly
// by foo; only "foo (= 2) | baz" needs its own fallback, baz.
func TestResolveDependencies_PrimaryCandidateSatisfiesOneOfTwoDistinctOREdges(t *testing.T) {
	all := []ospackage.PackageInfo{
		{
			Name:        "consumer",
			Version:     "1.0",
			Requires:    []string{"foo", "foo"},
			RequiresVer: []string{"foo (= 1) | bar", "foo (= 2) | baz"},
			URL:         "http://example.com/pool/main/c/consumer/consumer_1.0_amd64.deb",
		},
		{
			Name:    "foo",
			Version: "1",
			URL:     "http://example.com/pool/main/f/foo/foo_1_amd64.deb",
		},
		{
			Name:    "baz",
			Version: "1.0",
			URL:     "http://example.com/pool/main/b/baz/baz_1.0_amd64.deb",
		},
	}

	requested := []ospackage.PackageInfo{all[0]}

	resolved, err := ResolveDependencies(requested, all)
	if err != nil {
		t.Fatalf("expected foo (= 1) to satisfy its own edge directly and baz to satisfy the other, got error: %v", err)
	}

	var sawFoo, sawBaz, sawBar bool
	for _, p := range resolved {
		switch p.Name {
		case "foo":
			sawFoo = true
		case "baz":
			sawBaz = true
		case "bar":
			sawBar = true
		}
	}
	if !sawFoo {
		t.Error("foo (= 1) should be selected: it directly satisfies \"foo (= 1) | bar\"")
	}
	if !sawBaz {
		t.Error(`baz is missing: foo (= 1) does not satisfy "foo (= 2) | baz", so baz must be pulled in for that edge`)
	}
	if sawBar {
		t.Error("bar should never be resolved: foo (= 1) already satisfies its edge directly, so its fallback is unnecessary")
	}
}

// TestResolveDependencies_UnselectedCandidateDoesNotFalselySatisfyOREdge is a
// regression test for a review comment on
// resolveDependencyTermsIndependently: once a version of depName is chosen for
// one OR edge, a DIFFERENT edge naming depName must not be treated as
// satisfied just because SOME OTHER candidate version of depName (never
// actually selected — only one version of a package name can exist in the
// closure) would have matched it. With foo (= 1) chosen for "foo (= 1) | bar"
// and foo (= 2) merely existing as an unselected candidate, "foo (= 2) | baz"
// is NOT satisfied by the closure and must fall back to baz.
func TestResolveDependencies_UnselectedCandidateDoesNotFalselySatisfyOREdge(t *testing.T) {
	all := []ospackage.PackageInfo{
		{
			Name:        "consumer",
			Version:     "1.0",
			Requires:    []string{"foo", "foo"},
			RequiresVer: []string{"foo (= 1) | bar", "foo (= 2) | baz"},
			URL:         "http://example.com/pool/main/c/consumer/consumer_1.0_amd64.deb",
		},
		{
			Name:    "foo",
			Version: "1",
			URL:     "http://example.com/pool/main/f/foo/foo_1_amd64.deb",
		},
		{
			Name:    "foo", // never selected, but present as a candidate
			Version: "2",
			URL:     "http://example.com/pool/main/f/foo/foo_2_amd64.deb",
		},
		{
			Name:    "baz",
			Version: "1.0",
			URL:     "http://example.com/pool/main/b/baz/baz_1.0_amd64.deb",
		},
	}

	requested := []ospackage.PackageInfo{all[0]}

	resolved, err := ResolveDependencies(requested, all)
	if err != nil {
		t.Fatalf("expected foo (= 1) to satisfy its own edge directly and baz to satisfy the other, got error: %v", err)
	}

	var sawBaz bool
	for _, p := range resolved {
		if p.Name == "baz" {
			sawBaz = true
		}
	}
	if !sawBaz {
		t.Error(`baz is missing: the unselected foo (= 2) candidate must not be treated as satisfying "foo (= 2) | baz"`)
	}
}

// TestResolveDependencies_UnsatisfiedBareTermNotMaskedByOREdgeFallback is a
// regression test for a round-21 review comment: an OR edge's fallback
// resolving successfully must not mask a SEPARATE, unsatisfied bare mandatory
// term for the same depName. "Depends: foo (= 3), foo (= 1) | bar" with only
// foo (= 1) available (no candidate satisfies "= 3") must fail, even though
// "foo (= 1) | bar" is trivially satisfied by foo (= 1) itself.
func TestResolveDependencies_UnsatisfiedBareTermNotMaskedByOREdgeFallback(t *testing.T) {
	all := []ospackage.PackageInfo{
		{
			Name:        "consumer",
			Version:     "1.0",
			Requires:    []string{"foo", "foo"},
			RequiresVer: []string{"foo (= 3)", "foo (= 1) | bar"},
			URL:         "http://example.com/pool/main/c/consumer/consumer_1.0_amd64.deb",
		},
		{
			Name:    "foo",
			Version: "1",
			URL:     "http://example.com/pool/main/f/foo/foo_1_amd64.deb",
		},
		{
			Name:    "bar",
			Version: "1.0",
			URL:     "http://example.com/pool/main/b/bar/bar_1.0_amd64.deb",
		},
	}

	requested := []ospackage.PackageInfo{all[0]}

	_, err := ResolveDependencies(requested, all)
	if err == nil {
		t.Fatal("expected an error: the bare mandatory \"foo (= 3)\" term is unsatisfiable (only foo (= 1) exists), regardless of the separate OR edge resolving via foo (= 1) or bar")
	}
}

// TestResolveDependencyTermsIndependently_AlternativeCarriesOwnReqVer is a
// regression test for a review comment on resolveDependencyTermsIndependently:
// an alternativeResolution's ReqVer must be the single OR term that actually
// selected it, so a caller extracting its version constraint only sees that
// term — not every RequiresVer term of cur that happens to also name the same
// alternative. For "foo | bar, baz | bar (= 2)" with foo absent, resolving
// foo's edge picks bar via the first term (unversioned); the second term's
// "bar (= 2)" belongs to a completely separate edge (already satisfiable via
// baz directly) and must not leak onto bar's recorded constraint, or a later,
// perfectly valid replacement of bar could be wrongly blocked.
func TestResolveDependencyTermsIndependently_AlternativeCarriesOwnReqVer(t *testing.T) {
	cur := ospackage.PackageInfo{
		Name:        "consumer",
		Version:     "1.0",
		RequiresVer: []string{"foo | bar", "baz | bar (= 2)"},
		URL:         "http://example.com/pool/main/c/consumer/consumer_1.0_amd64.deb",
	}
	all := []ospackage.PackageInfo{
		{Name: "bar", Version: "1.0", URL: "http://example.com/pool/main/b/bar/bar_1.0_amd64.deb"},
	}

	_, alternatives, ok := resolveDependencyTermsIndependently(cur, "foo", nil, all)
	if !ok {
		t.Fatal("expected foo's OR term to resolve via its bar fallback")
	}
	if len(alternatives) != 1 || alternatives[0].Name != "bar" {
		t.Fatalf("expected exactly one alternative resolving to bar, got %+v", alternatives)
	}

	res := alternatives[0]
	if res.ReqVer != "foo | bar" {
		t.Fatalf("alternativeResolution.ReqVer = %q, want the originating term %q", res.ReqVer, "foo | bar")
	}

	// Scoping the extraction to res.ReqVer (what the fix does) must not see
	// term2's unrelated "bar (= 2)" pin.
	scopedVCs, _ := extractVersionRequirement([]string{res.ReqVer}, res.Name)
	for _, vc := range scopedVCs {
		if vc.Op == "=" && vc.Ver == "2" {
			t.Errorf("scoped extraction on res.ReqVer leaked term2's unrelated constraint: %+v", vc)
		}
	}

	// Confirms the bug is real: extracting against the FULL cur.RequiresVer
	// (what the buggy code did) does pick up the unrelated term's constraint.
	unscopedVCs, hasUnscoped := extractVersionRequirement(cur.RequiresVer, res.Name)
	leaked := false
	for _, vc := range unscopedVCs {
		if vc.Op == "=" && vc.Ver == "2" {
			leaked = true
		}
	}
	if !hasUnscoped || !leaked {
		t.Fatal("test setup invalid: unscoped extraction was expected to leak term2's \"bar (= 2)\" constraint")
	}
}

// TestResolveDependencies_StaleOrEdgeConstraintBridgesUnresolvedAlternative is
// a regression test for the candidate-replacement filter treating a recorded
// OR-edge constraint as still binding whenever its fallback ISN'T ALREADY
// selected, even when that fallback could be freshly resolved and queued
// instead. consumer1's "foo (= 1.0) | bar" resolves foo 1.0 directly (a real
// candidate satisfies the pin, so bar is never even considered yet); nothing
// else ever independently requires bar. consumer2's separate "foo (= 2.0)"
// then forces a replacement: foo 2.0 doesn't meet the recorded "= 1.0" pin
// either, so the fix must resolve bar as that edge's own fallback and queue
// it, rather than reporting a conflict.
func TestResolveDependencies_StaleOrEdgeConstraintBridgesUnresolvedAlternative(t *testing.T) {
	all := []ospackage.PackageInfo{
		{
			Name:        "consumer1",
			Version:     "1.0",
			Requires:    []string{"foo"}, // parser keeps only the first alternative
			RequiresVer: []string{"foo (= 1.0) | bar"},
			URL:         "http://example.com/pool/main/c/consumer1/consumer1_1.0_amd64.deb",
		},
		{
			Name:        "consumer2",
			Version:     "1.0",
			Requires:    []string{"foo"},
			RequiresVer: []string{"foo (= 2.0)"},
			URL:         "http://example.com/pool/main/c/consumer2/consumer2_1.0_amd64.deb",
		},
		{
			Name:    "bar", // never independently required by anyone else
			Version: "1.0",
			URL:     "http://example.com/pool/main/b/bar/bar_1.0_amd64.deb",
		},
		{
			Name:    "foo", // consumer1's own direct pick, satisfying "= 1.0" exactly
			Version: "1.0",
			URL:     "http://example.com/pool/main/f/foo/foo_1.0_amd64.deb",
		},
		{
			Name:    "foo", // what consumer2 actually needs
			Version: "2.0",
			URL:     "http://example.com/pool/main/f/foo/foo_2.0_amd64.deb",
		},
	}

	requested := []ospackage.PackageInfo{all[0], all[1]}

	resolved, err := ResolveDependencies(requested, all)
	if err != nil {
		t.Fatalf("expected foo to be replaceable with 2.0 by resolving bar as consumer1's own OR-edge fallback, got error: %v", err)
	}

	var fooVersions []string
	var sawBar bool
	for _, p := range resolved {
		if p.Name == "foo" {
			fooVersions = append(fooVersions, p.Version)
		}
		if p.Name == "bar" {
			sawBar = true
		}
	}
	if len(fooVersions) != 1 || fooVersions[0] != "2.0" {
		t.Errorf("resolved foo versions = %v, want exactly [\"2.0\"]", fooVersions)
	}
	if !sawBar {
		t.Error("bar was not resolved; consumer1's \"foo (= 1.0) | bar\" edge is unmet once foo is replaced with 2.0, so bar must be pulled in")
	}
}

// TestResolveDependencyTermsIndependently_VirtualDependencyAllowsIndependentProviders
// is a regression test for resolveDependencyTermsIndependently binding every
// OR term naming the same virtual depName to whichever single candidate
// satisfied the FIRST such term, even though two DIFFERENTLY NAMED real
// packages can independently provide different versions of the same virtual
// capability and coexist in the closure (unlike two versions of one real
// package, which cannot). "virtual (= 1) | missing-a" and
// "virtual (= 2) | missing-b" are each individually satisfiable — by
// provider-a and provider-b respectively — but the buggy code fixed chosen to
// provider-a from the first term and then reported the second term unmet
// (missing-b doesn't exist) instead of finding provider-b directly.
func TestResolveDependencyTermsIndependently_VirtualDependencyAllowsIndependentProviders(t *testing.T) {
	cur := ospackage.PackageInfo{
		Name:        "consumer",
		Version:     "1.0",
		Requires:    []string{"virtual", "virtual"},
		RequiresVer: []string{"virtual (= 1) | missing-a", "virtual (= 2) | missing-b"},
		URL:         "http://example.com/pool/main/c/consumer/consumer_1.0_amd64.deb",
	}
	all := []ospackage.PackageInfo{
		{
			Name:        "provider-a",
			Version:     "1.0",
			Provides:    []string{"virtual"},
			ProvidesVer: []string{"virtual (= 1)"},
			URL:         "http://example.com/pool/main/p/provider-a/provider-a_1.0_amd64.deb",
		},
		{
			Name:        "provider-b",
			Version:     "1.0",
			Provides:    []string{"virtual"},
			ProvidesVer: []string{"virtual (= 2)"},
			URL:         "http://example.com/pool/main/p/provider-b/provider-b_1.0_amd64.deb",
		},
	}
	candidates := findAllCandidates("virtual", all)

	chosen, alternatives, ok := resolveDependencyTermsIndependently(cur, "virtual", candidates, all)
	if !ok {
		t.Fatal("expected both virtual OR edges to resolve via provider-a and provider-b independently")
	}

	names := map[string]bool{}
	if chosen != nil {
		names[chosen.Name] = true
	}
	for _, res := range alternatives {
		names[res.Package.Name] = true
	}
	if !names["provider-a"] || !names["provider-b"] {
		t.Fatalf("resolved providers = %v, want both provider-a (virtual=1 edge) and provider-b (virtual=2 edge)", names)
	}
}

// TestResolveDependencies_VirtualDependencyIndependentProvidersEndToEnd is the
// same regression as
// TestResolveDependencyTermsIndependently_VirtualDependencyAllowsIndependentProviders
// but exercised through the full ResolveDependencies entry point, confirming
// both providers actually end up queued and resolved.
func TestResolveDependencies_VirtualDependencyIndependentProvidersEndToEnd(t *testing.T) {
	all := []ospackage.PackageInfo{
		{
			Name:        "consumer",
			Version:     "1.0",
			Requires:    []string{"virtual", "virtual"},
			RequiresVer: []string{"virtual (= 1) | missing-a", "virtual (= 2) | missing-b"},
			URL:         "http://example.com/pool/main/c/consumer/consumer_1.0_amd64.deb",
		},
		{
			Name:        "provider-a",
			Version:     "1.0",
			Provides:    []string{"virtual"},
			ProvidesVer: []string{"virtual (= 1)"},
			URL:         "http://example.com/pool/main/p/provider-a/provider-a_1.0_amd64.deb",
		},
		{
			Name:        "provider-b",
			Version:     "1.0",
			Provides:    []string{"virtual"},
			ProvidesVer: []string{"virtual (= 2)"},
			URL:         "http://example.com/pool/main/p/provider-b/provider-b_1.0_amd64.deb",
		},
	}

	requested := []ospackage.PackageInfo{all[0]}

	resolved, err := ResolveDependencies(requested, all)
	if err != nil {
		t.Fatalf("expected provider-a and provider-b to independently satisfy their own virtual OR edge, got error: %v", err)
	}

	var sawA, sawB bool
	for _, p := range resolved {
		if p.Name == "provider-a" {
			sawA = true
		}
		if p.Name == "provider-b" {
			sawB = true
		}
	}
	if !sawA || !sawB {
		t.Errorf("resolved packages did not include both providers: provider-a=%v provider-b=%v", sawA, sawB)
	}
}
