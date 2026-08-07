package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// SessionConfig holds session-related settings.
// Defaults match the ADR configuration section (§Configuration):
//   - timeout:          30m
//   - max_sessions:     100
//   - cleanup_interval: 5m
type SessionConfig struct {
	// Timeout is the inactivity duration after which a session expires.
	Timeout time.Duration

	// MaxSessions is the maximum number of concurrent sessions.
	MaxSessions int

	// CleanupInterval is how often the background goroutine sweeps
	// for expired sessions.
	CleanupInterval time.Duration
}

// DefaultSessionConfig returns the default session configuration
// as specified in the ADR configuration section.
func DefaultSessionConfig() SessionConfig {
	return SessionConfig{
		Timeout:         30 * time.Minute,
		MaxSessions:     100,
		CleanupInterval: 5 * time.Minute,
	}
}

// ── Data types matching OpenAPI schemas ──────────────────────────────────

// Session matches the OpenAPI Session schema.
// Holds the full conversation state for a single user interaction.
type Session struct {
	ID              string             `json:"id"`
	CurrentTemplate *GeneratedTemplate `json:"current_template"`
	History         []Message          `json:"history"`
	CreatedAt       time.Time          `json:"created_at"`
	LastActiveAt    time.Time          `json:"last_active_at"`
}

// GeneratedTemplate matches the OpenAPI GeneratedTemplate schema.
// Represents the most recent AI-generated template in a session.
type GeneratedTemplate struct {
	YAML             string    `json:"yaml"`
	SourceTemplates  []string  `json:"source_templates"`
	ValidationStatus string    `json:"validation_status"` // "valid", "invalid", "unchecked"
	LastModified     time.Time `json:"last_modified"`
}

// Message matches the OpenAPI Message schema.
// Represents a single turn in the conversation history.
type Message struct {
	Role             string             `json:"role"` // "user" or "assistant"
	Content          string             `json:"content"`
	TemplateSnapshot string             `json:"template_snapshot,omitempty"`
	SearchResults    []searchResultJSON `json:"search_results,omitempty"`
	Changes          []Change           `json:"changes,omitempty"`
	Timestamp        time.Time          `json:"timestamp"`
}

// Change matches the OpenAPI Change schema.
// Represents a single structural change between two versions of a template.
//
// NOTE: Change detection is deferred to a future phase. For now, the
// changes array is always empty ([]). When implemented, each Change
// should describe one atomic modification:
//   - Type: "added", "changed", or "removed"
//   - Path: dot-separated YAML path (e.g., "systemConfig.packages")
//   - From: the previous value (empty string for "added")
//   - To:   the new value (empty string for "removed")
type Change struct {
	Type string `json:"type"` // "added", "changed", "removed"
	Path string `json:"path"` // YAML path, e.g. "systemConfig.packages"
	From string `json:"from"`
	To   string `json:"to"`
}

// ── Session Manager ─────────────────────────────────────────────────────

// SessionManager maintains in-memory conversation sessions.
// It is safe for concurrent access from multiple HTTP handler goroutines.
type SessionManager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
	config   SessionConfig
	stopCh   chan struct{}
}

// NewSessionManager creates a new session manager and starts the
// background cleanup goroutine that evicts expired sessions.
func NewSessionManager(config SessionConfig) *SessionManager {
	sm := &SessionManager{
		sessions: make(map[string]*Session),
		config:   config,
		stopCh:   make(chan struct{}),
	}
	go sm.startCleanup()
	return sm
}

// Create creates a new empty session and returns it.
// The session ID follows the OpenAPI format: "s_" + 8 hex characters.
// Returns an error if the maximum number of sessions has been reached.
func (sm *SessionManager) Create() (*Session, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if len(sm.sessions) >= sm.config.MaxSessions {
		return nil, fmt.Errorf("maximum number of sessions (%d) reached", sm.config.MaxSessions)
	}

	id := generateSessionID()
	now := time.Now()

	session := &Session{
		ID:           id,
		History:      []Message{},
		CreatedAt:    now,
		LastActiveAt: now,
	}

	sm.sessions[id] = session
	return session, nil
}

