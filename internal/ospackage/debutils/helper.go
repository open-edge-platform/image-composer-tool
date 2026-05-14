package debutils

import (
	"archive/tar"
	"archive/zip"
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

// DetectDebSuiteFromSourcesList parses a Debian sources.list file and returns the
// suite (e.g. "focal", "bookworm"). Falls back to "stable" when the suite cannot
// be determined.
func DetectDebSuiteFromSourcesList(sourcesListPath string) string {
	log := logger.Logger()
	const defaultSuite = "stable"

	content, err := os.ReadFile(sourcesListPath)
	if err != nil {
		log.Warnf("Failed to read local sources list %s, defaulting suite to %s: %v", sourcesListPath, defaultSuite, err)
		return defaultSuite
	}

	for _, rawLine := range strings.Split(string(content), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "deb" {
			continue
		}

		idx := 1
		if strings.HasPrefix(fields[idx], "[") {
			for idx < len(fields) && !strings.HasSuffix(fields[idx], "]") {
				idx++
			}
			idx++
		}

		if idx+1 < len(fields) {
			return fields[idx+1]
		}
	}

	log.Warnf("Could not determine suite from %s, defaulting to %s", sourcesListPath, defaultSuite)
	return defaultSuite
}

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

// PrepareLocalRepositoryFiles copies or downloads entries in packages into repoPath,
// extracting any .deb payloads from supported archives (.tar, .tar.gz, .tgz, .zip).
// Each entry is either an https:// URL (downloaded), a local directory (all .deb files inside
// are copied), or a local file path (copied/extracted).
func PrepareLocalRepositoryFiles(repoPath string, packages []string, insecureSkipVerify bool) error {
	if len(packages) == 0 {
		return nil
	}

	if repoPath == "" {
		return fmt.Errorf("repository path cannot be empty when packages are configured")
	}

	if err := os.MkdirAll(repoPath, 0755); err != nil {
		return fmt.Errorf("failed to create repository path %s: %w", repoPath, err)
	}

	tmpDir, err := os.MkdirTemp("", "ict-packages-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary directory for packages: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	for _, entry := range packages {
		var srcPath string
		if strings.HasPrefix(entry, "https://") {
			localName, err := filenameFromURL(entry)
			if err != nil {
				return fmt.Errorf("invalid packages URL %q: %w", entry, err)
			}
			downloadPath := filepath.Join(tmpDir, localName)
			if err := downloadOnlineFile(entry, downloadPath, insecureSkipVerify); err != nil {
				return fmt.Errorf("failed to download %q: %w", entry, err)
			}
			srcPath = downloadPath
		} else {
			if strings.Contains(entry, "://") {
				return fmt.Errorf("packages URL entry %q must use https scheme", entry)
			}
			info, err := os.Stat(entry)
			if err != nil {
				return fmt.Errorf("local path %q not found: %w", entry, err)
			}
			if info.IsDir() {
				if err := importDebsFromDir(entry, repoPath); err != nil {
					return fmt.Errorf("failed to process directory %q: %w", entry, err)
				}
				continue
			}
			srcPath = entry
		}

		if _, err := importOnlineFileToRepo(srcPath, repoPath); err != nil {
			return fmt.Errorf("failed to process %q: %w", entry, err)
		}
	}

	return nil
}

// importDebsFromDir copies all .deb files found directly inside srcDir into repoPath.
func importDebsFromDir(srcDir, repoPath string) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("failed to read directory: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(e.Name()), ".deb") {
			continue
		}
		src := filepath.Join(srcDir, e.Name())
		dst := filepath.Join(repoPath, e.Name())
		if err := copyFile(src, dst); err != nil {
			return fmt.Errorf("failed to copy %q: %w", e.Name(), err)
		}
	}
	return nil
}

func filenameFromURL(rawURL string) (string, error) {
	return network.FilenameFromURL(rawURL)
}

func downloadOnlineFile(fileURL, dstPath string, insecureSkipVerify bool) error {
	return network.DownloadFile(fileURL, dstPath, insecureSkipVerify)
}

