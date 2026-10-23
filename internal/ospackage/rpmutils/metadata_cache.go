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
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
)

type rpmPrimaryLocationCache struct {
	PrimaryHref string              `json:"primary_href"`
	Primary     rpmPrimaryReference `json:"primary"`
}

type rpmRawMetadataCache struct {
	MetadataID string              `json:"metadata_id"`
	Primary    rpmPrimaryReference `json:"primary"`
	DataFile   string              `json:"data_file"`
	// ContentDigest is the SHA-256 of the payload, recorded when repomd.xml
	// publishes no checksum so that a later load can still detect a payload that
	// changed without changing size.
	ContentDigest string `json:"content_digest,omitempty"`
}

type rpmPrimaryReference struct {
	Href string `json:"href"`
	// ChecksumType is the repomd.xml checksum type in its canonical upper-case
	// form (for example "SHA256"); repomd.xml itself usually spells it in
	// lower case.
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
	return ref.Href == other.Href &&
		ref.Size == other.Size &&
		ref.OpenSize == other.OpenSize &&
		strings.EqualFold(ref.ChecksumType, other.ChecksumType) &&
		strings.EqualFold(ref.Checksum, other.Checksum) &&
		strings.EqualFold(ref.OpenChecksumType, other.OpenChecksumType) &&
		strings.EqualFold(ref.OpenChecksum, other.OpenChecksum)
}

func rpmMetadataID(baseURL, metadataHref string) string {
	metadataURL := strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(metadataHref, "/")
	sum := sha256.Sum256([]byte(metadataURL))
	return hex.EncodeToString(sum[:])
}

func redactURLForLog(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Opaque != "" || u.Host == "" {
		return redactedURLPlaceholder
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// redactedURLPlaceholder stands in for a URL that cannot be safely reduced to
// scheme and host.
const redactedURLPlaceholder = "<redacted-url>"

// malformedHeaderPlaceholder replaces the diagnostic for a malformed protocol
// message, which echoes the offending header line.
const malformedHeaderPlaceholder = "malformed MIME header (details redacted)"

// urlInTextPattern finds URL-like text in an error message, including text
// quoted by %q, whose escape sequences are consumed as pairs.
var urlInTextPattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.\-]*://(?:[^\s"\\]|\\.)*`)

// quotedTargetPattern finds the quoted target in the diagnostics net/http and
// net/url print for a reference they could not parse, as in `failed to parse
// Location header "..."` and `parse "..."`. The target may be relative or
// protocol-relative, which urlInTextPattern cannot recognise.
var quotedTargetPattern = regexp.MustCompile(`(Location header |\bparse )"(?:[^"\\]|\\.)*"`)

// quotedInner is s as %q would print it, without the surrounding quotes.
func quotedInner(s string) string {
	quoted := strconv.Quote(s)
	return quoted[1 : len(quoted)-1]
}

// redactedError displays only the sanitized message but unwraps to the original
// error, so errors.Is and errors.As keep working on the cause (for example
// context.DeadlineExceeded or a *net.OpError). Callers must not print values
// obtained by unwrapping it.
type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }

// walkErrorChain calls visit for every error in err's chain, walking both
// Unwrap() error and Unwrap() []error (errors.Join, fmt.Errorf with several %w)
// depth-first.
func walkErrorChain(err error, visit func(error)) {
	if err == nil {
		return
	}
	visit(err)
	switch w := err.(type) {
	case interface{ Unwrap() []error }:
		for _, inner := range w.Unwrap() {
			walkErrorChain(inner, visit)
		}
	case interface{ Unwrap() error }:
		walkErrorChain(w.Unwrap(), visit)
	}
}

// collectErrorURLs returns the URL of every *url.Error in err's chain.
func collectErrorURLs(err error) []string {
	var urls []string
	walkErrorChain(err, func(e error) {
		if urlErr, ok := e.(*url.Error); ok {
			urls = append(urls, urlErr.URL)
		}
	})
	return urls
}

// collectProtocolDiagnostics returns the message of every textproto.ProtocolError
// in err's chain. These quote the offending header line verbatim, so a malformed
// Location or Set-Cookie header would leak its value.
func collectProtocolDiagnostics(err error) []string {
	var diagnostics []string
	walkErrorChain(err, func(e error) {
		if protoErr, ok := e.(textproto.ProtocolError); ok {
			diagnostics = append(diagnostics, string(protoErr))
		}
	})
	return diagnostics
}

