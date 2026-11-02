package overlay

import (
	"fmt"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/dnsseed"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

// configExecFn is the seam over the streamed chroot command executor, so the
// configuration orchestration is unit-testable without root or a real chroot.
// Production uses shell.ExecCmdWithStream; tests override it.
var configExecFn = shell.ExecCmdWithStream

// RunOverlayConfigurations executes the template's systemConfig.configurations
// commands inside the mounted baseline chroot, mirroring create mode's
// addImageConfigs so an overlay build honors the same arbitrary-command escape
// hatch (e.g. downloading and dpkg-installing an out-of-repo driver).
//
// Each command runs as `chroot <rootMount> /bin/bash -c "<cmd>"`, exactly as create
// mode does: the whole user command is passed opaquely to bash, so shell features
// the resolver-driven package install path never needs — pipes, `||`, redirections,
// `xargs` — work here without being rejected by the command allowlist (only the
// outer `chroot` is validated). Commands run with sudo on the host path, which lets
// the shell layer inject the build's proxy environment; those variables are
// inherited across the chroot boundary so proxied networks work too.
//
// The kernel pseudo-filesystems are mounted for the duration (so maintainer scripts
// and tools like dpkg behave as they would on a running system) and torn down on
// every return path, including a panic. DNS is seeded best-effort into the chroot's
// ephemeral /run tmpfs so a direct-internet `wget`/`curl` can resolve names; a
// failure there is logged and does not fail the build (proxied builds do not rely
// on it, and a command that genuinely needs the network fails loudly on its own).
//
// It is a no-op (no mounts, no chroot entry) when the template declares no
// configurations, so builds that do not use the feature pay nothing for it.
func RunOverlayConfigurations(template *config.ImageTemplate, rootMount string) (err error) {
	if template == nil {
		return fmt.Errorf("overlay configure: template cannot be nil")
	}
	if strings.TrimSpace(rootMount) == "" {
		return fmt.Errorf("overlay configure: baseline root mount path cannot be empty")
	}

	configs := template.GetConfigurationInfo()
	if len(configs) == 0 {
		log.Debug("Overlay configure: no configurations to run")
		return nil
	}

	log.Infof("Overlay configure: running %d configuration command(s) in %s", len(configs), rootMount)

	// Mount the pseudo-filesystems so chrooted commands (dpkg maintainer scripts,
	// tools reading /proc or /sys) behave as on a live system, and tear them down on
	// every return path.
	if err = mountSysfs(rootMount); err != nil {
		// Best-effort rollback of any partial sysfs mounts before failing.
		if cerr := umountSysfs(rootMount); cerr != nil {
			log.Warnf("Overlay configure: rollback after failed sysfs mount also failed: %v", cerr)
		}
		return fmt.Errorf("overlay configure: failed to mount pseudo-filesystems into %s: %w", rootMount, err)
	}
	defer func() {
		if uerr := umountSysfs(rootMount); uerr != nil {
			log.Errorf("Overlay configure: failed to unmount pseudo-filesystems from %s: %v", rootMount, uerr)
			recordCleanupError(&err, fmt.Errorf("failed to unmount pseudo-filesystems from %s: %w", rootMount, uerr))
		}
	}()

	// Seed DNS into the (now-mounted, ephemeral) /run tmpfs so a direct-internet
	// command can resolve names. Best-effort: proxied builds inherit the proxy env
	// instead, and a command that truly needs DNS surfaces its own error.
	defer dnsseed.SeedEphemeral(rootMount)()

	for _, configInfo := range configs {
		cmdStr := strings.TrimSpace(configInfo.Cmd)
		if cmdStr == "" {
			continue
		}
		// Wrap the user command in a chroot + bash -c, both rootMount and cmdStr
		// single-quoted (shell.QuoteArg) so each reaches bash as one opaque argument —
		// the command allowlist only validates the outer `chroot`, matching create
		// mode. Single quotes, not strconv.Quote/double quotes: double quotes still
		// let bash expand $(...), backticks and $var inside cmdStr, and don't survive
		// a multi-line script's real newlines being embedded that way either.
		chrootCmd := fmt.Sprintf("chroot %s /bin/bash -c %s", shell.QuoteArg(rootMount), shell.QuoteArg(cmdStr))
		out, cerr := configExecFn(chrootCmd, true, shell.HostPath, nil)
		if cerr != nil {
			return fmt.Errorf("overlay configure: configuration command failed: %q: %w%s", cmdStr, cerr, formatCommandOutput(out))
		}
		log.Debugf("Overlay configure: command succeeded: %s", cmdStr)
	}

	log.Infof("Overlay configure: %d configuration command(s) completed", len(configs))
	return nil
}
