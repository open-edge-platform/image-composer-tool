package debutils

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/open-edge-platform/os-image-composer/internal/config"
	"github.com/open-edge-platform/os-image-composer/internal/ospackage"
	"github.com/open-edge-platform/os-image-composer/internal/ospackage/dotfilter"
	"github.com/open-edge-platform/os-image-composer/internal/ospackage/pkgfetcher"
	"github.com/open-edge-platform/os-image-composer/internal/ospackage/pkgsorter"
	"github.com/open-edge-platform/os-image-composer/internal/utils/logger"
	"github.com/open-edge-platform/os-image-composer/internal/utils/network"
	"github.com/open-edge-platform/os-image-composer/internal/utils/slice"
)

// Repository represents a Debian repository
type Repository struct {
	ID            string
	Codename      string
	URL           string
	PKey          string
	Component     string
	Priority      int
	AllowPackages []string
}

// repoConfig hold repo related info
type RepoConfig struct {
	Section       string // raw section header
	Name          string // human-readable name from name=
	PkgList       string
	PkgPrefix     string
	GPGCheck      bool
	RepoGPGCheck  bool
	Enabled       bool
	PbGPGKey      string
	ReleaseFile   string
	ReleaseSign   string
	BuildPath     string   // path to store builds, relative to the root of the repo
	Arch          string   // architecture, e.g., amd64, all
	Priority      int      // repository priority (higher numbers = higher priority)
	AllowPackages []string // optional package filter for this repository
}

type pkgChecksum struct {
	Name     string
	Checksum string
}

var (
	RepoCfg      RepoConfig
	RepoCfgs     []RepoConfig // Support for multiple repositories
	PkgChecksum  []pkgChecksum
	GzHref       string
	Architecture string
	UserRepo     []config.PackageRepository
	ReportPath   = "builds"
)

// Packages returns the list of base packages
func Packages() ([]ospackage.PackageInfo, error) {
	log := logger.Logger()
	log.Infof("fetching packages from %s", RepoCfg.PkgList)

	packages, err := ParseRepositoryMetadata(RepoCfg.PkgPrefix, GzHref, RepoCfg.ReleaseFile, RepoCfg.ReleaseSign, RepoCfg.PbGPGKey, RepoCfg.BuildPath, RepoCfg.Arch, RepoCfg.AllowPackages)
	if err != nil {
		return nil, fmt.Errorf("parsing default repo failed: %w", err)
	}

	log.Infof("found %d packages in deb repo", len(packages))
	return packages, nil
}

// PackagesFromMultipleRepos returns packages from all configured repositories
func PackagesFromMultipleRepos() ([]ospackage.PackageInfo, error) {
	log := logger.Logger()

	if len(RepoCfgs) == 0 {
		log.Warnf("No multiple repositories configured, falling back to single repository")
		return Packages()
	}

	var allPackages []ospackage.PackageInfo
	var failedRepos []string

	for i, repoCfg := range RepoCfgs {
		log.Infof("fetching packages from repository %d: %s (%s)", i+1, repoCfg.Name, repoCfg.PkgList)

		packages, err := ParseRepositoryMetadata(repoCfg.PkgPrefix, repoCfg.PkgList, repoCfg.ReleaseFile, repoCfg.ReleaseSign, repoCfg.PbGPGKey, repoCfg.BuildPath, repoCfg.Arch, repoCfg.AllowPackages)
		if err != nil {
			log.Warnf("Failed to parse repository %s: %v", repoCfg.Name, err)
			failedRepos = append(failedRepos, repoCfg.Name)
			continue // Skip this repository but continue with others
		}

		log.Infof("found %d packages in repository %s", len(packages), repoCfg.Name)
		allPackages = append(allPackages, packages...)
	}

	// If all repositories failed, return an error
	if len(failedRepos) == len(RepoCfgs) {
		return nil, fmt.Errorf("all %d repositories failed to parse", len(RepoCfgs))
	}

	log.Infof("found total of %d packages from %d repositories", len(allPackages), len(RepoCfgs))
	return allPackages, nil
}

