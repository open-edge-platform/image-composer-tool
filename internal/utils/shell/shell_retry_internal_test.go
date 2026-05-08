package shell

import (
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
