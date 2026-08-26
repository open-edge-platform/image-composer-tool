package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/spf13/cobra"
)

// resetResolveFlags clears package-level flag state between resolve tests so that
// each test observes the flag defaults.
func resetResolveFlags() {
	resolveFull = false
}

// writeResolveTemplate writes a minimal valid user template. When extends is
// non-empty it is emitted as the extends field; when password is non-empty a
// single user with that password is added; when secureBootDBKey is non-empty an
// immutability block referencing it is added. These knobs let each test exercise
// exactly one aspect of resolve.
func writeResolveTemplate(t *testing.T, path, imageName, extends, password, secureBootDBKey string) {
	t.Helper()

	var b strings.Builder
	if extends != "" {
		b.WriteString("extends: \"" + extends + "\"\n")
	}
	b.WriteString("image:\n")
	b.WriteString("  name: " + imageName + "\n")
	b.WriteString("  version: \"1.0.0\"\n")
	b.WriteString("target:\n")
	b.WriteString("  os: azure-linux\n")
	b.WriteString("  dist: azl3\n")
	b.WriteString("  arch: x86_64\n")
	b.WriteString("  imageType: raw\n")
	b.WriteString("systemConfig:\n")
	b.WriteString("  name: " + imageName + "-config\n")
	if password != "" {
		b.WriteString("  users:\n")
		b.WriteString("    - name: testuser\n")
		b.WriteString("      password: \"" + password + "\"\n")
	}
	if secureBootDBKey != "" {
		b.WriteString("  immutability:\n")
		b.WriteString("    enabled: true\n")
		b.WriteString("    secureBootDBKey: \"" + secureBootDBKey + "\"\n")
		b.WriteString("    secureBootDBCrt: \"" + secureBootDBKey + ".crt\"\n")
		b.WriteString("    secureBootDBCer: \"" + secureBootDBKey + ".cer\"\n")
	}

	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		t.Fatalf("failed to write template %s: %v", path, err)
	}
}

// runResolveCommand executes the resolve command with the given args and returns
// stdout+stderr bytes plus the returned error. Callers do not need to worry
// about restoring resolve flags — the test invoking this must defer resetResolveFlags.
func runResolveCommand(t *testing.T, args []string) (string, error) {
	t.Helper()

	cmd := createResolveCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)

	err := cmd.Execute()
	return buf.String(), err
}

// runResolveCommandWithConfig is like runResolveCommand but re-applies the
// caller-supplied global config in a PersistentPreRunE hook so that any
// initConfig callback registered on cobra by an earlier test cannot clobber
// the config we want for the resolve run. Callers pass a snapshot of the
// intended config; the hook copies it into the singleton before RunE fires.
func runResolveCommandWithConfig(t *testing.T, args []string, cfg *config.GlobalConfig) (string, error) {
	t.Helper()

	cmd := createResolveCommand()
	// The pre-run hook is executed AFTER cobra's initializers (which include
	// any leftover initConfig from a sibling test), giving us the last word.
	cmd.PersistentPreRunE = func(_ *cobra.Command, _ []string) error {
		snapshot := *cfg
		config.SetGlobal(&snapshot)
		return nil
	}

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)

	err := cmd.Execute()
	return buf.String(), err
}

// TestCreateResolveCommand_Structure verifies the command's shape: use, flags,
// arg validator, and RunE presence. The template file is passed as a positional
// argument (matching the build/validate convention), so we assert Args rejects
// zero and two-plus positional arguments.
func TestCreateResolveCommand_Structure(t *testing.T) {
	cmd := createResolveCommand()

	if cmd.Use == "" || !strings.HasPrefix(cmd.Use, "resolve") {
		t.Errorf("expected Use to start with 'resolve', got %q", cmd.Use)
	}
	if !strings.Contains(cmd.Use, "TEMPLATE_FILE") {
		t.Errorf("expected Use to advertise the positional TEMPLATE_FILE arg, got %q", cmd.Use)
	}
	if cmd.Short == "" {
		t.Error("expected Short to be set")
	}
	if cmd.Long == "" {
		t.Error("expected Long to be set")
	}
	if cmd.RunE == nil {
		t.Error("expected RunE to be set")
	}
	if cmd.Args == nil {
		t.Fatal("expected Args validator to be set")
	}
	if err := cmd.Args(cmd, []string{}); err == nil {
		t.Error("expected Args to reject zero positional arguments")
	}
	if err := cmd.Args(cmd, []string{"a.yml", "b.yml"}); err == nil {
		t.Error("expected Args to reject two positional arguments")
	}
	if err := cmd.Args(cmd, []string{"a.yml"}); err != nil {
		t.Errorf("expected Args to accept one positional argument, got: %v", err)
	}

	// The --template flag was removed in favor of the positional argument.
	if templateFlag := cmd.Flags().Lookup("template"); templateFlag != nil {
		t.Error("did not expect --template flag; the template path is now positional")
	}

	fullFlag := cmd.Flags().Lookup("full")
	if fullFlag == nil {
		t.Fatal("expected --full flag to exist")
	}
	if fullFlag.DefValue != "false" {
		t.Errorf("expected --full default to be false, got %q", fullFlag.DefValue)
	}
}

