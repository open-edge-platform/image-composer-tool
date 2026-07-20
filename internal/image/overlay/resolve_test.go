package overlay

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
)

// fakeBackend is a resolverBackend stub: it records the request it received and
// returns a canned closure/artifact set, so the deterministic orchestration in
// ResolveOverlayPackages can be exercised for both families without network/root.
type fakeBackend struct {
	fam      PackageManager
	closure  []ospackage.PackageInfo
	arts     []string
	err      error
	gotReq   resolveRequest
	gotCalls int
}

func (b *fakeBackend) family() PackageManager { return b.fam }

func (b *fakeBackend) resolveAndDownload(req resolveRequest) ([]ospackage.PackageInfo, []string, error) {
	b.gotCalls++
	b.gotReq = req
	if b.err != nil {
		return nil, nil, b.err
	}
	return b.closure, b.arts, nil
}

// withStubbedResolution swaps the backend selector and provider-repo loader for
// the duration of fn, restoring them afterward.
func withStubbedResolution(t *testing.T, backend resolverBackend, provider []config.ProviderRepoConfig, provErr error, fn func()) {
	t.Helper()
	origBackend := selectResolverBackend
	origLoader := loadProviderRepoConfig
	origClear := clearOverlayCacheDir
	defer func() {
		selectResolverBackend = origBackend
		loadProviderRepoConfig = origLoader
		clearOverlayCacheDir = origClear
	}()
	selectResolverBackend = func(PackageManager) (resolverBackend, error) { return backend, nil }
	loadProviderRepoConfig = func(_, _, _ string) ([]config.ProviderRepoConfig, error) {
		return provider, provErr
	}
	// Neutralize the on-disk cache purge in orchestration tests; the real behavior
	// is covered directly by TestPurgeOverlayArtifacts.
	clearOverlayCacheDir = func(string) error { return nil }
	fn()
}

func debProviderRepo() config.ProviderRepoConfig {
	return config.ProviderRepoConfig{
		Name:      "elxr-main",
		Type:      "deb",
		BaseURL:   "https://repo.example.com/elxr",
		Component: "main",
		Enabled:   true,
	}
}

func rpmProviderRepo() config.ProviderRepoConfig {
	return config.ProviderRepoConfig{
		Name:      "azl-base",
		Type:      "rpm",
		BaseURL:   "https://repo.example.com/azl/{arch}",
		Component: "base",
		Enabled:   true,
		GPGCheck:  true,
	}
}

func TestResolveOverlayPackages_DebFamily(t *testing.T) {
	backend := &fakeBackend{
		fam: PackageManagerAPT,
		closure: []ospackage.PackageInfo{
			{Name: "curl_8.deb", PkgName: "curl", Version: "8", Arch: "amd64", URL: "https://r/curl_8.deb"},
			{Name: "libc6_2.deb", PkgName: "libc6", Version: "2.34", Arch: "amd64", URL: "https://r/libc6.deb"},
		},
		arts: []string{"curl_8.deb", "libc6_2.deb"},
	}
	template := &config.ImageTemplate{
		Target: config.TargetInfo{OS: "wind-river-elxr", Dist: "elxr12", Arch: "amd64"},
		SystemConfig: config.SystemConfig{
			// "libc6" is already in the baseline; only "curl" should be seeded.
			Packages: []string{"curl", "libc6", "curl"},
		},
	}
	info := &BaselineInfo{OS: "wind-river-elxr", Arch: "amd64", PackageManager: PackageManagerAPT, PackageType: pkgTypeDeb}
	baseline := []BaselinePackage{
		{Name: "libc6", Version: "2.34", Arch: "amd64", Installed: true, Provides: []string{"libc"}},
		{Name: "old-pkg", Installed: false}, // config-files remnant: ignored
	}

	var plan *ResolutionPlan
	withStubbedResolution(t, backend, []config.ProviderRepoConfig{debProviderRepo()}, nil, func() {
		var err error
		plan, err = ResolveOverlayPackages(template, info, baseline)
		if err != nil {
			t.Fatalf("ResolveOverlayPackages: %v", err)
		}
	})

	if backend.gotCalls != 1 {
		t.Fatalf("backend called %d times, want 1", backend.gotCalls)
	}
	if !reflect.DeepEqual(backend.gotReq.seed, []string{"curl"}) {
		t.Errorf("seed = %v, want [curl] (libc6 already present)", backend.gotReq.seed)
	}
	if !reflect.DeepEqual(plan.Requested, []string{"curl", "libc6"}) {
		t.Errorf("requested = %v, want [curl libc6]", plan.Requested)
	}
	if len(plan.Repositories) != 1 || plan.Repositories[0].Type != "deb" {
		t.Errorf("repositories = %+v, want one deb repo", plan.Repositories)
	}
	// libc6 is in the closure but already present → only curl must be installed.
	if len(plan.ToInstall) != 1 || plan.ToInstall[0].Name != "curl" {
		t.Errorf("toInstall = %+v, want only curl", plan.ToInstall)
	}
	if !reflect.DeepEqual(plan.AlreadyPresent, []string{"libc6"}) {
		t.Errorf("alreadyPresent = %v, want [libc6]", plan.AlreadyPresent)
	}
	// Closure and artifacts are sorted deterministically.
	if !reflect.DeepEqual(plan.Artifacts, []string{"curl_8.deb", "libc6_2.deb"}) {
		t.Errorf("artifacts = %v", plan.Artifacts)
	}
	if plan.Closure[0].Name != "curl" || plan.Closure[1].Name != "libc6" {
		t.Errorf("closure not sorted by name: %+v", plan.Closure)
	}
	if !strings.HasSuffix(plan.DownloadDir, "overlay") {
		t.Errorf("downloadDir = %q, want overlay-suffixed cache path", plan.DownloadDir)
	}
}

