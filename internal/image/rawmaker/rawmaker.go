package rawmaker

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/chroot"
	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/config/manifest"
	"github.com/open-edge-platform/image-composer-tool/internal/image/imageconvert"
	"github.com/open-edge-platform/image-composer-tool/internal/image/imagedisc"
	"github.com/open-edge-platform/image-composer-tool/internal/image/imageos"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/system"
)

type RawMakerInterface interface {
	Init() error          // Initialize with stored template
	BuildRawImage() error // Build raw image using stored template
}

type RawMaker struct {
	template      *config.ImageTemplate
	ImageBuildDir string
	ChrootEnv     chroot.ChrootEnvInterface
	LoopDev       imagedisc.LoopDevInterface
	ImageOs       imageos.ImageOsInterface
	ImageConvert  imageconvert.ImageConvertInterface
}

var log = logger.Logger()

func NewRawMaker(chrootEnv chroot.ChrootEnvInterface, template *config.ImageTemplate) (*RawMaker, error) {
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

	return &RawMaker{
		template:     template, // Store template
		ChrootEnv:    chrootEnv,
		LoopDev:      imagedisc.NewLoopDev(),
		ImageOs:      imageOs, // Already template-aware
		ImageConvert: imageconvert.NewImageConvert(),
	}, nil
}

func (rawMaker *RawMaker) Init() error {
	globalWorkDir, err := config.WorkDir()
	if err != nil {
		return fmt.Errorf("failed to get work directory: %w", err)
	}

	providerId := system.GetProviderId(
		rawMaker.template.Target.OS,
		rawMaker.template.Target.Dist,
		rawMaker.template.Target.Arch,
	)

	rawMaker.ImageBuildDir = filepath.Join(
		globalWorkDir,
		providerId,
		"imagebuild",
		rawMaker.template.GetSystemConfigName(),
	)

	return os.MkdirAll(rawMaker.ImageBuildDir, 0700)
}

// Helper method for image file cleanup
func (rawMaker *RawMaker) cleanupImageFileOnError(imagePath string) {
	if imagePath == "" {
		return
	}

	if _, statErr := os.Stat(imagePath); statErr == nil {
		log.Warnf("Cleaning up image file due to error: %s", imagePath)
		if _, rmErr := shell.ExecCmd(fmt.Sprintf("rm -f %s", imagePath), true, shell.HostPath, nil); rmErr != nil {
			log.Errorf("Failed to remove image file %s during cleanup: %v", imagePath, rmErr)
		} else {
			log.Infof("Successfully cleaned up image file: %s", imagePath)
		}
	}
}

// Helper method for file renaming
func (rawMaker *RawMaker) renameImageFile(currentPath, imageName, versionInfo string) (string, error) {
	// Construct new file path
	newFilePath := filepath.Join(rawMaker.ImageBuildDir, fmt.Sprintf("%s-%s.raw", imageName, versionInfo))

	log.Infof("Renaming image file from %s to %s", currentPath, newFilePath)

	// Move file
	if _, err := shell.ExecCmd(fmt.Sprintf("mv %s %s", currentPath, newFilePath), true, shell.HostPath, nil); err != nil {
		return "", fmt.Errorf("failed to move file from %s to %s: %w", currentPath, newFilePath, err)
	}

	return newFilePath, nil
}

func (rawMaker *RawMaker) BuildRawImage() error {
	imageName := rawMaker.template.GetImageName()
	imageFile := filepath.Join(rawMaker.ImageBuildDir, imageName+".raw")

	log.Infof("Creating raw image file: %s", imageFile)

	// Create loop device
	loopDevPath, diskPathIdMap, err := rawMaker.LoopDev.CreateRawImageLoopDev(imageFile, rawMaker.template)
	if err != nil {
		return fmt.Errorf("failed to create loop device: %w", err)
	}

	// Setup cleanup for loop device (always needed)
	defer func() {
		if loopDevPath != "" {
			if detachErr := rawMaker.LoopDev.LoopSetupDelete(loopDevPath); detachErr != nil {
				log.Errorf("Failed to detach loopback device %s: %v", loopDevPath, detachErr)
			} else {
				log.Infof("Successfully detached loopback device: %s", loopDevPath)
			}
		}
	}()

	log.Infof("Created loop device: %s", loopDevPath)

	// Install OS
	versionInfo, err := rawMaker.ImageOs.InstallImageOs(diskPathIdMap)
	if err != nil {
		// Loop device will be cleaned up by defer
		// Image file cleanup handled separately if needed
		rawMaker.cleanupImageFileOnError(imageFile)
		return fmt.Errorf("failed to install OS: %w", err)
	}

	log.Infof("OS installation completed with version: %s", versionInfo)

	// File renaming
	finalImagePath, err := rawMaker.renameImageFile(imageFile, imageName, versionInfo)
	if err != nil {
		rawMaker.cleanupImageFileOnError(imageFile)
		return fmt.Errorf("failed to rename image file: %w", err)
	}
	rawMaker.template.FinishPureImageBuildTimer()

	pureImageBuildDuration := rawMaker.template.GetPureImageBuildDuration()
	if pureImageBuildDuration > 0 {
		log.Infof("Pure raw image build time: %s", pureImageBuildDuration.Round(time.Millisecond))
	}

	log.Infof("Raw image build completed successfully: %s", finalImagePath)

	// Image conversion (may compress/remove original file)
	rawMaker.template.StartConvertImageTimer()
	if err := rawMaker.ImageConvert.ConvertImageFile(finalImagePath, rawMaker.template); err != nil {
		rawMaker.template.FinishConvertImageTimer()
		convertImageDuration := rawMaker.template.GetConvertImageDuration()
		if convertImageDuration > 0 {
			log.Infof("Image conversion time before failure: %s", convertImageDuration.Round(time.Millisecond))
		}
		rawMaker.cleanupImageFileOnError(finalImagePath)
		return fmt.Errorf("failed to convert image file: %w", err)
	}
	rawMaker.template.FinishConvertImageTimer()

	convertImageDuration := rawMaker.template.GetConvertImageDuration()
	if convertImageDuration > 0 {
		log.Infof("Image conversion time: %s", convertImageDuration.Round(time.Millisecond))
	}

	// Copy SBOM to image build directory
	if err := manifest.CopySBOMToImageBuildDir(rawMaker.ImageBuildDir); err != nil {
		log.Warnf("Failed to copy SBOM to image build directory: %v", err)
		// Don't fail the build if SBOM copy fails, just log warning
	}

	return nil
}
