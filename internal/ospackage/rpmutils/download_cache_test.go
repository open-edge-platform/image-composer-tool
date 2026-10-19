package rpmutils

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/runctx"
)

func TestIsRPMPackageCacheOutdated(t *testing.T) {
	tmpDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(tmpDir, "bash-1.0-1.x86_64.rpm"), []byte("x"), 0644); err != nil {
		t.Fatalf("failed to write cached rpm: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "coreutils-1.0-1.x86_64.rpm"), []byte("x"), 0644); err != nil {
		t.Fatalf("failed to write cached rpm: %v", err)
	}

	outdated, missing, _, err := isRPMPackageCacheOutdated([]string{"bash", "coreutils"}, tmpDir)
	if err != nil {
		t.Fatalf("isRPMPackageCacheOutdated returned error: %v", err)
	}
	if outdated {
		t.Fatalf("expected cache to be up-to-date, missing=%v", missing)
	}

	outdated, missing, _, err = isRPMPackageCacheOutdated([]string{"bash", "curl"}, tmpDir)
	if err != nil {
		t.Fatalf("isRPMPackageCacheOutdated returned error: %v", err)
	}
	if !outdated {
		t.Fatalf("expected cache to be outdated")
	}
	if len(missing) != 1 || missing[0] != "curl" {
		t.Fatalf("expected missing=[curl], got %v", missing)
	}

	outdated, missing, _, err = isRPMPackageCacheOutdated([]string{"kernel-drivers-gpu-6.12.55"}, tmpDir)
	if err != nil {
		t.Fatalf("isRPMPackageCacheOutdated returned error: %v", err)
	}
	if !outdated {
		t.Fatalf("expected cache to be outdated for missing kernel-drivers-gpu")
	}
	if len(missing) != 1 || missing[0] != "kernel-drivers-gpu-6.12.55" {
		t.Fatalf("expected missing=[kernel-drivers-gpu-6.12.55], got %v", missing)
	}

	if err := os.WriteFile(filepath.Join(tmpDir, "kernel-drivers-gpu-6.12.55-2.emt3.x86_64.rpm"), []byte("x"), 0644); err != nil {
		t.Fatalf("failed to write cached rpm: %v", err)
	}
	outdated, missing, _, err = isRPMPackageCacheOutdated([]string{"kernel-drivers-gpu-6.12.55"}, tmpDir)
	if err != nil {
		t.Fatalf("isRPMPackageCacheOutdated returned error: %v", err)
	}
	if !outdated {
		t.Fatalf("expected version-pinned package to be reported missing when only exact cached package names are checked")
	}
	if len(missing) != 1 || missing[0] != "kernel-drivers-gpu-6.12.55" {
		t.Fatalf("expected missing=[kernel-drivers-gpu-6.12.55], got %v", missing)
	}
}

func TestClearRPMPackageCache(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(dir string)
		wantLeft []string // non-.rpm files that must survive
	}{
		{
			name: "clears all rpm files",
			setup: func(dir string) {
				_ = os.WriteFile(filepath.Join(dir, "bash-1.0-1.x86_64.rpm"), []byte("x"), 0644)
				_ = os.WriteFile(filepath.Join(dir, "curl-1.0-1.x86_64.rpm"), []byte("x"), 0644)
			},
		},
		{
			name: "leaves non-rpm files untouched",
			setup: func(dir string) {
				_ = os.WriteFile(filepath.Join(dir, "bash-1.0-1.x86_64.rpm"), []byte("x"), 0644)
				_ = os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0644)
			},
			wantLeft: []string{"notes.txt"},
		},
		{
			name:  "empty cache dir is a no-op",
			setup: func(dir string) {},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.setup(dir)

			if err := clearRPMPackageCache(dir); err != nil {
				t.Fatalf("clearRPMPackageCache() unexpected error: %v", err)
			}

			rpms, _ := filepath.Glob(filepath.Join(dir, "*.rpm"))
			if len(rpms) != 0 {
				t.Errorf("expected no .rpm files after clear, got %v", rpms)
			}

			for _, name := range tt.wantLeft {
				if _, statErr := os.Stat(filepath.Join(dir, name)); os.IsNotExist(statErr) {
					t.Errorf("expected file %s to survive cache clear, but it was removed", name)
				}
			}
		})
	}
}

