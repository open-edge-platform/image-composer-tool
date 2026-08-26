package debutils

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/ulikunitz/xz"
)

func Decompress(inFile string, outFile string) ([]string, error) {
	switch filepath.Ext(inFile) {
	case ".xz":
		return DecompressXZ(inFile, outFile)
	case ".gz":
		return DecompressGZ(inFile, outFile)
	}

	// An index with no compression suffix (a repository publishing only a plain
	// Packages) is already in its final form. Copying it to outFile would be a
	// no-op at best and truncate the input at worst, since callers derive outFile
	// by stripping the extension and so hand us the same path back.
	if inFile == outFile {
		return []string{outFile}, nil
	}
	if err := copyFileContents(inFile, outFile); err != nil {
		return nil, err
	}
	return []string{outFile}, nil
}

// copyFileContents copies src to dst, creating or truncating dst.
func copyFileContents(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", src, err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("failed to copy %s to %s: %w", src, dst, err)
	}
	return nil
}

func DecompressGZ(inFile string, outFile string) ([]string, error) {
	log := logger.Logger()

	gzFile, err := os.Open(inFile)
	if err != nil {
		log.Debugf("getting user packages failed: %v", err)
		return nil, fmt.Errorf("failed to open gz file: %w", err)
	}
	defer gzFile.Close()

	decompressedFile := outFile
	outDecompressed, err := os.Create(decompressedFile)
	if err != nil {
		return nil, fmt.Errorf("failed to create decompressed file: %v", err)
	}
	defer outDecompressed.Close()

	gzReader, err := gzip.NewReader(gzFile)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %v", err)
	}
	defer gzReader.Close()

	_, err = io.Copy(outDecompressed, gzReader)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress file: %v", err)
	}

	return []string{decompressedFile}, nil
}

func DecompressXZ(inFile string, outFile string) ([]string, error) {
	log := logger.Logger()

	xzFile, err := os.Open(inFile)
	if err != nil {
		log.Debugf("getting user packages failed: %v", err)
		return nil, fmt.Errorf("failed to open xz file: %w", err)
	}
	defer xzFile.Close()

	decompressedFile := outFile
	outDecompressed, err := os.Create(decompressedFile)
	if err != nil {
		return nil, fmt.Errorf("failed to create decompressed file: %v", err)
	}
	defer outDecompressed.Close()

	xzReader, err := xz.NewReader(xzFile)
	if err != nil {
		return nil, fmt.Errorf("failed to create xz reader: %v", err)
	}

	_, err = io.Copy(outDecompressed, xzReader)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress file: %v", err)
	}

	return []string{decompressedFile}, nil
}

func GetPackagesNames(baseURL string, codename string, arch string, component string) (string, error) {
	// if baseURL is a placeholder, dont process it
	if baseURL == "<URL>" || baseURL == "" {
		return "", nil
	}
	if cachedURL, ok := getPackageListURLFromCache(baseURL, codename, arch, component); ok {
		logger.Logger().Debugf("Using cached package list URL for %s/%s/%s/%s: %s", baseURL, codename, arch, component, cachedURL)
		return cachedURL, nil
	}
	// Ordered by preference: compressed indexes first, then the uncompressed
	// Packages. The plain file is what APT calls the last resort and some
	// repositories publish only that — packages.mozilla.org serves
	// dists/mozilla/main/binary-amd64/Packages and 404s both compressed names,
	// so omitting it makes an otherwise valid repository look unreachable.
	possibleFiles := []string{"Packages.gz", "Packages.xz", "Packages"}
	var foundFile string
	for _, fname := range possibleFiles {
		packageListURL := baseURL + "/dists/" + codename + "/" + component + "/binary-" + arch + "/" + fname
		fileExist, err := checkFileExists(packageListURL)
		if err != nil {
			return "", fmt.Errorf("error checking file existence at %s: %v", packageListURL, err)
		}
		if fileExist {
			foundFile = packageListURL
			savePackageListURLToCache(baseURL, codename, arch, component, packageListURL)
			break
		} else {
			logger.Logger().Debugf("Searching package list at: %s", packageListURL)
		}
	}
	return foundFile, nil
}
