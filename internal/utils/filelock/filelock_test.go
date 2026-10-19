package filelock

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestLockExcludesUntilReleased(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".lock")
	unlock, err := Lock(context.Background(), path)
	if err != nil {
		t.Fatalf("Lock() error = %v", err)
	}

	acquired := make(chan func(), 1)
	go func() {
		second, err := Lock(context.Background(), path)
		if err != nil {
			t.Errorf("second Lock() error = %v", err)
			acquired <- func() {}
			return
		}
		acquired <- second
	}()

	select {
	case <-acquired:
		t.Fatal("a second Lock() returned while the lock was held")
	case <-time.After(3 * pollInterval):
	}
	unlock()
	select {
	case second := <-acquired:
		second()
	case <-time.After(5 * time.Second):
		t.Fatal("Lock() did not return after the lock was released")
	}
}

func TestLockReturnsWhenContextIsCancelledWhileWaiting(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".lock")
	unlock, err := Lock(context.Background(), path)
	if err != nil {
		t.Fatalf("Lock() error = %v", err)
	}
	defer unlock()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := Lock(ctx, path)
		done <- err
	}()
	time.Sleep(2 * pollInterval)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Lock() error = %v, want one wrapping context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Lock() did not return after its context was cancelled")
	}
}

func TestLockWithCancelledContextDoesNotLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".lock")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Lock(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatalf("Lock() error = %v, want one wrapping context.Canceled", err)
	}

	unlock, err := Lock(context.Background(), path)
	if err != nil {
		t.Fatalf("Lock() after a cancelled attempt error = %v", err)
	}
	unlock()
}
