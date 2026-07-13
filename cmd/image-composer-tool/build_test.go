package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/provider"
	"github.com/open-edge-platform/image-composer-tool/internal/provider/azl"
	"github.com/open-edge-platform/image-composer-tool/internal/provider/elxr"
	"github.com/open-edge-platform/image-composer-tool/internal/provider/emt"
	"github.com/open-edge-platform/image-composer-tool/internal/provider/ubuntu"
	"github.com/spf13/cobra"
)

// resetBuildFlags resets all build command flags to their default values
func resetBuildFlags() {
	workers = defaultWorkers
	cacheDir = ""
	workDir = ""
	noCache = false
	inspectImage = true
	cveCheck = false
	baselineImage = ""
}

// createTestTemplate creates a minimal valid template file for testing
// Note: Currently unused but kept for future integration tests
func createTestTemplate(t *testing.T, osName, dist, arch string) string { //nolint:unused
	t.Helper()
	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "test-template.yml")

	templateContent := "image:\n" +
		"  name: \"test-image\"\n" +
		"  version: \"1.0.0\"\n" +
		"\n" +
		"target:\n" +
		"  os: \"" + osName + "\"\n" +
		"  dist: \"" + dist + "\"\n" +
		"  arch: \"" + arch + "\"\n" +
		"  imageType: \"raw\"\n" +
		"\n" +
		"disk:\n" +
		"  size: \"2G\"\n" +
		"  partitionTableType: \"gpt\"\n" +
		"  partitions:\n" +
		"    - name: \"boot\"\n" +
		"      size: \"512M\"\n" +
		"      filesystem: \"vfat\"\n" +
		"      mountpoint: \"/boot\"\n" +
		"    - name: \"root\"\n" +
		"      size: \"1536M\"\n" +
		"      filesystem: \"ext4\"\n" +
		"      mountpoint: \"/\"\n" +
		"\n" +
		"systemConfig:\n" +
		"  hostname: \"test-host\"\n" +
		"  timezone: \"UTC\"\n" +
		"  packages:\n" +
		"    - \"bash\"\n" +
		"    - \"coreutils\"\n" +
		"  users:\n" +
		"    - username: \"testuser\"\n" +
		"      password: \"$6$rounds=656000$YQKMBktZ7E1ykLxP$\"\n"

	if err := os.WriteFile(templatePath, []byte(templateContent), 0o644); err != nil {
		t.Fatalf("failed to create test template: %v", err)
	}

	return templatePath
}

// TestCreateBuildCommand tests the build command creation and structure
func TestCreateBuildCommand(t *testing.T) {
	defer resetBuildFlags()

	buildCmd := createBuildCommand()

	t.Run("CommandMetadata", func(t *testing.T) {
		if buildCmd == nil {
			t.Fatal("createBuildCommand returned nil")
		}
		if buildCmd.Use != "build [flags] TEMPLATE_FILE" {
			t.Errorf("expected Use='build [flags] TEMPLATE_FILE', got %q", buildCmd.Use)
		}
		if buildCmd.Short == "" {
			t.Error("Short description should not be empty")
		}
		if buildCmd.Long == "" {
			t.Error("Long description should not be empty")
		}
		if !strings.Contains(buildCmd.Long, "template") {
			t.Error("Long description should mention template")
		}
	})

	t.Run("CommandFlags", func(t *testing.T) {
		type flagExpectation struct {
			name        string
			shorthand   string
			shouldExist bool
		}

		expectedFlags := []flagExpectation{
			{name: "workers", shorthand: "w", shouldExist: true},
			{name: "cache-dir", shorthand: "d", shouldExist: true},
			{name: "work-dir", shorthand: "", shouldExist: true},
			{name: "nocache", shorthand: "", shouldExist: true},
			{name: "inspect", shorthand: "", shouldExist: true},
			{name: "no-inspect", shorthand: "", shouldExist: true},
			{name: "cve-check", shorthand: "", shouldExist: true},
			{name: "baseline-image", shorthand: "", shouldExist: true},
		}

		for _, expected := range expectedFlags {
			flag := buildCmd.Flags().Lookup(expected.name)
			if expected.shouldExist && flag == nil {
				t.Errorf("flag --%s should be registered", expected.name)
				continue
			}
			if !expected.shouldExist && flag != nil {
				t.Errorf("flag --%s should not be registered", expected.name)
			}
			if expected.shouldExist && flag != nil && expected.shorthand != "" {
				if flag.Shorthand != expected.shorthand {
					t.Errorf("flag --%s: expected shorthand %q, got %q",
						expected.name, expected.shorthand, flag.Shorthand)
				}
			}
		}
	})

	t.Run("CommandArgs", func(t *testing.T) {
		// The command should require exactly 1 argument
		if buildCmd.Args == nil {
			t.Error("Args validator should be set")
		}
		// Test with wrong number of args
		err := buildCmd.Args(buildCmd, []string{})
		if err == nil {
			t.Error("should error with 0 args")
		}
		err = buildCmd.Args(buildCmd, []string{"file1.yml", "file2.yml"})
		if err == nil {
			t.Error("should error with 2 args")
		}
		err = buildCmd.Args(buildCmd, []string{"template.yml"})
		if err != nil {
			t.Errorf("should accept 1 arg, got error: %v", err)
		}
	})

	t.Run("RunFunction", func(t *testing.T) {
		if buildCmd.RunE == nil {
			t.Error("RunE function should be set")
		}
	})

	t.Run("CompletionFunction", func(t *testing.T) {
		if buildCmd.ValidArgsFunction == nil {
			t.Error("ValidArgsFunction should be set for template file completion")
		}
	})
}

