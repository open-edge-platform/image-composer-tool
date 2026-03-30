package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/open-edge-platform/os-image-composer/internal/utils/logger"
	"github.com/open-edge-platform/os-image-composer/internal/utils/network"
)

// GenerateAptSourcesFromRepositories creates an apt sources file from packageRepositories
// and adds it to the image's additionalFiles configuration
func (t *ImageTemplate) GenerateAptSourcesFromRepositories() error {
	log := logger.Logger()

	// Only process if we have package repositories and it's a DEB-based system
	if !t.HasPackageRepositories() {
		log.Debug("No package repositories configured, skipping apt sources generation")
		return nil
	}

	// Check if this is a DEB-based system (ubuntu, elxr)
	if !isDEBBasedTarget(t.Target.OS) {
		log.Debug("Not a DEB-based system, skipping apt sources generation")
		return nil
	}

	log.Infof("Generating apt sources file from %d package repositories", len(t.PackageRepositories))

	// Normalize repository priorities (set default 500 if not specified)
	normalizedRepos := normalizeRepositoryPriorities(t.PackageRepositories)
	t.PackageRepositories = normalizedRepos

	// Generate apt sources content
	sourceContent := generateAptSourcesContent(normalizedRepos)
	if sourceContent == "" {
		log.Debug("No valid repositories to generate apt sources")
		return nil
	}

	// Create temporary apt sources file
	tempFile, err := createTempAptSourcesFile(sourceContent)
	if err != nil {
		return fmt.Errorf("failed to create temporary apt sources file: %w", err)
	}

	// Add to additionalFiles so it gets copied to the image
	aptSourcesFile := AdditionalFileInfo{
		Local: tempFile,
		Final: "/etc/apt/sources.list.d/package-repositories.list",
	}

	// Add to existing additionalFiles (avoiding duplicates by final path)
	t.addUniqueAdditionalFile(aptSourcesFile)

	log.Infof("Added apt sources file to additionalFiles: %s -> %s",
		aptSourcesFile.Local, aptSourcesFile.Final)

	// Download and add GPG keys to the image
	if err := t.downloadAndAddGPGKeys(normalizedRepos); err != nil {
		return fmt.Errorf("failed to download and add GPG keys: %w", err)
	}

	// Generate APT preferences files for repositories with priorities
	if err := t.generateAptPreferencesFromRepositories(); err != nil {
		return fmt.Errorf("failed to generate apt preferences from repositories: %w", err)
	}

	return nil
}

// isDEBBasedTarget checks if the target OS uses DEB packages
func isDEBBasedTarget(targetOS string) bool {
	debOSes := []string{"ubuntu", "elxr", "wind-river-elxr"}
	for _, os := range debOSes {
		if targetOS == os {
			return true
		}
	}
	return false
}

// normalizeRepositoryPriorities sets default priority of 500 for repositories without explicit priority
func normalizeRepositoryPriorities(repos []PackageRepository) []PackageRepository {
	log := logger.Logger()
	normalizedRepos := make([]PackageRepository, len(repos))

	for i, repo := range repos {
		normalizedRepos[i] = repo

		// Set default priority of 500 if not specified (priority == 0)
		if repo.Priority == 0 {
			normalizedRepos[i].Priority = 500
			log.Debugf("Repository %s: setting default priority 500", getRepositoryName(repo))
		} else {
			log.Debugf("Repository %s: using explicit priority %d", getRepositoryName(repo), repo.Priority)
		}
	}

	return normalizedRepos
}

// getRepositoryName returns a human-readable name for the repository
func getRepositoryName(repo PackageRepository) string {
	if repo.ID != "" {
		return repo.ID
	}
	if repo.Codename != "" {
		return repo.Codename
	}
	return repo.URL
}