// BuildRepoConfigs converts Repository entries to RepoConfig format
func BuildRepoConfigs(userRepoList []Repository, arch string) ([]RepoConfig, error) {
	var userRepo []RepoConfig
	for _, repoItem := range userRepoList {
		connectSuccess := false
		id := repoItem.ID
		codename := repoItem.Codename
		baseURL := repoItem.URL
		pkey := repoItem.PKey
		archs := arch + ",all"
		releaseNm := "Release"
		component := repoItem.Component
		if strings.TrimSpace(component) == "" {
			component = "main"
		}
		for _, componentName := range slice.SplitBySpace(component) {
			for _, localArch := range strings.Split(archs, ",") {
				package_list_url, err := GetPackagesNames(baseURL, codename, localArch, componentName)
				if err != nil {
					return nil, fmt.Errorf("getting package metadata name: %w, baseURL %s codename %s localArch %s componentName %s\n", err, baseURL, codename, localArch, componentName)
				}
				if package_list_url == "" {
					fmt.Printf("getting package metadata name: %v, baseURL %s codename %s localArch %s componentName %s\n", err, baseURL, codename, localArch, componentName)
					continue // No valid package list found for this arch/component
				}
				fmt.Printf("SUCCESS: baseURL %s codename %s localArch %s componentName %s\n", baseURL, codename, localArch, componentName)
				repo := RepoConfig{
					PkgList:       package_list_url,
					ReleaseFile:   fmt.Sprintf("%s/dists/%s/%s", baseURL, codename, releaseNm),
					ReleaseSign:   fmt.Sprintf("%s/dists/%s/%s.gpg", baseURL, codename, releaseNm),
					PkgPrefix:     baseURL,
					Name:          id,
					GPGCheck:      true,
					RepoGPGCheck:  true,
					Enabled:       true,
					PbGPGKey:      pkey,
					BuildPath:     filepath.Join(config.TempDir(), "builds", fmt.Sprintf("%s_%s_%s", id, localArch, componentName)),
					Arch:          localArch,
					Priority:      repoItem.Priority,
					AllowPackages: repoItem.AllowPackages,
				}
				userRepo = append(userRepo, repo)
				connectSuccess = true
			}
		}

		if !connectSuccess {
			return nil, fmt.Errorf("fail connecting to repository %s", baseURL)
		}
	}

	return userRepo, nil
}

func UserPackages() ([]ospackage.PackageInfo, error) {

	log := logger.Logger()
	log.Infof("fetching packages from %s", "user package list")

	var repoList []Repository
	repoGroup := "custrepo"
	for i, repo := range UserRepo {
		// if baseURL is a placeholder, dont process it
		if repo.URL == "<URL>" || repo.URL == "" {
			continue
		}
		baseURL := strings.TrimPrefix(strings.TrimPrefix(repo.URL, "http://"), "https://")
		repoList = append(repoList, Repository{
			ID:            fmt.Sprintf("%s%d", repoGroup+"-"+baseURL, i+1),
			Codename:      repo.Codename,
			URL:           repo.URL,
			PKey:          repo.PKey,
			Component:     repo.Component,
			Priority:      repo.Priority,
			AllowPackages: repo.AllowPackages,
		})
	}

	// If no valid repositories were found (all were placeholders), return empty package list
	if len(repoList) == 0 {
		return []ospackage.PackageInfo{}, nil
	}

	userRepo, err := BuildRepoConfigs(repoList, Architecture)
	if err != nil {
		return nil, fmt.Errorf("building user repo configs failed: %w", err)
	}

	var allUserPackages []ospackage.PackageInfo
	for _, rpItx := range userRepo {

		userPkgs, err := ParseRepositoryMetadata(rpItx.PkgPrefix, rpItx.PkgList, rpItx.ReleaseFile, rpItx.ReleaseSign, rpItx.PbGPGKey, rpItx.BuildPath, rpItx.Arch, rpItx.AllowPackages)
		if err != nil {
			return nil, fmt.Errorf("parsing user repo failed: %w", err)
		}
		allUserPackages = append(allUserPackages, userPkgs...)
	}

	return allUserPackages, nil
}

// CheckFileExists sends a HEAD request to the given URL and
// returns true if the file exists (status 200).
// Optimized to handle timeouts and slow server responses.
func checkFileExists(url string) (bool, error) {
	// Create a context with timeout for the request
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := network.NewSecureHTTPClient()

	// Create request with context
	req, err := http.NewRequestWithContext(ctx, "HEAD", url, nil)
	if err != nil {
		return false, fmt.Errorf("creating request for %s: %w", url, err)
	}

	// Set additional headers to encourage faster responses
	req.Header.Set("User-Agent", "os-image-composer/1.0")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Connection", "close") // Don't keep connection alive for HEAD requests

	resp, err := client.Do(req)
	if err != nil {
		// Handle common network errors as "not found" to avoid failing the entire operation
		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "eof") ||
			strings.Contains(errStr, "timeout") ||
			strings.Contains(errStr, "connection refused") ||
			strings.Contains(errStr, "no such host") ||
			strings.Contains(errStr, "context deadline exceeded") {
			return false, nil // Treat network issues as "file not found"
		}
		return false, fmt.Errorf("network error checking %s: %w", url, err)
	}
	defer func() {
		// Properly drain and close the response body to avoid connection leaks
		if resp.Body != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	}()

	switch {
	case resp.StatusCode == http.StatusOK:
		// File exists, all good
		return true, nil
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		// Client errors (404, 403, etc.) - treat as file not found
		return false, nil
	case resp.StatusCode >= 500:
		// Server errors - treat as temporary issue, file might exist
		return false, fmt.Errorf("server error checking file at %s: status %s", url, resp.Status)
	default:
		// Unexpected status codes
		return false, fmt.Errorf("unexpected response checking file at %s: status %s", url, resp.Status)
	}
}

