// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package customshortcutlist

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestNewList(t *testing.T) {
	list := NewList()

	if list == nil {
		t.Fatal("NewList() returned nil")
	}

	if list.Box == nil {
		t.Error("List.Box should not be nil")
	}

	// Check default values
	if !list.showSecondaryText {
		t.Error("expected showSecondaryText to be true by default")
	}

	if !list.wrapAround {
		t.Error("expected wrapAround to be true by default")
	}

	if list.currentItem != 0 {
		t.Errorf("expected currentItem to be 0, got %d", list.currentItem)
	}
}

func TestList_AddItem(t *testing.T) {
	list := NewList()

	result := list.AddItem("Item 1", "Secondary text", 'a', nil)

	if result != list {
		t.Error("AddItem() should return the same List instance for chaining")
	}

	if list.GetItemCount() != 1 {
		t.Errorf("expected 1 item, got %d", list.GetItemCount())
	}
}

func TestList_AddMultipleItems(t *testing.T) {
	list := NewList()

	list.AddItem("Item 1", "", 0, nil)
	list.AddItem("Item 2", "", 0, nil)
	list.AddItem("Item 3", "", 0, nil)

	if list.GetItemCount() != 3 {
		t.Errorf("expected 3 items, got %d", list.GetItemCount())
	}
}

func TestList_GetItemCount(t *testing.T) {
	list := NewList()

	// Empty list
	if list.GetItemCount() != 0 {
		t.Errorf("expected 0 items in empty list, got %d", list.GetItemCount())
	}

	// After adding items
	list.AddItem("Item 1", "", 0, nil)
	list.AddItem("Item 2", "", 0, nil)

	if list.GetItemCount() != 2 {
		t.Errorf("expected 2 items, got %d", list.GetItemCount())
	}
}

func TestList_GetItemText(t *testing.T) {
	list := NewList()
	list.AddItem("Main Text", "Secondary Text", 'a', nil)

	mainText, secondaryText := list.GetItemText(0)

	if mainText != "Main Text" {
		t.Errorf("expected mainText to be 'Main Text', got %q", mainText)
	}

	if secondaryText != "Secondary Text" {
		t.Errorf("expected secondaryText to be 'Secondary Text', got %q", secondaryText)
	}
}

func TestList_SetCurrentItem(t *testing.T) {
	list := NewList()
	list.AddItem("Item 1", "", 0, nil)
	list.AddItem("Item 2", "", 0, nil)
	list.AddItem("Item 3", "", 0, nil)

	result := list.SetCurrentItem(1)

	if result != list {
		t.Error("SetCurrentItem() should return the same List instance for chaining")
	}

	if list.GetCurrentItem() != 1 {
		t.Errorf("expected current item to be 1, got %d", list.GetCurrentItem())
	}
}

func TestList_SetCurrentItem_NegativeIndex(t *testing.T) {
	list := NewList()
	list.AddItem("Item 1", "", 0, nil)
	list.AddItem("Item 2", "", 0, nil)
	list.AddItem("Item 3", "", 0, nil)

	// -1 should select the last item
	list.SetCurrentItem(-1)

	if list.GetCurrentItem() != 2 {
		t.Errorf("expected current item to be 2 (last), got %d", list.GetCurrentItem())
	}
}

func TestList_SetCurrentItem_OutOfRange(t *testing.T) {
	list := NewList()
	list.AddItem("Item 1", "", 0, nil)
	list.AddItem("Item 2", "", 0, nil)

	// Index beyond range should clamp to last item
	list.SetCurrentItem(100)

	if list.GetCurrentItem() != 1 {
		t.Errorf("expected current item to be clamped to 1, got %d", list.GetCurrentItem())
	}
}

func TestList_GetCurrentItem(t *testing.T) {
	list := NewList()
	list.AddItem("Item 1", "", 0, nil)
	list.AddItem("Item 2", "", 0, nil)

	// Default should be 0
	if list.GetCurrentItem() != 0 {
		t.Errorf("expected current item to be 0, got %d", list.GetCurrentItem())
	}

	list.SetCurrentItem(1)

	if list.GetCurrentItem() != 1 {
		t.Errorf("expected current item to be 1, got %d", list.GetCurrentItem())
	}
}

