package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/config/validate"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/security"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/slice"
	"gopkg.in/yaml.v3"
)

type ImageInfo struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
}

type TargetInfo struct {
	OS        string `yaml:"os"`
	Dist      string `yaml:"dist"`
	Arch      string `yaml:"arch"`
	ImageType string `yaml:"imageType"`
}

type ArtifactInfo struct {
	Type        string `yaml:"type"`
	Compression string `yaml:"compression"`
}

type DiskSelectionPolicy struct {
	Strategy string `yaml:"strategy,omitempty"`
	// ExcludeRemovable is intentionally conservative for unattended installs and
	// excludes disks that appear externally attached, not only devices with RM=1.
	ExcludeRemovable *bool `yaml:"excludeRemovable,omitempty"`
	// RequireEmpty restricts unattended disk selection to empty disks when true.
	// If false, disks with existing partitions are eligible and may be overwritten.
	RequireEmpty *bool `yaml:"requireEmpty,omitempty"`
}

type DiskConfig struct {
	Name               string              `yaml:"name"`
	Path               string              `yaml:"path"` // Path to the disk device (e.g., /dev/sda), used by live installer
	SelectionPolicy    DiskSelectionPolicy `yaml:"selectionPolicy,omitempty"`
	Artifacts          []ArtifactInfo      `yaml:"artifacts"`
	Size               string              `yaml:"size"`
	PartitionTableType string              `yaml:"partitionTableType"`
	Partitions         []PartitionInfo     `yaml:"partitions"`
	// ExtendLastPartitionToFillDisk forces the final partition's end to "0"
	// (consume all remaining disk space) when enabled.
	ExtendLastPartitionToFillDisk bool `yaml:"extendLastPartitionToFillDisk,omitempty"`
}

type PackageRepository struct {
	ID                 string   `yaml:"id,omitempty"`                 // Auto-assigned
	Codename           string   `yaml:"codename"`                     // Repository identifier/codename
	URL                string   `yaml:"url,omitempty"`                // Repository base URL
	Path               string   `yaml:"path,omitempty"`               // Local directory path for file-based repositories
	Packages           []string `yaml:"packages,omitempty"`           // Files to copy/download into Path for local repositories (HTTPS URLs or local file paths)
	InsecureSkipVerify bool     `yaml:"insecureSkipVerify,omitempty"` // Skip TLS certificate verification for packages URL downloads (insecure, use with caution)
	PKey               string   `yaml:"pkey"`                         // Public GPG key URL for verification
	PKeys              []string `yaml:"pkeys,omitempty"`              // Multiple public GPG key URLs for verification
	Component          string   `yaml:"component,omitempty"`          // Repository component (e.g., "main", "restricted")
	Priority           int      `yaml:"priority,omitempty"`           // Repository priority (higher numbers = higher priority)
	AllowPackages      []string `yaml:"allowPackages,omitempty"`      // Optional: specific packages to include from this repo (pinning)
}

// ProviderRepoConfig represents the repository configuration for a provider
type ProviderRepoConfig struct {
	Name         string   `yaml:"name"`
	Type         string   `yaml:"type"` // Repository type: "rpm" or "deb"
	BaseURL      string   `yaml:"baseURL"`
	PkgPrefix    string   `yaml:"pkgPrefix"`
	ReleaseFile  string   `yaml:"releaseFile"`
	ReleaseSign  string   `yaml:"releaseSign"`
	PbGPGKey     string   `yaml:"pbGPGKey"` // For DEB repositories (eLxr)
	GPGKey       string   `yaml:"gpgKey"`   // For RPM repositories (azl, emt)
	GPGKeys      []string `yaml:"gpgKeys,omitempty"`
	GPGCheck     bool     `yaml:"gpgCheck"`
	RepoGPGCheck bool     `yaml:"repoGPGCheck"`
	Enabled      bool     `yaml:"enabled"`
	Component    string   `yaml:"component"` // Repository component/section identifier
	BuildPath    string   `yaml:"buildPath"`
}

// ProviderRepoConfigs represents multiple repository configurations for a provider
type ProviderRepoConfigs struct {
	Repositories []ProviderRepoConfig `yaml:"repositories"`
}

// Baseline-mode constants.
const (
	BaselineModeCreate  = "create"
	BaselineModeOverlay = "overlay"

	// Baseline image formats accepted for overlay mode. Non-RAW formats are
	// converted to RAW (via qemu-img) before the baseline is loop-attached.
	BaselineFormatRaw   = "raw"
	BaselineFormatQcow2 = "qcow2"
	BaselineFormatVHD   = "vhd"
	BaselineFormatVHDX  = "vhdx"

	OverlayPackageOpAdditiveOnly = "additive-only"
	// OverlayPackageOpAdditiveAndUpgrade permits, in addition to adding new
	// packages, upgrading a package already installed in the baseline to a newer
	// version. Downgrades and removals remain blocked. Opting in flips the
	// internal OverlayPolicy.AllowUpgrade gate (see OverlayPolicy.validate).
	OverlayPackageOpAdditiveAndUpgrade = "additive-and-upgrade"

	OverlayConflictPolicyFail          = "fail"
	OverlayConflictPolicyAllowExplicit = "allow-explicit"
)

// Baseline configures whether the image is built from scratch ("create") or
// constructed by overlaying packages onto an existing baseline image ("overlay").
type Baseline struct {
	Mode   string          `yaml:"mode,omitempty"`
	Source *BaselineSource `yaml:"source,omitempty"`
}

// BaselineSource describes the source baseline image for overlay mode.
// v1 supports a local RAW disk image, referenced either by a local filesystem
// path or by an http(s) URL that is downloaded before the overlay runs.
// Exactly one of Path or URL must be set.
type BaselineSource struct {
	Path   string `yaml:"path,omitempty"`
	URL    string `yaml:"url,omitempty"`
	Format string `yaml:"format,omitempty"`
}

// OverlayPolicy controls how overlay-mode preflight classifies and gates
// package operations against the baseline image.
type OverlayPolicy struct {
	PackageOperation string `yaml:"packageOperation,omitempty"`
	ConflictPolicy   string `yaml:"conflictPolicy,omitempty"`
	KernelCmdline    string `yaml:"kernelCmdline,omitempty"`

	// GrubDefault, when set, is written verbatim to GRUB_DEFAULT in
	// /etc/default/grub before the GRUB config is regenerated, pinning the boot
	// menu entry that becomes the default (e.g. an Ubuntu submenu path such as
	// "Advanced options for Ubuntu>Ubuntu, with Linux 6.8.0-40-generic"). It is
	// only applied on a GRUB2 baseline. Used to make an overlay-added kernel the
	// default boot target when it is not the entry GRUB would auto-select.
	GrubDefault string `yaml:"grubDefault,omitempty"`

	// AllowDiskResize gates whether an overlay build may grow the baseline image
	// to satisfy a larger disk.size. Overlay mode preserves the baseline layout by
	// default, so a disk.size larger than the baseline is rejected unless the user
	// opts in here. It never permits shrinking; resize stays grow-only.
	AllowDiskResize bool `yaml:"allowDiskResize,omitempty"`

	// AllowRemoval gates whether preflight permits removing a baseline package.
	// It is intentionally NOT a YAML field, and the schema rejects it via
	// additionalProperties:false, so it always carries its zero value (false):
	// overlay mode is additive-only in v1. A future release can lift the
	// restriction by surfacing this field in the schema/YAML.
	AllowRemoval bool `yaml:"-"`

	// AllowDowngrade gates whether preflight permits downgrading a baseline
	// package to an older version. Like AllowRemoval it is intentionally NOT a
	// YAML field (the schema rejects it via additionalProperties:false) and
	// always carries its zero value (false): overlay mode is additive-only in
	// v1, so downgrades are blocked by default. A future release can surface it.
	AllowDowngrade bool `yaml:"-"`

	// AllowUpgrade gates whether preflight permits upgrading a baseline package
	// to a newer version. Like AllowRemoval/AllowDowngrade it is intentionally
	// NOT a YAML field (the schema rejects it via additionalProperties:false);
	// it is instead derived from PackageOperation by validate(), which sets it
	// true when packageOperation is "additive-and-upgrade" and leaves it false
	// (the default) for "additive-only". Enabling it lets the deb backend replace
	// an installed package in place (dpkg -i upgrades), and switches the rpm
	// backend to `rpm -U`; downgrades and removals stay blocked regardless.
	AllowUpgrade bool `yaml:"-"`
}

