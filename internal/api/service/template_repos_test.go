// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/api/service/pkgindex"
	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

// A vendor snapshot and a PPA the NVL BKC template declares; neither is in the
// catalog.
var (
	snapshotRepo = config.PackageRepository{
		URL:       "https://af01p-png.devtools.intel.com/artifactory/hspe-edge-png-local/ubuntu/resolute/resolute/20261023-0019/",
		Codename:  "resolute",
		Component: "main non-free multimedia internal kernels",
		Priority:  1001,
		PKey:      "https://af01p-png.devtools.intel.com/artifactory/hspe-edge-png-local/ubuntu/keys/adl-hirsute-public.gpg",
	}
	ppaRepo = config.PackageRepository{URL: "https://ppa.launchpadcontent.net/ubuntu-virt/lp2168017/ubuntu", Codename: "resolute"}
)

func catalogService(t *testing.T) *Service {
	t.Helper()
	repos, err := loadPackageRepos("")
	if err != nil {
		t.Fatalf("loadPackageRepos: %v", err)
	}
	m, err := loadManifest("")
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	return &Service{manifest: m, repos: repos}
}

func TestTemplateOnlyRepos(t *testing.T) {
	t.Parallel()
	s := catalogService(t)
	lyrical := config.PackageRepository{URL: "http://packages.ros.org/ros2/ubuntu", Codename: "resolute", Component: "main"}

	got := s.templateOnlyRepos("ubuntu26", []config.PackageRepository{
		snapshotRepo, lyrical, ppaRepo, snapshotRepo,
		{URL: "<URL>", Codename: "resolute"},
		{URL: "https://example.com/no-suite"},
	})
	if len(got) != 2 {
		t.Fatalf("got %d repos, want the snapshot and the PPA only: %+v", len(got), got)
	}
	snap := got[0]
	if !strings.HasPrefix(snap.ID, templateOnlyRepoPrefix) {
		t.Errorf("ID = %q, want the %q prefix", snap.ID, templateOnlyRepoPrefix)
	}
	if snap.DisplayName != "af01p-png.devtools.intel.com …/resolute/20261023-0019 (resolute)" {
		t.Errorf("DisplayName = %q", snap.DisplayName)
	}
	wantIdx := RepoIndex{Codename: "resolute", Component: "internal kernels main multimedia non-free"}
	if snap.Type != repoTypeDeb || len(snap.Index) != 1 || snap.Index[0].Codename != wantIdx.Codename ||
		snap.Index[0].Component != wantIdx.Component {
		t.Errorf("Type/Index = %q %+v, want deb %+v", snap.Type, snap.Index, wantIdx)
	}
	if snap.Priority != 1001 || snap.PKey != snapshotRepo.PKey || !slices.Equal(snap.OS, []string{"ubuntu26"}) {
		t.Errorf("Priority/PKey/OS = %d %q %v", snap.Priority, snap.PKey, snap.OS)
	}
	if got[1].Priority != defaultRepoPriority {
		t.Errorf("PPA priority = %d, want the default %d", got[1].Priority, defaultRepoPriority)
	}

	// The same repository gets the same id every time, and another per target.
	if again := s.templateOnlyRepos("ubuntu26", []config.PackageRepository{snapshotRepo}); again[0].ID != snap.ID {
		t.Errorf("id changed between composes: %q then %q", snap.ID, again[0].ID)
	}
	if other := s.templateOnlyRepos("ubuntu26-server", []config.PackageRepository{snapshotRepo}); other[0].ID == snap.ID {
		t.Errorf("same id %q for another target", snap.ID)
	}
}

func TestTemplateOnlyRepoRPM(t *testing.T) {
	t.Parallel()
	r, ok := templateOnlyRepo("azl3", repoTypeRPM, config.PackageRepository{URL: "https://example.com/rpm/base/"})
	if !ok || r.Type != repoTypeRPM || len(r.Index) != 1 || r.Index[0].Codename != "" || r.Index[0].Component != "" {
		t.Errorf("templateOnlyRepo = %+v, %v; want an rpm repo read from its URL", r, ok)
	}
	if r.URL != "https://example.com/rpm/base" || r.DisplayName != "example.com/rpm/base" {
		t.Errorf("URL/DisplayName = %q %q", r.URL, r.DisplayName)
	}
}

// A registered repository is searchable by its id, for its own target only, and
// never part of the catalog GET /package-repos lists.
func TestSearchableReposIncludesRegisteredTemplateRepos(t *testing.T) {
	t.Parallel()
	s := catalogService(t)
	id := s.templateOnlyRepos("ubuntu26", []config.PackageRepository{ppaRepo})[0].ID

	if got := s.searchableRepos("ubuntu26", []string{id, "ubuntu-resolute-base"}); len(got) != 2 {
		t.Errorf("searchableRepos by id = %d repos, want the PPA and the base repo", len(got))
	}
	if got := s.searchableRepos("ubuntu24", []string{id}); len(got) != 0 {
		t.Errorf("searchableRepos on another target = %+v, want none", got)
	}
	for _, r := range s.searchableRepos("ubuntu26", nil) {
		if r.ID == id {
			t.Errorf("an unfiltered search includes %s; only one naming it should", id)
		}
	}
	for _, r := range s.PackageRepos("ubuntu26") {
		if r.ID == id {
			t.Errorf("PackageRepos lists %s", id)
		}
	}
}

