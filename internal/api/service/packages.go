// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

// TemplateQuery is the 5-tuple used to look up the matched curated template
// for the Packages step — the same lookup Compose uses, minus the
// Advanced-mode override fields that don't apply to browsing/searching.
type TemplateQuery struct {
	Vertical  string
	SKU       string
	Platform  string
	OS        string
	ImageType string
}

// resolveCuratedTemplate looks up q's matched template and loads it
// unmerged — i.e. the curated template's own fields, not the fully resolved
// extends+OS-defaults chain. This mirrors deltaForOverride's parent load and
// backs the ticket's decision that /package-repos and /packages/search
// reflect the matched template's own packageRepositories, not a synthesized
// global catalog.
func (s *Service) resolveCuratedTemplate(q TemplateQuery) (*config.ImageTemplate, error) {
	if q.Vertical == "" || q.Platform == "" || q.OS == "" || q.ImageType == "" {
		return nil, newError(http.StatusBadRequest, "BAD_REQUEST",
			"vertical, platform, os, and imageType are required")
	}
	tmpl := s.manifest.findTemplate(q.Vertical, q.SKU, q.Platform, q.OS, q.ImageType)
	if tmpl == "" {
		return nil, newError(http.StatusBadRequest, "NO_MATCH",
			"no template maps to the selected combination")
	}
	path, err := safeTemplatePath(s.cfg.TemplatesDir, tmpl)
	if err != nil {
		return nil, newError(http.StatusInternalServerError, "TEMPLATE_INVALID",
			"manifest template path is invalid")
	}
	if _, statErr := os.Stat(path); statErr != nil {
		if os.IsNotExist(statErr) {
			return nil, newError(http.StatusInternalServerError, "TEMPLATE_MISSING",
				"matched template file not found on disk")
		}
		return nil, newError(http.StatusInternalServerError, "TEMPLATE_STAT_FAILED",
			fmt.Sprintf("checking matched template file: %v", statErr))
	}
	parent, err := config.LoadTemplate(path, false)
	if err != nil {
		return nil, newError(http.StatusUnprocessableEntity, "TEMPLATE_INVALID",
			"matched template failed to load: "+err.Error())
	}
	return parent, nil
}

// repoIdentity derives a stable (id, displayName) pair for a repository,
// falling back the same way getRepositoryName (internal/config/apt_sources.go)
// does: explicit ID, then codename, then URL — so a repo without an
// auto-assigned ID still gets a usable, consistent identifier across
// /package-repos and /packages/search's repos filter.
func repoIdentity(repo config.PackageRepository) (id, displayName string) {
	switch {
	case repo.ID != "":
		return repo.ID, repo.ID
	case repo.Codename != "":
		return repo.Codename, repo.Codename
	default:
		return repo.URL, repo.URL
	}
}

// PackageRepoInfo is one repository declared on the matched curated
// template, with its identity resolved the same way SearchPackages resolves
// each hit's repo — so a repos filter passed to SearchPackages always lines
// up with the id returned here.
type PackageRepoInfo struct {
	ID          string
	DisplayName string
	URL         string
	Priority    int // 0 means "unset" — caller falls back to the schema default.
}

// ListPackageRepos returns the matched curated template's own
// packageRepositories entries. There is no separate, global repository
// catalog — see TemplateQuery's doc comment.
func (s *Service) ListPackageRepos(q TemplateQuery) ([]PackageRepoInfo, error) {
	parent, err := s.resolveCuratedTemplate(q)
	if err != nil {
		return nil, err
	}
	out := make([]PackageRepoInfo, len(parent.PackageRepositories))
	for i, repo := range parent.PackageRepositories {
		id, displayName := repoIdentity(repo)
		out[i] = PackageRepoInfo{ID: id, DisplayName: displayName, URL: repo.URL, Priority: repo.Priority}
	}
	return out, nil
}

// PackageSearchHit is one package search result: a package name available
// from one of the matched template's repositories.
type PackageSearchHit struct {
	Name            string
	Version         string
	Description     string
	RepoID          string
	RepoDisplayName string
}

const (
	defaultSearchLimit = 20
	maxSearchLimit     = 50
	minSearchQueryLen  = 2
)

// SearchPackages prefix-matches query against the AllowPackages lists of the
// matched curated template's own packageRepositories (there is no
// package-index integration, so AllowPackages — the only place real package
// names appear in a template — is the sole data source). Every hit's Version
// is the literal "latest": without a real package index there is no version
// history to report, so a specific version can only be pinned by hand in the
// UI, not chosen from a real list.
//
// total is the full match count before limit is applied, so callers can
// report "N of total" even though hits itself is capped.
func (s *Service) SearchPackages(q TemplateQuery, query string, repoFilter []string, limit int) (hits []PackageSearchHit, total int, err error) {
	if len(query) < minSearchQueryLen {
		return nil, 0, newError(http.StatusBadRequest, "BAD_REQUEST",
			fmt.Sprintf("query must be at least %d characters", minSearchQueryLen))
	}
	parent, err := s.resolveCuratedTemplate(q)
	if err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}

	var allow map[string]bool
	if len(repoFilter) > 0 {
		allow = make(map[string]bool, len(repoFilter))
		for _, r := range repoFilter {
			allow[r] = true
		}
	}

	needle := strings.ToLower(query)
	for _, repo := range parent.PackageRepositories {
		id, displayName := repoIdentity(repo)
		if allow != nil && !allow[id] {
			continue
		}
		for _, pkg := range repo.AllowPackages {
			if !strings.HasPrefix(strings.ToLower(pkg), needle) {
				continue
			}
			hits = append(hits, PackageSearchHit{
				Name:            pkg,
				Version:         "latest",
				RepoID:          id,
				RepoDisplayName: displayName,
			})
		}
	}

	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Name != hits[j].Name {
			return hits[i].Name < hits[j].Name
		}
		return hits[i].RepoID < hits[j].RepoID
	})
	total = len(hits)
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, total, nil
}
