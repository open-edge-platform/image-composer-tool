package rpmutils

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/network"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/runctx"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/system"
)

const (
	metadataMaxDownloadAttempts = 3
	metadataRetryBackoff        = 500 * time.Millisecond
)

func shouldRetryMetadataStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusRequestTimeout,
		http.StatusTooEarly,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func fetchURLWithRetry(ctx context.Context, client *http.Client, targetURL, resourceName string) ([]byte, error) {
	log := logger.Logger()
	safeTargetURL := redactURLForLog(targetURL)

	backoff := metadataRetryBackoff
	var lastErr error

	for attempt := 1; attempt <= metadataMaxDownloadAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("GET %s cancelled before attempt %d: %w", safeTargetURL, attempt, err)
		}
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
		if reqErr != nil {
			return nil, fmt.Errorf("build request for %s: %w", safeTargetURL, reqErr)
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = redactErrorURL(err, targetURL)
		} else {
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				if shouldRetryMetadataStatus(resp.StatusCode) {
					lastErr = fmt.Errorf("transient status: %s", resp.Status)
				} else {
					return nil, fmt.Errorf("GET %s: bad status: %s", safeTargetURL, resp.Status)
				}
			} else {
				body, readErr := io.ReadAll(resp.Body)
				resp.Body.Close()
				if readErr != nil {
					lastErr = readErr
				} else {
					return body, nil
				}
			}
		}

		if attempt == metadataMaxDownloadAttempts {
			break
		}

		log.Warnf("attempt %d/%d downloading %s failed: %v; retrying in %s", attempt, metadataMaxDownloadAttempts, resourceName, lastErr, backoff)
		// Cancel-aware sleep: a SIGINT during backoff aborts within the
		// delay quantum instead of running the full window.
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("GET %s cancelled during retry backoff: %w", safeTargetURL, ctx.Err())
		case <-timer.C:
		}
		backoff *= 2
	}

	return nil, fmt.Errorf("GET %s failed after %d attempts: %w", safeTargetURL, metadataMaxDownloadAttempts, lastErr)
}

func redactErrorURL(err error, rawURL string) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	message = strings.ReplaceAll(message, rawURL, redactURLForLog(rawURL))
	if u, parseErr := url.Parse(rawURL); parseErr == nil {
		redacted := *u
		redacted.User = nil
		redacted.RawQuery = ""
		redacted.Fragment = ""
		message = strings.ReplaceAll(message, u.String(), redacted.String())
	}
	return fmt.Errorf("%s", message)
}

// extractBaseRequirement takes a potentially complex requirement string
// and returns only the base package/capability name.
// Examples:
//   - "libc.so.6(GLIBC_2.38)(64bit)" -> "libc.so.6"
//   - "libsemanage.so.2(LIBSEMANAGE_1.0)(64bit)" -> "libsemanage.so.2"
//   - "(coreutils or busybox)" -> "coreutils"
//   - "filesystem >= 3.0" -> "filesystem"
func extractBaseRequirement(req string) string {
	req = strings.TrimSpace(req)
	if req == "" {
		return ""
	}

	// Handle complex conditional dependencies with "if" clauses
	if strings.Contains(req, ") if ") {
		// Extract content between first '((' and ') if'
		if start := strings.Index(req, "(("); start != -1 {
			if end := strings.Index(req, ") if "); end != -1 {
				inner := req[start+2 : end]
				// Handle multiple operators in priority order
				for _, op := range []string{" >= ", " <= ", " > ", " < ", " = "} {
					if idx := strings.Index(inner, op); idx != -1 {
						return strings.TrimSpace(inner[:idx])
					}
				}
				return strings.TrimSpace(inner)
			}
		}
	}

	// Handle simple parentheses cases
	if strings.HasPrefix(req, "(") && strings.HasSuffix(req, ")") {
		inner := req[1 : len(req)-1]
		inner = strings.TrimSpace(inner)
		// Handle version operators in priority order
		for _, op := range []string{" >= ", " <= ", " > ", " < ", " = "} {
			if idx := strings.Index(inner, op); idx != -1 {
				return strings.TrimSpace(inner[:idx])
			}
		}
		parts := strings.Fields(inner)
		if len(parts) > 0 {
			return parts[0]
		}
		return inner
	}

	// Handle regular cases with operators
	for _, op := range []string{" >= ", " <= ", " > ", " < ", " = "} {
		if idx := strings.Index(req, op); idx != -1 {
			return strings.TrimSpace(req[:idx])
		}
	}

	finalParts := strings.Fields(req)
	if len(finalParts) == 0 {
		return ""
	}
	base := finalParts[0]

	// Remove all parenthesized suffixes like (GLIBC_2.38)(64bit), (LIBSEMANAGE_1.0)(64bit), etc.
	// Keep removing until no more parentheses at the end
	for {
		idx := strings.Index(base, "(")
		if idx == -1 {
			break
		}
		base = base[:idx]
	}

	return base
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
		// Extract clean package name for display (e.g., "libgcrypt" instead of "libgcrypt-1.10.3-1.azl3.x86_64.rpm")
		// Note: Multiple package versions (e.g., glibc-2.38 and glibc-2.35) will both produce "glibc"
		// This causes duplicate node declarations in the DOT file, which is valid - GraphViz merges them.
		// For visualization purposes, we only care about package relationships, not specific versions.
		cleanName := pkg.PkgName
		if _, err := fmt.Fprintf(writer, "  \"%s\";\n", cleanName); err != nil {
			return fmt.Errorf("writing DOT node for %s: %w", cleanName, err)
		}
		for _, dep := range pkg.RequiresPkgNames {
			if dep == "" {
				continue
			}
			// Extract clean dependency name for edges (handles capabilities and package requirements)
			cleanDep := extractBaseRequirement(dep)
			edgeKey := cleanName + "|" + cleanDep
			if edgesWritten[edgeKey] {
				continue
			}
			if _, err := fmt.Fprintf(writer, "  \"%s\" -> \"%s\";\n", cleanName, cleanDep); err != nil {
				return fmt.Errorf("writing DOT edge %s->%s: %w", cleanName, cleanDep, err)
			}
			edgesWritten[edgeKey] = true
		}
	}

	if _, err := fmt.Fprintln(writer, "}"); err != nil {
		return fmt.Errorf("writing DOT footer: %w", err)
	}

	return nil
}

// matchesPackageFilter checks if a package name matches any of the filter patterns.
// Supports glob patterns, exact match, and version-specific prefix match
// (e.g., "kernel-6.17.11" matches "kernel-6.17.11-1.emt3.x86_64.rpm").
func matchesPackageFilter(pkgName string, filter []string) bool {
	if len(filter) == 0 {
		return true // No filter means include all
	}

	for _, pattern := range filter {
		if isGlobPattern(pattern) {
			if ok, err := path.Match(pattern, pkgName); err == nil && ok {
				return true
			}
		}

		// Exact match
		if pkgName == pattern {
			return true
		}
		// Prefix match with version (e.g., "kernel-drivers-gpu-6.17.11" matches "kernel-drivers-gpu-6.17.11-1.emt3.x86_64")
		if strings.HasPrefix(pkgName, pattern+"-") || strings.HasPrefix(pkgName, pattern) {
			return true
		}
	}
	return false
}