// ImageTemplate represents the YAML image template structure
type ImageTemplate struct {
	Extends             string              `yaml:"extends,omitempty"`
	Image               ImageInfo           `yaml:"image"`
	Target              TargetInfo          `yaml:"target"`
	Baseline            *Baseline           `yaml:"baseline,omitempty"`
	OverlayPolicy       *OverlayPolicy      `yaml:"overlayPolicy,omitempty"`
	Disk                DiskConfig          `yaml:"disk,omitempty"`
	SystemConfig        SystemConfig        `yaml:"systemConfig"`
	PackageRepositories []PackageRepository `yaml:"packageRepositories,omitempty"`

	// Explicitly excluded from YAML serialization/deserialization
	PathList            []string                `yaml:"-"`
	BootloaderPkgList   []string                `yaml:"-"`
	EssentialPkgList    []string                `yaml:"-"`
	KernelPkgList       []string                `yaml:"-"`
	FullPkgList         []string                `yaml:"-"`
	FullPkgListBom      []ospackage.PackageInfo `yaml:"-"`
	SBOMPackageMetadata []ospackage.PackageInfo `yaml:"sbomPackageMetadata,omitempty"`
	DotFilePath         string                  `yaml:"-"`
	DotSystemOnly       bool                    `yaml:"-"`
	// InspectEnabled toggles post-build image inspection for overlay builds. It is
	// driven by the CLI --inspect/--no-inspect flags (default on) rather than YAML,
	// so it is excluded from serialization. Consumed by the overlay postprocess
	// inspection stage.
	InspectEnabled       bool `yaml:"-"`
	pureBuildStart       time.Time
	pureBuildDuration    time.Duration
	downloadPkgsStart    time.Time
	downloadPkgsDuration time.Duration
	convertImageStart    time.Time
	convertImageDuration time.Duration
	chrootPkgDlStart     time.Time
	chrootPkgDlDuration  time.Duration
	buildTimelineStart   time.Time
	buildFinishedAt      time.Time
}

// PackageSource identifies why a package was requested in the merged template.
type PackageSource string

const (
	PackageSourceUnknown    PackageSource = "unknown"
	PackageSourceEssential  PackageSource = "essential"
	PackageSourceKernel     PackageSource = "kernel"
	PackageSourceSystem     PackageSource = "system"
	PackageSourceBootloader PackageSource = "bootloader"
)

type Initramfs struct {
	Template string `yaml:"template"` // Template: path to the initramfs configuration template file
}

type Bootloader struct {
	BootType string `yaml:"bootType"` // BootType: type of bootloader (e.g., "efi", "legacy")
	Provider string `yaml:"provider"` // Provider: bootloader provider (e.g., "grub2", "systemd-boot")
}

// ImmutabilityConfig holds the immutability configuration
type ImmutabilityConfig struct {
	Enabled         bool   `yaml:"enabled"`                   // Enabled: whether immutability is enabled (default: false)
	SecureBootDBKey string `yaml:"secureBootDBKey,omitempty"` // SecureBootDBKey: The private key file used to sign the bootloader for UEFI Secure Boot
	SecureBootDBCrt string `yaml:"secureBootDBCrt,omitempty"` // SecureBootDBCrt: The certificate file in PEM format, which corresponds to the private key for UEFI Secure Boot
	SecureBootDBCer string `yaml:"secureBootDBCer,omitempty"` // SecureBootDBCer: The same certificate file, but provided in DER (binary) format specifically for UEFI firmware
	wasProvided     bool   `yaml:"-"`                         // Internal flag to track if section was provided
}

// UserConfig holds the user configuration
type UserConfig struct {
	Name           string   `yaml:"name"`                     // Name: username for the user account
	Password       string   `yaml:"password,omitempty"`       // Password: plain text password (discouraged for security)
	HashAlgo       string   `yaml:"hash_algo,omitempty"`      // HashAlgo: algorithm to be used to hash the password (e.g., "sha512", "bcrypt")
	PasswordMaxAge int      `yaml:"passwordMaxAge,omitempty"` // PasswordMaxAge: maximum password age in days (like /etc/login.defs PASS_MAX_DAYS)
	StartupScript  string   `yaml:"startupScript,omitempty"`  // StartupScript: shell/script to run on login
	Groups         []string `yaml:"groups,omitempty"`         // Groups: additional groups to add user to
	Sudo           bool     `yaml:"sudo,omitempty"`           // Sudo: whether to grant sudo permissions
	Home           string   `yaml:"home,omitempty"`           // Home: custom home directory path
	Shell          string   `yaml:"shell,omitempty"`          // Shell: login shell (e.g., /bin/bash, /bin/zsh)
}

// NetworkRoute represents a static route entry
type NetworkRoute struct {
	To  string `yaml:"to"`  // To: destination (e.g., "default", "10.0.0.0/8")
	Via string `yaml:"via"` // Via: gateway address (e.g., "10.0.0.1")
}

// NetworkInterface represents a single network interface configuration
type NetworkInterface struct {
	Name        string         `yaml:"name"`                  // Name: interface name (e.g., enp1s0, ens3)
	DHCP4       *bool          `yaml:"dhcp4,omitempty"`       // DHCP4: enable DHCPv4
	DHCP6       *bool          `yaml:"dhcp6,omitempty"`       // DHCP6: enable DHCPv6
	Addresses   []string       `yaml:"addresses,omitempty"`   // Addresses: static IPv4/IPv6 addresses (e.g., "192.168.1.10/24")
	Routes      []NetworkRoute `yaml:"routes,omitempty"`      // Routes: static routes (replaces deprecated gateway4/gateway6)
	Nameservers []string       `yaml:"nameservers,omitempty"` // Nameservers: DNS server addresses
}

// NetworkConfig represents the network configuration for the installed OS
type NetworkConfig struct {
	Backend    string             `yaml:"backend,omitempty"`    // Backend: network backend (netplan or systemd-networkd)
	Interfaces []NetworkInterface `yaml:"interfaces,omitempty"` // Interfaces: list of interfaces to configure
}

