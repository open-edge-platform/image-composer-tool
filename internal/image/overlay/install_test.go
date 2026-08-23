package overlay

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

// fakeInstaller is an installerBackend stub: it records the request it received
// and returns canned install/verify outcomes, so the deterministic orchestration
// in InstallOverlayPackages can be exercised without root, mounts, or a chroot.
type fakeInstaller struct {
	fam          PackageManager
	installErr   error
	removeErr    error
	missing      []string
	verifyErr    error
	gotReq       installRequest
	gotRemoved   []string // names passed to removePackages, in call order
	installCalls int
	removeCalls  int
	verifyCalls  int
	auditCalls   int
	// auditBroken and auditErr are indexed by the audit call (1st = pre-removal,
	// 2nd = post-install), so a test can model the SET of packages with unmet
	// dependencies before vs. after (letting the caller diff for NEW breakage). A
	// missing index defaults to healthy (nil)/no-error.
	auditBroken [][]string
	auditErr    []error
	// removedBeforeInstall records whether removePackages ran before install, so a
	// test can assert the conflict-driven removal precedes the install.
	removedBeforeInstall bool
}

func (f *fakeInstaller) family() PackageManager { return f.fam }

func (f *fakeInstaller) install(req installRequest) error {
	f.installCalls++
	f.gotReq = req
	return f.installErr
}

func (f *fakeInstaller) removePackages(_ string, names []string) error {
	f.removeCalls++
	f.gotRemoved = append(f.gotRemoved, names...)
	if f.installCalls == 0 {
		f.removedBeforeInstall = true
	}
	return f.removeErr
}

func (f *fakeInstaller) verifyInstalled(_ string, _ []ResolvedPackage) ([]string, error) {
	f.verifyCalls++
	return f.missing, f.verifyErr
}

func (f *fakeInstaller) auditDependencies(_ string) ([]string, string, error) {
	idx := f.auditCalls
	f.auditCalls++
	var broken []string
	if idx < len(f.auditBroken) {
		broken = f.auditBroken[idx]
	}
	var err error
	if idx < len(f.auditErr) {
		err = f.auditErr[idx]
	}
	return broken, "", err
}

// installHarness wires the install-stage seams to in-memory fakes and records
// the mount/unmount lifecycle so tests can assert it is balanced.
type installHarness struct {
	mountedSysfs   []string
	umountedSysfs  []string
	bindMounts     [][2]string // [source, target]
	umountedBinds  []string
	sysfsMountErr  error
	bindMountErr   error
	umountBindErr  error
	umountSysfsErr error
}

// withStubbedInstall swaps the install-stage seams for the duration of fn,
// restoring them afterward. The backend (if non-nil) is returned by the selector.
func withStubbedInstall(t *testing.T, backend installerBackend, h *installHarness, fn func()) {
	t.Helper()
	origSelect := selectInstallerBackend
	origMountSysfs := mountSysfs
	origUmountSysfs := umountSysfs
	origBind := bindMountArtifacts
	origUmountBind := umountArtifacts
	defer func() {
		selectInstallerBackend = origSelect
		mountSysfs = origMountSysfs
		umountSysfs = origUmountSysfs
		bindMountArtifacts = origBind
		umountArtifacts = origUmountBind
	}()

	if backend != nil {
		selectInstallerBackend = func(PackageManager) (installerBackend, error) { return backend, nil }
	}
	mountSysfs = func(p string) error {
		if h.sysfsMountErr != nil {
			return h.sysfsMountErr
		}
		h.mountedSysfs = append(h.mountedSysfs, p)
		return nil
	}
	umountSysfs = func(p string) error {
		h.umountedSysfs = append(h.umountedSysfs, p)
		return h.umountSysfsErr
	}
	bindMountArtifacts = func(src, target, _ string) error {
		if h.bindMountErr != nil {
			return h.bindMountErr
		}
		h.bindMounts = append(h.bindMounts, [2]string{src, target})
		return nil
	}
	umountArtifacts = func(target string) error {
		h.umountedBinds = append(h.umountedBinds, target)
		return h.umountBindErr
	}
	fn()
}

// writeArtifacts creates empty artifact files in dir so planInstalls' existence
// check passes, and returns dir.
func writeArtifacts(t *testing.T, dir string, names ...string) string {
	t.Helper()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatalf("write artifact %s: %v", n, err)
		}
	}
	return dir
}

func aptInfo() *BaselineInfo {
	return &BaselineInfo{OS: "ubuntu", Arch: "amd64", PackageManager: PackageManagerAPT}
}

func passedReport() *PreflightReport { return &PreflightReport{Blocked: false} }

func TestInstallOverlayPackages_HappyPath(t *testing.T) {
	dir := writeArtifacts(t, t.TempDir(), "curl_8.deb", "libfoo_1.deb")
	plan := &ResolutionPlan{
		Requested:   []string{"curl"},
		DownloadDir: dir,
		ToInstall: []ResolvedPackage{
			{Name: "curl", Version: "8", Arch: "amd64", URL: "https://r/curl_8.deb"},
			{Name: "libfoo", Version: "1", Arch: "amd64", URL: "https://r/libfoo_1.deb"},
		},
	}
	backend := &fakeInstaller{fam: PackageManagerAPT}
	h := &installHarness{}

	var result *InstallResult
	var err error
	withStubbedInstall(t, backend, h, func() {
		result, err = InstallOverlayPackages(aptInfo(), "/mnt/root", plan, passedReport())
	})
	if err != nil {
		t.Fatalf("InstallOverlayPackages: %v", err)
	}

	if backend.installCalls != 1 || backend.verifyCalls != 1 {
		t.Errorf("install/verify calls = %d/%d, want 1/1", backend.installCalls, backend.verifyCalls)
	}
	// The backend installs from the in-chroot artifact dir, in deterministic order.
	if backend.gotReq.artifactChrootDir != chrootArtifactDir {
		t.Errorf("artifactChrootDir = %q, want %q", backend.gotReq.artifactChrootDir, chrootArtifactDir)
	}
	gotArtifacts := []string{backend.gotReq.items[0].artifact, backend.gotReq.items[1].artifact}
	if !reflect.DeepEqual(gotArtifacts, []string{"curl_8.deb", "libfoo_1.deb"}) {
		t.Errorf("install items = %v, want sorted [curl_8.deb libfoo_1.deb]", gotArtifacts)
	}
	if !reflect.DeepEqual(result.Installed, []string{"curl", "libfoo"}) {
		t.Errorf("installed = %v, want [curl libfoo]", result.Installed)
	}
	if result.Skipped {
		t.Error("result should not be Skipped when packages were installed")
	}

	// The chroot bind-mount lifecycle is balanced: sysfs + artifact mounted, then
	// both unmounted.
	if len(h.mountedSysfs) != 1 || len(h.umountedSysfs) != 1 {
		t.Errorf("sysfs mount/umount = %d/%d, want 1/1", len(h.mountedSysfs), len(h.umountedSysfs))
	}
	if len(h.bindMounts) != 1 || len(h.umountedBinds) != 1 {
		t.Errorf("artifact bind mount/umount = %d/%d, want 1/1", len(h.bindMounts), len(h.umountedBinds))
	}
	wantTarget := filepath.Join("/mnt/root", "run", "overlay-pkgs")
	if h.bindMounts[0][0] != dir || h.bindMounts[0][1] != wantTarget {
		t.Errorf("bind mount = %v, want [%s %s]", h.bindMounts[0], dir, wantTarget)
	}
	if h.umountedBinds[0] != wantTarget {
		t.Errorf("unmounted bind = %q, want %q", h.umountedBinds[0], wantTarget)
	}
}

// TestInstallOverlayPackages_RemovesConflictingBeforeInstall confirms the
// conflict-driven removal path (allowPackageRemoval): the packages in the preflight
// report's ToRemove are removed via the backend BEFORE the install runs, so a
// baseline package that the replacement conflicts with (e.g. initramfs-tools vs
// dracut) is gone before its replacement is unpacked.
func TestInstallOverlayPackages_RemovesConflictingBeforeInstall(t *testing.T) {
	dir := writeArtifacts(t, t.TempDir(), "dracut_1.deb")
	plan := &ResolutionPlan{
		Requested:   []string{"dracut"},
		DownloadDir: dir,
		ToInstall: []ResolvedPackage{
			{Name: "dracut", Version: "1", Arch: "amd64", URL: "https://r/dracut_1.deb"},
		},
	}
	// Preflight approved the build and asked for initramfs-tools to be removed first.
	report := &PreflightReport{Blocked: false, ToRemove: []string{"initramfs-tools"}}

	backend := &fakeInstaller{fam: PackageManagerAPT}
	h := &installHarness{}

	var err error
	withStubbedInstall(t, backend, h, func() {
		_, err = InstallOverlayPackages(aptInfo(), "/mnt/root", plan, report)
	})
	if err != nil {
		t.Fatalf("InstallOverlayPackages: %v", err)
	}

	if backend.removeCalls != 1 {
		t.Errorf("removePackages calls = %d, want 1", backend.removeCalls)
	}
	if !reflect.DeepEqual(backend.gotRemoved, []string{"initramfs-tools"}) {
		t.Errorf("removed = %v, want [initramfs-tools]", backend.gotRemoved)
	}
	if !backend.removedBeforeInstall {
		t.Error("conflict-driven removal must run BEFORE the install, not after")
	}
	if backend.installCalls != 1 {
		t.Errorf("install calls = %d, want 1", backend.installCalls)
	}
}

