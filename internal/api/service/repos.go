// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"embed"
	"fmt"
	"os"
	"sort"
	"strings"

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
}

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
