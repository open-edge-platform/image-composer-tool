package overlay

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage/debutils"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage/rpmutils"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/system"
)

// repoTypeDeb and repoTypeRPM are the provider-config repository "type" values
// (config.ProviderRepoConfig.Type) that map to the two package families.
const (
	repoTypeDeb = "deb"
	repoTypeRPM = "rpm"
)

// Repository is a normalized package repository used for overlay dependency
// resolution. It unifies the two on-disk sources — provider default repositories
// (config.ProviderRepoConfig, loaded from providerconfigs/<arch>_repo.yml) and
// user repositories declared in the template (config.PackageRepository) — into a
// single shape the resolver backends consume.
type Repository struct {
	// ID is a stable identifier for the repository, used in diagnostics and as a
	// tie-breaker in the deterministic ordering. It is NOT part of the dedup key:
	// buildRepositorySet de-dupes on Type+URL+Component+Name.
	ID string
	// Name is the repository name; for deb repositories it doubles as the suite
	// codename expected by the metadata fetcher.
	Name string
	// URL is the repository base URL (the directory that contains dists/ for deb
	// or repodata/ for rpm).
	URL string
	// Type is the repository family, "deb" or "rpm".
	Type string
	// Component is the repository component/section (e.g. "main"); deb only.
	Component string
	// Priority orders repositories when the same package appears in several
	// (higher wins). It also drives the deterministic ordering of the set.
	Priority int
	// GPGKey is the GPG key reference (comma-joined for rpm, single URL for deb).
	GPGKey string
	// GPGCheck and RepoGPGCheck mirror the provider-config verification flags.
	GPGCheck     bool
	RepoGPGCheck bool
	// AllowPackages optionally pins the repository to a subset of packages.
	AllowPackages []string
	// Source records where the repository came from ("provider" or "template"),
	// for diagnostics.
	Source string
}

// ResolvedPackage is a single package in the resolved transitive closure.
type ResolvedPackage struct {
	Name    string
	Version string
	Arch    string
	// URL is the artifact download URL, when known.
	URL string
}

// ResolutionPlan is the deterministic output of overlay dependency resolution.
// It is the unit the downstream preflight policy gate consumes: it never mutates
// the baseline, it only describes what would be added.
type ResolutionPlan struct {
	// Requested are the overlay packages requested by the template (sorted).
	Requested []string
	// Seed are the requested packages that are not already present in the
	// baseline, i.e. the packages actually fed into dependency resolution (sorted).
	Seed []string
	// Repositories are the repositories used for resolution, in deterministic order.
	Repositories []Repository
	// Closure is the full transitive dependency closure of Seed (sorted).
	Closure []ResolvedPackage
	// ToInstall are the closure members not already satisfied by the baseline —
	// the packages that must be added (sorted).
	ToInstall []ResolvedPackage
	// AlreadyPresent are the canonical names of requested/closure packages already
	// satisfied by the baseline inventory (sorted, de-duplicated).
	AlreadyPresent []string
	// DownloadDir is the cache directory the artifacts were downloaded into.
	DownloadDir string
	// Artifacts are the downloaded artifact filenames (sorted).
	Artifacts []string
}

// resolveRequest carries everything a resolver backend needs for one resolution.
type resolveRequest struct {
	seed      []string
	repos     []Repository
	userRepos []config.PackageRepository
	arch      string
	dist      string
	destDir   string
	dotFile   string
}

// resolverBackend resolves the transitive closure of a set of seed packages
// against a set of repositories and downloads the required artifacts to a cache
// directory. It is the family-specific (deb/rpm) seam over the existing
// debutils/rpmutils pipelines, kept behind an interface so the deterministic
// orchestration around it is unit-testable without network or root.
type resolverBackend interface {
	family() PackageManager
	resolveAndDownload(req resolveRequest) (closure []ospackage.PackageInfo, artifacts []string, err error)
}

// selectResolverBackend and loadProviderRepoConfig are indirection seams over the
// two impure dependencies of resolution (the network-bound backend and the
// file-backed provider repository config) so the deterministic orchestration in
// ResolveOverlayPackages is unit-testable for both families. Tests override them.
var (
	selectResolverBackend  = selectBackend
	loadProviderRepoConfig = config.LoadProviderRepoConfig
	// clearOverlayCacheDir removes stale artifacts from the overlay download
	// directory before a fresh resolve. It is a seam so tests can observe/skip it.
	clearOverlayCacheDir = purgeOverlayArtifacts
)