// redactErrorURL removes credentials, query strings and fragments from every URL
// an error mentions. It replaces the requested URL and the URLs carried by
// *url.Error values in the chain (redirect targets, and the form with the
// password masked by net/http), in the plain and the %q-quoted form url.Error
// prints, taking the longest match at each position. URLs that appear only in
// unstructured text, such as a malformed Location header (absolute, relative or
// protocol-relative), are found by pattern. Protocol diagnostics that quote a
// header line are replaced wholesale.
func redactErrorURL(err error, rawURL string) error {
	if err == nil {
		return nil
	}

	candidates := append([]string{rawURL}, collectErrorURLs(err)...)

	replacements := map[string]string{}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		redacted := redactURLForLog(candidate)
		replacements[candidate] = redacted
		replacements[quotedInner(candidate)] = quotedInner(redacted)
		if u, parseErr := url.Parse(candidate); parseErr == nil && u.Opaque == "" {
			replacements[u.String()] = redacted
		}
	}
	olds := make([]string, 0, len(replacements))
	for old := range replacements {
		olds = append(olds, old)
	}
	sort.Slice(olds, func(i, j int) bool { return len(olds[i]) > len(olds[j]) })
	pairs := make([]string, 0, 2*len(olds))
	for _, old := range olds {
		pairs = append(pairs, old, replacements[old])
	}

	message := err.Error()
	for _, diagnostic := range collectProtocolDiagnostics(err) {
		message = strings.ReplaceAll(message, diagnostic, malformedHeaderPlaceholder)
	}
	message = strings.NewReplacer(pairs...).Replace(message)
	message = quotedTargetPattern.ReplaceAllString(message, `${1}"`+redactedURLPlaceholder+`"`)
	message = urlInTextPattern.ReplaceAllStringFunc(message, redactURLForLog)
	if message == err.Error() {
		return err
	}
	return &redactedError{msg: message, err: err}
}

// writeFileAtomic replaces filePath with data through a synced temporary file in
// the same directory, so readers see either the old or the new content and never
// a truncated file. An error that mentions the file having been replaced means
// the new content is already visible and only syncing the directory entry
// failed, so it may not survive a crash.
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
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temporary cache file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temporary cache file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary cache file: %w", err)
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		return fmt.Errorf("replace cache file %s: %w", filePath, err)
	}
	dir, err := os.Open(filepath.Dir(filePath))
	if err != nil {
		return fmt.Errorf("cache file %s replaced but opening its directory to sync the rename failed: %w", filePath, err)
	}
	if syncErr := dir.Sync(); syncErr != nil {
		_ = dir.Close()
		return fmt.Errorf("cache file %s replaced but syncing directory %s failed: %w", filePath, filepath.Dir(filePath), syncErr)
	}
	if closeErr := dir.Close(); closeErr != nil {
		return fmt.Errorf("cache file %s replaced but closing directory %s failed: %w", filePath, filepath.Dir(filePath), closeErr)
	}
	cleanup = false
	return nil
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
		return rpmPrimaryReference{}, fmt.Errorf("invalid rpm primary location cache %s: %w", cacheFile, err)
	}
	if cache.Primary.isPartialChecksum() {
		return rpmPrimaryReference{}, fmt.Errorf("partial checksum in primary location cache %s", cacheFile)
	}
	if cache.Primary.Href == "" && cache.Primary.Checksum != "" {
		return rpmPrimaryReference{}, fmt.Errorf("checksum without href in primary location cache %s", cacheFile)
	}
	if cache.Primary.Href != "" {
		if cache.PrimaryHref != "" && cache.PrimaryHref != cache.Primary.Href {
			return rpmPrimaryReference{}, fmt.Errorf("primary location cache %s disagrees with itself: primary_href %q, primary.href %q",
				cacheFile, cache.PrimaryHref, cache.Primary.Href)
		}
		return cache.Primary, nil
	}
	if cache.PrimaryHref == "" {
		return rpmPrimaryReference{}, fmt.Errorf("empty primary href in cache %s", cacheFile)
	}
	return rpmPrimaryReference{Href: cache.PrimaryHref}, nil
}

