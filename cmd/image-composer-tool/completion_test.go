package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// helper to run the completion install command with given args
func runInstallCompletion(t *testing.T, args ...string) (string, error) {
	t.Helper()

	// Create root command
	root := &cobra.Command{Use: "image-composer-tool"}

	// Manually create and add completion command (since InitDefaultCompletionCmd doesn't actually add it immediately)
	completionCmd := &cobra.Command{
		Use:   "completion",
		Short: "Generate completion scripts",
	}
	completionCmd.AddCommand(createCompletionInstallCommand())
	root.AddCommand(completionCmd)

	root.SetArgs(append([]string{"completion", "install"}, args...))

	// Execute through cobra path so flag parsing is exercised
	_, err := root.ExecuteC()
	if err != nil {
		return "", err
	}
	return "", nil
}

func TestInstallCompletion_UnknownShellDetection(t *testing.T) {
	// Ensure environment would not auto-detect a supported shell
	t.Setenv("SHELL", "/bin/unknown-shell")
	t.Setenv("PSModulePath", "")

	// Create root command
	root := &cobra.Command{Use: "image-composer-tool"}

	// Manually create and add completion command
	completionCmd := &cobra.Command{
		Use:   "completion",
		Short: "Generate completion scripts",
	}
	completionCmd.AddCommand(createCompletionInstallCommand())
	root.AddCommand(completionCmd)

	root.SetArgs([]string{"completion", "install"})

	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error for unsupported shell detection, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported shell") && !strings.Contains(err.Error(), "could not detect shell") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInstallCompletion_ZshWritesToHome(t *testing.T) {
	// Use a temp HOME so we don't touch the real filesystem
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	// On some platforms os.UserHomeDir() consults additional vars; set both for safety
	t.Setenv("USERPROFILE", tmp)

	// Force overwrite just in case a prior run created a file
	errStr, err := runInstallCompletion(t, "--shell", "zsh", "--force")
	_ = errStr
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Validate the expected file path exists
	target := filepath.Join(tmp, ".zsh", "completion", "_image-composer-tool")
	if _, statErr := os.Stat(target); statErr != nil {
		t.Fatalf("expected completion file at %s, got stat error: %v", target, statErr)
	}
}

// findAnyFileUnder returns true if any file exists under root that satisfies match(name)
func findAnyFileUnder(root string, match func(string) bool) (bool, error) {
	found := false
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() && match(filepath.Base(path)) {
			found = true
			// No way to stop immediately other than returning a non-nil; just continue.
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.SkipDir) {
		return false, err
	}
	return found, nil
}

func runCompletionFor(t *testing.T, shell string) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp) // windows env used by os.UserHomeDir on some setups

	// Create root command
	root := &cobra.Command{Use: "image-composer-tool"}

	// Manually create and add completion command
	completionCmd := &cobra.Command{
		Use:   "completion",
		Short: "Generate completion scripts",
	}
	completionCmd.AddCommand(createCompletionInstallCommand())
	root.AddCommand(completionCmd)

	root.SetArgs([]string{"completion", "install", "--shell", shell, "--force"})

	if err := root.Execute(); err != nil {
		t.Fatalf("completion for %s failed: %v", shell, err)
	}

	// Be flexible: we accept any file whose base name indicates image-composer-tool completion.
	want := func(name string) bool {
		name = strings.ToLower(name)
		return strings.Contains(name, "image-composer-tool") &&
			(strings.HasSuffix(name, ".bash") ||
				strings.HasSuffix(name, ".fish") ||
				strings.HasSuffix(name, ".ps1") ||
				name == "_image-composer-tool" || // zsh
				name == "image-composer-tool") // some distros use no extension
	}
	ok, err := findAnyFileUnder(tmp, want)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if !ok {
		t.Fatalf("expected a completion file ...")
	}
}

func TestInstallCompletion_Bash(t *testing.T)       { runCompletionFor(t, "bash") }
func TestInstallCompletion_Fish(t *testing.T)       { runCompletionFor(t, "fish") }
func TestInstallCompletion_PowerShell(t *testing.T) { runCompletionFor(t, "powershell") }
