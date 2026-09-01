// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"embed"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"sigs.k8s.io/yaml"
)

//go:embed data/package-repos.yaml
var packageReposFS embed.FS

// defaultRepoPriority is the priority assigned to a repo that doesn't declare
// one. It matches both the OpenAPI default and normalizeRepositoryPriorities in
// internal/config, so the picker shows the same number a build would apply.
const defaultRepoPriority = 500

// RepoIndex locates one package index belonging to a repository.
//
// A single picker entry can need several: Ubuntu's base repo spans three suites
// across two hosts (noble and noble-updates on archive.ubuntu.com,
// noble-security on security.ubuntu.com), and its arm64 packages live on
// ports.ubuntu.com rather than either. Modelling those as one entry with
// several indexes keeps the picker to one row per repository while still
// naming every index a search has to read.
type RepoIndex struct {
	// URL overrides the parent PackageRepo.URL for this index only; empty
	// inherits it. Set it where a suite or an architecture is served from a
	// different host.
	URL string `json:"url,omitempty"`
	// Codename is the deb suite (the dists/ path segment), e.g. "noble" or
	// "isar". Required for deb repositories, unused for rpm.
	Codename string `json:"codename,omitempty"`
	// Component is space-separated, matching config.PackageRepository and the
	// providerconfigs, e.g. "main restricted universe multiverse".
	Component string `json:"component,omitempty"`
	// Arch lists the manifest target arches this index serves. Empty means all.
	Arch []string `json:"arch,omitempty"`
}

// PackageRepo is one repository offered by the Advanced tab's repository picker.
//
// The presentation fields (DisplayName, Description, EnabledByDefault) label the
// repository and seed its toggle. The repos a build actually uses still come from
// the template's packageRepositories block and the per-OS providerconfigs — see
// data/package-repos.yaml for the full rationale. Index carries the metadata a
// package search needs to read the repository's own index; it is deliberately
// absent from the wire type, so it never reaches the browser.
type PackageRepo struct {
	ID               string `json:"id"`
	DisplayName      string `json:"displayName"`
	URL              string `json:"url"`
	Description      string `json:"description,omitempty"`
	EnabledByDefault bool   `json:"enabledByDefault"`
	// Priority breaks ties when a package exists in several repos (higher wins).
	// Zero means unset; PackageRepos reports defaultRepoPriority instead so
	// callers never have to special-case it.
	Priority int `json:"priority,omitempty"`
	// OS lists the manifest target ids this repo applies to. Empty means it
	// applies to every target, so a repo can be offered everywhere without
	// having to enumerate (and then maintain) the full target list.
	OS []string `json:"os,omitempty"`
	// Type selects how the indexes are laid out: "deb" (dists/<codename>/
	// <component>/binary-<arch>/Packages) or "rpm" (repodata/repomd.xml).
	// Empty means deb.
	Type string `json:"type,omitempty"`
	// Index names the package indexes to read for this repository. Empty means
	// the repository is offered in the picker but not yet searchable.
	Index []RepoIndex `json:"index,omitempty"`
	// GPGKeyPath is a local filesystem path to the GPG keyring a search
	// verifies this repository's Release against before trusting its index.
	// Empty means the repository has no genuine local trust anchor and is
	// searched unverified — true today for every entry except the Ubuntu
	// base repos, which carry the archive keyring apt itself installs at
	// /usr/share/keyrings/ubuntu-archive-keyring.gpg. Like Index, it is kept
	// out of the wire type by fromPackageRepoList rather than by this tag, so
	// it never reaches the browser.
	GPGKeyPath string `json:"gpgKeyPath,omitempty"`
	// CuratedPackages is a hand-picked "frequently used" subset of this
	// repo's packages the browse pane's "Show frequently used" toggle
	// narrows to. Empty means the toggle has nothing to narrow to for this
	// repo. Like Index and GPGKeyPath, it is kept out of the wire type: the
	// browser sends a curated=true query flag and the server does the
	// filtering, rather than shipping the list itself.
	CuratedPackages []string `json:"curatedPackages,omitempty"`
	// PKey is the GPG signing-key URL written as this repository's `pkey` when
	// enabling it emits a packageRepositories entry (toTemplateRepos). It is a
	// URL a build fetches, which is what config.PackageRepository.PKey expects
	// — deliberately a separate field from GPGKeyPath, which is a *local* path
	// pkgindex reads to verify a search. A repo may legitimately have one and
	// not the other: the Ubuntu base repos carry the local archive keyring but
	// need no pkey (their repos come from the providerconfigs), while a
	// third-party repo has a published key URL and no local keyring.
	//
	// Empty means the catalog knows no key for this repository, so a template
	// enabling it fetches packages unverified. Kept out of the wire type like
	// the fields above; only the derived hasSigningKey flag is published.
	PKey string `json:"pkey,omitempty"`
}

