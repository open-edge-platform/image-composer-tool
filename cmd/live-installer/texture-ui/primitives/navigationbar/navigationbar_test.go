// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package navigationbar

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestNewNavigationBar(t *testing.T) {
	navbar := NewNavigationBar()

	if navbar == nil {
		t.Fatal("NewNavigationBar() returned nil")
	}

	if navbar.Box == nil {
		t.Error("NavigationBar.Box should not be nil")
	}
}

func TestNavigationBar_SetAlign(t *testing.T) {
	navbar := NewNavigationBar()

	result := navbar.SetAlign(tview.AlignCenter)

	if result != navbar {
		t.Error("SetAlign() should return the same NavigationBar instance for chaining")
	}

	if navbar.align != tview.AlignCenter {
		t.Errorf("expected align to be AlignCenter, got %d", navbar.align)
	}
}

func TestNavigationBar_SetLabelColor(t *testing.T) {
	navbar := NewNavigationBar()
	testColor := tcell.ColorRed

	result := navbar.SetLabelColor(testColor)

	if result != navbar {
		t.Error("SetLabelColor() should return the same NavigationBar instance for chaining")
	}

	if navbar.labelColor != testColor {
		t.Errorf("expected labelColor to be %v, got %v", testColor, navbar.labelColor)
	}
}

func TestNavigationBar_SetLabelColorActivated(t *testing.T) {
	navbar := NewNavigationBar()
	testColor := tcell.ColorGreen

	result := navbar.SetLabelColorActivated(testColor)

	if result != navbar {
		t.Error("SetLabelColorActivated() should return the same NavigationBar instance for chaining")
	}

	if navbar.labelColorActivated != testColor {
		t.Errorf("expected labelColorActivated to be %v, got %v", testColor, navbar.labelColorActivated)
	}
}

func TestNavigationBar_SetNavBackgroundColor(t *testing.T) {
	navbar := NewNavigationBar()
	testColor := tcell.ColorBlue

	result := navbar.SetNavBackgroundColor(testColor)

	if result != navbar {
		t.Error("SetNavBackgroundColor() should return the same NavigationBar instance for chaining")
	}

	if navbar.backgroundColor != testColor {
		t.Errorf("expected backgroundColor to be %v, got %v", testColor, navbar.backgroundColor)
	}
}

func TestNavigationBar_SetNavBackgroundColorActivated(t *testing.T) {
	navbar := NewNavigationBar()
	testColor := tcell.ColorYellow

	result := navbar.SetBackgroundColorActivated(testColor)

	if result != navbar {
		t.Error("SetBackgroundColorActivated() should return the same NavigationBar instance for chaining")
	}

	if navbar.backgroundColorActivated != testColor {
		t.Errorf("expected backgroundColorActivated to be %v, got %v", testColor, navbar.backgroundColorActivated)
	}
}

func TestNavigationBar_SetUserFeedback(t *testing.T) {
	navbar := NewNavigationBar()
	feedback := "Test feedback message"
	testColor := tcell.ColorRed

	navbar.SetUserFeedback(feedback, testColor)

	if navbar.feedback == "" {
		t.Error("expected feedback to be set")
	}
	if navbar.feedbackColor != testColor {
		t.Errorf("expected feedbackColor to be %v, got %v", testColor, navbar.feedbackColor)
	}
}

func TestNavigationBar_AddButton(t *testing.T) {
	navbar := NewNavigationBar()

	result := navbar.AddButton("Test Button", nil)

	if result != navbar {
		t.Error("AddButton() should return the same NavigationBar instance for chaining")
	}

	if len(navbar.buttons) != 1 {
		t.Errorf("expected 1 button, got %d", len(navbar.buttons))
	}
}

func TestNavigationBar_AddMultipleButtons(t *testing.T) {
	navbar := NewNavigationBar()

	navbar.AddButton("Button 1", nil)
	navbar.AddButton("Button 2", nil)
	navbar.AddButton("Button 3", nil)

	if len(navbar.buttons) != 3 {
		t.Errorf("expected 3 buttons, got %d", len(navbar.buttons))
	}
}

func TestNavigationBar_SetSelectedButton(t *testing.T) {
	navbar := NewNavigationBar()
	navbar.AddButton("Button 1", nil)
	navbar.AddButton("Button 2", nil)

	result := navbar.SetSelectedButton(1)

	if result != navbar {
		t.Error("SetSelectedButton() should return the same NavigationBar instance for chaining")
	}

	if navbar.selectedButton != 1 {
		t.Errorf("expected selectedButton to be 1, got %d", navbar.selectedButton)
	}
}

