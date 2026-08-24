package overlay

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/mount"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

// chrootArtifactDir is the in-chroot mount point the prepared overlay artifact
// cache is bind-mounted at. It lives under /run so it never collides with a
// baseline package path and is conventionally tmpfs-like (transient).
const chrootArtifactDir = "/run/overlay-pkgs"

// maxDpkgArgBytes and chunkArgs live in chunk.go.

// InstallResult records what the install step did, for logging and verification.
type InstallResult struct {
	// Installed are the package names confirmed present in the baseline package
	// database after installation (sorted).
	Installed []string
	// Artifacts are the artifact filenames that were installed (sorted).
	Artifacts []string
	// CascadeRemoved are baseline packages removed AFTER install because an
	// approved (conflict-driven) removal orphaned them — reverse-dependencies left
	// with an unmet dependency that the install did not satisfy (sorted). Empty
	// unless a removal triggered a cascade. These are also folded into the
	// preflight report's ApprovedRemovals so stats and the complete SBOM reflect
	// the final inventory.
	CascadeRemoved []string
	// Skipped is true when the plan had nothing to install and the step was a
	// no-op (the chroot was never entered).
	Skipped bool
}

// plannedInstall pairs a resolved package with the prepared artifact file that
// satisfies it.
type plannedInstall struct {
	pkg      ResolvedPackage
	artifact string // artifact filename, relative to the artifact cache dir
}

// installRequest is the family-agnostic input handed to an installer backend. It
// names the mounted baseline root, the in-chroot directory the prepared
// artifacts are reachable at, and the packages to install.
type installRequest struct {
	// chrootPath is the mounted baseline root the packages are installed into.
	chrootPath string
	// artifactChrootDir is the directory, as seen from inside the chroot, where
	// the prepared artifacts are bind-mounted.
	artifactChrootDir string
	// items are the packages to install, each paired with its artifact filename.
	items []plannedInstall
	// upgrade is true when the approved plan replaces at least one baseline package
	// with a newer version (an allowUpgrade-gated upgrade) OR carries an rpm
	// Obsoletes-driven removal (which `rpm -U` performs implicitly). It selects the
	// package-manager mode that can replace/obsolete an installed package: the rpm
	// backend switches from `rpm -i` to `rpm -U`. The deb backend ignores it —
	// `dpkg -i` already upgrades in place and deb has no Obsoletes mechanism.
	upgrade bool
}

// installerBackend installs prepared package artifacts into a mounted baseline
// chroot and verifies the result against the baseline package database. It is the
// family-specific (deb/rpm) seam, kept behind an interface so the deterministic
// install orchestration is unit-testable without root, mounts, or a real chroot.
type installerBackend interface {
	family() PackageManager
	// install installs the request's artifacts into the chroot, capturing the
	// package-manager output to the build log.
	install(req installRequest) error
	// removePackages removes the named baseline packages from the chroot BEFORE
	// install (a conflict-driven removal permitted by allowPackageRemoval, e.g.
	// removing initramfs-tools so dracut can install). It is a no-op when names is
	// empty. Preflight has already gated every removal (bootloader/kernel packages
	// are never in this set), so the backend removes exactly the approved names.
	removePackages(chrootPath string, names []string) error
	// verifyInstalled queries the baseline package database in chrootPath and
	// returns the names of the requested packages that are NOT installed.
	verifyInstalled(chrootPath string, pkgs []ResolvedPackage) (missing []string, err error)
	// auditDependencies recomputes the dependency graph from the on-disk package
	// state in chrootPath and returns the set of UNMET-dependency FAILURES as stable,
	// comparable descriptors ("<package-identity> | <requirement>"; see
	// parseUnmetDependencyFailures). It reads local state only (no network), so it
	// catches a baseline package left with a broken dependency after a force-removal —
	// which verifyInstalled (new packages only) and the package DB's install-state
	// flags cannot see. `broken` is empty when the dependency tree is satisfied;
	// `output` carries the tool's diagnostic. A non-nil error means the audit itself
	// could not run (e.g. the check tool is absent from the baseline), which the
	// caller treats as "cannot audit". Diffing the pre-removal descriptor set against
	// the post-install set lets the caller fail on a dependency failure the removal
	// INTRODUCED while ignoring failures the baseline already had — at
	// package+requirement granularity, so a new failure on an already-broken package
	// is still caught and a version/arch change is not mistaken for new breakage.
	auditDependencies(chrootPath string) (broken []string, output string, err error)
}

// Install-stage indirection seams over the impure dependencies (the package
// manager backend and the bind-mount lifecycle) so the orchestration in
// InstallOverlayPackages is unit-testable for both families. Tests override them.
var (
	selectInstallerBackend = selectInstaller
	mountSysfs             = mount.MountSysfs
	umountSysfs            = mount.UmountSysfs
	bindMountArtifacts     = mount.MountPath
	umountArtifacts        = mount.UmountAndDeletePath
)

