package debutils

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ulikunitz/xz"
)

func TestVerifySourceIndex(t *testing.T) {
	t.Parallel()

	index := []byte("source index contents")
	digest := fmt.Sprintf("%x", sha256.Sum256(index))
	indexPath := "main/source/Sources.xz"
	entry := fmt.Sprintf(" %s %d %s\n", digest, len(index), indexPath)
	other := fmt.Sprintf(" %s 1 universe/source/Sources.xz\n", digest)
	tests := []struct {
		name, release, path string
		index               []byte
		wantErr             bool
	}{
		{"valid", "Suite: test\nSHA256:\n" + other + entry, indexPath, index, false},
		{"uppercase digest", "SHA256:\n " + strings.ToUpper(digest) + fmt.Sprintf(" %d %s\n", len(index), indexPath), indexPath, index, false},
		{"digest mismatch", "SHA256:\n" + entry, indexPath, []byte(strings.Repeat("x", len(index))), true},
		{"size mismatch", "SHA256:\n" + entry, indexPath, []byte("short"), true},
		{"wrong component", "SHA256:\n" + entry, "universe/source/Sources.xz", index, true},
		{"duplicate", "SHA256:\n" + entry + entry, indexPath, index, true},
		{"missing", "SHA256:\n" + other, indexPath, index, true},
		{"traversal", "SHA256:\n" + entry, "../main/source/Sources.xz", index, true},
		{"wrong file", "SHA256:\n" + entry, "main/source/Packages.gz", index, true},
		{"bad digest", "SHA256:\n invalid 21 " + indexPath + "\n", indexPath, index, true},
		{"bad size", "SHA256:\n " + digest + " -1 " + indexPath + "\n", indexPath, index, true},
		{"duplicate section", "SHA256:\n" + entry + "SHA256:\n" + entry, indexPath, index, true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := verifySourceIndex([]byte(tt.release), tt.index, tt.path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("verifySourceIndex error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got != digest {
				t.Errorf("verifySourceIndex digest = %q, want %q", got, digest)
			}
			if tt.name == "digest mismatch" && !strings.Contains(err.Error(), "SHA256 mismatch") {
				t.Errorf("digest mismatch error = %v", err)
			}
			if tt.name == "size mismatch" && !strings.Contains(err.Error(), "size mismatch") {
				t.Errorf("size mismatch error = %v", err)
			}
		})
	}
}

func TestParseSourceIndexFromReleaseRejectsInvalidCompression(t *testing.T) {
	t.Parallel()

	index := []byte("not compressed")
	digest := fmt.Sprintf("%x", sha256.Sum256(index))
	for _, suffix := range []string{".gz", ".xz"} {
		suffix := suffix
		t.Run(suffix, func(t *testing.T) {
			t.Parallel()
			indexPath := "main/source/Sources" + suffix
			release := fmt.Sprintf("SHA256:\n %s %d %s\n", digest, len(index), indexPath)
			packages, gotDigest, err := parseSourceIndexFromRelease([]byte(release), index, indexPath)
			if err == nil || packages != nil || gotDigest != "" {
				t.Fatalf("got packages %v digest %q error %v, want decompression error", packages, gotDigest, err)
			}
		})
	}
}

func TestParseSourceIndexFromSignedRelease(t *testing.T) {
	t.Parallel()

	signer, err := openpgp.NewEntity("Signer", "test", "signer@example.invalid", nil)
	if err != nil {
		t.Fatalf("create signer: %v", err)
	}
	var publicKey bytes.Buffer
	if err := signer.Serialize(&publicKey); err != nil {
		t.Fatalf("serialize key: %v", err)
	}
	root := t.TempDir()
	keyPath := filepath.Join(root, "signer.gpg")
	if err := os.WriteFile(keyPath, publicKey.Bytes(), 0o644); err != nil {
		t.Fatalf("write key: %v", err)
	}
	checksum := strings.Repeat("a", 64)
	stanza := "Package: mesa\nVersion: 2:26.0-1\nDirectory: pool/main/m/mesa\nChecksums-Sha256:\n " +
		checksum + " 10 mesa.dsc\n " + checksum + " 20 mesa.orig.tar.xz\n " + checksum + " 30 mesa.extra.tar.gz\n"
	for _, format := range []string{"plain", "gz", "xz"} {
		format := format
		t.Run(format, func(t *testing.T) {
			var content bytes.Buffer
			indexPath := "main/source/Sources"
			switch format {
			case "gz":
				writer := gzip.NewWriter(&content)
				if _, err := writer.Write([]byte(stanza)); err != nil {
					t.Fatalf("write gzip: %v", err)
				}
				if err := writer.Close(); err != nil {
					t.Fatalf("close gzip: %v", err)
				}
				indexPath += ".gz"
			case "xz":
				writer, err := xz.NewWriter(&content)
				if err != nil {
					t.Fatalf("create xz: %v", err)
				}
				if _, err := writer.Write([]byte(stanza)); err != nil {
					t.Fatalf("write xz: %v", err)
				}
				if err := writer.Close(); err != nil {
					t.Fatalf("close xz: %v", err)
				}
				indexPath += ".xz"
			default:
				content.WriteString(stanza)
			}
			digest := fmt.Sprintf("%x", sha256.Sum256(content.Bytes()))
			release := fmt.Sprintf("Suite: test\nSHA256:\n %s %d %s\n", digest, content.Len(), indexPath)
			inRelease := filepath.Join(root, "InRelease."+format)
			clearsignInRelease(t, inRelease, signer, []byte(release))

			verifiedRelease, err := VerifyInRelease(inRelease, keyPath)
			if err != nil {
				t.Fatalf("verify InRelease: %v", err)
			}
			packages, gotDigest, err := parseSourceIndexFromRelease(verifiedRelease, content.Bytes(), indexPath)
			if err != nil {
				t.Fatalf("parse verified index: %v", err)
			}
			if gotDigest != digest || len(packages) != 1 || packages[0].Name != "mesa" ||
				len(packages[0].Artifacts) != 3 || packages[0].Artifacts[2].Size != 30 {
				t.Fatalf("unexpected digest %q or packages %+v", gotDigest, packages)
			}
		})
	}
}

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
