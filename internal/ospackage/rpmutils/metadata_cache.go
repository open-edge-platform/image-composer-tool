package rpmutils

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
)

type rpmPrimaryLocationCache struct {
	PrimaryHref string              `json:"primary_href"`
	Primary     rpmPrimaryReference `json:"primary"`
}

type rpmPrimaryReference struct {
	Href string `json:"href"`
	// ChecksumType is the repomd.xml checksum type in its canonical upper-case
	// form (for example "SHA256"); repomd.xml itself usually spells it in
	// lower case.
	ChecksumType string `json:"checksum_type,omitempty"`
	Checksum     string `json:"checksum,omitempty"`
}

func (ref rpmPrimaryReference) isPartialChecksum() bool {
	return (ref.ChecksumType != "" && ref.Checksum == "") || (ref.ChecksumType == "" && ref.Checksum != "")
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
				primary.ChecksumType = strings.ToUpper(primary.ChecksumType)
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
