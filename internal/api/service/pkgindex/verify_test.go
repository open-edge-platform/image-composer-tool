// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package pkgindex

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
)

// testKeyPair generates a fresh OpenPGP entity for signing fixtures, and
// returns its ASCII-armored public key in the form a catalog's GPGKeyPath
// would point at on disk.
func testKeyPair(t *testing.T) (*openpgp.Entity, []byte) {
	t.Helper()
	entity, err := openpgp.NewEntity("Test Repo Signing Key", "", "test@example.invalid", nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	if err != nil {
		t.Fatalf("armor encode: %v", err)
	}
	if err := entity.Serialize(w); err != nil {
		t.Fatalf("serialize public key: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close armor writer: %v", err)
	}
	return entity, buf.Bytes()
}

func armoredDetachSign(t *testing.T, entity *openpgp.Entity, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := openpgp.ArmoredDetachSign(&buf, entity, bytes.NewReader(data), nil); err != nil {
		t.Fatalf("sign: %v", err)
	}
	return buf.Bytes()
}

// binaryDetachSign produces the non-armored detached signature form apt
// historically serves as Release.gpg, distinct from InRelease's armored form.
func binaryDetachSign(t *testing.T, entity *openpgp.Entity, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := openpgp.DetachSign(&buf, entity, bytes.NewReader(data), nil); err != nil {
		t.Fatalf("sign: %v", err)
	}
	return buf.Bytes()
}

// tamperArmoredSignature flips one base64 character in the armored body of
// sig, leaving the armor structure (headers, line breaks, CRC line) intact so
// the corruption is in the signature data itself rather than in something
// armor.Decode would reject outright.
//
// Operates on a copy: bytes.Split's slices alias the input's backing array, so
// flipping a byte through one of them would corrupt the caller's own signature
// — which the parallel subtests share, making the valid-signature case fail
// depending on which subtest happened to run first.
func tamperArmoredSignature(t *testing.T, sig []byte) []byte {
	t.Helper()
	lines := bytes.Split(append([]byte(nil), sig...), []byte("\n"))
	for i, line := range lines {
		if len(line) == 0 || line[0] == '-' || line[0] == '=' {
			continue
		}
		if line[0] == 'A' {
			line[0] = 'B'
		} else {
			line[0] = 'A'
		}
		lines[i] = line
		return bytes.Join(lines, []byte("\n"))
	}
	t.Fatal("no armored body line found to tamper")
	return nil
}

// writeKeyFile writes keyring under t.TempDir and returns its path, standing
// in for the catalog's GPGKeyPath.
func writeKeyFile(t *testing.T, keyring []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "keyring.gpg")
	if err := os.WriteFile(p, keyring, 0o600); err != nil {
		t.Fatalf("write keyring: %v", err)
	}
	return p
}

func TestVerifyRelease(t *testing.T) {
	t.Parallel()
	entity, pubKey := testKeyPair(t)
	release := []byte("Origin: Test\nSuite: noble\n")
	sig := armoredDetachSign(t, entity, release)

	t.Run("valid signature passes", func(t *testing.T) {
		t.Parallel()
		if err := verifyRelease(release, sig, pubKey); err != nil {
			t.Fatalf("verifyRelease: %v", err)
		}
	})

	t.Run("tampered Release fails closed", func(t *testing.T) {
		t.Parallel()
		tampered := append([]byte{}, release...)
		tampered[0] = 'X'
		if err := verifyRelease(tampered, sig, pubKey); err == nil {
			t.Fatal("want an error for a Release that does not match its signature")
		}
	})

	t.Run("tampered signature fails closed", func(t *testing.T) {
		t.Parallel()
		tampered := tamperArmoredSignature(t, sig)
		if err := verifyRelease(release, tampered, pubKey); err == nil {
			t.Fatal("want an error for a corrupted signature")
		}
	})

	t.Run("wrong key fails closed", func(t *testing.T) {
		t.Parallel()
		_, otherKey := testKeyPair(t)
		if err := verifyRelease(release, sig, otherKey); err == nil {
			t.Fatal("want an error when the signature was not made by the configured key")
		}
	})

	t.Run("garbage keyring is an error, not a pass", func(t *testing.T) {
		t.Parallel()
		if err := verifyRelease(release, sig, []byte("not a key at all")); err == nil {
			t.Fatal("want an error for an unparseable keyring")
		}
	})

	t.Run("binary signature from an unrecognised key fails closed", func(t *testing.T) {
		t.Parallel()
		// Release.gpg is historically a binary (not armored) detached signature,
		// which is the form that reaches openpgp.CheckDetachedSignature rather than
		// CheckArmoredDetachedSignature. A signer absent from the keyring must be
		// rejected, not accepted with a warning: the keyring is the trust anchor
		// for the repo.
		_, otherKey := testKeyPair(t)
		binSig := binaryDetachSign(t, entity, release)
		if err := verifyRelease(release, binSig, otherKey); err == nil {
			t.Fatal("want an error for a binary signature made by a key absent from the keyring")
		}
	})
}

