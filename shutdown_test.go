package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/afittestide/asimi/court"
	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/storage"
	"github.com/maximhq/bifrost/core/schemas"
)

// TestSessionStoreCloseWithTimeout verifies that Close() waits for pending saves
func TestSessionStoreCloseWithTimeout(t *testing.T) {
	// Create a temporary home directory for the test
	tmpHome := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", originalHome)

	// Initialize storage
	dbPath := filepath.Join(tmpHome, ".local", "share", "asimi", "asimi.sqlite")
	db, err := storage.InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize storage: %v", err)
	}
	defer db.Close()

	repoInfo := repo.RepoInfo{ProjectRoot: tmpHome}
	store, err := NewSessionStore(db, repoInfo, 10, 30)
	if err != nil {
		t.Fatalf("Failed to create session store: %v", err)
	}

	// Create a test session
	session := &court.Session{
		ID:           "test-session-123",
		CreatedAt:    time.Now(),
		LastUpdated:  time.Now(),
		FirstPrompt:  "Test prompt",
		Provider:     "test",
		Model:        "test-model",
		WorkingDir:   repoInfo.ProjectRoot,
		ContextFiles: make(map[string]string),
	}

	// Add a user message so the session will be saved
	session.SetMessages([]schemas.ChatMessage{
		textMessage(schemas.ChatMessageRoleUser, "test message"),
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

	// Verify the session was saved to the database
	loadedSession, err := store.LoadSession(session.ID)
	if err != nil {
		t.Errorf("Session was not saved to database: %v", err)
	}
	if loadedSession.ID != session.ID {
		t.Errorf("Expected session ID %s, got %s", session.ID, loadedSession.ID)
	}
}

// TestTUIModelShutdown verifies that shutdown() calls Close() on the session store
func TestTUIModelShutdown(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", originalHome)

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

	// Initialize storage
	dbPath := filepath.Join(tmpDir, ".local", "share", "asimi", "asimi.sqlite")
	db, err := storage.InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize storage: %v", err)
	}
	defer db.Close()

	// Create a session store using NewSessionStore
	repoInfo := repo.RepoInfo{ProjectRoot: tmpDir}
	store, err := NewSessionStore(db, repoInfo, 10, 30)
	if err != nil {
		t.Fatalf("Failed to create session store: %v", err)
	}

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
