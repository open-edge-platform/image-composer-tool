package compression_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/compression"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

func TestDecompressFile(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name            string
		decompressPath  string
		outputPath      string
		decompressType  string
		sudo            bool
		mockCommands    []shell.MockCommand
		expectError     bool
		expectedError   string
		expectedCommand string
	}{
		{
			name:           "tar_xz_without_sudo",
			decompressPath: "/tmp/test/archive.tar.xz",
			outputPath:     "/tmp/output",
			decompressType: "tar.xz",
			sudo:           false,
			mockCommands: []shell.MockCommand{
				{Pattern: "tar", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:           "tar_xz_with_sudo",
			decompressPath: "/tmp/test/archive.tar.xz",
			outputPath:     "/tmp/output",
			decompressType: "tar.xz",
			sudo:           true,
			mockCommands: []shell.MockCommand{
				{Pattern: "tar", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:           "tar_gz_without_sudo",
			decompressPath: "/tmp/test/archive.tar.gz",
			outputPath:     "/tmp/output",
			decompressType: "tar.gz",
			sudo:           false,
			mockCommands: []shell.MockCommand{
				{Pattern: "tar", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:           "tar_gz_with_sudo",
			decompressPath: "/tmp/test/archive.tar.gz",
			outputPath:     "/tmp/output",
			decompressType: "tar.gz",
			sudo:           true,
			mockCommands: []shell.MockCommand{
				{Pattern: "tar", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:           "gz_without_sudo",
			decompressPath: "/tmp/test/file.gz",
			outputPath:     "/tmp/output/file",
			decompressType: "gz",
			sudo:           false,
			mockCommands: []shell.MockCommand{
				{Pattern: "gzip -d -c /tmp/test/file.gz > /tmp/output/file", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:           "gz_with_sudo",
			decompressPath: "/tmp/test/file.gz",
			outputPath:     "/tmp/output/file",
			decompressType: "gz",
			sudo:           true,
			mockCommands: []shell.MockCommand{
				{Pattern: "gzip -d -c /tmp/test/file.gz > /tmp/output/file", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:           "xz_without_sudo",
			decompressPath: "/tmp/test/file.xz",
			outputPath:     "/tmp/output/file",
			decompressType: "xz",
			sudo:           false,
			mockCommands: []shell.MockCommand{
				{Pattern: "xz -d -c /tmp/test/file.xz > /tmp/output/file", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:           "xz_with_sudo",
			decompressPath: "/tmp/test/file.xz",
			outputPath:     "/tmp/output/file",
			decompressType: "xz",
			sudo:           true,
			mockCommands: []shell.MockCommand{
				{Pattern: "xz -d -c /tmp/test/file.xz > /tmp/output/file", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:           "zstd_without_sudo",
			decompressPath: "/tmp/test/file.zst",
			outputPath:     "/tmp/output/file",
			decompressType: "zstd",
			sudo:           false,
			mockCommands: []shell.MockCommand{
				{Pattern: "zstd -d -c /tmp/test/file.zst > /tmp/output/file", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:           "zstd_with_sudo",
			decompressPath: "/tmp/test/file.zst",
			outputPath:     "/tmp/output/file",
			decompressType: "zstd",
			sudo:           true,
			mockCommands: []shell.MockCommand{
				{Pattern: "zstd -d -c /tmp/test/file.zst > /tmp/output/file", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:           "unsupported_type",
			decompressPath: "/tmp/test/file.zip",
			outputPath:     "/tmp/output",
			decompressType: "zip",
			sudo:           false,
			mockCommands:   []shell.MockCommand{},
			expectError:    true,
			expectedError:  "unsupported decompression type: zip",
		},
		{
			name:           "tar_xz_command_failure",
			decompressPath: "/tmp/test/archive.tar.xz",
			outputPath:     "/tmp/output",
			decompressType: "tar.xz",
			sudo:           false,
			mockCommands: []shell.MockCommand{
				{Pattern: "tar", Output: "", Error: fmt.Errorf("tar command failed")},
			},
			expectError:   true,
			expectedError: "tar command failed",
		},
		{
			name:           "gz_command_failure",
			decompressPath: "/tmp/test/file.gz",
			outputPath:     "/tmp/output/file",
			decompressType: "gz",
			sudo:           false,
			mockCommands: []shell.MockCommand{
				{Pattern: "gzip -d -c /tmp/test/file.gz > /tmp/output/file", Output: "", Error: fmt.Errorf("gzip command failed")},
			},
			expectError:   true,
			expectedError: "gzip command failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			err := compression.DecompressFile(tt.decompressPath, tt.outputPath, tt.decompressType, tt.sudo)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, but got none")
				} else if tt.expectedError != "" && !strings.Contains(err.Error(), tt.expectedError) {
					t.Errorf("Expected error containing '%s', but got: %v", tt.expectedError, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				}
			}
		})
	}
}

func TestCompressFile(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name          string
		compressPath  string
		outputPath    string
		compressType  string
		sudo          bool
		mockCommands  []shell.MockCommand
		expectError   bool
		expectedError string
	}{
		{
			name:         "tar_xz_without_sudo",
			compressPath: "/tmp/test/file.txt",
			outputPath:   "/tmp/output/archive.tar.xz",
			compressType: "tar.xz",
			sudo:         false,
			mockCommands: []shell.MockCommand{
				{Pattern: "tar", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:         "tar_xz_with_sudo",
			compressPath: "/tmp/test/file.txt",
			outputPath:   "/tmp/output/archive.tar.xz",
			compressType: "tar.xz",
			sudo:         true,
			mockCommands: []shell.MockCommand{
				{Pattern: "tar", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:         "tar_gz_without_sudo",
			compressPath: "/tmp/test/file.txt",
			outputPath:   "/tmp/output/archive.tar.gz",
			compressType: "tar.gz",
			sudo:         false,
			mockCommands: []shell.MockCommand{
				{Pattern: "tar", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:         "tar_gz_with_sudo",
			compressPath: "/tmp/test/file.txt",
			outputPath:   "/tmp/output/archive.tar.gz",
			compressType: "tar.gz",
			sudo:         true,
			mockCommands: []shell.MockCommand{
				{Pattern: "tar", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:         "gz_without_sudo",
			compressPath: "/tmp/test/file.txt",
			outputPath:   "/tmp/output/file.gz",
			compressType: "gz",
			sudo:         false,
			mockCommands: []shell.MockCommand{
				{Pattern: "gzip -c /tmp/test/file.txt > /tmp/output/file.gz", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:         "gz_with_sudo",
			compressPath: "/tmp/test/file.txt",
			outputPath:   "/tmp/output/file.gz",
			compressType: "gz",
			sudo:         true,
			mockCommands: []shell.MockCommand{
				{Pattern: "gzip -c /tmp/test/file.txt > /tmp/output/file.gz", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:         "xz_without_sudo",
			compressPath: "/tmp/test/file.txt",
			outputPath:   "/tmp/output/file.xz",
			compressType: "xz",
			sudo:         false,
			mockCommands: []shell.MockCommand{
				{Pattern: "xz -z -c /tmp/test/file.txt > /tmp/output/file.xz", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:         "xz_with_sudo",
			compressPath: "/tmp/test/file.txt",
			outputPath:   "/tmp/output/file.xz",
			compressType: "xz",
			sudo:         true,
			mockCommands: []shell.MockCommand{
				{Pattern: "xz -z -c /tmp/test/file.txt > /tmp/output/file.xz", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:         "zstd_without_sudo",
			compressPath: "/tmp/test/file.txt",
			outputPath:   "/tmp/output/file.zst",
			compressType: "zstd",
			sudo:         false,
			mockCommands: []shell.MockCommand{
				{Pattern: "zstd --threads=0 -f -o /tmp/output/file.zst /tmp/test/file.txt", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:         "zstd_with_sudo",
			compressPath: "/tmp/test/file.txt",
			outputPath:   "/tmp/output/file.zst",
			compressType: "zstd",
			sudo:         true,
			mockCommands: []shell.MockCommand{
				{Pattern: "zstd --threads=0 -f -o /tmp/output/file.zst /tmp/test/file.txt", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:          "unsupported_type",
			compressPath:  "/tmp/test/file.txt",
			outputPath:    "/tmp/output/file.zip",
			compressType:  "zip",
			sudo:          false,
			mockCommands:  []shell.MockCommand{},
			expectError:   true,
			expectedError: "unsupported compression type: zip",
		},
		{
			name:         "tar_xz_command_failure",
			compressPath: "/tmp/test/file.txt",
			outputPath:   "/tmp/output/archive.tar.xz",
			compressType: "tar.xz",
			sudo:         false,
			mockCommands: []shell.MockCommand{
				{Pattern: "tar", Output: "", Error: fmt.Errorf("tar command failed")},
			},
			expectError:   true,
			expectedError: "tar command failed",
		},
		{
			name:         "gz_command_failure",
			compressPath: "/tmp/test/file.txt",
			outputPath:   "/tmp/output/file.gz",
			compressType: "gz",
			sudo:         false,
			mockCommands: []shell.MockCommand{
				{Pattern: "gzip -c /tmp/test/file.txt > /tmp/output/file.gz", Output: "", Error: fmt.Errorf("gzip command failed")},
			},
			expectError:   true,
			expectedError: "gzip command failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			err := compression.CompressFile(tt.compressPath, tt.outputPath, tt.compressType, tt.sudo)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, but got none")
				} else if tt.expectedError != "" && !strings.Contains(err.Error(), tt.expectedError) {
					t.Errorf("Expected error containing '%s', but got: %v", tt.expectedError, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				}
			}
		})
	}
}

func TestCompressFolder(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name          string
		compressPath  string
		outputPath    string
		compressType  string
		sudo          bool
		mockCommands  []shell.MockCommand
		expectError   bool
		expectedError string
	}{
		{
			name:         "tar_xz_without_sudo",
			compressPath: "/tmp/test/folder",
			outputPath:   "/tmp/output/archive.tar.xz",
			compressType: "tar.xz",
			sudo:         false,
			mockCommands: []shell.MockCommand{
				{Pattern: "tar -cJf /tmp/output/archive.tar.xz -C /tmp/test/folder .", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:         "tar_xz_with_sudo",
			compressPath: "/tmp/test/folder",
			outputPath:   "/tmp/output/archive.tar.xz",
			compressType: "tar.xz",
			sudo:         true,
			mockCommands: []shell.MockCommand{
				{Pattern: "tar -cJf /tmp/output/archive.tar.xz -C /tmp/test/folder .", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:         "tar_gz_without_sudo",
			compressPath: "/tmp/test/folder",
			outputPath:   "/tmp/output/archive.tar.gz",
			compressType: "tar.gz",
			sudo:         false,
			mockCommands: []shell.MockCommand{
				{Pattern: "tar -czf /tmp/output/archive.tar.gz -C /tmp/test/folder .", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:         "tar_gz_with_sudo",
			compressPath: "/tmp/test/folder",
			outputPath:   "/tmp/output/archive.tar.gz",
			compressType: "tar.gz",
			sudo:         true,
			mockCommands: []shell.MockCommand{
				{Pattern: "tar -czf /tmp/output/archive.tar.gz -C /tmp/test/folder .", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:          "unsupported_type",
			compressPath:  "/tmp/test/folder",
			outputPath:    "/tmp/output/archive.zip",
			compressType:  "zip",
			sudo:          false,
			mockCommands:  []shell.MockCommand{},
			expectError:   true,
			expectedError: "unsupported compression type: zip",
		},
		{
			name:         "tar_xz_command_failure",
			compressPath: "/tmp/test/folder",
			outputPath:   "/tmp/output/archive.tar.xz",
			compressType: "tar.xz",
			sudo:         false,
			mockCommands: []shell.MockCommand{
				{Pattern: "tar -cJf /tmp/output/archive.tar.xz -C /tmp/test/folder .", Output: "", Error: fmt.Errorf("tar command failed")},
			},
			expectError:   true,
			expectedError: "tar command failed",
		},
		{
			name:         "tar_gz_command_failure",
			compressPath: "/tmp/test/folder",
			outputPath:   "/tmp/output/archive.tar.gz",
			compressType: "tar.gz",
			sudo:         false,
			mockCommands: []shell.MockCommand{
				{Pattern: "tar -czf /tmp/output/archive.tar.gz -C /tmp/test/folder .", Output: "", Error: fmt.Errorf("tar command failed")},
			},
			expectError:   true,
			expectedError: "tar command failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			err := compression.CompressFolder(tt.compressPath, tt.outputPath, tt.compressType, tt.sudo)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, but got none")
				} else if tt.expectedError != "" && !strings.Contains(err.Error(), tt.expectedError) {
					t.Errorf("Expected error containing '%s', but got: %v", tt.expectedError, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				}
			}
		})
	}
}

func TestDecompressFile_PathHandling(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name           string
		decompressPath string
		outputPath     string
		decompressType string
		sudo           bool
		mockCommands   []shell.MockCommand
		expectError    bool
	}{
		{
			name:           "path_with_spaces",
			decompressPath: "/tmp/test folder/archive.tar.xz",
			outputPath:     "/tmp/output folder",
			decompressType: "tar.xz",
			sudo:           false,
			mockCommands: []shell.MockCommand{
				{Pattern: "tar", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:           "nested_directory_path",
			decompressPath: "/tmp/deep/nested/path/archive.tar.gz",
			outputPath:     "/tmp/deep/output/path",
			decompressType: "tar.gz",
			sudo:           true,
			mockCommands: []shell.MockCommand{
				{Pattern: "tar", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:           "root_directory_file",
			decompressPath: "/archive.tar.xz",
			outputPath:     "/output",
			decompressType: "tar.xz",
			sudo:           true,
			mockCommands: []shell.MockCommand{
				{Pattern: "tar", Output: "", Error: nil},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			err := compression.DecompressFile(tt.decompressPath, tt.outputPath, tt.decompressType, tt.sudo)

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

func TestCompressFile_PathHandling(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name         string
		compressPath string
		outputPath   string
		compressType string
		sudo         bool
		mockCommands []shell.MockCommand
		expectError  bool
	}{
		{
			name:         "path_with_spaces",
			compressPath: "/tmp/test folder/file.txt",
			outputPath:   "/tmp/output folder/archive.tar.gz",
			compressType: "tar.gz",
			sudo:         false,
			mockCommands: []shell.MockCommand{
				{Pattern: "tar", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name:         "nested_directory_path",
			compressPath: "/tmp/deep/nested/path/file.txt",
			outputPath:   "/tmp/deep/output/path/archive.tar.xz",
			compressType: "tar.xz",
			sudo:         true,
			mockCommands: []shell.MockCommand{
				{Pattern: "tar", Output: "", Error: nil},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			err := compression.CompressFile(tt.compressPath, tt.outputPath, tt.compressType, tt.sudo)

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

func TestSudoStringGeneration(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name           string
		sudo           bool
		expectedPrefix string
		functionType   string
	}{
		{
			name:           "sudo_enabled_tar",
			sudo:           true,
			expectedPrefix: "sudo",
			functionType:   "tar",
		},
		{
			name:           "sudo_disabled_tar",
			sudo:           false,
			expectedPrefix: "",
			functionType:   "tar",
		},
		{
			name:           "sudo_enabled_single",
			sudo:           true,
			expectedPrefix: "with_sudo",
			functionType:   "single",
		},
		{
			name:           "sudo_disabled_single",
			sudo:           false,
			expectedPrefix: "without_sudo",
			functionType:   "single",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test tar operations
			if tt.functionType == "tar" {
				mockCommands := []shell.MockCommand{
					{Pattern: fmt.Sprintf("cd /tmp && %s tar", strings.TrimSpace(tt.expectedPrefix)), Output: "", Error: nil},
				}
				shell.Default = shell.NewMockExecutor(mockCommands)

				err := compression.DecompressFile("/tmp/test.tar.xz", "/tmp/output", "tar.xz", tt.sudo)
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				}
			}

			// Test single file operations (gz/xz) - these use the sudo parameter differently
			if tt.functionType == "single" {
				var expectedPattern string
				if tt.sudo {
					expectedPattern = "gzip -d -c /tmp/test.gz > /tmp/output"
				} else {
					expectedPattern = "gzip -d -c /tmp/test.gz > /tmp/output"
				}

				mockCommands := []shell.MockCommand{
					{Pattern: expectedPattern, Output: "", Error: nil},
				}
				shell.Default = shell.NewMockExecutor(mockCommands)

				err := compression.DecompressFile("/tmp/test.gz", "/tmp/output", "gz", tt.sudo)
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				}
			}
		})
	}
}

func TestCompression_EdgeCases(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name          string
		function      string
		args          []interface{}
		mockCommands  []shell.MockCommand
		expectError   bool
		expectedError string
	}{
		{
			name:     "empty_paths_decompress",
			function: "DecompressFile",
			args:     []interface{}{"", "", "tar.xz", false},
			mockCommands: []shell.MockCommand{
				{Pattern: "tar", Output: "", Error: fmt.Errorf("invalid path")},
			},
			expectError:   true,
			expectedError: "invalid path",
		},
		{
			name:     "empty_paths_compress",
			function: "CompressFile",
			args:     []interface{}{"", "", "tar.xz", false},
			mockCommands: []shell.MockCommand{
				{Pattern: "tar", Output: "", Error: fmt.Errorf("invalid path")},
			},
			expectError:   true,
			expectedError: "invalid path",
		},
		{
			name:     "empty_paths_compress_folder",
			function: "CompressFolder",
			args:     []interface{}{"", "", "tar.xz", false},
			mockCommands: []shell.MockCommand{
				{Pattern: "tar", Output: "", Error: fmt.Errorf("invalid path")},
			},
			expectError:   true,
			expectedError: "invalid path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			var err error
			switch tt.function {
			case "DecompressFile":
				err = compression.DecompressFile(
					tt.args[0].(string),
					tt.args[1].(string),
					tt.args[2].(string),
					tt.args[3].(bool),
				)
			case "CompressFile":
				err = compression.CompressFile(
					tt.args[0].(string),
					tt.args[1].(string),
					tt.args[2].(string),
					tt.args[3].(bool),
				)
			case "CompressFolder":
				err = compression.CompressFolder(
					tt.args[0].(string),
					tt.args[1].(string),
					tt.args[2].(string),
					tt.args[3].(bool),
				)
			}

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, but got none")
				} else if tt.expectedError != "" && !strings.Contains(err.Error(), tt.expectedError) {
					t.Errorf("Expected error containing '%s', but got: %v", tt.expectedError, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				}
			}
		})
	}
}

func TestAllCompressionTypes(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	compressionTypes := []string{"tar.xz", "tar.gz", "gz", "xz"}

	for _, compType := range compressionTypes {
		t.Run(fmt.Sprintf("decompress_%s", compType), func(t *testing.T) {
			var expectedPattern string
			switch compType {
			case "tar.xz":
				expectedPattern = "tar -xJf"
			case "tar.gz":
				expectedPattern = "tar -xzf"
			case "gz":
				expectedPattern = "gzip -d"
			case "xz":
				expectedPattern = "xz -d"
			}

			mockCommands := []shell.MockCommand{
				{Pattern: expectedPattern, Output: "", Error: nil},
			}
			shell.Default = shell.NewMockExecutor(mockCommands)

			testFile := fmt.Sprintf("/tmp/test.%s", compType)
			err := compression.DecompressFile(testFile, "/output", compType, false)

			if err != nil {
				t.Errorf("Expected no error for %s decompression, but got: %v", compType, err)
			}
		})

		t.Run(fmt.Sprintf("compress_%s", compType), func(t *testing.T) {
			var expectedPattern string
			switch compType {
			case "tar.xz":
				expectedPattern = "tar -cJf"
			case "tar.gz":
				expectedPattern = "tar -czf"
			case "gz":
				expectedPattern = "gzip -c"
			case "xz":
				expectedPattern = "xz -z"
			}

			mockCommands := []shell.MockCommand{
				{Pattern: expectedPattern, Output: "", Error: nil},
			}
			shell.Default = shell.NewMockExecutor(mockCommands)

			testFile := "/tmp/test"
			outputFile := fmt.Sprintf("/output.%s", compType)
			err := compression.CompressFile(testFile, outputFile, compType, false)

			if err != nil {
				t.Errorf("Expected no error for %s compression, but got: %v", compType, err)
			}
		})
	}

	// Test folder compression (only supports tar.xz and tar.gz)
	folderTypes := []string{"tar.xz", "tar.gz"}
	for _, compType := range folderTypes {
		t.Run(fmt.Sprintf("compress_folder_%s", compType), func(t *testing.T) {
			var expectedPattern string
			switch compType {
			case "tar.xz":
				expectedPattern = "tar -cJf /output.tar.xz -C /tmp/folder ."
			case "tar.gz":
				expectedPattern = "tar -czf /output.tar.gz -C /tmp/folder ."
			}

			mockCommands := []shell.MockCommand{
				{Pattern: expectedPattern, Output: "", Error: nil},
			}
			shell.Default = shell.NewMockExecutor(mockCommands)

			outputFile := fmt.Sprintf("/output.%s", compType)
			err := compression.CompressFolder("/tmp/folder", outputFile, compType, false)

			if err != nil {
				t.Errorf("Expected no error for %s folder compression, but got: %v", compType, err)
			}
		})
	}
}