func TestFindChecksumInRelease(t *testing.T) {
	t.Parallel()
	release := []byte(`Origin: Test
Suite: noble
MD5Sum:
 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa 1234 main/binary-amd64/Packages.gz
SHA256:
 bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb 1234 main/binary-amd64/Packages.gz
 cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc 5678 main/binary-arm64/Packages.gz
`)

	tests := []struct {
		name string
		path string
		want string
	}{
		{"amd64 entry", "main/binary-amd64/Packages.gz", strings.Repeat("b", 64)},
		{"arm64 entry", "main/binary-arm64/Packages.gz", strings.Repeat("c", 64)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := findChecksumInRelease(release, "SHA256", tc.path)
			if err != nil {
				t.Fatalf("findChecksumInRelease: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}

	t.Run("path not listed", func(t *testing.T) {
		t.Parallel()
		if _, err := findChecksumInRelease(release, "SHA256", "main/binary-riscv64/Packages.gz"); err == nil {
			t.Fatal("want an error for a path absent from the section")
		}
	})

	t.Run("checksum type not present", func(t *testing.T) {
		t.Parallel()
		if _, err := findChecksumInRelease(release, "SHA512", "main/binary-amd64/Packages.gz"); err == nil {
			t.Fatal("want an error for a checksum section that does not exist")
		}
	})

	t.Run("MD5Sum entry is not returned for a SHA256 request", func(t *testing.T) {
		t.Parallel()
		got, err := findChecksumInRelease(release, "SHA256", "main/binary-amd64/Packages.gz")
		if err != nil {
			t.Fatalf("findChecksumInRelease: %v", err)
		}
		if got == strings.Repeat("a", 32) {
			t.Errorf("returned the MD5Sum entry instead of SHA256's")
		}
	})
}

// debReleaseFixture builds a signed Release + Release.gpg pair covering a
// single Packages.gz whose checksum matches pkggz, plus the key file that
// verifies it.
func debReleaseFixture(t *testing.T, component, arch string, pkggz []byte) (release, sig, pubKey []byte) {
	t.Helper()
	sum := fmt.Sprintf("%x", sha256.Sum256(pkggz))
	release = []byte(fmt.Sprintf("Origin: Test\nSuite: noble\nSHA256:\n %s %d %s/binary-%s/Packages.gz\n",
		sum, len(pkggz), component, arch))
	entity, pubKey := testKeyPair(t)
	sig = armoredDetachSign(t, entity, release)
	return release, sig, pubKey
}

func TestFetchDebWithGPGVerification(t *testing.T) {
	const dir = "/dists/noble/main/binary-amd64/"
	pkggz := gzipped(t, twoStanzas)

	repo := func(url, keyPath string) Repo {
		return Repo{
			Type: TypeDeb, URL: url, Codename: "noble", Component: "main", Arch: "amd64",
			GPGKeyPath: keyPath,
		}
	}

	t.Run("valid signature and checksum pass", func(t *testing.T) {
		release, sig, pubKey := debReleaseFixture(t, "main", "amd64", pkggz)
		srv, _ := debServer(t, map[string][]byte{
			dir + "Packages.gz":        pkggz,
			"/dists/noble/Release":     release,
			"/dists/noble/Release.gpg": sig,
		})
		got, err := fetchDeb(context.Background(), srv.Client(), repo(srv.URL, writeKeyFile(t, pubKey)))
		if err != nil {
			t.Fatalf("fetchDeb: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d entries, want 2", len(got))
		}
	})

	t.Run("tampered signature fails closed", func(t *testing.T) {
		release, sig, pubKey := debReleaseFixture(t, "main", "amd64", pkggz)
		tampered := tamperArmoredSignature(t, sig)
		srv, _ := debServer(t, map[string][]byte{
			dir + "Packages.gz":        pkggz,
			"/dists/noble/Release":     release,
			"/dists/noble/Release.gpg": tampered,
		})
		if _, err := fetchDeb(context.Background(), srv.Client(), repo(srv.URL, writeKeyFile(t, pubKey))); err == nil {
			t.Fatal("want an error for a tampered Release signature")
		}
	})

	t.Run("wrong checksum fails closed", func(t *testing.T) {
		release, sig, pubKey := debReleaseFixture(t, "main", "amd64", pkggz)
		wrongPkggz := gzipped(t, twoStanzas+"\n")
		srv, _ := debServer(t, map[string][]byte{
			// Serve a Packages.gz that does not match what Release signed for.
			dir + "Packages.gz":        wrongPkggz,
			"/dists/noble/Release":     release,
			"/dists/noble/Release.gpg": sig,
		})
		if _, err := fetchDeb(context.Background(), srv.Client(), repo(srv.URL, writeKeyFile(t, pubKey))); err == nil {
			t.Fatal("want an error when the fetched index does not match Release's checksum")
		}
	})

	t.Run("no GPGKeyPath configured skips verification", func(t *testing.T) {
		srv, paths := debServer(t, map[string][]byte{dir + "Packages.gz": pkggz})
		got, err := fetchDeb(context.Background(), srv.Client(), repo(srv.URL, ""))
		if err != nil {
			t.Fatalf("fetchDeb: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d entries, want 2", len(got))
		}
		for _, p := range *paths {
			if strings.Contains(p, "Release") {
				t.Errorf("unverified repo should never request Release, got request for %q", p)
			}
		}
	})

	t.Run("missing key file is an error, not a silent skip", func(t *testing.T) {
		srv, _ := debServer(t, map[string][]byte{dir + "Packages.gz": pkggz})
		if _, err := fetchDeb(context.Background(), srv.Client(), repo(srv.URL, filepath.Join(t.TempDir(), "missing.gpg"))); err == nil {
			t.Fatal("want an error when GPGKeyPath is set but unreadable")
		}
	})
}
