package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/os-image-composer/internal/image/imageinspect"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// resetInspectFlags resets inspect flags to defaults.
func resetInspectFlags() {
	outputFormat = "text"
	prettyJSON = false
	sbomOutPath = ""
	newInspector = func(hash bool) inspector {
		return imageinspect.NewDiskfsInspector(hash)
	}
	newInspectorWithSBOM = func(hash bool, inspectSBOM bool) inspector {
		return imageinspect.NewDiskfsInspectorWithOptions(hash, inspectSBOM)
	}
}

// fakeInspector is a tiny test double so we can cover output branches without
// needing a real disk image.
type fakeInspector struct {
	summary *imageinspect.ImageSummary
	err     error
}

func (f *fakeInspector) Inspect(imagePath string) (*imageinspect.ImageSummary, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.summary, nil
}

// helper: execute a cobra command and capture output.
func execCmd(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestCreateInspectCommand(t *testing.T) {
	defer resetInspectFlags()

	cmd := createInspectCommand()

	t.Run("CommandMetadata", func(t *testing.T) {
		if cmd == nil {
			t.Fatal("createInspectCommand returned nil")
		}
		if cmd.Use != "inspect [flags] IMAGE_FILE" {
			t.Errorf("expected Use='inspect [flags] IMAGE_FILE', got %q", cmd.Use)
		}
		if cmd.Short == "" {
			t.Error("Short description should not be empty")
		}
		if cmd.Long == "" {
			t.Error("Long description should not be empty")
		}
	})

	t.Run("CommandFlags", func(t *testing.T) {
		formatFlag := cmd.Flags().Lookup("format")
		if formatFlag == nil {
			t.Fatal("--format flag should be registered")
		}
		if formatFlag.DefValue != "text" {
			t.Errorf("--format default should be 'text', got %q", formatFlag.DefValue)
		}
		if formatFlag.Usage == "" {
			t.Error("--format should have usage text")
		}
	})

	t.Run("CommandFlags", func(t *testing.T) {
		formatFlag := cmd.Flags().Lookup("pretty")
		if formatFlag == nil {
			t.Fatal("--pretty flag should be registered")
		}
		if formatFlag.DefValue != "false" {
			t.Errorf("--pretty default should be 'false', got %q", formatFlag.DefValue)
		}
		if formatFlag.Usage == "" {
			t.Error("--pretty should have usage text")
		}
	})

	t.Run("ExtractSBOMFlagOptionalValue", func(t *testing.T) {
		extractFlag := cmd.Flags().Lookup("extract-sbom")
		if extractFlag == nil {
			t.Fatal("--extract-sbom flag should be registered")
		}
		if extractFlag.NoOptDefVal != "." {
			t.Fatalf("expected --extract-sbom NoOptDefVal '.', got %q", extractFlag.NoOptDefVal)
		}
	})

	t.Run("ArgsValidation", func(t *testing.T) {
		if cmd.Args == nil {
			t.Fatal("Args validator should be set")
		}

		if err := cmd.Args(cmd, []string{}); err == nil {
			t.Error("should error with no arguments")
		}
		if err := cmd.Args(cmd, []string{"image.raw"}); err != nil {
			t.Errorf("should accept one argument, got error: %v", err)
		}
		if err := cmd.Args(cmd, []string{"image.raw", "extra"}); err == nil {
			t.Error("should error with two arguments")
		}
	})

	t.Run("CompletionFunction", func(t *testing.T) {
		if cmd.ValidArgsFunction == nil {
			t.Error("ValidArgsFunction should be set for shell completion")
		}
	})
}

func TestInspectCommand_HelpOutput(t *testing.T) {
	defer resetInspectFlags()

	cmd := createInspectCommand()

	out, err := execCmd(t, cmd, "--help")
	if err != nil {
		t.Fatalf("help should not error: %v", err)
	}

	expected := []string{
		"inspect",
		"IMAGE_FILE",
		"--format",
		"--extract-sbom",
	}
	for _, s := range expected {
		if !strings.Contains(out, s) {
			t.Errorf("help output should contain %q", s)
		}
	}
}

func TestWriteExtractedSBOM(t *testing.T) {
	t.Run("NoSBOMPresent_NoWrite", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := writeExtractedSBOM(imageinspect.SBOMSummary{}, tmpDir)
		if err == nil {
			t.Fatalf("expected error when SBOM is not present")
		}
		if !strings.Contains(err.Error(), "embedded SBOM not found") {
			t.Fatalf("expected missing SBOM error, got: %v", err)
		}
	})

	t.Run("WritesToDirectory", func(t *testing.T) {
		tmpDir := t.TempDir()
		sbom := imageinspect.SBOMSummary{
			Present:  true,
			FileName: "spdx_manifest_deb_demo.json",
			Content:  []byte(`{"packages":[]}`),
		}

		err := writeExtractedSBOM(sbom, tmpDir)
		if err != nil {
			t.Fatalf("writeExtractedSBOM returned error: %v", err)
		}

		outFile := filepath.Join(tmpDir, sbom.FileName)
		data, readErr := os.ReadFile(outFile)
		if readErr != nil {
			t.Fatalf("failed to read written SBOM file: %v", readErr)
		}
		if string(data) != string(sbom.Content) {
			t.Fatalf("written SBOM content mismatch")
		}
	})

	t.Run("WritesToExplicitFile", func(t *testing.T) {
		tmpDir := t.TempDir()
		outFile := filepath.Join(tmpDir, "custom-spdx.json")
		sbom := imageinspect.SBOMSummary{
			Present: true,
			Content: []byte(`{"packages":[{"name":"acl"}]}`),
		}

		err := writeExtractedSBOM(sbom, outFile)
		if err != nil {
			t.Fatalf("writeExtractedSBOM returned error: %v", err)
		}

		data, readErr := os.ReadFile(outFile)
		if readErr != nil {
			t.Fatalf("failed to read written SBOM file: %v", readErr)
		}
		if string(data) != string(sbom.Content) {
			t.Fatalf("written SBOM content mismatch")
		}
	})

	t.Run("EmptyOutPathDefaultsToCurrentDirectory", func(t *testing.T) {
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatalf("failed to get cwd: %v", err)
		}

		tmpDir := t.TempDir()
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatalf("failed to chdir to temp dir: %v", err)
		}
		t.Cleanup(func() {
			_ = os.Chdir(cwd)
		})

		sbom := imageinspect.SBOMSummary{
			Present:  true,
			FileName: "spdx_manifest_default.json",
			Content:  []byte(`{"packages":[]}`),
		}

		err = writeExtractedSBOM(sbom, "")
		if err != nil {
			t.Fatalf("writeExtractedSBOM returned error: %v", err)
		}

		data, readErr := os.ReadFile(filepath.Join(tmpDir, sbom.FileName))
		if readErr != nil {
			t.Fatalf("failed to read default-path SBOM file: %v", readErr)
		}
		if string(data) != string(sbom.Content) {
			t.Fatalf("written SBOM content mismatch")
		}
	})
}