// rpmRawMetadataManifestPath is the manifest that names the current payload for
// metadataHref. Payloads are named by rpmRawMetadataPayloadPath; the manifest
// records which one is current.
func rpmRawMetadataManifestPath(xmlCacheDir, metadataHref string) string {
	hrefHash := sha256.Sum256([]byte(metadataHref))
	return filepath.Join(xmlCacheDir, fmt.Sprintf("primary_%s.cache.json", hex.EncodeToString(hrefHash[:])[:16]))
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

// contentDigest is the hex SHA-256 of data.
func contentDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// rpmRawMetadataPayloadPath names a payload after its content identity, so two
// revisions never share a file: the checksum repomd.xml publishes when there is
// one, otherwise a digest of the payload itself.
func rpmRawMetadataPayloadPath(xmlCacheDir, metadataHref string, primary rpmPrimaryReference, data []byte) string {
	identity := strings.ToUpper(primary.ChecksumType) + ":" + strings.ToLower(primary.Checksum)
	if !primary.hasIntegrity() {
		identity = "CONTENT:" + contentDigest(data)
	}

	hrefHash := sha256.Sum256([]byte(metadataHref))
	checksumHash := sha256.Sum256([]byte(identity))
	ext := extractMetadataExtension(metadataHref)
	dataFile := fmt.Sprintf("primary_%s_%s%s", hex.EncodeToString(hrefHash[:])[:16], hex.EncodeToString(checksumHash[:])[:16], ext)
	return filepath.Join(xmlCacheDir, dataFile)
}

// afterRawManifestRead runs between loading the manifest and opening the payload
// it names. Tests use it to interleave a writer that replaces the manifest.
var afterRawManifestRead = func() {}

// loadRPMRawMetadataCache returns the cached primary payload for metadataHref.
// If the payload the manifest named is gone by the time it is opened, a writer
// has published a newer revision and pruned it, so the manifest is read once more
// and the call answers for the revision that is current now.
func loadRPMRawMetadataCache(xmlCacheDir, metadataHref, metadataID string, primary rpmPrimaryReference) ([]byte, string, error) {
	data, dataPath, payloadGone, err := readRPMRawMetadataCache(xmlCacheDir, metadataHref, metadataID, primary)
	if payloadGone {
		data, dataPath, _, err = readRPMRawMetadataCache(xmlCacheDir, metadataHref, metadataID, primary)
	}
	return data, dataPath, err
}

// readRPMRawMetadataCache reports payloadGone when the manifest was read but the
// payload it names does not exist. The returned path is the manifest until the
// payload it names is known, and that payload after.
func readRPMRawMetadataCache(xmlCacheDir, metadataHref, metadataID string, primary rpmPrimaryReference) (data []byte, dataPath string, payloadGone bool, err error) {
	metaPath := rpmRawMetadataManifestPath(xmlCacheDir, metadataHref)
	dataPath = metaPath
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, dataPath, false, err
	}

	var cache rpmRawMetadataCache
	if err := json.Unmarshal(metaData, &cache); err != nil {
		return nil, dataPath, false, fmt.Errorf("invalid rpm raw metadata cache %s: %w", metaPath, err)
	}
	if cache.MetadataID != metadataID {
		return nil, dataPath, false, fmt.Errorf("rpm raw metadata cache identity mismatch in %s: got %s, want %s",
			metaPath, cache.MetadataID, metadataID)
	}
	if primary.Href != "" && !cache.Primary.matches(primary) {
		return nil, dataPath, false, fmt.Errorf("rpm raw metadata cache primary reference mismatch in %s", metaPath)
	}
	if filepath.Base(cache.DataFile) != cache.DataFile || cache.DataFile == "" {
		return nil, dataPath, false, fmt.Errorf("invalid rpm raw metadata data file in %s", metaPath)
	}

	dataPath = filepath.Join(xmlCacheDir, cache.DataFile)
	afterRawManifestRead()
	data, err = os.ReadFile(dataPath)
	if err != nil {
		return nil, dataPath, os.IsNotExist(err), err
	}
	if len(data) == 0 {
		return nil, dataPath, false, fmt.Errorf("empty rpm raw metadata cache %s", dataPath)
	}
	// cache.Primary equals primary whenever one was requested, and is the only
	// integrity information available when none was.
	if err := verifyRPMPrimaryBytes(data, cache.Primary); err != nil {
		return nil, dataPath, false, fmt.Errorf("verify cached RPM primary metadata %s: %w", dataPath, err)
	}
	if !cache.Primary.hasIntegrity() {
		if got := contentDigest(data); !strings.EqualFold(got, cache.ContentDigest) {
			return nil, dataPath, false, fmt.Errorf("verify cached RPM primary metadata %s: content digest mismatch: got %s, want %q",
				dataPath, got, cache.ContentDigest)
		}
	}

	return data, dataPath, false, nil
}