func TestNavigationBar_ClearFeedback(t *testing.T) {
	navbar := NewNavigationBar()
	navbar.SetUserFeedback("Test feedback", tcell.ColorRed)

	navbar.ClearUserFeedback()

	if navbar.feedback != "" {
		t.Errorf("expected feedback to be empty after ClearUserFeedback(), got %q", navbar.feedback)
	}
}

func TestNavigationBar_GetHeight(t *testing.T) {
	navbar := NewNavigationBar()

	height := navbar.GetHeight()

	// Height should be a positive value
	if height <= 0 {
		t.Errorf("expected positive height, got %d", height)
	}
}

func TestNavigationBar_SetFinishedFunc(t *testing.T) {
	navbar := NewNavigationBar()
	called := false

	callback := func(key tcell.Key) {
		called = true
	}

	result := navbar.SetFinishedFunc(callback)

	if result != navbar {
		t.Error("SetFinishedFunc() should return the same NavigationBar instance for chaining")
	}

	// Test that callback was set
	if navbar.onFinished == nil {
		t.Error("expected onFinished callback to be set")
	}

	// Call the callback
	navbar.onFinished(tcell.KeyEnter)
	if !called {
		t.Error("expected callback to be called")
	}
}

func TestNavigationBar_SetFocusFunc(t *testing.T) {
	navbar := NewNavigationBar()
	called := false

	callback := func() {
		called = true
	}

	result := navbar.SetOnFocusFunc(callback)

	if result != navbar {
		t.Error("SetOnFocusFunc() should return the same NavigationBar instance for chaining")
	}

	if navbar.onFocus == nil {
		t.Error("expected onFocus callback to be set")
	}

	navbar.onFocus()
	if !called {
		t.Error("expected callback to be called")
	}
}

func TestNavigationBar_SetBlurFunc(t *testing.T) {
	navbar := NewNavigationBar()
	called := false

	callback := func() {
		called = true
	}

	result := navbar.SetOnBlurFunc(callback)

	if result != navbar {
		t.Error("SetOnBlurFunc() should return the same NavigationBar instance for chaining")
	}

	if navbar.onBlur == nil {
		t.Error("expected onBlur callback to be set")
	}

	navbar.onBlur()
	if !called {
		t.Error("expected callback to be called")
	}
}

func TestNavigationBar_MethodChaining(t *testing.T) {
	navbar := NewNavigationBar()

	// Test method chaining
	result := navbar.
		SetAlign(tview.AlignCenter).
		SetLabelColor(tcell.ColorRed).
		SetNavBackgroundColor(tcell.ColorBlue).
		AddButton("Test", nil)

	if result != navbar {
		t.Error("method chaining should return the same NavigationBar instance")
	}

	// Verify values were set
	if navbar.align != tview.AlignCenter {
		t.Error("align not set correctly during method chaining")
	}
	if navbar.labelColor != tcell.ColorRed {
		t.Error("labelColor not set correctly during method chaining")
	}
	if navbar.backgroundColor != tcell.ColorBlue {
		t.Error("backgroundColor not set correctly during method chaining")
	}
	if len(navbar.buttons) != 1 {
		t.Error("button not added correctly during method chaining")
	}
}

func TestNavigationBar_UnfocusedInputHandler_LeftKey(t *testing.T) {
	navbar := NewNavigationBar()
	navbar.AddButton("Button 1", nil)
	navbar.AddButton("Button 2", nil)
	navbar.AddButton("Button 3", nil)
	navbar.SetSelectedButton(2)

	event := tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone)
	result := navbar.UnfocusedInputHandler(event)

	if !result {
		t.Error("expected UnfocusedInputHandler to return true for left key")
	}

	if navbar.selectedButton != 1 {
		t.Errorf("expected selectedButton to be 1 after left key, got %d", navbar.selectedButton)
	}
}

func TestNavigationBar_UnfocusedInputHandler_LeftKeyAtBoundary(t *testing.T) {
	navbar := NewNavigationBar()
	navbar.AddButton("Button 1", nil)
	navbar.AddButton("Button 2", nil)
	navbar.SetSelectedButton(0)

	event := tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone)
	navbar.UnfocusedInputHandler(event)

	if navbar.selectedButton != 0 {
		t.Errorf("expected selectedButton to remain 0 at left boundary, got %d", navbar.selectedButton)
	}
}

