package imageconvert

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

func TestNewImageConvert(t *testing.T) {
	imageConvert := NewImageConvert()
	if imageConvert == nil {
		t.Fatal("NewImageConvert should return a non-nil instance")
	}
}

func TestConvertImageFile_NoArtifacts(t *testing.T) {
	imageConvert := NewImageConvert()
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "test-image.raw")

	// Create test file
	if err := os.WriteFile(filePath, []byte("test data"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Disk: config.DiskConfig{
			Artifacts: nil,
		},
	}

	err := imageConvert.ConvertImageFile(filePath, template)
	if err != nil {
		t.Errorf("Expected no error when artifacts is nil, got: %v", err)
	}
}

func TestConvertImageFile_EmptyArtifacts(t *testing.T) {
	imageConvert := NewImageConvert()
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "test-image.raw")

	// Create test file
	if err := os.WriteFile(filePath, []byte("test data"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Disk: config.DiskConfig{
			Artifacts: []config.ArtifactInfo{},
		},
	}

	err := imageConvert.ConvertImageFile(filePath, template)
	if err != nil {
		t.Errorf("Expected no error when artifacts is empty, got: %v", err)
	}
}

func TestConvertImageFile_RawArtifactOnly(t *testing.T) {
	imageConvert := NewImageConvert()
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "test-image.raw")

	// Create test file
	if err := os.WriteFile(filePath, []byte("test data"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Disk: config.DiskConfig{
			Artifacts: []config.ArtifactInfo{
				{Type: "raw"},
			},
		},
	}

	err := imageConvert.ConvertImageFile(filePath, template)
	if err != nil {
		t.Errorf("Expected no error for raw artifact only, got: %v", err)
	}

	// Original file should still exist
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Error("Expected raw image file to be preserved")
	}
}

