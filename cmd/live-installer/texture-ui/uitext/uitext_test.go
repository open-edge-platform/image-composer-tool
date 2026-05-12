// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package uitext

import (
	"strings"
	"testing"
)

func TestRequiredInputMark(t *testing.T) {
	if RequiredInputMark != "* " {
		t.Errorf("expected RequiredInputMark to be '* ', got %q", RequiredInputMark)
	}
}

func TestBoldPrefix(t *testing.T) {
	if BoldPrefix != "[::b]" {
		t.Errorf("expected BoldPrefix to be '[::b]', got %q", BoldPrefix)
	}
}

func TestWhiteBoldPrefix(t *testing.T) {
	if WhiteBoldPrefix != "[#ffffff::b]" {
		t.Errorf("expected WhiteBoldPrefix to be '[#ffffff::b]', got %q", WhiteBoldPrefix)
	}
}

func TestNavigationButtonConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		contains string
	}{
		{"ButtonAccept", ButtonAccept, "Accept"},
		{"ButtonCancel", ButtonCancel, "Cancel"},
		{"ButtonConfirm", ButtonConfirm, "Confirm"},
		{"ButtonGoBack", ButtonGoBack, "Go Back"},
		{"ButtonNext", ButtonNext, "Next"},
		{"ButtonYes", ButtonYes, "Yes"},
		{"ButtonQuit", ButtonQuit, "Quit"},
		{"ButtonRestart", ButtonRestart, "Restart"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(tt.constant, tt.contains) {
				t.Errorf("expected %s to contain %q, got %q", tt.name, tt.contains, tt.constant)
			}
		})
	}
}

func TestNavigationHelp(t *testing.T) {
	if NavigationHelp == "" {
		t.Error("NavigationHelp should not be empty")
	}
	if !strings.Contains(NavigationHelp, "Arrow keys") {
		t.Errorf("expected NavigationHelp to mention 'Arrow keys', got %q", NavigationHelp)
	}
}

func TestExitModalTitle(t *testing.T) {
	if ExitModalTitle == "" {
		t.Error("ExitModalTitle should not be empty")
	}
	if !strings.Contains(ExitModalTitle, "quit") {
		t.Errorf("expected ExitModalTitle to mention 'quit', got %q", ExitModalTitle)
	}
}

func TestConfirmViewConstants(t *testing.T) {
	if ConfirmTitle == "" {
		t.Error("ConfirmTitle should not be empty")
	}
	if ConfirmPrompt == "" {
		t.Error("ConfirmPrompt should not be empty")
	}
	if !strings.Contains(ConfirmPrompt, "installation") {
		t.Errorf("expected ConfirmPrompt to mention 'installation', got %q", ConfirmPrompt)
	}
}

func TestDiskViewConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		notEmpty bool
	}{
		{"DiskButtonAddPartition", DiskButtonAddPartition, true},
		{"DiskButtonAuto", DiskButtonAuto, true},
		{"DiskButtonCustom", DiskButtonCustom, true},
		{"DiskButtonRemovePartition", DiskButtonRemovePartition, true},
		{"DiskHelp", DiskHelp, true},
		{"DiskTitle", DiskTitle, true},
		{"DiskFormatLabel", DiskFormatLabel, true},
		{"DiskLabelLabel", DiskLabelLabel, true},
		{"DiskMountPointLabel", DiskMountPointLabel, true},
		{"DiskNameLabel", DiskNameLabel, true},
		{"DiskSizeLabel", DiskSizeLabel, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.notEmpty && tt.constant == "" {
				t.Errorf("expected %s to be non-empty", tt.name)
			}
		})
	}
}

func TestDiskErrorConstants(t *testing.T) {
	errorConstants := []struct {
		name  string
		value string
	}{
		{"MountPointAlreadyInUseError", MountPointAlreadyInUseError},
		{"MountPointStartError", MountPointStartError},
		{"MountPointInvalidCharacterError", MountPointInvalidCharacterError},
		{"LabelInvalidCharacterError", LabelInvalidCharacterError},
		{"NameInvalidCharacterError", NameInvalidCharacterError},
		{"NoPartitionsError", NoPartitionsError},
		{"NoPartitionSelectedError", NoPartitionSelectedError},
		{"NoSizeSpecifiedError", NoSizeSpecifiedError},
		{"NotEnoughDiskSpaceError", NotEnoughDiskSpaceError},
		{"SizeStartError", SizeStartError},
		{"SizeInvalidCharacterError", SizeInvalidCharacterError},
	}

	for _, ec := range errorConstants {
		t.Run(ec.name, func(t *testing.T) {
			if ec.value == "" {
				t.Errorf("expected %s to be non-empty", ec.name)
			}
		})
	}
}

