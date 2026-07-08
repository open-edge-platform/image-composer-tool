package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/image/imageinspect"
	"github.com/spf13/cobra"
)

// fakeInspector implements the inspector interface used by executeCompare.
type fakeCompareInspector struct {
	imgByPath map[string]*imageinspect.ImageSummary
	errByPath map[string]error
}

func (f *fakeCompareInspector) Inspect(path string) (*imageinspect.ImageSummary, error) {
	if err, ok := f.errByPath[path]; ok {
		return nil, err
	}
	if img, ok := f.imgByPath[path]; ok {
		return img, nil
	}
	return nil, errors.New("not found")
}

func minimalImage(file string, size int64) *imageinspect.ImageSummary {
	return &imageinspect.ImageSummary{
		File:      file,
		SizeBytes: size,
		PartitionTable: imageinspect.PartitionTableSummary{
			Type:               "gpt",
			LogicalSectorSize:  512,
			PhysicalSectorSize: 4096,
			ProtectiveMBR:      true,
			Partitions:         nil,
		},
	}
}

func runCompareExecute(t *testing.T, cmd *cobra.Command, args []string) (string, error) {
	t.Helper()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})

	err := executeCompare(cmd, args)
	return out.String(), err
}

// decodeJSON is tolerant of both “full” compare result and the “diff/summary wrapper” structs.
func decodeJSON(t *testing.T, s string, v any) {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(s))
	dec.DisallowUnknownFields() // helps catch shape regressions in these tests
	if err := dec.Decode(v); err != nil {
		t.Fatalf("failed to decode json: %v\njson:\n%s", err, s)
	}
}

func TestResolveDefaults(t *testing.T) {
	t.Run("json defaults to full when mode empty", func(t *testing.T) {
		format, mode := resolveDefaults("json", "")
		if format != "json" || mode != "full" {
			t.Fatalf("expected (json, full), got (%s, %s)", format, mode)
		}
	})

	t.Run("text defaults to diff when mode empty", func(t *testing.T) {
		format, mode := resolveDefaults("text", "")
		if format != "text" || mode != "diff" {
			t.Fatalf("expected (text, diff), got (%s, %s)", format, mode)
		}
	})

	t.Run("explicit mode is preserved", func(t *testing.T) {
		_, mode := resolveDefaults("text", "summary")
		if mode != "summary" {
			t.Fatalf("expected summary, got %s", mode)
		}
	})
}

func TestCompareCommand_JSONModes_PrettyAndCompact(t *testing.T) {

	origNewInspector := newInspector
	t.Cleanup(func() { newInspector = origNewInspector })

	fi := &fakeCompareInspector{
		imgByPath: map[string]*imageinspect.ImageSummary{
			"a.raw": minimalImage("a.raw", 10),
			"b.raw": minimalImage("b.raw", 20),
		},
		errByPath: map[string]error{},
	}
	newInspector = func(hash bool) inspector { return fi }

	// Make a command instance to provide OutOrStdout/flags context.
	cmd := &cobra.Command{}
	cmd.SetArgs([]string{})

	t.Run("full pretty", func(t *testing.T) {
		outFormat = "json"
		outMode = "full"
		prettyDiffJSON = true

		s, err := runCompareExecute(t, cmd, []string{"a.raw", "b.raw"})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if !strings.Contains(s, "\n  \"") {
			t.Fatalf("expected pretty-printed json with indentation, got:\n%s", s)
		}

		// Validate it looks like ImageCompareResult (at least top-level fields).
		var got struct {
			SchemaVersion string          `json:"schemaVersion"`
			Equality      json.RawMessage `json:"equality"`
			From          json.RawMessage `json:"from"`
			To            json.RawMessage `json:"to"`
			Summary       json.RawMessage `json:"summary"`
			Diff          json.RawMessage `json:"diff"`
		}
		decodeJSON(t, s, &got)
		if got.SchemaVersion == "" {
			t.Fatalf("expected schemaVersion to be set")
		}
	})

	t.Run("diff compact", func(t *testing.T) {
		outFormat = "json"
		outMode = "diff"
		prettyDiffJSON = false

		s, err := runCompareExecute(t, cmd, []string{"a.raw", "b.raw"})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		// compact JSON: no indentation by default; allow newlines from fmt.Fprintln only
		if strings.Contains(s, "\n  \"") {
			t.Fatalf("expected compact json, got:\n%s", s)
		}

		var got struct {
			EqualityClass imageinspect.EqualityClass `json:"equalityClass"`
			Diff          imageinspect.ImageDiff     `json:"diff"`
		}
		decodeJSON(t, s, &got)
	})

	t.Run("summary pretty", func(t *testing.T) {
		outFormat = "json"
		outMode = "summary"
		prettyDiffJSON = true

		s, err := runCompareExecute(t, cmd, []string{"a.raw", "b.raw"})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if !strings.Contains(s, "\n  \"") {
			t.Fatalf("expected pretty json, got:\n%s", s)
		}

		var got struct {
			EqualityClass imageinspect.EqualityClass  `json:"equalityClass"`
			Summary       imageinspect.CompareSummary `json:"summary"`
		}
		decodeJSON(t, s, &got)
	})
}

