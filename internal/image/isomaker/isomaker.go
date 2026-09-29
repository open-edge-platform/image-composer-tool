package isomaker

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/chroot"
	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/config/manifest"
	"github.com/open-edge-platform/image-composer-tool/internal/image/imageos"
	"github.com/open-edge-platform/image-composer-tool/internal/image/initrdmaker"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/file"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/system"
)

type IsoMakerInterface interface {
	Init() error          // Initialize with stored template
	BuildIsoImage() error // Build ISO image using stored template
}

type IsoMaker struct {
	template      *config.ImageTemplate
	ImageBuildDir string
	ChrootEnv     chroot.ChrootEnvInterface
	ImageOs       imageos.ImageOsInterface
	InitrdMaker   initrdmaker.InitrdMakerInterface
}

const IsoLabel = "ICT_CDROM"

var log = logger.Logger()

func NewIsoMaker(chrootEnv chroot.ChrootEnvInterface, template *config.ImageTemplate) (*IsoMaker, error) {
	// nil checking is done one in constructor only to avoid repetitive checks
	// in every method and schema check is done during template load making
	// sure internal structure is valid
	if template == nil {
		return nil, fmt.Errorf("image template cannot be nil")
	}
	if chrootEnv == nil {
		return nil, fmt.Errorf("chroot environment cannot be nil")
	}

	// Create ImageOs with template
	imageOs, err := imageos.NewImageOs(chrootEnv, template)
	if err != nil {
		return nil, fmt.Errorf("failed to create image OS: %w", err)
	}

	return &IsoMaker{
		template:  template,
		ChrootEnv: chrootEnv,
		ImageOs:   imageOs,
	}, nil
}

func (isoMaker *IsoMaker) Init() error {

	globalWorkDir, err := config.WorkDir()
	if err != nil {
		return fmt.Errorf("failed to get global work directory: %w", err)
	}

	providerId := system.GetProviderId(
		isoMaker.template.Target.OS,
		isoMaker.template.Target.Dist,
		isoMaker.template.Target.Arch,
	)

	isoMaker.ImageBuildDir = filepath.Join(globalWorkDir,
		providerId,
		"imagebuild",
		isoMaker.template.GetSystemConfigName(),
	)

	return os.MkdirAll(isoMaker.ImageBuildDir, 0700)
}

func (isoMaker *IsoMaker) BuildIsoImage() (err error) {

	log.Infof("Building ISO image for: %s", isoMaker.template.GetImageName())

	// The installer creates these accounts on the target; fail now rather than
	// emit an ISO whose every installation stops at the missing credential.
	if err := config.ValidateUserCredentials(isoMaker.template.SystemConfig.Users); err != nil {
		return err
	}

	if err := isoMaker.buildInitrd(isoMaker.template); err != nil {
		return fmt.Errorf("failed to build initrd image: %w", err)
	}
	defer func() {
		if cleanErr := isoMaker.InitrdMaker.CleanInitrdRootfs(); cleanErr != nil {
			err = fmt.Errorf("failed to clean initrd rootfs: %w", cleanErr)
		}
	}()

	versionInfo := isoMaker.InitrdMaker.GetInitrdVersion()
	ImageName := fmt.Sprintf("%s-%s", isoMaker.template.GetImageName(), versionInfo)
	isoFilePath := filepath.Join(isoMaker.ImageBuildDir, fmt.Sprintf("%s.iso", ImageName))

	sbomName, err := isoMaker.writeTargetSBOM()
	if err != nil {
		return err
	}
	composition, err := isoMaker.buildCompositionManifest(filepath.Base(isoFilePath), sbomName)
	if err != nil {
		return fmt.Errorf("failed to build composition manifest: %w", err)
	}

	initrdRootfsPath := isoMaker.InitrdMaker.GetInitrdRootfsPath()
	initrdFilePath := isoMaker.InitrdMaker.GetInitrdFilePath()
	if err := isoMaker.createIso(isoMaker.template, initrdRootfsPath, initrdFilePath, isoFilePath); err != nil {
		return fmt.Errorf("failed to create ISO image: %w", err)
	}

	compositionPath := filepath.Join(isoMaker.ImageBuildDir, manifest.CompositionManifestFileName(ImageName))
	if err := manifest.WriteCompositionManifest(composition, compositionPath); err != nil {
		return err
	}

	// The composition manifest references this SBOM, so it must be retained.
	if err := manifest.CopySBOMToImageBuildDir(isoMaker.ImageBuildDir); err != nil {
		return fmt.Errorf("failed to copy SBOM to image build directory: %w", err)
	}

	isoMaker.template.FinishPureImageBuildTimer()
	pureImageBuildDuration := isoMaker.template.GetPureImageBuildDuration()
	if pureImageBuildDuration > 0 {
		log.Infof("Pure ISO image build time: %s", pureImageBuildDuration.Round(time.Millisecond))
	}

	log.Infof("ISO image build completed successfully: %s", isoFilePath)

	return nil
}