// Get retrieves a session by ID.
// Returns the session or an error with a message indicating whether the
// session was not found or has expired.
func (sm *SessionManager) Get(id string) (*Session, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, exists := sm.sessions[id]
	if !exists {
		return nil, &SessionError{Code: ErrCodeSessionNotFound, ID: id}
	}

	// Check if the session has expired due to inactivity.
	if time.Since(session.LastActiveAt) > sm.config.Timeout {
		return nil, &SessionError{Code: ErrCodeSessionExpired, ID: id}
	}

	// Return a defensive copy so callers don't observe or cause races on the
	// internal mutable session state.
	cp := *session
	cp.History = append([]Message(nil), session.History...)
	if session.CurrentTemplate != nil {
		tmpl := *session.CurrentTemplate
		cp.CurrentTemplate = &tmpl
	}
	return &cp, nil
}

// Delete removes a session by ID.
// Returns an error if the session does not exist.
func (sm *SessionManager) Delete(id string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, exists := sm.sessions[id]; !exists {
		return &SessionError{Code: ErrCodeSessionNotFound, ID: id}
	}

	delete(sm.sessions, id)
	return nil
}

// Touch updates the session's LastActiveAt timestamp to now.
// Should be called on every interaction to keep the session alive.
func (sm *SessionManager) Touch(id string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if session, exists := sm.sessions[id]; exists {
		session.LastActiveAt = time.Now()
	}
}

// IsRefinement returns true if the session already has a generated
// template, meaning the next query should be treated as a refinement
// (skip RAG, modify existing template) rather than a fresh generation.
func (sm *SessionManager) IsRefinement(id string) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, exists := sm.sessions[id]
	if !exists {
		return false
	}
	return session.CurrentTemplate != nil
}

// UpdateTemplate sets the current template on a session.
func (sm *SessionManager) UpdateTemplate(id string, tmpl *GeneratedTemplate) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if session, exists := sm.sessions[id]; exists {
		session.CurrentTemplate = tmpl
	}
}

// AddMessage appends a message to the session's conversation history.
func (sm *SessionManager) AddMessage(id string, msg Message) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if session, exists := sm.sessions[id]; exists {
		session.History = append(session.History, msg)
	}
}

// Stop halts the background cleanup goroutine.
// Should be called during server shutdown.
func (sm *SessionManager) Stop() {
	close(sm.stopCh)
}

// startCleanup runs in a background goroutine and periodically removes
// sessions that have exceeded the inactivity timeout.
func (sm *SessionManager) startCleanup() {
	ticker := time.NewTicker(sm.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			sm.mu.Lock()
			for id, session := range sm.sessions {
				if time.Since(session.LastActiveAt) > sm.config.Timeout {
					delete(sm.sessions, id)
				}
			}
			sm.mu.Unlock()
		case <-sm.stopCh:
			return
		}
	}
}

// ── Helpers ─────────────────────────────────────────────────────────────

// SessionError represents a session-related error with a code that maps
// to the OpenAPI error codes (SESSION_NOT_FOUND, SESSION_EXPIRED).
type SessionError struct {
	Code string // One of the ErrCode* constants
	ID   string // The session ID that caused the error
}

func (e *SessionError) Error() string {
	switch e.Code {
	case ErrCodeSessionExpired:
		return fmt.Sprintf("Session '%s' has expired", e.ID)
	default:
		return fmt.Sprintf("Session '%s' not found", e.ID)
	}
}

// generateSessionID creates a session ID in the OpenAPI format:
// "s_" prefix + 8 random hex characters (e.g., "s_7f3a1b2c").
func generateSessionID() string {
	b := make([]byte, 4) // 4 bytes = 8 hex chars
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID if crypto/rand fails.
		return fmt.Sprintf("s_%08x", time.Now().UnixNano())
	}
	return "s_" + hex.EncodeToString(b)
}