// FDEConfig holds the full-disk-encryption configuration
type FDEConfig struct {
	Enabled        bool     `yaml:"enabled"`                  // Enabled: whether full-disk encryption is enabled (default: false)
	PassphraseFile string   `yaml:"passphraseFile,omitempty"` // PassphraseFile: local file containing the passphrase
	Partitions     []string `yaml:"partitions,omitempty"`     // Partitions: disk partition IDs to encrypt (e.g., "rootfs", "userdata")
	Unlock         string   `yaml:"unlock,omitempty"`         // Unlock: boot unlock mode, "auto" (keyfile, no prompt; default) or "manual" (interactive passphrase)

	// Passphrase is resolved from PassphraseFile at load time and is intentionally
	// never read from YAML to avoid plaintext secrets in templates.
	Passphrase string `yaml:"-"`
}

// SystemConfig represents a system configuration within the template
type SystemConfig struct {
	Name            string               `yaml:"name"`
	Description     string               `yaml:"description"`
	Initramfs       Initramfs            `yaml:"initramfs,omitempty"`
	HostName        string               `yaml:"hostname,omitempty"`
	Immutability    ImmutabilityConfig   `yaml:"immutability,omitempty"`
	FDE             FDEConfig            `yaml:"fde,omitempty"`
	Users           []UserConfig         `yaml:"users,omitempty"`
	Bootloader      Bootloader           `yaml:"bootloader"`
	Network         NetworkConfig        `yaml:"network,omitempty"`
	Packages        []string             `yaml:"packages"`
	AdditionalFiles []AdditionalFileInfo `yaml:"additionalFiles"`
	Configurations  []ConfigurationInfo  `yaml:"configurations"`
	Kernel          KernelConfig         `yaml:"kernel"`
}

// AdditionalFileInfo holds information about local file and final path to be placed in the image
type AdditionalFileInfo struct {
	Local string `yaml:"local"` // path to the file on the host system
	Final string `yaml:"final"` // path where the file should be placed in the image
}

// ConfigurationInfo holds information about instructions to execute during system configuration
type ConfigurationInfo struct {
	Cmd string `yaml:"cmd"`
}

// KernelConfig holds the kernel configuration
type KernelConfig struct {
	Version            string   `yaml:"version"`
	Cmdline            string   `yaml:"cmdline"`
	Packages           []string `yaml:"packages"`
	UKI                bool     `yaml:"uki,omitempty"`
	EnableExtraModules string   `yaml:"enableExtraModules"`
}

// PartitionInfo holds information about a partition in the disk layout
type PartitionInfo struct {
	Name         string   `yaml:"name"`            // Name: label for the partition
	ID           string   `yaml:"id"`              // ID: unique identifier for the partition; can be used as a key
	Index        *int     `yaml:"index,omitempty"` // Index: index for the partition sdx (x = 1, 2, 3, 4, ...)
	Flags        []string `yaml:"flags"`           // Flags: optional flags for the partition (e.g., "boot", "hidden")
	Type         string   `yaml:"type"`            // Type: partition type (e.g., "esp", "linux-root-amd64")
	TypeGUID     string   `yaml:"typeUUID"`        // TypeGUID: GPT type GUID for the partition (e.g., "8300" for Linux filesystem)
	FsType       string   `yaml:"fsType"`          // FsType: filesystem type (e.g., "ext4", "xfs", etc.);
	FsLabel      string   `yaml:"fsLabel"`         // FsLabel: filesystem label (e.g., "cloudimg-rootfs")
	Start        string   `yaml:"start"`           // Start: start offset of the partition; can be a absolute size (e.g., "512MiB")
	End          string   `yaml:"end"`             // End: end offset of the partition; can be a absolute size (e.g., "2GiB") or "0" for the end of the disk
	MountPoint   string   `yaml:"mountPoint"`      // MountPoint: optional mount point for the partition (e.g., "/boot", "/rootfs")
	MountOptions string   `yaml:"mountOptions"`    // MountOptions: optional mount options for the partition (e.g., "defaults", "noatime")
}

var log = logger.Logger()

var invalidBlockScalarHeaderPattern = regexp.MustCompile(`(: \|)\d+([+-]?)\n`)

// LoadTemplate loads an ImageTemplate from the specified YAML template path
func LoadTemplate(path string, validateFull bool) (*ImageTemplate, error) {

	// Use safe file reading to prevent symlink attacks
	data, err := security.SafeReadFile(path, security.RejectSymlinks)
	if err != nil {
		log.Errorf("Failed to read template file: %v", err)
		return nil, fmt.Errorf("failed to read template file: %w", err)
	}

	// Only support YAML/YML files
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".yml" && ext != ".yaml" {
		log.Errorf("Unsupported file format: %s", ext)
		return nil, fmt.Errorf("unsupported file format: %s (only .yml and .yaml are supported)", ext)
	}

	template, err := parseYAMLTemplate(data, validateFull)
	if err != nil {
		return nil, fmt.Errorf("failed to load template: %w", err)
	}

	if err := resolveFDEPassphrase(template, path); err != nil {
		return nil, err
	}

	// Store the template path info
	canonicalPath, absErr := filepath.Abs(path)
	if absErr != nil {
		canonicalPath = path
	}

	if !slice.Contains(template.PathList, canonicalPath) {
		template.PathList = append(template.PathList, canonicalPath)
	}

	log.Infof("Loaded image template from %s: name=%s, os=%s, dist=%s, arch=%s",
		path, template.Image.Name, template.Target.OS, template.Target.Dist, template.Target.Arch)
	return template, nil
}

// parseYAMLTemplate loads an ImageTemplate from YAML data
func parseYAMLTemplate(data []byte, validateFull bool) (*ImageTemplate, error) {
	// Parse YAML to generic interface for validation
	var raw interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		log.Errorf("Invalid YAML format: template parsing failed: %v", err)
		return nil, fmt.Errorf("invalid YAML format: template parsing failed: %w", err)
	}

	if err := security.ValidateStructStrings(&raw, security.DefaultLimits()); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	// Convert to JSON for schema validation
	jsonData, err := json.Marshal(raw)
	if err != nil {
		log.Errorf("Template validation error: unable to process template: %v", err)
		return nil, fmt.Errorf("template validation error: unable to process template: %w", err)
	}

	if validateFull {
		// Validate against image template schema
		if err := validate.ValidateImageTemplateJSON(jsonData); err != nil {
			return nil, fmt.Errorf("template validation error: %w", err)
		}
	} else {
		if err := validate.ValidateUserTemplateJSON(jsonData); err != nil {
			return nil, fmt.Errorf("template validation error: %w", err)
		}
	}

	// Parse into template structure
	var template ImageTemplate
	if err := yaml.Unmarshal(data, &template); err != nil {
		log.Errorf("Template parsing failed: invalid structure: %v", err)
		return nil, fmt.Errorf("template parsing failed: invalid structure: %w", err)
	}

	if err := template.validatePackageRepositories(); err != nil {
		return nil, err
	}

	if err := template.validateBaseline(); err != nil {
		return nil, err
	}

	return &template, nil
}

// GetProviderName returns the provider name for the given template
func (t *ImageTemplate) GetProviderName() string {
	// Map OS/dist combinations to provider names
	providerMap := map[string]map[string]string{
		"azure-linux": {"azl3": "AzureLinux3"},
		"emt":         {"emt3": "EMT3.0"},
		"elxr":        {"elxr12": "eLxr12"},
		"ubuntu":      {"ubuntu24": "Ubuntu24", "ubuntu26": "Ubuntu26"},
	}

	if providers, ok := providerMap[t.Target.OS]; ok {
		if provider, ok := providers[t.Target.Dist]; ok {
			return provider
		}
	}
	return ""
}