func (isoMaker *IsoMaker) buildInitrd(template *config.ImageTemplate) error {
	if isoMaker.InitrdMaker == nil {
		initrdTemplate, err := isoMaker.getInitrdTemplate(template)
		if err != nil {
			return fmt.Errorf("failed to get initrd template: %w", err)
		}

		if err := ValidateAdditionalFiles(initrdTemplate); err != nil {
			return fmt.Errorf("ISO build prerequisites not met: %w", err)
		}

		isoMaker.InitrdMaker, err = initrdmaker.NewInitrdMaker(isoMaker.ChrootEnv, initrdTemplate)
		if err != nil {
			return fmt.Errorf("failed to create initrd maker: %w", err)
		}
	}

	if err := isoMaker.InitrdMaker.Init(); err != nil {
		return fmt.Errorf("failed to initialize initrd maker: %w", err)
	}

	if err := isoMaker.InitrdMaker.DownloadInitrdPkgs(); err != nil {
		return fmt.Errorf("failed to download initrd packages: %w", err)
	}

	if err := isoMaker.InitrdMaker.BuildInitrdImage(); err != nil {
		return fmt.Errorf("failed to build initrd image: %w", err)
	}

	return nil
}

func (isoMaker *IsoMaker) getInitrdTemplate(template *config.ImageTemplate) (*config.ImageTemplate, error) {
	initrdTemplateFilePath, err := template.GetInitramfsTemplate()
	if err != nil {
		return nil, fmt.Errorf("failed to get initramfs template path: %w", err)
	}

	initrdTemplate, err := config.LoadAndMergeTemplate(initrdTemplateFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to load and merge initrd template: %w", err)
	}

	return initrdTemplate, nil
}

// ValidateISOPrerequisites checks that all prerequisites for an ISO build are
// met before starting expensive operations. Call this early (before provider init
// or package download) to fail fast on missing files like live-installer.
func ValidateISOPrerequisites(template *config.ImageTemplate) error {
	// The ISO template's own additionalFiles land on the installed system; a
	// missing one would otherwise be dropped with only a warning at build time.
	if err := ValidateAdditionalFiles(template); err != nil {
		return err
	}

	initrdTemplateFilePath, err := template.GetInitramfsTemplate()
	if err != nil {
		return fmt.Errorf("failed to resolve initramfs template: %w", err)
	}

	initrdTemplate, err := config.LoadAndMergeTemplate(initrdTemplateFilePath)
	if err != nil {
		return fmt.Errorf("failed to load initrd template for validation: %w", err)
	}

	return ValidateAdditionalFiles(initrdTemplate)
}

// ValidateAdditionalFiles checks that all additional files referenced in the
// template exist before starting expensive build operations. This catches
// missing dependencies like the live-installer binary early.
func ValidateAdditionalFiles(template *config.ImageTemplate) error {
	if template == nil {
		return fmt.Errorf("template cannot be nil")
	}

	for _, fileInfo := range template.SystemConfig.AdditionalFiles {
		if fileInfo.Local == "" || fileInfo.Final == "" {
			continue
		}

		if filepath.IsAbs(fileInfo.Local) {
			if err := checkFileExists(fileInfo.Local); err != nil {
				return liveInstallerHintOrError(fileInfo.Local, fileInfo.Final, err)
			}
			continue
		}

		// Use the same ancestor-walk resolution as the build, so validation is
		// never stricter than GetAdditionalFileInfo.
		candidatePath, found := config.ResolveTemplateRelativePath(template.PathList, fileInfo.Local)
		if !found {
			return liveInstallerHintOrError(fileInfo.Local, fileInfo.Final, nil)
		}
		if err := checkFileExists(candidatePath); err != nil {
			return fmt.Errorf("cannot access additional file %s: %w", fileInfo.Local, err)
		}
	}
	return nil
}

// checkFileExists verifies the path exists and is a regular file (not a directory).
func checkFileExists(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("path is a directory, expected a regular file: %s", path)
	}
	return nil
}

