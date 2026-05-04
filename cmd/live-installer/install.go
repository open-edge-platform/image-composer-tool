package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	attendedinstaller "github.com/open-edge-platform/image-composer-tool/cmd/live-installer/texture-ui"
	"github.com/open-edge-platform/image-composer-tool/internal/chroot"
	"github.com/open-edge-platform/image-composer-tool/internal/chroot/chrootbuild"
	"github.com/open-edge-platform/image-composer-tool/internal/chroot/deb"
	"github.com/open-edge-platform/image-composer-tool/internal/chroot/rpm"
	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/image/imagedisc"
	"github.com/open-edge-platform/image-composer-tool/internal/image/imageos"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/file"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/security"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

func newChrootBuilder(configDir, localRepo, targetOs, targetDist, targetArch string) (*chrootbuild.ChrootBuilder, error) {
	var targetOsConfig map[string]interface{}

	targetOsConfigDir := filepath.Join(configDir, "osv", targetOs, targetDist)
	if _, err := os.Stat(targetOsConfigDir); os.IsNotExist(err) {
		log.Errorf("Target OS config directory does not exist: %s", targetOsConfigDir)
		return nil, fmt.Errorf("target OS config directory does not exist: %s", targetOsConfigDir)
	}
	targetOsConfigFile := filepath.Join(targetOsConfigDir, "config.yml")
	if _, err := os.Stat(targetOsConfigFile); os.IsNotExist(err) {
		log.Errorf("Target OS config file does not exist: %s", targetOsConfigFile)
		return nil, fmt.Errorf("target OS config file does not exist: %s", targetOsConfigFile)
	}

	// Read the raw YAML data for validation
	data, err := security.SafeReadFile(targetOsConfigFile, security.RejectSymlinks)
	if err != nil {
		return nil, fmt.Errorf("reading target OS config file %s: %w", targetOsConfigFile, err)
	}

	// Validate the target OS configuration before parsing
	if err := chrootbuild.ValidateOsConfigYAML(data); err != nil {
		return nil, fmt.Errorf("target OS config validation failed for %s: %w", targetOsConfigFile, err)
	}

	targetOsConfigs, err := file.ReadFromYaml(targetOsConfigFile)
	if err != nil {
		log.Errorf("Failed to read target OS config file: %v", err)
		return nil, fmt.Errorf("failed to read target OS config file: %w", err)
	}
	if targetConfig, ok := targetOsConfigs[targetArch]; ok {
		targetOsConfig = targetConfig.(map[string]interface{})
	} else {
		log.Errorf("Target OS %s config for architecture %s not found in %s", targetOs, targetArch, targetOsConfigFile)
		return nil, fmt.Errorf("target OS %s config for architecture %s not found in %s", targetOs, targetArch, targetOsConfigFile)
	}

	return &chrootbuild.ChrootBuilder{
		TargetOsConfigDir: targetOsConfigDir,
		TargetOsConfig:    targetOsConfig,
		ChrootPkgCacheDir: localRepo,
		RpmInstaller:      rpm.NewRpmInstaller(),
		DebInstaller:      deb.NewDebInstaller(),
	}, nil
}

func newChrootEnv(configDir, localRepo, targetOs, targetDist, targetArch string) (*chroot.ChrootEnv, error) {
	chrootBuilder, err := newChrootBuilder(configDir, localRepo, targetOs, targetDist, targetArch)
	if err != nil {
		return nil, fmt.Errorf("failed to create chroot builder: %w", err)
	}

	return &chroot.ChrootEnv{
		ChrootEnvRoot: shell.HostPath,
		ChrootBuilder: chrootBuilder,
	}, nil
}