// TestResolveOverlayPackages_UpgradeMode confirms that under an
// additive-and-upgrade policy a package already present in the baseline is
// re-resolved and, when the resolved version is strictly newer, routed into
// ToInstall as an upgrade rather than dropped as already-present. A present
// package whose resolved version is not newer stays already-present.
func TestResolveOverlayPackages_UpgradeMode(t *testing.T) {
	backend := &fakeBackend{
		fam: PackageManagerAPT,
		closure: []ospackage.PackageInfo{
			// curl: baseline 8.5.0-2ubuntu10.9, repo 8.5.0-2ubuntu10.10 → upgrade.
			{PkgName: "curl", Version: "8.5.0-2ubuntu10.10", Arch: "amd64", URL: "https://r/curl_new.deb"},
			// bash: present at the same version the repo offers → no-op, stays present.
			{PkgName: "bash", Version: "5.2-1", Arch: "amd64", URL: "https://r/bash.deb"},
			// tree: genuinely new → plain add.
			{PkgName: "tree", Version: "2.1.1-1", Arch: "amd64", URL: "https://r/tree.deb"},
		},
		arts: []string{"curl_new.deb", "bash.deb", "tree.deb"},
	}
	template := &config.ImageTemplate{
		Target: config.TargetInfo{OS: "ubuntu", Dist: "ubuntu24", Arch: "amd64"},
		SystemConfig: config.SystemConfig{
			Packages: []string{"curl", "bash", "tree"},
		},
		OverlayPolicy: &config.OverlayPolicy{
			PackageOperation: config.OverlayPackageOpAdditiveAndUpgrade,
			AllowUpgrade:     true, // set by validate() in the real load path
		},
	}
	info := &BaselineInfo{OS: "ubuntu", Arch: "amd64", PackageManager: PackageManagerAPT, PackageType: pkgTypeDeb}
	baseline := []BaselinePackage{
		{Name: "curl", Version: "8.5.0-2ubuntu10.9", Arch: "amd64", Installed: true},
		{Name: "bash", Version: "5.2-1", Arch: "amd64", Installed: true},
	}

	var plan *ResolutionPlan
	withStubbedResolution(t, backend, []config.ProviderRepoConfig{debProviderRepo()}, nil, func() {
		var err error
		plan, err = ResolveOverlayPackages(template, info, baseline)
		if err != nil {
			t.Fatalf("ResolveOverlayPackages: %v", err)
		}
	})

	// Upgrade mode seeds every requested package, including the already-present ones.
	if !reflect.DeepEqual(backend.gotReq.seed, []string{"bash", "curl", "tree"}) {
		t.Errorf("seed = %v, want [bash curl tree] (present packages re-resolved in upgrade mode)", backend.gotReq.seed)
	}
	// curl (upgrade) + tree (add) install; bash (same version) does not.
	gotInstall := make([]string, 0, len(plan.ToInstall))
	for _, p := range plan.ToInstall {
		gotInstall = append(gotInstall, p.Name)
	}
	if !reflect.DeepEqual(gotInstall, []string{"curl", "tree"}) {
		t.Errorf("toInstall = %v, want [curl tree]", gotInstall)
	}
	// bash is present at the resolved version → left untouched.
	if !reflect.DeepEqual(plan.AlreadyPresent, []string{"bash"}) {
		t.Errorf("alreadyPresent = %v, want [bash]", plan.AlreadyPresent)
	}
	for _, p := range plan.ToInstall {
		if p.Name == "curl" && p.Version != "8.5.0-2ubuntu10.10" {
			t.Errorf("curl upgrade version = %q, want 8.5.0-2ubuntu10.10", p.Version)
		}
	}
}

// TestResolveOverlayPackages_UpgradeScopeExcludesUnrequestedTransitive confirms
// upgrade scope is bounded: a present baseline library that appears in the
// closure only as a transitive dependency and is merely newer in the repo (no
// versioned requirement forcing it) is NOT upgraded — only the requested package
// is. This guards against opting into one upgrade silently bumping core libs.
func TestResolveOverlayPackages_UpgradeScopeExcludesUnrequestedTransitive(t *testing.T) {
	backend := &fakeBackend{
		fam: PackageManagerAPT,
		closure: []ospackage.PackageInfo{
			// curl is requested and present-older → eligible upgrade. Its Depends on
			// libssl3 carries NO version constraint, so libssl3 must not be upgraded
			// merely because the repo has a newer one.
			{PkgName: "curl", Version: "8.6", Arch: "amd64", URL: "https://r/curl_86.deb", RequiresVer: []string{"libssl3"}},
			// libssl3 is present-older and newer in the repo, but only a transitive,
			// unversioned dep → must stay on the baseline version.
			{PkgName: "libssl3", Version: "3.2", Arch: "amd64", URL: "https://r/libssl3_32.deb"},
		},
		arts: []string{"curl_86.deb", "libssl3_32.deb"},
	}
	template := &config.ImageTemplate{
		Target:       config.TargetInfo{OS: "ubuntu", Dist: "ubuntu24", Arch: "amd64"},
		SystemConfig: config.SystemConfig{Packages: []string{"curl"}},
		OverlayPolicy: &config.OverlayPolicy{
			PackageOperation: config.OverlayPackageOpAdditiveAndUpgrade,
			AllowUpgrade:     true,
		},
	}
	info := &BaselineInfo{OS: "ubuntu", Arch: "amd64", PackageManager: PackageManagerAPT, PackageType: pkgTypeDeb}
	baseline := []BaselinePackage{
		{Name: "curl", Version: "8.5", Arch: "amd64", Installed: true},
		{Name: "libssl3", Version: "3.1", Arch: "amd64", Installed: true},
	}

	var plan *ResolutionPlan
	withStubbedResolution(t, backend, []config.ProviderRepoConfig{debProviderRepo()}, nil, func() {
		var err error
		plan, err = ResolveOverlayPackages(template, info, baseline)
		if err != nil {
			t.Fatalf("ResolveOverlayPackages: %v", err)
		}
	})

	gotInstall := make([]string, 0, len(plan.ToInstall))
	for _, p := range plan.ToInstall {
		gotInstall = append(gotInstall, p.Name)
	}
	if !reflect.DeepEqual(gotInstall, []string{"curl"}) {
		t.Errorf("toInstall = %v, want [curl] (libssl3 is only an unversioned transitive dep)", gotInstall)
	}
	if !reflect.DeepEqual(plan.AlreadyPresent, []string{"libssl3"}) {
		t.Errorf("alreadyPresent = %v, want [libssl3]", plan.AlreadyPresent)
	}
}

