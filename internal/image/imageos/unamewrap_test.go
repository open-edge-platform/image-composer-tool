package imageos

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

func useMockShell(t *testing.T, cmds []shell.MockCommand) {
	t.Helper()
	orig := shell.Default
	t.Cleanup(func() { shell.Default = orig })
	shell.Default = shell.NewMockExecutor(cmds)
}

func TestInstallTargetUnameWrapperLifecycle(t *testing.T) {
	root := t.TempDir()
	wrapper := filepath.Join(root, "usr", "bin", "uname")
	if err := os.MkdirAll(filepath.Dir(wrapper), 0755); err != nil {
		t.Fatal(err)
	}
	useMockShell(t, []shell.MockCommand{
		{Pattern: `dpkg-divert --local --rename --divert /usr/bin/uname\.oic-diverted --add /usr/bin/uname`},
		{Pattern: `dpkg-divert --local --rename --remove /usr/bin/uname`},
	})

	restore, err := installTargetUnameWrapper(root)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	info, err := os.Stat(wrapper)
	if err != nil {
		t.Fatalf("wrapper not created: %v", err)
	}
	if info.Mode().Perm()&0111 == 0 {
		t.Errorf("wrapper not executable: %v", info.Mode())
	}

	if err := restore(); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if _, err := os.Stat(wrapper); !os.IsNotExist(err) {
		t.Errorf("wrapper still present after restore: %v", err)
	}
}

func TestInstallTargetUnameWrapperSkippedWhenDiversionFails(t *testing.T) {
	root := t.TempDir()
	wrapper := filepath.Join(root, "usr", "bin", "uname")
	if err := os.MkdirAll(filepath.Dir(wrapper), 0755); err != nil {
		t.Fatal(err)
	}
	realUname := []byte("#!/bin/sh\n# real uname\n")
	if err := os.WriteFile(wrapper, realUname, 0755); err != nil {
		t.Fatal(err)
	}
	useMockShell(t, []shell.MockCommand{
		{Pattern: `dpkg-divert .*`, Error: errFake},
	})

	restore, err := installTargetUnameWrapper(root)
	if err != nil {
		t.Fatalf("a failed diversion must not fail the build: %v", err)
	}
	if got, err := os.ReadFile(wrapper); err != nil || string(got) != string(realUname) {
		t.Errorf("real uname changed despite failed diversion: %q, %v", got, err)
	}
	if err := restore(); err != nil {
		t.Errorf("restore after skipped install must be a no-op: %v", err)
	}
}

// diverterFake imitates dpkg-divert on a temp root: --add moves uname aside
// (and can then report failure, as a cancelled invocation would), --remove
// moves it back when removeFails is false.
type diverterFake struct {
	shell.Executor
	root        string
	addErr      error
	removeFails bool
	removeCalls int
}

func (d *diverterFake) ExecCmd(cmd string, _ bool, _ string, _ []string) (string, error) {
	uname := filepath.Join(d.root, targetUnamePath)
	real := filepath.Join(d.root, targetUnameRealPath)
	switch {
	case strings.Contains(cmd, "--add"):
		if err := os.Rename(uname, real); err != nil {
			return "", err
		}
		return "", d.addErr
	case strings.Contains(cmd, "--remove"):
		d.removeCalls++
		if d.removeFails {
			return "", errFake
		}
		if _, err := os.Stat(real); err == nil {
			return "", os.Rename(real, uname)
		}
	}
	return "", nil
}

