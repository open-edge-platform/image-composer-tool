// Package dnsseed makes the build host's resolver reachable inside a chroot so a
// build step that reaches the network directly — e.g. a template's
// `systemConfig.configurations` running `wget https://...` — can resolve names.
//
// Both build modes need this, but for different reasons, and the two rootfs shapes
// are not the same:
//
//   - Create mode bootstraps a fresh rootfs with mmdebstrap, which leaves NO
//     /etc/resolv.conf at all. glibc then falls back to 127.0.0.1:53, nothing is
//     listening, and every lookup fails instantly — fast enough that a wget
//     --tries/--waitretry never even gets to retry.
//   - Overlay mode mounts a pre-built baseline image, which normally ships
//     /etc/resolv.conf as a symlink to /run/systemd/resolve/stub-resolv.conf. That
//     target only exists once the image boots under a running systemd-resolved;
//     during the build /run is a fresh empty tmpfs, so the symlink dangles.
//
// SeedEphemeral handles both, plus the degenerate "file exists but lists no
// nameserver" case, and hands back a restore function that undoes exactly what it
// did so nothing leaks into the delivered image.
package dnsseed

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

var log = logger.Logger()

// hostResolvConf is the build host's resolver configuration. It is read (following
// any symlink) to seed DNS inside the chroot. It is a var, not a const, so tests can
// point it at a fixture instead of the real host file.
var hostResolvConf = "/etc/resolv.conf"

// execFn is the seam over the shell executor, so the seeding logic is unit-testable
// without root or a real chroot. Production uses shell.ExecCmd; tests override it.
var execFn = shell.ExecCmd

// noRestore is the no-op returned whenever nothing was changed.
func noRestore() {}

// SeedEphemeral makes the build host's resolver reachable inside the chroot rooted
// at rootMount, and returns a function that reverts the change.
//
// The returned restore function MUST be called once the network-using step is done
// and BEFORE anything that inspects /etc/resolv.conf to decide the image's final
// resolver configuration. It is safe to call exactly once; calling it is what keeps
// the seeded resolver out of the shipped image.
//
// Behaviour by the shape of <rootMount>/etc/resolv.conf:
//
//   - absent (create mode, post-mmdebstrap): write the host's resolver there;
//     restore deletes it, leaving the rootfs exactly as found.
//   - symlink into the ephemeral /run tmpfs (overlay mode): write the host's
//     resolver at the symlink target; restore deletes that file. Nothing under /run
//     survives into the image regardless, so this is belt-and-braces.
//   - regular file listing no nameserver (degenerate): overwrite it; restore puts
//     the original bytes back.
//   - regular file with at least one nameserver: left alone — it already works.
//   - symlink pointing outside /run: left alone — writing through it would modify
//     the delivered image.
//
// The whole helper is best-effort: every failure is logged and swallowed, and a
// failure to seed still yields a usable (no-op) restore function.
func SeedEphemeral(rootMount string) func() {
	content, err := os.ReadFile(hostResolvConf) // follows a host symlink to the real file
	if err != nil {
		log.Debugf("DNS seed: skipping (cannot read host %s: %v)", hostResolvConf, err)
		return noRestore
	}

	etcResolv := filepath.Join(rootMount, "etc", "resolv.conf")

	// Lstat, not Stat, so a dangling symlink is reported as a symlink rather than as
	// "not found" — the two need different handling.
	fi, lerr := os.Lstat(etcResolv)
	switch {
	case os.IsNotExist(lerr):
		// Create mode: mmdebstrap left no resolver at all.
		if !writeInto(etcResolv, content) {
			return noRestore
		}
		log.Infof("DNS seed: seeded ephemeral resolver at %s (removed after configuration commands)", etcResolv)
		return func() { removePath(etcResolv) }
	case lerr != nil:
		log.Debugf("DNS seed: skipping (cannot stat %s: %v)", etcResolv, lerr)
		return noRestore
	case fi.Mode()&os.ModeSymlink != 0:
		return seedSymlinkTarget(rootMount, etcResolv, content)
	case fi.Mode().IsRegular():
		return seedResolverlessFile(etcResolv, content)
	default:
		log.Debugf("DNS seed: skipping (%s is neither a regular file nor a symlink: %v)", etcResolv, fi.Mode())
		return noRestore
	}
}

