package imageos

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/file"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

// fdeKeyfilePath is the in-image path of the single shared LUKS keyfile used for
// automatic unlock of every encrypted partition.
const fdeKeyfilePath = "/etc/cryptsetup-keys.d/fde.key"

// fdeCmdlineRewriteConfig holds inputs for rewriting cmdline.conf after FDE.
type fdeCmdlineRewriteConfig struct {
	rootMapper   string
	hashDev      string
	immutability bool
	luksParams   []string
}

// enablingFDE is the entry point for full-disk encryption. It runs after the OS
// is installed and before the UKI is built. Because cryptsetup cannot operate on
// a mounted (or in-use) partition, it unmounts the chroot stack, encrypts each
// target partition in place with cryptsetup reencrypt, reopens it as a
// /dev/mapper device, and remounts the stack so the remaining build steps run
// against the decrypted volumes.
//
// It returns the (possibly updated) mount-point list to use for teardown and the
// names of the LUKS mappers that were opened and must be closed during cleanup.
func (imageOs *ImageOs) enablingFDE(installRoot string, diskPathIdMap map[string]string,
	mountPointInfoList []map[string]string) ([]map[string]string, []string, error) {

	template := imageOs.template
	if !template.IsFDEEnabled() {
		return mountPointInfoList, nil, nil
	}

	passphrase := template.GetFDEPassphrase()
	if passphrase == "" {
		return mountPointInfoList, nil, fmt.Errorf("FDE is enabled but no passphrase could be loaded from systemConfig.fde.passphraseFile")
	}

	targets := fdeTargetPartitionIDs(template)
	if len(targets) == 0 {
		return mountPointInfoList, nil, fmt.Errorf("FDE is enabled but no target partition could be determined")
	}

	log.Infof("Enabling FDE for image: %s", template.GetImageName())

	if err := imageOs.umountDiskFromChroot(installRoot, mountPointInfoList); err != nil {
		return mountPointInfoList, nil, fmt.Errorf("failed to unmount before FDE: %w", err)
	}

	var opened []string
	for _, id := range targets {
		dev, ok := diskPathIdMap[id]
		if !ok {
			closeLuksMappers(opened)
			return mountPointInfoList, nil, fmt.Errorf("FDE partition %q has no device in the disk map", id)
		}
		mapperPath, mapperName, err := reencryptPartitionInPlace(id, dev, passphrase)
		if err != nil {
			closeLuksMappers(opened)
			return mountPointInfoList, nil, fmt.Errorf("failed to encrypt partition %q: %w", id, err)
		}
		opened = append(opened, mapperName)
		diskPathIdMap[id] = mapperPath
		log.Infof("Encrypted partition %q, opened at %s", id, mapperPath)
	}

	newList, err := imageOs.mountDiskToChroot(installRoot, diskPathIdMap, template)
	if err != nil {
		closeLuksMappers(opened)
		return mountPointInfoList, nil, fmt.Errorf("failed to remount after FDE: %w", err)
	}

	return newList, opened, nil
}

