package debutils

import (
	"bufio"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/open-edge-platform/os-image-composer/internal/config"
	"github.com/open-edge-platform/os-image-composer/internal/ospackage"
	"github.com/open-edge-platform/os-image-composer/internal/ospackage/pkgfetcher"
	"github.com/open-edge-platform/os-image-composer/internal/utils/logger"
)

// VersionConstraint represents a version operator and version pair
type VersionConstraint struct {
	Op          string
	Ver         string
	Alternative string // Alternative package name for constraints like "logsave | e2fsprogs (<< 1.45.3-1~)"
}

func isGlobPattern(pattern string) bool {
	return strings.ContainsAny(pattern, "*?[]")
}

func matchesPackageFilter(pkgName string, filter []string) bool {
	if len(filter) == 0 {
		return true
	}

	for _, pattern := range filter {
		if isGlobPattern(pattern) {
			if ok, err := path.Match(pattern, pkgName); err == nil && ok {
				return true
			}
		}

		if pkgName == pattern {
			return true
		}

		if strings.HasPrefix(pkgName, pattern+"-") || strings.HasPrefix(pkgName, pattern) {
			return true
		}
	}

	return false
}

func GenerateDot(pkgs []ospackage.PackageInfo, file string, pkgSources map[string]config.PackageSource) error {
	log := logger.Logger()
	log.Infof("Generating DOT file %s", file)

	outFile, err := os.Create(file)
	if err != nil {
		return fmt.Errorf("creating DOT file: %w", err)
	}
	defer outFile.Close()

	writer := bufio.NewWriter(outFile)
	defer writer.Flush()

	if _, err := fmt.Fprintln(writer, "digraph G {"); err != nil {
		return fmt.Errorf("writing DOT header: %w", err)
	}
	if _, err := fmt.Fprintln(writer, "  rankdir=LR;"); err != nil {
		return fmt.Errorf("writing DOT attributes: %w", err)
	}
	if _, err := fmt.Fprintln(writer, "  node [shape=box];"); err != nil {
		return fmt.Errorf("writing DOT node defaults: %w", err)
	}

	edgesWritten := make(map[string]bool)

	for _, pkg := range pkgs {
		if pkg.Name == "" {
			continue
		}
		if _, err := fmt.Fprintf(writer, "  \"%s\";\n", pkg.Name); err != nil {
			return fmt.Errorf("writing DOT node for %s: %w", pkg.Name, err)
		}
		for _, dep := range pkg.Requires {
			depName := CleanDependencyName(dep)
			if depName == "" {
				continue
			}
			edgeKey := pkg.Name + "|" + depName
			if edgesWritten[edgeKey] {
				continue
			}
			if _, err := fmt.Fprintf(writer, "  \"%s\" -> \"%s\";\n", pkg.Name, depName); err != nil {
				return fmt.Errorf("writing DOT edge %s->%s: %w", pkg.Name, depName, err)
			}
			edgesWritten[edgeKey] = true
		}
	}

	if _, err := fmt.Fprintln(writer, "}"); err != nil {
		return fmt.Errorf("writing DOT footer: %w", err)
	}

	return nil
}