// rpmParsedMetadataCacheVersion is the schema version of the on-disk parsed RPM
// metadata cache. Bump it whenever the parsed shape changes so a cache written by
// an older binary is treated as a miss and re-parsed, rather than silently
// returning records missing the new data. A pre-versioning cache unmarshals
// Version as the zero value, which never matches a bumped constant, so it is
// correctly treated as stale too.
// v1: PackageInfo.InstalledSizeBytes is now parsed from <size installed=…> and
// feeds overlay auto-sizing.
// v2: PackageInfo.HasInstalledSize is now parsed alongside InstalledSizeBytes to
// distinguish an explicit zero footprint from a missing size; a v1 cache would
// report every package as HasInstalledSize=false (treated as unknown).
const rpmParsedMetadataCacheVersion = 2

type rpmParsedMetadataCache struct {
	Version     int                     `json:"version"`
	MetadataURL string                  `json:"metadata_url,omitempty"`
	MetadataID  string                  `json:"metadata_id"`
	Primary     rpmPrimaryReference     `json:"primary"`
	Packages    []ospackage.PackageInfo `json:"packages"`
}

type rpmPrimaryLocationCache struct {
	PrimaryHref string              `json:"primary_href"`
	Primary     rpmPrimaryReference `json:"primary"`
}

type rpmRawMetadataCache struct {
	MetadataID string              `json:"metadata_id"`
	Primary    rpmPrimaryReference `json:"primary"`
	DataFile   string              `json:"data_file"`
}

type rpmPrimaryReference struct {
	Href             string `json:"href"`
	ChecksumType     string `json:"checksum_type,omitempty"`
	Checksum         string `json:"checksum,omitempty"`
	Size             int64  `json:"size,omitempty"`
	OpenChecksumType string `json:"open_checksum_type,omitempty"`
	OpenChecksum     string `json:"open_checksum,omitempty"`
	OpenSize         int64  `json:"open_size,omitempty"`
}

func (ref rpmPrimaryReference) hasIntegrity() bool {
	return ref.ChecksumType != "" && ref.Checksum != ""
}

func (ref rpmPrimaryReference) matches(other rpmPrimaryReference) bool {
	if ref.Href != other.Href {
		return false
	}
	if ref.hasIntegrity() || other.hasIntegrity() {
		return strings.EqualFold(ref.ChecksumType, other.ChecksumType) && strings.EqualFold(ref.Checksum, other.Checksum)
	}
	return true
}

func rpmMetadataID(baseURL, gzHref string) string {
	metadataURL := strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(gzHref, "/")
	sum := sha256.Sum256([]byte(metadataURL))
	return hex.EncodeToString(sum[:])
}

func redactURLForLog(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func writeFileAtomic(filePath string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(filePath), "."+filepath.Base(filePath)+"-*")
	if err != nil {
		return fmt.Errorf("create temporary cache file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	n, err := tmp.Write(data)
	if err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary cache file: %w", err)
	}
	if n != len(data) {
		_ = tmp.Close()
		return fmt.Errorf("write temporary cache file: short write: wrote %d of %d bytes", n, len(data))
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temporary cache file: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temporary cache file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary cache file: %w", err)
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		return fmt.Errorf("replace cache file %s: %w", filePath, err)
	}
	if dir, err := os.Open(filepath.Dir(filePath)); err == nil {
		if syncErr := dir.Sync(); syncErr != nil {
			_ = dir.Close()
			return fmt.Errorf("sync cache directory %s: %w", filepath.Dir(filePath), syncErr)
		}
		if closeErr := dir.Close(); closeErr != nil {
			return fmt.Errorf("close cache directory %s: %w", filepath.Dir(filePath), closeErr)
		}
	}
	cleanup = false
	return nil
}

func loadRPMParsedMetadataCache(cacheFile string) (*rpmParsedMetadataCache, error) {
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		return nil, err
	}

	var cache rpmParsedMetadataCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("invalid rpm metadata cache: %w", err)
	}

	return &cache, nil
}

func saveRPMParsedMetadataCache(cacheFile, metadataID string, primary rpmPrimaryReference, pkgs []ospackage.PackageInfo) error {
	cache := rpmParsedMetadataCache{
		Version:    rpmParsedMetadataCacheVersion,
		MetadataID: metadataID,
		Primary:    primary,
		Packages:   pkgs,
	}

	data, err := json.Marshal(cache)
	if err != nil {
		return fmt.Errorf("failed to marshal rpm metadata cache: %w", err)
	}

	if err := writeFileAtomic(cacheFile, data, 0600); err != nil {
		return fmt.Errorf("failed to write rpm metadata cache: %w", err)
	}

	return nil
}

func parsedMetadataCacheMatches(cache *rpmParsedMetadataCache, metadataID string, primary rpmPrimaryReference) bool {
	if cache.Version != rpmParsedMetadataCacheVersion {
		return false
	}
	if cache.MetadataID != metadataID {
		return false
	}
	if primary.Href == "" {
		return true
	}
	return cache.Primary.matches(primary)
}

func saveRPMPrimaryLocationCache(cacheFile string, primary rpmPrimaryReference) error {
	cache := rpmPrimaryLocationCache{PrimaryHref: primary.Href, Primary: primary}

	data, err := json.Marshal(cache)
	if err != nil {
		return fmt.Errorf("failed to marshal rpm primary location cache: %w", err)
	}

	if err := writeFileAtomic(cacheFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write rpm primary location cache: %w", err)
	}

	return nil
}

func loadRPMPrimaryReferenceCache(cacheFile string) (rpmPrimaryReference, error) {
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		return rpmPrimaryReference{}, err
	}

	var cache rpmPrimaryLocationCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return rpmPrimaryReference{}, fmt.Errorf("invalid rpm primary location cache: %w", err)
	}
	if cache.Primary.Href != "" {
		return cache.Primary, nil
	}
	if cache.PrimaryHref == "" {
		return rpmPrimaryReference{}, fmt.Errorf("empty primary href in cache")
	}
	return rpmPrimaryReference{Href: cache.PrimaryHref}, nil
}

func rpmRawMetadataCachePaths(xmlCacheDir, gzHref string) (string, string) {
	hrefHash := sha256.Sum256([]byte(gzHref))
	hrefHashStr := hex.EncodeToString(hrefHash[:])[:16]
	ext := strings.ToLower(filepath.Ext(gzHref))
	if ext == "" {
		ext = ".xml"
	}
	dataFile := fmt.Sprintf("primary_%s%s", hrefHashStr, ext)
	metaFile := fmt.Sprintf("primary_%s.cache.json", hrefHashStr)
	return filepath.Join(xmlCacheDir, dataFile), filepath.Join(xmlCacheDir, metaFile)
}

