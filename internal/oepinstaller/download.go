package oepinstaller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/network"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/runctx"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/security"
)

// TokenEnv names the environment variable with a GitHub token for downloading
// from private repositories. It is optional and sent only to GitHub hosts.
const TokenEnv = "ICT_OEP_INSTALLER_TOKEN"

// TokenFileEnv names the environment variable with the path of a file holding
// the token, such as a mounted secret. TokenEnv takes precedence over it, and
// it over the global configuration's oep_installer.token_file.
const TokenFileEnv = "ICT_OEP_INSTALLER_TOKEN_FILE"

// tokenFileConfig names the global configuration setting in messages.
const tokenFileConfig = "oep_installer.token_file"

// maxDownloadSize bounds a downloaded file; the CLI and its output are scripts
// and text of a few hundred kilobytes.
const maxDownloadSize = 32 << 20

// downloadTimeout bounds one download, including reading the body, so a server
// that stalls after the headers cannot block the build.
const downloadTimeout = 2 * time.Minute

// httpClient returns the client used for downloads; tests replace it.
var httpClient = network.GetSecureHTTPClient

// Download fetches rawURL and verifies that its contents match wantSHA256.
func Download(rawURL, wantSHA256 string) ([]byte, error) {
	token, err := resolveToken()
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", rawURL, err)
	}
	ctx, cancel := context.WithTimeout(runctx.Context(), downloadTimeout)
	defer cancel()
	req, err := newDownloadRequest(ctx, rawURL, token)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s: %s%s", rawURL, resp.Status, downloadHint(req, resp.StatusCode))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadSize+1))
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", rawURL, err)
	}
	if len(data) > maxDownloadSize {
		return nil, fmt.Errorf("downloading %s: larger than %d bytes", rawURL, maxDownloadSize)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); !strings.EqualFold(got, wantSHA256) {
		return nil, fmt.Errorf("downloading %s: SHA-256 is %s, want %s", rawURL, got, wantSHA256)
	}
	return data, nil
}

// resolveToken returns the GitHub token from the first source that is set:
// TokenEnv, the file TokenFileEnv names, or the file the global configuration's
// oep_installer.token_file names. It returns "" when none is set. A file that is
// set but missing, unreadable, not a regular file, or empty is an error.
func resolveToken() (string, error) {
	if token := os.Getenv(TokenEnv); token != "" {
		logger.Logger().Debugf("OEP Installer: using the GitHub token from %s", TokenEnv)
		return token, nil
	}
	path, source := os.Getenv(TokenFileEnv), TokenFileEnv
	if path == "" {
		path, source = config.Global().OEPInstaller.TokenFile, tokenFileConfig
	}
	if path == "" {
		return "", nil
	}
	token, err := readTokenFile(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("reading the GitHub token file that %s names: %w", source, err)
	}
	logger.Logger().Debugf("OEP Installer: using the GitHub token from the file that %s names", source)
	return token, nil
}

// readTokenFile reads a token from path, which may be a symlink (Kubernetes
// mounts secrets that way) to a regular file. A file that group or others can
// read gets a warning, not an error: Docker mounts secrets with mode 0444.
func readTokenFile(path string) (string, error) {
	info, err := security.CheckSymlink(path, security.ResolveSymlinks)
	if err != nil {
		return "", err
	}
	if !info.FileInfo.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", path)
	}
	if info.FileInfo.Mode().Perm()&0o077 != 0 {
		logger.Logger().Warnf("OEP Installer: the GitHub token file %s can be read by other users (mode %04o); "+
			"restrict it to its owner with chmod 600", path, info.FileInfo.Mode().Perm())
	}
	data, err := os.ReadFile(info.ResolvedPath)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("%s is empty", path)
	}
	return token, nil
}

// newDownloadRequest builds the GET request, adding token as a bearer token
// only when the host is GitHub's.
func newDownloadRequest(ctx context.Context, rawURL, token string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid download URL %q: %w", rawURL, err)
	}
	if token != "" && isGitHubHost(req.URL) {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req, nil
}

// isGitHubHost reports whether u is served by GitHub.
func isGitHubHost(u *url.URL) bool {
	host := strings.ToLower(u.Hostname())
	return host == "github.com" || host == "api.github.com" || strings.HasSuffix(host, ".githubusercontent.com")
}

// downloadHint explains a GitHub "not found" response, which is also what a
// private repository returns without a valid token.
func downloadHint(req *http.Request, status int) string {
	if status != http.StatusNotFound || !isGitHubHost(req.URL) {
		return ""
	}
	if req.Header.Get("Authorization") == "" {
		return fmt.Sprintf(" (the file may be in a private repository; provide a GitHub token with read access in "+
			"%s, or in a file that %s or the global configuration's %s names)", TokenEnv, TokenFileEnv, tokenFileConfig)
	}
	return " (check the URL, and that the GitHub token has read access to the repository)"
}

// Load downloads the published output that o names, verifies its digest, and
// parses it for o's modules and arguments.
func Load(o config.OEPInstallerConfig) (*Output, error) {
	data, err := Download(o.SBOMURL, o.SBOMSHA256)
	if err != nil {
		return nil, err
	}
	out, err := Parse(data, o.Modules, o.Args)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", o.SBOMURL, err)
	}
	if err := checkCLI(out.CLIURL, o.URL); err != nil {
		return nil, fmt.Errorf("%s: %w", o.SBOMURL, err)
	}
	logger.Logger().Infof("OEP Installer: imported %s (cli-version %s, %d packages, %d repositories)",
		o.SBOMURL, out.CLIVersion, len(out.Packages), len(out.Repositories))
	return out, nil
}

// checkCLI compares the CLI that generated the output (cli-url) with the CLI
// the template installs on first boot (url). A cli-url that is a raw URL, and
// so pinned to a commit, must equal url. A web-page cli-url on a branch can
// only be compared by repository and path.
func checkCLI(cliURL, templateURL string) error {
	if cliURL == "" || cliURL == templateURL {
		return nil
	}
	if cliFile(cliURL) != cliFile(templateURL) {
		return fmt.Errorf("the output was generated with %s, a different CLI than systemConfig.oepInstaller.url %s",
			cliURL, templateURL)
	}
	if u, err := url.Parse(cliURL); err == nil && strings.EqualFold(u.Host, "raw.githubusercontent.com") {
		return fmt.Errorf("the output was generated with %s, a different version of the CLI than "+
			"systemConfig.oepInstaller.url %s", cliURL, templateURL)
	}
	logger.Logger().Warnf("OEP Installer: the output was generated with %s; check that systemConfig.oepInstaller.url "+
		"%s is the same version", cliURL, templateURL)
	return nil
}

// cliFile identifies a CLI file on GitHub by repository and path, ignoring
// the commit or branch, from either its web page or its raw URL.
func cliFile(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	switch {
	case strings.EqualFold(u.Host, "github.com") && len(parts) > 4 && (parts[2] == "blob" || parts[2] == "tree"):
		return strings.Join(append(parts[:2:2], parts[4:]...), "/")
	case strings.EqualFold(u.Host, "raw.githubusercontent.com") && len(parts) > 3:
		return strings.Join(append(parts[:2:2], parts[3:]...), "/")
	}
	return rawURL
}