// ResolveOverlayPackages resolves the transitive dependency closure for the
// overlay packages requested by the template, using the repositories configured
// for the detected baseline family, and downloads the required artifacts to the
// build cache. It returns a deterministic ResolutionPlan for downstream policy
// evaluation.
//
// It is read-only with respect to the baseline image: it inspects the baseline
// inventory to avoid re-resolving packages that are already installed, but it
// never mutates the baseline's package-manager configuration or database.
func ResolveOverlayPackages(template *config.ImageTemplate, info *BaselineInfo, baseline []BaselinePackage) (*ResolutionPlan, error) {
	if template == nil {
		return nil, fmt.Errorf("overlay resolution: image template cannot be nil")
	}
	if info == nil {
		return nil, fmt.Errorf("overlay resolution: baseline info cannot be nil")
	}

	backend, err := selectResolverBackend(info.PackageManager)
	if err != nil {
		return nil, err
	}

	// The baseline arch is detected from its ELF machine type ("x86_64"/"aarch64"),
	// but deb repositories, the provider repo-config filenames, and .deb artifact
	// names all use the Debian arch spelling ("amd64"/"arm64"). Translate for the
	// deb family so the repo-config path and package metadata resolve; rpm keeps
	// the ELF spelling ("x86_64"/"aarch64"), which is what its repos use.
	arch := packagingArch(info.Arch, info.PackageManager)

	repos, err := loadOverlayRepositories(info.OS, template.Target.Dist, arch, template.GetPackageRepositories(), info.PackageManager)
	if err != nil {
		return nil, err
	}

	requested := overlayRequestedPackages(template)
	present := baselinePresenceSet(baseline)
	allowUpgrade := overlayAllowsUpgrade(template)
	// Additive-only prunes requested packages already present in the baseline so
	// they are never re-resolved. In upgrade mode we must re-resolve them too, so
	// the resolver surfaces the repository's candidate version and the upgrade set
	// can decide whether it is strictly newer (an upgrade to install) or not (a
	// no-op we leave untouched). Absent packages are always seeded.
	seed := overlaySeedPackages(requested, present)
	if allowUpgrade {
		seed = append([]string(nil), requested...)
	}

	destDir, err := overlayCacheDir(info, template.Target.Dist, arch)
	if err != nil {
		return nil, err
	}

	var closure []ospackage.PackageInfo
	var artifacts []string
	if len(seed) == 0 {
		log.Infof("Overlay resolution: all %d requested package(s) are already present in the baseline; nothing to resolve", len(requested))
	} else {
		log.Infof("Overlay resolution: resolving %d package(s) %v against %d %s repositor(ies) [%s]",
			len(seed), seed, len(repos), info.PackageManager, summarizeRepositories(repos))
		// Start from a clean download directory so a superset left by an earlier
		// build with a larger package list cannot be mistaken for this request's
		// closure. The underlying package cache treats "requested packages present"
		// as "cache fresh" and never detects extra artifacts, so on overlay reuse it
		// would return every cached .deb — dragging in packages (e.g. systemd-boot)
		// the current template never asked for. Purging guarantees the closure comes
		// from a real resolve of exactly this seed.
		if err = clearOverlayCacheDir(destDir); err != nil {
			return nil, fmt.Errorf("overlay resolution: failed to clear stale artifact cache %s: %w", destDir, err)
		}
		closure, artifacts, err = backend.resolveAndDownload(resolveRequest{
			seed:      seed,
			repos:     repos,
			userRepos: template.GetPackageRepositories(),
			arch:      arch,
			dist:      template.Target.Dist,
			destDir:   destDir,
			dotFile:   template.DotFilePath,
		})
		if err != nil {
			return nil, fmt.Errorf("overlay dependency resolution failed for package(s) %v using %d %s repositor(ies) [%s]: %w",
				seed, len(repos), info.PackageManager, summarizeRepositories(repos), err)
		}
	}

	// The baseline version index is only consulted to classify upgrades, so build
	// it only in upgrade mode; the additive-only path never reads it.
	var baselineByName map[string]BaselinePackage
	if allowUpgrade {
		baselineByName = baselineVersionIndex(baseline)
	}

	plan := buildResolutionPlan(planInput{
		family:         info.PackageManager,
		requested:      requested,
		seed:           seed,
		repos:          repos,
		closure:        closure,
		artifacts:      artifacts,
		present:        present,
		baselineByName: baselineByName,
		allowUpgrade:   allowUpgrade,
		destDir:        destDir,
	})
	log.Infof("Overlay resolution complete: %d requested, %d in closure, %d to install (%d already present), %d artifact(s) in %s",
		len(plan.Requested), len(plan.Closure), len(plan.ToInstall), len(plan.AlreadyPresent), len(plan.Artifacts), plan.DownloadDir)
	logToInstallProvenance(plan)
	return plan, nil
}

// logToInstallProvenance annotates each to-be-installed package as either
// "requested" (named in the template) or "dependency" (pulled in transitively).
// The install set is the closure of requested packages minus what the baseline
// already satisfies, so it is routinely larger than the template list; without
// this breakdown the extra dependency packages look unexplained when they later
// surface in the image SBOM or a compare diff.
//
// The provenance counts are logged at Info level, but the per-package breakdown
// is logged at Debug: a large dependency closure would otherwise emit hundreds of
// Info lines per overlay, hurting log readability and slowing CI.
func logToInstallProvenance(plan *ResolutionPlan) {
	if len(plan.ToInstall) == 0 {
		return
	}
	// The resolver canonicalizes plan.ToInstall names to the base name (dropping
	// any deb ":arch" multiarch qualifier), but plan.Requested holds the raw
	// template strings, which may be arch-qualified ("gcc:amd64"). Key and look up
	// the requested set on the base name so an arch-qualified request is still
	// classified as "requested" rather than skewing the dependency count.
	requestedSet := make(map[string]bool, len(plan.Requested))
	for _, name := range plan.Requested {
		requestedSet[basePackageName(name)] = true
	}
	requestedCount := 0
	for _, pkg := range plan.ToInstall {
		origin := "dependency"
		if requestedSet[basePackageName(pkg.Name)] {
			origin = "requested"
			requestedCount++
		}
		log.Debugf("  to install [%s]: %s %s", origin, pkg.Name, pkg.Version)
	}
	log.Infof("To-install provenance: %d requested, %d pulled in as dependencies",
		requestedCount, len(plan.ToInstall)-requestedCount)
}

