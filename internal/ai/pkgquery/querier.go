package pkgquery

import (
	"context"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
)

var log = logger.Logger()

// Querier answers "does this package exist, and what is it called?"
// against real repository metadata.
type Querier interface {
	Lookup(ctx context.Context, names []string) ([]Result, error)
	Search(ctx context.Context, term string, limit int) ([]Result, error)
}

// State represents the verification outcome state for a package or bundle.
type State string

const (
	StateVerified   State = "verified"
	StateResolved   State = "resolved"
	StateUnverified State = "unverified" // repo unreachable — existence genuinely unknown
	StateNotFound   State = "not_available"
)

// Result describes the verification state and provenance of a single package.
type Result struct {
	Name        string `json:"name"`
	State       State  `json:"state"`
	Repo        string `json:"repo,omitempty"`
	Version     string `json:"version,omitempty"`
	Alternative string `json:"alternative,omitempty"` // bundle ID to suggest when state == StateNotFound
	Description string `json:"description,omitempty"`
}
