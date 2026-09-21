package debutils

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage/pkgfetcher"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/runctx"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/system"
)

// VersionConstraint represents a version operator and version pair
type VersionConstraint struct {
	Op          string
	Ver         string
	Alternative string // Alternative package name for constraints like "logsave | e2fsprogs (<< 1.45.3-1~)"
	// AlternativeTerms is the "|"-joined RAW alternative terms (name plus its
	// own version constraint, e.g. "e2fsprogs (<< 1.45.3-1~)"), unlike
	// Alternative which only carries cleaned names. Needed whenever deciding
	// an OR edge is satisfied requires checking the alternative's OWN version
	// constraint, not just its presence.
	AlternativeTerms string
}

func isGlobPattern(pattern string) bool {
	return strings.ContainsAny(pattern, "*?[]")
}

func matchesPackageFilter(pkgName string, filter []string) bool {
	if len(filter) == 0 {
		return true
	}

	for _, pattern := range filter {
		if isGlobPattern(pattern) {
			if ok, err := path.Match(pattern, pkgName); err == nil && ok {
				return true
			}
		}

		if pkgName == pattern {
			return true
		}

		if strings.HasPrefix(pkgName, pattern+"-") || strings.HasPrefix(pkgName, pattern) {
			return true
		}
	}

	return false
}

func GenerateDot(pkgs []ospackage.PackageInfo, file string, pkgSources map[string]config.PackageSource) error {
	log := logger.Logger()
	log.Infof("Generating DOT file %s", file)

	outFile, err := os.Create(file)
	if err != nil {
		return fmt.Errorf("creating DOT file: %w", err)
	}
	defer outFile.Close()

	writer := bufio.NewWriter(outFile)
	defer writer.Flush()

	if _, err := fmt.Fprintln(writer, "digraph G {"); err != nil {
		return fmt.Errorf("writing DOT header: %w", err)
	}
	if _, err := fmt.Fprintln(writer, "  rankdir=LR;"); err != nil {
		return fmt.Errorf("writing DOT attributes: %w", err)
	}
	if _, err := fmt.Fprintln(writer, "  node [shape=box];"); err != nil {
		return fmt.Errorf("writing DOT node defaults: %w", err)
	}

	edgesWritten := make(map[string]bool)

	for _, pkg := range pkgs {
		if pkg.Name == "" {
			continue
		}
		if _, err := fmt.Fprintf(writer, "  \"%s\";\n", pkg.Name); err != nil {
			return fmt.Errorf("writing DOT node for %s: %w", pkg.Name, err)
		}
		for _, dep := range pkg.Requires {
			depName := CleanDependencyName(dep)
			if depName == "" {
				continue
			}
			edgeKey := pkg.Name + "|" + depName
			if edgesWritten[edgeKey] {
				continue
			}
			if _, err := fmt.Fprintf(writer, "  \"%s\" -> \"%s\";\n", pkg.Name, depName); err != nil {
				return fmt.Errorf("writing DOT edge %s->%s: %w", pkg.Name, depName, err)
			}
			edgesWritten[edgeKey] = true
		}
	}

	if _, err := fmt.Fprintln(writer, "}"); err != nil {
		return fmt.Errorf("writing DOT footer: %w", err)
	}

	return nil
}

// parsedPackageCacheVersion is the schema version of the on-disk parsed metadata
// cache. Bump it whenever the parsed shape changes (e.g. a new PackageInfo field
// the parser now populates) so caches written by an older binary are treated as a
// miss and re-parsed, rather than silently returning records missing the new data.
// v2: PackageInfo.Breaks is now parsed and consumed by the overlay Breaks-driven
// upgrade; a v1 cache would omit it.
// v3: PackageInfo.InstalledSizeBytes is now parsed from Installed-Size and feeds
// overlay auto-sizing; a v2 (or older) cache would report every package as size 0.
// v4: PackageInfo.HasInstalledSize is now parsed alongside InstalledSizeBytes to
// distinguish an explicit zero footprint from a missing size; a v3 cache would
// report every package as HasInstalledSize=false (treated as unknown).
// v5: PackageInfo.ProvidesVer is now parsed from Provides: (mirroring Breaks); a
// v4 cache would report every package's ProvidesVer as empty, so a version
// constraint on a virtual capability would fall back to comparing against the
// provider's own Version instead of what it actually declares for that name.
const parsedPackageCacheVersion = 5

// inReleaseSentinel is passed as releaseSign to ParseRepositoryMetadata to
// mean "releaseFile is a combined InRelease file, not a detached signature
// pair" — see BuildRepoConfigs (download.go) and isInRelease below.
const inReleaseSentinel = "[inrelease]"

// packageMetadataCache stores parsed package metadata keyed by the Packages.gz SHA256
// checksum recorded in the Release file. A matching checksum means the upstream
// repository has not changed, so we can skip downloading, decompressing, and re-parsing.
// Version guards against reusing a cache written by an older parser (see
// parsedPackageCacheVersion).
type packageMetadataCache struct {
	Version  int                     `json:"version"`
	Checksum string                  `json:"checksum"`
	Packages []ospackage.PackageInfo `json:"packages"`
}

func shouldBypassParsedPackageCache(baseURL string) bool {
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return false
	}

	host := strings.ToLower(parsedURL.Hostname())
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func loadParsedPackageCache(cacheFile string) (*packageMetadataCache, error) {
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		return nil, err
	}
	var cache packageMetadataCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("invalid cache file: %w", err)
	}
	return &cache, nil
}

func saveParsedPackageCache(cacheFile, checksum string, pkgs []ospackage.PackageInfo) error {
	cache := packageMetadataCache{Version: parsedPackageCacheVersion, Checksum: checksum, Packages: pkgs}
	data, err := json.Marshal(cache)
	if err != nil {
		return fmt.Errorf("failed to marshal package cache: %w", err)
	}
	return os.WriteFile(cacheFile, data, 0600)
}

func metadataFileName(raw string) string {
	if parsed, err := url.Parse(raw); err == nil && parsed.Path != "" {
		return path.Base(parsed.Path)
	}
	return filepath.Base(raw)
}

// refreshRepoMetadata re-downloads the repository metadata files (Release, its
// signature, and the archive key where applicable) into pkgMetaDir.
//
// The download is staged in a sibling temporary directory and moved into place
// only after every file has arrived, so an interrupted or failed refresh cannot
// leave pkgMetaDir holding a partial Release — which would fail signature
// verification and break an otherwise working offline build. Returns whether the
// files were replaced; on error the existing files are untouched.
func refreshRepoMetadata(pkgMetaDir string, localFiles, urls []string) (bool, error) {
	stageDir, err := os.MkdirTemp(pkgMetaDir, ".meta-refresh-")
	if err != nil {
		return false, fmt.Errorf("creating metadata staging directory: %w", err)
	}
	defer func() {
		if remErr := os.RemoveAll(stageDir); remErr != nil {
			logger.Logger().Warnf("failed to remove metadata staging directory %s: %v", stageDir, remErr)
		}
	}()

	if err := pkgfetcher.FetchPackages(runctx.Context(), urls, stageDir, 1); err != nil {
		return false, err
	}

	// Require every expected file before committing: FetchPackages reports failures
	// in aggregate, and a partial set must not overwrite a complete one.
	for _, f := range localFiles {
		staged := filepath.Join(stageDir, metadataFileName(f))
		if fi, statErr := os.Stat(staged); statErr != nil || fi.Size() == 0 {
			return false, fmt.Errorf("refreshed metadata is missing or empty: %s", metadataFileName(f))
		}
	}

	for _, f := range localFiles {
		staged := filepath.Join(stageDir, metadataFileName(f))
		if err := os.Rename(staged, f); err != nil {
			// Past the first successful rename this leaves a mixed set. Report it
			// rather than pressing on: the caller treats a refresh error as
			// "continue with what's on disk", and verification will catch a
			// Release/signature pair that no longer agrees.
			return false, fmt.Errorf("installing refreshed metadata %s: %w", metadataFileName(f), err)
		}
	}
	return true, nil
}

// warnIfReleaseExpired logs when a Release file we could not refresh has passed
// its Valid-Until. Debian sets that field precisely to bound how long a mirror
// snapshot should be trusted, and past it the recorded package versions are
// likely to have been superseded and deleted from the pool — the failure then
// surfaces much later as a 404 on a single .deb, which reads like a broken
// mirror rather than an expired local cache. Advisory only: a repository with no
// Valid-Until is silently accepted, and this never fails the build.
func warnIfReleaseExpired(releaseFile, baseURL string) {
	f, err := os.Open(releaseFile)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		// Stop at the checksum blocks: they are the bulk of the file and cannot
		// contain the field (indented continuation lines).
		if strings.HasPrefix(line, " ") {
			continue
		}
		value, ok := strings.CutPrefix(line, "Valid-Until:")
		if !ok {
			continue
		}
		until, perr := http.ParseTime(strings.TrimSpace(value))
		if perr != nil {
			return
		}
		if time.Now().After(until) {
			logger.Logger().Warnf("cached metadata for %s expired on %s; package versions it "+
				"names may no longer exist in the repository (a download failing with 404 is the "+
				"usual symptom). Restore network access, or clear %s to force a refresh",
				baseURL, until.UTC().Format(time.RFC1123), filepath.Dir(releaseFile))
		}
		return
	}
}