func loadRPMRawMetadataCache(xmlCacheDir, gzHref, metadataID string, primary rpmPrimaryReference) ([]byte, string, error) {
	dataPath, metaPath := rpmRawMetadataCachePaths(xmlCacheDir, gzHref)
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, dataPath, err
	}

	var cache rpmRawMetadataCache
	if err := json.Unmarshal(metaData, &cache); err != nil {
		return nil, dataPath, fmt.Errorf("invalid rpm raw metadata cache %s: %w", metaPath, err)
	}
	if cache.MetadataID != metadataID {
		return nil, dataPath, fmt.Errorf("rpm raw metadata cache identity mismatch in %s", metaPath)
	}
	if primary.Href != "" && !cache.Primary.matches(primary) {
		return nil, dataPath, fmt.Errorf("rpm raw metadata cache primary reference mismatch in %s", metaPath)
	}
	if filepath.Base(cache.DataFile) != cache.DataFile || cache.DataFile == "" {
		return nil, dataPath, fmt.Errorf("invalid rpm raw metadata data file in %s", metaPath)
	}

	dataPath = filepath.Join(xmlCacheDir, cache.DataFile)
	data, err := os.ReadFile(dataPath)
	if err != nil {
		return nil, dataPath, err
	}
	if len(data) == 0 {
		return nil, dataPath, fmt.Errorf("empty rpm raw metadata cache %s", dataPath)
	}
	if err := verifyRPMPrimaryBytes(data, primary); err != nil {
		return nil, dataPath, fmt.Errorf("verify cached RPM primary metadata %s: %w", dataPath, err)
	}

	return data, dataPath, nil
}

func saveRPMRawMetadataCache(xmlCacheDir, gzHref, metadataID string, primary rpmPrimaryReference, data []byte) error {
	if err := verifyRPMPrimaryBytes(data, primary); err != nil {
		return fmt.Errorf("refusing to cache invalid RPM primary metadata: %w", err)
	}

	dataPath, metaPath := rpmRawMetadataCachePaths(xmlCacheDir, gzHref)
	if err := writeFileAtomic(dataPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write rpm raw metadata %s: %w", dataPath, err)
	}

	cache := rpmRawMetadataCache{
		MetadataID: metadataID,
		Primary:    primary,
		DataFile:   filepath.Base(dataPath),
	}
	metaData, err := json.Marshal(cache)
	if err != nil {
		return fmt.Errorf("failed to marshal rpm raw metadata cache: %w", err)
	}
	if err := writeFileAtomic(metaPath, metaData, 0644); err != nil {
		return fmt.Errorf("failed to write rpm raw metadata cache %s: %w", metaPath, err)
	}

	return nil
}

func filterRPMPackages(pkgs []ospackage.PackageInfo, packageFilter []string) []ospackage.PackageInfo {
	if len(packageFilter) == 0 {
		return pkgs
	}

	filtered := make([]ospackage.PackageInfo, 0, len(pkgs))
	for _, pkg := range pkgs {
		if matchesPackageFilter(pkg.Name, packageFilter) {
			filtered = append(filtered, pkg)
		}
	}

	return filtered
}

func rpmMetadataCacheDir(baseURL string) (string, error) {
	cacheRoot, err := config.CacheDir()
	if err != nil {
		return "", fmt.Errorf("resolving cache directory: %w", err)
	}

	return filepath.Join(cacheRoot, "rpm-metadata", generateRPMMetadataDir(baseURL)), nil
}

// ParseRepositoryMetadata parses repodata/primary.xml(.gz/.zst) from a repository.
// If packageFilter is non-empty, only packages matching the filter are included.
// Repository metadata is cached for repeat and offline dependency resolution.
func ParseRepositoryMetadata(baseURL, metadataHref string, packageFilter []string) ([]ospackage.PackageInfo, error) {
	log := logger.Logger()
	cacheEnabled := !system.IsLiveInstallerExecution()

	fullURL := strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(metadataHref, "/")
	metadataID := rpmMetadataID(baseURL, metadataHref)
	log.Infof("Fetching and parsing repository metadata from %s", redactURLForLog(fullURL))

	// Keep metadata cache under persistent cache-dir so rebuilds can run offline.
	xmlCacheDir := ""
	if cacheEnabled {
		var err error
		xmlCacheDir, err = rpmMetadataCacheDir(baseURL)
		if err != nil {
			log.Warnf("Failed to resolve RPM metadata cache directory: %v", err)
			xmlCacheDir = "" // Disable caching if cache root cannot be resolved
		} else if err := os.MkdirAll(xmlCacheDir, 0755); err != nil {
			log.Warnf("Failed to create XML cache directory: %v", err)
			xmlCacheDir = "" // Disable caching if directory creation fails
		}
	} else {
		log.Debugf("Bypassing RPM metadata cache in live-installer mode")
	}

	primary := rpmPrimaryReference{Href: metadataHref}
	if xmlCacheDir != "" {
		if cachedPrimary, primaryErr := loadPrimaryReferenceFromCachedRepomd(xmlCacheDir); primaryErr == nil {
			primary = cachedPrimary
		}
	}

	// Offline-first behavior: if parsed metadata is cached for this exact metadata identity,
	// return it immediately without any network operation.
	staleMetadataURLMatches := false
	if xmlCacheDir != "" {
		parsedCacheFile := filepath.Join(xmlCacheDir, "primary.parsed.json")
		cached, cacheErr := loadRPMParsedMetadataCache(parsedCacheFile)
		if cacheErr == nil && parsedMetadataCacheMatches(cached, metadataID, primary) {
			log.Infof("Using cached RPM metadata for %s", redactURLForLog(fullURL))
			return filterRPMPackages(cached.Packages, packageFilter), nil
		}
		// A stale (version-mismatched) cache whose MetadataURL still matches fullURL
		// confirms this exact URL was fetched before, which is the only safe
		// evidence for trusting a legacy, baseURL-only-keyed raw cache below (that
		// key alone can't tell two hrefs of the same repo apart).
		staleMetadataURLMatches = cacheErr == nil && cached.MetadataURL == fullURL
		if cacheErr != nil && !os.IsNotExist(cacheErr) {
			log.Warnf("Failed to load cached RPM metadata %s: %v", parsedCacheFile, cacheErr)
		}
	}

	compressedData, cachePath, loadedFromCache, err := loadOrFetchRPMRawMetadata(
		xmlCacheDir,
		baseURL,
		metadataHref,
		metadataID,
		primary,
		fullURL,
		staleMetadataURLMatches,
	)
	if err != nil {
		return nil, err
	}

	infos, xmlData, err := decodeAndParsePrimaryXML(compressedData, metadataHref, baseURL)
	if err != nil && loadedFromCache {
		log.Warnf("Cached RPM metadata %s is invalid: %v; refreshing from repository", cachePath, err)
		compressedData, cachePath, loadedFromCache, err = fetchRPMRawMetadata(xmlCacheDir, baseURL, metadataHref, metadataID, primary, fullURL, cachePath, err)
		if err != nil {
			return nil, err
		}
		infos, xmlData, err = decodeAndParsePrimaryXML(compressedData, metadataHref, baseURL)
	}
	if err != nil {
		return nil, err
	}

	// Save the uncompressed XML file
	if xmlCacheDir != "" {
		saveUncompressedXML(xmlCacheDir, metadataHref, fullURL, xmlData)

		parsedCacheFile := filepath.Join(xmlCacheDir, "primary.parsed.json")
		if saveErr := saveRPMParsedMetadataCache(parsedCacheFile, metadataID, primary, infos); saveErr != nil {
			log.Warnf("Failed to save RPM parsed metadata cache %s: %v", parsedCacheFile, saveErr)
		}
	}

	if loadedFromCache {
		log.Debugf("Parsed RPM metadata from cache %s", cachePath)
	}

	return filterRPMPackages(infos, packageFilter), nil
}

