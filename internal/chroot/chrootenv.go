package chroot

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/chroot/chrootbuild"
	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/compression"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/file"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/mount"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/system"
)

const (
	ChrootRepoDir       = "/cdrom/cache-repo"
	RPMRepoConfigFile   = "local.repo"
	DEBRepoConfigFile   = "local.list"
	RPMRepoConfigDir    = "/etc/yum.repos.d/"
	DEBRepoConfigDir    = "/etc/apt/sources.list.d/"
	ResolvConfPath      = "/etc/resolv.conf"
	DefaultArchitecture = "amd64"
)

var log = logger.Logger()

var rpmVersionSuffixRe = regexp.MustCompile(`^(.+)-([0-9][A-Za-z0-9.+_:~\-]*)-([0-9][A-Za-z0-9.+_:~\-]*)$`)

var knownRPMArch = map[string]struct{}{
	"x86_64":  {},
	"aarch64": {},
	"noarch":  {},
	"i686":    {},
	"i586":    {},
	"armv7hl": {},
	"ppc64le": {},
	"s390x":   {},
}

type ChrootEnvInterface interface {
	GetChrootEnvRoot() string
	GetChrootImageBuildDir() string
	GetTargetOsPkgType() string
	GetTargetOsConfigDir() string
	GetTargetOsReleaseVersion() string
	GetChrootPkgCacheDir() string
	GetChrootEnvEssentialPackageList() ([]string, error)
	GetChrootEnvHostPath(chrootPath string) (string, error)
	GetChrootEnvPath(ChrootEnvHostPath string) (string, error)
	MountChrootSysfs(chrootPath string) error
	UmountChrootSysfs(chrootPath string) error
	MountChrootPath(hostFullPath, chrootPath, mountFlags string) error
	UmountChrootPath(chrootPath string) error
	CopyFileFromHostToChroot(hostFilePath, chrootPath string) error
	CopyFileFromChrootToHost(hostFilePath, chrootPath string) error
	UpdateChrootLocalRepoMetadata(chrootRepoDir string, targetArch string, sudo bool) error
	RefreshLocalCacheRepo() error
	InitChrootEnv(targetOs, targetDist, targetArch string) error
	CleanupChrootEnv(targetOs, targetDist, targetArch string) error
	TdnfInstallPackage(packageName, installRoot string, repositoryIDList []string) error
	AptInstallPackage(packageName, installRoot string, repoSrcList []string) error
	UpdateSystemPkgs(template *config.ImageTemplate) error
}

type ChrootEnv struct {
	ChrootEnvRoot       string
	ChrootImageBuildDir string
	ChrootBuilder       chrootbuild.ChrootBuilderInterface
	buildTemplate       *config.ImageTemplate
	TargetOs            string // Store targetOs for package manager selection
}

func NewChrootEnv(targetOs, targetDist, targetArch string) (*ChrootEnv, error) {
	globalWorkDir, err := config.WorkDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get global work directory: %v", err)
	}
	providerId := system.GetProviderId(targetOs, targetDist, targetArch)
	chrootEnvRoot := filepath.Join(globalWorkDir, providerId, "chrootenv")
	if _, err := os.Stat(chrootEnvRoot); os.IsNotExist(err) {
		if err = os.MkdirAll(chrootEnvRoot, 0700); err != nil {
			return nil, fmt.Errorf("failed to create chroot environment root directory: %w", err)
		}
	}

	chrootBuilder, err := chrootbuild.NewChrootBuilder(targetOs, targetDist, targetArch)
	if err != nil {
		return nil, fmt.Errorf("failed to create chroot builder: %w", err)
	}

	return &ChrootEnv{
		ChrootEnvRoot: chrootEnvRoot,
		ChrootBuilder: chrootBuilder,
		TargetOs:      targetOs,
	}, nil
}

func (chrootEnv *ChrootEnv) GetChrootEnvRoot() string {
	return chrootEnv.ChrootEnvRoot
}

func (chrootEnv *ChrootEnv) SetBuildTemplate(template *config.ImageTemplate) {
	chrootEnv.buildTemplate = template
	if templateAwareBuilder, ok := chrootEnv.ChrootBuilder.(interface{ SetBuildTemplate(*config.ImageTemplate) }); ok {
		templateAwareBuilder.SetBuildTemplate(template)
	}
}

