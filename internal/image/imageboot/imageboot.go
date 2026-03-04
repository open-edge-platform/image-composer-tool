package imageboot

import (
	"fmt"
	"path/filepath"
	"strings"

	"os"

	"github.com/open-edge-platform/os-image-composer/internal/config"
	"github.com/open-edge-platform/os-image-composer/internal/image/imagedisc"
	"github.com/open-edge-platform/os-image-composer/internal/utils/file"
	"github.com/open-edge-platform/os-image-composer/internal/utils/logger"
	"github.com/open-edge-platform/os-image-composer/internal/utils/shell"
)

var log = logger.Logger()

type ImageBootInterface interface {
	InstallImageBoot(installRoot string, diskPathIdMap map[string]string, template *config.ImageTemplate, pkgType string) error
}

type ImageBoot struct{}

func NewImageBoot() *ImageBoot {
	return &ImageBoot{}
}

func getDiskPartDevByMountPoint(mountPoint string, diskPathIdMap map[string]string, template *config.ImageTemplate) string {
	diskInfo := template.GetDiskConfig()
	partions := diskInfo.Partitions
	for diskId, diskPath := range diskPathIdMap {
		for _, partition := range partions {
			if partition.ID == diskId && partition.MountPoint == mountPoint {
				return diskPath
			}
		}
	}
	return ""
}

func installGrubWithLegacyMode(installRoot, bootUUID, bootPrefix string, template *config.ImageTemplate) error {
	log.Errorf("Legacy boot mode is not implemented yet")
	return fmt.Errorf("legacy boot mode is not implemented yet")
}

func getGrubVersion(installRoot string) (string, error) {
	var grubVersion string
	program := "grub2-mkconfig"
	exists, err := shell.IsCommandExist(program, installRoot)
	if err != nil {
		return "", fmt.Errorf("failed to check if %s exists: %w", program, err)
	}
	if !exists {
		log.Debugf("%s not found, try grub-mkconfig instead", program)
		program = "grub-mkconfig"
		exists, err = shell.IsCommandExist(program, installRoot)
		if err != nil {
			return "", fmt.Errorf("failed to check if %s exists: %w", program, err)
		}
		if !exists {
			return "", fmt.Errorf("neither grub2-mkconfig nor grub-mkconfig found in the install root")
		}
		grubVersion = "grub"
		log.Debugf("Found %s, setting grub version to grub", program)
	} else {
		grubVersion = "grub2"
		log.Debugf("Found %s, setting grub version to grub2", program)
	}
	return grubVersion, nil
}