// InstallOverlayPackages installs the approved overlay packages into the mounted
// baseline chroot using the prepared artifacts from the resolution plan's cache.
//
// It consumes the resolution plan and the preflight report together: the report
// is the gate — installation never proceeds when the preflight is blocked (or
// absent), satisfying the "no install attempt if preflight failed" guarantee.
// Only plan.ToInstall is installed, so the operation is strictly additive: the
// baseline's existing packages and bootloader are never replaced.
//
// The chroot bind mounts (sysfs and the artifact cache) are created and torn down
// within this call regardless of success, failure, or panic, and every requested
// package is verified installed before returning.
func InstallOverlayPackages(info *BaselineInfo, rootMount string, plan *ResolutionPlan, report *PreflightReport) (result *InstallResult, err error) {
	if info == nil {
		return nil, fmt.Errorf("overlay install: baseline info cannot be nil")
	}
	if plan == nil {
		return nil, fmt.Errorf("overlay install: resolution plan cannot be nil")
	}
	if strings.TrimSpace(rootMount) == "" {
		return nil, fmt.Errorf("overlay install: baseline root mount path cannot be empty")
	}
	// The preflight report is the gate: a missing or blocked report means the
	// dependency/conflict policy has not approved this plan, so no install may run.
	if report == nil {
		return nil, fmt.Errorf("overlay install: refusing to install without a passed preflight report")
	}
	if report.Blocked {
		return nil, fmt.Errorf("overlay install: refusing to install because the preflight is blocked (%d policy violation(s))", len(report.Violations))
	}

	backend, err := selectInstallerBackend(info.PackageManager)
	if err != nil {
		return nil, err
	}

	// Build the install set from the additive ToInstall slice, mapping each
	// resolved package to its prepared artifact and confirming it exists on disk.
	items, err := planInstalls(plan)
	if err != nil {
		return nil, err
	}
	// A build with neither install items nor approved removals is a true no-op. But a
	// removal-only swap (e.g. the replacement kernel is already installed, so ToInstall
	// is empty while the baseline kernel family must still be removed) has removals to
	// perform: fall through so the removal + audit + cascade below still run, skipping
	// only the package-install call itself.
	if len(items) == 0 && len(report.ToRemove) == 0 {
		log.Infof("Overlay install: nothing to install (all %d requested package(s) already satisfied by the baseline)", len(plan.Requested))
		return &InstallResult{Skipped: true}, nil
	}

	artifactNames := make([]string, 0, len(items))
	pkgs := make([]ResolvedPackage, 0, len(items))
	for _, it := range items {
		artifactNames = append(artifactNames, it.artifact)
		pkgs = append(pkgs, it.pkg)
	}
	sort.Strings(artifactNames)

	if len(items) > 0 {
		log.Infof("Overlay install: installing %d package(s) from %d prepared artifact(s) in %s into %s",
			len(items), len(artifactNames), plan.DownloadDir, rootMount)
	} else {
		log.Infof("Overlay install: no packages to install; performing %d approved baseline removal(s) only in %s", len(report.ToRemove), rootMount)
	}

	// Establish the chroot bind-mount lifecycle (sysfs + artifact cache) and tear
	// it down in reverse on every return path, including a panic inside install.
	teardown, err := mountChrootForInstall(rootMount, plan.DownloadDir)
	if err != nil {
		return nil, err
	}
	defer teardown(&err)

	// Conflict-driven removals (permitted by allowPackageRemoval) run FIRST, so the
	// conflicting baseline package is gone before its replacement is unpacked (e.g.
	// remove initramfs-tools before installing dracut). Preflight populated ToRemove
	// only when the policy opted in and never includes bootloader/kernel packages.
	//
	// A force-removal (dpkg --force-depends / rpm -e --nodeps) can leave an UNRELATED
	// baseline package with an unmet dependency that its replacement does not
	// satisfy. verifyInstalled only checks the newly-added packages, so such
	// collateral breakage would otherwise go unreported and the build would claim
	// success. To detect it, snapshot the SET of unmet-dependency FAILURES
	// (package-identity + requirement) BEFORE the removal, then audit again after
	// install and act only on failures that are new (present after but not before) — a
	// pre-existing broken baseline is neither blamed on nor "fixed" by the overlay, yet
	// a NEW breakage the removal introduces is caught, INCLUDING a new missing
	// requirement on a package that was already broken on a different requirement.
	//
	// The post-install audit (below) then CASCADES: the reverse-dependency the removal
	// orphaned is itself removed, transitively, because the policy already consented to
	// removing a baseline package. It still fails closed when the orphan is a
	// bootloader/kernel image or a to-install package.
	//
	// preRemovalBroken holds the failure descriptors already present before the
	// removal, so the post-install audit acts only on NEW ones.
	var preRemovalBroken map[string]bool
	if len(report.ToRemove) > 0 {
		// Fail CLOSED when the audit cannot be established: a force-removal
		// (--force-depends / --nodeps) with no working whole-database integrity check
		// could silently leave unrelated baseline packages broken. Rather than run the
		// destructive removal with the only safety net disabled, abort with an
		// actionable error BEFORE removing anything. The build has changed nothing yet,
		// so this is a clean refusal, not a partial state.
		broken, _, aerr := backend.auditDependencies(rootMount)
		if aerr != nil {
			return nil, fmt.Errorf("overlay install: cannot verify dependency integrity for the requested package removal(s) — "+
				"the baseline dependency-audit tool is unavailable (%w); install it in the baseline (apt-get for deb, dnf for rpm) "+
				"or drop allowPackageRemoval so no baseline package is removed", aerr)
		}
		preRemovalBroken = make(map[string]bool, len(broken))
		for _, f := range broken {
			preRemovalBroken[f] = true
		}
		if len(broken) > 0 {
			sort.Strings(broken)
			log.Warnf("Overlay install: the baseline already has %d unmet-dependency failure(s) BEFORE the overlay (%s); only NEW failures introduced by the removal will fail the build",
				len(broken), strings.Join(broken, "; "))
		}

		log.Infof("Overlay install: removing %d baseline package(s) before install (conflict-driven and/or kernel replacement): %s",
			len(report.ToRemove), strings.Join(report.ToRemove, ", "))
		if err = backend.removePackages(rootMount, report.ToRemove); err != nil {
			return nil, fmt.Errorf("overlay install: failed to remove %d baseline package(s): %w",
				len(report.ToRemove), err)
		}
	}

	// Install the prepared artifacts, unless this is a removal-only swap (no items).
	// The removal + audit + cascade below still run in that case; only the package
	// install and its post-condition verification are skipped.
	if len(items) > 0 {
		// Select the upgrade-capable package-manager mode (rpm -U) when the approved plan
		// either upgrades a baseline package OR carries an rpm Obsoletes-driven removal.
		// An obsoletion is NOT an ActionUpgrade, so report.Upgrades would miss it and the
		// batch would run under `rpm -i`, which neither replaces nor obsoletes an
		// installed package — the obsoletion would silently not happen. An Obsoletes-
		// driven removal is precisely an approved removal that is not an explicit one, so
		// it shows up as ApprovedRemovals having more entries than ToRemove.
		upgradeMode := report.Upgrades > 0 || len(report.ApprovedRemovals) > len(report.ToRemove)
		if err = backend.install(installRequest{
			chrootPath:        rootMount,
			artifactChrootDir: chrootArtifactDir,
			items:             items,
			upgrade:           upgradeMode,
		}); err != nil {
			// A "no space left on device" failure here means the baseline root filled up
			// while unpacking the added packages. Overlay mode does not auto-grow the
			// image to fit the packages (resize is a later, grow-only step keyed purely
			// on disk.size), so surface an actionable hint pointing at disk.size rather
			// than leaving the user with an opaque dpkg/rpm ENOSPC diagnostic.
			return nil, fmt.Errorf("overlay install failed for %d package(s) using %s: %w%s",
				len(items), info.PackageManager, err, diskSpaceHint(err))
		}

		// Post-condition: every requested package must be installed in the baseline DB.
		missing, verifyErr := backend.verifyInstalled(rootMount, pkgs)
		if verifyErr != nil {
			return nil, fmt.Errorf("overlay install: failed to verify installed packages: %w", verifyErr)
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			return nil, fmt.Errorf("overlay install: %d requested package(s) not present after install: %s",
				len(missing), strings.Join(missing, ", "))
		}
	}

	// After a force-removal, re-audit the whole installed set's dependency graph. A
	// removal permitted by allowPackageRemoval can orphan an UNRELATED baseline package
	// that only DEPENDS on the removed one — a reverse-dependency the replacement does
	// not satisfy (e.g. cloud-initramfs-growroot depends on the removed initramfs-tools).
	// verifyInstalled only checks the newly-added packages, so such collateral breakage
	// would otherwise ship silently; the pre-removal snapshot (preRemovalBroken) makes
	// the audit fail only on breakage the removal INTRODUCED, never on a pre-existing
	// broken baseline.
	//
	// Because the policy already consented to removing a baseline package, that consent
	// cascades to the reverse-dependencies the removal orphaned: each newly-broken
	// baseline package is removed too, transitively, until the dependency tree is whole.
	// The package manager's own audit (apt-get check / dnf check) is the ground truth, so
	// only genuinely-unsatisfiable packages are removed — an alternative dependency the
	// baseline still satisfies is never mistaken for breakage (which a static reverse-dep
	// walk over the first-alternative-only baseline metadata could not guarantee).
	//
	// That cascading consent applies ONLY when allowPackageRemoval is enabled. A kernel
	// replacement self-authorizes just its own kernel-family removals (carried explicitly
	// in ToRemove), so a COLLATERAL non-kernel package it orphans (e.g. a DKMS module
	// bound to an old kernel-headers) is NOT within that narrow authorization: with
	// allowPackageRemoval off the cascade fails closed on it (report.CollateralRemovalAuthorized).
	//
	// The cascade is bounded and fails CLOSED: a newly-broken package that is a
	// bootloader or bootable-kernel image (immutable) or that is itself in the to-install
	// set is NOT removed — the build aborts with an actionable error instead, because
	// removing it would break an invariant stronger than the cascade.
	var cascadeRemoved []string
	if len(report.ToRemove) > 0 {
		toInstallNames := make(map[string]bool, len(pkgs))
		for _, p := range pkgs {
			toInstallNames[p.Name] = true
		}
		family := backend.family()
		// The installed set strictly shrinks each pass (every pass purges the packages
		// it found newly broken, so they cannot reappear), so a real cascade converges in
		// a handful of passes. The cap is a pure backstop against a pathological audit
		// that never stabilizes.
		const maxCascadePasses = 100
		for pass := 0; ; pass++ {
			broken, out, aerr := backend.auditDependencies(rootMount)
			if aerr != nil {
				// The audit could not run post-install even though it ran pre-removal:
				// surface it rather than silently skipping the integrity guarantee.
				return nil, fmt.Errorf("overlay install: failed to audit dependency integrity after removals: %w", aerr)
			}
			var newlyBroken []string
			for _, f := range broken {
				if !preRemovalBroken[f] {
					newlyBroken = append(newlyBroken, f)
				}
			}
			if len(newlyBroken) == 0 {
				break // dependency tree is whole again
			}
			sort.Strings(newlyBroken)

			// Resolve each broken descriptor to two values, refusing to cascade past a
			// protected (bootloader/kernel) or to-install package:
			//   - name:    the BARE package name, used for the guards and for reporting —
			//              both are arch-agnostic (guards match at a name boundary;
			//              ApprovedRemovals is matched against BaselinePackage.Name).
			//   - operand: the package-manager REMOVAL operand. On deb it keeps the audit
			//              identity's arch qualifier (e.g. "libc6:i386") so `dpkg --purge`
			//              removes the exact broken instance instead of an ambiguous bare
			//              name on a multiarch baseline; on rpm the bare name is used, as
			//              `rpm -e` does not accept a version-less "name.arch" erase spec.
			removeSet := make(map[string]bool) // dedup removal operands
			var removeOperands []string
			nameSet := make(map[string]bool) // dedup bare names for reporting
			var removedNames []string
			for _, desc := range newlyBroken {
				name := brokenDescriptorPackage(family, desc)
				if name == "" {
					// A broken descriptor that names no removable package cannot be resolved
					// by removal and would loop forever: fail closed with the raw report.
					return nil, fmt.Errorf("overlay install: package removals introduced an unmet dependency failure that could not be attributed to a removable package (%q); add the missing dependency to the overlay or drop the removal:%s",
						desc, formatCommandOutput(out))
				}
				if isBootloaderPackage(name) || isKernelImagePackage(name) || toInstallNames[name] {
					return nil, fmt.Errorf("overlay install: package removals introduced %d unmet dependency failure(s) that were satisfied before the overlay (%s), "+
						"and resolving them would require removing %q — a bootloader/kernel image or a package the overlay installs, which must not be removed; "+
						"add the missing dependency to the overlay or drop the removal:%s",
						len(newlyBroken), strings.Join(newlyBroken, "; "), name, formatCommandOutput(out))
				}
				// A kernel replacement self-authorizes ONLY its own kernel-family removals; a
				// collateral non-kernel package it orphans is outside that scope. Removing it
				// requires the operator to opt in via allowPackageRemoval, so fail closed here
				// rather than silently purging an unrelated baseline package.
				if !report.CollateralRemovalAuthorized {
					return nil, fmt.Errorf("overlay install: the approved removal(s) orphaned %d baseline package(s) that were satisfied before the overlay (%s), "+
						"and removing %q is not authorized — a kernel replacement self-authorizes only its own kernel-family swap, not collateral packages; "+
						"enable allowPackageRemoval to cascade-remove orphaned packages, or add the missing dependency to the overlay:%s",
						len(newlyBroken), strings.Join(newlyBroken, "; "), name, formatCommandOutput(out))
				}
				operand := name
				if family == PackageManagerAPT {
					operand = brokenDescriptorIdentity(desc)
				}
				if !removeSet[operand] {
					removeSet[operand] = true
					removeOperands = append(removeOperands, operand)
				}
				if !nameSet[name] {
					nameSet[name] = true
					removedNames = append(removedNames, name)
				}
			}

			if pass >= maxCascadePasses {
				return nil, fmt.Errorf("overlay install: cascade removal did not converge after %d passes (still %d unmet dependency failure(s): %s); "+
					"add the missing dependency to the overlay or drop the removal:%s",
					maxCascadePasses, len(newlyBroken), strings.Join(newlyBroken, "; "), formatCommandOutput(out))
			}

			sort.Strings(removeOperands)
			sort.Strings(removedNames)
			log.Infof("Overlay install: cascade-removing %d baseline reverse-dependency package(s) orphaned by the approved removal(s): %s",
				len(removeOperands), strings.Join(removeOperands, ", "))
			if rerr := backend.removePackages(rootMount, removeOperands); rerr != nil {
				return nil, fmt.Errorf("overlay install: failed to cascade-remove %d orphaned baseline package(s): %w", len(removeOperands), rerr)
			}
			cascadeRemoved = append(cascadeRemoved, removedNames...)
		}

		// Fold the cascade removals into the preflight report so downstream reporting
		// reflects the FINAL inventory: both ComputePackageStats and the complete SBOM
		// (stageOverlaySBOMArtifacts) key off report.ApprovedRemovals and run after this
		// install stage over the same report pointer. ToRemove is deliberately left alone
		// — it is the explicit pre-install removal set already consumed above.
		if len(cascadeRemoved) > 0 {
			sort.Strings(cascadeRemoved)
			report.ApprovedRemovals = append(report.ApprovedRemovals, cascadeRemoved...)
			report.Removes += len(cascadeRemoved)
			log.Infof("Overlay install: cascade removal complete — %d additional baseline package(s) removed to keep dependencies satisfied: %s",
				len(cascadeRemoved), strings.Join(cascadeRemoved, ", "))
		}
	}

	installed := make([]string, 0, len(pkgs))
	for _, p := range pkgs {
		installed = append(installed, p.Name)
	}
	sort.Strings(installed)

	log.Infof("Overlay install complete: %d package(s) installed and verified in %s", len(installed), rootMount)
	return &InstallResult{Installed: installed, Artifacts: artifactNames, CascadeRemoved: cascadeRemoved}, nil
}

