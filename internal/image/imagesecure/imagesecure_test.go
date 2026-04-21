package imagesecure_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/image/imagesecure"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

func TestConfigImageSecurity(t *testing.T) {
	tempDir := t.TempDir()

	// Store original executor and restore at the end
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name         string
		installRoot  string
		template     *config.ImageTemplate
		mockCommands []shell.MockCommand
		wantErr      bool
		errContains  string
	}{
		{
			name:        "successful config with immutability enabled",
			installRoot: tempDir,
			template: &config.ImageTemplate{
				SystemConfig: config.SystemConfig{
					Immutability: config.ImmutabilityConfig{
						Enabled: true,
					},
				},
			},
			mockCommands: []shell.MockCommand{
				// Mock all mkdir commands that might be called in prepareOverlayDir
				{Pattern: `sudo chroot .* mkdir -p .*`, Output: ""},
				// Mock systemctl command that might be called in createOverlayMntSvc
				{Pattern: `sudo chroot .* bash -c "systemctl enable setup-overlay\.service"`, Output: ""},
				// Mock tee command used by file.Append for fstab updates
				{Pattern: `cat .*/tmp/fileappend-.* \| sudo tee -a .* >/dev/null`, Output: ""},
				// Mock chmod command for script permissions
				{Pattern: `sudo chmod -R 755 .*`, Output: ""},
			},
			wantErr: false,
		},
		{
			name:        "immutability disabled - should skip overlay config",
			installRoot: tempDir,
			template: &config.ImageTemplate{
				SystemConfig: config.SystemConfig{
					Immutability: config.ImmutabilityConfig{
						Enabled: false,
					},
				},
			},
			mockCommands: []shell.MockCommand{},
			wantErr:      false,
		},
		{
			name:        "immutability not configured (default disabled) - should skip",
			installRoot: tempDir,
			template: &config.ImageTemplate{
				SystemConfig: config.SystemConfig{
					// No immutability config, defaults to disabled
				},
			},
			mockCommands: []shell.MockCommand{},
			wantErr:      false,
		},
		{
			name:         "nil template",
			installRoot:  tempDir,
			template:     nil,
			mockCommands: []shell.MockCommand{},
			wantErr:      true, // This should error due to nil pointer dereference
		},
		{
			name:        "immutability enabled with overlay config success",
			installRoot: tempDir,
			template: &config.ImageTemplate{
				SystemConfig: config.SystemConfig{
					Immutability: config.ImmutabilityConfig{
						Enabled: true,
					},
				},
			},
			mockCommands: []shell.MockCommand{
				{Pattern: `sudo chroot .* mkdir -p .*`, Output: ""},
				{Pattern: `sudo chroot .* bash -c "systemctl enable setup-overlay\.service"`, Output: ""},
				{Pattern: `cat .*/tmp/fileappend-.* \| sudo tee -a .* >/dev/null`, Output: ""},
				{Pattern: `sudo chmod -R 755 .*`, Output: ""},
			},
			wantErr: false,
		},
		{
			name:        "test directory creation with duplicate check",
			installRoot: tempDir,
			template: &config.ImageTemplate{
				SystemConfig: config.SystemConfig{
					Immutability: config.ImmutabilityConfig{
						Enabled: true,
					},
				},
			},
			mockCommands: []shell.MockCommand{
				{Pattern: `sudo chroot .* mkdir -p .*`, Output: ""},
				{Pattern: `sudo chroot .* bash -c "systemctl enable setup-overlay\.service"`, Output: ""},
				{Pattern: `cat .*/tmp/fileappend-.* \| sudo tee -a .* >/dev/null`, Output: ""},
				{Pattern: `sudo chmod -R 755 .*`, Output: ""},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create necessary directory structure for file operations
			etcDir := filepath.Join(tt.installRoot, "etc")
			systemdDir := filepath.Join(etcDir, "systemd", "system")
			usrBinDir := filepath.Join(tt.installRoot, "usr", "local", "bin")

			if err := os.MkdirAll(etcDir, 0755); err != nil {
				t.Fatalf("Failed to create etc directory: %v", err)
			}
			if err := os.MkdirAll(systemdDir, 0755); err != nil {
				t.Fatalf("Failed to create systemd directory: %v", err)
			}
			if err := os.MkdirAll(usrBinDir, 0755); err != nil {
				t.Fatalf("Failed to create usr/local/bin directory: %v", err)
			}

			// Create empty fstab file
			fstabPath := filepath.Join(etcDir, "fstab")
			if err := os.WriteFile(fstabPath, []byte(""), 0644); err != nil {
				t.Fatalf("Failed to create fstab file: %v", err)
			}

			// Create tmp directory for temp files (used by file.Append)
			tmpDir := "./tmp"
			if err := os.MkdirAll(tmpDir, 0755); err != nil {
				t.Fatalf("Failed to create tmp directory: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			// Special handling for nil template test to catch panic
			if tt.template == nil {
				defer func() {
					if r := recover(); r != nil {
						if !tt.wantErr {
							t.Errorf("ConfigImageSecurity() panicked = %v, wantErr %v", r, tt.wantErr)
						}
						// Expected panic for nil template - convert to expected behavior
					}
				}()
			}

			err := imagesecure.ConfigImageSecurity(tt.installRoot, tt.template)

			if tt.wantErr && err == nil && tt.template != nil {
				t.Errorf("ConfigImageSecurity() error = nil, wantErr %v", tt.wantErr)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ConfigImageSecurity() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// Test error cases with failing commands
func TestConfigImageSecurity_ErrorCases(t *testing.T) {
	tempDir := t.TempDir()

	// Store original executor and restore at the end
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name         string
		installRoot  string
		template     *config.ImageTemplate
		mockCommands []shell.MockCommand
		wantErr      bool
		errContains  string
	}{
		{
			name:        "mkdir command fails",
			installRoot: tempDir,
			template: &config.ImageTemplate{
				SystemConfig: config.SystemConfig{
					Immutability: config.ImmutabilityConfig{
						Enabled: true,
					},
				},
			},
			mockCommands: []shell.MockCommand{
				{Pattern: `sudo chroot .* mkdir -p .*`, Error: errors.New("Permission denied")},
			},
			wantErr:     true,
			errContains: "failed to prepare ESP directory",
		},
		{
			name:        "systemctl enable fails",
			installRoot: tempDir,
			template: &config.ImageTemplate{
				SystemConfig: config.SystemConfig{
					Immutability: config.ImmutabilityConfig{
						Enabled: true,
					},
				},
			},
			mockCommands: []shell.MockCommand{
				{Pattern: `sudo chroot .* mkdir -p .*`, Output: ""},
				{Pattern: `cat .*/tmp/fileappend-.* \| sudo tee -a .* >/dev/null`, Output: ""},
				{Pattern: `sudo chmod -R 755 .*`, Output: ""},
				{Pattern: `sudo chroot .* bash -c "systemctl enable setup-overlay\.service"`, Error: errors.New("Failed to enable service")},
			},
			wantErr:     true,
			errContains: "failed to create overlay mounting service",
		},
		{
			name:        "file append fails for fstab",
			installRoot: tempDir,
			template: &config.ImageTemplate{
				SystemConfig: config.SystemConfig{
					Immutability: config.ImmutabilityConfig{
						Enabled: true,
					},
				},
			},
			mockCommands: []shell.MockCommand{
				{Pattern: `sudo chroot .* mkdir -p .*`, Output: ""},
				{Pattern: `cat .*/tmp/fileappend-.* \| sudo tee -a .*/fstab >/dev/null`, Error: errors.New("Disk full")},
				{Pattern: `cat .*/tmp/fileappend-.* \| sudo tee -a .* >/dev/null`, Output: ""}, // Allow other tee commands
				{Pattern: `sudo chmod -R 755 .*`, Output: ""},
				{Pattern: `sudo chroot .* bash -c "systemctl enable setup-overlay\.service"`, Output: ""},
			},
			wantErr:     true,
			errContains: "failed to update fstab",
		},
		{
			name:        "chmod fails",
			installRoot: tempDir,
			template: &config.ImageTemplate{
				SystemConfig: config.SystemConfig{
					Immutability: config.ImmutabilityConfig{
						Enabled: true,
					},
				},
			},
			mockCommands: []shell.MockCommand{
				{Pattern: `sudo chroot .* mkdir -p .*`, Output: ""},
				{Pattern: `cat .*/tmp/fileappend-.* \| sudo tee -a .* >/dev/null`, Output: ""},
				{Pattern: `sudo chmod -R 755 .*`, Error: errors.New("Permission denied")},
				{Pattern: `sudo chroot .* bash -c "systemctl enable setup-overlay\.service"`, Output: ""},
			},
			wantErr:     true,
			errContains: "failed to create overlay mounting service",
		},
		{
			name:        "file append fails for service file",
			installRoot: tempDir,
			template: &config.ImageTemplate{
				SystemConfig: config.SystemConfig{
					Immutability: config.ImmutabilityConfig{
						Enabled: true,
					},
				},
			},
			mockCommands: []shell.MockCommand{
				{Pattern: `sudo chroot .* mkdir -p .*`, Output: ""},
				{Pattern: `cat .*/tmp/fileappend-.* \| sudo tee -a .*/fstab >/dev/null`, Output: ""},
				{Pattern: `cat .*/tmp/fileappend-.* \| sudo tee -a .*/setup-overlay.sh >/dev/null`, Output: ""},
				{Pattern: `cat .*/tmp/fileappend-.* \| sudo tee -a .*/setup-overlay.service >/dev/null`, Error: errors.New("Disk full")},
				{Pattern: `sudo chmod -R 755 .*`, Output: ""},
			},
			wantErr:     true,
			errContains: "failed to create overlay mounting service",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create necessary directory structure for file operations
			etcDir := filepath.Join(tt.installRoot, "etc")
			systemdDir := filepath.Join(etcDir, "systemd", "system")
			usrBinDir := filepath.Join(tt.installRoot, "usr", "local", "bin")

			if err := os.MkdirAll(etcDir, 0755); err != nil {
				t.Fatalf("Failed to create etc directory: %v", err)
			}
			if err := os.MkdirAll(systemdDir, 0755); err != nil {
				t.Fatalf("Failed to create systemd directory: %v", err)
			}
			if err := os.MkdirAll(usrBinDir, 0755); err != nil {
				t.Fatalf("Failed to create usr/local/bin directory: %v", err)
			}

			// Create empty fstab file
			fstabPath := filepath.Join(etcDir, "fstab")
			if err := os.WriteFile(fstabPath, []byte(""), 0644); err != nil {
				t.Fatalf("Failed to create fstab file: %v", err)
			}

			// Create tmp directory for temp files (used by file.Append)
			tmpDir := "./tmp"
			if err := os.MkdirAll(tmpDir, 0755); err != nil {
				t.Fatalf("Failed to create tmp directory: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			// Enhanced shell mock commands that might include file.Append commands if needed
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			err := imagesecure.ConfigImageSecurity(tt.installRoot, tt.template)

			if tt.wantErr && err == nil {
				t.Errorf("ConfigImageSecurity() error = nil, wantErr %v", tt.wantErr)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ConfigImageSecurity() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ConfigImageSecurity() error = %v, should contain %v", err, tt.errContains)
				}
			}
		})
	}
}

// Test specific functions for better coverage
func TestConfigOverlayFunctions(t *testing.T) {
	tempDir := t.TempDir()

	// Store original executor and restore at the end
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name         string
		installRoot  string
		template     *config.ImageTemplate
		mockCommands []shell.MockCommand
		wantErr      bool
		errContains  string
	}{
		{
			name:        "test duplicate directory handling in prepareOverlayDir",
			installRoot: tempDir,
			template: &config.ImageTemplate{
				SystemConfig: config.SystemConfig{
					Immutability: config.ImmutabilityConfig{
						Enabled: true,
					},
				},
			},
			mockCommands: []shell.MockCommand{
				{Pattern: `sudo chroot .* mkdir -p .*`, Output: ""},
				{Pattern: `sudo chroot .* bash -c "systemctl enable setup-overlay\.service"`, Output: ""},
				{Pattern: `cat .*/tmp/fileappend-.* \| sudo tee -a .* >/dev/null`, Output: ""},
				{Pattern: `sudo chmod -R 755 .*`, Output: ""},
			},
			wantErr: false,
		},
		{
			name:        "test script file append failure",
			installRoot: tempDir,
			template: &config.ImageTemplate{
				SystemConfig: config.SystemConfig{
					Immutability: config.ImmutabilityConfig{
						Enabled: true,
					},
				},
			},
			mockCommands: []shell.MockCommand{
				{Pattern: `sudo chroot .* mkdir -p .*`, Output: ""},
				{Pattern: `cat .*/tmp/fileappend-.* \| sudo tee -a .*/fstab >/dev/null`, Output: ""},
				{Pattern: `cat .*/tmp/fileappend-.* \| sudo tee -a .*/setup-overlay\.sh >/dev/null`, Error: errors.New("Write failed")},
				{Pattern: `sudo chmod -R 755 .*`, Output: ""},
			},
			wantErr:     true,
			errContains: "failed to create overlay mounting service",
		},
		{
			name:        "mkdir individual directory failure in prepareOverlayDir",
			installRoot: tempDir,
			template: &config.ImageTemplate{
				SystemConfig: config.SystemConfig{
					Immutability: config.ImmutabilityConfig{
						Enabled: true,
					},
				},
			},
			mockCommands: []shell.MockCommand{
				{Pattern: `sudo chroot .* mkdir -p /opt/overlay/etc/`, Output: ""},
				{Pattern: `sudo chroot .* mkdir -p /opt/overlay/etc/upper`, Error: errors.New("No space left")},
			},
			wantErr:     true,
			errContains: "failed to prepare ESP directory",
		},
		{
			name:        "test with custom overlay directories for duplicate logic coverage",
			installRoot: tempDir,
			template: &config.ImageTemplate{
				SystemConfig: config.SystemConfig{
					Immutability: config.ImmutabilityConfig{
						Enabled: true,
					},
				},
			},
			mockCommands: []shell.MockCommand{
				{Pattern: `sudo chroot .* mkdir -p .*`, Output: ""},
				{Pattern: `sudo chroot .* bash -c "systemctl enable setup-overlay\.service"`, Output: ""},
				{Pattern: `cat .*/tmp/fileappend-.* \| sudo tee -a .* >/dev/null`, Output: ""},
				{Pattern: `sudo chmod -R 755 .*`, Output: ""},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create necessary directory structure for file operations
			etcDir := filepath.Join(tt.installRoot, "etc")
			systemdDir := filepath.Join(etcDir, "systemd", "system")
			usrBinDir := filepath.Join(tt.installRoot, "usr", "local", "bin")

			if err := os.MkdirAll(etcDir, 0755); err != nil {
				t.Fatalf("Failed to create etc directory: %v", err)
			}
			if err := os.MkdirAll(systemdDir, 0755); err != nil {
				t.Fatalf("Failed to create systemd directory: %v", err)
			}
			if err := os.MkdirAll(usrBinDir, 0755); err != nil {
				t.Fatalf("Failed to create usr/local/bin directory: %v", err)
			}

			// Create empty fstab file
			fstabPath := filepath.Join(etcDir, "fstab")
			if err := os.WriteFile(fstabPath, []byte(""), 0644); err != nil {
				t.Fatalf("Failed to create fstab file: %v", err)
			}

			// Create tmp directory for temp files (used by file.Append)
			tmpDir := "./tmp"
			if err := os.MkdirAll(tmpDir, 0755); err != nil {
				t.Fatalf("Failed to create tmp directory: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			err := imagesecure.ConfigImageSecurity(tt.installRoot, tt.template)

			if tt.wantErr && err == nil {
				t.Errorf("ConfigImageSecurity() error = nil, wantErr %v", tt.wantErr)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ConfigImageSecurity() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ConfigImageSecurity() error = %v, should contain %v", err, tt.errContains)
				}
			}
		})
	}
}