func saveRPMRawMetadataCache(xmlCacheDir, metadataHref, metadataID string, primary rpmPrimaryReference, data []byte) error {
	if err := verifyRPMPrimaryBytes(data, primary); err != nil {
		return fmt.Errorf("refusing to cache invalid RPM primary metadata: %w", err)
	}

	// Writing a payload, publishing the manifest and pruning must not interleave
	// with another build doing the same: a pruner holding stale information about a
	// payload could otherwise delete one a writer has just republished. Without the
	// lock the payload and manifest are still written, which is safe on its own,
	// but nothing is pruned.
	unlock, lockErr := lockRPMRawMetadata(xmlCacheDir, metadataHref)
	if lockErr != nil {
		logger.Logger().Warnf("Failed to lock the RPM primary metadata cache; superseded payloads are not pruned: %v", lockErr)
	} else {
		defer unlock()
	}

	dataPath := rpmRawMetadataPayloadPath(xmlCacheDir, metadataHref, primary, data)
	metaPath := rpmRawMetadataManifestPath(xmlCacheDir, metadataHref)
	if err := writeFileAtomic(dataPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write rpm raw metadata %s: %w", dataPath, err)
	}

	cache := rpmRawMetadataCache{
		MetadataID: metadataID,
		Primary:    primary,
		DataFile:   filepath.Base(dataPath),
	}
	if !primary.hasIntegrity() {
		cache.ContentDigest = contentDigest(data)
	}
	metaData, err := json.Marshal(cache)
	if err != nil {
		return fmt.Errorf("failed to marshal rpm raw metadata cache: %w", err)
	}
	if err := writeFileAtomic(metaPath, metaData, 0644); err != nil {
		return fmt.Errorf("failed to write rpm raw metadata cache %s: %w", metaPath, err)
	}

	if lockErr == nil {
		pruneSupersededRawPayloads(xmlCacheDir, metadataHref, dataPath)
	}
	return nil
}

func rpmRawMetadataLockPath(xmlCacheDir, metadataHref string) string {
	hrefHash := sha256.Sum256([]byte(metadataHref))
	return filepath.Join(xmlCacheDir, fmt.Sprintf("primary_%s.lock", hex.EncodeToString(hrefHash[:])[:16]))
}

// lockRPMRawMetadata takes an exclusive advisory lock on the raw metadata cache
// of metadataHref, shared by every build using the same cache directory, and
// returns the function that releases it.
func lockRPMRawMetadata(xmlCacheDir, metadataHref string) (func(), error) {
	lockPath := rpmRawMetadataLockPath(xmlCacheDir, metadataHref)
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("open lock file %s: %w", lockPath, err)
	}
	for {
		err = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX)
		if err != syscall.EINTR {
			break
		}
	}
	if err != nil {
		_ = lockFile.Close()
		return nil, fmt.Errorf("lock %s: %w", lockPath, err)
	}
	return func() {
		_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		_ = lockFile.Close()
	}, nil
}

// rpmRawPayloadGracePeriod is how long a payload no longer named by the manifest
// is kept. A concurrent writer's payload is only unreferenced between its payload
// and manifest writes, and a reader only needs a payload for the moment between
// reading the manifest and the file, so anything this old is not in use.
const rpmRawPayloadGracePeriod = 10 * time.Minute

// beforeRawPayloadPrune runs just before a superseded payload is removed. Tests
// use it to interleave another cache writer.
var beforeRawPayloadPrune = func(path string) {}