func loadOrFetchRPMRawMetadata(
	xmlCacheDir, baseURL, gzHref, metadataID string,
	primary rpmPrimaryReference,
	fullURL string,
	allowLegacyRaw bool,
) ([]byte, string, bool, error) {
	log := logger.Logger()
	cachePath := ""
	var cacheErr error

	if xmlCacheDir != "" {
		var compressedData []byte
		compressedData, cachePath, cacheErr = loadRPMRawMetadataCache(xmlCacheDir, gzHref, metadataID, primary)
		if cacheErr == nil {
			log.Infof("Using cached RPM primary metadata for %s", redactURLForLog(baseURL))
			return compressedData, cachePath, true, nil
		}
		if !os.IsNotExist(cacheErr) {
			log.Warnf("Failed to load cached RPM primary metadata %s: %v", cachePath, cacheErr)
		}

		if os.IsNotExist(cacheErr) {
			legacyData, legacyErr := findLatestCachedRawMetadata(xmlCacheDir, gzHref, fullURL)
			migrateLegacyRaw := false
			if legacyErr != nil && os.IsNotExist(legacyErr) && allowLegacyRaw {
				legacyData, legacyErr = findLatestCachedRawMetadata(xmlCacheDir, gzHref, baseURL)
				migrateLegacyRaw = legacyErr == nil
			}
			if legacyErr == nil {
				if verifyErr := verifyRPMPrimaryBytes(legacyData, primary); verifyErr != nil {
					cacheErr = fmt.Errorf("verify legacy RPM primary metadata: %w", verifyErr)
					log.Warnf("Failed to verify legacy RPM primary metadata for %s: %v", redactURLForLog(baseURL), verifyErr)
				} else {
					if saveErr := saveRPMRawMetadataCache(xmlCacheDir, gzHref, metadataID, primary, legacyData); saveErr != nil {
						log.Warnf("Failed to migrate legacy RPM primary metadata cache for %s: %v", redactURLForLog(baseURL), saveErr)
					}
					if migrateLegacyRaw {
						saveOriginalXML(xmlCacheDir, gzHref, fullURL, legacyData)
					}
					log.Infof("Using legacy cached RPM primary metadata for %s", redactURLForLog(baseURL))
					return legacyData, cachePath, true, nil
				}
			} else if !os.IsNotExist(legacyErr) {
				log.Warnf("Failed to check legacy RPM primary metadata cache for %s: %v", redactURLForLog(baseURL), legacyErr)
			}
		}
	}

	compressedData, cachePath, _, err := fetchRPMRawMetadata(xmlCacheDir, baseURL, gzHref, metadataID, primary, fullURL, cachePath, cacheErr)
	return compressedData, cachePath, false, err
}

func fetchRPMRawMetadata(
	xmlCacheDir, baseURL, gzHref, metadataID string,
	primary rpmPrimaryReference,
	fullURL, cachePath string,
	cacheErr error,
) ([]byte, string, bool, error) {
	log := logger.Logger()
	client := network.NewSecureHTTPClient()
	compressedData, err := fetchURLWithRetry(runctx.Context(), client, fullURL, "repository metadata")
	if err != nil {
		if cacheErr != nil && !os.IsNotExist(cacheErr) {
			return nil, cachePath, false, fmt.Errorf(
				"failed to fetch compressed metadata for repository %s after invalid cache artifact %s: %w (cache error: %v)",
				redactURLForLog(baseURL),
				cachePath,
				err,
				cacheErr,
			)
		}
		return nil, cachePath, false, fmt.Errorf("failed to fetch compressed metadata for repository %s: %w", redactURLForLog(baseURL), err)
	}
	if err := verifyRPMPrimaryBytes(compressedData, primary); err != nil {
		return nil, cachePath, false, fmt.Errorf("verify downloaded RPM primary metadata for repository %s: %w", redactURLForLog(baseURL), err)
	}

	if xmlCacheDir != "" {
		if saveErr := saveRPMRawMetadataCache(xmlCacheDir, gzHref, metadataID, primary, compressedData); saveErr != nil {
			log.Warnf("Failed to save RPM primary metadata cache for %s; offline cache incomplete: %v", redactURLForLog(baseURL), saveErr)
		}
		saveOriginalXML(xmlCacheDir, gzHref, baseURL, compressedData)
		cachePath, _ = rpmRawMetadataCachePaths(xmlCacheDir, gzHref)
	}

	return compressedData, cachePath, false, nil
}

