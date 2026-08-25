package overlay

import (
	"errors"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

// stubSysfsMounts swaps the sysfs mount/unmount seams (shared with the install
// stage) for no-ops so boot-regen tests that reach the generator do not need root,
// and records the mount/unmount call counts.
func stubSysfsMounts(t *testing.T) (mounts, umounts *int) {
	t.Helper()
	origMount, origUmount := mountSysfs, umountSysfs
	t.Cleanup(func() { mountSysfs, umountSysfs = origMount, origUmount })
	var m, u int
	mountSysfs = func(string) error { m++; return nil }
	umountSysfs = func(string) error { u++; return nil }
	return &m, &u
}

func TestRegenerateBoot_SkipsWhenNothingInstalled(t *testing.T) {
	origExec := bootRegenExec
	defer func() { bootRegenExec = origExec }()
	called := false
	bootRegenExec = func(string, string) (string, error) { called = true; return "", nil }

	cases := []*InstallResult{
		nil,
		{Skipped: true},
		{Installed: nil},
	}
	for _, ir := range cases {
		if err := RegenerateBoot(nil, &BaselineInfo{PackageManager: PackageManagerAPT}, "/mnt/root", ir, nil, false); err != nil {
			t.Errorf("RegenerateBoot(%+v): unexpected error %v", ir, err)
		}
	}
	if called {
		t.Error("initramfs regeneration must not run when nothing was installed")
	}
}

// presentTools makes commandExistsFn report only the named tools as present, so a
// test can model exactly which initramfs generator the baseline ships.
func presentTools(t *testing.T, tools ...string) {
	t.Helper()
	orig := commandExistsFn
	t.Cleanup(func() { commandExistsFn = orig })
	set := make(map[string]bool, len(tools))
	for _, tool := range tools {
		set[tool] = true
	}
	commandExistsFn = func(tool, _ string) (bool, error) { return set[tool], nil }
}

// TestRegenerateBoot_SelectsGeneratorByWhatIsInstalled asserts the generator is
// chosen by what the baseline actually ships, NOT by package-manager family: an
// APT baseline that ships update-initramfs uses it, but an APT baseline that
// swapped to dracut (via allowPackageRemoval) regenerates with dracut. dracut wins
// when both are transiently present.
func TestRegenerateBoot_SelectsGeneratorByWhatIsInstalled(t *testing.T) {
	tests := []struct {
		name    string
		family  PackageManager
		present []string
		wantCmd string
	}{
		{"apt baseline with update-initramfs", PackageManagerAPT, []string{"update-initramfs"}, "update-initramfs"},
		{"apt baseline swapped to dracut", PackageManagerAPT, []string{"dracut"}, "dracut"},
		{"dnf baseline with dracut", PackageManagerDNF, []string{"dracut"}, "dracut"},
		{"both present prefers dracut", PackageManagerAPT, []string{"dracut", "update-initramfs"}, "dracut"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origExec := bootRegenExec
			t.Cleanup(func() { bootRegenExec = origExec })
			presentTools(t, tt.present...)
			mounts, umounts := stubSysfsMounts(t)
			var gotCmd, gotRoot string
			bootRegenExec = func(cmd, root string) (string, error) { gotCmd, gotRoot = cmd, root; return "", nil }

			err := RegenerateBoot(nil, &BaselineInfo{PackageManager: tt.family}, "/mnt/root", &InstallResult{Installed: []string{"curl"}}, nil, false)
			if err != nil {
				t.Fatalf("RegenerateBoot: %v", err)
			}
			if !strings.Contains(gotCmd, tt.wantCmd) {
				t.Errorf("expected generator %q, got command %q", tt.wantCmd, gotCmd)
			}
			if gotRoot != "/mnt/root" {
				t.Errorf("regeneration must run in the chroot root, got %q", gotRoot)
			}
			if *mounts != 1 || *umounts != 1 {
				t.Errorf("sysfs mount/umount = %d/%d, want 1/1", *mounts, *umounts)
			}
		})
	}
}

