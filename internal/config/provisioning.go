package config

import (
	"bytes"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/security"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/slice"
	"golang.org/x/crypto/ssh"
)

// Provisioning script stages.
const (
	ProvisioningStageFirstBoot = "first-boot"
	ProvisioningStageEveryBoot = "every-boot"
)

// AptPolicyTargetOS lists the target.os values whose APT upgrade policy ICT
// can write.
var AptPolicyTargetOS = []string{"ubuntu", "debian"}

// Boot entry policies for the live installer.
const (
	BootEntryPolicyPreserve  = "preserve"
	BootEntryPolicyExclusive = "exclusive"
)

// In-image locations that cloud-init inputs are copied to. NoCloud reads its
// seed from this directory without any network or attached media.
const (
	CloudInitSeedDir   = "/var/lib/cloud/seed/nocloud"
	CloudInitConfigDir = "/etc/cloud/cloud.cfg.d"
	cloudInitPackage   = "cloud-init"
	// CloudInitDatasourceFile is the datasource pin the tool writes into CloudInitConfigDir.
	CloudInitDatasourceFile = "90_ict_datasource.cfg"
)

// ProxyConfig holds the proxy settings persisted into the deployed system.
type ProxyConfig struct {
	HTTPProxy  string `yaml:"httpProxy,omitempty"`
	HTTPSProxy string `yaml:"httpsProxy,omitempty"`
	FTPProxy   string `yaml:"ftpProxy,omitempty"`
	NoProxy    string `yaml:"noProxy,omitempty"` // comma-separated hosts/domains/CIDRs

	wasProvided bool `yaml:"-"` // the section was present in YAML, even if empty
}

// UnmarshalYAML records that the proxy section was present, so a child
// template's empty `proxy: {}` can clear a proxy inherited from its parent.
func (p *ProxyConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type alias ProxyConfig
	if err := unmarshal((*alias)(p)); err != nil {
		return err
	}
	p.wasProvided = true
	return nil
}

// IsEmpty reports whether no proxy value is configured.
func (p ProxyConfig) IsEmpty() bool {
	return p.HTTPProxy == "" && p.HTTPSProxy == "" && p.FTPProxy == "" && p.NoProxy == ""
}

// ProvisioningScript is a user script copied into the image and run by a
// generated systemd unit at the configured boot stage.
type ProvisioningScript struct {
	Name  string `yaml:"name"`            // unit-name-safe identifier
	Local string `yaml:"local,omitempty"` // host path of the script
	Final string `yaml:"final"`           // in-image path of the script
	Stage string `yaml:"stage,omitempty"` // first-boot (default) | every-boot
	Order int    `yaml:"order,omitempty"` // lower runs first; ties keep template order
}

// ProvisioningConfig holds boot-time provisioning scripts.
type ProvisioningConfig struct {
	Scripts []ProvisioningScript `yaml:"scripts,omitempty"`
}

// CloudInitConfig installs cloud-init and seeds it with NoCloud data.
type CloudInitConfig struct {
	// Enabled is a pointer so a child layer can turn off an inherited
	// section with an explicit enabled: false.
	Enabled           *bool    `yaml:"enabled,omitempty"`
	UserDataFile      string   `yaml:"userDataFile,omitempty"`      // host path
	MetaDataFile      string   `yaml:"metaDataFile,omitempty"`      // host path; generated when empty
	NetworkConfigFile string   `yaml:"networkConfigFile,omitempty"` // host path
	ConfigFiles       []string `yaml:"configFiles,omitempty"`       // host paths copied to /etc/cloud/cloud.cfg.d
}

// IsEnabled reports whether cloud-init should be installed and seeded.
func (c CloudInitConfig) IsEnabled() bool {
	return c.Enabled != nil && *c.Enabled
}

func (c CloudInitConfig) isEmpty() bool {
	return c.Enabled == nil && c.UserDataFile == "" && c.MetaDataFile == "" &&
		c.NetworkConfigFile == "" && len(c.ConfigFiles) == 0
}

// AptPolicy restricts which of the template's packageRepositories may
// upgrade installed packages on the deployed system. The distribution
// archives (including -updates and -security) are never restricted by this
// policy; they only stop upgrading when systemConfig.immutability is enabled.
type AptPolicy struct {
	// UpgradeAllowedRepos lists packageRepositories (by id or codename) that
	// may upgrade packages. Empty means every configured repository may.
	UpgradeAllowedRepos []string `yaml:"upgradeAllowedRepos,omitempty"`
}

// isEmpty reports whether the section is absent. An explicitly empty
// upgradeAllowedRepos is present, so a child layer can reset an inherited
// restriction to "all repositories may upgrade".
func (a AptPolicy) isEmpty() bool {
	return a.UpgradeAllowedRepos == nil
}

// RepoUpgradeAllowed reports whether a package repository may upgrade packages.
func (a AptPolicy) RepoUpgradeAllowed(repo PackageRepository) bool {
	if len(a.UpgradeAllowedRepos) == 0 {
		return true
	}
	return slices.ContainsFunc(a.UpgradeAllowedRepos, func(ref string) bool { return repoMatches(ref, repo) })
}

// repoMatches reports whether an aptPolicy reference names a repository.
func repoMatches(ref string, repo PackageRepository) bool {
	return ref == repo.ID || ref == repo.Codename
}

