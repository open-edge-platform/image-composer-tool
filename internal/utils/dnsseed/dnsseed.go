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
// nameserver" case, and hands back a restore function that takes the seed out
// again — unless the configuration commands replaced or edited it, in which case
// their result is the image's resolver and is left alone.
//
// Every path is resolved inside the rootfs without following symlinks, and every
// write replaces its destination by rename, so nothing is ever written through a
// symlink onto the build host.
package dnsseed

import (
	"bytes"
	"fmt"
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

// Restore takes a seed back out of the rootfs. It returns an error only when the
// seeded resolver could not be removed or the original put back, so the seed
// would otherwise ship in the image; the caller must fail the build then.
type Restore func() error

// noRestore is the no-op returned whenever nothing was changed.
func noRestore() error { return nil }

// SeedEphemeral makes the build host's resolver reachable inside the chroot rooted
// at rootMount, and returns a Restore that reverts the change.
//
// Restore MUST be called once the network-using step is done and BEFORE anything
// that inspects /etc/resolv.conf to decide the image's final resolver
// configuration. It is safe to call exactly once.
//
// Behaviour by the shape of <rootMount>/etc/resolv.conf:
//
//   - absent (create mode, post-mmdebstrap): write the host's resolver there;
//     restore deletes it, leaving the rootfs exactly as found.
//   - symlink into the ephemeral /run tmpfs (overlay mode), target absent: write
//     the host's resolver at the target; restore deletes it.
//   - regular file listing no nameserver (degenerate): overwrite it; restore puts
//     the original bytes back.
//   - regular file with at least one nameserver: left alone — it already works.
//   - symlink pointing outside /run: left alone — writing through it would modify
//     the delivered image.
//
// Restore acts only on a seed the configuration commands left as it was: a file
// they replaced, edited or swapped for a symlink is theirs, and stays.
//
// Seeding is best-effort: a failure is logged and yields a no-op Restore.
func SeedEphemeral(rootMount string) Restore {
	content, err := os.ReadFile(hostResolvConf) // follows a host symlink to the real file
	if err != nil {
		log.Debugf("DNS seed: skipping (cannot read host %s: %v)", hostResolvConf, err)
		return noRestore
	}

	etcDir, err := dirInRoot(rootMount, "etc")
	if err != nil {
		log.Debugf("DNS seed: skipping (%v)", err)
		return noRestore
	}
	etcResolv := filepath.Join(etcDir, "resolv.conf")

	// Lstat, not Stat, so a dangling symlink is reported as a symlink rather than as
	// "not found" — the two need different handling.
	fi, err := os.Lstat(etcResolv)
	switch {
	case os.IsNotExist(err):
		// Create mode: mmdebstrap left no resolver at all.
		return seedNewFile(etcResolv, content, "removed after configuration commands")
	case err != nil:
		log.Debugf("DNS seed: skipping (cannot stat %s: %v)", etcResolv, err)
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

// seedNewFile writes content to path, which does not exist yet, and returns a
// Restore that deletes it again.
func seedNewFile(path string, content []byte, note string) Restore {
	if err := writeInto(path, content); err != nil {
		log.Debugf("DNS seed: skipping (%v)", err)
		return noRestore
	}
	log.Infof("DNS seed: seeded ephemeral resolver at %s (%s)", path, note)
	return func() error { return removeIfUnchanged(path, content) }
}

// seedSymlinkTarget writes content where the etcResolv symlink points, but only
// when that lands on the ephemeral /run tmpfs and nothing is there yet; anywhere
// else is part of the delivered image and must not be touched.
func seedSymlinkTarget(rootMount, etcResolv string, content []byte) Restore {
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
	if !strings.HasPrefix(guestTarget, "/run/") {
		log.Debugf("DNS seed: skipping (%s resolves to %s, not under /run)", etcResolv, guestTarget)
		return noRestore
	}
	targetDir, err := dirInRoot(rootMount, filepath.Dir(guestTarget))
	if err != nil {
		log.Debugf("DNS seed: skipping (%v)", err)
		return noRestore
	}
	hostTarget := filepath.Join(targetDir, filepath.Base(guestTarget))
	if _, err := os.Lstat(hostTarget); !os.IsNotExist(err) {
		log.Debugf("DNS seed: skipping (%s already exists)", hostTarget)
		return noRestore
	}
	return seedNewFile(hostTarget, content, "under /run, discarded with the tmpfs")
}

// seedResolverlessFile replaces a regular etcResolv that lists no nameserver
// (commonly an empty file) with content, and returns a Restore that puts the
// original bytes back. A file that already lists a nameserver works and is left
// alone.
func seedResolverlessFile(etcResolv string, content []byte) Restore {
	original, err := os.ReadFile(etcResolv)
	if err != nil {
		log.Debugf("DNS seed: skipping (cannot read %s: %v)", etcResolv, err)
		return noRestore
	}
	if hasNameserver(original) {
		log.Debugf("DNS seed: skipping (%s already lists a nameserver)", etcResolv)
		return noRestore
	}
	if err := writeInto(etcResolv, content); err != nil {
		log.Debugf("DNS seed: skipping (%v)", err)
		return noRestore
	}
	log.Infof("DNS seed: seeded ephemeral resolver at %s (original had no nameserver; restored afterwards)", etcResolv)
	return func() error {
		if !unchanged(etcResolv, content) {
			return nil
		}
		if err := writeInto(etcResolv, original); err != nil {
			return fmt.Errorf("DNS seed: restoring the original %s: %w", etcResolv, err)
		}
		return nil
	}
}

// dirInRoot returns the host path of the guest directory guestDir inside
// rootMount, refusing it when any part of it that exists is a symlink or not a
// directory: following one could lead out of the rootfs and onto the build host.
// Parts that do not exist yet are fine; writeInto creates them.
func dirInRoot(rootMount, guestDir string) (string, error) {
	dir := filepath.Clean(rootMount)
	for _, part := range strings.Split(strings.Trim(filepath.Clean(guestDir), "/"), "/") {
		if part == "" {
			continue
		}
		dir = filepath.Join(dir, part)
		fi, err := os.Lstat(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("cannot stat %s: %w", dir, err)
		}
		if !fi.IsDir() {
			return "", fmt.Errorf("%s is a symlink or not a directory", dir)
		}
	}
	return dir, nil
}

// unchanged reports whether path is still the regular file the seed wrote. A
// configuration command that replaced, edited or symlinked it has set the
// image's resolver itself, which must then be kept.
func unchanged(path string, seeded []byte) bool {
	fi, err := os.Lstat(path)
	if err == nil && fi.Mode().IsRegular() {
		if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, seeded) {
			return true
		}
	}
	log.Infof("DNS seed: %s was changed by the configuration commands; keeping it as they left it", path)
	return false
}

// removeIfUnchanged deletes the seeded file at path unless the configuration
// commands changed it.
func removeIfUnchanged(path string, seeded []byte) error {
	if !unchanged(path, seeded) {
		return nil
	}
	if _, err := execFn("rm -f "+shell.QuoteArg(path), true, shell.HostPath, nil); err != nil {
		return fmt.Errorf("DNS seed: removing the seeded resolver %s: %w", path, err)
	}
	log.Debugf("DNS seed: removed seeded resolver %s", path)
	return nil
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

// writeInto replaces dst with content, under sudo, so a non-root build can write
// into a root-owned tree. The content is copied next to dst and renamed over it:
// rename replaces a symlink at dst instead of writing through it, and dst ends up
// either fully replaced or exactly as it was.
func writeInto(dst string, content []byte) error {
	staged, err := stage(content)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(staged) }()

	dir := filepath.Dir(dst)
	if _, err := execFn("mkdir -p "+shell.QuoteArg(dir), true, shell.HostPath, nil); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	tmp := filepath.Join(dir, "."+filepath.Base(staged))
	if _, err := execFn("cp "+shell.QuoteArg(staged)+" "+shell.QuoteArg(tmp), true, shell.HostPath, nil); err != nil {
		discard(tmp)
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if _, err := execFn("mv -f "+shell.QuoteArg(tmp)+" "+shell.QuoteArg(dst), true, shell.HostPath, nil); err != nil {
		discard(tmp)
		return fmt.Errorf("replacing %s: %w", dst, err)
	}
	return nil
}

// stage writes content to a world-readable file in the build's temp dir and
// returns its path.
func stage(content []byte) (string, error) {
	stagedFile, err := os.CreateTemp(config.TempDir(), "ict-resolv-*.conf")
	if err != nil {
		return "", fmt.Errorf("creating staging file: %w", err)
	}
	staged := stagedFile.Name()
	// os.CreateTemp yields 0o600; resolv.conf is conventionally world-readable.
	err = stagedFile.Chmod(0o644)
	if err == nil {
		_, err = stagedFile.Write(content)
	}
	if cerr := stagedFile.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(staged)
		return "", fmt.Errorf("staging resolv.conf: %w", err)
	}
	return staged, nil
}

// discard removes a half-written temp file left by a failed writeInto.
func discard(tmp string) {
	if _, err := execFn("rm -f "+shell.QuoteArg(tmp), true, shell.HostPath, nil); err != nil {
		log.Warnf("DNS seed: failed to remove %s: %v", tmp, err)
	}
}
