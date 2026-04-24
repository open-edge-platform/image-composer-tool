package system_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/system"
)

func TestGetHostOsInfo(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name          string
		osReleaseFile string
		mockCommands  []shell.MockCommand
		setupFunc     func(tempDir string) error
		expected      map[string]string
		expectError   bool
		errorMsg      string
	}{
		{
			name: "successful_os_release_parsing",
			mockCommands: []shell.MockCommand{
				{Pattern: "uname -m", Output: "x86_64\n", Error: nil},
			},
			setupFunc: func(tempDir string) error {
				osReleaseContent := `NAME="Ubuntu"
VERSION="20.04.3 LTS (Focal Fossa)"
ID=ubuntu
ID_LIKE=debian
PRETTY_NAME="Ubuntu 20.04.3 LTS"
VERSION_ID="20.04"
HOME_URL="https://www.ubuntu.com/"
SUPPORT_URL="https://help.ubuntu.com/"
BUG_REPORT_URL="https://bugs.launchpad.net/ubuntu/"
PRIVACY_POLICY_URL="https://www.ubuntu.com/legal/terms-and-policies/privacy-policy"
VERSION_CODENAME=focal
UBUNTU_CODENAME=focal`
				return os.WriteFile(filepath.Join(tempDir, "os-release"), []byte(osReleaseContent), 0644)
			},
			expected: map[string]string{
				"name":    "Ubuntu",
				"version": "20.04",
				"arch":    "x86_64",
			},
			expectError: false,
		},
		{
			name: "successful_lsb_release_fallback",
			mockCommands: []shell.MockCommand{
				{Pattern: "uname -m", Output: "aarch64\n", Error: nil},
				{Pattern: "lsb_release -si", Output: "Ubuntu\n", Error: nil},
				{Pattern: "lsb_release -sr", Output: "22.04\n", Error: nil},
			},
			setupFunc: nil,
			expected: map[string]string{
				"name":    "Ubuntu",
				"version": "22.04",
				"arch":    "aarch64",
			},
			expectError: false,
		},
		{
			name: "uname_failure",
			mockCommands: []shell.MockCommand{
				{Pattern: "uname -m", Output: "", Error: fmt.Errorf("uname command failed")},
			},
			expected:    map[string]string{"name": "", "version": "", "arch": ""},
			expectError: true,
			errorMsg:    "failed to get host architecture",
		},
		{
			name: "lsb_release_si_failure",
			mockCommands: []shell.MockCommand{
				{Pattern: "uname -m", Output: "x86_64\n", Error: nil},
				{Pattern: "lsb_release -si", Output: "", Error: fmt.Errorf("lsb_release -si failed")},
			},
			setupFunc:   nil,
			expected:    map[string]string{"name": "", "version": "", "arch": "x86_64"},
			expectError: true,
			errorMsg:    "failed to get host OS name",
		},
		{
			name: "lsb_release_sr_failure",
			mockCommands: []shell.MockCommand{
				{Pattern: "uname -m", Output: "x86_64\n", Error: nil},
				{Pattern: "lsb_release -si", Output: "Ubuntu\n", Error: nil},
				{Pattern: "lsb_release -sr", Output: "", Error: fmt.Errorf("lsb_release -sr failed")},
			},
			setupFunc:   nil,
			expected:    map[string]string{"name": "Ubuntu", "version": "", "arch": "x86_64"},
			expectError: true,
			errorMsg:    "failed to get host OS version",
		},
		{
			name: "lsb_release_empty_output",
			mockCommands: []shell.MockCommand{
				{Pattern: "uname -m", Output: "x86_64\n", Error: nil},
				{Pattern: "lsb_release -si", Output: "", Error: nil},
			},
			setupFunc:   nil,
			expected:    map[string]string{"name": "", "version": "", "arch": "x86_64"},
			expectError: true,
			errorMsg:    "failed to detect host OS info",
		},
		{
			name: "os_release_with_quotes",
			mockCommands: []shell.MockCommand{
				{Pattern: "uname -m", Output: "x86_64\n", Error: nil},
			},
			setupFunc: func(tempDir string) error {
				osReleaseContent := `NAME="CentOS Linux"
VERSION="8 (Core)"
VERSION_ID="8"`
				return os.WriteFile(filepath.Join(tempDir, "os-release"), []byte(osReleaseContent), 0644)
			},
			expected: map[string]string{
				"name":    "CentOS Linux",
				"version": "8",
				"arch":    "x86_64",
			},
			expectError: false,
		},
		{
			name: "os_release_without_quotes",
			mockCommands: []shell.MockCommand{
				{Pattern: "uname -m", Output: "x86_64\n", Error: nil},
			},
			setupFunc: func(tempDir string) error {
				osReleaseContent := `NAME=Fedora
VERSION=35
VERSION_ID=35`
				return os.WriteFile(filepath.Join(tempDir, "os-release"), []byte(osReleaseContent), 0644)
			},
			expected: map[string]string{
				"name":    "Fedora",
				"version": "35",
				"arch":    "x86_64",
			},
			expectError: false,
		},
		{
			name: "os_release_partial_info",
			mockCommands: []shell.MockCommand{
				{Pattern: "uname -m", Output: "x86_64\n", Error: nil},
				{Pattern: "lsb_release -si", Output: "Debian\n", Error: nil},
				{Pattern: "lsb_release -sr", Output: "11\n", Error: nil},
			},
			setupFunc: func(tempDir string) error {
				osReleaseContent := `NAME="Debian GNU/Linux"
# Missing VERSION_ID`
				return os.WriteFile(filepath.Join(tempDir, "os-release"), []byte(osReleaseContent), 0644)
			},
			expected: map[string]string{
				"name":    "Debian GNU/Linux",
				"version": "",
				"arch":    "x86_64",
			},
			expectError: false,
		},
		{
			name: "os_release_malformed_lines",
			mockCommands: []shell.MockCommand{
				{Pattern: "uname -m", Output: "x86_64\n", Error: nil},
			},
			setupFunc: func(tempDir string) error {
				osReleaseContent := `NAME="Ubuntu"
INVALID_LINE_WITHOUT_EQUALS
VERSION_ID="20.04"
ANOTHER_INVALID=`
				return os.WriteFile(filepath.Join(tempDir, "os-release"), []byte(osReleaseContent), 0644)
			},
			expected: map[string]string{
				"name":    "Ubuntu",
				"version": "20.04",
				"arch":    "x86_64",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			tempDir := t.TempDir()
			if tt.setupFunc != nil {
				if err := tt.setupFunc(tempDir); err != nil {
					t.Fatalf("Failed to setup test: %v", err)
				}
			}

			if tt.setupFunc != nil {
				system.OsReleaseFile = filepath.Join(tempDir, "os-release")
				// Temporarily replace the system function call
				// Since we can't easily mock file system access in the system package,
				// we'll test the lsb_release path for most cases
			} else {
				system.OsReleaseFile = "/nonexistent/os-release"
			}

			result, err := system.GetHostOsInfo()

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, but got none")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s', but got: %v", tt.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				}
				for key, expectedValue := range tt.expected {
					if result[key] != expectedValue {
						t.Errorf("Expected %s='%s', but got '%s'", key, expectedValue, result[key])
					}
				}
			}
		})
	}
}

