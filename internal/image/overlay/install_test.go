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
	missing      []string
	verifyErr    error
	gotReq       installRequest
	installCalls int
	verifyCalls  int
}

func (f *fakeInstaller) family() PackageManager { return f.fam }

func (f *fakeInstaller) install(req installRequest) error {
	f.installCalls++
	f.gotReq = req
	return f.installErr
}

func (f *fakeInstaller) verifyInstalled(_ string, _ []ResolvedPackage) ([]string, error) {
	f.verifyCalls++
	return f.missing, f.verifyErr
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
		name     string
		upgrades int
		want     bool
	}{
		{"plan with an upgrade sets the flag", 1, true},
		{"pure-add plan leaves the flag off", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &fakeInstaller{fam: PackageManagerDNF}
			report := &PreflightReport{Blocked: false, Upgrades: tc.upgrades}
			withStubbedInstall(t, backend, &installHarness{}, func() {
				if _, err := InstallOverlayPackages(aptInfo(), "/mnt/root", plan, report); err != nil {
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

func TestPlanInstalls_MapsAndSorts(t *testing.T) {
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
	if !reflect.DeepEqual(got, []string{"a.deb", "z.deb"}) {
		t.Errorf("artifacts = %v, want sorted [a.deb z.deb]", got)
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
			if len(cap.cmds) != 1 {
				t.Fatalf("expected exactly one command, got %v", cap.cmds)
			}
			if !strings.Contains(cap.cmds[0], tc.want) {
				t.Errorf("command %q does not contain option terminator %q", cap.cmds[0], tc.want)
			}
			// The "--" must precede the (quoted) leading-dash operand.
			if idx := strings.Index(cap.cmds[0], "--"); idx == -1 || strings.Index(cap.cmds[0], "-weird") < idx {
				t.Errorf("operand not protected by leading %q: %q", "--", cap.cmds[0])
			}
		})
	}
}

// TestDebInstallUsesAutoDeconfigure guards that the deb install passes
// --auto-deconfigure. Without it, dpkg unpacks artifacts in command-line order and
// aborts ("deconfiguration is not permitted") when a to-be-unpacked artifact
// transiently Breaks an installed package that is ALSO being upgraded later in the
// same batch (the vim-runtime Breaks vim-tiny (<< newver) case): the old version is
// still present at unpack time. The break is self-resolving within the batch, so
// the preflight gate permits it; the flag lets dpkg complete it by temporarily
// deconfiguring and then reconfiguring the affected package.
func TestDebInstallUsesAutoDeconfigure(t *testing.T) {
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
	if len(cap.cmds) != 1 {
		t.Fatalf("expected exactly one command, got %v", cap.cmds)
	}
	if !strings.Contains(cap.cmds[0], "--auto-deconfigure") {
		t.Errorf("dpkg install command missing --auto-deconfigure: %q", cap.cmds[0])
	}
}
