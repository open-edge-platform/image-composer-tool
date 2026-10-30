package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/network"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
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

	// Check if this is a DEB-based system (ubuntu, debian, elxr)
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
	hasOwnFiles := slices.ContainsFunc(normalizedRepos, PackageRepository.hasOwnFiles)
	if sourceContent == "" && !hasOwnFiles {
		log.Debug("No valid repositories to generate apt sources")
		return nil
	}

	if sourceContent != "" {
		if err := t.addAptSourcesFile(sourceContent); err != nil {
			return err
		}
	}

	if err := t.addOwnSourcesListFiles(normalizedRepos); err != nil {
		return err
	}

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

// addAptSourcesFile adds the shared sources list of the template's
// repositories to additionalFiles.
func (t *ImageTemplate) addAptSourcesFile(content string) error {
	tempFile, err := createTempAptSourcesFile(content)
	if err != nil {
		return fmt.Errorf("failed to create temporary apt sources file: %w", err)
	}
	aptSourcesFile := AdditionalFileInfo{
		Local: tempFile,
		Final: "/etc/apt/sources.list.d/package-repositories.list",
	}
	if err := t.addGeneratedAdditionalFile(aptSourcesFile); err != nil {
		return err
	}
	logger.Logger().Infof("Added apt sources file to additionalFiles: %s -> %s",
		aptSourcesFile.Local, aptSourcesFile.Final)
	return nil
}

// addOwnSourcesListFiles adds the sources list of each repository that names
// its own list file.
func (t *ImageTemplate) addOwnSourcesListFiles(repos []PackageRepository) error {
	for _, repo := range repos {
		if !repo.hasOwnFiles() {
			continue
		}
		tempFile, err := createTempAptSourcesFile(strings.Join(repo.SourceLines, "\n") + "\n")
		if err != nil {
			return fmt.Errorf("failed to create sources list for %s: %w", getRepositoryName(repo), err)
		}
		if err := t.addGeneratedAdditionalFile(AdditionalFileInfo{Local: tempFile, Final: repo.ListFile}); err != nil {
			return err
		}
	}
	return nil
}

