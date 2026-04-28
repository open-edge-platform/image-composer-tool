package isomaker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

func TestValidateAdditionalFiles(t *testing.T) {
	// Create a temp directory with a fake binary for the "exists" test cases
	tempDir, err := os.MkdirTemp("", "validate-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	existingFile := filepath.Join(tempDir, "live-installer")
	if err := os.WriteFile(existingFile, []byte("binary"), 0755); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	otherFile := filepath.Join(tempDir, "attendedinstaller")
	if err := os.WriteFile(otherFile, []byte("script"), 0755); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Template file path used to resolve relative paths
	templatePath := filepath.Join(tempDir, "defaultconfigs", "default-initrd.yml")
	if err := os.MkdirAll(filepath.Dir(templatePath), 0755); err != nil {
		t.Fatalf("Failed to create template dir: %v", err)
	}

	tests := []struct {
		name        string
		template    *config.ImageTemplate
		expectError bool
		errorMsg    string
	}{
		{
			name: "no_additional_files",
			template: &config.ImageTemplate{
				SystemConfig: config.SystemConfig{
					AdditionalFiles: nil,
				},
			},
			expectError: false,
		},
		{
			name: "absolute_path_exists",
			template: &config.ImageTemplate{
				SystemConfig: config.SystemConfig{
					AdditionalFiles: []config.AdditionalFileInfo{
						{Local: existingFile, Final: "/usr/bin/live-installer"},
					},
				},
			},
			expectError: false,
		},
		{
			name: "absolute_path_missing",
			template: &config.ImageTemplate{
				SystemConfig: config.SystemConfig{
					AdditionalFiles: []config.AdditionalFileInfo{
						{Local: "/nonexistent/path/live-installer", Final: "/usr/bin/live-installer"},
					},
				},
			},
			expectError: true,
			errorMsg:    "live-installer binary not found",
		},
		{
			name: "relative_path_exists",
			template: &config.ImageTemplate{
				PathList: []string{templatePath},
				SystemConfig: config.SystemConfig{
					AdditionalFiles: []config.AdditionalFileInfo{
						{Local: "../attendedinstaller", Final: "/root/attendedinstaller"},
					},
				},
			},
			expectError: false,
		},
		{
			name: "relative_path_missing_live_installer",
			template: &config.ImageTemplate{
				PathList: []string{templatePath},
				SystemConfig: config.SystemConfig{
					AdditionalFiles: []config.AdditionalFileInfo{
						{Local: "../../../../../../build/live-installer", Final: "/usr/bin/live-installer"},
					},
				},
			},
			expectError: true,
			errorMsg:    "live-installer binary not found",
		},
		{
			name: "relative_path_missing_generic_file",
			template: &config.ImageTemplate{
				PathList: []string{templatePath},
				SystemConfig: config.SystemConfig{
					AdditionalFiles: []config.AdditionalFileInfo{
						{Local: "../nonexistent-file", Final: "/etc/some-config"},
					},
				},
			},
			expectError: true,
			errorMsg:    "required additional file not found",
		},
		{
			name: "empty_local_path_skipped",
			template: &config.ImageTemplate{
				SystemConfig: config.SystemConfig{
					AdditionalFiles: []config.AdditionalFileInfo{
						{Local: "", Final: "/usr/bin/something"},
					},
				},
			},
			expectError: false,
		},
		{
			name: "empty_final_path_skipped",
			template: &config.ImageTemplate{
				SystemConfig: config.SystemConfig{
					AdditionalFiles: []config.AdditionalFileInfo{
						{Local: "/some/path", Final: ""},
					},
				},
			},
			expectError: false,
		},
		{
			name: "multiple_files_all_exist",
			template: &config.ImageTemplate{
				PathList: []string{templatePath},
				SystemConfig: config.SystemConfig{
					AdditionalFiles: []config.AdditionalFileInfo{
						{Local: existingFile, Final: "/usr/bin/live-installer"},
						{Local: "../attendedinstaller", Final: "/root/attendedinstaller"},
					},
				},
			},
			expectError: false,
		},
		{
			name: "multiple_files_one_missing",
			template: &config.ImageTemplate{
				PathList: []string{templatePath},
				SystemConfig: config.SystemConfig{
					AdditionalFiles: []config.AdditionalFileInfo{
						{Local: "../attendedinstaller", Final: "/root/attendedinstaller"},
						{Local: "../../../../../../build/live-installer", Final: "/usr/bin/live-installer"},
					},
				},
			},
			expectError: true,
			errorMsg:    "live-installer binary not found",
		},
		{
			name: "live_installer_error_includes_build_hint",
			template: &config.ImageTemplate{
				PathList: []string{templatePath},
				SystemConfig: config.SystemConfig{
					AdditionalFiles: []config.AdditionalFileInfo{
						{Local: "../../../../../../build/live-installer", Final: "/usr/bin/live-installer"},
					},
				},
			},
			expectError: true,
			errorMsg:    "go build -buildmode=pie",
		},
		{
			name:        "nil_template",
			template:    nil,
			expectError: true,
			errorMsg:    "template cannot be nil",
		},
		{
			name: "relative_path_empty_pathlist",
			template: &config.ImageTemplate{
				PathList: []string{},
				SystemConfig: config.SystemConfig{
					AdditionalFiles: []config.AdditionalFileInfo{
						{Local: "../some-file", Final: "/etc/some-file"},
					},
				},
			},
			expectError: true,
			errorMsg:    "required additional file not found",
		},
		{
			name: "local_points_to_directory",
			template: &config.ImageTemplate{
				SystemConfig: config.SystemConfig{
					AdditionalFiles: []config.AdditionalFileInfo{
						{Local: tempDir, Final: "/usr/bin/something"},
					},
				},
			},
			expectError: true,
			errorMsg:    "path is a directory",
		},
		{
			name: "absolute_path_missing_live_installer_hint",
			template: &config.ImageTemplate{
				SystemConfig: config.SystemConfig{
					AdditionalFiles: []config.AdditionalFileInfo{
						{Local: "/nonexistent/path/live-installer", Final: "/usr/bin/live-installer"},
					},
				},
			},
			expectError: true,
			errorMsg:    "go build -buildmode=pie",
		},
		{
			name: "relative_directory_as_file",
			template: &config.ImageTemplate{
				PathList: []string{templatePath},
				SystemConfig: config.SystemConfig{
					AdditionalFiles: []config.AdditionalFileInfo{
						{Local: "..", Final: "/usr/bin/something"},
					},
				},
			},
			expectError: true,
			errorMsg:    "path is a directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAdditionalFiles(tt.template)
			if tt.expectError {
				if err == nil {
					t.Error("Expected error, but got none")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing %q, but got: %v", tt.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				}
			}
		})
	}
}
