package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestBuildSIGTERMCleansUpMountsAndLoops is an end-to-end integration test
// that starts a real build, sends SIGTERM mid-stage, and asserts:
//   - the process exits with the conventional 130 signal-cancel exit code;
//   - no bind mounts remain under the build's work directory;
//   - no loop devices remain attached to files under the build's work directory;
//   - no leftover child processes from the build (mmdebstrap, apt, mksquashfs, …).
//
// The test is expensive (requires root, network, sudo passwordless, and pulls
// package indices), so it is gated on ICT_INTEGRATION=1 and on os.Geteuid()==0.
// CI runs that don't opt in with the env var skip cleanly. The plan documents
// this pattern in Phase 6.
func TestBuildSIGTERMCleansUpMountsAndLoops(t *testing.T) {
	if os.Getenv("ICT_INTEGRATION") != "1" {
		t.Skip("integration test; set ICT_INTEGRATION=1 to run")
	}
	if os.Geteuid() != 0 {
		t.Skip("requires root to invoke losetup/mount")
	}
	if _, err := exec.LookPath("mount"); err != nil {
		t.Skipf("mount not available: %v", err)
	}
	if _, err := exec.LookPath("losetup"); err != nil {
		t.Skipf("losetup not available: %v", err)
	}

	// Locate the repo root by walking up from the test file's dir until we
	// find go.mod. The test binary's working dir differs across Go versions,
	// so we compute the path deterministically.
	repoRoot := findRepoRoot(t)

	// Pick a small user template we know builds against Ubuntu 24. The exact
	// template doesn't matter — we only need the build to progress far enough
	// to mount /proc/sys/dev into the chroot before we interrupt it.
	templatePath := filepath.Join(repoRoot, "image-templates", "ubuntu24-x86_64-edge-raw.yml")
	if _, err := os.Stat(templatePath); err != nil {
		t.Skipf("template not present at %s: %v", templatePath, err)
	}

	// Build the CLI binary into a temp dir so we don't pollute the source tree.
	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "image-composer-tool")
	buildCmd := exec.Command("go", "build",
		"-o", binPath, "./cmd/image-composer-tool")
	buildCmd.Dir = repoRoot
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	// Isolated work + cache dirs so we can precisely assert cleanup coverage.
	workDir := filepath.Join(t.TempDir(), "workspace")
	cacheDir := filepath.Join(t.TempDir(), "cache")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workDir: %v", err)
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cacheDir: %v", err)
	}

	build := exec.Command(binPath, "build",
		"--work-dir", workDir,
		"--cache-dir", cacheDir,
		templatePath)
	build.Dir = repoRoot
	// New process group so we can send SIGTERM to just the build (and its
	// descendants), independent of the test binary's group.
	build.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := build.Start(); err != nil {
		t.Fatalf("start build: %v", err)
	}
	buildPid := build.Process.Pid

	// Wait long enough for PreProcess to bootstrap the chroot and mount sysfs
	// — this is the point where cleanup work actually needs to happen. On a
	// warm cache the shortest observed time to first mount is ~5-10 s; give
	// it more headroom to be robust across CI hardware.
	select {
	case <-time.After(30 * time.Second):
	case err := <-waitAsync(build):
		t.Fatalf("build finished before we could interrupt it: %v", err)
	}

	// Signal the build's process group. This targets the CLI process (which
	// runs as its own group leader via SysProcAttr.Setpgid at build.Start).
	// The CLI's ctx cancels; each in-flight exec.CommandContext inside the
	// build then invokes its own Cancel closure, which does
	// kill(-<subprocess-pgid>, SIGTERM) per shell.applyExecAttrs — so
	// mmdebstrap/apt/mksquashfs and their descendants get their signals via
	// their own pgids, not by pgid inheritance from the CLI (each was
	// spawned with its own Setpgid=true).
	if err := syscall.Kill(-buildPid, syscall.SIGTERM); err != nil {
		t.Fatalf("kill -TERM %d: %v", -buildPid, err)
	}

	// Wait for the build to exit. Cap total elapsed at 3 minutes: 2 min budget
	// for PostProcess-under-detached-ctx + 30 s of per-entry coordinator work
	// on the worst-case umount escalation + slack.
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer waitCancel()
	exitErr := waitBounded(waitCtx, build)

	// Expect exit code 130.
	if exitErr == nil {
		t.Fatalf("build exited zero after SIGTERM; expected non-zero (130)")
	}
	var xe *exec.ExitError
	if !errors.As(exitErr, &xe) {
		t.Fatalf("build exit did not surface ExitError: %v", exitErr)
	}
	if got, want := xe.ExitCode(), 130; got != want {
		t.Fatalf("exit code = %d, want %d (SIGTERM/SIGINT conventional cancel)", got, want)
	}

	// Assert no leftover mounts under workDir.
	if lines := mountLinesUnder(t, workDir); len(lines) != 0 {
		t.Errorf("expected no mounts under %s after cancel, got %d:\n  %s",
			workDir, len(lines), strings.Join(lines, "\n  "))
	}

	// Assert no leftover loop devices backed by files under workDir.
	if lines := loopDevicesBackedBy(t, workDir); len(lines) != 0 {
		t.Errorf("expected no loop devices backed by files under %s, got %d:\n  %s",
			workDir, len(lines), strings.Join(lines, "\n  "))
	}

	// Assert no orphaned package-management child processes remain. The list
	// is scoped to well-known ICT-spawned commands rather than a pgid check
	// because each subprocess had its own pgid via shell.applyExecAttrs
	// (see the signaling comment above) — so a "descendants of buildPid"
	// walk would miss them entirely. pgrep-by-name is host-wide, which
	// means the test can false-positive if an unrelated mmdebstrap/dpkg is
	// running on the same host; document that constraint so operators
	// running this manually know to expect a clean host beforehand.
	for _, name := range []string{"mmdebstrap", "apt-get", "dpkg", "mksquashfs"} {
		out, err := exec.Command("pgrep", "-f", name).Output()
		// pgrep exits 1 when nothing matches — that's the success case.
		var ee *exec.ExitError
		if err != nil && !(errors.As(err, &ee) && ee.ExitCode() == 1) {
			t.Fatalf("pgrep -f %s: %v (output: %s)", name, err, out)
		}
		if trimmed := strings.TrimSpace(string(out)); trimmed != "" {
			t.Errorf("orphaned %s process(es) after cancel:\n%s", name, trimmed)
		}
	}
}

