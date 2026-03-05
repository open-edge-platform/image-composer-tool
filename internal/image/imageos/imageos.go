package imageos

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/open-edge-platform/os-image-composer/internal/chroot"
	"github.com/open-edge-platform/os-image-composer/internal/config"
	"github.com/open-edge-platform/os-image-composer/internal/config/manifest"
	"github.com/open-edge-platform/os-image-composer/internal/image/imageboot"
	"github.com/open-edge-platform/os-image-composer/internal/image/imagedisc"
	"github.com/open-edge-platform/os-image-composer/internal/image/imagesecure"
	"github.com/open-edge-platform/os-image-composer/internal/image/imagesign"
	"github.com/open-edge-platform/os-image-composer/internal/ospackage"
	"github.com/open-edge-platform/os-image-composer/internal/ospackage/debutils"
	"github.com/open-edge-platform/os-image-composer/internal/ospackage/rpmutils"
	"github.com/open-edge-platform/os-image-composer/internal/utils/file"
	"github.com/open-edge-platform/os-image-composer/internal/utils/logger"
	"github.com/open-edge-platform/os-image-composer/internal/utils/mount"
	"github.com/open-edge-platform/os-image-composer/internal/utils/shell"
	"github.com/open-edge-platform/os-image-composer/internal/utils/slice"
)

type ImageOsInterface interface {
	GetInstallRoot() string
	InstallInitrd() (installRoot, versionInfo string, err error)
	InstallImageOs(diskPathIdMap map[string]string) (versionInfo string, err error)
}

type ImageOs struct {
	installRoot string
	template    *config.ImageTemplate
	chrootEnv   chroot.ChrootEnvInterface
	imageBoot   imageboot.ImageBootInterface
}

var log = logger.Logger()

func NewImageOs(chrootEnv chroot.ChrootEnvInterface, template *config.ImageTemplate) (*ImageOs, error) {
	chrootImageBuildDir := chrootEnv.GetChrootImageBuildDir()
	if _, err := os.Stat(chrootImageBuildDir); os.IsNotExist(err) {
		log.Errorf("Chroot image build directory does not exist: %s", chrootImageBuildDir)
		return nil, fmt.Errorf("chroot image build directory does not exist: %s", chrootImageBuildDir)
	}
	if template == nil {
		return nil, fmt.Errorf("image template cannot be nil")
	}
	sysConfigName := template.GetSystemConfigName()
	installRoot := filepath.Join(chrootImageBuildDir, sysConfigName)
	if _, err := shell.ExecCmd("mkdir -p "+installRoot, true, shell.HostPath, nil); err != nil {
		log.Errorf("Failed to create install root directory %s: %v", installRoot, err)
		return nil, fmt.Errorf("failed to create directory %s: %w", installRoot, err)
	}

	imageBoot := imageboot.NewImageBoot()

	return &ImageOs{
		installRoot: installRoot,
		template:    template,
		chrootEnv:   chrootEnv,
		imageBoot:   imageBoot,
	}, nil
}

func (imageOs *ImageOs) GetInstallRoot() string {
	return imageOs.installRoot
}

func (imageOs *ImageOs) InstallInitrd() (installRoot, versionInfo string, err error) {
	installRoot = imageOs.installRoot
	versionInfo = ""
	log.Infof("Installing initrd for image: %s", imageOs.template.GetImageName())

	pkgType := imageOs.chrootEnv.GetTargetOsPkgType()
	if pkgType == "deb" {
		if err = imageOs.initRootfsForDeb(imageOs.installRoot); err != nil {
			err = fmt.Errorf("failed to initialize rootfs for deb: %w", err)
			return
		}
	}

	if err = imageOs.mountSysfsToRootfs(imageOs.installRoot); err != nil {
		return
	}

	defer func() {
		if umountErr := imageOs.umountSysfsFromRootfs(imageOs.installRoot); umountErr != nil {
			if err != nil {
				err = fmt.Errorf("operation failed: %w, cleanup errors: %v", err, umountErr)
			} else {
				err = fmt.Errorf("failed to unmount sysfs from image rootfs: %w", umountErr)
			}
		}
	}()

	log.Infof("Image installation pre-processing...")
	if err = preImageOsInstall(imageOs.installRoot, imageOs.template); err != nil {
		err = fmt.Errorf("pre-install failed: %w", err)
		return
	}

	log.Infof("Image package installation...")
	if err = imageOs.installImagePkgs(imageOs.installRoot, imageOs.template); err != nil {
		err = fmt.Errorf("failed to install image packages: %w", err)
		return
	}

	log.Infof("Image system configuration...")
	if err = updateInitrdConfig(imageOs.installRoot, imageOs.template); err != nil {
		err = fmt.Errorf("failed to update image config: %w", err)
		return
	}

	log.Infof("Image installation post-processing...")
	versionInfo, err = imageOs.postImageOsInstall(imageOs.installRoot, imageOs.template)
	if err != nil {
		err = fmt.Errorf("post-install failed: %w", err)
		return
	}

	return
}

func (imageOs *ImageOs) InstallImageOs(diskPathIdMap map[string]string) (versionInfo string, err error) {
	versionInfo = ""
	var mountPointInfoList []map[string]string
	var mounted bool = false
	log.Infof("Installing OS for image: %s", imageOs.template.GetImageName())

	defer func() {
		if mounted {
			if umountErr := imageOs.umountDiskFromChroot(imageOs.installRoot, mountPointInfoList); umountErr != nil {
				if err != nil {
					err = fmt.Errorf("operation failed: %w, cleanup errors: %v", err, umountErr)
				} else {
					err = fmt.Errorf("failed to unmount disk from chroot: %w", umountErr)
				}
			}
		}
	}()

	pkgType := imageOs.chrootEnv.GetTargetOsPkgType()
	if pkgType == "deb" {
		if err = mountDiskRootToChroot(imageOs.installRoot, diskPathIdMap, imageOs.template); err != nil {
			err = fmt.Errorf("failed to mount disk root to chroot: %w", err)
			return
		}
		mounted = true
		if err = imageOs.initRootfsForDeb(imageOs.installRoot); err != nil {
			err = fmt.Errorf("failed to initialize rootfs for deb: %w", err)
			return
		}
	}

	mountPointInfoList, err = imageOs.mountDiskToChroot(imageOs.installRoot, diskPathIdMap, imageOs.template)
	if err != nil {
		err = fmt.Errorf("failed to mount disk to chroot: %w", err)
		return
	}
	mounted = true

	log.Infof("Image installation pre-processing...")
	if err = preImageOsInstall(imageOs.installRoot, imageOs.template); err != nil {
		err = fmt.Errorf("pre-install failed: %w", err)
		return
	}

	log.Infof("Image package installation...")
	if err = imageOs.installImagePkgs(imageOs.installRoot, imageOs.template); err != nil {
		err = fmt.Errorf("failed to install image packages: %w", err)
		return
	}

	log.Infof("Image Kernel symlinks creation...")
	if err := fixKernelSymlinks(imageOs.installRoot); err != nil {
		// Don't fail the build if symlink fix fails, just warn as some distros may not need it
		log.Warnf("Failed to fix kernel symlinks: %v (continuing anyway)", err)
	}

	log.Infof("Image system configuration...")
	if err = updateImageConfig(imageOs.installRoot, diskPathIdMap, imageOs.template); err != nil {
		err = fmt.Errorf("failed to update image config: %w", err)
		return
	}

	log.Infof("Installing bootloader...")
	if err = imageOs.imageBoot.InstallImageBoot(imageOs.installRoot, diskPathIdMap, imageOs.template, pkgType); err != nil {
		err = fmt.Errorf("failed to install image boot: %w", err)
		return
	}

	log.Infof("Image SBOM generation...")
	versionInfo, err = imageOs.generateSBOM(imageOs.installRoot, imageOs.template)
	if err != nil {
		err = fmt.Errorf("generating SBOM failed: %w", err)
		return
	}

	if err = imagesecure.ConfigImageSecurity(imageOs.installRoot, imageOs.template); err != nil {
		err = fmt.Errorf("failed to configure image security: %w", err)
		return
	}

	log.Infof("Configuring UKI... ")
	if err = buildImageUKI(imageOs.installRoot, imageOs.template); err != nil {
		err = fmt.Errorf("failed to configure UKI: %w", err)
		return
	}

	log.Infof("Configuring Sign Image...")
	if err = imagesign.SignImage(imageOs.installRoot, imageOs.template); err != nil {
		err = fmt.Errorf("failed to sign image: %w", err)
		return
	}

	log.Infof("Image installation post-processing...")
	versionInfo, err = imageOs.postImageOsInstall(imageOs.installRoot, imageOs.template)
	if err != nil {
		err = fmt.Errorf("post-install failed: %w", err)
		return
	}

	return
}