// TestInstallOverlayPackages_CascadeRemovesOrphanedReverseDep confirms the
// post-install audit CASCADES: a reverse-dependency the force-removal orphaned (an
// unmet dependency that is new after the install) is itself removed, and the build
// succeeds. The removal is folded into the report so stats/SBOM see the final
// inventory. This is the dracut → initramfs-tools → cloud-initramfs-growroot case.
func TestInstallOverlayPackages_CascadeRemovesOrphanedReverseDep(t *testing.T) {
	dir := writeArtifacts(t, t.TempDir(), "dracut_1.deb")
	plan := &ResolutionPlan{
		Requested:   []string{"dracut"},
		DownloadDir: dir,
		ToInstall:   []ResolvedPackage{{Name: "dracut", Version: "1", Arch: "amd64", URL: "https://r/dracut_1.deb"}},
	}
	report := &PreflightReport{ToRemove: []string{"initramfs-tools"}, ApprovedRemovals: []string{"initramfs-tools"}, CollateralRemovalAuthorized: true}
	// Healthy before the removal; "cloud-initramfs-growroot" orphaned after the
	// install; clean once it is cascade-removed.
	backend := &fakeInstaller{fam: PackageManagerAPT, auditBroken: [][]string{nil, {"cloud-initramfs-growroot"}, nil}}
	h := &installHarness{}

	var result *InstallResult
	var err error
	withStubbedInstall(t, backend, h, func() {
		result, err = InstallOverlayPackages(aptInfo(), "/mnt/root", plan, report)
	})
	if err != nil {
		t.Fatalf("cascade removal of an orphaned reverse-dependency should succeed, got %v", err)
	}
	// pre-removal + post-install + post-cascade = 3 audits.
	if backend.auditCalls != 3 {
		t.Errorf("audit must run pre-removal, post-install, and post-cascade, got %d call(s)", backend.auditCalls)
	}
	// initramfs-tools removed first, then the orphaned reverse-dependency.
	if !reflect.DeepEqual(backend.gotRemoved, []string{"initramfs-tools", "cloud-initramfs-growroot"}) {
		t.Errorf("removed = %v, want [initramfs-tools cloud-initramfs-growroot]", backend.gotRemoved)
	}
	if !reflect.DeepEqual(result.CascadeRemoved, []string{"cloud-initramfs-growroot"}) {
		t.Errorf("result.CascadeRemoved = %v, want [cloud-initramfs-growroot]", result.CascadeRemoved)
	}
	// Folded into the report so stats/SBOM reflect the final inventory.
	if !contains(report.ApprovedRemovals, "cloud-initramfs-growroot") {
		t.Errorf("cascade removal must be folded into report.ApprovedRemovals, got %v", report.ApprovedRemovals)
	}
	if report.Removes != 1 {
		t.Errorf("report.Removes = %d, want 1 (the cascade removal)", report.Removes)
	}
}

// TestInstallOverlayPackages_CascadeTransitiveChain confirms the cascade is
// transitive: removing an orphaned package that in turn orphans ANOTHER package
// removes both, over successive passes, until the dependency tree is whole.
func TestInstallOverlayPackages_CascadeTransitiveChain(t *testing.T) {
	dir := writeArtifacts(t, t.TempDir(), "dracut_1.deb")
	plan := &ResolutionPlan{
		DownloadDir: dir,
		ToInstall:   []ResolvedPackage{{Name: "dracut", Version: "1", URL: "https://r/dracut_1.deb"}},
	}
	report := &PreflightReport{ToRemove: []string{"initramfs-tools"}, ApprovedRemovals: []string{"initramfs-tools"}, CollateralRemovalAuthorized: true}
	// pre: healthy; post-install: "a" broken; after removing "a": "b" broken; then clean.
	backend := &fakeInstaller{fam: PackageManagerAPT, auditBroken: [][]string{nil, {"a"}, {"b"}, nil}}
	h := &installHarness{}

	var result *InstallResult
	var err error
	withStubbedInstall(t, backend, h, func() {
		result, err = InstallOverlayPackages(aptInfo(), "/mnt/root", plan, report)
	})
	if err != nil {
		t.Fatalf("transitive cascade should succeed, got %v", err)
	}
	if !reflect.DeepEqual(backend.gotRemoved, []string{"initramfs-tools", "a", "b"}) {
		t.Errorf("removed = %v, want [initramfs-tools a b]", backend.gotRemoved)
	}
	if !reflect.DeepEqual(result.CascadeRemoved, []string{"a", "b"}) {
		t.Errorf("result.CascadeRemoved = %v, want [a b]", result.CascadeRemoved)
	}
	if report.Removes != 2 {
		t.Errorf("report.Removes = %d, want 2", report.Removes)
	}
}

// TestInstallOverlayPackages_CascadeRemovesExactMultiarchInstance confirms that on a
// multiarch (deb) baseline the cascade purges the EXACT broken instance by its
// arch-qualified identity (e.g. "libc6:i386"), not the ambiguous bare name — while the
// bare name is what is reported (folded into ApprovedRemovals / CascadeRemoved).
func TestInstallOverlayPackages_CascadeRemovesExactMultiarchInstance(t *testing.T) {
	dir := writeArtifacts(t, t.TempDir(), "dracut_1.deb")
	plan := &ResolutionPlan{
		DownloadDir: dir,
		ToInstall:   []ResolvedPackage{{Name: "dracut", Version: "1", URL: "https://r/dracut_1.deb"}},
	}
	report := &PreflightReport{ToRemove: []string{"initramfs-tools"}, ApprovedRemovals: []string{"initramfs-tools"}, CollateralRemovalAuthorized: true}
	// apt-get check reports the i386 instance broken with its arch qualifier.
	backend := &fakeInstaller{fam: PackageManagerAPT, auditBroken: [][]string{nil, {"libfoo:i386 | Depends: libbar"}, nil}}
	h := &installHarness{}

	var result *InstallResult
	var err error
	withStubbedInstall(t, backend, h, func() {
		result, err = InstallOverlayPackages(aptInfo(), "/mnt/root", plan, report)
	})
	if err != nil {
		t.Fatalf("cascade of a multiarch instance should succeed, got %v", err)
	}
	// The removal operand keeps the arch qualifier so dpkg targets the exact instance.
	if !reflect.DeepEqual(backend.gotRemoved, []string{"initramfs-tools", "libfoo:i386"}) {
		t.Errorf("removed = %v, want [initramfs-tools libfoo:i386]", backend.gotRemoved)
	}
	// Reporting uses the bare name (matches BaselinePackage.Name for stats/SBOM).
	if !reflect.DeepEqual(result.CascadeRemoved, []string{"libfoo"}) {
		t.Errorf("result.CascadeRemoved = %v, want [libfoo]", result.CascadeRemoved)
	}
	if !contains(report.ApprovedRemovals, "libfoo") {
		t.Errorf("cascade removal must fold the bare name into ApprovedRemovals, got %v", report.ApprovedRemovals)
	}
}

// TestInstallOverlayPackages_PreExistingBrokenBaselineNotBlamed confirms a baseline
// that was ALREADY broken before the overlay is neither failed nor cascade-removed
// when the SAME package remains broken afterward: the before/after diff finds no NEW
// breakage, so pre-existing breakage is not attributed to (or "fixed" by) the overlay.
func TestInstallOverlayPackages_PreExistingBrokenBaselineNotBlamed(t *testing.T) {
	dir := writeArtifacts(t, t.TempDir(), "dracut_1.deb")
	plan := &ResolutionPlan{
		DownloadDir: dir,
		ToInstall:   []ResolvedPackage{{Name: "dracut", Version: "1", URL: "https://r/dracut_1.deb"}},
	}
	report := &PreflightReport{ToRemove: []string{"initramfs-tools"}, ApprovedRemovals: []string{"initramfs-tools"}}
	// "oldbroken" is broken both before and after: no NEW breakage, so it must pass
	// WITHOUT cascade-removing the pre-existing broken package.
	backend := &fakeInstaller{fam: PackageManagerAPT, auditBroken: [][]string{{"oldbroken"}, {"oldbroken"}}}
	h := &installHarness{}

	var result *InstallResult
	var err error
	withStubbedInstall(t, backend, h, func() {
		result, err = InstallOverlayPackages(aptInfo(), "/mnt/root", plan, report)
	})
	if err != nil {
		t.Fatalf("a pre-existing broken baseline must not be blamed on the overlay, got %v", err)
	}
	if backend.auditCalls != 2 {
		t.Errorf("audit must run pre-removal and post-install (no cascade needed), got %d call(s)", backend.auditCalls)
	}
	if len(result.CascadeRemoved) != 0 {
		t.Errorf("a pre-existing broken package must NOT be cascade-removed, got %v", result.CascadeRemoved)
	}
	if contains(backend.gotRemoved, "oldbroken") {
		t.Errorf("pre-existing broken 'oldbroken' must not be removed, got %v", backend.gotRemoved)
	}
}

