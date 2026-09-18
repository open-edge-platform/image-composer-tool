// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
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

// reposService builds a Service around a caller-supplied catalog and the target
// ids that count as known, bypassing the build/template config the repo
// endpoints don't touch. The manifest matters here: PackageRepos rejects any
// osID the manifest doesn't offer.
func reposService(t *testing.T, catalog string, targets ...string) *Service {
	t.Helper()
	repos, err := loadPackageRepos(writeRepos(t, catalog))
	if err != nil {
		t.Fatalf("loadPackageRepos: %v", err)
	}
	m := &Manifest{}
	for _, id := range targets {
		m.Targets = append(m.Targets, Target{ID: id, DisplayName: id})
	}
	return &Service{manifest: m, repos: repos}
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
	svc := &Service{manifest: m, repos: repos}
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

// repoRef is one (url, codename, component) triple, normalised so a template and
// the catalog can be compared regardless of trailing slashes, component order,
// or an omitted component.
type repoRef struct {
	url, codename, component string
}

// normalizeRepoRef makes a triple comparable. An omitted component becomes
// "main" because that is what generateAptSourcesContent substitutes
// (internal/config/apt_sources.go), so an index reading "main" is reading
// exactly what a build would install from. Components are sorted because they
// are independent path segments, not an ordered list.
func normalizeRepoRef(url, codename, component string) repoRef {
	component = strings.TrimSpace(component)
	if component == "" {
		component = "main"
	}
	parts := strings.Fields(component)
	sort.Strings(parts)
	return repoRef{
		url:       strings.TrimSuffix(strings.TrimSpace(url), "/"),
		codename:  strings.TrimSpace(codename),
		component: strings.Join(parts, " "),
	}
}

// catalogRefs is every triple the shipped catalog can search, keyed by the
// manifest target it is offered for.
func catalogRefs(t *testing.T, repos []PackageRepo) map[string]map[repoRef]string {
	t.Helper()
	byTarget := make(map[string]map[repoRef]string)
	for _, r := range repos {
		for _, idx := range r.Index {
			url := idx.URL
			if url == "" {
				url = r.URL
			}
			ref := normalizeRepoRef(url, idx.Codename, idx.Component)
			for _, osID := range r.OS {
				if byTarget[osID] == nil {
					byTarget[osID] = make(map[repoRef]string)
				}
				byTarget[osID][ref] = r.ID
			}
		}
	}
	return byTarget
}

// templateRepos parses a template's own packageRepositories. None of the
// manifest's templates use `extends`, so reading the file directly is the whole
// picture and avoids dragging the config loader (and its OS-defaults lookup)
// into a unit test.
func templateRepos(t *testing.T, path string) []repoRef {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read template %s: %v", path, err)
	}
	var doc struct {
		PackageRepositories []struct {
			URL       string `json:"url"`
			Codename  string `json:"codename"`
			Component string `json:"component"`
		} `json:"packageRepositories"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse template %s: %v", path, err)
	}
	var out []repoRef
	for _, r := range doc.PackageRepositories {
		// "<URL>" is the fill-me-in placeholder for a private repo; it is not a
		// real repository and must never be mirrored into the catalog.
		if r.URL == "" || strings.Contains(r.URL, "<URL>") {
			continue
		}
		out = append(out, normalizeRepoRef(r.URL, r.Codename, r.Component))
	}
	return out
}

// Drift guard: every repository a manifest-reachable template installs from must
// be searchable in the catalog. Without this, adding a repo to a template
// silently produces a package picker that cannot find that repo's packages —
// the failure is invisible until someone goes looking for a package.
func TestEmbeddedCatalogCoversTemplateRepos(t *testing.T) {
	m, err := loadManifest("")
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	repos, err := loadPackageRepos("")
	if err != nil {
		t.Fatalf("loadPackageRepos: %v", err)
	}
	byTarget := catalogRefs(t, repos)

	// Tests run in the package dir; the templates live at the repo root.
	root := filepath.Join("..", "..", "..", "image-templates")
	for _, c := range m.Combinations {
		if strings.TrimSpace(c.Template) == "" {
			continue
		}
		for _, ref := range templateRepos(t, filepath.Join(root, c.Template)) {
			if _, ok := byTarget[c.OS][ref]; !ok {
				t.Errorf("template %s (target %s) installs from %+v, which no catalog index covers.\n"+
					"Add an index entry for it in data/package-repos.yaml.", c.Template, c.OS, ref)
			}
		}
	}
}

// The base repos must mirror the per-OS providerconfigs, since those are the
// repositories every build of that target reads regardless of template.
func TestEmbeddedCatalogCoversProviderRepos(t *testing.T) {
	repos, err := loadPackageRepos("")
	if err != nil {
		t.Fatalf("loadPackageRepos: %v", err)
	}
	byTarget := catalogRefs(t, repos)

	// Only the manifest's own targets are catalogued, so only their
	// providerconfigs are checked. See the note in data/package-repos.yaml.
	cases := []struct{ osID, dir string }{
		{"ubuntu24", filepath.Join("ubuntu", "ubuntu24")},
		{"ubuntu26-server", filepath.Join("ubuntu", "ubuntu26")},
		{"debian13", filepath.Join("debian", "debian13")},
	}
	for _, c := range cases {
		t.Run(c.osID, func(t *testing.T) {
			pattern := filepath.Join("..", "..", "..", "config", "osv", c.dir, "providerconfigs", "*_repo.yml")
			files, err := filepath.Glob(pattern)
			if err != nil || len(files) == 0 {
				t.Fatalf("no providerconfigs matched %s (err %v)", pattern, err)
			}
			for _, f := range files {
				raw, err := os.ReadFile(f)
				if err != nil {
					t.Fatalf("read %s: %v", f, err)
				}
				var doc struct {
					Repositories []struct {
						Name      string `json:"name"`
						BaseURL   string `json:"baseURL"`
						Component string `json:"component"`
					} `json:"repositories"`
				}
				if err := yaml.Unmarshal(raw, &doc); err != nil {
					t.Fatalf("parse %s: %v", f, err)
				}
				for _, r := range doc.Repositories {
					// The default repo.yml omits component for some targets; the
					// arch-specific files carry the authoritative value.
					if r.BaseURL == "" || r.Component == "" {
						continue
					}
					ref := normalizeRepoRef(r.BaseURL, r.Name, r.Component)
					if _, ok := byTarget[c.osID][ref]; !ok {
						t.Errorf("%s declares %+v, which no catalog index covers", f, ref)
					}
				}
			}
		})
	}
}

func TestPackageReposOSFilter(t *testing.T) {
	svc := reposService(t, `
repos:
  - {id: ubuntu-base, displayName: Ubuntu, url: "http://u", enabledByDefault: true, os: [ubuntu24]}
  - {id: debian-base, displayName: Debian, url: "http://d", enabledByDefault: true, os: [debian13]}
  - {id: everywhere, displayName: Everywhere, url: "http://e"}
`, "ubuntu24", "debian13")
	cases := []struct {
		name, osID string
		want       []string
	}{
		// No OS list means "offered everywhere", so it shows up under every target.
		{"ubuntu24", "ubuntu24", []string{"everywhere", "ubuntu-base"}},
		{"debian13", "debian13", []string{"debian-base", "everywhere"}},
		// An empty query is the pre-selection case: show the whole catalog.
		{"unfiltered", "", []string{"debian-base", "everywhere", "ubuntu-base"}},
		// An unknown target offers nothing at all — not even the unscoped repo,
		// which applies to every *known* target rather than to any string.
		{"unknown os", "no-such-os", nil},
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

// An unknown target must stay empty even when the catalog carries a repo that
// applies everywhere. Without the manifest check this passes only by accident of
// the shipped data — adding one global repo would silently start returning
// results for targets that don't exist.
func TestPackageReposUnknownTargetIgnoresGlobalRepos(t *testing.T) {
	svc := reposService(t, `
repos:
  - {id: everywhere, displayName: Everywhere, url: "http://e"}
`, "ubuntu24")
	if got := svc.PackageRepos("ubuntu24"); len(got) != 1 {
		t.Fatalf("known target got %d repos, want the global one", len(got))
	}
	got := svc.PackageRepos("no-such-os")
	if len(got) != 0 {
		t.Errorf("unknown target got %d repos, want 0", len(got))
	}
	if got == nil {
		t.Error("unknown target returned nil, want an empty slice (serializes as [])")
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
`, "ubuntu24")
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
`, "ubuntu24")
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
		{
			"placeholder url",
			"repos:\n  - {id: x, displayName: X, url: \"<URL>\"}\n",
			"url is a placeholder",
		},
		{
			"unknown type",
			"repos:\n  - {id: x, displayName: X, url: \"http://x\", type: snap}\n",
			"unknown type",
		},
		{
			"deb index without codename",
			"repos:\n  - {id: x, displayName: X, url: \"http://x\", index: [{component: main}]}\n",
			"missing codename",
		},
		{
			"placeholder index url",
			"repos:\n  - {id: x, displayName: X, url: \"http://x\", index: [{url: \"<URL>\", codename: noble}]}\n",
			"index 0: url is a placeholder",
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

// The codename requirement is deb-specific: an rpm repository is addressed by
// its repodata path, so requiring a suite there would reject a valid entry.
func TestLoadPackageReposRPMIndexNeedsNoCodename(t *testing.T) {
	repos, err := loadPackageRepos(writeRepos(t, `
repos:
  - id: emt-base
    displayName: EMT Base
    url: "https://example.invalid/rpms/3.0/base"
    type: rpm
    index:
      - {component: emt3.0-base}
`))
	if err != nil {
		t.Fatalf("loadPackageRepos rejected a valid rpm catalog: %v", err)
	}
	if len(repos) != 1 || len(repos[0].Index) != 1 {
		t.Fatalf("repos = %+v, want one repo with one index", repos)
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
	svc := reposService(t, "repos: []\n", "ubuntu24")
	got := svc.PackageRepos("ubuntu24")
	if got == nil {
		t.Fatal("PackageRepos returned nil, want an empty slice")
	}
	if len(got) != 0 {
		t.Errorf("got %d repos, want 0", len(got))
	}
}