// TestResolveOverlayPackages_UpgradeScopeIncludesRequiredDep confirms a present
// baseline dependency IS upgraded when a to-install package requires a newer
// version than the baseline carries (a versioned pin the baseline fails but the
// resolved copy satisfies) — the case additive-only could never resolve.
func TestResolveOverlayPackages_UpgradeScopeIncludesRequiredDep(t *testing.T) {
	backend := &fakeBackend{
		fam: PackageManagerAPT,
		closure: []ospackage.PackageInfo{
			// app is new and requires libfoo (>= 2.0); baseline libfoo is 1.0.
			{PkgName: "app", Version: "1.0", Arch: "amd64", URL: "https://r/app.deb", RequiresVer: []string{"libfoo (>= 2.0)"}},
			// libfoo present-older 1.0, repo 2.0 satisfies the pin → required upgrade.
			{PkgName: "libfoo", Version: "2.0", Arch: "amd64", URL: "https://r/libfoo_20.deb"},
		},
		arts: []string{"app.deb", "libfoo_20.deb"},
	}
	template := &config.ImageTemplate{
		Target:       config.TargetInfo{OS: "ubuntu", Dist: "ubuntu24", Arch: "amd64"},
		SystemConfig: config.SystemConfig{Packages: []string{"app"}},
		OverlayPolicy: &config.OverlayPolicy{
			PackageOperation: config.OverlayPackageOpAdditiveAndUpgrade,
			AllowUpgrade:     true,
		},
	}
	info := &BaselineInfo{OS: "ubuntu", Arch: "amd64", PackageManager: PackageManagerAPT, PackageType: pkgTypeDeb}
	baseline := []BaselinePackage{
		{Name: "libfoo", Version: "1.0", Arch: "amd64", Installed: true},
	}

	var plan *ResolutionPlan
	withStubbedResolution(t, backend, []config.ProviderRepoConfig{debProviderRepo()}, nil, func() {
		var err error
		plan, err = ResolveOverlayPackages(template, info, baseline)
		if err != nil {
			t.Fatalf("ResolveOverlayPackages: %v", err)
		}
	})

	gotInstall := make([]string, 0, len(plan.ToInstall))
	for _, p := range plan.ToInstall {
		gotInstall = append(gotInstall, p.Name)
	}
	if !reflect.DeepEqual(gotInstall, []string{"app", "libfoo"}) {
		t.Errorf("toInstall = %v, want [app libfoo] (libfoo required at >= 2.0, baseline has 1.0)", gotInstall)
	}
}

// TestResolveOverlayPackages_AdditiveOnlyLeavesPresentUntouched confirms the
// default additive-only policy still prunes a present package from the seed and
// never upgrades it, even when the repo offers a newer version.
func TestResolveOverlayPackages_AdditiveOnlyLeavesPresentUntouched(t *testing.T) {
	backend := &fakeBackend{
		fam: PackageManagerAPT,
		closure: []ospackage.PackageInfo{
			{PkgName: "tree", Version: "2.1.1-1", Arch: "amd64", URL: "https://r/tree.deb"},
		},
		arts: []string{"tree.deb"},
	}
	template := &config.ImageTemplate{
		Target: config.TargetInfo{OS: "ubuntu", Dist: "ubuntu24", Arch: "amd64"},
		// No OverlayPolicy → additive-only default.
		SystemConfig: config.SystemConfig{Packages: []string{"curl", "tree"}},
	}
	info := &BaselineInfo{OS: "ubuntu", Arch: "amd64", PackageManager: PackageManagerAPT, PackageType: pkgTypeDeb}
	baseline := []BaselinePackage{
		{Name: "curl", Version: "8.5.0-2ubuntu10.9", Arch: "amd64", Installed: true},
	}

	var plan *ResolutionPlan
	withStubbedResolution(t, backend, []config.ProviderRepoConfig{debProviderRepo()}, nil, func() {
		var err error
		plan, err = ResolveOverlayPackages(template, info, baseline)
		if err != nil {
			t.Fatalf("ResolveOverlayPackages: %v", err)
		}
	})

	// curl is pruned from the seed (already present); only tree is resolved.
	if !reflect.DeepEqual(backend.gotReq.seed, []string{"tree"}) {
		t.Errorf("seed = %v, want [tree] (curl pruned under additive-only)", backend.gotReq.seed)
	}
	if len(plan.ToInstall) != 1 || plan.ToInstall[0].Name != "tree" {
		t.Errorf("toInstall = %+v, want only tree", plan.ToInstall)
	}
	if !reflect.DeepEqual(plan.AlreadyPresent, []string{"curl"}) {
		t.Errorf("alreadyPresent = %v, want [curl]", plan.AlreadyPresent)
	}
}

