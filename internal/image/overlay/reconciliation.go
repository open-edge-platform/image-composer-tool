package overlay

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

// ReconciliationClass describes the evidence for a package in the final inventory.
type ReconciliationClass string

const (
	ReconciliationResolvedAndInstalled ReconciliationClass = "resolved-and-installed"
	ReconciliationBaselineRetained     ReconciliationClass = "baseline-retained"
	ReconciliationScriptInstalled      ReconciliationClass = "script-installed"
	ReconciliationRemoved              ReconciliationClass = "removed"
	ReconciliationOriginUnknown        ReconciliationClass = "origin-unknown"
)

// ReconciledPackage is one package or removal in the final overlay inventory view.
type ReconciledPackage struct {
	Name          string
	Version       string
	Arch          string
	MultiArch     string
	SourceName    string
	SourceVersion string
	RepositoryID  string
	Class         ReconciliationClass
	CoverageGap   bool
}

// FinalInventoryReport compares the baseline and final dpkg inventories against
// the overlay resolution plan. It does not infer repository origin from dpkg
// Source fields or a package that was absent from the resolved plan.
type FinalInventoryReport struct {
	Packages []ReconciledPackage
}

func reconciliationKey(name, version, arch string) string {
	return name + "\x00" + version + "\x00" + arch
}

// ReconcileFinalInventory compares exact name, version, and architecture identities.
// Plan matches establish overlay origin; baseline matches establish retained origin;
// unmatched final packages are script/local additions with an origin coverage gap.
func ReconcileFinalInventory(baseline, final []BaselinePackage, plan *ResolutionPlan) FinalInventoryReport {
	baselineByIdentity := make(map[string]BaselinePackage, len(baseline))
	for _, pkg := range baseline {
		baselineByIdentity[reconciliationKey(pkg.Name, pkg.Version, pkg.Arch)] = pkg
	}

	finalByIdentity := make(map[string]BaselinePackage, len(final))
	for _, pkg := range final {
		if pkg.Installed {
			finalByIdentity[reconciliationKey(pkg.Name, pkg.Version, pkg.Arch)] = pkg
		}
	}

	planByIdentity := make(map[string]ResolvedPackage)
	if plan != nil {
		for _, pkg := range plan.ToInstall {
			planByIdentity[reconciliationKey(pkg.Name, pkg.Version, pkg.Arch)] = pkg
		}
	}

	report := FinalInventoryReport{}
	for key, pkg := range finalByIdentity {
		reconciled := ReconciledPackage{
			Name: pkg.Name, Version: pkg.Version, Arch: pkg.Arch, MultiArch: pkg.MultiArch,
			SourceName: pkg.SourceName, SourceVersion: pkg.SourceVersion,
		}
		if resolved, found := planByIdentity[key]; found {
			reconciled.Class = ReconciliationResolvedAndInstalled
			reconciled.SourceName = resolved.SourceName
			reconciled.SourceVersion = resolved.SourceVersion
			reconciled.RepositoryID = resolved.SourceRepository
		} else if _, found := baselineByIdentity[key]; found {
			reconciled.Class = ReconciliationBaselineRetained
		} else {
			reconciled.Class = ReconciliationScriptInstalled
			reconciled.CoverageGap = true
		}
		report.Packages = append(report.Packages, reconciled)
	}

	for key, pkg := range baselineByIdentity {
		if _, found := finalByIdentity[key]; found {
			continue
		}
		report.Packages = append(report.Packages, ReconciledPackage{
			Name: pkg.Name, Version: pkg.Version, Arch: pkg.Arch, MultiArch: pkg.MultiArch,
			SourceName: pkg.SourceName, SourceVersion: pkg.SourceVersion,
			Class: ReconciliationRemoved,
		})
	}

	// Keep every report deterministic for SBOM and policy consumers.
	sort.Slice(report.Packages, func(i, j int) bool {
		left, right := report.Packages[i], report.Packages[j]
		if left.Class != right.Class {
			return left.Class < right.Class
		}
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		if left.Arch != right.Arch {
			return left.Arch < right.Arch
		}
		if left.Version != right.Version {
			return left.Version < right.Version
		}
		return left.SourceVersion < right.SourceVersion
	})
	return report
}

// OriginUnknownGap returns a class explicitly for coverage-gap reporting.
type SourceProvenancePolicyReport struct {
	Mode       string
	Failures   []string
	Absent     []string
	Unresolved []string
}

func EvaluateSourceProvenancePolicy(policy *config.SourceProvenancePolicy, report FinalInventoryReport) (SourceProvenancePolicyReport, error) {
	result := SourceProvenancePolicyReport{Mode: "advisory"}
	if policy == nil {
		return result, nil
	}
	result.Mode = policy.Mode

	for _, pkg := range report.Packages {
		if pkg.Class == ReconciliationRemoved {
			continue
		}
		resolved := pkg.Class == ReconciliationResolvedAndInstalled && pkg.SourceName != "" && pkg.SourceVersion != "" && pkg.RepositoryID != ""
		if !resolved {
			result.Unresolved = append(result.Unresolved, pkg.Name+"@"+pkg.Version+":"+pkg.Arch)
		}
	}

	switch policy.Mode {
	case "advisory":
		return result, nil
	case "strict-all":
		if len(result.Unresolved) > 0 {
			result.Failures = append(result.Failures, result.Unresolved...)
			return result, fmt.Errorf("strict-all source provenance unresolved for %d package(s)", len(result.Failures))
		}
		return result, nil
	case "strict-critical":
		for _, pkg := range report.Packages {
			if pkg.Class == ReconciliationRemoved {
				continue
			}
			matched := false
			for _, selector := range policy.CriticalPackages {
				matchedName, err := path.Match(strings.TrimSpace(selector), pkg.Name)
				if err == nil && matchedName {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			resolved := pkg.Class == ReconciliationResolvedAndInstalled && pkg.SourceName != "" && pkg.SourceVersion != "" && pkg.RepositoryID != ""
			if !resolved {
				result.Failures = append(result.Failures, pkg.Name+"@"+pkg.Version+":"+pkg.Arch)
			}
		}
		for _, selector := range policy.CriticalPackages {
			found := false
			for _, pkg := range report.Packages {
				if pkg.Class != ReconciliationRemoved {
					if match, err := path.Match(strings.TrimSpace(selector), pkg.Name); err == nil && match {
						found = true
						break
					}
				}
			}
			if !found {
				result.Absent = append(result.Absent, selector)
			}
		}
		if len(result.Failures) > 0 {
			return result, fmt.Errorf("strict-critical source provenance unresolved for %d package(s)", len(result.Failures))
		}
		return result, nil
	default:
		return result, fmt.Errorf("unsupported source provenance policy mode %q", policy.Mode)
	}
}

func (report FinalInventoryReport) OriginUnknownGap() bool {
	for _, pkg := range report.Packages {
		if pkg.CoverageGap && pkg.Class == ReconciliationScriptInstalled {
			return true
		}
	}
	return false
}