// repoMayUpgrade reports whether the deployed system may upgrade packages
// from a repository: never on an immutable image, otherwise as aptPolicy says.
func (t *ImageTemplate) repoMayUpgrade(repo PackageRepository) bool {
	return !t.SystemConfig.Immutability.Enabled && t.SystemConfig.AptPolicy.RepoUpgradeAllowed(repo)
}

// GetBootEntryPolicy returns the boot entry policy, defaulting to preserve.
func (t *ImageTemplate) GetBootEntryPolicy() string {
	if t.SystemConfig.Bootloader.BootEntryPolicy == "" {
		return BootEntryPolicyPreserve
	}
	return t.SystemConfig.Bootloader.BootEntryPolicy
}

// SortedProvisioningScripts returns the scripts in execution order.
func (t *ImageTemplate) SortedProvisioningScripts() []ProvisioningScript {
	scripts := append([]ProvisioningScript(nil), t.SystemConfig.Provisioning.Scripts...)
	sort.SliceStable(scripts, func(i, j int) bool { return scripts[i].Order < scripts[j].Order })
	return scripts
}

func hasProvisioningSections(sc SystemConfig) bool {
	return sc.Proxy.wasProvided || !sc.Proxy.IsEmpty() || len(sc.Provisioning.Scripts) > 0 ||
		!sc.CloudInit.isEmpty() || !sc.AptPolicy.isEmpty()
}

// mergeProvisioningSections applies child-wins merging: a section set by the
// user replaces the default. Provisioning scripts accumulate across layers;
// a script whose name matches an inherited one replaces it in place.
func mergeProvisioningSections(merged *SystemConfig, defaultConfig, userConfig SystemConfig) {
	if userConfig.Proxy.wasProvided || !userConfig.Proxy.IsEmpty() {
		merged.Proxy = userConfig.Proxy
	}
	if len(userConfig.Provisioning.Scripts) > 0 {
		merged.Provisioning.Scripts = mergeProvisioningScripts(
			defaultConfig.Provisioning.Scripts, userConfig.Provisioning.Scripts)
	}
	if !userConfig.CloudInit.isEmpty() {
		merged.CloudInit = userConfig.CloudInit
	}
	if !userConfig.AptPolicy.isEmpty() {
		merged.AptPolicy = userConfig.AptPolicy
	}
}

func mergeProvisioningScripts(inherited, own []ProvisioningScript) []ProvisioningScript {
	merged := append([]ProvisioningScript(nil), inherited...)
	for _, s := range own {
		i := slices.IndexFunc(merged, func(m ProvisioningScript) bool { return m.Name == s.Name })
		if i >= 0 {
			merged[i] = s
			continue
		}
		merged = append(merged, s)
	}
	return merged
}

// ProvisioningStampDir records completed first-boot scripts on the deployed system.
const ProvisioningStampDir = "/var/lib/image-composer-tool/provisioned"

// CloudInitMetaDataScript and CloudInitMetaDataUnit are the generated first-boot
// meta-data helper and its systemd unit (both relative to the image root).
const (
	CloudInitMetaDataScript = "/usr/libexec/ict-cloud-init-meta-data"
	CloudInitMetaDataUnit   = "ict-cloud-init-meta-data.service"
)

// KeyOnlySudoersFile is the sudoers drop-in for sudo users that have SSH keys
// and a locked password.
const KeyOnlySudoersFile = "/etc/sudoers.d/90-ict-key-only-admins"

// Paths the provisioning writers generate inside the image. /etc/environment
// is merged with any existing file; the others are written whole.
const (
	environmentPath         = "/etc/environment"
	aptProxyPath            = "/etc/apt/apt.conf.d/95ict-proxy"
	systemdProxyPath        = "/etc/systemd/system.conf.d/90-ict-proxy.conf"
	aptPolicyPrefsPath      = "/etc/apt/preferences.d/00-ict-apt-policy"
	unattendedUpgradesPath  = "/etc/apt/apt.conf.d/52ict-unattended-upgrades"
	provisionUnitPrefix     = "/etc/systemd/system/ict-provision-"
	provisionUnitWantPrefix = "/etc/systemd/system/multi-user.target.wants/ict-provision-"
)

// Files the provisioning writers generate inside the image. A script's final
// path may not land on them: the generated content would replace the script
// while its unit still points at it.
var (
	reservedProvisioningFiles = []string{
		environmentPath,
		aptProxyPath,
		systemdProxyPath,
		aptPolicyPrefsPath,
		unattendedUpgradesPath,
		CloudInitConfigDir + "/" + CloudInitDatasourceFile,
		CloudInitMetaDataScript,
		KeyOnlySudoersFile,
		"/etc/systemd/system/" + CloudInitMetaDataUnit,
		"/etc/systemd/system/multi-user.target.wants/" + CloudInitMetaDataUnit,
	}
	reservedProvisioningDirs = []string{
		ProvisioningStampDir,
		CloudInitSeedDir,
	}
	reservedProvisioningPrefixes = []string{
		provisionUnitPrefix,
		provisionUnitWantPrefix,
	}
)