func TestInspectCommand_ExtractSBOMWithoutValue(t *testing.T) {
	defer resetInspectFlags()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir to temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	newInspector = func(hash bool) inspector {
		return &fakeInspector{
			summary: &imageinspect.ImageSummary{
				File: "fake.img",
				SBOM: imageinspect.SBOMSummary{
					Present:  true,
					FileName: "spdx_manifest_rpm_demo.json",
					Content:  []byte(`{"packages":[]}`),
				},
			},
		}
	}
	newInspectorWithSBOM = func(hash bool, inspectSBOM bool) inspector {
		return &fakeInspector{
			summary: &imageinspect.ImageSummary{
				File: "fake.img",
				SBOM: imageinspect.SBOMSummary{
					Present:  true,
					FileName: "spdx_manifest_rpm_demo.json",
					Content:  []byte(`{"packages":[]}`),
				},
			},
		}
	}

	cmd := createInspectCommand()
	_, err = execCmd(t, cmd, "--extract-sbom", "fake.img")
	if err != nil {
		t.Fatalf("expected inspect with bare --extract-sbom to succeed, got: %v", err)
	}

	if _, statErr := os.Stat(filepath.Join(tmpDir, "spdx_manifest_rpm_demo.json")); statErr != nil {
		t.Fatalf("expected extracted SBOM file in current directory, stat err: %v", statErr)
	}
}

