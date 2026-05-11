package shell_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

func TestGetFullCmdStr(t *testing.T) {
	cmd, err := shell.GetFullCmdStr("echo 'hello'", false, shell.HostPath, nil)
	if err != nil {
		t.Fatalf("GetFullCmdStr failed: %v", err)
	}
	if !(strings.Contains(cmd, "/usr/bin/echo 'hello'") || strings.Contains(cmd, "/bin/echo 'hello'")) {
		t.Errorf("Expected full path for echo, got: %s", cmd)
	}
}

func TestGetFullCmdStr_SedEcho(t *testing.T) {
	// Test sed command
	cmd := "sed -i 's/foo/bar/g' file.txt"
	fullCmd, err := shell.GetFullCmdStr(cmd, false, shell.HostPath, nil)
	if err != nil {
		t.Fatalf("GetFullCmdStr failed for sed: %v", err)
	}
	if !strings.Contains(fullCmd, "/usr/bin/sed") && !strings.Contains(fullCmd, "/bin/sed") {
		t.Errorf("Expected full path for sed, got: %s", fullCmd)
	}

	// Test echo command
	cmd = "echo 'hello world'"
	fullCmd, err = shell.GetFullCmdStr(cmd, false, shell.HostPath, nil)
	if err != nil {
		t.Fatalf("GetFullCmdStr failed for echo: %v", err)
	}
	if !strings.Contains(fullCmd, "/bin/echo") && !strings.Contains(fullCmd, "/usr/bin/echo") {
		t.Errorf("Expected full path for echo, got: %s", fullCmd)
	}
}

func TestExecCmd(t *testing.T) {
	out, err := shell.ExecCmd("echo 'test-exec-cmd'", false, shell.HostPath, nil)
	if err != nil {
		t.Fatalf("ExecCmd failed: %v", err)
	}
	if !strings.Contains(out, "test-exec-cmd") {
		t.Errorf("Expected output to contain 'test-exec-cmd', got: %s", out)
	}
}

func TestExecCmdWithStream(t *testing.T) {
	out, err := shell.ExecCmdWithStream("echo 'test-exec-stream'", false, shell.HostPath, nil)
	if err != nil {
		t.Fatalf("ExecCmdWithStream failed: %v", err)
	}
	if !strings.Contains(out, "test-exec-stream") {
		t.Errorf("Expected output to contain 'test-exec-stream', got: %s", out)
	}
}

func TestExecCmdWithInput(t *testing.T) {
	out, err := shell.ExecCmdWithInput("input-line", "cat", false, shell.HostPath, nil)
	if err != nil {
		t.Fatalf("ExecCmdWithInput failed: %v", err)
	}
	if !strings.Contains(out, "input-line") {
		t.Errorf("Expected output to contain 'input-line', got: %s", out)
	}
}

func TestExecCmdOverride(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "echo 'test-exec-cmd-override'", Output: "override-test\n", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)
	out, err := shell.ExecCmd("echo 'test-exec-cmd-override'", true, shell.HostPath, nil)
	if err != nil {
		t.Fatalf("ExecCmd with override failed: %v", err)
	}
	if !strings.Contains(out, "override-test") {
		t.Errorf("Expected output to contain 'override-test', got: %s", out)
	}
}

func TestExecCmdSilentOverride(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "echo 'test-exec-cmd-override'", Output: "override-test\n", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)
	out, err := shell.ExecCmdSilent("echo 'test-exec-cmd-override'", false, shell.HostPath, nil)
	if err != nil {
		t.Fatalf("ExecCmd with silent override failed: %v", err)
	}
	if !strings.Contains(out, "override-test") {
		t.Errorf("Expected output to contain 'override-test', got: %s", out)
	}
}

func TestExecCmdWithStreamOverride(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "echo 'test-exec-cmd-override'", Output: "override-test\n", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)
	out, err := shell.ExecCmdWithStream("echo 'test-exec-cmd-override'", true, shell.HostPath, nil)
	if err != nil {
		t.Fatalf("ExecCmdWithStream with override failed: %v", err)
	}
	if !strings.Contains(out, "override-test") {
		t.Errorf("Expected output to contain 'override-test', got: %s", out)
	}
}

func TestExecCmdWithInputOverride(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "echo 'test-exec-cmd-override'", Output: "override-test\n", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)
	out, err := shell.ExecCmdWithInput("input-line", "echo 'test-exec-cmd-override'", true, shell.HostPath, nil)
	if err != nil {
		t.Fatalf("ExecCmdWithInput with override failed: %v", err)
	}
	if !strings.Contains(out, "override-test") {
		t.Errorf("Expected output to contain 'override-test', got: %s", out)
	}
}