// ParseRepositoryMetadata parses the Packages.gz file from gzHref.
//
// Caching is validated against the repository, not merely reused. The Release
// file is small and is re-fetched on every online run; the SHA256 it records for
// Packages.gz is the cache key for both the parse cache (packages.parsed.json)
// and the downloaded Packages.gz itself. A cache entry is used only when its key
// still matches the repository's current Release, so a mirror that has moved on
// invalidates the cache instead of being ignored.
//
// Offline use is preserved by falling back, not by skipping validation: if the
// Release re-fetch fails, the previously downloaded metadata is used as-is (and a
// warning is logged if it has passed its Valid-Until). The refresh is staged in a
// temporary directory and moved into place only once complete, so a failed
// refresh cannot destroy the copies an offline build depends on.
func ParseRepositoryMetadata(baseURL string, pkggz string, releaseFile string, releaseSign string, pbGPGKey string, buildPath string, arch string, packageFilter []string) ([]ospackage.PackageInfo, error) {
	log := logger.Logger()

	// Ensure pkgMetaDir exists, create if not
	// pkgMetaDir := filepath.Join(config.TempDir(), "builds", "elxr12")
	pkgMetaDir := buildPath
	if err := os.MkdirAll(pkgMetaDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create pkgMetaDir: %w", err)
	}

	// --- Parse cache load ---
	// Loaded up front but deliberately NOT returned yet: it is only usable once
	// its checksum has been checked against the repository's current Release
	// (below). Returning it here — as this code previously did — pins a build to
	// whatever package versions were current when the cache was first written.
	// Debian's security pool deletes superseded files, so a stale index makes
	// every subsequent build request a .deb that now 404s, with no way out short
	// of deleting the cache directory by hand.
	cacheFile := filepath.Join(pkgMetaDir, "packages.parsed.json")
	allowParsedCache := !shouldBypassParsedPackageCache(baseURL) && !system.IsLiveInstallerExecution()
	if !allowParsedCache {
		log.Debugf("Bypassing parsed package metadata cache for %s", baseURL)
	}
	var cached *packageMetadataCache
	if allowParsedCache {
		// Require both a non-empty checksum and a matching schema version; a cache
		// written by an older parser (different version) is ignored and re-parsed so
		// newly-parsed fields (e.g. Breaks) are populated.
		if c, loadErr := loadParsedPackageCache(cacheFile); loadErr == nil && c.Checksum != "" && c.Version == parsedPackageCacheVersion {
			cached = c
		}
	}

	//verify release file
	localPkggzFile := filepath.Join(pkgMetaDir, metadataFileName(pkggz))
	localReleaseFile := filepath.Join(pkgMetaDir, metadataFileName(releaseFile))
	localReleaseSign := filepath.Join(pkgMetaDir, metadataFileName(releaseSign))
	localPBGPGKey := filepath.Join(pkgMetaDir, metadataFileName(pbGPGKey))

	// Determine if pbGPGKey is a URL or file path
	pbkeyIsURL := false
	isTrustedRepo := pbGPGKey == "[trusted=yes]"

	// releaseSign == inReleaseSentinel means releaseFile is a combined
	// InRelease file (Release content + embedded signature) rather than a
	// plain Release paired with a detached Release.gpg. Some aptly-published
	// repos only serve InRelease, so BuildRepoConfigs signals that case this
	// way. This can't just be an empty releaseSign: some callers (e.g. local
	// trusted repos in LocalUserPackages) already pass "" to mean "no
	// detached signature to fetch, nothing else implied" for a plain
	// Release file, so a distinct sentinel avoids colliding with that.
	isInRelease := releaseSign == inReleaseSentinel

	if strings.HasPrefix(pbGPGKey, "http://") || strings.HasPrefix(pbGPGKey, "https://") {
		pbkeyIsURL = true
	} else {
		localPBGPGKey = pbGPGKey
	}

	var metaLocalFiles []string
	var metaURLList []string

	switch {
	case isTrustedRepo:
		// For trusted repos, skip Release.gpg/InRelease signature and GPG key download
		metaLocalFiles = []string{localReleaseFile}
		metaURLList = []string{releaseFile}
	case isInRelease && pbkeyIsURL:
		metaLocalFiles = []string{localReleaseFile, localPBGPGKey}
		metaURLList = []string{releaseFile, pbGPGKey}
	case isInRelease:
		metaLocalFiles = []string{localReleaseFile}
		metaURLList = []string{releaseFile}
	case pbkeyIsURL:
		metaLocalFiles = []string{localReleaseFile, localReleaseSign, localPBGPGKey}
		metaURLList = []string{releaseFile, releaseSign, pbGPGKey}
	default:
		metaLocalFiles = []string{localReleaseFile, localReleaseSign}
		metaURLList = []string{releaseFile, releaseSign}
	}

	// checkableReleaseFile always holds a plain Release body: the classic
	// Release file as-is, or (for InRelease repos) the plaintext extracted
	// from InRelease once verified below. Downstream Release parsing
	// (warnIfReleaseExpired, findChecksumInRelease) reads this, never
	// localReleaseFile directly, so it never has to deal with the
	// clearsign wrapper.
	checkableReleaseFile := localReleaseFile
	if isInRelease {
		checkableReleaseFile = localReleaseFile + ".plain"
	}

	// Re-fetch the Release file every online run so the cache is validated rather
	// than trusted. Release (plus its signature) is a few tens of KB against a
	// Packages index measured in tens of MB, so this costs little and is what makes
	// staleness detectable at all: the checksum it carries is the cache key used
	// below for both the parse cache and Packages.gz.
	//
	// Staged into a temp dir and committed only on success, so a refresh that fails
	// midway leaves the existing metadata intact for an offline build. A missing
	// local file makes the refresh mandatory; otherwise a failure is a warning and
	// the build proceeds against what it already has.
	haveLocalMeta := true
	for _, f := range metaLocalFiles {
		if _, err := os.Stat(f); err != nil {
			haveLocalMeta = false
			break
		}
	}
	// The plaintext derived from InRelease is not fetched over the network
	// (metaLocalFiles/metaURLList only cover it), so its absence has to be
	// checked separately: a cache holding the raw InRelease file but missing
	// its extracted plaintext (e.g. from a cache written before this derived
	// file existed) has everything needed to derive it locally, so do that
	// instead of forcing a refresh — which would be fatal for an otherwise
	// usable offline build. Only fall back to requiring a refresh if the
	// cached InRelease file itself is missing or fails to verify.
	if isInRelease {
		if _, err := os.Stat(checkableReleaseFile); err != nil {
			if plaintext, verifyErr := VerifyInRelease(localReleaseFile, localPBGPGKey); verifyErr == nil {
				if writeErr := os.WriteFile(checkableReleaseFile, plaintext, 0644); writeErr != nil {
					haveLocalMeta = false
				}
			} else {
				haveLocalMeta = false
			}
		}
	}

	refreshed, refreshErr := refreshRepoMetadata(pkgMetaDir, metaLocalFiles, metaURLList)
	switch {
	case refreshErr != nil && !haveLocalMeta:
		// Nothing cached to fall back to, so this is fatal — as it was before.
		return nil, fmt.Errorf("failed to fetch critical repo config packages: %w", refreshErr)
	case refreshErr != nil:
		log.Warnf("Could not refresh metadata for %s (%v); continuing with previously "+
			"downloaded metadata, which may name package versions the repository no "+
			"longer serves", baseURL, refreshErr)
		warnIfReleaseExpired(checkableReleaseFile, baseURL)
	default:
		log.Infof("Refreshed metadata files for %s", baseURL)
	}

	// Verify the release file whenever it came off the network this run; metadata
	// we could not refresh was verified when it was originally fetched.
	if refreshed {
		if isInRelease {
			plaintext, err := VerifyInRelease(localReleaseFile, localPBGPGKey)
			if err != nil {
				return nil, fmt.Errorf("failed to verify InRelease file: %w", err)
			}
			if err := os.WriteFile(checkableReleaseFile, plaintext, 0644); err != nil {
				return nil, fmt.Errorf("failed to write extracted Release plaintext: %w", err)
			}
		} else {
			relVryResult, err := VerifyRelease(localReleaseFile, localReleaseSign, localPBGPGKey)
			if err != nil {
				return nil, fmt.Errorf("failed to verify release file: %w", err)
			}
			if !relVryResult {
				return nil, fmt.Errorf("release file verification failed")
			}
		}
	} else {
		log.Debugf("Skipping release file verification (using cached offline files)")
	}

	// verify the sham256 checksum of the Packages.gz file
	log.Infof("verifying checksum of package metadata file %s %s", baseURL, localPkggzFile)
	component := "main"
	if parsedURL, parseErr := url.Parse(pkggz); parseErr == nil {
		parts := strings.Split(strings.Trim(parsedURL.Path, "/"), "/")
		for index, part := range parts {
			if strings.HasPrefix(part, "binary-") && index > 0 {
				component = parts[index-1]
				break
			}
		}
	}

	// Retrieve the expected SHA256 for Packages.gz from the Release file.
	// This serves as the authoritative cache key for the download cache.
	pkgPathSrch := fmt.Sprintf("%s/binary-%s/%s", component, arch, metadataFileName(pkggz))
	expectedChecksum, err := findChecksumInRelease(checkableReleaseFile, "SHA256", pkgPathSrch)
	if err != nil {
		return nil, fmt.Errorf("failed to get checksum from Release file: %w", err)
	}

	// --- Parse cache check (validated) ---
	// Now that the current Release has been read, the parse cache can be used —
	// but only if it was built from the same Packages index. A mismatch means the
	// repository has published a new index, so the cache is discarded and the
	// metadata re-downloaded and re-parsed below.
	if cached != nil {
		if strings.EqualFold(cached.Checksum, expectedChecksum) {
			log.Infof("Using cached package metadata for %s (checksum %s)", baseURL, cached.Checksum)
			return cached.Packages, nil
		}
		log.Infof("Cached package metadata for %s is stale (cached checksum %s, repository now %s); re-parsing",
			baseURL, cached.Checksum, expectedChecksum)
	}

	// --- Download cache check ---
	// Only re-download Packages.gz when the local copy is absent or has a different checksum.
	needsDownload := true
	if fi, statErr := os.Stat(localPkggzFile); statErr == nil && fi.Size() > 0 {
		actual, csErr := computeFileSHA256(localPkggzFile)
		if csErr == nil && strings.EqualFold(actual, expectedChecksum) {
			log.Infof("Packages.gz already up-to-date, skipping download")
			needsDownload = false
		}
	}
	if needsDownload {
		if _, statErr := os.Stat(localPkggzFile); statErr == nil {
			if remErr := os.Remove(localPkggzFile); remErr != nil {
				return nil, fmt.Errorf("failed to remove stale Packages.gz: %w", remErr)
			}
		}
		metadataURL, parseErr := url.Parse(pkggz)
		if parseErr != nil {
			return nil, fmt.Errorf("parse package metadata URL: %w", parseErr)
		}
		query := metadataURL.Query()
		query.Set("ict-release-sha256", expectedChecksum)
		metadataURL.RawQuery = query.Encode()
		if fetchErr := pkgfetcher.FetchPackages(runctx.Context(), []string{metadataURL.String()}, pkgMetaDir, 1); fetchErr != nil {
			return nil, fmt.Errorf("failed to fetch Packages.gz: %w", fetchErr)
		}
	}

	// Authoritative checksum verification after download
	pkggzVryResult, err := VerifyPackagegz(checkableReleaseFile, localPkggzFile, arch, component)
	if err != nil {
		return nil, fmt.Errorf("failed to verify pkg file: %w", err)
	}
	if !pkggzVryResult {
		return nil, fmt.Errorf("package file verification failed")
	}

	//Decompress the Packages (xz or gz) file
	// The decompressed file will be named as Packages
	PkgMetaFile := filepath.Join(pkgMetaDir, metadataFileName(pkggz))
	pkgMetaFileNoExt := filepath.Join(filepath.Dir(PkgMetaFile), strings.TrimSuffix(filepath.Base(PkgMetaFile), filepath.Ext(PkgMetaFile)))
	log.Infof("decompressing package metadata file %s to %s", PkgMetaFile, pkgMetaFileNoExt)

	files, err := Decompress(PkgMetaFile, pkgMetaFileNoExt)
	if err != nil {
		return []ospackage.PackageInfo{}, fmt.Errorf("failed package decompress: %w", err)
	}
	log.Infof("decompressed files: %v", files)

	//Parse the decompressed file
	if len(files) == 0 {
		return nil, fmt.Errorf("no decompressed files found")
	}
	f, err := os.Open(files[0])
	if err != nil {
		return nil, fmt.Errorf("failed to open decompressed file: %w", err)
	}
	defer f.Close()

	var pkgs []ospackage.PackageInfo
	pkg := ospackage.PackageInfo{}
	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("error reading file: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")

		if line == "" {
			// End of one package entry
			if pkg.Name != "" {
				if matchesPackageFilter(pkg.Name, packageFilter) {
					pkgs = append(pkgs, pkg)
				}
				pkg = ospackage.PackageInfo{}
			}
			if err == io.EOF {
				break
			}
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			if err == io.EOF {
				break
			}
			continue
		}

		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch key {
		case "Package":
			pkg.Name = val
			pkg.Type = "deb"
		case "Version":
			pkg.Version = val
		case "Pre-Depends":
			// Split dependencies by comma and clean each dependency. Pre-Depends is
			// stored in RequiresVer too (like Depends) so the OR-alternative selection
			// and version-constraint logic apply to its "a | b" and versioned terms.
			deps := strings.Split(val, ",")
			pkg.RequiresVer = append(pkg.RequiresVer, deps...)
			for _, dep := range deps {
				cleanedDep := CleanDependencyName(dep)
				if cleanedDep != "" {
					pkg.Requires = append(pkg.Requires, cleanedDep)
				}
			}
		case "Depends":
			// Split dependencies by comma and clean each dependency
			deps := strings.Split(val, ",")
			pkg.RequiresVer = append(pkg.RequiresVer, deps...)
			for _, dep := range deps {
				cleanedDep := CleanDependencyName(dep)
				if cleanedDep != "" {
					pkg.Requires = append(pkg.Requires, cleanedDep)
				}
			}
		case "Breaks":
			// Store the raw Breaks terms (comma-separated "name [(op ver)]"). Unlike
			// Depends they are not cleaned here: the overlay resolver needs the version
			// constraint to decide whether a baseline package must be upgraded to clear
			// a versioned break (rather than removed). Debian policy forbids "|"
			// alternatives in Breaks, so each comma term is a single package.
			for _, term := range strings.Split(val, ",") {
				if term = strings.TrimSpace(term); term != "" {
					pkg.Breaks = append(pkg.Breaks, term)
				}
			}
		case "Provides":
			// Provides forbids "|" alternatives per Debian policy, like Breaks, so
			// each comma term is a single package. Keep the raw terms too (as
			// ProvidesVer, mirroring Breaks) so a versioned dependency on a virtual
			// name can be checked against the version this package actually
			// declares for it, not against this package's own Version.
			for _, term := range strings.Split(val, ",") {
				if term = strings.TrimSpace(term); term != "" {
					pkg.ProvidesVer = append(pkg.ProvidesVer, term)
				}
			}

			// Split provides by comma and trim spaces, remove version constraints
			deps := strings.Split(val, ",")
			for i := range deps {
				dep := strings.TrimSpace(deps[i])
				// Remove version constraints, e.g. "foo (= 1.2)" -> "foo"
				if idx := strings.Index(dep, " "); idx > 0 {
					dep = dep[:idx]
				}
				deps[i] = dep
			}
			pkg.Provides = deps
		case "Filename":
			pkg.URL, _ = getFullUrl(val, baseURL)
		case "SHA256":
			pkg.Checksums = append(pkg.Checksums, ospackage.Checksum{
				Algorithm: "SHA256",
				Value:     val,
			})

		case "SHA1":
			pkg.Checksums = append(pkg.Checksums, ospackage.Checksum{
				Algorithm: "SHA1",
				Value:     val,
			})
		case "SHA512":
			pkg.Checksums = append(pkg.Checksums, ospackage.Checksum{
				Algorithm: "SHA512",
				Value:     val,
			})
		case "Installed-Size":
			// Installed-Size is the estimated unpacked footprint in KiB; store it in
			// bytes. A malformed value, or one whose ×1024 conversion would overflow
			// int64, is treated as "unknown" (HasInstalledSize stays false) rather than
			// fatal — it only feeds an overlay disk-size estimate, never correctness.
			if kib, perr := strconv.ParseInt(val, 10, 64); perr == nil && kib >= 0 && kib <= math.MaxInt64/1024 {
				pkg.InstalledSizeBytes = kib * 1024
				pkg.HasInstalledSize = true
			}
		case "Description":
			pkg.Description = val
		case "Architecture":
			if val == "all" || val == "any" {
				pkg.Arch = "noarch"
			} else {
				pkg.Arch = val
			}
		case "Maintainer":
			pkg.Origin = val
		}
		if err == io.EOF {
			break
		}

	}

	// Add the last package if file doesn't end with a blank line
	if pkg.Name != "" {
		if matchesPackageFilter(pkg.Name, packageFilter) {
			pkgs = append(pkgs, pkg)
		}
	}

	// Persist the parsed result so future calls with the same checksum skip decompression/parsing.
	if allowParsedCache {
		if saveErr := saveParsedPackageCache(cacheFile, expectedChecksum, pkgs); saveErr != nil {
			log.Warnf("failed to save package metadata cache: %v", saveErr)
		}
	}

	return pkgs, nil
}

// getRepositoryPriority returns the priority for a given repository URL
func getRepositoryPriority(packageURL string) int {
	repoBase, err := extractRepoBase(packageURL)
	if err != nil {
		return 0 // Default priority if we can't extract repo base
	}

	// Check global RepoCfgs for priority
	// Normalize trailing slashes before comparing, since user-supplied URLs may include them
	// but extractRepoBase always returns a URL without a trailing slash.
	repoBaseNorm := strings.TrimSuffix(repoBase, "/")
	if len(RepoCfgs) > 0 {
		for _, repoCfg := range RepoCfgs {
			if strings.TrimSuffix(repoCfg.PkgPrefix, "/") == repoBaseNorm {
				return repoCfg.Priority
			}
		}
	}

	for _, repoCfg := range UserRepoCfgs {
		if strings.TrimSuffix(repoCfg.PkgPrefix, "/") == repoBaseNorm {
			return repoCfg.Priority
		}
	}

	for _, repoCfg := range LocalUserRepoCfgs {
		if strings.TrimSuffix(repoCfg.PkgPrefix, "/") == repoBaseNorm {
			return repoCfg.Priority
		}
	}

	// Check single RepoCfg for backward compatibility
	if strings.TrimSuffix(RepoCfg.PkgPrefix, "/") == repoBaseNorm {
		return RepoCfg.Priority
	}

	return 0 // Default priority
}

// APT Priority behavior functions

// shouldBlockPackage returns true if the package should be blocked based on priority < 0
func shouldBlockPackage(pkg ospackage.PackageInfo) bool {
	priority := getRepositoryPriority(pkg.URL)
	return priority < 0
}

// shouldForceInstall returns true if the package should be force installed (priority > 1000)
func shouldForceInstall(pkg ospackage.PackageInfo) bool {
	priority := getRepositoryPriority(pkg.URL)
	return priority > 1000
}

// shouldInstallEvenIfLower returns true if the package should be installed even if version is lower (priority = 1000)
func shouldInstallEvenIfLower(pkg ospackage.PackageInfo) bool {
	priority := getRepositoryPriority(pkg.URL)
	return priority == 1000
}

// shouldPrefer returns true if the package should be preferred (priority = 990)
func shouldPrefer(pkg ospackage.PackageInfo) bool {
	priority := getRepositoryPriority(pkg.URL)
	return priority == 990
}

// filterCandidatesByPriority filters out blocked packages and applies priority-based sorting
func filterCandidatesByPriority(candidates []ospackage.PackageInfo) []ospackage.PackageInfo {
	var filtered []ospackage.PackageInfo

	// First pass: filter out blocked packages (priority < 0)
	for _, candidate := range candidates {
		if !shouldBlockPackage(candidate) {
			filtered = append(filtered, candidate)
		}
	}

	// Sort by APT priority rules
	sort.Slice(filtered, func(i, j int) bool {
		pkgI := filtered[i]
		pkgJ := filtered[j]

		priorityI := getRepositoryPriority(pkgI.URL)
		priorityJ := getRepositoryPriority(pkgJ.URL)

		// Force install (>1000) has highest preference
		forceI := shouldForceInstall(pkgI)
		forceJ := shouldForceInstall(pkgJ)
		if forceI != forceJ {
			return forceI // Force install comes first
		}

		// Install even if lower (1000) has next preference
		lowerI := shouldInstallEvenIfLower(pkgI)
		lowerJ := shouldInstallEvenIfLower(pkgJ)
		if lowerI != lowerJ {
			return lowerI
		}

		// Preferred (990) comes next
		preferI := shouldPrefer(pkgI)
		preferJ := shouldPrefer(pkgJ)
		if preferI != preferJ {
			return preferI
		}

		// For same priority category, use numerical priority comparison
		if priorityI != priorityJ {
			return priorityI > priorityJ
		}

		// Finally, compare by version (highest version first)
		return compareVersions(pkgI.Version, pkgJ.Version) > 0
	})

	return filtered
}