// InImagePathsOverlap reports whether two in-image paths are the same or one
// is an ancestor of the other. Copying both would make one path a file and a
// directory at once, so the installation would fail.
func InImagePathsOverlap(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

// IsReservedProvisioningPath reports whether an in-image path is generated by
// the provisioning features and therefore unavailable to script destinations.
// Descendants and ancestors of a generated file or directory are reserved too:
// a script there would turn the generated path into a directory, or be
// shadowed by it.
func IsReservedProvisioningPath(final string) bool {
	for _, reserved := range reservedProvisioningFiles {
		if InImagePathsOverlap(final, reserved) {
			return true
		}
	}
	for _, dir := range reservedProvisioningDirs {
		if InImagePathsOverlap(final, dir) {
			return true
		}
	}
	for _, prefix := range reservedProvisioningPrefixes {
		if strings.HasPrefix(final, prefix) {
			return true
		}
	}
	return false
}

// generatedProvisioningPaths returns the in-image paths the enabled
// provisioning writers overwrite, and the path prefixes of their generated unit
// files. /etc/environment is not included: the proxy writer merges into an
// existing file, so shipping one as an additional file is supported.
func (t *ImageTemplate) generatedProvisioningPaths() (paths, prefixes []string) {
	sc := t.SystemConfig
	if !sc.Proxy.IsEmpty() {
		paths = append(paths, systemdProxyPath)
		if sc.Proxy.HTTPProxy != "" || sc.Proxy.HTTPSProxy != "" || sc.Proxy.FTPProxy != "" {
			paths = append(paths, aptProxyPath)
		}
	}
	if sc.Immutability.Enabled && slice.Contains(AptPolicyTargetOS, t.Target.OS) {
		paths = append(paths, aptPolicyPrefsPath, unattendedUpgradesPath)
	}
	if sc.CloudInit.IsEnabled() {
		paths = append(paths, CloudInitConfigDir+"/"+CloudInitDatasourceFile)
		if sc.CloudInit.MetaDataFile == "" {
			paths = append(paths, CloudInitMetaDataScript,
				"/etc/systemd/system/"+CloudInitMetaDataUnit,
				"/etc/systemd/system/multi-user.target.wants/"+CloudInitMetaDataUnit)
		}
	}
	if slices.ContainsFunc(sc.Users, func(u UserConfig) bool {
		return u.HasSudoAccess() && u.Password == "" && len(u.SSHAuthorizedKeys) > 0
	}) {
		paths = append(paths, KeyOnlySudoersFile)
	}
	if len(sc.Provisioning.Scripts) > 0 {
		paths = append(paths, ProvisioningStampDir)
		prefixes = append(prefixes, provisionUnitPrefix, provisionUnitWantPrefix)
	}
	return paths, prefixes
}

// validateAdditionalFilesNotGenerated rejects an additionalFiles entry whose
// installed path is one the enabled provisioning writers generate. Additional
// files are copied first, so the generated content would silently replace the
// payload. A final that is a directory receives the file under its source
// basename (cp semantics), so both spellings are checked.
func (t *ImageTemplate) validateAdditionalFilesNotGenerated() error {
	paths, prefixes := t.generatedProvisioningPaths()
	for _, f := range t.SystemConfig.AdditionalFiles {
		clean := filepath.Clean("/" + f.Final)
		for _, dest := range []string{clean, clean + "/" + filepath.Base(f.Local)} {
			for _, p := range paths {
				if dest == p || strings.HasPrefix(dest, p+"/") {
					return fmt.Errorf("systemConfig.additionalFiles %q installs to %q, which image-composer-tool "+
						"generates for the enabled provisioning settings; choose another path", f.Local, dest)
				}
			}
			for _, prefix := range prefixes {
				if strings.HasPrefix(dest, prefix) {
					return fmt.Errorf("systemConfig.additionalFiles %q installs to %q, which image-composer-tool "+
						"generates for the provisioning scripts; choose another path", f.Local, dest)
				}
			}
		}
	}
	return nil
}

// scriptFinalRe keeps a script path safe to place in a systemd ExecStart=
// line: no quotes, backslashes, whitespace, or other characters systemd
// parses. A literal % is allowed and escaped when the unit is rendered.
var scriptFinalRe = regexp.MustCompile(`^/[A-Za-z0-9._@%+/-]*[A-Za-z0-9._@%+-]$`)

// provisioningScriptNameRe keeps script names usable inside a systemd unit
// file name without escaping.
var provisioningScriptNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)

// validateProvisioning checks the provisioning sections of a single layer.
// Values end up in generated files (/etc/environment, apt.conf.d, systemd
// units), so anything that could break out of a line is rejected.
func (t *ImageTemplate) validateProvisioning() error {
	sc := t.SystemConfig
	if err := validateProxyConfig(sc.Proxy); err != nil {
		return err
	}
	if err := validateProvisioningScripts(sc.Provisioning.Scripts); err != nil {
		return err
	}
	if err := validateSSHKeys(sc.Users); err != nil {
		return err
	}
	if err := validateHostname(sc.HostName); err != nil {
		return err
	}
	if err := validateDiskPath(t.Disk.Path); err != nil {
		return err
	}
	switch sc.Bootloader.BootEntryPolicy {
	case "", BootEntryPolicyPreserve, BootEntryPolicyExclusive:
	default:
		return fmt.Errorf("invalid systemConfig.bootloader.bootEntryPolicy %q: must be %q or %q",
			sc.Bootloader.BootEntryPolicy, BootEntryPolicyPreserve, BootEntryPolicyExclusive)
	}
	return nil
}

// proxySchemes are the proxy URL schemes apt and common tools accept.
var proxySchemes = []string{"http", "https", "socks5", "socks5h"}

// unsafeValueChars would break out of a quoted value in a generated file.
const unsafeValueChars = " \t\r\n\"'\\"