func (imageOs *ImageOs) initRootfsForDeb(installRoot string) error {
	essentialPkgsList, err := imageOs.chrootEnv.GetChrootEnvEssentialPackageList()
	if err != nil {
		return fmt.Errorf("failed to get essential packages list: %w", err)
	}
	pkgListStr := strings.Join(essentialPkgsList, ",")
	localRepoConfigChrootPath := "/etc/apt/sources.list.d/local.list"
	localRepoConfigHostPath, err := imageOs.chrootEnv.GetChrootEnvHostPath(localRepoConfigChrootPath)
	if err != nil {
		return fmt.Errorf("failed to get chroot environment host path for %s: %w", localRepoConfigChrootPath, err)
	}

	if _, err := os.Stat(localRepoConfigHostPath); os.IsNotExist(err) {
		log.Errorf("Local repository config file does not exist: %s", localRepoConfigHostPath)
		return fmt.Errorf("local repository config file does not exist: %s", localRepoConfigHostPath)
	}

	chrootInstallRoot, err := imageOs.chrootEnv.GetChrootEnvPath(installRoot)
	if err != nil {
		return fmt.Errorf("failed to get chroot environment path for install root %s: %w", installRoot, err)
	}

	cmd := fmt.Sprintf("mmdebstrap "+
		"--variant=custom "+
		"--format=directory "+
		"--aptopt=APT::Authentication::Trusted=true "+
		"--hook-dir=/usr/share/mmdebstrap/hooks/file-mirror-automount "+
		"--include=%s "+
		"--verbose --debug "+
		"-- bookworm %s %s",
		pkgListStr, chrootInstallRoot, localRepoConfigChrootPath)

	chrootEnvRoot := imageOs.chrootEnv.GetChrootEnvRoot()
	if _, err = shell.ExecCmdWithStream(cmd, true, chrootEnvRoot, nil); err != nil {
		log.Errorf("Failed to install essential packages into image: %v", err)
		return fmt.Errorf("failed to install packages into image: %w", err)
	}
	return nil
}

func (imageOs *ImageOs) mountSysfsToRootfs(installRoot string) error {
	chrootInstallRoot, err := imageOs.chrootEnv.GetChrootEnvPath(installRoot)
	if err != nil {
		return fmt.Errorf("failed to get chroot environment path: %w", err)
	}
	if err = imageOs.chrootEnv.MountChrootSysfs(chrootInstallRoot); err != nil {
		return fmt.Errorf("failed to mount sysfs into image rootfs %s: %w", chrootInstallRoot, err)
	}
	return nil
}

func (imageOs *ImageOs) umountSysfsFromRootfs(installRoot string) error {
	chrootInstallRoot, err := imageOs.chrootEnv.GetChrootEnvPath(installRoot)
	if err != nil {
		return fmt.Errorf("failed to get chroot environment path: %w", err)
	}
	if err := imageOs.chrootEnv.UmountChrootSysfs(chrootInstallRoot); err != nil {
		return fmt.Errorf("failed to unmount sysfs for image rootfs: %w", err)
	}
	return nil
}

func mountDiskRootToChroot(installRoot string, diskPathIdMap map[string]string, template *config.ImageTemplate) error {
	diskInfo := template.GetDiskConfig()
	partions := diskInfo.Partitions
	for diskId, diskPath := range diskPathIdMap {
		for _, partition := range partions {
			if partition.ID == diskId {
				if partition.MountPoint == "/" {
					mountPoint := filepath.Join(installRoot, partition.MountPoint)
					mountFlags := fmt.Sprintf("-t %s", partition.FsType)
					if err := mount.MountPath(diskPath, mountPoint, mountFlags); err != nil {
						log.Errorf("Failed to mount %s to %s: %v", diskPath, mountPoint, err)
						return fmt.Errorf("failed to mount %s to %s: %w", diskPath, mountPoint, err)
					}
					return nil
				}
			}
		}
	}
	log.Errorf("No root partition found in diskPathIdMap")
	return fmt.Errorf("no root partition found in diskPathIdMap")
}

func (imageOs *ImageOs) mountDiskToChroot(installRoot string, diskPathIdMap map[string]string, template *config.ImageTemplate) ([]map[string]string, error) {
	var mountPointInfoList []map[string]string
	diskInfo := template.GetDiskConfig()
	partions := diskInfo.Partitions
	for diskId, diskPath := range diskPathIdMap {
		for _, partition := range partions {
			if partition.ID == diskId {
				mountPointInfo := make(map[string]string)
				mountPointInfo["Id"] = diskId
				mountPointInfo["Path"] = diskPath
				mountPointInfo["MountPoint"] = filepath.Join(installRoot, partition.MountPoint)
				if partition.MountPoint == "/boot/efi" {
					if partition.FsType == "fat32" || partition.FsType == "fat16" {
						mountPointInfo["Flags"] = fmt.Sprintf("-t %s -o umask=0077", "vfat")
					} else {
						mountPointInfo["Flags"] = fmt.Sprintf("-t %s -o umask=0077", partition.FsType)
					}
				} else {
					mountPointInfo["Flags"] = fmt.Sprintf("-t %s", partition.FsType)
				}
				mountPointInfoList = append(mountPointInfoList, mountPointInfo)
			}
		}
	}

	if len(mountPointInfoList) == 0 {
		log.Errorf("No mount points found for the provided diskPathIdMap")
		return nil, fmt.Errorf("no mount points found for the provided diskPathIdMap")
	}

	// sort the mountPointInfoList by the partition.MountPoint
	// mount requires order that the "/" mounted first, then "/boot", "/boot/efi", etc.
	sort.Slice(mountPointInfoList, func(i, j int) bool {
		return mountPointInfoList[i]["MountPoint"] < mountPointInfoList[j]["MountPoint"]
	})

	for _, mountPointInfo := range mountPointInfoList {
		mountPoint := mountPointInfo["MountPoint"]
		path := mountPointInfo["Path"]
		flags := mountPointInfo["Flags"]
		if err := mount.MountPath(path, mountPoint, flags); err != nil {
			log.Errorf("Failed to mount %s to %s with flags %s: %v", path, mountPoint, flags, err)
			return nil, fmt.Errorf("failed to mount %s to %s with flags %s: %w", path, mountPoint, flags, err)
		}
	}

	if err := imageOs.mountSysfsToRootfs(installRoot); err != nil {
		return nil, err
	}

	return mountPointInfoList, nil
}

func (imageOs *ImageOs) umountDiskFromChroot(installRoot string, mountPointInfoList []map[string]string) error {
	if err := imageOs.umountSysfsFromRootfs(installRoot); err != nil {
		return err
	}

	mountPointInfoListLen := len(mountPointInfoList)
	for i := mountPointInfoListLen - 1; i >= 0; i-- {
		mountPointInfo := mountPointInfoList[i]
		mountPoint := mountPointInfo["MountPoint"]
		if err := mount.UmountPath(mountPoint); err != nil {
			log.Errorf("Failed to unmount %s: %v", mountPoint, err)
			return fmt.Errorf("failed to unmount %s: %w", mountPoint, err)
		}
	}
	return nil
}

func getRpmPkgInstallList(template *config.ImageTemplate) []string {
	var head, middle, tail []string
	imagePkgList := template.GetPackages()
	for _, pkg := range imagePkgList {
		if strings.HasPrefix(pkg, "filesystem") {
			head = append(head, pkg)
		} else if strings.HasPrefix(pkg, "initramfs") {
			tail = append(tail, pkg)
		} else {
			middle = append(middle, pkg)
		}
	}
	return append(append(head, middle...), tail...)
}

func getDebPkgInstallList(template *config.ImageTemplate) []string {
	var head, middle, tail []string
	var imagePkgList []string
	// Exclude the template.EssentialPkgList as it is already installed by mmdebstrap
	imagePkgList = append(imagePkgList, template.KernelPkgList...)
	imagePkgList = append(imagePkgList, template.SystemConfig.Packages...)
	imagePkgList = append(imagePkgList, template.BootloaderPkgList...)

	for _, pkg := range imagePkgList {
		if strings.HasPrefix(pkg, "base-files") {
			head = append(head, pkg)
		} else if strings.HasPrefix(pkg, "dracut") {
			tail = append(tail, pkg)
		} else if strings.HasPrefix(pkg, "systemd-boot") {
			tail = append(tail, pkg)
		} else {
			middle = append(middle, pkg)
		}
	}
	return append(append(head, middle...), tail...)
}

func (imageOs *ImageOs) initImageRpmDb(installRoot string, template *config.ImageTemplate) error {
	log.Infof("Initializing RPM database in %s", installRoot)
	rpmDbPath := filepath.Join(installRoot, "var", "lib", "rpm")
	if _, err := os.Stat(rpmDbPath); os.IsNotExist(err) {
		if _, err := shell.ExecCmd("mkdir -p "+rpmDbPath, true, shell.HostPath, nil); err != nil {
			log.Errorf("Failed to create RPM database directory %s: %v", rpmDbPath, err)
			return fmt.Errorf("failed to create RPM database directory: %w", err)
		}
	}
	chrootInstallRoot, err := imageOs.chrootEnv.GetChrootEnvPath(installRoot)
	if err != nil {
		return fmt.Errorf("failed to get chroot environment path: %w", err)
	}
	cmd := fmt.Sprintf("rpm --root %s --initdb", chrootInstallRoot)
	chrootEnvRoot := imageOs.chrootEnv.GetChrootEnvRoot()
	if _, err := shell.ExecCmd(cmd, true, chrootEnvRoot, nil); err != nil {
		log.Errorf("Failed to initialize RPM database in %s: %v", chrootInstallRoot, err)
		return fmt.Errorf("failed to initialize RPM database: %w", err)
	}
	return nil
}

