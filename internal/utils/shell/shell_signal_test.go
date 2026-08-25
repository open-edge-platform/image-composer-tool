package shell

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/runctx"
)

// waitProcessGone polls kill(pid, 0) until the process is gone or the deadline
// fires. Returns true if the process is no longer alive when we stop looking.
// Uses a small poll interval — enough to catch the ~5s WaitDelay + Cancel
// SIGTERM path without dominating test runtime.
func waitProcessGone(pid int, deadline time.Time) bool {
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			if errors.Is(err, syscall.ESRCH) {
				return true
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// installFakeSleep drops a wrapper into t.TempDir that resolves to the real
// /bin/sleep and points the commandMap "sleep" entry at it. Restoring is
// deferred by the caller. Returns the pidfile path where the wrapper writes
// its own pid so the test can observe the child.
func installFakeSleep(t *testing.T) (pidFile string, restore func()) {
	t.Helper()
	tempDir := t.TempDir()
	pidFile = filepath.Join(tempDir, "sleep.pid")
	// Wrapper writes its pid then execs the real sleep — after exec, the pid
	// is the same, so the test can kill by that pid and observe the group.
	wrapper := filepath.Join(tempDir, "sleep")
	script := "#!/bin/sh\necho $$ > " + pidFile + "\nexec /bin/sleep \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write fake sleep wrapper: %v", err)
	}
	original := commandMap["sleep"]
	commandMap["sleep"] = []string{wrapper}
	restore = func() { commandMap["sleep"] = original }
	return pidFile, restore
}

func readPid(t *testing.T, pidFile string, deadline time.Time) int {
	t.Helper()
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(pidFile); err == nil && len(data) > 0 {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for pid file %s", pidFile)
	return 0
}

// runExecInBackground fires an executor call on a goroutine so the test can
// cancel the ambient context mid-flight. Errors are surfaced via the returned
// channel.
func runExecInBackground(fn func() (string, error)) (<-chan error, *sync.WaitGroup) {
	errCh := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := fn()
		errCh <- err
	}()
	return errCh, &wg
}

// TestExecCmd_ContextCancelKillsChild asserts that cancelling the ambient
// context sent to shell.SetContext terminates the spawned child. Covers the
// retry-wrapped path (ExecCmd -> execCmdOnce).
func TestExecCmd_ContextCancelKillsChild(t *testing.T) {
	pidFile, restore := installFakeSleep(t)
	defer restore()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	restoreCtx := SetContext(ctx)
	defer restoreCtx()

	errCh, wg := runExecInBackground(func() (string, error) {
		return (&DefaultExecutor{}).ExecCmd("sleep 60", false, HostPath, nil)
	})

	pid := readPid(t, pidFile, time.Now().Add(3*time.Second))
	cancel()

	// The child should die well under 5s (Cancel sends SIGTERM to -pgid; sleep
	// exits immediately on TERM). WaitDelay of 5s is the SIGKILL escalation
	// ceiling — we allow 8s total to accommodate CI jitter without covering
	// for a hung path.
	if !waitProcessGone(pid, time.Now().Add(8*time.Second)) {
		t.Fatalf("expected pid %d to be gone within 8s of cancel; still alive", pid)
	}

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatalf("expected non-nil error from cancelled ExecCmd")
		}
	case <-time.After(8 * time.Second):
		t.Fatalf("ExecCmd did not return within 8s of cancel")
	}
	wg.Wait()
}

// TestExecCmdWithStream_ContextCancelKillsChild covers the streaming path,
// which spawns three additional goroutines around cmd.StdoutPipe/StderrPipe.
// Those goroutines must exit when the child dies so cmd.Wait returns.
func TestExecCmdWithStream_ContextCancelKillsChild(t *testing.T) {
	pidFile, restore := installFakeSleep(t)
	defer restore()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	restoreCtx := SetContext(ctx)
	defer restoreCtx()

	errCh, wg := runExecInBackground(func() (string, error) {
		return (&DefaultExecutor{}).ExecCmdWithStream("sleep 60", false, HostPath, nil)
	})

	pid := readPid(t, pidFile, time.Now().Add(3*time.Second))
	cancel()

	if !waitProcessGone(pid, time.Now().Add(8*time.Second)) {
		t.Fatalf("expected pid %d to be gone within 8s of cancel; still alive", pid)
	}

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatalf("expected non-nil error from cancelled ExecCmdWithStream")
		}
	case <-time.After(8 * time.Second):
		t.Fatalf("ExecCmdWithStream did not return within 8s of cancel")
	}
	wg.Wait()
}

