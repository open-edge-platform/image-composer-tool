package overlay

import (
	"fmt"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
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

	// readArtifactFileList is the impure seam over the host-side file-manifest read
	// of a prepared package artifact (dpkg -c / rpm -qlp; both on the shell
	// allowlist), so the boot-relevance gate is unit-testable without real
	// artifacts. The file is read on the host, so no chroot is entered.
	readArtifactFileList = func(family PackageManager, hostPath string) ([]string, error) {
		var cmd string
		switch family {
		case PackageManagerAPT:
			cmd = fmt.Sprintf("dpkg -c %s", shell.QuoteArg(hostPath))
		case PackageManagerDNF:
			cmd = fmt.Sprintf("rpm -qlp %s", shell.QuoteArg(hostPath))
		default:
			return nil, fmt.Errorf("overlay boot regen: unsupported package manager %q for file-list read", family)
		}
		out, err := shell.ExecCmdSilent(cmd, true, shell.HostPath, nil)
		if err != nil {
			return nil, fmt.Errorf("reading file list of %s: %w", hostPath, err)
		}
		return parseArtifactFileList(family, out), nil
	}
)

// bootRelevantPathPrefixes are the in-artifact file-path prefixes whose presence
// means an installed package can change the boot-time initramfs: kernel modules,
// firmware the initramfs may load, and initramfs-generator hooks/scripts. An
// overlay that adds none of these cannot alter the initramfs, so regeneration is
// skipped (see overlayAddedBootRelevantContent). Prefixes cover both the /lib and
// merged-usr /usr/lib locations.
var bootRelevantPathPrefixes = []string{
	"/lib/modules/",               // kernel modules
	"/usr/lib/modules/",           // kernel modules (merged-usr)
	"/lib/firmware/",              // device firmware the initramfs may load
	"/usr/lib/firmware/",          // device firmware (merged-usr)
	"/etc/initramfs-tools/",       // deb: initramfs-tools hooks & scripts
	"/usr/share/initramfs-tools/", // deb: initramfs-tools framework hooks
	"/usr/lib/dracut/",            // rpm: dracut modules
	"/etc/dracut.conf.d/",         // rpm: dracut drop-in config
}

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
// Regeneration is also skipped when none of the installed artifacts ship
// boot-relevant content — kernel modules, firmware, or initramfs hooks (see
// overlayAddedBootRelevantContent). A pure-userspace overlay (libraries, CLIs,
// -dev headers) cannot change the boot-time initramfs, so rebuilding it is wasted
// work; worse, on a systemd-boot/BLS baseline update-initramfs chains a loader
// hook that copies the kernel into the read-only ESP and fails. The plan carries
// the artifact download directory the gate reads the file manifests from; when it
// is unavailable or a manifest cannot be read, the gate fails safe and regenerates.
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
// forceRegen forces initramfs regeneration even when the two "nothing changed"
// gates below would otherwise skip it. The overlay build sets it when a
// stage: pre-initramfs additionalFiles entry was copied: such a file (e.g. a
// dracut module or initramfs-tools hook delivered purely through additionalFiles,
// with no boot-relevant PACKAGE installed) must still be baked into the initramfs,
// which the package-manifest-based gate cannot see.
//
// When overlayPolicy.replaceKernel.enableExtraModules is set, its driver modules
// are forced into the regenerated initramfs (dracut --add-drivers, or an
// initramfs-tools /etc/initramfs-tools/modules entry) — mirroring create mode's
// systemConfig.kernel.enableExtraModules, scoped to the replacement kernel swap.
func RegenerateBoot(template *config.ImageTemplate, info *BaselineInfo, rootMount string, installed *InstallResult, plan *ResolutionPlan, forceRegen bool) (err error) {
	if info == nil {
		return fmt.Errorf("overlay boot regen: baseline info cannot be nil")
	}
	if strings.TrimSpace(rootMount) == "" {
		return fmt.Errorf("overlay boot regen: baseline root mount path cannot be empty")
	}

	// A non-empty enableExtraModules must always be baked into the initramfs, so it
	// is read here — before the two "nothing changed" gates below — and folded into
	// forceRegen exactly like a stage:pre-initramfs additionalFiles entry. Without
	// this, a removal-only swap (the requested kernel already installed, so
	// installed.Installed is empty) or a swap whose artifacts the manifest-based
	// gate reads as non-boot-relevant would skip regeneration and silently drop the
	// requested modules.
	extraModules := ""
	if template != nil && template.OverlayPolicy != nil && template.OverlayPolicy.ReplaceKernel != nil {
		extraModules = strings.TrimSpace(template.OverlayPolicy.ReplaceKernel.EnableExtraModules)
	}
	forceReason := ""
	switch {
	case forceRegen:
		forceReason = "a stage:pre-initramfs additionalFiles entry was copied"
	case extraModules != "":
		forceRegen = true
		forceReason = "overlayPolicy.replaceKernel.enableExtraModules is set"
	}

	// Nothing was added (or the install was skipped): no initramfs change needed —
	// unless forceRegen is set (see forceReason above), which the generator must
	// still honor.
	if !forceRegen && (installed == nil || installed.Skipped || len(installed.Installed) == 0) {
		log.Infof("Overlay boot regen: no packages added, skipping initramfs regeneration")
		return nil
	}

	// Skip when the overlay added no kernel modules, firmware, or initramfs hooks:
	// the boot-time initramfs cannot have changed, so regeneration is unnecessary
	// (and on an ESP/BLS baseline would fail on the read-only ESP). The gate fails
	// safe — an unreadable manifest regenerates rather than risk a stale initramfs.
	// forceRegen bypasses it: its reason (see forceReason above) is boot-relevant
	// content the package manifests do not reflect.
	if !forceRegen && !overlayAddedBootRelevantContent(info.PackageManager, plan) {
		log.Infof("Overlay boot regen: no kernel modules, firmware, or initramfs hooks added; skipping initramfs regeneration")
		return nil
	}
	if forceRegen {
		log.Infof("Overlay boot regen: %s; regenerating initramfs so it takes effect", forceReason)
	}

	// Reject a package-manager family overlay does not support before probing.
	if err := supportedInitramfsFamily(info.PackageManager); err != nil {
		return err
	}

	// Select the generator by what the baseline actually ships, NOT by the
	// package-manager family: an overlay that installs dracut (removing
	// initramfs-tools) on a Debian baseline must regenerate with dracut, and one
	// that keeps initramfs-tools must use update-initramfs. This mirrors a
	// create-mode build, where the installed generator's own maintainer scripts run.
	// The first generator present in the baseline wins (see initramfsGenerators).
	cmd, tool, found, err := resolveInitramfsGenerator(rootMount)
	if err != nil {
		return err
	}
	// No known generator in the baseline. For an ordinary build this is a clean
	// no-op (some minimal images legitimately have none). But when forceRegen is set
	// (see forceReason above) — a stage:pre-initramfs file was copied, or
	// enableExtraModules was requested — silently succeeding would let the build
	// claim that content took effect when it did not, so fail with an actionable
	// error instead.
	if !found {
		if forceRegen {
			return fmt.Errorf("overlay boot regen: %s, which requires initramfs regeneration, but no supported generator "+
				"(%s) is present in the baseline; install one (e.g. dracut or initramfs-tools)",
				forceReason, knownInitramfsToolNames())
		}
		log.Warnf("Overlay boot regen: no supported initramfs generator (%s) present in baseline; skipping initramfs regeneration", knownInitramfsToolNames())
		return nil
	}

	if extraModules != "" {
		switch tool {
		case "dracut":
			// dracut accepts the space-separated module list directly via --add-drivers.
			cmd = fmt.Sprintf("%s --add-drivers %s", cmd, shell.QuoteArg(extraModules))
		case "update-initramfs":
			// initramfs-tools has no equivalent flag: each module is appended to its
			// modules config file, which update-initramfs -u picks up on the next run.
			log.Infof("Overlay boot regen: adding modules to initramfs-tools config: %s", extraModules)
			for _, mod := range strings.Fields(extraModules) {
				// printf (not echo) avoids leading-dash arguments being misread as options
				// by shell-dependent echo implementations.
				appendCmd := fmt.Sprintf("printf '%%s\\n' %s >> /etc/initramfs-tools/modules", shell.QuoteArg(mod))
				if _, aerr := bootRegenExec(appendCmd, rootMount); aerr != nil {
					return fmt.Errorf("overlay boot regen: failed to add module %q to /etc/initramfs-tools/modules: %w", mod, aerr)
				}
			}
		}
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
	// Surface the generator's captured output: on its own the wrapped error is
	// only "exit status 1", and the generator's actual diagnostic (the failing
	// hook or missing kernel) is otherwise streamed only to debug logging. Mirrors
	// the deb/rpm installer backends, which append formatCommandOutput likewise.
	if out, rerr := bootRegenExec(cmd, rootMount); rerr != nil {
		return fmt.Errorf("overlay boot regen: %s failed: %w%s", tool, rerr, formatCommandOutput(out))
	}
	return nil
}

// overlayAddedBootRelevantContent reports whether any artifact the overlay
// installed ships content that can change the boot-time initramfs — a kernel
// module, firmware, or an initramfs-generator hook (see bootRelevantPathPrefixes).
// It reads each artifact's file manifest on the host, mirroring the dependency
// reader's seam.
//
// It is fail-safe: it returns true (regenerate) whenever it cannot prove the
// overlay is purely userspace — no plan, no recorded download directory, or a
// manifest that could not be read. Skipping is only ever chosen when every
// artifact was read and none carried boot-relevant content, so an initramfs that
// genuinely needs rebuilding is never left stale.
func overlayAddedBootRelevantContent(family PackageManager, plan *ResolutionPlan) bool {
	if plan == nil || len(plan.ToInstall) == 0 {
		return true // no information to prove userspace-only; regenerate.
	}
	if strings.TrimSpace(plan.DownloadDir) == "" {
		log.Warnf("Overlay boot regen: plan has no artifact download directory; regenerating initramfs to be safe")
		return true
	}

	for _, rp := range plan.ToInstall {
		artifact, err := artifactFileFor(rp)
		if err != nil {
			log.Warnf("Overlay boot regen: cannot locate artifact for %q (%v); regenerating initramfs to be safe", rp.Name, err)
			return true
		}
		hostPath := joinArtifactPath(plan.DownloadDir, artifact)
		files, err := readArtifactFileList(family, hostPath)
		if err != nil {
			log.Warnf("Overlay boot regen: could not read file list of %q from %s (%v); regenerating initramfs to be safe", rp.Name, hostPath, err)
			return true
		}
		if pathListHasBootRelevantContent(files) {
			log.Infof("Overlay boot regen: %q ships boot-relevant content; initramfs regeneration required", rp.Name)
			return true
		}
	}
	return false
}

// pathListHasBootRelevantContent reports whether any file path is under one of the
// boot-relevant prefixes. Paths are normalized to a leading "/" first, since
// dpkg -c emits "./lib/modules/..." while rpm -qlp emits "/lib/modules/...".
func pathListHasBootRelevantContent(files []string) bool {
	for _, f := range files {
		f = normalizeArtifactPath(f)
		for _, prefix := range bootRelevantPathPrefixes {
			if strings.HasPrefix(f, prefix) {
				return true
			}
		}
	}
	return false
}

// parseArtifactFileList extracts the archive member paths from a package
// file-manifest listing. rpm -qlp prints one absolute path per line. dpkg -c
// prints `tar -tv`-style lines ("drwxr-xr-x root/root 0 ... ./path"); the path is
// the last whitespace-separated field, with a symlink's " -> target" suffix
// dropped so the owned path (not the target) is matched.
func parseArtifactFileList(family PackageManager, out string) []string {
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if family == PackageManagerAPT {
			// Keep the member path (last field); drop a "link -> target" suffix.
			if arrow := strings.Index(line, " -> "); arrow != -1 {
				line = line[:arrow]
			}
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			line = fields[len(fields)-1]
		}
		paths = append(paths, line)
	}
	return paths
}