func TestList_RemoveItem(t *testing.T) {
	list := NewList()
	list.AddItem("Item 1", "", 0, nil)
	list.AddItem("Item 2", "", 0, nil)
	list.AddItem("Item 3", "", 0, nil)

	result := list.RemoveItem(1)

	if result != list {
		t.Error("RemoveItem() should return the same List instance for chaining")
	}

	if list.GetItemCount() != 2 {
		t.Errorf("expected 2 items after removal, got %d", list.GetItemCount())
	}

	// Verify correct item was removed
	mainText, _ := list.GetItemText(1)
	if mainText != "Item 3" {
		t.Errorf("expected second item to be 'Item 3' after removal, got %q", mainText)
	}
}

func TestList_RemoveItem_EmptyList(t *testing.T) {
	list := NewList()

	// Should not panic on empty list
	list.RemoveItem(0)

	if list.GetItemCount() != 0 {
		t.Errorf("expected list to remain empty, got %d items", list.GetItemCount())
	}
}

func TestList_Clear(t *testing.T) {
	list := NewList()
	list.AddItem("Item 1", "", 0, nil)
	list.AddItem("Item 2", "", 0, nil)
	list.AddItem("Item 3", "", 0, nil)

	result := list.Clear()

	if result != list {
		t.Error("Clear() should return the same List instance for chaining")
	}

	if list.GetItemCount() != 0 {
		t.Errorf("expected 0 items after Clear(), got %d", list.GetItemCount())
	}
}

func TestList_SetMainTextColor(t *testing.T) {
	list := NewList()
	testColor := tcell.ColorRed

	result := list.SetMainTextColor(testColor)

	if result != list {
		t.Error("SetMainTextColor() should return the same List instance for chaining")
	}

	if list.mainTextColor != testColor {
		t.Errorf("expected mainTextColor to be %v, got %v", testColor, list.mainTextColor)
	}
}

func TestList_SetSecondaryTextColor(t *testing.T) {
	list := NewList()
	testColor := tcell.ColorGreen

	result := list.SetSecondaryTextColor(testColor)

	if result != list {
		t.Error("SetSecondaryTextColor() should return the same List instance for chaining")
	}

	if list.secondaryTextColor != testColor {
		t.Errorf("expected secondaryTextColor to be %v, got %v", testColor, list.secondaryTextColor)
	}
}

func TestList_SetShortcutColor(t *testing.T) {
	list := NewList()
	testColor := tcell.ColorBlue

	result := list.SetShortcutColor(testColor)

	if result != list {
		t.Error("SetShortcutColor() should return the same List instance for chaining")
	}

	if list.shortcutColor != testColor {
		t.Errorf("expected shortcutColor to be %v, got %v", testColor, list.shortcutColor)
	}
}

func TestList_SetSelectedTextColor(t *testing.T) {
	list := NewList()
	testColor := tcell.ColorYellow

	result := list.SetSelectedTextColor(testColor)

	if result != list {
		t.Error("SetSelectedTextColor() should return the same List instance for chaining")
	}

	if list.selectedTextColor != testColor {
		t.Errorf("expected selectedTextColor to be %v, got %v", testColor, list.selectedTextColor)
	}
}

func TestList_SetSelectedBackgroundColor(t *testing.T) {
	list := NewList()
	testColor := tcell.ColorBlue

	result := list.SetSelectedBackgroundColor(testColor)

	if result != list {
		t.Error("SetSelectedBackgroundColor() should return the same List instance for chaining")
	}

	if list.selectedBackgroundColor != testColor {
		t.Errorf("expected selectedBackgroundColor to be %v, got %v", testColor, list.selectedBackgroundColor)
	}
}

func TestList_ShowSecondaryText(t *testing.T) {
	list := NewList()

	result := list.ShowSecondaryText(false)

	if result != list {
		t.Error("ShowSecondaryText() should return the same List instance for chaining")
	}

	if list.showSecondaryText {
		t.Error("expected showSecondaryText to be false")
	}
}