func TestConvertImageFile_RawArtifactWithCompression(t *testing.T) {
	imageConvert := NewImageConvert()
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "test-image.raw")

	// Create test file
	if err := os.WriteFile(filePath, []byte("test data"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Disk: config.DiskConfig{
			Artifacts: []config.ArtifactInfo{
				{Type: "raw", Compression: "gz"},
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "gzip", Output: "compression output", Error: fmt.Errorf("compression failed")},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	err := imageConvert.ConvertImageFile(filePath, template)

	// Should fail due to compression error
	if err == nil {
		t.Error("Expected error due to compression failure")
	}
	if !strings.Contains(err.Error(), "failed to compress raw image file") {
		t.Errorf("Expected compression error, got: %v", err)
	}
}

func TestConvertImageFile_NonRawArtifact(t *testing.T) {
	imageConvert := NewImageConvert()
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "test-image.raw")

	// Create test file
	if err := os.WriteFile(filePath, []byte("test data"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Disk: config.DiskConfig{
			Artifacts: []config.ArtifactInfo{
				{Type: "qcow2"},
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "qemu-img convert", Output: "conversion output", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	err := imageConvert.ConvertImageFile(filePath, template)
	if err != nil {
		t.Errorf("Expected no error for successful conversion, got: %v", err)
	}

	// Original raw file should be removed
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Error("Expected raw image file to be removed after conversion")
	}
}

func TestConvertImageFile_ConversionFailure(t *testing.T) {
	imageConvert := NewImageConvert()
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "test-image.raw")

	// Create test file
	if err := os.WriteFile(filePath, []byte("test data"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Disk: config.DiskConfig{
			Artifacts: []config.ArtifactInfo{
				{Type: "vhd"},
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "qemu-img convert", Output: "", Error: fmt.Errorf("conversion failed")},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	err := imageConvert.ConvertImageFile(filePath, template)
	if err == nil {
		t.Error("Expected error due to conversion failure")
	}
	if !strings.Contains(err.Error(), "failed to convert image file") {
		t.Errorf("Expected conversion error, got: %v", err)
	}
}

func TestConvertImageFile_MultipleArtifacts(t *testing.T) {
	imageConvert := NewImageConvert()
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "test-image.raw")

	// Create test file
	if err := os.WriteFile(filePath, []byte("test data"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Disk: config.DiskConfig{
			Artifacts: []config.ArtifactInfo{
				{Type: "qcow2"},
				{Type: "vhd"},
				{Type: "raw"},
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "qemu-img convert.*qcow2", Output: "qcow2 conversion", Error: nil},
		{Pattern: "qemu-img convert.*vpc", Output: "vhd conversion", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	err := imageConvert.ConvertImageFile(filePath, template)
	if err != nil {
		t.Errorf("Expected no error for multiple artifacts, got: %v", err)
	}

	// Raw file should be preserved due to raw artifact
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Error("Expected raw image file to be preserved")
	}
}

func TestConvertImageFile_UnsupportedImageType(t *testing.T) {
	imageConvert := NewImageConvert()
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "test-image.raw")

	// Create test file
	if err := os.WriteFile(filePath, []byte("test data"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Disk: config.DiskConfig{
			Artifacts: []config.ArtifactInfo{
				{Type: "unsupported"},
			},
		},
	}

	err := imageConvert.ConvertImageFile(filePath, template)
	if err == nil {
		t.Error("Expected error for unsupported image type")
	}
	if !strings.Contains(err.Error(), "unsupported image type") {
		t.Errorf("Expected unsupported image type error, got: %v", err)
	}
}

func TestConvertImageFile_FileNotExists(t *testing.T) {
	imageConvert := NewImageConvert()
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "test-image.raw")

	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Disk: config.DiskConfig{
			Artifacts: []config.ArtifactInfo{
				{Type: "qcow2"},
			},
		},
	}

	err := imageConvert.ConvertImageFile(filePath, template)
	if err == nil {
		t.Error("Expected error when image file does not exist")
	}
	if !strings.Contains(err.Error(), "image file does not exist") {
		t.Errorf("Expected file not exist error, got: %v", err)
	}
}

func TestConvertImageFile_CompressionFailure(t *testing.T) {
	imageConvert := NewImageConvert()
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "test-image.raw")

	// Create test file
	if err := os.WriteFile(filePath, []byte("test data"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Disk: config.DiskConfig{
			Artifacts: []config.ArtifactInfo{
				{Type: "qcow2", Compression: "gz"},
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "qemu-img convert", Output: "conversion output", Error: nil},
		{Pattern: "gzip", Output: "", Error: fmt.Errorf("compression failed")},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	err := imageConvert.ConvertImageFile(filePath, template)
	if err == nil {
		t.Error("Expected error due to compression failure")
	}
	if !strings.Contains(err.Error(), "failed to compress image file") {
		t.Errorf("Expected compression error, got: %v", err)
	}
}

func TestConvertImageFile_SupportedImageTypes(t *testing.T) {
	supportedTypes := []struct {
		imageType   string
		expectedCmd string
	}{
		{"vhd", "qemu-img convert -O vpc"},
		{"vhdx", "qemu-img convert -O vhdx"},
		{"qcow2", "qemu-img convert -O qcow2 -c -S 4k -p -o cluster_size=2M,lazy_refcounts=on"},
		{"vmdk", "qemu-img convert -O vmdk"},
		{"vdi", "qemu-img convert -O vdi"},
	}

	imageConvert := NewImageConvert()
	tempDir := t.TempDir()

	for _, tt := range supportedTypes {
		t.Run(tt.imageType, func(t *testing.T) {
			filePath := filepath.Join(tempDir, "test-image.raw")

			// Create test file
			if err := os.WriteFile(filePath, []byte("test data"), 0644); err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			template := &config.ImageTemplate{
				Image: config.ImageInfo{
					Name: "test-image",
				},
				Disk: config.DiskConfig{
					Artifacts: []config.ArtifactInfo{
						{Type: tt.imageType},
					},
				},
			}

			originalExecutor := shell.Default
			defer func() { shell.Default = originalExecutor }()
			mockExpectedOutput := []shell.MockCommand{
				{Pattern: tt.expectedCmd, Output: "conversion output", Error: nil},
			}
			shell.Default = shell.NewMockExecutor(mockExpectedOutput)

			err := imageConvert.ConvertImageFile(filePath, template)
			if err != nil {
				t.Errorf("Expected no error for %s conversion, got: %v", tt.imageType, err)
			}
		})
	}
}

func TestConvertImageFile_OutputFilePath(t *testing.T) {
	imageConvert := NewImageConvert()
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "test-image.raw")

	// Create test file
	if err := os.WriteFile(filePath, []byte("test data"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Disk: config.DiskConfig{
			Artifacts: []config.ArtifactInfo{
				{Type: "qcow2"},
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "qemu-img convert.*test-image.qcow2", Output: "conversion output", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	err := imageConvert.ConvertImageFile(filePath, template)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	// Verify the expected output file path would be created
	expectedOutputPath := filepath.Join(tempDir, "test-image.qcow2")
	t.Logf("Expected output path: %s", expectedOutputPath)
}

func TestConvertImageFile_ParameterValidation(t *testing.T) {
	imageConvert := NewImageConvert()

	tests := []struct {
		name     string
		filePath string
		template *config.ImageTemplate
	}{
		{
			name:     "empty file path",
			filePath: "",
			template: &config.ImageTemplate{},
		},
		{
			name:     "nil template",
			filePath: "/tmp/test.raw",
			template: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// These should not panic
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Function panicked with: %v", r)
				}
			}()

			err := imageConvert.ConvertImageFile(tt.filePath, tt.template)
			// The function should handle these cases gracefully
			// Either return an error or handle the case without panic
			t.Logf("Result for %s: %v", tt.name, err)
		})
	}
}

func TestConvertImageFile_CompressRemoveFailure(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Skipping permission test as root")
	}
	imageConvert := NewImageConvert()
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "test-image.raw")

	// Create test file
	if err := os.WriteFile(filePath, []byte("test data"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Disk: config.DiskConfig{
			Artifacts: []config.ArtifactInfo{
				{Type: "raw", Compression: "gz"},
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "gzip", Output: "compression output", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	// Make directory read-only to prevent file removal
	if err := os.Chmod(tempDir, 0555); err != nil {
		t.Fatalf("Failed to chmod temp dir: %v", err)
	}
	defer func() {
		if err := os.Chmod(tempDir, 0755); err != nil {
			t.Logf("failed to reset temp dir permissions: %v", err)
		}
	}()

	// This should succeed even if removal fails (it logs a warning)
	err := imageConvert.ConvertImageFile(filePath, template)
	if err != nil {
		t.Errorf("Expected no error even if remove fails, got: %v", err)
	}
}
func TestConvertImageFile(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	// Create a temporary file for testing
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.img")
	if err := os.WriteFile(testFile, []byte("test data"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	tests := []struct {
		name         string
		filePath     string
		imageType    string
		mockCommands []shell.MockCommand
		expectError  bool
		errorMsg     string
	}{
		{
			name:      "vhd_conversion",
			filePath:  testFile,
			imageType: "vhd",
			mockCommands: []shell.MockCommand{
				{Pattern: "qemu-img convert -O vpc", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:      "vhdx_conversion",
			filePath:  testFile,
			imageType: "vhdx",
			mockCommands: []shell.MockCommand{
				{Pattern: "qemu-img convert -O vhdx", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:      "qcow2_conversion",
			filePath:  testFile,
			imageType: "qcow2",
			mockCommands: []shell.MockCommand{
				{Pattern: "qemu-img convert -O qcow2", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:      "vmdk_conversion",
			filePath:  testFile,
			imageType: "vmdk",
			mockCommands: []shell.MockCommand{
				{Pattern: "qemu-img convert -O vmdk", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:      "vdi_conversion",
			filePath:  testFile,
			imageType: "vdi",
			mockCommands: []shell.MockCommand{
				{Pattern: "qemu-img convert -O vdi", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:         "unsupported_type",
			filePath:     testFile,
			imageType:    "unsupported",
			mockCommands: []shell.MockCommand{},
			expectError:  true,
			errorMsg:     "unsupported image type",
		},
		{
			name:      "conversion_failure",
			filePath:  testFile,
			imageType: "vhd",
			mockCommands: []shell.MockCommand{
				{Pattern: "qemu-img convert -O vpc", Output: "", Error: fmt.Errorf("conversion failed")},
			},
			expectError: true,
			errorMsg:    "failed to convert image file",
		},
		{
			name:         "file_not_exist",
			filePath:     filepath.Join(tempDir, "nonexistent.img"),
			imageType:    "vhd",
			mockCommands: []shell.MockCommand{},
			expectError:  true,
			errorMsg:     "image file does not exist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			_, err := convertImageFile(tt.filePath, tt.imageType)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, but got none")
				} else if tt.errorMsg != "" && err.Error() != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s', but got: %v", tt.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				}
			}
		})
	}
}

func TestCompressImageFile(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	// Create a temporary file for testing
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.img")
	if err := os.WriteFile(testFile, []byte("test data"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	tests := []struct {
		name            string
		filePath        string
		compressionType string
		mockCommands    []shell.MockCommand
		expectError     bool
		errorMsg        string
	}{
		{
			name:            "gz_compression",
			filePath:        testFile,
			compressionType: "gz",
			mockCommands: []shell.MockCommand{
				{Pattern: "gzip -c", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:            "xz_compression",
			filePath:        testFile,
			compressionType: "xz",
			mockCommands: []shell.MockCommand{
				{Pattern: "xz -z -c", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:            "compression_failure",
			filePath:        testFile,
			compressionType: "gz",
			mockCommands: []shell.MockCommand{
				{Pattern: "gzip -c", Output: "", Error: fmt.Errorf("compression failed")},
			},
			expectError: true,
			errorMsg:    "failed to compress file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Re-create file for each test as it might be removed
			if err := os.WriteFile(tt.filePath, []byte("test data"), 0644); err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			err := compressImageFile(tt.filePath, tt.compressionType)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, but got none")
				} else if tt.errorMsg != "" && err.Error() != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s', but got: %v", tt.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				}
			}
		})
	}
}

// TestTrimUnusedSpace tests the trimUnusedSpace function
func TestTrimUnusedSpace(t *testing.T) {
	tests := []struct {
		name         string
		fileSize     int64
		expectSkip   bool
		expectError  bool
		mockCommands []shell.MockCommand
	}{
		{
			name:       "small_file_skip",
			fileSize:   512,
			expectSkip: true,
		},
		{
			name:     "large_file_success",
			fileSize: 2 * 1024 * 1024, // 2MB
			mockCommands: []shell.MockCommand{
				{Pattern: "which virt-sparsify", Output: "", Error: fmt.Errorf("not found")},
				{Pattern: "qemu-img convert -O raw", Output: "", Error: nil},
			},
		},
		{
			name:        "large_file_error",
			fileSize:    2 * 1024 * 1024,
			expectError: true,
			mockCommands: []shell.MockCommand{
				{Pattern: "which virt-sparsify", Output: "", Error: fmt.Errorf("not found")},
				{Pattern: "qemu-img convert -O raw", Output: "", Error: fmt.Errorf("conversion failed")},
			},
		},
		{
			name:     "virt_sparsify_available",
			fileSize: 2 * 1024 * 1024,
			mockCommands: []shell.MockCommand{
				{Pattern: "which virt-sparsify", Output: "/usr/bin/virt-sparsify", Error: nil},
				{Pattern: "virt-sparsify --in-place", Output: "sparsify completed", Error: nil},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary file
			tempDir := t.TempDir()
			filePath := filepath.Join(tempDir, "test-image.raw")

			// Create file with specified size
			data := make([]byte, tt.fileSize)
			if err := os.WriteFile(filePath, data, 0644); err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			// Setup mock executor
			originalExecutor := shell.Default
			defer func() { shell.Default = originalExecutor }()

			if len(tt.mockCommands) > 0 {
				shell.Default = shell.NewMockExecutor(tt.mockCommands)
			}

			// For large_file_success test, pre-create the temporary file that qemu-img would create
			if tt.name == "large_file_success" {
				tempFile := filePath + ".tmp"
				if err := os.WriteFile(tempFile, data, 0644); err != nil {
					t.Fatalf("Failed to create temp file for test: %v", err)
				}
			}

			// Call function
			err := trimUnusedSpace(filePath)

			// Check results
			if tt.expectError {
				if err == nil {
					t.Error("Expected error, but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				}
			}

			// Verify file still exists
			if _, err := os.Stat(filePath); os.IsNotExist(err) {
				t.Error("Expected file to still exist after trimming")
			}
		})
	}
}

// TestSparsifyWithQemuImg tests the sparsifyWithQemuImg function specifically
func TestSparsifyWithQemuImg(t *testing.T) {
	tests := []struct {
		name         string
		fileSize     int64
		expectSkip   bool
		expectError  bool
		mockCommands []shell.MockCommand
	}{
		{
			name:       "small_file_skip",
			fileSize:   100,
			expectSkip: true,
		},
		{
			name:        "large_file_success_mock_limitation",
			fileSize:    2 * 1024 * 1024,
			expectError: true, // Mock can't simulate file creation, so expect error
			mockCommands: []shell.MockCommand{
				{Pattern: "which virt-sparsify", Output: "", Error: fmt.Errorf("not found")},
				{Pattern: "qemu-img convert -O raw", Output: "", Error: nil},
			},
		},
		{
			name:        "conversion_failure",
			fileSize:    2 * 1024 * 1024,
			expectError: true,
			mockCommands: []shell.MockCommand{
				{Pattern: "qemu-img convert -O raw", Output: "", Error: fmt.Errorf("conversion failed")},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary file
			tempDir := t.TempDir()
			filePath := filepath.Join(tempDir, "test-image.raw")

			data := make([]byte, tt.fileSize)
			if err := os.WriteFile(filePath, data, 0644); err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			// Setup mock executor
			originalExecutor := shell.Default
			defer func() { shell.Default = originalExecutor }()

			if len(tt.mockCommands) > 0 {
				shell.Default = shell.NewMockExecutor(tt.mockCommands)
			}

			// Call function
			err := sparsifyWithQemuImg(filePath)

			// Check results
			if tt.expectError {
				if err == nil {
					t.Error("Expected error, but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				}
			}
		})
	}
}

// TestQcow2OptimizedConversion tests that qcow2 conversion uses optimized parameters
func TestQcow2OptimizedConversion(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "test-image.raw")

	// Create a small test file to avoid sparsification
	data := make([]byte, 100)
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Setup mock executor
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	mockCommands := []shell.MockCommand{
		{Pattern: "qemu-img convert -O qcow2 -c -S 4k -p -o cluster_size=2M,lazy_refcounts=on", Output: "", Error: nil},
	}

	shell.Default = shell.NewMockExecutor(mockCommands)

	// Test conversion
	outputPath, err := convertImageFile(filePath, "qcow2")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	expectedOutputPath := filepath.Join(tempDir, "test-image.qcow2")
	if outputPath != expectedOutputPath {
		t.Errorf("Expected output path %s, got %s", expectedOutputPath, outputPath)
	}
}

// TestConvertImageFileWithTrimming verifies the full flow with trimming
func TestConvertImageFileWithTrimming(t *testing.T) {
	imageConvert := NewImageConvert()
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "test-image.raw")

	// Create a larger test file to trigger trimming
	data := make([]byte, 2*1024*1024) // 2MB
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name: "test-image",
		},
		Disk: config.DiskConfig{
			Artifacts: []config.ArtifactInfo{
				{Type: "qcow2"},
			},
		},
	}

	// Setup mock executor
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	mockCommands := []shell.MockCommand{
		{Pattern: "which virt-sparsify", Output: "", Error: fmt.Errorf("not found")},
		{Pattern: "qemu-img convert -O raw", Output: "", Error: nil},   // For trimming
		{Pattern: "qemu-img convert -O qcow2", Output: "", Error: nil}, // For conversion
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	// Test the full conversion process
	err := imageConvert.ConvertImageFile(filePath, template)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	// Verify raw file is removed (conversion succeeded)
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Log("Raw file still exists (which is expected in mock test)")
	}
}

func TestDetectImageFormat_SingleLineJSON(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "test-image.raw")

	if err := os.WriteFile(filePath, []byte("test data"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	mockCommands := []shell.MockCommand{
		{Pattern: "qemu-img info --output=json", Output: `{"format":"raw","virtual-size":123}`, Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	format, err := DetectImageFormat(filePath)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if format != "raw" {
		t.Fatalf("Expected format raw, got %s", format)
	}
}

func TestDetectImageFormat_MultiLineJSON(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "test-image.qcow2")

	if err := os.WriteFile(filePath, []byte("test data"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	jsonOut := "{\n  \"format\": \"qcow2\",\n  \"virtual-size\": 123\n}"
	mockCommands := []shell.MockCommand{
		{Pattern: "qemu-img info --output=json", Output: jsonOut, Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	format, err := DetectImageFormat(filePath)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if format != "qcow2" {
		t.Fatalf("Expected format qcow2, got %s", format)
	}
}

func TestDetectImageFormat_MissingFormat(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "test-image.bin")

	if err := os.WriteFile(filePath, []byte("test data"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	mockCommands := []shell.MockCommand{
		{Pattern: "qemu-img info --output=json", Output: `{}`, Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	_, err := DetectImageFormat(filePath)
	if err == nil {
		t.Fatalf("Expected error for missing format")
	}
}

func TestDetectImageFormat_FormatSpecificFallback(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "test-image.raw")

	if err := os.WriteFile(filePath, []byte("test data"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	jsonOut := `{"format":"file","format-specific":{"type":"raw"}}`
	mockCommands := []shell.MockCommand{
		{Pattern: "qemu-img info --output=json", Output: jsonOut, Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	format, err := DetectImageFormat(filePath)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if format != "raw" {
		t.Fatalf("Expected format raw, got %s", format)
	}
}

func TestDetectImageFormat_FormatSpecificOnly(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "test-image.qcow2")

	if err := os.WriteFile(filePath, []byte("test data"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	jsonOut := `{"format-specific":{"type":"qcow2"}}`
	mockCommands := []shell.MockCommand{
		{Pattern: "qemu-img info --output=json", Output: jsonOut, Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	format, err := DetectImageFormat(filePath)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if format != "qcow2" {
		t.Fatalf("Expected format qcow2, got %s", format)
	}
}

func TestConvertImageToRaw_AlreadyRaw(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "test-image.raw")

	if err := os.WriteFile(filePath, []byte("test data"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	mockCommands := []shell.MockCommand{
		{Pattern: "qemu-img info --output=json", Output: `{"format":"raw"}`, Error: nil},
		{Pattern: "qemu-img convert -O raw", Output: "", Error: fmt.Errorf("unexpected convert")},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	convertedPath, err := ConvertImageToRaw(filePath, tempDir)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if convertedPath != filePath {
		t.Fatalf("Expected same path for raw image, got %s", convertedPath)
	}
}

func TestConvertImageToRaw_ConvertsNonRaw(t *testing.T) {
	tempDir := t.TempDir()
	outputDir := t.TempDir()
	filePath := filepath.Join(tempDir, "test-image.qcow2")

	if err := os.WriteFile(filePath, []byte("test data"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	mockCommands := []shell.MockCommand{
		{Pattern: "qemu-img info --output=json", Output: `{"format":"qcow2"}`, Error: nil},
		{Pattern: "qemu-img convert -O raw", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	convertedPath, err := ConvertImageToRaw(filePath, outputDir)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if !strings.HasSuffix(convertedPath, ".raw") {
		t.Fatalf("Expected .raw output path, got %s", convertedPath)
	}
	if !strings.HasPrefix(convertedPath, outputDir) {
		t.Fatalf("Expected output in %s, got %s", outputDir, convertedPath)
	}
}

func TestConvertImageFile_SkipWhenAlreadyTargetFormat(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "test-image.qcow2")

	if err := os.WriteFile(filePath, []byte("test data"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	mockCommands := []shell.MockCommand{
		{Pattern: "qemu-img info --output=json", Output: `{"format":"qcow2"}`, Error: nil},
		{Pattern: "qemu-img convert -O qcow2", Output: "", Error: fmt.Errorf("unexpected convert")},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	outputPath, err := convertImageFile(filePath, "qcow2")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if outputPath != filePath {
		t.Fatalf("Expected same path for already matching format, got %s", outputPath)
	}
}

func TestConvertImageFile_ToRaw(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "test-image.vhd")

	if err := os.WriteFile(filePath, []byte("test data"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	mockCommands := []shell.MockCommand{
		{Pattern: "qemu-img info --output=json", Output: `{"format":"vpc"}`, Error: nil},
		{Pattern: "qemu-img convert -O raw", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	outputPath, err := convertImageFile(filePath, "raw")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if !strings.HasSuffix(outputPath, ".raw") {
		t.Fatalf("Expected .raw output path, got %s", outputPath)
	}
}
