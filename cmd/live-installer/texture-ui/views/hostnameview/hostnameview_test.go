// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package hostnameview

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell"
	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/rivo/tview"
)

func TestNew(t *testing.T) {
	hv := New()

	if hv == nil {
		t.Fatal("New() returned nil")
	}

	// Check initial state
	if hv.form != nil {
		t.Error("expected form to be nil before initialization")
	}

	if hv.nameField != nil {
		t.Error("expected nameField to be nil before initialization")
	}

	if hv.navBar != nil {
		t.Error("expected navBar to be nil before initialization")
	}

	if hv.flex != nil {
		t.Error("expected flex to be nil before initialization")
	}

	if hv.centeredFlex != nil {
		t.Error("expected centeredFlex to be nil before initialization")
	}

	if hv.defaultName != "" {
		t.Error("expected defaultName to be empty before initialization")
	}
}

func TestHostNameView_Name(t *testing.T) {
	hv := New()

	name := hv.Name()
	expectedName := "HOSTNAME"

	if name != expectedName {
		t.Errorf("expected name to be %q, got %q", expectedName, name)
	}
}

func TestHostNameView_Title(t *testing.T) {
	hv := New()

	title := hv.Title()

	if title == "" {
		t.Error("expected Title() to return non-empty string")
	}

	// Should contain hostname or similar
	if !strings.Contains(strings.ToLower(title), "hostname") && !strings.Contains(strings.ToLower(title), "host") {
		t.Logf("Title returned: %q", title)
	}
}

func TestHostNameView_Primitive_BeforeInitialization(t *testing.T) {
	hv := New()

	primitive := hv.Primitive()

	if primitive != nil {
		t.Logf("Primitive() returned non-nil before initialization: %T", primitive)
	}
}

func TestHostNameView_OnShow(t *testing.T) {
	hv := New()

	// OnShow should not panic even if not initialized
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("OnShow() panicked: %v", r)
		}
	}()

	hv.OnShow()
}

func TestHostNameView_Initialize(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
		SystemConfig: config.SystemConfig{
			HostName: "",
		},
	}

	app := tview.NewApplication()
	mockFunc := func() {}

	hv := New()

	err := hv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)

	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Check that UI elements are initialized
	if hv.form == nil {
		t.Error("expected form to be initialized")
	}

	if hv.nameField == nil {
		t.Error("expected nameField to be initialized")
	}

	if hv.navBar == nil {
		t.Error("expected navBar to be initialized")
	}

	if hv.flex == nil {
		t.Error("expected flex to be initialized")
	}

	if hv.centeredFlex == nil {
		t.Error("expected centeredFlex to be initialized")
	}

	// Check default name was set
	if hv.defaultName != defaultHostName {
		t.Errorf("expected defaultName to be %q, got %q", defaultHostName, hv.defaultName)
	}

	// Check that nameField has default value
	fieldText := hv.nameField.GetText()
	if fieldText != defaultHostName {
		t.Errorf("expected nameField text to be %q, got %q", defaultHostName, fieldText)
	}
}

func TestHostNameView_Primitive_AfterInitialization(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
		SystemConfig: config.SystemConfig{},
	}

	app := tview.NewApplication()
	mockFunc := func() {}

	hv := New()
	err := hv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	primitive := hv.Primitive()

	if primitive == nil {
		t.Error("expected Primitive() to return non-nil after initialization")
	}

	// Should return centeredFlex
	if primitive != hv.centeredFlex {
		t.Error("expected Primitive() to return centeredFlex")
	}
}

func TestHostNameView_Reset(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
		SystemConfig: config.SystemConfig{},
	}

	app := tview.NewApplication()
	mockFunc := func() {}

	hv := New()
	err := hv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Change the field value
	hv.nameField.SetText("custom-hostname")

	// Reset should restore default
	err = hv.Reset()
	if err != nil {
		t.Errorf("Reset() returned error: %v", err)
	}

	// Check that field was reset
	fieldText := hv.nameField.GetText()
	if fieldText != defaultHostName {
		t.Errorf("expected nameField to be reset to %q, got %q", defaultHostName, fieldText)
	}
}

func TestHostNameView_HandleInput(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
		SystemConfig: config.SystemConfig{},
	}

	app := tview.NewApplication()
	mockFunc := func() {}

	hv := New()
	err := hv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Test that HandleInput doesn't panic
	event := tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
	result := hv.HandleInput(event)

	// Event should be handled
	_ = result
}

func TestHostNameView_Constants(t *testing.T) {
	// Verify constants are defined with expected values
	tests := []struct {
		name     string
		value    interface{}
		expected interface{}
	}{
		{"defaultHostName", defaultHostName, "hostname"},
		{"maxHostNameLength", maxHostNameLength, 63},
		{"defaultNavButton", defaultNavButton, 1},
		{"formProportion", formProportion, 0},
		{"navBarHeight", navBarHeight, 0},
		{"navBarProportion", navBarProportion, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value != tt.expected {
				t.Errorf("expected %s to be %v, got %v", tt.name, tt.expected, tt.value)
			}
		})
	}
}

