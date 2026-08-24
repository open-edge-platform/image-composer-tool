package overlay

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage/debutils"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage/rpmutils"
)

// ActionType classifies a single planned package operation produced by the
// two-slice preflight (baseline installed set vs. resolved overlay set).
type ActionType string

const (
	// ActionAdd installs a package that is not present in the baseline.
	ActionAdd ActionType = "add"
	// ActionUpgrade replaces a baseline package with a newer version.
	ActionUpgrade ActionType = "upgrade"
	// ActionDowngrade replaces a baseline package with an older version.
	ActionDowngrade ActionType = "downgrade"
	// ActionRemove deletes a package that is present in the baseline.
	ActionRemove ActionType = "remove"
	// ActionConflict marks a package whose installation conflicts with the
	// baseline (e.g. an exclusive capability or an uncomparable version change).
	ActionConflict ActionType = "conflict"
	// ActionUnsatisfiedDep marks a to-install package whose version-pinned
	// dependency names a package present in the baseline at a version that does
	// not satisfy the pin AND that the overlay is not upgrading to a satisfying
	// version. In additive-only mode the baseline package is never upgraded, so
	// the pin can never be met. In additive-and-upgrade mode the resolver upgrades
	// a required dependency when a satisfying newer version is available (see
	// upgradeEligibleNames); this action therefore fires only when even that is
	// impossible — the satisfying version is not in the post-install set. Either
	// way the install would fail at the package-manager's configure step (e.g.
	// systemd-boot's exact-version dep on libsystemd-shared against an older
	// baseline copy with no newer copy available).
	ActionUnsatisfiedDep ActionType = "unsatisfied-dependency"
)

// Policy rule identifiers reported on a blocked action, so error output can name
// the exact rule that was violated.
const (
	ruleAllowRemoval         = "allowPackageRemoval=false"
	ruleAllowDowngrade       = "allowDowngrade=false"
	ruleAllowUpgrade         = "allowUpgrade=false"
	ruleConflictPolicyFail   = "conflictPolicy=fail"
	ruleBootloaderImmutable  = "bootloader-immutable"
	ruleKernelImmutable      = "kernel-immutable"
	ruleUnsatisfiedDep       = "unsatisfiable-versioned-dependency"
	ruleReplaceKernelInvalid = "replaceKernel-no-kernel-installed"
	ruleReplaceKernelAmbig   = "replaceKernel-ambiguous"
)

// bootloaderPackagePrefixes are package-name prefixes (case-insensitive) that
// identify bootloader packages. Overlay mode must never modify the bootloader,
// so any non-trivial action touching one of these is blocked unconditionally.
// A prefix matches the bare name or a sub-package at a '-'/digit boundary (see
// isBootloaderPackage), so "grub" covers grub2, grub-efi-amd64, grub-pc, etc.
// but "systemd-boot" does NOT swallow the unrelated "systemd-bootchart".
var bootloaderPackagePrefixes = []string{
	"grub",   // grub, grub2, grub-efi-amd64, grub-pc, grub2-efi-x64, ...
	"grubby", // GRUB config tool on rpm distros (not caught by the "grub" boundary)
	"shim",   // shim, shim-signed, shim-x64
	"systemd-boot",
	"sd-boot",
	"gummiboot",
	"efibootmgr",
}

// kernelImagePackagePrefixes identify the bootable kernel-image packages overlay
// mode must never replace: RegenerateBoot only refreshes the initramfs, it does
// not rewrite the bootloader's menu entries for a changed kernel version, so an
// in-place kernel upgrade (especially rpm -U, which replaces the running kernel)
// can leave the boot config pointing at a removed/renamed kernel. Adding a new
// kernel alongside the existing one is fine; only upgrading/replacing an
// installed kernel image is blocked (see violatedRule).
//
// The match is boundary-aware (see isKernelImagePackage) so it catches the
// bootable images ("linux-image-*", "kernel", "kernel-core") without swallowing
// userspace kernel-adjacent packages ("linux-libc-dev", "linux-tools-common",
// "kernel-headers") that carry no boot entry and are safe to upgrade.
var kernelImagePackagePrefixes = []string{
	"linux-image",  // Debian/Ubuntu bootable kernel image
	"kernel-image", // some distros' explicit image package
	"kernel-core",  // rpm modular kernel: the bootable core
	"kernel",       // rpm monolithic kernel image ("kernel", "kernel-5.14...")
}

// kernelSafeExactNames are kernel-prefixed package names that are NOT bootable
// images and must stay upgradable even though a prefix above would otherwise
// match them. They are userspace/development packages with no /boot entry.
var kernelSafeExactNames = map[string]bool{
	"kernel-headers":       true,
	"kernel-devel":         true,
	"kernel-devel-matched": true,
	"kernel-tools":         true,
	"kernel-tools-libs":    true,
}

// kernelFamilyPackagePrefixes identify the FULL kernel package family that a
// kernel replacement (overlayPolicy.replaceKernel) removes from the baseline so
// the swapped image ships only the new kernel. It is deliberately broader than
// kernelImagePackagePrefixes (only the bootable image): removing the image alone
// would orphan the modules/headers/meta-packages that pin the old kernel version
// and trip the install step's post-removal cascade guard, so the whole family is
// removed as one batch. Userspace kernel-adjacent packages with no per-kernel
// identity (linux-libc-dev, linux-tools-common, and the kernel-headers/-devel/
// -tools names in kernelSafeExactNames) are excluded so a swap never drags them out.
var kernelFamilyPackagePrefixes = []string{
	"linux-image",      // Debian/Ubuntu bootable image + the "linux-image-generic" meta
	"linux-modules",    // per-kernel modules ("linux-modules-6.8.0-40-generic", "-extra")
	"linux-headers",    // per-kernel headers ("linux-headers-6.8.0-40-generic", "-generic" meta)
	"linux-generic",    // Ubuntu kernel meta flavour ("linux-generic", "linux-generic-hwe-*")
	"linux-oem",        // Ubuntu OEM kernel meta flavour
	"linux-lowlatency", // Ubuntu low-latency kernel meta flavour
	"kernel",           // rpm "kernel", "kernel-core", "kernel-modules", "kernel-modules-extra"
}

// PlannedAction is a single classified package operation.
type PlannedAction struct {
	// Type is the classified action (add/upgrade/downgrade/remove/conflict).
	Type ActionType
	// Package is the canonical package name the action targets.
	Package string
	// CurrentVersion is the version installed in the baseline (Slice A); empty
	// for a pure add.
	CurrentVersion string
	// RequestedVersion is the version the overlay resolution would install
	// (Slice B); empty for a remove.
	RequestedVersion string
	// Arch is the package architecture, when known.
	Arch string
	// ConflictWith names the baseline package this one conflicts with, for
	// conflict actions surfaced by the simulate aid.
	ConflictWith string
	// Bootloader reports whether this action touches a bootloader package.
	Bootloader bool
	// Kernel reports whether this action touches a bootable kernel-image package.
	Kernel bool
	// ExplicitRemoval marks an ActionRemove that the install step must perform as
	// an explicit package-manager removal BEFORE installing (a conflict-driven
	// removal, e.g. removing initramfs-tools so dracut can install). It is not set
	// for an rpm Obsoletes-driven removal, which `rpm -U` performs implicitly.
	ExplicitRemoval bool
	// ObsoletesDriven marks an ActionRemove that `rpm -U` performs IMPLICITLY because
	// a to-install package Obsoletes: the baseline package. It is the ONLY removal
	// kind the install step does not queue explicitly (see classifyObsoletions). Any
	// other approved removal — a conflict-driven reclassification or a
	// simulator-surfaced ActionRemove — must be executed explicitly, so a removal
	// that is neither ExplicitRemoval nor ObsoletesDriven is queued into ToRemove
	// rather than silently assumed to happen on its own.
	ObsoletesDriven bool
	// KernelReplacement marks an ActionRemove of a baseline kernel-family package
	// sanctioned by overlayPolicy.replaceKernel (the full kernel swap). It is what
	// permits a removal that touches a bootable kernel image — normally blocked by
	// ruleKernelImmutable — and it approves the removal WITHOUT requiring
	// allowPackageRemoval. It is set only for the auto-detected old-kernel family,
	// never for any other package, so kernel immutability is untouched for every
	// build that does not opt into replaceKernel.
	KernelReplacement bool
	// Detail carries optional extra diagnostic context (e.g. a simulator note).
	Detail string
}

// PolicyViolation records a planned action blocked by policy and the rule it
// violated.
type PolicyViolation struct {
	Action PlannedAction
	// Rule is the violated policy rule identifier (one of the rule* constants).
	Rule string
}

