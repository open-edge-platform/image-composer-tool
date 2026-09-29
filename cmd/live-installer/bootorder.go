package main

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/image/imagedisc"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

// bootEntryLabel is the firmware label of the entry the installer creates.
// Only entries with exactly this label are ever deleted, so other operating
// systems and vendor recovery entries are never touched.
const bootEntryLabel = "ICT"

// efiLoaders maps target.arch to the removable-media loader path every ICT
// bootloader flavour installs (grub-install --removable, shim, systemd-boot).
// efibootmgr converts the forward slashes; FAT is case-insensitive.
var efiLoaders = map[string]string{
	"x86_64":  "/EFI/BOOT/BOOTX64.EFI",
	"aarch64": "/EFI/BOOT/BOOTAA64.EFI",
}

// Seams so tests can script efibootmgr and sysfs.
var (
	execCmdFn         = shell.ExecCmd
	execStreamFn      = shell.ExecCmdWithStream
	partitionNumberFn = imagedisc.PartitionNumber
)

var bootEntryRe = regexp.MustCompile(`^Boot([0-9A-Fa-f]{4})\*?\s+(.*)$`)

type efiBootEntry struct {
	num, label string
}

// efiState is the parsed output of `efibootmgr`.
type efiState struct {
	order   []string
	entries []efiBootEntry
}

func parseEfibootmgr(output string) efiState {
	var st efiState
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, "\r")
		if rest, ok := strings.CutPrefix(line, "BootOrder:"); ok {
			for _, n := range strings.Split(strings.TrimSpace(rest), ",") {
				if n = strings.TrimSpace(n); n != "" {
					st.order = append(st.order, strings.ToUpper(n))
				}
			}
			continue
		}
		m := bootEntryRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		// Newer efibootmgr prints the device path after a tab.
		label, _, _ := strings.Cut(m[2], "\t")
		st.entries = append(st.entries, efiBootEntry{num: strings.ToUpper(m[1]), label: strings.TrimSpace(label)})
	}
	return st
}

func readEfiState() (efiState, error) {
	output, err := execCmdFn("efibootmgr", true, shell.HostPath, nil)
	if err != nil {
		log.Errorf("Failed to list existing boot entries: %v", err)
		return efiState{}, fmt.Errorf("failed to list existing boot entries: %w", err)
	}
	return parseEfibootmgr(output), nil
}

// removeOldBootEntries deletes entries created by previous installer runs.
func removeOldBootEntries() error {
	st, err := readEfiState()
	if err != nil {
		return err
	}
	for _, e := range st.entries {
		if e.label != bootEntryLabel {
			continue
		}
		log.Infof("Removing old boot entry: %s (%s)", e.num, e.label)
		cmdStr := fmt.Sprintf("efibootmgr --delete-bootnum --bootnum %s", e.num)
		if _, err := execCmdFn(cmdStr, true, shell.HostPath, nil); err != nil {
			log.Errorf("Failed to remove boot entry %s: %v", e.num, err)
			return fmt.Errorf("failed to remove boot entry %s: %w", e.num, err)
		}
	}
	return nil
}

// espPartitionPath finds the EFI system partition, preferring the declared
// partition type over the conventional mount point.
func espPartitionPath(partitions []config.PartitionInfo, diskPathIdMap map[string]string) string {
	fallback := ""
	for _, p := range partitions {
		dev, ok := diskPathIdMap[p.ID]
		if !ok {
			continue
		}
		if p.Type == "esp" {
			return dev
		}
		if fallback == "" && p.MountPoint == "/boot/efi" {
			fallback = dev
		}
	}
	return fallback
}

func createNewBootEntry(template *config.ImageTemplate, diskPathIdMap map[string]string) error {
	diskConfig := template.GetDiskConfig()
	diskPath := diskConfig.Path
	if diskPath == "" {
		return fmt.Errorf("no target disk path specified in the template")
	}
	bootPartPath := espPartitionPath(diskConfig.Partitions, diskPathIdMap)
	if bootPartPath == "" {
		return fmt.Errorf("no EFI boot partition found in the disk partitions")
	}
	loader, ok := efiLoaders[template.Target.Arch]
	if !ok {
		return fmt.Errorf("no EFI loader path known for architecture %q", template.Target.Arch)
	}
	partNum, err := partitionNumberFn(bootPartPath)
	if err != nil {
		return err
	}

	log.Infof("Creating new boot entry for disk %s partition %s", diskPath, partNum)
	cmdStr := fmt.Sprintf("efibootmgr --create --disk %s --part %s --loader %s --label '%s' --verbose",
		diskPath, partNum, loader, bootEntryLabel)
	if _, err := execStreamFn(cmdStr, true, shell.HostPath, nil); err != nil {
		log.Errorf("Failed to create new boot entry: %v", err)
		return fmt.Errorf("failed to create new boot entry: %w", err)
	}
	return nil
}

// setBootOrder puts the new entry first explicitly rather than relying on
// firmware honouring the implicit ordering of efibootmgr --create.
func setBootOrder(policy string) error {
	// Validate before touching firmware: an unknown value must not silently
	// behave like "preserve".
	if policy != config.BootEntryPolicyPreserve && policy != config.BootEntryPolicyExclusive {
		return fmt.Errorf("unknown boot entry policy %q: must be %q or %q",
			policy, config.BootEntryPolicyPreserve, config.BootEntryPolicyExclusive)
	}
	st, err := readEfiState()
	if err != nil {
		return err
	}
	newNum := ""
	for _, e := range st.entries {
		if e.label == bootEntryLabel {
			newNum = e.num
		}
	}
	if newNum == "" {
		return fmt.Errorf("created boot entry %q not found in firmware boot entries", bootEntryLabel)
	}
	order := bootOrderFor(policy, newNum, st.order)
	log.Infof("Setting firmware boot order (%s): %s", policy, strings.Join(order, ","))
	cmdStr := fmt.Sprintf("efibootmgr --bootorder %s", strings.Join(order, ","))
	if _, err := execCmdFn(cmdStr, true, shell.HostPath, nil); err != nil {
		return fmt.Errorf("failed to set boot order: %w", err)
	}
	return nil
}

func bootOrderFor(policy, newNum string, current []string) []string {
	order := []string{newNum}
	if policy == config.BootEntryPolicyExclusive {
		return order
	}
	for _, n := range current {
		if n != newNum {
			order = append(order, n)
		}
	}
	return order
}
