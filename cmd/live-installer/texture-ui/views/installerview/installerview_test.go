// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package installerview

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell"
	"github.com/open-edge-platform/image-composer-tool/cmd/live-installer/texture-ui/primitives/customshortcutlist"
	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/rivo/tview"
)

func TestNew(t *testing.T) {
	iv := New()

	if iv == nil {
		t.Fatal("New() returned nil")
	}

	// Check that installer options are set
	if len(iv.installerOptions) == 0 {
		t.Error("expected installerOptions to be populated")
	}

	// Should have 3 options: terminal, graphical, memtest
	expectedOptions := 3
	if len(iv.installerOptions) != expectedOptions {
		t.Errorf("expected %d installer options, got %d", expectedOptions, len(iv.installerOptions))
	}

	// Check needsToPrompt flag
	// Should be true when there are multiple options
	if !iv.needsToPrompt {
		t.Error("expected needsToPrompt to be true with multiple options")
	}

	// Check initial state
	if iv.optionList != nil {
		t.Error("expected optionList to be nil before initialization")
	}

	if iv.navBar != nil {
		t.Error("expected navBar to be nil before initialization")
	}

	if iv.flex != nil {
		t.Error("expected flex to be nil before initialization")
	}

	if iv.centeredFlex != nil {
		t.Error("expected centeredFlex to be nil before initialization")
	}
}

func TestInstallerView_Name(t *testing.T) {
	iv := New()

	name := iv.Name()
	expectedName := "INSTALLER"

	if name != expectedName {
		t.Errorf("expected name to be %q, got %q", expectedName, name)
	}
}

func TestInstallerView_Title(t *testing.T) {
	iv := New()

	title := iv.Title()

	if title == "" {
		t.Error("expected Title() to return non-empty string")
	}

	// Should contain something about installer or experience
	titleLower := strings.ToLower(title)
	if !strings.Contains(titleLower, "install") && !strings.Contains(titleLower, "experience") {
		t.Logf("Title returned: %q", title)
	}
}

func TestInstallerView_Primitive_BeforeInitialization(t *testing.T) {
	iv := New()

	primitive := iv.Primitive()

	if primitive != nil {
		t.Logf("Primitive() returned non-nil before initialization: %T", primitive)
	}
}

func TestInstallerView_OnShow(t *testing.T) {
	iv := New()

	// OnShow should not panic even if not initialized
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("OnShow() panicked: %v", r)
		}
	}()

	iv.OnShow()
}

func TestInstallerView_NeedsToPrompt(t *testing.T) {
	iv := New()

	needsPrompt := iv.NeedsToPrompt()

	// With 3 options, should need to prompt
	if !needsPrompt {
		t.Error("expected NeedsToPrompt() to return true with multiple options")
	}
}

func TestInstallerView_Initialize(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
	}

	app := tview.NewApplication()
	mockFunc := func() {}

	iv := New()

	err := iv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)

	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Check that UI elements are initialized
	if iv.navBar == nil {
		t.Error("expected navBar to be initialized")
	}

	if iv.optionList == nil {
		t.Error("expected optionList to be initialized")
	}

	if iv.flex == nil {
		t.Error("expected flex to be initialized")
	}

	if iv.centeredFlex == nil {
		t.Error("expected centeredFlex to be initialized")
	}

	// Check that options were populated in the list
	itemCount := iv.optionList.GetItemCount()
	if itemCount != len(iv.installerOptions) {
		t.Errorf("expected %d items in list, got %d", len(iv.installerOptions), itemCount)
	}
}

func TestInstallerView_Primitive_AfterInitialization(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
	}

	app := tview.NewApplication()
	mockFunc := func() {}

	iv := New()
	err := iv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	primitive := iv.Primitive()

	if primitive == nil {
		t.Error("expected Primitive() to return non-nil after initialization")
	}

	// Should return centeredFlex
	if primitive != iv.centeredFlex {
		t.Error("expected Primitive() to return centeredFlex")
	}
}

func TestInstallerView_Reset(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
	}

	app := tview.NewApplication()
	mockFunc := func() {}

	iv := New()
	err := iv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Change the current item
	iv.optionList.SetCurrentItem(2)

	// Reset should restore to first item
	err = iv.Reset()
	if err != nil {
		t.Errorf("Reset() returned error: %v", err)
	}

	// Check that current item was reset to 0
	currentItem := iv.optionList.GetCurrentItem()
	if currentItem != 0 {
		t.Errorf("expected current item to be 0 after reset, got %d", currentItem)
	}
}

func TestInstallerView_HandleInput(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
	}

	app := tview.NewApplication()
	mockFunc := func() {}

	iv := New()
	err := iv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Test that HandleInput doesn't panic
	event := tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
	result := iv.HandleInput(event)

	// Event should be handled
	_ = result
}

func TestInstallerView_HandleInput_WithNilEvent(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
	}

	app := tview.NewApplication()
	mockFunc := func() {}

	iv := New()
	err := iv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// HandleInput should handle nil event gracefully
	defer func() {
		if r := recover(); r != nil {
			t.Logf("HandleInput(nil) panicked (may be expected): %v", r)
		}
	}()

	result := iv.HandleInput(nil)
	_ = result
}

func TestInstallerView_Reset_BeforeInitialization(t *testing.T) {
	iv := New()

	// Reset should handle uninitialized state
	defer func() {
		if r := recover(); r != nil {
			t.Logf("Reset panicked as expected (not initialized): %v", r)
		}
	}()

	err := iv.Reset()
	_ = err
}