// packagingArch translates a detected baseline architecture (ELF-derived, e.g.
// "x86_64"/"aarch64") into the spelling the package family expects. The deb
// family uses the Debian arch names ("amd64"/"arm64") for repository paths,
// Packages metadata, and .deb filenames; the rpm family and everything else use
// the ELF spelling unchanged. This mirrors the translation the ubuntu/debian
// providers apply in Init for create-mode builds.
func packagingArch(arch string, family PackageManager) string {
	if family != PackageManagerAPT {
		return arch
	}
	switch arch {
	case "x86_64":
		return "amd64"
	case "aarch64":
		return "arm64"
	default:
		return arch
	}
}

// selectBackend returns the resolver backend for a package-manager family.
func selectBackend(family PackageManager) (resolverBackend, error) {
	switch family {
	case PackageManagerAPT:
		return &debResolverBackend{}, nil
	case PackageManagerDNF:
		return &rpmResolverBackend{}, nil
	default:
		return nil, fmt.Errorf("overlay resolution: unsupported package manager %q (expected %q or %q)",
			family, PackageManagerAPT, PackageManagerDNF)
	}
}

// overlayCacheDir returns the dedicated cache directory for overlay artifacts,
// kept separate from the create-mode package cache so the two never collide.
//
// The provider ID is built from the packaging arch (the deb "amd64"/"arm64" or
// rpm "x86_64"/"aarch64" spelling), not the raw ELF-derived info.Arch, so the
// overlay cache namespace matches the provider repo-config naming and the other
// GetProviderId-based cache keys instead of introducing a surprising extra one.
func overlayCacheDir(info *BaselineInfo, dist, packagingArch string) (string, error) {
	// Validate every component that flows into the joined cache path. The overlay
	// ingestor already checks Target.{OS,Dist,Arch}, but ResolveOverlayPackages can
	// be called independently on a programmatically built template, so re-checking
	// here keeps a value carrying a path separator or ".." from escaping the cache
	// root once providerID is passed to filepath.Join (path traversal).
	for _, part := range []struct{ name, value string }{
		{"baseline os", info.OS},
		{"target dist", dist},
		{"packaging arch", packagingArch},
	} {
		if err := validatePathSegment(part.value); err != nil {
			return "", fmt.Errorf("overlay resolution: invalid %s %q for cache path: %w", part.name, part.value, err)
		}
	}
	cacheRoot, err := config.CacheDir()
	if err != nil {
		return "", fmt.Errorf("overlay resolution: failed to resolve cache directory: %w", err)
	}
	providerID := system.GetProviderId(info.OS, dist, packagingArch)
	return filepath.Join(cacheRoot, "pkgCache", providerID, "overlay"), nil
}

// purgeOverlayArtifacts removes previously downloaded package artifacts from the
// overlay download directory so the next resolve starts clean. It removes every
// regular (non-directory) file directly under destDir — this is the directory the
// artifacts are downloaded into, so in practice these are the .deb/.rpm files —
// while leaving subdirectories and the directory itself in place. It does not
// touch the deb/rpm repository-metadata caches: those are persisted elsewhere by
// the debutils/rpmutils pipelines, outside destDir, and are not reset here. A
// missing directory is not an error — there is simply nothing to purge.
func purgeOverlayArtifacts(destDir string) error {
	if strings.TrimSpace(destDir) == "" {
		return nil
	}
	entries, err := os.ReadDir(destDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading overlay cache directory: %w", err)
	}

	removed := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if err := os.Remove(filepath.Join(destDir, e.Name())); err != nil {
			if os.IsNotExist(err) {
				// File vanished between ReadDir and Remove: nothing was removed here,
				// so don't count it toward the "cleared N file(s)" tally.
				continue
			}
			return fmt.Errorf("removing stale artifact %s: %w", e.Name(), err)
		}
		removed++
	}
	if removed > 0 {
		log.Infof("Overlay resolution: cleared %d stale file(s) from %s before resolving", removed, destDir)
	}
	return nil
}

// overlayRequestedPackages returns the additive overlay package set from the
// template: the explicit system packages, trimmed, de-duplicated, and sorted.
// Overlay mode is additive-only, so kernel and bootloader package lists are
// deliberately excluded — they belong to the baseline and must not be touched.
func overlayRequestedPackages(template *config.ImageTemplate) []string {
	var pkgs []string
	for _, p := range template.SystemConfig.Packages {
		if p = strings.TrimSpace(p); p != "" {
			pkgs = append(pkgs, p)
		}
	}
	pkgs = dedupeStrings(pkgs)
	sort.Strings(pkgs)
	return pkgs
}