// ParseRepositoryMetadata parses the Packages.gz file from gzHref.
func ParseRepositoryMetadata(baseURL string, pkggz string, releaseFile string, releaseSign string, pbGPGKey string, buildPath string, arch string, packageFilter []string) ([]ospackage.PackageInfo, error) {
	log := logger.Logger()

	// Ensure pkgMetaDir exists, create if not
	// pkgMetaDir := filepath.Join(config.TempDir(), "builds", "elxr12")
	pkgMetaDir := buildPath
	if err := os.MkdirAll(pkgMetaDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create pkgMetaDir: %w", err)
	}

	//verify release file
	localPkggzFile := filepath.Join(pkgMetaDir, filepath.Base(pkggz))
	localReleaseFile := filepath.Join(pkgMetaDir, filepath.Base(releaseFile))
	localReleaseSign := filepath.Join(pkgMetaDir, filepath.Base(releaseSign))
	localPBGPGKey := filepath.Join(pkgMetaDir, filepath.Base(pbGPGKey))

	// Determine if pbGPGKey is a URL or file path
	pbkeyIsURL := false
	isTrustedRepo := pbGPGKey == "[trusted=yes]"

	if strings.HasPrefix(pbGPGKey, "http://") || strings.HasPrefix(pbGPGKey, "https://") {
		pbkeyIsURL = true
	} else {
		localPBGPGKey = pbGPGKey
	}

	var localFiles []string
	var urllist []string

	if isTrustedRepo {
		// For trusted repos, skip Release.gpg and GPG key download
		localFiles = []string{localPkggzFile, localReleaseFile}
		urllist = []string{pkggz, releaseFile}
	} else if pbkeyIsURL {
		// Remove any existing local files to ensure fresh downloads
		localFiles = []string{localPkggzFile, localReleaseFile, localReleaseSign, localPBGPGKey}
		urllist = []string{pkggz, releaseFile, releaseSign, pbGPGKey}
	} else {
		localFiles = []string{localPkggzFile, localReleaseFile, localReleaseSign}
		urllist = []string{pkggz, releaseFile, releaseSign}
	}

	for _, f := range localFiles {
		if _, err := os.Stat(f); err == nil {
			if remErr := os.Remove(f); remErr != nil {
				return nil, fmt.Errorf("failed to remove old file %s: %w", f, remErr)
			}
		}
	}

	// Download the debian repo files
	err := pkgfetcher.FetchPackages(urllist, pkgMetaDir, 1)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch critical repo config packages: %w", err)
	}
	// Verify the release file
	relVryResult, err := VerifyRelease(localReleaseFile, localReleaseSign, localPBGPGKey)
	if err != nil {
		return nil, fmt.Errorf("failed to verify release file: %w", err)
	}
	if !relVryResult {
		return nil, fmt.Errorf("release file verification failed")
	}

	// verify the sham256 checksum of the Packages.gz file
	log.Infof("verifying checksum of package metadata file %s %s", baseURL, localPkggzFile)
	// get component from buildPath
	component := "main"
	// Detect last underscore and extract the word after it as component
	if idx := strings.LastIndex(buildPath, "_"); idx != -1 && len(buildPath) > idx+1 {
		component = buildPath[idx+1:]
	}
	//
	pkggzVryResult, err := VerifyPackagegz(localReleaseFile, localPkggzFile, arch, component)
	if err != nil {
		return nil, fmt.Errorf("failed to verify pkg file: %w", err)
	}
	if !pkggzVryResult {
		return nil, fmt.Errorf("package file verification failed")
	}

	//Decompress the Packages (xz or gz) file
	// The decompressed file will be named as Packages
	PkgMetaFile := filepath.Join(pkgMetaDir, filepath.Base(pkggz))
	pkgMetaFileNoExt := filepath.Join(filepath.Dir(PkgMetaFile), strings.TrimSuffix(filepath.Base(PkgMetaFile), filepath.Ext(PkgMetaFile)))
	log.Infof("decompressing package metadata file %s to %s", PkgMetaFile, pkgMetaFileNoExt)

	files, err := Decompress(PkgMetaFile, pkgMetaFileNoExt)
	if err != nil {
		return []ospackage.PackageInfo{}, fmt.Errorf("failed package decompress: %w", err)
	}
	log.Infof("decompressed files: %v", files)

	//Parse the decompressed file
	if len(files) == 0 {
		return nil, fmt.Errorf("no decompressed files found")
	}
	f, err := os.Open(files[0])
	if err != nil {
		return nil, fmt.Errorf("failed to open decompressed file: %w", err)
	}
	defer f.Close()

	var pkgs []ospackage.PackageInfo
	pkg := ospackage.PackageInfo{}
	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("error reading file: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")

		if line == "" {
			// End of one package entry
			if pkg.Name != "" {
				if matchesPackageFilter(pkg.Name, packageFilter) {
					pkgs = append(pkgs, pkg)
				}
				pkg = ospackage.PackageInfo{}
			}
			if err == io.EOF {
				break
			}
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			if err == io.EOF {
				break
			}
			continue
		}

		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch key {
		case "Package":
			pkg.Name = val
			pkg.Type = "deb"
		case "Version":
			pkg.Version = val
		case "Pre-Depends":
			// Split dependencies by comma and clean each dependency
			deps := strings.Split(val, ",")
			for _, dep := range deps {
				cleanedDep := CleanDependencyName(dep)
				if cleanedDep != "" {
					pkg.Requires = append(pkg.Requires, cleanedDep)
				}
			}
		case "Depends":
			// Split dependencies by comma and clean each dependency
			deps := strings.Split(val, ",")
			pkg.RequiresVer = append(pkg.RequiresVer, deps...)
			for _, dep := range deps {
				cleanedDep := CleanDependencyName(dep)
				if cleanedDep != "" {
					pkg.Requires = append(pkg.Requires, cleanedDep)
				}
			}
		case "Provides":
			// Split provides by comma and trim spaces, remove version constraints
			deps := strings.Split(val, ",")
			for i := range deps {
				dep := strings.TrimSpace(deps[i])
				// Remove version constraints, e.g. "foo (= 1.2)" -> "foo"
				if idx := strings.Index(dep, " "); idx > 0 {
					dep = dep[:idx]
				}
				deps[i] = dep
			}
			pkg.Provides = deps
		case "Filename":
			pkg.URL, _ = getFullUrl(val, baseURL)
		case "SHA256":
			pkg.Checksums = append(pkg.Checksums, ospackage.Checksum{
				Algorithm: "SHA256",
				Value:     val,
			})

		case "SHA1":
			pkg.Checksums = append(pkg.Checksums, ospackage.Checksum{
				Algorithm: "SHA1",
				Value:     val,
			})
		case "SHA512":
			pkg.Checksums = append(pkg.Checksums, ospackage.Checksum{
				Algorithm: "SHA512",
				Value:     val,
			})
		case "Description":
			pkg.Description = val
		case "Architecture":
			if val == "all" || val == "any" {
				pkg.Arch = "noarch"
			} else {
				pkg.Arch = val
			}
		case "Maintainer":
			pkg.Origin = val
		}
		if err == io.EOF {
			break
		}

	}

	// Add the last package if file doesn't end with a blank line
	if pkg.Name != "" {
		if matchesPackageFilter(pkg.Name, packageFilter) {
			pkgs = append(pkgs, pkg)
		}
	}

	return pkgs, nil
}

// getRepositoryPriority returns the priority for a given repository URL
func getRepositoryPriority(packageURL string) int {
	repoBase, err := extractRepoBase(packageURL)
	if err != nil {
		return 0 // Default priority if we can't extract repo base
	}

	// Check global RepoCfgs for priority
	if len(RepoCfgs) > 0 {
		for _, repoCfg := range RepoCfgs {
			if repoCfg.PkgPrefix == repoBase {
				return repoCfg.Priority
			}
		}
	}

	// Check single RepoCfg for backward compatibility
	if RepoCfg.PkgPrefix == repoBase {
		return RepoCfg.Priority
	}

	return 0 // Default priority
}

// APT Priority behavior functions

// shouldBlockPackage returns true if the package should be blocked based on priority < 0
func shouldBlockPackage(pkg ospackage.PackageInfo) bool {
	priority := getRepositoryPriority(pkg.URL)
	return priority < 0
}

// shouldForceInstall returns true if the package should be force installed (priority > 1000)
func shouldForceInstall(pkg ospackage.PackageInfo) bool {
	priority := getRepositoryPriority(pkg.URL)
	return priority > 1000
}

// shouldInstallEvenIfLower returns true if the package should be installed even if version is lower (priority = 1000)
func shouldInstallEvenIfLower(pkg ospackage.PackageInfo) bool {
	priority := getRepositoryPriority(pkg.URL)
	return priority == 1000
}

// shouldPrefer returns true if the package should be preferred (priority = 990)
func shouldPrefer(pkg ospackage.PackageInfo) bool {
	priority := getRepositoryPriority(pkg.URL)
	return priority == 990
}

