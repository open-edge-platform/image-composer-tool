package config

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// OEPInstallerTargetOS lists the target OSes the OEP Installer integration
// supports.
var OEPInstallerTargetOS = []string{"ubuntu"}

// sha256HexRe matches a hex-encoded SHA-256 digest.
var sha256HexRe = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

// OEPInstallerConfig selects an OEP Installer (openedge-cli) build and the
// modules (profiles or individual modules) to compose into the image. At build
// time ICT imports the modules' Debian package requirements from the output
// file the OEP Installer publishes with the CLI; on first boot the CLI
// completes the installation.
type OEPInstallerConfig struct {
	URL        string   `yaml:"url,omitempty"`        // HTTPS URL of the rendered openedge-cli, pinned to a commit
	SHA256     string   `yaml:"sha256,omitempty"`     // hex SHA-256 of the file at URL
	SBOMURL    string   `yaml:"sbomUrl,omitempty"`    // HTTPS URL of the published list --sbom output for Modules
	SBOMSHA256 string   `yaml:"sbomSha256,omitempty"` // hex SHA-256 of the file at SBOMURL
	Modules    []string `yaml:"modules,omitempty"`    // profiles or modules, as accepted by openedge-cli install
	User       string   `yaml:"user,omitempty"`       // systemConfig.users entry that runs openedge-cli on first boot
	Args       []string `yaml:"args,omitempty"`       // extra arguments passed to openedge-cli install
}

// IsEmpty reports whether the section is absent.
func (o OEPInstallerConfig) IsEmpty() bool {
	return o.URL == "" && o.SHA256 == "" && o.SBOMURL == "" && o.SBOMSHA256 == "" &&
		len(o.Modules) == 0 && o.User == "" && len(o.Args) == 0
}

// validateOEPInstaller checks the merged systemConfig.oepInstaller section.
// It needs the merged template: the user must be defined by some layer.
func (t *ImageTemplate) validateOEPInstaller() error {
	o := t.SystemConfig.OEPInstaller
	if o.IsEmpty() {
		return nil
	}
	if !slices.Contains(OEPInstallerTargetOS, t.Target.OS) {
		return fmt.Errorf("systemConfig.oepInstaller is only supported for ubuntu targets (got %q)", t.Target.OS)
	}
	if err := validateOEPInstallerDownload("url", o.URL, "sha256", o.SHA256); err != nil {
		return err
	}
	if err := validateOEPInstallerDownload("sbomUrl", o.SBOMURL, "sbomSha256", o.SBOMSHA256); err != nil {
		return err
	}
	if err := validateOEPInstallerModules(o.Modules); err != nil {
		return err
	}
	if err := t.validateOEPInstallerUser(o.User); err != nil {
		return err
	}
	for i, a := range o.Args {
		if a == "" || !provisioningValueRe.MatchString(a) {
			return fmt.Errorf("systemConfig.oepInstaller.args[%d] must be 1-256 characters of "+
				"letters, digits, and ._@%%+:/=,- (no spaces, quotes, $ or ;)", i)
		}
	}
	return nil
}

// validateOEPInstallerDownload checks a pinned download: an https:// URL of
// the file itself and the hex SHA-256 digest it must match.
func validateOEPInstallerDownload(urlField, raw, shaField, sha string) error {
	if raw == "" {
		return fmt.Errorf("systemConfig.oepInstaller.%s is required", urlField)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("systemConfig.oepInstaller.%s must be an https:// URL (got %q)", urlField, raw)
	}
	if u.User != nil {
		return fmt.Errorf("systemConfig.oepInstaller.%s must not contain credentials", urlField)
	}
	if rawURL, ok := githubRawURL(u); ok {
		return fmt.Errorf("systemConfig.oepInstaller.%s %q is a GitHub web page, not the file; use %q",
			urlField, u, rawURL)
	}
	if !sha256HexRe.MatchString(sha) {
		return fmt.Errorf("systemConfig.oepInstaller.%s must be a 64-character hex SHA-256 digest", shaField)
	}
	return nil
}

// githubRawURL returns the raw.githubusercontent.com URL for a github.com
// blob or tree page URL (https://github.com/<owner>/<repo>/blob/<ref>/<path>),
// which serves HTML rather than the file.
func githubRawURL(u *url.URL) (string, bool) {
	if !strings.EqualFold(u.Host, "github.com") {
		return "", false
	}
	parts := strings.SplitN(strings.TrimPrefix(u.Path, "/"), "/", 4)
	if len(parts) < 4 || (parts[2] != "blob" && parts[2] != "tree") {
		return "", false
	}
	return "https://raw.githubusercontent.com/" + parts[0] + "/" + parts[1] + "/" + parts[3], true
}

func validateOEPInstallerModules(modules []string) error {
	if len(modules) == 0 {
		return fmt.Errorf("systemConfig.oepInstaller.modules must list at least one profile or module")
	}
	seen := make(map[string]bool, len(modules))
	for _, m := range modules {
		if !provisioningScriptNameRe.MatchString(m) {
			return fmt.Errorf("invalid systemConfig.oepInstaller.modules entry %q: must match %s",
				m, provisioningScriptNameRe.String())
		}
		if seen[m] {
			return fmt.Errorf("duplicate systemConfig.oepInstaller.modules entry %q", m)
		}
		seen[m] = true
	}
	return nil
}

func (t *ImageTemplate) validateOEPInstallerUser(name string) error {
	if name == "" {
		return fmt.Errorf("systemConfig.oepInstaller.user is required")
	}
	if name == "root" {
		return fmt.Errorf("systemConfig.oepInstaller.user must not be root; openedge-cli runs as a regular user")
	}
	if !slices.ContainsFunc(t.SystemConfig.Users, func(u UserConfig) bool { return u.Name == name }) {
		return fmt.Errorf("systemConfig.oepInstaller.user %q is not defined in systemConfig.users", name)
	}
	return nil
}