// filterCandidatesByPriorityWithTarget filters and sorts candidates, prioritizing exact name matches
func filterCandidatesByPriorityWithTarget(candidates []ospackage.PackageInfo, targetName string) []ospackage.PackageInfo {
	log := logger.Logger()
	var filtered []ospackage.PackageInfo

	// First pass: filter out blocked packages (priority < 0)
	for _, candidate := range candidates {
		if !shouldBlockPackage(candidate) {
			filtered = append(filtered, candidate)
		}
	}

	// firstSeen records each provider name's first index in the filtered slice so the
	// provides tiebreak below is a TOTAL order. Ordering different providers by their
	// first-seen position (rather than treating them as equal) keeps sort.Slice's
	// comparator transitive: without it, an interleaving like [A@1, B@1, A@3] can leave
	// A@1 ahead of A@3, so a virtual request would still resolve to the oldest build.
	firstSeen := make(map[string]int, len(filtered))
	for idx, candidate := range filtered {
		if _, ok := firstSeen[candidate.Name]; !ok {
			firstSeen[candidate.Name] = idx
		}
	}

	// Sort by simple rule: exact name matches first, then provides matches
	sort.Slice(filtered, func(i, j int) bool {
		pkgI := filtered[i]
		pkgJ := filtered[j]

		isExactI := pkgI.Name == targetName
		isExactJ := pkgJ.Name == targetName

		// Simple rule: exact name matches always win over provides
		if isExactI != isExactJ {
			log.Debugf("    Exact match priority: %s (exact=%v) vs %s (exact=%v) -> %s wins",
				pkgI.Name, isExactI, pkgJ.Name, isExactJ,
				func() string {
					if isExactI {
						return pkgI.Name
					} else {
						return pkgJ.Name
					}
				}())
			return isExactI
		}

		// For same type (both exact or both provides), use standard APT priority + version
		priorityI := getRepositoryPriority(pkgI.URL)
		priorityJ := getRepositoryPriority(pkgJ.URL)

		// APT priority comparison
		if priorityI != priorityJ {
			return priorityI > priorityJ
		}

		// Only compare versions for exact matches (avoid kernel vs dkms version comparison)
		if isExactI && isExactJ {
			versionCmp := compareVersions(pkgI.Version, pkgJ.Version) > 0
			log.Debugf("    Exact match version comparison: %s (%s) vs %s (%s) -> %s wins",
				pkgI.Name, pkgI.Version, pkgJ.Name, pkgJ.Version,
				func() string {
					if versionCmp {
						return pkgI.Name
					} else {
						return pkgJ.Name
					}
				}())
			return versionCmp
		}

		// Both are provides matches for the target virtual name. Across DIFFERENT
		// provider packages the version schemes are not comparable, so keep a stable
		// order. But when both candidates are the SAME real package at different
		// versions (e.g. two libssl3t64 builds both providing the virtual "libssl3"),
		// rank the newer one first: otherwise a virtual-name dependency resolves to
		// whichever version the Packages file happened to list first (effectively the
		// oldest), which then fails a later exact "= <newer>" pin on the real package.
		if pkgI.Name == pkgJ.Name {
			return compareVersions(pkgI.Version, pkgJ.Version) > 0
		}
		// Different providers of the same virtual name: order by first-seen position so
		// the comparator stays a total order (see firstSeen above).
		return firstSeen[pkgI.Name] < firstSeen[pkgJ.Name]
	})

	return filtered
}

// comparePriorityBehavior compares two packages based on APT priority behavior
// Returns true if pkgA should be preferred over pkgB
func comparePriorityBehavior(pkgA, pkgB ospackage.PackageInfo) bool {
	// Block packages with negative priority
	if shouldBlockPackage(pkgA) {
		return false
	}
	if shouldBlockPackage(pkgB) {
		return true
	}

	// Force install (>1000) beats everything else
	if shouldForceInstall(pkgA) && !shouldForceInstall(pkgB) {
		return true
	}
	if shouldForceInstall(pkgB) && !shouldForceInstall(pkgA) {
		return false
	}

	// Install even if lower (1000) beats lower priorities
	if shouldInstallEvenIfLower(pkgA) && !shouldInstallEvenIfLower(pkgB) && !shouldForceInstall(pkgB) {
		return true
	}
	if shouldInstallEvenIfLower(pkgB) && !shouldInstallEvenIfLower(pkgA) && !shouldForceInstall(pkgA) {
		return false
	}

	// Preferred (990) beats default and lower
	if shouldPrefer(pkgA) && !shouldPrefer(pkgB) && !shouldInstallEvenIfLower(pkgB) && !shouldForceInstall(pkgB) {
		return true
	}
	if shouldPrefer(pkgB) && !shouldPrefer(pkgA) && !shouldInstallEvenIfLower(pkgA) && !shouldForceInstall(pkgA) {
		return false
	}

	// For same priority category, compare versions
	priorityA := getRepositoryPriority(pkgA.URL)
	priorityB := getRepositoryPriority(pkgB.URL)

	if priorityA == priorityB {
		// Special handling for priority 1000 - can install even if version is lower
		if priorityA == 1000 {
			return true // Accept either package for priority 1000
		}

		// For other priorities, prefer higher version
		return compareVersions(pkgA.Version, pkgB.Version) > 0
	}

	// Different priorities - higher numerical priority wins
	return priorityA > priorityB
}

