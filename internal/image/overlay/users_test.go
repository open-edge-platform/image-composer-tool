package overlay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

// writeBaselinePasswd creates a minimal baseline root at a temp dir with an
// /etc/passwd containing the given usernames, and returns the root mount path.
func writeBaselinePasswd(t *testing.T, usernames ...string) string {
	t.Helper()
	root := t.TempDir()
	etc := filepath.Join(root, "etc")
	if err := os.MkdirAll(etc, 0o755); err != nil {
		t.Fatalf("mkdir etc: %v", err)
	}
	var b strings.Builder
	b.WriteString("# baseline passwd\n\n")
	for i, name := range usernames {
		// name:x:uid:gid:gecos:home:shell
		b.WriteString(name)
		b.WriteString(":x:")
		b.WriteString(string(rune('0' + (1000+i)%10)))
		b.WriteString(":1000::/home/")
		b.WriteString(name)
		b.WriteString(":/bin/bash\n")
	}
	if err := os.WriteFile(filepath.Join(etc, "passwd"), []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write passwd: %v", err)
	}
	return root
}

func usersTemplate(names ...string) *config.ImageTemplate {
	users := make([]config.UserConfig, 0, len(names))
	for _, n := range names {
		users = append(users, config.UserConfig{Name: n})
	}
	return &config.ImageTemplate{SystemConfig: config.SystemConfig{Users: users}}
}

func TestValidateOverlayUsers_NoUsersIsNoOp(t *testing.T) {
	// No users: must return nil without needing a readable baseline passwd.
	if err := ValidateOverlayUsers(usersTemplate(), "/nonexistent/root"); err != nil {
		t.Fatalf("expected no-op for empty users, got %v", err)
	}
}

func TestValidateOverlayUsers_Validation(t *testing.T) {
	if err := ValidateOverlayUsers(nil, "/wd/mnt/root"); err == nil {
		t.Error("expected error for nil template")
	}
	if err := ValidateOverlayUsers(usersTemplate("admin"), "   "); err == nil {
		t.Error("expected error for empty root mount")
	}
}

func TestValidateOverlayUsers_NoConflict(t *testing.T) {
	root := writeBaselinePasswd(t, "root", "daemon", "bin")
	if err := ValidateOverlayUsers(usersTemplate("opsadmin", "deploy"), root); err != nil {
		t.Fatalf("expected nil for non-colliding users, got %v", err)
	}
}

func TestValidateOverlayUsers_Conflict(t *testing.T) {
	root := writeBaselinePasswd(t, "root", "daemon", "opsadmin")
	err := ValidateOverlayUsers(usersTemplate("deploy", "opsadmin"), root)
	if err == nil {
		t.Fatal("expected an error when a requested user already exists in the baseline")
	}
	if !strings.Contains(err.Error(), "opsadmin") {
		t.Errorf("error should name the conflicting user, got: %v", err)
	}
	if strings.Contains(err.Error(), "deploy") {
		t.Errorf("error should not name the non-conflicting user, got: %v", err)
	}
}

func TestValidateOverlayUsers_ReportsAllConflicts(t *testing.T) {
	root := writeBaselinePasswd(t, "root", "alice", "bob")
	err := ValidateOverlayUsers(usersTemplate("alice", "bob", "carol"), root)
	if err == nil {
		t.Fatal("expected an error for multiple conflicts")
	}
	if !strings.Contains(err.Error(), "alice") || !strings.Contains(err.Error(), "bob") {
		t.Errorf("expected both conflicts named, got: %v", err)
	}
}

func TestValidateOverlayUsers_MissingPasswdIsError(t *testing.T) {
	// A baseline root with no /etc/passwd cannot be validated; surfacing an error
	// (rather than silently passing) is the safe behavior.
	root := t.TempDir()
	if err := ValidateOverlayUsers(usersTemplate("admin"), root); err == nil {
		t.Error("expected error when baseline /etc/passwd is absent")
	}
}

func TestRunOverlayUsers_NoUsersIsNoOp(t *testing.T) {
	// No users: must return nil WITHOUT touching the mount/create seams.
	origMount, origUmount, origCreate := mountSysfs, umountSysfs, createUsersFn
	defer func() { mountSysfs, umountSysfs, createUsersFn = origMount, origUmount, origCreate }()
	mountSysfs = func(string) error { t.Fatal("mountSysfs must not be called for empty users"); return nil }
	umountSysfs = func(string) error { t.Fatal("umountSysfs must not be called for empty users"); return nil }
	createUsersFn = func(string, *config.ImageTemplate) error {
		t.Fatal("createUsersFn must not be called for empty users")
		return nil
	}
	if err := RunOverlayUsers(usersTemplate(), "/wd/mnt/root"); err != nil {
		t.Fatalf("expected no-op for empty users, got %v", err)
	}
}