// wireFDEBoot performs boot-side wiring for full-disk encryption. See helper
// functions in this file for LUKS UUID discovery, cmdline mutation, and crypttab
// rendering.
func wireFDEBoot(installRoot string, diskPathIdMap map[string]string, template *config.ImageTemplate) error {
	if !template.IsFDEEnabled() {
		return nil
	}

	targets := fdeTargetPartitionIDs(template)
	if len(targets) == 0 {
		return fmt.Errorf("FDE is enabled but no target partition could be determined")
	}

	rootID := fdeRootPartitionID(template)
	auto := template.IsFDEAutoUnlock()
	passphrase := template.GetFDEPassphrase()

	keyfileChroot, keyfileHost := "", ""
	if auto {
		if passphrase == "" {
			return fmt.Errorf("FDE auto unlock requires a passphrase loaded from systemConfig.fde.passphraseFile")
		}
		var err error
		keyfileChroot, keyfileHost, err = generateFDEKeyfile(installRoot, passphrase)
		if err != nil {
			return fmt.Errorf("failed to generate FDE keyfile: %w", err)
		}
	}

	luksParams, crypttabLines, rootMapper, err := fdeCollectBootParams(
		targets, rootID, auto, keyfileChroot, keyfileHost, passphrase, diskPathIdMap)
	if err != nil {
		return err
	}

	cmdlinePath := filepath.Join(installRoot, "boot", "cmdline.conf")
	content, err := file.Read(cmdlinePath)
	if err != nil {
		return fmt.Errorf("failed to read cmdline %s: %w", cmdlinePath, err)
	}

	fields, err := fdeRewriteCmdlineFields(strings.Fields(content), fdeCmdlineRewriteConfig{
		rootMapper:   rootMapper,
		hashDev:      fdeHashPartitionDev(diskPathIdMap, template),
		immutability: template.IsImmutabilityEnabled(),
		luksParams:   luksParams,
	})
	if err != nil {
		return err
	}

	if err := file.Write(strings.Join(fields, " ")+"\n", cmdlinePath); err != nil {
		return fmt.Errorf("failed to write cmdline %s: %w", cmdlinePath, err)
	}

	if err := fdeWriteCrypttab(installRoot, crypttabLines); err != nil {
		return err
	}

	log.Infof("Wired FDE boot parameters (%s unlock) for image: %s", template.GetFDEUnlockMode(), template.GetImageName())
	return nil
}

// fdeCollectBootParams discovers LUKS metadata and builds rd.luks / crypttab entries.
func fdeCollectBootParams(
	targets []string,
	rootID string,
	auto bool,
	keyfileChroot, keyfileHost, passphrase string,
	diskPathIdMap map[string]string,
) (luksParams, crypttabLines []string, rootMapper string, err error) {

	for _, id := range targets {
		mapperPath, ok := diskPathIdMap[id]
		if !ok {
			return nil, nil, "", fmt.Errorf("FDE partition %q has no device in the disk map", id)
		}

		backing, luksUUID, err := fdeDiscoverLuksUUID(id)
		if err != nil {
			return nil, nil, "", err
		}

		keySpec := "none"
		if auto {
			if err := addFDEKeyToDevice(backing, keyfileHost, passphrase); err != nil {
				return nil, nil, "", fmt.Errorf("failed to add keyfile keyslot for %q: %w", id, err)
			}
			keySpec = keyfileChroot
		}

		if id == rootID {
			rootMapper = mapperPath
			luksParams = append(luksParams, fdeLuksCmdlineParams(luksUUID, id, keySpec, auto)...)
			if auto {
				crypttabLines = append(crypttabLines, fdeCrypttabEntry(id, luksUUID, keySpec))
			}
		} else {
			crypttabLines = append(crypttabLines, fdeCrypttabEntry(id, luksUUID, keySpec))
		}
	}

	return luksParams, crypttabLines, rootMapper, nil
}

// fdeDiscoverLuksUUID returns the backing block device and LUKS header UUID for
// an opened mapper named mapperName.
func fdeDiscoverLuksUUID(mapperName string) (backing, luksUUID string, err error) {
	statusOut, err := shell.ExecCmd(fmt.Sprintf("cryptsetup status %s", shell.QuoteArg(mapperName)), true, shell.HostPath, nil)
	if err != nil {
		return "", "", fmt.Errorf("failed to query LUKS status for %q: %w", mapperName, err)
	}

	backing = parseCryptsetupStatusDevice(statusOut)
	if backing == "" {
		return "", "", fmt.Errorf("could not determine backing device for LUKS mapper %q", mapperName)
	}

	uuidOut, err := shell.ExecCmd(fmt.Sprintf("cryptsetup luksUUID %s", shell.QuoteArg(backing)), true, shell.HostPath, nil)
	if err != nil {
		return "", "", fmt.Errorf("failed to read LUKS UUID for %q: %w", mapperName, err)
	}

	return backing, strings.TrimSpace(uuidOut), nil
}

