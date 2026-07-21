package overlay

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/file"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

// grubRegenExec, detectKernelsFn, and the grubDefault resolve/read/write trio are
// the impure seams over GRUB regeneration's dependencies (in-chroot generator
// execution, the kernel re-scan, and the confined host-side edit of the baseline's
// /etc/default/grub) so the orchestration is unit-testable without a real chroot
// or root. Tests override them; production resolves the defaults file through the
// baseline-confined symlink walk (resolveInRoot), runs the generator through the
// shell allowlist, and edits the defaults file via the temp-stage-then-sudo-copy
// path (file.Write), the same mechanism seedChrootDNS uses for the root-owned tree.
//
// grubRegenExec is deliberately distinct from bootupdate.go's bootRegenExec so
// the two boot stages' tests stay decoupled; commandExistsFn (a stateless probe)
// is shared with the initramfs stage.
var (
	grubRegenExec = func(cmd, chrootPath string) (string, error) {
		return shell.ExecCmdWithStream(cmd, true, chrootPath, nil)
	}
	detectKernelsFn       = detectKernels
	grubDefaultsResolveFn = resolveInRoot
	grubDefaultReadFn     = os.ReadFile
	grubDefaultWriteFn    = func(hostPath string, content []byte) error {
		return file.Write(string(content), hostPath)
	}
)

// grubDefaultsRelPath is the baseline-relative path of the GRUB defaults file the
// kernel command line is applied to (sourced by grub-mkconfig/update-grub).
const grubDefaultsRelPath = "etc/default/grub"

// grubCmdlineKey and grubDefaultKey are the GRUB defaults variables this stage
// rewrites. grubCmdlineKey carries overlayPolicy.kernelCmdline; only this exact key
// is touched — never GRUB_CMDLINE_LINUX_DEFAULT (the boot menu's non-recovery
// default args), which replaceGrubAssignment excludes because the '=' must fall
// immediately after the key. grubDefaultKey carries overlayPolicy.grubDefault, which
// selects the boot menu entry that becomes the default (used to pin an added kernel
// so it, not the baseline's stock kernel, boots).
const (
	grubCmdlineKey = "GRUB_CMDLINE_LINUX"
	grubDefaultKey = "GRUB_DEFAULT"
)

// shimGlob matches a shim binary on the mounted ESP, used as the Secure Boot
// baseline heuristic. The ESP is a FAT filesystem, so there is no symlink-escape
// concern with a plain glob under it.
const shimGlob = "boot/efi/EFI/*/shim*.efi"

