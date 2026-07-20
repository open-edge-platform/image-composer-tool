package overlay

import (
	"errors"
	"strings"
	"testing"
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
		if err := RegenerateBoot(&BaselineInfo{PackageManager: PackageManagerAPT}, "/mnt/root", ir, nil); err != nil {
			t.Errorf("RegenerateBoot(%+v): unexpected error %v", ir, err)
		}
	}
	if called {
		t.Error("initramfs regeneration must not run when nothing was installed")
	}
}

func TestRegenerateBoot_RunsAptCommand(t *testing.T) {
	origExec := bootRegenExec
	origCmdExist := commandExistsFn
	defer func() { bootRegenExec = origExec; commandExistsFn = origCmdExist }()

	commandExistsFn = func(string, string) (bool, error) { return true, nil }
	mounts, umounts := stubSysfsMounts(t)
	var gotCmd, gotRoot string
	bootRegenExec = func(cmd, root string) (string, error) { gotCmd, gotRoot = cmd, root; return "", nil }

	err := RegenerateBoot(&BaselineInfo{PackageManager: PackageManagerAPT}, "/mnt/root", &InstallResult{Installed: []string{"curl"}}, nil)
	if err != nil {
		t.Fatalf("RegenerateBoot: %v", err)
	}
	if !strings.Contains(gotCmd, "update-initramfs") {
		t.Errorf("apt family must call update-initramfs, got %q", gotCmd)
	}
	if gotRoot != "/mnt/root" {
		t.Errorf("regeneration must run in the chroot root, got %q", gotRoot)
	}
	// The pseudo-filesystems must be mounted for the generator and torn down after.
	if *mounts != 1 || *umounts != 1 {
		t.Errorf("sysfs mount/umount = %d/%d, want 1/1", *mounts, *umounts)
	}
}

func TestRegenerateBoot_RunsDnfDracut(t *testing.T) {
	origExec := bootRegenExec
	origCmdExist := commandExistsFn
	defer func() { bootRegenExec = origExec; commandExistsFn = origCmdExist }()

	commandExistsFn = func(string, string) (bool, error) { return true, nil }
	stubSysfsMounts(t)
	var gotCmd string
	bootRegenExec = func(cmd, _ string) (string, error) { gotCmd = cmd; return "", nil }

	err := RegenerateBoot(&BaselineInfo{PackageManager: PackageManagerDNF}, "/mnt/root", &InstallResult{Installed: []string{"vim"}}, nil)
	if err != nil {
		t.Fatalf("RegenerateBoot: %v", err)
	}
	if !strings.Contains(gotCmd, "dracut") {
		t.Errorf("dnf family must call dracut, got %q", gotCmd)
	}
}

func TestRegenerateBoot_SkipsWhenToolAbsent(t *testing.T) {
	origExec := bootRegenExec
	origCmdExist := commandExistsFn
	defer func() { bootRegenExec = origExec; commandExistsFn = origCmdExist }()

	commandExistsFn = func(string, string) (bool, error) { return false, nil } // not present
	called := false
	bootRegenExec = func(string, string) (string, error) { called = true; return "", nil }

	err := RegenerateBoot(&BaselineInfo{PackageManager: PackageManagerAPT}, "/mnt/root", &InstallResult{Installed: []string{"curl"}}, nil)
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

	err := RegenerateBoot(&BaselineInfo{PackageManager: PackageManagerDNF}, "/mnt/root", &InstallResult{Installed: []string{"vim"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "dracut") {
		t.Fatalf("a present-but-failing generator must surface, got %v", err)
	}
}

func TestRegenerateBoot_UnsupportedFamily(t *testing.T) {
	err := RegenerateBoot(&BaselineInfo{PackageManager: PackageManager("apk")}, "/mnt/root", &InstallResult{Installed: []string{"x"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "unsupported package manager") {
		t.Fatalf("expected unsupported-family error, got %v", err)
	}
}

func TestRegenerateBoot_NilGuards(t *testing.T) {
	if err := RegenerateBoot(nil, "/mnt/root", &InstallResult{Installed: []string{"x"}}, nil); err == nil {
		t.Error("expected error for nil info")
	}
	if err := RegenerateBoot(&BaselineInfo{PackageManager: PackageManagerAPT}, "", &InstallResult{Installed: []string{"x"}}, nil); err == nil {
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

	if err := RegenerateBoot(&BaselineInfo{PackageManager: PackageManagerAPT}, "/mnt/root",
		&InstallResult{Installed: []string{"curl", "vim"}}, plan); err != nil {
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

			if err := RegenerateBoot(&BaselineInfo{PackageManager: PackageManagerAPT}, "/mnt/root",
				&InstallResult{Installed: []string{"pkg"}}, plan); err != nil {
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
		if err := RegenerateBoot(&BaselineInfo{PackageManager: PackageManagerAPT}, "/mnt/root",
			&InstallResult{Installed: []string{"curl"}}, nil); err != nil {
			t.Fatalf("RegenerateBoot: %v", err)
		}
		if !*ran {
			t.Error("a nil plan cannot prove userspace-only; must regenerate")
		}
	})
	t.Run("missing download dir", func(t *testing.T) {
		ran := aptRegenProbes(t)
		plan := &ResolutionPlan{ToInstall: []ResolvedPackage{{Name: "curl", URL: "http://x/curl.deb"}}}
		if err := RegenerateBoot(&BaselineInfo{PackageManager: PackageManagerAPT}, "/mnt/root",
			&InstallResult{Installed: []string{"curl"}}, plan); err != nil {
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
		if err := RegenerateBoot(&BaselineInfo{PackageManager: PackageManagerAPT}, "/mnt/root",
			&InstallResult{Installed: []string{"curl"}}, plan); err != nil {
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

func TestInitramfsCommand(t *testing.T) {
	cmd, tool, err := initramfsCommand(PackageManagerAPT)
	if err != nil || tool != "update-initramfs" || !strings.Contains(cmd, "-k all") {
		t.Errorf("apt: cmd=%q tool=%q err=%v", cmd, tool, err)
	}
	cmd, tool, err = initramfsCommand(PackageManagerDNF)
	if err != nil || tool != "dracut" || !strings.Contains(cmd, "--regenerate-all") {
		t.Errorf("dnf: cmd=%q tool=%q err=%v", cmd, tool, err)
	}
	if _, _, err := initramfsCommand(PackageManager("zypper")); err == nil {
		t.Error("expected error for unsupported family")
	}
}
