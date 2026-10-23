package rpmutils

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage/dotfilter"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage/pkgfetcher"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage/pkgsorter"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/network"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/runctx"
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

// repoOrigin associates a repository's actual package-download base URL with
// the raw GPG key value(s) configured for it (each entry may be a real key
// URL or the "[trusted=yes]" sentinel). ValidateOrigins uses it to scope a
// repository's "[trusted=yes]" opt-out to only the RPMs that were actually
// downloaded from that repository, instead of applying it to every
// downloaded RPM regardless of where it came from.
type repoOrigin struct {
	baseURL string
	keys    []string
}

var (
	RepoCfg        RepoConfig
	GzHref         string
	UserRepo       []config.PackageRepository
	Dist           string
	KernelVersion  string
	KernelPackages = make(map[string]struct{})

	isRPMPackageCacheOutdatedFunc = isRPMPackageCacheOutdated
	clearRPMMetadataCacheFunc     = clearRPMMetadataCache
	refreshRPMPrimaryFunc         = refreshRPMPrimaryReferences
	validateOriginsFunc           = validateSelectedOrigins

	// localRepoOrigins is populated by LocalUserPackages with the actual URL
	// each Path-based user repository's packages were served (and therefore
	// downloaded) from. That URL is generated fresh on every call — e.g. an
	// ephemeral local HTTP server — so it cannot be reconstructed from
	// UserRepo alone the way a remote repository's own configured URL can.
	localRepoOrigins []repoOrigin
)

// repoRawKeys merges a repository's single PKey and PKeys list into one
// ordered slice of raw GPG key values (URLs or the "[trusted=yes]" sentinel).
func repoRawKeys(pkey string, pkeys []string) []string {
	var keys []string
	if pkey != "" {
		keys = append(keys, splitGPGKeyURLs(pkey)...)
	}
	keys = append(keys, pkeys...)
	return keys
}

// ConfigureKernelSelection sets the kernel package requests and version used
// during top-level package matching.
func ConfigureKernelSelection(kernelPackages []string, kernelVersion string) {
	KernelVersion = kernelVersion
	KernelPackages = make(map[string]struct{}, len(kernelPackages))
	for _, pkg := range kernelPackages {
		pkg = strings.TrimSpace(pkg)
		if pkg == "" {
			continue
		}
		KernelPackages[pkg] = struct{}{}
	}
}

