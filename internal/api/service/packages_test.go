// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/api/service/pkgindex"
)

// gzipPackages compresses stanzas the way apt publishes Packages.gz.
func gzipPackages(t *testing.T, stanzas string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write([]byte(stanzas)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

// jazzyStanzas is a two-package deb index for the ros2-jazzy fixture repo.
const jazzyStanzas = `Package: ros-jazzy-demo-nodes-cpp
Version: 0.33.0-1
Architecture: amd64
Description: ROS 2 demo nodes in C++

Package: ros-jazzy-rviz2
Version: 14.1.4-1
Architecture: amd64
Description: ROS 2 3D visualization tool

`

// debFixtureServer serves a single Packages.gz for one (codename, component,
// arch) deb index, so SearchPackages has something real to fan out to.
func debFixtureServer(t *testing.T, codename, component, arch, stanzas string) *httptest.Server {
	t.Helper()
	path := fmt.Sprintf("/dists/%s/%s/binary-%s/Packages.gz", codename, component, arch)
	body := gzipPackages(t, stanzas)
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	})
	return httptest.NewServer(mux)
}

// newSearchTestService is newTestService with its catalog and pkgindex cache
// replaced, so a test controls exactly which repos/indexes a search fans out
// to and which HTTP client it fetches them through.
func newSearchTestService(t *testing.T, repos []PackageRepo, client *http.Client) *Service {
	t.Helper()
	s := newTestService(t)
	s.repos = repos
	s.pkgindexCache = pkgindex.New(pkgindex.Config{Client: client})
	return s
}

// jazzyRepo returns a one-repo, one-index catalog pointing at srv, with no
// arch pinned on the index so resolution goes through archesForOS("ubuntu24")
// -> "x86_64" -> debArch -> "amd64", exercising that translation.
func jazzyRepo(url string) []PackageRepo {
	return []PackageRepo{{
		ID: "ros2-jazzy", DisplayName: "ROS 2 Jazzy", URL: url,
		Index: []RepoIndex{{Codename: "noble", Component: "main"}},
	}}
}

func TestSearchPackagesBrowseAndQuery(t *testing.T) {
	srv := debFixtureServer(t, "noble", "main", "amd64", jazzyStanzas)
	defer srv.Close()
	s := newSearchTestService(t, jazzyRepo(srv.URL), srv.Client())

	hits, total, err := s.SearchPackages(context.Background(), "ubuntu24", "", nil, 0, 0)
	if err != nil {
		t.Fatalf("browse: %v", err)
	}
	if total != 2 || len(hits) != 2 {
		t.Fatalf("got %d hits (total %d), want 2", len(hits), total)
	}

	hits, total, err = s.SearchPackages(context.Background(), "ubuntu24", "ros-jazzy-rviz", nil, 0, 0)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if total != 1 || len(hits) != 1 || hits[0].Name != "ros-jazzy-rviz2" {
		t.Fatalf("got %+v (total %d), want exactly ros-jazzy-rviz2", hits, total)
	}
	if hits[0].RepoID != "ros2-jazzy" || hits[0].Version != "14.1.4-1" {
		t.Errorf("got repo %q version %q, want ros2-jazzy / 14.1.4-1", hits[0].RepoID, hits[0].Version)
	}
}

func TestSearchPackagesQueryTooShort(t *testing.T) {
	s := newSearchTestService(t, nil, http.DefaultClient)
	_, _, err := s.SearchPackages(context.Background(), "ubuntu24", "a", nil, 0, 0)
	assertServiceError(t, err, http.StatusBadRequest)
}

func TestSearchPackagesUnknownOS(t *testing.T) {
	srv := debFixtureServer(t, "noble", "main", "amd64", jazzyStanzas)
	defer srv.Close()
	s := newSearchTestService(t, jazzyRepo(srv.URL), srv.Client())

	hits, total, err := s.SearchPackages(context.Background(), "not-a-real-os", "", nil, 0, 0)
	if err != nil {
		t.Fatalf("unknown os: %v", err)
	}
	if total != 0 || len(hits) != 0 {
		t.Fatalf("got %d hits, want 0 for an unknown os", len(hits))
	}
}