func (imageOs *ImageOs) initDebLocalRepoWithinInstallRoot(installRoot string) error {
	chrootInstallRoot, err := imageOs.chrootEnv.GetChrootEnvPath(installRoot)
	if err != nil {
		return fmt.Errorf("failed to get chroot environment path for install root %s: %w", installRoot, err)
	}

	// from local.list
	repoPath := filepath.Join(chrootInstallRoot, "/cdrom/cache-repo")
	chrootPkgCacheDir := imageOs.chrootEnv.GetChrootPkgCacheDir()
	if err := imageOs.chrootEnv.MountChrootPath(chrootPkgCacheDir, repoPath, "--bind"); err != nil {
		return fmt.Errorf("failed to mount package cache directory %s to chroot repo directory %s: %w",
			chrootPkgCacheDir, repoPath, err)
	}

	imageRepoCongfigPath := filepath.Join(installRoot, "/etc/apt/sources.list.d/", "*")
	if _, err := shell.ExecCmd("rm -f "+imageRepoCongfigPath, true, shell.HostPath, nil); err != nil {
		log.Errorf("Failed to remove existing local repo config files: %v", err)
		return fmt.Errorf("failed to remove existing local repo config files: %w", err)
	}

	repoCongfigPath := filepath.Join(imageOs.chrootEnv.GetTargetOsConfigDir(), "chrootenvconfigs", "local.list")
	if _, err := os.Stat(repoCongfigPath); os.IsNotExist(err) {
		log.Errorf("Repo config file does not exist: %s", repoCongfigPath)
		return fmt.Errorf("repo config file does not exist: %s", repoCongfigPath)
	}

	targetPath := filepath.Join(chrootInstallRoot, "/etc/apt/sources.list.d/")
	if err := imageOs.chrootEnv.CopyFileFromHostToChroot(repoCongfigPath, targetPath); err != nil {
		return fmt.Errorf("failed to copy local repository config file to chroot: %w", err)
	}

	cmd := "apt-get update"
	if _, err := shell.ExecCmdWithStream(cmd, true, installRoot, nil); err != nil {
		log.Errorf("Failed to refresh cache for chroot repository: %v", err)
		return fmt.Errorf("failed to refresh cache for chroot repository: %w", err)
	}

	// Create a policy-rc.d file to prevent service startup in chroot
	policyFile := filepath.Join(installRoot, "/usr/sbin/policy-rc.d")
	policyContent := "#!/bin/sh\nexit 101\n"

	if _, err := shell.ExecCmd("mkdir -p "+filepath.Dir(policyFile), true, shell.HostPath, nil); err != nil {
		log.Errorf("Failed to create policy-rc.d directory: %v", err)
		return fmt.Errorf("failed to create policy-rc.d directory: %w", err)
	}

	if err := file.Write(policyContent, policyFile); err != nil {
		log.Errorf("Failed to create policy-rc.d file %s: %v", policyFile, err)
		return fmt.Errorf("failed to create policy-rc.d file: %w", err)
	}

	return nil
}

func (imageOs *ImageOs) deInitDebLocalRepoWithinInstallRoot(installRoot string) error {
	// from local.list
	repoPath := filepath.Join(installRoot, "/cdrom/cache-repo")
	if err := imageOs.chrootEnv.UmountChrootPath(repoPath); err != nil {
		return fmt.Errorf("failed to unmount chroot repo directory %s: %w", repoPath, err)
	}

	repoconfigPath := filepath.Join(installRoot, "/etc/apt/sources.list.d/local.list")
	if _, err := os.Stat(repoconfigPath); err == nil {
		if _, err := shell.ExecCmd("rm -f "+repoconfigPath, true, shell.HostPath, nil); err != nil {
			log.Errorf("Failed to remove local repository config file %s: %v", repoconfigPath, err)
			return fmt.Errorf("failed to remove local repository config file %s: %w", repoconfigPath, err)
		}
	}

	policyFile := filepath.Join(installRoot, "/usr/sbin/policy-rc.d")
	if _, err := os.Stat(policyFile); err == nil {
		if _, err := shell.ExecCmd("rm -f "+policyFile, true, shell.HostPath, nil); err != nil {
			log.Errorf("Failed to remove policy-rc.d file %s: %v", policyFile, err)
			return fmt.Errorf("failed to remove policy-rc.d file %s: %w", policyFile, err)
		}
	}
	return nil
}

func preImageOsInstall(installRoot string, template *config.ImageTemplate) error {
	return nil
}

func (imageOs *ImageOs) installImagePkgs(installRoot string, template *config.ImageTemplate) error {
	pkgType := imageOs.chrootEnv.GetTargetOsPkgType()

	if pkgType == "rpm" {
		if err := imageOs.initImageRpmDb(installRoot, template); err != nil {
			return fmt.Errorf("failed to initialize RPM database: %w", err)
		}
		imagePkgOrderedList := getRpmPkgInstallList(template)
		imagePkgNum := len(imagePkgOrderedList)
		// Force to use the local cache repository
		var repositoryIDList []string = []string{"cache-repo"}
		for i, pkg := range imagePkgOrderedList {
			log.Infof("Installing package %d/%d: %s", i+1, imagePkgNum, pkg)
			if err := imageOs.chrootEnv.TdnfInstallPackage(pkg, installRoot, repositoryIDList); err != nil {
				return fmt.Errorf("failed to install package %s: %w", pkg, err)
			}
		}
	} else if pkgType == "deb" {
		imagePkgOrderedList := getDebPkgInstallList(template)
		// Prepare local cache repository
		if err := imageOs.initDebLocalRepoWithinInstallRoot(installRoot); err != nil {
			return fmt.Errorf("failed to initialize local repository within install root: %w", err)
		}
		imagePkgNum := len(imagePkgOrderedList)
		// Force to use the local cache repository
		var repoSrcList []string = []string{"/etc/apt/sources.list.d/local.list"}
		var efiVariableAccessPkg = []string{"systemd-boot", "dracut-core"}
		for i, pkg := range imagePkgOrderedList {
			log.Infof("Installing package %d/%d: %s", i+1, imagePkgNum, pkg)
			if slice.Contains(efiVariableAccessPkg, pkg) {
				// systemd-boot and dracut-core are special cases,
				// 'Failed to write 'LoaderSystemToken' EFI variable: No such file or directory' error is expected.
				installCmd := fmt.Sprintf("apt-get install -y %s", pkg)

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
				// Always log the full output for debugging
				log.Infof("apt-get install output for %s:\n%s", pkg, output)
				if err != nil {
					if strings.Contains(output, "Failed to write 'LoaderSystemToken' EFI variable") ||
						strings.Contains(output, "Failed to create EFI Boot variable entry") {
						log.Debugf("Expected error: EFI variables cannot be accessed in chroot environment.")
					} else {
						log.Errorf("Failed to install package %s: %v", pkg, err)
						log.Errorf("Full apt-get output:\n%s", output)
						return fmt.Errorf("failed to install package %s: %w", pkg, err)
					}
				}
			} else {
				if err := imageOs.chrootEnv.AptInstallPackage(pkg, installRoot, repoSrcList); err != nil {
					return fmt.Errorf("failed to install package %s: %w", pkg, err)
				}

				// After apparmor is installed, create a wrapper to prevent postinst failures in chroot
				pkgNameOnly := strings.Split(pkg, "_")[0]
				if pkgNameOnly == "apparmor" {
					// Create a wrapper script for apparmor_parser that always succeeds
					apparmorOrigPath := filepath.Join(installRoot, "usr/sbin/apparmor_parser")
					apparmorRealPath := filepath.Join(installRoot, "usr/sbin/apparmor_parser.real")

					// Check if apparmor_parser exists
					if _, err := os.Stat(apparmorOrigPath); err == nil {
						// Rename the real apparmor_parser
						if err := os.Rename(apparmorOrigPath, apparmorRealPath); err != nil {
							log.Warnf("Failed to rename apparmor_parser: %v", err)
						} else {
							// Create a wrapper that calls the real parser but always returns success
							wrapperScript := `#!/bin/bash
# Wrapper for apparmor_parser in chroot environment
# Calls the real parser but ignores errors since AppArmor kernel interface is not available
/usr/sbin/apparmor_parser.real "$@" 2>&1 | grep -v "Cache read/write disabled" | grep -v "Kernel needs AppArmor" | grep -v "interface file missing" || true
exit 0
`
							if err := os.WriteFile(apparmorOrigPath, []byte(wrapperScript), 0755); err != nil {
								log.Warnf("Failed to create apparmor_parser wrapper: %v", err)
							} else {
								log.Debugf("Created apparmor_parser wrapper at %s", apparmorOrigPath)
							}
						}
					} else {
						log.Warnf("apparmor_parser not found at %s", apparmorOrigPath)
					}
				}
			}
		}
		if err := imageOs.deInitDebLocalRepoWithinInstallRoot(installRoot); err != nil {
			return fmt.Errorf("failed to de-initialize local repository within install root: %w", err)
		}

		// Restore original apparmor_parser after all packages are installed
		apparmorRealPath := filepath.Join(installRoot, "usr/sbin/apparmor_parser.real")
		if _, statErr := os.Stat(apparmorRealPath); statErr == nil {
			apparmorOrigPath := filepath.Join(installRoot, "usr/sbin/apparmor_parser")
			// Remove the wrapper
			if err := os.Remove(apparmorOrigPath); err != nil {
				log.Warnf("Failed to remove apparmor_parser wrapper: %v", err)
			}
			// Restore the original
			if err := os.Rename(apparmorRealPath, apparmorOrigPath); err != nil {
				log.Warnf("Failed to restore original apparmor_parser: %v", err)
			} else {
				log.Debugf("Restored original apparmor_parser after package installation")
			}
		}
	} else {
		return fmt.Errorf("unsupported package type: %s", pkgType)
	}
	return nil
}