func TestNavigationBar_UnfocusedInputHandler_RightKey(t *testing.T) {
	navbar := NewNavigationBar()
	navbar.AddButton("Button 1", nil)
	navbar.AddButton("Button 2", nil)
	navbar.AddButton("Button 3", nil)
	navbar.SetSelectedButton(0)

	event := tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone)
	result := navbar.UnfocusedInputHandler(event)

	if !result {
		t.Error("expected UnfocusedInputHandler to return true for right key")
	}

	if navbar.selectedButton != 1 {
		t.Errorf("expected selectedButton to be 1 after right key, got %d", navbar.selectedButton)
	}
}

func TestNavigationBar_UnfocusedInputHandler_RightKeyAtBoundary(t *testing.T) {
	navbar := NewNavigationBar()
	navbar.AddButton("Button 1", nil)
	navbar.AddButton("Button 2", nil)
	navbar.SetSelectedButton(1)

	event := tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone)
	navbar.UnfocusedInputHandler(event)

	if navbar.selectedButton != 1 {
		t.Errorf("expected selectedButton to remain 1 at right boundary, got %d", navbar.selectedButton)
	}
}

func TestNavigationBar_UnfocusedInputHandler_EscapeKey(t *testing.T) {
	navbar := NewNavigationBar()
	navbar.AddButton("Button 1", nil)
	navbar.AddButton("Button 2", nil)
	navbar.SetSelectedButton(1)

	event := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	result := navbar.UnfocusedInputHandler(event)

	if !result {
		t.Error("expected UnfocusedInputHandler to return true for escape key")
	}

	if navbar.selectedButton != 0 {
		t.Errorf("expected selectedButton to be 0 after escape key, got %d", navbar.selectedButton)
	}
}

func TestNavigationBar_UnfocusedInputHandler_EnterKey(t *testing.T) {
	navbar := NewNavigationBar()

	navbar.AddButton("Button 1", nil)

	event := tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)
	result := navbar.UnfocusedInputHandler(event)

	if !result {
		t.Error("expected UnfocusedInputHandler to return true for enter key")
	}

	// Note: The button callback won't be invoked in this test since we're not
	// simulating the full tview event handling
}

func TestNavigationBar_UnfocusedInputHandler_EnterKeyNoButtons(t *testing.T) {
	navbar := NewNavigationBar()

	event := tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)
	result := navbar.UnfocusedInputHandler(event)

	if !result {
		t.Error("expected UnfocusedInputHandler to return true even with no buttons")
	}
}

func TestNavigationBar_UnfocusedInputHandler_TabKey(t *testing.T) {
	navbar := NewNavigationBar()
	navbar.AddButton("Button 1", nil)

	finishedCalled := false
	var finishedKey tcell.Key

	navbar.SetFinishedFunc(func(key tcell.Key) {
		finishedCalled = true
		finishedKey = key
	})

	event := tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
	result := navbar.UnfocusedInputHandler(event)

	if !result {
		t.Error("expected UnfocusedInputHandler to return true for tab key")
	}

	if !finishedCalled {
		t.Error("expected onFinished to be called for tab key")
	}

	if finishedKey != tcell.KeyTab {
		t.Errorf("expected finished key to be KeyTab, got %v", finishedKey)
	}
}

func TestNavigationBar_UnfocusedInputHandler_BacktabKey(t *testing.T) {
	navbar := NewNavigationBar()
	navbar.AddButton("Button 1", nil)

	finishedCalled := false
	navbar.SetFinishedFunc(func(key tcell.Key) {
		finishedCalled = true
	})

	event := tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModNone)
	result := navbar.UnfocusedInputHandler(event)

	if !result {
		t.Error("expected UnfocusedInputHandler to return true for backtab key")
	}

	if !finishedCalled {
		t.Error("expected onFinished to be called for backtab key")
	}
}

func TestNavigationBar_UnfocusedInputHandler_UpKey(t *testing.T) {
	navbar := NewNavigationBar()
	navbar.AddButton("Button 1", nil)

	finishedCalled := false
	navbar.SetFinishedFunc(func(key tcell.Key) {
		finishedCalled = true
	})

	event := tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
	result := navbar.UnfocusedInputHandler(event)

	if !result {
		t.Error("expected UnfocusedInputHandler to return true for up key")
	}

	if !finishedCalled {
		t.Error("expected onFinished to be called for up key")
	}
}

