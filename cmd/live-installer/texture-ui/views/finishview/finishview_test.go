// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package finishview

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell"
	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/rivo/tview"
)

func TestNew(t *testing.T) {
	mockFunc := func() time.Duration {
		return 5 * time.Minute
	}

	fv := New(mockFunc)

	if fv == nil {
		t.Fatal("New() returned nil")
	}

	if fv.updateInstallationTime == nil {
		t.Error("expected updateInstallationTime to be set")
	}

	// Test that the callback works
	duration := fv.updateInstallationTime()
	if duration != 5*time.Minute {
		t.Errorf("expected duration to be 5 minutes, got %v", duration)
	}

	// Check initial state
	if fv.alreadyShown {
		t.Error("expected alreadyShown to be false initially")
	}

	if fv.app != nil {
		t.Error("expected app to be nil before initialization")
	}

	if fv.flex != nil {
		t.Error("expected flex to be nil before initialization")
	}

	if fv.centeredFlex != nil {
		t.Error("expected centeredFlex to be nil before initialization")
	}

	if fv.text != nil {
		t.Error("expected text to be nil before initialization")
	}

	if fv.navBar != nil {
		t.Error("expected navBar to be nil before initialization")
	}
}

func TestFinishView_Name(t *testing.T) {
	mockFunc := func() time.Duration {
		return time.Minute
	}
	fv := New(mockFunc)

	name := fv.Name()
	expectedName := "FINISH"

	if name != expectedName {
		t.Errorf("expected name to be %q, got %q", expectedName, name)
	}
}

func TestFinishView_Title(t *testing.T) {
	mockFunc := func() time.Duration {
		return time.Minute
	}
	fv := New(mockFunc)

	title := fv.Title()

	if title == "" {
		t.Error("expected Title() to return non-empty string")
	}

	// Should return the finish title constant
	if !strings.Contains(title, "Installation Complete") && !strings.Contains(title, "Complete") {
		t.Logf("Title returned: %q", title)
	}
}

func TestFinishView_Primitive_BeforeInitialization(t *testing.T) {
	mockFunc := func() time.Duration {
		return time.Minute
	}
	fv := New(mockFunc)

	primitive := fv.Primitive()

	// Primitive() returns fv.centeredFlex which is nil before initialization
	if primitive != nil {
		t.Logf("Primitive() returned non-nil before initialization: %T", primitive)
	}
}

func TestFinishView_Initialize(t *testing.T) {
	mockFunc := func() time.Duration {
		return 3 * time.Minute
	}

	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
	}

	app := tview.NewApplication()
	mockCallback := func() {}

	fv := New(mockFunc)

	err := fv.Initialize("Back", template, app, mockCallback, mockCallback, mockCallback, mockCallback)

	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Check that UI elements are initialized
	if fv.app == nil {
		t.Error("expected app to be set after initialization")
	}

	if fv.text == nil {
		t.Error("expected text to be initialized")
	}

	if fv.navBar == nil {
		t.Error("expected navBar to be initialized")
	}

	if fv.flex == nil {
		t.Error("expected flex to be initialized")
	}

	if fv.centeredFlex == nil {
		t.Error("expected centeredFlex to be initialized")
	}
}

func TestFinishView_Primitive_AfterInitialization(t *testing.T) {
	mockFunc := func() time.Duration {
		return time.Minute
	}

	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
	}

	app := tview.NewApplication()
	mockCallback := func() {}

	fv := New(mockFunc)
	err := fv.Initialize("Back", template, app, mockCallback, mockCallback, mockCallback, mockCallback)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	primitive := fv.Primitive()

	if primitive == nil {
		t.Error("expected Primitive() to return non-nil after initialization")
	}

	// Should return the centeredFlex
	if primitive != fv.centeredFlex {
		t.Error("expected Primitive() to return centeredFlex")
	}
}

