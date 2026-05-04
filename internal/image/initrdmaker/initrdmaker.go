package initrdmaker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/chroot"
	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/config/manifest"
	"github.com/open-edge-platform/image-composer-tool/internal/image/imageos"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage/debutils"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage/rpmutils"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/file"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/mount"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/system"
)

type InitrdMakerInterface interface {
	Init() error
	DownloadInitrdPkgs() error
	BuildInitrdImage() error
	GetInitrdVersion() string
	GetInitrdFilePath() string
	GetInitrdRootfsPath() string
	CleanInitrdRootfs() error
}

type InitrdMaker struct {
	template         *config.ImageTemplate
	ImageBuildDir    string
	InitrdRootfsPath string
	InitrdFilePath   string
	VersionInfo      string
	ChrootEnv        chroot.ChrootEnvInterface
	ImageOs          imageos.ImageOsInterface
}

var log = logger.Logger()

func NewInitrdMaker(chrootEnv chroot.ChrootEnvInterface, template *config.ImageTemplate) (*InitrdMaker, error) {
	// nil checking is done one in constructor only to avoid repetitive checks
	// in every method and schema check is done during template load making
	// sure internal structure is valid
	if template == nil {
		return nil, fmt.Errorf("image template cannot be nil")
	}
	if chrootEnv == nil {
		return nil, fmt.Errorf("chroot environment cannot be nil")
	}

	imageOs, err := imageos.NewImageOs(chrootEnv, template)
	if err != nil {
		return nil, fmt.Errorf("failed to create image OS: %w", err)
	}

	return &InitrdMaker{
		template:  template, // Store template
		ChrootEnv: chrootEnv,
		ImageOs:   imageOs, // Already template-aware
	}, nil
}

func (initrdMaker *InitrdMaker) Init() error {
	globalWorkDir, err := config.WorkDir()
	if err != nil {
		return fmt.Errorf("failed to get global work directory: %w", err)
	}

	providerId := system.GetProviderId(
		initrdMaker.template.Target.OS,
		initrdMaker.template.Target.Dist,
		initrdMaker.template.Target.Arch,
	)

	initrdMaker.ImageBuildDir = filepath.Join(
		globalWorkDir,
		providerId,
		"imagebuild",
		initrdMaker.template.GetSystemConfigName(),
	)

	return os.MkdirAll(initrdMaker.ImageBuildDir, 0700)
}

func (initrdMaker *InitrdMaker) GetInitrdVersion() string {
	return initrdMaker.VersionInfo
}

func (initrdMaker *InitrdMaker) GetInitrdFilePath() string {
	return initrdMaker.InitrdFilePath
}

func (initrdMaker *InitrdMaker) GetInitrdRootfsPath() string {
	return initrdMaker.InitrdRootfsPath
}

func (initrdMaker *InitrdMaker) DownloadInitrdPkgs() error {
	log.Infof("Downloading packages for: %s", initrdMaker.template.GetImageName())

	if err := initrdMaker.ChrootEnv.UpdateSystemPkgs(initrdMaker.template); err != nil {
		return fmt.Errorf("failed to update system packages: %w", err)
	}

	pkgList := initrdMaker.template.GetPackages()
	pkgType := initrdMaker.ChrootEnv.GetTargetOsPkgType()
	if pkgType == "deb" {
		_, err := debutils.DownloadPackages(pkgList, initrdMaker.ChrootEnv.GetChrootPkgCacheDir(), "", nil, false)
		if err != nil {
			return fmt.Errorf("failed to download initrd packages: %w", err)
		}
	} else if pkgType == "rpm" {
		_, err := rpmutils.DownloadPackages(pkgList, initrdMaker.ChrootEnv.GetChrootPkgCacheDir(), "", nil, false)
		if err != nil {
			return fmt.Errorf("failed to download initrd packages: %w", err)
		}
	}

	if err := initrdMaker.ChrootEnv.UpdateChrootLocalRepoMetadata(
		chroot.ChrootRepoDir,
		initrdMaker.template.Target.Arch, false); err != nil {
		return fmt.Errorf("failed to update local cache repository metadata: %w", err)
	}

	if err := initrdMaker.ChrootEnv.RefreshLocalCacheRepo(); err != nil {
		return fmt.Errorf("failed to refresh local cache repository: %w", err)
	}
	return nil
}

