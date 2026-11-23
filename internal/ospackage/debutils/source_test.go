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
	"github.com/ProtonMail/go-crypto/openpgp/clearsign"
	"github.com/ulikunitz/xz"
)

func TestVerifySourceRelease(t *testing.T) {
	t.Parallel()

	signer, err := openpgp.NewEntity("Signer", "test", "signer@example.invalid", nil)
	if err != nil {
		t.Fatalf("create signer: %v", err)
	}
	other, err := openpgp.NewEntity("Other", "test", "other@example.invalid", nil)
	if err != nil {
		t.Fatalf("create other signer: %v", err)
	}
	var key, wrongKey, signature, signed bytes.Buffer
	if err := signer.Serialize(&key); err != nil {
		t.Fatalf("serialize key: %v", err)
	}
	if err := other.Serialize(&wrongKey); err != nil {
		t.Fatalf("serialize other key: %v", err)
	}
	release := []byte("Suite: test\nSHA256:\n")
	if err := openpgp.DetachSign(&signature, signer, bytes.NewReader(release), nil); err != nil {
		t.Fatalf("sign Release: %v", err)
	}
	writer, err := clearsign.Encode(&signed, signer.PrivateKey, nil)
	if err != nil {
		t.Fatalf("create InRelease: %v", err)
	}
	if _, err := writer.Write(release); err != nil {
		t.Fatalf("write InRelease: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close InRelease: %v", err)
	}
	tests := []struct {
		name                    string
		release, signature, key []byte
		inRelease, wantErr      bool
	}{
		{"detached", release, signature.Bytes(), key.Bytes(), false, false},
		{"inrelease", signed.Bytes(), nil, key.Bytes(), true, false},
		{"wrong detached key", release, signature.Bytes(), wrongKey.Bytes(), false, true},
		{"wrong inrelease key", signed.Bytes(), nil, wrongKey.Bytes(), true, true},
		{"missing key", signed.Bytes(), nil, nil, true, true},
		{"invalid signature", release, []byte("invalid"), key.Bytes(), false, true},
		{"invalid inrelease", release, nil, key.Bytes(), true, true},
		{"trailing data", append(bytes.Clone(signed.Bytes()), []byte("unverified")...), nil, key.Bytes(), true, true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := verifySourceRelease(tt.release, tt.signature, tt.key, tt.inRelease)
			if tt.wantErr {
				if !errors.Is(err, errSourceVerification) || got != nil {
					t.Fatalf("verifySourceRelease = %q, %v; want verification error", got, err)
				}
			} else if err != nil || !bytes.Equal(got, release) {
				t.Fatalf("verifySourceRelease = %q, %v; want %q", got, err, release)
			}
		})
	}
}

func TestFindSourcePackage(t *testing.T) {
	t.Parallel()

	packages := []SourcePackageMetadata{
		{Name: "mesa", Version: "2:26.0-1", Directory: "pool/first"},
		{Name: "mesa", Version: "2:26.0-2", Directory: "pool/second"},
		{Name: "other", Version: "2:26.0-1", Directory: "pool/other"},
	}
	got, err := findSourcePackage(packages, "mesa", "2:26.0-1")
	if err != nil || got == nil || got.Directory != "pool/first" {
		t.Fatalf("exact match = %+v, %v", got, err)
	}
	if _, err := findSourcePackage(packages, "mesa", "26.0-1"); !errors.Is(err, errSourceVersionMissing) {
		t.Errorf("missing epoch error = %v", err)
	}
	packages = append(packages, SourcePackageMetadata{Name: "mesa", Version: "2:26.0-1"})
	if got, err := findSourcePackage(packages, "mesa", "2:26.0-1"); err == nil || got != nil {
		t.Errorf("duplicate match = %+v, %v", got, err)
	}
}

func TestDiscoverSourceIndexes(t *testing.T) {
	t.Parallel()

	digest := strings.Repeat("a", 64)
	release := "Suite: test\nSHA256:\n" +
		" " + digest + " 10 main/source/Sources\n" +
		" " + digest + " 20 main/source/Sources.gz\n" +
		" " + strings.ToUpper(digest) + " 30 main/source/Sources.xz\n" +
		" " + digest + " 40 universe/source/Sources.xz\n"

	indexes, err := discoverSourceIndexes([]byte(release), "main")
	if err != nil {
		t.Fatalf("discoverSourceIndexes: %v", err)
	}
	want := []sourceIndexMetadata{
		{Path: "main/source/Sources.xz", SHA256: digest, Size: 30},
		{Path: "main/source/Sources.gz", SHA256: digest, Size: 20},
		{Path: "main/source/Sources", SHA256: digest, Size: 10},
	}
	if len(indexes) != len(want) {
		t.Fatalf("got indexes %+v, want %+v", indexes, want)
	}
	for index := range want {
		if indexes[index] != want[index] {
			t.Errorf("index %d = %+v, want %+v", index, indexes[index], want[index])
		}
	}
}

