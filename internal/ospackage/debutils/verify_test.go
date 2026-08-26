package debutils

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
)

// TestVerifyPackagegz tests the VerifyPackagegz function
func TestVerifyPackagegz(t *testing.T) {
	tests := []struct {
		name          string
		setupFiles    func(tempDir string) (relPath, pkggzPath, arch string)
		expectOK      bool
		expectError   bool
		errorContains string
	}{
		{
			name: "valid checksum match",
			setupFiles: func(tempDir string) (string, string, string) {
				// Create a test Packages.gz file
				pkggzPath := filepath.Join(tempDir, "Packages.gz")
				content := []byte("test packages content")
				err := os.WriteFile(pkggzPath, content, 0644)
				if err != nil {
					t.Fatalf("Failed to create test packages file: %v", err)
				}

				// Calculate its checksum
				checksum := fmt.Sprintf("%x", sha256.Sum256(content))

				// Create Release file with matching checksum
				relPath := filepath.Join(tempDir, "Release")
				releaseContent := fmt.Sprintf(`Suite: stable
SHA256:
 %s 21 main/binary-amd64/Packages.gz
 other_checksum 123 other/file
`, checksum)
				err = os.WriteFile(relPath, []byte(releaseContent), 0644)
				if err != nil {
					t.Fatalf("Failed to create Release file: %v", err)
				}

				return relPath, pkggzPath, "amd64"
			},
			expectOK:    true,
			expectError: false,
		},
		{
			name: "checksum mismatch",
			setupFiles: func(tempDir string) (string, string, string) {
				// Create a test Packages.gz file
				pkggzPath := filepath.Join(tempDir, "Packages.gz")
				content := []byte("test packages content")
				err := os.WriteFile(pkggzPath, content, 0644)
				if err != nil {
					t.Fatalf("Failed to create test packages file: %v", err)
				}

				// Create Release file with wrong checksum
				relPath := filepath.Join(tempDir, "Release")
				releaseContent := `Suite: stable
SHA256:
 wrongchecksum123456789 21 main/binary-amd64/Packages.gz
`
				err = os.WriteFile(relPath, []byte(releaseContent), 0644)
				if err != nil {
					t.Fatalf("Failed to create Release file: %v", err)
				}

				return relPath, pkggzPath, "amd64"
			},
			expectOK:      false,
			expectError:   true,
			errorContains: "checksum mismatch",
		},
		{
			name: "release file not found",
			setupFiles: func(tempDir string) (string, string, string) {
				pkggzPath := filepath.Join(tempDir, "Packages.gz")
				content := []byte("test content")
				err := os.WriteFile(pkggzPath, content, 0644)
				if err != nil {
					t.Fatalf("Failed to create test packages file: %v", err)
				}

				return filepath.Join(tempDir, "nonexistent.Release"), pkggzPath, "amd64"
			},
			expectOK:      false,
			expectError:   true,
			errorContains: "failed to get checksum from Release",
		},
		{
			name: "packages file not found",
			setupFiles: func(tempDir string) (string, string, string) {
				relPath := filepath.Join(tempDir, "Release")
				releaseContent := `Suite: stable
SHA256:
 somechecksum 21 main/binary-amd64/Packages.gz
`
				err := os.WriteFile(relPath, []byte(releaseContent), 0644)
				if err != nil {
					t.Fatalf("Failed to create Release file: %v", err)
				}

				return relPath, filepath.Join(tempDir, "nonexistent.gz"), "amd64"
			},
			expectOK:      false,
			expectError:   true,
			errorContains: "checksum for main/binary-amd64/nonexistent.gz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir, err := os.MkdirTemp("", "verify_test")
			if err != nil {
				t.Fatalf("Failed to create temp directory: %v", err)
			}
			defer os.RemoveAll(tempDir)

			relPath, pkggzPath, arch := tt.setupFiles(tempDir)

			ok, err := VerifyPackagegz(relPath, pkggzPath, arch, "main")

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain %q, got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}

			if ok != tt.expectOK {
				t.Errorf("Expected OK=%v, got %v", tt.expectOK, ok)
			}
		})
	}
}

