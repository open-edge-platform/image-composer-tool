// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"context"
	"embed"
	"fmt"
	"net/http"
	"os"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"sigs.k8s.io/yaml"
)

//go:embed data/edge-pack.yaml
var edgePackFS embed.FS

// ── On-disk catalog ────────────────────────────────────────────────────────
//
// These mirror data/edge-pack.yaml verbatim. They are deliberately separate
// from the resolved types below: resolution is target-specific (a domain gains
// an availability verdict) and index-specific (a package gains real versions),
// so a single struct would have to carry fields that are meaningless at rest.

// edgePackCatalog is the root of the on-disk file.
type edgePackCatalog struct {
	Pack edgePackSpec `json:"pack"`
}

type edgePackSpec struct {
	ID           string                `json:"id"`
	DisplayName  string                `json:"displayName"`
	Description  string                `json:"description"`
	Repo         string                `json:"repo"`
	BaseRuntimes []edgePackRuntimeSpec `json:"baseRuntimes"`
	Domains      []edgePackDomainSpec  `json:"domains"`
}

type edgePackRuntimeSpec struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Package     string `json:"package"`
	// Available is a pointer so an omitted key means "available" rather than
	// the zero value's "unavailable" — the common case must not have to be
	// spelled out on every entry.
	Available         *bool  `json:"available,omitempty"`
	UnavailableReason string `json:"unavailableReason,omitempty"`
}

// isAvailable reports the runtime's availability, treating an omitted key as
// available. See Available's comment for why it is a pointer.
func (r edgePackRuntimeSpec) isAvailable() bool { return r.Available == nil || *r.Available }