func TestSearchPackagesRepoFilter(t *testing.T) {
	srv := debFixtureServer(t, "noble", "main", "amd64", jazzyStanzas)
	defer srv.Close()
	repos := jazzyRepo(srv.URL)
	repos = append(repos, PackageRepo{ID: "other-repo", DisplayName: "Other", URL: srv.URL})
	s := newSearchTestService(t, repos, srv.Client())

	hits, _, err := s.SearchPackages(context.Background(), "ubuntu24", "", []string{"other-repo"}, 0, 0)
	if err != nil {
		t.Fatalf("SearchPackages: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("got %d hits from a repo filter naming only the indexless repo, want 0", len(hits))
	}
}

func TestSearchPackagesPagination(t *testing.T) {
	srv := debFixtureServer(t, "noble", "main", "amd64", jazzyStanzas)
	defer srv.Close()
	s := newSearchTestService(t, jazzyRepo(srv.URL), srv.Client())

	hits, total, err := s.SearchPackages(context.Background(), "ubuntu24", "", nil, 1, 1)
	if err != nil {
		t.Fatalf("SearchPackages: %v", err)
	}
	if total != 2 || len(hits) != 1 {
		t.Fatalf("got %d hits (total %d), want 1 hit of 2", len(hits), total)
	}
	// Sorted by name: ros-jazzy-demo-nodes-cpp, then ros-jazzy-rviz2. offset=1
	// skips the first.
	if hits[0].Name != "ros-jazzy-rviz2" {
		t.Errorf("got %q at offset 1, want ros-jazzy-rviz2", hits[0].Name)
	}
}

func TestSearchPackagesLookupFailureSkipsRepo(t *testing.T) {
	good := debFixtureServer(t, "noble", "main", "amd64", jazzyStanzas)
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()

	repos := jazzyRepo(good.URL)
	repos = append(repos, PackageRepo{
		ID: "broken-repo", DisplayName: "Broken", URL: bad.URL,
		Index: []RepoIndex{{Codename: "noble", Component: "main"}},
	})
	s := newSearchTestService(t, repos, good.Client())

	hits, total, err := s.SearchPackages(context.Background(), "ubuntu24", "", nil, 0, 0)
	if err != nil {
		t.Fatalf("a broken repo should not fail the whole search: %v", err)
	}
	if total != 2 || len(hits) != 2 {
		t.Fatalf("got %d hits (total %d), want the good repo's 2 despite the broken one", len(hits), total)
	}
}

func TestSearchPackagesRepoWithNoIndexIsSkipped(t *testing.T) {
	s := newSearchTestService(t, []PackageRepo{{ID: "no-index", DisplayName: "No Index", URL: "http://example.invalid"}},
		http.DefaultClient)
	hits, total, err := s.SearchPackages(context.Background(), "ubuntu24", "", nil, 0, 0)
	if err != nil {
		t.Fatalf("SearchPackages: %v", err)
	}
	if total != 0 || len(hits) != 0 {
		t.Fatalf("got %d hits, want 0 for a repo with no Index entries", len(hits))
	}
}

func TestDedupAndFilterGroupsVersions(t *testing.T) {
	// A Debian-style repository publishes release, -updates and -security as
	// separate indexes, so the same package arrives several times at different
	// versions. They must collapse into one hit offering every version.
	deb := func(string) bool { return false }
	results := []lookupResult{
		{repoID: "noble", entries: []pkgindex.Entry{
			{Name: "curl", Version: "8.5.0-2ubuntu10.9", Description: "transfer a URL"},
			{Name: "vim", Version: "2.0"},
			{Name: "nginx", Version: "1.0~rc1"},
		}},
		{repoID: "noble", entries: []pkgindex.Entry{
			{Name: "curl", Version: "8.5.0-2ubuntu10.13"},
			{Name: "curl", Version: "8.5.0-2ubuntu10.9"}, // duplicate, must not repeat
			{Name: "vim", Version: "1:1.0"},
			{Name: "nginx", Version: "1.0"},
		}},
	}

	hits := dedupAndFilter(results, "", deb)
	if len(hits) != 3 {
		t.Fatalf("got %d hits, want 3 (one per package name): %+v", len(hits), hits)
	}

	byName := map[string]PackageSearchHit{}
	for _, h := range hits {
		byName[h.Name] = h
	}

	curl := byName["curl"]
	if len(curl.Versions) != 2 {
		t.Errorf("curl has %d versions, want 2: %+v", len(curl.Versions), curl.Versions)
	}
	if curl.Version != curl.Versions[0].Version || curl.RepoID != curl.Versions[0].RepoID {
		t.Errorf("hit fields %q/%q do not mirror Versions[0] %+v", curl.Version, curl.RepoID, curl.Versions[0])
	}
	if curl.Description != "transfer a URL" {
		t.Errorf("description = %q; a later suite's empty one overwrote it", curl.Description)
	}

	// Each case inverts under plain string comparison, so these pin the
	// ordering to the target's real version rules rather than to byte order.
	for _, tc := range []struct{ name, wantNewest, why string }{
		{"curl", "8.5.0-2ubuntu10.13", "numeric segments: 13 > 9, but \"...10.9\" > \"...10.13\" as strings"},
		{"vim", "1:1.0", "an epoch outranks a higher upstream version"},
		{"nginx", "1.0", "a tilde sorts before its release, so 1.0 > 1.0~rc1"},
	} {
		if got := byName[tc.name].Versions[0].Version; got != tc.wantNewest {
			t.Errorf("%s newest = %q, want %q (%s)", tc.name, got, tc.wantNewest, tc.why)
		}
	}
}

func TestDedupAndFilterVersionsCarryTheirRepo(t *testing.T) {
	// The same package from two repositories: pinning a version has to pick
	// the repository that actually provides it.
	deb := func(string) bool { return false }
	results := []lookupResult{
		{repoID: "ubuntu-noble-base", entries: []pkgindex.Entry{{Name: "gz-harmonic", Version: "8.8.2-1~noble"}}},
		{repoID: "gazebo", entries: []pkgindex.Entry{{Name: "gz-harmonic", Version: "8.9.0-1~noble"}}},
	}
	hits := dedupAndFilter(results, "", deb)
	if len(hits) != 1 {
		t.Fatalf("got %d hits, want 1", len(hits))
	}
	got := hits[0].Versions
	if len(got) != 2 {
		t.Fatalf("got %d versions, want 2: %+v", len(got), got)
	}
	if got[0].Version != "8.9.0-1~noble" || got[0].RepoID != "gazebo" {
		t.Errorf("newest = %+v, want 8.9.0-1~noble from gazebo", got[0])
	}
	if got[1].RepoID != "ubuntu-noble-base" {
		t.Errorf("older version lost its repository: %+v", got[1])
	}
}

func TestSearchPackagesLimitCountsPackagesNotVersions(t *testing.T) {
	// `limit` must page over packages; counting versions would silently return
	// fewer packages than asked for whenever any of them had an update.
	deb := func(string) bool { return false }
	results := []lookupResult{
		{repoID: "noble", entries: []pkgindex.Entry{
			{Name: "aaa", Version: "1.0"}, {Name: "aaa", Version: "1.1"},
			{Name: "bbb", Version: "2.0"}, {Name: "bbb", Version: "2.1"},
			{Name: "ccc", Version: "3.0"},
		}},
	}
	hits := dedupAndFilter(results, "", deb)
	if len(hits) != 3 {
		t.Fatalf("got %d hits, want 3 packages", len(hits))
	}
	if got := paginate(hits, 0, 2); len(got) != 2 {
		t.Errorf("paginate returned %d, want 2 packages", len(got))
	}
}
