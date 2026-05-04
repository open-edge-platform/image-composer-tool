package rpm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/mount"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/system"
)

var log = logger.Logger()

type RpmInstallerInterface interface {
	InstallRpmPkg(targetOs, chrootPath, cacheDir string, packages []string) error
}

type RpmInstaller struct {
}

func NewRpmInstaller() *RpmInstaller {
	return &RpmInstaller{}
}

func (rpmInstaller *RpmInstaller) cleanupOnSuccess(chrootEnvPath string, err *error) {
	if umountErr := mount.UmountSysfs(chrootEnvPath); umountErr != nil {
		log.Errorf("failed to unmount system directories in chroot environment: %v", umountErr)
		*err = fmt.Errorf("failed to unmount system directories in chroot environment: %w", umountErr)
		return
	}
	if cleanErr := mount.CleanSysfs(chrootEnvPath); cleanErr != nil {
		log.Errorf("failed to clean system directories in chroot environment: %v", cleanErr)
		*err = fmt.Errorf("failed to clean system directories in chroot environment: %w", cleanErr)
	}
}

func (rpmInstaller *RpmInstaller) cleanupOnError(chrootEnvPath string, err *error) {
	if umountErr := mount.UmountSysfs(chrootEnvPath); umountErr != nil {
		log.Errorf("failed to unmount system directories in chroot environment: %v", umountErr)
		*err = fmt.Errorf("operation failed: %w, cleanup errors: %v", *err, umountErr)
		return
	}
	if cleanErr := mount.CleanSysfs(chrootEnvPath); cleanErr != nil {
		log.Errorf("failed to clean system directories in chroot environment: %v", cleanErr)
		*err = fmt.Errorf("operation failed: %w, cleanup errors: %v", *err, cleanErr)
		return
	}
	if _, RemoveErr := shell.ExecCmd("rm -rf "+chrootEnvPath, true, shell.HostPath, nil); RemoveErr != nil {
		log.Errorf("failed to remove chroot environment build path: %v", RemoveErr)
		*err = fmt.Errorf("operation failed: %w, cleanup errors: %v", *err, RemoveErr)
	}
}

func (rpmInstaller *RpmInstaller) InstallRpmPkg(targetOs, chrootEnvPath, chrootPkgCacheDir string, allPkgsList []string) (err error) {
	chrootRpmDbPath := filepath.Join(chrootEnvPath, "var", "lib", "rpm")
	if _, err := os.Stat(chrootRpmDbPath); os.IsNotExist(err) {
		if _, err := shell.ExecCmd("mkdir -p "+chrootRpmDbPath, true, shell.HostPath, nil); err != nil {
			log.Errorf("Failed to create chroot RPM database directory: %v", err)
			return fmt.Errorf("failed to create chroot environment directory: %w", err)
		}
	}

	if err = mount.MountSysfs(chrootEnvPath); err != nil {
		log.Errorf("failed to mount system directories in chroot environment: %v", err)
		return fmt.Errorf("failed to mount system directories in chroot environment: %w", err)
	}

	defer func() {
		if err == nil {
			rpmInstaller.cleanupOnSuccess(chrootEnvPath, &err)
		} else {
			rpmInstaller.cleanupOnError(chrootEnvPath, &err)
		}
	}()

	for _, pkg := range allPkgsList {
		pkgPath := filepath.Join(chrootPkgCacheDir, pkg)
		if _, err = os.Stat(pkgPath); os.IsNotExist(err) {
			log.Errorf("Package %s does not exist in cache directory: %v", pkg, err)
			return fmt.Errorf("package %s does not exist in cache directory: %w", pkg, err)
		}
		log.Infof("Installing package %s in chroot environment", pkg)
		cmdStr := fmt.Sprintf("rpm -i -v --nodeps --force --ignorearch --root %s --define '_dbpath /var/lib/rpm' %s",
			chrootEnvPath, pkgPath)
		var output string
		output, err = shell.ExecCmd(cmdStr, true, shell.HostPath, nil)
		if err != nil {
			log.Errorf("Failed to install package %s: %v, output: %s", pkg, err, output)
			return fmt.Errorf("failed to install package %s: %w, output: %s", pkg, err, output)
		}
	}

	if err = rpmInstaller.updateRpmDB(chrootEnvPath, chrootPkgCacheDir, allPkgsList); err != nil {
		return fmt.Errorf("failed to update RPM database in chroot environment: %w", err)
	}
	if err = importGpgKeys(targetOs, chrootEnvPath); err != nil {
		return fmt.Errorf("failed to import GPG keys in chroot environment: %w", err)
	}
	if err = system.StopGPGComponents(chrootEnvPath); err != nil {
		return fmt.Errorf("failed to stop GPG components in chroot environment: %w", err)
	}

	return nil
}