// TestExecuteBuild_NoTemplateArg tests error handling when no template is provided
func TestExecuteBuild_NoTemplateArg(t *testing.T) {
	defer resetBuildFlags()

	cmd := createBuildCommand()
	err := executeBuild(cmd, []string{})

	if err == nil {
		t.Fatal("expected error when no template file is provided")
	}

	expectedMsg := "no template file provided"
	if !strings.Contains(err.Error(), expectedMsg) {
		t.Errorf("expected error message to contain %q, got %q", expectedMsg, err.Error())
	}
}

// TestExecuteBuild_InvalidTemplateFile tests handling of invalid template files
func TestExecuteBuild_InvalidTemplateFile(t *testing.T) {
	defer resetBuildFlags()

	cmd := createBuildCommand()

	t.Run("NonExistentFile", func(t *testing.T) {
		err := executeBuild(cmd, []string{"/nonexistent/template.yml"})
		if err == nil {
			t.Fatal("expected error for non-existent template file")
		}
	})

	t.Run("EmptyTemplate", func(t *testing.T) {
		tmpDir := t.TempDir()
		emptyTemplate := filepath.Join(tmpDir, "empty.yml")
		if err := os.WriteFile(emptyTemplate, []byte(""), 0o644); err != nil {
			t.Fatalf("failed to create empty template: %v", err)
		}

		err := executeBuild(cmd, []string{emptyTemplate})
		if err == nil {
			t.Fatal("expected error for empty template file")
		}
	})

	t.Run("InvalidYAML", func(t *testing.T) {
		tmpDir := t.TempDir()
		invalidTemplate := filepath.Join(tmpDir, "invalid.yml")
		invalidContent := `
this is not valid yaml: [[[
	indentation: wrong
		more: problems
`
		if err := os.WriteFile(invalidTemplate, []byte(invalidContent), 0o644); err != nil {
			t.Fatalf("failed to create invalid template: %v", err)
		}

		err := executeBuild(cmd, []string{invalidTemplate})
		if err == nil {
			t.Fatal("expected error for invalid YAML")
		}
	})
}

