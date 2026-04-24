package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// resetValidateFlags resets validate command flags to their default values
func resetValidateFlags() {
	validateMerged = false
}

// TestCreateValidateCommand tests the validate command creation and structure
func TestCreateValidateCommand(t *testing.T) {
	defer resetValidateFlags()

	cmd := createValidateCommand()

	t.Run("CommandMetadata", func(t *testing.T) {
		if cmd == nil {
			t.Fatal("createValidateCommand returned nil")
		}
		if cmd.Use != "validate [flags] TEMPLATE_FILE" {
			t.Errorf("expected Use='validate [flags] TEMPLATE_FILE', got %q", cmd.Use)
		}
		if cmd.Short == "" {
			t.Error("Short description should not be empty")
		}
		if cmd.Long == "" {
			t.Error("Long description should not be empty")
		}
		if !strings.Contains(cmd.Long, "YAML") {
			t.Error("Long description should mention YAML format")
		}
	})

	t.Run("CommandFlags", func(t *testing.T) {
		mergedFlag := cmd.Flags().Lookup("merged")
		if mergedFlag == nil {
			t.Fatal("--merged flag should be registered")
		}
		if mergedFlag.Usage == "" {
			t.Error("--merged flag should have usage text")
		}
		if mergedFlag.DefValue != "false" {
			t.Errorf("--merged flag default should be false, got %q", mergedFlag.DefValue)
		}
	})

	t.Run("ArgsValidation", func(t *testing.T) {
		if cmd.Args == nil {
			t.Fatal("Args validator should be set")
		}
		// Test with no args
		err := cmd.Args(cmd, []string{})
		if err == nil {
			t.Error("should error with no arguments")
		}
		// Test with one arg
		err = cmd.Args(cmd, []string{"template.yml"})
		if err != nil {
			t.Errorf("should accept one argument, got error: %v", err)
		}
		// Test with two args
		err = cmd.Args(cmd, []string{"template.yml", "extra"})
		if err == nil {
			t.Error("should error with two arguments")
		}
	})

	t.Run("RunFunction", func(t *testing.T) {
		if cmd.RunE == nil {
			t.Error("RunE function should be set")
		}
	})

	t.Run("CompletionFunction", func(t *testing.T) {
		if cmd.ValidArgsFunction == nil {
			t.Error("ValidArgsFunction should be set for shell completion")
		}
	})
}

// TestValidateCommand_ArgumentValidation tests argument validation scenarios
func TestValidateCommand_ArgumentValidation(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError bool
	}{
		{
			name:        "NoArguments",
			args:        []string{},
			expectError: true,
		},
		{
			name:        "OneArgument",
			args:        []string{"template.yml"},
			expectError: false,
		},
		{
			name:        "TwoArguments",
			args:        []string{"template.yml", "extra"},
			expectError: true,
		},
		{
			name:        "ThreeArguments",
			args:        []string{"template.yml", "extra1", "extra2"},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer resetValidateFlags()

			cmd := createValidateCommand()
			err := cmd.Args(cmd, tt.args)

			if tt.expectError && err == nil {
				t.Error("expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("expected no error but got: %v", err)
			}
		})
	}
}

// TestValidateCommand_MissingTemplateFile tests behavior with missing file
func TestValidateCommand_MissingTemplateFile(t *testing.T) {
	defer resetValidateFlags()

	cmd := createValidateCommand()
	cmd.SetArgs([]string{"/nonexistent/template.yml"})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for nonexistent template file")
	}
	if !strings.Contains(err.Error(), "validation failed") {
		t.Errorf("error should mention validation failure, got: %v", err)
	}
}

// TestValidateCommand_EmptyTemplateFile tests behavior with empty file
func TestValidateCommand_EmptyTemplateFile(t *testing.T) {
	defer resetValidateFlags()

	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "empty.yml")
	if err := os.WriteFile(templatePath, []byte(""), 0644); err != nil {
		t.Fatalf("failed to create empty template: %v", err)
	}

	cmd := createValidateCommand()
	cmd.SetArgs([]string{templatePath})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for empty template file")
	}
}