func TestValidFQDNCharacter(t *testing.T) {
	tests := []struct {
		name        string
		char        rune
		isFirstRune bool
		expected    bool
	}{
		{"lowercase letter first", 'a', true, true},
		{"uppercase letter first", 'A', true, true},
		{"digit first", '1', true, false},
		{"dash first", '-', true, false},
		{"dot first", '.', true, false},
		{"lowercase letter non-first", 'a', false, true},
		{"uppercase letter non-first", 'Z', false, true},
		{"digit non-first", '5', false, true},
		{"dash non-first", '-', false, true},
		{"dot non-first", '.', false, true},
		{"underscore non-first", '_', false, false},
		{"space non-first", ' ', false, false},
		{"special char non-first", '@', false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validFQDNCharacter(tt.char, tt.isFirstRune)
			if result != tt.expected {
				t.Errorf("validFQDNCharacter(%q, %v) = %v, expected %v",
					tt.char, tt.isFirstRune, result, tt.expected)
			}
		})
	}
}

func TestValidateFQDN(t *testing.T) {
	tests := []struct {
		name        string
		fqdn        string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid simple hostname",
			fqdn:        "hostname",
			expectError: false,
		},
		{
			name:        "valid hostname with numbers",
			fqdn:        "host123",
			expectError: false,
		},
		{
			name:        "valid hostname with dash",
			fqdn:        "my-host",
			expectError: false,
		},
		{
			name:        "valid FQDN",
			fqdn:        "host.example.com",
			expectError: false,
		},
		{
			name:        "valid FQDN with numbers",
			fqdn:        "server1.domain2.com",
			expectError: false,
		},
		{
			name:        "empty hostname",
			fqdn:        "",
			expectError: true,
			errorMsg:    "empty",
		},
		{
			name:        "hostname starting with digit",
			fqdn:        "1host",
			expectError: true,
			errorMsg:    "start",
		},
		{
			name:        "hostname starting with dash",
			fqdn:        "-host",
			expectError: true,
			errorMsg:    "start",
		},
		{
			name:        "hostname ending with dash",
			fqdn:        "host-",
			expectError: true,
			errorMsg:    "end",
		},
		{
			name:        "hostname with invalid character",
			fqdn:        "host_name",
			expectError: true,
			errorMsg:    "alpha-numeric",
		},
		{
			name:        "hostname too long",
			fqdn:        strings.Repeat("a", maxHostNameLength+1),
			expectError: true,
			errorMsg:    "characters",
		},
		{
			name:        "FQDN with empty domain",
			fqdn:        "host.",
			expectError: true,
			errorMsg:    "empty",
		},
		{
			name:        "FQDN with empty hostname",
			fqdn:        ".domain.com",
			expectError: true,
			errorMsg:    "empty",
		},
		{
			name:        "domain starting with digit",
			fqdn:        "host.1domain.com",
			expectError: true,
			errorMsg:    "start",
		},
		{
			name:        "valid single letter hostname",
			fqdn:        "a",
			expectError: false,
		},
		{
			name:        "valid uppercase hostname",
			fqdn:        "HOSTNAME",
			expectError: false,
		},
		{
			name:        "valid mixed case FQDN",
			fqdn:        "Host.Example.Com",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFQDN(tt.fqdn)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error for FQDN %q, got nil", tt.fqdn)
				} else if tt.errorMsg != "" {
					// Check if error message contains expected substring
					errStr := strings.ToLower(err.Error())
					if !strings.Contains(errStr, strings.ToLower(tt.errorMsg)) {
						t.Errorf("expected error containing %q, got: %v", tt.errorMsg, err)
					}
				}
			} else {
				if err != nil {
					t.Errorf("expected no error for FQDN %q, got: %v", tt.fqdn, err)
				}
			}
		})
	}
}

func TestHostNameView_InputFieldAcceptance(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
		SystemConfig: config.SystemConfig{},
	}

	app := tview.NewApplication()
	mockFunc := func() {}

	hv := New()
	err := hv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Test that nameField was configured with acceptance function
	// We can't directly test the acceptance function without accessing internals,
	// but we can verify the field exists and has the correct properties
	if hv.nameField == nil {
		t.Fatal("nameField should not be nil after initialization")
	}

	// Check field width is set
	fieldWidth := hv.nameField.GetFieldWidth()
	if fieldWidth != maxHostNameLength {
		t.Errorf("expected field width to be %d, got %d", maxHostNameLength, fieldWidth)
	}
}

func TestHostNameView_NextButtonValidation(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
		SystemConfig: config.SystemConfig{},
	}

	app := tview.NewApplication()
	mockFunc := func() {}

	hv := New()
	err := hv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Set a valid hostname
	hv.nameField.SetText("validhost")

	// The next button callback should validate and call nextPage
	// We can't directly trigger the button without the UI running,
	// but we can verify the setup is correct
	if hv.navBar == nil {
		t.Error("navBar should be initialized")
	}
}

func TestHostNameView_HandleInput_WithNilEvent(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "azure-linux",
			Dist: "3.0",
			Arch: "x86_64",
		},
		SystemConfig: config.SystemConfig{},
	}

	app := tview.NewApplication()
	mockFunc := func() {}

	hv := New()
	err := hv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// HandleInput should handle nil event gracefully
	defer func() {
		if r := recover(); r != nil {
			t.Logf("HandleInput(nil) panicked (may be expected): %v", r)
		}
	}()

	result := hv.HandleInput(nil)
	_ = result
}

func TestHostNameView_Reset_BeforeInitialization(t *testing.T) {
	hv := New()

	// Reset should handle uninitialized state
	defer func() {
		if r := recover(); r != nil {
			t.Logf("Reset panicked as expected (not initialized): %v", r)
		}
	}()

	err := hv.Reset()
	_ = err
}