// findRepoRoot walks up from the test file's directory until it finds a go.mod.
// Used so subprocess invocations of `go build` and template paths resolve to
// the repo root regardless of test working directory.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find go.mod above %s", dir)
		}
		dir = parent
	}
}

// waitAsync runs cmd.Wait on a goroutine so callers can select over it. Sends
// the wait error (nil on success) exactly once. Test-only: no leak concerns
// because the receiving side always drains before the test returns.
func waitAsync(cmd *exec.Cmd) <-chan error {
	ch := make(chan error, 1)
	go func() { ch <- cmd.Wait() }()
	return ch
}

// waitBounded waits for cmd to exit under ctx; if the deadline fires first it
// kills the process group and returns ctx.Err. Used so a hung post-cancel
// build can't wedge the test binary indefinitely.
func waitBounded(ctx context.Context, cmd *exec.Cmd) error {
	done := waitAsync(cmd)
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		<-done
		return fmt.Errorf("build did not exit within %s: %w",
			deadlineFrom(ctx), ctx.Err())
	}
}

func deadlineFrom(ctx context.Context) time.Duration {
	if d, ok := ctx.Deadline(); ok {
		return time.Until(d)
	}
	return 0
}

// mountLinesUnder returns lines from /proc/mounts whose mount point starts
// with workDir. Uses /proc/mounts directly (rather than `mount` output) so
// the result is trivially parseable and we don't spawn another subprocess
// that might race with the just-cancelled build.
func mountLinesUnder(t *testing.T, workDir string) []string {
	t.Helper()
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		t.Fatalf("read /proc/mounts: %v", err)
	}
	var matches []string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		mountPoint := fields[1]
		if strings.HasPrefix(mountPoint, workDir+"/") || mountPoint == workDir {
			matches = append(matches, line)
		}
	}
	return matches
}

// loopDevicesBackedBy returns lines from `losetup -l` whose BACK-FILE column
// is a path under workDir. Runs the real losetup so partial matches on our
// own private path don't accidentally include unrelated loop devices.
func loopDevicesBackedBy(t *testing.T, workDir string) []string {
	t.Helper()
	out, err := exec.Command("losetup", "-l", "--noheadings",
		"--output", "NAME,BACK-FILE").Output()
	if err != nil {
		t.Fatalf("losetup -l: %v", err)
	}
	var matches []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		backFile := fields[len(fields)-1]
		if strings.HasPrefix(backFile, workDir+"/") {
			matches = append(matches, line)
		}
	}
	return matches
}