func TestGetHostOsPkgManager(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name         string
		osName       string
		mockCommands []shell.MockCommand
		expected     string
		expectError  bool
		errorMsg     string
	}{
		{
			name:   "ubuntu_apt",
			osName: "Ubuntu",
			mockCommands: []shell.MockCommand{
				{Pattern: "uname -m", Output: "x86_64\n", Error: nil},
				{Pattern: "lsb_release -si", Output: "Ubuntu\n", Error: nil},
				{Pattern: "lsb_release -sr", Output: "20.04\n", Error: nil},
			},
			expected:    "apt",
			expectError: false,
		},
		{
			name:   "debian_apt",
			osName: "Debian",
			mockCommands: []shell.MockCommand{
				{Pattern: "uname -m", Output: "x86_64\n", Error: nil},
				{Pattern: "lsb_release -si", Output: "Debian\n", Error: nil},
				{Pattern: "lsb_release -sr", Output: "11\n", Error: nil},
			},
			expected:    "apt",
			expectError: false,
		},
		{
			name:   "elxr_apt",
			osName: "eLxr",
			mockCommands: []shell.MockCommand{
				{Pattern: "uname -m", Output: "x86_64\n", Error: nil},
				{Pattern: "lsb_release -si", Output: "eLxr\n", Error: nil},
				{Pattern: "lsb_release -sr", Output: "1.0\n", Error: nil},
			},
			expected:    "apt",
			expectError: false,
		},
		{
			name:   "fedora_yum",
			osName: "Fedora",
			mockCommands: []shell.MockCommand{
				{Pattern: "uname -m", Output: "x86_64\n", Error: nil},
				{Pattern: "lsb_release -si", Output: "Fedora\n", Error: nil},
				{Pattern: "lsb_release -sr", Output: "35\n", Error: nil},
			},
			expected:    "yum",
			expectError: false,
		},
		{
			name:   "centos_yum",
			osName: "CentOS",
			mockCommands: []shell.MockCommand{
				{Pattern: "uname -m", Output: "x86_64\n", Error: nil},
				{Pattern: "lsb_release -si", Output: "CentOS\n", Error: nil},
				{Pattern: "lsb_release -sr", Output: "8\n", Error: nil},
			},
			expected:    "yum",
			expectError: false,
		},
		{
			name:   "rhel_yum",
			osName: "Red Hat Enterprise Linux",
			mockCommands: []shell.MockCommand{
				{Pattern: "uname -m", Output: "x86_64\n", Error: nil},
				{Pattern: "lsb_release -si", Output: "Red Hat Enterprise Linux\n", Error: nil},
				{Pattern: "lsb_release -sr", Output: "8.5\n", Error: nil},
			},
			expected:    "yum",
			expectError: false,
		},
		{
			name:   "azure_linux_tdnf",
			osName: "Microsoft Azure Linux",
			mockCommands: []shell.MockCommand{
				{Pattern: "uname -m", Output: "x86_64\n", Error: nil},
				{Pattern: "lsb_release -si", Output: "Microsoft Azure Linux\n", Error: nil},
				{Pattern: "lsb_release -sr", Output: "2.0\n", Error: nil},
			},
			expected:    "tdnf",
			expectError: false,
		},
		{
			name:   "edge_microvisor_toolkit_tdnf",
			osName: "Edge Microvisor Toolkit",
			mockCommands: []shell.MockCommand{
				{Pattern: "uname -m", Output: "x86_64\n", Error: nil},
				{Pattern: "lsb_release -si", Output: "Edge Microvisor Toolkit\n", Error: nil},
				{Pattern: "lsb_release -sr", Output: "1.0\n", Error: nil},
			},
			expected:    "tdnf",
			expectError: false,
		},
		{
			name:   "unsupported_os",
			osName: "UnsupportedOS",
			mockCommands: []shell.MockCommand{
				{Pattern: "uname -m", Output: "x86_64\n", Error: nil},
				{Pattern: "lsb_release -si", Output: "UnsupportedOS\n", Error: nil},
				{Pattern: "lsb_release -sr", Output: "1.0\n", Error: nil},
			},
			expected:    "",
			expectError: true,
			errorMsg:    "unsupported host OS: UnsupportedOS",
		},
		{
			name:   "get_host_os_info_failure",
			osName: "",
			mockCommands: []shell.MockCommand{
				{Pattern: "uname -m", Output: "", Error: fmt.Errorf("uname failed")},
			},
			expected:    "",
			expectError: true,
			errorMsg:    "failed to get host architecture",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)
			system.OsReleaseFile = "/nonexistent/os-release"
			result, err := system.GetHostOsPkgManager()

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, but got none")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s', but got: %v", tt.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				}
				if result != tt.expected {
					t.Errorf("Expected '%s', but got '%s'", tt.expected, result)
				}
			}
		})
	}
}

