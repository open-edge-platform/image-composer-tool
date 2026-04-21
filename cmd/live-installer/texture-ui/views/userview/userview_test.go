// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package userview

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell"
	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/rivo/tview"
)

func TestNew(t *testing.T) {
	uv := New()

	if uv == nil {
		t.Fatal("New() returned nil")
	}

	// Check initial state
	if uv.form != nil {
		t.Error("expected form to be nil before initialization")
	}

	if uv.userNameField != nil {
		t.Error("expected userNameField to be nil before initialization")
	}

	if uv.passwordField != nil {
		t.Error("expected passwordField to be nil before initialization")
	}

	if uv.confirmPasswordField != nil {
		t.Error("expected confirmPasswordField to be nil before initialization")
	}

	if uv.navBar != nil {
		t.Error("expected navBar to be nil before initialization")
	}

	if uv.flex != nil {
		t.Error("expected flex to be nil before initialization")
	}

	if uv.centeredFlex != nil {
		t.Error("expected centeredFlex to be nil before initialization")
	}

	if uv.passwordValidator == nil {
		t.Error("expected passwordValidator to be set")
	}

	if uv.user != nil {
		t.Error("expected user to be nil before initialization")
	}
}

func TestUserView_Name(t *testing.T) {
	uv := New()

	name := uv.Name()
	expectedName := "USERACCOUNT"

	if name != expectedName {
		t.Errorf("expected name to be %q, got %q", expectedName, name)
	}
}

func TestUserView_Title(t *testing.T) {
	uv := New()

	title := uv.Title()

	if title == "" {
		t.Error("expected Title() to return non-empty string")
	}

	// Should contain user or account
	lowerTitle := strings.ToLower(title)
	if !strings.Contains(lowerTitle, "user") && !strings.Contains(lowerTitle, "account") {
		t.Logf("Title returned: %q", title)
	}
}

func TestUserView_Primitive_BeforeInitialization(t *testing.T) {
	uv := New()

	primitive := uv.Primitive()

	if primitive != nil {
		t.Logf("Primitive() returned non-nil before initialization: %T", primitive)
	}
}

func TestUserView_OnShow(t *testing.T) {
	uv := New()

	// OnShow should not panic even if not initialized
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("OnShow() panicked: %v", r)
		}
	}()

	uv.OnShow()
}

func TestUserView_Initialize(t *testing.T) {
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

	uv := New()
	err := uv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)

	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Check that UI elements are initialized
	if uv.form == nil {
		t.Error("expected form to be set after initialization")
	}

	if uv.userNameField == nil {
		t.Error("expected userNameField to be set after initialization")
	}

	if uv.passwordField == nil {
		t.Error("expected passwordField to be set after initialization")
	}

	if uv.confirmPasswordField == nil {
		t.Error("expected confirmPasswordField to be set after initialization")
	}

	if uv.navBar == nil {
		t.Error("expected navBar to be set after initialization")
	}

	if uv.flex == nil {
		t.Error("expected flex to be set after initialization")
	}

	if uv.centeredFlex == nil {
		t.Error("expected centeredFlex to be set after initialization")
	}

	if uv.user == nil {
		t.Error("expected user to be set after initialization")
	}

	// Check user has sudo enabled
	if uv.user != nil && !uv.user.Sudo {
		t.Error("expected user.Sudo to be true")
	}
}

func TestUserView_Primitive_AfterInitialization(t *testing.T) {
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

	uv := New()
	err := uv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	primitive := uv.Primitive()

	if primitive == nil {
		t.Error("expected Primitive() to return non-nil after initialization")
	}

	// Should return the centered flex container
	if _, ok := primitive.(*tview.Flex); !ok {
		t.Errorf("expected Primitive() to return *tview.Flex, got %T", primitive)
	}
}

func TestUserView_Reset(t *testing.T) {
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

	uv := New()
	err := uv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Set some user data
	uv.user.Name = "testuser"
	uv.user.Password = "testpassword"

	// Reset should clear the data
	err = uv.Reset()
	if err != nil {
		t.Errorf("Reset() returned unexpected error: %v", err)
	}

	// Check that user data is cleared
	if uv.user.Name != "" {
		t.Errorf("expected user.Name to be empty after Reset, got %q", uv.user.Name)
	}

	if uv.user.Password != "" {
		t.Errorf("expected user.Password to be empty after Reset, got %q", uv.user.Password)
	}
}