func Packages() ([]ospackage.PackageInfo, error) {
	log := logger.Logger()
	log.Infof("fetching packages from %s", redactURLForLog(RepoCfg.URL))

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

	localRepoOrigins = nil

	var allLocalPackages []ospackage.PackageInfo
	var cleanups []func()
	combinedCleanup := func() {
		for _, fn := range cleanups {
			fn()
		}
	}

	for i, repo := range UserRepo {
		repoPath := repo.Path
		if repoPath == "" {
			if len(repo.Packages) == 0 {
				continue
			}
			// auto-create a temp dir when path is not specified but packages are
			tmpPath, err := os.MkdirTemp(config.TempDir(), "ict-localrepo-*")
			if err != nil {
				combinedCleanup()
				return nil, nil, fmt.Errorf("failed to create temporary directory for local repository: %w", err)
			}
			cleanups = append(cleanups, func() { os.RemoveAll(tmpPath) })
			repoPath = tmpPath
		}

		if err := PrepareLocalRepositoryFiles(repoPath, repo.Packages, repo.InsecureSkipVerify); err != nil {
			combinedCleanup()
			return nil, nil, fmt.Errorf("failed to prepare local RPM repository source path %s: %w", repoPath, err)
		}

		repoName := fmt.Sprintf("rpmlocrepo%d", i+1)
		var repoURL string

		// Check if it's already a proper repository with repodata metadata
		repoMetaDataPath := filepath.Join(repoPath, "repodata/repomd.xml")
		if _, err := os.Stat(repoMetaDataPath); err != nil {
			if os.IsNotExist(err) {
				// Not a proper repo - copy RPMs, generate metadata, and serve over HTTP
				_, tempURL, cleanup, err := CreateTemporaryRepository(repoPath, repoName)
				if err != nil {
					combinedCleanup()
					return nil, nil, fmt.Errorf("failed to create temporary RPM repository for %s: %w", repoPath, err)
				}
				cleanups = append(cleanups, cleanup)
				repoURL = tempURL
			} else {
				combinedCleanup()
				return nil, nil, fmt.Errorf("failed to access local RPM repository metadata %s: %w", repoMetaDataPath, err)
			}
		} else {
			// Already a proper repo - serve it directly over HTTP
			tempURL, serverCleanup, err := network.ServeRepositoryHTTP(repoPath)
			if err != nil {
				combinedCleanup()
				return nil, nil, fmt.Errorf("failed to serve local RPM repository %s via HTTP: %w", repoPath, err)
			}
			cleanups = append(cleanups, serverCleanup)
			repoURL = tempURL
		}

		localRepoOrigins = append(localRepoOrigins, repoOrigin{
			baseURL: repoURL,
			keys:    repoRawKeys(repo.PKey, repo.PKeys),
		})

		repomdURL := repoURL + "/repodata/repomd.xml"
		primaryXmlURL, err := FetchPrimaryURL(repomdURL)
		if err != nil {
			combinedCleanup()
			return nil, nil, fmt.Errorf("fetching primary XML URL from %s failed: %w", repomdURL, err)
		}

		localPkgs, err := ParseRepositoryMetadata(repoURL, primaryXmlURL, repo.AllowPackages)
		if err != nil {
			combinedCleanup()
			return nil, nil, fmt.Errorf("parsing local RPM repository %s failed: %w", repoPath, err)
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
			log.Errorf("invalid repo metadata URL: %s/%s, skipping", redactURLForLog(rpItx.URL), metadataXmlPath)
			continue
		}

		primaryXmlURL, err := FetchPrimaryURL(repoMetaDataURL)
		if err != nil {
			return nil, fmt.Errorf("fetching %s URL failed: %w", redactURLForLog(repoMetaDataURL), err)
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
	return createTempGPGKeyFilesUsing(gpgKeyURLs, config.GetCachedOrDownloadGPGKey)
}

// isPlaceholderGPGKeyURL reports whether a configured key value names no key to
// fetch: it is empty, an unfilled template placeholder, or the trusted opt-out.
func isPlaceholderGPGKeyURL(gpgKeyURL string) bool {
	return gpgKeyURL == "<PUBLIC_KEY_URL>" || gpgKeyURL == "" || gpgKeyURL == "[trusted=yes]"
}

// createTempGPGKeyFilesUsing is createTempGPGKeyFiles with the key source
// chosen by the caller, so verification can retry with freshly downloaded keys.
func createTempGPGKeyFilesUsing(gpgKeyURLs []string, fetchKey func(string) ([]byte, error)) (keyPaths []string, cleanup func(), err error) {
	log := logger.Logger()

	if len(gpgKeyURLs) == 0 {
		return nil, nil, fmt.Errorf("no GPG key URLs provided")
	}

	var tempFiles []*os.File
	var filePaths []string

	// Download and create temp files for each GPG key
	for i, gpgKeyURL := range gpgKeyURLs {

		if isPlaceholderGPGKeyURL(gpgKeyURL) {
			log.Warnf("GPG key URL %d is empty or marked as trusted, skipping", i+1)
			continue
		}

		// Check if the GPG key URL is a binary file (ends with .gpg or .bin)
		isBinary := strings.HasSuffix(strings.ToLower(gpgKeyURL), ".gpg") || strings.HasSuffix(strings.ToLower(gpgKeyURL), ".bin")

		keyBytes, err := fetchKey(gpgKeyURL)
		if err != nil {
			for _, f := range tempFiles {
				f.Close()
				os.Remove(f.Name())
			}
			return nil, nil, fmt.Errorf("fetch GPG key %s: %w", config.RedactKeyURL(gpgKeyURL), err)
		}

		// If it's a binary GPG key, we need to handle it differently
		if isBinary {
			log.Infof("GPG key %s is binary format, checking if conversion is needed", config.RedactKeyURL(gpgKeyURL))

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

		log.Infof("fetched GPG key %d (%d bytes) from %s", i+1, len(keyBytes), config.RedactKeyURL(gpgKeyURL))

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

// Validate verifies GPG signatures on every downloaded RPM in destDir. Every
// RPM is checked against the union of all configured repositories' keys — it
// passes if it validates against any one of them — and a repository whose
// keys are all "[trusted=yes]" only skips verification if every configured
// repository is trusted, since without knowing which RPM came from which
// repository there is no safe way to scope the opt-out further.
//
// The production download path uses ValidateOrigins, which scopes each
// repository's "[trusted=yes]" opt-out to only that repository's own RPMs.
// Validate is intentionally retained as the exported entry point for the
// union check when no per-file origin mapping is available, and is the path
// the black-box rpmutils_test suite exercises directly.
func Validate(destDir string) error {
	return validateUnion(destDir)
}

// allKeysTrusted reports whether every raw key value in keys is the
// "[trusted=yes]" sentinel.
func allKeysTrusted(keys []string) bool {
	for _, k := range keys {
		if k != "[trusted=yes]" {
			return false
		}
	}
	return len(keys) > 0
}

func validateUnion(destDir string) error {
	rpmPaths, err := listRPMFiles(destDir)
	if err != nil {
		return err
	}
	return validateUnionPaths(rpmPaths)
}

// listRPMFiles returns the paths of the .rpm files directly inside dir.
// The directory is read literally rather than globbed, since the cache root may
// contain glob metacharacters such as "[".
func listRPMFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing RPMs in %s: %w", dir, err)
	}
	var paths []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".rpm") {
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}
	return paths, nil
}

// validateUnionPaths verifies rpmPaths against the union of all configured keys.
func validateUnionPaths(rpmPaths []string) error {
	log := logger.Logger()

	// Collect all GPG key URLs (could be from RepoCfg and UserRepo)
	var gpgKeyURLs []string

	// Add main repo GPG key
	if RepoCfg.GPGKey != "" {
		gpgKeyURLs = append(gpgKeyURLs, splitGPGKeyURLs(RepoCfg.GPGKey)...)
	}

	// Add user repo GPG keys, including local repos, so their RPMs can still verify
	for _, userRepo := range UserRepo {
		userKeys := repoRawKeys(userRepo.PKey, userRepo.PKeys)
		if len(userKeys) == 0 {
			return fmt.Errorf("no GPG key URL configured for user repo: %s", userRepo.URL)
		}

		gpgKeyURLs = append(gpgKeyURLs, userKeys...)
	}

	if len(gpgKeyURLs) == 0 {
		return fmt.Errorf("no GPG keys configured for verification")
	}

	// If every configured key is the [trusted=yes] sentinel, skip RPM signature verification.
	if allKeysTrusted(gpgKeyURLs) {
		log.Infof("all repositories are marked [trusted=yes], skipping RPM signature verification")
		return nil
	}

	if len(rpmPaths) == 0 {
		// Still resolve the keys, so an unreachable or invalid key is reported.
		_, cleanup, err := createTempGPGKeyFiles(gpgKeyURLs)
		if err != nil {
			return fmt.Errorf("failed to create temp GPG key files: %w", err)
		}
		cleanup()
		log.Warn("no RPMs found to verify")
		return nil
	}

	if err := verifyRPMsRefreshingKeys(rpmPaths, gpgKeyURLs); err != nil {
		return err
	}
	log.Info("all RPMs verified successfully")

	return nil
}

// verifyAllRPMs is the signature check, replaceable in tests; a test that replaces
// it must not run in parallel.
var verifyAllRPMs = VerifyAll

// keyRefresh records how one set of RPMs was verified against its key URLs: the
// cached keys it used, the RPMs that passed with them, and the keys downloaded to
// verify the RPMs that did not. The downloaded keys are published to the cache by
// publishKeyRefreshes, once every set has been verified.
type keyRefresh struct {
	keyURLs   []string
	cached    map[string][]byte
	refreshed map[string][]byte
	failed    []string
	passed    []string
}

// verifyRPMsRefreshingKeys verifies rpmPaths against the keys at keyURLs and
// publishes any refreshed key to the cache.
func verifyRPMsRefreshingKeys(rpmPaths, keyURLs []string) error {
	refresh, err := verifyRPMsWithKeyRefresh(rpmPaths, keyURLs)
	if err != nil {
		return err
	}
	return publishKeyRefreshes([]*keyRefresh{refresh})
}

// verifyRPMsWithKeyRefresh verifies rpmPaths against the keys at keyURLs,
// reading keys from the persistent cache so a cache hit needs no network. If a
// cached key no longer verifies what the repository signs, which happens when
// the repository rotates its signing key at the same URL, the keys are
// downloaded again and the RPMs that failed are verified once more.
//
// Only the RPMs that failed are retried: the others verified with the cached
// keys, so a key that rotated at another URL cannot make them fail. The retry
// uses each freshly downloaded key and, for a key that cannot be downloaded, the
// cached key the first attempt used, so one unreachable URL does not stop the
// others from being refreshed. If nothing was refreshed, for example offline,
// the original verification error is returned.
//
// The cache is not written here. The returned keyRefresh holds the refreshed
// keys for publishKeyRefreshes, which can see every set of RPMs that shares them.
func verifyRPMsWithKeyRefresh(rpmPaths, keyURLs []string) (*keyRefresh, error) {
	log := logger.Logger()

	cached, err := snapshotCachedKeys(keyURLs)
	if err != nil {
		return nil, fmt.Errorf("failed to read the cached GPG keys: %w", err)
	}
	results, setupErr := verifyRPMsWithKeys(rpmPaths, keyURLs, func(keyURL string) ([]byte, error) {
		if keyData, ok := cached[keyURL]; ok {
			return keyData, nil
		}
		// Not cached: download outside the cache lock, which is not reentrant.
		keyData, err := config.GetCachedOrDownloadGPGKey(keyURL)
		if err == nil {
			cached[keyURL] = keyData
		}
		return keyData, err
	})
	if setupErr != nil {
		return nil, fmt.Errorf("failed to create temp GPG key files: %w", setupErr)
	}
	failed, passed := splitVerifyResults(results)
	refresh := &keyRefresh{keyURLs: keyURLs, cached: cached, failed: failed, passed: passed}
	if len(failed) == 0 {
		return refresh, nil
	}
	verifyErr := verifyFailure(results)

	log.Warnf("RPM verification failed with cached GPG keys (%v); refreshing the keys and retrying once", verifyErr)
	candidate := make(map[string][]byte, len(cached))
	refresh.refreshed = make(map[string][]byte)
	for _, keyURL := range keyURLs {
		if _, fetched := candidate[keyURL]; fetched || isPlaceholderGPGKeyURL(keyURL) {
			continue
		}
		keyData, err := config.DownloadGPGKey(keyURL)
		if err != nil {
			log.Warnf("Could not refresh GPG key %s, keeping the cached key: %v", config.RedactKeyURL(keyURL), err)
			candidate[keyURL] = cached[keyURL]
			continue
		}
		candidate[keyURL] = keyData
		if !bytes.Equal(keyData, cached[keyURL]) {
			refresh.refreshed[keyURL] = keyData
		}
	}
	if len(refresh.refreshed) == 0 {
		return nil, verifyErr
	}

	retryResults, setupErr := verifyRPMsWithKeys(failed, keyURLs, keysFrom(candidate))
	if setupErr != nil {
		log.Warnf("Could not use the refreshed GPG keys: %v", setupErr)
		return nil, verifyErr
	}
	if retryErr := verifyFailure(retryResults); retryErr != nil {
		return nil, retryErr
	}
	return refresh, nil
}

// afterCachedKeyRead runs after each key is read by snapshotCachedKeys. Tests use
// it to interleave another build's publication.
var afterCachedKeyRead = func(keyURL string) {}

// snapshotCachedKeys returns the valid cached key of every key URL that has one.
// The keys are read under the cache lock, so they are the state of the cache at
// one moment: publishKeyRefreshes replaces several keys under the same lock, and
// reading them one by one without it could pair a key from before a publication
// with one from after, a set that omits the key an RPM is signed with. A build
// cancelled while waiting for the lock gets the cancellation back; if the lock
// fails for another reason the keys are read without it.
func snapshotCachedKeys(keyURLs []string) (map[string][]byte, error) {
	log := logger.Logger()

	ctx := runctx.Context()
	if unlock, err := config.LockGPGKeyCache(ctx); err == nil {
		defer unlock()
	} else if ctx.Err() != nil {
		return nil, err
	} else {
		log.Warnf("Reading the cached GPG keys without the cache lock: %v", err)
	}

	snapshot := make(map[string][]byte, len(keyURLs))
	for _, keyURL := range keyURLs {
		if isPlaceholderGPGKeyURL(keyURL) {
			continue
		}
		if keyData, ok := config.ReadCachedGPGKey(keyURL); ok {
			log.Infof("Using cached GPG key (%d bytes) for %s", len(keyData), config.RedactKeyURL(keyURL))
			snapshot[keyURL] = keyData
		}
		afterCachedKeyRead(keyURL)
	}
	return snapshot, nil
}

// publishKeyRefreshes writes the refreshed keys of refreshes to the cache, each
// only if it is both useful and safe to replace the cached key: it verifies at
// least one of the RPMs that failed with the cached keys, and every RPM that
// passed with the cached keys, in any of the sets that use the same key URL,
// still verifies with it in place of the cached key. Otherwise the cached key
// stays, since it may still verify RPMs that are already cached.
//
// Keys are checked in URL order against the cache as earlier publications have
// left it, so the result does not depend on the order the sets were verified in,
// and two keys that each look safe alone cannot together remove a key an RPM
// still needs. Publishing stops at the first write error: the write may have
// replaced the entry before failing, which leaves the state of the cache unknown.
//
// Other builds share the cache, and the keys a set was verified with may have been
// replaced since. The check and the writes therefore run under the cache lock,
// against the keys cached at that moment, so that two builds that each look safe
// alone cannot together remove a key an RPM still needs.
//
// A build cancelled while waiting for the lock gets the cancellation back. If the
// lock fails for another reason the keys are simply not cached.
func publishKeyRefreshes(refreshes []*keyRefresh) error {
	log := logger.Logger()

	refreshedKeys := make(map[string][]byte)
	for _, r := range refreshes {
		for keyURL, keyData := range r.refreshed {
			refreshedKeys[keyURL] = keyData
		}
	}
	if len(refreshedKeys) == 0 {
		return nil
	}

	ctx := runctx.Context()
	unlock, err := config.LockGPGKeyCache(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return err
		}
		log.Warnf("Not caching the refreshed GPG keys: %v", err)
		return nil
	}
	defer unlock()

	working := currentCachedKeys(refreshes)
	for _, keyURL := range slices.Sorted(maps.Keys(refreshedKeys)) {
		keyData := refreshedKeys[keyURL]
		if bytes.Equal(working[keyURL], keyData) {
			continue
		}
		if !refreshedKeyIsSafeToCache(keyURL, keyData, working, refreshes) {
			log.Infof("Keeping the cached GPG key for %s: the refreshed key is not needed or would stop verifying cached RPMs", config.RedactKeyURL(keyURL))
			continue
		}
		if err := config.StoreGPGKey(keyURL, keyData); err != nil {
			log.Warnf("Failed to cache the refreshed GPG key %s, not caching the remaining refreshed keys: %v", config.RedactKeyURL(keyURL), err)
			return nil
		}
		working[keyURL] = keyData
	}
	return nil
}

// currentCachedKeys returns the key now cached for every key URL of refreshes. A
// URL with no valid cache entry is absent from the result: the key its set was
// verified with may only have been held in memory, because writing it to the
// cache failed, so it is not a key a later build can still use. The caller holds
// the cache lock.
func currentCachedKeys(refreshes []*keyRefresh) map[string][]byte {
	working := make(map[string][]byte)
	read := make(map[string]bool)
	for _, r := range refreshes {
		for keyURL := range r.cached {
			if read[keyURL] {
				continue
			}
			read[keyURL] = true
			if current, ok := config.ReadCachedGPGKey(keyURL); ok {
				working[keyURL] = current
			}
		}
	}
	return working
}

// refreshedKeyIsSafeToCache reports whether keyData may replace the key cached for
// keyURL, given the keys now cached: it verifies at least one RPM that failed with
// the cached key, and the RPMs that passed in every set using keyURL still verify
// when it takes the cached key's place.
func refreshedKeyIsSafeToCache(keyURL string, keyData []byte, working map[string][]byte, refreshes []*keyRefresh) bool {
	useful := false
	for _, r := range refreshes {
		if _, ok := r.refreshed[keyURL]; !ok {
			continue
		}
		alone, _ := verifyRPMsWithKeys(r.failed, []string{keyURL}, keysFrom(map[string][]byte{keyURL: keyData}))
		if _, verifiedByKey := splitVerifyResults(alone); len(verifiedByKey) > 0 {
			useful = true
			break
		}
	}
	if !useful {
		return false
	}

	replaced := make(map[string][]byte, len(working))
	for k, v := range working {
		replaced[k] = v
	}
	replaced[keyURL] = keyData
	for _, r := range refreshes {
		if len(r.passed) == 0 || !slices.Contains(r.keyURLs, keyURL) {
			continue
		}
		results, setupErr := verifyRPMsWithKeys(r.passed, cachedKeyURLs(r.keyURLs, replaced), keysFrom(replaced))
		if setupErr != nil || verifyFailure(results) != nil {
			return false
		}
	}
	return true
}

// cachedKeyURLs returns the key URLs that have a key in keys.
func cachedKeyURLs(keyURLs []string, keys map[string][]byte) []string {
	var cached []string
	for _, keyURL := range keyURLs {
		if _, ok := keys[keyURL]; ok {
			cached = append(cached, keyURL)
		}
	}
	return cached
}

func keysFrom(keys map[string][]byte) func(string) ([]byte, error) {
	return func(keyURL string) ([]byte, error) { return keys[keyURL], nil }
}

// splitVerifyResults returns the paths that failed verification and those that
// passed.
func splitVerifyResults(results []Result) (failed, passed []string) {
	for _, r := range results {
		if r.OK {
			passed = append(passed, r.Path)
		} else {
			failed = append(failed, r.Path)
		}
	}
	return failed, passed
}

// verifyFailure describes the first RPM in results that failed verification, or
// returns nil if all passed.
func verifyFailure(results []Result) error {
	for _, r := range results {
		if !r.OK {
			return fmt.Errorf("RPM %s failed verification: %v", r.Path, r.Error)
		}
	}
	return nil
}

// verifyRPMsWithKeys returns the verification result of every RPM, or a setup
// error when the keys cannot be obtained.
func verifyRPMsWithKeys(rpmPaths, keyURLs []string, fetchKey func(string) ([]byte, error)) (results []Result, setupErr error) {
	log := logger.Logger()

	gpgKeyPaths, cleanup, err := createTempGPGKeyFilesUsing(keyURLs, fetchKey)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	log.Infof("created %d temporary GPG key files for verification", len(gpgKeyPaths))

	start := time.Now()
	results = verifyAllRPMs(rpmPaths, gpgKeyPaths, 4)
	log.Infof("RPM verification took %s", time.Since(start))
	return results, nil
}

// ValidateOrigins is like Validate, but scopes verification — and any
// "[trusted=yes]" opt-out — to the repository each RPM actually came from.
//
// fileOrigins maps a downloaded RPM's filename in destDir to the URL it was
// fetched from. repos records, for every configured repository, the base URL
// its packages were served from and its raw GPG key value(s) (see
// repoOrigin). An RPM is assigned to the repo whose baseURL is the longest
// matching prefix of its source URL; a repo's RPMs are only exempted from
// verification if that specific repo's keys are all "[trusted=yes]".
//
// An RPM whose origin can't be determined — fileOrigins has no entry for it,
// or no repo's baseURL matches — falls back to the same union-of-all-keys
// check Validate uses, so an unrecognized origin can never be silently
// exempted from verification.
func ValidateOrigins(destDir string, fileOrigins map[string]string, repos []repoOrigin) error {
	rpmPaths, err := listRPMFiles(destDir)
	if err != nil {
		return err
	}
	return validateOriginPaths(rpmPaths, fileOrigins, repos)
}

// validateSelectedOrigins is ValidateOrigins limited to the named files in
// destDir, so RPMs left in a shared cache by other builds are not verified
// against, or blamed on, the current configuration.
func validateSelectedOrigins(destDir string, files []string, fileOrigins map[string]string, repos []repoOrigin) error {
	rpmPaths := make([]string, len(files))
	for i, f := range files {
		rpmPaths[i] = filepath.Join(destDir, f)
	}
	return validateOriginPaths(rpmPaths, fileOrigins, repos)
}

func validateOriginPaths(rpmPaths []string, fileOrigins map[string]string, repos []repoOrigin) error {
	log := logger.Logger()

	if len(fileOrigins) == 0 || len(repos) == 0 {
		return validateUnionPaths(rpmPaths)
	}

	if len(rpmPaths) == 0 {
		log.Warn("no RPMs found to verify")
		return nil
	}

	var allKeys []string
	for _, r := range repos {
		allKeys = append(allKeys, r.keys...)
	}
	if len(allKeys) == 0 {
		return fmt.Errorf("no GPG keys configured for verification")
	}

	return verifyGroups(groupRPMsByOrigin(rpmPaths, fileOrigins, repos, allKeys))
}

// verifyGroup is a set of RPMs sharing one GPG-verification policy: verify
// against keys, or skip when trust is set (the owning repo opted out via
// "[trusted=yes]").
type verifyGroup struct {
	keys  []string
	trust bool
	files []string
}

// groupRPMsByOrigin buckets rpmPaths by the repository each RPM came from,
// using fileOrigins (filename→source URL) and repos. An RPM whose owning repo
// can't be determined lands in the fallback group (index -1), which verifies
// against allKeys — the union of every repo's keys — so an unrecognized origin
// is never silently exempted.
func groupRPMsByOrigin(
	rpmPaths []string, fileOrigins map[string]string, repos []repoOrigin, allKeys []string,
) map[int]*verifyGroup {
	groups := map[int]*verifyGroup{
		-1: {keys: allKeys},
	}
	for _, rpmPath := range rpmPaths {
		idx := -1
		if url, known := fileOrigins[filepath.Base(rpmPath)]; known {
			idx = matchRepoOrigin(url, repos)
		}
		g, ok := groups[idx]
		if !ok {
			g = &verifyGroup{keys: repos[idx].keys, trust: allKeysTrusted(repos[idx].keys)}
			groups[idx] = g
		}
		g.files = append(g.files, rpmPath)
	}
	return groups
}

// verifyGroups verifies each non-empty group's RPMs against that group's keys,
// skipping any group whose repository opted out via "[trusted=yes]". It returns
// the first verification failure encountered, or nil if every group passes.
// Groups are verified in index order, and keys refreshed along the way are
// published together once every group has passed, so repositories that share a
// key URL cannot replace each other's cached key.
func verifyGroups(groups map[int]*verifyGroup) error {
	log := logger.Logger()
	var refreshes []*keyRefresh
	for _, idx := range slices.Sorted(maps.Keys(groups)) {
		g := groups[idx]
		if len(g.files) == 0 {
			continue
		}
		if g.trust {
			log.Infof("skipping RPM signature verification for %d package(s) from a "+
				"[trusted=yes] repository", len(g.files))
			continue
		}
		if len(g.keys) == 0 {
			return fmt.Errorf("no GPG keys configured for verification")
		}

		refresh, err := verifyRPMsWithKeyRefresh(g.files, g.keys)
		if err != nil {
			return err
		}
		refreshes = append(refreshes, refresh)
	}
	if err := publishKeyRefreshes(refreshes); err != nil {
		return err
	}
	log.Info("all RPMs verified successfully")
	return nil
}

// matchRepoOrigin returns the index into repos whose baseURL is the longest
// matching prefix of url, or -1 if none match.
func matchRepoOrigin(url string, repos []repoOrigin) int {
	best, bestLen := -1, 0
	for i, r := range repos {
		if r.baseURL == "" || !strings.HasPrefix(url, r.baseURL) {
			continue
		}
		if len(r.baseURL) > bestLen {
			best, bestLen = i, len(r.baseURL)
		}
	}
	return best
}

// buildRepoOrigins assembles the per-repository provenance used to scope RPM
// verification: each repository's package base URL and its raw GPG key value(s).
// Local repositories (repo.Path != "") come from localRepoOrigins, recorded with
// the URL LocalUserPackages actually served them from.
func buildRepoOrigins() []repoOrigin {
	var origins []repoOrigin
	if RepoCfg.URL != "" {
		origins = append(origins, repoOrigin{baseURL: RepoCfg.URL, keys: repoRawKeys(RepoCfg.GPGKey, nil)})
	}
	for _, repo := range UserRepo {
		if repo.Path != "" {
			continue
		}
		origins = append(origins, repoOrigin{baseURL: repo.URL, keys: repoRawKeys(repo.PKey, repo.PKeys)})
	}
	return append(origins, localRepoOrigins...)
}

// purgeTrustedCachedRPMs deletes any cached file whose package will be served
// this run by a "[trusted=yes]" repository. FetchPackages skips existing
// non-empty files, so a stale cached file sharing the basename would inherit the
// trust opt-out and skip verification (CWE-347). Dropping it forces a fresh
// download, so the opt-out only covers bytes actually fetched from that
// repository this run. A removal failure is fatal: leaving the stale file would
// reopen the bypass, so the caller must not fetch. urls and filenames are
// index-aligned by the caller.
func purgeTrustedCachedRPMs(destDir string, urls, filenames []string, repos []repoOrigin) error {
	log := logger.Logger()
	for i, u := range urls {
		idx := matchRepoOrigin(u, repos)
		if idx < 0 || !allKeysTrusted(repos[idx].keys) {
			continue
		}
		cached := filepath.Join(destDir, filenames[i])
		switch err := os.Remove(cached); {
		case err == nil:
			log.Infof("dropped cached %s so it is re-downloaded from its [trusted=yes] source", filenames[i])
		case os.IsNotExist(err):
			// Nothing cached; the fresh download will fetch it.
		default:
			return fmt.Errorf("removing cached %s for trusted re-download: %w", filenames[i], err)
		}
	}
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

func isRPMRequirementInCache(required string, cachedPackageInfos []ospackage.PackageInfo) bool {
	required = strings.TrimSpace(required)
	if required == "" {
		return true
	}

	for _, pkg := range cachedPackageInfos {
		// A requirement is the package's base name or the file name it has on disk.
		if strings.TrimSpace(pkg.Name) == required || filepath.Base(pkg.URL) == required {
			return true
		}
	}

	return false
}

func isRPMPackageCacheOutdated(requiredPackages []string, cacheDir string) (bool, []string, []string, error) {
	cachedPaths, err := listRPMFiles(cacheDir)
	if err != nil {
		return false, nil, nil, err
	}

	cachedPackageInfos := make([]ospackage.PackageInfo, 0, len(cachedPaths))
	cachedFiles := make([]string, 0, len(cachedPaths))
	for _, p := range cachedPaths {
		base := filepath.Base(p)
		cachedFiles = append(cachedFiles, base)
		cachedPackageInfos = append(cachedPackageInfos, ospackage.PackageInfo{
			Name: extractBasePackageNameFromFile(base),
			URL:  p,
			Type: "rpm",
		})
	}

	missingSet := make(map[string]struct{})
	var missing []string
	for _, req := range requiredPackages {
		req = strings.TrimSpace(req)
		if req == "" {
			continue
		}
		if isRPMRequirementInCache(req, cachedPackageInfos) {
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

func configuredRPMRepoURLs() []string {
	seen := make(map[string]struct{})
	urls := make([]string, 0, 1+len(UserRepo))
	add := func(url string) {
		url = strings.TrimSpace(url)
		if url == "" || url == "<URL>" {
			return
		}
		if _, ok := seen[url]; ok {
			return
		}
		seen[url] = struct{}{}
		urls = append(urls, url)
	}

	add(RepoCfg.URL)
	for _, repo := range UserRepo {
		add(repo.URL)
	}

	return urls
}

// clearRPMMetadataCache removes functional RPM repository metadata cache entries
// from the metadata cache directories of repoURLs so that their metadata is
// re-fetched and re-verified on the next run. Other repositories keep theirs, so
// a repository that is only reachable from the cache stays usable.
func clearRPMMetadataCache(repoURLs []string) {
	log := logger.Logger()

	for _, repoURL := range repoURLs {
		metaDir, err := rpmMetadataCacheDir(repoURL)
		if err != nil {
			log.Warnf("failed to resolve RPM metadata cache directory for %s: %v", redactURLForLog(repoURL), err)
			continue
		}
		clearRPMMetadataDir(metaDir)
	}
}

// clearRPMMetadataDir clears one repository's metadata cache directory.
//
// repomd.xml and primary.location.json are the authoritative manifests
// established by FetchPrimaryURL at Init(); deleting them breaks subsequent
// offline rebuilds because Init() runs before any network operation and cannot
// re-populate them without network access.
//
// A raw primary payload and the manifest naming it are only consistent while the
// writer holds the href's lock, so each href's files are listed and removed under
// that same lock; a writer then either finishes before the clear or starts after
// it. The remaining files are each written atomically on their own.
func clearRPMMetadataDir(metaDir string) {
	// The directory is read literally rather than globbed, since the cache
	// root may contain glob metacharacters.
	entries, err := os.ReadDir(metaDir)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Logger().Warnf("failed to read RPM metadata cache directory %s: %v", metaDir, pathErrorCause(err))
		}
		return
	}

	seen := make(map[string]struct{})
	for _, entry := range entries {
		hash := rpmRawMetadataHrefHash(entry.Name())
		if _, done := seen[hash]; hash == "" || done {
			continue
		}
		seen[hash] = struct{}{}
		clearRawMetadataForHref(metaDir, hash)
	}

	removeClearableRPMMetadata(metaDir, func(name string) bool { return rpmRawMetadataHrefHash(name) == "" })
}

// clearRawMetadataForHref removes the raw primary files of one href while holding
// the lock the cache writer takes. If the lock cannot be taken, whether the build
// was cancelled while waiting for it or the lock file could not be opened, the
// files are left: another build may hold the lock through a file this one cannot
// open, and removing a payload it is writing would leave an unusable cache.
func clearRawMetadataForHref(metaDir, hrefHash string) {
	unlock, err := lockRPMRawMetadataFile(rpmRawMetadataLockPathForHash(metaDir, hrefHash))
	if err != nil {
		logger.Logger().Warnf("failed to lock the RPM primary metadata cache; leaving it in place: %v", pathErrorCause(err))
		return
	}
	defer unlock()
	removeClearableRPMMetadata(metaDir, func(name string) bool { return rpmRawMetadataHrefHash(name) == hrefHash })
}

// removeClearableRPMMetadata lists metaDir afresh and removes the clearable files
// that match.
func removeClearableRPMMetadata(metaDir string, match func(name string) bool) {
	log := logger.Logger()

	entries, err := os.ReadDir(metaDir)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Warnf("failed to read RPM metadata cache directory %s: %v", metaDir, pathErrorCause(err))
		}
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !isClearableRPMMetadataFile(name) || !match(name) {
			continue
		}
		if err := os.Remove(filepath.Join(metaDir, name)); err != nil && !os.IsNotExist(err) {
			log.Warnf("failed to remove RPM metadata cache %s: %v", filepath.Join(metaDir, redactCacheFileName(name)), pathErrorCause(err))
			continue
		}
		log.Infof("removed RPM metadata cache: %s", filepath.Join(metaDir, redactCacheFileName(name)))
	}
}

// rpmMetadataClearPatterns are the cache file names, relative to a repository's
// metadata directory, that clearRPMMetadataCache removes.
var rpmMetadataClearPatterns = []string{
	"primary.parsed.json",
	"repomd_*.xml",
	"primary_*.cache.json",
	"primary_*.gz",
	"primary_*.zst",
	"primary_*.xml",
	// Timestamped copies from saveOriginalXML and saveUncompressedXML are
	// named after the href's basename, which need not start with "primary_".
	"*_????????_????-??-??_??-??-??.*",
}

func isClearableRPMMetadataFile(name string) bool {
	for _, pattern := range rpmMetadataClearPatterns {
		if matched, err := path.Match(pattern, name); err == nil && matched {
			return true
		}
	}
	return false
}

// redactCacheFileName drops the query string or fragment that files written
// under a raw href name can carry, before the name is logged.
func redactCacheFileName(name string) string {
	if i := strings.IndexAny(name, "?#"); i >= 0 {
		return name[:i] + "<redacted>"
	}
	return name
}

// pathErrorCause returns the underlying error of a *fs.PathError, whose text
// would otherwise repeat the full path.
func pathErrorCause(err error) error {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err
	}
	return err
}