type edgePackDomainSpec struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"displayName"`
	Description string   `json:"description"`
	OS          []string `json:"os,omitempty"`
	// RequiresRepos names repo ids this domain needs in addition to the pack's
	// own. A metapackage can depend on packages published somewhere the pack
	// repository does not carry, and selecting it without that repository
	// produces a template that cannot resolve at build time.
	RequiresRepos []string `json:"requiresRepos,omitempty"`
	Packages      []string `json:"packages"`
}

// appliesTo reports whether the domain is published for a target OS id. A
// domain with no OS list applies to every known target, and an empty query
// matches every domain. Mirrors PackageRepo.appliesTo, including its contract
// that callers reject unknown targets first.
func (d edgePackDomainSpec) appliesTo(osID string) bool {
	if osID == "" || len(d.OS) == 0 {
		return true
	}
	for _, id := range d.OS {
		if id == osID {
			return true
		}
	}
	return false
}

// ── Resolved types (what the API serves) ───────────────────────────────────

// EdgePack is the capability grouping as offered for one target: every domain
// the catalog defines, each with a verdict on whether this target can select
// it, and every package with whatever version metadata the repository index
// yielded.
type EdgePack struct {
	ID          string
	DisplayName string
	Description string
	// Repo is the catalog repo id every package resolves from. The UI needs it
	// to enable that repository when a package is picked, exactly as picking a
	// search hit does.
	Repo string
	// RepoAvailable reports whether Repo is offered for this target at all.
	// False means the pack cannot be used here — the UI says so rather than
	// rendering an empty grid that looks broken.
	RepoAvailable bool
	BaseRuntimes  []EdgePackBaseRuntime
	Domains       []EdgePackDomain
}

// EdgePackBaseRuntime is one runtime flavour a domain's packages run on top of.
type EdgePackBaseRuntime struct {
	ID          string
	DisplayName string
	Package     EdgePackPackage
	// Available false means shown but unselectable, with UnavailableReason
	// always populated to say why.
	Available         bool
	UnavailableReason string
}

// EdgePackDomain is one capability group.
type EdgePackDomain struct {
	ID          string
	DisplayName string
	Description string
	// Available false means this target cannot select the domain — either it
	// does not publish it, or a repository the domain needs is not offered
	// here. The domain is still reported so the UI can show it locked with a
	// reason, rather than silently omitting a capability that exists elsewhere.
	Available         bool
	UnavailableReason string
	// RequiresRepos are repo ids to enable alongside the pack's own when this
	// domain is selected. Published to the browser because enabling a
	// repository is a client-side action here, exactly as it is when a search
	// hit is picked.
	RequiresRepos []string
	Packages      []EdgePackPackage
}

// EdgePackPackage is one package in the pack, with whatever the repository
// index could tell us about it. Version and Versions are empty when the index
// was unreachable or does not carry the package — the package stays selectable
// at "latest", which is what an unpinned pick means anyway.
type EdgePackPackage struct {
	Name        string
	Description string
	Version     string
	Versions    []PackageVersion
}

// ── Resolution ─────────────────────────────────────────────────────────────

// EdgePack returns the pack as offered for a target OS id.
//
// An osID the manifest doesn't offer is rejected, matching PackageRepos, which
// reports an empty catalog for one. Here there is a single pack rather than a
// list, so there is no empty slice to return and an error is the only honest
// answer.
//
// Version resolution is best-effort by design: runLookups already treats an
// unreachable index as "contributes nothing" rather than as a failure, so a
// dead mirror costs the version chips, not the tab.
func (s *Service) EdgePack(ctx context.Context, osID string) (*EdgePack, error) {
	if osID != "" && !s.manifest.knowsTargetOS(osID) {
		return nil, newError(http.StatusNotFound, "UNKNOWN_TARGET",
			fmt.Sprintf("unknown target os %q", osID))
	}
	spec := s.edgePack
	// The whole offered set, not just the pack's own repo: a domain can require
	// repositories beyond it, and whether those are offered here decides
	// whether the domain can be selected at all.
	offered := s.PackageRepos(osID)
	repos := filterReposByID(offered, []string{spec.Repo})
	offeredIDs := repoIDSet(offered)
	meta := s.edgePackMetadata(ctx, osID, repos)

	out := &EdgePack{
		ID:            spec.ID,
		DisplayName:   spec.DisplayName,
		Description:   spec.Description,
		Repo:          spec.Repo,
		RepoAvailable: len(repos) > 0,
	}
	for _, r := range spec.BaseRuntimes {
		out.BaseRuntimes = append(out.BaseRuntimes, EdgePackBaseRuntime{
			ID:                r.ID,
			DisplayName:       r.DisplayName,
			Package:           meta.packageFor(r.Package),
			Available:         r.isAvailable(),
			UnavailableReason: r.UnavailableReason,
		})
	}
	for _, d := range spec.Domains {
		available, reason := s.domainAvailability(d, osID, offeredIDs)
		out.Domains = append(out.Domains, EdgePackDomain{
			ID:                d.ID,
			DisplayName:       d.DisplayName,
			Description:       d.Description,
			Available:         available,
			UnavailableReason: reason,
			RequiresRepos:     d.RequiresRepos,
			Packages:          meta.packagesFor(d.Packages),
		})
	}
	return out, nil
}

// domainAvailability decides whether a target can select a domain, and says why
// not when it cannot, so the UI states a cause rather than showing a card greyed
// out for no stated reason. The reason is empty when the domain is available.
//
// Two independent blocks, reported in this order: the catalog may not publish
// the domain for this target at all, or a repository the domain's packages
// depend on may not be offered here. The second is a hard block rather than a
// warning — the domain would select cleanly and then emit a template that
// cannot resolve at build time, which is the worse failure of the two because
// it surfaces long after the choice was made.
func (s *Service) domainAvailability(d edgePackDomainSpec, osID string, offered map[string]bool) (bool, string) {
	if !d.appliesTo(osID) {
		return false, fmt.Sprintf("Not published for %s", s.targetLabel(osID))
	}
	for _, id := range d.RequiresRepos {
		if !offered[id] {
			return false, fmt.Sprintf("Needs the %s repository, which is not offered for %s",
				s.repoLabel(id), s.targetLabel(osID))
		}
	}
	return true, ""
}

// repoIDSet indexes a repo list by id, for membership tests.
func repoIDSet(repos []PackageRepo) map[string]bool {
	set := make(map[string]bool, len(repos))
	for _, r := range repos {
		set[r.ID] = true
	}
	return set
}

// repoLabel renders a repo id as its display name. It searches the whole
// catalog rather than the target's offered subset, because the id it is asked
// about is typically one that is NOT offered here — that being the reason it
// needs naming.
func (s *Service) repoLabel(id string) string {
	for _, r := range s.repos {
		if r.ID == id {
			return r.DisplayName
		}
	}
	return id
}

// targetLabel renders a target id as its manifest display name, falling back to
// the raw id for an OS that appears only in `combinations` and so has no label
// entry — the same fallback the UI's own dropdowns make.
func (s *Service) targetLabel(osID string) string {
	for _, t := range s.manifest.Targets {
		if t.ID == osID {
			return t.DisplayName
		}
	}
	return osID
}

// edgePackIndex is the version metadata resolved for one target, keyed by
// package name.
type edgePackIndex map[string]PackageSearchHit

// packagesFor renders a name list as packages, preserving catalog order.
func (ix edgePackIndex) packagesFor(names []string) []EdgePackPackage {
	out := make([]EdgePackPackage, 0, len(names))
	for _, n := range names {
		out = append(out, ix.packageFor(n))
	}
	return out
}

// packageFor renders one name, carrying through whatever the index knew about
// it. A name the index never saw still yields a package — with no version — so
// a repository that is merely unreachable does not make the pack look empty.
func (ix edgePackIndex) packageFor(name string) EdgePackPackage {
	hit, ok := ix[name]
	if !ok {
		return EdgePackPackage{Name: name}
	}
	return EdgePackPackage{
		Name:        name,
		Description: hit.Description,
		Version:     hit.Version,
		Versions:    hit.Versions,
	}
}

// edgePackMetadata reads the pack repository's indexes and keeps only the
// packages the pack names.
//
// This is SearchPackages' body minus pagination, which is why it calls the same
// unexported helpers rather than the exported method: the pack's packages are a
// handful of names scattered through a repository of hundreds, and
// SearchPackages clamps to maxSearchLimit before a name filter could be applied
// — so the pack's entries would fall outside the first page and vanish.
func (s *Service) edgePackMetadata(ctx context.Context, osID string, repos []PackageRepo) edgePackIndex {
	ix := edgePackIndex{}
	if len(repos) == 0 {
		return ix
	}
	want := s.edgePackNameSet()
	lookups := s.planLookups(osID, repos)
	results := runLookups(ctx, s.pkgindexCache, lookups)
	for _, hit := range dedupAndFilter(results, "", rpmRepoSet(lookups)) {
		if want[hit.Name] {
			ix[hit.Name] = hit
		}
	}
	return ix
}

// edgePackNameSet is every package name the pack references, base runtimes
// included. A name in two domains appears once — this is also what makes the
// pack-level count a count of unique packages rather than a sum of domains.
func (s *Service) edgePackNameSet() map[string]bool {
	set := make(map[string]bool)
	for _, r := range s.edgePack.BaseRuntimes {
		set[r.Package] = true
	}
	for _, d := range s.edgePack.Domains {
		for _, n := range d.Packages {
			set[n] = true
		}
	}
	return set
}

// ── Loading ────────────────────────────────────────────────────────────────

// loadEdgePack parses the Edge Pack catalog. When path is non-empty it reads
// that file from disk (live-editable, no rebuild needed); otherwise it uses the
// copy embedded at build time. Mirrors loadPackageRepos.
func loadEdgePack(path string) (*edgePackSpec, error) {
	var raw []byte
	var err error
	if path != "" {
		raw, err = os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading edge pack %q: %w", path, err)
		}
		logger.Logger().Infof("loaded edge pack from file: %s", path)
	} else {
		raw, err = edgePackFS.ReadFile("data/edge-pack.yaml")
		if err != nil {
			return nil, fmt.Errorf("reading embedded edge pack: %w", err)
		}
	}
	var c edgePackCatalog
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parsing edge pack: %w", err)
	}
	if err := validateEdgePack(c.Pack); err != nil {
		return nil, err
	}
	return &c.Pack, nil
}

// validateEdgePack rejects a catalog the UI could not render or the API could
// not resolve. The file can be operator-supplied, so failing at construction
// with the offending id beats serving blank cards or a domain whose packages
// have nowhere to come from.
func validateEdgePack(p edgePackSpec) error {
	switch {
	case p.ID == "":
		return fmt.Errorf("edge pack: missing id")
	case p.DisplayName == "":
		return fmt.Errorf("edge pack %q: missing displayName", p.ID)
	case p.Repo == "":
		return fmt.Errorf("edge pack %q: missing repo", p.ID)
	case len(p.BaseRuntimes) == 0:
		return fmt.Errorf("edge pack %q: no base runtimes", p.ID)
	case len(p.Domains) == 0:
		return fmt.Errorf("edge pack %q: no domains", p.ID)
	}
	if err := validateEdgePackRuntimes(p); err != nil {
		return err
	}
	return validateEdgePackDomains(p)
}

// validateEdgePackRuntimes requires every runtime to be identifiable, to name a
// package, and — when it is marked unavailable — to say why. An option greyed
// out with no stated reason reads to the user as a bug rather than a decision,
// so the reason is mandatory rather than merely encouraged.
func validateEdgePackRuntimes(p edgePackSpec) error {
	seen := make(map[string]bool, len(p.BaseRuntimes))
	available := 0
	for i, r := range p.BaseRuntimes {
		switch {
		case r.ID == "":
			return fmt.Errorf("edge pack %q: base runtime %d: missing id", p.ID, i)
		case r.DisplayName == "":
			return fmt.Errorf("edge pack %q: base runtime %q: missing displayName", p.ID, r.ID)
		case r.Package == "":
			return fmt.Errorf("edge pack %q: base runtime %q: missing package", p.ID, r.ID)
		case seen[r.ID]:
			return fmt.Errorf("edge pack %q: base runtime %q: duplicate id", p.ID, r.ID)
		case !r.isAvailable() && r.UnavailableReason == "":
			return fmt.Errorf("edge pack %q: base runtime %q: unavailable with no unavailableReason", p.ID, r.ID)
		}
		seen[r.ID] = true
		if r.isAvailable() {
			available++
		}
	}
	// Every domain is gated behind choosing a base runtime, so a catalog where
	// none can be chosen locks the whole pack with no way out.
	if available == 0 {
		return fmt.Errorf("edge pack %q: no selectable base runtime", p.ID)
	}
	return nil
}

// validateEdgePackDomains requires every domain to be identifiable and to carry
// at least one package. An empty domain would render as a card whose checkbox
// selects nothing.
func validateEdgePackDomains(p edgePackSpec) error {
	seen := make(map[string]bool, len(p.Domains))
	for i, d := range p.Domains {
		switch {
		case d.ID == "":
			return fmt.Errorf("edge pack %q: domain %d: missing id", p.ID, i)
		case d.DisplayName == "":
			return fmt.Errorf("edge pack %q: domain %q: missing displayName", p.ID, d.ID)
		case len(d.Packages) == 0:
			return fmt.Errorf("edge pack %q: domain %q: no packages", p.ID, d.ID)
		case seen[d.ID]:
			return fmt.Errorf("edge pack %q: domain %q: duplicate id", p.ID, d.ID)
		}
		seen[d.ID] = true
		for j, n := range d.Packages {
			if n == "" {
				return fmt.Errorf("edge pack %q: domain %q: package %d is empty", p.ID, d.ID, j)
			}
		}
		for j, id := range d.RequiresRepos {
			// An empty id would resolve to no repository and silently drop the
			// prerequisite, which is precisely the failure requiresRepos exists to
			// prevent. Whether the id names a real repo is asserted by the drift
			// guard, as it is for the pack's own repo.
			if id == "" {
				return fmt.Errorf("edge pack %q: domain %q: requiresRepos %d is empty", p.ID, d.ID, j)
			}
		}
	}
	return nil
}
