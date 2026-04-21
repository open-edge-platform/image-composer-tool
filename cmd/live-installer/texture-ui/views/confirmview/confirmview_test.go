// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package confirmview

import (
	"testing"

	"github.com/gdamore/tcell"
	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/rivo/tview"
)

func TestNew(t *testing.T) {
	cv := New()

	if cv == nil {
		t.Fatal("New() returned nil")
	}

	// Before initialization, fields should be nil
	if cv.text != nil {
		t.Error("expected text to be nil before initialization")
	}

	if cv.navBar != nil {
		t.Error("expected navBar to be nil before initialization")
	}

	if cv.flex != nil {
		t.Error("expected flex to be nil before initialization")
	}

	if cv.centeredFlex != nil {
		t.Error("expected centeredFlex to be nil before initialization")
	}
}

func TestInitialize(t *testing.T) {
	cv := New()

	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
	}

	app := tview.NewApplication()

	mockFunc := func() {}

	err := cv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)

	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Verify components are initialized
	if cv.text == nil {
		t.Error("text should be initialized")
	}

	if cv.navBar == nil {
		t.Error("navBar should be initialized")
	}

	if cv.flex == nil {
		t.Error("flex should be initialized")
	}

	if cv.centeredFlex == nil {
		t.Error("centeredFlex should be initialized")
	}
}

func TestInitialize_WithDifferentBackButtonText(t *testing.T) {
	tests := []struct {
		name           string
		backButtonText string
	}{
		{"DefaultBack", "Back"},
		{"CustomBack", "Previous"},
		{"EmptyBack", ""},
		{"LongBack", "Go Back to Previous Screen"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cv := New()

			template := &config.ImageTemplate{
				Target: config.TargetInfo{
					OS:   "azure-linux",
					Dist: "3.0",
					Arch: "x86_64",
				},
			}

			app := tview.NewApplication()
			mockFunc := func() {}

			err := cv.Initialize(tt.backButtonText, template, app, mockFunc, mockFunc, mockFunc, mockFunc)

			if err != nil {
				t.Errorf("Initialize() with backButtonText %q returned error: %v", tt.backButtonText, err)
			}

			if cv.navBar == nil {
				t.Fatal("navBar should be initialized")
			}
		})
	}
}

func TestHandleInput(t *testing.T) {
	cv := New()

	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
	}

	app := tview.NewApplication()
	mockFunc := func() {}

	err := cv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() failed: %v", err)
	}

	tests := []struct {
		name     string
		event    *tcell.EventKey
		expected *tcell.EventKey
	}{
		{
			name:     "EnterKey",
			event:    tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone),
			expected: tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone),
		},
		{
			name:     "TabKey",
			event:    tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone),
			expected: tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone),
		},
		{
			name:     "EscapeKey",
			event:    tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone),
			expected: tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone),
		},
		{
			name:     "RuneKey",
			event:    tcell.NewEventKey(tcell.KeyRune, 'y', tcell.ModNone),
			expected: tcell.NewEventKey(tcell.KeyRune, 'y', tcell.ModNone),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cv.HandleInput(tt.event)

			if result == nil {
				t.Error("HandleInput() returned nil")
				return
			}

			if result.Key() != tt.expected.Key() {
				t.Errorf("expected key %v, got %v", tt.expected.Key(), result.Key())
			}

			if result.Rune() != tt.expected.Rune() {
				t.Errorf("expected rune %v, got %v", tt.expected.Rune(), result.Rune())
			}
		})
	}
}

func TestHandleInput_NilEvent(t *testing.T) {
	cv := New()

	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
	}

	app := tview.NewApplication()
	mockFunc := func() {}

	err := cv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() failed: %v", err)
	}

	result := cv.HandleInput(nil)

	if result != nil {
		t.Error("HandleInput(nil) should return nil")
	}
}

func TestReset(t *testing.T) {
	cv := New()

	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
	}

	app := tview.NewApplication()
	mockFunc := func() {}

	err := cv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() failed: %v", err)
	}

	// Reset should not return error
	err = cv.Reset()
	if err != nil {
		t.Errorf("Reset() returned error: %v", err)
	}

	// Should be able to reset multiple times
	err = cv.Reset()
	if err != nil {
		t.Errorf("Second Reset() returned error: %v", err)
	}
}

func TestReset_Uninitialized(t *testing.T) {
	cv := New()

	// Reset on uninitialized view should panic
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected Reset() to panic on uninitialized view")
		}
	}()

	_ = cv.Reset()
}

