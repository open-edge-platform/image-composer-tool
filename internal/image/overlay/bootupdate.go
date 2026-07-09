package overlay

import (
	"fmt"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

// bootRegenExec and commandExistsFn are the indirection seams over the impure
// dependencies of boot regeneration (in-chroot command execution and the
// generator-presence probe) so the orchestration is unit-testable without a real
// chroot. Tests override them; production runs through the shell allowlist,
// capturing output to the build log.
var (
	bootRegenExec = func(cmd, chrootPath string) (string, error) {
		return shell.ExecCmdWithStream(cmd, true, chrootPath, nil)
	}
	commandExistsFn = shell.IsCommandExist
)

// RegenerateBoot refreshes the initramfs in the baseline chroot so that modules or
// hooks pulled in by the newly added overlay packages take effect at boot.
//
// It is deliberately conservative and additive: it ONLY regenerates the initramfs
// (via the baseline family's native tool) and never touches the bootloader binary
// or the ESP — overlay mode treats the installed bootloader as immutable (the ESP
// is mounted read-only upstream). Regenerating the bootloader's own config (e.g.
// grub.cfg) is out of scope for this step but is not precluded by that contract:
// such config lives on the writable root, not the ESP, and would be a separate
// step. When the install added nothing, regeneration is skipped entirely.
//
// Regeneration is best-effort with respect to tool availability: if the baseline
// has no initramfs generator on PATH, that is logged and treated as a no-op rather
// than failing the build, since not every image ships one. A generator that is
// present but fails IS surfaced as an error.
//
// The generator runs in the chroot with the kernel pseudo-filesystems (/proc,
// /sys, /dev, ...) mounted: the install step mounts them only for its own duration
// and tears them down on return, so this stage establishes its own set (dracut in
// particular reads /proc and /sys) and removes them afterward.
func RegenerateBoot(info *BaselineInfo, rootMount string, installed *InstallResult) (err error) {
	if info == nil {
		return fmt.Errorf("overlay boot regen: baseline info cannot be nil")
	}
	if strings.TrimSpace(rootMount) == "" {
		return fmt.Errorf("overlay boot regen: baseline root mount path cannot be empty")
	}

	// Nothing was added (or the install was skipped): no initramfs change needed.
	if installed == nil || installed.Skipped || len(installed.Installed) == 0 {
		log.Infof("Overlay boot regen: no packages added, skipping initramfs regeneration")
		return nil
	}

	cmd, tool, err := initramfsCommand(info.PackageManager)
	if err != nil {
		return err
	}

	// Skip cleanly when the baseline does not ship the generator; some minimal
	// images legitimately have none.
	present, err := commandExistsFn(tool, rootMount)
	if err != nil {
		return fmt.Errorf("overlay boot regen: failed to probe for %s in baseline: %w", tool, err)
	}
	if !present {
		log.Warnf("Overlay boot regen: %s not present in baseline; skipping initramfs regeneration", tool)
		return nil
	}

	// Mount the pseudo-filesystems for the generator and tear them down after; the
	// teardown error is surfaced only when the generator itself succeeded.
	if err := mountSysfs(rootMount); err != nil {
		return fmt.Errorf("overlay boot regen: failed to mount pseudo-filesystems into %s: %w", rootMount, err)
	}
	defer func() {
		if uerr := umountSysfs(rootMount); uerr != nil {
			log.Errorf("Overlay boot regen: failed to unmount pseudo-filesystems from %s: %v", rootMount, uerr)
			if err == nil {
				err = fmt.Errorf("overlay boot regen: failed to unmount pseudo-filesystems from %s: %w", rootMount, uerr)
			}
		}
	}()

	log.Infof("Overlay boot regen: regenerating initramfs in %s with %s", rootMount, tool)
	if _, err := bootRegenExec(cmd, rootMount); err != nil {
		return fmt.Errorf("overlay boot regen: %s failed: %w", tool, err)
	}
	return nil
}

// initramfsCommand returns the initramfs-regeneration command and the tool name it
// depends on for a package-manager family. The bootloader is intentionally out of
// scope; only the initramfs is regenerated.
//
//   - apt/dpkg: update-initramfs -u -k all (rebuilds for every installed kernel)
//   - dnf/rpm:  dracut --force --regenerate-all (rebuilds every initramfs in place)
func initramfsCommand(family PackageManager) (cmd, tool string, err error) {
	switch family {
	case PackageManagerAPT:
		return "update-initramfs -u -k all", "update-initramfs", nil
	case PackageManagerDNF:
		return "dracut --force --regenerate-all", "dracut", nil
	default:
		return "", "", fmt.Errorf("overlay boot regen: unsupported package manager %q (expected %q or %q)",
			family, PackageManagerAPT, PackageManagerDNF)
	}
}
