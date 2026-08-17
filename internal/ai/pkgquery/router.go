package pkgquery

import (
	"fmt"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/ospackage/debutils"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage/rpmutils"
)

// NewQuerier returns the appropriate Querier adapter for the given OS.
// os must be one of: "ubuntu", "debian", "elxr" (→ DebAdapter)
//
//	"azure-linux", "emt" (→ RpmAdapter).
func NewQuerier(os string, debRepos []debutils.RepoConfig, rpmRepos []rpmutils.RepoConfig) (Querier, error) {
	osNormalized := strings.ToLower(strings.TrimSpace(os))

	switch {
	case strings.Contains(osNormalized, "ubuntu"),
		strings.Contains(osNormalized, "debian"),
		strings.Contains(osNormalized, "elxr"):
		adapter, err := NewDebAdapter(debRepos)
		if err != nil {
			return nil, fmt.Errorf("pkgquery: failed to create deb adapter for %s: %w", os, err)
		}
		return adapter, nil

	case strings.Contains(osNormalized, "azure"),
		strings.Contains(osNormalized, "azl"),
		strings.Contains(osNormalized, "emt"):
		adapter, err := NewRpmAdapter(rpmRepos)
		if err != nil {
			return nil, fmt.Errorf("pkgquery: failed to create rpm adapter for %s: %w", os, err)
		}
		return adapter, nil

	default:
		return nil, fmt.Errorf("pkgquery: unsupported OS %q", os)
	}
}