func TestName(t *testing.T) {
	cv := New()

	name := cv.Name()

	if name == "" {
		t.Error("Name() returned empty string")
	}

	if name != "CONFIRM" {
		t.Errorf("expected name to be 'CONFIRM', got %q", name)
	}
}

func TestTitle(t *testing.T) {
	cv := New()

	title := cv.Title()

	if title == "" {
		t.Error("Title() returned empty string")
	}

	// Title should be a non-empty string from uitext
	// The actual value depends on uitext.ConfirmTitle
}

func TestPrimitive(t *testing.T) {
	cv := New()

	// Before initialization, Primitive() returns the centeredFlex field (which is nil)
	primitive := cv.Primitive()
	if primitive != nil {
		t.Log("Primitive() returns centeredFlex field even before initialization")
	}

	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
	}

	app := tview.NewApplication()
	mockFunc := func() {}

	err := cv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() failed: %v", err)
	}

	// After initialization, Primitive() should return the centeredFlex
	primitive = cv.Primitive()
	if primitive == nil {
		t.Error("expected Primitive() to return non-nil after initialization")
	}

	if primitive != cv.centeredFlex {
		t.Error("expected Primitive() to return centeredFlex")
	}
}

func TestOnShow(t *testing.T) {
	cv := New()

	// OnShow should not panic on uninitialized view
	cv.OnShow()

	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
	}

	app := tview.NewApplication()
	mockFunc := func() {}

	err := cv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() failed: %v", err)
	}

	// OnShow should not panic after initialization
	cv.OnShow()
}

func TestConstants(t *testing.T) {
	// Test that constants are defined
	if navButtonGoBack != 0 {
		t.Errorf("expected navButtonGoBack to be 0, got %d", navButtonGoBack)
	}

	if navButtonYes != 1 {
		t.Errorf("expected navButtonYes to be 1, got %d", navButtonYes)
	}

	if defaultNavButton != navButtonYes {
		t.Errorf("expected defaultNavButton to be navButtonYes (%d), got %d", navButtonYes, defaultNavButton)
	}

	// Verify other constants are accessible
	_ = defaultPadding
	_ = textProportion
	_ = navBarHeight
	_ = navBarProportion
}

func TestInitialize_CallbacksWork(t *testing.T) {
	cv := New()

	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
	}

	app := tview.NewApplication()

	nextPage := func() {}
	previousPage := func() {}
	quit := func() {}
	refreshTitle := func() {}

	err := cv.Initialize("Back", template, app, nextPage, previousPage, quit, refreshTitle)

	if err != nil {
		t.Fatalf("Initialize() failed: %v", err)
	}

	// The callbacks should be stored in the navigation bar
	// We can't directly test them without simulating button clicks,
	// but we can verify initialization succeeded
	if cv.navBar == nil {
		t.Error("navBar should be initialized with callbacks")
	}
}

func TestInitialize_WithNilCallbacks(t *testing.T) {
	cv := New()

	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
	}

	app := tview.NewApplication()

	// Pass nil callbacks
	err := cv.Initialize("Back", template, app, nil, nil, nil, nil)

	if err != nil {
		t.Fatalf("Initialize() with nil callbacks returned error: %v", err)
	}

	// Should still initialize successfully
	if cv.navBar == nil {
		t.Error("navBar should be initialized even with nil callbacks")
	}
}

func TestInitialize_MultipleInitializations(t *testing.T) {
	cv := New()

	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
	}

	app := tview.NewApplication()
	mockFunc := func() {}

	// First initialization
	err := cv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("First Initialize() failed: %v", err)
	}

	firstNavBar := cv.navBar

	// Second initialization (re-initialize)
	err = cv.Initialize("Previous", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Second Initialize() failed: %v", err)
	}

	// Should create new components
	if cv.navBar == firstNavBar {
		t.Error("expected new navBar instance after re-initialization")
	}
}

func TestConfirmView_TextContent(t *testing.T) {
	cv := New()

	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
	}

	app := tview.NewApplication()
	mockFunc := func() {}

	err := cv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() failed: %v", err)
	}

	if cv.text == nil {
		t.Fatal("text should be initialized")
	}

	// Text should contain the confirmation prompt
	textContent := cv.text.GetText(false)
	if textContent == "" {
		t.Error("text content should not be empty")
	}
}

func TestConfirmView_NavBarButtons(t *testing.T) {
	cv := New()

	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
	}

	app := tview.NewApplication()
	mockFunc := func() {}

	err := cv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() failed: %v", err)
	}

	if cv.navBar == nil {
		t.Fatal("navBar should be initialized")
	}

	// The navigation bar should have been configured with buttons
	// We can't directly test button configuration without accessing internals,
	// but we can verify it was created
}