// PreflightReport is the deterministic result of the two-slice preflight. It is
// the unit the install step gates on: when Blocked is true, installation must
// not proceed.
type PreflightReport struct {
	// Actions are all classified planned actions, in deterministic order.
	Actions []PlannedAction
	// Violations are the actions blocked by policy, in deterministic order.
	Violations []PolicyViolation
	// Counts of each action class, for logging/diagnostics.
	Adds, Upgrades, Downgrades, Removes, Conflicts, UnsatisfiedDeps int
	// ToRemove are the canonical names of baseline packages the install step must
	// explicitly remove before installing (conflict-driven removals permitted by
	// allowPackageRemoval), in deterministic order. Empty unless a conflict was
	// reclassified into a permitted removal. It deliberately EXCLUDES an rpm
	// Obsoletes-driven removal: `rpm -U` erases the obsoleted package implicitly, so
	// re-removing it explicitly is redundant (and would fail once it is gone).
	ToRemove []string
	// ApprovedRemovals are the canonical names of ALL baseline packages that policy
	// approved for removal — both the explicit conflict-driven removals in ToRemove
	// AND the rpm Obsoletes-driven removals `rpm -U` performs implicitly — in
	// deterministic order. It is the set that will actually be gone from the final
	// image, so it drives the removal stats and the complete-SBOM exclusion (an
	// Obsoletes-driven removal is absent from ToRemove but must still not appear in
	// the final inventory).
	ApprovedRemovals []string
	// CollateralRemovalAuthorized reports whether the install step may cascade-remove
	// a baseline package that a permitted removal ORPHANS (a reverse-dependency of a
	// removed package). It mirrors overlayPolicy.allowPackageRemoval: a kernel
	// replacement self-authorizes ONLY its own kernel-family removals (recorded
	// explicitly in ToRemove), so when allowPackageRemoval is off the cascade must
	// fail on any collateral non-kernel breakage rather than silently purging an
	// unrelated package the operator never consented to remove.
	CollateralRemovalAuthorized bool
	// ReplacementKernels are the concrete bootable kernel-image packages that
	// overlayPolicy.replaceKernel.package resolved to (empty when replaceKernel is
	// unset). A valid swap resolves to exactly one; zero (no kernel would remain) or
	// several (an over-broad glob) block the build (ruleReplaceKernelInvalid /
	// ruleReplaceKernelAmbig) and are surfaced so the operator can narrow the request.
	ReplacementKernels []string
	// Blocked is true when at least one policy violation was found.
	Blocked bool
}

// PreflightInput is the pure, side-effect-free input to EvaluatePreflight. It is
// deliberately decoupled from I/O so every policy path is unit-testable.
type PreflightInput struct {
	// Family is the package-manager family, used to pick the version comparator.
	Family PackageManager
	// Baseline is Slice A: the baseline package inventory (only installed
	// packages participate).
	Baseline []BaselinePackage
	// Resolved is Slice B: the set the overlay will actually install (the
	// plan's ToInstall), carrying the requested versions. In additive-only mode it
	// is exactly the closure members not already present in the baseline; in
	// additive-and-upgrade mode it ALSO contains the approved upgrades of present
	// baseline packages (a present package the resolver routed in because it is in
	// the bounded upgrade set). It still excludes present packages that are NOT
	// being changed, so classifyActions never spuriously flags an untouched
	// baseline package.
	Resolved []ResolvedPackage
	// ResolvedClosure is the FULL transitive dependency closure the resolver
	// produced (plan.Closure), including members already satisfied by the baseline
	// and therefore absent from Resolved (ToInstall). It is used only to build the
	// keep-set for a kernel replacement, so a kernel-family package the replacement
	// depends on but that is already present in the baseline is never swept into the
	// removal set. It is optional: when empty, the keep-set falls back to Resolved.
	ResolvedClosure []ResolvedPackage
	// SimulatedActions are removals/conflicts surfaced by a package-manager
	// simulate run, merged in as a validation aid. The two-slice comparison
	// remains authoritative for add/upgrade/downgrade; this only contributes the
	// remove/conflict actions a purely additive closure cannot itself produce.
	SimulatedActions []PlannedAction
	// ArtifactDeps are the version-constrained dependency edges declared by the
	// to-install packages, read from their artifact metadata. They let the
	// preflight catch a version pin on a baseline package that additive-only
	// install can never satisfy (present-but-wrong-version), which a purely
	// name-based closure cannot see.
	ArtifactDeps []ArtifactDependency
	// Obsoletions are the Obsoletes: declarations of the to-install rpm artifacts.
	// Under `rpm -U` an Obsoletes on a present baseline package erases it, so each
	// such obsoletion is classified as an ActionRemove and governed by the
	// AllowRemoval gate rather than silently removing the package at install time.
	Obsoletions []ArtifactObsoletion
	// Policy is the overlay policy that gates the classified actions.
	Policy config.OverlayPolicy
}

// simulateOverlayInstall simulates the overlay install and returns the
// remove/conflict actions it would trigger, for the policy gate to enforce. Its
// output is a validation aid — the two-slice model still drives add/upgrade/
// downgrade decisions — so a failure here is non-fatal (see Preflight).
//
// The default implementation is a METADATA-based simulation: it reads the
// declared Conflicts:/Breaks: (deb) and Conflicts: (rpm) of every to-install
// artifact on the host and reports each one that names a package present in the
// baseline (at a version the conflict's range covers) as an ActionConflict. This
// catches a declared conflict — which dpkg -i / rpm -i would otherwise only
// reveal by aborting at unpack time — up front, without entering a chroot or
// running the package manager, so it is deterministic and always executes.
//
// It does NOT catch a file-level collision that no package declares (two packages
// shipping the same path): nothing in the artifact metadata expresses that, so it
// still surfaces as a loud install-time failure. A future live simulator
// (apt-get install --simulate / dnf install --assumeno, run in the mounted chroot
// during the install phase) could augment this to cover those cases. Tests
// override this seam to exercise the remove/conflict policy paths directly.
var simulateOverlayInstall = func(info *BaselineInfo, baseline []BaselinePackage, plan *ResolutionPlan) ([]PlannedAction, error) {
	conflicts, err := readOverlayArtifactConflicts(info.PackageManager, plan)
	if err != nil {
		return nil, err
	}
	actions := classifyConflicts(info.PackageManager, baselineVersionIndex(baseline), plan.ToInstall, conflicts)

	return actions, nil
}

// Preflight runs the two-slice dependency/conflict preflight for an overlay
// build and enforces the overlay policy. It compares the baseline installed set
// (Slice A) against the set the overlay will actually install (Slice B =
// plan.ToInstall), classifies every planned action, and blocks installation on
// any policy violation with an actionable diagnostic.
//
// Slice B is deliberately plan.ToInstall, NOT the full plan.Closure: only
// ToInstall is ever handed to dpkg/rpm. In additive-only mode ToInstall is the
// closure members not already present in the baseline; in additive-and-upgrade
// mode it additionally holds the approved upgrades the resolver selected. Either
// way, a present baseline package that the overlay does NOT change is excluded,
// so comparing its repo-pool version against the baseline can never spuriously
// flag a security-patched base package (whose installed version outranks the
// pool) as a downgrade — the resolver already decided such packages stay put.
//
// It returns the report unconditionally (so callers can log the full plan) and a
// non-nil error when the preflight is blocked.
func Preflight(info *BaselineInfo, baseline []BaselinePackage, plan *ResolutionPlan, policy *config.OverlayPolicy) (*PreflightReport, error) {
	if info == nil {
		return nil, fmt.Errorf("overlay preflight: baseline info cannot be nil")
	}
	if plan == nil {
		return nil, fmt.Errorf("overlay preflight: resolution plan cannot be nil")
	}

	effectivePolicy := config.OverlayPolicy{}
	if policy != nil {
		effectivePolicy = *policy
	}

	// A kernel swap regenerates only the GRUB config (see RegenerateGrub) and never
	// touches the ESP or bootloader binary; that is only safe on a GRUB2 baseline.
	// This is the same gate RegenerateGrub enforces, but checked here — before any
	// package is installed or removed — so a systemd-boot or UKI baseline (detected
	// distinctly from grub2 by detectBootloader) fails fast instead of leaving the
	// chroot partially mutated.
	if effectivePolicy.ReplaceKernel != nil && info.Bootloader != "grub2" {
		return nil, fmt.Errorf("overlay preflight: overlayPolicy.replaceKernel is set but the baseline "+
			"bootloader is %q, not grub2; kernel replacement is only supported on GRUB2 baselines", info.Bootloader)
	}

	// The simulate step is an optional validation aid; its failure must not mask
	// the authoritative two-slice decision, so a simulate error is logged and the
	// preflight continues on the two-slice model alone. The default simulator reads
	// the to-install artifacts' declared conflicts against the baseline (see
	// simulateOverlayInstall), catching a declared Conflicts:/Breaks: before the
	// install would abort at unpack time.
	simulated, err := simulateOverlayInstall(info, baseline, plan)
	if err != nil {
		log.Warnf("Overlay preflight: package-manager simulation unavailable, continuing on two-slice model only: %v", err)
		simulated = nil
	}

	// The artifact dependency read is likewise a best-effort aid: it lets the
	// preflight catch an unsatisfiable version pin before the install fails at
	// configure time, but an unreadable artifact must not block the build, so a
	// read error is logged and the preflight proceeds without this net.
	artifactDeps, err := readOverlayArtifactDependencies(info.PackageManager, plan)
	if err != nil {
		log.Warnf("Overlay preflight: could not read artifact dependencies, skipping version-pin check: %v", err)
		artifactDeps = nil
	}

	// The Obsoletes read is a best-effort aid too: it lets the preflight gate an
	// rpm -U obsoletion of a baseline package through AllowRemoval, but an
	// unreadable artifact must not block the build.
	obsoletions, err := readOverlayArtifactObsoletes(info.PackageManager, plan)
	if err != nil {
		log.Warnf("Overlay preflight: could not read artifact Obsoletes, skipping obsoletion check: %v", err)
		obsoletions = nil
	}

	report := EvaluatePreflight(PreflightInput{
		Family:           info.PackageManager,
		Baseline:         baseline,
		Resolved:         plan.ToInstall,
		ResolvedClosure:  plan.Closure,
		SimulatedActions: simulated,
		ArtifactDeps:     artifactDeps,
		Obsoletions:      obsoletions,
		Policy:           effectivePolicy,
	})

	log.Infof("Overlay preflight: %d add, %d upgrade, %d downgrade, %d remove, %d conflict, %d unsatisfiable dep; %d policy violation(s)",
		report.Adds, report.Upgrades, report.Downgrades, report.Removes, report.Conflicts, report.UnsatisfiedDeps, len(report.Violations))

	// Surface the kernel swap explicitly (from -> to) so the operator can see the
	// baseline kernel packages being removed and the concrete replacement being
	// installed. When the request did not resolve to exactly one bootable kernel, log
	// what it matched (nothing, or several) so the operator can correct the pattern.
	if effectivePolicy.ReplaceKernel != nil {
		req := strings.TrimSpace(effectivePolicy.ReplaceKernel.Package)
		switch len(report.ReplacementKernels) {
		case 1:
			var removedKernel []string
			for _, a := range report.Actions {
				if a.KernelReplacement && a.Type == ActionRemove {
					removedKernel = append(removedKernel, a.Package)
				}
			}
			log.Infof("Overlay preflight: replacing kernel in baseline image: removing %d baseline kernel package(s) [%s] -> installing %q (matched by %q)",
				len(removedKernel), strings.Join(removedKernel, ", "), report.ReplacementKernels[0], req)
		case 0:
			log.Warnf("Overlay preflight: overlayPolicy.replaceKernel.package %q did not resolve to any bootable kernel image; nothing to swap", req)
		default:
			log.Warnf("Overlay preflight: overlayPolicy.replaceKernel.package %q is ambiguous — it matched %d bootable kernel images: %s; narrow the pattern to name exactly one",
				req, len(report.ReplacementKernels), strings.Join(report.ReplacementKernels, ", "))
		}
	}

	if report.Blocked {
		return report, fmt.Errorf("overlay preflight failed: %s", formatViolations(report.Violations))
	}
	return report, nil
}

