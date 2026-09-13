// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeEdgePack writes a pack catalog to a temp file and returns its path, for
// exercising the on-disk override path. Mirrors writeRepos.
func writeEdgePack(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "edge-pack.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write edge pack: %v", err)
	}
	return p
}

// edgePackService builds a Service around a caller-supplied pack and repository
// catalog plus the target ids that count as known.
//
// The repository catalogs these tests pass declare no `index`, so planLookups
// produces no lookups and resolution never reaches the network or the (nil)
// index cache. Version metadata is therefore always empty here — which is
// exactly the degraded shape a real unreachable mirror produces, and is
// asserted as such rather than worked around.
func edgePackService(t *testing.T, pack, catalog string, targets ...string) *Service {
	t.Helper()
	spec, err := loadEdgePack(writeEdgePack(t, pack))
	if err != nil {
		t.Fatalf("loadEdgePack: %v", err)
	}
	svc := reposService(t, catalog, targets...)
	svc.edgePack = spec
	return svc
}

// A pack whose domains differ in OS reach, with one package shared by two
// domains so the unique-vs-summed count distinction is exercised.
const testPack = `
pack:
  id: edge-pack
  displayName: Edge Pack
  description: capabilities
  repo: test-repo
  baseRuntimes:
    - id: standard
      displayName: Standard
      package: base-standard
    - id: realtime
      displayName: Real-time
      package: base-realtime
      available: false
      unavailableReason: not shipped yet
  domains:
    - id: media
      displayName: Media
      description: transcode
      packages: [media-ffmpeg, shared-runtime]
    - id: npu
      displayName: NPU
      description: npu stack
      os: [ubuntu24]
      packages: [npu-driver]
    - id: graphics
      displayName: Graphics
      description: display stack
      packages: [shared-runtime]
`

const testPackRepos = `
repos:
  - id: test-repo
    displayName: Test Repo
    url: https://example.com/repo
    enabledByDefault: false
    os: [ubuntu24]
`

// The embedded catalog is what ships, so it must satisfy the same invariants
// the loader enforces on an operator-supplied file.
func TestLoadEdgePackEmbedded(t *testing.T) {
	spec, err := loadEdgePack("")
	if err != nil {
		t.Fatalf("loading embedded edge pack: %v", err)
	}
	if len(spec.Domains) == 0 {
		t.Fatal("embedded edge pack has no domains")
	}
	for _, d := range spec.Domains {
		if d.ID == "" || d.DisplayName == "" || len(d.Packages) == 0 {
			t.Errorf("embedded domain incomplete: %+v", d)
		}
	}
}

// Drift guard: the pack's repository must exist in the repository catalog, and
// every domain's OS restriction must name a real manifest target. Neither
// mistake fails loudly at runtime — a bad repo id yields a pack whose packages
// silently have nowhere to resolve from, and a misspelled target withholds a
// domain everywhere — so they are caught here instead.
func TestEmbeddedEdgePackReferencesAreReal(t *testing.T) {
	spec, err := loadEdgePack("")
	if err != nil {
		t.Fatalf("loadEdgePack: %v", err)
	}
	repos, err := loadPackageRepos("")
	if err != nil {
		t.Fatalf("loadPackageRepos: %v", err)
	}
	m, err := loadManifest("")
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}

	known := make(map[string]bool, len(repos))
	for _, r := range repos {
		known[r.ID] = true
	}
	if !known[spec.Repo] {
		t.Errorf("edge pack names repo %q, which data/package-repos.yaml does not define", spec.Repo)
	}
	for _, d := range spec.Domains {
		for _, osID := range d.OS {
			if !m.knowsTargetOS(osID) {
				t.Errorf("domain %q restricts to target %q, which the manifest does not offer", d.ID, osID)
			}
		}
	}
}

