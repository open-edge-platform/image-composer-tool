package display_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-edge-platform/os-image-composer/internal/utils/display"
	"github.com/open-edge-platform/os-image-composer/internal/utils/logger"
)

func captureLogs(t *testing.T, fn func()) string {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := logger.ReplaceStderrWriter(buf)
	defer logger.ReplaceStderrWriter(prev)

	fn()
	_ = logger.Logger().Sync()

	return buf.String()
}

func TestPrintImageDirectorySummary_MissingDir(t *testing.T) {
	logs := captureLogs(t, func() {
		display.PrintImageDirectorySummary("/path/does/not/exist", "iso")
	})

	if !strings.Contains(logs, "Unable to read image build directory") {
		t.Fatalf("expected warning about missing directory, got: %s", logs)
	}
}

func TestPrintImageDirectorySummary_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	logs := captureLogs(t, func() {
		display.PrintImageDirectorySummary(dir, "raw")
	})

	if !strings.Contains(logs, "No artifacts found") {
		t.Fatalf("expected no artifact warning, got: %s", logs)
	}
}

func TestPrintImageDirectorySummary_WithArtifacts(t *testing.T) {
	dir := t.TempDir()
	files := map[string]int{
		"image.raw": 1024,
		"sbom.json": 512,
	}

	for name, size := range files {
		data := bytes.Repeat([]byte("a"), size)
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}

	nestedDir := filepath.Join(dir, "nested")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("failed to create nested dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nestedDir, "ignored.raw"), []byte("data"), 0o644); err != nil {
		t.Fatalf("failed to write nested file: %v", err)
	}

	logs := captureLogs(t, func() {
		display.PrintImageDirectorySummary(dir, "iso")
	})

	if !strings.Contains(logs, "IMAGE CREATED SUCCESSFULLY") {
		t.Fatalf("expected success banner in logs, got: %s", logs)
	}

	for name := range files {
		if !strings.Contains(logs, name) {
			t.Fatalf("expected artifact %s to be listed", name)
		}
	}

	if strings.Contains(logs, "ignored.raw") {
		t.Fatalf("nested files should not be listed as artifacts: %s", logs)
	}
}

func TestPrintImageBuildingTiming_NoVisibleRows(t *testing.T) {
	logs := captureLogs(t, func() {
		display.PrintImageBuildingTiming("raw", 0, 0, 0, 0, 0, 0, 0)
	})

	if !strings.Contains(logs, "Build Timings:") {
		t.Fatalf("expected build timings header even when all durations are zero, got: %s", logs)
	}

	allStages := []string{
		"Initialization and Configuration",
		"Package Download",
		"Chroot Package Download",
		"Chroot Env Initialization",
		"Image Build",
		"Image Conversion",
		"Finalization and Clean Up",
	}
	for _, stage := range allStages {
		if !strings.Contains(logs, stage) {
			t.Fatalf("expected stage %q to appear in table", stage)
		}
	}

	if !strings.Contains(logs, "Total Time") {
		t.Fatalf("expected total row in table, got: %s", logs)
	}
	if !strings.Contains(logs, "0s") {
		t.Fatalf("expected zero-duration values in table, got: %s", logs)
	}
}

func TestPrintImageBuildingTiming_TableIncludesVisibleRowsAndTotal(t *testing.T) {
	logs := captureLogs(t, func() {
		display.PrintImageBuildingTiming(
			"iso",
			1500*time.Millisecond,
			0,
			500*time.Millisecond,
			250*time.Millisecond,
			2*time.Second,
			0,
			750*time.Millisecond,
		)
	})

	if !strings.Contains(logs, "Build Timings:") {
		t.Fatalf("expected build timings header, got: %s", logs)
	}
	if !strings.Contains(logs, "Stage") || !strings.Contains(logs, "Duration") {
		t.Fatalf("expected table headers, got: %s", logs)
	}

	allStages := []string{
		"Initialization and Configuration",
		"Package Download",
		"Chroot Package Download",
		"Chroot Env Initialization",
		"Image Build",
		"Image Conversion",
		"Finalization and Clean Up",
	}
	for _, stage := range allStages {
		if !strings.Contains(logs, stage) {
			t.Fatalf("expected stage %q to appear in table", stage)
		}
	}

	if !strings.Contains(logs, "Total Time") {
		t.Fatalf("expected total row in table, got: %s", logs)
	}
	if !strings.Contains(logs, "5s") {
		t.Fatalf("expected total duration 5s in table, got: %s", logs)
	}
}