// GetDistroVersion returns the version string expected by providers
func (t *ImageTemplate) GetDistroVersion() string {
	versionMap := map[string]string{
		"azl3":     "3",
		"emt3":     "3.0",
		"elxr12":   "12",
		"ubuntu24": "24.04",
		"ubuntu26": "26.04",
	}
	return versionMap[t.Target.Dist]
}

func (t *ImageTemplate) GetImageName() string {
	return t.Image.Name
}

// StartPureImageBuildTimer starts tracking the pure image build time window.
func (t *ImageTemplate) StartPureImageBuildTimer() {
	if t == nil {
		return
	}

	t.pureBuildStart = time.Now()
	t.pureBuildDuration = 0
}

// FinishPureImageBuildTimer stores the elapsed pure image build time if tracking was started.
func (t *ImageTemplate) FinishPureImageBuildTimer() {
	if t == nil || t.pureBuildStart.IsZero() {
		return
	}

	t.pureBuildDuration = time.Since(t.pureBuildStart)
}

// GetPureImageBuildDuration returns the tracked pure image build duration.
func (t *ImageTemplate) GetPureImageBuildDuration() time.Duration {
	if t == nil {
		return 0
	}

	return t.pureBuildDuration
}

// StartBuildTimeline starts the overall build timeline at the provided timestamp.
func (t *ImageTemplate) StartBuildTimeline(buildTimelineStart time.Time) {
	if t == nil {
		return
	}

	t.buildTimelineStart = buildTimelineStart
	t.buildFinishedAt = time.Time{}
}

// MarkBuildFinished marks the overall build timeline end.
func (t *ImageTemplate) MarkBuildFinished() {
	if t == nil {
		return
	}

	t.buildFinishedAt = time.Now()
}

// StartDownloadImagePkgsTimer starts tracking downloadImagePkgs duration.
func (t *ImageTemplate) StartDownloadImagePkgsTimer() {
	if t == nil {
		return
	}

	t.downloadPkgsStart = time.Now()
	t.downloadPkgsDuration = 0
}

// FinishDownloadImagePkgsTimer stores elapsed downloadImagePkgs duration if tracking was started.
func (t *ImageTemplate) FinishDownloadImagePkgsTimer() {
	if t == nil || t.downloadPkgsStart.IsZero() {
		return
	}

	t.downloadPkgsDuration = time.Since(t.downloadPkgsStart)
	t.chrootPkgDlStart = time.Now()
	t.chrootPkgDlDuration = 0
}

// FinishChrootPkgDownloadTimer stores elapsed chroot package download wait time if tracking was started.
func (t *ImageTemplate) FinishChrootPkgDownloadTimer() {
	if t == nil || t.chrootPkgDlStart.IsZero() {
		return
	}

	t.chrootPkgDlDuration = time.Since(t.chrootPkgDlStart)
}

// GetChrootPkgDownloadDuration returns tracked chroot package download wait duration.
func (t *ImageTemplate) GetChrootPkgDownloadDuration() time.Duration {
	if t == nil {
		return 0
	}

	return t.chrootPkgDlDuration
}

// GetDownloadImagePkgsDuration returns tracked downloadImagePkgs duration.
func (t *ImageTemplate) GetDownloadImagePkgsDuration() time.Duration {
	if t == nil {
		return 0
	}

	return t.downloadPkgsDuration
}

// GetDurationStartToDownloadImagePkgs returns the gap from build start to downloadImagePkgs start.
func (t *ImageTemplate) GetDurationStartToDownloadImagePkgs() time.Duration {
	if t == nil || t.buildTimelineStart.IsZero() || t.downloadPkgsStart.IsZero() {
		return 0
	}

	d := t.downloadPkgsStart.Sub(t.buildTimelineStart)
	if d < 0 {
		return 0
	}

	return d
}

// GetDurationDownloadImagePkgsToPureBuild returns the gap from downloadImagePkgs end to pure build start.
func (t *ImageTemplate) GetDurationDownloadImagePkgsToPureBuild() time.Duration {
	if t == nil || t.downloadPkgsStart.IsZero() || t.downloadPkgsDuration <= 0 || t.pureBuildStart.IsZero() {
		return 0
	}

	downloadEnd := t.downloadPkgsStart.Add(t.downloadPkgsDuration)
	d := t.pureBuildStart.Sub(downloadEnd)
	if d < 0 {
		return 0
	}

	if t.chrootPkgDlDuration > 0 {
		d -= t.chrootPkgDlDuration
		if d < 0 {
			return 0
		}
	}

	return d
}

// GetDurationConvertImageFileToFinish returns the gap from convertImageFile end to build finish.
func (t *ImageTemplate) GetDurationConvertImageFileToFinish() time.Duration {
	if t == nil || t.convertImageStart.IsZero() || t.convertImageDuration <= 0 || t.buildFinishedAt.IsZero() {
		return 0
	}

	convertEnd := t.convertImageStart.Add(t.convertImageDuration)
	d := t.buildFinishedAt.Sub(convertEnd)
	if d < 0 {
		return 0
	}

	return d
}

// StartConvertImageTimer starts tracking image conversion time.
func (t *ImageTemplate) StartConvertImageTimer() {
	if t == nil {
		return
	}

	t.convertImageStart = time.Now()
	t.convertImageDuration = 0
}

// FinishConvertImageTimer stores elapsed image conversion time if tracking was started.
func (t *ImageTemplate) FinishConvertImageTimer() {
	if t == nil || t.convertImageStart.IsZero() {
		return
	}

	t.convertImageDuration = time.Since(t.convertImageStart)
}

// GetConvertImageDuration returns tracked image conversion duration.
func (t *ImageTemplate) GetConvertImageDuration() time.Duration {
	if t == nil {
		return 0
	}

	return t.convertImageDuration
}

func (t *ImageTemplate) GetTargetInfo() TargetInfo {
	return t.Target
}

// Updated methods to work with single objects instead of arrays
func (t *ImageTemplate) GetDiskConfig() DiskConfig {
	return t.Disk
}

func (t *ImageTemplate) GetSystemConfig() SystemConfig {
	return t.SystemConfig
}

func (t *ImageTemplate) GetInitramfsTemplate() (string, error) {
	var initrdTemplateFilePath string
	if t.SystemConfig.Initramfs.Template == "" {
		return "", fmt.Errorf("initramfs template not specified in system configuration")
	}
	if filepath.IsAbs(t.SystemConfig.Initramfs.Template) {
		initrdTemplateFilePath = t.SystemConfig.Initramfs.Template
		if _, err := os.Stat(initrdTemplateFilePath); err != nil {
			return "", fmt.Errorf("initrd template file does not exist or is not accessible: %s", initrdTemplateFilePath)
		}
	} else {
		if len(t.PathList) == 0 {
			return "", fmt.Errorf("cannot resolve relative initramfs template path without template file context")
		}
		var found bool
		for _, path := range t.PathList {
			templateDir := filepath.Dir(path)
			candidatePath := filepath.Join(templateDir, t.SystemConfig.Initramfs.Template)
			if _, err := os.Stat(candidatePath); err == nil {
				initrdTemplateFilePath = candidatePath
				found = true
				break
			}
		}
		if !found {
			return "", fmt.Errorf("initrd template file does not exist: %s", t.SystemConfig.Initramfs.Template)
		}
	}
	return initrdTemplateFilePath, nil
}