func updateInitrdConfig(installRoot string, template *config.ImageTemplate) error {
	if err := updateImageHostname(installRoot, template); err != nil {
		return fmt.Errorf("failed to update image hostname: %w", err)
	}
	if err := addImageAdditionalFiles(installRoot, template); err != nil {
		return fmt.Errorf("failed to add additional files to image: %w", err)
	}
	if err := updateImageUsrGroup(installRoot, template); err != nil {
		return fmt.Errorf("failed to update image user/group: %w", err)
	}
	if err := updateImageNetwork(installRoot, template); err != nil {
		return fmt.Errorf("failed to update image network: %w", err)
	}
	if err := addImageIDFile(installRoot, template); err != nil {
		return fmt.Errorf("failed to add image ID file: %w", err)
	}
	if err := createResolvConfSymlink(installRoot, template); err != nil {
		return fmt.Errorf("failed to create resolv.conf: %w", err)
	}
	if err := addImageConfigs(installRoot, template); err != nil {
		return fmt.Errorf("failed to execute customized configurations to image: %w", err)
	}
	return nil
}

func updateImageConfig(installRoot string, diskPathIdMap map[string]string, template *config.ImageTemplate) error {
	if err := updateImageHostname(installRoot, template); err != nil {
		return fmt.Errorf("failed to update image hostname: %w", err)
	}
	if err := addImageAdditionalFiles(installRoot, template); err != nil {
		return fmt.Errorf("failed to add additional files to image: %w", err)
	}
	if err := updateImageUsrGroup(installRoot, template); err != nil {
		return fmt.Errorf("failed to update image user/group: %w", err)
	}
	if err := updateImageNetwork(installRoot, template); err != nil {
		return fmt.Errorf("failed to update image network: %w", err)
	}
	if err := addImageIDFile(installRoot, template); err != nil {
		return fmt.Errorf("failed to add image ID file: %w", err)
	}
	if err := updateImageFstab(installRoot, diskPathIdMap, template); err != nil {
		return fmt.Errorf("failed to update image fstab: %w", err)
	}
	if err := createResolvConfSymlink(installRoot, template); err != nil {
		return fmt.Errorf("failed to create resolv.conf: %w", err)
	}
	if err := addImageConfigs(installRoot, template); err != nil {
		return fmt.Errorf("failed to execute customized configurations to image: %w", err)
	}
	return nil
}

func (imageOs *ImageOs) getImageVersionInfo(installRoot string, template *config.ImageTemplate) (string, error) {
	var versionInfo string
	log.Infof("Getting image version info for: %s", template.GetImageName())

	switch template.Target.OS {
	case "azure-linux", "edge-microvisor-toolkit":
		imageVersionFilePath := filepath.Join(installRoot, "etc", "os-release")
		if _, err := os.Stat(imageVersionFilePath); os.IsNotExist(err) {
			log.Errorf("os-release file does not exist: %s", imageVersionFilePath)
			return "", fmt.Errorf("os-release file does not exist: %s", imageVersionFilePath)
		}
		content, err := file.Read(imageVersionFilePath)
		if err != nil {
			log.Errorf("Failed to read image version file %s: %v", imageVersionFilePath, err)
			return "", fmt.Errorf("failed to read image version file %s: %w", imageVersionFilePath, err)
		}
		// Parse the content to extract version information
		lines := strings.Split(content, "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "VERSION=") {
				// Remove prefix, quotes and trim whitespace
				value := strings.TrimPrefix(line, "VERSION=")
				versionInfo = strings.TrimSpace(strings.Trim(value, "\""))
				break
			}
		}
		if versionInfo == "" {
			log.Debugf("Version info not found in %s", imageVersionFilePath)
		}
	default:
		versionInfo = imageOs.chrootEnv.GetTargetOsReleaseVersion()
	}

	log.Infof("Extracted version info: %s", versionInfo)

	return versionInfo, nil
}

func (imageOs *ImageOs) postImageOsInstall(installRoot string, template *config.ImageTemplate) (string, error) {
	versionInfo, err := imageOs.getImageVersionInfo(installRoot, template)
	if err != nil {
		return versionInfo, fmt.Errorf("failed to get image version info: %w", err)
	}
	return versionInfo, nil
}

func updateImageHostname(installRoot string, template *config.ImageTemplate) error {
	hostname := template.SystemConfig.HostName
	if hostname != "" {
		log.Infof("Configuring Hostname...")
		hostnameFilePath := filepath.Join(installRoot, "etc", "hostname")
		if err := file.Write(hostname+"\n", hostnameFilePath); err != nil {
			return fmt.Errorf("failed to write hostname to %s: %w", hostnameFilePath, err)
		}
		if _, err := shell.ExecCmd("chmod 0644 "+hostnameFilePath, true, shell.HostPath, nil); err != nil {
			log.Errorf("Failed to set permissions for hostname file %s: %v", hostnameFilePath, err)
			return fmt.Errorf("failed to set permissions for hostname file %s: %w", hostnameFilePath, err)
		}
	}
	return nil
}

func updateImageUsrGroup(installRoot string, template *config.ImageTemplate) error {
	log.Infof("Configuring User...")
	if err := createUser(installRoot, template); err != nil {
		return fmt.Errorf("failed to configuring User: %w", err)
	}
	return nil
}

func updateImageNetwork(installRoot string, template *config.ImageTemplate) error {
	unitFilePath := filepath.Join(installRoot, "lib", "systemd", "system", "systemd-networkd.service")
	if _, err := os.Stat(unitFilePath); os.IsNotExist(err) {
		log.Warnf("systemd-networkd is not installed in %s, skipping enable", installRoot)
		return nil
	}
	cmd := "systemctl enable --root=\"" + installRoot + "\" systemd-networkd"
	if _, err := shell.ExecCmd(cmd, true, shell.HostPath, nil); err != nil {
		return fmt.Errorf("failed to enable systemd-networkd: %w", err)
	}
	return nil
}

func addImageIDFile(installRoot string, template *config.ImageTemplate) error {
	log.Infof("Adding image ID file for image: %s", template.GetImageName())
	imageIDFilePath := filepath.Join(installRoot, "etc", "image-id")
	// Get the current time in UTC and in format "YYYYMMDDHHMMSS"
	imageBuildDate := time.Now().UTC().Format("20060102150405")
	imageIDContent := fmt.Sprintf("IMAGE_BUILD_DATE=%s\nIMAGE_UUID=%s\n", imageBuildDate, uuid.New().String())
	if err := file.Write(imageIDContent, imageIDFilePath); err != nil {
		log.Errorf("Failed to write file %s: %v", imageIDFilePath, err)
		return fmt.Errorf("failed to write file %s: %w", imageIDFilePath, err)
	}
	if _, err := shell.ExecCmd("chmod 0444 "+imageIDFilePath, true, shell.HostPath, nil); err != nil {
		log.Errorf("Failed to set permissions for image ID file %s: %v", imageIDFilePath, err)
		return fmt.Errorf("failed to set permissions for image ID file %s: %w", imageIDFilePath, err)
	}
	return nil
}

func addImageAdditionalFiles(installRoot string, template *config.ImageTemplate) error {
	log.Infof("Adding additional files to image: %s", template.GetImageName())
	additionalFiles := template.GetAdditionalFileInfo()
	if len(additionalFiles) == 0 {
		log.Debug("No additional files to add to the image")
		return nil
	}

	for _, fileInfo := range additionalFiles {
		srcFile := fileInfo.Local
		dstFile := filepath.Join(installRoot, fileInfo.Final)
		if err := file.CopyFile(srcFile, dstFile, "-p", true); err != nil {
			log.Errorf("Failed to copy additional file %s to image: %v", srcFile, err)
			return fmt.Errorf("failed to copy additional file %s to image: %w", srcFile, err)
		}
		log.Debugf("Successfully added additional file: %s", dstFile)
	}
	return nil
}
func addImageConfigs(installRoot string, template *config.ImageTemplate) error {
	customConfigs := template.GetConfigurationInfo()
	if len(customConfigs) == 0 {
		log.Debug("No custom configurations to add to the image")
		return nil
	}

	for _, configInfo := range customConfigs {
		cmdStr := configInfo.Cmd
		// Use chroot to execute commands in the image context with proper shell
		chrootCmd := fmt.Sprintf("chroot %s /bin/bash -c %s", installRoot, strconv.Quote(cmdStr))
		if _, err := shell.ExecCmd(chrootCmd, true, shell.HostPath, nil); err != nil {
			log.Errorf("Failed to execute custom configuration cmd %s: %v", configInfo.Cmd, err)
			return fmt.Errorf("failed to execute custom configuration cmd %s: %w", configInfo.Cmd, err)
		}
		log.Debugf("Successfully executed custom configuration cmd: %s", configInfo.Cmd)
	}

	return nil
}