// liveInstallerHintOrError returns an actionable error with build instructions
// if the missing file is live-installer, otherwise a generic missing file error.
func liveInstallerHintOrError(local, final string, wrapped error) error {
	if isLiveInstaller(local) || isLiveInstaller(final) {
		return fmt.Errorf("live-installer binary not found (referenced as %s). "+
			"Build it first: go build -buildmode=pie -o ./build/live-installer ./cmd/live-installer",
			local)
	}
	if wrapped != nil {
		return fmt.Errorf("required file not accessible: %s (target: %s): %w", local, final, wrapped)
	}
	return fmt.Errorf("required additional file not found: %s (target: %s)", local, final)
}

func isLiveInstaller(path string) bool {
	return strings.Contains(path, "live-installer")
}

func (isoMaker *IsoMaker) copyConfigFilesToIso(template *config.ImageTemplate, installRoot string) error {
	// Copy general config files to ISO
	generalConfigSrcDir, err := config.GetGeneralConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get general config directory: %w", err)
	}
	generalConfigDestDir := filepath.Join(installRoot, "config", "general")
	if err := file.CopyDir(generalConfigSrcDir, generalConfigDestDir, "--preserve=mode", true); err != nil {
		log.Errorf("Failed to copy general config files to iso root: %v", err)
		return fmt.Errorf("failed to copy general config files to iso root: %w", err)
	}

	// Copy OSV config files to ISO
	osvConfigSrcDir := isoMaker.ChrootEnv.GetTargetOsConfigDir()
	osvConfigDestDir := filepath.Join(installRoot, "config", "osv", template.Target.OS, template.Target.Dist)
	if err := file.CopyDir(osvConfigSrcDir, osvConfigDestDir, "--preserve=mode", true); err != nil {
		log.Errorf("Failed to copy OSV config files to iso root: %v", err)
		return fmt.Errorf("failed to copy OSV config files to iso root: %w", err)
	}

	// Copy additional files to ISO and update path info
	var PathUpdatedList []config.AdditionalFileInfo
	additionalFiles := template.GetAdditionalFileInfo()
	if len(additionalFiles) != 0 {
		isoNames := make(map[string]string, len(additionalFiles))
		for _, fileInfo := range additionalFiles {
			srcFile := fileInfo.Local
			relName := isoAdditionalFileName(fileInfo.Local, fileInfo.Final)
			// Directory destinations can normalise onto another entry's name;
			// a second, different source would silently replace the first.
			if prev, ok := isoNames[relName]; ok && prev != srcFile {
				return fmt.Errorf("additional files %s and %s both map to %s on the ISO; "+
					"give them distinct destinations", prev, srcFile, relName)
			}
			isoNames[relName] = srcFile
			newPath := "../additionalfiles/" + relName
			dstFile := filepath.Join(osvConfigDestDir, "imageconfigs", "additionalfiles", relName)
			if err := file.CopyFile(srcFile, dstFile, "-p", true); err != nil {
				log.Errorf("Failed to copy additional file %s to image: %v", srcFile, err)
				return fmt.Errorf("failed to copy additional file %s to image: %w", srcFile, err)
			}
			newFileInfo := config.AdditionalFileInfo{
				Local: newPath,
				Final: fileInfo.Final,
				Stage: fileInfo.Stage,
			}
			PathUpdatedList = append(PathUpdatedList, newFileInfo)
		}
	}
	template.SystemConfig.AdditionalFiles = PathUpdatedList
	rewriteProvisioningSourcesForISO(template)

	// Dump updated template to ISO. The per-package SBOM metadata the installer
	// needs travels in a sidecar rather than inside this file, so the dump stays
	// a readable template instead of a template followed by thousands of lines
	// of package records.
	sbomMetadata := takeSBOMMetadata(template)

	defaultConfigsDir := filepath.Join(osvConfigDestDir, "imageconfigs", "defaultconfigs")
	templateDumpFilePath := filepath.Join(isoMaker.ImageBuildDir, "template-dump.yaml")
	if err := template.SaveUpdatedConfigFile(templateDumpFilePath); err != nil {
		log.Errorf("Failed to dump updated template to file: %v", err)
		return fmt.Errorf("failed to dump updated template to file: %w", err)
	}
	templateDestFilePath := filepath.Join(defaultConfigsDir, "template-dump.yaml")
	if err := file.CopyFile(templateDumpFilePath, templateDestFilePath, "--preserve=mode", true); err != nil {
		log.Errorf("Failed to copy template dump file to iso root: %v", err)
		return fmt.Errorf("failed to copy template dump file to iso root: %w", err)
	}

	sbomMetaSrcPath := filepath.Join(isoMaker.ImageBuildDir, config.SBOMMetadataFileName)
	if err := config.WriteSBOMMetadataSidecar(sbomMetaSrcPath, sbomMetadata); err != nil {
		log.Errorf("Failed to write SBOM metadata sidecar: %v", err)
		return fmt.Errorf("failed to write SBOM metadata sidecar: %w", err)
	}
	sbomMetaDestPath := filepath.Join(defaultConfigsDir, config.SBOMMetadataFileName)
	if err := file.CopyFile(sbomMetaSrcPath, sbomMetaDestPath, "--preserve=mode", true); err != nil {
		log.Errorf("Failed to copy SBOM metadata sidecar to iso root: %v", err)
		return fmt.Errorf("failed to copy SBOM metadata sidecar to iso root: %w", err)
	}

	return nil
}

