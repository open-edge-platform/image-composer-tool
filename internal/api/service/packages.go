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
	"sync"

	"github.com/open-edge-platform/image-composer-tool/internal/api/service/pkgindex"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage/debutils"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage/rpmutils"
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
	// Versions is every version of this package the search found, newest
	// first; Version/RepoID mirror Versions[0]. A package routinely exists at
	// several versions because a Debian-style repository publishes its
	// release, -updates and -security suites as separate indexes.
	Versions []PackageVersion
}

// PackageVersion is one available version of a package and the repository
// providing it. Pinning a version therefore also picks a repository.
type PackageVersion struct {
	Version string
	RepoID  string
}

const (
	defaultSearchLimit = 20
	maxSearchLimit     = 100
	minSearchQueryLen  = 2
	// maxConcurrentLookups bounds how many pkgindex.Cache.Lookup calls one
	// search issues at once, so fanning out across a repo's several indexes
	// doesn't open dozens of simultaneous upstream connections.
	maxConcurrentLookups = 8
	// maxConcurrentStreamLookups is the same bound for the streaming search,
	// raised because that path fans out across every repository the target
	// offers rather than a filtered few. At 8, a handful of unreachable mirrors
	// hold most of the slots for their full dial timeout and starve the
	// reachable repositories behind them — which would defeat the point of
	// streaming, since the early results are exactly what it exists to deliver.
	maxConcurrentStreamLookups = 24
)

// ValidateSearchQuery rejects a query that is present but too short to be a
// useful prefix. Empty is allowed — it means browse rather than search.
//
// It is exported because the streaming search has to screen the query before it
// writes any SSE header: once the stream is open the status is already sent, so
// a bad query can no longer be reported as a 400.
func (s *Service) ValidateSearchQuery(query string) error {
	if query != "" && len(query) < minSearchQueryLen {
		return newError(http.StatusBadRequest, "BAD_REQUEST",
			fmt.Sprintf("query must be at least %d characters", minSearchQueryLen))
	}
	return nil
}

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
//
// curatedOnly narrows the result to the matched repos' hand-picked
// CuratedPackages instead of their full catalog — what the browse pane's
// "Show frequently used" toggle uses. A matched repo with no curated list
// contributes nothing to that narrowed set, so the caller sees zero results
// rather than silently falling back to the full catalog.
func (s *Service) SearchPackages(ctx context.Context, osID, query string, repoFilter []string, limit, offset int, curatedOnly bool) (hits []PackageSearchHit, total int, err error) {
	if err := s.ValidateSearchQuery(query); err != nil {
		return nil, 0, err
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
	lookups := s.planLookups(osID, repos)
	results := runLookups(ctx, s.pkgindexCache, lookups)

	hits = dedupAndFilter(results, query, rpmRepoSet(lookups))
	if curatedOnly {
		hits = filterCurated(hits, curatedNameSet(repos))
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Name != hits[j].Name {
			return hits[i].Name < hits[j].Name
		}
		return hits[i].RepoID < hits[j].RepoID
	})
	return paginate(hits, offset, limit), len(hits), nil
}

// curatedNameSet unions the CuratedPackages of every given repo into a
// lookup set.
func curatedNameSet(repos []PackageRepo) map[string]bool {
	set := make(map[string]bool)
	for _, r := range repos {
		for _, name := range r.CuratedPackages {
			set[name] = true
		}
	}
	return set
}

// filterCurated keeps only hits whose Name is in curated.
func filterCurated(hits []PackageSearchHit, curated map[string]bool) []PackageSearchHit {
	out := make([]PackageSearchHit, 0, len(hits))
	for _, h := range hits {
		if curated[h.Name] {
			out = append(out, h)
		}
	}
	return out
}

// PackageSearchBatch is one repository's contribution to a streaming search.
// Err is set when every index that repository publishes failed to load, in
// which case Hits is empty; a repository whose indexes partly loaded reports
// the hits it did find rather than an error, matching how the non-streaming
// search tolerates a single bad index.
type PackageSearchBatch struct {
	RepoID string
	Hits   []PackageSearchHit
	Err    error
}