func TestEncryptViewConstants(t *testing.T) {
	if EncryptTitle == "" {
		t.Error("EncryptTitle should not be empty")
	}
	if SkipEncryption == "" {
		t.Error("SkipEncryption should not be empty")
	}
	if EncryptPasswordLabel == "" {
		t.Error("EncryptPasswordLabel should not be empty")
	}
	if ConfirmEncryptPasswordLabel == "" {
		t.Error("ConfirmEncryptPasswordLabel should not be empty")
	}
}

func TestInstallerViewConstants(t *testing.T) {
	if InstallerExperienceTitle == "" {
		t.Error("InstallerExperienceTitle should not be empty")
	}
	if InstallerTerminalOption == "" {
		t.Error("InstallerTerminalOption should not be empty")
	}
	if InstallerGraphicalOption == "" {
		t.Error("InstallerGraphicalOption should not be empty")
	}
}

func TestUserViewConstants(t *testing.T) {
	if UserNameInputLabel == "" {
		t.Error("UserNameInputLabel should not be empty")
	}
	if PasswordInputLabel == "" {
		t.Error("PasswordInputLabel should not be empty")
	}
	if ConfirmPasswordInputLabel == "" {
		t.Error("ConfirmPasswordInputLabel should not be empty")
	}
}

func TestHostnameViewConstants(t *testing.T) {
	if HostNameTitle == "" {
		t.Error("HostNameTitle should not be empty")
	}
	if HostNameInputLabel == "" {
		t.Error("HostNameInputLabel should not be empty")
	}
}

func TestProgressViewConstants(t *testing.T) {
	if ProgressTitle == "" {
		t.Error("ProgressTitle should not be empty")
	}
	if ProgressSpinnerFmt == "" {
		t.Error("ProgressSpinnerFmt should not be empty")
	}
}

func TestFinishViewConstants(t *testing.T) {
	if FinishTitle == "" {
		t.Error("FinishTitle should not be empty")
	}
	if FinishTextFmt == "" {
		t.Error("FinishTextFmt should not be empty")
	}
}

func TestFormattedStringConstants(t *testing.T) {
	// Test format strings contain format specifiers
	formatStrings := []struct {
		name  string
		value string
	}{
		{"DiskAdvanceTitleFmt", DiskAdvanceTitleFmt},
		{"DiskSpaceLeftFmt", DiskSpaceLeftFmt},
		{"InvalidBootPartitionErrorFmt", InvalidBootPartitionErrorFmt},
		{"InvalidRootPartitionErrorFmt", InvalidRootPartitionErrorFmt},
		{"InvalidRootPartitionErrorFormatFmt", InvalidRootPartitionErrorFormatFmt},
		{"PartitionExceedsDiskErrorFmt", PartitionExceedsDiskErrorFmt},
		{"UnexpectedPartitionErrorFmt", UnexpectedPartitionErrorFmt},
	}

	for _, fs := range formatStrings {
		t.Run(fs.name, func(t *testing.T) {
			if !strings.Contains(fs.value, "%") {
				t.Errorf("expected %s to contain format specifier (%%)", fs.name)
			}
		})
	}
}

func TestRequiredFieldLabels(t *testing.T) {
	// Test that required field labels have the RequiredInputMark
	requiredLabels := []struct {
		name  string
		value string
	}{
		{"FormDiskFormatLabel", FormDiskFormatLabel},
		{"FormDiskMountPointLabel", FormDiskMountPointLabel},
		{"FormDiskNameLabel", FormDiskNameLabel},
		{"FormDiskSizeLabel", FormDiskSizeLabel},
	}

	for _, rl := range requiredLabels {
		t.Run(rl.name, func(t *testing.T) {
			if !strings.HasPrefix(rl.value, RequiredInputMark) {
				t.Errorf("expected %s to start with RequiredInputMark (%q), got %q", rl.name, RequiredInputMark, rl.value)
			}
		})
	}
}
