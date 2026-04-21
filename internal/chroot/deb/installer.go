package deb

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/mount"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

var log = logger.Logger()

type DebInstallerInterface interface {
	UpdateLocalDebRepo(cacheDir, arch string, sudo bool) error
	InstallDebPkg(configDir, chrootPath, cacheDir string, packages []string) error
}

type DebInstaller struct {
}

func NewDebInstaller() *DebInstaller {
	return &DebInstaller{}
}

func (debInstaller *DebInstaller) cleanupOnSuccess(repoPath string, err *error) {
	if umountErr := mount.UmountPath(repoPath); umountErr != nil {
		log.Errorf("Failed to unmount debian local repository: %v", umountErr)
		*err = fmt.Errorf("failed to unmount debian local repository: %w", umountErr)
	}
}

func (debInstaller *DebInstaller) cleanupOnError(chrootEnvPath, repoPath string, err *error) {
	if umountErr := mount.UmountPath(repoPath); umountErr != nil {
		log.Errorf("Failed to unmount debian local repository: %v", umountErr)
		*err = fmt.Errorf("operation failed: %w, cleanup errors: %v", *err, umountErr)
		return
	}

	if _, RemoveErr := shell.ExecCmd("rm -rf "+chrootEnvPath, true, shell.HostPath, nil); RemoveErr != nil {
		log.Errorf("Failed to remove chroot environment build path: %v", RemoveErr)
		*err = fmt.Errorf("operation failed: %w, cleanup errors: %v", *err, RemoveErr)
	}
}

func (debInstaller *DebInstaller) UpdateLocalDebRepo(repoPath, targetArch string, sudo bool) error {
	if repoPath == "" {
		return fmt.Errorf("repository path cannot be empty")
	}

	switch targetArch {
	case "amd64", "x86_64":
		targetArch = "amd64"
	case "arm64", "aarch64":
		targetArch = "arm64"
	default:
		return fmt.Errorf("unsupported architecture: %s", targetArch)
	}

	metaDataPath := filepath.Join(repoPath,
		fmt.Sprintf("dists/stable/main/binary-%s", targetArch), "Packages.gz")
	if _, err := os.Stat(metaDataPath); err == nil {
		if _, err = shell.ExecCmd("rm -f "+metaDataPath, sudo, shell.HostPath, nil); err != nil {
			return fmt.Errorf("failed to remove existing Packages.gz: %w", err)
		}
	}
	metaDataDir := filepath.Dir(metaDataPath)
	if _, err := os.Stat(metaDataDir); os.IsNotExist(err) {
		if _, err = shell.ExecCmd("mkdir -p "+metaDataDir, sudo, shell.HostPath, nil); err != nil {
			return fmt.Errorf("failed to create metadata directory %s: %w", metaDataDir, err)
		}
	}

	// Escape any double quotes and dollar signs in the paths
	safeRepoPath := strings.ReplaceAll(repoPath, `"`, `\"`)
	safeRepoPath = strings.ReplaceAll(safeRepoPath, "$", `\$`)
	safeMetaDataPath := strings.ReplaceAll(metaDataPath, `"`, `\"`)
	safeMetaDataPath = strings.ReplaceAll(safeMetaDataPath, "$", `\$`)

	cmd := fmt.Sprintf("bash -c \"cd %s && dpkg-scanpackages . /dev/null | gzip -9c > %s\"", safeRepoPath, safeMetaDataPath)
	if _, err := shell.ExecCmd(cmd, sudo, shell.HostPath, nil); err != nil {
		return fmt.Errorf("failed to create local debian cache repository: %w", err)
	}

	return nil
}

func (debInstaller *DebInstaller) InstallDebPkg(targetOsConfigDir, chrootEnvPath, chrootPkgCacheDir string, pkgsList []string) (err error) {
	if chrootEnvPath == "" || chrootPkgCacheDir == "" || len(pkgsList) == 0 {
		return fmt.Errorf("invalid parameters: chrootEnvPath, chrootPkgCacheDir, and pkgsList cannot be empty")
	}

	// from local.list
	repoPath := "/cdrom/cache-repo"
	pkgListStr := strings.Join(pkgsList, ",")

	localRepoConfigPath := filepath.Join(targetOsConfigDir, "chrootenvconfigs", "local.list")
	if _, err := os.Stat(localRepoConfigPath); os.IsNotExist(err) {
		log.Errorf("Local repository config file does not exist: %s", localRepoConfigPath)
		return fmt.Errorf("local repository config file does not exist: %s", localRepoConfigPath)
	}

	if err := mount.MountPath(chrootPkgCacheDir, repoPath, "--bind"); err != nil {
		log.Errorf("Failed to mount debian local repository: %v", err)
		return fmt.Errorf("failed to mount debian local repository: %w", err)
	}

	defer func() {
		if err == nil {
			debInstaller.cleanupOnSuccess(repoPath, &err)
		} else {
			debInstaller.cleanupOnError(chrootEnvPath, repoPath, &err)
		}
	}()

	if _, err := os.Stat(chrootEnvPath); os.IsNotExist(err) {
		if err := os.MkdirAll(chrootEnvPath, 0700); err != nil {
			log.Errorf("Failed to create chroot environment directory: %v", err)
			return fmt.Errorf("failed to create chroot environment directory: %w", err)
		}
	}

	cmd := fmt.Sprintf("mmdebstrap "+
		"--variant=custom "+
		"--format=directory "+
		"--aptopt=APT::Authentication::Trusted=true "+
		"--aptopt=Dpkg::Options::=--force-confdef "+
		"--aptopt=Dpkg::Options::=--force-confold "+
		"--aptopt=APT::Get::Assume-Yes=true "+
		"--hook-dir=/usr/share/mmdebstrap/hooks/file-mirror-automount "+
		"--include=%s "+
		"--verbose --debug "+
		"-- bookworm %s %s",
		pkgListStr, chrootEnvPath, localRepoConfigPath)

	// Set environment variables to ensure non-interactive installation
	envVars := []string{
		"DEBIAN_FRONTEND=noninteractive",
		"DEBCONF_NONINTERACTIVE_SEEN=true",
		"DEBCONF_NOWARNINGS=yes",
	}

	if _, err = shell.ExecCmdWithStream(cmd, true, shell.HostPath, envVars); err != nil {
		log.Errorf("Failed to install debian packages in chroot environment: %v", err)
		return fmt.Errorf("failed to install debian packages in chroot environment: %w", err)
	}

	return nil
}