// TestValidateCommand_InvalidYAML tests behavior with invalid YAML
func TestValidateCommand_InvalidYAML(t *testing.T) {
	defer resetValidateFlags()

	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "invalid.yml")
	invalidYAML := `
image:
  name: "test"
	invalid_indentation: "bad"
`
	if err := os.WriteFile(templatePath, []byte(invalidYAML), 0644); err != nil {
		t.Fatalf("failed to create invalid template: %v", err)
	}

	cmd := createValidateCommand()
	cmd.SetArgs([]string{templatePath})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

// TestValidateCommand_MergedFlag tests the --merged flag functionality
func TestValidateCommand_MergedFlag(t *testing.T) {
	tests := []struct {
		name       string
		useMerged  bool
		expectFlag bool
	}{
		{
			name:       "WithoutMergedFlag",
			useMerged:  false,
			expectFlag: false,
		},
		{
			name:       "WithMergedFlag",
			useMerged:  true,
			expectFlag: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer resetValidateFlags()

			tmpDir := t.TempDir()
			templatePath := filepath.Join(tmpDir, "template.yml")
			// Create a minimal template that might pass basic validation
			minimalTemplate := `
image:
  name: "test-image"
target:
  imageType: "raw"
  os: "azure-linux"
  dist: "azl3"
  arch: "x86_64"
systemConfig:
  name: "test-config"
`
			if err := os.WriteFile(templatePath, []byte(minimalTemplate), 0644); err != nil {
				t.Fatalf("failed to create template: %v", err)
			}

			cmd := createValidateCommand()
			if tt.useMerged {
				cmd.SetArgs([]string{"--merged", templatePath})
			} else {
				cmd.SetArgs([]string{templatePath})
			}

			// Execute command (may fail due to schema validation, but we're testing flag parsing)
			_ = cmd.Execute()

			// Verify flag was set correctly
			if validateMerged != tt.expectFlag {
				t.Errorf("validateMerged flag: expected %v, got %v", tt.expectFlag, validateMerged)
			}
		})
	}
}

// TestValidateCommand_HelpOutput tests help text
func TestValidateCommand_HelpOutput(t *testing.T) {
	defer resetValidateFlags()

	cmd := createValidateCommand()

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("help command should not error: %v", err)
	}

	output := buf.String()

	expectedStrings := []string{
		"validate",
		"TEMPLATE_FILE",
		"--merged",
		"YAML",
		"schema",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("help output should contain %q", expected)
		}
	}
}

// TestValidateCommand_FlagParsing tests various flag combinations
func TestValidateCommand_FlagParsing(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		expectMerged   bool
		expectArgCount int
	}{
		{
			name:           "NoFlags",
			args:           []string{"template.yml"},
			expectMerged:   false,
			expectArgCount: 1,
		},
		{
			name:           "MergedFlagBefore",
			args:           []string{"--merged", "template.yml"},
			expectMerged:   true,
			expectArgCount: 1,
		},
		{
			name:           "MergedFlagAfter",
			args:           []string{"template.yml", "--merged"},
			expectMerged:   true,
			expectArgCount: 1,
		},
		{
			name:           "MergedFlagExplicitTrue",
			args:           []string{"--merged=true", "template.yml"},
			expectMerged:   true,
			expectArgCount: 1,
		},
		{
			name:           "MergedFlagExplicitFalse",
			args:           []string{"--merged=false", "template.yml"},
			expectMerged:   false,
			expectArgCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer resetValidateFlags()

			cmd := createValidateCommand()
			cmd.SetArgs(tt.args)

			// Parse flags (don't execute to avoid validation errors)
			if err := cmd.ParseFlags(tt.args); err != nil {
				t.Fatalf("failed to parse flags: %v", err)
			}

			if validateMerged != tt.expectMerged {
				t.Errorf("validateMerged: expected %v, got %v", tt.expectMerged, validateMerged)
			}
		})
	}
}

