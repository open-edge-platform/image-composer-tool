// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// packagesTestService is newTestService plus a robotics.yml overwritten with
// templateWithPackagesAndRepo, so ListPackageRepos/SearchPackages have a
// populated packageRepositories/allowPackages to work with.
func packagesTestService(t *testing.T) *Service {
	t.Helper()
	s := newTestService(t)
	if err := os.WriteFile(filepath.Join(s.cfg.TemplatesDir, "robotics.yml"), []byte(templateWithPackagesAndRepo), 0o644); err != nil {
		t.Fatal(err)
	}
	return s
}

func robotics() TemplateQuery {
	return TemplateQuery{Vertical: "robotics", SKU: "robotics-jazzy-ubuntu24", Platform: "wcl", OS: "ubuntu24", ImageType: "iso"}
}

func TestListPackageRepos(t *testing.T) {
	s := packagesTestService(t)

	repos, err := s.ListPackageRepos(robotics())
	if err != nil {
		t.Fatalf("ListPackageRepos: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("repos = %+v, want exactly the template's own single entry", repos)
	}
	if repos[0].ID != "test-repo" || repos[0].DisplayName != "test-repo" {
		t.Errorf("repo identity = %+v, want id/displayName derived from codename", repos[0])
	}
	if repos[0].URL != "https://example.com/repo" {
		t.Errorf("repo URL = %q, want the template's own url", repos[0].URL)
	}
}

func TestListPackageReposErrors(t *testing.T) {
	s := packagesTestService(t)
	cases := []struct {
		name string
		q    TemplateQuery
		want int
	}{
		{"missing fields", TemplateQuery{Vertical: "robotics"}, http.StatusBadRequest},
		{"no match", TemplateQuery{Vertical: "robotics", Platform: "ptl", OS: "ubuntu24", ImageType: "iso"}, http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := s.ListPackageRepos(c.q)
			assertServiceError(t, err, c.want)
		})
	}
}

func TestSearchPackages(t *testing.T) {
	s := packagesTestService(t)

	hits, total, err := s.SearchPackages(robotics(), "ba", nil, 0)
	if err != nil {
		t.Fatalf("SearchPackages: %v", err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2 (bar, baz-dev)", total)
	}
	if len(hits) != 2 || hits[0].Name != "bar" || hits[1].Name != "baz-dev" {
		t.Fatalf("hits = %+v, want [bar baz-dev] sorted", hits)
	}
	for _, h := range hits {
		if h.Version != "latest" {
			t.Errorf("hit %q version = %q, want latest (no package-index integration)", h.Name, h.Version)
		}
		if h.RepoID != "test-repo" {
			t.Errorf("hit %q repo id = %q, want test-repo", h.Name, h.RepoID)
		}
	}
}

func TestSearchPackagesRepoFilter(t *testing.T) {
	s := packagesTestService(t)

	// A filter naming an unrelated repo id must exclude every hit, even though
	// the query itself matches.
	hits, total, err := s.SearchPackages(robotics(), "ba", []string{"other-repo"}, 0)
	if err != nil {
		t.Fatalf("SearchPackages: %v", err)
	}
	if total != 0 || len(hits) != 0 {
		t.Fatalf("hits = %+v total = %d, want none (repo filter excludes the only repo)", hits, total)
	}

	hits, total, err = s.SearchPackages(robotics(), "ba", []string{"test-repo"}, 0)
	if err != nil {
		t.Fatalf("SearchPackages: %v", err)
	}
	if total != 2 || len(hits) != 2 {
		t.Fatalf("hits = %+v total = %d, want 2 (matching repo filter)", hits, total)
	}
}

func TestSearchPackagesLimit(t *testing.T) {
	s := packagesTestService(t)

	hits, total, err := s.SearchPackages(robotics(), "ba", nil, 1)
	if err != nil {
		t.Fatalf("SearchPackages: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2 (unaffected by limit)", total)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %+v, want capped to limit 1", hits)
	}
}

func TestSearchPackagesQueryTooShort(t *testing.T) {
	s := packagesTestService(t)

	_, _, err := s.SearchPackages(robotics(), "b", nil, 0)
	code, _ := assertServiceError(t, err, http.StatusBadRequest)
	if code != "BAD_REQUEST" {
		t.Errorf("code = %q, want BAD_REQUEST", code)
	}
}

func TestSearchPackagesNoMatchCombination(t *testing.T) {
	s := packagesTestService(t)

	_, _, err := s.SearchPackages(TemplateQuery{Vertical: "robotics", Platform: "ptl", OS: "ubuntu24", ImageType: "iso"}, "ba", nil, 0)
	assertServiceError(t, err, http.StatusBadRequest)
}

const templateWithPackagesAndRepo = `image:
  name: robotics-test
  version: "1.0"
target:
  os: ubuntu24
  dist: ubuntu24
  platform: wcl
  arch: x86_64
  imageType: iso
disk:
  name: disk0
  size: 5GB
systemConfig:
  hostname: robotics
packageRepositories:
  - codename: test-repo
    pkey: ubuntu
    url: https://example.com/repo
allowPackages:
  - bar
  - baz-dev
`