// normalizeArtifactPath maps a manifest path to a leading-slash absolute form so
// dpkg's "./lib/..." and rpm's "/lib/..." compare against the same prefixes.
func normalizeArtifactPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, ".")
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

// initramfsGenerator pairs an initramfs-generator tool with the command that
// rebuilds every installed kernel's initramfs in place.
type initramfsGenerator struct {
	tool string
	cmd  string
}

// initramfsGenerators are the supported generators in PREFERENCE order. Selection
// is by what the baseline actually ships, not by package-manager family, so an
// overlay that swaps the generator (e.g. installs dracut and removes
// initramfs-tools on a Debian baseline via allowPackageRemoval) regenerates with
// the tool that is now installed. dracut is listed first: when both are present
// (a transient state mid-swap) dracut is the intended generator, and it is also
// the cross-distro tool. Each command rebuilds the initramfs for every installed
// kernel in place; the bootloader is out of scope (only the initramfs is rebuilt).
var initramfsGenerators = []initramfsGenerator{
	{tool: "dracut", cmd: "dracut --force --regenerate-all"},
	{tool: "update-initramfs", cmd: "update-initramfs -u -k all"},
}

// supportedInitramfsFamily rejects a package-manager family overlay does not
// support, preserving the previous explicit error for an unknown family.
func supportedInitramfsFamily(family PackageManager) error {
	switch family {
	case PackageManagerAPT, PackageManagerDNF:
		return nil
	default:
		return fmt.Errorf("overlay boot regen: unsupported package manager %q (expected %q or %q)",
			family, PackageManagerAPT, PackageManagerDNF)
	}
}

// resolveInitramfsGenerator probes the baseline for each known generator in
// preference order and returns the first one present, along with its regeneration
// command. found is false when the baseline ships none. A probe error is surfaced
// so a failed detection is never mistaken for "absent".
func resolveInitramfsGenerator(rootMount string) (cmd, tool string, found bool, err error) {
	for _, g := range initramfsGenerators {
		present, perr := commandExistsFn(g.tool, rootMount)
		if perr != nil {
			return "", "", false, fmt.Errorf("overlay boot regen: failed to probe for %s in baseline: %w", g.tool, perr)
		}
		if present {
			return g.cmd, g.tool, true, nil
		}
	}
	return "", "", false, nil
}

// knownInitramfsToolNames lists the supported generator tool names for diagnostics.
func knownInitramfsToolNames() string {
	names := make([]string, 0, len(initramfsGenerators))
	for _, g := range initramfsGenerators {
		names = append(names, g.tool)
	}
	return strings.Join(names, ", ")
}