// TestInstallOverlayPackages_CascadeRemovesOnlyNewBreakage confirms the cascade
// removes a NEW breakage the removal introduced while leaving a DIFFERENT, pre-existing
// broken package untouched.
func TestInstallOverlayPackages_CascadeRemovesOnlyNewBreakage(t *testing.T) {
	dir := writeArtifacts(t, t.TempDir(), "dracut_1.deb")
	plan := &ResolutionPlan{
		DownloadDir: dir,
		ToInstall:   []ResolvedPackage{{Name: "dracut", Version: "1", URL: "https://r/dracut_1.deb"}},
	}
	report := &PreflightReport{ToRemove: []string{"initramfs-tools"}, ApprovedRemovals: []string{"initramfs-tools"}, CollateralRemovalAuthorized: true}
	// "oldbroken" pre-exists; "newbroken" is introduced by the removal; after removing
	// "newbroken" only "oldbroken" (pre-existing) remains → no further cascade.
	backend := &fakeInstaller{fam: PackageManagerAPT, auditBroken: [][]string{{"oldbroken"}, {"oldbroken", "newbroken"}, {"oldbroken"}}}
	h := &installHarness{}

	var result *InstallResult
	var err error
	withStubbedInstall(t, backend, h, func() {
		result, err = InstallOverlayPackages(aptInfo(), "/mnt/root", plan, report)
	})
	if err != nil {
		t.Fatalf("cascade of the new breakage should succeed, got %v", err)
	}
	if !reflect.DeepEqual(result.CascadeRemoved, []string{"newbroken"}) {
		t.Errorf("result.CascadeRemoved = %v, want [newbroken]", result.CascadeRemoved)
	}
	if contains(backend.gotRemoved, "oldbroken") {
		t.Errorf("pre-existing breakage 'oldbroken' must not be cascade-removed, got %v", backend.gotRemoved)
	}
}

// TestInstallOverlayPackages_CascadeFailsClosedOnBootloader confirms the cascade will
// NOT remove a bootloader/kernel-image package: if an approved removal would orphan
// one, the build fails closed with an actionable error rather than removing it.
func TestInstallOverlayPackages_CascadeFailsClosedOnBootloader(t *testing.T) {
	dir := writeArtifacts(t, t.TempDir(), "dracut_1.deb")
	plan := &ResolutionPlan{
		DownloadDir: dir,
		ToInstall:   []ResolvedPackage{{Name: "dracut", Version: "1", URL: "https://r/dracut_1.deb"}},
	}
	report := &PreflightReport{ToRemove: []string{"initramfs-tools"}, ApprovedRemovals: []string{"initramfs-tools"}}
	// A bootloader package is orphaned: it must never be cascade-removed.
	backend := &fakeInstaller{fam: PackageManagerAPT, auditBroken: [][]string{nil, {"grub-efi-amd64"}}}
	h := &installHarness{}

	var err error
	withStubbedInstall(t, backend, h, func() {
		_, err = InstallOverlayPackages(aptInfo(), "/mnt/root", plan, report)
	})
	if err == nil || !strings.Contains(err.Error(), "grub-efi-amd64") {
		t.Fatalf("cascade must fail closed on an orphaned bootloader package, got %v", err)
	}
	// Only the initial approved removal ran; the bootloader was NOT removed.
	if !reflect.DeepEqual(backend.gotRemoved, []string{"initramfs-tools"}) {
		t.Errorf("bootloader must not be cascade-removed; gotRemoved = %v", backend.gotRemoved)
	}
}

// TestInstallOverlayPackages_CascadeFailsClosedOnToInstall confirms the cascade will
// NOT remove a package the overlay is installing, even if the audit reports it broken.
func TestInstallOverlayPackages_CascadeFailsClosedOnToInstall(t *testing.T) {
	dir := writeArtifacts(t, t.TempDir(), "dracut_1.deb")
	plan := &ResolutionPlan{
		DownloadDir: dir,
		ToInstall:   []ResolvedPackage{{Name: "dracut", Version: "1", URL: "https://r/dracut_1.deb"}},
	}
	report := &PreflightReport{ToRemove: []string{"initramfs-tools"}, ApprovedRemovals: []string{"initramfs-tools"}}
	backend := &fakeInstaller{fam: PackageManagerAPT, auditBroken: [][]string{nil, {"dracut"}}}
	h := &installHarness{}

	var err error
	withStubbedInstall(t, backend, h, func() {
		_, err = InstallOverlayPackages(aptInfo(), "/mnt/root", plan, report)
	})
	if err == nil || !strings.Contains(err.Error(), "dracut") {
		t.Fatalf("cascade must fail closed rather than remove a to-install package, got %v", err)
	}
	if contains(backend.gotRemoved, "dracut") {
		t.Errorf("a to-install package must never be cascade-removed, got %v", backend.gotRemoved)
	}
}

// TestInstallOverlayPackages_CascadeFailsClosedOnUnauthorizedCollateral confirms a
// kernel replacement (which self-authorizes only its kernel-family removals, so
// CollateralRemovalAuthorized is false) does NOT silently cascade-remove an unrelated
// non-kernel package it orphaned: the build fails closed, telling the operator to opt
// into allowPackageRemoval or add the missing dependency.
func TestInstallOverlayPackages_CascadeFailsClosedOnUnauthorizedCollateral(t *testing.T) {
	dir := writeArtifacts(t, t.TempDir(), "linux-image-6.11.0-generic_1.deb")
	plan := &ResolutionPlan{
		DownloadDir: dir,
		ToInstall:   []ResolvedPackage{{Name: "linux-image-6.11.0-generic", Version: "1", URL: "https://r/linux-image-6.11.0-generic_1.deb"}},
	}
	// A kernel-family removal self-authorized by replaceKernel, WITHOUT allowPackageRemoval.
	report := &PreflightReport{
		ToRemove:                    []string{"linux-modules-6.8.0-40-generic"},
		ApprovedRemovals:            []string{"linux-modules-6.8.0-40-generic"},
		CollateralRemovalAuthorized: false,
	}
	// A DKMS-style package bound to the old modules is orphaned by the swap.
	backend := &fakeInstaller{fam: PackageManagerAPT, auditBroken: [][]string{nil, {"some-dkms-module"}}}
	h := &installHarness{}

	var err error
	withStubbedInstall(t, backend, h, func() {
		_, err = InstallOverlayPackages(aptInfo(), "/mnt/root", plan, report)
	})
	if err == nil || !strings.Contains(err.Error(), "some-dkms-module") || !strings.Contains(err.Error(), "allowPackageRemoval") {
		t.Fatalf("an unauthorized collateral removal must fail closed pointing at allowPackageRemoval, got %v", err)
	}
	// Only the self-authorized kernel-family removal ran; the collateral was NOT removed.
	if !reflect.DeepEqual(backend.gotRemoved, []string{"linux-modules-6.8.0-40-generic"}) {
		t.Errorf("collateral package must not be cascade-removed; gotRemoved = %v", backend.gotRemoved)
	}
}

// TestBrokenDescriptorPackage confirms the family-specific extraction of a bare
// package name from an audit failure descriptor.
func TestBrokenDescriptorPackage(t *testing.T) {
	cases := []struct {
		name       string
		family     PackageManager
		descriptor string
		want       string
	}{
		{"deb plain", PackageManagerAPT, "cloud-initramfs-growroot | Depends: initramfs-tools", "cloud-initramfs-growroot"},
		{"deb multiarch", PackageManagerAPT, "libc6:i386 | Depends: libgcc-s1", "libc6"},
		{"deb dotted name", PackageManagerAPT, "libssl1.1 | Depends: x", "libssl1.1"},
		{"rpm name.arch", PackageManagerDNF, "kernel-core.x86_64 | requires foo", "kernel-core"},
		{"rpm noarch", PackageManagerDNF, "python3-foo.noarch | ", "python3-foo"},
		{"no requirement", PackageManagerAPT, "barepkg", "barepkg"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := brokenDescriptorPackage(tc.family, tc.descriptor); got != tc.want {
				t.Errorf("brokenDescriptorPackage(%q, %q) = %q, want %q", tc.family, tc.descriptor, got, tc.want)
			}
		})
	}
}

// TestInstallOverlayPackages_UnauditableBaselineFailsClosed confirms that when the
// pre-removal audit tool cannot run (no reliable before snapshot), the build FAILS
// CLOSED — the destructive force-removal is refused before it runs rather than
// executing with the only whole-database integrity check disabled.
func TestInstallOverlayPackages_UnauditableBaselineFailsClosed(t *testing.T) {
	dir := writeArtifacts(t, t.TempDir(), "dracut_1.deb")
	plan := &ResolutionPlan{
		DownloadDir: dir,
		ToInstall:   []ResolvedPackage{{Name: "dracut", Version: "1", URL: "https://r/dracut_1.deb"}},
	}
	report := &PreflightReport{ToRemove: []string{"initramfs-tools"}, ApprovedRemovals: []string{"initramfs-tools"}}
	// Pre-removal audit errors (tool absent from the baseline).
	backend := &fakeInstaller{
		fam:      PackageManagerAPT,
		auditErr: []error{errors.New("apt-get: not found")},
	}
	h := &installHarness{}

	var err error
	withStubbedInstall(t, backend, h, func() {
		_, err = InstallOverlayPackages(aptInfo(), "/mnt/root", plan, report)
	})
	if err == nil || !strings.Contains(err.Error(), "cannot verify dependency integrity") {
		t.Fatalf("an unauditable baseline must fail closed before removing, got %v", err)
	}
	// The destructive removal and the install must NOT have run.
	if backend.removeCalls != 0 {
		t.Errorf("removal must not run when integrity cannot be verified, got %d call(s)", backend.removeCalls)
	}
	if backend.installCalls != 0 {
		t.Errorf("install must not run after a fail-closed abort, got %d call(s)", backend.installCalls)
	}
	// Only the pre-removal audit was attempted.
	if backend.auditCalls != 1 {
		t.Errorf("only the pre-removal audit should be attempted, got %d call(s)", backend.auditCalls)
	}
}

