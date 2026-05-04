// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package progressview

import (
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell"
	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/rivo/tview"
)

func TestNew(t *testing.T) {
	mockInstallFunc := func(progress chan int, status chan string) {
		close(progress)
		close(status)
	}

	pv := New(mockInstallFunc)

	if pv == nil {
		t.Fatal("New() returned nil")
	}

	if pv.performInstallation == nil {
		t.Error("expected performInstallation to be set")
	}

	// Check initial state
	if pv.moreDetails != defaultMoreDetail {
		t.Errorf("expected moreDetails to be %v, got %v", defaultMoreDetail, pv.moreDetails)
	}

	if pv.alreadyShown {
		t.Error("expected alreadyShown to be false initially")
	}

	if pv.app != nil {
		t.Error("expected app to be nil before initialization")
	}

	if pv.flex != nil {
		t.Error("expected flex to be nil before initialization")
	}

	if pv.logText != nil {
		t.Error("expected logText to be nil before initialization")
	}

	if pv.progressBar != nil {
		t.Error("expected progressBar to be nil before initialization")
	}

	if pv.centeredProgressBar != nil {
		t.Error("expected centeredProgressBar to be nil before initialization")
	}
}

func TestProgressView_Name(t *testing.T) {
	mockInstallFunc := func(progress chan int, status chan string) {
		close(progress)
		close(status)
	}

	pv := New(mockInstallFunc)

	name := pv.Name()
	expectedName := "PROGRESS"

	if name != expectedName {
		t.Errorf("expected Name() to return %q, got %q", expectedName, name)
	}
}

func TestProgressView_Title(t *testing.T) {
	mockInstallFunc := func(progress chan int, status chan string) {
		close(progress)
		close(status)
	}

	pv := New(mockInstallFunc)

	title := pv.Title()

	// Title should not be empty
	if title == "" {
		t.Error("expected Title() to return non-empty string")
	}
}

func TestProgressView_Primitive_BeforeInitialization(t *testing.T) {
	mockInstallFunc := func(progress chan int, status chan string) {
		close(progress)
		close(status)
	}

	pv := New(mockInstallFunc)

	primitive := pv.Primitive()

	if primitive != nil {
		t.Logf("Primitive() returned non-nil before initialization: %T", primitive)
	}
}

func TestProgressView_Initialize(t *testing.T) {
	mockInstallFunc := func(progress chan int, status chan string) {
		close(progress)
		close(status)
	}

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

	app := tview.NewApplication()
	mockFunc := func() {}

	pv := New(mockInstallFunc)
	err := pv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)

	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Check that UI elements are initialized
	if pv.app == nil {
		t.Error("expected app to be set after initialization")
	}

	if pv.flex == nil {
		t.Error("expected flex to be set after initialization")
	}

	if pv.logText == nil {
		t.Error("expected logText to be set after initialization")
	}

	if pv.progressBar == nil {
		t.Error("expected progressBar to be set after initialization")
	}

	if pv.centeredProgressBar == nil {
		t.Error("expected centeredProgressBar to be set after initialization")
	}

	if pv.nextPage == nil {
		t.Error("expected nextPage callback to be set after initialization")
	}

	if pv.quit == nil {
		t.Error("expected quit callback to be set after initialization")
	}
}

func TestProgressView_Primitive_AfterInitialization(t *testing.T) {
	mockInstallFunc := func(progress chan int, status chan string) {
		close(progress)
		close(status)
	}

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

	app := tview.NewApplication()
	mockFunc := func() {}

	pv := New(mockInstallFunc)
	err := pv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	primitive := pv.Primitive()

	if primitive == nil {
		t.Error("expected Primitive() to return non-nil after initialization")
	}

	// Should return the flex container
	if _, ok := primitive.(*tview.Flex); !ok {
		t.Errorf("expected Primitive() to return *tview.Flex, got %T", primitive)
	}
}

func TestProgressView_Reset(t *testing.T) {
	mockInstallFunc := func(progress chan int, status chan string) {
		close(progress)
		close(status)
	}

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

	app := tview.NewApplication()
	mockFunc := func() {}

	pv := New(mockInstallFunc)
	err := pv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Reset should not return an error
	err = pv.Reset()
	if err != nil {
		t.Errorf("Reset() returned unexpected error: %v", err)
	}
}

func TestProgressView_HandleInput(t *testing.T) {
	mockInstallFunc := func(progress chan int, status chan string) {
		close(progress)
		close(status)
	}

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

	app := tview.NewApplication()
	mockFunc := func() {}

	pv := New(mockInstallFunc)
	err := pv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Test normal key event passes through
	normalEvent := tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone)
	result := pv.HandleInput(normalEvent)

	if result != normalEvent {
		t.Error("expected HandleInput to return the event for normal keys")
	}
}

