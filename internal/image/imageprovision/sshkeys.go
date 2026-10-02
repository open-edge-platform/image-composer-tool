package imageprovision

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

// chownFn is a seam so tests running unprivileged can observe ownership
// changes without performing them.
var chownFn = func(root *os.Root, name string, uid, gid int) error {
	return root.Lchown(name, uid, gid)
}

// WriteAuthorizedKeys installs the user's SSH public keys into
// ~/.ssh/authorized_keys with the ownership and modes sshd's StrictModes
// requires (.ssh 0700, authorized_keys 0600, both owned by the user). Keys
// already present in the file are kept; configured keys are appended once.
func WriteAuthorizedKeys(installRoot string, user config.UserConfig) error {
	if len(user.SSHAuthorizedKeys) == 0 {
		return nil
	}
	return withRoot(installRoot, func(root *os.Root) error {
		entry, err := lookupPasswd(root, user.Name)
		if err != nil {
			return err
		}
		if !path.IsAbs(entry.home) || path.Clean(entry.home) == "/" {
			return fmt.Errorf("user %s has no usable home directory (%q) for authorized_keys", user.Name, entry.home)
		}
		sshDir := rel(path.Join(entry.home, ".ssh"))
		keysFile := sshDir + "/authorized_keys"

		// os.Root only stops links that leave the image; a link inside it
		// (/home/admin -> ../root, or /home -> /root) would still send the key
		// to another account, so every component of the destination is checked.
		if err := rejectSymlinkPath(root, keysFile); err != nil {
			return fmt.Errorf("user %s: %w", user.Name, err)
		}
		if err := root.MkdirAll(sshDir, 0700); err != nil {
			return fmt.Errorf("creating %s: %w", sshDir, err)
		}
		existing, err := readFileIfExists(root, keysFile)
		if err != nil {
			return err
		}
		if err := writeFile(root, keysFile, []byte(mergeKeyLines(existing, user.SSHAuthorizedKeys)), 0600); err != nil {
			return err
		}
		if err := root.Chmod(sshDir, 0700); err != nil {
			return fmt.Errorf("setting mode on /%s: %w", sshDir, err)
		}
		for _, name := range []string{sshDir, keysFile} {
			if err := chownFn(root, name, entry.uid, entry.gid); err != nil {
				return fmt.Errorf("setting owner on /%s: %w", name, err)
			}
		}
		log.Infof("Installed %d SSH authorized key(s) for user %s", len(user.SSHAuthorizedKeys), user.Name)
		return nil
	})
}

// rejectSymlinkPath walks name one component at a time and fails when any
// existing component is a symbolic link. Checking only the full path would
// miss a link in an intermediate directory, because Lstat follows those.
func rejectSymlinkPath(root *os.Root, name string) error {
	current := ""
	for _, part := range strings.Split(name, "/") {
		if part == "" {
			continue
		}
		current = path.Join(current, part)
		info, err := root.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			return nil // nothing below a missing component exists either
		}
		if err != nil {
			return fmt.Errorf("inspecting /%s: %w", current, err)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("/%s is a symbolic link; refusing to install authorized_keys through it", current)
		}
	}
	return nil
}

// mergeKeyLines appends each key not already present and returns the file
// content, always newline-terminated.
func mergeKeyLines(existing string, keys []string) string {
	present := make(map[string]bool)
	var lines []string
	for _, line := range strings.Split(existing, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		present[strings.TrimSpace(line)] = true
		lines = append(lines, line)
	}
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" || present[key] {
			continue
		}
		present[key] = true
		lines = append(lines, key)
	}
	return strings.Join(lines, "\n") + "\n"
}