// noProxyEntryRe matches one noProxy entry: a host name, domain suffix, IP
// literal, wildcard, or CIDR. An empty entry (trailing comma) is tolerated.
var noProxyEntryRe = regexp.MustCompile(`^[A-Za-z0-9._:/*\[\]-]*$`)

func validateProxyConfig(p ProxyConfig) error {
	for _, f := range []struct{ field, value string }{
		{"httpProxy", p.HTTPProxy}, {"httpsProxy", p.HTTPSProxy}, {"ftpProxy", p.FTPProxy},
	} {
		if f.value == "" {
			continue
		}
		if strings.ContainsAny(f.value, unsafeValueChars) {
			return fmt.Errorf("invalid systemConfig.proxy.%s: must not contain whitespace, quotes, or backslashes",
				f.field)
		}
		u, err := url.Parse(f.value)
		if err != nil || u.Host == "" || !slice.Contains(proxySchemes, u.Scheme) {
			return fmt.Errorf("invalid systemConfig.proxy.%s: must be a URL such as http://proxy.example.com:3128",
				f.field)
		}
		// The proxy is written to world-readable files (/etc/environment) and
		// to the template dump on the ISO, so it must not carry credentials.
		if u.User != nil {
			return fmt.Errorf("invalid systemConfig.proxy.%s: credentials in the proxy URL are not supported; "+
				"use a proxy that authenticates the host, or supply credentials at deployment time", f.field)
		}
		// A query string or fragment can carry a token just as userinfo can.
		if u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(f.value, "?#") {
			return fmt.Errorf("invalid systemConfig.proxy.%s: a query string or fragment is not supported "+
				"in the proxy URL", f.field)
		}
	}
	if strings.ContainsAny(p.NoProxy, unsafeValueChars) {
		return fmt.Errorf("invalid systemConfig.proxy.noProxy: use a comma-separated list without whitespace or quotes")
	}
	for _, entry := range strings.Split(p.NoProxy, ",") {
		if !noProxyEntryRe.MatchString(entry) {
			return fmt.Errorf("invalid systemConfig.proxy.noProxy entry %q: "+
				"use host names, domain suffixes, IP addresses or CIDRs", entry)
		}
	}
	return nil
}

func validateProvisioningScripts(scripts []ProvisioningScript) error {
	seen := make(map[string]bool, len(scripts))
	var prior []ProvisioningScript
	for _, s := range scripts {
		if !provisioningScriptNameRe.MatchString(s.Name) {
			return fmt.Errorf("invalid systemConfig.provisioning.scripts name %q: must match %s",
				s.Name, provisioningScriptNameRe.String())
		}
		if seen[s.Name] {
			return fmt.Errorf("duplicate systemConfig.provisioning.scripts name %q", s.Name)
		}
		seen[s.Name] = true
		for _, o := range prior {
			if s.Final == "" || o.Final == "" {
				continue
			}
			if o.Final == s.Final {
				return fmt.Errorf("systemConfig.provisioning.scripts %q and %q share final path %q",
					o.Name, s.Name, s.Final)
			}
			if InImagePathsOverlap(o.Final, s.Final) {
				return fmt.Errorf("systemConfig.provisioning.scripts %q (%q) and %q (%q): one final path "+
					"is inside the other", o.Name, o.Final, s.Name, s.Final)
			}
		}
		prior = append(prior, s)
		if strings.TrimSpace(s.Local) == "" {
			return fmt.Errorf("systemConfig.provisioning.scripts %q: local is required", s.Name)
		}
		if !isConfinedImagePath(s.Final) || !scriptFinalRe.MatchString(s.Final) {
			return fmt.Errorf("systemConfig.provisioning.scripts %q: final must be a clean absolute in-image file "+
				"path of letters, digits, and ._@%%+- (got %q)", s.Name, s.Final)
		}
		if IsReservedProvisioningPath(s.Final) {
			return fmt.Errorf("systemConfig.provisioning.scripts %q: final %q is generated by image-composer-tool; "+
				"choose another path", s.Name, s.Final)
		}
		if s.Stage != "" && s.Stage != ProvisioningStageFirstBoot && s.Stage != ProvisioningStageEveryBoot {
			return fmt.Errorf("systemConfig.provisioning.scripts %q: stage must be %q or %q",
				s.Name, ProvisioningStageFirstBoot, ProvisioningStageEveryBoot)
		}
		if s.Order < 0 {
			return fmt.Errorf("systemConfig.provisioning.scripts %q: order must be >= 0", s.Name)
		}
	}
	return nil
}

// validateSSHKeys requires every entry to be a single public-key line
// ([options] type base64 [comment]). Errors never echo the value, since a
// private key passed by mistake must not end up in logs.
func validateSSHKeys(users []UserConfig) error {
	for _, u := range users {
		for i, key := range u.SSHAuthorizedKeys {
			if err := validateSSHKeyLine(key); err != nil {
				return fmt.Errorf("invalid sshAuthorizedKeys entry %d for user %q: %w", i+1, u.Name, err)
			}
		}
	}
	return nil
}

// authorizedKeyOptions are the option names sshd accepts in authorized_keys
// (sshd(8), AUTHORIZED_KEYS FILE FORMAT); sshd rejects a line with any other.
var authorizedKeyOptions = []string{
	"agent-forwarding", "cert-authority", "command", "environment", "expiry-time", "from",
	"no-agent-forwarding", "no-port-forwarding", "no-pty", "no-user-rc", "no-x11-forwarding",
	"no-touch-required", "permitlisten", "permitopen", "port-forwarding", "principals", "pty",
	"restrict", "tunnel", "user-rc", "verify-required", "x11-forwarding",
}