// parseCryptsetupStatusDevice extracts the backing device path from cryptsetup status output.
func parseCryptsetupStatusDevice(statusOut string) string {
	for _, line := range strings.Split(statusOut, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "device:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "device:"))
		}
	}
	return ""
}

// fdeLuksCmdlineParams builds rd.luks.* kernel parameters for the root volume.
func fdeLuksCmdlineParams(luksUUID, mapperID, keyfileChroot string, auto bool) []string {
	params := []string{
		fmt.Sprintf("rd.luks.uuid=%s", luksUUID),
		fmt.Sprintf("rd.luks.name=%s=%s", luksUUID, mapperID),
	}
	if auto && keyfileChroot != "" {
		params = append(params, fmt.Sprintf("rd.luks.key=%s", keyfileChroot))
	}
	return params
}

// fdeCrypttabEntry formats a single /etc/crypttab line.
func fdeCrypttabEntry(name, luksUUID, keySpec string) string {
	return fmt.Sprintf("%s UUID=%s %s luks,discard", name, luksUUID, keySpec)
}

// fdeRewriteCmdlineFields updates root/verity/roothash tokens and prepends LUKS params.
func fdeRewriteCmdlineFields(fields []string, cfg fdeCmdlineRewriteConfig) ([]string, error) {
	for i, f := range fields {
		switch {
		case cfg.rootMapper != "" && !cfg.immutability && strings.HasPrefix(f, "root="):
			fields[i] = "root=" + cfg.rootMapper
		case cfg.rootMapper != "" && cfg.immutability && strings.HasPrefix(f, "systemd.verity_root_data="):
			fields[i] = "systemd.verity_root_data=" + cfg.rootMapper
		case cfg.rootMapper != "" && cfg.immutability && strings.HasPrefix(f, "roothash="):
			if cfg.hashDev == "" {
				return nil, fmt.Errorf("FDE with dm-verity requires a hash partition (e.g. roothashmap)")
			}
			fields[i] = fmt.Sprintf("roothash=%s-%s", cfg.rootMapper, cfg.hashDev)
		}
	}
	return append(cfg.luksParams, fields...), nil
}

// fdeWriteCrypttab writes crypttab entries into the image root, if any.
func fdeWriteCrypttab(installRoot string, lines []string) error {
	if len(lines) == 0 {
		return nil
	}
	crypttabPath := filepath.Join(installRoot, "etc", "crypttab")
	if err := file.Write(fdeRenderCrypttab(lines), crypttabPath); err != nil {
		return fmt.Errorf("failed to write crypttab %s: %w", crypttabPath, err)
	}
	return nil
}

// fdeRenderCrypttab joins crypttab lines with a trailing newline.
func fdeRenderCrypttab(lines []string) string {
	return strings.Join(lines, "\n") + "\n"
}

func fdeTargetPartitionIDs(template *config.ImageTemplate) []string {
	if ids := template.GetFDEPartitions(); len(ids) > 0 {
		return ids
	}
	for _, p := range template.GetDiskConfig().Partitions {
		if p.MountPoint == "/" {
			return []string{p.ID}
		}
	}
	return nil
}

func fdeRootPartitionID(template *config.ImageTemplate) string {
	for _, p := range template.GetDiskConfig().Partitions {
		if p.MountPoint == "/" {
			return p.ID
		}
	}
	return ""
}

func fdeHashPartitionDev(diskPathIdMap map[string]string, template *config.ImageTemplate) string {
	for _, p := range template.GetDiskConfig().Partitions {
		if p.ID == "roothashmap" || p.ID == "hash" {
			if dev, ok := diskPathIdMap[p.ID]; ok {
				return dev
			}
		}
	}
	for _, p := range template.GetDiskConfig().Partitions {
		if p.MountPoint == "none" {
			if dev, ok := diskPathIdMap[p.ID]; ok {
				return dev
			}
		}
	}
	return ""
}

