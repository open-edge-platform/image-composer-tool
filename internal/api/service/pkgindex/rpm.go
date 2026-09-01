// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package pkgindex

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// repomd is the subset of repodata/repomd.xml needed to locate the primary
// metadata. Field names carry no namespace, so they match whichever one the
// repository declares.
type repomd struct {
	Data []struct {
		Type     string `xml:"type,attr"`
		Location struct {
			Href string `xml:"href,attr"`
		} `xml:"location"`
	} `xml:"data"`
}

// rpmPackage is the subset of a primary.xml <package> the picker shows.
type rpmPackage struct {
	Name    string `xml:"name"`
	Arch    string `xml:"arch"`
	Summary string `xml:"summary"`
	Version struct {
		Epoch string `xml:"epoch,attr"`
		Ver   string `xml:"ver,attr"`
		Rel   string `xml:"rel,attr"`
	} `xml:"version"`
}

// fetchRPM reads a dnf repository's primary metadata.
//
// The build path's rpmutils.FetchPrimaryURL is not used: it takes its deadline
// from the process-wide runctx.Context() instead of the caller's, and writes a
// timestamped copy of every repomd.xml it sees into the build cache.
func fetchRPM(ctx context.Context, client *http.Client, r Repo) ([]Entry, error) {
	// Provider configs template the architecture into the base URL, e.g.
	// https://packages.microsoft.com/azurelinux/3.0/prod/base/{arch}.
	base := strings.TrimSuffix(strings.ReplaceAll(r.URL, "{arch}", r.Arch), "/")

	href, err := primaryHref(ctx, client, base+"/repodata/repomd.xml")
	if err != nil {
		return nil, err
	}
	primaryURL := base + "/" + strings.TrimPrefix(href, "/")

	body, err := get(ctx, client, primaryURL)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	rd, err := decompress(primaryURL, body)
	if err != nil {
		return nil, err
	}
	defer rd.Close()

	entries, err := parsePrimary(rd)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", primaryURL, err)
	}
	return entries, nil
}

// primaryHref returns the location of the primary metadata named by repomd.xml.
func primaryHref(ctx context.Context, client *http.Client, repomdURL string) (string, error) {
	body, err := get(ctx, client, repomdURL)
	if err != nil {
		return "", err
	}
	defer body.Close()

	var md repomd
	if err := xml.NewDecoder(body).Decode(&md); err != nil {
		return "", fmt.Errorf("parse %s: %w", repomdURL, err)
	}
	for _, d := range md.Data {
		if d.Type == "primary" && d.Location.Href != "" {
			return d.Location.Href, nil
		}
	}
	return "", fmt.Errorf("%s names no primary metadata", repomdURL)
}

// parsePrimary streams primary.xml, decoding one <package> at a time so a
// repository with tens of thousands of packages never holds the whole document.
func parsePrimary(r io.Reader) ([]Entry, error) {
	dec := xml.NewDecoder(r)
	var entries []Entry
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return entries, nil
		}
		if err != nil {
			return nil, err
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "package" {
			continue
		}
		var p rpmPackage
		if err := dec.DecodeElement(&p, &start); err != nil {
			return nil, err
		}
		// Source packages are not installable, so they would only be noise in
		// a picker. The build path's parser drops them for the same reason.
		if p.Name == "" || p.Arch == "src" || p.Arch == "nosrc" {
			continue
		}
		entries = append(entries, Entry{
			Name:        p.Name,
			Version:     rpmVersion(p),
			Description: p.Summary,
			Arch:        p.Arch,
		})
	}
}

// rpmVersion renders the epoch:version-release form dnf displays, omitting a
// zero or absent epoch the way dnf itself does.
func rpmVersion(p rpmPackage) string {
	v := p.Version.Ver
	if p.Version.Rel != "" {
		v += "-" + p.Version.Rel
	}
	if e := p.Version.Epoch; e != "" && e != "0" {
		v = e + ":" + v
	}
	return v
}