func updateImageFstab(installRoot string, diskPathIdMap map[string]string, template *config.ImageTemplate) error {
	const (
		rootfsMountPoint = "/"
		defaultOptions   = "defaults"
		swapFsType       = "swap"
		swapOptions      = "sw"
		defaultDump      = "0"
		disablePass      = "0"
		rootPass         = "1"
		defaultPass      = "2"
	)
	log.Infof("Updating fstab for image: %s", template.GetImageName())
	fstabFullPath := filepath.Join(installRoot, "etc", "fstab")
	diskInfo := template.GetDiskConfig()
	partitions := diskInfo.Partitions
	for diskId, diskPath := range diskPathIdMap {
		for _, partition := range partitions {
			if partition.ID == diskId {
				// Get the partition UUID and mount point
				partUUID, err := imagedisc.GetPartUUID(diskPath)
				if err != nil {
					return fmt.Errorf("failed to get partition UUID for %s: %w", diskPath, err)
				}
				mountId := fmt.Sprintf("PARTUUID=%s", partUUID)
				mountPoint := partition.MountPoint

				// Get the filesystem type
				var fsType, options, pass string
				if partition.FsType == "fat16" || partition.FsType == "fat32" {
					fsType = "vfat"
				} else {
					fsType = partition.FsType
				}

				// Get the mount options
				options = defaultOptions
				if partition.MountOptions != "" {
					options = partition.MountOptions
				}

				// Get the default dump and pass values
				pass = defaultPass
				if mountPoint == rootfsMountPoint {
					pass = rootPass
				}

				if fsType == swapFsType {
					// For swap partitions, set the options accordingly
					options = swapOptions
					pass = disablePass // No pass value for swap
				}

				newEntry := fmt.Sprintf("%v %v %v %v %v %v\n",
					mountId, mountPoint, fsType, options, defaultDump, pass)
				log.Debugf("Adding fstab entry: %s", newEntry)
				err = file.Append(newEntry, fstabFullPath)
				if err != nil {
					log.Errorf("Failed to append fstab entry for %s: %v", mountPoint, err)
					return fmt.Errorf("failed to append fstab entry for %s: %w", mountPoint, err)
				}
			}
		}
	}
	return nil
}

func createResolvConfSymlink(installRoot string, template *config.ImageTemplate) error {
	log.Infof("Creating resolv.conf for image: %s", template.GetImageName())
	resolveConfPath := "/etc/resolv.conf"
	resolveConfFullPath := filepath.Join(installRoot, resolveConfPath)
	if _, err := os.Stat(resolveConfFullPath); os.IsNotExist(err) {
		stubResolveConfPath := "/run/systemd/resolve/stub-resolv.conf"
		cmdStr := fmt.Sprintf("ln -sf %s %s", stubResolveConfPath, resolveConfPath)
		if _, err := shell.ExecCmd(cmdStr, true, installRoot, nil); err != nil {
			log.Errorf("Failed to create symlink for resolv.conf: %v", err)
			return fmt.Errorf("failed to create symlink for resolv.conf: %w", err)
		}
		log.Debugf("Created symlink for resolv.conf to %s", stubResolveConfPath)
	} else {
		log.Debug("resolv.conf already exists, skipping creation")
	}
	return nil
}

func buildImageUKI(installRoot string, template *config.ImageTemplate) error {
	bootloaderConfig := template.GetBootloaderConfig()
	if bootloaderConfig.Provider == "systemd-boot" {
		// 1. Update initramfs
		kernelVersion, err := getKernelVersion(installRoot)
		if err != nil {
			return fmt.Errorf("failed to get kernel version: %w", err)
		}

		log.Debugf("Kernel version:%s", kernelVersion)

		if err := updateInitramfs(installRoot, kernelVersion, template); err != nil {
			return fmt.Errorf("failed to update initramfs: %w", err)
		}

		log.Debug("Initramfs updated successfully")

		// 2. Build UKI with ukify
		kernelPath := filepath.Join("/boot", "vmlinuz-"+kernelVersion)
		initrdPath := fmt.Sprintf("/boot/initramfs-%s.img", kernelVersion)

		espRoot := installRoot
		espDir, err := prepareESPDir(espRoot)
		if err != nil {
			return fmt.Errorf("failed to prepare ESP directory: %w", err)
		}
		log.Debugf("Succesfully Creating EspPath:", espDir)

		outputPath := filepath.Join(espDir, "EFI", "Linux", "linux.efi")
		log.Debugf("UKI Path:", outputPath)

		cmdlineFile := filepath.Join("/boot", "cmdline.conf")

		// do checks for file paths
		if _, err := os.Stat(installRoot); err == nil {
			log.Infof("Install Root Exists at %s", installRoot)
		} else {
			log.Errorf("Install Root does not exist at %s", installRoot)
		}
		if _, err := os.Stat(kernelPath); err == nil {
			log.Infof("kernelPath  Exists at %s", kernelPath)
		} else {
			log.Errorf("Install Root does not exist at %s", installRoot)
		}

		if _, err := os.Stat(kernelPath); err == nil {
			log.Infof("kernelPath  Exists at %s", kernelPath)
		} else {
			log.Errorf("kernelPath does not exist at %s", kernelPath)
		}

		if _, err := os.Stat(initrdPath); err == nil {
			log.Infof("initrdPath  Exists at %s", initrdPath)
		} else {
			log.Errorf("initrdPath does not exist at %s", initrdPath)
		}
		if _, err := os.Stat(cmdlineFile); err == nil {
			log.Infof("cmdlineFile  Exists at %s", cmdlineFile)
			return nil
		} else {
			log.Errorf("cmdlineFile does not exist at %s", cmdlineFile)
		}
		if _, err := os.Stat(outputPath); err == nil {
			log.Infof("outputPath  Exists at %s", outputPath)
			return nil
		} else {
			log.Errorf("outputPath does not exist at %s", outputPath)
		}

		if err := buildUKI(installRoot, kernelPath, initrdPath, cmdlineFile, outputPath, template); err != nil {
			return fmt.Errorf("failed to build UKI: %w", err)
		}
		log.Debugf("UKI created successfully on:", outputPath)
		log.Infof("Target architecture is %v ", template.Target.Arch)

		srcBootloader := ""
		dstBootloader := ""

		switch template.Target.Arch {
		case "x86_64":
			log.Infof("Target architecture is x86_64, proceeding with bootloader copy")
			// 3. Copy systemd-bootx64.efi to ESP/EFI/BOOT/BOOTX64.EFI
			srcBootloader = filepath.Join("usr", "lib", "systemd", "boot", "efi", "systemd-bootx64.efi")
			dstBootloader = filepath.Join(espDir, "EFI", "BOOT", "BOOTX64.EFI")
		case "aarch64":
			log.Infof("Target architecture is ARM64, proceeding with bootloader copy")
			// 3. Copy systemd-bootx64.efi to ESP/EFI/BOOT/BOOT64.EFI
			srcBootloader = filepath.Join("usr", "lib", "systemd", "boot", "efi", "systemd-bootaa64.efi")
			dstBootloader = filepath.Join(espDir, "EFI", "BOOT", "BOOTAA64.EFI")
		default:
			log.Infof("Skipping bootloader copy for architecture: %s", template.Target.Arch)
			return nil
		}

		if err := copyBootloader(installRoot, srcBootloader, dstBootloader); err != nil {
			return fmt.Errorf("failed to copy bootloader: %w", err)
		}
		log.Debugf("Bootloader copied successfully to:", dstBootloader)
	} else {
		log.Infof("Skipping UKI build for image: %s, bootloader provider is not systemd-boot", template.GetImageName())
	}

	return nil
}

// Helper to get the current kernel version from the rootfs
func getKernelVersion(installRoot string) (string, error) {
	kernelDir := filepath.Join(installRoot, "boot")
	fileList, err := file.GetFileList(kernelDir)
	if err != nil {
		log.Errorf("Failed to list kernel directory %s: %v", kernelDir, err)
		return "", fmt.Errorf("failed to list kernel directory %s: %w", kernelDir, err)
	}
	for _, f := range fileList {
		if strings.HasPrefix(f, "vmlinuz-") {
			return strings.TrimPrefix(f, "vmlinuz-"), nil
		}
	}
	log.Errorf("Kernel image not found in %s", kernelDir)
	return "", fmt.Errorf("kernel image not found in %s", kernelDir)
}

// Helper to update initramfs for the given kernel version
func updateInitramfs(installRoot, kernelVersion string, template *config.ImageTemplate) error {
	// Other distributions use initramfs- prefix
	initrdPath := fmt.Sprintf("/boot/initramfs-%s.img", kernelVersion)

	// Build dracut command with all required options
	var cmdParts []string
	cmdParts = append(cmdParts, "dracut")
	cmdParts = append(cmdParts, "--force")
	cmdParts = append(cmdParts, "--no-hostonly")
	cmdParts = append(cmdParts, "--verbose")

	// Add systemd-veritysetup module if immutability is enabled
	if template.IsImmutabilityEnabled() {
		cmdParts = append(cmdParts, "--add", "systemd-veritysetup")
		cmdParts = append(cmdParts, "--add", "dm")
		cmdParts = append(cmdParts, "--add", "crypt")
	}

	// Add cut utility for EMT images only
	if template.Target.OS == "edge-microvisor-toolkit" {
		log.Debugf("Adding /usr/bin/cut to initramfs for EMT image")
		cmdParts = append(cmdParts, "--install", "/usr/bin/cut")
	} else {
		log.Debugf("Skipping /usr/bin/cut for non-EMT image (OS: %s)", template.Target.OS)
	}
	cmdParts = append(cmdParts, "--add", "systemd")

	// Always add USB drivers
	extraModules := strings.TrimSpace(template.SystemConfig.Kernel.EnableExtraModules)
	if extraModules != "" {
		cmdParts = append(cmdParts, fmt.Sprintf("--add-drivers '%s'", extraModules))
	}

	// Add kernel version and output path
	cmdParts = append(cmdParts, "--kver", kernelVersion)
	cmdParts = append(cmdParts, initrdPath)

	// Execute single dracut command
	cmd := strings.Join(cmdParts, " ")
	_, err := shell.ExecCmd(cmd, true, installRoot, nil)
	if err != nil {
		if template.IsImmutabilityEnabled() {
			log.Errorf("Failed to update initramfs with veritysetup and USB drivers: %v", err)
			return fmt.Errorf("failed to update initramfs with veritysetup and USB drivers: %w", err)
		} else {
			log.Errorf("Failed to update initramfs with USB drivers: %v", err)
			return fmt.Errorf("failed to update initramfs with USB drivers: %w", err)
		}
	}

	if template.IsImmutabilityEnabled() {
		log.Debugf("Initramfs updated successfully with veritysetup and USB drivers")
	} else {
		log.Debugf("Initramfs updated successfully with USB drivers")
	}

	return nil
}