func (chrootEnv *ChrootEnv) GetChrootImageBuildDir() string {
	return chrootEnv.ChrootImageBuildDir
}

func (chrootEnv *ChrootEnv) GetTargetOsPkgType() string {
	return chrootEnv.ChrootBuilder.GetTargetOsPkgType()
}

func (chrootEnv *ChrootEnv) GetTargetOsConfigDir() string {
	return chrootEnv.ChrootBuilder.GetTargetOsConfigDir()
}

func (chrootEnv *ChrootEnv) GetTargetOsReleaseVersion() string {
	targetOsConfig := chrootEnv.ChrootBuilder.GetTargetOsConfig()
	releaseVersion, ok := targetOsConfig["releaseVersion"]
	if !ok {
		log.Errorf("releaseVersion not found in target OS config")
		return "unknown"
	}
	if s, ok := releaseVersion.(string); ok {
		return s
	}
	log.Errorf("releaseVersion is not a string")
	return "unknown"
}

func (chrootEnv *ChrootEnv) GetChrootPkgCacheDir() string {
	return chrootEnv.ChrootBuilder.GetChrootPkgCacheDir()
}

func (chrootEnv *ChrootEnv) GetChrootEnvEssentialPackageList() ([]string, error) {
	return chrootEnv.ChrootBuilder.GetChrootEnvEssentialPackageList()
}

func (chrootEnv *ChrootEnv) GetChrootEnvHostPath(chrootPath string) (string, error) {
	if strings.Contains(chrootPath, "..") {
		return "", fmt.Errorf("path contains invalid characters: %s", chrootPath)
	}

	if chrootEnv.ChrootEnvRoot == "" {
		log.Errorf("Chroot env is not initialized")
		return "", fmt.Errorf("chroot env is not initialized")
	}
	return filepath.Join(chrootEnv.ChrootEnvRoot, chrootPath), nil
}

func (chrootEnv *ChrootEnv) GetChrootEnvPath(ChrootEnvHostPath string) (string, error) {
	if chrootEnv.ChrootEnvRoot == "" {
		log.Errorf("Chroot env is not initialized")
		return "", fmt.Errorf("chroot env is not initialized")
	}
	isSubPath, err := file.IsSubPath(chrootEnv.ChrootEnvRoot, ChrootEnvHostPath)
	if err != nil {
		log.Errorf("Failed to check if path %s is a subpath of chroot env root %s: %v", ChrootEnvHostPath, chrootEnv.ChrootEnvRoot, err)
		return "", fmt.Errorf("failed to check if path %s is a subpath of chroot env root %s: %w",
			ChrootEnvHostPath, chrootEnv.ChrootEnvRoot, err)
	}
	if !isSubPath {
		return "", fmt.Errorf("path %s is not a subpath of chroot env root %s", ChrootEnvHostPath, chrootEnv.ChrootEnvRoot)
	}

	chrootPath := ChrootEnvHostPath[len(chrootEnv.ChrootEnvRoot):]
	if strings.HasPrefix(chrootPath, "/") {
		return chrootPath, nil
	} else {
		return filepath.Join("/", chrootPath), nil
	}
}

func (chrootEnv *ChrootEnv) MountChrootSysfs(chrootPath string) error {
	chrootHostPath, err := chrootEnv.GetChrootEnvHostPath(chrootPath)
	if err != nil {
		return fmt.Errorf("failed to get chroot host path for %s: %w", chrootPath, err)
	}
	return mount.MountSysfs(chrootHostPath)
}

func (chrootEnv *ChrootEnv) UmountChrootSysfs(chrootPath string) error {
	chrootHostPath, err := chrootEnv.GetChrootEnvHostPath(chrootPath)
	if err != nil {
		return fmt.Errorf("failed to get chroot host path for %s: %w", chrootPath, err)
	}

	if err := system.StopGPGComponents(chrootHostPath); err != nil {
		return fmt.Errorf("failed to stop GPG components in chroot environment: %w", err)
	}

	if err = mount.UmountSysfs(chrootHostPath); err != nil {
		return fmt.Errorf("failed to unmount sysfs for %s: %w", chrootHostPath, err)
	}
	return nil
}