// TestExecCmdSilent_ContextCancelKillsChild covers the non-retry non-streaming
// path. Same cancellation contract as the other two.
func TestExecCmdSilent_ContextCancelKillsChild(t *testing.T) {
	pidFile, restore := installFakeSleep(t)
	defer restore()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	restoreCtx := SetContext(ctx)
	defer restoreCtx()

	errCh, wg := runExecInBackground(func() (string, error) {
		return (&DefaultExecutor{}).ExecCmdSilent("sleep 60", false, HostPath, nil)
	})

	pid := readPid(t, pidFile, time.Now().Add(3*time.Second))
	cancel()

	if !waitProcessGone(pid, time.Now().Add(8*time.Second)) {
		t.Fatalf("expected pid %d to be gone within 8s of cancel; still alive", pid)
	}

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatalf("expected non-nil error from cancelled ExecCmdSilent")
		}
	case <-time.After(8 * time.Second):
		t.Fatalf("ExecCmdSilent did not return within 8s of cancel")
	}
	wg.Wait()
}

// TestSetContext_RestoresPrevious asserts SetContext's restore closure returns
// the ambient context to its previous value, which is what executeBuild relies
// on when wrapping PostProcess with a detached ctx.
func TestSetContext_RestoresPrevious(t *testing.T) {
	if got := ctxOrBackground(); got != context.Background() {
		// Not literally equal — Background is a singleton, so this comparison is
		// safe. If a prior test in this package leaked a bound ctx, we'd catch it here.
		t.Fatalf("expected clean starting ctx (Background), got %T", got)
	}

	ctx1 := context.WithValue(context.Background(), struct{ k string }{"k"}, "one")
	restore1 := SetContext(ctx1)
	if got := ctxOrBackground(); got != ctx1 {
		t.Fatalf("expected ctx1 bound after first SetContext")
	}

	ctx2 := context.WithValue(context.Background(), struct{ k string }{"k"}, "two")
	restore2 := SetContext(ctx2)
	if got := ctxOrBackground(); got != ctx2 {
		t.Fatalf("expected ctx2 bound after nested SetContext")
	}

	restore2()
	if got := ctxOrBackground(); got != ctx1 {
		t.Fatalf("expected ctx1 restored after inner restore")
	}

	restore1()
	if got := ctxOrBackground(); got != context.Background() {
		t.Fatalf("expected Background restored after outer restore")
	}
}

// TestApplyExecAttrs_SetsPgidAndCancel verifies the spawned command carries
// Setpgid, a non-nil Cancel closure, and a positive WaitDelay so a future
// refactor of applyExecAttrs (e.g. splitting it per call site) can't silently
// drop one of the three.
func TestApplyExecAttrs_SetsPgidAndCancel(t *testing.T) {
	// Reach for an *exec.Cmd via ExecCmd's happy path by intercepting a fake
	// "true" binary. But applyExecAttrs is package-internal; assert its effect
	// on a directly-constructed cmd instead — the property under test is the
	// helper's contract, not a full end-to-end path.
	//
	// We inspect the *exec.Cmd fields with reflection-free direct access.
	cmd := exec.CommandContext(context.Background(), "/bin/true")
	applyExecAttrs(cmd)

	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatalf("expected SysProcAttr.Setpgid=true")
	}
	if cmd.Cancel == nil {
		t.Fatalf("expected non-nil Cancel closure")
	}
	if cmd.WaitDelay <= 0 {
		t.Fatalf("expected positive WaitDelay, got %s", cmd.WaitDelay)
	}
	// Cancel closure should be safe to call on a not-yet-started cmd (Process nil).
	if err := cmd.Cancel(); err != nil {
		t.Fatalf("Cancel on unstarted cmd should return nil, got: %v", err)
	}
}