// baselinePresenceSet builds the set of capability names already satisfied by
// the installed baseline packages: each installed package's name plus everything
// it Provides. It is used to prune packages that need not be resolved/installed.
func baselinePresenceSet(baseline []BaselinePackage) map[string]bool {
	present := map[string]bool{}
	for _, p := range baseline {
		if !p.Installed {
			continue
		}
		if p.Name != "" {
			present[p.Name] = true
		}
		for _, prov := range p.Provides {
			if prov != "" {
				present[prov] = true
			}
		}
	}
	return present
}

// overlaySeedPackages returns the requested packages that are not already present
// in the baseline. The input is assumed sorted/de-duplicated; the output preserves
// that order so resolution is deterministic.
//
// The baseline presence set is keyed by canonical base name, but a template request
// may carry an APT ":arch" multiarch qualifier ("gcc:amd64"). Presence is therefore
// tested on the base name so an arch-qualified request for an already-installed
// package is recognized as satisfied; the original request token is preserved in the
// seed slice so the resolver still receives the arch qualifier when it is not.
func overlaySeedPackages(requested []string, present map[string]bool) []string {
	var seed []string
	for _, p := range requested {
		if !present[basePackageName(p)] {
			seed = append(seed, p)
		}
	}
	return seed
}

// planInput carries the resolver output and the baseline state needed to
// assemble a ResolutionPlan. It is a struct rather than a long positional
// argument list so the upgrade-classification inputs (baseline versions, the
// upgrade opt-in, the family comparator) read clearly at the call site.
type planInput struct {
	family         PackageManager
	requested      []string
	seed           []string
	repos          []Repository
	closure        []ospackage.PackageInfo
	artifacts      []string
	present        map[string]bool
	baselineByName map[string]BaselinePackage
	allowUpgrade   bool
	destDir        string
}

// upgradeSet returns the bounded set of baseline-present package names this plan
// is permitted to upgrade. It is empty unless the overlay opted into upgrades.
func (in planInput) upgradeSet() map[string]bool {
	if !in.allowUpgrade {
		return nil
	}
	return upgradeEligibleNames(in.family, in.requested, in.closure, in.present, in.baselineByName)
}

// overlayAllowsUpgrade reports whether the template's overlay policy permits
// upgrading baseline packages. The policy's AllowUpgrade gate is derived from
// packageOperation during validation; an absent policy defaults to additive-only.
func overlayAllowsUpgrade(template *config.ImageTemplate) bool {
	return template.OverlayPolicy != nil && template.OverlayPolicy.AllowUpgrade
}

// isNewerThanBaseline reports whether version is strictly newer than the copy of
// name installed in the baseline. A package absent from the baseline index, or
// whose versions cannot be compared, is NOT newer (the caller handles the absent
// case as a plain add; an uncomparable version is left to the baseline so
// resolution never forces an ambiguous replacement).
func isNewerThanBaseline(family PackageManager, name, version string, baselineByName map[string]BaselinePackage) bool {
	base, ok := baselineByName[name]
	if !ok {
		return false
	}
	cmp, err := comparePkgVersions(family, version, base.Version)
	if err != nil {
		return false
	}
	return cmp > 0
}

// resolvedNameSet returns the set of package names in a resolved slice.
func resolvedNameSet(pkgs []ResolvedPackage) map[string]bool {
	set := make(map[string]bool, len(pkgs))
	for _, p := range pkgs {
		set[p.Name] = true
	}
	return set
}