func TestGetOSEnvirons(t *testing.T) {
	env := shell.GetOSEnvirons()
	if len(env) == 0 {
		t.Log("Warning: No environment variables found")
	}
}

func TestGetOSProxyEnvirons(t *testing.T) {
	// Set some proxy env vars
	os.Setenv("http_proxy", "http://proxy.example.com:8080")
	os.Setenv("https_proxy", "http://proxy.example.com:8443")
	defer os.Unsetenv("http_proxy")
	defer os.Unsetenv("https_proxy")

	env := shell.GetOSProxyEnvirons()
	if val, ok := env["http_proxy"]; !ok || val != "http://proxy.example.com:8080" {
		t.Errorf("Expected http_proxy to be set, got %v", val)
	}
	if val, ok := env["https_proxy"]; !ok || val != "http://proxy.example.com:8443" {
		t.Errorf("Expected https_proxy to be set, got %v", val)
	}
}

func TestIsBashAvailable(t *testing.T) {
	// This depends on the system having bash.
	// Assuming the environment has bash.
	if !shell.IsBashAvailable(shell.HostPath) {
		t.Log("Bash not found in HostPath, skipping assertion")
	}
}

func TestIsCommandExist(t *testing.T) {
	exists, err := shell.IsCommandExist("ls", shell.HostPath)
	if err != nil {
		t.Fatalf("IsCommandExist failed: %v", err)
	}
	if !exists {
		t.Errorf("Expected 'ls' to exist")
	}

	exists, err = shell.IsCommandExist("nonexistentcommand12345", shell.HostPath)
	if err != nil {
		t.Fatalf("IsCommandExist failed: %v", err)
	}
	if exists {
		t.Errorf("Expected 'nonexistentcommand12345' to not exist")
	}
}

func TestExecCmdSilent(t *testing.T) {
	out, err := shell.ExecCmdSilent("echo 'silent'", false, shell.HostPath, nil)
	if err != nil {
		t.Fatalf("ExecCmdSilent failed: %v", err)
	}
	if !strings.Contains(out, "silent") {
		t.Errorf("Expected output to contain 'silent', got: %s", out)
	}
}

func TestGetFullCmdStr_Complex(t *testing.T) {
	// Test command with pipes
	cmd := "ls | grep 'test'"
	fullCmd, err := shell.GetFullCmdStr(cmd, false, shell.HostPath, nil)
	if err != nil {
		t.Fatalf("GetFullCmdStr failed for pipe: %v", err)
	}
	if !strings.Contains(fullCmd, "/bin/ls") && !strings.Contains(fullCmd, "/usr/bin/ls") {
		t.Errorf("Expected full path for ls, got: %s", fullCmd)
	}
	if !strings.Contains(fullCmd, "/usr/bin/grep") && !strings.Contains(fullCmd, "/bin/grep") {
		t.Errorf("Expected full path for grep, got: %s", fullCmd)
	}

	// Test command with &&
	cmd = "mkdir test && cd test"
	fullCmd, err = shell.GetFullCmdStr(cmd, false, shell.HostPath, nil)
	if err != nil {
		t.Fatalf("GetFullCmdStr failed for &&: %v", err)
	}
	if !strings.Contains(fullCmd, "/bin/mkdir") {
		t.Errorf("Expected full path for mkdir, got: %s", fullCmd)
	}
	// cd is a builtin, so it should remain as is
	if !strings.Contains(fullCmd, "cd test") {
		t.Errorf("Expected cd test, got: %s", fullCmd)
	}
}

func TestGetFullCmdStr_SedComplex(t *testing.T) {
	// Test sed with double quotes
	cmd := `sed -i "s/foo/bar/g" file.txt`
	fullCmd, err := shell.GetFullCmdStr(cmd, false, shell.HostPath, nil)
	if err != nil {
		t.Fatalf("GetFullCmdStr failed for sed double quotes: %v", err)
	}
	if !strings.Contains(fullCmd, "/usr/bin/sed") && !strings.Contains(fullCmd, "/bin/sed") {
		t.Errorf("Expected full path for sed, got: %s", fullCmd)
	}

	// Test sed with different delimiter
	cmd = "sed -i 's|foo|bar|g' file.txt"
	fullCmd, err = shell.GetFullCmdStr(cmd, false, shell.HostPath, nil)
	if err != nil {
		t.Fatalf("GetFullCmdStr failed for sed pipe delimiter: %v", err)
	}
	if !strings.Contains(fullCmd, "/usr/bin/sed") && !strings.Contains(fullCmd, "/bin/sed") {
		t.Errorf("Expected full path for sed, got: %s", fullCmd)
	}
}