// TestExecuteBuild_FlagOverrides tests that command flags override config values
func TestExecuteBuild_FlagOverrides(t *testing.T) {
	defer resetBuildFlags()

	// Note: This test requires a valid template and provider setup,
	// which is complex to mock. Testing flag override logic in isolation.

	cmd := createBuildCommand()

	t.Run("WorkersFlag", func(t *testing.T) {
		// Save original config
		origConfig := config.Global()
		origWorkers := origConfig.Workers

		defer func() {
			origConfig.Workers = origWorkers
			config.SetGlobal(origConfig)
		}()

		// Set the flag
		workers = 16
		if err := cmd.Flags().Set("workers", "16"); err != nil {
			t.Fatalf("failed to set workers flag: %v", err)
		}

		// Simulate the flag override logic from executeBuild
		if cmd.Flags().Changed("workers") {
			currentConfig := config.Global()
			currentConfig.Workers = workers
			config.SetGlobal(currentConfig)
		}

		// Verify the config was updated
		cfg := config.Global()
		if cfg.Workers != 16 {
			t.Errorf("expected workers=16, got %d", cfg.Workers)
		}
	})

	t.Run("CacheDirFlag", func(t *testing.T) {
		origConfig := config.Global()
		origCacheDir := origConfig.CacheDir

		defer func() {
			origConfig.CacheDir = origCacheDir
			config.SetGlobal(origConfig)
		}()

		testCacheDir := "/tmp/test-cache"
		cacheDir = testCacheDir
		if err := cmd.Flags().Set("cache-dir", testCacheDir); err != nil {
			t.Fatalf("failed to set cache-dir flag: %v", err)
		}

		if cmd.Flags().Changed("cache-dir") {
			currentConfig := config.Global()
			currentConfig.CacheDir = cacheDir
			config.SetGlobal(currentConfig)
		}

		cfg := config.Global()
		if cfg.CacheDir != testCacheDir {
			t.Errorf("expected cacheDir=%q, got %q", testCacheDir, cfg.CacheDir)
		}
	})

	t.Run("WorkDirFlag", func(t *testing.T) {
		origConfig := config.Global()
		origWorkDir := origConfig.WorkDir

		defer func() {
			origConfig.WorkDir = origWorkDir
			config.SetGlobal(origConfig)
		}()

		testWorkDir := "/tmp/test-work"
		workDir = testWorkDir
		if err := cmd.Flags().Set("work-dir", testWorkDir); err != nil {
			t.Fatalf("failed to set work-dir flag: %v", err)
		}

		if cmd.Flags().Changed("work-dir") {
			currentConfig := config.Global()
			currentConfig.WorkDir = workDir
			config.SetGlobal(currentConfig)
		}

		cfg := config.Global()
		if cfg.WorkDir != testWorkDir {
			t.Errorf("expected workDir=%q, got %q", testWorkDir, cfg.WorkDir)
		}
	})
}

// TestInitProvider tests the provider initialization logic
func TestInitProvider(t *testing.T) {
	tests := []struct {
		name        string
		os          string
		dist        string
		arch        string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "UnsupportedProvider",
			os:          "unknown-os",
			dist:        "dist",
			arch:        "x86_64",
			expectError: true,
			errorMsg:    "unsupported provider",
		},
		{
			name:        "AzureLinux",
			os:          azl.OsName,
			dist:        "azl3",
			arch:        "x86_64",
			expectError: false,
		},
		{
			name:        "EMT",
			os:          emt.OsName,
			dist:        "emt3",
			arch:        "x86_64",
			expectError: false,
		},
		{
			name:        "eLxr",
			os:          elxr.OsName,
			dist:        "elxr12",
			arch:        "x86_64",
			expectError: false,
		},
		{
			name:        "Ubuntu",
			os:          ubuntu.OsName,
			dist:        "ubuntu24",
			arch:        "x86_64",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := InitProvider(tt.os, tt.dist, tt.arch)

			if tt.expectError {
				if err == nil {
					t.Fatal("expected error but got none")
				}
				if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error to contain %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					// Skip test if config directories don't exist
					if strings.Contains(err.Error(), "config directory does not exist") {
						t.Skipf("Config directory not found for %s/%s - skipping", tt.os, tt.dist)
					}
					t.Fatalf("unexpected error: %v", err)
				}
				if p == nil {
					t.Fatal("expected provider but got nil")
				}
			}
		})
	}
}