// MountChrootPath mounts a host path to a chroot path
func (chrootEnv *ChrootEnv) MountChrootPath(hostFullPath, chrootPath, mountFlags string) error {
	chrootHostPath, err := chrootEnv.GetChrootEnvHostPath(chrootPath)
	if err != nil {
		return fmt.Errorf("failed to get chroot host path for %s: %w", chrootPath, err)
	}
	if hostFullPath == chrootHostPath {
		return nil
	} else {
		if _, err := os.Stat(chrootHostPath); os.IsNotExist(err) {
			if _, err = shell.ExecCmd("mkdir -p "+chrootHostPath, true, shell.HostPath, nil); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", chrootHostPath, err)
			}
		}
	}
	return mount.MountPath(hostFullPath, chrootHostPath, mountFlags)
}

// UmountChrootPath unmounts a chroot path
func (chrootEnv *ChrootEnv) UmountChrootPath(chrootPath string) error {
	chrootHostPath, err := chrootEnv.GetChrootEnvHostPath(chrootPath)
	if err != nil {
		return fmt.Errorf("failed to get chroot host path for %s: %w", chrootPath, err)
	}

	return mount.UmountPath(chrootHostPath)
}

// CopyFileFromHostToChroot copies a file from the host to the chroot environment
func (chrootEnv *ChrootEnv) CopyFileFromHostToChroot(hostFilePath, chrootPath string) error {
	chrootHostPath, err := chrootEnv.GetChrootEnvHostPath(chrootPath)
	if err != nil {
		return fmt.Errorf("failed to get chroot host path for %s: %w", chrootPath, err)
	}
	if hostFilePath == chrootHostPath {
		return nil
	} else {
		return file.CopyFile(hostFilePath, chrootHostPath, "-f", true)
	}
}

// CopyFileFromChrootToHost copies a file from the chroot environment to the host
func (chrootEnv *ChrootEnv) CopyFileFromChrootToHost(hostFilePath, chrootPath string) error {
	chrootHostPath, err := chrootEnv.GetChrootEnvHostPath(chrootPath)
	if err != nil {
		return fmt.Errorf("failed to get chroot host path for %s: %w", chrootPath, err)
	}
	if hostFilePath == chrootHostPath {
		return nil
	} else {
		return file.CopyFile(chrootHostPath, hostFilePath, "-f", true)
	}
}

func (chrootEnv *ChrootEnv) updateChrootLocalRPMRepo(chrootRepoDir string) error {
	chrootHostPath, err := chrootEnv.GetChrootEnvHostPath(chrootRepoDir)
	if err != nil {
		return fmt.Errorf("failed to get chroot host path for %s: %w", chrootRepoDir, err)
	}
	if _, err := os.Stat(chrootHostPath); os.IsNotExist(err) {
		return fmt.Errorf("chroot repo directory not existing%s: %w", chrootHostPath, err)
	}
	cmd := fmt.Sprintf("createrepo_c --compatibility --update %s", chrootRepoDir)
	if _, err = shell.ExecCmd(cmd, false, chrootEnv.ChrootEnvRoot, nil); err != nil {
		return fmt.Errorf("failed to update chroot local cache repository: %w", err)
	}
	return nil
}

func (chrootEnv *ChrootEnv) updateChrootLocalDebRepo(chrootPkgCacheDir, targetArch string, sudo bool) error {
	return chrootEnv.ChrootBuilder.UpdateLocalDebRepo(chrootPkgCacheDir, targetArch, sudo)
}

func (chrootEnv *ChrootEnv) UpdateChrootLocalRepoMetadata(chrootRepoDir string, targetArch string, sudo bool) error {
	pkgType := chrootEnv.GetTargetOsPkgType()
	if pkgType == "rpm" {
		if err := chrootEnv.updateChrootLocalRPMRepo(chrootRepoDir); err != nil {
			return fmt.Errorf("failed to update rpm local cache repository %s: %w", chrootRepoDir, err)
		}
	} else if pkgType == "deb" {
		chrootPkgCacheDir, err := chrootEnv.GetChrootEnvHostPath(chrootRepoDir)
		if err != nil {
			return fmt.Errorf("failed to get chroot host path for %s: %w", chrootRepoDir, err)
		}
		if err := chrootEnv.updateChrootLocalDebRepo(chrootPkgCacheDir, targetArch, sudo); err != nil {
			return fmt.Errorf("failed to update debian local cache repository: %v", err)
		}
	} else {
		return fmt.Errorf("unsupported package type: %s", pkgType)
	}
	return nil
}