// SearchPackagesStream is SearchPackages with the results delivered per
// repository as each one finishes, rather than collected and returned once the
// slowest has. It exists because a search fans out across the whole catalog:
// collecting first means one unreachable mirror sets the latency of the entire
// search, while results already gathered from reachable repositories sit unsent.
//
// emit is called once per repository, from several goroutines but never
// concurrently, and must not block for long. limit caps each repository's
// contribution — there is no global ranking to page through here, so merging
// and ranking the batches is the caller's job. The returned count is how many
// repositories were planned, so a caller can report what it did not hear from.
//
// ctx bounds the whole fan-out: when it is done, emit stops being called and
// the function returns. Lookups already in flight are not wasted — pkgindex
// detaches its fetches, so they finish and warm the cache for the next search.
func (s *Service) SearchPackagesStream(ctx context.Context, osID, query string,
	repoFilter []string, limit int, emit func(PackageSearchBatch)) (planned int, err error) {
	if err := s.ValidateSearchQuery(query); err != nil {
		return 0, err
	}
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}

	repos := filterReposByID(s.PackageRepos(osID), repoFilter)
	groups := groupByRepo(s.planLookups(osID, repos))

	// One goroutine per repository, all drawing their index lookups from a
	// shared slot budget, so a repository is emitted the moment its own indexes
	// are in without any repository being able to monopolise the upstream
	// connections.
	sem := make(chan struct{}, maxConcurrentStreamLookups)
	var emitMu sync.Mutex
	var wg sync.WaitGroup
	for _, g := range groups {
		wg.Add(1)
		go func(g repoGroup) {
			defer wg.Done()
			batch := s.lookupRepo(ctx, g, sem, query, limit)
			emitMu.Lock()
			defer emitMu.Unlock()
			if ctx.Err() != nil {
				return // the caller stopped listening; drop the batch
			}
			emit(batch)
		}(g)
	}
	wg.Wait()
	return len(groups), nil
}

// repoGroup is every index lookup belonging to one catalog repository, so that
// repository can be reported as a unit.
type repoGroup struct {
	repoID  string
	lookups []repoLookup
}

// groupByRepo collects lookups by their catalog repo id, preserving the order
// planLookups produced (catalog order, which is priority-descending).
func groupByRepo(lookups []repoLookup) []repoGroup {
	var groups []repoGroup
	at := make(map[string]int)
	for _, lk := range lookups {
		i, ok := at[lk.repoID]
		if !ok {
			at[lk.repoID] = len(groups)
			groups = append(groups, repoGroup{repoID: lk.repoID})
			i = len(groups) - 1
		}
		groups[i].lookups = append(groups[i].lookups, lk)
	}
	return groups
}

// lookupRepo loads every index of one repository, each taking a slot from sem,
// and reduces them to that repository's batch.
func (s *Service) lookupRepo(ctx context.Context, g repoGroup, sem chan struct{},
	query string, limit int) PackageSearchBatch {
	results := make([]lookupResult, len(g.lookups))
	errs := make([]error, len(g.lookups))
	var wg sync.WaitGroup
	for i, lk := range g.lookups {
		wg.Add(1)
		go func(i int, lk repoLookup) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				errs[i] = ctx.Err()
				return
			}
			entries, err := s.pkgindexCache.Lookup(ctx, lk.repo)
			if err != nil {
				errs[i] = err
				logger.Logger().Warnf("pkgindex: search lookup failed for repo %s (%s %s %s %s): %v",
					lk.repoID, lk.repo.Type, lk.repo.URL, lk.repo.Codename, lk.repo.Arch, err)
				return
			}
			results[i] = lookupResult{entries: entries, repoID: lk.repoID}
		}(i, lk)
	}
	wg.Wait()

	// Only a repository whose every index failed is reported as failed: a
	// partial load still has something honest to show.
	if failed := countNonNil(errs); failed == len(errs) && failed > 0 {
		return PackageSearchBatch{RepoID: g.repoID, Err: errs[0]}
	}
	hits := dedupAndFilter(results, query, rpmRepoSet(g.lookups))
	sort.Slice(hits, func(i, j int) bool { return hits[i].Name < hits[j].Name })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return PackageSearchBatch{RepoID: g.repoID, Hits: hits}
}