// filterCandidatesByPriority filters out blocked packages and applies priority-based sorting
func filterCandidatesByPriority(candidates []ospackage.PackageInfo) []ospackage.PackageInfo {
	var filtered []ospackage.PackageInfo

	// First pass: filter out blocked packages (priority < 0)
	for _, candidate := range candidates {
		if !shouldBlockPackage(candidate) {
			filtered = append(filtered, candidate)
		}
	}

	// Sort by APT priority rules
	sort.Slice(filtered, func(i, j int) bool {
		pkgI := filtered[i]
		pkgJ := filtered[j]

		priorityI := getRepositoryPriority(pkgI.URL)
		priorityJ := getRepositoryPriority(pkgJ.URL)

		// Force install (>1000) has highest preference
		forceI := shouldForceInstall(pkgI)
		forceJ := shouldForceInstall(pkgJ)
		if forceI != forceJ {
			return forceI // Force install comes first
		}

		// Install even if lower (1000) has next preference
		lowerI := shouldInstallEvenIfLower(pkgI)
		lowerJ := shouldInstallEvenIfLower(pkgJ)
		if lowerI != lowerJ {
			return lowerI
		}

		// Preferred (990) comes next
		preferI := shouldPrefer(pkgI)
		preferJ := shouldPrefer(pkgJ)
		if preferI != preferJ {
			return preferI
		}

		// For same priority category, use numerical priority comparison
		if priorityI != priorityJ {
			return priorityI > priorityJ
		}

		// Finally, compare by version (highest version first)
		return compareVersions(pkgI.Version, pkgJ.Version) > 0
	})

	return filtered
}

// comparePriorityBehavior compares two packages based on APT priority behavior
// Returns true if pkgA should be preferred over pkgB
func comparePriorityBehavior(pkgA, pkgB ospackage.PackageInfo) bool {
	// Block packages with negative priority
	if shouldBlockPackage(pkgA) {
		return false
	}
	if shouldBlockPackage(pkgB) {
		return true
	}

	// Force install (>1000) beats everything else
	if shouldForceInstall(pkgA) && !shouldForceInstall(pkgB) {
		return true
	}
	if shouldForceInstall(pkgB) && !shouldForceInstall(pkgA) {
		return false
	}

	// Install even if lower (1000) beats lower priorities
	if shouldInstallEvenIfLower(pkgA) && !shouldInstallEvenIfLower(pkgB) && !shouldForceInstall(pkgB) {
		return true
	}
	if shouldInstallEvenIfLower(pkgB) && !shouldInstallEvenIfLower(pkgA) && !shouldForceInstall(pkgA) {
		return false
	}

	// Preferred (990) beats default and lower
	if shouldPrefer(pkgA) && !shouldPrefer(pkgB) && !shouldInstallEvenIfLower(pkgB) && !shouldForceInstall(pkgB) {
		return true
	}
	if shouldPrefer(pkgB) && !shouldPrefer(pkgA) && !shouldInstallEvenIfLower(pkgA) && !shouldForceInstall(pkgA) {
		return false
	}

	// For same priority category, compare versions
	priorityA := getRepositoryPriority(pkgA.URL)
	priorityB := getRepositoryPriority(pkgB.URL)

	if priorityA == priorityB {
		// Special handling for priority 1000 - can install even if version is lower
		if priorityA == 1000 {
			return true // Accept either package for priority 1000
		}

		// For other priorities, prefer higher version
		return compareVersions(pkgA.Version, pkgB.Version) > 0
	}

	// Different priorities - higher numerical priority wins
	return priorityA > priorityB
}