// decodeAndParsePrimaryXML decompresses compressedData (gzip or zstd, chosen by
// metadataHref's extension) and parses it as a repodata primary.xml document,
// returning the resolved packages and the raw decompressed XML. baseURL resolves
// each package's <location href=...> into an absolute download URL.
func decodeAndParsePrimaryXML(compressedData []byte, metadataHref, baseURL string) ([]ospackage.PackageInfo, []byte, error) {
	var gr io.ReadCloser
	var err error
	ext := strings.ToLower(filepath.Ext(metadataHref))
	reader := bytes.NewReader(compressedData)
	switch ext {
	case ".gz":
		gr, err = gzip.NewReader(reader)

	case ".zst":
		zstDecoder, zerr := zstd.NewReader(reader)
		if zerr != nil {
			return nil, nil, zerr
		}
		gr = zstDecoder.IOReadCloser()

	default:
		err = fmt.Errorf("unsupported compression type %s", ext)
	}

	if err != nil {
		return nil, nil, err
	}
	defer gr.Close()

	// Read and save the uncompressed XML
	var xmlBuffer bytes.Buffer
	teeReader := io.TeeReader(gr, &xmlBuffer)

	dec := xml.NewDecoder(teeReader)

	var (
		infos   []ospackage.PackageInfo
		curInfo *ospackage.PackageInfo
	)

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, nil, err
		}

		switch elem := tok.(type) {
		case xml.StartElement:
			switch elem.Name.Local {
			case "package":
				// start a new PackageInfo
				curInfo = &ospackage.PackageInfo{}
				curInfo.Type = "rpm"

			case "version":
				// Parse version attributes and combine them
				var epoch, ver, rel string
				for _, attr := range elem.Attr {
					switch attr.Name.Local {
					case "epoch":
						epoch = attr.Value
					case "ver":
						ver = attr.Value
					case "rel":
						rel = attr.Value
					}
				}

				// Build version string in format: epoch:ver-rel
				if curInfo != nil {
					// Fill missing fields with "0"
					if epoch == "" {
						epoch = "0"
					}
					if ver == "" {
						ver = "0"
					}
					if rel == "" {
						rel = "0"
					}
					versionStr := fmt.Sprintf("%s:%s-%s", epoch, ver, rel)
					curInfo.Version = versionStr
				}

			case "location":
				// read the href and build full URL + infer Name (filename)
				for _, a := range elem.Attr {
					if a.Name.Local == "href" {
						curInfo.URL = strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(a.Value, "/")
						curInfo.Name = path.Base(a.Value)
						break
					}
				}

			case "format":
				const rpmNS = "http://linux.duke.edu/metadata/rpm"

				// parse everything inside <format> (including rpm:provides/requires)
				section := "" // "provides" | "requires" | ""

			FormatLoop:
				for {
					tok2, err2 := dec.Token()
					if err2 != nil {
						break // EOF or error
					}
					switch inner := tok2.(type) {
					case xml.StartElement:
						switch {
						case inner.Name.Local == "license" && inner.Name.Space == rpmNS:
							if tok3, err := dec.Token(); err == nil {
								if cd, ok := tok3.(xml.CharData); ok && curInfo != nil {
									curInfo.License = strings.TrimSpace(string(cd))
								}
							}

						case inner.Name.Local == "vendor" && inner.Name.Space == rpmNS:
							if tok3, err := dec.Token(); err == nil {
								if cd, ok := tok3.(xml.CharData); ok && curInfo != nil {
									curInfo.Origin = strings.TrimSpace(string(cd))
								}
							}

						case inner.Name.Local == "provides" && inner.Name.Space == rpmNS:
							section = "provides"

						case inner.Name.Local == "requires" && inner.Name.Space == rpmNS:
							section = "requires"

						case inner.Name.Local == "entry" && inner.Name.Space == rpmNS:
							// rpm:entry name="..." ver="..." rel="..." epoch="..." flags="..."
							var name, version, release, epoch, flags string
							for _, a := range inner.Attr {
								switch a.Name.Local {
								case "name":
									name = a.Value
								case "ver":
									version = a.Value
								case "rel":
									release = a.Value
								case "epoch":
									epoch = a.Value
								case "flags":
									flags = a.Value
								}
							}
							if name != "" && curInfo != nil {
								if section == "provides" {
									curInfo.Provides = append(curInfo.Provides, name)
								} else if section == "requires" {
									// Store the base name in Requires
									curInfo.Requires = append(curInfo.Requires, name)
									// Store the base name in RequiresPkgNames
									curInfo.RequiresPkgNames = append(curInfo.RequiresPkgNames, name)

									// Store version constraint with package name prefix in RequiresVer
									if version != "" || release != "" || epoch != "" || flags != "" {
										versionPart := ""
										if epoch != "" {
											versionPart = epoch + ":"
										}
										if version != "" {
											versionPart += version
										}
										if release != "" {
											versionPart += "-" + release
										}

										var versionConstraint string
										if flags != "" && versionPart != "" {
											// Convert flags to readable format (GE = >=, EQ = =, etc.)
											operator := convertFlags(flags)
											versionConstraint = fmt.Sprintf("%s (%s %s)", name, operator, versionPart) // samuel (>=2.3)
										} else if versionPart != "" {
											// Version info but no operator, assume equality
											versionConstraint = fmt.Sprintf("%s = %s", name, versionPart)
										} else {
											// Only package name
											versionConstraint = name
										}
										curInfo.RequiresVer = append(curInfo.RequiresVer, versionConstraint)
									} else {
										// No version constraint, just store the package name
										curInfo.RequiresVer = append(curInfo.RequiresVer, name)
									}
								}
							}

						// some repos list <file> entries inside <format> without a namespace
						case inner.Name.Local == "file" && inner.Name.Space != rpmNS:
							if tok3, err := dec.Token(); err == nil {
								if cd, ok := tok3.(xml.CharData); ok && curInfo != nil {
									curInfo.Files = append(curInfo.Files, strings.TrimSpace(string(cd)))
								}
							}
						}

					case xml.EndElement:
						switch {
						case inner.Name.Local == "provides" && inner.Name.Space == rpmNS:
							section = ""
						case inner.Name.Local == "requires" && inner.Name.Space == rpmNS:
							section = ""
						case inner.Name.Local == "format":
							break FormatLoop
						}
					}
				}

			case "name":
				// canonical package name
				if tok2, err2 := dec.Token(); err2 == nil {
					if cd, ok := tok2.(xml.CharData); ok && curInfo != nil {
						curInfo.Name = string(cd)
						curInfo.PkgName = string(cd) // store canonical package name in PkgName field
					}
				}

			case "description":
				if tok2, err2 := dec.Token(); err2 == nil {
					if cd, ok := tok2.(xml.CharData); ok && curInfo != nil {
						curInfo.Description = string(cd)
					}
				}

			case "arch":
				if tok2, err2 := dec.Token(); err2 == nil {
					if cd, ok := tok2.(xml.CharData); ok && curInfo != nil {
						curInfo.Arch = string(cd)
					}
				}

			case "size":
				// <size package=".." installed=".." archive=".."/> — the installed
				// attribute is the uncompressed on-disk footprint in bytes. It is a
				// self-closing element (no CharData). A malformed value is treated as
				// "unknown" (HasInstalledSize stays false): it only feeds an overlay
				// disk-size estimate.
				for _, attr := range elem.Attr {
					if attr.Name.Local == "installed" {
						if n, perr := strconv.ParseInt(attr.Value, 10, 64); perr == nil && n >= 0 && curInfo != nil {
							curInfo.InstalledSizeBytes = n
							curInfo.HasInstalledSize = true
						}
						break
					}
				}

			case "checksum":
				// primary.xml checksum for the rpm payload (outside <format>)
				cs := ospackage.Checksum{}
				for _, attr := range elem.Attr {
					if attr.Name.Local == "type" {
						cs.Algorithm = strings.ToUpper(attr.Value) // SHA256, etc.
						break
					}
				}
				if tok2, err2 := dec.Token(); err2 == nil {
					if cd, ok := tok2.(xml.CharData); ok && curInfo != nil {
						cs.Value = string(cd)
						curInfo.Checksums = append(curInfo.Checksums, cs)
					}
				}

			case "file":
				// sometimes <file> is outside <format> as well
				if tok2, err2 := dec.Token(); err2 == nil {
					if cd, ok := tok2.(xml.CharData); ok && curInfo != nil {
						curInfo.Files = append(curInfo.Files, strings.TrimSpace(string(cd)))
					}
				}
			}

		case xml.EndElement:
			switch elem.Name.Local {
			case "package":
				if curInfo.Arch == "src" {
					continue
				}
				// finish this package
				infos = append(infos, *curInfo)
			}
		}
	}

	return infos, xmlBuffer.Bytes(), nil
}

func FetchPrimaryURL(repomdURL string) (string, error) {
	primary, err := fetchPrimaryReference(repomdURL)
	if err != nil {
		return "", err
	}
	return primary.Href, nil
}