// replaceKernelTemplate builds a minimal template requesting the given
// enableExtraModules under overlayPolicy.replaceKernel.
func replaceKernelTemplate(enableExtraModules string) *config.ImageTemplate {
	return &config.ImageTemplate{
		OverlayPolicy: &config.OverlayPolicy{
			ReplaceKernel: &config.ReplaceKernel{
				Package:            "linux-image-6.11.0-1004-oem",
				EnableExtraModules: enableExtraModules,
			},
		},
	}
}

// TestRegenerateBoot_EnableExtraModulesDracut asserts that when
// replaceKernel.enableExtraModules is set and dracut is the selected generator,
// the modules are passed via --add-drivers on the same regeneration command.
func TestRegenerateBoot_EnableExtraModulesDracut(t *testing.T) {
	origExec := bootRegenExec
	t.Cleanup(func() { bootRegenExec = origExec })
	presentTools(t, "dracut")
	stubSysfsMounts(t)
	var gotCmd string
	bootRegenExec = func(cmd, _ string) (string, error) { gotCmd = cmd; return "", nil }

	err := RegenerateBoot(replaceKernelTemplate("intel_vpu uas"), &BaselineInfo{PackageManager: PackageManagerAPT}, "/mnt/root",
		&InstallResult{Installed: []string{"linux-image-6.11.0-1004-oem"}}, nil, false)
	if err != nil {
		t.Fatalf("RegenerateBoot: %v", err)
	}
	if !strings.Contains(gotCmd, "--regenerate-all") || !strings.Contains(gotCmd, "--add-drivers") || !strings.Contains(gotCmd, "intel_vpu uas") {
		t.Errorf("dracut command = %q, want --regenerate-all and --add-drivers with the requested modules", gotCmd)
	}
}

// TestRegenerateBoot_EnableExtraModulesUpdateInitramfs asserts that when
// replaceKernel.enableExtraModules is set and update-initramfs is the selected
// generator (no --add-drivers equivalent), each module is appended to
// /etc/initramfs-tools/modules before the generator itself runs.
func TestRegenerateBoot_EnableExtraModulesUpdateInitramfs(t *testing.T) {
	origExec := bootRegenExec
	t.Cleanup(func() { bootRegenExec = origExec })
	presentTools(t, "update-initramfs")
	stubSysfsMounts(t)
	var gotCmds []string
	bootRegenExec = func(cmd, _ string) (string, error) { gotCmds = append(gotCmds, cmd); return "", nil }

	err := RegenerateBoot(replaceKernelTemplate("intel_vpu uas"), &BaselineInfo{PackageManager: PackageManagerAPT}, "/mnt/root",
		&InstallResult{Installed: []string{"linux-image-6.11.0-1004-oem"}}, nil, false)
	if err != nil {
		t.Fatalf("RegenerateBoot: %v", err)
	}
	if len(gotCmds) != 3 {
		t.Fatalf("expected 2 module-append commands + 1 generator command, got %v", gotCmds)
	}
	for i, mod := range []string{"intel_vpu", "uas"} {
		if !strings.Contains(gotCmds[i], "/etc/initramfs-tools/modules") || !strings.Contains(gotCmds[i], mod) {
			t.Errorf("cmd[%d] = %q, want it to append %q to /etc/initramfs-tools/modules", i, gotCmds[i], mod)
		}
	}
	if !strings.Contains(gotCmds[2], "update-initramfs") {
		t.Errorf("last command = %q, want the update-initramfs generator run", gotCmds[2])
	}
}

// TestRegenerateBoot_NoExtraModulesLeavesCommandUnchanged confirms an unset
// enableExtraModules (the common case) does not alter the generator command.
func TestRegenerateBoot_NoExtraModulesLeavesCommandUnchanged(t *testing.T) {
	origExec := bootRegenExec
	t.Cleanup(func() { bootRegenExec = origExec })
	presentTools(t, "dracut")
	stubSysfsMounts(t)
	var gotCmd string
	bootRegenExec = func(cmd, _ string) (string, error) { gotCmd = cmd; return "", nil }

	err := RegenerateBoot(nil, &BaselineInfo{PackageManager: PackageManagerAPT}, "/mnt/root", &InstallResult{Installed: []string{"curl"}}, nil, false)
	if err != nil {
		t.Fatalf("RegenerateBoot: %v", err)
	}
	if strings.Contains(gotCmd, "--add-drivers") {
		t.Errorf("no enableExtraModules requested, but command gained --add-drivers: %q", gotCmd)
	}
}