// Validate verifies the downloaded files
func Validate(destDir string, downloadPkgList []string) error {
	log := logger.Logger()

	// get all DEBs in the destDir
	debPattern := filepath.Join(destDir, "*.deb")
	debPaths, err := filepath.Glob(debPattern)
	if err == nil {
		// Filter debPaths to only those in downloadPkgList
		downloadSet := make(map[string]struct{}, len(downloadPkgList))
		for _, name := range downloadPkgList {
			downloadSet[name] = struct{}{}
		}
		var filtered []string
		for _, path := range debPaths {
			if _, ok := downloadSet[filepath.Base(path)]; ok {
				filtered = append(filtered, path)
			}
		}
		debPaths = filtered
	} else {
		return fmt.Errorf("glob %q: %w", debPattern, err)
	}
	// If no DEBs found, log a warning and return
	if len(debPaths) == 0 {
		log.Warn("no DEBs found to verify")
		return nil
	}

	// Create a simple dictionary (map) to store all records from PkgChecksum
	checksumMap := make(map[string][]string)
	for _, pc := range PkgChecksum {
		checksumMap[pc.Name] = append(checksumMap[pc.Name], pc.Checksum)
	}

	start := time.Now()
	results := VerifyDEBs(debPaths, checksumMap, 4)
	log.Infof("Debian verification took %s", time.Since(start))

	// Check results
	for _, r := range results {
		if !r.OK {
			return fmt.Errorf("deb %s failed verification: %w", r.Path, r.Error)
		}
	}
	log.Info("all DEBs verified successfully")

	return nil
}

// Resolve resolves dependencies
func Resolve(req []ospackage.PackageInfo, all []ospackage.PackageInfo) ([]ospackage.PackageInfo, error) {
	log := logger.Logger()

	log.Infof("resolving dependencies for %d DEBIANs", len(req))
	// Resolve all the required dependencies for the initial seed of Debian packages
	needed, err := ResolveDependencies(req, all)
	if err != nil {
		log.Debugf("resolving dependencies failed: %v", err)
		return nil, fmt.Errorf("resolving dependencies failed: %w", err)
	}

	log.Infof("requested %d packages, resolved to %d packages", len(req), len(needed))
	log.Infof("need a total of %d DEBs (including dependencies)", len(needed))

	for _, pkg := range needed {
		log.Debugf("%s %s -> %s", pkg.Name, pkg.Version, filepath.Base(pkg.URL))
	}

	// Adding full packages to the pkgChecksum list
	for _, pkg := range all {
		var sha256 string
		for _, c := range pkg.Checksums {
			if strings.EqualFold(c.Algorithm, "SHA256") {
				sha256 = c.Value
				break
			}
		}
		PkgChecksum = append(PkgChecksum, pkgChecksum{
			Name:     filepath.Base(pkg.URL),
			Checksum: sha256,
		})
	}

	return needed, nil
}

// MatchRequested matches requested packages
func MatchRequested(requests []string, all []ospackage.PackageInfo) ([]ospackage.PackageInfo, error) {
	log := logger.Logger()

	var out []ospackage.PackageInfo
	var requestedPkgs []string
	gotMissingPkg := false

	for _, want := range requests {
		if pkg, found := ResolveTopPackageConflicts(want, all); found {
			out = append(out, pkg)
		} else {
			requestedPkgs = append(requestedPkgs, want)
			log.Warnf("requested package '%q' not found in repo", want)
			gotMissingPkg = true
		}
	}

	log.Infof("found %d packages in request of %d", len(out), len(requests))
	if gotMissingPkg {
		report, err := WriteArrayToFile(requestedPkgs, "Missing Requested Packages")
		if err != nil {
			return out, fmt.Errorf("writing missing packages report failed: %w", err)
		}
		return out, fmt.Errorf("one or more requested packages not found. See list in %s", report)
	}
	return out, nil
}

