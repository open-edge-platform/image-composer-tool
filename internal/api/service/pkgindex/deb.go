// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package pkgindex

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
)

// debIndexNames are the index file names to try, in APT's own order of
// preference. The plain Packages is last but must be present: some
// repositories publish only that, packages.mozilla.org among them, and
// omitting it makes a working repository look unreachable.
var debIndexNames = []string{"Packages.gz", "Packages.xz", "Packages"}

// maxDebLine bounds one line of a Packages file. Real stanzas keep individual
// fields far below this; the cap only stops a malformed index from being read
// into memory without limit.
const maxDebLine = 1 << 20

// fetchDeb reads the binary index for one suite/component/arch.
//
// The probe is done here rather than through debutils.GetPackagesNames because
// that helper takes no context, so its requests could not be held to the
// caller's deadline, and it persists what it finds into the root-owned build
// cache.
func fetchDeb(ctx context.Context, client *http.Client, r Repo) ([]Entry, error) {
	if r.Codename == "" {
		return nil, fmt.Errorf("deb repository %s has no codename", r.URL)
	}
	base := strings.TrimSuffix(r.URL, "/")
	dir := fmt.Sprintf("%s/dists/%s/%s/binary-%s/", base, r.Codename, r.Component, r.Arch)

	var missing []string
	for _, name := range debIndexNames {
		entries, err := readDebIndex(ctx, client, r, base, dir+name)
		if err == nil {
			return entries, nil
		}
		if !errors.Is(err, errNotFound) {
			return nil, err
		}
		missing = append(missing, name)
	}
	return nil, fmt.Errorf("no package index under %s (tried %s)", dir, strings.Join(missing, ", "))
}

// readDebIndex fetches one candidate index and parses it. The raw
// (compressed) body is buffered rather than streamed straight into the
// decompressor, because verifyDebRelease needs those exact bytes to check
// against the checksum Release lists for this file.
func readDebIndex(ctx context.Context, client *http.Client, r Repo, base, url string) ([]Entry, error) {
	body, err := get(ctx, client, url)
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(body)
	body.Close()
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", url, err)
	}

	if r.GPGKeyPath != "" {
		if err := verifyDebRelease(ctx, client, r, base, url, raw); err != nil {
			return nil, fmt.Errorf("verify %s: %w", url, err)
		}
	}

	rd, err := decompress(url, io.NopCloser(bytes.NewReader(raw)))
	if err != nil {
		return nil, err
	}
	defer rd.Close()

	entries, err := parseDebIndex(rd)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", url, err)
	}
	return entries, nil
}

// verifyDebRelease checks that indexURL's already-fetched raw bytes match the
// checksum this suite's Release lists for it, and that Release itself carries
// a signature verifiable against r.GPGKeyPath. Any failure — a missing key
// file, a bad signature, a checksum mismatch — is returned as an error, which
// the caller treats the same as a failed fetch: it drops this one repo/index
// from the results rather than failing the whole search.
func verifyDebRelease(ctx context.Context, client *http.Client, r Repo, base, indexURL string, raw []byte) error {
	keyring, err := os.ReadFile(r.GPGKeyPath)
	if err != nil {
		return fmt.Errorf("read GPG key %s: %w", r.GPGKeyPath, err)
	}

	releaseDir := fmt.Sprintf("%s/dists/%s/", base, r.Codename)
	release, sig, err := fetchRelease(ctx, client, releaseDir)
	if err != nil {
		return err
	}
	if err := verifyRelease(release, sig, keyring); err != nil {
		return err
	}

	wantPath := fmt.Sprintf("%s/binary-%s/%s", r.Component, r.Arch, path.Base(indexURL))
	want, err := findChecksumInRelease(release, "SHA256", wantPath)
	if err != nil {
		return err
	}
	got := fmt.Sprintf("%x", sha256.Sum256(raw))
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum mismatch: Release lists %s, got %s", want, got)
	}
	return nil
}

// parseDebIndex reads RFC822 stanzas, keeping only the fields the picker
// shows, plus Depends for metapackages (see Entry.Depends). Continuation lines
// are skipped — the indented body of a Description never reaches the heap —
// except under Depends, which some repositories wrap across several lines.
func parseDebIndex(r io.Reader) ([]Entry, error) {
	var (
		entries []Entry
		cur     debStanza
		sc      = bufio.NewScanner(r)
	)
	sc.Buffer(make([]byte, 0, 64*1024), maxDebLine)

	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			// A blank line ends the stanza.
			if e, ok := cur.entry(); ok {
				entries = append(entries, e)
			}
			cur = debStanza{}
		case line[0] == ' ' || line[0] == '\t':
			// Continuation of the previous field; only Depends needs it.
			if cur.field == "Depends" {
				cur.depends += " " + strings.TrimSpace(line)
			}
		default:
			cur.apply(line)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	// A file whose last stanza is not followed by a blank line still has one.
	if e, ok := cur.entry(); ok {
		entries = append(entries, e)
	}
	return entries, nil
}

// debStanza is one stanza under construction. Section and the raw Depends are
// held here rather than on Entry because whether Depends is kept depends on
// Section, and the two may arrive in either order.
type debStanza struct {
	Entry
	section string
	depends string
	field   string // the most recent field name, for continuation lines
}

// entry finishes the stanza, reporting false for one with no Package field.
func (s *debStanza) entry() (Entry, bool) {
	if s.Name == "" {
		return Entry{}, false
	}
	e := s.Entry
	if isMetapackageSection(s.section) {
		e.Depends = parseDebDepends(s.depends)
	}
	return e, true
}

// apply assigns one "Key: value" line to the stanza under construction.
func (s *debStanza) apply(line string) {
	name, value, ok := strings.Cut(line, ":")
	if !ok {
		return
	}
	s.field = name
	value = strings.TrimSpace(value)
	switch name {
	case "Package":
		s.Name = value
	case "Version":
		s.Version = value
	case "Architecture":
		s.Arch = value
	case "Section":
		s.section = value
	case "Depends":
		s.depends = value
	case "Description":
		// Only the synopsis. A stanza may also carry Description-en, which is
		// not matched here, so the plain field always wins.
		if s.Description == "" {
			s.Description = value
		}
	}
}

// isMetapackageSection matches both the bare section and the archive-area
// prefixed form Ubuntu uses for non-main components ("universe/metapackages").
func isMetapackageSection(section string) bool {
	return section == "metapackages" || strings.HasSuffix(section, "/metapackages")
}

// parseDebDepends reduces a Depends value to package names: each comma-
// separated clause keeps its first alternative, minus any version constraint
// "(>= 1.0)", architecture qualifier ":any" or restriction "[amd64]".
func parseDebDepends(raw string) []string {
	var out []string
	for _, clause := range strings.Split(raw, ",") {
		first, _, _ := strings.Cut(clause, "|")
		name := strings.TrimSpace(first)
		if i := strings.IndexAny(name, " ([<"); i >= 0 {
			name = name[:i]
		}
		name, _, _ = strings.Cut(name, ":")
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}
