// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

// templateWithPackages is a curated parent that already lists packages, so the
// additive merge and the pin-collision reporting have something to collide with.
const templateWithPackages = `image:
  name: test-image
  version: "1.0"
target:
  os: ubuntu
  dist: ubuntu24
  arch: x86_64
  imageType: raw
systemConfig:
  packages:
    - curl
    - vim
`

// newPackageTestService is newTestService with a curated template that carries
// packages and with the shipped repository catalog loaded, since mapping repo
// ids to packageRepositories entries needs real catalog metadata.
func newPackageTestService(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"robotics.yml", "retail.yml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(templateWithPackages), 0o644); err != nil {
			t.Fatalf("writing template %s: %v", name, err)
		}
	}
	repos, err := loadPackageRepos("")
	if err != nil {
		t.Fatalf("loadPackageRepos: %v", err)
	}
	return &Service{
		cfg:      Config{TemplatesDir: dir, ICTBinary: "/bin/true", WorkDir: t.TempDir()},
		manifest: testManifest(),
		repos:    repos,
		tracker:  newBuildTracker(),
	}
}

func ubuntu24Selection() Selection {
	return Selection{Vertical: "robotics", SKU: "amr", Platform: "wcl", OS: "ubuntu24", ImageType: "iso"}
}

// --- ValidatePackages ---

func TestValidatePackages(t *testing.T) {
	t.Parallel()
	longName := strings.Repeat("a", maxPackageEntryLen+1)
	tests := []struct {
		name     string
		packages []string
		wantErr  bool
	}{
		{"empty is valid", nil, false},
		{"plain names", []string{"curl", "build-essential", "python3"}, false},
		// The pin form the resolvers match, with and without an epoch.
		{"pinned version", []string{"curl_8.5.0-2ubuntu10.13"}, false},
		{"pinned with epoch", []string{"vim_2:9.1.0016-1ubuntu7.8"}, false},
		{"pinned with tilde", []string{"librealsense2_2.56.5-0~realsense.17055"}, false},
		// Underscores are legal in a name of their own right.
		{"underscore in name", []string{"createrepo_c"}, false},
		// The glob forms the schema documents.
		{"glob", []string{"linux-image-*-oem", "libva-?[0-9]"}, false},
		{"empty entry", []string{"curl", ""}, true},
		{"leading dash", []string{"-curl"}, true},
		{"space", []string{"curl vim"}, true},
		{"shell metacharacter", []string{"curl;rm -rf /"}, true},
		{"path traversal", []string{"../etc/passwd"}, true},
		{"duplicate", []string{"curl", "vim", "curl"}, true},
		{"over-long entry", []string{longName}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidatePackages(tt.packages)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePackages(%q) error = %v, wantErr %v", tt.packages, err, tt.wantErr)
			}
		})
	}
}

func TestValidatePackagesRejectsOverCap(t *testing.T) {
	t.Parallel()
	pkgs := make([]string, maxPackages+1)
	for i := range pkgs {
		// Distinct names, so the failure is the cap and not the duplicate check.
		pkgs[i] = "pkg" + strings.Repeat("a", i%3) + string(rune('0'+i%10)) + "x" + itoa(i)
	}
	if err := ValidatePackages(pkgs); err == nil {
		t.Errorf("ValidatePackages accepted %d packages, want the cap of %d to reject", len(pkgs), maxPackages)
	}
	if err := ValidatePackages(pkgs[:maxPackages]); err != nil {
		t.Errorf("ValidatePackages(%d packages) = %v, want nil at exactly the cap", maxPackages, err)
	}
}

// itoa avoids pulling strconv in for one call site in a table.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// --- packageName / isPinned ---