func TestClearRPMMetadataCache(t *testing.T) {
	origCfg := config.Global()
	updatedCfg := origCfg
	updatedCfg.CacheDir = t.TempDir()
	config.SetGlobal(updatedCfg)
	defer config.SetGlobal(origCfg)

	origRepoCfg := RepoCfg
	defer func() { RepoCfg = origRepoCfg }()

	testURL := "https://example.com/rpms/"
	RepoCfg = RepoConfig{URL: testURL}

	metaDirName := generateRPMMetadataDir(testURL)
	metaDir := filepath.Join(updatedCfg.CacheDir, "rpm-metadata", metaDirName)
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		t.Fatalf("failed to create metadata dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(metaDir) })

	for _, name := range []string{
		"primary.parsed.json",
		"primary.location.json",
		"repomd.xml",
		"repomd_aaaaaaaa_2000-01-01_00-00-00.xml",
		"primary_aaaaaaaaaaaaaaaa.cache.json",
		"primary_aaaaaaaaaaaaaaaa.gz",
		"primary_bbbbbbbbbbbbbbbb.zst",
		"primary_cccccccccccccccc.xml",
	} {
		if err := os.WriteFile(filepath.Join(metaDir, name), []byte("{}"), 0644); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}

	pkgDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(pkgDir, "bash-1.0-1.x86_64.rpm"), []byte("x"), 0644); err != nil {
		t.Fatalf("failed to write rpm: %v", err)
	}

	if err := clearRPMPackageCache(pkgDir); err != nil {
		t.Fatalf("clearRPMPackageCache() unexpected error: %v", err)
	}

	// repomd.xml and primary.location.json are the authoritative manifests
	// established by FetchPrimaryURL at Init(); they must survive metadata
	// clearing so that a subsequent offline rebuild can reuse the cache.
	for _, name := range []string{
		"primary.parsed.json",
		"repomd_aaaaaaaa_2000-01-01_00-00-00.xml",
		"primary_aaaaaaaaaaaaaaaa.cache.json",
		"primary_aaaaaaaaaaaaaaaa.gz",
		"primary_bbbbbbbbbbbbbbbb.zst",
		"primary_cccccccccccccccc.xml",
	} {
		f := filepath.Join(metaDir, name)
		if _, statErr := os.Stat(f); !os.IsNotExist(statErr) {
			t.Errorf("expected %s to be removed after cache clear", name)
		}
	}

	for _, name := range []string{
		"repomd.xml",
		"primary.location.json",
	} {
		f := filepath.Join(metaDir, name)
		if _, statErr := os.Stat(f); statErr != nil {
			t.Errorf("expected %s to be preserved after cache clear (offline manifest), got: %v", name, statErr)
		}
	}
}

func TestHandleRPMCacheRetry(t *testing.T) {
	origCacheOutdatedFunc := isRPMPackageCacheOutdatedFunc
	origClearMetadataFunc := clearRPMMetadataCacheFunc
	origRefreshFunc := refreshRPMPrimaryFunc
	t.Cleanup(func() {
		isRPMPackageCacheOutdatedFunc = origCacheOutdatedFunc
		clearRPMMetadataCacheFunc = origClearMetadataFunc
		refreshRPMPrimaryFunc = origRefreshFunc
	})

	tests := []struct {
		name               string
		retried            bool
		wantHandled        bool
		wantClearCalls     int
		wantRetryCalls     int
		wantDownloadResult bool
	}{
		{
			name:               "clears metadata and retries once on first outdated check",
			retried:            false,
			wantHandled:        true,
			wantClearCalls:     1,
			wantRetryCalls:     1,
			wantDownloadResult: true,
		},
		{
			name:               "does not clear metadata or retry again after retry flag is set",
			retried:            true,
			wantHandled:        false,
			wantClearCalls:     0,
			wantRetryCalls:     0,
			wantDownloadResult: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			cacheChecks := 0
			clearCalls := 0
			retryCalls := 0

			isRPMPackageCacheOutdatedFunc = func(requiredPackages []string, cacheDir string) (bool, []string, []string, error) {
				cacheChecks++
				return true, []string{"pkg-a"}, nil, nil
			}
			clearRPMMetadataCacheFunc = func(repoURLs []string) {
				clearCalls++
			}
			refreshRPMPrimaryFunc = func(repoURLs []string) []string { return repoURLs }

			gotDownloads, gotInfos, handled, err := handleRPMCacheRetry(
				[]string{"pkg-a"},
				map[string]string{"pkg-a": "https://repo.example"},
				t.TempDir(),
				tt.retried,
				func() ([]string, []ospackage.PackageInfo, error) {
					retryCalls++
					return []string{"pkg-a-1.0-1.x86_64.rpm"}, []ospackage.PackageInfo{{Name: "pkg-a"}}, nil
				},
			)
			if err != nil {
				t.Fatalf("handleRPMCacheRetry() error = %v", err)
			}
			if cacheChecks != 1 {
				t.Fatalf("cache state checks = %d, want 1", cacheChecks)
			}
			if handled != tt.wantHandled {
				t.Fatalf("handled = %v, want %v", handled, tt.wantHandled)
			}
			if clearCalls != tt.wantClearCalls {
				t.Fatalf("metadata clear calls = %d, want %d", clearCalls, tt.wantClearCalls)
			}
			if retryCalls != tt.wantRetryCalls {
				t.Fatalf("retry calls = %d, want %d", retryCalls, tt.wantRetryCalls)
			}
			if tt.wantDownloadResult {
				if len(gotDownloads) != 1 {
					t.Fatalf("download result length = %d, want 1", len(gotDownloads))
				}
				if len(gotInfos) != 1 {
					t.Fatalf("package info length = %d, want 1", len(gotInfos))
				}
			} else {
				if len(gotDownloads) != 0 {
					t.Fatalf("download result length = %d, want 0", len(gotDownloads))
				}
				if len(gotInfos) != 0 {
					t.Fatalf("package info length = %d, want 0", len(gotInfos))
				}
			}
		})
	}
}

