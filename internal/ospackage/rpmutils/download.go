package rpmutils

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"

	"github.com/open-edge-platform/os-image-composer/internal/config"
	"github.com/open-edge-platform/os-image-composer/internal/ospackage"
	"github.com/open-edge-platform/os-image-composer/internal/ospackage/dotfilter"
	"github.com/open-edge-platform/os-image-composer/internal/ospackage/pkgfetcher"
	"github.com/open-edge-platform/os-image-composer/internal/ospackage/pkgsorter"
	"github.com/open-edge-platform/os-image-composer/internal/utils/logger"
	"github.com/open-edge-platform/os-image-composer/internal/utils/network"
)

// repoConfig holds .repo file values
type RepoConfig struct {
	Section      string // raw section header
	Name         string // human-readable name from name=
	URL          string
	Path         string
	GPGCheck     bool
	RepoGPGCheck bool
	Enabled      bool
	GPGKey       string
}

var (
	RepoCfg  RepoConfig
	GzHref   string
	UserRepo []config.PackageRepository
	Dist     string
)

func Packages() ([]ospackage.PackageInfo, error) {
	log := logger.Logger()
	log.Infof("fetching packages from %s", RepoCfg.URL)

	packages, err := ParseRepositoryMetadata(RepoCfg.URL, GzHref, nil)
	if err != nil {
		log.Errorf("parsing primary.xml.gz failed: %v", err)
		return nil, err
	}

	log.Infof("found %d packages in rpm repo", len(packages))
	return packages, nil
}

func LocalUserPackages() ([]ospackage.PackageInfo, func(), error) {
	log := logger.Logger()
	log.Infof("fetching packages from local user package list")

	var allLocalPackages []ospackage.PackageInfo
	var cleanups []func()
	combinedCleanup := func() {
		for _, fn := range cleanups {
			fn()
		}
	}

	for i, repo := range UserRepo {
		if repo.Path == "" {
			continue
		}

		repoName := fmt.Sprintf("rpmlocrepo%d", i+1)
		var repoURL string

		// Check if it's already a proper repository with repodata metadata
		repoMetaDataPath := filepath.Join(repo.Path, "repodata/repomd.xml")
		if _, err := os.Stat(repoMetaDataPath); err != nil {
			if os.IsNotExist(err) {
				// Not a proper repo - copy RPMs, generate metadata, and serve over HTTP
				_, tempURL, cleanup, err := CreateTemporaryRepository(repo.Path, repoName)
				if err != nil {
					combinedCleanup()
					return nil, nil, fmt.Errorf("failed to create temporary RPM repository for %s: %w", repo.Path, err)
				}
				cleanups = append(cleanups, cleanup)
				repoURL = tempURL
			} else {
				combinedCleanup()
				return nil, nil, fmt.Errorf("failed to access local RPM repository metadata %s: %w", repoMetaDataPath, err)
			}
		} else {
			// Already a proper repo - serve it directly over HTTP
			tempURL, serverCleanup, err := network.ServeRepositoryHTTP(repo.Path)
			if err != nil {
				combinedCleanup()
				return nil, nil, fmt.Errorf("failed to serve local RPM repository %s via HTTP: %w", repo.Path, err)
			}
			cleanups = append(cleanups, serverCleanup)
			repoURL = tempURL
		}

		repomdURL := repoURL + "/repodata/repomd.xml"
		primaryXmlURL, err := FetchPrimaryURL(repomdURL)
		if err != nil {
			combinedCleanup()
			return nil, nil, fmt.Errorf("fetching primary XML URL from %s failed: %w", repomdURL, err)
		}

		localPkgs, err := ParseRepositoryMetadata(repoURL, primaryXmlURL, repo.AllowPackages)
		if err != nil {
			combinedCleanup()
			return nil, nil, fmt.Errorf("parsing local RPM repository %s failed: %w", repo.Path, err)
		}
		allLocalPackages = append(allLocalPackages, localPkgs...)
	}

	return allLocalPackages, combinedCleanup, nil
}