// EvaluatePreflight performs the pure two-slice classification and policy
// enforcement. It is deterministic and side-effect free.
func EvaluatePreflight(in PreflightInput) *PreflightReport {
	sliceA := baselineVersionIndex(in.Baseline)

	// The concrete bootable kernel image(s) the replaceKernel request resolves to. A
	// valid swap must resolve to exactly one; zero (no kernel would remain) or several
	// (an over-broad glob) are surfaced as blocking violations below and never
	// authorize removal of the baseline kernel family.
	var replacementKernels []string
	if in.Policy.ReplaceKernel != nil {
		replacementKernels = replacementKernelImages(
			strings.TrimSpace(in.Policy.ReplaceKernel.Package), in.Resolved, in.ResolvedClosure, sliceA)
	}

	actions := classifyActions(in.Family, sliceA, in.Resolved)
	actions = append(actions, normalizeSimulatedActions(in.SimulatedActions, sliceA)...)
	actions = append(actions, classifyUnsatisfiedDeps(in.Family, sliceA, in.Resolved, in.ArtifactDeps)...)
	actions = append(actions, classifyObsoletions(in.Family, sliceA, in.Obsoletions)...)
	actions = append(actions, classifyKernelReplacementRemovals(sliceA, in.Resolved, in.ResolvedClosure, in.Policy)...)

	// Flag any action that touches a bootloader package so the policy gate can
	// block bootloader replacement regardless of the other knobs. An
	// unsatisfied-dependency action is a diagnostic that the install would fail,
	// not a modification of the bootloader, so it is left unflagged: its own,
	// more specific rule (and the version detail) must be the reported reason
	// even when the declaring package happens to be a bootloader (e.g. systemd-boot).
	for i := range actions {
		if actions[i].Type == ActionUnsatisfiedDep {
			continue
		}
		if isBootloaderPackage(actions[i].Package) {
			actions[i].Bootloader = true
		}
		if isKernelImagePackage(actions[i].Package) {
			actions[i].Kernel = true
		}
	}

	// Mark the replacement kernel's own install action as part of the swap so the
	// kernel-immutable rule permits it. On deb the replacement is a new,
	// version-qualified package name (an ActionAdd, already allowed), but on rpm the
	// installonly kernel keeps the same name ("kernel"/"kernel-core") across versions,
	// so a newer resolved kernel is classified as ActionUpgrade and would otherwise be
	// blocked by ruleKernelImmutable. This is deliberately narrow — only the CONCRETE
	// kernel image the request resolved to (so a glob is matched by its resolved name,
	// not its raw pattern) plus any verified rpm kernel-build companion (e.g. the
	// "kernel" tracking package alongside "kernel-core", the package that actually
	// carries the bootable image — see isSameKernelBuildComponent) — and only an
	// add/upgrade of a kernel image — so an unrelated kernel upgrade stays blocked.
	// Only a request that resolves to exactly one kernel authorizes the swap; zero
	// or several is blocked below.
	if len(replacementKernels) == 1 {
		concrete := replacementKernels[0]
		concreteVersion := kernelImageVersion(concrete, in.Resolved, in.ResolvedClosure)
		for i := range actions {
			a := &actions[i]
			if !a.Kernel || (a.Type != ActionAdd && a.Type != ActionUpgrade) {
				continue
			}
			if a.Package == concrete ||
				isSameKernelBuildComponent(in.Family, concrete, concreteVersion, a.Package, a.RequestedVersion) {
				a.KernelReplacement = true
			}
		}
	}

	// When allowPackageRemoval is opted in, reclassify a DECLARED conflict against a
	// present baseline package into an explicit removal: installing a package that
	// Conflicts:/Breaks: a baseline package (e.g. dracut vs initramfs-tools) is
	// resolved by removing the conflicting baseline package before install.
	//
	// The reclassification is deliberately narrow:
	//   - ConflictWith must be set — only a conflict DECLARED by an install artifact
	//     (classifyConflicts/Obsoletes) names the declaring package. A bare
	//     ActionConflict with no ConflictWith (e.g. classifyActions' uncomparable-
	//     version case) is uncertainty, not a declared conflict, and must NOT be
	//     turned into an approved purge — it stays a conflict for conflictPolicy.
	//   - the target must NOT itself be in the to-install set: removing a package the
	//     overlay is about to (re)install would just reintroduce the conflict, so
	//     such a case is left as a conflict rather than a self-defeating removal.
	//   - bootloader and bootable-kernel packages are never removed — left as
	//     conflicts so the immutability rule still blocks them.
	// This runs after the bootloader/kernel flags are set so the guard can consult them.
	if in.Policy.AllowPackageRemoval {
		toInstallNames := make(map[string]bool, len(in.Resolved))
		for _, rp := range in.Resolved {
			if n := strings.TrimSpace(rp.Name); n != "" {
				toInstallNames[n] = true
			}
		}
		for i := range actions {
			a := &actions[i]
			if a.Type != ActionConflict || a.Bootloader || a.Kernel {
				continue
			}
			if strings.TrimSpace(a.ConflictWith) == "" || toInstallNames[a.Package] {
				continue
			}
			a.Type = ActionRemove
			a.ExplicitRemoval = true
			// The removed package's baseline version is the "current"; there is no
			// requested version for a removal.
			a.RequestedVersion = ""
			if a.Detail == "" {
				a.Detail = fmt.Sprintf("removed to resolve a conflict declared by %q (allowPackageRemoval)", a.ConflictWith)
			}
		}
	}

	sortActions(actions)

	// A kernel replacement self-authorizes only its own kernel-family removals; the
	// install step's reverse-dependency cascade may remove COLLATERAL (non-kernel)
	// orphans only when allowPackageRemoval is explicitly enabled.
	report := &PreflightReport{Actions: actions, CollateralRemovalAuthorized: in.Policy.AllowPackageRemoval}
	// De-duplicate removals by package name: two installed artifacts can conflict
	// with the SAME baseline package, or one artifact can name it in both Conflicts
	// and Breaks, yielding several ActionRemove entries for one package. Without
	// dedup the name would be passed repeatedly to dpkg --purge / rpm -e (the second
	// pass failing on the already-removed package) and the removal count would be
	// inflated. Actions are already sorted, so first-sight order stays deterministic.
	removeSeen := make(map[string]bool)
	toRemoveSeen := make(map[string]bool)
	for _, a := range actions {
		switch a.Type {
		case ActionAdd:
			report.Adds++
		case ActionUpgrade:
			report.Upgrades++
		case ActionDowngrade:
			report.Downgrades++
		case ActionRemove:
			// Count and list each removed package once, even if several conflict/Breaks/
			// Obsoletes actions target it. The three dedup checks are INDEPENDENT (not a
			// single early-continue): a package can appear both as a non-explicit
			// obsoletion and as an explicit conflict-driven removal, and it must still
			// reach ToRemove for the explicit case regardless of which entry sorts first.
			//
			// A policy-permitted removal leaves the baseline (a blocked removal is not
			// recorded — the build will not proceed anyway). Two lists track it:
			//   - ToRemove: every approved removal the install step must perform
			//     EXPLICITLY. This is ALL approved removals EXCEPT the rpm
			//     Obsoletes-driven ones (which `rpm -U` erases implicitly, so an
			//     explicit re-remove is redundant and would fail once it is already
			//     gone). A conflict-driven reclassification (ExplicitRemoval) and any
			//     simulator-surfaced ActionRemove are both queued here — the discriminator
			//     is ObsoletesDriven, NOT ExplicitRemoval, so a removal whose origin does
			//     not set ExplicitRemoval is executed rather than silently assumed to
			//     happen on its own (which would leave the package installed while stats
			//     and the complete SBOM report it removed).
			//   - ApprovedRemovals: EVERY approved removal (explicit + simulator +
			//     Obsoletes), the set actually absent from the final image, used for
			//     stats and the complete-SBOM exclusion.
			//
			// A kernel-replacement removal (KernelReplacement) is approved without
			// allowPackageRemoval — replaceKernel self-authorizes the kernel-family
			// swap — so it too is recorded in ApprovedRemovals and ToRemove.
			approved := in.Policy.AllowPackageRemoval || a.KernelReplacement
			if !removeSeen[a.Package] {
				removeSeen[a.Package] = true
				report.Removes++
				if approved {
					report.ApprovedRemovals = append(report.ApprovedRemovals, a.Package)
				}
			}
			if approved && !a.ObsoletesDriven && !toRemoveSeen[a.Package] {
				toRemoveSeen[a.Package] = true
				report.ToRemove = append(report.ToRemove, a.Package)
			}
		case ActionConflict:
			report.Conflicts++
		case ActionUnsatisfiedDep:
			report.UnsatisfiedDeps++
		}
		if rule, blocked := violatedRule(a, in.Policy); blocked {
			report.Violations = append(report.Violations, PolicyViolation{Action: a, Rule: rule})
		}
	}

	// A kernel replacement must resolve to EXACTLY ONE bootable kernel image via the
	// request, or the swap would remove the baseline kernel family and leave the image
	// with either no kernel or an ambiguous choice of default. classifyKernelReplacementRemovals
	// already declined to authorize the removals in either case; raise a blocking
	// violation so the operator sees which packages (if any) the request matched and
	// can correct it, rather than silently producing a no-op or an unbootable image.
	if in.Policy.ReplaceKernel != nil {
		req := strings.TrimSpace(in.Policy.ReplaceKernel.Package)
		switch {
		case len(replacementKernels) == 0:
			report.Violations = append(report.Violations, PolicyViolation{
				Action: PlannedAction{
					Type:    ActionAdd,
					Package: req,
					Detail:  "overlayPolicy.replaceKernel.package does not resolve to a bootable kernel image, so no kernel would remain after the swap",
				},
				Rule: ruleReplaceKernelInvalid,
			})
		case len(replacementKernels) > 1:
			report.Violations = append(report.Violations, PolicyViolation{
				Action: PlannedAction{
					Type:    ActionAdd,
					Package: req,
					Detail: fmt.Sprintf("matched %d bootable kernel images (%s); narrow overlayPolicy.replaceKernel.package to name exactly one",
						len(replacementKernels), strings.Join(replacementKernels, ", ")),
				},
				Rule: ruleReplaceKernelAmbig,
			})
		default:
			// Exactly one concrete kernel resolved from `package` (and its closure). But
			// additionalPackages — or any other requested package — can independently
			// resolve to ANOTHER bootable kernel image; GRUB is defaulted to entry 0 on
			// the assumption the chosen replacement is the only new kernel, so a second
			// one installed alongside it would leave the wrong kernel bootable.
			// kernelImagesIn's alias-vs-concrete filtering means a legitimate
			// kernel-meta swap's own intermediary alias package is never mistaken for a
			// second kernel here. A verified rpm kernel-build companion (e.g.
			// "kernel-core" alongside a "kernel" request — both share one NEVRA) is
			// likewise excluded via isSameKernelBuildComponent: it is part of the SAME
			// build as the chosen replacement, not an independently requested second
			// kernel.
			chosen := replacementKernels[0]
			chosenVersion := kernelImageVersion(chosen, in.Resolved, in.ResolvedClosure)
			var extra []string
			for _, img := range kernelImagesIn(in.Resolved, in.ResolvedClosure) {
				if img == chosen {
					continue
				}
				imgVersion := kernelImageVersion(img, in.Resolved, in.ResolvedClosure)
				if isSameKernelBuildComponent(in.Family, chosen, chosenVersion, img, imgVersion) {
					continue
				}
				extra = append(extra, img)
			}
			if len(extra) > 0 {
				report.Violations = append(report.Violations, PolicyViolation{
					Action: PlannedAction{
						Type:    ActionAdd,
						Package: req,
						Detail: fmt.Sprintf("resolves to %q, but the install set also supplies additional bootable kernel image(s) %s; drop them from additionalPackages/systemConfig.packages or narrow replaceKernel.package to cover them",
							chosen, strings.Join(extra, ", ")),
					},
					Rule: ruleReplaceKernelAmbig,
				})
			}
		}
	}
	report.ReplacementKernels = replacementKernels

	report.Blocked = len(report.Violations) > 0
	return report
}