// TestRegenerateBoot_EnableExtraModulesBypassesNothingInstalledGate asserts that
// enableExtraModules still triggers regeneration for a removal-only kernel swap
// (the requested kernel is already installed, so InstallResult.Installed is
// empty) — the case that would otherwise hit the "no packages added" skip gate
// before enableExtraModules is ever read.
func TestRegenerateBoot_EnableExtraModulesBypassesNothingInstalledGate(t *testing.T) {
	ran := aptRegenProbes(t)

	err := RegenerateBoot(replaceKernelTemplate("intel_vpu uas"), &BaselineInfo{PackageManager: PackageManagerAPT}, "/mnt/root",
		&InstallResult{Installed: nil}, nil, false)
	if err != nil {
		t.Fatalf("RegenerateBoot: %v", err)
	}
	if !*ran {
		t.Error("enableExtraModules must regenerate even when nothing was installed (a removal-only swap)")
	}
}

// TestRegenerateBoot_EnableExtraModulesNoGeneratorErrors asserts that
// enableExtraModules with no supported generator present fails loudly, like
// forceRegen, instead of silently dropping the requested modules.
func TestRegenerateBoot_EnableExtraModulesNoGeneratorErrors(t *testing.T) {
	origExec := bootRegenExec
	origCmdExist := commandExistsFn
	defer func() { bootRegenExec = origExec; commandExistsFn = origCmdExist }()

	commandExistsFn = func(string, string) (bool, error) { return false, nil } // no generator present
	called := false
	bootRegenExec = func(string, string) (string, error) { called = true; return "", nil }

	err := RegenerateBoot(replaceKernelTemplate("intel_vpu uas"), &BaselineInfo{PackageManager: PackageManagerAPT}, "/mnt/root",
		&InstallResult{Installed: nil}, nil, false)
	if err == nil || !strings.Contains(err.Error(), "enableExtraModules") {
		t.Fatalf("enableExtraModules with no generator must fail with an actionable error, got %v", err)
	}
	if called {
		t.Error("no generator can run when none is present")
	}
}

// TestRegenerateBoot_ForceRegenNoGeneratorErrors asserts that when forceRegen is
// set (a stage:pre-initramfs additionalFiles entry was copied) but the baseline
// ships NO supported initramfs generator, the build fails with an actionable error
// rather than silently succeeding — otherwise the staged file would never be baked
// into the initramfs while the build claimed it took effect.
func TestRegenerateBoot_ForceRegenNoGeneratorErrors(t *testing.T) {
	origExec := bootRegenExec
	origCmdExist := commandExistsFn
	defer func() { bootRegenExec = origExec; commandExistsFn = origCmdExist }()

	commandExistsFn = func(string, string) (bool, error) { return false, nil } // no generator present
	called := false
	bootRegenExec = func(string, string) (string, error) { called = true; return "", nil }

	err := RegenerateBoot(nil, &BaselineInfo{PackageManager: PackageManagerAPT}, "/mnt/root",
		&InstallResult{Installed: nil}, nil, true)
	if err == nil || !strings.Contains(err.Error(), "pre-initramfs") {
		t.Fatalf("forceRegen with no generator must fail with an actionable error, got %v", err)
	}
	if called {
		t.Error("no generator can run when none is present")
	}
}

func TestRegenerateBoot_SkipsWhenToolAbsent(t *testing.T) {
	origExec := bootRegenExec
	origCmdExist := commandExistsFn
	defer func() { bootRegenExec = origExec; commandExistsFn = origCmdExist }()

	commandExistsFn = func(string, string) (bool, error) { return false, nil } // not present
	called := false
	bootRegenExec = func(string, string) (string, error) { called = true; return "", nil }

	err := RegenerateBoot(nil, &BaselineInfo{PackageManager: PackageManagerAPT}, "/mnt/root", &InstallResult{Installed: []string{"curl"}}, nil, false)
	if err != nil {
		t.Fatalf("absent generator must be a clean no-op, got %v", err)
	}
	if called {
		t.Error("must not run a generator that is not present in the baseline")
	}
}