func TestCompareCommand_TextOutput(t *testing.T) {
	origNewInspector := newInspector
	t.Cleanup(func() { newInspector = origNewInspector })

	// Make two images that differ in partition table type to force a diff
	img1 := minimalImage("a.raw", 10)
	img2 := minimalImage("b.raw", 10)
	img2.PartitionTable.Type = "mbr"

	fi := &fakeCompareInspector{
		imgByPath: map[string]*imageinspect.ImageSummary{
			"a.raw": img1,
			"b.raw": img2,
		},
	}
	newInspector = func(hash bool) inspector { return fi }

	cmd := &cobra.Command{}
	outFormat = "text"
	outMode = "" // let resolveDefaults pick "diff"

	s, err := runCompareExecute(t, cmd, []string{"a.raw", "b.raw"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	// Basic structure checks (don’t overfit exact wording)
	if !strings.Contains(s, "Equality:") {
		t.Fatalf("expected 'Equality:' header, got:\n%s", s)
	}
	if !strings.Contains(s, "Partition table:") {
		t.Fatalf("expected partition table section, got:\n%s", s)
	}
	if !strings.Contains(s, "Type:") {
		t.Fatalf("expected partition table field diff, got:\n%s", s)
	}
}

func TestCompareCommand_InspectorError(t *testing.T) {
	origNewInspector := newInspector
	t.Cleanup(func() { newInspector = origNewInspector })

	fi := &fakeCompareInspector{
		imgByPath: map[string]*imageinspect.ImageSummary{
			"a.raw": minimalImage("a.raw", 10),
		},
		errByPath: map[string]error{
			"b.raw": errors.New("boom"),
		},
	}
	newInspector = func(hash bool) inspector { return fi }

	cmd := &cobra.Command{}
	outFormat = "json"
	outMode = "summary"

	_, err := runCompareExecute(t, cmd, []string{"a.raw", "b.raw"})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "inspection") {
		t.Fatalf("expected inspection error, got: %v", err)
	}
}

func TestCompareCommand_InvalidModeErrors(t *testing.T) {
	origNewInspector := newInspector
	origOutFormat, origOutMode := outFormat, outMode
	t.Cleanup(func() {
		newInspector = origNewInspector
		outFormat, outMode = origOutFormat, origOutMode
	})

	newInspector = func(hash bool) inspector {
		return &fakeCompareInspector{imgByPath: map[string]*imageinspect.ImageSummary{
			"a.raw": minimalImage("a.raw", 1),
			"b.raw": minimalImage("b.raw", 1),
		}}
	}

	cmd := &cobra.Command{}
	outFormat = "json"
	outMode = "bogus"

	_, err := runCompareExecute(t, cmd, []string{"a.raw", "b.raw"})
	if err == nil || !strings.Contains(err.Error(), "invalid --mode") {
		t.Fatalf("expected invalid mode error, got %v", err)
	}
}

func TestCompareCommand_SPDXMode(t *testing.T) {
	origNewInspector := newInspector
	origOutFormat, origOutMode := outFormat, outMode
	t.Cleanup(func() {
		newInspector = origNewInspector
		outFormat, outMode = origOutFormat, origOutMode
	})

	newInspector = func(hash bool) inspector {
		return &fakeCompareInspector{errByPath: map[string]error{
			"a.spdx.json": errors.New("should not inspect images in spdx mode"),
			"b.spdx.json": errors.New("should not inspect images in spdx mode"),
		}}
	}

	tmpDir := t.TempDir()
	fromPath := filepath.Join(tmpDir, "a.spdx.json")
	toPath := filepath.Join(tmpDir, "b.spdx.json")

	fromContent := `{"packages":[{"name":"acl","versionInfo":"2.3.1","downloadLocation":"https://example.com/acl.rpm"}]}`
	toContent := `{"packages":[{"name":"acl","versionInfo":"2.3.2","downloadLocation":"https://example.com/acl.rpm"}]}`

	if err := os.WriteFile(fromPath, []byte(fromContent), 0644); err != nil {
		t.Fatalf("write from SPDX file: %v", err)
	}
	if err := os.WriteFile(toPath, []byte(toContent), 0644); err != nil {
		t.Fatalf("write to SPDX file: %v", err)
	}

	t.Run("JSON", func(t *testing.T) {
		cmd := &cobra.Command{}
		outFormat = "json"
		outMode = "spdx"
		prettyDiffJSON = false

		s, err := runCompareExecute(t, cmd, []string{fromPath, toPath})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var got imageinspect.SPDXCompareResult
		decodeJSON(t, s, &got)
		if got.Equal {
			t.Fatalf("expected SPDX compare to be different")
		}
		if len(got.AddedPackages) == 0 || len(got.RemovedPackages) == 0 {
			t.Fatalf("expected added/removed package entries, got %+v", got)
		}
	})

	t.Run("Text", func(t *testing.T) {
		cmd := &cobra.Command{}
		outFormat = "text"
		outMode = "spdx"

		s, err := runCompareExecute(t, cmd, []string{fromPath, toPath})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(s, "SPDX Compare") {
			t.Fatalf("expected SPDX text header, got:\n%s", s)
		}
	})
}

func TestCompareCommand_SPDXMode_ExtractsFromImages(t *testing.T) {
	origResolver := spdxInputResolver
	origOutFormat, origOutMode, origPretty := outFormat, outMode, prettyDiffJSON
	t.Cleanup(func() {
		spdxInputResolver = origResolver
		outFormat, outMode, prettyDiffJSON = origOutFormat, origOutMode, origPretty
	})

	// Two RAW images (binary, leading NUL from the partition table): the resolver
	// must NOT parse them directly but extract each image's embedded SBOM instead.
	tmpDir := t.TempDir()
	fromImg := filepath.Join(tmpDir, "from.raw")
	toImg := filepath.Join(tmpDir, "to.raw")
	rawBytes := []byte{0x00, 0x01, 0x02, 0x03}
	if err := os.WriteFile(fromImg, rawBytes, 0644); err != nil {
		t.Fatalf("write from image: %v", err)
	}
	if err := os.WriteFile(toImg, rawBytes, 0644); err != nil {
		t.Fatalf("write to image: %v", err)
	}

	sbomByImage := map[string][]byte{
		fromImg: []byte(`{"packages":[{"name":"curl","versionInfo":"8.5.0","downloadLocation":"https://x/curl.deb"}]}`),
		toImg:   []byte(`{"packages":[{"name":"curl","versionInfo":"8.6.0","downloadLocation":"https://x/curl.deb"}]}`),
	}
	spdxInputResolver = func(path string) ([]byte, error) {
		data, ok := sbomByImage[path]
		if !ok {
			return nil, errors.New("unexpected image path: " + path)
		}
		return data, nil
	}

	cmd := &cobra.Command{}
	outFormat = "json"
	outMode = "spdx"
	prettyDiffJSON = false

	s, err := runCompareExecute(t, cmd, []string{fromImg, toImg})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got imageinspect.SPDXCompareResult
	decodeJSON(t, s, &got)
	if got.Equal {
		t.Fatalf("expected the two images' SBOMs to differ, got equal")
	}
	// curl 8.5.0 -> 8.6.0 is one removed + one added (name+version keyed).
	if len(got.AddedPackages) == 0 || len(got.RemovedPackages) == 0 {
		t.Fatalf("expected added/removed entries from the version bump, got %+v", got)
	}
}

func TestCompareCommand_SPDXMode_ImageWithoutSBOM(t *testing.T) {
	origResolver := spdxInputResolver
	origOutFormat, origOutMode := outFormat, outMode
	t.Cleanup(func() {
		spdxInputResolver = origResolver
		outFormat, outMode = origOutFormat, origOutMode
	})

	tmpDir := t.TempDir()
	img := filepath.Join(tmpDir, "nosbom.raw")
	if err := os.WriteFile(img, []byte{0x00, 0x11, 0x22}, 0644); err != nil {
		t.Fatalf("write image: %v", err)
	}
	spdxInputResolver = func(string) ([]byte, error) {
		return nil, errors.New("no embedded SBOM found in image")
	}

	cmd := &cobra.Command{}
	outFormat = "json"
	outMode = "spdx"

	_, err := runCompareExecute(t, cmd, []string{img, img})
	if err == nil {
		t.Fatal("expected an error when the image has no embedded SBOM")
	}
	if !strings.Contains(err.Error(), "no embedded SBOM") {
		t.Fatalf("expected a missing-SBOM error, got: %v", err)
	}
}

func TestLooksLikeJSONDocument(t *testing.T) {
	tmpDir := t.TempDir()
	cases := []struct {
		name    string
		content []byte
		want    bool
	}{
		{"plain json object", []byte(`{"packages":[]}`), true},
		{"json with leading whitespace", []byte("  \n\t{\"a\":1}"), true},
		{"json with utf-8 bom", append([]byte{0xEF, 0xBB, 0xBF}, []byte(`{"a":1}`)...), true},
		{"json with bom and whitespace", append([]byte{0xEF, 0xBB, 0xBF}, []byte("  \n{\"a\":1}")...), true},
		{"raw image with leading NUL", []byte{0x00, 0x01, 0x02}, false},
		{"json array is not an object", []byte(`[1,2,3]`), false},
		{"empty file", []byte{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(tmpDir, tc.name)
			if err := os.WriteFile(p, tc.content, 0644); err != nil {
				t.Fatalf("write: %v", err)
			}
			got, err := looksLikeJSONDocument(p)
			if err != nil {
				t.Fatalf("looksLikeJSONDocument: %v", err)
			}
			if got != tc.want {
				t.Errorf("looksLikeJSONDocument(%q) = %v, want %v", tc.content, got, tc.want)
			}
		})
	}
}

func TestCompareCommand_InvalidFormatErrors(t *testing.T) {
	origNewInspector := newInspector
	origOutFormat, origOutMode := outFormat, outMode
	t.Cleanup(func() {
		newInspector = origNewInspector
		outFormat, outMode = origOutFormat, origOutMode
	})

	newInspector = func(hash bool) inspector {
		return &fakeCompareInspector{imgByPath: map[string]*imageinspect.ImageSummary{
			"a.raw": minimalImage("a.raw", 1),
			"b.raw": minimalImage("b.raw", 1),
		}}
	}

	cmd := &cobra.Command{}
	outFormat = "yaml" // unsupported
	outMode = "diff"

	_, err := runCompareExecute(t, cmd, []string{"a.raw", "b.raw"})
	if err == nil {
		t.Fatalf("expected error for invalid format")
	}
	if !strings.Contains(err.Error(), "text|json") {
		t.Fatalf("expected error mentioning text|json, got %v", err)
	}
}

func TestWriteCompareResult_MarshalError(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := writeCompareResult(cmd, make(chan int), false)
	if err == nil {
		t.Fatalf("expected marshal error for unsupported type")
	}
	if !strings.Contains(err.Error(), "marshal json") {
		t.Fatalf("expected marshal json error, got %v", err)
	}
}