// TestInstallOverlayPackages_NoAuditWithoutRemoval confirms the dependency audit
// runs ONLY when a force-removal occurred: a pure additive build (no ToRemove)
// never invokes it.
func TestInstallOverlayPackages_NoAuditWithoutRemoval(t *testing.T) {
	dir := writeArtifacts(t, t.TempDir(), "curl_8.deb")
	plan := &ResolutionPlan{
		DownloadDir: dir,
		ToInstall:   []ResolvedPackage{{Name: "curl", Version: "8", URL: "https://r/curl_8.deb"}},
	}
	backend := &fakeInstaller{fam: PackageManagerAPT, auditBroken: [][]string{{"x"}}}
	h := &installHarness{}

	var err error
	withStubbedInstall(t, backend, h, func() {
		_, err = InstallOverlayPackages(aptInfo(), "/mnt/root", plan, passedReport())
	})
	if err != nil {
		t.Fatalf("InstallOverlayPackages: %v", err)
	}
	if backend.auditCalls != 0 {
		t.Errorf("no removal means no dependency audit, got %d call(s)", backend.auditCalls)
	}
}

// TestInstallOverlayPackages_NoRemovalWhenToRemoveEmpty confirms the removal step
// is skipped entirely when preflight approved no removals (the default path), so a
// build that opted into nothing never touches removePackages.
func TestInstallOverlayPackages_NoRemovalWhenToRemoveEmpty(t *testing.T) {
	dir := writeArtifacts(t, t.TempDir(), "curl_8.deb")
	plan := &ResolutionPlan{
		Requested:   []string{"curl"},
		DownloadDir: dir,
		ToInstall:   []ResolvedPackage{{Name: "curl", Version: "8", Arch: "amd64", URL: "https://r/curl_8.deb"}},
	}
	backend := &fakeInstaller{fam: PackageManagerAPT}
	h := &installHarness{}

	var err error
	withStubbedInstall(t, backend, h, func() {
		_, err = InstallOverlayPackages(aptInfo(), "/mnt/root", plan, passedReport())
	})
	if err != nil {
		t.Fatalf("InstallOverlayPackages: %v", err)
	}
	if backend.removeCalls != 0 {
		t.Errorf("removePackages must not run when ToRemove is empty, got %d call(s)", backend.removeCalls)
	}
}

// TestInstallOverlayPackages_RemovalFailureAbortsBeforeInstall confirms a failed
// conflict-driven removal aborts the build before any install runs, so a
// half-removed baseline is never handed to the installer.
func TestInstallOverlayPackages_RemovalFailureAbortsBeforeInstall(t *testing.T) {
	dir := writeArtifacts(t, t.TempDir(), "dracut_1.deb")
	plan := &ResolutionPlan{
		Requested:   []string{"dracut"},
		DownloadDir: dir,
		ToInstall:   []ResolvedPackage{{Name: "dracut", Version: "1", Arch: "amd64", URL: "https://r/dracut_1.deb"}},
	}
	report := &PreflightReport{Blocked: false, ToRemove: []string{"initramfs-tools"}}
	backend := &fakeInstaller{fam: PackageManagerAPT, removeErr: errors.New("dpkg --purge failed")}
	h := &installHarness{}

	var err error
	withStubbedInstall(t, backend, h, func() {
		_, err = InstallOverlayPackages(aptInfo(), "/mnt/root", plan, report)
	})
	if err == nil {
		t.Fatal("expected the removal failure to abort the install")
	}
	if backend.installCalls != 0 {
		t.Errorf("install must not run after a removal failure, got %d call(s)", backend.installCalls)
	}
}

// TestInstallOverlayPackages_UpgradeFlagPropagates confirms the orchestration
// derives installRequest.upgrade from the preflight report's upgrade count, so
// the backend can pick an upgrade-capable package-manager mode (rpm -U).
func TestInstallOverlayPackages_UpgradeFlagPropagates(t *testing.T) {
	dir := writeArtifacts(t, t.TempDir(), "curl_new.deb")
	plan := &ResolutionPlan{
		Requested:   []string{"curl"},
		DownloadDir: dir,
		ToInstall:   []ResolvedPackage{{Name: "curl", Version: "8.10", Arch: "amd64", URL: "https://r/curl_new.deb"}},
	}

	for _, tc := range []struct {
		name   string
		report *PreflightReport
		want   bool
	}{
		{"plan with an upgrade sets the flag", &PreflightReport{Upgrades: 1}, true},
		{"pure-add plan leaves the flag off", &PreflightReport{}, false},
		// An rpm Obsoletes-driven removal is NOT an upgrade (Upgrades==0) and is
		// absent from ToRemove (rpm -U erases it implicitly), so it shows up as
		// ApprovedRemovals having more entries than ToRemove. That must still select
		// -U, or the obsoletion silently would not happen under `rpm -i`.
		{"obsoletion-only plan sets the flag", &PreflightReport{ApprovedRemovals: []string{"oldpkg"}}, true},
		// An explicit conflict-driven removal (in both lists) is not an obsoletion, so
		// it alone does not force -U — the removal is performed separately via rpm -e.
		{"explicit-removal-only plan leaves the flag off", &PreflightReport{ToRemove: []string{"initramfs-tools"}, ApprovedRemovals: []string{"initramfs-tools"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &fakeInstaller{fam: PackageManagerDNF}
			withStubbedInstall(t, backend, &installHarness{}, func() {
				if _, err := InstallOverlayPackages(aptInfo(), "/mnt/root", plan, tc.report); err != nil {
					t.Fatalf("InstallOverlayPackages: %v", err)
				}
			})
			if backend.gotReq.upgrade != tc.want {
				t.Errorf("installRequest.upgrade = %v, want %v", backend.gotReq.upgrade, tc.want)
			}
		})
	}
}

func TestInstallOverlayPackages_NothingToInstall(t *testing.T) {
	plan := &ResolutionPlan{Requested: []string{"bash"}, AlreadyPresent: []string{"bash"}}
	backend := &fakeInstaller{fam: PackageManagerAPT}
	h := &installHarness{}

	var result *InstallResult
	var err error
	withStubbedInstall(t, backend, h, func() {
		result, err = InstallOverlayPackages(aptInfo(), "/mnt/root", plan, passedReport())
	})
	if err != nil {
		t.Fatalf("InstallOverlayPackages: %v", err)
	}
	if !result.Skipped {
		t.Error("expected Skipped result for an empty install set")
	}
	// No chroot must be entered when there is nothing to install.
	if backend.installCalls != 0 || len(h.mountedSysfs) != 0 || len(h.bindMounts) != 0 {
		t.Errorf("no-op must not mount or install: installCalls=%d sysfs=%d binds=%d",
			backend.installCalls, len(h.mountedSysfs), len(h.bindMounts))
	}
}

// TestInstallOverlayPackages_RemovalOnlySwap confirms a removal-only plan (empty
// ToInstall but non-empty ToRemove — e.g. a kernel swap where the replacement kernel
// is already present, so only the baseline kernel family must be removed) still
// performs the removals and is NOT reported as Skipped; only the package-install call
// is skipped.
func TestInstallOverlayPackages_RemovalOnlySwap(t *testing.T) {
	plan := &ResolutionPlan{
		Requested:   []string{"linux-image-6.18-intel"},
		DownloadDir: t.TempDir(), // required to mount the chroot; no artifacts needed
		ToInstall:   nil,         // replacement already present -> nothing to install
	}
	report := &PreflightReport{Blocked: false, ToRemove: []string{"linux-image-6.8.0-40-generic", "linux-image-generic"}}

	backend := &fakeInstaller{fam: PackageManagerAPT}
	h := &installHarness{}

	var result *InstallResult
	var err error
	withStubbedInstall(t, backend, h, func() {
		result, err = InstallOverlayPackages(aptInfo(), "/mnt/root", plan, report)
	})
	if err != nil {
		t.Fatalf("InstallOverlayPackages: %v", err)
	}
	if result.Skipped {
		t.Error("a removal-only swap must not be reported as Skipped")
	}
	if backend.installCalls != 0 {
		t.Errorf("install must not run when there are no items, got %d call(s)", backend.installCalls)
	}
	if backend.removeCalls != 1 || !reflect.DeepEqual(backend.gotRemoved, []string{"linux-image-6.8.0-40-generic", "linux-image-generic"}) {
		t.Errorf("expected the baseline kernel family to be removed, removeCalls=%d removed=%v", backend.removeCalls, backend.gotRemoved)
	}
	if len(h.mountedSysfs) == 0 {
		t.Error("removal-only swap must still mount the chroot to run the removal")
	}
}

func TestInstallOverlayPackages_BlockedPreflightRefuses(t *testing.T) {
	dir := writeArtifacts(t, t.TempDir(), "curl_8.deb")
	plan := &ResolutionPlan{
		DownloadDir: dir,
		ToInstall:   []ResolvedPackage{{Name: "curl", URL: "https://r/curl_8.deb"}},
	}
	backend := &fakeInstaller{fam: PackageManagerAPT}
	h := &installHarness{}
	blocked := &PreflightReport{Blocked: true, Violations: []PolicyViolation{{Rule: ruleAllowRemoval}}}

	var err error
	withStubbedInstall(t, backend, h, func() {
		_, err = InstallOverlayPackages(aptInfo(), "/mnt/root", plan, blocked)
	})
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("expected refusal on blocked preflight, got %v", err)
	}
	if backend.installCalls != 0 || len(h.mountedSysfs) != 0 {
		t.Error("a blocked preflight must result in no install attempt and no mounts")
	}
}

