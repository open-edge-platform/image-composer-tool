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
	"slices"
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

// TokenFileFlag names the build flag that sets a token file for one build.
const TokenFileFlag = "--oep-installer-token-file"

// buildTokenFile is the file SetTokenFile named; "" when unset.
var buildTokenFile string

// SetTokenFile sets the file holding the token for this build. It takes
// precedence over every other source: the Web UI passes the token a user enters
// for one build this way, and the build may inherit a token from the server's
// environment that must not override it.
func SetTokenFile(path string) {
	buildTokenFile = path
}

// maxDownloadSize bounds a downloaded file; the CLI and its output are scripts
// and text of a few hundred kilobytes.
const maxDownloadSize = 32 << 20

// downloadTimeout bounds one download, including reading the body, so a server
// that stalls after the headers cannot block the build.
const downloadTimeout = 2 * time.Minute

// httpClient returns the client used for downloads; tests replace it.
var httpClient = network.GetSecureHTTPClient

// Download fetches rawURL and verifies that its contents match wantSHA256. An
// empty wantSHA256, for a file that is not pinned, skips the check; the
// digest of what was downloaded is then logged as a warning, so the build log
// still records what the image got.
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
	got := sha256Hex(data)
	if wantSHA256 == "" {
		logger.Logger().Warnf("OEP Installer: %s has no SHA-256 to verify against; downloaded SHA-256 %s",
			rawURL, got)
		return data, nil
	}
	if !strings.EqualFold(got, wantSHA256) {
		return nil, fmt.Errorf("downloading %s: SHA-256 is %s, want %s", rawURL, got, wantSHA256)
	}
	return data, nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// resolveToken returns the GitHub token from the first source that is set: the
// file SetTokenFile named, TokenEnv, the file TokenFileEnv names, or the file the
// global configuration's oep_installer.token_file names. It returns "" when none
// is set. A file that is set but missing, unreadable, not a regular file, or
// empty is an error.
func resolveToken() (string, error) {
	path, source := buildTokenFile, TokenFileFlag
	if path == "" {
		if token := os.Getenv(TokenEnv); token != "" {
			logger.Logger().Debugf("OEP Installer: using the GitHub token from %s", TokenEnv)
			return token, nil
		}
		path, source = os.Getenv(TokenFileEnv), TokenFileEnv
	}
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
			"a file that build %s names, in %s, or in a file that %s or the global configuration's %s names)",
			TokenFileFlag, TokenEnv, TokenFileEnv, tokenFileConfig)
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
	out.SHA256 = sha256Hex(data)
	// Without a CLI of its own the template installs the output's, which
	// cannot differ from it.
	if o.URL != "" {
		if err := checkCLI(out, o); err != nil {
			return nil, fmt.Errorf("%s: %w", o.SBOMURL, err)
		}
	}
	logger.Logger().Infof("OEP Installer: imported %s (cli-version %s, %d packages, %d repositories)",
		o.SBOMURL, out.CLIVersion, len(out.Packages), len(out.Repositories))
	return out, nil
}

// checkCLI compares the CLI that generated the output (cli-url) with the CLI
// the template installs on first boot (url). A cli-url that is a raw URL, and
// so pinned to a commit, must equal url, and a cli-sha256 must equal sha256
// whatever the URLs. A web-page cli-url on a branch can only be compared by
// repository and path.
func checkCLI(out *Output, o config.OEPInstallerConfig) error {
	// The digests pin content, so they must agree whatever the URLs say.
	if out.CLISHA256 != "" && !strings.EqualFold(out.CLISHA256, o.SHA256) {
		return fmt.Errorf("the output gives the CLI's SHA-256 as %s, but systemConfig.oepInstaller.sha256 is %s",
			out.CLISHA256, o.SHA256)
	}
	cliURL, templateURL := out.CLIURL, o.URL
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

// Resolve returns o completed from out, the output it names: the CLI the
// output was generated with (cli-url and cli-sha256) when o names none, and
// the output's modules and arguments when o lists none. It downloads the CLI,
// verified against the template's or the output's SHA-256 when either gives
// one, and records the digests of what was downloaded, so the template the
// image carries pins exactly what was installed.
func Resolve(o config.OEPInstallerConfig, out *Output) (resolved config.OEPInstallerConfig, cli []byte, err error) {
	resolved, err = complete(o, out)
	if err != nil {
		return o, nil, fmt.Errorf("%s: %w", o.SBOMURL, err)
	}
	cli, err = Download(resolved.URL, resolved.SHA256)
	if err != nil {
		return o, nil, err
	}
	resolved.SHA256 = sha256Hex(cli)
	resolved.SBOMSHA256 = out.SHA256
	logger.Logger().Infof("OEP Installer: CLI %s (SHA-256 %s); openedge-cli install %s",
		resolved.URL, resolved.SHA256, strings.Join(append(slices.Clone(resolved.Modules), resolved.Args...), " "))
	return resolved, cli, nil
}

// complete fills in what o omits from out, checking each value it takes with
// the rules for the template's own field: the CLI (cli-url and cli-sha256)
// when o has no url, the modules when o lists none, and the arguments when o
// omits args. An explicitly empty args is kept, and Parse has already checked
// that it matches the output.
func complete(o config.OEPInstallerConfig, out *Output) (config.OEPInstallerConfig, error) {
	if o.URL == "" {
		if out.CLIURL == "" {
			return o, fmt.Errorf("the output names no CLI (cli-url); set systemConfig.oepInstaller.url and sha256")
		}
		if err := config.ValidateOEPInstallerURL("the output's cli-url", out.CLIURL); err != nil {
			return o, err
		}
		o.URL, o.SHA256 = out.CLIURL, out.CLISHA256
	}
	if len(o.Modules) == 0 {
		if err := config.ValidateOEPInstallerModules("the output's module-list", out.Modules); err != nil {
			return o, err
		}
		o.Modules = slices.Clone(out.Modules)
	}
	if o.Args == nil {
		if err := config.ValidateOEPInstallerArgs("the output's install-cmdline arguments", out.Arguments); err != nil {
			return o, err
		}
		o.Args = slices.Clone(out.Arguments)
	}
	return o, nil
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
