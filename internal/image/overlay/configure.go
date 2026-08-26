package overlay

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

// hostResolvConf is the build host's resolver configuration. It is read (following
// any symlink) to seed DNS inside the baseline chroot so a configuration command
// that reaches the network directly — e.g. `wget https://...` — can resolve names.
const hostResolvConf = "/etc/resolv.conf"

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
	seedChrootDNS(rootMount)

	for _, configInfo := range configs {
		cmdStr := strings.TrimSpace(configInfo.Cmd)
		if cmdStr == "" {
			continue
		}
		// Wrap the user command in a chroot + bash -c, quoting the whole command as a
		// single argument (strconv.Quote) so it is handed to bash opaquely — the
		// command allowlist only validates the outer `chroot`, matching create mode.
		// rootMount is single-quoted (shell.QuoteArg) so any shell metacharacter in the
		// workspace path is neutralized at the final `bash -c`. Note this does NOT make
		// the path whitespace-safe: the allowlist verifier re-tokenizes the command with
		// strings.Fields (not quote-aware) and collapses internal whitespace runs, so a
		// path with tabs or repeated spaces would be corrupted. The overlay workspace
		// path is tool-derived and carries no such whitespace, so this is not a concern
		// in practice; the quoting here is defense-in-depth against metacharacters, not a
		// promise of arbitrary-whitespace support.
		chrootCmd := fmt.Sprintf("chroot %s /bin/bash -c %s", shell.QuoteArg(rootMount), strconv.Quote(cmdStr))
		out, cerr := configExecFn(chrootCmd, true, shell.HostPath, nil)
		if cerr != nil {
			return fmt.Errorf("overlay configure: configuration command failed: %q: %w%s", cmdStr, cerr, formatCommandOutput(out))
		}
		log.Debugf("Overlay configure: command succeeded: %s", cmdStr)
	}

	log.Infof("Overlay configure: %d configuration command(s) completed", len(configs))
	return nil
}

// seedChrootDNS makes the build host's resolver reachable inside the chroot so a
// configuration command that hits the network directly can resolve hostnames.
//
// It writes the host's resolv.conf content to wherever the chroot's
// /etc/resolv.conf ultimately points, but ONLY when that destination lands on the
// ephemeral /run tmpfs (the common case: cloud images symlink
// /etc/resolv.conf -> ../run/systemd/resolve/stub-resolv.conf). Restricting the
// write to /run guarantees the seeded DNS is discarded when the tmpfs is unmounted
// and never persists into the shipped image. Any other shape (a real /etc file, a
// symlink pointing outside /run, no resolv.conf at all) is left untouched, since
// writing there would modify the delivered image. The whole helper is best-effort:
// every failure is logged and swallowed.
func seedChrootDNS(rootMount string) {
	content, err := os.ReadFile(hostResolvConf) // follows a host symlink to the real file
	if err != nil {
		log.Debugf("Overlay configure: skipping DNS seed (cannot read host %s: %v)", hostResolvConf, err)
		return
	}

	// Resolve where the chroot's /etc/resolv.conf points. os.Readlink returns the
	// link's raw target; a non-symlink (regular file or absent) yields an error, in
	// which case we do not touch it.
	etcResolv := filepath.Join(rootMount, "etc", "resolv.conf")
	target, err := os.Readlink(etcResolv)
	if err != nil {
		log.Debugf("Overlay configure: skipping DNS seed (%s is not a symlink into /run: %v)", etcResolv, err)
		return
	}

	// Compute the symlink target as an absolute path inside the guest, whether it is
	// written relative (../run/...) or absolute (/run/...).
	var guestTarget string
	if filepath.IsAbs(target) {
		guestTarget = filepath.Clean(target)
	} else {
		guestTarget = filepath.Clean(filepath.Join("/etc", target))
	}
	// Only seed when the destination is on the ephemeral /run tmpfs, so nothing is
	// written into the persistent image.
	if guestTarget != "/run" && !strings.HasPrefix(guestTarget, "/run/") {
		log.Debugf("Overlay configure: skipping DNS seed (%s resolves to %s, not under /run)", etcResolv, guestTarget)
		return
	}

	hostTargetPath := filepath.Join(rootMount, strings.TrimPrefix(guestTarget, "/"))
	// Stage the host resolver content in our own temp dir, then copy it into the
	// chroot's /run under sudo (the mounted tmpfs is root-owned). A unique temp file
	// (os.CreateTemp) keeps concurrent overlay builds or retries from racing on a
	// shared staging path.
	stagedFile, werr := os.CreateTemp(config.TempDir(), "overlay-resolv-*.conf")
	if werr != nil {
		log.Debugf("Overlay configure: skipping DNS seed (creating staging file failed: %v)", werr)
		return
	}
	staged := stagedFile.Name()
	defer func() { _ = os.Remove(staged) }()
	// os.CreateTemp yields 0o600; resolv.conf is conventionally world-readable.
	if werr := stagedFile.Chmod(0o644); werr != nil {
		_ = stagedFile.Close()
		log.Debugf("Overlay configure: skipping DNS seed (chmod staged resolv.conf failed: %v)", werr)
		return
	}
	if _, werr := stagedFile.Write(content); werr != nil {
		_ = stagedFile.Close()
		log.Debugf("Overlay configure: skipping DNS seed (staging resolv.conf failed: %v)", werr)
		return
	}
	if werr := stagedFile.Close(); werr != nil {
		log.Debugf("Overlay configure: skipping DNS seed (closing staged resolv.conf failed: %v)", werr)
		return
	}

	if _, merr := shell.ExecCmd("mkdir -p "+shell.QuoteArg(filepath.Dir(hostTargetPath)), true, shell.HostPath, nil); merr != nil {
		log.Debugf("Overlay configure: skipping DNS seed (creating %s failed: %v)", filepath.Dir(hostTargetPath), merr)
		return
	}
	if _, cerr := shell.ExecCmd("cp "+shell.QuoteArg(staged)+" "+shell.QuoteArg(hostTargetPath), true, shell.HostPath, nil); cerr != nil {
		log.Debugf("Overlay configure: skipping DNS seed (writing %s failed: %v)", hostTargetPath, cerr)
		return
	}
	log.Debugf("Overlay configure: seeded chroot DNS at %s (ephemeral)", guestTarget)
}