// ResolveDependencies takes a seed list of PackageInfos (the exact versions
// matched) and the full list of all PackageInfos from the repo, and
// returns the minimal closure of PackageInfos needed to satisfy all Requires.
func ResolveDependencies(requested []ospackage.PackageInfo, all []ospackage.PackageInfo) ([]ospackage.PackageInfo, error) {
	log := logger.Logger()

	// Build maps for fast lookup
	byNameVer := make(map[string]ospackage.PackageInfo, len(all))
	for _, pi := range all {
		if pi.Version != "" {
			key := fmt.Sprintf("%s=%s", pi.Name, pi.Version)
			byNameVer[key] = pi
		}
	}
	neededSet := make(map[string]struct{})
	resolvedDeps := make(map[string]ospackage.PackageInfo) // Track resolved dependencies for conflict detection
	queue := make([]ospackage.PackageInfo, 0, len(requested))
	for _, pi := range requested {
		if pi.Version != "" {
			key := fmt.Sprintf("%s=%s", pi.Name, pi.Version)
			if pkg, ok := byNameVer[key]; ok {
				queue = append(queue, pkg)
				continue
			}
		}
		return nil, fmt.Errorf("requested package %q not in repo listing", pi.Name)
	}

	// depedencies resolution logic
	result := make([]ospackage.PackageInfo, 0)
	var parentChildPairs [][]ospackage.PackageInfo // Track parent->child relationships for reporting
	gotMissingPkg := false

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		if _, seen := neededSet[cur.Name]; seen {
			continue
		}
		neededSet[cur.Name] = struct{}{}
		result = append(result, cur)

		// Traverse dependencies
		for _, dep := range cur.Requires {

			depName := CleanDependencyName(dep)
			if depName == "" {
				continue
			}
			if resolvedPkg, seen := resolvedDeps[depName]; seen {
				// Dependency already resolved - check for version conflicts

				// Check if this is a direct dependency without constraints
				isDirect := hasDirectDependency(cur.Requires, depName)

				// Extract version constraints for this dependency from current package
				versionConstraints, hasVersionConstraint := extractVersionRequirement(cur.RequiresVer, depName)

				// If it's a direct dependency, ignore version constraints from alternatives
				if isDirect && hasVersionConstraint {
					// Filter out constraints that come from alternatives (keep only direct constraints)
					var directConstraints []VersionConstraint
					for _, constraint := range versionConstraints {
						// If constraint has alternatives, it's from an alternative requirement
						if constraint.Alternative == "" {
							directConstraints = append(directConstraints, constraint)
						}
					}
					versionConstraints = directConstraints
					hasVersionConstraint = len(directConstraints) > 0
				}

				if hasVersionConstraint {
					var requiredVer string
					var requiredDep string
					// Check if the already-resolved package satisfies the version constraints
					constraintsSatisfied := true
					for _, constraint := range versionConstraints {
						// Check if main package satisfies constraint
						mainSatisfied := false
						if constraint.Op != "" && constraint.Ver != "" {
							cmp, err := CompareDebianVersions(resolvedPkg.Version, constraint.Ver)
							if err == nil {
								switch constraint.Op {
								case "=":
									mainSatisfied = (cmp == 0)
								case "<<", "<":
									mainSatisfied = (cmp < 0)
								case "<=":
									mainSatisfied = (cmp <= 0)
								case ">>", ">":
									mainSatisfied = (cmp > 0)
								case ">=":
									mainSatisfied = (cmp >= 0)
								}
							}
						}

						// If main package doesn't satisfy and we have alternatives, check them
						alternativeSatisfied := false
						if !mainSatisfied && constraint.Alternative != "" {
							alternatives := strings.Split(constraint.Alternative, "|")
							for _, altName := range alternatives {
								altName = strings.TrimSpace(altName)
								if _, altSeen := resolvedDeps[altName]; altSeen {
									// Alternative package is resolved, check if it satisfies (no version constraint for alternatives)
									alternativeSatisfied = true
									break
								}
							}
						}

						if !mainSatisfied && !alternativeSatisfied {
							constraintsSatisfied = false
							requiredVer = constraint.Ver
							requiredDep = depName
							break
						}
					}

					if !constraintsSatisfied {
						// Check if replacement is allowed - if current constraint has exact version (=)
						// and resolved package has different version, this is a conflict
						hasExactVersionConstraint := false
						for _, constraint := range versionConstraints {
							if constraint.Op == "=" {
								hasExactVersionConstraint = true
								break
							}
						}

						// Before throwing error, check if there's a higher priority candidate available
						// But only allow replacement if we don't have an exact version conflict
						candidates := findAllCandidates(depName, all)

						if len(candidates) > 0 && !hasExactVersionConstraint {
							// Find candidates that satisfy the version constraint
							var satisfyingCandidates []ospackage.PackageInfo
							for _, candidate := range candidates {
								candidateSatisfies := true
								for _, constraint := range versionConstraints {
									if constraint.Op != "" && constraint.Ver != "" {
										cmp, err := CompareDebianVersions(candidate.Version, constraint.Ver)
										if err == nil {
											satisfied := false
											switch constraint.Op {
											case "=":
												satisfied = (cmp == 0)
											case "<<", "<":
												satisfied = (cmp < 0)
											case "<=":
												satisfied = (cmp <= 0)
											case ">>", ">":
												satisfied = (cmp > 0)
											case ">=":
												satisfied = (cmp >= 0)
											}
											if !satisfied {
												candidateSatisfies = false
												break
											}
										}
									}
								}
								if candidateSatisfies {
									satisfyingCandidates = append(satisfyingCandidates, candidate)
								}
							}

							if len(satisfyingCandidates) > 0 {
								// Pick the best candidate using the resolver
								newCandidate, err := resolveMultiCandidates(cur, satisfyingCandidates)
								if err == nil {
									resolvedPriority := getRepositoryPriority(resolvedPkg.URL)
									newPriority := getRepositoryPriority(newCandidate.URL)

									// Apply APT priority comparison
									if comparePriorityBehavior(newCandidate, resolvedPkg) {
										// New candidate has higher priority - replace the resolved package
										log.Debugf("replacing %s_%s (priority %d) with higher priority package %s_%s (priority %d)",
											resolvedPkg.Name, resolvedPkg.Version, resolvedPriority,
											newCandidate.Name, newCandidate.Version, newPriority)

										// Remove old package from result and neededSet
										delete(neededSet, resolvedPkg.Name)
										for i, pkg := range result {
											if pkg.Name == resolvedPkg.Name && pkg.Version == resolvedPkg.Version {
												result = append(result[:i], result[i+1:]...)
												break
											}
										}

										// Add new candidate to queue and resolvedDeps
										queue = append(queue, newCandidate)
										resolvedDeps[depName] = newCandidate
										AddParentChildPair(cur, newCandidate, &parentChildPairs)
										continue
									} else {
										log.Debugf("new candidate does not have higher priority, cannot replace")
									}
								}
							}
						}
						return nil, fmt.Errorf("conflicting package dependencies: %s_%s requires %s_%s, but %s_%s is already installed", cur.Name, cur.Version, requiredDep, requiredVer, resolvedPkg.Name, resolvedPkg.Version)
					}
				}
				continue
			}

			candidates := findAllCandidates(depName, all)
			if len(candidates) >= 1 {
				// Pick the candidate using the resolver and add it to the queue
				chosenCandidate, err := resolveMultiCandidates(cur, candidates)
				if err != nil {
					gotMissingPkg = true
					AddParentMissingChildPair(cur, depName+"(missing)", &parentChildPairs)
					log.Warnf("failed to resolve multiple candidates for dependency %q of package %q: %v", depName, cur.Name, err)
					continue
				}
				queue = append(queue, chosenCandidate)
				resolvedDeps[depName] = chosenCandidate // Track resolved dependency
				AddParentChildPair(cur, chosenCandidate, &parentChildPairs)
				continue
			} else {
				// No candidates for primary dependency, check for alternatives
				versionConstraints, _ := extractVersionRequirement(cur.RequiresVer, depName)
				alternativeResolved := false
				for _, constraint := range versionConstraints {
					if constraint.Alternative != "" {
						alternatives := strings.Split(constraint.Alternative, "|")
						for _, altName := range alternatives {
							altName = strings.TrimSpace(altName)
							altCandidates := findAllCandidates(altName, all)
							if len(altCandidates) >= 1 {
								chosenCandidate, err := resolveMultiCandidates(cur, altCandidates)
								if err == nil {
									log.Infof("Successfully resolved alternative %q version %q for missing dependency %q", altName, chosenCandidate.Version, depName)
									queue = append(queue, chosenCandidate)
									resolvedDeps[altName] = chosenCandidate // Track resolved alternative dependency
									AddParentChildPair(cur, chosenCandidate, &parentChildPairs)
									alternativeResolved = true
									break
								} else {
									log.Warnf("Failed to resolve alternative %q for %q: %v", altName, depName, err)
								}
							}
						}
						if alternativeResolved {
							break
						}
					}
				}

				if !alternativeResolved {
					log.Warnf("no candidates found for dependency %q of package %q", depName, cur.Name)
					gotMissingPkg = true
					AddParentMissingChildPair(cur, depName+"(missing)", &parentChildPairs)
				}
				continue
			}
		}
	}

	// check missing dep and write report
	if gotMissingPkg {
		report := BuildDependencyChains(parentChildPairs)
		return nil, fmt.Errorf("one or more requested dependencies not found. See list in %s", report)
	}

	// Sort result by package name for determinism
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result, nil
}

