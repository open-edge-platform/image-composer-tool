package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withEuid swaps the package-level geteuid seam for the duration of a test so
// the root / non-root branches of sudoPrefix can be exercised without actually
// changing the process's privileges.
func withEuid(t *testing.T, uid int) {
	t.Helper()
	orig := geteuid
	geteuid = func() int { return uid }
	t.Cleanup(func() { geteuid = orig })
}

func TestSudoPrefix_SuppressedWhenRoot(t *testing.T) {
	withEuid(t, 0)
	if got := sudoPrefix(); got != "" {
		t.Errorf("sudoPrefix() as root = %q, want empty (inner sudo is redundant)", got)
	}
}

func TestSudoPrefix_KeptWhenNonRoot(t *testing.T) {
	withEuid(t, 1000)
	if got := sudoPrefix(); got != "sudo " {
		t.Errorf("sudoPrefix() as non-root = %q, want %q", got, "sudo ")
	}
}

// TestGetFullCmdStr_HostSudoSuppressedWhenRoot verifies the host (non-chroot)
// branch drops the inner sudo when already root but still emits the full binary
// path and env vars.
func TestGetFullCmdStr_HostSudoSuppressedWhenRoot(t *testing.T) {
	withEuid(t, 0)
	got, err := GetFullCmdStr("ls", true, HostPath, []string{"VAR=VALUE"})
	if err != nil {
		t.Fatalf("GetFullCmdStr: %v", err)
	}
	if strings.HasPrefix(got, "sudo ") {
		t.Errorf("expected no leading sudo when root, got: %s", got)
	}
	if !strings.Contains(got, "VAR=VALUE") {
		t.Errorf("expected env var preserved, got: %s", got)
	}
}

func TestGetFullCmdStr_HostSudoKeptWhenNonRoot(t *testing.T) {
	withEuid(t, 1000)
	got, err := GetFullCmdStr("ls", true, HostPath, nil)
	if err != nil {
		t.Fatalf("GetFullCmdStr: %v", err)
	}
	if !strings.HasPrefix(got, "sudo ") {
		t.Errorf("expected leading sudo when non-root, got: %s", got)
	}
}

// TestGetFullCmdStr_ChrootSudoSuppressedWhenRoot verifies the chroot branch
// drops the inner sudo when root while still emitting `chroot <path> <cmd>`.
func TestGetFullCmdStr_ChrootSudoSuppressedWhenRoot(t *testing.T) {
	withEuid(t, 0)
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "ls"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := GetFullCmdStr("ls", false, tempDir, nil)
	if err != nil {
		t.Fatalf("GetFullCmdStr: %v", err)
	}
	if strings.HasPrefix(got, "sudo ") {
		t.Errorf("expected no leading sudo in chroot cmd when root, got: %s", got)
	}
	if !strings.Contains(got, "chroot "+tempDir+" /bin/ls") {
		t.Errorf("expected chroot invocation preserved, got: %s", got)
	}
}

func TestGetFullCmdStr_ChrootSudoKeptWhenNonRoot(t *testing.T) {
	withEuid(t, 1000)
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "ls"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := GetFullCmdStr("ls", false, tempDir, nil)
	if err != nil {
		t.Fatalf("GetFullCmdStr: %v", err)
	}
	// Proxy env vars may be interpolated between `sudo ` and `chroot ` (see
	// GetOSProxyEnvirons), so assert the leading sudo and the chroot invocation
	// separately rather than requiring them to be adjacent.
	if !strings.HasPrefix(got, "sudo ") {
		t.Errorf("expected leading sudo when non-root, got: %s", got)
	}
	if !strings.Contains(got, "chroot "+tempDir+" /bin/ls") {
		t.Errorf("expected chroot invocation preserved, got: %s", got)
	}
}