// TestVerifyRelease tests the VerifyRelease function
func TestVerifyRelease(t *testing.T) {
	tests := []struct {
		name          string
		setupFiles    func(tempDir string) (relPath, sigPath, keyPath string)
		expectOK      bool
		expectError   bool
		errorContains string
	}{
		{
			name: "release file not found",
			setupFiles: func(tempDir string) (string, string, string) {
				// Create dummy signature and key files
				sigPath := filepath.Join(tempDir, "Release.gpg")
				keyPath := filepath.Join(tempDir, "key.pub")

				err := os.WriteFile(sigPath, []byte("dummy signature"), 0644)
				if err != nil {
					t.Fatalf("Failed to create signature file: %v", err)
				}

				err = os.WriteFile(keyPath, []byte("dummy key"), 0644)
				if err != nil {
					t.Fatalf("Failed to create key file: %v", err)
				}

				return filepath.Join(tempDir, "nonexistent.Release"), sigPath, keyPath
			},
			expectOK:      false,
			expectError:   true,
			errorContains: "failed to read Release file",
		},
		{
			name: "signature file not found",
			setupFiles: func(tempDir string) (string, string, string) {
				relPath := filepath.Join(tempDir, "Release")
				keyPath := filepath.Join(tempDir, "key.pub")

				err := os.WriteFile(relPath, []byte("test release content"), 0644)
				if err != nil {
					t.Fatalf("Failed to create Release file: %v", err)
				}

				err = os.WriteFile(keyPath, []byte("dummy key"), 0644)
				if err != nil {
					t.Fatalf("Failed to create key file: %v", err)
				}

				return relPath, filepath.Join(tempDir, "nonexistent.gpg"), keyPath
			},
			expectOK:      false,
			expectError:   true,
			errorContains: "failed to read Release signature",
		},
		{
			name: "key file not found",
			setupFiles: func(tempDir string) (string, string, string) {
				relPath := filepath.Join(tempDir, "Release")
				sigPath := filepath.Join(tempDir, "Release.gpg")

				err := os.WriteFile(relPath, []byte("test release content"), 0644)
				if err != nil {
					t.Fatalf("Failed to create Release file: %v", err)
				}

				err = os.WriteFile(sigPath, []byte("dummy signature"), 0644)
				if err != nil {
					t.Fatalf("Failed to create signature file: %v", err)
				}

				return relPath, sigPath, filepath.Join(tempDir, "nonexistent.pub")
			},
			expectOK:      false,
			expectError:   true,
			errorContains: "failed to read public key",
		},
		{
			name: "invalid key format",
			setupFiles: func(tempDir string) (string, string, string) {
				relPath := filepath.Join(tempDir, "Release")
				sigPath := filepath.Join(tempDir, "Release.gpg")
				keyPath := filepath.Join(tempDir, "key.pub")

				err := os.WriteFile(relPath, []byte("test release content"), 0644)
				if err != nil {
					t.Fatalf("Failed to create Release file: %v", err)
				}

				err = os.WriteFile(sigPath, []byte("dummy signature"), 0644)
				if err != nil {
					t.Fatalf("Failed to create signature file: %v", err)
				}

				err = os.WriteFile(keyPath, []byte("invalid key content"), 0644)
				if err != nil {
					t.Fatalf("Failed to create key file: %v", err)
				}

				return relPath, sigPath, keyPath
			},
			expectOK:      false,
			expectError:   true,
			errorContains: "failed to parse public key",
		},
		{
			name: "trusted=yes skips verification",
			setupFiles: func(tempDir string) (string, string, string) {
				relPath := filepath.Join(tempDir, "Release")
				sigPath := filepath.Join(tempDir, "Release.gpg")

				err := os.WriteFile(relPath, []byte("test release content"), 0644)
				if err != nil {
					t.Fatalf("Failed to create Release file: %v", err)
				}

				// Signature file doesn't need to exist when using [trusted=yes]

				return relPath, sigPath, "[trusted=yes]"
			},
			expectOK:    true,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir, err := os.MkdirTemp("", "verify_release_test")
			if err != nil {
				t.Fatalf("Failed to create temp directory: %v", err)
			}
			defer os.RemoveAll(tempDir)

			relPath, sigPath, keyPath := tt.setupFiles(tempDir)

			ok, err := VerifyRelease(relPath, sigPath, keyPath)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain %q, got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}

			if ok != tt.expectOK {
				t.Errorf("Expected OK=%v, got %v", tt.expectOK, ok)
			}
		})
	}
}

