package isopayload

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/compression"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/file"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
)

var log = logger.Logger()

// compressionExt maps a payload compression algorithm to the file extension
// appended to "payload.raw" when staging it onto the ISO. Its key set is also
// the set of algorithms a payload manifest may declare; "none" ships the raw
// image uncompressed (no CompressFile call, no decompressor required in the
// installer initrd).
var compressionExt = map[string]string{
	"zstd": "zst",
	"xz":   "xz",
	"gz":   "gz",
	"none": "",
}

// PayloadFileName returns the basename Compress writes for algo ("payload.raw"
// or "payload.raw.<ext>"), so a caller holding only a manifest's Compression
// field (e.g. live-installer, reading the manifest back on a booted ISO) can
// locate the payload file without reaching into the unexported compressionExt
// map.
func PayloadFileName(algo string) (string, error) {
	ext, ok := compressionExt[algo]
	if !ok {
		return "", fmt.Errorf("unsupported payload compression %q: must be one of zstd, xz, gz, none", algo)
	}
	if ext == "" {
		return "payload.raw", nil
	}
	return "payload.raw." + ext, nil
}

// Compress compresses the raw image at srcRaw with algo into dstDir, writing
// "payload.raw.<ext>" (or a plain copy named "payload.raw" for algo "none"),
// and returns the resulting Manifest with both raw and compressed digests and
// sizes populated. The caller is responsible for writing the manifest to the
// ISO tree via Write.
func Compress(srcRaw, dstDir, algo string) (Manifest, string, error) {
	var m Manifest

	outName, err := PayloadFileName(algo)
	if err != nil {
		return m, "", err
	}

	rawInfo, err := os.Stat(srcRaw)
	if err != nil {
		return m, "", fmt.Errorf("stat raw payload %s: %w", srcRaw, err)
	}

	rawSha256, err := FileSHA256(srcRaw)
	if err != nil {
		return m, "", fmt.Errorf("checksum raw payload: %w", err)
	}

	outPath := filepath.Join(dstDir, outName)

	if algo == "none" {
		if err := file.CopyFile(srcRaw, outPath, "", false); err != nil {
			return m, "", fmt.Errorf("copy raw payload: %w", err)
		}
	} else {
		log.Infof("Compressing installer payload %s -> %s (%s)", srcRaw, outPath, algo)
		if err := compression.CompressFile(srcRaw, outPath, algo, false); err != nil {
			return m, "", fmt.Errorf("compress raw payload with %s: %w", algo, err)
		}
	}

	// Always verify against the file actually staged at outPath, even for
	// "none": this is the only build-time check that the exact bytes shipped
	// on the ISO match what the manifest claims. Aliasing this from the
	// pre-copy source digest would let a corrupted copy go undetected until
	// live-installer's own checksum gate runs against real target hardware.
	compressedInfo, err := os.Stat(outPath)
	if err != nil {
		return m, "", fmt.Errorf("stat compressed payload %s: %w", outPath, err)
	}
	compressedSha256, err := FileSHA256(outPath)
	if err != nil {
		return m, "", fmt.Errorf("checksum compressed payload: %w", err)
	}

	m = Manifest{
		SchemaVersion:    ManifestSchemaVersion,
		Compression:      algo,
		CompressedSha256: compressedSha256,
		CompressedBytes:  compressedInfo.Size(),
		RawSha256:        rawSha256,
		RawBytes:         rawInfo.Size(),
	}
	return m, outPath, nil
}