// RegenerateGrub applies an optional kernel command line override and regenerates
// the baseline's GRUB2 configuration so a kernel added by the overlay (or an
// operator-supplied cmdline) takes effect at boot.
//
// It reproduces, natively, the conventional GRUB kernel-update sequence: full-line
// replace GRUB_CMDLINE_LINUX (and GRUB_DEFAULT) in /etc/default/grub, then run
// update-grub / grub-mkconfig. It rewrites GRUB_CMDLINE_LINUX from
// overlayPolicy.kernelCmdline (never GRUB_CMDLINE_LINUX_DEFAULT — the '=' must fall
// exactly after the key) and, when set, GRUB_DEFAULT from overlayPolicy.grubDefault.
// Pinning GRUB_DEFAULT matters when the added kernel is a flavored/custom build
// that is not the highest-versioned entry GRUB auto-selects: without it the machine
// could still boot the baseline's stock kernel. The exact GRUB_DEFAULT string (e.g.
// the Ubuntu submenu path "Advanced options for Ubuntu>Ubuntu, with Linux <ver>") is
// supplied by the template rather than inferred, since its shape depends on
// GRUB_DISTRIBUTOR and GRUB_DISABLE_SUBMENU.
//
// It is conservative and never touches the bootloader binary or the ESP (mounted
// read-only upstream): the regenerated grub.cfg lives on the writable root
// (/boot/grub/grub.cfg or /boot/grub2/grub.cfg), and grub-install is never run.
//
// Gating: the stage runs only for a GRUB2 baseline (info.Bootloader == "grub2",
// which covers both the apt boot/grub and rpm boot/grub2 layouts) AND only when
// there is something to do — a non-empty kernelCmdline OR a kernel version that
// appeared since the pre-install baseline scan. A grub.cfg is NOT regenerated
// merely because an existing kernel's initramfs was rebuilt (added firmware or
// modules): the menu entries key on kernel version/path, so only a NEW kernel
// warrants a new entry, and regenerating otherwise would be needless churn.
//
// On a non-GRUB baseline (systemd-boot/uki/unknown) with no cmdline the stage is
// a clean no-op (those bootloaders manage entries via kernel-install hooks). A
// kernelCmdline set on a non-GRUB baseline is a hard error rather than a silent
// drop: honoring it would require rewriting loader entries this stage does not
// own, and shipping an image that ignores an explicit request is worse than
// failing the build.
//
// The generator runs in the chroot with the kernel pseudo-filesystems mounted
// (grub-mkconfig probes the running system), established here for its own
// duration and torn down afterward, mirroring the initramfs stage. When the
// stage has determined there IS work to do (an override or a new kernel) but the
// baseline ships no GRUB generator on PATH, that is a hard error rather than a
// silent skip — emitting a stale grub.cfg would drop the requested boot change.
// A present-but-failing generator is likewise an error, so a failed regeneration
// prevents image emission.
func RegenerateGrub(template *config.ImageTemplate, info *BaselineInfo, rootMount string) (err error) {
	if template == nil {
		return fmt.Errorf("overlay grub regen: template cannot be nil")
	}
	if info == nil {
		return fmt.Errorf("overlay grub regen: baseline info cannot be nil")
	}
	if strings.TrimSpace(rootMount) == "" {
		return fmt.Errorf("overlay grub regen: baseline root mount path cannot be empty")
	}

	cmdline, grubDefault := "", ""
	if template.OverlayPolicy != nil {
		cmdline = strings.TrimSpace(template.OverlayPolicy.KernelCmdline)
		grubDefault = strings.TrimSpace(template.OverlayPolicy.GrubDefault)
	}

	// Only a GRUB2 baseline is in scope. A requested cmdline/grubDefault on a
	// non-GRUB baseline fails loudly; with neither set it is a no-op.
	if info.Bootloader != "grub2" {
		if cmdline != "" || grubDefault != "" {
			return fmt.Errorf("overlay grub regen: overlayPolicy.kernelCmdline/grubDefault is set but the baseline "+
				"bootloader is %q, not grub2; these overrides are only supported on GRUB2 baselines", info.Bootloader)
		}
		log.Infof("Overlay grub regen: baseline bootloader is %q (not grub2); skipping GRUB regeneration", info.Bootloader)
		return nil
	}

	newKernels := addedKernels(info.Kernels, detectKernelsFn(rootMount))
	if len(newKernels) > 0 {
		log.Infof("Overlay grub regen: new kernel(s) detected since baseline: %v", newKernels)
	}

	// Nothing to do: no overrides and no newly-added kernel.
	if cmdline == "" && grubDefault == "" && len(newKernels) == 0 {
		log.Infof("Overlay grub regen: no kernel command line/default override and no new kernel; skipping GRUB regeneration")
		return nil
	}

	// Apply the defaults-file overrides BEFORE regenerating, since grub-mkconfig
	// sources /etc/default/grub. Both edits share one read/write pass.
	if aerr := applyGrubDefaults(rootMount, cmdline, grubDefault); aerr != nil {
		return aerr
	}

	// Best-effort Secure Boot advisory before regenerating: a locally-added kernel
	// or regenerated boot artifact may be unsigned on an SB baseline.
	warnIfSecureBootUnsigned(template, rootMount, len(newKernels) > 0)

	cmd, tool, present, cerr := grubRegenCommand(rootMount)
	if cerr != nil {
		return cerr
	}
	if !present {
		// Reaching here means there IS work to do (a cmdline/grubDefault override or
		// a newly added kernel — the no-op gate above already returned otherwise).
		// A missing generator therefore cannot be a clean skip: it would emit a
		// successful build whose grub.cfg is stale, silently dropping the requested
		// boot change / new kernel entry. Fail the build instead.
		return fmt.Errorf("overlay grub regen: no GRUB config generator (update-grub/grub-mkconfig) present in baseline, " +
			"but a kernel command line/default override or a newly added kernel requires regenerating grub.cfg")
	}

	// Mount the pseudo-filesystems for the generator and tear them down after; the
	// teardown error is surfaced only when the generator itself succeeded.
	if merr := mountSysfs(rootMount); merr != nil {
		return fmt.Errorf("overlay grub regen: failed to mount pseudo-filesystems into %s: %w", rootMount, merr)
	}
	defer func() {
		if uerr := umountSysfs(rootMount); uerr != nil {
			log.Errorf("Overlay grub regen: failed to unmount pseudo-filesystems from %s: %v", rootMount, uerr)
			recordCleanupError(&err, fmt.Errorf("overlay grub regen: failed to unmount pseudo-filesystems from %s: %w", rootMount, uerr))
		}
	}()

	log.Infof("Overlay grub regen: regenerating GRUB configuration in %s with %s", rootMount, tool)
	if out, rerr := grubRegenExec(cmd, rootMount); rerr != nil {
		return fmt.Errorf("overlay grub regen: %s failed: %w%s", tool, rerr, formatCommandOutput(out))
	}
	return nil
}

