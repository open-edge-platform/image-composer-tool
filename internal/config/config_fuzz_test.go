package config

import (
	"os"
	"testing"
)

// FuzzLoadTemplate tests the LoadTemplate function with various file inputs
func FuzzLoadTemplate(f *testing.F) {
	// Seed with various YAML content patterns
	f.Add("image:\n  name: test\n  version: 1.0\ntarget:\n  os: linux\n  dist: azl3\n  arch: x86_64\nsystemConfig:\n  kernel:\n    version: 6.1", false)
	f.Add("{}", false)
	f.Add("", false)
	f.Add("invalid: yaml: content: [", false)
	f.Add("image:\n  name: \"\"\n  version: \"\"\ntarget:\n  os: \"\"\n  dist: \"\"\n  arch: \"\"", true)
	f.Add("image:\n  name: \"very-long-name-that-might-cause-buffer-issues-and-memory-problems\"\n  version: \"1.0.0-alpha-beta-gamma\"", false)
	f.Add("---\nimage:\n  name: test\n  version: 1.0", false) // Document separator
	f.Add("image: null\ntarget: null", false)                 // Null values
	f.Add("image:\n  name: \"test\"\n  version: 1.0\n  extra_field: \"should be ignored\"", false)

	f.Fuzz(func(t *testing.T, yamlContent string, validateFull bool) {
		// Write content to a temporary file
		tempFile := t.TempDir() + "/test.yaml"
		if err := writeTestFile(tempFile, yamlContent); err != nil {
			t.Skip("Failed to create temp file")
		}

		// Test LoadTemplate - should not crash regardless of input
		template, err := LoadTemplate(tempFile, validateFull)

		// Function should handle all inputs gracefully
		if err != nil {
			// Error is acceptable for invalid inputs
			if template != nil {
				t.Error("Expected nil template when error occurred")
			}
		} else {
			// If no error, template should be valid
			if template == nil {
				t.Error("Expected non-nil template when no error occurred")
			}
		}
	})
}

// FuzzParseYAMLTemplate tests the parseYAMLTemplate function with raw YAML data
func FuzzParseYAMLTemplate(f *testing.F) {
	// Seed with various YAML patterns that might cause parsing issues
	f.Add([]byte("image:\n  name: test\n  version: 1.0"), false)
	f.Add([]byte(""), false)
	f.Add([]byte("null"), false)
	f.Add([]byte("{}"), false)
	f.Add([]byte("[]"), false)
	f.Add([]byte("invalid yaml content ]["), false)
	f.Add([]byte("---\n---\n---"), false) // Multiple document separators
	f.Add([]byte("image:\n  name: \"test\\\n  with newlines\""), false)
	f.Add([]byte("image:\n  name: test\n  version: !!str 1.0"), false)   // YAML tags
	f.Add([]byte("image: &anchor\n  name: test\nother: *anchor"), false) // YAML anchors
	f.Add([]byte(string(make([]byte, 10000))), false)                    // Large input
	f.Add([]byte("image:\n  name: test\n  version: 1.0\n# comment"), false)

	f.Fuzz(func(t *testing.T, yamlData []byte, validateFull bool) {
		// Test parseYAMLTemplate - should not crash with any input
		template, err := parseYAMLTemplate(yamlData, validateFull)

		// Function should handle all inputs gracefully
		if err != nil {
			// Error is acceptable for invalid inputs
			if template != nil {
				t.Error("Expected nil template when error occurred")
			}
		} else {
			// If no error, template should be valid
			if template == nil {
				t.Error("Expected non-nil template when no error occurred")
			}
		}
	})
}

// writeTestFile is a helper to write content to a file for testing
func writeTestFile(path, content string) error {
	// Use a simple implementation to avoid external dependencies
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.WriteString(content)
	return err
}
