package debutils

import (
	"errors"
	"strings"
	"testing"
)

func TestParseSourceIndex(t *testing.T) {
	t.Parallel()

	checksum := strings.Repeat("a", 64)
	index := "Package: mesa\nVersion: 2:26.0-1\nDirectory: pool/main/m/mesa\nArchitecture: any all\n" +
		"Vcs-Git: https://example.org/mesa.git\nChecksums-Sha256:\n " + checksum + " 10 mesa.dsc\n " +
		checksum + " 20 mesa.orig.tar.xz\n " + checksum + " 30 mesa.extra.tar.gz\n\n" +
		"Source: other\nVersion: 1.0\nDirectory: pool/main/o/other\nChecksums-Sha256:\n " + checksum + " 1 other.dsc"

	packages, err := ParseSourceIndex(strings.NewReader(index))
	if err != nil {
		t.Fatalf("ParseSourceIndex: %v", err)
	}
	if len(packages) != 2 || packages[0].Name != "mesa" || packages[0].Version != "2:26.0-1" ||
		len(packages[0].Artifacts) != 3 || packages[0].Artifacts[2].Size != 30 ||
		packages[0].Artifacts[2].SHA256 != checksum || packages[0].VCS["Vcs-Git"] == "" ||
		packages[1].Name != "other" {
		t.Fatalf("unexpected source packages: %+v", packages)
	}
}

func TestParseSourceIndexRejectsInvalidEntries(t *testing.T) {
	t.Parallel()

	checksum := strings.Repeat("a", 64)
	tests := []struct{ name, directory, checksums string }{
		{"traversal", "pool/../outside", checksum + " 1 pkg.dsc"},
		{"absolute directory", "/pool", checksum + " 1 pkg.dsc"},
		{"encoded traversal", "pool/%2e%2e", checksum + " 1 pkg.dsc"},
		{"unsafe filename", "pool/main", checksum + " 1 ../pkg.dsc"},
		{"missing descriptor", "pool/main", checksum + " 1 pkg.tar.xz"},
		{"missing sha256", "pool/main", ""},
		{"invalid digest", "pool/main", "abcd 1 pkg.dsc"},
		{"invalid size", "pool/main", checksum + " -1 pkg.dsc"},
		{"duplicate file", "pool/main", checksum + " 1 pkg.dsc " + checksum + " 1 pkg.dsc"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			index := "Package: pkg\nVersion: 1\nDirectory: " + tt.directory + "\nChecksums-Sha256: " + tt.checksums + "\n"
			_, err := ParseSourceIndex(strings.NewReader(index))
			var indexErr *SourceIndexError
			if !errors.As(err, &indexErr) || indexErr.Stanza != 1 {
				t.Fatalf("ParseSourceIndex error = %v, want stanza 1 error", err)
			}
		})
	}
}

func TestParseSourceIndexChecksumLists(t *testing.T) {
	t.Parallel()

	sha256 := strings.Repeat("a", 64)
	sha1 := strings.Repeat("b", 40)
	md5 := strings.Repeat("c", 32)
	base := "Package: pkg\nVersion: 1\nDirectory: pool/main/p/pkg\n" +
		"Checksums-Sha256:\n " + sha256 + " 10 pkg.dsc\n " + sha256 + " 20 pkg.tar.xz\n"
	valid := base + "Files:\n " + md5 + " 20 pkg.tar.xz\n " + md5 + " 10 pkg.dsc\n" +
		"Checksums-Sha1:\n " + sha1 + " 10 pkg.dsc\n " + sha1 + " 20 pkg.tar.xz\n"

	packages, err := ParseSourceIndex(strings.NewReader(valid))
	if err != nil {
		t.Fatalf("ParseSourceIndex: %v", err)
	}
	for _, artifact := range packages[0].Artifacts {
		if artifact.MD5 != md5 || artifact.SHA1 != sha1 || artifact.SHA256 != sha256 {
			t.Errorf("unexpected checksums: %+v", artifact)
		}
	}

	tests := []struct{ name, fields string }{
		{"missing file", "Files: " + md5 + " 10 pkg.dsc\n"},
		{"different size", "Files: " + md5 + " 11 pkg.dsc " + md5 + " 20 pkg.tar.xz\n"},
		{"duplicate file", "Files: " + md5 + " 10 pkg.dsc " + md5 + " 10 pkg.dsc\n"},
		{"unknown file", "Files: " + md5 + " 10 pkg.dsc " + md5 + " 20 other.tar.xz\n"},
		{"duplicate field", "Version: 2\n"},
		{"conflicting source", "Source: other\n"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseSourceIndex(strings.NewReader(base + tt.fields))
			var indexErr *SourceIndexError
			if !errors.As(err, &indexErr) {
				t.Fatalf("ParseSourceIndex error = %v, want source index error", err)
			}
		})
	}
}
