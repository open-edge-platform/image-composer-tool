package debutils

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"io"
	"net/textproto"
	"path"
	"strconv"
	"strings"
)

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