// ResolveDependencies takes a seed list of PackageInfos (the exact versions
// matched) and the full list of all PackageInfos from the repo, and
// returns the minimal closure of PackageInfos needed to satisfy all Requires.
func ResolveDependencies(requested []ospackage.PackageInfo, all []ospackage.PackageInfo) ([]ospackage.PackageInfo, error) {
	log := logger.Logger()

	// Build maps for fast lookup
	byNameVer := make(map[string]ospackage.PackageInfo, len(all))
	for _, pi := range all {
		if pi.Version != "" {
			key := fmt.Sprintf("%s=%s", pi.Name, pi.Version)
			byNameVer[key] = pi
		}
	}
	neededSet := make(map[string]struct{})
	resolvedDeps := make(map[string]ospackage.PackageInfo) // Track resolved dependencies for conflict detection
	// depVersionConstraints accumulates every version constraint any processed
	// parent has placed on a given dependency name, not just the one currently
	// being checked. A constraint-driven replacement (below) must be checked
	// against this full history: filtering only against the CURRENT parent's
	// constraint could pick a candidate that satisfies the parent triggering
	// the check while breaking an earlier parent's already-satisfied (and
	// otherwise unrevisited) requirement on the same name.
	depVersionConstraints := make(map[string][]VersionConstraint)
	// requiredDepNames records every dependency name actually traversed as a
	// real Requires edge (including an OR edge's resolved alternative), as
	// opposed to rememberResolvedDependency's own-name bookkeeping alias it
	// sets for EVERY selected provider whether or not anything ever depended
	// on that name directly. packageStillRequired must only treat the former
	// as evidence a package is independently needed.
	requiredDepNames := make(map[string]struct{})
	// seedByName holds the explicitly requested packages (the resolution seed) keyed
	// by name. It lets an OR-dependency prefer an alternative the caller already asked
	// for, even before that alternative has been dequeued into neededSet/resolvedDeps,
	// and carries the requested version so a versioned alternative can be evaluated.
	seedByName := make(map[string]ospackage.PackageInfo, len(requested))
	for _, pi := range requested {
		seedByName[pi.Name] = pi
	}
	// selectedVersionsLookup returns every version name is already selected
	// under (as a resolved dependency, a requested seed, or via either's
	// Provides:) — potentially more than one, since different selected
	// packages can Provide the same virtual name at different versions. An
	// empty-string entry means "selected but concrete version unknown". A nil
	// slice means name is not selected at all. Shared by the OR-dependency
	// skip decision below and the stale-OR-edge constraint skip in the
	// replacement path further down: callers must check the constraint
	// against EVERY returned version, not just the first — collapsing to one
	// arbitrary match (e.g. by stopping at the first Provides: hit found
	// while ranging over a map) can miss a DIFFERENT selected provider that
	// actually satisfies the alternative's own version constraint.
	selectedVersionsLookup := func(name string) []string {
		var versions []string
		add := func(p ospackage.PackageInfo) {
			ver, _ := versionForDependency(p, name)
			versions = append(versions, ver)
		}
		if p, ok := resolvedDeps[name]; ok {
			add(p)
		}
		if p, ok := seedByName[name]; ok {
			add(p)
		}
		for _, p := range resolvedDeps {
			for _, provided := range p.Provides {
				if CleanDependencyName(provided) == name {
					add(p)
				}
			}
		}
		for _, p := range seedByName {
			for _, provided := range p.Provides {
				if CleanDependencyName(provided) == name {
					add(p)
				}
			}
		}
		if len(versions) == 0 {
			if _, ok := neededSet[name]; ok {
				versions = append(versions, "")
			}
		}
		return versions
	}

	queue := make([]ospackage.PackageInfo, 0, len(requested))
	for _, pi := range requested {
		if pi.Version != "" {
			key := fmt.Sprintf("%s=%s", pi.Name, pi.Version)
			if pkg, ok := byNameVer[key]; ok {
				queue = append(queue, pkg)
				rememberResolvedDependency(resolvedDeps, pkg.Name, pkg)
				continue
			}
		}
		return nil, fmt.Errorf("requested package %q not in repo listing", pi.Name)
	}

	// depedencies resolution logic
	result := make([]ospackage.PackageInfo, 0)
	var parentChildPairs [][]ospackage.PackageInfo // Track parent->child relationships for reporting
	gotMissingPkg := false

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		if _, seen := neededSet[cur.Name]; seen {
			continue
		}
		neededSet[cur.Name] = struct{}{}
		result = append(result, cur)

		// Track in resolvedDeps so later packages reuse this version
		if _, alreadyResolved := resolvedDeps[cur.Name]; !alreadyResolved {
			rememberResolvedDependency(resolvedDeps, cur.Name, cur)
		}

		// Traverse dependencies
		for _, dep := range cur.Requires {

			depName := CleanDependencyName(dep)
			if depName == "" {
				continue
			}
			// OR-dependency ("a | b") handling: cur.Requires carries only the FIRST
			// alternative (depName). apt takes the first alternative UNLESS another
			// alternative is already installed/selected AND satisfies its version
			// constraint, in which case the edge is already met and the first must NOT
			// be pulled in — pulling it can drag in a package that conflicts with the
			// already-selected one (e.g. va-driver-all's
			// "intel-media-va-driver | intel-media-va-driver-non-free" pulling the free
			// driver even though the non-free one was requested). Only the requested
			// seed and already-resolved packages count as "selected"; the baseline is
			// not visible to this repo-only resolver.
			if alternativeAlreadySelected(cur.RequiresVer, depName, selectedVersionsLookup) {
				// depName itself was never pulled in for this edge — the alternative
				// that actually satisfied it is the genuine dependency, not depName.
				// Recording depName here would let an obsolete alias (rememberResolvedDependency's
				// own-name bookkeeping under depName, from some unrelated earlier
				// resolution) masquerade as "still required" by this edge.
				for _, alt := range satisfiedAlternativeNames(cur.RequiresVer, depName, selectedVersionsLookup) {
					requiredDepNames[alt] = struct{}{}
				}
				continue
			}
			requiredDepNames[depName] = struct{}{}
			if resolvedPkg, seen := resolvedDeps[depName]; seen {
				// Dependency already resolved - check for version conflicts

				// Check if this is a direct dependency without constraints
				isDirect := hasDirectDependency(cur.Requires, depName)

				// Extract version constraints for this dependency from current package
				versionConstraints, hasVersionConstraint := extractVersionRequirement(cur.RequiresVer, depName)

				// If it's a direct dependency, ignore version constraints from alternatives
				if isDirect && hasVersionConstraint {
					// Filter out constraints that come from alternatives (keep only direct constraints)
					var directConstraints []VersionConstraint
					for _, constraint := range versionConstraints {
						// If constraint has alternatives, it's from an alternative requirement
						if constraint.Alternative == "" {
							directConstraints = append(directConstraints, constraint)
						}
					}
					// hasDirectDependency cannot tell a truly bare dependency apart
					// from depName merely being an OR term's own first alternative
					// (both put depName in Requires). Decide per original dependency
					// term, not from whether depName is a first alternative anywhere:
					// strip alternative-tagged constraints when depName is NOT its OR
					// term's own first alternative, OR when depName ALSO appears as a
					// separate bare, mandatory term. In "logsave | e2fsprogs (<< X)"
					// with e2fsprogs also a bare Requires entry, e2fsprogs is
					// unconditionally required regardless of that unrelated OR clause,
					// so its constraint must not apply. Likewise "Depends: foo,
					// foo (= 1) | bar": foo is mandatory unconditionally, so the OR
					// term's "= 1" pin must not be enforced against the mandatory foo
					// (that edge is satisfiable via bar). Only for a pure OR term like
					// "libfoo-abi (= 3) | fallback", where libfoo-abi is the term's own
					// first alternative and not independently mandatory, is its pin
					// enforced when selecting its providers.
					if !isOwnFirstAlternative(cur.RequiresVer, depName) || hasBareMandatoryTerm(cur.RequiresVer, depName) {
						versionConstraints = directConstraints
						hasVersionConstraint = len(directConstraints) > 0
					}
				}

				// The strip above only concerns whether resolvedPkg should be
				// REPLACED for the bare mandatory term. When depName is ALSO an OR
				// term's own first alternative (e.g. "Depends: foo, foo (= 1) |
				// bar"), that OR term is a genuinely separate edge that the bare
				// term's mandatory presence does not satisfy on its own — apt still
				// requires foo (= 1) OR bar. Check each such edge independently and
				// pull in its alternative when neither resolvedPkg nor an already
				// selected alternative satisfies it, instead of silently treating
				// the edge as met.
				if isDirect && isOwnFirstAlternative(cur.RequiresVer, depName) && hasBareMandatoryTerm(cur.RequiresVer, depName) {
					var missing bool
					queue, missing = resolveUnmetOwnOREdges(cur, resolvedPkg, depName, all, selectedVersionsLookup,
						queue, resolvedDeps, requiredDepNames, depVersionConstraints, &parentChildPairs)
					gotMissingPkg = gotMissingPkg || missing
				}

				if hasVersionConstraint {
					// Record these constraints for depName so a future replacement
					// decision (for this or another parent) considers the full
					// accumulated history, not just whichever parent is being
					// checked at that moment.
					depVersionConstraints[depName] = append(depVersionConstraints[depName], versionConstraints...)

					var requiredVer string
					var requiredDep string
					// Check if the already-resolved package satisfies the version constraints
					constraintsSatisfied := true
					for _, constraint := range versionConstraints {
						// Check if main package satisfies constraint. resolvedPkg is only
						// ONE of possibly several already-selected providers of depName — a
						// virtual capability can be independently satisfied by more than one
						// real package (each pulled in via a different OR term naming the
						// same first alternative) — so every version depName is currently
						// selected under is checked, not just resolvedPkg's own, or a
						// DIFFERENT already-satisfied edge on the same virtual name would be
						// wrongly reported as a conflict.
						mainSatisfied := false
						if constraint.Op != "" && constraint.Ver != "" {
							for _, ver := range selectedVersionsLookup(depName) {
								if ver != "" && debVersionSatisfies(ver, constraint.Op, constraint.Ver) {
									mainSatisfied = true
									break
								}
							}
						}

						// If main package doesn't satisfy and we have alternatives, check them —
						// using AlternativeTerms (raw, versioned terms) rather than the bare
						// cleaned names in Alternative, so an alternative's OWN version
						// constraint (e.g. "bar (>= 2)") is actually evaluated instead of
						// treating any selected version of it as satisfying.
						alternativeSatisfied := false
						if !mainSatisfied && constraint.AlternativeTerms != "" {
							_, alternativeSatisfied = edgeSatisfyingAlternative(strings.Split(constraint.AlternativeTerms, "|"), selectedVersionsLookup)
						}

						if !mainSatisfied && !alternativeSatisfied {
							constraintsSatisfied = false
							requiredVer = constraint.Ver
							requiredDep = depName
							break
						}
					}

					if !constraintsSatisfied {
						// Before throwing error, check if there's a higher priority candidate
						// available. Always filter against every constraint any processed
						// parent has recorded for depName (not just cur's, and regardless of
						// whether any of them is exact): a candidate that makes it through
						// this filter by definition satisfies every exact pin too, so gating
						// the search itself on "is any recorded constraint exact" would reject
						// perfectly valid replacements (e.g. an earlier ">= 1.0" plus a new
						// "= 2.0" both accepting version 2.0) before ever looking for one.
						candidates := findAllCandidates(depName, all)

						if len(candidates) > 0 {
							// Find candidates that satisfy EVERY constraint any processed
							// parent has placed on depName, not just cur's — otherwise the
							// replacement could satisfy cur while silently breaking an
							// earlier parent's already-verified requirement.
							var satisfyingCandidates []ospackage.PackageInfo
							for _, candidate := range candidates {
								candidateSatisfies := true
								for _, constraint := range depVersionConstraints[depName] {
									if constraint.Op != "" && constraint.Ver != "" {
										// A constraint recorded from an OR edge (e.g. "foo (= 1) |
										// bar (>= 2)") is obsolete once that edge is satisfied by a
										// DIFFERENT, already-selected alternative — the parent it
										// came from no longer needs depName's specific version,
										// so enforcing it here would report phantom conflicts
										// against unrelated later requirements. Uses AlternativeTerms
										// (the raw, versioned terms) so the alternative's OWN version
										// constraint is actually checked, and the same provider-aware
										// selectedVersionsLookup as the OR-dependency skip decision
										// above, so a virtual capability satisfied via Provides: (not
										// just a package selected under its own name) is recognized
										// too, across EVERY selected provider of that name rather than
										// an arbitrary single match.
										if constraint.AlternativeTerms != "" && edgeSatisfiedByOtherAlternative(strings.Split(constraint.AlternativeTerms, "|"), selectedVersionsLookup) {
											continue
										}
										if ver, ok := versionForDependency(candidate, depName); ok && debVersionSatisfies(ver, constraint.Op, constraint.Ver) {
											continue
										}
										// The candidate itself doesn't meet this constraint. When it
										// came from an OR edge, that edge can still be satisfied by
										// resolving its OWN fallback alternative instead of depName —
										// the edge's fallback was only unselected so far because
										// depName itself used to satisfy it directly; apt's "|"
										// semantics don't require every future replacement of depName
										// to keep meeting a pin that merely existed to satisfy that
										// edge, as long as the edge is met some other way. Actually
										// queuing the resolved fallback happens once, after the final
										// replacement candidate is committed to below — not here,
										// since this loop only evaluates whether THIS candidate is
										// acceptable.
										if constraint.AlternativeTerms != "" {
											if _, _, _, bridgeable := bridgeableOREdge(cur, reconstructOREdgeTerm(depName, constraint), all, selectedVersionsLookup); bridgeable {
												continue
											}
										}
										candidateSatisfies = false
										break
									}
								}
								// A candidate satisfying depName's own constraints can still
								// violate a genuine constraint some OTHER parent placed
								// directly on the candidate's OWN real-package name — e.g. a
								// newer "provider" version chosen here to satisfy a virtual
								// capability, when an earlier parent pinned "provider (= 1)"
								// directly. Two versions of the SAME package name cannot
								// coexist in the closure, so this must be rejected at
								// selection time (forcing either a different candidate or a
								// genuine conflict below) rather than relying on
								// packageStillRequired, which can only keep-or-drop the OLD
								// package and cannot un-pick an already-chosen replacement.
								if candidateSatisfies && candidate.Name != depName {
									if !aliasVersionCovered(candidate, candidate.Name, depVersionConstraints[candidate.Name]) {
										candidateSatisfies = false
									}
								}
								if candidateSatisfies {
									satisfyingCandidates = append(satisfyingCandidates, candidate)
								}
							}

							if len(satisfyingCandidates) > 0 {
								// Pick the best candidate using the resolver
								newCandidate, err := resolveMultiCandidates(cur, depName, satisfyingCandidates)
								if err == nil {
									// The resolved package violates a version constraint
									// required by the current package. A candidate that
									// satisfies the constraint exists, so replace
									// unconditionally — constraint satisfaction takes
									// precedence over priority.
									log.Debugf("replacing %s_%s with constraint-satisfying version %s_%s (required by %s_%s)",
										resolvedPkg.Name, resolvedPkg.Version,
										newCandidate.Name, newCandidate.Version,
										cur.Name, cur.Version)

									// A same-name replacement (a version bump of the SAME real
									// package) cannot keep the old version around for an alias
									// newCandidate fails to cover: only one version of a given
									// package name can exist in the closure, so the later-queued
									// newCandidate is skipped via neededSet and the OLD,
									// constraint-violating version is silently retained. When the
									// old version uniquely satisfies a still-required capability
									// that newCandidate cannot (e.g. keeping provider v1 for
									// "old-capability (= 1)" while provider v2 is needed for
									// "shared-abi (>= 2)"), the two requirements are genuinely
									// unsatisfiable by a single package name — surface a conflict
									// rather than shipping a version that violates one of them.
									if alias, conflict := uncoveredAliasOnSameNameReplacement(resolvedPkg, newCandidate, resolvedDeps, depName, requiredDepNames, depVersionConstraints); conflict {
										return nil, fmt.Errorf("conflicting package dependencies: cannot replace %s_%s with %s_%s to satisfy %q because the older version is still required for %q",
											resolvedPkg.Name, resolvedPkg.Version, newCandidate.Name, newCandidate.Version, depName, alias)
									}

									// The old package may still independently satisfy an explicit
									// seed, or another already-established resolvedDeps alias
									// (e.g. a different capability it also Provides) that
									// newCandidate does not cover. Removing it outright would
									// silently break that earlier, unrelated requirement without
									// ever re-resolving it, so only drop it from the closure when
									// nothing else still needs it — otherwise both packages are
									// kept, and only depName's own resolution is repointed.
									if _, isSeed := seedByName[resolvedPkg.Name]; isSeed && resolvedPkg.Name == newCandidate.Name {
										// Keep the seed's own tracked version in sync with the
										// replacement: otherwise selectedVersionsLookup would keep
										// reporting the ALREADY-SUPERSEDED requested version as
										// still selected (e.g. a later "foo | bar (= 1)" edge could
										// be wrongly skipped off a stale bar = 1 seed entry after
										// bar was actually replaced with bar = 2).
										seedByName[resolvedPkg.Name] = newCandidate
									}
									if !packageStillRequired(resolvedPkg, depName, resolvedDeps, newCandidate, requiredDepNames, seedByName, depVersionConstraints) {
										// Remove old package from result and neededSet
										delete(neededSet, resolvedPkg.Name)
										for i, pkg := range result {
											if pkg.Name == resolvedPkg.Name && pkg.Version == resolvedPkg.Version {
												result = append(result[:i], result[i+1:]...)
												break
											}
										}

										// The old package may still be sitting unprocessed in queue
										// (resolvedDeps is populated at queue-time, not dequeue-time).
										// Drop any queued copy, and clean up resolvedDeps aliases that
										// pointed at the old package: depName is repointed below, but
										// any OTHER alias (the old package's own name, or another
										// virtual name only it Provides) must be dropped rather than
										// repointed, since newCandidate may not satisfy that alias at
										// all — leaving it pointed at the old package's replacement
										// would make a later direct dependency on that alias wrongly
										// appear pre-resolved instead of triggering fresh resolution.
										queue = replaceQueuedAndAliasedDependency(queue, resolvedDeps, resolvedPkg, newCandidate, depVersionConstraints)
									}

									// A recorded OR-edge constraint that newCandidate itself doesn't
									// meet, and that no already-selected alternative satisfies
									// either, is exactly why newCandidate was let through above:
									// bridgeableOREdge found its fallback resolvable. Commit that
									// resolution now — resolve and queue the fallback for real — or
									// the "|" it came from would silently go unsatisfied.
									for _, constraint := range depVersionConstraints[depName] {
										if constraint.Op == "" || constraint.Ver == "" || constraint.AlternativeTerms == "" {
											continue
										}
										if ver, ok := versionForDependency(newCandidate, depName); ok && debVersionSatisfies(ver, constraint.Op, constraint.Ver) {
											continue
										}
										reqVer := reconstructOREdgeTerm(depName, constraint)
										altName, altCandidate, needsQueue, bridged := bridgeableOREdge(cur, reqVer, all, selectedVersionsLookup)
										if !bridged || !needsQueue {
											continue
										}
										log.Infof("Successfully resolved alternative %q version %q for OR edge %q left unmet by replacing %s with %s_%s",
											altName, altCandidate.Version, reqVer, depName, newCandidate.Name, newCandidate.Version)
										queue = queueResolvedAlternative(cur, reqVer, altName, altCandidate, queue, resolvedDeps, requiredDepNames, depVersionConstraints, &parentChildPairs)
									}

									// Add new candidate to queue and resolvedDeps
									queue = append(queue, newCandidate)
									rememberResolvedDependency(resolvedDeps, depName, newCandidate)
									AddParentChildPair(cur, newCandidate, &parentChildPairs)
									continue
								}
							}
						}
						return nil, fmt.Errorf("conflicting package dependencies: %s_%s requires %s_%s, but %s_%s is already installed", cur.Name, cur.Version, requiredDep, requiredVer, resolvedPkg.Name, resolvedPkg.Version)
					}
				}
				continue
			}

			candidates := findAllCandidates(depName, all)
			if len(candidates) >= 1 {
				// Record cur's constraints on depName so a later replacement
				// decision for this same dependency (triggered by a different
				// parent) sees this parent's requirement too, not just its own.
				if vcs, hasVC := extractVersionRequirement(cur.RequiresVer, depName); hasVC {
					depVersionConstraints[depName] = append(depVersionConstraints[depName], vcs...)
				}
				// Pick the candidate using the resolver and add it to the queue
				chosenCandidate, err := resolveMultiCandidates(cur, depName, candidates)
				if err != nil {
					// depName has candidates, but none satisfy its own (first
					// alternative) version constraint — e.g. "libfoo-abi (= 3) |
					// fallback" with only libfoo-abi (= 2) available. This can also
					// happen because resolveMultiCandidates aggregates EVERY
					// RequiresVer term naming depName first into one candidate
					// search, which fails whenever two such terms pin mutually
					// exclusive versions even though each is individually
					// satisfiable (e.g. "foo (= 1) | bar, foo (= 2) | baz" with foo
					// (= 1) available: foo (= 1) satisfies the first term directly,
					// and only the second needs its fallback baz). Resolve every
					// raw term independently instead of failing outright.
					chosen, alternatives, okAll := resolveDependencyTermsIndependently(cur, depName, candidates, all)
					for _, res := range alternatives {
						log.Infof("Successfully resolved alternative %q version %q for %q (no primary candidate satisfied its constraint)", res.Name, res.Package.Version, depName)
						// Scope to res.ReqVer (the one OR term that selected this
						// alternative), not every cur.RequiresVer term naming res.Name —
						// otherwise a different, already-satisfied OR edge that happens
						// to share this alternative's name would wrongly contribute its
						// own constraint here too.
						if altVCs, hasAltVC := extractVersionRequirement([]string{res.ReqVer}, res.Name); hasAltVC {
							depVersionConstraints[res.Name] = append(depVersionConstraints[res.Name], altVCs...)
						}
						requiredDepNames[res.Name] = struct{}{}
						queue = append(queue, res.Package)
						rememberResolvedDependency(resolvedDeps, res.Name, res.Package)
						AddParentChildPair(cur, res.Package, &parentChildPairs)
					}
					if chosen != nil {
						log.Infof("Successfully resolved %q version %q to satisfy its own bare/first-alternative term(s)", depName, chosen.Version)
						requiredDepNames[depName] = struct{}{}
						queue = append(queue, *chosen)
						rememberResolvedDependency(resolvedDeps, depName, *chosen)
						AddParentChildPair(cur, *chosen, &parentChildPairs)
					}
					if !okAll {
						gotMissingPkg = true
						AddParentMissingChildPair(cur, depName+"(missing)", &parentChildPairs)
						log.Warnf("failed to resolve multiple candidates for dependency %q of package %q: %v", depName, cur.Name, err)
					}
					continue
				}
				queue = append(queue, chosenCandidate)
				rememberResolvedDependency(resolvedDeps, depName, chosenCandidate) // Track resolved dependency
				AddParentChildPair(cur, chosenCandidate, &parentChildPairs)
				// depName being both bare-mandatory and an OR term's own first
				// alternative means resolveMultiCandidates above picked
				// chosenCandidate with that OR term's version pin stripped (the
				// bare term alone justifies picking it); the OR term is a separate
				// edge that must still be checked against chosenCandidate.
				if isOwnFirstAlternative(cur.RequiresVer, depName) && hasBareMandatoryTerm(cur.RequiresVer, depName) {
					var missing bool
					queue, missing = resolveUnmetOwnOREdges(cur, chosenCandidate, depName, all, selectedVersionsLookup,
						queue, resolvedDeps, requiredDepNames, depVersionConstraints, &parentChildPairs)
					gotMissingPkg = gotMissingPkg || missing
				}
				continue
			} else {
				// No candidates for primary dependency at all — depName can only
				// be satisfied via OR terms' alternatives; bare terms naming
				// depName cannot be satisfied by anything, and are reported below.
				_, alternatives, okAll := resolveDependencyTermsIndependently(cur, depName, nil, all)
				if len(alternatives) > 0 {
					for _, res := range alternatives {
						log.Infof("Successfully resolved alternative %q version %q for missing dependency %q", res.Name, res.Package.Version, depName)
						// Record res.Name's own constraint(s) before registering it:
						// constraint.Alternative only carries alternative NAMES
						// (CleanDependencyName strips any version), so its own
						// version requirement — the one resolveMultiCandidates just
						// used internally to pick res.Package — has to be
						// re-extracted here too, or a later replacement of this
						// alternative would only ever see ITS OWN constraint, never
						// this parent's. Scope to res.ReqVer, not every cur.RequiresVer
						// term naming res.Name, so an unrelated OR edge sharing this
						// alternative's name can't leak its own constraint in here.
						if altVCs, hasAltVC := extractVersionRequirement([]string{res.ReqVer}, res.Name); hasAltVC {
							depVersionConstraints[res.Name] = append(depVersionConstraints[res.Name], altVCs...)
						}
						requiredDepNames[res.Name] = struct{}{}
						queue = append(queue, res.Package)
						rememberResolvedDependency(resolvedDeps, res.Name, res.Package) // Track resolved alternative dependency
						AddParentChildPair(cur, res.Package, &parentChildPairs)
					}
				}
				if !okAll {
					log.Warnf("no candidates found for dependency %q of package %q", depName, cur.Name)
					gotMissingPkg = true
					AddParentMissingChildPair(cur, depName+"(missing)", &parentChildPairs)
				}
				continue
			}
		}
	}

	// check missing dep and write report
	if gotMissingPkg {
		report := BuildDependencyChains(parentChildPairs)
		return nil, fmt.Errorf("one or more requested dependencies not found. See list in %s", report)
	}

	// Sort result by package name for determinism
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result, nil
}