func installGrubWithEfiMode(installRoot, bootUUID, bootPrefix, pkgType, grubVersion string, template *config.ImageTemplate) error {
	// Expect that shim (bootx64.efi) and grub (grub.efi) are installed
	// into the EFI directory via the package installation step previously.

	log.Infof("Installing Grub bootloader with EFI mode")
	efiDir := "/boot/efi"
	configDir, err := config.GetGeneralConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get general config directory: %w", err)
	}
	grubAssetPath := filepath.Join(configDir, "image", "efi", "grub", "grub.cfg")
	grubFinalPath := filepath.Join(installRoot, efiDir, "boot", grubVersion, "grub.cfg")

	if err = file.CopyFile(grubAssetPath, grubFinalPath, "-f", true); err != nil {
		log.Errorf("Failed to copy grub configuration file: %v", err)
		return fmt.Errorf("failed to copy grub configuration file: %w", err)
	}

	if err := file.ReplacePlaceholdersInFile("{{.BootUUID}}", bootUUID, grubFinalPath); err != nil {
		log.Errorf("Failed to replace boot UUID in grub configuration: %v", err)
		return fmt.Errorf("failed to replace boot UUID in grub configuration: %w", err)
	}

	// Replace CryptoMountCommand placeholder with an empty string for now.
	if err := file.ReplacePlaceholdersInFile("{{.CryptoMountCommand}}", "", grubFinalPath); err != nil {
		log.Errorf("Failed to replace CryptoMountCommand in grub configuration: %v", err)
		return fmt.Errorf("failed to replace CryptoMountCommand in grub configuration: %w", err)
	}

	prefixPath := fmt.Sprintf("%s/%s", bootPrefix, grubVersion)
	if err := file.ReplacePlaceholdersInFile("{{.PrefixPath}}", prefixPath, grubFinalPath); err != nil {
		log.Errorf("Failed to replace PrefixPath in grub configuration: %v", err)
		return fmt.Errorf("failed to replace prefix path in grub configuration: %w", err)
	}

	chmodCmd := fmt.Sprintf("chmod -R 700 %s", filepath.Dir(grubFinalPath))
	if _, err = shell.ExecCmd(chmodCmd, true, shell.HostPath, nil); err != nil {
		log.Errorf("Failed to set permissions for grub configuration directory: %v", err)
		return fmt.Errorf("failed to set permissions for grub configuration directory: %w", err)
	}

	chmodCmd = fmt.Sprintf("chmod 400 %s", grubFinalPath)
	if _, err = shell.ExecCmd(chmodCmd, true, shell.HostPath, nil); err != nil {
		log.Errorf("Failed to set permissions for grub configuration file: %v", err)
		return fmt.Errorf("failed to set permissions for grub configuration file: %w", err)
	}

	if pkgType == "deb" {
		// Generate bootx64.efi for debian based systems at /EFI/BOOT/bootx64.efi
		installCmd := fmt.Sprintf("grub-install --target=x86_64-efi --efi-directory=%s --removable", efiDir)
		if _, err = shell.ExecCmd(installCmd, true, installRoot, nil); err != nil {
			log.Errorf("Failed to install bootx64.efi for GRUB EFI bootloader: %v", err)
			return fmt.Errorf("failed to install bootx64.efi for GRUB EFI bootloader: %w", err)
		}
	}

	return nil
}

func copyGrubEnvFile(installRoot, grubVersion string) error {
	configDir, err := config.GetGeneralConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get general config directory: %w", err)
	}
	grubEnvAssetPath := filepath.Join(configDir, "image", "grub2", "grubenv")
	grubEnvFinalPath := filepath.Join(installRoot, "boot", grubVersion, "grubenv")
	if err = file.CopyFile(grubEnvAssetPath, grubEnvFinalPath, "-f", true); err != nil {
		log.Errorf("Failed to copy grubenv file: %v", err)
		return fmt.Errorf("failed to copy grubenv file: %w", err)
	}
	return nil
}

func updateGrubConfig(installRoot, grubVersion string) error {
	grubConfigFile := fmt.Sprintf("/boot/%s/grub.cfg", grubVersion)
	program := fmt.Sprintf("%s-mkconfig", grubVersion)
	cmdStr := fmt.Sprintf("%s -o %s", program, grubConfigFile)
	if _, err := shell.ExecCmd(cmdStr, true, installRoot, nil); err != nil {
		log.Errorf("Failed to update grub configuration: %v", err)
		return fmt.Errorf("failed to update grub configuration: %w", err)
	}
	return nil
}

// Helper to get the current kernel version from the rootfs
func getKernelVersionFromBoot(installRoot string) (string, error) {
	kernelDir := filepath.Join(installRoot, "boot")
	files, err := os.ReadDir(kernelDir)
	if err != nil {
		log.Errorf("Failed to list kernel directory %s: %v", kernelDir, err)
		return "", fmt.Errorf("failed to list kernel directory %s: %w", kernelDir, err)
	}
	for _, f := range files {
		if strings.HasPrefix(f.Name(), "vmlinuz-") {
			return strings.TrimPrefix(f.Name(), "vmlinuz-"), nil
		}
	}
	log.Errorf("Kernel image not found in %s", kernelDir)
	return "", fmt.Errorf("kernel image not found in %s", kernelDir)
}