// generateAptSourcesContent creates apt sources.list content from PackageRepository slice
// Following ubuntu-noble.list format: simple deb lines without signed-by directives
func generateAptSourcesContent(repos []PackageRepository) string {
	var sources []string

	for _, repo := range repos {
		// Skip if essential fields are missing
		if repo.URL == "" || repo.Codename == "" {
			continue
		}

		// Default component if not specified
		component := repo.Component
		if component == "" {
			component = "main"
		}

		// Create the deb line in ubuntu-noble.list format (no signed-by directive)
		debLine := fmt.Sprintf("deb %s %s %s", repo.URL, repo.Codename, component)
		sources = append(sources, debLine)
	}

	return strings.Join(sources, "\n")
}

// extractGPGKeyFilename extracts a reasonable filename for GPG key storage
func extractGPGKeyFilename(keyURL string) string {
	// Extract filename from URL first
	parts := strings.Split(keyURL, "/")
	if len(parts) > 0 {
		filename := parts[len(parts)-1]
		// Ensure it has .gpg extension
		if !strings.HasSuffix(filename, ".gpg") {
			filename = strings.TrimSuffix(filename, ".asc") + ".gpg"
		}
		return fmt.Sprintf("/etc/apt/trusted.gpg.d/%s", filename)
	}

	// For common patterns, provide reasonable defaults
	if strings.Contains(keyURL, "intel") {
		return "/etc/apt/trusted.gpg.d/intel-archive-keyring.gpg"
	}

	// Default fallback
	return "/etc/apt/trusted.gpg.d/package-repository.gpg"
}

// createTempAptSourcesFile creates a temporary file with the apt sources content
func createTempAptSourcesFile(content string) (string, error) {
	// Ensure temp directory exists
	tempDir := TempDir()
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create temp directory %s: %w", tempDir, err)
	}

	// Create temporary file using proper temp directory
	tempFile, err := os.CreateTemp(tempDir, "package-repositories-*.list")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary apt sources file: %w", err)
	}
	defer tempFile.Close()

	// Write content to temp file
	if _, err := tempFile.WriteString(content); err != nil {
		return "", fmt.Errorf("failed to write apt sources file: %w", err)
	}

	// Return relative path from default config location to tmp directory
	return getRelativePathFromDefaultConfig(tempFile.Name()), nil
}

// addUniqueAdditionalFile adds an additional file if it doesn't already exist (by Final path)
func (t *ImageTemplate) addUniqueAdditionalFile(newFile AdditionalFileInfo) {
	// Check if file with same final path already exists
	for i, existingFile := range t.SystemConfig.AdditionalFiles {
		if existingFile.Final == newFile.Final {
			// Replace existing file
			t.SystemConfig.AdditionalFiles[i] = newFile
			return
		}
	}

	// Add new file
	t.SystemConfig.AdditionalFiles = append(t.SystemConfig.AdditionalFiles, newFile)
}

// generateAptPreferencesFromRepositories creates APT preferences files for all repositories
func (t *ImageTemplate) generateAptPreferencesFromRepositories() error {
	log := logger.Logger()

	log.Infof("Generating apt preferences files for %d repositories", len(t.PackageRepositories))

	for _, repo := range t.PackageRepositories {

		// Extract origin from URL
		origin := extractOriginFromURL(repo.URL)
		if origin == "" {
			log.Warnf("Could not extract origin from URL %s, skipping preferences for repository %s", repo.URL, repo.ID)
			continue
		}

		// Generate preferences content
		preferencesContent := generateAptPreferencesContent(origin, repo.Priority)

		// Create temporary preferences file
		tempFile, err := createTempAptPreferencesFile(repo, preferencesContent)
		if err != nil {
			return fmt.Errorf("failed to create temporary apt preferences file for %s: %w", repo.ID, err)
		}

		// Determine filename for preferences
		filename := generatePreferencesFilename(repo)

		// Add to additionalFiles
		preferencesFile := AdditionalFileInfo{
			Local: tempFile,
			Final: fmt.Sprintf("/etc/apt/preferences.d/%s", filename),
		}

		t.addUniqueAdditionalFile(preferencesFile)

		log.Infof("Added apt preferences file for %s (priority %d): %s -> %s",
			repo.ID, repo.Priority, preferencesFile.Local, preferencesFile.Final)
	}

	return nil
}