// clearRPMPackageCache removes all .rpm files from cacheDir and invalidates
// functional repository metadata cache files so that a full re-download including
// fresh repository metadata is performed on the next run.
func clearRPMPackageCache(cacheDir string) error {
	log := logger.Logger()
	files, err := listRPMFiles(cacheDir)
	if err != nil {
		return err
	}
	for _, f := range files {
		if err := os.Remove(f); err != nil {
			return fmt.Errorf("removing cached file %s: %w", f, err)
		}
		log.Debugf("removed stale cached file: %s", filepath.Base(f))
	}
	log.Infof("cleared %d stale RPM files from cache directory %s", len(files), cacheDir)
	clearRPMMetadataCache(configuredRPMRepoURLs())
	return nil
}

// DownloadPackages downloads packages and returns the list of downloaded package names.
func DownloadPackages(pkgList []string, destDir, dotFile string, pkgSources map[string]config.PackageSource, systemRootsOnly bool) ([]string, error) {
	downloadedPkgs, _, err := DownloadPackagesComplete(pkgList, destDir, dotFile, pkgSources, systemRootsOnly)
	return downloadedPkgs, err
}

// DownloadPackagesComplete downloads packages and returns both package names and full package info.
func DownloadPackagesComplete(pkgList []string, destDir, dotFile string, pkgSources map[string]config.PackageSource, systemRootsOnly bool) ([]string, []ospackage.PackageInfo, error) {
	return downloadPackagesComplete(pkgList, destDir, dotFile, pkgSources, systemRootsOnly, false)
}