// takeSBOMMetadata moves the per-package SBOM metadata off the template and
// returns it for the sidecar, leaving the template dump a plain template.
//
// sbomPackageMetadata is still a template field, so a build whose input already
// carries the block inline — rebuilding from a template-dump.yaml written by an
// earlier release, say — would have it written straight back out and the dump
// would be thousands of lines long despite the sidecar also being written.
// Clearing it is what keeps the dump short in that case.
//
// The inline block is also the payload of last resort: FullPkgListBom is not
// serialized, so loading such a template leaves it empty and the block is the
// only copy of the metadata left to hand to the sidecar.
func takeSBOMMetadata(template *config.ImageTemplate) []ospackage.PackageInfo {
	pkgs := template.FullPkgListBom
	if len(pkgs) == 0 {
		pkgs = template.SBOMPackageMetadata
	}
	template.SBOMPackageMetadata = nil
	return pkgs
}

// rewriteProvisioningSourcesForISO replaces the build-host paths that the
// provisioning sections still reference with what the installer can use. Their
// files already travel on the ISO as additionalFiles (see
// config.lowerProvisioningInputs), so the dump shipped on the ISO must not
// expose build-host directories: script sources point at the on-ISO copy,
// cloud-init file paths and SSH key files (already inlined) are dropped.
func rewriteProvisioningSourcesForISO(template *config.ImageTemplate) {
	sc := &template.SystemConfig
	for i := range sc.Provisioning.Scripts {
		script := &sc.Provisioning.Scripts[i]
		script.Local = "../additionalfiles/" + isoAdditionalFileName(script.Local, script.Final)
	}
	sc.CloudInit.UserDataFile = ""
	sc.CloudInit.MetaDataFile = ""
	sc.CloudInit.NetworkConfigFile = ""
	sc.CloudInit.ConfigFiles = nil
	for i := range sc.Users {
		sc.Users[i].SSHAuthorizedKeysFiles = nil
	}
}

// isoAdditionalFileName returns where an additional file is stored on the
// ISO, relative to its additionalfiles directory: a directory derived from
// the file's cleaned in-image destination, holding the file under its source
// basename. The installer copies the staged file to the destination with cp,
// so keeping the basename preserves cp's directory semantics ("final: /etc"
// yields /etc/<basename>, exactly as in a raw build), while the per-
// destination directory stops files that share a basename from overwriting
// each other. A hash names the directory because one destination may be
// another's parent ("/etc" and "/etc/motd"), which a mirrored tree cannot hold.
func isoAdditionalFileName(local, final string) string {
	sum := sha256.Sum256([]byte(filepath.Clean("/" + final)))
	return hex.EncodeToString(sum[:6]) + "/" + filepath.Base(local)
}

func (isoMaker *IsoMaker) copyImagePkgsToIso(template *config.ImageTemplate, installRoot string) error {
	pkgCacheSrcDir := isoMaker.ChrootEnv.GetChrootPkgCacheDir()
	pkgCacheDestDir := filepath.Join(installRoot, "cache-repo")
	for _, pkg := range template.FullPkgList {
		pkgFileSrcPath := filepath.Join(pkgCacheSrcDir, pkg)
		if _, err := os.Stat(pkgFileSrcPath); os.IsNotExist(err) {
			log.Errorf("Package file does not exist in cache: %s", pkgFileSrcPath)
			return fmt.Errorf("package file does not exist in cache: %s", pkgFileSrcPath)
		}
		pkgFileDestPath := filepath.Join(pkgCacheDestDir, pkg)
		if err := file.CopyFile(pkgFileSrcPath, pkgFileDestPath, "--preserve=mode", true); err != nil {
			log.Errorf("Failed to copy package file to iso cache-repo: %v", err)
			return fmt.Errorf("failed to copy package file to iso cache-repo: %w", err)
		}
	}

	pkgCacheChrootDir, err := isoMaker.ChrootEnv.GetChrootEnvPath(pkgCacheDestDir)
	if err != nil {
		return fmt.Errorf("failed to get chroot path for iso cache-repo: %w", err)
	}

	if err := isoMaker.ChrootEnv.UpdateChrootLocalRepoMetadata(pkgCacheChrootDir, template.Target.Arch, true); err != nil {
		return fmt.Errorf("failed to update local cache repository metadata in iso: %w", err)
	}

	return nil
}