func generateFDEKeyfile(installRoot, passphrase string) (chrootPath, hostPath string, err error) {
	keyDirHost := filepath.Join(installRoot, "etc", "cryptsetup-keys.d")
	hostPath = filepath.Join(keyDirHost, "fde.key")

	if err = os.MkdirAll(keyDirHost, 0700); err != nil {
		return "", "", fmt.Errorf("failed to create key directory: %w", err)
	}
	if err = os.WriteFile(hostPath, []byte(passphrase), 0400); err != nil {
		return "", "", fmt.Errorf("failed to write keyfile: %w", err)
	}

	return fdeKeyfilePath, hostPath, nil
}

func addFDEKeyToDevice(backing, keyfileHost, passphrase string) error {
	addCmd := fmt.Sprintf("cryptsetup luksAddKey --key-file - %s %s", shell.QuoteArg(backing), shell.QuoteArg(keyfileHost))
	if _, err := shell.ExecCmdWithInput(passphrase, addCmd, true, shell.HostPath, nil); err != nil {
		return fmt.Errorf("failed to add keyfile keyslot: %w", err)
	}
	return nil
}

func reencryptPartitionInPlace(id, dev, passphrase string) (mapperPath, mapperName string, err error) {
	mapperName = id
	mapperPath = filepath.Join("/dev/mapper", mapperName)

	fsckCmd := fmt.Sprintf("e2fsck -fy %s", shell.QuoteArg(dev))
	// e2fsck exit codes 1 and 2 mean errors were found and corrected, which is
	// acceptable here; only codes above 2 indicate a real failure.
	if _, err = shell.ExecCmd(fsckCmd, true, shell.HostPath, nil); err != nil && exitCodeAbove(err, 2) {
		return "", "", fmt.Errorf("e2fsck failed: %w", err)
	}

	shrinkCmd := fmt.Sprintf("resize2fs -M %s", shell.QuoteArg(dev))
	if _, err = shell.ExecCmd(shrinkCmd, true, shell.HostPath, nil); err != nil {
		return "", "", fmt.Errorf("resize2fs shrink failed: %w", err)
	}

	reCmd := fmt.Sprintf("cryptsetup reencrypt --encrypt --type luks2 --reduce-device-size 32M --batch-mode --key-file - %s",
		shell.QuoteArg(dev))
	if _, err = shell.ExecCmdWithInput(passphrase, reCmd, true, shell.HostPath, nil); err != nil {
		return "", "", fmt.Errorf("cryptsetup reencrypt failed: %w", err)
	}

	openCmd := fmt.Sprintf("cryptsetup open --key-file - %s %s", shell.QuoteArg(dev), shell.QuoteArg(mapperName))
	if _, err = shell.ExecCmdWithInput(passphrase, openCmd, true, shell.HostPath, nil); err != nil {
		return "", "", fmt.Errorf("cryptsetup open failed: %w", err)
	}

	growCmd := fmt.Sprintf("resize2fs %s", shell.QuoteArg(mapperPath))
	if _, err = shell.ExecCmd(growCmd, true, shell.HostPath, nil); err != nil {
		if closeErr := closeLuks(mapperName); closeErr != nil {
			log.Warnf("Failed to close LUKS mapper %s after grow error: %v", mapperName, closeErr)
		}
		return "", "", fmt.Errorf("resize2fs grow failed: %w", err)
	}

	return mapperPath, mapperName, nil
}

func exitCodeAbove(err error, max int) bool {
	if err == nil {
		return false
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode() > max
	}
	return true
}

func closeLuks(mapperName string) error {
	cmd := fmt.Sprintf("cryptsetup close %s", shell.QuoteArg(mapperName))
	if _, err := shell.ExecCmd(cmd, true, shell.HostPath, nil); err != nil {
		return fmt.Errorf("cryptsetup close %s failed: %w", mapperName, err)
	}
	return nil
}

func closeLuksMappers(names []string) {
	for i := len(names) - 1; i >= 0; i-- {
		if err := closeLuks(names[i]); err != nil {
			log.Warnf("%v", err)
		}
	}
}