// extractOriginFromURL extracts the domain/origin from a repository URL
func extractOriginFromURL(url string) string {
	// Remove protocol
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")

	// Extract domain (everything before the first slash)
	parts := strings.Split(url, "/")
	if len(parts) > 0 && parts[0] != "" {
		return parts[0]
	}

	return ""
}

// generateAptPreferencesContent creates APT preferences file content with priority behavior comments
func generateAptPreferencesContent(origin string, priority int) string {
	var comment string

	// Add priority behavior comment based on the priority value
	switch {
	case priority > 1000:
		comment = "# Priority >1000: Force install even downgrade"
	case priority == 1000:
		comment = "# Priority 1000: Install even if version is lower than installed"
	case priority == 990:
		comment = "# Priority 990: Preferred"
	case priority == 500:
		comment = "# Priority 500: Default"
	case priority < 0:
		comment = "# Priority <0: Never install"
	default:
		comment = fmt.Sprintf("# Priority %d: Custom priority", priority)
	}

	return fmt.Sprintf("%s\nPackage: *\nPin: origin %s\nPin-Priority: %d\n", comment, origin, priority)
}

// generatePreferencesFilename creates a filename for the preferences file
func generatePreferencesFilename(repo PackageRepository) string {
	// Use repository ID if available
	if repo.ID != "" {
		return sanitizeFilename(repo.ID)
	}

	// Create a unique name using codename and URL hash to avoid conflicts
	name := repo.Codename
	if name == "" {
		name = "repository"
	}

	// Extract a unique identifier from the URL
	urlPart := extractOriginFromURL(repo.URL)
	if urlPart != "" && urlPart != name {
		// Combine codename with URL part for uniqueness
		name = fmt.Sprintf("%s-%s", name, urlPart)
	}

	return sanitizeFilename(name)
}

// sanitizeFilename removes invalid characters from a filename
func sanitizeFilename(name string) string {
	// Replace invalid characters
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, ":", "-")
	name = strings.ReplaceAll(name, ".", "-")
	name = strings.ToLower(name)

	// Ensure it's a valid filename
	if name == "" {
		name = "repository"
	}

	return name
}

// createTempAptPreferencesFile creates a temporary file with the preferences content
func createTempAptPreferencesFile(repo PackageRepository, content string) (string, error) {
	// Ensure temp directory exists
	tempDir := TempDir()
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create temp directory %s: %w", tempDir, err)
	}

	// Create filename pattern based on repository
	pattern := fmt.Sprintf("apt-preferences-%s-*.pref", generatePreferencesFilename(repo))

	// Create temporary file using proper temp directory
	tempFile, err := os.CreateTemp(tempDir, pattern)
	if err != nil {
		return "", fmt.Errorf("failed to create temporary apt preferences file for %s: %w", getRepositoryName(repo), err)
	}
	defer tempFile.Close()

	// Write content to temp file
	if _, err := tempFile.WriteString(content); err != nil {
		return "", fmt.Errorf("failed to write apt preferences file: %w", err)
	}

	// Return relative path from default config location to tmp directory
	return getRelativePathFromDefaultConfig(tempFile.Name()), nil
}

