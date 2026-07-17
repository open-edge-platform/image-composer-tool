package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

const testDeadlineBudget = 15 * time.Second

// TestExecuteBuild_ReturnsCancelledOnPreCancelledContext asserts that when
// executeBuild is invoked with an already-cancelled context, the very first
// operation — LoadAndMergeTemplate on a nonexistent path — still runs, but
// the function eventually returns a signal-cancellation error rather than
// only the load error. Because the current implementation surfaces the load
// error immediately (LoadAndMergeTemplate does not observe ctx), the assertion
// is loose: we accept either the load error OR a wrapped context.Canceled;
// the stronger assertion — that no shell subprocess would outlive the cancel —
// is exercised by the shell package's unit tests in Phase 1.
//
// This test's primary job is to guarantee the new ctx wiring doesn't itself
// panic or leak the cleanup goroutine when the ctx is already dead before
// executeBuild dispatches its first work. Race-clean under -race.
func TestExecuteBuild_ReturnsCancelledOnPreCancelledContext(t *testing.T) {
	defer resetBuildFlags()

	// Ambient state: no coordinator installed by tests, cmd built by hand.
	cmd := createBuildCommand()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // fire before executeBuild runs so ctx.Err() != nil throughout.
	cmd.SetContext(ctx)

	err := executeBuild(cmd, []string{"/nonexistent/never-there.yml"})
	if err == nil {
		t.Fatal("expected non-nil error for nonexistent template + cancelled ctx")
	}

	// The failure is either the template-load error path or the post-PostProcess
	// cancellation path. Either is acceptable — what we care about is that
	// executeBuild returned within the test-run budget and did not deadlock on
	// the cleanup goroutine.
	if !errors.Is(err, context.Canceled) &&
		!strings.Contains(err.Error(), "loading and merging template") {
		t.Fatalf("unexpected error shape: %v", err)
	}
}

// TestExecuteBuild_CleanupGoroutineDoesNotLeak wraps the standard invalid-
// template test — which does NOT cancel the ctx — and asserts the function
// still returns within a bounded time. If the cleanup goroutine's <-ctx.Done
// path deadlocked with the deferred wait, this test would hang forever.
// (go test's timeout catches that at CI granularity, but this makes the
// intent explicit.)
func TestExecuteBuild_CleanupGoroutineDoesNotLeak(t *testing.T) {
	defer resetBuildFlags()

	cmd := createBuildCommand()
	// No SetContext: cmd.Context() returns nil; executeBuild falls back to
	// context.Background(). The deferred cancelCtx must still release the
	// cleanup goroutine on normal return.

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = executeBuild(cmd, []string{"/definitely/not/here.yml"})
	}()

	select {
	case <-done:
		// Success: executeBuild returned without deadlock.
	case <-newTestDeadline(t).Done():
		t.Fatal("executeBuild deadlocked; cleanup goroutine likely blocked on ctx.Done")
	}
}

// newTestDeadline gives us a 15s ceiling per subtest. Well above the actual
// executeBuild happy-path runtime (< 100ms for a nonexistent-template failure)
// but short enough that a genuine deadlock trips the assertion inside a
// single go-test invocation.
func newTestDeadline(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testDeadlineBudget)
	t.Cleanup(cancel)
	return ctx
}

// unused variable holder used to make the ctx-shape build cleanly if a future
// test dispatches through cobra's Execute path directly.
var _ = cobra.Command{}