func (initrdMaker *InitrdMaker) BuildInitrdImage() (err error) {
	log.Infof("Building initrd image for: %s", initrdMaker.template.GetImageName())

	imageName := initrdMaker.template.GetImageName()

	initrdMaker.InitrdRootfsPath, initrdMaker.VersionInfo, err = initrdMaker.ImageOs.InstallInitrd()
	if err != nil {
		if cleanErr := initrdMaker.CleanInitrdRootfs(); cleanErr != nil {
			log.Errorf("Failed to clean initrd rootfs after install failure: %v", cleanErr)
		}
		return fmt.Errorf("failed to install initrd: %w", err)
	}

	initrdMaker.InitrdFilePath = filepath.Join(initrdMaker.ImageBuildDir, fmt.Sprintf("%s-%s.img",
		imageName, initrdMaker.VersionInfo))

	// Copy SBOM into the initrd rootfs (inside the image)
	if err := manifest.CopySBOMToChroot(initrdMaker.InitrdRootfsPath); err != nil {
		log.Warnf("Failed to copy SBOM into initrd filesystem: %v", err)
		// Don't fail the build if SBOM copy fails, just log warning
	}

	if err := addInitScriptsToInitrd(initrdMaker.InitrdRootfsPath); err != nil {
		return fmt.Errorf("failed to add init scripts to initrd: %w", err)
	}

	if err := copyKernelToOutput(initrdMaker.InitrdRootfsPath, initrdMaker.ImageBuildDir); err != nil {
		return fmt.Errorf("failed to copy kernel to output directory: %w", err)
	}

	if err := initrdMaker.createInitrdImg(); err != nil {
		return fmt.Errorf("failed to create initrd image: %w", err)
	}

	// Copy SBOM to image build directory
	if err := manifest.CopySBOMToImageBuildDir(initrdMaker.ImageBuildDir); err != nil {
		log.Warnf("Failed to copy SBOM to image build directory: %v", err)
		// Don't fail the build if SBOM copy fails, just log warning
	}

	initrdMaker.template.FinishPureImageBuildTimer()
	pureImageBuildDuration := initrdMaker.template.GetPureImageBuildDuration()
	if pureImageBuildDuration > 0 {
		log.Infof("Pure initrd image build time: %s", pureImageBuildDuration.Round(time.Millisecond))
		log.Infof("Initrd image build completed successfully: %s", initrdMaker.InitrdFilePath)
	}

	return nil
}

func addInitScriptsToInitrd(initrdRootfsPath string) error {
	log.Infof("Adding init scripts to initrd...")

	generalConfigDir, err := config.GetGeneralConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get general config directory: %w", err)
	}
	rcLocalSrc := filepath.Join(generalConfigDir, "isolinux", "rc.local")
	if _, err := os.Stat(rcLocalSrc); os.IsNotExist(err) {
		log.Errorf("rc.local file does not exist: %s", rcLocalSrc)
		return fmt.Errorf("rc.local file does not exist: %s", rcLocalSrc)
	}

	rcLocalDest := filepath.Join(initrdRootfsPath, "etc", "rc.d", "rc.local")
	return file.CopyFile(rcLocalSrc, rcLocalDest, "--preserve=mode", true)
}