func fetchPrimaryReference(repomdURL string) (rpmPrimaryReference, error) {
	log := logger.Logger()
	baseURL := strings.TrimSuffix(repomdURL, "/repodata/repomd.xml")
	cacheEnabled := !system.IsLiveInstallerExecution()

	// Create cache directory for repomd-derived artifacts.
	xmlCacheDir := ""
	if cacheEnabled {
		cacheDir, cacheDirErr := rpmMetadataCacheDir(baseURL)
		if cacheDirErr != nil {
			log.Warnf("Failed to resolve RPM metadata cache directory: %v", cacheDirErr)
		} else {
			xmlCacheDir = cacheDir
		}
		if xmlCacheDir != "" {
			if err := os.MkdirAll(xmlCacheDir, 0755); err != nil {
				log.Warnf("Failed to create XML cache directory: %v", err)
				xmlCacheDir = ""
			}
		}
	} else {
		log.Debugf("Bypassing RPM primary location cache in live-installer mode")
	}
	if xmlCacheDir != "" {
		primaryLocationCacheFile := filepath.Join(xmlCacheDir, "primary.location.json")
		if cachedPrimary, cacheErr := loadRPMPrimaryReferenceCache(primaryLocationCacheFile); cacheErr == nil {
			log.Infof("Using cached primary metadata location for %s", redactURLForLog(baseURL))
			return cachedPrimary, nil
		}

		if repomdCachedPrimary, repomdCacheErr := loadPrimaryReferenceFromCachedRepomd(xmlCacheDir); repomdCacheErr == nil {
			if saveErr := saveRPMPrimaryLocationCache(primaryLocationCacheFile, repomdCachedPrimary); saveErr != nil {
				log.Warnf("Failed to save primary location cache %s; offline cache incomplete: %v", primaryLocationCacheFile, saveErr)
			}
			log.Infof("Using primary metadata location from cached repomd for %s", redactURLForLog(baseURL))
			return repomdCachedPrimary, nil
		}
	}

	client := network.NewSecureHTTPClient()
	repomdData, err := fetchURLWithRetry(runctx.Context(), client, repomdURL, "repomd.xml")
	if err != nil {
		return rpmPrimaryReference{}, err
	}

	// Save repomd.xml file using same pattern as debutils
	if xmlCacheDir != "" {
		stablePath := filepath.Join(xmlCacheDir, "repomd.xml")
		if writeErr := writeFileAtomic(stablePath, repomdData, 0644); writeErr != nil {
			log.Warnf("Failed to save repomd.xml cache %s; offline cache incomplete: %v", stablePath, writeErr)
		}

		urlHash := sha256.Sum256([]byte(baseURL))
		urlHashStr := hex.EncodeToString(urlHash[:])[:8]
		timestamp := time.Now().Format("2006-01-02_15-04-05")
		filename := fmt.Sprintf("repomd_%s_%s.xml", urlHashStr, timestamp)
		filePath := filepath.Join(xmlCacheDir, filename)
		if writeErr := os.WriteFile(filePath, repomdData, 0644); writeErr == nil {
			log.Infof("Saved repomd.xml file: %s", filePath)
		} else {
			log.Warnf("Failed to save repomd.xml file: %v", writeErr)
		}
	}

	primary, err := extractPrimaryReferenceFromRepomdData(repomdData)
	if err != nil {
		return rpmPrimaryReference{}, fmt.Errorf("parsing primary location from %s: %w", redactURLForLog(repomdURL), err)
	}

	if xmlCacheDir != "" {
		primaryLocationCacheFile := filepath.Join(xmlCacheDir, "primary.location.json")
		if saveErr := saveRPMPrimaryLocationCache(primaryLocationCacheFile, primary); saveErr != nil {
			log.Warnf("Failed to save primary location cache %s; offline cache incomplete: %v", primaryLocationCacheFile, saveErr)
		}
	}

	return primary, nil
}

func loadPrimaryReferenceFromCachedRepomd(xmlCacheDir string) (rpmPrimaryReference, error) {
	stablePath := filepath.Join(xmlCacheDir, "repomd.xml")
	if data, err := os.ReadFile(stablePath); err == nil {
		if primary, parseErr := extractPrimaryReferenceFromRepomdData(data); parseErr == nil {
			return primary, nil
		}
	}

	pattern := filepath.Join(xmlCacheDir, "repomd_*.xml")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return rpmPrimaryReference{}, fmt.Errorf("glob %q: %w", pattern, err)
	}
	if len(files) == 0 {
		return rpmPrimaryReference{}, fmt.Errorf("no cached repomd files found")
	}

	// Repomd cache file names include a sortable timestamp suffix.
	sort.Strings(files)
	for i := len(files) - 1; i >= 0; i-- {
		data, readErr := os.ReadFile(files[i])
		if readErr != nil {
			continue
		}

		primary, parseErr := extractPrimaryReferenceFromRepomdData(data)
		if parseErr == nil {
			return primary, nil
		}
	}

	return rpmPrimaryReference{}, fmt.Errorf("failed to parse primary location from cached repomd files")
}

func extractPrimaryReferenceFromRepomdData(repomdData []byte) (rpmPrimaryReference, error) {
	dec := xml.NewDecoder(bytes.NewReader(repomdData))

	// Walk the tokens looking for <data type="primary">
	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return rpmPrimaryReference{}, err
		}

		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "data" {
			continue
		}

		var isPrimary bool
		for _, attr := range se.Attr {
			if attr.Name.Local == "type" && attr.Value == "primary" {
				isPrimary = true
				break
			}
		}
		if !isPrimary {
			if err := dec.Skip(); err != nil {
				return rpmPrimaryReference{}, fmt.Errorf("error skipping token: %w", err)
			}
			continue
		}

		primary := rpmPrimaryReference{}
		for {
			tok2, err := dec.Token()
			if err != nil {
				if err == io.EOF {
					break
				}
				return rpmPrimaryReference{}, err
			}

			if ee, ok := tok2.(xml.EndElement); ok && ee.Name.Local == "data" {
				if primary.Href == "" {
					return rpmPrimaryReference{}, fmt.Errorf("primary location not found in repomd.xml")
				}
				primary.ChecksumType = strings.ToUpper(primary.ChecksumType)
				primary.OpenChecksumType = strings.ToUpper(primary.OpenChecksumType)
				return primary, nil
			}

			le, ok := tok2.(xml.StartElement)
			if !ok {
				continue
			}
			switch le.Name.Local {
			case "location":
				for _, attr := range le.Attr {
					if attr.Name.Local == "href" {
						primary.Href = attr.Value
					}
				}
			case "checksum":
				primary.ChecksumType = attrValue(le.Attr, "type")
				text, err := readElementText(dec)
				if err != nil {
					return rpmPrimaryReference{}, fmt.Errorf("read primary checksum: %w", err)
				}
				primary.Checksum = text
			case "open-checksum":
				primary.OpenChecksumType = attrValue(le.Attr, "type")
				text, err := readElementText(dec)
				if err != nil {
					return rpmPrimaryReference{}, fmt.Errorf("read primary open-checksum: %w", err)
				}
				primary.OpenChecksum = text
			case "size":
				text, err := readElementText(dec)
				if err != nil {
					return rpmPrimaryReference{}, fmt.Errorf("read primary size: %w", err)
				}
				primary.Size = parseInt64Text(text)
			case "open-size":
				text, err := readElementText(dec)
				if err != nil {
					return rpmPrimaryReference{}, fmt.Errorf("read primary open-size: %w", err)
				}
				primary.OpenSize = parseInt64Text(text)
			}
		}
	}

	return rpmPrimaryReference{}, fmt.Errorf("primary location not found in repomd.xml")
}

func attrValue(attrs []xml.Attr, name string) string {
	for _, attr := range attrs {
		if attr.Name.Local == name {
			return attr.Value
		}
	}
	return ""
}

func readElementText(dec *xml.Decoder) (string, error) {
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", err
		}
		if cd, ok := tok.(xml.CharData); ok {
			return strings.TrimSpace(string(cd)), nil
		}
		if _, ok := tok.(xml.EndElement); ok {
			return "", nil
		}
	}
}

