package overlay

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

// TestQueryRPMPackages_RealEmptyDB initializes a real (but empty) rpm database
// with the host rpm and confirms queryRPMPackages reads it through the
// sudo-routed allowlist and reports the empty-database guard. It needs root
// (rpm --root via sudo) and the rpm binary; it skips otherwise so the suite
// stays green on unprivileged dev machines and CI sandboxes.
func TestQueryRPMPackages_RealEmptyDB(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to run rpm --root via the sudo-routed shell allowlist")
	}
	// The read path routes rpm through shell.ExecCmd(..., sudo=true), which
	// unconditionally prefixes the command with sudo; require the sudo binary too
	// so minimal root-run containers that lack it skip rather than fail.
	for _, b := range []string{"rpm", "sudo"} {
		if ok, err := shell.IsCommandExist(b, shell.HostPath); err != nil || !ok {
			t.Skipf("%s not available on host", b)
		}
	}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "var", "lib", "rpm"), 0o755); err != nil {
		t.Fatalf("mkdir rpmdb: %v", err)
	}
	if _, err := shell.ExecCmd("rpm --root "+shell.QuoteArg(root)+" --initdb", true, shell.HostPath, nil); err != nil {
		t.Skipf("rpm --initdb unsupported in this environment: %v", err)
	}

	// A freshly initialized database has no installed packages, so the read
	// succeeds but the empty-DB guard must fire.
	if _, err := queryRPMPackages(root); err == nil {
		t.Error("expected error for an rpm database with no installed packages")
	}
}