func getFullUrl(filePath string, baseUrl string) (string, error) {
	// Check if the file path is already a full URL
	if strings.HasPrefix(filePath, "http://") || strings.HasPrefix(filePath, "https://") {
		return filePath, nil
	}

	// If not, construct the full URL using the base URL
	fullURL := fmt.Sprintf("%s/%s", strings.TrimSuffix(baseUrl, "/"), filePath)
	return fullURL, nil
}

// CompareDebianVersions compares two Debian version strings.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func CompareDebianVersions(a, b string) (int, error) {
	// Empty-version handling: empty < any non-empty
	if a == "" && b == "" {
		return 0, nil
	}
	if a == "" {
		return -1, nil
	}
	if b == "" {
		return 1, nil
	}

	// Helper to split epoch
	splitEpoch := func(ver string) (epoch int, rest string) {
		parts := strings.SplitN(ver, ":", 2)
		if len(parts) == 2 {
			if _, err := fmt.Sscanf(parts[0], "%d", &epoch); err != nil {
				epoch = 0
			}
			rest = parts[1]
		} else {
			epoch = 0
			rest = ver
		}
		return
	}

	// Split upstream_version and debian_revision at last hyphen
	splitRevision := func(ver string) (upstream string, debian string) {
		if i := strings.LastIndex(ver, "-"); i >= 0 {
			return ver[:i], ver[i+1:]
		}
		return ver, ""
	}

	// nextSegment returns the next contiguous numeric or non-numeric segment.
	nextSegment := func(s string) (seg string, rest string, numeric bool) {
		if s == "" {
			return "", "", false
		}
		// numeric segment
		if s[0] >= '0' && s[0] <= '9' {
			i := 0
			for i < len(s) && s[i] >= '0' && s[i] <= '9' {
				i++
			}
			return s[:i], s[i:], true
		}
		// non-numeric segment
		i := 0
		for i < len(s) && (s[i] < '0' || s[i] > '9') {
			i++
		}
		return s[:i], s[i:], false
	}

	// Character ordering per Debian: '~' < end-of-string < letters < other characters.
	// This ordering is crucial for correct Debian version comparison, as defined in
	// Debian Policy Manual section 5.6.12 ("Version"). See:
	// https://www.debian.org/doc/debian-policy/ch-controlfields.html#version
	charOrder := func(r rune) int {
		if r == '~' {
			return -2
		}
		if r == 0 {
			return -1
		}
		if unicode.IsLetter(r) {
			return int(r)
		}
		return 0x100 + int(r)
	}

	// Compare two non-digit segments using Debian ordering
	compareNonDigitSegments := func(aSeg, bSeg string) int {
		ai, bi := 0, 0
		for {
			var ra, rb rune
			if ai < len(aSeg) {
				ra = rune(aSeg[ai])
			} else {
				ra = 0
			}
			if bi < len(bSeg) {
				rb = rune(bSeg[bi])
			} else {
				rb = 0
			}
			// both ended
			if ra == 0 && rb == 0 {
				return 0
			}
			if ra != rb {
				oa := charOrder(ra)
				ob := charOrder(rb)
				if oa < ob {
					return -1
				}
				return 1
			}
			ai++
			bi++
		}
	}

	// Compare numeric segments (as dpkg: strip leading zeros, compare length, then lexicographically)
	compareNumericSegments := func(aSeg, bSeg string) int {
		aTrim := strings.TrimLeft(aSeg, "0")
		bTrim := strings.TrimLeft(bSeg, "0")
		// treat empty as zero
		if aTrim == "" && bTrim == "" {
			return 0
		}
		if aTrim == "" {
			return -1
		}
		if bTrim == "" {
			return 1
		}
		// longer numeric (more digits) is greater
		if len(aTrim) > len(bTrim) {
			return 1
		}
		if len(aTrim) < len(bTrim) {
			return -1
		}
		// same length -> lexical compare works
		if aTrim > bTrim {
			return 1
		}
		if aTrim < bTrim {
			return -1
		}
		return 0
	}

	// Handle epoch
	epochA, restA := splitEpoch(a)
	epochB, restB := splitEpoch(b)
	if epochA < epochB {
		return -1, nil
	}
	if epochA > epochB {
		return 1, nil
	}

	// Split upstream and debian revisions
	upA, debA := splitRevision(restA)
	upB, debB := splitRevision(restB)

	// Compare iterative parts (used for upstream version and debian revision)
	compareParts := func(sa, sb string) int {
		for sa != "" || sb != "" {
			// Handle tilde first: '~' sorts before everything (including end-of-string)
			if (len(sa) > 0 && sa[0] == '~') || (len(sb) > 0 && sb[0] == '~') {
				if len(sa) > 0 && sa[0] == '~' && !(len(sb) > 0 && sb[0] == '~') {
					return -1
				}
				if len(sb) > 0 && sb[0] == '~' && !(len(sa) > 0 && sa[0] == '~') {
					return 1
				}
				// both have tilde: consume and continue
				if len(sa) > 0 && sa[0] == '~' && len(sb) > 0 && sb[0] == '~' {
					sa = sa[1:]
					sb = sb[1:]
					continue
				}
			}

			// After tilde handling, if either side is exhausted, the exhausted side is less
			if sa == "" && sb == "" {
				break
			}
			if sa == "" {
				return -1
			}
			if sb == "" {
				return 1
			}

			segA, restASeg, numA := nextSegment(sa)
			segB, restBSeg, numB := nextSegment(sb)

			// both empty segments -> continue
			if segA == "" && segB == "" {
				sa, sb = restASeg, restBSeg
				continue
			}

			// numeric vs non-numeric: numeric < non-numeric
			if numA != numB {
				if numA {
					return -1
				}
				return 1
			}

			// both numeric
			if numA && numB {
				if cmp := compareNumericSegments(segA, segB); cmp != 0 {
					return cmp
				}
			} else { // both non-numeric
				if cmp := compareNonDigitSegments(segA, segB); cmp != 0 {
					return cmp
				}
			}

			sa, sb = restASeg, restBSeg
		}
		return 0
	}

	// Compare upstream versions first, then debian revisions
	if cmp := compareParts(upA, upB); cmp != 0 {
		return cmp, nil
	}
	if cmp := compareParts(debA, debB); cmp != 0 {
		return cmp, nil
	}
	return 0, nil
}