// upgradeEligibleNames computes which baseline-present packages an
// additive-and-upgrade overlay is allowed to upgrade. Upgrade scope is
// deliberately bounded so opting into upgrades does not silently replace core
// baseline libraries: only two classes are eligible.
//
//  1. Requested-and-present: a package named in the template that the baseline
//     already has at an older version.
//  2. Required transitive dependency: a package a to-install package depends on
//     via a single-alternative, version-constrained edge that the baseline copy
//     does NOT satisfy but the resolved (newer) copy does. This is the case
//     additive-only could not resolve — the install would fail at configure time
//     otherwise — so it is upgraded to keep the requested set installable.
//
// Every other present closure member (merely newer in the repo, or required only
// through a multi-alternative edge that might be satisfiable another way) is left
// on its baseline version. A genuinely unsatisfiable pin that this scoping does
// not upgrade is caught fail-closed by preflight's unsatisfied-dependency check.
//
// The eligible set is grown to a fixpoint so an upgraded dependency's own
// required upgrades are also pulled in.
func upgradeEligibleNames(family PackageManager, requested []string, closure []ospackage.PackageInfo, present map[string]bool, baselineByName map[string]BaselinePackage) map[string]bool {
	closureByName := make(map[string]ospackage.PackageInfo, len(closure))
	for _, p := range closure {
		closureByName[canonicalPackageName(p)] = p
	}
	// The resolver canonicalizes closure names to the base name (dropping any deb
	// ":arch" multiarch qualifier), but the template request strings may be arch-
	// qualified ("gcc:amd64"). Key the requested set on the base name so an arch-
	// qualified request is still recognized as requested-and-present below and is
	// eligible for upgrade.
	requestedSet := make(map[string]bool, len(requested))
	for _, r := range requested {
		requestedSet[basePackageName(r)] = true
	}

	eligible := map[string]bool{}  // present baseline names approved for upgrade
	installed := map[string]bool{} // names being installed: adds + eligible upgrades
	var work []string
	enqueue := func(name string) {
		if installed[name] {
			return
		}
		installed[name] = true
		work = append(work, name)
	}

	// Seed: every add (absent from the baseline) plus every requested-and-present
	// package whose resolved version is strictly newer than the baseline's.
	for _, p := range closure {
		name := canonicalPackageName(p)
		if !present[name] {
			enqueue(name)
			continue
		}
		if requestedSet[basePackageName(name)] && isNewerThanBaseline(family, name, p.Version, baselineByName) {
			eligible[name] = true
			enqueue(name)
		}
	}

	// Fixpoint: a package being installed can require a newer version of a present
	// baseline dependency; upgrade that dependency (which may in turn require more).
	for len(work) > 0 {
		cur := work[0]
		work = work[1:]
		pi, ok := closureByName[cur]
		if !ok {
			continue
		}
		for _, edge := range directVersionedDeps(family, pi) {
			dep := edge.Name
			if !present[dep] || eligible[dep] {
				continue
			}
			base, ok := baselineByName[dep]
			if !ok {
				continue
			}
			// Skip when the baseline copy already satisfies the pin (no upgrade
			// needed) or the comparison is undeterminable.
			if cmp, err := comparePkgVersions(family, base.Version, edge.Constraint.Ver); err != nil || constraintSatisfied(edge.Constraint.Op, cmp) {
				continue
			}
			// Baseline fails the pin: upgrade only if the resolved copy is newer
			// (and thus can satisfy it); otherwise leave it for preflight to flag.
			dpi, ok := closureByName[dep]
			if !ok || !isNewerThanBaseline(family, dep, dpi.Version, baselineByName) {
				continue
			}
			eligible[dep] = true
			enqueue(dep)
		}
	}
	return eligible
}

// directVersionedDeps returns the single-alternative, version-constrained
// dependency edges a resolved package declares, parsed from its family-specific
// dependency metadata. Multi-alternative edges ("a | b") are skipped: forcing an
// upgrade to satisfy one branch could be wrong when another branch is already
// met, so those are left to preflight's unsatisfied-dependency check. Unversioned
// edges carry no upgrade signal and are dropped.
func directVersionedDeps(family PackageManager, pi ospackage.PackageInfo) []DependencyAlternative {
	var out []DependencyAlternative
	if family == PackageManagerDNF {
		for _, entry := range pi.RequiresVer {
			if alt, ok := parseRPMRequiresVerEntry(entry); ok {
				out = append(out, alt)
			}
		}
		return out
	}
	// deb: RequiresVer holds the individual Depends terms; parse them back into
	// edges and keep only the unambiguous single-alternative versioned ones.
	for _, edge := range parseDebDependsField(canonicalPackageName(pi), strings.Join(pi.RequiresVer, ",")) {
		if len(edge.Alternatives) == 1 && edge.Alternatives[0].Constraint != nil {
			out = append(out, edge.Alternatives[0])
		}
	}
	return out
}

// parseRPMRequiresVerEntry parses one rpm RequiresVer entry into a versioned
// dependency alternative. The resolver records these as "name (op ver)",
// "name op ver", or a bare "name"; only the version-constrained forms yield an
// edge (ok=false for bare names or unparseable input).
func parseRPMRequiresVerEntry(entry string) (DependencyAlternative, bool) {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return DependencyAlternative{}, false
	}
	if open := strings.Index(entry, "("); open != -1 {
		name := strings.TrimSpace(entry[:open])
		if closeIdx := strings.Index(entry[open:], ")"); closeIdx != -1 && name != "" {
			if c, ok := parseConstraint(entry[open+1 : open+closeIdx]); ok {
				return DependencyAlternative{Name: name, Constraint: &c}, true
			}
		}
		return DependencyAlternative{}, false
	}
	if fields := strings.Fields(entry); len(fields) == 3 {
		if c, ok := parseConstraint(fields[1] + " " + fields[2]); ok {
			return DependencyAlternative{Name: fields[0], Constraint: &c}, true
		}
	}
	return DependencyAlternative{}, false
}

