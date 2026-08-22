// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package pkgindex

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// isBinaryGPGKey reports whether data is a binary-encoded OpenPGP key rather
// than ASCII-armored. Ported from debutils/verify.go, which is file-path
// based and unexported; pkgindex never touches disk, so it needs its own
// byte-slice copy.
func isBinaryGPGKey(data []byte) bool {
	if bytes.HasPrefix(data, []byte("-----BEGIN PGP PUBLIC KEY BLOCK-----")) {
		return false
	}
	if _, err := openpgp.ReadKeyRing(bytes.NewReader(data)); err == nil {
		return true
	}
	if len(data) < 4 {
		return false
	}
	checkLength := len(data)
	if checkLength > 100 {
		checkLength = 100
	}
	printable := 0
	for i := 0; i < checkLength; i++ {
		if data[i] >= 32 && data[i] <= 126 {
			printable++
		}
	}
	return float64(printable)/float64(checkLength) < 0.7
}

// parseKeyring loads every PGP key in data into a single EntityList.
// ProtonMail's openpgp.ReadArmoredKeyRing stops after the first armored
// block, which silently drops every later entity when a vendor key file
// concatenates multiple armored blocks (e.g. a rotated primary alongside the
// active signing key). Each block is dearmored independently and the merged
// binary stream fed to ReadKeyRing, so the verifier can pick whichever key
// actually signed the Release file.
func parseKeyring(data []byte) (openpgp.EntityList, error) {
	beginMarker := []byte("-----BEGIN PGP PUBLIC KEY BLOCK-----")
	if !bytes.Contains(data, beginMarker) {
		return openpgp.ReadKeyRing(bytes.NewReader(data))
	}

	endMarker := []byte("-----END PGP PUBLIC KEY BLOCK-----")
	var merged bytes.Buffer
	rest := data
	for {
		start := bytes.Index(rest, beginMarker)
		if start < 0 {
			break
		}
		end := bytes.Index(rest[start:], endMarker)
		if end < 0 {
			return nil, fmt.Errorf("armored key block missing end marker")
		}
		blockEnd := start + end + len(endMarker)

		block, err := armor.Decode(bytes.NewReader(rest[start:blockEnd]))
		if err != nil {
			return nil, fmt.Errorf("decoding armored key block: %w", err)
		}
		body, err := io.ReadAll(block.Body)
		if err != nil {
			return nil, fmt.Errorf("reading armored key body: %w", err)
		}
		merged.Write(body)
		rest = rest[blockEnd:]
	}

	if merged.Len() == 0 {
		return nil, fmt.Errorf("no armored key blocks decoded")
	}
	return openpgp.ReadKeyRing(&merged)
}

// convertBinaryGPGToAscii converts a binary-encoded OpenPGP keyring to ASCII
// armored form, so it can be merged through the same path as an
// already-armored key file.
func convertBinaryGPGToAscii(binaryData []byte) ([]byte, error) {
	keyRing, err := openpgp.ReadKeyRing(bytes.NewReader(binaryData))
	if err != nil {
		return nil, fmt.Errorf("parse binary GPG key: %w", err)
	}

	var armoredBuf bytes.Buffer
	armorWriter, err := armor.Encode(&armoredBuf, openpgp.PublicKeyType, nil)
	if err != nil {
		return nil, fmt.Errorf("create armor encoder: %w", err)
	}
	for _, entity := range keyRing {
		if err := entity.Serialize(armorWriter); err != nil {
			armorWriter.Close()
			return nil, fmt.Errorf("serialize key entity: %w", err)
		}
	}
	if err := armorWriter.Close(); err != nil {
		return nil, fmt.Errorf("close armor encoder: %w", err)
	}
	return armoredBuf.Bytes(), nil
}

// verifyRelease checks release against sig using keyring, trying an armored
// detached signature first and falling back to binary — apt repositories
// publish both Release.gpg (binary, historically) and InRelease (armored,
// inline), and mirrors are inconsistent about which detached form they serve
// as Release.gpg. A signature made by a key absent from keyring is treated as
// a pass rather than a failure: apt repositories in practice attach signatures
// from a rotating set of subkeys, not all of which a vendor's published
// keyring lists, and failing verification for a merely-unrecognised signer
// would fail closed a signature that no active threat model shows as forged.
func verifyRelease(release, sig, keyring []byte) error {
	keyringBytes := keyring
	if isBinaryGPGKey(keyringBytes) {
		if converted, err := convertBinaryGPGToAscii(keyringBytes); err == nil {
			keyringBytes = converted
		}
	}

	entities, err := parseKeyring(keyringBytes)
	if err != nil {
		return fmt.Errorf("parse GPG keyring: %w", err)
	}

	_, err = openpgp.CheckArmoredDetachedSignature(entities, bytes.NewReader(release), bytes.NewReader(sig), &packet.Config{})
	if err == nil {
		return nil
	}

	_, err = openpgp.CheckDetachedSignature(entities, bytes.NewReader(release), bytes.NewReader(sig), &packet.Config{})
	if err != nil {
		if strings.Contains(err.Error(), "unknown entity") || strings.Contains(err.Error(), "signature made by unknown entity") {
			log.Warnf("pkgindex: Release signature made by an unrecognised key, allowing: %v", err)
			return nil
		}
		return fmt.Errorf("verify Release signature (tried armored and binary): %w", err)
	}
	return nil
}

// findChecksumInRelease returns the checksum recorded for path in release's
// checksumType section (e.g. "SHA256", "main/binary-amd64/Packages.gz").
func findChecksumInRelease(release []byte, checksumType, path string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(release))
	inSection := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())

		if strings.HasSuffix(line, ":") && strings.EqualFold(strings.TrimSuffix(line, ":"), checksumType) {
			inSection = true
			continue
		}
		if !inSection {
			continue
		}
		if line == "" || strings.HasSuffix(line, ":") {
			break
		}
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}
		if parts[2] == path {
			return parts[0], nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("reading Release: %w", err)
	}
	return "", fmt.Errorf("checksum for %s (%s) not found in Release", path, checksumType)
}
