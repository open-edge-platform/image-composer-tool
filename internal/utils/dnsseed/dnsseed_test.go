package dnsseed

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const hostContent = "nameserver 10.0.0.1\nsearch example.test\n"

// newFixture builds a throwaway chroot tree and points hostResolvConf at a fake host
// resolv.conf, restoring both package vars on cleanup. It returns the chroot root.
func newFixture(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "etc"), 0o755); err != nil {
		t.Fatalf("mkdir etc: %v", err)
	}

	host := filepath.Join(t.TempDir(), "host-resolv.conf")
	if err := os.WriteFile(host, []byte(hostContent), 0o644); err != nil {
		t.Fatalf("write host resolv.conf: %v", err)
	}

	origHost, origExec := hostResolvConf, execFn
	hostResolvConf = host
	t.Cleanup(func() { hostResolvConf, execFn = origHost, origExec })

	return root
}

// captureExec swaps in a recording executor that performs mkdir -p, cp and rm -f for
// real (so tests can assert on the resulting tree) without needing sudo.
func captureExec(t *testing.T, cmds *[]string) {
	t.Helper()
	execFn = func(cmdStr string, _ bool, _ string, _ []string) (string, error) {
		*cmds = append(*cmds, cmdStr)
		fields := strings.Fields(strings.ReplaceAll(cmdStr, "'", ""))
		switch {
		case strings.HasPrefix(cmdStr, "mkdir -p ") && len(fields) == 3:
			return "", os.MkdirAll(fields[2], 0o755)
		case strings.HasPrefix(cmdStr, "cp ") && len(fields) == 3:
			data, err := os.ReadFile(fields[1])
			if err != nil {
				return "", err
			}
			return "", os.WriteFile(fields[2], data, 0o644)
		case strings.HasPrefix(cmdStr, "rm -f ") && len(fields) == 3:
			if err := os.Remove(fields[2]); err != nil && !os.IsNotExist(err) {
				return "", err
			}
		}
		return "", nil
	}
}

// TestSeedWhenResolvConfAbsent is the create-mode case that the PTL worker build hit:
// mmdebstrap leaves no /etc/resolv.conf, so name resolution fails instantly. The seed
// must create one, and restore must remove it so createResolvConfSymlink still sees
// an absent file and nothing leaks into the shipped image.
func TestSeedWhenResolvConfAbsent(t *testing.T) {
	root := newFixture(t)
	var cmds []string
	captureExec(t, &cmds)

	etcResolv := filepath.Join(root, "etc", "resolv.conf")
	restore := SeedEphemeral(root)

	got, err := os.ReadFile(etcResolv)
	if err != nil {
		t.Fatalf("expected a seeded resolver at %s: %v", etcResolv, err)
	}
	if string(got) != hostContent {
		t.Errorf("seeded content = %q, want %q", got, hostContent)
	}

	restore()

	if _, err := os.Stat(etcResolv); !os.IsNotExist(err) {
		t.Errorf("restore left %s behind (err=%v); it would suppress createResolvConfSymlink", etcResolv, err)
	}
}

// TestSeedWhenSymlinkIntoRun covers the overlay-mode shape: /etc/resolv.conf is a
// dangling symlink into the ephemeral /run tmpfs.
func TestSeedWhenSymlinkIntoRun(t *testing.T) {
	for _, tc := range []struct {
		name, link, wantAt string
	}{
		{"relative link", "../run/systemd/resolve/stub-resolv.conf", "run/systemd/resolve/stub-resolv.conf"},
		{"absolute link", "/run/systemd/resolve/stub-resolv.conf", "run/systemd/resolve/stub-resolv.conf"},
		{"run root", "/run/resolv.conf", "run/resolv.conf"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := newFixture(t)
			if err := os.Symlink(tc.link, filepath.Join(root, "etc", "resolv.conf")); err != nil {
				t.Fatalf("symlink: %v", err)
			}
			var cmds []string
			captureExec(t, &cmds)

			restore := SeedEphemeral(root)

			target := filepath.Join(root, tc.wantAt)
			got, err := os.ReadFile(target)
			if err != nil {
				t.Fatalf("expected seeded resolver at %s: %v", tc.wantAt, err)
			}
			if string(got) != hostContent {
				t.Errorf("seeded content = %q, want %q", got, hostContent)
			}

			restore()

			if _, err := os.Stat(target); !os.IsNotExist(err) {
				t.Errorf("restore left %s behind (err=%v)", target, err)
			}
			// The symlink itself must survive untouched — it is part of the image.
			if _, err := os.Lstat(filepath.Join(root, "etc", "resolv.conf")); err != nil {
				t.Errorf("the /etc/resolv.conf symlink was disturbed: %v", err)
			}
		})
	}
}