// buildResolutionPlan assembles the deterministic ResolutionPlan from the resolver
// output. All slices are sorted so the same template and repository state always
// produce byte-identical plans.
//
// A closure member already present in the baseline is normally left untouched
// (additive-only). When in.allowUpgrade is set, a present package is routed into
// ToInstall as an upgrade only when it is in the bounded upgrade set (see
// upgradeEligibleNames): a requested-and-present package, or a required
// transitive dependency whose baseline copy fails a versioned pin. Everything
// else — including a present package merely newer in the repo — keeps its
// baseline version (a same-or-older version is never a downgrade).
func buildResolutionPlan(in planInput) *ResolutionPlan {
	requested, present, destDir := in.requested, in.present, in.destDir
	upgradeSet := in.upgradeSet()
	resolved := make([]ResolvedPackage, 0, len(in.closure))
	alreadyPresent := map[string]bool{}
	var toInstall []ResolvedPackage

	for _, p := range in.closure {
		name := canonicalPackageName(p)
		rp := ResolvedPackage{Name: name, Version: p.Version, Arch: p.Arch, URL: p.URL}
		resolved = append(resolved, rp)
		if !present[name] {
			toInstall = append(toInstall, rp)
			continue
		}
		// Present in the baseline. Install it only when it is in the approved
		// upgrade set; otherwise the baseline copy stands.
		if upgradeSet[name] {
			toInstall = append(toInstall, rp)
		} else {
			alreadyPresent[name] = true
		}
	}

	// Requested packages already satisfied by the baseline are "already present"
	// too, even when they never entered the closure (they were never seeded). In
	// upgrade mode a present-and-upgraded requested package is in toInstall, so it
	// is excluded here to avoid labelling it both installed and already-present.
	//
	// present/toInstallNames are keyed by canonical base name, so an arch-qualified
	// request ("gcc:amd64") is normalized with basePackageName before the lookups
	// (and recorded under the base name) — otherwise the qualifier mismatch would
	// miss an already-present requested package and mislabel it.
	toInstallNames := resolvedNameSet(toInstall)
	for _, r := range requested {
		base := basePackageName(r)
		if present[base] && !toInstallNames[base] {
			alreadyPresent[base] = true
		}
	}

	sortResolved(resolved)
	sortResolved(toInstall)

	sortedArtifacts := append([]string(nil), in.artifacts...)
	sort.Strings(sortedArtifacts)

	presentNames := make([]string, 0, len(alreadyPresent))
	for name := range alreadyPresent {
		presentNames = append(presentNames, name)
	}
	sort.Strings(presentNames)

	return &ResolutionPlan{
		Requested:      append([]string(nil), requested...),
		Seed:           append([]string(nil), in.seed...),
		Repositories:   in.repos,
		Closure:        resolved,
		ToInstall:      toInstall,
		AlreadyPresent: presentNames,
		DownloadDir:    destDir,
		Artifacts:      sortedArtifacts,
	}
}

// canonicalPackageName returns the canonical package name for a resolved package,
// preferring the parsed package name over the (possibly path/prefix-bearing) file
// name field.
func canonicalPackageName(p ospackage.PackageInfo) string {
	if strings.TrimSpace(p.PkgName) != "" {
		return p.PkgName
	}
	return p.Name
}

// basePackageName strips a deb ":arch" multiarch qualifier from a package name
// ("gcc:amd64" -> "gcc"), leaving unqualified names unchanged. It reconciles the
// raw template request strings (which may carry an arch suffix) against the
// resolver's canonicalized base names.
func basePackageName(name string) string {
	if colon := strings.Index(name, ":"); colon != -1 {
		return name[:colon]
	}
	return name
}

// sortResolved orders resolved packages by name, then version, then arch.
func sortResolved(pkgs []ResolvedPackage) {
	sort.Slice(pkgs, func(i, j int) bool {
		if pkgs[i].Name != pkgs[j].Name {
			return pkgs[i].Name < pkgs[j].Name
		}
		if pkgs[i].Version != pkgs[j].Version {
			return pkgs[i].Version < pkgs[j].Version
		}
		return pkgs[i].Arch < pkgs[j].Arch
	})
}

// loadOverlayRepositories loads the provider default repositories and merges them
// with the template's user repositories, filtered to the baseline family. It
// fails when no repository of the right family is available, since dependency
// resolution then has nothing to resolve against.
func loadOverlayRepositories(osName, dist, arch string, userRepos []config.PackageRepository, family PackageManager) ([]Repository, error) {
	providerRepos, err := loadProviderRepoConfig(osName, dist, arch)
	if err != nil {
		// Provider defaults are optional when the template supplies its own
		// repositories; surface the failure but continue so user repos still work.
		if len(userRepos) == 0 {
			return nil, fmt.Errorf("overlay resolution: failed to load provider repositories for os=%s dist=%s arch=%s and no template repositories are configured: %w",
				osName, dist, arch, err)
		}
		log.Warnf("Overlay resolution: failed to load provider repositories (continuing with template repositories only): %v", err)
		providerRepos = nil
	}

	repos := buildRepositorySet(providerRepos, userRepos, family, arch)
	if len(repos) == 0 {
		return nil, fmt.Errorf("overlay resolution: no %s repositories configured for os=%s dist=%s arch=%s (checked %d provider and %d template repositories)",
			family, osName, dist, arch, len(providerRepos), len(userRepos))
	}
	return repos, nil
}