func UserPackages() ([]ospackage.PackageInfo, error) {
	log := logger.Logger()
	log.Infof("fetching packages from %s", "user package list")

	repoList := make([]struct {
		id            string
		codename      string
		url           string
		path          string
		pkey          string
		pkeys         []string
		allowPackages []string
	}, 0, len(UserRepo))
	for i, repo := range UserRepo {
		if repo.URL == "" || repo.URL == "<URL>" {
			continue
		}

		repoList = append(repoList, struct {
			id            string
			codename      string
			url           string
			path          string
			pkey          string
			pkeys         []string
			allowPackages []string
		}{
			id:            fmt.Sprintf("rpmcustrepo%d", i+1),
			codename:      repo.Codename,
			url:           repo.URL,
			path:          repo.Path,
			pkey:          repo.PKey,
			pkeys:         repo.PKeys,
			allowPackages: repo.AllowPackages,
		})
	}

	type RepoConfigWithPackages struct {
		RepoConfig
		AllowPackages []string
	}

	var userRepo []RepoConfigWithPackages
	for _, repoItem := range repoList {
		id := repoItem.id
		codename := repoItem.codename
		baseURL := repoItem.url
		path := repoItem.path
		allowPackages := repoItem.allowPackages

		allKeys := repoItem.pkeys
		if repoItem.pkey != "" {
			allKeys = append([]string{repoItem.pkey}, allKeys...)
		}
		gpgKey := strings.Join(allKeys, ",")

		repo := RepoConfigWithPackages{
			RepoConfig: RepoConfig{
				Name:         id,
				GPGCheck:     true,
				RepoGPGCheck: true,
				Enabled:      true,
				GPGKey:       gpgKey,
				URL:          baseURL,
				Path:         path,
				Section:      fmt.Sprintf("[%s]", codename),
			},
			AllowPackages: allowPackages,
		}

		userRepo = append(userRepo, repo)
	}

	metadataXmlPath := "repodata/repomd.xml"
	var allUserPackages []ospackage.PackageInfo
	for _, rpItx := range userRepo {
		repoMetaDataURL := GetRepoMetaDataURL(rpItx.URL, metadataXmlPath)
		if repoMetaDataURL == "" {
			log.Errorf("invalid repo metadata URL: %s/%s, skipping", rpItx.URL, metadataXmlPath)
			continue
		}

		primaryXmlURL, err := FetchPrimaryURL(repoMetaDataURL)
		if err != nil {
			return nil, fmt.Errorf("fetching %s URL failed: %w", repoMetaDataURL, err)
		}

		userPkgs, err := ParseRepositoryMetadata(rpItx.URL, primaryXmlURL, rpItx.AllowPackages)
		if err != nil {
			return nil, fmt.Errorf("parsing user repo failed: %w", err)
		}
		allUserPackages = append(allUserPackages, userPkgs...)
	}

	return allUserPackages, nil
}

// isBinaryGPGKey checks if the data appears to be a binary GPG key
func isBinaryGPGKey(data []byte) bool {
	// Check for ASCII armored format first
	if bytes.HasPrefix(data, []byte("-----BEGIN PGP PUBLIC KEY BLOCK-----")) {
		return false // This is ASCII armored, not binary
	}

	// Try to parse as OpenPGP packet to determine if it's binary
	reader := bytes.NewReader(data)
	_, err := openpgp.ReadKeyRing(reader)
	if err == nil {
		return true // Successfully parsed as binary OpenPGP
	}

	// Additional heuristic: if it contains mostly non-printable characters
	if len(data) < 4 {
		return false
	}

	printableCount := 0
	checkLength := len(data)
	if checkLength > 100 {
		checkLength = 100
	}

	for i := 0; i < checkLength; i++ {
		if data[i] >= 32 && data[i] <= 126 {
			printableCount++
		}
	}

	// If less than 70% printable characters, likely binary
	return float64(printableCount)/float64(checkLength) < 0.7
}