func (t *ImageTemplate) GetBootloaderConfig() Bootloader {
	return t.SystemConfig.Bootloader
}

// GetPackages returns all packages from the system configuration
func (t *ImageTemplate) GetPackages() []string {
	var allPkgList []string
	allPkgList = append(allPkgList, t.EssentialPkgList...)
	allPkgList = append(allPkgList, t.KernelPkgList...)
	allPkgList = append(allPkgList, t.SystemConfig.Packages...)
	allPkgList = append(allPkgList, t.BootloaderPkgList...)
	return allPkgList
}

var packageSourcePriority = map[PackageSource]int{
	PackageSourceUnknown:    0,
	PackageSourceSystem:     10,
	PackageSourceKernel:     20,
	PackageSourceBootloader: 20,
	PackageSourceEssential:  30,
}

// GetPackageSourceMap returns a map of package name to the template section that requested it.
func (t *ImageTemplate) GetPackageSourceMap() map[string]PackageSource {
	sources := make(map[string]PackageSource)
	setSources := func(pkgs []string, source PackageSource) {
		for _, pkg := range pkgs {
			pkg = strings.TrimSpace(pkg)
			if pkg == "" {
				continue
			}
			if current, ok := sources[pkg]; !ok || packageSourcePriority[source] >= packageSourcePriority[current] {
				sources[pkg] = source
			}
		}
	}

	setSources(t.SystemConfig.Packages, PackageSourceSystem)
	setSources(t.KernelPkgList, PackageSourceKernel)
	setSources(t.BootloaderPkgList, PackageSourceBootloader)
	setSources(t.EssentialPkgList, PackageSourceEssential)

	return sources
}

func (t *ImageTemplate) GetAdditionalFileInfo() []AdditionalFileInfo {
	var PathUpdatedList []AdditionalFileInfo
	if len(t.SystemConfig.AdditionalFiles) == 0 {
		return []AdditionalFileInfo{}
	}

	for i := range t.SystemConfig.AdditionalFiles {
		if t.SystemConfig.AdditionalFiles[i].Local == "" || t.SystemConfig.AdditionalFiles[i].Final == "" {
			log.Warnf("Ignoring additional file entry with empty local or final path: %+v",
				t.SystemConfig.AdditionalFiles[i])
		} else {
			if filepath.IsAbs(t.SystemConfig.AdditionalFiles[i].Local) {
				if _, err := os.Stat(t.SystemConfig.AdditionalFiles[i].Local); err == nil {
					PathUpdatedList = append(PathUpdatedList, t.SystemConfig.AdditionalFiles[i])
				} else {
					log.Warnf("Ignoring additional file entry with non-existent local path: %+v",
						t.SystemConfig.AdditionalFiles[i])
				}
			} else {
				if len(t.PathList) == 0 {
					log.Warnf("Cannot resolve relative additional file path without template file context: %+v",
						t.SystemConfig.AdditionalFiles[i])
				} else {
					var found bool
					for _, path := range t.PathList {
						templateDir := filepath.Dir(path)
						candidatePath := filepath.Join(templateDir, t.SystemConfig.AdditionalFiles[i].Local)
						if _, err := os.Stat(candidatePath); err == nil {
							newFileInfo := AdditionalFileInfo{
								Local: candidatePath,
								Final: t.SystemConfig.AdditionalFiles[i].Final,
							}
							PathUpdatedList = append(PathUpdatedList, newFileInfo)
							found = true
							break
						}
					}
					if !found {
						log.Warnf("Ignoring additional file entry with non-existent local path: %+v",
							t.SystemConfig.AdditionalFiles[i])
					}
				}
			}
		}
	}
	return PathUpdatedList
}

func (t *ImageTemplate) GetConfigurationInfo() []ConfigurationInfo {
	return t.SystemConfig.Configurations
}

// GetKernel returns the kernel configuration from the system configuration
func (t *ImageTemplate) GetKernel() KernelConfig {
	return t.SystemConfig.Kernel
}

func (t *ImageTemplate) GetKernelPackages() []string {
	return t.SystemConfig.Kernel.Packages
}

// GetSystemConfigName returns the name of the system configuration
func (t *ImageTemplate) GetSystemConfigName() string {
	return t.SystemConfig.Name
}

func (t *ImageTemplate) SaveUpdatedConfigFile(path string) error {
	if path == "" {
		return fmt.Errorf("output path is empty")
	}

	// Ensure destination directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		log.Errorf("Failed to create directory for config file %s: %v", dir, err)
		return fmt.Errorf("failed to create directory for config file: %w", err)
	}

	// Marshal to YAML (includes the block-scalar-header repair).
	data, err := MarshalTemplateYAML(t)
	if err != nil {
		log.Errorf("Failed to marshal image template: %v", err)
		return err
	}

	// Write file safely with symlink protection
	if err := security.SafeWriteFile(path, data, 0644, security.RejectSymlinks); err != nil {
		log.Errorf("Failed to write image template to %s: %v", path, err)
		return fmt.Errorf("failed to write image template: %w", err)
	}

	log.Infof("Saved image template to %s", path)
	return nil
}

// MarshalTemplateYAML marshals an ImageTemplate to YAML bytes, applying the same
// block-scalar-header repair used by SaveUpdatedConfigFile so callers can safely
// emit the result to stdout or another sink.
func MarshalTemplateYAML(t *ImageTemplate) ([]byte, error) {
	if t == nil {
		return nil, fmt.Errorf("template is nil")
	}

	data, err := yaml.Marshal(t)
	if err != nil {
		return nil, fmt.Errorf("error marshaling template to YAML: %w", err)
	}

	if err := validateYAMLBytes(data); err != nil {
		data = fixInvalidBlockScalarHeader(data)
		if verr := validateYAMLBytes(data); verr != nil {
			return nil, fmt.Errorf("generated YAML is invalid after block scalar header fix: %w", verr)
		}
	}

	return data, nil
}

func validateYAMLBytes(data []byte) error {
	var parsed any

	return yaml.Unmarshal(data, &parsed)
}

func fixInvalidBlockScalarHeader(data []byte) []byte {
	// Work around yaml.v3 emitting invalid explicit indent headers such as "|4-".
	// Keep the payload and chomping mode untouched, and drop only the indent indicator.
	return invalidBlockScalarHeaderPattern.ReplaceAll(data, []byte("$1$2\n"))
}

// GetImmutability returns the immutability configuration from systemConfig
func (t *ImageTemplate) GetImmutability() ImmutabilityConfig {
	return t.SystemConfig.Immutability
}

// IsImmutabilityEnabled returns whether immutability is enabled
func (t *ImageTemplate) IsImmutabilityEnabled() bool {
	return t.SystemConfig.Immutability.Enabled
}

// IsFDEEnabled returns whether full-disk encryption is enabled
func (t *ImageTemplate) IsFDEEnabled() bool {
	return t.SystemConfig.FDE.Enabled
}

// GetFDEPassphrase returns the full-disk-encryption passphrase resolved in
// memory from systemConfig.fde.passphraseFile.
func (t *ImageTemplate) GetFDEPassphrase() string {
	return t.SystemConfig.FDE.Passphrase
}

// GetFDEPassphraseFile returns the local file path used to source the
// full-disk-encryption passphrase, when configured.
func (t *ImageTemplate) GetFDEPassphraseFile() string {
	return t.SystemConfig.FDE.PassphraseFile
}