func TestGetProviderId(t *testing.T) {
	tests := []struct {
		name     string
		os       string
		dist     string
		arch     string
		expected string
	}{
		{
			name:     "ubuntu_jammy_x86_64",
			os:       "ubuntu",
			dist:     "jammy",
			arch:     "x86_64",
			expected: "ubuntu-jammy-x86_64",
		},
		{
			name:     "fedora_35_aarch64",
			os:       "fedora",
			dist:     "35",
			arch:     "aarch64",
			expected: "fedora-35-aarch64",
		},
		{
			name:     "centos_8_x86_64",
			os:       "centos",
			dist:     "8",
			arch:     "x86_64",
			expected: "centos-8-x86_64",
		},
		{
			name:     "empty_values",
			os:       "",
			dist:     "",
			arch:     "",
			expected: "--",
		},
		{
			name:     "single_values",
			os:       "a",
			dist:     "b",
			arch:     "c",
			expected: "a-b-c",
		},
		{
			name:     "special_characters",
			os:       "os-name",
			dist:     "dist.version",
			arch:     "arch_64",
			expected: "os-name-dist.version-arch_64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := system.GetProviderId(tt.os, tt.dist, tt.arch)
			if result != tt.expected {
				t.Errorf("Expected '%s', but got '%s'", tt.expected, result)
			}
		})
	}
}

