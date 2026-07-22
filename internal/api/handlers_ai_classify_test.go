package api

import (
	"errors"
	"fmt"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/ai/provider"
)

// TestIsProviderUnavailable verifies connectivity errors are classified via
// typed-error matching (errors.Is) rather than message-substring heuristics.
func TestIsProviderUnavailable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "wrapped provider unavailable",
			err:  fmt.Errorf("failed to call Ollama API: %w: connection refused", provider.ErrProviderUnavailable),
			want: true,
		},
		{
			name: "doubly wrapped",
			err:  fmt.Errorf("generate: %w", fmt.Errorf("%w: dial tcp", provider.ErrProviderUnavailable)),
			want: true,
		},
		{
			name: "generation failure is not unavailable",
			err:  errors.New("no relevant templates found for query"),
			want: false,
		},
		{
			name: "message mentioning connect is not misclassified",
			err:  errors.New("failed to connect the generated pipeline stage"),
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isProviderUnavailable(tc.err); got != tc.want {
				t.Errorf("isProviderUnavailable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