func resolveFDEPassphrase(template *ImageTemplate, templatePath string) error {
	if !template.IsFDEEnabled() {
		return nil
	}

	passphraseFile := strings.TrimSpace(template.SystemConfig.FDE.PassphraseFile)
	if passphraseFile == "" {
		return nil
	}

	if !filepath.IsAbs(passphraseFile) {
		passphraseFile = filepath.Join(filepath.Dir(templatePath), passphraseFile)
	}

	passphraseData, err := security.SafeReadFile(passphraseFile, security.RejectSymlinks)
	if err != nil {
		return fmt.Errorf("failed to read systemConfig.fde.passphraseFile %q: %w", passphraseFile, err)
	}

	passphrase := strings.TrimSpace(string(passphraseData))
	if passphrase == "" {
		return fmt.Errorf("systemConfig.fde.passphraseFile %q is empty", passphraseFile)
	}

	template.SystemConfig.FDE.PassphraseFile = passphraseFile
	template.SystemConfig.FDE.Passphrase = passphrase

	return nil
}

// GetFDEPartitions returns the list of partition IDs to encrypt.
func (t *ImageTemplate) GetFDEPartitions() []string {
	return t.SystemConfig.FDE.Partitions
}

// GetFDEUnlockMode returns the boot unlock mode for full-disk encryption. It
// defaults to "auto" (keyfile-based, no prompt); only an explicit "manual"
// selects interactive passphrase unlock.
func (t *ImageTemplate) GetFDEUnlockMode() string {
	if t.SystemConfig.FDE.Unlock == "manual" {
		return "manual"
	}
	return "auto"
}

// IsFDEAutoUnlock reports whether FDE is enabled and set to automatic
// (keyfile-based) unlock at boot.
func (t *ImageTemplate) IsFDEAutoUnlock() bool {
	return t.IsFDEEnabled() && t.GetFDEUnlockMode() == "auto"
}

// IsFDEPartition reports whether the given partition ID is marked for encryption.
func (t *ImageTemplate) IsFDEPartition(id string) bool {
	if !t.IsFDEEnabled() {
		return false
	}
	for _, p := range t.SystemConfig.FDE.Partitions {
		if p == id {
			return true
		}
	}
	return false
}

// GetSecureBootDBKeyPath returns the secure boot DB key path from the immutability config
func (t *ImageTemplate) GetSecureBootDBKeyPath() string {
	return t.SystemConfig.Immutability.GetSecureBootDBKeyPath()
}

// GetSecureBootDBCrtPath returns the secure boot DB certificate path (PEM) from the immutability config
func (t *ImageTemplate) GetSecureBootDBCrtPath() string {
	return t.SystemConfig.Immutability.GetSecureBootDBCrtPath()
}

// GetSecureBootDBCerPath returns the secure boot DB certificate path (DER) from the immutability config
func (t *ImageTemplate) GetSecureBootDBCerPath() string {
	return t.SystemConfig.Immutability.GetSecureBootDBCerPath()
}

// HasSecureBootDBConfig returns whether secure boot DB configuration is available
func (t *ImageTemplate) HasSecureBootDBConfig() bool {
	return t.SystemConfig.Immutability.HasSecureBootDBConfig()
}

// GetImmutability returns the immutability configuration (SystemConfig method)
func (sc *SystemConfig) GetImmutability() ImmutabilityConfig {
	return sc.Immutability
}

// IsImmutabilityEnabled returns whether immutability is enabled (SystemConfig method)
func (sc *SystemConfig) IsImmutabilityEnabled() bool {
	return sc.Immutability.Enabled
}

// GetSecureBootDBKeyPath returns the secure boot DB key path from the immutability config
func (sc *SystemConfig) GetSecureBootDBKeyPath() string {
	return sc.Immutability.GetSecureBootDBKeyPath()
}

// GetSecureBootDBCrtPath returns the secure boot DB certificate path (PEM) from the immutability config
func (sc *SystemConfig) GetSecureBootDBCrtPath() string {
	return sc.Immutability.GetSecureBootDBCrtPath()
}

// GetSecureBootDBCerPath returns the secure boot DB certificate path (DER) from the immutability config
func (sc *SystemConfig) GetSecureBootDBCerPath() string {
	return sc.Immutability.GetSecureBootDBCerPath()
}

// HasSecureBootDBConfig returns whether secure boot DB configuration is available
func (sc *SystemConfig) HasSecureBootDBConfig() bool {
	return sc.Immutability.HasSecureBootDBConfig()
}

// HasSecureBootDBConfig returns whether any secure boot DB configuration is provided
func (ic *ImmutabilityConfig) HasSecureBootDBConfig() bool {
	return ic.SecureBootDBKey != "" || ic.SecureBootDBCrt != "" || ic.SecureBootDBCer != ""
}

// GetSecureBootDBKeyPath returns the secure boot DB private key file path
func (ic *ImmutabilityConfig) GetSecureBootDBKeyPath() string {
	return ic.SecureBootDBKey
}

// GetSecureBootDBCrtPath returns the secure boot DB certificate file path (PEM format)
func (ic *ImmutabilityConfig) GetSecureBootDBCrtPath() string {
	return ic.SecureBootDBCrt
}

// GetSecureBootDBCerPath returns the secure boot DB certificate file path (DER format)
func (ic *ImmutabilityConfig) GetSecureBootDBCerPath() string {
	return ic.SecureBootDBCer
}

// HasSecureBootDBKey returns whether a secure boot DB private key is configured
func (ic *ImmutabilityConfig) HasSecureBootDBKey() bool {
	return ic.SecureBootDBKey != ""
}

// HasSecureBootDBCrt returns whether a secure boot DB certificate (PEM) is configured
func (ic *ImmutabilityConfig) HasSecureBootDBCrt() bool {
	return ic.SecureBootDBCrt != ""
}

// HasSecureBootDBCer returns whether a secure boot DB certificate (DER) is configured
func (ic *ImmutabilityConfig) HasSecureBootDBCer() bool {
	return ic.SecureBootDBCer != ""
}

// GetUsers returns the user configurations from systemConfig
func (t *ImageTemplate) GetUsers() []UserConfig {
	return t.SystemConfig.Users
}

// GetUserByName returns a user configuration by name, or nil if not found
func (t *ImageTemplate) GetUserByName(name string) *UserConfig {
	for i := range t.SystemConfig.Users {
		if t.SystemConfig.Users[i].Name == name {
			return &t.SystemConfig.Users[i]
		}
	}
	return nil
}

// HasUsers returns whether any users are configured
func (t *ImageTemplate) HasUsers() bool {
	return len(t.SystemConfig.Users) > 0
}

// GetUsers returns the user configurations (SystemConfig method)
func (sc *SystemConfig) GetUsers() []UserConfig {
	return sc.Users
}

// GetUserByName returns a user configuration by name (SystemConfig method)
func (sc *SystemConfig) GetUserByName(name string) *UserConfig {
	for i := range sc.Users {
		if sc.Users[i].Name == name {
			return &sc.Users[i]
		}
	}
	return nil
}

// HasUsers returns whether any users are configured (SystemConfig method)
func (sc *SystemConfig) HasUsers() bool {
	return len(sc.Users) > 0
}

// GetPackageRepositories returns the list of additional package repositories
func (t *ImageTemplate) GetPackageRepositories() []PackageRepository {
	return t.PackageRepositories
}

