package pkgquery

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage/debutils"
)

type debMetadataParserFunc func(baseURL string, pkggz string, releaseFile string, releaseSign string, pbGPGKey string, buildPath string, arch string, packageFilter []string) ([]ospackage.PackageInfo, error)

// DebAdapter implements Querier for Debian, Ubuntu, and eLxr distributions.
type DebAdapter struct {
	mu                sync.Mutex
	repoCfgs          []debutils.RepoConfig
	parseMetadataFunc debMetadataParserFunc
	cache             map[string][]ospackage.PackageInfo
}

// NewDebAdapter constructs a new DebAdapter with the given repository configurations.
func NewDebAdapter(repoCfgs []debutils.RepoConfig) (*DebAdapter, error) {
	if len(repoCfgs) == 0 {
		return nil, fmt.Errorf("pkgquery: deb adapter requires at least one repository configuration")
	}
	return &DebAdapter{
		repoCfgs:          repoCfgs,
		parseMetadataFunc: debutils.ParseRepositoryMetadata,
		cache:             make(map[string][]ospackage.PackageInfo),
	}, nil
}

func getReleaseHash(ctx context.Context, releaseURL string) (string, error) {
	// Local paths (for tests or local mirrors) bypass HTTP
	if strings.HasPrefix(releaseURL, "/") || strings.HasPrefix(releaseURL, "file://") {
		// Just return a unique hash for local files to force parsing, or we could hash the file
		// Returning an empty string here falls back to parsing
		return "", fmt.Errorf("local file paths do not support HTTP fetch")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", releaseURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	h := sha256.New()
	if _, err := io.Copy(h, resp.Body); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func (a *DebAdapter) getRepoPackages(ctx context.Context, repoCfg debutils.RepoConfig) ([]ospackage.PackageInfo, error) {
	isLocalhost := strings.Contains(repoCfg.ReleaseFile, "localhost") || strings.Contains(repoCfg.ReleaseFile, "127.0.0.1")

	var releaseHash string
	if !isLocalhost {
		hash, err := getReleaseHash(ctx, repoCfg.ReleaseFile)
		if err != nil {
			log.Warnf("pkgquery: failed to fetch Release file for %s, bypassing cache: %v", repoCfg.Name, err)
		} else {
			releaseHash = hash
			a.mu.Lock()
			pkgs, ok := a.cache[releaseHash]
			a.mu.Unlock()

			if ok {
				return pkgs, nil
			}
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// Double check cache in case another goroutine just updated it
	if !isLocalhost && releaseHash != "" {
		if pkgs, ok := a.cache[releaseHash]; ok {
			return pkgs, nil
		}
	}

	pkgs, err := a.parseMetadataFunc(
		repoCfg.PkgPrefix,
		repoCfg.PkgList,
		repoCfg.ReleaseFile,
		repoCfg.ReleaseSign,
		repoCfg.PbGPGKey,
		repoCfg.BuildPath,
		repoCfg.Arch,
		repoCfg.AllowPackages,
	)
	if err != nil {
		return nil, err
	}

	if !isLocalhost && releaseHash != "" {
		a.cache[releaseHash] = pkgs
	}

	return pkgs, nil
}

// Lookup queries the repositories for the specified package names.
func (a *DebAdapter) Lookup(ctx context.Context, names []string) ([]Result, error) {
	results := make([]Result, len(names))
	repoPackages := make(map[string][]ospackage.PackageInfo)
	repoErrors := make(map[string]error)

	// Fetch metadata for each repository
	for _, repoCfg := range a.repoCfgs {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("pkgquery: context cancelled during deb metadata lookup: %w", ctx.Err())
		default:
		}

		pkgs, err := a.getRepoPackages(ctx, repoCfg)
		if err != nil {
			log.Warnf("pkgquery: failed to parse deb repository %s (%s): %v", repoCfg.Name, repoCfg.PkgPrefix, err)
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
func (a *DebAdapter) Search(ctx context.Context, term string, limit int) ([]Result, error) {
	if limit <= 0 {
		limit = 20
	}

	term = strings.ToLower(strings.TrimSpace(term))
	var results []Result
	seen := make(map[string]bool)

	for _, repoCfg := range a.repoCfgs {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("pkgquery: context cancelled during deb search: %w", ctx.Err())
		default:
		}

		pkgs, err := a.getRepoPackages(ctx, repoCfg)
		if err != nil {
			log.Warnf("pkgquery: search skipping unreachable deb repo %s: %v", repoCfg.Name, err)
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