func dependencyCheck(targetOs string) error {
	// Check if required host dependencies are installed
	var dependencyInfo map[string]string
	switch targetOs {
	case "azure-linux":
		dependencyInfo = map[string]string{
			"mkfs.fat": "dosfstools", // For the FAT32 boot partition creation
			//"sbsign":   "sbsigntool", // For the UKI image creation
		}
	case "edge-microvisor-toolkit":
		dependencyInfo = map[string]string{
			"mkfs.fat": "dosfstools", // For the FAT32 boot partition creation
			//"sbsign":   "sbsigntool", // For the UKI image creation
		}
	case "wind-river-elxr":
		dependencyInfo = map[string]string{
			"mmdebstrap":  "mmdebstrap", // For the chroot env build
			"mkfs.fat":    "dosfstools", // For the FAT32 boot partition creation
			"sgdisk":      "gdisk",      // For GPT partition creation
			"veritysetup": "cryptsetup", // For the veritysetup command
			//"sbsign":      "sbsigntools", // For the UKI image creation
		}
	case "ubuntu":
		dependencyInfo = map[string]string{
			"mmdebstrap":  "mmdebstrap", // For the chroot env build
			"mkfs.fat":    "dosfstools", // For the FAT32 boot partition creation
			"sgdisk":      "gdisk",      // For GPT partition creation
			"veritysetup": "cryptsetup", // For the veritysetup command
			//"sbsign":      "sbsigntools", // For the UKI image creation
		}
	case "debian":
		dependencyInfo = map[string]string{
			"mmdebstrap":  "mmdebstrap", // For the chroot env build
			"mkfs.fat":    "dosfstools", // For the FAT32 boot partition creation
			"sgdisk":      "gdisk",      // For GPT partition creation
			"veritysetup": "cryptsetup", // For the veritysetup command
		}
	default:
		return fmt.Errorf("unsupported target OS for dependency check: %s", targetOs)
	}

	for cmd, pkg := range dependencyInfo {
		cmdExist, err := shell.IsCommandExist(cmd, shell.HostPath)
		if err != nil {
			return fmt.Errorf("failed to check command %s existence: %w", cmd, err)
		}
		if !cmdExist {
			return fmt.Errorf("required command %s not found, please install package %s", cmd, pkg)
		}
		log.Debugf("Host dependency %s is already installed", pkg)
	}
	return nil
}

func install(template *config.ImageTemplate, configDir, localRepo string) error {
	if err := dependencyCheck(template.Target.OS); err != nil {
		return fmt.Errorf("dependency check failed: %w", err)
	}

	globalConfig.ConfigDir = configDir

	hostAsChrootEnv, err := newChrootEnv(configDir,
		localRepo,
		template.Target.OS,
		template.Target.Dist,
		template.Target.Arch)
	if err != nil {
		return fmt.Errorf("failed to create chroot environment: %w", err)
	}

	if err := hostAsChrootEnv.InitChrootEnv(
		template.Target.OS,
		template.Target.Dist,
		template.Target.Arch); err != nil {
		return fmt.Errorf("failed to initialize chroot environment: %w", err)
	}

	if err := hostAsChrootEnv.UpdateSystemPkgs(template); err != nil {
		return fmt.Errorf("failed to update system packages: %w", err)
	}

	diskInfo := template.GetDiskConfig()
	diskPath, err := imagedisc.ResolveInstallDiskPath(diskInfo)
	if err != nil {
		return fmt.Errorf("failed to resolve target disk: %w", err)
	}
	template.Disk.Path = diskPath
	log.Infof("Using target disk: %s", diskPath)

	diskPathIdMap, err := imagedisc.DiskPartitionsCreate(diskPath, diskInfo.Partitions, diskInfo.PartitionTableType)
	if err != nil {
		return fmt.Errorf("failed to create partitions on disk %s: %w", diskPath, err)
	}

	// Create ImageOs with template
	imageOs, err := imageos.NewImageOs(hostAsChrootEnv, template)
	if err != nil {
		return fmt.Errorf("failed to create image OS instance: %w", err)
	}

	versionInfo, err := imageOs.InstallImageOs(diskPathIdMap)
	if err != nil {
		return fmt.Errorf("failed to install image OS: %w", err)
	}

	log.Infof("OS installation completed with version: %s", versionInfo)

	if err := updateBootOrder(template, diskPathIdMap); err != nil {
		return fmt.Errorf("failed to update boot order: %w", err)
	}

	return nil
}

func updateBootOrder(template *config.ImageTemplate, diskPathIdMap map[string]string) error {
	if template.SystemConfig.Bootloader.BootType != "efi" {
		log.Infof("Boot order update skipped: non-UEFI boot type detected")
		return nil
	}

	if err := removeOldBootEntries(); err != nil {
		return fmt.Errorf("failed to remove old boot entries: %w", err)
	}

	if err := createNewBootEntry(template, diskPathIdMap); err != nil {
		return fmt.Errorf("failed to create new boot entry: %w", err)
	}

	log.Infof("Boot order updated successfully")

	return nil
}