// TestVerifyDEBs tests the VerifyDEBs function
func TestVerifyDEBs(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "verify_debs_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test DEB files
	validDeb := filepath.Join(tempDir, "valid.deb")
	invalidDeb := filepath.Join(tempDir, "invalid.deb")
	missingDeb := filepath.Join(tempDir, "missing.deb")

	validContent := []byte("valid deb content")
	invalidContent := []byte("invalid deb content")

	err = os.WriteFile(validDeb, validContent, 0644)
	if err != nil {
		t.Fatalf("Failed to create valid deb file: %v", err)
	}

	err = os.WriteFile(invalidDeb, invalidContent, 0644)
	if err != nil {
		t.Fatalf("Failed to create invalid deb file: %v", err)
	}

	// Create checksum map
	validChecksum := fmt.Sprintf("%x", sha256.Sum256(validContent))
	wrongChecksum := "wrongchecksum123456789"

	pkgChecksum := map[string][]string{
		"valid.deb":   {validChecksum},
		"invalid.deb": {wrongChecksum},
		// missing.deb intentionally not included
	}

	paths := []string{validDeb, invalidDeb, missingDeb, filepath.Join(tempDir, "nonexistent.deb")}

	results := VerifyDEBs(paths, pkgChecksum, 2)

	// Verify results
	if len(results) != len(paths) {
		t.Errorf("Expected %d results, got %d", len(paths), len(results))
	}

	// Check valid.deb result
	if results[0].Path != validDeb {
		t.Errorf("Expected path %s, got %s", validDeb, results[0].Path)
	}
	if !results[0].OK {
		t.Errorf("Expected valid.deb to pass verification, but it failed: %v", results[0].Error)
	}
	if results[0].Duration <= 0 {
		t.Errorf("Expected positive duration, got %v", results[0].Duration)
	}

	// Check invalid.deb result (checksum mismatch)
	if results[1].Path != invalidDeb {
		t.Errorf("Expected path %s, got %s", invalidDeb, results[1].Path)
	}
	if results[1].OK {
		t.Errorf("Expected invalid.deb to fail verification")
	}
	if results[1].Error == nil || !strings.Contains(results[1].Error.Error(), "checksum mismatch") {
		t.Errorf("Expected checksum mismatch error, got: %v", results[1].Error)
	}

	// Check missing.deb result (no checksum found)
	if results[2].Path != missingDeb {
		t.Errorf("Expected path %s, got %s", missingDeb, results[2].Path)
	}
	if results[2].OK {
		t.Errorf("Expected missing.deb to fail verification")
	}
	if results[2].Error == nil || !strings.Contains(results[2].Error.Error(), "no checksums found") {
		t.Errorf("Expected 'no checksums found' error, got: %v", results[2].Error)
	}

	// Check nonexistent.deb result (file not found)
	if results[3].OK {
		t.Errorf("Expected nonexistent.deb to fail verification")
	}
	if results[3].Error == nil {
		t.Errorf("Expected error for nonexistent file")
	}
}

// TestGetChecksumByName tests the getChecksumByName function
func TestGetChecksumByName(t *testing.T) {
	pkgChecksum := map[string]string{
		"package1.deb": "checksum1",
		"package2.deb": "checksum2",
	}

	tests := []struct {
		name     string
		debPath  string
		expected string
	}{
		{
			name:     "existing package",
			debPath:  "/path/to/package1.deb",
			expected: "checksum1",
		},
		{
			name:     "another existing package",
			debPath:  "package2.deb",
			expected: "checksum2",
		},
		{
			name:     "non-existing package",
			debPath:  "/path/to/nonexistent.deb",
			expected: "NOT FOUND",
		},
		{
			name:     "empty path",
			debPath:  "",
			expected: "NOT FOUND",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getChecksumByName(pkgChecksum, tt.debPath)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

// TestComputeFileSHA256 tests the computeFileSHA256 function
func TestComputeFileSHA256(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		expectError bool
	}{
		{
			name:        "normal file",
			content:     "Hello, World!",
			expectError: false,
		},
		{
			name:        "empty file",
			content:     "",
			expectError: false,
		},
		{
			name:        "binary content",
			content:     string([]byte{0, 1, 2, 3, 255, 254, 253}),
			expectError: false,
		},
		{
			name:        "large content",
			content:     strings.Repeat("Lorem ipsum dolor sit amet. ", 1000),
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir, err := os.MkdirTemp("", "sha256_test")
			if err != nil {
				t.Fatalf("Failed to create temp directory: %v", err)
			}
			defer os.RemoveAll(tempDir)

			testFile := filepath.Join(tempDir, "test.txt")
			err = os.WriteFile(testFile, []byte(tt.content), 0644)
			if err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			result, err := computeFileSHA256(testFile)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}

				// Verify the checksum is correct
				expected := fmt.Sprintf("%x", sha256.Sum256([]byte(tt.content)))
				if result != expected {
					t.Errorf("Expected checksum %s, got %s", expected, result)
				}

				// Verify it's a valid hex string of correct length
				if len(result) != 64 {
					t.Errorf("Expected 64-character hex string, got %d characters", len(result))
				}
			}
		})
	}
}