func TestList_SetSelectedFunc(t *testing.T) {
	list := NewList()
	called := false
	var capturedIndex int

	callback := func(index int, mainText, secondaryText string, shortcut rune) {
		called = true
		capturedIndex = index
	}

	result := list.SetSelectedFunc(callback)

	if result != list {
		t.Error("SetSelectedFunc() should return the same List instance for chaining")
	}

	if list.selected == nil {
		t.Error("expected selected callback to be set")
	}

	// Trigger callback
	list.selected(1, "Test", "", 'a')
	if !called {
		t.Error("expected callback to be called")
	}
	if capturedIndex != 1 {
		t.Errorf("expected captured index to be 1, got %d", capturedIndex)
	}
}

func TestList_SetChangedFunc(t *testing.T) {
	list := NewList()
	called := false

	callback := func(index int, mainText, secondaryText string, shortcut rune) {
		called = true
	}

	result := list.SetChangedFunc(callback)

	if result != list {
		t.Error("SetChangedFunc() should return the same List instance for chaining")
	}

	// Add items and change selection to trigger callback
	list.AddItem("Item 1", "", 0, nil)
	list.AddItem("Item 2", "", 0, nil)
	list.SetCurrentItem(1)

	if !called {
		t.Error("expected changed callback to be called when selection changes")
	}
}

func TestList_SetDoneFunc(t *testing.T) {
	list := NewList()
	called := false

	callback := func() {
		called = true
	}

	result := list.SetDoneFunc(callback)

	if result != list {
		t.Error("SetDoneFunc() should return the same List instance for chaining")
	}

	if list.done == nil {
		t.Error("expected done callback to be set")
	}

	list.done()
	if !called {
		t.Error("expected callback to be called")
	}
}

func TestList_MethodChaining(t *testing.T) {
	list := NewList()

	result := list.
		SetMainTextColor(tcell.ColorRed).
		SetSecondaryTextColor(tcell.ColorGreen).
		SetShortcutColor(tcell.ColorBlue).
		ShowSecondaryText(false).
		AddItem("Test", "", 0, nil)

	if result != list {
		t.Error("method chaining should return the same List instance")
	}

	// Verify values were set
	if list.mainTextColor != tcell.ColorRed {
		t.Error("mainTextColor not set correctly during method chaining")
	}
	if list.secondaryTextColor != tcell.ColorGreen {
		t.Error("secondaryTextColor not set correctly during method chaining")
	}
	if list.shortcutColor != tcell.ColorBlue {
		t.Error("shortcutColor not set correctly during method chaining")
	}
	if list.showSecondaryText {
		t.Error("showSecondaryText not set correctly during method chaining")
	}
	if list.GetItemCount() != 1 {
		t.Error("item not added correctly during method chaining")
	}
}

func TestList_SetSelectedFocusOnly(t *testing.T) {
	list := NewList()

	result := list.SetSelectedFocusOnly(true)

	if result != list {
		t.Error("SetSelectedFocusOnly() should return the same List instance for chaining")
	}

	if !list.selectedFocusOnly {
		t.Error("expected selectedFocusOnly to be true")
	}

	list.SetSelectedFocusOnly(false)
	if list.selectedFocusOnly {
		t.Error("expected selectedFocusOnly to be false")
	}
}

func TestList_SetHighlightFullLine(t *testing.T) {
	list := NewList()

	result := list.SetHighlightFullLine(true)

	if result != list {
		t.Error("SetHighlightFullLine() should return the same List instance for chaining")
	}

	if !list.highlightFullLine {
		t.Error("expected highlightFullLine to be true")
	}

	list.SetHighlightFullLine(false)
	if list.highlightFullLine {
		t.Error("expected highlightFullLine to be false")
	}
}

func TestList_SetWrapAround(t *testing.T) {
	list := NewList()

	result := list.SetWrapAround(false)

	if result != list {
		t.Error("SetWrapAround() should return the same List instance for chaining")
	}

	if list.wrapAround {
		t.Error("expected wrapAround to be false")
	}

	list.SetWrapAround(true)
	if !list.wrapAround {
		t.Error("expected wrapAround to be true")
	}
}

