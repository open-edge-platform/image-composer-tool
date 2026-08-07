package system

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// sudoUserIDs returns the uid/gid of the user who invoked the tool via sudo.
//
// sudo exports SUDO_UID/SUDO_GID for the original (unprivileged) invoker. When
// they are absent the tool was not started through sudo — it is running as a
// genuine root login or inside a container where there is no unprivileged owner
// to hand artifacts back to — so ownership must be left as-is. A SUDO_UID of 0
// (root escalating to root) is treated the same: there is nothing to drop to.
// ok is false in every one of those cases so callers no-op rather than chowning
// files to uid 0 (a no-op that would still walk the tree pointlessly).
func sudoUserIDs() (uid, gid int, ok bool) {
	uidStr := strings.TrimSpace(os.Getenv("SUDO_UID"))
	gidStr := strings.TrimSpace(os.Getenv("SUDO_GID"))
	if uidStr == "" || gidStr == "" {
		return 0, 0, false
	}
	uid, err := strconv.Atoi(uidStr)
	if err != nil || uid <= 0 {
		return 0, 0, false
	}
	gid, err = strconv.Atoi(gidStr)
	if err != nil || gid < 0 {
		return 0, 0, false
	}
	return uid, gid, true
}

// ChownArtifactsToSudoUser returns ownership of the build's output to the user
// who invoked the tool via `sudo`, so the resulting image/SBOM are readable and
// removable without root.
//
// The tool runs as root (for loop devices, mount, chroot), which leaves every
// directory and file it creates owned by root:root — including the final image
// directory — so the invoking user cannot read or delete their own build output.
// This restores access with the minimum ownership change:
//
//   - The path nodes from workDir down to imageBuildDir's parent are chowned
//     non-recursively (one inode each). That transfers ownership of the directory
//     nodes themselves — enough for the user to traverse (cd) and list them —
//     WITHOUT touching their other children. In particular the sibling chroot
//     trees (chrootenv/, chrootbuild/) keep their root:root ownership, which they
//     must: they are real root filesystems whose file ownership is load-bearing.
//   - imageBuildDir itself is chowned recursively, so every artifact inside it
//     (the .raw/.iso, SBOM, checksums, converted images) becomes user-owned.
//
// imageBuildDir must be equal to or nested under workDir; both must be absolute.
// The walk uses Lchown and does not follow symlinks, so a symlink planted in the
// artifact directory cannot redirect the chown outside the tree.
//
// It is best-effort: individual chown failures are collected and returned as a
// single joined error for the caller to log, but they never abort the build —
// a successfully built image the user cannot chmod is still a successfully built
// image. When the tool was not invoked via sudo it is a no-op returning nil.
func ChownArtifactsToSudoUser(workDir, imageBuildDir string) error {
	uid, gid, ok := sudoUserIDs()
	if !ok {
		log.Debugf("SUDO_UID/SUDO_GID unset or root; leaving build artifact ownership unchanged")
		return nil
	}

	workDir = filepath.Clean(workDir)
	imageBuildDir = filepath.Clean(imageBuildDir)
	if !filepath.IsAbs(workDir) || !filepath.IsAbs(imageBuildDir) {
		return fmt.Errorf("chown artifacts: workDir %q and imageBuildDir %q must both be absolute", workDir, imageBuildDir)
	}

	// Reject an obvious escape on the lexical paths first, so a target that is
	// plainly outside workDir is refused even when it does not exist yet.
	if err := assertContained(workDir, imageBuildDir); err != nil {
		return err
	}

	// Lchown does not follow a final symlink, but a symlinked *intermediate*
	// component (e.g. workDir/<provider> pointing at /etc) is resolved by the
	// kernel during path traversal, so the recursive walk below would descend the
	// real target instead of the tree we validated. Resolve both paths to their
	// real locations and re-check containment so the paths we walk are the ones we
	// vetted. Resolution is best-effort: a not-yet-materialised path keeps its
	// cleaned form (the lexical check above already ran).
	if resolved, err := filepath.EvalSymlinks(workDir); err == nil {
		workDir = resolved
	}
	if resolved, err := filepath.EvalSymlinks(imageBuildDir); err == nil {
		imageBuildDir = resolved
	}
	// Never recursively chown a system root (e.g. a workDir misconfigured to "/"
	// or a top-level directory), matching ChownDirTreeToSudoUser's guard.
	if err := refuseDangerousChownRoot(workDir); err != nil {
		return err
	}
	if err := assertContained(workDir, imageBuildDir); err != nil {
		return err
	}
	rel, err := filepath.Rel(workDir, imageBuildDir)
	if err != nil {
		return fmt.Errorf("chown artifacts: relating %q to %q: %w", imageBuildDir, workDir, err)
	}

	log.Infof("returning build artifact ownership to invoking user %d:%d under %s", uid, gid, workDir)

	var errs []error
	// Chown the path nodes non-recursively, from the workspace root down to (but
	// not including) the artifact leaf. When rel == "." the leaf IS the workspace
	// root and the loop body never runs — the recursive chown below covers it.
	current := workDir
	if err := chownNode(current, uid, gid); err != nil {
		errs = append(errs, err)
	}
	if rel != "." {
		for _, comp := range strings.Split(rel, string(os.PathSeparator)) {
			current = filepath.Join(current, comp)
			if current == imageBuildDir {
				break // the leaf is handled by the recursive walk below
			}
			if err := chownNode(current, uid, gid); err != nil {
				errs = append(errs, err)
			}
		}
	}

	// Chown the artifact directory and everything inside it recursively.
	errs = append(errs, chownTree(imageBuildDir, uid, gid)...)

	return errors.Join(errs...)
}