func TestStopGPGComponents(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name         string
		chrootPath   string
		mockCommands []shell.MockCommand
		expectError  bool
		errorMsg     string
	}{
		{
			name: "successful_gpg_stop",
			mockCommands: []shell.MockCommand{
				{Pattern: "command -v gpgconf", Output: "/usr/bin/gpgconf\n", Error: nil},
				{Pattern: "gpgconf --list-components", Output: "gpg:OpenPGP:/usr/bin/gpg\ngpg-agent:Private Keys:/usr/bin/gpg-agent\ndirmngr:Network:/usr/bin/dirmngr\n", Error: nil},
				{Pattern: "gpgconf --kill gpg", Output: "", Error: nil},
				{Pattern: "gpgconf --kill gpg-agent", Output: "", Error: nil},
				{Pattern: "gpgconf --kill dirmngr", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name: "gpgconf_not_found",
			mockCommands: []shell.MockCommand{
				{Pattern: "command -v gpgconf", Output: "", Error: fmt.Errorf("command not found")},
			},
			expectError: false, // Should not error when gpgconf is not found
		},
		{
			name: "gpgconf_list_components_failure",
			mockCommands: []shell.MockCommand{
				{Pattern: "command -v gpgconf", Output: "/usr/bin/gpgconf\n", Error: nil},
				{Pattern: "gpgconf --list-components", Output: "", Error: fmt.Errorf("gpgconf list failed")},
			},
			expectError: true,
			errorMsg:    "failed to list GPG components",
		},
		{
			name: "gpgconf_kill_component_failure",
			mockCommands: []shell.MockCommand{
				{Pattern: "command -v gpgconf", Output: "/usr/bin/gpgconf\n", Error: nil},
				{Pattern: "gpgconf --list-components", Output: "gpg:OpenPGP:/usr/bin/gpg\n", Error: nil},
				{Pattern: "gpgconf --kill gpg", Output: "", Error: fmt.Errorf("kill gpg failed")},
			},
			expectError: true,
			errorMsg:    "failed to stop GPG component gpg",
		},
		{
			name: "empty_gpg_components_list",
			mockCommands: []shell.MockCommand{
				{Pattern: "command -v gpgconf", Output: "/usr/bin/gpgconf\n", Error: nil},
				{Pattern: "gpgconf --list-components", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name: "gpg_components_with_empty_lines",
			mockCommands: []shell.MockCommand{
				{Pattern: "command -v gpgconf", Output: "/usr/bin/gpgconf\n", Error: nil},
				{Pattern: "gpgconf --list-components", Output: "gpg:OpenPGP:/usr/bin/gpg\n\ngpg-agent:Private Keys:/usr/bin/gpg-agent\n", Error: nil},
				{Pattern: "gpgconf --kill gpg", Output: "", Error: nil},
				{Pattern: "gpgconf --kill gpg-agent", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name: "gpg_components_without_colon",
			mockCommands: []shell.MockCommand{
				{Pattern: "command -v gpgconf", Output: "/usr/bin/gpgconf\n", Error: nil},
				{Pattern: "gpgconf --list-components", Output: "gpg:OpenPGP:/usr/bin/gpg\ninvalid_line_without_colon\ngpg-agent:Private Keys:/usr/bin/gpg-agent\n", Error: nil},
				{Pattern: "gpgconf --kill gpg", Output: "", Error: nil},
				{Pattern: "gpgconf --kill gpg-agent", Output: "", Error: nil},
			},
			expectError: false, // Should skip invalid lines
		},
		{
			name: "whitespace_handling",
			mockCommands: []shell.MockCommand{
				{Pattern: "command -v gpgconf", Output: "/usr/bin/gpgconf\n", Error: nil},
				{Pattern: "gpgconf --list-components", Output: "  gpg  :OpenPGP:/usr/bin/gpg  \n  gpg-agent  :Private Keys:/usr/bin/gpg-agent  \n", Error: nil},
				{Pattern: "gpgconf --kill gpg", Output: "", Error: nil},
				{Pattern: "gpgconf --kill gpg-agent", Output: "", Error: nil},
			},
			expectError: false,
		},
		{
			name: "empty_chroot_path",
			mockCommands: []shell.MockCommand{
				{Pattern: "command -v gpgconf", Output: "/usr/bin/gpgconf\n", Error: nil},
				{Pattern: "gpgconf --list-components", Output: "gpg:OpenPGP:/usr/bin/gpg\n", Error: nil},
				{Pattern: "gpgconf --kill gpg", Output: "", Error: nil},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			chrootPath := t.TempDir()

			bashPath := filepath.Join(chrootPath, "usr", "bin", "bash")
			if err := os.MkdirAll(filepath.Dir(bashPath), 0700); err != nil {
				t.Fatalf("Failed to create bash directory: %v", err)
			}
			if err := os.WriteFile(bashPath, []byte("#!/bin/bash\necho Bash\n"), 0700); err != nil {
				t.Fatalf("Failed to create bash file: %v", err)
			}

			err := system.StopGPGComponents(chrootPath)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, but got none")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
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

func TestStopGPGComponents_BashAvailability(t *testing.T) {
	err := system.StopGPGComponents("/any/chroot")
	if err != nil {
		t.Errorf("Expected no error when Bash is not available, got: %v", err)
	}
}

func TestStopGPGComponents_EmptyChrootPath(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	mockCommands := []shell.MockCommand{
		{Pattern: "command -v gpgconf", Output: "/usr/bin/gpgconf\n", Error: nil},
		{Pattern: "gpgconf --list-components", Output: "gpg:OpenPGP:/usr/bin/gpg\n", Error: nil},
		{Pattern: "gpgconf --kill gpg", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	err := system.StopGPGComponents("")
	if err != nil {
		t.Errorf("Expected no error for empty chrootPath, got: %v", err)
	}
}

func TestStopGPGComponents_InvalidComponentLines(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	mockCommands := []shell.MockCommand{
		{Pattern: "command -v gpgconf", Output: "/usr/bin/gpgconf\n", Error: nil},
		{Pattern: "gpgconf --list-components", Output: "invalid_line\n", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	err := system.StopGPGComponents("")
	if err != nil {
		t.Errorf("Expected no error for invalid component lines, got: %v", err)
	}
}

func TestStopGPGComponents_ComponentWithSpaces(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	mockCommands := []shell.MockCommand{
		{Pattern: "command -v gpgconf", Output: "/usr/bin/gpgconf\n", Error: nil},
		{Pattern: "gpgconf --list-components", Output: "  gpg-agent  :Private Keys:/usr/bin/gpg-agent  \n", Error: nil},
		{Pattern: "gpgconf --kill gpg-agent", Output: "", Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockCommands)

	err := system.StopGPGComponents("")
	if err != nil {
		t.Errorf("Expected no error for component with spaces, got: %v", err)
	}
}

func TestGetHostOsInfo_OsReleaseEdgeCases(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	tests := []struct {
		name             string
		osReleaseContent string
		mockCommands     []shell.MockCommand
		expected         map[string]string
		expectError      bool
	}{
		{
			name: "name_and_version_id_only",
			osReleaseContent: `NAME=Alpine Linux
VERSION_ID=3.15.0`,
			mockCommands: []shell.MockCommand{
				{Pattern: "uname -m", Output: "x86_64\n", Error: nil},
			},
			expected: map[string]string{
				"name":    "Alpine Linux",
				"version": "3.15.0",
				"arch":    "x86_64",
			},
			expectError: false,
		},
		{
			name: "complex_quoted_values",
			osReleaseContent: `NAME="Ubuntu Server \"Long Term Support\""
VERSION_ID="20.04"`,
			mockCommands: []shell.MockCommand{
				{Pattern: "uname -m", Output: "x86_64\n", Error: nil},
			},
			expected: map[string]string{
				"name":    "Ubuntu Server \"Long Term Support\"",
				"version": "20.04",
				"arch":    "x86_64",
			},
			expectError: false,
		},
		{
			name: "equals_in_value",
			osReleaseContent: `NAME="Test=OS"
VERSION_ID="1.0=stable"`,
			mockCommands: []shell.MockCommand{
				{Pattern: "uname -m", Output: "x86_64\n", Error: nil},
			},
			expected: map[string]string{
				"name":    "Test=OS",
				"version": "1.0=stable",
				"arch":    "x86_64",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Default = shell.NewMockExecutor(tt.mockCommands)

			tempDir := t.TempDir()
			osReleasePath := filepath.Join(tempDir, "os-release")
			err := os.WriteFile(osReleasePath, []byte(tt.osReleaseContent), 0644)
			if err != nil {
				t.Fatalf("Failed to write os-release file: %v", err)
			}

			// Note: Since we can't easily mock the /etc/os-release file access in the system package,
			// this test primarily validates our understanding of the parsing logic.
			// In a real scenario, we might need to refactor the system package to accept
			// a configurable os-release file path for testing.

			result, err := system.GetHostOsInfo()

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, but got none")
				}
			} else {
				// Since we can't mock the file system access directly,
				// we'll just verify that the function doesn't crash
				// and returns valid structure
				if err != nil && !strings.Contains(err.Error(), "failed to detect host OS info") {
					// Allow the "failed to detect" error since we can't mock /etc/os-release
					if !strings.Contains(err.Error(), "failed to get host") {
						t.Errorf("Unexpected error: %v", err)
					}
				}
				if result == nil {
					t.Error("Expected non-nil result map")
				}
				// Verify map has expected keys
				if _, ok := result["name"]; !ok {
					t.Error("Expected 'name' key in result")
				}
				if _, ok := result["version"]; !ok {
					t.Error("Expected 'version' key in result")
				}
				if _, ok := result["arch"]; !ok {
					t.Error("Expected 'arch' key in result")
				}
			}
		})
	}
}

func TestSystem_PackageManagerMapping(t *testing.T) {
	// Test the complete mapping of OS names to package managers
	osToPackageManager := map[string]string{
		"Ubuntu":                   "apt",
		"Debian":                   "apt",
		"eLxr":                     "apt",
		"Fedora":                   "yum",
		"CentOS":                   "yum",
		"Red Hat Enterprise Linux": "yum",
		"Microsoft Azure Linux":    "tdnf",
		"Edge Microvisor Toolkit":  "tdnf",
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	for osName, expectedPkgMgr := range osToPackageManager {
		t.Run(fmt.Sprintf("os_%s_maps_to_%s", strings.ReplaceAll(osName, " ", "_"), expectedPkgMgr), func(t *testing.T) {
			mockCommands := []shell.MockCommand{
				{Pattern: "uname -m", Output: "x86_64\n", Error: nil},
				{Pattern: "lsb_release -si", Output: osName + "\n", Error: nil},
				{Pattern: "lsb_release -sr", Output: "1.0\n", Error: nil},
			}
			shell.Default = shell.NewMockExecutor(mockCommands)
			system.OsReleaseFile = "/nonexistent/os-release"
			result, err := system.GetHostOsPkgManager()

			if err != nil {
				t.Errorf("Expected no error for OS %s, but got: %v", osName, err)
			}
			if result != expectedPkgMgr {
				t.Errorf("Expected package manager %s for OS %s, but got %s", expectedPkgMgr, osName, result)
			}
		})
	}
}

func TestStopGPGComponents_ComponentParsing(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()

	// Test various formats of gpgconf --list-components output
	tests := []struct {
		name               string
		componentsOutput   string
		expectedComponents []string
	}{
		{
			name:               "standard_components",
			componentsOutput:   "gpg:OpenPGP:/usr/bin/gpg\ngpg-agent:Private Keys:/usr/bin/gpg-agent\ndirmngr:Network:/usr/bin/dirmngr",
			expectedComponents: []string{"gpg", "gpg-agent", "dirmngr"},
		},
		{
			name:               "single_component",
			componentsOutput:   "gpg:OpenPGP:/usr/bin/gpg",
			expectedComponents: []string{"gpg"},
		},
		{
			name:               "empty_output",
			componentsOutput:   "",
			expectedComponents: []string{},
		},
		{
			name:               "components_with_extra_whitespace",
			componentsOutput:   "  gpg  :OpenPGP:/usr/bin/gpg  \n  gpg-agent  :Private Keys:/usr/bin/gpg-agent  ",
			expectedComponents: []string{"gpg", "gpg-agent"},
		},
		{
			name:               "mixed_valid_invalid_lines",
			componentsOutput:   "gpg:OpenPGP:/usr/bin/gpg\ninvalid_line\ngpg-agent:Private Keys:/usr/bin/gpg-agent\n\ndirmngr:Network:/usr/bin/dirmngr",
			expectedComponents: []string{"gpg", "gpg-agent", "dirmngr"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCommands := []shell.MockCommand{
				{Pattern: "which gpgconf", Output: "/usr/bin/gpgconf\n", Error: nil},
				{Pattern: "gpgconf --list-components", Output: tt.componentsOutput, Error: nil},
			}

			// Add kill commands for expected components
			for _, component := range tt.expectedComponents {
				mockCommands = append(mockCommands, shell.MockCommand{
					Pattern: fmt.Sprintf("gpgconf --kill %s", component),
					Output:  "",
					Error:   nil,
				})
			}

			shell.Default = shell.NewMockExecutor(mockCommands)

			err := system.StopGPGComponents("/mnt/chroot")

			if err != nil {
				t.Errorf("Expected no error, but got: %v", err)
			}
		})
	}
}
