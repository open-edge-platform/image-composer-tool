package debutils

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/textproto"
	"net/url"
	"path"
	"strconv"
	"strings"
	"unicode"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/clearsign"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/ulikunitz/xz"
)

var errSourceIndexMissing = errors.New("source index not advertised in Release")
var errSourceVerification = errors.New("source repository metadata verification failed")
var errSourceVersionMissing = errors.New("exact source package version not found")

var ErrSourceIndexMissing = errSourceIndexMissing
var ErrSourceVerification = errSourceVerification
var ErrSourceVersionMissing = errSourceVersionMissing

func findSourcePackage(packages []SourcePackageMetadata, name, version string) (*SourcePackageMetadata, error) {
	var match *SourcePackageMetadata
	for index := range packages {
		if packages[index].Name != name || packages[index].Version != version {
			continue
		}
		if match != nil {
			return nil, fmt.Errorf("duplicate source package identity %q %q", name, version)
		}
		match = &packages[index]
	}
	if match == nil {
		return nil, fmt.Errorf("%w: %q %q", errSourceVersionMissing, name, version)
	}
	return match, nil
}

func verifySourceRelease(release, signature, key []byte, inRelease bool) ([]byte, error) {
	keyring, err := parseKeyring(key)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid signing key: %w", errSourceVerification, err)
	}
	if inRelease {
		block, rest := clearsign.Decode(release)
		if block == nil || len(bytes.TrimSpace(rest)) != 0 {
			return nil, fmt.Errorf("%w: invalid InRelease", errSourceVerification)
		}
		if _, err := block.VerifySignature(keyring, &packet.Config{}); err != nil {
			return nil, fmt.Errorf("%w: invalid InRelease signature: %w", errSourceVerification, err)
		}
		return block.Plaintext, nil
	}
	_, err = openpgp.CheckArmoredDetachedSignature(keyring, bytes.NewReader(release), bytes.NewReader(signature), &packet.Config{})
	if err != nil {
		_, err = openpgp.CheckDetachedSignature(keyring, bytes.NewReader(release), bytes.NewReader(signature), &packet.Config{})
	}
	if err != nil {
		return nil, fmt.Errorf("%w: invalid Release signature: %w", errSourceVerification, err)
	}
	return release, nil
}

type SourceIndexMetadata struct {
	Path   string
	SHA256 string
	Size   uint64
}

type sourceIndexMetadata = SourceIndexMetadata

func validSourceComponent(component string) bool {
	invalidComponentCharacter := func(character rune) bool {
		return character == ':' || unicode.IsControl(character) || unicode.IsSpace(character)
	}
	return safeSourcePath(component, false) && strings.IndexFunc(component, invalidComponentCharacter) < 0
}

func discoverSourceIndexes(release []byte, component string) ([]sourceIndexMetadata, error) {
	if !validSourceComponent(component) {
		return nil, fmt.Errorf("invalid source repository component")
	}
	fields, err := textproto.NewReader(bufio.NewReader(bytes.NewReader(release))).ReadMIMEHeader()
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("parse Release metadata: %w", err)
	}
	checksums := fields.Values("SHA256")
	if len(checksums) != 1 {
		return nil, fmt.Errorf("Release must contain exactly one SHA256 section")
	}
	words := strings.Fields(checksums[0])
	if len(words)%3 != 0 {
		return nil, fmt.Errorf("invalid Release SHA256 section")
	}
	paths := []string{component + "/source/Sources.xz", component + "/source/Sources.gz", component + "/source/Sources"}
	entries := make(map[string]sourceIndexMetadata, len(paths))
	for position := 0; position < len(words); position += 3 {
		indexPath := words[position+2]
		if indexPath != paths[0] && indexPath != paths[1] && indexPath != paths[2] {
			continue
		}
		if _, exists := entries[indexPath]; exists {
			return nil, fmt.Errorf("duplicate Release entry for %q", indexPath)
		}
		digest, decodeErr := hex.DecodeString(words[position])
		if decodeErr != nil || len(digest) != sha256.Size {
			return nil, fmt.Errorf("invalid Release SHA256 for %q", indexPath)
		}
		size, parseErr := strconv.ParseUint(words[position+1], 10, 64)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid Release size for %q: %w", indexPath, parseErr)
		}
		entries[indexPath] = sourceIndexMetadata{Path: indexPath, SHA256: strings.ToLower(words[position]), Size: size}
	}
	var result []sourceIndexMetadata
	for _, indexPath := range paths {
		if entry, found := entries[indexPath]; found {
			result = append(result, entry)
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("%w for component %q", errSourceIndexMissing, component)
	}
	return result, nil
}