// validateSSHKeyLine parses the line with the OpenSSH authorized_keys parser,
// which decodes the full key blob, and checks the option names, so a
// validated template cannot install a key sshd would ignore.
func validateSSHKeyLine(line string) error {
	if strings.ContainsAny(line, "\r\n\x00") {
		return fmt.Errorf("each entry must be one line")
	}
	if strings.Contains(line, "PRIVATE KEY") || strings.HasPrefix(strings.TrimSpace(line), "-----BEGIN") {
		return fmt.Errorf("this looks like a private key; supply the public key (.pub) instead")
	}
	_, _, options, rest, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		return fmt.Errorf("not a valid OpenSSH public key line (expected \"[options] <type> <base64> [comment]\")")
	}
	if len(bytes.TrimSpace(rest)) != 0 {
		return fmt.Errorf("each entry must hold exactly one key")
	}
	for _, opt := range options {
		name, _, _ := strings.Cut(opt, "=")
		if !slice.Contains(authorizedKeyOptions, strings.ToLower(name)) {
			return fmt.Errorf("unsupported authorized_keys option %q", name)
		}
	}
	return nil
}

// hostnameRe is an RFC 1123 host name: dot-separated labels of letters,
// digits, and inner hyphens. The value is written to /etc/hostname and the
// generated cloud-init meta-data, so nothing else is accepted.
var hostnameRe = regexp.MustCompile(
	`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$`)

// diskPathRe limits a target disk to a device node path. The path is
// interpolated into partitioning and efibootmgr commands run as root, so
// shell metacharacters must never reach them.
var diskPathRe = regexp.MustCompile(`^/dev/[A-Za-z0-9._:+/-]+$`)

func validateDiskPath(path string) error {
	if path == "" {
		return nil
	}
	if !diskPathRe.MatchString(path) || filepath.Clean(path) != path {
		return fmt.Errorf("invalid disk.path %q: must be a device path such as /dev/nvme0n1 "+
			"or /dev/disk/by-id/<id>", path)
	}
	return nil
}

func validateHostname(hostname string) error {
	if hostname == "" {
		return nil
	}
	if len(hostname) > 253 || !hostnameRe.MatchString(hostname) {
		return fmt.Errorf("invalid systemConfig.hostname %q: must be an RFC 1123 host name", hostname)
	}
	return nil
}

// validateMergedProvisioning runs checks that need the fully merged template
// (cross-references into packageRepositories, interactions with other
// sections). Hard errors are returned; soft problems are logged as warnings.
func (t *ImageTemplate) validateMergedProvisioning() error {
	policy := t.SystemConfig.AptPolicy
	for _, ref := range policy.UpgradeAllowedRepos {
		if !slices.ContainsFunc(t.PackageRepositories, func(r PackageRepository) bool { return repoMatches(ref, r) }) {
			return fmt.Errorf("systemConfig.aptPolicy.upgradeAllowedRepos references %q, which matches no "+
				"packageRepositories id or codename", ref)
		}
	}
	if err := t.checkAptPolicyOrigins(); err != nil {
		return err
	}
	if !policy.isEmpty() && !slice.Contains(AptPolicyTargetOS, t.Target.OS) {
		return fmt.Errorf("systemConfig.aptPolicy is only supported for ubuntu and debian targets (got %q)",
			t.Target.OS)
	}
	if t.hasSSHKeys() && !t.hasPackage("openssh-server") {
		log.Warnf("sshAuthorizedKeys are configured but openssh-server is not in systemConfig.packages; " +
			"key-based SSH login will not be available unless the base image provides an SSH server")
	}
	if t.SystemConfig.CloudInit.NetworkConfigFile != "" && !isEmptyNetworkConfig(t.SystemConfig.Network) {
		log.Warnf("both systemConfig.cloudInit.networkConfigFile and systemConfig.network are set; " +
			"cloud-init and the declarative network configuration may conflict")
	}
	return nil
}

// checkAptPolicyOrigins rejects a policy that allows one repository and
// denies another on the same host: APT pins repositories by origin (host),
// so both would get conflicting pins and neither intent could be enforced.
func (t *ImageTemplate) checkAptPolicyOrigins() error {
	if len(t.SystemConfig.AptPolicy.UpgradeAllowedRepos) == 0 {
		return nil
	}
	allowed := map[string]string{}
	for _, repo := range t.PackageRepositories {
		if origin := extractOriginFromURL(repo.URL); origin != "" && t.SystemConfig.AptPolicy.RepoUpgradeAllowed(repo) {
			allowed[origin] = repoLabel(repo)
		}
	}
	for _, repo := range t.PackageRepositories {
		origin := extractOriginFromURL(repo.URL)
		if other, ok := allowed[origin]; ok && origin != "" && !t.SystemConfig.AptPolicy.RepoUpgradeAllowed(repo) {
			return fmt.Errorf("systemConfig.aptPolicy: repositories %q (allowed) and %q (not allowed) share the "+
				"origin %s, which APT cannot pin separately; list both in upgradeAllowedRepos or neither",
				other, repoLabel(repo), origin)
		}
	}
	return nil
}