// Helper to update initramfs for Debian/Ubuntu systems using initramfs-tools
func updateInitramfsForGrub(installRoot, kernelVersion string, template *config.ImageTemplate) error {
	log.Debugf("Updating initramfs for Debian/Ubuntu at kernel version: %s", kernelVersion)

	// Add kernel modules specified in enableExtraModules
	extraModules := strings.TrimSpace(template.SystemConfig.Kernel.EnableExtraModules)
	if extraModules != "" {
		log.Debugf("Adding modules to initramfs: %s", extraModules)
		// Split by space and add each module
		modules := strings.Fields(extraModules)
		for _, mod := range modules {
			appendCmd := fmt.Sprintf("echo '%s' >> %s", mod, "/etc/initramfs-tools/modules")
			if _, err := shell.ExecCmd(appendCmd, true, installRoot, nil); err != nil {
				log.Warnf("Failed to add module %s to initramfs: %v", mod, err)
			}
		}
	} else {
		log.Debugf("No extra modules specified in enableExtraModules")
	}

	updateInitramfsExists, err := shell.IsCommandExist("update-initramfs", installRoot)
	if err != nil {
		return fmt.Errorf("failed to check update-initramfs availability: %w", err)
	}

	var cmd string
	if updateInitramfsExists {
		cmd = fmt.Sprintf("update-initramfs -u -k %s", kernelVersion)
	} else {
		dracutExists, dracutCheckErr := shell.IsCommandExist("dracut", installRoot)
		if dracutCheckErr != nil {
			return fmt.Errorf("failed to check dracut availability: %w", dracutCheckErr)
		}
		if !dracutExists {
			return fmt.Errorf("neither update-initramfs nor dracut found in the install root")
		}

		initrdPath := fmt.Sprintf("/boot/initrd.img-%s", kernelVersion)
		cmd = fmt.Sprintf("dracut --force --kver %s %s", kernelVersion, initrdPath)
		if extraModules != "" {
			cmd = fmt.Sprintf("%s --add-drivers '%s'", cmd, extraModules)
		}
		log.Infof("update-initramfs not found, using dracut fallback")
	}

	log.Debugf("Executing: %s", cmd)
	_, err = shell.ExecCmd(cmd, true, installRoot, nil)
	if err != nil {
		log.Errorf("Failed to update initramfs: %v", err)
		return fmt.Errorf("failed to update initramfs: %w", err)
	}

	log.Debugf("Initramfs updated successfully")
	return nil
}

