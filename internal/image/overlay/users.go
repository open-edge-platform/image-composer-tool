package overlay

import (
	"fmt"
	"os"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/image/imageos"
)

// baselinePasswdPath is the in-image path to the account database read to detect
// whether a requested overlay user already exists in the baseline.
const baselinePasswdPath = "/etc/passwd"

// createUsersFn is the seam over create mode's user provisioning, so the overlay
// user stage's mount/create orchestration is unit-testable without root or a real
// chroot. Production uses imageos.CreateUsers; tests override it.
var createUsersFn = imageos.CreateUsers

// ValidateOverlayUsers fails the build when a user requested by the template's
// systemConfig.users already exists in the baseline image. An overlay adds users
// onto an existing baseline; re-declaring an account that the baseline already
// ships is ambiguous (it would silently collide with a different UID/home/shell),
// so it is rejected up front — during Preprocess, before any package install or
// resize mutates the copied baseline.
//
// It reads the baseline's /etc/passwd through resolveInRoot so a symlinked passwd
// stays confined to rootMount and cannot escape onto the host filesystem, mirroring
// how readOSRelease reads os-release. A template declaring no users is a no-op.
func ValidateOverlayUsers(template *config.ImageTemplate, rootMount string) error {
	if template == nil {
		return fmt.Errorf("overlay users: template cannot be nil")
	}
	users := template.GetUsers()
	if len(users) == 0 {
		return nil
	}
	if strings.TrimSpace(rootMount) == "" {
		return fmt.Errorf("overlay users: baseline root mount path cannot be empty")
	}

	if err := assertNoBaselineUserConflicts(users, rootMount); err != nil {
		return err
	}

	log.Infof("Overlay users: %d requested user(s) validated against baseline; no conflicts", len(users))
	return nil
}

// assertNoBaselineUserConflicts returns an error naming every requested user that
// already exists in the baseline account database at rootMount. It is the shared
// guard behind both the Preprocess validation and the immediately-before-creation
// re-check, so a name present in the baseline can never be provisioned "over".
func assertNoBaselineUserConflicts(users []config.UserConfig, rootMount string) error {
	existing, err := readBaselineUsernames(rootMount)
	if err != nil {
		return fmt.Errorf("overlay users: failed to read baseline account database: %w", err)
	}

	var conflicts []string
	for _, u := range users {
		name := strings.TrimSpace(u.Name)
		if name == "" {
			continue
		}
		if _, ok := existing[name]; ok {
			conflicts = append(conflicts, name)
		}
	}
	if len(conflicts) > 0 {
		return fmt.Errorf("overlay users: cannot add user(s) %s: already present in the baseline image; "+
			"an overlay cannot redefine a baseline account — remove them from systemConfig.users",
			strings.Join(conflicts, ", "))
	}
	return nil
}

// readBaselineUsernames parses the baseline /etc/passwd and returns the set of
// usernames it defines. The passwd path is resolved inside rootMount so a symlink
// cannot redirect the read outside the baseline root.
func readBaselineUsernames(rootMount string) (map[string]struct{}, error) {
	hostPath, err := resolveInRoot(rootMount, baselinePasswdPath)
	if err != nil {
		return nil, fmt.Errorf("resolving %s under %s: %w", baselinePasswdPath, rootMount, err)
	}

	// Read the whole file (passwd is small) rather than streaming it, mirroring how
	// readOSRelease consumes a resolved-in-root file and avoiding a deferred Close
	// whose error would have to be threaded back on this read-only path.
	data, err := os.ReadFile(hostPath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", hostPath, err)
	}

	names := make(map[string]struct{})
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// passwd format: name:passwd:uid:gid:gecos:home:shell — the username is the
		// first colon-separated field.
		name := line
		if idx := strings.IndexByte(line, ':'); idx >= 0 {
			name = line[:idx]
		}
		if name != "" {
			names[name] = struct{}{}
		}
	}
	return names, nil
}

// RunOverlayUsers provisions the template's systemConfig.users into the mounted
// baseline root, reusing create mode's user-creation logic (imageos.CreateUsers):
// useradd, password/hash handling, group and sudo membership, and startup-script
// wiring. It is a no-op when the template declares no users.
//
// It re-checks the baseline account database immediately before creating, not just
// in Preprocess: package installation runs between that gate and this stage, and a
// package maintainer script can create a service account with a requested name.
// create mode's useradd treats an already-existing account as non-fatal and would
// then reset its password/groups/shell — so an account that appeared after the
// preflight check must fail the build here rather than be silently modified.
//
// The kernel pseudo-filesystems are mounted for the duration (so useradd and its
// PAM/maintainer helpers behave as on a live system) and torn down on every return
// path, matching RunOverlayConfigurations.
func RunOverlayUsers(template *config.ImageTemplate, rootMount string) (err error) {
	if template == nil {
		return fmt.Errorf("overlay users: template cannot be nil")
	}
	if len(template.GetUsers()) == 0 {
		log.Debug("Overlay users: no users to create")
		return nil
	}
	if strings.TrimSpace(rootMount) == "" {
		return fmt.Errorf("overlay users: baseline root mount path cannot be empty")
	}

	// Authoritative conflict gate, evaluated against the post-install baseline so a
	// package-created account with a requested name fails instead of being modified.
	if err = assertNoBaselineUserConflicts(template.GetUsers(), rootMount); err != nil {
		return err
	}

	log.Infof("Overlay users: creating %d user(s) in %s", len(template.GetUsers()), rootMount)

	// Mount the pseudo-filesystems so chrooted account tooling behaves as on a live
	// system, and tear them down on every return path.
	if err = mountSysfs(rootMount); err != nil {
		if uerr := umountSysfs(rootMount); uerr != nil {
			log.Warnf("Overlay users: rollback after failed sysfs mount also failed: %v", uerr)
		}
		return fmt.Errorf("overlay users: failed to mount pseudo-filesystems into %s: %w", rootMount, err)
	}
	defer func() {
		if uerr := umountSysfs(rootMount); uerr != nil {
			log.Errorf("Overlay users: failed to unmount pseudo-filesystems from %s: %v", rootMount, uerr)
			recordCleanupError(&err, fmt.Errorf("failed to unmount pseudo-filesystems from %s: %w", rootMount, uerr))
		}
	}()

	if cerr := createUsersFn(rootMount, template); cerr != nil {
		return fmt.Errorf("overlay users: user creation failed: %w", cerr)
	}

	log.Infof("Overlay users: %d user(s) created", len(template.GetUsers()))
	return nil
}
