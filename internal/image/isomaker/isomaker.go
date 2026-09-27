package isomaker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/chroot"
	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/config/manifest"
	"github.com/open-edge-platform/image-composer-tool/internal/image/imagedisc"
	"github.com/open-edge-platform/image-composer-tool/internal/image/imageos"
	"github.com/open-edge-platform/image-composer-tool/internal/image/initrdmaker"
	"github.com/open-edge-platform/image-composer-tool/internal/image/rawmaker"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/file"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/slice"
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
	RawMaker      rawmaker.RawMakerInterface // nil unless systemConfig.installerPayload.enabled

	// Populated by buildPayloadRaw; paths of the staged payload files in
	// ImageBuildDir/payload/, grafted onto the ISO by payloadGraftPathspecs.
	payloadCompressedPath string
	payloadManifestPath   string
	payloadSBOMPath       string
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

	if isoMaker.template.IsInstallerPayloadMode() {
		if err := isoMaker.buildPayloadRaw(); err != nil {
			return fmt.Errorf("failed to build installer payload: %w", err)
		}
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

	initrdRootfsPath := isoMaker.InitrdMaker.GetInitrdRootfsPath()
	initrdFilePath := isoMaker.InitrdMaker.GetInitrdFilePath()
	if err := isoMaker.createIso(isoMaker.template, initrdRootfsPath, initrdFilePath, isoFilePath); err != nil {
		return fmt.Errorf("failed to create ISO image: %w", err)
	}

	// Copy SBOM to image build directory
	if err := manifest.CopySBOMToImageBuildDir(isoMaker.ImageBuildDir); err != nil {
		log.Warnf("Failed to copy SBOM to image build directory: %v", err)
		// Don't fail the build if SBOM copy fails, just log warning
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
	if err := ValidateAdditionalFiles(template); err != nil {
		return fmt.Errorf("main template prerequisites not met: %w", err)
	}

	initrdTemplateFilePath, err := template.GetInitramfsTemplate()
	if err != nil {
		return fmt.Errorf("failed to resolve initramfs template: %w", err)
	}

	initrdTemplate, err := config.LoadAndMergeTemplate(initrdTemplateFilePath)
	if err != nil {
		return fmt.Errorf("failed to load initrd template for validation: %w", err)
	}

	if err := ValidateAdditionalFiles(initrdTemplate); err != nil {
		return fmt.Errorf("initrd template prerequisites not met: %w", err)
	}

	if template.IsInstallerPayloadMode() {
		if err := validateInstallerPayloadPrerequisites(template, initrdTemplate); err != nil {
			return err
		}
	}

	return nil
}

// rpmFamilyOS names the target.os values whose package manager is rpm-based
// (dnf/tdnf/yum), matching the per-provider host-dependency maps in
// internal/provider/{azl,emt,rcd}. Every other target.os (ubuntu, debian,
// wind-river-elxr) is apt-based.
var rpmFamilyOS = map[string]bool{
	"azure-linux":              true,
	"edge-microvisor-toolkit":  true,
	"redhat-compatible-distro": true,
}

// payloadDecompressorPackage returns the package that must be present in the
// initrd rootfs to decompress systemConfig.installerPayload.compression at
// deploy time. "none" needs no decompressor since dd reads the raw image
// directly. The xz package is named differently across package managers
// (xz-utils on apt, xz on rpm), so it is resolved per target.os rather than
// hard-coded to one family.
func payloadDecompressorPackage(targetOS, compression string) string {
	switch compression {
	case config.PayloadCompressionZstd:
		return "zstd"
	case config.PayloadCompressionXz:
		if rpmFamilyOS[targetOS] {
			return "xz"
		}
		return "xz-utils"
	case config.PayloadCompressionGz:
		return "gzip"
	default:
		return ""
	}
}

// validateInstallerPayloadPrerequisites checks the payload-mode-specific
// prerequisites that the JSON schema and validateInstallerPayload (a pure
// config-level check) cannot: disk.size must be set (rawmaker needs it and the
// ISO OS defaults do not supply one), the initrd must actually boot the
// unattended installer path, the initrd rootfs must carry the decompressor
// package for the configured compression, and there must be enough free
// space to hold the payload raw and compressed copies side-by-side.
func validateInstallerPayloadPrerequisites(template, initrdTemplate *config.ImageTemplate) error {
	diskCfg := template.GetDiskConfig()
	if strings.TrimSpace(diskCfg.Size) == "" {
		return fmt.Errorf("systemConfig.installerPayload requires disk.size to be set")
	}

	rawSizeBytes, err := imagedisc.TranslateSizeStrToBytes(diskCfg.Size)
	if err != nil {
		return fmt.Errorf("invalid disk.size %q for installer payload: %w", diskCfg.Size, err)
	}

	if err := validateInstallerPayloadInitrd(initrdTemplate); err != nil {
		return err
	}

	compression := template.PayloadCompression()
	if decompressorPkg := payloadDecompressorPackage(template.Target.OS, compression); decompressorPkg != "" {
		if !slice.Contains(initrdTemplate.GetPackages(), decompressorPkg) {
			return fmt.Errorf("systemConfig.installerPayload.compression %q requires package %q in the initrd template",
				compression, decompressorPkg)
		}
	}

	if err := validateInstallerPayloadDiskLayout(diskCfg, template.ResetInstanceIdentity()); err != nil {
		return err
	}

	if err := validateInstallerPayloadDeployTools(template.Target.OS, diskCfg, initrdTemplate); err != nil {
		return err
	}

	workDir, err := config.WorkDir()
	if err != nil {
		return fmt.Errorf("failed to resolve work directory for installer payload disk space check: %w", err)
	}
	// Three payload-sized copies can coexist in the workspace at once: the
	// raw image, its compressed copy (graft-pointed, not further copied, but
	// no smaller than the raw image when compression is "none"), and the
	// final .iso itself, which xorriso writes as a new file embedding that
	// compressed copy. Size for the worst case since the actual compressed
	// size isn't known until compression runs.
	requiredBytes := int64(rawSizeBytes) * 3
	if err := file.CheckDiskSpace(workDir, requiredBytes, 0.1); err != nil {
		return fmt.Errorf("installer payload disk space check failed: %w", err)
	}

	// Growth of the end: "0" partition (required by validateInstallerPayloadDiskLayout
	// below) is mandatory - there is no way to opt out of installer-side growth -
	// and dm-verity's hash tree cannot tolerate the root partition/filesystem
	// changing after the build, so the two are never compatible; reject rather
	// than warn.
	if template.IsImmutabilityEnabled() {
		return fmt.Errorf("systemConfig.installerPayload is not supported with systemConfig.immutability enabled: " +
			"dm-verity root protection is incompatible with installer-side partition/filesystem growth")
	}

	return nil
}

// attendedInstallerStartupScript is the fixed root startupScript wired by the
// repo's attended initrd templates (e.g. default-initrd-x86_64.yml) via
// config/general/isolinux/attendedinstaller.
const attendedInstallerStartupScript = "/root/attendedinstaller"

// unattendedInstallerStartupScript is the fixed root startupScript wired by
// the repo's unattended initrd templates (e.g.
// default-initrd-unattended-x86_64.yml) via
// config/general/isolinux/unattendedinstaller - the only entry point that
// actually invokes live-installer's unattended path, which is in turn the
// only path that runs deployInstallerPayload (cmd/live-installer/main.go).
const unattendedInstallerStartupScript = "/root/unattendedinstaller"

// validateInstallerPayloadInitrd requires an initrd template that boots
// straight into live-installer's unattended path. A negative check (merely
// rejecting the known attended script) is not enough: an initrd with no root
// startup script, or an unrelated one, would build and boot successfully
// without ever invoking live-installer, so the payload would never be
// deployed and attendedInstall's own installerPayload rejection
// (cmd/live-installer/install.go) would never even be reached.
func validateInstallerPayloadInitrd(initrdTemplate *config.ImageTemplate) error {
	for _, u := range initrdTemplate.GetUsers() {
		if u.Name != "root" {
			continue
		}
		if u.StartupScript != unattendedInstallerStartupScript {
			return fmt.Errorf("systemConfig.installerPayload requires an unattended initrd template: "+
				"this initrd's root startupScript is %q, but only %q boots live-installer's unattended path",
				u.StartupScript, unattendedInstallerStartupScript)
		}
		return nil
	}
	return fmt.Errorf("systemConfig.installerPayload requires an unattended initrd template: "+
		"no root user with startupScript %q is configured", unattendedInstallerStartupScript)
}

// deployToolPackage names the additional package (beyond util-linux and the
// OS-specific sfdisk provider, see sfdiskProvidingPackage) that must be
// present in the initrd rootfs to grow the growable partition's filesystem
// at deploy time. Unlike the xz decompressor, these package names are
// already used identically across every provider's own default initrd
// config in this repo (e2fsprogs and xfsprogs are the same literal package
// name on both apt- and rpm-based providers here), so no per-OS resolution
// is needed.
var deployToolPackage = map[string]string{
	"ext2": "e2fsprogs",
	"ext3": "e2fsprogs",
	"ext4": "e2fsprogs",
	"xfs":  "xfsprogs",
}

// sfdiskProvidingPackage names the package that ships sfdisk for targetOS.
// util-linux still bundles sfdisk on rpm-based distros, but Debian-family
// util-linux split sfdisk (and fdisk/cfdisk) out into the separate "fdisk"
// package, so a Debian-family initrd with only util-linux has lsblk/wipefs/
// partx/mkswap but not sfdisk.
func sfdiskProvidingPackage(targetOS string) string {
	if rpmFamilyOS[targetOS] {
		return "util-linux"
	}
	return "fdisk"
}

// validateInstallerPayloadDeployTools checks that the initrd rootfs carries
// every package live-installer's deploy path unconditionally needs
// (util-linux, for lsblk/wipefs/partx/mkswap, plus the OS-specific sfdisk
// provider) plus whichever resize tool the disk layout's growable partition
// requires, so a custom or non-Ubuntu initrd that only added the
// decompressor package still fails template validation instead of failing
// after the target disk has already been overwritten. Called after
// validateInstallerPayloadDiskLayout, which guarantees diskCfg has exactly
// one growable partition with a supported fsType. dd, sync (coreutils) and
// udevadm (systemd) are not checked: they are part of every base rootfs this
// repo builds, not an optional package a template could omit.
func validateInstallerPayloadDeployTools(targetOS string, diskCfg config.DiskConfig, initrdTemplate *config.ImageTemplate) error {
	initrdPackages := initrdTemplate.GetPackages()
	if !slice.Contains(initrdPackages, "util-linux") {
		return fmt.Errorf("systemConfig.installerPayload deploy requires package %q "+
			"(lsblk/wipefs/partx/mkswap) in the initrd template", "util-linux")
	}

	if sfdiskPkg := sfdiskProvidingPackage(targetOS); !slice.Contains(initrdPackages, sfdiskPkg) {
		return fmt.Errorf("systemConfig.installerPayload deploy requires package %q (sfdisk) in the initrd template for target.os %q",
			sfdiskPkg, targetOS)
	}

	for i := range diskCfg.Partitions {
		p := &diskCfg.Partitions[i]
		if strings.TrimSpace(p.End) != "0" {
			continue
		}
		fsType := strings.ToLower(strings.TrimSpace(p.FsType))
		if pkg, ok := deployToolPackage[fsType]; ok && !slice.Contains(initrdPackages, pkg) {
			return fmt.Errorf("systemConfig.installerPayload growing a %q partition at deploy time requires package %q "+
				"in the initrd template", p.FsType, pkg)
		}
		break
	}

	return nil
}

// growableFsTypes are the only fsTypes resizeFilesystem
// (cmd/live-installer/deploy.go) actually grows post-write. Any other fsType
// on the growable partition (fat32/fat16/vfat, btrfs, or unknown) would
// silently no-op there, reporting a successful deploy with an unexpanded
// filesystem.
var growableFsTypes = []string{"ext2", "ext3", "ext4", "xfs", "linux-swap", "swap"}

// identityRelevantMountPointPrefixes are the top-level directories
// resetIdentityFiles (cmd/live-installer/deploy.go) edits directly on the
// single partition it mounts at "/": etc/machine-id, etc/ssh/ssh_host_*,
// var/lib/dbus/machine-id, var/lib/systemd/random-seed. A layout with a
// separate partition mounted at or under one of these would leave those
// files untouched on the actual target while resetInstanceIdentity reports
// success.
var identityRelevantMountPointPrefixes = []string{"/etc", "/var"}

func isIdentityRelevantMountPoint(mountPoint string) bool {
	mountPoint = strings.TrimSpace(mountPoint)
	for _, prefix := range identityRelevantMountPointPrefixes {
		if mountPoint == prefix || strings.HasPrefix(mountPoint, prefix+"/") {
			return true
		}
	}
	return false
}

// validateInstallerPayloadDiskLayout rejects disk layouts that live-installer's
// deploy path cannot safely apply. Each of these currently fails only after
// the payload has already been written to the target disk (silently, for MBR
// growth, or via a downstream command error for the rest), so catching them
// here means a bad template fails before any target disk write is attempted.
func validateInstallerPayloadDiskLayout(diskCfg config.DiskConfig, resetInstanceIdentity bool) error {
	if strings.ToLower(strings.TrimSpace(diskCfg.PartitionTableType)) != imagedisc.PartitionTableTypeGpt {
		return fmt.Errorf("systemConfig.installerPayload requires disk.partitionTableType %q: "+
			"MBR payload disks are not grown to fill the target disk", imagedisc.PartitionTableTypeGpt)
	}

	var growable *config.PartitionInfo
	hasRoot := false
	for i := range diskCfg.Partitions {
		p := &diskCfg.Partitions[i]
		if strings.EqualFold(p.Type, "linux-lvm") {
			return fmt.Errorf("systemConfig.installerPayload does not support LVM partitions (partition %q): "+
				"live-installer cannot grow or mount an LVM logical volume", p.ID)
		}
		if strings.TrimSpace(p.End) == "0" {
			growable = p
		}
		if p.MountPoint == "/" {
			hasRoot = true
		} else if resetInstanceIdentity && isIdentityRelevantMountPoint(p.MountPoint) {
			return fmt.Errorf("systemConfig.installerPayload.resetInstanceIdentity requires machine-id, SSH host keys, "+
				"and the systemd random seed to live on the root partition, but partition %q mounts %q separately",
				p.ID, p.MountPoint)
		}
	}

	if growable == nil {
		return fmt.Errorf(`systemConfig.installerPayload requires a partition with end: "0" to grow into the target disk`)
	}
	growableFsType := strings.ToLower(strings.TrimSpace(growable.FsType))
	if !slice.Contains(growableFsTypes, growableFsType) {
		return fmt.Errorf("systemConfig.installerPayload does not support growing a %q partition (partition %q): "+
			"only %v are grown", growable.FsType, growable.ID, growableFsTypes)
	}

	if resetInstanceIdentity && !hasRoot {
		return fmt.Errorf(`systemConfig.installerPayload.resetInstanceIdentity requires a partition with mountPoint: "/"`)
	}

	return nil
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

		candidatePath, found := config.ResolveTemplateRelativePath(template.PathList, fileInfo.Local)
		if !found {
			return liveInstallerHintOrError(fileInfo.Local, fileInfo.Final, nil)
		}
		if err := checkFileExists(candidatePath); err != nil {
			return liveInstallerHintOrError(fileInfo.Local, fileInfo.Final, err)
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
		seenBasenames := make(map[string]string, len(additionalFiles))
		for _, fileInfo := range additionalFiles {
			srcFile := fileInfo.Local
			srcFileName := filepath.Base(srcFile)
			// mergeAdditionalFiles keys by Final path, so the same Local source
			// intentionally installed at two different Final destinations is a
			// valid, supported pattern - only two *different* sources sharing a
			// basename would actually clobber each other's staged copy.
			if prior, exists := seenBasenames[srcFileName]; exists && prior != srcFile {
				return fmt.Errorf("additional files %s and %s both resolve to basename %q on the ISO; "+
					"rename one to avoid clobbering the other", prior, srcFile, srcFileName)
			}
			seenBasenames[srcFileName] = srcFile
			newPath := fmt.Sprintf("../additionalfiles/%s", srcFileName)
			dstFile := filepath.Join(osvConfigDestDir, "imageconfigs", "additionalfiles", srcFileName)
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
	template.SBOMPackageMetadata = template.FullPkgListBom

	// Dump updated template to ISO
	templateDumpFilePath := filepath.Join(isoMaker.ImageBuildDir, "template-dump.yaml")
	if err := template.SaveUpdatedConfigFile(templateDumpFilePath); err != nil {
		log.Errorf("Failed to dump updated template to file: %v", err)
		return fmt.Errorf("failed to dump updated template to file: %w", err)
	}
	templateDestFilePath := filepath.Join(osvConfigDestDir, "imageconfigs", "defaultconfigs", "template-dump.yaml")
	if err := file.CopyFile(templateDumpFilePath, templateDestFilePath, "--preserve=mode", true); err != nil {
		log.Errorf("Failed to copy template dump file to iso root: %v", err)
		return fmt.Errorf("failed to copy template dump file to iso root: %w", err)
	}

	return nil
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

	// In payload mode, rawmaker's ImageOs and isoMaker's ImageOs share the
	// same systemConfig.name and therefore the same installRoot
	// (imageos.NewImageOs). Staging the ISO tree there would let
	// cleanIsoInstallRoot's rm -rf race the payload raw build's own mount
	// point, so stage into a derived sibling directory instead.
	installRoot := isoMaker.ImageOs.GetInstallRoot()
	isoRoot := installRoot
	if template.IsInstallerPayloadMode() {
		isoRoot = installRoot + "-isoroot"
		// A previous build attempt may have failed before cleanIsoInstallRoot
		// ran, leaving stale files under isoRoot that a template change no
		// longer produces. Clear it so every build starts from an empty tree.
		if err := os.RemoveAll(isoRoot); err != nil {
			return fmt.Errorf("failed to clear stale ISO staging root %s: %w", isoRoot, err)
		}
		if err := os.MkdirAll(isoRoot, 0755); err != nil {
			return fmt.Errorf("failed to create ISO staging root %s: %w", isoRoot, err)
		}
	}
	imageName := template.GetImageName()

	log.Infof("Creating ISO image: %s", isoFilePath)

	// Create standard ISO directory structure
	isoBootPath := filepath.Join(isoRoot, "boot")
	isoEfiPath := filepath.Join(isoRoot, "EFI", "BOOT")
	isoImagesPath := filepath.Join(isoRoot, "images")

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
	if err := isoMaker.copyConfigFilesToIso(template, isoRoot); err != nil {
		return fmt.Errorf("failed to copy config files to ISO: %w", err)
	}

	// Copy image packages to ISO. Skipped in payload mode: live-installer's
	// deploy path writes the payload raw image directly and never calls
	// InstallImageOs, so a cache-repo of every resolved .deb is dead weight.
	if !template.IsInstallerPayloadMode() {
		log.Infof("Copying image packages to ISO...")
		if err := isoMaker.copyImagePkgsToIso(template, isoRoot); err != nil {
			return fmt.Errorf("failed to copy image packages to ISO: %w", err)
		}
	}

	// Create GRUB config for EFI boot
	if err := createGrubCfg(isoRoot, imageName); err != nil {
		return fmt.Errorf("failed to create GRUB configuration: %w", err)
	}

	// Copy GRUB files to ISO boot path
	log.Infof("Copying GRUB files to ISO boot path...")
	if err := copyGrubFilesToGrubPath(initrdRootfsPath, isoRoot); err != nil {
		return fmt.Errorf("failed to copy GRUB files to ISO boot path: %w", err)
	}

	log.Infof("Creating EFI FAT image...")
	efiFatImgPath, err := createEfiFatImage(template, initrdRootfsPath, isoRoot)
	if err != nil {
		return fmt.Errorf("failed to create EFI FAT image: %w", err)
	}
	efiFatImgRelPath := strings.TrimPrefix(efiFatImgPath, isoRoot)

	log.Infof("Creating image for Bios boot...")
	biosImgRelPath, err := createBiosImage(template, initrdRootfsPath, isoRoot)
	if err != nil {
		return fmt.Errorf("failed to create BIOS image: %w", err)
	}

	// Create ISO image with xorriso
	log.Infof("Creating ISO image with xorriso...")
	var graftPathspecs []string
	if template.IsInstallerPayloadMode() {
		graftPathspecs = isoMaker.payloadGraftPathspecs()
	}
	xorrisoCmd := buildXorrisoCommand(xorrisoArgs{
		isoRoot:          isoRoot,
		isoFilePath:      isoFilePath,
		isoLabel:         IsoLabel,
		biosImgRelPath:   biosImgRelPath,
		efiFatImgRelPath: efiFatImgRelPath,
		efiFatImgPath:    efiFatImgPath,
		graftPathspecs:   graftPathspecs,
	})

	if _, err := shell.ExecCmdWithStream(xorrisoCmd, true, shell.HostPath, nil); err != nil {
		log.Errorf("Failed to create ISO image: %v", err)
		return fmt.Errorf("failed to create ISO image: %w", err)
	}

	if err := cleanIsoInstallRoot(isoRoot); err != nil {
		return fmt.Errorf("failed to clean up ISO install root: %w", err)
	}

	log.Infof("ISO creation completed successfully")
	return nil
}

// xorrisoArgs holds the inputs to buildXorrisoCommand. Kept as a struct
// (config-struct convention for >4-5 params) and separated from createIso so
// the command-string assembly is unit-testable without any I/O.
type xorrisoArgs struct {
	isoRoot          string
	isoFilePath      string
	isoLabel         string
	biosImgRelPath   string
	efiFatImgRelPath string
	efiFatImgPath    string
	// graftPathspecs are extra "iso_path=local_path" pathspecs appended after
	// the staging tree, e.g. for files (like the installer payload) that live
	// outside isoRoot and should be included by reference rather than copied
	// into the staging tree first.
	graftPathspecs []string
}

// buildXorrisoCommand assembles the xorriso command line for either a hybrid
// BIOS+UEFI ISO (when biosImgRelPath is set) or a UEFI-only ISO. Both forms
// pass -iso-level 3 to lift the ISO9660 single-file 4 GiB cap - required once
// an installer payload raw image is grafted onto the ISO.
func buildXorrisoCommand(a xorrisoArgs) string {
	var cmd string
	if a.biosImgRelPath != "" {
		// Support both BIOS and UEFI boot mode
		biosImgRelDir := filepath.Dir(a.biosImgRelPath)
		cmd = fmt.Sprintf("xorriso -as mkisofs -graft-points -r -J -l -b %s", a.biosImgRelPath)
		cmd += " -no-emul-boot -boot-load-size 4 -boot-info-table --grub2-boot-info"
		cmd += fmt.Sprintf(" --grub2-mbr %s", filepath.Join(a.isoRoot, biosImgRelDir, "boot_hybrid.img"))
		cmd += fmt.Sprintf(" -eltorito-alt-boot -e %s -no-emul-boot", a.efiFatImgRelPath)
		cmd += fmt.Sprintf(" -append_partition 2 0xef %s -appended_part_as_gpt", a.efiFatImgPath)
		cmd += " -iso-level 3"
		cmd += fmt.Sprintf(" -r %s --sort-weight 0 / --sort-weight 1 /boot", a.isoRoot)
		for _, pathspec := range a.graftPathspecs {
			// pathspec's local-path half is rooted under the configurable work
			// directory, so quote the whole "iso_path=local_path" token rather
			// than assume it is shell-safe.
			cmd += " " + shell.QuoteArg(pathspec)
		}
		cmd += fmt.Sprintf(" -volid \"%s\" --protective-msdos-label -o \"%s\" \"%s\"",
			a.isoLabel, a.isoFilePath, a.isoRoot)
	} else {
		// Support only UEFI boot mode
		cmd = fmt.Sprintf("xorriso -as mkisofs -graft-points -r -J -l --efi-boot %s", a.efiFatImgPath)
		cmd += " -efi-boot-part --efi-boot-image --protective-msdos-label"
		cmd += " -iso-level 3"
		cmd += fmt.Sprintf(" -r %s --sort-weight 0 / --sort-weight 1 /boot", a.isoRoot)
		for _, pathspec := range a.graftPathspecs {
			// pathspec's local-path half is rooted under the configurable work
			// directory, so quote the whole "iso_path=local_path" token rather
			// than assume it is shell-safe.
			cmd += " " + shell.QuoteArg(pathspec)
		}
		cmd += fmt.Sprintf(" -volid \"%s\" -o \"%s\" \"%s\"",
			a.isoLabel, a.isoFilePath, a.isoRoot)
	}
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
