package imageos

import (
	"fmt"
	"os"
	"path/filepath"

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
)

// targetUnameWrapperScript makes `uname -r` report the newest kernel installed
// under /boot instead of the build host's running kernel. Everything else
// (other flags, or no kernel installed yet) goes to the real uname.
const targetUnameWrapperScript = `#!/bin/sh
# Temporary ICT wrapper, removed once package installation finishes.
# Inside the image-build chroot the real uname reports the build host's kernel,
# so postinst scripts that build for "uname -r" (e.g. DKMS) target the wrong one.
if [ "$#" -eq 1 ] && { [ "$1" = "-r" ] || [ "$1" = "--kernel-release" ]; }; then
	version=$(ls -1 /boot 2>/dev/null | sed -n 's/^vmlinuz-//p' | sort -V | tail -n 1)
	if [ -n "$version" ]; then
		echo "$version"
		exit 0
	fi
fi
exec /usr/bin/uname.oic-diverted "$@"
`

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
// rollback itself fails, because the image is then left without its real uname.
func installTargetUnameWrapper(installRoot string) (restore func() error, err error) {
	noop := func() error { return nil }
	wrapperPath := filepath.Join(installRoot, targetUnamePath)

	divertCmd := fmt.Sprintf("dpkg-divert --local --rename --divert %s --add %s", targetUnameRealPath, targetUnamePath)
	if _, divertErr := shell.ExecCmd(divertCmd, true, installRoot, nil); divertErr != nil {
		log.Warnf("Failed to divert %s, not installing the target-kernel uname wrapper: %v", targetUnamePath, divertErr)
		return noop, nil
	}

	// Remove the wrapper (a failed write can leave a partial file) before
	// removing the diversion: dpkg-divert --rename refuses to restore the real
	// uname over an existing file.
	rollback := func() error {
		if err := os.Remove(wrapperPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove uname wrapper %s: %w", wrapperPath, err)
		}
		removeCmd := fmt.Sprintf("dpkg-divert --local --rename --remove %s", targetUnamePath)
		if _, err := shell.ExecCmd(removeCmd, true, installRoot, nil); err != nil {
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
