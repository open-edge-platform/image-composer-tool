package imageos

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

const (
	// targetUnamePath is the packaged uname inside the chroot. The wrapper has
	// to replace this file, not sit in /usr/local/bin: apt runs maintainer
	// scripts with PATH=/usr/sbin:/usr/bin:/sbin:/bin (DPkg::Path), so a
	// /usr/local/bin/uname is never found. Replacing /usr/bin/uname also covers
	// scripts that call it by absolute path.
	targetUnamePath = "/usr/bin/uname"
	// targetUnameRealPath is where dpkg-divert moves the real uname.
	targetUnameRealPath = "/usr/bin/uname.oic-diverted"
	// unameRollbackTimeout bounds the dpkg-divert call that puts the real uname back.
	unameRollbackTimeout = 2 * time.Minute
)

// targetUnameWrapperScript makes `uname -r` report the newest kernel installed
// in the image instead of the build host's running kernel. A kernel counts when
// it has /boot/vmlinuz-<version> or /lib/modules/<version>/vmlinuz (kernel
// packages may ship only the latter and link it into /boot later; a
// /lib/modules/<version> without a vmlinuz, such as a headers-only directory,
// is ignored). Everything else (other flags, or no kernel installed yet) goes
// to the real uname.
const targetUnameWrapperScript = `#!/bin/sh
# Temporary ICT wrapper, removed once package installation finishes.
# Inside the image-build chroot the real uname reports the build host's kernel,
# so postinst scripts that build for "uname -r" (e.g. DKMS) target the wrong one.
if [ "$#" -eq 1 ] && { [ "$1" = "-r" ] || [ "$1" = "--kernel-release" ]; }; then
	version=$({
		ls -1 /boot 2>/dev/null | sed -n 's/^vmlinuz-//p'
		for image in /lib/modules/*/vmlinuz; do
			[ -e "$image" ] && basename "$(dirname "$image")"
		done
	} | sort -V | tail -n 1)
	if [ -n "$version" ]; then
		echo "$version"
		exit 0
	fi
fi
exec /usr/bin/uname.oic-diverted "$@"
`

// runUnderCleanupContext runs a command that must still happen after the build
// was cancelled: it uses its own bounded context instead of the build's, and
// puts the previous shell-context binding back afterwards.
func runUnderCleanupContext(cmd, installRoot string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), unameRollbackTimeout)
	defer cancel()
	restoreShell := shell.SetContext(ctx)
	defer restoreShell()
	return shell.ExecCmd(cmd, true, installRoot, nil)
}

// installTargetUnameWrapper replaces the chroot's uname with a wrapper so
// packages installed into the image see the target kernel from `uname -r`.
// dpkg-divert moves the real uname aside (and keeps any coreutils upgrade
// during the install from overwriting the wrapper). The returned function
// restores the real uname and must be called after package installation, or
// the wrapper would ship in the image.
//
// Setting it up is best effort: if the diversion or the wrapper cannot be put
// in place, it is rolled back and the build carries on as it did before (the
// returned function is then a no-op). An error is returned only when the
// rollback itself fails or the real uname cannot be confirmed in place, because
// the image is then left without its real uname.
func installTargetUnameWrapper(installRoot string) (restore func() error, err error) {
	noop := func() error { return nil }
	wrapperPath := filepath.Join(installRoot, targetUnamePath)

	divertCmd := fmt.Sprintf("dpkg-divert --local --rename --divert %s --add %s", targetUnameRealPath, targetUnamePath)
	removeDivertCmd := fmt.Sprintf("dpkg-divert --local --rename --remove %s", targetUnamePath)
	if _, divertErr := shell.ExecCmd(divertCmd, true, installRoot, nil); divertErr != nil {
		// A failed (or cancelled) dpkg-divert may already have moved uname
		// aside, so undo anything it did before carrying on without the wrapper.
		if _, rmErr := runUnderCleanupContext(removeDivertCmd, installRoot); rmErr != nil {
			log.Debugf("Removing the uname diversion after a failed add: %v", rmErr)
		}
		if _, statErr := os.Lstat(wrapperPath); statErr != nil {
			return noop, fmt.Errorf("failed to divert %s (%v) and the real uname could not be confirmed back in place: %w",
				targetUnamePath, divertErr, statErr)
		}
		log.Warnf("Failed to divert %s, not installing the target-kernel uname wrapper: %v", targetUnamePath, divertErr)
		return noop, nil
	}

	// Remove the wrapper (a failed write can leave a partial file) before
	// removing the diversion: dpkg-divert --rename refuses to restore the real
	// uname over an existing file.
	//
	// dpkg-divert runs under its own bounded context: after Ctrl+C the build's
	// shell context is already cancelled, and it would never start, leaving the
	// image without its real uname.
	rollback := func() error {
		if err := os.Remove(wrapperPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove uname wrapper %s: %w", wrapperPath, err)
		}
		if _, err := runUnderCleanupContext(removeDivertCmd, installRoot); err != nil {
			return fmt.Errorf("failed to restore the real uname: %w", err)
		}
		return nil
	}

	if err := os.WriteFile(wrapperPath, []byte(targetUnameWrapperScript), 0755); err != nil {
		if rbErr := rollback(); rbErr != nil {
			return noop, fmt.Errorf("failed to write uname wrapper %s: %w; rollback also failed, the image has no usable uname: %v",
				wrapperPath, err, rbErr)
		}
		log.Warnf("Failed to write uname wrapper %s, not installing the target-kernel uname wrapper: %v", wrapperPath, err)
		return noop, nil
	}
	log.Debugf("Installed target-kernel uname wrapper at %s", wrapperPath)

	return func() error {
		if err := rollback(); err != nil {
			return err
		}
		log.Debugf("Restored the real uname at %s", wrapperPath)
		return nil
	}, nil
}