func TestResolveOverlayPackages_RPMFamily(t *testing.T) {
	backend := &fakeBackend{
		fam: PackageManagerDNF,
		closure: []ospackage.PackageInfo{
			{PkgName: "vim", Version: "9.0", Arch: "x86_64", URL: "https://r/vim.rpm"},
			{PkgName: "glibc", Version: "2.38", Arch: "x86_64", URL: "https://r/glibc.rpm"},
		},
		arts: []string{"vim.rpm", "glibc.rpm"},
	}
	template := &config.ImageTemplate{
		Target:       config.TargetInfo{OS: "azure-linux", Dist: "azl3", Arch: "x86_64"},
		SystemConfig: config.SystemConfig{Packages: []string{"vim"}},
	}
	info := &BaselineInfo{OS: "azure-linux", Arch: "x86_64", PackageManager: PackageManagerDNF, PackageType: pkgTypeRPM}
	baseline := []BaselinePackage{
		{Name: "glibc", Version: "2.38", Arch: "x86_64", Installed: true},
	}

	var plan *ResolutionPlan
	withStubbedResolution(t, backend, []config.ProviderRepoConfig{rpmProviderRepo()}, nil, func() {
		var err error
		plan, err = ResolveOverlayPackages(template, info, baseline)
		if err != nil {
			t.Fatalf("ResolveOverlayPackages: %v", err)
		}
	})

	if !reflect.DeepEqual(backend.gotReq.seed, []string{"vim"}) {
		t.Errorf("seed = %v, want [vim]", backend.gotReq.seed)
	}
	if len(plan.Repositories) != 1 || plan.Repositories[0].Type != "rpm" {
		t.Errorf("repositories = %+v, want one rpm repo", plan.Repositories)
	}
	// {arch} placeholder must be substituted in the resolved repo URL.
	if got := plan.Repositories[0].URL; got != "https://repo.example.com/azl/x86_64" {
		t.Errorf("repo URL = %q, want arch-substituted", got)
	}
	if len(plan.ToInstall) != 1 || plan.ToInstall[0].Name != "vim" {
		t.Errorf("toInstall = %+v, want only vim", plan.ToInstall)
	}
	if !reflect.DeepEqual(plan.AlreadyPresent, []string{"glibc"}) {
		t.Errorf("alreadyPresent = %v, want [glibc]", plan.AlreadyPresent)
	}
}

func TestResolveOverlayPackages_NoSeedSkipsBackend(t *testing.T) {
	backend := &fakeBackend{fam: PackageManagerAPT}
	template := &config.ImageTemplate{
		Target:       config.TargetInfo{OS: "ubuntu", Dist: "ubuntu24", Arch: "amd64"},
		SystemConfig: config.SystemConfig{Packages: []string{"bash", "coreutils"}},
	}
	info := &BaselineInfo{OS: "ubuntu", Arch: "amd64", PackageManager: PackageManagerAPT}
	baseline := []BaselinePackage{
		{Name: "bash", Installed: true},
		{Name: "coreutils", Installed: true},
	}

	var plan *ResolutionPlan
	withStubbedResolution(t, backend, []config.ProviderRepoConfig{debProviderRepo()}, nil, func() {
		var err error
		plan, err = ResolveOverlayPackages(template, info, baseline)
		if err != nil {
			t.Fatalf("ResolveOverlayPackages: %v", err)
		}
	})

	if backend.gotCalls != 0 {
		t.Errorf("backend should not be called when nothing needs resolving, got %d calls", backend.gotCalls)
	}
	if len(plan.Seed) != 0 || len(plan.ToInstall) != 0 {
		t.Errorf("expected empty seed/toInstall, got seed=%v toInstall=%v", plan.Seed, plan.ToInstall)
	}
	if !reflect.DeepEqual(plan.AlreadyPresent, []string{"bash", "coreutils"}) {
		t.Errorf("alreadyPresent = %v", plan.AlreadyPresent)
	}
}

func TestResolveOverlayPackages_BackendErrorIsDiagnostic(t *testing.T) {
	backend := &fakeBackend{fam: PackageManagerAPT, err: errors.New("metadata 404 for curl")}
	template := &config.ImageTemplate{
		Target:       config.TargetInfo{OS: "ubuntu", Dist: "ubuntu24", Arch: "amd64"},
		SystemConfig: config.SystemConfig{Packages: []string{"curl"}},
	}
	info := &BaselineInfo{OS: "ubuntu", Arch: "amd64", PackageManager: PackageManagerAPT}

	withStubbedResolution(t, backend, []config.ProviderRepoConfig{debProviderRepo()}, nil, func() {
		_, err := ResolveOverlayPackages(template, info, nil)
		if err == nil {
			t.Fatal("expected resolution error")
		}
		// Diagnostic must name the package(s), the family, and the repository.
		for _, want := range []string{"curl", "apt", "elxr-main", "metadata 404"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q missing %q", err, want)
			}
		}
	})
}

func TestResolveOverlayPackages_NoRepositoriesFails(t *testing.T) {
	backend := &fakeBackend{fam: PackageManagerAPT}
	template := &config.ImageTemplate{
		Target:       config.TargetInfo{OS: "ubuntu", Dist: "ubuntu24", Arch: "amd64"},
		SystemConfig: config.SystemConfig{Packages: []string{"curl"}},
	}
	info := &BaselineInfo{OS: "ubuntu", Arch: "amd64", PackageManager: PackageManagerAPT}

	// Provider loader returns an rpm repo only — wrong family for an apt baseline,
	// and no template repos — so resolution must fail with a clear message.
	withStubbedResolution(t, backend, []config.ProviderRepoConfig{rpmProviderRepo()}, nil, func() {
		_, err := ResolveOverlayPackages(template, info, nil)
		if err == nil || !strings.Contains(err.Error(), "no apt repositories") {
			t.Fatalf("error = %v, want no-repositories failure", err)
		}
	})
}

