package display_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/display"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
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

// TestPrintImageDirectorySummary_SkipsWorkingFiles guards the artifact list this
// block advertises: a chroot leaves its own files in the build directory, and the
// web UI parses these bullet lines, so listing them here surfaced bash.bashrc and
// debian_version as build artifacts in the UI.
func TestPrintImageDirectorySummary_SkipsWorkingFiles(t *testing.T) {
	dir := t.TempDir()
	outputs := []string{
		"minimal-os-image-ubuntu-26.04.raw.gz",
		"spdx_manifest_deb_minimal-os-image-ubuntu_20260707_165343.json",
	}
	workingFiles := []string{
		"bash.bashrc", "debconf.conf", "debian_version",
		"bindresvport.blacklist", "chrootpkgs-pkgCache.dot", "UPLOAD-MANIFEST.txt",
	}

	for _, name := range append(append([]string{}, outputs...), workingFiles...) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("data"), 0o644); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}

	logs := captureLogs(t, func() {
		display.PrintImageDirectorySummary(dir, "raw")
	})

	// Only the summary block matters: the per-file "Checking file:" debug lines
	// above it mention every entry by design.
	summary := logs
	if i := strings.Index(logs, "Generated Artifacts"); i >= 0 {
		summary = logs[i:]
	}

	for _, name := range outputs {
		if !strings.Contains(summary, name) {
			t.Errorf("expected output %s to be listed, got: %s", name, summary)
		}
	}
	for _, name := range workingFiles {
		if strings.Contains(summary, name) {
			t.Errorf("working file %s must not be listed as an artifact, got: %s", name, summary)
		}
	}
}

// TestPrintImageDirectorySummary_OnlyWorkingFiles ensures a directory holding no
// real outputs reports none, rather than advertising the chroot's leftovers.
func TestPrintImageDirectorySummary_OnlyWorkingFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"bash.bashrc", "debian_version"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("data"), 0o644); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}

	logs := captureLogs(t, func() {
		display.PrintImageDirectorySummary(dir, "raw")
	})

	if !strings.Contains(logs, "No artifacts found") {
		t.Fatalf("expected no artifact warning, got: %s", logs)
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