// planInstalls maps the plan's additive ToInstall packages to their prepared
// artifacts, confirming each artifact exists in the download cache. It is the
// "prepared artifacts, not ad-hoc unresolved install" guarantee: every package
// installed is backed by a concrete file the resolver already downloaded.
func planInstalls(plan *ResolutionPlan) ([]plannedInstall, error) {
	if len(plan.ToInstall) == 0 {
		return nil, nil
	}
	if strings.TrimSpace(plan.DownloadDir) == "" {
		return nil, fmt.Errorf("overlay install: resolution plan has %d package(s) to install but no artifact download directory", len(plan.ToInstall))
	}

	items := make([]plannedInstall, 0, len(plan.ToInstall))
	for _, rp := range plan.ToInstall {
		artifact, err := artifactFileFor(rp)
		if err != nil {
			return nil, err
		}
		hostPath := filepath.Join(plan.DownloadDir, artifact)
		if _, statErr := os.Stat(hostPath); statErr != nil {
			return nil, fmt.Errorf("overlay install: prepared artifact for %q not found at %s: %w", rp.Name, hostPath, statErr)
		}
		items = append(items, plannedInstall{pkg: rp, artifact: artifact})
	}

	// Preserve plan.ToInstall's order: the resolver put it in dependency-first
	// (topological) install order so dpkg -i can satisfy Pre-Depends left-to-right
	// (see orderInstallByArtifacts in resolve.go). It is already deterministic, so
	// the install command is stable WITHOUT an alphabetical re-sort — which would
	// reintroduce the pre-dependency ordering bug (e.g. gawk before libmpfr6).
	return items, nil
}