// LoadProviderRepoConfig loads provider repository configuration from YAML file
// Returns a slice of ProviderRepoConfig to support multiple repositories
func LoadProviderRepoConfig(targetOS, targetDist string, arch string) ([]ProviderRepoConfig, error) {
	// Get the target OS config directory
	targetOsConfigDir, err := GetTargetOsConfigDir(targetOS, targetDist)
	if err != nil {
		return nil, fmt.Errorf("failed to get target OS config directory: %w", err)
	}

	// Construct path to repo.yml
	repoConfigPath := filepath.Join(targetOsConfigDir, "providerconfigs", arch+"_repo.yml")

	// Read the YAML file
	yamlData, err := security.SafeReadFile(repoConfigPath, security.RejectSymlinks)
	if err != nil {
		log.Errorf("Failed to read repo config file: %v", err)
		return nil, fmt.Errorf("failed to read repo config file %s: %w", repoConfigPath, err)
	}

	// Try to parse as new multiple repository format first
	var repoConfigs ProviderRepoConfigs
	if err := yaml.Unmarshal(yamlData, &repoConfigs); err == nil && len(repoConfigs.Repositories) > 0 {
		log.Infof("Loaded provider repo config from %s: %d repositories", repoConfigPath, len(repoConfigs.Repositories))
		return repoConfigs.Repositories, nil
	}

	// Fall back to old single repository format for backward compatibility
	var singleRepoConfig ProviderRepoConfig
	if err := yaml.Unmarshal(yamlData, &singleRepoConfig); err != nil {
		log.Errorf("Failed to parse repo config YAML: %v", err)
		return nil, fmt.Errorf("failed to parse repo config YAML: %w", err)
	}

	log.Infof("Loaded provider repo config from %s: %s (single repository format)", repoConfigPath, singleRepoConfig.Name)
	return []ProviderRepoConfig{singleRepoConfig}, nil
}

// ToRepoConfigData returns the unified repo configuration data for both DEB and RPM repositories
func (prc *ProviderRepoConfig) ToRepoConfigData(arch string) (repoType, name, url, gpgKey, component, buildPath string,
	pkgPrefix, releaseFile, releaseSign, baseURL string, gpgCheck, repoGPGCheck, enabled bool) {

	repoType = prc.Type
	name = prc.Name
	component = prc.Component
	// Replace "./builds" with temp_dir/builds
	if strings.HasPrefix(prc.BuildPath, "./builds") {
		buildPath = filepath.Join(TempDir(), strings.TrimPrefix(prc.BuildPath, "./"))
	} else {
		buildPath = prc.BuildPath
	}
	gpgCheck = prc.GPGCheck
	repoGPGCheck = prc.RepoGPGCheck
	enabled = prc.Enabled
	baseURL = prc.BaseURL

	switch strings.ToLower(prc.Type) {
	case "rpm":
		// RPM repository configuration (Azure Linux, EMT)
		// Check if baseURL contains {arch} placeholder for substitution
		if strings.Contains(prc.BaseURL, "{arch}") {
			url = strings.ReplaceAll(prc.BaseURL, "{arch}", arch)
		} else {
			// For repositories without {arch} placeholder, use baseURL as-is (like EMT)
			url = prc.BaseURL
		}

		gpgKeyValues := make([]string, 0, len(prc.GPGKeys)+1)
		if len(prc.GPGKeys) > 0 {
			gpgKeyValues = append(gpgKeyValues, prc.GPGKeys...)
		}
		if prc.GPGKey != "" {
			gpgKeyValues = append(gpgKeyValues, prc.GPGKey)
		}

		resolvedKeys := make([]string, 0, len(gpgKeyValues))
		for _, keyURL := range gpgKeyValues {
			keyURL = strings.TrimSpace(keyURL)
			if keyURL == "" {
				continue
			}
			if !strings.HasPrefix(keyURL, "http") {
				keyURL = fmt.Sprintf("%s/%s", url, keyURL)
			}
			resolvedKeys = append(resolvedKeys, keyURL)
		}
		gpgKey = strings.Join(resolvedKeys, ",")

		// DEB-specific fields are empty for RPM
		pkgPrefix = ""
		releaseFile = ""
		releaseSign = ""

	case "deb":
		// DEB repository configuration (eLxr)
		url = fmt.Sprintf("%s/binary-%s/Packages.gz", prc.BaseURL, arch)
		gpgKey = prc.PbGPGKey // Use pbGPGKey for DEB repositories
		pkgPrefix = prc.PkgPrefix
		releaseFile = prc.ReleaseFile
		releaseSign = prc.ReleaseSign

	default:
		// Unknown repository type - log warning and default to RPM behavior
		log.Warnf("Unknown repository type '%s', defaulting to RPM behavior", prc.Type)
		url = fmt.Sprintf("%s/%s", prc.BaseURL, arch)
		gpgKey = prc.GPGKey
		pkgPrefix = ""
		releaseFile = ""
		releaseSign = ""
	}

	return
}

// HasPackageRepositories returns true if additional repositories are configured
func (t *ImageTemplate) HasPackageRepositories() bool {
	return len(t.PackageRepositories) > 0
}

// GetRepositoryByCodename returns a repository by its codename, or nil if not found
func (t *ImageTemplate) GetRepositoryByCodename(codename string) *PackageRepository {
	for _, repo := range t.PackageRepositories {
		if repo.Codename == codename {
			return &repo
		}
	}
	return nil
}

// UnmarshalYAML implements yaml.Unmarshaler to track if immutability section was provided
func (i *ImmutabilityConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Use a type alias to avoid infinite recursion
	type alias ImmutabilityConfig
	temp := (*alias)(i)

	if err := unmarshal(temp); err != nil {
		return err
	}

	i.wasProvided = true // Mark that this section was explicitly provided in YAML
	return nil
}

// WasProvided returns true if the immutability section was explicitly defined in YAML
func (i *ImmutabilityConfig) WasProvided() bool {
	return i.wasProvided
}

func (t *ImageTemplate) validatePackageRepositories() error {
	for _, repo := range t.PackageRepositories {
		if err := repo.ValidatePackageRepository(); err != nil {
			return err
		}
	}

	return nil
}

// ValidatePackageRepository validates that either URL or Path is provided
func (pr *PackageRepository) ValidatePackageRepository() error {
	if len(pr.Packages) > 0 {
		// path is optional when packages is set — a temp dir is auto-created at runtime
		for _, entry := range pr.Packages {
			if strings.TrimSpace(entry) == "" {
				return fmt.Errorf("repository '%s': 'packages' entries cannot be empty", pr.Codename)
			}
			// If the entry looks like a URL it must use https; plain paths are copied at runtime
			if strings.Contains(entry, "://") {
				parsedURL, err := url.Parse(entry)
				if err != nil {
					return fmt.Errorf("repository '%s': invalid packages URL '%s': %w", pr.Codename, entry, err)
				}
				if parsedURL.Scheme != "https" {
					return fmt.Errorf("repository '%s': packages URL '%s' must use https", pr.Codename, entry)
				}
			}
		}
	}

	if pr.URL == "" && pr.Path == "" && len(pr.Packages) == 0 {
		return fmt.Errorf("repository '%s': either 'url', 'path', or 'packages' must be provided", pr.Codename)
	}
	if pr.URL != "" && pr.Path != "" {
		return fmt.Errorf("repository '%s': cannot specify both 'url' and 'path', choose one", pr.Codename)
	}
	return nil
}