// A domain the target does not publish is reported, not dropped — the UI needs
// the row to show it locked — and it carries a reason naming that target.
func TestEdgePackDomainOSGating(t *testing.T) {
	svc := edgePackService(t, testPack, testPackRepos, "ubuntu24", "ubuntu26-server")

	cases := []struct {
		osID        string
		npuWanted   bool
		wantsReason string
	}{
		{osID: "ubuntu24", npuWanted: true},
		{osID: "ubuntu26-server", npuWanted: false, wantsReason: "ubuntu26-server"},
		// No target named means no restriction can apply, so every domain is
		// reported available.
		{osID: "", npuWanted: true},
	}
	for _, c := range cases {
		t.Run(c.osID, func(t *testing.T) {
			pack, err := svc.EdgePack(context.Background(), c.osID)
			if err != nil {
				t.Fatalf("EdgePack: %v", err)
			}
			if len(pack.Domains) != 3 {
				t.Fatalf("got %d domains, want all 3 reported", len(pack.Domains))
			}
			npu := domainByID(t, pack, "npu")
			if npu.Available != c.npuWanted {
				t.Errorf("npu available = %v, want %v", npu.Available, c.npuWanted)
			}
			if c.wantsReason == "" {
				if npu.UnavailableReason != "" {
					t.Errorf("available domain carries reason %q", npu.UnavailableReason)
				}
			} else if !strings.Contains(npu.UnavailableReason, c.wantsReason) {
				t.Errorf("reason %q does not name %q", npu.UnavailableReason, c.wantsReason)
			}
			// A domain with no OS restriction is never withheld.
			if m := domainByID(t, pack, "media"); !m.Available {
				t.Error("unrestricted domain media reported unavailable")
			}
		})
	}
}

// An unavailable base runtime is reported alongside the selectable one so it
// can be shown disabled, and it always states why.
func TestEdgePackBaseRuntimes(t *testing.T) {
	svc := edgePackService(t, testPack, testPackRepos, "ubuntu24")
	pack, err := svc.EdgePack(context.Background(), "ubuntu24")
	if err != nil {
		t.Fatalf("EdgePack: %v", err)
	}
	if len(pack.BaseRuntimes) != 2 {
		t.Fatalf("got %d base runtimes, want 2", len(pack.BaseRuntimes))
	}
	std, rt := pack.BaseRuntimes[0], pack.BaseRuntimes[1]
	if !std.Available {
		t.Error("standard runtime reported unavailable")
	}
	if std.Package.Name != "base-standard" {
		t.Errorf("standard package = %q, want base-standard", std.Package.Name)
	}
	if rt.Available {
		t.Error("realtime runtime reported available")
	}
	if rt.UnavailableReason == "" {
		t.Error("unavailable runtime states no reason")
	}
}

// The pack's package set counts a name once however many domains claim it, so
// a shared package cannot inflate the pack total.
func TestEdgePackNameSetDeduplicates(t *testing.T) {
	svc := edgePackService(t, testPack, testPackRepos, "ubuntu24")
	set := svc.edgePackNameSet()
	// 2 base runtimes + media-ffmpeg + shared-runtime + npu-driver.
	if len(set) != 5 {
		t.Errorf("name set has %d entries, want 5 (shared-runtime counted once): %v", len(set), set)
	}
	for _, n := range []string{"base-standard", "base-realtime", "media-ffmpeg", "shared-runtime", "npu-driver"} {
		if !set[n] {
			t.Errorf("name set missing %q", n)
		}
	}
	// Summing the domains would reach 4 for 3 distinct domain packages, which
	// is why the pack total is computed over the set rather than the sum.
	pack, err := svc.EdgePack(context.Background(), "ubuntu24")
	if err != nil {
		t.Fatalf("EdgePack: %v", err)
	}
	summed := 0
	for _, d := range pack.Domains {
		summed += len(d.Packages)
	}
	if summed != 4 {
		t.Errorf("summed domain packages = %d, want 4", summed)
	}
}

// A repository with no reachable index still yields the full pack structure,
// with packages named and unversioned. This is the degraded path a dead mirror
// produces, and it must not empty the tab.
func TestEdgePackWithoutIndexKeepsPackages(t *testing.T) {
	svc := edgePackService(t, testPack, testPackRepos, "ubuntu24")
	pack, err := svc.EdgePack(context.Background(), "ubuntu24")
	if err != nil {
		t.Fatalf("EdgePack: %v", err)
	}
	media := domainByID(t, pack, "media")
	if len(media.Packages) != 2 {
		t.Fatalf("media has %d packages, want 2", len(media.Packages))
	}
	if media.Packages[0].Name != "media-ffmpeg" {
		t.Errorf("package order not preserved: %+v", media.Packages)
	}
	if media.Packages[0].Version != "" || len(media.Packages[0].Versions) != 0 {
		t.Errorf("unresolved package carries version metadata: %+v", media.Packages[0])
	}
}