func (isoMaker *IsoMaker) createIso(template *config.ImageTemplate, initrdRootfsPath, initrdFilePath, isoFilePath string) error {
	var err error

	log.Infof("Creating ISO image...")

	installRoot := isoMaker.ImageOs.GetInstallRoot()
	imageName := template.GetImageName()

	log.Infof("Creating ISO image: %s", isoFilePath)

	// Create standard ISO directory structure
	isoBootPath := filepath.Join(installRoot, "boot")
	isoEfiPath := filepath.Join(installRoot, "EFI", "BOOT")
	isoImagesPath := filepath.Join(installRoot, "images")

	dirs := []string{
		isoBootPath,
		isoEfiPath,
		isoImagesPath,
	}

	log.Infof("Creating ISO directory structure...")
	for _, dir := range dirs {
		if _, err := shell.ExecCmd("mkdir -p "+dir, true, shell.HostPath, nil); err != nil {
			log.Errorf("Failed to create directory %s: %v", dir, err)
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Copy kernel and initrd
	log.Infof("Copying kernel and initrd files...")
	if err := copyKernelToIsoImagesPath(initrdRootfsPath, isoImagesPath); err != nil {
		return fmt.Errorf("failed to copy kernel to iso image path: %w", err)
	}

	if err := copyInitrdToIsoImagesPath(initrdFilePath, isoImagesPath); err != nil {
		return fmt.Errorf("failed to copy initrd to iso image path: %w", err)
	}

	// Copy config files to ISO
	log.Infof("Copying config files to ISO...")
	if err := isoMaker.copyConfigFilesToIso(template, installRoot); err != nil {
		return fmt.Errorf("failed to copy config files to ISO: %w", err)
	}

	// Copy image packages to ISO
	log.Infof("Copying image packages to ISO...")
	if err := isoMaker.copyImagePkgsToIso(template, installRoot); err != nil {
		return fmt.Errorf("failed to copy image packages to ISO: %w", err)
	}

	// Create GRUB config for EFI boot
	if err := createGrubCfg(installRoot, imageName); err != nil {
		return fmt.Errorf("failed to create GRUB configuration: %w", err)
	}

	// Copy GRUB files to ISO boot path
	log.Infof("Copying GRUB files to ISO boot path...")
	if err := copyGrubFilesToGrubPath(initrdRootfsPath, installRoot); err != nil {
		return fmt.Errorf("failed to copy GRUB files to ISO boot path: %w", err)
	}

	log.Infof("Creating EFI FAT image...")
	efiFatImgPath, err := createEfiFatImage(template, initrdRootfsPath, installRoot)
	if err != nil {
		return fmt.Errorf("failed to create EFI FAT image: %w", err)
	}
	efiFatImgRelPath := strings.TrimPrefix(efiFatImgPath, installRoot)

	log.Infof("Creating image for Bios boot...")
	biosImgRelPath, err := createBiosImage(template, initrdRootfsPath, installRoot)
	if err != nil {
		return fmt.Errorf("failed to create BIOS image: %w", err)
	}

	// Create ISO image with xorriso
	log.Infof("Creating ISO image with xorriso...")
	if biosImgRelPath != "" {
		log.Infof("Creating hybrid ISO for both BIOS and UEFI boot modes...")
	} else {
		log.Infof("Creating ISO for UEFI boot mode only...")
	}
	xorrisoCmd := buildXorrisoCommand(xorrisoArgs{
		installRoot:      installRoot,
		isoFilePath:      isoFilePath,
		efiFatImgPath:    efiFatImgPath,
		efiFatImgRelPath: efiFatImgRelPath,
		biosImgRelPath:   biosImgRelPath,
	})

	if _, err := shell.ExecCmdWithStream(xorrisoCmd, true, shell.HostPath, nil); err != nil {
		log.Errorf("Failed to create ISO image: %v", err)
		return fmt.Errorf("failed to create ISO image: %w", err)
	}

	if err := cleanIsoInstallRoot(installRoot); err != nil {
		return fmt.Errorf("failed to clean up ISO install root: %w", err)
	}

	log.Infof("ISO creation completed successfully")
	return nil
}

// xorrisoArgs holds the paths needed to assemble the xorriso command line.
// An empty biosImgRelPath selects the UEFI-only layout.
type xorrisoArgs struct {
	installRoot      string
	isoFilePath      string
	efiFatImgPath    string
	efiFatImgRelPath string
	biosImgRelPath   string
}

// buildXorrisoCommand assembles the xorriso command line. It does no I/O so
// the exact invocation can be unit-tested. -iso-level 3 lifts the ISO9660
// 4 GiB single-file limit, so large additional files (e.g. source archives)
// can be carried on the ISO.
func buildXorrisoCommand(args xorrisoArgs) string {
	q := shell.QuoteArg
	var cmd string
	if args.biosImgRelPath != "" {
		biosImgRelDir := filepath.Dir(args.biosImgRelPath)
		cmd = fmt.Sprintf("xorriso -as mkisofs -iso-level 3 -graft-points -r -J -l -b %s", q(args.biosImgRelPath))
		cmd += " -no-emul-boot -boot-load-size 4 -boot-info-table --grub2-boot-info"
		cmd += fmt.Sprintf(" --grub2-mbr %s", q(filepath.Join(args.installRoot, biosImgRelDir, "boot_hybrid.img")))
		cmd += fmt.Sprintf(" -eltorito-alt-boot -e %s -no-emul-boot", q(args.efiFatImgRelPath))
		cmd += fmt.Sprintf(" -append_partition 2 0xef %s -appended_part_as_gpt", q(args.efiFatImgPath))
		cmd += fmt.Sprintf(" -r %s --sort-weight 0 / --sort-weight 1 /boot", q(args.installRoot))
		cmd += fmt.Sprintf(" -volid %s --protective-msdos-label -o %s %s",
			q(IsoLabel), q(args.isoFilePath), q(args.installRoot))
		return cmd
	}
	cmd = fmt.Sprintf("xorriso -as mkisofs -iso-level 3 -graft-points -r -J -l --efi-boot %s", q(args.efiFatImgPath))
	cmd += " -efi-boot-part --efi-boot-image --protective-msdos-label"
	cmd += fmt.Sprintf(" -r %s --sort-weight 0 / --sort-weight 1 /boot", q(args.installRoot))
	cmd += fmt.Sprintf(" -volid %s -o %s %s", q(IsoLabel), q(args.isoFilePath), q(args.installRoot))
	return cmd
}

func copyKernelToIsoImagesPath(initrdRootfsPath, isoImagesPath string) error {
	// Copy kernel to iso image path
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
	kernelDestPath := filepath.Join(isoImagesPath, "vmlinuz")
	if err := file.CopyFile(kernelPath, kernelDestPath, "--preserve=mode", true); err != nil {
		log.Errorf("Failed to copy kernel to iso image path: %v", err)
		return fmt.Errorf("failed to copy kernel to iso image path: %w", err)
	}
	return nil
}

func copyInitrdToIsoImagesPath(initrdFilePath, isoImagesPath string) error {
	// Copy initrd image to iso image path
	initrdDestPath := filepath.Join(isoImagesPath, "initrd.img")
	if err := file.CopyFile(initrdFilePath, initrdDestPath, "--preserve=mode", true); err != nil {
		log.Errorf("Failed to copy initrd image to iso image path: %v", err)
		return fmt.Errorf("failed to copy initrd image to iso image path: %w", err)
	}
	return nil
}

func createGrubCfg(installRoot, imageName string) error {
	log.Infof("Creating GRUB configuration for EFI boot...")

	generalConfigDir, err := config.GetGeneralConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get general config directory: %w", err)
	}
	grubCfgSrc := filepath.Join(generalConfigDir, "isolinux", "grub.cfg")
	if _, err := os.Stat(grubCfgSrc); os.IsNotExist(err) {
		log.Errorf("grub.cfg file does not exist: %s", grubCfgSrc)
		return fmt.Errorf("grub.cfg file does not exist: %s", grubCfgSrc)
	}

	grubCfgDest := filepath.Join(installRoot, "boot", "grub", "grub.cfg")
	if err := file.CopyFile(grubCfgSrc, grubCfgDest, "--preserve=mode", true); err != nil {
		log.Errorf("Failed to copy grub.cfg to install root: %v", err)
		return fmt.Errorf("failed to copy grub.cfg to install root: %w", err)
	}

	if err := file.ReplacePlaceholdersInFile("{{.ImageName}}", imageName, grubCfgDest); err != nil {
		log.Errorf("Failed to replace ImageName in grub configuration: %v", err)
		return fmt.Errorf("failed to replace ImageName in grub configuration: %w", err)
	}

	grubCfgSrc = grubCfgDest
	grubCfgDest = filepath.Join(installRoot, "EFI", "BOOT", "grub.cfg")
	if err := file.CopyFile(grubCfgSrc, grubCfgDest, "--preserve=mode", true); err != nil {
		log.Errorf("Failed to copy grub.cfg to install root: %v", err)
		return fmt.Errorf("failed to copy grub.cfg to install root: %w", err)
	}

	return nil
}

func copyGrubFilesToGrubPath(initrdRootfsPath, installRoot string) error {

	// Copy GRUB locale files if exists
	localSrcDir := filepath.Join(initrdRootfsPath, "usr", "share", "locale")
	localLangpackDir := filepath.Join(initrdRootfsPath, "usr", "share", "locale-langpack")
	for _, dir := range []string{localSrcDir, localLangpackDir} {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			log.Warnf("GRUB locale directory does not exist: %s", dir)
		} else {
			// traverse locale directory and copy only language directories
			entries, err := os.ReadDir(dir)
			if err != nil {
				log.Errorf("Failed to read GRUB locale directory: %v", err)
				return fmt.Errorf("failed to read GRUB locale directory: %w", err)
			}
			for _, entry := range entries {
				if entry.IsDir() {
					langDir := entry.Name()
					langSrc := filepath.Join(dir, langDir, "LC_MESSAGES", "grub.mo")
					if _, err := os.Stat(langSrc); os.IsNotExist(err) {
						continue
					}
					langDest := filepath.Join(installRoot, "boot", "grub", "locale", fmt.Sprintf("%s.mo", langDir))
					if err := file.CopyFile(langSrc, langDest, "--preserve=mode", true); err != nil {
						log.Errorf("Failed to copy GRUB locale file to iso boot dir: %v", err)
						return fmt.Errorf("failed to copy GRUB locale file to iso boot dir: %w", err)
					}
				}
			}
		}
	}

	// Copy GRUB font file if exists
	fontsSrc := filepath.Join(initrdRootfsPath, "usr", "share", "grub", "unicode.pf2")
	if _, err := os.Stat(fontsSrc); os.IsNotExist(err) {
		log.Warnf("GRUB font file does not exist: %s", fontsSrc)
	} else {
		fontsDest := filepath.Join(installRoot, "boot", "grub", "fonts", "unicode.pf2")
		if err := file.CopyFile(fontsSrc, fontsDest, "--preserve=mode", true); err != nil {
			log.Errorf("Failed to copy GRUB font file to iso boot dir: %v", err)
			return fmt.Errorf("failed to copy GRUB font file to iso boot dir: %w", err)
		}
	}

	return nil
}