func TestTemplateRepoLabel(t *testing.T) {
	t.Parallel()
	tests := []struct{ url, codename, want string }{
		{"https://ppa.launchpadcontent.net/ubuntu-virt/lp2168017/ubuntu", "resolute", "ppa.launchpadcontent.net …/lp2168017/ubuntu (resolute)"},
		{"https://repo.example.com/apt", "stable", "repo.example.com/apt (stable)"},
		{"https://repo.example.com", "", "repo.example.com"},
		{"not a url", "x", "not a url (x)"},
	}
	for _, tc := range tests {
		if got := templateRepoLabel(tc.url, tc.codename); got != tc.want {
			t.Errorf("templateRepoLabel(%q, %q) = %q, want %q", tc.url, tc.codename, got, tc.want)
		}
	}
}

// Compose reports a template repository the catalog doesn't carry under its
// registered id, with the entry to show for it.
func TestComposeTemplateOnlyRepos(t *testing.T) {
	t.Parallel()
	s := newTestService(t)
	repos, err := loadPackageRepos("")
	if err != nil {
		t.Fatalf("loadPackageRepos: %v", err)
	}
	s.repos = repos
	body := minimalTemplate + `packageRepositories:
  - codename: noble
    url: http://packages.ros.org/ros2/ubuntu
    pkey: https://raw.githubusercontent.com/ros/rosdistro/master/ros.key
    component: main
  - codename: noble
    url: https://ppa.launchpadcontent.net/example/tools/ubuntu
    pkey: https://keyserver.ubuntu.com/pks/lookup?op=get&search=0x1234
`
	if err := os.WriteFile(filepath.Join(s.cfg.TemplatesDir, "robotics.yml"), []byte(body), 0o644); err != nil {
		t.Fatalf("writing template: %v", err)
	}
	res, err := s.Compose(Selection{Vertical: "robotics", SKU: "amr", Platform: "wcl", OS: "ubuntu24", ImageType: "iso"})
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if len(res.TemplateOnlyRepos) != 1 {
		t.Fatalf("TemplateOnlyRepos = %+v, want the PPA", res.TemplateOnlyRepos)
	}
	ppa := res.TemplateOnlyRepos[0]
	if !slices.Contains(res.TemplateRepos, ppa.ID) || !slices.Contains(res.TemplateRepos, "ros2-jazzy") {
		t.Errorf("TemplateRepos = %v, want ros2-jazzy and %s", res.TemplateRepos, ppa.ID)
	}
	if got := s.searchableRepos("ubuntu24", []string{ppa.ID}); len(got) != 1 {
		t.Errorf("the composed PPA isn't searchable by its id")
	}
}

// A template's allowPackages carries over to its registered repository, is
// part of its identity, and narrows what a search of it returns.
func TestTemplateOnlyRepoAllowPackages(t *testing.T) {
	t.Parallel()
	s := catalogService(t)
	restricted := ppaRepo
	restricted.AllowPackages = []string{"qemu-system-x86", "libvirt*"}

	got := s.templateOnlyRepos("ubuntu26", []config.PackageRepository{restricted})
	if len(got) != 1 || !slices.Equal(got[0].AllowPackages, []string{"libvirt*", "qemu-system-x86"}) {
		t.Fatalf("AllowPackages = %+v, want the template's filter, sorted", got)
	}
	if open := s.templateOnlyRepos("ubuntu26", []config.PackageRepository{ppaRepo}); open[0].ID == got[0].ID {
		t.Errorf("same id %q with and without allowPackages", got[0].ID)
	}

	lookups := s.planLookups("ubuntu26", got)
	if len(lookups) == 0 || !slices.Equal(lookups[0].allow, got[0].AllowPackages) {
		t.Fatalf("lookups don't carry the filter: %+v", lookups)
	}
	entries := []pkgindex.Entry{{Name: "qemu-system-x86"}, {Name: "libvirt0"}, {Name: "libvirt-daemon"}, {Name: "curl"}}
	var names []string
	for _, e := range lookups[0].allowed(entries) {
		names = append(names, e.Name)
	}
	if !slices.Equal(names, []string{"qemu-system-x86", "libvirt0", "libvirt-daemon"}) {
		t.Errorf("allowed = %v, want curl filtered out", names)
	}
	if all := (repoLookup{}).allowed(entries); len(all) != len(entries) {
		t.Errorf("an unrestricted lookup dropped entries: %v", all)
	}
}
