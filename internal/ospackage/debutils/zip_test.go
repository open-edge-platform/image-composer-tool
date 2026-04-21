package debutils

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ulikunitz/xz"
)

// TestDecompress_Success tests successful decompression of a gzipped file
func TestDecompress_Success(t *testing.T) {
	// Create a temporary directory for test files
	tempDir, err := os.MkdirTemp("", "zip_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test data
	testContent := "Hello, World!\nThis is a test file for gzip decompression.\nLine 3 with some data.\n"

	// Create input file paths
	inputFile := filepath.Join(tempDir, "test_input.gz")
	outputFile := filepath.Join(tempDir, "test_output.txt")

	// Create a gzipped file with test content
	err = createGzipFile(inputFile, testContent)
	if err != nil {
		t.Fatalf("Failed to create gzip test file: %v", err)
	}

	// Test the Decompress function
	result, err := Decompress(inputFile, outputFile)
	if err != nil {
		t.Fatalf("Decompress failed: %v", err)
	}

	// Verify return value
	if len(result) != 1 {
		t.Errorf("Expected 1 file in result, got %d", len(result))
	}
	if result[0] != outputFile {
		t.Errorf("Expected output file %s, got %s", outputFile, result[0])
	}

	// Verify the output file exists
	if _, err := os.Stat(outputFile); os.IsNotExist(err) {
		t.Errorf("Output file %s was not created", outputFile)
	}

	// Verify the content was decompressed correctly
	decompressedContent, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read decompressed file: %v", err)
	}

	if string(decompressedContent) != testContent {
		t.Errorf("Content mismatch.\nExpected: %q\nGot: %q", testContent, string(decompressedContent))
	}
}

// TestDecompress_LargeFile tests decompression of a larger file
func TestDecompress_LargeFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "zip_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a larger test content (repeat content multiple times)
	baseContent := "This is a line of text that will be repeated many times to create a larger file for testing.\n"
	testContent := strings.Repeat(baseContent, 1000) // Create ~80KB of text

	inputFile := filepath.Join(tempDir, "large_test.gz")
	outputFile := filepath.Join(tempDir, "large_output.txt")

	// Create gzipped file
	err = createGzipFile(inputFile, testContent)
	if err != nil {
		t.Fatalf("Failed to create large gzip test file: %v", err)
	}

	// Test decompression
	result, err := Decompress(inputFile, outputFile)
	if err != nil {
		t.Fatalf("Decompress failed for large file: %v", err)
	}

	// Verify result
	if len(result) != 1 || result[0] != outputFile {
		t.Errorf("Unexpected result for large file decompression: %v", result)
	}

	// Verify content
	decompressedContent, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read large decompressed file: %v", err)
	}

	if string(decompressedContent) != testContent {
		t.Errorf("Large file content mismatch. Expected length: %d, Got length: %d",
			len(testContent), len(decompressedContent))
	}
}

// TestDecompress_EmptyFile tests decompression of an empty gzipped file
func TestDecompress_EmptyFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "zip_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	inputFile := filepath.Join(tempDir, "empty_test.gz")
	outputFile := filepath.Join(tempDir, "empty_output.txt")

	// Create empty gzipped file
	err = createGzipFile(inputFile, "")
	if err != nil {
		t.Fatalf("Failed to create empty gzip test file: %v", err)
	}

	// Test decompression
	result, err := Decompress(inputFile, outputFile)
	if err != nil {
		t.Fatalf("Decompress failed for empty file: %v", err)
	}

	// Verify result
	if len(result) != 1 || result[0] != outputFile {
		t.Errorf("Unexpected result for empty file decompression: %v", result)
	}

	// Verify output file is empty
	decompressedContent, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read empty decompressed file: %v", err)
	}

	if len(decompressedContent) != 0 {
		t.Errorf("Expected empty file, got content: %q", string(decompressedContent))
	}
}

// TestDecompress_InputFileNotExist tests error handling when input file doesn't exist
func TestDecompress_InputFileNotExist(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "zip_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	nonExistentInput := filepath.Join(tempDir, "nonexistent.gz")
	outputFile := filepath.Join(tempDir, "output.txt")

	// Test with non-existent input file
	result, err := Decompress(nonExistentInput, outputFile)
	if err == nil {
		t.Error("Expected error for non-existent input file, but got none")
	}

	if result != nil {
		t.Errorf("Expected nil result for error case, got: %v", result)
	}

	// Verify error message contains expected information
	if !strings.Contains(err.Error(), "failed to open gz file") {
		t.Errorf("Error message should mention 'failed to open gz file', got: %v", err)
	}
}