func getFullUrl(filePath string, baseUrl string) (string, error) {
	// Check if the file path is already a full URL
	if strings.HasPrefix(filePath, "http://") || strings.HasPrefix(filePath, "https://") {
		return filePath, nil
	}

	// If not, construct the full URL using the base URL
	fullURL := fmt.Sprintf("%s/%s", strings.TrimSuffix(baseUrl, "/"), filePath)
	return fullURL, nil
}

// CompareDebianVersions compares two Debian version strings.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func CompareDebianVersions(a, b string) (int, error) {
	// Empty-version handling: empty < any non-empty
	if a == "" && b == "" {
		return 0, nil
	}
	if a == "" {
		return -1, nil
	}
	if b == "" {
		return 1, nil
	}

	// Helper to split epoch
	splitEpoch := func(ver string) (epoch int, rest string) {
		parts := strings.SplitN(ver, ":", 2)
		if len(parts) == 2 {
			if _, err := fmt.Sscanf(parts[0], "%d", &epoch); err != nil {
				epoch = 0
			}
			rest = parts[1]
		} else {
			epoch = 0
			rest = ver
		}
		return
	}

	// Split upstream_version and debian_revision at last hyphen
	splitRevision := func(ver string) (upstream string, debian string) {
		if i := strings.LastIndex(ver, "-"); i >= 0 {
			return ver[:i], ver[i+1:]
		}
		return ver, ""
	}

	// nextSegment returns the next contiguous numeric or non-numeric segment.
	nextSegment := func(s string) (seg string, rest string, numeric bool) {
		if s == "" {
			return "", "", false
		}
		// numeric segment
		if s[0] >= '0' && s[0] <= '9' {
			i := 0
			for i < len(s) && s[i] >= '0' && s[i] <= '9' {
				i++
			}
			return s[:i], s[i:], true
		}
		// non-numeric segment
		i := 0
		for i < len(s) && (s[i] < '0' || s[i] > '9') {
			i++
		}
		return s[:i], s[i:], false
	}

	// Character ordering per Debian: '~' < end-of-string < letters < other characters.
	// This ordering is crucial for correct Debian version comparison, as defined in
	// Debian Policy Manual section 5.6.12 ("Version"). See:
	// https://www.debian.org/doc/debian-policy/ch-controlfields.html#version
	charOrder := func(r rune) int {
		if r == '~' {
			return -2
		}
		if r == 0 {
			return -1
		}
		if unicode.IsLetter(r) {
			return int(r)
		}
		return 0x100 + int(r)
	}

	// Compare two non-digit segments using Debian ordering
	compareNonDigitSegments := func(aSeg, bSeg string) int {
		ai, bi := 0, 0
		for {
			var ra, rb rune
			if ai < len(aSeg) {
				ra = rune(aSeg[ai])
			} else {
				ra = 0
			}
			if bi < len(bSeg) {
				rb = rune(bSeg[bi])
			} else {
				rb = 0
			}
			// both ended
			if ra == 0 && rb == 0 {
				return 0
			}
			if ra != rb {
				oa := charOrder(ra)
				ob := charOrder(rb)
				if oa < ob {
					return -1
				}
				return 1
			}
			ai++
			bi++
		}
	}

	// Compare numeric segments (as dpkg: strip leading zeros, compare length, then lexicographically)
	compareNumericSegments := func(aSeg, bSeg string) int {
		aTrim := strings.TrimLeft(aSeg, "0")
		bTrim := strings.TrimLeft(bSeg, "0")
		// treat empty as zero
		if aTrim == "" && bTrim == "" {
			return 0
		}
		if aTrim == "" {
			return -1
		}
		if bTrim == "" {
			return 1
		}
		// longer numeric (more digits) is greater
		if len(aTrim) > len(bTrim) {
			return 1
		}
		if len(aTrim) < len(bTrim) {
			return -1
		}
		// same length -> lexical compare works
		if aTrim > bTrim {
			return 1
		}
		if aTrim < bTrim {
			return -1
		}
		return 0
	}

	// Handle epoch
	epochA, restA := splitEpoch(a)
	epochB, restB := splitEpoch(b)
	if epochA < epochB {
		return -1, nil
	}
	if epochA > epochB {
		return 1, nil
	}

	// Split upstream and debian revisions
	upA, debA := splitRevision(restA)
	upB, debB := splitRevision(restB)

	// Compare iterative parts (used for upstream version and debian revision)
	compareParts := func(sa, sb string) int {
		for sa != "" || sb != "" {
			// Handle tilde first: '~' sorts before everything (including end-of-string)
			if (len(sa) > 0 && sa[0] == '~') || (len(sb) > 0 && sb[0] == '~') {
				if len(sa) > 0 && sa[0] == '~' && !(len(sb) > 0 && sb[0] == '~') {
					return -1
				}
				if len(sb) > 0 && sb[0] == '~' && !(len(sa) > 0 && sa[0] == '~') {
					return 1
				}
				// both have tilde: consume and continue
				if len(sa) > 0 && sa[0] == '~' && len(sb) > 0 && sb[0] == '~' {
					sa = sa[1:]
					sb = sb[1:]
					continue
				}
			}

			// After tilde handling, if either side is exhausted, the exhausted side is less
			if sa == "" && sb == "" {
				break
			}
			if sa == "" {
				return -1
			}
			if sb == "" {
				return 1
			}

			segA, restASeg, numA := nextSegment(sa)
			segB, restBSeg, numB := nextSegment(sb)

			// both empty segments -> continue
			if segA == "" && segB == "" {
				sa, sb = restASeg, restBSeg
				continue
			}

			// numeric vs non-numeric: numeric < non-numeric
			if numA != numB {
				if numA {
					return -1
				}
				return 1
			}

			// both numeric
			if numA && numB {
				if cmp := compareNumericSegments(segA, segB); cmp != 0 {
					return cmp
				}
			} else { // both non-numeric
				if cmp := compareNonDigitSegments(segA, segB); cmp != 0 {
					return cmp
				}
			}

			sa, sb = restASeg, restBSeg
		}
		return 0
	}

	// Compare upstream versions first, then debian revisions
	if cmp := compareParts(upA, upB); cmp != 0 {
		return cmp, nil
	}
	if cmp := compareParts(debA, debB); cmp != 0 {
		return cmp, nil
	}
	return 0, nil
}

// CleanDependencyName extracts the base package name from a complex dependency string.
// Handles version constraints, alternatives, and architecture qualifiers.
// Examples:
//
//	"libc6 (>= 2.34)" -> "libc6"
//	"python3 | python3-dev" -> "python3"
//	"gcc:amd64" -> "gcc"
func CleanDependencyName(dep string) string {
	depName := strings.TrimSpace(dep)

	// Handle alternatives (|) - take the first option
	if idx := strings.Index(depName, "|"); idx > 0 {
		depName = strings.TrimSpace(depName[:idx])
	}

	// Handle architecture qualifiers (:)
	if idx := strings.Index(depName, ":"); idx > 0 {
		depName = depName[:idx]
	}

	// Handle version constraints - remove everything from opening parenthesis
	if idx := strings.Index(depName, "("); idx > 0 {
		depName = strings.TrimSpace(depName[:idx])
	} else if idx := strings.Index(depName, " "); idx > 0 {
		// Handle cases where there's a space but no parentheses
		depName = depName[:idx]
	}

	return strings.TrimSpace(depName)
}

// compareVersions compares two Debian package versions
// Returns 1 if v1 > v2, -1 if v1 < v2, 0 if equal
func compareVersions(v1, v2 string) int {
	// Extract version from Debian package names like "acct_6.6.4-5+b1_amd64.deb"
	extractVersion := func(name string) string {
		parts := strings.Split(name, "_")
		if len(parts) >= 2 {
			return parts[1]
		}
		return name
	}
	ver1 := extractVersion(v1)
	ver2 := extractVersion(v2)
	cmp, _ := CompareDebianVersions(ver1, ver2)
	return cmp
}

func isKernelPackageRequest(want string) bool {
	for pattern := range KernelPackages {
		if pattern == want {
			return true
		}

		if !isGlobPattern(pattern) {
			continue
		}

		matched, err := path.Match(pattern, want)
		if err == nil && matched {
			return true
		}
	}

	return false
}

func stripEpoch(version string) string {
	if colonIdx := strings.Index(version, ":"); colonIdx != -1 {
		return version[colonIdx+1:]
	}
	return version
}

func matchesKernelVersion(candidateVersion string) bool {
	if KernelVersion == "" {
		return false
	}

	versionNoEpoch := stripEpoch(candidateVersion)
	if versionNoEpoch == KernelVersion {
		return true
	}

	if !strings.HasPrefix(versionNoEpoch, KernelVersion) {
		return false
	}

	if len(versionNoEpoch) == len(KernelVersion) {
		return true
	}

	nextChar := versionNoEpoch[len(KernelVersion)]
	return nextChar == '.' || nextChar == '-' || nextChar == '_' || nextChar == '+' || nextChar == '~'
}

func filterKernelCandidates(candidates []ospackage.PackageInfo) []ospackage.PackageInfo {
	var matched []ospackage.PackageInfo
	for _, candidate := range candidates {
		if matchesKernelVersion(candidate.Version) {
			matched = append(matched, candidate)
		}
	}
	return matched
}

// ResolvePackage finds the best matching package for a given package name
func ResolveTopPackageConflicts(want string, all []ospackage.PackageInfo) (ospackage.PackageInfo, bool) {
	log := logger.Logger()
	log.Debugf("ResolveTopPackageConflicts: Searching for package '%s' in %d available packages", want, len(all))

	var candidates []ospackage.PackageInfo
	isKernelPackage := isKernelPackageRequest(want)
	for _, pi := range all {
		// 1) exact name and version matched with .deb filenamae, e.g. acct_7.6.4-5+b1_amd64
		if filepath.Base(pi.URL) == want+".deb" {
			candidates = append(candidates, pi)
			break
		}
		// 2) exact name, e.g. acct
		if pi.Name == want {
			log.Debugf("  Found EXACT NAME candidate: %s (version: %s)", pi.Name, pi.Version)
			candidates = append(candidates, pi)
			continue
		}
		// 3) prefix by want-version ("acl-")
		if strings.HasPrefix(pi.Name, want+"-") {
			// Extract string after "-" and compare with pi.Version
			if dashIdx := strings.LastIndex(want, "-"); dashIdx != -1 {
				verStr := want[dashIdx+1:]
				if strings.Contains(pi.Version, verStr) {
					candidates = append(candidates, pi)
					continue
				}
			}
		}
		// 4) prefix by want.release ("acl-2.3.1-2.")
		if strings.HasPrefix(pi.Name, want+".") {
			suffix := strings.TrimPrefix(pi.Name, want+".") // e.g. "10" or "defs"
			if len(suffix) > 0 && suffix[0] >= '0' && suffix[0] <= '9' {
				// treat as versioned dotted-variant like python3.10
				candidates = append(candidates, pi)
				continue
			}
			// otherwise ignore dotted names like login.defs for want=login
		}
		// 5) Debian package format (packagename_version_arch.deb)
		if strings.HasPrefix(pi.Name, want+"_") {
			candidates = append(candidates, pi)
			continue
		}
		// 6) Match package_epoch:version format (e.g., qemu-system_3:9.1.0+git...)
		// The want string includes epoch, but the filename in the repo doesn't
		// Example: want="qemu-system_3:9.1.0+git...", pi.Version="3:9.1.0+git...", filename="qemu-system_9.1.0+git..."
		if strings.Contains(want, "_") && strings.Contains(want, ":") {
			parts := strings.SplitN(want, "_", 2)
			if len(parts) == 2 {
				pkgName := parts[0]
				wantVersion := parts[1]
				// Check if package name matches and version matches (with epoch)
				if pi.Name == pkgName && pi.Version == wantVersion {
					candidates = append(candidates, pi)
					continue
				}
			}
		}
		// 7) Match package_version format without epoch (e.g., intel-gsc_0.9.5-1ppa1~noble1)
		// The want string doesn't include epoch, but the package version might have it
		// Example: want="intel-gsc_0.9.5-1ppa1~noble1", pi.Version="0:0.9.5-1ppa1~noble1" or "0.9.5-1ppa1~noble1"
		if strings.Contains(want, "_") && !strings.Contains(want, ":") {
			parts := strings.SplitN(want, "_", 2)
			if len(parts) == 2 {
				pkgName := parts[0]
				wantVersion := parts[1]
				// Check if package name matches
				if pi.Name == pkgName {
					// Strip epoch from package version if present and compare
					piVersionNoEpoch := pi.Version
					if colonIdx := strings.Index(pi.Version, ":"); colonIdx != -1 {
						piVersionNoEpoch = pi.Version[colonIdx+1:]
					}
					if piVersionNoEpoch == wantVersion {
						candidates = append(candidates, pi)
						continue
					}
				}
			}
		}
		// 8) Match through Provides field (virtual packages or alternative names)
		// Example: want="mail-transport-agent", pi.Provides=["mail-transport-agent"]
		for _, provided := range pi.Provides {
			if provided == want {
				log.Debugf("  Found PROVIDES candidate: %s (version: %s, provides: %s)", pi.Name, pi.Version, provided)
				candidates = append(candidates, pi)
				break
			}
		}
	}

	log.Debugf("ResolveTopPackageConflicts: Found %d initial candidates for package '%s'", len(candidates), want)
	for i, candidate := range candidates {
		log.Debugf("  Candidate %d: %s (version: %s, provides: %v)", i+1, candidate.Name, candidate.Version, candidate.Provides)
	}

	if len(candidates) == 0 {
		log.Debugf("ResolveTopPackageConflicts: No candidates found for package '%s'", want)
		return ospackage.PackageInfo{}, false
	}

	// Filter out blocked packages (priority < 0) and prioritize exact name matches
	candidates = filterCandidatesByPriorityWithTarget(candidates, want)
	log.Debugf("ResolveTopPackageConflicts: After priority filtering, %d candidates remain for package '%s'", len(candidates), want)
	for i, candidate := range candidates {
		log.Debugf("  Filtered candidate %d: %s (version: %s)", i+1, candidate.Name, candidate.Version)
	}
	if len(candidates) == 0 {
		return ospackage.PackageInfo{}, false
	}

	if isKernelPackage && KernelVersion != "" {
		candidates = filterKernelCandidates(candidates)
		if len(candidates) == 0 {
			return ospackage.PackageInfo{}, false
		}
	}

	// If we got an exact match in step (1), it's the only candidate
	if len(candidates) == 1 && (candidates[0].Name == want || candidates[0].Name == want+".deb") {
		log.Debugf("ResolveTopPackageConflicts: Selected exact match candidate: %s (version: %s)", candidates[0].Name, candidates[0].Version)
		return candidates[0], true
	}

	// Candidates already sorted by filterCandidatesByPriority
	log.Debugf("ResolveTopPackageConflicts: Selected best candidate: %s (version: %s) for package '%s'", candidates[0].Name, candidates[0].Version, want)
	return candidates[0], true
}

