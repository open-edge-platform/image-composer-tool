package isopayload

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validManifest() Manifest {
	return Manifest{
		SchemaVersion:    ManifestSchemaVersion,
		Compression:      "zstd",
		CompressedSha256: strings.Repeat("a", 64),
		CompressedBytes:  100,
		RawSha256:        strings.Repeat("b", 64),
		RawBytes:         200,
	}
}

func TestManifestWriteLoadRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "payload.manifest.yaml")

	want := validManifest()
	if err := Write(want, path); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != want {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", got, want)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	t.Parallel()
	if _, err := Load(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("expected error loading a missing manifest")
	}
}

func TestManifestValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(m *Manifest)
		wantErr string
	}{
		{name: "valid manifest", wantErr: ""},
		{
			name:    "bad schema version",
			mutate:  func(m *Manifest) { m.SchemaVersion = "99" },
			wantErr: "unsupported payload manifest schemaVersion",
		},
		{
			name:    "unsupported compression",
			mutate:  func(m *Manifest) { m.Compression = "bogus" },
			wantErr: "unsupported payload compression",
		},
		{
			name:    "bad compressed sha hex length",
			mutate:  func(m *Manifest) { m.CompressedSha256 = "deadbeef" },
			wantErr: "invalid compressedSha256",
		},
		{
			name:    "bad compressed sha hex chars",
			mutate:  func(m *Manifest) { m.CompressedSha256 = strings.Repeat("Z", 64) },
			wantErr: "invalid compressedSha256",
		},
		{
			name:    "bad raw sha",
			mutate:  func(m *Manifest) { m.RawSha256 = "not-hex" },
			wantErr: "invalid rawSha256",
		},
		{
			name:    "zero compressed bytes",
			mutate:  func(m *Manifest) { m.CompressedBytes = 0 },
			wantErr: "compressedBytes must be positive",
		},
		{
			name:    "negative raw bytes",
			mutate:  func(m *Manifest) { m.RawBytes = -1 },
			wantErr: "rawBytes must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := validManifest()
			if tt.mutate != nil {
				tt.mutate(&m)
			}
			err := m.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

func TestFileSHA256(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "data.bin")
	if err := os.WriteFile(path, []byte("hello world"), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	got, err := FileSHA256(path)
	if err != nil {
		t.Fatalf("FileSHA256: %v", err)
	}
	// Known-answer test: sha256("hello world").
	const knownAnswer = "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if got != knownAnswer {
		t.Fatalf("FileSHA256 = %s, want %s", got, knownAnswer)
	}
}

func TestFileSHA256_MissingFile(t *testing.T) {
	t.Parallel()
	if _, err := FileSHA256(filepath.Join(t.TempDir(), "missing.bin")); err == nil {
		t.Fatal("expected error hashing a missing file")
	}
}