// seedSymlinkTarget writes content where the etcResolv symlink points, but only
// when that lands on the ephemeral /run tmpfs; anywhere else is part of the
// delivered image and must not be touched.
func seedSymlinkTarget(rootMount, etcResolv string, content []byte) func() {
	target, err := os.Readlink(etcResolv)
	if err != nil {
		log.Debugf("DNS seed: skipping (cannot read symlink %s: %v)", etcResolv, err)
		return noRestore
	}
	// Express the target as an absolute guest path, whether written relative
	// (../run/...) or absolute (/run/...).
	guestTarget := filepath.Clean(target)
	if !filepath.IsAbs(target) {
		guestTarget = filepath.Clean(filepath.Join("/etc", target))
	}
	if guestTarget != "/run" && !strings.HasPrefix(guestTarget, "/run/") {
		log.Debugf("DNS seed: skipping (%s resolves to %s, not under /run)", etcResolv, guestTarget)
		return noRestore
	}
	hostTargetPath := filepath.Join(rootMount, strings.TrimPrefix(guestTarget, "/"))
	if !writeInto(hostTargetPath, content) {
		return noRestore
	}
	log.Infof("DNS seed: seeded ephemeral resolver at %s (under /run, discarded with the tmpfs)", guestTarget)
	return func() { removePath(hostTargetPath) }
}

// seedResolverlessFile replaces a regular etcResolv that lists no nameserver
// (commonly an empty file) with content, and restores the original bytes
// afterwards so the image ships what it shipped before. A file that already
// lists a nameserver works and is left alone.
func seedResolverlessFile(etcResolv string, content []byte) func() {
	existing, err := os.ReadFile(etcResolv)
	if err != nil {
		log.Debugf("DNS seed: skipping (cannot read %s: %v)", etcResolv, err)
		return noRestore
	}
	if hasNameserver(existing) {
		log.Debugf("DNS seed: skipping (%s already lists a nameserver)", etcResolv)
		return noRestore
	}
	if !writeInto(etcResolv, content) {
		return noRestore
	}
	log.Infof("DNS seed: seeded ephemeral resolver at %s (original had no nameserver; restored afterwards)", etcResolv)
	return func() {
		if !writeInto(etcResolv, existing) {
			log.Warnf("DNS seed: could not restore the original %s", etcResolv)
		}
	}
}

// hasNameserver reports whether a resolv.conf body carries at least one
// non-commented nameserver directive, i.e. whether it can actually resolve.
func hasNameserver(body []byte) bool {
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if fields := strings.Fields(line); len(fields) >= 2 && fields[0] == "nameserver" {
			return true
		}
	}
	return false
}

// writeInto stages content in the build's temp dir and copies it to dst under sudo,
// creating dst's parent if needed. The staging hop is what lets a non-root build
// write into a root-owned tree. Reports whether the write landed.
func writeInto(dst string, content []byte) bool {
	stagedFile, err := os.CreateTemp(config.TempDir(), "ict-resolv-*.conf")
	if err != nil {
		log.Debugf("DNS seed: skipping (creating staging file failed: %v)", err)
		return false
	}
	staged := stagedFile.Name()
	defer func() { _ = os.Remove(staged) }()
	// os.CreateTemp yields 0o600; resolv.conf is conventionally world-readable.
	if err := stagedFile.Chmod(0o644); err != nil {
		_ = stagedFile.Close()
		log.Debugf("DNS seed: skipping (chmod staged resolv.conf failed: %v)", err)
		return false
	}
	if _, err := stagedFile.Write(content); err != nil {
		_ = stagedFile.Close()
		log.Debugf("DNS seed: skipping (staging resolv.conf failed: %v)", err)
		return false
	}
	if err := stagedFile.Close(); err != nil {
		log.Debugf("DNS seed: skipping (closing staged resolv.conf failed: %v)", err)
		return false
	}

	if _, err := execFn("mkdir -p "+shell.QuoteArg(filepath.Dir(dst)), true, shell.HostPath, nil); err != nil {
		log.Debugf("DNS seed: skipping (creating %s failed: %v)", filepath.Dir(dst), err)
		return false
	}
	if _, err := execFn("cp "+shell.QuoteArg(staged)+" "+shell.QuoteArg(dst), true, shell.HostPath, nil); err != nil {
		log.Debugf("DNS seed: skipping (writing %s failed: %v)", dst, err)
		return false
	}
	return true
}

// removePath deletes a file the seed created. Best-effort: a leftover under /run is
// discarded with the tmpfs anyway, and one in /etc is reported so it is not silent.
func removePath(path string) {
	if _, err := execFn("rm -f "+shell.QuoteArg(path), true, shell.HostPath, nil); err != nil {
		log.Warnf("DNS seed: failed to remove seeded resolver %s: %v", path, err)
		return
	}
	log.Debugf("DNS seed: removed seeded resolver %s", path)
}