func TestNavigationBar_UnfocusedInputHandler_DownKey(t *testing.T) {
	navbar := NewNavigationBar()
	navbar.AddButton("Button 1", nil)

	finishedCalled := false
	navbar.SetFinishedFunc(func(key tcell.Key) {
		finishedCalled = true
	})

	event := tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
	result := navbar.UnfocusedInputHandler(event)

	if !result {
		t.Error("expected UnfocusedInputHandler to return true for down key")
	}

	if !finishedCalled {
		t.Error("expected onFinished to be called for down key")
	}
}

func TestNavigationBar_UnfocusedInputHandler_NavKeysNoCallback(t *testing.T) {
	navbar := NewNavigationBar()
	navbar.AddButton("Button 1", nil)

	tests := []struct {
		name string
		key  tcell.Key
	}{
		{"Up", tcell.KeyUp},
		{"Down", tcell.KeyDown},
		{"Tab", tcell.KeyTab},
		{"Backtab", tcell.KeyBacktab},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := tcell.NewEventKey(tt.key, 0, tcell.ModNone)
			result := navbar.UnfocusedInputHandler(event)

			if result {
				t.Errorf("expected UnfocusedInputHandler to return false for %s key when no callback set", tt.name)
			}
		})
	}
}

func TestNavigationBar_UnfocusedInputHandler_UnhandledKey(t *testing.T) {
	navbar := NewNavigationBar()
	navbar.AddButton("Button 1", nil)

	event := tcell.NewEventKey(tcell.KeyF1, 0, tcell.ModNone)
	result := navbar.UnfocusedInputHandler(event)

	if result {
		t.Error("expected UnfocusedInputHandler to return false for unhandled key")
	}
}

func TestNavigationBar_InputHandler(t *testing.T) {
	navbar := NewNavigationBar()
	navbar.AddButton("Button 1", nil)
	navbar.AddButton("Button 2", nil)

	handler := navbar.InputHandler()

	if handler == nil {
		t.Fatal("InputHandler() returned nil")
	}

	// Test that handler works
	navbar.SetSelectedButton(0)
	event := tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone)
	handler(event, nil)

	if navbar.selectedButton != 1 {
		t.Errorf("expected selectedButton to be 1 after using InputHandler, got %d", navbar.selectedButton)
	}
}

func TestNavigationBar_GetLabel(t *testing.T) {
	navbar := NewNavigationBar()
	label := navbar.GetLabel()

	if label != "" {
		t.Errorf("expected empty label, got %q", label)
	}
}

func TestNavigationBar_GetFieldWidth(t *testing.T) {
	navbar := NewNavigationBar()

	// Test with no buttons
	width := navbar.GetFieldWidth()
	if width != 0 {
		t.Errorf("expected width 0 with no buttons, got %d", width)
	}

	// Test with buttons
	navbar.AddButton("Btn1", nil)
	width1 := navbar.GetFieldWidth()
	if width1 <= 0 {
		t.Error("expected positive width after adding button")
	}

	navbar.AddButton("Button2", nil)
	width2 := navbar.GetFieldWidth()
	if width2 <= width1 {
		t.Error("expected width to increase after adding second button")
	}
}

func TestNavigationBar_SetFormAttributes(t *testing.T) {
	navbar := NewNavigationBar()

	result := navbar.SetFormAttributes(
		10,
		tcell.ColorWhite,
		tcell.ColorBlack,
		tcell.ColorYellow,
		tcell.ColorBlue,
	)

	if result != navbar {
		t.Error("SetFormAttributes() should return the same NavigationBar instance")
	}
}

func TestNavigationBar_Focus(t *testing.T) {
	navbar := NewNavigationBar()
	focusCalled := false

	navbar.SetOnFocusFunc(func() {
		focusCalled = true
	})

	navbar.Focus(func(p tview.Primitive) {})

	if !focusCalled {
		t.Error("expected onFocus callback to be called")
	}
}

func TestNavigationBar_FocusNoCallback(t *testing.T) {
	navbar := NewNavigationBar()

	// Should not panic
	navbar.Focus(func(p tview.Primitive) {})
}

func TestNavigationBar_Blur(t *testing.T) {
	navbar := NewNavigationBar()
	blurCalled := false

	navbar.SetOnBlurFunc(func() {
		blurCalled = true
	})

	navbar.Blur()

	if !blurCalled {
		t.Error("expected onBlur callback to be called")
	}
}