func parseSourceIndexFromRelease(release, index []byte, indexPath string) ([]SourcePackageMetadata, string, error) {
	digest, err := verifySourceIndex(release, index, indexPath)
	if err != nil {
		return nil, "", err
	}
	var reader io.Reader = bytes.NewReader(index)
	var compressed *gzip.Reader
	if strings.HasSuffix(indexPath, ".gz") {
		compressed, err = gzip.NewReader(reader)
		if err != nil {
			return nil, "", fmt.Errorf("open gzip source index: %w", err)
		}
		reader = compressed
	} else if strings.HasSuffix(indexPath, ".xz") {
		reader, err = xz.NewReader(reader)
		if err != nil {
			return nil, "", fmt.Errorf("open xz source index: %w", err)
		}
	}
	limited := &io.LimitedReader{R: reader, N: (512 << 20) + 1}
	packages, parseErr := ParseSourceIndex(limited)
	if compressed != nil {
		if closeErr := compressed.Close(); closeErr != nil && parseErr == nil {
			return nil, "", fmt.Errorf("close gzip source index: %w", closeErr)
		}
	}
	if parseErr != nil {
		return nil, "", fmt.Errorf("parse source index: %w", parseErr)
	}
	if limited.N == 0 {
		return nil, "", fmt.Errorf("source index exceeds decompressed size limit")
	}
	return packages, digest, nil
}

func verifySourceIndex(release []byte, index []byte, indexPath string) (string, error) {
	if !safeSourcePath(indexPath, false) {
		return "", fmt.Errorf("invalid source index path %q", indexPath)
	}
	name := path.Base(indexPath)
	component := strings.TrimSuffix(indexPath, "/source/"+name)
	if component == indexPath || component == "" ||
		(name != "Sources" && name != "Sources.gz" && name != "Sources.xz") {
		return "", fmt.Errorf("invalid source index path %q", indexPath)
	}
	indexes, err := discoverSourceIndexes(release, component)
	if err != nil {
		return "", err
	}
	for _, entry := range indexes {
		if entry.Path != indexPath {
			continue
		}
		if uint64(len(index)) != entry.Size {
			return "", fmt.Errorf("source index %q size mismatch", indexPath)
		}
		digest := sha256.Sum256(index)
		actual := hex.EncodeToString(digest[:])
		if actual != entry.SHA256 {
			return "", fmt.Errorf("source index %q SHA256 mismatch", indexPath)
		}
		return actual, nil
	}
	return "", fmt.Errorf("%w: %q", errSourceIndexMissing, indexPath)
}

type SourceArtifactMetadata struct {
	Filename string
	URL      string
	Size     uint64
	MD5      string
	SHA1     string
	SHA256   string
}

type SourcePackageMetadata struct {
	Name         string
	Version      string
	Directory    string
	Architecture string
	Artifacts    []SourceArtifactMetadata
	VCS          map[string]string
}

type SourceIndexError struct {
	Stanza int
	Err    error
}

func sourceRepositoryBaseURL(baseURL string) (*url.URL, error) {
	base, err := url.Parse(baseURL)
	if err != nil || base == nil || (base.Scheme != "http" && base.Scheme != "https") ||
		base.Hostname() == "" || base.Opaque != "" || base.RawQuery != "" || base.ForceQuery || base.Fragment != "" ||
		strings.ContainsAny(base.EscapedPath(), "%\\") {
		return nil, fmt.Errorf("invalid source repository URL")
	}
	if base.Path != "" && (path.Clean(base.Path) != strings.TrimSuffix(base.Path, "/") && base.Path != "/") {
		return nil, fmt.Errorf("invalid source repository URL path")
	}
	return base, nil
}

func sourceArtifactsWithURLs(baseURL string, pkg SourcePackageMetadata) ([]SourceArtifactMetadata, error) {
	base, err := sourceRepositoryBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	if !safeSourcePath(pkg.Directory, false) {
		return nil, fmt.Errorf("invalid source artifact directory")
	}
	base.User = nil
	artifacts := make([]SourceArtifactMetadata, len(pkg.Artifacts))
	for index, artifact := range pkg.Artifacts {
		if !safeSourcePath(artifact.Filename, true) {
			return nil, fmt.Errorf("invalid source artifact filename")
		}
		artifactURL := *base
		artifactURL.Path = path.Join(base.Path, pkg.Directory, artifact.Filename)
		if !strings.HasPrefix(artifactURL.Path, "/") {
			artifactURL.Path = "/" + artifactURL.Path
		}
		artifactURL.RawPath = ""
		artifact.URL = artifactURL.String()
		artifacts[index] = artifact
	}
	return artifacts, nil
}

func (err *SourceIndexError) Error() string {
	return fmt.Sprintf("source index stanza %d: %v", err.Stanza, err.Err)
}

func (err *SourceIndexError) Unwrap() error { return err.Err }

