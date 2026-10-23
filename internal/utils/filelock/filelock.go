// Package filelock takes advisory locks on files shared between builds, giving
// up when the build's context is cancelled.
package filelock

import (
	"context"
	"fmt"
	"os"
	"syscall"
	"time"
)

// pollInterval is how long Lock waits between attempts while another process
// holds the lock.
const pollInterval = 50 * time.Millisecond

// Lock takes an exclusive advisory lock on the file at path, creating it if
// needed, and returns the function that releases it. A blocking flock cannot be
// interrupted by ctx, so Lock tries without blocking and retries until the lock
// is free or ctx is done; in the latter case the returned error wraps ctx.Err().
func Lock(ctx context.Context, path string) (unlock func(), err error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	lockFile, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("open lock file %s: %w", path, err)
	}

	timer := time.NewTimer(pollInterval)
	defer timer.Stop()
	for {
		err = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EINTR {
			_ = lockFile.Close()
			return nil, fmt.Errorf("lock %s: %w", path, err)
		}
		timer.Reset(pollInterval)
		select {
		case <-ctx.Done():
			_ = lockFile.Close()
			return nil, fmt.Errorf("lock %s: %w", path, ctx.Err())
		case <-timer.C:
		}
	}
	return func() {
		_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		_ = lockFile.Close()
	}, nil
}