func TestResolveOverlayPackages_UnsupportedFamily(t *testing.T) {
	template := &config.ImageTemplate{Target: config.TargetInfo{OS: "x", Dist: "y", Arch: "z"}}
	info := &BaselineInfo{OS: "x", Arch: "z", PackageManager: PackageManager("zypper")}
	if _, err := ResolveOverlayPackages(template, info, nil); err == nil {
		t.Fatal("expected unsupported-family error")
	}
}

func TestBuildRepositorySet_FiltersAndOrders(t *testing.T) {
	provider := []config.ProviderRepoConfig{
		{Name: "deb-a", Type: "deb", BaseURL: "https://a", Component: "main", Enabled: true},
		{Name: "rpm-x", Type: "rpm", BaseURL: "https://x", Enabled: true},         // wrong family
		{Name: "deb-disabled", Type: "deb", BaseURL: "https://d", Enabled: false}, // disabled
	}
	userRepos := []config.PackageRepository{
		{Codename: "user1", URL: "https://u1", Component: "main", Priority: 900},
		{Codename: "placeholder", URL: "<URL>"}, // skipped
		{Codename: "local", Path: "/srv/repo"},  // no URL: skipped
	}

	repos := buildRepositorySet(provider, userRepos, PackageManagerAPT, "amd64")
	if len(repos) != 2 {
		t.Fatalf("got %d repos, want 2 (one provider deb + one user), repos=%+v", len(repos), repos)
	}
	// Highest priority first: the user repo (900) outranks the provider repo (500).
	if repos[0].Name != "user1" || repos[0].Source != "template" {
		t.Errorf("repos[0] = %+v, want user1/template first", repos[0])
	}
	if repos[1].Name != "deb-a" || repos[1].Source != "provider" {
		t.Errorf("repos[1] = %+v, want deb-a/provider", repos[1])
	}
	for _, r := range repos {
		if r.Type != "deb" {
			t.Errorf("non-deb repo leaked into apt set: %+v", r)
		}
	}
}

func TestBuildRepositorySet_DedupesSameURL(t *testing.T) {
	provider := []config.ProviderRepoConfig{
		{Name: "noble", Type: "deb", BaseURL: "https://dup", Component: "main", Enabled: true},
	}
	userRepos := []config.PackageRepository{
		{Codename: "noble", URL: "https://dup", Component: "main"}, // same suite+url+component
	}
	repos := buildRepositorySet(provider, userRepos, PackageManagerAPT, "amd64")
	if len(repos) != 1 {
		t.Fatalf("got %d repos, want 1 after dedup; repos=%+v", len(repos), repos)
	}
}

// TestBuildRepositorySet_KeepsDistinctDebSuites guards against the dedup key
// collapsing distinct deb suites that share the same base URL and component. For
// Debian-family providers, "noble", "noble-updates", and "noble-security" all
// point at the same archive URL with the same component but are separate metadata
// sources; dropping any of them would silently discard the updates/security repos.
func TestBuildRepositorySet_KeepsDistinctDebSuites(t *testing.T) {
	provider := []config.ProviderRepoConfig{
		{Name: "noble", Type: "deb", BaseURL: "http://archive.ubuntu.com/ubuntu", Component: "main", Enabled: true},
		{Name: "noble-updates", Type: "deb", BaseURL: "http://archive.ubuntu.com/ubuntu", Component: "main", Enabled: true},
		{Name: "noble-security", Type: "deb", BaseURL: "http://archive.ubuntu.com/ubuntu", Component: "main", Enabled: true},
	}
	repos := buildRepositorySet(provider, nil, PackageManagerAPT, "amd64")
	if len(repos) != 3 {
		t.Fatalf("got %d repos, want 3 distinct suites preserved; repos=%+v", len(repos), repos)
	}
	names := map[string]bool{}
	for _, r := range repos {
		names[r.Name] = true
	}
	for _, want := range []string{"noble", "noble-updates", "noble-security"} {
		if !names[want] {
			t.Errorf("suite %q missing from repository set; repos=%+v", want, repos)
		}
	}
}

func TestBuildRepositorySet_RPMArchSubstitution(t *testing.T) {
	provider := []config.ProviderRepoConfig{
		{Name: "azl", Type: "rpm", BaseURL: "https://r/{arch}/os", Enabled: true},
	}
	repos := buildRepositorySet(provider, nil, PackageManagerDNF, "aarch64")
	if len(repos) != 1 || repos[0].URL != "https://r/aarch64/os" {
		t.Fatalf("repos = %+v, want arch-substituted rpm URL", repos)
	}
}

func TestPackagingArch(t *testing.T) {
	tests := []struct {
		arch   string
		family PackageManager
		want   string
	}{
		// deb family: ELF spelling is translated to the Debian arch names.
		{"x86_64", PackageManagerAPT, "amd64"},
		{"aarch64", PackageManagerAPT, "arm64"},
		{"amd64", PackageManagerAPT, "amd64"}, // already-translated is left alone
		{"riscv64", PackageManagerAPT, "riscv64"},
		// rpm family (and anything else) keeps the ELF spelling unchanged.
		{"x86_64", PackageManagerDNF, "x86_64"},
		{"aarch64", PackageManagerDNF, "aarch64"},
	}
	for _, tt := range tests {
		if got := packagingArch(tt.arch, tt.family); got != tt.want {
			t.Errorf("packagingArch(%q, %q) = %q, want %q", tt.arch, tt.family, got, tt.want)
		}
	}
}

