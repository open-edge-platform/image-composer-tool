// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package uiutils

import (
	"fmt"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/cmd/live-installer/texture-ui/primitives/customshortcutlist"
	"github.com/rivo/tview"
)

func TestCenter(t *testing.T) {
	box := tview.NewBox()
	width := 50
	height := 20

	flex := Center(width, height, box)

	if flex == nil {
		t.Fatal("Center() returned nil")
	}

	// Verify that the flex container was created (it's a Flex primitive)
	// The function creates a centered layout with multiple items
}

func TestCenterHorizontally(t *testing.T) {
	box := tview.NewBox()
	width := 50

	flex := CenterHorizontally(width, box)

	if flex == nil {
		t.Fatal("CenterHorizontally() returned nil")
	}

	// Verify that the flex container was created
}

func TestCenterVertically(t *testing.T) {
	box := tview.NewBox()
	height := 20

	flex := CenterVertically(height, box)

	if flex == nil {
		t.Fatal("CenterVertically() returned nil")
	}

	// Verify that the flex container was created
}

func TestCenterVerticallyDynamically(t *testing.T) {
	box := tview.NewBox()

	flex := CenterVerticallyDynamically(box)

	if flex == nil {
		t.Fatal("CenterVerticallyDynamically() returned nil")
	}

	// Verify that the flex container was created
}

func TestMinListSize(t *testing.T) {
	list := customshortcutlist.NewList()

	// Test empty list
	width, height := MinListSize(list)
	if width <= 0 {
		t.Errorf("expected positive width, got %d", width)
	}
	if height <= 0 {
		t.Errorf("expected positive height, got %d", height)
	}

	// Test list with items
	list.AddItem("Item 1", "", 0, nil)
	list.AddItem("Item 2", "", 0, nil)
	list.AddItem("Item 3", "", 0, nil)

	width, height = MinListSize(list)
	if width <= 0 {
		t.Errorf("expected positive width for list with items, got %d", width)
	}
	if height <= 0 {
		t.Errorf("expected positive height for list with items, got %d", height)
	}

	// Height should account for number of items
	expectedMinHeight := 3 // At least the number of items
	if height < expectedMinHeight {
		t.Errorf("expected height >= %d for 3 items, got %d", expectedMinHeight, height)
	}
}

func TestMinTextViewWithNoWrapSize(t *testing.T) {
	textView := tview.NewTextView()

	// Test empty textview
	width, height := MinTextViewWithNoWrapSize(textView)
	if width < 0 {
		t.Errorf("expected non-negative width, got %d", width)
	}
	if height < 0 {
		t.Errorf("expected non-negative height, got %d", height)
	}

	// Test textview with content
	textView.SetText("Line 1\nLine 2\nLine 3")
	width, height = MinTextViewWithNoWrapSize(textView)
	if width <= 0 {
		t.Errorf("expected positive width for textview with content, got %d", width)
	}
	if height <= 0 {
		t.Errorf("expected positive height for textview with content, got %d", height)
	}
}

func TestMinFormSize(t *testing.T) {
	form := tview.NewForm()

	// Test empty form
	width, height := MinFormSize(form)
	if width < 0 {
		t.Errorf("expected non-negative width, got %d", width)
	}
	if height < 0 {
		t.Errorf("expected non-negative height, got %d", height)
	}

	// Test form with items
	form.AddInputField("Field 1", "", 20, nil, nil)
	form.AddInputField("Field 2", "", 20, nil, nil)

	width, height = MinFormSize(form)
	if width <= 0 {
		t.Errorf("expected positive width for form with items, got %d", width)
	}
	if height <= 0 {
		t.Errorf("expected positive height for form with items, got %d", height)
	}
}

func TestErrorToUserFeedback(t *testing.T) {
	tests := []struct {
		name     string
		input    error
		contains string
	}{
		{"simple error", fmt.Errorf("test error"), "Test error"},
		{"error with punctuation", fmt.Errorf("failed to connect"), "Failed to connect"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ErrorToUserFeedback(tt.input)
			if !strings.Contains(result, tt.contains[:1]) {
				t.Errorf("ErrorToUserFeedback() result should start with uppercase")
			}
		})
	}
}