func handleRPMCacheRetry(
	requiredPackages []string,
	requirementRepos map[string]string,
	absDestDir string,
	retriedAfterMetadataClear bool,
	retryFunc func() ([]string, []ospackage.PackageInfo, error),
) ([]string, []ospackage.PackageInfo, bool, error) {
	log := logger.Logger()

	cacheOutdated, missingRequired, _, cacheErr := isRPMPackageCacheOutdatedFunc(requiredPackages, absDestDir)
	if cacheErr != nil {
		log.Warnf("Failed to evaluate RPM package cache state: %v", cacheErr)
		return nil, nil, false, nil
	}

	// A complete cache is not a result by itself: the caller still validates the
	// file names and verifies the cached RPMs against their repositories, and
	// reports only the resolved packages.
	if !cacheOutdated {
		log.Infof("RPM package cache is up-to-date; all %d resolved packages are available locally", len(requiredPackages))
		return nil, nil, false, nil
	}

	if len(missingRequired) == 0 {
		return nil, nil, false, nil
	}

	log.Infof("RPM package cache is outdated; missing required packages: %v", missingRequired)

	// Clear only once: after the retry has repopulated the metadata cache,
	// clearing it again would discard metadata the next offline build needs.
	if retriedAfterMetadataClear {
		log.Infof("Keeping existing cached RPM files and continuing to fetch only missing/new packages")
		return nil, nil, false, nil
	}

	// The cached repomd.xml names the primary file the metadata was built from,
	// and a repository may since have retired it. Take the current reference first,
	// so the retry fetches the file the repository serves now, and keep the cached
	// metadata of any repository that cannot be reached.
	refreshed := refreshRPMPrimaryFunc(reposToRefresh(missingRequired, requirementRepos))
	if len(refreshed) == 0 {
		log.Warnf("Could not refresh the repository metadata reference; keeping cached metadata and continuing with the cached package list")
		return nil, nil, false, nil
	}
	clearRPMMetadataCacheFunc(refreshed)
	log.Infof("Cleared RPM metadata cache due to missing required packages")

	log.Infof("Retrying RPM package resolution after metadata cache clear")
	pkgs, infos, err := retryFunc()
	var resolveErr *resolutionError
	if !errors.As(err, &resolveErr) {
		return pkgs, infos, true, err
	}

	// The refreshed repositories may now offer packages that need newer versions
	// than the indexes retained for the others provide. Refresh the other
	// reachable repositories too and resolve once more; unreachable ones keep
	// their cached metadata.
	others := repositoriesNotIn(configuredRPMRepoURLs(), refreshed)
	if len(others) == 0 {
		return pkgs, infos, true, err
	}
	log.Warnf("Resolution failed against the retained metadata of other repositories (%v); refreshing them too", err)
	refreshedOthers := refreshRPMPrimaryFunc(others)
	if len(refreshedOthers) == 0 {
		return pkgs, infos, true, err
	}
	clearRPMMetadataCacheFunc(refreshedOthers)
	pkgs, infos, err = retryFunc()
	return pkgs, infos, true, err
}

