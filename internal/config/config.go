package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/open-edge-platform/os-image-composer/internal/config/validate"
	"github.com/open-edge-platform/os-image-composer/internal/ospackage"
	"github.com/open-edge-platform/os-image-composer/internal/utils/logger"
	"github.com/open-edge-platform/os-image-composer/internal/utils/security"
	"github.com/open-edge-platform/os-image-composer/internal/utils/slice"
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

type DiskConfig struct {
	Name               string          `yaml:"name"`
	Path               string          `yaml:"path"` // Path to the disk device (e.g., /dev/sda), used by live installer
	Artifacts          []ArtifactInfo  `yaml:"artifacts"`
	Size               string          `yaml:"size"`
	PartitionTableType string          `yaml:"partitionTableType"`
	Partitions         []PartitionInfo `yaml:"partitions"`
}

type PackageRepository struct {
	ID            string   `yaml:"id,omitempty"`            // Auto-assigned
	Codename      string   `yaml:"codename"`                // Repository identifier/codename
	URL           string   `yaml:"url"`                     // Repository base URL
	PKey          string   `yaml:"pkey"`                    // Public GPG key URL for verification
	Component     string   `yaml:"component,omitempty"`     // Repository component (e.g., "main", "restricted")
	Priority      int      `yaml:"priority,omitempty"`      // Repository priority (higher numbers = higher priority)
	AllowPackages []string `yaml:"allowPackages,omitempty"` // Optional: specific packages to include from this repo (pinning)
}

// ProviderRepoConfig represents the repository configuration for a provider
type ProviderRepoConfig struct {
	Name         string `yaml:"name"`
	Type         string `yaml:"type"` // Repository type: "rpm" or "deb"
	BaseURL      string `yaml:"baseURL"`
	PkgPrefix    string `yaml:"pkgPrefix"`
	ReleaseFile  string `yaml:"releaseFile"`
	ReleaseSign  string `yaml:"releaseSign"`
	PbGPGKey     string `yaml:"pbGPGKey"` // For DEB repositories (eLxr)
	GPGKey       string `yaml:"gpgKey"`   // For RPM repositories (azl, emt)
	GPGCheck     bool   `yaml:"gpgCheck"`
	RepoGPGCheck bool   `yaml:"repoGPGCheck"`
	Enabled      bool   `yaml:"enabled"`
	Component    string `yaml:"component"` // Repository component/section identifier
	BuildPath    string `yaml:"buildPath"`
}

// ProviderRepoConfigs represents multiple repository configurations for a provider
type ProviderRepoConfigs struct {
	Repositories []ProviderRepoConfig `yaml:"repositories"`
}

// ImageTemplate represents the YAML image template structure (unchanged)
type ImageTemplate struct {
	Image               ImageInfo           `yaml:"image"`
	Target              TargetInfo          `yaml:"target"`
	Disk                DiskConfig          `yaml:"disk,omitempty"`
	SystemConfig        SystemConfig        `yaml:"systemConfig"`
	PackageRepositories []PackageRepository `yaml:"packageRepositories,omitempty"`

	// Explicitly excluded from YAML serialization/deserialization
	PathList          []string                `yaml:"-"`
	BootloaderPkgList []string                `yaml:"-"`
	EssentialPkgList  []string                `yaml:"-"`
	KernelPkgList     []string                `yaml:"-"`
	FullPkgList       []string                `yaml:"-"`
	FullPkgListBom    []ospackage.PackageInfo `yaml:"-"`
	DotFilePath       string                  `yaml:"-"`
	DotSystemOnly     bool                    `yaml:"-"`
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

// SystemConfig represents a system configuration within the template
type SystemConfig struct {
	Name            string               `yaml:"name"`
	Description     string               `yaml:"description"`
	Initramfs       Initramfs            `yaml:"initramfs,omitempty"`
	HostName        string               `yaml:"hostname,omitempty"`
	Immutability    ImmutabilityConfig   `yaml:"immutability,omitempty"`
	Users           []UserConfig         `yaml:"users,omitempty"`
	Bootloader      Bootloader           `yaml:"bootloader"`
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
	Name         string   `yaml:"name"`         // Name: label for the partition
	ID           string   `yaml:"id"`           // ID: unique identifier for the partition; can be used as a key
	Flags        []string `yaml:"flags"`        // Flags: optional flags for the partition (e.g., "boot", "hidden")
	Type         string   `yaml:"type"`         // Type: partition type (e.g., "esp", "linux-root-amd64")
	TypeGUID     string   `yaml:"typeUUID"`     // TypeGUID: GPT type GUID for the partition (e.g., "8300" for Linux filesystem)
	FsType       string   `yaml:"fsType"`       // FsType: filesystem type (e.g., "ext4", "xfs", etc.);
	FsLabel      string   `yaml:"fsLabel"`      // FsLabel: filesystem label (e.g., "cloudimg-rootfs")
	Start        string   `yaml:"start"`        // Start: start offset of the partition; can be a absolute size (e.g., "512MiB")
	End          string   `yaml:"end"`          // End: end offset of the partition; can be a absolute size (e.g., "2GiB") or "0" for the end of the disk
	MountPoint   string   `yaml:"mountPoint"`   // MountPoint: optional mount point for the partition (e.g., "/boot", "/rootfs")
	MountOptions string   `yaml:"mountOptions"` // MountOptions: optional mount options for the partition (e.g., "defaults", "noatime")
}

var log = logger.Logger()

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

	// Store the template path info
	if !slice.Contains(template.PathList, path) {
		template.PathList = append(template.PathList, path)
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

	return &template, nil
}

// GetProviderName returns the provider name for the given template
func (t *ImageTemplate) GetProviderName() string {
	// Map OS/dist combinations to provider names
	providerMap := map[string]map[string]string{
		"azure-linux": {"azl3": "AzureLinux3"},
		"emt":         {"emt3": "EMT3.0"},
		"elxr":        {"elxr12": "eLxr12"},
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
		"azl3":   "3",
		"emt3":   "3.0",
		"elxr12": "12",
	}
	return versionMap[t.Target.Dist]
}

func (t *ImageTemplate) GetImageName() string {
	return t.Image.Name
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

	// Marshal the template to YAML
	data, err := yaml.Marshal(t)
	if err != nil {
		log.Errorf("Error marshaling image template to YAML: %v", err)
		return fmt.Errorf("error marshaling template to YAML: %w", err)
	}

	// Write file safely with symlink protection
	if err := security.SafeWriteFile(path, data, 0644, security.RejectSymlinks); err != nil {
		log.Errorf("Failed to write image template to %s: %v", path, err)
		return fmt.Errorf("failed to write image template: %w", err)
	}

	log.Infof("Saved image template to %s", path)
	return nil
}

// GetImmutability returns the immutability configuration from systemConfig
func (t *ImageTemplate) GetImmutability() ImmutabilityConfig {
	return t.SystemConfig.Immutability
}

// IsImmutabilityEnabled returns whether immutability is enabled
func (t *ImageTemplate) IsImmutabilityEnabled() bool {
	return t.SystemConfig.Immutability.Enabled
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

		// Handle GPG key URL construction
		gpgKey = prc.GPGKey
		if !strings.HasPrefix(gpgKey, "http") && gpgKey != "" {
			// For relative GPG key paths, use the constructed repository URL
			gpgKey = fmt.Sprintf("%s/%s", url, gpgKey)
		}
		// If gpgKey starts with http, use it as-is

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