func TestInstallOverlayPackages_NilReportRefuses(t *testing.T) {
	dir := writeArtifacts(t, t.TempDir(), "curl_8.deb")
	plan := &ResolutionPlan{DownloadDir: dir, ToInstall: []ResolvedPackage{{Name: "curl", URL: "https://r/curl_8.deb"}}}
	backend := &fakeInstaller{fam: PackageManagerAPT}
	h := &installHarness{}

	var err error
	withStubbedInstall(t, backend, h, func() {
		_, err = InstallOverlayPackages(aptInfo(), "/mnt/root", plan, nil)
	})
	if err == nil || !strings.Contains(err.Error(), "without a passed preflight") {
		t.Fatalf("expected refusal on nil preflight report, got %v", err)
	}
	if backend.installCalls != 0 {
		t.Error("a nil preflight report must result in no install attempt")
	}
}

func TestInstallOverlayPackages_MissingArtifactFails(t *testing.T) {
	// DownloadDir is empty: the prepared artifact file is absent.
	plan := &ResolutionPlan{
		DownloadDir: t.TempDir(),
		ToInstall:   []ResolvedPackage{{Name: "curl", URL: "https://r/curl_8.deb"}},
	}
	backend := &fakeInstaller{fam: PackageManagerAPT}
	h := &installHarness{}

	var err error
	withStubbedInstall(t, backend, h, func() {
		_, err = InstallOverlayPackages(aptInfo(), "/mnt/root", plan, passedReport())
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected missing-artifact error, got %v", err)
	}
	if backend.installCalls != 0 || len(h.mountedSysfs) != 0 {
		t.Error("a missing artifact must fail before mounting or installing")
	}
}

func TestInstallOverlayPackages_VerifyMissingFails(t *testing.T) {
	dir := writeArtifacts(t, t.TempDir(), "curl_8.deb")
	plan := &ResolutionPlan{
		DownloadDir: dir,
		ToInstall:   []ResolvedPackage{{Name: "curl", URL: "https://r/curl_8.deb"}},
	}
	// Install "succeeds" but verification reports the package missing.
	backend := &fakeInstaller{fam: PackageManagerAPT, missing: []string{"curl"}}
	h := &installHarness{}

	var err error
	withStubbedInstall(t, backend, h, func() {
		_, err = InstallOverlayPackages(aptInfo(), "/mnt/root", plan, passedReport())
	})
	if err == nil || !strings.Contains(err.Error(), "not present after install") {
		t.Fatalf("expected post-install verification failure, got %v", err)
	}
	// The chroot must still be torn down even though install reported success.
	if len(h.umountedSysfs) != 1 || len(h.umountedBinds) != 1 {
		t.Errorf("teardown must run on verification failure: sysfs=%d binds=%d",
			len(h.umountedSysfs), len(h.umountedBinds))
	}
}

func TestInstallOverlayPackages_InstallErrorTearsDown(t *testing.T) {
	dir := writeArtifacts(t, t.TempDir(), "curl_8.deb")
	plan := &ResolutionPlan{
		DownloadDir: dir,
		ToInstall:   []ResolvedPackage{{Name: "curl", URL: "https://r/curl_8.deb"}},
	}
	backend := &fakeInstaller{fam: PackageManagerAPT, installErr: errors.New("dpkg blew up")}
	h := &installHarness{}

	var err error
	withStubbedInstall(t, backend, h, func() {
		_, err = InstallOverlayPackages(aptInfo(), "/mnt/root", plan, passedReport())
	})
	if err == nil || !strings.Contains(err.Error(), "dpkg blew up") {
		t.Fatalf("expected install error to propagate, got %v", err)
	}
	// Even on install failure the mounts established beforehand are torn down.
	if len(h.umountedSysfs) != 1 || len(h.umountedBinds) != 1 {
		t.Errorf("teardown must run on install failure: sysfs=%d binds=%d",
			len(h.umountedSysfs), len(h.umountedBinds))
	}
}

func TestInstallOverlayPackages_DiskFullAddsSizeHint(t *testing.T) {
	dir := writeArtifacts(t, t.TempDir(), "curl_8.deb")
	plan := &ResolutionPlan{
		DownloadDir: dir,
		ToInstall:   []ResolvedPackage{{Name: "curl", URL: "https://r/curl_8.deb"}},
	}
	// Mirror how the deb backend wraps dpkg's captured ENOSPC output into the error.
	backend := &fakeInstaller{
		fam:        PackageManagerAPT,
		installErr: errors.New("dpkg install of 1 artifact(s) failed: exit status 1\n  dpkg: error: No space left on device"),
	}
	h := &installHarness{}

	var err error
	withStubbedInstall(t, backend, h, func() {
		_, err = InstallOverlayPackages(aptInfo(), "/mnt/root", plan, passedReport())
	})
	if err == nil {
		t.Fatal("expected install error to propagate")
	}
	if !strings.Contains(err.Error(), "disk.size") {
		t.Errorf("a no-space install failure must point the user at disk.size, got %v", err)
	}
	// The original diagnostic must still be present alongside the hint.
	if !strings.Contains(err.Error(), "No space left on device") {
		t.Errorf("hint must not replace the underlying diagnostic, got %v", err)
	}
}

func TestDiskSpaceHint(t *testing.T) {
	if got := diskSpaceHint(nil); got != "" {
		t.Errorf("nil error must yield no hint, got %q", got)
	}
	if got := diskSpaceHint(errors.New("exit status 1: dpkg: dependency problems")); got != "" {
		t.Errorf("unrelated failure must yield no hint, got %q", got)
	}
	// Case-insensitive match, since the exact casing varies by tool/kernel.
	for _, msg := range []string{
		"No space left on device",
		"write error: no space left on device",
	} {
		hint := diskSpaceHint(errors.New(msg))
		if !strings.Contains(hint, "disk.size") {
			t.Errorf("ENOSPC error %q must yield a disk.size hint, got %q", msg, hint)
		}
	}
}

func TestInstallOverlayPackages_BindMountFailureRollsBackSysfs(t *testing.T) {
	dir := writeArtifacts(t, t.TempDir(), "curl_8.deb")
	plan := &ResolutionPlan{
		DownloadDir: dir,
		ToInstall:   []ResolvedPackage{{Name: "curl", URL: "https://r/curl_8.deb"}},
	}
	backend := &fakeInstaller{fam: PackageManagerAPT}
	h := &installHarness{bindMountErr: errors.New("bind mount denied")}

	var err error
	withStubbedInstall(t, backend, h, func() {
		_, err = InstallOverlayPackages(aptInfo(), "/mnt/root", plan, passedReport())
	})
	if err == nil || !strings.Contains(err.Error(), "bind-mount artifact cache") {
		t.Fatalf("expected bind-mount failure, got %v", err)
	}
	// The sysfs mount established first must be rolled back, and no install runs.
	if len(h.umountedSysfs) != 1 {
		t.Errorf("sysfs must be rolled back after a bind-mount failure, umounts=%d", len(h.umountedSysfs))
	}
	if backend.installCalls != 0 {
		t.Error("install must not run when the artifact bind mount fails")
	}
}

func TestInstallOverlayPackages_CleanupErrorSurfacedOnSuccess(t *testing.T) {
	dir := writeArtifacts(t, t.TempDir(), "curl_8.deb")
	plan := &ResolutionPlan{
		DownloadDir: dir,
		ToInstall:   []ResolvedPackage{{Name: "curl", URL: "https://r/curl_8.deb"}},
	}
	backend := &fakeInstaller{fam: PackageManagerAPT}
	// Install + verify succeed, but unmounting the artifact cache fails.
	h := &installHarness{umountBindErr: errors.New("device busy")}

	var err error
	withStubbedInstall(t, backend, h, func() {
		_, err = InstallOverlayPackages(aptInfo(), "/mnt/root", plan, passedReport())
	})
	if err == nil || !strings.Contains(err.Error(), "device busy") {
		t.Fatalf("a teardown failure on an otherwise-successful install must surface, got %v", err)
	}
}

func TestInstallOverlayPackages_UnsupportedFamily(t *testing.T) {
	plan := &ResolutionPlan{
		DownloadDir: t.TempDir(),
		ToInstall:   []ResolvedPackage{{Name: "curl", URL: "https://r/curl_8.deb"}},
	}
	info := &BaselineInfo{OS: "x", Arch: "amd64", PackageManager: PackageManager("zypper")}
	// No backend override: exercise the real selectInstaller.
	h := &installHarness{}
	var err error
	withStubbedInstall(t, nil, h, func() {
		_, err = InstallOverlayPackages(info, "/mnt/root", plan, passedReport())
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported package manager") {
		t.Fatalf("expected unsupported-family error, got %v", err)
	}
}

func TestInstallOverlayPackages_NilGuards(t *testing.T) {
	if _, err := InstallOverlayPackages(nil, "/mnt/root", &ResolutionPlan{}, passedReport()); err == nil {
		t.Error("expected error for nil info")
	}
	if _, err := InstallOverlayPackages(aptInfo(), "/mnt/root", nil, passedReport()); err == nil {
		t.Error("expected error for nil plan")
	}
	if _, err := InstallOverlayPackages(aptInfo(), "", &ResolutionPlan{}, passedReport()); err == nil {
		t.Error("expected error for empty root mount")
	}
}

// TestPlanInstalls_PreservesOrder confirms planInstalls maps packages to their
// artifacts WITHOUT re-sorting: it must preserve plan.ToInstall's dependency-first
// order (set by the resolver so dpkg -i can satisfy Pre-Depends left-to-right), so
// a "zpkg before apkg" input stays z.deb before a.deb rather than being
// alphabetized (which would reintroduce the pre-dependency ordering bug).
func TestPlanInstalls_PreservesOrder(t *testing.T) {
	dir := writeArtifacts(t, t.TempDir(), "a.deb", "z.deb")
	plan := &ResolutionPlan{
		DownloadDir: dir,
		ToInstall: []ResolvedPackage{
			{Name: "zpkg", URL: "https://r/z.deb"},
			{Name: "apkg", URL: "https://r/a.deb"},
		},
	}
	items, err := planInstalls(plan)
	if err != nil {
		t.Fatalf("planInstalls: %v", err)
	}
	got := []string{items[0].artifact, items[1].artifact}
	if !reflect.DeepEqual(got, []string{"z.deb", "a.deb"}) {
		t.Errorf("artifacts = %v, want ToInstall order [z.deb a.deb]", got)
	}
}

func TestPlanInstalls_NoDownloadDirFails(t *testing.T) {
	plan := &ResolutionPlan{ToInstall: []ResolvedPackage{{Name: "curl", URL: "https://r/curl.deb"}}}
	if _, err := planInstalls(plan); err == nil || !strings.Contains(err.Error(), "no artifact download directory") {
		t.Fatalf("expected missing-download-dir error, got %v", err)
	}
}

func TestPlanInstalls_EmptyIsNoError(t *testing.T) {
	items, err := planInstalls(&ResolutionPlan{})
	if err != nil || items != nil {
		t.Errorf("empty plan should yield (nil, nil), got items=%v err=%v", items, err)
	}
}

func TestArtifactFileFor(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://repo.example.com/pool/main/c/curl/curl_8.0_amd64.deb", "curl_8.0_amd64.deb"},
		{"https://r/path/glibc-2.38.rpm", "glibc-2.38.rpm"},
		{"file:///srv/cache/vim.rpm", "vim.rpm"},
		// Real package filenames legitimately contain '+' and '~'; these must pass
		// (they are not restricted to a strict alnum allowlist).
		{"https://r/pool/libstdc++6_13.2_amd64.deb", "libstdc++6_13.2_amd64.deb"},
		{"https://r/pool/foo_1.0~beta1_amd64.deb", "foo_1.0~beta1_amd64.deb"},
	}
	for _, tt := range tests {
		got, err := artifactFileFor(ResolvedPackage{Name: "p", URL: tt.url})
		if err != nil {
			t.Errorf("artifactFileFor(%q): %v", tt.url, err)
			continue
		}
		if got != tt.want {
			t.Errorf("artifactFileFor(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}

	if _, err := artifactFileFor(ResolvedPackage{Name: "noURL"}); err == nil {
		t.Error("expected error for a package with no artifact URL")
	}

	// A URL whose basename resolves to a traversal segment or non-filename must be
	// rejected, so it can never redirect the <downloadDir>/<base> join outside the
	// artifact directory.
	for _, bad := range []string{
		"https://r/pool/..",        // base == ".." (the traversal case)
		"https://r/pool/.",         // base == "."
		`https://r/pool/a\..\evil`, // backslash separator (Windows-style traversal)
	} {
		if got, err := artifactFileFor(ResolvedPackage{Name: "bad", URL: bad}); err == nil {
			t.Errorf("artifactFileFor(%q) = %q, want error (unsafe basename)", bad, got)
		}
	}
}

func TestRecordCleanupError(t *testing.T) {
	// No primary error: cleanup error becomes the result.
	var e1 error
	recordCleanupError(&e1, errors.New("unmount failed"))
	if e1 == nil || !strings.Contains(e1.Error(), "unmount failed") {
		t.Errorf("expected cleanup error to be set, got %v", e1)
	}

	// Primary error present: cleanup error is appended, primary is preserved.
	e2 := errors.New("install failed")
	recordCleanupError(&e2, errors.New("unmount failed"))
	if !strings.Contains(e2.Error(), "install failed") || !strings.Contains(e2.Error(), "unmount failed") {
		t.Errorf("expected both errors surfaced, got %v", e2)
	}

	// nil target is a no-op (must not panic).
	recordCleanupError(nil, errors.New("ignored"))
}

func TestFormatCommandOutput(t *testing.T) {
	// Empty (or whitespace-only) output renders as nothing to append.
	if got := formatCommandOutput(""); got != "" {
		t.Errorf("empty output = %q, want \"\"", got)
	}
	if got := formatCommandOutput("  \n\t "); got != "" {
		t.Errorf("whitespace-only output = %q, want \"\"", got)
	}

	// A single line is surrounded by a leading newline and indented two spaces.
	if got := formatCommandOutput("dpkg: error"); got != "\n  dpkg: error" {
		t.Errorf("single line = %q, want %q", got, "\n  dpkg: error")
	}

	// Every line of multi-line output is indented, matching the doc's "indented
	// block" description. Surrounding whitespace is trimmed first.
	got := formatCommandOutput("\nline one\nline two\n")
	want := "\n  line one\n  line two"
	if got != want {
		t.Errorf("multi-line = %q, want %q", got, want)
	}
}

func TestSelectInstaller(t *testing.T) {
	apt, err := selectInstaller(PackageManagerAPT)
	if err != nil || apt.family() != PackageManagerAPT {
		t.Errorf("apt backend = %v, %v", apt, err)
	}
	dnf, err := selectInstaller(PackageManagerDNF)
	if err != nil || dnf.family() != PackageManagerDNF {
		t.Errorf("dnf backend = %v, %v", dnf, err)
	}
	if _, err := selectInstaller(PackageManager("apk")); err == nil {
		t.Error("expected error for unsupported family")
	}
}

// TestInstallOverlayPackages_Deterministic confirms reordered ToInstall inputs
// produce identical install ordering and result.
func TestInstallOverlayPackages_Deterministic(t *testing.T) {
	dir := writeArtifacts(t, t.TempDir(), "a.deb", "b.deb", "c.deb")
	run := func(toInstall []ResolvedPackage) *InstallResult {
		plan := &ResolutionPlan{DownloadDir: dir, ToInstall: toInstall}
		backend := &fakeInstaller{fam: PackageManagerAPT}
		var result *InstallResult
		var err error
		withStubbedInstall(t, backend, &installHarness{}, func() {
			result, err = InstallOverlayPackages(aptInfo(), "/mnt/root", plan, passedReport())
		})
		if err != nil {
			t.Fatalf("InstallOverlayPackages: %v", err)
		}
		return result
	}
	a := run([]ResolvedPackage{
		{Name: "c", URL: "https://r/c.deb"},
		{Name: "a", URL: "https://r/a.deb"},
		{Name: "b", URL: "https://r/b.deb"},
	})
	b := run([]ResolvedPackage{
		{Name: "b", URL: "https://r/b.deb"},
		{Name: "c", URL: "https://r/c.deb"},
		{Name: "a", URL: "https://r/a.deb"},
	})
	// Installed names are sorted; confirm both runs agree.
	sort.Strings(a.Installed)
	sort.Strings(b.Installed)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("results differ for reordered inputs:\n a=%+v\n b=%+v", a, b)
	}
}

// stubExecutor is a shell.Executor that returns a canned ExecCmdSilent result,
// letting the deb/rpm verifyInstalled backends be exercised without a chroot.
type stubExecutor struct {
	shell.Executor // embedded so unused methods panic if ever called
	out            string
	err            error
}

func (s *stubExecutor) ExecCmdSilent(string, bool, string, []string) (string, error) {
	return s.out, s.err
}

// capturingExecutor records every command string built by the install backends
// so tests can assert on how arguments are passed (e.g. the "--" terminator),
// while returning a canned success output.
type capturingExecutor struct {
	shell.Executor // embedded so unused methods panic if ever called
	cmds           []string
	out            string
}

func (c *capturingExecutor) ExecCmdWithStream(cmd string, _ bool, _ string, _ []string) (string, error) {
	c.cmds = append(c.cmds, cmd)
	return c.out, nil
}

func (c *capturingExecutor) ExecCmdSilent(cmd string, _ bool, _ string, _ []string) (string, error) {
	c.cmds = append(c.cmds, cmd)
	return c.out, nil
}

// stubShell swaps shell.Default with s for the duration of the test.
func stubShell(t *testing.T, s shell.Executor) {
	t.Helper()
	prev := shell.Default
	shell.Default = s
	t.Cleanup(func() { shell.Default = prev })
}

// TestVerifyInstalled_DistinguishesMissingFromToolFailure guards the review fix:
// an expected "is not installed" diagnostic reports the package as missing (nil
// error), while any other query failure (tool absent, corrupt DB) surfaces a real
// error instead of masquerading as a missing package.
func TestVerifyInstalled_DistinguishesMissingFromToolFailure(t *testing.T) {
	pkgs := []ResolvedPackage{{Name: "curl", URL: "https://r/curl.deb"}}
	tests := []struct {
		name        string
		backend     installerBackend
		out         string
		err         error
		wantMissing []string
		wantErr     bool
	}{
		{
			name:        "deb installed",
			backend:     &debInstallerBackend{},
			out:         "Status: install ok installed\n",
			wantMissing: nil,
		},
		{
			name:        "deb genuinely absent",
			backend:     &debInstallerBackend{},
			out:         "dpkg-query: package 'curl' is not installed and no information is available\n",
			err:         errors.New("exit status 1"),
			wantMissing: []string{"curl"},
		},
		{
			name:    "deb tool failure surfaces error",
			backend: &debInstallerBackend{},
			out:     "bash: dpkg: command not found\n",
			err:     errors.New("exit status 127"),
			wantErr: true,
		},
		{
			name:        "rpm installed",
			backend:     &rpmInstallerBackend{},
			out:         "curl-8.0-1.x86_64\n",
			wantMissing: nil,
		},
		{
			name:        "rpm genuinely absent",
			backend:     &rpmInstallerBackend{},
			out:         "package curl is not installed\n",
			err:         errors.New("exit status 1"),
			wantMissing: []string{"curl"},
		},
		{
			name:    "rpm DB failure surfaces error",
			backend: &rpmInstallerBackend{},
			out:     "error: rpmdb: BDB0113 Thread died in Berkeley DB library\n",
			err:     errors.New("exit status 2"),
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stubShell(t, &stubExecutor{out: tc.out, err: tc.err})
			missing, err := tc.backend.verifyInstalled("/mnt/root", pkgs)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected a real verification error, got missing=%v err=nil", missing)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(missing, tc.wantMissing) {
				t.Errorf("missing = %v, want %v", missing, tc.wantMissing)
			}
		})
	}
}

func TestHasUnmetDependencyMarker(t *testing.T) {
	broken := []string{
		"You might want to run 'apt --fix-broken install' to correct these.\nThe following packages have unmet dependencies:",
		"Error: \n Problem: broken dependency detected",
		"Depsolve Error occurred",
		// dnf per-package line shape — must be gated too, or it is misclassified as a
		// tool failure and never reaches parseUnmetDependencyPackages.
		"foo-1.0-1.x86_64 has missing requires of bar",
	}
	for _, out := range broken {
		if !hasUnmetDependencyMarker(out) {
			t.Errorf("expected %q to be recognized as unmet-dependency output", out)
		}
	}
	healthy := []string{"", "Reading package lists...", "bash: apt-get: command not found"}
	for _, out := range healthy {
		if hasUnmetDependencyMarker(out) {
			t.Errorf("output %q must not be flagged as unmet dependencies", out)
		}
	}
}

// TestAuditDependencies_DistinguishesBrokenFromToolFailure guards the audit's
// three-way outcome: a clean exit is healthy, a non-zero exit carrying the
// unmet-dependency marker is genuine breakage, and any other failure (tool absent)
// is a real audit error the caller treats as "cannot audit" rather than breakage.
func TestAuditDependencies_DistinguishesBrokenFromToolFailure(t *testing.T) {
	tests := []struct {
		name       string
		backend    installerBackend
		out        string
		err        error
		wantBroken []string // expected offending package names (nil = healthy)
		wantErr    bool
	}{
		{name: "deb healthy", backend: &debInstallerBackend{}, out: "", err: nil, wantBroken: nil},
		{
			name:       "deb broken",
			backend:    &debInstallerBackend{},
			out:        "The following packages have unmet dependencies:\n libfoo : Depends: libbar but it is not installed",
			err:        errors.New("exit status 100"),
			wantBroken: []string{"libfoo | Depends: libbar but it is not installed"},
		},
		{name: "deb tool absent", backend: &debInstallerBackend{}, out: "bash: apt-get: command not found", err: errors.New("exit status 127"), wantErr: true},
		{
			// Marked broken (marker present) but no package parseable: must be an error,
			// not a clean empty set that the caller would read as "no new breakage".
			name:    "deb broken-but-unparseable",
			backend: &debInstallerBackend{},
			out:     "E: Unmet dependencies. Try 'apt --fix-broken install' with no packages",
			err:     errors.New("exit status 100"),
			wantErr: true,
		},
		{name: "rpm healthy", backend: &rpmInstallerBackend{}, out: "", err: nil, wantBroken: nil},
		{
			name:    "rpm broken",
			backend: &rpmInstallerBackend{},
			out:     "package foo-1.0-1.x86_64 requires bar, but none of the providers can be installed\nfoo-1.0-1.x86_64 has broken dependencies",
			err:     errors.New("exit status 1"),
			// NEVRA reduced to name.arch; two distinct failures (the requires + the bare
			// "has broken dependencies"), sorted.
			wantBroken: []string{"foo.x86_64 | ", "foo.x86_64 | bar, but none of the providers can be installed"},
		},
		{name: "rpm tool absent", backend: &rpmInstallerBackend{}, out: "bash: dnf: command not found", err: errors.New("exit status 127"), wantErr: true},
		{
			// A bare "Depsolve Error" carries the broken marker but no parseable package.
			name:    "rpm broken-but-unparseable",
			backend: &rpmInstallerBackend{},
			out:     "Error: Depsolve Error occurred",
			err:     errors.New("exit status 1"),
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stubShell(t, &stubExecutor{out: tc.out, err: tc.err})
			broken, _, err := tc.backend.auditDependencies("/mnt/root")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an audit-tool error, got broken=%v err=nil", broken)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(broken, tc.wantBroken) {
				t.Errorf("broken = %v, want %v", broken, tc.wantBroken)
			}
		})
	}
}