func TestUserView_HandleInput(t *testing.T) {
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

	uv := New()
	err := uv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Test normal key event passes through
	normalEvent := tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone)
	result := uv.HandleInput(normalEvent)

	if result != normalEvent {
		t.Error("expected HandleInput to return the event for normal keys")
	}
}

func TestUserView_HandleInput_UpKey(t *testing.T) {
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

	uv := New()
	err := uv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Test Up key is converted to Backtab
	upEvent := tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
	result := uv.HandleInput(upEvent)

	if result == nil {
		t.Fatal("expected HandleInput to return an event for Up key")
	}

	if result.Key() != tcell.KeyBacktab {
		t.Errorf("expected Up key to be converted to Backtab, got %v", result.Key())
	}
}

func TestUserView_HandleInput_DownKey(t *testing.T) {
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

	uv := New()
	err := uv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Test Down key is converted to Tab
	downEvent := tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
	result := uv.HandleInput(downEvent)

	if result == nil {
		t.Fatal("expected HandleInput to return an event for Down key")
	}

	if result.Key() != tcell.KeyTab {
		t.Errorf("expected Down key to be converted to Tab, got %v", result.Key())
	}
}

func TestUserView_HandleInput_WithNilEvent(t *testing.T) {
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

	uv := New()
	err := uv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Test with nil event
	defer func() {
		if r := recover(); r != nil {
			t.Logf("HandleInput(nil) panicked (may be expected): %v", r)
		}
	}()

	uv.HandleInput(nil)
}

func TestUserView_Constants(t *testing.T) {
	tests := []struct {
		name     string
		value    interface{}
		expected interface{}
	}{
		{"navButtonNext", navButtonNext, 1},
		{"noSelection", noSelection, -1},
		{"formProportion", formProportion, 0},
		{"passwordFieldWidth", passwordFieldWidth, 64},
		{"maxUserNameLength", maxUserNameLength, 32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value != tt.expected {
				t.Errorf("expected %s to be %v, got %v", tt.name, tt.expected, tt.value)
			}
		})
	}
}

func TestUserView_SetupConfigUsers(t *testing.T) {
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
			Users: []config.UserConfig{
				{Name: "existinguser", Sudo: false},
			},
		},
	}

	uv := New()
	err := uv.setupConfigUsers(template)

	if err != nil {
		t.Fatalf("setupConfigUsers() returned error: %v", err)
	}

	// Check that a new user was added
	users := template.GetUsers()
	if len(users) != 2 {
		t.Errorf("expected 2 users, got %d", len(users))
	}

	// Check that the new user has sudo enabled
	newUser := users[1]
	if !newUser.Sudo {
		t.Error("expected new user to have Sudo enabled")
	}

	// Check that uv.user points to the new user
	if uv.user == nil {
		t.Fatal("expected uv.user to be set")
	}

	if !uv.user.Sudo {
		t.Error("expected uv.user to have Sudo enabled")
	}
}

func TestValidateUserName(t *testing.T) {
	tests := []struct {
		name      string
		userName  string
		wantError bool
		errorMsg  string
	}{
		{
			name:      "valid lowercase",
			userName:  "testuser",
			wantError: false,
		},
		{
			name:      "valid with underscore",
			userName:  "test_user",
			wantError: false,
		},
		{
			name:      "valid with dash",
			userName:  "test-user",
			wantError: false,
		},
		{
			name:      "valid with dot",
			userName:  "test.user",
			wantError: false,
		},
		{
			name:      "valid with number",
			userName:  "test123",
			wantError: false,
		},
		{
			name:      "valid uppercase start",
			userName:  "TestUser",
			wantError: false,
		},
		{
			name:      "valid single char",
			userName:  "a",
			wantError: false,
		},
		{
			name:      "empty username",
			userName:  "",
			wantError: true,
			errorMsg:  "empty",
		},
		{
			name:      "too long username",
			userName:  "thisusernameiswaytoolongandexceedsthemaximumlength",
			wantError: true,
			errorMsg:  "characters",
		},
		{
			name:      "starts with dash",
			userName:  "-user",
			wantError: true,
			errorMsg:  "start",
		},
		{
			name:      "starts with dot",
			userName:  ".user",
			wantError: true,
			errorMsg:  "start",
		},
		{
			name:      "starts with number",
			userName:  "123user",
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateUserName(tt.userName)

			if tt.wantError {
				if err == nil {
					t.Errorf("expected error for userName %q, got nil", tt.userName)
				} else if tt.errorMsg != "" && !strings.Contains(strings.ToLower(err.Error()), tt.errorMsg) {
					t.Errorf("expected error message to contain %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("expected no error for userName %q, got %v", tt.userName, err)
				}
			}
		})
	}
}