// TestDecompress_InvalidOutputPath tests error handling when output path is invalid
func TestDecompress_InvalidOutputPath(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "zip_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create valid input file
	inputFile := filepath.Join(tempDir, "valid_input.gz")
	err = createGzipFile(inputFile, "test content")
	if err != nil {
		t.Fatalf("Failed to create test input file: %v", err)
	}

	// Use invalid output path (directory that doesn't exist)
	invalidOutput := filepath.Join(tempDir, "nonexistent_dir", "output.txt")

	// Test with invalid output path
	result, err := Decompress(inputFile, invalidOutput)
	if err == nil {
		t.Error("Expected error for invalid output path, but got none")
	}

	if result != nil {
		t.Errorf("Expected nil result for error case, got: %v", result)
	}

	// Verify error message
	if !strings.Contains(err.Error(), "failed to create decompressed file") {
		t.Errorf("Error message should mention 'failed to create decompressed file', got: %v", err)
	}
}

// TestDecompress_NotGzipFile tests error handling when input file is not a valid gzip file
func TestDecompress_NotGzipFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "zip_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a file that's not gzipped
	inputFile := filepath.Join(tempDir, "not_gzip.gz")
	err = os.WriteFile(inputFile, []byte("This is not a gzip file"), 0644)
	if err != nil {
		t.Fatalf("Failed to create non-gzip test file: %v", err)
	}

	outputFile := filepath.Join(tempDir, "output.txt")

	// Test with non-gzip file
	result, err := Decompress(inputFile, outputFile)
	if err == nil {
		t.Error("Expected error for non-gzip file, but got none")
	}

	if result != nil {
		t.Errorf("Expected nil result for error case, got: %v", result)
	}

	// Verify error message
	if !strings.Contains(err.Error(), "failed to create gzip reader") {
		t.Errorf("Error message should mention 'failed to create gzip reader', got: %v", err)
	}
}

// TestDecompress_CorruptedGzipFile tests error handling with corrupted gzip data
func TestDecompress_CorruptedGzipFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "zip_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	inputFile := filepath.Join(tempDir, "corrupted.gz")
	outputFile := filepath.Join(tempDir, "output.txt")

	// Create a corrupted gzip file (valid header but corrupted data)
	// Start with valid gzip magic number but add invalid data
	corruptedData := []byte{0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xff, 0x12, 0x34, 0x56, 0x78}
	err = os.WriteFile(inputFile, corruptedData, 0644)
	if err != nil {
		t.Fatalf("Failed to create corrupted gzip file: %v", err)
	}

	// Test with corrupted gzip file
	result, err := Decompress(inputFile, outputFile)
	if err == nil {
		t.Error("Expected error for corrupted gzip file, but got none")
	}

	if result != nil {
		t.Errorf("Expected nil result for error case, got: %v", result)
	}

	// Error could be in creating reader or during decompression
	if !strings.Contains(err.Error(), "failed to") {
		t.Errorf("Error message should contain 'failed to', got: %v", err)
	}
}

// TestDecompress_OverwriteExistingFile tests that existing output files are overwritten
func TestDecompress_OverwriteExistingFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "zip_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	testContent := "New decompressed content"
	existingContent := "This file already exists"

	inputFile := filepath.Join(tempDir, "input.gz")
	outputFile := filepath.Join(tempDir, "existing_output.txt")

	// Create existing output file
	err = os.WriteFile(outputFile, []byte(existingContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create existing output file: %v", err)
	}

	// Create input gzip file
	err = createGzipFile(inputFile, testContent)
	if err != nil {
		t.Fatalf("Failed to create gzip input file: %v", err)
	}

	// Test decompression (should overwrite existing file)
	result, err := Decompress(inputFile, outputFile)
	if err != nil {
		t.Fatalf("Decompress failed: %v", err)
	}

	// Verify result
	if len(result) != 1 || result[0] != outputFile {
		t.Errorf("Unexpected result: %v", result)
	}

	// Verify the file was overwritten
	finalContent, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read final output file: %v", err)
	}

	if string(finalContent) != testContent {
		t.Errorf("File was not overwritten. Expected: %q, Got: %q", testContent, string(finalContent))
	}
}