// buildRepositorySet normalizes provider and template repositories into a single
// deterministically-ordered set, keeping only those matching the baseline family.
// It is pure (no I/O) so both family paths are unit-testable.
func buildRepositorySet(providerRepos []config.ProviderRepoConfig, userRepos []config.PackageRepository, family PackageManager, arch string) []Repository {
	wantType := familyRepoType(family)
	seen := map[string]bool{}
	var repos []Repository

	add := func(r Repository) {
		if r.Type != wantType || strings.TrimSpace(r.URL) == "" {
			return
		}
		// Include Name in the key: for deb repositories Name is the suite/codename
		// (e.g. "noble" vs "noble-updates" vs "noble-security"), and distinct suites
		// sharing the same baseURL+component are separate metadata sources that must
		// not be collapsed — dropping them would silently discard the updates/security
		// repos. For rpm, Name is a stable per-repo identifier, so including it is
		// harmless (identical repos carry identical names).
		key := r.Type + "\x00" + r.URL + "\x00" + r.Component + "\x00" + r.Name
		if seen[key] {
			return
		}
		seen[key] = true
		repos = append(repos, r)
	}

	for i := range providerRepos {
		prc := providerRepos[i]
		if !prc.Enabled {
			continue
		}
		repoType, name, url, gpgKey, component, _, _, _, _, baseURL, gpgCheck, repoGPGCheck, _ := prc.ToRepoConfigData(arch)
		// deb resolution works from the repository base; rpm from the resolved URL.
		repoURL := baseURL
		if repoType == repoTypeRPM {
			repoURL = url
		}
		add(Repository{
			ID:           fmt.Sprintf("provider-%s-%d", name, i+1),
			Name:         name,
			URL:          repoURL,
			Type:         repoType,
			Component:    component,
			Priority:     500, // standard provider priority
			GPGKey:       gpgKey,
			GPGCheck:     gpgCheck,
			RepoGPGCheck: repoGPGCheck,
			Source:       "provider",
		})
	}

	for _, ur := range userRepos {
		url := strings.TrimSpace(ur.URL)
		if url == "" || url == "<URL>" {
			continue // placeholder or local-only repository: not resolvable here
		}
		id := ur.ID
		if id == "" {
			id = "user-" + ur.Codename
		}
		add(Repository{
			ID:            id,
			Name:          ur.Codename,
			URL:           url,
			Type:          wantType, // user repos apply to the baseline family
			Component:     ur.Component,
			Priority:      ur.Priority,
			GPGKey:        ur.PKey,
			GPGCheck:      true,
			RepoGPGCheck:  true,
			AllowPackages: ur.AllowPackages,
			Source:        "template",
		})
	}

	sortRepositories(repos)
	return repos
}

// familyRepoType maps a package-manager family to its provider-config repo type.
func familyRepoType(family PackageManager) string {
	if family == PackageManagerDNF {
		return repoTypeRPM
	}
	return repoTypeDeb
}

// sortRepositories orders repositories deterministically: highest priority first,
// then by ID, URL, and component to fully break ties.
func sortRepositories(repos []Repository) {
	sort.Slice(repos, func(i, j int) bool {
		if repos[i].Priority != repos[j].Priority {
			return repos[i].Priority > repos[j].Priority
		}
		if repos[i].ID != repos[j].ID {
			return repos[i].ID < repos[j].ID
		}
		if repos[i].URL != repos[j].URL {
			return repos[i].URL < repos[j].URL
		}
		return repos[i].Component < repos[j].Component
	})
}

// summarizeRepositories renders a compact, deterministic repository summary for
// diagnostics (name and URL of each repository).
func summarizeRepositories(repos []Repository) string {
	parts := make([]string, 0, len(repos))
	for _, r := range repos {
		parts = append(parts, fmt.Sprintf("%s=%s", r.Name, r.URL))
	}
	return strings.Join(parts, ", ")
}

// debResolverBackend resolves and downloads deb-family overlay packages by
// reusing the debutils pipeline that create-mode builds use.
type debResolverBackend struct{}

func (b *debResolverBackend) family() PackageManager { return PackageManagerAPT }

func (b *debResolverBackend) resolveAndDownload(req resolveRequest) ([]ospackage.PackageInfo, []string, error) {
	repoList := make([]debutils.Repository, 0, len(req.repos))
	for _, r := range req.repos {
		repoList = append(repoList, debutils.Repository{
			ID:            r.ID,
			Codename:      r.Name,
			URL:           r.URL,
			PKey:          r.GPGKey,
			Component:     r.Component,
			Priority:      r.Priority,
			AllowPackages: r.AllowPackages,
		})
	}

	repoCfgs, err := debutils.BuildRepoConfigs(repoList, req.arch)
	if err != nil {
		return nil, nil, fmt.Errorf("building deb repository configurations: %w", err)
	}
	if len(repoCfgs) == 0 {
		return nil, nil, fmt.Errorf("no usable deb repository configurations after metadata discovery")
	}

	// The debutils pipeline is driven by package-level globals. Snapshot and restore
	// them around this resolve so overlay repo/user-repo state does not leak into
	// any later in-process debutils caller (e.g. a create-mode path). This only
	// prevents cross-call leakage after return; the globals are still shared mutable
	// state, so it is NOT goroutine-safe against a concurrent debutils user. Overlay
	// resolution runs as a single-threaded preprocess step, so that is sufficient
	// here; a package-level mutex would be needed to make debutils truly concurrent.
	defer func(cfgs []debutils.RepoConfig, cfg debutils.RepoConfig, gz, arch string, user []config.PackageRepository) {
		debutils.RepoCfgs = cfgs
		debutils.RepoCfg = cfg
		debutils.GzHref = gz
		debutils.Architecture = arch
		debutils.UserRepo = user
	}(debutils.RepoCfgs, debutils.RepoCfg, debutils.GzHref, debutils.Architecture, debutils.UserRepo)

	debutils.RepoCfgs = repoCfgs
	debutils.RepoCfg = repoCfgs[0]
	debutils.GzHref = repoCfgs[0].PkgList
	debutils.Architecture = repoCfgs[0].Arch
	debutils.UserRepo = req.userRepos

	artifacts, closure, err := debutils.DownloadPackagesComplete(req.seed, req.destDir, req.dotFile, nil, false)
	if err != nil {
		return nil, nil, err
	}
	return closure, artifacts, nil
}

