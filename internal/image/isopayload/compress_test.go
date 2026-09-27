package isopayload

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompress_None(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rawPath := filepath.Join(dir, "payload.raw")
	rawContent := []byte("fake raw disk image contents")
	if err := os.WriteFile(rawPath, rawContent, 0600); err != nil {
		t.Fatalf("write fixture raw: %v", err)
	}

	dstDir := t.TempDir()
	m, outPath, err := Compress(rawPath, dstDir, "none")
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	if filepath.Base(outPath) != "payload.raw" {
		t.Fatalf("outPath basename = %s, want payload.raw", filepath.Base(outPath))
	}
	if m.Compression != "none" {
		t.Fatalf("Compression = %s, want none", m.Compression)
	}
	if m.RawBytes != int64(len(rawContent)) || m.CompressedBytes != int64(len(rawContent)) {
		t.Fatalf("unexpected sizes: raw=%d compressed=%d, want %d", m.RawBytes, m.CompressedBytes, len(rawContent))
	}
	if m.RawSha256 != m.CompressedSha256 {
		t.Fatalf("expected identical digests for uncompressed payload, got raw=%s compressed=%s", m.RawSha256, m.CompressedSha256)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestPayloadFileName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		algo        string
		want        string
		expectError bool
	}{
		{algo: "zstd", want: "payload.raw.zst"},
		{algo: "xz", want: "payload.raw.xz"},
		{algo: "gz", want: "payload.raw.gz"},
		{algo: "none", want: "payload.raw"},
		{algo: "bogus", expectError: true},
		{algo: "", expectError: true},
	}

	for _, tt := range tests {
		t.Run(tt.algo, func(t *testing.T) {
			t.Parallel()
			got, err := PayloadFileName(tt.algo)
			if tt.expectError {
				if err == nil {
					t.Fatalf("PayloadFileName(%q): expected error, got %q", tt.algo, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("PayloadFileName(%q): unexpected error: %v", tt.algo, err)
			}
			if got != tt.want {
				t.Fatalf("PayloadFileName(%q) = %q, want %q", tt.algo, got, tt.want)
			}
		})
	}
}

func TestPayloadFileName_MatchesCompressOutputBasename(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rawPath := filepath.Join(dir, "payload.raw")
	if err := os.WriteFile(rawPath, []byte("data"), 0600); err != nil {
		t.Fatalf("write fixture raw: %v", err)
	}

	for _, algo := range []string{"none"} {
		_, outPath, err := Compress(rawPath, t.TempDir(), algo)
		if err != nil {
			t.Fatalf("Compress(%q): %v", algo, err)
		}
		want, err := PayloadFileName(algo)
		if err != nil {
			t.Fatalf("PayloadFileName(%q): %v", algo, err)
		}
		if got := filepath.Base(outPath); got != want {
			t.Fatalf("Compress basename = %q, PayloadFileName = %q, want match", got, want)
		}
	}
}

func TestCompress_UnsupportedAlgo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rawPath := filepath.Join(dir, "payload.raw")
	if err := os.WriteFile(rawPath, []byte("data"), 0600); err != nil {
		t.Fatalf("write fixture raw: %v", err)
	}

	_, _, err := Compress(rawPath, t.TempDir(), "bogus")
	if err == nil || !strings.Contains(err.Error(), "unsupported payload compression") {
		t.Fatalf("expected unsupported-compression error, got: %v", err)
	}
}

func TestCompress_MissingSource(t *testing.T) {
	t.Parallel()
	_, _, err := Compress(filepath.Join(t.TempDir(), "missing.raw"), t.TempDir(), "none")
	if err == nil {
		t.Fatal("expected error compressing a missing source file")
	}
}

func TestCompress_Zstd(t *testing.T) {
	if _, err := exec.LookPath("zstd"); err != nil {
		t.Skip("zstd not available in this environment")
	}
	t.Parallel()

	dir := t.TempDir()
	rawPath := filepath.Join(dir, "payload.raw")
	rawContent := []byte(strings.Repeat("payload-bytes-", 1024))
	if err := os.WriteFile(rawPath, rawContent, 0600); err != nil {
		t.Fatalf("write fixture raw: %v", err)
	}

	dstDir := t.TempDir()
	m, outPath, err := Compress(rawPath, dstDir, "zstd")
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	if filepath.Base(outPath) != "payload.raw.zst" {
		t.Fatalf("outPath basename = %s, want payload.raw.zst", filepath.Base(outPath))
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("expected compressed output to exist: %v", err)
	}
	if m.RawBytes != int64(len(rawContent)) {
		t.Fatalf("RawBytes = %d, want %d", m.RawBytes, len(rawContent))
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}
