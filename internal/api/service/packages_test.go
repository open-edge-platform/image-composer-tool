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