// Requirements derived from repository metadata are rpm file names. The cache
// satisfies one only with that exact file, so a cached older version still counts
// as missing and the metadata is refreshed.
func TestIsRPMPackageCacheOutdated_FileNameRequirements(t *testing.T) {
	cacheDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(cacheDir, "bash-5.0-1.el9.x86_64.rpm"), []byte("old"), 0644); err != nil {
		t.Fatalf("seed cached rpm: %v", err)
	}

	outdated, missing, _, err := isRPMPackageCacheOutdated([]string{"bash-5.0-1.el9.x86_64.rpm"}, cacheDir)
	if err != nil || outdated || len(missing) != 0 {
		t.Fatalf("exact file name: outdated=%v missing=%v err=%v, want a cache hit", outdated, missing, err)
	}

	outdated, missing, _, err = isRPMPackageCacheOutdated([]string{"bash-5.1-8.el9.x86_64.rpm"}, cacheDir)
	if err != nil || !outdated || len(missing) != 1 || missing[0] != "bash-5.1-8.el9.x86_64.rpm" {
		t.Fatalf("newer version: outdated=%v missing=%v err=%v, want it reported missing", outdated, missing, err)
	}
}

// bracketedDirs returns a directory whose name is a glob character class and a
// sibling directory that the class matches, so a globbed read of the first one
// would see the second one's files instead.
func bracketedDirs(t *testing.T) (literal, sibling string) {
	t.Helper()
	root := t.TempDir()
	literal = filepath.Join(root, "cache[1]")
	sibling = filepath.Join(root, "cache1")
	for _, dir := range []string{literal, sibling} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	return literal, sibling
}

func writeTestFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("rpm"), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