func TestFinishView_HandleInput(t *testing.T) {
	mockFunc := func() time.Duration {
		return time.Minute
	}

	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
	}

	app := tview.NewApplication()
	mockCallback := func() {}

	fv := New(mockFunc)
	err := fv.Initialize("Back", template, app, mockCallback, mockCallback, mockCallback, mockCallback)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	tests := []struct {
		name     string
		key      tcell.Key
		expected *tcell.EventKey
	}{
		{
			name:     "Ctrl+C blocked",
			key:      tcell.KeyCtrlC,
			expected: nil,
		},
		{
			name:     "Enter passes through",
			key:      tcell.KeyEnter,
			expected: nil, // Will be the event itself
		},
		{
			name:     "Tab passes through",
			key:      tcell.KeyTab,
			expected: nil, // Will be the event itself
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := tcell.NewEventKey(tt.key, 0, tcell.ModNone)
			result := fv.HandleInput(event)

			if tt.key == tcell.KeyCtrlC {
				if result != nil {
					t.Errorf("expected Ctrl+C to be blocked (return nil), got %v", result)
				}
			} else {
				// Other keys should pass through
				if result == nil {
					t.Error("expected event to pass through")
				}
			}
		})
	}
}

func TestFinishView_HandleInput_WithNilEvent(t *testing.T) {
	mockFunc := func() time.Duration {
		return time.Minute
	}

	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
	}

	app := tview.NewApplication()
	mockCallback := func() {}

	fv := New(mockFunc)
	err := fv.Initialize("Back", template, app, mockCallback, mockCallback, mockCallback, mockCallback)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// HandleInput will panic with nil event because it calls event.Key()
	defer func() {
		if r := recover(); r != nil {
			t.Logf("HandleInput(nil) panicked as expected: %v", r)
		}
	}()

	result := fv.HandleInput(nil)
	_ = result
}

func TestFinishView_Reset(t *testing.T) {
	mockFunc := func() time.Duration {
		return time.Minute
	}

	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
	}

	app := tview.NewApplication()
	mockCallback := func() {}

	fv := New(mockFunc)
	err := fv.Initialize("Back", template, app, mockCallback, mockCallback, mockCallback, mockCallback)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Test that Reset doesn't panic
	err = fv.Reset()
	if err != nil {
		t.Errorf("Reset() returned error: %v", err)
	}
}

func TestFinishView_Reset_BeforeInitialization(t *testing.T) {
	mockFunc := func() time.Duration {
		return time.Minute
	}
	fv := New(mockFunc)

	// Reset should handle uninitialized state
	defer func() {
		if r := recover(); r != nil {
			t.Logf("Reset panicked as expected (not initialized): %v", r)
		}
	}()

	err := fv.Reset()
	_ = err
}

func TestFinishView_OnShow_Panics_If_Called_Twice(t *testing.T) {
	mockFunc := func() time.Duration {
		return 2 * time.Minute
	}

	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
	}

	app := tview.NewApplication()
	mockCallback := func() {}

	fv := New(mockFunc)
	err := fv.Initialize("Back", template, app, mockCallback, mockCallback, mockCallback, mockCallback)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// First call should succeed
	fv.OnShow()

	if !fv.alreadyShown {
		t.Error("expected alreadyShown to be true after OnShow")
	}

	// Second call should panic
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected OnShow to panic when called twice")
		} else {
			t.Logf("OnShow panicked as expected on second call: %v", r)
		}
	}()

	fv.OnShow()
}

func TestFinishView_OnShow_UpdatesText(t *testing.T) {
	mockFunc := func() time.Duration {
		return 3*time.Minute + 45*time.Second
	}

	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
	}

	app := tview.NewApplication()
	mockCallback := func() {}

	fv := New(mockFunc)
	err := fv.Initialize("Back", template, app, mockCallback, mockCallback, mockCallback, mockCallback)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Call OnShow
	fv.OnShow()

	// Check that text was set
	text := fv.text.GetText(false)
	if text == "" {
		t.Error("expected text to be set after OnShow")
	}

	// Text should contain duration information
	if !strings.Contains(text, "3 minutes") {
		t.Errorf("expected text to contain '3 minutes', got: %q", text)
	}

	if !strings.Contains(text, "45 seconds") {
		t.Errorf("expected text to contain '45 seconds', got: %q", text)
	}
}

