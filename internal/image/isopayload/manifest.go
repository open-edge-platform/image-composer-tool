// Package isopayload defines the on-ISO payload manifest and compression
// helpers shared by isomaker (which writes them) and live-installer (which
// reads them) when systemConfig.installerPayload.enabled is true.
package isopayload

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"regexp"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/security"
	"gopkg.in/yaml.v3"
)

// ManifestSchemaVersion is the schema version emitted into every manifest.
// Bump this whenever a field is added or removed so live-installer can
// detect an incompatible manifest from an older/newer isomaker.
const ManifestSchemaVersion = "1"

// Manifest describes the compressed raw payload staged onto an installer
// ISO. live-installer loads it, verifies CompressedSha256 against the
// on-disk payload before touching the target disk, decompresses the payload
// while streaming it onto the target block device, and uses RawBytes to size
// the post-write GPT/filesystem growth.
type Manifest struct {
	SchemaVersion    string `yaml:"schemaVersion"`
	Compression      string `yaml:"compression"`
	CompressedSha256 string `yaml:"compressedSha256"`
	CompressedBytes  int64  `yaml:"compressedBytes"`
	RawSha256        string `yaml:"rawSha256"`
	RawBytes         int64  `yaml:"rawBytes"`
}

var sha256HexPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Write marshals m as YAML and writes it to path with symlink protection,
// rejecting a destination that resolves through a symlink.
func Write(m Manifest, path string) error {
	data, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal payload manifest: %w", err)
	}
	if err := security.SafeWriteFile(path, data, 0644, security.RejectSymlinks); err != nil {
		return fmt.Errorf("write payload manifest %s: %w", path, err)
	}
	return nil
}

// Load reads and parses the manifest at path with symlink protection.
func Load(path string) (Manifest, error) {
	var m Manifest
	data, err := security.SafeReadFile(path, security.RejectSymlinks)
	if err != nil {
		return m, fmt.Errorf("read payload manifest %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &m); err != nil {
		return m, fmt.Errorf("parse payload manifest %s: %w", path, err)
	}
	return m, nil
}

// Validate reports whether m is structurally usable: known schema version,
// known compression algorithm, well-formed sha256 hex digests, and positive
// sizes. It does not touch the filesystem — that is CompressedSha256's job,
// checked by the caller against the actual on-disk payload before any byte
// reaches the target device.
func (m Manifest) Validate() error {
	if m.SchemaVersion != ManifestSchemaVersion {
		return fmt.Errorf("unsupported payload manifest schemaVersion %q (expected %q)", m.SchemaVersion, ManifestSchemaVersion)
	}
	if _, ok := compressionExt[m.Compression]; !ok {
		return fmt.Errorf("unsupported payload compression %q", m.Compression)
	}
	if !sha256HexPattern.MatchString(m.CompressedSha256) {
		return fmt.Errorf("invalid compressedSha256 %q: must be 64 lowercase hex characters", m.CompressedSha256)
	}
	if !sha256HexPattern.MatchString(m.RawSha256) {
		return fmt.Errorf("invalid rawSha256 %q: must be 64 lowercase hex characters", m.RawSha256)
	}
	if m.CompressedBytes <= 0 {
		return fmt.Errorf("compressedBytes must be positive, got %d", m.CompressedBytes)
	}
	if m.RawBytes <= 0 {
		return fmt.Errorf("rawBytes must be positive, got %d", m.RawBytes)
	}
	return nil
}

// FileSHA256 computes the lowercase hex sha256 digest of the file at path.
// It is used both when isomaker builds the manifest and when live-installer
// verifies the on-ISO payload against CompressedSha256 before writing to disk.
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s for checksum: %w", path, err)
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}