// artifactFileFor returns the prepared artifact filename for a resolved package,
// taken from the resolver-recorded download URL.
func artifactFileFor(rp ResolvedPackage) (string, error) {
	url := strings.TrimSpace(rp.URL)
	if url == "" {
		return "", fmt.Errorf("overlay install: resolved package %q has no artifact URL; cannot locate its prepared file", rp.Name)
	}
	base := filepath.Base(strings.TrimRight(url, "/"))
	// The basename is later joined into <downloadDir>/<base>, so it must be a single
	// path segment that cannot escape the artifact directory. A URL ending in "/.."
	// yields base == "..", and a stray separator could redirect the join, so reject
	// "", ".", "..", and any value containing a path separator ('/' or, for
	// completeness, a Windows-style '\'). (Shell-metacharacter safety at the
	// dpkg/rpm command line is handled separately by shell.QuoteArg; package
	// filenames legitimately contain '+'/'~', so they are not restricted to an alnum
	// allowlist here.)
	//
	// This basename mirrors how the downloader (pkgfetcher) names the cached file:
	// it too takes path.Base of the same raw resolver URL string, so query/fragment
	// suffixes (if any) are treated identically on both sides and the lookup stays
	// consistent with what was written to DownloadDir.
	if base == "" || base == "." || base == ".." || strings.ContainsRune(base, '/') || strings.ContainsRune(base, '\\') {
		return "", fmt.Errorf("overlay install: resolved package %q has an unusable artifact URL %q (basename %q is not a valid filename)", rp.Name, url, base)
	}
	return base, nil
}

// mountChrootForInstall sets up the chroot bind mounts needed for a package
// install — the kernel pseudo-filesystems (so maintainer/scriptlet hooks run) and
// the prepared artifact cache — and returns a teardown that reverses them.
//
// The teardown is idempotent against partial setup and records its own failures
// into the caller's error (without masking an earlier one), so cleanup problems
// are never silently swallowed.
func mountChrootForInstall(rootMount, artifactDir string) (func(*error), error) {
	if strings.TrimSpace(artifactDir) == "" {
		return nil, fmt.Errorf("overlay install: artifact download directory cannot be empty")
	}

	if err := mountSysfs(rootMount); err != nil {
		// Best-effort rollback of any partial sysfs mounts before failing.
		if cerr := umountSysfs(rootMount); cerr != nil {
			log.Warnf("Overlay install: rollback after failed sysfs mount also failed: %v", cerr)
		}
		return nil, fmt.Errorf("overlay install: failed to mount pseudo-filesystems into %s: %w", rootMount, err)
	}

	// The prepared artifact cache is bind-mounted into the chroot. It is only ever
	// read from (dpkg/rpm install the files in place), so a plain bind is used; a
	// read-only bind would require a follow-up remount and is not relied upon for
	// correctness here.
	artifactMountPoint := filepath.Join(rootMount, strings.TrimPrefix(chrootArtifactDir, "/"))
	if err := bindMountArtifacts(artifactDir, artifactMountPoint, "--bind"); err != nil {
		if cerr := umountSysfs(rootMount); cerr != nil {
			log.Warnf("Overlay install: rollback after failed artifact bind mount also failed: %v", cerr)
		}
		return nil, fmt.Errorf("overlay install: failed to bind-mount artifact cache %s into chroot: %w", artifactDir, err)
	}

	teardown := func(outErr *error) {
		// Unmount in reverse order: artifact bind mount first, then sysfs.
		if err := umountArtifacts(artifactMountPoint); err != nil {
			log.Errorf("Overlay install: failed to unmount artifact cache %s: %v", artifactMountPoint, err)
			recordCleanupError(outErr, fmt.Errorf("failed to unmount artifact cache %s: %w", artifactMountPoint, err))
		}
		if err := umountSysfs(rootMount); err != nil {
			log.Errorf("Overlay install: failed to unmount pseudo-filesystems from %s: %v", rootMount, err)
			recordCleanupError(outErr, fmt.Errorf("failed to unmount pseudo-filesystems from %s: %w", rootMount, err))
		}
	}
	return teardown, nil
}