func repoLabel(repo PackageRepository) string {
	if repo.ID != "" {
		return repo.ID
	}
	return repo.Codename
}

func (t *ImageTemplate) hasSSHKeys() bool {
	for _, u := range t.SystemConfig.Users {
		if len(u.SSHAuthorizedKeys) > 0 || len(u.SSHAuthorizedKeysFiles) > 0 {
			return true
		}
	}
	return false
}

func (t *ImageTemplate) hasPackage(name string) bool {
	return slice.Contains(t.SystemConfig.Packages, name)
}

// resolveProvisioningPaths makes the host paths of one template layer
// absolute, relative to that layer's file, so they still resolve after the
// layer is merged with others. Missing files are left untouched here and
// reported by lowerProvisioningInputs; this keeps loading a template dump on
// the target (where host paths do not exist) working.
func resolveProvisioningPaths(t *ImageTemplate, templatePath string) {
	resolve := func(p string) string {
		if p == "" || filepath.IsAbs(p) {
			return p
		}
		found, ok := ResolveTemplateRelativePath([]string{templatePath}, p)
		if !ok {
			return p
		}
		if abs, err := filepath.Abs(found); err == nil {
			return abs
		}
		return found
	}
	sc := &t.SystemConfig
	for i := range sc.Provisioning.Scripts {
		sc.Provisioning.Scripts[i].Local = resolve(sc.Provisioning.Scripts[i].Local)
	}
	sc.CloudInit.UserDataFile = resolve(sc.CloudInit.UserDataFile)
	sc.CloudInit.MetaDataFile = resolve(sc.CloudInit.MetaDataFile)
	sc.CloudInit.NetworkConfigFile = resolve(sc.CloudInit.NetworkConfigFile)
	for i := range sc.CloudInit.ConfigFiles {
		sc.CloudInit.ConfigFiles[i] = resolve(sc.CloudInit.ConfigFiles[i])
	}
	for i := range sc.Users {
		for j := range sc.Users[i].SSHAuthorizedKeysFiles {
			sc.Users[i].SSHAuthorizedKeysFiles[j] = resolve(sc.Users[i].SSHAuthorizedKeysFiles[j])
		}
	}
}

// lowerProvisioningInputs turns host-file inputs into existing primitives so
// every downstream path (raw build, ISO transport, live installer) handles
// them without special cases: scripts and cloud-init files become
// additionalFiles, SSH key files are read into inline keys, and enabling
// cloud-init adds its package. It is idempotent.
func (t *ImageTemplate) lowerProvisioningInputs() error {
	if err := t.lowerSSHKeyFiles(); err != nil {
		return err
	}
	for _, s := range t.SystemConfig.Provisioning.Scripts {
		if err := t.addLoweredFile(s.Local, s.Final, "systemConfig.provisioning.scripts "+s.Name); err != nil {
			return err
		}
	}
	return t.lowerCloudInit()
}

func (t *ImageTemplate) lowerCloudInit() error {
	ci := t.SystemConfig.CloudInit
	if !ci.IsEnabled() {
		if ci.UserDataFile != "" || ci.MetaDataFile != "" || ci.NetworkConfigFile != "" || len(ci.ConfigFiles) > 0 {
			log.Warnf("systemConfig.cloudInit files are set but cloudInit.enabled is not true; ignoring them")
		}
		return nil
	}
	if !t.hasPackage(cloudInitPackage) {
		t.SystemConfig.Packages = append(t.SystemConfig.Packages, cloudInitPackage)
	}
	seeds := []struct{ local, name string }{
		{ci.UserDataFile, "user-data"}, {ci.MetaDataFile, "meta-data"}, {ci.NetworkConfigFile, "network-config"},
	}
	for _, s := range seeds {
		if s.local == "" {
			continue
		}
		owner := "systemConfig.cloudInit " + s.name
		if err := t.addLoweredFile(s.local, CloudInitSeedDir+"/"+s.name, owner); err != nil {
			return err
		}
	}
	// Checked after addLoweredFile has vetted the path (symlink, regular file).
	if err := checkUserDataHeader(ci.UserDataFile); err != nil {
		return err
	}
	for _, f := range ci.ConfigFiles {
		if filepath.Base(f) == CloudInitDatasourceFile {
			return fmt.Errorf("systemConfig.cloudInit.configFiles: %q would be overwritten by the datasource "+
				"file image-composer-tool generates; rename it", f)
		}
		final := CloudInitConfigDir + "/" + filepath.Base(f)
		if err := t.addLoweredFile(f, final, "systemConfig.cloudInit.configFiles"); err != nil {
			return err
		}
	}
	return nil
}

func (t *ImageTemplate) lowerSSHKeyFiles() error {
	for i := range t.SystemConfig.Users {
		u := &t.SystemConfig.Users[i]
		for _, f := range u.SSHAuthorizedKeysFiles {
			if err := addKeysFromFile(u, f); err != nil {
				return err
			}
		}
	}
	return nil
}

// addKeysFromFile appends the public key lines of a host file to a user's
// sshAuthorizedKeys, skipping blank lines, comments, and keys already present.
func addKeysFromFile(u *UserConfig, path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("sshAuthorizedKeysFiles %q for user %q not found", path, u.Name)
	}
	data, err := security.SafeReadFile(path, security.RejectSymlinks)
	if err != nil {
		return fmt.Errorf("reading sshAuthorizedKeysFiles %q for user %q: %w", path, u.Name, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || slice.Contains(u.SSHAuthorizedKeys, line) {
			continue
		}
		if err := validateSSHKeyLine(line); err != nil {
			return fmt.Errorf("sshAuthorizedKeysFiles %q for user %q: %w", path, u.Name, err)
		}
		u.SSHAuthorizedKeys = append(u.SSHAuthorizedKeys, line)
	}
	return nil
}