// TestComputeFileSHA256_NonexistentFile tests error handling for non-existent files
func TestComputeFileSHA256_NonexistentFile(t *testing.T) {
	result, err := computeFileSHA256("/nonexistent/path/file.txt")
	if err == nil {
		t.Errorf("Expected error for nonexistent file, but got none")
	}
	if result != "" {
		t.Errorf("Expected empty result for error case, got: %s", result)
	}
}

// TestFindChecksumInRelease tests the findChecksumInRelease function
func TestFindChecksumInRelease(t *testing.T) {
	tests := []struct {
		name           string
		releaseContent string
		checksumType   string
		fileName       string
		expectedSum    string
		expectError    bool
		errorContains  string
	}{
		{
			name: "valid SHA256 checksum",
			releaseContent: `Suite: stable
Version: 1.0
SHA256:
 checksum1234567890abcdef 1234 main/binary-amd64/Packages.gz
 otherchecksum123456789 5678 main/binary-arm64/Packages.gz
MD5Sum:
 md5checksum123 1234 main/binary-amd64/Packages.gz
`,
			checksumType: "SHA256",
			fileName:     "main/binary-amd64/Packages.gz",
			expectedSum:  "checksum1234567890abcdef",
			expectError:  false,
		},
		{
			name: "valid MD5 checksum",
			releaseContent: `Suite: stable
SHA256:
 sha256checksum123 1234 other/file
MD5Sum:
 md5checksum456 5678 main/binary-amd64/Packages.gz
`,
			checksumType: "MD5Sum",
			fileName:     "main/binary-amd64/Packages.gz",
			expectedSum:  "md5checksum456",
			expectError:  false,
		},
		{
			name: "file not found in section",
			releaseContent: `Suite: stable
SHA256:
 checksum1234 1234 main/binary-arm64/Packages.gz
 checksum5678 5678 other/file
`,
			checksumType:  "SHA256",
			fileName:      "main/binary-amd64/Packages.gz",
			expectError:   true,
			errorContains: "checksum for main/binary-amd64/Packages.gz (SHA256) not found",
		},
		{
			name: "checksum section not found",
			releaseContent: `Suite: stable
Version: 1.0
MD5Sum:
 md5checksum123 1234 main/binary-amd64/Packages.gz
`,
			checksumType:  "SHA256",
			fileName:      "main/binary-amd64/Packages.gz",
			expectError:   true,
			errorContains: "checksum for main/binary-amd64/Packages.gz (SHA256) not found",
		},
		{
			name: "case insensitive checksum type",
			releaseContent: `Suite: stable
sha256:
 checksum1234567890abcdef 1234 main/binary-amd64/Packages.gz
`,
			checksumType: "SHA256",
			fileName:     "main/binary-amd64/Packages.gz",
			expectedSum:  "checksum1234567890abcdef",
			expectError:  false,
		},
		{
			name: "malformed line in checksum section",
			releaseContent: `Suite: stable
SHA256:
 checksum1234567890abcdef 1234 main/binary-amd64/Packages.gz
 malformed_line_with_only_two_parts incomplete
 anothergoodchecksum 5678 main/binary-arm64/Packages.gz
`,
			checksumType: "SHA256",
			fileName:     "main/binary-arm64/Packages.gz",
			expectedSum:  "anothergoodchecksum",
			expectError:  false,
		},
		{
			name:           "empty release file",
			releaseContent: ``,
			checksumType:   "SHA256",
			fileName:       "main/binary-amd64/Packages.gz",
			expectError:    true,
			errorContains:  "checksum for main/binary-amd64/Packages.gz (SHA256) not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir, err := os.MkdirTemp("", "release_test")
			if err != nil {
				t.Fatalf("Failed to create temp directory: %v", err)
			}
			defer os.RemoveAll(tempDir)

			releasePath := filepath.Join(tempDir, "Release")
			err = os.WriteFile(releasePath, []byte(tt.releaseContent), 0644)
			if err != nil {
				t.Fatalf("Failed to create Release file: %v", err)
			}

			result, err := findChecksumInRelease(releasePath, tt.checksumType, tt.fileName)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain %q, got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
				if result != tt.expectedSum {
					t.Errorf("Expected checksum %s, got %s", tt.expectedSum, result)
				}
			}
		})
	}
}