// validateBaseline enforces cross-field rules that the JSON schema cannot express:
// mode/source coupling, format restrictions, and overlay policy invariants.
// overlayPolicy is a top-level peer to baseline (per the image-extension ADR),
// so it is only permitted when baseline.mode is "overlay".
func (t *ImageTemplate) validateBaseline() error {
	mode := BaselineModeCreate
	if t.Baseline != nil && t.Baseline.Mode != "" {
		mode = t.Baseline.Mode
	}

	switch mode {
	case BaselineModeCreate:
		if t.Baseline != nil && t.Baseline.Source != nil {
			return fmt.Errorf("baseline: source must not be set when mode is %q", BaselineModeCreate)
		}
		if t.OverlayPolicy != nil {
			return fmt.Errorf("overlayPolicy must not be set when baseline.mode is %q", BaselineModeCreate)
		}
	case BaselineModeOverlay:
		if t.Baseline.Source == nil {
			return fmt.Errorf("baseline: source is required when mode is %q", BaselineModeOverlay)
		}
		if err := t.Baseline.Source.Validate(); err != nil {
			return err
		}
		format := t.Baseline.Source.Format
		if format == "" {
			format = BaselineFormatRaw
		}
		switch format {
		case BaselineFormatRaw, BaselineFormatQcow2, BaselineFormatVHD, BaselineFormatVHDX:
			// supported; non-raw formats are converted to RAW before loop-attach.
		default:
			return fmt.Errorf("baseline.source.format must be one of %q, %q, %q, %q (got %q)",
				BaselineFormatRaw, BaselineFormatQcow2, BaselineFormatVHD, BaselineFormatVHDX, format)
		}
		if t.OverlayPolicy != nil {
			if err := t.OverlayPolicy.validate(); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("baseline.mode must be %q or %q (got %q)",
			BaselineModeCreate, BaselineModeOverlay, t.Baseline.Mode)
	}
	return nil
}

// Validate enforces that exactly one of Path or URL is set, and that a URL uses
// the https scheme. Plain http is rejected so the baseline is always fetched
// over TLS, matching the https-only policy used for other remote downloads in
// this tool (e.g. RPM packages). Integrity verification of the downloaded image
// is intentionally deferred. Local paths are taken from the host build system
// as-is; https URLs are downloaded before the overlay runs.
func (s *BaselineSource) Validate() error {
	// Normalize by trimming surrounding whitespace and persist it back onto the
	// struct so callers (overlay ingestion) use the same value that was
	// validated. Otherwise a padded path/URL could pass validation here but then
	// fail with a confusing error during copy/download.
	path := strings.TrimSpace(s.Path)
	rawURL := strings.TrimSpace(s.URL)
	s.Path = path
	s.URL = rawURL
	// Normalize the format to lower-case so downstream overlay ingestion compares
	// the declared format against qemu-img's (lower-cased) detected format on equal
	// footing. YAML-loaded templates are already constrained to the lower-case
	// schema enum (raw/qcow2/vhd/vhdx) before this runs, so this primarily
	// normalizes programmatically-built templates, which reach ingestion via
	// Validate() without passing through schema validation.
	s.Format = strings.ToLower(strings.TrimSpace(s.Format))

	switch {
	case path == "" && rawURL == "":
		return fmt.Errorf("baseline.source must set either %q or %q", "path", "url")
	case path != "" && rawURL != "":
		return fmt.Errorf("baseline.source must set only one of %q or %q", "path", "url")
	case path != "":
		// Reject any URI scheme (e.g. http:/, file:/path, file://...). Local
		// paths have no scheme; remote images belong in baseline.source.url.
		if parsed, err := url.Parse(path); err == nil && parsed.Scheme != "" {
			return fmt.Errorf("baseline.source.path must be a local file path; use baseline.source.url for remote images")
		}
	case rawURL != "":
		parsed, err := url.Parse(rawURL)
		if err != nil {
			return fmt.Errorf("baseline.source.url is not a valid URL: %w", err)
		}
		// Require https so the baseline is always fetched over TLS. Plain http
		// offers no integrity or authenticity guarantee for a whole-disk image
		// and is rejected here, consistent with other remote downloads.
		if parsed.Scheme != "https" {
			return fmt.Errorf("baseline.source.url must use https (got %q)", parsed.Scheme)
		}
		// Reject a scheme-only URL like "https://" (no host): it passes the
		// scheme check but would fail later with an opaque download error.
		// Catching it here gives an immediate, clear message.
		if parsed.Host == "" {
			return fmt.Errorf("baseline.source.url must include a host (got %q)", rawURL)
		}
	}
	return nil
}

func (p *OverlayPolicy) validate() error {
	op := p.PackageOperation
	if op == "" {
		op = OverlayPackageOpAdditiveOnly
	}
	switch op {
	case OverlayPackageOpAdditiveOnly:
		// Additive-only: upgrades of baseline packages stay blocked.
		p.AllowUpgrade = false
	case OverlayPackageOpAdditiveAndUpgrade:
		// Opt in to upgrading already-installed baseline packages. Downgrades and
		// removals are still gated off (AllowDowngrade/AllowRemoval stay false).
		p.AllowUpgrade = true
	default:
		return fmt.Errorf("baseline.overlayPolicy.packageOperation must be %q or %q (got %q)",
			OverlayPackageOpAdditiveOnly, OverlayPackageOpAdditiveAndUpgrade, p.PackageOperation)
	}
	cp := p.ConflictPolicy
	if cp == "" {
		cp = OverlayConflictPolicyFail
	}
	if cp != OverlayConflictPolicyFail && cp != OverlayConflictPolicyAllowExplicit {
		return fmt.Errorf("baseline.overlayPolicy.conflictPolicy must be %q or %q (got %q)",
			OverlayConflictPolicyFail, OverlayConflictPolicyAllowExplicit, p.ConflictPolicy)
	}
	// kernelCmdline and grubDefault are written verbatim into
	// GRUB_CMDLINE_LINUX="..." / GRUB_DEFAULT="..." assignments in /etc/default/grub,
	// which update-grub/grub-mkconfig then `.`-source (as root) during regeneration.
	// Inside that double-quoted, shell-sourced assignment a double quote prematurely
	// closes it, a newline splits it, a '$' or backtick is expanded /
	// command-substituted (the latter an injection surface running as root at regen
	// time), and a trailing backslash escapes the closing quote (leaving the string
	// unterminated). None are needed by a kernel command line or a GRUB_DEFAULT entry,
	// so all are rejected up front rather than producing a broken or dangerous defaults
	// file.
	const grubValueForbidden = "\"$`\\\n"
	if strings.ContainsAny(p.KernelCmdline, grubValueForbidden) {
		return fmt.Errorf("baseline.overlayPolicy.kernelCmdline must not contain a double quote, dollar sign, backtick, backslash, or newline")
	}
	if strings.ContainsAny(p.GrubDefault, grubValueForbidden) {
		return fmt.Errorf("baseline.overlayPolicy.grubDefault must not contain a double quote, dollar sign, backtick, backslash, or newline")
	}
	return nil
}

// IsOverlayMode reports whether the template requests overlay-mode baseline.
func (t *ImageTemplate) IsOverlayMode() bool {
	return t.Baseline != nil && t.Baseline.Mode == BaselineModeOverlay
}