func TestDiscoverSourceIndexesRejectsInvalidMetadata(t *testing.T) {
	t.Parallel()

	digest := strings.Repeat("a", 64)
	entry := " " + digest + " 10 main/source/Sources.gz\n"
	tests := []struct {
		name, release, component string
		missing                  bool
	}{
		{"missing index", "SHA256:\n" + entry, "universe", true},
		{"missing section", "Suite: test\n", "main", false},
		{"duplicate section", "SHA256:\n" + entry + "SHA256:\n" + entry, "main", false},
		{"duplicate entry", "SHA256:\n" + entry + entry, "main", false},
		{"invalid digest", "SHA256:\n invalid 10 main/source/Sources.gz\n", "main", false},
		{"invalid size", "SHA256:\n " + digest + " -1 main/source/Sources.gz\n", "main", false},
		{"malformed entries", "SHA256:\n " + digest + " 10\n", "main", false},
		{"traversal", "SHA256:\n" + entry, "../main", false},
		{"encoded component", "SHA256:\n" + entry, "%2e%2e", false},
		{"absolute component", "SHA256:\n" + entry, "/main", false},
		{"query component", "SHA256:\n" + entry, "main?secret", false},
		{"control component", "SHA256:\n" + entry, "main\x00extra", false},
		{"unicode space component", "SHA256:\n" + entry, "main\u00a0extra", false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			indexes, err := discoverSourceIndexes([]byte(tt.release), tt.component)
			if err == nil || indexes != nil || errors.Is(err, errSourceIndexMissing) != tt.missing {
				t.Fatalf("discoverSourceIndexes = %v, %v; want missing=%v", indexes, err, tt.missing)
			}
		})
	}
}

func TestSourceArtifactsWithURLs(t *testing.T) {
	t.Parallel()

	pkg := SourcePackageMetadata{
		Directory: "pool/main/m/mesa",
		Artifacts: []SourceArtifactMetadata{
			{Filename: "mesa.dsc", Size: 10, SHA256: "descriptor"},
			{Filename: "mesa.orig.tar.xz", Size: 20, SHA256: "archive"},
			{Filename: "mesa.extra+vendor.tar.gz", Size: 30, SHA256: "extra"},
		},
	}

	artifacts, err := sourceArtifactsWithURLs("https://user:secret@example.org/snapshot/", pkg)
	if err != nil {
		t.Fatalf("sourceArtifactsWithURLs: %v", err)
	}
	want := []string{
		"https://example.org/snapshot/pool/main/m/mesa/mesa.dsc",
		"https://example.org/snapshot/pool/main/m/mesa/mesa.orig.tar.xz",
		"https://example.org/snapshot/pool/main/m/mesa/mesa.extra+vendor.tar.gz",
	}
	if len(artifacts) != len(want) {
		t.Fatalf("got %d artifacts, want %d", len(artifacts), len(want))
	}
	for index, artifact := range artifacts {
		if artifact.URL != want[index] || artifact.Size != pkg.Artifacts[index].Size ||
			artifact.SHA256 != pkg.Artifacts[index].SHA256 || pkg.Artifacts[index].URL != "" {
			t.Errorf("artifact %d = %+v, want URL %q and original metadata", index, artifact, want[index])
		}
	}
}

func TestSourceArtifactsWithURLsRejectsUnsafeInput(t *testing.T) {
	t.Parallel()

	tests := []struct{ name, base, directory, filename string }{
		{"query", "https://example.org/repo?token=secret", "pool/main", "pkg.dsc"},
		{"empty query", "https://example.org/repo?", "pool/main", "pkg.dsc"},
		{"fragment", "https://example.org/repo#secret", "pool/main", "pkg.dsc"},
		{"unsupported scheme", "file:///repo", "pool/main", "pkg.dsc"},
		{"missing host", "https:///repo", "pool/main", "pkg.dsc"},
		{"encoded base traversal", "https://example.org/%2e%2e/repo", "pool/main", "pkg.dsc"},
		{"base traversal", "https://example.org/a/../repo", "pool/main", "pkg.dsc"},
		{"directory traversal", "https://example.org/repo", "../outside", "pkg.dsc"},
		{"absolute directory", "https://example.org/repo", "/outside", "pkg.dsc"},
		{"encoded directory", "https://example.org/repo", "pool/%2e%2e", "pkg.dsc"},
		{"filename traversal", "https://example.org/repo", "pool/main", "../pkg.dsc"},
		{"filename query", "https://example.org/repo", "pool/main", "pkg.dsc?token=secret"},
		{"absolute filename", "https://example.org/repo", "pool/main", "//other.example/pkg.dsc"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pkg := SourcePackageMetadata{Directory: tt.directory, Artifacts: []SourceArtifactMetadata{{Filename: tt.filename}}}
			artifacts, err := sourceArtifactsWithURLs(tt.base, pkg)
			if err == nil || artifacts != nil {
				t.Fatalf("sourceArtifactsWithURLs = %v, %v; want error", artifacts, err)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatalf("error contains sensitive input: %v", err)
			}
		})
	}
}

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
			indexes, err := discoverSourceIndexes(verifiedRelease, "main")
			if err != nil || len(indexes) != 1 || indexes[0].Path != indexPath ||
				indexes[0].SHA256 != digest || indexes[0].Size != uint64(content.Len()) {
				t.Fatalf("discovered indexes %+v, error %v", indexes, err)
			}
			packages, gotDigest, err := parseSourceIndexFromRelease(verifiedRelease, content.Bytes(), indexPath)
			if err != nil {
				t.Fatalf("parse verified index: %v", err)
			}
			if gotDigest != digest || len(packages) != 1 || packages[0].Name != "mesa" ||
				len(packages[0].Artifacts) != 3 || packages[0].Artifacts[2].Size != 30 {
				t.Fatalf("unexpected digest %q or packages %+v", gotDigest, packages)
			}
			artifacts, err := sourceArtifactsWithURLs("https://example.org/snapshot/", packages[0])
			if err != nil {
				t.Fatalf("resolve source artifacts: %v", err)
			}
			wantNames := []string{"mesa.dsc", "mesa.orig.tar.xz", "mesa.extra.tar.gz"}
			for index, artifact := range artifacts {
				wantURL := "https://example.org/snapshot/pool/main/m/mesa/" + wantNames[index]
				if artifact.URL != wantURL || artifact.Size != uint64((index+1)*10) || artifact.SHA256 != checksum {
					t.Errorf("artifact %d = %+v, want URL %q", index, artifact, wantURL)
				}
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