// resolutionError marks a failure to match or resolve the requested packages
// against the repository metadata, as opposed to a failure to download them.
type resolutionError struct{ err error }

func (e *resolutionError) Error() string { return e.err.Error() }
func (e *resolutionError) Unwrap() error { return e.err }

// repositoriesNotIn returns the URLs of all that are not in excluded, ignoring
// trailing slashes.
func repositoriesNotIn(all, excluded []string) []string {
	skip := make(map[string]struct{}, len(excluded))
	for _, repoURL := range excluded {
		skip[strings.TrimRight(repoURL, "/")] = struct{}{}
	}
	var rest []string
	for _, repoURL := range all {
		if _, ok := skip[strings.TrimRight(repoURL, "/")]; !ok {
			rest = append(rest, repoURL)
		}
	}
	return rest
}

// refreshRPMPrimaryReferences re-reads the repomd.xml of each repository in
// repoURLs from the repository and returns those that could be refreshed. The base
// repository's primary href is updated for the retry; the others are read through
// the refreshed cache by UserPackages.
func refreshRPMPrimaryReferences(repoURLs []string) []string {
	log := logger.Logger()

	var refreshed []string
	for _, repoURL := range repoURLs {
		primary, err := refreshPrimaryReference(repoURL)
		if err != nil {
			log.Warnf("Failed to refresh repository metadata reference for %s: %v", redactURLForLog(repoURL), err)
			continue
		}
		if strings.TrimRight(repoURL, "/") == strings.TrimRight(RepoCfg.URL, "/") {
			GzHref = primary.Href
		}
		refreshed = append(refreshed, repoURL)
	}
	return refreshed
}