func TestGetFullCmdStr_Chroot(t *testing.T) {
	// Create a fake chroot directory
	tempDir := t.TempDir()

	// Create bash in the fake chroot so IsBashAvailable works if called,
	// and verifyCmdWithFullPath checks for existence in chroot.
	// We need to create the directory structure for the command map.
	// e.g. /bin/ls
	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("Failed to create bin dir: %v", err)
	}
	lsPath := filepath.Join(binDir, "ls")
	if err := os.WriteFile(lsPath, []byte("fake ls"), 0755); err != nil {
		t.Fatalf("Failed to create fake ls: %v", err)
	}

	cmd := "ls"
	fullCmd, err := shell.GetFullCmdStr(cmd, false, tempDir, nil)
	if err != nil {
		t.Fatalf("GetFullCmdStr failed with chroot: %v", err)
	}

	// In chroot, it should use the path relative to chroot, but the command string returned
	// by GetFullCmdStr includes "sudo chroot <path> <cmd>".
	// The internal verification checks if the file exists in the chroot.
	// Since we created /bin/ls in the tempDir, verifyCmdWithFullPath should find it as /bin/ls.

	expectedSubStr := "chroot " + tempDir + " /bin/ls"
	if !strings.Contains(fullCmd, expectedSubStr) {
		t.Errorf("Expected command to contain '%s', got: %s", expectedSubStr, fullCmd)
	}
}

func TestIsBashAvailable_Chroot(t *testing.T) {
	tempDir := t.TempDir()

	// Initially bash should not be available
	if shell.IsBashAvailable(tempDir) {
		t.Errorf("Expected bash to not be available in empty chroot")
	}

	// Create bash
	usrBinDir := filepath.Join(tempDir, "usr", "bin")
	if err := os.MkdirAll(usrBinDir, 0755); err != nil {
		t.Fatalf("Failed to create usr/bin: %v", err)
	}
	bashPath := filepath.Join(usrBinDir, "bash")
	if err := os.WriteFile(bashPath, []byte("fake bash"), 0755); err != nil {
		t.Fatalf("Failed to create fake bash: %v", err)
	}

	if !shell.IsBashAvailable(tempDir) {
		t.Errorf("Expected bash to be available after creating it")
	}
}

func TestExecCmd_Error(t *testing.T) {
	// Test executing a non-existent command
	_, err := shell.ExecCmd("nonexistentcommand123", false, shell.HostPath, nil)
	if err == nil {
		t.Errorf("Expected error for nonexistent command, got nil")
	}
}

func TestExecCmdWithStream_Error(t *testing.T) {
	// Test executing a command that fails
	_, err := shell.ExecCmdWithStream("false", false, shell.HostPath, nil)
	if err == nil {
		t.Errorf("Expected error for failing command, got nil")
	}
}

func TestGetFullCmdStr_UnknownCommand(t *testing.T) {
	// Test a command that is not in the commandMap
	// "unknowncmd" is not in the map
	// verifyCmdWithFullPath should return error
	_, err := shell.GetFullCmdStr("unknowncmd", false, shell.HostPath, nil)
	if err == nil {
		t.Errorf("Expected error for unknown command, got nil")
	}
}

func TestGetFullCmdStr_CommandNotFoundInChroot(t *testing.T) {
	tempDir := t.TempDir()
	// ls is in commandMap but not in our empty chroot
	_, err := shell.GetFullCmdStr("ls", false, tempDir, nil)
	if err == nil {
		t.Errorf("Expected error for command not found in chroot, got nil")
	}
}

func TestGetFullCmdStr_Sudo(t *testing.T) {
	cmd := "ls"
	fullCmd, err := shell.GetFullCmdStr(cmd, true, shell.HostPath, nil)
	if err != nil {
		t.Fatalf("GetFullCmdStr failed with sudo: %v", err)
	}
	if !strings.Contains(fullCmd, "sudo") {
		t.Errorf("Expected sudo in command, got: %s", fullCmd)
	}
}

func TestGetFullCmdStr_Env(t *testing.T) {
	cmd := "ls"
	env := []string{"VAR=VALUE"}
	fullCmd, err := shell.GetFullCmdStr(cmd, true, shell.HostPath, env)
	if err != nil {
		t.Fatalf("GetFullCmdStr failed with env: %v", err)
	}
	// The env vars are added after sudo
	if !strings.Contains(fullCmd, "VAR=VALUE") {
		t.Errorf("Expected env var in command, got: %s", fullCmd)
	}
}
