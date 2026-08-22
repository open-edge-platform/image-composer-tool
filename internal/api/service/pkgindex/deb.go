// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package pkgindex

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
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
	dir := fmt.Sprintf("%s/dists/%s/%s/binary-%s/",
		strings.TrimSuffix(r.URL, "/"), r.Codename, r.Component, r.Arch)

	var missing []string
	for _, name := range debIndexNames {
		entries, err := readDebIndex(ctx, client, dir+name)
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

// readDebIndex fetches one candidate index and parses it as it streams.
func readDebIndex(ctx context.Context, client *http.Client, url string) ([]Entry, error) {
	body, err := get(ctx, client, url)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	rd, err := decompress(url, body)
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

// parseDebIndex reads RFC822 stanzas, keeping only the four fields the picker
// shows. Continuation lines — the indented body of a Description, and the
// multi-line Depends some repositories emit — are skipped, so the long
// description never reaches the heap.
func parseDebIndex(r io.Reader) ([]Entry, error) {
	var (
		entries []Entry
		cur     Entry
		sc      = bufio.NewScanner(r)
	)
	sc.Buffer(make([]byte, 0, 64*1024), maxDebLine)

	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			// A blank line ends the stanza.
			if cur.Name != "" {
				entries = append(entries, cur)
			}
			cur = Entry{}
		case line[0] == ' ' || line[0] == '\t':
			// Continuation of the previous field; nothing here needs it.
		default:
			applyDebField(&cur, line)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	// A file whose last stanza is not followed by a blank line still has one.
	if cur.Name != "" {
		entries = append(entries, cur)
	}
	return entries, nil
}

// applyDebField assigns one "Key: value" line to the stanza under construction.
func applyDebField(e *Entry, line string) {
	name, value, ok := strings.Cut(line, ":")
	if !ok {
		return
	}
	value = strings.TrimSpace(value)
	switch name {
	case "Package":
		e.Name = value
	case "Version":
		e.Version = value
	case "Architecture":
		e.Arch = value
	case "Description":
		// Only the synopsis. A stanza may also carry Description-en, which is
		// not matched here, so the plain field always wins.
		if e.Description == "" {
			e.Description = value
		}
	}
}