// addLoweredFile adds (or replaces, by final path) an additionalFiles entry
// for a host file referenced from a provisioning section.
func (t *ImageTemplate) addLoweredFile(local, final, owner string) error {
	if !filepath.IsAbs(local) {
		return fmt.Errorf("%s: file %q not found relative to the template", owner, local)
	}
	info, err := security.CheckSymlink(local, security.RejectSymlinks)
	if err != nil {
		return fmt.Errorf("%s: %w", owner, err)
	}
	if !info.FileInfo.Mode().IsRegular() {
		return fmt.Errorf("%s: %q is not a regular file", owner, local)
	}
	// addUniqueAdditionalFile replaces on a matching final path, which would
	// silently swap one payload for another; refuse distinct sources instead.
	// A path above or below another destination cannot be installed either.
	for _, f := range t.SystemConfig.AdditionalFiles {
		if f.Final == final && f.Local == local {
			continue
		}
		if f.Final == final {
			return fmt.Errorf("%s: %q is also the destination of additional file %q", owner, final, f.Local)
		}
		if InImagePathsOverlap(f.Final, final) {
			return fmt.Errorf("%s: %q overlaps the destination %q of additional file %q",
				owner, final, f.Final, f.Local)
		}
	}
	t.addUniqueAdditionalFile(AdditionalFileInfo{Local: local, Final: final})
	return nil
}

// finalizeProvisioning validates the merged provisioning sections and lowers
// their host-file inputs. Called once on the fully merged create-mode template.
func (t *ImageTemplate) finalizeProvisioning() error {
	if err := t.validateProvisioning(); err != nil {
		return err
	}
	if err := t.validateMergedProvisioning(); err != nil {
		return err
	}
	if err := t.lowerProvisioningInputs(); err != nil {
		return err
	}
	return t.validateAdditionalFilesNotGenerated()
}

// userDataHeaders are the first-line markers cloud-init recognises.
var userDataHeaders = []string{
	"#cloud-config", "#!", "#include", "#cloud-boothook", "## template: jinja", "Content-Type:",
}

// checkUserDataHeader rejects user-data cloud-init would silently ignore:
// it must be a #cloud-config document, a script (#!), or another format
// cloud-init recognises by its first line.
func checkUserDataHeader(path string) error {
	if path == "" || !filepath.IsAbs(path) {
		return nil // reported as not found by addLoweredFile
	}
	data, err := security.SafeReadFile(path, security.RejectSymlinks)
	if err != nil {
		return fmt.Errorf("reading systemConfig.cloudInit.userDataFile %q: %w", path, err)
	}
	first, _, _ := strings.Cut(string(data), "\n")
	first = strings.TrimSpace(first)
	for _, prefix := range userDataHeaders {
		if strings.HasPrefix(first, prefix) {
			return nil
		}
	}
	return fmt.Errorf("systemConfig.cloudInit.userDataFile %q must start with #cloud-config, #! or another "+
		"cloud-init user-data header (got %q)", path, first)
}

// DiskSelectionStrategies are the accepted disk.selectionPolicy.strategy
// values. They mirror imagedisc.DiskSelectStrategy* and the schema enum;
// config cannot import imagedisc, which depends on it.
var DiskSelectionStrategies = []string{"first", "largest", "fastest"}

// SSHKeyOverride adds the public keys in File to an existing template user.
type SSHKeyOverride struct {
	User, File string
}

// ProxyOverrides are proxy settings supplied on the command line. A nil field
// was not set and leaves the template value alone; a pointer to an empty
// string was set explicitly and clears it.
type ProxyOverrides struct {
	HTTPProxy  *string
	HTTPSProxy *string
	FTPProxy   *string
	NoProxy    *string
}

func (o ProxyOverrides) isEmpty() bool {
	return o.HTTPProxy == nil && o.HTTPSProxy == nil && o.FTPProxy == nil && o.NoProxy == nil
}

// CompositionOverrides are composition settings supplied on the command
// line. Disk, hostname, and SSH key values override the template when
// non-empty; proxy values override it whenever they are set, even to empty.
type CompositionOverrides struct {
	DiskStrategy string
	Hostname     string
	Proxy        ProxyOverrides
	SSHKeys      []SSHKeyOverride
}

func (o CompositionOverrides) isEmpty() bool {
	return o.DiskStrategy == "" && o.Hostname == "" && o.Proxy.isEmpty() && len(o.SSHKeys) == 0
}