func TestExecuteInspect_InspectorSelection(t *testing.T) {
	defer resetInspectFlags()

	t.Run("UsesRegularInspectorWhenExtractNotRequested", func(t *testing.T) {
		resetInspectFlags()
		regularCalled := false
		sbomInspectorCalled := false

		newInspector = func(hash bool) inspector {
			regularCalled = true
			return &fakeInspector{summary: &imageinspect.ImageSummary{File: "fake.img", SizeBytes: 1}}
		}
		newInspectorWithSBOM = func(hash bool, inspectSBOM bool) inspector {
			sbomInspectorCalled = true
			return &fakeInspector{summary: &imageinspect.ImageSummary{File: "fake.img", SizeBytes: 1}}
		}

		cmd := createInspectCommand()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})

		err := executeInspect(cmd, []string{"fake.img"})
		if err != nil {
			t.Fatalf("expected success, got error: %v", err)
		}
		if !regularCalled {
			t.Fatalf("expected regular inspector constructor to be called")
		}
		if sbomInspectorCalled {
			t.Fatalf("did not expect SBOM inspector constructor when extraction not requested")
		}
	})

	t.Run("UsesSBOMInspectorWhenExtractRequested", func(t *testing.T) {
		resetInspectFlags()
		tmpDir := t.TempDir()
		outFile := filepath.Join(tmpDir, "out.json")

		regularCalled := false
		sbomInspectorCalled := false

		newInspector = func(hash bool) inspector {
			regularCalled = true
			return &fakeInspector{summary: &imageinspect.ImageSummary{File: "fake.img", SizeBytes: 1}}
		}
		newInspectorWithSBOM = func(hash bool, inspectSBOM bool) inspector {
			sbomInspectorCalled = true
			return &fakeInspector{
				summary: &imageinspect.ImageSummary{
					File: "fake.img",
					SBOM: imageinspect.SBOMSummary{
						Present:  true,
						FileName: "spdx_manifest_demo.json",
						Content:  []byte(`{"packages":[]}`),
					},
				},
			}
		}

		cmd := createInspectCommand()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		if err := cmd.Flags().Set("extract-sbom", outFile); err != nil {
			t.Fatalf("set extract-sbom flag: %v", err)
		}

		err := executeInspect(cmd, []string{"fake.img"})
		if err != nil {
			t.Fatalf("expected success, got error: %v", err)
		}
		if !sbomInspectorCalled {
			t.Fatalf("expected SBOM inspector constructor to be called")
		}
		if regularCalled {
			t.Fatalf("did not expect regular inspector constructor when extraction is requested")
		}
		if _, statErr := os.Stat(outFile); statErr != nil {
			t.Fatalf("expected extracted SBOM file at %s: %v", outFile, statErr)
		}
	})
}

func TestWriteExtractedSBOM_MissingIncludesNotes(t *testing.T) {
	err := writeExtractedSBOM(imageinspect.SBOMSummary{
		Notes: []string{"first reason", "second reason"},
	}, "ignored.json")
	if err == nil {
		t.Fatalf("expected error when SBOM is missing")
	}
	if !strings.Contains(err.Error(), "first reason") || !strings.Contains(err.Error(), "second reason") {
		t.Fatalf("expected notes in error message, got: %v", err)
	}
}

func TestExecuteInspect_DirectCall(t *testing.T) {
	defer resetInspectFlags()

	t.Run("NonexistentFile", func(t *testing.T) {
		cmd := createInspectCommand()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})

		err := executeInspect(cmd, []string{"/nonexistent/image.raw"})
		if err == nil {
			t.Fatal("expected error for nonexistent image")
		}
		if !strings.Contains(strings.ToLower(err.Error()), "inspection") &&
			!strings.Contains(strings.ToLower(err.Error()), "stat") &&
			!strings.Contains(strings.ToLower(err.Error()), "open") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("UnsupportedFormat", func(t *testing.T) {
		resetInspectFlags()
		t.Cleanup(resetInspectFlags)

		oldNew := newInspector
		t.Cleanup(func() { newInspector = oldNew })
		newInspector = func(hash bool) inspector {
			return &fakeInspector{
				summary: &imageinspect.ImageSummary{File: "fake.img", SizeBytes: 123},
			}
		}

		cmd := createInspectCommand()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})

		if err := cmd.Flags().Set("format", "nope"); err != nil {
			t.Fatalf("failed to set format flag: %v", err)
		}

		err := executeInspect(cmd, []string{"fake.img"})
		if err == nil {
			t.Fatal("expected error for unsupported output format")
		}
		if !strings.Contains(err.Error(), "unsupported output format") {
			t.Errorf("expected unsupported output format error, got: %v", err)
		}
	})

	t.Run("InspectFailsPropagates", func(t *testing.T) {
		resetInspectFlags()
		t.Cleanup(resetInspectFlags)

		oldNew := newInspector
		t.Cleanup(func() { newInspector = oldNew })
		newInspector = func(hash bool) inspector {
			return &fakeInspector{err: errors.New("boom")}
		}

		cmd := createInspectCommand()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})

		err := executeInspect(cmd, []string{"fake.img"})
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "image inspection failed") {
			t.Errorf("expected wrapped error, got: %v", err)
		}
	})
}

