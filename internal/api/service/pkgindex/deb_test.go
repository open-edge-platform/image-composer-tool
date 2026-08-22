// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package pkgindex

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// gzipped returns s as gzip bytes, for serving a Packages.gz fixture.
func gzipped(t *testing.T, s string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(s)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

const twoStanzas = `Package: ros-jazzy-desktop
Version: 0.11.0-1noble.20250612
Architecture: amd64
Description: A metapackage for the ROS 2 desktop install
 This is the long description, which the picker never shows,
 and which must not end up in Entry.Description.
Depends: ros-jazzy-ros-base,
 ros-jazzy-rviz2

Package: librealsense2
Version: 2.56.5-0~realsense.17055
Architecture: amd64
Description: Intel RealSense SDK

`

func TestParseDebIndex(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want []Entry
	}{
		{
			name: "two stanzas, continuations dropped",
			body: twoStanzas,
			want: []Entry{
				{
					Name:        "ros-jazzy-desktop",
					Version:     "0.11.0-1noble.20250612",
					Arch:        "amd64",
					Description: "A metapackage for the ROS 2 desktop install",
				},
				{
					Name:        "librealsense2",
					Version:     "2.56.5-0~realsense.17055",
					Arch:        "amd64",
					Description: "Intel RealSense SDK",
				},
			},
		},
		{
			// A file that just stops after the last field still has a stanza.
			name: "no trailing blank line",
			body: "Package: bash\nVersion: 5.2\nArchitecture: amd64",
			want: []Entry{{Name: "bash", Version: "5.2", Arch: "amd64"}},
		},
		{
			name: "empty index",
			body: "",
			want: nil,
		},
		{
			name: "blank lines only",
			body: "\n\n\n",
			want: nil,
		},
		{
			// Garbage must not abort the parse or invent a package.
			name: "line without a colon is ignored",
			body: "this is not a field\nPackage: bash\nVersion: 5.2\n",
			want: []Entry{{Name: "bash", Version: "5.2"}},
		},
		{
			// Stanzas with no Package field are not packages.
			name: "stanza without Package is dropped",
			body: "Version: 5.2\nArchitecture: amd64\n\nPackage: bash\nVersion: 1\n",
			want: []Entry{{Name: "bash", Version: "1"}},
		},
		{
			// Description-en is a different field and must not win.
			name: "translated description does not override",
			body: "Package: bash\nDescription: real synopsis\nDescription-en: translated\n",
			want: []Entry{{Name: "bash", Description: "real synopsis"}},
		},
		{
			name: "tab continuation is dropped",
			body: "Package: bash\nDescription: synopsis\n\ttabbed continuation\n",
			want: []Entry{{Name: "bash", Description: "synopsis"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseDebIndex(strings.NewReader(tc.body))
			if err != nil {
				t.Fatalf("parseDebIndex: %v", err)
			}
			assertEntries(t, got, tc.want)
		})
	}
}

func assertEntries(t *testing.T, got, want []Entry) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d:\n got %+v\nwant %+v", i, got[i], want[i])
		}
	}
}

func TestParseDebIndexRejectsOverlongLine(t *testing.T) {
	t.Parallel()
	// A line past the cap is reported rather than silently truncating the index.
	body := "Package: bash\nDepends: " + strings.Repeat("x", maxDebLine+1) + "\n"
	if _, err := parseDebIndex(strings.NewReader(body)); err == nil {
		t.Fatal("want an error for a line past maxDebLine, got nil")
	}
}