// CleanDependencyName extracts the base package name from a complex dependency string.
// Handles version constraints, alternatives, and architecture qualifiers.
// Examples:
//
//	"libc6 (>= 2.34)" -> "libc6"
//	"python3 | python3-dev" -> "python3"
//	"gcc:amd64" -> "gcc"
func CleanDependencyName(dep string) string {
	depName := strings.TrimSpace(dep)

	// Handle alternatives (|) - take the first option
	if idx := strings.Index(depName, "|"); idx > 0 {
		depName = strings.TrimSpace(depName[:idx])
	}

	// Handle architecture qualifiers (:)
	if idx := strings.Index(depName, ":"); idx > 0 {
		depName = depName[:idx]
	}

	// Handle version constraints - remove everything from opening parenthesis
	if idx := strings.Index(depName, "("); idx > 0 {
		depName = strings.TrimSpace(depName[:idx])
	} else if idx := strings.Index(depName, " "); idx > 0 {
		// Handle cases where there's a space but no parentheses
		depName = depName[:idx]
	}

	return strings.TrimSpace(depName)
}

// compareVersions compares two Debian package versions
// Returns 1 if v1 > v2, -1 if v1 < v2, 0 if equal
func compareVersions(v1, v2 string) int {
	// Extract version from Debian package names like "acct_6.6.4-5+b1_amd64.deb"
	extractVersion := func(name string) string {
		parts := strings.Split(name, "_")
		if len(parts) >= 2 {
			return parts[1]
		}
		return name
	}
	ver1 := extractVersion(v1)
	ver2 := extractVersion(v2)
	cmp, _ := CompareDebianVersions(ver1, ver2)
	return cmp
}

// ResolvePackage finds the best matching package for a given package name
func ResolveTopPackageConflicts(want string, all []ospackage.PackageInfo) (ospackage.PackageInfo, bool) {
	var candidates []ospackage.PackageInfo
	for _, pi := range all {
		// 1) exact name and version matched with .deb filenamae, e.g. acct_7.6.4-5+b1_amd64
		if filepath.Base(pi.URL) == want+".deb" {
			candidates = append(candidates, pi)
			break
		}
		// 2) exact name, e.g. acct
		if pi.Name == want {
			candidates = append(candidates, pi)
			continue
		}
		// 3) prefix by want-version ("acl-")
		if strings.HasPrefix(pi.Name, want+"-") {
			// Extract string after "-" and compare with pi.Version
			if dashIdx := strings.LastIndex(want, "-"); dashIdx != -1 {
				verStr := want[dashIdx+1:]
				if strings.Contains(pi.Version, verStr) {
					candidates = append(candidates, pi)
					continue
				}
			}
		}
		// 4) prefix by want.release ("acl-2.3.1-2.")
		if strings.HasPrefix(pi.Name, want+".") {
			candidates = append(candidates, pi)
			continue
		}
		// 5) Debian package format (packagename_version_arch.deb)
		if strings.HasPrefix(pi.Name, want+"_") {
			candidates = append(candidates, pi)
			continue
		}
		// 6) Match package_epoch:version format (e.g., qemu-system_3:9.1.0+git...)
		// The want string includes epoch, but the filename in the repo doesn't
		// Example: want="qemu-system_3:9.1.0+git...", pi.Version="3:9.1.0+git...", filename="qemu-system_9.1.0+git..."
		if strings.Contains(want, "_") && strings.Contains(want, ":") {
			parts := strings.SplitN(want, "_", 2)
			if len(parts) == 2 {
				pkgName := parts[0]
				wantVersion := parts[1]
				// Check if package name matches and version matches (with epoch)
				if pi.Name == pkgName && pi.Version == wantVersion {
					candidates = append(candidates, pi)
					continue
				}
			}
		}
		// 7) Match package_version format without epoch (e.g., intel-gsc_0.9.5-1ppa1~noble1)
		// The want string doesn't include epoch, but the package version might have it
		// Example: want="intel-gsc_0.9.5-1ppa1~noble1", pi.Version="0:0.9.5-1ppa1~noble1" or "0.9.5-1ppa1~noble1"
		if strings.Contains(want, "_") && !strings.Contains(want, ":") {
			parts := strings.SplitN(want, "_", 2)
			if len(parts) == 2 {
				pkgName := parts[0]
				wantVersion := parts[1]
				// Check if package name matches
				if pi.Name == pkgName {
					// Strip epoch from package version if present and compare
					piVersionNoEpoch := pi.Version
					if colonIdx := strings.Index(pi.Version, ":"); colonIdx != -1 {
						piVersionNoEpoch = pi.Version[colonIdx+1:]
					}
					if piVersionNoEpoch == wantVersion {
						candidates = append(candidates, pi)
						continue
					}
				}
			}
		}
		// 8) Match through Provides field (virtual packages or alternative names)
		// Example: want="mail-transport-agent", pi.Provides=["mail-transport-agent"]
		for _, provided := range pi.Provides {
			if provided == want {
				candidates = append(candidates, pi)
				break
			}
		}
	}

	if len(candidates) == 0 {
		return ospackage.PackageInfo{}, false
	}

	// Filter out blocked packages (priority < 0)
	candidates = filterCandidatesByPriority(candidates)
	if len(candidates) == 0 {
		return ospackage.PackageInfo{}, false
	}

	// If we got an exact match in step (1), it's the only candidate
	if len(candidates) == 1 && (candidates[0].Name == want || candidates[0].Name == want+".deb") {
		return candidates[0], true
	}

	// Candidates already sorted by filterCandidatesByPriority
	return candidates[0], true
}