// dpkg-divert can move uname aside and still report failure (for example when
// the build is cancelled). The diversion must then be undone, and an error
// returned if the real uname cannot be confirmed back in place.
func TestInstallTargetUnameWrapperDiversionFailsAfterMoving(t *testing.T) {
	tests := []struct {
		name        string
		removeFails bool
		wantErr     bool
		wantUname   bool
	}{
		{"diversion undone", false, false, true},
		{"undo fails", true, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			uname := filepath.Join(root, "usr", "bin", "uname")
			if err := os.MkdirAll(filepath.Dir(uname), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(uname, []byte("real"), 0755); err != nil {
				t.Fatal(err)
			}
			fake := &diverterFake{root: root, addErr: errFake, removeFails: tt.removeFails}
			orig := shell.Default
			t.Cleanup(func() { shell.Default = orig })
			shell.Default = fake

			restore, err := installTargetUnameWrapper(root)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if fake.removeCalls == 0 {
				t.Error("the diversion was not undone after the failed add")
			}
			_, statErr := os.Stat(uname)
			if (statErr == nil) != tt.wantUname {
				t.Errorf("uname present = %v, want %v", statErr == nil, tt.wantUname)
			}
			if err := restore(); err != nil {
				t.Errorf("restore after a failed install must be a no-op: %v", err)
			}
		})
	}
}

// A wrapper that cannot be written is rolled back (diversion removed) and the
// build carries on; if that rollback fails, the build must fail.
func TestInstallTargetUnameWrapperWriteFailure(t *testing.T) {
	tests := []struct {
		name      string
		removeErr error
		wantErr   bool
	}{
		{"rollback succeeds", nil, false},
		{"rollback fails", errFake, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// No usr/bin under the root, so writing the wrapper fails.
			root := t.TempDir()
			useMockShell(t, []shell.MockCommand{
				{Pattern: `dpkg-divert --local --rename --divert /usr/bin/uname\.oic-diverted --add /usr/bin/uname`},
				{Pattern: `dpkg-divert --local --rename --remove /usr/bin/uname`, Error: tt.removeErr},
			})
			restore, err := installTargetUnameWrapper(root)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err := restore(); err != nil {
				t.Errorf("restore after a failed install must be a no-op: %v", err)
			}
		})
	}
}

// TestTargetUnameWrapperScript runs the script with /boot pointed at a temp
// dir (the script reads the absolute /boot, which is the chroot's /boot in use).
func TestTargetUnameWrapperScript(t *testing.T) {
	if _, err := os.Stat("/usr/bin/uname"); err != nil {
		t.Skip("/usr/bin/uname not available")
	}

	tests := []struct {
		name    string
		kernels []string // /boot/vmlinuz-<v>
		modules []string // /lib/modules/<v>/vmlinuz
		headers []string // /lib/modules/<v> without a vmlinuz (headers only)
		args    []string
		want    string // exact output, or "" to expect the real uname's output
	}{
		{"newest kernel for -r", []string{"6.8.0-1", "7.0.0-42", "7.0.0-9"}, nil, nil, []string{"-r"}, "7.0.0-42"},
		{"long flag", []string{"7.0.0-42"}, nil, nil, []string{"--kernel-release"}, "7.0.0-42"},
		{"no kernel falls back to real uname", nil, nil, nil, []string{"-r"}, ""},
		{"other flags fall back to real uname", []string{"7.0.0-42"}, nil, nil, []string{"-m"}, ""},
		{"kernel only under lib/modules with empty boot", nil, []string{"7.0.0-42"}, nil, []string{"-r"}, "7.0.0-42"},
		{"newest across boot and lib/modules", []string{"6.8.0-1"}, []string{"7.0.0-42"}, nil, []string{"-r"}, "7.0.0-42"},
		{"headers-only lib/modules dir is not a kernel", []string{"6.8.0-1"}, nil, []string{"9.9.9-1"}, []string{"-r"}, "6.8.0-1"},
		{"only headers falls back to real uname", nil, nil, []string{"9.9.9-1"}, []string{"-r"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			boot := filepath.Join(dir, "boot")
			if err := os.MkdirAll(boot, 0755); err != nil {
				t.Fatal(err)
			}
			for _, k := range tt.kernels {
				if err := os.WriteFile(filepath.Join(boot, "vmlinuz-"+k), nil, 0644); err != nil {
					t.Fatal(err)
				}
			}
			modules := filepath.Join(dir, "modules")
			if err := os.MkdirAll(modules, 0755); err != nil {
				t.Fatal(err)
			}
			for _, k := range tt.modules {
				if err := os.MkdirAll(filepath.Join(modules, k), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(modules, k, "vmlinuz"), nil, 0644); err != nil {
					t.Fatal(err)
				}
			}
			for _, k := range tt.headers {
				if err := os.MkdirAll(filepath.Join(modules, k, "build"), 0755); err != nil {
					t.Fatal(err)
				}
			}
			// Non-versioned entries must be ignored.
			if err := os.WriteFile(filepath.Join(boot, "vmlinuz.old"), nil, 0644); err != nil {
				t.Fatal(err)
			}

			script := strings.ReplaceAll(targetUnameWrapperScript, "ls -1 /boot", "ls -1 "+boot)
			script = strings.ReplaceAll(script, "/lib/modules/", modules+"/")
			script = strings.ReplaceAll(script, targetUnameRealPath, "/usr/bin/uname")
			scriptPath := filepath.Join(dir, "uname")
			if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
				t.Fatal(err)
			}

			got, err := exec.Command(scriptPath, tt.args...).Output()
			if err != nil {
				t.Fatalf("run wrapper: %v", err)
			}
			want := tt.want
			if want == "" {
				real, err := exec.Command("/usr/bin/uname", tt.args...).Output()
				if err != nil {
					t.Fatal(err)
				}
				want = strings.TrimSpace(string(real))
			}
			if strings.TrimSpace(string(got)) != want {
				t.Errorf("got %q, want %q", strings.TrimSpace(string(got)), want)
			}
		})
	}
}

