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
	if filepath.Ext(inFile) == ".xz" {
		return DecompressXZ(inFile, outFile)
	}
	return DecompressGZ(inFile, outFile)
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
	possibleFiles := []string{"Packages.gz", "Packages.xz"}
	var foundFile string
	for _, fname := range possibleFiles {
		packageListURL := baseURL + "/dists/" + codename + "/" + component + "/binary-" + arch + "/" + fname
		fileExist, err := checkFileExists(packageListURL)
		if err != nil {
			return "", fmt.Errorf("error checking file existence at %s: %v", packageListURL, err)
		}
		if fileExist {
			foundFile = packageListURL
			break
		} else {
			logger.Logger().Debugf("Searching package list at: %s", packageListURL)
		}
	}
	return foundFile, nil
}
