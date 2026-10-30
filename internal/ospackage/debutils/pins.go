package debutils

import (
	"net/url"
	"path"
	"strings"
	"sync"

	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
)

// Pin is an apt_preferences(5) stanza: packages matching Packages (names or
// globs, or "*") get Priority when they come from Origin (a host) or their
// version matches Version (a glob). Exactly one of Origin and Version is set.
type Pin struct {
	Packages []string
	Origin   string
	Version  string
	Priority int
}

var (
	pinsMu sync.RWMutex
	pins   []Pin
)

// ConfigurePins sets the pins applied when resolving packages, replacing any
// set before. A nil slice clears them.
func ConfigurePins(p []Pin) {
	pinsMu.Lock()
	defer pinsMu.Unlock()
	pins = p
}

// packagePriority returns the apt priority of pkg: that of the first pin that
// applies to it, with pins naming specific packages taking precedence over
// "Package: *" pins as in apt, or else its repository's priority.
func packagePriority(pkg ospackage.PackageInfo) int {
	pinsMu.RLock()
	defer pinsMu.RUnlock()
	for _, specific := range []bool{true, false} {
		for _, p := range pins {
			if p.isGeneral() != specific && p.applies(pkg) {
				return p.Priority
			}
		}
	}
	return getRepositoryPriority(pkg.URL)
}

func (p Pin) isGeneral() bool {
	return len(p.Packages) == 1 && p.Packages[0] == "*"
}

// applies reports whether the pin matches the package's name and its origin
// or version.
func (p Pin) applies(pkg ospackage.PackageInfo) bool {
	named := false
	for _, pattern := range p.Packages {
		if ok, err := path.Match(pattern, pkg.Name); err == nil && ok {
			named = true
			break
		}
	}
	if !named {
		return false
	}
	if p.Origin != "" {
		u, err := url.Parse(pkg.URL)
		return err == nil && strings.EqualFold(u.Hostname(), p.Origin)
	}
	ok, err := path.Match(p.Version, pkg.Version)
	if err == nil && !ok {
		ok, err = path.Match(p.Version, stripEpoch(pkg.Version))
	}
	return err == nil && ok
}