// addedKernels returns the kernel versions present after install that were not in
// the pre-install baseline set, in their order of appearance in after. It is the
// ground-truth "was a kernel added" signal, independent of package-naming quirks.
// The sole production caller passes detectKernels output (already sorted) as after,
// so the result is sorted in practice; addedKernels itself does not reorder.
func addedKernels(before, after []string) []string {
	prior := make(map[string]bool, len(before))
	for _, k := range before {
		prior[k] = true
	}
	var added []string
	for _, k := range after {
		if !prior[k] {
			added = append(added, k)
		}
	}
	return added
}

// applyGrubDefaults full-line-replaces the requested GRUB defaults keys in the
// baseline's /etc/default/grub in a single read/write pass, then writes the file
// back. A non-empty cmdline rewrites GRUB_CMDLINE_LINUX; a non-empty grubDefault
// rewrites GRUB_DEFAULT (the pinned boot entry). An empty value leaves that key
// untouched. It is a no-op (no read/write) when both are empty.
//
// The edit is done host-side in Go rather than via `sed` so an arbitrary value
// (containing '*', spaces, '/', '=', '&', or GRUB_DEFAULT's ">"-delimited submenu
// path) cannot be mangled by sed replacement syntax or a shell. The defaults file
// lives on the writable root; the write goes through file.Write (temp-stage + sudo
// copy) since the mounted tree is root-owned.
//
// The destination is resolved through the baseline-confined symlink walk
// (resolveInRoot) rather than a bare filepath.Join, so a symlink at
// etc/default/grub (or along the path) that points outside rootMount cannot
// redirect the sudo-backed copy onto an arbitrary host file. resolveInRoot keeps
// every hop under rootMount and hands back the real (non-symlink) regular-file
// target, which is what both the read and the write then act on.
func applyGrubDefaults(rootMount, cmdline, grubDefault string) error {
	if cmdline == "" && grubDefault == "" {
		return nil
	}
	grubDefaults, err := grubDefaultsResolveFn(rootMount, string(filepath.Separator)+grubDefaultsRelPath)
	if err != nil {
		return fmt.Errorf("overlay grub regen: cannot resolve %s within the baseline to apply GRUB defaults: %w", grubDefaultsRelPath, err)
	}
	content, err := grubDefaultReadFn(grubDefaults)
	if err != nil {
		return fmt.Errorf("overlay grub regen: cannot read %s to apply GRUB defaults: %w", grubDefaultsRelPath, err)
	}
	updated := string(content)
	if cmdline != "" {
		updated = replaceGrubAssignment(updated, grubCmdlineKey, cmdline)
		log.Infof("Overlay grub regen: applied kernel command line override to %s", grubDefaultsRelPath)
	}
	if grubDefault != "" {
		updated = replaceGrubAssignment(updated, grubDefaultKey, grubDefault)
		log.Infof("Overlay grub regen: pinned %s in %s", grubDefaultKey, grubDefaultsRelPath)
	}
	if werr := grubDefaultWriteFn(grubDefaults, []byte(updated)); werr != nil {
		return fmt.Errorf("overlay grub regen: failed to write %s: %w", grubDefaultsRelPath, werr)
	}
	return nil
}

// replaceGrubAssignment returns content with every `key=...` assignment replaced
// by key="value", appending the assignment when none was present. It is pure and
// deterministic.
//
// The value is placed verbatim inside the double quotes rather than Go-quoted
// (%q), which would escape tabs, backslashes, and other bytes and so write a
// transformed value into the shell-sourced defaults file. Verbatim writing is
// safe because OverlayPolicy.validate rejects the characters that would break or
// escape a double-quoted, shell-sourced assignment (double quote, dollar sign,
// backtick, backslash, and newline) before this is ever reached.
//
// The match requires the '=' to fall exactly after key, so GRUB_CMDLINE_LINUX does
// not match GRUB_CMDLINE_LINUX_DEFAULT (and vice versa). Every matching line is
// replaced (not just the first) so a file that already carries a duplicate
// definition does not end up with a stale one shadowing the new value. Other
// lines, comments, and the trailing newline are preserved.
func replaceGrubAssignment(content, key, value string) string {
	newLine := fmt.Sprintf(`%s="%s"`, key, value)
	lines := strings.Split(content, "\n")
	replaced := false
	for i, line := range lines {
		if isGrubAssignment(line, key) {
			lines[i] = newLine
			replaced = true
		}
	}
	if replaced {
		return strings.Join(lines, "\n")
	}

	// No existing assignment: append one. Guard against duplicating a blank line
	// when the file already ends in a newline (trailing empty split element).
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines[len(lines)-1] = newLine
		return strings.Join(lines, "\n") + "\n"
	}
	return strings.Join(append(lines, newLine), "\n")
}