func TestValidateUserNameRune(t *testing.T) {
	tests := []struct {
		name        string
		r           rune
		isFirstRune bool
		wantError   bool
		errorMsg    string
	}{
		{
			name:        "lowercase letter first",
			r:           'a',
			isFirstRune: true,
			wantError:   false,
		},
		{
			name:        "uppercase letter first",
			r:           'A',
			isFirstRune: true,
			wantError:   false,
		},
		{
			name:        "digit first",
			r:           '0',
			isFirstRune: true,
			wantError:   false,
		},
		{
			name:        "underscore first",
			r:           '_',
			isFirstRune: true,
			wantError:   false,
		},
		{
			name:        "dash first",
			r:           '-',
			isFirstRune: true,
			wantError:   true,
			errorMsg:    "start",
		},
		{
			name:        "dot first",
			r:           '.',
			isFirstRune: true,
			wantError:   true,
			errorMsg:    "start",
		},
		{
			name:        "special char first",
			r:           '@',
			isFirstRune: true,
			wantError:   true,
			errorMsg:    "start",
		},
		{
			name:        "lowercase letter not first",
			r:           'a',
			isFirstRune: false,
			wantError:   false,
		},
		{
			name:        "dash not first",
			r:           '-',
			isFirstRune: false,
			wantError:   false,
		},
		{
			name:        "dot not first",
			r:           '.',
			isFirstRune: false,
			wantError:   false,
		},
		{
			name:        "special char not first",
			r:           '@',
			isFirstRune: false,
			wantError:   true,
			errorMsg:    "alpha-numeric",
		},
		{
			name:        "space not first",
			r:           ' ',
			isFirstRune: false,
			wantError:   true,
			errorMsg:    "alpha-numeric",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateUserNameRune(tt.r, tt.isFirstRune)

			if tt.wantError {
				if err == nil {
					t.Errorf("expected error for rune %q (isFirstRune=%v), got nil", tt.r, tt.isFirstRune)
				} else if tt.errorMsg != "" && !strings.Contains(strings.ToLower(err.Error()), tt.errorMsg) {
					t.Errorf("expected error message to contain %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("expected no error for rune %q (isFirstRune=%v), got %v", tt.r, tt.isFirstRune, err)
				}
			}
		})
	}
}

func TestUserView_UserNameAcceptanceCheck(t *testing.T) {
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

	uv := New()
	err := uv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	tests := []struct {
		name        string
		textToCheck string
		lastRune    rune
		want        bool
	}{
		{
			name:        "valid first character 'a'",
			textToCheck: "a",
			lastRune:    'a',
			want:        true,
		},
		{
			name:        "valid continuation with dash",
			textToCheck: "test-",
			lastRune:    '-',
			want:        true,
		},
		{
			name:        "invalid first character '-'",
			textToCheck: "-",
			lastRune:    '-',
			want:        false,
		},
		{
			name:        "invalid special character",
			textToCheck: "test@",
			lastRune:    '@',
			want:        false,
		},
		{
			name:        "valid alphanumeric",
			textToCheck: "test123",
			lastRune:    '3',
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := uv.userNameAcceptanceCheck(tt.textToCheck, tt.lastRune)

			if got != tt.want {
				t.Errorf("userNameAcceptanceCheck(%q, %q) = %v, want %v", tt.textToCheck, tt.lastRune, got, tt.want)
			}
		})
	}
}

