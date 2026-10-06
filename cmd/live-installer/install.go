package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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

// waitForDiskQuiescence waits for the disk to stabilize by checking for I/O inactivity.
// This ensures hypervisors (QEMU, KVM, etc.) have fully initialized virtual disks before
// partitioning begins. Errors from the I/O check are treated as "not-ready" (busy) and
// retried until the timeout, preventing the check from being skipped on transient failures
// such as /proc/diskstats not yet reporting the device.
func waitForDiskQuiescence(diskPath string) error {
	const (
		maxRetries = 20 // ~10 seconds with 500ms sleeps
		sleepTime  = 500 * time.Millisecond
	)

	var quietCount int
	const quietThreshold = 2 // Two consecutive checks with no I/O

	for attempt := 0; attempt < maxRetries; attempt++ {
		isBusy, err := imagedisc.CheckDiskIOStats(diskPath)
		if err != nil {
			// Treat errors as busy/not-ready: the device may not yet be visible in
			// /proc/diskstats. Reset quiet count and keep retrying until timeout.
			log.Debugf("Disk %s I/O stats not yet available (attempt %d): %v", diskPath, attempt+1, err)
			quietCount = 0
			time.Sleep(sleepTime)
			continue
		}

		if !isBusy {
			quietCount++
			if quietCount >= quietThreshold {
				log.Debugf("Disk %s is quiescent after %d attempts", diskPath, attempt+1)
				return nil
			}
		} else {
			quietCount = 0
		}

		time.Sleep(sleepTime)
	}

	// Timeout reached; disk may not be fully quiescent, but proceed anyway (non-fatal)
	log.Warnf("Disk %s did not reach quiescence within timeout; proceeding anyway", diskPath)
	return nil
}

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

func targetUsesApt(targetOS string) bool {
	switch targetOS {
	case "ubuntu", "debian", "wind-river-elxr":
		return true
	default:
		return false
	}
}

var aptBackgroundUnits = []string{
	"apt-daily.service",
	"apt-daily.timer",
	"apt-daily-upgrade.service",
	"apt-daily-upgrade.timer",
	"unattended-upgrades.service",
	"unattended-upgrades.timer",
}

func isIgnorableSystemctlSuppressionFailure(output string) bool {
	lowerOutput := strings.ToLower(strings.TrimSpace(output))
	if lowerOutput == "" {
		return false
	}

	markers := []string{
		"not loaded",
		"not found",
		"does not exist",
		"inactive",
		"not active",
	}

	for _, marker := range markers {
		if strings.Contains(lowerOutput, marker) {
			return true
		}
	}

	return false
}

func runSystemctlSuppressionCommand(cmd string) error {
	output, err := shell.ExecCmd(cmd, true, shell.HostPath, nil)
	if err == nil {
		return nil
	}
	if isIgnorableSystemctlSuppressionFailure(output) {
		log.Warnf("Ignoring non-fatal systemctl suppression failure for %q: %s", cmd, strings.TrimSpace(output))
		return nil
	}
	return fmt.Errorf("command %q failed: %w", cmd, err)
}

func suppressHostAptBackgroundTasks() error {
	systemctlExists, err := shell.IsCommandExist("systemctl", shell.HostPath)
	if err != nil {
		return fmt.Errorf("failed to check systemctl availability: %w", err)
	}
	if !systemctlExists {
		log.Debugf("systemctl is not available; skipping host apt background task suppression")
		return nil
	}

	commands := []string{
		"systemctl stop " + strings.Join(aptBackgroundUnits, " "),
		"systemctl mask --runtime " + strings.Join(aptBackgroundUnits, " "),
	}

	for _, cmd := range commands {
		if err := runSystemctlSuppressionCommand(cmd); err != nil {
			return fmt.Errorf("failed to suppress host apt background tasks: %w", err)
		}
	}

	log.Infof("Suppressed host apt background services and timers")
	return nil
}

func install(template *config.ImageTemplate, configDir, localRepo string) error {
	if err := dependencyCheck(template.Target.OS); err != nil {
		return fmt.Errorf("dependency check failed: %w", err)
	}

	if targetUsesApt(template.Target.OS) {
		if err := suppressHostAptBackgroundTasks(); err != nil {
			return fmt.Errorf("failed to suppress host apt background tasks: %w", err)
		}
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

	// Wait for disk to stabilize before partitioning. QEMU and other hypervisors may take
	// time to fully initialize virtual disks. Check for disk I/O quiescence (no active reads/writes)
	// with a timeout to avoid indefinite waits.
	log.Infof("Waiting for disk %s to stabilize...", diskPath)
	if err := waitForDiskQuiescence(diskPath); err != nil {
		log.Warnf("Disk quiescence check failed (continuing anyway): %v", err)
	}

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

	policy := template.GetBootEntryPolicy()
	if err := validateBootEntryPolicy(policy); err != nil {
		return err
	}

	// Entries from previous runs are deleted last, once the new entry is in the
	// BootOrder, so a failure part-way never leaves the disk without one.
	oldEntries, err := oldBootEntries()
	if err != nil {
		return fmt.Errorf("failed to list old boot entries: %w", err)
	}

	if err := createNewBootEntry(template, diskPathIdMap); err != nil {
		return fmt.Errorf("failed to create new boot entry: %w", err)
	}

	if err := setBootOrder(policy, oldEntries); err != nil {
		return fmt.Errorf("failed to set boot order: %w", err)
	}

	if err := removeBootEntries(oldEntries); err != nil {
		return fmt.Errorf("failed to remove old boot entries: %w", err)
	}

	log.Infof("Boot order updated successfully")

	return nil
}

// hydrateSBOMMetadataForInstaller restores the per-package SBOM metadata the
// installed system's SPDX document is built from. FullPkgListBom is not
// serialized, so after loading template-dump.yaml it is empty and has to be
// refilled from what the ISO carries.
//
// templateFile is the template that was just loaded; the metadata sidecar sits
// beside it. An ISO built before the sidecar existed carries the same payload
// inline in the template's sbomPackageMetadata field instead, so that is tried
// second — without it, such an ISO would silently fall back to a names-only
// inventory and emit a thinner SBOM than the image it was built from.
func hydrateSBOMMetadataForInstaller(template *config.ImageTemplate, templateFile string) {
	if template == nil || len(template.FullPkgListBom) > 0 {
		return
	}

	sidecarPath := filepath.Join(filepath.Dir(templateFile), config.SBOMMetadataFileName)
	pkgs, err := config.ReadSBOMMetadataSidecar(sidecarPath)
	if err != nil {
		log.Warnf("Failed to read SBOM metadata sidecar %s: %v", sidecarPath, err)
	}
	if len(pkgs) > 0 {
		template.FullPkgListBom = pkgs
		return
	}

	if len(template.SBOMPackageMetadata) > 0 {
		log.Infof("No SBOM metadata sidecar found; using the template's inline metadata")
		template.FullPkgListBom = template.SBOMPackageMetadata
	}
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
	hydrateSBOMMetadataForInstaller(template, templateFile)
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
	hydrateSBOMMetadataForInstaller(template, templateFile)
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