// isGrubAssignment reports whether a line assigns exactly key (not a longer key
// sharing the same prefix, e.g. GRUB_CMDLINE_LINUX vs GRUB_CMDLINE_LINUX_DEFAULT).
// Leading whitespace is tolerated; the character immediately after key must be '='.
func isGrubAssignment(line, key string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	rest, ok := strings.CutPrefix(trimmed, key)
	if !ok {
		return false
	}
	return strings.HasPrefix(rest, "=")
}

// grubRegenCommand selects the GRUB config-regeneration command for the baseline,
// probing PATH inside the chroot for each generator in turn. It mirrors create
// mode's updateGrubConfig ordering and outputs to the writable root's /boot, never
// the ESP:
//
//   - update-grub: Debian/Ubuntu wrapper (fixed output path)
//   - grub2-mkconfig -o /boot/grub2/grub.cfg: rpm/dnf families
//   - grub-mkconfig  -o /boot/grub/grub.cfg:  generic GRUB2
//
// present is false when the baseline ships none of them; because grubRegenCommand
// is only consulted once the stage has decided there is work to do, the caller
// treats that as a hard error rather than a skip.
func grubRegenCommand(rootMount string) (cmd, tool string, present bool, err error) {
	candidates := []struct{ tool, cmd string }{
		{"update-grub", "update-grub"},
		{"grub2-mkconfig", "grub2-mkconfig -o /boot/grub2/grub.cfg"},
		{"grub-mkconfig", "grub-mkconfig -o /boot/grub/grub.cfg"},
	}
	for _, c := range candidates {
		exists, cerr := commandExistsFn(c.tool, rootMount)
		if cerr != nil {
			return "", "", false, fmt.Errorf("overlay grub regen: failed to probe for %s in baseline: %w", c.tool, cerr)
		}
		if exists {
			return c.cmd, c.tool, true, nil
		}
	}
	return "", "", false, nil
}

// secureBootBaseline reports whether the baseline looks like a Secure Boot setup,
// heuristically, by the presence of a shim binary on the mounted ESP. It is a
// best-effort probe for the advisory warning; a false negative only suppresses a
// warning, never affects correctness.
func secureBootBaseline(rootMount string) bool {
	matches, err := filepath.Glob(filepath.Join(rootMount, shimGlob))
	if err != nil {
		return false
	}
	return len(matches) > 0
}

// needsSecureBootWarning reports whether an advisory Secure Boot warning is
// warranted: a kernel was added by the overlay, the baseline is a Secure Boot
// setup, and the template carries no signing material to re-sign it. It is pure
// over the (immutability/keys) template state and kernelAdded, so the decision is
// unit-testable.
//
// The signal is kernelAdded specifically (not "a boot artifact was regenerated"):
// a regenerated grub.cfg is not part of the shim signature chain, so it raises no
// Secure Boot concern on its own — only a locally-added, unsigned kernel image
// does. See warnIfSecureBootUnsigned.
func needsSecureBootWarning(template *config.ImageTemplate, rootMount string, kernelAdded bool) bool {
	if !kernelAdded || !secureBootBaseline(rootMount) {
		return false
	}
	return !hasSigningMaterial(template)
}

// hasSigningMaterial reports whether the template supplies the Secure Boot signing
// material overlay would need to re-sign boot artifacts (it currently never does;
// see warnIfSecureBootUnsigned).
func hasSigningMaterial(template *config.ImageTemplate) bool {
	return template.IsImmutabilityEnabled() &&
		template.GetSecureBootDBKeyPath() != "" &&
		template.GetSecureBootDBCrtPath() != "" &&
		template.GetSecureBootDBCerPath() != ""
}

// warnIfSecureBootUnsigned emits a best-effort advisory (never an error) when the
// baseline is a Secure Boot setup, the overlay added a kernel, and no signing
// material is available.
//
// Overlay mode never re-signs boot artifacts even when material IS present: the
// ESP is mounted read-only, so shim/grub are immutable, and the regenerated
// grub.cfg is not part of the shim signature chain. The real Secure Boot risk is
// a locally-added, unsigned kernel image, which will be rejected by firmware — the
// warning surfaces that so the operator can sign it out of band.
func warnIfSecureBootUnsigned(template *config.ImageTemplate, rootMount string, kernelAdded bool) {
	if !needsSecureBootWarning(template, rootMount, kernelAdded) {
		return
	}
	log.Warnf("Overlay grub regen: baseline appears to use Secure Boot but no signing material is configured; " +
		"the newly added kernel may be unsigned and may fail to boot under Secure Boot (the regenerated grub.cfg is " +
		"not part of the shim signature chain and is not itself the concern). " +
		"Sign the kernel out of band or provide signing material.")
}
