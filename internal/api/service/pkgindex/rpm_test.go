// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package pkgindex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const repomdXML = `<?xml version="1.0" encoding="UTF-8"?>
<repomd xmlns="http://linux.duke.edu/metadata/repo">
  <data type="filelists">
    <location href="repodata/filelists.xml.gz"/>
  </data>
  <data type="primary">
    <location href="repodata/abc123-primary.xml.gz"/>
  </data>
</repomd>`

const primaryXML = `<?xml version="1.0" encoding="UTF-8"?>
<metadata xmlns="http://linux.duke.edu/metadata/common"
          xmlns:rpm="http://linux.duke.edu/metadata/rpm" packages="3">
  <package type="rpm">
    <name>bash</name>
    <arch>x86_64</arch>
    <version epoch="0" ver="5.2.15" rel="8.azl3"/>
    <summary>The GNU Bourne Again shell</summary>
    <description>A much longer description the picker never shows.</description>
    <format><rpm:requires><rpm:entry name="glibc"/></rpm:requires></format>
  </package>
  <package type="rpm">
    <name>bash</name>
    <arch>src</arch>
    <version epoch="0" ver="5.2.15" rel="8.azl3"/>
    <summary>Source package, not installable</summary>
  </package>
  <package type="rpm">
    <name>vim</name>
    <arch>x86_64</arch>
    <version epoch="2" ver="9.0" rel="1"/>
    <summary>Vi IMproved</summary>
  </package>
</metadata>`

func TestPrimaryHref(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, body, want string
		wantErr          bool
	}{
		{
			// filelists comes first in the document and must not be picked.
			name: "picks primary over filelists",
			body: repomdXML,
			want: "repodata/abc123-primary.xml.gz",
		},
		{
			name:    "no primary entry",
			body:    `<repomd><data type="filelists"><location href="f.xml.gz"/></data></repomd>`,
			wantErr: true,
		},
		{
			name:    "primary with an empty href",
			body:    `<repomd><data type="primary"><location href=""/></data></repomd>`,
			wantErr: true,
		},
		{
			name:    "malformed xml",
			body:    `<repomd><data type="primary">`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if _, err := w.Write([]byte(tc.body)); err != nil {
					t.Errorf("write: %v", err)
				}
			}))
			defer srv.Close()

			got, err := primaryHref(context.Background(), srv.Client(), srv.URL+"/repodata/repomd.xml")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want an error, got href %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("primaryHref: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParsePrimary(t *testing.T) {
	t.Parallel()
	got, err := parsePrimary(strings.NewReader(primaryXML))
	if err != nil {
		t.Fatalf("parsePrimary: %v", err)
	}
	assertEntries(t, got, []Entry{
		// A zero epoch is omitted, matching how dnf prints a version.
		{Name: "bash", Version: "5.2.15-8.azl3", Arch: "x86_64", Description: "The GNU Bourne Again shell"},
		// A non-zero epoch is shown.
		{Name: "vim", Version: "2:9.0-1", Arch: "x86_64", Description: "Vi IMproved"},
	})
}

func TestRPMVersion(t *testing.T) {
	t.Parallel()
	mk := func(epoch, ver, rel string) rpmPackage {
		var p rpmPackage
		p.Version.Epoch, p.Version.Ver, p.Version.Rel = epoch, ver, rel
		return p
	}
	tests := []struct {
		name string
		pkg  rpmPackage
		want string
	}{
		{"zero epoch is omitted", mk("0", "1.2", "3"), "1.2-3"},
		{"absent epoch is omitted", mk("", "1.2", "3"), "1.2-3"},
		{"non-zero epoch is kept", mk("2", "9.0", "1"), "2:9.0-1"},
		{"no release", mk("0", "1.2", ""), "1.2"},
		{"epoch with no release", mk("1", "1.2", ""), "1:1.2"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := rpmVersion(tc.pkg); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFetchRPMSubstitutesArch(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		var body string
		switch r.URL.Path {
		case "/prod/base/aarch64/repodata/repomd.xml":
			body = repomdXML
		case "/prod/base/aarch64/repodata/abc123-primary.xml.gz":
			// Gzipped, matching the .gz href repomd advertised: decompress
			// keys off the URL suffix, so this also proves the href is
			// honoured rather than the body being read raw.
			w.WriteHeader(http.StatusOK)
			if _, err := w.Write(gzipped(t, primaryXML)); err != nil {
				t.Errorf("write: %v", err)
			}
			return
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer srv.Close()

	// Provider configs template the arch into the base URL; without the
	// substitution the request would ask for a literal "{arch}" directory.
	r := Repo{Type: TypeRPM, URL: srv.URL + "/prod/base/{arch}", Arch: "aarch64"}
	got, err := fetchRPM(context.Background(), srv.Client(), r)
	if err != nil {
		t.Fatalf("fetchRPM: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(got), got)
	}
	for _, p := range paths {
		if strings.Contains(p, "{arch}") {
			t.Errorf("request %q still carries the arch placeholder", p)
		}
	}
	if len(paths) != 2 {
		t.Errorf("made %d requests (%v), want 2", len(paths), paths)
	}
}

func TestFetchRPMMissingRepomd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	r := Repo{Type: TypeRPM, URL: srv.URL + "/base", Arch: "x86_64"}
	if _, err := fetchRPM(context.Background(), srv.Client(), r); err == nil {
		t.Fatal("want an error when repomd.xml is absent")
	}
}