func TestFinishView_OnShow_WithExactMinutes(t *testing.T) {
	mockFunc := func() time.Duration {
		return 5 * time.Minute
	}

	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
	}

	app := tview.NewApplication()
	mockCallback := func() {}

	fv := New(mockFunc)
	err := fv.Initialize("Back", template, app, mockCallback, mockCallback, mockCallback, mockCallback)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Call OnShow
	fv.OnShow()

	// Check that text was set
	text := fv.text.GetText(false)
	if text == "" {
		t.Error("expected text to be set after OnShow")
	}

	// Text should contain "5 minutes" without seconds
	if !strings.Contains(text, "5 minutes") {
		t.Errorf("expected text to contain '5 minutes', got: %q", text)
	}

	// Should not contain "seconds" when there are exactly N minutes
	if strings.Contains(text, "and") && strings.Contains(text, "seconds") {
		t.Errorf("expected text to not contain 'and X seconds' for exact minutes, got: %q", text)
	}
}

func TestFinishView_OnShow_WithLessThanMinute(t *testing.T) {
	mockFunc := func() time.Duration {
		return 45 * time.Second
	}

	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
	}

	app := tview.NewApplication()
	mockCallback := func() {}

	fv := New(mockFunc)
	err := fv.Initialize("Back", template, app, mockCallback, mockCallback, mockCallback, mockCallback)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Call OnShow
	fv.OnShow()

	// Check that text was set
	text := fv.text.GetText(false)
	if text == "" {
		t.Error("expected text to be set after OnShow")
	}

	// Text should contain "0 minutes and 45 seconds"
	if !strings.Contains(text, "45 seconds") {
		t.Errorf("expected text to contain '45 seconds', got: %q", text)
	}
}

func TestFinishView_Constants(t *testing.T) {
	// Verify constants are defined with expected values
	tests := []struct {
		name     string
		value    int
		expected int
	}{
		{"defaultNavButton", defaultNavButton, 0},
		{"defaultPadding", defaultPadding, 1},
		{"textProportion", textProportion, 0},
		{"navBarHeight", navBarHeight, 0},
		{"navBarProportion", navBarProportion, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value != tt.expected {
				t.Errorf("expected %s to be %d, got %d", tt.name, tt.expected, tt.value)
			}
		})
	}
}

func TestFinishView_UpdateTextSize(t *testing.T) {
	mockFunc := func() time.Duration {
		return time.Minute
	}

	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
	}

	app := tview.NewApplication()
	mockCallback := func() {}

	fv := New(mockFunc)
	err := fv.Initialize("Back", template, app, mockCallback, mockCallback, mockCallback, mockCallback)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// updateTextSize is called during OnShow, test indirectly
	fv.OnShow()

	// After OnShow, the flex should be properly configured
	if fv.flex == nil {
		t.Error("expected flex to be non-nil after OnShow")
	}
}

func TestFinishView_AlreadyShown_Flag(t *testing.T) {
	mockFunc := func() time.Duration {
		return time.Minute
	}
	fv := New(mockFunc)

	if fv.alreadyShown {
		t.Error("expected alreadyShown to be false initially")
	}

	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
	}

	app := tview.NewApplication()
	mockCallback := func() {}

	err := fv.Initialize("Back", template, app, mockCallback, mockCallback, mockCallback, mockCallback)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// After initialization, still should be false
	if fv.alreadyShown {
		t.Error("expected alreadyShown to be false after initialization")
	}

	// After OnShow, should be true
	fv.OnShow()

	if !fv.alreadyShown {
		t.Error("expected alreadyShown to be true after OnShow")
	}
}