// convertBinaryGPGToAscii converts binary GPG key to ASCII armored format using Go crypto
func convertBinaryGPGToAscii(binaryData []byte) ([]byte, error) {
	// Try to parse the binary data as an OpenPGP key ring
	reader := bytes.NewReader(binaryData)
	keyRing, err := openpgp.ReadKeyRing(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to parse binary GPG key: %w", err)
	}

	var armoredBuf bytes.Buffer

	// Create ASCII armor encoder
	armorWriter, err := armor.Encode(&armoredBuf, openpgp.PublicKeyType, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create armor encoder: %w", err)
	}

	// Serialize each entity in the keyring
	for _, entity := range keyRing {
		if err := entity.Serialize(armorWriter); err != nil {
			armorWriter.Close()
			return nil, fmt.Errorf("failed to serialize key entity: %w", err)
		}
	}

	if err := armorWriter.Close(); err != nil {
		return nil, fmt.Errorf("failed to close armor encoder: %w", err)
	}

	return armoredBuf.Bytes(), nil
} // createTempGPGKeyFiles downloads multiple GPG keys from URLs and creates temporary files.
// Returns the file paths and a cleanup function. The caller is responsible for calling cleanup.
func createTempGPGKeyFiles(gpgKeyURLs []string) (keyPaths []string, cleanup func(), err error) {
	log := logger.Logger()

	if len(gpgKeyURLs) == 0 {
		return nil, nil, fmt.Errorf("no GPG key URLs provided")
	}

	var tempFiles []*os.File
	var filePaths []string

	client := network.NewSecureHTTPClient()

	// Download and create temp files for each GPG key
	for i, gpgKeyURL := range gpgKeyURLs {

		if gpgKeyURL == "<PUBLIC_KEY_URL>" || gpgKeyURL == "" || gpgKeyURL == "[trusted=yes]" {
			log.Warnf("GPG key URL %d is empty or marked as trusted, skipping", i+1)
			continue
		}

		// Check if the GPG key URL is a binary file (ends with .gpg or .bin)
		isBinary := strings.HasSuffix(strings.ToLower(gpgKeyURL), ".gpg") || strings.HasSuffix(strings.ToLower(gpgKeyURL), ".bin")

		resp, err := client.Get(gpgKeyURL)
		if err != nil {
			// Cleanup any files created so far
			for _, f := range tempFiles {
				f.Close()
				os.Remove(f.Name())
			}
			return nil, nil, fmt.Errorf("fetch GPG key %s: %w", gpgKeyURL, err)
		}

		keyBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			// Cleanup any files created so far
			for _, f := range tempFiles {
				f.Close()
				os.Remove(f.Name())
			}
			return nil, nil, fmt.Errorf("read GPG key body from %s: %w", gpgKeyURL, err)
		}

		// If it's a binary GPG key, we need to handle it differently
		if isBinary {
			log.Infof("GPG key %s is binary format, checking if conversion is needed", gpgKeyURL)

			// Check if the downloaded data is actually binary
			if isBinaryGPGKey(keyBytes) {
				log.Infof("Converting binary GPG key to ASCII armored format")
				convertedBytes, err := convertBinaryGPGToAscii(keyBytes)
				if err != nil {
					log.Warnf("Failed to convert binary GPG key to ASCII: %v, using original data", err)
				} else {
					keyBytes = convertedBytes
					log.Infof("Successfully converted binary GPG key to ASCII armored format")
				}
			} else {
				log.Infof("GPG key data appears to be ASCII armored already")
			}
		}

		log.Infof("fetched GPG key %d (%d bytes) from %s", i+1, len(keyBytes), gpgKeyURL)

		// Create temp file with unique pattern
		tmp, err := os.CreateTemp("", fmt.Sprintf("rpm-gpg-%d-*.asc", i))
		if err != nil {
			// Cleanup any files created so far
			for _, f := range tempFiles {
				f.Close()
				os.Remove(f.Name())
			}
			return nil, nil, fmt.Errorf("create temp key file %d: %w", i, err)
		}

		if _, err := tmp.Write(keyBytes); err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			// Cleanup any files created so far
			for _, f := range tempFiles {
				f.Close()
				os.Remove(f.Name())
			}
			return nil, nil, fmt.Errorf("write key to temp file %d: %w", i, err)
		}

		tempFiles = append(tempFiles, tmp)
		filePaths = append(filePaths, tmp.Name())
	}

	cleanup = func() {
		for _, f := range tempFiles {
			f.Close()
			os.Remove(f.Name())
		}
	}

	return filePaths, cleanup, nil
}

