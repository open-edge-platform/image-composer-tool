package dotfilter

import (
	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
)

// FilterPackagesForDot returns the package slice that should be rendered in the DOT graph.
// When systemOnly is true, only packages reachable from SystemConfig roots are kept.
// Order is preserved based on the input slice.
func FilterPackagesForDot(pkgs []ospackage.PackageInfo, pkgSources map[string]config.PackageSource, systemOnly bool) []ospackage.PackageInfo {
	if !systemOnly {
		return pkgs
	}
	if len(pkgs) == 0 {
		return pkgs
	}

	pkgByName := make(map[string]ospackage.PackageInfo, len(pkgs))
	for _, pkg := range pkgs {
		if pkg.Name == "" {
			continue
		}
		pkgByName[pkg.Name] = pkg
	}

	queue := make([]string, 0, len(pkgSources))
	visited := make(map[string]struct{})

	for name, source := range pkgSources {
		if source != config.PackageSourceSystem {
			continue
		}
		if _, ok := pkgByName[name]; ok {
			queue = append(queue, name)
		}
	}

	if len(queue) == 0 {
		return []ospackage.PackageInfo{}
	}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if _, ok := visited[cur]; ok {
			continue
		}
		visited[cur] = struct{}{}

		pkg, ok := pkgByName[cur]
		if !ok {
			continue
		}
		for _, dep := range pkg.Requires {
			if dep == "" {
				continue
			}
			if _, seen := visited[dep]; seen {
				continue
			}
			if _, present := pkgByName[dep]; present {
				queue = append(queue, dep)
			}
		}
	}

	filtered := make([]ospackage.PackageInfo, 0, len(visited))
	for _, pkg := range pkgs {
		if _, ok := visited[pkg.Name]; ok {
			filtered = append(filtered, pkg)
		}
	}
	return filtered
}