// classifyActions derives add/upgrade/downgrade actions from the two slices by
// walking the resolved set (Slice B) against the baseline index (Slice A).
// Packages present in the baseline at the same version yield no action; packages
// in the baseline but absent from the resolved set are left untouched (overlay is
// additive-only), so removals never originate here — they arrive via the
// simulate aid.
func classifyActions(family PackageManager, sliceA map[string]BaselinePackage, resolved []ResolvedPackage) []PlannedAction {
	var actions []PlannedAction
	for _, rp := range resolved {
		name := strings.TrimSpace(rp.Name)
		if name == "" {
			continue
		}
		base, present := sliceA[name]
		if !present {
			actions = append(actions, PlannedAction{
				Type:             ActionAdd,
				Package:          name,
				RequestedVersion: rp.Version,
				Arch:             rp.Arch,
			})
			continue
		}

		cmp, err := comparePkgVersions(family, rp.Version, base.Version)
		if err != nil {
			// Direction is undeterminable, so we cannot prove this is a safe
			// upgrade. Treat it as a conflict (conservative: blocked by the
			// default fail policy) rather than silently assuming an upgrade.
			actions = append(actions, PlannedAction{
				Type:             ActionConflict,
				Package:          name,
				CurrentVersion:   base.Version,
				RequestedVersion: rp.Version,
				Arch:             rp.Arch,
				Detail:           fmt.Sprintf("version comparison failed: %v", err),
			})
			continue
		}
		switch {
		case cmp > 0:
			actions = append(actions, PlannedAction{
				Type:             ActionUpgrade,
				Package:          name,
				CurrentVersion:   base.Version,
				RequestedVersion: rp.Version,
				Arch:             rp.Arch,
			})
		case cmp < 0:
			actions = append(actions, PlannedAction{
				Type:             ActionDowngrade,
				Package:          name,
				CurrentVersion:   base.Version,
				RequestedVersion: rp.Version,
				Arch:             rp.Arch,
			})
			// cmp == 0: package already present at the requested version, no action.
		}
	}
	return actions
}

// classifyUnsatisfiedDeps flags to-install packages whose version-pinned
// dependency names a package that is present after install (in the baseline or
// in the to-install set) but at a version the pin rejects. It checks against the
// post-install state, so a pin satisfied by a co-installed package — including a
// baseline package the overlay is upgrading in additive-and-upgrade mode — is
// correctly NOT flagged. It fires only when the satisfying version is absent from
// the post-install set: a strict pin against an older baseline version with no
// newer copy being installed (e.g. systemd-boot's "libsystemd-shared (= X)"
// against baseline version Y), which the package manager cannot meet, so it fails
// at its configure step.
//
// It deliberately does NOT flag an edge whose package is entirely absent: those
// are typically satisfied by a Provides/virtual capability the artifact metadata
// does not expose here, and flagging them would produce false positives. The
// check targets only the present-but-wrong-version case, which is unambiguous.
func classifyUnsatisfiedDeps(family PackageManager, sliceA map[string]BaselinePackage, resolved []ResolvedPackage, deps []ArtifactDependency) []PlannedAction {
	if len(deps) == 0 {
		return nil
	}

	// Post-install version index: the baseline overlaid with what to-install adds.
	// A dependency is checked against the state that will exist after install, so a
	// pin satisfied by a co-installed to-install package is correctly not flagged.
	postInstall := postInstallVersionIndex(sliceA, resolved)

	var actions []PlannedAction
	for _, dep := range deps {
		unmet, ok := unsatisfiedVersionedAlternative(family, dep.Alternatives, postInstall)
		if !ok {
			continue
		}
		actions = append(actions, PlannedAction{
			Type:             ActionUnsatisfiedDep,
			Package:          dep.Package,
			CurrentVersion:   postInstall[unmet.Name],
			RequestedVersion: unmet.Constraint.Op + " " + unmet.Constraint.Ver,
			ConflictWith:     unmet.Name,
			Detail: fmt.Sprintf("requires %s (%s %s) but the post-install set has %s and no satisfying version is being installed",
				unmet.Name, unmet.Constraint.Op, unmet.Constraint.Ver, postInstall[unmet.Name]),
		})
	}
	return actions
}