// pruneSupersededRawPayloads removes payloads for metadataHref other than keepPath
// that are older than the grace period, so refreshes of metadata at the same
// href do not accumulate payloads without bound. The caller holds the href's
// cache lock.
func pruneSupersededRawPayloads(xmlCacheDir, metadataHref, keepPath string) {
	log := logger.Logger()

	hrefHash := sha256.Sum256([]byte(metadataHref))
	prefix := fmt.Sprintf("primary_%s_", hex.EncodeToString(hrefHash[:])[:16])
	ext := extractMetadataExtension(metadataHref)

	entries, err := os.ReadDir(xmlCacheDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(xmlCacheDir, name)
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ext) || path == keepPath {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil || !info.Mode().IsRegular() || time.Since(info.ModTime()) < rpmRawPayloadGracePeriod {
			continue
		}
		beforeRawPayloadPrune(path)
		if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
			log.Warnf("Failed to prune superseded RPM primary metadata payload %s: %v", name, rmErr)
		}
	}
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
	if writeErr := writeFileAtomic(filePath, repomdData, 0644); writeErr == nil {
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

	return rpmPrimaryReference{}, fmt.Errorf("failed to parse primary reference from cached repomd files")
}

func extractPrimaryReferenceFromRepomdData(repomdData []byte) (rpmPrimaryReference, error) {
	dec := xml.NewDecoder(bytes.NewReader(repomdData))

	// Walk the whole document, so a response truncated after the primary entry
	// is rejected rather than cached, and keep the first <data type="primary">.
	var found rpmPrimaryReference
	haveFound := false
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
		if haveFound {
			if err := dec.Skip(); err != nil {
				return rpmPrimaryReference{}, fmt.Errorf("error skipping token: %w", err)
			}
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
					err = io.ErrUnexpectedEOF
				}
				return rpmPrimaryReference{}, fmt.Errorf("read primary data in repomd.xml: %w", err)
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
				found, haveFound = primary, true
				break
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
				if primary.Size, err = parseSizeText(text); err != nil {
					return rpmPrimaryReference{}, fmt.Errorf("malformed repomd: primary size: %w", err)
				}
				// Size 0 doubles as "no size published" in rpmPrimaryReference, and
				// no real primary file is empty, so refuse it rather than let it
				// switch size verification off.
				if primary.Size == 0 {
					return rpmPrimaryReference{}, fmt.Errorf("malformed repomd: primary size is 0")
				}
			case "open-size":
				text, err := readElementText(dec)
				if err != nil {
					return rpmPrimaryReference{}, fmt.Errorf("read primary open-size: %w", err)
				}
				if primary.OpenSize, err = parseSizeText(text); err != nil {
					return rpmPrimaryReference{}, fmt.Errorf("malformed repomd: primary open-size: %w", err)
				}
			}
		}
	}

	if haveFound {
		return found, nil
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
	var text strings.Builder
	depth := 0
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.CharData:
			if depth == 0 {
				text.Write(t)
			}
		case xml.StartElement:
			depth++
		case xml.EndElement:
			if depth == 0 {
				return strings.TrimSpace(text.String()), nil
			}
			depth--
		}
	}
}

// parseSizeText parses a repomd.xml size element. A size that is not a
// non-negative integer is an error rather than "no size", so malformed integrity
// metadata cannot switch size verification off.
func parseSizeText(value string) (int64, error) {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("invalid size %q", strings.TrimSpace(value))
	}
	return parsed, nil
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

// cachedPrimaryReferenceFor returns the integrity data cached for metadataHref,
// from the cached repomd.xml or, when that is gone, the primary location cache,
// or a reference holding only the href. Only data that describes metadataHref
// itself is used: the cache may have moved on to a different primary file. A
// repository may publish the href with a leading slash; the reference returned
// always carries the slash-less form, like metadataHref.
func cachedPrimaryReferenceFor(xmlCacheDir, metadataHref string) rpmPrimaryReference {
	metadataHref = strings.TrimLeft(metadataHref, "/")
	describes := func(cached rpmPrimaryReference) bool {
		return strings.TrimLeft(cached.Href, "/") == metadataHref
	}
	if xmlCacheDir != "" {
		if cached, err := loadPrimaryReferenceFromCachedRepomd(xmlCacheDir); err == nil && describes(cached) {
			cached.Href = metadataHref
			return cached
		}
		locationFile := filepath.Join(xmlCacheDir, "primary.location.json")
		if cached, err := loadRPMPrimaryReferenceCache(locationFile); err == nil && describes(cached) {
			cached.Href = metadataHref
			return cached
		}
	}
	return rpmPrimaryReference{Href: metadataHref}
}