func updateBootConfigTemplate(installRoot, rootDevID, bootUUID, bootPrefix, hashDevID, rootHashPH string, template *config.ImageTemplate) error {
	log.Infof("Updating boot configurations")

	configDir, err := config.GetGeneralConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get general config directory: %w", err)
	}

	var configAssetPath string
	var configFinalPath string
	bootloaderConfig := template.GetBootloaderConfig()
	switch bootloaderConfig.Provider {
	case "grub":
		configAssetPath = filepath.Join(configDir, "image", "grub2", "grub")
		configFinalPath = filepath.Join(installRoot, "etc", "default", "grub")
		if err = file.CopyFile(configAssetPath, configFinalPath, "-f", true); err != nil {
			log.Errorf("Failed to copy boot configuration file: %v", err)
			return fmt.Errorf("failed to copy boot configuration file: %w", err)
		}

		if err := file.ReplacePlaceholdersInFile("{{.Hostname}}", template.GetImageName(), configFinalPath); err != nil {
			log.Errorf("Failed to replace Hostname in boot configuration: %v", err)
			return fmt.Errorf("failed to replace Hostname in boot configuration: %w", err)
		}
	case "systemd-boot":
		configAssetPath = filepath.Join(configDir, "image", "efi", "bootParams.conf")
		configFinalPath = filepath.Join(installRoot, "boot", "cmdline.conf")
		if err = file.CopyFile(configAssetPath, configFinalPath, "-f", true); err != nil {
			log.Errorf("Failed to copy boot configuration file: %v", err)
			return fmt.Errorf("failed to copy boot configuration file: %w", err)
		}
	default:
		log.Errorf("Unsupported bootloader provider: %s", bootloaderConfig.Provider)
		return fmt.Errorf("unsupported bootloader provider: %s", bootloaderConfig.Provider)
	}

	if err := file.ReplacePlaceholdersInFile("{{.BootUUID}}", bootUUID, configFinalPath); err != nil {
		log.Errorf("Failed to replace BootUUID in boot configuration: %v", err)
		return fmt.Errorf("failed to replace BootUUID in boot configuration: %w", err)
	}

	if err := file.ReplacePlaceholdersInFile("{{.BootPrefix}}", bootPrefix, configFinalPath); err != nil {
		log.Errorf("Failed to replace BootPrefix in boot configuration: %v", err)
		return fmt.Errorf("failed to replace BootPrefix in boot configuration: %w", err)
	}

	if template.IsImmutabilityEnabled() {
		// For dm-verity, use /dev/mapper/root as the root device
		// The initramfs script will create this device using the systemd.verity_* parameters
		if err := file.ReplacePlaceholdersInFile("{{.RootPartition}}", "/dev/mapper/root", configFinalPath); err != nil {
			log.Errorf("Failed to replace RootPartition in boot configuration: %v", err)
			return fmt.Errorf("failed to replace RootPartition in boot configuration: %w", err)
		}
		// Construct systemd verity command line if hashDevID is provided
		verityCmd := ""
		if hashDevID != "" {
			verityCmd = fmt.Sprintf("systemd.verity_name=root systemd.verity_root_data=%s systemd.verity_root_hash=%s", rootDevID, hashDevID)
		}
		if err := file.ReplacePlaceholdersInFile("{{.SystemdVerity}}", verityCmd, configFinalPath); err != nil {
			log.Errorf("Failed to replace dm verity arg in boot configuration: %v", err)
			return fmt.Errorf("failed to replace dm verity arg in boot configuration: %w", err)
		}
		if err := file.ReplacePlaceholdersInFile("{{.RootHash}}", rootHashPH, configFinalPath); err != nil {
			log.Errorf("Failed to replace dm verity roothash in boot configuration: %v", err)
			return fmt.Errorf("failed to replace dm verity roothash in boot configuration: %w", err)
		}
	} else {

		rootPartition := rootDevID

		// Special case for some security module like EMF required hardcoded root partition
		cmdline := template.GetKernel().Cmdline
		cmdlineMap := make(map[string]string)
		if cmdline != "" {
			// Parse cmdline into key-value pairs
			fields := strings.Fields(cmdline)
			for _, field := range fields {
				parts := strings.SplitN(field, "=", 2)
				if len(parts) == 2 {
					cmdlineMap[parts[0]] = parts[1]
				}
			}
			// Check if "root" key exists and assign to rootPartition
			if rootVal, exists := cmdlineMap["root"]; exists {
				rootPartition = rootVal
			}
		}

		if err := file.ReplacePlaceholdersInFile("{{.RootPartition}}", rootPartition, configFinalPath); err != nil {
			log.Errorf("Failed to replace RootPartition in boot configuration: %v", err)
			return fmt.Errorf("failed to replace RootPartition in boot configuration: %w", err)
		}
		if err := file.ReplacePlaceholdersInFile("{{.SystemdVerity}}", "", configFinalPath); err != nil {
			log.Errorf("Failed to replace dm verity arg in boot configuration: %v", err)
			return fmt.Errorf("failed to replace dm verity arg in boot configuration: %w", err)
		}
		if err := file.ReplacePlaceholdersInFile("{{.RootHash}}", "", configFinalPath); err != nil {
			log.Errorf("Failed to replace dm verity roothash in boot configuration: %v", err)
			return fmt.Errorf("failed to replace dm verity roothash in boot configuration: %w", err)
		}

	}

	// For now, we do not support LUKS encryption, so we replace the LuksUUID placeholder with an empty string.
	if err := file.ReplacePlaceholdersInFile("{{.LuksUUID}}", "", configFinalPath); err != nil {
		log.Errorf("Failed to replace LuksUUID in boot configuration: %v", err)
		return fmt.Errorf("failed to replace LuksUUID in boot configuration: %w", err)
	}

	// For now, we do not support LVM, so we replace the LVM placeholder with an empty string.
	if err := file.ReplacePlaceholdersInFile("{{.LVM}}", "", configFinalPath); err != nil {
		log.Errorf("Failed to replace LVM in boot configuration: %v", err)
		return fmt.Errorf("failed to replace LVM in boot configuration: %w", err)
	}

	// For now, we do not support IMAPolicy, so we replace the IMAPolicy placeholder with an empty string.
	if err := file.ReplacePlaceholdersInFile("{{.IMAPolicy}}", "", configFinalPath); err != nil {
		log.Errorf("Failed to replace IMAPolicy in boot configuration: %v", err)
		return fmt.Errorf("failed to replace IMAPolicy in boot configuration: %w", err)
	}

	// For now, we do not support SELinux, so we replace the SELinux placeholder with an empty string.
	if err := file.ReplacePlaceholdersInFile("{{.SELinux}}", "", configFinalPath); err != nil {
		log.Errorf("Failed to replace SELinux in boot configuration: %v", err)
		return fmt.Errorf("failed to replace SELinux in boot configuration: %w", err)
	}

	// For now, we do not support FIPS, so we replace the FIPS placeholder with an empty string.
	if err := file.ReplacePlaceholdersInFile("{{.FIPS}}", "", configFinalPath); err != nil {
		log.Errorf("Failed to replace FIPS in boot configuration: %v", err)
		return fmt.Errorf("failed to replace FIPS in boot configuration: %w", err)
	}

	// For now, we do not support CGroup, so we replace the CGroup placeholder with an empty string.
	if err := file.ReplacePlaceholdersInFile("{{.CGroup}}", "", configFinalPath); err != nil {
		log.Errorf("Failed to replace CGroup in boot configuration: %v", err)
		return fmt.Errorf("failed to replace CGroup in boot configuration: %w", err)
	}

	kernelConfig := template.GetKernel()
	// Special cases for some security module like EMF required hardcoded root partition as additional cmdline arg
	// Remove the "root" parameter and its value from the cmdline as it's has been handled previously
	trimRootArgfromCmdLine := kernelConfig.Cmdline
	if trimRootArgfromCmdLine != "" {
		fields := strings.Fields(trimRootArgfromCmdLine)
		var filteredFields []string
		for _, field := range fields {
			// Skip fields that start with "root="
			if !strings.HasPrefix(field, "root=") {
				filteredFields = append(filteredFields, field)
			}
		}
		trimRootArgfromCmdLine = strings.Join(filteredFields, " ")
	}

	if err := file.ReplacePlaceholdersInFile("{{.ExtraCommandLine}}", trimRootArgfromCmdLine, configFinalPath); err != nil {
		log.Errorf("Failed to replace ExtraCommandLine in boot configuration: %v", err)
		return fmt.Errorf("failed to replace ExtraCommandLine in boot configuration: %w", err)
	}

	// For now, we do not support EncryptionBootUUID, so we replace the EncryptionBootUUID placeholder with an empty string.
	if err := file.ReplacePlaceholdersInFile("{{.EncryptionBootUUID}}", "", configFinalPath); err != nil {
		log.Errorf("Failed to replace EncryptionBootUUID in boot configuration: %v", err)
		return fmt.Errorf("failed to replace EncryptionBootUUID in boot configuration: %w", err)
	}

	if err := file.ReplacePlaceholdersInFile("{{.rdAuto}}", "rd.auto=1", configFinalPath); err != nil {
		log.Errorf("Failed to replace rdAuto in boot configuration: %v", err)
		return fmt.Errorf("failed to replace rdAuto in boot configuration: %w", err)
	}

	return nil
}