// debServer serves the given path->body map and records every request path.
func debServer(t *testing.T, files map[string][]byte) (*httptest.Server, *[]string) {
	t.Helper()
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		body, ok := files[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if _, err := w.Write(body); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &paths
}

func TestFetchDeb(t *testing.T) {
	const dir = "/dists/noble/main/binary-amd64/"
	repo := func(url string) Repo {
		return Repo{Type: TypeDeb, URL: url, Codename: "noble", Component: "main", Arch: "amd64"}
	}

	t.Run("reads Packages.gz and builds the APT path", func(t *testing.T) {
		srv, paths := debServer(t, map[string][]byte{dir + "Packages.gz": gzipped(t, twoStanzas)})
		got, err := fetchDeb(context.Background(), srv.Client(), repo(srv.URL))
		if err != nil {
			t.Fatalf("fetchDeb: %v", err)
		}
		if len(got) != 2 || got[0].Name != "ros-jazzy-desktop" {
			t.Fatalf("unexpected entries: %+v", got)
		}
		if (*paths)[0] != dir+"Packages.gz" {
			t.Errorf("first request was %q, want %q", (*paths)[0], dir+"Packages.gz")
		}
	})

	t.Run("falls through to the plain Packages", func(t *testing.T) {
		// packages.mozilla.org publishes only the uncompressed index, so the
		// probe must not stop at the two compressed names.
		srv, paths := debServer(t, map[string][]byte{dir + "Packages": []byte(twoStanzas)})
		got, err := fetchDeb(context.Background(), srv.Client(), repo(srv.URL))
		if err != nil {
			t.Fatalf("fetchDeb: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d entries, want 2", len(got))
		}
		want := []string{dir + "Packages.gz", dir + "Packages.xz", dir + "Packages"}
		if len(*paths) != 3 {
			t.Fatalf("probed %v, want %v", *paths, want)
		}
		for i := range want {
			if (*paths)[i] != want[i] {
				t.Errorf("probe %d was %q, want %q", i, (*paths)[i], want[i])
			}
		}
	})

	t.Run("all candidates missing", func(t *testing.T) {
		srv, _ := debServer(t, nil)
		_, err := fetchDeb(context.Background(), srv.Client(), repo(srv.URL))
		if err == nil {
			t.Fatal("want an error when no index exists")
		}
		// The message must name what was tried, so an operator can see whether
		// the path or the repository is wrong.
		for _, name := range debIndexNames {
			if !strings.Contains(err.Error(), name) {
				t.Errorf("error %q does not mention %q", err, name)
			}
		}
	})

	t.Run("a server error is not treated as absence", func(t *testing.T) {
		// Falling through on a 500 would report "no package index" for a
		// repository that is merely broken, so the error must propagate.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()
		_, err := fetchDeb(context.Background(), srv.Client(), repo(srv.URL))
		if err == nil {
			t.Fatal("want an error for a 500")
		}
		if errors.Is(err, errNotFound) {
			t.Errorf("a 500 was classified as not-found: %v", err)
		}
		if !strings.Contains(err.Error(), "500") {
			t.Errorf("error %q does not report the status", err)
		}
	})

	t.Run("missing codename", func(t *testing.T) {
		srv, paths := debServer(t, nil)
		r := repo(srv.URL)
		r.Codename = ""
		if _, err := fetchDeb(context.Background(), srv.Client(), r); err == nil {
			t.Fatal("want an error for a deb repo with no codename")
		}
		if len(*paths) != 0 {
			t.Errorf("requested %v; a missing codename must not reach the network", *paths)
		}
	})

	t.Run("trailing slash on the base URL", func(t *testing.T) {
		srv, paths := debServer(t, map[string][]byte{dir + "Packages.gz": gzipped(t, twoStanzas)})
		// The catalog contains both slashed and unslashed forms of the same
		// base URL, so a trailing slash must not double up in the path.
		if _, err := fetchDeb(context.Background(), srv.Client(), repo(srv.URL+"/")); err != nil {
			t.Fatalf("fetchDeb: %v", err)
		}
		if (*paths)[0] != dir+"Packages.gz" {
			t.Errorf("requested %q, want %q", (*paths)[0], dir+"Packages.gz")
		}
	})

	t.Run("corrupt gzip", func(t *testing.T) {
		srv, _ := debServer(t, map[string][]byte{dir + "Packages.gz": []byte("not gzip at all")})
		if _, err := fetchDeb(context.Background(), srv.Client(), repo(srv.URL)); err == nil {
			t.Fatal("want an error for a corrupt index")
		}
	})
}