// beforeLegacyMigrationPublish runs just before a legacy cache file is published
// to its normalized name. Tests use it to interleave another cache writer.
var beforeLegacyMigrationPublish = func(target string) {}

// beforeCopyPublish runs once a copy is complete in its temporary file and just
// before it is published. Tests use it to interrupt the copy.
var beforeCopyPublish = func(tmpPath, target string) {}

// linkFile creates newname as a hard link to oldname. Tests replace it to
// simulate a filesystem without hard links.
var linkFile = os.Link

// renameNoReplace atomically renames oldpath to newpath and fails with an error
// satisfying os.IsExist if newpath exists, so a file another build published is
// never replaced. Filesystems that cannot do this fail with another error.
// Tests replace it.
var renameNoReplace = func(oldpath, newpath string) error {
	err := unix.Renameat2(unix.AT_FDCWD, oldpath, unix.AT_FDCWD, newpath, unix.RENAME_NOREPLACE)
	if err != nil {
		return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: err}
	}
	return nil
}

// legacyCopyTempPrefix names the temporary files copyFileExclusive writes in the
// target directory. No cache lookup pattern matches it.
const legacyCopyTempPrefix = ".migrating-"

// copyFileExclusive copies source to target with source's permissions. The copy
// is written to a temporary file in target's directory and published only once
// complete, so target is never visible partially written, even if the process
// is killed mid-copy. Publication does not replace an existing target: it fails
// with an error satisfying os.IsExist, so a file another build sharing the cache
// has written is kept. It hard-links the finished copy; where the filesystem has
// no hard links it renames the copy without replacing target. Where neither is
// possible it fails with an error that does not satisfy os.IsExist, rather than
// risk replacing a file written meanwhile.
func copyFileExclusive(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), legacyCopyTempPrefix+"*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	// After a rename the temporary name is already gone; otherwise it is either
	// the partial copy of a failure or the spare name left by the hard link.
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err = io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Chmod(info.Mode().Perm()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}

	beforeCopyPublish(tmpPath, target)
	linkErr := linkFile(tmpPath, target)
	if linkErr == nil || os.IsExist(linkErr) {
		return linkErr
	}
	return renameNoReplace(tmpPath, target)
}

// migrateLegacyRPMMetadataDir moves cache files from the directory earlier
// builds used for a repository URL with a trailing slash, whose name was derived
// from the URL with the slash, into cacheDir, so a warm offline build survives
// the directory name being normalized. Files keep their permissions, an existing
// file is never overwritten, and a legacy file is removed once it has been moved
// or found to exist already, so clearing the cache cannot bring an old index
// back. The legacy directory goes with its last file; a file that could not be
// moved stays, and the move is tried again on the next run. Everything moved is
// verified again when it is read.
//
// A file is published by hard-linking it to its new name, or, where that is not
// possible, by copying it (see copyFileExclusive). Both fail if another build
// sharing the cache has created that name in the meantime, rather than
// replacing the newer file, and a name that exists is always a complete file, so
// the legacy source can be removed once the name is taken.
func migrateLegacyRPMMetadataDir(baseURL, cacheDir string) {
	log := logger.Logger()

	legacyDir := filepath.Join(filepath.Dir(cacheDir), rpmMetadataDirName(baseURL))
	if legacyDir == cacheDir {
		return
	}
	entries, err := os.ReadDir(legacyDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		source := filepath.Join(legacyDir, entry.Name())
		target := filepath.Join(cacheDir, entry.Name())
		beforeLegacyMigrationPublish(target)
		linkErr := linkFile(source, target)
		if linkErr == nil || os.IsExist(linkErr) {
			// Linked, or the normalized directory already has the file.
			_ = os.Remove(source)
			continue
		}
		copyErr := copyFileExclusive(source, target)
		if copyErr == nil || os.IsExist(copyErr) {
			_ = os.Remove(source)
			continue
		}
		log.Warnf("Failed to migrate legacy RPM metadata cache file for %s: %v", redactURLForLog(baseURL), copyErr)
	}
	_ = os.Remove(legacyDir) // only succeeds once empty
}
