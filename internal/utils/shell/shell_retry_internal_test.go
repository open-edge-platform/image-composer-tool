package shell

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExecCmdWithStream_RetriesAptLock(t *testing.T) {
	tempDir := t.TempDir()
	stateFile := filepath.Join(tempDir, "apt-state")
	fakeAptPath := filepath.Join(tempDir, "apt")
	script := fmt.Sprintf(`#!/bin/sh
if [ ! -f %q ]; then
  touch %q
  echo "E: Could not get lock /var/lib/apt/lists/lock. It is held by process 358 (apt)" 1>&2
  echo "E: Unable to lock directory /var/lib/apt/lists/" 1>&2
  exit 100
fi
echo "updated"
`, stateFile, stateFile)
	if err := os.WriteFile(fakeAptPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write fake apt script: %v", err)
	}

	originalAptPaths := commandMap["apt"]
	originalAttempts := aptLockRetryAttempts
	originalDelay := aptLockRetryDelay
	defer func() {
		commandMap["apt"] = originalAptPaths
		aptLockRetryAttempts = originalAttempts
		aptLockRetryDelay = originalDelay
	}()

	commandMap["apt"] = []string{fakeAptPath}
	aptLockRetryAttempts = 2
	aptLockRetryDelay = 10 * time.Millisecond

	output, err := (&DefaultExecutor{}).ExecCmdWithStream("apt update", false, HostPath, nil)
	if err != nil {
		t.Fatalf("expected apt lock retry to succeed, got: %v", err)
	}
	if output != "updated\n" {
		t.Fatalf("expected output to contain final successful apt output, got %q", output)
	}
	if _, err := os.Stat(stateFile); err != nil {
		t.Fatalf("expected state file to exist after first failed attempt, got: %v", err)
	}
}

// TestExecCmd_AptLockRetryCancels asserts that a cancelled ambient context
// interrupts the apt-lock retry backoff — the loop must not sleep for the
// full 20*3s worst case. It exercises execCmdWithRetry via ExecCmd rather
// than the streaming variant; sleepCtx behavior is shared.
func TestExecCmd_AptLockRetryCancels(t *testing.T) {
	tempDir := t.TempDir()
	fakeAptPath := filepath.Join(tempDir, "apt")
	// Always fails with the apt-lock marker so the retry loop keeps spinning.
	script := `#!/bin/sh
echo "E: Could not get lock /var/lib/apt/lists/lock" 1>&2
exit 100
`
	if err := os.WriteFile(fakeAptPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write fake apt script: %v", err)
	}

	originalAptPaths := commandMap["apt"]
	originalAttempts := aptLockRetryAttempts
	originalDelay := aptLockRetryDelay
	defer func() {
		commandMap["apt"] = originalAptPaths
		aptLockRetryAttempts = originalAttempts
		aptLockRetryDelay = originalDelay
	}()

	commandMap["apt"] = []string{fakeAptPath}
	aptLockRetryAttempts = 20
	// Long enough that a naive time.Sleep would visibly dominate the test time.
	aptLockRetryDelay = 5 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	restore := SetContext(ctx)
	defer restore()

	// Cancel while the first backoff sleep is in flight (well inside the 5s window).
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := (&DefaultExecutor{}).ExecCmd("apt update", false, HostPath, nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected error from cancelled apt retry, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected wrapped context.Canceled, got: %v", err)
	}
	// Generous ceiling: the first attempt runs synchronously (fast: fake apt exits
	// immediately), then the backoff sleep is preempted by cancel within ~200ms;
	// worst realistic elapsed under load is well under 2s.
	if elapsed > 2*time.Second {
		t.Fatalf("expected retry loop to abort within 2s after cancel, took %s", elapsed)
	}
}
