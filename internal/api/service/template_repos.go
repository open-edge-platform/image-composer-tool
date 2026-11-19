// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

// templateOnlyRepoPrefix starts the id of every repository registered by
// templateOnlyRepos, so one can never collide with a catalog id.
const templateOnlyRepoPrefix = "template-"

// templateOnlyRepos returns a catalog-shaped entry for each repository in a
// resolved template's packageRepositories that the catalog doesn't carry for
// osID, such as a vendor snapshot or a PPA only that template uses, so the
// Packages step can list and search it alongside the catalog's own.
//
// Each entry is registered as it is returned, so SearchPackages and
// LookupPackages accept its id from then on. GET /package-repos never lists
// one: it belongs to the templates declaring it, not to every template for the
// target. Only server-side templates ever reach this, so a client can't get
// the server to fetch an index from a URL of its choosing; it can only name an
// id a compose already returned. The set is bounded by the templates served.
//
// The id is derived from the target and the repository's own (url, suite,
// components), so it is the same on every compose and after a restart.
func (s *Service) templateOnlyRepos(osID string, repos []config.PackageRepository) []PackageRepo {
	catalog := s.catalogMatcher(osID)
	repoType := s.baseRepoType(osID)
	seen := make(map[string]bool)
	var out []PackageRepo
	for _, tr := range repos {
		if !usableTemplateRepo(tr) {
			continue
		}
		if _, ok := catalog.match(tr); ok {
			continue
		}
		r, ok := templateOnlyRepo(osID, repoType, tr)
		if !ok || seen[r.ID] {
			continue
		}
		seen[r.ID] = true
		out = append(out, r)
	}
	if len(out) > 0 {
		s.templateOnlyMu.Lock()
		if s.templateOnly == nil {
			s.templateOnly = make(map[string]PackageRepo)
		}
		for _, r := range out {
			s.templateOnly[r.ID] = r
		}
		s.templateOnlyMu.Unlock()
	}
	return out
}

// templateOnlyRepo renders one template repository entry as a catalog repo.
// Reports false for a deb entry with no suite, which has no index to read.
func templateOnlyRepo(osID, repoType string, tr config.PackageRepository) (PackageRepo, bool) {
	idx := RepoIndex{}
	ref := repoRef{url: normalizeRepoURL(tr.URL)}
	if repoType == repoTypeDeb {
		if strings.TrimSpace(tr.Codename) == "" {
			return PackageRepo{}, false
		}
		ref = normalizeRepoRef(tr.URL, tr.Codename, tr.Component)
		idx = RepoIndex{Codename: ref.codename, Component: ref.component}
	}
	// The allowPackages filter is part of the identity: two templates naming
	// one repository with different filters get separate entries, so neither
	// overwrites the other's.
	allow := append([]string(nil), tr.AllowPackages...)
	sort.Strings(allow)
	sum := sha256.Sum256([]byte(strings.Join([]string{osID, repoType, ref.url, ref.codename, ref.component, strings.Join(allow, " ")}, "\n")))
	priority := tr.Priority
	if priority == 0 {
		priority = defaultRepoPriority
	}
	return PackageRepo{
		ID:            templateOnlyRepoPrefix + hex.EncodeToString(sum[:6]),
		DisplayName:   templateRepoLabel(ref.url, ref.codename),
		URL:           ref.url,
		Description:   "Declared by the selected template's packageRepositories; not in the Web UI's repository catalog.",
		Priority:      priority,
		OS:            []string{osID},
		Type:          repoType,
		Index:         []RepoIndex{idx},
		PKey:          tr.PKey,
		AllowPackages: allow,
	}, true
}

// templateRepoLabel names a repository the catalog has no display name for
// by its host and the last two segments of its path, which is where vendor
// snapshots and PPAs keep what tells them apart, plus the suite when there is
// one: "ppa.launchpadcontent.net …/lp2168017/ubuntu (resolute)".
func templateRepoLabel(rawURL, codename string) string {
	label := rawURL
	if u, err := url.Parse(rawURL); err == nil && u.Host != "" {
		segs := strings.FieldsFunc(u.Path, func(r rune) bool { return r == '/' })
		switch {
		case len(segs) > 2:
			label = u.Host + " …/" + strings.Join(segs[len(segs)-2:], "/")
		case len(segs) > 0:
			label = u.Host + "/" + strings.Join(segs, "/")
		default:
			label = u.Host
		}
	}
	if codename != "" {
		label += " (" + codename + ")"
	}
	return label
}

// baseRepoType is the index layout of osID's base repository ("deb" or "rpm"),
// which is the layout every repository a template for that target declares.
func (s *Service) baseRepoType(osID string) string {
	for _, r := range s.PackageRepos(osID) {
		if r.EnabledByDefault && r.Type != "" {
			return r.Type
		}
	}
	return repoTypeDeb
}

// searchableRepos is filterReposByID over the catalog for osID plus, when ids
// names any, the template-only repositories registered for it. An empty ids
// means the whole catalog, without template-only ones: those are searched only
// when a client asks for one by the id a compose gave it.
func (s *Service) searchableRepos(osID string, ids []string) []PackageRepo {
	repos := s.PackageRepos(osID)
	if len(ids) == 0 {
		return repos
	}
	s.templateOnlyMu.RLock()
	for _, id := range ids {
		if r, ok := s.templateOnly[id]; ok && r.appliesTo(osID) {
			repos = append(repos, r)
		}
	}
	s.templateOnlyMu.RUnlock()
	return filterReposByID(repos, ids)
}