// Helper to determine the ESP directory (assumes /boot/efi)
func prepareESPDir(installRoot string) (string, error) {
	espDirs := []string{
		"/boot/efi",
		"/boot/efi/EFI/Linux",
		"/boot/efi/EFI/BOOT",
	}

	// remove previous bootloader
	cleanupDirs := []string{
		"/boot/efi/*",
	}

	// Remove all from efi directories
	for _, dir := range cleanupDirs {
		cmd := fmt.Sprintf("sh -c 'rm -rf %s'", dir)
		if _, err := shell.ExecCmd(cmd, true, installRoot, nil); err != nil {
			log.Errorf("Failed to clean up ESP directory %s: %v", dir, err)
			return "", fmt.Errorf("failed to clean up ESP directory %s: %w", dir, err)
		}
	}

	// Create required ESP directories
	for _, dir := range espDirs {
		cmd := fmt.Sprintf("mkdir -p %s", dir)
		if _, err := shell.ExecCmd(cmd, true, installRoot, nil); err != nil {
			log.Errorf("Failed to create ESP directory %s: %v", dir, err)
			return "", fmt.Errorf("failed to create ESP directory %s: %w", dir, err)
		}
	}

	// Return the ESP root directory
	return espDirs[0], nil
}

func extractRootHashPH(input string) string {
	parts := strings.Fields(input)
	for _, part := range parts {
		if strings.HasPrefix(part, "roothash=") {
			val := strings.TrimPrefix(part, "roothash=")
			val = strings.ReplaceAll(val, "-", " ")
			return val
		}
	}
	return ""
}

func replaceRootHashPH(input, newRootHash string) string {
	parts := strings.Fields(input)
	for i, part := range parts {
		if strings.HasPrefix(part, "roothash=") {
			parts[i] = "roothash=" + newRootHash
			break
		}
	}
	return strings.Join(parts, " ")
}

func prepareVeritySetup(partPair, installRoot string) error {
	// Extract the first part of partPair (before the space)
	parts := strings.Fields(partPair)
	if len(parts) < 1 {
		log.Errorf("Invalid partPair format: %s", partPair)
		return fmt.Errorf("invalid partPair: %s", partPair)
	}
	device := parts[0]

	// Remount the device as read-only
	remountCmd := fmt.Sprintf("mount -o remount,ro %s", device)
	log.Debugf("Remounting device as read-only: %s", remountCmd)
	if _, err := shell.ExecCmd(remountCmd, true, installRoot, nil); err != nil {
		log.Errorf("Failed to remount %s as read-only: %v", device, err)
		return fmt.Errorf("failed to remount %s as read-only: %w", device, err)
	}

	// Create and mount /tmp for ukify (Python tempfile needs this)
	tmpDir := filepath.Join(installRoot, "tmp")
	if _, err := shell.ExecCmd("mkdir -p "+tmpDir, true, shell.HostPath, nil); err != nil {
		log.Errorf("Failed to create /tmp directory: %v", err)
		return fmt.Errorf("failed to create /tmp directory: %w", err)
	}
	if _, err := shell.ExecCmd("mount -t tmpfs tmpfs /tmp", true, installRoot, nil); err != nil {
		log.Errorf("Failed to mount tmpfs on /tmp: %v", err)
		return fmt.Errorf("failed to mount tmpfs on /tmp: %w", err)
	}
	if _, err := shell.ExecCmd("chmod 1777 /tmp", true, installRoot, nil); err != nil {
		log.Errorf("Failed to chmod 1777 on /tmp: %v", err)
		return fmt.Errorf("failed to chmod 1777 on /tmp: %w", err)
	}

	// Create and mount /boot/efi/tmp for veritysetup
	veritytmpDir := filepath.Join(installRoot, "boot/efi/tmp")
	if _, err := shell.ExecCmd("mkdir -p "+veritytmpDir, true, shell.HostPath, nil); err != nil {
		log.Errorf("Failed to create /boot/efi/tmp directory: %v", err)
		return fmt.Errorf("failed to create /boot/efi/tmp directory: %w", err)
	}
	if _, err := shell.ExecCmd("mount -t tmpfs tmpfs /boot/efi/tmp", true, installRoot, nil); err != nil {
		log.Errorf("Failed to mount tmpfs on /boot/efi/tmp: %v", err)
		return fmt.Errorf("failed to mount tmpfs on /boot/efi/tmp: %w", err)
	}
	if _, err := shell.ExecCmd("chmod 1777 /boot/efi/tmp", true, installRoot, nil); err != nil {
		log.Errorf("Failed to chmod 1777 on /boot/efi/tmp: %v", err)
		return fmt.Errorf("failed to chmod 1777 on /boot/efi/tmp: %w", err)
	}
	return nil
}

func removeVerityTmp(installRoot string) {

	// Unmount and clean up /tmp
	tmpDir := filepath.Join(installRoot, "tmp")
	if _, err := shell.ExecCmd("umount /tmp", true, installRoot, nil); err != nil {
		log.Warnf("Failed to unmount tmpfs on /tmp: %v", err)
	}
	if _, err := shell.ExecCmd("rm -rf "+tmpDir, true, shell.HostPath, nil); err != nil {
		log.Warnf("Failed to remove tmp directory %s: %v", tmpDir, err)
	}

	// Unmount and clean up /boot/efi/tmp
	veritytmpDir := filepath.Join(installRoot, "boot/efi/tmp")
	if _, err := shell.ExecCmd("umount /boot/efi/tmp", true, installRoot, nil); err != nil {
		log.Warnf("Failed to unmount tmpfs on /boot/efi/tmp: %v", err)
	}

	if _, err := shell.ExecCmd("rm -rf "+veritytmpDir, true, shell.HostPath, nil); err != nil {
		log.Warnf("Failed to remove tmp directory %s: %v", veritytmpDir, err)
	}
}

func getVerityRootHash(partPair, installRoot string) (string, error) {
	cmd := fmt.Sprintf(`veritysetup format %s`, partPair)
	log.Debugf("Veritysetup Executing command:", cmd)
	// runs on host
	exists, _ := shell.IsCommandExist("ukify", installRoot)
	if !exists {
		log.Debugf("Ukify not found, running veritysetup on host")
		installRoot = shell.HostPath
	}
	output, err := shell.ExecCmd(cmd, true, installRoot, nil)
	if err != nil {
		log.Errorf("Failed to run veritysetup format: %v", err)
		return "", fmt.Errorf("failed to run veritysetup format: %w", err)
	}
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "Root hash:") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				return fields[2], nil
			}
		}
	}
	log.Errorf("Root hash not found in veritysetup output")
	return "", fmt.Errorf("root hash not found in veritysetup output")
}

// Helper to build UKI using ukify
func buildUKI(installRoot, kernelPath, initrdPath, cmdlineFile, outputPath string, template *config.ImageTemplate) error {
	data, err := file.Read(filepath.Join(installRoot, cmdlineFile))
	if err != nil {
		log.Errorf("Failed to read cmdline file %s: %v", cmdlineFile, err)
		return fmt.Errorf("failed to read cmdline file: %w", err)
	}

	cmdlineStr := string(data)
	if template.IsImmutabilityEnabled() {
		partData := extractRootHashPH(cmdlineStr)
		err := prepareVeritySetup(partData, installRoot)
		if err != nil {
			return fmt.Errorf("failed to get root hash part: %w", err)
		}
		rootHashR, err := getVerityRootHash(partData, installRoot)
		if err != nil {
			return fmt.Errorf("failed to get verity root hash: %w", err)
		}
		cmdlineStr = replaceRootHashPH(cmdlineStr, rootHashR)
	}

	// runs on host
	var cmd string
	var backInstallRoot = installRoot
	exists, _ := shell.IsCommandExist("ukify", installRoot)
	if !exists {
		log.Debugf("Ukify not found, running ukify on host")
		kernelPath = filepath.Join(installRoot, kernelPath)
		initrdPath = filepath.Join(installRoot, initrdPath)
		outputPath = filepath.Join(installRoot, outputPath)
		osRelease := filepath.Join(installRoot, "/etc/os-release")
		installRoot = shell.HostPath

		cmd = fmt.Sprintf(
			"ukify build --linux \"%s\" --initrd \"%s\" --cmdline \"%s\" --os-release @\"%s\" --output \"%s\"",
			kernelPath,
			initrdPath,
			cmdlineStr,
			osRelease,
			outputPath,
		)

	} else {
		cmd = fmt.Sprintf(
			"ukify build --linux \"%s\" --initrd \"%s\" --cmdline \"%s\" --output \"%s\"",
			kernelPath,
			initrdPath,
			cmdlineStr,
			outputPath,
		)
	}

	log.Debugf("UKI Executing command:", cmd)
	if template.IsImmutabilityEnabled() {
		// Set TMPDIR environment variable to use the mounted tmpfs
		envVars := []string{"TMPDIR=/tmp"}
		_, err = shell.ExecCmd(cmd, true, installRoot, envVars)
		if err != nil {
			log.Errorf("Failed to build UKI with veritysetup: %v failing command: %s", err, cmd)
			err = fmt.Errorf("failed to build UKI with veritysetup: %w", err)
		}
		installRoot = backInstallRoot
		removeVerityTmp(installRoot)
	} else {
		_, err = shell.ExecCmd(cmd, true, installRoot, nil)
		if err != nil {
			log.Errorf("non-immutable: Failed to build UKI: %v failing command %s", err, cmd)
			err = fmt.Errorf("failed to build UKI: %w", err)
		} else {
			log.Infof("non-immutable: Successfully built UKI: %v  command %s", err, cmd)
		}

	}
	return err
}

