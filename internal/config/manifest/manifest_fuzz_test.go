package manifest

import (
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
)

// FuzzWriteManifestToFile tests manifest file writing with various inputs
func FuzzWriteManifestToFile(f *testing.F) {
	// Seed with various manifest data patterns
	f.Add("test-image", "1.0.0", "x86_64", int64(1024), "sha256abc", "sha256", "", "", "")
	f.Add("", "", "", int64(0), "", "", "", "", "") // Empty values
	f.Add("very-long-image-name-that-might-cause-issues", "1.0.0-alpha-beta-gamma", "arm64", int64(999999999), "sha256def", "md5", "sig123", "rsa", "0.9.0")
	f.Add("test", "1.0", "x86_64", int64(-1), "invalid-hash", "unknown-alg", "malformed-sig", "unknown-sig-alg", "future-version")
	f.Add("test\nwith\nnewlines", "1.0\ttabs", "arch", int64(1024), "hash", "alg", "sig", "sig-alg", "ver")
	f.Add("test", "1.0", "x86_64", int64(1024), "", "", "", "", "") // Missing hash/signature

	f.Fuzz(func(t *testing.T, imageName, imageVersion, arch string, sizeBytes int64, hash, hashAlg, signature, sigAlg, minCurrentVersion string) {
		// Create temporary directory for test
		tempDir := t.TempDir()
		manifestPath := tempDir + "/manifest.json"

		// Create manifest data (note: not a pointer)
		manifest := SoftwarePackageManifest{
			SchemaVersion:     "1.0",
			ImageVersion:      imageVersion,
			BuiltAt:           "2024-01-01T00:00:00Z",
			Arch:              arch,
			SizeBytes:         sizeBytes,
			Hash:              hash,
			HashAlg:           hashAlg,
			Signature:         signature,
			SigAlg:            sigAlg,
			MinCurrentVersion: minCurrentVersion,
		}

		// Test WriteManifestToFile - should not crash with any input
		err := WriteManifestToFile(manifest, manifestPath)

		// Function should handle all inputs gracefully
		_ = err // We accept both success and error, just no crashes
	})
}

// FuzzWriteSPDXToFile tests SPDX file writing with various package inputs
func FuzzWriteSPDXToFile(f *testing.F) {
	// Seed with various package info patterns
	f.Add("package1", "1.0.0", "x86_64", "description1", "MIT")
	f.Add("", "", "", "", "") // Empty values
	f.Add("very-long-package-name-that-might-cause-issues", "1.0.0-alpha-beta", "arm64", "A very long description that might cause buffer issues", "Apache-2.0")
	f.Add("pkg\nwith\nnewlines", "1.0\ttabs", "arch", "desc\nwith\nnewlines", "license\nwith\nnewlines")
	f.Add("package with spaces", "version with spaces", "arch with spaces", "description with spaces", "license with spaces")

	f.Fuzz(func(t *testing.T, name, version, arch, description, license string) {
		// Create temporary directory for test
		tempDir := t.TempDir()
		spdxPath := tempDir + "/spdx.json"

		// Create package info slice
		pkgs := []ospackage.PackageInfo{
			{
				Name:        name,
				Version:     version,
				Arch:        arch,
				Description: description,
				License:     license,
				Type:        "rpm", // Fixed type for testing
			},
		}

		// Test WriteSPDXToFile - should not crash with any input
		err := WriteSPDXToFile(pkgs, spdxPath)

		// Function should handle all inputs gracefully
		_ = err // We accept both success and error, just no crashes
	})
}

// FuzzGenerateDocumentNamespace tests namespace generation
func FuzzGenerateDocumentNamespace(f *testing.F) {
	// This function takes no parameters, so we just test it runs without crashing
	f.Add(true) // Dummy seed value

	f.Fuzz(func(t *testing.T, dummy bool) {
		// Test generateDocumentNamespace - should not crash
		namespace := generateDocumentNamespace()

		// Function should always return a string
		if namespace == "" {
			t.Log("Generated empty namespace") // This might be OK
		}
	})
}