func TestInspectCommand_OutputFormats_WithFakeInspector(t *testing.T) {
	defer resetInspectFlags()

	// Deterministic summary for output checks
	fake := &imageinspect.ImageSummary{
		File:      "fake.img",
		SizeBytes: 42,
		PartitionTable: imageinspect.PartitionTableSummary{
			Type:              "gpt",
			LogicalSectorSize: 512,
			Partitions: []imageinspect.PartitionSummary{
				{Index: 1, Name: "boot", Type: "C12A7328-F81F-11D2-BA4B-00A0C93EC93B", StartLBA: 2048, EndLBA: 4095, SizeBytes: 2048 * 512},
			},
		},
	}

	newInspector = func(hash bool) inspector {
		return &fakeInspector{summary: fake}
	}
	defer resetInspectFlags()

	t.Run("Text", func(t *testing.T) {
		outputFormat = "text"
		cmd := createInspectCommand()
		out, err := execCmd(t, cmd, "fake.img")
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}

		_ = out
	})

	t.Run("JSON", func(t *testing.T) {
		resetInspectFlags()

		newInspector = func(hash bool) inspector {
			return &fakeInspector{
				summary: &imageinspect.ImageSummary{
					File:      "fake.img",
					SizeBytes: 42,
					PartitionTable: imageinspect.PartitionTableSummary{
						Type:              "gpt",
						LogicalSectorSize: 512,
						Partitions: []imageinspect.PartitionSummary{
							{Index: 1, Name: "boot", Type: "C12A7328-F81F-11D2-BA4B-00A0C93EC93B", StartLBA: 2048, EndLBA: 4095, SizeBytes: 1024 * 1024},
						},
					},
				},
			}
		}

		cmd := createInspectCommand()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})

		if err := cmd.Flags().Set("format", "json"); err != nil {
			t.Fatalf("set flag: %v", err)
		}

		out, err := execCmd(t, cmd, "fake.img")
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}

		// Stronger: unmarshal instead of substring match
		var got imageinspect.ImageSummary
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("invalid json: %v\nout:\n%s", err, out)
		}
		if got.File != "fake.img" {
			t.Fatalf("expected File=fake.img got %q", got.File)
		}
		if got.PartitionTable.Type != "gpt" {
			t.Fatalf("expected PT type=gpt got %q", got.PartitionTable.Type)
		}
	})

	t.Run("YAML", func(t *testing.T) {
		newInspector = func(hash bool) inspector {
			return &fakeInspector{
				summary: &imageinspect.ImageSummary{
					File:      "fake.img",
					SizeBytes: 42,
					PartitionTable: imageinspect.PartitionTableSummary{
						Type:              "gpt",
						LogicalSectorSize: 512,
						Partitions: []imageinspect.PartitionSummary{
							{Index: 1, Name: "boot", Type: "C12A7328-F81F-11D2-BA4B-00A0C93EC93B", StartLBA: 2048, EndLBA: 4095, SizeBytes: 1024 * 1024},
						},
					},
				},
			}
		}

		cmd := createInspectCommand()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})

		if err := cmd.Flags().Set("format", "yaml"); err != nil {
			t.Fatalf("set flag: %v", err)
		}

		out, err := execCmd(t, cmd, "fake.img")
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}

		var got imageinspect.ImageSummary
		if err := yaml.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("invalid yaml: %v\nout:\n%s", err, out)
		}
		if got.File != "fake.img" {
			t.Fatalf("expected File=fake.img got %q", got.File)
		}
		if got.PartitionTable.Type != "gpt" {
			t.Fatalf("expected PT type=gpt got %q", got.PartitionTable.Type)
		}
	})
}
