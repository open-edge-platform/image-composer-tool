package wsl2maker

import (
	"fmt"
	"os"
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

type WSL2Maker struct {
	template      *config.ImageTemplate
	ImageBuildDir string
	ChrootEnv     chroot.ChrootEnvInterface
	ImageOs       *imageos.ImageOs
}

var log = logger.Logger()

func NewWSL2Maker(chrootEnv chroot.ChrootEnvInterface, template *config.ImageTemplate) (*WSL2Maker, error) {
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

	return &WSL2Maker{
		template:  template,
		ChrootEnv: chrootEnv,
		ImageOs:   imageOs,
	}, nil
}

func (wsl2Maker *WSL2Maker) Init() error {
	globalWorkDir, err := config.WorkDir()
	if err != nil {
		return fmt.Errorf("failed to get work directory: %w", err)
	}

	providerID := system.GetProviderId(
		wsl2Maker.template.Target.OS,
		wsl2Maker.template.Target.Dist,
		wsl2Maker.template.Target.Arch,
	)

	wsl2Maker.ImageBuildDir = filepath.Join(
		globalWorkDir,
		providerID,
		"imagebuild",
		wsl2Maker.template.GetSystemConfigName(),
	)

	return os.MkdirAll(wsl2Maker.ImageBuildDir, 0700)
}

func (wsl2Maker *WSL2Maker) BuildWSL2Image() error {
	if wsl2Maker.ImageOs == nil {
		return fmt.Errorf("image OS cannot be nil")
	}

	installRoot, versionInfo, err := wsl2Maker.ImageOs.InstallRootfs()
	if err != nil {
		return fmt.Errorf("failed to install WSL2 rootfs: %w", err)
	}

	if err := manifest.CopySBOMToChroot(installRoot); err != nil {
		log.Warnf("Failed to copy SBOM into WSL2 rootfs: %v", err)
	}

	archiveType, archiveExt, err := archiveFormat(wsl2Maker.template)
	if err != nil {
		return err
	}

	archiveName := fmt.Sprintf("%s-%s.%s", wsl2Maker.template.GetImageName(), versionInfo, archiveExt)
	if versionInfo == "" {
		archiveName = fmt.Sprintf("%s.%s", wsl2Maker.template.GetImageName(), archiveExt)
	}

	archivePath := filepath.Join(wsl2Maker.ImageBuildDir, archiveName)
	if err := compression.CompressFolder(installRoot, archivePath, archiveType, false); err != nil {
		return fmt.Errorf("failed to package WSL2 rootfs: %w", err)
	}
	if err := manifest.CopySBOMToImageBuildDir(wsl2Maker.ImageBuildDir); err != nil {
		log.Warnf("Failed to copy SBOM to WSL2 image build directory: %v", err)
	}

	display.PrintImageDirectorySummary(wsl2Maker.ImageBuildDir, "WSL2")
	return nil
}

func archiveFormat(template *config.ImageTemplate) (archiveType, archiveExt string, err error) {
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
	default:
		return "", "", fmt.Errorf("wsl2 image requires supported tar compression (gz), got %s", artifact.Compression)
	}
}