// TestOverlayCacheDir_UsesPackagingArch confirms the overlay cache namespace is
// built from the packaging arch passed in (the deb/rpm spelling) rather than an
// ELF-derived arch, so it lines up with the provider repo-config naming and the
// other GetProviderId-based cache keys.
func TestOverlayCacheDir_UsesPackagingArch(t *testing.T) {
	info := &BaselineInfo{OS: "ubuntu", Arch: "x86_64", PackageManager: PackageManagerAPT}
	dir, err := overlayCacheDir(info, "ubuntu24", "amd64")
	if err != nil {
		t.Fatalf("overlayCacheDir: %v", err)
	}
	if !strings.Contains(dir, filepath.Join("pkgCache", "ubuntu-ubuntu24-amd64", "overlay")) {
		t.Errorf("cache dir = %q, want it to contain pkgCache/ubuntu-ubuntu24-amd64/overlay", dir)
	}
	if strings.Contains(dir, "x86_64") {
		t.Errorf("cache dir = %q, must not embed the ELF arch spelling x86_64", dir)
	}
}

// TestOverlayCacheDir_RejectsPathTraversal confirms overlayCacheDir refuses os/dist/arch
// values that are not safe single path segments, so a programmatically built template
// cannot redirect writes/removals outside the cache root via a separator or "..".
func TestOverlayCacheDir_RejectsPathTraversal(t *testing.T) {
	cases := []struct {
		name           string
		os, dist, arch string
	}{
		{"dotdot dist", "ubuntu", "..", "amd64"},
		{"separator in os", "ubuntu/../../etc", "ubuntu24", "amd64"},
		{"separator in arch", "ubuntu", "ubuntu24", "amd64/../.."},
		{"empty dist", "ubuntu", "  ", "amd64"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info := &BaselineInfo{OS: tc.os, Arch: "x86_64", PackageManager: PackageManagerAPT}
			if _, err := overlayCacheDir(info, tc.dist, tc.arch); err == nil {
				t.Errorf("overlayCacheDir(%q, %q, %q) = nil error, want rejection", tc.os, tc.dist, tc.arch)
			}
		})
	}
}

// TestRPMResolveUserRepos_AppendsSecondariesAndDedupes confirms the rpm backend
// exposes secondary provider repositories to the pipeline (which otherwise only
// resolves against the single primary RepoCfg), keeps template repos verbatim and
// first, and does not fetch a repository present in both sets twice.
func TestRPMResolveUserRepos_AppendsSecondariesAndDedupes(t *testing.T) {
	userRepos := []config.PackageRepository{
		{ID: "tmpl1", Codename: "user", URL: "https://repo/user", PKeys: []string{"k1", "k2"}},
	}
	secondary := []Repository{
		{ID: "prov2", Name: "secondary", URL: "https://repo/secondary", GPGKey: "gk", Component: "os", Priority: 500, AllowPackages: []string{"vim"}},
		{ID: "prov3", Name: "dup", URL: "https://repo/user"}, // same URL as the template repo
		{ID: "prov4", Name: "blank", URL: "  "},              // empty after trim: skipped
	}
	got := rpmResolveUserRepos(secondary, userRepos)
	if len(got) != 2 {
		t.Fatalf("got %d repos, want 2 (template + one unique secondary): %+v", len(got), got)
	}
	// Template repo is preserved verbatim and listed first.
	if !reflect.DeepEqual(got[0], userRepos[0]) {
		t.Errorf("got[0] = %+v, want template repo verbatim %+v", got[0], userRepos[0])
	}
	// The unique secondary is mapped into a PackageRepository.
	if got[1].URL != "https://repo/secondary" || got[1].Codename != "secondary" ||
		got[1].PKey != "gk" || !reflect.DeepEqual(got[1].AllowPackages, []string{"vim"}) {
		t.Errorf("got[1] = %+v, want secondary repo mapped from Repository", got[1])
	}
}

func TestOverlayRequestedPackages_SortedDeduped(t *testing.T) {
	template := &config.ImageTemplate{
		SystemConfig: config.SystemConfig{Packages: []string{" vim ", "curl", "vim", "", "bash"}},
	}
	got := overlayRequestedPackages(template)
	want := []string{"bash", "curl", "vim"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("requested = %v, want %v", got, want)
	}
}

func TestBaselinePresenceSet(t *testing.T) {
	baseline := []BaselinePackage{
		{Name: "bash", Installed: true, Provides: []string{"sh"}},
		{Name: "halfinstalled", Installed: false, Provides: []string{"ignored"}},
	}
	present := baselinePresenceSet(baseline)
	if !present["bash"] || !present["sh"] {
		t.Errorf("expected bash and its provided sh to be present: %v", present)
	}
	if present["halfinstalled"] || present["ignored"] {
		t.Errorf("uninstalled package must not register as present: %v", present)
	}
}