// TestSeedWhenRegularFileHasNoNameserver covers a present-but-useless resolv.conf:
// seed over it, then put the original bytes back so the image ships what it shipped.
func TestSeedWhenRegularFileHasNoNameserver(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"empty", ""},
		{"comments only", "# nothing here\n; also nothing\n"},
		{"options but no nameserver", "search corp.example\noptions ndots:5\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := newFixture(t)
			etcResolv := filepath.Join(root, "etc", "resolv.conf")
			if err := os.WriteFile(etcResolv, []byte(tc.body), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
			var cmds []string
			captureExec(t, &cmds)

			restore := SeedEphemeral(root)

			got, err := os.ReadFile(etcResolv)
			if err != nil {
				t.Fatalf("read seeded: %v", err)
			}
			if string(got) != hostContent {
				t.Errorf("seeded content = %q, want %q", got, hostContent)
			}

			restore()

			back, err := os.ReadFile(etcResolv)
			if err != nil {
				t.Fatalf("read restored: %v", err)
			}
			if string(back) != tc.body {
				t.Errorf("restored content = %q, want original %q", back, tc.body)
			}
		})
	}
}

// TestSeedLeavesUsableAndPersistentPathsUntouched is the safety property: a resolver
// that already works, and any symlink that would write through into the delivered
// image, must be left exactly as found with no command executed.
func TestSeedLeavesUsableAndPersistentPathsUntouched(t *testing.T) {
	const working = "nameserver 8.8.8.8\n"

	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, root string)
	}{
		{"regular file with nameserver", func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, "etc", "resolv.conf"), []byte(working), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
		}},
		{"symlink outside run", func(t *testing.T, root string) {
			if err := os.Symlink("/etc/real-resolv.conf", filepath.Join(root, "etc", "resolv.conf")); err != nil {
				t.Fatalf("symlink: %v", err)
			}
		}},
		{"symlink to run-adjacent decoy", func(t *testing.T, root string) {
			// "/runtime/..." must not satisfy the /run prefix check.
			if err := os.Symlink("/runtime/resolv.conf", filepath.Join(root, "etc", "resolv.conf")); err != nil {
				t.Fatalf("symlink: %v", err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := newFixture(t)
			tc.setup(t, root)

			var cmds []string
			captureExec(t, &cmds)

			restore := SeedEphemeral(root)
			restore()

			if len(cmds) != 0 {
				t.Errorf("expected no commands, got: %v", cmds)
			}
			if _, err := os.Stat(filepath.Join(root, "run")); !os.IsNotExist(err) {
				t.Errorf("a /run tree was created that should not have been (err=%v)", err)
			}
			if data, err := os.ReadFile(filepath.Join(root, "etc", "resolv.conf")); err == nil {
				if string(data) != working {
					t.Errorf("resolv.conf mutated: got %q, want %q", data, working)
				}
			}
		})
	}
}

// TestSeedSkipsWhenHostResolverUnreadable covers the best-effort contract: with no
// readable host resolver there is nothing to copy, and the caller must still get a
// usable restore function.
func TestSeedSkipsWhenHostResolverUnreadable(t *testing.T) {
	root := newFixture(t)
	hostResolvConf = filepath.Join(t.TempDir(), "does-not-exist")

	var cmds []string
	captureExec(t, &cmds)

	SeedEphemeral(root)() // must not panic

	if len(cmds) != 0 {
		t.Errorf("expected no commands when host resolver is unreadable, got: %v", cmds)
	}
}

// TestSeedSwallowsExecFailure asserts the helper stays best-effort when the write
// into the root-owned tree fails: no panic, and a usable restore.
func TestSeedSwallowsExecFailure(t *testing.T) {
	root := newFixture(t)
	execFn = func(string, bool, string, []string) (string, error) {
		return "", os.ErrPermission
	}

	SeedEphemeral(root)() // must simply return
}