// TestValidateCommand_ShellCompletion tests shell completion function
func TestValidateCommand_ShellCompletion(t *testing.T) {
	defer resetValidateFlags()

	cmd := createValidateCommand()

	if cmd.ValidArgsFunction == nil {
		t.Fatal("ValidArgsFunction should be set")
	}

	completions, directive := cmd.ValidArgsFunction(cmd, []string{}, "")

	t.Run("CompletionValues", func(t *testing.T) {
		if len(completions) == 0 {
			t.Error("should return completion values")
		}
		// Should suggest YAML files
		hasYAML := false
		for _, c := range completions {
			if strings.Contains(c, "yml") || strings.Contains(c, "yaml") {
				hasYAML = true
				break
			}
		}
		if !hasYAML {
			t.Error("completions should include YAML file extensions")
		}
	})

	t.Run("CompletionDirective", func(t *testing.T) {
		if directive != cobra.ShellCompDirectiveFilterFileExt {
			t.Errorf("expected ShellCompDirectiveFilterFileExt, got %d", directive)
		}
	})
}

// TestValidateCommand_LongDescription tests long description content
func TestValidateCommand_LongDescription(t *testing.T) {
	defer resetValidateFlags()

	cmd := createValidateCommand()

	expectedPhrases := []string{
		"YAML",
		"schema",
		"merged",
		"defaults",
	}

	for _, phrase := range expectedPhrases {
		if !strings.Contains(cmd.Long, phrase) {
			t.Errorf("Long description should mention %q", phrase)
		}
	}
}

// TestValidateCommand_DefaultFlagValues tests default flag values
func TestValidateCommand_DefaultFlagValues(t *testing.T) {
	defer resetValidateFlags()

	// Verify global default
	if validateMerged != false {
		t.Errorf("validateMerged should default to false, got %v", validateMerged)
	}

	cmd := createValidateCommand()

	mergedFlag := cmd.Flags().Lookup("merged")
	if mergedFlag == nil {
		t.Fatal("merged flag should exist")
	}

	if mergedFlag.DefValue != "false" {
		t.Errorf("merged flag default should be 'false', got %q", mergedFlag.DefValue)
	}
}

// TestExecuteValidate_DirectCall tests calling executeValidate directly
func TestExecuteValidate_DirectCall(t *testing.T) {
	defer resetValidateFlags()

	t.Run("WithNonexistentFile", func(t *testing.T) {
		cmd := createValidateCommand()
		err := executeValidate(cmd, []string{"/nonexistent/file.yml"})
		if err == nil {
			t.Error("should return error for nonexistent file")
		}
		if !strings.Contains(err.Error(), "validation failed") {
			t.Errorf("error should mention validation failure, got: %v", err)
		}
	})

	t.Run("WithEmptyArguments", func(t *testing.T) {
		cmd := createValidateCommand()
		// This should panic or fail since we expect args[0]
		// But we handle it gracefully
		defer func() {
			if r := recover(); r != nil {
				// Expected to panic with empty args
				t.Logf("recovered from panic: %v", r)
			}
		}()
		_ = executeValidate(cmd, []string{})
	})
}

// TestValidateCommand_ErrorMessages tests error message formatting
func TestValidateCommand_ErrorMessages(t *testing.T) {
	tests := []struct {
		name          string
		templatePath  string
		templateData  string
		expectedError string
	}{
		{
			name:          "NonexistentFile",
			templatePath:  "/path/does/not/exist.yml",
			expectedError: "validation failed",
		},
		{
			name:          "EmptyFile",
			templatePath:  "", // will be created
			templateData:  "",
			expectedError: "validation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer resetValidateFlags()

			templatePath := tt.templatePath
			if tt.templateData != "" || tt.templatePath == "" {
				tmpDir := t.TempDir()
				templatePath = filepath.Join(tmpDir, "template.yml")
				if err := os.WriteFile(templatePath, []byte(tt.templateData), 0644); err != nil {
					t.Fatalf("failed to create template: %v", err)
				}
			}

			cmd := createValidateCommand()
			cmd.SetArgs([]string{templatePath})

			err := cmd.Execute()
			if err == nil {
				t.Error("expected error but got none")
			} else if !strings.Contains(err.Error(), tt.expectedError) {
				t.Errorf("expected error to contain %q, got: %v", tt.expectedError, err)
			}
		})
	}
}