func createBiosImage(template *config.ImageTemplate, initrdRootfsPath, installRoot string) (biosImgRelPath string, err error) {
	target := template.GetTargetInfo()
	switch target.Arch {
	case "x86_64", "i386":
		format := "i386-pc"
		prefixDir := "/boot/grub/"

		generalConfigDir, err := config.GetGeneralConfigDir()
		if err != nil {
			return "", fmt.Errorf("failed to get general config directory: %w", err)
		}
		loadCfgSrc := filepath.Join(generalConfigDir, "isolinux", "load.cfg")
		if _, err := os.Stat(loadCfgSrc); os.IsNotExist(err) {
			log.Errorf("load.cfg file does not exist: %s", loadCfgSrc)
			return "", fmt.Errorf("load.cfg file does not exist: %s", loadCfgSrc)
		}

		grubLibDir := filepath.Join(initrdRootfsPath, "usr", "lib", "grub", format)
		if _, err := os.Stat(grubLibDir); os.IsNotExist(err) {
			log.Debugf("GRUB modules directory does not exist: %s, skip BIOS boot enabling", grubLibDir)
			return "", nil
		}

		bootGrubLibDir := filepath.Join(installRoot, "boot", "grub", format)
		if err = file.CopyDir(grubLibDir, bootGrubLibDir, "--preserve=mode", true); err != nil {
			log.Errorf("Failed to copy grub modules to iso boot dir: %v", err)
			return "", fmt.Errorf("failed to copy grub modules to iso boot dir: %w", err)
		}

		biosImgRelPath = filepath.Join("boot", "grub", format, "eltorito.img")
		biosImgPath := filepath.Join(installRoot, biosImgRelPath)

		grubmkCmd := fmt.Sprintf("grub-mkimage --format=%s-eltorito --output=%s", format, biosImgPath)
		grubmkCmd += fmt.Sprintf(" --config=%s --directory=%s --prefix=%s", loadCfgSrc, grubLibDir, prefixDir)
		grubmkCmd += " biosdisk iso9660"

		if _, err := shell.ExecCmd(grubmkCmd, true, shell.HostPath, nil); err != nil {
			log.Errorf("Failed to create eltorito image: %v", err)
			return biosImgRelPath, fmt.Errorf("failed to create eltorito image: %w", err)
		}

	default:
		log.Debugf("BIOS boot not supported for architecture: %s", target.Arch)
		return "", nil
	}
	return biosImgRelPath, nil
}