// Helper function to find all candidates for a dependency
func findAllCandidates(depName string, all []ospackage.PackageInfo) []ospackage.PackageInfo {
	var candidates []ospackage.PackageInfo

	// First pass: look for exact name matches
	for _, pi := range all {
		if pi.Name == depName {
			candidates = append(candidates, pi)
		}
	}

	// If no direct matches found, search in Provides field
	if len(candidates) == 0 {
		for _, pi := range all {
			for _, provided := range pi.Provides {
				if provided == depName {
					candidates = append(candidates, pi)
				}
			}
		}
	}

	// Apply APT priority filtering and sorting
	filtered := filterCandidatesByPriority(candidates)
	return filtered
}

// Helper function to resolve multiple candidates by picking the last one
// extractRepoBase extracts the Debian repo base URL (everything up to /pool/)
func extractRepoBase(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	// Split path by "/pool/"
	parts := strings.SplitN(u.Path, "/pool/", 2)
	if len(parts) < 2 {
		return "", fmt.Errorf("URL does not contain /pool/: %s", rawURL)
	}

	// Rebuild base URL: scheme + host + prefix before /pool/ (without trailing slash)
	base := fmt.Sprintf("%s://%s%s", u.Scheme, u.Host, parts[0])
	return base, nil
}

// hasDirectDependency checks if a dependency appears as a direct requirement (not in alternatives)
func hasDirectDependency(requires []string, depName string) bool {
	for _, req := range requires {
		cleanReq := CleanDependencyName(req)
		if cleanReq == depName {
			return true
		}
	}
	return false
}

func extractVersionRequirement(reqVers []string, depName string) ([]VersionConstraint, bool) {
	var constraints []VersionConstraint
	found := false

	for _, reqVer := range reqVers {
		reqVer = strings.TrimSpace(reqVer)

		// Handle alternatives (|) - check if our depName is in any of the alternatives
		alternatives := strings.Split(reqVer, "|")
		for i, alt := range alternatives {
			alt = strings.TrimSpace(alt)

			// Check if this alternative starts with the dependency name we're looking for
			cleanReqName := CleanDependencyName(alt)
			if cleanReqName != depName {
				continue // Skip to next alternative
			}

			// Found our dependency in this alternative, now extract version constraints
			// Find all version constraints inside parentheses (can be multiple separated by commas)
			if idx := strings.Index(alt, "("); idx != -1 {
				verConstraint := alt[idx+1:]
				if idx2 := strings.Index(verConstraint, ")"); idx2 != -1 {
					verConstraint = verConstraint[:idx2]
				}

				// Handle multiple constraints separated by commas
				constraintParts := strings.Split(verConstraint, ",")
				for _, constraintPart := range constraintParts {
					constraintPart = strings.TrimSpace(constraintPart)
					// Split into operator and version
					parts := strings.Fields(constraintPart)

					// Handle both ">> 1.2.3" and ">>1.2.3" formats
					var op, ver string
					if len(parts) == 2 {
						op, ver = parts[0], parts[1]
					} else if len(parts) == 1 {
						// Try to extract operator from the beginning
						part := parts[0]
						// Check for two-character operators first (<<, >>, >=, <=)
						if len(part) >= 2 {
							prefix := part[:2]
							if prefix == "<<" || prefix == ">>" || prefix == ">=" || prefix == "<=" {
								op = prefix
								ver = part[2:]
							} else if part[0] == '<' || part[0] == '>' || part[0] == '=' {
								// Single-character operator
								op = string(part[0])
								ver = part[1:]
							}
						} else if len(part) >= 1 && (part[0] == '<' || part[0] == '>' || part[0] == '=') {
							op = string(part[0])
							ver = part[1:]
						}
					}

					if op != "" && ver != "" {
						// Collect alternative package names (all alternatives except the current one)
						var altNames []string
						for j, altPkg := range alternatives {
							if j != i {
								altNames = append(altNames, strings.TrimSpace(CleanDependencyName(altPkg)))
							}
						}
						constraint := VersionConstraint{
							Op:          op,
							Ver:         ver,
							Alternative: strings.Join(altNames, "|"),
						}
						constraints = append(constraints, constraint)
						found = true
					}
				}
			} else {
				// No version constraint, but we have alternatives
				if len(alternatives) > 1 {
					// Collect alternative package names (all alternatives except the current one)
					var altNames []string
					for j, altPkg := range alternatives {
						if j != i {
							altNames = append(altNames, strings.TrimSpace(CleanDependencyName(altPkg)))
						}
					}
					constraint := VersionConstraint{
						Alternative: strings.Join(altNames, "|"),
					}
					constraints = append(constraints, constraint)
				}
			}

			// If we found the dependency but no version constraint, still mark as found
			if !found {
				found = true
			}
		}
	}

	return constraints, found
}

// matchesRepoBase checks if candidateBase matches any of the parent repo bases
func matchesRepoBase(parentBase []string, candidateBase string) bool {
	for _, pBase := range parentBase {
		if candidateBase == pBase {
			return true
		}
	}
	return false
}

