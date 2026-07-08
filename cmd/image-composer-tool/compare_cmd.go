package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/image/imageinspect"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/spf13/cobra"
)

// Output format command flags
var (
	prettyDiffJSON bool   = true  // Pretty-print JSON output
	outFormat      string         // "text" | "json"
	outMode        string = ""    // "full" | "diff" | "summary" | "spdx"
	hashImages     bool   = false // Skip hashing during inspection
)

// createCompareCommand creates the compare subcommand
func createCompareCommand() *cobra.Command {
	compareCmd := &cobra.Command{
		Use:   "compare [flags] IMAGE_FILE1 IMAGE_FILE2",
		Short: "compares two RAW image files",
		Long: `Compare performs a deep comparison of two generated
		RAW images and provides useful details of the differences such as
		partition table layout, filesystem type, bootloader type and 
		configuration and overall SBOM details if available.`,
		Args: cobra.ExactArgs(2),

		RunE:              executeCompare,
		ValidArgsFunction: templateFileCompletion,
	}

	// Add flags
	compareCmd.Flags().BoolVar(&prettyDiffJSON, "pretty", true,
		"Pretty-print JSON output (only for --format json)")
	compareCmd.Flags().StringVar(&outFormat, "format", "text",
		"Output format: text or json")
	compareCmd.Flags().StringVar(&outMode, "mode", "",
		"Output mode: full, diff, summary, or spdx (default: diff for text, full for json)")
	compareCmd.Flags().BoolVar(&hashImages, "hash-images", false,
		"Compute SHA256 hash of images during inspection (slower but enables binary identity verification")
	return compareCmd
}

func resolveDefaults(format, mode string) (string, string) {
	format = strings.ToLower(format)
	mode = strings.ToLower(mode)

	if mode == "spdx" {
		return format, mode
	}

	// Set default mode if not specified
	if mode == "" {
		if format == "json" {
			mode = "full"
		} else {
			mode = "diff"
		}
	}
	return format, mode
}

// executeCompare handles the compare command execution logic
func executeCompare(cmd *cobra.Command, args []string) error {
	log := logger.Logger()
	imageFile1 := args[0]
	imageFile2 := args[1]
	log.Infof("Comparing image files: (%s) & (%s)", imageFile1, imageFile2)

	format, mode := resolveDefaults(outFormat, outMode)

	if mode == "spdx" {
		fromData, err := resolveSPDXInput(imageFile1)
		if err != nil {
			return fmt.Errorf("SPDX compare failed: reading %s: %w", imageFile1, err)
		}
		toData, err := resolveSPDXInput(imageFile2)
		if err != nil {
			return fmt.Errorf("SPDX compare failed: reading %s: %w", imageFile2, err)
		}

		spdxResult, err := imageinspect.CompareSPDXData(imageFile1, fromData, imageFile2, toData)
		if err != nil {
			return fmt.Errorf("SPDX compare failed: %w", err)
		}

		switch format {
		case "json":
			return writeCompareResult(cmd, spdxResult, prettyDiffJSON)
		case "text":
			return imageinspect.RenderSPDXCompareText(cmd.OutOrStdout(), spdxResult)
		default:
			return fmt.Errorf("invalid --format %q (expected text|json)", format)
		}
	}

	inspector := newInspector(hashImages)

	image1, err1 := inspector.Inspect(imageFile1)
	if err1 != nil {
		return fmt.Errorf("image inspection failed: %v", err1)
	}
	image2, err2 := inspector.Inspect(imageFile2)
	if err2 != nil {
		return fmt.Errorf("image inspection failed: %v", err2)
	}

	compareResult := imageinspect.CompareImages(image1, image2)

	switch format {
	case "json":
		var payload any
		switch mode {
		case "full":
			payload = &compareResult
		case "diff":
			payload = struct {
				//				Equal         bool                   `json:"equal"`
				EqualityClass string                 `json:"equalityClass"`
				Diff          imageinspect.ImageDiff `json:"diff"`
			}{EqualityClass: string(compareResult.Equality.Class), Diff: compareResult.Diff}
		case "summary":
			payload = struct {
				EqualityClass string                      `json:"equalityClass"`
				Summary       imageinspect.CompareSummary `json:"summary"`
			}{EqualityClass: string(compareResult.Equality.Class), Summary: compareResult.Summary}
		default:
			return fmt.Errorf("invalid --mode or --format %q (expected --mode=diff|summary|full|spdx) and --format=text|json", mode)
		}
		return writeCompareResult(cmd, payload, prettyDiffJSON)

	case "text":
		return imageinspect.RenderCompareText(cmd.OutOrStdout(), &compareResult,
			imageinspect.CompareTextOptions{Mode: mode})

	default:
		return fmt.Errorf("invalid --format %q (expected text|json)", format)
	}
}

// spdxInputResolver extracts the embedded SBOM from an image. It is a package
// var so tests can inject a fake without touching a real disk image.
var spdxInputResolver = extractImageSBOM

// resolveSPDXInput returns the SPDX JSON bytes for a compare input. The input is
// either a standalone SPDX JSON document or an OS image with an embedded SBOM;
// they are distinguished by peeking at the first non-whitespace byte ('{' means
// a JSON document). A raw image can be many GB, so a JSON file is read directly
// while an image is opened and only its embedded SBOM is extracted, never slurped
// whole.
func resolveSPDXInput(path string) ([]byte, error) {
	isJSON, err := looksLikeJSONDocument(path)
	if err != nil {
		return nil, err
	}
	if isJSON {
		return os.ReadFile(path)
	}
	return spdxInputResolver(path)
}

// looksLikeJSONDocument reports whether the file's first non-whitespace byte is
// '{', i.e. it is a JSON object rather than a binary disk image. A leading UTF-8
// BOM is skipped first. It reads only a small prefix, so it is safe on multi-GB
// images.
func looksLikeJSONDocument(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return false, err
	}
	prefix := buf[:n]
	// Skip a leading UTF-8 BOM so a BOM-prefixed JSON document is not mistaken
	// for a binary disk image.
	prefix = bytes.TrimPrefix(prefix, []byte{0xEF, 0xBB, 0xBF})
	for _, b := range prefix {
		switch b {
		case ' ', '\t', '\r', '\n':
			continue // skip leading whitespace
		case '{':
			return true, nil
		default:
			return false, nil
		}
	}
	return false, nil // empty or all-whitespace: not JSON
}

// extractImageSBOM inspects an OS image and returns its embedded SBOM document.
// It fails with a clear message when the image carries no SBOM, so the compare
// error names the missing artifact rather than surfacing a JSON parse error.
func extractImageSBOM(imagePath string) ([]byte, error) {
	summary, err := imageinspect.NewDiskfsInspectorWithOptions(false, true).Inspect(imagePath)
	if err != nil {
		return nil, fmt.Errorf("inspecting image: %w", err)
	}
	if !summary.SBOM.Present || len(summary.SBOM.Content) == 0 {
		notes := ""
		if len(summary.SBOM.Notes) > 0 {
			notes = ": " + strings.Join(summary.SBOM.Notes, "; ")
		}
		return nil, fmt.Errorf("no embedded SBOM found in image (expected at %s)%s", "/usr/share/sbom", notes)
	}
	return summary.SBOM.Content, nil
}

func writeCompareResult(cmd *cobra.Command, v any, pretty bool) error {
	out := cmd.OutOrStdout()

	var (
		b   []byte
		err error
	)
	if pretty {
		b, err = json.MarshalIndent(v, "", "  ")
	} else {
		b, err = json.Marshal(v)
	}
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	_, _ = fmt.Fprintln(out, string(b))
	return nil
}