func Validate(destDir string) error {
	log := logger.Logger()

	localRepoRPMNames := make(map[string]struct{})
	for _, userRepo := range UserRepo {
		if userRepo.Path == "" {
			continue
		}

		localRPMs, err := filepath.Glob(filepath.Join(userRepo.Path, "*.rpm"))
		if err != nil {
			return fmt.Errorf("glob local repo RPMs in %s: %w", userRepo.Path, err)
		}

		for _, rpmPath := range localRPMs {
			localRepoRPMNames[filepath.Base(rpmPath)] = struct{}{}
		}
	}

	rpmPattern := filepath.Join(destDir, "*.rpm")
	rpmPaths, err := filepath.Glob(rpmPattern)
	if err != nil {
		return fmt.Errorf("glob %q: %w", rpmPattern, err)
	}

	verifiableRPMPaths := make([]string, 0, len(rpmPaths))
	skippedLocalRPMs := 0
	for _, rpmPath := range rpmPaths {
		if _, isLocal := localRepoRPMNames[filepath.Base(rpmPath)]; isLocal {
			skippedLocalRPMs++
			continue
		}

		verifiableRPMPaths = append(verifiableRPMPaths, rpmPath)
	}

	if skippedLocalRPMs > 0 {
		log.Infof("skipping verification for %d local-repo RPM(s)", skippedLocalRPMs)
	}

	if len(rpmPaths) > 0 && len(verifiableRPMPaths) == 0 {
		log.Info("no non-local RPMs to verify")
		return nil
	}

	// Collect all GPG key URLs (could be from RepoCfg and UserRepo)
	var gpgKeyURLs []string

	// Add main repo GPG key
	if RepoCfg.GPGKey != "" {
		gpgKeyURLs = append(gpgKeyURLs, splitGPGKeyURLs(RepoCfg.GPGKey)...)
	}

	// Add user repo GPG keys
	for _, userRepo := range UserRepo {
		if userRepo.Path != "" {
			continue
		}

		// Collect keys from both PKey (string) and PKeys (array)
		var userKeys []string

		if userRepo.PKey != "" {
			userKeys = append(userKeys, splitGPGKeyURLs(userRepo.PKey)...)
		}
		if len(userRepo.PKeys) > 0 {
			userKeys = append(userKeys, userRepo.PKeys...)
		}

		if len(userKeys) == 0 {
			return fmt.Errorf("no GPG key URL configured for user repo: %s", userRepo.URL)
		}

		gpgKeyURLs = append(gpgKeyURLs, userKeys...)
	}

	if len(gpgKeyURLs) == 0 {
		return fmt.Errorf("no GPG keys configured for verification")
	}

	// If every configured key is the [trusted=yes] sentinel, skip RPM signature verification.
	allTrusted := true
	for _, url := range gpgKeyURLs {
		if url != "[trusted=yes]" {
			allTrusted = false
			break
		}
	}
	if allTrusted {
		log.Infof("all repositories are marked [trusted=yes], skipping RPM signature verification")
		return nil
	}

	// Create temporary GPG key files
	gpgKeyPaths, cleanup, err := createTempGPGKeyFiles(gpgKeyURLs)
	if err != nil {
		return fmt.Errorf("failed to create temp GPG key files: %w", err)
	}
	defer cleanup()

	log.Infof("created %d temporary GPG key files for verification", len(gpgKeyPaths))

	if len(rpmPaths) == 0 {
		log.Warn("no RPMs found to verify")
		return nil
	}

	start := time.Now()
	results := VerifyAll(verifiableRPMPaths, gpgKeyPaths, 4)
	log.Infof("RPM verification took %s", time.Since(start))

	// Check results
	for _, r := range results {
		if !r.OK {
			return fmt.Errorf("RPM %s failed verification: %v", r.Path, r.Error)
		}
	}
	log.Info("all RPMs verified successfully")

	return nil
}

func splitGPGKeyURLs(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	})

	urls := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			urls = append(urls, part)
		}
	}

	return urls
}

func Resolve(req []ospackage.PackageInfo, all []ospackage.PackageInfo) ([]ospackage.PackageInfo, error) {
	log := logger.Logger()

	log.Infof("resolving dependencies for %d RPMs", len(req))

	// Resolve all the required dependencies for the initial seed of RPMs
	needed, err := ResolveDependencies(req, all)
	if err != nil {
		log.Errorf("resolving dependencies failed: %v", err)
		return nil, err
	}
	log.Infof("need a total of %d RPMs (including dependencies)", len(needed))

	for _, pkg := range needed {
		log.Debugf("-> %s", pkg.Name)
	}

	return needed, nil
}

func isRPMRequirementInCache(required string, cachedPackageNames map[string]struct{}) bool {
	required = strings.TrimSpace(extractBaseNameFromDep(required))
	if required == "" {
		return true
	}

	for cachedName := range cachedPackageNames {
		if matchesPackageFilter(cachedName, []string{required}) {
			return true
		}
		if matchesPackageFilter(required, []string{cachedName}) {
			return true
		}
	}

	return false
}