// ExpandKernelInstallNames replaces a glob kernel entry (for example
// linux-image-generic*) with the single newest matching package name from
// resolved, so the deb install step passes one concrete name to apt instead of
// a glob. apt would otherwise expand the glob against the shared local repo (and
// dependency resolution pulls every provider of the linux-image-generic virtual
// package), installing multiple kernels. Non-glob entries, and globs with no
// resolved match, pass through unchanged.
func ExpandKernelInstallNames(kernelPkgs []string, resolved []ospackage.PackageInfo) []string {
	var out []string
	for _, pkg := range kernelPkgs {
		if !isGlobPattern(pkg) {
			out = append(out, pkg)
			continue
		}
		var matches []ospackage.PackageInfo
		for _, r := range resolved {
			if ok, err := path.Match(pkg, r.Name); err == nil && ok {
				matches = append(matches, r)
			}
		}
		if len(matches) == 0 {
			out = append(out, pkg)
			continue
		}
		sort.Slice(matches, func(i, j int) bool {
			if c := compareVersions(matches[i].Version, matches[j].Version); c != 0 {
				return c > 0
			}
			return matches[i].Name < matches[j].Name
		})
		out = append(out, matches[0].Name)
	}
	return out
}

// ResolveWildcardPackageConflicts expands a wildcard request to the best package
// for each matched base package name.
func ResolveWildcardPackageConflicts(want string, all []ospackage.PackageInfo) ([]ospackage.PackageInfo, bool) {
	if !isGlobPattern(want) {
		pkg, found := ResolveTopPackageConflicts(want, all)
		if !found {
			return nil, false
		}
		return []ospackage.PackageInfo{pkg}, true
	}

	baseNames := make(map[string]struct{})
	for _, pi := range all {
		matched, err := path.Match(want, pi.Name)
		if err != nil || !matched {
			continue
		}
		baseNames[pi.Name] = struct{}{}
	}

	if len(baseNames) == 0 {
		return nil, false
	}

	var results []ospackage.PackageInfo
	for baseName := range baseNames {
		pkg, found := ResolveTopPackageConflicts(baseName, all)
		if found {
			results = append(results, pkg)
		}
	}

	if len(results) == 0 {
		return nil, false
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Name == results[j].Name {
			return compareVersions(results[i].Version, results[j].Version) > 0
		}
		return results[i].Name < results[j].Name
	})

	return results, true
}

// Helper function to find all candidates for a dependency
func findAllCandidates(depName string, all []ospackage.PackageInfo) []ospackage.PackageInfo {
	var candidates []ospackage.PackageInfo

	// First pass: look for exact name matches
	for _, pi := range all {
		if pi.Name == depName {
			candidates = append(candidates, pi)
		}
	}

	// If no direct matches found, search in Provides field
	if len(candidates) == 0 {
		for _, pi := range all {
			for _, provided := range pi.Provides {
				if provided == depName {
					candidates = append(candidates, pi)
				}
			}
		}
	}

	// Apply APT priority filtering and sorting with exact name preference
	filtered := filterCandidatesByPriorityWithTarget(candidates, depName)
	return filtered
}

func rememberResolvedDependency(resolvedDeps map[string]ospackage.PackageInfo, key string, pkg ospackage.PackageInfo) {
	if key != "" {
		resolvedDeps[key] = pkg
	}
	resolvedDeps[pkg.Name] = pkg
}

// packageStillRequired reports whether resolvedPkg must remain in the
// resolved closure independent of depName — the dependency currently being
// replaced with newCandidate — because it is still the resolvedDeps value for
// some OTHER dependency name that newCandidate does not also satisfy (its own
// name, or a capability newCandidate Provides). A provider satisfying two
// unrelated capabilities must not be dropped from the result just because a
// stricter constraint on ONE of those capabilities forces a replacement for
// it — the other, already-established requirement was never re-resolved and
// would otherwise silently end up unsatisfied. A seed's own VERSION can still
// be legitimately replaced by constraint resolution (e.g. a later dependency
// pinning it below the originally requested version) when newCandidate keeps
// the SAME name; only a replacement that switches to a DIFFERENTLY named
// package is blocked for a seed, since the user explicitly asked for that
// package by name and it must not silently disappear from the closure just
// because some other capability it happens to also provide got reassigned.
//
// requiredDepNames distinguishes a genuine dependency edge from
// rememberResolvedDependency's own-name bookkeeping alias, which it sets for
// EVERY selected provider regardless of whether anything ever actually
// depended on that name directly — without this check, a virtual capability
// replaced by a differently-named provider would always look "independently
// required" via its own stale bookkeeping alias, defeating replacement
// entirely.
func packageStillRequired(resolvedPkg ospackage.PackageInfo, depName string, resolvedDeps map[string]ospackage.PackageInfo, newCandidate ospackage.PackageInfo, requiredDepNames map[string]struct{}, seedByName map[string]ospackage.PackageInfo, depVersionConstraints map[string][]VersionConstraint) bool {
	if _, isSeed := seedByName[resolvedPkg.Name]; isSeed && resolvedPkg.Name != newCandidate.Name {
		return true
	}
	newProvides := make(map[string]struct{}, len(newCandidate.Provides))
	for _, p := range newCandidate.Provides {
		newProvides[CleanDependencyName(p)] = struct{}{}
	}
	for k, v := range resolvedDeps {
		if k == depName {
			continue
		}
		if v.Name != resolvedPkg.Name || v.Version != resolvedPkg.Version {
			continue
		}
		if k == newCandidate.Name {
			// Safe unconditionally: the candidate-filtering search that chose
			// newCandidate already rejected any candidate whose own real name
			// violates a genuine accumulated constraint (see the
			// aliasVersionCovered check next to depVersionConstraints[candidate.Name]),
			// so newCandidate is guaranteed to satisfy its own-name alias here.
			continue
		}
		if _, covered := newProvides[k]; covered && aliasVersionCovered(newCandidate, k, depVersionConstraints[k]) {
			continue
		}
		if _, genuine := requiredDepNames[k]; !genuine {
			continue
		}
		return true
	}
	return false
}

// uncoveredAliasOnSameNameReplacement reports the first genuine virtual alias
// that resolvedPkg satisfies but newCandidate does not, when both share the
// same real package name. Because only one version of a given package name can
// exist in the closure, such an alias cannot be preserved by keeping resolvedPkg
// alongside newCandidate: they would collide on Name, the later-queued
// newCandidate is skipped via neededSet, and the OLD, constraint-violating
// version is silently retained. A non-empty alias therefore signals a genuine
// conflict the caller must surface rather than a package that can coexist.
// Aliases newCandidate covers by name AND version (aliasVersionCovered), the
// shared real name itself, and depName (repointed to newCandidate by the
// caller) are not conflicts. Only aliases in requiredDepNames count — a
// convenience-key alias nothing genuinely requested is not a real requirement.
func uncoveredAliasOnSameNameReplacement(resolvedPkg, newCandidate ospackage.PackageInfo, resolvedDeps map[string]ospackage.PackageInfo, depName string, requiredDepNames map[string]struct{}, depVersionConstraints map[string][]VersionConstraint) (string, bool) {
	if resolvedPkg.Name != newCandidate.Name {
		return "", false
	}
	newProvides := make(map[string]struct{}, len(newCandidate.Provides))
	for _, p := range newCandidate.Provides {
		newProvides[CleanDependencyName(p)] = struct{}{}
	}
	for k, v := range resolvedDeps {
		if k == depName || k == newCandidate.Name {
			continue
		}
		if v.Name != resolvedPkg.Name || v.Version != resolvedPkg.Version {
			continue
		}
		if _, genuine := requiredDepNames[k]; !genuine {
			continue
		}
		if _, covered := newProvides[k]; covered && aliasVersionCovered(newCandidate, k, depVersionConstraints[k]) {
			continue
		}
		return k, true
	}
	return "", false
}

// aliasVersionCovered reports whether newCandidate actually satisfies every
// accumulated version constraint recorded for alias, not just whether
// newCandidate provides alias BY NAME. A name-only match is not enough: e.g.
// an earlier parent pinning "old-only-capability (= 1)" is not covered by a
// replacement that also provides "old-only-capability" but only at "= 2". An
// alias with no recorded constraints is covered by name alone, since nothing
// ever pinned a specific version on it.
func aliasVersionCovered(newCandidate ospackage.PackageInfo, alias string, constraints []VersionConstraint) bool {
	for _, c := range constraints {
		if c.Op == "" || c.Ver == "" {
			continue
		}
		ver, ok := versionForDependency(newCandidate, alias)
		if !ok {
			return false
		}
		if !debVersionSatisfies(ver, c.Op, c.Ver) {
			return false
		}
	}
	return true
}

// replaceQueuedAndAliasedDependency returns queue with every not-yet-processed
// entry for old (matched by Name+Version) removed. It also cleans up
// resolvedDeps aliases that pointed at old (rememberResolvedDependency can
// register a package under multiple keys — its own name plus whatever virtual
// name it was queued to satisfy): an alias newCandidate genuinely still
// satisfies (its own name, or a capability it also Provides AT A VERSION that
// satisfies every accumulated constraint on that alias, per
// aliasVersionCovered) is repointed at newCandidate; every other alias is
// removed rather than repointed, since newCandidate may not satisfy it at all
// — leaving a stale alias pointed at an unrelated (or version-incompatible)
// replacement would make a later direct dependency on that name wrongly
// appear pre-resolved instead of triggering fresh resolution. Only call this
// once packageStillRequired has confirmed old is safe to fully remove from
// the closure. The caller is responsible for (re)registering depName itself
// afterward.
func replaceQueuedAndAliasedDependency(queue []ospackage.PackageInfo, resolvedDeps map[string]ospackage.PackageInfo, old, newCandidate ospackage.PackageInfo, depVersionConstraints map[string][]VersionConstraint) []ospackage.PackageInfo {
	filtered := make([]ospackage.PackageInfo, 0, len(queue))
	for _, q := range queue {
		if q.Name == old.Name && q.Version == old.Version {
			continue
		}
		filtered = append(filtered, q)
	}

	newProvides := make(map[string]struct{}, len(newCandidate.Provides))
	for _, p := range newCandidate.Provides {
		newProvides[CleanDependencyName(p)] = struct{}{}
	}
	for k, v := range resolvedDeps {
		if v.Name != old.Name || v.Version != old.Version {
			continue
		}
		if k == newCandidate.Name {
			resolvedDeps[k] = newCandidate
			continue
		}
		if _, ok := newProvides[k]; ok && aliasVersionCovered(newCandidate, k, depVersionConstraints[k]) {
			resolvedDeps[k] = newCandidate
			continue
		}
		delete(resolvedDeps, k)
	}
	return filtered
}

// Helper function to resolve multiple candidates by picking the last one
// extractRepoBase extracts the Debian repo base URL (everything up to /pool/)
func extractRepoBase(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	// Split path by "/pool/"
	parts := strings.SplitN(u.Path, "/pool/", 2)
	if len(parts) < 2 {
		return "", fmt.Errorf("URL does not contain /pool/: %s", rawURL)
	}

	// Rebuild base URL: scheme + host + prefix before /pool/ (without trailing slash)
	base := fmt.Sprintf("%s://%s%s", u.Scheme, u.Host, parts[0])
	return base, nil
}

// alternativeResolution pairs an OR-edge's resolved alternative name with the
// package chosen to satisfy it. ReqVer is the single raw RequiresVer term that
// produced this alternative, so callers can extract that edge's own version
// constraint without picking up constraints from any other term that also
// happens to name the same alternative.
type alternativeResolution struct {
	Name    string
	Package ospackage.PackageInfo
	ReqVer  string
}

// resolveAlternativeForTerm tries one raw OR term's remaining alternatives (in
// order) and returns the first one resolveMultiCandidates can actually satisfy.
func resolveAlternativeForTerm(cur ospackage.PackageInfo, reqVer string, all []ospackage.PackageInfo) (altName string, chosen ospackage.PackageInfo, ok bool) {
	log := logger.Logger()
	alts := strings.Split(reqVer, "|")
	if len(alts) < 2 {
		return "", ospackage.PackageInfo{}, false
	}
	// resolveMultiCandidates aggregates version constraints for an alternative
	// name from every RequiresVer term of the parent it's given, but this OR
	// edge must be resolved independently of any other term that happens to
	// name the same alternative (e.g. "foo | bar (= 1), baz | bar (= 2)") — so
	// scope the parent it sees down to just this one term.
	termParent := cur
	termParent.RequiresVer = []string{reqVer}
	for _, alt := range alts[1:] {
		name := CleanDependencyName(strings.TrimSpace(alt))
		if name == "" {
			continue
		}
		altCandidates := findAllCandidates(name, all)
		if len(altCandidates) == 0 {
			continue
		}
		candidate, err := resolveMultiCandidates(termParent, name, altCandidates)
		if err == nil {
			return name, candidate, true
		}
		log.Warnf("Failed to resolve alternative %q for term %q: %v", name, reqVer, err)
	}
	return "", ospackage.PackageInfo{}, false
}

// termConstraintSatisfiedByCandidate reports whether candidate satisfies the
// version constraint (if any) that raw RequiresVer term reqVer's own first
// alternative places on depName. An unversioned first alternative is
// satisfied by mere presence.
func termConstraintSatisfiedByCandidate(reqVer, depName string, candidate ospackage.PackageInfo) bool {
	alts := strings.Split(reqVer, "|")
	name, op, ver := splitAltNameConstraint(strings.TrimSpace(alts[0]))
	if name != depName {
		return false
	}
	if op == "" || ver == "" {
		return true
	}
	candidateVer, ok := versionForDependency(candidate, depName)
	return ok && debVersionSatisfies(candidateVer, op, ver)
}

// findSatisfyingCandidate returns the first of candidates that satisfies the
// version constraint (if any) reqVer's own first alternative places on
// depName, per termConstraintSatisfiedByCandidate.
func findSatisfyingCandidate(reqVer, depName string, candidates []ospackage.PackageInfo) (ospackage.PackageInfo, bool) {
	for _, candidate := range candidates {
		if termConstraintSatisfiedByCandidate(reqVer, depName, candidate) {
			return candidate, true
		}
	}
	return ospackage.PackageInfo{}, false
}

