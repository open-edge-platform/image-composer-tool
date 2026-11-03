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

// captureExec swaps in a recording executor that performs mkdir -p, cp, mv -f and
// rm -f for real (so tests can assert on the resulting tree) without needing sudo.
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
		case strings.HasPrefix(cmdStr, "mv -f ") && len(fields) == 4:
			return "", os.Rename(fields[2], fields[3])
		case strings.HasPrefix(cmdStr, "rm -f ") && len(fields) == 3:
			if err := os.Remove(fields[2]); err != nil && !os.IsNotExist(err) {
				return "", err
			}
		}
		return "", nil
	}
}

// failingExec runs commands for real except those whose prefix matches failOn, which
// return an error. It lets each bail-out be exercised independently.
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

func mustRestore(t *testing.T, restore Restore) {
	t.Helper()
	if err := restore(); err != nil {
		t.Fatalf("restore: %v", err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// leftoverTemps lists the hidden temp files a writeInto would leave in dir.
func leftoverTemps(t *testing.T, dir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".ict-resolv-*"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	return matches
}

// TestSeedWhenResolvConfAbsent is the create-mode case: mmdebstrap leaves no
// /etc/resolv.conf, so name resolution fails instantly. The seed must create one,
// and restore must remove it so createResolvConfSymlink still sees an absent file
// and nothing leaks into the shipped image.
func TestSeedWhenResolvConfAbsent(t *testing.T) {
	root := newFixture(t)
	var cmds []string
	captureExec(t, &cmds)

	etcResolv := filepath.Join(root, "etc", "resolv.conf")
	restore := SeedEphemeral(root)

	if got := readFile(t, etcResolv); got != hostContent {
		t.Errorf("seeded content = %q, want %q", got, hostContent)
	}

	mustRestore(t, restore)

	if _, err := os.Lstat(etcResolv); !os.IsNotExist(err) {
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
			if got := readFile(t, target); got != hostContent {
				t.Errorf("seeded content = %q, want %q", got, hostContent)
			}

			mustRestore(t, restore)

			if _, err := os.Lstat(target); !os.IsNotExist(err) {
				t.Errorf("restore left %s behind (err=%v)", target, err)
			}
			// The symlink itself must survive untouched — it is part of the image.
			if got, err := os.Readlink(filepath.Join(root, "etc", "resolv.conf")); err != nil || got != tc.link {
				t.Errorf("the /etc/resolv.conf symlink was disturbed: %q, %v", got, err)
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

			if got := readFile(t, etcResolv); got != hostContent {
				t.Errorf("seeded content = %q, want %q", got, hostContent)
			}

			mustRestore(t, restore)

			if got := readFile(t, etcResolv); got != tc.body {
				t.Errorf("restored content = %q, want original %q", got, tc.body)
			}
			if temps := leftoverTemps(t, filepath.Join(root, "etc")); len(temps) != 0 {
				t.Errorf("temp files left behind: %v", temps)
			}
		})
	}
}

// TestSeedLeavesUsableAndPersistentPathsUntouched is the safety property: a resolver
// that already works, any symlink that would write through into the delivered image,
// and any path that crosses a symlinked directory must be left exactly as found
// with no command executed.
func TestSeedLeavesUsableAndPersistentPathsUntouched(t *testing.T) {
	const working = "nameserver 8.8.8.8\n"
	outside := t.TempDir() // stands in for the build host's own tree

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
		{"symlink into run, target already there", func(t *testing.T, root string) {
			if err := os.MkdirAll(filepath.Join(root, "run"), 0o755); err != nil {
				t.Fatalf("mkdir run: %v", err)
			}
			if err := os.WriteFile(filepath.Join(root, "run", "resolv.conf"), []byte(working), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
			if err := os.Symlink("/run/resolv.conf", filepath.Join(root, "etc", "resolv.conf")); err != nil {
				t.Fatalf("symlink: %v", err)
			}
		}},
		{"etc is a symlink out of the rootfs", func(t *testing.T, root string) {
			if err := os.RemoveAll(filepath.Join(root, "etc")); err != nil {
				t.Fatalf("remove etc: %v", err)
			}
			if err := os.Symlink(outside, filepath.Join(root, "etc")); err != nil {
				t.Fatalf("symlink: %v", err)
			}
		}},
		{"run is a symlink out of the rootfs", func(t *testing.T, root string) {
			if err := os.Symlink(outside, filepath.Join(root, "run")); err != nil {
				t.Fatalf("symlink: %v", err)
			}
			if err := os.Symlink("/run/resolv.conf", filepath.Join(root, "etc", "resolv.conf")); err != nil {
				t.Fatalf("symlink: %v", err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := newFixture(t)
			tc.setup(t, root)

			var cmds []string
			captureExec(t, &cmds)

			mustRestore(t, SeedEphemeral(root))

			if len(cmds) != 0 {
				t.Errorf("expected no commands, got: %v", cmds)
			}
			if entries, _ := os.ReadDir(outside); len(entries) != 0 {
				t.Errorf("wrote outside the rootfs: %v", entries)
			}
			if data, err := os.ReadFile(filepath.Join(root, "etc", "resolv.conf")); err == nil && string(data) != "" {
				if string(data) != working {
					t.Errorf("resolv.conf mutated: got %q, want %q", data, working)
				}
			}
		})
	}
}

// seedShapes are the rootfs shapes SeedEphemeral writes to, each with the path the
// seed lands at.
var seedShapes = []struct {
	name   string
	setup  func(t *testing.T, root string)
	seeded string
}{
	{"absent", func(*testing.T, string) {}, "etc/resolv.conf"},
	{"symlink into run", func(t *testing.T, root string) {
		link := filepath.Join(root, "etc", "resolv.conf")
		if err := os.Symlink("/run/systemd/resolve/stub-resolv.conf", link); err != nil {
			t.Fatalf("symlink: %v", err)
		}
	}, "run/systemd/resolve/stub-resolv.conf"},
	{"regular file without nameserver", func(t *testing.T, root string) {
		if err := os.WriteFile(filepath.Join(root, "etc", "resolv.conf"), []byte("# empty\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}, "etc/resolv.conf"},
}

// TestRestoreKeepsWhatTheCommandsLeft: a configuration command that rewrote or
// replaced the seeded file has set the image's resolver itself; restore must keep
// its result and run no command.
func TestRestoreKeepsWhatTheCommandsLeft(t *testing.T) {
	const commandResolver = "nameserver 192.0.2.53\n"
	for _, shape := range seedShapes {
		for _, change := range []struct {
			name  string
			apply func(t *testing.T, path string)
			check func(t *testing.T, path string)
		}{
			{"rewrote it", func(t *testing.T, path string) {
				if err := os.WriteFile(path, []byte(commandResolver), 0o644); err != nil {
					t.Fatalf("write: %v", err)
				}
			}, func(t *testing.T, path string) {
				if got := readFile(t, path); got != commandResolver {
					t.Errorf("command's resolver = %q, want %q", got, commandResolver)
				}
			}},
			{"replaced it with a symlink", func(t *testing.T, path string) {
				if err := os.Remove(path); err != nil {
					t.Fatalf("remove: %v", err)
				}
				if err := os.Symlink("../run/NetworkManager/resolv.conf", path); err != nil {
					t.Fatalf("symlink: %v", err)
				}
			}, func(t *testing.T, path string) {
				if got, err := os.Readlink(path); err != nil || got != "../run/NetworkManager/resolv.conf" {
					t.Errorf("command's symlink was disturbed: %q, %v", got, err)
				}
			}},
		} {
			t.Run(shape.name+"/"+change.name, func(t *testing.T) {
				root := newFixture(t)
				shape.setup(t, root)
				var cmds []string
				captureExec(t, &cmds)

				restore := SeedEphemeral(root)
				path := filepath.Join(root, shape.seeded)
				change.apply(t, path)
				seedCmds := len(cmds)

				mustRestore(t, restore)

				change.check(t, path)
				if len(cmds) != seedCmds {
					t.Errorf("restore ran commands on a changed seed: %v", cmds[seedCmds:])
				}
			})
		}
	}
}

// TestRestoreNeverWritesThroughASymlink: a command that replaces a seeded resolver
// file with an absolute symlink must not get the restore written through that link
// onto the build host.
func TestRestoreNeverWritesThroughASymlink(t *testing.T) {
	root := newFixture(t)
	etcResolv := filepath.Join(root, "etc", "resolv.conf")
	if err := os.WriteFile(etcResolv, []byte("# empty\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	hostFile := filepath.Join(t.TempDir(), "stub-resolv.conf")
	const hostBytes = "host file, not to be touched\n"
	if err := os.WriteFile(hostFile, []byte(hostBytes), 0o644); err != nil {
		t.Fatalf("write host file: %v", err)
	}
	var cmds []string
	captureExec(t, &cmds)

	restore := SeedEphemeral(root)
	if err := os.Remove(etcResolv); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := os.Symlink(hostFile, etcResolv); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	mustRestore(t, restore)

	if got := readFile(t, hostFile); got != hostBytes {
		t.Errorf("restore wrote through the symlink onto the host: %q", got)
	}
}

// TestSeedSkipsWhenHostResolverUnreadable: with no readable host resolver there is
// nothing to copy, and the caller still gets a usable restore.
func TestSeedSkipsWhenHostResolverUnreadable(t *testing.T) {
	root := newFixture(t)
	hostResolvConf = filepath.Join(t.TempDir(), "does-not-exist")

	var cmds []string
	captureExec(t, &cmds)

	mustRestore(t, SeedEphemeral(root))

	if len(cmds) != 0 {
		t.Errorf("expected no commands when host resolver is unreadable, got: %v", cmds)
	}
}

// TestSeedRemovesStagingFile guards against leaking a staging file on the success
// path.
func TestSeedRemovesStagingFile(t *testing.T) {
	root := newFixture(t)
	var cmds []string
	captureExec(t, &cmds)

	mustRestore(t, SeedEphemeral(root))

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

// TestSeedFailuresLeaveTheRootfsAsFound: whichever command fails while seeding, the
// destination keeps its original state, no temp file is left, and restore has
// nothing to undo. A failed rm is the one exception: seeding succeeds, and it is
// the restore that fails, which must be reported.
func TestSeedFailuresLeaveTheRootfsAsFound(t *testing.T) {
	for _, shape := range seedShapes {
		for _, failOn := range []string{"mkdir", "cp", "mv", "rm"} {
			t.Run(shape.name+"/fail "+failOn, func(t *testing.T) {
				root := newFixture(t)
				shape.setup(t, root)
				path := filepath.Join(root, shape.seeded)
				before, beforeErr := os.ReadFile(path)
				failingExec(t, failOn)

				err := SeedEphemeral(root)()

				removesSeed := shape.name != "regular file without nameserver"
				if failOn == "rm" && removesSeed {
					if err == nil || !strings.Contains(err.Error(), path) {
						t.Fatalf("restore error = %v, want one naming %s", err, path)
					}
					return
				}
				if err != nil {
					t.Fatalf("restore: %v", err)
				}
				after, afterErr := os.ReadFile(path)
				if string(after) != string(before) || os.IsNotExist(afterErr) != os.IsNotExist(beforeErr) {
					t.Errorf("%s changed: %q (%v) -> %q (%v)", path, before, beforeErr, after, afterErr)
				}
				if temps := leftoverTemps(t, filepath.Dir(path)); len(temps) != 0 {
					t.Errorf("temp files left behind: %v", temps)
				}
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

	mustRestore(t, SeedEphemeral(root))

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