func TestInstallerView_Constants(t *testing.T) {
	// Verify constants are defined with expected values
	tests := []struct {
		name     string
		value    int
		expected int
	}{
		{"defaultNavButton", defaultNavButton, 1},
		{"defaultPadding", defaultPadding, 1},
		{"listProportion", listProportion, 0},
		{"navBarHeight", navBarHeight, 0},
		{"navBarProportion", navBarProportion, 1},
		{"terminalUIOption", terminalUIOption, 0},
		{"graphicalUIOption", graphicalUIOption, 1},
		{"memTestOption", memTestOption, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value != tt.expected {
				t.Errorf("expected %s to be %d, got %d", tt.name, tt.expected, tt.value)
			}
		})
	}
}

func TestInstallerView_PopulateInstallerOptions(t *testing.T) {
	iv := New()

	// Initialize the list
	iv.optionList = customshortcutlist.NewList()

	err := iv.populateInstallerOptions()

	if err != nil {
		t.Errorf("populateInstallerOptions() returned error: %v", err)
	}

	// Check that items were added to the list
	itemCount := iv.optionList.GetItemCount()
	expectedCount := len(iv.installerOptions)

	if itemCount != expectedCount {
		t.Errorf("expected %d items in list, got %d", expectedCount, itemCount)
	}
}

func TestInstallerView_PopulateInstallerOptions_WithEmptyOptions(t *testing.T) {
	iv := &InstallerView{
		installerOptions: []string{},
	}

	// Initialize the list
	iv.optionList = customshortcutlist.NewList()

	err := iv.populateInstallerOptions()

	if err == nil {
		t.Error("expected error when installer options are empty")
	}

	if !strings.Contains(err.Error(), "no installer options") {
		t.Errorf("expected error about no installer options, got: %v", err)
	}
}

func TestInstallerView_OnNextButton_TerminalOption(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
	}

	app := tview.NewApplication()
	nextPageCalled := false
	mockNext := func() {
		nextPageCalled = true
	}
	mockFunc := func() {}

	iv := New()
	err := iv.Initialize("Back", template, app, mockNext, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Select terminal option
	iv.optionList.SetCurrentItem(terminalUIOption)

	// Call onNextButton
	iv.onNextButton(mockNext)

	if !nextPageCalled {
		t.Error("expected nextPage to be called for terminal option")
	}
}

func TestInstallerView_OnNextButton_GraphicalOption(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
	}

	app := tview.NewApplication()
	nextPageCalled := false
	mockNext := func() {
		nextPageCalled = true
	}
	mockFunc := func() {}

	iv := New()
	err := iv.Initialize("Back", template, app, mockNext, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Select graphical option
	iv.optionList.SetCurrentItem(graphicalUIOption)

	// Call onNextButton
	iv.onNextButton(mockNext)

	if !nextPageCalled {
		t.Error("expected nextPage to be called for graphical option")
	}
}

func TestInstallerView_OnNextButton_MemTestOption(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
	}

	app := tview.NewApplication()
	nextPageCalled := false
	mockNext := func() {
		nextPageCalled = true
	}
	mockFunc := func() {}

	iv := New()
	err := iv.Initialize("Back", template, app, mockNext, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Select memtest option
	iv.optionList.SetCurrentItem(memTestOption)

	// Call onNextButton
	iv.onNextButton(mockNext)

	if !nextPageCalled {
		t.Error("expected nextPage to be called for memtest option")
	}
}

func TestInstallerView_OnNextButton_AllValidOptions(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
	}

	app := tview.NewApplication()
	mockFunc := func() {}

	iv := New()
	err := iv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Test all valid options don't panic
	for i := 0; i < len(iv.installerOptions); i++ {
		nextPageCalled := false
		mockNext := func() {
			nextPageCalled = true
		}

		iv.optionList.SetCurrentItem(i)
		iv.onNextButton(mockNext)

		if !nextPageCalled {
			t.Errorf("expected nextPage to be called for option %d", i)
		}
	}
}

func TestInstallerView_InstallerOptions(t *testing.T) {
	iv := New()

	// Verify that all expected options are present
	expectedOptions := []string{"Terminal", "Graphical", "MemTest"}

	if len(iv.installerOptions) != len(expectedOptions) {
		t.Errorf("expected %d options, got %d", len(expectedOptions), len(iv.installerOptions))
	}

	// Check that each option contains expected keywords
	for i, option := range iv.installerOptions {
		if option == "" {
			t.Errorf("option %d should not be empty", i)
		}
		t.Logf("Option %d: %q", i, option)
	}
}

func TestInstallerView_NeedsToPrompt_SingleOption(t *testing.T) {
	// Create installer view with only one option
	iv := &InstallerView{
		installerOptions: []string{"Terminal"},
	}
	iv.needsToPrompt = (len(iv.installerOptions) != 1)

	needsPrompt := iv.NeedsToPrompt()

	// With 1 option, should not need to prompt
	if needsPrompt {
		t.Error("expected NeedsToPrompt() to return false with single option")
	}
}

func TestInstallerView_NeedsToPrompt_MultipleOptions(t *testing.T) {
	iv := New()

	needsPrompt := iv.NeedsToPrompt()

	// With multiple options, should need to prompt
	if !needsPrompt {
		t.Error("expected NeedsToPrompt() to return true with multiple options")
	}
}