// isDEBBasedTarget checks if the target OS uses DEB packages
func isDEBBasedTarget(targetOS string) bool {
	debOSes := []string{"ubuntu", "debian", "elxr", "wind-river-elxr"}
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
		// Skip if essential fields are missing, or the repository has its own list file
		if repo.URL == "" || repo.Codename == "" || repo.hasOwnFiles() {
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

// gpgFilenameUnsafeRe matches characters that are unsafe or merely untidy in
// a keyring filename (keyserver URLs in particular carry a query string, e.g.
// "?op=get&search=0x...", which otherwise ends up verbatim in the filename).
var gpgFilenameUnsafeRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// extractGPGKeyFilename extracts a reasonable filename for GPG key storage.
// The query string (if any) is dropped from the candidate name and, for
// keyserver "pks/lookup" URLs, replaced with the requested key ID/fingerprint
// so the name stays both clean and unique: two repositories resolving to the
// same last path segment (e.g. every keyserver.ubuntu.com key is
// ".../pks/lookup") must not collide on the same destination filename, which
// would silently make the later one overwrite the earlier one's key file.
func extractGPGKeyFilename(keyURL string) string {
	candidate, needsDisambiguation := gpgKeyFilenameCandidate(keyURL)
	if candidate == "" {
		// No usable path segment (e.g. a bare query string on the root
		// path, as a keyserver URL can be): still disambiguate below if
		// needed, so two such URLs can't collide on this generic name.
		if strings.Contains(keyURL, "intel") {
			candidate = "intel-archive-keyring"
		} else {
			candidate = "package-repository"
		}
	}

	if !strings.HasSuffix(candidate, ".gpg") {
		candidate = strings.TrimSuffix(candidate, ".asc") + ".gpg"
	}
	base := strings.TrimSuffix(candidate, ".gpg")
	sanitizedBase := gpgFilenameUnsafeRe.ReplaceAllString(base, "-")
	if sanitizedBase != base {
		// Sanitization alone can introduce a collision even without a query
		// string, e.g. "repo+signing.gpg" and "repo-signing.gpg" both
		// sanitize to "repo-signing.gpg": disambiguate whenever the raw
		// candidate had to be rewritten, not only when it carried a query.
		needsDisambiguation = true
	}
	candidate = sanitizedBase + ".gpg"
	// The path segment alone (e.g. "lookup") is not guaranteed unique
	// across repositories when it came with a query string that wasn't a
	// recognized "search=<key ID>"; disambiguate with a short hash of the
	// full URL so two such repositories can't collide on one file.
	if needsDisambiguation {
		hash := sha256.Sum256([]byte(keyURL))
		candidate = fmt.Sprintf("%s-%s.gpg", strings.TrimSuffix(candidate, ".gpg"), hex.EncodeToString(hash[:])[:8])
	}
	return fmt.Sprintf("/etc/apt/trusted.gpg.d/%s", candidate)
}

// gpgKeyIDRe matches a plain hexadecimal OpenPGP key ID or fingerprint: a
// short ID (8 hex digits), long ID (16), or v4 fingerprint (40), with an
// optional "0x" prefix. A keyserver "search=" value only identifies a single
// key on its own when it is one of these — HKP also allows searching by
// name or email, which does not.
var gpgKeyIDRe = regexp.MustCompile(`^(?:0x)?[0-9A-Fa-f]{8}$|^(?:0x)?[0-9A-Fa-f]{16}$|^(?:0x)?[0-9A-Fa-f]{40}$`)

// gpgKeyFilenameCandidate returns a base filename derived from keyURL (without
// its query string) and whether that base name still needs disambiguating
// (true unless it was already derived from a value, like a key fingerprint,
// that uniquely identifies the key on its own). A keyserver
// "pks/lookup?...search=<key ID>" URL uses the requested key ID/fingerprint
// instead of the generic "lookup" path segment, since that is the part that
// actually identifies the key; other "search=" values (HKP also allows name
// or email searches) are not guaranteed unique and still get disambiguated.
func gpgKeyFilenameCandidate(keyURL string) (string, bool) {
	parsed, err := url.Parse(keyURL)
	if err != nil || parsed.Path == "" {
		// Not a parseable URL (or no path): fall back to the last "/"-separated
		// segment of the raw string, same as before this function existed.
		parts := strings.Split(keyURL, "/")
		if len(parts) == 0 {
			return "", false
		}
		return parts[len(parts)-1], strings.Contains(keyURL, "?")
	}

	if search := parsed.Query().Get("search"); strings.HasPrefix(parsed.Path, "/pks/lookup") && gpgKeyIDRe.MatchString(search) {
		// Already unique: no disambiguation needed.
		return strings.TrimPrefix(search, "0x"), false
	}

	// Split the escaped path, not the decoded Path: url.Parse decodes
	// "%2F" in Path, so an encoded separator inside a single path segment
	// would otherwise be treated as a literal "/" and two distinct request
	// paths (e.g. ".../team%2Frelease.gpg" and ".../release.gpg") would
	// both end up with the same last segment "release.gpg". Keeping it
	// escaped leaves "%2F" in the candidate, where sanitization and (once
	// it differs from the unsanitized form) hash disambiguation still
	// apply.
	escapedPath := strings.TrimPrefix(parsed.EscapedPath(), "/")
	// A trailing slash is itself a distinct request path (".../release" vs
	// ".../release/" can serve different keys) that plain trimming would
	// otherwise erase, collapsing both onto the same last segment.
	hasTrailingSlash := strings.HasSuffix(escapedPath, "/") && escapedPath != "/"
	pathParts := strings.Split(strings.TrimSuffix(escapedPath, "/"), "/")
	needsPathDisambiguation := parsed.RawQuery != "" || hasTrailingSlash
	if len(pathParts) == 0 || pathParts[len(pathParts)-1] == "" {
		return "", needsPathDisambiguation
	}
	return pathParts[len(pathParts)-1], needsPathDisambiguation
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

// addGeneratedAdditionalFile adds a tool-generated file, refusing a destination
// that a provisioning script owns, or that contains or sits inside it, so the
// payload is not silently replaced and the copy cannot fail on a path that
// would be both a file and a directory.
func (t *ImageTemplate) addGeneratedAdditionalFile(f AdditionalFileInfo) error {
	for _, s := range t.SystemConfig.Provisioning.Scripts {
		if InImagePathsOverlap(s.Final, f.Final) {
			return fmt.Errorf("systemConfig.provisioning.scripts %q: final %q collides with %q, a path the tool "+
				"generates; choose another path", s.Name, s.Final, f.Final)
		}
	}
	t.addUniqueAdditionalFile(f)
	return nil
}

// generateAptPreferencesFromRepositories creates APT preferences files for all repositories
func (t *ImageTemplate) generateAptPreferencesFromRepositories() error {
	log := logger.Logger()

	log.Infof("Generating apt preferences files for %d repositories", len(t.PackageRepositories))

	for _, repo := range t.PackageRepositories {
		if repo.hasOwnFiles() {
			if err := t.addOwnPreferencesFile(repo); err != nil {
				return err
			}
			continue
		}

		// Extract origin from URL
		origin := extractOriginFromURL(repo.URL)
		if origin == "" {
			log.Warnf("Could not extract origin from URL %s, skipping preferences for repository %s", repo.URL, repo.ID)
			continue
		}

		// Generate preferences content
		preferencesContent := generateAptPreferencesContent(origin, repo.Priority)
		// A priority already below the no-upgrade pin (including negative,
		// "never install") is stricter, so it is kept as configured. Zero is
		// the unset default, which apt documents as undefined, so it is pinned.
		if !t.repoMayUpgrade(repo) && (repo.Priority >= NoUpgradePinPriority || repo.Priority == 0) {
			preferencesContent = generateNoUpgradePreferencesContent(origin)
		}

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

		if err := t.addGeneratedAdditionalFile(preferencesFile); err != nil {
			return err
		}

		log.Infof("Added apt preferences file for %s (priority %d): %s -> %s",
			repo.ID, repo.Priority, preferencesFile.Local, preferencesFile.Final)
	}

	return nil
}

// extractOriginFromURL extracts the host (without port or credentials) that APT
// matches `Pin: origin` against from a repository URL
func extractOriginFromURL(url string) string {
	// Remove the scheme; URL schemes are case-insensitive ("HTTPS://host").
	if i := strings.Index(url, "://"); i >= 0 && !strings.ContainsAny(url[:i], "/?#") {
		url = url[i+len("://"):]
	}

	// The authority ends at the first slash, query, or fragment delimiter.
	authority := url
	if i := strings.IndexAny(url, "/?#"); i >= 0 {
		authority = url[:i]
	}
	if authority == "" {
		return ""
	}
	// APT origins are matched on the bare host, so drop any user:token@ and
	// the port. DNS names are case-insensitive, so the host is lowercased to
	// compare equal.
	host := authority[strings.LastIndex(authority, "@")+1:]
	if strings.HasPrefix(host, "[") {
		// IPv6 literal: the port, if any, follows the closing bracket.
		if i := strings.Index(host, "]"); i >= 0 {
			host = host[:i+1]
		}
	} else if i := strings.Index(host, ":"); i >= 0 {
		host = host[:i]
	}
	return strings.ToLower(host)
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

// NoUpgradePinPriority keeps a version installable when the package is
// absent but below an installed version's priority (100), so it never
// upgrades an installed package.
const NoUpgradePinPriority = 50

// generateNoUpgradePreferencesContent pins a repository the deployed system
// may install from but not upgrade from (systemConfig.aptPolicy or an
// immutable image).
func generateNoUpgradePreferencesContent(origin string) string {
	return fmt.Sprintf("# Priority %d: installable, never upgrades (systemConfig.aptPolicy / immutability)\n"+
		"Package: *\nPin: origin %s\nPin-Priority: %d\n", NoUpgradePinPriority, origin, NoUpgradePinPriority)
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

// addOwnPreferencesFile adds the preferences of a repository that names its
// own files, when it has any, instead of ICT's generated pin.
func (t *ImageTemplate) addOwnPreferencesFile(repo PackageRepository) error {
	if repo.Preferences == "" {
		return nil
	}
	tempFile, err := createTempAptPreferencesFile(repo, repo.Preferences)
	if err != nil {
		return fmt.Errorf("failed to create apt preferences file for %s: %w", getRepositoryName(repo), err)
	}
	return t.addGeneratedAdditionalFile(AdditionalFileInfo{Local: tempFile, Final: repo.PreferencesFile})
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
		if repo.KeyFile != "" {
			keyFilename = repo.KeyFile
		}

		// Add to additionalFiles
		gpgKeyFile := AdditionalFileInfo{
			Local: tempKeyFile,
			Final: keyFilename,
		}

		if err := t.addGeneratedAdditionalFile(gpgKeyFile); err != nil {
			return err
		}

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
			// A cache entry written before this validation existed (or
			// corrupted since) must not be trusted blindly: that is exactly
			// how one bad download (truncated response, HTML error page,
			// proxy hiccup) used to get cached forever and silently poison
			// every later build. Treat an invalid entry as a cache miss.
			if validateErr := validateGPGKeyData(cachedData); validateErr != nil {
				log.Warnf("Cached GPG key for %s at %s failed validation, re-downloading: %v",
					keyURL, cacheFilePath, validateErr)
			} else {
				log.Infof("Using cached GPG key (%d bytes) for %s", len(cachedData), keyURL)
				return cachedData, nil
			}
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

	// Reject anything that isn't an actual OpenPGP key here, before the
	// caller (getCachedOrDownloadGPGKey) persists it: a transient bad
	// response (truncated body, HTML error page) once cached would
	// otherwise silently poison every later build using this key URL.
	if err := validateGPGKeyData(keyData); err != nil {
		return nil, fmt.Errorf("downloaded GPG key from %s is invalid: %w", keyURL, err)
	}

	log.Infof("Successfully downloaded GPG key (%d bytes) from %s", len(keyData), keyURL)

	return keyData, nil
}

// validateGPGKeyData sanity-checks that keyData is an actual OpenPGP public
// key (armored or binary), not empty, truncated, or an HTML/error response
// from a flaky download or proxy. See downloadGPGKey and
// getCachedOrDownloadGPGKey for why this must run before data is trusted or
// cached.
func validateGPGKeyData(keyData []byte) error {
	if len(bytes.TrimSpace(keyData)) == 0 {
		return fmt.Errorf("GPG key data is empty")
	}

	if _, err := exec.LookPath("gpg"); err != nil {
		// gpg isn't available to validate with here; accept the data as-is
		// rather than failing a step that doesn't otherwise need gpg.
		return nil
	}

	// Routed through the shell package's executor (allowlisted commands,
	// cancellation, process-group handling) rather than a raw exec.Command,
	// same as HashPasswordForHost's use of ExecCmdSilentWithInput for stdin-fed
	// commands whose output (an error can echo back the input) must not be
	// logged unconditionally.
	out, err := shell.ExecCmdSilentWithInput(string(keyData), "gpg --with-colons --show-keys", false, shell.HostPath, nil)
	if err != nil {
		return fmt.Errorf("not a valid OpenPGP key: %w", err)
	}
	if !strings.Contains(out, "pub:") {
		return fmt.Errorf("data does not contain an OpenPGP public key")
	}
	return nil
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