// packageName decides whether an underscore separates a version or is part of
// the name. Getting it wrong would report a bogus pin collision for a package
// whose name legitimately contains an underscore.
func TestPackageName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		entry      string
		wantName   string
		wantPinned bool
	}{
		{"curl", "curl", false},
		{"curl_8.5.0-2ubuntu10.13", "curl", true},
		{"vim_2:9.1.0016-1ubuntu7.8", "vim", true},
		{"intel-oneapi-runtime-compilers_2025.3.3-30", "intel-oneapi-runtime-compilers", true},
		// A suffix that isn't a version is part of the name: createrepo_c is one
		// package, not createrepo pinned to version "c".
		{"createrepo_c", "createrepo_c", false},
		{"lib_foo", "lib_foo", false},
		// A trailing underscore names nothing after it.
		{"curl_", "curl_", false},
		// A leading underscore can't be a separator; the name part would be empty.
		{"_curl", "_curl", false},
	}
	for _, tt := range tests {
		t.Run(tt.entry, func(t *testing.T) {
			t.Parallel()
			if got := packageName(tt.entry); got != tt.wantName {
				t.Errorf("packageName(%q) = %q, want %q", tt.entry, got, tt.wantName)
			}
			if got := isPinned(tt.entry); got != tt.wantPinned {
				t.Errorf("isPinned(%q) = %v, want %v", tt.entry, got, tt.wantPinned)
			}
		})
	}
}

// --- delta emission ---

// The delta must carry the selected packages, sorted so the same selection
// always renders the same bytes regardless of click order.
func TestBuildDeltaEmitsPackagesSorted(t *testing.T) {
	t.Parallel()
	img := config.ImageInfo{Name: "test-image", Version: "1.0"}
	tgt := config.TargetInfo{OS: "ubuntu", Dist: "ubuntu24", Arch: "x86_64", ImageType: "raw"}

	clickOrder := Selection{Packages: []string{"htop", "curl", "build-essential"}}
	otherOrder := Selection{Packages: []string{"build-essential", "htop", "curl"}}

	a, err := buildDelta("robotics.yml", img, tgt, clickOrder, nil)
	if err != nil {
		t.Fatalf("buildDelta: %v", err)
	}
	b, err := buildDelta("robotics.yml", img, tgt, otherOrder, nil)
	if err != nil {
		t.Fatalf("buildDelta: %v", err)
	}
	if string(a) != string(b) {
		t.Errorf("delta depends on click order:\n--- a ---\n%s\n--- b ---\n%s", a, b)
	}
	for _, want := range []string{"systemConfig:", "packages:", "build-essential", "curl", "htop"} {
		if !strings.Contains(string(a), want) {
			t.Errorf("delta missing %q:\n%s", want, a)
		}
	}
}

// An empty selection must not emit empty systemConfig/packageRepositories
// blocks: the schema sets additionalProperties:false on systemConfig and an
// empty block is noise the Review pane would show as a change.
func TestBuildDeltaOmitsEmptyBlocks(t *testing.T) {
	t.Parallel()
	img := config.ImageInfo{Name: "test-image", Version: "1.0"}
	tgt := config.TargetInfo{OS: "ubuntu", Dist: "ubuntu24", Arch: "x86_64", ImageType: "raw"}

	data, err := buildDelta("robotics.yml", img, tgt, Selection{ImageName: "renamed"}, nil)
	if err != nil {
		t.Fatalf("buildDelta: %v", err)
	}
	for _, unwanted := range []string{"systemConfig", "packageRepositories", "packages"} {
		if strings.Contains(string(data), unwanted) {
			t.Errorf("delta emits %q for a selection with no packages or repos:\n%s", unwanted, data)
		}
	}
}

func TestBuildDeltaEmitsPackageRepositories(t *testing.T) {
	t.Parallel()
	img := config.ImageInfo{Name: "test-image", Version: "1.0"}
	tgt := config.TargetInfo{OS: "ubuntu", Dist: "ubuntu24", Arch: "x86_64", ImageType: "raw"}
	repos := []config.PackageRepository{{
		Codename: "noble", URL: "https://download.docker.com/linux/ubuntu",
		Component: "stable", PKey: "https://download.docker.com/linux/ubuntu/gpg",
	}}

	data, err := buildDelta("robotics.yml", img, tgt, Selection{Packages: []string{"docker-ce"}}, repos)
	if err != nil {
		t.Fatalf("buildDelta: %v", err)
	}
	for _, want := range []string{
		"packageRepositories:", "codename: noble",
		"https://download.docker.com/linux/ubuntu", "component: stable",
		"pkey: https://download.docker.com/linux/ubuntu/gpg",
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("delta missing %q:\n%s", want, data)
		}
	}
}

