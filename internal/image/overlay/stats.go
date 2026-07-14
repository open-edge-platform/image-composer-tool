// Package overlay provides statistics tracking for overlay image builds.
//
// After an overlay build completes, package statistics are automatically computed
// and displayed, categorizing packages into three groups:
//
//  1. Unchanged: Baseline packages that were not modified by the overlay
//  2. Added: New packages installed by the overlay (not present in baseline)
//  3. Upgraded: Baseline packages upgraded to newer versions
//
// The statistics are computed by comparing the baseline package inventory
// (detected during the Inspect Baseline stage) with the packages in the
// resolution plan's ToInstall set. In overlay mode with additive-and-upgrade
// policy, packages can be both newly added or upgraded from the baseline.
//
// When the baseline image contains an SBOM, it is used to determine the initial
// package inventory. If no SBOM exists in the baseline, the package database
// (/var/lib/dpkg/status for apt, /var/lib/rpm for dnf) is parsed on the fly
// during baseline inspection to build the inventory.
package overlay

import (
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"
)

// PackageStats captures the package changes made by an overlay build.
type PackageStats struct {
	// TotalBaseline is the count of installed packages in the baseline image.
	TotalBaseline int
	// Unchanged are baseline packages not modified by the overlay (sorted).
	Unchanged []string
	// Added are packages newly installed by the overlay (not present in baseline, sorted).
	Added []PackageAdd
	// Upgraded are baseline packages upgraded to a newer version (sorted).
	Upgraded []PackageUpgrade
}

// PackageAdd describes one package that was newly added by the overlay.
type PackageAdd struct {
	Name    string
	Version string
}

// PackageUpgrade describes one package that was upgraded from the baseline version.
type PackageUpgrade struct {
	Name            string
	BaselineVersion string
	NewVersion      string
}

// ComputePackageStats analyzes the baseline inventory and resolution plan to
// categorize packages as unchanged, newly added, or upgraded.
//
//   - Unchanged: baseline packages not touched by the overlay, plus packages in
//     ToInstall whose resolved version equals the baseline version (no-op reinstall)
//   - Added: packages in ToInstall that were not present in the baseline
//   - Upgraded: packages in ToInstall that existed in baseline at a different version
func ComputePackageStats(baseline []BaselinePackage, plan *ResolutionPlan) *PackageStats {
	if plan == nil {
		return &PackageStats{}
	}

	// Build baseline index: name -> package for installed packages only
	baselineByName := make(map[string]BaselinePackage)
	for _, p := range baseline {
		if p.Installed {
			baselineByName[p.Name] = p
		}
	}

	stats := &PackageStats{
		TotalBaseline: len(baselineByName),
		Unchanged:     []string{},
		Added:         []PackageAdd{},
		Upgraded:      []PackageUpgrade{},
	}

	// Categorize packages in ToInstall
	toInstallSet := make(map[string]ResolvedPackage)
	for _, pkg := range plan.ToInstall {
		toInstallSet[pkg.Name] = pkg
	}

	for _, pkg := range plan.ToInstall {
		basePkg, existedInBaseline := baselineByName[pkg.Name]
		switch {
		case !existedInBaseline:
			// Package is new, not in baseline
			stats.Added = append(stats.Added, PackageAdd{
				Name:    pkg.Name,
				Version: pkg.Version,
			})
		case basePkg.Version == pkg.Version:
			// Package is in ToInstall at the same version it already has in the
			// baseline: a no-op reinstall, not an upgrade. Count it as unchanged.
			stats.Unchanged = append(stats.Unchanged, pkg.Name)
		default:
			// Package existed in baseline at a different version - it's an upgrade
			stats.Upgraded = append(stats.Upgraded, PackageUpgrade{
				Name:            pkg.Name,
				BaselineVersion: basePkg.Version,
				NewVersion:      pkg.Version,
			})
		}
	}

	// Identify unchanged packages: in baseline but not in ToInstall
	for name := range baselineByName {
		if _, installed := toInstallSet[name]; !installed {
			stats.Unchanged = append(stats.Unchanged, name)
		}
	}

	// Sort all categories for deterministic output
	sort.Strings(stats.Unchanged)
	sort.Slice(stats.Added, func(i, j int) bool {
		return stats.Added[i].Name < stats.Added[j].Name
	})
	sort.Slice(stats.Upgraded, func(i, j int) bool {
		return stats.Upgraded[i].Name < stats.Upgraded[j].Name
	})

	return stats
}

// PrintPackageStats renders the package statistics as a formatted report.
func PrintPackageStats(stats *PackageStats) {
	if stats == nil {
		return
	}

	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString("═══════════════════════════════════════════════════════════════\n")
	sb.WriteString("                    OVERLAY PACKAGE STATISTICS                  \n")
	sb.WriteString("═══════════════════════════════════════════════════════════════\n")
	sb.WriteString("\n")

	// Summary counts table
	summary := tabwriter.NewWriter(&sb, 0, 0, 2, ' ', 0)
	fmt.Fprintln(summary, "CATEGORY\tCOUNT")
	fmt.Fprintf(summary, "Total baseline packages\t%d\n", stats.TotalBaseline)
	fmt.Fprintf(summary, "Unchanged\t%d\n", len(stats.Unchanged))
	fmt.Fprintf(summary, "Added\t%d\n", len(stats.Added))
	fmt.Fprintf(summary, "Upgraded\t%d\n", len(stats.Upgraded))
	_ = summary.Flush()
	sb.WriteString("\n")

	// Single detail table grouped by change type. The CHANGE column leads so
	// rows cluster into an "added" group followed by an "upgraded" group. Added
	// packages have no baseline version (shown as "-"); upgraded packages show
	// the baseline -> new version transition.
	if len(stats.Added) > 0 || len(stats.Upgraded) > 0 {
		sb.WriteString("━━━ Package Changes ━━━\n")
		details := tabwriter.NewWriter(&sb, 0, 0, 2, ' ', 0)
		fmt.Fprintln(details, "CHANGE\tPACKAGE\tFROM\tTO")
		for _, pkg := range stats.Added {
			fmt.Fprintf(details, "added\t%s\t%s\t%s\n", pkg.Name, "-", pkg.Version)
		}
		for _, upg := range stats.Upgraded {
			fmt.Fprintf(details, "upgraded\t%s\t%s\t%s\n", upg.Name, upg.BaselineVersion, upg.NewVersion)
		}
		_ = details.Flush()
		sb.WriteString("\n")
	}

	// Note: We don't print the full unchanged list as it can be very long.
	if len(stats.Unchanged) > 0 {
		sb.WriteString("━━━ Unchanged Baseline Packages ━━━\n")
		sb.WriteString(fmt.Sprintf("  %d packages from the baseline remain unchanged.\n",
			len(stats.Unchanged)))
		sb.WriteString("  (Use debug mode to see the full list)\n")
		sb.WriteString("\n")
	}

	sb.WriteString("═══════════════════════════════════════════════════════════════\n")

	log.Info(sb.String())
}

// PrintVerboseUnchangedPackages prints the full list of unchanged packages,
// intended for verbose/debug output.
func PrintVerboseUnchangedPackages(stats *PackageStats) {
	if stats == nil || len(stats.Unchanged) == 0 {
		return
	}

	var sb strings.Builder
	sb.WriteString("\n━━━ Unchanged Baseline Packages (Full List) ━━━\n")
	for _, name := range stats.Unchanged {
		sb.WriteString(fmt.Sprintf("  - %s\n", name))
	}
	sb.WriteString("\n")

	log.Info(sb.String())
}