func TestList_SetItemText(t *testing.T) {
	list := NewList()
	list.AddItem("Original Main", "Original Secondary", 0, nil)

	result := list.SetItemText(0, "New Main", "New Secondary")

	if result != list {
		t.Error("SetItemText() should return the same List instance for chaining")
	}

	main, secondary := list.GetItemText(0)
	if main != "New Main" {
		t.Errorf("expected main text to be 'New Main', got %q", main)
	}
	if secondary != "New Secondary" {
		t.Errorf("expected secondary text to be 'New Secondary', got %q", secondary)
	}
}

func TestList_InsertItem_Beginning(t *testing.T) {
	list := NewList()
	list.AddItem("Item 1", "", 0, nil)
	list.AddItem("Item 2", "", 0, nil)

	result := list.InsertItem(0, "New Item", "New Secondary", 'n', nil)

	if result != list {
		t.Error("InsertItem() should return the same List instance for chaining")
	}

	if list.GetItemCount() != 3 {
		t.Errorf("expected 3 items, got %d", list.GetItemCount())
	}

	main, _ := list.GetItemText(0)
	if main != "New Item" {
		t.Errorf("expected first item to be 'New Item', got %q", main)
	}
}

func TestList_InsertItem_Middle(t *testing.T) {
	list := NewList()
	list.AddItem("Item 1", "", 0, nil)
	list.AddItem("Item 2", "", 0, nil)
	list.AddItem("Item 3", "", 0, nil)

	list.InsertItem(1, "Inserted", "", 0, nil)

	if list.GetItemCount() != 4 {
		t.Errorf("expected 4 items, got %d", list.GetItemCount())
	}

	main, _ := list.GetItemText(1)
	if main != "Inserted" {
		t.Errorf("expected item at index 1 to be 'Inserted', got %q", main)
	}
}

func TestList_InsertItem_End(t *testing.T) {
	list := NewList()
	list.AddItem("Item 1", "", 0, nil)
	list.AddItem("Item 2", "", 0, nil)

	list.InsertItem(-1, "Last Item", "", 0, nil)

	if list.GetItemCount() != 3 {
		t.Errorf("expected 3 items, got %d", list.GetItemCount())
	}

	main, _ := list.GetItemText(2)
	if main != "Last Item" {
		t.Errorf("expected last item to be 'Last Item', got %q", main)
	}
}

func TestList_InsertItem_NegativeIndex(t *testing.T) {
	list := NewList()
	list.AddItem("Item 1", "", 0, nil)
	list.AddItem("Item 2", "", 0, nil)
	list.AddItem("Item 3", "", 0, nil)

	// -2 should insert before the last item
	list.InsertItem(-2, "Before Last", "", 0, nil)

	main, _ := list.GetItemText(2)
	if main != "Before Last" {
		t.Errorf("expected item at index 2 to be 'Before Last', got %q", main)
	}
}

func TestList_InsertItem_FirstItem_CallsChanged(t *testing.T) {
	list := NewList()
	called := false

	list.SetChangedFunc(func(index int, mainText, secondaryText string, shortcut rune) {
		called = true
	})

	list.InsertItem(0, "First", "Secondary", 'f', nil)

	if !called {
		t.Error("expected changed callback to be called when first item is inserted")
	}
}

func TestList_FindItems_MainText(t *testing.T) {
	list := NewList()
	list.AddItem("Apple", "", 0, nil)   // matches "Ap"
	list.AddItem("Banana", "", 0, nil)  // no match
	list.AddItem("Carrot", "", 0, nil)  // no match
	list.AddItem("Apricot", "", 0, nil) // matches "Ap"

	// When searching for "Ap" in main text and empty secondary text
	indices := list.FindItems("Ap", "", false, false)

	if len(indices) != 2 {
		t.Errorf("expected 2 matches, got %d", len(indices))
	}

	if len(indices) >= 2 {
		if indices[0] != 0 || indices[1] != 3 {
			t.Errorf("expected indices [0, 3], got %v", indices)
		}
	}
}

func TestList_FindItems_SecondaryText(t *testing.T) {
	list := NewList()
	list.AddItem("Apple", "Fruit", 0, nil)
	list.AddItem("Banana", "Fruit", 0, nil)
	list.AddItem("Carrot", "Vegetable", 0, nil)

	// When searching with empty main and "Fruit" secondary,
	// it will match all items because empty mainSearch matches everything
	// This is expected behavior based on the implementation
	indices := list.FindItems("", "Fruit", false, false)

	// All 3 items have non-empty mainText, so all match
	if len(indices) != 3 {
		t.Errorf("expected 3 matches, got %d", len(indices))
	}
}