func TestRegenerateBoot_GeneratorFailureSurfaces(t *testing.T) {
	origExec := bootRegenExec
	origCmdExist := commandExistsFn
	defer func() { bootRegenExec = origExec; commandExistsFn = origCmdExist }()

	commandExistsFn = func(string, string) (bool, error) { return true, nil }
	stubSysfsMounts(t)
	bootRegenExec = func(string, string) (string, error) { return "", errors.New("dracut boom") }

	err := RegenerateBoot(nil, &BaselineInfo{PackageManager: PackageManagerDNF}, "/mnt/root", &InstallResult{Installed: []string{"vim"}}, nil, false)
	if err == nil || !strings.Contains(err.Error(), "dracut") {
		t.Fatalf("a present-but-failing generator must surface, got %v", err)
	}
}

func TestRegenerateBoot_UnsupportedFamily(t *testing.T) {
	err := RegenerateBoot(nil, &BaselineInfo{PackageManager: PackageManager("apk")}, "/mnt/root", &InstallResult{Installed: []string{"x"}}, nil, false)
	if err == nil || !strings.Contains(err.Error(), "unsupported package manager") {
		t.Fatalf("expected unsupported-family error, got %v", err)
	}
}

func TestRegenerateBoot_NilGuards(t *testing.T) {
	if err := RegenerateBoot(nil, nil, "/mnt/root", &InstallResult{Installed: []string{"x"}}, nil, false); err == nil {
		t.Error("expected error for nil info")
	}
	if err := RegenerateBoot(nil, &BaselineInfo{PackageManager: PackageManagerAPT}, "", &InstallResult{Installed: []string{"x"}}, nil, false); err == nil {
		t.Error("expected error for empty root mount")
	}
}

// stubFileList swaps the artifact file-manifest reader for one returning the given
// files per host path (or an error if files is nil for that path), and restores it.
func stubFileList(t *testing.T, byPath map[string][]string, failPaths map[string]bool) {
	t.Helper()
	orig := readArtifactFileList
	t.Cleanup(func() { readArtifactFileList = orig })
	readArtifactFileList = func(_ PackageManager, hostPath string) ([]string, error) {
		if failPaths[hostPath] {
			return nil, errors.New("read boom")
		}
		return byPath[hostPath], nil
	}
}

// aptRegenProbes wires the generator seams for an apt regen and returns whether
// the generator ran, so the boot-relevance gate's skip/run decision is observable.
func aptRegenProbes(t *testing.T) *bool {
	t.Helper()
	origExec, origCmdExist := bootRegenExec, commandExistsFn
	t.Cleanup(func() { bootRegenExec, commandExistsFn = origExec, origCmdExist })
	commandExistsFn = func(string, string) (bool, error) { return true, nil }
	stubSysfsMounts(t)
	ran := false
	bootRegenExec = func(string, string) (string, error) { ran = true; return "", nil }
	return &ran
}

func planWith(pkgs ...ResolvedPackage) *ResolutionPlan {
	return &ResolutionPlan{DownloadDir: "/cache", ToInstall: pkgs}
}

// TestRegenerateBoot_ForceRegenBypassesGates asserts forceRegen makes the
// generator run even when the two "nothing changed" gates would otherwise skip it
// (no packages installed, or a pure-userspace install). This is the case where a
// dracut module / initramfs-tools hook is delivered ONLY via a pre-initramfs
// additionalFiles entry, with no boot-relevant package.
func TestRegenerateBoot_ForceRegenBypassesGates(t *testing.T) {
	t.Run("no packages installed", func(t *testing.T) {
		ran := aptRegenProbes(t)
		if err := RegenerateBoot(nil, &BaselineInfo{PackageManager: PackageManagerAPT}, "/mnt/root",
			&InstallResult{Installed: nil}, nil, true); err != nil {
			t.Fatalf("RegenerateBoot: %v", err)
		}
		if !*ran {
			t.Error("forceRegen must regenerate even when no packages were installed")
		}
	})
	t.Run("pure-userspace install", func(t *testing.T) {
		ran := aptRegenProbes(t)
		plan := planWith(ResolvedPackage{Name: "curl", URL: "http://x/curl.deb"})
		stubFileList(t, map[string][]string{"/cache/curl.deb": {"./usr/bin/curl"}}, nil)
		if err := RegenerateBoot(nil, &BaselineInfo{PackageManager: PackageManagerAPT}, "/mnt/root",
			&InstallResult{Installed: []string{"curl"}}, plan, true); err != nil {
			t.Fatalf("RegenerateBoot: %v", err)
		}
		if !*ran {
			t.Error("forceRegen must regenerate even for a pure-userspace install")
		}
	})
}

