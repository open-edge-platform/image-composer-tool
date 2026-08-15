package pkgquery

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage/rpmutils"
)

type rpmMetadataParserFunc func(baseURL, gzHref string, packageFilter []string) ([]ospackage.PackageInfo, error)

// RpmAdapter implements Querier for RPM-based distributions like Azure Linux and EMT.
type RpmAdapter struct {
	mu                sync.Mutex
	repoCfgs          []rpmutils.RepoConfig
	parseMetadataFunc rpmMetadataParserFunc
}

// NewRpmAdapter constructs a new RpmAdapter with the given repository configurations.
func NewRpmAdapter(repoCfgs []rpmutils.RepoConfig) (*RpmAdapter, error) {
	if len(repoCfgs) == 0 {
		return nil, fmt.Errorf("pkgquery: rpm adapter requires at least one repository configuration")
	}
	return &RpmAdapter{
		repoCfgs:          repoCfgs,
		parseMetadataFunc: rpmutils.ParseRepositoryMetadata,
	}, nil
}

// Lookup queries the RPM repositories for the specified package names.
func (a *RpmAdapter) Lookup(ctx context.Context, names []string) ([]Result, error) {
	results := make([]Result, len(names))
	repoPackages := make(map[string][]ospackage.PackageInfo)
	repoErrors := make(map[string]error)

	// Fetch metadata for each repository
	for _, repoCfg := range a.repoCfgs {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("pkgquery: context cancelled during rpm metadata lookup: %w", ctx.Err())
		default:
		}

		a.mu.Lock()
		pkgs, err := a.parseMetadataFunc(repoCfg.URL, "repodata/primary.xml.gz", nil)
		a.mu.Unlock()

		if err != nil {
			log.Warnf("pkgquery: failed to parse rpm repository %s (%s): %v", repoCfg.Name, repoCfg.URL, err)
			repoErrors[repoCfg.Name] = err
			continue
		}
		repoPackages[repoCfg.Name] = pkgs
	}

	for i, name := range names {
		found := false
		for _, repoCfg := range a.repoCfgs {
			pkgs, ok := repoPackages[repoCfg.Name]
			if !ok {
				continue
			}

			for _, pkg := range pkgs {
				pkgName := pkg.PkgName
				if pkgName == "" {
					pkgName = pkg.Name
				}
				if strings.EqualFold(pkgName, name) || strings.EqualFold(pkg.Name, name) {
					results[i] = Result{
						Name:        name,
						State:       StateVerified,
						Repo:        repoCfg.Name,
						Version:     pkg.Version,
						Description: pkg.Description,
					}
					found = true
					break
				}
			}
			if found {
				break
			}
		}

		if !found {
			// If any repository failed to fetch, package existence is unverified
			if len(repoErrors) > 0 {
				results[i] = Result{
					Name:  name,
					State: StateUnverified,
				}
			} else {
				results[i] = Result{
					Name:  name,
					State: StateNotFound,
				}
			}
		}
	}

	return results, nil
}

// Search performs a case-insensitive search across repository packages by name and description.
func (a *RpmAdapter) Search(ctx context.Context, term string, limit int) ([]Result, error) {
	if limit <= 0 {
		limit = 20
	}

	term = strings.ToLower(strings.TrimSpace(term))
	var results []Result
	seen := make(map[string]bool)

	for _, repoCfg := range a.repoCfgs {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("pkgquery: context cancelled during rpm search: %w", ctx.Err())
		default:
		}

		a.mu.Lock()
		pkgs, err := a.parseMetadataFunc(repoCfg.URL, "repodata/primary.xml.gz", nil)
		a.mu.Unlock()

		if err != nil {
			log.Warnf("pkgquery: search skipping unreachable rpm repo %s: %v", repoCfg.Name, err)
			continue
		}

		for _, pkg := range pkgs {
			pkgName := pkg.PkgName
			if pkgName == "" {
				pkgName = pkg.Name
			}

			if seen[pkgName] {
				continue
			}

			nameMatch := strings.Contains(strings.ToLower(pkgName), term) || strings.Contains(strings.ToLower(pkg.Name), term)
			descMatch := strings.Contains(strings.ToLower(pkg.Description), term)

			if nameMatch || descMatch {
				seen[pkgName] = true
				results = append(results, Result{
					Name:        pkgName,
					State:       StateVerified,
					Repo:        repoCfg.Name,
					Version:     pkg.Version,
					Description: pkg.Description,
				})
				if len(results) >= limit {
					return results, nil
				}
			}
		}
	}

	return results, nil
}