func TestListRPMFilesReadsDirectoryLiterally(t *testing.T) {
	literal, sibling := bracketedDirs(t)
	writeTestFiles(t, literal, "bash-1.0-1.x86_64.rpm", "notes.txt")
	writeTestFiles(t, sibling, "curl-1.0-1.x86_64.rpm")
	if err := os.Mkdir(filepath.Join(literal, "dir.rpm"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, err := listRPMFiles(literal)
	if err != nil {
		t.Fatalf("listRPMFiles() error = %v", err)
	}
	want := filepath.Join(literal, "bash-1.0-1.x86_64.rpm")
	if len(got) != 1 || got[0] != want {
		t.Fatalf("listRPMFiles() = %v, want [%s]", got, want)
	}

	if got, err := listRPMFiles(filepath.Join(literal, "missing")); err != nil || len(got) != 0 {
		t.Fatalf("listRPMFiles(missing) = %v, %v; want no files and no error", got, err)
	}
}

func TestIsRPMPackageCacheOutdatedReadsCacheDirLiterally(t *testing.T) {
	literal, sibling := bracketedDirs(t)
	writeTestFiles(t, literal, "bash-1.0-1.x86_64.rpm")
	writeTestFiles(t, sibling, "curl-1.0-1.x86_64.rpm")

	outdated, missing, _, err := isRPMPackageCacheOutdated([]string{"bash-1.0-1.x86_64.rpm"}, literal)
	if err != nil || outdated {
		t.Fatalf("outdated = %v, missing = %v, err = %v; want a complete cache", outdated, missing, err)
	}

	outdated, missing, _, err = isRPMPackageCacheOutdated([]string{"curl-1.0-1.x86_64.rpm"}, literal)
	if err != nil || !outdated || len(missing) != 1 {
		t.Fatalf("outdated = %v, missing = %v, err = %v; want curl missing from the literal directory", outdated, missing, err)
	}
}

func TestClearRPMPackageCacheReadsCacheDirLiterally(t *testing.T) {
	literal, sibling := bracketedDirs(t)
	writeTestFiles(t, literal, "bash-1.0-1.x86_64.rpm")
	writeTestFiles(t, sibling, "curl-1.0-1.x86_64.rpm")

	if err := clearRPMPackageCache(literal); err != nil {
		t.Fatalf("clearRPMPackageCache() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(literal, "bash-1.0-1.x86_64.rpm")); !os.IsNotExist(err) {
		t.Fatalf("the configured package cache was not cleared (err=%v)", err)
	}
	if _, err := os.Stat(filepath.Join(sibling, "curl-1.0-1.x86_64.rpm")); err != nil {
		t.Fatalf("a sibling directory matching the glob pattern was cleared: %v", err)
	}
}

func TestMetadataRefreshRequirementsSkipsLocalRepoPackages(t *testing.T) {
	orig := localRepoOrigins
	t.Cleanup(func() { localRepoOrigins = orig })
	localRepoOrigins = []repoOrigin{{baseURL: "http://127.0.0.1:4000"}}

	pkgs := []ospackage.PackageInfo{
		{Name: "bash", URL: "https://repo.example/Packages/bash-1.0-1.x86_64.rpm"},
		{Name: "mine", URL: "http://127.0.0.1:4000/mine-1.0-1.x86_64.rpm"},
		// Shares the local repository's URL as a string prefix but is another host.
		{Name: "other", URL: "http://127.0.0.1:40001/other-1.0-1.x86_64.rpm"},
		{Name: "bare"},
	}

	got, _ := metadataRefreshRequirements(pkgs)
	want := []string{"bash-1.0-1.x86_64.rpm", "other-1.0-1.x86_64.rpm", "bare"}
	if len(got) != len(want) {
		t.Fatalf("metadataRefreshRequirements() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("metadataRefreshRequirements() = %v, want %v", got, want)
		}
	}
}

func setConfiguredRepos(t *testing.T, baseURL string, userURLs ...string) {
	t.Helper()
	origRepoCfg, origUserRepo := RepoCfg, UserRepo
	t.Cleanup(func() { RepoCfg, UserRepo = origRepoCfg, origUserRepo })

	RepoCfg = RepoConfig{URL: baseURL}
	UserRepo = nil
	for _, u := range userURLs {
		UserRepo = append(UserRepo, config.PackageRepository{URL: u})
	}
}

func TestMetadataRefreshRequirementsMapsFilesToConfiguredRepos(t *testing.T) {
	setConfiguredRepos(t, "https://base.example/os/", "https://base.example/os/extra", "https://user.example")

	pkgs := []ospackage.PackageInfo{
		{Name: "bash", URL: "https://base.example/os/Packages/bash-1.0-1.x86_64.rpm"},
		// The user repository nested under the base URL is the longer match.
		{Name: "extra", URL: "https://base.example/os/extra/extra-1.0-1.x86_64.rpm"},
		{Name: "tool", URL: "https://user.example/tool-1.0-1.x86_64.rpm"},
		{Name: "stray", URL: "https://elsewhere.example/stray-1.0-1.x86_64.rpm"},
	}

	_, got := metadataRefreshRequirements(pkgs)
	want := map[string]string{
		"bash-1.0-1.x86_64.rpm":  "https://base.example/os/",
		"extra-1.0-1.x86_64.rpm": "https://base.example/os/extra",
		"tool-1.0-1.x86_64.rpm":  "https://user.example",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("repositories = %v, want %v", got, want)
	}
}

func TestReposToRefresh(t *testing.T) {
	setConfiguredRepos(t, "https://base.example/", "https://user.example")
	repos := map[string]string{
		"base-1.rpm": "https://base.example/",
		"user-1.rpm": "https://user.example",
		"user-2.rpm": "https://user.example",
	}

	tests := []struct {
		name    string
		missing []string
		want    []string
	}{
		{"only the repository serving the missing file", []string{"user-1.rpm", "user-2.rpm"}, []string{"https://user.example"}},
		{"each repository once, in order", []string{"user-1.rpm", "base-1.rpm"}, []string{"https://user.example", "https://base.example/"}},
		{"an unmapped file leaves every repository in question", []string{"user-1.rpm", "unknown"}, []string{"https://base.example/", "https://user.example"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reposToRefresh(tt.missing, repos); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("reposToRefresh(%v) = %v, want %v", tt.missing, got, tt.want)
			}
		})
	}
}

// A file missing from one repository must not discard the metadata of another:
// a base repository that is only available from its cache has to stay usable
// while the retry refreshes the user repository that supplies the missing RPM.
func TestHandleRPMCacheRetryClearsOnlyRepositoryWithMissingFile(t *testing.T) {
	origOutdated, origClear, origRefresh := isRPMPackageCacheOutdatedFunc, clearRPMMetadataCacheFunc, refreshRPMPrimaryFunc
	t.Cleanup(func() {
		isRPMPackageCacheOutdatedFunc, clearRPMMetadataCacheFunc, refreshRPMPrimaryFunc = origOutdated, origClear, origRefresh
	})
	setConfiguredRepos(t, "https://base.example/", "https://user.example")

	isRPMPackageCacheOutdatedFunc = func([]string, string) (bool, []string, []string, error) {
		return true, []string{"user-1.rpm"}, nil, nil
	}
	var refreshed, cleared []string
	refreshRPMPrimaryFunc = func(repoURLs []string) []string { refreshed = repoURLs; return repoURLs }
	clearRPMMetadataCacheFunc = func(repoURLs []string) { cleared = repoURLs }

	_, _, _, err := handleRPMCacheRetry(
		[]string{"base-1.rpm", "user-1.rpm"},
		map[string]string{"base-1.rpm": "https://base.example/", "user-1.rpm": "https://user.example"},
		t.TempDir(), false,
		func() ([]string, []ospackage.PackageInfo, error) { return nil, nil, nil },
	)
	if err != nil {
		t.Fatalf("handleRPMCacheRetry() error = %v", err)
	}
	want := []string{"https://user.example"}
	if !reflect.DeepEqual(refreshed, want) {
		t.Fatalf("refreshed repositories = %v, want %v", refreshed, want)
	}
	if !reflect.DeepEqual(cleared, want) {
		t.Fatalf("cleared repositories = %v, want %v", cleared, want)
	}
}

// Cached app-1 needs lib-1 from the base repository and only the app repository
// is refreshed. If it now serves app-2, which needs a newer lib than the
// retained base index offers, resolution fails until the base repository is
// refreshed as well.
func TestHandleRPMCacheRetryRefreshesOtherRepositoriesWhenResolutionFails(t *testing.T) {
	origOutdated, origClear, origRefresh := isRPMPackageCacheOutdatedFunc, clearRPMMetadataCacheFunc, refreshRPMPrimaryFunc
	t.Cleanup(func() {
		isRPMPackageCacheOutdatedFunc, clearRPMMetadataCacheFunc, refreshRPMPrimaryFunc = origOutdated, origClear, origRefresh
	})
	setConfiguredRepos(t, "https://base.example/", "https://app.example")

	isRPMPackageCacheOutdatedFunc = func([]string, string) (bool, []string, []string, error) {
		return true, []string{"app-2.rpm"}, nil, nil
	}
	baseMetadataCleared := false
	var refreshCalls, clearCalls [][]string
	refreshRPMPrimaryFunc = func(repoURLs []string) []string {
		refreshCalls = append(refreshCalls, repoURLs)
		return repoURLs
	}
	clearRPMMetadataCacheFunc = func(repoURLs []string) {
		clearCalls = append(clearCalls, repoURLs)
		for _, repoURL := range repoURLs {
			if repoURL == "https://base.example/" {
				baseMetadataCleared = true
			}
		}
	}

	retries := 0
	pkgs, _, handled, err := handleRPMCacheRetry(
		[]string{"app-2.rpm"},
		map[string]string{"app-2.rpm": "https://app.example"},
		t.TempDir(), false,
		func() ([]string, []ospackage.PackageInfo, error) {
			retries++
			if !baseMetadataCleared {
				return nil, nil, &resolutionError{errors.New("resolving packages: app-2 needs lib >= 2")}
			}
			return []string{"app-2.rpm"}, nil, nil
		},
	)
	if err != nil || !handled {
		t.Fatalf("handleRPMCacheRetry() = handled %v, error %v, want a successful retry", handled, err)
	}
	if len(pkgs) != 1 || retries != 2 {
		t.Fatalf("packages = %v after %d retries, want the packages after exactly 2", pkgs, retries)
	}
	wantCalls := [][]string{{"https://app.example"}, {"https://base.example/"}}
	if !reflect.DeepEqual(refreshCalls, wantCalls) || !reflect.DeepEqual(clearCalls, wantCalls) {
		t.Fatalf("refreshed %v, cleared %v, want both %v", refreshCalls, clearCalls, wantCalls)
	}
}

func TestHandleRPMCacheRetryFallbackIsBoundedAndSelective(t *testing.T) {
	origOutdated, origClear, origRefresh := isRPMPackageCacheOutdatedFunc, clearRPMMetadataCacheFunc, refreshRPMPrimaryFunc
	t.Cleanup(func() {
		isRPMPackageCacheOutdatedFunc, clearRPMMetadataCacheFunc, refreshRPMPrimaryFunc = origOutdated, origClear, origRefresh
	})
	setConfiguredRepos(t, "https://base.example/", "https://app.example")
	isRPMPackageCacheOutdatedFunc = func([]string, string) (bool, []string, []string, error) {
		return true, []string{"app-2.rpm"}, nil, nil
	}

	resolveFailure := &resolutionError{errors.New("resolving packages: unsatisfied")}
	tests := []struct {
		name        string
		retryErr    error
		baseReach   bool
		wantRetries int
		wantCleared [][]string
	}{
		{"download failures do not refresh other repositories", errors.New("fetch failed"), true, 1,
			[][]string{{"https://app.example"}}},
		{"an unreachable repository keeps its cached metadata", resolveFailure, false, 1,
			[][]string{{"https://app.example"}}},
		{"a persisting resolution failure is retried only once more", resolveFailure, true, 2,
			[][]string{{"https://app.example"}, {"https://base.example/"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cleared [][]string
			refreshRPMPrimaryFunc = func(repoURLs []string) []string {
				if len(repoURLs) == 1 && repoURLs[0] == "https://base.example/" && !tt.baseReach {
					return nil
				}
				return repoURLs
			}
			clearRPMMetadataCacheFunc = func(repoURLs []string) { cleared = append(cleared, repoURLs) }

			retries := 0
			_, _, handled, err := handleRPMCacheRetry(
				[]string{"app-2.rpm"},
				map[string]string{"app-2.rpm": "https://app.example"},
				t.TempDir(), false,
				func() ([]string, []ospackage.PackageInfo, error) {
					retries++
					return nil, nil, tt.retryErr
				},
			)
			if !handled || err != tt.retryErr {
				t.Fatalf("handleRPMCacheRetry() = handled %v, error %v, want the retry's error %v", handled, err, tt.retryErr)
			}
			if retries != tt.wantRetries {
				t.Fatalf("retries = %d, want %d", retries, tt.wantRetries)
			}
			if !reflect.DeepEqual(cleared, tt.wantCleared) {
				t.Fatalf("cleared = %v, want %v", cleared, tt.wantCleared)
			}
		})
	}
}

// A repository whose current reference cannot be fetched keeps its cached
// metadata: discarding it would leave nothing to resolve from.
func TestHandleRPMCacheRetryKeepsMetadataWhenReferenceCannotBeRefreshed(t *testing.T) {
	origOutdated, origClear, origRefresh := isRPMPackageCacheOutdatedFunc, clearRPMMetadataCacheFunc, refreshRPMPrimaryFunc
	t.Cleanup(func() {
		isRPMPackageCacheOutdatedFunc, clearRPMMetadataCacheFunc, refreshRPMPrimaryFunc = origOutdated, origClear, origRefresh
	})
	setConfiguredRepos(t, "https://base.example/")

	isRPMPackageCacheOutdatedFunc = func([]string, string) (bool, []string, []string, error) {
		return true, []string{"base-1.rpm"}, nil, nil
	}
	refreshRPMPrimaryFunc = func([]string) []string { return nil }
	clearRPMMetadataCacheFunc = func([]string) { t.Fatal("metadata cleared although no reference was refreshed") }

	_, _, handled, err := handleRPMCacheRetry(
		[]string{"base-1.rpm"},
		map[string]string{"base-1.rpm": "https://base.example/"},
		t.TempDir(), false,
		func() ([]string, []ospackage.PackageInfo, error) {
			t.Fatal("retried although no reference was refreshed")
			return nil, nil, nil
		},
	)
	if err != nil {
		t.Fatalf("handleRPMCacheRetry() error = %v", err)
	}
	if handled {
		t.Fatal("handled = true, want false so the caller continues with the cached metadata")
	}
}

func TestClearRPMMetadataCacheLeavesOtherRepositories(t *testing.T) {
	cacheRoot := setRPMMetadataTestCache(t)
	baseURL, userURL := "https://base.example/os", "https://user.example/repo"

	dirs := map[string]string{}
	for _, u := range []string{baseURL, userURL} {
		dir := filepath.Join(cacheRoot, "rpm-metadata", generateRPMMetadataDir(u))
		mustMkdir(t, dir)
		writeTestFiles(t, dir, "primary.parsed.json", "primary_aaaaaaaaaaaaaaaa_bbbbbbbbbbbbbbbb.gz", "primary_aaaaaaaaaaaaaaaa.cache.json", "repomd.xml")
		dirs[u] = dir
	}

	clearRPMMetadataCache([]string{userURL})

	for _, name := range []string{"primary.parsed.json", "primary_aaaaaaaaaaaaaaaa_bbbbbbbbbbbbbbbb.gz", "primary_aaaaaaaaaaaaaaaa.cache.json"} {
		if _, err := os.Stat(filepath.Join(dirs[userURL], name)); !os.IsNotExist(err) {
			t.Errorf("%s was not cleared from the repository being refreshed (err=%v)", name, err)
		}
		if _, err := os.Stat(filepath.Join(dirs[baseURL], name)); err != nil {
			t.Errorf("%s was cleared from a repository that was not being refreshed: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dirs[userURL], "repomd.xml")); err != nil {
		t.Errorf("repomd.xml must survive a clear: %v", err)
	}
}

func mustMkdir(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	return dir
}

func TestRPMRawMetadataHrefHash(t *testing.T) {
	const h = "0123456789abcdef"
	tests := map[string]string{
		"primary_" + h + ".lock":                  h,
		"primary_" + h + ".cache.json":            h,
		"primary_" + h + "_fedcba9876543210.gz":   h,
		"primary_" + h + "_fedcba9876543210.zst":  h,
		"primary.parsed.json":                     "",
		"repomd.xml":                              "",
		"primary_" + h[:8] + ".cache.json":        "",
		"primary_" + h + "_fedcba98.gz":           "",
		"repomd_aaaaaaaa_2000-01-01_00-00-00.xml": "",
	}
	for name, want := range tests {
		if got := rpmRawMetadataHrefHash(name); got != want {
			t.Errorf("rpmRawMetadataHrefHash(%q) = %q, want %q", name, got, want)
		}
	}
}

// A writer publishes a payload and then the manifest naming it, all under the
// href's lock. A clear that ran between the two would delete the payload and
// leave a manifest naming a file that no longer exists, so the clear has to wait
// for the writer and then remove both.
func TestClearRPMMetadataDirWaitsForRawMetadataWriter(t *testing.T) {
	dir := t.TempDir()
	const href = "repodata/primary.xml.gz"
	primary := rpmPrimaryReference{Href: href}
	payload := rpmRawMetadataPayloadPath(dir, href, primary, []byte("payload"))
	manifest := rpmRawMetadataManifestPath(dir, href)

	unlock, err := lockRPMRawMetadata(dir, href)
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	released := false
	t.Cleanup(func() {
		if !released {
			unlock()
		}
	})
	if err := writeFileAtomic(payload, []byte("payload"), 0644); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	done := make(chan struct{})
	go func() {
		clearRPMMetadataDir(dir)
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("clearRPMMetadataDir returned while a writer held the lock")
	case <-time.After(200 * time.Millisecond):
	}
	if _, err := os.Stat(payload); err != nil {
		t.Fatalf("payload removed while the writer held the lock: %v", err)
	}

	if err := writeFileAtomic(manifest, []byte("{}"), 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	unlock()
	released = true

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("clearRPMMetadataDir did not finish after the writer released the lock")
	}
	for _, p := range []string{payload, manifest} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s survived the clear (err=%v)", filepath.Base(p), err)
		}
	}
}

// A build cancelled while another holds the cache lock must stop waiting for it
// and report the cancellation, not write without the lock.
func TestSaveRPMRawMetadataCacheStopsWaitingForLockWhenCancelled(t *testing.T) {
	dir := t.TempDir()
	const href = "repodata/primary.xml.gz"
	primary := rpmPrimaryReference{Href: href}
	data := []byte("payload")

	unlock, err := lockRPMRawMetadata(dir, href)
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	defer unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	t.Cleanup(runctx.SetContext(ctx))

	done := make(chan error, 1)
	go func() { done <- saveRPMRawMetadataCache(dir, href, rpmMetadataID("http://repo", href), primary, data) }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("saveRPMRawMetadataCache() error = %v, want one wrapping the cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("saveRPMRawMetadataCache did not return after the build was cancelled")
	}
	if _, err := os.Stat(rpmRawMetadataPayloadPath(dir, href, primary, data)); !os.IsNotExist(err) {
		t.Fatalf("a payload was written without the lock (err=%v)", err)
	}
}

// A clear that is cancelled while waiting for the cache lock must leave the files
// in place rather than remove them without it.
func TestClearRPMMetadataDirLeavesFilesWhenCancelledWaitingForLock(t *testing.T) {
	dir := t.TempDir()
	const href = "repodata/primary.xml.gz"
	primary := rpmPrimaryReference{Href: href}
	payload := rpmRawMetadataPayloadPath(dir, href, primary, []byte("payload"))
	manifest := rpmRawMetadataManifestPath(dir, href)
	for _, p := range []string{payload, manifest} {
		if err := writeFileAtomic(p, []byte("x"), 0644); err != nil {
			t.Fatalf("seed %s: %v", p, err)
		}
	}

	unlock, err := lockRPMRawMetadata(dir, href)
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	defer unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	t.Cleanup(runctx.SetContext(ctx))

	done := make(chan struct{})
	go func() {
		clearRPMMetadataDir(dir)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("clearRPMMetadataDir did not return after the build was cancelled")
	}
	for _, p := range []string{payload, manifest} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s was removed without the lock: %v", filepath.Base(p), err)
		}
	}
}

// A lock file this build cannot open may be one another build holds, so a clear
// that cannot take the lock must leave the files rather than remove a payload a
// writer is about to publish a manifest for.
func TestClearRPMMetadataDirLeavesFilesWhenLockCannotBeOpened(t *testing.T) {
	dir := t.TempDir()
	const href = "repodata/primary.xml.gz"
	primary := rpmPrimaryReference{Href: href}
	payload := rpmRawMetadataPayloadPath(dir, href, primary, []byte("payload"))
	manifest := rpmRawMetadataManifestPath(dir, href)
	for _, p := range []string{payload, manifest} {
		if err := writeFileAtomic(p, []byte("x"), 0644); err != nil {
			t.Fatalf("seed %s: %v", p, err)
		}
	}
	// A directory in place of the lock file cannot be opened for writing.
	if err := os.Mkdir(rpmRawMetadataLockPath(dir, href), 0755); err != nil {
		t.Fatalf("make unopenable lock: %v", err)
	}

	clearRPMMetadataDir(dir)

	for _, p := range []string{payload, manifest} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s was removed without the lock: %v", filepath.Base(p), err)
		}
	}
}

// A cache write that cannot take the lock must not write unlocked: a clear that
// holds the lock through a file this build cannot open would race it.
func TestSaveRPMRawMetadataCacheFailsWhenLockCannotBeOpened(t *testing.T) {
	dir := t.TempDir()
	const href = "repodata/primary.xml.gz"
	primary := rpmPrimaryReference{Href: href}
	data := []byte("payload")
	if err := os.Mkdir(rpmRawMetadataLockPath(dir, href), 0755); err != nil {
		t.Fatalf("make unopenable lock: %v", err)
	}

	if err := saveRPMRawMetadataCache(dir, href, rpmMetadataID("http://repo", href), primary, data); err == nil {
		t.Fatal("saveRPMRawMetadataCache() succeeded without the lock")
	}
	if _, err := os.Stat(rpmRawMetadataPayloadPath(dir, href, primary, data)); !os.IsNotExist(err) {
		t.Fatalf("a payload was written without the lock (err=%v)", err)
	}
	if _, err := os.Stat(rpmRawMetadataManifestPath(dir, href)); !os.IsNotExist(err) {
		t.Fatalf("a manifest was written without the lock (err=%v)", err)
	}
}