// metadataRefreshRequirements lists the file names whose absence from the package
// cache means the repository metadata may be stale, and maps each to the
// configured repository that serves it (when one does). Packages served from a
// local repository are left out: they are produced on every run from the local
// directory, so a miss says nothing about the remote metadata and must not
// discard it.
func metadataRefreshRequirements(pkgs []ospackage.PackageInfo) ([]string, map[string]string) {
	// Repository metadata names a package by its rpm file name, so require the
	// file name the fetcher writes, which is what the cache directory holds.
	required := make([]string, 0, len(pkgs))
	repos := make(map[string]string, len(pkgs))
	for _, pkg := range pkgs {
		name := pkg.Name
		if pkg.URL != "" {
			if isLocalRepoURL(pkg.URL) {
				continue
			}
			name = packageFileNameFromURL(pkg.URL)
			if repoURL := configuredRepoURLFor(pkg.URL); repoURL != "" {
				repos[name] = repoURL
			}
		}
		required = append(required, name)
	}
	return required, repos
}

// configuredRepoURLFor returns the configured repository whose URL is the longest
// prefix of packageURL, or "" if none is.
func configuredRepoURLFor(packageURL string) string {
	best := ""
	for _, repoURL := range configuredRPMRepoURLs() {
		base := strings.TrimRight(repoURL, "/")
		if strings.HasPrefix(packageURL, base+"/") && len(repoURL) > len(best) {
			best = repoURL
		}
	}
	return best
}