// WriteArrayToFile writes the contents of arr to a JSON file.
// The file will contain a report_type and a "missing" array of strings.
// The filename will be prefixed with the current date and time in "YYYYMMDD_HHMMSS_" format.
func WriteArrayToFile(arr []string, title string) (string, error) {
	now := time.Now()
	if err := os.MkdirAll(ReportPath, 0755); err != nil {
		return "", fmt.Errorf("creating base path: %w", err)
	}
	filename := filepath.Join(ReportPath, fmt.Sprintf("%s_%s.json", strings.ReplaceAll(title, " ", "_"), now.Format("20060102_150405")))

	// Ensure "report_type" is the first key in the output
	type reportStruct struct {
		ReportType string   `json:"report_type"`
		Missing    []string `json:"missing"`
	}

	report := reportStruct{
		ReportType: "missing_packages_report",
		Missing:    arr,
	}

	file, err := os.Create(filename)
	if err != nil {
		return "", fmt.Errorf("creating file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return "", fmt.Errorf("writing json: %w", err)
	}

	return filename, nil
}

// DownloadPackages downloads packages and returns the list of downloaded package names.
func DownloadPackages(pkgList []string, destDir, dotFile string, pkgSources map[string]config.PackageSource, systemRootsOnly bool) ([]string, error) {
	downloadedPkgs, _, err := DownloadPackagesComplete(pkgList, destDir, dotFile, pkgSources, systemRootsOnly)
	return downloadedPkgs, err
}

func DownloadPackagesComplete(pkgList []string, destDir, dotFile string, pkgSources map[string]config.PackageSource, systemRootsOnly bool) ([]string, []ospackage.PackageInfo, error) {
	var downloadPkgList []string

	log := logger.Logger()

	// Fetch the entire base package list from multiple repositories if configured
	var all []ospackage.PackageInfo
	var err error

	if len(RepoCfgs) > 0 {
		// Use multiple repositories
		log.Infof("Using multiple repositories (%d configured)", len(RepoCfgs))
		all, err = PackagesFromMultipleRepos()
	} else {
		// Fall back to single repository
		log.Infof("Using single repository (legacy mode)")
		all, err = Packages()
	}

	if err != nil {
		return downloadPkgList, nil, fmt.Errorf("getting packages: %w", err)
	}

	// Fetch the entire user repos package list
	userpkg, err := UserPackages()
	if err != nil {
		log.Debugf("getting user packages failed: %v", err)
		return downloadPkgList, nil, fmt.Errorf("user package fetch failed: %w", err)
	}
	all = append(all, userpkg...)

	// Match the packages in the template against all the packages
	req, err := MatchRequested(pkgList, all)
	if err != nil {
		return downloadPkgList, nil, fmt.Errorf("matching packages: %w", err)
	}
	log.Infof("matched a total of %d packages", len(req))

	// Resolve the dependencies of the requested packages
	needed, err := Resolve(req, all)
	if err != nil {
		return downloadPkgList, nil, fmt.Errorf("resolving packages: %w", err)
	}
	log.Infof("resolved %d packages", len(needed))

	sorted_pkgs, err := pkgsorter.SortPackages(needed)
	if err != nil {
		log.Debugf("sorting packages: %w", err)
	}
	log.Infof("sorted %d packages for installation", len(sorted_pkgs))

	// If a dot file is specified, generate the dependency graph
	if dotFile != "" {
		graphPkgs := needed
		if systemRootsOnly {
			graphPkgs = dotfilter.FilterPackagesForDot(needed, pkgSources, true)
		}
		if err := GenerateDot(graphPkgs, dotFile, pkgSources); err != nil {
			log.Debugf("generating dot file: %w", err)
		}
	}

	// Extract URLs
	urls := make([]string, len(sorted_pkgs))
	for i, pkg := range sorted_pkgs {
		urls[i] = pkg.URL
		downloadPkgList = append(downloadPkgList, filepath.Base(pkg.URL))
	}

	// Ensure dest directory exists
	absDestDir, err := filepath.Abs(destDir)
	if err != nil {
		return downloadPkgList, nil, fmt.Errorf("resolving cache directory: %w", err)
	}
	if err := os.MkdirAll(absDestDir, 0755); err != nil {
		return downloadPkgList, nil, fmt.Errorf("creating cache directory %s: %w", absDestDir, err)
	}

	// Download packages using configured workers and cache directory
	log.Infof("downloading %d packages to %s using %d workers", len(urls), absDestDir, config.Workers())
	if err := pkgfetcher.FetchPackages(urls, absDestDir, config.Workers()); err != nil {
		return downloadPkgList, nil, fmt.Errorf("fetch failed: %w", err)
	}
	log.Info("all downloads complete")

	// Verify downloaded packages
	if err := Validate(destDir, downloadPkgList); err != nil {
		return downloadPkgList, nil, fmt.Errorf("verification failed: %w", err)
	}

	return downloadPkgList, needed, nil
}