func copyKernelToOutput(initrdRootfsPath, outputDir string) error {
	// Copy kernel to the target path
	var vmlinuzFileList []string
	cmdStr := "ls /boot | grep vmlinuz"
	output, err := shell.ExecCmd(cmdStr, true, initrdRootfsPath, nil)
	if err != nil {
		log.Errorf("Failed to list vmlinuz files in /boot: %v", err)
		return fmt.Errorf("failed to list vmlinuz files in /boot: %w", err)
	}
	for _, line := range strings.Split(output, "\n") {
		vmlinuzFile := strings.TrimSpace(line)
		if vmlinuzFile == "" {
			continue
		}
		if strings.HasPrefix(vmlinuzFile, "vmlinuz") {
			vmlinuzFileList = append(vmlinuzFileList, vmlinuzFile)
		}
	}

	if len(vmlinuzFileList) == 0 {
		log.Errorf("No vmlinuz files found in /boot")
		return fmt.Errorf("no vmlinuz files found in /boot")
	}

	kernelPath := filepath.Join(initrdRootfsPath, "boot", vmlinuzFileList[0])
	if _, err := os.Stat(kernelPath); os.IsNotExist(err) {
		log.Errorf("Kernel file does not exist: %s", kernelPath)
		return fmt.Errorf("kernel file does not exist: %s", kernelPath)
	}
	kernelDestPath := filepath.Join(outputDir, vmlinuzFileList[0])
	if err := file.CopyFile(kernelPath, kernelDestPath, "--preserve=mode", true); err != nil {
		log.Errorf("Failed to copy kernel to %s: %v", outputDir, err)
		return fmt.Errorf("failed to copy kernel to %s: %w", outputDir, err)
	}
	return nil
}

func (initrdMaker *InitrdMaker) createInitrdImg() error {
	if err := mount.UmountPath(initrdMaker.InitrdRootfsPath + "/cdrom/cache-repo"); err != nil {
		log.Errorf("Failed to unmount cache-repo %s: %v",
			initrdMaker.InitrdRootfsPath+"/cdrom/cache-repo", err)
		return fmt.Errorf("failed to unmount cache-repo %s: %w",
			initrdMaker.InitrdRootfsPath+"/cdrom/cache-repo", err)
	}

	cmdStr := fmt.Sprintf("cd %s && sudo find . | sudo cpio -o -H newc | sudo gzip > %s",
		initrdMaker.InitrdRootfsPath, initrdMaker.InitrdFilePath)
	if _, err := shell.ExecCmdWithStream(cmdStr, false, shell.HostPath, nil); err != nil {
		log.Errorf("Failed to create initrd image: %v", err)
		return fmt.Errorf("failed to create initrd image: %w", err)
	}
	if _, err := os.Stat(initrdMaker.InitrdFilePath); os.IsNotExist(err) {
		log.Errorf("Initrd image file does not exist: %s", initrdMaker.InitrdFilePath)
		return fmt.Errorf("initrd image file does not exist: %s", initrdMaker.InitrdFilePath)
	}
	return nil
}

func (initrdMaker *InitrdMaker) CleanInitrdRootfs() error {
	log.Infof("Cleaning up initrd rootfs: %s", initrdMaker.InitrdRootfsPath)

	if initrdMaker.InitrdRootfsPath == "" {
		log.Debugf("Initrd rootfs path is empty, nothing to clean")
		return nil
	}

	if _, err := os.Stat(initrdMaker.InitrdRootfsPath); os.IsNotExist(err) {
		log.Debugf("Initrd rootfs path does not exist: %s", initrdMaker.InitrdRootfsPath)
		return nil
	}

	if err := mount.UmountPath(initrdMaker.InitrdRootfsPath + "/cdrom/cache-repo"); err != nil {
		log.Errorf("Failed to unmount cache-repo %s: %v",
			initrdMaker.InitrdRootfsPath+"/cdrom/cache-repo", err)
		return fmt.Errorf("failed to unmount cache-repo %s: %w",
			initrdMaker.InitrdRootfsPath+"/cdrom/cache-repo", err)
	}

	// Remove the initrd rootfs directory
	if _, err := shell.ExecCmd("rm -rf "+initrdMaker.InitrdRootfsPath, true, shell.HostPath, nil); err != nil {
		log.Errorf("Failed to remove initrd rootfs directory %s: %v",
			initrdMaker.InitrdRootfsPath, err)
		return fmt.Errorf("failed to remove initrd rootfs directory: %w", err)
	}

	return nil
}