// classifyObsoletions turns each rpm Obsoletes: on a present baseline package
// into an ActionRemove, so the AllowRemoval gate governs an obsoletion that
// `rpm -U` would otherwise perform silently at install time. An Obsoletes whose
// target is absent from the baseline is a no-op (nothing to erase) and is
// skipped; a versioned Obsoletes only fires when the baseline version falls
// within the obsoleted range.
func classifyObsoletions(family PackageManager, sliceA map[string]BaselinePackage, obsoletions []ArtifactObsoletion) []PlannedAction {
	var actions []PlannedAction
	for _, o := range obsoletions {
		target := strings.TrimSpace(o.Obsoletes.Name)
		if target == "" {
			continue
		}
		base, present := sliceA[target]
		if !present {
			continue // nothing installed under this name to obsolete
		}
		// A versioned Obsoletes only erases the baseline copy when its version
		// satisfies the constraint; an uncomparable version is treated
		// conservatively as a potential removal (better to gate than to miss it).
		if c := o.Obsoletes.Constraint; c != nil {
			if cmp, err := comparePkgVersions(family, base.Version, c.Ver); err == nil && !constraintSatisfied(c.Op, cmp) {
				continue
			}
		}
		actions = append(actions, PlannedAction{
			Type:            ActionRemove,
			Package:         target,
			CurrentVersion:  base.Version,
			Arch:            base.Arch,
			ConflictWith:    o.Package,
			ObsoletesDriven: true, // rpm -U erases it implicitly; the install step must NOT re-remove it
			Detail:          fmt.Sprintf("obsoleted by %q, which rpm -U would erase from the baseline", o.Package),
		})
	}
	return actions
}

// classifyKernelReplacementRemovals emits an ActionRemove for every installed
// baseline package in the old-kernel family when overlayPolicy.replaceKernel is
// set — the "full swap" side of a kernel replacement. The new kernel is added
// through the normal resolve/ToInstall path (an ActionAdd), and the baseline
// kernel family is removed here so the image ships only the new kernel.
//
// The removal set is every installed baseline package matched by
// isKernelFamilyPackage, MINUS anything the overlay is itself installing (the
// resolved ToInstall set) and the named replacement package — so a kernel-family
// package the new kernel legitimately (re)installs is never removed. Removing the
// COMPLETE family (image + meta + modules + headers) as one batch is deliberate:
// it leaves no kernel package orphaned, so the install step's post-removal cascade
// never has to remove a kernel package (which its guard forbids).
//
// Each emitted action is marked KernelReplacement so violatedRule permits it (past
// both the kernel-immutable rule and the allowPackageRemoval gate) and the report
// records it in ToRemove/ApprovedRemovals. It returns nil when replaceKernel is
// unset, so a normal overlay build is entirely unaffected. Map iteration order is
// irrelevant: sortActions orders the result deterministically downstream.
//
// It also returns nil (authorizing NO removals) unless the request resolves to
// EXACTLY ONE bootable kernel image: a replaceKernel package that resolves to a
// non-kernel (e.g. "curl") or to several kernels (an over-broad glob) must not purge
// the baseline kernel family and leave an unbootable or ambiguous image.
// EvaluatePreflight raises the blocking violation for those cases
// (ruleReplaceKernelInvalid / ruleReplaceKernelAmbig); refusing the removals here is
// the belt-and-suspenders.
//
// The keep-set is built from the FULL resolved closure (not just ToInstall) plus the
// concrete replacement kernel, so a kernel-family package the replacement depends on
// but that is already present in the baseline — and thus absent from ToInstall — is
// not swept into the removal set. The old baseline kernel family is not in the new
// kernel's closure, so it is still removed.
func classifyKernelReplacementRemovals(sliceA map[string]BaselinePackage, resolved, closure []ResolvedPackage, policy config.OverlayPolicy) []PlannedAction {
	if policy.ReplaceKernel == nil {
		return nil
	}
	replacement := strings.TrimSpace(policy.ReplaceKernel.Package)
	kernels := replacementKernelImages(replacement, resolved, closure, sliceA)
	if len(kernels) != 1 {
		return nil
	}

	keep := make(map[string]bool, len(resolved)+len(closure)+1)
	for _, rp := range resolved {
		if n := strings.TrimSpace(rp.Name); n != "" {
			keep[n] = true
		}
	}
	for _, rp := range closure {
		if n := strings.TrimSpace(rp.Name); n != "" {
			keep[n] = true
		}
	}
	keep[kernels[0]] = true

	var actions []PlannedAction
	for name, base := range sliceA {
		if keep[name] || !isKernelFamilyPackage(name) {
			continue
		}
		actions = append(actions, PlannedAction{
			Type:              ActionRemove,
			Package:           name,
			CurrentVersion:    base.Version,
			Arch:              base.Arch,
			ExplicitRemoval:   true,
			KernelReplacement: true,
			Detail:            fmt.Sprintf("removed as part of kernel replacement by %q", kernels[0]),
		})
	}
	return actions
}

// matchReplacementRequest reports whether a concrete package name satisfies the
// replaceKernel request. The request is an exact package name unless it contains a
// glob metacharacter (*, ?, [...]), in which case it is matched with the same
// shell-glob semantics the package resolver accepts.
//
// RPM wildcard resolution (rpmutils.ResolveWildcardPackageConflicts) canonicalizes
// a version-specific glob match down to its base package name — e.g.
// "kernel-[0-9]*" matches the artifact "kernel-5.14.0-570...x86_64.rpm" but
// resolves to the plain ResolvedPackage.Name "kernel" — so the canonical name a
// glob is later re-matched against here may no longer contain the version text
// the pattern targeted, and a direct path.Match against it fails even though the
// resolver genuinely satisfied the request. As a fallback, a name is also
// accepted when it exactly equals the glob's literal prefix (the text before its
// first metacharacter) with a trailing "-" separator trimmed — e.g.
// "kernel-[0-9]*" additionally accepts "kernel". The fallback applies ONLY when
// everything from that metacharacter onward is plausible version-glob syntax
// (digits, ".", "-", "_", and glob metacharacters); a pattern carrying further
// literal name text after the wildcard (e.g. "kernel*headers") never qualifies,
// so a resolved "kernel" can never be misattributed to it. This keeps the
// fallback narrow enough that it can never turn one glob into matching several
// unrelated packages; a Debian glob's canonical resolved name always retains the
// version text a real match requires, so the fallback is inert there.
func matchReplacementRequest(request, name string) bool {
	request = strings.TrimSpace(request)
	name = strings.TrimSpace(name)
	if request == "" || name == "" {
		return false
	}
	if !strings.ContainsAny(request, "*?[") {
		return name == request
	}
	ok, err := path.Match(request, name)
	if err != nil {
		// Invalid glob syntax (e.g. an unclosed "[") matches nothing, including via
		// the canonical-name fallback below.
		return false
	}
	if ok {
		return true
	}
	fallback := globCanonicalFallbackName(request)
	return fallback != "" && name == fallback
}

// globCanonicalFallbackName returns the literal text before a glob pattern's
// first metacharacter, with a trailing "-" package-name separator trimmed (e.g.
// "kernel-[0-9]*" -> "kernel"), but ONLY when everything from that
// metacharacter to the end of the pattern is plausible RPM version-glob syntax
// (digits, ".", "-", "_", and glob metacharacters). A pattern that carries
// further literal name text after the wildcard (e.g. "kernel*headers", whose
// "headers" text a canonicalized "kernel" package name never carries) returns
// "", meaning the fallback does not apply. See matchReplacementRequest.
func globCanonicalFallbackName(pattern string) string {
	i := strings.IndexAny(pattern, "*?[")
	if i == -1 {
		return ""
	}
	lit, tail := pattern[:i], pattern[i:]
	for _, r := range tail {
		switch {
		case r >= '0' && r <= '9':
		case r == '.' || r == '-' || r == '_' || r == '*' || r == '?' || r == '[' || r == ']':
		default:
			return ""
		}
	}
	return strings.TrimSuffix(lit, "-")
}

// kernelImagesIn returns the sorted, de-duplicated bootable kernel-image packages
// present across the given resolved sets.
//
// A Debian/Ubuntu kernel meta flavor (e.g. "linux-oem-24.04") commonly resolves a
// closure containing BOTH an intermediary "linux-image-<flavor>" alias package
// (e.g. "linux-image-oem-24.04", itself no more bootable than the top-level meta)
// AND the concrete, per-build image it in turn depends on (e.g.
// "linux-image-6.11.0-1004-oem"). Both match isKernelImagePackage's "linux-image"
// prefix, so naively collecting every match here would report two "replacement
// kernels" for a single valid meta-driven swap. isLinuxImageMetaAlias filters the
// alias out whenever a concrete image is also present, so only the real bootable
// image is returned; the alias is kept only as a fallback when it is the sole
// "linux-image"-prefixed match found (so a genuinely ambiguous/empty result is
// never manufactured by over-filtering).
func kernelImagesIn(sets ...[]ResolvedPackage) []string {
	seen := map[string]bool{}
	var images, aliases []string
	for _, set := range sets {
		for _, rp := range set {
			n := strings.TrimSpace(rp.Name)
			if n == "" || !isKernelImagePackage(n) || seen[n] {
				continue
			}
			seen[n] = true
			if isLinuxImageMetaAlias(n) {
				aliases = append(aliases, n)
				continue
			}
			images = append(images, n)
		}
	}
	if len(images) == 0 {
		images = aliases
	}
	sort.Strings(images)
	return images
}