// reposToRefresh returns the repositories whose metadata the missing files call
// into question. A file that no configured repository is known to serve could be
// from any of them, so every configured repository is refreshed then.
func reposToRefresh(missing []string, requirementRepos map[string]string) []string {
	seen := make(map[string]struct{}, len(missing))
	var repoURLs []string
	for _, file := range missing {
		repoURL, known := requirementRepos[file]
		if !known {
			return configuredRPMRepoURLs()
		}
		if _, dup := seen[repoURL]; !dup {
			seen[repoURL] = struct{}{}
			repoURLs = append(repoURLs, repoURL)
		}
	}
	return repoURLs
}

// isLocalRepoURL reports whether rawURL is served by one of the local
// repositories LocalUserPackages started for this run.
func isLocalRepoURL(rawURL string) bool {
	for _, r := range localRepoOrigins {
		if r.baseURL != "" && strings.HasPrefix(rawURL, strings.TrimSuffix(r.baseURL, "/")+"/") {
			return true
		}
	}
	return false
}

// packageFileNameFromURL derives the on-disk basename the fetcher writes for a
// package URL, decoding the path exactly as pkgfetcher does so validation sees
// the same name that lands on disk (e.g. %2B -> +), not the still-encoded URL.
func packageFileNameFromURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Path == "" {
		return path.Base(strings.SplitN(rawURL, "?", 2)[0])
	}
	return path.Base(parsed.Path)
}

