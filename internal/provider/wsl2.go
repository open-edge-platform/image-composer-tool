package provider

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/chroot"
	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/config/manifest"
	"github.com/open-edge-platform/image-composer-tool/internal/image/imageos"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/compression"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/display"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/system"
)

var wsl2Log = logger.Logger()

func WSL2ArchiveFormat(template *config.ImageTemplate) (archiveType, archiveExt string, err error) {
	artifacts := template.GetDiskConfig().Artifacts
	if len(artifacts) == 0 {
		return "", "", fmt.Errorf("wsl2 image requires a tar artifact with compression")
	}

	artifact := artifacts[0]
	if artifact.Type != "tar" {
		return "", "", fmt.Errorf("wsl2 image requires tar artifact type, got %s", artifact.Type)
	}

	switch strings.ToLower(artifact.Compression) {
	case "gz", "gzip":
		return "tar.gz", "tar.gz", nil
	case "xz":
		return "tar.xz", "tar.xz", nil
	default:
		return "", "", fmt.Errorf("wsl2 image requires supported tar compression, got %s", artifact.Compression)
	}
}

func BuildWSL2Image(chrootEnv chroot.ChrootEnvInterface, template *config.ImageTemplate) error {
	if chrootEnv == nil {
		return fmt.Errorf("chroot environment cannot be nil")
	}

	imageOs, err := imageos.NewImageOs(chrootEnv, template)
	if err != nil {
		return fmt.Errorf("failed to create image OS: %w", err)
	}

	installRoot, versionInfo, err := imageOs.InstallRootfs()
	if err != nil {
		return fmt.Errorf("failed to install WSL2 rootfs: %w", err)
	}

	if err := manifest.CopySBOMToChroot(installRoot); err != nil {
		wsl2Log.Warnf("Failed to copy SBOM into WSL2 rootfs: %v", err)
	}

	archiveType, archiveExt, err := WSL2ArchiveFormat(template)
	if err != nil {
		return err
	}

	archiveName := fmt.Sprintf("%s-%s.%s", template.GetImageName(), versionInfo, archiveExt)
	if versionInfo == "" {
		archiveName = fmt.Sprintf("%s.%s", template.GetImageName(), archiveExt)
	}

	globalWorkDir, err := config.WorkDir()
	if err != nil {
		return fmt.Errorf("failed to get work directory: %w", err)
	}
	providerID := system.GetProviderId(template.Target.OS, template.Target.Dist, template.Target.Arch)
	imageBuildDir := filepath.Join(globalWorkDir, providerID, "imagebuild", template.GetSystemConfigName())
	archivePath := filepath.Join(imageBuildDir, archiveName)

	if err := compression.CompressFolder(installRoot, archivePath, archiveType, false); err != nil {
		return fmt.Errorf("failed to package WSL2 rootfs: %w", err)
	}
	if err := manifest.CopySBOMToImageBuildDir(imageBuildDir); err != nil {
		wsl2Log.Warnf("Failed to copy SBOM to WSL2 image build directory: %v", err)
	}

	display.PrintImageDirectorySummary(imageBuildDir, "WSL2")
	return nil
}