// TestCleanupCallback_ReceivesFreshCtxNotCancelledParent regresses on the
// defect Copilot flagged: a coordinator-registered cleanup callback that
// ignored the per-entry ctx and used the ambient parent-bound (already
// cancelled) ctx for its shell/HTTP work. The Fix A pattern wraps each
// callback's body with shell.SetContext(ctx) + runctx.SetContext(ctx). This
// test verifies the wrapper's contract:
//
//  1. Register a callback with a fresh Coordinator.
//  2. Bind an ALREADY-CANCELLED ctx as the ambient shell ctx (simulating the
//     signal-fired state build.go's cleanup goroutine sees).
//  3. Call coord.Run(context.Background()) — the coordinator hands the
//     callback a fresh per-entry timeout ctx.
//  4. Inside the callback, apply the Fix A wrapper and inspect the ambient
//     shell.ctxOrBackground() — it must be the fresh entry ctx, NOT the
//     cancelled parent, and it must not be cancelled.
func TestCleanupCallback_ReceivesFreshCtxNotCancelledParent(t *testing.T) {
	// Simulate the "signal fired" state: bind a cancelled ctx as ambient.
	cancelledCtx, cancelParent := context.WithCancel(context.Background())
	cancelParent()
	restoreParent := SetContext(cancelledCtx)
	defer restoreParent()

	// Sanity: without the Fix A wrapper, a callback would observe this
	// cancelled ctx via ctxOrBackground().
	if ctxOrBackground().Err() == nil {
		t.Fatalf("test setup: expected ambient ctx to be cancelled before Register")
	}

	// Coordinator with one callback that captures what it sees.
	coord := runctx.New()
	var observedShellErr error
	var observedShellCtxSameAsEntry bool
	coord.Register("test", func(entryCtx context.Context) error {
		// Fix A pattern — the exact shape used in loopdev.go and chrootenv.go.
		restoreShell := SetContext(entryCtx)
		defer restoreShell()
		restoreRun := runctx.SetContext(entryCtx)
		defer restoreRun()

		// Read back the ambient shell ctx from inside the wrapper. This is
		// what LoopSetupDelete/CleanupChrootEnv's shell.ExecCmd calls see.
		got := ctxOrBackground()
		observedShellErr = got.Err()
		observedShellCtxSameAsEntry = got == entryCtx
		return nil
	})

	// Fire the coordinator with a context.Background() ctx — the caller
	// context is irrelevant per Run's contract; each entry gets a fresh
	// per-entry timeout ctx.
	if residual := coord.Run(context.Background()); len(residual) != 0 {
		t.Fatalf("expected clean run, got residual: %v", residual)
	}

	// Assertions:
	// 1. The shell ctx inside the callback must not have been the cancelled parent.
	if observedShellErr != nil {
		t.Fatalf("Fix A regression: callback observed shell ctx with Err %v — should be nil (fresh per-entry ctx)", observedShellErr)
	}
	// 2. It must have been exactly the entry ctx the coordinator handed in.
	if !observedShellCtxSameAsEntry {
		t.Fatalf("Fix A regression: callback's shell ctx was not the per-entry ctx — SetContext binding did not take")
	}
	// 3. After the callback returns, the ambient shell ctx must be back to
	//    the cancelled parent (restore fired). This guards against a defer
	//    order bug in the wrapper.
	if ctxOrBackground().Err() == nil {
		t.Fatalf("Fix A regression: ambient shell ctx not restored to cancelled parent after callback")
	}
	if ctxOrBackground() != cancelledCtx {
		t.Fatalf("Fix A regression: ambient shell ctx after callback = %v, want cancelledCtx", ctxOrBackground())
	}
}
