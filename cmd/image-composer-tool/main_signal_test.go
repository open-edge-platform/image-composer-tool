package main

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// TestExitCodeForError covers the ExecuteContext-return-to-exit-code mapping
// used by main(). Signal-triggered cancellation must produce the conventional
// 130 exit so scripting can distinguish "user aborted" from other failures.
func TestExitCodeForError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"context.Canceled bare", context.Canceled, signalExitCode},
		{"context.Canceled wrapped", fmt.Errorf("build cancelled: %w", context.Canceled), signalExitCode},
		{"context.Canceled double-wrapped", fmt.Errorf(
			"post-processing failed: %w",
			fmt.Errorf("build cancelled: %w", context.Canceled),
		), signalExitCode},
		// DeadlineExceeded is NOT user-initiated cancel — it comes from an
		// internal timeout such as PostProcess's detached cleanup budget.
		// Callers scripting around the tool need this distinct from a signal,
		// so it maps to the generic-failure exit (1), not signalExitCode.
		{"context.DeadlineExceeded bare", context.DeadlineExceeded, 1},
		{"context.DeadlineExceeded wrapped", fmt.Errorf("timeout: %w", context.DeadlineExceeded), 1},
		{"generic error", errors.New("something else went wrong"), 1},
		{"wrapped generic error", fmt.Errorf("build: %w", errors.New("mmdebstrap failed")), 1},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := exitCodeForError(tt.err); got != tt.want {
				t.Fatalf("exitCodeForError(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

// TestSignalExitCodeConstant guards the well-known Unix convention that
// SIGINT-triggered exits use 130 (128 + 2). If a future refactor accidentally
// changes this constant, scripts around the tool would silently mis-classify
// cancellations as generic failures.
func TestSignalExitCodeConstant(t *testing.T) {
	if signalExitCode != 130 {
		t.Fatalf("signalExitCode = %d, want 130 (conventional 128 + SIGINT=2)", signalExitCode)
	}
}