func TestRegenerateBoot_SkipsPureUserspaceOverlay(t *testing.T) {
	ran := aptRegenProbes(t)
	plan := planWith(
		ResolvedPackage{Name: "curl", URL: "http://x/curl.deb"},
		ResolvedPackage{Name: "vim", URL: "http://x/vim.deb"},
	)
	stubFileList(t, map[string][]string{
		"/cache/curl.deb": {"./usr/bin/curl", "./usr/share/doc/curl/changelog"},
		"/cache/vim.deb":  {"./usr/bin/vim", "./usr/share/vim/vimrc"},
	}, nil)

	if err := RegenerateBoot(nil, &BaselineInfo{PackageManager: PackageManagerAPT}, "/mnt/root",
		&InstallResult{Installed: []string{"curl", "vim"}}, plan, false); err != nil {
		t.Fatalf("RegenerateBoot: %v", err)
	}
	if *ran {
		t.Error("pure-userspace overlay must skip initramfs regeneration")
	}
}

func TestRegenerateBoot_RunsWhenBootRelevantContentAdded(t *testing.T) {
	cases := map[string]string{
		"kernel module":  "./lib/modules/6.8.0/kernel/drivers/net/foo.ko",
		"firmware":       "./usr/lib/firmware/foo/bar.bin",
		"initramfs hook": "./usr/share/initramfs-tools/hooks/foo",
	}
	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			ran := aptRegenProbes(t)
			plan := planWith(ResolvedPackage{Name: "pkg", URL: "http://x/pkg.deb"})
			stubFileList(t, map[string][]string{"/cache/pkg.deb": {"./usr/bin/pkg", path}}, nil)

			if err := RegenerateBoot(nil, &BaselineInfo{PackageManager: PackageManagerAPT}, "/mnt/root",
				&InstallResult{Installed: []string{"pkg"}}, plan, false); err != nil {
				t.Fatalf("RegenerateBoot: %v", err)
			}
			if !*ran {
				t.Errorf("overlay adding %s must regenerate the initramfs", name)
			}
		})
	}
}

func TestRegenerateBoot_FailSafeRegenerates(t *testing.T) {
	t.Run("nil plan", func(t *testing.T) {
		ran := aptRegenProbes(t)
		if err := RegenerateBoot(nil, &BaselineInfo{PackageManager: PackageManagerAPT}, "/mnt/root",
			&InstallResult{Installed: []string{"curl"}}, nil, false); err != nil {
			t.Fatalf("RegenerateBoot: %v", err)
		}
		if !*ran {
			t.Error("a nil plan cannot prove userspace-only; must regenerate")
		}
	})
	t.Run("missing download dir", func(t *testing.T) {
		ran := aptRegenProbes(t)
		plan := &ResolutionPlan{ToInstall: []ResolvedPackage{{Name: "curl", URL: "http://x/curl.deb"}}}
		if err := RegenerateBoot(nil, &BaselineInfo{PackageManager: PackageManagerAPT}, "/mnt/root",
			&InstallResult{Installed: []string{"curl"}}, plan, false); err != nil {
			t.Fatalf("RegenerateBoot: %v", err)
		}
		if !*ran {
			t.Error("a plan with no download dir must regenerate to be safe")
		}
	})
	t.Run("unreadable manifest", func(t *testing.T) {
		ran := aptRegenProbes(t)
		plan := planWith(ResolvedPackage{Name: "curl", URL: "http://x/curl.deb"})
		stubFileList(t, nil, map[string]bool{"/cache/curl.deb": true})
		if err := RegenerateBoot(nil, &BaselineInfo{PackageManager: PackageManagerAPT}, "/mnt/root",
			&InstallResult{Installed: []string{"curl"}}, plan, false); err != nil {
			t.Fatalf("RegenerateBoot: %v", err)
		}
		if !*ran {
			t.Error("an unreadable manifest must regenerate to be safe")
		}
	})
}

