package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecuteConfigInit_CreatesFile(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "my-config.yml")

	cmd := createConfigCommand()
	// Run: image-composer-tool config init <path>
	cmd.SetArgs([]string{"init", target})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute config init failed: %v", err)
	}

	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected config file to be created at %s, got error: %v", target, err)
	}

	contents, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("failed to read generated config: %v", err)
	}

	text := string(contents)
	if !strings.Contains(text, "# ICT - Global Configuration") {
		t.Fatalf("generated config missing header comments: %s", text)
	}

	if !strings.Contains(text, "file: \"image-composer-tool.log\"") {
		t.Fatalf("generated config missing logging.file entry: %s", text)
	}
}
