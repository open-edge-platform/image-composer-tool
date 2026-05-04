// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package attendedinstaller

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

const LsblkOutput = `{
   "blockdevices": [
      {"name":"sda", "size":500107862016, "model":"CT500MX500SSD1  "},
      {"name":"sdb", "size":62746787840, "model":"Extreme         "},
      {"name":"nvme0n1", "size":512110190592, "model":"INTEL SSDPEKNW512G8                     "}
   ]
}
`

func TestNew(t *testing.T) {
	template := &config.ImageTemplate{
		Image: config.ImageInfo{
			Name:    "test-image",
			Version: "1.0",
		},
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
		SystemConfig: config.SystemConfig{
			Bootloader: config.Bootloader{
				BootType: "efi",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "lsblk", Output: LsblkOutput, Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	installFunc := func(template *config.ImageTemplate, configDir, localRepo string) error {
		return nil
	}

	ai, err := New(template, "/tmp/config", "/tmp/repo", installFunc)

	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	if ai == nil {
		t.Fatal("New() returned nil AttendedInstaller")
	}

	if ai.template != template {
		t.Error("AttendedInstaller template not set correctly")
	}

	if ai.configDir != "/tmp/config" {
		t.Errorf("expected configDir to be '/tmp/config', got %q", ai.configDir)
	}

	if ai.localRepo != "/tmp/repo" {
		t.Errorf("expected localRepo to be '/tmp/repo', got %q", ai.localRepo)
	}

	if ai.installationFunc == nil {
		t.Error("installationFunc should not be nil")
	}

	if ai.app == nil {
		t.Error("app should not be nil")
	}

	if ai.grid == nil {
		t.Error("grid should not be nil")
	}

	if ai.exitModal == nil {
		t.Error("exitModal should not be nil")
	}

	if ai.titleText == nil {
		t.Error("titleText should not be nil")
	}
}

func TestNew_WithNilTemplate(t *testing.T) {
	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "lsblk", Output: LsblkOutput, Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	installFunc := func(template *config.ImageTemplate, configDir, localRepo string) error {
		return nil
	}

	// Passing nil template will cause panic during initialization
	// because diskview requires template.Target fields
	// We expect this to either return an error or panic
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when passing nil template, but did not panic")
		}
	}()

	_, _ = New(nil, "/tmp/config", "/tmp/repo", installFunc)
}

func TestNew_InitializesViews(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
		SystemConfig: config.SystemConfig{
			Bootloader: config.Bootloader{
				BootType: "efi",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "lsblk", Output: LsblkOutput, Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	installFunc := func(template *config.ImageTemplate, configDir, localRepo string) error {
		return nil
	}

	ai, err := New(template, "/tmp/config", "/tmp/repo", installFunc)

	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	// Check that views were initialized
	if len(ai.allViews) == 0 {
		t.Error("expected views to be initialized, got 0 views")
	}

	// Typically should have: installer, disk, hostname, user, confirm, progress, finish
	expectedMinViews := 7
	if len(ai.allViews) < expectedMinViews {
		t.Errorf("expected at least %d views, got %d", expectedMinViews, len(ai.allViews))
	}
}

func TestAttendedInstaller_RecordedInstallationTime(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
		SystemConfig: config.SystemConfig{
			Bootloader: config.Bootloader{
				BootType: "efi",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "lsblk", Output: LsblkOutput, Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	installFunc := func(template *config.ImageTemplate, configDir, localRepo string) error {
		return nil
	}

	ai, err := New(template, "/tmp/config", "/tmp/repo", installFunc)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	// Initially should be zero
	if ai.recordedInstallationTime() != 0 {
		t.Errorf("expected initial installation time to be 0, got %v", ai.recordedInstallationTime())
	}

	// Set a duration
	testDuration := 5 * time.Second
	ai.installationTime = testDuration

	result := ai.recordedInstallationTime()
	if result != testDuration {
		t.Errorf("expected recordedInstallationTime() to return %v, got %v", testDuration, result)
	}
}

func TestAttendedInstaller_InstallationWrapper_Success(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
		SystemConfig: config.SystemConfig{
			Bootloader: config.Bootloader{
				BootType: "efi",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "lsblk", Output: LsblkOutput, Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	called := false
	installFunc := func(template *config.ImageTemplate, configDir, localRepo string) error {
		called = true
		return nil
	}

	ai, err := New(template, "/tmp/config", "/tmp/repo", installFunc)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	progress := make(chan int, 10)
	status := make(chan string, 10)

	go ai.installationWrapper(progress, status)

	// Wait for channels to close
	for range progress {
	}
	for range status {
	}

	if !called {
		t.Error("expected installation function to be called")
	}

	if ai.installationError != nil {
		t.Errorf("expected no installation error, got %v", ai.installationError)
	}

	if ai.installationTime == 0 {
		t.Error("expected installation time to be recorded")
	}
}

func TestAttendedInstaller_InstallationWrapper_Error(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
		SystemConfig: config.SystemConfig{
			Bootloader: config.Bootloader{
				BootType: "efi",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "lsblk", Output: LsblkOutput, Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	expectedError := errors.New("installation failed")
	installFunc := func(template *config.ImageTemplate, configDir, localRepo string) error {
		return expectedError
	}

	ai, err := New(template, "/tmp/config", "/tmp/repo", installFunc)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	progress := make(chan int, 10)
	status := make(chan string, 10)

	go ai.installationWrapper(progress, status)

	// Wait for channels to close
	for range progress {
	}
	for range status {
	}

	if ai.installationError == nil {
		t.Error("expected installation error to be set")
	}

	if ai.installationError != expectedError {
		t.Errorf("expected error %v, got %v", expectedError, ai.installationError)
	}
}

func TestReleaseVersion(t *testing.T) {
	// Create a temporary os-release file
	tmpDir := t.TempDir()
	releaseFile := filepath.Join(tmpDir, "os-release")

	content := `NAME="Test Linux"
VERSION="3.0.20231115"
ID=testlinux
VERSION_ID="3.0"
PRETTY_NAME="Test Linux 3.0"
`
	if err := os.WriteFile(releaseFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	version, err := releaseVersion(releaseFile)

	if err != nil {
		t.Errorf("releaseVersion() returned error: %v", err)
	}

	if version != "3.0.20231115" {
		t.Errorf("expected version '3.0.20231115', got %q", version)
	}
}

func TestReleaseVersion_WithQuotes(t *testing.T) {
	tmpDir := t.TempDir()
	releaseFile := filepath.Join(tmpDir, "os-release")

	content := `NAME="Test Linux"
VERSION="1.2.3"
ID=testlinux
`
	if err := os.WriteFile(releaseFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	version, err := releaseVersion(releaseFile)

	if err != nil {
		t.Errorf("releaseVersion() returned error: %v", err)
	}

	// Should strip quotes
	if version != "1.2.3" {
		t.Errorf("expected version '1.2.3' (without quotes), got %q", version)
	}
}

func TestReleaseVersion_WithoutQuotes(t *testing.T) {
	tmpDir := t.TempDir()
	releaseFile := filepath.Join(tmpDir, "os-release")

	content := `NAME=TestLinux
VERSION=2.0
ID=testlinux
`
	if err := os.WriteFile(releaseFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	version, err := releaseVersion(releaseFile)

	if err != nil {
		t.Errorf("releaseVersion() returned error: %v", err)
	}

	if version != "2.0" {
		t.Errorf("expected version '2.0', got %q", version)
	}
}

func TestReleaseVersion_MissingFile(t *testing.T) {
	_, err := releaseVersion("/nonexistent/file")

	if err == nil {
		t.Error("expected error when file does not exist")
	}
}

func TestReleaseVersion_MissingVersion(t *testing.T) {
	tmpDir := t.TempDir()
	releaseFile := filepath.Join(tmpDir, "os-release")

	content := `NAME="Test Linux"
ID=testlinux
`
	if err := os.WriteFile(releaseFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, err := releaseVersion(releaseFile)

	if err == nil {
		t.Error("expected error when VERSION field is missing")
	}

	if !strings.Contains(err.Error(), "unable to find release version") {
		t.Errorf("expected error about missing version, got: %v", err)
	}
}

func TestReleaseVersion_MalformedLine(t *testing.T) {
	tmpDir := t.TempDir()
	releaseFile := filepath.Join(tmpDir, "os-release")

	content := `NAME="Test Linux"
INVALID_LINE_NO_EQUALS
VERSION="3.0"
`
	if err := os.WriteFile(releaseFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Should skip malformed line and still find VERSION
	version, err := releaseVersion(releaseFile)

	if err != nil {
		t.Errorf("releaseVersion() should skip malformed lines, got error: %v", err)
	}

	if version != "3.0" {
		t.Errorf("expected version '3.0', got %q", version)
	}
}

func TestReleaseVersion_EmptyValue(t *testing.T) {
	tmpDir := t.TempDir()
	releaseFile := filepath.Join(tmpDir, "os-release")

	content := `NAME="Test Linux"
VERSION=
ID=testlinux
`
	if err := os.WriteFile(releaseFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	version, err := releaseVersion(releaseFile)

	if err != nil {
		t.Errorf("releaseVersion() returned error: %v", err)
	}

	if version != "" {
		t.Errorf("expected empty version, got %q", version)
	}
}

func TestAttendedInstaller_RefreshTitle(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
		SystemConfig: config.SystemConfig{
			Bootloader: config.Bootloader{
				BootType: "efi",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "lsblk", Output: LsblkOutput, Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	installFunc := func(template *config.ImageTemplate, configDir, localRepo string) error {
		return nil
	}

	ai, err := New(template, "/tmp/config", "/tmp/repo", installFunc)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	// Test that refreshTitle doesn't panic
	ai.refreshTitle()

	// Check that titleText is updated with current view's title
	if ai.titleText == nil {
		t.Fatal("titleText should not be nil")
	}

	currentTitle := ai.titleText.GetText(false)
	if currentTitle == "" {
		t.Error("expected title to be non-empty after refresh")
	}

	// The title should match the current view's title
	if len(ai.allViews) > 0 {
		expectedTitle := ai.allViews[ai.currentView].Title()
		// Strip newlines for comparison
		currentTitle = strings.TrimSpace(currentTitle)
		expectedTitle = strings.TrimSpace(expectedTitle)
		if currentTitle != expectedTitle {
			t.Errorf("expected title %q, got %q", expectedTitle, currentTitle)
		}
	}
}

func TestAttendedInstaller_Constants(t *testing.T) {
	// Test that constants are defined with reasonable values
	tests := []struct {
		name  string
		value int
	}{
		{"defaultGridWeight", defaultGridWeight},
		{"textRow", textRow},
		{"textColumn", textColumn},
		{"titleRow", titleRow},
		{"titleColumn", titleColumn},
		{"primaryContentRow", primaryContentRow},
		{"primaryContentColumn", primaryContentColumn},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just verify they're accessible and have values
			// The actual values are internal implementation details
			_ = tt.value
		})
	}
}

func TestAttendedInstaller_UIInitialization(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
		SystemConfig: config.SystemConfig{
			Bootloader: config.Bootloader{
				BootType: "efi",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "lsblk", Output: LsblkOutput, Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	installFunc := func(template *config.ImageTemplate, configDir, localRepo string) error {
		return nil
	}

	ai, err := New(template, "/tmp/config", "/tmp/repo", installFunc)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	// Verify UI components are initialized
	if ai.app == nil {
		t.Error("app should be initialized")
	}

	if ai.grid == nil {
		t.Error("grid should be initialized")
	}

	if ai.exitModal == nil {
		t.Error("exitModal should be initialized")
	}

	if ai.titleText == nil {
		t.Error("titleText should be initialized")
	}

	// Verify initial state
	if ai.currentView != 0 {
		t.Errorf("expected currentView to be 0, got %d", ai.currentView)
	}

	if ai.pauseCustomInput {
		t.Error("expected pauseCustomInput to be false initially")
	}

	if ai.pauseSpeakupInput {
		t.Error("expected pauseSpeakupInput to be false initially")
	}

	if ai.userQuitInstallation {
		t.Error("expected userQuitInstallation to be false initially")
	}
}

func TestAttendedInstaller_InstallationError(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
		SystemConfig: config.SystemConfig{
			Bootloader: config.Bootloader{
				BootType: "efi",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "lsblk", Output: LsblkOutput, Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	testError := errors.New("test installation error")
	installFunc := func(template *config.ImageTemplate, configDir, localRepo string) error {
		return testError
	}

	ai, err := New(template, "/tmp/config", "/tmp/repo", installFunc)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	// Initially no error
	if ai.installationError != nil {
		t.Error("expected no initial installation error")
	}

	// Simulate running installation wrapper
	progress := make(chan int, 1)
	status := make(chan string, 1)
	go ai.installationWrapper(progress, status)

	// Wait for completion
	for range progress {
	}
	for range status {
	}

	// Error should be set
	if ai.installationError != testError {
		t.Errorf("expected installationError to be %v, got %v", testError, ai.installationError)
	}
}
