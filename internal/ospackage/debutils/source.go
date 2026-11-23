package debutils

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/textproto"
	"path"
	"strconv"
	"strings"

	"github.com/ulikunitz/xz"
)

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
	packages, parseErr := ParseSourceIndex(reader)
	if compressed != nil {
		if closeErr := compressed.Close(); closeErr != nil && parseErr == nil {
			return nil, "", fmt.Errorf("close gzip source index: %w", closeErr)
		}
	}
	if parseErr != nil {
		return nil, "", fmt.Errorf("parse source index: %w", parseErr)
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
	fields, err := textproto.NewReader(bufio.NewReader(strings.NewReader(string(release)))).ReadMIMEHeader()
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("parse Release metadata: %w", err)
	}
	checksums := fields.Values("SHA256")
	if len(checksums) != 1 {
		return "", fmt.Errorf("Release must contain exactly one SHA256 section")
	}
	words := strings.Fields(checksums[0])
	if len(words)%3 != 0 {
		return "", fmt.Errorf("invalid Release SHA256 section")
	}
	var expected string
	var expectedSize uint64
	for position := 0; position < len(words); position += 3 {
		if words[position+2] != indexPath {
			continue
		}
		if expected != "" {
			return "", fmt.Errorf("duplicate Release entry for %q", indexPath)
		}
		digest, decodeErr := hex.DecodeString(words[position])
		if decodeErr != nil || len(digest) != sha256.Size {
			return "", fmt.Errorf("invalid Release SHA256 for %q", indexPath)
		}
		expectedSize, err = strconv.ParseUint(words[position+1], 10, 64)
		if err != nil {
			return "", fmt.Errorf("invalid Release size for %q: %w", indexPath, err)
		}
		expected = words[position]
	}
	if expected == "" {
		return "", fmt.Errorf("source index %q missing from Release SHA256", indexPath)
	}
	if uint64(len(index)) != expectedSize {
		return "", fmt.Errorf("source index %q size mismatch", indexPath)
	}
	digest := sha256.Sum256(index)
	actual := hex.EncodeToString(digest[:])
	if !strings.EqualFold(actual, expected) {
		return "", fmt.Errorf("source index %q SHA256 mismatch", indexPath)
	}
	return actual, nil
}

type SourceArtifactMetadata struct {
	Filename string
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