// recordCleanupError folds a deferred cleanup error into the function's named
// return: it sets it when no primary error occurred, and otherwise annotates the
// primary error so the cleanup failure is still surfaced.
func recordCleanupError(outErr *error, cleanupErr error) {
	if outErr == nil {
		return
	}
	if *outErr == nil {
		*outErr = cleanupErr
	} else {
		*outErr = fmt.Errorf("%w; additionally, cleanup failed: %v", *outErr, cleanupErr)
	}
}

// formatCommandOutput renders captured package-manager output as an indented
// block appended to an error, or "" when there was none. It lets the install
// backends attach dpkg/rpm's actual diagnostic to the otherwise opaque
// "exit status 1" the streamed executor returns.
func formatCommandOutput(out string) string {
	out = strings.TrimSpace(out)
	if out == "" {
		return ""
	}
	// Indent every line by two spaces so the package-manager diagnostic reads as a
	// distinct block beneath the one-line error rather than blending into it.
	indented := "  " + strings.ReplaceAll(out, "\n", "\n  ")
	return "\n" + indented
}

// selectInstaller returns the installer backend for a package-manager family.
func selectInstaller(family PackageManager) (installerBackend, error) {
	switch family {
	case PackageManagerAPT:
		return &debInstallerBackend{}, nil
	case PackageManagerDNF:
		return &rpmInstallerBackend{}, nil
	default:
		return nil, fmt.Errorf("overlay install: unsupported package manager %q (expected %q or %q)",
			family, PackageManagerAPT, PackageManagerDNF)
	}
}

// debInstallerBackend installs prepared .deb artifacts into the baseline chroot
// with dpkg. The closure was already resolved, so installing the set in ordered,
// size-bounded dpkg batches satisfies inter-package dependencies among them; deps that were
// already present in the baseline remain untouched. `dpkg -i` installs new
// packages and upgrades an installed one in place, so it serves both the
// additive-only and additive-and-upgrade policies without a mode switch (the
// preflight gate decides which upgrades, if any, are in the approved set).
type debInstallerBackend struct{}

func (b *debInstallerBackend) family() PackageManager { return PackageManagerAPT }

func (b *debInstallerBackend) install(req installRequest) error {
	paths := make([]string, 0, len(req.items))
	for _, it := range req.items {
		// it.artifact is a URL-derived basename, so shell-quote each path before
		// joining it into the bash -c command line.
		paths = append(paths, shell.QuoteArg(filepath.Join(req.artifactChrootDir, it.artifact)))
	}

	envVars := []string{
		"DEBIAN_FRONTEND=noninteractive",
		"DEBCONF_NONINTERACTIVE_SEEN=true",
		"DEBCONF_NOWARNINGS=yes",
	}

	// Install the prepared local artifacts in dependency-first batches.
	//
	// A Pre-Depends must be unpacked AND CONFIGURED before its dependent is even
	// unpacked (e.g. gawk pre-depends on libmpfr6). `dpkg -i A B C…` does not
	// configure A until it has attempted C, so it can skip C even in a correctly
	// sorted list. Running one archive per invocation preserves the resolver's
	// dependency-first order while configuring each provider before its dependent.
	// It also removes the MAX_ARG_STRLEN limit that affects large overlays. Failed
	// batches are retried after configuration without replaying successful batches.
	//
	// Package files are supplied directly (no network, no repository resolution),
	// so the install stays strictly within the approved, pre-downloaded set.
	//
	// --auto-deconfigure lets dpkg temporarily deconfigure an installed package
	// that an artifact transiently Breaks, then reconfigure it — mirroring apt.
	// This covers an upgraded package (e.g. vim-runtime) that
	// `Breaks: <other> (<< newver)` against a baseline package ALSO upgraded later
	// in the same batch (e.g. vim-tiny): the break is self-resolving within the
	// set, which is why the preflight conflict gate permits it.
	//
	// "--" terminates option parsing so a URL-derived artifact basename beginning
	// with '-' is treated as a file path, not a dpkg option (shell-quoting stops
	// word-splitting, not option parsing).
	configureCmd := "dpkg --configure -a --auto-deconfigure"
	const maxInstallPasses = 6
	chunks := chunkArgs(paths, maxDpkgArgBytes)
	pending := chunks
	var lastErr error
	var lastOut string
	var lastFingerprint string
	for pass := 1; pass <= maxInstallPasses; pass++ {
		nextPending := make([][]string, 0, len(pending))
		var fingerprint strings.Builder
		for index, chunk := range pending {
			out, err := shell.ExecCmdWithStream("dpkg -i --auto-deconfigure -- "+strings.Join(chunk, " "), true, req.chrootPath, envVars)
			fmt.Fprintf(&fingerprint, "batch=%d\nout=%q\nerr=%v\n", index, out, err)
			if err != nil {
				nextPending = append(nextPending, chunk)
				if lastErr == nil {
					lastErr, lastOut = err, out
				}
			}
		}

		configureOut, configureErr := shell.ExecCmdWithStream(configureCmd, true, req.chrootPath, envVars)
		fmt.Fprintf(&fingerprint, "configure-out=%q\nconfigure-err=%v\n", configureOut, configureErr)
		currentFingerprint := fingerprint.String()
		if len(nextPending) == 0 && configureErr == nil {
			return nil
		}
		if configureErr != nil {
			lastErr, lastOut = configureErr, configureOut
		}
		if len(nextPending) == 0 {
			return fmt.Errorf("dpkg configure of %d artifact(s) failed: %w%s", len(paths), lastErr, formatCommandOutput(lastOut))
		}
		if currentFingerprint == lastFingerprint {
			return fmt.Errorf("dpkg install of %d artifact(s) failed with %d archive(s) still pending after pass %d: %w%s",
				len(paths), len(nextPending), pass, lastErr, formatCommandOutput(lastOut))
		}
		log.Infof("Overlay install: %d/%d batch(es) need a dependency-order retry after pass %d", len(nextPending), len(pending), pass)
		lastFingerprint = currentFingerprint
		pending = nextPending
		lastErr = nil
	}
	return fmt.Errorf("dpkg install of %d artifact(s) failed with %d archive(s) still pending after %d passes: %w%s",
		len(paths), len(pending), maxInstallPasses, lastErr, formatCommandOutput(lastOut))
}

