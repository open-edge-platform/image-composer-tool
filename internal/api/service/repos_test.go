// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeRepos writes a catalog YAML to a temp file and returns its path, for
// exercising the on-disk override path.
func writeRepos(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "package-repos.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	return p
}

// reposService builds a Service around a caller-supplied catalog, bypassing the
// build/template config the repo endpoints don't touch.
func reposService(t *testing.T, catalog string) *Service {
	t.Helper()
	repos, err := loadPackageRepos(writeRepos(t, catalog))
	if err != nil {
		t.Fatalf("loadPackageRepos: %v", err)
	}
	return &Service{repos: repos}
}

// The embedded catalog is what ships, so it must satisfy the same invariants the
// loader enforces on an operator-supplied file.
func TestLoadPackageReposEmbedded(t *testing.T) {
	repos, err := loadPackageRepos("")
	if err != nil {
		t.Fatalf("loading embedded catalog: %v", err)
	}
	if len(repos) == 0 {
		t.Fatal("embedded catalog is empty")
	}
	for _, r := range repos {
		if r.ID == "" || r.DisplayName == "" || r.URL == "" {
			t.Errorf("embedded repo incomplete: %+v", r)
		}
	}
}

// Every manifest target must offer repos, and exactly one base repo per target
// may be on by default — otherwise the picker opens with nothing selected (or
// with another OS's base repo selected).
func TestEmbeddedCatalogCoversManifestTargets(t *testing.T) {
	m, err := loadManifest("")
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	repos, err := loadPackageRepos("")
	if err != nil {
		t.Fatalf("loadPackageRepos: %v", err)
	}
	svc := &Service{repos: repos}
	for _, target := range m.Targets {
		got := svc.PackageRepos(target.ID)
		if len(got) == 0 {
			t.Errorf("target %q: no repos offered", target.ID)
			continue
		}
		defaults := 0
		for _, r := range got {
			if r.EnabledByDefault {
				defaults++
			}
		}
		if defaults != 1 {
			t.Errorf("target %q: %d enabledByDefault repos, want exactly 1", target.ID, defaults)
		}
	}
}

func TestPackageReposOSFilter(t *testing.T) {
	svc := reposService(t, `
repos:
  - {id: ubuntu-base, displayName: Ubuntu, url: "http://u", enabledByDefault: true, os: [ubuntu24]}
  - {id: debian-base, displayName: Debian, url: "http://d", enabledByDefault: true, os: [debian13]}
  - {id: everywhere, displayName: Everywhere, url: "http://e"}
`)
	cases := []struct {
		name, osID string
		want       []string
	}{
		// No OS list means "offered everywhere", so it shows up under every target.
		{"ubuntu24", "ubuntu24", []string{"everywhere", "ubuntu-base"}},
		{"debian13", "debian13", []string{"debian-base", "everywhere"}},
		// An empty query is the pre-selection case: show the whole catalog.
		{"unfiltered", "", []string{"debian-base", "everywhere", "ubuntu-base"}},
		// An unknown target still matches the unscoped repo, and nothing else.
		{"unknown os", "no-such-os", []string{"everywhere"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := svc.PackageRepos(c.osID)
			var ids []string
			for _, r := range got {
				ids = append(ids, r.ID)
			}
			if strings.Join(ids, ",") != strings.Join(c.want, ",") {
				t.Errorf("PackageRepos(%q) = %v, want %v", c.osID, ids, c.want)
			}
		})
	}
}

// Unset priorities are reported as the default rather than 0, so callers never
// have to special-case it, and ordering is by descending priority then id.
func TestPackageReposPriorityDefaultAndOrder(t *testing.T) {
	svc := reposService(t, `
repos:
  - {id: zeta, displayName: Zeta, url: "http://z"}
  - {id: alpha, displayName: Alpha, url: "http://a"}
  - {id: high, displayName: High, url: "http://h", priority: 2000}
  - {id: mid, displayName: Mid, url: "http://m", priority: 1000}
`)
	got := svc.PackageRepos("")
	want := []string{"high", "mid", "alpha", "zeta"} // 2000, 1000, then 500-ties by id
	var ids []string
	for _, r := range got {
		ids = append(ids, r.ID)
	}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", ids, want)
	}
	for _, r := range got {
		if r.Priority == 0 {
			t.Errorf("repo %q: priority reported as 0, want the %d default", r.ID, defaultRepoPriority)
		}
	}
	if got[len(got)-1].Priority != defaultRepoPriority {
		t.Errorf("unset priority = %d, want %d", got[len(got)-1].Priority, defaultRepoPriority)
	}
}