func createEfiFatImage(template *config.ImageTemplate, initrdRootfsPath, installRoot string) (efiFatImgPath string, err error) {
	target := template.GetTargetInfo()
	switch target.Arch {
	case "x86_64":
		format := "x86_64-efi"
		prefixDir := "/boot/grub"

		generalConfigDir, err := config.GetGeneralConfigDir()
		if err != nil {
			return "", fmt.Errorf("failed to get general config directory: %w", err)
		}
		loadCfgSrc := filepath.Join(generalConfigDir, "isolinux", "load.cfg")
		if _, err := os.Stat(loadCfgSrc); os.IsNotExist(err) {
			log.Errorf("load.cfg file does not exist: %s", loadCfgSrc)
			return "", fmt.Errorf("load.cfg file does not exist: %s", loadCfgSrc)
		}

		grubLibDir := filepath.Join(initrdRootfsPath, "usr", "lib", "grub", format)
		if _, err := os.Stat(grubLibDir); os.IsNotExist(err) {
			log.Errorf("GRUB modules directory does not exist: %s", grubLibDir)
			return "", fmt.Errorf("GRUB modules directory does not exist: %s", grubLibDir)
		}

		bootGrubLibDir := filepath.Join(installRoot, "boot", "grub", format)
		if err = file.CopyDir(grubLibDir, bootGrubLibDir, "--preserve=mode", true); err != nil {
			log.Errorf("Failed to copy grub modules to iso boot dir: %v", err)
			return "", fmt.Errorf("failed to copy grub modules to iso boot dir: %w", err)
		}

		efiFatImgPath = filepath.Join(installRoot, prefixDir, "efi.img")
		cmdStr := fmt.Sprintf("mformat -C -f 2880 -L 16 -i %s ::.", efiFatImgPath)
		if _, err := shell.ExecCmd(cmdStr, true, shell.HostPath, nil); err != nil {
			log.Errorf("Failed to create EFI FAT image: %v", err)
			return efiFatImgPath, fmt.Errorf("failed to create EFI FAT image: %w", err)
		}

		efiDirPath := filepath.Join(installRoot, "EFI")
		efiImgPath := filepath.Join(efiDirPath, "BOOT", "BOOTX64.EFI")

		grubmkCmd := fmt.Sprintf("grub-mkimage --format=%s --output=%s", format, efiImgPath)
		grubmkCmd += fmt.Sprintf(" --config=%s --directory=%s --prefix=%s", loadCfgSrc, grubLibDir, prefixDir)
		grubmkCmd += " part_gpt part_msdos fat ext2 ntfs search iso9660"

		if _, err := shell.ExecCmd(grubmkCmd, true, shell.HostPath, nil); err != nil {
			log.Errorf("Failed to create EFI image: %v", err)
			return efiFatImgPath, fmt.Errorf("failed to create EFI image: %w", err)
		}

		cmdStr = fmt.Sprintf("mcopy -s -i %s %s ::/.", efiFatImgPath, efiDirPath)
		if _, err := shell.ExecCmd(cmdStr, true, shell.HostPath, nil); err != nil {
			log.Errorf("Failed to copy EFI files to FAT image: %v", err)
			return efiFatImgPath, fmt.Errorf("failed to copy EFI files to FAT image: %w", err)
		}

	default:
		return "", fmt.Errorf("unsupported architecture for EFI FAT image: %s", target.Arch)
	}

	return efiFatImgPath, nil
}

func cleanIsoInstallRoot(installRoot string) error {
	log.Infof("Cleaning up ISO workspace: %s", installRoot)

	// Remove the entire image build directory
	if _, err := shell.ExecCmd("rm -rf "+installRoot, true, shell.HostPath, nil); err != nil {
		log.Errorf("Failed to remove iso installRoot directory %s: %v", installRoot, err)
		return fmt.Errorf("failed to remove iso installRoot directory %s: %w", installRoot, err)
	}

	return nil
}
