// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/api/service/pkgindex"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"golang.org/x/sync/errgroup"
)

// PackageSearchHit is one package search result: a package available from
// one of osID's catalog repositories.
type PackageSearchHit struct {
	Name        string
	Version     string
	Description string
	RepoID      string
}

const (
	defaultSearchLimit = 20
	maxSearchLimit     = 50
	minSearchQueryLen  = 2
	// maxConcurrentLookups bounds how many pkgindex.Cache.Lookup calls one
	// search issues at once, so fanning out across a repo's several indexes
	// doesn't open dozens of simultaneous upstream connections.
	maxConcurrentLookups = 8
)

// SearchPackages searches (or, with an empty query, browses) the package
// indexes of osID's catalog repositories.
//
// Unknown or empty osID behaves exactly like PackageRepos: an empty osID
// searches the whole catalog, an osID the manifest doesn't know returns an
// empty, non-error result. A non-empty query shorter than minSearchQueryLen
// is a 400 — empty is allowed (it means browse) but a single character is not
// a useful prefix.
//
// total is the deduplicated match count before offset/limit is applied, so
// callers can report "N of total" even though hits itself is paginated.
func (s *Service) SearchPackages(ctx context.Context, osID, query string, repoFilter []string, limit, offset int) (hits []PackageSearchHit, total int, err error) {
	if query != "" && len(query) < minSearchQueryLen {
		return nil, 0, newError(http.StatusBadRequest, "BAD_REQUEST",
			fmt.Sprintf("query must be at least %d characters", minSearchQueryLen))
	}
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}
	if offset < 0 {
		offset = 0
	}

	repos := filterReposByID(s.PackageRepos(osID), repoFilter)
	results := runLookups(ctx, s.pkgindexCache, s.planLookups(osID, repos))

	hits = dedupAndFilter(results, query)
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Name != hits[j].Name {
			return hits[i].Name < hits[j].Name
		}
		return hits[i].RepoID < hits[j].RepoID
	})
	return paginate(hits, offset, limit), len(hits), nil
}

// filterReposByID keeps only repos whose ID appears in ids; an empty ids
// keeps everything.
func filterReposByID(repos []PackageRepo, ids []string) []PackageRepo {
	if len(ids) == 0 {
		return repos
	}
	allow := make(map[string]bool, len(ids))
	for _, id := range ids {
		allow[id] = true
	}
	out := make([]PackageRepo, 0, len(repos))
	for _, r := range repos {
		if allow[r.ID] {
			out = append(out, r)
		}
	}
	return out
}

// repoLookup pairs a pkgindex.Repo with the catalog repo id it came from, so
// a fetched entry can be attributed back to it.
type repoLookup struct {
	repo   pkgindex.Repo
	repoID string
}

// planLookups expands each repo's Index entries into individual pkgindex.Repo
// lookups: deb splits Component on whitespace and pairs every component with
// every resolved arch; rpm gets one lookup per arch. An index's own Arch wins
// when set; empty resolves via archesForOS, since most of the catalog omits
// it to mean "every arch the OS builds".
func (s *Service) planLookups(osID string, repos []PackageRepo) []repoLookup {
	osArches := s.manifest.archesForOS(osID)
	var lookups []repoLookup
	for _, r := range repos {
		repoType := r.Type
		if repoType == "" {
			repoType = repoTypeDeb
		}
		gpgKeyPath := verifiableGPGKeyPath(r.GPGKeyPath)
		for _, idx := range r.Index {
			lookups = append(lookups, indexLookups(r, idx, repoType, gpgKeyPath, osArches)...)
		}
	}
	return lookups
}