func (chrootEnv *ChrootEnv) RefreshLocalCacheRepo() error {
	// From local.repo
	pkgType := chrootEnv.GetTargetOsPkgType()
	if pkgType == "rpm" {
		pkgManager := chrootEnv.getPackageManagerCmd()
		releaseVersion := chrootEnv.GetTargetOsReleaseVersion()

		var cmd string
		if pkgManager == "dnf" {
			cmd = "dnf makecache"
		} else {
			cmd = fmt.Sprintf("tdnf makecache --releasever %s", releaseVersion)
		}

		if _, err := shell.ExecCmdWithStream(cmd, true, chrootEnv.ChrootEnvRoot, nil); err != nil {
			return fmt.Errorf("failed to refresh cache for chroot repository: %w", err)
		}
	} else if pkgType == "deb" {
		cmd := "apt clean"
		if _, err := shell.ExecCmdWithStream(cmd, true, chrootEnv.ChrootEnvRoot, nil); err != nil {
			return fmt.Errorf("failed to clean cache for chroot repository: %w", err)
		}

		cmd = "apt update"
		if _, err := shell.ExecCmdWithStream(cmd, true, chrootEnv.ChrootEnvRoot, nil); err != nil {
			return fmt.Errorf("failed to refresh cache for chroot repository: %w", err)
		}
	} else {
		return fmt.Errorf("unsupported package type: %s", pkgType)
	}
	return nil
}

func (chrootEnv *ChrootEnv) initChrootLocalRepo(targetArch string) error {
	chrootPkgCacheDir := chrootEnv.GetChrootPkgCacheDir()
	if err := chrootEnv.MountChrootPath(chrootPkgCacheDir, ChrootRepoDir, "--bind"); err != nil {
		return fmt.Errorf("failed to mount package cache directory %s to chroot repo directory %s: %w",
			chrootPkgCacheDir, ChrootRepoDir, err)
	}

	if chrootEnv.ChrootEnvRoot != shell.HostPath {
		// Within iso initramfs system, local repo metadata should have been generated
		// And the repo cache is read-only, not able to update by live-installer
		if err := chrootEnv.UpdateChrootLocalRepoMetadata(ChrootRepoDir, targetArch, false); err != nil {
			return fmt.Errorf("failed to update chroot local cache repository metadata: %w", err)
		}
	}

	if err := chrootEnv.RefreshLocalCacheRepo(); err != nil {
		return fmt.Errorf("failed to refresh local cache repository: %w", err)
	}
	return nil
}

func (chrootEnv *ChrootEnv) createChrootRepo(targetOs, targetDist string) error {
	var repoConfigDir string
	var repoConfigFile string

	targetOsConfigDir := chrootEnv.GetTargetOsConfigDir()
	pkgType := chrootEnv.GetTargetOsPkgType()
	if pkgType == "rpm" {
		repoConfigDir = RPMRepoConfigDir
		repoConfigFile = RPMRepoConfigFile
	} else if pkgType == "deb" {
		repoConfigDir = DEBRepoConfigDir
		repoConfigFile = DEBRepoConfigFile
	} else {
		return fmt.Errorf("unsupported package type: %s", pkgType)
	}

	// Backup existing local repo config files in chroot environment
	chrootRepoCongfigPath, err := chrootEnv.GetChrootEnvHostPath(repoConfigDir)
	if err != nil {
		return fmt.Errorf("failed to get chroot host path for local repo config: %w", err)
	}
	if _, err := os.Stat(chrootRepoCongfigPath); err == nil {
		if files, _ := os.ReadDir(chrootRepoCongfigPath); len(files) != 0 {
			repoConfigBackupPath := filepath.Join(chrootEnv.ChrootEnvRoot, "repo-config-backup")
			if err := file.CopyDir(chrootRepoCongfigPath, repoConfigBackupPath, "-f", true); err != nil {
				return fmt.Errorf("failed to backup existing repo config files: %w", err)
			}
			if _, err := shell.ExecCmd("rm -f "+chrootRepoCongfigPath+"/*", true, shell.HostPath, nil); err != nil {
				return fmt.Errorf("failed to remove existing local repo config files: %w", err)
			}
		}
	}

	// Copy local repo config file to chroot environment
	localRepoConfigPath := filepath.Join(targetOsConfigDir, "chrootenvconfigs", repoConfigFile)
	if _, err := os.Stat(localRepoConfigPath); os.IsNotExist(err) {
		return fmt.Errorf("chroot repo config file does not exist: %s", localRepoConfigPath)
	}

	repoConfigDistFile := filepath.Join(repoConfigDir, repoConfigFile)
	if err := chrootEnv.CopyFileFromHostToChroot(localRepoConfigPath, repoConfigDistFile); err != nil {
		return fmt.Errorf("failed to copy local.repo: %w", err)
	}

	return nil
}

