// Package imageprovision writes the deployed-system provisioning settings —
// SSH authorized keys, proxy, cloud-init seed, boot-time provisioning units,
// and the APT upgrade policy — into an image root. It runs identically for a
// raw build and for the live installer on target, and only touches files
// through an os.Root so no path from the template can escape the image root.
package imageprovision

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
)

var log = logger.Logger()

// withRoot opens installRoot as an os.Root for the duration of fn.
func withRoot(installRoot string, fn func(root *os.Root) error) (err error) {
	root, err := os.OpenRoot(installRoot)
	if err != nil {
		return fmt.Errorf("opening image root %s: %w", installRoot, err)
	}
	defer func() {
		if cerr := root.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("closing image root %s: %w", installRoot, cerr)
		}
	}()
	return fn(root)
}

// rel converts an absolute in-image path into an os.Root-relative name.
func rel(p string) string {
	return strings.TrimPrefix(p, "/")
}

// writeFile creates parent directories and writes data with the given mode.
// WriteFile only applies perm to new files, so the mode is set explicitly.
func writeFile(root *os.Root, name string, data []byte, perm os.FileMode) error {
	if dir := parentDir(name); dir != "" {
		if err := root.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("creating directory /%s: %w", dir, err)
		}
	}
	if err := root.WriteFile(name, data, perm); err != nil {
		return fmt.Errorf("writing /%s: %w", name, err)
	}
	if err := root.Chmod(name, perm); err != nil {
		return fmt.Errorf("setting mode on /%s: %w", name, err)
	}
	return nil
}

// readFileIfExists returns the file content, or "" when it does not exist.
func readFileIfExists(root *os.Root, name string) (string, error) {
	data, err := root.ReadFile(name)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading /%s: %w", name, err)
	}
	return string(data), nil
}

func parentDir(name string) string {
	if i := strings.LastIndex(name, "/"); i > 0 {
		return name[:i]
	}
	return ""
}

// passwdEntry is the subset of an /etc/passwd line needed here.
type passwdEntry struct {
	uid, gid int
	home     string
}

// lookupPasswd finds a user in the image's /etc/passwd.
func lookupPasswd(root *os.Root, name string) (passwdEntry, error) {
	content, err := root.ReadFile("etc/passwd")
	if err != nil {
		return passwdEntry{}, fmt.Errorf("reading /etc/passwd: %w", err)
	}
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) != 7 || fields[0] != name {
			continue
		}
		uid, uerr := strconv.Atoi(fields[2])
		gid, gerr := strconv.Atoi(fields[3])
		if uerr != nil || gerr != nil {
			return passwdEntry{}, fmt.Errorf("malformed /etc/passwd entry for %q", name)
		}
		return passwdEntry{uid: uid, gid: gid, home: fields[5]}, nil
	}
	return passwdEntry{}, fmt.Errorf("no /etc/passwd entry for user %q", name)
}
