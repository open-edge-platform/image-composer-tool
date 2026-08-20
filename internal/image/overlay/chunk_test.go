package overlay

import (
	"fmt"
	"strings"
	"testing"
)

// Commands run as `bash -c "<cmd>"`, so the whole command is one argv entry and
// the binding kernel limit is MAX_ARG_STRLEN (128 KiB), not ARG_MAX. A large
// overlay (2004 ROS 2 artifacts produced a ~142 KiB command) must therefore be
// split, or execve fails with "argument list too long" before dpkg starts.
func TestChunkArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		args       []string
		budget     int
		wantChunks int
		// wantAllPresent asserts every input argument survives the split exactly
		// once and in order — a chunker that drops one would silently skip a package.
		wantAllPresent bool
	}{
		{
			name:           "everything fits in one batch",
			args:           []string{"/a.deb", "/b.deb", "/c.deb"},
			budget:         1024,
			wantChunks:     1,
			wantAllPresent: true,
		},
		{
			// "/aa.deb /bb.deb" is 15 bytes; a 10-byte budget forces a split.
			name:           "splits when the joined length would exceed the budget",
			args:           []string{"/aa.deb", "/bb.deb"},
			budget:         10,
			wantChunks:     2,
			wantAllPresent: true,
		},
		{
			// A single argument larger than the budget still has to be attempted:
			// dropping it would skip a package, and the caller must see the real
			// execve error rather than a silent omission.
			name:           "an oversized single argument gets its own batch",
			args:           []string{"/short.deb", "/this-one-is-far-too-long-for-the-budget.deb"},
			budget:         12,
			wantChunks:     2,
			wantAllPresent: true,
		},
		{
			// Callers must still invoke once for the no-artifact case, matching the
			// behavior before batching existed.
			name:       "empty input yields a single empty batch",
			args:       nil,
			budget:     1024,
			wantChunks: 1,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			chunks := chunkArgs(tt.args, tt.budget)
			if len(chunks) != tt.wantChunks {
				t.Fatalf("got %d chunk(s) %v, want %d", len(chunks), chunks, tt.wantChunks)
			}
			if !tt.wantAllPresent {
				return
			}
			var flat []string
			for _, c := range chunks {
				flat = append(flat, c...)
			}
			if len(flat) != len(tt.args) {
				t.Fatalf("got %d arg(s) after chunking, want %d", len(flat), len(tt.args))
			}
			for i, want := range tt.args {
				if flat[i] != want {
					t.Errorf("arg[%d] = %q, want %q (order must be preserved for dpkg)", i, flat[i], want)
				}
			}
		})
	}
}

// Each produced batch must actually fit the budget — that is the whole point.
func TestChunkArgs_EveryBatchRespectsBudget(t *testing.T) {
	t.Parallel()

	// Approximate the real failure: ~2000 quoted artifact paths of realistic length.
	args := make([]string, 0, 2000)
	for i := 0; i < 2000; i++ {
		args = append(args, fmt.Sprintf("'/run/overlay-pkgs/ros-jazzy-some-package-name_%04d_amd64.deb'", i))
	}

	const budget = maxDpkgArgBytes
	chunks := chunkArgs(args, budget)
	if len(chunks) < 2 {
		t.Fatalf("expected the 2000-artifact list to need multiple batches, got %d", len(chunks))
	}
	for i, c := range chunks {
		if joined := len(strings.Join(c, " ")); joined > budget {
			t.Errorf("batch %d is %d bytes, over the %d-byte budget", i, joined, budget)
		}
	}

	// And nothing was lost in the split.
	total := 0
	for _, c := range chunks {
		total += len(c)
	}
	if total != len(args) {
		t.Errorf("chunking produced %d arg(s), want %d", total, len(args))
	}
}
