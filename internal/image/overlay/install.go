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

// InstallResult records what the install step did, for logging and verification.
type InstallResult struct {
	// Installed are the package names confirmed present in the baseline package
	// database after installation (sorted).
	Installed []string
	// Artifacts are the artifact filenames that were installed (sorted).
	Artifacts []string
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
	// verifyInstalled queries the baseline package database in chrootPath and
	// returns the names of the requested packages that are NOT installed.
	verifyInstalled(chrootPath string, pkgs []ResolvedPackage) (missing []string, err error)
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
	if len(items) == 0 {
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

	log.Infof("Overlay install: installing %d package(s) from %d prepared artifact(s) in %s into %s",
		len(items), len(artifactNames), plan.DownloadDir, rootMount)

	// Establish the chroot bind-mount lifecycle (sysfs + artifact cache) and tear
	// it down in reverse on every return path, including a panic inside install.
	teardown, err := mountChrootForInstall(rootMount, plan.DownloadDir)
	if err != nil {
		return nil, err
	}
	defer teardown(&err)

	if err = backend.install(installRequest{
		chrootPath:        rootMount,
		artifactChrootDir: chrootArtifactDir,
		items:             items,
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

	installed := make([]string, 0, len(pkgs))
	for _, p := range pkgs {
		installed = append(installed, p.Name)
	}
	sort.Strings(installed)

	log.Infof("Overlay install complete: %d package(s) installed and verified in %s", len(installed), rootMount)
	return &InstallResult{Installed: installed, Artifacts: artifactNames}, nil
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

	// Deterministic order by artifact filename so the install command is stable.
	sort.Slice(items, func(i, j int) bool { return items[i].artifact < items[j].artifact })
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
// with dpkg. The closure was already resolved, so installing the additive set in
// one dpkg invocation satisfies inter-package dependencies among them; deps that
// were already present in the baseline remain untouched.
type debInstallerBackend struct{}

func (b *debInstallerBackend) family() PackageManager { return PackageManagerAPT }

func (b *debInstallerBackend) install(req installRequest) error {
	paths := make([]string, 0, len(req.items))
	for _, it := range req.items {
		// it.artifact is a URL-derived basename, so shell-quote each path before
		// joining it into the bash -c command line.
		paths = append(paths, shell.QuoteArg(filepath.Join(req.artifactChrootDir, it.artifact)))
	}

	// Non-interactive install of the local artifacts. dpkg -i takes the prepared
	// files directly (no network, no repository resolution), keeping the install
	// strictly to the approved, pre-downloaded set. "--" terminates option parsing
	// so a URL-derived artifact basename beginning with '-' is treated as a file
	// path, not a dpkg option (shell-quoting stops word-splitting, not option parsing).
	cmd := "dpkg -i -- " + strings.Join(paths, " ")
	envVars := []string{
		"DEBIAN_FRONTEND=noninteractive",
		"DEBCONF_NONINTERACTIVE_SEEN=true",
		"DEBCONF_NOWARNINGS=yes",
	}
	out, err := shell.ExecCmdWithStream(cmd, true, req.chrootPath, envVars)
	if err != nil {
		// Surface dpkg's captured output: on its own the wrapped error is only
		// "exit status 1", and dpkg's actual diagnostic (the failing package and
		// maintainer-script reason) is otherwise streamed only to debug logging.
		return fmt.Errorf("dpkg install of %d artifact(s) failed: %w%s", len(paths), err, formatCommandOutput(out))
	}
	return nil
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

	// rpm -i installs (adds) the local artifacts; it deliberately does not upgrade
	// or replace existing baseline packages, preserving the additive-only contract.
	// rpm -i fails outright on an already-installed package, but that never reaches
	// here: the preflight gate blocks any ActionUpgrade by default (AllowUpgrade is
	// off in v1), and ToInstall excludes packages already present in the baseline,
	// so only genuinely-new packages are installed.
	// "--" terminates option parsing so a URL-derived artifact basename beginning
	// with '-' is treated as a file path rather than an rpm option.
	cmd := "rpm -i -v -- " + strings.Join(paths, " ")
	out, err := shell.ExecCmdWithStream(cmd, true, req.chrootPath, nil)
	if err != nil {
		return fmt.Errorf("rpm install of %d artifact(s) failed: %w%s", len(paths), err, formatCommandOutput(out))
	}
	return nil
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