// removePackages purges the named baseline packages with dpkg before the install.
// --force-depends is required for the conflict-driven case: the package being
// removed (e.g. initramfs-tools) may still be depended on at removal time by
// packages the replacement (dracut) will satisfy once installed, so dpkg's normal
// dependency check would refuse. Preflight has already approved the removal set
// (and excluded bootloader/kernel packages), so forcing the dependency check here
// is the deliberate, gated behavior — not a blanket override. --purge also drops
// config files so the replacement package's files do not collide with orphaned
// conffiles. "--" guards a name that begins with '-'.
func (b *debInstallerBackend) removePackages(chrootPath string, names []string) error {
	if len(names) == 0 {
		return nil
	}
	quoted := make([]string, 0, len(names))
	for _, n := range names {
		quoted = append(quoted, shell.QuoteArg(n))
	}
	envVars := []string{
		"DEBIAN_FRONTEND=noninteractive",
		"DEBCONF_NONINTERACTIVE_SEEN=true",
		"DEBCONF_NOWARNINGS=yes",
	}
	cmd := "dpkg --purge --force-depends -- " + strings.Join(quoted, " ")
	out, err := shell.ExecCmdWithStream(cmd, true, chrootPath, envVars)
	if err != nil {
		return fmt.Errorf("dpkg --purge of %d package(s) failed: %w%s", len(names), err, formatCommandOutput(out))
	}
	return nil
}

// auditDependencies runs `apt-get check`, which recomputes the dependency graph
// from the local dpkg status database (no network, no list update) and exits
// non-zero printing "unmet dependencies" when an installed package's dependency
// is missing — exactly the collateral damage a `dpkg --purge --force-depends`
// removal can leave behind. A clean run (exit 0) returns an empty set; a non-zero
// exit whose output carries the "unmet dependencies" marker returns the offending
// package names parsed from the report; any other failure (apt-get absent from a
// minimal baseline, etc.) is returned as an error so the caller skips the check.
func (b *debInstallerBackend) auditDependencies(chrootPath string) ([]string, string, error) {
	envVars := []string{
		"DEBIAN_FRONTEND=noninteractive",
		"DEBCONF_NONINTERACTIVE_SEEN=true",
		"DEBCONF_NOWARNINGS=yes",
	}
	out, err := shell.ExecCmdSilent("apt-get check", true, chrootPath, envVars)
	if err == nil {
		return nil, out, nil // dependency tree is satisfied
	}
	if hasUnmetDependencyMarker(out) {
		broken := parseUnmetDependencyFailures(out)
		if len(broken) == 0 {
			// The report is marked broken but no offending package could be parsed (an
			// unhandled diagnostic shape). Returning an empty set would make the caller
			// conclude "no new breakage" and ship a possibly-broken image, so treat an
			// unparseable-but-broken report as an integrity-check failure instead.
			return nil, out, fmt.Errorf("apt-get check reported unmet dependencies but no package could be parsed from the report%s", formatCommandOutput(out))
		}
		return broken, out, nil // genuine unmet dependencies
	}
	return nil, out, fmt.Errorf("apt-get check could not run: %w%s", err, formatCommandOutput(out))
}

func (b *debInstallerBackend) verifyInstalled(chrootPath string, pkgs []ResolvedPackage) ([]string, error) {
	var missing []string
	for _, p := range pkgs {
		// dpkg -s prints a "Status: install ok installed" line for an installed
		// package and exits non-zero for an unknown one. (dpkg is on the shell
		// allowlist; dpkg-query is not.) Quote the package name defensively before
		// interpolating it into the bash -c command, and pass "--" so a name that
		// ever begins with '-' is parsed as an operand rather than a dpkg option.
		cmd := "dpkg -s -- " + shell.QuoteArg(p.Name)
		out, err := shell.ExecCmdSilent(cmd, true, chrootPath, nil)
		if err == nil && strings.Contains(out, "install ok installed") {
			continue // present and fully installed
		}
		// A non-zero exit whose output carries dpkg's own "not installed"
		// diagnostic is the expected signal for a genuinely-absent package.
		// Any other failure (dpkg missing from the chroot, a corrupt status DB,
		// etc.) is a real verification error: surface it rather than reporting a
		// misleading "not present after install".
		if err != nil && !isNotInstalledOutput(out) {
			return nil, fmt.Errorf("verifying %q with dpkg -s failed: %w%s", p.Name, err, formatCommandOutput(out))
		}
		missing = append(missing, p.Name)
	}
	return missing, nil
}

// rpmInstallerBackend installs prepared .rpm artifacts into the baseline chroot
// with rpm. As with deb, the additive set is installed from local files only; the
// pre-resolved closure satisfies dependencies among the new packages.
type rpmInstallerBackend struct{}

func (b *rpmInstallerBackend) family() PackageManager { return PackageManagerDNF }

func (b *rpmInstallerBackend) install(req installRequest) error {
	paths := make([]string, 0, len(req.items))
	for _, it := range req.items {
		// it.artifact is a URL-derived basename, so shell-quote each path before
		// joining it into the bash -c command line.
		paths = append(paths, shell.QuoteArg(filepath.Join(req.artifactChrootDir, it.artifact)))
	}

	// rpm -i installs (adds) the local artifacts and fails outright on an
	// already-installed package. Under additive-only that is exactly right: the
	// preflight gate blocks every ActionUpgrade, and ToInstall excludes packages
	// already present in the baseline, so only genuinely-new packages reach here,
	// and an unexpected already-installed one fails loudly instead of being
	// silently replaced.
	//
	// When the approved plan contains upgrades (allowUpgrade policy), switch the
	// whole batch to `rpm -U`, which upgrades an installed package and still
	// installs new ones — `rpm -i` cannot replace an installed package, and rpm
	// installs the artifact set as one transaction, so a single mode must cover
	// it. Running the pure-adds in the batch under -U (rather than -i) is safe
	// here: preflight has already classified every present-package change as an
	// ActionUpgrade under the AllowUpgrade gate, blocked bootable-kernel and
	// bootloader replacement (ruleKernelImmutable / ruleBootloaderImmutable), and
	// gated any Obsoletes-driven removal through AllowRemoval — so no unreviewed
	// replacement can slip through the coarser -U mode. Downgrades never reach
	// here (preflight leaves AllowDowngrade off), so no --oldpackage is needed.
	//
	// "--" terminates option parsing so a URL-derived artifact basename beginning
	// with '-' is treated as a file path rather than an rpm option.
	op := "-i"
	if req.upgrade {
		op = "-U"
	}
	cmd := "rpm " + op + " -v -- " + strings.Join(paths, " ")
	out, err := shell.ExecCmdWithStream(cmd, true, req.chrootPath, nil)
	if err != nil {
		return fmt.Errorf("rpm install of %d artifact(s) failed: %w%s", len(paths), err, formatCommandOutput(out))
	}
	return nil
}

