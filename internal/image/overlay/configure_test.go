package overlay

import (
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

func overlayConfigTemplate(cmds ...string) *config.ImageTemplate {
	cfgs := make([]config.ConfigurationInfo, 0, len(cmds))
	for _, c := range cmds {
		cfgs = append(cfgs, config.ConfigurationInfo{Cmd: c})
	}
	return &config.ImageTemplate{
		SystemConfig: config.SystemConfig{Configurations: cfgs},
	}
}

func TestRunOverlayConfigurations_NoConfigsIsNoOp(t *testing.T) {
	// No configurations: must return nil WITHOUT touching the mount seams, so a
	// build that does not use the feature never pays the mount/unmount cost. A
	// panic in the (unset here) real mountSysfs would fail this test.
	if err := RunOverlayConfigurations(overlayConfigTemplate(), "/wd/mnt/root"); err != nil {
		t.Fatalf("expected no-op for empty configurations, got %v", err)
	}
}

func TestRunOverlayConfigurations_Validation(t *testing.T) {
	if err := RunOverlayConfigurations(nil, "/wd/mnt/root"); err == nil {
		t.Error("expected error for nil template")
	}
	if err := RunOverlayConfigurations(overlayConfigTemplate("echo hi"), "   "); err == nil {
		t.Error("expected error for empty root mount")
	}
}

func TestRunOverlayConfigurations_RunsCommandsInChroot(t *testing.T) {
	origMount, origUmount := mountSysfs, umountSysfs
	origExec := configExecFn
	defer func() {
		mountSysfs, umountSysfs = origMount, origUmount
		configExecFn = origExec
	}()

	var mounted, umounted int
	mountSysfs = func(string) error { mounted++; return nil }
	umountSysfs = func(string) error { umounted++; return nil }

	var gotCmds []string
	configExecFn = func(cmdStr string, _ bool, _ string, _ []string) (string, error) {
		gotCmds = append(gotCmds, cmdStr)
		return "", nil
	}

	tmpl := overlayConfigTemplate("wget https://x/y.deb", "dpkg -i y.deb || true")
	if err := RunOverlayConfigurations(tmpl, "/wd/mnt/root"); err != nil {
		t.Fatalf("RunOverlayConfigurations: %v", err)
	}

	if mounted != 1 || umounted != 1 {
		t.Errorf("sysfs mount/unmount = %d/%d, want 1/1", mounted, umounted)
	}
	if len(gotCmds) != 2 {
		t.Fatalf("expected 2 chroot commands, got %d: %v", len(gotCmds), gotCmds)
	}
	// Each command must be wrapped as `chroot <root> /bin/bash -c "<cmd>"` with the
	// root single-quoted, so the whole user command reaches bash opaquely (shell
	// operators like `||` survive) and a metacharacter-bearing root stays one arg.
	for i, want := range []string{"wget https://x/y.deb", "dpkg -i y.deb || true"} {
		if !strings.HasPrefix(gotCmds[i], "chroot '/wd/mnt/root' /bin/bash -c ") {
			t.Errorf("cmd %d not chroot-wrapped: %q", i, gotCmds[i])
		}
		if !strings.Contains(gotCmds[i], want) {
			t.Errorf("cmd %d = %q, want it to carry %q", i, gotCmds[i], want)
		}
	}
}

func TestRunOverlayConfigurations_UnmountsOnCommandFailure(t *testing.T) {
	origMount, origUmount := mountSysfs, umountSysfs
	origExec := configExecFn
	defer func() {
		mountSysfs, umountSysfs = origMount, origUmount
		configExecFn = origExec
	}()

	mountSysfs = func(string) error { return nil }
	var umounted int
	umountSysfs = func(string) error { umounted++; return nil }
	configExecFn = func(string, bool, string, []string) (string, error) {
		return "boom", errCommandFailedForTest
	}

	err := RunOverlayConfigurations(overlayConfigTemplate("false"), "/wd/mnt/root")
	if err == nil {
		t.Fatal("expected command failure to propagate")
	}
	// The pseudo-filesystems must be unmounted even when a command fails.
	if umounted != 1 {
		t.Errorf("expected sysfs unmount on failure, got %d", umounted)
	}
}

var errCommandFailedForTest = &configTestError{"command failed"}

type configTestError struct{ msg string }

func (e *configTestError) Error() string { return e.msg }