// TestFindChecksumInRelease_FileNotFound tests error handling for non-existent Release file
func TestFindChecksumInRelease_FileNotFound(t *testing.T) {
	result, err := findChecksumInRelease("/nonexistent/Release", "SHA256", "test.gz")
	if err == nil {
		t.Errorf("Expected error for nonexistent file, but got none")
	}
	if !strings.Contains(err.Error(), "failed to open release file") {
		t.Errorf("Expected 'failed to open release file' in error, got: %v", err)
	}
	if result != "" {
		t.Errorf("Expected empty result for error case, got: %s", result)
	}
}

// TestVerifyWithGoDeb tests the verifyWithGoDeb function
func TestVerifyWithGoDeb(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "verify_godeb_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test files
	validDeb := filepath.Join(tempDir, "valid.deb")
	invalidDeb := filepath.Join(tempDir, "invalid.deb")

	validContent := []byte("valid deb content")
	invalidContent := []byte("invalid deb content")

	err = os.WriteFile(validDeb, validContent, 0644)
	if err != nil {
		t.Fatalf("Failed to create valid deb file: %v", err)
	}

	err = os.WriteFile(invalidDeb, invalidContent, 0644)
	if err != nil {
		t.Fatalf("Failed to create invalid deb file: %v", err)
	}

	// Create checksum map
	validChecksum := fmt.Sprintf("%x", sha256.Sum256(validContent))
	wrongChecksum := "wrongchecksum123456789"

	tests := []struct {
		name          string
		debFile       string
		pkgChecksum   map[string]string
		expectError   bool
		errorContains string
	}{
		{
			name:    "valid checksum match",
			debFile: validDeb,
			pkgChecksum: map[string]string{
				"valid.deb": validChecksum,
			},
			expectError: false,
		},
		{
			name:    "checksum mismatch",
			debFile: invalidDeb,
			pkgChecksum: map[string]string{
				"invalid.deb": wrongChecksum,
			},
			expectError:   true,
			errorContains: "checksum mismatch",
		},
		{
			name:          "no checksum found",
			debFile:       validDeb,
			pkgChecksum:   map[string]string{},
			expectError:   true,
			errorContains: "no checksum found",
		},
		{
			name:    "file not found",
			debFile: filepath.Join(tempDir, "nonexistent.deb"),
			pkgChecksum: map[string]string{
				"nonexistent.deb": "somechecksum",
			},
			expectError:   true,
			errorContains: "no such file or directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifyWithGoDeb(tt.debFile, tt.pkgChecksum)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain %q, got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

// generateArmoredTestKey produces a self-contained ASCII-armored PGP public key
// suitable for parseKeyring fixtures. Each call returns a freshly-generated key.
func generateArmoredTestKey(t *testing.T, name, email string) []byte {
	t.Helper()
	entity, err := openpgp.NewEntity(name, "test", email, nil)
	if err != nil {
		t.Fatalf("NewEntity: %v", err)
	}

	var armored bytes.Buffer
	w, err := armor.Encode(&armored, openpgp.PublicKeyType, nil)
	if err != nil {
		t.Fatalf("armor.Encode: %v", err)
	}
	if err := entity.Serialize(w); err != nil {
		t.Fatalf("entity.Serialize: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("armor close: %v", err)
	}
	return armored.Bytes()
}

// TestParseKeyring covers the verify-side keyring parser, especially the
// concatenated-armored-blocks case that the ProtonMail openpgp library does
// not handle natively (it stops after the first block).
func TestParseKeyring(t *testing.T) {
	keyA := generateArmoredTestKey(t, "Test Key A", "a@example.invalid")
	keyB := generateArmoredTestKey(t, "Test Key B", "b@example.invalid")

	t.Run("single armored block", func(t *testing.T) {
		ring, err := parseKeyring(keyA)
		if err != nil {
			t.Fatalf("parseKeyring: %v", err)
		}
		if len(ring) != 1 {
			t.Fatalf("expected 1 entity, got %d", len(ring))
		}
	})

	t.Run("two concatenated armored blocks", func(t *testing.T) {
		// Mimic vendor key files (e.g. intel-graphics.key) that ship a
		// rotated primary alongside the active signing key in one file.
		concatenated := append(append([]byte{}, keyA...), keyB...)

		ring, err := parseKeyring(concatenated)
		if err != nil {
			t.Fatalf("parseKeyring: %v", err)
		}
		if len(ring) != 2 {
			t.Fatalf("expected 2 entities from concatenated blocks, got %d", len(ring))
		}
	})

	t.Run("concatenated blocks separated by whitespace", func(t *testing.T) {
		concatenated := append(append([]byte{}, keyA...), []byte("\n\n")...)
		concatenated = append(concatenated, keyB...)

		ring, err := parseKeyring(concatenated)
		if err != nil {
			t.Fatalf("parseKeyring: %v", err)
		}
		if len(ring) != 2 {
			t.Fatalf("expected 2 entities, got %d", len(ring))
		}
	})

	t.Run("binary keyring is forwarded as-is", func(t *testing.T) {
		entity, err := openpgp.NewEntity("Binary Key", "test", "bin@example.invalid", nil)
		if err != nil {
			t.Fatalf("NewEntity: %v", err)
		}
		var binary bytes.Buffer
		if err := entity.Serialize(&binary); err != nil {
			t.Fatalf("entity.Serialize: %v", err)
		}

		ring, err := parseKeyring(binary.Bytes())
		if err != nil {
			t.Fatalf("parseKeyring: %v", err)
		}
		if len(ring) != 1 {
			t.Fatalf("expected 1 entity from binary input, got %d", len(ring))
		}
	})

	t.Run("armored block missing end marker", func(t *testing.T) {
		truncated := bytes.Replace(keyA, []byte("-----END PGP PUBLIC KEY BLOCK-----"), []byte{}, 1)

		_, err := parseKeyring(truncated)
		if err == nil {
			t.Fatalf("expected error for truncated armored block")
		}
		if !strings.Contains(err.Error(), "missing end marker") {
			t.Fatalf("expected missing-end-marker error, got: %v", err)
		}
	})

	t.Run("garbage input is rejected", func(t *testing.T) {
		_, err := parseKeyring([]byte("this is not a key"))
		if err == nil {
			t.Fatalf("expected error for garbage input")
		}
	})
}

// TestResult tests the Result struct
func TestResult(t *testing.T) {
	result := Result{
		Path:     "/path/to/test.deb",
		OK:       true,
		Duration: time.Millisecond * 100,
		Error:    nil,
	}

	if result.Path != "/path/to/test.deb" {
		t.Errorf("Expected path '/path/to/test.deb', got %s", result.Path)
	}
	if !result.OK {
		t.Errorf("Expected OK=true, got %v", result.OK)
	}
	if result.Duration != time.Millisecond*100 {
		t.Errorf("Expected duration 100ms, got %v", result.Duration)
	}
	if result.Error != nil {
		t.Errorf("Expected no error, got %v", result.Error)
	}

	// Test with error
	testError := fmt.Errorf("test error")
	errorResult := Result{
		Path:     "/path/to/error.deb",
		OK:       false,
		Duration: time.Millisecond * 50,
		Error:    testError,
	}

	if errorResult.OK {
		t.Errorf("Expected OK=false, got %v", errorResult.OK)
	}
	if errorResult.Error != testError {
		t.Errorf("Expected error %v, got %v", testError, errorResult.Error)
	}
}