// --- toTemplateRepos ---

func TestToTemplateRepos(t *testing.T) {
	s := newPackageTestService(t)

	got := s.toTemplateRepos("ubuntu24", []string{
		"docker-ce-ubuntu",    // optional, default priority
		"intel-linux-overlay", // optional, raised priority
		"ubuntu-noble-base",   // base repo: comes from the providerconfigs
		"no-such-repo",        // unknown id from a stale client
	})
	if len(got) != 2 {
		t.Fatalf("got %d repos, want 2 (base and unknown ids dropped): %+v", len(got), got)
	}

	byURL := make(map[string]config.PackageRepository, len(got))
	for _, r := range got {
		byURL[r.URL] = r
	}

	docker, ok := byURL["https://download.docker.com/linux/ubuntu"]
	if !ok {
		t.Fatalf("docker-ce-ubuntu missing from %+v", got)
	}
	if docker.Codename != "noble" || docker.Component != "stable" {
		t.Errorf("docker repo = %+v, want codename noble / component stable", docker)
	}
	if docker.PKey == "" {
		t.Errorf("docker repo has no pkey: %+v", docker)
	}
	// The catalog default is what a build applies anyway, so emitting it would
	// state the default as though it were a choice.
	if docker.Priority != 0 {
		t.Errorf("docker repo priority = %d, want 0 (default not emitted)", docker.Priority)
	}

	overlay, ok := byURL["https://download.01.org/intel-linux-overlay/ubuntu"]
	if !ok {
		t.Fatalf("intel-linux-overlay missing from %+v", got)
	}
	if overlay.Priority != 2000 {
		t.Errorf("overlay repo priority = %d, want 2000 (raised priority is emitted)", overlay.Priority)
	}
}

func TestToTemplateReposEmptySelection(t *testing.T) {
	s := newPackageTestService(t)
	if got := s.toTemplateRepos("ubuntu24", nil); got != nil {
		t.Errorf("toTemplateRepos(nil) = %+v, want nil", got)
	}
}

// A repo whose index override URL differs from the repo URL must use the index's
// — that is the host actually serving that suite.
func TestTemplateRepoUsesIndexURLOverride(t *testing.T) {
	t.Parallel()
	r := PackageRepo{
		ID: "example", URL: "https://example.test/base", PKey: "https://example.test/key.gpg",
		Index: []RepoIndex{{URL: "https://other.test/repo", Codename: "noble", Component: "main"}},
	}
	got, ok := r.templateRepo()
	if !ok {
		t.Fatal("templateRepo reported not-ok for a repo with an index")
	}
	if got.URL != "https://other.test/repo" {
		t.Errorf("URL = %q, want the index override", got.URL)
	}
}

func TestTemplateRepoWithoutIndexIsSkipped(t *testing.T) {
	t.Parallel()
	r := PackageRepo{ID: "example", URL: "https://example.test/base"}
	if _, ok := r.templateRepo(); ok {
		t.Error("templateRepo accepted a repo with no index; a deb build has no suite to read")
	}
}

// An rpm repo is addressed by base URL alone, so it needs no codename from the
// index — but it still gets one, because the build path uses it as the repo's
// display name and dnf id.
func TestTemplateRepoRPMNeedsNoCodename(t *testing.T) {
	t.Parallel()
	r := PackageRepo{
		ID: "azl-base", URL: "https://packages.microsoft.com/azurelinux/{arch}", Type: repoTypeRPM,
		Index: []RepoIndex{{}},
	}
	got, ok := r.templateRepo()
	if !ok {
		t.Fatal("templateRepo reported not-ok for an rpm repo")
	}
	if got.Codename != "azl-base" {
		t.Errorf("Codename = %q, want the repo id", got.Codename)
	}
}