func importOnlineFileToRepo(srcPath, repoPath string) (int, error) {
	lowerName := strings.ToLower(filepath.Base(srcPath))
	if strings.HasSuffix(lowerName, ".deb") {
		dstPath := filepath.Join(repoPath, filepath.Base(srcPath))
		if err := copyFile(srcPath, dstPath); err != nil {
			return 0, fmt.Errorf("failed to copy deb file into repository: %w", err)
		}
		return 1, nil
	}

	if strings.HasSuffix(lowerName, ".tar.gz") || strings.HasSuffix(lowerName, ".tgz") {
		f, err := os.Open(srcPath)
		if err != nil {
			return 0, fmt.Errorf("failed to open tar.gz file: %w", err)
		}
		defer f.Close()

		gzReader, err := gzip.NewReader(f)
		if err != nil {
			return 0, fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer gzReader.Close()

		return extractDebsFromTarReader(tar.NewReader(gzReader), repoPath)
	}

	if strings.HasSuffix(lowerName, ".tar") {
		f, err := os.Open(srcPath)
		if err != nil {
			return 0, fmt.Errorf("failed to open tar file: %w", err)
		}
		defer f.Close()

		return extractDebsFromTarReader(tar.NewReader(f), repoPath)
	}

	if strings.HasSuffix(lowerName, ".zip") {
		zipReader, err := zip.OpenReader(srcPath)
		if err != nil {
			return 0, fmt.Errorf("failed to open zip file: %w", err)
		}
		defer zipReader.Close()

		copied := 0
		for _, zipFile := range zipReader.File {
			if zipFile.FileInfo().IsDir() {
				continue
			}
			if !strings.HasSuffix(strings.ToLower(zipFile.Name), ".deb") {
				continue
			}

			fileName := filepath.Base(zipFile.Name)
			if fileName == "." || fileName == "/" || fileName == "" {
				continue
			}

			srcFile, err := zipFile.Open()
			if err != nil {
				return 0, fmt.Errorf("failed to read zip entry %s: %w", zipFile.Name, err)
			}

			dstPath := filepath.Join(repoPath, fileName)
			dstFile, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
			if err != nil {
				srcFile.Close()
				return 0, fmt.Errorf("failed to create output file %s: %w", dstPath, err)
			}

			_, copyErr := io.Copy(dstFile, srcFile)
			closeDstErr := dstFile.Close()
			closeSrcErr := srcFile.Close()
			if copyErr != nil {
				return 0, fmt.Errorf("failed to extract zip entry %s: %w", zipFile.Name, copyErr)
			}
			if closeDstErr != nil {
				return 0, fmt.Errorf("failed to close output file %s: %w", dstPath, closeDstErr)
			}
			if closeSrcErr != nil {
				return 0, fmt.Errorf("failed to close zip entry %s: %w", zipFile.Name, closeSrcErr)
			}

			copied++
		}
		if copied == 0 {
			return 0, fmt.Errorf("no .deb files found in zip archive %s", srcPath)
		}
		return copied, nil
	}

	return 0, fmt.Errorf("unsupported online file type %q (supported: .deb, .tar, .tar.gz, .tgz, .zip)", filepath.Base(srcPath))
}

func extractDebsFromTarReader(tarReader *tar.Reader, repoPath string) (int, error) {
	copied := 0

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("failed reading tar entry: %w", err)
		}

		if header.FileInfo().IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(header.Name), ".deb") {
			continue
		}

		fileName := filepath.Base(header.Name)
		if fileName == "." || fileName == "/" || fileName == "" {
			continue
		}

		dstPath := filepath.Join(repoPath, fileName)
		dstFile, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			return 0, fmt.Errorf("failed to create output file %s: %w", dstPath, err)
		}

		if _, err := io.Copy(dstFile, tarReader); err != nil {
			dstFile.Close()
			return 0, fmt.Errorf("failed to extract tar entry %s: %w", header.Name, err)
		}
		if err := dstFile.Close(); err != nil {
			return 0, fmt.Errorf("failed to close output file %s: %w", dstPath, err)
		}

		copied++
	}

	if copied == 0 {
		return 0, fmt.Errorf("no .deb files found in tar archive")
	}

	return copied, nil
}