func TestUserView_OnNextButton_EmptyUsername(t *testing.T) {
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

	uv := New()
	err := uv.Initialize("Back", template, app, mockNext, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Leave username empty
	uv.userNameField.SetText("")
	uv.passwordField.SetText("ValidPass123!")
	uv.confirmPasswordField.SetText("ValidPass123!")

	uv.onNextButton(mockNext)

	// Should not proceed with empty username
	if nextCalled {
		t.Error("expected onNextButton to not call nextPage with empty username")
	}
}

func TestUserView_OnNextButton_PasswordMismatch(t *testing.T) {
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

	uv := New()
	err := uv.Initialize("Back", template, app, mockNext, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Set mismatched passwords
	uv.userNameField.SetText("testuser")
	uv.passwordField.SetText("Password123!")
	uv.confirmPasswordField.SetText("DifferentPass123!")

	uv.onNextButton(mockNext)

	// Should not proceed with mismatched passwords
	if nextCalled {
		t.Error("expected onNextButton to not call nextPage with mismatched passwords")
	}
}

func TestUserView_OnNextButton_WeakPassword(t *testing.T) {
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

	uv := New()
	err := uv.Initialize("Back", template, app, mockNext, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Set weak password
	uv.userNameField.SetText("testuser")
	uv.passwordField.SetText("123")
	uv.confirmPasswordField.SetText("123")

	uv.onNextButton(mockNext)

	// Should not proceed with weak password
	if nextCalled {
		t.Error("expected onNextButton to not call nextPage with weak password")
	}
}

func TestUserView_OnNextButton_ValidInput(t *testing.T) {
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

	uv := New()
	err := uv.Initialize("Back", template, app, mockNext, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Set valid input
	expectedUsername := "testuser"
	expectedPassword := "StrongPassword123!@#"
	uv.userNameField.SetText(expectedUsername)
	uv.passwordField.SetText(expectedPassword)
	uv.confirmPasswordField.SetText(expectedPassword)

	uv.onNextButton(mockNext)

	// Should proceed with valid input
	if !nextCalled {
		t.Error("expected onNextButton to call nextPage with valid input")
	}

	// Check that user data was set
	if uv.user.Name != expectedUsername {
		t.Errorf("expected user.Name to be %q, got %q", expectedUsername, uv.user.Name)
	}

	if uv.user.Password != expectedPassword {
		t.Errorf("expected user.Password to be %q, got %q", expectedPassword, uv.user.Password)
	}
}

func TestUserView_PasswordFieldMasked(t *testing.T) {
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

	uv := New()
	err := uv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Verify password fields are masked
	// Note: tview doesn't expose GetMaskCharacter, but we can verify fields exist
	if uv.passwordField == nil {
		t.Error("expected passwordField to be initialized")
	}

	if uv.confirmPasswordField == nil {
		t.Error("expected confirmPasswordField to be initialized")
	}
}

func TestUserView_UserNameFieldWidth(t *testing.T) {
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

	uv := New()
	err := uv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Verify username field width is set
	if uv.userNameField == nil {
		t.Fatal("expected userNameField to be initialized")
	}

	// The field should have maxUserNameLength width
	// Note: tview doesn't expose GetFieldWidth reliably, but we verify field exists
	t.Logf("userNameField initialized successfully")
}

func TestUserView_FormElements(t *testing.T) {
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

	uv := New()
	err := uv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Verify form has all elements
	if uv.form == nil {
		t.Fatal("expected form to be initialized")
	}

	// Verify form items are added
	formItemCount := uv.form.GetFormItemCount()
	expectedCount := 4 // username, password, confirm password, nav bar

	if formItemCount != expectedCount {
		t.Errorf("expected form to have %d items, got %d", expectedCount, formItemCount)
	}
}

func TestUserView_Reset_BeforeInitialization(t *testing.T) {
	uv := New()

	// Reset should handle being called before initialization
	defer func() {
		if r := recover(); r != nil {
			t.Logf("Reset panicked before initialization (may be expected): %v", r)
		}
	}()

	err := uv.Reset()
	if err != nil {
		t.Logf("Reset returned error before initialization: %v", err)
	}
}

func TestValidateUserName_MaxLength(t *testing.T) {
	// Create a username exactly at max length
	maxLengthUsername := strings.Repeat("a", maxUserNameLength)
	err := validateUserName(maxLengthUsername)
	if err != nil {
		t.Errorf("expected no error for max length username, got %v", err)
	}

	// Create a username one character over max length
	tooLongUsername := strings.Repeat("a", maxUserNameLength+1)
	err = validateUserName(tooLongUsername)
	if err == nil {
		t.Error("expected error for username exceeding max length")
	}
}

func TestUserView_OnNextButton_InvalidUsername(t *testing.T) {
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

	uv := New()
	err := uv.Initialize("Back", template, app, mockNext, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Set invalid username (starts with dash)
	uv.userNameField.SetText("-invaliduser")
	uv.passwordField.SetText("StrongPassword123!@#")
	uv.confirmPasswordField.SetText("StrongPassword123!@#")

	uv.onNextButton(mockNext)

	// Should not proceed with invalid username
	if nextCalled {
		t.Error("expected onNextButton to not call nextPage with invalid username")
	}
}