// TestResolveCommand_NoExtends_PrintsMessage verifies that a template without
// an extends: field, resolved without --full, prints only the informational
// message and does not print any YAML content.
func TestResolveCommand_NoExtends_PrintsMessage(t *testing.T) {
	defer resetResolveFlags()

	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "leaf.yml")
	writeResolveTemplate(t, templatePath, "solo", "", "", "")

	out, err := runResolveCommand(t, []string{templatePath})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !strings.Contains(out, "No extends used in template, nothing to resolve") {
		t.Errorf("expected informational message in output, got: %q", out)
	}
	if strings.Contains(out, "image:") {
		t.Errorf("expected no YAML content in output, got: %q", out)
	}
}

// TestResolveCommand_WithExtends_PrintsMergedYAML verifies that a two-file
// extends chain resolves and prints the chain-merged YAML. The output must
// carry values from the child (which override the parent), must not carry the
// extends field (it is stripped by the merge), and must not carry any OS
// defaults such as disk configuration.
func TestResolveCommand_WithExtends_PrintsMergedYAML(t *testing.T) {
	defer resetResolveFlags()

	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "root.yml")
	leafPath := filepath.Join(tmpDir, "leaf.yml")
	writeResolveTemplate(t, rootPath, "parent-image", "", "", "")
	writeResolveTemplate(t, leafPath, "child-image", "root.yml", "", "")

	out, err := runResolveCommand(t, []string{leafPath})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !strings.Contains(out, "name: child-image") {
		t.Errorf("expected merged YAML to carry child image name, got: %q", out)
	}
	if strings.Contains(out, "extends:") {
		t.Errorf("expected merged YAML to strip extends field, got: %q", out)
	}
	if strings.Contains(out, "partitionTableType") {
		t.Errorf("expected merged YAML to omit OS-defaults fields, got: %q", out)
	}
	if strings.Contains(out, "No extends used") {
		t.Errorf("did not expect informational message when extends is used, got: %q", out)
	}
}

// TestResolveCommand_WithFull_MergesOSDefaults verifies that --full merges OS
// defaults on top of the user template, so the output carries fields that only
// exist in the default config (partition table, kernel package list, etc.).
// This test uses the real config/osv defaults shipped with the repo — the
// azure-linux/azl3 x86_64 raw default — which are compiled in reference form
// but must be reachable via config.Global().ConfigDir. We point it at the repo
// root's config/ directory so LoadDefaultConfig can find the file.
func TestResolveCommand_WithFull_MergesOSDefaults(t *testing.T) {
	defer resetResolveFlags()

	restore, cfg := pointGlobalConfigAtRepo(t)
	defer restore()

	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "leaf.yml")
	writeResolveTemplate(t, templatePath, "user-image", "", "", "")

	out, err := runResolveCommandWithConfig(t, []string{templatePath, "--full"}, cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// The user template sets image.name explicitly, so the child value wins.
	if !strings.Contains(out, "name: user-image") {
		t.Errorf("expected merged YAML to carry user image name, got: %q", out)
	}
	// The OS defaults for azl3 raw carry a partitionTableType field the user
	// template never sets. Its presence proves defaults were merged in.
	if !strings.Contains(out, "partitionTableType") {
		t.Errorf("expected merged YAML to include OS-defaults partition config, got: %q", out)
	}
	if strings.Contains(out, "No extends used") {
		t.Errorf("did not expect informational message with --full, got: %q", out)
	}
}

// TestResolveCommand_NoExtends_FullMergesDefaults verifies that --full applies
// OS defaults even when the template does not use extends. This is the
// documented behavior: --full means "show exactly what will be built" and is
// independent of extends.
func TestResolveCommand_NoExtends_FullMergesDefaults(t *testing.T) {
	defer resetResolveFlags()

	restore, cfg := pointGlobalConfigAtRepo(t)
	defer restore()

	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "leaf.yml")
	writeResolveTemplate(t, templatePath, "solo-full", "", "", "")

	out, err := runResolveCommandWithConfig(t, []string{templatePath, "--full"}, cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if strings.Contains(out, "No extends used") {
		t.Errorf("--full should suppress the 'nothing to resolve' short-circuit, got: %q", out)
	}
	if !strings.Contains(out, "partitionTableType") {
		t.Errorf("expected --full output to include OS defaults, got: %q", out)
	}
}

