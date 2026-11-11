package imageos

import (
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
	useMockShell(t, []shell.MockCommand{
		{Pattern: `dpkg-divert .*`, Error: errFake},
	})

	restore, err := installTargetUnameWrapper(root)
	if err != nil {
		t.Fatalf("a failed diversion must not fail the build: %v", err)
	}
	if _, err := os.Stat(wrapper); !os.IsNotExist(err) {
		t.Errorf("wrapper written despite failed diversion: %v", err)
	}
	if err := restore(); err != nil {
		t.Errorf("restore after skipped install must be a no-op: %v", err)
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
		kernels []string
		args    []string
		want    string // exact output, or "" to expect the real uname's output
	}{
		{"newest kernel for -r", []string{"6.8.0-1", "7.0.0-42", "7.0.0-9"}, []string{"-r"}, "7.0.0-42"},
		{"long flag", []string{"7.0.0-42"}, []string{"--kernel-release"}, "7.0.0-42"},
		{"no kernel falls back to real uname", nil, []string{"-r"}, ""},
		{"other flags fall back to real uname", []string{"7.0.0-42"}, []string{"-m"}, ""},
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
			// Non-versioned entries must be ignored.
			if err := os.WriteFile(filepath.Join(boot, "vmlinuz.old"), nil, 0644); err != nil {
				t.Fatal(err)
			}

			script := strings.ReplaceAll(targetUnameWrapperScript, "ls -1 /boot", "ls -1 "+boot)
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