// TestParseUnmetDependencyFailures covers the apt-get and dnf report line shapes,
// the (identity | requirement) descriptor format, deb-arch preservation, rpm NEVRA
// reduction, and de-duplication.
func TestParseUnmetDependencyFailures(t *testing.T) {
	// apt: identity keeps :arch (multiarch instances are distinct); requirement is
	// the text after ':'.
	apt := "The following packages have unmet dependencies:\n" +
		" libfoo : Depends: libbar (>= 2) but it is not installable\n" +
		" libc6:amd64 : Depends: libqux but it is not installed\n"
	got := parseUnmetDependencyFailures(apt)
	want := []string{
		"libc6:amd64 | Depends: libqux but it is not installed",
		"libfoo | Depends: libbar (>= 2) but it is not installable",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("apt parse = %v, want %v", got, want)
	}

	// dnf: NEVRA reduced to name.arch so an upgrade's version churn does not look new;
	// the repeat "foo ... requires quux" is a distinct requirement, not a dup.
	dnf := "package foo-1.0-1.x86_64 requires bar >= 2, but none of the providers can be installed\n" +
		"baz-2.0-3.noarch has broken dependencies\n" +
		"foo-1.0-1.x86_64 requires quux\n"
	got = parseUnmetDependencyFailures(dnf)
	want = []string{
		"baz.noarch | ",
		"foo.x86_64 | bar >= 2, but none of the providers can be installed",
		"foo.x86_64 | quux",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("dnf parse = %v, want %v", got, want)
	}

	// The SAME package upgraded (NEVRA changes) but missing the SAME requirement must
	// produce the SAME descriptor, so a pre/post diff does not flag it as new.
	before := parseUnmetDependencyFailures("foo-1.0-1.x86_64 requires bar")
	after := parseUnmetDependencyFailures("foo-1.1-2.x86_64 requires bar")
	if !reflect.DeepEqual(before, after) {
		t.Errorf("NEVRA change must not alter the descriptor: before=%v after=%v", before, after)
	}

	if got := parseUnmetDependencyFailures(""); got != nil {
		t.Errorf("empty output must yield nil, got %v", got)
	}
}

