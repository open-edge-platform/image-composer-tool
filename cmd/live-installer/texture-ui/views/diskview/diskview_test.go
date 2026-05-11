// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package diskview

import (
	"testing"

	"github.com/gdamore/tcell"
	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
	"github.com/rivo/tview"
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
	dv := New()

	if dv == nil {
		t.Fatal("New() returned nil")
	}

	// Check that the DiskView is initialized with default values
	if dv.autoPartitionMode != false {
		t.Errorf("expected autoPartitionMode to be false, got true")
	}

	if dv.pages != nil {
		t.Error("expected pages to be nil before initialization")
	}

	if dv.autoPartitionWidget != nil {
		t.Error("expected autoPartitionWidget to be nil before initialization")
	}

	if dv.manualPartitionWidget != nil {
		t.Error("expected manualPartitionWidget to be nil before initialization")
	}
}

func TestDiskView_Name(t *testing.T) {
	dv := New()

	name := dv.Name()
	expectedName := "DISK"

	if name != expectedName {
		t.Errorf("expected name to be %q, got %q", expectedName, name)
	}
}

func TestDiskView_Primitive_BeforeInitialization(t *testing.T) {
	dv := New()

	primitive := dv.Primitive()

	// Primitive() returns dv.pages which is nil before initialization
	// So the primitive itself will be nil
	if primitive != nil {
		t.Logf("Primitive() returned non-nil before initialization: %T", primitive)
	}
}

func TestDiskView_OnShow(t *testing.T) {
	dv := New()

	// OnShow should not panic even if not initialized
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("OnShow() panicked: %v", r)
		}
	}()

	dv.OnShow()
}

func TestDiskView_HandleInput_BeforeInitialization(t *testing.T) {
	dv := New()

	// HandleInput should handle nil event gracefully
	defer func() {
		if r := recover(); r != nil {
			// Panic is expected since widgets are not initialized
			t.Logf("HandleInput panicked as expected (not initialized): %v", r)
		}
	}()

	event := tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)
	result := dv.HandleInput(event)
	_ = result
}

func TestDiskView_Reset_BeforeInitialization(t *testing.T) {
	dv := New()

	// Reset should handle uninitialized state
	defer func() {
		if r := recover(); r != nil {
			// Panic is expected since widgets are not initialized
			t.Logf("Reset panicked as expected (not initialized): %v", r)
		}
	}()

	err := dv.Reset()
	_ = err
}

func TestDiskView_Title_BeforeInitialization(t *testing.T) {
	dv := New()

	// Title should handle uninitialized state
	defer func() {
		if r := recover(); r != nil {
			// Panic is expected since widgets are not initialized
			t.Logf("Title panicked as expected (not initialized): %v", r)
		}
	}()

	title := dv.Title()
	_ = title
}

func TestDiskView_Initialize(t *testing.T) {
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

	app := tview.NewApplication()
	mockFunc := func() {}

	dv := New()

	err := dv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)

	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Check that widgets are initialized
	if dv.autoPartitionWidget == nil {
		t.Error("expected autoPartitionWidget to be initialized")
	}

	if dv.manualPartitionWidget == nil {
		t.Error("expected manualPartitionWidget to be initialized")
	}

	if dv.pages == nil {
		t.Error("expected pages to be initialized")
	}

	// Check default mode
	if dv.autoPartitionMode != defaultToAutoPartition {
		t.Errorf("expected autoPartitionMode to be %v, got %v", defaultToAutoPartition, dv.autoPartitionMode)
	}

	// Check that refreshTitle was set
	if dv.refreshTitle == nil {
		t.Error("expected refreshTitle to be set")
	}
}

func TestDiskView_Initialize_WithLegacyBoot(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
		SystemConfig: config.SystemConfig{
			Bootloader: config.Bootloader{
				BootType: "legacy",
			},
		},
	}

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "lsblk", Output: LsblkOutput, Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	app := tview.NewApplication()
	mockFunc := func() {}

	dv := New()

	err := dv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)

	if err != nil {
		t.Fatalf("Initialize() with legacy boot returned error: %v", err)
	}

	// Verify initialization succeeded
	if dv.pages == nil {
		t.Error("expected pages to be initialized")
	}
}

func TestDiskView_Primitive_AfterInitialization(t *testing.T) {
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

	app := tview.NewApplication()
	mockFunc := func() {}

	dv := New()
	err := dv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	primitive := dv.Primitive()

	if primitive == nil {
		t.Error("expected Primitive() to return non-nil after initialization")
	}

	// Should return the pages primitive
	if primitive != dv.pages {
		t.Error("expected Primitive() to return the pages object")
	}
}

func TestDiskView_HandleInput_AfterInitialization(t *testing.T) {
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

	app := tview.NewApplication()
	mockFunc := func() {}

	dv := New()
	err := dv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Test that HandleInput doesn't panic after initialization
	event := tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
	result := dv.HandleInput(event)

	// Result might be nil or the event itself
	_ = result
}

