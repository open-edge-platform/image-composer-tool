package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// captureOutput captures everything written to os.Stdout and os.Stderr
// during the execution of fn and returns it as a string.
func captureOutput(t *testing.T, fn func()) string {
	t.Helper()

	// Create a pipe and swap stdout/stderr
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer func() {
		if err := pr.Close(); err != nil {
			t.Errorf("close read pipe: %v", err)
		}
	}()

	oldOut := os.Stdout
	oldErr := os.Stderr
	os.Stdout = pw
	os.Stderr = pw
	defer func() {
		os.Stdout = oldOut
		os.Stderr = oldErr
	}()

	// Read from the pipe concurrently
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, pr)
		done <- buf.String()
	}()

	// Run the function while stdout/stderr are redirected
	fn()

	// Close writer to unblock reader goroutine
	if err := pw.Close(); err != nil {
		t.Fatalf("close write pipe: %v", err)
	}

	// Wait for the captured output
	return <-done
}

func TestVersionCommand_PrintsFields(t *testing.T) {
	cmd := &cobra.Command{Use: "os-image-composer"}
	cmd.AddCommand(createVersionCommand())

	// Execute "version" and capture global output (fmt.Printf used by the command).
	out := captureOutput(t, func() {
		cmd.SetArgs([]string{"version"})
		_ = cmd.Execute()
	})

	// We don't assert specific values (they vary by build), just presence of labels
	for _, want := range []string{"Build Date:", "Commit:", "Organization:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}
