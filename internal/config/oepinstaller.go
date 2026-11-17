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
	URL        string   `yaml:"url,omitempty"`        // rendered openedge-cli; omitted: the output's cli-url
	SHA256     string   `yaml:"sha256,omitempty"`     // hex SHA-256 of the file at URL; required with URL
	SBOMURL    string   `yaml:"sbomUrl,omitempty"`    // HTTPS URL of the published list --sbom output
	SBOMSHA256 string   `yaml:"sbomSha256,omitempty"` // hex SHA-256 of the file at SBOMURL; omitted: not verified
	Modules    []string `yaml:"modules,omitempty"`    // openedge-cli install modules; omitted: the output's module-list
	User       string   `yaml:"user,omitempty"`       // systemConfig.users entry that runs openedge-cli on first boot
	Args       []string `yaml:"args,omitempty"`       // install arguments; omitted: the output's install-cmdline
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
	// The CLI is optional as a pair: without it ICT installs the CLI the output
	// names in cli-url, the one it was generated with.
	if o.URL != "" || o.SHA256 != "" {
		if o.URL == "" {
			return fmt.Errorf("systemConfig.oepInstaller.sha256 needs systemConfig.oepInstaller.url")
		}
		if err := validateOEPInstallerDownload("url", o.URL, "sha256", o.SHA256, true); err != nil {
			return err
		}
	}
	if err := validateOEPInstallerDownload("sbomUrl", o.SBOMURL, "sbomSha256", o.SBOMSHA256, false); err != nil {
		return err
	}
	if len(o.Modules) > 0 {
		if err := ValidateOEPInstallerModules("systemConfig.oepInstaller.modules", o.Modules); err != nil {
			return err
		}
	}
	if err := t.validateOEPInstallerUser(o.User); err != nil {
		return err
	}
	return ValidateOEPInstallerArgs("systemConfig.oepInstaller.args", o.Args)
}

// validateOEPInstallerDownload checks a download: an https:// URL of the file
// itself and, when set or required, the hex SHA-256 digest it must match. An
// unpinned download, such as a file on a branch, is not verified.
func validateOEPInstallerDownload(urlField, raw, shaField, sha string, shaRequired bool) error {
	if raw == "" {
		return fmt.Errorf("systemConfig.oepInstaller.%s is required", urlField)
	}
	if err := ValidateOEPInstallerURL("systemConfig.oepInstaller."+urlField, raw); err != nil {
		return err
	}
	if (sha != "" || shaRequired) && !sha256HexRe.MatchString(sha) {
		return fmt.Errorf("systemConfig.oepInstaller.%s must be a 64-character hex SHA-256 digest", shaField)
	}
	return nil
}

// ValidateOEPInstallerURL checks that raw, which name describes in messages,
// is an https:// URL of the file itself: with a host, without credentials, and
// not a GitHub web page. ICT applies it to a template's url and sbomUrl, and to
// a CLI URL it takes from an OEP Installer output.
func ValidateOEPInstallerURL(name, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("%s must be an https:// URL (got %q)", name, raw)
	}
	if u.User != nil {
		return fmt.Errorf("%s must not contain credentials", name)
	}
	if rawURL, ok := githubRawURL(u); ok {
		return fmt.Errorf("%s %q is a GitHub web page, not the file; use %q", name, u, rawURL)
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

// ValidateOEPInstallerModules checks modules, which name describes in
// messages: at least one, each a valid module name, none twice. ICT applies it
// to a template's modules and to the module-list it takes from an output.
func ValidateOEPInstallerModules(name string, modules []string) error {
	if len(modules) == 0 {
		return fmt.Errorf("%s must list at least one profile or module", name)
	}
	seen := make(map[string]bool, len(modules))
	for _, m := range modules {
		if !provisioningScriptNameRe.MatchString(m) {
			return fmt.Errorf("invalid %s entry %q: must match %s", name, m, provisioningScriptNameRe.String())
		}
		if seen[m] {
			return fmt.Errorf("duplicate %s entry %q", name, m)
		}
		seen[m] = true
	}
	return nil
}

// ValidateOEPInstallerArgs checks install arguments, which name describes in
// messages; they become arguments of the first-boot unit. ICT applies it to a
// template's args and to the arguments it takes from an output.
func ValidateOEPInstallerArgs(name string, args []string) error {
	for i, a := range args {
		if a == "" || !provisioningValueRe.MatchString(a) {
			return fmt.Errorf("%s[%d] must be 1-256 characters of "+
				"letters, digits, and ._@%%+:/=,- (no spaces, quotes, $ or ;)", name, i)
		}
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