// resolveDependencyTermsIndependently resolves every raw RequiresVer term
// whose own first alternative cleans to depName, INDEPENDENTLY of the others —
// used when the aggregated resolveMultiCandidates call for depName fails
// because it requires ONE candidate to satisfy every such term simultaneously,
// which is impossible when two terms pin mutually exclusive exact versions
// (e.g. "foo (= 1) | bar, foo (= 2) | baz"), even though each term
// individually may be satisfiable (foo (= 1) directly; baz as (= 2)'s
// fallback) — and when depName has no candidates at all. A bare (non-OR) term
// has no alternative to fall back to: it can only be satisfied by SOME
// candidate meeting ALL bare terms at once, since only one version of
// depName can exist in the closure, so bare terms are resolved first and
// jointly, before OR terms (which can each independently fall back) are
// checked against whatever candidate the bare terms settled on.
//
// "Only one version can exist in the closure" holds for a REAL package name,
// but depName is often virtual (findAllCandidates only returns Provides-based
// candidates when no package is literally named depName): two DIFFERENTLY
// NAMED real packages can independently provide different versions of the
// same virtual capability and coexist, e.g. "virtual (= 1) | missing-a" and
// "virtual (= 2) | missing-b" satisfied by two distinct providers. So once
// chosen is fixed to a candidate whose Name differs from depName (a virtual
// resolution, not depName's own real package), a later OR term that chosen
// doesn't satisfy is not necessarily unmet — every candidate is searched
// again for one that satisfies THAT term independently, and a match under a
// different real Name is recorded as its own alternativeResolution rather
// than overwriting chosen. Only when chosen.Name == depName (a genuine single
// real package) does failing to satisfy a later term fall straight to the
// alternative fallback, since no other candidate could take its place.
//
// Returns the depName candidate to adopt (nil if depName itself is not needed to satisfy
// anything), the alternatives resolved for unmet OR terms, and whether every
// term could be satisfied one way or another.
func resolveDependencyTermsIndependently(cur ospackage.PackageInfo, depName string, candidates []ospackage.PackageInfo, all []ospackage.PackageInfo) (chosen *ospackage.PackageInfo, alternatives []alternativeResolution, ok bool) {
	ok = true

	var bareReqVers []string
	sawTerm := false
	for _, reqVer := range cur.RequiresVer {
		alts := strings.Split(reqVer, "|")
		if CleanDependencyName(strings.TrimSpace(alts[0])) != depName {
			continue
		}
		sawTerm = true
		if len(alts) == 1 {
			bareReqVers = append(bareReqVers, reqVer)
		}
	}
	// cur.Requires and cur.RequiresVer are populated in lockstep by the real
	// parser, but a hand-built PackageInfo (tests, or any other caller) may
	// only set Requires. Without this, a depName with no matching RequiresVer
	// term at all would look like it has nothing to satisfy and be silently
	// treated as resolved.
	if !sawTerm && hasDirectDependency(cur.Requires, depName) {
		bareReqVers = append(bareReqVers, depName)
	}
	if len(bareReqVers) > 0 {
		for _, candidate := range candidates {
			satisfiesAllBare := true
			for _, reqVer := range bareReqVers {
				if !termConstraintSatisfiedByCandidate(reqVer, depName, candidate) {
					satisfiesAllBare = false
					break
				}
			}
			if satisfiesAllBare {
				c := candidate
				chosen = &c
				break
			}
		}
		if chosen == nil {
			ok = false // mandatory bare term(s) unsatisfiable by any candidate
		}
	}

	for _, reqVer := range cur.RequiresVer {
		alts := strings.Split(reqVer, "|")
		if len(alts) < 2 || CleanDependencyName(strings.TrimSpace(alts[0])) != depName {
			continue
		}

		resolvedDirect := false
		if chosen != nil && termConstraintSatisfiedByCandidate(reqVer, depName, *chosen) {
			// Whichever candidate an earlier term already fixed chosen to also
			// satisfies this edge directly.
			resolvedDirect = true
		} else if chosen == nil || chosen.Name != depName {
			// depName is not yet fixed, or is virtual (chosen is just one
			// incidental provider fixed for a DIFFERENT edge, under some other
			// real Name) — a different real package can independently provide
			// another version of the same virtual capability and coexist, so
			// search every candidate for one that satisfies THIS edge, instead
			// of only *chosen. (When chosen.Name == depName — a genuine single
			// real package — and it didn't satisfy the edge above, no other
			// candidate could either, since only one version of a real package
			// can exist in the closure; that case falls straight through to
			// the alternative fallback below.)
			if satisfying, found := findSatisfyingCandidate(reqVer, depName, candidates); found {
				if chosen == nil {
					chosen = &satisfying
					resolvedDirect = true
				} else if satisfying.Name != chosen.Name {
					alternatives = append(alternatives, alternativeResolution{Name: depName, Package: satisfying, ReqVer: reqVer})
					resolvedDirect = true
				}
				// else: same real package as chosen but an incompatible
				// version — only one version of it can exist in the closure,
				// so this edge still isn't satisfied; fall through below.
			}
		}
		if resolvedDirect {
			continue
		}

		if altName, altCandidate, resolvedOK := resolveAlternativeForTerm(cur, reqVer, all); resolvedOK {
			alternatives = append(alternatives, alternativeResolution{Name: altName, Package: altCandidate, ReqVer: reqVer})
			continue
		}
		ok = false
	}
	return chosen, alternatives, ok
}

// orEdgeSatisfied reports whether the OR term reqVer (whose own first
// alternative must clean to a name resolvedPkg is resolved under) is already
// satisfied — either because resolvedPkg's own version meets the first
// alternative's version pin (an unversioned first alternative is satisfied by
// mere presence), or because another alternative of the same edge is already
// selected (selectedVersions) and meets its own constraint.
func orEdgeSatisfied(reqVer string, resolvedPkg ospackage.PackageInfo, selectedVersions func(string) []string) bool {
	alts := strings.Split(reqVer, "|")
	if len(alts) < 2 {
		return true
	}
	name, op, ver := splitAltNameConstraint(strings.TrimSpace(alts[0]))
	if op == "" || ver == "" {
		return true
	}
	if resVer, ok := versionForDependency(resolvedPkg, name); ok && debVersionSatisfies(resVer, op, ver) {
		return true
	}
	return edgeSatisfiedByOtherAlternative(alts[1:], selectedVersions)
}

// resolveUnmetOwnOREdges checks, for every raw RequiresVer term whose own first
// alternative cleans to depName, whether that OR edge is satisfied by
// resolvedPkg (the package just picked/kept for depName) or another already
// selected alternative — and if not, resolves and queues that edge's own
// alternative. Needed because when depName is BOTH a bare mandatory term and an
// OR term's own first alternative, the OR term's version pin is deliberately
// stripped from candidate selection/replacement decisions (the bare term makes
// depName mandatory regardless of that pin), which must not silently drop the
// separate OR edge itself — apt still requires it to be met, by depName's own
// version or by pulling in its alternative. Returns the (possibly grown) queue
// and whether any edge's alternative could not be resolved.
func resolveUnmetOwnOREdges(cur, resolvedPkg ospackage.PackageInfo, depName string, all []ospackage.PackageInfo, selectedVersions func(string) []string,
	queue []ospackage.PackageInfo, resolvedDeps map[string]ospackage.PackageInfo, requiredDepNames map[string]struct{},
	depVersionConstraints map[string][]VersionConstraint, parentChildPairs *[][]ospackage.PackageInfo) ([]ospackage.PackageInfo, bool) {
	log := logger.Logger()
	gotMissing := false
	for _, reqVer := range cur.RequiresVer {
		alts := strings.Split(reqVer, "|")
		if len(alts) < 2 || CleanDependencyName(alts[0]) != depName {
			continue
		}
		if orEdgeSatisfied(reqVer, resolvedPkg, selectedVersions) {
			continue
		}
		if altName, altCandidate, ok := resolveAlternativeForTerm(cur, reqVer, all); ok {
			log.Infof("Successfully resolved alternative %q version %q for OR edge %q (mandatory %q=%q does not satisfy this edge)",
				altName, altCandidate.Version, reqVer, depName, resolvedPkg.Version)
			queue = queueResolvedAlternative(cur, reqVer, altName, altCandidate, queue, resolvedDeps, requiredDepNames, depVersionConstraints, parentChildPairs)
		} else {
			gotMissing = true
			AddParentMissingChildPair(cur, depName+"(missing)", parentChildPairs)
			log.Warnf("failed to resolve unsatisfied OR edge %q of package %q (mandatory %q=%q does not satisfy it)",
				reqVer, cur.Name, depName, resolvedPkg.Version)
		}
	}
	return queue, gotMissing
}

// queueResolvedAlternative records altCandidate as the package chosen to
// satisfy an OR edge whose raw term is reqVer, mirroring the bookkeeping a
// direct dependency resolution performs: constrains altName by reqVer's own
// version (scoped to this one term, so an unrelated edge sharing the same
// alternative name can't leak its own constraint in here), marks it genuinely
// required, queues it for processing, and records the parent-child edge.
func queueResolvedAlternative(cur ospackage.PackageInfo, reqVer, altName string, altCandidate ospackage.PackageInfo, queue []ospackage.PackageInfo,
	resolvedDeps map[string]ospackage.PackageInfo, requiredDepNames map[string]struct{}, depVersionConstraints map[string][]VersionConstraint,
	parentChildPairs *[][]ospackage.PackageInfo) []ospackage.PackageInfo {
	if altVCs, hasAltVC := extractVersionRequirement([]string{reqVer}, altName); hasAltVC {
		depVersionConstraints[altName] = append(depVersionConstraints[altName], altVCs...)
	}
	requiredDepNames[altName] = struct{}{}
	queue = append(queue, altCandidate)
	rememberResolvedDependency(resolvedDeps, altName, altCandidate)
	AddParentChildPair(cur, altCandidate, parentChildPairs)
	return queue
}

// reconstructOREdgeTerm rebuilds the raw "depName (op ver) | alt1 | alt2..."
// OR term that a VersionConstraint recorded in depVersionConstraints was
// extracted from. VersionConstraint.AlternativeTerms only stores the OTHER
// alternatives (extractVersionRequirement deliberately excludes depName's own
// term when building it), so it cannot be handed to resolveAlternativeForTerm
// directly — that function expects the FULL term, including depName's own
// leading alternative, and only skips it internally.
func reconstructOREdgeTerm(depName string, constraint VersionConstraint) string {
	return fmt.Sprintf("%s (%s %s) | %s", depName, constraint.Op, constraint.Ver, constraint.AlternativeTerms)
}

// bridgeableOREdge reports whether the OR edge whose raw term is reqVer (e.g.
// "foo (= 1) | bar (>= 2)", see reconstructOREdgeTerm) can still be considered
// met when depName's own resolution no longer satisfies the edge's first
// alternative's pin — either because a DIFFERENT alternative is already
// selected (needsQueue=false: the edge is already satisfied, nothing further
// to do), or because a fresh candidate for that alternative can be resolved
// right now (needsQueue=true: the caller must actually queue altCandidate
// under altName to make good on this bridge). Returns ok=false when neither
// holds, meaning the edge's pin genuinely conflicts and cannot be bridged.
func bridgeableOREdge(cur ospackage.PackageInfo, reqVer string, all []ospackage.PackageInfo, selectedVersions func(string) []string) (altName string, altCandidate ospackage.PackageInfo, needsQueue, ok bool) {
	alts := strings.Split(reqVer, "|")
	if len(alts) < 2 {
		return "", ospackage.PackageInfo{}, false, false
	}
	if name, satisfied := edgeSatisfyingAlternative(alts[1:], selectedVersions); satisfied {
		return name, ospackage.PackageInfo{}, false, true
	}
	if name, candidate, resolved := resolveAlternativeForTerm(cur, reqVer, all); resolved {
		return name, candidate, true, true
	}
	return "", ospackage.PackageInfo{}, false, false
}

// satisfiedAlternativeNames returns, for every OR term whose first alternative
// cleans to depName, the name of whichever OTHER alternative already
// satisfies that edge (per edgeSatisfyingAlternative). Used alongside
// alternativeAlreadySelected: when it reports depName skippable, THIS records
// which alternative(s) actually satisfied the edge(s) as the genuine
// dependency, since depName itself was never pulled in.
func satisfiedAlternativeNames(reqVers []string, depName string, selectedVersions func(string) []string) []string {
	var names []string
	for _, reqVer := range reqVers {
		alts := strings.Split(reqVer, "|")
		if len(alts) < 2 || CleanDependencyName(alts[0]) != depName {
			continue
		}
		if name, ok := edgeSatisfyingAlternative(alts[1:], selectedVersions); ok {
			names = append(names, name)
		}
	}
	return names
}

// alternativeAlreadySelected reports whether the OR-dependency edge whose first
// (default) alternative is depName has any OTHER alternative that is already
// selected AND satisfies that alternative's version constraint. It scans the raw
// Depends terms (reqVers) for the term whose first "|"-alternative cleans to
// depName, then checks every remaining alternative of that term. selectedVersions
// returns every version name is selected under (possibly more than one, since
// different selected packages can Provide the same virtual name at different
// versions); an empty-string entry means "selected but concrete version
// unknown", and a nil/empty slice means not selected at all. A versioned
// alternative counts as satisfied when ANY returned version meets its
// constraint; an unversioned alternative is satisfied by mere presence.
// Single-alternative terms and terms belonging to a different edge are ignored.
// This implements apt's rule that an already-installed/selected alternative
// satisfies the edge, so the first alternative should not be pulled in.
func alternativeAlreadySelected(reqVers []string, depName string, selectedVersions func(string) []string) bool {
	edgeFound := false
	for _, reqVer := range reqVers {
		alts := strings.Split(reqVer, "|")
		if len(alts) < 2 {
			// A bare (non-OR) term naming depName is a mandatory direct dependency —
			// e.g. "Depends: a, a | b" — so depName must be pulled regardless of any
			// satisfied OR edge that happens to share it as a first alternative.
			if CleanDependencyName(alts[0]) == depName {
				return false
			}
			continue // an unrelated non-OR term
		}
		if CleanDependencyName(alts[0]) != depName {
			continue // a different dependency edge
		}
		edgeFound = true
		// depName is THIS edge's first alternative, so pulling it satisfies this edge.
		// Distinct edges can share the same first alternative (e.g. "a | b, a | c"); the
		// first is skippable only if EVERY such edge is already met by another selected
		// alternative — otherwise depName is still needed to satisfy the unmet edge.
		if !edgeSatisfiedByOtherAlternative(alts[1:], selectedVersions) {
			return false
		}
	}
	return edgeFound
}

// edgeSatisfiedByOtherAlternative reports whether any of an OR-edge's non-first
// alternatives is already selected and (when the alternative is versioned) meets
// its version constraint. An unknown selected version is treated conservatively as
// NOT satisfying, so the first alternative is still taken.
func edgeSatisfiedByOtherAlternative(alts []string, selectedVersions func(string) []string) bool {
	_, ok := edgeSatisfyingAlternative(alts, selectedVersions)
	return ok
}

// edgeSatisfyingAlternative returns the name of the first of an OR-edge's
// non-first alternatives that is already selected and (when versioned) meets
// its version constraint, and true if one was found. Checks EVERY version
// selectedVersions returns for a given alternative name — not just one — since
// different selected packages can Provide the same virtual name at different
// versions, and collapsing to an arbitrary single match (e.g. the first hit
// while ranging over a map) could miss a different selected provider that
// actually satisfies the constraint. Mirrors edgeSatisfiedByOtherAlternative
// but also reports WHICH alternative satisfied the edge, so a caller can
// record that name (rather than the unpulled first alternative) as the one
// genuinely depended upon.
func edgeSatisfyingAlternative(alts []string, selectedVersions func(string) []string) (string, bool) {
	for _, alt := range alts {
		name, op, ver := splitAltNameConstraint(alt)
		if name == "" {
			continue
		}
		for _, selVer := range selectedVersions(name) {
			if op == "" || ver == "" {
				return name, true // unversioned alternative: any selected instance satisfies it
			}
			if selVer != "" && debVersionSatisfies(selVer, op, ver) {
				return name, true
			}
		}
	}
	return "", false
}