func TestRunOverlayUsers_Validation(t *testing.T) {
	if err := RunOverlayUsers(nil, "/wd/mnt/root"); err == nil {
		t.Error("expected error for nil template")
	}
	if err := RunOverlayUsers(usersTemplate("admin"), "   "); err == nil {
		t.Error("expected error for empty root mount")
	}
}

func TestRunOverlayUsers_MountsAndCreates(t *testing.T) {
	origMount, origUmount, origCreate := mountSysfs, umountSysfs, createUsersFn
	defer func() { mountSysfs, umountSysfs, createUsersFn = origMount, origUmount, origCreate }()

	var mounted, umounted int
	mountSysfs = func(string) error { mounted++; return nil }
	umountSysfs = func(string) error { umounted++; return nil }

	var createdRoot string
	var createdUsers int
	createUsersFn = func(root string, tmpl *config.ImageTemplate) error {
		createdRoot = root
		createdUsers = len(tmpl.GetUsers())
		return nil
	}

	// The pre-creation re-check reads the baseline /etc/passwd, so a real baseline
	// root is required; the requested users must not collide with it.
	root := writeBaselinePasswd(t, "root", "daemon")
	if err := RunOverlayUsers(usersTemplate("opsadmin", "deploy"), root); err != nil {
		t.Fatalf("RunOverlayUsers: %v", err)
	}
	if mounted != 1 || umounted != 1 {
		t.Errorf("expected one mount and one unmount, got mounted=%d umounted=%d", mounted, umounted)
	}
	if createdRoot != root {
		t.Errorf("createUsersFn got root %q, want %q", createdRoot, root)
	}
	if createdUsers != 2 {
		t.Errorf("createUsersFn got %d users, want 2", createdUsers)
	}
}

func TestRunOverlayUsers_UnmountsOnCreateFailure(t *testing.T) {
	origMount, origUmount, origCreate := mountSysfs, umountSysfs, createUsersFn
	defer func() { mountSysfs, umountSysfs, createUsersFn = origMount, origUmount, origCreate }()

	var umounted int
	mountSysfs = func(string) error { return nil }
	umountSysfs = func(string) error { umounted++; return nil }
	createUsersFn = func(string, *config.ImageTemplate) error {
		return os.ErrPermission
	}

	root := writeBaselinePasswd(t, "root", "daemon")
	if err := RunOverlayUsers(usersTemplate("admin"), root); err == nil {
		t.Fatal("expected error to propagate from createUsersFn")
	}
	if umounted != 1 {
		t.Errorf("expected pseudo-filesystems to be unmounted on failure, got umounted=%d", umounted)
	}
}

// TestRunOverlayUsers_FailsWhenUserExistsInBaseline covers the authoritative
// re-check: a requested user that is present in the baseline at creation time (for
// example added by a package maintainer script during install, after the
// Preprocess gate) must fail before any account is created — without touching the
// mount or create seams.
func TestRunOverlayUsers_FailsWhenUserExistsInBaseline(t *testing.T) {
	origMount, origUmount, origCreate := mountSysfs, umountSysfs, createUsersFn
	defer func() { mountSysfs, umountSysfs, createUsersFn = origMount, origUmount, origCreate }()
	mountSysfs = func(string) error { t.Fatal("mountSysfs must not run on a baseline conflict"); return nil }
	umountSysfs = func(string) error { t.Fatal("umountSysfs must not run on a baseline conflict"); return nil }
	createUsersFn = func(string, *config.ImageTemplate) error {
		t.Fatal("createUsersFn must not run on a baseline conflict")
		return nil
	}

	root := writeBaselinePasswd(t, "root", "opsadmin")
	err := RunOverlayUsers(usersTemplate("opsadmin"), root)
	if err == nil {
		t.Fatal("expected RunOverlayUsers to fail when a requested user already exists in the baseline")
	}
	if !strings.Contains(err.Error(), "opsadmin") {
		t.Errorf("error should name the conflicting user, got: %v", err)
	}
}