// kernelImageVersion returns the Version recorded for a package name across the
// given resolved sets, or "" if the name is not found in any of them.
func kernelImageVersion(name string, sets ...[]ResolvedPackage) string {
	for _, set := range sets {
		for _, rp := range set {
			if strings.TrimSpace(rp.Name) == name {
				return strings.TrimSpace(rp.Version)
			}
		}
	}
	return ""
}

// rpmKernelComponentPrefixes are the subset of kernelImagePackagePrefixes that
// name an individual rpm per-build kernel package. "linux-image" is deliberately
// excluded: unlike rpm's "kernel"/"kernel-core" pair (which always share one
// NEVRA), a same-Version match between two Debian packages does not establish
// they are the same build — see isSameKernelBuildComponent.
var rpmKernelComponentPrefixes = []string{
	"kernel-image",
	"kernel-core",
	"kernel",
}

// isRpmKernelComponentPackage reports whether name is one of the recognized rpm
// per-build kernel component packages (see rpmKernelComponentPrefixes).
func isRpmKernelComponentPackage(name string) bool {
	if kernelSafeExactNames[strings.ToLower(strings.TrimSpace(name))] {
		return false
	}
	return matchesPackagePrefix(name, rpmKernelComponentPrefixes)
}

// isSameKernelBuildComponent reports whether candidate is a verified companion of
// concrete within the SAME rpm kernel build: rpm's "kernel" tracking package and
// its "kernel-core" companion (the package that actually carries the bootable
// image) always share one NEVRA, so a request naming one must also authorize the
// other's own install/upgrade and must not count it as an independent second
// kernel. This is deliberately narrow to the verified rpm pairing — NOT a bare
// "same Version" shortcut — since neither its package-manager family nor its
// name/component scoping may be relaxed:
//   - family: rpm (PackageManagerDNF) only. Debian's own alias/concrete pairing
//     is resolved by NAME through kernelImagesIn, not by version, because a deb
//     meta and its concrete dependency do not reliably share a version string;
//     applying a same-version shortcut there could instead pair two
//     independently built images that coincidentally share one.
//   - name: both concrete and candidate must be recognized rpm kernel component
//     names (isRpmKernelComponentPackage), never any isKernelImagePackage match —
//     otherwise a distinct rpm kernel build that happens to share a version
//     string with the chosen one would be wrongly treated as a companion.
func isSameKernelBuildComponent(family PackageManager, concrete, concreteVersion, candidate, candidateVersion string) bool {
	if family != PackageManagerDNF || concreteVersion == "" || candidateVersion == "" {
		return false
	}
	if !isRpmKernelComponentPackage(concrete) || !isRpmKernelComponentPackage(candidate) {
		return false
	}
	return candidateVersion == concreteVersion
}

// isLinuxImageMetaAlias reports whether a "linux-image-"-prefixed name is a
// flavor alias (e.g. "linux-image-generic", "linux-image-oem-24.04") rather than
// a concrete, per-build bootable image (e.g. "linux-image-6.11.0-1004-oem" or
// "linux-image-6.8.0-40-generic"). A concrete image's version segment right after
// the prefix always starts with a digit; an alias's flavor name starts with a
// letter. Ubuntu also ships a parallel "unsigned" variant of the concrete image
// (e.g. "linux-image-unsigned-6.17.0-1017-oem"), so that optional segment is
// stripped before the digit check, or it would be misread as a flavor name. Names
// outside the "linux-image-" prefix (rpm's bare "kernel"/"kernel-core", which
// carry no such alias tier) are never aliases here.
func isLinuxImageMetaAlias(name string) bool {
	const prefix = "linux-image-"
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	rest := strings.TrimPrefix(name[len(prefix):], "unsigned-")
	return rest == "" || rest[0] < '0' || rest[0] > '9'
}

// replacementKernelImages returns the concrete bootable kernel-image packages that
// overlayPolicy.replaceKernel.package resolves to. The request may name an exact
// package or use the resolver's glob syntax (*, ?, [...]). The result drives both the
// exactly-one-kernel invariant enforced in EvaluatePreflight and the removal
// authorization: a request that resolves to zero kernels (a typo, or a non-kernel
// package) or to several (an over-broad glob) is rejected there rather than silently
// purging the baseline kernel family.
//
// An EXACT request is classified by name (matching the historical behavior): a name
// that is itself a bootable kernel image supplies that kernel directly — valid whether
// it is being installed or is already present in the baseline (a removal-only swap) —
// EXCEPT a Debian flavor alias (e.g. "linux-image-generic"), which is resolved to its
// concrete per-build image the same way the glob/meta paths are, so it is never
// treated as a package distinct from the concrete image the extra-kernel-image check
// below expects; a kernel meta of another family (e.g. linux-oem) supplies the
// bootable kernel image its resolution pulls into the install set or closure.
//
// A GLOB request is resolved against CONCRETE package names — first those in the
// install set and its closure, so the swap logic never treats a raw pattern as a
// package: matched names that are bootable kernel images ARE the replacement kernels;
// if none are but the pattern matches a kernel meta, the bootable image its resolution
// pulls in is used instead. This prevents an over-broad pattern such as
// "kernel-headers*" (which matches only a userspace package) from being mistaken for a
// bootable kernel and authorizing removal of the real baseline kernel. The baseline is
// consulted ONLY when the install set and closure produced no concrete match at all
// (the removal-only case, where the requested kernel is already installed) — never
// combined with a resolved match, since an ordinary same-flavor upgrade would
// otherwise make the old (not-yet-removed) baseline kernel and the newly resolved one
// both match the glob and look ambiguous.
func replacementKernelImages(request string, resolved, closure []ResolvedPackage, sliceA map[string]BaselinePackage) []string {
	request = strings.TrimSpace(request)
	if request == "" {
		return nil
	}

	if !strings.ContainsAny(request, "*?[") {
		if isKernelImagePackage(request) {
			if isLinuxImageMetaAlias(request) {
				if imgs := kernelImagesIn(resolved, closure); len(imgs) > 0 {
					return imgs
				}
			}
			return []string{request}
		}
		if isKernelFamilyPackage(request) {
			return kernelImagesIn(resolved, closure)
		}
		return nil
	}

	// Glob: resolve against the concrete package names we know will be present.
	seen := map[string]bool{}
	var matched []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] || !matchReplacementRequest(request, name) {
			return
		}
		seen[name] = true
		matched = append(matched, name)
	}
	for _, rp := range resolved {
		add(rp.Name)
	}
	for _, rp := range closure {
		add(rp.Name)
	}

	imgSeen := map[string]bool{}
	var images []string
	// metaOrAliasMatched tracks a matched kernel-family meta (e.g. "linux-oem") OR a
	// matched Debian flavor ALIAS (e.g. "linux-image-generic"): neither is itself
	// bootable, so either is resolved through kernelImagesIn to find the concrete
	// per-build image it depends on, exactly like the exact-request path above. A
	// glob such as "linux-image-[g]eneric" that matches only the alias must not
	// return the alias name directly — kernelImagesIn's alias-filtering is what
	// keeps that concrete dependency from later being misread as a second kernel.
	metaOrAliasMatched := false
	for _, m := range matched {
		switch {
		case isKernelImagePackage(m):
			if isLinuxImageMetaAlias(m) {
				metaOrAliasMatched = true
				continue
			}
			if !imgSeen[m] {
				imgSeen[m] = true
				images = append(images, m)
			}
		case isKernelFamilyPackage(m):
			metaOrAliasMatched = true
		}
	}
	if len(images) > 0 {
		sort.Strings(images)
		return images
	}
	if metaOrAliasMatched {
		return kernelImagesIn(resolved, closure)
	}
	// Resolution (install set + closure) produced no concrete match at all — this is
	// the removal-only case, where the requested kernel is already installed and
	// nothing new is being resolved. Only THEN fall back to the baseline: consulting
	// it unconditionally would make an ordinary same-flavor upgrade look ambiguous,
	// since the old (not-yet-removed) baseline kernel and the newly resolved one can
	// both match a glob like "linux-image-*-oem".
	for name := range sliceA {
		if !matchReplacementRequest(request, name) || !isKernelImagePackage(name) || imgSeen[name] {
			continue
		}
		imgSeen[name] = true
		images = append(images, name)
	}
	sort.Strings(images)
	return images
}