// Helper to copy the bootloader EFI file
func copyBootloader(installRoot, src, dst string) error {
	// src and dst should be absolute paths inside the chroot
	// (e.g., /usr/lib/systemd/boot/efi/systemd-bootx64.efi
	// and /boot/efi/EFI/BOOT/BOOTX64.EFI)
	cmd := fmt.Sprintf("cp %s %s", src, dst)
	if _, err := shell.ExecCmd(cmd, true, installRoot, nil); err != nil {
		log.Errorf("Failed to copy bootloader from %s to %s: %v", src, dst, err)
		return fmt.Errorf("failed to copy bootloader from %s to %s: %w", src, dst, err)
	}
	return nil
}

// Verify that the user was created correctly
func verifyUserCreated(installRoot, username string) error {

	// Check if user exists in passwd file
	passwdCmd := fmt.Sprintf("grep '^%s:' /etc/passwd", username)
	// output, err := shell.ExecCmd(passwdCmd, true, installRoot, nil)
	_, err := shell.ExecCmd(passwdCmd, true, installRoot, nil)
	if err != nil {
		// log.Errorf("User %s not found in passwd file: %v", username, err)
		// return fmt.Errorf("user %s not found in passwd file: %w", username, err)
		// Do not log command output or sensitive file contents
		log.Errorf("User %s not found in passwd file", username)
		return fmt.Errorf("user %s not found in passwd file", username)
	}
	// log.Debugf("User in passwd: %s", strings.TrimSpace(output))
	// User was found in passwd; avoid logging the line content to prevent leaking sensitive data

	// Check if user has password in shadow file
	shadowCmd := fmt.Sprintf("grep '^%s:' /etc/shadow", username)
	// output, err = shell.ExecCmd(shadowCmd, true, installRoot, nil)
	_, err = shell.ExecCmd(shadowCmd, true, installRoot, nil)
	if err != nil {
		// log.Errorf("User %s not found in shadow file: %v", username, err)
		// return fmt.Errorf("user %s not found in shadow file: %w", username, err)
		// Do not log command output or sensitive file contents
		log.Errorf("User %s not found in shadow file", username)
		return fmt.Errorf("user %s not found in shadow file", username)
	}
	// log.Debugf("User in shadow: %s", strings.TrimSpace(output))
	// User was found in shadow; avoid logging the line content to prevent leaking sensitive data

	return nil
}

func createUser(installRoot string, template *config.ImageTemplate) error {
	// Check if there are any users to create
	if len(template.SystemConfig.Users) == 0 {
		log.Debug("No users defined in template, skipping user creation")
		return nil
	}

	// Loop through each user in the template configuration
	for _, user := range template.SystemConfig.Users {
		log.Infof("Creating user: %s", user.Name)

		// Create the user with useradd command
		// -m creates home directory, -s sets shell
		cmd := fmt.Sprintf("useradd -m -s /bin/bash %s", user.Name)
		output, err := shell.ExecCmdSilent(cmd, true, installRoot, nil)
		if err != nil {
			if strings.Contains(output, "already exists") {
				log.Warnf("User %s already exists", user.Name)
			} else {
				log.Errorf("Failed to create user %s: output: %s, err: %v", user.Name, output, err)
				return fmt.Errorf("failed to create user %s: output: %s, err: %w", user.Name, output, err)
			}
		}

		// Set password if provided
		if user.Password != "" {
			if err := setUserPassword(installRoot, user); err != nil {
				return fmt.Errorf("failed to set password for user %s: %w", user.Name, err)
			}
		} else {
			cmd := fmt.Sprintf("passwd -d %s", user.Name)
			if _, err := shell.ExecCmd(cmd, true, installRoot, nil); err != nil {
				log.Errorf("Failed to delete password for user %s: %v", user.Name, err)
				return fmt.Errorf("failed to delete password for user %s: %w", user.Name, err)
			}
			log.Debugf("Deleted password for user %s (no password set)", user.Name)
		}

		// Collect requested groups and auto-add sudo groups when needed
		groupCandidates := collectUserGroups(user, template)
		for _, group := range groupCandidates {
			if err := ensureGroupExists(installRoot, group); err != nil {
				return fmt.Errorf("failed to ensure group %s exists: %w", group, err)
			}

			groupCmd := fmt.Sprintf("usermod -aG %s %s", group, user.Name)
			if _, err := shell.ExecCmd(groupCmd, true, installRoot, nil); err != nil {
				log.Errorf("Failed to add user %s to group %s: %v", user.Name, group, err)
				return fmt.Errorf("failed to add user %s to group %s: %w", user.Name, group, err)
			}
			log.Debugf("Added user %s to group %s", user.Name, group)
		}

		// Verify user creation
		if err := verifyUserCreated(installRoot, user.Name); err != nil {
			return fmt.Errorf("user verification failed for %s: %w", user.Name, err)
		}

		if user.StartupScript != "" {
			if err := configUserStartupScript(installRoot, user); err != nil {
				return fmt.Errorf("failed to configure startup script for user %s: %w", user.Name, err)
			}
		}

		log.Infof("User %s created successfully", user.Name)
	}

	return nil
}

func ensureGroupExists(installRoot, group string) error {
	cmd := fmt.Sprintf("getent group %s", group)
	if _, err := shell.ExecCmdSilent(cmd, true, installRoot, nil); err == nil {
		return nil
	}

	createCmd := fmt.Sprintf("groupadd %s", group)
	if output, err := shell.ExecCmd(createCmd, true, installRoot, nil); err != nil {
		if strings.Contains(output, "already exists") {
			return nil
		}
		return fmt.Errorf("groupadd failed: %w", err)
	}
	return nil
}

func collectUserGroups(user config.UserConfig, template *config.ImageTemplate) []string {
	var groups []string
	seen := make(map[string]struct{})

	appendGroup := func(group string) {
		group = strings.TrimSpace(group)
		if group == "" {
			return
		}
		if strings.HasPrefix(group, "<") && strings.HasSuffix(group, ">") {
			return
		}
		if _, ok := seen[group]; ok {
			return
		}
		seen[group] = struct{}{}
		groups = append(groups, group)
	}

	for _, group := range user.Groups {
		appendGroup(group)
	}

	if user.Sudo {
		for _, sudoGroup := range defaultSudoGroups(template) {
			appendGroup(sudoGroup)
		}
	}

	return groups
}

func defaultSudoGroups(template *config.ImageTemplate) []string {
	switch template.Target.OS {
	case "azure-linux", "edge-microvisor-toolkit":
		return []string{"wheel", "sudo"}
	default:
		return []string{"sudo"}
	}
}

// Helper function to set user password based on hash algorithm
func setUserPassword(installRoot string, user config.UserConfig) error {
	// Check if password is already hashed or needs hashing
	if user.HashAlgo != "" {
		log.Debugf("Setting password with hash algorithm %s for user %s", user.HashAlgo, user.Name)

		// Check if password is already in hashed format (starts with $)
		if strings.HasPrefix(user.Password, "$") {
			// Password is already hashed, use usermod to set it directly
			usermodCmd := fmt.Sprintf("usermod -p '%s' %s", user.Password, user.Name)
			if _, err := shell.ExecCmd(usermodCmd, true, installRoot, nil); err != nil {
				// log.Errorf("Failed to set hashed password for user %s: %v", user.Name, err)
				// return fmt.Errorf("failed to set hashed password for user %s: %w", user.Name, err)
				log.Errorf("Failed to set hashed password for user %s", user.Name)
				return fmt.Errorf("failed to set hashed password for user %s", user.Name)
			}
		} else {
			// Password is plaintext, need to hash it first
			hashedPassword, err := hashPassword(user.Password, user.HashAlgo, installRoot)
			if err != nil {
				return fmt.Errorf("failed to hash password for user %s: %w", user.Name, err)
			}

			usermodCmd := fmt.Sprintf("usermod -p '%s' %s", hashedPassword, user.Name)
			if _, err := shell.ExecCmd(usermodCmd, true, installRoot, nil); err != nil {
				// log.Errorf("Failed to set hashed password for user %s: %v", user.Name, err)
				// return fmt.Errorf("failed to set hashed password for user %s: %w", user.Name, err)
				log.Errorf("Failed to set password for user %s", user.Name)
				return fmt.Errorf("failed to set password for user %s", user.Name)
			}
		}
	} else {
		// No hash algorithm specified, use interactive passwd command (legacy behavior)
		passwdInput := fmt.Sprintf("%s\n%s\n", user.Password, user.Password)
		passwdCmd := fmt.Sprintf("passwd %s", user.Name)
		if _, err := shell.ExecCmdWithInput(passwdInput, passwdCmd, true, installRoot, nil); err != nil {
			// log.Errorf("Failed to set password for user %s: %v", user.Name, err)
			// return fmt.Errorf("failed to set password for user %s: %w", user.Name, err)
			log.Errorf("Failed to set password for user %s", user.Name)
			return fmt.Errorf("failed to set password for user %s", user.Name)
		}
	}

	return nil
}