// removePackages erases the named baseline packages with rpm before the install.
// --nodeps mirrors the deb --force-depends rationale: the conflicting package
// being removed may still be depended on until its replacement is installed, so
// rpm's dependency check would otherwise refuse. Preflight has already approved
// the set (bootloader/kernel packages excluded), so bypassing the dependency check
// here is the deliberate, gated behavior. "--" guards a name beginning with '-'.
//
// --allmatches erases EVERY installed version that matches a name, not just a
// unique one. RPM keeps multiple installonly kernel versions under the SAME name
// (e.g. two `kernel-core`), and a replaceKernel swap must clear the whole baseline
// kernel family; a bare `rpm -e kernel-core` would abort as ambiguous
// ("specifies multiple packages") and leave the old kernels behind. It is a no-op
// for an ordinary single-version package, so it is safe to apply to every removal.
func (b *rpmInstallerBackend) removePackages(chrootPath string, names []string) error {
	if len(names) == 0 {
		return nil
	}
	quoted := make([]string, 0, len(names))
	for _, n := range names {
		quoted = append(quoted, shell.QuoteArg(n))
	}
	cmd := "rpm -e --nodeps --allmatches -- " + strings.Join(quoted, " ")
	out, err := shell.ExecCmdWithStream(cmd, true, chrootPath, nil)
	if err != nil {
		return fmt.Errorf("rpm -e of %d package(s) failed: %w%s", len(names), err, formatCommandOutput(out))
	}
	return nil
}

// auditDependencies runs `dnf check --dependencies`, which inspects the local
// rpmdb (no network, no metadata refresh) and exits non-zero listing broken
// dependencies when an installed package requires something no installed package
// provides — the collateral damage an `rpm -e --nodeps` removal can leave. A clean
// run returns an empty set; a non-zero exit whose output carries the broken-
// dependency marker returns the offending package names parsed from the report;
// any other failure (dnf absent from the baseline, etc.) is returned as an error so
// the caller skips the check rather than misreporting.
func (b *rpmInstallerBackend) auditDependencies(chrootPath string) ([]string, string, error) {
	out, err := shell.ExecCmdSilent("dnf check --dependencies", true, chrootPath, nil)
	if err == nil {
		return nil, out, nil // dependency tree is satisfied
	}
	if hasUnmetDependencyMarker(out) {
		broken := parseUnmetDependencyFailures(out)
		if len(broken) == 0 {
			// Marked broken but nothing parseable (e.g. a bare "Depsolve Error"): treat
			// as an integrity-check failure rather than a clean empty result, so the
			// caller never mistakes an unparseable breakage report for "no new breakage".
			return nil, out, fmt.Errorf("dnf check reported broken dependencies but no package could be parsed from the report%s", formatCommandOutput(out))
		}
		return broken, out, nil // genuine broken dependencies
	}
	return nil, out, fmt.Errorf("dnf check could not run: %w%s", err, formatCommandOutput(out))
}

func (b *rpmInstallerBackend) verifyInstalled(chrootPath string, pkgs []ResolvedPackage) ([]string, error) {
	var missing []string
	for _, p := range pkgs {
		// rpm -q exits non-zero and prints "package <name> is not installed" for a
		// genuinely-absent package. Any other failure (rpm missing from the chroot,
		// an rpm DB error, etc.) is a real verification error and must not be
		// silently reported as a missing package. "--" terminates option parsing so
		// a name that ever begins with '-' is parsed as an operand, not an rpm option.
		cmd := "rpm -q -- " + shell.QuoteArg(p.Name)
		out, err := shell.ExecCmdSilent(cmd, true, chrootPath, nil)
		if err == nil {
			continue // installed
		}
		if !isNotInstalledOutput(out) {
			return nil, fmt.Errorf("verifying %q with rpm -q failed: %w%s", p.Name, err, formatCommandOutput(out))
		}
		missing = append(missing, p.Name)
	}
	return missing, nil
}

// isNotInstalledOutput reports whether package-manager query output carries the
// expected "not installed" diagnostic. Both dpkg -s and rpm -q emit a message
// containing "is not installed" for an absent package, which lets verifyInstalled
// distinguish that expected signal from a genuine tool/DB failure.
func isNotInstalledOutput(out string) bool {
	return strings.Contains(out, "is not installed")
}