// TestResolveCommand_RedactsSensitive verifies that user passwords and secure
// boot key/cert/cer paths never appear as plaintext in the output; they must
// all be replaced with [REDACTED].
func TestResolveCommand_RedactsSensitive(t *testing.T) {
	defer resetResolveFlags()

	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "root.yml")
	leafPath := filepath.Join(tmpDir, "leaf.yml")
	// The leaf carries the sensitive fields; extends is needed so resolve
	// actually emits YAML rather than the short-circuit message.
	writeResolveTemplate(t, rootPath, "parent-image", "", "", "")
	writeResolveTemplate(t, leafPath, "child-image", "root.yml", "hunter2-secret", "/keys/private-db.key")

	out, err := runResolveCommand(t, []string{leafPath})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if strings.Contains(out, "hunter2-secret") {
		t.Errorf("plaintext password leaked into output: %q", out)
	}
	if strings.Contains(out, "/keys/private-db.key") {
		t.Errorf("secure boot key path leaked into output: %q", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Errorf("expected [REDACTED] marker in output, got: %q", out)
	}
}

// TestResolveCommand_MissingTemplateArg verifies that invoking resolve with no
// positional argument yields cobra's ExactArgs(1) error and does not attempt
// to load anything.
func TestResolveCommand_MissingTemplateArg(t *testing.T) {
	defer resetResolveFlags()

	_, err := runResolveCommand(t, []string{})
	if err == nil {
		t.Fatal("expected error when TEMPLATE_FILE is omitted, got none")
	}
	if !strings.Contains(err.Error(), "arg") {
		t.Errorf("expected cobra ExactArgs error mentioning 'arg', got: %v", err)
	}
}

// TestResolveCommand_MissingTemplateFile verifies that pointing resolve at a
// non-existent file produces a wrapped error identifying the resolution phase.
func TestResolveCommand_MissingTemplateFile(t *testing.T) {
	defer resetResolveFlags()

	tmpDir := t.TempDir()
	missing := filepath.Join(tmpDir, "does-not-exist.yml")

	_, err := runResolveCommand(t, []string{missing})
	if err == nil {
		t.Fatal("expected error for missing template file, got none")
	}
	if !strings.Contains(err.Error(), "resolving template") {
		t.Errorf("expected wrapped resolve error, got: %v", err)
	}
}

// TestResolveCommand_BrokenExtendsChain verifies that when the extends parent
// cannot be resolved, the resolve command surfaces the error and identifies
// the offending file in the chain.
func TestResolveCommand_BrokenExtendsChain(t *testing.T) {
	defer resetResolveFlags()

	tmpDir := t.TempDir()
	leafPath := filepath.Join(tmpDir, "leaf.yml")
	writeResolveTemplate(t, leafPath, "orphan", "missing-parent.yml", "", "")

	_, err := runResolveCommand(t, []string{leafPath})
	if err == nil {
		t.Fatal("expected error for broken extends chain, got none")
	}
	if !strings.Contains(err.Error(), "missing-parent.yml") {
		t.Errorf("expected error to identify the missing parent, got: %v", err)
	}
}

// pointGlobalConfigAtRepo builds a GlobalConfig whose ConfigDir is the repo's
// top-level config/ directory so that LoadDefaultConfig can locate the shipped
// per-OS default templates. It installs the config as the singleton and
// returns (a) a restore function that reverts to the previous global config
// and (b) the config itself, so callers can pass it to
// runResolveCommandWithConfig — which re-installs it inside PersistentPreRunE
// so a leftover cobra initializer from a sibling test cannot overwrite it.
//
// The repo root is derived from runtime.Caller (the path of this test file)
// rather than from os.Getwd, because sibling tests may os.Chdir into temp
// directories and leave the CWD elsewhere when they run before us.
func pointGlobalConfigAtRepo(t *testing.T) (func(), *config.GlobalConfig) {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to determine test file path")
	}
	// thisFile is <repo>/cmd/image-composer-tool/resolve_test.go
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))

	prev := *config.Global()
	cfg := config.DefaultGlobalConfig()
	cfg.ConfigDir = filepath.Join(repoRoot, "config")
	cfg.CacheDir = filepath.Join(t.TempDir(), "cache")
	cfg.WorkDir = filepath.Join(t.TempDir(), "workspace")
	cfg.TempDir = filepath.Join(t.TempDir(), "tmp")
	config.SetGlobal(cfg)

	return func() {
		config.SetGlobal(&prev)
	}, cfg
}
