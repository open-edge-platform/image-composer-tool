// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package progressbar

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestNewProgressBar(t *testing.T) {
	pb := NewProgressBar()

	if pb == nil {
		t.Fatal("NewProgressBar() returned nil")
	}

	if pb.Box == nil {
		t.Error("ProgressBar.Box should not be nil")
	}

	// Check default values
	if pb.progress != 0 {
		t.Errorf("expected initial progress to be 0, got %d", pb.progress)
	}

	if pb.status != "" {
		t.Errorf("expected initial status to be empty, got %q", pb.status)
	}
}

func TestProgressBar_SetFillColor(t *testing.T) {
	pb := NewProgressBar()
	testColor := tcell.ColorGreen

	result := pb.SetFillColor(testColor)

	if result != pb {
		t.Error("SetFillColor() should return the same ProgressBar instance for chaining")
	}

	if pb.fillColor != testColor {
		t.Errorf("expected fillColor to be %v, got %v", testColor, pb.fillColor)
	}
}

func TestProgressBar_SetLabelColor(t *testing.T) {
	pb := NewProgressBar()
	testColor := tcell.ColorYellow

	result := pb.SetLabelColor(testColor)

	if result != pb {
		t.Error("SetLabelColor() should return the same ProgressBar instance for chaining")
	}

	if pb.labelColor != testColor {
		t.Errorf("expected labelColor to be %v, got %v", testColor, pb.labelColor)
	}
}

func TestProgressBar_GetHeight(t *testing.T) {
	pb := NewProgressBar()

	height := pb.GetHeight()

	if height != minProgressBarHeight {
		t.Errorf("expected height to be %d, got %d", minProgressBarHeight, height)
	}
}

func TestProgressBar_SetStatus(t *testing.T) {
	pb := NewProgressBar()
	testStatus := "Installing packages..."

	pb.SetStatus(testStatus)

	if pb.status != testStatus {
		t.Errorf("expected status to be %q, got %q", testStatus, pb.status)
	}
}

func TestProgressBar_SetProgress(t *testing.T) {
	pb := NewProgressBar()

	testCases := []int{0, 25, 50, 75, 100}

	for _, progress := range testCases {
		pb.SetProgress(progress)

		if pb.progress != progress {
			t.Errorf("expected progress to be %d, got %d", progress, pb.progress)
		}
	}
}

func TestProgressBar_SetChangedFunc(t *testing.T) {
	pb := NewProgressBar()
	callCount := 0

	callback := func() {
		callCount++
	}

	result := pb.SetChangedFunc(callback)

	if result != pb {
		t.Error("SetChangedFunc() should return the same ProgressBar instance for chaining")
	}

	// Test that callback is called when progress changes
	pb.SetProgress(50)
	if callCount != 1 {
		t.Errorf("expected callback to be called once after SetProgress, got %d calls", callCount)
	}

	// Test that callback is called when status changes
	pb.SetStatus("Test status")
	if callCount != 2 {
		t.Errorf("expected callback to be called twice after SetStatus, got %d calls", callCount)
	}
}

func TestProgressBar_SetChangedFunc_NilCallback(t *testing.T) {
	pb := NewProgressBar()

	// Should not panic with nil callback
	pb.SetChangedFunc(nil)

	// These should not panic even though changed is nil
	pb.SetProgress(50)
	pb.SetStatus("Test")
}

func TestProgressBar_MethodChaining(t *testing.T) {
	pb := NewProgressBar()
	called := false

	result := pb.
		SetFillColor(tcell.ColorBlue).
		SetLabelColor(tcell.ColorWhite).
		SetChangedFunc(func() { called = true })

	if result != pb {
		t.Error("method chaining should return the same ProgressBar instance")
	}

	// Verify values were set
	if pb.fillColor != tcell.ColorBlue {
		t.Error("fillColor not set correctly during method chaining")
	}
	if pb.labelColor != tcell.ColorWhite {
		t.Error("labelColor not set correctly during method chaining")
	}

	// Trigger callback
	pb.SetProgress(10)
	if !called {
		t.Error("callback not set correctly during method chaining")
	}
}

func TestProgressBar_ProgressBounds(t *testing.T) {
	pb := NewProgressBar()

	// Test edge cases
	testCases := []struct {
		name     string
		progress int
	}{
		{"zero progress", 0},
		{"full progress", 100},
		{"negative progress", -10},
		{"over 100 progress", 150},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pb.SetProgress(tc.progress)
			if pb.progress != tc.progress {
				t.Errorf("expected progress to be %d, got %d", tc.progress, pb.progress)
			}
		})
	}
}

func TestProgressBar_StatusUpdate(t *testing.T) {
	pb := NewProgressBar()

	statuses := []string{
		"",
		"Starting...",
		"Installing...",
		"Complete!",
	}

	for _, status := range statuses {
		pb.SetStatus(status)
		if pb.status != status {
			t.Errorf("expected status to be %q, got %q", status, pb.status)
		}
	}
}