// verifiableGPGKeyPath returns path if it names a file that exists on disk,
// else "" — a configured-but-missing key silently falls back to unverified
// rather than failing every lookup for that repo.
func verifiableGPGKeyPath(path string) string {
	if path == "" {
		return ""
	}
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

// indexLookups expands one RepoIndex into its component pkgindex.Repo lookups.
func indexLookups(r PackageRepo, idx RepoIndex, repoType, gpgKeyPath string, osArches []string) []repoLookup {
	url := idx.URL
	if url == "" {
		url = r.URL
	}
	arches := idx.Arch
	if len(arches) == 0 {
		arches = osArches
	}
	components := []string{""}
	if repoType == repoTypeDeb && idx.Component != "" {
		components = strings.Fields(idx.Component)
	}

	var out []repoLookup
	for _, arch := range arches {
		if repoType == repoTypeDeb {
			arch = debArch(arch)
		}
		for _, comp := range components {
			out = append(out, repoLookup{
				repo: pkgindex.Repo{
					Type: repoType, URL: url, Codename: idx.Codename,
					Component: comp, Arch: arch, GPGKeyPath: gpgKeyPath,
				},
				repoID: r.ID,
			})
		}
	}
	return out
}

// debArch maps a manifest/catalog arch name to the name deb archives publish
// their binary-<arch>/ directories under. Provider Init does the same
// translation for builds (internal/provider/ubuntu/ubuntu.go); rpm needs no
// equivalent, since dnf repodata already uses the manifest's own x86_64 /
// aarch64 names.
func debArch(arch string) string {
	switch arch {
	case "x86_64":
		return "amd64"
	case "aarch64":
		return "arm64"
	default:
		return arch
	}
}

// lookupResult pairs one repo's fetched entries with the catalog repo id they
// belong to.
type lookupResult struct {
	entries []pkgindex.Entry
	repoID  string
}

// runLookups fetches every planned lookup concurrently, bounded by
// maxConcurrentLookups. A failed lookup logs a warning and contributes
// nothing rather than failing the whole search — the same tolerance any
// other per-index fetch error already gets.
func runLookups(ctx context.Context, cache *pkgindex.Cache, lookups []repoLookup) []lookupResult {
	results := make([]lookupResult, len(lookups))
	g := new(errgroup.Group)
	g.SetLimit(maxConcurrentLookups)
	for i, lk := range lookups {
		i, lk := i, lk
		g.Go(func() error {
			entries, err := cache.Lookup(ctx, lk.repo)
			if err != nil {
				logger.Logger().Warnf("pkgindex: search lookup failed for repo %s (%s %s %s %s): %v",
					lk.repoID, lk.repo.Type, lk.repo.URL, lk.repo.Codename, lk.repo.Arch, err)
				return nil
			}
			results[i] = lookupResult{entries: entries, repoID: lk.repoID}
			return nil
		})
	}
	_ = g.Wait() // workers never return a non-nil error; nothing to check
	return results
}

// dedupAndFilter flattens results into hits whose Name matches query as a
// case-insensitive prefix (query empty matches everything), deduplicating by
// (Name, Version, RepoID) — the wire schema has no arch field, so the same
// package published for several arches would otherwise appear once per arch.
func dedupAndFilter(results []lookupResult, query string) []PackageSearchHit {
	type key struct{ name, version, repoID string }
	seen := make(map[key]bool)
	needle := strings.ToLower(query)
	hits := make([]PackageSearchHit, 0)
	for _, res := range results {
		for _, e := range res.entries {
			if needle != "" && !strings.HasPrefix(strings.ToLower(e.Name), needle) {
				continue
			}
			k := key{e.Name, e.Version, res.repoID}
			if seen[k] {
				continue
			}
			seen[k] = true
			hits = append(hits, PackageSearchHit{
				Name: e.Name, Version: e.Version, Description: e.Description, RepoID: res.repoID,
			})
		}
	}
	return hits
}

// paginate slices hits by offset/limit, clamped to bounds.
func paginate(hits []PackageSearchHit, offset, limit int) []PackageSearchHit {
	if offset >= len(hits) {
		return []PackageSearchHit{}
	}
	end := offset + limit
	if end > len(hits) {
		end = len(hits)
	}
	return hits[offset:end]
}
