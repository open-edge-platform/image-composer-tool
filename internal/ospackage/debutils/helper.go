package debutils

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/network"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

// GenerateSPDXFileName creates a SPDX manifest filename based on repository configuration
func GenerateSPDXFileName(repoNm string) string {
	timestamp := time.Now().Format("20060102_150405")
	SPDXFileNm := filepath.Join("spdx_manifest_deb_" + strings.ReplaceAll(repoNm, " ", "_") + "_" + timestamp + ".json")
	return SPDXFileNm
}

// CreateTemporaryRepository creates a temporary Debian repository from a source directory containing .deb files.
// arch is the target architecture (e.g. "amd64", "arm64") used to create the binary-<arch> metadata directory.
// Returns: repository path, HTTP server URL, cleanup function, and error
func CreateTemporaryRepository(sourcePath, repoName, arch string) (repoPath, serverURL string, cleanup func(), err error) {
	log := logger.Logger()

	// Validate input path
	sourcePath, err = filepath.Abs(sourcePath)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to get absolute path of source directory: %w", err)
	}

	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", nil, fmt.Errorf("source directory does not exist: %s", sourcePath)
		}
		return "", "", nil, fmt.Errorf("failed to stat source directory %s: %w", sourcePath, err)
	}

	if !sourceInfo.IsDir() {
		return "", "", nil, fmt.Errorf("source path is not a directory: %s", sourcePath)
	}

	// Check if source contains DEB files
	pattern := filepath.Join(sourcePath, "*.deb")
	debFiles, err := filepath.Glob(pattern)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to search for DEB files in %s: %w", sourcePath, err)
	}
	if len(debFiles) == 0 {
		return "", "", nil, fmt.Errorf("no DEB files found in source directory: %s", sourcePath)
	}

	log.Infof("found %d DEB files in source directory: %s", len(debFiles), sourcePath)

	// Create temporary repository directory with Debian structure
	tempRepoPath, err := os.MkdirTemp("", fmt.Sprintf("debrepo_%s_*", repoName))
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to create temporary repository directory: %w", err)
	}

	// Create pool/main subdirectory for proper Debian repository structure
	poolPath := filepath.Join(tempRepoPath, "pool", "main")
	if err := os.MkdirAll(poolPath, 0755); err != nil {
		// Clean up on failure
		os.RemoveAll(tempRepoPath)
		return "", "", nil, fmt.Errorf("failed to create pool directory: %w", err)
	}

	// Create dists/stable/main/binary-<arch> subdirectory for metadata
	distsPath := filepath.Join(tempRepoPath, "dists", "stable", "main", "binary-"+arch)
	if err := os.MkdirAll(distsPath, 0755); err != nil {
		// Clean up on failure
		os.RemoveAll(tempRepoPath)
		return "", "", nil, fmt.Errorf("failed to create dists directory: %w", err)
	}

	log.Infof("created temporary repository directory: %s", tempRepoPath)

	// Copy all DEB files from source to pool/main directory without shelling out.
	for _, debFile := range debFiles {
		dstPath := filepath.Join(poolPath, filepath.Base(debFile))
		if err := copyFile(debFile, dstPath); err != nil {
			// Clean up on failure
			os.RemoveAll(tempRepoPath)
			return "", "", nil, fmt.Errorf("failed to copy DEB file %s to temporary repository: %w", debFile, err)
		}
	}

	log.Infof("copied DEB files from %s to %s", sourcePath, poolPath)

	// Generate Packages file using dpkg-scanpackages
	packagesPath := filepath.Join(distsPath, "Packages")
	// Use absolute paths for dpkg-scanpackages command
	poolRelativePath := "pool/main"
	scanPackagesCmd := fmt.Sprintf("cd %s && dpkg-scanpackages %s /dev/null > %s",
		tempRepoPath, poolRelativePath, packagesPath)

	output, err := shell.ExecCmd(scanPackagesCmd, false, shell.HostPath, nil)
	if err != nil {
		// Clean up on failure
		os.RemoveAll(tempRepoPath)
		return "", "", nil, fmt.Errorf("failed to create Packages file: %w", err)
	}

	log.Debugf("dpkg-scanpackages output: %s", output)

	// Verify Packages file was created
	if _, err := os.Stat(packagesPath); os.IsNotExist(err) {
		os.RemoveAll(tempRepoPath)
		return "", "", nil, fmt.Errorf("repository metadata was not created properly: missing %s", packagesPath)
	}

	// Gzip the Packages file to create Packages.gz (required by ParseRepositoryMetadata)
	packagesGzPath := packagesPath + ".gz"
	packagesData, readErr := os.ReadFile(packagesPath)
	if readErr != nil {
		os.RemoveAll(tempRepoPath)
		return "", "", nil, fmt.Errorf("failed to read Packages file: %w", readErr)
	}
	gzFile, createErr := os.Create(packagesGzPath)
	if createErr != nil {
		os.RemoveAll(tempRepoPath)
		return "", "", nil, fmt.Errorf("failed to create Packages.gz file: %w", createErr)
	}
	if gzipErr := func() (retErr error) {
		defer func() {
			if closeErr := gzFile.Close(); closeErr != nil && retErr == nil {
				retErr = fmt.Errorf("failed to close Packages.gz file: %w", closeErr)
			}
		}()

		gzWriter := gzip.NewWriter(gzFile)
		defer func() {
			if closeErr := gzWriter.Close(); closeErr != nil && retErr == nil {
				retErr = fmt.Errorf("failed to finalize Packages.gz: %w", closeErr)
			}
		}()

		if _, writeErr := gzWriter.Write(packagesData); writeErr != nil {
			return fmt.Errorf("failed to write Packages.gz: %w", writeErr)
		}

		return nil
	}(); gzipErr != nil {
		os.RemoveAll(tempRepoPath)
		return "", "", nil, gzipErr
	}

	// Compute SHA256 checksums and file sizes for the Release file
	packagesHash, hashErr := computeFileSHA256(packagesPath)
	if hashErr != nil {
		os.RemoveAll(tempRepoPath)
		return "", "", nil, fmt.Errorf("failed to compute Packages checksum: %w", hashErr)
	}
	packagesGzHash, gzHashErr := computeFileSHA256(packagesGzPath)
	if gzHashErr != nil {
		os.RemoveAll(tempRepoPath)
		return "", "", nil, fmt.Errorf("failed to compute Packages.gz checksum: %w", gzHashErr)
	}
	packagesStat, statErr := os.Stat(packagesPath)
	if statErr != nil {
		os.RemoveAll(tempRepoPath)
		return "", "", nil, fmt.Errorf("failed to stat Packages file: %w", statErr)
	}
	packagesGzStat, gzStatErr := os.Stat(packagesGzPath)
	if gzStatErr != nil {
		os.RemoveAll(tempRepoPath)
		return "", "", nil, fmt.Errorf("failed to stat Packages.gz file: %w", gzStatErr)
	}

	// Create Release file with SHA256 checksums so VerifyPackagegz can validate the download
	releasePath := filepath.Join(tempRepoPath, "dists", "stable", "Release")
	releaseContent := fmt.Sprintf("Suite: stable\nCodename: stable\nComponents: main\nArchitectures: %s\nDate: %s\nSHA256:\n %s %d main/binary-%s/Packages\n %s %d main/binary-%s/Packages.gz\n",
		arch,
		time.Now().UTC().Format("Mon, 02 Jan 2006 15:04:05 MST"),
		packagesHash, packagesStat.Size(), arch,
		packagesGzHash, packagesGzStat.Size(), arch,
	)

	if err := os.WriteFile(releasePath, []byte(releaseContent), 0644); err != nil {
		// Clean up on failure
		os.RemoveAll(tempRepoPath)
		return "", "", nil, fmt.Errorf("failed to create Release file: %w", err)
	}

	log.Infof("generated repository metadata with checksums for %s", tempRepoPath)

	// Start HTTP server to serve the repository
	serverURL, serverCleanup, err := network.ServeRepositoryHTTP(tempRepoPath)
	if err != nil {
		// Clean up repository if server fails
		os.RemoveAll(tempRepoPath)
		return "", "", nil, fmt.Errorf("failed to serve repository via HTTP: %w", err)
	}

	// Combined cleanup function
	cleanup = func() {
		serverCleanup()            // Stop HTTP server first
		os.RemoveAll(tempRepoPath) // Then remove repository directory
	}

	// Verify HTTP server is working by fetching Packages.gz
	packagesGzURL := serverURL + "/dists/stable/main/binary-" + arch + "/Packages.gz"
	log.Infof("verifying HTTP server by fetching: %s", packagesGzURL)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(packagesGzURL)
	if err != nil {
		// Clean up if verification fails
		cleanup()
		return "", "", nil, fmt.Errorf("failed to verify HTTP server - could not fetch Packages.gz: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		// Clean up if verification fails
		cleanup()
		return "", "", nil, fmt.Errorf("failed to verify HTTP server - Packages.gz returned status %d", resp.StatusCode)
	}

	log.Infof("HTTP server verification successful - Packages.gz accessible at %s", packagesGzURL)
	log.Infof("successfully created and serving temporary DEB repository: %s", tempRepoPath)

	return tempRepoPath, serverURL, cleanup, nil
}

func copyFile(srcPath, dstPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open source file %s: %w", srcPath, err)
	}
	defer src.Close()

	srcInfo, err := src.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat source file %s: %w", srcPath, err)
	}

	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return fmt.Errorf("failed to create destination file %s: %w", dstPath, err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("failed to copy file data from %s to %s: %w", srcPath, dstPath, err)
	}

	return nil
}