func parseInt64Text(value string) int64 {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func verifyRPMPrimaryBytes(data []byte, primary rpmPrimaryReference) error {
	if primary.Size > 0 && int64(len(data)) != primary.Size {
		return fmt.Errorf("size mismatch for %s: got %d, want %d", primary.Href, len(data), primary.Size)
	}
	if !primary.hasIntegrity() {
		return nil
	}

	digest, err := digestBytes(primary.ChecksumType, data)
	if err != nil {
		return err
	}
	if !strings.EqualFold(digest, primary.Checksum) {
		return fmt.Errorf("checksum mismatch for %s: got %s, want %s", primary.Href, digest, primary.Checksum)
	}
	return nil
}

func digestBytes(algorithm string, data []byte) (string, error) {
	var h hash.Hash
	switch strings.ToUpper(strings.TrimSpace(algorithm)) {
	case "SHA", "SHA1", "SHA-1":
		h = sha1.New()
	case "SHA256", "SHA-256":
		h = sha256.New()
	case "SHA512", "SHA-512":
		h = sha512.New()
	default:
		return "", fmt.Errorf("unsupported primary metadata checksum type %q", algorithm)
	}
	if _, err := h.Write(data); err != nil {
		return "", fmt.Errorf("hash primary metadata: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func GetRepoMetaDataURL(baseURL, repoMetaXmlPath string) string {
	repoMetaDataURL := strings.TrimRight(baseURL, "/") + "/" + repoMetaXmlPath
	// Check if baseURL is a valid URL,
	if !strings.HasPrefix(repoMetaDataURL, "http://") && !strings.HasPrefix(repoMetaDataURL, "https://") {
		return ""
	}
	return repoMetaDataURL
}

// Helper function to convert RPM flags to readable operators
func convertFlags(flags string) string {
	switch flags {
	case "EQ":
		return "="
	case "GE":
		return ">="
	case "LE":
		return "<="
	case "GT":
		return ">"
	case "LT":
		return "<"
	default:
		return flags
	}
}

// MatchRequested matches requested package names to the best available versions in the repo.
func MatchRequested(requests []string, all []ospackage.PackageInfo) ([]ospackage.PackageInfo, error) {

	var out []ospackage.PackageInfo
	seen := make(map[string]struct{})
	for _, want := range requests {
		if isGlobPattern(want) {
			pkgs, found := ResolveWildcardPackageConflicts(want, all)
			if !found {
				return nil, fmt.Errorf("requested package '%q' not found in repo", want)
			}
			for _, pkg := range pkgs {
				key := fmt.Sprintf("%s=%s", pkg.Name, pkg.Version)
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				out = append(out, pkg)
			}
			continue
		}

		if pkg, found := ResolveTopPackageConflicts(want, all); found {
			key := fmt.Sprintf("%s=%s", pkg.Name, pkg.Version)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, pkg)
		} else {
			return nil, fmt.Errorf("requested package '%q' not found in repo", want)
		}
	}
	return out, nil
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

	// Clear required fields for all and requested
	for i := range all {
		all[i].Requires = nil
		all[i].RequiresPkgNames = nil
	}
	for i := range requested {
		requested[i].Requires = nil
		requested[i].RequiresPkgNames = nil
	}

	neededSet := make(map[string]struct{})
	queue := make([]ospackage.PackageInfo, 0, len(requested))

	// Initialize queue with requested packages
	for _, pi := range requested {
		if pi.Version != "" {
			key := fmt.Sprintf("%s=%s", pi.Name, pi.Version)
			if pkg, ok := byNameVer[key]; ok {
				queue = append(queue, pkg)
				continue
			}
		}
		return nil, fmt.Errorf("requested package %q not in repo listing", pi.Name)
	}

	// Use a map to store results so we can modify them
	resultMap := make(map[string]*ospackage.PackageInfo)

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		if _, seen := neededSet[cur.Name]; seen {
			continue
		}
		neededSet[cur.Name] = struct{}{}

		// Store a copy in the result map so we can modify it
		curCopy := cur
		resultMap[cur.Name] = &curCopy

		// Process dependencies
		for _, dep := range cur.RequiresVer {
			// Use proper dependency name cleaning
			// depName := extractBaseRequirement(dep)
			depName := extractBaseNameFromDep(dep)
			filename, seen := findMatchingKeyInNeededSet(neededSet, depName)
			if depName == "" || neededSet[filename] != struct{}{} {
				continue
			}

			// Check if already resolved
			// if _, seen := neededSet[depName]; seen {
			if seen {
				// ENHANCEMENT: Check version compatibility for already-resolved dependencies
				existing, err := findAllCandidates(cur, depName, queue) //convertMapToSlice(resultMap))
				if err == nil && len(existing) > 0 {
					// Validate that existing package satisfies current requirement
					_, err := resolveMultiCandidates(cur, existing)
					if err != nil {
						// Find the specific version constraint from RequiresVer
						var requiredVer string
						for _, req := range cur.RequiresVer {
							if strings.Contains(req, depName) {
								requiredVer = req
								break
							}
						}
						return nil, fmt.Errorf("conflicting package dependencies: %s_%s requires %s, but %s is already selected",
							cur.Name, cur.Version, requiredVer, existing[0].Name)
					}
				}
				// Append to parent's Requires field even if already resolved
				if resultPkg, exists := resultMap[cur.Name]; exists {
					resultPkg.Requires = append(resultPkg.Requires, filename)
					// Store canonical package name from the already-resolved dependency
					if depPkg, depExists := resultMap[filename]; depExists {
						if depPkg.PkgName != "" {
							resultPkg.RequiresPkgNames = append(resultPkg.RequiresPkgNames, depPkg.PkgName)
						} else {
							// Extract canonical name from filename if PkgName not available
							canonicalName := extractBasePackageNameFromFile(filename)
							resultPkg.RequiresPkgNames = append(resultPkg.RequiresPkgNames, canonicalName)
						}
					}
				}

				continue
			}

			// Find candidates for this dependency
			candidates, err := findAllCandidates(cur, depName, all)
			if err != nil {
				return nil, fmt.Errorf("failed to find candidates for dependency %q of package %q: %v", depName, cur.Name, err)
			}

			if len(candidates) >= 1 {
				chosenCandidate, err := resolveMultiCandidates(cur, candidates)
				if err != nil {
					log.Errorf("failed to resolve multiple candidates for dependency %q of package %q: %v", depName, cur.Name, err)
					return nil, fmt.Errorf("failed to resolve multiple candidates for dependency %q of package %q: %v", depName, cur.Name, err)
				}

				// Update the parent's Requires field with the chosen candidate's name
				if resultPkg, exists := resultMap[cur.Name]; exists {
					resultPkg.Requires = append(resultPkg.Requires, chosenCandidate.Name)
					// Store canonical package name of the chosen candidate
					if chosenCandidate.PkgName != "" {
						resultPkg.RequiresPkgNames = append(resultPkg.RequiresPkgNames, chosenCandidate.PkgName)
					} else {
						// Extract canonical name from filename if PkgName not available
						canonicalName := extractBasePackageNameFromFile(chosenCandidate.Name)
						resultPkg.RequiresPkgNames = append(resultPkg.RequiresPkgNames, canonicalName)
					}
				}

				// Add chosen candidate to the queue for further processing
				queue = append(queue, chosenCandidate)
			} else {
				// FAIL FAST instead of just warning
				// return nil, fmt.Errorf("no candidates found for required dependency %q of package %q", depName, cur.Name)
				log.Warnf("No candidates found for required dependency %q of package %q", depName, cur.Name)
			}
		}
	}

	// Convert result map back to slice
	result := make([]ospackage.PackageInfo, 0, len(resultMap))
	for _, pkg := range resultMap {
		result = append(result, *pkg)
	}

	// Sort result by package name for determinism
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	log.Infof("Successfully resolved %d packages from %d requested packages", len(result), len(requested))
	return result, nil
}

// findMatchingKeyInNeededSet checks if any key in neededSet contains depName as a substring,
// and returns the first matching key whose base package name equals depName.
func findMatchingKeyInNeededSet(neededSet map[string]struct{}, depName string) (string, bool) {
	for k := range neededSet {
		if strings.Contains(k, depName) {
			fileName := extractBasePackageNameFromFile(k)
			if fileName == depName {
				return k, true
			}
		}
	}
	return "", false
}

// generateRPMMetadataDir creates a dynamic directory name for RPM metadata storage
// following the same pattern as debutils: <repoId>_<arch>_<type>
func generateRPMMetadataDir(baseURL string) string {
	// Extract meaningful identifier from URL
	urlHash := sha256.Sum256([]byte(baseURL))
	urlHashStr := hex.EncodeToString(urlHash[:])[:8]

	// Try to extract repository name from URL
	repoId := "rpm"
	if u, err := url.Parse(baseURL); err == nil {
		pathParts := strings.Split(strings.Trim(u.Path, "/"), "/")
		for _, part := range pathParts {
			if part != "" && !strings.Contains(part, ".") {
				repoId = part
				break
			}
		}
	}

	// Detect architecture from URL if possible
	arch := "x86_64" // default
	if strings.Contains(baseURL, "aarch64") {
		arch = "aarch64"
	} else if strings.Contains(baseURL, "i386") {
		arch = "i386"
	} else if strings.Contains(baseURL, "armhf") {
		arch = "armhf"
	}

	return fmt.Sprintf("%s_%s_%s_rpm", repoId, arch, urlHashStr)
}

// findLatestCachedRawMetadata locates the most recently saved raw (compressed)
// primary XML matching metadataHref and hashKey, keyed the same way
// saveOriginalXML names its files. The caller normally passes fullURL as
// hashKey (matching saveOriginalXML, so a repomd href change can't match raw
// content cached under a different path that happens to share the same
// basename); it may also pass the legacy baseURL-only key to look up raw
// metadata saved by a binary built before the fullURL key existed, but only
// once independently confirmed safe (see the staleMetadataURLMatches check in
// ParseRepositoryMetadata) — that legacy key alone can't tell two hrefs of the
// same repo apart. Returns an os.ErrNotExist-wrapped error when none is cached.
func findLatestCachedRawMetadata(xmlCacheDir, metadataHref, hashKey string) ([]byte, error) {
	urlHash := sha256.Sum256([]byte(hashKey))
	urlHashStr := hex.EncodeToString(urlHash[:])[:8]
	ext := filepath.Ext(metadataHref)
	baseFilename := strings.TrimSuffix(filepath.Base(metadataHref), ext)

	matches, err := filepath.Glob(filepath.Join(xmlCacheDir, fmt.Sprintf("%s_%s_*%s", baseFilename, urlHashStr, ext)))
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, os.ErrNotExist
	}
	// Filenames embed a lexically-sortable timestamp ("2006-01-02_15-04-05"), so
	// the last sorted match is the most recently cached raw file.
	sort.Strings(matches)
	return os.ReadFile(matches[len(matches)-1])
}