// TestBasePackageName confirms an APT-style ":arch" qualifier is stripped so
// arch-qualified template requests ("gcc:amd64") reconcile against the resolver's
// canonical base names ("gcc"), while unqualified names are left untouched.
func TestBasePackageName(t *testing.T) {
	cases := map[string]string{
		"gcc":         "gcc",
		"gcc:amd64":   "gcc",
		"libc6:i386":  "libc6",
		"foo:bar:baz": "foo", // only the first qualifier boundary matters
		"":            "",
	}
	for in, want := range cases {
		if got := basePackageName(in); got != want {
			t.Errorf("basePackageName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestUpgradeEligibleNames_ArchQualifiedRequest confirms an APT-style arch-
// qualified template request ("gcc:amd64") is still recognized as requested-and-
// present, so its canonical closure entry ("gcc") — newer than the baseline — is
// marked eligible for upgrade rather than skipped on a multiarch qualifier
// mismatch.
func TestUpgradeEligibleNames_ArchQualifiedRequest(t *testing.T) {
	requested := []string{"gcc:amd64"}
	closure := []ospackage.PackageInfo{
		{PkgName: "gcc", Version: "4:13.2.0", Arch: "amd64"},
	}
	present := map[string]bool{"gcc": true}
	baselineByName := map[string]BaselinePackage{
		"gcc": {Name: "gcc", Version: "4:12.3.0", Arch: "amd64"},
	}

	eligible := upgradeEligibleNames(PackageManagerAPT, requested, closure, present, baselineByName)

	if !eligible["gcc"] {
		t.Errorf("arch-qualified request gcc:amd64 not upgrade-eligible: %v", eligible)
	}
}

func TestOverlaySeedPackages_PreservesOrder(t *testing.T) {
	requested := []string{"bash", "curl", "vim"}
	present := map[string]bool{"bash": true}
	got := overlaySeedPackages(requested, present)
	if !reflect.DeepEqual(got, []string{"curl", "vim"}) {
		t.Errorf("seed = %v, want [curl vim]", got)
	}
}

// TestOverlaySeedPackages_ArchQualifiedPresent confirms an APT arch-qualified
// request ("bash:amd64") for a package already present in the baseline (keyed by
// the canonical base name "bash") is recognized as satisfied and NOT seeded, while
// an arch-qualified request for an absent package keeps its original token in the
// seed slice so the resolver still receives the qualifier.
func TestOverlaySeedPackages_ArchQualifiedPresent(t *testing.T) {
	requested := []string{"bash:amd64", "curl:amd64"}
	present := map[string]bool{"bash": true}
	got := overlaySeedPackages(requested, present)
	if !reflect.DeepEqual(got, []string{"curl:amd64"}) {
		t.Errorf("seed = %v, want [curl:amd64]", got)
	}
}

// TestResolveOverlayPackages_ArchQualifiedRequestAlreadyPresent confirms that an
// APT arch-qualified template request ("libc6:amd64") for a package the baseline
// already has (keyed under the canonical base name "libc6") is not seeded and is
// recorded as already-present under the base name — rather than being missed on
// the ":arch" qualifier mismatch and mislabelled. Only the genuinely new package
// is installed.
func TestResolveOverlayPackages_ArchQualifiedRequestAlreadyPresent(t *testing.T) {
	backend := &fakeBackend{
		fam: PackageManagerAPT,
		closure: []ospackage.PackageInfo{
			{Name: "curl_8.deb", PkgName: "curl", Version: "8", Arch: "amd64", URL: "https://r/curl_8.deb"},
		},
		arts: []string{"curl_8.deb"},
	}
	template := &config.ImageTemplate{
		Target: config.TargetInfo{OS: "ubuntu", Dist: "ubuntu24", Arch: "amd64"},
		SystemConfig: config.SystemConfig{
			// libc6 is present in the baseline under its base name; the request is
			// arch-qualified, so only curl should be seeded/installed.
			Packages: []string{"curl", "libc6:amd64"},
		},
	}
	info := &BaselineInfo{OS: "ubuntu", Arch: "amd64", PackageManager: PackageManagerAPT, PackageType: pkgTypeDeb}
	baseline := []BaselinePackage{
		{Name: "libc6", Version: "2.34", Arch: "amd64", Installed: true},
	}

	var plan *ResolutionPlan
	withStubbedResolution(t, backend, []config.ProviderRepoConfig{debProviderRepo()}, nil, func() {
		var err error
		plan, err = ResolveOverlayPackages(template, info, baseline)
		if err != nil {
			t.Fatalf("ResolveOverlayPackages: %v", err)
		}
	})

	// The arch-qualified libc6:amd64 request is recognized as present → not seeded.
	if !reflect.DeepEqual(backend.gotReq.seed, []string{"curl"}) {
		t.Errorf("seed = %v, want [curl] (libc6:amd64 already present)", backend.gotReq.seed)
	}
	// Only curl installs; libc6 is left on the baseline copy.
	if len(plan.ToInstall) != 1 || plan.ToInstall[0].Name != "curl" {
		t.Errorf("toInstall = %+v, want only curl", plan.ToInstall)
	}
	// Recorded already-present under the canonical base name, not the raw token.
	if !reflect.DeepEqual(plan.AlreadyPresent, []string{"libc6"}) {
		t.Errorf("alreadyPresent = %v, want [libc6]", plan.AlreadyPresent)
	}
}

// TestResolveOverlayPackages_Deterministic confirms identical inputs yield
// byte-identical plans regardless of input ordering of the closure/artifacts.
func TestResolveOverlayPackages_Deterministic(t *testing.T) {
	template := &config.ImageTemplate{
		Target:       config.TargetInfo{OS: "ubuntu", Dist: "ubuntu24", Arch: "amd64"},
		SystemConfig: config.SystemConfig{Packages: []string{"curl"}},
	}
	info := &BaselineInfo{OS: "ubuntu", Arch: "amd64", PackageManager: PackageManagerAPT}

	run := func(closure []ospackage.PackageInfo, arts []string) *ResolutionPlan {
		backend := &fakeBackend{fam: PackageManagerAPT, closure: closure, arts: arts}
		var plan *ResolutionPlan
		withStubbedResolution(t, backend, []config.ProviderRepoConfig{debProviderRepo()}, nil, func() {
			var err error
			plan, err = ResolveOverlayPackages(template, info, nil)
			if err != nil {
				t.Fatalf("ResolveOverlayPackages: %v", err)
			}
		})
		return plan
	}

	a := run(
		[]ospackage.PackageInfo{{PkgName: "curl", Version: "8"}, {PkgName: "libc6", Version: "2"}},
		[]string{"curl_8.deb", "libc6.deb"},
	)
	b := run(
		[]ospackage.PackageInfo{{PkgName: "libc6", Version: "2"}, {PkgName: "curl", Version: "8"}},
		[]string{"libc6.deb", "curl_8.deb"},
	)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("plans differ for reordered inputs:\n a=%+v\n b=%+v", a, b)
	}
}

// TestPurgeOverlayArtifacts confirms the cache purge removes stale files (the
// scenario that made a `tree`-only template pull in systemd-boot: a superset of
// artifacts left by a prior larger build), leaves the directory in place, and
// tolerates a missing directory.
func TestPurgeOverlayArtifacts(t *testing.T) {
	dir := t.TempDir()
	stale := []string{
		"tree_2.1.1-2ubuntu3_amd64.deb",
		"systemd-boot_255.4-1ubuntu8.16_amd64.deb", // leftover from an earlier request
		"packages.json",                            // sibling metadata cache
	}
	for _, name := range stale {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("seeding %s: %v", name, err)
		}
	}

	if err := purgeOverlayArtifacts(dir); err != nil {
		t.Fatalf("purgeOverlayArtifacts: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("dir must still exist after purge: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty cache dir after purge, got %d file(s): %+v", len(entries), entries)
	}

	// A missing directory is a no-op, not an error.
	if err := purgeOverlayArtifacts(filepath.Join(dir, "does-not-exist")); err != nil {
		t.Errorf("purge of a missing directory must be a no-op, got %v", err)
	}
	// Empty path is also a no-op.
	if err := purgeOverlayArtifacts(""); err != nil {
		t.Errorf("purge of an empty path must be a no-op, got %v", err)
	}
}

// TestResolveOverlayPackages_PurgesCacheBeforeResolve confirms the resolver
// clears the download directory before resolving a non-empty seed, so a stale
// superset can never be reused as the closure.
func TestResolveOverlayPackages_PurgesCacheBeforeResolve(t *testing.T) {
	backend := &fakeBackend{
		fam:     PackageManagerAPT,
		closure: []ospackage.PackageInfo{{PkgName: "tree", Version: "2", Arch: "amd64", URL: "https://r/tree.deb"}},
		arts:    []string{"tree.deb"},
	}
	template := &config.ImageTemplate{
		Target:       config.TargetInfo{OS: "ubuntu", Dist: "ubuntu24", Arch: "amd64"},
		SystemConfig: config.SystemConfig{Packages: []string{"tree"}},
	}
	info := &BaselineInfo{OS: "ubuntu", Arch: "amd64", PackageManager: PackageManagerAPT}

	origBackend := selectResolverBackend
	origLoader := loadProviderRepoConfig
	origClear := clearOverlayCacheDir
	defer func() {
		selectResolverBackend = origBackend
		loadProviderRepoConfig = origLoader
		clearOverlayCacheDir = origClear
	}()
	selectResolverBackend = func(PackageManager) (resolverBackend, error) { return backend, nil }
	loadProviderRepoConfig = func(_, _, _ string) ([]config.ProviderRepoConfig, error) {
		return []config.ProviderRepoConfig{debProviderRepo()}, nil
	}

	var clearedDir string
	cleared := false
	clearOverlayCacheDir = func(dir string) error {
		clearedDir = dir
		cleared = true
		return nil
	}

	plan, err := ResolveOverlayPackages(template, info, nil)
	if err != nil {
		t.Fatalf("ResolveOverlayPackages: %v", err)
	}
	if !cleared {
		t.Fatal("expected the cache to be purged before resolving a non-empty seed")
	}
	if clearedDir != plan.DownloadDir {
		t.Errorf("purged %q, want the plan download dir %q", clearedDir, plan.DownloadDir)
	}
}

// TestResolveOverlayPackages_NoSeedSkipsPurge confirms that when nothing needs
// resolving (all requested packages already present), the resolver does not
// touch the cache at all.
func TestResolveOverlayPackages_NoSeedSkipsPurge(t *testing.T) {
	backend := &fakeBackend{fam: PackageManagerAPT}
	template := &config.ImageTemplate{
		Target:       config.TargetInfo{OS: "ubuntu", Dist: "ubuntu24", Arch: "amd64"},
		SystemConfig: config.SystemConfig{Packages: []string{"bash"}},
	}
	info := &BaselineInfo{OS: "ubuntu", Arch: "amd64", PackageManager: PackageManagerAPT}
	baseline := []BaselinePackage{{Name: "bash", Installed: true}}

	origBackend := selectResolverBackend
	origLoader := loadProviderRepoConfig
	origClear := clearOverlayCacheDir
	defer func() {
		selectResolverBackend = origBackend
		loadProviderRepoConfig = origLoader
		clearOverlayCacheDir = origClear
	}()
	selectResolverBackend = func(PackageManager) (resolverBackend, error) { return backend, nil }
	loadProviderRepoConfig = func(_, _, _ string) ([]config.ProviderRepoConfig, error) {
		return []config.ProviderRepoConfig{debProviderRepo()}, nil
	}
	purged := false
	clearOverlayCacheDir = func(string) error { purged = true; return nil }

	if _, err := ResolveOverlayPackages(template, info, baseline); err != nil {
		t.Fatalf("ResolveOverlayPackages: %v", err)
	}
	if purged {
		t.Error("cache must not be purged when there is nothing to resolve")
	}
}