func resolveMultiCandidates(parentPkg ospackage.PackageInfo, candidates []ospackage.PackageInfo) (ospackage.PackageInfo, error) {
	// Filter out blocked packages (priority < 0) first
	candidates = filterCandidatesByPriority(candidates)
	if len(candidates) == 0 {
		return ospackage.PackageInfo{}, fmt.Errorf("all candidates are blocked by negative priority")
	}

	parent, err := extractRepoBase(parentPkg.URL)
	if err != nil {
		return ospackage.PackageInfo{}, fmt.Errorf("failed to extract repo base from parent package URL: %w", err)
	}
	// Extract parent repo base and handle potential multiple repo configurations
	var imageBase []string
	if len(RepoCfgs) > 0 {
		for _, repocfg := range RepoCfgs {
			if repocfg.PkgPrefix != "" {
				imageBase = append(imageBase, repocfg.PkgPrefix)
			}
		}
	}
	if len(imageBase) == 0 && RepoCfg.PkgPrefix != "" {
		imageBase = append(imageBase, RepoCfg.PkgPrefix)
	}

	var parentBase []string
	// check if parent part of base
	if matchesRepoBase(imageBase, parent) {
		parentBase = imageBase
	} else {
		parentBase = []string{parent}
	}

	/////////////////////////////////////
	//A: if version is specified
	/////////////////////////////////////
	// All candidates have the same .Name, so just use candidates[0].Name for version extraction
	var versionConstraints []VersionConstraint
	hasVersionConstraint := false
	if len(candidates) > 0 {
		depName := candidates[0].Name
		isDirect := hasDirectDependency(parentPkg.Requires, depName)

		versionConstraints, hasVersionConstraint = extractVersionRequirement(parentPkg.RequiresVer, depName)

		// If it's a direct dependency, ignore version constraints from alternatives
		if isDirect && hasVersionConstraint {
			var directConstraints []VersionConstraint
			for _, constraint := range versionConstraints {
				if constraint.Alternative == "" {
					directConstraints = append(directConstraints, constraint)
				}
			}
			versionConstraints = directConstraints
			hasVersionConstraint = len(directConstraints) > 0
		}
	}

	if hasVersionConstraint {
		// First pass: look for candidates from the same repo that meet version constraint
		var sameRepoMatches []ospackage.PackageInfo
		var otherRepoMatches []ospackage.PackageInfo

		for _, candidate := range candidates {
			candidateBase, err := extractRepoBase(candidate.URL)
			if err != nil {
				continue
			}

			// Check if all version constraints are satisfied
			allConstraintsSatisfied := true
			for _, constraint := range versionConstraints {
				// Check if main package (candidate) satisfies constraint
				mainSatisfied := false
				if constraint.Op != "" && constraint.Ver != "" {
					cmp, err := CompareDebianVersions(candidate.Version, constraint.Ver)
					if err == nil {
						switch constraint.Op {
						case "=":
							mainSatisfied = (cmp == 0)
						case "<<", "<":
							mainSatisfied = (cmp < 0)
						case "<=":
							mainSatisfied = (cmp <= 0)
						case ">>", ">":
							mainSatisfied = (cmp > 0)
						case ">=":
							mainSatisfied = (cmp >= 0)
						}
					}
				} else {
					// No version constraint, satisfied by default
					mainSatisfied = true
				}

				// If main package doesn't satisfy and we have alternatives, check if this is acceptable
				// In candidate selection, we should NOT mark alternatives as satisfied automatically
				// since we're evaluating the specific candidate for the main package
				alternativeSatisfied := false

				if !mainSatisfied && !alternativeSatisfied {
					allConstraintsSatisfied = false
					break
				}
			}

			if allConstraintsSatisfied {
				// if candidateBase == parentBase {
				if matchesRepoBase(parentBase, candidateBase) {
					sameRepoMatches = append(sameRepoMatches, candidate)
				} else {
					otherRepoMatches = append(otherRepoMatches, candidate)
				}
			}
		}

		// Compare using APT priority behavior rules between sameRepoMatches[0] and otherRepoMatches[0]
		if len(sameRepoMatches) > 0 && len(otherRepoMatches) > 0 {
			// Apply APT priority behavior comparison
			if comparePriorityBehavior(sameRepoMatches[0], otherRepoMatches[0]) {
				return sameRepoMatches[0], nil
			} else {
				return otherRepoMatches[0], nil
			}
		}

		// Priority 1: return first match from same repo (if only sameRepo has matches)
		if len(sameRepoMatches) > 0 {
			return sameRepoMatches[0], nil
		}

		// Priority 2: return first match from other repos (if only otherRepo has matches)
		if len(otherRepoMatches) > 0 {
			return otherRepoMatches[0], nil
		}

		constraintStr := ""
		for i, vc := range versionConstraints {
			if i > 0 {
				constraintStr += ", "
			}
			constraintStr += vc.Op + vc.Ver
		}
		return ospackage.PackageInfo{}, fmt.Errorf("no candidates satisfy version constraints: %s", constraintStr)
	}

	/////////////////////////////////////
	// B: if version is not specified
	//////////////////////////////////////

	// Check for empty candidates list
	if len(candidates) == 0 {
		return ospackage.PackageInfo{}, fmt.Errorf("no candidates provided for selection")
	}

	// If only one candidate, return it
	if len(candidates) == 1 {
		return candidates[0], nil
	}

	// Rule 1: find all candidates with the same base URL and candidates from other repos
	var sameBaseCandidates []ospackage.PackageInfo
	var otherBaseCandidates []ospackage.PackageInfo
	for _, candidate := range candidates {
		candidateBase, err := extractRepoBase(candidate.URL)
		if err != nil {
			continue
		}
		// if candidateBase == parentBase {
		if matchesRepoBase(parentBase, candidateBase) {
			sameBaseCandidates = append(sameBaseCandidates, candidate)
		} else {
			otherBaseCandidates = append(otherBaseCandidates, candidate)
		}
	}

	// Compare using APT priority behavior rules between same base and other base candidates
	if len(sameBaseCandidates) > 0 && len(otherBaseCandidates) > 0 {
		// Apply APT priority behavior comparison
		if comparePriorityBehavior(sameBaseCandidates[0], otherBaseCandidates[0]) {
			return sameBaseCandidates[0], nil
		} else {
			return otherBaseCandidates[0], nil
		}
	}

	// If we only have candidates with the same base URL, return the first (latest) one
	if len(sameBaseCandidates) > 0 {
		return sameBaseCandidates[0], nil
	}

	// If we only have candidates from other repos, return the first (latest) one
	if len(otherBaseCandidates) > 0 {
		return otherBaseCandidates[0], nil
	}

	// Fallback: return first candidate if no categorization worked
	return candidates[0], nil
}