func TestProgressView_HandleInput_CtrlC_Blocked(t *testing.T) {
	mockInstallFunc := func(progress chan int, status chan string) {
		close(progress)
		close(status)
	}

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

	app := tview.NewApplication()
	mockFunc := func() {}

	pv := New(mockInstallFunc)
	err := pv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Test Ctrl+C is blocked during installation
	ctrlCEvent := tcell.NewEventKey(tcell.KeyCtrlC, 0, tcell.ModCtrl)
	result := pv.HandleInput(ctrlCEvent)

	if result != nil {
		t.Error("expected HandleInput to return nil for Ctrl+C (blocked during installation)")
	}
}

func TestProgressView_HandleInput_CtrlA_ToggleDetail(t *testing.T) {
	mockInstallFunc := func(progress chan int, status chan string) {
		close(progress)
		close(status)
	}

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

	app := tview.NewApplication()
	mockFunc := func() {}

	pv := New(mockInstallFunc)
	err := pv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Check initial state
	initialDetail := pv.moreDetails

	// Test Ctrl+A toggles detail level
	ctrlAEvent := tcell.NewEventKey(tcell.KeyCtrlA, 0, tcell.ModCtrl)
	result := pv.HandleInput(ctrlAEvent)

	// Event should be consumed (returned as nil or the event)
	if result != ctrlAEvent {
		t.Logf("HandleInput returned %v for Ctrl+A", result)
	}

	// Detail level should be toggled
	if pv.moreDetails == initialDetail {
		t.Error("expected moreDetails to be toggled after Ctrl+A")
	}
}

func TestProgressView_HandleInput_WithNilEvent(t *testing.T) {
	mockInstallFunc := func(progress chan int, status chan string) {
		close(progress)
		close(status)
	}

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

	app := tview.NewApplication()
	mockFunc := func() {}

	pv := New(mockInstallFunc)
	err := pv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Test with nil event
	defer func() {
		if r := recover(); r != nil {
			t.Logf("HandleInput(nil) panicked (may be expected): %v", r)
		}
	}()

	pv.HandleInput(nil)
}

func TestProgressView_OnShow_Panics_If_Called_Twice(t *testing.T) {
	mockInstallFunc := func(progress chan int, status chan string) {
		// Don't close channels immediately to avoid race
		time.Sleep(10 * time.Millisecond)
		close(progress)
		close(status)
	}

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

	app := tview.NewApplication()
	mockNext := func() {}
	mockFunc := func() {}

	pv := New(mockInstallFunc)
	err := pv.Initialize("Back", template, app, mockNext, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// First call should work
	pv.OnShow()

	// Wait a bit for the installation to start
	time.Sleep(50 * time.Millisecond)

	// Second call should panic
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected OnShow to panic when called twice")
		} else {
			t.Logf("OnShow panicked as expected: %v", r)
		}
	}()

	pv.OnShow()
}

func TestProgressView_Constants(t *testing.T) {
	tests := []struct {
		name     string
		value    interface{}
		expected interface{}
	}{
		{"defaultPadding", defaultPadding, 1},
		{"defaultProportion", defaultProportion, 1},
		{"defaultMoreDetail", defaultMoreDetail, false},
		{"logTextHeight", logTextHeight, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value != tt.expected {
				t.Errorf("expected %s to be %v, got %v", tt.name, tt.expected, tt.value)
			}
		})
	}
}

func TestProgressView_SwitchDetailLevel(t *testing.T) {
	mockInstallFunc := func(progress chan int, status chan string) {
		close(progress)
		close(status)
	}

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

	app := tview.NewApplication()
	mockFunc := func() {}

	pv := New(mockInstallFunc)
	err := pv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Test switching to more details
	pv.switchDetailLevel(true)
	if !pv.moreDetails {
		t.Error("expected moreDetails to be true after switchDetailLevel(true)")
	}

	// Test switching to less details
	pv.switchDetailLevel(false)
	if pv.moreDetails {
		t.Error("expected moreDetails to be false after switchDetailLevel(false)")
	}
}

func TestProgressView_MonitorProgress(t *testing.T) {
	mockInstallFunc := func(progress chan int, status chan string) {
		close(progress)
		close(status)
	}

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

	app := tview.NewApplication()
	mockFunc := func() {}

	pv := New(mockInstallFunc)
	err := pv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Test monitorProgress
	progress := make(chan int, 3)
	wg := new(sync.WaitGroup)
	wg.Add(1)

	go pv.monitorProgress(progress, wg)

	// Send some progress updates
	progress <- 25
	progress <- 50
	progress <- 75

	// Close the channel to signal completion
	close(progress)

	// Wait for the goroutine to finish
	wg.Wait()

	// If we get here without hanging, the test passes
	t.Log("monitorProgress completed successfully")
}

