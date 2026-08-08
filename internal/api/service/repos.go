// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"embed"
	"fmt"
	"os"
	"sort"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"sigs.k8s.io/yaml"
)

//go:embed data/package-repos.yaml
var packageReposFS embed.FS

// defaultRepoPriority is the priority assigned to a repo that doesn't declare
// one. It matches both the OpenAPI default and normalizeRepositoryPriorities in
// internal/config, so the picker shows the same number a build would apply.
const defaultRepoPriority = 500

// PackageRepo is one repository offered by the Advanced tab's repository picker.
//
// This is presentation metadata, not build configuration: it labels and
// describes a repository and seeds its toggle. The repos a build actually uses
// still come from the template's packageRepositories block and the per-OS
// providerconfigs — see data/package-repos.yaml for the full rationale.
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
}

// packageRepoCatalog is the on-disk shape of the repository catalog.
type packageRepoCatalog struct {
	Repos []PackageRepo `json:"repos"`
}

// PackageRepos returns the repositories offered for a target OS id, ordered by
// descending priority and then by id so the picker is stable across calls.
//
// An empty osID returns the whole catalog: the UI asks for repos before a target
// is chosen, and showing everything is more useful there than showing nothing.
// An osID that matches no repo yields an empty (non-nil) slice rather than an
// error — an unknown target simply has nothing to offer, which the UI renders
// as an empty picker instead of a failure.
func (s *Service) PackageRepos(osID string) []PackageRepo {
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
// no OS list applies everywhere, and an empty query matches every repo.
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
		}
		if _, dup := seen[r.ID]; dup {
			return nil, fmt.Errorf("package repo %q: duplicate id", r.ID)
		}
		seen[r.ID] = struct{}{}
	}
	return c.Repos, nil
}