// The pack's repository is not offered for every target. Where it is absent the
// pack says so, rather than reporting domains the target could never install.
func TestEdgePackRepoAvailability(t *testing.T) {
	svc := edgePackService(t, testPack, testPackRepos, "ubuntu24", "debian13")

	pack, err := svc.EdgePack(context.Background(), "ubuntu24")
	if err != nil {
		t.Fatalf("EdgePack: %v", err)
	}
	if !pack.RepoAvailable {
		t.Error("repoAvailable false on the target the repo is offered for")
	}
	if pack.Repo != "test-repo" {
		t.Errorf("repo = %q, want test-repo", pack.Repo)
	}

	pack, err = svc.EdgePack(context.Background(), "debian13")
	if err != nil {
		t.Fatalf("EdgePack: %v", err)
	}
	if pack.RepoAvailable {
		t.Error("repoAvailable true on a target the repo is not offered for")
	}
	// The domains are still described, so switching targets doesn't make the
	// pack look like it stopped existing.
	if len(pack.Domains) != 3 {
		t.Errorf("got %d domains on a repo-less target, want 3", len(pack.Domains))
	}
}

// Unlike PackageRepos, which reports an empty catalog for an unknown target,
// there is no empty pack to return — so this is an error rather than a pack
// with no domains, which would misreport the capability as gone.
func TestEdgePackUnknownTarget(t *testing.T) {
	svc := edgePackService(t, testPack, testPackRepos, "ubuntu24")
	if _, err := svc.EdgePack(context.Background(), "no-such-os"); err == nil {
		t.Fatal("unknown target accepted")
	}
}

func TestLoadEdgePackRejectsInvalid(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "missing id",
			body: "pack:\n  displayName: X\n  repo: r\n",
			want: "missing id",
		},
		{
			name: "missing repo",
			body: "pack:\n  id: p\n  displayName: X\n",
			want: "missing repo",
		},
		{
			name: "no domains",
			body: "pack:\n  id: p\n  displayName: X\n  repo: r\n" +
				"  baseRuntimes:\n    - {id: s, displayName: S, package: pkg}\n",
			want: "no domains",
		},
		{
			name: "no base runtimes",
			body: "pack:\n  id: p\n  displayName: X\n  repo: r\n" +
				"  domains:\n    - {id: d, displayName: D, packages: [a]}\n",
			want: "no base runtimes",
		},
		{
			name: "runtime without package",
			body: "pack:\n  id: p\n  displayName: X\n  repo: r\n" +
				"  baseRuntimes:\n    - {id: s, displayName: S}\n" +
				"  domains:\n    - {id: d, displayName: D, packages: [a]}\n",
			want: "missing package",
		},
		{
			name: "unavailable runtime without reason",
			body: "pack:\n  id: p\n  displayName: X\n  repo: r\n" +
				"  baseRuntimes:\n    - {id: s, displayName: S, package: pkg, available: false}\n" +
				"  domains:\n    - {id: d, displayName: D, packages: [a]}\n",
			want: "unavailableReason",
		},
		{
			name: "every runtime unavailable",
			body: "pack:\n  id: p\n  displayName: X\n  repo: r\n" +
				"  baseRuntimes:\n    - {id: s, displayName: S, package: pkg, available: false, unavailableReason: nope}\n" +
				"  domains:\n    - {id: d, displayName: D, packages: [a]}\n",
			want: "no selectable base runtime",
		},
		{
			name: "empty domain",
			body: "pack:\n  id: p\n  displayName: X\n  repo: r\n" +
				"  baseRuntimes:\n    - {id: s, displayName: S, package: pkg}\n" +
				"  domains:\n    - {id: d, displayName: D, packages: []}\n",
			want: "no packages",
		},
		{
			name: "duplicate domain id",
			body: "pack:\n  id: p\n  displayName: X\n  repo: r\n" +
				"  baseRuntimes:\n    - {id: s, displayName: S, package: pkg}\n" +
				"  domains:\n    - {id: d, displayName: D, packages: [a]}\n" +
				"    - {id: d, displayName: D2, packages: [b]}\n",
			want: "duplicate id",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := loadEdgePack(writeEdgePack(t, c.body))
			if err == nil {
				t.Fatal("invalid catalog accepted")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not mention %q", err, c.want)
			}
		})
	}
}

func TestLoadEdgePackMissingFile(t *testing.T) {
	if _, err := loadEdgePack(filepath.Join(t.TempDir(), "absent.yaml")); err == nil {
		t.Fatal("missing file accepted")
	}
}

// domainByID fails the test rather than returning a zero value, so a missing
// domain is reported where it happened instead of as a confusing field
// mismatch further down.
func domainByID(t *testing.T, p *EdgePack, id string) EdgePackDomain {
	t.Helper()
	for _, d := range p.Domains {
		if d.ID == id {
			return d
		}
	}
	t.Fatalf("pack has no domain %q", id)
	return EdgePackDomain{}
}