func countNonNil(errs []error) int {
	n := 0
	for _, e := range errs {
		if e != nil {
			n++
		}
	}
	return n
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
	arches := archesToFetch(idx.Arch, osArches)
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

// archesToFetch narrows an index's declared arches to the ones the target
// actually builds for. A catalog entry commonly lists an arch per publishing
// host — Ubuntu serves x86_64 from archive.ubuntu.com and aarch64 from
// ports.ubuntu.com — and fetching both meant downloading an 18 MB index the
// selected target can never install from, doubling the cost of every cold
// search.
//
// An index that declares no arch means "every arch the OS builds", which
// osArches already answers. An index that declares arches but none matching
// the target is dropped: it has nothing that target could install.
func archesToFetch(idxArches, osArches []string) []string {
	if len(idxArches) == 0 {
		return osArches
	}
	// Nothing to narrow against (an OS the manifest doesn't list): keep the
	// index's own arches rather than silently dropping every lookup.
	if len(osArches) == 0 {
		return idxArches
	}
	want := make(map[string]bool, len(osArches))
	for _, a := range osArches {
		want[a] = true
	}
	var out []string
	for _, a := range idxArches {
		if want[a] {
			out = append(out, a)
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

// rpmRepoSet reports which of these lookups' repositories are rpm, so version
// ordering can pick the matching comparator. Derived from the planned lookups
// rather than re-read from the catalog, so it can only describe repositories
// this search actually touched.
func rpmRepoSet(lookups []repoLookup) func(repoID string) bool {
	rpm := make(map[string]bool)
	for _, lk := range lookups {
		if lk.repo.Type == pkgindex.TypeRPM {
			rpm[lk.repoID] = true
		}
	}
	return func(repoID string) bool { return rpm[repoID] }
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
// dedupAndFilter reduces raw index entries to one hit per package name,
// carrying every distinct version found so the picker can offer a choice.
//
// It groups rather than emitting a row per version because the same package
// legitimately appears in several indexes: a Debian-style repository publishes
// release, -updates and -security separately, so `curl` is present at both
// 8.5.0-2ubuntu10 and 8.5.0-2ubuntu10.13. Listing those as unrelated rows
// would read as two different packages, and would also make `limit` count
// versions instead of packages.
//
// isRPM selects the version ordering: deb and rpm order versions by different
// rules, and neither is string ordering.
func dedupAndFilter(results []lookupResult, query string, isRPM func(repoID string) bool) []PackageSearchHit {
	type versionKey struct{ version, repoID string }
	needle := strings.ToLower(query)
	byName := make(map[string]*PackageSearchHit)
	seen := make(map[string]map[versionKey]bool)
	var order []string

	for _, res := range results {
		for _, e := range res.entries {
			if needle != "" && !strings.HasPrefix(strings.ToLower(e.Name), needle) {
				continue
			}
			hit, ok := byName[e.Name]
			if !ok {
				hit = &PackageSearchHit{Name: e.Name, Description: e.Description}
				byName[e.Name] = hit
				seen[e.Name] = make(map[versionKey]bool)
				order = append(order, e.Name)
			}
			// The first index to describe a package wins the description;
			// later suites repeat it and an empty one shouldn't blank it out.
			if hit.Description == "" {
				hit.Description = e.Description
			}
			k := versionKey{e.Version, res.repoID}
			if seen[e.Name][k] {
				continue
			}
			seen[e.Name][k] = true
			hit.Versions = append(hit.Versions, PackageVersion{Version: e.Version, RepoID: res.repoID})
		}
	}

	hits := make([]PackageSearchHit, 0, len(order))
	for _, name := range order {
		hit := byName[name]
		sortVersionsNewestFirst(hit.Versions, isRPM)
		hit.Version = hit.Versions[0].Version
		hit.RepoID = hit.Versions[0].RepoID
		hits = append(hits, *hit)
	}
	return hits
}

// sortVersionsNewestFirst orders versions by the target's own version rules,
// reusing the build path's comparators rather than a second implementation.
// Ties break on repository ID so the result is stable regardless of the order
// indexes happened to load in.
func sortVersionsNewestFirst(versions []PackageVersion, isRPM func(repoID string) bool) {
	sort.SliceStable(versions, func(i, j int) bool {
		a, b := versions[i], versions[j]
		var cmp int
		if isRPM(a.RepoID) && isRPM(b.RepoID) {
			cmp, _ = rpmutils.CompareRPMVersions(a.Version, b.Version)
		} else {
			cmp, _ = debutils.CompareDebianVersions(a.Version, b.Version)
		}
		if cmp != 0 {
			return cmp > 0 // newest first
		}
		return a.RepoID < b.RepoID
	})
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