// classifyConflicts turns each declared Conflicts:/Breaks: (deb) or Conflicts:
// (rpm) on a present baseline package — OR on another package this same overlay
// is adding — into an ActionConflict, so conflictPolicy gates a conflict that the
// package manager would otherwise only reveal by aborting at unpack time. A
// conflict whose target is neither installed in the baseline nor being added by
// the overlay is a no-op (nothing to clash with) and is skipped.
//
// The new-vs-new case matters because the resolver can pull two conflicting
// packages into the same install set (historically only baseline targets were
// checked here, so two newly-added conflicting packages slipped through to dpkg
// unpack — e.g. the free and non-free intel-media-va-driver both being installed).
//
// A versioned conflict is evaluated against the POST-INSTALL version of the
// target, not the baseline version: a Breaks:/Conflicts: bound to a version range
// (e.g. vim-runtime's "Breaks: vim-tiny (<< 9.1.0016-1ubuntu7.17)") is a lockstep
// upgrade marker, and when the overlay upgrades that target to a satisfying
// version in the SAME batch the range no longer covers it, so there is no real
// conflict — dpkg's --auto-deconfigure resolves the transient break at unpack
// time. Checking the baseline version alone would spuriously block that upgrade.
// An uncomparable version is treated conservatively as a potential conflict
// (better to gate than to miss it).
func classifyConflicts(family PackageManager, sliceA map[string]BaselinePackage, resolved []ResolvedPackage, conflicts []ArtifactConflict) []PlannedAction {
	postInstall := postInstallVersionIndex(sliceA, resolved)
	// Index the to-install set so a conflict whose target is not in the baseline can
	// still be gated when the overlay is adding that target in the same batch.
	addedByName := make(map[string]ResolvedPackage, len(resolved))
	for _, rp := range resolved {
		addedByName[rp.Name] = rp
	}
	// Index every virtual name a to-install package Provides, so a conflict declared
	// against a virtual name a co-added package satisfies is gated too (dpkg would
	// otherwise reject the batch — e.g. two packages that both Provide and Conflict
	// "mail-transport-agent"). The declaring package is excluded at lookup time so a
	// package that Provides and Conflicts the same name does not clash with itself.
	providedByName := make(map[string][]ResolvedPackage)
	for _, rp := range resolved {
		for _, prov := range rp.Provides {
			if prov = strings.TrimSpace(prov); prov != "" && prov != rp.Name {
				providedByName[prov] = append(providedByName[prov], rp)
			}
		}
	}

	var actions []PlannedAction
	for _, c := range conflicts {
		target := strings.TrimSpace(c.Conflicts.Name)
		if target == "" {
			continue
		}
		// A package declaring a conflict against itself is not a real clash.
		if target == c.Package {
			continue
		}
		base, present := sliceA[target]
		added, adding := addedByName[target]
		reportTarget := target
		if !present && !adding {
			// Not a real package on either side, but a co-added package may satisfy it as
			// a virtual name (e.g. two packages that both Provide and Conflict
			// "mail-transport-agent"). Gate that, but report/act on the REAL provider
			// package — not the virtual name — so a removal-enabled policy targets a
			// package that actually exists. A VERSIONED conflict is not matched here:
			// this resolver carries only unversioned Provides, and per Debian policy a
			// versioned conflict does not match an unversioned virtual provider.
			provider, ok := firstProviderExcluding(providedByName[target], c.Package)
			if !ok || c.Conflicts.Constraint != nil {
				continue
			}
			added, adding = provider, true
			reportTarget = provider.Name
		}
		// A versioned conflict only clashes when the version that will be present
		// after install falls within the declared range; a version outside it — most
		// commonly because the overlay upgrades the target in the same batch — is not
		// a conflict.
		if vc := c.Conflicts.Constraint; vc != nil {
			if cmp, err := comparePkgVersions(family, postInstall[reportTarget], vc.Ver); err == nil && !constraintSatisfied(vc.Op, cmp) {
				continue
			}
		}
		// Report the version/arch of the copy that will be present AFTER install: when
		// the overlay adds or upgrades the target in this same batch, that is the version
		// it installs (added), not the baseline copy it supersedes; only an untouched
		// baseline target reports base.Version. The new-vs-new case (target added but not
		// in the baseline) also gets a distinct diagnostic.
		curVersion, arch := base.Version, base.Arch
		detail := fmt.Sprintf("declared as a conflict by %q, which would abort the install at unpack time", c.Package)
		if adding {
			curVersion, arch = added.Version, added.Arch
		}
		if !present {
			detail = fmt.Sprintf("declared as a conflict by %q; both are added by this overlay, which would abort the install at unpack time", c.Package)
		}
		actions = append(actions, PlannedAction{
			Type:           ActionConflict,
			Package:        reportTarget,
			CurrentVersion: curVersion,
			Arch:           arch,
			ConflictWith:   c.Package,
			Detail:         detail,
		})
	}
	return actions
}

// firstProviderExcluding returns the first package in providers whose name is not
// exclude, so a package that Provides and Conflicts the same virtual name is not
// treated as conflicting with itself.
func firstProviderExcluding(providers []ResolvedPackage, exclude string) (ResolvedPackage, bool) {
	for _, p := range providers {
		if p.Name != exclude {
			return p, true
		}
	}
	return ResolvedPackage{}, false
}

// postInstallVersionIndex builds a name→version map of the state that will exist
// after the overlay install: the baseline overlaid with the versions the overlay
// will install (which supersede the baseline copy for any upgraded package). It
// backs the post-install checks in classifyConflicts and classifyUnsatisfiedDeps.
func postInstallVersionIndex(sliceA map[string]BaselinePackage, resolved []ResolvedPackage) map[string]string {
	postInstall := make(map[string]string, len(sliceA)+len(resolved))
	for name, bp := range sliceA {
		postInstall[name] = bp.Version
	}
	for _, rp := range resolved {
		if name := strings.TrimSpace(rp.Name); name != "" {
			postInstall[name] = rp.Version
		}
	}
	return postInstall
}

// unsatisfiedVersionedAlternative reports whether a dependency edge is blocked by
// the present-but-wrong-version case, returning the offending alternative. An
// edge holds if ANY alternative is satisfied, so it is unsatisfied only when
// every alternative fails. It returns ok=true only when at least one alternative
// names a present package with a versioned pin that its installed version
// violates AND no alternative is satisfied — i.e. a genuine, unavoidable miss.
// Edges with an unversioned or absent-package alternative are treated as
// potentially satisfiable (returns ok=false) to avoid Provides/virtual false
// positives.
func unsatisfiedVersionedAlternative(family PackageManager, alts []DependencyAlternative, postInstall map[string]string) (DependencyAlternative, bool) {
	var offending DependencyAlternative
	haveOffending := false

	for _, alt := range alts {
		installedVer, present := postInstall[alt.Name]

		// An unversioned alternative keeps the edge potentially satisfiable: if the
		// package is present the edge holds outright, and if it is absent it may
		// still be met via a Provides we cannot see here. Either way it is not a
		// provable version miss, so the whole edge is treated as met.
		if alt.Constraint == nil {
			return DependencyAlternative{}, false
		}

		// A versioned alternative on an absent package cannot be proven unsatisfiable
		// (a Provides could carry the version), so it keeps the edge open.
		if !present {
			return DependencyAlternative{}, false
		}

		cmp, err := comparePkgVersions(family, installedVer, alt.Constraint.Ver)
		if err != nil {
			// Uncomparable versions: cannot prove a violation, so do not flag.
			return DependencyAlternative{}, false
		}
		if constraintSatisfied(alt.Constraint.Op, cmp) {
			// This alternative is satisfied, so the whole edge holds.
			return DependencyAlternative{}, false
		}
		// This alternative is present but at a rejecting version; remember it in case
		// no other alternative rescues the edge.
		if !haveOffending {
			offending = alt
			haveOffending = true
		}
	}
	return offending, haveOffending
}

// constraintSatisfied reports whether an installed-vs-required comparison result
// (cmp = sign of installed - required) satisfies a Debian/RPM version operator.
func constraintSatisfied(op string, cmp int) bool {
	switch op {
	case "=", "==":
		return cmp == 0
	case ">=":
		return cmp >= 0
	case "<=":
		return cmp <= 0
	case ">>", ">":
		return cmp > 0
	case "<<", "<":
		return cmp < 0
	default:
		// Unknown operator: do not claim a violation.
		return true
	}
}

// normalizeSimulatedActions filters simulator-reported actions to the
// remove/conflict classes (the two-slice comparison owns add/upgrade/downgrade)
// and fills in the baseline version for removals when the simulator omitted it.
// Package names are trimmed and empty ones dropped (mirroring classifyActions),
// so a blank or whitespace-padded name from the simulator can neither slip into
// the report nor break baseline backfill, bootloader detection, or sorting.
func normalizeSimulatedActions(simulated []PlannedAction, sliceA map[string]BaselinePackage) []PlannedAction {
	var out []PlannedAction
	for _, a := range simulated {
		if a.Type != ActionRemove && a.Type != ActionConflict {
			continue
		}
		a.Package = strings.TrimSpace(a.Package)
		if a.Package == "" {
			continue
		}
		if a.CurrentVersion == "" {
			if base, ok := sliceA[a.Package]; ok {
				a.CurrentVersion = base.Version
			}
		}
		out = append(out, a)
	}
	return out
}