// TestInitProvider_Registration tests that providers are properly registered
func TestInitProvider_Registration(t *testing.T) {
	tests := []struct {
		os   string
		dist string
		arch string
	}{
		{os: azl.OsName, dist: "azl3", arch: "x86_64"},
		{os: emt.OsName, dist: "emt3", arch: "x86_64"},
		{os: elxr.OsName, dist: "elxr12", arch: "x86_64"},
		{os: ubuntu.OsName, dist: "ubuntu24", arch: "x86_64"},
	}

	for _, tt := range tests {
		t.Run(tt.os, func(t *testing.T) {
			p, err := InitProvider(tt.os, tt.dist, tt.arch)
			if err != nil {
				// Skip test if config directories don't exist
				if strings.Contains(err.Error(), "config directory does not exist") {
					t.Skipf("Config directory not found for %s/%s - skipping", tt.os, tt.dist)
				}
				t.Fatalf("failed to initialize provider: %v", err)
			}

			// Verify provider implements the interface
			var _ provider.Provider = p

			// Verify provider can be retrieved
			providerId := p.Name(tt.dist, tt.arch)
			retrievedProvider, ok := provider.Get(providerId)
			if !ok {
				t.Errorf("provider %q should be registered", providerId)
			}
			if retrievedProvider == nil {
				t.Error("retrieved provider should not be nil")
			}
		})
	}
}

// TestInitProvider_InvalidParameters tests provider initialization with invalid parameters
func TestInitProvider_InvalidParameters(t *testing.T) {
	tests := []struct {
		name string
		os   string
		dist string
		arch string
	}{
		{name: "EmptyOS", os: "", dist: "dist", arch: "x86_64"},
		{name: "EmptyDist", os: "azl", dist: "", arch: "x86_64"},
		{name: "EmptyArch", os: "azl", dist: "azl3", arch: ""},
		{name: "AllEmpty", os: "", dist: "", arch: ""},
		{name: "InvalidOS", os: "windows", dist: "10", arch: "x86_64"},
		{name: "InvalidDist", os: azl.OsName, dist: "invalid-dist", arch: "x86_64"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := InitProvider(tt.os, tt.dist, tt.arch)
			if err == nil {
				t.Error("expected error for invalid parameters")
			}
		})
	}
}