// updateRpmDB updates the RPM database in the chroot environment
func (rpmInstaller *RpmInstaller) updateRpmDB(chrootEnvBuildPath, chrootPkgCacheDir string, rpmList []string) (err error) {
	cmdStr := "rpm -E '%{_db_backend}'"
	hostRpmDbBackend, err := shell.ExecCmd(cmdStr, false, shell.HostPath, nil)
	if err != nil {
		log.Errorf("Failed to get host RPM DB backend: %v", err)
		return fmt.Errorf("failed to get host RPM DB backend: %w", err)
	}
	hostRpmDbBackend = strings.TrimSpace(hostRpmDbBackend)
	chrootRpmDbBackend, err := shell.ExecCmd(cmdStr, false, chrootEnvBuildPath, nil)
	if err != nil {
		log.Errorf("Failed to get chroot RPM DB backend: %v", err)
		return fmt.Errorf("failed to get chroot RPM DB backend: %w", err)
	}
	chrootRpmDbBackend = strings.TrimSpace(chrootRpmDbBackend)
	if hostRpmDbBackend == chrootRpmDbBackend {
		log.Debugf("The host RPM DB: " + hostRpmDbBackend + " matches the chroot RPM DB: " + chrootRpmDbBackend)
		log.Debugf("Not rebuilding the chroot RPM database.")
		return nil
	}

	log.Debugf("The host RPM DB: " + hostRpmDbBackend + " differs from the chroot RPM DB: " + chrootRpmDbBackend)
	log.Debugf("Rebuilding the chroot RPM database.")
	if _, err = shell.ExecCmd("rm -rf /var/lib/rpm/*", true, chrootEnvBuildPath, nil); err != nil {
		log.Errorf("Failed to remove RPM database: %v", err)
		return fmt.Errorf("failed to remove RPM database: %w", err)
	}
	if _, err = shell.ExecCmd("rpm --initdb", false, chrootEnvBuildPath, nil); err != nil {
		log.Errorf("Failed to initialize RPM database: %v", err)
		return fmt.Errorf("failed to initialize RPM database: %w", err)
	}

	chrootPkgDir := filepath.Join(chrootEnvBuildPath, "packages")
	if err = mount.MountPath(chrootPkgCacheDir, chrootPkgDir, "--bind"); err != nil {
		log.Errorf("Failed to mount package cache directory: %v", err)
		return fmt.Errorf("failed to mount package cache directory: %w", err)
	}

	defer func() {
		if umountErr := mount.UmountAndDeletePath(chrootPkgDir); umountErr != nil {
			log.Errorf("Failed to unmount and delete path %s: %v", chrootPkgDir, umountErr)
			if err == nil {
				err = fmt.Errorf("failed to unmount and delete path %s: %w", chrootPkgDir, err)
			} else {
				err = fmt.Errorf("operation failed: %w, cleanup errors: %v", err, umountErr)
			}
		}
	}()

	for _, rpm := range rpmList {
		rpmChrootPath := filepath.Join("/packages", rpm)
		cmdStr := "rpm -i -v --nodeps --force --ignorearch --justdb " + rpmChrootPath
		if _, err := shell.ExecCmdWithStream(cmdStr, true, chrootEnvBuildPath, nil); err != nil {
			log.Errorf("Failed to update RPM Database for %s in chroot environment: %v", rpm, err)
			return fmt.Errorf("failed to update RPM Database for %s in chroot environment: %w", rpm, err)
		}
	}

	return nil
}

// importGpgKeys imports GPG keys into the chroot environment
func importGpgKeys(targetOs string, chrootEnvBuildPath string) error {
	var cmdStr string
	switch targetOs {
	case "edge-microvisor-toolkit":
		cmdStr = "rpm -q -l edge-repos-shared | grep 'rpm-gpg'"
	case "azure-linux":
		cmdStr = "rpm -q -l azurelinux-repos-shared | grep 'rpm-gpg'"
	case "redhat-compatible-distro":
		cmdStr = "rpm -q -l centos-gpg-keys | grep 'RPM-GPG-KEY'"
	}

	output, err := shell.ExecCmd(cmdStr, false, chrootEnvBuildPath, nil)
	if err != nil {
		log.Errorf("Failed to get GPG keys: %v", err)
		return fmt.Errorf("failed to get GPG keys: %w", err)
	}
	if output != "" {
		gpgKeys := strings.Split(output, "\n")
		log.Infof("Importing GPG key: " + gpgKeys[0])
		cmdStr = "rpm --import " + gpgKeys[0]
		_, err = shell.ExecCmd(cmdStr, false, chrootEnvBuildPath, nil)
		if err != nil {
			log.Errorf("Failed to import GPG key: %v", err)
			return fmt.Errorf("failed to import GPG key: %w", err)
		}
	} else {
		log.Errorf("No GPG keys found in the chroot environment")
		return fmt.Errorf("no GPG keys found in the chroot environment")
	}
	return nil
}
