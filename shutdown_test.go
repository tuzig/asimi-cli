package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tmc/langchaingo/llms"
)

// TestSessionStoreCloseWithTimeout verifies that Close() waits for pending saves
func TestSessionStoreCloseWithTimeout(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := t.TempDir()
	
	// Create a session store
	store := &SessionStore{
		storageDir:  tmpDir,
		maxSessions: 10,
		maxAgeDays:  30,
		saveChan:    make(chan *Session, 100),
		stopChan:    make(chan struct{}),
	}
	
	// Start the save worker
	go store.saveWorker()
	
	// Create a test session
	session := &Session{
		ID:          "test-session-123",
		CreatedAt:   time.Now(),
		LastUpdated: time.Now(),
		FirstPrompt: "Test prompt",
		Provider:    "test",
		Model:       "test-model",
		WorkingDir:  tmpDir,
		ProjectSlug: "test-project",
		ContextFiles: make(map[string]string),
	}
	
	// Add a user message so the session will be saved
	session.Messages = append(session.Messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextPart("test message")},
	})
	
	// Queue a save
	store.SaveSession(session)
	
	// Close the store (should wait for the save to complete)
	start := time.Now()
	store.Close()
	duration := time.Since(start)
	
	// Verify the close completed within a reasonable time (should be < 2 seconds timeout)
	if duration > 3*time.Second {
		t.Errorf("Close() took too long: %v", duration)
	}
	
	// Verify the session was saved
	sessionDir := filepath.Join(tmpDir, "test-project", "session-test-session-123")
	sessionFile := filepath.Join(sessionDir, "session.json")
	
	// Give it a moment for the file system to sync
	time.Sleep(50 * time.Millisecond)
	
	if _, err := os.Stat(sessionFile); os.IsNotExist(err) {
		t.Errorf("Session file was not created: %s", sessionFile)
	}
}

// TestTUIModelShutdown verifies that shutdown() calls Close() on the session store
func TestTUIModelShutdown(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := t.TempDir()
	
	// Create a minimal config
	config := &Config{
		LLM: LLMConfig{
			Provider: "test",
			Model:    "test-model",
		},
		Session: SessionConfig{
			Enabled:     true,
			AutoSave:    true,
			MaxSessions: 10,
			MaxAgeDays:  30,
		},
	}
	
	// Create a session store
	store := &SessionStore{
		storageDir:  tmpDir,
		maxSessions: 10,
		maxAgeDays:  30,
		saveChan:    make(chan *Session, 100),
		stopChan:    make(chan struct{}),
	}
	
	// Start the save worker
	go store.saveWorker()
	
	// Create a TUI model
	model := &TUIModel{
		config:       config,
		sessionStore: store,
	}
	
	// Call shutdown
	model.shutdown()
	
	// Verify that the stop channel was closed by trying to receive from it
	select {
	case <-store.stopChan:
		// Good - channel was closed
	case <-time.After(100 * time.Millisecond):
		t.Error("shutdown() did not close the session store")
	}
}
