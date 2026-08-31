package api

import (
	"testing"
	"time"
)

func TestSessionManager_Create(t *testing.T) {
	config := SessionConfig{
		Timeout:         10 * time.Minute,
		MaxSessions:     2,
		CleanupInterval: 1 * time.Minute,
	}
	sm := NewSessionManager(config)
	defer sm.Stop()

	session1, err := sm.Create()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if session1.ID == "" {
		t.Errorf("expected session ID to be non-empty")
	}

	session2, err := sm.Create()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Should hit max sessions
	_, err = sm.Create()
	if err == nil {
		t.Errorf("expected error for max sessions reached, got nil")
	}

	if session1.ID == session2.ID {
		t.Errorf("expected unique session IDs")
	}
}

func TestSessionManager_GetAndDelete(t *testing.T) {
	config := DefaultSessionConfig()
	sm := NewSessionManager(config)
	defer sm.Stop()

	session, _ := sm.Create()

	// Test Get
	retrieved, err := sm.Get(session.ID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if retrieved.ID != session.ID {
		t.Errorf("expected ID %s, got %s", session.ID, retrieved.ID)
	}

	// Test Delete
	err = sm.Delete(session.ID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	_, err = sm.Get(session.ID)
	if err == nil {
		t.Errorf("expected error getting deleted session")
	}
}

func TestSessionManager_TimeoutCleanup(t *testing.T) {
	config := SessionConfig{
		Timeout:         50 * time.Millisecond,
		MaxSessions:     10,
		CleanupInterval: 10 * time.Millisecond,
	}
	sm := NewSessionManager(config)
	defer sm.Stop()

	session, _ := sm.Create()

	// Wait for session to expire and cleanup to run
	time.Sleep(100 * time.Millisecond)

	_, err := sm.Get(session.ID)
	if err == nil {
		t.Errorf("expected error getting expired session")
	}
	if sessErr, ok := err.(*SessionError); ok {
		if sessErr.Code != ErrCodeSessionNotFound {
			// Note: If cleanup runs before Get, it deletes the session completely (NotFound).
			// If Get is called after Timeout but before cleanup, it returns SessionExpired.
			t.Errorf("expected NotFound or Expired, got %v", sessErr.Code)
		}
	}
}

func TestSessionManager_Touch(t *testing.T) {
	config := DefaultSessionConfig()
	sm := NewSessionManager(config)
	defer sm.Stop()

	session, _ := sm.Create()
	originalActiveAt := session.LastActiveAt

	time.Sleep(10 * time.Millisecond)
	sm.Touch(session.ID)

	retrieved, _ := sm.Get(session.ID)
	if !retrieved.LastActiveAt.After(originalActiveAt) {
		t.Errorf("expected LastActiveAt to be updated")
	}
}

func TestSessionManager_IsRefinement(t *testing.T) {
	config := DefaultSessionConfig()
	sm := NewSessionManager(config)
	defer sm.Stop()

	session, _ := sm.Create()

	// New session should not be a refinement.
	if sm.IsRefinement(session.ID) {
		t.Error("expected IsRefinement to be false for new session")
	}

	// After setting a template, it should be a refinement.
	sm.UpdateTemplate(session.ID, &GeneratedTemplate{
		YAML:             "image:\n  name: test\n",
		ValidationStatus: "unchecked",
		LastModified:     time.Now(),
	})

	if !sm.IsRefinement(session.ID) {
		t.Error("expected IsRefinement to be true after setting template")
	}

	// Non-existent session should not be a refinement.
	if sm.IsRefinement("s_nonexist") {
		t.Error("expected IsRefinement to be false for non-existent session")
	}
}

func TestSessionManager_UpdateTemplate(t *testing.T) {
	config := DefaultSessionConfig()
	sm := NewSessionManager(config)
	defer sm.Stop()

	session, _ := sm.Create()

	tmpl := &GeneratedTemplate{
		YAML:             "image:\n  name: updated\n",
		SourceTemplates:  []string{"base.yml"},
		ValidationStatus: "valid",
		LastModified:     time.Now(),
	}
	sm.UpdateTemplate(session.ID, tmpl)

	retrieved, err := sm.Get(session.ID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if retrieved.CurrentTemplate == nil {
		t.Fatal("expected CurrentTemplate to be set")
	}
	if retrieved.CurrentTemplate.YAML != tmpl.YAML {
		t.Errorf("expected YAML %q, got %q", tmpl.YAML, retrieved.CurrentTemplate.YAML)
	}
	if retrieved.CurrentTemplate.ValidationStatus != "valid" {
		t.Errorf("expected validation status 'valid', got %q", retrieved.CurrentTemplate.ValidationStatus)
	}

	// UpdateTemplate on a non-existent session should be a no-op (no panic).
	sm.UpdateTemplate("s_nonexist", tmpl)
}

func TestSessionManager_AddMessage(t *testing.T) {
	config := DefaultSessionConfig()
	sm := NewSessionManager(config)
	defer sm.Stop()

	session, _ := sm.Create()

	userMsg := Message{
		Role:      "user",
		Content:   "create an nginx image",
		Timestamp: time.Now(),
	}
	sm.AddMessage(session.ID, userMsg)

	asstMsg := Message{
		Role:             "assistant",
		Content:          "",
		TemplateSnapshot: "image:\n  name: nginx\n",
		Timestamp:        time.Now(),
	}
	sm.AddMessage(session.ID, asstMsg)

	retrieved, err := sm.Get(session.ID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(retrieved.History) != 2 {
		t.Fatalf("expected 2 messages in history, got %d", len(retrieved.History))
	}
	if retrieved.History[0].Role != "user" {
		t.Errorf("expected first message role 'user', got %q", retrieved.History[0].Role)
	}
	if retrieved.History[1].TemplateSnapshot != asstMsg.TemplateSnapshot {
		t.Errorf("expected template snapshot in second message")
	}

	// AddMessage on a non-existent session should be a no-op (no panic).
	sm.AddMessage("s_nonexist", userMsg)
}

func TestSessionError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *SessionError
		contains string
	}{
		{
			name:     "not found",
			err:      &SessionError{Code: ErrCodeSessionNotFound, ID: "s_abc123"},
			contains: "not found",
		},
		{
			name:     "expired",
			err:      &SessionError{Code: ErrCodeSessionExpired, ID: "s_abc123"},
			contains: "expired",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.err.Error()
			if msg == "" {
				t.Fatal("expected non-empty error message")
			}
			if !contains(msg, tt.contains) {
				t.Errorf("expected error message to contain %q, got %q", tt.contains, msg)
			}
			if !contains(msg, tt.err.ID) {
				t.Errorf("expected error message to contain session ID %q, got %q", tt.err.ID, msg)
			}
		})
	}
}

// contains checks if s contains substr.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