// hasUnmetDependencyMarker reports whether a dependency-audit tool's output
// carries its "broken/unmet dependency" diagnostic, distinguishing a genuine
// dependency-integrity failure (which must fail the build) from the tool failing
// to run at all (e.g. absent from a minimal baseline). apt-get emits "unmet
// dependencies"; dnf's `check` emits "broken dependency"/"broken dependencies",
// "has missing requires", and on some versions "Depsolve Error". The markers here
// MUST cover every line shape parseUnmetDependencyFailures recognizes — otherwise a
// standard broken report (e.g. dnf's "has missing requires") would be misclassified
// as a tool failure and never reach the parser. The match is case-insensitive since
// casing varies across tool versions.
func hasUnmetDependencyMarker(out string) bool {
	lower := strings.ToLower(out)
	for _, marker := range []string{
		"unmet dependencies", // apt-get check
		"broken dependency",  // dnf check --dependencies
		"broken dependencies",
		"has missing requires", // dnf check --dependencies (per-package line)
		"depsolve error",       // dnf (some versions)
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// parseUnmetDependencyFailures extracts the set of unmet-dependency FAILURES from an
// `apt-get check` / `dnf check --dependencies` report as stable, comparable
// descriptors, so the caller can diff the pre-removal snapshot against the
// post-install snapshot and fail only on breakage the removal actually introduced.
//
// Each descriptor pairs a stable PACKAGE IDENTITY with the specific missing
// REQUIREMENT ("<identity> | <requirement>"), because diffing by identity alone is
// too coarse in three ways the before/after comparison must survive:
//   - a package that was ALREADY broken on one requirement and is broken on a
//     DIFFERENT requirement after the removal (identity unchanged, new pair);
//   - deb multiarch, where libc6:amd64 and libc6:i386 are distinct instances (the
//     apt token already carries :arch, so it is kept, NOT trimmed);
//   - rpm version churn, where an upgraded package's NEVRA changes between audits —
//     so an rpm token (NAME-VERSION-RELEASE.ARCH) is reduced to a version-independent
//     name.arch identity (see rpmNameArch) that stays equal across the upgrade.
//
// A line that names a broken package but no specific requirement (e.g. dnf's bare
// "<pkg> has broken dependencies") yields a "<identity> | " descriptor, which still
// compares correctly at identity granularity. The result is de-duplicated and
// sorted; an empty result means nothing could be attributed (the caller keeps the
// raw output for diagnostics).
//
// Recognized shapes:
//   - apt-get: " <pkg> : Depends: <dep> ..." (identity before ':', requirement after)
//   - dnf:     "package <pkg> requires <dep> ...", "<pkg-nevra> requires <dep>",
//     "<pkg-nevra> has broken dependencies", "<pkg-nevra> has missing requires ..."
func parseUnmetDependencyFailures(out string) []string {
	seen := make(map[string]bool)
	var failures []string
	add := func(identity, requirement string) {
		identity = strings.TrimSpace(identity)
		if identity == "" {
			return
		}
		descriptor := identity + " | " + strings.TrimSpace(requirement)
		if seen[descriptor] {
			return
		}
		seen[descriptor] = true
		failures = append(failures, descriptor)
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		switch {
		case len(fields) >= 2 && fields[1] == ":":
			// apt-get: "<pkg> : <requirement...>". The apt token is name or name:arch
			// (no version), so it is a stable identity as-is — keep the arch qualifier.
			add(fields[0], strings.Join(fields[2:], " "))
		case len(fields) >= 4 && fields[0] == "package" && fields[2] == "requires":
			// dnf: "package <nevra> requires <dep>, but none of the providers ..."
			add(rpmNameArch(fields[1]), strings.Join(fields[3:], " "))
		case len(fields) >= 3 && fields[1] == "requires":
			// dnf: "<nevra> requires <dep>"
			add(rpmNameArch(fields[0]), strings.Join(fields[2:], " "))
		case strings.Contains(line, "has broken dependencies") || strings.Contains(line, "has missing requires"):
			// dnf: "<nevra> has broken dependencies" / "<nevra> has missing requires <dep>".
			// Requirement text (if any) follows the marker; the identity is the leading token.
			req := ""
			if idx := strings.Index(line, "has missing requires"); idx != -1 {
				req = strings.TrimSpace(line[idx+len("has missing requires"):])
			}
			add(rpmNameArch(fields[0]), req)
		}
	}
	sort.Strings(failures)
	return failures
}

// brokenDescriptorIdentity returns the package IDENTITY from an audit failure
// descriptor produced by parseUnmetDependencyFailures ("<identity> | <requirement>"):
// the portion before " | ", trimmed. The identity retains its family-specific
// qualifier — a deb "name:arch" multiarch suffix or an rpm "name.arch" — so the deb
// removal can target the exact broken instance (see brokenDescriptorPackage for why the
// GUARDS instead use the bare name). Returns "" when the descriptor carries no identity.
func brokenDescriptorIdentity(descriptor string) string {
	identity := descriptor
	if i := strings.Index(descriptor, " | "); i != -1 {
		identity = descriptor[:i]
	}
	return strings.TrimSpace(identity)
}

// brokenDescriptorPackage extracts the bare package name from an audit failure
// descriptor, so the cascade can apply the bootloader/kernel and to-install guards to
// it and report it — all of which are arch-agnostic. The identity's family-specific
// qualifier is stripped so the guards (which match at a name boundary) see the bare name:
//   - deb: an apt identity is "name" or "name:arch"; deb names never contain ':', so
//     the ":arch" multiarch suffix is cut at the first ':'.
//   - rpm: rpmNameArch produced "name.arch"; the arch is the final '.'-delimited field
//     by construction, so it is cut at the LAST '.' (an rpm name may itself contain a
//     '.', which this leaves intact). Stripping it matters: matchesPackagePrefix treats
//     '.' as a non-boundary, so "kernel-core.x86_64" would evade the kernel-image guard.
//
// The bare name is NOT necessarily the removal operand: on deb the arch-qualified
// identity is used to purge the exact broken instance on a multiarch system (see the
// cascade loop). Returns "" when the descriptor carries no identity (an un-actionable
// report the caller must fail closed on rather than loop over).
func brokenDescriptorPackage(family PackageManager, descriptor string) string {
	identity := brokenDescriptorIdentity(descriptor)
	switch family {
	case PackageManagerAPT:
		if i := strings.IndexByte(identity, ':'); i != -1 {
			identity = identity[:i]
		}
	case PackageManagerDNF:
		if i := strings.LastIndexByte(identity, '.'); i > 0 {
			identity = identity[:i]
		}
	}
	return strings.TrimSpace(identity)
}

// rpmNameArch reduces an rpm NEVRA token (NAME-VERSION-RELEASE.ARCH, e.g.
// "kernel-core-6.8.0-1.x86_64") to a version-INDEPENDENT "name.arch" identity
// ("kernel-core.x86_64"), so a package upgraded in the same transaction — whose
// NEVRA changes between the pre-removal and post-install audits — is not mistaken
// for newly-broken. The arch is the suffix after the final '.'; version and release
// are the last two '-'-delimited fields of the remainder (NAME may itself contain
// '-', which is why only the trailing two fields are stripped). A token that does
// not look like a NEVRA (no '.', or too few '-' fields) is returned trimmed as-is.
func rpmNameArch(nevra string) string {
	nevra = strings.TrimSpace(nevra)
	dot := strings.LastIndexByte(nevra, '.')
	if dot <= 0 || dot == len(nevra)-1 {
		return nevra // no arch suffix; not a NEVRA we can split
	}
	arch := nevra[dot+1:]
	nvr := nevra[:dot] // NAME-VERSION-RELEASE
	// Strip the trailing release and version fields (last two '-' segments).
	if i := strings.LastIndexByte(nvr, '-'); i > 0 {
		nvr = nvr[:i] // drop release
		if j := strings.LastIndexByte(nvr, '-'); j > 0 {
			nvr = nvr[:j] // drop version
		} else {
			return nevra // only one '-' segment before arch: not a full NEVRA
		}
	} else {
		return nevra // no '-' segments: not a full NEVRA
	}
	return nvr + "." + arch
}

// diskSpaceHint returns an actionable, indented hint appended to an install error
// when the failure was caused by the baseline root filling up, or "" otherwise.
// The install backends fold the package manager's captured output (which carries
// the "No space left on device" ENOSPC diagnostic) into the error they return, so
// the hint keys off that text. Overlay mode never auto-grows the image to fit the
// added packages — resize is a separate, grow-only step keyed on disk.size — so a
// full baseline is a configuration problem the hint points the user at directly.
func diskSpaceHint(installErr error) string {
	if installErr == nil {
		return ""
	}
	// ENOSPC surfaces as "No space left on device" from dpkg/rpm and the kernel;
	// match case-insensitively since the exact casing varies by tool.
	if !strings.Contains(strings.ToLower(installErr.Error()), "no space left on device") {
		return ""
	}
	return "\n  hint: the baseline root filesystem ran out of space while installing the added " +
		"package(s). Overlay mode does not auto-grow the image to fit new packages; set disk.size " +
		"in the template larger than the baseline image to make room, then rebuild."
}