// downloadAndAddGPGKeys downloads GPG keys from repository URLs and adds them to additionalFiles
func (t *ImageTemplate) downloadAndAddGPGKeys(repos []PackageRepository) error {
	log := logger.Logger()

	for _, repo := range repos {
		// Skip if no GPG key URL is specified
		if repo.PKey == "" {
			log.Debugf("Repository %s has no GPG key URL, skipping", getRepositoryName(repo))
			continue
		}

		// Skip placeholder URLs
		if repo.PKey == "<PUBLIC_KEY_URL>" {
			log.Debugf("Repository %s has placeholder GPG key URL, skipping", getRepositoryName(repo))
			continue
		}

		// Skip [trusted=yes] marker - no key to download
		if repo.PKey == "[trusted=yes]" {
			log.Debugf("Repository %s marked as [trusted=yes], skipping GPG key download", getRepositoryName(repo))
			continue
		}

		// Check if pkey is a local file path (like pbGPGKey in provider configs)
		isLocalFilePath := !strings.HasPrefix(repo.PKey, "http://") &&
			!strings.HasPrefix(repo.PKey, "https://") &&
			!strings.HasPrefix(repo.PKey, "file://") &&
			strings.HasPrefix(repo.PKey, "/")

		var keyData []byte
		var tempKeyFile string
		var err error

		if isLocalFilePath {
			// For local file paths, read the file directly from the host system
			log.Infof("Using local GPG key file for repository %s: %s", getRepositoryName(repo), repo.PKey)
			keyData, err = os.ReadFile(repo.PKey)
			if err != nil {
				return fmt.Errorf("failed to read local GPG key from %s: %w", repo.PKey, err)
			}
		} else {
			// For URLs, prefer persistent cache and download only on cache miss
			keyData, err = getCachedOrDownloadGPGKey(repo.PKey)
			if err != nil {
				return fmt.Errorf("failed to download GPG key from %s: %w", repo.PKey, err)
			}
		}

		// Dearmor the GPG key to convert ASCII-armored keys to binary format
		log.Debugf("Dearmoring GPG key for repository %s", getRepositoryName(repo))
		dearmoredKeyData, err := dearmorGPGKey(keyData)
		if err != nil {
			log.Warnf("Failed to dearmor GPG key for %s, using original key data: %v", getRepositoryName(repo), err)
			// If dearmoring fails, use the original key data (might already be in binary format)
			dearmoredKeyData = keyData
		}

		// Create temporary file for the GPG key
		tempKeyFile, err = createTempGPGKeyFile(repo.PKey, dearmoredKeyData)
		if err != nil {
			return fmt.Errorf("failed to create temp GPG key file: %w", err)
		}

		// Determine the final destination path in the image
		keyFilename := extractGPGKeyFilename(repo.PKey)

		// Add to additionalFiles
		gpgKeyFile := AdditionalFileInfo{
			Local: tempKeyFile,
			Final: keyFilename,
		}

		t.addUniqueAdditionalFile(gpgKeyFile)

		log.Infof("Added GPG key file to additionalFiles: %s -> %s", tempKeyFile, keyFilename)
	}

	return nil
}

// gpgKeyCacheFilePath returns the persistent cache path for a repository GPG key URL.
func gpgKeyCacheFilePath(keyURL string) (string, error) {
	cacheDir, err := CacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve cache directory: %w", err)
	}

	gpgCacheDir := filepath.Join(cacheDir, "gpg-keys")
	if err := os.MkdirAll(gpgCacheDir, 0755); err != nil {
		return "", fmt.Errorf("create GPG cache directory %s: %w", gpgCacheDir, err)
	}

	hash := sha256.Sum256([]byte(keyURL))
	hashHex := hex.EncodeToString(hash[:])

	return filepath.Join(gpgCacheDir, fmt.Sprintf("%s.gpg", hashHex)), nil
}

// getCachedOrDownloadGPGKey loads a key from persistent cache, downloading only on cache miss.
func getCachedOrDownloadGPGKey(keyURL string) ([]byte, error) {
	log := logger.Logger()

	cacheFilePath, err := gpgKeyCacheFilePath(keyURL)
	if err == nil {
		if cachedData, readErr := os.ReadFile(cacheFilePath); readErr == nil {
			log.Infof("Using cached GPG key (%d bytes) for %s", len(cachedData), keyURL)
			return cachedData, nil
		}
	} else {
		// Cache path failures should not block build; fall back to direct download.
		log.Warnf("Failed to initialize GPG key cache for %s: %v", keyURL, err)
	}

	log.Infof("Downloading GPG key from %s", keyURL)
	keyData, err := downloadGPGKey(keyURL)
	if err != nil {
		return nil, err
	}

	if cacheFilePath == "" {
		return keyData, nil
	}

	if writeErr := os.WriteFile(cacheFilePath, keyData, 0644); writeErr != nil {
		log.Warnf("Failed to persist GPG key cache for %s at %s: %v", keyURL, cacheFilePath, writeErr)
		return keyData, nil
	}

	log.Infof("Cached GPG key at %s", cacheFilePath)
	return keyData, nil
}