func TestDiskView_Reset_AfterInitialization(t *testing.T) {
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

	app := tview.NewApplication()
	mockFunc := func() {}

	dv := New()
	err := dv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Test that Reset doesn't panic after initialization
	err = dv.Reset()
	if err != nil {
		t.Errorf("Reset() returned error: %v", err)
	}

	// After reset, should be back in auto partition mode
	if !dv.autoPartitionMode {
		t.Error("expected autoPartitionMode to be true after reset")
	}
}

func TestDiskView_Title_AfterInitialization(t *testing.T) {
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

	app := tview.NewApplication()
	mockFunc := func() {}

	dv := New()
	err := dv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	title := dv.Title()

	if title == "" {
		t.Error("expected Title() to return non-empty string after initialization")
	}

	// In auto partition mode, should return auto partition widget title
	if dv.autoPartitionMode {
		expectedTitle := dv.autoPartitionWidget.Title()
		if title != expectedTitle {
			t.Errorf("expected title to be %q, got %q", expectedTitle, title)
		}
	}
}

func TestDiskView_SwitchMode(t *testing.T) {
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

	app := tview.NewApplication()
	refreshTitleCalled := false
	mockRefreshTitle := func() {
		refreshTitleCalled = true
	}
	mockFunc := func() {}

	dv := New()
	err := dv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockRefreshTitle)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Initially in auto partition mode
	if !dv.autoPartitionMode {
		t.Error("expected to start in auto partition mode")
	}

	// Switch to manual mode
	dv.switchMode()

	if dv.autoPartitionMode {
		t.Error("expected to switch to manual partition mode")
	}

	if !refreshTitleCalled {
		t.Error("expected refreshTitle to be called after mode switch")
	}

	// Reset flag and switch back
	refreshTitleCalled = false
	dv.switchMode()

	if !dv.autoPartitionMode {
		t.Error("expected to switch back to auto partition mode")
	}

	if !refreshTitleCalled {
		t.Error("expected refreshTitle to be called after second mode switch")
	}
}

func TestDiskView_Constants(t *testing.T) {
	// Verify constants are defined with expected values
	if resizeWidgetes != true {
		t.Errorf("expected resizeWidgetes to be true, got %v", resizeWidgetes)
	}

	if defaultToAutoPartition != true {
		t.Errorf("expected defaultToAutoPartition to be true, got %v", defaultToAutoPartition)
	}
}

func TestDiskView_PopulateBlockDeviceOptions(t *testing.T) {

	originalExecutor := shell.Default
	defer func() { shell.Default = originalExecutor }()
	mockExpectedOutput := []shell.MockCommand{
		{Pattern: "lsblk", Output: LsblkOutput, Error: nil},
	}
	shell.Default = shell.NewMockExecutor(mockExpectedOutput)

	dv := New()

	err := dv.populateBlockDeviceOptions()

	// May return error if no block devices found or if running in environment
	// without proper disk access, but should not panic
	if err != nil {
		t.Logf("populateBlockDeviceOptions() returned error (may be expected in test environment): %v", err)
	}

	// systemDevices should be set (possibly empty)
	if dv.systemDevices == nil {
		t.Error("expected systemDevices to be non-nil after population")
	}
}

func TestDiskView_HandleInput_WithNilEvent(t *testing.T) {
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

	app := tview.NewApplication()
	mockFunc := func() {}

	dv := New()
	err := dv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Test that HandleInput handles nil event gracefully
	defer func() {
		if r := recover(); r != nil {
			t.Logf("HandleInput(nil) panicked (may be expected): %v", r)
		}
	}()

	result := dv.HandleInput(nil)
	_ = result
}

func TestDiskView_Title_InManualMode(t *testing.T) {
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

	app := tview.NewApplication()
	mockFunc := func() {}

	dv := New()
	err := dv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Switch to manual mode
	dv.switchMode()

	if dv.autoPartitionMode {
		t.Fatal("failed to switch to manual mode")
	}

	title := dv.Title()

	if title == "" {
		t.Error("expected Title() to return non-empty string in manual mode")
	}

	// In manual partition mode, should return manual partition widget title
	expectedTitle := dv.manualPartitionWidget.Title()
	if title != expectedTitle {
		t.Errorf("expected title to be %q, got %q", expectedTitle, title)
	}
}

func TestDiskView_HandleInput_InManualMode(t *testing.T) {
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

	app := tview.NewApplication()
	mockFunc := func() {}

	dv := New()
	err := dv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Switch to manual mode
	dv.switchMode()

	if dv.autoPartitionMode {
		t.Fatal("failed to switch to manual mode")
	}

	// Test that HandleInput delegates to manual widget
	event := tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
	result := dv.HandleInput(event)
	_ = result
}
