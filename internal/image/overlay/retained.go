package overlay

import (
	"fmt"
	"sort"
	"strings"
)

func retainedDependencyFailures(family PackageManager, baseline []BaselinePackage,
	resolved []ResolvedPackage, removals []PlannedAction, nativeArch string,
) []PlannedAction {
	if family != PackageManagerAPT {
		return nil
	}
	before := retainedPackageIndex(baseline)
	after := retainedPackageIndex(baseline)
	for _, pkg := range resolved {
		var unchanged []BaselinePackage
		for _, current := range after[pkg.Name] {
			if pkg.Arch != "" && current.Arch != "" && pkg.Arch != "all" &&
				current.Arch != "all" && pkg.Arch != current.Arch {
				unchanged = append(unchanged, current)
			}
		}
		after[pkg.Name] = append(unchanged, BaselinePackage{
			Name: pkg.Name, Version: pkg.Version, Arch: pkg.Arch, Provides: pkg.Provides, Installed: true,
			MultiArch: pkg.MultiArch, ProvidedRelations: parseDebProvidedRelations(strings.Join(pkg.ProvidesVer, ",")),
		})
	}
	for _, action := range removals {
		if action.Type == ActionRemove {
			delete(after, action.Package)
		}
	}
	var failures []PlannedAction
	for _, pkg := range baseline {
		if !pkg.Installed || !retainedUnchanged(pkg, after) {
			continue
		}
		for _, requirement := range pkg.Requirements {
			if !retainedRequirementSatisfied(pkg, requirement, before, nativeArch) ||
				retainedRequirementSatisfied(pkg, requirement, after, nativeArch) {
				continue
			}
			failures = append(failures, PlannedAction{
				Requirement: formatDependencyAlternatives(requirement.Alternatives),
				Type:        ActionUnsatisfiedDep, Package: pkg.Name, Arch: pkg.Arch,
				Detail: fmt.Sprintf("retained baseline package %s %s requires %s; proposed candidates: %s; "+
					"a compatible upgrade is required (check repository candidates and explicit pins)",
					pkg.Name, pkg.Version, formatDependencyAlternatives(requirement.Alternatives),
					retainedCandidateSummary(requirement, after)),
			})
		}
	}
	sortActions(failures)
	return failures
}

func retainedPackageIndex(packages []BaselinePackage) map[string][]BaselinePackage {
	index := make(map[string][]BaselinePackage, len(packages))
	for _, pkg := range packages {
		if pkg.Installed {
			index[pkg.Name] = append(index[pkg.Name], pkg)
		}
	}
	return index
}

func retainedUnchanged(pkg BaselinePackage, after map[string][]BaselinePackage) bool {
	for _, candidate := range after[pkg.Name] {
		if candidate.Version == pkg.Version && candidate.Arch == pkg.Arch {
			return true
		}
	}
	return false
}

func retainedRequirementSatisfied(owner BaselinePackage, requirement ArtifactDependency,
	packages map[string][]BaselinePackage,
	nativeArch string,
) bool {
	for _, alternative := range requirement.Alternatives {
		for _, pkg := range packages[alternative.Name] {
			if retainedArchMatches(alternative.Arch, owner, pkg, nativeArch) &&
				retainedVersionSatisfied(alternative.Constraint, pkg.Version) {
				return true
			}
		}
		for _, candidates := range packages {
			for _, pkg := range candidates {
				if retainedArchMatches(alternative.Arch, owner, pkg, nativeArch) && retainedProvides(pkg, alternative) {
					return true
				}
			}
		}
	}
	return false
}

func retainedArchMatches(required string, owner, candidate BaselinePackage, nativeArch string) bool {
	if required == "any" {
		return candidate.MultiArch == "allowed"
	}
	if candidate.Arch == "all" || candidate.Arch == "" {
		return true
	}
	if required == "native" {
		return nativeArch != "" && candidate.Arch == nativeArch
	}
	if required == "" {
		if candidate.MultiArch == "foreign" {
			return true
		}
		required = owner.Arch
		if required == "all" {
			required = nativeArch
		}
		return required == "" || candidate.Arch == required
	}
	return candidate.Arch == required
}