func removeOldBootEntries() error {
	// Entries created by previous runs of installer should be removed.
	// Do NOT remove entries for other OSes or hardware recovery tools.
	output, err := shell.ExecCmd("efibootmgr", true, shell.HostPath, nil)
	if err != nil {
		log.Errorf("Failed to list existing boot entries: %v", err)
		return fmt.Errorf("failed to list existing boot entries: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "Boot") && strings.Contains(line, "*") {
			// Extract bootnum and label
			parts := strings.Fields(line)
			if len(parts) < 2 {
				continue
			}
			bootnum := parts[0][4:8] // Boot0001 -> 0001
			label := strings.Join(parts[1:], " ")
			if strings.Contains(label, "ICT") {
				log.Infof("Removing old boot entry: %s (%s)", bootnum, label)
				cmdStr := fmt.Sprintf("efibootmgr --delete-bootnum --bootnum %s", bootnum)
				if _, err := shell.ExecCmd(cmdStr, true, shell.HostPath, nil); err != nil {
					log.Errorf("Failed to remove boot entry %s: %v", bootnum, err)
					return fmt.Errorf("failed to remove boot entry %s: %w", bootnum, err)
				}
				log.Infof("Successfully removed boot entry: %s", bootnum)
			}
		}
	}
	return nil
}

func createNewBootEntry(template *config.ImageTemplate, diskPathIdMap map[string]string) error {
	diskConfig := template.GetDiskConfig()
	diskPath := diskConfig.Path
	if diskPath == "" {
		return fmt.Errorf("no target disk path specified in the template")
	}
	var bootPartPath string
	for diskId, diskPartPath := range diskPathIdMap {
		for _, partition := range diskConfig.Partitions {
			if partition.ID == diskId {
				if partition.MountPoint == "/boot/efi" {
					bootPartPath = diskPartPath
					break
				}
			}
		}
	}
	if bootPartPath == "" {
		return fmt.Errorf("no EFI boot partition found in the disk partitions")
	}

	partNum := strings.TrimPrefix(bootPartPath, diskPath)
	if partNum[0] == 'p' {
		partNum = partNum[1:]
	}

	log.Infof("Creating new boot entry for disk %s partition %s", diskPath, partNum)
	cmdStr := fmt.Sprintf("efibootmgr --create --disk %s --part %s", diskPath, partNum)
	cmdStr += " --loader /EFI/BOOT/bootx64.efi"
	cmdStr += " --label 'ICT' --verbose"

	if _, err := shell.ExecCmdWithStream(cmdStr, true, shell.HostPath, nil); err != nil {
		log.Errorf("Failed to create new boot entry: %v", err)
		return fmt.Errorf("failed to create new boot entry: %w", err)
	}

	return nil
}

func unattendedInstall(templateFile, localRepo string) error {
	templateDir := filepath.Dir(templateFile)
	configDir := filepath.Join(templateDir, "..", "..", "..", "..", "..")
	generalConfigDir := filepath.Join(configDir, "general")
	if _, err := os.Stat(generalConfigDir); os.IsNotExist(err) {
		log.Errorf("General config directory does not exist: %s", generalConfigDir)
		return fmt.Errorf("general config directory does not exist: %s", generalConfigDir)
	}
	template, err := config.LoadTemplate(templateFile, false)
	if err != nil {
		return fmt.Errorf("failed to load template: %w", err)
	}
	log.Infof("Loaded template: %s (type: %s)", template.Image.Name, template.Target.ImageType)

	return install(template, configDir, localRepo)
}

func attendedInstall(templateFile, localRepo string) (installationQuit bool, err error) {
	templateDir := filepath.Dir(templateFile)
	configDir := filepath.Join(templateDir, "..", "..", "..", "..", "..")
	generalConfigDir := filepath.Join(configDir, "general")
	if _, err := os.Stat(generalConfigDir); os.IsNotExist(err) {
		log.Errorf("General config directory does not exist: %s", generalConfigDir)
		return false, fmt.Errorf("general config directory does not exist: %s", generalConfigDir)
	}
	template, err := config.LoadTemplate(templateFile, false)
	if err != nil {
		return false, fmt.Errorf("failed to load template: %w", err)
	}
	log.Infof("Loaded template: %s (type: %s)", template.Image.Name, template.Target.ImageType)

	attendedInstaller, err := attendedinstaller.New(template, configDir, localRepo, install)
	if err != nil {
		return false, fmt.Errorf("failed to create attended installer: %w", err)
	}

	finalConfig, installationQuit, err := attendedInstaller.Run()

	if finalConfig != nil {
		if err := finalConfig.SaveUpdatedConfigFile(filepath.Join("/tmp", "final-template.yml")); err != nil {
			log.Errorf("Failed to save final updated template: %v", err)
			return installationQuit, fmt.Errorf("failed to save final updated template: %w", err)
		}
	}

	return installationQuit, err
}
