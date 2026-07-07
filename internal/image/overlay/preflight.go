package overlay

import (
	"fmt"
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
	ruleAllowRemoval        = "allowRemoval=false"
	ruleAllowDowngrade      = "allowDowngrade=false"
	ruleAllowUpgrade        = "allowUpgrade=false"
	ruleConflictPolicyFail  = "conflictPolicy=fail"
	ruleBootloaderImmutable = "bootloader-immutable"
	ruleKernelImmutable     = "kernel-immutable"
	ruleUnsatisfiedDep      = "unsatisfiable-versioned-dependency"
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

// simulateOverlayInstall is a seam over the optional package-manager simulate
// step (apt-get install --simulate / dnf install --assumeno). Its output is a
// validation aid only — the two-slice model drives the policy decision — so the
// default is a no-op. The install-wiring story can plug a real simulator in, and
// tests override it to exercise the remove/conflict policy paths.
var simulateOverlayInstall = func(info *BaselineInfo, plan *ResolutionPlan) ([]PlannedAction, error) {
	return nil, nil
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

	// The simulate step is an optional validation aid; its failure must not mask
	// the authoritative two-slice decision, so a simulate error is logged and the
	// preflight continues on the two-slice model alone.
	simulated, err := simulateOverlayInstall(info, plan)
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
		SimulatedActions: simulated,
		ArtifactDeps:     artifactDeps,
		Obsoletions:      obsoletions,
		Policy:           effectivePolicy,
	})

	log.Infof("Overlay preflight: %d add, %d upgrade, %d downgrade, %d remove, %d conflict, %d unsatisfiable dep; %d policy violation(s)",
		report.Adds, report.Upgrades, report.Downgrades, report.Removes, report.Conflicts, report.UnsatisfiedDeps, len(report.Violations))

	if report.Blocked {
		return report, fmt.Errorf("overlay preflight failed: %s", formatViolations(report.Violations))
	}
	return report, nil
}

// EvaluatePreflight performs the pure two-slice classification and policy
// enforcement. It is deterministic and side-effect free.
func EvaluatePreflight(in PreflightInput) *PreflightReport {
	sliceA := baselineVersionIndex(in.Baseline)

	actions := classifyActions(in.Family, sliceA, in.Resolved)
	actions = append(actions, normalizeSimulatedActions(in.SimulatedActions, sliceA)...)
	actions = append(actions, classifyUnsatisfiedDeps(in.Family, sliceA, in.Resolved, in.ArtifactDeps)...)
	actions = append(actions, classifyObsoletions(in.Family, sliceA, in.Obsoletions)...)

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

	sortActions(actions)

	report := &PreflightReport{Actions: actions}
	for _, a := range actions {
		switch a.Type {
		case ActionAdd:
			report.Adds++
		case ActionUpgrade:
			report.Upgrades++
		case ActionDowngrade:
			report.Downgrades++
		case ActionRemove:
			report.Removes++
		case ActionConflict:
			report.Conflicts++
		case ActionUnsatisfiedDep:
			report.UnsatisfiedDeps++
		}
		if rule, blocked := violatedRule(a, in.Policy); blocked {
			report.Violations = append(report.Violations, PolicyViolation{Action: a, Rule: rule})
		}
	}

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
	postInstall := make(map[string]string, len(sliceA)+len(resolved))
	for name, bp := range sliceA {
		postInstall[name] = bp.Version
	}
	for _, rp := range resolved {
		if name := strings.TrimSpace(rp.Name); name != "" {
			postInstall[name] = rp.Version
		}
	}

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
			Type:           ActionRemove,
			Package:        target,
			CurrentVersion: base.Version,
			Arch:           base.Arch,
			ConflictWith:   o.Package,
			Detail:         fmt.Sprintf("obsoleted by %q, which rpm -U would erase from the baseline", o.Package),
		})
	}
	return actions
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
	// the initramfs, not the bootloader's menu entries for a changed kernel.
	if a.Kernel && a.Type != ActionAdd {
		return ruleKernelImmutable, true
	}

	switch a.Type {
	case ActionRemove:
		if !policy.AllowRemoval {
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
		msg += " (bootable kernel image must not be replaced in overlay mode; boot regeneration refreshes only the initramfs, not the bootloader menu entries)"
	}
	if a.ConflictWith != "" && a.Type == ActionConflict {
		msg += fmt.Sprintf(" (conflicts with %q)", a.ConflictWith)
	}
	if a.Detail != "" {
		msg += fmt.Sprintf(" (%s)", a.Detail)
	}
	return msg
}
