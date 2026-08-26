// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package enumfield

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestNewEnumField(t *testing.T) {
	options := []string{"Option 1", "Option 2", "Option 3"}
	ef := NewEnumField(options)

	if ef == nil {
		t.Fatal("NewEnumField() returned nil")
	}

	if ef.Box == nil {
		t.Error("EnumField.Box should not be nil")
	}

	if len(ef.options) != len(options) {
		t.Errorf("expected %d options, got %d", len(options), len(ef.options))
	}

	// Check default selected option
	if ef.selectedOption != 0 {
		t.Errorf("expected selectedOption to be 0, got %d", ef.selectedOption)
	}
}

func TestNewEnumField_EmptyOptions(t *testing.T) {
	options := []string{}
	ef := NewEnumField(options)

	if ef == nil {
		t.Fatal("NewEnumField() returned nil for empty options")
	}

	if len(ef.options) != 0 {
		t.Errorf("expected 0 options, got %d", len(ef.options))
	}
}

func TestEnumField_SetLabelColor(t *testing.T) {
	ef := NewEnumField([]string{"Option 1"})
	testColor := tcell.ColorRed

	result := ef.SetLabelColor(testColor)

	if result != ef {
		t.Error("SetLabelColor() should return the same EnumField instance for chaining")
	}

	if ef.labelColor != testColor {
		t.Errorf("expected labelColor to be %v, got %v", testColor, ef.labelColor)
	}
}

func TestEnumField_SetLabelColorActivated(t *testing.T) {
	ef := NewEnumField([]string{"Option 1"})
	testColor := tcell.ColorGreen

	result := ef.SetLabelColorActivated(testColor)

	if result != ef {
		t.Error("SetLabelColorActivated() should return the same EnumField instance for chaining")
	}

	if ef.labelColorActivated != testColor {
		t.Errorf("expected labelColorActivated to be %v, got %v", testColor, ef.labelColorActivated)
	}
}

func TestEnumField_SetFieldBackgroundColor(t *testing.T) {
	ef := NewEnumField([]string{"Option 1"})
	testColor := tcell.ColorBlue

	result := ef.SetFieldBackgroundColor(testColor)

	if result != ef {
		t.Error("SetFieldBackgroundColor() should return the same EnumField instance for chaining")
	}

	if ef.backgroundColor != testColor {
		t.Errorf("expected backgroundColor to be %v, got %v", testColor, ef.backgroundColor)
	}
}

func TestEnumField_SetBackgroundColorActivated(t *testing.T) {
	ef := NewEnumField([]string{"Option 1"})
	testColor := tcell.ColorYellow

	result := ef.SetBackgroundColorActivated(testColor)

	if result != ef {
		t.Error("SetBackgroundColorActivated() should return the same EnumField instance for chaining")
	}

	if ef.backgroundColorActivated != testColor {
		t.Errorf("expected backgroundColorActivated to be %v, got %v", testColor, ef.backgroundColorActivated)
	}
}

func TestEnumField_GetLabel(t *testing.T) {
	ef := NewEnumField([]string{"Option 1"})
	testLabel := "Select an option:"
	ef.SetLabel(testLabel)

	result := ef.GetLabel()

	if result != testLabel {
		t.Errorf("expected GetLabel() to return %q, got %q", testLabel, result)
	}
}

func TestEnumField_GetText(t *testing.T) {
	options := []string{"Option 1", "Option 2", "Option 3"}
	ef := NewEnumField(options)
	ef.selectedOption = 1

	result := ef.GetText()

	if result != "Option 2" {
		t.Errorf("expected GetText() to return 'Option 2', got %q", result)
	}
}

func TestEnumField_SetLabel(t *testing.T) {
	ef := NewEnumField([]string{"Option 1"})
	testLabel := "Test Label"

	result := ef.SetLabel(testLabel)

	if result != ef {
		t.Error("SetLabel() should return the same EnumField instance for chaining")
	}

	if ef.label != testLabel {
		t.Errorf("expected label to be %q, got %q", testLabel, ef.label)
	}
}

func TestEnumField_SetLabelWidth(t *testing.T) {
	ef := NewEnumField([]string{"Option 1"})
	width := 20

	result := ef.SetLabelWidth(width)

	if result != ef {
		t.Error("SetLabelWidth() should return the same EnumField instance for chaining")
	}

	if ef.GetLabelWidth() != width {
		t.Errorf("expected label width to be %d, got %d", width, ef.GetLabelWidth())
	}
}

func TestEnumField_SetFinishedFunc(t *testing.T) {
	ef := NewEnumField([]string{"Option 1"})
	called := false

	callback := func(key tcell.Key) {
		called = true
	}

	result := ef.SetFinishedFunc(callback)

	if result != ef {
		t.Error("SetFinishedFunc() should return the same EnumField instance")
	}

	if ef.onFinished == nil {
		t.Error("expected onFinished callback to be set")
	}

	ef.onFinished(tcell.KeyEnter)
	if !called {
		t.Error("expected callback to be called")
	}
}

func TestEnumField_SetFocusFunc(t *testing.T) {
	ef := NewEnumField([]string{"Option 1"})
	called := false

	callback := func() {
		called = true
	}

	result := ef.SetOnFocusFunc(callback)

	if result != ef {
		t.Error("SetOnFocusFunc() should return the same EnumField instance")
	}

	if ef.onFocus == nil {
		t.Error("expected onFocus callback to be set")
	}

	ef.onFocus()
	if !called {
		t.Error("expected callback to be called")
	}
}

func TestEnumField_SetBlurFunc(t *testing.T) {
	ef := NewEnumField([]string{"Option 1"})
	called := false

	callback := func() {
		called = true
	}

	result := ef.SetOnBlurFunc(callback)

	if result != ef {
		t.Error("SetOnBlurFunc() should return the same EnumField instance")
	}

	if ef.onBlur == nil {
		t.Error("expected onBlur callback to be set")
	}

	ef.onBlur()
	if !called {
		t.Error("expected callback to be called")
	}
}

func TestEnumField_MethodChaining(t *testing.T) {
	options := []string{"Option 1", "Option 2"}
	ef := NewEnumField(options)

	result := ef.
		SetLabelColor(tcell.ColorRed).
		SetFieldBackgroundColor(tcell.ColorBlue).
		SetLabelColorActivated(tcell.ColorGreen)

	if result != ef {
		t.Error("method chaining should return the same EnumField instance")
	}

	// Verify values were set
	if ef.labelColor != tcell.ColorRed {
		t.Error("labelColor not set correctly during method chaining")
	}
	if ef.backgroundColor != tcell.ColorBlue {
		t.Error("backgroundColor not set correctly during method chaining")
	}
	if ef.labelColorActivated != tcell.ColorGreen {
		t.Error("labelColorActivated not set correctly during method chaining")
	}
}