func ParseSourceIndex(input io.Reader) ([]SourcePackageMetadata, error) {
	reader := textproto.NewReader(bufio.NewReader(input))
	var packages []SourcePackageMetadata
	for stanza := 1; ; stanza++ {
		fields, err := reader.ReadMIMEHeader()
		if err != nil && err != io.EOF {
			return nil, &SourceIndexError{Stanza: stanza, Err: err}
		}
		if len(fields) != 0 {
			pkg, parseErr := parseSourceStanza(fields)
			if parseErr != nil {
				return nil, &SourceIndexError{Stanza: stanza, Err: parseErr}
			}
			packages = append(packages, pkg)
		}
		if err == io.EOF {
			return packages, nil
		}
	}
}

func parseSourceStanza(fields textproto.MIMEHeader) (SourcePackageMetadata, error) {
	var pkg SourcePackageMetadata
	for key, values := range fields {
		if len(values) != 1 {
			return pkg, fmt.Errorf("duplicate field %q", key)
		}
	}
	pkg.Name = fields.Get("Package")
	if pkg.Name == "" {
		pkg.Name = fields.Get("Source")
	} else if source := fields.Get("Source"); source != "" && source != pkg.Name {
		return pkg, fmt.Errorf("conflicting Package and Source fields")
	}
	pkg.Version = fields.Get("Version")
	pkg.Directory = fields.Get("Directory")
	pkg.Architecture = fields.Get("Architecture")
	if pkg.Name == "" || pkg.Version == "" || !safeSourcePath(pkg.Directory, false) {
		return pkg, fmt.Errorf("missing source identity or invalid Directory")
	}
	pkg.VCS = make(map[string]string)
	for key, values := range fields {
		if strings.HasPrefix(strings.ToLower(key), "vcs-") {
			pkg.VCS[key] = values[0]
		}
	}
	artifacts, err := parseSourceChecksums(fields)
	if err != nil {
		return pkg, err
	}
	pkg.Artifacts = artifacts
	return pkg, nil
}

func safeSourcePath(value string, filename bool) bool {
	if value == "" || strings.ContainsAny(value, "\\%?#") || strings.HasPrefix(value, "/") ||
		path.Clean(value) != value || value == "." || value == ".." || strings.HasPrefix(value, "../") {
		return false
	}
	return !filename || path.Base(value) == value
}

func parseSourceChecksums(fields textproto.MIMEHeader) ([]SourceArtifactMetadata, error) {
	sha256Entries, err := sourceChecksumEntries(fields.Get("Checksums-Sha256"), 32)
	if err != nil {
		return nil, fmt.Errorf("invalid Checksums-Sha256: %w", err)
	}
	if len(sha256Entries) == 0 {
		return nil, fmt.Errorf("missing Checksums-Sha256")
	}
	artifacts := make([]SourceArtifactMetadata, 0, len(sha256Entries))
	byName := make(map[string]int, len(sha256Entries))
	hasDescriptor := false
	for _, entry := range sha256Entries {
		if _, exists := byName[entry.Filename]; exists {
			return nil, fmt.Errorf("duplicate artifact %q", entry.Filename)
		}
		byName[entry.Filename] = len(artifacts)
		artifacts = append(artifacts, entry)
		hasDescriptor = hasDescriptor || strings.HasSuffix(entry.Filename, ".dsc")
	}
	if !hasDescriptor {
		return nil, fmt.Errorf("missing .dsc descriptor")
	}
	for _, field := range []struct {
		name string
		size int
	}{{"Files", 16}, {"Checksums-Sha1", 20}} {
		if fields.Get(field.name) == "" {
			continue
		}
		entries, parseErr := sourceChecksumEntries(fields.Get(field.name), field.size)
		if parseErr != nil || len(entries) != len(artifacts) {
			return nil, fmt.Errorf("invalid %s entries", field.name)
		}
		seen := make(map[string]bool, len(entries))
		for _, entry := range entries {
			index, exists := byName[entry.Filename]
			if !exists || seen[entry.Filename] || artifacts[index].Size != entry.Size {
				return nil, fmt.Errorf("inconsistent %s entry %q", field.name, entry.Filename)
			}
			seen[entry.Filename] = true
			if field.name == "Files" {
				artifacts[index].MD5 = entry.SHA256
			} else {
				artifacts[index].SHA1 = entry.SHA256
			}
		}
	}
	return artifacts, nil
}

func sourceChecksumEntries(value string, digestSize int) ([]SourceArtifactMetadata, error) {
	words := strings.Fields(value)
	if len(words)%3 != 0 {
		return nil, fmt.Errorf("checksum list is not a sequence of digest, size, and filename")
	}
	entries := make([]SourceArtifactMetadata, 0, len(words)/3)
	for index := 0; index < len(words); index += 3 {
		digest, err := hex.DecodeString(words[index])
		if err != nil || len(digest) != digestSize || !safeSourcePath(words[index+2], true) {
			return nil, fmt.Errorf("invalid checksum or filename %q", words[index+2])
		}
		size, err := strconv.ParseUint(words[index+1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid size for %q: %w", words[index+2], err)
		}
		entries = append(entries, SourceArtifactMetadata{Filename: words[index+2], Size: size, SHA256: words[index]})
	}
	return entries, nil
}