// splitAltNameConstraint parses one OR-dependency alternative ("e2fsprogs (<< 1.45)")
// into its package name and optional version constraint. It returns an empty name
// for an unparseable/empty alternative, and empty op/ver when the alternative
// carries no version constraint.
func splitAltNameConstraint(alt string) (name, op, ver string) {
	name = CleanDependencyName(alt)
	if name == "" {
		return "", "", ""
	}
	open := strings.Index(alt, "(")
	if open == -1 {
		return name, "", ""
	}
	rel := strings.Index(alt[open:], ")")
	if rel == -1 {
		return name, "", ""
	}
	if fields := strings.Fields(alt[open+1 : open+rel]); len(fields) == 2 {
		return name, fields[0], fields[1]
	}
	return name, "", ""
}

// debVersionSatisfies reports whether candidate satisfies the Debian version
// relation "op ver" (e.g. ">= 1.2", "<< 3"). An unrecognized operator or an
// uncomparable version pair is treated as not satisfied.
func debVersionSatisfies(candidate, op, ver string) bool {
	cmp, err := CompareDebianVersions(candidate, ver)
	if err != nil {
		return false
	}
	switch op {
	case "<<", "<":
		return cmp < 0
	case "<=":
		return cmp <= 0
	case "=", "==":
		return cmp == 0
	case ">=":
		return cmp >= 0
	case ">>", ">":
		return cmp > 0
	}
	return false
}

// versionForDependency returns the version of pkg to check against a
// constraint on depName, and whether a usable version exists at all: pkg's
// own Version when depName is pkg's real name, or the version pkg's
// Provides: line declares for depName when depName is a virtual capability
// pkg only provides (e.g. libqt6core6t64 declares "Provides: qt6-base-abi (=
// 6.4.2)" at a different version than its own package Version).
//
// Returns ok=false when pkg provides depName with no version at all (e.g.
// "Provides: foo") — per Debian policy an unversioned Provides never
// satisfies a versioned dependency, so callers must treat this as
// unsatisfied rather than falling back to pkg's own, unrelated Version.
func versionForDependency(pkg ospackage.PackageInfo, depName string) (string, bool) {
	if pkg.Name == depName {
		return pkg.Version, true
	}
	if constraints, ok := extractVersionRequirement(pkg.ProvidesVer, depName); ok {
		for _, c := range constraints {
			if c.Ver != "" {
				return c.Ver, true
			}
		}
	}
	return "", false
}

// hasDirectDependency checks if a dependency appears as a direct requirement (not in alternatives)
func hasDirectDependency(requires []string, depName string) bool {
	for _, req := range requires {
		cleanReq := CleanDependencyName(req)
		if cleanReq == depName {
			return true
		}
	}
	return false
}

// isOwnFirstAlternative reports whether depName is the designated FIRST
// alternative of at least one OR term in reqVers (e.g. "libfoo-abi (= 3) |
// fallback" for depName "libfoo-abi") — as opposed to only appearing as a
// LATER, fallback-only alternative of a term whose first alternative is some
// OTHER name (e.g. "logsave | e2fsprogs (<< X)" for depName "e2fsprogs").
// hasDirectDependency alone cannot make this distinction, since Requires
// always keeps only the OR term's first alternative name regardless of
// position — this refines that check for whether an alternative-tagged
// version constraint on depName is genuinely depName's own pin (keep it) or
// just an unrelated OR clause's fallback option that happens to co-occur
// with depName being separately, unconditionally required (ignore it).
func isOwnFirstAlternative(reqVers []string, depName string) bool {
	for _, reqVer := range reqVers {
		alts := strings.Split(reqVer, "|")
		if len(alts) < 2 {
			continue
		}
		if CleanDependencyName(alts[0]) == depName {
			return true
		}
	}
	return false
}

// hasBareMandatoryTerm reports whether depName appears as a standalone (non-OR)
// mandatory term in reqVers — e.g. the "foo" in "Depends: foo, foo (= 1) | bar".
// Such a term requires depName unconditionally, so any version pin carried by a
// SEPARATE OR term that merely lists depName as its first alternative does not
// constrain the mandatory copy (that OR edge is independently satisfiable by its
// other alternative). A bare term may itself carry a direct version pin
// (e.g. "foo (>= 1)"); that pin is preserved separately as a non-alternative
// constraint, so treating the term as mandatory here does not lose it.
func hasBareMandatoryTerm(reqVers []string, depName string) bool {
	for _, reqVer := range reqVers {
		alts := strings.Split(reqVer, "|")
		if len(alts) != 1 {
			continue
		}
		if CleanDependencyName(alts[0]) == depName {
			return true
		}
	}
	return false
}

func extractVersionRequirement(reqVers []string, depName string) ([]VersionConstraint, bool) {
	var constraints []VersionConstraint
	found := false

	for _, reqVer := range reqVers {
		reqVer = strings.TrimSpace(reqVer)

		// Handle alternatives (|) - check if our depName is in any of the alternatives
		alternatives := strings.Split(reqVer, "|")
		for i, alt := range alternatives {
			alt = strings.TrimSpace(alt)

			// Check if this alternative starts with the dependency name we're looking for
			cleanReqName := CleanDependencyName(alt)
			if cleanReqName != depName {
				continue // Skip to next alternative
			}

			// Found our dependency in this alternative, now extract version constraints
			// Find all version constraints inside parentheses (can be multiple separated by commas)
			if idx := strings.Index(alt, "("); idx != -1 {
				verConstraint := alt[idx+1:]
				if idx2 := strings.Index(verConstraint, ")"); idx2 != -1 {
					verConstraint = verConstraint[:idx2]
				}

				// Handle multiple constraints separated by commas
				constraintParts := strings.Split(verConstraint, ",")
				for _, constraintPart := range constraintParts {
					constraintPart = strings.TrimSpace(constraintPart)
					// Split into operator and version
					parts := strings.Fields(constraintPart)

					// Handle both ">> 1.2.3" and ">>1.2.3" formats
					var op, ver string
					if len(parts) == 2 {
						op, ver = parts[0], parts[1]
					} else if len(parts) == 1 {
						// Try to extract operator from the beginning
						part := parts[0]
						// Check for two-character operators first (<<, >>, >=, <=)
						if len(part) >= 2 {
							prefix := part[:2]
							if prefix == "<<" || prefix == ">>" || prefix == ">=" || prefix == "<=" {
								op = prefix
								ver = part[2:]
							} else if part[0] == '<' || part[0] == '>' || part[0] == '=' {
								// Single-character operator
								op = string(part[0])
								ver = part[1:]
							}
						} else if len(part) >= 1 && (part[0] == '<' || part[0] == '>' || part[0] == '=') {
							op = string(part[0])
							ver = part[1:]
						}
					}

					if op != "" && ver != "" {
						// Collect alternative package names (all alternatives except the current one)
						var altNames, altTerms []string
						for j, altPkg := range alternatives {
							if j != i {
								altNames = append(altNames, strings.TrimSpace(CleanDependencyName(altPkg)))
								altTerms = append(altTerms, strings.TrimSpace(altPkg))
							}
						}
						constraint := VersionConstraint{
							Op:               op,
							Ver:              ver,
							Alternative:      strings.Join(altNames, "|"),
							AlternativeTerms: strings.Join(altTerms, "|"),
						}
						constraints = append(constraints, constraint)
						found = true
					}
				}
			} else {
				// No version constraint, but we have alternatives
				if len(alternatives) > 1 {
					// Collect alternative package names (all alternatives except the current one)
					var altNames, altTerms []string
					for j, altPkg := range alternatives {
						if j != i {
							altNames = append(altNames, strings.TrimSpace(CleanDependencyName(altPkg)))
							altTerms = append(altTerms, strings.TrimSpace(altPkg))
						}
					}
					constraint := VersionConstraint{
						Alternative:      strings.Join(altNames, "|"),
						AlternativeTerms: strings.Join(altTerms, "|"),
					}
					constraints = append(constraints, constraint)
				}
			}

			// If we found the dependency but no version constraint, still mark as found
			if !found {
				found = true
			}
		}
	}

	return constraints, found
}

// matchesRepoBase checks if candidateBase matches any of the parent repo bases
func matchesRepoBase(parentBase []string, candidateBase string) bool {
	for _, pBase := range parentBase {
		if candidateBase == pBase {
			return true
		}
	}
	return false
}

func resolveMultiCandidates(parentPkg ospackage.PackageInfo, depName string, candidates []ospackage.PackageInfo) (ospackage.PackageInfo, error) {
	// Filter out blocked packages (priority < 0) first
	// All candidates should have the same name here, so no need for target-aware filtering
	candidates = filterCandidatesByPriority(candidates)
	if len(candidates) == 0 {
		return ospackage.PackageInfo{}, fmt.Errorf("all candidates are blocked by negative priority")
	}

	parent, err := extractRepoBase(parentPkg.URL)
	if err != nil {
		return ospackage.PackageInfo{}, fmt.Errorf("failed to extract repo base from parent package URL: %w", err)
	}
	// Extract parent repo base and handle potential multiple repo configurations
	var imageBase []string
	if len(RepoCfgs) > 0 {
		for _, repocfg := range RepoCfgs {
			if repocfg.PkgPrefix != "" {
				imageBase = append(imageBase, repocfg.PkgPrefix)
			}
		}
	}
	if len(imageBase) == 0 && RepoCfg.PkgPrefix != "" {
		imageBase = append(imageBase, RepoCfg.PkgPrefix)
	}

	var parentBase []string
	// check if parent part of base
	if matchesRepoBase(imageBase, parent) {
		parentBase = imageBase
	} else {
		parentBase = []string{parent}
	}

	/////////////////////////////////////
	//A: if version is specified
	/////////////////////////////////////
	// depName is the dependency actually being resolved, which may be a virtual
	// name none of the candidates are named after (they only Provides: it), so
	// it's passed in rather than inferred from candidates[0].Name.
	var versionConstraints []VersionConstraint
	hasVersionConstraint := false
	if len(candidates) > 0 {
		isDirect := hasDirectDependency(parentPkg.Requires, depName)

		versionConstraints, hasVersionConstraint = extractVersionRequirement(parentPkg.RequiresVer, depName)

		// If it's a direct dependency, ignore version constraints from alternatives
		if isDirect && hasVersionConstraint {
			var directConstraints []VersionConstraint
			for _, constraint := range versionConstraints {
				if constraint.Alternative == "" {
					directConstraints = append(directConstraints, constraint)
				}
			}
			// See the identical guard in the main resolution loop: decide per
			// original dependency term, not from whether depName is a first
			// alternative anywhere. Strip alternative-tagged constraints when
			// depName is NOT its OR term's own first alternative, OR when depName
			// also appears as a separate bare, mandatory term — so a genuinely
			// bare depName that merely co-occurs in an unrelated OR clause (e.g.
			// "logsave | e2fsprogs (<< X)", or "Depends: foo, foo (= 1) | bar")
			// ignores that clause's constraint, while a pure OR term like
			// "libfoo-abi (= 3) | fallback" still enforces libfoo-abi's own pin
			// when selecting its providers.
			if !isOwnFirstAlternative(parentPkg.RequiresVer, depName) || hasBareMandatoryTerm(parentPkg.RequiresVer, depName) {
				versionConstraints = directConstraints
				hasVersionConstraint = len(directConstraints) > 0
			}
		}
	}

	if hasVersionConstraint {
		// First pass: look for candidates from the same repo that meet version constraint
		var sameRepoMatches []ospackage.PackageInfo
		var otherRepoMatches []ospackage.PackageInfo

		for _, candidate := range candidates {
			candidateBase, err := extractRepoBase(candidate.URL)
			if err != nil {
				continue
			}

			// Check if all version constraints are satisfied
			allConstraintsSatisfied := true
			for _, constraint := range versionConstraints {
				// Check if main package (candidate) satisfies constraint
				mainSatisfied := false
				if constraint.Op != "" && constraint.Ver != "" {
					if ver, ok := versionForDependency(candidate, depName); ok {
						mainSatisfied = debVersionSatisfies(ver, constraint.Op, constraint.Ver)
					}
				} else {
					// No version constraint, satisfied by default
					mainSatisfied = true
				}

				// If main package doesn't satisfy and we have alternatives, check if this is acceptable
				// In candidate selection, we should NOT mark alternatives as satisfied automatically
				// since we're evaluating the specific candidate for the main package
				alternativeSatisfied := false

				if !mainSatisfied && !alternativeSatisfied {
					allConstraintsSatisfied = false
					break
				}
			}

			if allConstraintsSatisfied {
				// if candidateBase == parentBase {
				if matchesRepoBase(parentBase, candidateBase) {
					sameRepoMatches = append(sameRepoMatches, candidate)
				} else {
					otherRepoMatches = append(otherRepoMatches, candidate)
				}
			}
		}

		// Compare using APT priority behavior rules between sameRepoMatches[0] and otherRepoMatches[0]
		if len(sameRepoMatches) > 0 && len(otherRepoMatches) > 0 {
			// Apply APT priority behavior comparison
			if comparePriorityBehavior(sameRepoMatches[0], otherRepoMatches[0]) {
				return sameRepoMatches[0], nil
			} else {
				return otherRepoMatches[0], nil
			}
		}

		// Priority 1: return first match from same repo (if only sameRepo has matches)
		if len(sameRepoMatches) > 0 {
			return sameRepoMatches[0], nil
		}

		// Priority 2: return first match from other repos (if only otherRepo has matches)
		if len(otherRepoMatches) > 0 {
			return otherRepoMatches[0], nil
		}

		constraintStr := ""
		for i, vc := range versionConstraints {
			if i > 0 {
				constraintStr += ", "
			}
			constraintStr += vc.Op + vc.Ver
		}
		return ospackage.PackageInfo{}, fmt.Errorf("no candidates satisfy version constraints: %s", constraintStr)
	}

	/////////////////////////////////////
	// B: if version is not specified
	//////////////////////////////////////

	// Check for empty candidates list
	if len(candidates) == 0 {
		return ospackage.PackageInfo{}, fmt.Errorf("no candidates provided for selection")
	}

	// If only one candidate, return it
	if len(candidates) == 1 {
		return candidates[0], nil
	}

	// Rule 1: find all candidates with the same base URL and candidates from other repos
	var sameBaseCandidates []ospackage.PackageInfo
	var otherBaseCandidates []ospackage.PackageInfo
	for _, candidate := range candidates {
		candidateBase, err := extractRepoBase(candidate.URL)
		if err != nil {
			continue
		}
		// if candidateBase == parentBase {
		if matchesRepoBase(parentBase, candidateBase) {
			sameBaseCandidates = append(sameBaseCandidates, candidate)
		} else {
			otherBaseCandidates = append(otherBaseCandidates, candidate)
		}
	}

	// Compare using APT priority behavior rules between same base and other base candidates
	if len(sameBaseCandidates) > 0 && len(otherBaseCandidates) > 0 {
		// Apply APT priority behavior comparison
		if comparePriorityBehavior(sameBaseCandidates[0], otherBaseCandidates[0]) {
			return sameBaseCandidates[0], nil
		} else {
			return otherBaseCandidates[0], nil
		}
	}

	// If we only have candidates with the same base URL, return the first (latest) one
	if len(sameBaseCandidates) > 0 {
		return sameBaseCandidates[0], nil
	}

	// If we only have candidates from other repos, return the first (latest) one
	if len(otherBaseCandidates) > 0 {
		return otherBaseCandidates[0], nil
	}

	// Fallback: return first candidate if no categorization worked
	return candidates[0], nil
}