func isRPMPackageCacheOutdated(requiredPackages []string, cacheDir string) (bool, []string, []string, error) {
	pattern := filepath.Join(cacheDir, "*.rpm")
	cachedPaths, err := filepath.Glob(pattern)
	if err != nil {
		return false, nil, nil, fmt.Errorf("glob %q: %w", pattern, err)
	}

	cachedPackageNames := make(map[string]struct{}, len(cachedPaths))
	cachedFiles := make([]string, 0, len(cachedPaths))
	for _, p := range cachedPaths {
		base := filepath.Base(p)
		cachedFiles = append(cachedFiles, base)
		cachedPackageNames[extractBasePackageNameFromFile(base)] = struct{}{}
	}

	missingSet := make(map[string]struct{})
	var missing []string
	for _, req := range requiredPackages {
		req = strings.TrimSpace(req)
		if req == "" {
			continue
		}
		if isRPMRequirementInCache(req, cachedPackageNames) {
			continue
		}
		if _, seen := missingSet[req]; seen {
			continue
		}
		missingSet[req] = struct{}{}
		missing = append(missing, req)
	}

	return len(missing) > 0, missing, cachedFiles, nil
}

// clearRPMMetadataCache removes primary.parsed.json and primary.location.json
// from the metadata cache directory derived from the configured repo URL so that
// repository metadata is re-fetched on the next run.
func clearRPMMetadataCache() {
	log := logger.Logger()

	if RepoCfg.URL == "" {
		return
	}

	metaDir, err := rpmMetadataCacheDir(RepoCfg.URL)
	if err != nil {
		log.Warnf("failed to resolve RPM metadata cache directory: %v", err)
		return
	}

	for _, name := range []string{"primary.parsed.json", "primary.location.json"} {
		f := filepath.Join(metaDir, name)
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			log.Warnf("failed to remove RPM metadata cache %s: %v", f, err)
			continue
		}
		log.Infof("removed RPM metadata cache: %s", f)
	}
}

// clearRPMPackageCache removes all .rpm files from cacheDir and invalidates
// the repository metadata cache (primary.parsed.json, primary.location.json)
// so that a full re-download including fresh repository metadata is performed
// on the next run.
func clearRPMPackageCache(cacheDir string) error {
	log := logger.Logger()
	pattern := filepath.Join(cacheDir, "*.rpm")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("glob %q: %w", pattern, err)
	}
	for _, f := range files {
		if err := os.Remove(f); err != nil {
			return fmt.Errorf("removing cached file %s: %w", f, err)
		}
		log.Debugf("removed stale cached file: %s", filepath.Base(f))
	}
	log.Infof("cleared %d stale RPM files from cache directory %s", len(files), cacheDir)
	clearRPMMetadataCache()
	return nil
}

func buildRPMPackageInfosFromCache(cacheDir string, cachedFiles []string) []ospackage.PackageInfo {
	infos := make([]ospackage.PackageInfo, 0, len(cachedFiles))
	for _, file := range cachedFiles {
		infos = append(infos, ospackage.PackageInfo{
			Name: extractBasePackageNameFromFile(file),
			Type: "rpm",
			URL:  filepath.Join(cacheDir, file),
		})
	}
	return infos
}

// DownloadPackages downloads packages and returns the list of downloaded package names.
func DownloadPackages(pkgList []string, destDir, dotFile string, pkgSources map[string]config.PackageSource, systemRootsOnly bool) ([]string, error) {
	downloadedPkgs, _, err := DownloadPackagesComplete(pkgList, destDir, dotFile, pkgSources, systemRootsOnly)
	return downloadedPkgs, err
}