func TestList_FindItems_Both(t *testing.T) {
	list := NewList()
	list.AddItem("Apple", "Fruit", 0, nil)
	list.AddItem("Banana", "Fruit", 0, nil)
	list.AddItem("Carrot", "Vegetable", 0, nil)

	indices := list.FindItems("Banana", "Fruit", true, false)

	if len(indices) != 1 {
		t.Errorf("expected 1 match, got %d", len(indices))
	}

	if len(indices) >= 1 && indices[0] != 1 {
		t.Errorf("expected index 1, got %d", indices[0])
	}
}

func TestList_FindItems_CaseInsensitive(t *testing.T) {
	list := NewList()
	list.AddItem("Apple", "", 0, nil)
	list.AddItem("Banana", "", 0, nil)

	// Case-insensitive search for "apple" should match "Apple"
	indices := list.FindItems("apple", "", false, true)

	if len(indices) != 1 {
		t.Errorf("expected 1 match with case-insensitive search, got %d", len(indices))
	}

	if len(indices) >= 1 && indices[0] != 0 {
		t.Errorf("expected index 0, got %d", indices[0])
	}
}

func TestList_FindItems_EmptySearch(t *testing.T) {
	list := NewList()
	list.AddItem("Apple", "Fruit", 0, nil)

	indices := list.FindItems("", "", false, false)

	if len(indices) != 0 {
		t.Errorf("expected 0 matches for empty search, got %d", len(indices))
	}
}

func TestList_RemoveItem_NegativeIndex(t *testing.T) {
	list := NewList()
	list.AddItem("Item 1", "", 0, nil)
	list.AddItem("Item 2", "", 0, nil)
	list.AddItem("Item 3", "", 0, nil)

	// -1 should remove the last item
	list.RemoveItem(-1)

	if list.GetItemCount() != 2 {
		t.Errorf("expected 2 items after removal, got %d", list.GetItemCount())
	}

	main, _ := list.GetItemText(1)
	if main != "Item 2" {
		t.Errorf("expected last item to be 'Item 2', got %q", main)
	}
}

func TestList_RemoveItem_CurrentItem(t *testing.T) {
	list := NewList()
	called := false

	list.SetChangedFunc(func(index int, mainText, secondaryText string, shortcut rune) {
		called = true
	})

	list.AddItem("Item 1", "", 0, nil)
	list.AddItem("Item 2", "", 0, nil)
	list.AddItem("Item 3", "", 0, nil)
	list.SetCurrentItem(1)

	called = false // Reset after SetCurrentItem
	list.RemoveItem(1)

	if !called {
		t.Error("expected changed callback to be called when current item is removed")
	}
}

func TestList_RemoveItem_AdjustsCurrentItem(t *testing.T) {
	list := NewList()
	list.AddItem("Item 1", "", 0, nil)
	list.AddItem("Item 2", "", 0, nil)
	list.AddItem("Item 3", "", 0, nil)

	list.SetCurrentItem(2)
	list.RemoveItem(0)

	if list.GetCurrentItem() != 1 {
		t.Errorf("expected current item to be adjusted to 1, got %d", list.GetCurrentItem())
	}
}

func TestList_InputHandler_Escape(t *testing.T) {
	list := NewList()
	called := false

	list.SetDoneFunc(func() {
		called = true
	})

	handler := list.InputHandler()
	event := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	handler(event, func(p tview.Primitive) {})

	if !called {
		t.Error("expected done callback to be called on Escape key")
	}
}