func TestParseArtifactFileList(t *testing.T) {
	// dpkg -c: tar -tv style; the member path is the last field, symlink targets dropped.
	deb := "drwxr-xr-x root/root         0 2024-01-01 00:00 ./usr/bin/\n" +
		"-rwxr-xr-x root/root      1234 2024-01-01 00:00 ./usr/bin/curl\n" +
		"lrwxrwxrwx root/root         0 2024-01-01 00:00 ./usr/lib/x.so -> x.so.1\n"
	got := parseArtifactFileList(PackageManagerAPT, deb)
	want := []string{"./usr/bin/", "./usr/bin/curl", "./usr/lib/x.so"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("deb parse = %v, want %v", got, want)
	}
	// rpm -qlp: one absolute path per line.
	rpm := "/usr/bin/vim\n/lib/modules/6.8/foo.ko\n"
	got = parseArtifactFileList(PackageManagerDNF, rpm)
	if len(got) != 2 || got[1] != "/lib/modules/6.8/foo.ko" {
		t.Errorf("rpm parse = %v", got)
	}
}

func TestPathListHasBootRelevantContent(t *testing.T) {
	// Both dpkg's "./lib/..." and rpm's "/lib/..." forms must match.
	if !pathListHasBootRelevantContent([]string{"./lib/modules/6.8/foo.ko"}) {
		t.Error("dpkg-style module path must be boot-relevant")
	}
	if !pathListHasBootRelevantContent([]string{"/usr/lib/firmware/foo.bin"}) {
		t.Error("rpm-style firmware path must be boot-relevant")
	}
	if pathListHasBootRelevantContent([]string{"./usr/bin/curl", "/usr/share/doc/x"}) {
		t.Error("pure-userspace paths must not be boot-relevant")
	}
	// A path merely containing "modules" elsewhere must not match.
	if pathListHasBootRelevantContent([]string{"./usr/lib/python3/site-packages/modules/x.py"}) {
		t.Error("a non-kernel 'modules' path must not be boot-relevant")
	}
}

func TestSupportedInitramfsFamily(t *testing.T) {
	if err := supportedInitramfsFamily(PackageManagerAPT); err != nil {
		t.Errorf("apt must be supported: %v", err)
	}
	if err := supportedInitramfsFamily(PackageManagerDNF); err != nil {
		t.Errorf("dnf must be supported: %v", err)
	}
	if err := supportedInitramfsFamily(PackageManager("zypper")); err == nil {
		t.Error("expected error for unsupported family")
	}
}

func TestResolveInitramfsGenerator(t *testing.T) {
	// dracut present -> dracut wins (it is first in preference order).
	presentTools(t, "dracut")
	if cmd, tool, found, err := resolveInitramfsGenerator("/mnt/root"); err != nil || !found || tool != "dracut" || !strings.Contains(cmd, "--regenerate-all") {
		t.Errorf("dracut present: cmd=%q tool=%q found=%v err=%v", cmd, tool, found, err)
	}
	// Only update-initramfs present -> it is selected.
	presentTools(t, "update-initramfs")
	if cmd, tool, found, err := resolveInitramfsGenerator("/mnt/root"); err != nil || !found || tool != "update-initramfs" || !strings.Contains(cmd, "-k all") {
		t.Errorf("update-initramfs present: cmd=%q tool=%q found=%v err=%v", cmd, tool, found, err)
	}
	// Both present -> dracut is preferred.
	presentTools(t, "update-initramfs", "dracut")
	if _, tool, found, err := resolveInitramfsGenerator("/mnt/root"); err != nil || !found || tool != "dracut" {
		t.Errorf("both present: tool=%q found=%v err=%v, want dracut", tool, found, err)
	}
	// None present -> found=false, no error.
	presentTools(t)
	if _, _, found, err := resolveInitramfsGenerator("/mnt/root"); err != nil || found {
		t.Errorf("none present: found=%v err=%v, want found=false", found, err)
	}
}