// ChownDirTreeToSudoUser recursively returns ownership of an entire directory
// tree to the user who invoked the tool via sudo. It is the coarse companion to
// ChownArtifactsToSudoUser, intended for the cache and temp directories: these
// hold only inert build inputs (downloaded packages, GPG keys, decompressed
// metadata) — NOT an extracted root filesystem whose file ownership is
// load-bearing — so a blanket chown is safe and lets the user re-run or clean
// up without root.
//
// It refuses to touch clearly-dangerous roots — "/", the system temp directory
// (which ICT falls back to when temp_dir is left empty; see config.TempDir),
// and any other path that is not a private, ICT-managed subdirectory — so a
// misconfiguration can never turn this into a recursive chown of /tmp or the
// whole filesystem. dir must be absolute.
//
// Like its sibling it is best-effort (per-file failures are joined and
// returned, never fatal) and a no-op when the tool was not launched via sudo.
func ChownDirTreeToSudoUser(dir string) error {
	uid, gid, ok := sudoUserIDs()
	if !ok {
		log.Debugf("SUDO_UID/SUDO_GID unset or root; leaving %s ownership unchanged", dir)
		return nil
	}

	dir = filepath.Clean(dir)
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("chown tree: %q must be absolute", dir)
	}
	// Resolve symlink components before validating and walking: refuseDangerousChownRoot
	// and WalkDir must act on the same real directory. Otherwise a symlinked component
	// (or dir itself being a symlink to, say, "/") could pass the string check yet make
	// the walk chown a different tree than the one vetted. Best-effort: a path that does
	// not exist yet keeps its cleaned form and is still checked lexically.
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	if err := refuseDangerousChownRoot(dir); err != nil {
		return err
	}

	log.Infof("returning ownership of %s to invoking user %d:%d", dir, uid, gid)
	return errors.Join(chownTree(dir, uid, gid)...)
}

// assertContained returns an error unless child equals or is nested under parent.
// Both must already be absolute and cleaned. It is the lexical containment check
// shared by ChownArtifactsToSudoUser's pre- and post-symlink-resolution guards.
func assertContained(parent, child string) error {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return fmt.Errorf("chown artifacts: relating %q to %q: %w", child, parent, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("chown artifacts: imageBuildDir %q escapes workDir %q", child, parent)
	}
	return nil
}

// refuseDangerousChownRoot rejects paths that must never be recursively chowned:
// the filesystem root, the system temp directory (ICT's empty-temp_dir
// fallback), and any top-level directory (a path with no parent segment, e.g.
// "/tmp" or "/home"). A safe target is a nested, ICT-created directory such as
// "<repo>/cache" or "<repo>/tmp".
func refuseDangerousChownRoot(dir string) error {
	if dir == string(os.PathSeparator) {
		return fmt.Errorf("refusing to recursively chown filesystem root %q", dir)
	}
	sys := filepath.Clean(os.TempDir())
	if resolved, err := filepath.EvalSymlinks(sys); err == nil {
		sys = resolved
	}
	if dir == sys {
		return fmt.Errorf("refusing to recursively chown the system temp directory %q "+
			"(set a dedicated temp_dir to enable ownership restore)", dir)
	}
	// Refuse common shared system trees (or their descendants) that are not
	// ICT-managed private workspaces and are likely to be used by other services.
	for _, root := range []string{"/var/tmp", "/var/cache", "/var/lib"} {
		if dir == root || strings.HasPrefix(dir, root+string(os.PathSeparator)) {
			return fmt.Errorf("refusing to recursively chown shared system directory %q", dir)
		}
	}
	// A top-level directory like "/tmp" or "/var" has a parent of "/". ICT's own
	// cache/temp dirs are always nested at least one level below a top-level dir
	// (e.g. "<workspaceroot>/cache"), so a parent of "/" signals a bare system
	// directory we must not touch.
	if filepath.Dir(dir) == string(os.PathSeparator) {
		return fmt.Errorf("refusing to recursively chown top-level directory %q", dir)
	}
	return nil
}

// chownTree recursively lchowns dir and everything under it, returning the
// collected per-entry errors (empty when fully successful). Walk errors and
// individual chown failures are accumulated rather than aborting, so a single
// unreadable entry does not prevent the rest of the tree from being fixed.
func chownTree(dir string, uid, gid int) []error {
	var errs []error
	walkErr := filepath.WalkDir(dir, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			errs = append(errs, err)
			return nil // keep walking; best-effort
		}
		if chErr := chownNode(path, uid, gid); chErr != nil {
			errs = append(errs, chErr)
		}
		return nil
	})
	if walkErr != nil {
		errs = append(errs, fmt.Errorf("walking directory %q: %w", dir, walkErr))
	}
	return errs
}

// lchown is the ownership-changing primitive, indirected through a package
// variable so tests can record calls without needing real root (os.Lchown of a
// file to another uid requires CAP_CHOWN). Production always uses os.Lchown,
// which does not follow a final symlink.
var lchown = os.Lchown

// chownNode changes ownership of a single path without following a final
// symlink. A "no such file" is ignored: the path-node walk visits directories
// that are always present, but the recursive walk can race a concurrent
// remove, and a missing node needs no ownership fix.
func chownNode(path string, uid, gid int) error {
	if err := lchown(path, uid, gid); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("chown %q to %d:%d: %w", path, uid, gid, err)
	}
	return nil
}