// ApplyCompositionOverrides applies CLI overrides to a merged template and
// validates the overridden values the same way template values are.
func (t *ImageTemplate) ApplyCompositionOverrides(o CompositionOverrides) error {
	if o.isEmpty() {
		return nil
	}
	if t.IsOverlayMode() && (o.DiskStrategy != "" || o.Hostname != "" || !o.Proxy.isEmpty()) {
		return fmt.Errorf("disk, hostname, and proxy overrides are not supported for overlay-mode templates")
	}
	if err := t.applyDiskStrategyOverride(o.DiskStrategy); err != nil {
		return err
	}
	if o.Hostname != "" {
		if err := validateHostname(o.Hostname); err != nil {
			return err
		}
		t.SystemConfig.HostName = o.Hostname
	}
	if !o.Proxy.isEmpty() {
		applyProxyOverride(&t.SystemConfig.Proxy, o.Proxy)
		if err := validateProxyConfig(t.SystemConfig.Proxy); err != nil {
			return err
		}
	}
	for _, k := range o.SSHKeys {
		u := t.findUser(k.User)
		if u == nil {
			return fmt.Errorf("SSH key override for user %q: no such user in systemConfig.users", k.User)
		}
		abs, err := filepath.Abs(k.File)
		if err != nil {
			return fmt.Errorf("SSH key override for user %q: %w", k.User, err)
		}
		if err := addKeysFromFile(u, abs); err != nil {
			return err
		}
	}
	// Overrides can enable a generated file (a proxy, or SSH keys that make a
	// sudo user key-only) after finalizeProvisioning checked the template.
	return t.validateAdditionalFilesNotGenerated()
}

// applyDiskStrategyOverride sets the disk selection strategy. It clears any
// template disk.path, because an explicit path always takes precedence over
// the selection policy.
func (t *ImageTemplate) applyDiskStrategyOverride(strategy string) error {
	if strategy == "" {
		return nil
	}
	if !slice.Contains(DiskSelectionStrategies, strategy) {
		return fmt.Errorf("invalid disk selection strategy %q: must be one of %s",
			strategy, strings.Join(DiskSelectionStrategies, ", "))
	}
	t.Disk.SelectionPolicy.Strategy = strategy
	if t.Disk.Path != "" {
		log.Infof("Disk strategy %q overrides the template disk.path %s", strategy, t.Disk.Path)
		t.Disk.Path = ""
	}
	return nil
}

func applyProxyOverride(dst *ProxyConfig, o ProxyOverrides) {
	for _, f := range []struct {
		dst *string
		src *string
	}{
		{&dst.HTTPProxy, o.HTTPProxy}, {&dst.HTTPSProxy, o.HTTPSProxy},
		{&dst.FTPProxy, o.FTPProxy}, {&dst.NoProxy, o.NoProxy},
	} {
		if f.src != nil {
			*f.dst = *f.src
		}
	}
}

func (t *ImageTemplate) findUser(name string) *UserConfig {
	for i := range t.SystemConfig.Users {
		if t.SystemConfig.Users[i].Name == name {
			return &t.SystemConfig.Users[i]
		}
	}
	return nil
}

// adminGroups are the groups that grant sudo through the distributions' default
// sudoers policy: "sudo" on Debian-family systems, "wheel" on RPM-based ones.
var adminGroups = []string{"sudo", "wheel"}

// HasSudoAccess reports whether the user is granted sudo, either by the
// sudo flag or by membership of an admin group listed in groups.
func (u UserConfig) HasSudoAccess() bool {
	return u.Sudo || slices.ContainsFunc(u.Groups, func(g string) bool {
		return slices.Contains(adminGroups, strings.TrimSpace(g))
	})
}

// IsValidUnixUserName reports whether name satisfies the account-name contract
// enforced for systemConfig.users.
func IsValidUnixUserName(name string) bool {
	return unixUserNameRe.MatchString(name)
}

// IsPrivileged reports whether the account is created with elevated rights:
// sudo access, or the root account. A root account whose login shell is a
// startupScript is exempt, being the installer environment's console account,
// confined to running that script.
//
// Only a privileged account is required to carry a credential, so this is the
// predicate both ValidateUserCredentials and the Web UI's prompt select on.
func (u UserConfig) IsPrivileged() bool {
	return u.HasSudoAccess() || (u.Name == "root" && u.StartupScript == "")
}

// NeedsCredential reports whether the account would be created with an empty
// password: privileged, but carrying neither a password nor an SSH authorized
// key.
//
// Exported so a caller that wants to *offer* a credential (the Web UI, via the
// compose response) decides with the same rule that ValidateUserCredentials
// rejects by, and the two can never disagree about which accounts need one.
//
// Only the inline SSHAuthorizedKeys count. By the time this runs,
// lowerSSHKeyFiles has read every sshAuthorizedKeysFiles entry into that slice,
// skipping blanks and comments — so a key file containing only comments
// contributes nothing and the account still needs a credential.
func (u UserConfig) NeedsCredential() bool {
	return u.IsPrivileged() && u.Password == "" && len(u.SSHAuthorizedKeys) == 0
}

// ValidateUserCredentials rejects a privileged user that has neither a
// password nor an SSH authorized key: such an account would be created with an
// empty password.
func ValidateUserCredentials(users []UserConfig) error {
	for _, u := range users {
		if u.NeedsCredential() {
			return fmt.Errorf("user %s has root or sudo access but no password or SSH authorized key; set password "+
				"(or sshAuthorizedKeys/sshAuthorizedKeysFiles, or --ssh-authorized-key %s=FILE) so the "+
				"account is not left with an empty password", u.Name, u.Name)
		}
	}
	return nil
}

// ValidateSSHAuthorizedKey checks a single authorized_keys line supplied by a
// caller outside this package (the Web UI posts one directly, rather than
// pointing at a file the way --ssh-authorized-key does).
func ValidateSSHAuthorizedKey(line string) error {
	return validateSSHKeyLine(line)
}