func TestList_InputHandler_Navigation(t *testing.T) {
	list := NewList()
	list.AddItem("Item 1", "", 0, nil)
	list.AddItem("Item 2", "", 0, nil)
	list.AddItem("Item 3", "", 0, nil)

	handler := list.InputHandler()

	// Test Down key
	event := tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
	handler(event, func(p tview.Primitive) {})

	if list.GetCurrentItem() != 1 {
		t.Errorf("expected current item to be 1 after Down key, got %d", list.GetCurrentItem())
	}

	// Test Up key
	event = tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
	handler(event, func(p tview.Primitive) {})

	if list.GetCurrentItem() != 0 {
		t.Errorf("expected current item to be 0 after Up key, got %d", list.GetCurrentItem())
	}
}

func TestList_InputHandler_Home_End(t *testing.T) {
	list := NewList()
	list.AddItem("Item 1", "", 0, nil)
	list.AddItem("Item 2", "", 0, nil)
	list.AddItem("Item 3", "", 0, nil)

	handler := list.InputHandler()

	// Test End key
	event := tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModNone)
	handler(event, func(p tview.Primitive) {})

	if list.GetCurrentItem() != 2 {
		t.Errorf("expected current item to be 2 after End key, got %d", list.GetCurrentItem())
	}

	// Test Home key
	event = tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModNone)
	handler(event, func(p tview.Primitive) {})

	if list.GetCurrentItem() != 0 {
		t.Errorf("expected current item to be 0 after Home key, got %d", list.GetCurrentItem())
	}
}

func TestList_InputHandler_Enter(t *testing.T) {
	list := NewList()
	itemCalled := false
	listCalled := false

	list.AddItem("Item 1", "", 0, func() {
		itemCalled = true
	})

	list.SetSelectedFunc(func(index int, mainText, secondaryText string, shortcut rune) {
		listCalled = true
	})

	handler := list.InputHandler()
	event := tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)
	handler(event, func(p tview.Primitive) {})

	if !itemCalled {
		t.Error("expected item's selected callback to be called on Enter key")
	}

	if !listCalled {
		t.Error("expected list's selected callback to be called on Enter key")
	}
}

func TestList_InputHandler_Shortcut(t *testing.T) {
	list := NewList()
	called := false

	list.AddItem("Item 1", "", 'a', nil)
	list.AddItem("Item 2", "", 'b', func() {
		called = true
	})

	handler := list.InputHandler()
	event := tcell.NewEventKey(tcell.KeyRune, 'b', tcell.ModNone)
	handler(event, func(p tview.Primitive) {})

	if !called {
		t.Error("expected item's selected callback to be called when shortcut is pressed")
	}

	if list.GetCurrentItem() != 1 {
		t.Errorf("expected current item to be 1 after shortcut, got %d", list.GetCurrentItem())
	}
}

func TestList_InputHandler_WrapAround(t *testing.T) {
	list := NewList()
	list.AddItem("Item 1", "", 0, nil)
	list.AddItem("Item 2", "", 0, nil)
	list.SetWrapAround(true)

	handler := list.InputHandler()

	// Go to last item
	list.SetCurrentItem(1)

	// Press Down - should wrap to first item
	event := tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
	handler(event, func(p tview.Primitive) {})

	if list.GetCurrentItem() != 0 {
		t.Errorf("expected current item to wrap to 0, got %d", list.GetCurrentItem())
	}
}

func TestList_InputHandler_NoWrapAround(t *testing.T) {
	list := NewList()
	list.AddItem("Item 1", "", 0, nil)
	list.AddItem("Item 2", "", 0, nil)
	list.SetWrapAround(false)

	handler := list.InputHandler()

	// Go to last item
	list.SetCurrentItem(1)

	// Press Down - should stay on last item
	event := tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
	handler(event, func(p tview.Primitive) {})

	if list.GetCurrentItem() != 1 {
		t.Errorf("expected current item to stay at 1, got %d", list.GetCurrentItem())
	}
}

func TestList_InputHandler_EmptyList(t *testing.T) {
	list := NewList()

	handler := list.InputHandler()
	event := tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)

	// Should not panic with empty list
	handler(event, func(p tview.Primitive) {})
}

func TestList_InputHandler_Space(t *testing.T) {
	list := NewList()
	called := false

	list.AddItem("Item 1", "", 0, func() {
		called = true
	})

	handler := list.InputHandler()
	event := tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone)
	handler(event, func(p tview.Primitive) {})

	if !called {
		t.Error("expected item's selected callback to be called on space key")
	}
}