// Every optional repo the catalog ships must carry a signing key, so enabling
// one never silently produces an unverified build. A new repo added without one
// fails here rather than at build time.
func TestEmbeddedCatalogOptionalReposHaveSigningKeys(t *testing.T) {
	t.Parallel()
	repos, err := loadPackageRepos("")
	if err != nil {
		t.Fatalf("loadPackageRepos: %v", err)
	}
	for _, r := range repos {
		// Base repos are configured by the providerconfigs, never by a delta.
		if r.EnabledByDefault {
			continue
		}
		if !r.HasSigningKey() {
			t.Errorf("optional repo %q has no pkey; enabling it would emit an unverified packageRepositories entry", r.ID)
		}
	}
}

// --- Compose with packages ---

func TestComposeWithPackages(t *testing.T) {
	s := newPackageTestService(t)

	sel := ubuntu24Selection()
	sel.Packages = []string{"htop", "build-essential"}

	res, err := s.Compose(sel)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	// The point of the whole change: the picks reach the resolved template.
	for _, want := range []string{"htop", "build-essential"} {
		if !strings.Contains(res.YAML, want) {
			t.Errorf("resolved YAML missing selected package %q:\n%s", want, res.YAML)
		}
	}
	// The merge is additive, so the parent's own packages must survive.
	for _, want := range []string{"curl", "vim"} {
		if !strings.Contains(res.YAML, want) {
			t.Errorf("resolved YAML dropped the parent's package %q:\n%s", want, res.YAML)
		}
	}
	if res.Summary.PackageCount != 4 {
		t.Errorf("summary.PackageCount = %d, want 4 (2 parent + 2 selected)", res.Summary.PackageCount)
	}
	if res.DeltaYAML == "" {
		t.Error("DeltaYAML is empty for a selection with packages")
	}
	if res.BaseYAML == "" {
		t.Error("BaseYAML is empty for a selection with packages")
	}
	if strings.Contains(res.BaseYAML, "htop") {
		t.Errorf("BaseYAML carries a selected package; it must be the parent alone:\n%s", res.BaseYAML)
	}

	assertNoLeakedDeltas(t, s.cfg.TemplatesDir)
}

// A pinned version is emitted verbatim, so the resolver's name_version matching
// sees exactly the string the user chose.
func TestComposeWithPinnedPackage(t *testing.T) {
	s := newPackageTestService(t)

	sel := ubuntu24Selection()
	sel.Packages = []string{"htop_3.3.0-4build1"}

	res, err := s.Compose(sel)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if !strings.Contains(res.YAML, "htop_3.3.0-4build1") {
		t.Errorf("resolved YAML missing the pinned version:\n%s", res.YAML)
	}
	if len(res.PinConflicts) != 0 {
		t.Errorf("PinConflicts = %v, want none (htop is not in the parent)", res.PinConflicts)
	}

	assertNoLeakedDeltas(t, s.cfg.TemplatesDir)
}

// Pinning a package the parent already lists unpinned leaves both entries in the
// resolved template, because the merge unions rather than overrides. That is
// reported, not silently fixed.
func TestComposePinConflictReported(t *testing.T) {
	s := newPackageTestService(t)

	sel := ubuntu24Selection()
	sel.Packages = []string{"curl_8.5.0-2ubuntu10.13", "htop"}

	res, err := s.Compose(sel)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if len(res.PinConflicts) != 1 || res.PinConflicts[0] != "curl_8.5.0-2ubuntu10.13" {
		t.Errorf("PinConflicts = %v, want [curl_8.5.0-2ubuntu10.13]", res.PinConflicts)
	}
	// Both entries really are present — the report describes reality.
	if !strings.Contains(res.YAML, "- curl\n") || !strings.Contains(res.YAML, "curl_8.5.0-2ubuntu10.13") {
		t.Errorf("expected both the parent's curl and the pinned one:\n%s", res.YAML)
	}

	assertNoLeakedDeltas(t, s.cfg.TemplatesDir)
}

