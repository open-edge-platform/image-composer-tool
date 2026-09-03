package rpmutils

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"hash"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
)

// rpmParsedMetadataCacheVersion is the schema version of the on-disk parsed RPM
// metadata cache. Bump it whenever the parsed shape changes so a cache written by
// an older binary is treated as a miss and re-parsed, rather than silently
// returning records missing the new data. A pre-versioning cache unmarshals
// Version as the zero value, which never matches a bumped constant, so it is
// correctly treated as stale too.
// v1: PackageInfo.InstalledSizeBytes is now parsed from <size installed=...> and
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

func (ref rpmPrimaryReference) isPartialChecksum() bool {
	return (ref.ChecksumType != "" && ref.Checksum == "") || (ref.ChecksumType == "" && ref.Checksum != "")
}

func (ref rpmPrimaryReference) isPartialOpenChecksum() bool {
	return (ref.OpenChecksumType != "" && ref.OpenChecksum == "") || (ref.OpenChecksumType == "" && ref.OpenChecksum != "")
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

func rpmMetadataID(baseURL, metadataHref string) string {
	metadataURL := strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(metadataHref, "/")
	sum := sha256.Sum256([]byte(metadataURL))
	return hex.EncodeToString(sum[:])
}

func redactURLForLog(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "<redacted-url>"
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	u.Opaque = ""
	return u.String()
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
		redacted.Opaque = ""
		message = strings.ReplaceAll(message, u.String(), redacted.String())
	}
	return fmt.Errorf("%s", message)
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

func rpmRawMetadataCachePaths(xmlCacheDir, metadataHref string) (string, string) {
	hrefHash := sha256.Sum256([]byte(metadataHref))
	hrefHashStr := hex.EncodeToString(hrefHash[:])[:16]
	ext := extractMetadataExtension(metadataHref)
	dataFile := fmt.Sprintf("primary_%s%s", hrefHashStr, ext)
	metaFile := fmt.Sprintf("primary_%s.cache.json", hrefHashStr)
	return filepath.Join(xmlCacheDir, dataFile), filepath.Join(xmlCacheDir, metaFile)
}

func normalizeMetadataHref(metadataHref string) string {
	if idx := strings.IndexAny(metadataHref, "?#"); idx != -1 {
		return metadataHref[:idx]
	}
	return metadataHref
}

func extractMetadataExtension(metadataHref string) string {
	basePath := normalizeMetadataHref(metadataHref)
	ext := strings.ToLower(filepath.Ext(basePath))
	if ext == "" {
		ext = ".xml"
	}
	return ext
}

func rpmRawMetadataPayloadPath(xmlCacheDir, metadataHref string, primary rpmPrimaryReference) string {
	dataPath, _ := rpmRawMetadataCachePaths(xmlCacheDir, metadataHref)
	if !primary.hasIntegrity() {
		return dataPath
	}

	hrefHash := sha256.Sum256([]byte(metadataHref))
	checksumHash := sha256.Sum256([]byte(strings.ToUpper(primary.ChecksumType) + ":" + strings.ToLower(primary.Checksum)))
	ext := extractMetadataExtension(metadataHref)
	dataFile := fmt.Sprintf("primary_%s_%s%s", hex.EncodeToString(hrefHash[:])[:16], hex.EncodeToString(checksumHash[:])[:16], ext)
	return filepath.Join(xmlCacheDir, dataFile)
}

func loadRPMRawMetadataCache(xmlCacheDir, metadataHref, metadataID string, primary rpmPrimaryReference) ([]byte, string, error) {
	dataPath, metaPath := rpmRawMetadataCachePaths(xmlCacheDir, metadataHref)
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

func saveRPMRawMetadataCache(xmlCacheDir, metadataHref, metadataID string, primary rpmPrimaryReference, data []byte) error {
	if err := verifyRPMPrimaryBytes(data, primary); err != nil {
		return fmt.Errorf("refusing to cache invalid RPM primary metadata: %w", err)
	}

	dataPath := rpmRawMetadataPayloadPath(xmlCacheDir, metadataHref, primary)
	_, metaPath := rpmRawMetadataCachePaths(xmlCacheDir, metadataHref)
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

func rpmMetadataCacheDir(baseURL string) (string, error) {
	cacheRoot, err := config.CacheDir()
	if err != nil {
		return "", fmt.Errorf("resolving cache directory: %w", err)
	}

	return filepath.Join(cacheRoot, "rpm-metadata", generateRPMMetadataDir(baseURL)), nil
}

func saveRepomdXMLCache(xmlCacheDir, baseURL string, repomdData []byte) {
	log := logger.Logger()
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
				if primary.isPartialChecksum() {
					return rpmPrimaryReference{}, fmt.Errorf("malformed repomd: partial checksum (type=%q, value=%q)", primary.ChecksumType, primary.Checksum)
				}
				if primary.isPartialOpenChecksum() {
					return rpmPrimaryReference{}, fmt.Errorf("malformed repomd: partial open-checksum (type=%q, value=%q)", primary.OpenChecksumType, primary.OpenChecksum)
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
	if err != nil || parsed < 0 {
		return 0
	}
	return parsed
}

func verifyRPMPrimaryBytes(data []byte, primary rpmPrimaryReference) error {
	if primary.Size > 0 && int64(len(data)) != primary.Size {
		return fmt.Errorf("size mismatch: got %d, want %d", len(data), primary.Size)
	}
	if !primary.hasIntegrity() {
		return nil
	}

	digest, err := digestBytes(primary.ChecksumType, data)
	if err != nil {
		return err
	}
	if !strings.EqualFold(digest, primary.Checksum) {
		return fmt.Errorf("checksum mismatch: got %s, want %s", digest, primary.Checksum)
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

// generateRPMMetadataDir creates a dynamic directory name for RPM metadata storage
// following the same pattern as debutils: <repoId>_<arch>_<type>
func generateRPMMetadataDir(baseURL string) string {
	urlHash := sha256.Sum256([]byte(baseURL))
	urlHashStr := hex.EncodeToString(urlHash[:])[:8]

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

	arch := "x86_64"
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
// ParseRepositoryMetadata) -- that legacy key alone can't tell two hrefs of the
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
	sort.Strings(matches)
	return os.ReadFile(matches[len(matches)-1])
}

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

func saveOriginalXML(xmlCacheDir, metadataHref, fullURL string, data []byte) {
	log := logger.Logger()
	urlHash := sha256.Sum256([]byte(fullURL))
	urlHashStr := hex.EncodeToString(urlHash[:])[:8]
	timestamp := time.Now().Format("2006-01-02_15-04-05")

	normalizedHref := normalizeMetadataHref(metadataHref)
	baseFilename := strings.TrimSuffix(filepath.Base(normalizedHref), filepath.Ext(normalizedHref))
	filename := fmt.Sprintf("%s_%s_%s%s", baseFilename, urlHashStr, timestamp, filepath.Ext(normalizedHref))

	filePath := filepath.Join(xmlCacheDir, filename)
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		log.Warnf("Failed to save original XML file %s: %v", filePath, err)
		return
	}

	log.Infof("Saved original XML file: %s", filePath)
	pruneOldCachedFiles(xmlCacheDir, normalizedHref, fullURL, filepath.Ext(normalizedHref), filePath)
}

func saveUncompressedXML(xmlCacheDir, metadataHref, fullURL string, xmlData []byte) {
	log := logger.Logger()
	urlHash := sha256.Sum256([]byte(fullURL))
	urlHashStr := hex.EncodeToString(urlHash[:])[:8]
	timestamp := time.Now().Format("2006-01-02_15-04-05")

	normalizedHref := normalizeMetadataHref(metadataHref)
	baseFilename := strings.TrimSuffix(filepath.Base(normalizedHref), filepath.Ext(normalizedHref))
	filename := fmt.Sprintf("%s_%s_%s.xml", baseFilename, urlHashStr, timestamp)

	filePath := filepath.Join(xmlCacheDir, filename)
	if err := os.WriteFile(filePath, xmlData, 0644); err != nil {
		log.Warnf("Failed to save uncompressed XML file %s: %v", filePath, err)
		return
	}

	log.Infof("Saved uncompressed XML file: %s", filePath)
	pruneOldCachedFiles(xmlCacheDir, normalizedHref, fullURL, ".xml", filePath)
}
