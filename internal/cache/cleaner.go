package cache

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	fileutil "github.com/open-edge-platform/image-composer-tool/internal/utils/file"
)

// CleanOptions defines what cache artifacts should be removed.
type CleanOptions struct {
	CleanPackages  bool   // remove package cache entries under cache_dir/pkgCache
	CleanWorkspace bool   // remove workspace chroot cache directories
	ProviderID     string // optional provider filter (os-dist-arch)
	DryRun         bool   // report actions without deleting anything
}

// CleanResult contains the outcome of a cache cleanup run.
type CleanResult struct {
	RemovedPaths []string
	SkippedPaths []string
}

// Clean removes cached artifacts according to the provided options.
func Clean(opts CleanOptions) (*CleanResult, error) {
	if !opts.CleanPackages && !opts.CleanWorkspace {
		return nil, fmt.Errorf("at least one scope must be specified")
	}

	targets, missing, err := gatherTargets(opts)
	if err != nil {
		return nil, err
	}

	removed := make([]string, 0, len(targets))
	skippedSet := make(map[string]struct{}, len(missing))
	for _, path := range missing {
		skippedSet[path] = struct{}{}
	}

	for _, target := range targets {
		exists, err := pathExists(target)
		if err != nil {
			return nil, fmt.Errorf("checking %s: %w", target, err)
		}
		if !exists {
			skippedSet[target] = struct{}{}
			continue
		}

		if opts.DryRun {
			removed = append(removed, target)
			continue
		}

		if err := os.RemoveAll(target); err != nil {
			return nil, fmt.Errorf("removing %s: %w", target, err)
		}
		removed = append(removed, target)
	}

	sort.Strings(removed)

	skipped := make([]string, 0, len(skippedSet))
	for path := range skippedSet {
		skipped = append(skipped, path)
	}
	sort.Strings(skipped)

	return &CleanResult{
		RemovedPaths: removed,
		SkippedPaths: skipped,
	}, nil
}

func gatherTargets(opts CleanOptions) ([]string, []string, error) {
	targets := make(map[string]struct{})
	missing := make(map[string]struct{})

	if opts.CleanPackages {
		pkgTargets, pkgMissing, err := packageTargets(opts.ProviderID)
		if err != nil {
			return nil, nil, err
		}
		for _, path := range pkgTargets {
			targets[path] = struct{}{}
		}
		for _, path := range pkgMissing {
			missing[path] = struct{}{}
		}
	}

	if opts.CleanWorkspace {
		workspaceTargets, workspaceMissing, err := workspaceTargets(opts.ProviderID)
		if err != nil {
			return nil, nil, err
		}
		for _, path := range workspaceTargets {
			targets[path] = struct{}{}
		}
		for _, path := range workspaceMissing {
			missing[path] = struct{}{}
		}
	}

	targetList := make([]string, 0, len(targets))
	for path := range targets {
		targetList = append(targetList, path)
	}
	sort.Strings(targetList)

	missingList := make([]string, 0, len(missing))
	for path := range missing {
		missingList = append(missingList, path)
	}
	sort.Strings(missingList)

	return targetList, missingList, nil
}

func packageTargets(providerID string) ([]string, []string, error) {
	cacheDir, err := config.CacheDir()
	if err != nil {
		return nil, nil, fmt.Errorf("resolving cache directory: %w", err)
	}

	pkgRoot := filepath.Join(cacheDir, "pkgCache")
	if err := ensureSubPath(cacheDir, pkgRoot); err != nil {
		return nil, nil, err
	}

	if providerID != "" {
		target := filepath.Join(pkgRoot, providerID)
		if err := ensureSubPath(pkgRoot, target); err != nil {
			return nil, nil, err
		}

		exists, err := pathExists(target)
		if err != nil {
			return nil, nil, fmt.Errorf("checking %s: %w", target, err)
		}

		if exists {
			return []string{target}, nil, nil
		}
		return nil, nil, nil // Provider-specific target doesn't exist
	}

	entries, err := os.ReadDir(pkgRoot)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, nil // No package cache directory = no targets, no missing
		}
		return nil, nil, fmt.Errorf("listing package cache directory: %w", err)
	}

	targets := make([]string, 0, len(entries))
	for _, entry := range entries {
		target := filepath.Join(pkgRoot, entry.Name())
		if err := ensureSubPath(pkgRoot, target); err != nil {
			return nil, nil, err
		}
		targets = append(targets, target)
	}
	return targets, nil, nil
}

func workspaceTargets(providerID string) ([]string, []string, error) {
	workDir, err := config.WorkDir()
	if err != nil {
		return nil, nil, fmt.Errorf("resolving work directory: %w", err)
	}

	if providerID != "" {
		// Handle specific provider request
		return workspaceTargetsForProvider(workDir, providerID)
	}

	// Handle all providers - only target directories that actually exist
	entries, err := os.ReadDir(workDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, nil // No workspace directory = no targets, no missing
		}
		return nil, nil, fmt.Errorf("listing workspace directory: %w", err)
	}

	targets := []string{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		providerTargets, _, err := workspaceTargetsForProvider(workDir, entry.Name())
		if err != nil {
			return nil, nil, err
		}
		targets = append(targets, providerTargets...)
	}

	return targets, nil, nil
}

func workspaceTargetsForProvider(workDir, providerID string) ([]string, []string, error) {
	targets := []string{}

	for _, sub := range []string{"chrootenv", "chrootbuild"} {
		target := filepath.Join(workDir, providerID, sub)
		if err := ensureSubPath(workDir, target); err != nil {
			return nil, nil, err
		}

		exists, err := pathExists(target)
		if err != nil {
			return nil, nil, fmt.Errorf("checking %s: %w", target, err)
		}

		if exists {
			targets = append(targets, target)
		}
		// Don't report missing subdirectories - only clean what actually exists
	}

	return targets, nil, nil
}

func ensureSubPath(base, target string) error {
	ok, err := fileutil.IsSubPath(base, target)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("refusing to operate on %s because it is outside %s", target, base)
	}
	return nil
}

func pathExists(path string) (bool, error) {
	if path == "" {
		return false, fmt.Errorf("path must not be empty")
	}
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, err
}
