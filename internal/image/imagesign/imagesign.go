package imagesign

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/system"
)

func SignImage(installRoot string, template *config.ImageTemplate) error {

	// If immutability is not enabled, skip signing
	if !template.IsImmutabilityEnabled() {
		return nil
	}

	// Check if secure boot keys are provided
	// If not, skip signing
	if template.GetSecureBootDBKeyPath() == "" ||
		template.GetSecureBootDBCrtPath() == "" ||
		template.GetSecureBootDBCerPath() == "" {
		return nil
	}

	pbKeyPath := template.GetSecureBootDBKeyPath()
	prKeyPath := template.GetSecureBootDBCrtPath()
	prCerPath := template.GetSecureBootDBCerPath()

	// Check if the key and certificate files exist
	if _, err := os.Stat(pbKeyPath); err != nil {
		return fmt.Errorf("secure boot key file not found at %s: %w", pbKeyPath, err)
	}
	if _, err := os.Stat(prKeyPath); err != nil {
		return fmt.Errorf("secure boot certificate file not found at %s: %w", prKeyPath, err)
	}
	if _, err := os.Stat(prCerPath); err != nil {
		return fmt.Errorf("secure boot UEFI certificate file not found at %s: %w", prCerPath, err)
	}

	espDir := filepath.Join(installRoot, "boot", "efi")
	ukiPath := filepath.Join(espDir, "EFI", "Linux", "linux.efi")
	bootloaderPath := filepath.Join(espDir, "EFI", "BOOT", "BOOTX64.EFI")

	// Sign the UKI (Unified Kernel Image) - create signed file then replace original
	ukiSignedPath := filepath.Join(espDir, "EFI", "Linux", "linux.efi.signed")
	cmd := fmt.Sprintf("sbsign --key %s --cert %s --output %s %s",
		pbKeyPath, prKeyPath, ukiSignedPath, ukiPath)
	if _, err := shell.ExecCmd(cmd, true, shell.HostPath, nil); err != nil {
		return fmt.Errorf("failed to sign UKI: %w", err)
	}

	// Replace original with signed version
	if err := os.Rename(ukiSignedPath, ukiPath); err != nil {
		return fmt.Errorf("failed to replace UKI with signed version: %w", err)
	}

	// Sign the bootloader - create signed file then replace original
	bootloaderSignedPath := filepath.Join(espDir, "EFI", "BOOT", "BOOTX64.EFI.signed")
	cmd = fmt.Sprintf("sbsign --key %s --cert %s --output %s %s",
		pbKeyPath, prKeyPath, bootloaderSignedPath, bootloaderPath)
	if _, err := shell.ExecCmd(cmd, true, shell.HostPath, nil); err != nil {
		return fmt.Errorf("failed to sign bootloader: %w", err)
	}

	// Replace original with signed version
	if err := os.Rename(bootloaderSignedPath, bootloaderPath); err != nil {
		return fmt.Errorf("failed to replace bootloader with signed version: %w", err)
	}

	// Getting image build directory
	globalWorkDir, err := config.WorkDir()
	if err != nil {
		return fmt.Errorf("failed to get global work directory: %w", err)
	}
	providerId := system.GetProviderId(template.Target.OS, template.Target.Dist,
		template.Target.Arch)
	imageBuildDir := filepath.Join(globalWorkDir, providerId, "imagebuild")
	sysConfigName := template.GetSystemConfigName()
	finalCerFilePath := filepath.Join(imageBuildDir, sysConfigName, "DB.cer")

	// Copy the certificate file to the temp directory using Go's file library
	input, err := os.Open(prCerPath)
	if err != nil {
		return fmt.Errorf("failed to open certificate file: %w", err)
	}
	defer input.Close()

	// Ensure the destination directory exists
	if err := os.MkdirAll(filepath.Dir(finalCerFilePath), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	output, err := os.Create(finalCerFilePath)
	if err != nil {
		return fmt.Errorf("failed to create destination certificate file: %w", err)
	}
	defer output.Close()

	if _, err := io.Copy(output, input); err != nil {
		return fmt.Errorf("failed to copy certificate file: %w", err)
	}

	return nil
}