// HasSigningKey reports whether this repository's packages are ever fetched
// with signature verification: either PKey (a build verifies the enabled
// packageRepositories entry) or GPGKeyPath (the picker's own search already
// verifies the repo's Release against a local keyring, as the Ubuntu base
// repos do). A repo can have either, both, or neither — see PKey's comment
// for why the two are independent — but the browser only needs to know
// whether verification happens by some means, not which one.
func (r PackageRepo) HasSigningKey() bool { return r.PKey != "" || r.GPGKeyPath != "" }

// Repository types recognised in the catalog. Empty defaults to repoTypeDeb.
const (
	repoTypeDeb = "deb"
	repoTypeRPM = "rpm"
)

// packageRepoCatalog is the on-disk shape of the repository catalog.
type packageRepoCatalog struct {
	Repos []PackageRepo `json:"repos"`
}

// PackageRepos returns the repositories offered for a target OS id, ordered by
// descending priority and then by id so the picker is stable across calls.
//
// An empty osID returns the whole catalog: the UI asks for repos before a target
// is chosen, and showing everything is more useful there than showing nothing.
//
// An osID the manifest doesn't offer yields an empty (non-nil) slice rather than
// an error — an unknown target has nothing to offer, which the UI renders as an
// empty picker instead of a failure. That check is against the manifest, not
// against the catalog: a repo with no OS list applies to every *known* target,
// so without it, adding one global repo would start returning results for
// targets that don't exist.
func (s *Service) PackageRepos(osID string) []PackageRepo {
	if osID != "" && !s.manifest.knowsTargetOS(osID) {
		return []PackageRepo{}
	}
	out := make([]PackageRepo, 0, len(s.repos))
	for _, r := range s.repos {
		if !r.appliesTo(osID) {
			continue
		}
		if r.Priority == 0 {
			r.Priority = defaultRepoPriority
		}
		out = append(out, r)
	}
	// Highest priority first — that's the repo that would win a package tie, so
	// it's the one to show first. Ties break on id to keep the order stable.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// toTemplateRepos maps enabled catalog repo ids to the packageRepositories
// entries a generated delta declares, so a package picked from a repository the
// matched template doesn't already configure can still resolve at build time.
//
// Only repos offered for osID are mapped, and only optional ones: a repo the
// catalog marks EnabledByDefault is the target's base repository, already
// supplied by the per-OS providerconfigs, so re-declaring it in the template
// would duplicate configuration the build already has. An id the catalog
// doesn't know, or one with no index metadata to derive a codename from, is
// skipped rather than emitted half-formed — the UI can only produce ids it was
// served, so a mismatch means a stale client, not a user error worth failing on.
//
// Order follows PackageRepos (priority-descending, id tie-broken) so the emitted
// block is deterministic for a given selection.
func (s *Service) toTemplateRepos(osID string, ids []string) []config.PackageRepository {
	if len(ids) == 0 {
		return nil
	}
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	var out []config.PackageRepository
	for _, r := range s.PackageRepos(osID) {
		if !want[r.ID] || r.EnabledByDefault {
			continue
		}
		if repo, ok := r.templateRepo(); ok {
			out = append(out, repo)
		}
	}
	return out
}

// templateRepo renders one catalog repo as a template packageRepositories
// entry. Reports false when the repo carries no index metadata, since without a
// codename there is no suite for a deb build to read.
//
// Every optional repo in the catalog publishes exactly one index, so the first
// is the whole story; a repo that grows a second would need its own entry per
// index, which is why this returns one value rather than silently using [0] of
// several.
func (r PackageRepo) templateRepo() (config.PackageRepository, bool) {
	if len(r.Index) == 0 {
		return config.PackageRepository{}, false
	}
	idx := r.Index[0]
	url := idx.URL
	if url == "" {
		url = r.URL
	}
	repo := config.PackageRepository{
		URL:  url,
		PKey: r.PKey,
	}
	// An rpm repository is addressed by base URL alone: it publishes one
	// repodata/ per URL, with no suite or component to name. Codename still
	// carries the catalog id there, because the build path uses it as the
	// repository's display name (config.getRepositoryName) and as the dnf repo
	// id, and an empty one would render as a bare URL in build logs.
	if r.Type == repoTypeRPM {
		repo.Codename = r.ID
		return repo, true
	}
	if idx.Codename == "" {
		return config.PackageRepository{}, false
	}
	repo.Codename = idx.Codename
	repo.Component = idx.Component
	// Only a raised priority is worth emitting: defaultRepoPriority is what a
	// build applies to an entry that declares none (normalizeRepositoryPriorities
	// in internal/config), so writing it out states the default as though it
	// were a choice.
	if r.Priority != 0 && r.Priority != defaultRepoPriority {
		repo.Priority = r.Priority
	}
	return repo, true
}

// appliesTo reports whether the repo is offered for a target OS id. A repo with
// no OS list applies to every known target, and an empty query matches every
// repo. Callers must reject unknown targets before this point — see
// PackageRepos, which does.
func (r PackageRepo) appliesTo(osID string) bool {
	if osID == "" || len(r.OS) == 0 {
		return true
	}
	for _, id := range r.OS {
		if id == osID {
			return true
		}
	}
	return false
}

// loadPackageRepos parses the repository catalog. When path is non-empty it
// reads that file from disk (live-editable, no rebuild needed); otherwise it
// uses the copy embedded at build time. Mirrors loadManifest.
func loadPackageRepos(path string) ([]PackageRepo, error) {
	var raw []byte
	var err error
	if path != "" {
		raw, err = os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading package repos %q: %w", path, err)
		}
		logger.Logger().Infof("loaded package repos from file: %s", path)
	} else {
		raw, err = packageReposFS.ReadFile("data/package-repos.yaml")
		if err != nil {
			return nil, fmt.Errorf("reading embedded package repos: %w", err)
		}
	}
	var c packageRepoCatalog
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parsing package repos: %w", err)
	}
	// Reject a catalog that can't be rendered or referenced. The file can be
	// operator-supplied, so failing at construction with the offending id beats
	// serving entries the UI would show as blank rows or couldn't send back.
	seen := make(map[string]struct{}, len(c.Repos))
	for i, r := range c.Repos {
		switch {
		case r.ID == "":
			return nil, fmt.Errorf("package repo %d: missing id", i)
		case r.DisplayName == "":
			return nil, fmt.Errorf("package repo %q: missing displayName", r.ID)
		case r.URL == "":
			return nil, fmt.Errorf("package repo %q: missing url", r.ID)
		// Templates use "<URL>" as a fill-me-in placeholder for private repos.
		// Copying one into the catalog would offer the user a repository that
		// cannot resolve, so reject it at load rather than at fetch.
		case strings.Contains(r.URL, "<URL>"):
			return nil, fmt.Errorf("package repo %q: url is a placeholder", r.ID)
		case r.Type != "" && r.Type != repoTypeDeb && r.Type != repoTypeRPM:
			return nil, fmt.Errorf("package repo %q: unknown type %q", r.ID, r.Type)
		}
		if _, dup := seen[r.ID]; dup {
			return nil, fmt.Errorf("package repo %q: duplicate id", r.ID)
		}
		seen[r.ID] = struct{}{}
		if err := validateRepoIndexes(r); err != nil {
			return nil, err
		}
	}
	return c.Repos, nil
}

// validateRepoIndexes rejects index entries a search could not act on. A deb
// index without a codename has no dists/ path to read, and a placeholder
// override URL is as unusable here as it is on the repo itself.
func validateRepoIndexes(r PackageRepo) error {
	for i, idx := range r.Index {
		if r.Type != repoTypeRPM && idx.Codename == "" {
			return fmt.Errorf("package repo %q index %d: missing codename", r.ID, i)
		}
		if strings.Contains(idx.URL, "<URL>") {
			return fmt.Errorf("package repo %q index %d: url is a placeholder", r.ID, i)
		}
	}
	return nil
}