// buildDownloadList derives each package's on-disk filename and rejects any that
// is not a plain RPM basename before it can reach the rpm install sinks (CWE-78).
func buildDownloadList(pkgs []ospackage.PackageInfo) (urls, downloadPkgList []string, err error) {
	urls = make([]string, len(pkgs))
	for i, pkg := range pkgs {
		name := packageFileNameFromURL(pkg.URL)
		if verr := validatePackageFileName(name); verr != nil {
			return nil, nil, fmt.Errorf("rejecting package from %q: %w", pkg.URL, verr)
		}
		urls[i] = pkg.URL
		downloadPkgList = append(downloadPkgList, name)
	}
	return urls, downloadPkgList, nil
}

func downloadPackagesComplete(pkgList []string, destDir, dotFile string, pkgSources map[string]config.PackageSource, systemRootsOnly bool, retriedAfterMetadataClear bool) ([]string, []ospackage.PackageInfo, error) {
	var downloadPkgList []string

	log := logger.Logger()
	absDestDir, err := filepath.Abs(destDir)
	if err != nil {
		return downloadPkgList, nil, fmt.Errorf("resolving cache directory: %v", err)
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
		return downloadPkgList, nil, &resolutionError{fmt.Errorf("matching packages: %v", err)}
	}
	log.Infof("Matched a total of %d packages", len(req))

	for _, pkg := range req {
		log.Debugf("-> %s", pkg.Name)
	}

	// Resolve the dependencies of the requested packages
	needed, err := Resolve(req, all)
	if err != nil {
		return downloadPkgList, nil, &resolutionError{fmt.Errorf("resolving packages: %v", err)}
	}

	sorted_pkgs, err := pkgsorter.SortPackages(needed)
	if err != nil {
		log.Errorf("sorting packages: %v", err)
	}
	log.Infof("Sorted %d packages for installation", len(sorted_pkgs))

	if len(sorted_pkgs) > 0 {
		requiredPackages, requirementRepos := metadataRefreshRequirements(sorted_pkgs)

		handledPkgList, handledInfos, handled, handledErr := handleRPMCacheRetry(
			requiredPackages,
			requirementRepos,
			absDestDir,
			retriedAfterMetadataClear,
			func() ([]string, []ospackage.PackageInfo, error) {
				return downloadPackagesComplete(pkgList, destDir, dotFile, pkgSources, systemRootsOnly, true)
			},
		)
		if handled {
			return handledPkgList, handledInfos, handledErr
		}
	}

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

	// Extract URLs, rejecting any package whose on-disk filename is unsafe.
	urls, validated, buildErr := buildDownloadList(sorted_pkgs)
	if buildErr != nil {
		return downloadPkgList, nil, buildErr
	}
	downloadPkgList = validated

	// Ensure dest directory exists
	if err := os.MkdirAll(absDestDir, 0755); err != nil {
		return downloadPkgList, nil, fmt.Errorf("creating cache directory %s: %v", absDestDir, err)
	}

	// Per-repository provenance scopes the "[trusted=yes]" opt-out during
	// verification. It is built before download because trusted packages must be
	// force-refreshed first (see purgeTrustedCachedRPMs).
	origins := buildRepoOrigins()
	if err := purgeTrustedCachedRPMs(absDestDir, urls, downloadPkgList, origins); err != nil {
		return downloadPkgList, nil, fmt.Errorf("preparing trusted-repository downloads: %w", err)
	}

	// Download packages using configured workers and cache directory
	log.Infof("Downloading %d packages to %s using %d workers", len(urls), absDestDir, config.Workers())
	if err := pkgfetcher.FetchPackages(runctx.Context(), urls, absDestDir, config.Workers()); err != nil {
		return downloadPkgList, nil, fmt.Errorf("fetch failed: %v", err)
	}
	log.Info("All downloads complete")

	// Verify downloaded packages, scoping any "[trusted=yes]" opt-out to only
	// the repository each RPM was actually downloaded from.
	fileOrigins := make(map[string]string, len(urls))
	for i, u := range urls {
		fileOrigins[downloadPkgList[i]] = u
	}

	if err := validateOriginsFunc(destDir, downloadPkgList, fileOrigins, origins); err != nil {
		return downloadPkgList, nil, fmt.Errorf("verification failed: %v", err)
	}

	return downloadPkgList, needed, nil
}
