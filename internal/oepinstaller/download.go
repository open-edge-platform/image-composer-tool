package oepinstaller

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/network"
)

// TokenEnv names the environment variable with a GitHub token for downloading
// from private repositories. It is optional and sent only to GitHub hosts.
const TokenEnv = "ICT_OEP_INSTALLER_TOKEN"

// maxDownloadSize bounds a downloaded file; the CLI and its output are scripts
// and text of a few hundred kilobytes.
const maxDownloadSize = 32 << 20

// httpClient returns the client used for downloads; tests replace it.
var httpClient = network.GetSecureHTTPClient

// Download fetches rawURL and verifies that its contents match wantSHA256.
func Download(rawURL, wantSHA256 string) ([]byte, error) {
	req, err := newDownloadRequest(rawURL, os.Getenv(TokenEnv))
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

// newDownloadRequest builds the GET request, adding token as a bearer token
// only when the host is GitHub's.
func newDownloadRequest(rawURL, token string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
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
		return fmt.Sprintf(" (the file may be in a private repository; set %s to a GitHub token with read access)",
			TokenEnv)
	}
	return fmt.Sprintf(" (check the URL, and that the token in %s has read access to the repository)", TokenEnv)
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
	log := logger.Logger()
	log.Infof("OEP Installer: imported %s (cli-version %s, %d packages, %d repositories)",
		o.SBOMURL, out.CLIVersion, len(out.Packages), len(out.Repositories))
	if out.CLIURL != "" && cliFile(out.CLIURL) != cliFile(o.URL) {
		log.Warnf("OEP Installer: %s was generated with %s, but systemConfig.oepInstaller.url is %s",
			o.SBOMURL, out.CLIURL, o.URL)
	}
	return out, nil
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