// pruneOldCachedFiles removes every previously cached file matching
// baseFilename_urlHash_*ext (the same naming scheme saveOriginalXML and
// saveUncompressedXML use) except keepPath, so each (metadataHref, hashKey,
// ext) key retains at most one cached file on disk instead of growing one
// timestamped file per fetch/reparse without bound.
func pruneOldCachedFiles(xmlCacheDir, metadataHref, hashKey, ext, keepPath string) {
	log := logger.Logger()
	urlHash := sha256.Sum256([]byte(hashKey))
	urlHashStr := hex.EncodeToString(urlHash[:])[:8]
	baseFilename := strings.TrimSuffix(filepath.Base(metadataHref), filepath.Ext(metadataHref))

	matches, err := filepath.Glob(filepath.Join(xmlCacheDir, fmt.Sprintf("%s_%s_*%s", baseFilename, urlHashStr, ext)))
	if err != nil {
		log.Warnf("Failed to enumerate old cached metadata files for pruning: %v", err)
		return
	}
	for _, m := range matches {
		if m == keepPath {
			continue
		}
		if rmErr := os.Remove(m); rmErr != nil {
			log.Warnf("Failed to prune old cached metadata file %s: %v", m, rmErr)
		}
	}
}

// saveOriginalXML saves the original compressed XML file to cache directory,
// then prunes any other cached file for the same key so the cache holds at
// most one raw file per (metadataHref, fullURL) pair.
func saveOriginalXML(xmlCacheDir, metadataHref, fullURL string, data []byte) {
	log := logger.Logger()

	// Generate filename from the full metadata URL and timestamp. Hashing fullURL
	// (not just baseURL) keeps this keyed the same way findLatestCachedRawMetadata
	// looks it up: a repomd href change must not match a differently-sourced file
	// that happens to share the same basename.
	urlHash := sha256.Sum256([]byte(fullURL))
	urlHashStr := hex.EncodeToString(urlHash[:])[:8]
	timestamp := time.Now().Format("2006-01-02_15-04-05")

	baseFilename := strings.TrimSuffix(filepath.Base(metadataHref), filepath.Ext(metadataHref))
	filename := fmt.Sprintf("%s_%s_%s%s", baseFilename, urlHashStr, timestamp, filepath.Ext(metadataHref))

	filePath := filepath.Join(xmlCacheDir, filename)
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		log.Warnf("Failed to save original XML file %s: %v", filePath, err)
		return
	}

	log.Infof("Saved original XML file: %s", filePath)
	pruneOldCachedFiles(xmlCacheDir, metadataHref, fullURL, filepath.Ext(metadataHref), filePath)
}

// saveUncompressedXML saves the uncompressed XML content to cache directory,
// then prunes any other cached debug copy for the same key.
func saveUncompressedXML(xmlCacheDir, metadataHref, fullURL string, xmlData []byte) {
	log := logger.Logger()

	// Generate filename from the full metadata URL and timestamp (see
	// saveOriginalXML for why fullURL, not just baseURL, is hashed).
	urlHash := sha256.Sum256([]byte(fullURL))
	urlHashStr := hex.EncodeToString(urlHash[:])[:8]
	timestamp := time.Now().Format("2006-01-02_15-04-05")

	baseFilename := strings.TrimSuffix(filepath.Base(metadataHref), filepath.Ext(metadataHref))
	filename := fmt.Sprintf("%s_%s_%s.xml", baseFilename, urlHashStr, timestamp)

	filePath := filepath.Join(xmlCacheDir, filename)
	if err := os.WriteFile(filePath, xmlData, 0644); err != nil {
		log.Warnf("Failed to save uncompressed XML file %s: %v", filePath, err)
		return
	}

	log.Infof("Saved uncompressed XML file: %s", filePath)
	pruneOldCachedFiles(xmlCacheDir, metadataHref, fullURL, ".xml", filePath)
}