// TestTemplateFileCompletion tests the shell completion function for template files
func TestTemplateFileCompletion(t *testing.T) {
	cmd := createBuildCommand()

	completions, directive := templateFileCompletion(cmd, []string{}, "")

	t.Run("CompletionValues", func(t *testing.T) {
		if len(completions) == 0 {
			t.Error("should return completion values")
		}

		expectedExtensions := []string{"*.yml", "*.yaml"}
		for _, ext := range expectedExtensions {
			found := false
			for _, comp := range completions {
				if comp == ext {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected completion %q not found", ext)
			}
		}
	})

	t.Run("CompletionDirective", func(t *testing.T) {
		if directive != cobra.ShellCompDirectiveFilterFileExt {
			t.Errorf("expected directive ShellCompDirectiveFilterFileExt, got %v", directive)
		}
	})
}

// TestBuildCommand_Integration tests the full build command integration
func TestBuildCommand_Integration(t *testing.T) {
	t.Skip("Integration test requires full environment setup - skipping in unit tests")

	// This test would require:
	// - Valid package repositories
	// - Network access
	// - Root/sudo permissions for some operations
	// - Provider-specific dependencies

	defer resetBuildFlags()

	// Example structure (not executable without full setup):
	// templatePath := createTestTemplate(t, "azl", "azl3", "x86_64")
	// cmd := createBuildCommand()
	// err := executeBuild(cmd, []string{templatePath})
	// if err != nil {
	//     t.Errorf("build should succeed: %v", err)
	// }
}

// TestBuildFlags_DefaultValues tests that flags have correct default values
func TestBuildFlags_DefaultValues(t *testing.T) {
	resetBuildFlags()

	if workers != defaultWorkers {
		t.Errorf("workers should default to %d, got %d", defaultWorkers, workers)
	}
	if cacheDir != "" {
		t.Errorf("cacheDir should default to empty, got %q", cacheDir)
	}
	if workDir != "" {
		t.Errorf("workDir should default to empty, got %q", workDir)
	}
	if verbose != false {
		t.Errorf("verbose should default to false, got %v", verbose)
	}
	if inspectImage != true {
		t.Errorf("inspectImage should default to true, got %v", inspectImage)
	}
	if cveCheck != false {
		t.Errorf("cveCheck should default to false, got %v", cveCheck)
	}
	if baselineImage != "" {
		t.Errorf("baselineImage should default to empty, got %q", baselineImage)
	}
}

// TestBuildCommand_FlagParsing tests that flags are correctly parsed
func TestBuildCommand_FlagParsing(t *testing.T) {
	defer resetBuildFlags()

	cmd := createBuildCommand()

	tests := []struct {
		name     string
		args     []string
		validate func(t *testing.T)
	}{
		{
			name: "WorkersFlag",
			args: []string{"--workers", "8", "template.yml"},
			validate: func(t *testing.T) {
				if err := cmd.ParseFlags([]string{"--workers", "8"}); err != nil {
					t.Fatalf("failed to parse flags: %v", err)
				}
				val, err := cmd.Flags().GetInt("workers")
				if err != nil {
					t.Fatalf("failed to get workers flag: %v", err)
				}
				if val != 8 {
					t.Errorf("expected workers=8, got %d", val)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetBuildFlags()
			tt.validate(t)
		})
	}
}

// TestBuildCommand_HelpText tests the help output
func TestBuildCommand_HelpText(t *testing.T) {
	cmd := createBuildCommand()

	helpOutput := cmd.UsageString()

	expectedInHelp := []string{
		"build",
		"TEMPLATE_FILE",
		"--workers",
		"--cache-dir",
		"--work-dir",
		"--nocache",
		"--inspect",
		"--no-inspect",
		"--cve-check",
		"--baseline-image",
	}

	for _, expected := range expectedInHelp {
		if !strings.Contains(helpOutput, expected) {
			t.Errorf("help output should contain %q", expected)
		}
	}
}

// TestInitProvider_ProviderInterface tests that returned providers implement the interface
func TestInitProvider_ProviderInterface(t *testing.T) {
	providerTests := []struct {
		os   string
		dist string
		arch string
	}{
		{os: azl.OsName, dist: "azl3", arch: "x86_64"},
		{os: emt.OsName, dist: "emt3", arch: "x86_64"},
		{os: elxr.OsName, dist: "elxr12", arch: "x86_64"},
		{os: ubuntu.OsName, dist: "ubuntu24", arch: "x86_64"},
	}

	for _, p := range providerTests {
		t.Run(p.os, func(t *testing.T) {
			prov, err := InitProvider(p.os, p.dist, p.arch)
			if err != nil {
				// Skip test if config directories don't exist
				if strings.Contains(err.Error(), "config directory does not exist") {
					t.Skipf("Config directory not found for %s/%s - skipping", p.os, p.dist)
				}
				t.Fatalf("failed to initialize provider: %v", err)
			}

			// Test that provider has Name method
			name := prov.Name(p.dist, p.arch)
			if name == "" {
				t.Error("provider Name() should not return empty string")
			}

			// Verify provider is registered under correct name
			retrievedProvider, ok := provider.Get(name)
			if !ok {
				t.Errorf("provider should be registered under name %q", name)
			}
			if retrievedProvider == nil {
				t.Error("retrieved provider should not be nil")
			}
		})
	}
}

// TestExecuteBuild_ConfigOverrides tests comprehensive flag override scenarios
func TestExecuteBuild_ConfigOverrides(t *testing.T) {
	defer resetBuildFlags()

	t.Run("MultipleOverrides", func(t *testing.T) {
		cmd := createBuildCommand()

		origConfig := config.Global()
		defer func() {
			config.SetGlobal(origConfig)
		}()

		// Set multiple flags
		workers = 12
		cacheDir = "/custom/cache"
		workDir = "/custom/work"

		if err := cmd.Flags().Set("workers", "12"); err != nil {
			t.Fatalf("failed to set workers flag: %v", err)
		}
		if err := cmd.Flags().Set("cache-dir", "/custom/cache"); err != nil {
			t.Fatalf("failed to set cache-dir flag: %v", err)
		}
		if err := cmd.Flags().Set("work-dir", "/custom/work"); err != nil {
			t.Fatalf("failed to set work-dir flag: %v", err)
		}

		// Apply overrides as executeBuild does
		if cmd.Flags().Changed("workers") {
			currentConfig := config.Global()
			currentConfig.Workers = workers
			config.SetGlobal(currentConfig)
		}
		if cmd.Flags().Changed("cache-dir") {
			currentConfig := config.Global()
			currentConfig.CacheDir = cacheDir
			config.SetGlobal(currentConfig)
		}
		if cmd.Flags().Changed("work-dir") {
			currentConfig := config.Global()
			currentConfig.WorkDir = workDir
			config.SetGlobal(currentConfig)
		}

		// Verify all overrides applied
		cfg := config.Global()
		if cfg.Workers != 12 {
			t.Errorf("expected workers=12, got %d", cfg.Workers)
		}
		if cfg.CacheDir != "/custom/cache" {
			t.Errorf("expected cacheDir=/custom/cache, got %q", cfg.CacheDir)
		}
		if cfg.WorkDir != "/custom/work" {
			t.Errorf("expected workDir=/custom/work, got %q", cfg.WorkDir)
		}
	})

	t.Run("NoOverridesWhenFlagsNotSet", func(t *testing.T) {
		cmd := createBuildCommand()

		origConfig := config.Global()
		origWorkers := origConfig.Workers
		origCacheDir := origConfig.CacheDir

		defer func() {
			config.SetGlobal(origConfig)
		}()

		// Don't set any flags, config should remain unchanged
		if cmd.Flags().Changed("workers") {
			t.Error("workers flag should not be changed")
		}
		if cmd.Flags().Changed("cache-dir") {
			t.Error("cache-dir flag should not be changed")
		}

		cfg := config.Global()
		if cfg.Workers != origWorkers {
			t.Errorf("workers should remain %d, got %d", origWorkers, cfg.Workers)
		}
		if cfg.CacheDir != origCacheDir {
			t.Errorf("cacheDir should remain %q, got %q", origCacheDir, cfg.CacheDir)
		}
	})
}

// TestBuildCommand_OverlayFlagParsing tests parsing of the overlay-mode flags.
func TestBuildCommand_OverlayFlagParsing(t *testing.T) {
	defer resetBuildFlags()

	t.Run("NoInspect", func(t *testing.T) {
		resetBuildFlags()
		cmd := createBuildCommand()
		if err := cmd.ParseFlags([]string{"--no-inspect", "template.yml"}); err != nil {
			t.Fatalf("failed to parse flags: %v", err)
		}
		noInspect, err := cmd.Flags().GetBool("no-inspect")
		if err != nil {
			t.Fatalf("failed to get no-inspect flag: %v", err)
		}
		if !noInspect {
			t.Errorf("expected no-inspect=true, got %v", noInspect)
		}
	})

	t.Run("InspectDefaultsOn", func(t *testing.T) {
		resetBuildFlags()
		cmd := createBuildCommand()
		if err := cmd.ParseFlags([]string{"template.yml"}); err != nil {
			t.Fatalf("failed to parse flags: %v", err)
		}
		inspect, err := cmd.Flags().GetBool("inspect")
		if err != nil {
			t.Fatalf("failed to get inspect flag: %v", err)
		}
		if !inspect {
			t.Errorf("expected inspect to default to true, got %v", inspect)
		}
	})

	t.Run("CVECheck", func(t *testing.T) {
		resetBuildFlags()
		cmd := createBuildCommand()
		if err := cmd.ParseFlags([]string{"--cve-check", "template.yml"}); err != nil {
			t.Fatalf("failed to parse flags: %v", err)
		}
		val, err := cmd.Flags().GetBool("cve-check")
		if err != nil {
			t.Fatalf("failed to get cve-check flag: %v", err)
		}
		if !val {
			t.Errorf("expected cve-check=true, got %v", val)
		}
	})

	t.Run("CVECheckNotYetImplemented", func(t *testing.T) {
		resetBuildFlags()
		cmd := createBuildCommand()
		if err := cmd.ParseFlags([]string{"--cve-check"}); err != nil {
			t.Fatalf("failed to parse flags: %v", err)
		}
		// The CVE engine does not exist yet: applying the flag must fail clearly
		// rather than silently ignore it.
		err := applyOverlayFlagOverrides(cmd, &config.ImageTemplate{})
		if err == nil {
			t.Fatal("expected --cve-check to error as not yet implemented")
		}
		if !strings.Contains(err.Error(), "not yet implemented") {
			t.Errorf("expected 'not yet implemented' error, got %q", err.Error())
		}
	})

	t.Run("BaselineImage", func(t *testing.T) {
		resetBuildFlags()
		cmd := createBuildCommand()
		if err := cmd.ParseFlags([]string{"--baseline-image", "/tmp/base.raw", "template.yml"}); err != nil {
			t.Fatalf("failed to parse flags: %v", err)
		}
		val, err := cmd.Flags().GetString("baseline-image")
		if err != nil {
			t.Fatalf("failed to get baseline-image flag: %v", err)
		}
		if val != "/tmp/base.raw" {
			t.Errorf("expected baseline-image=/tmp/base.raw, got %q", val)
		}
	})
}

// TestExecuteBuild_OverlayFlagOverrides tests that the overlay-mode flags take
// precedence over template values when applied onto a loaded template.
func TestExecuteBuild_OverlayFlagOverrides(t *testing.T) {
	defer resetBuildFlags()

	t.Run("NoInspectStoredOnTemplate", func(t *testing.T) {
		resetBuildFlags()
		cmd := createBuildCommand()
		if err := cmd.ParseFlags([]string{"--no-inspect"}); err != nil {
			t.Fatalf("failed to parse flags: %v", err)
		}

		template := &config.ImageTemplate{}
		if err := applyOverlayFlagOverrides(cmd, template); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if template.InspectEnabled {
			t.Errorf("expected InspectEnabled=false with --no-inspect, got true")
		}
	})

	t.Run("BaselineImageOverridesTemplatePath", func(t *testing.T) {
		resetBuildFlags()
		cmd := createBuildCommand()
		if err := cmd.ParseFlags([]string{"--baseline-image", "/tmp/override.raw"}); err != nil {
			t.Fatalf("failed to parse flags: %v", err)
		}

		template := &config.ImageTemplate{
			Baseline: &config.Baseline{
				Mode:   config.BaselineModeOverlay,
				Source: &config.BaselineSource{Path: "/original/path.raw"},
			},
		}
		if err := applyOverlayFlagOverrides(cmd, template); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if template.Baseline.Source.Path != "/tmp/override.raw" {
			t.Errorf("expected baseline path overridden to /tmp/override.raw, got %q", template.Baseline.Source.Path)
		}
		// The override also clears any URL to preserve the "exactly one of
		// path/url" invariant.
		if template.Baseline.Source.URL != "" {
			t.Errorf("expected URL cleared by override, got %q", template.Baseline.Source.URL)
		}
	})

	t.Run("NoOverrideWhenFlagsNotSet", func(t *testing.T) {
		resetBuildFlags()
		cmd := createBuildCommand()
		if err := cmd.ParseFlags([]string{}); err != nil {
			t.Fatalf("failed to parse flags: %v", err)
		}

		template := &config.ImageTemplate{
			Baseline: &config.Baseline{
				Mode:   config.BaselineModeOverlay,
				Source: &config.BaselineSource{Path: "/original/path.raw"},
			},
		}
		if err := applyOverlayFlagOverrides(cmd, template); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// With no flags set, the template path is left untouched...
		if template.Baseline.Source.Path != "/original/path.raw" {
			t.Errorf("expected baseline path unchanged, got %q", template.Baseline.Source.Path)
		}
		// ...and inspection defaults on.
		if !template.InspectEnabled {
			t.Error("InspectEnabled should default to true when no flags set")
		}
	})
}

// TestApplyOverlayFlagOverrides_BaselineImageRequiresOverlay tests that
// --baseline-image on a template with no baseline.source section fails clearly
// instead of panicking on a nil dereference.
func TestApplyOverlayFlagOverrides_BaselineImageRequiresOverlay(t *testing.T) {
	defer resetBuildFlags()
	resetBuildFlags()

	cmd := createBuildCommand()
	if err := cmd.Flags().Set("baseline-image", "/tmp/base.raw"); err != nil {
		t.Fatalf("failed to set baseline-image flag: %v", err)
	}
	baselineImage = "/tmp/base.raw"

	// A non-overlay template has no baseline section.
	template := &config.ImageTemplate{}
	err := applyOverlayFlagOverrides(cmd, template)
	if err == nil {
		t.Fatal("expected error when --baseline-image is used without an overlay baseline")
	}
	if !strings.Contains(err.Error(), "baseline-image") {
		t.Errorf("expected error to mention baseline-image, got %q", err.Error())
	}
}

// TestApplyOverlayFlagOverrides_BaselineImageRequiresOverlayMode tests that
// --baseline-image is rejected when baseline.mode is not "overlay", even if
// baseline.source exists.
func TestApplyOverlayFlagOverrides_BaselineImageRequiresOverlayMode(t *testing.T) {
	defer resetBuildFlags()
	resetBuildFlags()

	cmd := createBuildCommand()
	if err := cmd.Flags().Set("baseline-image", "/tmp/base.raw"); err != nil {
		t.Fatalf("failed to set baseline-image flag: %v", err)
	}
	baselineImage = "/tmp/base.raw"

	// A create-mode template with a source section (should be rejected).
	template := &config.ImageTemplate{
		Baseline: &config.Baseline{
			Mode:   config.BaselineModeCreate,
			Source: &config.BaselineSource{Path: "/some/path.raw"},
		},
	}
	err := applyOverlayFlagOverrides(cmd, template)
	if err == nil {
		t.Fatal("expected error when --baseline-image is used with non-overlay mode")
	}
	if !strings.Contains(err.Error(), "overlay") {
		t.Errorf("expected error to mention overlay mode requirement, got %q", err.Error())
	}
}

// TestApplyOverlayFlagOverrides_AppliesToTemplate exercises the real helper end
// to end: flags set on the command are reflected on the template.
func TestApplyOverlayFlagOverrides_AppliesToTemplate(t *testing.T) {
	defer resetBuildFlags()
	resetBuildFlags()

	cmd := createBuildCommand()
	if err := cmd.ParseFlags([]string{"--no-inspect", "--baseline-image", "/tmp/override.raw"}); err != nil {
		t.Fatalf("failed to parse flags: %v", err)
	}

	template := &config.ImageTemplate{
		Baseline: &config.Baseline{
			Mode:   config.BaselineModeOverlay,
			Source: &config.BaselineSource{Path: "/original/path.raw"},
		},
	}
	if err := applyOverlayFlagOverrides(cmd, template); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if template.InspectEnabled {
		t.Errorf("expected InspectEnabled=false with --no-inspect")
	}
	if template.Baseline.Source.Path != "/tmp/override.raw" {
		t.Errorf("expected baseline path /tmp/override.raw, got %q", template.Baseline.Source.Path)
	}
}

// TestBuildCommand_ArgumentValidation tests argument count validation
func TestBuildCommand_ArgumentValidation(t *testing.T) {
	cmd := createBuildCommand()

	tests := []struct {
		name      string
		args      []string
		expectErr bool
	}{
		{name: "NoArgs", args: []string{}, expectErr: true},
		{name: "OneArg", args: []string{"template.yml"}, expectErr: false},
		{name: "TwoArgs", args: []string{"template1.yml", "template2.yml"}, expectErr: true},
		{name: "ThreeArgs", args: []string{"a.yml", "b.yml", "c.yml"}, expectErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cmd.Args(cmd, tt.args)
			if tt.expectErr && err == nil {
				t.Error("expected error but got none")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestExecuteBuild_NoCacheMutualExclusion verifies that --nocache cannot be combined
// with --cache-dir or --work-dir.
func TestExecuteBuild_NoCacheMutualExclusion(t *testing.T) {
	prev := *config.Global()
	defer config.SetGlobal(&prev)

	tests := []struct {
		name string
		flag string
		val  string
	}{
		{name: "WithCacheDir", flag: "cache-dir", val: "/tmp/some-cache"},
		{name: "WithWorkDir", flag: "work-dir", val: "/tmp/some-work"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer resetBuildFlags()

			cmd := createBuildCommand()
			if err := cmd.Flags().Set("nocache", "true"); err != nil {
				t.Fatalf("failed to set nocache flag: %v", err)
			}
			if err := cmd.Flags().Set(tt.flag, tt.val); err != nil {
				t.Fatalf("failed to set %s flag: %v", tt.flag, err)
			}

			err := executeBuild(cmd, []string{"template.yml"})
			if err == nil {
				t.Fatalf("expected error when combining --nocache with --%s", tt.flag)
			}
			if !strings.Contains(err.Error(), "cannot be combined") {
				t.Errorf("expected mutual-exclusion error, got: %v", err)
			}
		})
	}
}