// TestRpmNameArch covers NEVRA reduction to a version-independent name.arch identity.
func TestRpmNameArch(t *testing.T) {
	cases := map[string]string{
		"kernel-core-6.8.0-1.x86_64": "kernel-core.x86_64", // name with '-'
		"foo-1.0-1.x86_64":           "foo.x86_64",
		"baz-2.0-3.noarch":           "baz.noarch",
		"nodots":                     "nodots",     // not a NEVRA: no arch
		"foo.x86_64":                 "foo.x86_64", // too few '-' fields: returned as-is
	}
	for in, want := range cases {
		if got := rpmNameArch(in); got != want {
			t.Errorf("rpmNameArch(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestInstallCommandsTerminateOptions guards that every package-manager command
// passes "--" before its file/name operands. Artifact basenames are URL-derived
// and package names are externally influenced, so a value beginning with '-'
// would otherwise be parsed as a dpkg/rpm option even though it is shell-quoted
// (quoting stops word-splitting, not option parsing).
func TestInstallCommandsTerminateOptions(t *testing.T) {
	// A leading-dash artifact name is the value that must survive as an operand.
	req := installRequest{
		chrootPath:        "/mnt/root",
		artifactChrootDir: chrootArtifactDir,
		items:             []plannedInstall{{pkg: ResolvedPackage{Name: "-weird"}, artifact: "-weird.deb"}},
	}
	pkgs := []ResolvedPackage{{Name: "-weird"}}

	tests := []struct {
		name string
		want string // substring the built command must contain
		run  func(shell.Executor)
	}{
		{
			name: "dpkg install",
			want: "dpkg -i --auto-deconfigure -- ",
			run:  func(shell.Executor) { _ = (&debInstallerBackend{}).install(req) },
		},
		{
			name: "dpkg verify",
			want: "dpkg -s -- ",
			run:  func(shell.Executor) { _, _ = (&debInstallerBackend{}).verifyInstalled("/mnt/root", pkgs) },
		},
		{
			name: "rpm install",
			want: "rpm -i -v -- ",
			run:  func(shell.Executor) { _ = (&rpmInstallerBackend{}).install(req) },
		},
		{
			name: "rpm verify",
			want: "rpm -q -- ",
			run:  func(shell.Executor) { _, _ = (&rpmInstallerBackend{}).verifyInstalled("/mnt/root", pkgs) },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// dpkg -s reports installed so verifyInstalled treats the name as present
			// and does not surface a spurious error; the other commands ignore output.
			cap := &capturingExecutor{out: "Status: install ok installed\n"}
			stubShell(t, cap)
			tc.run(cap)
			if len(cap.cmds) == 0 {
				t.Fatalf("expected at least one command, got none")
			}
			// The command carrying the file/name operands is the one under test —
			// the first command in every case (the deb install also issues a second
			// `dpkg --configure -a` with no operands, which is exercised separately).
			operandCmd := cap.cmds[0]
			if !strings.Contains(operandCmd, tc.want) {
				t.Errorf("command %q does not contain option terminator %q", operandCmd, tc.want)
			}
			// The "--" must precede the (quoted) leading-dash operand.
			if idx := strings.Index(operandCmd, "--"); idx == -1 || strings.Index(operandCmd, "-weird") < idx {
				t.Errorf("operand not protected by leading %q: %q", "--", operandCmd)
			}
		})
	}
}

// scriptedExecutor returns a queued (output, error) per ExecCmdWithStream call,
// recording every command. It lets a test drive the deb install's retry loop: fail
// the first `dpkg -i` (a Pre-Depends left a package unconfigured), then succeed.
type scriptedExecutor struct {
	shell.Executor // embedded so unused methods panic if ever called
	cmds           []string
	results        []struct {
		out string
		err error
	}
	idx int
}

func (s *scriptedExecutor) ExecCmdWithStream(cmd string, _ bool, _ string, _ []string) (string, error) {
	s.cmds = append(s.cmds, cmd)
	if s.idx >= len(s.results) {
		return "", nil // default: success
	}
	r := s.results[s.idx]
	s.idx++
	return r.out, r.err
}

// TestDebInstallSucceedsFirstPass: when each `dpkg -i` succeeds immediately, the
// backend processes each artifact in order and performs a final configuration.
// --auto-deconfigure covers the transiently-Breaks case
// (vim-runtime Breaks vim-tiny (<< newver) while both upgrade in one batch), which
// the preflight gate permits because the break is self-resolving within the set.
func TestDebInstallSucceedsFirstPass(t *testing.T) {
	req := installRequest{
		chrootPath:        "/mnt/root",
		artifactChrootDir: chrootArtifactDir,
		items: []plannedInstall{
			{pkg: ResolvedPackage{Name: "vim-runtime"}, artifact: "vim-runtime_9.deb"},
			{pkg: ResolvedPackage{Name: "vim-tiny"}, artifact: "vim-tiny_9.deb"},
		},
	}
	cap := &capturingExecutor{}
	stubShell(t, cap)
	if err := (&debInstallerBackend{}).install(req); err != nil {
		t.Fatalf("install: %v", err)
	}
	if len(cap.cmds) != 3 {
		t.Fatalf("expected two installs and final configure, got %v", cap.cmds)
	}
	if !strings.HasPrefix(cap.cmds[0], "dpkg -i --auto-deconfigure -- ") {
		t.Errorf("install command wrong: %q", cap.cmds[0])
	}
	if !strings.HasPrefix(cap.cmds[1], "dpkg -i --auto-deconfigure -- ") {
		t.Errorf("second install command wrong: %q", cap.cmds[1])
	}
	if cap.cmds[2] != "dpkg --configure -a --auto-deconfigure" {
		t.Errorf("final configure command wrong: %q", cap.cmds[2])
	}
}

// TestDebInstallRetriesForPreDepends: the first `dpkg -i` and interim configure
// fail because a Pre-Depends chain is incomplete, so the backend retries and then
// succeeds once the missing pre-dependency is configured.
func TestDebInstallRetriesForPreDepends(t *testing.T) {
	req := installRequest{
		chrootPath:        "/mnt/root",
		artifactChrootDir: chrootArtifactDir,
		items: []plannedInstall{
			{pkg: ResolvedPackage{Name: "libmpfr6"}, artifact: "libmpfr6_4.deb"},
			{pkg: ResolvedPackage{Name: "gawk"}, artifact: "gawk_5.deb"},
		},
	}
	sc := &scriptedExecutor{results: []struct {
		out string
		err error
	}{
		{out: "Setting up libmpfr6", err: nil},
		{out: "gawk pre-depends on libmpfr6 ... not installing gawk", err: errors.New("exit status 1")},
		{out: "Setting up libmpfr6", err: nil},
		{out: "Setting up gawk", err: nil},
		{out: "", err: nil},
	}}
	stubShell(t, sc)
	if err := (&debInstallerBackend{}).install(req); err != nil {
		t.Fatalf("install should have recovered on retry: %v", err)
	}
	if len(sc.cmds) != 5 {
		t.Fatalf("expected provider, dependent, configure, retry, configure; got %d: %v", len(sc.cmds), sc.cmds)
	}
	if !strings.HasPrefix(sc.cmds[0], "dpkg -i --auto-deconfigure -- ") {
		t.Errorf("cmd[0] not the first install pass: %q", sc.cmds[0])
	}
	if !strings.HasPrefix(sc.cmds[1], "dpkg -i --auto-deconfigure -- ") {
		t.Errorf("cmd[1] not the second install: %q", sc.cmds[1])
	}
	if sc.cmds[2] != "dpkg --configure -a --auto-deconfigure" || !strings.Contains(sc.cmds[3], "gawk_5.deb") || sc.cmds[4] != "dpkg --configure -a --auto-deconfigure" {
		t.Errorf("commands should configure then retry only gawk: %v", sc.cmds)
	}
}

func TestDebInstallConfiguresAfterOrderedArtifacts(t *testing.T) {
	req := installRequest{
		chrootPath:        "/mnt/root",
		artifactChrootDir: chrootArtifactDir,
		items: []plannedInstall{
			{pkg: ResolvedPackage{Name: "libmpfr6"}, artifact: "libmpfr6_4.deb"},
			{pkg: ResolvedPackage{Name: "gawk"}, artifact: "gawk_5.deb"},
		},
	}
	sc := &scriptedExecutor{results: []struct {
		out string
		err error
	}{
		{out: "Setting up libmpfr6", err: nil},
		{out: "gawk pre-depends on libmpfr6 ... not installing gawk", err: errors.New("exit status 1")},
		{out: "Setting up libmpfr6", err: nil},
		{out: "Setting up gawk", err: nil},
		{out: "", err: nil},
	}}
	stubShell(t, sc)

	if err := (&debInstallerBackend{}).install(req); err != nil {
		t.Fatalf("install should succeed after configure: %v", err)
	}
	if len(sc.cmds) != 5 {
		t.Fatalf("expected provider, dependent, configure, retry, configure; got %d: %v", len(sc.cmds), sc.cmds)
	}
	if !strings.Contains(sc.cmds[3], "gawk_5.deb") {
		t.Errorf("cmd[3] not the retried dependent artifact: %q", sc.cmds[3])
	}
	if sc.cmds[4] != "dpkg --configure -a --auto-deconfigure" {
		t.Errorf("cmd[4] not the final configure: %q", sc.cmds[4])
	}
}

func TestDebInstallContinuesAfterIndividualFailure(t *testing.T) {
	req := installRequest{
		chrootPath:        "/mnt/root",
		artifactChrootDir: chrootArtifactDir,
		items: []plannedInstall{
			{pkg: ResolvedPackage{Name: "libmpfr6"}, artifact: "libmpfr6_4.deb"},
			{pkg: ResolvedPackage{Name: "gawk"}, artifact: "gawk_5.deb"},
		},
	}
	sc := &scriptedExecutor{results: []struct {
		out string
		err error
	}{
		{out: "Setting up libmpfr6", err: nil},
		{out: "gawk pre-depends on libmpfr6 ... not installing gawk", err: errors.New("exit status 1")},
		{out: "Setting up libmpfr6", err: nil},
		{out: "Setting up gawk", err: nil},
		{out: "", err: nil},
	}}
	stubShell(t, sc)

	if err := (&debInstallerBackend{}).install(req); err != nil {
		t.Fatalf("install should succeed after configuration retries: %v", err)
	}
	if len(sc.cmds) != 5 {
		t.Fatalf("expected provider, dependent, configure, retry, configure; got %d: %v", len(sc.cmds), sc.cmds)
	}
	if !strings.Contains(sc.cmds[3], "gawk_5.deb") {
		t.Errorf("cmd[3] should retry only the failed artifact: %q", sc.cmds[3])
	}
}

// TestDebInstallFailsFastOnNoProgress: when two consecutive `dpkg -i` passes emit
// identical failure output, the set has a genuine problem (not ordering); the
// backend must stop retrying and surface the error rather than loop.
func TestDebInstallFailsFastOnNoProgress(t *testing.T) {
	req := installRequest{
		chrootPath:        "/mnt/root",
		artifactChrootDir: chrootArtifactDir,
		items:             []plannedInstall{{pkg: ResolvedPackage{Name: "broken"}, artifact: "broken_1.deb"}},
	}
	sc := &scriptedExecutor{results: []struct {
		out string
		err error
	}{
		{out: "broken depends on missing-lib; not configured", err: errors.New("exit status 1")},
		{out: "broken depends on missing-lib; not configured", err: errors.New("exit status 1")},
	}}
	stubShell(t, sc)
	err := (&debInstallerBackend{}).install(req)
	if err == nil {
		t.Fatal("expected install to fail fast on no progress")
	}
	if !strings.Contains(err.Error(), "still pending") {
		t.Errorf("error should cite pending archive, got: %v", err)
	}
	if len(sc.cmds) != 2 {
		t.Errorf("expected install and configure, got %d: %v", len(sc.cmds), sc.cmds)
	}
}