// A uname restoration that fails after the wrapper was installed must fail the
// build, since the image would ship the wrapper and have no real uname. When a
// package install failed too, both errors must be reported.
func TestInstallImagePkgsUnameRestoreFailure(t *testing.T) {
	installErr := errors.New("apt install boom")
	tests := []struct {
		name           string
		aptErr         error
		wantInstallErr bool
	}{
		{"package install succeeds", nil, false},
		{"package install fails", installErr, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useMockShell(t, []shell.MockCommand{
				{Pattern: `dpkg-divert --local --rename --remove /usr/bin/uname`, Error: errFake},
				{Pattern: `.*`},
			})

			testDir := t.TempDir()
			installRoot := filepath.Join(testDir, "install-root")
			for _, dir := range []string{
				filepath.Join(installRoot, "etc", "apt", "sources.list.d"),
				filepath.Join(installRoot, "usr", "bin"),
				filepath.Join(testDir, "config", "chrootenvconfigs"),
			} {
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
			}
			localList := filepath.Join(testDir, "config", "chrootenvconfigs", "local.list")
			if err := os.WriteFile(localList, []byte("deb file:///repo bookworm main\n"), 0644); err != nil {
				t.Fatal(err)
			}

			template := createTestImageTemplate()
			template.Target.OS = "ubuntu"
			template.SystemConfig.Packages = []string{"curl"}

			imageOs := &ImageOs{
				installRoot: installRoot,
				template:    template,
				chrootEnv: &cleanupCountMockChrootEnv{
					MockChrootEnv:   MockChrootEnv{pkgType: "deb", chrootPath: installRoot},
					targetConfigDir: filepath.Join(testDir, "config"),
					cacheDir:        filepath.Join(testDir, "cache"),
					aptInstallErr:   tt.aptErr,
				},
			}

			err := imageOs.installImagePkgs(installRoot, template)
			if err == nil {
				t.Fatal("expected an error: a failed uname restore must not yield a successful build")
			}
			if !errors.Is(err, errFake) && !strings.Contains(err.Error(), errFake.Error()) {
				t.Errorf("error %q does not report the uname restore failure", err)
			}
			if tt.wantInstallErr {
				if !errors.Is(err, installErr) {
					t.Errorf("error %q lost the package install failure", err)
				}
			} else if strings.Contains(err.Error(), installErr.Error()) {
				t.Errorf("unexpected package install failure in %q", err)
			}
		})
	}
}