func (chrootEnv *ChrootEnv) initChrootWorkspace() error {
	chrootWorkspace := filepath.Join(chrootEnv.ChrootEnvRoot, "workspace")
	chrootEnv.ChrootImageBuildDir = filepath.Join(chrootWorkspace, "imagebuild")
	if _, err := os.Stat(chrootEnv.ChrootImageBuildDir); os.IsNotExist(err) {
		if _, err = shell.ExecCmd("mkdir -p "+chrootEnv.ChrootImageBuildDir, true, shell.HostPath, nil); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", chrootEnv.ChrootImageBuildDir, err)
		}
	}
	return nil
}

func (chrootEnv *ChrootEnv) InitChrootEnv(targetOs, targetDist, targetArch string) (err error) {
	if files, _ := os.ReadDir(chrootEnv.ChrootEnvRoot); len(files) == 0 {
		chrootBuildDir := chrootEnv.ChrootBuilder.GetChrootBuildDir()
		chrootEnvTarPath := filepath.Join(chrootBuildDir, "chrootenv.tar.gz")
		if _, err := os.Stat(chrootEnvTarPath); os.IsNotExist(err) {
			if templateAwareBuilder, ok := chrootEnv.ChrootBuilder.(interface{ SetBuildTemplate(*config.ImageTemplate) }); ok {
				templateAwareBuilder.SetBuildTemplate(chrootEnv.buildTemplate)
			}
			// Build chroot environment tarball
			if err = chrootEnv.ChrootBuilder.BuildChrootEnv(targetOs, targetDist, targetArch); err != nil {
				return fmt.Errorf("failed to build chroot environment: %w", err)
			}
		}

		// Extract chroot environment tarball
		if err = compression.DecompressFile(chrootEnvTarPath, chrootEnv.ChrootEnvRoot, "tar.gz", true); err != nil {
			return fmt.Errorf("failed to extract chroot environment tarball: %w", err)
		}

		// Copy resolv.conf to the chroot environment
		if err = chrootEnv.CopyFileFromHostToChroot(ResolvConfPath, "/etc/"); err != nil {
			return fmt.Errorf("failed to copy resolv.conf: %w", err)
		}
	}

	// Initialize the chroot workspace
	if err = chrootEnv.initChrootWorkspace(); err != nil {
		return fmt.Errorf("failed to initialize chroot workspace: %w", err)
	}

	if chrootEnv.ChrootEnvRoot != shell.HostPath {
		// Mount sysfs to the chroot environment
		err = chrootEnv.MountChrootSysfs("/")
		if err != nil {
			return fmt.Errorf("failed to mount sysfs for chroot environment: %w", err)
		}

		defer func() {
			if err != nil {
				if umountErr := chrootEnv.UmountChrootSysfs("/"); umountErr != nil {
					log.Errorf("Failed to unmount sysfs for chroot environment: %v", umountErr)
					err = fmt.Errorf("operation failed: %w, cleanup errors: %v", err, umountErr)
				}
			}
		}()
	}

	// Create chroot local repository
	if err = chrootEnv.createChrootRepo(targetOs, targetDist); err != nil {
		return fmt.Errorf("failed to create chroot repository: %w", err)
	}

	if err = chrootEnv.initChrootLocalRepo(targetArch); err != nil {
		return fmt.Errorf("failed to initialize chroot local repository: %w", err)
	}

	return nil
}