func retainedProvides(pkg BaselinePackage, alternative DependencyAlternative) bool {
	provided := pkg.ProvidedRelations
	if provided == nil {
		provided = parseDebProvidedRelations(strings.Join(pkg.Provides, ","))
	}
	for _, capability := range provided {
		if capability.Name != alternative.Name {
			continue
		}
		if alternative.Constraint == nil || (capability.Constraint != nil &&
			capability.Constraint.Op == "=" && retainedVersionSatisfied(alternative.Constraint, capability.Constraint.Ver)) {
			return true
		}
	}
	return false
}

func retainedVersionSatisfied(constraint *VersionConstraint, version string) bool {
	if constraint == nil {
		return true
	}
	comparison, err := comparePkgVersions(PackageManagerAPT, version, constraint.Ver)
	return err == nil && constraintSatisfied(constraint.Op, comparison)
}

func formatDependencyAlternatives(alternatives []DependencyAlternative) string {
	var terms []string
	for _, alternative := range alternatives {
		term := alternative.Name
		if alternative.Arch != "" {
			term += ":" + alternative.Arch
		}
		if alternative.Constraint != nil {
			term += " (" + alternative.Constraint.Op + " " + alternative.Constraint.Ver + ")"
		}
		terms = append(terms, term)
	}
	return strings.Join(terms, " | ")
}

func retainedUpgradeTargets(failures []PlannedAction, requested []string,
	alreadyForced map[string]bool, arch string,
) []string {
	seen := make(map[string]bool, len(requested)+len(alreadyForced))
	for _, name := range requested {
		seen[requestedBaseName(PackageManagerAPT, name)] = true
	}
	for name := range alreadyForced {
		seen[name] = true
	}
	var targets []string
	for _, failure := range failures {
		if failure.Arch != "" && failure.Arch != "all" && failure.Arch != arch {
			continue
		}
		if !seen[failure.Package] && !isBootloaderPackage(failure.Package) && !isKernelFamilyPackage(failure.Package) {
			seen[failure.Package] = true
			targets = append(targets, failure.Package)
		}
	}
	sort.Strings(targets)
	return targets
}

func retainedCandidateSummary(requirement ArtifactDependency, packages map[string][]BaselinePackage) string {
	var candidates []string
	for _, alternative := range requirement.Alternatives {
		if len(packages[alternative.Name]) == 0 {
			candidates = append(candidates, alternative.Name+" absent")
		}
		for _, pkg := range packages[alternative.Name] {
			candidates = append(candidates, pkg.Name+":"+pkg.Arch+"="+pkg.Version)
		}
	}
	sort.Strings(candidates)
	return strings.Join(candidates, ", ")
}

func retainedPreflightFailures(in PreflightInput, actions []PlannedAction) []PlannedAction {
	failures := retainedDependencyFailures(in.Family, in.Baseline, in.Resolved, actions, in.NativeArch)
	if !in.Policy.AllowPackageRemoval {
		return failures
	}
	upgradeFailures := retainedDependencyFailures(in.Family, in.Baseline, in.Resolved, nil, in.NativeArch)
	var blocking []PlannedAction
	for _, failure := range failures {
		if isBootloaderPackage(failure.Package) || isKernelImagePackage(failure.Package) {
			blocking = append(blocking, failure)
			continue
		}
		for _, upgradeFailure := range upgradeFailures {
			if failure.Package == upgradeFailure.Package && failure.Arch == upgradeFailure.Arch &&
				failure.Requirement == upgradeFailure.Requirement {
				blocking = append(blocking, failure)
				break
			}
		}
	}
	return blocking
}