// TestSeedRemovesStagingFile guards against leaking a staging file on the success
// path.
func TestSeedRemovesStagingFile(t *testing.T) {
	root := newFixture(t)
	var cmds []string
	captureExec(t, &cmds)

	SeedEphemeral(root)()

	var cp string
	for _, c := range cmds {
		if strings.HasPrefix(c, "cp ") {
			cp = c
			break
		}
	}
	if cp == "" {
		t.Fatalf("expected a cp command, got: %v", cmds)
	}
	fields := strings.Fields(strings.ReplaceAll(cp, "'", ""))
	if len(fields) != 3 {
		t.Fatalf("unexpected cp shape: %q", cp)
	}
	if _, err := os.Stat(fields[1]); !os.IsNotExist(err) {
		t.Errorf("staging file %s was not cleaned up (err=%v)", fields[1], err)
	}
}

// failingExec runs commands for real except those whose prefix matches failOn, which
// return an error. It lets each best-effort bail-out be exercised independently.
func failingExec(t *testing.T, failOn string) {
	t.Helper()
	var cmds []string
	captureExec(t, &cmds)
	real := execFn
	execFn = func(cmdStr string, sudo bool, chroot string, env []string) (string, error) {
		if strings.HasPrefix(cmdStr, failOn) {
			return "", os.ErrPermission
		}
		return real(cmdStr, sudo, chroot, env)
	}
}

// TestSeedBestEffortAcrossShapesAndFailures asserts that whichever shell step fails,
// for whichever rootfs shape, Seed neither panics nor propagates — and always hands
// back a callable restore.
func TestSeedBestEffortAcrossShapesAndFailures(t *testing.T) {
	shapes := map[string]func(t *testing.T, root string){
		"absent": func(_ *testing.T, _ string) {},
		"symlink into run": func(t *testing.T, root string) {
			if err := os.Symlink("/run/systemd/resolve/stub-resolv.conf", filepath.Join(root, "etc", "resolv.conf")); err != nil {
				t.Fatalf("symlink: %v", err)
			}
		},
		"regular file without nameserver": func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, "etc", "resolv.conf"), []byte("# empty\n"), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
		},
	}

	for shapeName, setup := range shapes {
		for _, failOn := range []string{"mkdir", "cp", "rm"} {
			t.Run(shapeName+"/fail "+failOn, func(t *testing.T) {
				root := newFixture(t)
				setup(t, root)
				failingExec(t, failOn)

				SeedEphemeral(root)() // seed + restore, neither may panic or propagate
			})
		}
	}
}

// TestSeedSkipsUnexpectedFileType covers the defensive default branch: a directory
// (or any non-file, non-symlink) at /etc/resolv.conf must be left alone.
func TestSeedSkipsUnexpectedFileType(t *testing.T) {
	root := newFixture(t)
	if err := os.MkdirAll(filepath.Join(root, "etc", "resolv.conf"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	var cmds []string
	captureExec(t, &cmds)

	SeedEphemeral(root)()

	if len(cmds) != 0 {
		t.Errorf("expected no commands for an unexpected file type, got: %v", cmds)
	}
	fi, err := os.Lstat(filepath.Join(root, "etc", "resolv.conf"))
	if err != nil || !fi.IsDir() {
		t.Errorf("the directory at /etc/resolv.conf was disturbed (err=%v)", err)
	}
}

func TestHasNameserver(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want bool
	}{
		{"plain", "nameserver 1.1.1.1\n", true},
		{"leading whitespace", "   nameserver 1.1.1.1\n", true},
		{"after other directives", "search a.b\noptions ndots:2\nnameserver 1.1.1.1\n", true},
		{"ipv6", "nameserver ::1\n", true},
		{"empty", "", false},
		{"hash comment", "#nameserver 1.1.1.1\n", false},
		{"semicolon comment", ";nameserver 1.1.1.1\n", false},
		{"directive without value", "nameserver\n", false},
		{"search only", "search corp.example\n", false},
		{"substring is not a match", "mynameserver 1.1.1.1\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasNameserver([]byte(tc.body)); got != tc.want {
				t.Errorf("hasNameserver(%q) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
}