func (chrootEnv *ChrootEnv) CleanupChrootEnv(targetOs, targetDist, targetArch string) error {
	log := logger.Logger()
	if _, err := os.Stat(chrootEnv.ChrootEnvRoot); err == nil {
		if err := system.StopGPGComponents(chrootEnv.ChrootEnvRoot); err != nil {
			return fmt.Errorf("failed to stop GPG components in chroot environment: %w", err)
		}
		if err := mount.UmountSubPath(chrootEnv.ChrootEnvRoot); err != nil {
			return fmt.Errorf("failed to unmount path for chroot environment: %w", err)
		}

		// Restore existing local repo config files in chroot environment
		repoConfigBackupPath := filepath.Join(chrootEnv.ChrootEnvRoot, "repo-config-backup")
		if _, err := os.Stat(repoConfigBackupPath); err == nil {
			var repoConfigDir string
			pkgType := chrootEnv.GetTargetOsPkgType()
			if pkgType == "rpm" {
				repoConfigDir = RPMRepoConfigDir
			} else if pkgType == "deb" {
				repoConfigDir = DEBRepoConfigDir
			} else {
				return fmt.Errorf("unsupported package type: %s", pkgType)
			}

			chrootRepoCongfigPath, err := chrootEnv.GetChrootEnvHostPath(repoConfigDir)
			if err != nil {
				return fmt.Errorf("failed to get chroot host path for local repo config: %w", err)
			}
			if _, err := os.Stat(chrootRepoCongfigPath); err == nil {
				if files, _ := os.ReadDir(chrootRepoCongfigPath); len(files) != 0 {
					if _, err := shell.ExecCmd("rm -f "+chrootRepoCongfigPath+"/*", true, shell.HostPath, nil); err != nil {
						return fmt.Errorf("failed to remove existing local repo config files: %w", err)
					}
					if err := file.CopyDir(repoConfigBackupPath, chrootRepoCongfigPath, "-f", true); err != nil {
						return fmt.Errorf("failed to backup existing repo config files: %w", err)
					}
					if _, err := shell.ExecCmd("rm -rf "+repoConfigBackupPath, true, shell.HostPath, nil); err != nil {
						return fmt.Errorf("failed to remove repo config backup directory %s: %w", repoConfigBackupPath, err)
					}
				}
			}
		}
	} else {
		log.Infof("Chroot environment root %s does not exist, skipping cleanup", chrootEnv.ChrootEnvRoot)
	}
	return nil
}

// getPackageManagerCmd returns the appropriate package manager command based on target OS
func (chrootEnv *ChrootEnv) getPackageManagerCmd() string {
	if strings.Contains(chrootEnv.TargetOs, "redhat-compatible-distro") {
		return "dnf"
	}
	return "tdnf"
}

// buildInstallCmd builds the package installation command based on the package manager
func (chrootEnv *ChrootEnv) buildInstallCmd(packageName, chrootInstallRoot string, repositoryIDList []string) string {
	pkgManager := chrootEnv.getPackageManagerCmd()
	releaseVersion := chrootEnv.GetTargetOsReleaseVersion()

	if pkgManager == "dnf" {
		// dnf syntax for RCD builds (similar to tdnf but with dnf)
		installCmd := fmt.Sprintf("dnf install %s -y --nogpgcheck --installroot %s --setopt=reposdir=%s",
			packageName, chrootInstallRoot, RPMRepoConfigDir)

		// Add repository configuration for dnf
		if len(repositoryIDList) > 0 {
			installCmd += " --disablerepo=*"
			for _, repoID := range repositoryIDList {
				installCmd += " --enablerepo=" + repoID
			}
		}
		return installCmd
	} else {
		// tdnf original syntax
		installCmd := fmt.Sprintf("tdnf install %s --releasever %s --setopt reposdir=%s --nogpgcheck --assumeyes --installroot %s",
			packageName, releaseVersion, RPMRepoConfigDir, chrootInstallRoot)

		// Add repository configuration for tdnf
		if len(repositoryIDList) > 0 {
			installCmd += " --disablerepo=*"
			for _, repoID := range repositoryIDList {
				installCmd += " --enablerepo=" + repoID
			}
		}
		return installCmd
	}
}