// TestDecompress_BinaryData tests decompression of binary data
func TestDecompress_BinaryData(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "zip_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create binary test data (including null bytes and non-UTF8 data)
	binaryData := make([]byte, 256)
	for i := 0; i < 256; i++ {
		binaryData[i] = byte(i)
	}

	inputFile := filepath.Join(tempDir, "binary.gz")
	outputFile := filepath.Join(tempDir, "binary_output")

	// Create gzipped binary file
	err = createGzipFile(inputFile, string(binaryData))
	if err != nil {
		t.Fatalf("Failed to create binary gzip file: %v", err)
	}

	// Test decompression
	result, err := Decompress(inputFile, outputFile)
	if err != nil {
		t.Fatalf("Decompress failed for binary data: %v", err)
	}

	// Verify result
	if len(result) != 1 || result[0] != outputFile {
		t.Errorf("Unexpected result for binary data: %v", result)
	}

	// Verify binary data was preserved
	decompressedData, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read decompressed binary file: %v", err)
	}

	if len(decompressedData) != len(binaryData) {
		t.Errorf("Binary data length mismatch. Expected: %d, Got: %d", len(binaryData), len(decompressedData))
	}

	for i, b := range binaryData {
		if i >= len(decompressedData) || decompressedData[i] != b {
			t.Errorf("Binary data mismatch at index %d. Expected: %d, Got: %d", i, b, decompressedData[i])
			break
		}
	}
}

// Helper function to create a gzipped file with given content
func createGzipFile(filename, content string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	gzWriter := gzip.NewWriter(file)
	defer gzWriter.Close()

	_, err = gzWriter.Write([]byte(content))
	if err != nil {
		return err
	}

	return nil
}

func TestDecompressXZ_Success(t *testing.T) {
	tempDir := t.TempDir()

	content := "test content for XZ decompression\nLine 2\n"
	inputFile := filepath.Join(tempDir, "test.xz")
	outputFile := filepath.Join(tempDir, "test.txt")

	// Create XZ file using the xz library
	var buf bytes.Buffer
	xzWriter, err := xz.NewWriter(&buf)
	if err != nil {
		t.Fatalf("failed to create xz writer: %v", err)
	}
	if _, err := xzWriter.Write([]byte(content)); err != nil {
		t.Fatalf("failed to write xz content: %v", err)
	}
	if err := xzWriter.Close(); err != nil {
		t.Fatalf("failed to close xz writer: %v", err)
	}
	if err := os.WriteFile(inputFile, buf.Bytes(), 0644); err != nil {
		t.Fatalf("failed to write xz file: %v", err)
	}

	result, err := DecompressXZ(inputFile, outputFile)
	if err != nil {
		t.Fatalf("DecompressXZ failed: %v", err)
	}
	if len(result) != 1 || result[0] != outputFile {
		t.Errorf("unexpected result: %v", result)
	}
	data, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}
	if string(data) != content {
		t.Errorf("content mismatch: got %q, want %q", string(data), content)
	}
}

func TestDecompressXZ_NonExistentFile(t *testing.T) {
	tempDir := t.TempDir()
	_, err := DecompressXZ("/nonexistent/path/to/file.xz", filepath.Join(tempDir, "out.txt"))
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
	if !strings.Contains(err.Error(), "failed to open xz file") {
		t.Errorf("expected 'failed to open xz file' error, got: %v", err)
	}
}

func TestDecompressXZ_InvalidData(t *testing.T) {
	tempDir := t.TempDir()
	inputFile := filepath.Join(tempDir, "invalid.xz")
	outputFile := filepath.Join(tempDir, "out.txt")

	if err := os.WriteFile(inputFile, []byte("this is not valid xz data"), 0644); err != nil {
		t.Fatalf("failed to write invalid xz file: %v", err)
	}

	_, err := DecompressXZ(inputFile, outputFile)
	if err == nil {
		t.Fatal("expected error for invalid XZ data")
	}
	if !strings.Contains(err.Error(), "failed to create xz reader") {
		t.Errorf("expected 'failed to create xz reader' error, got: %v", err)
	}
}

func TestDecompress_DispatchesToXZ(t *testing.T) {
	tempDir := t.TempDir()
	inputFile := filepath.Join(tempDir, "data.xz")
	outputFile := filepath.Join(tempDir, "out.txt")

	// Create a valid XZ file
	var buf bytes.Buffer
	xzWriter, err := xz.NewWriter(&buf)
	if err != nil {
		t.Fatalf("failed to create xz writer: %v", err)
	}
	_, _ = xzWriter.Write([]byte("dispatch test"))
	_ = xzWriter.Close()
	if err := os.WriteFile(inputFile, buf.Bytes(), 0644); err != nil {
		t.Fatalf("failed to write xz file: %v", err)
	}

	// Decompress via the dispatcher (should call DecompressXZ for .xz extension)
	_, err = Decompress(inputFile, outputFile)
	if err != nil {
		t.Fatalf("Decompress .xz dispatch failed: %v", err)
	}
}