// violatedRule returns the policy rule an action violates, if any. Bootloader
// replacement is checked first (it is unconditional and the most severe), then
// the per-class knobs. Each action yields at most one violation.
func violatedRule(a PlannedAction, policy config.OverlayPolicy) (string, bool) {
	// Adds and no-ops are always permitted unless they would modify the
	// bootloader; only state-changing actions on bootloader packages are blocked.
	if a.Bootloader && a.Type != ActionAdd {
		return ruleBootloaderImmutable, true
	}
	// The bootable kernel image is likewise immutable: adding a new kernel
	// alongside the existing one is allowed, but upgrading/replacing/removing an
	// installed kernel image is blocked because boot regeneration only refreshes
	// the initramfs, not the bootloader's menu entries for a changed kernel. The
	// sole exception is a removal sanctioned by overlayPolicy.replaceKernel (a full
	// swap), where the GRUB config IS regenerated for the new kernel — that removal
	// carries KernelReplacement and is permitted here.
	if a.Kernel && a.Type != ActionAdd && !a.KernelReplacement {
		return ruleKernelImmutable, true
	}

	switch a.Type {
	case ActionRemove:
		// A kernel-replacement removal (overlayPolicy.replaceKernel) is self-
		// authorizing: it does not require allowPackageRemoval, which governs only
		// conflict-driven removal of non-kernel baseline packages.
		if a.KernelReplacement {
			break
		}
		if !policy.AllowPackageRemoval {
			return ruleAllowRemoval, true
		}
	case ActionUpgrade:
		// Upgrades are gated by policy: additive-only (AllowUpgrade=false) blocks
		// every upgrade so overlay never replaces a baseline package; the
		// additive-and-upgrade policy (AllowUpgrade=true) permits them, and the
		// install step then uses an upgrade-capable package-manager mode (dpkg -i,
		// which upgrades in place, or rpm -U in place of rpm -i).
		if !policy.AllowUpgrade {
			return ruleAllowUpgrade, true
		}
	case ActionDowngrade:
		if !policy.AllowDowngrade {
			return ruleAllowDowngrade, true
		}
	case ActionConflict:
		if conflictPolicy(policy) == config.OverlayConflictPolicyFail {
			return ruleConflictPolicyFail, true
		}
	case ActionUnsatisfiedDep:
		// Unconditional: this action is only emitted when no satisfying version of
		// the pinned dependency is in the post-install set (the baseline copy is
		// rejected and no newer copy is being installed), so the dependency can
		// never be met. No policy knob relaxes it — the install would simply fail
		// at configure time. The fix is to bring a satisfying version into the
		// resolved set, not to toggle a policy.
		return ruleUnsatisfiedDep, true
	}
	return "", false
}

// conflictPolicy returns the effective conflict policy, defaulting to "fail"
// when unset (matching config.OverlayPolicy.validate).
func conflictPolicy(policy config.OverlayPolicy) string {
	if strings.TrimSpace(policy.ConflictPolicy) == "" {
		return config.OverlayConflictPolicyFail
	}
	return policy.ConflictPolicy
}

// baselineVersionIndex builds Slice A: a name→package index of the installed
// baseline packages. Non-installed records (config-files remnants, etc.) are
// excluded so they never register as a current version.
func baselineVersionIndex(baseline []BaselinePackage) map[string]BaselinePackage {
	index := make(map[string]BaselinePackage, len(baseline))
	for _, p := range baseline {
		if !p.Installed || strings.TrimSpace(p.Name) == "" {
			continue
		}
		index[p.Name] = p
	}
	return index
}

// comparePkgVersions compares two version strings for a package family, reusing
// the resolver's family-specific comparator. Returns -1/0/1 for a<b / a==b / a>b.
func comparePkgVersions(family PackageManager, a, b string) (int, error) {
	if family == PackageManagerDNF {
		return rpmutils.CompareRPMVersions(a, b)
	}
	return debutils.CompareDebianVersions(a, b)
}

// isBootloaderPackage reports whether a package name identifies a bootloader
// component that overlay mode must never modify. A prefix matches the bare
// package or a sub-package separated by '-' or a digit (e.g. "grub2",
// "grub-efi-amd64", "systemd-boot-efi"), but NOT a different package that merely
// shares the prefix's letters (e.g. "systemd-bootchart", a boot profiler).
func isBootloaderPackage(name string) bool {
	return matchesPackagePrefix(name, bootloaderPackagePrefixes)
}

// isKernelImagePackage reports whether a package name identifies a bootable
// kernel-image package overlay mode must not upgrade in place (see
// kernelImagePackagePrefixes). Userspace kernel-adjacent packages that merely
// share the prefix (kernel-headers, linux-libc-dev, linux-tools-common) are NOT
// matched: linux-libc-dev/linux-tools-common fail the "linux-image" prefix, and
// the kernel-*-dev/-tools family is excluded explicitly via kernelSafeExactNames.
func isKernelImagePackage(name string) bool {
	if kernelSafeExactNames[strings.ToLower(strings.TrimSpace(name))] {
		return false
	}
	return matchesPackagePrefix(name, kernelImagePackagePrefixes)
}

// isKernelFamilyPackage reports whether a package name belongs to the kernel
// family that a kernel replacement removes (image + modules + headers + meta; see
// kernelFamilyPackagePrefixes). It is a superset of isKernelImagePackage. The same
// kernelSafeExactNames exclusion applies, so userspace dev/tools packages are never
// swept into the removal set.
func isKernelFamilyPackage(name string) bool {
	if kernelSafeExactNames[strings.ToLower(strings.TrimSpace(name))] {
		return false
	}
	return matchesPackagePrefix(name, kernelFamilyPackagePrefixes)
}

// matchesPackagePrefix reports whether name matches any of prefixes at a
// package-name boundary: the bare prefix, or a sub-package separated by '-' or a
// version digit (e.g. "grub2", "linux-image-6.8.0-40-generic"), but NOT a
// different package that merely shares the prefix's letters ("systemd-bootchart",
// "kernelshark").
func matchesPackagePrefix(name string, prefixes []string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return false
	}
	for _, prefix := range prefixes {
		if !strings.HasPrefix(lower, prefix) {
			continue
		}
		if len(lower) == len(prefix) {
			return true // exact package name
		}
		// A sub-package boundary is a '-' separator or a version digit ("grub2");
		// any other continuing letter means a different package ("systemd-bootchart").
		next := lower[len(prefix)]
		if next == '-' || (next >= '0' && next <= '9') {
			return true
		}
	}
	return false
}

// sortActions orders actions deterministically. It keys on type, package, and
// arch, then breaks remaining ties on the version/detail fields so two actions
// that share the primary keys (e.g. a two-slice conflict and a simulate-sourced
// conflict on the same package/arch) still order identically across runs.
func sortActions(actions []PlannedAction) {
	sort.Slice(actions, func(i, j int) bool {
		a, b := actions[i], actions[j]
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		if a.Package != b.Package {
			return a.Package < b.Package
		}
		if a.Arch != b.Arch {
			return a.Arch < b.Arch
		}
		if a.RequestedVersion != b.RequestedVersion {
			return a.RequestedVersion < b.RequestedVersion
		}
		if a.CurrentVersion != b.CurrentVersion {
			return a.CurrentVersion < b.CurrentVersion
		}
		if a.ConflictWith != b.ConflictWith {
			return a.ConflictWith < b.ConflictWith
		}
		return a.Detail < b.Detail
	})
}

// formatViolations renders policy violations into an actionable, deterministic
// multi-line diagnostic naming the offending package, current and requested
// versions, and the violated rule for each.
func formatViolations(violations []PolicyViolation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d policy violation(s) block installation:", len(violations))
	for _, v := range violations {
		fmt.Fprintf(&b, "\n  - %s", describeViolation(v))
	}
	return b.String()
}

// describeViolation renders one violation line.
func describeViolation(v PolicyViolation) string {
	a := v.Action
	current := a.CurrentVersion
	if current == "" {
		current = "(absent)"
	}
	requested := a.RequestedVersion
	if requested == "" {
		requested = "(removed)"
	}

	msg := fmt.Sprintf("%s %q: current=%s requested=%s [rule: %s]", a.Type, a.Package, current, requested, v.Rule)
	if a.Bootloader && v.Rule == ruleBootloaderImmutable {
		msg += " (bootloader packages must not be replaced in overlay mode)"
	}
	if a.Kernel && v.Rule == ruleKernelImmutable {
		msg += " (bootable kernel image is immutable in overlay mode: it cannot be upgraded, downgraded, or removed in place; to swap the kernel set overlayPolicy.replaceKernel, which installs the new kernel, removes the baseline kernel family, and regenerates the GRUB menu)"
	}
	if v.Rule == ruleReplaceKernelInvalid {
		msg += " (overlayPolicy.replaceKernel.package must resolve to a bootable kernel image so a kernel remains after the swap; name a kernel package such as linux-image-<flavour> and ensure a repository provides it)"
	}
	if v.Rule == ruleReplaceKernelAmbig {
		msg += " (overlayPolicy.replaceKernel.package must resolve to exactly one bootable kernel image; narrow the pattern so it matches a single kernel)"
	}
	if a.ConflictWith != "" && a.Type == ActionConflict {
		msg += fmt.Sprintf(" (conflicts with %q)", a.ConflictWith)
	}
	if a.Detail != "" {
		msg += fmt.Sprintf(" (%s)", a.Detail)
	}
	return msg
}