func TestProgressView_MonitorStatus(t *testing.T) {
	mockInstallFunc := func(progress chan int, status chan string) {
		close(progress)
		close(status)
	}

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

	app := tview.NewApplication()
	mockFunc := func() {}

	pv := New(mockInstallFunc)
	err := pv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Test monitorStatus
	progress := make(chan int)
	status := make(chan string, 3)
	wg := new(sync.WaitGroup)
	wg.Add(1)

	go pv.monitorStatus(progress, status, wg)

	// Send some status updates
	status <- "Installing packages..."
	status <- "Configuring system..."
	status <- "Finalizing..."

	// Close the channel to signal completion
	close(status)

	// Wait for the goroutine to finish
	wg.Wait()

	// If we get here without hanging, the test passes
	t.Log("monitorStatus completed successfully")
}

func TestProgressView_StartInstallation(t *testing.T) {
	installationCalled := false
	mockInstallFunc := func(progress chan int, status chan string) {
		installationCalled = true
		// Simulate some progress
		progress <- 10
		progress <- 50
		status <- "Installing..."
		progress <- 100
		status <- "Complete"
		close(progress)
		close(status)
	}

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

	app := tview.NewApplication()
	nextCalled := false
	mockNext := func() {
		nextCalled = true
	}
	mockFunc := func() {}

	pv := New(mockInstallFunc)
	err := pv.Initialize("Back", template, app, mockNext, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Start installation in the same goroutine for testing
	pv.startInstallation()

	// Check that installation was called
	if !installationCalled {
		t.Error("expected performInstallation to be called")
	}

	// Check that nextPage was called after installation
	if !nextCalled {
		t.Error("expected nextPage to be called after installation completes")
	}
}

func TestProgressView_Initialize_SetsCallbacks(t *testing.T) {
	mockInstallFunc := func(progress chan int, status chan string) {
		close(progress)
		close(status)
	}

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

	app := tview.NewApplication()
	nextCalled := false
	quitCalled := false

	mockNext := func() { nextCalled = true }
	mockQuit := func() { quitCalled = true }
	mockFunc := func() {}

	pv := New(mockInstallFunc)
	err := pv.Initialize("Back", template, app, mockNext, mockFunc, mockQuit, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Test that callbacks are set and work
	if pv.nextPage == nil {
		t.Fatal("expected nextPage callback to be set")
	}
	pv.nextPage()
	if !nextCalled {
		t.Error("expected nextPage callback to be called")
	}

	if pv.quit == nil {
		t.Fatal("expected quit callback to be set")
	}
	pv.quit()
	if !quitCalled {
		t.Error("expected quit callback to be called")
	}
}

func TestProgressView_Initialize_SetsUIElements(t *testing.T) {
	mockInstallFunc := func(progress chan int, status chan string) {
		close(progress)
		close(status)
	}

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

	app := tview.NewApplication()
	mockFunc := func() {}

	pv := New(mockInstallFunc)
	err := pv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Verify all UI elements are created
	if pv.logText == nil {
		t.Error("expected logText to be created")
	}

	if pv.progressBar == nil {
		t.Error("expected progressBar to be created")
	}

	if pv.centeredProgressBar == nil {
		t.Error("expected centeredProgressBar to be created")
	}

	if pv.flex == nil {
		t.Error("expected flex to be created")
	}

	// Verify logText properties
	if pv.logText != nil {
		// logText should be scrollable
		// Note: tview doesn't expose all properties, but we can check it exists
		t.Log("logText created successfully")
	}
}

func TestProgressView_AlreadyShownFlag(t *testing.T) {
	mockInstallFunc := func(progress chan int, status chan string) {
		// Small delay to allow checking the flag
		time.Sleep(10 * time.Millisecond)
		close(progress)
		close(status)
	}

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

	app := tview.NewApplication()
	mockFunc := func() {}

	pv := New(mockInstallFunc)
	err := pv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Check initial state
	if pv.alreadyShown {
		t.Error("expected alreadyShown to be false before OnShow")
	}

	// Call OnShow
	pv.OnShow()

	// Give it a moment to set the flag
	time.Sleep(5 * time.Millisecond)

	// Check flag is set
	if !pv.alreadyShown {
		t.Error("expected alreadyShown to be true after OnShow")
	}
}