// rpmResolverBackend resolves and downloads rpm-family overlay packages by
// reusing the rpmutils pipeline that create-mode builds use.
type rpmResolverBackend struct{}

func (b *rpmResolverBackend) family() PackageManager { return PackageManagerDNF }

func (b *rpmResolverBackend) resolveAndDownload(req resolveRequest) ([]ospackage.PackageInfo, []string, error) {
	primary := req.repos[0]
	// Trim any trailing slash before joining so a repo URL like "https://r/os/"
	// yields ".../os/repodata/repomd.xml", not a double-slash ".../os//repodata/..."
	// that some servers treat as a distinct (often 404-ing) path.
	primaryURL := strings.TrimRight(primary.URL, "/")
	href, err := rpmutils.FetchPrimaryURL(primaryURL + "/repodata/repomd.xml")
	if err != nil {
		return nil, nil, fmt.Errorf("fetching rpm repository metadata from %s: %w", primaryURL, err)
	}

	// The rpmutils pipeline is driven by package-level globals. Snapshot and restore
	// them around this resolve so overlay repo/user-repo state does not leak into any
	// later in-process rpmutils caller. As with the deb backend, this only prevents
	// cross-call leakage after return and is NOT goroutine-safe against a concurrent
	// rpmutils user; overlay resolution runs single-threaded in preprocess, so that
	// is sufficient (a package-level mutex would be needed for true concurrency).
	defer func(cfg rpmutils.RepoConfig, gz, dist string, user []config.PackageRepository) {
		rpmutils.RepoCfg = cfg
		rpmutils.GzHref = gz
		rpmutils.Dist = dist
		rpmutils.UserRepo = user
	}(rpmutils.RepoCfg, rpmutils.GzHref, rpmutils.Dist, rpmutils.UserRepo)

	rpmutils.RepoCfg = rpmutils.RepoConfig{
		Name:         primary.Name,
		URL:          primaryURL,
		GPGKey:       primary.GPGKey,
		Section:      primary.Component,
		GPGCheck:     primary.GPGCheck,
		RepoGPGCheck: primary.RepoGPGCheck,
		Enabled:      true,
	}
	rpmutils.GzHref = href
	rpmutils.Dist = req.dist
	// The rpm pipeline resolves against the single primary RepoCfg plus every entry
	// in UserRepo. Feed the remaining normalized repositories (repos[1:]) in as
	// additional UserRepo entries so packages that exist only in a secondary
	// provider repo are still discovered instead of being silently dropped.
	rpmutils.UserRepo = rpmResolveUserRepos(req.repos[1:], req.userRepos)

	artifacts, closure, err := rpmutils.DownloadPackagesComplete(req.seed, req.destDir, req.dotFile, nil, false)
	if err != nil {
		return nil, nil, err
	}
	return closure, artifacts, nil
}

// rpmResolveUserRepos combines the template's user repositories with the secondary
// normalized repositories so the rpm pipeline resolves against the full repository
// set (its RepoCfg holds only the primary). The template's own repositories are
// listed first and kept verbatim — they carry fields (PKeys, Path) the normalized
// Repository shape does not — and secondary repositories are appended, deduped by
// URL so a repo present in both sets is fetched only once.
func rpmResolveUserRepos(secondary []Repository, userRepos []config.PackageRepository) []config.PackageRepository {
	combined := make([]config.PackageRepository, 0, len(userRepos)+len(secondary))
	seen := map[string]bool{}
	for _, ur := range userRepos {
		combined = append(combined, ur)
		if url := strings.TrimSpace(ur.URL); url != "" && url != "<URL>" {
			seen[url] = true
		}
	}
	for _, r := range secondary {
		url := strings.TrimSpace(r.URL)
		if url == "" || seen[url] {
			continue
		}
		seen[url] = true
		combined = append(combined, config.PackageRepository{
			ID:            r.ID,
			Codename:      r.Name,
			URL:           r.URL,
			PKey:          r.GPGKey,
			Component:     r.Component,
			Priority:      r.Priority,
			AllowPackages: r.AllowPackages,
		})
	}
	return combined
}