// An unpinned pick that duplicates a parent entry is not a conflict: it dedups
// on the exact string.
func TestComposeUnpinnedDuplicateIsNotAConflict(t *testing.T) {
	s := newPackageTestService(t)

	sel := ubuntu24Selection()
	sel.Packages = []string{"curl"}

	res, err := s.Compose(sel)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if len(res.PinConflicts) != 0 {
		t.Errorf("PinConflicts = %v, want none for an unpinned duplicate", res.PinConflicts)
	}
	if res.Summary.PackageCount != 2 {
		t.Errorf("summary.PackageCount = %d, want 2 (curl deduped against the parent)", res.Summary.PackageCount)
	}
}

// With no overrides at all there is no delta, so neither the delta nor the
// baseline view is published — the resolved YAML already is the base.
func TestComposeWithoutOverridesPublishesNoDeltaOrBase(t *testing.T) {
	s := newPackageTestService(t)

	res, err := s.Compose(ubuntu24Selection())
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if res.DeltaYAML != "" {
		t.Errorf("DeltaYAML = %q, want empty with no overrides", res.DeltaYAML)
	}
	if res.BaseYAML != "" {
		t.Errorf("BaseYAML = %q, want empty with no overrides", res.BaseYAML)
	}
	if len(res.PinConflicts) != 0 {
		t.Errorf("PinConflicts = %v, want none with no overrides", res.PinConflicts)
	}
}

// Enabling a repo alone (no packages, no image name) is still an override, so a
// delta must be generated and carry the repository.
func TestComposeWithReposOnly(t *testing.T) {
	s := newPackageTestService(t)

	sel := ubuntu24Selection()
	sel.Repos = []string{"docker-ce-ubuntu"}

	res, err := s.Compose(sel)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if res.DeltaYAML == "" {
		t.Fatal("DeltaYAML is empty for a repos-only selection")
	}
	if !strings.Contains(res.DeltaYAML, "download.docker.com") {
		t.Errorf("delta missing the enabled repository:\n%s", res.DeltaYAML)
	}
	if !strings.Contains(res.YAML, "download.docker.com") {
		t.Errorf("resolved YAML missing the enabled repository:\n%s", res.YAML)
	}

	assertNoLeakedDeltas(t, s.cfg.TemplatesDir)
}

func TestComposeInvalidPackageRejected(t *testing.T) {
	s := newPackageTestService(t)

	sel := ubuntu24Selection()
	sel.Packages = []string{"curl; rm -rf /"}

	_, err := s.Compose(sel)
	code, _ := assertServiceError(t, err, http.StatusBadRequest)
	if code != "BAD_REQUEST" {
		t.Errorf("code = %q, want BAD_REQUEST", code)
	}

	assertNoLeakedDeltas(t, s.cfg.TemplatesDir)
}

// --- build path ---

// Packages alone must trigger delta generation on the build path too. Before
// hasOverrides this gate tested ImageName only, so a package-only selection
// would have built the curated template and silently dropped the picks.
func TestResolveBuildTemplateWithPackagesOnly(t *testing.T) {
	s := newPackageTestService(t)

	sel := ubuntu24Selection()
	sel.Packages = []string{"htop"}
	workDir := t.TempDir()

	path, name, err := s.resolveBuildTemplate(&BuildRequest{Compose: &sel}, workDir)
	if err != nil {
		t.Fatalf("resolveBuildTemplate: %v", err)
	}
	// The display name stays the curated parent's, as it does for imageName.
	if name != "robotics.yml" {
		t.Errorf("name = %q, want robotics.yml", name)
	}
	if filepath.Base(path) == "robotics.yml" {
		t.Fatalf("path = %q, want a generated delta rather than the curated parent", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading delta: %v", err)
	}
	if !strings.Contains(string(data), "htop") {
		t.Errorf("build delta missing the selected package:\n%s", data)
	}
	_ = os.Remove(path)
}

func TestResolveBuildTemplateInvalidPackage(t *testing.T) {
	s := newPackageTestService(t)

	sel := ubuntu24Selection()
	sel.Packages = []string{"../etc/passwd"}

	_, _, err := s.resolveBuildTemplate(&BuildRequest{Compose: &sel}, t.TempDir())
	if err == nil {
		t.Fatal("resolveBuildTemplate accepted an invalid package name")
	}

	assertNoLeakedDeltas(t, s.cfg.TemplatesDir)
}
