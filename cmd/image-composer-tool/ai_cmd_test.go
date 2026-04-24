package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateAICommand(t *testing.T) {
	cmd := createAICommand()

	if cmd == nil {
		t.Fatal("createAICommand returned nil")
	}

	if cmd.Use != "ai [query]" {
		t.Errorf("expected Use 'ai [query]', got '%s'", cmd.Use)
	}

	if cmd.Short == "" {
		t.Error("expected non-empty Short description")
	}

	// Verify flags are registered
	flags := cmd.Flags()

	providerFlag := flags.Lookup("provider")
	if providerFlag == nil {
		t.Error("expected --provider flag to be registered")
	}

	templatesDirFlag := flags.Lookup("templates-dir")
	if templatesDirFlag == nil {
		t.Error("expected --templates-dir flag to be registered")
	}

	clearCacheFlag := flags.Lookup("clear-cache")
	if clearCacheFlag == nil {
		t.Error("expected --clear-cache flag to be registered")
	}

	cacheStatsFlag := flags.Lookup("cache-stats")
	if cacheStatsFlag == nil {
		t.Error("expected --cache-stats flag to be registered")
	}

	searchOnlyFlag := flags.Lookup("search-only")
	if searchOnlyFlag == nil {
		t.Error("expected --search-only flag to be registered")
	}

	outputFlag := flags.Lookup("output")
	if outputFlag == nil {
		t.Error("expected --output flag to be registered")
	}
}

func TestDetermineOutputPath(t *testing.T) {
	templatesDir := "./image-templates"

	// Get current working directory for absolute path tests
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}

	tests := []struct {
		name         string
		aiOutputVal  string
		expectedPath string
		expectError  bool
	}{
		{
			name:         "full path with extension",
			aiOutputVal:  "/custom/path/my-template.yml",
			expectedPath: "/custom/path/my-template.yml",
		},
		{
			name:         "name only (no extension, no path)",
			aiOutputVal:  "my-template",
			expectedPath: filepath.Join(templatesDir, "my-template.yml"),
		},
		{
			name:         "name with .yml extension",
			aiOutputVal:  "custom-edge-image.yml",
			expectedPath: filepath.Join(cwd, "custom-edge-image.yml"),
		},
		{
			name:         "relative path with directory",
			aiOutputVal:  "subdir/my-template.yml",
			expectedPath: filepath.Join(cwd, "subdir/my-template.yml"),
		},
		{
			name:         "explicit relative path with ./",
			aiOutputVal:  "./my-output",
			expectedPath: filepath.Join(cwd, "my-output.yml"),
		},
		{
			name:        "no output specified",
			aiOutputVal: "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original value
			origOutput := aiOutput
			defer func() {
				aiOutput = origOutput
			}()

			// Set test value
			aiOutput = tt.aiOutputVal

			path, err := determineOutputPath(templatesDir)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if path != tt.expectedPath {
				t.Errorf("expected path '%s', got '%s'", tt.expectedPath, path)
			}
		})
	}
}

func TestTemplateSaveIntegration(t *testing.T) {
	// Create a temporary directory
	tmpDir, err := os.MkdirTemp("", "ai-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Save original value
	origOutput := aiOutput
	defer func() {
		aiOutput = origOutput
	}()

	// Test saving with --output (name only)
	aiOutput = "test-template"

	outputPath, err := determineOutputPath(tmpDir)
	if err != nil {
		t.Fatalf("failed to determine output path: %v", err)
	}

	expectedPath := filepath.Join(tmpDir, "test-template.yml")
	if outputPath != expectedPath {
		t.Errorf("expected path '%s', got '%s'", expectedPath, outputPath)
	}

	// Verify we can write to this path
	testContent := "test: content"
	if err := os.WriteFile(outputPath, []byte(testContent), 0644); err != nil {
		t.Errorf("failed to write test file: %v", err)
	}

	// Verify the file was created
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Error("expected file to be created")
	}
}

func TestMinFunc(t *testing.T) {
	tests := []struct {
		a, b     int
		expected int
	}{
		{1, 2, 1},
		{5, 3, 3},
		{10, 10, 10},
		{-1, 1, -1},
		{0, 5, 0},
	}

	for _, tt := range tests {
		result := min(tt.a, tt.b)
		if result != tt.expected {
			t.Errorf("min(%d, %d) = %d, expected %d", tt.a, tt.b, result, tt.expected)
		}
	}
}