func (chrootEnv *ChrootEnv) TdnfInstallPackage(packageName, installRoot string, repositoryIDList []string) error {
	packageName = CleanRpmName(packageName)

	chrootInstallRoot, err := chrootEnv.GetChrootEnvPath(installRoot)
	if err != nil {
		return fmt.Errorf("failed to get chroot environment path for install root %s: %w", installRoot, err)
	}

	installCmd := chrootEnv.buildInstallCmd(packageName, chrootInstallRoot, repositoryIDList)

	if _, err := shell.ExecCmdWithStream(installCmd, true, chrootEnv.ChrootEnvRoot, nil); err != nil {
		return fmt.Errorf("failed to install package %s: %w", packageName, err)
	}

	return nil
}

func CleanDebName(packageName string) string {
	packageName = strings.Replace(packageName, "_", "=", 1)
	if idx := strings.LastIndex(packageName, "_"); idx != -1 {
		archTag := packageName[idx+1:]
		switch archTag {
		case "amd64", "arm64", "all":
			packageName = packageName[:idx]
		}
	}
	return packageName
}

// CleanRpmName normalizes version-suffixed RPM package requests to canonical package names.
// Example: qemu-kvm-9.1.0-7 -> qemu-kvm.
func CleanRpmName(packageName string) string {
	packageName = strings.TrimSpace(packageName)
	packageName = strings.TrimSuffix(packageName, ".rpm")

	if idx := strings.LastIndex(packageName, "."); idx != -1 {
		if _, ok := knownRPMArch[packageName[idx+1:]]; ok {
			packageName = packageName[:idx]
		}
	}

	matches := rpmVersionSuffixRe.FindStringSubmatch(packageName)
	if len(matches) != 4 {
		return packageName
	}

	if strings.Contains(matches[2], ".") {
		return matches[1]
	}

	return packageName
}

func (chrootEnv *ChrootEnv) AptInstallPackage(packageName, installRoot string, repoSrcList []string) error {
	packageName = CleanDebName(packageName)
	installCmd := fmt.Sprintf("apt-get install -y --no-install-recommends %s", packageName)

	if len(repoSrcList) > 0 {
		for _, repoSrc := range repoSrcList {
			installCmd += fmt.Sprintf(" -o Dir::Etc::sourcelist=%s", repoSrc)
		}
	}

	// Set environment variables to ensure non-interactive installation
	envVars := []string{
		"DEBIAN_FRONTEND=noninteractive",
		"DEBCONF_NONINTERACTIVE_SEEN=true",
		"DEBCONF_NOWARNINGS=yes",
	}

	output, err := shell.ExecCmdWithStream(installCmd, true, installRoot, envVars)
	if err != nil {
		log.Errorf("Failed to install package %s: %v", packageName, err)
		log.Errorf("Full apt-get output for %s:\n%s", packageName, output)
		return fmt.Errorf("failed to install package %s: %w\napt output:\n%s", packageName, err, output)
	}

	return nil
}

func (chrootEnv *ChrootEnv) UpdateSystemPkgs(template *config.ImageTemplate) error {
	// Update essential packages
	essentialPkgList, err := chrootEnv.GetChrootEnvEssentialPackageList()
	if err != nil {
		return fmt.Errorf("failed to get essential package list: %w", err)
	}
	template.EssentialPkgList = essentialPkgList

	// Update bootloader packages by bootloader type
	bootloaderConfig := template.GetBootloaderConfig()
	// To do: support bootloader package selection by bootloader type
	switch bootloaderConfig.Provider {
	case "grub":
		if bootloaderConfig.BootType == "efi" {
			template.BootloaderPkgList = []string{}
		} else if bootloaderConfig.BootType == "legacy" {
			template.BootloaderPkgList = []string{}
		} else {
			return fmt.Errorf("unsupported boot type: %s", bootloaderConfig.BootType)
		}
	case "systemd-boot":
		template.BootloaderPkgList = []string{}
	default:
		return fmt.Errorf("unsupported bootloader provider: %s", bootloaderConfig.Provider)
	}

	// Update kernel packages by kernel version
	kernelConfig := template.GetKernel()
	if kernelConfig.Version == "" {
		// Get the latest kernel version package by default
		template.KernelPkgList = kernelConfig.Packages
	} else {
		// To do: search for exact kernel version package name
		template.KernelPkgList = kernelConfig.Packages
	}

	return nil
}