// downloadGPGKey downloads a GPG key from the given URL
func downloadGPGKey(keyURL string) ([]byte, error) {
	log := logger.Logger()

	client := network.NewSecureHTTPClient()

	resp, err := client.Get(keyURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch GPG key from %s: %w", keyURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download GPG key from %s: HTTP status %d", keyURL, resp.StatusCode)
	}

	keyData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read GPG key data from %s: %w", keyURL, err)
	}

	log.Infof("Successfully downloaded GPG key (%d bytes) from %s", len(keyData), keyURL)

	return keyData, nil
}

// dearmorGPGKey converts ASCII-armored GPG key to binary format using gpg --dearmor
func dearmorGPGKey(keyData []byte) ([]byte, error) {
	log := logger.Logger()

	// Check if gpg is available
	gpgPath, err := exec.LookPath("gpg")
	if err != nil {
		return nil, fmt.Errorf("gpg command not found: %w", err)
	}

	// Create command to dearmor the key
	cmd := exec.Command(gpgPath, "--dearmor")

	// Set up pipes for stdin and stdout
	cmd.Stdin = bytes.NewReader(keyData)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run the command
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gpg --dearmor failed: %w (stderr: %s)", err, stderr.String())
	}

	dearmoredData := stdout.Bytes()
	log.Debugf("Successfully dearmored GPG key: %d bytes -> %d bytes", len(keyData), len(dearmoredData))

	return dearmoredData, nil
}

// getRelativePathFromDefaultConfig converts an absolute temp file path to a relative path
// from the default config directory (config/osv/{os}/{dist}/imageconfigs/defaultconfigs/)
func getRelativePathFromDefaultConfig(absPath string) string {
	// Get absolute path if not already absolute
	if !filepath.IsAbs(absPath) {
		var err error
		absPath, err = filepath.Abs(absPath)
		if err != nil {
			// Fallback to returning the path as-is
			return absPath
		}
	}

	// Get the base filename from the absolute path
	filename := filepath.Base(absPath)

	// Default configs are at: config/osv/{os}/{dist}/imageconfigs/defaultconfigs/
	// Tmp directory is at root: tmp/
	// Relative path from defaultconfigs to tmp: ../../../../../../tmp/
	return filepath.Join("..", "..", "..", "..", "..", "..", "tmp", filename)
}

// createTempGPGKeyFile creates a temporary file with the GPG key content
func createTempGPGKeyFile(keyURL string, keyData []byte) (string, error) {
	// Ensure temp directory exists
	tempDir := TempDir()
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create temp directory %s: %w", tempDir, err)
	}

	// Extract key filename from URL for pattern
	parts := strings.Split(keyURL, "/")
	keyName := "gpg-key"
	if len(parts) > 0 {
		keyName = strings.ReplaceAll(parts[len(parts)-1], ".", "-")
	}

	// Create temporary file
	tempFile, err := os.CreateTemp(tempDir, fmt.Sprintf("%s-*.gpg", keyName))
	if err != nil {
		return "", fmt.Errorf("failed to create temporary GPG key file: %w", err)
	}
	defer tempFile.Close()

	// Write key data to temp file
	if _, err := tempFile.Write(keyData); err != nil {
		return "", fmt.Errorf("failed to write GPG key file: %w", err)
	}

	// Set permissions to 0644 so all users can read the GPG key
	if err := os.Chmod(tempFile.Name(), 0644); err != nil {
		return "", fmt.Errorf("failed to set permissions on GPG key file: %w", err)
	}

	// Return relative path from default config location to tmp directory
	return getRelativePathFromDefaultConfig(tempFile.Name()), nil
}
