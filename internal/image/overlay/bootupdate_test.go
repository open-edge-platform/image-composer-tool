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
		if err := RegenerateBoot(&BaselineInfo{PackageManager: PackageManagerAPT}, "/mnt/root", ir); err != nil {
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

	err := RegenerateBoot(&BaselineInfo{PackageManager: PackageManagerAPT}, "/mnt/root", &InstallResult{Installed: []string{"curl"}})
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

	err := RegenerateBoot(&BaselineInfo{PackageManager: PackageManagerDNF}, "/mnt/root", &InstallResult{Installed: []string{"vim"}})
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

	err := RegenerateBoot(&BaselineInfo{PackageManager: PackageManagerAPT}, "/mnt/root", &InstallResult{Installed: []string{"curl"}})
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

	err := RegenerateBoot(&BaselineInfo{PackageManager: PackageManagerDNF}, "/mnt/root", &InstallResult{Installed: []string{"vim"}})
	if err == nil || !strings.Contains(err.Error(), "dracut") {
		t.Fatalf("a present-but-failing generator must surface, got %v", err)
	}
}

func TestRegenerateBoot_UnsupportedFamily(t *testing.T) {
	err := RegenerateBoot(&BaselineInfo{PackageManager: PackageManager("apk")}, "/mnt/root", &InstallResult{Installed: []string{"x"}})
	if err == nil || !strings.Contains(err.Error(), "unsupported package manager") {
		t.Fatalf("expected unsupported-family error, got %v", err)
	}
}

func TestRegenerateBoot_NilGuards(t *testing.T) {
	if err := RegenerateBoot(nil, "/mnt/root", &InstallResult{Installed: []string{"x"}}); err == nil {
		t.Error("expected error for nil info")
	}
	if err := RegenerateBoot(&BaselineInfo{PackageManager: PackageManagerAPT}, "", &InstallResult{Installed: []string{"x"}}); err == nil {
		t.Error("expected error for empty root mount")
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