// Helper function to hash password using specified algorithm
func hashPassword(password, hashAlgo, installRoot string) (string, error) {
	var cmd string

	switch strings.ToLower(hashAlgo) {
	case "sha512":
		// Use openssl to generate SHA-512 hash
		cmd = fmt.Sprintf("openssl passwd -6 '%s'", password)
	case "sha256":
		// Use openssl to generate SHA-256 hash
		cmd = fmt.Sprintf("openssl passwd -5 '%s'", password)
	case "md5":
		// Use openssl to generate MD5 hash (not recommended for production)
		cmd = fmt.Sprintf("openssl passwd -1 '%s'", password)
	case "bcrypt":
		// Use python3 to generate bcrypt hash
		pythonScript := fmt.Sprintf("import bcrypt; print(bcrypt.hashpw(b'%s', bcrypt.gensalt()).decode())", password)
		cmd = fmt.Sprintf("python3 -c \"%s\"", pythonScript)
	default:
		return "", fmt.Errorf("unsupported hash algorithm: %s", hashAlgo)
	}

	log.Debugf("Hashing password with algorithm %s", hashAlgo)
	output, err := shell.ExecCmd(cmd, true, installRoot, nil)
	if err != nil {
		// log.Errorf("Failed to hash password with algorithm %s: %v", hashAlgo, err)
		log.Errorf("Failed to hash password with algorithm %s", hashAlgo)
		return "", fmt.Errorf("failed to hash password with algorithm %s: %w", hashAlgo, err)
	}

	hashedPassword := strings.TrimSpace(output)
	log.Debugf("Password hashed successfully with algorithm %s", hashAlgo)

	return hashedPassword, nil
}

func configUserStartupScript(installRoot string, user config.UserConfig) error {
	log.Infof("Configuring user '%s' startup script to: %s", user.Name, user.StartupScript)

	// Escape user.Name and user.StartupScript for regex safety
	escapedUserName := regexp.QuoteMeta(user.Name)
	escapedStartupScript := regexp.QuoteMeta(user.StartupScript)
	startupScriptHostPath := filepath.Join(installRoot, user.StartupScript)

	// Verify that the startup script exists in the image
	if _, err := os.Stat(startupScriptHostPath); os.IsNotExist(err) {
		log.Errorf("Startup script %s does not exist in image for user %s", user.StartupScript, user.Name)
		return fmt.Errorf("startup script %s does not exist in image for user %s", user.StartupScript, user.Name)
	}

	findPattern := fmt.Sprintf(`^(%s.*):[^:]*$`, escapedUserName)
	replacePattern := fmt.Sprintf(`\1:%s`, escapedStartupScript)
	passwdFile := filepath.Join(installRoot, "etc", "passwd")

	if err := file.ReplaceRegexInFile(findPattern, replacePattern, passwdFile); err != nil {
		// log.Errorf("Failed to update user %s startup command: %v", user.Name, err)
		// Log only high-level context to avoid leaking potentially sensitive details from the underlying error.
		log.Errorf("Failed to update startup command for user %s", user.Name)
		return fmt.Errorf("failed to update user %s startup command: %w", user.Name, err)
	}
	return nil
}
func (imageOs *ImageOs) generateSBOM(installRoot string, template *config.ImageTemplate) (string, error) {
	pkgType := imageOs.chrootEnv.GetTargetOsPkgType()
	sBomFNm := rpmutils.GenerateSPDXFileName(template.GetImageName())
	cmd := "rpm -qa"
	if pkgType == "deb" {
		cmd = "dpkg -l | awk '/^ii/ {print $2}'"
		sBomFNm = debutils.GenerateSPDXFileName(template.GetImageName())
	}
	manifest.DefaultSPDXFile = sBomFNm

	result, err := shell.ExecCmd(cmd, true, installRoot, nil)
	if err != nil {
		log.Errorf("failed to pull BOM from actual image: %v", err)
		return "", fmt.Errorf("Failed to pull BOM from actual image: %w", err)
	}

	installRootPkgs := strings.Split(strings.TrimSpace(result), "\n")
	downloadedPkgs := template.FullPkgListBom

	// Create a map of normalized package names from installed packages for faster lookup
	installedPkgMap := make(map[string]bool)
	for _, pkg := range installRootPkgs {
		// Remove architecture tag (e.g., ":amd64") if present
		normalizedPkg := pkg
		if colonIndex := strings.Index(pkg, ":"); colonIndex != -1 {
			normalizedPkg = pkg[:colonIndex]
		}
		installedPkgMap[normalizedPkg] = true
	}

	var finalPkgs []ospackage.PackageInfo
	for _, pkg := range downloadedPkgs {
		// Normalize package name by removing file extensions
		normalizedName := pkg.Name
		if strings.HasSuffix(normalizedName, ".rpm") {
			normalizedName = strings.TrimSuffix(normalizedName, ".rpm")
		} else if strings.HasSuffix(normalizedName, ".deb") {
			normalizedName = strings.TrimSuffix(normalizedName, ".deb")
		}

		if installedPkgMap[normalizedName] {
			finalPkgs = append(finalPkgs, pkg)
		}
	}

	log.Infof("SBOM raw data (installed=%d, downloaded=%d, final=%d)", len(installRootPkgs), len(downloadedPkgs), len(finalPkgs))

	// Generate SPDX manifest, generated in temp directory
	spdxFile := filepath.Join(config.TempDir(), manifest.DefaultSPDXFile)
	if err := manifest.WriteSPDXToFile(finalPkgs, spdxFile); err != nil {
		log.Warnf("SPDX SBOM creation error: %v", err)
	}
	log.Infof("SPDX file created at %s", spdxFile)

	// Copy SBOM into image filesystem
	if err := manifest.CopySBOMToChroot(installRoot); err != nil {
		log.Warnf("failed to copy SBOM into image filesystem: %v", err)
		// Don't fail the build if SBOM copy fails, just log warning
	}

	return result, nil
}

// isSymlink checks if a given path is a symbolic link
func isSymlink(path string) (bool, error) {
	fileInfo, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	return fileInfo.Mode()&os.ModeSymlink != 0, nil
}

// fixKernelSymlinks ensures that /boot/vmlinuz-{version} symlinks exist
// pointing to /lib/modules/{version}/vmlinuz. This is normally done by the
// kernel package's post-install script, but that may not run properly in chroot.
func fixKernelSymlinks(installRoot string) error {
	log.Debug("Creating kernel symlinks if needed")
	bootDir := filepath.Join(installRoot, "boot")
	libModulesDir := filepath.Join(installRoot, "lib", "modules")

	// Check if boot directory exists
	if _, err := os.Stat(bootDir); os.IsNotExist(err) {
		log.Debugf("boot directory does not exist at %s, skipping symlink fix", bootDir)
		return nil
	}

	// Check if destination directory already has vmlinuz files - if so, ignore and return
	bootEntries, err := os.ReadDir(bootDir)
	if err == nil {
		for _, entry := range bootEntries {
			if strings.HasPrefix(entry.Name(), "vmlinuz") {
				log.Debugf("Found existing vmlinuz file in boot directory: %s, skipping kernel symlink creation", entry.Name())
				return nil
			}
		}
	}

	// Check if lib/modules directory exists
	if _, err := os.Stat(libModulesDir); os.IsNotExist(err) {
		log.Debugf("lib/modules directory does not exist at %s, skipping symlink fix", libModulesDir)
		return nil
	}

	// Read lib/modules directory to find kernel versions
	entries, err := os.ReadDir(libModulesDir)
	if err != nil {
		return fmt.Errorf("failed to read lib/modules directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		kernelVersion := entry.Name()

		// Skip non-kernel version directories (like "build", "source", etc.)
		if !strings.Contains(kernelVersion, ".") {
			log.Debugf("Skipping non-version directory: %s", kernelVersion)
			continue
		}

		kernelSourcePath := filepath.Join(libModulesDir, kernelVersion, "vmlinuz")
		kernelBootLink := filepath.Join(bootDir, "vmlinuz-"+kernelVersion)

		// Check if the source file exists
		if _, err := os.Stat(kernelSourcePath); os.IsNotExist(err) {
			log.Debugf("vmlinuz file not found at %s, skipping symlink creation", kernelSourcePath)
			continue
		}

		// Check if symlink already exists
		if _, err := os.Lstat(kernelBootLink); err == nil {
			// Symlink or file already exists
			if isSymlink, _ := isSymlink(kernelBootLink); isSymlink {
				log.Debugf("vmlinuz symlink already exists at %s", kernelBootLink)
				continue
			}

			// If it's not a symlink, try to replace it
			log.Debugf("Non-symlink file exists at %s, removing it", kernelBootLink)
			if err := os.Remove(kernelBootLink); err != nil {
				log.Warnf("Failed to remove file at %s: %v", kernelBootLink, err)
				continue
			}
		}

		// Create the symlink - use relative path from /boot to /lib/modules
		relPath := filepath.Join("..", "..", "lib", "modules", kernelVersion, "vmlinuz")
		if err := os.Symlink(relPath, kernelBootLink); err != nil {
			log.Warnf("Failed to create symlink from %s to %s: %v", kernelBootLink, relPath, err)
			// Don't fail, continue with other kernel versions
			continue
		}

		log.Infof("Created vmlinuz symlink for kernel %s: %s -> %s", kernelVersion, kernelBootLink, relPath)
	}
	log.Debug("Finished creating kernel symlinks")
	return nil
}