func (imageBoot *ImageBoot) InstallImageBoot(installRoot string, diskPathIdMap map[string]string, template *config.ImageTemplate, pkgType string) error {
	var bootUUID string
	var bootPrefix string = ""
	var rootDev string
	var hashDev string
	var err error

	log.Infof("Installing image bootloader for: %s", template.Image.Name)

	bootPartDev := getDiskPartDevByMountPoint("/boot", diskPathIdMap, template)
	if bootPartDev == "" {
		// /boot is not a separate partition, use root partition instead
		bootPrefix = "/boot"
		rootDev = getDiskPartDevByMountPoint("/", diskPathIdMap, template)
		if rootDev == "" {
			return fmt.Errorf("failed to find root partition for mount point '/'")
		}
		bootUUID, err = imagedisc.GetUUID(rootDev)
		if err != nil {
			return fmt.Errorf("failed to get UUID for boot partition %s: %w", rootDev, err)
		}
	} else {
		bootUUID, err = imagedisc.GetUUID(bootPartDev)
		if err != nil {
			return fmt.Errorf("failed to get UUID for boot partition %s: %w", bootPartDev, err)
		}
		rootDev = getDiskPartDevByMountPoint("/", diskPathIdMap, template)
		if rootDev == "" {
			return fmt.Errorf("failed to find root partition for mount point '/'")
		}
	}

	rootPartUUID, err := imagedisc.GetPartUUID(rootDev)
	if err != nil {
		return fmt.Errorf("failed to get partition UUID for root partition %s: %w", rootDev, err)
	}
	rootDevID := fmt.Sprintf("PARTUUID=%s", rootPartUUID)

	bootloaderConfig := template.GetBootloaderConfig()
	switch bootloaderConfig.Provider {
	case "grub":
		log.Infof("Installing GRUB bootloader")

		grubVersion, err := getGrubVersion(installRoot)
		if err != nil {
			log.Errorf("Failed to get grub version: %v", err)
			return fmt.Errorf("failed to get grub version: %w", err)
		}

		if bootloaderConfig.BootType == "efi" {
			if err := installGrubWithEfiMode(installRoot, bootUUID, bootPrefix, pkgType, grubVersion, template); err != nil {
				return fmt.Errorf("failed to install GRUB bootloader with EFI mode: %w", err)
			}
		} else if bootloaderConfig.BootType == "legacy" {
			if err := installGrubWithLegacyMode(installRoot, bootUUID, bootPrefix, template); err != nil {
				return fmt.Errorf("failed to install GRUB bootloader with legacy mode: %w", err)
			}
		}

		if err := updateBootConfigTemplate(installRoot, rootDevID, bootUUID, bootPrefix, "", "", template); err != nil {
			return fmt.Errorf("failed to update boot configuration: %w", err)
		}

		if err := copyGrubEnvFile(installRoot, grubVersion); err != nil {
			return fmt.Errorf("failed to copy grubenv file: %w", err)
		}

		// Update initramfs for Debian/Ubuntu systems with GRUB
		// This must happen after updateBootConfigTemplate but before updateGrubConfig
		if pkgType == "deb" {
			kernelVersion, err := getKernelVersionFromBoot(installRoot)
			if err != nil {
				return fmt.Errorf("Failed to get kernel version for initramfs update: %w", err)
			} else {
				if err := updateInitramfsForGrub(installRoot, kernelVersion, template); err != nil {
					return fmt.Errorf("Failed to update initramfs: %w", err)
				} else {
					log.Infof("Initramfs updated successfully for kernel version: %s", kernelVersion)
				}
			}
		}

		if err := updateGrubConfig(installRoot, grubVersion); err != nil {
			return fmt.Errorf("failed to update grub configuration: %w", err)
		}

	case "systemd-boot":
		log.Infof("Installing systemd-boot bootloader")
		if bootloaderConfig.BootType == "efi" {

			if template.IsImmutabilityEnabled() {
				hashDev = getDiskPartDevByMountPoint("none", diskPathIdMap, template)
				if hashDev == "" {
					return fmt.Errorf("failed to find dm verity hash partition")
				}
				hashPartUUID, err := imagedisc.GetPartUUID(hashDev)
				if err != nil {
					return fmt.Errorf("failed to get partition UUID for dm verity hash partition %s: %w", rootDev, err)
				}
				hashDevID := fmt.Sprintf("PARTUUID=%s", hashPartUUID)
				rootHashPH := fmt.Sprintf("roothash=%s-%s", rootDev, hashDev)
				if err := updateBootConfigTemplate(installRoot, rootDevID, bootUUID, bootPrefix, hashDevID, rootHashPH, template); err != nil {
					return fmt.Errorf("failed to update boot configuration: %w", err)
				}
			} else {
				if err := updateBootConfigTemplate(installRoot, rootDevID, bootUUID, bootPrefix, "", "", template); err != nil {
					return fmt.Errorf("failed to update boot configuration: %w", err)
				}
			}
		} else {
			return fmt.Errorf("systemd-boot is only supported in EFI mode")
		}
	default:
		return fmt.Errorf("unsupported bootloader provider: %s", bootloaderConfig.Provider)
	}

	return nil
}