// DownloadPackagesComplete downloads packages and returns both package names and full package info.
func DownloadPackagesComplete(pkgList []string, destDir, dotFile string, pkgSources map[string]config.PackageSource, systemRootsOnly bool) ([]string, []ospackage.PackageInfo, error) {
	var downloadPkgList []string

	log := logger.Logger()
	absDestDir, err := filepath.Abs(destDir)
	if err != nil {
		return downloadPkgList, nil, fmt.Errorf("resolving cache directory: %v", err)
	}

	if len(pkgList) > 0 {
		cacheOutdated, missingRequired, cachedFiles, cacheErr := isRPMPackageCacheOutdated(pkgList, absDestDir)
		if cacheErr != nil {
			log.Warnf("Failed to evaluate RPM package cache state: %v", cacheErr)
		} else if !cacheOutdated {
			log.Infof("RPM package cache is up-to-date; all %d required packages are available locally", len(pkgList))
			return cachedFiles, buildRPMPackageInfosFromCache(absDestDir, cachedFiles), nil
		} else if len(missingRequired) > 0 {
			log.Infof("RPM package cache is outdated; missing required packages: %v", missingRequired)
			log.Infof("Keeping existing cached RPM files and continuing to fetch only missing/new packages")
		}
	}

	// Fetch the entire package list
	all, err := Packages()
	if err != nil {
		log.Errorf("base packages fetch failed: %v", err)
		return downloadPkgList, nil, fmt.Errorf("base package fetch failed: %v", err)
	}

	// Fetch the entire user repos package list
	userpkg, err := UserPackages()
	if err != nil {
		log.Errorf("getting user packages failed: %v", err)
		return downloadPkgList, nil, fmt.Errorf("user package fetch failed: %w", err)
	}
	all = append(all, userpkg...)

	// Adding local repo packages
	localRepoPkgs, localRepoCleanup, err := LocalUserPackages()
	if err != nil {
		log.Errorf("getting local repo packages failed: %v", err)
		return downloadPkgList, nil, fmt.Errorf("local repo package fetch failed: %w", err)
	}
	if localRepoCleanup != nil {
		defer localRepoCleanup()
	}
	all = append(all, localRepoPkgs...)

	// Adjust package names to remove any prefixes before PkgName - Azure Linux RPM repos often prefix package file names
	for i := range all {
		// Find where the package name starts in the full name
		if idx := strings.Index(all[i].Name, all[i].PkgName); idx > 0 {
			// Remove the prefix by taking substring from where PkgName starts
			all[i].Name = all[i].Name[idx:]
		}
		// If PkgName is not found or is at the beginning, keep the original Name
	}

	// Match the packages in the template against all the packages
	req, err := MatchRequested(pkgList, all)
	if err != nil {
		return downloadPkgList, nil, fmt.Errorf("matching packages: %v", err)
	}
	log.Infof("Matched a total of %d packages", len(req))

	for _, pkg := range req {
		log.Debugf("-> %s", pkg.Name)
	}

	// Resolve the dependencies of the requested packages
	needed, err := Resolve(req, all)
	if err != nil {
		return downloadPkgList, nil, fmt.Errorf("resolving packages: %v", err)
	}

	sorted_pkgs, err := pkgsorter.SortPackages(needed)
	if err != nil {
		log.Errorf("sorting packages: %v", err)
	}
	log.Infof("Sorted %d packages for installation", len(sorted_pkgs))

	// If a dot file is specified, generate the dependency graph
	if dotFile != "" {
		graphPkgs := sorted_pkgs
		if systemRootsOnly {
			graphPkgs = dotfilter.FilterPackagesForDot(sorted_pkgs, pkgSources, true)
		}
		if err := GenerateDot(graphPkgs, dotFile, pkgSources); err != nil {
			log.Errorf("generating dot file: %v", err)
		}
	}

	// Extract URLs
	urls := make([]string, len(sorted_pkgs))
	for i, pkg := range sorted_pkgs {
		urls[i] = pkg.URL
		downloadPkgList = append(downloadPkgList, path.Base(pkg.URL))
	}

	// Ensure dest directory exists
	if err := os.MkdirAll(absDestDir, 0755); err != nil {
		return downloadPkgList, nil, fmt.Errorf("creating cache directory %s: %v", absDestDir, err)
	}

	// Download packages using configured workers and cache directory
	log.Infof("Downloading %d packages to %s using %d workers", len(urls), absDestDir, config.Workers())
	if err := pkgfetcher.FetchPackages(urls, absDestDir, config.Workers()); err != nil {
		return downloadPkgList, nil, fmt.Errorf("fetch failed: %v", err)
	}
	log.Info("All downloads complete")

	// Verify downloaded packages
	if err := Validate(destDir); err != nil {
		return downloadPkgList, nil, fmt.Errorf("verification failed: %v", err)
	}

	return downloadPkgList, needed, nil
}