// PackageRepos must not mutate the loaded catalog when it fills in the default
// priority — a second call has to see the same values as the first.
func TestPackageReposDoesNotMutateCatalog(t *testing.T) {
	svc := reposService(t, `
repos:
  - {id: unset, displayName: Unset, url: "http://u"}
`)
	if got := svc.PackageRepos("")[0].Priority; got != defaultRepoPriority {
		t.Fatalf("first call priority = %d, want %d", got, defaultRepoPriority)
	}
	if svc.repos[0].Priority != 0 {
		t.Errorf("catalog mutated: stored priority = %d, want 0 (unset)", svc.repos[0].Priority)
	}
	if got := svc.PackageRepos("")[0].Priority; got != defaultRepoPriority {
		t.Errorf("second call priority = %d, want %d", got, defaultRepoPriority)
	}
}

// A catalog the UI couldn't render (or send back) fails at load, naming the
// offending entry, rather than surfacing as blank rows at request time.
func TestLoadPackageReposRejectsInvalid(t *testing.T) {
	cases := []struct {
		name, catalog, wantErr string
	}{
		{
			"missing id",
			"repos:\n  - {displayName: X, url: \"http://x\"}\n",
			"missing id",
		},
		{
			"missing displayName",
			"repos:\n  - {id: x, url: \"http://x\"}\n",
			"missing displayName",
		},
		{
			"missing url",
			"repos:\n  - {id: x, displayName: X}\n",
			"missing url",
		},
		{
			"duplicate id",
			"repos:\n  - {id: x, displayName: X, url: \"http://x\"}\n  - {id: x, displayName: Y, url: \"http://y\"}\n",
			"duplicate id",
		},
		{
			"malformed yaml",
			"repos: [oops\n",
			"parsing package repos",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := loadPackageRepos(writeRepos(t, c.catalog))
			if err == nil {
				t.Fatalf("loadPackageRepos accepted %s", c.name)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("error = %q, want it to mention %q", err, c.wantErr)
			}
		})
	}
}

func TestLoadPackageReposMissingFile(t *testing.T) {
	_, err := loadPackageRepos(filepath.Join(t.TempDir(), "absent.yaml"))
	if err == nil {
		t.Fatal("loadPackageRepos accepted a nonexistent path")
	}
	if !strings.Contains(err.Error(), "reading package repos") {
		t.Errorf("error = %q, want it to mention reading package repos", err)
	}
}

// A bad catalog must fail construction, not boot a server that 500s on the
// Advanced tab.
func TestNewRejectsInvalidPackageRepos(t *testing.T) {
	_, err := New(Config{
		TemplatesDir:     t.TempDir(),
		WorkDir:          t.TempDir(),
		PackageReposPath: writeRepos(t, "repos:\n  - {id: x, displayName: X}\n"),
	})
	if err == nil {
		t.Fatal("New accepted an invalid package-repos catalog")
	}
	if !strings.Contains(err.Error(), "missing url") {
		t.Errorf("error = %q, want it to name the invalid field", err)
	}
}

// An empty catalog is legal (an operator can ship one with no repos): the picker
// is simply empty, and the result must still be a non-nil slice.
func TestPackageReposEmptyCatalog(t *testing.T) {
	svc := reposService(t, "repos: []\n")
	got := svc.PackageRepos("ubuntu24")
	if got == nil {
		t.Fatal("PackageRepos returned nil, want an empty slice")
	}
	if len(got) != 0 {
		t.Errorf("got %d repos, want 0", len(got))
	}
}