func TestNavigationBar_BlurNoCallback(t *testing.T) {
	navbar := NewNavigationBar()

	// Should not panic
	navbar.Blur()
}

func TestNavigationBar_DefaultValues(t *testing.T) {
	navbar := NewNavigationBar()

	if navbar.backgroundColor != tview.Styles.PrimitiveBackgroundColor {
		t.Error("backgroundColor not set to default value")
	}

	if navbar.backgroundColorActivated != tview.Styles.GraphicsColor {
		t.Error("backgroundColorActivated not set to default value")
	}

	if navbar.labelColor != tview.Styles.PrimaryTextColor {
		t.Error("labelColor not set to default value")
	}

	if navbar.labelColorActivated != tview.Styles.InverseTextColor {
		t.Error("labelColorActivated not set to default value")
	}

	if navbar.selectedButton != 0 {
		t.Error("selectedButton should default to 0")
	}

	if navbar.align != 0 {
		t.Error("align should default to 0")
	}

	if navbar.feedback != "" {
		t.Error("feedback should default to empty string")
	}
}

func TestNavigationBar_MultipleButtonInteraction(t *testing.T) {
	navbar := NewNavigationBar()
	navbar.AddButton("First", nil)
	navbar.AddButton("Second", nil)
	navbar.AddButton("Third", nil)
	navbar.AddButton("Fourth", nil)

	// Navigate through all buttons
	for i := 0; i < 3; i++ {
		event := tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone)
		navbar.UnfocusedInputHandler(event)
	}

	if navbar.selectedButton != 3 {
		t.Errorf("expected selectedButton to be 3, got %d", navbar.selectedButton)
	}

	// Navigate back
	for i := 0; i < 3; i++ {
		event := tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone)
		navbar.UnfocusedInputHandler(event)
	}

	if navbar.selectedButton != 0 {
		t.Errorf("expected selectedButton to be 0, got %d", navbar.selectedButton)
	}

	// Use escape to reset
	navbar.SetSelectedButton(2)
	event := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	navbar.UnfocusedInputHandler(event)

	if navbar.selectedButton != 0 {
		t.Errorf("expected selectedButton to be reset to 0 by escape, got %d", navbar.selectedButton)
	}
}

func TestNavigationBar_AlignmentOptions(t *testing.T) {
	navbar := NewNavigationBar()

	tests := []struct {
		name  string
		align int
	}{
		{"Left", tview.AlignLeft},
		{"Center", tview.AlignCenter},
		{"Right", tview.AlignRight},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			navbar.SetAlign(tt.align)
			if navbar.align != tt.align {
				t.Errorf("expected align to be %d, got %d", tt.align, navbar.align)
			}
		})
	}
}

func TestNavigationBar_ButtonCallback(t *testing.T) {
	navbar := NewNavigationBar()

	navbar.AddButton("Test", nil)

	// Verify button was added
	if len(navbar.buttons) != 1 {
		t.Fatal("button not added")
	}
}

func TestNavigationBar_CompleteChaining(t *testing.T) {
	navbar := NewNavigationBar().
		SetAlign(tview.AlignCenter).
		SetLabelColor(tcell.ColorBlue).
		SetLabelColorActivated(tcell.ColorRed).
		SetNavBackgroundColor(tcell.ColorGreen).
		SetBackgroundColorActivated(tcell.ColorYellow).
		AddButton("Button 1", nil).
		AddButton("Button 2", nil).
		SetSelectedButton(1).
		SetUserFeedback("Test feedback", tcell.ColorRed).
		SetOnFocusFunc(func() {}).
		SetOnBlurFunc(func() {})

	if navbar == nil {
		t.Fatal("complete chaining resulted in nil")
	}

	if navbar.align != tview.AlignCenter {
		t.Error("align not set correctly")
	}

	if len(navbar.buttons) != 2 {
		t.Error("buttons not added correctly")
	}

	if navbar.selectedButton != 1 {
		t.Error("selectedButton not set correctly")
	}
}

func TestNavigationBar_FeedbackWithBoldPrefix(t *testing.T) {
	navbar := NewNavigationBar()
	feedbackText := "Error message"

	navbar.SetUserFeedback(feedbackText, tcell.ColorRed)

	// Feedback should contain the text (with bold prefix prepended)
	if navbar.feedback == "" {
		t.Error("expected feedback to be set")
	}

	// Clear and verify
	navbar.ClearUserFeedback()
	if navbar.feedback != "" {
		t.Error("expected feedback to be cleared")
	}
}
